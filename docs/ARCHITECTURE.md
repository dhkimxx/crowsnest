# Crowsnest Architecture

Status: initial baseline agreed; details still evolving

## Goal

Crowsnest is a self-hosted service that converts events from Git services such as GitLab, GitHub, and Forgejo into a common internal event, resolves recipients deterministically, and delivers personalized Feishu direct messages through a persistent outbox.

This implementation targets GitLab 17.6 and the Feishu Self-Built App Bot. Boundaries are drawn so that other providers and LLM features can be added as adapters without rewriting the core logic.

## Key decisions

- A modular single service with a Ports & Adapters structure, not microservices
- The HTTP server and internal workers run inside one Go process
- Webhook events and per-recipient deliveries are written to SQLite before the webhook returns success
- Feishu delivery happens asynchronously in a persistent outbox worker
- GitLab payload shapes and version differences are absorbed by the provider adapter's decoder
- Recipients, authentication, deduplication, and delivery success are deterministic
- The LLM is an asynchronous enricher added later; it never blocks base notifications
- Feishu is called through the official HTTP API; no Custom Bot SDK dependency

## Runtime layout

```text
                    ┌─────────────────────────┐
GitLab System Hook ─▶                         │
GitLab Project Hook ─▶  HTTP Ingress :5680     │
                    │  auth · provider decoder │
                    └────────────┬────────────┘
                                 │ transaction
                                 ▼
                    ┌─────────────────────────┐
                    │ SQLite                  │
                    │ events / deliveries     │
                    │ identities / preferences│
                    └────────────┬────────────┘
                                 │ claim
                    ┌────────────▼────────────┐
                    │ Internal Workers        │
                    │ delivery / recovery     │
                    │ hook reconcile scheduler│
                    │ identity sync scheduler│
                    └────────────┬────────────┘
                                 ▼
                    ┌─────────────────────────┐
                    │ Feishu Official API     │
                    │ email personal message  │
                    └─────────────────────────┘
```

Single process means a single executable; internally it uses role-specific goroutines and bounded worker pools.

## Request flow

1. HTTP ingress receives `POST /webhook/gitlab`.
2. It verifies `X-Gitlab-Token`.
3. The provider registry picks the decoder that matches the webhook headers and body.
4. The decoder converts the GitLab payload into a canonical event.
5. Unsupported events are recorded with an `ignored` result.
6. The application router computes recipient candidates and notification reasons.
7. The event and per-recipient deliveries are stored in one database transaction.
8. GitLab receives a 2xx only after a successful write.
9. The delivery worker claims pending deliveries and sends them to Feishu.
10. Success is recorded as `delivered`; retryable failures become `failed` or the next attempt state.

A Feishu outage never fails the GitLab webhook response, and delivery state survives process restarts.

## Layers and dependency direction

```text
domain
  ▲
application ─── ports
  ▲              ▲
adapters ────────┘
platform
```

### Domain

The common business model that knows nothing about external API shapes.

- `CanonicalEvent`
- `Identity`
- `ProjectRef`
- `ResourceRef`
- `Notification`
- `RecipientAddress`
- `Delivery`

GitLab's `object_attributes` or Feishu's `receive_id` never leak into the domain model.

### Application

Composes the business flows.

- Webhook intake
- Event normalization
- Recipient resolution
- Recipient allowlist policy
- Preference handling
- Delivery creation
- Outbox dispatch
- Failure recovery
- Hook reconciliation
- User mapping sync

### Ports

Defines the minimal interfaces each caller needs. There is no single giant provider or repository interface.

- `WebhookDecoder`
- `UserDirectory`
- `HookController`
- `Messenger`
- `EmailDirectory`
- `EventStore`
- `OutboxStore`
- `IdentityStore`
- `PreferenceStore`
- `PipelineStateStore`

### Adapters

- `git/gitlab/v17_6`: GitLab 17.6 webhook decoder and GitLab API hook controller
- `messenger/feishu`: tenant token and personal message delivery
- `database/sqlite`: initial persistence implementation
- Future additions: `git/github`, `git/forgejo`, `database/postgres`, `messenger/slack`

## Canonical event model

Not every provider field is generalized. The model keeps only the stable meanings that current routing rules and future shared features need.

```text
source
source_version
source_event
event_key
kind
action
occurred_at
project
actor
author
object
reviewers
assignees
mentions
changes
source_text
raw_status
```

Provider-specific field paths stay inside the decoder. Only bounded string metadata may be kept when needed; full raw payloads and secrets are never stored on the canonical event.

GitLab 17.6 is implemented first; if version differences grow, a decoder such as `gitlab/v17_7` is added. Routing, outbox, and messenger code never branches on provider versions.

## Identity boundary

The identity of a Git user and the delivery address of a messenger are separate concerns.

```text
GitLab user ID / username / email
  → IdentityResolver
  → canonical company email
  → Feishu email recipient
```

A valid email in the payload is preferred. When the email is missing or `[REDACTED]`, the ID/username mapping in `IdentityStore` is used. The Feishu Contact API is an optional verification and conversion step; it is not a way to guess a GitLab username's email.

When a webhook has no email, the GitLab adapter enriches the user through the `/users/:id` endpoint or a username search and passes the result to the router. Enriched emails are remembered in the SQLite identity store and reused for later events. A GitLab API outage never fails the base webhook response; an existing mapping is used when available.

### Identity sync

User sync is a separate application flow that combines directories from the GitLab API and the Feishu Open Platform API.

```text
GitLab Admin API /users or /users/:id
  → GitLab user ID, username, state, email
  → allowed-domain and active-state filter
  → (optional) Feishu Contact API /contact/v3/users/batch_get_id
  → activate the GitLab email mapping
  → IdentityStore upsert + disable stale mappings
```

A Feishu lookup failure fails the whole sync run, and partial responses are not applied to mappings. A successful Feishu response that lacks a user disables that mapping. The identity sync scheduler in `serve` runs once on startup and then repeats at the configured interval.

## Outbox and delivery state

Event-level state and recipient-level delivery state are separate.

```text
event: accepted / ignored / invalid
delivery: pending / processing / delivered / failed
```

A `delivery_key` is derived from the event, normalized email, and reason group. Workers use leases and retry counts, and a delivered notification is never sent again.

The initial SQLite store uses WAL and explicit transactions. Replacing the persistence adapter must not change the application layer.

## Hook reconciler

The hook reconciler runs as a scheduler worker in the same process.

- Identifies only hooks owned by Crowsnest.
- Creates missing hooks.
- Updates Crowsnest hooks whose settings differ.
- Never deletes or modifies other hooks.
- Provides `dry-run` and `once` execution paths.
- Handles API rate limits and races right after project creation.

Live webhook handling and the reconciler share a process but keep separate code, permissions, and worker queues.

## Feishu boundary

The application never builds Feishu card JSON directly. It produces a meaning-oriented notification, and the Feishu adapter renders the official API request.

```text
Notification
  → Feishu MessageRenderer
  → tenant_access_token
  → receive_id_type=email
  → application-level response check
```

Tokens, app secrets, and raw payloads are never logged.

## Card interactions

Cards can carry actions. A click is delivered to the app over the Feishu long connection (`card.action.trigger`), normalized into a provider-neutral interaction, and handled by one application service.

```text
card.action.trigger
  → Feishu interaction worker (long connection)
  → InteractionService: dedupe → delivery lookup → action
  → updated card or settings panel in the callback response
```

- A card is identified by its message id, which maps back to the delivery and recipient, so only the recipient's card can trigger an action for that delivery.
- Interaction callbacks are deduplicated by event id and audited in `interaction_events` (actor, action, result).
- The settings panel is rendered from the database on every open, so card state is never stale.
- Actions that only change Crowsnest's own notification state (mute, unmute, panel navigation) live in this path. Changing external systems (GitLab) requires an explicit approval and audit boundary and is out of scope.

## LLM extension

The LLM is an asynchronous `AIEnricher` that reads `CanonicalEvent` and deterministic notifications.

```text
CanonicalEvent
  ├─ deterministic router → base delivery
  └─ AIEnricher → AIAnnotation → optional card enrichment
```

The LLM never decides recipients, bypasses authentication, deduplicates events, or marks deliveries successful.

## Go package layout

```text
cmd/crowsnest/
internal/domain/
internal/application/
internal/ports/
internal/adapter/git/gitlab/v17_6/
internal/adapter/messenger/feishu/
internal/adapter/database/sqlite/
internal/platform/config/
internal/platform/httpserver/
testdata/gitlab/v17_6/
deploy/
docs/
```

## Open decisions

- SQLite driver and migration tooling
- Canonical event detail fields and how changes are represented
- Default interval of the hook reconciler
- Operational storage of the GitLab admin API token
- External exposure and HTTPS/reverse proxy for `crowsnest.example.test:5680`
- Real GitLab 17.6 instance fixtures and the confidential-event policy
