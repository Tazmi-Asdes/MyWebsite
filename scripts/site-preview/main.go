package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"mywebsite/internal/article"
	"mywebsite/internal/platform"
	"mywebsite/internal/project"
	"mywebsite/internal/publicsite"
	publicassets "mywebsite/web/public"
)

type fixtureArticleReader struct {
	items   []article.PublishedArticle
	details map[string]article.Article
}

func (reader *fixtureArticleReader) ListPublished(_ context.Context, limit, offset int32) ([]article.PublishedArticle, error) {
	if offset < 0 || limit <= 0 || int(offset) >= len(reader.items) {
		return []article.PublishedArticle{}, nil
	}
	start := int(offset)
	end := start + int(limit)
	if end > len(reader.items) {
		end = len(reader.items)
	}
	return append([]article.PublishedArticle{}, reader.items[start:end]...), nil
}

func (reader *fixtureArticleReader) CountPublished(context.Context) (int64, error) {
	return int64(len(reader.items)), nil
}

func (reader *fixtureArticleReader) GetPublishedByULID(_ context.Context, value string) (article.Article, error) {
	item, ok := reader.details[value]
	if !ok {
		return article.Article{}, sql.ErrNoRows
	}
	return item, nil
}

type fixtureProjectReader struct {
	items []project.Project
}

func (reader *fixtureProjectReader) ListPublicProjects(context.Context) ([]project.Project, error) {
	return append([]project.Project{}, reader.items...), nil
}

func (reader *fixtureProjectReader) ListFeatured(context.Context) ([]project.Project, error) {
	if len(reader.items) > 3 {
		return append([]project.Project{}, reader.items[:3]...), nil
	}
	return append([]project.Project{}, reader.items...), nil
}

func (reader *fixtureProjectReader) CountPublicProjects(context.Context) (int64, error) {
	return int64(len(reader.items)), nil
}

func main() {
	port := flag.Int("port", 18094, "loopback port for the local fixture preview")
	flag.Parse()
	if *port < 1 || *port > 65535 {
		log.Fatalf("invalid port %d: choose a value from 1 to 65535", *port)
	}

	articles := newFixtureArticles()
	projects := fixtureProjectReader{items: []project.Project{
		{Name: "Kubernetes 学习笔记", GitHubURL: stringPointer("https://github.com/Tazmi-Asdes/MyWebsite"), Status: project.StatusPublic},
		{Name: "CI/CD 自动化实践", Status: project.StatusPublic},
		{Name: "可观测性实验", Status: project.StatusPublic},
	}}
	publicHandler, err := publicsite.NewHandlerWithStage2(publicassets.Files, platform.NewShanghaiClock(), publicsite.Stage2Options{
		ArticleReader: articles,
		ProjectReader: &projects,
	})
	if err != nil {
		log.Fatalf("create public handler: %v", err)
	}

	mux := http.NewServeMux()
	publicHandler.RegisterRoutes(mux)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(publicassets.Assets))))

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("local fixture data only; no production database is configured")
	log.Printf("serving Astro proxy API at http://%s/api/public/v1/home", address)
	log.Printf("legacy public pages available at http://%s/, /articles, /articles/{ulid}, /projects, and /about", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(fmt.Errorf("serve local preview: %w", err))
	}
}

func newFixtureArticles() *fixtureArticleReader {
	publishedAt := time.Date(2026, 10, 16, 9, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	seeds := []struct {
		id      string
		title   string
		preview string
		body    string
		toc     string
	}{
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA0", title: "把 Kubernetes 探针接入上线检查", preview: "记录 readiness 与 liveness 探针的配置思路。", body: `<h2 id="overview">概览</h2>
<p>这篇固定样例展示公开详情页的目录和已渲染正文。</p>
<h2 id="readiness">探针配置</h2>
<pre><code>kubectl rollout status deployment/site</code></pre>
<pre><code>kubectl set image deployment/mywebsite-frontend mywebsite-frontend=registry.example.com/platform/web/mywebsite-frontend:2026.10.16-rc.1 --namespace=staging</code></pre>
<table><thead><tr><th>探针</th><th>用途</th></tr></thead><tbody><tr><td>readiness</td><td>接收流量前检查</td></tr><tr><td>liveness</td><td>发现卡住的容器</td></tr></tbody></table>
<table><thead><tr><th>检查项</th><th>镜像引用</th><th>部署命令</th></tr></thead><tbody><tr><td>前端发布</td><td><code>registry.example.com/platform/web/mywebsite-frontend:2026.10.16-rc.1</code></td><td><code>kubectl set image deployment/mywebsite-frontend mywebsite-frontend=registry.example.com/platform/web/mywebsite-frontend:2026.10.16-rc.1 --namespace=staging</code></td></tr></tbody></table>
<p><a href="https://example.com/guides/observability/kubernetes/readiness-and-liveness-probes/configuration-details">https://example.com/guides/observability/kubernetes/readiness-and-liveness-probes/configuration-details</a></p>
<p><img src="/assets/tazmi-avatar.jpg" alt="Tazmi 头像"></p>`, toc: `{"items":[{"id":"overview","text":"概览","level":2},{"id":"readiness","text":"探针配置","level":2}]}`},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA1", title: "一次 CI/CD 发布流程整理", preview: "从提交校验到构建镜像，梳理可重复的发布步骤。", body: "这篇样例文章展示 CI/CD 内容和详情链接。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA2", title: "用日志、指标和追踪观察服务", preview: "用可观测性信号定位一次简单的服务异常。", body: "这篇样例文章展示可观测性内容和详情链接。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA3", title: "给容器镜像设置清晰的版本", preview: "整理镜像标签和发布记录之间的对应关系。", body: "固定的本地示例文章四。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA4", title: "让服务配置更容易复查", preview: "把关键配置集中起来，减少上线前的遗漏。", body: "固定的本地示例文章五。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA5", title: "使用健康检查观察依赖状态", preview: "区分进程状态与依赖服务的可用状态。", body: "固定的本地示例文章六。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA6", title: "为日志添加稳定的请求标识", preview: "让一条请求在多个服务日志中更容易追踪。", body: "固定的本地示例文章七。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA7", title: "记录一次小型故障复盘", preview: "按时间线回顾问题发现、定位与恢复过程。", body: "固定的本地示例文章八。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA8", title: "整理部署前的检查清单", preview: "把构建、配置和健康检查放进同一流程。", body: "固定的本地示例文章九。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FA9", title: "为指标选择明确的名称", preview: "让服务指标能表达实际观察的对象和单位。", body: "固定的本地示例文章十。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FAA", title: "分阶段推进网站改造", preview: "先保持公开页面可用，再逐步替换数据来源。", body: "固定的本地示例文章十一。"},
		{id: "01ARZ3NDEKTSV4RRFFQ69G5FAB", title: "为本地预览准备稳定样例", preview: "使用固定内容检查分页和详情链接。", body: "固定的本地示例文章十二。"},
	}
	reader := &fixtureArticleReader{details: make(map[string]article.Article, len(seeds))}
	reader.items = make([]article.PublishedArticle, 0, len(seeds))
	for _, seed := range seeds {
		id := seed.id
		preview := seed.preview
		body := "<p>" + seed.body + "</p>"
		reader.items = append(reader.items, article.PublishedArticle{
			PublicULID:       &id,
			Title:            seed.title,
			PreviewText:      &preview,
			Status:           article.StatusPublished,
			FirstPublishedAt: &publishedAt,
		})
		reader.details[id] = article.Article{
			PublicULID:       &id,
			Title:            seed.title,
			PreviewText:      &preview,
			BodyHTML:         &body,
			TOCJSON:          json.RawMessage(seed.toc),
			Status:           article.StatusPublished,
			FirstPublishedAt: &publishedAt,
		}
	}
	return reader
}

func stringPointer(value string) *string {
	return &value
}
