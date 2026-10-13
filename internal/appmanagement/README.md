# App management implementation

[Contributor index](../../docs/README.md) · [User connection guide](https://mailwake.oritx.com/en/docs/connect-app/)

Core grants device and resource permissions after owner approval. The official App's business entitlement checks remain in iOS and Platform.

## Invitations

`POST /api/v1/device-invitations` creates an invitation with a phone-reachable Core HTTPS origin and selected scopes: `mailboxes`, `folders`, `channels`, `diagnostics` and `content`. The owner can also request native push pairing through the configured Relay; `MAILWAKE_RELAY_URL` overrides the production default.

Invitations expire in five minutes. The `mailwake://connect?v=3&...` URI appears only in the creation response and carries comma-separated management `scopes`. The App displays the origin, Core fingerprint and granted scopes before calling `POST /api/v1/app/accept`. Acceptance returns an `mwac_` credential; Core stores its hash. The invitation token is single-use. A new accepted invitation for the same device replaces its previous grant.

Administrators list grants through `GET /api/v1/management/devices` and revoke one immediately with `DELETE /api/v1/management/devices/{controller_id}`. The App revokes itself through `DELETE /api/v1/app/controller`.

`GET /api/v1/device-invitations/{id}` returns presentation-only fields `id`, `status`, `device_name` and `expires_at`. Status is `waiting`, `active`, `expired` or `revoked`. Management-only invitations use it to end QR waiting; native pairings keep their separate status endpoint.

## Requests and permissions

App requests use `Authorization: Bearer mwac_…`. Each `/api/v1/app/*` route declares its required scope in `appRoutes` in `internal/httpapi/app_management.go`. Core validates the credential, revocation state, scope, resource range and business constraints. Configuration mutations retain revision optimistic locking. Tests of saved configuration are limited to once per device and target per minute.

The official App checks Pro for folder discovery and management modifications. Reads, deletion, diagnostics, saved-configuration tests and body reading are free. Core configures networking, storage and Relay access; iOS and Platform own commercial state.

## Content authorization

The owner separately grants `content` for all currently subscribed folders on this Core. `POST /api/v1/app/content` uses the controller credential and verifies content scope, the signed message reference, current subscription and mailbox identity. Body reading is free. HTTP handling lives in `internal/httpapi/app_management.go`; message-reference signatures live in `internal/native/message_reference.go`. Limits and privacy rules are in [HTML content](../../docs/html-content.md).

## Subscription directory and native receiving device

- `GET /api/v1/app/folder-mailboxes` requires `folders` and returns `{mailboxes:[{id,label,revision,connection_limit}]}` without mailbox login or credential fields. Mailbox configuration endpoints continue to require `mailboxes`.
- `GET /api/v1/app/native/devices` requires `channels` and returns `{devices:[{id,device_id,device_name}]}` for active pairings. Saving native notification configuration uses the selected `id` as `native_pairing_id`; iOS checks AppManagement entitlement for the configuration update.
