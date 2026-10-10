package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cpanato/github-actions-dashboard/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Dashboard stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse command line flags
	configPath := flag.String("config", "", "path to configuration file")
	flag.Parse()

	// Setup logging
	if os.Getenv("DASHBOARD_LOGGING_TEXT") == "true" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))
	}

	logger := slog.Default().With("app", "github-actions-dashboard")

	// Load configuration
	var cfg *server.Config
	var err error

	if *configPath != "" {
		// Load from file
		configBytes, readErr := os.ReadFile(*configPath)
		if readErr != nil {
			return fmt.Errorf("failed to read config file %q: %w", *configPath, readErr)
		}
		cfg, err = server.ParseConfig(configBytes)
		if err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
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
			return fmt.Errorf("failed to parse default config: %w", err)
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
	logger = slog.Default().With("app", "github-actions-dashboard")

	// Create and start server
	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	defer srv.Close()

	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down gracefully...")
		if err := srv.Close(); err != nil {
			logger.Error("Failed to shut down server", "error", err)
		}
	}()

	// Start server
	logger.Info("Starting GitHub Actions Dashboard")
	if err := srv.Start(); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}
	return nil
}
