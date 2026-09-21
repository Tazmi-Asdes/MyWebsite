package publicsite_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
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
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "暂时还没有文章") {
		t.Fatalf("empty articles response = %d %s", response.Code, body)
	}
	if strings.Contains(body, `class="page-shell"`) || strings.Count(body, `<header class="page-heading">`) != 1 || strings.Count(body, `<div class="container">`) != 2 {
		t.Fatalf("empty article heading structure is incorrect: %s", body)
	}
	if strings.Count(body, `<section class="section" aria-label="文章列表">`) != 1 || strings.Count(body, `<div class="empty-state" aria-live="polite">`) != 1 {
		t.Fatalf("empty article section structure is incorrect: %s", body)
	}
	if strings.Contains(body, `class="article-list"`) || strings.Contains(body, `class="article-list__item"`) || strings.Contains(body, `<nav class="pagination" aria-label="文章分页">`) {
		t.Fatalf("empty article page unexpectedly contains filled-list structure: %s", body)
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
		name       string
		handler    http.Handler
		target     string
		activeHref string
	}{
		{name: "home", handler: newPublicHandler(t), target: "/", activeHref: "/"},
		{name: "articles", handler: newPublicHandler(t), target: "/articles", activeHref: "/articles"},
		{name: "article", handler: newPublicHandler(t, articleReader), target: "/articles/" + testArticleULID, activeHref: "/articles"},
		{name: "projects", handler: newPublicHandler(t), target: "/projects", activeHref: "/projects"},
		{name: "about", handler: newPublicHandler(t), target: "/about", activeHref: "/about"},
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
			if strings.Count(body, `<details class="site-menu">`) != 1 || strings.Count(body, `<summary class="menu-button">菜单</summary>`) != 1 || strings.Count(body, `<nav class="site-nav" aria-label="主要导航">`) != 1 {
				t.Fatalf("menu structure counts = details:%d summary:%d nav:%d, body = %s", strings.Count(body, `<details class="site-menu">`), strings.Count(body, `<summary class="menu-button">菜单</summary>`), strings.Count(body, `<nav class="site-nav" aria-label="主要导航">`), body)
			}
			if strings.Count(body, `<header class="site-header">`) != 1 || strings.Count(body, `<div class="container site-header__inner">`) != 1 || strings.Count(body, `<a class="site-brand" href="/">MyWebsite</a>`) != 1 {
				t.Fatalf("header shell structure missing: %s", body)
			}
			if strings.Count(body, `<footer class="site-footer">`) != 1 || strings.Count(body, `<div class="container site-footer__inner">`) != 1 || !strings.Contains(body, `<span>© 2026 MyWebsite</span>`) {
				t.Fatalf("footer shell structure missing: %s", body)
			}
			if strings.Contains(body, `class="brand"`) {
				t.Fatalf("legacy brand class remains: %s", body)
			}
			wantCurrent := 0
			if page.activeHref != "" {
				wantCurrent = 1
			}
			if strings.Count(body, `aria-current="page"`) != wantCurrent {
				t.Fatalf("aria-current count = %d, want %d: %s", strings.Count(body, `aria-current="page"`), wantCurrent, body)
			}
			if strings.Contains(body, "<script") {
				t.Fatalf("public page unexpectedly depends on a script: %s", body)
			}
			if page.activeHref != "" && !strings.Contains(body, `<a class="active" href="`+page.activeHref+`" aria-current="page">`) {
				t.Fatalf("active navigation link %q missing: %s", page.activeHref, body)
			}
		})
	}
}

func TestPublicStylesUseSystemThemeTokens(t *testing.T) {
	content, err := fs.ReadFile(publicassets.Files, "assets/styles.css")
	if err != nil {
		t.Fatalf("read public styles: %v", err)
	}
	css := string(content)
	if !strings.Contains(css, "color-scheme: light;") || !strings.Contains(css, "@media (prefers-color-scheme: dark)") {
		t.Fatalf("public styles do not declare system color schemes")
	}
	darkStart := strings.Index(css, "@media (prefers-color-scheme: dark)")
	darkCSS := css[darkStart:]
	for _, token := range []string{
		"color-scheme: dark;",
		"--bg: #151513;",
		"--surface: #1d1d1a;",
		"--surface-muted: #292925;",
		"--text: #f1f1eb;",
		"--text-muted: #aaa9a0;",
		"--border: #42423c;",
		"--border-strong: #f1f1eb;",
		"--inverse: #151513;",
		"--focus: #ffffff;",
		"--code-bg: #090908;",
		"--code-text: #f5f5ef;",
	} {
		if !strings.Contains(darkCSS, token) {
			t.Errorf("dark theme token %q missing", token)
		}
	}
	if strings.Contains(css, "data-theme") || strings.Contains(css, "localStorage") {
		t.Fatal("public styles must not add manual theme switching")
	}
	for _, rule := range []string{
		".site-menu > .site-nav {\n  display: flex;",
		".menu-button::-webkit-details-marker",
		".menu-button::marker",
		".menu-button:focus-visible",
		"@media (max-width: 640px)",
		".menu-button {\n    display: flex;",
		".site-menu[open] > .site-nav {\n    display: flex;",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("mobile menu CSS rule %q missing", rule)
		}
	}
	for _, rule := range []string{
		".site-header {",
		".site-header__inner",
		".site-brand",
		".site-nav",
		".site-footer {",
		".site-footer__inner",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("public shell CSS rule %q missing", rule)
		}
	}
	for _, rule := range []string{
		".page-heading {\n  padding: clamp(3rem, 6vw, 5rem) 0 2.5rem;",
		".page-heading h1 {",
		".page-heading p {",
		".article-list {",
		".article-list__item {",
		".pagination [aria-current=\"page\"]",
		".section[aria-label=\"文章列表\"] .empty-state {",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("article list CSS rule %q missing", rule)
		}
	}
	if strings.Contains(css, ".article-list-item") {
		t.Fatal("public styles still use the legacy article list item selector")
	}
	if strings.Contains(css, "\nnav {") || strings.Contains(css, "\nnav a") || strings.Contains(css, "\n.brand {") {
		t.Fatal("public shell still uses legacy global nav or brand selectors")
	}
}

func TestPublicPagesExposeMetadataAndErrorSemantics(t *testing.T) {
	const internalError = "secret storage connection string"
	pages := []struct {
		name        string
		handler     http.Handler
		target      string
		status      int
		title       string
		description string
		ogTitle     string
		ogDesc      string
	}{
		{
			name:        "home",
			handler:     newPublicHandler(t),
			target:      "/",
			status:      http.StatusOK,
			title:       "首页 — MyWebsite",
			description: "一个简洁、安静的个人网站首页。",
		},
		{
			name:        "articles",
			handler:     newPublicHandler(t),
			target:      "/articles",
			status:      http.StatusOK,
			title:       "文章 — MyWebsite",
			description: "记录思考、实践与长期积累的文章。",
		},
		{
			name: "article one",
			handler: newPublicHandler(t, &fakeArticleReader{article: article.Article{
				Title:       "第一篇独立文章",
				Status:      article.StatusPublished,
				BodyHTML:    stringPointer("<p>第一篇正文</p>"),
				PreviewText: stringPointer("第一篇独立摘要"),
			}}),
			target:      "/articles/" + testArticleULID,
			status:      http.StatusOK,
			title:       "第一篇独立文章 — MyWebsite",
			description: "第一篇独立摘要",
			ogTitle:     "第一篇独立文章",
			ogDesc:      "第一篇独立摘要",
		},
		{
			name: "article two",
			handler: newPublicHandler(t, &fakeArticleReader{article: article.Article{
				Title:       "第二篇独立文章",
				Status:      article.StatusPublished,
				BodyHTML:    stringPointer("<p>第二篇正文</p>"),
				PreviewText: stringPointer("第二篇独立摘要"),
			}}),
			target:      "/articles/" + testArticleULID,
			status:      http.StatusOK,
			title:       "第二篇独立文章 — MyWebsite",
			description: "第二篇独立摘要",
			ogTitle:     "第二篇独立文章",
			ogDesc:      "第二篇独立摘要",
		},
		{
			name:        "projects",
			handler:     newPublicHandler(t),
			target:      "/projects",
			status:      http.StatusOK,
			title:       "项目 — MyWebsite",
			description: "正在做过、正在做和想要继续做的项目。",
		},
		{
			name:        "about",
			handler:     newPublicHandler(t),
			target:      "/about",
			status:      http.StatusOK,
			title:       "关于 — MyWebsite",
			description: "关于我、我的工作方式，以及这个网站。",
		},
		{
			name:        "404",
			handler:     newPublicHandler(t),
			target:      "/missing",
			status:      http.StatusNotFound,
			title:       "页面不存在 — MyWebsite",
			description: "你访问的页面不存在或已经移除。",
		},
		{
			name:        "500",
			handler:     newPublicHandler(t, &fakeArticleReader{listErr: errors.New(internalError)}),
			target:      "/",
			status:      http.StatusInternalServerError,
			title:       "暂时无法加载 — MyWebsite",
			description: "服务遇到了临时问题，请稍后重试。",
		},
	}

	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			response := request(t, page.handler, page.target)
			body := response.Body.String()
			if response.Code != page.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, page.status, body)
			}
			for _, want := range []string{
				"<title>" + page.title + "</title>",
				`<meta name="description" content="` + page.description + `">`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("metadata missing %q in %s", want, body)
				}
			}
			if page.ogTitle != "" {
				for _, want := range []string{
					`<meta property="og:type" content="article">`,
					`<meta property="og:title" content="` + page.ogTitle + `">`,
					`<meta property="og:description" content="` + page.ogDesc + `">`,
				} {
					if !strings.Contains(body, want) {
						t.Errorf("article sharing metadata missing %q in %s", want, body)
					}
				}
			}
			if page.name == "404" {
				for _, want := range []string{"页面不存在", `href="/"`, `href="/articles"`} {
					if !strings.Contains(body, want) {
						t.Errorf("404 semantic affordance missing %q in %s", want, body)
					}
				}
			}
			if page.name == "500" {
				for _, want := range []string{"页面暂时无法加载", "重试", `href="/"`} {
					if !strings.Contains(body, want) {
						t.Errorf("500 semantic affordance missing %q in %s", want, body)
					}
				}
				if strings.Contains(body, internalError) {
					t.Errorf("internal error leaked into response: %s", body)
				}
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
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "第 2 页文章") || !strings.Contains(body, "2026-09-19") {
		t.Fatalf("page 2 response = %d %s", response.Code, body)
	}
	if strings.Contains(body, `class="page-shell"`) || strings.Count(body, `<header class="page-heading">`) != 1 || strings.Count(body, `<div class="container">`) != 2 {
		t.Fatalf("article heading structure is incorrect: %s", body)
	}
	if strings.Count(body, `<section class="section" aria-label="文章列表">`) != 1 || strings.Count(body, `<div class="article-list">`) != 1 || strings.Count(body, `class="article-list__item"`) != 1 {
		t.Fatalf("article list structure is incorrect: %s", body)
	}
	if strings.Count(body, `<nav class="pagination" aria-label="文章分页">`) != 1 || !strings.Contains(body, `href="/articles?page=1" aria-label="上一页"`) || !strings.Contains(body, `<span aria-current="page">第 2 / 2 页</span>`) || strings.Contains(body, `aria-label="下一页"`) {
		t.Fatalf("pagination semantics are incorrect: %s", body)
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
