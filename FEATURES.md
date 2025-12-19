# GitHub Actions Dashboard - Features

## Overview
A modern GitHub App that monitors and analyzes GitHub Actions workflows across multiple repositories, providing real-time updates and advanced analytics.

## Core Features

### 1. GitHub App Integration
- Built using `palantir/go-githubapp` framework
- Webhook-driven architecture for real-time updates
- Secure GitHub App authentication
- Support for multiple repositories

### 2. Multi-Event Support
The dashboard monitors three types of GitHub webhook events:

#### Workflow Run Events (`workflow_run`)
- Tracks workflow executions across all repositories
- Stores workflow metadata: name, status, conclusion, branch, commit info
- Calculates success rates and statistics

#### Check Run Events (`check_run`)
- Monitors individual check/test executions
- Tracks test-level failures and successes
- Essential for flaky test detection

#### Installation Events (`installation`, `installation_repositories`)
- Automatically tracks when the app is installed/uninstalled
- Monitors repository additions and removals
- Updates repository catalog in real-time

### 3. Database Support
Dual database support for different environments:

#### SQLite (Local Development)
- No configuration needed
- Auto-creates schema on startup
- File-based storage: `./dashboard.db`
- Perfect for testing and development

#### PostgreSQL (Production)
- Production-ready with Cloud SQL support
- Unix socket connections for GCP
- Connection pooling
- Auto-schema initialization

Database Schema:
- `repositories`: Repository metadata and ownership
- `workflow_runs`: Complete workflow execution history
- `workflow_jobs`: Individual job information
- `check_runs`: Test/check execution details

### 4. Real-Time Updates
WebSocket integration for instant dashboard updates:
- Automatic connection and reconnection
- Broadcasts on workflow completion
- Broadcasts on check run completion
- Broadcasts on repository changes
- Zero-config client implementation

### 5. REST API

#### Repository Endpoints
```
GET /api/repositories
GET /api/repositories?include_stats=true
```
Returns repository list with optional statistics (total runs, success rate, failed runs).

#### Workflow Endpoints
```
GET /api/workflows
GET /api/workflows?repository_id=123
GET /api/workflows?status=completed
GET /api/workflows?conclusion=success
GET /api/workflows?since=2024-01-01T00:00:00Z
GET /api/workflows?limit=20
```
Powerful filtering for workflow runs with pagination.

#### Analytics Endpoints
```
GET /api/analytics?days=7
GET /api/analytics?repository_id=123&days=30
```
Returns overall statistics: total runs, success rate.

```
GET /api/flaky-tests?days=30&min_runs=5
GET /api/flaky-tests?repository_id=123&min_runs=10
```
Identifies tests with intermittent failures:
- Requires minimum number of runs
- Calculates flake rate (ratio of failures to total runs)
- Filters tests that have both successes AND failures
- Ordered by flake rate

```
GET /api/trends?days=30
GET /api/trends?repository_id=123&days=7
```
Returns daily success rate trends for charting.

### 6. Advanced Analytics

#### Flaky Test Detection
Algorithm:
1. Queries check_runs for tests with BOTH successes and failures
2. Requires minimum number of runs (default: 5)
3. Calculates flake rate: `failures / (successes + failures)`
4. Returns top 20 flaky tests ordered by flake rate

Benefits:
- Identify unreliable tests early
- Focus engineering effort on stability
- Track improvement over time

#### Success Rate Trends
- Daily aggregation of workflow results
- Calculates success percentage per day
- Enables trend visualization
- Helps identify degrading quality

### 7. Modern UI

Built with:
- **HTMX 1.9.10**: Dynamic content loading without heavy JavaScript
- **Tailwind CSS**: Modern, responsive design
- **WebSocket**: Real-time updates
- **No build step**: Runs directly from HTML/JS

Dashboard sections:
1. **Analytics Overview**: 7-day success rate, flaky test count, trend indicator
2. **Flaky Tests**: Table of unreliable tests with flake rates
3. **Repositories**: Cards showing repository stats with color-coded success rates
4. **Recent Workflows**: Table of recent workflow runs with status badges

### 8. Configuration

Configuration file (`config/local.yml` or `config/production.yml`):
```yaml
server:
  address: ":8080"

github:
  app:
    integration_id: 123456
    webhook_secret: "your-webhook-secret"
    private_key: |
      -----BEGIN RSA PRIVATE KEY-----
      ...
      -----END RSA PRIVATE KEY-----

database:
  driver: "sqlite"  # or "postgres"
  # SQLite
  path: "./dashboard.db"
  # PostgreSQL
  host: "/cloudsql/project:region:instance"
  port: 5432
  name: "dashboard"
  user: "postgres"
  password: "secret"
  ssl_mode: "disable"

websocket:
  enabled: true
```

Environment variable overrides:
- `DASHBOARD_DATABASE_DRIVER`
- `DASHBOARD_DATABASE_HOST`
- `DASHBOARD_DATABASE_PORT`
- `DASHBOARD_DATABASE_NAME`
- `DASHBOARD_DATABASE_USER`
- `DASHBOARD_DATABASE_PASSWORD`

### 9. Deployment

#### Local Development
```bash
# Configure SQLite
cp config/local.yml config.yml
go run . --config config.yml

# Access at http://localhost:8080
```

#### GCP Cloud Run
```bash
# Build and deploy
make docker-build
make deploy

# Automatically connects to Cloud SQL via Unix socket
# Secrets managed via Secret Manager
```

Features:
- Auto-scaling based on load
- HTTPS by default
- Cloud SQL connection via Unix socket
- Secret management via Secret Manager

### 10. Security

- GitHub App authentication (not OAuth tokens)
- Webhook signature verification
- Secret management for production
- Database connection encryption (SSL/TLS)
- No sensitive data in logs

## Architecture

```
┌─────────────┐
│   GitHub    │
│   Webhooks  │
└──────┬──────┘
       │
       ▼
┌─────────────────────────────┐
│   go-githubapp Handler      │
│   - workflow_run            │
│   - check_run               │
│   - installation            │
└──────┬──────────────────────┘
       │
       ├──────► Database (SQLite/PostgreSQL)
       │
       └──────► WebSocket Hub ──► Browser Clients
                      │
                      ▼
              ┌───────────────┐
              │  HTMX + JS    │
              │  Dashboard    │
              └───────────────┘
```

## Use Cases

1. **CI/CD Monitoring**: Track workflow success across all repositories
2. **Quality Assurance**: Identify flaky tests impacting reliability
3. **Team Metrics**: Measure build success rates and trends
4. **Incident Response**: Quickly identify failing workflows
5. **Historical Analysis**: Query workflow history with powerful filters

## Future Enhancements

Potential additions:
- Notification system (Slack, email)
- Custom alerts and thresholds
- Workflow duration tracking
- Cost analysis (Actions minutes)
- Team/user analytics
- Custom dashboards
- Export capabilities (CSV, JSON)
- Advanced charting (Chart.js, D3.js)
