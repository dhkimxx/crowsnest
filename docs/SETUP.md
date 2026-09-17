# Crowsnest Setup

## Requirements

- Go 1.24+ or Docker
- GitLab Self-Managed 17.6
- GitLab API token with administrator scope: only needed for the Hook Reconciler
- Feishu Self-Built App Bot: needed for real delivery mode
- A network route from the GitLab server to the Crowsnest webhook address

## Run locally

```bash
cp .env.example .env
go run ./cmd/crowsnest serve
```

The default HTTP address is `:8080`.

## Run with Docker Compose

Compose runs Crowsnest as its own service and exposes it on host port `5680`.

```bash
docker compose --env-file .env -f deploy/compose.yaml up --build
```

Inject production settings through your secret manager or a local `.env`. Never commit `.env`.

## Environment variables

Only the names of required settings are documented here. Never record values in this document or in Git.

| Variable | Description |
| --- | --- |
| `CROWSNEST_HTTP_ADDR` | HTTP address inside the container. Default `:8080` |
| `CROWSNEST_DB_PATH` | SQLite file path. Default `data/crowsnest.sqlite3` |
| `CROWSNEST_DRY_RUN` | When `true`, uses a dry-run messenger instead of Feishu |
| `CROWSNEST_RECONCILE_DRY_RUN` | When `true`, the Hook Reconciler does not write to GitLab |
| `CROWSNEST_DELIVERY_POLL_INTERVAL` | Outbox worker poll interval. Default `2s` |
| `CROWSNEST_RECONCILE_INTERVAL` | Hook reconcile interval. Default `15m` |
| `CROWSNEST_IDENTITY_SYNC_ENABLED` | Enables the worker that syncs GitLab user to Feishu user mappings |
| `CROWSNEST_IDENTITY_SYNC_DRY_RUN` | When `true`, the user mapping database is not modified |
| `CROWSNEST_IDENTITY_SYNC_INTERVAL` | User mapping sync interval. Default `1h` |
| `CROWSNEST_IDENTITY_SYNC_VERIFY_FEISHU` | When `true`, also verifies email existence through the Feishu Contact API. Default `false` |
| `CROWSNEST_RECIPIENT_ALLOWLIST` | Empty sends to all resolved recipients; otherwise a comma-separated email allowlist |
| `CROWSNEST_WEBHOOK_SECRET` | Value used to verify the `X-Gitlab-Token` header of inbound webhooks |
| `CROWSNEST_GITLAB_BASE_URL` | GitLab base URL. Used for API access and to normalize notification card links |
| `CROWSNEST_GITLAB_API_TOKEN` | API token used to manage project hooks across the instance |
| `CROWSNEST_GITLAB_WEBHOOK_URL` | Crowsnest URL that GitLab hooks should call |
| `CROWSNEST_GITLAB_WEBHOOK_TOKEN` | Secret token embedded into automatically created hooks |
| `CROWSNEST_GITLAB_HOOK_TOKEN` | Administrative hook token when it must differ from the webhook token |
| `CROWSNEST_GITLAB_HOOK_NAME` | Hook name owned by Crowsnest |
| `CROWSNEST_GITLAB_SSL_VERIFY` | Whether outgoing GitLab HTTPS requests verify certificates |
| `CROWSNEST_ALLOWED_EMAIL_DOMAINS` | Allowed email domain list for recipients |
| `CROWSNEST_FEISHU_BASE_URL` | Feishu Open API base URL. Default `https://open.feishu.cn` |
| `CROWSNEST_FEISHU_APP_ID` | Feishu App ID |
| `CROWSNEST_FEISHU_APP_SECRET` | Feishu App Secret |

The inbound webhook secret and the auto-generated hook token can share one value by default; production deployments may separate them.

## Automatic user sync

GitLab user IDs/usernames without an email in a webhook payload are enriched through the GitLab administrator API, and the resolved email mapping is remembered in SQLite. Periodic sync also uses the GitLab email as its primary source. Email existence is additionally verified through the Feishu Open Platform `batch_get_id` API only when `CROWSNEST_IDENTITY_SYNC_VERIFY_FEISHU=true`. When Feishu verification is enabled and the Contact API returns a permission or network error, that run leaves existing mappings unchanged.

Manual runs default to dry-run.

```bash
go run ./cmd/crowsnest sync-users --dry-run
go run ./cmd/crowsnest sync-users --apply
```

Enable continuous sync with:

```text
CROWSNEST_IDENTITY_SYNC_ENABLED=true
CROWSNEST_IDENTITY_SYNC_DRY_RUN=false
CROWSNEST_IDENTITY_SYNC_INTERVAL=1h
```

`serve` syncs once on startup and then repeats at the configured interval. Secrets and raw responses from the GitLab Users API and the Feishu Contact API are never logged.

## Recipient limits

When `CROWSNEST_RECIPIENT_ALLOWLIST` is empty, notifications go to every resolved recipient. When one or more emails are set, only recipients matching those emails are allowed, both during routing and in the Outbox worker. Enabling the allowlist therefore also blocks pending deliveries for other recipients.

To test with a single recipient:

```text
CROWSNEST_RECIPIENT_ALLOWLIST=carol@example.com
```

The list is normalized to lowercase and separated by commas. To restore full delivery, clear the value and restart the service.

## Notification preferences

The default notification policy lives in SQLite; the `issue_updated` default is `false`. Recipients can also pause all alerts for 30 days from the card settings panel: the mute deadline is stored per recipient and evaluated lazily, so alerts resume automatically when it passes.

## Hook Reconciler

Check with a dry run first:

```bash
go run ./cmd/crowsnest reconcile --dry-run
```

Create or update hooks only with an explicit `--apply`:

```bash
go run ./cmd/crowsnest reconcile --apply
```

The Reconciler treats only hooks whose name matches `CROWSNEST_GITLAB_HOOK_NAME` as owned by Crowsnest. It never deletes or modifies other hooks.

Default management policy:

- System Hook: Merge Request events
- Project Hook: Pipeline, Note, and Issue events
- Push, Tag, Job, Deployment, Wiki, and Confidential events: disabled by default

## GitLab network

The default Compose address is:

```text
http://<crowsnest-host>:5680/webhook/gitlab
```

Replace the placeholder with an address routable from the GitLab server. If that is not possible, use an HTTPS reverse proxy or an address reachable from the GitLab network.

## Feishu permissions

Real delivery mode requires the Feishu Self-Built App Bot feature and message-send permission. Crowsnest uses the email directly as `receive_id`; converting email to `open_id` through the Feishu Contact API is not part of the default path.

## Card interactions

Notification cards expose a single **Settings** button that opens a control panel card: it shows the current alert state and offers mute (30 days), unmute, and close. The panel is rendered from the database on every click, so it never shows stale state. When Feishu app credentials are configured and `CROWSNEST_DRY_RUN=false`, `serve` also opens a Feishu long connection (WebSocket) to receive card callbacks, so no public callback URL is required.

Console steps, in order:

1. Start Crowsnest with the Feishu app credentials so the long connection is online.
2. In the Feishu developer console under **Events and callbacks**, enable **Receive events through persistent connection** and verify the connection.
3. Under **Callback configuration**, subscribe **Card interaction callback** (`card.action.trigger`). Do not subscribe the legacy `card.action.trigger_v1`; subscribing both produces duplicate callbacks.
4. Publish a new app version for the callback subscription. Enterprise admin approval may be required.

Runtime behavior:

- The callback handler must respond within 3 seconds; Crowsnest applies the action and returns the updated card in the response body.
- Closing the panel restores the original notification card from the stored delivery payload.
- `deliveries.provider_message_id` maps a clicked card back to its delivery and recipient. Callbacks for unknown message IDs are rejected.
- Interactions are recorded in `interaction_events` keyed by the callback event ID, so Feishu retries are deduplicated.
- Card interactions are accepted for 30 days after sending; card updates only take effect for 14 days.
- Muting sets a 30-day deadline for that recipient; deliveries are skipped until it passes. The deadline is evaluated lazily on read, so notifications resume automatically without a scheduler.
- Unmuting clears the deadline from the panel. Because a muted recipient receives no new cards, the last received card (interactable for 30 days) is the control surface for resuming.

## Security notes

- Never put App Secrets, GitLab API tokens, or webhook tokens into `.env.example`, fixtures, logs, or Git.
- To verify GitLab user emails through the Feishu Contact API, add and approve the `contact:user.id:readonly` permission for the app.
- Do not copy production payloads into `testdata`.
- Start with `CROWSNEST_DRY_RUN=true`.
- Before enabling production hooks, verify GitLab's webhook test and Crowsnest delivery state together.
