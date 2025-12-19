// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"  // PostgreSQL driver
	_ "modernc.org/sqlite" // SQLite driver (pure Go, no CGO)
)

// InitDatabase creates and configures the database connection
func InitDatabase(cfg *DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open(cfg.Driver, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize schema
	if err := initSchema(db, cfg.Driver); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

// initSchema creates the necessary tables if they don't exist
func initSchema(db *sql.DB, driver string) error {
	var schema string

	if driver == "sqlite" {
		schema = sqliteSchema
	} else {
		schema = postgresSchema
	}

	_, err := db.Exec(schema)
	return err
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS repositories (
	id INTEGER PRIMARY KEY,
	owner TEXT NOT NULL,
	name TEXT NOT NULL,
	full_name TEXT NOT NULL UNIQUE,
	default_branch TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_repositories_full_name ON repositories(full_name);

CREATE TABLE IF NOT EXISTS workflow_runs (
	id INTEGER PRIMARY KEY,
	repository_id INTEGER NOT NULL,
	workflow_id INTEGER NOT NULL,
	workflow_name TEXT NOT NULL,
	head_branch TEXT,
	head_sha TEXT,
	run_number INTEGER,
	event TEXT,
	status TEXT,
	conclusion TEXT,
	html_url TEXT,
	created_at DATETIME,
	updated_at DATETIME,
	run_started_at DATETIME,
	FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_repository ON workflow_runs(repository_id);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_created_at ON workflow_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_conclusion ON workflow_runs(conclusion);

CREATE TABLE IF NOT EXISTS workflow_jobs (
	id INTEGER PRIMARY KEY,
	workflow_run_id INTEGER NOT NULL,
	run_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	status TEXT,
	conclusion TEXT,
	started_at DATETIME,
	completed_at DATETIME,
	html_url TEXT,
	FOREIGN KEY (workflow_run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_workflow_jobs_run_id ON workflow_jobs(workflow_run_id);
CREATE INDEX IF NOT EXISTS idx_workflow_jobs_status ON workflow_jobs(status);

CREATE TABLE IF NOT EXISTS check_runs (
	id INTEGER PRIMARY KEY,
	repository_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	status TEXT,
	conclusion TEXT,
	html_url TEXT,
	details_url TEXT,
	started_at DATETIME,
	completed_at DATETIME,
	FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_check_runs_repository ON check_runs(repository_id);
CREATE INDEX IF NOT EXISTS idx_check_runs_name ON check_runs(name);
CREATE INDEX IF NOT EXISTS idx_check_runs_conclusion ON check_runs(conclusion);
CREATE INDEX IF NOT EXISTS idx_check_runs_completed ON check_runs(completed_at DESC);
`

const postgresSchema = `
CREATE TABLE IF NOT EXISTS repositories (
	id BIGINT PRIMARY KEY,
	owner TEXT NOT NULL,
	name TEXT NOT NULL,
	full_name TEXT NOT NULL UNIQUE,
	default_branch TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_repositories_full_name ON repositories(full_name);

CREATE TABLE IF NOT EXISTS workflow_runs (
	id BIGINT PRIMARY KEY,
	repository_id BIGINT NOT NULL,
	workflow_id BIGINT NOT NULL,
	workflow_name TEXT NOT NULL,
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
