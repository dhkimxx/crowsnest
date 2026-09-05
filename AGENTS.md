# Crowsnest workspace instructions

## Scope

Crowsnest receives GitLab webhook events, determines the relevant people, and sends personalized Feishu App Bot messages. The service must remain useful without the future LLM layer.

## Required boundaries

- Keep webhook authentication, event normalization, recipient identity, preference checks, deduplication, and delivery state deterministic.
- Do not let an LLM decide recipients, bypass authentication, mark deliveries successful, or perform external actions directly.
- Use GitLab System Hook for instance-wide events and an API-managed Project Hook fleet for events unavailable from System Hook.
- Treat System Hook, Project Hook, Pipeline Hook, Note Hook, Issue Hook, and Merge Request Hook as different input contracts until normalized.
- Use the official Feishu Open API and Self-Built App Bot flow. Prefer `receive_id_type=email` for personal messages; do not add a Custom Bot dependency.
- Keep GitLab mutation operations out of the notification path unless a future feature explicitly defines human approval and an auditable action boundary.

## Secrets and data

- Never commit App Secret, tenant access token, GitLab token, Webhook token, personal data, or captured production payloads.
- Use placeholders in documentation and redacted fixtures in tests.
- Do not log full webhook payloads, message bodies, authorization headers, or tokens.
- Keep delivery state and AI enrichment state separate so an LLM failure cannot block the base notification.

## Verification

- Pin and document external API assumptions, especially the GitLab 17.6 payloads and Feishu API response codes.
- Test HTTP status and application-level response codes.
- Test retries, failed delivery recovery, duplicate webhook delivery, missing fields, `[REDACTED]` emails, and self-notification suppression.
- Do not register, activate, or modify external webhooks during local fixture tests.
