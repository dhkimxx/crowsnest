# Crowsnest

Self-hosted service that turns GitLab events into personalized Feishu direct messages.

## Direction

- Uses the GitLab global System Hook as the default input.
- Events that System Hook does not provide (Pipeline, Note/Comment, Issue) are covered by Project Hooks managed automatically through the GitLab API.
- Webhook intake, authentication, normalization, recipient resolution, and deduplication are deterministic.
- Uses the Feishu Self-Built App Bot with the official Open API, not a Custom Bot webhook.
- Personal DMs prefer email-based delivery (`receive_id_type=email`).
- Notification card titles carry the provider source label, for example `[GitLab] Pipeline failed`.
- Future LLM features are asynchronous aids only (summaries, risk, suggested actions). They never decide recipients, bypass authentication, or gate delivery.

## Run locally

With the Go toolchain:

```bash
go run ./cmd/crowsnest serve
```

With Docker Compose:

```bash
docker compose --env-file .env -f deploy/compose.yaml up --build
```

The health endpoint is `http://127.0.0.1:8080/healthz`; Compose exposes host port `5680`.

User mappings are synced from the GitLab API by default:

```bash
go run ./cmd/crowsnest sync-users --dry-run
go run ./cmd/crowsnest sync-users --apply
```

`CROWSNEST_IDENTITY_SYNC_ENABLED=true` makes the internal worker sync mappings periodically. Adding `CROWSNEST_IDENTITY_SYNC_VERIFY_FEISHU=true` additionally requires the Feishu Contact API permission. Notification preferences currently use the default policy; an admin API/UI will manage them later.

Set `CROWSNEST_RECIPIENT_ALLOWLIST` to deliver only to specific emails; leave it empty to notify every resolved recipient.

## Documentation

- [docs/PLAN.md](docs/PLAN.md) — scope and roadmap
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — structure and boundaries
- [docs/SETUP.md](docs/SETUP.md) — setup and configuration
- [docs/TESTING.md](docs/TESTING.md) — test strategy

## Security principles

- Never commit secrets, tokens, or real user mappings to Git.
- Changing external GitLab or Feishu environments requires a separately approved task.
- Webhook payloads and message bodies are not written to default logs.
- Code, comments, and change descriptions sent to an LLM follow per-project policy and retention.

## License

Apache License 2.0 — see [LICENSE](LICENSE). Copyright 2026 dhkimxx.
