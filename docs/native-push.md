# Native push and App integration

[Contributor documentation](README.md) · [User guides](https://mailwake.oritx.com/en/docs/)

## Mailwake App notifications

Core uses the official production Relay (`https://notify.mailwake.oritx.com`) when `MAILWAKE_RELAY_URL` is unset or empty. Set the variable to `https://notify-sandbox.mailwake.oritx.com` for sandbox testing or your own HTTPS Relay origin (loopback HTTP is supported for local development). The Web App authorization surface creates a five-minute, single-use pairing credential. The App confirms the Core fingerprint before acceptance. User steps live in the [connection guide](https://mailwake.oritx.com/en/docs/connect-app/). The iOS App implements pairing and notification-extension decryption. Production APNs delivery and device display have separate acceptance results.

The management API exposes `POST /api/v1/native/pairings` (201, including the only management response containing the pairing URI), `GET /api/v1/native/pairings/:id`, `GET /api/v1/native/devices` and `DELETE /api/v1/native/devices/:pairing_id`. These use the existing administrator authentication and CSRF protection. At most three pairings may wait at once; active and waiting pairings, plus selected devices awaiting revocation cleanup, reserve a total of sixteen device slots. Dates use UTC RFC3339; the URI's `exp` and signed timestamps use integer Unix seconds.

Core verifies Relay audience and environment at startup and before pairing, and binds them with the configured origin in encrypted configuration. The Core signing identity survives restart. Pairing tokens stay encrypted locally for at most the pairing lifetime; Core locks the verified device identity and both public keys before revealing the token to Relay in the final pairing request. Completed, failed and expired pairings clear the temporary token. Relay device names are display-only. Removing a phone revokes it locally first, so no further notifications are sent, then deletes the pairing at Relay; failed calls are retried with backoff until Relay confirms or reports the pairing gone. A waiting pairing that never selected a device is simply deleted.

Switching from sandbox to production requires pairing your phones again. When `native_environment_mismatch` appears after changing the Relay address, audience or environment:

1. Stop Core and keep the same `MAILWAKE_DATA_DIR` (including `secret.key`).
2. Run `./bin/mailwake admin reset-native-push` in an interactive terminal and type `reset-native-push` to confirm. For Docker, use `docker compose stop core`, then `docker compose run --rm -it core admin reset-native-push`.
3. Wait for the command to confirm zero unresolved targets, then set `MAILWAKE_RELAY_URL` to the new Relay, start Core and pair your phones again.

The command acquires the data-directory lock and deletes every paired device at the saved old Relay. If the old Relay is unreachable, pairings and the old binding are kept; run the command again once it is reachable. When all pairings are gone, one transaction clears the Relay binding, local pairings and pending native work. The Core signing key and fingerprint and ordinary-channel history are kept.

Before creating a pairing code, Core compares its clock with the HTTP `Date` from Relay `/v1/info`. A difference above 120 seconds returns `clock_skew` with the approximate difference and NTP guidance. Request timestamps use the measured offset within this tolerance; the signature format stays the same.

Select **Mailwake App** as the notification channel. Native events always include sender and subject, encrypted for the active phone selected by `native_pairing_id`. New notifications and tests use this target; retries retain the original device plan. The third-party preview preference is preserved. Notification plaintext contains version, optional stable mailbox_id, account label, folder, sender, optional sender_name, subject and received time. `sender` is the first From mailbox address; `sender_name` is its MIME-decoded display name and is omitted when empty. Both ordinary notifications and verification-code activities carry this field. Names come from the message header; domain inference and provider contacts stay outside this path. Native notifications also include `code` when a verification code is detected. Only Native reads message content, using `BODY.PEEK[]` limited to the first 256 KiB; truncated messages or MIME parsing failures leave the code empty. Bodies stay in memory during detection and are excluded from storage, logs and notifications. Core truncates subject, then sender_name, at Unicode boundaries to fit the 2032-byte JSON limit while preserving the complete sender address, code and signed reference. Oversized fixed fields produce `native_payload_too_large`.

Core stores each device's envelope before sending and reuses it on retry or restart. Once a native event starts, its device plan and channel stay fixed through retries. Core `accepted` means every target was accepted by Relay; per-phone records show APNs outcomes after checks scheduled for 10 and 60 seconds after Relay acceptance. These schedules survive restart and overdue checks run when Core resumes. A phone's `accepted` still does not prove display. When Relay returns `pairing_revoked` on submission, or reports a cancelled delivery with that reason, Core marks the device as unpaired with an observation time and cancels its unsent tasks. The Phones list retains the device until an administrator removes it. Revoking the selected native target resets the channel to unconfigured and pauses delivery until the administrator chooses a target again.

Native terminal records clear delivery plaintext, encrypted envelopes and signatures; the administrator history retains the limited summary defined in [Storage](storage.md#outbox-payloads-and-history). Native failed events therefore support diagnosis rather than manual replay. Third-party manual retry remains available. Device status records are removed with their parent outbox record. The Relay's production APNs credentials and iOS device display require separate acceptance; the local end-to-end harness uses an explicit APNs HTTP test double.

The [Core integration guide](../internal/appmanagement/README.md) defines the interfaces with Relay and the App. Core tests use public synthetic vectors in `internal/native/testdata/test-vectors.json` and `internal/appmanagement/testdata/`.
## Read-only message content and verification-code activities

Native notifications carry a Core-signed message reference containing the Core, mailbox, folder, UIDVALIDITY and UID. The owner grants `content` separately for all currently subscribed folders on this Core. Configuration scopes and standalone native pairing retain their own boundaries.

The App calls `POST /api/v1/app/content` over the confirmed HTTPS address with its device-bound controller credential and explicit owner content permission. Reading is free and uses a non-watcher connection from the mailbox budget. Core returns bounded plain text and optional sanitized HTML; the App controls remote image loading. [HTML content](html-content.md) owns MIME selection, read/response limits, error semantics and real-mail acceptance steps. Bodies and images stay in request memory, outside persistent history and diagnostics.

Verification-code events create independent encrypted notification and activity plans in one transaction. Activity failure leaves ordinary notification state unchanged; activities expire 600 seconds after the original received_at. See [App management](../internal/appmanagement/README.md) for authorization and `internal/appmanagement/testdata/` for public signature vectors.

## Pairing revocation synchronization

Core independently checks established pairings with signed `GET /v1/pairings/:id`. It reads at most 16 local active pairings, checks one per 5-second tick and checks each pairing at most once per minute, oldest due first. With fast successful responses, a round takes about 60–80 seconds; the foreground console refreshes within another 5 seconds. An untouched notification form follows the refreshed settings. A form with edits preserves its draft and shows a conflict; the user explicitly reloads to adopt the latest settings. Serial requests use the existing 15-second HTTP timeout, so network latency extends a round. Waiting invitations retain their separate 3-second poll.

Responses must match the pairing ID, device ID and encryption key. `200 status: revoked`, `404 not_found` and `410 pairing_revoked` confirm revocation. Timeouts, transport errors, 429, 5xx, authentication failures, malformed responses and unknown states preserve authorization and settings. Starting when response processing finishes, failures pause the established-pairing poller for 30, 60, 120, 240, 480 and then 900 seconds; a longer Retry-After takes precedence, capped at 24 hours. The per-pairing one-minute interval also applies. Success restores the normal schedule. This schedule lives in memory and restarts on the first 5-second tick; queued Web unlink retries retain their existing persisted schedule.

Web unlink, observed Relay revocation and explicit delivery revocation share one transaction: revoke the pairing, cancel unsent native notifications and clear their ciphertext, and finish unsent code activities. If the current channel is native and its target matches, the transaction also writes `channel: ""`, `native_pairing_id: ""`, the enqueue preview policy and the incremented configuration revision. Runtime policy follows the committed configuration. Other targets, third-party credentials and preferences, management authorization and history remain intact. Observed revoked pairings stay visible; Web unlink removes the pairing record after Relay confirms deletion.

Revocation and notification saves share a configuration lock and revision check. A new target saved first survives revocation of the old target. If revocation commits first, a stale save returns `409 settings_conflict`; read the new revision before saving again. Repeated observations preserve the revision and first revocation time. Restart restores the committed pairing and configuration together. An unconfigured channel pauses the queue under the existing delivery rules.

Manual acceptance: select a paired phone, note the settings revision, remove the outpost in iOS while no mail arrives, and verify the revoked device plus empty channel/target within roughly 80 seconds of healthy Relay service. Restart Core and verify the same API and Web state. Repeat while switching to another phone or Bark, with a temporary Relay outage, with Web unlink, and with an explicit delivery revocation. Outages preserve configuration until a definitive response arrives after backoff.

## Local identity ownership

`internal/application` supplies Native delivery with the shared `internal/coreidentity.Store`. App invitation creation and body-reference verification use that store directly and work with Relay disabled. Identity persistence retains the `native.identity` configuration name and authenticated-encryption context. Native reset retains this identity and the independent App management grants.

### Activity submission retry

Core reads at most 32 due activity submissions per delivery poll, ordered by the persisted next-attempt time and business identity. Transient transport errors, HTTP 429 and 5xx retry after 30, 60, 120, 240 and then 300 seconds, measured from response completion. A longer Relay `Retry-After` takes precedence. The retry deadline and failure count survive restart; retries reuse the original encrypted request and expiry. Relay metadata discovery follows the same delay when it fails transiently.

Expired submissions and failures whose next attempt would reach or exceed the activity expiry finish locally and clear their encrypted request. Permanent submission failures finish immediately. Shutdown cancellation leaves pending work available for recovery. Activity retries and terminal outcomes preserve ordinary notification delivery state.

## Sender fields for clients

```json
{
  "v": 1,
  "mailbox_id": "mbx_work",
  "account": "Work",
  "folder": "INBOX",
  "sender": "alerts@example.org",
  "sender_name": "通知中心",
  "subject": "Service update",
  "received_at": "2026-10-19T08:00:00Z"
}
```

For a From address without a name, the payload has `"sender": "alerts@example.org"` and omits `sender_name`. Clients decode the name as optional, retain both fields in local history, and use the address as the display fallback. A display name describes the sender; it provides no identity verification. The event and outbox retain the name under the same preview policy as the address. Third-party channel payloads retain their existing schema.

Reading actions use the separate [mailbox read endpoint](html-content.md#mailbox-read-action). Body downloads remain read-only; opening a locally cached body also requires that action to synchronize the mailbox flag.
