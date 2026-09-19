package publicsite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"mywebsite/internal/article"
	"mywebsite/internal/platform"
)

const articlesPerPage int32 = 10

// ArticleReader is the minimum article domain contract needed by the public
// site.  The article service satisfies it without an adapter.
type ArticleReader interface {
	ListPublished(context.Context, int32, int32) ([]article.PublishedArticle, error)
	CountPublished(context.Context) (int64, error)
	GetPublishedByULID(context.Context, string) (article.Article, error)
}

// Handler renders the read-only public pages.
type Handler struct {
	templates     *template.Template
	clock         platform.Clock
	articleReader ArticleReader
}

// NewHandler parses the embedded public templates and returns a renderer.
// The optional article reader keeps the Stage 0 constructor source-compatible
// while allowing the app layer to enable the Stage 1 article pages.
func NewHandler(files fs.FS, clock platform.Clock, readers ...ArticleReader) (*Handler, error) {
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
	var reader ArticleReader
	if len(readers) > 0 {
		reader = readers[0]
	}
	return &Handler{templates: templates, clock: clock, articleReader: reader}, nil
}

// NewHandlerWithArticles is an explicit constructor for callers that prefer
// not to use the optional argument on NewHandler.
func NewHandlerWithArticles(files fs.FS, clock platform.Clock, reader ArticleReader) (*Handler, error) {
	return NewHandler(files, clock, reader)
}

// RegisterRoutes registers the public GET routes on a Go 1.22+ ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
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
	h.render(w, http.StatusOK, "home.html", pageData{
		Title:       "首页 — MyWebsite",
		Description: "一个简洁、安静的个人网站首页。",
		Heading:     "你好，这里是我的个人网站。",
		Eyebrow:     "PERSONAL SITE",
		EmptyTitle:  "内容正在准备中",
		EmptyText:   "文章与项目会在完成整理后陆续发布。",
	})
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
		Description:  "记录思考、实践与长期积累的文章。",
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
	h.render(w, http.StatusOK, "projects.html", pageData{
		Title:       "项目 — MyWebsite",
		Description: "正在做过、正在做和想要继续做的项目。",
		Heading:     "项目",
		Eyebrow:     "PROJECTS",
		EmptyTitle:  "暂时还没有公开项目",
		EmptyText:   "项目资料会在整理完成后展示在这里。",
	})
}

func (h *Handler) about(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, "about.html", pageData{
		Title:       "关于 — MyWebsite",
		Description: "关于我、我的工作方式，以及这个网站。",
		Heading:     "关于我",
		Eyebrow:     "ABOUT",
		EmptyTitle:  "个人介绍正在准备中",
		EmptyText:   "这里会放置一份简短、真实且持续更新的介绍。",
	})
}

func (h *Handler) serviceUnavailable(w http.ResponseWriter, _ *http.Request) {
	h.renderTemplate(w, http.StatusInternalServerError, "500.html", serviceFaultPageData{
		Title:       "暂时无法加载 — MyWebsite",
		Description: "服务遇到了临时问题，请稍后重试。",
		Heading:     "页面暂时无法加载",
		Eyebrow:     "TEMPORARY ERROR",
		EmptyText:   "服务正在恢复，请稍后再试。",
	})
}

// NotFound renders the shared Chinese 404 page for unknown public paths.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusNotFound, "404.html", pageData{
		Title:       "页面不存在 — MyWebsite",
		Description: "你访问的页面不存在或已经移除。",
		Heading:     "找不到这个页面",
		Eyebrow:     "404",
		EmptyTitle:  "页面不存在",
		EmptyText:   "请检查地址，或从导航回到公开页面。",
	})
}

type pageData struct {
	Title       string
	Description string
	Heading     string
	Eyebrow     string
	EmptyTitle  string
	EmptyText   string
	Year        int
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
	Eyebrow     string
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
