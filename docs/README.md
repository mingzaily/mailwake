# Contributor documentation

[Project home](../README.md) · [Contributing](../CONTRIBUTING.md) · [User documentation](https://mailwake.oritx.com/en/docs/)

This directory contains English technical references for contributors. Installation, configuration, connection tutorials and operator procedures are maintained on the [Mailwake documentation site](https://mailwake.oritx.com/en/docs/). Keep user-facing instructions there and update the relevant technical contract here when behavior changes.

| Work area | Read first |
| --- | --- |
| Session preflight and branch integration | [Branching workflow](branching.md) |
| Build, fixtures and acceptance | [Development](development.md) |
| Published versions and release verification | [GitHub Releases](https://github.com/mingzaily/mailwake/releases) |
| IMAP adapters, workers and checkpoints | [Monitoring](monitoring.md) |
| Dispatcher, retry and preview invariants | [Delivery](notifications.md) |
| HTTP boundaries, authentication and localization | [API implementation](api.md) |
| Browser state, components and CSP | [Console](console.md) |
| Pairing, encrypted push and revocation synchronization | [Native integration](native-push.md) |
| Device authorization and resource scopes | [App management](../internal/appmanagement/README.md) |
| MIME, HTML, image privacy and content budgets | [HTML content](html-content.md) |
| SQLite, encryption, schema and persistence | [Storage](storage.md) |

Each topic owns its current implementation contract; link to that contract from related topics. Keep dated review evidence in the relevant pull request and publication evidence in [GitHub Releases](https://github.com/mingzaily/mailwake/releases), with the tested revision, environment and limitations. When reviewing content, check it against implementation, consolidate repeated rules and remove completed task instructions after preserving useful evidence. Preserve upstream dependency documentation and license notices.

The task-oriented reading route is [AGENTS.md](../AGENTS.md). Security reports follow [SECURITY.md](../SECURITY.md).
