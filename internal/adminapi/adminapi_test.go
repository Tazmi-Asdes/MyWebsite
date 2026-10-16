package adminapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	reauthCalls    int
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
	service.reauthCalls++
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

func TestLoginLimiterSharesAccountAndIPBudgets(t *testing.T) {
	t.Run("account across IPs", func(t *testing.T) {
		authService := &fakeAuthService{loginErr: auth.ErrInvalidCredentials}
		handler := testHandler(t, authService, &fakeArticleService{})
		for attempt := 1; attempt <= loginFailureLimit; attempt++ {
			response := failedLoginResponse(t, handler, "admin", "192.0.2."+strconv.Itoa(attempt)+":1234")
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("failure %d status = %d", attempt, response.Code)
			}
		}
		response := failedLoginResponse(t, handler, "admin", "192.0.2.99:1234")
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || authService.loginCalls != loginFailureLimit {
			t.Fatalf("cross-IP attempt = %d, Retry-After=%q, login calls=%d", response.Code, response.Header().Get("Retry-After"), authService.loginCalls)
		}
	})

	t.Run("IP across usernames", func(t *testing.T) {
		authService := &fakeAuthService{loginErr: auth.ErrInvalidCredentials}
		handler := testHandler(t, authService, &fakeArticleService{})
		for attempt := 1; attempt <= loginIPFailureLimit; attempt++ {
			response := failedLoginResponse(t, handler, "admin"+strconv.Itoa(attempt), "198.51.100.7:1234")
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("failure %d status = %d", attempt, response.Code)
			}
		}
		response := failedLoginResponse(t, handler, "another-account", "198.51.100.7:5678")
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || authService.loginCalls != loginIPFailureLimit {
			t.Fatalf("cross-account attempt = %d, Retry-After=%q, login calls=%d", response.Code, response.Header().Get("Retry-After"), authService.loginCalls)
		}
	})
}

func TestLoginLimiterReauthSharesFailureWindow(t *testing.T) {
	authService := &fakeAuthService{loginErr: auth.ErrInvalidCredentials, refreshSession: authenticatedSession()}
	handler := testHandler(t, authService, &fakeArticleService{})
	for attempt := 1; attempt < loginFailureLimit; attempt++ {
		if response := failedLoginResponse(t, handler, "admin", "203.0.113.7:1234"); response.Code != http.StatusUnauthorized {
			t.Fatalf("login failure %d status = %d", attempt, response.Code)
		}
	}
	authService.reauthErr = auth.ErrInvalidCredentials
	reauth := httptest.NewRequest(http.MethodPost, "/api/v1/session/reauth", strings.NewReader(`{"password":"wrong"}`))
	reauth.RemoteAddr = "203.0.113.7:4321"
	reauth.Header.Set("Content-Type", "application/json")
	reauth.Header.Set("Origin", "https://example.test")
	reauth.Header.Set("X-CSRF-Token", "csrf-token")
	reauth.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	reauthResponse := httptest.NewRecorder()
	handler.ServeHTTP(reauthResponse, reauth)
	if reauthResponse.Code != http.StatusUnauthorized || authService.reauthCalls != 1 {
		t.Fatalf("reauth status = %d, reauth calls = %d", reauthResponse.Code, authService.reauthCalls)
	}
	blocked := failedLoginResponse(t, handler, "admin", "203.0.113.8:1234")
	if blocked.Code != http.StatusTooManyRequests || authService.loginCalls != loginFailureLimit-1 {
		t.Fatalf("login after shared reauth failures = %d, login calls = %d", blocked.Code, authService.loginCalls)
	}
}

func TestReauthBlocksSixthCredentialFailureBeforePasswordService(t *testing.T) {
	authService := &fakeAuthService{refreshSession: authenticatedSession(), reauthErr: auth.ErrInvalidCredentials}
	handler := testHandler(t, authService, &fakeArticleService{})
	for attempt := 1; attempt <= loginFailureLimit; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/session/reauth", strings.NewReader(`{"password":"wrong"}`))
		request.RemoteAddr = "192.0.2.200:1234"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://example.test")
		request.Header.Set("X-CSRF-Token", "csrf-token")
		request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("reauth attempt %d status = %d, want %d", attempt, response.Code, http.StatusUnauthorized)
		}
	}
	csrfFailure := httptest.NewRequest(http.MethodPost, "/api/v1/session/reauth", strings.NewReader(`{"password":"wrong"}`))
	csrfFailure.RemoteAddr = "192.0.2.200:1234"
	csrfFailure.Header.Set("Content-Type", "application/json")
	csrfFailure.Header.Set("Origin", "https://example.test")
	csrfFailure.Header.Set("X-CSRF-Token", "incorrect")
	csrfFailure.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	csrfFailureResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfFailureResponse, csrfFailure)
	if csrfFailureResponse.Code != http.StatusForbidden {
		t.Fatalf("reauth CSRF failure status = %d, want %d", csrfFailureResponse.Code, http.StatusForbidden)
	}

	blocked := httptest.NewRequest(http.MethodPost, "/api/v1/session/reauth", strings.NewReader(`{"password":"wrong"}`))
	blocked.RemoteAddr = "192.0.2.200:1234"
	blocked.Header.Set("Content-Type", "application/json")
	blocked.Header.Set("Origin", "https://example.test")
	blocked.Header.Set("X-CSRF-Token", "csrf-token")
	blocked.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	blockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(blockedResponse, blocked)
	if blockedResponse.Code != http.StatusTooManyRequests || blockedResponse.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth valid reauth status = %d, Retry-After=%q", blockedResponse.Code, blockedResponse.Header().Get("Retry-After"))
	}
	if authService.refreshCalls != loginFailureLimit+2 || authService.reauthCalls != loginFailureLimit {
		t.Fatalf("AuthenticateForReauth calls=%d, password-service calls=%d", authService.refreshCalls, authService.reauthCalls)
	}
}

func TestLoginLimiterSuccessClearsAccountAndServiceErrorsDoNotCount(t *testing.T) {
	authService := &fakeAuthService{loginErr: auth.ErrInvalidCredentials}
	handler := testHandler(t, authService, &fakeArticleService{})
	if response := failedLoginResponse(t, handler, "admin", "192.0.2.8:1234"); response.Code != http.StatusUnauthorized {
		t.Fatalf("initial failure status = %d", response.Code)
	}
	accountKey, ipKey := loginLimiterKeys("192.0.2.8", "admin")
	authService.loginErr = nil
	request := newLoginHTTPRequest("admin", "correct password", "192.0.2.8:5678")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("successful login status = %d", response.Code)
	}
	if _, ok := handler.limiter.entries[accountKey]; ok {
		t.Fatal("successful login retained account failure bucket")
	}
	if entry, ok := handler.limiter.entries[ipKey]; !ok || entry.count != 1 {
		t.Fatalf("successful login IP bucket = %+v, exists=%v", entry, ok)
	}

	serviceErrorIP := "198.51.100.24"
	serviceErrorAccount, serviceErrorBucket := loginLimiterKeys(serviceErrorIP, "other")
	authService.loginErr = errors.New("database unavailable")
	serviceError := httptest.NewRecorder()
	handler.ServeHTTP(serviceError, newLoginHTTPRequest("other", "password", serviceErrorIP+":1234"))
	if serviceError.Code != http.StatusServiceUnavailable {
		t.Fatalf("service error status = %d", serviceError.Code)
	}
	if _, ok := handler.limiter.entries[serviceErrorAccount]; ok {
		t.Fatal("service error created an account failure bucket")
	}
	if _, ok := handler.limiter.entries[serviceErrorBucket]; ok {
		t.Fatal("service error created an IP failure bucket")
	}
}

func TestLoginLimiterExpiresEntriesAndFailsClosedAtCapacity(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(func() time.Time { return now })
	for index := 0; index < loginLimiterMaxEntries; index++ {
		key := loginLimitKey{kind: loginAccountBucket, value: "entry-" + strconv.Itoa(index)}
		limiter.entries[key] = failureEntry{started: now, count: 1}
	}
	accountKey, ipKey := loginLimiterKeys("192.0.2.44", "new-account")
	if retryAfter, blocked := limiter.allow(accountKey, ipKey); !blocked || retryAfter < 1 {
		t.Fatalf("full limiter allow = retry %d, blocked %v", retryAfter, blocked)
	}
	if len(limiter.entries) != loginLimiterMaxEntries {
		t.Fatalf("capacity denial changed entries to %d", len(limiter.entries))
	}
	authService := &fakeAuthService{loginErr: auth.ErrInvalidCredentials}
	handler := testHandler(t, authService, &fakeArticleService{})
	handler.limiter = limiter
	capacityResponse := failedLoginResponse(t, handler, "new-account", "192.0.2.44:1234")
	if capacityResponse.Code != http.StatusTooManyRequests || capacityResponse.Header().Get("Retry-After") == "" || authService.loginCalls != 0 {
		t.Fatalf("capacity request = %d, Retry-After=%q, login calls=%d", capacityResponse.Code, capacityResponse.Header().Get("Retry-After"), authService.loginCalls)
	}
	now = now.Add(loginFailureWindow)
	if retryAfter, blocked := limiter.allow(accountKey, ipKey); blocked {
		t.Fatalf("expired entries still blocked after %d seconds", retryAfter)
	}
	if len(limiter.entries) != 2 {
		t.Fatalf("lazy expiry retained %d entries, want only the two new buckets", len(limiter.entries))
	}
}

func TestClientIPHonorsOnlyTrustedSingleXRealIP(t *testing.T) {
	handler, err := NewHandler(HandlerOptions{
		Auth: &fakeAuthService{}, Articles: &fakeArticleService{},
		TrustedProxyCIDRs: []string{"198.51.100.0/24"},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	tests := []struct {
		name       string
		remoteAddr string
		realIPs    []string
		xff        string
		want       string
	}{
		{name: "trusted proxy", remoteAddr: "198.51.100.8:1234", realIPs: []string{"203.0.113.12"}, want: "203.0.113.12"},
		{name: "untrusted source", remoteAddr: "203.0.113.9:1234", realIPs: []string{"192.0.2.10"}, want: "203.0.113.9"},
		{name: "duplicate real IP", remoteAddr: "198.51.100.8:1234", realIPs: []string{"203.0.113.12", "203.0.113.13"}, want: "198.51.100.8"},
		{name: "invalid real IP", remoteAddr: "198.51.100.8:1234", realIPs: []string{"203.0.113.12, 192.0.2.9"}, want: "198.51.100.8"},
		{name: "forwarded for ignored", remoteAddr: "198.51.100.8:1234", xff: "192.0.2.9", want: "198.51.100.8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remoteAddr
			for _, value := range test.realIPs {
				request.Header.Add("X-Real-IP", value)
			}
			if test.xff != "" {
				request.Header.Set("X-Forwarded-For", test.xff)
			}
			if got := handler.clientIP(request); got != test.want {
				t.Fatalf("clientIP() = %q, want %q", got, test.want)
			}
		})
	}
	defaultTrustHandler, err := NewHandler(HandlerOptions{Auth: &fakeAuthService{}, Articles: &fakeArticleService{}})
	if err != nil {
		t.Fatalf("NewHandler() without trusted proxies error = %v", err)
	}
	defaultRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	defaultRequest.RemoteAddr = "198.51.100.8:1234"
	defaultRequest.Header.Set("X-Real-IP", "203.0.113.12")
	if got := defaultTrustHandler.clientIP(defaultRequest); got != "198.51.100.8" {
		t.Fatalf("default trusted proxy behavior = %q, want remote address", got)
	}

	if _, err := NewHandler(HandlerOptions{Auth: &fakeAuthService{}, Articles: &fakeArticleService{}, TrustedProxyCIDRs: []string{"invalid"}}); err == nil {
		t.Fatal("NewHandler() accepted an invalid trusted proxy CIDR")
	}
}

func failedLoginResponse(t *testing.T, handler http.Handler, username, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := newLoginHTTPRequest(username, "wrong password", remoteAddr)
	handler.ServeHTTP(response, request)
	return response
}

func newLoginHTTPRequest(username, password, remoteAddr string) *http.Request {
	body := `{"username":"` + username + `","password":"` + password + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(body))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")
	return request
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
