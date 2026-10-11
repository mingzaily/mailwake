# 部署 Mailwake Core

[English](deployment.md) · [文档目录](README.zh-CN.md)

Core 在你的服务器监听 TLS IMAP 邮箱，并通过 Bark、Pushover 或 Webhook 发送通知。Free 的 Web 控制台和脚本 API 可独立运行。官方 App、Pro 和原生推送另有服务接入条件。

## 1. 准备与启动

准备 Git、Docker Engine 或 Docker Desktop、Docker Compose v2，以及允许 TLS IMAP 登录的邮箱。邮箱开启两步验证时，按服务商要求创建应用专用密码。服务器需要持续运行，并能访问邮箱和通知服务。

当前从公开源码构建；版本镜像统一发布到 `ghcr.io/mingzaily/mailwake`，二进制附在 [GitHub Releases](https://github.com/mingzaily/mailwake/releases)。以实际完成发布的版本为准。

```sh
git clone https://github.com/mingzaily/mailwake.git
cd mailwake
docker compose up -d --build --pull never
docker compose logs core
```

首次构建需要下载基础镜像、Go 与 npm 依赖。服务使用非 root 用户、只读容器文件系统和 `core-data` 持久卷，监听主机的 `127.0.0.1:8080`。所有后续命令在同一个克隆目录执行，保持 Compose 项目名称不变，以继续使用原来的数据卷。

启动错误中，`data_directory_permission_denied` 表示文件系统权限不足，`data_directory_read_only` 表示只读挂载，`storage_open_failed` 表示其他存储初始化失败。这些诊断从 `v1.0.0-rc.2` 起生效；`v1.0.0-rc.1` 对其中部分错误仍显示 `request_failed`。

## 2. 创建管理员与连接邮箱

在运行 Docker 的电脑打开 `http://127.0.0.1:8080`。远程服务器可先通过 SSH 转发端口，把 `user@server` 替换为实际 SSH 地址：

```sh
ssh -L 18080:127.0.0.1:8080 user@server
```

保持 SSH 连接，在本机打开 `http://127.0.0.1:18080`。使用日志中的一次性设置码，创建至少 12 位的管理员密码。设置完成前每次启动都会更新设置码。

在向导中添加邮箱，填写 TLS IMAP 主机、端口（通常为 993）、用户名与应用密码。扫描并选择文件夹，为需要及时通知的文件夹选择实时监听，其余可定时检查。最多连接 20 个邮箱，每个邮箱独立管理连接额度。邮箱和通知配置可以稍后补充。

在通知设置选择 Bark、Pushover 或 Webhook，保存后发送测试通知；再向受规则影响的文件夹投递一封新邮件，检查监听状态和最近投递。阅读与回复继续使用原来的邮件客户端。

## 3. 配置 HTTPS

远程 Web 访问和官方 App 管理需要可访问的地址；App 使用受信任的 HTTPS。将自己的域名指向服务器，在同一主机安装反向代理。下面是宿主机 Caddy 的配置，替换 `mail.example.org`：

```caddyfile
mail.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

按 Caddy 的要求开放 80/443 端口，并确认域名解析与证书签发成功。8080 继续绑定本机。代理须保留原始 Host（含端口）并设置 X-Forwarded-For，Caddy 默认满足这两项要求。容器内运行的代理应使用对应 Docker 网络地址，容器的 `127.0.0.1` 指向代理自身。

同一主机使用 nginx 时，替换下面的域名与证书路径：

```nginx
server {
    listen 443 ssl;
    server_name mail.example.org;
    ssl_certificate /etc/letsencrypt/live/mail.example.org/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mail.example.org/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Core 信任这些代理对端网段：`127.0.0.0/8`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`。监听端口应仅对代理和可信本机客户端开放。

## 4. 接入官方 App 与 Pro

Free 部署到这里即可使用。官方 App 尚未上架；App Store 下载、正式 Relay 地址和 Platform 公钥信任文件以官方发布为准。

接入 App 管理时，先取得并核实官方公钥信任 JSON。在本地 `compose.yaml` 的 `services.core` 下，将以下配置合并到已有的 `environment` 和 `volumes` 中，保留原有环境变量和 `core-data:/data` 数据卷。将 `source` 替换为已存在的公钥文件绝对路径：

```yaml
environment:
  MAILWAKE_APP_MANAGEMENT_TRUST_FILE: /etc/mailwake/app-management-trust.json
volumes:
  - type: bind
    source: /absolute/path/to/app-management-trust.json
    target: /etc/mailwake/app-management-trust.json
    read_only: true
    bind:
      create_host_path: false
```

执行 `docker compose up -d` 应用配置。更新公钥文件后执行 `docker compose restart core`。需要指定 Relay 时，在本地 `.env` 中设置 `MAILWAKE_RELAY_URL`；留空使用默认生产 Relay。

在 Web 的“App 设置”页面创建邀请，分别授予管理、原生推送与正文读取权限。Pro 购买、Core 所有者授权和设备配对分别验证。公钥轮换、scope 与资格校验见 [App 管理说明](../internal/appmanagement/README.md)。

## 5. 备份、恢复与升级

备份包含邮箱凭据、管理员和设备授权等敏感状态，保存在受限目录或加密存储。完整复制数据卷，包括 SQLite 文件和 `secret.key`；密钥与数据库共同用于恢复。

先停止写入，再复制整个数据目录。所有操作使用同一份 `compose.yaml`：

```sh
docker compose stop core
umask 077
backup="../mailwake-backup-$(date +%Y%m%d-%H%M%S)"
mkdir "$backup"
docker compose cp core:/data/. "$backup/"
docker compose start core
```

复制成功后再继续升级。保持原目录和数据卷，先备份，然后更新源码并重建：

```sh
git pull --ff-only
docker compose up -d --build --pull never
docker compose logs --tail=50 core
```

`main` 包含持续开发的版本；需要固定版本时，先从 Releases 选择已存在的 tag，再 checkout 该 tag 并重建。版本镜像发布后也可在 `compose.yaml` 的 `image` 固定对应 tag，再执行 `docker compose pull core` 与 `docker compose up -d --no-build`。数据库格式变化时，回退应同时恢复升级前的完整备份与对应代码版本。

恢复时停止 Core，将完整备份复制到目标空数据卷，确保目录及文件归容器用户 `10001:10001` 所有，再启动与备份相符的版本。保留原备份直至验证邮箱、订阅、投递状态和设备授权。`docker compose down` 保留卷；`docker compose down -v` 会删除卷及其中所有状态。

## 6. 常见问题

- **远程浏览器打不开**：默认仅监听服务器本机。先使用 SSH 转发，或检查 HTTPS 代理与域名解析。
- **设置码失效**：读取当前容器日志；完成初始化前，重启会轮换设置码。
- **邮箱认证失败**：确认 TLS IMAP 已启用，主机与端口正确，并使用服务商允许的应用密码。Gmail 当前使用允许 IMAP 与应用密码的账号。
- **收不到通知**：先发送通道测试，再检查邮箱监听状态、目标 Folder 是否已订阅及最近投递记录。
- **忘记管理员密码**：停止 Core，使用同一数据卷交互重置，再启动：

```sh
docker compose stop core
docker compose run --rm -it core admin reset-password
docker compose start core
```

恢复命令会获取数据目录锁，执行前先停止 Core。重置密码会注销浏览器会话，保留 API Token、邮箱与队列。问题反馈可附 Web 控制台的脱敏诊断；公开前检查附件中自己补充的日志和文件夹名称。

## 从源码构建

需要 Go 1.25 与 Node.js 24.18.1（`.nvmrc`），使用 npm 与已提交的 `web/package-lock.json`。

```sh
git clone https://github.com/mingzaily/mailwake && cd mailwake
make build
./bin/mailwake
```

监听与存储环境变量为 `MAILWAKE_LISTEN`（默认 `127.0.0.1:8080`，容器为 `0.0.0.0:8080`）与 `MAILWAKE_DATA_DIR`（默认 `data`，容器为 `/data`）。`MAILWAKE_RELAY_URL` 未设置或为空时，默认使用官方生产 Relay（`https://notify.mailwake.oritx.com`）。沙盒测试设置为 `https://notify-sandbox.mailwake.oritx.com`，自定义 Relay 填写对应 origin。该进程配置通过 Docker 环境变量注入（随附 Compose 文件从宿主机环境或 `.env` 读取），修改后重建容器；Web 控制台负责手机配对。业务配置从 SQLite 读取；配置文件和凭据环境变量已移除。

停止 Core 后，在相同数据目录运行恢复命令：

```sh
./bin/mailwake admin reset-password  # 从交互终端读取密码，输入不回显
./bin/mailwake admin reset-setup
```

两条命令都先获取数据目录锁。重置密码注销全部浏览器会话，保留 API Token；重置设置清除管理员、会话和 API Token，下次启动打印新设置码，保留邮箱配置、订阅和队列。
