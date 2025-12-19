// Copyright 2025

package handler

import (
	"database/sql"

	"github.com/cpanato/github-actions-dashboard/server/websocket"
	"github.com/palantir/go-baseapp/baseapp"
	"github.com/palantir/go-githubapp/githubapp"
)

// Base provides common dependencies for all handlers
type Base struct {
	githubapp.ClientCreator

	Installations githubapp.InstallationsService
	BaseConfig    *baseapp.HTTPConfig
	Options       *DashboardOptions

	// Database connection
	DB *sql.DB

	// WebSocket hub for real-time updates
	WSHub *websocket.Hub

	AppName string
}

// DashboardOptions contains runtime configuration
type DashboardOptions struct {
	EnableRealtime    bool
	DefaultTimeWindow string
	MaxReposPerPage   int
}
