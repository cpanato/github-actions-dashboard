// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
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

	// Test connection with retries (for Cloud SQL socket availability)
	maxRetries := 10
	retryDelay := 2 * time.Second
	var pingErr error

	for i := 0; i < maxRetries; i++ {
		pingErr = db.Ping()
		if pingErr == nil {
			if i > 0 {
				fmt.Printf("Database connection established after %d attempt(s)\n", i+1)
			}
			break
		}

		if i < maxRetries-1 {
			// Only log every 3rd attempt to reduce noise
			if i%3 == 0 || i >= maxRetries-2 {
				fmt.Printf("Database ping failed (attempt %d/%d): %v. Retrying in %v...\n",
					i+1, maxRetries, pingErr, retryDelay)
			}
			time.Sleep(retryDelay)
		}
	}

	if pingErr != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database after %d attempts: %w", maxRetries, pingErr)
	}

	fmt.Println("Database connection established successfully")

	// Initialize schema
	if err := initSchema(db, cfg.Driver); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

// initSchema creates the necessary tables if they don't exist
func initSchema(db *sql.DB, driver string) error {
	_, err := db.Exec(postgresSchema)
	return err
}

// maskPassword redacts password from connection string for logging
func maskPassword(connStr string) string {
	if len(connStr) == 0 {
		return ""
	}
	// For Unix socket format: host=/cloudsql/... password=xxx
	if idx := strings.Index(connStr, "password="); idx != -1 {
		end := strings.Index(connStr[idx:], " ")
		if end == -1 {
			return connStr[:idx] + "password=***"
		}
		return connStr[:idx] + "password=*** " + connStr[idx+end:]
	}
	// For URL format: postgres://user:password@host
	if strings.Contains(connStr, "://") && strings.Contains(connStr, ":") && strings.Contains(connStr, "@") {
		parts := strings.Split(connStr, "@")
		if len(parts) == 2 {
			userPart := strings.Split(parts[0], ":")
			if len(userPart) >= 3 {
				return strings.Join(userPart[:len(userPart)-1], ":") + ":***@" + parts[1]
			}
		}
	}
	return connStr
}

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
