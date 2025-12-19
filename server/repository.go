// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Repository represents a GitHub repository
type Repository struct {
	ID            int64     `json:"id"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Aggregate data
	TotalRuns      int     `json:"total_runs,omitempty"`
	SuccessfulRuns int     `json:"successful_runs,omitempty"`
	FailedRuns     int     `json:"failed_runs,omitempty"`
	SuccessRate    float64 `json:"success_rate,omitempty"`
}

// WorkflowRun represents a workflow run
type WorkflowRun struct {
	ID           int64      `json:"id"`
	RepositoryID int64      `json:"repository_id"`
	WorkflowID   int64      `json:"workflow_id"`
	WorkflowName string     `json:"workflow_name"`
	HeadBranch   string     `json:"head_branch"`
	HeadSHA      string     `json:"head_sha"`
	RunNumber    int        `json:"run_number"`
	Event        string     `json:"event"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	HTMLURL      string     `json:"html_url"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RunStartedAt *time.Time `json:"run_started_at,omitempty"`

	// Joined data
	RepositoryFullName string `json:"repository_full_name,omitempty"`
}

// GetRepositories returns all tracked repositories with optional statistics
func GetRepositories(ctx context.Context, db *sql.DB, withStats bool) ([]Repository, error) {
	query := `
		SELECT r.id, r.owner, r.name, r.full_name, r.default_branch,
		       r.created_at, r.updated_at
	`

	if withStats {
		query += `,
			COUNT(wr.id) as total_runs,
			SUM(CASE WHEN wr.conclusion = 'success' THEN 1 ELSE 0 END) as successful_runs,
			SUM(CASE WHEN wr.conclusion = 'failure' THEN 1 ELSE 0 END) as failed_runs
		`
	}

	query += ` FROM repositories r`

	if withStats {
		query += ` LEFT JOIN workflow_runs wr ON r.id = wr.repository_id`
	}

	if withStats {
		query += ` GROUP BY r.id, r.owner, r.name, r.full_name, r.default_branch, r.created_at, r.updated_at`
	}

	query += ` ORDER BY r.updated_at DESC`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []Repository
	for rows.Next() {
		var repo Repository

		if withStats {
			err = rows.Scan(
				&repo.ID, &repo.Owner, &repo.Name, &repo.FullName, &repo.DefaultBranch,
				&repo.CreatedAt, &repo.UpdatedAt,
				&repo.TotalRuns, &repo.SuccessfulRuns, &repo.FailedRuns,
			)
			if repo.TotalRuns > 0 {
				repo.SuccessRate = float64(repo.SuccessfulRuns) / float64(repo.TotalRuns) * 100
			}
		} else {
			err = rows.Scan(
				&repo.ID, &repo.Owner, &repo.Name, &repo.FullName, &repo.DefaultBranch,
				&repo.CreatedAt, &repo.UpdatedAt,
			)
		}

		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}

	return repos, rows.Err()
}

// GetWorkflowRuns returns workflow runs with optional filtering
func GetWorkflowRuns(ctx context.Context, db *sql.DB, opts WorkflowRunOptions) ([]WorkflowRun, error) {
	query := `
		SELECT wr.id, wr.repository_id, wr.workflow_id, wr.workflow_name,
		       wr.head_branch, wr.head_sha, wr.run_number, wr.event,
		       wr.status, wr.conclusion, wr.html_url,
		       wr.created_at, wr.updated_at, wr.run_started_at,
		       r.full_name
		FROM workflow_runs wr
		JOIN repositories r ON wr.repository_id = r.id
		WHERE 1=1
	`

	args := []interface{}{}

	if opts.RepositoryID > 0 {
		query += ` AND wr.repository_id = ?`
		args = append(args, opts.RepositoryID)
	}

	if opts.Status != "" {
		query += ` AND wr.status = ?`
		args = append(args, opts.Status)
	}

	if opts.Conclusion != "" {
		query += ` AND wr.conclusion = ?`
		args = append(args, opts.Conclusion)
	}

	if !opts.Since.IsZero() {
		query += ` AND wr.created_at >= ?`
		args = append(args, opts.Since)
	}

	query += ` ORDER BY wr.created_at DESC`

	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var run WorkflowRun
		err = rows.Scan(
			&run.ID, &run.RepositoryID, &run.WorkflowID, &run.WorkflowName,
			&run.HeadBranch, &run.HeadSHA, &run.RunNumber, &run.Event,
			&run.Status, &run.Conclusion, &run.HTMLURL,
			&run.CreatedAt, &run.UpdatedAt, &run.RunStartedAt,
			&run.RepositoryFullName,
		)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	return runs, rows.Err()
}

// WorkflowRunOptions defines filtering options for workflow runs
type WorkflowRunOptions struct {
	RepositoryID int64
	Status       string
	Conclusion   string
	Since        time.Time
	Limit        int
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
