// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/cpanato/github-actions-dashboard/server/auth"
	"github.com/cpanato/github-actions-dashboard/server/handler"
	"github.com/cpanato/github-actions-dashboard/server/websocket"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/palantir/go-githubapp/oauth2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// Server represents the dashboard application server
type Server struct {
	config        *Config
	db            *sql.DB
	handlers      []githubapp.EventHandler
	base          *handler.Base
	wsHub         *websocket.Hub
	sessions      *scs.SessionManager
	clientCreator githubapp.ClientCreator
	tmpl          *template.Template
}

// New creates a new Server instance
func New(config *Config) (*Server, error) {
	ctx := context.Background()
	logger := zerolog.Ctx(ctx)

	// Initialize database
	db, err := InitDatabase(&config.Database)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize database")
	}

	// Create GitHub App client creator
	cc, err := githubapp.NewDefaultCachingClientCreator(
		config.Github,
		githubapp.WithClientUserAgent("github-actions-dashboard/1.0"),
		githubapp.WithClientTimeout(30),
	)
	if err != nil {
		db.Close()
		return nil, errors.Wrap(err, "failed to create client creator")
	}

	// Create session manager
	sessionLifetime := 86400 // 24 hours
	if config.Sessions.Lifetime > 0 {
		sessionLifetime = config.Sessions.Lifetime
	}
	sessions, err := auth.NewSessionManager(ctx, config.Sessions.SecretKey, sessionLifetime)
	if err != nil {
		db.Close()
		return nil, errors.Wrap(err, "failed to create session manager")
	}

	// Create WebSocket hub
	wsHub := websocket.NewHub()

	// Create base handler with shared dependencies
	base := &handler.Base{
		ClientCreator: cc,
		Installations: nil, // Not using installations service
		BaseConfig:    &config.Server,
		Options: &handler.DashboardOptions{
			EnableRealtime:    config.Options.EnableRealtime,
			DefaultTimeWindow: config.Options.DefaultTimeWindow,
			MaxReposPerPage:   config.Options.MaxReposPerPage,
		},
		AppName: "github-actions-dashboard",
		DB:      db,
		WSHub:   wsHub,
	}

	// Create webhook handlers
	handlers := []githubapp.EventHandler{
		&handler.WorkflowRunHandler{Base: *base},
		&handler.CheckRunHandler{Base: *base},
		&handler.InstallationHandler{Base: *base},
	}

	logger.Info().Msg("Session-based authentication enabled")

	// Load templates
	templatesPath := filepath.Join(os.Getenv("KO_DATA_PATH"), "templates")
	if _, err := os.Stat(templatesPath); os.IsNotExist(err) {
		templatesPath = "kodata/templates"
	}

	// Define custom template functions
	funcMap := template.FuncMap{
		"divf": func(a, b float64) float64 { return a / b },
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseGlob(filepath.Join(templatesPath, "*.html"))
	if err != nil {
		db.Close()
		return nil, errors.Wrap(err, "failed to parse templates")
	}

	// Parse partials with same FuncMap
	tmpl, err = tmpl.Funcs(funcMap).ParseGlob(filepath.Join(templatesPath, "partials", "*.html"))
	if err != nil {
		db.Close()
		return nil, errors.Wrap(err, "failed to parse partial templates")
	}

	return &Server{
		config:        config,
		db:            db,
		handlers:      handlers,
		base:          base,
		wsHub:         wsHub,
		sessions:      sessions,
		clientCreator: cc,
		tmpl:          tmpl,
	}, nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	logger := zerolog.Ctx(context.Background()).With().
		Str("component", "server").
		Logger()

	// Start WebSocket hub
	ctx := logger.WithContext(context.Background())
	if s.config.Options.EnableRealtime && s.wsHub != nil {
		go s.wsHub.Run(ctx)
		logger.Info().Msg("WebSocket hub started")
	}

	// Create HTTP mux
	mux := http.NewServeMux()

	// Logger middleware - adds logger to all request contexts
	loggerMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLogger := logger.With().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Logger()
			ctx := reqLogger.WithContext(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// Session middleware - used for routes that need session access
	sessionMiddleware := s.sessions.LoadAndSave

	// GitHub OAuth routes (MUST be registered first, before any protected routes)
	// This route is unprotected but needs session access to store auth data
	basePath := ""

	// Create OAuth config with explicit redirect URL if public_url is set
	oauthConfig := oauth2.GetConfig(s.config.Github, nil)
	if s.config.Server.PublicURL != "" {
		// Override redirect URL to use public URL
		oauthConfig.RedirectURL = s.config.Server.PublicURL + "/login/callback"
		logger.Info().
			Str("public_url", s.config.Server.PublicURL).
			Str("redirect_url", oauthConfig.RedirectURL).
			Msg("Using explicit OAuth redirect URL from config")
	}

	oauthHandler := oauth2.NewHandler(
		oauthConfig,
		oauth2.OnLogin(auth.LoginCallback(s.config.Github, basePath, s.sessions)),
	)
	// Register at /login/callback to match GitHub App callback URL configuration
	mux.Handle("/login/callback", loggerMiddleware(sessionMiddleware(oauthHandler)))
	logger.Info().Str("path", "/login/callback").Msg("OAuth callback route registered (unprotected)")

	// Also register the OAuth initiation route
	mux.Handle("/api/github/auth", loggerMiddleware(sessionMiddleware(oauthHandler)))
	logger.Info().Str("path", "/api/github/auth").Msg("OAuth initiation route registered (unprotected)")

	// GitHub webhook endpoint (unprotected, no sessions needed)
	webhookHandler := githubapp.NewEventDispatcher(s.handlers, s.config.Github.App.WebhookSecret, githubapp.WithScheduler(
		githubapp.QueueAsyncScheduler(100, 100),
	))
	mux.Handle("/api/github/hook", webhookHandler)

	// Health check endpoint (unprotected, no sessions needed)
	mux.HandleFunc("/api/health", s.handleHealth)

	// Protected routes - require authentication
	requireAuth := auth.RequireLogin(s.sessions, basePath)

	// WebSocket endpoint (protected)
	if s.config.Options.EnableRealtime && s.wsHub != nil {
		mux.Handle("/ws", requireAuth(http.HandlerFunc(s.wsHub.ServeWS)))
		logger.Info().Msg("WebSocket endpoint enabled at /ws (protected)")
	}

	// Static assets (unprotected, but could be protected if desired)
	staticDir := filepath.Join(os.Getenv("KO_DATA_PATH"), "static")
	if _, err := os.Stat(staticDir); err == nil {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	} else {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("kodata/static"))))
	}

	// Dashboard UI endpoints (protected)
	// IMPORTANT: Register / LAST as it's a catch-all pattern
	mux.Handle("/api/repositories", requireAuth(http.HandlerFunc(s.handleRepositories)))
	mux.Handle("/api/workflows", requireAuth(http.HandlerFunc(s.handleWorkflows)))
	mux.Handle("/api/analytics", requireAuth(http.HandlerFunc(s.handleAnalytics)))
	mux.Handle("/api/flaky-tests", requireAuth(http.HandlerFunc(s.handleFlakyTests)))
	mux.Handle("/api/trends", requireAuth(http.HandlerFunc(s.handleTrends)))
	mux.Handle("/repository/", requireAuth(http.HandlerFunc(s.handleRepositoryDetail)))

	// Root handler MUST be last (catch-all)
	mux.Handle("/", requireAuth(http.HandlerFunc(s.handleDashboard)))

	// Determine address
	addr := fmt.Sprintf("%s:%d", s.config.Server.Address, s.config.Server.Port)

	// Start server with logger and session middleware wrapping everything
	logger.Info().
		Str("address", addr).
		Bool("auth_enabled", true).
		Msg("Starting server with authentication")

	return http.ListenAndServe(addr, loggerMiddleware(sessionMiddleware(mux)))
}

// Close gracefully shuts down the server
func (s *Server) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	if err := s.db.Ping(); err != nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","database":"connected"}`))
}

// handleDashboard serves the main dashboard UI
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Try to read from KO_DATA_PATH first (for ko builds), then from local
	templatePath := filepath.Join(os.Getenv("KO_DATA_PATH"), "templates/dashboard.html")
	content, err := os.ReadFile(templatePath)
	if err != nil {
		// Fall back to local file
		content, err = os.ReadFile("kodata/templates/dashboard.html")
		if err != nil {
			http.Error(w, "Template not found", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

// handleRepositories returns repository list
func (s *Server) handleRepositories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	// Get repositories with statistics
	repos, err := GetRepositories(ctx, s.db, true)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get repositories")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Info().Int("count", len(repos)).Msg("Retrieved repositories")
	if len(repos) > 0 {
		logger.Info().
			Str("first_repo", repos[0].FullName).
			Int("total_runs", repos[0].TotalRuns).
			Float64("success_rate", repos[0].SuccessRate).
			Msg("First repository details")
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "repositories.html", map[string]interface{}{
			"repositories": repos,
		}); err != nil {
			logger.Error().Err(err).Msg("Failed to render template")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, map[string]interface{}{"repositories": repos}); err != nil {
		logger.Error().Err(err).Msg("Failed to write JSON response")
	}
}

// handleWorkflows returns workflow runs
func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := zerolog.Ctx(ctx)

	// Parse query parameters
	opts := WorkflowRunOptions{
		Limit: 50, // Default limit
	}

	if repoID := r.URL.Query().Get("repository_id"); repoID != "" {
		fmt.Sscanf(repoID, "%d", &opts.RepositoryID)
	}

	if status := r.URL.Query().Get("status"); status != "" {
		opts.Status = status
	}

	if conclusion := r.URL.Query().Get("conclusion"); conclusion != "" {
		opts.Conclusion = conclusion
	}

	// Parse time window (e.g., "12h", "24h")
	if since := r.URL.Query().Get("since"); since != "" {
		if duration, err := time.ParseDuration(since); err == nil {
			opts.Since = time.Now().Add(-duration)
		}
	} else {
		// Default to configured time window
		if duration, err := time.ParseDuration(s.config.Options.DefaultTimeWindow); err == nil {
			opts.Since = time.Now().Add(-duration)
		}
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		fmt.Sscanf(limit, "%d", &opts.Limit)
	}

	// Get workflow runs
	runs, err := GetWorkflowRuns(ctx, s.db, opts)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get workflow runs")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "workflows.html", map[string]interface{}{
			"workflows": runs,
		}); err != nil {
			logger.Error().Err(err).Msg("Failed to render template")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, map[string]interface{}{"workflows": runs}); err != nil {
		logger.Error().Err(err).Msg("Failed to write JSON response")
	}
}

// writeJSON marshals and writes JSON response
func writeJSON(w http.ResponseWriter, data interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// handleAnalytics returns analytics data
func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	var repoID int64
	if id := r.URL.Query().Get("repository_id"); id != "" {
		fmt.Sscanf(id, "%d", &repoID)
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		fmt.Sscanf(d, "%d", &days)
	}

	analytics, err := GetAnalytics(ctx, s.db, repoID, days)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get analytics")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "analytics.html", map[string]interface{}{
			"analytics": analytics,
		}); err != nil {
			logger.Error().Err(err).Msg("Failed to render analytics template")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, analytics)
}

// handleFlakyTests returns flaky test data
func (s *Server) handleFlakyTests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	var repoID int64
	if id := r.URL.Query().Get("repository_id"); id != "" {
		fmt.Sscanf(id, "%d", &repoID)
	}

	minRuns := 5
	if m := r.URL.Query().Get("min_runs"); m != "" {
		fmt.Sscanf(m, "%d", &minRuns)
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		fmt.Sscanf(d, "%d", &days)
	}

	flakyTests, err := GetFlakyTests(ctx, s.db, repoID, minRuns, days)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get flaky tests")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "flaky-tests.html", map[string]interface{}{
			"flaky_tests": flakyTests,
		}); err != nil {
			logger.Error().Err(err).Msg("Failed to render template")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]interface{}{"flaky_tests": flakyTests})
}

// handleTrends returns trend data
func (s *Server) handleTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	var repoID int64
	if id := r.URL.Query().Get("repository_id"); id != "" {
		fmt.Sscanf(id, "%d", &repoID)
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		fmt.Sscanf(d, "%d", &days)
	}

	trends, err := GetTrendData(ctx, s.db, repoID, days)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get trends")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]interface{}{"trends": trends})
}

func (s *Server) handleRepositoryDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	// Extract owner/repo from path: /repository/owner/repo
	path := strings.TrimPrefix(r.URL.Path, "/repository/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid repository path", http.StatusBadRequest)
		return
	}

	owner := parts[0]
	name := parts[1]
	fullName := owner + "/" + name

	// Get repository
	repos, err := GetRepositories(ctx, s.db, false)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get repositories")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var repo *Repository
	for i := range repos {
		if repos[i].FullName == fullName {
			repo = &repos[i]
			break
		}
	}

	if repo == nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	// Get workflow runs for this repository (last 50)
	runs, err := GetWorkflowRuns(ctx, s.db, WorkflowRunOptions{
		RepositoryID: repo.ID,
		Limit:        50,
	})
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get workflow runs")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Render template
	w.Header().Set("Content-Type", "text/html")
	if err := s.tmpl.ExecuteTemplate(w, "repository.html", map[string]interface{}{
		"repository": repo,
		"runs":       runs,
	}); err != nil {
		logger.Error().Err(err).Msg("Failed to render repository template")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
