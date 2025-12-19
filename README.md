# GitHub Actions Dashboard

A modern dashboard for monitoring GitHub Actions workflows across multiple repositories using GitHub Apps.

## Features

- **GitHub App Integration**: Receives real-time webhook events from GitHub
- **Multi-Repository Support**: Track workflows across all repositories where the app is installed
- **Database Storage**: SQLite for local development, PostgreSQL for production
- **Cloud Run Ready**: Optimized for deployment on GCP Cloud Run with Cloud SQL
- **Real-time Updates**: WebSocket support for live dashboard updates (coming soon)

## Quick Start (Local Development)

```bash
# 1. Build
go build -o dashboard .

# 2. Run with SQLite
./dashboard -config config/local.yml
```

Server starts on `http://localhost:8080`. SQLite database (`dashboard.db`) is created automatically.

**Health check**: `curl http://localhost:8080/api/health`

## Configuration

Example `config/local.yml`:
```yaml
server:
  address: "0.0.0.0"
  port: 8080

database:
  driver: "sqlite"  # Uses file:dashboard.db by default

logging:
  level: "info"
  text: true  # Pretty console output
```

### Database Options

**SQLite** (default for local dev):
```yaml
database:
  driver: "sqlite"
  url: "file:dashboard.db"
  # Or in-memory: url: ":memory:"
```

**PostgreSQL**:
```yaml
database:
  driver: "postgres"
  url: "postgres://user:pass@localhost:5432/dashboard?sslmode=disable"
```

**Cloud SQL** (auto-configured via env vars):
```bash
export DASHBOARD_DATABASE_CLOUD_SQL_INSTANCE=project:region:instance
export DASHBOARD_DATABASE_NAME=dashboard
export DASHBOARD_DATABASE_USER=dashboard-user
export DASHBOARD_DATABASE_PASSWORD=secret
```

## GitHub App Setup

1. **Create GitHub App**: Settings → Developer settings → GitHub Apps
   - Webhook URL: `https://your-domain.com/api/github/hook`
   - Permissions: Actions (Read), Checks (Read), Metadata (Read)
   - Events: `workflow_run`

2. **Configure**:
```bash
export DASHBOARD_GITHUB_APP_INTEGRATION_ID=123456
export DASHBOARD_GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"
export DASHBOARD_GITHUB_APP_WEBHOOK_SECRET=your-secret
```

## Deployment (GCP Cloud Run)

```bash
# Setup Cloud SQL
./deploy/cloud-run/setup-cloudsql.sh

# Setup GitHub App secrets
./deploy/cloud-run/setup-secrets.sh

# Deploy
./deploy/cloud-run/deploy.sh
```

See deployment scripts in `deploy/cloud-run/` for details.

## API Endpoints

- `GET /` - Dashboard UI (basic placeholder)
- `POST /api/github/hook` - GitHub webhook endpoint
- `GET /api/health` - Health check
- `GET /api/repositories` - List repositories
- `GET /api/workflows` - List workflow runs

## Database Schema

Auto-created on startup:

- **repositories**: GitHub repo metadata
- **workflow_runs**: Workflow execution data
- **workflow_jobs**: Individual job results

View schema:
```bash
sqlite3 dashboard.db ".schema"
```

## Project Structure

```
├── main.go                     # Entry point
├── server/
│   ├── config.go              # Configuration
│   ├── server.go              # HTTP server
│   ├── db.go                  # Database setup
│   └── handler/               # Webhook handlers
├── config/                    # Config examples
├── deploy/cloud-run/          # GCP deployment
└── scripts/                   # Helper scripts
```

## Development Status

✅ **Completed** (Phase 1-2):
- GitHub App webhook integration
- Database layer (SQLite + PostgreSQL)
- Cloud Run deployment infrastructure
- Workflow run event handling

🚧 **In Progress** (Phase 3-4):
- Modern UI with HTMX + Tailwind
- WebSocket real-time updates
- Additional event handlers

See [REFACTORING_PLAN.md](REFACTORING_PLAN.md) for full roadmap.

## License

Apache License 2.0
