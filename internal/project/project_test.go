package project

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProjectRepository struct {
	projects     map[uint64]Project
	orderVersion uint64
	nextID       uint64
	media        map[string]bool
	lastFields   ProjectFields
	verifyCalls  int
}

func newFakeProjectRepository() *fakeProjectRepository {
	return &fakeProjectRepository{
		projects:     make(map[uint64]Project),
		orderVersion: 1,
		nextID:       1,
		media:        map[string]bool{"01ARZ3NDEKTSV4RRFFQ69G5FAV": true},
	}
}

func (repository *fakeProjectRepository) CreateProject(_ context.Context, name string, now time.Time) (Project, error) {
	id := repository.nextID
	repository.nextID++
	project := Project{ID: id, Name: name, Status: StatusHidden, Version: 1, CreatedAt: now, UpdatedAt: now}
	repository.projects[id] = project
	return project, nil
}

func (repository *fakeProjectRepository) GetProject(_ context.Context, id uint64) (Project, error) {
	project, ok := repository.projects[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (repository *fakeProjectRepository) UpdateProject(_ context.Context, id uint64, fields ProjectFields, version uint64, now time.Time) (Project, error) {
	project, err := repository.GetProject(context.Background(), id)
	if err != nil {
		return Project{}, err
	}
	if project.Version != version {
		return Project{}, ErrConflict
	}
	project.Name, project.GitHubURL, project.ImageAssetID = fields.Name, fields.GitHubURL, fields.ImageAssetID
	project.Version++
	project.UpdatedAt = now
	repository.projects[id] = project
	return project, nil
}

func (repository *fakeProjectRepository) ListProjectGroups(context.Context) (ProjectGroups, error) {
	return ProjectGroups{OrderVersion: repository.orderVersion}, nil
}

func (repository *fakeProjectRepository) ListAllPublicProjects(context.Context) ([]Project, error) {
	return repository.publicProjects(), nil
}

func (repository *fakeProjectRepository) ListPublicProjects(_ context.Context, limit, offset int32) ([]Project, error) {
	items := repository.publicProjects()
	start := int(offset)
	if start > len(items) {
		start = len(items)
	}
	end := start + int(limit)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], nil
}

func (repository *fakeProjectRepository) CountPublicProjects(context.Context) (int64, error) {
	return int64(len(repository.publicProjects())), nil
}

func (repository *fakeProjectRepository) MediaExists(_ context.Context, id string) (bool, error) {
	return repository.media[id], nil
}

func (repository *fakeProjectRepository) PublishProject(_ context.Context, id uint64, fields ProjectFields, projectVersion, orderVersion uint64, now time.Time) (PublishResult, error) {
	if repository.orderVersion != orderVersion {
		return PublishResult{}, ErrConflict
	}
	project, err := repository.GetProject(context.Background(), id)
	if err != nil {
		return PublishResult{}, err
	}
	if project.Version != projectVersion || project.Status != StatusHidden {
		return PublishResult{}, ErrConflict
	}
	project.Name, project.GitHubURL, project.ImageAssetID = fields.Name, fields.GitHubURL, fields.ImageAssetID
	project.Status = StatusPublic
	project.SortOrder = int64PointerValue(int64(len(repository.publicProjects()) + 1))
	project.Version++
	project.UpdatedAt = now
	repository.projects[id] = project
	repository.lastFields = fields
	repository.orderVersion++
	return PublishResult{Project: project, OrderVersion: repository.orderVersion}, nil
}

func (repository *fakeProjectRepository) HideProject(_ context.Context, id uint64, projectVersion, orderVersion uint64, now time.Time) (HideResult, error) {
	if repository.orderVersion != orderVersion {
		return HideResult{}, ErrConflict
	}
	project, err := repository.GetProject(context.Background(), id)
	if err != nil {
		return HideResult{}, err
	}
	if project.Version != projectVersion || project.Status != StatusPublic {
		return HideResult{}, ErrConflict
	}
	project.Status, project.SortOrder, project.Version, project.UpdatedAt = StatusHidden, nil, project.Version+1, now
	repository.projects[id] = project
	remaining := repository.publicProjects()
	for index := range remaining {
		remaining[index].SortOrder = int64PointerValue(int64(index + 1))
		repository.projects[remaining[index].ID] = remaining[index]
	}
	repository.orderVersion++
	return HideResult{Project: project, OrderVersion: repository.orderVersion}, nil
}

func (repository *fakeProjectRepository) ReorderProjects(_ context.Context, ids []uint64, orderVersion uint64, now time.Time) (uint64, error) {
	if repository.orderVersion != orderVersion {
		return 0, ErrConflict
	}
	current := repository.publicProjects()
	if err := sameIDSet(projectIDs(current), ids); err != nil {
		return 0, err
	}
	for index, id := range ids {
		project := repository.projects[id]
		project.SortOrder = int64PointerValue(int64(index + 1))
		project.UpdatedAt = now
		repository.projects[id] = project
	}
	repository.orderVersion++
	return repository.orderVersion, nil
}

func (repository *fakeProjectRepository) publicProjects() []Project {
	items := make([]Project, 0)
	for _, project := range repository.projects {
		if project.Status == StatusPublic {
			items = append(items, project)
		}
	}
	for index := 0; index < len(items); index++ {
		for next := index + 1; next < len(items); next++ {
			if sortValue(items[next]) < sortValue(items[index]) {
				items[index], items[next] = items[next], items[index]
			}
		}
	}
	return items
}

func projectIDs(items []Project) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func sortValue(project Project) int64 {
	if project.SortOrder == nil {
		return 0
	}
	return *project.SortOrder
}

func int64PointerValue(value int64) *int64 { return &value }

func fixedProjectClock() time.Time {
	return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
}

func TestServicePublishNormalizesAndPublishes(t *testing.T) {
	repository := newFakeProjectRepository()
	created, err := repository.CreateProject(context.Background(), "隐藏项目", fixedProjectClock())
	if err != nil {
		t.Fatal(err)
	}
	verifier := GitHubVerifyFunc(func(_ context.Context, value string) error {
		if value != "https://github.com/acme/demo.git" {
			t.Fatalf("verified URL = %q", value)
		}
		return nil
	})
	service := NewService(repository, verifier, WithClock(fixedProjectClock))
	urlValue := " https://github.com/acme/demo.git/ "
	imageID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	published, orderVersion, err := service.Publish(context.Background(), created.ID, PublishRequest{
		Name:         "  新项目  ",
		GitHubURL:    &urlValue,
		ImageAssetID: &imageID,
		Version:      created.Version,
		OrderVersion: repository.orderVersion,
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Status != StatusPublic || published.Name != "新项目" || orderVersion != 2 {
		t.Fatalf("published = %+v, order version = %d", published, orderVersion)
	}
	if repository.lastFields.GitHubURL == nil || *repository.lastFields.GitHubURL != "https://github.com/acme/demo.git" {
		t.Fatalf("persisted URL = %+v", repository.lastFields.GitHubURL)
	}
}

func TestServiceHideCompactsPublicOrder(t *testing.T) {
	repository := newFakeProjectRepository()
	for id := uint64(1); id <= 3; id++ {
		repository.projects[id] = Project{ID: id, Name: "项目", Status: StatusPublic, SortOrder: int64PointerValue(int64(id)), Version: 1}
	}
	repository.nextID = 4
	service := NewService(repository, WithClock(fixedProjectClock))
	orderVersion, err := service.Hide(context.Background(), 2, HideRequest{Version: 1, OrderVersion: 1})
	if err != nil {
		t.Fatalf("Hide() error = %v", err)
	}
	if orderVersion != 2 {
		t.Fatalf("order version = %d, want 2", orderVersion)
	}
	remaining := repository.publicProjects()
	if len(remaining) != 2 || remaining[0].ID != 1 || remaining[1].ID != 3 || sortValue(remaining[0]) != 1 || sortValue(remaining[1]) != 2 {
		t.Fatalf("remaining public projects = %+v", remaining)
	}
}

func TestServiceReorderSuccessAndConflict(t *testing.T) {
	repository := newFakeProjectRepository()
	for id := uint64(1); id <= 3; id++ {
		repository.projects[id] = Project{ID: id, Name: "项目", Status: StatusPublic, SortOrder: int64PointerValue(int64(id)), Version: 1}
	}
	service := NewService(repository, WithClock(fixedProjectClock))
	orderVersion, err := service.Reorder(context.Background(), OrderRequest{OrderVersion: 1, PublicIDs: []uint64{3, 1, 2}})
	if err != nil || orderVersion != 2 {
		t.Fatalf("Reorder() = %d, %v", orderVersion, err)
	}
	ordered := repository.publicProjects()
	if ids := projectIDs(ordered); len(ids) != 3 || ids[0] != 3 || ids[1] != 1 || ids[2] != 2 {
		t.Fatalf("ordered ids = %v", ids)
	}
	if _, err := service.Reorder(context.Background(), OrderRequest{OrderVersion: 1, PublicIDs: []uint64{1, 2, 3}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale reorder error = %v, want conflict", err)
	}
}
