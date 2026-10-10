// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
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
	DemoMode bool               `yaml:"demo_mode"`
}

type LoggingConfig struct {
	Level string `yaml:"level" json:"level"`
	Text  bool   `yaml:"text" json:"text"`
}

type DatabaseConfig struct {
	// Database driver: "postgres" (default: postgres)
	Driver string `yaml:"driver"`

	// Database connection string
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
	// Enable real-time updates by default
	if !c.Options.EnableRealtime {
		c.Options.EnableRealtime = true
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}

	// Load from environment variables
	envPrefix := DefaultEnvPrefix
	if v, ok := os.LookupEnv("DASHBOARD_ENV_PREFIX"); ok {
		envPrefix = v
	}

	c.Github.SetValuesFromEnv(envPrefix)

	// Set GitHub defaults if not provided
	if c.Github.WebURL == "" {
		c.Github.WebURL = "https://github.com"
	}
	if c.Github.V3APIURL == "" {
		c.Github.V3APIURL = "https://api.github.com"
	}

	// Server configuration from environment
	if v, ok := os.LookupEnv(envPrefix + "PUBLIC_URL"); ok {
		c.Server.PublicURL = v
		slog.Debug("Loaded public URL from environment")
	}

	// Database configuration from environment
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_DRIVER"); ok {
		c.Database.Driver = v
		slog.Debug("Loaded database driver from environment")
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_URL"); ok {
		c.Database.URL = v
		slog.Debug("Loaded database URL from environment")
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_CLOUD_SQL_INSTANCE"); ok {
		c.Database.CloudSQLInstance = v
		c.Database.Driver = "postgres" // Cloud SQL is always PostgreSQL
		slog.Debug("Loaded Cloud SQL instance from environment")
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_NAME"); ok {
		c.Database.DBName = v
		slog.Debug("Loaded database name from environment")
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_USER"); ok {
		c.Database.User = v
		slog.Debug("Loaded database user from environment")
	}
	if v, ok := os.LookupEnv(envPrefix + "DATABASE_PASSWORD"); ok {
		c.Database.Password = v
		slog.Debug("Loaded database password from environment")
	}

	// Build Cloud SQL connection string if not provided
	if c.Database.URL == "" && c.Database.CloudSQLInstance != "" {
		c.Database.URL = buildCloudSQLConnectionString(&c.Database)
		slog.Debug("Built Cloud SQL connection string")
	}

	// Default to PostgreSQL
	if c.Database.Driver == "" {
		c.Database.Driver = "postgres"
	}

	return &c, nil
}

func validateDemoMode(config *Config) error {
	if !config.DemoMode {
		return nil
	}

	address := strings.Trim(config.Server.Address, "[]")
	ip := net.ParseIP(address)
	if !strings.EqualFold(address, "localhost") && (ip == nil || !ip.IsLoopback() || ip.To4() == nil) {
		return fmt.Errorf("demo mode requires a loopback server address, got %q", config.Server.Address)
	}
	if config.Server.PublicURL != "" {
		return fmt.Errorf("demo mode cannot be used with a public URL")
	}
	return nil
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
