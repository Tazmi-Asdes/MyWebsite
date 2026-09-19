package article

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"mywebsite/internal/database/dbgen"
)

type adminFakeStore struct {
	*fakeStore
	listParams  dbgen.ListAdminArticlesParams
	countParams dbgen.CountAdminArticlesParams
	listRows    []dbgen.ListAdminArticlesRow
	count       int64
}

func (s *adminFakeStore) ListAdminArticles(_ context.Context, params dbgen.ListAdminArticlesParams) ([]dbgen.ListAdminArticlesRow, error) {
	s.listParams = params
	return s.listRows, nil
}

func (s *adminFakeStore) CountAdminArticles(_ context.Context, params dbgen.CountAdminArticlesParams) (int64, error) {
	s.countParams = params
	return s.count, nil
}

func TestListAdminUsesSharedFiltersAndMapsNullableUTCFields(t *testing.T) {
	publishedAt := time.Date(2026, 9, 19, 3, 4, 5, 0, time.FixedZone("CST", 8*60*60))
	createdAt := publishedAt.Add(-time.Hour)
	updatedAt := publishedAt.Add(time.Hour)
	store := &adminFakeStore{
		fakeStore: &fakeStore{},
		listRows: []dbgen.ListAdminArticlesRow{{
			ID:               7,
			PublicUlid:       sql.NullString{String: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Valid: true},
			Title:            "A title",
			Status:           "published",
			FirstPublishedAt: sql.NullTime{Time: publishedAt, Valid: true},
			Version:          3,
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
		}},
		count: 1,
	}
	service := NewService(store, &fakeRenderer{})
	status := StatusPublished
	query := `  100%_\\  `
	items, err := service.ListAdmin(context.Background(), &status, &query, 10, 20)
	if err != nil {
		t.Fatalf("ListAdmin() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != 7 || items[0].PublicULID == nil || items[0].Status != StatusPublished {
		t.Fatalf("ListAdmin() items = %+v", items)
	}
	if items[0].FirstPublishedAt == nil || items[0].FirstPublishedAt.Location() != time.UTC || items[0].CreatedAt.Location() != time.UTC || items[0].UpdatedAt.Location() != time.UTC {
		t.Fatalf("ListAdmin() did not map times to UTC: %+v", items[0])
	}
	if !store.listParams.StatusFilter.Valid || store.listParams.StatusFilter.String != "published" {
		t.Fatalf("status filter = %+v", store.listParams.StatusFilter)
	}
	if got, want := store.listParams.TitlePattern.String, `%100\%\_\\\\%`; got != want {
		t.Fatalf("title pattern = %q, want %q", got, want)
	}
	if store.listParams.Limit != 10 || store.listParams.Offset != 20 {
		t.Fatalf("pagination params = %+v", store.listParams)
	}

	total, err := service.CountAdmin(context.Background(), &status, &query)
	if err != nil || total != 1 {
		t.Fatalf("CountAdmin() = %d, %v", total, err)
	}
	if store.countParams.StatusFilter != store.listParams.StatusFilter || store.countParams.TitlePattern != store.listParams.TitlePattern {
		t.Fatalf("List/Count filters differ: list=%+v count=%+v", store.listParams, store.countParams)
	}
}

func TestListAdminNoFiltersAndValidation(t *testing.T) {
	store := &adminFakeStore{fakeStore: &fakeStore{}}
	service := NewService(store, &fakeRenderer{})
	if _, err := service.ListAdmin(context.Background(), nil, nil, 5, 0); err != nil {
		t.Fatalf("ListAdmin(no filters) error = %v", err)
	}
	if store.listParams.StatusFilter.Valid || store.listParams.TitlePattern.Valid {
		t.Fatalf("no filters = %+v", store.listParams)
	}
	empty := "  \t"
	if _, err := service.CountAdmin(context.Background(), nil, &empty); err != nil {
		t.Fatalf("CountAdmin(empty query) error = %v", err)
	}
	if store.countParams.TitlePattern.Valid {
		t.Fatalf("empty query filter = %+v", store.countParams.TitlePattern)
	}

	badStatus := Status("archived")
	if _, err := service.ListAdmin(context.Background(), &badStatus, nil, 5, 0); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v", err)
	}
	tooLong := "界" + string(make([]rune, 200))
	if _, err := service.CountAdmin(context.Background(), nil, &tooLong); !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("invalid search error = %v", err)
	}
}

func TestAdminListRequiresOptionalStoreCapability(t *testing.T) {
	service := NewService(&fakeStore{}, &fakeRenderer{})
	if _, err := service.ListAdmin(context.Background(), nil, nil, 1, 0); !errors.Is(err, ErrStoreCapability) {
		t.Fatalf("ListAdmin() error = %v", err)
	}
	if _, err := service.CountAdmin(context.Background(), nil, nil); !errors.Is(err, ErrStoreCapability) {
		t.Fatalf("CountAdmin() error = %v", err)
	}
}
