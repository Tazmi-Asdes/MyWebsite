package cache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePageCache struct {
	version       string
	pages         map[string]Page
	currentErr    error
	getErr        error
	setErr        error
	invalidateErr error
	invalidations int
}

func newFakePageCache() *fakePageCache {
	return &fakePageCache{version: "1", pages: make(map[string]Page)}
}

func (f *fakePageCache) CurrentVersion(context.Context) (string, error) {
	if f.currentErr != nil {
		return "", f.currentErr
	}
	return f.version, nil
}

func (f *fakePageCache) Get(_ context.Context, version, target string) (Page, bool, error) {
	if f.getErr != nil {
		return Page{}, false, f.getErr
	}
	page, ok := f.pages[version+"|"+target]
	return page, ok, nil
}

func (f *fakePageCache) Set(_ context.Context, version, target string, page Page) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.pages[version+"|"+target] = page
	return nil
}

func (f *fakePageCache) Invalidate(context.Context) error {
	if f.invalidateErr != nil {
		return f.invalidateErr
	}
	f.invalidations++
	f.version = "2"
	return nil
}

func TestEligibleRequest(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		target   string
		cookie   string
		authoriz string
		want     bool
	}{
		{name: "home", method: http.MethodGet, target: "/", want: true},
		{name: "article page query", method: http.MethodGet, target: "/articles?page=2", want: true},
		{name: "invalid public path", method: http.MethodGet, target: "/articles/01ARZ3NDEKTSV4RRFFQ69G5FAV", want: false},
		{name: "asset", method: http.MethodGet, target: "/assets/site.css", want: false},
		{name: "post", method: http.MethodPost, target: "/", want: false},
		{name: "cookie", method: http.MethodGet, target: "/about", cookie: "session=secret", want: false},
		{name: "authorization", method: http.MethodGet, target: "/projects", authoriz: "Bearer secret", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			if test.cookie != "" {
				request.Header.Set("Cookie", test.cookie)
			}
			if test.authoriz != "" {
				request.Header.Set("Authorization", test.authoriz)
			}
			if got := eligibleRequest(request); got != test.want {
				t.Fatalf("eligibleRequest() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMiddlewareCachesAndRevalidatesHTML(t *testing.T) {
	store := newFakePageCache()
	calls := 0
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>cached</html>"))
	}), store, nil)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/articles?page=2", nil))
	if first.Code != http.StatusOK || first.Body.String() != "<html>cached</html>" {
		t.Fatalf("first response = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != publicCacheControl {
		t.Fatalf("first cache headers = %#v", first.Header())
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/articles?page=2", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional response = %d %q", second.Code, second.Body.String())
	}
	if calls != 1 {
		t.Fatalf("downstream calls = %d, want 1", calls)
	}

	store.Invalidate(context.Background())
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, httptest.NewRequest(http.MethodGet, "/articles?page=2", nil))
	if third.Code != http.StatusOK || calls != 2 {
		t.Fatalf("after invalidation response/calls = %d/%d", third.Code, calls)
	}
	if third.Header().Get("ETag") == etag {
		t.Fatal("ETag did not change after version invalidation")
	}
	if store.invalidations != 1 {
		t.Fatalf("invalidations = %d, want 1", store.invalidations)
	}
}

func TestMiddlewareRedisFailureFallsBackAndKeepsBrowserRevalidation(t *testing.T) {
	store := newFakePageCache()
	store.currentErr = errors.New("redis unavailable")
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("fallback"))
	}), store, nil)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/about", nil))
	if first.Code != http.StatusOK || first.Body.String() != "fallback" {
		t.Fatalf("fallback response = %d %q", first.Code, first.Body.String())
	}
	if first.Header().Get("Cache-Control") != publicCacheControl || first.Header().Get("ETag") == "" {
		t.Fatalf("fallback cache headers = %#v", first.Header())
	}

	conditional := httptest.NewRequest(http.MethodGet, "/about", nil)
	conditional.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, conditional)
	if second.Code != http.StatusNotModified {
		t.Fatalf("fallback conditional response = %d", second.Code)
	}
}

func TestPageKeyAndValidatePage(t *testing.T) {
	first := PageKey(DefaultPrefix, "1", "/articles?page=1")
	second := PageKey(DefaultPrefix, "1", "/articles?page=2")
	if first == second || !strings.HasPrefix(first, "mywebsite:public:page:1:") {
		t.Fatalf("page keys = %q and %q", first, second)
	}
	valid := Page{Body: []byte("<html>ok</html>"), ETag: `"etag"`, ContentType: "text/html; charset=utf-8"}
	if err := ValidatePage(valid); err != nil {
		t.Fatalf("ValidatePage(valid) error = %v", err)
	}
	for _, invalid := range []Page{
		{ETag: valid.ETag, ContentType: valid.ContentType},
		{Body: valid.Body, ContentType: valid.ContentType},
		{Body: valid.Body, ETag: valid.ETag, ContentType: "application/json"},
	} {
		if err := ValidatePage(invalid); err == nil {
			t.Fatalf("ValidatePage(%+v) unexpectedly succeeded", invalid)
		}
	}
}
