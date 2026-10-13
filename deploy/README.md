# 开发者 CI/CD 与生产部署

日常流程是 `功能分支 → Merge Request → main`。分支和 MR 运行 Go、管理端校验；受保护的默认分支通过校验后，自动构建、推送、扫描并部署。保留两个现有 Runner，不增加常驻服务。

## 流水线

| 阶段 / 作业 | Runner | 内容 |
| --- | --- | --- |
| `verify_go` | Docker，`security` | gofmt、go vet、Go 测试、部署脚本语法及模拟恢复测试；生成 JUnit |
| `verify_admin` | Docker，`security` | 锁定依赖安装、类型检查、单元测试、生产构建；生成 JUnit |
| `build_images` | Shell，`server` | 构建 app/admin-web，推送完整 Git SHA 标签，生成含两个镜像 digest 的 `release.env` |
| `scan_images` | Docker，`security` | Trivy 远程扫描两个 digest，保存 JSON；有修复版本的 HIGH/CRITICAL 漏洞阻止部署 |
| `deploy_prod` | Shell，`server` | 使用扫描通过的 digest 更新服务，检查本机健康与公网关键页面，更新发布记录并清理旧镜像 |

构建和部署分开。部署使用 `--no-build --pull never`，先显式拉取待部署 digest；不会在 deploy 作业重新构建。`release.env` 不含生产配置或密码，作为作业产物保留 14 天。扫描报告、JUnit 与脱敏部署诊断也保留 14 天；扫描报告可下载，当前没有接入付费安全面板。

`scan_images` 在生成 JSON 报告前先预热 Trivy 漏洞库，默认依次使用 `registry.gitlab.com/gitlab-org/security-products/dependencies/trivy-db:2` 和 `ghcr.io/aquasecurity/trivy-db:2`。可用 GitLab CI/CD 变量 `MYWEBSITE_TRIVY_DB_REPOSITORIES` 覆盖空格分隔的源列表，或用 `MYWEBSITE_TRIVY_DB_DOWNLOAD_TIMEOUT` 覆盖单源下载超时（默认 `3m`）。成功下载的数据库保存在 Trivy 缓存中，报告扫描和漏洞门禁共用该缓存；门禁失败时 GitLab 仍会回传已更新的缓存。所有源都失败时扫描作业失败并阻止部署。

分支推送已有打开的 MR 时仅运行 MR 流水线。新提交可以取消旧的校验作业；构建、扫描和生产操作不自动中断。生产发布与回退共用 `production` 资源锁，脚本在更新前核对远程默认分支 HEAD，阻止过时流水线覆盖新版本。

## 首次使用前

1. GitLab Container Registry 已启用，默认分支（当前为 `main`）设为 protected。Shell Runner 使用 `server` 标签、仅服务本项目，并设为 protected；Docker Runner 使用 `security` 标签，允许执行分支/MR 校验，不挂载宿主机 Docker socket。
2. Shell Runner 上存在 Bash、Git、Docker Compose v2、curl、install、sha256sum、awk、grep 等常用命令，`gitlab-runner` 可操作 Docker。能访问 GitLab Registry；扫描 Runner 能拉取 `aquasec/trivy:0.75.0`、读取镜像并下载 Trivy 漏洞库。
3. 保留现有 `/opt/mywebsite/deploy/env.production`、`secrets/db_password`、`secrets/redis_password`、`data/uploads`，确保 `gitlab-runner` 可读配置及两个单行密码文件、可写上传目录。密码不会进入 Git、镜像或产物。
4. 创建新发布记录目录（在服务器执行一次）：

   ```sh
   sudo install -d -o gitlab-runner -g gitlab-runner -m 0700 /opt/mywebsite/deploy-state
   ```

5. 核对 GitLab CI/CD 变量中的 `MYSQL_NETWORK_NAME`、`COMPOSE_PROJECT_NAME`、`APP_PORT`、`ADMIN_WEB_PORT`、`PRODUCTION_URL`。仓库默认分别是 `1panel-network`、`mywebsite`、`18080`、`18081`、`https://tazmi.online`。**外部网络以服务器实际配置为准**；如果现有 app 的 MySQL 网络是 `Mysql`，在 GitLab 变量中设置该值。Redis 与 app 必须共享可解析 `Redis` 的网络，连接地址和密码路径继续由服务器的 `env.production` 提供。

当前稳定 Compose 的 uploads 和密码路径固定为 `/opt/mywebsite/...`，按此目录使用。迁移服务器路径需要同时调整 Compose，属于下述配置变更流程。GitLab 内置的 `CI_REGISTRY_USER` / `CI_REGISTRY_PASSWORD` 用于本次作业登录，无需增加长期 Registry token。

建议在 GitLab 项目设置中开启防止过时部署。首次扫描可能发现现有基础镜像或依赖中的漏洞；应先查看两个 JSON 报告并修复有补丁的 HIGH/CRITICAL 问题，再重跑。扫描工具或漏洞库访问失败同样阻止部署。

## 发布与自动恢复

生产 Compose 按以下顺序叠加：`compose.yaml`、`deploy/compose.onepanel.example.yaml`、`deploy/compose.redis.onepanel.yaml`、`deploy/compose.images.yaml`。只管理 app/admin-web，继续使用外部 MySQL、Redis 和现有 OpenResty。

每次发布先预检配置、文件权限、网络及 Compose，拉取两个 digest，再更新两个服务。随后核对运行镜像 ID，检查本机 `/-/live`、`/-/ready`、管理端，以及公网首页、文章、项目、关于、管理端和 CSS 的状态、类型与页面标记。

`deploy-state/current` 与 `previous` 保存当前、上一成功版本的提交、digest 和配置指纹。第一次采用此流程时，会尝试从健康的现有 app/admin-web 初始化 current：两个服务须属于同一个完整 SHA 标签版本，镜像需有本项目 RepoDigest，旧提交必须在 CI checkout 中可读取。digest 形式的现有容器还需有可核对的本地 SHA 标签。无法识别时会在更新服务前停止，需先核对现有镜像和 Git 对象。若服务器没有现有服务，可完成首次发布，但首次失败时没有已验证版本可恢复。

普通发布在启动、健康或公网检查失败后，会尝试重新应用 current 中的镜像对；恢复成功后作业仍显示失败，成功记录不会前移。已验证成功才更新 current/previous。重复部署相同版本不覆盖上一版本。该流程是原地更新，可能短暂中断访问；不提供零停机保证。进程被强制终止或服务器断电后的状态需要人工核对。

成功后仅清理本项目超过两个保留版本的本地 SHA 镜像标签，保留仍被其他容器引用的镜像。清理失败只记录警告，不把健康发布改判失败；不运行全局 prune，不删除卷或外部服务。GitLab Registry 的清理策略是独立配置，需保留准备回退的 SHA 标签/digest，使回退时仍可拉取。

## 手动回退

在 GitLab 的 **Run pipeline** 中选受保护默认分支，设置 `PIPELINE_ACTION=rollback`，创建一条新的流水线。它只执行 `rollback_prod`，不重新构建或扫描旧版本，不重跑历史 deploy 作业。

回退目标来自服务器的 previous。脚本检查其迁移目录、Compose、环境与部署参数和 current 兼容，然后拉取并应用 previous 的两个 digest。成功后交换 current/previous；失败则尝试恢复 current，作业保持失败。previous 不存在、镜像已被仓库清理、旧 Git 对象缺失或配置不兼容时会停止。

## 数据库、Compose 或环境变更

自动发布会比较 `db/migrations`、稳定 Compose 文件、服务器 `env.production` 和网络/端口等参数的指纹。它们变化时，普通流水线停止，不自动操作数据库。

先人工完成备份，并确认新旧程序与数据库/配置的兼容性，再从受保护默认分支创建 web 流水线：

```text
PIPELINE_ACTION=deploy
ALLOW_DEPLOY_CONFIG_CHANGE=true
CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
```

该例外只表示操作者已完成备份和兼容性确认，脚本不会替你备份、执行迁移或还原数据库。**此路径关闭自动旧镜像恢复**，发布失败后需要按本次配置/迁移的实际情况处理，避免把不兼容的旧程序直接放回线上。

## 定时扫描与依赖刷新

可创建默认分支的 Pipeline schedule，运行 `scan_main_images`。它扫描该次默认分支 SHA 的两个已发布镜像标签，不构建、不部署；如果该提交未发布镜像或标签被清理，扫描会失败。它扫描的是 main 对应的镜像，并不承诺等同于当前线上版本。

日常构建沿用本地基础镜像缓存。需刷新时，在默认分支 web 流水线设置 `PULL_BASE_IMAGES=true`，重新执行校验、构建、扫描和部署。

## 本地验证与旧脚本

可用 Bash 运行不连接外部服务的流程检查：

```sh
bash deploy/tests/build-scan-test.sh
bash deploy/tests/deploy-production-test.sh
```

这些检查模拟 Docker、Registry、HTTP 和发布记录，验证编排、失败恢复与清理边界。它们不等于真实镜像构建、Trivy 扫描、GitLab CI Lint 或生产验收；首次实际流水线仍需查看各阶段结果。

`scripts/release.sh`、`rollback.sh`、`backup-batch.sh` 与候选端口 overlay 是早期手动发布工具，本次 CI 不调用它们；它们仍要求单独的服务器配置和执行开关。现有 OpenResty 路由继续保持：`/admin/*` 交给管理端 SPA，API、媒体、健康检查和公开页面交给 Go；本次不修改反向代理配置。
