package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigRealtimeAndWindow(t *testing.T) {
	for _, tt := range []struct {
		name, yaml        string
		realtime, wantErr bool
	}{
		{name: "default", realtime: true},
		{name: "explicit disable", yaml: "options:\n  enable_realtime: false"},
		{name: "valid window", yaml: "options:\n  default_time_window: 168h", realtime: true},
		{name: "days rejected", yaml: "options:\n  default_time_window: 7d", wantErr: true},
		{name: "zero rejected", yaml: "options:\n  default_time_window: 0s", wantErr: true},
		{name: "negative rejected", yaml: "options:\n  default_time_window: -1h", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.yaml))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil && cfg.Options.EnableRealtime != tt.realtime {
				t.Fatalf("realtime = %v", cfg.Options.EnableRealtime)
			}
		})
	}
}

func TestUnavailableDatabaseResponses(t *testing.T) {
	s := &Server{}
	for _, h := range []http.HandlerFunc{s.handleHealth, s.handleRepositories, s.handleWorkflows, s.handleAnalytics, s.handleFlakyTests, s.handleTrends, s.handleWebhookMetrics, s.handleRepositoryDetail} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", w.Code)
		}
	}
}

type recoveryDriver struct{ online *atomic.Bool }
type recoveryConn struct{ online *atomic.Bool }

func (d recoveryDriver) Open(string) (driver.Conn, error)  { return recoveryConn(d), nil }
func (c recoveryConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c recoveryConn) Close() error                        { return nil }
func (c recoveryConn) Begin() (driver.Tx, error)           { return nil, errors.New("not implemented") }
func (c recoveryConn) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.online.Load() {
		return errors.New("database offline")
	}
	return nil
}
func (c recoveryConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if err := c.Ping(ctx); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

var recoveryDriverID atomic.Int64

func TestDatabaseRecoveryAndCancellation(t *testing.T) {
	var online atomic.Bool
	name := fmt.Sprintf("recovery-%d", recoveryDriverID.Add(1))
	sql.Register(name, recoveryDriver{online: &online})
	db, err := OpenDatabase(&DatabaseConfig{Driver: name})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{db: db, ctx: ctx, cancel: cancel}
	if err := initializeDatabase(ctx, db); err == nil {
		t.Fatal("offline initialization succeeded")
	}
	online.Store(true)
	s.retryDatabaseConnection()
	if !s.ready.Load() || s.db != db {
		t.Fatal("recovery did not initialize the existing pool")
	}
	online.Store(false)
	done := make(chan struct{})
	go func() { s.retryDatabaseConnection(); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry did not stop on cancellation")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("close must be idempotent:", err)
	}
}
