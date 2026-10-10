# Core App 管理

官方 Mailwake App 可以在管理员批准后管理这台 Core。本页说明 Core 的接入、授权与信任配置；完整部署步骤见[部署指南](../../docs/deployment.zh-CN.md)。

## 邀请

管理员在 Web 的“App 授权”页面创建邀请（`POST /api/v1/device-invitations`），选择：

- **App 管理**：填写手机能访问到的 Core HTTPS 地址，勾选 scope（`mailboxes`、`folders`、`channels`、`diagnostics`、`content`）。
- **原生推送**：同时创建一个 Relay 配对（默认使用官方生产 Relay，`MAILWAKE_RELAY_URL` 可覆盖地址）。

邀请 5 分钟有效，二维码为 `mailwake://connect?v=3&...`，只在创建响应中出现；管理邀请通过 `scopes` 参数携带逗号分隔的授权范围。App 展示 Core 指纹、地址与本次授权范围，用户确认后调用 `POST /api/v1/app/accept`，取得 `mwac_` 凭证（Core 只保存哈希）。邀请 token 一次性；同一台设备再次接受新邀请会替换它原来的授权。

管理员在 `GET /api/v1/management/devices` 查看已授权设备，`DELETE /api/v1/management/devices/{controller_id}` 立即撤销；App 也可以 `DELETE /api/v1/app/controller` 撤销自己。

## 请求与付费动作

App 请求携带 `Authorization: Bearer mwac_…`。每个 `/api/v1/app/*` 路由在 `internal/httpapi/app_management.go` 的 `appRoutes` 表中声明所需 scope 和是否付费。付费路由还要求 `X-Mailwake-App-Qualification`：Platform 签发的 300 秒 ES256 JWT，必须绑定本 Core 的 `core_id` 与当前设备的 `device_id`。读取、删除、诊断和已存配置测试免费，因此 Pro 到期后这些功能照常可用。已存配置测试每设备每目标每分钟一次。

## 信任配置

```sh
MAILWAKE_APP_MANAGEMENT_TRUST='{"issuer":"mailwake-platform","environment":"production","keys":{"<kid>":"<P-256 SPKI base64url>"}}'
# 或者挂载文件：
MAILWAKE_APP_MANAGEMENT_TRUST_FILE=/etc/mailwake/app-management-trust.json
```

文件方式按[部署指南](../../docs/deployment.zh-CN.md#4-接入官方-app-与-pro)配置只读挂载，修改后重启 Core。未配置信任时，付费路由返回 503 `app_management_unavailable`，其余功能不受影响。

Platform 轮换签发密钥时：先把新 kid 加入信任并重启 Core，Platform 切换签发后 300 秒再移除旧 kid。

## 正文授权

owner 单独授予 `content`，范围为此 Core 当前全部已订阅 Folder。`POST /api/v1/app/content` 使用同一 controller 凭证，但商业资格来自独立 purpose=`native_push` 和 `X-Mailwake-Native-Qualification`。配置读权限保持原范围；同一信任配置验证两类用途和 device/Core/environment/subject/有效期。请求实现位于 `internal/httpapi/app_management.go`，签名引用实现在 `internal/native/message_reference.go`；公开的合成测试向量位于 `internal/appmanagement/testdata/`。

管理员通过 `GET /api/v1/device-invitations/{id}` 查询管理授权状态，返回 `id`、`status`、`device_name`、`expires_at`，状态为 `waiting`、`active`、`expired`、`revoked`。仅管理邀请的二维码据此结束等待；Native 配对继续使用 Native 状态接口。状态响应只含展示信息。

## 订阅目录与 Native 接收设备

- `GET /api/v1/app/folder-mailboxes` 使用 `folders` scope，免费返回 `{mailboxes:[{id,label,revision,connection_limit}]}`。它为单独订阅授权提供目录，不返回邮箱登录与凭据信息；邮箱配置接口继续要求 `mailboxes`。
- `GET /api/v1/app/native/devices` 使用 `channels` scope，免费返回 `{devices:[{id,device_id,device_name}]}`，仅包含当前有效配对。保存 Native 通知配置时使用所选 `id` 作为 `native_pairing_id`；更新配置继续要求有效 AppManagement 资格。
