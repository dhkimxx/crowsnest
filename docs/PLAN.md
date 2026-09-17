# Crowsnest Implementation Plan

Status: initial implementation in progress

## Goal

Convert GitLab instance events into per-person actionable notifications and deliver them as Feishu Self-Built App Bot direct messages. Run as a standalone Go service, with boundaries that allow adding LLM analysis later.

## Agreed defaults

- Language: Go
- Deployment: Docker Compose
- Crowsnest external port: `5680`
- Storage: SQLite initially
- Input URL: a single `/webhook/gitlab` endpoint
- GitLab input: global System Hook plus Project Hooks managed automatically through the API
- Feishu: official HTTP API, `receive_id_type=email`, personal DMs
- Initial run: dry-run first
- LLM: not called in this version; only the extension interface is prepared
- GitLab mutations: no approvals, comments, or label changes on the notification path
- Existing external automation: left untouched

## Scope

### 1. Go service foundation

- Go module
- `net/http` based HTTP server
- `log/slog` structured logging
- `/healthz`, `/readyz`
- Environment variable and secret file configuration
- Dockerfile and a separate Compose stack
- SQLite connection and migration structure

### 2. GitLab event intake

- `X-Gitlab-Token` verification
- System Hook, Merge Request Hook, Pipeline Hook, Note Hook, and Issue Hook intake
- Normalize headers and bodies into the common internal event model
- Unsupported events are recorded with `ignored` status
- Full raw payloads and authorization headers are never logged

### 3. Hook Reconciler

- Inspect, create, and update the global System Hook
- Paginate through all projects
- Create and update only the Project Hooks owned by Crowsnest
- Enable only Pipeline, Note/Comment, and Issue events on Project Hooks
- Detect newly created projects automatically
- Support dry-run and once execution
- Handle periodic runs, rate limits, retries, and race conditions
- Never delete or modify hooks owned by other systems

### 4. Deterministic personalized routing

- Pipeline failure and clear recovery notifications
- MR reviewer and assignee assignments
- Meaningful merge request updates
- Comments from other users on a merge request
- Issue comments and assignee changes
- `@username` mentions
- Map GitLab user ID/username to company email
- User mapping sync through the GitLab Administrator API with optional Feishu Contact verification
- Suppress self-notifications
- Merge multiple notification reasons into one DM
- Apply per-user notification preferences

### 5. Feishu delivery

- Tenant access token issuance and caching with an expiry safety margin
- Email-based personal DMs
- Interactive cards
- Check both HTTP status and Feishu application codes
- Distinguish authentication, permission, recipient, rate limit, and transient errors
- Limited retries and a single retry on token expiry
- Never store secrets and tokens in logs, fixtures, or documentation

### 6. Delivery state and recovery

- `pending`, `delivered`, and `failed` states
- Separate event-level and recipient-level delivery state
- Per-recipient delivery keys
- No resend to recipients that already succeeded
- Retry only failed recipients
- Recover pending/failed deliveries after restart
- Practical at-least-once delivery on SQLite

### 7. Test and operations documentation

- Secret-free GitLab 17.6 fixtures
- Pipeline, MR, Note, Issue, mention, duplicate, recovery, and error scenarios
- Dry-run tests against a Feishu mock server
- Tests for GitLab user listing, Feishu email lookup, and mapping sync
- Configuration checks before sending real messages
- Compose operations, secret injection, and SQLite backup guidance
- Procedures for GitLab hook registration, reconciliation, and network reachability

## Out of scope for this version

- LLM summaries, risk scoring, or suggested actions
- Two-way Feishu card buttons and approval actions
- GitLab MR approvals, comments, or label changes
- Support for every GitLab event
- Default delivery of confidential events
- Migrating existing event intake paths
- Activating real hooks or changing external systems in production

## Future LLM boundary

Prepare an `AIEnricher` boundary that takes normalized events and returns structured summaries, impact, suggested actions, and evidence links. The LLM never decides recipients, webhook authentication, deduplication, or delivery success.

## Architecture decision status

The following directions are agreed. Detailed structures and fields are finalized after further discussion.

- The canonical event model is generalized to the business meanings shared by GitLab, GitHub, and Forgejo.
- Provider-specific payload paths and version differences are handled inside each adapter's decoder.
- Implement the GitLab `v17.6` adapter first; later versions extend through separate decoders or a compatibility layer.
- Crowsnest runs as a single process with delivery, recovery, and hook reconcile schedulers as internal workers.
- `reconcile --once` and `reconcile --dry-run` are manual operations, not long-running separate processes.
- Webhook events and per-recipient deliveries are stored in SQLite before the webhook returns success.
- A persistent outbox worker performs Feishu delivery.

Open items:

- Where to store the GitLab API token and its permission scope
- Private network access and HTTPS/reverse proxy setup
- SQLite driver and migration tooling
- Final canonical event fields and the provider-specific metadata range
