# Mailwake

**让重要文件夹里的邮件，也能及时通知你。**

[![CI](https://github.com/mingzaily/mailwake/actions/workflows/ci.yml/badge.svg)](https://github.com/mingzaily/mailwake/actions/workflows/ci.yml)
[![License: AGPL-3.0-only](https://img.shields.io/badge/license-AGPL--3.0--only-blue)](LICENSE)

[English](README.md) · 简体中文 · [使用文档](docs/README.zh-CN.md) · [版本下载](https://github.com/mingzaily/mailwake/releases)

Mailwake 在你自己的服务器上监听 IMAP 文件夹。邮件被规则自动归类后，通过 Bark、Pushover 或签名 Webhook 及时收到通知。阅读和回复继续使用你习惯的邮件客户端。

## 可以做什么

- **选择重要的文件夹。** 最多连接 20 个邮箱，每个文件夹可选择 IMAP IDLE 实时监听或定时检查。
- **使用习惯的通知渠道。** 接入 Bark、Pushover 或 Webhook，配置通知预览和重试。
- **通过浏览器管理。** 配置邮箱、查看投递记录、排查运行问题，支持中英文界面。
- **数据保存在自己的服务器。** 凭据加密存储，SQLite 保存监听进度与待发通知，重启后继续处理。

自部署服务、Web 控制台和脚本 API 免费使用。官方 App 与 Pro 服务通过独立接口接入，详见 [App 接入说明](docs/native-push.zh-CN.md)。

## 快速开始

准备 Git、Docker Compose v2，以及已开启 TLS IMAP 的邮箱。按服务商要求使用应用专用密码或授权码。

```sh
git clone https://github.com/mingzaily/mailwake.git
cd mailwake
docker compose up -d --build --pull never
docker compose logs core
```

1. 在 Docker 所在主机打开 **http://127.0.0.1:8080**。
2. 输入日志中的设置码，创建管理员账号，密码至少 12 个字符。
3. 添加邮箱，选择需要监听的文件夹。
4. 进入通知设置，配置渠道并发送测试通知。

上述命令从当前 `main` 源码构建。当前发布版本为 **v1.0.0-rc.6**，即 1.0.0 的候选版本。默认端口仅绑定本机，数据保存在 Docker 持久卷。远程访问、HTTPS、备份和升级步骤见[部署指南](docs/deployment.zh-CN.md)。

希望直接使用构建好的版本？可[下载 macOS/Linux 二进制](https://github.com/mingzaily/mailwake/releases/tag/v1.0.0-rc.6)，或使用容器镜像 `ghcr.io/mingzaily/mailwake:v1.0.0-rc.6`（amd64、arm64）。`main` 分支持续开发，版本变更见 [CHANGELOG.md](CHANGELOG.md)。

## 界面预览

在概览中查看邮箱连接和文件夹监听状态。

![Mailwake 运行概览](docs/images/overview.zh-CN.jpg)

<details>
<summary>查看文件夹选择与连接额度</summary>

![文件夹选择与连接额度](docs/images/folders.zh-CN.jpg)

</details>

*截图使用演示邮箱和模拟邮件，界面外观可能随版本调整。*

## 使用文档

| 你想了解 | 对应指南 |
| --- | --- |
| 部署、升级与备份 | [部署指南](docs/deployment.zh-CN.md) |
| 邮箱兼容性与文件夹行为 | [邮箱监听](docs/monitoring.zh-CN.md) |
| 通知渠道与 Webhook 配置 | [通知通道](docs/notifications.zh-CN.md) |
| 连接官方 App | [App 接入](docs/native-push.zh-CN.md) |
| 通过脚本管理服务 | [HTTP API](docs/api.zh-CN.md) |
| 本地开发与参与贡献 | [贡献指南](CONTRIBUTING.md) |

管理界面、存储等说明见[完整文档目录](docs/README.zh-CN.md)。使用问题可提交到 [GitHub Issues](https://github.com/mingzaily/mailwake/issues)；安全漏洞请按 [SECURITY.md](SECURITY.md) 私下报告。

## 许可证

本仓库的自部署 Core 使用 **AGPL-3.0-only**，详见 [LICENSE](LICENSE) 与 [COPYRIGHT](COPYRIGHT)。第三方组件保留各自许可证，相关声明随构建产物提供。
