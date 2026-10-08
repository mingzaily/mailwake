# Native push and App integration

[简体中文](native-push.zh-CN.md) · [Documentation](README.md)

## Mailwake App notifications

Core uses the official production Relay (`https://notify.mailwake.oritx.com`) when `MAILWAKE_RELAY_URL` is unset or empty. Set the variable to `https://notify-sandbox.mailwake.oritx.com` for sandbox testing or your own HTTPS Relay origin (loopback HTTP is supported for local development). Open Notifications → Phones. Create a five-minute QR code and compare the Core fingerprint on your phone. The QR code is a one-time credential: scan it only with your own phone. The iOS App implements pairing and notification-extension decryption. Production APNs delivery and device display have separate acceptance results.

The management API exposes `POST /api/v1/native/pairings` (201, including the only management response containing the pairing URI), `GET /api/v1/native/pairings/:id`, `GET /api/v1/native/devices` and `DELETE /api/v1/native/devices/:pairing_id`. These use the existing administrator authentication and CSRF protection. At most three pairings may wait at once; active and waiting pairings, plus selected devices awaiting revocation cleanup, reserve a total of sixteen device slots. Dates use UTC RFC3339; the URI's `exp` and signed timestamps use integer Unix seconds.

Core verifies Relay audience and environment at startup and before pairing, and binds them with the configured origin in encrypted configuration. The Core signing identity survives restart. Pairing tokens stay encrypted locally for at most the pairing lifetime; Core locks the verified device identity and both public keys before revealing the token to Relay in the final pairing request. Completed, failed and expired pairings clear the temporary token. Relay device names are display-only. Removing a phone revokes it locally first, so no further notifications are sent, then deletes the pairing at Relay; failed calls are retried with backoff until Relay confirms or reports the pairing gone. A waiting pairing that never selected a device is simply deleted.

Switching from sandbox to production requires pairing your phones again. When `native_environment_mismatch` appears after changing the Relay address, audience or environment:

1. Stop Core and keep the same `MAILWAKE_DATA_DIR` (including `secret.key`).
2. Run `./bin/mailwake admin reset-native-push` in an interactive terminal and type `reset-native-push` to confirm. For Docker, use `docker compose stop core`, then `docker compose run --rm -it core admin reset-native-push`.
3. Wait for the command to confirm zero unresolved targets, then set `MAILWAKE_RELAY_URL` to the new Relay, start Core and pair your phones again.

The command acquires the data-directory lock and deletes every paired device at the saved old Relay. If the old Relay is unreachable, pairings and the old binding are kept; run the command again once it is reachable. When all pairings are gone, one transaction clears the Relay binding, local pairings and pending native work. The Core signing key and fingerprint and ordinary-channel history are kept.

Before creating a pairing code, Core compares its clock with the HTTP `Date` from Relay `/v1/info`. A difference above 120 seconds returns `clock_skew` with the approximate difference and NTP guidance. Request timestamps use the measured offset within this tolerance; the signature format stays the same.

Select **Mailwake App** as the notification channel. Native events always include sender and subject, encrypted separately for each paired phone. The third-party preview preference is preserved. Notification plaintext contains version, optional stable mailbox_id, account label, folder, sender, subject and received time. Native notifications also include `code` when a verification code is detected. Only Native reads message content, using `BODY.PEEK[]` limited to the first 256 KiB; truncated messages or MIME parsing failures leave the code empty. Bodies stay in memory during detection and are excluded from storage, logs and notifications. Core truncates subject, then sender, to fit the 2032-byte JSON limit while preserving the code.

Core stores each device's envelope before sending and reuses it on retry or restart. Once a native event starts, its device plan and channel stay fixed through retries. Core `accepted` means every target was accepted by Relay; per-phone records show APNs outcomes after checks scheduled for 10 and 60 seconds after Relay acceptance. These schedules survive restart and overdue checks run when Core resumes. A phone's `accepted` still does not prove display. When Relay returns `pairing_revoked` on submission, or reports a cancelled delivery with that reason, Core marks the device as unpaired with an observation time and cancels its unsent tasks. The Phones list retains the device until an administrator removes it. With no active phones, new mailbox events advance the mailbox checkpoint without entering the native outbox; the overview shows `native_no_devices`. Explicit test sends still report that error.

Native terminal records clear delivery plaintext, encrypted envelopes and signatures; the administrator history retains the limited summary described above. Native failed events therefore support diagnosis rather than manual replay. Third-party manual retry remains available. Device status records are removed with their parent outbox record. The Relay's production APNs credentials and iOS device display require separate acceptance; the local end-to-end harness uses an explicit APNs HTTP test double.

The [Core integration guide](../internal/appmanagement/README.md) defines the interfaces with Relay and the App. Core tests use public synthetic vectors in `internal/native/testdata/test-vectors.json` and `internal/appmanagement/testdata/`.
## Read-only message content and verification-code activities

Native notifications carry a Core-signed message reference containing the Core, mailbox, folder, UIDVALIDITY and UID. The owner grants `content` separately for all currently subscribed folders on this Core. Configuration scopes and standalone native pairing retain their own boundaries.

The App calls `POST /api/v1/app/content` over the confirmed HTTPS address with a device-bound controller credential and a short-lived NativePush qualification. Core uses a non-watcher connection from the mailbox budget, reads the MIME structure, and fetches the selected text/plain or HTML text part through BODY.PEEK. The operation has a 20-second deadline, a 1 MiB raw-message limit and a 64 KiB readable-text limit; responses use no-store. Bodies are parsed in memory and excluded from the outbox, history and diagnostics.

Verification-code events create independent encrypted notification and activity plans in one transaction. Activity failure leaves ordinary notification state unchanged; activities expire 600 seconds after the original received_at. See [App management](../internal/appmanagement/README.md) for authorization and `internal/appmanagement/testdata/` for public signature vectors.
