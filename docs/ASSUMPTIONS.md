# Crowsnest Assumptions and Limits

- The current GitLab target version is Self-Managed 17.6.
- System Hook is used for merge requests and lifecycle detection; Pipeline, Note, and Issue events are covered by Project Hooks.
- Managing project hooks across the instance requires a GitLab Administrator API token.
- System Hook and Project Hook share a single Crowsnest webhook URL.
- Push, Tag, Job, Deployment, Wiki, and Confidential events are not notified by default.
- Pipeline failure recipients prefer the HEAD commit author email. The pipeline user and the commit author are not assumed to be the same person.
- When the email is `[REDACTED]` or missing, the GitLab user ID/username mapping is used.
- Feishu recipients prefer direct email delivery. Contact API email→open_id lookup is not part of the default path.
- Users without a mapping or preference configuration are safely not notified.
- Webhook events and per-recipient deliveries are stored in a SQLite outbox before the webhook returns success.
- SQLite assumes a single host and a single process. If high availability or multiple replicas are needed, a PostgreSQL adapter must be added.
- Feishu delivery dry-run and GitLab hook reconciler dry-run are controlled by independent settings.
- The LLM is not part of this version and will only be added later as an asynchronous enrichment feature.
- Crowsnest never performs external state changes such as GitLab approvals, comments, or label changes on the notification path.
- Real production GitLab payloads may differ from local fixtures; verify webhook test results separately before going live.
