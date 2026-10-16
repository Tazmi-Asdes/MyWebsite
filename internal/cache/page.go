// Package cache contains the optional server-side cache used by public HTML
// pages. The cache is deliberately an acceleration layer: callers must keep
// rendering a page when Redis is unavailable.
package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/oklog/ulid/v2"
)

const (
	publicCacheControl = "public, max-age=0, must-revalidate"
	pagePathPrefix     = "page:"
)

// Page is the complete response representation that may be replayed for a
// public page. Only successful HTML and whitelisted JSON responses are stored.
type Page struct {
	Body        []byte
	ETag        string
	ContentType string
}

// PageCache is the small contract used by the HTTP middleware and admin API.
// The version is read once before rendering so a concurrent invalidation can
// never make a newly rendered old page visible under the new version.
type PageCache interface {
	CurrentVersion(context.Context) (string, error)
	Get(context.Context, string, string) (Page, bool, error)
	Set(context.Context, string, string, Page) error
	Invalidate(context.Context) error
}

// Middleware caches anonymous public HTML and explicitly whitelisted JSON
// routes. A nil cache is valid and turns the middleware into a
// browser-revalidation wrapper only.
func Middleware(next http.Handler, store PageCache, logger *slog.Logger) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !eligibleRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		version := ""
		if store != nil {
			value, err := store.CurrentVersion(r.Context())
			if err != nil {
				logger.Warn("public_cache_version_failed", "error", err, "path", r.URL.Path)
			} else {
				version = value
				if page, ok, getErr := store.Get(r.Context(), version, r.URL.RequestURI()); getErr != nil {
					logger.Warn("public_cache_get_failed", "error", getErr, "path", r.URL.Path)
				} else if ok && isContentTypeForPath(r.URL.Path, page.ContentType) {
					writePage(w, r, page)
					return
				}
			}
		}

		capture := newCaptureWriter(w)
		next.ServeHTTP(capture, r)
		status := capture.statusCode()
		body := capture.body.Bytes()
		if status != http.StatusOK || !isContentTypeForPath(r.URL.Path, capture.Header().Get("Content-Type")) || len(capture.Header().Values("Set-Cookie")) != 0 {
			capture.writeTo(w)
			return
		}

		page := Page{
			Body:        bytes.Clone(body),
			ContentType: capture.Header().Get("Content-Type"),
			ETag:        etag(version, body),
		}
		setPageHeaders(w, page)
		if matchesETag(r.Header.Get("If-None-Match"), page.ETag) {
			w.Header().Del("Content-Length")
			w.WriteHeader(http.StatusNotModified)
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(page.Body)
		}
		if store != nil && version != "" {
			if err := store.Set(r.Context(), version, r.URL.RequestURI(), page); err != nil {
				logger.Warn("public_cache_set_failed", "error", err, "path", r.URL.Path)
			}
		}
	})
}

// eligibleRequest intentionally rejects every request carrying a cookie or
// authorization token. An administrator can therefore never receive a page
// captured for another anonymous browser.
func eligibleRequest(r *http.Request) bool {
	if r == nil || r.Method != http.MethodGet || r.URL == nil {
		return false
	}
	if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		return false
	}
	return isPublicHTMLPath(r.URL.Path) || isPublicJSONPath(r.URL.Path)
}

func isPublicHTMLPath(path string) bool {
	switch path {
	case "/", "/articles", "/projects", "/about":
		return true
	default:
		return false
	}
}

func isPublicJSONPath(path string) bool {
	switch path {
	case "/api/public/v1/home", "/api/public/v1/articles", "/api/public/v1/projects", "/api/public/v1/about":
		return true
	}
	const articlePrefix = "/api/public/v1/articles/"
	if !strings.HasPrefix(path, articlePrefix) {
		return false
	}
	value := strings.TrimPrefix(path, articlePrefix)
	if value == "" || strings.Contains(value, "/") {
		return false
	}
	_, err := ulid.ParseStrict(value)
	return err == nil
}

func isHTML(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "text/html")
}

func isPublicContentType(contentType string) bool {
	if isHTML(contentType) {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func isContentTypeForPath(path, contentType string) bool {
	if isPublicJSONPath(path) {
		mediaType, _, err := mime.ParseMediaType(contentType)
		return err == nil && strings.EqualFold(mediaType, "application/json")
	}
	return isPublicHTMLPath(path) && isHTML(contentType)
}

func writePage(w http.ResponseWriter, r *http.Request, page Page) {
	setPageHeaders(w, page)
	if matchesETag(r.Header.Get("If-None-Match"), page.ETag) {
		w.Header().Del("Content-Length")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(page.Body)
}

func setPageHeaders(w http.ResponseWriter, page Page) {
	w.Header().Set("Cache-Control", publicCacheControl)
	if page.ETag != "" {
		w.Header().Set("ETag", page.ETag)
	}
	if page.ContentType != "" {
		w.Header().Set("Content-Type", page.ContentType)
	}
}

func etag(version string, body []byte) string {
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	if version == "" {
		return `"` + hash + `"`
	}
	return `"v` + version + `-` + hash + `"`
}

func matchesETag(header, current string) bool {
	if current == "" || strings.TrimSpace(header) == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		value := strings.TrimPrefix(current, "W/")
		if candidate == value {
			return true
		}
	}
	return false
}

type captureWriter struct {
	destination http.ResponseWriter
	header      http.Header
	body        bytes.Buffer
	status      int
	written     bool
}

func newCaptureWriter(destination http.ResponseWriter) *captureWriter {
	return &captureWriter{destination: destination, header: destination.Header()}
}

func (w *captureWriter) Header() http.Header { return w.header }

func (w *captureWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	w.status = status
}

func (w *captureWriter) Write(body []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(body)
}

func (w *captureWriter) statusCode() int {
	if !w.written {
		return http.StatusOK
	}
	return w.status
}

func (w *captureWriter) writeTo(destination http.ResponseWriter) {
	for key, values := range w.header {
		destination.Header()[key] = append([]string(nil), values...)
	}
	destination.WriteHeader(w.statusCode())
	if w.body.Len() > 0 && w.statusCode() != http.StatusNoContent && w.statusCode() != http.StatusNotModified {
		_, _ = io.Copy(destination, &w.body)
	}
}

// PageKey is exported for diagnostics and tests. The request target is
// hashed to keep Redis key size bounded while retaining a one-to-one mapping
// for the complete path and query string.
func PageKey(prefix, version, requestTarget string) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = "mywebsite:public:"
	}
	digest := sha256.Sum256([]byte(requestTarget))
	return strings.TrimRight(prefix, ":") + ":" + pagePathPrefix + version + ":" + hex.EncodeToString(digest[:])
}

// ValidatePage prevents malformed values from being replayed if Redis was
// written by an incompatible deployment.
func ValidatePage(page Page) error {
	if len(page.Body) == 0 {
		return errors.New("public cache page body is empty")
	}
	if page.ETag == "" {
		return errors.New("public cache page etag is empty")
	}
	if !isPublicContentType(page.ContentType) {
		return fmt.Errorf("public cache content type %q is not HTML or JSON", page.ContentType)
	}
	return nil
}
