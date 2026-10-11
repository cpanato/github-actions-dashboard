// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/chainguard-dev/clog"
	"github.com/cpanato/github-actions-dashboard/server/auth"
	"github.com/cpanato/github-actions-dashboard/server/handler"
	"github.com/cpanato/github-actions-dashboard/server/websocket"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/palantir/go-githubapp/oauth2"
	"github.com/pkg/errors"
)

// Server represents the dashboard application server
type Server struct {
	config        *Config
	db            *sql.DB
	httpServer    *http.Server
	handlers      []githubapp.EventHandler
	base          *handler.Base
	wsHub         *websocket.Hub
	sessions      *scs.SessionManager
	clientCreator githubapp.ClientCreator
	tmpl          *template.Template
	stats         *statsCache
	ctx           context.Context
	cancel        context.CancelFunc
	ready         atomic.Bool
	mu            sync.Mutex
	closed        bool
	workers       sync.WaitGroup
	webhookInbox  *webhookInbox
	closeOnce     sync.Once
	closeErr      error
}

// New creates a new Server instance
func New(config *Config) (*Server, error) {
	ctx := context.Background()
	logger := clog.FromContext(ctx)
	if err := validateDemoMode(config); err != nil {
		return nil, err
	}

	if !config.DemoMode {
		if err := config.Access.Validate(); err != nil {
			return nil, err
		}
	}

	// Keep the pool stable so HTTP and webhook handlers recover together.
	db, err := OpenDatabase(&config.Database)
	if err != nil {
		return nil, err
	}
	initCtx, initCancel := context.WithTimeout(ctx, 2*time.Second)
	initErr := initializeDatabase(initCtx, db)
	initCancel()
	if config.DemoMode && initErr != nil {
		db.Close()
		db, initErr = InitDatabase(&config.Database)
	}
	if config.DemoMode {
		if initErr != nil {
			return nil, errors.Wrap(initErr, "demo mode requires a working database")
		}
		if err := seedDemoData(ctx, db); err != nil {
			db.Close()
			return nil, errors.Wrap(err, "failed to seed demo data")
		}
	}
	if initErr != nil {
		logger.Warn("Database unavailable; initialization will retry on startup", "error", initErr)
	}

	// Create GitHub App client creator
	cc, err := githubapp.NewDefaultCachingClientCreator(
		config.Github,
		githubapp.WithClientUserAgent("github-actions-dashboard/1.0"),
		githubapp.WithClientTimeout(30),
	)
	if err != nil {
		if db != nil {
			db.Close()
		}
		return nil, errors.Wrap(err, "failed to create client creator")
	}

	// Create session manager
	sessionLifetime := 86400 // 24 hours
	if config.Sessions.Lifetime > 0 {
		sessionLifetime = config.Sessions.Lifetime
	}
	secure := strings.HasPrefix(config.Server.PublicURL, "https://")
	if config.Sessions.Secure != nil {
		secure = *config.Sessions.Secure
	}
	sessions := auth.NewSessionManager(sessionLifetime, secure)

	// Create WebSocket hub
	var wsHub *websocket.Hub
	if config.Options.EnableRealtime {
		wsHub = websocket.NewHub()
	}

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

	logger.Info("Session-based authentication enabled")

	// Load templates
	templatesPath := filepath.Join(os.Getenv("KO_DATA_PATH"), "templates")
	// #nosec G703 -- KO_DATA_PATH is deployment-controlled; template filenames are fixed.
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

	lifetimeCtx, cancel := context.WithCancel(context.Background())
	s := &Server{
		config:        config,
		db:            db,
		handlers:      handlers,
		base:          base,
		wsHub:         wsHub,
		sessions:      sessions,
		clientCreator: cc,
		tmpl:          tmpl,
		ctx:           lifetimeCtx, cancel: cancel,
	}
	s.stats = newStatsCache(lifetimeCtx, config.Cache.StatsTTL, config.Cache.MaxEntries)
	s.ready.Store(initErr == nil)
	s.webhookInbox = newWebhookInbox(db, handlers, 4, s.ready.Load)
	return s, nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	logger := clog.New(clog.FromContext(context.Background()).Handler()).With("component", "server")

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return http.ErrServerClosed
	}
	if !s.ready.Load() {
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.retryDatabaseConnection() }()
	}
	if s.config.Options.EnableRealtime && s.wsHub != nil {
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.wsHub.Run(s.ctx) }()
	}
	s.mu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return http.ErrServerClosed
	}
	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.config.Server.Address, s.config.Server.Port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second,
	}
	httpServer := s.httpServer
	s.mu.Unlock()
	logger.Info("Starting server", "address", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler returns the application routes for serving or integration testing.
func (s *Server) Handler() http.Handler {
	logger := clog.FromContext(context.Background())

	// Create HTTP mux
	mux := http.NewServeMux()

	// Logger middleware - adds logger to all request contexts
	loggerMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLogger := clog.New(logger.Handler()).With(
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			)
			ctx := clog.WithLogger(r.Context(), reqLogger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// Session middleware - used for routes that need session access
	sessionMiddleware := s.sessions.LoadAndSave

	// GitHub OAuth routes (MUST be registered first, before any protected routes)
	// This route is unprotected but needs session access to store auth data
	basePath := ""

	if s.config.DemoMode {
		logger.Warn("Demo mode enabled: OAuth disabled; dashboard access is limited to loopback")
	} else {
		// Create OAuth config with explicit redirect URL if public_url is set
		oauthConfig := oauth2.GetConfig(s.config.Github, nil)
		if s.config.Server.PublicURL != "" {
			// Override redirect URL to use public URL
			oauthConfig.RedirectURL = s.config.Server.PublicURL + "/login/callback"
			logger.Info("Using explicit OAuth redirect URL from config",
				"public_url", s.config.Server.PublicURL,
				"redirect_url", oauthConfig.RedirectURL)
		}

		oauthHandler := oauth2.NewHandler(
			oauthConfig,
			oauth2.OnLogin(auth.LoginCallback(s.config.Github, basePath, s.sessions, s.config.Access)),
		)
		mux.Handle("/login/callback", loggerMiddleware(sessionMiddleware(oauthHandler)))
		logger.Info("OAuth callback route registered (unprotected)", "path", "/login/callback")

		mux.Handle("/api/github/auth", loggerMiddleware(sessionMiddleware(oauthHandler)))
		logger.Info("OAuth initiation route registered (unprotected)", "path", "/api/github/auth")
	}

	// GitHub webhook endpoint (unprotected, no sessions needed)
	webhookHandler := githubapp.NewEventDispatcher(s.handlers, s.config.Github.App.WebhookSecret,
		githubapp.WithScheduler(s.webhookInbox), githubapp.WithErrorCallback(func(w http.ResponseWriter, r *http.Request, err error) {
			switch {
			case errors.Is(err, errInboxUnavailable):
				logger.Error("Failed to persist webhook", "error", err)
				http.Error(w, "Webhook inbox unavailable", http.StatusServiceUnavailable)
			case errors.Is(err, errDeliveryConflict):
				http.Error(w, errDeliveryConflict.Error(), http.StatusConflict)
			default:
				var oversized *http.MaxBytesError
				if errors.As(err, &oversized) {
					http.Error(w, "Webhook payload too large", http.StatusRequestEntityTooLarge)
					return
				}
				githubapp.DefaultErrorCallback(w, r, err)
			}
		}))
	mux.Handle("/api/github/hook", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err != nil {
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				http.Error(w, "Webhook payload too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "Cannot read webhook payload", http.StatusBadRequest)
			}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(payload))
		webhookHandler.ServeHTTP(w, r)
	}))

	// Health check endpoint (unprotected, no sessions needed)
	mux.HandleFunc("/api/health", s.handleHealth)

	// Protected routes - require authentication
	requireAuth := auth.RequireLogin(s.sessions, basePath)
	if s.config.DemoMode {
		requireAuth = func(next http.Handler) http.Handler { return next }
	}

	// WebSocket endpoint (protected)
	if s.config.Options.EnableRealtime && s.wsHub != nil {
		wsHandler := requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var expiresAt time.Time
			if !s.config.DemoMode {
				expiresAt = s.sessions.Deadline(r.Context())
			}
			s.wsHub.ServeWSUntil(w, r, expiresAt)
		}))
		mux.Handle("/ws", wsHandler)
		logger.Info("WebSocket endpoint enabled at /ws and /ws/", "auth_enabled", !s.config.DemoMode)
	}

	// Static assets (unprotected, but could be protected if desired)
	staticDir := filepath.Join(os.Getenv("KO_DATA_PATH"), "static")
	// #nosec G703 -- KO_DATA_PATH is deployment-controlled; static paths are fixed.
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
	mux.Handle("/api/webhooks/metrics", requireAuth(http.HandlerFunc(s.handleWebhookMetrics)))
	mux.Handle("/repository/", requireAuth(http.HandlerFunc(s.handleRepositoryDetail)))

	// Root handler MUST be last (catch-all)
	mux.Handle("/", requireAuth(http.HandlerFunc(s.handleDashboard)))

	return loggerMiddleware(sessionMiddleware(mux))
}

// Close cancels background work and shuts down the server exactly once.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
		httpServer := s.httpServer
		s.mu.Unlock()
		if httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.closeErr = httpServer.Shutdown(ctx)
		}
		if s.webhookInbox != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.webhookInbox.Close(ctx); s.closeErr == nil {
				s.closeErr = err
			}
		}
		s.workers.Wait()
		if s.db != nil {
			if err := s.db.Close(); s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

func (s *Server) requireDatabase(w http.ResponseWriter) bool {
	if s.db == nil || !s.ready.Load() {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	logger := clog.FromContext(r.Context())
	// Check database connection
	if !s.requireDatabase(w) {
		return
	}
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, `{"status":"healthy","database":"connected"}`); err != nil {
		logger.Warn("Failed to write health response", "error", err)
	}
}

// handleDashboard serves the main dashboard UI
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := s.tmpl.ExecuteTemplate(w, "dashboard.html", map[string]interface{}{"enableRealtime": s.config.Options.EnableRealtime}); err != nil {
		clog.FromContext(r.Context()).Error("Failed to render dashboard", "error", err)
	}
}

// handleRepositories returns repository list
func (s *Server) handleRepositories(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabase(w) {
		return
	}
	ctx := r.Context()
	logger := clog.FromContext(ctx)

	// Get repositories with statistics
	repos, err := cachedStats(ctx, s.stats, "repositories", func(ctx context.Context) ([]Repository, error) {
		return GetRepositories(ctx, s.db, true)
	})
	if err != nil {
		logger.Error("Failed to get repositories", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Info("Retrieved repositories", "count", len(repos))
	if len(repos) > 0 {
		logger.Info("First repository details",
			"first_repo", repos[0].FullName,
			"total_runs", repos[0].TotalRuns,
			"success_rate", repos[0].SuccessRate)
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "repositories.html", map[string]interface{}{
			"repositories": repos,
		}); err != nil {
			logger.Error("Failed to render template", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, map[string]interface{}{"repositories": repos}); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
	}
}

// handleWorkflows returns workflow runs
func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabase(w) {
		return
	}
	ctx := r.Context()

	logger := clog.FromContext(ctx)

	opts, err := parseWorkflowQuery(r, s.config.Options.DefaultTimeWindow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get workflow runs
	runs, err := GetWorkflowRuns(ctx, s.db, opts)
	if err != nil {
		logger.Error("Failed to get workflow runs", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "workflows.html", map[string]interface{}{
			"workflows": runs,
			"count":     len(runs),
		}); err != nil {
			logger.Error("Failed to render template", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, map[string]interface{}{"workflows": runs, "limit": opts.Limit, "offset": opts.Offset}); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
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
	if !s.requireDatabase(w) {
		return
	}
	ctx := r.Context()
	logger := clog.FromContext(ctx)

	repoID, days, err := parseMetricQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	analytics, err := cachedStats(ctx, s.stats, fmt.Sprintf("analytics:%d:%d", repoID, days), func(ctx context.Context) (*Analytics, error) {
		return GetAnalytics(ctx, s.db, repoID, days)
	})
	if err != nil {
		logger.Error("Failed to get analytics", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "analytics.html", map[string]interface{}{
			"analytics": analytics,
		}); err != nil {
			logger.Error("Failed to render analytics template", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, analytics); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
	}
}

// handleFlakyTests returns flaky test data
func (s *Server) handleFlakyTests(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabase(w) {
		return
	}
	ctx := r.Context()
	logger := clog.FromContext(ctx)

	repoID, days, err := parseMetricQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	minRuns, err := parseQueryInt(r, "min_runs", 5, 1, 10000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	flakyTests, err := cachedStats(ctx, s.stats, fmt.Sprintf("flaky:%d:%d:%d", repoID, days, minRuns), func(ctx context.Context) ([]FlakyTest, error) {
		return GetFlakyTests(ctx, s.db, repoID, minRuns, days)
	})
	if err != nil {
		logger.Error("Failed to get flaky tests", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request (expects HTML partial)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		if err := s.tmpl.ExecuteTemplate(w, "flaky-tests.html", map[string]interface{}{
			"flaky_tests": flakyTests,
		}); err != nil {
			logger.Error("Failed to render template", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fallback to JSON for API requests
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, map[string]interface{}{"flaky_tests": flakyTests}); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
	}
}

// handleTrends returns trend data
func (s *Server) handleTrends(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabase(w) {
		return
	}
	ctx := r.Context()
	logger := clog.FromContext(ctx)

	repoID, days, err := parseMetricQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	trends, err := GetTrendData(ctx, s.db, repoID, days)
	if err != nil {
		logger.Error("Failed to get trends", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, map[string]interface{}{"trends": trends}); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
	}
}

func (s *Server) handleRepositoryDetail(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabase(w) {
		return
	}
	ctx := r.Context()
	logger := clog.FromContext(ctx)

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
		logger.Error("Failed to get repositories", "error", err)
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

	// Parse limit from query parameter (default: 50)
	limit := 50
	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}
	if limit > 500 {
		limit = 500
	}

	// Parse branch filter from query parameter
	branch := r.URL.Query().Get("branch")

	// Parse workflow filter from query parameter
	workflow := r.URL.Query().Get("workflow")
	event := r.URL.Query().Get("event")
	view := r.URL.Query().Get("view")
	if view != "cards" {
		view = "grid"
	}

	// Get all workflow runs for this repository (up to a reasonable max)
	// We need to get all runs first, then limit per workflow to ensure all workflows are shown
	allRuns, err := GetWorkflowRuns(ctx, s.db, WorkflowRunOptions{
		RepositoryID: repo.ID,
		HeadBranch:   branch,
		WorkflowName: workflow,
		Limit:        10000, // Get a large number to ensure we have all workflows
	})
	if err != nil {
		logger.Error("Failed to get workflow runs", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Build list of unique branches for the filter dropdown
	branchesMap := make(map[string]bool)
	workflowsMap := make(map[string]bool)
	eventsMap := make(map[string]bool)
	for _, run := range allRuns {
		if run.HeadBranch != "" {
			branchesMap[run.HeadBranch] = true
		}
		if run.WorkflowName != "" {
			workflowsMap[run.WorkflowName] = true
		}
		if run.Event != "" {
			eventsMap[run.Event] = true
		}
	}
	branches := make([]string, 0, len(branchesMap))
	for b := range branchesMap {
		branches = append(branches, b)
	}
	workflows := make([]string, 0, len(workflowsMap))
	for w := range workflowsMap {
		workflows = append(workflows, w)
	}
	events := make([]string, 0, len(eventsMap))
	for eventName := range eventsMap {
		events = append(events, eventName)
	}
	sort.Strings(branches)
	sort.Strings(workflows)
	sort.Strings(events)
	if event != "" {
		filteredRuns := make([]WorkflowRun, 0, len(allRuns))
		for _, run := range allRuns {
			if run.Event == event {
				filteredRuns = append(filteredRuns, run)
			}
		}
		allRuns = filteredRuns
	}

	columns, workflowRows, workflowOrder := buildWorkflowGrid(allRuns, limit)

	// Render template
	w.Header().Set("Content-Type", "text/html")
	if err := s.tmpl.ExecuteTemplate(w, "repository-matrix.html", map[string]interface{}{
		"repository":      repo,
		"columns":         columns,
		"workflowRows":    workflowRows,
		"workflowOrder":   workflowOrder,
		"branches":        branches,
		"workflows":       workflows,
		"events":          events,
		"currentBranch":   branch,
		"currentWorkflow": workflow,
		"currentEvent":    event,
		"currentLimit":    limit,
		"currentView":     view,
		"limitOptions":    []int{50, 100, 200, 500},
	}); err != nil {
		logger.Error("Failed to render repository template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// retryDatabaseConnection initializes the stable pool until ready or stopped.
func (s *Server) retryDatabaseConnection() {
	logger := clog.FromContext(s.ctx)
	for {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		err := initializeDatabase(ctx, s.db)
		cancel()
		if err == nil {
			s.ready.Store(true)
			return
		}
		logger.Warn("Database initialization failed; retrying", "error", err)
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
