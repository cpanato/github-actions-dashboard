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

func TestWorkflowRunHandler_Handles(t *testing.T) {
	handler := &WorkflowRunHandler{}
	expected := []string{"workflow_run"}
	
	got := handler.Handles()
	
	if len(got) != len(expected) {
		t.Errorf("Handles() = %v, want %v", got, expected)
		return
	}
	
	if got[0] != expected[0] {
		t.Errorf("Handles()[0] = %v, want %v", got[0], expected[0])
	}
}

func TestWorkflowRunHandler_Handle_NonCompleted(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	handler := &WorkflowRunHandler{}

	actions := []string{"requested", "in_progress", "queued"}
	
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			event := github.WorkflowRunEvent{
				Action: github.String(action),
				WorkflowRun: &github.WorkflowRun{
					ID:     github.Int64(123),
					Name:   github.String("test"),
					Status: github.String(action),
				},
			}
			
			payload, _ := json.Marshal(event)
			err := handler.Handle(ctx, "workflow_run", "delivery-123", payload)
			
			if err != nil {
				t.Errorf("Handle() error = %v", err)
			}
		})
	}
}

func TestWorkflowRunHandler_Handle_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	handler := &WorkflowRunHandler{}

	invalidPayload := []byte(`{"invalid": "json"`)
	err := handler.Handle(ctx, "workflow_run", "delivery-123", invalidPayload)
	
	if err == nil {
		t.Error("Handle() should return error for invalid payload")
	}
}
