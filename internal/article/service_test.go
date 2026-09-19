package article

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mywebsite/internal/database/dbgen"
	"mywebsite/internal/markdown"
)

type testResult struct {
	id   int64
	rows int64
}

func (r testResult) LastInsertId() (int64, error) { return r.id, nil }
func (r testResult) RowsAffected() (int64, error) { return r.rows, nil }

type fakeRenderer struct {
	calls int
}

func (r *fakeRenderer) Render(string) (markdown.Derived, error) {
	r.calls++
	return markdown.Derived{
		HTML:            "<p>derived</p>",
		TOCJSON:         json.RawMessage(`{"items":[]}`),
		Preview:         "derived",
		RendererVersion: markdown.RendererVersion,
	}, nil
}

type fakeStore struct {
	row       dbgen.Article
	created   dbgen.CreateArticleParams
	updated   dbgen.UpdateArticleParams
	withdrawn dbgen.WithdrawArticleParams
	rows      int64
}

func (s *fakeStore) CreateArticle(_ context.Context, params dbgen.CreateArticleParams) (sql.Result, error) {
	s.created = params
	s.row = dbgen.Article{
		ID:              1,
		Title:           params.Title,
		BodyMarkdown:    params.BodyMarkdown,
		BodyHtml:        params.BodyHtml,
		TocJson:         params.TocJson,
		PreviewText:     params.PreviewText,
		RendererVersion: params.RendererVersion,
		Status:          "draft",
		Version:         1,
		CreatedAt:       params.CreatedAt,
		UpdatedAt:       params.UpdatedAt,
	}
	return testResult{id: 1}, nil
}

func (s *fakeStore) GetArticleByID(context.Context, uint64) (dbgen.Article, error) {
	return s.row, nil
}

func (s *fakeStore) UpdateArticle(_ context.Context, params dbgen.UpdateArticleParams) (sql.Result, error) {
	s.updated = params
	s.rows++
	s.row.ID = params.ID
	s.row.PublicUlid = params.PublicUlid
	s.row.Title = params.Title
	s.row.BodyMarkdown = params.BodyMarkdown
	s.row.BodyHtml = params.BodyHtml
	s.row.TocJson = params.TocJson
	s.row.PreviewText = params.PreviewText
	s.row.RendererVersion = params.RendererVersion
	s.row.Status = params.Status
	s.row.FirstPublishedAt = params.FirstPublishedAt
	s.row.Version = params.Version + 1
	s.row.UpdatedAt = params.UpdatedAt
	return testResult{rows: s.rows}, nil
}

func (s *fakeStore) WithdrawArticle(_ context.Context, params dbgen.WithdrawArticleParams) (sql.Result, error) {
	s.withdrawn = params
	s.row.Status = "draft"
	s.row.Version++
	s.row.UpdatedAt = params.UpdatedAt
	return testResult{rows: 1}, nil
}

func TestCreateDraftAllowsNilBody(t *testing.T) {
	store := &fakeStore{}
	renderer := &fakeRenderer{}
	service := NewService(store, renderer)
	got, err := service.CreateDraft(context.Background(), CreateDraftRequest{Title: "Draft"})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if got.ID != 1 || got.Status != StatusDraft || store.created.BodyMarkdown.Valid {
		t.Fatalf("created article = %+v, params = %+v", got, store.created)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0 for empty draft", renderer.calls)
	}
}

func TestCreateDraftPersistsDerivedFieldsAtVersionOne(t *testing.T) {
	store := &fakeStore{}
	renderer := &fakeRenderer{}
	service := NewService(store, renderer)
	body := "draft body"
	got, err := service.CreateDraft(context.Background(), CreateDraftRequest{Title: "Draft", BodyMarkdown: &body})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if renderer.calls != 1 || got.Version != 1 {
		t.Fatalf("renderer calls/version = %d/%d, want 1/1", renderer.calls, got.Version)
	}
	if !store.created.BodyHtml.Valid || !json.Valid(store.created.TocJson) || store.created.RendererVersion.String != markdown.RendererVersion {
		t.Fatalf("CreateArticle derived params = %+v", store.created)
	}
}

func TestPublishWithdrawAndVersionConflict(t *testing.T) {
	store := &fakeStore{row: dbgen.Article{
		ID:        1,
		Title:     "Draft",
		Status:    "draft",
		Version:   1,
		CreatedAt: time.Date(2026, 9, 19, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		UpdatedAt: time.Date(2026, 9, 19, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	}}
	renderer := &fakeRenderer{}
	clock := time.Date(2026, 9, 19, 12, 34, 56, 0, time.FixedZone("CST", 8*60*60))
	service := NewService(store, renderer,
		WithClock(func() time.Time { return clock }),
		WithULIDGenerator(func() (string, error) { return "01ARZ3NDEKTSV4RRFFQ69G5FAV", nil }),
	)
	body := "published body"
	got, err := service.Publish(context.Background(), 1, PublishRequest{Title: "Published", BodyMarkdown: &body, Version: 1})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if got.Status != StatusPublished || got.PublicULID == nil || *got.PublicULID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("published article = %+v", got)
	}
	if store.updated.FirstPublishedAt.Time.Location() != time.UTC {
		t.Fatalf("first published time is not UTC: %v", store.updated.FirstPublishedAt.Time.Location())
	}
	firstPublished := store.updated.FirstPublishedAt

	got, err = service.Withdraw(context.Background(), 1, WithdrawRequest{Version: 2})
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if got.Status != StatusDraft || store.withdrawn.Version != 2 {
		t.Fatalf("withdrawn article = %+v, params = %+v", got, store.withdrawn)
	}
	if store.row.PublicUlid.String != "01ARZ3NDEKTSV4RRFFQ69G5FAV" || store.row.FirstPublishedAt != firstPublished {
		t.Fatalf("withdraw cleared publication fields: %+v", store.row)
	}

	if _, err := service.Save(context.Background(), 1, SaveRequest{Title: "stale", BodyMarkdown: &body, Version: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Save() error = %v, want ErrConflict", err)
	}
}

func TestPublishedArticleRejectsEmptySave(t *testing.T) {
	store := &fakeStore{row: dbgen.Article{ID: 1, Status: "published", Version: 4}}
	service := NewService(store, &fakeRenderer{})
	body := " \n\t"
	if _, err := service.Save(context.Background(), 1, SaveRequest{Title: "Published", BodyMarkdown: &body, Version: 4}); !errors.Is(err, ErrPublishedBodyRequired) {
		t.Fatalf("Save() error = %v, want ErrPublishedBodyRequired", err)
	}
	if store.rows != 0 {
		t.Fatalf("UpdateArticle calls = %d, want 0", store.rows)
	}
}
