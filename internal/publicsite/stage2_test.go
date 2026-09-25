package publicsite_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"mywebsite/internal/article"
	"mywebsite/internal/media"
	"mywebsite/internal/project"
	"mywebsite/internal/publicsite"
	publicassets "mywebsite/web/public"
)

type fakeProjectReader struct {
	featured      []project.Project
	projects      []project.Project
	project       int64
	listErr       error
	featuredErr   error
	countErr      error
	listCalls     int
	featuredCalls int
}

func (f *fakeProjectReader) ListPublicProjects(context.Context) ([]project.Project, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.projects, nil
}

func (f *fakeProjectReader) ListFeatured(context.Context) ([]project.Project, error) {
	f.featuredCalls++
	if f.featuredErr != nil {
		return nil, f.featuredErr
	}
	return f.featured, nil
}

func (f *fakeProjectReader) CountPublicProjects(context.Context) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.project, nil
}

type fakeMediaReader struct {
	result media.OpenResult
	err    error
	id     string
	admin  bool
}

func (f *fakeMediaReader) Open(_ context.Context, id string, authenticatedAdmin bool) (media.OpenResult, error) {
	f.id = id
	f.admin = authenticatedAdmin
	if f.err != nil {
		return media.OpenResult{}, f.err
	}
	return f.result, nil
}

func newStage2Handler(t *testing.T, articleReader publicsite.ArticleReader, projects publicsite.ProjectReader, mediaReader publicsite.MediaReader) http.Handler {
	t.Helper()
	handler, err := publicsite.NewHandlerWithStage2(publicassets.Files, fixedClock{}, publicsite.Stage2Options{
		ArticleReader: articleReader,
		ProjectReader: projects,
		MediaReader:   mediaReader,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithStage2() error = %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func TestStage2HomeProjectsAndAboutUseIndependentProjections(t *testing.T) {
	article := &fakeArticleReader{
		items: articleItemForTestList{
			{title: "第一篇", preview: "摘要一"},
			{title: "第二篇", preview: "摘要二"},
			{title: "第三篇", preview: "摘要三"},
			{title: "不应展示", preview: "摘要四"},
		}.published(),
		total: 4,
	}
	projects := &fakeProjectReader{
		featured: []project.Project{
			{Name: "精选一", ImageURL: "/media/one"},
			{Name: "精选二"},
			{Name: "精选三", GitHubURL: stringPointer("https://github.com/example/three")},
			{Name: "不应展示"},
		},
		projects: []project.Project{
			{Name: "项目二"},
			{Name: "项目一", ImageURL: "/media/custom", GitHubURL: stringPointer("https://github.com/example/one")},
		},
		project: 7,
	}
	handler := newStage2Handler(t, article.reader(), projects, nil)

	home := request(t, handler, "/")
	body := home.Body.String()
	if home.Code != http.StatusOK || !strings.Contains(body, "第一篇") || !strings.Contains(body, "精选一") || strings.Contains(body, "不应展示") {
		t.Fatalf("home response = %d %s", home.Code, body)
	}
	if !strings.Contains(body, `<section class="hero">`) || !strings.Contains(body, `<div class="container hero__content">`) {
		t.Fatalf("home hero structure missing: %s", body)
	}
	if strings.Count(body, "查看全部") != 2 {
		t.Fatalf("home view-all link count = %d, body = %s", strings.Count(body, "查看全部"), body)
	}
	if article.listLimit != 3 || article.listOffset != 0 || projects.featuredCalls != 1 {
		t.Fatalf("home projection calls = article(%d,%d), featured(%d)", article.listLimit, article.listOffset, projects.featuredCalls)
	}

	allProjects := request(t, handler, "/projects")
	allBody := allProjects.Body.String()
	projectTwoIndex := strings.Index(allBody, `<h2>项目二</h2>`)
	projectOneIndex := strings.Index(allBody, `<h2>项目一</h2>`)
	if allProjects.Code != http.StatusOK || projectTwoIndex < 0 || projectOneIndex < 0 || projectTwoIndex > projectOneIndex {
		t.Fatalf("projects order = %d %s", allProjects.Code, allBody)
	}
	for marker, want := range map[string]int{
		`<header class="page-heading">`:               1,
		`<div class="container">`:                     2,
		`<section class="section" aria-label="项目列表">`: 1,
		`<div class="grid grid--three project-grid">`: 1,
		`<article class="card project-card">`:         2,
		`<div class="project-card__media">`:           2,
		`<div class="project-card__footer">`:          2,
	} {
		if got := strings.Count(allBody, marker); got != want {
			t.Errorf("projects %q count = %d, want %d: %s", marker, got, want, allBody)
		}
	}
	if !strings.Contains(allBody, `src="/media/custom" alt="项目一"`) || !strings.Contains(allBody, `src="/assets/default-project.svg" alt=""`) {
		t.Fatalf("project image semantics missing: %s", allBody)
	}
	if strings.Count(allBody, `target="_blank" rel="noopener noreferrer">GitHub`) != 1 || !strings.Contains(allBody, `href="https://github.com/example/one"`) {
		t.Fatalf("github attributes missing: %s", allBody)
	}

	about := request(t, handler, "/about")
	aboutBody := about.Body.String()
	if about.Code != http.StatusOK || !strings.Contains(aboutBody, ">4</strong><span>已发布文章</span>") || !strings.Contains(aboutBody, ">7</strong><span>公开项目</span>") {
		t.Fatalf("about response = %d %s", about.Code, aboutBody)
	}
}

func TestStage2AboutAlwaysRendersProfileStatsInterestsAndContact(t *testing.T) {
	assertAbout := func(t *testing.T, body string, articleCount, projectCount string) {
		t.Helper()
		for _, marker := range []string{
			`<main id="main-content" class="container">`,
			`<div class="about-layout">`,
			`<header class="about-profile">`,
			`<img class="avatar" src="/assets/default-avatar.svg" alt="Tazmi的默认头像">`,
			`<section class="about-block" aria-labelledby="stats-heading">`,
			`<div class="stats">`,
			`<section class="about-block" aria-labelledby="interests-heading">`,
			`<section class="about-block" aria-labelledby="contact-heading">`,
			`<div class="contact-list">`,
		} {
			if !strings.Contains(body, marker) {
				t.Errorf("about structure missing %q: %s", marker, body)
			}
		}
		if strings.Count(body, `<div class="stat">`) != 2 || strings.Count(body, `<section class="about-block"`) != 3 {
			t.Errorf("about stats/block count = %d/%d, body = %s", strings.Count(body, `<div class="stat">`), strings.Count(body, `<section class="about-block"`), body)
		}
		if !strings.Contains(body, `<div class="stat"><strong>`+articleCount+`</strong><span>已发布文章</span></div>`) || !strings.Contains(body, `<div class="stat"><strong>`+projectCount+`</strong><span>公开项目</span></div>`) {
			t.Errorf("about counts missing %q/%q: %s", articleCount, projectCount, body)
		}
		if strings.Contains(body, "个人介绍正在准备中") || strings.Contains(body, `<p class="eyebrow">`) || strings.Contains(body, `class="empty-state"`) {
			t.Errorf("about page contains the removed fallback profile state: %s", body)
		}
		if !strings.Contains(body, `href="mailto:2926238971@qq.com"`) || !strings.Contains(body, `href="https://github.com/Tazmi-Asdes" target="_blank" rel="noopener noreferrer"`) {
			t.Errorf("about contact links missing: %s", body)
		}
		for _, marker := range []string{
			`<header class="about-profile">`,
			`<section class="about-block" aria-labelledby="stats-heading">`,
			`<section class="about-block" aria-labelledby="interests-heading">`,
			`<section class="about-block" aria-labelledby="contact-heading">`,
		} {
			if index := strings.Index(body, marker); index < 0 {
				t.Errorf("about order marker missing %q", marker)
			}
		}
		profileIndex := strings.Index(body, `<header class="about-profile">`)
		statsIndex := strings.Index(body, `<section class="about-block" aria-labelledby="stats-heading">`)
		interestsIndex := strings.Index(body, `<section class="about-block" aria-labelledby="interests-heading">`)
		contactIndex := strings.Index(body, `<section class="about-block" aria-labelledby="contact-heading">`)
		if !(profileIndex < statsIndex && statsIndex < interestsIndex && interestsIndex < contactIndex) {
			t.Errorf("about content order is incorrect: %d, %d, %d, %d", profileIndex, statsIndex, interestsIndex, contactIndex)
		}
	}

	t.Run("zero counts without readers", func(t *testing.T) {
		response := request(t, newStage2Handler(t, nil, nil, nil), "/about")
		if response.Code != http.StatusOK {
			t.Fatalf("about status = %d", response.Code)
		}
		assertAbout(t, response.Body.String(), "0", "0")
	})

	t.Run("configured counts", func(t *testing.T) {
		articles := &fakeArticleReader{total: 4}
		projects := &fakeProjectReader{project: 7}
		response := request(t, newStage2Handler(t, articles, projects, nil), "/about")
		if response.Code != http.StatusOK {
			t.Fatalf("about status = %d", response.Code)
		}
		assertAbout(t, response.Body.String(), "4", "7")
	})

	for _, test := range []struct {
		name     string
		articles publicsite.ArticleReader
		projects publicsite.ProjectReader
		secret   string
	}{
		{name: "article count error", articles: &fakeArticleReader{countErr: errors.New("article count secret")}, secret: "article count secret"},
		{name: "project count error", projects: &fakeProjectReader{countErr: errors.New("project count secret")}, secret: "project count secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, newStage2Handler(t, test.articles, test.projects, nil), "/about")
			if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), test.secret) {
				t.Fatalf("about error response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestStage2EmptyPagesAndDependencyErrors(t *testing.T) {
	empty := newStage2Handler(t, &fakeArticleReader{}, &fakeProjectReader{}, nil)
	home := request(t, empty, "/")
	homeBody := home.Body.String()
	if home.Code != http.StatusOK || strings.Count(homeBody, "查看全部") != 0 || !strings.Contains(homeBody, "暂时没有公开文章") || !strings.Contains(homeBody, "暂时没有公开项目") || strings.Count(homeBody, `<div class="empty-state" aria-live="polite">`) != 2 {
		t.Fatalf("empty home response = %d %s", home.Code, homeBody)
	}
	for _, target := range []string{"/", "/projects"} {
		response := request(t, empty, target)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "没有公开") {
			t.Fatalf("empty %s response = %d %s", target, response.Code, response.Body.String())
		}
	}
	projectPage := request(t, empty, "/projects")
	projectBody := projectPage.Body.String()
	if strings.Count(projectBody, `<section class="section" aria-label="项目列表">`) != 1 || strings.Count(projectBody, `<div class="container">`) != 2 || strings.Count(projectBody, `<div class="empty-state" aria-live="polite">`) != 1 || strings.Count(projectBody, `<h2>暂时还没有公开项目</h2>`) != 1 {
		t.Fatalf("empty projects structure is incorrect: %s", projectBody)
	}
	if strings.Contains(projectBody, `class="project-grid"`) || strings.Contains(projectBody, `class="project-card"`) {
		t.Fatalf("empty projects page unexpectedly contains project cards: %s", projectBody)
	}
	broken := newStage2Handler(t, &fakeArticleReader{listErr: errors.New("down")}, nil, nil)
	if response := request(t, broken, "/"); response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "down") {
		t.Fatalf("dependency error response = %d %s", response.Code, response.Body.String())
	}
}

func TestStage2ProjectPageStylesUseV2Selectors(t *testing.T) {
	content, err := fs.ReadFile(publicassets.Files, "assets/styles.css")
	if err != nil {
		t.Fatalf("read public styles: %v", err)
	}
	css := string(content)
	for _, rule := range []string{
		".project-card {",
		".project-card__media {\n  aspect-ratio: 16 / 9;\n  display: grid;",
		".project-card__media img {\n  display: block;",
		"object-fit: contain;",
		".project-card__footer {",
		".section[aria-label=\"项目列表\"] .empty-state {",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("project page CSS rule %q missing", rule)
		}
	}
}

func TestStage2MediaServesPublicImageAndConditionalRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asset.png")
	body := []byte("png test bytes")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	id := ulid.MustParseStrict(testArticleULID)
	mediaReader := &fakeMediaReader{result: media.OpenResult{
		Path:            path,
		StoredMediaType: "image/png",
		SHA256:          digest[:],
		CreatedAt:       time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC),
	}}
	handler := newStage2Handler(t, nil, nil, mediaReader)

	req := httptest.NewRequest(http.MethodGet, "/media/"+id.String(), nil)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, req)
	if first.Code != http.StatusOK || first.Header().Get("Content-Type") != "image/png" || first.Header().Get("Content-Length") != "14" || first.Body.String() != string(body) {
		t.Fatalf("media response = %d headers=%v body=%q", first.Code, first.Header(), first.Body.String())
	}
	if first.Header().Get("ETag") != `"`+hexDigest(digest[:])+`"` || first.Header().Get("Cache-Control") != "no-cache" || first.Header().Get("Last-Modified") == "" {
		t.Fatalf("media headers = %v", first.Header())
	}
	if mediaReader.id != id.String() || mediaReader.admin {
		t.Fatalf("media reader args = id %q admin %t", mediaReader.id, mediaReader.admin)
	}

	conditional := httptest.NewRequest(http.MethodGet, "/media/"+id.String(), nil)
	conditional.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, conditional)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional media response = %d body=%q", second.Code, second.Body.String())
	}

	for _, target := range []string{"/media/nope", "/media/01ARZ3NDEKTSV4RRFFQ69G5FAS/extra"} {
		if response := request(t, handler, target); response.Code != http.StatusNotFound {
			t.Errorf("invalid media %s status = %d", target, response.Code)
		}
	}
}

func TestStage2MediaMapsNotFoundAndStorageErrors(t *testing.T) {
	id := testArticleULID
	for name, err := range map[string]error{
		"missing": media.ErrNotFound,
		"storage": errors.New("storage down"),
	} {
		t.Run(name, func(t *testing.T) {
			handler := newStage2Handler(t, nil, nil, &fakeMediaReader{err: err})
			response := request(t, handler, "/media/"+id)
			want := http.StatusNotFound
			if name == "storage" {
				want = http.StatusInternalServerError
			}
			if response.Code != want {
				t.Fatalf("status = %d, want %d", response.Code, want)
			}
		})
	}
}

func hexDigest(value []byte) string {
	const hex = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, value := range value {
		result[index*2] = hex[value>>4]
		result[index*2+1] = hex[value&0x0f]
	}
	return string(result)
}

type articleItemForTest struct {
	title   string
	preview string
}

func (item articleItemForTest) published() []article.PublishedArticle {
	return []article.PublishedArticle{{Title: item.title, PreviewText: stringPointer(item.preview), Status: article.StatusPublished}}
}

type articleItemForTestList []articleItemForTest

func (items articleItemForTestList) published() []article.PublishedArticle {
	result := make([]article.PublishedArticle, 0, len(items))
	for _, item := range items {
		result = append(result, item.published()...)
	}
	return result
}

func (f *fakeArticleReader) reader() publicsite.ArticleReader { return f }
