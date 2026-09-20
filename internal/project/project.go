// Package project contains the project domain, including the publication
// invariant that is shared by the administrator and public-site adapters.
package project

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	StatusHidden Status = "hidden"
	StatusPublic Status = "public"

	DefaultImageURL        = "/assets/default-project.svg"
	ProjectDefaultImageURL = DefaultImageURL
	maxProjectNameRunes    = 120
)

// Status is deliberately independent from HTTP status codes.
type Status string

// Stable domain errors. Adapters can map these errors to transport-specific
// responses without making the domain depend on HTTP.
var (
	ErrNotFound                      = errors.New("not_found")
	ErrConflict                      = errors.New("conflict")
	ErrValidationFailed              = errors.New("validation_failed")
	ErrGitHubRepositoryInvalid       = errors.New("github_repository_invalid")
	ErrGitHubVerificationUnavailable = errors.New("github_verification_unavailable")
	ErrStorage                       = errors.New("storage_failure")
	ErrDependency                    = errors.New("dependency_failure")
)

// Compatibility aliases keep the domain vocabulary convenient at adapter
// call sites while retaining one error identity for each category.
var (
	ErrVersionConflict   = ErrConflict
	ErrInvalid           = ErrValidationFailed
	ErrGitHubInvalid     = ErrGitHubRepositoryInvalid
	ErrGitHubUnavailable = ErrGitHubVerificationUnavailable
	ErrStorageFailure    = ErrStorage
	ErrDependencyFailure = ErrDependency
)

// ErrorCode returns the stable machine code represented by err.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrValidationFailed):
		return "validation_failed"
	case errors.Is(err, ErrGitHubRepositoryInvalid):
		return "github_repository_invalid"
	case errors.Is(err, ErrGitHubVerificationUnavailable):
		return "github_verification_unavailable"
	case errors.Is(err, ErrStorage), errors.Is(err, ErrDependency):
		return "storage_failure"
	default:
		return ""
	}
}

// Project is the administrator-facing project DTO. ImagePreviewURL is only
// populated for administrator responses. ImageURL is only populated for
// public responses, where a missing asset intentionally uses the shared
// decorative default image.
type Project struct {
	ID              uint64    `json:"id"`
	Name            string    `json:"name"`
	GitHubURL       *string   `json:"github_url,omitempty"`
	ImageAssetID    *string   `json:"image_asset_id,omitempty"`
	ImagePreviewURL *string   `json:"image_preview_url"`
	ImageURL        string    `json:"image_url,omitempty"`
	Status          Status    `json:"status"`
	SortOrder       *int64    `json:"sort_order,omitempty"`
	Version         uint64    `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ProjectGroups contains the two administrator lists and the optimistic-lock
// version of the public ordering collection.
type ProjectGroups struct {
	Public       []Project `json:"public"`
	Hidden       []Project `json:"hidden"`
	OrderVersion uint64    `json:"order_version"`
}

// OrderResult is returned after a successful complete-order replacement.
type OrderResult struct {
	OrderVersion uint64 `json:"order_version"`
}

// HideResult is returned by the domain operation even though the HTTP
// adapter may choose a 204 response and omit its body.
type HideResult struct {
	Project      Project `json:"project"`
	OrderVersion uint64  `json:"order_version"`
}

// PublishResult is the transaction result for a newly public project.
type PublishResult struct {
	Project      Project `json:"project"`
	OrderVersion uint64  `json:"order_version"`
}

type CreateRequest struct {
	Name string `json:"name"`
}

type UpdateRequest struct {
	Name      string  `json:"name"`
	GitHubURL *string `json:"github_url,omitempty"`
	// GithubURL accepts the spelling used by sqlc-generated names. The
	// service treats it as an input alias for GitHubURL.
	GithubURL    *string `json:"-"`
	ImageAssetID *string `json:"image_asset_id,omitempty"`
	Version      uint64  `json:"version"`
}

type PublishRequest struct {
	Name         string  `json:"name"`
	GitHubURL    *string `json:"github_url"`
	GithubURL    *string `json:"-"`
	ImageAssetID *string `json:"image_asset_id"`
	Version      uint64  `json:"version"`
	OrderVersion uint64  `json:"order_version"`
}

type HideRequest struct {
	Version      uint64 `json:"version"`
	OrderVersion uint64 `json:"order_version"`
}

type OrderRequest struct {
	OrderVersion uint64   `json:"order_version"`
	PublicIDs    []uint64 `json:"public_ids"`
}

// Names used by transport adapters and tests remain aliases, not separate
// request contracts.
type ProjectCreateRequest = CreateRequest
type ProjectUpdateRequest = UpdateRequest
type ProjectPublishRequest = PublishRequest
type ProjectHideRequest = HideRequest
type ProjectOrderRequest = OrderRequest
type ProjectOrderResult = OrderResult
type CreateInput = CreateRequest
type UpdateInput = UpdateRequest
type PublishInput = PublishRequest
type HideInput = HideRequest
type OrderInput = OrderRequest

// Repository is the small domain persistence boundary. SQLRepository is the
// production implementation; fakes can implement this interface without a
// database connection.
type Repository interface {
	CreateProject(context.Context, string, time.Time) (Project, error)
	GetProject(context.Context, uint64) (Project, error)
	UpdateProject(context.Context, uint64, ProjectFields, uint64, time.Time) (Project, error)
	ListProjectGroups(context.Context) (ProjectGroups, error)
	ListAllPublicProjects(context.Context) ([]Project, error)
	ListPublicProjects(context.Context, int32, int32) ([]Project, error)
	CountPublicProjects(context.Context) (int64, error)
	MediaExists(context.Context, string) (bool, error)
	PublishProject(context.Context, uint64, ProjectFields, uint64, uint64, time.Time) (PublishResult, error)
	HideProject(context.Context, uint64, uint64, uint64, time.Time) (HideResult, error)
	ReorderProjects(context.Context, []uint64, uint64, time.Time) (uint64, error)
}

// Store is a compatibility name for adapters that use store terminology.
type Store = Repository

// ProjectFields is the normalized form persisted by update and publish.
type ProjectFields struct {
	Name         string
	GitHubURL    *string
	ImageAssetID *string
}

// GitHubVerifier checks that a normalized GitHub repository exists and is
// public. It intentionally accepts the canonical repository URL as a single
// value so alternative implementations can be used in tests.
type GitHubVerifier interface {
	Verify(context.Context, string) error
}

// RepositoryVerifier is an alternate readable method name accepted by
// NewService when an adapter already exposes VerifyRepository.
type RepositoryVerifier interface {
	VerifyRepository(context.Context, string) error
}

type repositoryVerifierAdapter struct{ verifier RepositoryVerifier }

func (adapter repositoryVerifierAdapter) Verify(ctx context.Context, value string) error {
	return adapter.verifier.VerifyRepository(ctx, value)
}

// GitHubVerifyFunc adapts a function to GitHubVerifier.
type GitHubVerifyFunc func(context.Context, string) error

func (f GitHubVerifyFunc) Verify(ctx context.Context, value string) error {
	if f == nil {
		return ErrGitHubVerificationUnavailable
	}
	return f(ctx, value)
}

// Service coordinates validation, dependencies, and domain operations.
type Service struct {
	repository Repository
	verifier   GitHubVerifier
	now        func() time.Time
}

type Option func(*Service)

// WithClock injects a deterministic time source. It accepts either a
// func() time.Time or a small clock interface with a Now method.
func WithClock(value any) Option {
	return func(service *Service) {
		switch clock := value.(type) {
		case func() time.Time:
			if clock != nil {
				service.now = clock
			}
		case interface{ Now() time.Time }:
			if clock != nil {
				service.now = clock.Now
			}
		}
	}
}

func WithNow(value func() time.Time) Option { return WithClock(value) }

func WithGitHubVerifier(verifier GitHubVerifier) Option {
	return func(service *Service) {
		if verifier != nil {
			service.verifier = verifier
		}
	}
}

// NewService creates a project service. Dependencies may be supplied in any
// order as a verifier and/or Option; accepting both forms keeps construction
// convenient for small adapters while the domain boundary remains typed.
func NewService(repository Repository, dependencies ...any) *Service {
	service := &Service{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case GitHubVerifier:
			service.verifier = value
		case RepositoryVerifier:
			service.verifier = repositoryVerifierAdapter{verifier: value}
		case Option:
			if value != nil {
				value(service)
			}
		}
	}
	return service
}

func New(repository Repository, dependencies ...any) *Service {
	return NewService(repository, dependencies...)
}

func (service *Service) Create(ctx context.Context, request CreateRequest) (Project, error) {
	if service == nil || service.repository == nil {
		return Project{}, ErrDependency
	}
	name, err := normalizeName(request.Name)
	if err != nil {
		return Project{}, err
	}
	return service.repository.CreateProject(ctx, name, service.currentTime())
}

func (service *Service) CreateProject(ctx context.Context, request CreateRequest) (Project, error) {
	return service.Create(ctx, request)
}

func (service *Service) Get(ctx context.Context, id uint64) (Project, error) {
	if service == nil || service.repository == nil {
		return Project{}, ErrDependency
	}
	return service.repository.GetProject(ctx, id)
}

func (service *Service) GetProject(ctx context.Context, id uint64) (Project, error) {
	return service.Get(ctx, id)
}

func (service *Service) Update(ctx context.Context, id uint64, request UpdateRequest) (Project, error) {
	if service == nil || service.repository == nil {
		return Project{}, ErrDependency
	}
	current, err := service.repository.GetProject(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if current.Version != request.Version {
		return Project{}, ErrConflict
	}
	name, err := normalizeName(request.Name)
	if err != nil {
		return Project{}, err
	}
	githubURL, err := normalizeOptionalGitHubURL(request.githubURL())
	if err != nil {
		return Project{}, err
	}
	imageID, err := service.validateMedia(ctx, request.ImageAssetID)
	if err != nil {
		return Project{}, err
	}
	if current.Status == StatusPublic {
		if githubURL == nil {
			return Project{}, ErrGitHubRepositoryInvalid
		}
		if err := service.verifyGitHub(ctx, *githubURL); err != nil {
			return Project{}, err
		}
	}
	return service.repository.UpdateProject(ctx, id, ProjectFields{
		Name: name, GitHubURL: githubURL, ImageAssetID: imageID,
	}, request.Version, service.currentTime())
}

func (service *Service) UpdateProject(ctx context.Context, id uint64, request UpdateRequest) (Project, error) {
	return service.Update(ctx, id, request)
}

func (service *Service) Publish(ctx context.Context, id uint64, request PublishRequest) (Project, uint64, error) {
	if service == nil || service.repository == nil {
		return Project{}, 0, ErrDependency
	}
	name, err := normalizeName(request.Name)
	if err != nil {
		return Project{}, 0, err
	}
	githubURL, err := normalizeOptionalGitHubURL(request.githubURL())
	if err != nil || githubURL == nil {
		if err != nil {
			return Project{}, 0, err
		}
		return Project{}, 0, ErrGitHubRepositoryInvalid
	}
	if err := service.verifyGitHub(ctx, *githubURL); err != nil {
		return Project{}, 0, err
	}
	imageID, err := service.validateMedia(ctx, request.ImageAssetID)
	if err != nil {
		return Project{}, 0, err
	}
	result, err := service.repository.PublishProject(ctx, id, ProjectFields{
		Name: name, GitHubURL: githubURL, ImageAssetID: imageID,
	}, request.Version, request.OrderVersion, service.currentTime())
	if err != nil {
		return Project{}, 0, err
	}
	return result.Project, result.OrderVersion, nil
}

func (service *Service) PublishProject(ctx context.Context, id uint64, request PublishRequest) (Project, uint64, error) {
	return service.Publish(ctx, id, request)
}

func (service *Service) Hide(ctx context.Context, id uint64, request HideRequest) (uint64, error) {
	if service == nil || service.repository == nil {
		return 0, ErrDependency
	}
	result, err := service.repository.HideProject(ctx, id, request.Version, request.OrderVersion, service.currentTime())
	if err != nil {
		return 0, err
	}
	return result.OrderVersion, nil
}

func (service *Service) HideProject(ctx context.Context, id uint64, request HideRequest) (uint64, error) {
	return service.Hide(ctx, id, request)
}

func (service *Service) Reorder(ctx context.Context, request OrderRequest) (uint64, error) {
	if service == nil || service.repository == nil {
		return 0, ErrDependency
	}
	if err := validateOrderIDs(request.PublicIDs); err != nil {
		return 0, err
	}
	return service.repository.ReorderProjects(ctx, append([]uint64(nil), request.PublicIDs...), request.OrderVersion, service.currentTime())
}

func (service *Service) UpdateOrder(ctx context.Context, request OrderRequest) (uint64, error) {
	return service.Reorder(ctx, request)
}

func (service *Service) UpdateProjectOrder(ctx context.Context, request OrderRequest) (uint64, error) {
	return service.Reorder(ctx, request)
}

func (service *Service) ListGroups(ctx context.Context) (ProjectGroups, error) {
	if service == nil || service.repository == nil {
		return ProjectGroups{}, ErrDependency
	}
	return service.repository.ListProjectGroups(ctx)
}

func (service *Service) ListProjectGroups(ctx context.Context) (ProjectGroups, error) {
	return service.ListGroups(ctx)
}

// ListPublic returns all public projects by default. A single optional limit
// (and optional offset) is accepted for the home-page projection; callers that
// need no paging should use the zero-argument form.
func (service *Service) ListPublic(ctx context.Context, paging ...int32) ([]Project, error) {
	if service == nil || service.repository == nil {
		return nil, ErrDependency
	}
	if len(paging) == 0 {
		return service.repository.ListAllPublicProjects(ctx)
	}
	limit := paging[0]
	offset := int32(0)
	if len(paging) > 1 {
		offset = paging[1]
	}
	if limit <= 0 || offset < 0 {
		return nil, ErrValidationFailed
	}
	return service.repository.ListPublicProjects(ctx, limit, offset)
}

func (service *Service) ListPublicProjects(ctx context.Context) ([]Project, error) {
	return service.ListPublic(ctx)
}

func (service *Service) ListFeatured(ctx context.Context) ([]Project, error) {
	return service.ListPublic(ctx, 3)
}

func (service *Service) CountPublic(ctx context.Context) (int64, error) {
	if service == nil || service.repository == nil {
		return 0, ErrDependency
	}
	return service.repository.CountPublicProjects(ctx)
}

func (service *Service) CountPublicProjects(ctx context.Context) (int64, error) {
	return service.CountPublic(ctx)
}

func (service *Service) currentTime() time.Time {
	if service == nil || service.now == nil {
		return time.Now().UTC()
	}
	return service.now().UTC()
}

func (service *Service) verifyGitHub(ctx context.Context, value string) error {
	if service.verifier == nil {
		return ErrGitHubVerificationUnavailable
	}
	if err := service.verifier.Verify(ctx, value); err != nil {
		if errors.Is(err, ErrGitHubRepositoryInvalid) || errors.Is(err, ErrGitHubVerificationUnavailable) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrGitHubVerificationUnavailable, err)
	}
	return nil
}

func (service *Service) validateMedia(ctx context.Context, imageID *string) (*string, error) {
	if imageID == nil {
		return nil, nil
	}
	if !isStrictULID(*imageID) {
		return nil, ErrValidationFailed
	}
	exists, err := service.repository.MediaExists(ctx, *imageID)
	if err != nil {
		if errors.Is(err, ErrStorage) || errors.Is(err, ErrDependency) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: media existence check: %v", ErrDependency, err)
	}
	if !exists {
		return nil, ErrValidationFailed
	}
	value := *imageID
	return &value, nil
}

func (request UpdateRequest) githubURL() *string {
	if request.GitHubURL != nil {
		return request.GitHubURL
	}
	return request.GithubURL
}

func (request PublishRequest) githubURL() *string {
	if request.GitHubURL != nil {
		return request.GitHubURL
	}
	return request.GithubURL
}

func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > maxProjectNameRunes {
		return "", ErrValidationFailed
	}
	return value, nil
}

func normalizeOptionalGitHubURL(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(*value)
	if raw == "" {
		return nil, nil
	}
	canonical, _, _, err := ParseGitHubRepositoryURL(raw)
	if err != nil {
		return nil, err
	}
	return &canonical, nil
}

// ParseGitHubRepositoryURL validates and canonicalizes the only accepted
// repository URL form. It returns the canonical URL, owner, and repository.
func ParseGitHubRepositoryURL(raw string) (string, string, string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", "", ErrValidationFailed
	}
	if parsed.Opaque != "" || parsed.Path == "" || strings.ContainsAny(raw, "\r\n\t") {
		return "", "", "", ErrValidationFailed
	}
	if strings.Contains(strings.ToLower(parsed.RawPath), "%2f") {
		return "", "", "", ErrValidationFailed
	}
	path := parsed.Path
	if strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
		if strings.HasSuffix(path, "/") {
			return "", "", "", ErrValidationFailed
		}
	}
	if !strings.HasPrefix(path, "/") {
		return "", "", "", ErrValidationFailed
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", "", "", ErrValidationFailed
	}
	if strings.Contains(parts[0], "\\") || strings.Contains(parts[1], "\\") || strings.ContainsAny(parts[0], "?#") || strings.ContainsAny(parts[1], "?#") {
		return "", "", "", ErrValidationFailed
	}
	// Encoded slashes decode into additional path segments above. Preserve
	// ordinary escaping in the canonical URL while disallowing an alternate
	// host spelling or a trailing slash.
	owner, repo := parts[0], parts[1]
	if owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", "", "", ErrValidationFailed
	}
	canonical := "https://github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	return canonical, owner, repo, nil
}

func isStrictULID(value string) bool {
	if len(value) != 26 {
		return false
	}
	for index, character := range value {
		if index == 0 && character > '7' {
			return false
		}
		if !(character >= '0' && character <= '9') && !(character >= 'A' && character <= 'H') && !(character >= 'J' && character <= 'K') && !(character >= 'M' && character <= 'N') && !(character >= 'P' && character <= 'T') && !(character >= 'V' && character <= 'Z') {
			return false
		}
	}
	_, err := ulid.ParseStrict(value)
	return err == nil
}

func validateOrderIDs(ids []uint64) error {
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return ErrValidationFailed
		}
		if _, ok := seen[id]; ok {
			return ErrValidationFailed
		}
		seen[id] = struct{}{}
	}
	return nil
}
