package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"mywebsite/internal/adminapi"
	"mywebsite/internal/platform"
	"mywebsite/internal/publicsite"
	publicassets "mywebsite/web/public"
)

// Readiness is the minimal dependency contract used by the readiness probe.
type Readiness interface {
	CheckReady(context.Context) error
}

// ArticleServices is the shared article dependency used by the administrator
// API and the public article renderer.
type ArticleServices interface {
	adminapi.ArticleService
	publicsite.ArticleReader
}

// ProjectServices is the shared project dependency used by the administrator
// API and the public project renderer.
type ProjectServices interface {
	adminapi.ProjectService
	publicsite.ProjectReader
}

// MediaServices is the shared media dependency used by the administrator API
// and the public media renderer.
type MediaServices interface {
	adminapi.MediaService
	publicsite.MediaReader
}

// HandlerOptions controls the dependencies used by the HTTP handler.
type HandlerOptions struct {
	Logger         *slog.Logger
	Clock          platform.Clock
	Readiness      Readiness
	Auth           adminapi.AuthService
	Articles       ArticleServices
	Projects       ProjectServices
	Media          MediaServices
	MaxUploadBytes int64
	PublicBaseURL  string
	CookieSecure   bool
}

// NewHandler creates the Stage 0 HTTP handler and all of its routes.
func NewHandler(options HandlerOptions) (http.Handler, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := options.Clock
	if clock == nil {
		clock = platform.NewShanghaiClock()
	}

	if (options.Auth == nil) != (options.Articles == nil) {
		return nil, fmt.Errorf("auth and article services must be configured together")
	}

	var (
		publicHandler *publicsite.Handler
		err           error
	)
	if options.Projects != nil || options.Media != nil {
		publicHandler, err = publicsite.NewHandlerWithStage2(publicassets.Files, clock, publicsite.Stage2Options{
			ArticleReader: options.Articles,
			ProjectReader: options.Projects,
			MediaReader:   options.Media,
		})
	} else if options.Articles == nil {
		publicHandler, err = publicsite.NewHandler(publicassets.Files, clock)
	} else {
		publicHandler, err = publicsite.NewHandlerWithArticles(publicassets.Files, clock, options.Articles)
	}
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	publicHandler.RegisterRoutes(mux)
	if options.Auth != nil {
		adminHandler, adminErr := adminapi.NewHandler(adminapi.HandlerOptions{
			Auth:           options.Auth,
			Articles:       options.Articles,
			Projects:       options.Projects,
			Media:          options.Media,
			MaxUploadBytes: options.MaxUploadBytes,
			PublicBaseURL:  options.PublicBaseURL,
			CookieSecure:   options.CookieSecure,
			Now:            clock.Now,
		})
		if adminErr != nil {
			return nil, adminErr
		}
		// Go's ServeMux rejects an all-method prefix pattern alongside the
		// public `GET /` pattern. Register the API prefix for each method it
		// exposes so the API remains more specific without being shadowed.
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			mux.Handle(method+" /api/v1/", adminHandler)
		}
	}
	mux.Handle("GET /-/live", http.HandlerFunc(liveHandler))
	mux.Handle("GET /-/ready", readyHandler(options.Readiness))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(publicassets.Assets))))

	return requestLoggingMiddleware(mux, logger), nil
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

func readyHandler(readiness Readiness) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if readiness != nil && readiness.CheckReady(r.Context()) == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"ok"}`)
			return
		}

		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"type":"about:blank","title":"服务尚未就绪","status":503,"detail":"数据库或 Schema 尚未就绪","code":"dependencies_not_configured"}`)
	})
}

func requestLoggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := newRequestID()
		if err != nil {
			logger.Error("request_id_generation_failed", "method", r.Method, "path", r.URL.Path, "error", err)
			http.Error(w, "内部服务器错误", http.StatusInternalServerError)
			return
		}

		w.Header().Set("X-Request-ID", requestID)
		recorder := &statusRecorder{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.statusCode(),
			"duration", time.Since(started),
			"request_id", requestID,
		)
	})
}

func newRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.written {
		return
	}
	r.status = status
	r.written = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}

func (r *statusRecorder) statusCode() int {
	if r.written {
		return r.status
	}
	return http.StatusOK
}
