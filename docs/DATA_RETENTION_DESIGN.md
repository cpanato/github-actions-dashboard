# Data retention and cleanup design

Status: proposal, not implemented. Written so the work can be picked up later.

## Problem

The dashboard stores every completed workflow run and check run it receives and
never deletes anything. In the production deployment (Cloud SQL PostgreSQL,
about 10 GB of data) this already shows:

| Table | Rows | Size (with indexes) |
| --- | --- | --- |
| `check_runs` | 21.9 M | 7.6 GB |
| `workflow_runs` | 5.0 M | 1.9 GB |
| `workflow_jobs` | 0 | unused |
| `webhook_deliveries` | not measured | durable webhook inbox with raw payloads, never pruned |

Consequences: slow aggregate queries (Flaky Tests took ~100 s, Repositories
~14 s before covering indexes and caching), growing disk and backup cost, and
`VACUUM`/index maintenance that grows with table size. Nothing in the product
needs history older than the longest query window (`days` is capped at 365 in
`parseQueryInt`; the default UI windows are 7 and 30 days), except the
all-time per-repository statistics (see "Open decisions").

## Goals

- Operators can bound database size with a configurable retention period.
- Cleanup never blocks webhook ingestion or dashboard reads.
- Safe by default: no deletion unless retention is explicitly configured, and a
  dry run is always available.
- Works for a fresh install and for the existing 20 M-row database.
- Idempotent and resumable: interrupted runs can be re-run.

## Non-goals

- Archiving or exporting deleted rows (could be a later `--export` option).
- Per-repository retention rules (start with one global setting).
- Compacting storage inside PostgreSQL beyond what `VACUUM` does.

## Configuration

New `retention` block in the YAML config, with environment overrides using the
existing `DASHBOARD_` prefix convention (read in `server/config.go`):

```yaml
retention:
  # Delete runs older than this. Empty or 0 disables cleanup (default).
  check_runs: "90d"        # accepts Go durations or <n>d
  workflow_runs: "365d"
  # Finished webhook deliveries (succeeded) older than this; failed ones are
  # kept longer for diagnosis.
  webhook_deliveries: "14d"
  webhook_deliveries_failed: "60d"
  # Rows deleted per transaction.
  batch_size: 5000
  # Pause between batches to leave I/O for foreground queries.
  batch_pause: "200ms"
  # Abort a run after this long; remaining rows are handled next run.
  max_runtime: "30m"
```

Environment: `DASHBOARD_RETENTION_CHECK_RUNS`, `DASHBOARD_RETENTION_WORKFLOW_RUNS`,
`DASHBOARD_RETENTION_BATCH_SIZE`.

Validation: durations must be positive and not shorter than the largest
analytics window the API accepts (365 days) unless `--force` is passed, so the
UI never silently shows truncated windows. Note the existing default time
window is a Go duration (`7d` is invalid there); the retention parser should
either reuse that rule or explicitly add a `d` suffix and document it.

Time basis: `check_runs.completed_at` (the column the analytics queries and the
`idx_check_runs_flaky` index already use) and `workflow_runs.created_at`
(used by trends and `idx_workflow_runs_created_at`).

## CLI

The binary currently takes only `-config`. Add a subcommand without changing
the default behaviour (no subcommand = serve):

```
github-actions-dashboard serve   -config config.yml        # default
github-actions-dashboard cleanup -config config.yml [flags]
```

`cleanup` flags:

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Count rows that would be deleted per table and print the oldest/newest affected timestamps; delete nothing. |
| `--check-runs-older-than 90d` | Override config for this run. |
| `--workflow-runs-older-than 365d` | Override config for this run. |
| `--batch-size`, `--batch-pause`, `--max-runtime` | Override config. |
| `--vacuum` | Run `VACUUM (ANALYZE)` on touched tables afterwards. |
| `--force` | Allow windows shorter than 365 days. |

Exit codes: 0 success (including "nothing to do"), 1 error, 2 stopped by
`max_runtime` with work remaining (lets schedulers alert or re-run).

Running it on Cloud Run: a Cloud Run **Job** using the same image and service
account, triggered by Cloud Scheduler (for example nightly), with
`args: ["cleanup"]`. It needs the same database env vars as the service. This
is better than an in-process timer because the service is pinned to one
instance for in-memory sessions and should not spend CPU on maintenance.
A later optional `retention.schedule` for non-Cloud-Run deployments is out of
scope for the first version.

## Deletion algorithm

For each table, repeat until no rows match or `max_runtime` elapses:

```sql
DELETE FROM check_runs
WHERE id IN (
  SELECT id FROM check_runs
  WHERE completed_at < $1
  ORDER BY completed_at
  LIMIT $2
  FOR UPDATE SKIP LOCKED
);
```

- Each batch is its own short transaction, followed by `batch_pause`.
- `completed_at < cutoff` uses `idx_check_runs_completed`; `workflow_runs`
  uses `idx_workflow_runs_created_at`. Rows with a NULL timestamp are never
  deleted by age (log the count so operators notice).
- `SKIP LOCKED` avoids contention with webhook upserts of the same rows.
- Log per batch: table, rows deleted, total, elapsed. Final summary line is the
  machine-readable result.
- Take a PostgreSQL advisory lock (`pg_try_advisory_lock`) so two cleanup runs
  (or a cleanup and a future migration) cannot overlap; exit 0 with a message
  if the lock is held.
- Cancel cleanly on SIGINT/SIGTERM between batches.

`webhook_deliveries` (the durable inbox added in #142) is pruned by `state` and
`completed_at`/`received_at`: never delete `pending` or `processing` rows, and
keep `failed` rows longer because they hold payloads and errors for diagnosis.
It is deleted in batches with the same algorithm; index `state, received_at` if
the table is large.

Order: delete `check_runs` and `workflow_runs` independently; there are no
foreign keys between them. `workflow_jobs` references `workflow_runs`
(`workflow_run_id`) but is unused today; if it is ever populated, delete its
rows first or add `ON DELETE CASCADE`.

### Existing 20 M-row database

The first run would delete millions of rows. Recommendations:

1. Run `--dry-run` first and review counts.
2. Use a large `--max-runtime` or several runs; expect heavy WAL and replica
   or backup churn. Point-in-time recovery is enabled, so deletes are
   recoverable only within the PITR window; take a backup first.
3. Space is not returned to the OS by `DELETE`. Plain `VACUUM` marks space
   reusable (enough to stop growth). To shrink files, use `pg_repack`
   (not available on all Cloud SQL tiers) or accept the reuse. Do not use
   `VACUUM FULL` on a live instance (exclusive lock).
4. For a very large one-time purge, an alternative is time-based partitioning
   (see below), which makes old data a `DROP PARTITION`.

## Interaction with existing behaviour

- **Webhook ingestion** keeps upserting by primary key. A delayed delivery for a
  run older than the cutoff would re-insert a row that the next cleanup removes
  again. Acceptable; optionally skip inserts older than the retention window in
  the handlers so they are not stored at all.
- **Installation tombstones / removal** already delete a repository's dependent
  runs; retention is orthogonal.
- **Caching** (`cache.stats_ttl`): results can be stale for up to the TTL after
  a cleanup; no invalidation needed.
- **Covering indexes** (`idx_check_runs_flaky`, `idx_workflow_runs_repo_stats`)
  shrink as rows are vacuumed; index-only scans depend on the visibility map,
  so the `--vacuum` step (or autovacuum) matters after large deletes.

## Open decisions

1. **All-time repository statistics.** `GetRepositories` aggregates all-time
   `workflow_runs` and the README documents that contract. Deleting old runs
   changes these numbers. Options:
   - (a) Accept it and document that "all-time" means "within retention".
   - (b) Maintain a small `repository_stats` rollup (per repository: total,
     successful, failed, skipped, cancelled, eligible), updated by the webhook
     handlers and by cleanup (add deleted counts before deleting). Keeps the
     all-time numbers exact and makes the Repositories query O(repositories).
   - Recommendation: (b), since it also removes the last full-table aggregate.
2. **Retention defaults.** Disabled by default (safe) versus a default such as
   `check_runs: 90d`. Recommendation: disabled by default, documented example
   in `config/production.yml`.
3. **Partitioning instead of deletes.** Range-partition `check_runs` and
   `workflow_runs` by month; cleanup drops old partitions (instant, no bloat).
   Requires a migration of existing data, a composite primary key including the
   partition column, and changes to the `ON CONFLICT (id)` upserts. Larger
   change; consider only if delete-based cleanup proves too slow.
4. **Export before delete** (for example to GCS/BigQuery) if long-term analysis
   becomes a requirement.

## Implementation plan

1. Config: `RetentionConfig`, defaults, env overrides, validation, example
   config entries (`config/dashboard.example.yml`, `config/production.yml`),
   README section.
2. `server/retention.go`: `Cleanup(ctx, db, opts) (Result, error)` with the batch
   loop, advisory lock, dry run and result struct. Unit tests with a Postgres
   test schema via `internal/testdb` (seed old and new rows; assert only old
   rows are removed, batching, resumability, dry run, lock contention, context
   cancel).
3. `main.go`: subcommand dispatch (`serve` default, `cleanup`), flag parsing,
   exit codes. Keep the existing `-config` behaviour working unchanged.
4. If open decision 1(b) is chosen: `repository_stats` table, handler updates,
   backfill during the first cleanup, and switch `GetRepositories` to it.
5. Deployment (local iac only): Cloud Run Job + Cloud Scheduler for
   `cleanup`, sharing the service account and database secrets; run `--dry-run`
   manually first.
6. Docs: README (retention config and CLI), `docs/TESTING.md` (how to run the
   retention tests).

## Verification checklist

- [ ] `--dry-run` reports counts and deletes nothing.
- [ ] Rows newer than the cutoff are untouched; NULL timestamps are untouched.
- [ ] Webhook ingestion and dashboard queries stay responsive during a run
      (batch pause, short transactions).
- [ ] Interrupted run resumes and finishes on the next run.
- [ ] Two concurrent runs: the second exits cleanly.
- [ ] After a large delete, `VACUUM (ANALYZE)` restores index-only scans for the
      Flaky Tests and Repositories queries (check with `EXPLAIN`).
