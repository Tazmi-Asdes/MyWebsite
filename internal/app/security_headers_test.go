package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/cache"
)

type securityHeaderPageCache struct {
	version string
	pages   map[string]cache.Page
}

func newSecurityHeaderPageCache() *securityHeaderPageCache {
	return &securityHeaderPageCache{version: "1", pages: make(map[string]cache.Page)}
}

func (c *securityHeaderPageCache) CurrentVersion(context.Context) (string, error) {
	return c.version, nil
}

func (c *securityHeaderPageCache) Get(_ context.Context, version, target string) (cache.Page, bool, error) {
	page, ok := c.pages[version+"|"+target]
	return page, ok, nil
}

func (c *securityHeaderPageCache) Set(_ context.Context, version, target string, page cache.Page) error {
	c.pages[version+"|"+target] = page
	return nil
}

func (c *securityHeaderPageCache) Invalidate(context.Context) error {
	c.version = "2"
	return nil
}

func TestSecurityHeadersCoverRoutesAndCachedResponses(t *testing.T) {
	handler, err := NewHandler(HandlerOptions{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)},
		Auth:      handlerAuthStub{},
		Articles:  handlerArticleStub{},
		PageCache: newSecurityHeaderPageCache(),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	firstPage := performSecurityHeaderRequest(handler, http.MethodGet, "/", "")
	if firstPage.Code != http.StatusOK {
		t.Fatalf("first public response = %d, want 200", firstPage.Code)
	}
	requireSecurityHeaders(t, firstPage)

	cachedPage := performSecurityHeaderRequest(handler, http.MethodGet, "/", "")
	if cachedPage.Code != http.StatusOK {
		t.Fatalf("cached public response = %d, want 200", cachedPage.Code)
	}
	requireSecurityHeaders(t, cachedPage)

	conditionalPage := performSecurityHeaderRequest(handler, http.MethodGet, "/", firstPage.Header().Get("ETag"))
	if conditionalPage.Code != http.StatusNotModified {
		t.Fatalf("conditional public response = %d, want 304", conditionalPage.Code)
	}
	requireSecurityHeaders(t, conditionalPage)

	for _, request := range []struct {
		name string
		path string
		code int
	}{
		{name: "unauthorized API", path: "/api/v1/session", code: http.StatusUnauthorized},
		{name: "not found error", path: "/missing", code: http.StatusNotFound},
		{name: "asset", path: "/assets/styles.css", code: http.StatusOK},
	} {
		t.Run(request.name, func(t *testing.T) {
			response := performSecurityHeaderRequest(handler, http.MethodGet, request.path, "")
			if response.Code != request.code {
				t.Fatalf("response status = %d, want %d", response.Code, request.code)
			}
			requireSecurityHeaders(t, response)
		})
	}

	methodNotAllowed := performSecurityHeaderRequest(handler, http.MethodPatch, "/api/v1/session", "")
	if methodNotAllowed.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported API method response = %d, want 405", methodNotAllowed.Code)
	}
	if got := methodNotAllowed.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("unsupported API method Cache-Control = %q, want no-store", got)
	}
	requireSecurityHeaders(t, methodNotAllowed)

	apiRoot := performSecurityHeaderRequest(handler, http.MethodGet, "/api/v1", "")
	if got := apiRoot.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("API root Cache-Control = %q, want no-store", got)
	}
	requireSecurityHeaders(t, apiRoot)

	unknownAPI := performSecurityHeaderRequest(handler, http.MethodGet, "/api/v1/no-such-route", "")
	if unknownAPI.Code != http.StatusNotFound {
		t.Errorf("unknown API route response = %d, want 404", unknownAPI.Code)
	}
	if got := unknownAPI.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("unknown API route Cache-Control = %q, want no-store", got)
	}
	requireSecurityHeaders(t, unknownAPI)
	if got := firstPage.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Errorf("public page Cache-Control = %q, want public revalidation policy", got)
	}
}

func performSecurityHeaderRequest(handler http.Handler, method, path, etag string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requireSecurityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	wantHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": contentSecurityPolicy,
	}
	for name, want := range wantHeaders {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := response.Header().Get("Content-Security-Policy-Report-Only"); got != "" {
		t.Errorf("Content-Security-Policy-Report-Only = %q, want no report-only policy without a reporting endpoint", got)
	}
	policy := response.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src", "script-src", "style-src", "connect-src"} {
		if strings.Contains(policy, directive) {
			t.Errorf("enforced Content-Security-Policy unexpectedly contains %q: %q", directive, policy)
		}
	}
}
