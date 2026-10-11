// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/chainguard-dev/clog"
	"github.com/google/go-github/v92/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/palantir/go-githubapp/oauth2"
)

const (
	SessionKeyUsername   = "username"
	SessionKeyRedirect   = "redirect"
	SessionKeyAuthorized = "authorized"
)

// AccessPolicy authorizes access to the entire shared dashboard.
type AccessPolicy struct {
	AllowedUsers         []string `yaml:"allowed_users"`
	AllowedOrganizations []string `yaml:"allowed_organizations"`
}

// Validate rejects empty policies and blank entries outside demo mode.
func (p AccessPolicy) Validate() error {
	if len(p.AllowedUsers)+len(p.AllowedOrganizations) == 0 {
		return fmt.Errorf("configure access.allowed_users or access.allowed_organizations outside demo mode")
	}
	for _, names := range [][]string{p.AllowedUsers, p.AllowedOrganizations} {
		for _, name := range names {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("access allowlist entries must not be blank")
			}
		}
	}
	return nil
}

func (p AccessPolicy) authorize(ctx context.Context, client *github.Client, username string) (bool, error) {
	if username == "" {
		return false, nil
	}
	for _, user := range p.AllowedUsers {
		if strings.EqualFold(strings.TrimSpace(user), username) {
			return true, nil
		}
	}
	for _, org := range p.AllowedOrganizations {
		membership, response, err := client.Organizations.GetOrgMembership(ctx, "", strings.TrimSpace(org))
		if err != nil {
			if response != nil && response.StatusCode == http.StatusNotFound {
				continue
			}
			return false, fmt.Errorf("verify organization membership: %w", err)
		}
		if membership.GetState() == "active" {
			return true, nil
		}
	}
	return false, nil
}

// LoginCallback handles successful OAuth login
func LoginCallback(c githubapp.Config, basePath string, sessions *scs.SessionManager, policy AccessPolicy) oauth2.LoginCallback {
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

		allowed, err := policy.authorize(ctx, client, user.GetLogin())
		if err != nil || !allowed {
			if destroyErr := sessions.Destroy(ctx); destroyErr != nil {
				logger.Warn("Failed to clear session", "error", destroyErr)
			}
			if err != nil {
				logger.Error("Authorization check failed", "error", err)
				http.Error(w, "Authorization unavailable", http.StatusServiceUnavailable)
			} else {
				http.Error(w, "Forbidden", http.StatusForbidden)
			}
			return
		}
		if err := sessions.RenewToken(ctx); err != nil {
			http.Error(w, "Session unavailable", http.StatusInternalServerError)
			return
		}
		sessions.Put(ctx, SessionKeyUsername, user.GetLogin())
		sessions.Put(ctx, SessionKeyAuthorized, true)

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

			user := sessions.GetString(r.Context(), SessionKeyUsername)
			logger.Info("RequireLogin: Checking authentication",
				"path", r.URL.Path,
				"user_from_session", user,
				"has_user", user != "")

			if user == "" || !sessions.GetBool(ctx, SessionKeyAuthorized) {
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

// NewSessionManager creates a new session manager with the given configuration
func NewSessionManager(lifetime int, secure bool) *scs.SessionManager {
	sessions := scs.New()
	sessions.Cookie.Name = "dashboard_session"
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.SameSite = http.SameSiteLaxMode
	sessions.Cookie.Secure = secure

	// Note: scs v2 doesn't support MaxAge, use Lifetime instead
	if lifetime > 0 {
		sessions.Lifetime = time.Duration(lifetime) * time.Second
	}

	return sessions
}
