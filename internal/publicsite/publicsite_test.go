package publicsite_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/platform"
	"mywebsite/internal/publicsite"
	publicassets "mywebsite/web/public"
)

const testArticleULID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

type fakeArticleReader struct {
	items       []article.PublishedArticle
	total       int64
	article     article.Article
	countErr    error
	listErr     error
	detailErr   error
	listLimit   int32
	listOffset  int32
	listCalled  bool
	detailInput string
}

func (f *fakeArticleReader) ListPublished(_ context.Context, limit, offset int32) ([]article.PublishedArticle, error) {
	f.listCalled = true
	f.listLimit = limit
	f.listOffset = offset
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.items, nil
}

func (f *fakeArticleReader) CountPublished(context.Context) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.total, nil
}

func (f *fakeArticleReader) GetPublishedByULID(_ context.Context, value string) (article.Article, error) {
	f.detailInput = value
	if f.detailErr != nil {
		return article.Article{}, f.detailErr
	}
	return f.article, nil
}

func newPublicHandler(t *testing.T, reader ...publicsite.ArticleReader) http.Handler {
	t.Helper()
	handler, err := publicsite.NewHandler(publicassets.Files, fixedClock{}, reader...)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 9, 19, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
}

func request(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestArticlesWithoutReaderRemainEmptyAndRejectUnknownQuery(t *testing.T) {
	handler := newPublicHandler(t)
	response := request(t, handler, "/articles")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "暂时还没有文章") {
		t.Fatalf("empty articles response = %d %s", response.Code, response.Body.String())
	}
	if response := request(t, handler, "/articles?page=2"); response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured page 2 status = %d", response.Code)
	}
	if response := request(t, handler, "/articles?source=test"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown query status = %d", response.Code)
	}
}

func TestPublicTemplatesExposeSkipNavigation(t *testing.T) {
	articleReader := &fakeArticleReader{article: article.Article{
		Title:       "可访问文章",
		Status:      article.StatusPublished,
		BodyHTML:    stringPointer("<p>正文</p>"),
		PreviewText: stringPointer("文章摘要"),
	}}
	pages := []struct {
		name    string
		handler http.Handler
		target  string
	}{
		{name: "home", handler: newPublicHandler(t), target: "/"},
		{name: "articles", handler: newPublicHandler(t), target: "/articles"},
		{name: "article", handler: newPublicHandler(t, articleReader), target: "/articles/" + testArticleULID},
		{name: "projects", handler: newPublicHandler(t), target: "/projects"},
		{name: "about", handler: newPublicHandler(t), target: "/about"},
		{name: "404", handler: newPublicHandler(t), target: "/missing"},
		{name: "500", handler: newPublicHandler(t, &fakeArticleReader{listErr: errors.New("database unavailable")}), target: "/"},
	}

	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			response := request(t, page.handler, page.target)
			body := response.Body.String()
			if page.name == "404" && response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
			}
			if page.name == "500" && response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
			if page.name != "404" && page.name != "500" && response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if strings.Count(body, `class="skip-link" href="#main-content"`) != 1 {
				t.Fatalf("skip link count = %d, body = %s", strings.Count(body, `class="skip-link" href="#main-content"`), body)
			}
			if strings.Count(body, `id="main-content"`) != 1 {
				t.Fatalf("main content id count = %d, body = %s", strings.Count(body, `id="main-content"`), body)
			}
		})
	}
}

func TestArticlesPaginationAndStrictPageQuery(t *testing.T) {
	reader := &fakeArticleReader{total: 11, items: []article.PublishedArticle{{
		Title:            "第 2 页文章",
		PublicULID:       stringPointer(testArticleULID),
		PreviewText:      stringPointer("文章摘要"),
		FirstPublishedAt: timePointer(time.Date(2026, 9, 18, 16, 0, 0, 0, time.UTC)),
	}}}
	handler := newPublicHandler(t, reader)
	response := request(t, handler, "/articles?page=2")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "第 2 页文章") || !strings.Contains(response.Body.String(), "2026-09-19") {
		t.Fatalf("page 2 response = %d %s", response.Code, response.Body.String())
	}
	if reader.listLimit != 10 || reader.listOffset != 10 || !reader.listCalled {
		t.Fatalf("list arguments = limit %d offset %d called %t", reader.listLimit, reader.listOffset, reader.listCalled)
	}

	for _, target := range []string{
		"/articles?page=0",
		"/articles?page=01",
		"/articles?page=-1",
		"/articles?page=abc",
		"/articles?page=1&page=2",
		"/articles?page=1&sort=title",
	} {
		if response := request(t, handler, target); response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", target, response.Code)
		}
	}
	if response := request(t, handler, "/articles?page=3"); response.Code != http.StatusNotFound {
		t.Errorf("out of range status = %d, want 404", response.Code)
	}
}

func TestArticleDetailRendersPublishedContentAndTOC(t *testing.T) {
	reader := &fakeArticleReader{article: article.Article{
		Title:            "从本地开发到服务器部署",
		Status:           article.StatusPublished,
		BodyHTML:         stringPointer("<h2 id=\"intro\">介绍</h2><p>正文内容</p>"),
		TOCJSON:          []byte(`{"items":[{"id":"intro","text":"介绍","level":2}]}`),
		PreviewText:      stringPointer("构建、发布与验证的实践记录。"),
		FirstPublishedAt: timePointer(time.Date(2026, 9, 18, 16, 0, 0, 0, time.UTC)),
	}}
	handler := newPublicHandler(t, reader)
	response := request(t, handler, "/articles/"+testArticleULID)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "从本地开发到服务器部署") || !strings.Contains(body, "<h2 id=\"intro\">介绍</h2>") || !strings.Contains(body, "介绍") || !strings.Contains(body, "2026-09-19") {
		t.Fatalf("detail response = %d %s", response.Code, body)
	}
	if !strings.Contains(body, `property="og:title" content="从本地开发到服务器部署"`) || !strings.Contains(body, `property="og:description" content="构建、发布与验证的实践记录。"`) {
		t.Fatalf("detail metadata missing: %s", body)
	}
	if reader.detailInput != testArticleULID {
		t.Fatalf("detail ULID = %q", reader.detailInput)
	}
	if strings.Count(body, "返回文章列表") != 1 {
		t.Fatalf("back link count = %d", strings.Count(body, "返回文章列表"))
	}
}

func TestArticleDetailMapsMissingAndStorageErrors(t *testing.T) {
	for name, err := range map[string]error{
		"missing":       sql.ErrNoRows,
		"not published": article.ErrNotPublished,
	} {
		t.Run(name, func(t *testing.T) {
			reader := &fakeArticleReader{detailErr: err}
			handler := newPublicHandler(t, reader)
			if response := request(t, handler, "/articles/"+testArticleULID); response.Code != http.StatusNotFound {
				t.Fatalf("status = %d", response.Code)
			}
		})
	}
	draftReader := &fakeArticleReader{article: article.Article{Status: article.StatusDraft}}
	if response := request(t, newPublicHandler(t, draftReader), "/articles/"+testArticleULID); response.Code != http.StatusNotFound {
		t.Fatalf("draft detail status = %d", response.Code)
	}
	if response := request(t, newPublicHandler(t, &fakeArticleReader{detailErr: errors.New("database unavailable")}), "/articles/"+testArticleULID); response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatalf("storage detail response = %d %s", response.Code, response.Body.String())
	}
	if response := request(t, newPublicHandler(t, &fakeArticleReader{countErr: errors.New("database unavailable")}), "/articles"); response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatalf("storage list response = %d %s", response.Code, response.Body.String())
	}
	if response := request(t, newPublicHandler(t, &fakeArticleReader{detailErr: nil}), "/articles/not-a-ulid"); response.Code != http.StatusNotFound {
		t.Fatalf("invalid ULID status = %d", response.Code)
	}
}

func stringPointer(value string) *string { return &value }

func timePointer(value time.Time) *time.Time { return &value }

var _ platform.Clock = fixedClock{}
