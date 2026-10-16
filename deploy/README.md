# 开发者 CI/CD 与生产部署

日常流程是 `功能分支 → Merge Request → main`。分支和 MR 运行 Go、管理端及公开站校验；受保护的默认分支通过校验后，自动构建并上传 `app`、`admin-web`、`site-web` 三个镜像，再执行部署。每次新发布始终更新三个服务；`PUBLIC_SITE_MODE` 只选择公网冒烟检查预期，不启停服务，也不修改 OpenResty 路由。

## 流水线

| 阶段 / 作业 | Runner | 内容 |
| --- | --- | --- |
| `verify_go` | Docker，`security` | gofmt、go vet、Go 测试、部署脚本语法及模拟恢复测试；生成 JUnit |
| `verify_admin` | Docker，`security` | 锁定依赖安装、类型检查、单元测试、生产构建；生成 JUnit |
| `verify_site` | Docker，`security` | Astro 类型检查、单元测试和生产构建 |
| `build_images` | Shell，`server` | 构建 app/admin-web/site-web，推送完整 Git SHA 标签，生成含三个镜像 digest 的 `release.env` |
| `deploy_prod` | Shell，`server` | 使用构建作业交付的 digest 更新三个服务，检查容器健康与公网关键页面，更新发布记录并清理旧镜像 |

构建和部署分开。部署使用 `--no-build --pull never`，先显式拉取待部署 digest；不会在 deploy 作业重新构建。`release.env` 不含生产配置或密码，作为作业产物保留 14 天。JUnit 与脱敏部署诊断也保留 14 天。

本次暂时停用真实 Trivy 镜像扫描和定时扫描。扫描脚本与 `verify_go` 中的离线模拟测试仍保留，便于后续恢复；现有 GitLab 项目变量 `MYWEBSITE_TRIVY_DB_*` 在此期间不会使用。

分支推送已有打开的 MR 时仅运行 MR 流水线。新提交可以取消旧的校验作业；构建和生产操作不自动中断。生产发布与回退共用 `production` 资源锁，脚本在更新前核对远程默认分支 HEAD，阻止过时流水线覆盖新版本。

## 首次使用前

1. GitLab Container Registry 已启用，默认分支（当前为 `main`）设为 protected。Shell Runner 使用 `server` 标签、仅服务本项目，并设为 protected；Docker Runner 使用 `security` 标签，允许执行分支/MR 校验，不挂载宿主机 Docker socket。
2. Shell Runner 上存在 Bash、Git、Docker Compose v2、curl、install、sha256sum、awk、grep 等常用命令，`gitlab-runner` 可操作 Docker。能访问 GitLab Registry；Docker `security` Runner 用于 Go、管理端和 Astro 校验。
3. 保留现有 `/opt/mywebsite/deploy/env.production`、`secrets/db_password`、`secrets/redis_password`、`data/uploads`，确保 `gitlab-runner` 可读配置及两个单行密码文件、可写上传目录。密码不会进入 Git、镜像或产物。
4. 创建新发布记录目录（在服务器执行一次）：

   ```sh
   sudo install -d -o gitlab-runner -g gitlab-runner -m 0700 /opt/mywebsite/deploy-state
   ```

5. 核对 GitLab CI/CD 变量中的 `MYSQL_NETWORK_NAME`、`COMPOSE_PROJECT_NAME`、`APP_PORT`、`ADMIN_WEB_PORT`、`SITE_WEB_PORT`、`PRODUCTION_URL` 与 `PUBLIC_SITE_MODE`。仓库默认分别是 `1panel-network`、`mywebsite`、`18080`、`18081`、`18082`、`https://tazmi.online`；`SITE_WEB_PORT` 是可配置的宿主机回环端口，容器端口固定为 `4321`，Go origin 固定为 `app:8080`。若改 `SITE_WEB_PORT`，必须同步修改 OpenResty Astro 模板中的 `127.0.0.1:18082` upstream。`PUBLIC_SITE_MODE` 默认为 `go`。它是非敏感短值，仅取 `go` 或 `astro`，使用普通 **Visible** 项目变量，不要设为 **Masked**；可设为 **Protected**，配合受保护默认分支流水线。**外部网络以服务器实际配置为准**；如果现有 app 的 MySQL 网络是 `Mysql`，在 GitLab 变量中设置该值。Redis 与 app 必须共享可解析 `Redis` 的网络，连接地址和密码路径继续由服务器的 `env.production` 提供。site-web 只通过容器网络访问 `app:8080`，不读取数据库或 Redis 密钥。

当前稳定 Compose 的 uploads 和密码路径固定为 `/opt/mywebsite/...`，按此目录使用。迁移服务器路径需要同时调整 Compose，属于下述配置变更流程。GitLab 内置的 `CI_REGISTRY_USER` / `CI_REGISTRY_PASSWORD` 用于本次作业登录，无需增加长期 Registry token。

建议在 GitLab 项目设置中开启防止过时部署。

## 反向代理与 HTTP 安全头

`TRUSTED_PROXY_CIDRS` 默认留空，应用不会信任转发的客户端 IP 头。只有先确认 Go 服务实际看到的反向代理对端 IP 后，才在服务器 `env.production` 中填写精确 CIDR；单个 IPv4 地址使用 `/32`，单个 IPv6 地址使用 `/128`，多个值以逗号分隔。Ingress 必须在转发请求时覆盖 `X-Real-IP`。不要按猜测填写整个 Docker 或私网网段。

Go `app` 与 `admin-web` 镜像提供 `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY` 和 `Referrer-Policy: strict-origin-when-cross-origin`；Astro 正式入口模板也由 OpenResty统一提供这些安全头并隐藏上游同名头。强制执行的 CSP 仅保留防插件、`base-uri`、`form-action`、`frame-ancestors` 兼容基线；更严格的 `script-src` / `style-src` 策略待配置报告端点并完成浏览器兼容验证后再启用。OpenResty 模板中的代理错误与静态 503 安全头需人工合并到当前 1Panel 站点配置；模板包含待替换的域名和证书路径，不能直接覆盖生产配置。合并时保留已有 HSTS 设置。

## 发布与自动恢复

生产 Compose 在现有 `compose.yaml`、`deploy/compose.onepanel.example.yaml`、`deploy/compose.redis.onepanel.yaml`、`deploy/compose.images.yaml` 上叠加 `deploy/compose.site.yaml`。每次新发布管理 `app`、`admin-web`、`site-web` 三个服务，继续使用外部 MySQL、Redis 和现有 OpenResty。site-web 通过可配置的 `SITE_WEB_PORT` 绑定宿主机回环，默认 `127.0.0.1:18082` 并转发到固定 Node 端口 `4321`；它通过容器网络访问固定 Go origin `app:8080`，不直接连接数据库、Redis 或读取其密钥。修改宿主机端口时须同步调整 OpenResty Astro upstream。

每次发布先预检配置、文件权限、网络及 Compose，拉取三个 digest，再更新三个服务。随后核对运行镜像 ID；分别检查 Go `app`、`admin-web`、site-web 的健康状态。Go 的 `/-/live` 与 `/-/ready` 仍走 Go；Node 的 `/-/live` 与 `/-/ready` 通过本机 `127.0.0.1:18082` 检查，其中 Node ready 还要求 Go 公开投影 API 可用。公网首页、文章、项目、关于、管理端和资源检查按 `PUBLIC_SITE_MODE` 选择预期：`go` 校验 Go 公开页，`astro` 校验响应中 `<meta name="public-site-renderer" content="astro">`。该变量不控制容器状态或 OpenResty 路由。

`deploy-state/current` 与 `previous` 保存当前、上一成功版本的提交、镜像 digest、公开模式和配置指纹。新 format v2 同时记录三个服务的 digest 与 `PUBLIC_SITE_MODE`；旧 format v1 的 app/admin 双镜像记录仍须完整读取，不得伪造 site digest。首次从 v1 迁移到 v2 时须走下述配置确认流程，公网保持 Go；旧 v1 previous 引用的真实镜像继续受清理保护。初始化时还需核对完整 SHA 标签、Registry RepoDigest 和 CI checkout 中可读取的旧提交；无法识别时在更新服务前停止。若服务器没有现有服务，首次失败时没有已验证版本可恢复。

同一拓扑、同一 `PUBLIC_SITE_MODE` 下的 v2 普通发布在启动、健康或公网检查失败后，会尝试重新应用 current 中的三镜像版本；恢复成功后作业仍显示失败，成功记录不会前移。已验证成功才更新 current/previous。v1 回退与跨 `go`/`astro` 模式回退明确拒绝。配置确认例外关闭自动旧镜像恢复。重复部署相同版本不覆盖上一版本。该流程是原地更新，可能短暂中断访问；不提供零停机保证。进程被强制终止或服务器断电后的状态需要人工核对。

成功后仅清理本项目超过两个保留版本的本地 SHA 镜像标签，保留 current、previous（含旧 v1 双镜像记录）及仍被其他容器引用的镜像。清理失败只记录警告，不把健康发布改判失败；不运行全局 prune，不删除卷或外部服务。GitLab Registry 的清理策略是独立配置，需保留准备回退的 SHA 标签/digest，使回退时仍可拉取。

## 手动回退

在 GitLab 的 **Run pipeline** 中选受保护默认分支，设置 `PIPELINE_ACTION=rollback`，创建一条新的流水线。它只执行 `rollback_prod`，不重新构建旧版本，不重跑历史 deploy 作业。

回退目标来自服务器的 previous。仅当 current/previous 都是兼容的 v2 且拓扑和公开模式相同，脚本才拉取并应用 previous 的三个 digest；成功后交换 current/previous，失败则尝试恢复 current，作业保持失败。previous 不存在、目标是 v1、模式不同、镜像已被仓库清理、旧 Git 对象缺失或配置不兼容时会停止。

## 数据库、Compose 或环境变更

自动发布会比较 `db/migrations`、稳定 Compose 文件、服务器 `env.production`、网络/端口及公开模式等参数的指纹。它们变化时，普通流水线停止，不自动操作数据库或切换 OpenResty。

先人工完成备份，并确认新旧程序与数据库/配置的兼容性，再从受保护默认分支创建 web 流水线：

```text
PIPELINE_ACTION=deploy
ALLOW_DEPLOY_CONFIG_CHANGE=true
CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
```

该例外只表示操作者已完成备份和兼容性确认，脚本不会替你备份、执行迁移或还原数据库。**此路径关闭自动旧镜像恢复**，发布失败后需要按本次配置/迁移的实际情况处理，避免把不兼容的旧程序直接放回线上。

## 依赖刷新

日常构建沿用本地基础镜像缓存。需刷新时，在默认分支 web 流水线设置 `PULL_BASE_IMAGES=true`，重新执行校验、构建和部署。

## 本地验证与旧脚本

可用 Bash 运行不连接外部服务的流程检查：

```sh
bash deploy/tests/build-scan-test.sh
bash deploy/tests/deploy-production-test.sh
```

这些检查模拟 Docker、Registry、HTTP 和发布记录，验证编排、失败恢复与清理边界。它们不等于真实镜像构建、GitLab CI Lint 或生产验收；首次实际流水线仍需查看各阶段结果。

`scripts/release.sh`、`rollback.sh`、`backup-batch.sh` 与候选端口 overlay 是早期手动发布工具，本次 CI 不调用它们；它们仍要求单独的服务器配置和执行开关。流水线不自动改写 OpenResty；`PUBLIC_SITE_MODE` 仅选择公网冒烟预期。Astro 正式入口须由操作者按下文手工合并到 1Panel 当前站点配置。

## Astro 全站生产接入与手工入口切换

生产 Compose 每次都启动 `app`、`admin-web`、`site-web`。site-web 通过可配置的 `SITE_WEB_PORT` 绑定宿主机回环，默认 `127.0.0.1:18082` 并转发到固定 Node 端口 `4321`；它通过容器网络访问固定 Go origin `app:8080`，不直接连接数据库、Redis 或读取其密钥。修改宿主机端口时须同步调整 OpenResty Astro upstream。`PUBLIC_SITE_MODE` 默认为 `go`，只决定公网冒烟检查期待 Go 页面还是 Astro 的 `<meta name="public-site-renderer" content="astro">` 标记，不启动/停止 site-web，也不自动修改 OpenResty。Astro 通过内部地址请求 Go 的 `/api/public/v1/`；该路径不新增公网入口，浏览器不依赖它，公网落到 Astro 后返回 404 是预期行为。

按以下次序接入，公网入口始终由人工控制：

1. 首次从旧 format v1 转为 format v2 时，先保持 `PUBLIC_SITE_MODE=go`，通过受保护默认分支的 web pipeline 发布三个容器；公网仍走 Go。v1 → v2 必须使用既有配置确认门槛：

   ```text
   PIPELINE_ACTION=deploy
   ALLOW_DEPLOY_CONFIG_CHANGE=true
   CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
   ```

   这两个门槛变量只在本次 **Run pipeline** 使用，不要作为持久项目变量保存。v2 记录 app、admin-web、site-web 三个真实 digest 及模式；旧 v1 记录仍完整读取，不伪造 site digest。该配置例外不提供自动旧镜像恢复；旧 v1 previous 真实引用的镜像仍受清理保护。

2. 不改公网入口，先按 `SITE_WEB_PORT` 实际配置值直连 site-web（默认 `http://127.0.0.1:18082`），检查 `/-/live`、`/-/ready`、首页、文章、项目、关于以及页面实际引用的 `/_astro/` 资源。这里的 Node ready 会检查 Go 公开投影 API；Go 的 `/-/live`、`/-/ready` 仍由 Go 服务负责。另检查现有 Go `/assets/`、`/media/` 与管理端 `/admin/`。现有 `compose.site.preview.yaml` 的 `127.0.0.1:28082` 仍是可选的隔离候选，不等同生产 site-web。

3. 备份当前 1Panel 站点的**完整实际配置**，再把 `deploy/openresty/site.astro.conf.template` 的路由合并进去。保留实际域名、证书路径、HSTS 和现有站点设置，不直接覆盖生产配置。`/admin/` 继续去管理端，`/api/v1/`、`/media/`、`/-/` 与新增的 `/assets/` 显式路由继续去 Go；`/_astro/` 与其余公开页面去 site-web。继续使用现有 HTTP 安全头、上游重名头隐藏、maintenance marker 和静态 503 处理，不扩大 CSP。检查配置后重载 OpenResty，再从公网核对页面 marker、页面内容、`/_astro/` 资源、Go 资源及管理端。

4. 入口切换并验证后，在 GitLab 项目 CI/CD Variables 中把 `PUBLIC_SITE_MODE=astro` 持久设为普通 **Visible** 变量（建议 **Protected**），再对受保护默认分支运行使用配置确认门槛的 web pipeline，形成 v2 astro 记录。若只在一次 **Run pipeline** 中临时填写 `PUBLIC_SITE_MODE=astro`，值不会持久化，后续流水线会回到默认 `go`。Astro 模式流水线发布成功后，后续每次发布都须继续使用持久的 `astro` 值。下一次同拓扑、同模式的普通 Astro 成功发布后，previous 才成为可自动回退的上一 Astro 记录。

如果 Astro 入口或发布异常，先恢复保存的完整旧 1Panel 入口配置并检查、重载；把持久 `PUBLIC_SITE_MODE` 设回 `go`，再使用配置确认门槛发布已确认兼容 Go 入口的三镜像版本。这个跨模式处理是人工配置例外，不是自动回退。v1 previous 或 `go`/`astro` 跨模式目标不允许走普通自动回退。整个原地发布与手工入口重载不保证零停机或瞬时原子切换。

## Astro 独立候选验证（可选）

`compose.site.preview.yaml` 仅用于服务器本机隔离验证，不叠加生产 Compose、不读取生产环境文件或数据库/Redis 密钥、不切换公网路由，也不纳入生产发布、回滚和镜像清理。候选只绑定 `127.0.0.1:28082`，通过应用网络访问 Go `app:8080`。运行前须核对 Go 容器实际网络，并确认运行中的 Go 版本包含匿名公开投影 API；不要假设 `/opt/mywebsite` 指向当前源码 checkout。

在与运行中 Go 版本匹配的源码 checkout 中，找到 Go 容器并确认其网络：

```sh
docker ps --filter label=com.docker.compose.service=app --format '{{.Names}}'
docker inspect <Go应用容器名> --format '{{json .NetworkSettings.Networks}}'
export APP_NETWORK_NAME='<核对后的Go应用网络名>'
docker network inspect "$APP_NETWORK_NAME"
```

固定候选项目名后预览并启动：

```sh
export SITE_PREVIEW_PROJECT_NAME=mywebsite-site-preview
docker compose --project-name "$SITE_PREVIEW_PROJECT_NAME" -f deploy/compose.site.preview.yaml config
docker compose --project-name "$SITE_PREVIEW_PROJECT_NAME" -f deploy/compose.site.preview.yaml build site-web
docker compose --project-name "$SITE_PREVIEW_PROJECT_NAME" -f deploy/compose.site.preview.yaml up -d site-web
curl --fail --silent --show-error http://127.0.0.1:28082/-/ready
```

如需停止候选，在相同 Compose 环境变量下运行 `docker compose --project-name "$SITE_PREVIEW_PROJECT_NAME" -f deploy/compose.site.preview.yaml down`；不要添加 `--volumes`。这只停止隔离候选项目，不影响默认生产项目。
