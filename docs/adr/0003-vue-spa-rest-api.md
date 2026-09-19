---
status: superseded by ADR-0011
---

# 使用 Vue SPA 与 REST API 分离前后端

前端采用 Vue 3、TypeScript 与 Vite 构建客户端 SPA，后端通过 REST 风格 JSON API 提供能力。该选择用于完整实践前后端分离，同时以 Vue 控制内容浏览和后台表单的实现复杂度；代价是需要独立维护前端构建、客户端路由、API 契约和加载失败状态。
