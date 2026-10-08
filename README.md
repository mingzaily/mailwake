# Mailwake Core

**Notifications for the email folders that matter.**

[![CI](https://github.com/mingzaily/mailwake/actions/workflows/ci.yml/badge.svg)](https://github.com/mingzaily/mailwake/actions/workflows/ci.yml)
[![License: AGPL-3.0-only](https://img.shields.io/badge/license-AGPL--3.0--only-blue)](LICENSE)

English · [简体中文](README.zh-CN.md)

Mailwake Core is a self-hosted IMAP folder monitor. When a mail rule moves a message out of your inbox, Core can still notify you through Bark, Pushover or a signed webhook. Keep reading and replying in your existing mail client.

![Mailwake Core overview](docs/images/overview.en.jpg)

<details>
<summary>Choose folders and check methods</summary>

![Folder selection and connection budget](docs/images/folders.en.jpg)

</details>

*Screenshots use local demonstration mailboxes and synthetic messages.*

## Features

- **Folder-level monitoring** — up to 20 mailboxes, with realtime IMAP IDLE or scheduled checks for each folder.
- **Your notification service** — Bark, Pushover and signed webhooks, with configurable previews and retries.
- **Persistent delivery queue** — SQLite stores monitoring progress and pending notifications across restarts.
- **Built-in Web console** — mailbox setup, folder selection, delivery history and diagnostics in English and Chinese.
- **Self-contained deployment** — a Go binary with embedded Web assets, encrypted credentials and persistent local storage.

Core, its Web console and script API are free to use. The official Mailwake App and Pro services are separate integrations; see [App integration](docs/native-push.md).

## Quick start

Requires Git, Docker Compose v2 and a mailbox that supports TLS IMAP. Use an app password when required by your provider.

```sh
git clone https://github.com/mingzaily/mailwake.git
cd mailwake
docker compose up -d --build --pull never
docker compose logs core
```

Open **http://127.0.0.1:8080** on the Docker host. Enter the setup code from the logs, create an administrator password of at least 12 characters, then connect a mailbox and choose folders to monitor.

The default port is host-local and data persists in a Docker volume. For remote access, HTTPS, backups and upgrades, follow the **[deployment guide](docs/deployment.md)**.

## Release status

The public `main` branch contains development source for 1.0.0. The command above builds it locally. Versioned container images are published to **`ghcr.io/mingzaily/mailwake`**; binaries and release notes appear in [GitHub Releases](https://github.com/mingzaily/mailwake/releases) when a version ships.

## Documentation

| Guide | Contents |
| --- | --- |
| [Deployment](docs/deployment.md) | Docker, HTTPS, backups, upgrades and password recovery |
| [Mailbox monitoring](docs/monitoring.md) | Provider compatibility, folder behavior and data privacy |
| [Notifications](docs/notifications.md) | Channels, previews and webhook signatures |
| [HTTP API](docs/api.md) | Authentication, mailbox configuration, deliveries and logs |
| [App integration](docs/native-push.md) | Native push, device pairing and scoped content access |
| [Development](CONTRIBUTING.md) | Local setup, checks, contribution guidelines and releases |

Browse the [complete documentation](docs/README.md) for console and storage references.

## Contributing and security

Bug reports and focused contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report vulnerabilities privately through the channels in [SECURITY.md](SECURITY.md).

## License

Mailwake Core is licensed under **AGPL-3.0-only**. See [LICENSE](LICENSE) and [COPYRIGHT](COPYRIGHT). Third-party components retain their own licenses; notices are included in builds.
