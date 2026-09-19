package adminapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/auth"
)

type fakeAuthService struct {
	loginResult    auth.LoginResult
	loginErr       error
	authSession    auth.Session
	authErr        error
	refreshSession auth.Session
	refreshErr     error
	logoutErr      error
	reauthResult   auth.LoginResult
	reauthErr      error
	loginCalls     int
	authCalls      int
	refreshCalls   int
}

func (service *fakeAuthService) Login(context.Context, string, string) (auth.LoginResult, error) {
	service.loginCalls++
	return service.loginResult, service.loginErr
}

func (service *fakeAuthService) Authenticate(context.Context, string) (auth.Session, error) {
	service.authCalls++
	return service.authSession, service.authErr
}

func (service *fakeAuthService) AuthenticateForReauth(context.Context, string) (auth.Session, error) {
	service.refreshCalls++
	return service.refreshSession, service.refreshErr
}

func (service *fakeAuthService) ValidateCSRF(_ any, token string) error {
	if token != "csrf-token" {
		return auth.ErrCSRFInvalid
	}
	return nil
}

func (service *fakeAuthService) Logout(context.Context, uint64) error { return service.logoutErr }

func (service *fakeAuthService) Reauthenticate(context.Context, auth.Session, string) (auth.LoginResult, error) {
	return service.reauthResult, service.reauthErr
}

type fakeArticleService struct {
	created      article.Article
	createErr    error
	got          article.Article
	getErr       error
	saved        article.Article
	saveErr      error
	published    article.Article
	publishErr   error
	withdrawn    article.Article
	withdrawErr  error
	list         []article.AdminArticleSummary
	listErr      error
	total        int64
	countErr     error
	currentOnGet article.Article
	getCalls     int
}

func (service *fakeArticleService) ListAdmin(context.Context, *article.Status, *string, int32, int32) ([]article.AdminArticleSummary, error) {
	return service.list, service.listErr
}

func (service *fakeArticleService) CountAdmin(context.Context, *article.Status, *string) (int64, error) {
	return service.total, service.countErr
}

func (service *fakeArticleService) CreateDraft(context.Context, article.CreateDraftRequest) (article.Article, error) {
	return service.created, service.createErr
}

func (service *fakeArticleService) GetByID(context.Context, uint64) (article.Article, error) {
	service.getCalls++
	if service.currentOnGet.ID != 0 {
		return service.currentOnGet, service.getErr
	}
	return service.got, service.getErr
}

func (service *fakeArticleService) Save(context.Context, uint64, article.SaveRequest) (article.Article, error) {
	return service.saved, service.saveErr
}

func (service *fakeArticleService) Publish(context.Context, uint64, article.PublishRequest) (article.Article, error) {
	return service.published, service.publishErr
}

func (service *fakeArticleService) Withdraw(context.Context, uint64, article.WithdrawRequest) (article.Article, error) {
	return service.withdrawn, service.withdrawErr
}

func testHandler(t *testing.T, authService *fakeAuthService, articleService *fakeArticleService) *Handler {
	t.Helper()
	handler, err := NewHandler(HandlerOptions{
		Auth:          authService,
		Articles:      articleService,
		PublicBaseURL: "https://example.test",
		Now:           func() time.Time { return time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func authenticatedSession() auth.Session {
	return auth.Session{
		ID:                9,
		AdminID:           7,
		Username:          "admin",
		LastSeenAt:        time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
		IdleExpiresAt:     time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC),
		AbsoluteExpiresAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
	}
}

func TestLoginSetsStrictSessionCookieAndSessionResponse(t *testing.T) {
	authService := &fakeAuthService{loginResult: auth.LoginResult{
		Admin:             auth.Admin{ID: 7, Username: "admin"},
		Token:             "token",
		CSRFToken:         "csrf-token",
		LastSeenAt:        time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
		IdleExpiresAt:     time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC),
		AbsoluteExpiresAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
	}}
	handler := testHandler(t, authService, &fakeArticleService{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"username":"admin","password":"a sufficiently long password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != "session" || cookie.Value != "token.csrf-token" || cookie.Path != "/" || !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %+v", cookie)
	}
	if !cookie.Expires.Equal(authService.loginResult.AbsoluteExpiresAt) {
		t.Fatalf("cookie expiry = %v, want %v", cookie.Expires, authService.loginResult.AbsoluteExpiresAt)
	}
}

func TestGetSessionRequiresCookieCSRFAndArticleCreateUsesOriginAndHeader(t *testing.T) {
	authService := &fakeAuthService{authSession: authenticatedSession()}
	articleService := &fakeArticleService{created: article.Article{ID: 3, Title: "Draft", Status: article.StatusDraft, Version: 1}}
	handler := testHandler(t, authService, articleService)

	get := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	get.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"csrf_token":"csrf-token"`) {
		t.Fatalf("GET session = %d %s", getResponse.Code, getResponse.Body.String())
	}

	create := httptest.NewRequest(http.MethodPost, "/api/v1/articles", strings.NewReader(`{"title":"Draft","body_markdown":null}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Origin", "https://example.test")
	create.Header.Set("X-CSRF-Token", "csrf-token")
	create.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST article = %d, body=%s", createResponse.Code, createResponse.Body.String())
	}

	create.Header.Del("Origin")
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, create)
	if failed.Code != http.StatusForbidden || !strings.Contains(failed.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("missing Origin = %d, body=%s", failed.Code, failed.Body.String())
	}
}

func TestArticleJSONAndVersionConflictMappings(t *testing.T) {
	authService := &fakeAuthService{authSession: authenticatedSession()}
	articleService := &fakeArticleService{saveErr: article.ErrConflict, currentOnGet: article.Article{ID: 3, Version: 8}}
	handler := testHandler(t, authService, articleService)

	unknown := httptest.NewRequest(http.MethodPut, "/api/v1/articles/3", strings.NewReader(`{"title":"Draft","body_markdown":"x","version":1,"unknown":true}`))
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set("Origin", "https://example.test")
	unknown.Header.Set("X-CSRF-Token", "csrf-token")
	unknown.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", unknownResponse.Code)
	}

	conflict := httptest.NewRequest(http.MethodPut, "/api/v1/articles/3", strings.NewReader(`{"title":"Draft","body_markdown":"x","version":1} trailing`))
	conflict.Header.Set("Content-Type", "application/json")
	conflict.Header.Set("Origin", "https://example.test")
	conflict.Header.Set("X-CSRF-Token", "csrf-token")
	conflict.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflict)
	if conflictResponse.Code != http.StatusBadRequest {
		t.Fatalf("trailing content status = %d", conflictResponse.Code)
	}

	valid := httptest.NewRequest(http.MethodPut, "/api/v1/articles/3", strings.NewReader(`{"title":"Draft","body_markdown":"x","version":1}`))
	valid.Header.Set("Content-Type", "application/json")
	valid.Header.Set("Origin", "https://example.test")
	valid.Header.Set("X-CSRF-Token", "csrf-token")
	valid.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusConflict || !strings.Contains(validResponse.Body.String(), `"current=8"`) {
		t.Fatalf("conflict = %d, body=%s", validResponse.Code, validResponse.Body.String())
	}
}

func TestLoginLimiterBlocksSixthCredentialFailure(t *testing.T) {
	authService := &fakeAuthService{loginErr: auth.ErrInvalidCredentials}
	handler := testHandler(t, authService, &fakeArticleService{})
	for attempt := 1; attempt <= 6; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"username":"admin","password":"wrong password"}`))
		request.RemoteAddr = "192.0.2.7:1234"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if attempt == 6 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.Code, want)
		}
	}
	if authService.loginCalls != 5 {
		t.Fatalf("login calls = %d, want 5 after rate limit", authService.loginCalls)
	}
}

func TestUnknownArticleStoreErrorMapsToDependencyUnavailable(t *testing.T) {
	authService := &fakeAuthService{authSession: authenticatedSession()}
	articleService := &fakeArticleService{getErr: errors.New("database temporarily unavailable")}
	handler := testHandler(t, authService, articleService)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/articles/3", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"dependency_unavailable"`) {
		t.Fatalf("GET article = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestReauthUsesIdleExpiredSessionWithoutNormalAuthentication(t *testing.T) {
	authService := &fakeAuthService{
		refreshSession: authenticatedSession(),
		reauthResult: auth.LoginResult{
			Admin:             auth.Admin{ID: 7, Username: "admin"},
			Token:             "rotated-token",
			CSRFToken:         "rotated-csrf",
			LastSeenAt:        time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC),
			IdleExpiresAt:     time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
			AbsoluteExpiresAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC),
		},
	}
	handler := testHandler(t, authService, &fakeArticleService{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/reauth", strings.NewReader(`{"password":"a sufficiently long password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://example.test")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("POST reauth = %d, body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "rotated-token.rotated-csrf" {
		t.Fatalf("rotated cookie = %+v", cookies)
	}
	if authService.refreshCalls != 1 || authService.authCalls != 0 {
		t.Fatalf("authentication calls = normal %d, reauth %d", authService.authCalls, authService.refreshCalls)
	}
}

func TestNormalSessionAuthenticationStillReportsIdleExpiry(t *testing.T) {
	authService := &fakeAuthService{authErr: auth.ErrSessionExpired}
	handler := testHandler(t, authService, &fakeArticleService{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"session_expired"`) {
		t.Fatalf("GET session = %d, body=%s", response.Code, response.Body.String())
	}
	if authService.authCalls != 1 || authService.refreshCalls != 0 {
		t.Fatalf("authentication calls = normal %d, reauth %d", authService.authCalls, authService.refreshCalls)
	}
}

var _ ArticleService = (*fakeArticleService)(nil)
var _ AuthService = (*fakeAuthService)(nil)
