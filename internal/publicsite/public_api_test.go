package publicsite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/project"
)

type publicAPITestArticleReader struct {
	items      []article.PublishedArticle
	details    map[string]article.Article
	total      int64
	countErr   error
	listErr    error
	detailErr  error
	listLimit  int32
	listOffset int32
	listCalls  int
	detailID   string
}

func (reader *publicAPITestArticleReader) ListPublished(_ context.Context, limit, offset int32) ([]article.PublishedArticle, error) {
	reader.listCalls++
	reader.listLimit = limit
	reader.listOffset = offset
	if reader.listErr != nil {
		return nil, reader.listErr
	}
	if offset < 0 || limit < 0 || int(offset) >= len(reader.items) {
		return []article.PublishedArticle{}, nil
	}
	end := int(offset + limit)
	if end > len(reader.items) {
		end = len(reader.items)
	}
	return append([]article.PublishedArticle{}, reader.items[int(offset):end]...), nil
}

func (reader *publicAPITestArticleReader) CountPublished(context.Context) (int64, error) {
	if reader.countErr != nil {
		return 0, reader.countErr
	}
	return reader.total, nil
}

func (reader *publicAPITestArticleReader) GetPublishedByULID(_ context.Context, id string) (article.Article, error) {
	reader.detailID = id
	if reader.detailErr != nil {
		return article.Article{}, reader.detailErr
	}
	item, ok := reader.details[id]
	if !ok {
		return article.Article{}, sql.ErrNoRows
	}
	return item, nil
}

func TestPublicArticlesAPIUsesExistingPaginationAndReturns404ForInvalidPages(t *testing.T) {
	publishedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	reader := &publicAPITestArticleReader{total: 11}
	for index := 0; index < 11; index++ {
		title := "文章 " + strconv.Itoa(index+1)
		preview := "摘要 " + strconv.Itoa(index+1)
		reader.items = append(reader.items, article.PublishedArticle{
			Title:            title,
			PreviewText:      &preview,
			Status:           article.StatusPublished,
			FirstPublishedAt: &publishedAt,
		})
	}
	handler := newStage2Handler(t, reader, &fakeProjectReader{}, nil)
	response := request(t, handler, "/api/public/v1/articles?page=2")
	if response.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d: %s", response.Code, response.Body.String())
	}
	assertHomeAPIHeaders(t, response.Header())
	assertJSONKeys(t, response.Body.Bytes(), "site", "year", "page", "articles", "pagination")
	assertJSONListItemKeys(t, response.Body.Bytes(), "articles", "title", "preview", "published", "date_time", "url")
	var body struct {
		Site       map[string]json.RawMessage `json:"site"`
		Year       int                        `json:"year"`
		Page       map[string]json.RawMessage `json:"page"`
		Articles   []homeAPIArticleTest       `json:"articles"`
		Pagination homeAPIPagingTest          `json:"pagination"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode page 2 response: %v", err)
	}
	assertJSONKeys(t, mustRaw(t, body.Site), "name", "description", "eyebrow")
	assertJSONKeys(t, mustRaw(t, body.Page), "title", "description", "heading", "eyebrow")
	if body.Year != 2026 || len(body.Articles) != 1 || body.Articles[0].Title != "文章 11" || body.Articles[0].URL != "/articles" {
		t.Fatalf("page 2 projection = year %d, articles %+v", body.Year, body.Articles)
	}
	if reader.listLimit != 10 || reader.listOffset != 10 || reader.listCalls != 1 {
		t.Fatalf("article paging calls = limit %d offset %d calls %d", reader.listLimit, reader.listOffset, reader.listCalls)
	}
	if body.Pagination != (homeAPIPagingTest{Page: 2, TotalPages: 2, PreviousPage: 1, NextPage: 3, HasPrevious: true, HasNext: false}) {
		t.Fatalf("pagination = %+v", body.Pagination)
	}
	for _, target := range []string{"/api/public/v1/articles?page=3", "/api/public/v1/articles?page=01", "/api/public/v1/articles?page=2&other=x"} {
		missing := request(t, handler, target)
		assertPublicAPIFailure(t, missing, http.StatusNotFound, "not_found")
	}
}

func TestPublicArticlesAPIEmptyPageAndDependencyErrors(t *testing.T) {
	emptyReader := &publicAPITestArticleReader{}
	empty := request(t, newStage2Handler(t, emptyReader, &fakeProjectReader{}, nil), "/api/public/v1/articles")
	if empty.Code != http.StatusOK {
		t.Fatalf("empty page status = %d: %s", empty.Code, empty.Body.String())
	}
	var emptyBody struct {
		Articles   []json.RawMessage `json:"articles"`
		Pagination homeAPIPagingTest `json:"pagination"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyBody); err != nil {
		t.Fatalf("decode empty page: %v", err)
	}
	if emptyBody.Articles == nil || len(emptyBody.Articles) != 0 || emptyBody.Pagination != (homeAPIPagingTest{Page: 1, TotalPages: 1, NextPage: 2}) {
		t.Fatalf("empty page projection = articles %v, paging %+v", emptyBody.Articles, emptyBody.Pagination)
	}
	failedReader := &publicAPITestArticleReader{countErr: errors.New("db secret")}
	failed := request(t, newStage2Handler(t, failedReader, &fakeProjectReader{}, nil), "/api/public/v1/articles")
	assertPublicAPIFailure(t, failed, http.StatusServiceUnavailable, "service_unavailable")
	if strings.Contains(failed.Body.String(), "secret") {
		t.Fatalf("error leaked dependency detail: %s", failed.Body.String())
	}
}

func TestPublicArticleDetailAPIOnlyProjectsPublishedFieldsAndTOC(t *testing.T) {
	id := testArticleULID
	markdown := "# private markdown"
	bodyHTML := `<h2 id="heading">Heading</h2><p><a href="/assets/tazmi-avatar.jpg">example</a></p>`
	preview := "公开摘要"
	publishedAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	reader := &publicAPITestArticleReader{details: make(map[string]article.Article)}
	reader.details[id] = article.Article{
		ID:               77,
		PublicULID:       &id,
		Title:            "文章标题",
		BodyMarkdown:     &markdown,
		BodyHTML:         &bodyHTML,
		TOCJSON:          json.RawMessage(`{"items":[{"id":"heading","text":"Heading","level":2}]}`),
		PreviewText:      &preview,
		Status:           article.StatusPublished,
		FirstPublishedAt: &publishedAt,
		Version:          9,
		CreatedAt:        publishedAt,
	}
	handler := newStage2Handler(t, reader, &fakeProjectReader{}, nil)
	response := request(t, handler, "/api/public/v1/articles/"+id)
	if response.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", response.Code, response.Body.String())
	}
	assertHomeAPIHeaders(t, response.Header())
	assertJSONKeys(t, response.Body.Bytes(), "site", "year", "page", "article")
	var root map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &root); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	assertJSONKeys(t, root["page"], "title", "description", "heading", "eyebrow")
	assertJSONKeys(t, root["article"], "title", "url", "preview", "published", "date_time", "body_html", "toc")
	assertJSONListItemKeys(t, root["article"], "toc", "id", "text", "level")
	var detail struct {
		Title     string                 `json:"title"`
		URL       string                 `json:"url"`
		Preview   string                 `json:"preview"`
		Published string                 `json:"published"`
		DateTime  string                 `json:"date_time"`
		BodyHTML  string                 `json:"body_html"`
		TOC       []publicAPITOCItemTest `json:"toc"`
	}
	if err := json.Unmarshal(root["article"], &detail); err != nil {
		t.Fatalf("decode article detail: %v", err)
	}
	if reader.detailID != id || detail.Title != "文章标题" || detail.URL != "/articles/"+id || detail.Preview != preview || detail.Published != "2026-07-08" || detail.DateTime != detail.Published || detail.BodyHTML != bodyHTML {
		t.Fatalf("article detail projection = %+v, fetched ID %q", detail, reader.detailID)
	}
	if len(detail.TOC) != 1 || detail.TOC[0] != (publicAPITOCItemTest{ID: "heading", Text: "Heading", Level: 2}) {
		t.Fatalf("TOC projection = %+v", detail.TOC)
	}
	for _, privateField := range []string{`"version"`, `"status"`, `"body_markdown"`, `"markdown"`, `"created_at"`} {
		if strings.Contains(response.Body.String(), privateField) {
			t.Fatalf("detail leaked %s: %s", privateField, response.Body.String())
		}
	}

	reader.details[id] = article.Article{Title: "withdrawn", Status: article.StatusDraft}
	withdrawn := request(t, handler, "/api/public/v1/articles/"+id)
	assertPublicAPIFailure(t, withdrawn, http.StatusNotFound, "not_found")
	malformed := request(t, handler, "/api/public/v1/articles/not-a-ulid")
	assertPublicAPIFailure(t, malformed, http.StatusNotFound, "not_found")
	reader.detailErr = sql.ErrNoRows
	notFound := request(t, handler, "/api/public/v1/articles/"+id)
	assertPublicAPIFailure(t, notFound, http.StatusNotFound, "not_found")
	reader.detailErr = errors.New("private storage error")
	unavailable := request(t, handler, "/api/public/v1/articles/"+id)
	assertPublicAPIFailure(t, unavailable, http.StatusServiceUnavailable, "service_unavailable")
	if strings.Contains(unavailable.Body.String(), "private") {
		t.Fatalf("detail error leaked: %s", unavailable.Body.String())
	}
}

func TestPublicProjectsAndAboutAPIMatchPublicPageOrderAndCopy(t *testing.T) {
	githubURL := "https://github.com/Tazmi-Asdes/MyWebsite"
	projects := &fakeProjectReader{
		projects: []project.Project{
			{Name: "项目一", Status: project.StatusPublic},
			{Name: "隐藏项目", Status: project.StatusHidden},
			{Name: "项目二", GitHubURL: &githubURL, Status: project.StatusPublic},
			{Name: "项目三", Status: project.StatusPublic},
			{Name: "项目四", Status: project.StatusPublic},
		},
		project: 4,
	}
	articles := &publicAPITestArticleReader{total: 12}
	handler := newStage2Handler(t, articles, projects, nil)
	projectResponse := request(t, handler, "/api/public/v1/projects")
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("projects status = %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	assertHomeAPIHeaders(t, projectResponse.Header())
	assertJSONKeys(t, projectResponse.Body.Bytes(), "site", "year", "page", "projects")
	var projectBody struct {
		Page     map[string]json.RawMessage `json:"page"`
		Projects []homeAPIProjectTest       `json:"projects"`
	}
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectBody); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	assertJSONKeys(t, mustRaw(t, projectBody.Page), "title", "description", "heading", "eyebrow")
	assertJSONListItemKeys(t, projectResponse.Body.Bytes(), "projects", "name", "image_url", "image_alt", "github_url", "url", "external")
	if len(projectBody.Projects) != 4 || projectBody.Projects[0].Name != "项目一" || projectBody.Projects[1].Name != "项目二" || projectBody.Projects[3].Name != "项目四" || projectBody.Projects[1].URL != githubURL || !projectBody.Projects[1].External || strings.Contains(projectResponse.Body.String(), "隐藏项目") {
		t.Fatalf("project projection = %+v", projectBody.Projects)
	}

	about := request(t, handler, "/api/public/v1/about")
	if about.Code != http.StatusOK {
		t.Fatalf("about status = %d: %s", about.Code, about.Body.String())
	}
	assertHomeAPIHeaders(t, about.Header())
	assertJSONKeys(t, about.Body.Bytes(), "site", "year", "page", "profile", "article_count", "project_count", "interests", "contacts")
	var aboutBody struct {
		Page         map[string]json.RawMessage `json:"page"`
		Profile      map[string]json.RawMessage `json:"profile"`
		ArticleCount int64                      `json:"article_count"`
		ProjectCount int64                      `json:"project_count"`
		Interests    []string                   `json:"interests"`
		Contacts     []publicAPIContactTest     `json:"contacts"`
	}
	if err := json.Unmarshal(about.Body.Bytes(), &aboutBody); err != nil {
		t.Fatalf("decode about response: %v", err)
	}
	assertJSONKeys(t, mustRaw(t, aboutBody.Page), "title", "description", "heading", "eyebrow")
	assertJSONKeys(t, mustRaw(t, aboutBody.Profile), "name", "avatar_url", "tagline")
	assertJSONListItemKeys(t, about.Body.Bytes(), "contacts", "label", "href", "external")
	if aboutBody.ArticleCount != 12 || aboutBody.ProjectCount != 4 || len(aboutBody.Interests) != 1 || aboutBody.Interests[0] != "耍起嘛,每天用ai整点有的没的好舒服啊" {
		t.Fatalf("about counts/interests = %d/%d %v", aboutBody.ArticleCount, aboutBody.ProjectCount, aboutBody.Interests)
	}
	if len(aboutBody.Contacts) != 2 || aboutBody.Contacts[0] != (publicAPIContactTest{Label: "2926238971@qq.com", Href: "mailto:2926238971@qq.com", External: false}) || aboutBody.Contacts[1] != (publicAPIContactTest{Label: "github.com/Tazmi-Asdes", Href: "https://github.com/Tazmi-Asdes", External: true}) {
		t.Fatalf("about contacts = %+v", aboutBody.Contacts)
	}
}

func assertPublicAPIFailure(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("error status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	assertHomeAPIHeaders(t, response.Header())
	assertJSONKeys(t, response.Body.Bytes(), "code", "message")
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode API error: %v", err)
	}
	if body.Code != code || body.Message == "" {
		t.Fatalf("error body = %+v, want code %q", body, code)
	}
}

func mustRaw(t *testing.T, value any) []byte {
	t.Helper()
	result, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw object: %v", err)
	}
	return result
}

func assertJSONListItemKeys(t *testing.T, raw []byte, listKey string, expected ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode JSON object for %s: %v", listKey, err)
	}
	list, ok := object[listKey]
	if !ok {
		t.Fatalf("JSON object has no %q list", listKey)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(list, &items); err != nil {
		t.Fatalf("decode JSON list %s: %v", listKey, err)
	}
	if len(items) == 0 {
		t.Fatalf("JSON list %s is empty", listKey)
	}
	assertJSONKeys(t, items[0], expected...)
}

type homeAPIArticleTest struct {
	Title     string `json:"title"`
	Preview   string `json:"preview"`
	Published string `json:"published"`
	DateTime  string `json:"date_time"`
	URL       string `json:"url"`
}

type homeAPIPagingTest struct {
	Page         int  `json:"page"`
	TotalPages   int  `json:"total_pages"`
	PreviousPage int  `json:"previous_page"`
	NextPage     int  `json:"next_page"`
	HasPrevious  bool `json:"has_previous"`
	HasNext      bool `json:"has_next"`
}

type publicAPITOCItemTest struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

type homeAPIProjectTest struct {
	Name      string `json:"name"`
	ImageURL  string `json:"image_url"`
	ImageAlt  string `json:"image_alt"`
	GitHubURL string `json:"github_url"`
	URL       string `json:"url"`
	External  bool   `json:"external"`
}

type publicAPIContactTest struct {
	Label    string `json:"label"`
	Href     string `json:"href"`
	External bool   `json:"external"`
}
