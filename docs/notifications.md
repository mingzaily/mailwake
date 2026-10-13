# Delivery implementation

[Contributor index](README.md) · [User notification guide and webhook contract](https://mailwake.oritx.com/en/docs/notifications/)

## Ownership

`internal/delivery/` owns request attempts, retry policy, delivery states, notification rendering and shared HTTP rules. The Bark, Pushover and Webhook subpackages implement their adapters. `internal/runtime/` assembles channels; `internal/storage/` persists the outbox. Native delivery has its own [encrypted device plans](native-push.md).

## Dispatch and retry invariants

Persist an event and watcher progress transactionally before dispatch. Record attempts before network I/O so restart preserves retry accounting. One initial attempt is the default; `retry_count` explicitly permits 0–9 additional attempts. Retry transient network failures, HTTP 429 and 5xx under that policy. Treat a 2xx response as channel acceptance. Device display remains a separate outcome.

Webhook requests preserve event identity across retries, sign the timestamp and exact raw body, and follow the published header contract. Keep redirect following disabled. Tests use local HTTPS endpoints and synthetic credentials; check observable requests and outcomes.

## Channel configuration

Configuration updates use revisions. Channel, preview, language and retry settings apply to all mailboxes. Validate credentials and destinations at the configuration boundary, encrypt saved secrets, and preserve omitted write-only values. Saving validates configuration; only the explicit test operation sends a test notification.

## Privacy and persistence

Third-party previews default to `off`; `subject` permits the provider payload described in the public guide. Switching a third-party preview from `subject` to `off` atomically clears sender and subject from pending and failed records. Future enqueues use the saved policy. Native delivery always includes encrypted sender and subject and preserves the third-party preview preference.

Mailbox and folder labels are part of notification metadata. Message bodies remain outside outbox persistence, history and logs. Renaming or deleting a mailbox preserves identity metadata already captured for queued work. Terminal payload clearing and history retention follow [Storage](storage.md); native terminal records additionally clear device envelopes and signatures.

Channel changes require focused adapter, dispatcher, storage and privacy tests for affected behavior. Keep user configuration steps and webhook examples on the documentation site.
