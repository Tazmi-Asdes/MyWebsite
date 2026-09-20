package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/auth"
	"mywebsite/internal/media"
	"mywebsite/internal/project"
)

type fixedClock struct {
	value time.Time
}

type readinessStub struct {
	err error
}

type handlerAuthStub struct{}

func (handlerAuthStub) Login(context.Context, string, string) (auth.LoginResult, error) {
	return auth.LoginResult{}, auth.ErrInvalidCredentials
}

func (handlerAuthStub) Authenticate(context.Context, string) (auth.Session, error) {
	return auth.Session{}, auth.ErrInvalidSession
}

func (handlerAuthStub) AuthenticateForReauth(ctx context.Context, token string) (auth.Session, error) {
	return handlerAuthStub{}.Authenticate(ctx, token)
}

func (handlerAuthStub) ValidateCSRF(any, string) error { return auth.ErrCSRFInvalid }

func (handlerAuthStub) Logout(context.Context, uint64) error { return nil }

func (handlerAuthStub) Reauthenticate(context.Context, auth.Session, string) (auth.LoginResult, error) {
	return auth.LoginResult{}, auth.ErrInvalidCredentials
}

type handlerArticleStub struct {
	item article.PublishedArticle
}

type authorizedHandlerAuthStub struct{}

func (authorizedHandlerAuthStub) Login(context.Context, string, string) (auth.LoginResult, error) {
	return auth.LoginResult{}, auth.ErrInvalidCredentials
}

func (authorizedHandlerAuthStub) Authenticate(context.Context, string) (auth.Session, error) {
	return auth.Session{ID: 1, AdminID: 1, Username: "admin"}, nil
}

func (authorizedHandlerAuthStub) AuthenticateForReauth(ctx context.Context, token string) (auth.Session, error) {
	return authorizedHandlerAuthStub{}.Authenticate(ctx, token)
}

func (authorizedHandlerAuthStub) ValidateCSRF(any, string) error { return nil }

func (authorizedHandlerAuthStub) Logout(context.Context, uint64) error { return nil }

func (authorizedHandlerAuthStub) Reauthenticate(context.Context, auth.Session, string) (auth.LoginResult, error) {
	return auth.LoginResult{}, auth.ErrInvalidCredentials
}

type handlerProjectStub struct {
	groupsCalls   int
	publicCalls   int
	featuredCalls int
}

func (stub *handlerProjectStub) ListGroups(context.Context) (project.ProjectGroups, error) {
	stub.groupsCalls++
	return project.ProjectGroups{Public: []project.Project{{ID: 1, Name: "管理项目"}}}, nil
}

func (stub *handlerProjectStub) Create(context.Context, project.CreateRequest) (project.Project, error) {
	return project.Project{}, nil
}

func (stub *handlerProjectStub) Get(context.Context, uint64) (project.Project, error) {
	return project.Project{}, nil
}

func (stub *handlerProjectStub) Update(context.Context, uint64, project.UpdateRequest) (project.Project, error) {
	return project.Project{}, nil
}

func (stub *handlerProjectStub) Publish(context.Context, uint64, project.PublishRequest) (project.Project, uint64, error) {
	return project.Project{}, 0, nil
}

func (stub *handlerProjectStub) Hide(context.Context, uint64, project.HideRequest) (uint64, error) {
	return 0, nil
}

func (stub *handlerProjectStub) Reorder(context.Context, project.OrderRequest) (uint64, error) {
	return 0, nil
}

func (stub *handlerProjectStub) ListPublicProjects(context.Context) ([]project.Project, error) {
	stub.publicCalls++
	return []project.Project{{ID: 2, Name: "公开项目"}}, nil
}

func (stub *handlerProjectStub) ListFeatured(context.Context) ([]project.Project, error) {
	stub.featuredCalls++
	return []project.Project{{ID: 2, Name: "精选项目"}}, nil
}

func (stub *handlerProjectStub) CountPublicProjects(context.Context) (int64, error) {
	return 1, nil
}

type handlerMediaStub struct {
	openCalls int
	lastID    string
	lastAdmin bool
}

func (stub *handlerMediaStub) Upload(context.Context, uint64, io.Reader) (media.Asset, error) {
	return media.Asset{}, nil
}

func (stub *handlerMediaStub) Open(_ context.Context, id string, admin bool) (media.OpenResult, error) {
	stub.openCalls++
	stub.lastID = id
	stub.lastAdmin = admin
	return media.OpenResult{}, media.ErrNotFound
}

func (stub handlerArticleStub) ListAdmin(context.Context, *article.Status, *string, int32, int32) ([]article.AdminArticleSummary, error) {
	return nil, nil
}

func (stub handlerArticleStub) CountAdmin(context.Context, *article.Status, *string) (int64, error) {
	return 0, nil
}

func (stub handlerArticleStub) CreateDraft(context.Context, article.CreateDraftRequest) (article.Article, error) {
	return article.Article{}, nil
}

func (stub handlerArticleStub) GetByID(context.Context, uint64) (article.Article, error) {
	return article.Article{}, sql.ErrNoRows
}

func (stub handlerArticleStub) Save(context.Context, uint64, article.SaveRequest) (article.Article, error) {
	return article.Article{}, nil
}

func (stub handlerArticleStub) Publish(context.Context, uint64, article.PublishRequest) (article.Article, error) {
	return article.Article{}, nil
}

func (stub handlerArticleStub) Withdraw(context.Context, uint64, article.WithdrawRequest) (article.Article, error) {
	return article.Article{}, nil
}

func (stub handlerArticleStub) ListPublished(context.Context, int32, int32) ([]article.PublishedArticle, error) {
	return []article.PublishedArticle{stub.item}, nil
}

func (stub handlerArticleStub) CountPublished(context.Context) (int64, error) { return 1, nil }

func (stub handlerArticleStub) GetPublishedByULID(context.Context, string) (article.Article, error) {
	return article.Article{Status: article.StatusPublished}, nil
}

func (r readinessStub) CheckReady(_ context.Context) error {
	return r.err
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
	if !strings.Contains(response.Body.String(), `"detail":"数据库或 Schema 尚未就绪"`) {
		t.Fatalf("body = %q, want generic readiness detail", response.Body.String())
	}
}

func TestReadyReturnsOKWhenDatabaseIsReady(t *testing.T) {
	handler, err := NewHandler(HandlerOptions{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)},
		Readiness: readinessStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/-/ready", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("body = %q, want ready response", got)
	}
}

func TestReadyReturnsUnavailableWhenDatabaseIsNotReady(t *testing.T) {
	handler, err := NewHandler(HandlerOptions{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)},
		Readiness: readinessStub{err: errors.New("internal database detail")},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/-/ready", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if strings.Contains(response.Body.String(), "internal database detail") {
		t.Fatalf("body = %q, must not expose readiness error", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"detail":"数据库或 Schema 尚未就绪"`) {
		t.Fatalf("body = %q, want generic readiness detail", response.Body.String())
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

func TestStage1RoutesAreMountedAlongsidePublicRoutes(t *testing.T) {
	publicULID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	articles := handlerArticleStub{item: article.PublishedArticle{Title: "已发布文章", PublicULID: &publicULID}}
	handler, err := NewHandler(HandlerOptions{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:         fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)},
		Auth:          handlerAuthStub{},
		Articles:      articles,
		PublicBaseURL: "https://example.test",
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	if apiResponse.Code != http.StatusUnauthorized || apiResponse.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("API response = %d %q, want admin API 401 problem response", apiResponse.Code, apiResponse.Header().Get("Content-Type"))
	}

	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, httptest.NewRequest(http.MethodGet, "/articles", nil))
	if publicResponse.Code != http.StatusOK || !strings.Contains(publicResponse.Body.String(), "已发布文章") {
		t.Fatalf("public response = %d %s, want rendered published article", publicResponse.Code, publicResponse.Body.String())
	}
}

func TestStage2RoutesAreMountedForSharedServices(t *testing.T) {
	projects := &handlerProjectStub{}
	mediaReader := &handlerMediaStub{}
	handler, err := NewHandler(HandlerOptions{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:          fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)},
		Auth:           authorizedHandlerAuthStub{},
		Articles:       handlerArticleStub{},
		Projects:       projects,
		Media:          mediaReader,
		MaxUploadBytes: 1024,
		PublicBaseURL:  "https://example.test",
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	adminResponse := httptest.NewRecorder()
	adminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	adminRequest.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf"})
	handler.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK || !strings.Contains(adminResponse.Body.String(), "管理项目") {
		t.Fatalf("admin projects response = %d %s", adminResponse.Code, adminResponse.Body.String())
	}
	if projects.groupsCalls != 1 {
		t.Fatalf("admin project calls = %d, want 1", projects.groupsCalls)
	}

	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, httptest.NewRequest(http.MethodGet, "/projects", nil))
	if publicResponse.Code != http.StatusOK || !strings.Contains(publicResponse.Body.String(), "公开项目") {
		t.Fatalf("public projects response = %d %s", publicResponse.Code, publicResponse.Body.String())
	}
	if projects.publicCalls != 1 {
		t.Fatalf("public project calls = %d, want 1", projects.publicCalls)
	}

	aboutResponse := httptest.NewRecorder()
	handler.ServeHTTP(aboutResponse, httptest.NewRequest(http.MethodGet, "/about", nil))
	if aboutResponse.Code != http.StatusOK || !strings.Contains(aboutResponse.Body.String(), ">1</strong><span>已发布文章</span>") || !strings.Contains(aboutResponse.Body.String(), ">1</strong><span>公开项目</span>") {
		t.Fatalf("about response = %d %s", aboutResponse.Code, aboutResponse.Body.String())
	}

	const mediaID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	mediaResponse := httptest.NewRecorder()
	handler.ServeHTTP(mediaResponse, httptest.NewRequest(http.MethodGet, "/media/"+mediaID, nil))
	if mediaResponse.Code != http.StatusNotFound {
		t.Fatalf("media response = %d, want 404 from configured reader", mediaResponse.Code)
	}
	if mediaReader.openCalls != 1 || mediaReader.lastID != mediaID || mediaReader.lastAdmin {
		t.Fatalf("media reader calls = (%d, %q, %t), want (1, %q, false)", mediaReader.openCalls, mediaReader.lastID, mediaReader.lastAdmin, mediaID)
	}
}

func TestNewHandlerRejectsPartiallyConfiguredStage1Services(t *testing.T) {
	_, err := NewHandler(HandlerOptions{Auth: handlerAuthStub{}})
	if err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("NewHandler() error = %v, want paired dependency error", err)
	}
}
