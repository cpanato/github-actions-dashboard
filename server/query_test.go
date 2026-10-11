package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWorkflowQueryDefaultsAndBounds(t *testing.T) {
	opts, err := parseWorkflowQuery(httptest.NewRequest(http.MethodGet, "/", nil), "24h")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Limit != 50 || opts.Offset != 0 || time.Since(opts.Since) < 23*time.Hour {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
	for _, query := range []string{"since=7d", "since=0s", "since=-1h", "limit=0", "limit=-1", "limit=501", "limit=abc", "offset=-1", "offset=100001", "repository_id=-1", "repository_id=abc", "status=wrong", "conclusion=wrong"} {
		t.Run(query, func(t *testing.T) {
			if _, err := parseWorkflowQuery(httptest.NewRequest(http.MethodGet, "/?"+query, nil), "24h"); err == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
	opts, err = parseWorkflowQuery(httptest.NewRequest(http.MethodGet, "/?limit=500&offset=100000&repository_id=42&since=168h&conclusion=failure", nil), "24h")
	if err != nil || opts.Limit != 500 || opts.Offset != 100000 || opts.RepositoryID != 42 {
		t.Fatalf("boundary query: %+v, %v", opts, err)
	}
}

func TestPostgresInvalidAPIQueriesAndPagination(t *testing.T) {
	s, ts := integrationServer(t)
	for _, endpoint := range []string{
		"/api/workflows?since=7d", "/api/workflows?limit=0", "/api/workflows?limit=501", "/api/workflows?offset=-1",
		"/api/analytics?repository_id=invalid", "/api/analytics?days=0", "/api/trends?days=366", "/api/flaky-tests?min_runs=0",
	} {
		resp, err := ts.Client().Get(ts.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s returned %d", endpoint, resp.StatusCode)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO repositories (id,owner,name,full_name) VALUES (93000,'test','pagination','test/pagination');
 INSERT INTO workflow_runs (id,repository_id,workflow_id,workflow_name,workflow_path,head_branch,head_sha,run_number,event,status,conclusion,html_url,created_at,updated_at)
 SELECT n,93000,1,'CI','','main','sha',1,'push','completed','success','',NOW(),NOW() FROM generate_series(93001,93003) n`); err != nil {
		t.Fatal(err)
	}
	first, err := GetWorkflowRuns(t.Context(), s.db, WorkflowRunOptions{RepositoryID: 93000, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := GetWorkflowRuns(t.Context(), s.db, WorkflowRunOptions{RepositoryID: 93000, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 1 || first[0].ID != 93003 || first[1].ID != 93002 || second[0].ID != 93001 {
		t.Fatalf("unstable pagination: first=%+v second=%+v", first, second)
	}
}
