package publicsite

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"mywebsite/internal/platform"
)

// Handler renders the read-only public pages for Stage 0.
type Handler struct {
	templates *template.Template
	clock     platform.Clock
}

// NewHandler parses the embedded public templates and returns a renderer.
func NewHandler(files fs.FS, clock platform.Clock) (*Handler, error) {
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
	return &Handler{templates: templates, clock: clock}, nil
}

// RegisterRoutes registers the public GET routes on a Go 1.22+ ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /", http.HandlerFunc(h.homeOrNotFound))
	mux.Handle("GET /articles", http.HandlerFunc(h.articles))
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
	h.render(w, http.StatusOK, "articles.html", pageData{
		Title:       "文章 — MyWebsite",
		Description: "记录思考、实践与长期积累的文章。",
		Heading:     "文章",
		Eyebrow:     "WRITING",
		EmptyTitle:  "暂时还没有文章",
		EmptyText:   "新的文章正在整理中，欢迎稍后再来看看。",
	})
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

func (h *Handler) render(w http.ResponseWriter, status int, name string, data pageData) {
	data.Year = platform.CurrentYear(h.clock)
	var body bytes.Buffer
	if err := h.templates.ExecuteTemplate(&body, name, data); err != nil {
		http.Error(w, "内部服务器错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}
