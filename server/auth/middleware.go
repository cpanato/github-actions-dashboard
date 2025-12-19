// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/go-github/v66/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/palantir/go-githubapp/oauth2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	SessionKeyUsername = "username"
	SessionKeyRedirect = "redirect"
)

// LoginCallback handles successful OAuth login
func LoginCallback(c githubapp.Config, basePath string, sessions *scs.SessionManager) oauth2.LoginCallback {
	return func(w http.ResponseWriter, r *http.Request, login *oauth2.Login) {
		ctx := r.Context()
		logger := zerolog.Ctx(ctx)
		if logger.GetLevel() == zerolog.Disabled {
			logger = &log.Logger
		}

		logger.Info().
			Str("path", r.URL.Path).
			Str("method", r.Method).
			Msg("LoginCallback: OAuth callback received")

		client := github.NewClient(login.Client)

		// Parse GitHub API URL
		u, err := url.Parse(strings.TrimSuffix(c.V3APIURL, "/") + "/")
		if err != nil {
			logger.Error().Err(err).Msg("Failed to parse GitHub URL")
			http.Error(w, "Configuration error", http.StatusInternalServerError)
			return
		}
		client.BaseURL = u

		// Get authenticated user
		user, _, err := client.Users.Get(ctx, "")
		if err != nil {
			logger.Error().Err(err).Msg("Failed to get GitHub user")
			http.Error(w, "Failed to authenticate with GitHub", http.StatusUnauthorized)
			return
		}

		logger.Info().Str("user", user.GetLogin()).Msg("User authenticated via OAuth")

		// Store username in session
		sessions.Put(r.Context(), SessionKeyUsername, user.GetLogin())
		logger.Info().Str("user", user.GetLogin()).Msg("Stored username in session")

		// Verify session was stored by immediately reading it back
		stored := sessions.GetString(r.Context(), SessionKeyUsername)
		logger.Info().
			Str("user", user.GetLogin()).
			Str("stored_value", stored).
			Bool("matches", stored == user.GetLogin()).
			Msg("Session verification check")

		// Redirect to original page or home
		target := sessions.PopString(r.Context(), SessionKeyRedirect)
		if target == "" {
			target = basePath + "/"
		}

		logger.Info().
			Str("user", user.GetLogin()).
			Str("redirect_to", target).
			Msg("Redirecting after successful OAuth login")

		http.Redirect(w, r, target, http.StatusFound)
	}
}

// RequireLogin middleware checks if user is authenticated
func RequireLogin(sessions *scs.SessionManager, basePath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger := zerolog.Ctx(ctx)
			if logger.GetLevel() == zerolog.Disabled {
				logger = &log.Logger
			}

			// Skip authentication for OAuth-related paths
			if strings.HasPrefix(r.URL.Path, "/login") ||
				strings.HasPrefix(r.URL.Path, "/api/github/auth") ||
				strings.HasPrefix(r.URL.Path, "/api/github/hook") {
				logger.Debug().Str("path", r.URL.Path).Msg("Skipping auth check for unprotected route")
				next.ServeHTTP(w, r)
				return
			}

			user := sessions.GetString(r.Context(), SessionKeyUsername)
			logger.Info().
				Str("path", r.URL.Path).
				Str("user_from_session", user).
				Bool("has_user", user != "").
				Msg("RequireLogin: Checking authentication")

			if user == "" {
				// Store redirect target (but not if already going to OAuth)
				if !strings.HasPrefix(r.URL.Path, "/login") && !strings.HasPrefix(r.URL.Path, "/api/github/") {
					sessions.Put(r.Context(), SessionKeyRedirect, r.URL.String())
					logger.Info().
						Str("redirect_from", r.URL.String()).
						Msg("Storing redirect target in session")
				}

				// Redirect to OAuth login
				logger.Info().
					Str("path", r.URL.Path).
					Str("redirect_to", basePath+"/api/github/auth").
					Msg("User not authenticated, redirecting to OAuth login")
				http.Redirect(w, r, basePath+"/api/github/auth", http.StatusFound)
				return
			}

			// User is authenticated, continue
			logger.Debug().Str("user", user).Str("path", r.URL.Path).Msg("User authenticated")
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
	logger := zerolog.Ctx(ctx)

	// Generate secret key if not provided
	if secretKey == "" {
		var err error
		secretKey, err = GenerateSecretKey()
		if err != nil {
			return nil, err
		}
		logger.Warn().Msg("No session secret key provided, generated a random key (sessions will be invalidated on restart)")
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
