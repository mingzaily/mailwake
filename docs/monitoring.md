# Mailbox monitoring

[Contributor documentation](README.md) · [User guides](https://mailwake.oritx.com/en/docs/)

## Provider adapter behavior

Gmail Labels appear as IMAP folders. Core keeps selectable paths, including nested Labels and system folders beneath `[Gmail]` or `[GoogleMail]`. Discovery excludes entries marked `\Noselect` or `\NonExistent`; RFC 5258 defines `\NonExistent` as implying `\Noselect`. Parent grouping nodes remain outside the selectable results, while their selectable children remain available. The `\HasChildren` attribute alone allows selection. Each subscribed folder has its own UID cursor and event identity; one message appearing in two subscribed Labels can produce two folder notifications. New subscriptions establish a baseline, while reconnects resume their saved cursor and apply the existing 24-hour age limit.

NetEase mailboxes (163.com, 126.com and yeah.net) require an **authorization code** as the IMAP password. Core automatically sends the [RFC 2971 ID command](https://www.rfc-editor.org/rfc/rfc2971) when the server advertises ID support, after login and before folder discovery or opening. It sends only `name=Mailwake`, the build version, `vendor=Mailwake` and `support-url=https://github.com/mingzaily/mailwake`. A rejected ID command is logged at debug level and the connection continues; a timeout or broken transport follows the normal reconnect policy.

The connection reuses the post-login capability list for both ID and IDLE. With go-imap beta.8, a NOOP after login waits for the library to invalidate its previous capability cache before Core reads the automatic refresh. Capabilities included in the login response are reused directly; Core sends no additional CAPABILITY command after opening a folder.

A folder rejection with an IMAP response code uses `last_error.code=imap_folder_rejected` and `params.response_code`, such as `UNAVAILABLE` or `NONEXISTENT`. Core keeps only `A-Z`, `0-9`, `-` and `/`, up to 32 characters. The localized message and diagnostics show this sanitized code and omit raw server explanations. An absent or fully filtered code uses `imap_folder_open_failed` without a response-code parameter.

## Behavior

| Situation | Behavior |
| --- | --- |
| Subscribing to a folder | Uses UIDNEXT at the time the folder opens as the baseline and skips existing mail |
| New mail arrives | Reads UID, sender, subject and internal date and queues a notification; subjects and sender names in GBK, Big5, Shift_JIS and other legacy charsets are decoded |
| Mail received more than 24 hours before it appears in the folder | Advances progress without a notification, for example old mail filed by hand or a backlog after long downtime |
| Realtime Folder supports IDLE | Waits for change notifications and reconciles every 60 seconds; `mode=idle` |
| Realtime Folder without IDLE | Checks every 60 seconds; `mode=poll` |
| Scheduled Folder (`5m` / `15m`) | Checks immediately at startup or subscription change, then at its interval; `mode=scheduled` |
| Connection drop or command timeout | Reconnects with exponential backoff and resumes from the saved progress |
| Server rejects the credentials | Status shows "Authentication required"; retries every 30 minutes to avoid locking the account. Update that mailbox through its API |
| A connection has been open for 30 minutes | Reopens the folder and re-reads UIDVALIDITY; not reported as a failure |
| Restart | Catches up on mail that arrived during downtime, is still in the folder and was received within 24 hours |
| Unsubscribing | Stops that folder’s watcher; queued notifications are still delivered |
| Updating a mailbox | Connection changes test outside its lock, then replace only its watchers; other mailboxes keep running |
| Changing only the display name | Saves with the current revision, updates future notifications and preserves connections and progress |
| Changing only a Folder check method | Hands over the watcher and preserves progress; other realtime Folder connections stay open |
| Deleting a mailbox | Stops its watchers and deletes its configuration, subscriptions and progress; queued tasks continue |
| Subscribing again | Builds a new baseline and notifies only mail that arrives afterwards |
| UIDVALIDITY changes | Builds a new baseline, skips existing mail and shows a notice |
| Delivery fails | Ends the task and records the result by default |
| Automatic retry configured | Retries temporary failures such as network errors and HTTP 429/5xx |
| Manual retry | The management interface requeues a failed task for a new round |

FETCH responses are combined by UID through tagged command completion. FLAGS, envelope, internal date and body sections may arrive separately; BODY literals may precede UID. The maintained go-imap client preserves these responses with bounded pre-UID buffering, and the shared Core collector enforces section and batch read budgets. Monitoring requires an envelope and internal date for each searched UID. If either is missing, one bounded UID SEARCH checks only the affected UIDs: confirmed expunged messages are skipped; an existing message with incomplete metadata fails the entire batch with `imap_metadata_failed` and retains the checkpoint. An empty subject in a complete envelope remains valid. See [the maintained dependency record](../deps/go-imap/MAILWAKE.md).

Connecting times out after 15 seconds; login, capability discovery, ID, folder discovery, reconciliation and entering or leaving IDLE each time out after 30 seconds, which closes the connection and reconnects. Reconciliation sends NOOP so the server reports folder changes, then uses `UID SEARCH` for mail after the saved progress, at most 500 messages per metadata batch or 8 per Native code-detection batch. Native content is parsed one message at a time, and each successful batch advances the saved progress. Core never uses STATUS on the selected folder. Realtime backoff resets after a connection has run for a minute and completed a wait and a reconciliation. Scheduled backoff resets after a successful round.

All scheduled Folders in one mailbox share one connection per due round. Core processes the earliest `next_check` first (name order for ties), checks each due Folder in batches, then closes the connection. A round logs in at most once. Successful checks schedule their next run from completion time. Connection failures use exponential backoff; authentication failures pause the mailbox’s scheduled checks for 30 minutes. A rejected Folder that leaves the connection usable is retried separately while the other due Folders continue.

Each mailbox has its own subscriptions and revision, written to SQLite before its watchers change. Unchanged watchers keep their connection; removed watchers stop before replacements start. Saving a subscription clears the old progress of newly added folders in the same transaction. A successful save means the subscription is stored; check `/api/v1/status` for the live connection state. Unsubscribing from everything persists an empty list.

Delivery attempts, restart accounting and retry limits follow [Delivery](notifications.md#dispatch-and-retry-invariants).

Folder progress and queued notifications are saved in one SQLite transaction. The event namespace includes the stable mailbox ID and its identity (host, port, username), followed by folder, UIDVALIDITY and UID. Two mailboxes with the same folder and UID produce different events. Run one Core per data directory; changing a mailbox’s host, port or username clears only its progress and establishes a new baseline. Updating its password, display name or budget preserves progress. Other mailboxes keep their connections and progress. Old queued notifications remain deliverable.

When server rules file mail directly, subscribe to the destination folder. Deduplication is per folder: the same message in two subscribed folders produces two notifications. Mail that enters and leaves a folder between two checks may be missed.

## Privacy and data

Credential encryption, outbox summaries and retention follow [Storage](storage.md); payload preview rules follow [Delivery](notifications.md#privacy-and-persistence). Monitoring keeps message bodies out of persistent state and logs. Native verification-code extraction uses the bounded, in-memory reads in [Native integration](native-push.md).

## Folder presentation metadata

Discovery derives `folder_names` from each LIST item's actual hierarchy delimiter, including non-slash delimiters. NIL delimiters and empty leaves retain the full name. Names and roles are presentation metadata; subscriptions, SELECT, cursors and event identities use the original path.

Successful mailbox creation and connection updates save the directory's names and roles inside the encrypted mailbox configuration. Saved-mailbox scans refresh this data under the mailbox lock and the current configuration revision; metadata refresh leaves the owner's revision unchanged. A concurrent configuration change or deletion rejects the stale scan. Label-only saves preserve current metadata, connection changes replace it, and failed scans preserve the last successful result. Deletion removes it with the mailbox configuration. Discovery remains bounded by the existing 2,000 selectable-folder limit and performs no per-folder probes.
