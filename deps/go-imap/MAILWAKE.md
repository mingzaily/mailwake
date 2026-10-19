# Mailwake maintained go-imap source

This directory copies `github.com/emersion/go-imap/v2 v2.0.0-beta.8` from its verified Go module distribution. The upstream MIT license and source files remain included. The Core `go.mod` replacement makes builds use this source reproducibly; no module-cache edits or build-time patching are involved.

Production changes are confined to `imapclient/client.go` and `imapclient/fetch.go`. The upstream `imapclient/fetch_test.go` also accepts the separate flag update produced by a non-PEEK read while verifying the exact body:

- Deliver every matching FETCH response until the tagged command completes. Flags and static metadata can arrive separately for the same UID. Callers combine the responses by UID.
- Preserve an explicit empty `PERMANENTFLAGS` response as an empty slice, distinct from an omitted response (`nil`). RFC 3501 section 6.3.1 gives omitted responses the default of allowing permanent flag changes; Core uses this distinction when marking mail read.
- Keep a literal's terminal read result after EOF or transport failure. Subsequent cleanup returns that result while the decoder resumes, preserving exclusive access to the shared transport reader during cancellation.
- Buffer body literals that precede UID until the decoder can route the message to the correct pending command. This also preserves routing for unsolicited or unrelated UIDs. After UID, literals continue streaming.
- `Options.MaxBufferedFetchBytes` bounds total pre-UID body bytes per response (default 16 MiB). At most 32 data items can precede UID. Exceeding either limit terminates the connection, allowing the caller to preserve progress. Core sets the byte limit to its MIME read budget plus one byte and independently enforces requested section and batch budgets.

The original streaming API and its requirement to consume each response still apply. `FetchCommand.Collect` may return multiple entries per UID; Core's shared collector assembles metadata and requested body sections before using them.

Run `make test` and `make check` from the Core root. These include this module's upstream tests and vet; Core's protocol fixtures cover repeated FLAGS, split metadata, BODY before UID, unrelated UIDs, partial literals, size limits, cancellation and expunged messages. Review any future upstream replacement against these regressions before removing this source copy.

The unchanged upstream server fixtures use positional literals for pinned go-message header structs. The full upstream vet run disables only the `composites` analyzer for these existing literals; the changed client and internal packages also receive a separate full vet run. Core itself retains both full vet configurations.
