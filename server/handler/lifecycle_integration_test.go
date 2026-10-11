package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/cpanato/github-actions-dashboard/internal/testdb"
	"github.com/cpanato/github-actions-dashboard/server"
	"github.com/cpanato/github-actions-dashboard/server/handler"
)

func repository(id int64) map[string]any {
	return map[string]any{"id": id, "name": "repo", "full_name": "acme/repo", "owner": map[string]any{"login": "acme"}, "default_branch": "trunk"}
}
func installation(id int64) map[string]any {
	return map[string]any{"id": id, "account": map[string]any{"login": "acme"}}
}
func deliver(t *testing.T, h interface {
	Handle(context.Context, string, string, []byte) error
}, kind string, data map[string]any) {
	t.Helper()
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), kind, "test-delivery", payload); err != nil {
		t.Fatal(err)
	}
}
func workflow(repo map[string]any, id int64) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{"action": "completed", "installation": installation(99), "repository": repo,
		"workflow_run": map[string]any{"id": id, "workflow_id": 20, "name": "CI", "status": "completed", "conclusion": "success", "created_at": now, "updated_at": now, "run_started_at": now}}
}
func count(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("%s: got %d, want %d", query, n, want)
	}
}

func TestPostgresInstallationLifecycle(t *testing.T) {
	db, err := server.InitDatabase(&server.DatabaseConfig{Driver: "postgres", URL: testdb.URL(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	base := handler.Base{DB: db}
	installs := &handler.InstallationHandler{Base: base}
	workflows := &handler.WorkflowRunHandler{Base: base}
	checks := &handler.CheckRunHandler{Base: base}
	repo := repository(1)
	other := repository(2)
	other["name"] = "other"
	other["full_name"] = "acme/other"
	unrelated := repository(3)
	unrelated["name"] = "unrelated"
	unrelated["full_name"] = "acme/unrelated"
	created := map[string]any{"action": "created", "installation": installation(99), "repositories": []any{repo, other}}
	deliver(t, installs, "installation", created)
	deliver(t, installs, "installation", created)
	deliver(t, installs, "installation", map[string]any{"action": "created", "installation": installation(88), "repositories": []any{unrelated}})
	run := workflow(repo, 10)
	deliver(t, workflows, "workflow_run", run)
	deliver(t, workflows, "workflow_run", run)
	count(t, db, "SELECT COUNT(*) FROM workflow_runs", 1)
	old := workflow(repo, 10)
	oldRun := old["workflow_run"].(map[string]any)
	oldRun["conclusion"] = "failure"
	oldRun["updated_at"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	deliver(t, workflows, "workflow_run", old)
	count(t, db, "SELECT COUNT(*) FROM workflow_runs WHERE conclusion='success'", 1)
	now := time.Now().UTC().Format(time.RFC3339)
	check := map[string]any{"action": "completed", "installation": installation(99), "repository": repo,
		"check_run": map[string]any{"id": 30, "name": "tests", "status": "completed", "conclusion": "failure", "started_at": now, "completed_at": now}}
	deliver(t, checks, "check_run", check)
	deliver(t, checks, "check_run", check)
	if _, err := db.Exec(`INSERT INTO workflow_jobs (id,workflow_run_id,run_id,name) VALUES (1,10,10,'job')`); err != nil {
		t.Fatal(err)
	}
	removed := map[string]any{"action": "removed", "installation": installation(99), "repositories_removed": []any{repo}}
	deliver(t, installs, "installation_repositories", removed)
	deliver(t, installs, "installation_repositories", removed)
	count(t, db, "SELECT COUNT(*) FROM repositories", 2)
	count(t, db, "SELECT COUNT(*) FROM workflow_runs", 0)
	count(t, db, "SELECT COUNT(*) FROM check_runs", 0)
	count(t, db, "SELECT COUNT(*) FROM workflow_jobs", 0)
	deliver(t, workflows, "workflow_run", run)
	count(t, db, "SELECT COUNT(*) FROM repositories WHERE id=1", 0)
	deliver(t, installs, "installation_repositories", map[string]any{"action": "added", "installation": installation(99), "repositories_added": []any{repo}})
	deliver(t, workflows, "workflow_run", run)
	count(t, db, "SELECT COUNT(*) FROM workflow_runs", 1)
	deleted := map[string]any{"action": "deleted", "installation": installation(99)}
	deliver(t, installs, "installation", deleted)
	deliver(t, installs, "installation", deleted)
	deliver(t, workflows, "workflow_run", run)
	deliver(t, installs, "installation", created)
	count(t, db, "SELECT COUNT(*) FROM repositories", 1)
	count(t, db, "SELECT COUNT(*) FROM repositories WHERE installation_id=88", 1)
	count(t, db, "SELECT COUNT(*) FROM workflow_runs", 0)
}

func TestPostgresInstallationChangesRollback(t *testing.T) {
	db, err := server.InitDatabase(&server.DatabaseConfig{Driver: "postgres", URL: testdb.URL(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &handler.InstallationHandler{Base: handler.Base{DB: db}}
	payload, err := json.Marshal(map[string]any{"action": "created", "installation": installation(99), "repositories": []any{repository(1), map[string]any{"id": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), "installation", "test", payload); err == nil {
		t.Fatal("incomplete repository should fail")
	}
	count(t, db, "SELECT COUNT(*) FROM repositories", 0)
}
