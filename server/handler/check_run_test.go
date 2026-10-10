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

func TestCheckRunHandler_Handles(t *testing.T) {
	handler := &CheckRunHandler{}
	expected := []string{"check_run"}

	got := handler.Handles()

	if len(got) != len(expected) {
		t.Errorf("Handles() = %v, want %v", got, expected)
		return
	}

	if got[0] != expected[0] {
		t.Errorf("Handles()[0] = %v, want %v", got[0], expected[0])
	}
}

func TestCheckRunHandler_Handle_NonCompleted(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	handler := &CheckRunHandler{}

	actions := []string{"created", "requested_action"}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			event := github.CheckRunEvent{
				Action: new(action),
				CheckRun: &github.CheckRun{
					ID:     new(int64(123)),
					Name:   new("test"),
					Status: new(action),
				},
			}

			payload, _ := json.Marshal(event)
			err := handler.Handle(ctx, "check_run", "delivery-123", payload)

			if err != nil {
				t.Errorf("Handle() error = %v", err)
			}
		})
	}
}

func TestCheckRunHandler_Handle_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	handler := &CheckRunHandler{}

	invalidPayload := []byte(`{"invalid": "json"`)
	err := handler.Handle(ctx, "check_run", "delivery-123", invalidPayload)

	if err == nil {
		t.Error("Handle() should return error for invalid payload")
	}
}
