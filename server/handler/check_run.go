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

// CheckRunHandler processes check_run webhook events
type CheckRunHandler struct {
	Base
}

// Handles returns the GitHub event types this handler processes
func (h *CheckRunHandler) Handles() []string {
	return []string{"check_run"}
}

// Handle processes a check_run webhook event
func (h *CheckRunHandler) Handle(ctx context.Context, _, _ string, payload []byte) error {
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
	stored, err := h.storeCheckRun(ctx, checkRun, repo, event.GetInstallation().GetID())
	if err != nil {
		logger.Error("Failed to store check run", "error", err)
		return err
	}

	if !stored {
		return nil
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
		if err := h.WSHub.Broadcast("check_run_update", update); err != nil {
			logger.Warn("Failed to broadcast check run update", "error", err)
		}
	}

	return nil
}

// storeCheckRun atomically stores an event only while installation access is active.
func (h *CheckRunHandler) storeCheckRun(ctx context.Context, checkRun *github.CheckRun, repo *github.Repository, installationID int64) (bool, error) {
	if checkRun.GetID() <= 0 {
		return false, errors.New("missing run ID")
	}
	return h.withRepository(ctx, installationID, repo, func(tx *sql.Tx) error {
		// Store check run details
		_, err := tx.ExecContext(ctx, `
		INSERT INTO check_runs (
			id, repository_id, name, status, conclusion,
			html_url, details_url, started_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion,
			completed_at = EXCLUDED.completed_at
 WHERE EXCLUDED.completed_at >= check_runs.completed_at
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
		return err
	})
}
