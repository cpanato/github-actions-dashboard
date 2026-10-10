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
	SkippedRuns    int     `json:"skipped_runs,omitempty"`
	CancelledRuns  int     `json:"cancelled_runs,omitempty"`
	SuccessRate    float64 `json:"success_rate,omitempty"`
}

// WorkflowRun represents a workflow run
type WorkflowRun struct {
	ID           int64      `json:"id"`
	RepositoryID int64      `json:"repository_id"`
	WorkflowID   int64      `json:"workflow_id"`
	WorkflowName string     `json:"workflow_name"`
	WorkflowPath string     `json:"workflow_path,omitempty"`
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
			SUM(CASE WHEN wr.conclusion = 'failure' THEN 1 ELSE 0 END) as failed_runs,
			SUM(CASE WHEN wr.conclusion = 'skipped' THEN 1 ELSE 0 END) as skipped_runs,
			SUM(CASE WHEN wr.conclusion = 'cancelled' THEN 1 ELSE 0 END) as cancelled_runs
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
				&repo.TotalRuns, &repo.SuccessfulRuns, &repo.FailedRuns, &repo.SkippedRuns, &repo.CancelledRuns,
			)
			// Calculate success rate excluding skipped and cancelled
			activeRuns := repo.TotalRuns - repo.SkippedRuns - repo.CancelledRuns
			if activeRuns > 0 {
				repo.SuccessRate = float64(repo.SuccessfulRuns) / float64(activeRuns) * 100
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
		SELECT wr.id, wr.repository_id, wr.workflow_id, wr.workflow_name, wr.workflow_path,
		       wr.head_branch, wr.head_sha, wr.run_number, wr.event,
		       wr.status, wr.conclusion, wr.html_url,
		       wr.created_at, wr.updated_at, wr.run_started_at,
		       r.full_name
		FROM workflow_runs wr
		JOIN repositories r ON wr.repository_id = r.id
		WHERE ($1::BIGINT = 0 OR wr.repository_id = $1)
		  AND ($2::TEXT = '' OR wr.status = $2)
		  AND ($3::TEXT = '' OR wr.conclusion = $3)
		  AND ($4::TEXT = '' OR wr.head_branch = $4)
		  AND ($5::TEXT = '' OR wr.workflow_name = $5)
		  AND ($6::TEXT = '' OR wr.event = $6)
		  AND ($7::TIMESTAMP IS NULL OR wr.created_at >= $7)
		ORDER BY wr.created_at DESC
		LIMIT NULLIF($8::INTEGER, 0)
	`

	var since interface{}
	if !opts.Since.IsZero() {
		since = opts.Since
	}
	limit := opts.Limit
	if limit < 0 {
		limit = 0
	}
	args := []interface{}{
		opts.RepositoryID,
		opts.Status,
		opts.Conclusion,
		opts.HeadBranch,
		opts.WorkflowName,
		opts.Event,
		since,
		limit,
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
			&run.ID, &run.RepositoryID, &run.WorkflowID, &run.WorkflowName, &run.WorkflowPath,
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
	HeadBranch   string
	WorkflowName string
	Event        string
	Since        time.Time
	Limit        int
}

type WorkflowGridRow struct {
	Name  string
	Cells []*WorkflowRun
}

func buildWorkflowGrid(runs []WorkflowRun, limit int) ([]WorkflowRun, []WorkflowGridRow, []string) {
	columns := make([]WorkflowRun, 0)
	columnIndexByCommit := make(map[string]int)
	workflowOrder := make([]string, 0)
	workflowSeen := make(map[string]bool)
	workflowCells := make(map[string]map[int]*WorkflowRun)

	for _, run := range runs {
		commit := run.HeadSHA
		if commit == "" {
			commit = fmt.Sprintf("run:%d", run.ID)
		}

		columnIndex, exists := columnIndexByCommit[commit]
		if !exists {
			if limit > 0 && len(columns) >= limit {
				continue
			}
			columnIndex = len(columns)
			columnIndexByCommit[commit] = columnIndex
			columns = append(columns, run)
		}

		if !workflowSeen[run.WorkflowName] {
			workflowSeen[run.WorkflowName] = true
			workflowOrder = append(workflowOrder, run.WorkflowName)
			workflowCells[run.WorkflowName] = make(map[int]*WorkflowRun)
		}
		if _, exists := workflowCells[run.WorkflowName][columnIndex]; !exists {
			runCopy := run
			workflowCells[run.WorkflowName][columnIndex] = &runCopy
		}
	}

	workflowRows := make([]WorkflowGridRow, 0, len(workflowOrder))
	for _, workflowName := range workflowOrder {
		row := WorkflowGridRow{Name: workflowName, Cells: make([]*WorkflowRun, len(columns))}
		for columnIndex, run := range workflowCells[workflowName] {
			row.Cells[columnIndex] = run
		}
		workflowRows = append(workflowRows, row)
	}
	return columns, workflowRows, workflowOrder
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
