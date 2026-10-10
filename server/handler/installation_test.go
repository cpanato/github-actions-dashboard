// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/chainguard-dev/clog"
	"github.com/google/go-github/v92/github"
)

func TestInstallationHandler_Handles(t *testing.T) {
	handler := &InstallationHandler{}
	expected := []string{"installation", "installation_repositories"}

	got := handler.Handles()

	if len(got) != len(expected) {
		t.Errorf("Handles() = %v, want %v", got, expected)
		return
	}

	for i, event := range expected {
		if got[i] != event {
			t.Errorf("Handles()[%d] = %v, want %v", i, got[i], event)
		}
	}
}

func TestInstallationHandler_Handle_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	handler := &InstallationHandler{}

	tests := []struct {
		name      string
		eventType string
		payload   []byte
	}{
		{
			name:      "invalid installation event",
			eventType: "installation",
			payload:   []byte(`{"invalid": "json"`),
		},
		{
			name:      "invalid installation_repositories event",
			eventType: "installation_repositories",
			payload:   []byte(`{"action": "added"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.Handle(ctx, tt.eventType, "delivery-123", tt.payload)
			if err == nil {
				t.Error("Handle() should return error for invalid payload")
			}
		})
	}
}

func TestInstallationHandler_Handle_IgnoredActions(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	handler := &InstallationHandler{}

	event := github.InstallationEvent{
		Action: new("suspend"),
		Installation: &github.Installation{
			ID: new(int64(12345)),
			Account: &github.User{
				Login: new("test-org"),
			},
		},
	}

	payload, _ := json.Marshal(event)
	err := handler.Handle(ctx, "installation", "delivery-123", payload)

	if err != nil {
		t.Errorf("Handle() error = %v", err)
	}
}
