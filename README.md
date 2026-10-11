# GitHub Actions Dashboard

Monitor GitHub Actions across repositories installed in a GitHub App. The shared
dashboard provides repository statistics, workflow history, intermittently failing
checks and a workflow-by-commit matrix with branch, workflow and event filters.

![Dashboard overview](assets/home.png)
![Repository workflow matrix](assets/dashboard.png)

## Local demo

Requires Go 1.27.2 and Docker Compose. Frontend assets are checked in, so running
the app does not require Node or a CDN connection.

```bash
docker compose up -d --wait postgres
go run . -config config/demo.yml
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). The demo seeds synthetic data
into PostgreSQL and disables OAuth. It refuses non-loopback binds and a configured
public URL. Use it only locally. Seeding is idempotent; restarting updates the
synthetic timestamps without duplicating rows.

Stop the app with Ctrl+C and PostgreSQL with `docker compose down`. The database
volume is retained. PostgreSQL is published only on `127.0.0.1:5432`; newly
initialized volumes require the configured `local-dev-password` for TCP access.
Existing volumes retain their `pg_hba.conf` authentication rules. If an older
volume used `trust`, change its TCP host rules to `scram-sha-256` and reload
PostgreSQL; removing the Compose setting does not update existing database files.

`scripts/dev-setup.sh demo` starts the database and builds a
binary, then prints the command to run it. `scripts/dev-setup.sh oauth` selects
`config/local.dev.yml` instead.

## Real GitHub data

Configure a GitHub App with Actions, Checks and Metadata read permissions and
subscribe to `workflow_run`, `check_run`, `installation` and
`installation_repositories`. Set the webhook URL to `/api/github/hook` and the
OAuth callback to `/login/callback` on the externally reachable dashboard URL.

```bash
export DASHBOARD_GITHUB_APP_INTEGRATION_ID=123456
export DASHBOARD_GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"
export DASHBOARD_GITHUB_APP_WEBHOOK_SECRET=your-webhook-secret
export DASHBOARD_GITHUB_OAUTH_CLIENT_ID=your-client-id
export DASHBOARD_GITHUB_OAUTH_CLIENT_SECRET=your-client-secret
export DASHBOARD_ALLOWED_USERS=your-github-login
# Alternatively/additionally: DASHBOARD_ALLOWED_ORGANIZATIONS=your-org
export DASHBOARD_PUBLIC_URL=https://your-dashboard.example

go run . -config config/local.dev.yml
```

Private-key configuration is PEM **contents**, not a filename. An explicit user
or organization allowlist is required outside demo mode. Allowed users can see
**every tracked repository, aggregate and realtime message**; there is no per-user
repository isolation. Organization membership is verified at login and remains
valid for the session lifetime. See [authentication](docs/AUTHENTICATION.md) for
permissions, cookies, access checks and session-store limits.

History is ingested from future webhooks; there is no initial workflow-history
backfill. Only completed workflow and check events are persisted. Repository
removal and app uninstall delete tracked data, including dependent runs/checks.
Installation tombstones prevent delayed completed events from recreating removed
data. Existing rows gain installation ownership on subsequent webhooks; uninstall
cannot identify historical rows that still lack an installation ID.

## Configuration and deployment

Use `config/dashboard.example.yml` as a reference, `config/local.dev.yml` for local
OAuth testing or `config/production.yml` for deployment. PostgreSQL is the only
implemented database driver. The schema is initialized automatically, including
additive installation-tracking migrations. Back up existing databases before
upgrading a deployed instance.

Relevant environment values:

| Variable | Purpose |
| --- | --- |
| `DASHBOARD_PUBLIC_URL` | Externally reachable URL for OAuth callbacks |
| `DASHBOARD_ALLOWED_USERS` | Comma-separated usernames allowed to view the shared dashboard |
| `DASHBOARD_ALLOWED_ORGANIZATIONS` | Comma-separated organizations whose active members may sign in |
| `DASHBOARD_DATABASE_URL` | PostgreSQL URL or lib/pq connection string |
| `DASHBOARD_DATABASE_CLOUD_SQL_INSTANCE` | Cloud SQL instance connection name |
| `DASHBOARD_DATABASE_NAME`, `DASHBOARD_DATABASE_USER`, `DASHBOARD_DATABASE_PASSWORD` | Cloud SQL database credentials |
| `DASHBOARD_ENV_PREFIX` | Replaces `DASHBOARD_` for supported credential/database/access/public-URL variables |

A database URL takes precedence over automatic Cloud SQL connection-string
construction. Cloud SQL uses `/cloudsql/<instance>` Unix sockets, which must be
provided by the deployment environment.

`options.default_time_window` must be a positive Go duration, such as `24h` or
`168h` (seven days); `7d` is invalid. `options.enable_realtime: false` disables the
WebSocket endpoint and browser connection. Database-backed routes return 503 until
schema initialization succeeds; background retries share the stable pool used by
webhook handlers. Shutdown cancels retries and closes realtime connections.

Use HTTPS and secure cookies in production. Sessions are held in memory, so
restarts invalidate them; multiple replicas need shared session storage or routing
affinity. Handled webhooks are acknowledged only after the verified payload is committed
to PostgreSQL. Delivery IDs deduplicate identical redeliveries; conflicting content
under the same ID returns 409. Missing IDs return 400, payloads above 10 MiB return
413, and persistence failures return 503. Four workers claim deliveries with
60-second leases and 30-second handler deadlines. Pending work and expired leases
resume after restart. Processing is at least once, so handlers must be idempotent.
Successful payloads are cleared, while IDs/hashes remain for deduplication; failed
payloads and errors remain in the inbox for diagnosis. Automatic retry of handler
failures is the next implementation step. Shutdown stops claims and finishes active
work before closing PostgreSQL; unfinished persisted work remains available to the
next process. See [webhook operations](docs/WEBHOOKS.md). Cloud Run
and other deployments must provision the database connection and secrets separately.
`make ko-local` builds a local image; the repository does not provide a deploy target.

## Pages and APIs

Dashboard pages and data APIs require an authorized session outside demo mode.
API requests return JSON; `HX-Request: true` returns HTML partials for repositories,
workflows, analytics and flaky checks. Trends always returns JSON.

| Route | Parameters / behavior |
| --- | --- |
| `/` | Repository overview, analytics and recent workflow runs |
| `/repository/{owner}/{repo}` | `workflow`, `branch`, `event`, `view=grid\|cards`, `limit` (commit columns; default 50, maximum 500) |
| `/api/repositories` | All tracked repositories with all-time statistics |
| `/api/workflows` | `repository_id`, `status`, `conclusion`, `since` (relative Go duration), `limit` (1–500; default 50), `offset` (0–100000; default 0) |
| `/api/analytics` | `repository_id`, `days` (1–365; default 30) |
| `/api/flaky-tests` | `repository_id`, `days` (1–365; default 30), `min_runs` (1–10000; default 5); top 20 results |
| `/api/trends` | `repository_id`, `days` (1–365; default 30); daily aggregates |
| `/api/health` | Public database readiness check, 200 or 503 |
| `/api/github/hook` | Public POST receiver; verifies GitHub webhook signatures |
| `/ws` | Authorized realtime connection when enabled |

Invalid API parameters return 400. `since` must be a positive Go duration;
`repository_id` must be a nonnegative integer. Workflow JSON includes `limit` and
`offset`; fetch the next page by adding `limit` to `offset`, stopping when fewer
than `limit` runs are returned. Runs sort by creation time and ID descending.
Offsets can shift as new webhooks arrive; this API does not provide snapshot isolation.

Omitting `repository_id` aggregates across all tracked repositories. Repository
statistics are all-time; analytics and trends use the requested date window.
Success rate is successful completed runs divided by completed runs with an
outcome, excluding skipped, cancelled, neutral and empty outcomes. Total runs also
includes unfinished runs. Flake rate is failed checks divided by successful plus
failed checks, expressed as a percentage. Results require both success and failure;
`last_failure` is the latest failing completion, not the latest successful check.
This is a check-level intermittency heuristic, not proof that individual tests flake.

## Development and testing

- `make test`: Go tests with the race detector; database cases skip without a test URL.
- `make test-integration`: PostgreSQL cases plus Go tests; requires a dedicated test database.
- `npm ci && make test-js`: frontend regressions, requiring Node 20 or newer.
- `make test-e2e`: Chromium against an isolated loopback demo; install Chromium first.
- `make assets`: regenerate checked-in HTMX and Tailwind assets from locked dependencies.
- `make lint`: golangci-lint.
- `make verify`: module verification, tidy check, vet and reachable vulnerability checks.

See [testing](docs/TESTING.md) for disposable-database setup, browser installation
and failure artifacts. CI runs Go, database, frontend and browser tests, checks
asset reproducibility and builds the ko image. Releases use GoReleaser, SBOMs and
cosign signatures; see [Releases and verification](#releases-and-verification). See the [improvement plan](docs/IMPROVEMENT_PLAN.md) for
review order and remaining project boundaries.

## Releases and verification

Releases are built by GoReleaser from `v*` tags. Each release publishes binary
archives, `checksums.txt`, a `checksums.txt.sigstore.json` signature bundle, and a
container image at `ghcr.io/cpanato/github-actions-dashboard` with an SBOM. The
image and checksums are signed with [cosign](https://github.com/sigstore/cosign)
keyless signing from the release workflow.

Verify the container image (replace `vX.Y.Z` with the release tag):

```bash
cosign verify ghcr.io/cpanato/github-actions-dashboard:vX.Y.Z \
  --certificate-identity-regexp '^https://github.com/cpanato/github-actions-dashboard/\.github/workflows/release\.yaml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Verify the release archives by checking the signed checksums, then the archives
against them:

```bash
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/cpanato/github-actions-dashboard/\.github/workflows/release\.yaml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

sha256sum --ignore-missing -c checksums.txt
```

## License

Apache License 2.0. The vendored HTMX script retains its MIT license alongside the
asset; compiled Tailwind CSS retains its upstream license header.
