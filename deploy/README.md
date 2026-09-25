# Stage 4 deployment skeleton

这些文件只提供可审查的构建、候选端口、OpenResty 路由和发布脚本骨架。它们不包含真实域名、1Panel 路径、容器名、网络名、OSS 参数或凭据。

## Compose

项目 Compose 只有 `app` 和 `admin-web` 两个服务。稳定端口是 `127.0.0.1:18080` 与 `127.0.0.1:18081`。稳定发布必须叠加 `deploy/compose.onepanel.example.yaml`，由只读预检提供 `MYSQL_NETWORK_NAME`，只让 `app` 加入实际外部 MySQL 网络；项目不会猜测外部网络名。候选发布再叠加 `deploy/compose.candidate.yaml`，端口覆盖为 `28080` 与 `28081`。

`db_password` 是外部 Docker secret，上传目录是唯一项目数据卷。Compose 不创建 MySQL、OpenResty 或 OSS 服务。运行容器按已接受决策以 root 执行，配置没有声称已经完成容器加固。

### 构建依赖代理

Dockerfile 默认使用 `GOPROXY=https://proxy.golang.org,direct`、`GOSUMDB=sum.golang.org` 和 `NPM_CONFIG_REGISTRY=https://registry.npmjs.org`。Compose 会把这些值传给对应的构建阶段，服务器可以在仓库外的 `.env` 中覆盖它们。当前服务器如果访问 `proxy.golang.org` 超时，可设置 `GOPROXY=https://goproxy.cn,direct` 后重新构建。`GOSUMDB` 保持 `sum.golang.org`；只有独立确认 `sum.golang.org` 不可达且确认组织代理可信后，才讨论替代校验服务，不能把 `GOSUMDB=off` 作为默认修复。

## OpenResty

先把 `openresty/site.conf.template` 中的域名、TLS 路径和网站目录替换为预检记录中的值，再运行实际 OpenResty 配置测试。`/admin` 重定向到 `/admin/`；只有 `/admin/*` 交给管理端 SPA fallback；`/api/v1/*`、`/media/*`、`/-/*` 和其他公开页面交给 Go。模板只把连接失败、502、503、504 映射为 `static-503.html`，应用的普通 404 保持 404。

维护模式使用与模板中相同网站目录下的 `maintenance.flag`。发布脚本应在服务器上以临时文件加原子 rename 创建或删除它，避免观察到半写入状态。

## 脚本安全边界

`scripts/release.sh`、`rollback.sh` 和 `backup-batch.sh` 默认只做预检/计划输出。它们要求 `MYWEBSITE_SERVER_CONFIG` 指向预检后、仓库外的真实配置，并拒绝 `<...>`、`TODO`、`REPLACE` 等占位值。没有真实服务器上下文时会 fail-closed；本机不会触发生产动作。

`backup-batch.sh` 的执行路径（需要明确的服务器配置和确认开关）按批次生成 `mysqldump`、uploads 归档、包含 Git Tag/Schema 版本/大小/SHA-256 的清单，并以 `ready/current` 的原子指针切换保留上一份完整批次。密码只从服务器外部文件读取，仓库不保存 secret。
