---
status: superseded by ADR-0011
---

# 使用独立前端静态容器与 Go API 容器

生产环境由 Compose 运行 Vue 静态站容器和 Go API 容器，1Panel 管理的 OpenResty 在同一域名下将页面请求与 `/api/` 请求转发到对应服务。开发阶段保持前后端独立工程，生产环境保持同源，从而避免 CORS 与跨站 Cookie，同时使两类构建产物均可固定为容器镜像。
