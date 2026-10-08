# Deploy Mailwake Core

[简体中文](deployment.zh-CN.md) · [Documentation](README.md)

Core watches TLS IMAP folders on your server and delivers through Bark, Pushover or Webhook. The Free Web console and script API run independently. The official App, Pro and native push have additional service requirements.

## 1. Prepare and start

Install Git, Docker Engine or Docker Desktop, and Docker Compose v2. Use a mailbox that permits TLS IMAP and, where required, an app password. Keep the server running with network access to your mail and notification providers.

Build from the public source today. Versioned images are published exclusively to `ghcr.io/mingzaily/mailwake`; binaries are attached to [GitHub Releases](https://github.com/mingzaily/mailwake/releases). Use versions that have completed release publication.

```sh
git clone https://github.com/mingzaily/mailwake.git
cd mailwake
docker compose up -d --build --pull never
docker compose logs core
```

The first build downloads base images, Go modules and npm dependencies. Core uses a non-root user, a read-only container filesystem and the persistent `core-data` volume. The host listens on `127.0.0.1:8080`. Run subsequent commands from the same checkout and retain the Compose project name so they use the same volume.

## 2. Set up your administrator and mailbox

Open `http://127.0.0.1:8080` on the Docker host. For a remote server, first forward its port over SSH, replacing `user@server` with your SSH destination:

```sh
ssh -L 18080:127.0.0.1:8080 user@server
```

Keep SSH connected and open `http://127.0.0.1:18080` locally. Enter the one-time setup code from the logs and create an administrator with a password of at least 12 characters. Each restart rotates the code until setup completes.

Add the TLS IMAP host, port (usually 993), username and app password. Scan and select folders. Choose realtime monitoring for prompt notifications or scheduled checks for less urgent folders. Core supports up to 20 mailboxes, each with its own connection budget. Mailbox and notification setup can be completed later.

Select Bark, Pushover or Webhook in notification settings. Save and send a test notification, then deliver a new message into a watched folder and check monitoring and recent deliveries. Keep reading and replying in your usual mail client.

## 3. Configure HTTPS

Use a reachable address for remote access. The official App requires trusted HTTPS. Point your domain at the server and install a reverse proxy on the same host. This host-installed Caddy example uses a domain you must replace:

```caddyfile
mail.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

Allow ports 80/443 as required by Caddy, and verify DNS and certificate issuance. Keep port 8080 bound to loopback. Preserve the original Host, including its port, and set X-Forwarded-For; Caddy does both by default. A containerized proxy must use the appropriate Docker network address: its loopback address refers to itself.

For nginx on the same host, replace the domain and certificate paths:

```nginx
server {
    listen 443 ssl;
    server_name mail.example.org;
    ssl_certificate /etc/letsencrypt/live/mail.example.org/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mail.example.org/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Core trusts proxy peers in `127.0.0.0/8`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`. Keep its listener accessible only to the proxy or trusted local clients. Public peers cannot override their source address with forwarding headers.

## 4. Connect the official App and Pro

Free is ready after the steps above. The official App has not launched on the App Store yet. Use the download, production Relay URL and Platform public-key trust file supplied with the official launch.

For App management, obtain and verify the official trust JSON. Follow the [environment example](../app-management.env.example) to set its absolute path and the official Relay address; the example hostname only illustrates the format. Save local settings in `app-management.env` and use the [Compose overlay](../compose.app-management.yaml):

```sh
docker compose --env-file app-management.env \
  -f compose.yaml -f compose.app-management.yaml up -d --build --pull never
```

Keep the same `--env-file` and `-f` arguments for subsequent Compose commands. Create an invitation on Core's Phones page, granting management, native push and content access separately. Pro purchase, Core owner approval and device pairing are separate checks. See [App management](../internal/appmanagement/README.md) for scopes and key rotation.

## 5. Back up, restore and upgrade

Backups contain credentials and administrator/device authorizations. Store them with restricted access or encrypted storage. Copy the entire volume, including SQLite files and `secret.key`; both are required for recovery.

Stop writes before copying. These examples use base Compose; when using the overlay, add the same arguments from the previous section to every Compose command:

```sh
docker compose stop core
umask 077
backup="../mailwake-backup-$(date +%Y%m%d-%H%M%S)"
mkdir "$backup"
docker compose cp core:/data/. "$backup/"
docker compose start core
```

Confirm the copy succeeded before upgrading. Retain the directory and volume, back up first, then update and rebuild:

```sh
git pull --ff-only
docker compose up -d --build --pull never
docker compose logs --tail=50 core
```

`main` tracks ongoing development. To pin a version, choose an existing release tag, check it out and rebuild. Once versioned images are published, set the matching `image` tag in `compose.yaml`, run `docker compose pull core`, then `docker compose up -d --no-build`. When database formats change, roll back with both the complete pre-upgrade backup and its matching code version.

To restore, stop Core, copy the complete backup into an empty destination volume, assign all data to container user `10001:10001`, and start the matching version. Keep the original backup until mailboxes, subscriptions, delivery state and device authorizations have been verified. `docker compose down` preserves the volume; `docker compose down -v` deletes it and all its state.

## 6. Troubleshoot

- **Remote browser cannot connect:** the default listener is host-local. Use SSH forwarding or check the HTTPS proxy and DNS.
- **Setup code expired:** read the current container logs. Restarting rotates the code until setup completes.
- **Mailbox authentication failed:** check TLS IMAP, hostname, port and provider-approved app password. Gmail currently requires an account that permits IMAP and app passwords.
- **Missing notifications:** send a channel test, then check mailbox monitoring, folder subscriptions and recent deliveries.
- **Forgotten administrator password:** stop Core, reset interactively using the same volume, then restart:

```sh
docker compose stop core
docker compose run --rm -it core admin reset-password
docker compose start core
```

The reset command acquires the data directory lock; stop Core before running it. Password reset signs out browser sessions and keeps API tokens, mailboxes and queued work. Include the console's redacted diagnostics in reports; review any additional logs and folder names before posting publicly.

## Build from source

Requires Go 1.25 and Node.js 24.18.1 (`.nvmrc`). Use npm and the committed `web/package-lock.json`.

```sh
git clone https://github.com/mingzaily/mailwake && cd mailwake
make build
./bin/mailwake
```

The listen and storage environment settings are `MAILWAKE_LISTEN` (default `127.0.0.1:8080`, container `0.0.0.0:8080`) and `MAILWAKE_DATA_DIR` (default `data`, container `/data`). Optional `MAILWAKE_RELAY_URL` enables Mailwake App notifications; leave it empty to hide native push. Startup uses SQLite configuration. Configuration files and credential environment variables have been removed.

Stop Core before recovery commands, using the same data directory:

```sh
./bin/mailwake admin reset-password  # interactive terminal, input is hidden
./bin/mailwake admin reset-setup
```

Both commands acquire the process data lock. Password reset signs out all browser sessions and preserves API tokens. Setup reset clears the administrator, sessions and API tokens; the next start prints a fresh code. Mailbox settings, subscriptions and queue data remain intact.
