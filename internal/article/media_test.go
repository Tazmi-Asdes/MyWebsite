package article

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"mywebsite/internal/database/dbgen"
	"mywebsite/internal/markdown"
)

type mediaRenderer struct {
	references []markdown.MediaReference
}

func (r *mediaRenderer) Render(string) (markdown.Derived, error) {
	return markdown.Derived{
		HTML:            "<p>derived</p>",
		TOCJSON:         json.RawMessage(`{"items":[]}`),
		Preview:         "derived",
		RendererVersion: markdown.RendererVersion,
		MediaReferences: append([]markdown.MediaReference(nil), r.references...),
	}, nil
}

type mediaAtomicStore struct {
	*fakeStore
	createdReferences []markdown.MediaReference
	updatedReferences []markdown.MediaReference
	updatedCalled     bool
	createErr         error
	updateErr         error
}

func (s *mediaAtomicStore) CreateArticleWithMedia(ctx context.Context, params dbgen.CreateArticleParams, references []markdown.MediaReference) (sql.Result, error) {
	s.createdReferences = append([]markdown.MediaReference(nil), references...)
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.fakeStore.CreateArticle(ctx, params)
}

func (s *mediaAtomicStore) UpdateArticleWithMedia(ctx context.Context, params dbgen.UpdateArticleParams, references []markdown.MediaReference) (sql.Result, error) {
	s.updatedCalled = true
	s.updatedReferences = append([]markdown.MediaReference(nil), references...)
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.fakeStore.UpdateArticle(ctx, params)
}

func TestServiceAtomicStoreReceivesCreateAndUpdateReferences(t *testing.T) {
	asset := markdown.MediaReference{AssetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", AltText: "cover"}
	store := &mediaAtomicStore{fakeStore: &fakeStore{}}
	service := NewService(store, &mediaRenderer{references: []markdown.MediaReference{asset}})
	if _, err := service.CreateDraft(context.Background(), CreateDraftRequest{Title: "Draft", BodyMarkdown: stringPtr("body")}); err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if len(store.createdReferences) != 1 || store.createdReferences[0] != asset {
		t.Fatalf("created references = %+v, want %+v", store.createdReferences, asset)
	}

	store.row.Status = "draft"
	store.row.Version = 1
	if _, err := service.Save(context.Background(), 1, SaveRequest{Title: "Draft", BodyMarkdown: stringPtr("body"), Version: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(store.updatedReferences) != 1 || store.updatedReferences[0] != asset {
		t.Fatalf("updated references = %+v, want %+v", store.updatedReferences, asset)
	}
}

func TestServiceAtomicStoreReceivesEmptyReferencesToClearOldRows(t *testing.T) {
	store := &mediaAtomicStore{fakeStore: &fakeStore{row: dbgen.Article{ID: 1, Status: "draft", Version: 3}}}
	service := NewService(store, &mediaRenderer{})
	if _, err := service.Save(context.Background(), 1, SaveRequest{Title: "Draft", BodyMarkdown: stringPtr("body"), Version: 3}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !store.updatedCalled {
		t.Fatalf("atomic update was not called")
	}
	if len(store.updatedReferences) != 0 {
		t.Fatalf("updated references = %+v, want empty", store.updatedReferences)
	}
}

func TestServiceRequiresAtomicStoreForMediaReferences(t *testing.T) {
	service := NewService(&fakeStore{}, &mediaRenderer{references: []markdown.MediaReference{{AssetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", AltText: "cover"}}})
	if _, err := service.CreateDraft(context.Background(), CreateDraftRequest{Title: "Draft", BodyMarkdown: stringPtr("body")}); !errors.Is(err, ErrStoreCapability) {
		t.Fatalf("CreateDraft() error = %v, want ErrStoreCapability", err)
	}
}

func TestServicePropagatesAtomicMediaNotFound(t *testing.T) {
	store := &mediaAtomicStore{fakeStore: &fakeStore{}, createErr: ErrReferencedMediaNotFound}
	service := NewService(store, &mediaRenderer{references: []markdown.MediaReference{{AssetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", AltText: "cover"}}})
	if _, err := service.CreateDraft(context.Background(), CreateDraftRequest{Title: "Draft", BodyMarkdown: stringPtr("body")}); !errors.Is(err, ErrReferencedMediaNotFound) {
		t.Fatalf("CreateDraft() error = %v, want ErrReferencedMediaNotFound", err)
	}
}

func stringPtr(value string) *string {
	return &value
}
