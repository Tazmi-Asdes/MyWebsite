# 使用标准库 net/http 构建 Go HTTP 服务

Go 后端使用标准库 `net/http` 和增强后的 `ServeMux` 实现路由与中间件，不引入 Gin、Chi 或其他 Web 框架。当前 REST API 规模有限，标准库已经支持方法路由与路径参数；该选择用于直接学习 Go HTTP 模型并减少框架耦合，公共中间件仅按实际需求组合。
