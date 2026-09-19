# 个人技术网站 V2 原型

本目录是依据根目录 `产品方案.md` 生成的高保真离线 HTML 原型。它只用于视觉、交互与状态评审，不连接真实后端，也不向 `front/` 或 `backend/` 提供实现代码。

## 浏览方式

直接打开 `index.html`。索引页包含 11 个产品页面以及关键状态入口。

- 所有资源均位于本目录，不依赖 CDN、网络字体或构建工具。
- `?theme=light` 与 `?theme=dark` 用于评审主题；正式产品页面不显示主题切换按钮。
- `?state=empty`、`?state=error` 等参数用于进入原型状态；它们不是正式产品 API。
- 页面内交互只在当前会话模拟，刷新后恢复初始数据。

## 目录

```text
design/v2/
├── index.html
├── public/
│   ├── home.html
│   ├── articles.html
│   ├── article.html
│   ├── projects.html
│   ├── about.html
│   └── error.html
├── admin/
│   ├── login.html
│   ├── articles.html
│   ├── article-edit.html
│   ├── projects.html
│   └── project-edit.html
└── assets/
    ├── styles.css
    ├── prototype.js
    ├── default-avatar.svg
    ├── default-project.svg
    ├── favicon.svg
    └── article-diagram.svg
```

## 代表宽度

- 390px：手机
- 768px：平板
- 1440px：桌面

## 约束

- 黑白灰、直角、细边框、无阴影。
- 公开站宽松，管理端紧凑。
- 深浅主题跟随系统；主题参数只用于评审。
- 草稿没有预览能力。
- 示例身份和内容仅用于原型，不是正式站点资料。
