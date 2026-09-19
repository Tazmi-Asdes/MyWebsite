package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fixedClock struct {
	value time.Time
}

func (c fixedClock) Now() time.Time {
	return c.value
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := NewHandler(HandlerOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:  fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func TestPublicRoutesRenderIndependentPages(t *testing.T) {
	handler := testHandler(t)
	routes := []struct {
		path        string
		title       string
		description string
	}{
		{path: "/", title: "首页 — MyWebsite", description: "一个简洁、安静的个人网站首页。"},
		{path: "/articles", title: "文章 — MyWebsite", description: "记录思考、实践与长期积累的文章。"},
		{path: "/projects", title: "项目 — MyWebsite", description: "正在做过、正在做和想要继续做的项目。"},
		{path: "/about", title: "关于 — MyWebsite", description: "关于我、我的工作方式，以及这个网站。"},
	}

	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, route.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want HTML", got)
			}
			body := response.Body.String()
			if !strings.Contains(body, "<title>"+route.title+"</title>") {
				t.Fatalf("body does not contain independent title %q", route.title)
			}
			if !strings.Contains(body, `content="`+route.description+`"`) {
				t.Fatalf("body does not contain independent description %q", route.description)
			}
			for _, navigationPath := range []string{"/", "/articles", "/projects", "/about"} {
				if !strings.Contains(body, `href="`+navigationPath+`"`) {
					t.Fatalf("body does not contain navigation link %q", navigationPath)
				}
			}
			if !strings.Contains(body, "暂时") && !strings.Contains(body, "正在准备中") {
				t.Fatalf("body does not contain a Chinese empty state")
			}
			if !strings.Contains(body, "© 2026 MyWebsite") {
				t.Fatalf("body does not use Asia/Shanghai current year")
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if !strings.Contains(response.Body.String(), "页面不存在") {
		t.Fatalf("404 body does not contain the Chinese not-found state")
	}
}

func TestLive(t *testing.T) {
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/-/live", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("body = %q, want fixed live response", got)
	}
}

func TestReadyIsNotReadyUntilDependenciesAreConfigured(t *testing.T) {
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/-/ready", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	if !strings.Contains(response.Body.String(), `"code":"dependencies_not_configured"`) {
		t.Fatalf("body = %q, want stable dependencies_not_configured code", response.Body.String())
	}
}

func TestRequestIDIsGeneratedAndReturned(t *testing.T) {
	handler := testHandler(t)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))

	firstID := first.Header().Get("X-Request-ID")
	secondID := second.Header().Get("X-Request-ID")
	if len(firstID) != 32 || len(secondID) != 32 {
		t.Fatalf("request IDs have lengths %d and %d, want 32 hex characters", len(firstID), len(secondID))
	}
	if firstID == secondID {
		t.Fatalf("request IDs should be unique, both were %q", firstID)
	}
}

func TestAssetsAreServedFromEmbeddedFilesystem(t *testing.T) {
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/styles.css", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), ".site-header") {
		t.Fatalf("embedded stylesheet was not served")
	}
}
