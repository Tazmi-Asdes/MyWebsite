// Package adminapi exposes the Stage 1 administrator HTTP API.
//
// The package deliberately depends on the auth and article service contracts
// instead of a database implementation. The application wiring can therefore
// provide the real services while the HTTP behaviour remains unit-testable.
package adminapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/auth"
	"mywebsite/internal/markdown"
	"mywebsite/internal/media"
	"mywebsite/internal/project"
)

const (
	defaultPageSize    int32 = 20
	maxPageSize        int32 = 20
	loginBodyLimit           = 4 * 1024
	articleBodyLimit         = 2*1024*1024 + 8*1024
	loginFailureWindow       = 15 * time.Minute
	loginFailureLimit        = 5
)

var (
	errUnsupportedMediaType = errors.New("adminapi: unsupported media type")
	errRequestTooLarge      = errors.New("adminapi: request body too large")
	errInvalidJSON          = errors.New("adminapi: invalid JSON request")
	errTrailingJSON         = errors.New("adminapi: trailing JSON content")
	errMissingField         = errors.New("adminapi: required request field is missing")
)

// AuthService is the authentication contract used by the HTTP layer. The
// concrete *auth.Service satisfies it directly.
type AuthService interface {
	Login(context.Context, string, string) (auth.LoginResult, error)
	Authenticate(context.Context, string) (auth.Session, error)
	AuthenticateForReauth(context.Context, string) (auth.Session, error)
	ValidateCSRF(any, string) error
	Logout(context.Context, uint64) error
	Reauthenticate(context.Context, auth.Session, string) (auth.LoginResult, error)
}

// ArticleService is the article contract used by the HTTP layer. The
// concrete *article.Service satisfies it directly.
type ArticleService interface {
	ListAdmin(context.Context, *article.Status, *string, int32, int32) ([]article.AdminArticleSummary, error)
	CountAdmin(context.Context, *article.Status, *string) (int64, error)
	CreateDraft(context.Context, article.CreateDraftRequest) (article.Article, error)
	GetByID(context.Context, uint64) (article.Article, error)
	Save(context.Context, uint64, article.SaveRequest) (article.Article, error)
	Publish(context.Context, uint64, article.PublishRequest) (article.Article, error)
	Withdraw(context.Context, uint64, article.WithdrawRequest) (article.Article, error)
}

// ProjectService is the project contract used by the HTTP layer. The
// concrete *project.Service satisfies it directly while focused HTTP tests
// can provide a small fake without a database.
type ProjectService interface {
	ListGroups(context.Context) (project.ProjectGroups, error)
	Create(context.Context, project.CreateRequest) (project.Project, error)
	Get(context.Context, uint64) (project.Project, error)
	Update(context.Context, uint64, project.UpdateRequest) (project.Project, error)
	Publish(context.Context, uint64, project.PublishRequest) (project.Project, uint64, error)
	Hide(context.Context, uint64, project.HideRequest) (uint64, error)
	Reorder(context.Context, project.OrderRequest) (uint64, error)
}

// MediaService is the media contract used by the HTTP layer. The concrete
// *media.Service satisfies it directly.
type MediaService interface {
	Upload(context.Context, uint64, io.Reader) (media.Asset, error)
	Open(context.Context, string, bool) (media.OpenResult, error)
}

// HandlerOptions configures the HTTP adapter. Auth and Articles are required;
// AuthService and ArticleService are aliases kept to make wiring explicit at
// call sites that prefer interface-named fields.
type HandlerOptions struct {
	Auth           AuthService
	Articles       ArticleService
	Projects       ProjectService
	Media          MediaService
	MaxUploadBytes int64
	AuthService    AuthService
	ArticleService ArticleService
	PublicBaseURL  string
	PublicOrigin   string
	CookieSecure   bool
	SecureCookies  bool
	Now            func() time.Time
}

// Config is a compatibility alias for HandlerOptions.
type Config = HandlerOptions

// Handler implements the administrator routes. Stage 2 project and media
// routes are enabled when their optional services are configured.
type Handler struct {
	auth           AuthService
	articles       ArticleService
	projects       ProjectService
	media          MediaService
	maxUploadBytes int64
	publicOrigin   string
	cookieSecure   bool
	now            func() time.Time
	limiter        *loginLimiter
	routes         http.Handler
}

// NewHandler constructs an administrator API handler. Routes are registered
// separately with RegisterRoutes so the caller can compose them into the
// application's existing ServeMux.
func NewHandler(options HandlerOptions) (*Handler, error) {
	authService := options.Auth
	if authService == nil {
		authService = options.AuthService
	}
	articleService := options.Articles
	if articleService == nil {
		articleService = options.ArticleService
	}
	if authService == nil {
		return nil, errors.New("adminapi: auth service is required")
	}
	if articleService == nil {
		return nil, errors.New("adminapi: article service is required")
	}

	originValue := options.PublicBaseURL
	if originValue == "" {
		originValue = options.PublicOrigin
	}
	origin, err := configuredOrigin(originValue)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	handler := &Handler{
		auth:           authService,
		articles:       articleService,
		projects:       options.Projects,
		media:          options.Media,
		maxUploadBytes: options.MaxUploadBytes,
		publicOrigin:   origin,
		cookieSecure:   options.CookieSecure || options.SecureCookies,
		now:            func() time.Time { return now().UTC() },
	}
	if handler.maxUploadBytes <= 0 {
		handler.maxUploadBytes = defaultMediaUploadBytes
	}
	handler.limiter = newLoginLimiter(handler.now)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	handler.routes = mux
	return handler, nil
}

// New is a concise constructor alias for NewHandler.
func New(options HandlerOptions) (*Handler, error) { return NewHandler(options) }

// RegisterRoutes registers the complete /api/v1 administrator route set on a
// Go 1.22+ ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if h == nil || mux == nil {
		return
	}
	mux.Handle("GET /api/v1/session", http.HandlerFunc(h.getSession))
	mux.Handle("POST /api/v1/session", http.HandlerFunc(h.createSession))
	mux.Handle("DELETE /api/v1/session", http.HandlerFunc(h.deleteSession))
	mux.Handle("POST /api/v1/session/reauth", http.HandlerFunc(h.reauthenticateSession))
	mux.Handle("GET /api/v1/articles", http.HandlerFunc(h.listArticles))
	mux.Handle("POST /api/v1/articles", http.HandlerFunc(h.createArticle))
	mux.Handle("GET /api/v1/articles/{id}", http.HandlerFunc(h.getArticle))
	mux.Handle("PUT /api/v1/articles/{id}", http.HandlerFunc(h.updateArticle))
	mux.Handle("POST /api/v1/articles/{id}/publish", http.HandlerFunc(h.publishArticle))
	mux.Handle("POST /api/v1/articles/{id}/withdraw", http.HandlerFunc(h.withdrawArticle))
	if h.projects != nil {
		mux.Handle("GET /api/v1/projects", http.HandlerFunc(h.listProjects))
		mux.Handle("POST /api/v1/projects", http.HandlerFunc(h.createProject))
		mux.Handle("GET /api/v1/projects/{id}", http.HandlerFunc(h.getProject))
		mux.Handle("PUT /api/v1/projects/{id}", http.HandlerFunc(h.updateProject))
		mux.Handle("POST /api/v1/projects/{id}/publish", http.HandlerFunc(h.publishProject))
		mux.Handle("POST /api/v1/projects/{id}/hide", http.HandlerFunc(h.hideProject))
		mux.Handle("PUT /api/v1/projects/order", http.HandlerFunc(h.reorderProjects))
	}
	if h.media != nil {
		mux.Handle("POST /api/v1/media", http.HandlerFunc(h.uploadMedia))
		mux.Handle("GET /api/v1/media/{id}", http.HandlerFunc(h.getMedia))
	}
}

// ServeHTTP makes Handler directly usable as an http.Handler in tests and
// small integrations. Applications should normally call RegisterRoutes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.routes == nil {
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	h.routes.ServeHTTP(w, r)
}

type loginRequest struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
}

type reauthRequest struct {
	Password *string `json:"password"`
}

type articleWriteRequest struct {
	Title        *string `json:"title"`
	BodyMarkdown *string `json:"body_markdown"`
	Version      *uint64 `json:"version"`
}

type articleVersionRequest struct {
	Version *uint64 `json:"version"`
}

type sessionResponse struct {
	AdminID           uint64    `json:"admin_id"`
	Username          string    `json:"username"`
	CSRFToken         string    `json:"csrf_token"`
	LastSeenAt        time.Time `json:"last_seen_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
}

type articleSummaryResponse struct {
	ID               uint64     `json:"id"`
	PublicULID       *string    `json:"public_ulid"`
	Title            string     `json:"title"`
	Status           string     `json:"status"`
	FirstPublishedAt *time.Time `json:"first_published_at"`
	Version          uint64     `json:"version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type articleEditResponse struct {
	ID               uint64          `json:"id"`
	PublicULID       *string         `json:"public_ulid"`
	Title            string          `json:"title"`
	BodyMarkdown     *string         `json:"body_markdown"`
	BodyHTML         *string         `json:"body_html"`
	TOCJSON          json.RawMessage `json:"toc_json"`
	PreviewText      *string         `json:"preview_text"`
	RendererVersion  *string         `json:"renderer_version"`
	Status           string          `json:"status"`
	FirstPublishedAt *time.Time      `json:"first_published_at"`
	Version          uint64          `json:"version"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type paginationResponse struct {
	Page       int32 `json:"page"`
	PerPage    int32 `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int32 `json:"total_pages"`
}

type articleListResponse struct {
	Items      []articleSummaryResponse `json:"items"`
	Pagination paginationResponse       `json:"pagination"`
}

type problemDetails struct {
	Type      string              `json:"type"`
	Title     string              `json:"title"`
	Status    int                 `json:"status"`
	Code      string              `json:"code"`
	RequestID string              `json:"request_id"`
	Errors    map[string][]string `json:"errors"`
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	fields, err := decodeObject(w, r, loginBodyLimit, map[string]struct{}{"username": {}, "password": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if _, ok := fields["username"]; !ok || request.Username == nil || !isPresent(fields, "password") || request.Password == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"request": {"username and password are required"}})
		return
	}

	key := limiterKey(r.RemoteAddr, strings.TrimSpace(*request.Username))
	if retryAfter, blocked := h.limiter.blocked(key); blocked {
		w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
		h.problem(w, http.StatusTooManyRequests, "rate_limited", map[string][]string{"login": {"too many failed login attempts"}})
		return
	}
	result, err := h.auth.Login(r.Context(), *request.Username, *request.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			if retryAfter, blocked := h.limiter.failure(key); blocked {
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				h.problem(w, http.StatusTooManyRequests, "rate_limited", map[string][]string{"login": {"too many failed login attempts"}})
				return
			}
			h.problem(w, http.StatusUnauthorized, "authentication_required", nil)
			return
		}
		if errors.Is(err, auth.ErrStore) {
			h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
			return
		}
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	h.limiter.success(key)
	h.setSessionCookie(w, result)
	h.writeJSON(w, http.StatusOK, loginSessionResponse(result))
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	session, cookie, ok := h.authenticate(w, r, false)
	if !ok {
		return
	}
	if err := h.auth.ValidateCSRF(session, cookie.csrfToken); err != nil {
		h.problem(w, http.StatusForbidden, "csrf_failed", nil)
		return
	}
	h.writeJSON(w, http.StatusOK, sessionResponse{
		AdminID:           session.AdminID,
		Username:          session.Username,
		CSRFToken:         cookie.csrfToken,
		LastSeenAt:        session.LastSeenAt.UTC(),
		ExpiresAt:         session.IdleExpiresAt.UTC(),
		AbsoluteExpiresAt: session.AbsoluteExpiresAt.UTC(),
	})
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.authenticateMutation(w, r)
	if !ok {
		return
	}
	if err := h.auth.Logout(r.Context(), session.ID); err != nil {
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reauthenticateSession(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.authenticateReauth(w, r)
	if !ok {
		return
	}
	var request reauthRequest
	fields, err := decodeObject(w, r, loginBodyLimit, map[string]struct{}{"password": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if !isPresent(fields, "password") || request.Password == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"password": {"password is required"}})
		return
	}
	result, err := h.auth.Reauthenticate(r.Context(), session, *request.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			h.problem(w, http.StatusUnauthorized, "authentication_required", nil)
		case errors.Is(err, auth.ErrInvalidSession), errors.Is(err, auth.ErrSessionRevoked):
			h.problem(w, http.StatusUnauthorized, "authentication_required", nil)
		default:
			h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		}
		return
	}
	h.setSessionCookie(w, result)
	h.writeJSON(w, http.StatusOK, loginSessionResponse(result))
}

func (h *Handler) listArticles(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticate(w, r, false); !ok {
		return
	}
	page, perPage, err := parsePagination(r)
	if err != nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"pagination": {err.Error()}})
		return
	}
	var status *article.Status
	if values, ok := r.URL.Query()["status"]; ok {
		if len(values) != 1 || (values[0] != string(article.StatusDraft) && values[0] != string(article.StatusPublished)) {
			h.problem(w, http.StatusUnprocessableEntity, "validation_failed", map[string][]string{"status": {"status must be draft or published"}})
			return
		}
		parsed := article.Status(values[0])
		status = &parsed
	}
	var query *string
	if values, ok := r.URL.Query()["q"]; ok {
		if len(values) != 1 {
			h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"q": {"q must occur once"}})
			return
		}
		query = &values[0]
	}
	offset64 := (int64(page) - 1) * int64(perPage)
	if offset64 > math.MaxInt32 {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"page": {"page is too large"}})
		return
	}
	items, err := h.articles.ListAdmin(r.Context(), status, query, perPage, int32(offset64))
	if err != nil {
		h.writeArticleError(w, r, err, 0)
		return
	}
	total, err := h.articles.CountAdmin(r.Context(), status, query)
	if err != nil {
		h.writeArticleError(w, r, err, 0)
		return
	}
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(perPage) - 1) / int64(perPage)
	}
	if totalPages > math.MaxInt32 {
		totalPages = math.MaxInt32
	}
	responses := make([]articleSummaryResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, mapArticleSummary(item))
	}
	h.writeJSON(w, http.StatusOK, articleListResponse{
		Items: responses,
		Pagination: paginationResponse{
			Page: page, PerPage: perPage, Total: total, TotalPages: int32(totalPages),
		},
	})
}

func (h *Handler) createArticle(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	request, ok := h.decodeArticleWrite(w, r, false)
	if !ok {
		return
	}
	result, err := h.articles.CreateDraft(r.Context(), article.CreateDraftRequest{Title: *request.Title, BodyMarkdown: request.BodyMarkdown})
	if err != nil {
		h.writeArticleError(w, r, err, 0)
		return
	}
	h.writeJSON(w, http.StatusCreated, mapArticleEdit(result))
}

func (h *Handler) getArticle(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticate(w, r, false); !ok {
		return
	}
	id, ok := parseArticleID(w, r)
	if !ok {
		return
	}
	result, err := h.articles.GetByID(r.Context(), id)
	if err != nil {
		h.writeArticleError(w, r, err, id)
		return
	}
	h.writeJSON(w, http.StatusOK, mapArticleEdit(result))
}

func (h *Handler) updateArticle(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	id, ok := parseArticleID(w, r)
	if !ok {
		return
	}
	request, ok := h.decodeArticleWrite(w, r, true)
	if !ok {
		return
	}
	result, err := h.articles.Save(r.Context(), id, article.SaveRequest{Title: *request.Title, BodyMarkdown: request.BodyMarkdown, Version: *request.Version})
	if err != nil {
		h.writeArticleError(w, r, err, id)
		return
	}
	h.writeJSON(w, http.StatusOK, mapArticleEdit(result))
}

func (h *Handler) publishArticle(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	id, ok := parseArticleID(w, r)
	if !ok {
		return
	}
	request, ok := h.decodeArticleWrite(w, r, true)
	if !ok {
		return
	}
	result, err := h.articles.Publish(r.Context(), id, article.PublishRequest{Title: *request.Title, BodyMarkdown: request.BodyMarkdown, Version: *request.Version})
	if err != nil {
		h.writeArticleError(w, r, err, id)
		return
	}
	h.writeJSON(w, http.StatusOK, mapArticleEdit(result))
}

func (h *Handler) withdrawArticle(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	id, ok := parseArticleID(w, r)
	if !ok {
		return
	}
	var request articleVersionRequest
	fields, err := decodeObject(w, r, articleBodyLimit, map[string]struct{}{"version": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return
	}
	if !isPresent(fields, "version") || request.Version == nil || *request.Version == 0 {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"version": {"version is required"}})
		return
	}
	if _, err := h.articles.Withdraw(r.Context(), id, article.WithdrawRequest{Version: *request.Version}); err != nil {
		h.writeArticleError(w, r, err, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) decodeArticleWrite(w http.ResponseWriter, r *http.Request, versionRequired bool) (articleWriteRequest, bool) {
	var request articleWriteRequest
	fields, err := decodeObject(w, r, articleBodyLimit, map[string]struct{}{"title": {}, "body_markdown": {}, "version": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return articleWriteRequest{}, false
	}
	if !isPresent(fields, "title") || request.Title == nil || !isPresent(fields, "body_markdown") {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"request": {"title and body_markdown are required"}})
		return articleWriteRequest{}, false
	}
	if versionRequired && (!isPresent(fields, "version") || request.Version == nil || *request.Version == 0) {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"version": {"version is required"}})
		return articleWriteRequest{}, false
	}
	return request, true
}

func (h *Handler) authenticateMutation(w http.ResponseWriter, r *http.Request) (auth.Session, sessionCookie, bool) {
	return h.authenticateWithOptions(w, r, true)
}

func (h *Handler) authenticateReauth(w http.ResponseWriter, r *http.Request) (auth.Session, sessionCookie, bool) {
	return h.authenticateWithAuthenticator(w, r, true, h.auth.AuthenticateForReauth)
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request, _ bool) (auth.Session, sessionCookie, bool) {
	return h.authenticateWithOptions(w, r, false)
}

func (h *Handler) authenticateWithOptions(w http.ResponseWriter, r *http.Request, mutation bool) (auth.Session, sessionCookie, bool) {
	return h.authenticateWithAuthenticator(w, r, mutation, h.auth.Authenticate)
}

func (h *Handler) authenticateWithAuthenticator(w http.ResponseWriter, r *http.Request, mutation bool, authenticate func(context.Context, string) (auth.Session, error)) (auth.Session, sessionCookie, bool) {
	cookie, err := h.readSessionCookie(r)
	if err != nil {
		h.problem(w, http.StatusUnauthorized, "authentication_required", nil)
		return auth.Session{}, sessionCookie{}, false
	}
	session, err := authenticate(r.Context(), cookie.token)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSessionExpired):
			h.problem(w, http.StatusUnauthorized, "session_expired", nil)
		case errors.Is(err, auth.ErrStore):
			h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		default:
			h.problem(w, http.StatusUnauthorized, "authentication_required", nil)
		}
		return auth.Session{}, sessionCookie{}, false
	}
	if mutation {
		if !h.validOrigin(r) {
			h.problem(w, http.StatusForbidden, "csrf_failed", nil)
			return auth.Session{}, sessionCookie{}, false
		}
		csrfValues := r.Header.Values("X-CSRF-Token")
		if len(csrfValues) != 1 || h.auth.ValidateCSRF(session, csrfValues[0]) != nil {
			h.problem(w, http.StatusForbidden, "csrf_failed", nil)
			return auth.Session{}, sessionCookie{}, false
		}
	}
	return session, cookie, true
}

func (h *Handler) validOrigin(r *http.Request) bool {
	if h.publicOrigin == "" {
		return false
	}
	values := r.Header.Values("Origin")
	return len(values) == 1 && values[0] == h.publicOrigin
}

type sessionCookie struct {
	token     string
	csrfToken string
}

func (h *Handler) readSessionCookie(r *http.Request) (sessionCookie, error) {
	name := h.cookieName()
	var matched []*http.Cookie
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			matched = append(matched, cookie)
		}
	}
	if len(matched) != 1 {
		return sessionCookie{}, auth.ErrInvalidSession
	}
	parts := strings.Split(matched[0].Value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return sessionCookie{}, auth.ErrInvalidSession
	}
	return sessionCookie{token: parts[0], csrfToken: parts[1]}, nil
}

func (h *Handler) cookieName() string {
	if h.cookieSecure {
		return "__Host-session"
	}
	return "session"
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, result auth.LoginResult) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName(),
		Value:    result.Token + "." + result.CSRFToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  result.AbsoluteExpiresAt.UTC(),
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
	})
}

func loginSessionResponse(result auth.LoginResult) sessionResponse {
	return sessionResponse{
		AdminID:           result.Admin.ID,
		Username:          result.Admin.Username,
		CSRFToken:         result.CSRFToken,
		LastSeenAt:        result.LastSeenAt.UTC(),
		ExpiresAt:         result.IdleExpiresAt.UTC(),
		AbsoluteExpiresAt: result.AbsoluteExpiresAt.UTC(),
	}
}

func mapArticleSummary(value article.AdminArticleSummary) articleSummaryResponse {
	return articleSummaryResponse{
		ID: value.ID, PublicULID: value.PublicULID, Title: value.Title, Status: string(value.Status),
		FirstPublishedAt: utcTimePtr(value.FirstPublishedAt), Version: value.Version,
		CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func mapArticleEdit(value article.Article) articleEditResponse {
	toc := value.TOCJSON
	trimmedTOC := strings.TrimSpace(string(toc))
	if len(toc) == 0 || !json.Valid(toc) || trimmedTOC == "null" || !strings.HasPrefix(trimmedTOC, "{") {
		toc = json.RawMessage(`{}`)
	}
	return articleEditResponse{
		ID: value.ID, PublicULID: value.PublicULID, Title: value.Title,
		BodyMarkdown: value.BodyMarkdown, BodyHTML: value.BodyHTML, TOCJSON: toc,
		PreviewText: value.PreviewText, RendererVersion: value.RendererVersion,
		Status: string(value.Status), FirstPublishedAt: utcTimePtr(value.FirstPublishedAt),
		Version: value.Version, CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}

func parseArticleID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	value := r.PathValue("id")
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		// Path IDs are syntax errors rather than missing domain rows.
		problemForRequest(w, http.StatusBadRequest, "invalid_request", map[string][]string{"id": {"id must be a positive integer"}})
		return 0, false
	}
	return id, true
}

func parsePagination(r *http.Request) (int32, int32, error) {
	query := r.URL.Query()
	page := int64(1)
	perPage := int64(defaultPageSize)
	if values, ok := query["page"]; ok {
		if len(values) != 1 {
			return 0, 0, errors.New("page must occur once")
		}
		parsed, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || parsed < 1 {
			return 0, 0, errors.New("page must be a positive integer")
		}
		page = parsed
	}
	if values, ok := query["per_page"]; ok {
		if len(values) != 1 {
			return 0, 0, errors.New("per_page must occur once")
		}
		parsed, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || parsed < 1 || parsed > int64(maxPageSize) {
			return 0, 0, errors.New("per_page must be between 1 and 20")
		}
		perPage = parsed
	}
	return int32(page), int32(perPage), nil
}

func configuredOrigin(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("adminapi: PUBLIC_BASE_URL must be an origin")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func (h *Handler) writeArticleError(w http.ResponseWriter, r *http.Request, err error, id uint64) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		h.problem(w, http.StatusNotFound, "not_found", nil)
	case errors.Is(err, article.ErrConflict):
		errorsMap := map[string][]string{}
		if id != 0 {
			if current, getErr := h.articles.GetByID(r.Context(), id); getErr == nil {
				errorsMap["version"] = []string{fmt.Sprintf("current=%d", current.Version)}
			}
		}
		h.problem(w, http.StatusConflict, "version_conflict", errorsMap)
	case errors.Is(err, article.ErrBodyTooLarge):
		h.problem(w, http.StatusRequestEntityTooLarge, "validation_failed", map[string][]string{"body_markdown": {"body exceeds 2 MiB"}})
	case errors.Is(err, article.ErrInvalidTitle), errors.Is(err, article.ErrPublishedBodyRequired), errors.Is(err, article.ErrNotPublished), errors.Is(err, article.ErrInvalidStatus), errors.Is(err, article.ErrInvalidSearch):
		h.problem(w, http.StatusUnprocessableEntity, "validation_failed", nil)
	case errors.Is(err, markdown.ErrInvalidMediaReference):
		h.problem(w, http.StatusUnprocessableEntity, "invalid_media_reference", nil)
	case errors.Is(err, markdown.ErrImageAltRequired):
		h.problem(w, http.StatusUnprocessableEntity, "image_alt_required", nil)
	case errors.Is(err, markdown.ErrImageAltInvalid):
		h.problem(w, http.StatusUnprocessableEntity, "image_alt_invalid", nil)
	case errors.Is(err, article.ErrReferencedMediaNotFound):
		h.problem(w, http.StatusUnprocessableEntity, "referenced_media_not_found", nil)
	case errors.Is(err, article.ErrRendererUnavailable), errors.Is(err, article.ErrStoreCapability):
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
	default:
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
	}
}

func (h *Handler) writeDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnsupportedMediaType):
		h.problem(w, http.StatusUnsupportedMediaType, "unsupported_media_type", nil)
	case errors.Is(err, errRequestTooLarge):
		h.problem(w, http.StatusRequestEntityTooLarge, "validation_failed", nil)
	default:
		h.problem(w, http.StatusBadRequest, "invalid_request", nil)
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) problem(w http.ResponseWriter, status int, code string, fields map[string][]string) {
	problemForRequestWithID(w, status, code, fields, w.Header().Get("X-Request-ID"))
}

func problemForRequest(w http.ResponseWriter, status int, code string, fields map[string][]string) {
	problemForRequestWithID(w, status, code, fields, w.Header().Get("X-Request-ID"))
}

func problemForRequestWithID(w http.ResponseWriter, status int, code string, fields map[string][]string, requestID string) {
	if fields == nil {
		fields = map[string][]string{}
	}
	problem := problemDetails{Type: "about:blank", Title: problemTitle(code), Status: status, Code: code, RequestID: requestID, Errors: fields}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

func problemTitle(code string) string {
	switch code {
	case "authentication_required", "session_expired":
		return "认证失败"
	case "invalid_actor":
		return "身份无效"
	case "csrf_failed":
		return "请求来源或 CSRF 校验失败"
	case "not_found":
		return "资源不存在"
	case "version_conflict":
		return "版本冲突"
	case "unsupported_media_type":
		return "不支持的媒体类型"
	case "github_repository_invalid":
		return "GitHub 仓库无效"
	case "github_verification_unavailable":
		return "GitHub 仓库验证不可用"
	case "upload_too_large":
		return "上传文件过大"
	case "upload_type_invalid":
		return "上传文件类型无效"
	case "image_dimensions_exceeded":
		return "图片尺寸超限"
	case "invalid_media_reference":
		return "媒体引用无效"
	case "image_alt_required":
		return "图片说明必填"
	case "image_alt_invalid":
		return "图片说明无效"
	case "referenced_media_not_found":
		return "引用的媒体不存在"
	case "internal_error":
		return "服务器内部错误"
	case "rate_limited":
		return "请求过于频繁"
	case "dependency_unavailable":
		return "依赖服务不可用"
	case "save_failed":
		return "保存失败"
	default:
		return "请求无效"
	}
}

func isPresent(fields map[string]json.RawMessage, name string) bool {
	_, ok := fields[name]
	return ok
}

func decodeObject(w http.ResponseWriter, r *http.Request, limit int64, allowed map[string]struct{}, destination any) (map[string]json.RawMessage, error) {
	if !isJSONContentType(r) {
		return nil, errUnsupportedMediaType
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		if isMaxBytesError(err) {
			return nil, errRequestTooLarge
		}
		return nil, errInvalidJSON
	}
	if fields == nil {
		return nil, errInvalidJSON
	}
	for name := range fields {
		if _, ok := allowed[name]; !ok {
			return nil, errInvalidJSON
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil && isMaxBytesError(err) {
			return nil, errRequestTooLarge
		}
		return nil, errTrailingJSON
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, errInvalidJSON
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		return nil, errInvalidJSON
	}
	return fields, nil
}

func isJSONContentType(r *http.Request) bool {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func isMaxBytesError(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

// loginLimiter counts only failed credential checks. Its key deliberately
// uses RemoteAddr's host and never trusts forwarding headers.
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]failureEntry
	now     func() time.Time
}

type failureEntry struct {
	started time.Time
	count   int
}

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{entries: make(map[string]failureEntry), now: now}
}

func (limiter *loginLimiter) blocked(key string) (int64, bool) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	entry, ok := limiter.entries[key]
	if !ok {
		return 0, false
	}
	now := limiter.now().UTC()
	if !now.Before(entry.started.Add(loginFailureWindow)) {
		delete(limiter.entries, key)
		return 0, false
	}
	if entry.count < loginFailureLimit {
		return 0, false
	}
	return retryAfterSeconds(entry.started.Add(loginFailureWindow), now), true
}

func (limiter *loginLimiter) failure(key string) (int64, bool) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now().UTC()
	entry, ok := limiter.entries[key]
	if !ok || !now.Before(entry.started.Add(loginFailureWindow)) {
		entry = failureEntry{started: now}
	}
	entry.count++
	limiter.entries[key] = entry
	if entry.count < loginFailureLimit+1 {
		return 0, false
	}
	return retryAfterSeconds(entry.started.Add(loginFailureWindow), now), true
}

func (limiter *loginLimiter) success(key string) {
	limiter.mu.Lock()
	delete(limiter.entries, key)
	limiter.mu.Unlock()
}

func retryAfterSeconds(deadline, now time.Time) int64 {
	seconds := deadline.Sub(now).Seconds()
	if seconds < 1 {
		return 1
	}
	return int64(math.Ceil(seconds))
}

func limiterKey(remoteAddr, username string) string {
	host := remoteAddr
	if parsedHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsedHost
	} else if parsedIP := net.ParseIP(remoteAddr); parsedIP != nil {
		host = parsedIP.String()
	}
	return host + "\x00" + strings.ToLower(strings.TrimSpace(username))
}
