# 开发指南

[English](development.md) · [文档目录](README.zh-CN.md)

## 开发

开发使用 Go 与 Node.js 24.18.1。首次检查或测试前执行 `cd web && npm ci`；运行 Core 只需要二进制。

```sh
make web      # npm ci、TypeScript、Vite、许可检查和声明
make test     # go test -race 与前端测试
make check    # TypeScript、ESLint 和 go vet
make build    # bin/mailwake；设置 VERSION=v1.0.0 写入发布版本
```

自动化测试使用本地 TLS IMAP 服务、真实 SQLite 数据库和本地 HTTPS 通知接收端，覆盖文件夹监听与重启续接、邮箱隔离、配置并发修改、投递重试、Webhook 签名、鉴权与日志脱敏。真实邮箱服务商的规则和设备展示需要使用自己的账户联调。

目录职责见[英文开发指南](development.md) 与 [CONTRIBUTING.md](../CONTRIBUTING.md)，版本记录见 [CHANGELOG.md](../CHANGELOG.md)，安全问题报告方式见 [SECURITY.md](../SECURITY.md)。请求字段依据 [Bark API](https://github.com/Finb/bark-server/blob/master/docs/API_V2.md) 与 [Pushover API](https://pushover.net/api)。

前端开发先用全新临时数据目录运行 Core，然后启动 Vite：

```sh
MAILWAKE_DATA_DIR="$(mktemp -d)" ./bin/mailwake
# 另开终端：
cd web
npm ci
npm run dev
```

打开 `http://127.0.0.1:5173`。Vite 将 `/api`、`/locales` 代理到 `127.0.0.1:8080`，保留 Host 和 Origin（changeOrigin=false），登录 Cookie 属于 Vite 入口，修改请求携带内存中的 CSRF。8080 已有实例时，另外运行临时实例，并使用 `MAILWAKE_DEV_TARGET=http://127.0.0.1:18080 npm run dev`。界面验证使用临时目录。Vite 开发模式用于本地迭代；严格 CSP 以 `make build` 生成的二进制验证。

`web/` 保存 React 源码；`internal/httpapi/webdist/` 的生成资源忽略提交，仅跟踪 `.gitkeep`，通过 `go:embed all:webdist` 保证纯 Go 构建可用。构建后的 index.html 缺失时，首页返回独立嵌入的 `internal/httpapi/placeholder.html`，提示运行 make web。`make check-build` 构建并检查 webdist 的被跟踪文件保持原样，`make check-webdist` 单独执行 Git 检查。Docker 通过独立 Node 阶段构建前端，最终镜像不含 Node。

锁文件中的依赖许可全部按 MIT、ISC、Apache-2.0、BSD、OFL 与 BlueOak-1.0.0 检查；minimatch 的完整许可文本随产物提供。直接使用 Tailwind 的 MIT 编译器和扫描器，满足构建工具的许可约束；toast 使用静态样式替代 sonner 的自动注入。shadcn 的 MIT 声明位于 `web/SHADCN-LICENSE` 并进入产物。

当前支持密码或应用专用密码登录、最多 20 个 IMAP 邮箱与单一全局通知通道。Free 提供自部署监听与 BYO 投递，Pro 提供官方 App 管理和 Native 通知。Gmail 使用 TLS IMAP 与应用密码；Microsoft Graph 在执行后端完成前保持不可用。go-imap/v2 固定在 beta.8，欢迎反馈邮箱服务商兼容性问题。
## 隔离的本地 TLS 测试

自部署进程通过显式测试 CA 文件接入本地 TLS IMAP、Native Relay 和 BYO 投递。设置 `MAILWAKE_LOCAL_TEST_TLS=1`，再分别设置 `MAILWAKE_LOCAL_TEST_IMAP_CA_FILE`、`MAILWAKE_LOCAL_TEST_RELAY_CA_FILE`、`MAILWAKE_LOCAL_TEST_DELIVERY_CA_FILE` 中需要的绝对 PEM CA 文件路径。开关为空或 `0` 时，配置 CA 文件会拒绝启动；开关为 `1` 时至少需要一个有效文件。文件须为不超过 64 KiB 的普通文件，只含 CA 证书 PEM；符号链接、私钥、叶子证书及杂项数据返回 `config_local_test_tls_invalid`。

三份启动快照分别供 IMAP、Relay 和 Delivery 使用。IMAP 测试信任只用于 `localhost`、loopback IP 和 `.test` 域名，其他 IMAP 地址继续使用系统信任。Relay 测试池要求 HTTPS 且主机属于同一测试范围。Delivery 测试根只用于 Bark/Pushover/Webhook 的 HTTPS 测试主机，公开目标继续使用系统信任；用户配置中的 Pushover 地址保持官方固定地址。三条链路均验证准确的配置 hostname/IP，保持 TLS 1.2 及以上版本；进程保持系统信任，Darwin 本地测试通过显式文件安装测试根。出站测试信任、App 固定 Core HTTPS origin、Platform 资格签名信任分别配置。

```sh
MAILWAKE_LOCAL_TEST_TLS=1
MAILWAKE_LOCAL_TEST_IMAP_CA_FILE=/absolute/path/to/test-root-ca.pem
MAILWAKE_LOCAL_TEST_RELAY_CA_FILE=/absolute/path/to/test-root-ca.pem
MAILWAKE_LOCAL_TEST_DELIVERY_CA_FILE=/absolute/path/to/test-root-ca.pem
MAILWAKE_RELAY_URL=https://127.0.0.1:19444
```

`localhost:11993` 的 IMAP 测试证书需要 `localhost` DNS SAN；上面的 Relay 需要 `127.0.0.1` IP SAN。IMAP AUTH 保留原配置用户名。Relay 和 BYO 保持禁止跳转，失败返回安全的通道或协议领域码。三份独立配置可以明确安装同一测试 CA。运行时分别通过 `Options.IMAPRootCAs`、`Options.RelayRootCAs`、`Options.DeliveryRootCAs` 注入；Delivery 沿现有 sender HTTP client 注入，通道配置保持现有 HTTPS 和目标校验规则。生产环境保持四个本地测试变量为空。
