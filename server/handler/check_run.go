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

// CheckRunHandler processes check_run webhook events
type CheckRunHandler struct {
	Base
}

// Handles returns the GitHub event types this handler processes
func (h *CheckRunHandler) Handles() []string {
	return []string{"check_run"}
}

// Handle processes a check_run webhook event
func (h *CheckRunHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	logger := clog.FromContext(ctx)

	var event github.CheckRunEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to unmarshal check_run event")
	}

	// Only process completed check runs
	if event.GetAction() != "completed" {
		logger.Debug("Skipping non-completed check_run", "action", event.GetAction())
		return nil
	}

	checkRun := event.GetCheckRun()
	repo := event.GetRepo()

	logger.Info("Processing completed check run",
		"check_run_id", checkRun.GetID(),
		"name", checkRun.GetName(),
		"status", checkRun.GetStatus(),
		"conclusion", checkRun.GetConclusion(),
		"repository", repo.GetFullName())

	// Store check run in database
	if err := h.storeCheckRun(ctx, checkRun, repo); err != nil {
		logger.Error("Failed to store check run", "error", err)
		return err
	}

	// Broadcast update if WebSocket enabled
	if h.WSHub != nil {
		update := map[string]interface{}{
			"type":                 "check_run",
			"id":                   checkRun.GetID(),
			"name":                 checkRun.GetName(),
			"status":               checkRun.GetStatus(),
			"conclusion":           checkRun.GetConclusion(),
			"repository_id":        repo.GetID(),
			"repository_full_name": repo.GetFullName(),
		}
		h.WSHub.Broadcast("check_run_update", update)
	}

	return nil
}

// storeCheckRun saves check run data to the database
func (h *CheckRunHandler) storeCheckRun(ctx context.Context, checkRun *github.CheckRun, repo *github.Repository) error {
	logger := clog.FromContext(ctx)

	// Check if database is available
	if h.DB == nil {
		logger.Warn("Database not available, skipping check run storage")
		return errors.New("database not available")
	}

	// Ensure repository exists
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO repositories (id, owner, name, full_name, default_branch)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP
	`, repo.GetID(), repo.GetOwner().GetLogin(), repo.GetName(), repo.GetFullName(), repo.GetDefaultBranch())
	if err != nil {
		return errors.Wrap(err, "failed to upsert repository")
	}

	// Store check run details
	_, err = h.DB.ExecContext(ctx, `
		INSERT INTO check_runs (
			id, repository_id, name, status, conclusion,
			html_url, details_url, started_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion,
			completed_at = EXCLUDED.completed_at
	`,
		checkRun.GetID(),
		repo.GetID(),
		checkRun.GetName(),
		checkRun.GetStatus(),
		checkRun.GetConclusion(),
		checkRun.GetHTMLURL(),
		checkRun.GetDetailsURL(),
		checkRun.GetStartedAt().Time,
		checkRun.GetCompletedAt().Time,
	)
	if err != nil {
		return errors.Wrap(err, "failed to insert check run")
	}

	logger.Info("Stored check run",
		"check_run_id", checkRun.GetID(),
		"name", checkRun.GetName(),
		"conclusion", checkRun.GetConclusion())

	return nil
}
