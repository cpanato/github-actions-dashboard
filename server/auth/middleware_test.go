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

	"github.com/alexedwards/scs/v2"
	"github.com/chainguard-dev/clog"
)

func TestGenerateSecretKey(t *testing.T) {
	key, err := GenerateSecretKey()
	if err != nil {
		t.Fatalf("GenerateSecretKey() error = %v", err)
	}

	if len(key) == 0 {
		t.Error("GenerateSecretKey() returned empty key")
	}

	key2, err := GenerateSecretKey()
	if err != nil {
		t.Fatalf("GenerateSecretKey() error = %v", err)
	}

	if key == key2 {
		t.Error("GenerateSecretKey() returned same key twice")
	}
}

func TestNewSessionManager(t *testing.T) {
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	tests := []struct {
		name      string
		secretKey string
		lifetime  int
		wantErr   bool
	}{
		{
			name:      "with secret key",
			secretKey: "test-secret-key-32-bytes-long!",
			lifetime:  3600,
			wantErr:   false,
		},
		{
			name:      "without secret key",
			secretKey: "",
			lifetime:  3600,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, err := NewSessionManager(ctx, tt.secretKey, tt.lifetime)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSessionManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if sm == nil {
				t.Error("NewSessionManager() returned nil")
			}
			if sm.Cookie.Name != "dashboard_session" {
				t.Errorf("Cookie.Name = %v, want dashboard_session", sm.Cookie.Name)
			}
		})
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
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
