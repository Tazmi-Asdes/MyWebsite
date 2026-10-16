package publicsite

import (
	"net/http"
	"net/url"

	"github.com/oklog/ulid/v2"
	"mywebsite/internal/article"
	"mywebsite/internal/platform"
	"mywebsite/internal/project"
)

type publicAPIPage struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Heading     string `json:"heading"`
	Eyebrow     string `json:"eyebrow"`
}

type publicAPIArticleListResponse struct {
	Site       homeAPISite      `json:"site"`
	Year       int              `json:"year"`
	Page       publicAPIPage    `json:"page"`
	Articles   []homeAPIArticle `json:"articles"`
	Pagination publicAPIPaging  `json:"pagination"`
}

type publicAPIPaging struct {
	Page         int  `json:"page"`
	TotalPages   int  `json:"total_pages"`
	PreviousPage int  `json:"previous_page"`
	NextPage     int  `json:"next_page"`
	HasPrevious  bool `json:"has_previous"`
	HasNext      bool `json:"has_next"`
}

type publicAPIArticleDetailResponse struct {
	Site    homeAPISite            `json:"site"`
	Year    int                    `json:"year"`
	Page    publicAPIPage          `json:"page"`
	Article publicAPIArticleDetail `json:"article"`
}

type publicAPIArticleDetail struct {
	Title     string             `json:"title"`
	URL       string             `json:"url"`
	Preview   string             `json:"preview"`
	Published string             `json:"published"`
	DateTime  string             `json:"date_time"`
	BodyHTML  string             `json:"body_html"`
	TOC       []publicAPITOCItem `json:"toc"`
}

type publicAPITOCItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

type publicAPIProjectListResponse struct {
	Site     homeAPISite      `json:"site"`
	Year     int              `json:"year"`
	Page     publicAPIPage    `json:"page"`
	Projects []homeAPIProject `json:"projects"`
}

type publicAPIAboutResponse struct {
	Site         homeAPISite        `json:"site"`
	Year         int                `json:"year"`
	Page         publicAPIPage      `json:"page"`
	Profile      homeAPIProfile     `json:"profile"`
	ArticleCount int64              `json:"article_count"`
	ProjectCount int64              `json:"project_count"`
	Interests    []string           `json:"interests"`
	Contacts     []publicAPIContact `json:"contacts"`
}

type publicAPIContact struct {
	Label    string `json:"label"`
	Href     string `json:"href"`
	External bool   `json:"external"`
}

func publicSiteInfo() homeAPISite {
	return homeAPISite{
		Name:        "Tazmi-Website",
		Description: "Welcome here",
		Eyebrow:     "PERSONAL SITE",
	}
}

func publicProfileInfo() homeAPIProfile {
	return homeAPIProfile{
		Name:      "Tazmi",
		AvatarURL: "/assets/tazmi-avatar.jpg",
		Tagline:   "Kubernetes/CICD/Observability",
	}
}

func publicAPIPageInfo(title, description, heading, eyebrow string) publicAPIPage {
	return publicAPIPage{
		Title:       title + " — Tazmi-Website",
		Description: description,
		Heading:     heading,
		Eyebrow:     eyebrow,
	}
}

func publicAPIArticles(items []article.PublishedArticle) []homeAPIArticle {
	safeItems := make([]article.PublishedArticle, 0, len(items))
	for _, item := range items {
		if item.Status == article.StatusPublished {
			safeItems = append(safeItems, item)
		}
	}
	formatted := makeArticleListItems(safeItems)
	result := make([]homeAPIArticle, 0, len(formatted))
	for _, item := range formatted {
		result = append(result, homeAPIArticle{
			Title:     item.Title,
			Preview:   item.Preview,
			Published: item.Published,
			DateTime:  item.DateTime,
			URL:       homeAPIArticleURL(item),
		})
	}
	return result
}

func publicAPIProjects(items []project.Project, limit int) []homeAPIProject {
	safeItems := make([]project.Project, 0, len(items))
	for _, item := range items {
		if item.Status != project.StatusPublic {
			continue
		}
		safeItems = append(safeItems, item)
		if limit > 0 && len(safeItems) == limit {
			break
		}
	}
	formatted := makeProjectItems(safeItems)
	result := make([]homeAPIProject, 0, len(formatted))
	for _, item := range formatted {
		projectURL := "/projects"
		external := item.HasGitHub
		if external {
			projectURL = item.GitHubURL
		}
		result = append(result, homeAPIProject{
			Name:      item.Name,
			ImageURL:  item.ImageURL,
			ImageAlt:  item.ImageAlt,
			GitHubURL: item.GitHubURL,
			URL:       projectURL,
			External:  external,
		})
	}
	return result
}

func (h *Handler) publicArticlesAPI(w http.ResponseWriter, r *http.Request) {
	page, ok := parseArticlePage(r)
	if !ok || h.articleReader == nil {
		if !ok {
			writeHomeAPIError(w, r, http.StatusNotFound, "not_found", "The requested public resource was not found.")
			return
		}
		writePublicAPIUnavailable(w, r)
		return
	}
	total, err := h.articleReader.CountPublished(r.Context())
	if err != nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	totalPages := publishedTotalPages(total)
	if int64(page) > totalPages {
		writeHomeAPIError(w, r, http.StatusNotFound, "not_found", "The requested public resource was not found.")
		return
	}
	offset := int32((int64(page) - 1) * int64(articlesPerPage))
	items, err := h.articleReader.ListPublished(r.Context(), articlesPerPage, offset)
	if err != nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	items = limitArticles(items, int(articlesPerPage))
	response := publicAPIArticleListResponse{
		Site:     publicSiteInfo(),
		Year:     platform.CurrentYear(h.clock),
		Page:     publicAPIPageInfo("文章", "耍起文章一览。", "文章", "WRITING"),
		Articles: publicAPIArticles(items),
		Pagination: publicAPIPaging{
			Page:         int(page),
			TotalPages:   int(totalPages),
			PreviousPage: int(page) - 1,
			NextPage:     int(page) + 1,
			HasPrevious:  page > 1,
			HasNext:      int64(page) < totalPages,
		},
	}
	writeHomeAPIJSON(w, r, http.StatusOK, response)
}

func (h *Handler) publicArticleDetailAPI(w http.ResponseWriter, r *http.Request) {
	value := r.PathValue("ulid")
	if !isStrictULID(value) {
		writeHomeAPIError(w, r, http.StatusNotFound, "not_found", "The requested public resource was not found.")
		return
	}
	if h.articleReader == nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	id, _ := ulid.ParseStrict(value)
	item, err := h.articleReader.GetPublishedByULID(r.Context(), id.String())
	if err != nil {
		if errorsIsNotFound(err) {
			writeHomeAPIError(w, r, http.StatusNotFound, "not_found", "The requested public resource was not found.")
			return
		}
		writePublicAPIUnavailable(w, r)
		return
	}
	if item.Status != article.StatusPublished {
		writeHomeAPIError(w, r, http.StatusNotFound, "not_found", "The requested public resource was not found.")
		return
	}
	toc, err := decodeTOC(item.TOCJSON)
	if err != nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	preview := articlePreview(item.PreviewText, item.Title)
	published := formatPublishedDate(item.FirstPublishedAt)
	tocItems := make([]publicAPITOCItem, 0, len(toc))
	for _, entry := range toc {
		tocItems = append(tocItems, publicAPITOCItem{ID: entry.ID, Text: entry.Text, Level: entry.Level})
	}
	apiArticle := publicAPIArticleDetail{
		Title:     item.Title,
		URL:       "/articles/" + url.PathEscape(id.String()),
		Preview:   preview,
		Published: published,
		DateTime:  published,
		BodyHTML:  articleBody(item.BodyHTML),
		TOC:       tocItems,
	}
	response := publicAPIArticleDetailResponse{
		Site:    publicSiteInfo(),
		Year:    platform.CurrentYear(h.clock),
		Page:    publicAPIPageInfo(item.Title, preview, item.Title, "实践记录"),
		Article: apiArticle,
	}
	writeHomeAPIJSON(w, r, http.StatusOK, response)
}

func (h *Handler) publicProjectsAPI(w http.ResponseWriter, r *http.Request) {
	if h.projectReader == nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	items, err := h.projectReader.ListPublicProjects(r.Context())
	if err != nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	response := publicAPIProjectListResponse{
		Site:     publicSiteInfo(),
		Year:     platform.CurrentYear(h.clock),
		Page:     publicAPIPageInfo("项目", "耍起项目一览。", "项目", "PROJECTS"),
		Projects: publicAPIProjects(items, 0),
	}
	writeHomeAPIJSON(w, r, http.StatusOK, response)
}

func (h *Handler) publicAboutAPI(w http.ResponseWriter, r *http.Request) {
	if h.articleReader == nil || h.projectReader == nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	articleCount, err := h.articleReader.CountPublished(r.Context())
	if err != nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	projectCount, err := h.projectReader.CountPublicProjects(r.Context())
	if err != nil {
		writePublicAPIUnavailable(w, r)
		return
	}
	response := publicAPIAboutResponse{
		Site:         publicSiteInfo(),
		Year:         platform.CurrentYear(h.clock),
		Page:         publicAPIPageInfo("关于", "关于我、我的耍起方式,以及这个耍起网站。", "关于我", "ABOUT"),
		Profile:      publicProfileInfo(),
		ArticleCount: articleCount,
		ProjectCount: projectCount,
		Interests:    []string{"耍起嘛,每天用ai整点有的没的好舒服啊"},
		Contacts: []publicAPIContact{
			{Label: "2926238971@qq.com", Href: "mailto:2926238971@qq.com", External: false},
			{Label: "github.com/Tazmi-Asdes", Href: "https://github.com/Tazmi-Asdes", External: true},
		},
	}
	writeHomeAPIJSON(w, r, http.StatusOK, response)
}

func writePublicAPIUnavailable(w http.ResponseWriter, r *http.Request) {
	writeHomeAPIError(w, r, http.StatusServiceUnavailable, "service_unavailable", "Public content is temporarily unavailable.")
}
