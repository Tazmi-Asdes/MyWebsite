// Package article contains article validation, Markdown derivation and the
// optimistic-locking domain operations used by the HTTP layer.
package article

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"mywebsite/internal/database/dbgen"
	"mywebsite/internal/markdown"
)

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
)

var (
	ErrInvalidTitle          = errors.New("article title must be 1-200 Unicode characters without leading or trailing whitespace")
	ErrBodyTooLarge          = errors.New("article body exceeds 2 MiB")
	ErrPublishedBodyRequired = errors.New("published article body must contain non-whitespace text")
	ErrConflict              = errors.New("article version conflict")
	ErrNotPublished          = errors.New("article is not published")
	ErrStoreCapability       = errors.New("article store does not support the requested operation")
	ErrInvalidDerivedTOC     = errors.New("article renderer returned invalid toc JSON")
	ErrRendererUnavailable   = errors.New("article Markdown renderer is not configured")
)

// Compatibility aliases keep the domain vocabulary explicit at call sites
// that prefer the longer names.
var (
	ErrVersionConflict = ErrConflict
	ErrInvalidBody     = ErrPublishedBodyRequired
)

// Status is deliberately separate from HTTP status codes.
type Status string

// Article is the domain representation of an article. Nullable database
// columns remain pointers so a draft with no derived representation is not
// confused with an empty derived value.
type Article struct {
	ID               uint64
	PublicULID       *string
	Title            string
	BodyMarkdown     *string
	BodyHTML         *string
	TOCJSON          json.RawMessage
	PreviewText      *string
	RendererVersion  *string
	Status           Status
	FirstPublishedAt *time.Time
	Version          uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PublishedArticle is the row shape returned by the public article list.
// It intentionally omits the full body.
type PublishedArticle struct {
	ID               uint64
	PublicULID       *string
	Title            string
	PreviewText      *string
	Status           Status
	FirstPublishedAt *time.Time
	Version          uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateDraftRequest struct {
	Title        string
	BodyMarkdown *string
}

type SaveRequest struct {
	Title        string
	BodyMarkdown *string
	Version      uint64
}

type PublishRequest struct {
	Title        string
	BodyMarkdown *string
	Version      uint64
}

type WithdrawRequest struct {
	Version uint64
}

// Common alternate names used by adapters are aliases, not separate request
// contracts.
type CreateDraftInput = CreateDraftRequest
type SaveInput = SaveRequest
type PublishInput = PublishRequest

// Store is the minimum persistence contract needed by draft/save/publish/
// withdraw. Public query methods use the optional PublishedStore extension,
// so a focused fake store can test mutations without implementing list code.
type Store interface {
	CreateArticle(context.Context, dbgen.CreateArticleParams) (sql.Result, error)
	GetArticleByID(context.Context, uint64) (dbgen.Article, error)
	UpdateArticle(context.Context, dbgen.UpdateArticleParams) (sql.Result, error)
	WithdrawArticle(context.Context, dbgen.WithdrawArticleParams) (sql.Result, error)
}

type PublishedStore interface {
	Store
	GetPublishedArticleByULID(context.Context, sql.NullString) (dbgen.Article, error)
	ListPublishedArticles(context.Context, dbgen.ListPublishedArticlesParams) ([]dbgen.ListPublishedArticlesRow, error)
	CountPublishedArticles(context.Context) (int64, error)
}

// ULIDGenerator is injected in tests and defaults to a crypto-rand generator.
// The default implementation uses the service clock for the timestamp.
type ULIDGenerator func() (string, error)

type Service struct {
	store    Store
	renderer markdown.Renderer
	now      func() time.Time
	newULID  ULIDGenerator
}

type Option func(*Service)

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func WithNow(now func() time.Time) Option { return WithClock(now) }

func WithULIDGenerator(generator ULIDGenerator) Option {
	return func(service *Service) {
		if generator != nil {
			service.newULID = generator
		}
	}
}

func NewService(store Store, renderer markdown.Renderer, options ...Option) *Service {
	service := &Service{
		store:    store,
		renderer: renderer,
		now:      func() time.Time { return time.Now().UTC() },
	}
	service.newULID = func() (string, error) {
		id, err := ulid.New(ulid.Timestamp(service.now().UTC()), rand.Reader)
		if err != nil {
			return "", err
		}
		return id.String(), nil
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// New is a short constructor alias for NewService.
func New(store Store, renderer markdown.Renderer, options ...Option) *Service {
	return NewService(store, renderer, options...)
}

func (service *Service) CreateDraft(ctx context.Context, request CreateDraftRequest) (Article, error) {
	if err := validateTitle(request.Title); err != nil {
		return Article{}, err
	}
	body := request.BodyMarkdown
	if err := validateBodySize(body); err != nil {
		return Article{}, err
	}
	derived, err := service.derive(body, false)
	if err != nil {
		return Article{}, err
	}
	now := service.now().UTC()
	result, err := service.store.CreateArticle(ctx, dbgen.CreateArticleParams{
		Title:           request.Title,
		BodyMarkdown:    nullableString(body),
		BodyHtml:        nullableDerivedString(derived.HTML, body),
		TocJson:         nullableTOC(derived.TOCJSON, body),
		PreviewText:     nullableDerivedString(derived.Preview, body),
		RendererVersion: nullableDerivedString(derived.RendererVersion, body),
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return Article{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Article{}, err
	}
	_ = derived // CreateArticle persists the derived fields in its Stage 1 contract.
	return service.getDomainArticle(ctx, uint64(id))
}

func (service *Service) Save(ctx context.Context, id uint64, request SaveRequest) (Article, error) {
	current, err := service.store.GetArticleByID(ctx, id)
	if err != nil {
		return Article{}, err
	}
	if current.Version != request.Version {
		return Article{}, ErrConflict
	}
	if err := validateTitle(request.Title); err != nil {
		return Article{}, err
	}
	if err := validateBodySize(request.BodyMarkdown); err != nil {
		return Article{}, err
	}
	if current.Status == string(StatusPublished) && (request.BodyMarkdown == nil || strings.TrimSpace(*request.BodyMarkdown) == "") {
		return Article{}, ErrPublishedBodyRequired
	}
	derived, err := service.derive(request.BodyMarkdown, false)
	if err != nil {
		return Article{}, err
	}
	now := service.now().UTC()
	params, err := service.updateParams(current, request.Title, request.BodyMarkdown, derived, current.Status, now)
	if err != nil {
		return Article{}, err
	}
	result, err := service.store.UpdateArticle(ctx, params)
	if err != nil {
		return Article{}, err
	}
	if err := requireRowsAffected(result); err != nil {
		return Article{}, err
	}
	return service.getDomainArticle(ctx, id)
}

func (service *Service) Publish(ctx context.Context, id uint64, request PublishRequest) (Article, error) {
	current, err := service.store.GetArticleByID(ctx, id)
	if err != nil {
		return Article{}, err
	}
	if current.Version != request.Version {
		return Article{}, ErrConflict
	}
	if err := validateTitle(request.Title); err != nil {
		return Article{}, err
	}
	if err := validateBodySize(request.BodyMarkdown); err != nil {
		return Article{}, err
	}
	if request.BodyMarkdown == nil || strings.TrimSpace(*request.BodyMarkdown) == "" {
		return Article{}, ErrPublishedBodyRequired
	}
	derived, err := service.derive(request.BodyMarkdown, true)
	if err != nil {
		return Article{}, err
	}
	now := service.now().UTC()
	publicULID := current.PublicUlid
	firstPublishedAt := current.FirstPublishedAt
	if !publicULID.Valid || !firstPublishedAt.Valid {
		value, generateErr := service.newULID()
		if generateErr != nil {
			return Article{}, generateErr
		}
		publicULID = sql.NullString{String: value, Valid: true}
		firstPublishedAt = sql.NullTime{Time: now, Valid: true}
	}
	params, err := service.updateParams(current, request.Title, request.BodyMarkdown, derived, string(StatusPublished), now)
	if err != nil {
		return Article{}, err
	}
	params.PublicUlid = publicULID
	params.FirstPublishedAt = firstPublishedAt
	params.Status = string(StatusPublished)
	result, err := service.store.UpdateArticle(ctx, params)
	if err != nil {
		return Article{}, err
	}
	if err := requireRowsAffected(result); err != nil {
		return Article{}, err
	}
	return service.getDomainArticle(ctx, id)
}

func (service *Service) Withdraw(ctx context.Context, id uint64, request WithdrawRequest) (Article, error) {
	current, err := service.store.GetArticleByID(ctx, id)
	if err != nil {
		return Article{}, err
	}
	if current.Version != request.Version {
		return Article{}, ErrConflict
	}
	if current.Status != string(StatusPublished) {
		return Article{}, ErrNotPublished
	}
	result, err := service.store.WithdrawArticle(ctx, dbgen.WithdrawArticleParams{
		UpdatedAt: service.now().UTC(),
		ID:        id,
		Version:   request.Version,
	})
	if err != nil {
		return Article{}, err
	}
	if err := requireRowsAffected(result); err != nil {
		return Article{}, err
	}
	return service.getDomainArticle(ctx, id)
}

func (service *Service) GetByID(ctx context.Context, id uint64) (Article, error) {
	return service.getDomainArticle(ctx, id)
}

func (service *Service) GetPublishedByULID(ctx context.Context, publicULID string) (Article, error) {
	store, ok := service.store.(interface {
		GetPublishedArticleByULID(context.Context, sql.NullString) (dbgen.Article, error)
	})
	if !ok {
		return Article{}, ErrStoreCapability
	}
	row, err := store.GetPublishedArticleByULID(ctx, sql.NullString{String: publicULID, Valid: true})
	if err != nil {
		return Article{}, err
	}
	return fromDBArticle(row), nil
}

func (service *Service) ListPublished(ctx context.Context, limit, offset int32) ([]PublishedArticle, error) {
	store, ok := service.store.(interface {
		ListPublishedArticles(context.Context, dbgen.ListPublishedArticlesParams) ([]dbgen.ListPublishedArticlesRow, error)
	})
	if !ok {
		return nil, ErrStoreCapability
	}
	rows, err := store.ListPublishedArticles(ctx, dbgen.ListPublishedArticlesParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, err
	}
	items := make([]PublishedArticle, 0, len(rows))
	for _, row := range rows {
		items = append(items, fromDBPublishedArticle(row))
	}
	return items, nil
}

func (service *Service) CountPublished(ctx context.Context) (int64, error) {
	store, ok := service.store.(interface {
		CountPublishedArticles(context.Context) (int64, error)
	})
	if !ok {
		return 0, ErrStoreCapability
	}
	return store.CountPublishedArticles(ctx)
}

func (service *Service) getDomainArticle(ctx context.Context, id uint64) (Article, error) {
	row, err := service.store.GetArticleByID(ctx, id)
	if err != nil {
		return Article{}, err
	}
	return fromDBArticle(row), nil
}

func (service *Service) derive(body *string, requireNonEmpty bool) (markdown.Derived, error) {
	if body == nil || *body == "" {
		if requireNonEmpty {
			return markdown.Derived{}, ErrPublishedBodyRequired
		}
		return markdown.Derived{}, nil
	}
	if service.renderer == nil {
		return markdown.Derived{}, ErrRendererUnavailable
	}
	derived, err := service.renderer.Render(*body)
	if err != nil {
		return markdown.Derived{}, err
	}
	if len(derived.TOCJSON) == 0 || !json.Valid(derived.TOCJSON) {
		return markdown.Derived{}, ErrInvalidDerivedTOC
	}
	if derived.RendererVersion == "" {
		derived.RendererVersion = markdown.RendererVersion
	}
	return derived, nil
}

func (service *Service) updateParams(current dbgen.Article, title string, body *string, derived markdown.Derived, status string, now time.Time) (dbgen.UpdateArticleParams, error) {
	if status == "" {
		status = current.Status
	}
	return dbgen.UpdateArticleParams{
		PublicUlid:       current.PublicUlid,
		Title:            title,
		BodyMarkdown:     nullableString(body),
		BodyHtml:         nullableDerivedString(derived.HTML, body),
		TocJson:          nullableTOC(derived.TOCJSON, body),
		PreviewText:      nullableDerivedString(derived.Preview, body),
		RendererVersion:  nullableDerivedString(derived.RendererVersion, body),
		Status:           status,
		FirstPublishedAt: current.FirstPublishedAt,
		UpdatedAt:        now.UTC(),
		ID:               current.ID,
		Version:          current.Version,
	}, nil
}

func validateTitle(title string) error {
	if title == "" || title != strings.TrimSpace(title) || utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 200 {
		return ErrInvalidTitle
	}
	return nil
}

func validateBodySize(body *string) error {
	if body != nil && len([]byte(*body)) > markdown.MaxMarkdownBytes {
		return ErrBodyTooLarge
	}
	return nil
}

func nullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullableDerivedString(value string, body *string) sql.NullString {
	if body == nil || *body == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableTOC(value json.RawMessage, body *string) json.RawMessage {
	if body == nil || *body == "" {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func requireRowsAffected(result sql.Result) error {
	if result == nil {
		return ErrConflict
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConflict
	}
	return nil
}

func fromDBArticle(row dbgen.Article) Article {
	return Article{
		ID:               row.ID,
		PublicULID:       nullableStringPtr(row.PublicUlid),
		Title:            row.Title,
		BodyMarkdown:     nullableStringPtr(row.BodyMarkdown),
		BodyHTML:         nullableStringPtr(row.BodyHtml),
		TOCJSON:          append(json.RawMessage(nil), row.TocJson...),
		PreviewText:      nullableStringPtr(row.PreviewText),
		RendererVersion:  nullableStringPtr(row.RendererVersion),
		Status:           Status(row.Status),
		FirstPublishedAt: nullableTimePtr(row.FirstPublishedAt),
		Version:          row.Version,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}

func fromDBPublishedArticle(row dbgen.ListPublishedArticlesRow) PublishedArticle {
	return PublishedArticle{
		ID:               row.ID,
		PublicULID:       nullableStringPtr(row.PublicUlid),
		Title:            row.Title,
		PreviewText:      nullableStringPtr(row.PreviewText),
		Status:           Status(row.Status),
		FirstPublishedAt: nullableTimePtr(row.FirstPublishedAt),
		Version:          row.Version,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func (service Service) String() string {
	return fmt.Sprintf("article.Service(renderer=%t)", service.renderer != nil)
}
