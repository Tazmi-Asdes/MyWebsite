package adminapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mywebsite/internal/media"
)

const (
	defaultMediaUploadBytes int64 = 5 * 1024 * 1024
	mediaMultipartOverhead  int64 = 1 * 1024 * 1024
)

type mediaResponse struct {
	ID                string    `json:"id"`
	PreviewURL        string    `json:"preview_url"`
	MarkdownReference string    `json:"markdown_reference"`
	SourceMediaType   string    `json:"source_media_type"`
	StoredMediaType   string    `json:"stored_media_type"`
	ByteSize          uint64    `json:"byte_size"`
	Width             uint32    `json:"width"`
	Height            uint32    `json:"height"`
	SHA256            string    `json:"sha256"`
	CreatedAt         time.Time `json:"created_at"`
}

func (h *Handler) uploadMedia(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.authenticateMutation(w, r)
	if !ok {
		return
	}
	if !isMultipartContentType(r) {
		h.problem(w, http.StatusUnsupportedMediaType, "unsupported_media_type", nil)
		return
	}
	limit := h.maxUploadBytes
	if limit > math.MaxInt64-mediaMultipartOverhead {
		limit = math.MaxInt64
	} else {
		limit += mediaMultipartOverhead
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	multipartReader, err := r.MultipartReader()
	if err != nil {
		if isMaxBytesError(err) {
			h.problem(w, http.StatusRequestEntityTooLarge, "upload_too_large", nil)
		} else {
			h.problem(w, http.StatusBadRequest, "invalid_request", nil)
		}
		return
	}
	part, err := multipartReader.NextPart()
	if err != nil {
		if errors.Is(err, io.EOF) {
			h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"file": {"file is required"}})
		} else if isMaxBytesError(err) {
			h.problem(w, http.StatusRequestEntityTooLarge, "upload_too_large", nil)
		} else {
			h.problem(w, http.StatusBadRequest, "invalid_request", nil)
		}
		return
	}
	defer part.Close()
	if part.FormName() != "file" || part.FileName() == "" {
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"file": {"exactly one file part named file is required"}})
		return
	}
	asset, err := h.media.Upload(r.Context(), session.AdminID, part)
	if err != nil {
		h.writeMediaUploadError(w, err)
		return
	}
	extra, err := multipartReader.NextPart()
	if err == nil {
		_ = extra.Close()
		h.problem(w, http.StatusBadRequest, "invalid_request", map[string][]string{"file": {"exactly one file part named file is required"}})
		return
	}
	if !errors.Is(err, io.EOF) {
		if isMaxBytesError(err) {
			h.problem(w, http.StatusRequestEntityTooLarge, "upload_too_large", nil)
		} else {
			h.problem(w, http.StatusBadRequest, "invalid_request", nil)
		}
		return
	}
	h.writeJSON(w, http.StatusCreated, mapMediaAsset(asset))
}

func (h *Handler) writeMediaUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrUploadTooLarge):
		h.problem(w, http.StatusRequestEntityTooLarge, "upload_too_large", nil)
	case errors.Is(err, media.ErrUploadTypeInvalid):
		h.problem(w, http.StatusUnsupportedMediaType, "upload_type_invalid", nil)
	case errors.Is(err, media.ErrImageDimensionsExceeded):
		h.problem(w, http.StatusUnprocessableEntity, "image_dimensions_exceeded", nil)
	case errors.Is(err, media.ErrInvalidActor):
		h.problem(w, http.StatusUnauthorized, "invalid_actor", nil)
	case errors.Is(err, media.ErrStorage):
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
	default:
		h.problem(w, http.StatusInternalServerError, "internal_error", nil)
	}
}

func (h *Handler) getMedia(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.authenticate(w, r, false); !ok {
		return
	}
	opened, err := h.media.Open(r.Context(), r.PathValue("id"), true)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrNotFound):
			h.problem(w, http.StatusNotFound, "not_found", nil)
		case errors.Is(err, media.ErrStorage):
			h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		default:
			h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		}
		return
	}
	path := opened.Path
	if path == "" {
		path = opened.FilePath
	}
	storedType := opened.StoredMediaType
	if storedType == "" {
		storedType = opened.Asset.StoredMediaType
	}
	if storedType != "image/jpeg" && storedType != "image/png" || path == "" {
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	digest := opened.SHA256
	if len(digest) == 0 {
		digest = opened.Sha256
	}
	if len(digest) == 0 {
		digest = opened.Asset.SHA256
	}
	if len(digest) == 0 {
		digest = opened.Asset.Sha256
	}
	etag, ok := sha256HeaderValue(digest)
	if !ok {
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		h.problem(w, http.StatusServiceUnavailable, "dependency_unavailable", nil)
		return
	}
	createdAt := opened.CreatedAt
	if createdAt.IsZero() {
		createdAt = opened.Asset.CreatedAt
	}
	w.Header().Set("Content-Type", storedType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, filepath.Base(path), createdAt.UTC(), file)
}

func isMultipartContentType(r *http.Request) bool {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	return err == nil && strings.EqualFold(mediaType, "multipart/form-data")
}

func mapMediaAsset(value media.Asset) mediaResponse {
	digest := value.SHA256
	if len(digest) == 0 {
		digest = value.Sha256
	}
	shaValue, _ := sha256Hex(digest)
	return mediaResponse{
		ID:                value.ID,
		PreviewURL:        value.PreviewURL,
		MarkdownReference: value.MarkdownReference,
		SourceMediaType:   value.SourceMediaType,
		StoredMediaType:   value.StoredMediaType,
		ByteSize:          value.ByteSize,
		Width:             value.Width,
		Height:            value.Height,
		SHA256:            shaValue,
		CreatedAt:         value.CreatedAt.UTC(),
	}
}

func sha256HeaderValue(value []byte) (string, bool) {
	hexValue, ok := sha256Hex(value)
	if !ok {
		return "", false
	}
	return `"` + hexValue + `"`, true
}

func sha256Hex(value []byte) (string, bool) {
	if len(value) == sha256.Size {
		return hex.EncodeToString(value), true
	}
	if len(value) == sha256.Size*2 {
		decoded, err := hex.DecodeString(string(value))
		if err == nil && len(decoded) == sha256.Size {
			return strings.ToLower(string(value)), true
		}
	}
	return "", false
}
