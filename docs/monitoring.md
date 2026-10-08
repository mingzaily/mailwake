# Mailbox monitoring

[简体中文](monitoring.zh-CN.md) · [Documentation](README.md)

## Mailbox compatibility

Gmail accounts that permit IMAP and app passwords use the same mailbox fields as other IMAP accounts: `imap.gmail.com`, port `993`, the complete account address and an app password. Google requires two-step verification for app passwords, and some account policies prevent their use. Changing the Google account password revokes its app passwords; replace the saved IMAP credential to reconnect. See Google's [app-password requirements](https://support.google.com/accounts/answer/185833?hl=en).

Gmail Labels appear as IMAP folders. Core keeps selectable paths, including nested Labels and `[Gmail]` or `[GoogleMail]` system folders, and filters `\Noselect` entries. Each subscribed folder has its own UID cursor and event identity; one message appearing in two subscribed Labels can produce two folder notifications. New subscriptions establish a baseline, while reconnects resume their saved cursor and apply the existing 24-hour age limit.

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

Connecting times out after 15 seconds; login, capability discovery, ID, folder discovery, reconciliation and entering or leaving IDLE each time out after 30 seconds, which closes the connection and reconnects. Reconciliation sends NOOP so the server reports folder changes, then uses `UID SEARCH` for mail after the saved progress, at most 500 messages per metadata batch or 8 per Native code-detection batch. Native content is parsed one message at a time, and each successful batch advances the saved progress. Core never uses STATUS on the selected folder. Realtime backoff resets after a connection has run for a minute and completed a wait and a reconciliation. Scheduled backoff resets after a successful round.

All scheduled Folders in one mailbox share one connection per due round. Core processes the earliest `next_check` first (name order for ties), checks each due Folder in batches, then closes the connection. A round logs in at most once. Successful checks schedule their next run from completion time. Connection failures use exponential backoff; authentication failures pause the mailbox’s scheduled checks for 30 minutes. A rejected Folder that leaves the connection usable is retried separately while the other due Folders continue.

Each mailbox has its own subscriptions and revision, written to SQLite before its watchers change. Unchanged watchers keep their connection; removed watchers stop before replacements start. Saving a subscription clears the old progress of newly added folders in the same transaction. A successful save means the subscription is stored; check `/api/v1/status` for the live connection state. Unsubscribing from everything persists an empty list.

`delivery.retry_count` defaults to **0**: each task is requested at most once. **2** means the first request plus up to two automatic retries. Allowed values are 0–9. Backoff starts around 5 seconds and caps at 30 minutes; a longer `Retry-After` is honored up to 24 hours, beyond which the task ends as failed. Waits are persisted. Permanent failures such as rejected credentials end immediately; fix the configuration and retry manually. Tasks that have yet to start use the current channel; started native tasks retain the native channel.

The attempt count is saved before each request. By default an interrupted process ends a request that had started instead of sending it again; if the process exits between saving the count and sending, that request may not have been sent. Mailwake treats the request attempt as the delivery boundary and records the channel's response; display on the device is the channel's responsibility.

Folder progress and queued notifications are saved in one SQLite transaction. The event namespace includes the stable mailbox ID and its identity (host, port, username), followed by folder, UIDVALIDITY and UID. Two mailboxes with the same folder and UID produce different events. Run one Core per data directory; changing a mailbox’s host, port or username clears only its progress and establishes a new baseline. Updating its password, display name or budget preserves progress. Other mailboxes keep their connections and progress. Old queued notifications remain deliverable.

When server rules file mail directly, subscribe to the destination folder. Deduplication is per folder: the same message in two subscribed folders produces two notifications. Mail that enters and leaves a folder between two checks may be missed.
## Privacy and data

- Each mailbox is encrypted separately under `mailbox:<id>`, with the full configuration name bound into AES-GCM authentication. Mailbox and channel settings are encrypted using AES-256-GCM with a random 256-bit `secret.key` (0600) in the data directory. This protects a database file or dump leaked on its own. Anyone who can read the entire directory, including the key, can decrypt it. Protect the volume and backups together. A missing original key makes encrypted configuration unavailable.
- Administrator passwords use Argon2id with encoded parameters. Session IDs, setup codes and API tokens are stored as hashes. API token values appear only in their creation response; the setup code appears only in its startup log line. Credentials are omitted from settings responses, diagnostics and runtime logs.
- IMAP uses implicit TLS (usually port 993) with certificate verification. Every channel uses HTTPS, never follows redirects and never forwards credentials to another host.
- With `delivery.preview=off` (default), a task stores the mailbox ID, display name, message location, folder and time. Message bodies are always excluded.
- With `subject`, pending and failed tasks store sender and subject. Bark/Pushover bodies contain the truncated subject; Webhook additionally carries structured sender and subject. When you use a third-party channel, the notification passes through that service. Message bodies always stay in your mailbox.
- After the channel accepts a request its delivery payload is cleared. A separate history summary keeps mailbox ID, display name, folder and subject until the record expires; it excludes sender and body. The preview policy at enqueue time determines whether the summary records a subject (always enabled for the native channel). Third-party preview `off` omits subjects from new history entries; existing history keeps its recorded subjects. Accepted records are kept about 7 days and failed records 30 days for diagnosis and manual retry, pruned hourly in batches.
- Your deployment configures file permissions, volumes, disk encryption and backups; SQLite uses ordinary local storage.
