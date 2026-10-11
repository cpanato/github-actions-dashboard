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

// InstallationHandler processes installation webhook events
type InstallationHandler struct {
	Base
}

// Handles returns the GitHub event types this handler processes
func (h *InstallationHandler) Handles() []string {
	return []string{"installation", "installation_repositories"}
}

// Handle processes installation webhook events
func (h *InstallationHandler) Handle(ctx context.Context, eventType, _ string, payload []byte) error {
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
	return h.addRepositories(ctx, event.GetInstallation().GetID(), event.Repositories, "installation_update")
}

func (h *InstallationHandler) handleInstallationDeleted(ctx context.Context, event github.InstallationEvent) error {
	id := event.GetInstallation().GetID()
	if err := h.withInstallation(ctx, id, func(tx *sql.Tx, _ bool) error {
		if _, err := tx.ExecContext(ctx, `UPDATE installations SET active=FALSE WHERE id=$1`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE installation_id=$1`, id)
		return err
	}); err != nil {
		return err
	}
	h.broadcastLifecycle(ctx, "installation_update")
	return nil
}

func (h *InstallationHandler) handleRepositoriesAdded(ctx context.Context, event github.InstallationRepositoriesEvent) error {
	return h.addRepositories(ctx, event.GetInstallation().GetID(), event.RepositoriesAdded, "repositories_added")
}

func (h *InstallationHandler) addRepositories(ctx context.Context, id int64, repos []*github.Repository, eventType string) error {
	if err := h.withInstallation(ctx, id, func(tx *sql.Tx, active bool) error {
		// Installation IDs are never reused; delayed created/added events cannot resurrect a deleted installation.
		if !active {
			return nil
		}
		for _, repo := range repos {
			if err := upsertRepository(ctx, tx, id, repo); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO repository_access (installation_id,repository_id) VALUES ($1,$2)
    ON CONFLICT (installation_id,repository_id) DO UPDATE SET active=TRUE`, id, repo.GetID()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	h.broadcastLifecycle(ctx, eventType)
	return nil
}

func (h *InstallationHandler) handleRepositoriesRemoved(ctx context.Context, event github.InstallationRepositoriesEvent) error {
	id := event.GetInstallation().GetID()
	if err := h.withInstallation(ctx, id, func(tx *sql.Tx, _ bool) error {
		for _, repo := range event.RepositoriesRemoved {
			if repo.GetID() <= 0 {
				return errors.New("missing repository ID")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO repository_access (installation_id,repository_id,active) VALUES ($1,$2,FALSE)
    ON CONFLICT (installation_id,repository_id) DO UPDATE SET active=FALSE`, id, repo.GetID()); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE id=$1 AND (installation_id=$2 OR installation_id IS NULL)`, repo.GetID(), id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	h.broadcastLifecycle(ctx, "repositories_removed")
	return nil
}

func (h *InstallationHandler) broadcastLifecycle(ctx context.Context, eventType string) {
	if h.WSHub != nil {
		if err := h.WSHub.Broadcast(eventType, nil); err != nil {
			clog.FromContext(ctx).Warn("Failed to broadcast installation update", "error", err)
		}
	}
}
