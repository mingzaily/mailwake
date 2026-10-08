# HTTP API 参考

[English](api.md) · [文档目录](README.zh-CN.md)

## 语言与错误码

英文是默认语言。唯一文案来源是 `internal/i18n/locales/en.json` 和 `zh-CN.json`，供 API、管理界面和通知共用，并嵌入二进制。

- 页面按 `Accept-Language` 选择语言，可切换 English / 简体中文；浏览器只保存语言偏好，不保存 Token。
- API 按 `Accept-Language` 权重协商语言，返回 `Content-Language` 和 `Vary: Accept-Language`，缺省回退 `en`。
- 通道设置的 `language`（`en` 或 `zh-CN`）决定通知语言。
- 日志和 CLI 使用英文。业务错误使用稳定的英文 `snake_case` 错误码与命名参数；存储只保存错误码和参数，展示时再翻译。

```json
{
  "error": {
    "code": "unauthorized",
    "message": "请使用有效会话或 API Token 登录。"
  }
}
```

投递记录的 `last_error` 与文件夹状态的 `notice` 使用相同的 `{code, message, params?}` 结构，例如 `bark_http_error` 搭配 `{"status":"503"}`。按 `code` 判断分支，`message` 用于展示。

每个邮箱的独立连接预算内预留 1 个名额给文件夹发现与连接测试。监听需求按“实时 Folder 数 +（存在定时 Folder 时的 1 个）”计算，最多使用 `connection_limit - 1` 个名额（默认 9 个）；该邮箱所有操作的总连接数保持在 `connection_limit`（2–100）以内。已保存上限为 1 的配置按 `configuration_invalid` 降级启动，允许通过该邮箱的 PUT 接口修正。
## HTTP API

`GET /`、静态资源和 `/healthz` 公开访问。首次设置完成前，其他管理接口统一返回 403 `setup_required`；`POST /api/v1/setup` 只能成功一次。设置后公开登录接口 `POST /api/v1/session`，其余接口使用会话 Cookie 或 `Authorization: Bearer mwk_…`。

Cookie 属性为 HttpOnly、SameSite=Strict、Path=/，TLS 连接或 HTTPS Origin 加 Secure；空闲 7 天或绝对 30 天过期。Cookie 修改请求同时要求 `X-CSRF-Token` 和使用 HTTP 或 HTTPS、主机与端口和请求 Host 完全一致的 `Origin`。Bearer 请求免于 CSRF；显式提交错误的 Authorization 时，即使 Cookie 有效也拒绝请求。

登录和设置码失败按连接来源地址共享限额：15 分钟内 10 次失败，后续请求返回 429 `too_many_attempts`，`Retry-After` 为秒数。密码修改也使用该限额，来源由 Gin ClientIP 按上述可信代理网段解析。登录和设置请求中的跨站 Origin 会被拒绝。由于所有内网对端都被视为可信代理，局域网内的客户端可以伪造 X-Forwarded-For，把失败尝试分散到多个来源地址。不要把 Core 监听端口暴露在不可信的局域网中，只通过反向代理对外提供访问。

JSON 请求体上限 64 KiB，未知字段或多余 JSON 返回 400。密码至少 12 个字符、最多 1024 个 UTF-8 字节；用户名及 Token 名称为 1–128 字节。管理 API 的时间戳统一为 UTC RFC 3339 字符串，包括投递 created_at/accepted_at、Token created_at/last_used_at、文件夹检查时间、日志时间和诊断生成时间；小数秒保留存储精度。未使用 Token 的 last_used_at 为 null，尚未接受的投递省略 accepted_at。计数、时长和 revision 保持数字。

401 表示会话或 API Token 鉴权失败，或 `POST /api/v1/session` 登录凭据错误。设置码错误返回 400 `setup_code_invalid`，向导回到设置码步骤并保留用户名。`PUT /api/v1/admin/password` 的当前密码错误返回 400 `current_password_invalid`，仍计入失败限速，当前会话保持有效。登录失败与当前密码错误在原表单内提示并保留输入。

| 方法 | 路径 | 请求与响应 |
| --- | --- | --- |
| POST | `/api/v1/setup` | `{"code":"XXXX-XXXX-XXXX","username":"admin","password":"…"}`；201，Cookie 与 `csrf_token`；重复设置返回 409 |
| POST | `/api/v1/session` | `{"username":"admin","password":"…"}`；200，Cookie 与 `csrf_token` |
| GET | `/api/v1/session` | 返回 `authenticated:true`、`csrf_token`；Bearer 返回 `method:"token"` |
| DELETE | `/api/v1/session` | 204，注销当前 Cookie 会话；需要会话鉴权 |
| PUT | `/api/v1/admin/password` | `{"current_password":"…","password":"…"}`；204，注销其他会话；Bearer 修改会注销全部会话 |
| POST | `/api/v1/tokens` | `{"name":"automation"}`；201，返回 `id`、`name`、`created_at`、`last_used_at` 和仅显示一次的 `token` |
| GET | `/api/v1/tokens` | `{"tokens":[…]}`，仅元数据 |
| DELETE | `/api/v1/tokens/:id` | 204，撤销 Token；重复撤销仍返回 204 |
| GET | `/api/v1/mailboxes` | `{"mailboxes":[…]}`，邮箱脱敏视图列表 |
| POST | `/api/v1/mailboxes` | 使用下方邮箱表单创建；201，返回生成的 ID、revision 1 和脱敏视图 |
| GET / PUT / DELETE | `/api/v1/mailboxes/:id` | 读取 / 携带当前 revision 更新 / 删除（204）；不存在返回 404 `mailbox_not_found` |
| POST | `/api/v1/mailboxes/test` | 测试未保存的邮箱表单，返回 `{"folders":[…]}`，保持配置原值 |
| POST | `/api/v1/mailboxes/:id/test` | 合并该邮箱已保存的凭据后测试，省略密码保留原值 |
| GET | `/api/v1/mailboxes/:id/folders` | 使用该邮箱预算发现可选文件夹 |
| GET / PUT | `/api/v1/mailboxes/:id/subscriptions` | 读取 / 按独立 revision 保存订阅；成功返回 202 |
| GET / PUT | `/api/v1/settings/delivery` | 读取 / 携带独立 revision 保存全局通道设置 |
| DELETE | `/api/v1/settings/delivery` | 携带 `{revision}` 清除通道与凭据；204，版本冲突409 |
| POST | `/api/v1/settings/delivery/test` | 合并已保存凭据并测试通道表单；204，保持配置原值 |
| GET | `/api/v1/status` | 文件夹状态、通道、投递计数及按作用域组织的 notices |
| GET | `/api/v1/diagnostics` | 下载脱敏 JSON 诊断报告，省略运行日志 |
| GET | `/api/v1/logs` | 鉴权后查询运行日志，支持 after、level、mailbox_id、limit（见下文） |
| GET | `/api/v1/deliveries` | 最近 50 条请求记录，包含标题和邮箱 / 文件夹摘要 |
| POST | `/api/v1/test-push` | 测试通知入队，返回 202 |
| POST | `/api/v1/deliveries/:id/retry` | 重新处理 `dead` 任务，返回 202 |

投递记录的可选 `message` 对象包含 `mailbox_id`、`mailbox_label`、`folder`、`subject`（字符串或 null）和 `test`（布尔值）。名称按入队时保存，邮箱改名或删除后保持历史来源。subject 为 null 表示标题未保留，空字符串表示邮件无主题。测试通知的 test=true，来源为空。摘要接口省略发件人和正文。


创建邮箱示例：

```json
{"label":"工作邮箱","host":"imap.example.org","port":993,"username":"user@example.org","password":"…","connection_limit":10}
```

最多保存 20 个邮箱，包含待修正的无效配置；继续创建返回 400 `mailbox_limit_exceeded`。ID 为 `mbx_` 加随机串，创建后保持稳定。`label` 默认取 username（邮箱地址），最长 64 个 Unicode 字符，禁止换行。用户名超过 64 字符时需显式填写较短名称。更新时省略名称保留原值，空名称使用本次提交的 username。

邮箱 PUT 使用相同表单并携带 GET 读到的 `revision`，例如 `"revision":1`，必须填写 host、port、username。省略 password 保留原密码，显式空密码会被拒绝。省略 `connection_limit` 保留当前值；新邮箱默认 10，范围 2–100。创建 POST 省略 revision 或传 0。测试接口接受相同字段，无须 revision，保持配置原值；未保存邮箱的测试需要凭据，带 ID 的测试只合并该邮箱的已有凭据。

邮箱 GET 返回 `id`、`label`、`revision`、`host`、`port`、`username`、`connection_limit`、`connections_in_use` 与 `"password":{"configured":true}`。降级的无效邮箱返回 `configured:false`，保留 ID 和 revision 供修正。读取响应中的密码对象属于展示视图，不能作为写入值。

每个邮箱的连接预算独立。实时 Folder 各占一条连接，全部定时 Folder 合计占一条，需求最多为 `connection_limit - 1`，预留一条供管理。PUT 订阅及修改连接上限共用此公式，容量不足返回 `connection_budget_exceeded`。未保存邮箱的测试使用临时预算。未保存邮箱测试、已保存邮箱测试和文件夹发现三个入口还共用全局并发上限 2，超出立即返回 429 `discovery_busy`，结束或取消时释放名额。

首次保存通道示例：

```json
{"revision":0,"channel":"bark","preview":"off","retry_count":0,"language":"zh-CN","bark":{"endpoint":"https://api.day.app","key":"…"}}
```

每次更新前读取 `/settings/delivery` 并提交其 revision，首次保存从 0 增加到 1。Pushover 使用 `pushover.token`、`pushover.user`；Webhook 使用 `webhook.url`、`webhook.secret`。channel、preview、revision 必填；省略 language、retry_count 使用默认值。省略凭据（含 Webhook URL）保留已保存值，显式空的当前通道凭据会被拒绝；GET 将凭据替换为 `{"configured":true|false}`。独立测试接口无须 revision。

邮箱创建和连接配置修改在保存前测试连接及登录。host、port、username、connection_limit 与现值相同且省略 password 时，PUT 只改 label：仍校验 revision 并保存，保持连接与进度，已入队通知保留原名称，之后入队使用新名称。创建或修改后的身份若与其他邮箱相同，返回 409 `mailbox_duplicate`；host 与 username 忽略大小写、port 精确匹配，同身份并发创建只成功一个。通道 PUT 仅在类型或当前通道地址、凭据变化时发送测试通知，预览、重试、语言修改直接保存。网络测试在配置锁外执行，现有监听保持运行，测试失败保留原配置。设置 revision 过期返回 409 `settings_conflict`；邮箱测试期间订阅变化同样使快照失效，需重新读取后提交。各邮箱、全局通道分别维护 revision，修改不同邮箱、同时修改邮箱和通道互不冲突。

PUT 成功返回脱敏视图并立即生效。邮箱连接配置更新先停止自身监听，再保存并按需清理进度；存储失败恢复原监听，其他邮箱继续运行。通道变更时，进行中的请求沿用原快照，后续尝试使用新快照；尚未配置通道时队列保持待处理。

`GET /api/v1/mailboxes/:id/subscriptions` 返回 `{"revision":1,"folders":[{"name":"INBOX","check":"realtime"},{"name":"Clients","check":"5m"}]}`。PUT 提交读取到的 revision 与完整 folders 数组，空数组取消该邮箱的全部订阅。名称在邮箱内唯一，INBOX 大小写统一，每项最多 4096 个 UTF-8 字节；每项 name、check 必填，check 为 realtime、5m 或 15m；缺失、非法 check 或旧字符串数组返回 400。连接需求使用上文公式。只改 check 保留进度和事件 ID，衔接新邮件补查；取消后重新订阅继续重建基线。保存成功返回 202 和下一个版本，旧版本返回 409 `subscriptions_conflict`。文件夹存在性由监听确认，邮箱离线时仍可取消订阅。存储失败返回 503，当前订阅保持不变。

`/status.folders[]` 每项包含 `mailbox_id`、`mailbox_label` 与 `check`。mode 为 idle、poll 或 scheduled；定时 Folder 另有 RFC 3339 时间 next_check（失败时为下次重试时间），实时 Folder 省略该字段。已保存配置的 JSON 或模型校验失败时，对应项降级为未配置，在 `/status.notices` 的 `mailbox:<id>` 或 `delivery` 下报告 `configuration_invalid`，可通过该项 PUT 修正。订阅超预算时仅暂停该邮箱，在 `subscriptions:<id>` 下报告 `connection_budget_exceeded`；减少订阅或提高预算后恢复。解密失败仍拒绝启动。

诊断白名单包含生成时间、构建/Go/平台信息、队列计数以及匿名邮箱和监听状态（含 check 与 mode）。`mailboxes[].index` 与 `watches[].mailbox_index` 对应本次报告的“邮箱 1、邮箱 2……”；邮箱内监听有独立的 `index`。邮箱条目包含配置及订阅 notice 的错误码。仅 `imap_folder_rejected` 可导出 `watches[].response_code`。报告省略邮箱 ID、显示名称、地址、主机、文件夹名、邮件内容、任务 ID、凭据及其他错误参数。

任务状态：`pending` 等待请求，`accepted` 通道接受请求，`dead` 本轮请求结束且记录失败。
## 运行日志

通过 `docker compose logs -f core`（或 `docker logs <容器名>`）查看 JSON stderr 日志。管理客户端可使用会话 Cookie 或 API Token 查询同一组运行事件：`GET /api/v1/logs?after=0&level=info&mailbox_id=mbx_example&limit=200`。

内存环形缓冲保留最近 **2000** 条 Info/Warn/Error 日志，重启清空。after 是排除本身的非负序号；level 为 info/warn/error，包含指定级别及以上，默认 info；省略 mailbox_id 时包含全部邮箱和全局操作。limit 默认 200，范围 1–500。参数非法返回 400 `logs_request_invalid`。

```json
{"entries":[{"seq":42,"time":"2026-09-29T00:00:00Z","level":"info","message":"Folder connection established","attrs":{"mailbox_id":"mbx_example","folder":"INBOX","mode":"idle","check":"realtime"}}],"next":42}
```

下一次请求把 next 作为 after。达到页大小时 next 为最后返回序号；遍历完快照时为快照最大序号，过滤结果为空也推进。淘汰的日志从当前保留记录继续；重启导致 next 小于 after 时，将游标重置为 0。条目按序号升序返回。

日志包含邮箱/Folder 生命周期、基线与入队数量、投递尝试、配置变更和管理员操作，全局事件的 mailbox_id 为空。attrs 仅输出这些标量字段：mailbox_id、folder、mode、check、code、event_id、channel、attempt、duration_ms、http_status、next_attempt、backoff_seconds、count、revision、token_id、version、listen。http_status=0 表示未收到 HTTP 响应；token_id 为公开记录 ID，区别于 Token 完整值。密码、Token/会话值、设置码、发件人、主题、正文和凭据 URL 均省略；设置码那一行只输出到控制台。日志包含 Folder 名称，匿名诊断导出继续省略日志和名称。
