package publicsite

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/oklog/ulid/v2"
	"mywebsite/internal/platform"
)

type homeAPIResponse struct {
	Site     homeAPISite      `json:"site"`
	Profile  homeAPIProfile   `json:"profile"`
	Year     int              `json:"year"`
	Articles []homeAPIArticle `json:"articles"`
	Projects []homeAPIProject `json:"projects"`
}

type homeAPISite struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Eyebrow     string `json:"eyebrow"`
}

type homeAPIProfile struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Tagline   string `json:"tagline"`
}

type homeAPIArticle struct {
	Title     string `json:"title"`
	Preview   string `json:"preview"`
	Published string `json:"published"`
	DateTime  string `json:"date_time"`
	URL       string `json:"url"`
}

type homeAPIProject struct {
	Name      string `json:"name"`
	ImageURL  string `json:"image_url"`
	ImageAlt  string `json:"image_alt"`
	GitHubURL string `json:"github_url"`
	URL       string `json:"url"`
	External  bool   `json:"external"`
}

type homeAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *Handler) homeAPI(w http.ResponseWriter, r *http.Request) {
	if h.articleReader == nil || h.projectReader == nil {
		writeHomeAPIError(w, r, http.StatusServiceUnavailable, "service_unavailable", "Public home data is temporarily unavailable.")
		return
	}

	publishedArticles, err := h.articleReader.ListPublished(r.Context(), 3, 0)
	if err != nil {
		writeHomeAPIError(w, r, http.StatusServiceUnavailable, "service_unavailable", "Public home data is temporarily unavailable.")
		return
	}
	publishedArticles = limitArticles(publishedArticles, 3)

	featuredProjects, err := h.projectReader.ListFeatured(r.Context())
	if err != nil {
		writeHomeAPIError(w, r, http.StatusServiceUnavailable, "service_unavailable", "Public home data is temporarily unavailable.")
		return
	}
	apiArticles := publicAPIArticles(publishedArticles)
	apiProjects := publicAPIProjects(featuredProjects, 3)

	response := homeAPIResponse{
		Site:     publicSiteInfo(),
		Profile:  publicProfileInfo(),
		Year:     platform.CurrentYear(h.clock),
		Articles: apiArticles,
		Projects: apiProjects,
	}

	writeHomeAPIJSON(w, r, http.StatusOK, response)
}

func homeAPIArticleURL(item articleListItem) string {
	if !item.HasURL {
		return "/articles"
	}
	const prefix = "/articles/"
	if !strings.HasPrefix(item.URL, prefix) {
		return "/articles"
	}
	encodedID := strings.TrimPrefix(item.URL, prefix)
	if encodedID == "" || strings.Contains(encodedID, "/") {
		return "/articles"
	}
	id, err := url.PathUnescape(encodedID)
	if err != nil {
		return "/articles"
	}
	parsed, err := ulid.ParseStrict(id)
	if err != nil {
		return "/articles"
	}
	return prefix + url.PathEscape(parsed.String())
}

func (h *Handler) homeAPIMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	writeHomeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and HEAD are supported.")
}

func writeHomeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeHomeAPIJSON(w, r, status, homeAPIError{Code: code, Message: message})
}

func writeHomeAPIJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"code":"internal_error","message":"The response could not be created."}`)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
