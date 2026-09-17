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

Crowsnest currently uses the default notification policy stored in SQLite; the `issue_updated` default is `false`. Per-user preference management will be added later through an admin API/UI.

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

## Security notes

- Never put App Secrets, GitLab API tokens, or webhook tokens into `.env.example`, fixtures, logs, or Git.
- To verify GitLab user emails through the Feishu Contact API, add and approve the `contact:user.id:readonly` permission for the app.
- Do not copy production payloads into `testdata`.
- Start with `CROWSNEST_DRY_RUN=true`.
- Before enabling production hooks, verify GitLab's webhook test and Crowsnest delivery state together.
