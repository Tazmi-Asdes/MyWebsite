package article

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"mywebsite/internal/database/dbgen"
)

var (
	ErrInvalidStatus = errors.New("article status filter must be draft or published")
	ErrInvalidSearch = errors.New("article search query must be at most 200 Unicode characters")
)

// AdminArticleSummary is the nullable/UTC-mapped domain form of the
// generated administrative list row.
type AdminArticleSummary struct {
	ID               uint64
	PublicULID       *string
	Title            string
	Status           Status
	FirstPublishedAt *time.Time
	Version          uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type adminArticleStore interface {
	ListAdminArticles(context.Context, dbgen.ListAdminArticlesParams) ([]dbgen.ListAdminArticlesRow, error)
	CountAdminArticles(context.Context, dbgen.CountAdminArticlesParams) (int64, error)
}

type adminFilters struct {
	StatusFilter sql.NullString
	TitlePattern sql.NullString
}

func (service *Service) ListAdmin(ctx context.Context, statusFilter *Status, query *string, limit, offset int32) ([]AdminArticleSummary, error) {
	store, ok := service.store.(adminArticleStore)
	if !ok {
		return nil, ErrStoreCapability
	}
	filters, err := makeAdminFilters(statusFilter, query)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListAdminArticles(ctx, dbgen.ListAdminArticlesParams{
		StatusFilter: filters.StatusFilter,
		TitlePattern: filters.TitlePattern,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, err
	}
	items := make([]AdminArticleSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, fromDBAdminArticle(row))
	}
	return items, nil
}

func (service *Service) CountAdmin(ctx context.Context, statusFilter *Status, query *string) (int64, error) {
	store, ok := service.store.(adminArticleStore)
	if !ok {
		return 0, ErrStoreCapability
	}
	filters, err := makeAdminFilters(statusFilter, query)
	if err != nil {
		return 0, err
	}
	return store.CountAdminArticles(ctx, dbgen.CountAdminArticlesParams{
		StatusFilter: filters.StatusFilter,
		TitlePattern: filters.TitlePattern,
	})
}

func makeAdminFilters(statusFilter *Status, query *string) (adminFilters, error) {
	filters := adminFilters{}
	if statusFilter != nil {
		switch *statusFilter {
		case StatusDraft, StatusPublished:
			filters.StatusFilter = sql.NullString{String: string(*statusFilter), Valid: true}
		default:
			return adminFilters{}, ErrInvalidStatus
		}
	}
	if query == nil {
		return filters, nil
	}
	value := strings.TrimSpace(*query)
	if value == "" {
		return filters, nil
	}
	if utf8.RuneCountInString(value) > 200 {
		return adminFilters{}, ErrInvalidSearch
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	value = strings.ReplaceAll(value, "_", `\_`)
	filters.TitlePattern = sql.NullString{String: "%" + value + "%", Valid: true}
	return filters, nil
}

func fromDBAdminArticle(row dbgen.ListAdminArticlesRow) AdminArticleSummary {
	return AdminArticleSummary{
		ID:               row.ID,
		PublicULID:       nullableStringPtr(row.PublicUlid),
		Title:            row.Title,
		Status:           Status(row.Status),
		FirstPublishedAt: nullableTimePtr(row.FirstPublishedAt),
		Version:          row.Version,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}
