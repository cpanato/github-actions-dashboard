// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package handler

import (
	"context"
	"encoding/json"

	"github.com/google/go-github/v66/github"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
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
func (h *WorkflowRunHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	logger := zerolog.Ctx(ctx)

	var event github.WorkflowRunEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to unmarshal workflow_run event")
	}

	// Only process completed workflow runs
	if event.GetAction() != "completed" {
		logger.Debug().
			Str("action", event.GetAction()).
			Msg("Skipping non-completed workflow_run")
		return nil
	}

	workflowRun := event.GetWorkflowRun()
	repo := event.GetRepo()

	logger.Info().
		Int64("workflow_run_id", workflowRun.GetID()).
		Str("name", workflowRun.GetName()).
		Str("status", workflowRun.GetStatus()).
		Str("conclusion", workflowRun.GetConclusion()).
		Str("repository", repo.GetFullName()).
		Str("branch", workflowRun.GetHeadBranch()).
		Msg("Processing completed workflow run")

	// TODO: Store in database
	if err := h.storeWorkflowRun(ctx, workflowRun, repo); err != nil {
		logger.Error().Err(err).Msg("Failed to store workflow run")
		return err
	}

	// TODO: Broadcast update via WebSocket
	if err := h.broadcastUpdate(ctx, workflowRun, repo); err != nil {
		logger.Warn().Err(err).Msg("Failed to broadcast update")
		// Don't return error - this is non-critical
	}

	return nil
}

// storeWorkflowRun saves workflow run data to the database
func (h *WorkflowRunHandler) storeWorkflowRun(ctx context.Context, run *github.WorkflowRun, repo *github.Repository) error {
	logger := zerolog.Ctx(ctx)

	// First, ensure repository exists
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO repositories (id, owner, name, full_name, default_branch)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			updated_at = CURRENT_TIMESTAMP,
			default_branch = EXCLUDED.default_branch
	`, repo.GetID(), repo.GetOwner().GetLogin(), repo.GetName(), repo.GetFullName(), repo.GetDefaultBranch())
	if err != nil {
		logger.Error().Err(err).
			Int64("repo_id", repo.GetID()).
			Str("repo_name", repo.GetFullName()).
			Msg("Failed to upsert repository")
		return errors.Wrap(err, "failed to upsert repository")
	}

	logger.Debug().
		Int64("repo_id", repo.GetID()).
		Str("repo_name", repo.GetFullName()).
		Msg("Repository upserted successfully")

	// Insert workflow run
	_, err = h.DB.ExecContext(ctx, `
		INSERT INTO workflow_runs (
			id, repository_id, workflow_id, workflow_name,
			head_branch, head_sha, run_number, event,
			status, conclusion, html_url,
			created_at, updated_at, run_started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion,
			updated_at = EXCLUDED.updated_at
	`,
		run.GetID(),
		repo.GetID(),
		run.GetWorkflowID(),
		run.GetName(),
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

	logger.Info().
		Int64("workflow_run_id", run.GetID()).
		Str("repository", repo.GetFullName()).
		Str("conclusion", run.GetConclusion()).
		Msg("Stored workflow run")

	return nil
}

// broadcastUpdate sends real-time updates via WebSocket
func (h *WorkflowRunHandler) broadcastUpdate(ctx context.Context, run *github.WorkflowRun, repo *github.Repository) error {
	if h.WSHub == nil {
		return nil // WebSocket not enabled
	}

	logger := zerolog.Ctx(ctx)

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
		logger.Error().Err(err).Msg("Failed to broadcast workflow update")
		return err
	}

	logger.Debug().
		Int64("workflow_run_id", run.GetID()).
		Int("connected_clients", h.WSHub.ClientCount()).
		Msg("Broadcasted workflow update")
	return nil
}
