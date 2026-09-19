# 复用 1Panel 管理的 OpenResty 作为公网入口

目标物理服务器使用 1Panel v1.10.34-lts 管理 OpenResty，因此首版继续由它负责域名、TLS 和公网反向代理。项目 Compose 栈不再部署 Caddy 或第二套入口，也不直接占用宿主机 80/443。

1Panel 主程序版本没有与 App Store 模板一一绑定的历史快照，部署前必须读取服务器落盘 Compose 并通过 `docker inspect` 核验网络、容器名和端口。若 OpenResty 实际使用 host 网络，则 Go 与管理端容器只发布到 `127.0.0.1:18080/18081`；若 MySQL 8.4.11 实际加入 `1panel-network`，则只有 Go 容器额外加入该外部网络并通过实际容器名连接 `3306`。任何检查不符合预期时停止部署，不自动修改 1Panel 管理的应用网络。
