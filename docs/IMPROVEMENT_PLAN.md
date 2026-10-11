# Dashboard improvement plan

Baseline: `38fbe27` on main. Each implementation PR includes regression tests
and documentation for behavior it changes. Dependent PRs are stacked in this
order and should be merged from the bottom of the stack.

| PR | Scope | Acceptance criteria |
| --- | --- | --- |
| 1 | This plan | Agree on scope and review order. |
| 2 | Configuration and database reliability | Respect explicit realtime disablement; validate time windows; share one database pool across HTTP and webhooks; return 503 while unavailable; cancel retries and realtime work at shutdown. |
| 3 | Authorization and session security | Fail closed without an explicit user/organization allowlist outside demo mode; reject unauthorized users before creating sessions; secure HTTPS cookies; restrict WebSocket origins; redact request headers. |
| 4 | Frontend and realtime behavior | Render HTMX HTML without JSON parsing; refresh for all webhook event types; preserve workflow filters; populate summary cards from actual metrics; respect realtime disablement. |
| 5 | Analytics correctness | Report the latest failing check timestamp; define flake-rate and success-rate denominators; exclude unfinished runs from success-rate calculations. |
| 6 | Installation lifecycle | Track installation ownership; remove repositories and dependent runs on removal/uninstall; return persistence errors; keep duplicate deliveries idempotent. |
| 7 | Database integration and browser E2E coverage | Exercise PostgreSQL queries, signed webhook ingestion, duplicate delivery, lifecycle, HTML/JSON responses, demo seeding, matrix filters, summary cards and realtime refreshes; run in CI and retain browser failure artifacts. |
| 8 | Documentation and development setup | Replace SQLite and missing-command references; document supported configuration, authorization, API parameters, metric definitions, test commands, deployment limits and webhook-only history ingestion. |

## Authorization decision

Use an explicit user/organization allowlist for access to the entire shared
dashboard. Allowed users can view every tracked repository, aggregate and
realtime message. This is an operator-controlled dashboard, not a per-user
repository view. Repository-level isolation would require a separate design
covering every query, aggregate and broadcast.

## Verification

- Run Go tests with the race detector and `go vet` for each Go change.
- Run the configured golangci-lint checks before publishing each Go PR.
- Use a disposable PostgreSQL database for integration tests; never point the
  test harness at an operator's existing database.
- Run browser tests against a loopback-only demo instance, with console-error
  assertions, traces and screenshots on failure.
- Keep regression tests close to each fix. Add cross-layer coverage in PR 7.

## Follow-up boundaries

Workflow-history backfill, shared session storage for
multiple replicas, computed workflow durations, cache implementation and
per-user repository isolation are separate projects. Document these limits
instead of claiming they are implemented. Existing repository rows cannot be
assigned an installation without a subsequent installation or workflow event;
the lifecycle PR must document that migration behavior.

## Follow-up implementation order

The original eight changes are merged. The next focused PRs cover:

1. Local PostgreSQL: loopback port binding, password authentication for new volumes,
   and instructions for existing volumes.
2. API validation: reject invalid filters and provide bounded workflow pagination.
3. WebSocket authorization: close connections when the authorizing session expires.
4. Shutdown: drain accepted webhook work before closing the database.
5. Browser failure paths: realtime disabled, reconnect, repository removal and
   database outage/recovery.

Each PR includes relevant validation and documentation. Commit creation and
commit-rewriting rebases require explicit user approval. The durable webhook inbox project is now underway; see
[its implementation order](WEBHOOKS.md#implementation-order) for storage, retries
and processing metrics.
