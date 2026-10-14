# Release records

[Contributor documentation](README.md) · [Changelog](../CHANGELOG.md)

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
