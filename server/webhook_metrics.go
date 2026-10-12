package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/chainguard-dev/clog"
)

// WebhookMetrics summarizes retained inbox records without exposing payloads or
// delivery identifiers. These values are shared by all replicas using the inbox.
type WebhookMetrics struct {
	Total                int64   `json:"total"`
	Pending              int64   `json:"pending"`
	Ready                int64   `json:"ready"`
	Delayed              int64   `json:"delayed"`
	Processing           int64   `json:"processing"`
	ExpiredLeases        int64   `json:"expired_leases"`
	Succeeded            int64   `json:"succeeded"`
	Failed               int64   `json:"failed"`
	Attempts             int64   `json:"attempts"`
	HandlerFailures      int64   `json:"handler_failures"`
	OldestPendingSeconds float64 `json:"oldest_pending_seconds"`
}

func getWebhookMetrics(ctx context.Context, db *sql.DB) (WebhookMetrics, error) {
	var m WebhookMetrics
	err := db.QueryRowContext(ctx, `SELECT COUNT(*),
 COUNT(*) FILTER (WHERE state='pending'),
 COUNT(*) FILTER (WHERE state='pending' AND available_at<=NOW()),
 COUNT(*) FILTER (WHERE state='pending' AND available_at>NOW()),
 COUNT(*) FILTER (WHERE state='processing'),
 COUNT(*) FILTER (WHERE state='processing' AND locked_until<=NOW()),
 COUNT(*) FILTER (WHERE state='succeeded'),
 COUNT(*) FILTER (WHERE state='failed'),
 COALESCE(SUM(attempts),0),COALESCE(SUM(failure_total),0),
 GREATEST(0,COALESCE(EXTRACT(EPOCH FROM (NOW()-MIN(received_at) FILTER (WHERE state='pending'))),0))
 FROM webhook_deliveries`).Scan(&m.Total, &m.Pending, &m.Ready, &m.Delayed,
		&m.Processing, &m.ExpiredLeases, &m.Succeeded, &m.Failed,
		&m.Attempts, &m.HandlerFailures, &m.OldestPendingSeconds)
	return m, err
}

func (s *Server) handleWebhookMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireDatabase(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	metrics, err := getWebhookMetrics(ctx, s.db)
	if err != nil {
		clog.FromContext(ctx).Error("Failed to query webhook metrics", "error", err)
		http.Error(w, "Webhook metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		clog.FromContext(ctx).Warn("Failed to write webhook metrics", "error", err)
	}
}
