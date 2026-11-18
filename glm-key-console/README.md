# GLM API Key 控制台

这个 Go 服务在客户端与本机推理服务之间校验 Key。管理员邀请用户，用户登录后可以创建多把 Key，并单独停用。数据库只保存 API Key 摘要，明文仅在创建时显示。停用对后续请求生效。

## 本地运行

进入 `glm-key-console/`。构建需要 Go 1.25 或更高版本以及 C 编译器，SQLite 驱动使用 cgo。

```sh
go test ./...
go build -o glm-key-console .
```

在 Bash 中初始化本地数据库的 `admin` 账号。初始密码长度为 12 至 256 字节。

```sh
read -rsp '请输入管理员初始密码 ' glm_password
printf '\n'
printf '%s\n' "$glm_password" | ./glm-key-console -db ./keys.sqlite3 init-admin
unset glm_password
./glm-key-console -listen 127.0.0.1:18080 -db ./keys.sqlite3
```

打开 [本机控制台](http://127.0.0.1:18080/ui)。页面与账号操作可独立测试。发送模型请求时，推理服务需在 `127.0.0.1:18000` 提供对应接口。

## 调用模型

客户端将 `base_url` 设为网关地址加 `/v1`，在 `Authorization: Bearer <API_KEY>` 中发送个人 Key。部署使用的模型名为 `glm-5.3-flash-awq`，可用模型以 `GET /v1/models` 的返回为准。

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/completions
```

网关按原路径转发请求并及时刷新流式响应。上游生成接口返回 HTTP 2xx 时，网关为该 Key 记一次成功调用。目前可按 Key 查看成功次数和手动停用，尚无请求数、Token 数和速率配额。

## 部署到 Linux

`glm-key-console.service` 在 `127.0.0.1:18080` 提供管理页面。`glm-key-ip.service` 提供 HTTPS，仅接受已获外部访问权限的普通账号及其 Key。两者共享 `/var/lib/glm-key-console/keys.sqlite3`。

1. 创建系统账号和组 `glmkeys`。将二进制安装到 `/opt/glm-key-console/glm-key-console`。创建 `/var/lib/glm-key-console`，将属主设为 `glmkeys`、权限设为 `0700`。首次部署时以该账号对这个数据库执行 `init-admin`。
2. 在 `/etc/glm-key-console/gateway.env` 设置 `GLM_HTTPS_LISTEN`，例如 `192.0.2.10:443`。填服务器网卡实际持有的地址。公网经 NAT 接入时，将公网 TCP 443 映射到该监听地址。
3. 将 TLS 证书链和私钥分别放入 `/etc/glm-key-console/tls/ip-cert.pem`、`ip-key.pem`。私钥应允许 `glmkeys` 读取。裸 IP 访问需要证书包含对应 IP 的 SAN，且客户端信任签发者。
4. 在 `/etc/glm-key-console/origin` 首行填写客户端实际访问的 HTTPS 根地址，例如 `https://192.0.2.10`。可在第二行写接入状态。管理页据此显示 Base URL，邀请也使用同一地址。未配置根地址时，邀请仅供本机使用。
5. 将两个 `.service` 文件放入 `/etc/systemd/system/`，执行 `systemctl daemon-reload`，再执行 `systemctl enable --now glm-key-console.service glm-key-ip.service`。
6. 管理员在本机控制台点击「邀请用户」。受邀用户通过一次性链接设置密码，再登录创建 Key。邀请有效期为七天。验收时，从客户端请求 `/v1/models`，然后停用其中一把 Key，核对它返回 `401`、另一把仍返回 `200`。

## 继续开发

- `main.go` 包含 SQLite 表结构、账号和 Key 操作、网页路由及模型反向代理。
- `console.html` 和 `assets/` 为随程序编译的管理界面。
- `main_test.go` 覆盖浏览器表单来源校验、公开地址解析和外部账号隔离。
- `expire-trial.py DATABASE USER_ID USERNAME` 用于结束指定普通账号的试用。它核对 ID 和账号名，撤销该账号的 Key、会话和外部访问权限，并保留调用记录。
