# Release records

[Contributor documentation](README.md) · [Changelog](../CHANGELOG.md)

This file preserves dated evidence for the revisions and environments named below. Use [Branching workflow](branching.md) for current release policy and [Development](development.md) for current checks. Earlier results establish historical coverage; each candidate needs verification of its own changes.

## RC source mapping — 2026-10-16

During maintainer-authorized prerelease history consolidation, `v1.0.0-rc.1` was moved from `35313acaec401f581c8a4a015fab36f53941bd3d` to the mainline commit `ee97a75f767115028f4a286dd97dbb2f3f2251cc`. Both commits have the identical source tree `60d212c4f9f88e8a62de74d1a9c5416808589b42`. Existing binaries and images retain their original embedded build revision and checksums. Use this mapping when comparing those artifacts with the tag; the metadata difference does not indicate changed source. This was a one-time history cleanup; subsequent release tags follow the immutable-tag policy.

## Prerelease baseline reset — 2026-10-14

The maintainer requested consolidation of the public Core history and a fresh `v1.0.0-rc.1` release sequence. The source snapshot is the complete implementation at `7b0eebd7e643b8f17505d69c91abf1e6a7323996`, including the previously verified rc11 code and contributor quality rules. This operation changes history and release numbering while preserving behavior and public interfaces.

The 22 mainline commits were grouped into nine functional stages. Each stage reuses the exact Git tree at its former boundary, and the final consolidated tree matches the original tip. A subsequent documentation commit records this baseline. Original authorship and co-author trailers are preserved. The maintainer retains a verified Git bundle, an old-to-new commit map, release metadata and the former downloadable artifacts outside the source repository.

Earlier development RC tags and releases are retired. The new rc1 has a different source revision from the earlier development rc1. Existing downloads remain historical artifacts; identify the new release by its source revision and published SHA256 checksums. The database baseline policy remains as documented in [storage](storage.md).

### Review and evidence

Scope: Core history, release records and packaging. Application code, dependency locks, the database baseline and HTTP contracts retain their reviewed content.

| Review | Score | Evidence and remaining limitations |
| --- | --- | --- |
| Business logic | 9.0/10 | Exact tree comparisons preserve authorization, mailbox lifecycle, content privacy, revocation and retry behavior. Existing behavior and concurrency tests apply to identical source. Long-running behavior across mailbox providers remains an operational observation. |
| Code design | 9.0/10 | Functional history boundaries and concise messages retain coherent implementation stages. Existing service and UI responsibilities are preserved. Runtime coordination remains concentrated in one manager; further separation is driven by concrete extension needs. |

The rc11 implementation at `2ec20ca4cfe60c631eceb904652aef55d437e2f6` passed 165 frontend tests, all Go race tests, both vet variants, production builds, CSP and license checks, four binary archive checksums, anonymous dual-platform image retrieval and container startup. The maintainer confirmed real-mail and device acceptance before that release. The subsequent change at `7b0eebd` updated contributor rules only.

For the new baseline, `make test` passed with 165 frontend tests and all Go race tests; unchanged Go packages reused their test cache. `make check` and `make check-build VERSION=v1.0.0-rc.1` passed on macOS arm64 with Node.js 24.18.1 and Go 1.25.14. Formatting, whitespace and the complete consolidated mainline gitleaks scan passed. The production source trees match the previously accepted implementation. The release workflow repeats CI, builds four binary archives and two container architectures, and publishes checksums. Published assets are verified against the tagged source revision after the workflow completes.

The reset introduces no new feature or provider integration. User configuration and running deployments remain under the operator's control. GitHub Actions toolchain deprecation notices are tracked as maintenance work; successful checks establish the tested runner configuration, while future runner updates require their own verification.

## HTML content review — 2026-10-13

The following record preserves the original implementation and rc10 acceptance evidence. Its RC names, dependency versions, pending items and validation results describe that historical candidate. Current behavior and repeatable acceptance steps live in [HTML content](html-content.md).


- Business review: verified free owner-authorized reading, signed resource scope, same-message MIME selection, unread state, remote-image privacy and complete fallback on size limits. Fixed invalid CID transfer encodings so they remove the resource while preserving readable content.
- Design review: kept MIME selection, HTML/CSS allowlists and response budgeting in the content package, with protocol fetching in the IMAP adapter. Bounded repeated CID expansion during sanitization, reused request-local decoded resources and kept notification extraction independent. No known blocking finding remains in the HTML reading path.
- Validation: full Core race tests, production/test-tag vet, production build and isolated startup/health check passed. Targeted parsing/IMAP/HTTP race tests passed again after the CID correction. A 20-second fuzz run on the patched parser completed 24,637 executions without a failure. Gitleaks found no secrets in the new content implementation, HTTP tests or integration guide.
- Dependency check: the new content path reports zero reachable vulnerabilities with `govulncheck`; the HTML parser uses `golang.org/x/net v0.55.0` and WebP uses `golang.org/x/image v0.45.0`. The broader Core scan also reports HTTP/2 and HTTP/3 server dependency warnings. Core currently serves a standard `net/http.Server` over TCP and does not activate Gin H2C or QUIC servers; those server paths are outside the enabled protocol configuration. Enabling them requires a separate dependency/toolchain upgrade (the HTTP/2 fixes in x/net v0.60.0 require Go 1.26).
- Remaining acceptance: actual provider MIME and iOS reading/image-permission flows require the real-mail steps above. The owner authorized publishing rc10 as the deployable integration candidate before completing this acceptance.

### Selective-read follow-up

- Business review: changed the 1 MiB boundary from the whole message to the combined selected text, HTML and referenced CID payloads. Verified skipped attachments/alternatives, related multipart root selection, exact-budget success, one-byte overflow, preserved unread flags and cancellation during part fetching. Invalid CID encodings still preserve readable content. Remote image references remain local data throughout parsing and response serialization.
- Design review: the IMAP adapter owns the current UID and shared wire-byte budget; the content package owns MIME selection, decoding and sanitization. Shared MIME metadata parsing avoids duplicated rules. CSS declarations are validated before resolving CID resources, so discarded declarations do not trigger image reads. Both review rounds have no known blocking findings.
- Validation: full Core race tests and both production/test-tag vet passed. Focused race tests passed after the final MIME metadata and charset changes. Production build and isolated startup/health check passed. The follow-up 20-second fuzz run completed 20,861 executions without a failure; scoped Gitleaks and whitespace checks passed.

### rc10 release review

- Business and design reviews found no blocking issue in this change. Full Core race tests, both vet variants, formatting and scoped secret scans passed. An isolated release snapshot excludes unrelated console edits.
- The complete candidate build includes the Web console and passed startup, health and asset checks. Frontend static checks and all 137 Web tests passed.
- Real mailbox-to-iOS acceptance remains pending. The rc10 prerelease provides the versioned binaries and container needed to run that acceptance.
- Container release verification found the content license file excluded by `.dockerignore`. The build-context allowlist now includes that file. A complete container build, isolated health/Web-console smoke test and in-container license check passed after correction. The incomplete rc10 publication is rebuilt from the packaging-fix commit so all artifacts share the release source revision.
