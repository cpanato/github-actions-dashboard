// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"encoding/hex"
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

func TestValidateDemoMode(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		publicURL string
		demoMode  bool
		wantErr   bool
	}{
		{name: "loopback IP", address: "127.0.0.1", demoMode: true},
		{name: "localhost", address: "localhost", demoMode: true},
		{name: "demo disabled on public bind", address: "0.0.0.0"},
		{name: "reject wildcard bind", address: "0.0.0.0", demoMode: true, wantErr: true},
		{name: "reject public URL", address: "127.0.0.1", publicURL: "https://example.com", demoMode: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{DemoMode: tt.demoMode}
			config.Server.Address = tt.address
			config.Server.PublicURL = tt.publicURL

			err := validateDemoMode(config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDemoMode() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
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

func TestBuildWorkflowGridGroupsRunsByCommit(t *testing.T) {
	runs := []WorkflowRun{
		{ID: 1, WorkflowName: "Build", HeadSHA: "commit-a", RunNumber: 10},
		{ID: 2, WorkflowName: "Lint", HeadSHA: "commit-a", RunNumber: 10},
		{ID: 3, WorkflowName: "Build", HeadSHA: "commit-b", RunNumber: 9},
		{ID: 4, WorkflowName: "Tests", HeadSHA: "commit-c", RunNumber: 8},
	}

	columns, rows, order := buildWorkflowGrid(runs, 2)
	if len(columns) != 2 || columns[0].HeadSHA != "commit-a" || columns[1].HeadSHA != "commit-b" {
		t.Fatalf("unexpected commit columns: %+v", columns)
	}
	if len(rows) != 2 || len(order) != 2 || order[0] != "Build" || order[1] != "Lint" {
		t.Fatalf("expected workflows present in included commits only: order=%v rows=%+v", order, rows)
	}
	if len(rows[0].Cells) != 2 || rows[0].Cells[0] == nil || rows[0].Cells[1] == nil {
		t.Fatalf("Build should have a cell for each commit: %+v", rows[0].Cells)
	}

	columns, rows, order = buildWorkflowGrid(runs[:3], 10)
	if len(columns) != 2 {
		t.Fatalf("runs on one commit should share a column, got %d columns", len(columns))
	}
	if len(order) != 2 || len(rows) != 2 {
		t.Fatalf("expected two workflow rows, got order=%v rows=%d", order, len(rows))
	}
	if rows[1].Cells[1] != nil {
		t.Fatalf("Lint should be absent from commit-b, got %+v", rows[1].Cells[1])
	}
}

func TestDemoCommitSHA(t *testing.T) {
	first := demoCommitSHA(0)
	if len(first) != 40 {
		t.Fatalf("demoCommitSHA() length = %d, want 40", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("demoCommitSHA() returned invalid hex: %v", err)
	}
	if got := demoCommitSHA(0); got != first {
		t.Fatalf("demoCommitSHA() is not stable: first %q, second %q", first, got)
	}
	if got := demoCommitSHA(1); got == first {
		t.Fatalf("different commits received the same SHA %q", first)
	}
	if first[:8] == "00000000" {
		t.Fatalf("demoCommitSHA() looks zero-padded: %q", first)
	}
}
