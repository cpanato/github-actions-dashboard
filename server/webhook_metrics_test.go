package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpanato/github-actions-dashboard/server/auth"
)

func TestPostgresWebhookMetrics(t *testing.T) {
	s := testInbox(t, 0, inboxTestHandler(func(context.Context, string, []byte) error { return nil }))
	m, err := getWebhookMetrics(t.Context(), s.db)
	if err != nil || m != (WebhookMetrics{}) {
		t.Fatalf("empty inbox: %+v %v", m, err)
	}
	_, err = s.db.Exec(`INSERT INTO webhook_deliveries(delivery_id,event_type,payload,payload_hash,state,attempts,failure_total,received_at,available_at,locked_until,last_error) VALUES
 ('ready','workflow_run','secret payload','hash','pending',0,0,NOW()-INTERVAL '5 minutes',NOW(),NULL,'secret error'),
 ('delayed','workflow_run',NULL,'hash','pending',1,1,NOW()-INTERVAL '1 minute',NOW()+INTERVAL '5 minutes',NULL,''),
 ('active','workflow_run',NULL,'hash','processing',2,0,NOW(),NOW(),NOW()+INTERVAL '1 minute',''),
 ('expired','workflow_run',NULL,'hash','processing',3,1,NOW(),NOW(),NOW()-INTERVAL '1 minute',''),
 ('success','workflow_run',NULL,'hash','succeeded',1,0,NOW(),NOW(),NULL,''),
 ('exhausted','workflow_run',NULL,'hash','failed',8,8,NOW(),NOW(),NULL,'')`)
	if err != nil {
		t.Fatal(err)
	}
	m, err = getWebhookMetrics(t.Context(), s.db)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 6 || m.Pending != 2 || m.Ready != 1 || m.Delayed != 1 || m.Processing != 2 || m.ExpiredLeases != 1 || m.Succeeded != 1 || m.Failed != 1 || m.Attempts != 15 || m.HandlerFailures != 10 || m.OldestPendingSeconds < 300 || m.OldestPendingSeconds > 310 {
		t.Fatalf("metrics: %+v", m)
	}
}

func TestPostgresWebhookMetricsHTTP(t *testing.T) {
	s, ts := integrationServer(t)
	resp, err := ts.Client().Get(ts.URL + "/api/webhooks/metrics")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	err = json.NewDecoder(resp.Body).Decode(&fields)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Content-Type") != "application/json" || len(fields) != 11 {
		t.Fatalf("HTTP metrics: status=%d fields=%v error=%v", resp.StatusCode, fields, err)
	}
	for _, name := range []string{"payload", "delivery_id", "last_error"} {
		if _, exists := fields[name]; exists {
			t.Fatalf("exposes %s", name)
		}
	}
	w := httptest.NewRecorder()
	s.handleWebhookMetrics(w, httptest.NewRequest(http.MethodPost, "/api/webhooks/metrics", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatalf("POST: %d", w.Code)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.handleWebhookMetrics(w, httptest.NewRequest(http.MethodGet, "/api/webhooks/metrics", nil))
	if w.Code != 503 {
		t.Fatalf("closed database: %d", w.Code)
	}
}

func TestWebhookMetricsRequiresAuthorizedSession(t *testing.T) {
	s := &Server{config: &Config{}, sessions: auth.NewSessionManager(3600, false)}
	h := s.Handler()
	for _, authorized := range []bool{false, true} {
		login := httptest.NewRecorder()
		s.sessions.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			s.sessions.Put(r.Context(), auth.SessionKeyUsername, "operator")
			s.sessions.Put(r.Context(), auth.SessionKeyAuthorized, authorized)
		})).ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/", nil))
		req := httptest.NewRequest(http.MethodGet, "/api/webhooks/metrics", nil)
		for _, cookie := range login.Result().Cookies() {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		want := 302
		if authorized {
			want = 503
		} // Authorized requests reach the unavailable database.
		if w.Code != want {
			t.Fatalf("authorized=%v: status=%d want=%d", authorized, w.Code, want)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/webhooks/metrics", nil))
	if w.Code != 302 || w.Header().Get("Location") != "/api/github/auth" {
		t.Fatalf("anonymous: %d %s", w.Code, w.Header().Get("Location"))
	}
}
