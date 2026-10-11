package server

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/cpanato/github-actions-dashboard/internal/testdb"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testdb.Open(t)
	if err := initializeDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresAnalytics(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO repositories (id, owner, name, full_name, default_branch) VALUES (1,'acme','repo','acme/repo','main');
 INSERT INTO workflow_runs (id,repository_id,workflow_id,workflow_name,status,conclusion,created_at) VALUES
 (1,1,1,'CI','completed','success',CURRENT_TIMESTAMP),
 (2,1,1,'CI','completed','failure',CURRENT_TIMESTAMP),
 (3,1,1,'CI','queued','',CURRENT_TIMESTAMP),
 (4,1,1,'CI','in_progress','',CURRENT_TIMESTAMP),
 (5,1,1,'CI','completed','cancelled',CURRENT_TIMESTAMP),
 (6,1,1,'CI','completed','skipped',CURRENT_TIMESTAMP),
 (7,1,1,'CI','completed','neutral',CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{0, 1} {
		metrics, err := GetAnalytics(ctx, db, id, 7)
		if err != nil {
			t.Fatal(err)
		}
		if metrics.TotalRuns != 7 || metrics.SuccessRate != 50 {
			t.Fatalf("metrics=%+v", metrics)
		}
		trends, err := GetTrendData(ctx, db, id, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(trends) != 1 || trends[0].SuccessRate != 50 {
			t.Fatalf("trends=%+v", trends)
		}
	}
	repos, err := GetRepositories(ctx, db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].SuccessRate != 50 {
		t.Fatalf("repositories=%+v", repos)
	}
	empty, err := GetAnalytics(ctx, db, 999, 7)
	if err != nil || empty.SuccessRate != 0 {
		t.Fatalf("empty=%+v, err=%v", empty, err)
	}
}

func TestPostgresFlakyLastFailure(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	failure := now.Add(-24 * time.Hour)
	_, err := db.Exec(`INSERT INTO repositories (id,owner,name,full_name) VALUES (1,'acme','repo','acme/repo')`)
	if err != nil {
		t.Fatal(err)
	}
	for i, outcome := range []string{"failure", "success", "success", "success", "success", "cancelled", "skipped"} {
		completed := now
		if outcome == "failure" {
			completed = failure
		}
		if _, err := db.Exec(`INSERT INTO check_runs (id,repository_id,name,status,conclusion,completed_at) VALUES ($1,1,'test','completed',$2,$3)`, i+1, outcome, completed); err != nil {
			t.Fatal(err)
		}
	}
	flaky, err := GetFlakyTests(ctx, db, 0, 5, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(flaky) != 1 || flaky[0].TotalRuns != 5 || flaky[0].FlakeRate != 20 || !flaky[0].LastFailure.Equal(failure) {
		t.Fatalf("flaky=%+v", flaky)
	}
	none, err := GetFlakyTests(ctx, db, 0, 6, 30)
	if err != nil || len(none) != 0 {
		t.Fatalf("minimum threshold ignored: %+v, %v", none, err)
	}
}
