// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func demoCommitSHA(index int) string {
	seed := fmt.Sprintf("github-actions-dashboard-demo-commit-%02d", index)
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:20])
}

func seedDemoData(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			slog.Warn("Failed to roll back demo seed transaction", "error", err)
		}
	}()

	repositories := []struct {
		id                  int64
		owner, name, branch string
	}{
		{910001, "acme-demo", "payments-api", "main"},
		{910002, "acme-demo", "web-portal", "main"},
		{910003, "acme-demo", "infra", "trunk"},
	}
	for _, repo := range repositories {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO repositories (id, owner, name, full_name, default_branch)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				owner = EXCLUDED.owner,
				name = EXCLUDED.name,
				full_name = EXCLUDED.full_name,
				default_branch = EXCLUDED.default_branch,
				updated_at = CURRENT_TIMESTAMP
		`, repo.id, repo.owner, repo.name, repo.owner+"/"+repo.name, repo.branch)
		if err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	runs := []struct {
		id, repoID, workflowID int64
		name, branch, sha      string
		number                 int
		status, conclusion     string
		hoursAgo               int
	}{
		{920001, 910001, 930001, "Build and Test", "main", demoCommitSHA(0), 184, "completed", "success", 2},
		{920002, 910001, 930001, "Build and Test", "feature/refunds", demoCommitSHA(1), 183, "completed", "failure", 8},
		{920003, 910001, 930002, "Security Scan", "main", demoCommitSHA(0), 71, "completed", "success", 20},
		{920004, 910002, 930003, "Web Checks", "main", demoCommitSHA(2), 392, "completed", "success", 1},
		{920005, 910002, 930003, "Web Checks", "feature/nav", demoCommitSHA(3), 391, "completed", "cancelled", 13},
		{920006, 910002, 930004, "Deploy Preview", "main", demoCommitSHA(2), 118, "in_progress", "", 0},
		{920007, 910003, 930005, "Terraform Validate", "trunk", demoCommitSHA(4), 56, "completed", "success", 4},
		{920008, 910003, 930005, "Terraform Validate", "feature/networking", demoCommitSHA(5), 55, "completed", "failure", 27},
	}
	for _, run := range runs {
		createdAt := now.Add(-time.Duration(run.hoursAgo) * time.Hour)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO workflow_runs (
				id, repository_id, workflow_id, workflow_name, workflow_path,
				head_branch, head_sha, run_number, event, status, conclusion,
				html_url, created_at, updated_at, run_started_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, $13)
			ON CONFLICT (id) DO UPDATE SET
				status = EXCLUDED.status,
				conclusion = EXCLUDED.conclusion,
				head_sha = EXCLUDED.head_sha,
				created_at = EXCLUDED.created_at,
				updated_at = EXCLUDED.updated_at
		`, run.id, run.repoID, run.workflowID, run.name, ".github/workflows/ci.yml",
			run.branch, run.sha, run.number, "push", run.status, run.conclusion,
			"https://github.com/"+repositories[0].owner+"/actions/runs/"+itoa(int(run.id)), createdAt)
		if err != nil {
			return err
		}
	}

	workflowNames := []string{
		"Agent Comment",
		"ProdSec Risk Review",
		"Agent Eval",
		"DFC Evals",
		"Claude Assistant",
		"Coverage Comment",
		"CI Summary",
		"Coverage PR",
		"Do Not Submit",
		"SDK Release Note",
		"Check Model Support",
		"Pre-commit",
		"Sentinel Ruby Authority Contract",
		"Skillshare Restore Contract",
		"Code Style",
		"Verify",
		"Check Enforce Documentation",
		"Validate Terraform",
		"CODEOWNERS Coverage",
		"Boilerplate",
		"Kind E2E",
		"Dependency Bot Pull Request",
		"Open Source Attestation",
		"Build and Test",
		"Security Scan",
	}

	for commitIndex := 0; commitIndex < 32; commitIndex++ {
		commitSHA := demoCommitSHA(commitIndex)
		branch := "main"
		if commitIndex%7 == 3 {
			branch = fmt.Sprintf("feature/demo-%02d", commitIndex)
		}
		event := "push"
		if commitIndex%5 == 2 {
			event = "pull_request"
		}
		createdAt := now.Add(-time.Duration(commitIndex*6) * time.Hour)

		for workflowIndex, workflowName := range workflowNames {
			if (commitIndex*7+workflowIndex*11)%10 >= 4 {
				continue
			}

			status := "completed"
			conclusion := "success"
			switch {
			case commitIndex == 0 && workflowIndex == 0:
				status, conclusion = "in_progress", ""
			case (commitIndex+workflowIndex)%29 == 0:
				conclusion = "failure"
			case (commitIndex+workflowIndex)%37 == 0:
				conclusion = "cancelled"
			case (commitIndex+workflowIndex)%19 == 0:
				conclusion = "skipped"
			}

			runID := int64(921000 + commitIndex*100 + workflowIndex)
			runNumber := 220 - commitIndex
			_, err := tx.ExecContext(ctx, `
				INSERT INTO workflow_runs (
					id, repository_id, workflow_id, workflow_name, workflow_path,
					head_branch, head_sha, run_number, event, status, conclusion,
					html_url, created_at, updated_at, run_started_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, $13)
				ON CONFLICT (id) DO UPDATE SET
					workflow_name = EXCLUDED.workflow_name,
					head_branch = EXCLUDED.head_branch,
					head_sha = EXCLUDED.head_sha,
					event = EXCLUDED.event,
					status = EXCLUDED.status,
					conclusion = EXCLUDED.conclusion,
					created_at = EXCLUDED.created_at,
					updated_at = EXCLUDED.updated_at
			`, runID, int64(910001), int64(931000+workflowIndex), workflowName,
				".github/workflows/demo.yml", branch, commitSHA, runNumber, event,
				status, conclusion,
				fmt.Sprintf("https://github.com/acme-demo/payments-api/actions/runs/%d", runID),
				createdAt)
			if err != nil {
				return err
			}
		}
	}

	checks := []struct {
		id, repoID       int64
		name, conclusion string
		daysAgo          int
	}{
		{940001, 910001, "TestPaymentAuthorization", "success", 0},
		{940002, 910001, "TestPaymentAuthorization", "failure", 1},
		{940003, 910001, "TestPaymentAuthorization", "success", 2},
		{940004, 910001, "TestPaymentAuthorization", "success", 3},
		{940005, 910001, "TestPaymentAuthorization", "failure", 4},
		{940006, 910001, "TestPaymentAuthorization", "success", 5},
		{940007, 910002, "TestNavigationMenu", "success", 0},
		{940008, 910002, "TestNavigationMenu", "success", 1},
		{940009, 910002, "TestNavigationMenu", "failure", 2},
		{940010, 910002, "TestNavigationMenu", "success", 3},
		{940011, 910002, "TestNavigationMenu", "success", 4},
	}
	for _, check := range checks {
		completedAt := now.AddDate(0, 0, -check.daysAgo)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO check_runs (
				id, repository_id, name, status, conclusion, html_url, details_url,
				started_at, completed_at
			) VALUES ($1, $2, $3, 'completed', $4, $5, $5, $6, $7)
			ON CONFLICT (id) DO UPDATE SET
				conclusion = EXCLUDED.conclusion,
				completed_at = EXCLUDED.completed_at
		`, check.id, check.repoID, check.name, check.conclusion,
			"https://github.com/acme-demo/actions/runs/"+itoa(int(check.id)),
			completedAt.Add(-2*time.Minute), completedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
