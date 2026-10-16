---
date: 2026-10-16
status: accepted
baseline: 4b1491cb77aa2d0c69a22372f2e218d151904949
authorization: 用户授权来源本轮对话
---

# Astro 公开站分阶段迁移

第一阶段首页本地试点已验收；用户随后授权继续第二阶段全站本地迁移与候选验证配置。第二阶段本地实现、接口检查和 Edge 视口模拟关键流程验收已完成。第三阶段三镜像生产接入代码及本地发布编排验证已完成，实际 Docker 构建、GitLab CI 验证、Registry 发布与服务器入口切换仍待验收。本 ADR 不表示物理设备、性能或生产验收完成，也不表示候选容器已部署或生产入口已切换。生产目标采用 Go、管理端和 Astro 三容器；OpenResty 入口仍须由操作者人工切换并验收。

## 当前决策

第二阶段由独立 `web/site` 使用 Astro 与 TypeScript 对所有公开页面进行 SSR；文章、项目、关于等页面均由 Astro 渲染，公开页面导航不再代理到 Go。浏览器使用同源入口；页面数据由 Astro 服务端通过内部地址请求 Go 的匿名只读、白名单公开 JSON 投影（接口约定见 [Go 公开 API 规范](../../api/public.openapi.yaml)）。Go 继续以 Redis 缓存该公开 JSON，并复用既有版本失效机制。Astro 不直接连接 Redis，也不持久化 HTML；浏览器可通过 ETag 重新验证。只有 assets 与 media 请求继续代理至 Go。

Go 继续负责文章与项目业务、SQL/MySQL、Markdown 渲染、媒体访问和管理员认证；Vue 管理端不迁移。公开站保留黑白灰、动漫背景（背景模糊 3px）与圆角液态玻璃视觉，并兼顾 phone、tablet、desktop。必要的交互使用 React 岛并通过 Motion 复用动画能力。根 `Dockerfile` 保留独立 `site-runtime` 目标，默认 `admin-web` 构建目标不变；site-web 经可配置的宿主机回环端口 `SITE_WEB_PORT` 转发到固定 Node `4321`（默认 `127.0.0.1:18082`），并在应用容器网络内访问固定 Go origin `app:8080`，不连接数据库或 Redis，也不读取其密钥。变更宿主机端口时须同步 OpenResty Astro upstream。

## 取舍与影响

Go 页面加局部 React 改动较少，但无法统一 Astro 页面层；纯 React SPA 会增加整页 JavaScript 并改变 SSR 方式；Astro 静态构建会改变后台发布后的生效流程，因此选择 SSR。Go 的 Redis 版本失效机制用于公开 JSON 投影，不缓存 Astro HTML；本决策不承诺性能提升，比较时应使用相同内容和资源。生产发布始终同时更新 app、admin-web、site-web；`PUBLIC_SITE_MODE=go|astro` 默认 `go`，只选择公网冒烟检查预期，不启停 site-web，也不自动修改 OpenResty。生产状态 format v2 保存三镜像 digest 与模式，并继续完整读取旧 format v1 双镜像记录，不补造 site digest。普通自动回退仅允许同拓扑、同模式的 v2 记录；v1 与跨模式回退拒绝，首次 v1→v2 或 Go→Astro 模式变更使用配置确认门槛且不自动恢复旧镜像。

## 第一阶段：首页本地试点历史验收（2026-10-16）

本阶段只迁移首页；其余页面链接仍进入旧 Go 公开页。当时 Astro 首页经内部地址调用 Go 的 `GET /api/public/v1/home`，接口约定见 [Go 公开 API 规范](../../api/public.openapi.yaml)。本地 `npm ci`、`npm run check`（15 个文件，0 错误、0 警告）、`npm run build`、`go test ./internal/publicsite ./internal/app` 和 `go build -o .tmp/astro-site-preview/site-preview.exe ./scripts/site-preview` 均通过。独立 Node 产物返回 3 篇文章、3 个项目并带 `no-store`；上游不可用时返回 `503` 和 `no-store`，不显示 fixture 内容。该阶段首页投影尚未接入 Go Redis 缓存。

第一阶段桌面浏览器视口模拟覆盖 390×844、768×1024、1440×1000：无横向溢出，布局为 1/2/3 列，手机与平板使用竖图、桌面切换横图；主题切换和手机浮层菜单可用，菜单支持 Escape 且不增加 header 高度；文章及项目整卡可原生跳转到旧 Go 页面。跨页使用原生导航，不引入 Astro `ClientRouter`；CSS 对齐 Go 的 220ms 原生 view-transition，并在 `reduced-motion` 下关闭过渡。新产物重启后验证了整卡进入 Go 文章详情并返回首页，未复现此前的 `ViewTransition opt-in disabled` 错误；这不代表全部 console 检查均通过，也不代表物理设备或真实性能测试。

## 第二阶段：本地构建、接口与浏览器模拟验收

Go `go test ./internal/publicsite ./internal/cache ./internal/app` 通过；Node 的 4 项 `html-cache` 测试通过；前端收尾加入文章列表卡片 hover 轻微上浮 2px、active 状态和 focus 外描边，并在 `reduced-motion` 下禁用位移。完成后 `npm run check` 再次检查 33 个文件且无错误、警告，`npm run build` 再次通过。hover 效果记录为实现项，不代表已手工测量。

本地 fixture 检查中，ETag 弱匹配返回 `304` 且响应体为空；Cookie 响应的缓存控制符合 `no-store`/`Vary`；分页第 2 页返回 2 项；`FA0` 详情包含目录、代码、表格和图片；projects、about 返回 `200`；未知 Astro 页面返回 `404` 且带 `no-store`；`/-/live` 与 `/-/ready` 返回 `200`。这些检查使用本地 fixture，不是真实 Redis 或数据库验证。

同一 Edge 浏览器以视口模拟 390×844、768×1024、1440×1000 检查关键流程：项目列表分别为 1/2/3 列，图片 loaded 且 `object-fit: contain`；手机菜单展开时 header 保持约 69px，Escape 可关闭，点击导航后关闭；浅/深主题切换后跨页保持。文章列表显示 10 项，分页进入第 2 页显示 2 项，整卡可进入 `FA0` 详情。Desktop 显示侧边 TOC，phone/tablet 的折叠 TOC 可展开并点击锚点，定位 top 约 24px。长 `kubectl` 代码块可在内容区内横向滚动（phone 内容宽约 303px、代码 scrollWidth 约 1116px；tablet 内容宽约 649px、代码 scrollWidth 约 1116px）；表格、长 URL 和图片保持正文宽度，settled 后 `document.scrollWidth == clientWidth`。Portrait 使用竖图、desktop 使用横图，背景 computed filter 为 `blur(3px)`。以上为本地 fixture 上的桌面浏览器视口模拟，不是物理手机/平板、真实性能或生产环境验证。

修正 CSS 声明顺序后，构建产物保留标准 `backdrop-filter`；Edge computed 首页卡片和文章列表为 `blur(18px) saturate(1.26)`，header 为 `blur(16px) saturate(1.3)`，背景独立为 `blur(3px)`。修复后 `npm run check` 再次检查 33 个文件且无错误、警告，`npm run build` 再次通过。

## 生产接入与入口切换

入口方案保留 `/admin/` 到管理端，`/api/v1/`、`/media/`、`/-/` 与 `/assets/` 到 Go；`/_astro/` 与其余公开页面到 site-web。`/api/public/v1/` 是 Astro 服务端的内部 Go API，不增加公网路由，浏览器不依赖它，因此命中 Astro 并返回 404 符合此边界。OpenResty 继续提供现有安全头、隐藏上游同名头并处理 maintenance marker 与静态 503；不扩大 CSP，人工合并时保留生产域名、证书和 HSTS。

生产切换按以下次序进行：先以默认 `go` 模式通过配置确认发布三容器，维持原 Go 入口；按 `SITE_WEB_PORT` 实际配置值直连 site-web（默认 `127.0.0.1:18082`）检查 Node live/ready、公开页面与 `/_astro/` 资源，并检查 Go assets/media 和管理端；若调整了宿主机端口，同时调整 Astro upstream。备份实际 1Panel 完整站点配置，人工合并 Astro 模板、检查并重载 OpenResty，再验证公网 marker；最后把 GitLab 项目 CI/CD 变量持久设置为 `PUBLIC_SITE_MODE=astro`，通过配置确认 web pipeline 记录 v2 astro。该变量是非敏感短值，应使用 Visible 而非 Masked，可设 Protected；临时配置确认变量仅用于当次 pipeline。仅一次性填写 astro 不会保留模式，后续流水线会回到默认 go。

入口或发布异常时，人工恢复完整旧入口配置、将持久模式改回 `go`，再通过配置确认发布已确认兼容 Go 入口的三镜像版本。配置例外不走自动旧镜像恢复；普通自动回退不能跨 v1/v2 或 `go`/`astro` 模式。首次 Astro 配置例外发布之后，还需有一次同拓扑、同模式的 Astro 普通成功发布，previous 才成为可自动回退的 Astro 版本。该流程不保证零停机或瞬时原子切换。`deploy/compose.site.preview.yaml` 仍是可选的隔离候选，绑定 `127.0.0.1:28082`，不属于生产 site-web，不切换公网、不纳入生产回滚。

## 第三阶段：发布编排本地验证（2026-10-17）

离线 `deploy/tests/build-scan-test.sh` 通过；`deploy/tests/deploy-production-test.sh` 最终完整一轮 exit 0、24 个 PASS 分组、stderr 为空，日志见 [stdout.log](../../.tmp/astro-deploy-final-check/run-20261017-000943-suite/stdout.log)。覆盖首次 v1→v2 配置门槛且不补造 site digest、三镜像失败恢复与同模式回退、同 commit 的 site digest 变更及重复发布保留 previous、Astro HTML 引用 CSS 检查、Go/Astro 模式不匹配、无状态 site 容器与异常 record 阻断，以及 cleanup 保留 current/previous；`bash -n` 通过。主代理另行完成 CI/Compose YAML 解析、job/port/context/origin 字段核对及 diff check。

隔离副本 `npm run check` 检查 33 个文件且 0 错误、0 警告，`npm run build` 通过。

隔离 Node harness 的同进程 `listening` 事件确认 PID `51252` 持有 `127.0.0.1:4329`；首页返回 `200` 且含 Astro marker，首页实际引用的 CSS 返回 `200`、`text/css` 并含背景变量，`/-/ready` 返回 `200`。检查记录见 [stdout](../../.tmp/astro-ingress-check/owned-http-4329-20261017-003152-968.stdout.log) 与 [stderr](../../.tmp/astro-ingress-check/owned-http-4329-20261017-003152-968.stderr.log)。server close 回调完成后，Node 以 `-1073740791` 退出并报告 Windows libuv assertion；该结果仅验证本地产物与本地 fixture。真实 Docker 构建、GitLab CI Lint/流水线、Registry 和服务器入口切换均未验证。

## 本地 fixture 启动

终端 1 在仓库根目录启动 Go fixture。它监听 `127.0.0.1:18094`，只提供本地样例数据，不连接生产数据库：

```powershell
go run ./scripts/site-preview
```

终端 2 在仓库根目录运行以下命令，从 `web/site` 启动 Astro：

```powershell
cd web/site
npm ci
npm run check
npm run build
$env:GO_PUBLIC_ORIGIN = 'http://127.0.0.1:18094'
$env:HOST = '127.0.0.1'
$env:PORT = '4321'
npm start
```

Astro 首页为 [http://127.0.0.1:4321/](http://127.0.0.1:4321/)，Go fixture 基线为 [http://127.0.0.1:18094/](http://127.0.0.1:18094/)；对照时直接访问该根路径，不依赖 legacy 别名。fixture 或请求出错时应报告错误，不得伪造成功内容。
