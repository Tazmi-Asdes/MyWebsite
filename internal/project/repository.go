package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"mywebsite/internal/database/dbgen"
)

// SQLRepository is the production persistence boundary. All publication,
// hiding, and complete-order replacement operations use one MySQL transaction
// and the generated FOR UPDATE queries supplied by the current schema.
type SQLRepository struct {
	db      *sql.DB
	queries *dbgen.Queries
}

// NewSQLRepository creates a repository backed by db. An optional generated
// query set is accepted for callers that already own one; otherwise it is
// created from db.
func NewSQLRepository(db *sql.DB, querySets ...*dbgen.Queries) *SQLRepository {
	var queries *dbgen.Queries
	if len(querySets) > 0 {
		queries = querySets[0]
	}
	if queries == nil && db != nil {
		queries = dbgen.New(db)
	}
	return &SQLRepository{db: db, queries: queries}
}

func NewRepository(db *sql.DB, querySets ...*dbgen.Queries) *SQLRepository {
	return NewSQLRepository(db, querySets...)
}

func (repository *SQLRepository) CreateProject(ctx context.Context, name string, now time.Time) (Project, error) {
	if err := repository.ready(); err != nil {
		return Project{}, err
	}
	result, err := repository.queries.CreateProject(ctx, dbgen.CreateProjectParams{
		Name: name, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	})
	if err != nil {
		return Project{}, storageError(err)
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		if err == nil {
			err = errors.New("database returned an invalid project id")
		}
		return Project{}, storageError(err)
	}
	return repository.GetProject(ctx, uint64(id))
}

func (repository *SQLRepository) GetProject(ctx context.Context, id uint64) (Project, error) {
	if err := repository.ready(); err != nil {
		return Project{}, err
	}
	row, err := repository.queries.GetProjectByID(ctx, id)
	if err != nil {
		return Project{}, projectRowError(err)
	}
	return adminProjectFromDB(row), nil
}

func (repository *SQLRepository) UpdateProject(ctx context.Context, id uint64, fields ProjectFields, version uint64, now time.Time) (Project, error) {
	if err := repository.ready(); err != nil {
		return Project{}, err
	}
	result, err := repository.queries.UpdateProject(ctx, dbgen.UpdateProjectParams{
		Name:         fields.Name,
		GithubUrl:    nullableString(fields.GitHubURL),
		ImageAssetID: nullableString(fields.ImageAssetID),
		UpdatedAt:    now.UTC(),
		ID:           id,
		Version:      version,
	})
	if err != nil {
		return Project{}, storageError(err)
	}
	if err := rowsAffected(result); err != nil {
		return Project{}, err
	}
	return repository.GetProject(ctx, id)
}

func (repository *SQLRepository) ListProjectGroups(ctx context.Context) (ProjectGroups, error) {
	if err := repository.ready(); err != nil {
		return ProjectGroups{}, err
	}
	publicRows, err := repository.queries.ListAdminPublicProjects(ctx)
	if err != nil {
		return ProjectGroups{}, storageError(err)
	}
	hiddenRows, err := repository.queries.ListAdminHiddenProjects(ctx)
	if err != nil {
		return ProjectGroups{}, storageError(err)
	}
	state, err := repository.queries.GetProjectOrderState(ctx)
	if err != nil {
		return ProjectGroups{}, storageError(err)
	}
	result := ProjectGroups{
		Public:       make([]Project, 0, len(publicRows)),
		Hidden:       make([]Project, 0, len(hiddenRows)),
		OrderVersion: state.Version,
	}
	for _, row := range publicRows {
		result.Public = append(result.Public, adminProjectFromDB(row))
	}
	for _, row := range hiddenRows {
		result.Hidden = append(result.Hidden, adminProjectFromDB(row))
	}
	return result, nil
}

func (repository *SQLRepository) ListAllPublicProjects(ctx context.Context) ([]Project, error) {
	if err := repository.ready(); err != nil {
		return nil, err
	}
	rows, err := repository.queries.ListAllPublicProjects(ctx)
	if err != nil {
		return nil, storageError(err)
	}
	return publicProjectsFromDB(rows), nil
}

func (repository *SQLRepository) ListPublicProjects(ctx context.Context, limit, offset int32) ([]Project, error) {
	if err := repository.ready(); err != nil {
		return nil, err
	}
	rows, err := repository.queries.ListPublicProjects(ctx, dbgen.ListPublicProjectsParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, storageError(err)
	}
	return publicProjectsFromDB(rows), nil
}

func (repository *SQLRepository) CountPublicProjects(ctx context.Context) (int64, error) {
	if err := repository.ready(); err != nil {
		return 0, err
	}
	total, err := repository.queries.CountPublicProjects(ctx)
	if err != nil {
		return 0, storageError(err)
	}
	return total, nil
}

func (repository *SQLRepository) MediaExists(ctx context.Context, id string) (bool, error) {
	if err := repository.ready(); err != nil {
		return false, err
	}
	_, err := repository.queries.GetMediaAssetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, storageError(err)
	}
	return true, nil
}

func (repository *SQLRepository) PublishProject(ctx context.Context, id uint64, fields ProjectFields, projectVersion, orderVersion uint64, now time.Time) (PublishResult, error) {
	if err := repository.ready(); err != nil {
		return PublishResult{}, err
	}
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return PublishResult{}, storageError(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()

	queries := repository.queries.WithTx(transaction)
	state, err := queries.GetProjectOrderStateForUpdate(ctx)
	if err != nil {
		return PublishResult{}, storageError(err)
	}
	if state.Version != orderVersion {
		return PublishResult{}, ErrConflict
	}
	current, err := queries.GetProjectByIDForUpdate(ctx, id)
	if err != nil {
		return PublishResult{}, projectRowError(err)
	}
	if current.Version != projectVersion || current.Status != string(StatusHidden) {
		return PublishResult{}, ErrConflict
	}
	maxSort, err := queries.GetMaxPublicProjectSortOrder(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PublishResult{}, storageError(err)
	}
	nextSort := int64(1)
	if maxSort.Valid {
		nextSort = maxSort.Int64 + 1
	}
	result, err := queries.PublishProject(ctx, dbgen.PublishProjectParams{
		Name:         fields.Name,
		GithubUrl:    nullableString(fields.GitHubURL),
		ImageAssetID: nullableString(fields.ImageAssetID),
		SortOrder:    sql.NullInt64{Int64: nextSort, Valid: true},
		UpdatedAt:    now.UTC(),
		ID:           id,
		Version:      projectVersion,
	})
	if err != nil {
		return PublishResult{}, storageError(err)
	}
	if err := rowsAffected(result); err != nil {
		return PublishResult{}, err
	}
	if err := updateOrderState(queries, ctx, state.Version, now); err != nil {
		return PublishResult{}, err
	}
	updated, err := queries.GetProjectByID(ctx, id)
	if err != nil {
		return PublishResult{}, storageError(err)
	}
	if err := transaction.Commit(); err != nil {
		return PublishResult{}, storageError(err)
	}
	committed = true
	return PublishResult{Project: adminProjectFromDB(updated), OrderVersion: state.Version + 1}, nil
}

func (repository *SQLRepository) HideProject(ctx context.Context, id uint64, projectVersion, orderVersion uint64, now time.Time) (HideResult, error) {
	if err := repository.ready(); err != nil {
		return HideResult{}, err
	}
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return HideResult{}, storageError(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()

	queries := repository.queries.WithTx(transaction)
	state, err := queries.GetProjectOrderStateForUpdate(ctx)
	if err != nil {
		return HideResult{}, storageError(err)
	}
	if state.Version != orderVersion {
		return HideResult{}, ErrConflict
	}
	current, err := queries.GetProjectByIDForUpdate(ctx, id)
	if err != nil {
		return HideResult{}, projectRowError(err)
	}
	if current.Version != projectVersion || current.Status != string(StatusPublic) {
		return HideResult{}, ErrConflict
	}
	result, err := queries.HideProject(ctx, dbgen.HideProjectParams{
		UpdatedAt: now.UTC(), ID: id, Version: projectVersion,
	})
	if err != nil {
		return HideResult{}, storageError(err)
	}
	if err := rowsAffected(result); err != nil {
		return HideResult{}, err
	}
	remaining, err := queries.ListPublicProjectIDsForUpdate(ctx)
	if err != nil {
		return HideResult{}, storageError(err)
	}
	if err := compactProjectOrder(ctx, queries, remaining); err != nil {
		return HideResult{}, err
	}
	if err := updateOrderState(queries, ctx, state.Version, now); err != nil {
		return HideResult{}, err
	}
	updated, err := queries.GetProjectByID(ctx, id)
	if err != nil {
		return HideResult{}, storageError(err)
	}
	if err := transaction.Commit(); err != nil {
		return HideResult{}, storageError(err)
	}
	committed = true
	return HideResult{Project: adminProjectFromDB(updated), OrderVersion: state.Version + 1}, nil
}

func (repository *SQLRepository) ReorderProjects(ctx context.Context, ids []uint64, orderVersion uint64, now time.Time) (uint64, error) {
	if err := repository.ready(); err != nil {
		return 0, err
	}
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, storageError(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()

	queries := repository.queries.WithTx(transaction)
	state, err := queries.GetProjectOrderStateForUpdate(ctx)
	if err != nil {
		return 0, storageError(err)
	}
	if state.Version != orderVersion {
		return 0, ErrConflict
	}
	currentIDs, err := queries.ListPublicProjectIDsForUpdate(ctx)
	if err != nil {
		return 0, storageError(err)
	}
	if err := sameIDSet(currentIDs, ids); err != nil {
		return 0, err
	}
	if err := setTemporaryOrder(ctx, queries, ids); err != nil {
		return 0, err
	}
	if err := setFinalOrder(ctx, queries, ids); err != nil {
		return 0, err
	}
	if err := updateOrderState(queries, ctx, state.Version, now); err != nil {
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, storageError(err)
	}
	committed = true
	return state.Version + 1, nil
}

func (repository *SQLRepository) ready() error {
	if repository == nil || repository.db == nil || repository.queries == nil {
		return ErrDependency
	}
	return nil
}

func updateOrderState(queries *dbgen.Queries, ctx context.Context, version uint64, now time.Time) error {
	result, err := queries.UpdateProjectOrderState(ctx, dbgen.UpdateProjectOrderStateParams{
		UpdatedAt: now.UTC(), Version: version,
	})
	if err != nil {
		return storageError(err)
	}
	return rowsAffected(result)
}

func compactProjectOrder(ctx context.Context, queries *dbgen.Queries, ids []uint64) error {
	if err := setTemporaryOrder(ctx, queries, ids); err != nil {
		return err
	}
	return setFinalOrder(ctx, queries, ids)
}

func setTemporaryOrder(ctx context.Context, queries *dbgen.Queries, ids []uint64) error {
	n := int64(len(ids))
	for index, id := range ids {
		result, err := queries.SetProjectSortOrderTemporary(ctx, dbgen.SetProjectSortOrderTemporaryParams{
			TemporarySortOrder: sql.NullInt64{Int64: n + int64(index) + 1, Valid: true},
			ID:                 id,
		})
		if err != nil {
			return storageError(err)
		}
		if err := rowsAffected(result); err != nil {
			return err
		}
	}
	return nil
}

func setFinalOrder(ctx context.Context, queries *dbgen.Queries, ids []uint64) error {
	for index, id := range ids {
		result, err := queries.SetProjectSortOrder(ctx, dbgen.SetProjectSortOrderParams{
			SortOrder: sql.NullInt64{Int64: int64(index) + 1, Valid: true},
			ID:        id,
		})
		if err != nil {
			return storageError(err)
		}
		if err := rowsAffected(result); err != nil {
			return err
		}
	}
	return nil
}

func sameIDSet(current, requested []uint64) error {
	if len(current) != len(requested) {
		return ErrValidationFailed
	}
	seen := make(map[uint64]struct{}, len(current))
	for _, id := range current {
		seen[id] = struct{}{}
	}
	for _, id := range requested {
		if id == 0 {
			return ErrValidationFailed
		}
		if _, ok := seen[id]; !ok {
			return ErrValidationFailed
		}
		delete(seen, id)
	}
	if len(seen) != 0 {
		return ErrValidationFailed
	}
	return nil
}

func rowsAffected(result sql.Result) error {
	if result == nil {
		return ErrConflict
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func projectRowError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return storageError(err)
}

func storageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrStorage) || errors.Is(err, ErrDependency) || errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrStorage, err)
}

func nullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func adminProjectFromDB(row dbgen.Project) Project {
	return Project{
		ID:              row.ID,
		Name:            row.Name,
		GitHubURL:       stringPointer(row.GithubUrl),
		ImageAssetID:    stringPointer(row.ImageAssetID),
		ImagePreviewURL: imagePreviewURL(row.ImageAssetID),
		Status:          Status(row.Status),
		SortOrder:       int64Pointer(row.SortOrder),
		Version:         row.Version,
		CreatedAt:       row.CreatedAt.UTC(),
		UpdatedAt:       row.UpdatedAt.UTC(),
	}
}

func publicProjectsFromDB(rows []dbgen.Project) []Project {
	items := make([]Project, 0, len(rows))
	for _, row := range rows {
		item := adminProjectFromDB(row)
		item.ImagePreviewURL = nil
		if item.ImageAssetID != nil {
			item.ImageURL = "/media/" + *item.ImageAssetID
		} else {
			item.ImageURL = DefaultImageURL
		}
		items = append(items, item)
	}
	return items
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func imagePreviewURL(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := "/api/v1/media/" + value.String
	return &result
}

func int64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
