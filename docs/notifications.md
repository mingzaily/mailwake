# Notifications

[简体中文](notifications.zh-CN.md) · [Documentation](README.md)

## Channel configuration

Configure the global channel through `/api/v1/settings/delivery`, sending its current `revision`. Channel, preview, retry count and notification language apply to all mailboxes. All credentials are encrypted and write-only; omitted credential fields keep their saved values.

| Channel | Parameters | Credentials |
| --- | --- | --- |
| `bark` | `bark.endpoint`, default `https://api.day.app` | `bark.key` |
| `pushover` | none | `pushover.token`, `pushover.user` |
| `webhook` | HTTPS, query allowed | `webhook.url` and `webhook.secret` (at least 32 non-whitespace characters) |
| `native` | Official production Relay by default; override with `MAILWAKE_RELAY_URL`; pair phones in Notifications | Locally generated encrypted Core identity |

Webhook URLs are write-only because paths and queries may contain routing credentials. `preview` accepts `off` or `subject`; `retry_count` accepts 0–9 and defaults to 0; `language` accepts `en` or `zh-CN` and defaults to `en`. Bark and Pushover show only the subject with `subject`; Webhook retains structured sender and subject, while its `message` also contains only the subject. `off` (and an empty subject) uses “New mail” / “收到新邮件”. Titles stay `{display name} · {folder}`; Bark groups by that same truncated title. Test notification text is unchanged. API input `sender_subject` returns 400 `config_preview_invalid`. For third-party channels, switching from `subject` to `off` atomically removes sender and subject from all pending and failed records; later enqueues also follow the saved privacy policy. Native push always records sender and subject while preserving the third-party preview preference.

Bark history retention follows the Bark app's save-history setting for both mail and test notifications.

### Webhook payload

Core sends a `POST` with a JSON body and never follows redirects:

```json
{
  "version": 1,
  "id": "3f9c…",
  "test": false,
  "mailbox_id": "mbx_example",
  "account": "Work mailbox",
  "folder": "Clients",
  "sender": "anna@example.org",
  "subject": "Re: Q4 proposal",
  "received_at": "2026-09-28T10:32:00Z",
  "title": "Work mailbox · Clients",
  "message": "Re: Q4 proposal"
}
```

`mailbox_id` is the stable mailbox ID; `account` is its display name at enqueue time. Bark, Pushover and Webhook ordinary notifications use the title `{display name} · {folder}` in both languages. Renaming or deleting a mailbox preserves the identity metadata of already queued tasks. Test notifications use separate text and omit mailbox fields.

`sender` and `subject` appear only with the `subject` preview. `title` and `message` are localized and ready to forward to ntfy, Gotify and similar services. The display name and folder are sent even with preview off; choose a label suitable for sharing with your notification provider.

| Header | Meaning |
| --- | --- |
| `X-Mailwake-Event` | Event ID, equal to `id`; unchanged across retries, use it to deduplicate |
| `X-Mailwake-Timestamp` | Send time in Unix seconds |
| `X-Mailwake-Signature` | `sha256=` followed by the hex `HMAC-SHA256(secret, "<timestamp>.<raw body>")` |

Verify the signature with the same secret using a constant-time comparison, and reject timestamps more than five minutes away:

```python
import hashlib, hmac, time

def verify(secret: bytes, timestamp: str, body: bytes, signature: str) -> bool:
    expected = "sha256=" + hmac.new(secret, timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
    return abs(time.time() - int(timestamp)) <= 300 and hmac.compare_digest(expected, signature)
```

Any 2xx response counts as accepted. 429 and 5xx follow the retry setting; other statuses end the attempt and record a failure.

Saving notification settings validates fields, addresses, pairing state and revision without sending a test. Use the explicit test button after saving. Native tests use Relay’s fixed free test endpoint and share the App test quota; ordinary email notifications require Native Push entitlement.
