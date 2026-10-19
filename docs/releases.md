# Release records

[Contributor documentation](README.md) · [Changelog](../CHANGELOG.md)

This file preserves dated evidence for the revisions and environments named below. Use [Branching workflow](branching.md) for current release policy and [Development](development.md) for current checks. Earlier results establish historical coverage; each candidate needs verification of its own changes.

## rc2 candidate — 2026-10-16

Scope: the changes since `v1.0.0-rc.1`, including complete IMAP FETCH assembly, enqueue-time preview privacy, credential target binding, console state handling and the maintained dependency layout. The implementation reviewed at `605ad32` is unchanged under `internal/` and `web/` in candidate `42b229c`; `88348b3` relocates the maintained module without changing its source. This release preparation adds the changelog, this record and dependency documentation to binary archives.

- Business review: **9.0/10** for the affected Core behavior. Reused the behavioral review of `605ad32` and rechecked shared FETCH budgets, checkpoint failure semantics, persisted preview policy and credential target boundaries. Fresh automated checks passed. Real-provider timing and actual device display remain candidate acceptance work.
- Code design review: **9.0/10**. Fetch assembly, settings validation and persisted privacy rules stay in their owning layers. The maintained client retains its MIT provenance, reproducible module replacement and regression coverage. Maintaining the source copy requires review when upgrading upstream; no new runtime abstraction is introduced for this release.
- Local verification: `make check`, `make test` (213 frontend tests, Core and maintained-module race tests) and `make check-build VERSION=v1.0.0-rc.2` passed. Unchanged Go packages reused test caches. An isolated temporary-data process passed startup, health, embedded-console and CSP-header checks. The binary-archive documentation layout passed relative-link checks after including the maintained dependency records.
- Publication verification: the tag workflow reruns CI before publishing four binary archives, SHA256 checksums and amd64/arm64 container images. Check the completed workflow and actual published artifacts against the final tagged revision; local checks alone do not establish publication success.

Code readiness meets the affected-change quality gate. This maintainer-authorized prerelease supplies a candidate for live acceptance; general release readiness still requires Gmail/QQ monitoring and body reads plus Gmail/Bark preview-transition checks. Verify correct new subjects, unavailable-title semantics for older queued records, continued watcher progress and preserved unread flags. Historical empty-string subjects cannot be reconstructed reliably. Earlier real-device results do not prove these new provider-specific cases.

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

## Sender names and mailbox read actions — 2026-10-19

Scope: Core changes on `dev/v1.0.0` based on `d6ec5538ca5bbb9ba4023e8e2060e8b541142111`, currently uncommitted. The changed `internal/` files (including new tests), sorted by path and hashed as path + NUL + contents + NUL, have SHA-256 `4262a29ba88e143fa2220e00e2c5bc77d42c64b5b94f3aee78449704c56735ac`.

- Business review: **9.0/10**. Signed message identity, current subscriptions, owner content permission, connection admission, timeout and provider failures were reviewed. Reading actions preserve other flags and watcher progress and support repeated requests. Sender names survive enqueue, SQLite reopening, signed encrypted notifications and activities; preview removal covers names. Real-provider flag propagation and uncommon encodings remain release checks.
- Code design review: **9.0/10**. Resource resolution and safe errors are shared; the IMAP adapter owns the write. Sender names use the existing optional JSON/event path. Tests cover each transfer boundary; HTTP callbacks pass captured requests to the test goroutine for decryption assertions. The expanded Source interface has explicit test substitutes.
- Verification: full Core `go test -tags nomsgpack,mailwake_test -race ./...`, both production and test-tag vet runs, and `make check-build` passed. After review additions, mail/IMAP, native, HTTP and localization race tests passed again. Unchanged packages may reuse Go test caches. Whitespace and changed-document relative-path checks passed.

Core code meets the development gate. Client integration, real mailboxes and device presentation remain pending; this record establishes no deployment or release. Client acceptance must cover names in history, opening downloaded/cached bodies, failed read synchronization and retry. See [sender fields](native-push.md#sender-fields-for-clients) and [mailbox read actions](html-content.md#mailbox-read-action).

### Follow-up review — 2026-10-19

The follow-up found a missed correctness condition in the preceding review: a server can acknowledge a session-only `\Seen` update, which expires when the independent connection closes. Core now checks permanent flag permission. The maintained parser preserves explicit empty permissions separately from the protocol's omitted-response default. Six protocol cases cover allowed, omitted, empty, other-only, keyword wildcard and case-insensitive flags; the three denied cases failed before the fix and pass afterward. The memory-server fixture now advertises the permanent system flags it supports.

Business logic: **9.0/10** after the fix and repeated review, covering persistent read semantics, signed ownership, failure mapping, sender persistence, preview privacy and encrypted payloads. The earlier conclusion missed this condition and is superseded by this review. Live-provider propagation remains pending. Code design: **9.0/10**; protocol parsing owns the empty/omitted distinction and the IMAP adapter owns read permission. Maintaining the small upstream parser change remains a low-impact upgrade obligation. No known blocking code findings remain.

Source: base `d6ec5538ca5bbb9ba4023e8e2060e8b541142111`, uncommitted `dev/v1.0.0`. SHA-256 over sorted changed/new Go files in `internal/` and `deps/`, using `path + NUL + content + NUL`: `b58ee54cb54a19497f4bdf5dde1a7731c0d6cba8f41cfe8f6b822fcbc72447c7`. Core and maintained-module full race tests passed, as did both Core vet variants, dependency vet with its existing composites exception plus full client/internal vet, and the production Go build. The earlier frontend/build evidence applies to unchanged frontend and build configuration only. Code is ready; client integration and real-provider release acceptance remain pending.

### Payload and session cleanup — 2026-10-19

Ordinary notifications and activities now share one plaintext constructor. The internal folder selector returns the concrete session type; its public adapter preserves a nil session on failure. HTTP routes, JSON fields, encryption and error contracts retain their previous behavior.

Business review: **9.0/10**. Eight payload cases cover long names, long subjects, their combination and oversized fixed sender fields across ordinary notifications and activities. Tests check the complete serialized size, Unicode/JSON escaping, truncation order, unchanged addresses and metadata, and signed message references. Activity tests also verify the envelope signature and decrypt the planned notification. Fixed-field overflow returns the exact non-retryable `native_payload_too_large` failure. Design review: **9.0/10**. Shared construction removes duplicate field mapping, and the concrete selector eliminates a runtime type assertion. Review preserved the public nil-on-error contract with an explicit adapter and regression test. No blocking findings remain; the maintained parser's upstream upgrade obligation remains unchanged.

The baseline and branch remain unchanged. The changed/new Go source fingerprint, using the preceding algorithm, is `8a02950a7363afa2eb584f01598b6d94346751987ad8496203e9a5a3609f3ec2`. Native, mail/IMAP and runtime race tests, both Core vet configurations, the production Go build and whitespace checks passed. The preceding full-suite result at fingerprint `b58ee54cb54a19497f4bdf5dde1a7731c0d6cba8f41cfe8f6b822fcbc72447c7` applies to unchanged packages and the maintained dependency. Core code is ready and uncommitted; client integration and live-provider release acceptance retain their earlier scope.
