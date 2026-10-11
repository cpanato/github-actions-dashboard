package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpanato/github-actions-dashboard/internal/testdb"
	"github.com/gorilla/websocket"
)

func integrationServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dataPath, err := filepath.Abs("../kodata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KO_DATA_PATH", dataPath)
	cfg, err := ParseConfig([]byte("demo_mode: true\nserver:\n  address: 127.0.0.1\noptions:\n  default_time_window: 168h\ngithub:\n  app:\n    webhook_secret: integration-secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = testdb.URL(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	s.workers.Add(1)
	go func() { defer s.workers.Done(); s.wsHub.Run(s.ctx) }()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestPostgresHTTPRepresentationsAndDemo(t *testing.T) {
	s, ts := integrationServer(t)
	for _, endpoint := range []string{"/api/repositories", "/api/workflows?limit=3", "/api/analytics", "/api/flaky-tests", "/api/trends"} {
		for _, html := range []bool{false, true} {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			if html && endpoint != "/api/trends" {
				req.Header.Set("HX-Request", "true")
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("%s returned %d", endpoint, resp.StatusCode)
			}
			if req.Header.Get("HX-Request") == "true" {
				if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
					t.Fatal("expected HTML")
				}
			} else {
				var data any
				if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
					t.Fatal(err)
				}
			}
			resp.Body.Close()
		}
	}
	before, err := GetRepositories(context.Background(), s.db, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedDemoData(context.Background(), s.db); err != nil {
		t.Fatal(err)
	}
	after, err := GetRepositories(context.Background(), s.db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 3 || len(after) != 3 {
		t.Fatal("demo seed is not idempotent")
	}
	for i := range before {
		if before[i].TotalRuns != after[i].TotalRuns {
			t.Fatal("demo duplicated runs")
		}
	}
}

func TestPostgresSignedWebhookAndWebSocket(t *testing.T) {
	s, ts := integrationServer(t)
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response != nil {
		response.Body.Close()
	}
	defer conn.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	payload, err := json.Marshal(map[string]any{"action": "completed", "installation": map[string]any{"id": 999},
		"repository":   map[string]any{"id": 123, "owner": map[string]any{"login": "acme"}, "name": "test", "full_name": "acme/test", "default_branch": "main"},
		"workflow_run": map[string]any{"id": 456, "workflow_id": 789, "name": "integration CI", "status": "completed", "conclusion": "success", "created_at": now, "updated_at": now, "run_started_at": now}})
	if err != nil {
		t.Fatal(err)
	}
	for _, signed := range []bool{false, true, true} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/api/github/hook", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "workflow_run")
		req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("integration-%v", signed))
		if signed {
			mac := hmac.New(sha256.New, []byte("integration-secret"))
			mac.Write(payload)
			req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if signed && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
			t.Fatalf("signed webhook status %d", resp.StatusCode)
		}
		if !signed && resp.StatusCode < 400 {
			t.Fatal("unsigned webhook accepted")
		}
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message struct{ Type string }
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "workflow_update" {
		t.Fatalf("message=%+v", message)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE id=456`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("duplicate delivery created additional rows")
	}
}
