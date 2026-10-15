package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"mywebsite/internal/project"
)

const projectBodyLimit int64 = 64 * 1024

type projectCreateRequest struct {
	Name *string `json:"name"`
}

type projectUpdateRequest struct {
	Name         *string `json:"name"`
	GitHubURL    *string `json:"github_url"`
	ImageAssetID *string `json:"image_asset_id"`
	Version      *uint64 `json:"version"`
}

type projectPublishRequest struct {
	Name         *string `json:"name"`
	GitHubURL    *string `json:"github_url"`
	ImageAssetID *string `json:"image_asset_id"`
	Version      *uint64 `json:"version"`
	OrderVersion *uint64 `json:"order_version"`
}

type projectHideRequest struct {
	Version      *uint64 `json:"version"`
	OrderVersion *uint64 `json:"order_version"`
}

type projectOrderRequest struct {
	OrderVersion *uint64   `json:"order_version"`
	PublicIDs    *[]uint64 `json:"public_ids"`
}

// projectResponse is deliberately explicit rather than serializing the
// domain value: nullable fields are required by the HTTP contract even when
// their values are nil.
type projectResponse struct {
	ID              uint64    `json:"id"`
	Name            string    `json:"name"`
	GitHubURL       *string   `json:"github_url"`
	ImageAssetID    *string   `json:"image_asset_id"`
	ImagePreviewURL *string   `json:"image_preview_url"`
	Status          string    `json:"status"`
	SortOrder       *int64    `json:"sort_order"`
	Version         uint64    `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type projectGroupsResponse struct {
	Public       []projectResponse `json:"public"`
	Hidden       []projectResponse `json:"hidden"`
	OrderVersion uint64            `json:"order_version"`
}

type orderResponse struct {
	OrderVersion uint64 `json:"order_version"`
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticate(w, r, false); !ok {
		return
	}
	groups, err := h.projects.ListGroups(r.Context())
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	response := projectGroupsResponse{
		Public:       mapProjects(groups.Public),
		Hidden:       mapProjects(groups.Hidden),
		OrderVersion: groups.OrderVersion,
	}
	h.writeJSON(w, http.StatusOK, response)
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	request, ok := h.decodeProjectCreate(w, r)
	if !ok {
		return
	}
	result, err := h.projects.Create(r.Context(), project.CreateRequest{Name: *request.Name})
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, mapProject(result))
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticate(w, r, false); !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	result, err := h.projects.Get(r.Context(), id)
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, mapProject(result))
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	request, ok := h.decodeProjectUpdate(w, r)
	if !ok {
		return
	}
	result, err := h.projects.Update(r.Context(), id, project.UpdateRequest{
		Name:         *request.Name,
		GitHubURL:    request.GitHubURL,
		ImageAssetID: request.ImageAssetID,
		Version:      *request.Version,
	})
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	if result.Status == project.StatusPublic {
		h.invalidatePublic(r.Context())
	}
	h.writeJSON(w, http.StatusOK, mapProject(result))
}

func (h *Handler) publishProject(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	request, ok := h.decodeProjectPublish(w, r)
	if !ok {
		return
	}
	result, _, err := h.projects.Publish(r.Context(), id, project.PublishRequest{
		Name:         *request.Name,
		GitHubURL:    request.GitHubURL,
		ImageAssetID: request.ImageAssetID,
		Version:      *request.Version,
		OrderVersion: *request.OrderVersion,
	})
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	h.invalidatePublic(r.Context())
	h.writeJSON(w, http.StatusOK, mapProject(result))
}

func (h *Handler) hideProject(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	request, ok := h.decodeProjectHide(w, r)
	if !ok {
		return
	}
	if _, err := h.projects.Hide(r.Context(), id, project.HideRequest{
		Version:      *request.Version,
		OrderVersion: *request.OrderVersion,
	}); err != nil {
		h.writeProjectError(w, err)
		return
	}
	h.invalidatePublic(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reorderProjects(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticateMutation(w, r); !ok {
		return
	}
	request, ok := h.decodeProjectOrder(w, r)
	if !ok {
		return
	}
	orderVersion, err := h.projects.Reorder(r.Context(), project.OrderRequest{
		OrderVersion: *request.OrderVersion,
		PublicIDs:    append([]uint64(nil), (*request.PublicIDs)...),
	})
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	h.invalidatePublic(r.Context())
	h.writeJSON(w, http.StatusOK, orderResponse{OrderVersion: orderVersion})
}

func (h *Handler) decodeProjectCreate(w http.ResponseWriter, r *http.Request) (projectCreateRequest, bool) {
	var request projectCreateRequest
	fields, err := decodeObject(w, r, projectBodyLimit, map[string]struct{}{"name": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return projectCreateRequest{}, false
	}
	if !isPresent(fields, "name") || request.Name == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"name": {"name is required"}})
		return projectCreateRequest{}, false
	}
	return request, true
}

func (h *Handler) decodeProjectUpdate(w http.ResponseWriter, r *http.Request) (projectUpdateRequest, bool) {
	var request projectUpdateRequest
	fields, err := decodeObject(w, r, projectBodyLimit, map[string]struct{}{
		"name": {}, "github_url": {}, "image_asset_id": {}, "version": {},
	}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return projectUpdateRequest{}, false
	}
	if !isPresent(fields, "name") || request.Name == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"name": {"name is required"}})
		return projectUpdateRequest{}, false
	}
	if !h.requireProjectVersion(w, fields, "version", request.Version) {
		return projectUpdateRequest{}, false
	}
	return request, true
}

func (h *Handler) decodeProjectPublish(w http.ResponseWriter, r *http.Request) (projectPublishRequest, bool) {
	var request projectPublishRequest
	fields, err := decodeObject(w, r, projectBodyLimit, map[string]struct{}{
		"name": {}, "github_url": {}, "image_asset_id": {}, "version": {}, "order_version": {},
	}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return projectPublishRequest{}, false
	}
	if !isPresent(fields, "name") || request.Name == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"name": {"name is required"}})
		return projectPublishRequest{}, false
	}
	if !isPresent(fields, "github_url") || request.GitHubURL == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"github_url": {"github_url is required and cannot be null"}})
		return projectPublishRequest{}, false
	}
	if !isPresent(fields, "image_asset_id") {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"image_asset_id": {"image_asset_id is required"}})
		return projectPublishRequest{}, false
	}
	if !h.requireProjectVersion(w, fields, "version", request.Version) {
		return projectPublishRequest{}, false
	}
	if !h.requireProjectVersion(w, fields, "order_version", request.OrderVersion) {
		return projectPublishRequest{}, false
	}
	return request, true
}

func (h *Handler) decodeProjectHide(w http.ResponseWriter, r *http.Request) (projectHideRequest, bool) {
	var request projectHideRequest
	fields, err := decodeObject(w, r, projectBodyLimit, map[string]struct{}{"version": {}, "order_version": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return projectHideRequest{}, false
	}
	if !h.requireProjectVersion(w, fields, "version", request.Version) {
		return projectHideRequest{}, false
	}
	if !h.requireProjectVersion(w, fields, "order_version", request.OrderVersion) {
		return projectHideRequest{}, false
	}
	return request, true
}

func (h *Handler) decodeProjectOrder(w http.ResponseWriter, r *http.Request) (projectOrderRequest, bool) {
	var request projectOrderRequest
	fields, err := decodeObject(w, r, projectBodyLimit, map[string]struct{}{"order_version": {}, "public_ids": {}}, &request)
	if err != nil {
		h.writeDecodeError(w, err)
		return projectOrderRequest{}, false
	}
	if !h.requireProjectVersion(w, fields, "order_version", request.OrderVersion) {
		return projectOrderRequest{}, false
	}
	if !isPresent(fields, "public_ids") || request.PublicIDs == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"public_ids": {"public_ids is required and cannot be null"}})
		return projectOrderRequest{}, false
	}
	return request, true
}

func (h *Handler) requireProjectVersion(w http.ResponseWriter, fields map[string]json.RawMessage, name string, value *uint64) bool {
	if !isPresent(fields, name) || value == nil {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{name: {name + " is required"}})
		return false
	}
	if *value == 0 {
		h.problem(w, http.StatusUnprocessableEntity, "validation_failed", map[string][]string{name: {"must be greater than 0"}})
		return false
	}
	return true
}

func parseProjectID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	value := r.PathValue("id")
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		problemForRequest(w, http.StatusBadRequest, "invalid_request", map[string][]string{"id": {"id must be a positive integer"}})
		return 0, false
	}
	return id, true
}

func (h *Handler) writeProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, project.ErrNotFound):
		h.problem(w, http.StatusNotFound, "not_found", nil)
	case errors.Is(err, project.ErrConflict):
		h.problem(w, http.StatusConflict, "version_conflict", nil)
	case errors.Is(err, project.ErrValidationFailed):
		h.problem(w, http.StatusUnprocessableEntity, "validation_failed", nil)
	case errors.Is(err, project.ErrGitHubRepositoryInvalid):
		h.problem(w, http.StatusUnprocessableEntity, "github_repository_invalid", nil)
	case errors.Is(err, project.ErrGitHubVerificationUnavailable):
		h.problem(w, http.StatusServiceUnavailable, "github_verification_unavailable", nil)
	case errors.Is(err, project.ErrStorage), errors.Is(err, project.ErrDependency):
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
	default:
		h.problem(w, http.StatusInternalServerError, "internal_error", nil)
	}
}

func mapProjects(values []project.Project) []projectResponse {
	result := make([]projectResponse, 0, len(values))
	for _, value := range values {
		result = append(result, mapProject(value))
	}
	return result
}

func mapProject(value project.Project) projectResponse {
	return projectResponse{
		ID:              value.ID,
		Name:            value.Name,
		GitHubURL:       value.GitHubURL,
		ImageAssetID:    value.ImageAssetID,
		ImagePreviewURL: value.ImagePreviewURL,
		Status:          string(value.Status),
		SortOrder:       value.SortOrder,
		Version:         value.Version,
		CreatedAt:       value.CreatedAt.UTC(),
		UpdatedAt:       value.UpdatedAt.UTC(),
	}
}
