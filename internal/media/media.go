// Package media contains the image upload, normalization, storage, and
// publication-aware read policy used by the media handlers.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/image/webp"
	"mywebsite/internal/database/dbgen"
)

var (
	// ErrNotFound is returned for an unknown resource and for a resource that
	// is not publicly referenced. The two cases intentionally share one error
	// so the public handler cannot disclose whether a private asset exists.
	ErrNotFound = errors.New("not_found")

	// ErrUploadTooLarge means the input exceeded MaxUploadBytes.
	ErrUploadTooLarge = errors.New("upload_too_large")

	// ErrUploadTypeInvalid means the file header or image decoder rejected the
	// input, including unsupported formats such as GIF and SVG.
	ErrUploadTypeInvalid = errors.New("upload_type_invalid")

	// ErrImageDimensionsExceeded means the decoded image exceeds either image
	// dimension limit configured on the service.
	ErrImageDimensionsExceeded = errors.New("image_dimensions_exceeded")

	// ErrInvalidActor means the caller did not provide a valid administrator ID.
	ErrInvalidActor = errors.New("invalid_actor")

	// ErrStorage is used for filesystem and persistence failures. It is kept
	// independent from HTTP status codes so adapters can choose their mapping.
	ErrStorage = errors.New("storage failure")
)

// Compatibility aliases keep the error vocabulary useful at adapter call
// sites without introducing a second error identity.
var (
	ErrInvalidUploadType = ErrUploadTypeInvalid
	ErrImageSizeExceeded = ErrImageDimensionsExceeded
	ErrStorageFailure    = ErrStorage
	ErrMediaNotFound     = ErrNotFound
	ErrUploadTooBig      = ErrUploadTooLarge
)

// ErrorCode returns the stable machine code represented by err. It is a
// convenience for HTTP adapters; errors.Is remains the authoritative mapping.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrUploadTooLarge):
		return "upload_too_large"
	case errors.Is(err, ErrUploadTypeInvalid):
		return "upload_type_invalid"
	case errors.Is(err, ErrImageDimensionsExceeded):
		return "image_dimensions_exceeded"
	case errors.Is(err, ErrInvalidActor):
		return "invalid_actor"
	case errors.Is(err, ErrStorage):
		return "storage_failure"
	default:
		return ""
	}
}

// Store is the smallest database contract needed by Service. The generated
// dbgen parameter and row types remain at this boundary so a real *dbgen.Queries
// and focused test fakes can both be used without a global database handle.
type Store interface {
	CreateMediaAsset(context.Context, dbgen.CreateMediaAssetParams) (sql.Result, error)
	GetMediaAssetByID(context.Context, string) (dbgen.MediaAsset, error)
	IsMediaReferencedByPublishedArticle(context.Context, string) (bool, error)
	IsMediaReferencedByPublicProject(context.Context, sql.NullString) (bool, error)
}

// Clock supplies the current time to code that needs deterministic tests.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

// Now implements Clock.
func (f ClockFunc) Now() time.Time {
	if f == nil {
		return time.Time{}
	}
	return f()
}

// Config contains the media limits and filesystem root. All limits are signed
// so validation can reject both zero and negative values at construction time.
type Config struct {
	UploadDir      string
	MaxUploadBytes int64
	MaxImagePixels int64
	MaxImageEdge   int64
	// Clock accepts a Clock implementation or func() time.Time. A nil value
	// uses UTC wall-clock time.
	Clock       any
	ULIDEntropy io.Reader
}

// DefaultConfig provides the limits from the project design. Callers still
// need to pass a non-empty upload directory and may override any limit.
func DefaultConfig(uploadDir string) Config {
	return Config{
		UploadDir:      uploadDir,
		MaxUploadBytes: 5 * 1024 * 1024,
		MaxImagePixels: 20_000_000,
		MaxImageEdge:   8_000,
	}
}

// Option customizes dependencies without expanding the public operation API.
type Option func(*Service)

// WithClock accepts either a Clock or a func() time.Time. The function form is
// convenient in small unit tests and mirrors the other domain packages.
func WithClock(value any) Option {
	return func(service *Service) {
		switch clock := value.(type) {
		case Clock:
			if clock != nil {
				service.clock = clock
			}
		case func() time.Time:
			if clock != nil {
				service.clock = ClockFunc(clock)
			}
		case ClockFunc:
			if clock != nil {
				service.clock = clock
			}
		}
	}
}

// WithNow is an alias for WithClock.
func WithNow(value any) Option { return WithClock(value) }

// WithULIDEntropy replaces the entropy source used for generated IDs.
func WithULIDEntropy(entropy io.Reader) Option {
	return func(service *Service) {
		if entropy != nil {
			service.ulidEntropy = entropy
		}
	}
}

// WithEntropy is an alias for WithULIDEntropy.
func WithEntropy(entropy io.Reader) Option { return WithULIDEntropy(entropy) }

// Service implements upload normalization and publication-aware file access.
type Service struct {
	store       Store
	uploadDir   string
	maxBytes    int64
	maxPixels   int64
	maxEdge     int64
	clock       Clock
	ulidEntropy io.Reader
}

// ServiceConfig is an explicit constructor form for callers that prefer one
// configuration object containing the Store dependency.
type ServiceConfig struct {
	Store Store
	Config
}

// NewService validates the limits and upload root before returning a service.
// The root is made absolute once so Open can return safe absolute paths.
func NewService(store Store, config Config, options ...Option) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: media store is nil", ErrStorage)
	}
	if config.UploadDir == "" {
		return nil, fmt.Errorf("%w: upload directory is empty", ErrStorage)
	}
	if config.MaxUploadBytes <= 0 || config.MaxImagePixels <= 0 || config.MaxImageEdge <= 0 {
		return nil, fmt.Errorf("%w: media limits must be positive", ErrStorage)
	}
	uploadDir, err := filepath.Abs(config.UploadDir)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve upload directory: %v", ErrStorage, err)
	}
	clock, err := clockValue(config.Clock)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid clock: %v", ErrStorage, err)
	}
	service := &Service{
		store:       store,
		uploadDir:   filepath.Clean(uploadDir),
		maxBytes:    config.MaxUploadBytes,
		maxPixels:   config.MaxImagePixels,
		maxEdge:     config.MaxImageEdge,
		clock:       clock,
		ulidEntropy: config.ULIDEntropy,
	}
	if service.clock == nil {
		service.clock = realClock{}
	}
	if service.ulidEntropy == nil {
		service.ulidEntropy = rand.Reader
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	if service.clock == nil {
		service.clock = realClock{}
	}
	if service.ulidEntropy == nil {
		service.ulidEntropy = rand.Reader
	}
	return service, nil
}

func clockValue(value any) (Clock, error) {
	if value == nil {
		return realClock{}, nil
	}
	switch clock := value.(type) {
	case Clock:
		if clock == nil {
			return realClock{}, nil
		}
		return clock, nil
	case func() time.Time:
		if clock == nil {
			return realClock{}, nil
		}
		return ClockFunc(clock), nil
	case ClockFunc:
		if clock == nil {
			return realClock{}, nil
		}
		return clock, nil
	default:
		return nil, fmt.Errorf("unsupported clock type %T", value)
	}
}

// New is a concise constructor alias.
func New(store Store, config Config, options ...Option) (*Service, error) {
	return NewService(store, config, options...)
}

// NewServiceWithConfig constructs a service from the explicit config wrapper.
func NewServiceWithConfig(config ServiceConfig, options ...Option) (*Service, error) {
	return NewService(config.Store, config.Config, options...)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Asset is the public domain representation returned after a successful
// upload or open. SHA256 is the digest of the normalized bytes on disk.
type Asset struct {
	ID                string
	PreviewURL        string
	MarkdownReference string
	SourceMediaType   string
	StoredMediaType   string
	ByteSize          uint64
	Width             uint32
	Height            uint32
	SHA256            []byte
	// Sha256 is retained as an initialism-compatible alias for dbgen callers.
	Sha256    []byte
	CreatedAt time.Time
}

// MediaAsset is a descriptive alias for Asset.
type MediaAsset = Asset

// OpenResult contains safe file access information and the corresponding
// metadata. Path and FilePath carry the same absolute path for adapters that
// use either naming convention.
type OpenResult struct {
	Asset    Asset
	Path     string
	FilePath string

	ID              string
	StorageKey      string
	SourceMediaType string
	StoredMediaType string
	ByteSize        uint64
	Width           uint32
	Height          uint32
	SHA256          []byte
	Sha256          []byte
	CreatedAt       time.Time
}

// OpenedAsset is a descriptive alias for OpenResult.
type OpenedAsset = OpenResult

// Upload reads, validates, normalizes, stores, and registers one image.
func (service *Service) Upload(ctx context.Context, createdBy uint64, reader io.Reader) (Asset, error) {
	if service == nil || service.store == nil {
		return Asset{}, fmt.Errorf("%w: media service is not initialized", ErrStorage)
	}
	if createdBy == 0 {
		return Asset{}, ErrInvalidActor
	}
	if reader == nil {
		return Asset{}, fmt.Errorf("%w: nil upload reader", ErrUploadTypeInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	input, err := readUpload(reader, service.maxBytes)
	if err != nil {
		return Asset{}, err
	}
	sourceType := detectMediaType(input)
	if sourceType == "" {
		return Asset{}, ErrUploadTypeInvalid
	}
	if sourceType == "image/webp" && webpIsAnimated(input) {
		return Asset{}, ErrUploadTypeInvalid
	}

	decodedConfig, decodeConfig, err := decodeImageConfig(sourceType, input)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: decode config: %v", ErrUploadTypeInvalid, err)
	}
	if err := checkDimensions(decodedConfig, service.maxPixels, service.maxEdge); err != nil {
		return Asset{}, err
	}
	decoded, err := decodeConfig(input)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: decode image: %v", ErrUploadTypeInvalid, err)
	}
	orientation := 1
	if sourceType == "image/jpeg" {
		orientation = jpegOrientation(input)
		decoded = applyOrientation(decoded, orientation)
	}

	storedType, normalized, err := normalizeImage(sourceType, decoded)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: encode normalized image: %v", ErrStorage, err)
	}
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if width <= 0 || height <= 0 || uint64(width) > math.MaxUint32 || uint64(height) > math.MaxUint32 {
		return Asset{}, fmt.Errorf("%w: normalized image dimensions are invalid", ErrUploadTypeInvalid)
	}

	now := service.clock.Now().UTC()
	assetID, err := service.newID(now)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: generate asset id: %v", ErrStorage, err)
	}
	extension := ".jpg"
	if storedType == "image/png" {
		extension = ".png"
	}
	storageKey := fmt.Sprintf("%04d/%02d/%s%s", now.Year(), int(now.Month()), assetID, extension)
	finalPath, err := service.storagePath(storageKey)
	if err != nil {
		return Asset{}, err
	}
	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0o750); err != nil {
		return Asset{}, fmt.Errorf("%w: create upload directory: %v", ErrStorage, err)
	}

	temporaryDir := filepath.Join(service.uploadDir, temporaryDirectoryName)
	if err := os.MkdirAll(temporaryDir, 0o750); err != nil {
		return Asset{}, fmt.Errorf("%w: create temporary upload directory: %v", ErrStorage, err)
	}
	temp, err := os.CreateTemp(temporaryDir, ".media-upload-*")
	if err != nil {
		return Asset{}, fmt.Errorf("%w: create temporary file: %v", ErrStorage, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := writeAndClose(temp, normalized); err != nil {
		return Asset{}, fmt.Errorf("%w: write temporary file: %v", ErrStorage, err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil && !isWindows() {
		return Asset{}, fmt.Errorf("%w: set temporary file permissions: %v", ErrStorage, err)
	}
	storedBytes, err := os.ReadFile(tempPath)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: read normalized file for metadata: %v", ErrStorage, err)
	}
	digest := sha256.Sum256(storedBytes)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return Asset{}, fmt.Errorf("%w: move normalized file into place: %v", ErrStorage, err)
	}
	removeTemp = false

	params := dbgen.CreateMediaAssetParams{
		ID:              assetID,
		StorageKey:      storageKey,
		SourceMediaType: sourceType,
		StoredMediaType: storedType,
		ByteSize:        uint64(len(storedBytes)),
		Width:           uint32(width),
		Height:          uint32(height),
		Sha256:          append([]byte(nil), digest[:]...),
		CreatedBy:       createdBy,
		CreatedAt:       now,
	}
	if _, err := service.store.CreateMediaAsset(ctx, params); err != nil {
		_ = os.Remove(finalPath)
		return Asset{}, fmt.Errorf("%w: create media metadata: %v", ErrStorage, err)
	}
	return assetFromParams(params), nil
}

// Open loads metadata and applies administrator/public reference policy. The
// returned path has already passed the storage-key containment check.
func (service *Service) Open(ctx context.Context, id string, authenticatedAdmin bool) (OpenResult, error) {
	if service == nil || service.store == nil {
		return OpenResult{}, fmt.Errorf("%w: media service is not initialized", ErrStorage)
	}
	if id == "" {
		return OpenResult{}, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	row, err := service.store.GetMediaAssetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OpenResult{}, ErrNotFound
		}
		return OpenResult{}, fmt.Errorf("%w: read media metadata: %v", ErrStorage, err)
	}
	if !authenticatedAdmin {
		articleReferenced, err := service.store.IsMediaReferencedByPublishedArticle(ctx, id)
		if err != nil {
			return OpenResult{}, fmt.Errorf("%w: read published article reference: %v", ErrStorage, err)
		}
		if !articleReferenced {
			projectReferenced, err := service.store.IsMediaReferencedByPublicProject(ctx, sql.NullString{String: id, Valid: true})
			if err != nil {
				return OpenResult{}, fmt.Errorf("%w: read public project reference: %v", ErrStorage, err)
			}
			if !projectReferenced {
				return OpenResult{}, ErrNotFound
			}
		}
	}
	path, err := service.storagePath(row.StorageKey)
	if err != nil {
		return OpenResult{}, ErrNotFound
	}
	asset := assetFromDB(row)
	return OpenResult{
		Asset:           asset,
		Path:            path,
		FilePath:        path,
		ID:              asset.ID,
		StorageKey:      row.StorageKey,
		SourceMediaType: asset.SourceMediaType,
		StoredMediaType: asset.StoredMediaType,
		ByteSize:        asset.ByteSize,
		Width:           asset.Width,
		Height:          asset.Height,
		SHA256:          append([]byte(nil), asset.SHA256...),
		Sha256:          append([]byte(nil), asset.SHA256...),
		CreatedAt:       asset.CreatedAt,
	}, nil
}

// OpenAsset is an explicit alias for Open.
func (service *Service) OpenAsset(ctx context.Context, id string, authenticatedAdmin bool) (OpenResult, error) {
	return service.Open(ctx, id, authenticatedAdmin)
}

func (service *Service) newID(now time.Time) (string, error) {
	id, err := ulid.New(ulid.Timestamp(now.UTC()), service.ulidEntropy)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func readUpload(reader io.Reader, maxBytes int64) ([]byte, error) {
	limit := maxBytes
	if limit < math.MaxInt64 {
		limit++
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, fmt.Errorf("%w: read upload: %v", ErrUploadTypeInvalid, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrUploadTooLarge
	}
	return data, nil
}

func detectMediaType(data []byte) string {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		return ""
	}
}

func decodeImageConfig(mediaType string, data []byte) (image.Config, func([]byte) (image.Image, error), error) {
	switch mediaType {
	case "image/jpeg":
		config, err := jpeg.DecodeConfig(bytes.NewReader(data))
		return config, func(value []byte) (image.Image, error) { return jpeg.Decode(bytes.NewReader(value)) }, err
	case "image/png":
		config, err := png.DecodeConfig(bytes.NewReader(data))
		return config, func(value []byte) (image.Image, error) { return png.Decode(bytes.NewReader(value)) }, err
	case "image/webp":
		config, err := webp.DecodeConfig(bytes.NewReader(data))
		return config, func(value []byte) (image.Image, error) { return webp.Decode(bytes.NewReader(value)) }, err
	default:
		return image.Config{}, nil, ErrUploadTypeInvalid
	}
}

func checkDimensions(config image.Config, maxPixels, maxEdge int64) error {
	if config.Width <= 0 || config.Height <= 0 {
		return ErrUploadTypeInvalid
	}
	width, height := uint64(config.Width), uint64(config.Height)
	if width > uint64(maxEdge) || height > uint64(maxEdge) {
		return ErrImageDimensionsExceeded
	}
	if width > uint64(maxPixels)/height {
		return ErrImageDimensionsExceeded
	}
	return nil
}

func normalizeImage(sourceType string, decoded image.Image) (string, []byte, error) {
	var output bytes.Buffer
	storedType := sourceType
	if sourceType == "image/webp" {
		if hasTransparency(decoded) {
			storedType = "image/png"
		} else {
			storedType = "image/jpeg"
		}
	}
	switch storedType {
	case "image/png":
		if err := png.Encode(&output, decoded); err != nil {
			return "", nil, err
		}
	case "image/jpeg":
		if err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90}); err != nil {
			return "", nil, err
		}
	default:
		return "", nil, fmt.Errorf("unsupported normalized type %q", storedType)
	}
	return storedType, output.Bytes(), nil
}

func hasTransparency(img image.Image) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha != math.MaxUint16 {
				return true
			}
		}
	}
	return false
}

func writeAndClose(file *os.File, data []byte) error {
	if file == nil {
		return errors.New("temporary file is nil")
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func assetFromParams(params dbgen.CreateMediaAssetParams) Asset {
	return Asset{
		ID:                params.ID,
		PreviewURL:        "/api/v1/media/" + params.ID,
		MarkdownReference: "![请填写图片说明](/media/" + params.ID + ")",
		SourceMediaType:   params.SourceMediaType,
		StoredMediaType:   params.StoredMediaType,
		ByteSize:          params.ByteSize,
		Width:             params.Width,
		Height:            params.Height,
		SHA256:            append([]byte(nil), params.Sha256...),
		Sha256:            append([]byte(nil), params.Sha256...),
		CreatedAt:         params.CreatedAt.UTC(),
	}
}

func assetFromDB(row dbgen.MediaAsset) Asset {
	return Asset{
		ID:                row.ID,
		PreviewURL:        "/api/v1/media/" + row.ID,
		MarkdownReference: "![请填写图片说明](/media/" + row.ID + ")",
		SourceMediaType:   row.SourceMediaType,
		StoredMediaType:   row.StoredMediaType,
		ByteSize:          row.ByteSize,
		Width:             row.Width,
		Height:            row.Height,
		SHA256:            append([]byte(nil), row.Sha256...),
		Sha256:            append([]byte(nil), row.Sha256...),
		CreatedAt:         row.CreatedAt.UTC(),
	}
}

var storageKeyPattern = regexp.MustCompile(`^[0-9]{4}/(?:0[1-9]|1[0-2])/[0-9A-HJKMNP-TV-Z]{26}\.(?:jpg|png)$`)

const temporaryDirectoryName = ".tmp"

func (service *Service) storagePath(storageKey string) (string, error) {
	if service == nil || service.uploadDir == "" || !storageKeyPattern.MatchString(storageKey) || strings.Contains(storageKey, `\`) {
		return "", ErrNotFound
	}
	root, err := filepath.Abs(service.uploadDir)
	if err != nil {
		return "", ErrNotFound
	}
	root = filepath.Clean(root)
	candidate := filepath.Clean(filepath.Join(root, filepath.FromSlash(storageKey)))
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", ErrNotFound
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrNotFound
	}
	return candidate, nil
}

func webpIsAnimated(data []byte) bool {
	if len(data) < 12 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return false
	}
	for offset := 12; offset+8 <= len(data); {
		chunkType := data[offset : offset+4]
		chunkSize := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		if bytes.Equal(chunkType, []byte("VP8X")) && chunkSize >= 1 && offset+8 < len(data) {
			// The animation feature bit in a VP8X flags byte is 0x02.
			if data[offset+8]&0x02 != 0 {
				return true
			}
		}
		if bytes.Equal(chunkType, []byte("ANIM")) || bytes.Equal(chunkType, []byte("ANMF")) {
			return true
		}
		next := uint64(offset) + 8 + chunkSize
		if next > uint64(len(data)) {
			return false
		}
		if chunkSize&1 != 0 {
			next++
		}
		if next > uint64(len(data)) {
			return false
		}
		offset = int(next)
	}
	return false
}

func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+1 < len(data); {
		if data[offset] != 0xff {
			return 1
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			return 1
		}
		marker := data[offset]
		offset++
		if marker == 0xd9 || marker == 0xda {
			return 1
		}
		if marker == 0xd8 || marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if offset+2 > len(data) {
			return 1
		}
		segmentLength := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if segmentLength < 2 || segmentLength > len(data)-offset {
			return 1
		}
		if marker == 0xe1 && segmentLength >= 8 && bytes.Equal(data[offset+2:offset+8], []byte("Exif\x00\x00")) {
			if orientation, ok := parseTIFFOrientation(data[offset+8 : offset+segmentLength]); ok {
				return orientation
			}
		}
		offset += segmentLength
	}
	return 1
}

func parseTIFFOrientation(data []byte) (int, bool) {
	if len(data) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, false
	}
	ifdOffset := uint64(order.Uint32(data[4:8]))
	if ifdOffset > uint64(len(data)-2) {
		return 0, false
	}
	ifd := int(ifdOffset)
	count := int(order.Uint16(data[ifd : ifd+2]))
	for index := 0; index < count; index++ {
		entryOffset64 := uint64(ifd) + 2 + uint64(index)*12
		if entryOffset64+12 > uint64(len(data)) {
			return 0, false
		}
		entryOffset := int(entryOffset64)
		if order.Uint16(data[entryOffset:entryOffset+2]) != 0x0112 {
			continue
		}
		if order.Uint16(data[entryOffset+2:entryOffset+4]) != 3 || order.Uint32(data[entryOffset+4:entryOffset+8]) < 1 {
			return 0, false
		}
		orientation := int(order.Uint16(data[entryOffset+8 : entryOffset+10]))
		if orientation >= 1 && orientation <= 8 {
			return orientation, true
		}
		return 0, false
	}
	return 0, false
}

func applyOrientation(src image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return src
	}
	bounds := src.Bounds()
	srcWidth, srcHeight := bounds.Dx(), bounds.Dy()
	outWidth, outHeight := srcWidth, srcHeight
	if orientation >= 5 && orientation <= 8 {
		outWidth, outHeight = srcHeight, srcWidth
	}
	dst := image.NewNRGBA(image.Rect(0, 0, outWidth, outHeight))
	for y := 0; y < outHeight; y++ {
		for x := 0; x < outWidth; x++ {
			srcX, srcY := x, y
			switch orientation {
			case 2:
				srcX, srcY = srcWidth-1-x, y
			case 3:
				srcX, srcY = srcWidth-1-x, srcHeight-1-y
			case 4:
				srcX, srcY = x, srcHeight-1-y
			case 5:
				srcX, srcY = y, x
			case 6:
				srcX, srcY = y, srcHeight-1-x
			case 7:
				srcX, srcY = srcWidth-1-y, srcHeight-1-x
			case 8:
				srcX, srcY = srcWidth-1-y, x
			}
			dst.Set(x, y, src.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return dst
}

func isWindows() bool {
	return filepath.Separator == '\\'
}
