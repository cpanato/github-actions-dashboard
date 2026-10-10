// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package handler

import (
	"context"
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

	// TODO: Store in database
	if err := h.storeWorkflowRun(ctx, workflowRun, repo); err != nil {
		logger.Error("Failed to store workflow run", "error", err)
		return err
	}

	// TODO: Broadcast update via WebSocket
	if err := h.broadcastUpdate(ctx, workflowRun, repo); err != nil {
		logger.Warn("Failed to broadcast update", "error", err)
		// Don't return error - this is non-critical
	}

	return nil
}

// storeWorkflowRun saves workflow run data to the database
func (h *WorkflowRunHandler) storeWorkflowRun(ctx context.Context, run *github.WorkflowRun, repo *github.Repository) error {
	logger := clog.FromContext(ctx)

	// Check if database is available
	if h.DB == nil {
		logger.Warn("Database not available, skipping workflow run storage")
		return errors.New("database not available")
	}

	// First, ensure repository exists
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO repositories (id, owner, name, full_name, default_branch)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			updated_at = CURRENT_TIMESTAMP,
			default_branch = EXCLUDED.default_branch
	`, repo.GetID(), repo.GetOwner().GetLogin(), repo.GetName(), repo.GetFullName(), repo.GetDefaultBranch())
	if err != nil {
		logger.Error("Failed to upsert repository",
			"error", err,
			"repo_id", repo.GetID(),
			"repo_name", repo.GetFullName())
		return errors.Wrap(err, "failed to upsert repository")
	}

	logger.Debug("Repository upserted successfully",
		"repo_id", repo.GetID(),
		"repo_name", repo.GetFullName())

	// Insert workflow run
	_, err = h.DB.ExecContext(ctx, `
		INSERT INTO workflow_runs (
			id, repository_id, workflow_id, workflow_name, workflow_path,
			head_branch, head_sha, run_number, event,
			status, conclusion, html_url,
			created_at, updated_at, run_started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion,
			updated_at = EXCLUDED.updated_at
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
	if err != nil {
		return errors.Wrap(err, "failed to insert workflow run")
	}

	logger.Info("Stored workflow run",
		"workflow_run_id", run.GetID(),
		"repository", repo.GetFullName(),
		"conclusion", run.GetConclusion())

	return nil
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
