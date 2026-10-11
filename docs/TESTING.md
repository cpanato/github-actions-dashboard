# Testing

Run `make test` for Go tests with the race detector, `make lint` for golangci-lint,
and `go vet ./...` for vet. Node 20 or newer is required for frontend tests;
CI uses Node 24. Install the pinned dependencies with `npm ci`, then run
`make test-js`.

## PostgreSQL integration tests

Tests skip database coverage unless `DASHBOARD_TEST_DATABASE_URL` is set. The
integration helper rejects non-loopback hosts and database names without an
`_test` suffix, creates a random schema per test and drops only that schema.
Use a dedicated disposable database, never an existing dashboard database:

```bash
docker run --rm -d --name dashboard-tests \
  -e POSTGRES_PASSWORD=test-password -e POSTGRES_DB=dashboard_test \
  -p 127.0.0.1:55432:5432 postgres:16-alpine
export DASHBOARD_TEST_DATABASE_URL='postgres://postgres:test-password@127.0.0.1:55432/dashboard_test?sslmode=disable'
make test-integration
```

Coverage includes analytics, lifecycle cascades and rollback, duplicate and
stale events, JSON/HTML responses, demo seed idempotence, signed/unsigned webhook
requests and actual WebSocket broadcasts. Allowlist tests use a local fake
GitHub API and cover membership failures without needing real credentials.

## Browser E2E tests

With the disposable database running:

```bash
npm ci
npx playwright install chromium
make test-e2e
```

The harness builds a temporary application binary, creates its own random
PostgreSQL schema and starts a demo instance at `127.0.0.1:18080`. It refuses to
reuse an existing server. It removes external DASHBOARD_* credentials from the
child process, seeds synthetic data, and removes its schema and temporary files
when stopped. Stop the test database with `docker stop dashboard-tests`.

Chromium tests assert error-free page execution, accurate summary cards,
repository navigation, combined/empty filters, grid/card state, signed workflow
updates preserving filters and check-completion bursts refreshing flaky data.
OAuth itself is covered by local API tests, not by browser login to GitHub.

CI runs the database tests and browser suite on every PR and push to main.
Failures retain screenshots, traces and the HTML report for seven days. Locally,
use `npx playwright show-report` or `npx playwright show-trace <trace.zip>`.

## Frontend assets

HTMX and compiled Tailwind CSS are served locally, so browser tests and dashboard
rendering do not need a CDN. The checked-in assets are regenerated with
`make assets`; CI checks that the generated output matches the committed files.
HTMX's MIT license is stored beside the vendored script. JavaScript is source;
`package-lock.json` pins the test and build dependencies.
