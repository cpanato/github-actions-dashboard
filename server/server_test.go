// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"testing"
	"time"
)

func TestParseConfig_ValidYAML(t *testing.T) {
	configYAML := []byte(`
server:
  address: "0.0.0.0"
  port: 8080
  public_url: "https://example.com"

database:
  url: "./test.db"

github:
  app:
    integration_id: 123456
    webhook_secret: "test-secret"
    private_key: "test-key"
  oauth:
    client_id: "test-client"
    client_secret: "test-secret"

sessions:
  secret_key: "test-session-key"
  lifetime: 86400

options:
  enable_realtime: true
  default_time_window: "7d"
  max_repos_per_page: 50
`)

	config, err := ParseConfig(configYAML)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.Server.Address != "0.0.0.0" {
		t.Errorf("Server.Address = %v, want 0.0.0.0", config.Server.Address)
	}

	if config.Server.Port != 8080 {
		t.Errorf("Server.Port = %v, want 8080", config.Server.Port)
	}

	if config.Github.App.IntegrationID != 123456 {
		t.Errorf("Github.App.IntegrationID = %v, want 123456", config.Github.App.IntegrationID)
	}

	if !config.Options.EnableRealtime {
		t.Error("Options.EnableRealtime = false, want true")
	}
}

func TestParseConfig_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`invalid yaml [`)

	_, err := ParseConfig(invalidYAML)
	if err == nil {
		t.Error("ParseConfig() should return error for invalid YAML")
	}
}

func TestRepository_Fields(t *testing.T) {
	repo := Repository{
		ID:            12345,
		Owner:         "test-owner",
		Name:          "test-repo",
		FullName:      "test-owner/test-repo",
		DefaultBranch: "main",
		TotalRuns:     100,
		SuccessRate:   95.5,
		CreatedAt:     time.Now(),
	}

	if repo.ID != 12345 {
		t.Errorf("Repository.ID = %v, want 12345", repo.ID)
	}

	if repo.FullName != "test-owner/test-repo" {
		t.Errorf("Repository.FullName = %v, want test-owner/test-repo", repo.FullName)
	}
}

func TestWorkflowRun_Fields(t *testing.T) {
	now := time.Now()
	run := WorkflowRun{
		ID:           123456,
		WorkflowName: "CI",
		Conclusion:   "success",
		RunNumber:    42,
		CreatedAt:    now,
	}

	if run.ID != 123456 {
		t.Errorf("WorkflowRun.ID = %v, want 123456", run.ID)
	}

	if run.Conclusion != "success" {
		t.Errorf("WorkflowRun.Conclusion = %v, want success", run.Conclusion)
	}
}
