# Supported features

The implementation currently provides:

- GitHub App webhook ingestion for completed workflows and checks.
- Durable PostgreSQL webhook inbox with deduplication, leased restart recovery, bounded retries and operator redelivery.
- Authenticated webhook processing metrics for queue counts, failures, expired leases and pending age.
- OAuth sign-in with an explicit user/organization allowlist for the entire shared dashboard.
- PostgreSQL storage with automatic schema initialization and installation ownership.
- Repository statistics, date-window analytics, daily trends and intermittent-check detection.
- A commit matrix and card view with workflow, branch, event and commit-count filters.
- HTMX HTML partials and JSON APIs; locally served HTMX and compiled Tailwind assets.
- WebSocket updates for workflow, check and repository lifecycle changes, preserving filters.
- Repository removal/uninstall cascades, idempotent event storage and protection against delayed run events.
- Race-enabled Go tests, PostgreSQL integration tests and Chromium E2E coverage in CI.

See [README](README.md) for configuration, parameters and metric definitions,
[authentication](docs/AUTHENTICATION.md) for the shared-dashboard access model,
and [testing](docs/TESTING.md) for the test harness.

## Limits

SQLite, history backfill, per-user repository filtering,
shared session storage, workflow-duration analytics and stored workflow-job
collection are not implemented. `cache.workflow_ttl` and
`options.max_repos_per_page` remain accepted for compatibility but do not currently
cache or paginate data. Repository, analytics and flaky-check results are cached in
memory per instance (`cache.stats_ttl`, `cache.max_entries`).
Only completed webhook events are stored, so queued/running examples are synthetic
demo data rather than live progress tracking. The flaky-check result is a heuristic
based on check outcomes across commits, not individual test retries on one commit.

The original refactoring proposal is retained in [REFACTORING_PLAN.md](REFACTORING_PLAN.md)
as historical design context. Current improvement work is tracked in the
[implementation plan](docs/IMPROVEMENT_PLAN.md).
