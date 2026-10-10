// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/chainguard-dev/clog"
	"github.com/google/go-github/v92/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/palantir/go-githubapp/oauth2"
	"github.com/pkg/errors"
)

const (
	SessionKeyUsername = "username"
	SessionKeyRedirect = "redirect"
)

// LoginCallback handles successful OAuth login
func LoginCallback(c githubapp.Config, basePath string, sessions *scs.SessionManager) oauth2.LoginCallback {
	return func(w http.ResponseWriter, r *http.Request, login *oauth2.Login) {
		ctx := r.Context()
		logger := clog.FromContext(ctx)

		logger.Info("LoginCallback: OAuth callback received",
			"path", r.URL.Path,
			"method", r.Method)

		apiURL := strings.TrimSuffix(c.V3APIURL, "/") + "/"
		client, err := github.NewClient(
			github.WithHTTPClient(login.Client),
			github.WithURLs(&apiURL, nil),
		)
		if err != nil {
			logger.Error("Failed to create GitHub client", "error", err)
			http.Error(w, "Configuration error", http.StatusInternalServerError)
			return
		}

		// Get authenticated user
		user, _, err := client.Users.Get(ctx, "")
		if err != nil {
			logger.Error("Failed to get GitHub user", "error", err)
			http.Error(w, "Failed to authenticate with GitHub", http.StatusUnauthorized)
			return
		}

		logger.Info("User authenticated via OAuth", "user", user.GetLogin())

		// Store username in session
		sessions.Put(r.Context(), SessionKeyUsername, user.GetLogin())
		logger.Info("Stored username in session", "user", user.GetLogin())

		// Verify session was stored by immediately reading it back
		stored := sessions.GetString(r.Context(), SessionKeyUsername)
		logger.Info("Session verification check",
			"user", user.GetLogin(),
			"stored_value", stored,
			"matches", stored == user.GetLogin())

		// Redirect to original page or home
		target := sessions.PopString(r.Context(), SessionKeyRedirect)
		if target == "" {
			target = basePath + "/"
		}

		logger.Info("Redirecting after successful OAuth login",
			"user", user.GetLogin(),
			"redirect_to", target)

		http.Redirect(w, r, target, http.StatusFound)
	}
}

// RequireLogin middleware checks if user is authenticated
func RequireLogin(sessions *scs.SessionManager, basePath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger := clog.FromContext(ctx)

			// Skip authentication for OAuth-related paths
			if strings.HasPrefix(r.URL.Path, "/login") ||
				strings.HasPrefix(r.URL.Path, "/api/github/auth") ||
				strings.HasPrefix(r.URL.Path, "/api/github/hook") {
				logger.Debug("Skipping auth check for unprotected route", "path", r.URL.Path)
				next.ServeHTTP(w, r)
				return
			}

			user := sessions.GetString(r.Context(), SessionKeyUsername)
			logger.Info("RequireLogin: Checking authentication",
				"path", r.URL.Path,
				"user_from_session", user,
				"has_user", user != "")

			if user == "" {
				// Store redirect target (but not if already going to OAuth)
				if !strings.HasPrefix(r.URL.Path, "/login") && !strings.HasPrefix(r.URL.Path, "/api/github/") {
					sessions.Put(r.Context(), SessionKeyRedirect, r.URL.String())
					logger.Info("Storing redirect target in session",
						"redirect_from", r.URL.String())
				}

				// Redirect to OAuth login
				logger.Info("User not authenticated, redirecting to OAuth login",
					"path", r.URL.Path,
					"redirect_to", basePath+"/api/github/auth")
				http.Redirect(w, r, basePath+"/api/github/auth", http.StatusFound)
				return
			}

			// User is authenticated, continue
			logger.Debug("User authenticated", "user", user, "path", r.URL.Path)
			next.ServeHTTP(w, r)
		})
	}
}

// GenerateSecretKey generates a random secret key for session encryption
func GenerateSecretKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", errors.Wrap(err, "failed to generate random key")
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// NewSessionManager creates a new session manager with the given configuration
func NewSessionManager(ctx context.Context, secretKey string, lifetime int) (*scs.SessionManager, error) {
	logger := clog.FromContext(ctx)

	// Generate secret key if not provided
	if secretKey == "" {
		var err error
		secretKey, err = GenerateSecretKey()
		if err != nil {
			return nil, err
		}
		logger.Warn("No session secret key provided, generated a random key (sessions will be invalidated on restart)")
	}

	sessions := scs.New()
	sessions.Cookie.Name = "dashboard_session"
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.SameSite = http.SameSiteLaxMode
	sessions.Cookie.Secure = false // Set to true in production with HTTPS

	// Note: scs v2 doesn't support MaxAge, use Lifetime instead
	if lifetime > 0 {
		sessions.Lifetime = time.Duration(lifetime) * time.Second
	}

	return sessions, nil
}
