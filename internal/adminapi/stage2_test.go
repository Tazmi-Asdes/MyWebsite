package adminapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/markdown"
	"mywebsite/internal/media"
	"mywebsite/internal/project"
)

type stage2ProjectFake struct {
	groups       project.ProjectGroups
	created      project.Project
	updated      project.Project
	published    project.Project
	createErr    error
	updateErr    error
	publishErr   error
	hideErr      error
	listErr      error
	getErr       error
	orderErr     error
	createInput  project.CreateRequest
	updateID     uint64
	updateInput  project.UpdateRequest
	publishID    uint64
	publishInput project.PublishRequest
	hideID       uint64
	hideInput    project.HideRequest
	orderInput   project.OrderRequest
}

func (service *stage2ProjectFake) ListGroups(context.Context) (project.ProjectGroups, error) {
	return service.groups, service.listErr
}

func (service *stage2ProjectFake) Create(_ context.Context, input project.CreateRequest) (project.Project, error) {
	service.createInput = input
	return service.created, service.createErr
}

func (service *stage2ProjectFake) Get(context.Context, uint64) (project.Project, error) {
	return service.updated, service.getErr
}

func (service *stage2ProjectFake) Update(_ context.Context, id uint64, input project.UpdateRequest) (project.Project, error) {
	service.updateID, service.updateInput = id, input
	return service.updated, service.updateErr
}

func (service *stage2ProjectFake) Publish(_ context.Context, id uint64, input project.PublishRequest) (project.Project, uint64, error) {
	service.publishID, service.publishInput = id, input
	return service.published, 8, service.publishErr
}

func (service *stage2ProjectFake) Hide(_ context.Context, id uint64, input project.HideRequest) (uint64, error) {
	service.hideID, service.hideInput = id, input
	return 9, service.hideErr
}

func (service *stage2ProjectFake) Reorder(_ context.Context, input project.OrderRequest) (uint64, error) {
	service.orderInput = input
	return 10, service.orderErr
}

type stage2MediaFake struct {
	asset       media.Asset
	uploadErr   error
	open        media.OpenResult
	openErr     error
	uploaded    []byte
	uploadActor uint64
	openID      string
	openAdmin   bool
}

func (service *stage2MediaFake) Upload(_ context.Context, actor uint64, reader io.Reader) (media.Asset, error) {
	service.uploadActor = actor
	if service.uploadErr != nil {
		return media.Asset{}, service.uploadErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return media.Asset{}, err
	}
	service.uploaded = data
	return service.asset, nil
}

func (service *stage2MediaFake) Open(_ context.Context, id string, admin bool) (media.OpenResult, error) {
	service.openID, service.openAdmin = id, admin
	return service.open, service.openErr
}

func stage2Handler(t *testing.T, projects ProjectService, mediaService MediaService) *Handler {
	t.Helper()
	handler, err := NewHandler(HandlerOptions{
		Auth:          &fakeAuthService{authSession: authenticatedSession()},
		Articles:      &fakeArticleService{},
		Projects:      projects,
		Media:         mediaService,
		PublicBaseURL: "https://example.test",
		Now:           func() time.Time { return time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func stage2Request(method, path, body string, mutation bool) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	if mutation {
		request.Header.Set("Origin", "https://example.test")
		request.Header.Set("X-CSRF-Token", "csrf-token")
	}
	return request
}

func TestStage2ProjectRoutesAndMutationAuthentication(t *testing.T) {
	service := &stage2ProjectFake{
		groups: project.ProjectGroups{
			Public:       []project.Project{{ID: 1, Name: "Public", Status: project.StatusPublic, Version: 2}},
			Hidden:       []project.Project{{ID: 2, Name: "Hidden", Status: project.StatusHidden, Version: 1}},
			OrderVersion: 7,
		},
		created:   project.Project{ID: 3, Name: "Created", Status: project.StatusHidden, Version: 1},
		published: project.Project{ID: 3, Name: "Published", Status: project.StatusPublic, Version: 2},
	}
	handler := stage2Handler(t, service, nil)

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, stage2Request(http.MethodGet, "/api/v1/projects", "", false))
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"github_url":null`) {
		t.Fatalf("GET projects = %d, body=%s", listResponse.Code, listResponse.Body.String())
	}

	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, stage2Request(http.MethodPost, "/api/v1/projects", `{"name":"New"}`, true))
	if createResponse.Code != http.StatusCreated || service.createInput.Name != "New" {
		t.Fatalf("POST projects = %d, body=%s, input=%+v", createResponse.Code, createResponse.Body.String(), service.createInput)
	}

	missingOrigin := stage2Request(http.MethodPost, "/api/v1/projects", `{"name":"Rejected"}`, false)
	missingOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingOriginResponse, missingOrigin)
	if missingOriginResponse.Code != http.StatusForbidden || !strings.Contains(missingOriginResponse.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("missing Origin = %d, body=%s", missingOriginResponse.Code, missingOriginResponse.Body.String())
	}

	publishResponse := httptest.NewRecorder()
	handler.ServeHTTP(publishResponse, stage2Request(http.MethodPost, "/api/v1/projects/3/publish", `{"name":"Published","github_url":"https://github.com/acme/demo","image_asset_id":null,"version":1,"order_version":7}`, true))
	if publishResponse.Code != http.StatusOK || service.publishID != 3 || service.publishInput.ImageAssetID != nil || service.publishInput.OrderVersion != 7 {
		t.Fatalf("publish = %d, body=%s, input=%+v", publishResponse.Code, publishResponse.Body.String(), service.publishInput)
	}

	hideResponse := httptest.NewRecorder()
	handler.ServeHTTP(hideResponse, stage2Request(http.MethodPost, "/api/v1/projects/3/hide", `{"version":2,"order_version":8}`, true))
	if hideResponse.Code != http.StatusNoContent || hideResponse.Body.Len() != 0 || service.hideID != 3 || service.hideInput.OrderVersion != 8 {
		t.Fatalf("hide = %d, body=%s, input=%+v", hideResponse.Code, hideResponse.Body.String(), service.hideInput)
	}

	orderResponse := httptest.NewRecorder()
	handler.ServeHTTP(orderResponse, stage2Request(http.MethodPut, "/api/v1/projects/order", `{"order_version":9,"public_ids":[3,1]}`, true))
	if orderResponse.Code != http.StatusOK || !strings.Contains(orderResponse.Body.String(), `"order_version":10`) || len(service.orderInput.PublicIDs) != 2 {
		t.Fatalf("order = %d, body=%s, input=%+v", orderResponse.Code, orderResponse.Body.String(), service.orderInput)
	}
}

func TestStage2ProjectVersionZeroIsRejectedBeforeDomain(t *testing.T) {
	service := &stage2ProjectFake{}
	handler := stage2Handler(t, service, nil)
	tests := []struct {
		name  string
		path  string
		body  string
		field string
	}{
		{name: "update version", path: "/api/v1/projects/1", body: `{"name":"Project","version":0}`, field: "version"},
		{name: "publish version", path: "/api/v1/projects/1/publish", body: `{"name":"Project","github_url":"https://github.com/acme/demo","image_asset_id":null,"version":0,"order_version":1}`, field: "version"},
		{name: "hide order version", path: "/api/v1/projects/1/hide", body: `{"version":1,"order_version":0}`, field: "order_version"},
		{name: "order version", path: "/api/v1/projects/order", body: `{"order_version":0,"public_ids":[]}`, field: "order_version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := stage2Request(http.MethodPut, test.path, test.body, true)
			if strings.HasSuffix(test.path, "/publish") || strings.HasSuffix(test.path, "/hide") {
				request.Method = http.MethodPost
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"validation_failed"`) || !strings.Contains(response.Body.String(), `"`+test.field+`"`) {
				t.Fatalf("zero version = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func makeMultipartRequest(t *testing.T, data []byte, filename string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("part.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "https://example.test")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	return request
}

func TestStage2MediaUploadMapsErrorsAndHexEncodesDigest(t *testing.T) {
	digest := sha256.Sum256([]byte("normalized"))
	service := &stage2MediaFake{asset: media.Asset{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", PreviewURL: "/api/v1/media/01ARZ3NDEKTSV4RRFFQ69G5FAV",
		MarkdownReference: "![请填写图片说明](/media/01ARZ3NDEKTSV4RRFFQ69G5FAV)", SourceMediaType: "image/png", StoredMediaType: "image/png",
		ByteSize: 9, Width: 1, Height: 1, SHA256: digest[:], CreatedAt: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
	}}
	handler := stage2Handler(t, nil, service)

	success := httptest.NewRecorder()
	handler.ServeHTTP(success, makeMultipartRequest(t, []byte("png bytes"), "image.png"))
	if success.Code != http.StatusCreated || service.uploadActor != 7 || string(service.uploaded) != "png bytes" {
		t.Fatalf("upload = %d, body=%s, actor=%d, data=%q", success.Code, success.Body.String(), service.uploadActor, service.uploaded)
	}
	var response mediaResponse
	if err := json.Unmarshal(success.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if response.SHA256 != hex.EncodeToString(digest[:]) || len(response.SHA256) != 64 {
		t.Fatalf("sha256 = %q", response.SHA256)
	}

	for _, test := range []struct {
		name string
		err  error
		code int
		want string
	}{
		{name: "too large", err: media.ErrUploadTooLarge, code: http.StatusRequestEntityTooLarge, want: "upload_too_large"},
		{name: "type", err: media.ErrUploadTypeInvalid, code: http.StatusUnsupportedMediaType, want: "upload_type_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service.uploadErr = test.err
			failed := httptest.NewRecorder()
			handler.ServeHTTP(failed, makeMultipartRequest(t, []byte("input"), "image.bin"))
			if failed.Code != test.code || !strings.Contains(failed.Body.String(), `"code":"`+test.want+`"`) {
				t.Fatalf("upload error = %d, body=%s", failed.Code, failed.Body.String())
			}
		})
	}
}

func TestStage2AdminMediaPreviewSetsBinaryHeaders(t *testing.T) {
	content := []byte("PNG DATA")
	digest := sha256.Sum256(content)
	path := t.TempDir() + "/preview.png"
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	service := &stage2MediaFake{open: media.OpenResult{
		Path: path, StoredMediaType: "image/png", SHA256: digest[:],
		CreatedAt: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
	}}
	handler := stage2Handler(t, nil, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/media/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "token.csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Content-Length") != "8" || response.Header().Get("ETag") != `"`+hex.EncodeToString(digest[:])+`"` || response.Header().Get("Cache-Control") != "private, no-cache" || response.Body.String() != string(content) {
		t.Fatalf("preview = %d, headers=%v, body=%q", response.Code, response.Header(), response.Body.String())
	}
	if service.openID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" || !service.openAdmin {
		t.Fatalf("Open() = id %q admin %v", service.openID, service.openAdmin)
	}
}

func TestStage2ArticleMediaErrorsMapToStableCodes(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "invalid reference", err: markdown.ErrInvalidMediaReference, want: "invalid_media_reference"},
		{name: "alt required", err: markdown.ErrImageAltRequired, want: "image_alt_required"},
		{name: "alt invalid", err: markdown.ErrImageAltInvalid, want: "image_alt_invalid"},
		{name: "missing media", err: article.ErrReferencedMediaNotFound, want: "referenced_media_not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := stage2Handler(t, nil, nil)
			handler.articles = &fakeArticleService{publishErr: test.err}
			request := stage2Request(http.MethodPost, "/api/v1/articles/1/publish", `{"title":"Published","body_markdown":"![alt](/media/01ARZ3NDEKTSV4RRFFQ69G5FAV)","version":1}`, true)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"`+test.want+`"`) {
				t.Fatalf("article error = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}
}

var _ ProjectService = (*stage2ProjectFake)(nil)
var _ MediaService = (*stage2MediaFake)(nil)
