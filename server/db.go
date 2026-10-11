// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// OpenDatabase creates a stable pool; it can reconnect without replacing its pointer.
func OpenDatabase(cfg *DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open(cfg.Driver, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	return db, nil
}

// InitDatabase initializes a database within a bounded startup window.
func InitDatabase(cfg *DatabaseConfig) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := OpenDatabase(cfg)
	if err != nil {
		return nil, err
	}
	for {
		err = initializeDatabase(ctx, db)
		if err == nil {
			return db, nil
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, fmt.Errorf("failed to initialize database: %w", err)
		case <-time.After(2 * time.Second):
		}
	}
}

func initializeDatabase(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, postgresSchema)
	return err
}

const postgresSchema = `
CREATE TABLE IF NOT EXISTS webhook_deliveries (
 delivery_id TEXT PRIMARY KEY,
 event_type TEXT NOT NULL,
 payload BYTEA,
 payload_hash TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','succeeded','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 locked_until TIMESTAMPTZ,
 completed_at TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending ON webhook_deliveries(available_at,received_at) WHERE state='pending';
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_leases ON webhook_deliveries(locked_until) WHERE state='processing';

CREATE TABLE IF NOT EXISTS repositories (
	id BIGINT PRIMARY KEY,
	owner TEXT NOT NULL,
	name TEXT NOT NULL,
	full_name TEXT NOT NULL UNIQUE,
	default_branch TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE repositories ADD COLUMN IF NOT EXISTS installation_id BIGINT;

CREATE TABLE IF NOT EXISTS installations (
 id BIGINT PRIMARY KEY,
 active BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE TABLE IF NOT EXISTS repository_access (
 installation_id BIGINT NOT NULL REFERENCES installations(id),
 repository_id BIGINT NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 PRIMARY KEY (installation_id, repository_id)
);

CREATE INDEX IF NOT EXISTS idx_repositories_installation ON repositories(installation_id);
CREATE INDEX IF NOT EXISTS idx_repositories_full_name ON repositories(full_name);

CREATE TABLE IF NOT EXISTS workflow_runs (
	id BIGINT PRIMARY KEY,
	repository_id BIGINT NOT NULL,
	workflow_id BIGINT NOT NULL,
	workflow_name TEXT NOT NULL,
	workflow_path TEXT,
	head_branch TEXT,
	head_sha TEXT,
	run_number INTEGER,
	event TEXT,
	status TEXT,
	conclusion TEXT,
	html_url TEXT,
	created_at TIMESTAMP,
	updated_at TIMESTAMP,
	run_started_at TIMESTAMP,
	FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_repository ON workflow_runs(repository_id);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_created_at ON workflow_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_conclusion ON workflow_runs(conclusion);

CREATE TABLE IF NOT EXISTS workflow_jobs (
	id BIGINT PRIMARY KEY,
	workflow_run_id BIGINT NOT NULL,
	run_id BIGINT NOT NULL,
	name TEXT NOT NULL,
	status TEXT,
	conclusion TEXT,
	started_at TIMESTAMP,
	completed_at TIMESTAMP,
	html_url TEXT,
	FOREIGN KEY (workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_workflow_jobs_run_id ON workflow_jobs(workflow_run_id);
CREATE INDEX IF NOT EXISTS idx_workflow_jobs_status ON workflow_jobs(status);

CREATE TABLE IF NOT EXISTS check_runs (
	id BIGINT PRIMARY KEY,
	repository_id BIGINT NOT NULL,
	name TEXT NOT NULL,
	status TEXT,
	conclusion TEXT,
	html_url TEXT,
	details_url TEXT,
	started_at TIMESTAMP,
	completed_at TIMESTAMP,
	FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_check_runs_repository ON check_runs(repository_id);
CREATE INDEX IF NOT EXISTS idx_check_runs_name ON check_runs(name);
CREATE INDEX IF NOT EXISTS idx_check_runs_conclusion ON check_runs(conclusion);
CREATE INDEX IF NOT EXISTS idx_check_runs_completed ON check_runs(completed_at DESC);
`
