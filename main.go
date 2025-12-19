package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chainguard-dev/clog"

	"github.com/cpanato/github-actions-dashboard/server"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "path to configuration file")
	flag.Parse()

	// Setup logging
	if os.Getenv("DASHBOARD_LOGGING_TEXT") == "true" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))
	}

	ctx := context.Background()
	logger := clog.New(slog.Default().Handler()).With("app", "github-actions-dashboard")
	ctx = clog.WithLogger(ctx, logger)

	// Load configuration
	var cfg *server.Config
	var err error

	if *configPath != "" {
		// Load from file
		configBytes, readErr := os.ReadFile(*configPath)
		if readErr != nil {
			logger.Error("Failed to read config file", "error", readErr, "path", *configPath)
			os.Exit(1)
		}
		cfg, err = server.ParseConfig(configBytes)
		if err != nil {
			logger.Error("Failed to parse config", "error", err)
			os.Exit(1)
		}
		logger.Info("Loaded configuration from file", "config", *configPath)
	} else {
		// Use minimal default config for environment-based configuration
		cfg, err = server.ParseConfig([]byte(`
server:
  address: "0.0.0.0"
  port: 8080
logging:
  level: "info"
  text: false
`))
		if err != nil {
			logger.Error("Failed to parse default config", "error", err)
			os.Exit(1)
		}
		logger.Info("Using environment-based configuration")
	}

	// Apply logging configuration
	var logLevel slog.Level
	switch cfg.Logging.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	if cfg.Logging.Text {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		})))
	} else {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		})))
	}

	// Create and start server
	srv, err := server.New(cfg)
	if err != nil {
		logger.Error("Failed to create server", "error", err)
		os.Exit(1)
	}
	defer srv.Close()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutting down gracefully...")
		srv.Close()
		os.Exit(0)
	}()

	// Start server
	logger.Info("Starting GitHub Actions Dashboard")
	if err := srv.Start(); err != nil {
		logger.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
