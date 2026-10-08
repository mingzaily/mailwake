# 通知通道

[English](notifications.md) · [文档目录](README.zh-CN.md)

## 通道配置

通过 `/api/v1/settings/delivery` 携带当前 `revision` 配置全局通道。通道、预览、重试次数和通知语言对所有邮箱生效。凭据加密且只写；省略凭据字段表示保留原值。

| 通道 | 参数 | 凭据 |
| --- | --- | --- |
| `bark` | `bark.endpoint`，默认 `https://api.day.app` | `bark.key` |
| `native` | 默认使用官方生产 Relay；通过 `MAILWAKE_RELAY_URL` 覆盖；在通知设置中配对手机 | 本地生成并加密保存的 Core 身份 |
| `pushover` | 无 | `pushover.token`、`pushover.user` |
| `webhook` | HTTPS，可带查询参数 | `webhook.url`、`webhook.secret`（至少 32 个非空白字符） |

Webhook URL 的路径或查询参数可能包含凭据，因此整个 URL 只写。`preview` 仅支持 `off` 或 `subject`；`retry_count` 为 0–9，默认 0；`language` 为 `en` 或 `zh-CN`，默认英文。`subject` 下 Bark/Pushover 正文只显示主题；Webhook 保留结构化的 sender 与 subject，message 只显示主题。`off` 或主题为空时，正文为“收到新邮件”/“New mail”。标题保持 `{显示名称} · {文件夹}`，Bark group 使用同一个截断后的标题；测试通知文案保持原值。API 输入 `sender_subject` 返回 400 `config_preview_invalid`。第三方通道从 `subject` 切换为 `off` 时，同一事务清理所有待处理及失败记录的发件人与主题；后续入队也遵循已保存的预览策略。原生通道始终记录发件人与主题，并保留第三方预览偏好。

Bark 的邮件通知和测试通知均按 Bark App 的历史保存设置保留记录。

### Webhook 格式

Core 向配置的地址 `POST` JSON，不跟随重定向：

```json
{
  "version": 1,
  "id": "3f9c…",
  "test": false,
  "mailbox_id": "mbx_example",
  "account": "工作邮箱",
  "folder": "Clients",
  "sender": "anna@example.org",
  "subject": "Re: Q4 proposal",
  "received_at": "2026-09-28T10:32:00Z",
  "title": "工作邮箱 · Clients",
  "message": "Re: Q4 proposal"
}
```

`mailbox_id` 为稳定邮箱 ID，`account` 为入队时的显示名称。Bark、Pushover、Webhook 普通通知的中英文标题统一为“{显示名称} · {文件夹}”。邮箱改名或删除后，已入队任务保留原来的来源信息。测试通知使用独立文案并省略邮箱字段。

`sender`、`subject` 仅在 `subject` 预览下出现；`title`、`message` 是已本地化的展示文本，可直接转发给 ntfy、Gotify 等服务。关闭预览后仍会发送显示名称和文件夹，请使用适合提供给通知服务商的名称。

| 请求头 | 含义 |
| --- | --- |
| `X-Mailwake-Event` | 事件 ID，与正文 `id` 相同；重试时不变，可用于去重 |
| `X-Mailwake-Timestamp` | 发送时的 Unix 秒 |
| `X-Mailwake-Signature` | `sha256=` 加 `HMAC-SHA256(secret, "<timestamp>.<原始正文>")` 的十六进制 |

接收端用同一密钥计算签名并以常量时间比较，同时拒绝时间戳偏差超过 5 分钟的请求：

```python
import hashlib, hmac, time

def verify(secret: bytes, timestamp: str, body: bytes, signature: str) -> bool:
    expected = "sha256=" + hmac.new(secret, timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
    return abs(time.time() - int(timestamp)) <= 300 and hmac.compare_digest(expected, signature)
```

返回任意 2xx 视为接收成功；429 与 5xx 按重试配置处理，其他状态结束本轮并记录失败。
