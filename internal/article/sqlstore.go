package article

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mywebsite/internal/database/dbgen"
	"mywebsite/internal/markdown"
)

// SQLStore adapts the generated queries to the article domain.  The DB and
// Queries fields are exported so the application wiring can provide the
// shared connection and generated query object directly.
type SQLStore struct {
	DB      *sql.DB
	Queries *dbgen.Queries
}

// NewSQLStore creates a store backed by db and queries.  When queries is nil,
// it is derived from db for the common application-wiring case.
func NewSQLStore(db *sql.DB, queries *dbgen.Queries) *SQLStore {
	if queries == nil && db != nil {
		queries = dbgen.New(db)
	}
	return &SQLStore{DB: db, Queries: queries}
}

var (
	_ Store             = (*SQLStore)(nil)
	_ PublishedStore    = (*SQLStore)(nil)
	_ AtomicStore       = (*SQLStore)(nil)
	_ adminArticleStore = (*SQLStore)(nil)
)

func (s *SQLStore) query() (*dbgen.Queries, error) {
	if s == nil || s.Queries == nil {
		return nil, ErrStoreCapability
	}
	return s.Queries, nil
}

func (s *SQLStore) begin(ctx context.Context) (*sql.Tx, *dbgen.Queries, error) {
	if s == nil || s.DB == nil {
		return nil, nil, ErrStoreCapability
	}
	queries, err := s.query()
	if err != nil {
		return nil, nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin article transaction: %w", err)
	}
	return tx, queries.WithTx(tx), nil
}

func (s *SQLStore) CreateArticle(ctx context.Context, params dbgen.CreateArticleParams) (sql.Result, error) {
	queries, err := s.query()
	if err != nil {
		return nil, err
	}
	return queries.CreateArticle(ctx, params)
}

func (s *SQLStore) GetArticleByID(ctx context.Context, id uint64) (dbgen.Article, error) {
	queries, err := s.query()
	if err != nil {
		return dbgen.Article{}, err
	}
	return queries.GetArticleByID(ctx, id)
}

func (s *SQLStore) UpdateArticle(ctx context.Context, params dbgen.UpdateArticleParams) (sql.Result, error) {
	queries, err := s.query()
	if err != nil {
		return nil, err
	}
	return queries.UpdateArticle(ctx, params)
}

func (s *SQLStore) WithdrawArticle(ctx context.Context, params dbgen.WithdrawArticleParams) (sql.Result, error) {
	queries, err := s.query()
	if err != nil {
		return nil, err
	}
	return queries.WithdrawArticle(ctx, params)
}

func (s *SQLStore) GetPublishedArticleByULID(ctx context.Context, publicULID sql.NullString) (dbgen.Article, error) {
	queries, err := s.query()
	if err != nil {
		return dbgen.Article{}, err
	}
	return queries.GetPublishedArticleByULID(ctx, publicULID)
}

func (s *SQLStore) ListPublishedArticles(ctx context.Context, params dbgen.ListPublishedArticlesParams) ([]dbgen.ListPublishedArticlesRow, error) {
	queries, err := s.query()
	if err != nil {
		return nil, err
	}
	return queries.ListPublishedArticles(ctx, params)
}

func (s *SQLStore) CountPublishedArticles(ctx context.Context) (int64, error) {
	queries, err := s.query()
	if err != nil {
		return 0, err
	}
	return queries.CountPublishedArticles(ctx)
}

func (s *SQLStore) ListAdminArticles(ctx context.Context, params dbgen.ListAdminArticlesParams) ([]dbgen.ListAdminArticlesRow, error) {
	queries, err := s.query()
	if err != nil {
		return nil, err
	}
	return queries.ListAdminArticles(ctx, params)
}

func (s *SQLStore) CountAdminArticles(ctx context.Context, params dbgen.CountAdminArticlesParams) (int64, error) {
	queries, err := s.query()
	if err != nil {
		return 0, err
	}
	return queries.CountAdminArticles(ctx, params)
}

func (s *SQLStore) CreateArticleWithMedia(ctx context.Context, params dbgen.CreateArticleParams, references []markdown.MediaReference) (result sql.Result, err error) {
	tx, queries, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err = queries.CreateArticle(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create article: %w", err)
	}
	articleID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read created article id: %w", err)
	}
	if articleID < 0 {
		return nil, fmt.Errorf("read created article id: %w", ErrConflict)
	}
	if err = validateMediaReferences(ctx, queries, references); err != nil {
		return nil, err
	}
	for _, reference := range references {
		if err = queries.InsertArticleMedia(ctx, dbgen.InsertArticleMediaParams{
			ArticleID: uint64(articleID),
			AssetID:   reference.AssetID,
			AltText:   reference.AltText,
		}); err != nil {
			return nil, fmt.Errorf("insert article media reference: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit article transaction: %w", err)
	}
	return result, nil
}

func (s *SQLStore) UpdateArticleWithMedia(ctx context.Context, params dbgen.UpdateArticleParams, references []markdown.MediaReference) (result sql.Result, err error) {
	tx, queries, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err = queries.UpdateArticle(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("update article: %w", err)
	}
	if err = requireExactlyOneRowsAffected(result); err != nil {
		return nil, err
	}
	if err = validateMediaReferences(ctx, queries, references); err != nil {
		return nil, err
	}
	if err = queries.DeleteArticleMediaByArticleID(ctx, params.ID); err != nil {
		return nil, fmt.Errorf("delete article media references: %w", err)
	}
	for _, reference := range references {
		if err = queries.InsertArticleMedia(ctx, dbgen.InsertArticleMediaParams{
			ArticleID: params.ID,
			AssetID:   reference.AssetID,
			AltText:   reference.AltText,
		}); err != nil {
			return nil, fmt.Errorf("insert article media reference: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit article transaction: %w", err)
	}
	return result, nil
}

func validateMediaReferences(ctx context.Context, queries *dbgen.Queries, references []markdown.MediaReference) error {
	for _, reference := range references {
		_, err := queries.GetMediaAssetByID(ctx, reference.AssetID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReferencedMediaNotFound
		}
		if err != nil {
			return fmt.Errorf("validate media asset %q: %w", reference.AssetID, err)
		}
	}
	return nil
}
