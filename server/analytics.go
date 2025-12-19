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

// Analytics provides statistical analysis of workflow data
type Analytics struct {
	TotalRuns       int          `json:"total_runs"`
	SuccessRate     float64      `json:"success_rate"`
	AverageDuration float64      `json:"average_duration_seconds"`
	FlakyTests      []FlakyTest  `json:"flaky_tests,omitempty"`
	TrendData       []TrendPoint `json:"trend_data,omitempty"`
}

// FlakyTest represents a test that intermittently fails
type FlakyTest struct {
	Name         string    `json:"name"`
	Repository   string    `json:"repository"`
	TotalRuns    int       `json:"total_runs"`
	FailureCount int       `json:"failure_count"`
	FlakeRate    float64   `json:"flake_rate"`
	LastFailure  time.Time `json:"last_failure"`
}

// TrendPoint represents success rate at a point in time
type TrendPoint struct {
	Date        string  `json:"date"`
	SuccessRate float64 `json:"success_rate"`
	TotalRuns   int     `json:"total_runs"`
}

// GetAnalytics returns overall analytics for a repository or all repositories
func GetAnalytics(ctx context.Context, db *sql.DB, repositoryID int64, days int) (*Analytics, error) {
	since := time.Now().AddDate(0, 0, -days)

	var query string
	var args []interface{}

	if repositoryID > 0 {
		query = `
			SELECT
				COUNT(*) as total_runs,
				SUM(CASE WHEN conclusion = 'success' THEN 1 ELSE 0 END) as successful_runs
			FROM workflow_runs
			WHERE repository_id = ? AND created_at >= ?
		`
		args = []interface{}{repositoryID, since}
	} else {
		query = `
			SELECT
				COUNT(*) as total_runs,
				SUM(CASE WHEN conclusion = 'success' THEN 1 ELSE 0 END) as successful_runs
			FROM workflow_runs
			WHERE created_at >= ?
		`
		args = []interface{}{since}
	}

	var totalRuns, successfulRuns int
	err := db.QueryRowContext(ctx, query, args...).Scan(&totalRuns, &successfulRuns)
	if err != nil {
		return nil, err
	}

	successRate := 0.0
	if totalRuns > 0 {
		successRate = float64(successfulRuns) / float64(totalRuns) * 100
	}

	return &Analytics{
		TotalRuns:       totalRuns,
		SuccessRate:     successRate,
		AverageDuration: 0, // TODO: Calculate duration
	}, nil
}

// GetFlakyTests identifies tests that fail intermittently
func GetFlakyTests(ctx context.Context, db *sql.DB, repositoryID int64, minRuns int, days int) ([]FlakyTest, error) {
	since := time.Now().AddDate(0, 0, -days)

	query := `
		SELECT
			cr.name,
			r.full_name,
			COUNT(*) as total_runs,
			SUM(CASE WHEN cr.conclusion = 'failure' THEN 1 ELSE 0 END) as failure_count,
			MAX(cr.completed_at) as last_failure
		FROM check_runs cr
		JOIN repositories r ON cr.repository_id = r.id
		WHERE cr.completed_at >= $1
	`

	args := []interface{}{since}

	if repositoryID > 0 {
		query += ` AND cr.repository_id = $2`
		args = append(args, repositoryID)
	}

	query += `
		GROUP BY cr.name, r.full_name
		HAVING COUNT(*) >= $` + fmt.Sprintf("%d", len(args)+1) + `
			AND SUM(CASE WHEN cr.conclusion = 'failure' THEN 1 ELSE 0 END) > 0
			AND SUM(CASE WHEN cr.conclusion = 'success' THEN 1 ELSE 0 END) > 0
		ORDER BY (CAST(SUM(CASE WHEN cr.conclusion = 'failure' THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*)) DESC
		LIMIT 20
	`
	args = append(args, minRuns)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flakyTests []FlakyTest
	for rows.Next() {
		var ft FlakyTest
		err = rows.Scan(&ft.Name, &ft.Repository, &ft.TotalRuns, &ft.FailureCount, &ft.LastFailure)
		if err != nil {
			return nil, err
		}
		ft.FlakeRate = float64(ft.FailureCount) / float64(ft.TotalRuns) * 100
		flakyTests = append(flakyTests, ft)
	}

	return flakyTests, rows.Err()
}

// GetTrendData returns success rate trends over time
func GetTrendData(ctx context.Context, db *sql.DB, repositoryID int64, days int) ([]TrendPoint, error) {
	since := time.Now().AddDate(0, 0, -days)

	// Get daily aggregates
	query := `
		SELECT
			DATE(created_at) as date,
			COUNT(*) as total_runs,
			SUM(CASE WHEN conclusion = 'success' THEN 1 ELSE 0 END) as successful_runs
		FROM workflow_runs
		WHERE created_at >= $1
	`

	args := []interface{}{since}

	if repositoryID > 0 {
		query += ` AND repository_id = $2`
		args = append(args, repositoryID)
	}

	query += `
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trends []TrendPoint
	for rows.Next() {
		var tp TrendPoint
		var successfulRuns int
		err = rows.Scan(&tp.Date, &tp.TotalRuns, &successfulRuns)
		if err != nil {
			return nil, err
		}
		if tp.TotalRuns > 0 {
			tp.SuccessRate = float64(successfulRuns) / float64(tp.TotalRuns) * 100
		}
		trends = append(trends, tp)
	}

	return trends, rows.Err()
}
