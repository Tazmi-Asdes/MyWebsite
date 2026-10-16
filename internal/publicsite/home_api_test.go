package publicsite_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/project"
	"mywebsite/internal/publicsite"
)

func TestPublicHomeAPIProjectsOnlyThePublicContract(t *testing.T) {
	publishedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	articles := &fakeArticleReader{items: []article.PublishedArticle{
		{Title: "文章一", PreviewText: stringPointer("摘要一"), PublicULID: stringPointer(testArticleULID), Status: article.StatusPublished, FirstPublishedAt: &publishedAt},
		{Title: "文章二", PreviewText: stringPointer("摘要二"), Status: article.StatusPublished, FirstPublishedAt: &publishedAt},
		{Title: "文章三", PreviewText: stringPointer("摘要三"), PublicULID: stringPointer("invalid"), Status: article.StatusPublished, FirstPublishedAt: &publishedAt},
		{Title: "不应返回的文章", Status: article.StatusPublished},
	}}
	githubURL := "https://github.com/tazmi/sample"
	projects := &fakeProjectReader{featured: []project.Project{
		{Name: "项目一", Status: project.StatusPublic},
		{Name: "项目二", GitHubURL: &githubURL, ImageURL: "/media/project-two", Status: project.StatusPublic},
		{Name: "项目三", Status: project.StatusPublic},
		{Name: "不应返回的项目", Status: project.StatusPublic},
	}}
	handler := newStage2Handler(t, articles, projects, nil)
	response := request(t, handler, "/api/public/v1/home")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	assertHomeAPIHeaders(t, response.Header())
	assertJSONKeys(t, response.Body.Bytes(), "site", "profile", "year", "articles", "projects")
	var rawBody map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &rawBody); err != nil {
		t.Fatalf("decode response fields: %v", err)
	}
	assertJSONKeys(t, rawBody["site"], "name", "description", "eyebrow")
	assertJSONKeys(t, rawBody["profile"], "name", "avatar_url", "tagline")
	var rawArticles []json.RawMessage
	if err := json.Unmarshal(rawBody["articles"], &rawArticles); err != nil {
		t.Fatalf("decode article fields: %v", err)
	}
	for _, item := range rawArticles {
		assertJSONKeys(t, item, "title", "preview", "published", "date_time", "url")
	}
	var rawProjects []json.RawMessage
	if err := json.Unmarshal(rawBody["projects"], &rawProjects); err != nil {
		t.Fatalf("decode project fields: %v", err)
	}
	for _, item := range rawProjects {
		assertJSONKeys(t, item, "name", "image_url", "image_alt", "github_url", "url", "external")
	}

	var body struct {
		Site struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Eyebrow     string `json:"eyebrow"`
		} `json:"site"`
		Profile struct {
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			Tagline   string `json:"tagline"`
		} `json:"profile"`
		Year     int `json:"year"`
		Articles []struct {
			Title     string `json:"title"`
			Preview   string `json:"preview"`
			Published string `json:"published"`
			DateTime  string `json:"date_time"`
			URL       string `json:"url"`
		} `json:"articles"`
		Projects []struct {
			Name      string `json:"name"`
			ImageURL  string `json:"image_url"`
			ImageAlt  string `json:"image_alt"`
			GitHubURL string `json:"github_url"`
			URL       string `json:"url"`
			External  bool   `json:"external"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Site.Name != "Tazmi-Website" || body.Site.Description != "Welcome here" || body.Site.Eyebrow != "PERSONAL SITE" {
		t.Fatalf("site projection = %+v", body.Site)
	}
	if body.Profile.Name != "Tazmi" || body.Profile.AvatarURL != "/assets/tazmi-avatar.jpg" || body.Profile.Tagline != "Kubernetes/CICD/Observability" || body.Year != 2026 {
		t.Fatalf("profile/year projection = %+v, year %d", body.Profile, body.Year)
	}
	if articles.listLimit != 3 || articles.listOffset != 0 || !articles.listCalled || projects.featuredCalls != 1 {
		t.Fatalf("reader calls: article limit=%d offset=%d called=%t, featured=%d", articles.listLimit, articles.listOffset, articles.listCalled, projects.featuredCalls)
	}
	if len(body.Articles) != 3 || body.Articles[0].Title != "文章一" || body.Articles[1].Title != "文章二" || body.Articles[2].Title != "文章三" {
		t.Fatalf("article order/limit = %+v", body.Articles)
	}
	if body.Articles[0].Preview != "摘要一" || body.Articles[0].Published != "2026-05-06" || body.Articles[0].DateTime != "2026-05-06" || body.Articles[0].URL != "/articles/"+testArticleULID {
		t.Fatalf("first article projection = %+v", body.Articles[0])
	}
	if body.Articles[1].URL != "/articles" || body.Articles[2].URL != "/articles" {
		t.Fatalf("fallback article urls = %q, %q", body.Articles[1].URL, body.Articles[2].URL)
	}
	if len(body.Projects) != 3 || body.Projects[0].Name != "项目一" || body.Projects[1].Name != "项目二" || body.Projects[2].Name != "项目三" {
		t.Fatalf("project order/limit = %+v", body.Projects)
	}
	if body.Projects[0].ImageURL != "/assets/default-project.svg" || body.Projects[0].ImageAlt != "" || body.Projects[0].GitHubURL != "" || body.Projects[0].URL != "/projects" || body.Projects[0].External {
		t.Fatalf("default project projection = %+v", body.Projects[0])
	}
	if body.Projects[1].ImageURL != "/media/project-two" || body.Projects[1].ImageAlt != "项目二" || body.Projects[1].GitHubURL != githubURL || body.Projects[1].URL != githubURL || !body.Projects[1].External {
		t.Fatalf("GitHub project projection = %+v", body.Projects[1])
	}
	for _, privateField := range []string{`"id"`, `"version"`, `"status"`, `"body_markdown"`, `"markdown"`, `"created_at"`, `"updated_at"`} {
		if strings.Contains(response.Body.String(), privateField) {
			t.Fatalf("response leaked private field %s: %s", privateField, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "不应返回") {
		t.Fatalf("response exceeded the three-item reader projection: %s", response.Body.String())
	}
}

func TestPublicHomeAPIReturnsNonNullEmptyArraysAndFiltersUnpublishedRows(t *testing.T) {
	emptyHandler := newStage2Handler(t, &fakeArticleReader{}, &fakeProjectReader{}, nil)
	emptyResponse := request(t, emptyHandler, "/api/public/v1/home")
	if emptyResponse.Code != http.StatusOK {
		t.Fatalf("empty response status = %d: %s", emptyResponse.Code, emptyResponse.Body.String())
	}
	var emptyBody map[string]json.RawMessage
	if err := json.Unmarshal(emptyResponse.Body.Bytes(), &emptyBody); err != nil {
		t.Fatalf("decode empty response: %v", err)
	}
	if string(emptyBody["articles"]) != "[]" || string(emptyBody["projects"]) != "[]" {
		t.Fatalf("empty arrays = articles %s, projects %s", emptyBody["articles"], emptyBody["projects"])
	}

	articles := &fakeArticleReader{items: []article.PublishedArticle{
		{Title: "草稿", Status: article.StatusDraft},
		{Title: "公开文章", Status: article.StatusPublished},
	}}
	projects := &fakeProjectReader{featured: []project.Project{
		{Name: "隐藏项目", Status: project.StatusHidden},
		{Name: "公开项目", Status: project.StatusPublic},
	}}
	filtered := request(t, newStage2Handler(t, articles, projects, nil), "/api/public/v1/home")
	if filtered.Code != http.StatusOK || strings.Contains(filtered.Body.String(), "草稿") || strings.Contains(filtered.Body.String(), "隐藏项目") || !strings.Contains(filtered.Body.String(), "公开文章") || !strings.Contains(filtered.Body.String(), "公开项目") {
		t.Fatalf("unpublished rows were not filtered: %d %s", filtered.Code, filtered.Body.String())
	}
}

func TestPublicHomeAPIDependencyErrorsAreFixedAndDoNotLeak(t *testing.T) {
	tests := []struct {
		name     string
		articles publicsite.ArticleReader
		projects publicsite.ProjectReader
	}{
		{name: "article", articles: &fakeArticleReader{listErr: errors.New("connection password=private")}, projects: &fakeProjectReader{}},
		{name: "project", articles: &fakeArticleReader{}, projects: &fakeProjectReader{featuredErr: errors.New("connection password=private")}},
		{name: "missing reader", articles: &fakeArticleReader{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, newStage2Handler(t, test.articles, test.projects, nil), "/api/public/v1/home")
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
			}
			assertHomeAPIHeaders(t, response.Header())
			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			assertJSONKeys(t, response.Body.Bytes(), "code", "message")
			if string(body["code"]) != `"service_unavailable"` || string(body["message"]) != `"Public home data is temporarily unavailable."` {
				t.Fatalf("error response = %s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatalf("dependency error leaked: %s", response.Body.String())
			}
		})
	}
}

func TestPublicHomeAPIRoutesGETHEADAndMethodNotAllowed(t *testing.T) {
	handler := newStage2Handler(t, &fakeArticleReader{}, &fakeProjectReader{}, nil)
	getResponse := request(t, handler, "/api/public/v1/home")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", getResponse.Code, getResponse.Body.String())
	}
	headResponse := httptest.NewRecorder()
	handler.ServeHTTP(headResponse, httptest.NewRequest(http.MethodHead, "/api/public/v1/home", nil))
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 || headResponse.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD response = %d, body %q, content length %q", headResponse.Code, headResponse.Body.String(), headResponse.Header().Get("Content-Length"))
	}
	assertHomeAPIHeaders(t, headResponse.Header())
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/api/public/v1/home", nil))
	if postResponse.Code != http.StatusMethodNotAllowed || postResponse.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST response = %d Allow %q: %s", postResponse.Code, postResponse.Header().Get("Allow"), postResponse.Body.String())
	}
	assertHomeAPIHeaders(t, postResponse.Header())
	assertJSONKeys(t, postResponse.Body.Bytes(), "code", "message")
}

func assertHomeAPIHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	if got := headers.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := headers.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func assertJSONKeys(t *testing.T, raw []byte, expected ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode JSON object keys: %v", err)
	}
	if len(object) != len(expected) {
		t.Fatalf("JSON keys = %v, want %v", keysOf(object), expected)
	}
	for _, key := range expected {
		if _, ok := object[key]; !ok {
			t.Fatalf("JSON keys = %v, missing %q", keysOf(object), key)
		}
	}
}

func keysOf(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return keys
}
