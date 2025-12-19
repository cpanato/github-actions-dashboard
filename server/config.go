// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"fmt"
	"os"
	"time"

	"github.com/palantir/go-baseapp/baseapp"
	"github.com/palantir/go-githubapp/githubapp"
	"gopkg.in/yaml.v2"
)

const (
	DefaultEnvPrefix = "DASHBOARD_"
)

type Config struct {
	Server   baseapp.HTTPConfig `yaml:"server"`
	Logging  LoggingConfig      `yaml:"logging"`
	Github   githubapp.Config   `yaml:"github"`
	Database DatabaseConfig     `yaml:"database"`
	Cache    CacheConfig        `yaml:"cache"`
	Options  DashboardOptions   `yaml:"options"`
	Sessions SessionsConfig     `yaml:"sessions"`
}

type LoggingConfig struct {
	Level string `yaml:"level" json:"level"`
	Text  bool   `yaml:"text" json:"text"`
}

type DatabaseConfig struct {
	// Database driver: "sqlite" or "postgres" (default: sqlite)
	Driver string `yaml:"driver"`

	// Database connection string
	// For SQLite: "file:dashboard.db" or ":memory:" for testing
	// For PostgreSQL: "postgres://user:pass@localhost:5432/dbname?sslmode=disable"
	// For Cloud SQL: auto-generated from CloudSQLInstance
	URL string `yaml:"url"`

	// Cloud SQL Instance connection name (format: project:region:instance)
	// When set, automatically configures Unix socket connection for Cloud Run
	CloudSQLInstance string `yaml:"cloud_sql_instance"`

	// Database name (PostgreSQL only)
	DBName string `yaml:"db_name"`

	// Database user (PostgreSQL only)
	User string `yaml:"user"`

	// Database password (prefer using Secret Manager in production)
	Password string `yaml:"password"`

	// Maximum number of open connections
	MaxOpenConns int `yaml:"max_open_conns"`

	// Maximum number of idle connections
	MaxIdleConns int `yaml:"max_idle_conns"`
}

type CacheConfig struct {
	// Cache duration for workflow runs
	WorkflowTTL time.Duration `yaml:"workflow_ttl"`

	// Maximum cache entries
	MaxEntries int `yaml:"max_entries"`
}

type DashboardOptions struct {
	// Default time window for workflow display (e.g., "12h", "24h")
	DefaultTimeWindow string `yaml:"default_time_window"`

	// Maximum repositories to display per page
	MaxReposPerPage int `yaml:"max_repos_per_page"`

	// Enable real-time updates via WebSocket
	EnableRealtime bool `yaml:"enable_realtime"`
}

type SessionsConfig struct {
	// SecretKey for session encryption (required for production, auto-generated if empty)
	SecretKey string `yaml:"secret_key"`

	// Lifetime in seconds (default: 86400 = 24 hours)
	Lifetime int `yaml:"lifetime"`
}

func ParseConfig(bytes []byte) (*Config, error) {
	var c Config
	if err := yaml.UnmarshalStrict(bytes, &c); err != nil {
		return nil, err
	}

	// Set defaults
	if c.Cache.WorkflowTTL == 0 {
		c.Cache.WorkflowTTL = 15 * time.Minute
	}
	if c.Cache.MaxEntries == 0 {
		c.Cache.MaxEntries = 10000
	}
	if c.Options.DefaultTimeWindow == "" {
		c.Options.DefaultTimeWindow = "12h"
	}
	if c.Options.MaxReposPerPage == 0 {
		c.Options.MaxReposPerPage = 50
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}

	// Default to SQLite for easy local development
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.URL == "" && c.Database.Driver == "sqlite" {
		c.Database.URL = "file:dashboard.db?cache=shared&mode=rwc"
	}

	// Load from environment variables
	envPrefix := DefaultEnvPrefix
	if v, ok := os.LookupEnv("DASHBOARD_ENV_PREFIX"); ok {
		envPrefix = v
	}

	c.Github.SetValuesFromEnv(envPrefix)

	// Database configuration from environment
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_DRIVER"); ok {
		c.Database.Driver = v
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_URL"); ok {
		c.Database.URL = v
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_CLOUD_SQL_INSTANCE"); ok {
		c.Database.CloudSQLInstance = v
		c.Database.Driver = "postgres" // Cloud SQL is always PostgreSQL
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_NAME"); ok {
		c.Database.DBName = v
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_USER"); ok {
		c.Database.User = v
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_PASSWORD"); ok {
		c.Database.Password = v
	}

	// Build Cloud SQL connection string if not provided
	if c.Database.URL == "" && c.Database.CloudSQLInstance != "" {
		c.Database.URL = buildCloudSQLConnectionString(&c.Database)
	}

	return &c, nil
}

// buildCloudSQLConnectionString creates a PostgreSQL connection string for Cloud SQL
// When running on Cloud Run, this uses Unix socket connection for better performance
func buildCloudSQLConnectionString(cfg *DatabaseConfig) string {
	if cfg.CloudSQLInstance == "" {
		return ""
	}

	// Cloud Run uses Unix socket connection to Cloud SQL
	// The socket is available at /cloudsql/project:region:instance
	return fmt.Sprintf(
		"host=/cloudsql/%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.CloudSQLInstance,
		cfg.User,
		cfg.Password,
		cfg.DBName,
	)
}
