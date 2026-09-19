# 公开站服务端渲染，管理端使用 Vue SPA

公开站由 Go 使用服务端模板输出完整 HTML，管理端使用 Vue 3、TypeScript 与 Vite 构建 SPA，并通过同源 REST API 管理内容。该混合架构替代全站客户端 SPA：它保留复杂后台交互的前后端分离实践，同时确保公开文章在首个响应中包含正文、独立标题、描述和链接分享信息，不增加 Node SSR 服务。

生产环境中，1Panel OpenResty 将公开页面与 `/api/` 转发到 Go 容器，将 `/admin/` 和对应静态资源转发到管理端静态容器。
