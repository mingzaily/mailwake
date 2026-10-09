# 原生推送与 App 接入

[English](native-push.md) · [文档目录](README.zh-CN.md)

## Mailwake App 原生推送

`MAILWAKE_RELAY_URL` 未设置或为空时，默认使用官方生产 Relay（`https://notify.mailwake.oritx.com`）。沙盒测试设置为 `https://notify-sandbox.mailwake.oritx.com`，自定义 Relay 填写对应 HTTPS origin，本机开发可用回环 HTTP。通知设置中的 Mailwake App 和“手机”区块可生成五分钟二维码并解除配对；同时最多三个等待配对；正在等待、已建立和已选设备待撤权清理共占十六台手机名额，已撤未选设备邀请释放名额。二维码是一次性凭证，只用本人手机扫描并核对 Core 短指纹。iOS App 已实现配对与通知扩展解密；生产 APNs 投递和设备展示分别验收。

管理 API 为 POST `/api/v1/native/pairings`、GET `/api/v1/native/pairings/:id`、GET `/api/v1/native/devices`、DELETE `/api/v1/native/devices/:pairing_id`，复用管理员鉴权和 CSRF。创建响应的 URI 包含一次性 Token，其余管理响应省略 Token。Core 将等待配对所需 Token 加密保存，终态清除；先验证并持久化锁定设备，再向 Relay 提交正式配对 Token。日期使用 UTC RFC3339，URI 的 `exp` 和签名时间戳为整数 Unix 秒。

移除手机时先在本地撤销，停止后续通知，再到 Relay 删除配对；调用失败按退避重试，直到 Relay 确认或报告配对已不存在。尚未选定设备的等待中配对直接删除。

原生通道始终加密携带发件人和主题，识别到验证码时额外携带 `code`，第三方预览偏好保留原值。仅 Native 通道通过 `BODY.PEEK[]` 读取邮件前 256 KiB；邮件截断或 MIME 解析失败时验证码为空。正文仅在识别期间留在内存，存储、日志和通知均省略正文。明文 JSON 上限为 2032 字节，超限时依次截短主题、发件人，始终保留验证码。每事件固定设备集合并预存独立密文，重试与重启复用。Core accepted 表示全部设备的请求已被 Relay 接收，逐设备状态在接收后的第十秒、第六十秒查询并持久化；重启后补查。已经开始的原生任务沿用原生通道完成重试。原生终态清除投递载荷明文、密文和签名，管理员历史保留标题与邮箱 / 文件夹摘要，因此失败记录用于诊断，第三方通道继续支持手动重试。

从测试环境切换到正式环境需要重新配对手机。Relay 地址、audience 或环境变化出现 `native_environment_mismatch` 时：

1. 停止 Core，继续使用原 `MAILWAKE_DATA_DIR` 和 `secret.key`。
2. 在交互终端运行 `./bin/mailwake admin reset-native-push`，输入 `reset-native-push` 确认。Docker 使用 `docker compose stop core`，再运行 `docker compose run --rm -it core admin reset-native-push`。
3. 命令确认剩余目标为零后，将 `MAILWAKE_RELAY_URL` 设置为新 Relay 地址，启动 Core 并重新配对手机。

命令取得数据目录锁后，到已保存的旧 Relay 删除全部已配对设备。旧 Relay 无法访问时保留配对和旧绑定，可访问后再次运行命令。全部配对删除后，单个事务清除 Relay 绑定、本地配对和待处理的原生任务；Core 签名密钥、指纹和普通通道历史保留。

当前 Native 通知载荷携带发件人、主题和识别到的验证码。与 Relay、App 的接口依据[App 管理说明](../internal/appmanagement/README.md)；Core 测试使用 `internal/native/testdata/test-vectors.json` 与 `internal/appmanagement/testdata/` 中的公开合成向量。

## 只读正文与验证码活动

Native 通知密文带有 Core 签名的稳定邮件引用：Core、邮箱、Folder、UIDVALIDITY、UID。owner 在邀请中单独勾选 `content`，授权此 Core 当前全部已订阅 Folder 的正文；普通配置 scopes 与单独 Native 配对保持各自范围。

App 使用已确认 HTTPS 地址、设备绑定 controller 凭证和 NativePush 用途短期资格调用 `POST /api/v1/app/content`。Core 使用现有连接额度中的非监听连接，先读取 MIME 结构，再以 BODY.PEEK 读取选择的 text/plain 或 HTML 文本 part。20 秒总超时、1 MiB 原始邮件、64 KiB 可读文本；响应 no-store。正文只在内存中解析，不写入 Outbox、历史或诊断。

验证码事件同时生成普通通知计划和独立活动密文计划，保存在同一事务中。活动失败保持普通通知状态；活动截止为首次 received_at + 600 秒。接口与授权见 [App 管理说明](../internal/appmanagement/README.md)，签名测试向量位于 `internal/appmanagement/testdata/`。
