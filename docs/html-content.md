# HTML message content

`POST /api/v1/app/content` accepts the existing `{ "reference": { ... } }` signed message reference. It requires the device's `mwac_` credential and the owner's `content` scope for a currently subscribed folder. Reading is free. Core uses local authorization and IMAP; it makes no Platform or Pro request.

```json
{
  "text": "Plain-text fallback",
  "html": "<p>HTML body</p>",
  "has_remote_images": false,
  "truncated": false
}
```

`text` and `truncated` are required. `html` is optional (clients also accept null). HTML responses always include `has_remote_images`; Core emits false when HTML is omitted. The HTTP response uses `Cache-Control: no-store`.

## MIME and privacy

Core reads `BODYSTRUCTURE` on a short-lived, read-only IMAP connection, selects the display representations, and fetches their transfer-encoded bytes with `BODY.PEEK[part]`. CID images are fetched from the current message only when the sanitized presentation references them. Ordinary attachments, unused alternatives and unreferenced images are skipped. The total mailbox message may be larger than 1 MiB. The mailbox's unread flags and monitoring cursor stay unchanged. MIME transfer encodings and character sets are decoded. `multipart/alternative` selects the last supported plain and HTML representations. A `multipart/related` presentation uses its `start` root or first part; its CID namespace belongs to that presentation. Named attachments and forwarded messages stay outside the displayed body. HTML-only messages receive a readable text fallback with paragraphs, lists and safe links.

The HTML allowlist keeps common formatting, tables, safe inline CSS and absolute HTTP(S)/mailto links. Scripts, event handlers, forms, embedded documents, SVG/MathML, redirects, style blocks, external styles/fonts, audio/video and prefetch hints are removed. Unknown CSS functions, CSS variables and escaped CSS values are discarded. This deliberately supports a passive subset of email layout; newsletters relying on style blocks may render more simply.

Safe absolute HTTP(S) `img` sources, `img`/`picture` source sets and inline CSS backgrounds remain in HTML. URLs with credentials, relative URLs and other schemes are removed. `has_remote_images` reflects retained image references, including source sets and CSS backgrounds; ordinary hyperlinks do not set it. Core performs no image fetch, DNS probe, proxying or filesystem lookup. The iOS reader blocks remote images until the user permits loading in that reading session or enables its global preference.

CID references resolve only within the current related MIME presentation. PNG, JPEG, GIF and WebP images are checked against their declared MIME, decoded for validation and embedded as data URLs. Duplicate/missing IDs, SVG, damaged images, invalid image transfer encodings, format mismatches and sender-supplied data URLs are removed. An invalid CID resource leaves the readable message available. Alternative text remains available. Resources from sibling presentations and previous requests are inaccessible. Body text, HTML, embedded images and image URLs remain in request memory; they are excluded from storage, notifications, logs and diagnostics.

## Limits and failures

| Boundary | Behavior |
| --- | --- |
| Total read deadline | 20 seconds, including connection, fetching and parsing; timeout returns `504 message_timeout` |
| Selected MIME payload | A shared 1 MiB budget for transfer-encoded plain text, HTML and referenced CID bytes (including base64/quoted-printable overhead); oversize returns `413 message_too_large`. Ordinary attachments do not consume this budget. |
| Text | 64 KiB, UTF-8-safe clipping sets `truncated: true` |
| HTML including data URLs | 256 KiB; oversize omits the entire HTML representation |
| Encoded JSON | 512 KiB, including JSON escaping; oversize omits HTML |
| CID image | At most 4096 pixels on either axis and 4,194,304 total pixels; invalid/oversize dimensions remove the reference |
| Embedded image bytes | A referenced image larger than 192 KiB cannot fit its base64 representation in the HTML budget and causes HTML fallback |
| MIME structure | Maximum depth 32 and 256 parts; invalid structure returns `422 message_content_invalid`. Multipart roots selected by a related `start` parameter may require a MIME-header read, bounded to 64 KiB and charged to the same shared read budget. |

HTML fallback returns bounded text, `has_remote_images: false` and `truncated: true`. HTML and data URLs are never byte-clipped. Bad MIME/character encodings return `422 message_content_invalid`. Existing scope (`403 content_permission_required`), revocation (`401`), resource/reference (`404 message_not_found`) and connection/budget (`503 message_unavailable`) behavior remains in force.

## Real-mail integration steps

Use Core v1.0.0-rc.10 and the iOS build with HTML reading. This prerelease supplies the deployable build for real-mail integration acceptance; rc9 returns the earlier plain-text response. Use synthetic mail content in a real test mailbox, and keep the mailbox credentials in Core's normal encrypted settings.

1. Configure a dedicated subscribed folder and authorize the iOS device with `content` scope. Keep the iOS **Automatically Load Remote Images** setting off. Obtain new message references from normal Native notifications; use the references exactly as received. The reading endpoint is free; notification delivery keeps its existing Relay entitlement requirement.
2. Send separate messages covering plain text; HTML only; plain/HTML alternatives; nested alternative/related with CID PNG/JPEG/GIF/WebP; and GBK or UTF-8 with base64/quoted-printable. Include tables, inline formatting, and a distinct plain fallback. Add SVG, missing CID and a named attachment as negative cases. Include HTML tags for a script, event handler, iframe and form in a synthetic message to verify removal.
3. Add uniquely named HTTP(S) image paths on a server you control: an `img`, two `srcset` candidates and a CSS background. Use a URL containing a unique per-message token. Keep server access logs visible, and disable any mailbox web preview or third-party scanner that would fetch those URLs during the measurement.
4. Before opening the notification, verify that the message is unread using your provider's flag view or an IMAP `UID FETCH <uid> (FLAGS)` command. Read it through the iOS notification detail. Confirm HTML layout, immediate CID display, blocked remote images and the image permission indicator. Image-server traffic attributable to Core and the blocked iOS session must remain zero. Inspect the response locally if needed: required fields, correct remote flag, `no-store` and total response size. Keep inspection out of shared logs and delete temporary captures afterward.
5. Tap **Load Images**. Confirm that requests now arrive from the iOS reading session for retained images. Navigate away and reopen; the per-message permission resets. Repeat with global automatic loading enabled, then restore the preferred setting. Recheck IMAP flags: the message remains unread throughout.
6. Send an HTML body over 256 KiB with selected payloads within 1 MiB, then a message whose escaped JSON exceeds 512 KiB. Verify a readable text response with omitted HTML and `truncated: true`. Attach a multi-megabyte PDF to a small body and confirm that reading succeeds. Add a large unreferenced image or unused alternative and confirm the same behavior. Send selected body/CID payloads whose combined transfer-encoded bytes exceed 1 MiB and verify `413 message_too_large`. Revoke `content` permission or the device credential and verify `403` or `401` respectively.
7. Check Core delivery history, diagnostics and logs for the synthetic body/CID/image URL markers: they must be absent. Delete the test messages and any local response captures when finished.

## Automated verification

The content tests cover representations, encodings, sanitization, image flags, CID formats/isolation, budgets and cancellation. Local TLS IMAP tests exercise selective MIME part fetching, multi-megabyte attachments, skipped unused alternatives/CIDs, exact and cumulative read budgets, absence of image requests, unread flags, missing messages, invalid UIDVALIDITY, moves/deletion, oversize and cancellation. HTTP tests use real owner/device authorization plus the parser: an observable image server and HTTP/DNS traps record zero outbound image operations through response serialization, and an escaping-heavy response verifies the 512 KiB limit. Runtime and native tests exercise signed reference/account boundaries.

These fixtures validate Core behavior. Real-provider MIME differences and the Core-to-iOS rendering flow require the manual steps above.

The additional parser and image decoder licenses are bundled in [third-party notices](html-content-third-party.txt).

## Review record — 2026-10-13

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
