# Security Policy

Mailwake Core handles mailbox credentials, so security reports are treated as the highest priority.

## Reporting a vulnerability

Please report vulnerabilities privately through [GitHub Security Advisories](https://github.com/mingzaily/mailwake/security/advisories/new), or by email to dev@oritx.com. Do not open a public issue.

Include the affected version (`/api/v1/diagnostics` or the startup log shows it), the configuration involved with all secrets removed, and steps to reproduce. You will receive an acknowledgement within 3 days and a plan within 10 days. Fixes are released as a new version and credited in the changelog unless you prefer otherwise.

## Supported versions

Security fixes are made for the latest release. Please upgrade before reporting an issue that may already be fixed.

## Scope

In scope: credential handling, the HTTP API and its authentication, the diagnostics export, notification channel requests and webhook signatures, and the container image.

Out of scope: the security of your mail provider, notification services such as Bark or Pushover, your reverse proxy, and exposing the API to the internet without HTTPS.
