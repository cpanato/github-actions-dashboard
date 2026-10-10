// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/chainguard-dev/clog"
)

func TestNewSessionManager(t *testing.T) {
	sessions := NewSessionManager(3600)
	if sessions.Lifetime != time.Hour {
		t.Errorf("SessionManager.Lifetime = %v, want %v", sessions.Lifetime, time.Hour)
	}
	if sessions.Cookie.Name != "dashboard_session" {
		t.Errorf("Cookie.Name = %q, want dashboard_session", sessions.Cookie.Name)
	}
	if !sessions.Cookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
}

func TestRequireLogin_UnprotectedPaths(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	sessions := scs.New()
	middleware := RequireLogin(sessions, "")

	unprotectedPaths := []string{
		"/login",
		"/login/callback",
		"/api/github/auth",
		"/api/github/hook",
	}

	for _, path := range unprotectedPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()

			handlerCalled := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			})

			middleware(handler).ServeHTTP(rr, req)

			if !handlerCalled {
				t.Errorf("Handler should be called for path %s", path)
			}
		})
	}
}

func TestRequireLogin_ProtectedPaths_NoAuth(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	sessions := scs.New()
	middleware := RequireLogin(sessions, "")

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handlerCalled := false
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
	})

	sessions.LoadAndSave(middleware(handler)).ServeHTTP(rr, req)

	if handlerCalled {
		t.Error("Handler should not be called without auth")
	}

	if rr.Code != http.StatusFound {
		t.Errorf("Expected redirect (302), got %d", rr.Code)
	}
}
