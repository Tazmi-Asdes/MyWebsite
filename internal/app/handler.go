package app

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"time"

	"mywebsite/internal/platform"
	"mywebsite/internal/publicsite"
	publicassets "mywebsite/web/public"
)

// HandlerOptions controls the dependencies used by the HTTP handler.
type HandlerOptions struct {
	Logger *slog.Logger
	Clock  platform.Clock
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

	publicHandler, err := publicsite.NewHandler(publicassets.Files, clock)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	publicHandler.RegisterRoutes(mux)
	mux.Handle("GET /-/live", http.HandlerFunc(liveHandler))
	mux.Handle("GET /-/ready", http.HandlerFunc(readyHandler))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(publicassets.Assets))))

	return requestLoggingMiddleware(mux, logger), nil
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

func readyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, `{"type":"about:blank","title":"服务尚未就绪","status":503,"detail":"Stage 0 依赖尚未配置","code":"dependencies_not_configured"}`)
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
