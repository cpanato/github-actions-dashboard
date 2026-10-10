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

// InstallationHandler processes installation webhook events
type InstallationHandler struct {
	Base
}

// Handles returns the GitHub event types this handler processes
func (h *InstallationHandler) Handles() []string {
	return []string{"installation", "installation_repositories"}
}

// Handle processes installation webhook events
func (h *InstallationHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	logger := clog.FromContext(ctx)

	// Handle installation_repositories events separately
	if eventType == "installation_repositories" {
		var event github.InstallationRepositoriesEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return errors.Wrap(err, "failed to unmarshal installation_repositories event")
		}

		action := event.GetAction()
		logger.Info("Processing installation_repositories event",
			"action", action,
			"repos_count", len(event.RepositoriesAdded)+len(event.RepositoriesRemoved))

		switch action {
		case "added":
			return h.handleRepositoriesAdded(ctx, event)
		case "removed":
			return h.handleRepositoriesRemoved(ctx, event)
		default:
			logger.Info("Ignoring installation_repositories action", "action", action)
		}
		return nil
	}

	// Handle regular installation events
	var event github.InstallationEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to unmarshal installation event")
	}

	action := event.GetAction()
	installation := event.GetInstallation()

	logger.Info("Processing installation event",
		"action", action,
		"installation_id", installation.GetID(),
		"account", installation.GetAccount().GetLogin())

	switch action {
	case "created":
		return h.handleInstallationCreated(ctx, event)
	case "deleted":
		return h.handleInstallationDeleted(ctx, event)
	default:
		logger.Info("Ignoring installation action", "action", action)
	}

	return nil
}

func (h *InstallationHandler) handleInstallationCreated(ctx context.Context, event github.InstallationEvent) error {
	logger := clog.FromContext(ctx)

	// Add all repositories from the installation
	for _, repo := range event.Repositories {
		_, err := h.DB.ExecContext(ctx, `
			INSERT INTO repositories (id, owner, name, full_name, default_branch)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP
		`, repo.GetID(), event.Installation.GetAccount().GetLogin(), repo.GetName(), repo.GetFullName(), "main")
		if err != nil {
			logger.Error("Failed to add repository", "error", err, "repo", repo.GetFullName())
			continue
		}
		logger.Info("Added repository from installation", "repository", repo.GetFullName())
	}

	if h.WSHub != nil {
		h.WSHub.Broadcast("installation_update", map[string]interface{}{
			"action": "created",
			"count":  len(event.Repositories),
		})
	}

	return nil
}

func (h *InstallationHandler) handleInstallationDeleted(ctx context.Context, event github.InstallationEvent) error {
	logger := clog.FromContext(ctx)

	// Optionally: Mark repositories as inactive or remove them
	// For now, we'll just log it
	logger.Info("Installation deleted - repositories will remain in database",
		"installation_id", event.Installation.GetID())

	if h.WSHub != nil {
		h.WSHub.Broadcast("installation_update", map[string]interface{}{
			"action": "deleted",
		})
	}

	return nil
}

func (h *InstallationHandler) handleRepositoriesAdded(ctx context.Context, event github.InstallationRepositoriesEvent) error {
	logger := clog.FromContext(ctx)

	for _, repo := range event.RepositoriesAdded {
		_, err := h.DB.ExecContext(ctx, `
			INSERT INTO repositories (id, owner, name, full_name, default_branch)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP
		`, repo.GetID(), event.Installation.GetAccount().GetLogin(), repo.GetName(), repo.GetFullName(), "main")
		if err != nil {
			logger.Error("Failed to add repository", "error", err, "repo", repo.GetFullName())
			continue
		}
		logger.Info("Added repository", "repository", repo.GetFullName())
	}

	if h.WSHub != nil {
		h.WSHub.Broadcast("repositories_added", map[string]interface{}{
			"count": len(event.RepositoriesAdded),
		})
	}

	return nil
}

func (h *InstallationHandler) handleRepositoriesRemoved(ctx context.Context, event github.InstallationRepositoriesEvent) error {
	logger := clog.FromContext(ctx)

	// Optionally: Remove or mark as inactive
	logger.Info("Repositories removed from installation",
		"count", len(event.RepositoriesRemoved))

	if h.WSHub != nil {
		h.WSHub.Broadcast("repositories_removed", map[string]interface{}{
			"count": len(event.RepositoriesRemoved),
		})
	}

	return nil
}
