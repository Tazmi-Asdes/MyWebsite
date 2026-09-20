package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mywebsite/internal/database/dbgen"
)

type mediaTestResult struct{}

func (mediaTestResult) LastInsertId() (int64, error) { return 0, nil }
func (mediaTestResult) RowsAffected() (int64, error) { return 1, nil }

type mediaFakeStore struct {
	created      dbgen.CreateMediaAssetParams
	row          dbgen.MediaAsset
	createErr    error
	getErr       error
	articleRef   bool
	projectRef   bool
	articleCalls int
	projectCalls int
}

func (store *mediaFakeStore) CreateMediaAsset(_ context.Context, params dbgen.CreateMediaAssetParams) (sql.Result, error) {
	store.created = params
	if store.createErr != nil {
		return nil, store.createErr
	}
	store.row = dbgen.MediaAsset{
		ID:              params.ID,
		StorageKey:      params.StorageKey,
		SourceMediaType: params.SourceMediaType,
		StoredMediaType: params.StoredMediaType,
		ByteSize:        params.ByteSize,
		Width:           params.Width,
		Height:          params.Height,
		Sha256:          append([]byte(nil), params.Sha256...),
		CreatedBy:       params.CreatedBy,
		CreatedAt:       params.CreatedAt,
	}
	return mediaTestResult{}, nil
}

func (store *mediaFakeStore) GetMediaAssetByID(_ context.Context, _ string) (dbgen.MediaAsset, error) {
	if store.getErr != nil {
		return dbgen.MediaAsset{}, store.getErr
	}
	return store.row, nil
}

func (store *mediaFakeStore) IsMediaReferencedByPublishedArticle(context.Context, string) (bool, error) {
	store.articleCalls++
	return store.articleRef, nil
}

func (store *mediaFakeStore) IsMediaReferencedByPublicProject(context.Context, sql.NullString) (bool, error) {
	store.projectCalls++
	return store.projectRef, nil
}

type mediaFixedClock time.Time

func (clock mediaFixedClock) Now() time.Time { return time.Time(clock) }

func newMediaTestService(t *testing.T, store Store, root string) *Service {
	t.Helper()
	service, err := NewService(store, Config{
		UploadDir:      root,
		MaxUploadBytes: 1 << 20,
		MaxImagePixels: 100,
		MaxImageEdge:   20,
		Clock:          mediaFixedClock(time.Date(2026, 9, 20, 3, 4, 5, 0, time.FixedZone("test", 8*60*60))),
		ULIDEntropy:    bytes.NewReader(bytes.Repeat([]byte{0x22}, 10)),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestNewServiceRejectsInvalidConfig(t *testing.T) {
	store := &mediaFakeStore{}
	base := Config{UploadDir: t.TempDir(), MaxUploadBytes: 1, MaxImagePixels: 1, MaxImageEdge: 1}
	cases := []Config{
		{},
		{UploadDir: base.UploadDir, MaxUploadBytes: 0, MaxImagePixels: 1, MaxImageEdge: 1},
		{UploadDir: base.UploadDir, MaxUploadBytes: 1, MaxImagePixels: -1, MaxImageEdge: 1},
		{UploadDir: base.UploadDir, MaxUploadBytes: 1, MaxImagePixels: 1, MaxImageEdge: -1},
	}
	for _, config := range cases {
		if _, err := NewService(store, config); err == nil || !errors.Is(err, ErrStorage) {
			t.Fatalf("NewService(%+v) error = %v, want ErrStorage", config, err)
		}
	}
}

func TestUploadJPEGAppliesExifOrientationAndRegistersFinalBytes(t *testing.T) {
	store := &mediaFakeStore{}
	service := newMediaTestService(t, store, t.TempDir())
	original := encodeJPEG(t, 2, 3)
	oriented := addJPEGOrientation(original, 6)

	asset, err := service.Upload(context.Background(), 7, bytes.NewReader(oriented))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if asset.SourceMediaType != "image/jpeg" || asset.StoredMediaType != "image/jpeg" {
		t.Fatalf("asset media types = %q/%q", asset.SourceMediaType, asset.StoredMediaType)
	}
	if asset.Width != 3 || asset.Height != 2 {
		t.Fatalf("oriented dimensions = %dx%d, want 3x2", asset.Width, asset.Height)
	}
	if asset.PreviewURL != "/api/v1/media/"+asset.ID || asset.MarkdownReference != "![请填写图片说明](/media/"+asset.ID+")" {
		t.Fatalf("asset references = %+v", asset)
	}
	if store.created.CreatedBy != 7 || store.created.StorageKey != "2026/09/"+asset.ID+".jpg" {
		t.Fatalf("created params = %+v", store.created)
	}
	path := filepath.Join(service.uploadDir, filepath.FromSlash(store.created.StorageKey))
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	digest := sha256.Sum256(stored)
	if !bytes.Equal(digest[:], asset.SHA256) || uint64(len(stored)) != asset.ByteSize {
		t.Fatalf("stored digest/size do not match asset: size=%d asset=%d", len(stored), asset.ByteSize)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(stored))
	if err != nil || config.Width != 3 || config.Height != 2 {
		t.Fatalf("normalized JPEG config = %+v, err=%v", config, err)
	}
	temporaryEntries, err := os.ReadDir(filepath.Join(service.uploadDir, temporaryDirectoryName))
	if err != nil {
		t.Fatalf("ReadDir(.tmp) error = %v", err)
	}
	if len(temporaryEntries) != 0 {
		t.Fatalf("temporary directory contains %d entries after successful upload", len(temporaryEntries))
	}
}

func TestUploadPNGReencodesAndRejectsDimensionsAndSize(t *testing.T) {
	store := &mediaFakeStore{}
	service := newMediaTestService(t, store, t.TempDir())
	pngBytes := encodePNG(t, 2, 2)
	asset, err := service.Upload(context.Background(), 1, bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("Upload(PNG) error = %v", err)
	}
	if asset.SourceMediaType != "image/png" || asset.StoredMediaType != "image/png" || filepath.Ext(store.created.StorageKey) != ".png" {
		t.Fatalf("PNG asset = %+v, params = %+v", asset, store.created)
	}
	if _, err := png.Decode(bytes.NewReader(mustReadFile(t, filepath.Join(service.uploadDir, filepath.FromSlash(store.created.StorageKey))))); err != nil {
		t.Fatalf("normalized PNG decode error = %v", err)
	}

	tooLarge := bytes.Repeat([]byte("x"), int(service.maxBytes)+1)
	if _, err := service.Upload(context.Background(), 1, bytes.NewReader(tooLarge)); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("oversize error = %v, want ErrUploadTooLarge", err)
	}

	dimensionService, err := NewService(store, Config{UploadDir: t.TempDir(), MaxUploadBytes: 1 << 20, MaxImagePixels: 4, MaxImageEdge: 2})
	if err != nil {
		t.Fatal(err)
	}
	tooWide := encodePNG(t, 3, 2)
	if _, err := dimensionService.Upload(context.Background(), 1, bytes.NewReader(tooWide)); !errors.Is(err, ErrImageDimensionsExceeded) {
		t.Fatalf("dimension error = %v, want ErrImageDimensionsExceeded", err)
	}
}

func TestUploadDatabaseFailureRemovesNewFile(t *testing.T) {
	store := &mediaFakeStore{createErr: errors.New("database unavailable")}
	service := newMediaTestService(t, store, t.TempDir())
	_, err := service.Upload(context.Background(), 1, bytes.NewReader(encodePNG(t, 1, 1)))
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("Upload() error = %v, want ErrStorage", err)
	}
	path := filepath.Join(service.uploadDir, filepath.FromSlash(store.created.StorageKey))
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed DB write left file: stat=%v", statErr)
	}
}

func TestOpenUsesAdminOrPublicReferenceAuthorization(t *testing.T) {
	store := &mediaFakeStore{row: dbgen.MediaAsset{
		ID:              "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		StorageKey:      "2026/09/01ARZ3NDEKTSV4RRFFQ69G5FAV.png",
		SourceMediaType: "image/png",
		StoredMediaType: "image/png",
		ByteSize:        4,
		Width:           1,
		Height:          1,
		Sha256:          []byte{1, 2, 3},
		CreatedAt:       time.Date(2026, 9, 20, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	}}
	service := newMediaTestService(t, store, t.TempDir())

	if _, err := service.Open(context.Background(), store.row.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private visitor open error = %v, want ErrNotFound", err)
	}
	if store.articleCalls != 1 || store.projectCalls != 1 {
		t.Fatalf("reference calls = article %d, project %d", store.articleCalls, store.projectCalls)
	}

	store.articleRef = true
	opened, err := service.Open(context.Background(), store.row.ID, false)
	if err != nil {
		t.Fatalf("published visitor Open() error = %v", err)
	}
	if opened.Path != filepath.Join(service.uploadDir, filepath.FromSlash(store.row.StorageKey)) || !bytes.Equal(opened.SHA256, store.row.Sha256) {
		t.Fatalf("opened result = %+v", opened)
	}

	store.articleRef = false
	store.projectRef = false
	adminOpened, err := service.Open(context.Background(), store.row.ID, true)
	if err != nil || adminOpened.Path == "" {
		t.Fatalf("admin Open() = %+v, err=%v", adminOpened, err)
	}

	store.row.StorageKey = "../../escape.png"
	if _, err := service.Open(context.Background(), store.row.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("escaped storage key error = %v, want ErrNotFound", err)
	}
}

func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x == 0 && y == 0 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{G: 255, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func addJPEGOrientation(jpegBytes []byte, orientation uint16) []byte {
	tiff := make([]byte, 26)
	tiff[0], tiff[1] = 'I', 'I'
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x0112)
	binary.LittleEndian.PutUint16(tiff[12:14], 3)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint16(tiff[18:20], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := make([]byte, 4+len(payload))
	segment[0], segment[1] = 0xff, 0xe1
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(payload)+2))
	copy(segment[4:], payload)
	return append(append(append([]byte(nil), jpegBytes[:2]...), segment...), jpegBytes[2:]...)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
