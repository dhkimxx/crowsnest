# Crowsnest Testing

## Static checks

```bash
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go vet ./...
git diff --check
```

## Docker checks

```bash
docker compose -f deploy/compose.yaml config
docker compose -f deploy/compose.yaml build
```

After starting the container, check these endpoints:

```bash
curl http://127.0.0.1:5680/healthz
curl http://127.0.0.1:5680/readyz
```

## Fixture checks

Fixtures under `testdata/gitlab/v17_6/` follow the official GitLab 17.6 webhook shapes and contain no secrets or real user data.

Covered scenarios:

- Pipeline failure
- Merge request creation and reviewer changes
- MR notes
- Issue assignee changes
- Unsupported lifecycle events from System Hook
- Missing email and username-based mapping

User sync tests mock GitLab user pagination, private-email fallback, Feishu `batch_get_id` batch lookups, Contact API permission errors, and disabled stale mappings.

## Feishu mock checks

Feishu adapter tests use an `httptest` server and verify:

- Tenant access token requests
- `receive_id_type=email`
- Interactive card JSON
- Feishu application-level success codes
- One token refresh after expiry
- 429 rate-limit classification

Automated tests never send real Feishu messages.

## Outbox checks

- Events and deliveries are stored together.
- The same delivery key is never created twice.
- Workers claim pending deliveries.
- Delivered notifications are not sent again.
- Retryable failures are retried after backoff.
- Deliveries become terminally failed after the maximum attempt count.

## Hook Reconciler checks

Using a mock GitLab API:

- Dry-run performs no POST/PUT requests.
- Missing System/Project Hooks are created.
- Crowsnest hooks with different settings are updated.
- Hooks with other names are left alone.
- Project pagination is handled.
- API errors on one project do not stop the rest; the failure count is reported.

Running `--apply` against a real GitLab and activating production hooks are separate approval steps.
