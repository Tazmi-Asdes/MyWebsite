package publicsite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"mywebsite/internal/article"
	"mywebsite/internal/media"
	"mywebsite/internal/platform"
	"mywebsite/internal/project"
)

const articlesPerPage int32 = 10

// ArticleReader is the minimum article domain contract needed by the public
// site.  The article service satisfies it without an adapter.
type ArticleReader interface {
	ListPublished(context.Context, int32, int32) ([]article.PublishedArticle, error)
	CountPublished(context.Context) (int64, error)
	GetPublishedByULID(context.Context, string) (article.Article, error)
}

// ProjectReader is the smallest project domain contract needed by the public
// site. The service methods deliberately expose both the home-page featured
// projection and the complete public collection.
type ProjectReader interface {
	ListPublicProjects(context.Context) ([]project.Project, error)
	ListFeatured(context.Context) ([]project.Project, error)
	CountPublicProjects(context.Context) (int64, error)
}

// MediaReader is the public media read contract. Media.Open applies the
// publication/reference policy before returning a filesystem path.
type MediaReader interface {
	Open(context.Context, string, bool) (media.OpenResult, error)
}

// Stage2Options contains the optional domain readers used by the public
// home, projects, about, and media routes. A nil reader keeps the matching
// projection in its early-stage empty state for compatibility with callers
// that only enabled article pages.
type Stage2Options struct {
	ArticleReader ArticleReader
	ProjectReader ProjectReader
	MediaReader   MediaReader
}

// PublicOptions is an explicit alias for callers that prefer a public-site
// focused name when assembling the Stage 2 handler.
type PublicOptions = Stage2Options

// Handler renders the read-only public pages.
type Handler struct {
	templates     *template.Template
	clock         platform.Clock
	articleReader ArticleReader
	projectReader ProjectReader
	mediaReader   MediaReader
}

// NewHandler parses the embedded public templates and returns a renderer.
// The optional article reader keeps the Stage 0 constructor source-compatible
// while allowing the app layer to enable the Stage 1 article pages.
func NewHandler(files fs.FS, clock platform.Clock, readers ...ArticleReader) (*Handler, error) {
	var options Stage2Options
	if len(readers) > 0 {
		options.ArticleReader = readers[0]
	}
	return newHandler(files, clock, options)
}

// NewHandlerWithStage2 constructs the public handler with the complete Stage
// 2 reader set while keeping all readers optional for incremental rollout.
func NewHandlerWithStage2(files fs.FS, clock platform.Clock, options Stage2Options) (*Handler, error) {
	return newHandler(files, clock, options)
}

// NewHandlerStage2 is a concise compatibility alias for the Stage 2
// constructor.
func NewHandlerStage2(files fs.FS, clock platform.Clock, options Stage2Options) (*Handler, error) {
	return NewHandlerWithStage2(files, clock, options)
}

// NewHandlerWithOptions is an explicit options-named alias for callers that
// use constructor naming conventions shared by other HTTP adapters.
func NewHandlerWithOptions(files fs.FS, clock platform.Clock, options Stage2Options) (*Handler, error) {
	return NewHandlerWithStage2(files, clock, options)
}

func newHandler(files fs.FS, clock platform.Clock, options Stage2Options) (*Handler, error) {
	if files == nil {
		return nil, fmt.Errorf("public template filesystem is nil")
	}
	if clock == nil {
		clock = platform.NewShanghaiClock()
	}
	templates, err := template.New("public").Option("missingkey=error").ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse public templates: %w", err)
	}
	return &Handler{
		templates:     templates,
		clock:         clock,
		articleReader: options.ArticleReader,
		projectReader: options.ProjectReader,
		mediaReader:   options.MediaReader,
	}, nil
}

// NewHandlerWithArticles is an explicit constructor for callers that prefer
// not to use the optional argument on NewHandler.
func NewHandlerWithArticles(files fs.FS, clock platform.Clock, reader ArticleReader) (*Handler, error) {
	return NewHandler(files, clock, reader)
}

// RegisterRoutes registers the public GET routes on a Go 1.22+ ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /media/{id}", http.HandlerFunc(h.media))
	mux.Handle("GET /", http.HandlerFunc(h.homeOrNotFound))
	mux.Handle("GET /articles", http.HandlerFunc(h.articles))
	mux.Handle("GET /articles/{ulid}", http.HandlerFunc(h.articleDetail))
	mux.Handle("GET /projects", http.HandlerFunc(h.projects))
	mux.Handle("GET /about", http.HandlerFunc(h.about))
}

func (h *Handler) homeOrNotFound(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.NotFound(w, r)
		return
	}
	data := homePageData{
		Title:       "首页 — MyWebsite",
		Description: "在网站耍起嘛,好舒服啊。",
		Heading:     "个人网站首页。",
		Eyebrow:     "PERSONAL SITE",
	}
	if h.articleReader != nil {
		items, err := h.articleReader.ListPublished(r.Context(), 3, 0)
		if err != nil {
			h.serviceUnavailable(w, r)
			return
		}
		data.Articles = makeArticleListItems(limitArticles(items, 3))
	}
	if h.projectReader != nil {
		items, err := h.projectReader.ListFeatured(r.Context())
		if err != nil {
			h.serviceUnavailable(w, r)
			return
		}
		data.Projects = makeProjectItems(limitProjects(items, 3))
	}
	h.renderTemplate(w, http.StatusOK, "home.html", data)
}

func (h *Handler) articles(w http.ResponseWriter, r *http.Request) {
	page, ok := parseArticlePage(r)
	if !ok {
		h.NotFound(w, r)
		return
	}

	var (
		items []article.PublishedArticle
		total int64
	)
	if h.articleReader != nil {
		var err error
		total, err = h.articleReader.CountPublished(r.Context())
		if err != nil {
			h.serviceUnavailable(w, r)
			return
		}
		if int64(page) <= publishedTotalPages(total) {
			offset := int32((int64(page) - 1) * int64(articlesPerPage))
			items, err = h.articleReader.ListPublished(r.Context(), articlesPerPage, offset)
			if err != nil {
				h.serviceUnavailable(w, r)
				return
			}
		}
	}

	totalPages := publishedTotalPages(total)
	if int64(page) > totalPages {
		h.NotFound(w, r)
		return
	}
	data := articleListPageData{
		Title:        "文章 — MyWebsite",
		Description:  "耍起文章一览。",
		Heading:      "文章",
		Eyebrow:      "WRITING",
		EmptyTitle:   "暂时还没有文章",
		EmptyText:    "新的文章正在整理中，欢迎稍后再来看看。",
		Page:         int(page),
		TotalPages:   int(totalPages),
		PreviousPage: int(page) - 1,
		NextPage:     int(page) + 1,
		HasPrevious:  page > 1,
		HasNext:      int64(page) < totalPages,
		Articles:     makeArticleListItems(items),
	}
	h.renderTemplate(w, http.StatusOK, "articles.html", data)
}

func (h *Handler) articleDetail(w http.ResponseWriter, r *http.Request) {
	if h.articleReader == nil {
		h.NotFound(w, r)
		return
	}
	value := r.PathValue("ulid")
	id, err := ulid.Parse(value)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	item, err := h.articleReader.GetPublishedByULID(r.Context(), id.String())
	if err != nil {
		if errorsIsNotFound(err) {
			h.NotFound(w, r)
			return
		}
		h.serviceUnavailable(w, r)
		return
	}
	if item.Status != article.StatusPublished {
		h.NotFound(w, r)
		return
	}

	toc, err := decodeTOC(item.TOCJSON)
	if err != nil {
		h.serviceUnavailable(w, r)
		return
	}
	preview := articlePreview(item.PreviewText, item.Title)
	data := articleDetailPageData{
		Title:          item.Title + " — MyWebsite",
		Description:    preview,
		OpenGraphTitle: item.Title,
		OpenGraphDesc:  preview,
		Heading:        item.Title,
		PublishedDate:  formatPublishedDate(item.FirstPublishedAt),
		BodyHTML:       template.HTML(articleBody(item.BodyHTML)),
		TOC:            toc,
	}
	h.renderTemplate(w, http.StatusOK, "article.html", data)
}

func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	data := projectPageData{
		Title:       "项目 — MyWebsite",
		Description: "耍起项目一览。",
		Heading:     "项目",
		Eyebrow:     "PROJECTS",
		EmptyTitle:  "暂时还没有公开项目",
		EmptyText:   "项目资料会在整理完成后展示在这里。",
	}
	if h.projectReader != nil {
		items, err := h.projectReader.ListPublicProjects(r.Context())
		if err != nil {
			h.serviceUnavailable(w, r)
			return
		}
		data.Projects = makeProjectItems(items)
	}
	h.renderTemplate(w, http.StatusOK, "projects.html", data)
}

func (h *Handler) about(w http.ResponseWriter, r *http.Request) {
	data := aboutPageData{
		Title:       "关于 — MyWebsite",
		Description: "关于我、我的耍起方式，以及这个耍起网站。",
		Heading:     "关于我",
		DisplayName: "Tazmi",
		AvatarURL:   "/assets/default-avatar.svg",
	}
	if h.articleReader != nil {
		count, err := h.articleReader.CountPublished(r.Context())
		if err != nil {
			h.serviceUnavailable(w, r)
			return
		}
		data.ArticleCount = count
	}
	if h.projectReader != nil {
		count, err := h.projectReader.CountPublicProjects(r.Context())
		if err != nil {
			h.serviceUnavailable(w, r)
			return
		}
		data.ProjectCount = count
	}
	h.renderTemplate(w, http.StatusOK, "about.html", data)
}

func (h *Handler) media(w http.ResponseWriter, r *http.Request) {
	value := r.PathValue("id")
	if !isStrictULID(value) || h.mediaReader == nil {
		h.NotFound(w, r)
		return
	}

	opened, err := h.mediaReader.Open(r.Context(), value, false)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			h.NotFound(w, r)
			return
		}
		h.serviceUnavailable(w, r)
		return
	}

	contentType, ok := publicMediaContentType(opened)
	if !ok {
		h.serviceUnavailable(w, r)
		return
	}
	path := opened.Path
	if path == "" {
		path = opened.FilePath
	}
	if !filepath.IsAbs(path) {
		h.serviceUnavailable(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		h.serviceUnavailable(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() < 0 {
		h.serviceUnavailable(w, r)
		return
	}

	digest, err := mediaDigest(file, opened)
	if err != nil {
		h.serviceUnavailable(w, r)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.serviceUnavailable(w, r)
		return
	}
	etag := `"` + hex.EncodeToString(digest) + `"`
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if !opened.CreatedAt.IsZero() {
		w.Header().Set("Last-Modified", opened.CreatedAt.UTC().Format(http.TimeFormat))
	}
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Del("Content-Length")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), opened.CreatedAt, file)
}

func (h *Handler) serviceUnavailable(w http.ResponseWriter, _ *http.Request) {
	h.renderTemplate(w, http.StatusInternalServerError, "500.html", serviceFaultPageData{
		Title:       "暂时无法加载 — MyWebsite",
		Description: "服务遇到了临时问题，请稍后重试。",
		Heading:     "页面暂时无法加载",
		EmptyText:   "服务正在恢复，请稍后再试。",
	})
}

// NotFound renders the shared Chinese 404 page for unknown public paths.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusNotFound, "404.html", pageData{
		Title:       "页面不存在 — MyWebsite",
		Description: "你访问的页面不存在或已经移除。",
		Heading:     "页面不存在",
		EmptyText:   "请检查地址，或从导航回到公开页面。",
	})
}

type pageData struct {
	Title       string
	Description string
	Heading     string
	EmptyText   string
	Year        int
}

type homePageData struct {
	Title       string
	Description string
	Heading     string
	Eyebrow     string
	Year        int
	Articles    []articleListItem
	Projects    []projectItem
}

type projectPageData struct {
	Title       string
	Description string
	Heading     string
	Eyebrow     string
	EmptyTitle  string
	EmptyText   string
	Year        int
	Projects    []projectItem
}

type aboutPageData struct {
	Title        string
	Description  string
	Heading      string
	Year         int
	DisplayName  string
	AvatarURL    string
	ArticleCount int64
	ProjectCount int64
}

type articleListPageData struct {
	Title        string
	Description  string
	Heading      string
	Eyebrow      string
	EmptyTitle   string
	EmptyText    string
	Year         int
	Articles     []articleListItem
	Page         int
	TotalPages   int
	PreviousPage int
	NextPage     int
	HasPrevious  bool
	HasNext      bool
}

type articleListItem struct {
	Title     string
	Preview   string
	Published string
	DateTime  string
	URL       string
	HasURL    bool
}

type projectItem struct {
	Name      string
	ImageURL  string
	ImageAlt  string
	HasImage  bool
	GitHubURL string
	HasGitHub bool
}

type articleDetailPageData struct {
	Title          string
	Description    string
	OpenGraphTitle string
	OpenGraphDesc  string
	Heading        string
	PublishedDate  string
	BodyHTML       template.HTML
	TOC            []tocItem
	Year           int
}

type tocItem struct {
	ID    string
	Text  string
	Level int
}

type tocDocument struct {
	Items []tocItem `json:"items"`
}

type serviceFaultPageData struct {
	Title       string
	Description string
	Heading     string
	EmptyText   string
	Year        int
}

func (h *Handler) renderTemplate(w http.ResponseWriter, status int, name string, data any) {
	data = withYear(data, h.clock)
	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, name, data); err != nil {
		http.Error(w, "内部服务器错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func (h *Handler) render(w http.ResponseWriter, status int, name string, data pageData) {
	h.renderTemplate(w, status, name, data)
}

func withYear(data any, clock platform.Clock) any {
	year := platform.CurrentYear(clock)
	switch value := data.(type) {
	case *pageData:
		value.Year = year
		return value
	case pageData:
		value.Year = year
		return value
	case *articleListPageData:
		value.Year = year
		return value
	case articleListPageData:
		value.Year = year
		return value
	case *articleDetailPageData:
		value.Year = year
		return value
	case articleDetailPageData:
		value.Year = year
		return value
	case *serviceFaultPageData:
		value.Year = year
		return value
	case serviceFaultPageData:
		value.Year = year
		return value
	case *homePageData:
		value.Year = year
		return value
	case homePageData:
		value.Year = year
		return value
	case *projectPageData:
		value.Year = year
		return value
	case projectPageData:
		value.Year = year
		return value
	case *aboutPageData:
		value.Year = year
		return value
	case aboutPageData:
		value.Year = year
		return value
	}
	return data
}

func parseArticlePage(r *http.Request) (int32, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, false
	}
	if len(values) == 0 {
		return 1, true
	}
	pageValues, ok := values["page"]
	if !ok || len(values) != 1 || len(pageValues) != 1 || pageValues[0] == "" {
		return 0, false
	}
	if pageValues[0][0] == '0' {
		return 0, false
	}
	for _, value := range pageValues[0] {
		if value < '0' || value > '9' {
			return 0, false
		}
	}
	page, err := strconv.ParseInt(pageValues[0], 10, 32)
	if err != nil || page <= 0 {
		return 0, false
	}
	return int32(page), true
}

func publishedTotalPages(total int64) int64 {
	if total <= 0 {
		return 1
	}
	return (total-1)/int64(articlesPerPage) + 1
}

func makeArticleListItems(items []article.PublishedArticle) []articleListItem {
	result := make([]articleListItem, 0, len(items))
	for _, item := range items {
		listItem := articleListItem{
			Title:     item.Title,
			Preview:   articlePreview(item.PreviewText, item.Title),
			Published: formatPublishedDate(item.FirstPublishedAt),
		}
		listItem.DateTime = listItem.Published
		if item.PublicULID != nil && strings.TrimSpace(*item.PublicULID) != "" {
			listItem.URL = "/articles/" + url.PathEscape(*item.PublicULID)
			listItem.HasURL = true
		}
		result = append(result, listItem)
	}
	return result
}

func limitArticles(items []article.PublishedArticle, limit int) []article.PublishedArticle {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func makeProjectItems(items []project.Project) []projectItem {
	result := make([]projectItem, 0, len(items))
	for _, item := range items {
		imageURL := strings.TrimSpace(item.ImageURL)
		if imageURL == "" && item.ImageAssetID != nil {
			assetID := strings.TrimSpace(*item.ImageAssetID)
			if assetID != "" {
				imageURL = "/media/" + url.PathEscape(assetID)
			}
		}
		if imageURL == "" {
			imageURL = project.DefaultImageURL
		}
		customImage := imageURL != project.DefaultImageURL
		githubURL := ""
		if item.GitHubURL != nil {
			githubURL = strings.TrimSpace(*item.GitHubURL)
		}
		result = append(result, projectItem{
			Name:      item.Name,
			ImageURL:  imageURL,
			ImageAlt:  valueWhen(customImage, item.Name, ""),
			HasImage:  customImage,
			GitHubURL: githubURL,
			HasGitHub: githubURL != "",
		})
	}
	return result
}

func limitProjects(items []project.Project, limit int) []project.Project {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func valueWhen(condition bool, value, fallback string) string {
	if condition {
		return value
	}
	return fallback
}

func isStrictULID(value string) bool {
	if len(value) != ulid.EncodedSize {
		return false
	}
	_, err := ulid.ParseStrict(value)
	return err == nil
}

func publicMediaContentType(value media.OpenResult) (string, bool) {
	contentType := strings.ToLower(strings.TrimSpace(value.StoredMediaType))
	if contentType == "" {
		contentType = strings.ToLower(strings.TrimSpace(value.SourceMediaType))
	}
	switch contentType {
	case "image/jpeg", "image/png":
		return contentType, true
	default:
		return "", false
	}
}

func mediaDigest(file *os.File, value media.OpenResult) ([]byte, error) {
	digest := value.SHA256
	if len(digest) == 0 {
		digest = value.Sha256
	}
	if len(digest) == sha256.Size {
		return append([]byte(nil), digest...), nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, err
	}
	return hasher.Sum(nil), nil
}

func articlePreview(value *string, fallback string) string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return *value
	}
	return fallback
}

func formatPublishedDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(platform.ShanghaiLocation()).Format("2006-01-02")
}

func articleBody(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func decodeTOC(raw json.RawMessage) ([]tocItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var document tocDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return document.Items, nil
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, article.ErrNotPublished)
}
