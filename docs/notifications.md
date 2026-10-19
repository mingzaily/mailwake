# Delivery implementation

[Contributor index](README.md) · [User notification guide and webhook contract](https://mailwake.oritx.com/en/docs/notifications/)

## Ownership

`internal/delivery/` owns request attempts, retry policy, delivery states, notification rendering and shared HTTP rules. The Bark, Pushover and Webhook subpackages implement their adapters. `internal/runtime/` assembles channels; `internal/storage/` persists the outbox. Native delivery has its own [encrypted device plans](native-push.md).

## Dispatch and retry invariants

Persist an event and watcher progress transactionally before dispatch. Treat a successful channel response, including any provider-level result, as channel acceptance; device display remains a separate outcome.

`delivery.retry_count` defaults to **0**: each task is requested at most once. **2** means the first request plus up to two automatic retries. Allowed values are 0–9. Retry transient network failures, HTTP 429 and 5xx within this allowance. Backoff starts around 5 seconds and caps at 30 minutes; a longer `Retry-After` is honored up to 24 hours, beyond which the task ends as failed. Waits are persisted. Permanent failures such as rejected credentials end immediately; fix the configuration and retry manually. Tasks that have yet to start use the current channel; started native tasks retain the native channel.

The attempt count is saved before each request. By default an interrupted process ends a request that had started instead of sending it again; if the process exits between saving the count and sending, that request may not have been sent. Mailwake treats the request attempt as the delivery boundary and records the channel's response; display on the device is the channel's responsibility.

Webhook requests preserve event identity across retries, sign the timestamp and exact raw body, and follow the published header contract. Keep redirect following disabled. Tests use local HTTPS endpoints and synthetic credentials; check observable requests and outcomes.

## Channel configuration

Configuration updates use revisions. Channel, preview, language and retry settings apply to all mailboxes. Validate credentials and destinations at the configuration boundary, encrypt saved secrets, and preserve omitted write-only values only for the same credential target ([binding rules](api.md#credential-target-binding)). Saving validates configuration; only the explicit test operation sends a test notification.

## Privacy and persistence

Third-party previews default to `off`; `subject` permits the provider payload described in the public guide. Switching a third-party preview from `subject` to `off` atomically clears sender, sender_name and subject from pending and failed records. The enqueue transaction applies the saved policy to both payloads and history; an absent policy means `off`. It also removes verification codes when previews are off. Watchers pass transient metadata to this transaction rather than keeping a separate preview switch. Native delivery always includes encrypted sender and subject and preserves the third-party preview preference.

Before a channel is configured, detected mail stays queued with the default preview policy. Configuring a channel can deliver these queued events, while enabling previews applies to future enqueues. Previously omitted subjects remain unavailable: history uses `subject: null` (title not retained), while an actual empty subject uses `subject: ""`. Core does not fetch old subjects to populate queued events.

Mailbox and folder labels are part of notification metadata. Message bodies remain outside outbox persistence, history and logs. Renaming or deleting a mailbox preserves identity metadata already captured for queued work. Terminal payload clearing and history retention follow [Storage](storage.md); native terminal records additionally clear device envelopes and signatures.

Channel changes require focused adapter, dispatcher, storage and privacy tests for affected behavior. Keep user configuration steps and webhook examples on the documentation site.

Bark credentials stay bound to their saved service address. Tests and saves that change the address require an explicitly supplied device key, including edits while another channel is selected. See [credential target binding](api.md#credential-target-binding).
