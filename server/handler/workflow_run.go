// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package handler

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/chainguard-dev/clog"
	"github.com/google/go-github/v92/github"
	"github.com/pkg/errors"
)

// WorkflowRunHandler processes workflow_run webhook events
type WorkflowRunHandler struct {
	Base
}

// Handles returns the GitHub event types this handler processes
func (h *WorkflowRunHandler) Handles() []string {
	return []string{"workflow_run"}
}

// Handle processes a workflow_run webhook event
func (h *WorkflowRunHandler) Handle(ctx context.Context, _, _ string, payload []byte) error {
	logger := clog.FromContext(ctx)

	var event github.WorkflowRunEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to unmarshal workflow_run event")
	}

	// Only process completed workflow runs
	if event.GetAction() != "completed" {
		logger.Debug("Skipping non-completed workflow_run", "action", event.GetAction())
		return nil
	}

	workflowRun := event.GetWorkflowRun()
	repo := event.GetRepo()

	logger.Info("Processing completed workflow run",
		"workflow_run_id", workflowRun.GetID(),
		"name", workflowRun.GetName(),
		"status", workflowRun.GetStatus(),
		"conclusion", workflowRun.GetConclusion(),
		"repository", repo.GetFullName(),
		"branch", workflowRun.GetHeadBranch())

	// Store the completed run.
	stored, err := h.storeWorkflowRun(ctx, workflowRun, repo, event.GetInstallation().GetID())
	if err != nil {
		logger.Error("Failed to store workflow run", "error", err)
		return err
	}

	if !stored {
		return nil
	}

	// Broadcast the persisted update.
	if err := h.broadcastUpdate(ctx, workflowRun, repo); err != nil {
		logger.Warn("Failed to broadcast update", "error", err)
		// Don't return error - this is non-critical
	}

	return nil
}

// storeWorkflowRun atomically stores an event only while installation access is active.
func (h *WorkflowRunHandler) storeWorkflowRun(ctx context.Context, run *github.WorkflowRun, repo *github.Repository, installationID int64) (bool, error) {
	if run.GetID() <= 0 {
		return false, errors.New("missing run ID")
	}
	return h.withRepository(ctx, installationID, repo, func(tx *sql.Tx) error {
		// Insert workflow run
		_, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_runs (
			id, repository_id, workflow_id, workflow_name, workflow_path,
			head_branch, head_sha, run_number, event,
			status, conclusion, html_url,
			created_at, updated_at, run_started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion,
			updated_at = EXCLUDED.updated_at,
 run_started_at = EXCLUDED.run_started_at
 WHERE EXCLUDED.updated_at >= workflow_runs.updated_at
	`,
			run.GetID(),
			repo.GetID(),
			run.GetWorkflowID(),
			run.GetName(),
			run.GetPath(),
			run.GetHeadBranch(),
			run.GetHeadSHA(),
			run.GetRunNumber(),
			run.GetEvent(),
			run.GetStatus(),
			run.GetConclusion(),
			run.GetHTMLURL(),
			run.GetCreatedAt().Time,
			run.GetUpdatedAt().Time,
			run.GetRunStartedAt().Time,
		)
		return err
	})
}

// broadcastUpdate sends real-time updates via WebSocket
func (h *WorkflowRunHandler) broadcastUpdate(ctx context.Context, run *github.WorkflowRun, repo *github.Repository) error {
	if h.WSHub == nil {
		return nil // WebSocket not enabled
	}

	logger := clog.FromContext(ctx)

	// Broadcast workflow run update
	update := map[string]interface{}{
		"id":                   run.GetID(),
		"repository_id":        repo.GetID(),
		"repository_full_name": repo.GetFullName(),
		"workflow_name":        run.GetName(),
		"head_branch":          run.GetHeadBranch(),
		"status":               run.GetStatus(),
		"conclusion":           run.GetConclusion(),
		"html_url":             run.GetHTMLURL(),
		"created_at":           run.GetCreatedAt().Time,
	}

	if err := h.WSHub.Broadcast("workflow_update", update); err != nil {
		logger.Error("Failed to broadcast workflow update", "error", err)
		return err
	}

	logger.Debug("Broadcasted workflow update",
		"workflow_run_id", run.GetID(),
		"connected_clients", h.WSHub.ClientCount())
	return nil
}
