# 使用服务端管理员会话

管理员登录使用随机不透明会话 ID，服务端会话保存在 MySQL，浏览器只持有 `Secure`、`HttpOnly`、`SameSite` Cookie，修改请求额外校验 CSRF Token。首个管理员和密码重置通过容器内 CLI 完成，密码使用 Argon2id 哈希，不把明文密码写入 Compose、环境变量或部署日志。
