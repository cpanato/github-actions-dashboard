# GitHub Actions Dashboard Refactoring Plan

## Overview

This document outlines a comprehensive refactoring strategy for the GitHub Actions Dashboard, inspired by [palantir/policy-bot](https://github.com/palantir/policy-bot)'s architecture.

## Current State Analysis

**What you have:**
- Simple Go web server with form-based input
- OAuth2 token authentication (personal access token)
- In-memory caching (15-minute TTL)
- Basic Bootstrap UI showing workflow runs from last 12 hours
- Polling-based data fetching (no webhooks)
- No persistent storage

**Pain Points:**
- No real-time updates
- Limited to 12-hour window
- No multi-repo support
- Basic UI with limited interactivity
- No historical data
- Manual repo input required each time

---

## Strategic Refactoring Approach

### Phase 1: GitHub App Foundation (Weeks 1-2)

#### 1.1 GitHub App Setup

**Create a GitHub App** with these permissions:

**Repository Permissions:**
| Permission | Access | Reason |
|------------|--------|---------|
| Actions | Read-only | Read workflow run data |
| Contents | Read-only | Access repository metadata |
| Metadata | Read-only | Basic repository information |

**Events to Subscribe:**
- `workflow_run` (primary event for CI updates)
- `check_run` (optional: for check status)
- `status` (optional: for commit statuses)

#### 1.2 Implement GitHub App Authentication

Use the `palantir/go-githubapp` library (same as policy-bot):

```go
// server/server.go
import (
    "github.com/palantir/go-baseapp/baseapp"
    "github.com/palantir/go-githubapp/githubapp"
)

func New(c *Config) (*Server, error) {
    // Create GitHub App client
    cc, err := githubapp.NewDefaultCachingClientCreator(
        c.Github,
        githubapp.WithClientUserAgent("actions-dashboard/1.0"),
        githubapp.WithClientCaching(true, func() httpcache.Cache {
            return lrucache.New(maxSize, 0)
        }),
    )
    
    // Create event dispatcher for webhooks
    dispatcher := githubapp.NewEventDispatcher(
        []githubapp.EventHandler{
            &handler.WorkflowRun{Base: baseHandler},
            &handler.Installation{Base: baseHandler},
        },
        c.Github.App.WebhookSecret,
    )
    
    return &Server{...}, nil
}
```

#### 1.3 Webhook Handler Implementation

```go
// server/handler/workflow_run.go
type WorkflowRun struct {
    Base
}

func (h *WorkflowRun) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
    var event github.WorkflowRunEvent
    json.Unmarshal(payload, &event)
    
    if event.GetAction() != "completed" {
        return nil
    }
    
    // Store in database
    h.StoreWorkflowRun(event)
    
    // Broadcast via WebSocket
    h.BroadcastUpdate(event)
    
    return nil
}
```

---

### Phase 2: Database Layer (Weeks 2-3)

#### 2.1 Database Schema

```sql
-- SQLite/PostgreSQL compatible

CREATE TABLE repositories (
    id INTEGER PRIMARY KEY,
    github_id INTEGER UNIQUE NOT NULL,
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    installation_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_repos_owner_name ON repositories(owner, name);
CREATE INDEX idx_repos_installation ON repositories(installation_id);

CREATE TABLE workflow_runs (
    id INTEGER PRIMARY KEY,
    github_id INTEGER UNIQUE NOT NULL,
    repository_id INTEGER NOT NULL,
    workflow_name TEXT NOT NULL,
    workflow_path TEXT,
    head_branch TEXT,
    head_sha TEXT NOT NULL,
    run_number INTEGER NOT NULL,
    event TEXT NOT NULL,
    status TEXT NOT NULL,
    conclusion TEXT,
    html_url TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE
);

CREATE INDEX idx_runs_repo ON workflow_runs(repository_id);
CREATE INDEX idx_runs_created ON workflow_runs(created_at DESC);
CREATE INDEX idx_runs_status ON workflow_runs(status, conclusion);
CREATE INDEX idx_runs_sha ON workflow_runs(head_sha);
CREATE INDEX idx_runs_workflow ON workflow_runs(workflow_name);

CREATE TABLE workflow_jobs (
    id INTEGER PRIMARY KEY,
    github_id INTEGER UNIQUE NOT NULL,
    workflow_run_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    conclusion TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    html_url TEXT,
    
    FOREIGN KEY (workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_jobs_run ON workflow_jobs(workflow_run_id);
```

#### 2.2 Database Access Layer

```go
// storage/database.go
type Database struct {
    db *sql.DB
}

func NewDatabase(url string) (*Database, error) {
    db, err := sql.Open("sqlite3", url) // or "postgres"
    if err != nil {
        return nil, err
    }
    
    // Run migrations
    if err := runMigrations(db); err != nil {
        return nil, err
    }
    
    return &Database{db: db}, nil
}

func (d *Database) StoreWorkflowRun(ctx context.Context, run *WorkflowRun) error {
    // Upsert repository
    repoID, err := d.upsertRepository(ctx, run.Repository)
    if err != nil {
        return err
    }
    
    // Insert/update workflow run
    _, err = d.db.ExecContext(ctx, `
        INSERT INTO workflow_runs (
            github_id, repository_id, workflow_name, head_sha,
            run_number, event, status, conclusion, html_url,
            created_at, updated_at, completed_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(github_id) DO UPDATE SET
            status = excluded.status,
            conclusion = excluded.conclusion,
            updated_at = excluded.updated_at,
            completed_at = excluded.completed_at
    `, run.ID, repoID, run.Name, run.HeadSHA, ...)
    
    return err
}

func (d *Database) GetRecentWorkflows(ctx context.Context, opts QueryOptions) ([]WorkflowRun, error) {
    // Query with filters
    query := `
        SELECT w.*, r.owner, r.name
        FROM workflow_runs w
        JOIN repositories r ON w.repository_id = r.id
        WHERE w.created_at >= ?
    `
    
    if opts.Repository != "" {
        query += " AND r.owner || '/' || r.name = ?"
    }
    
    query += " ORDER BY w.created_at DESC LIMIT ?"
    
    rows, err := d.db.QueryContext(ctx, query, time.Now().Add(-opts.TimeWindow), opts.Limit)
    // ... process rows
}
```

---

### Phase 3: Modern UI (Weeks 3-4)

#### 3.1 Technology Choice: **HTMX + Tailwind CSS**

**Why this stack (inspired by policy-bot):**
- **HTMX**: Hypermedia-driven interactions, no heavy JavaScript framework
- **Tailwind CSS**: Utility-first styling, matches policy-bot approach
- **Go Templates**: Server-side rendering with partials
- **Alpine.js**: Minimal JS for local interactions

**Benefits:**
- Simple to maintain
- Fast page loads
- Progressive enhancement
- Works without JavaScript (graceful degradation)

#### 3.2 UI Structure

```
server/
├── assets/
│   ├── css/
│   │   └── main.css         # Tailwind + custom styles
│   ├── js/
│   │   ├── filter.js        # Dashboard filtering
│   │   └── realtime.js      # WebSocket handling
│   └── img/
└── templates/
    ├── base.html.tmpl       # Base layout
    ├── dashboard.html.tmpl  # Main dashboard
    ├── repository.html.tmpl # Single repo view
    └── partials/
        ├── workflow_row.html.tmpl
        └── status_badge.html.tmpl
```

#### 3.3 Dashboard UI Features

```html
<!-- templates/dashboard.html.tmpl -->
{{define "dashboard"}}
<div class="container mx-auto px-4 py-8">
    <!-- Filters -->
    <div class="mb-6 flex gap-4">
        <select hx-get="/api/dashboard" 
                hx-trigger="change" 
                hx-target="#workflow-grid"
                name="repository"
                class="px-4 py-2 border rounded">
            <option value="">All Repositories</option>
            {{range .Repositories}}
            <option value="{{.FullName}}">{{.FullName}}</option>
            {{end}}
        </select>
        
        <select name="status" 
                hx-get="/api/dashboard" 
                hx-trigger="change" 
                hx-target="#workflow-grid">
            <option value="">All Statuses</option>
            <option value="success">Success</option>
            <option value="failure">Failure</option>
            <option value="in_progress">In Progress</option>
        </select>
        
        <input type="text" 
               name="workflow"
               placeholder="Filter workflows..."
               hx-get="/api/dashboard"
               hx-trigger="keyup changed delay:500ms"
               hx-target="#workflow-grid">
    </div>
    
    <!-- Workflow Grid -->
    <div id="workflow-grid" 
         hx-get="/api/dashboard" 
         hx-trigger="load"
         ws-connect="/ws/updates">
        <!-- Grid rows loaded here -->
    </div>
</div>
{{end}}
```

#### 3.4 Matrix/Grid View

```html
<!-- Show workflows × commits -->
<div class="overflow-x-auto">
    <table class="min-w-full border-collapse">
        <thead>
            <tr>
                <th class="sticky left-0 bg-white">Workflow</th>
                {{range .RecentCommits}}
                <th class="px-2 py-1 text-xs">
                    <a href="{{.URL}}" class="text-blue-600 hover:underline">
                        {{.ShortSHA}}
                    </a>
                </th>
                {{end}}
            </tr>
        </thead>
        <tbody>
            {{range .Workflows}}
            <tr class="border-t">
                <td class="sticky left-0 bg-white font-medium">
                    {{.Name}}
                </td>
                {{range .Runs}}
                <td class="px-2 py-1 text-center">
                    <a href="{{.URL}}" 
                       class="inline-block w-6 h-6 rounded status-{{.Conclusion}}"
                       title="{{.Status}}: {{.Conclusion}}">
                    </a>
                </td>
                {{end}}
            </tr>
            {{end}}
        </tbody>
    </table>
</div>
```

---

### Phase 4: Real-time Updates (Week 4)

#### 4.1 WebSocket Implementation

```go
// server/handler/websocket.go
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
}

func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.clients[client] = true
            
        case client := <-h.unregister:
            delete(h.clients, client)
            close(client.send)
            
        case message := <-h.broadcast:
            for client := range h.clients {
                select {
                case client.send <- message:
                default:
                    close(client.send)
                    delete(h.clients, client)
                }
            }
        }
    }
}

// Broadcast workflow update
func (h *Hub) BroadcastWorkflowUpdate(run *WorkflowRun) {
    data, _ := json.Marshal(run)
    h.broadcast <- data
}
```

#### 4.2 Frontend WebSocket Handler

```javascript
// assets/js/realtime.js
const ws = new WebSocket('ws://localhost:8080/ws/updates');

ws.onmessage = (event) => {
    const run = JSON.parse(event.data);
    
    // Update UI element using HTMX
    htmx.ajax('GET', `/api/workflow/${run.id}`, {
        target: `#workflow-${run.id}`,
        swap: 'outerHTML'
    });
};
```

---

### Phase 5: Advanced Features (Weeks 5-6)

#### 5.1 Multi-Repository Dashboard

```go
// List all repositories with recent activity
func (h *Dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    installationID := h.getInstallationFromSession(r)
    
    repos, err := h.DB.GetRepositoriesForInstallation(ctx, installationID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    
    // Get workflow summary for each repo
    summaries := make(map[string]*WorkflowSummary)
    for _, repo := range repos {
        summary, _ := h.DB.GetWorkflowSummary(ctx, repo.ID, 24*time.Hour)
        summaries[repo.FullName] = summary
    }
    
    data := struct {
        Repositories []Repository
        Summaries    map[string]*WorkflowSummary
    }{repos, summaries}
    
    h.Templates.ExecuteTemplate(w, "dashboard.html.tmpl", data)
}
```

#### 5.2 Filtering & Search

```go
type QueryOptions struct {
    Repository  string
    Workflow    string
    Status      string
    Conclusion  string
    Branch      string
    TimeWindow  time.Duration
    Limit       int
}

func (d *Database) QueryWorkflows(ctx context.Context, opts QueryOptions) ([]WorkflowRun, error) {
    query := `SELECT * FROM workflow_runs WHERE 1=1`
    args := []interface{}{}
    
    if opts.Repository != "" {
        query += " AND repository_id = (SELECT id FROM repositories WHERE owner || '/' || name = ?)"
        args = append(args, opts.Repository)
    }
    
    if opts.Workflow != "" {
        query += " AND workflow_name LIKE ?"
        args = append(args, "%"+opts.Workflow+"%")
    }
    
    // ... more filters
    
    return d.executeQuery(ctx, query, args...)
}
```

#### 5.3 Flaky Test Detection

```go
// Identify workflows with inconsistent results
func (d *Database) GetFlakyWorkflows(ctx context.Context, threshold float64) ([]FlakyWorkflow, error) {
    query := `
        WITH workflow_stats AS (
            SELECT 
                workflow_name,
                repository_id,
                COUNT(*) as total_runs,
                SUM(CASE WHEN conclusion = 'failure' THEN 1 ELSE 0 END) as failures,
                SUM(CASE WHEN conclusion = 'success' THEN 1 ELSE 0 END) as successes
            FROM workflow_runs
            WHERE created_at >= datetime('now', '-7 days')
            GROUP BY workflow_name, repository_id
            HAVING total_runs >= 10
        )
        SELECT *,
            CAST(failures AS FLOAT) / total_runs as failure_rate
        FROM workflow_stats
        WHERE failure_rate > ? AND failure_rate < 0.9
        ORDER BY failure_rate DESC
    `
    
    rows, err := d.db.QueryContext(ctx, query, threshold)
    // ...
}
```

#### 5.4 Trend Analysis

```go
// Show success rate trends over time
func (d *Database) GetWorkflowTrends(ctx context.Context, workflowName string, days int) (*TrendData, error) {
    query := `
        SELECT 
            DATE(created_at) as date,
            COUNT(*) as total,
            SUM(CASE WHEN conclusion = 'success' THEN 1 ELSE 0 END) as successes,
            AVG(CASE WHEN completed_at IS NOT NULL 
                THEN (julianday(completed_at) - julianday(created_at)) * 24 * 60 
                END) as avg_duration_minutes
        FROM workflow_runs
        WHERE workflow_name = ?
            AND created_at >= datetime('now', '-' || ? || ' days')
        GROUP BY DATE(created_at)
        ORDER BY date
    `
    
    rows, err := d.db.QueryContext(ctx, query, workflowName, days)
    // ...
}
```

---

## Deployment Architecture

### Docker Deployment

```dockerfile
# docker/Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o dashboard ./cmd/server

FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite

WORKDIR /app
COPY --from=builder /build/dashboard .
COPY config/dashboard.example.yml /config/dashboard.yml
COPY server/templates ./templates
COPY build/static ./static

EXPOSE 8080

CMD ["./dashboard", "server", "--config", "/config/dashboard.yml"]
```

### Kubernetes Deployment

```yaml
# deploy/kubernetes/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: actions-dashboard
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: dashboard
        image: ghcr.io/cpanato/actions-dashboard:latest
        ports:
        - containerPort: 8080
        env:
        - name: DASHBOARD_GITHUB_APP_PRIVATE_KEY
          valueFrom:
            secretKeyRef:
              name: github-app
              key: private-key
        - name: DASHBOARD_DATABASE_URL
          value: "postgres://user:pass@postgres:5432/dashboard"
        volumeMounts:
        - name: config
          mountPath: /config
      volumes:
      - name: config
        configMap:
          name: dashboard-config
---
apiVersion: v1
kind: Service
metadata:
  name: actions-dashboard
spec:
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: actions-dashboard
```

---

## Migration Strategy

### Option A: Incremental Migration (Recommended)

1. **Week 1**: Keep existing code, add GitHub App alongside OAuth
2. **Week 2**: Add database layer, populate from webhooks
3. **Week 3**: Build new UI components, serve alongside old
4. **Week 4**: Add real-time features
5. **Week 5**: Switch default to new UI
6. **Week 6**: Remove old code

### Option B: Parallel Development

1. Create new repo/branch for refactored version
2. Run both versions simultaneously during transition
3. Gradually migrate users
4. Deprecate old version after validation

---

## File Structure (Complete)

```
github-actions-dashboard/
├── cmd/
│   └── server/
│       └── main.go
├── server/
│   ├── server.go
│   ├── config.go
│   ├── handler/
│   │   ├── base.go
│   │   ├── workflow_run.go
│   │   ├── check_run.go
│   │   ├── installation.go
│   │   ├── dashboard.go
│   │   ├── repository.go
│   │   ├── api.go
│   │   ├── health.go
│   │   ├── websocket.go
│   │   └── frontend.go
│   ├── templates/
│   │   ├── base.html.tmpl
│   │   ├── dashboard.html.tmpl
│   │   ├── repository.html.tmpl
│   │   ├── trends.html.tmpl
│   │   └── partials/
│   │       ├── workflow_row.html.tmpl
│   │       ├── status_badge.html.tmpl
│   │       └── filters.html.tmpl
│   └── assets/
│       ├── css/
│       │   └── main.css
│       ├── js/
│       │   ├── filter.js
│       │   └── realtime.js
│       └── img/
├── storage/
│   ├── database.go
│   ├── migrations/
│   │   ├── 001_initial.sql
│   │   ├── 002_add_jobs.sql
│   │   └── 003_add_indexes.sql
│   └── queries.go
├── dashboard/
│   ├── models.go
│   ├── aggregator.go
│   └── analyzer.go
├── config/
│   └── dashboard.example.yml
├── docker/
│   └── Dockerfile
├── deploy/
│   ├── kubernetes/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── ingress.yaml
│   └── helm/
├── go.mod
├── go.sum
├── Makefile
├── tailwind.config.js
├── package.json
└── README.md
```

---

## Dependencies to Add

```go
// go.mod additions
require (
    github.com/palantir/go-baseapp/baseapp v0.5.0
    github.com/palantir/go-githubapp/githubapp v0.21.0
    github.com/gorilla/websocket v1.5.1
    github.com/mattn/go-sqlite3 v1.14.18  // or
    github.com/lib/pq v1.10.9             // for PostgreSQL
    github.com/pressly/goose/v3 v3.17.0   // migrations
)
```

```json
// package.json
{
  "devDependencies": {
    "tailwindcss": "^3.4.0",
    "autoprefixer": "^10.4.16",
    "postcss": "^8.4.32",
    "htmx.org": "^1.9.10",
    "alpinejs": "^3.13.3"
  }
}
```

---

## Next Steps

1. **Review this plan** and decide on scope
2. **Set up GitHub App** in your organization
3. **Choose database** (SQLite for simple, PostgreSQL for production)
4. **Start with Phase 1** (GitHub App integration)
5. **Implement incrementally** with working software at each phase

Would you like me to:
1. Implement any specific phase in detail?
2. Create the complete server initialization code?
3. Build out the database layer?
4. Create the modernized UI templates?
