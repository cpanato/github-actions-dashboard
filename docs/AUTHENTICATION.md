# Authentication & Security

## Overview

The GitHub Actions Dashboard implements OAuth-based authentication to protect sensitive workflow data from unauthorized access. Only users who are members of organizations where the GitHub App is installed can access the dashboard.

## How It Works

### 1. GitHub OAuth Flow

When a user tries to access the dashboard without being authenticated:

1. User is redirected to GitHub OAuth authorization page
2. User grants permission to the app
3. GitHub redirects back with an authorization code
4. App exchanges code for an access token
5. App fetches the user's GitHub username
6. Username is stored in a secure session cookie
7. User is redirected back to the original page

### 2. Session Management

- **Cookie-based sessions**: Secure, HTTP-only cookies
- **Lifetime**: Default 24 hours (configurable)
- **Auto-renewal**: Sessions renew on activity

### 3. Access Control

The system provides two levels of protection:

#### Basic Authentication (`RequireLogin`)
- Checks if user is authenticated with GitHub
- Redirects to OAuth login if not authenticated
- Applied to all dashboard routes

#### Repository Access Check (Optional)
- Verifies user has access to at least one repository where the app is installed
- Can be added for stricter access control
- Checks actual GitHub repository permissions

## Configuration

### Session Configuration

Add to your `config.yml`:

```yaml
sessions:
  # Session lifetime in seconds (default: 86400 = 24 hours)
  lifetime: 86400
```

### GitHub App OAuth Configuration

The GitHub App must be configured with OAuth capabilities:

1. Go to your GitHub App settings
2. Set **Callback URL**: `https://your-domain.com/login/callback`
3. Enable **Request user authorization (OAuth) during installation**
4. Set permissions:
   - **Repository permissions**:
     - Actions: Read
     - Checks: Read
     - Metadata: Read
   - **Organization permissions**:
     - Members: Read (to verify org membership)

## Protected Routes

By default, these routes require authentication:

- `/` - Main dashboard
- `/api/repositories` - Repository list
- `/api/workflows` - Workflow runs
- `/api/analytics` - Analytics data
- `/api/flaky-tests` - Flaky test detection
- `/api/trends` - Trend data
- `/ws` - WebSocket connection

## Unprotected Routes

These routes are publicly accessible:

- `/api/health` - Health check
- `/api/github/hook` - GitHub webhook endpoint
- `/login/callback` - OAuth callback
- `/static/*` - Static assets

## Security Best Practices

### Production Deployment

1. **Enable HTTPS**:
   - Cloud Run automatically provides HTTPS
  - Configure the session cookie with the `Secure` attribute in production

2. **Secure cookie settings**:
   ```go
   sessions.Cookie.HttpOnly = true       // Prevent JavaScript access
   sessions.Cookie.SameSite = http.SameSiteLaxMode  // CSRF protection
   sessions.Cookie.Secure = true         // HTTPS only
   ```

3. **Set appropriate session lifetime**:
   - Development: 24 hours (convenient)
   - Production: 8 hours (more secure)
   - High-security: 1 hour with auto-renewal

### Session Storage

Current implementation uses in-memory sessions via `scs/v2`. For production with multiple instances, consider:

- **Redis store**: `github.com/alexedwards/scs/redisstore`
- **PostgreSQL store**: `github.com/alexedwards/scs/postgresstore`
- **Cloud SQL store**: PostgreSQL-based

Example Redis configuration:

```go
import "github.com/alexedwards/scs/redisstore"

redisPool := &redis.Pool{/* config */}
sessions.Store = redisstore.New(redisPool)
```

## Access Control Examples

### Simple Authentication

```go
// In server.go
mux.Handle("/", requireAuth(http.HandlerFunc(s.handleDashboard)))
```

### With Repository Access Check

```go
// Add stricter check
requireRepoAccess := auth.CheckRepositoryAccess(sessions, installations, clientCreator)

mux.Handle("/",
  requireAuth(
    requireRepoAccess(
      http.HandlerFunc(s.handleDashboard)
    )
  )
)
```

### Custom Authorization

```go
func requireOrgMembership(org string) func(http.Handler) http.Handler {
  return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      username := sessions.GetString(r.Context(), SessionKeyUsername)

      // Check org membership
      client, _ := clientCreator.NewAppClient()
      member, _, err := client.Organizations.IsMember(ctx, org, username)

      if !member {
        http.Error(w, "Forbidden", http.StatusForbidden)
        return
      }

      next.ServeHTTP(w, r)
    })
  }
}
```

## Troubleshooting

### "Session not found" errors

- Ensure cookies are enabled in browser
- Sessions are held in the configured session store; an in-memory store loses sessions on restart.

### OAuth redirect issues

- Verify **Callback URL** in GitHub App settings matches your deployment
- Check `github.v3_api_url` in config matches your GitHub instance
- Ensure `github.app.integration_id` is correct

### "Unauthorized" after successful login

- Verify user has access to at least one repository with the app installed
- Check GitHub App permissions (Actions: Read, Checks: Read)
- Ensure org membership is properly configured

### Multiple instance session sync

- Implement a shared session store (Redis, PostgreSQL)
- Or use sticky sessions in load balancer

## Testing Authentication Locally

1. **Set up GitHub App OAuth**:
   ```bash
   # Use ngrok for local testing
   ngrok http 8080

   # Set callback URL in GitHub App:
   # https://your-ngrok-url.ngrok.io/login/callback
   ```

2. **Configure environment**:
   ```bash
   export DASHBOARD_GITHUB_APP_INTEGRATION_ID=123456
   export DASHBOARD_GITHUB_APP_WEBHOOK_SECRET="your-secret"
   export DASHBOARD_GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"
   ```

3. **Run dashboard**:
   ```bash
   ./github-actions-dashboard --config config/local.yml
   ```

4. **Test flow**:
   - Navigate to `http://localhost:8080`
   - You'll be redirected to GitHub OAuth
   - After authorization, you'll be back at the dashboard

## API Authentication for Automation

For programmatic access (CI/CD, scripts), consider adding API token support:

```go
func requireAPIToken() func(http.Handler) http.Handler {
  return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      token := r.Header.Get("Authorization")
      if !strings.HasPrefix(token, "Bearer ") {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
      }

      // Validate token
      if !validateToken(token[7:]) {
        http.Error(w, "Invalid token", http.StatusUnauthorized)
        return
      }

      next.ServeHTTP(w, r)
    })
  }
}
```

## Migration from Unprotected Setup

If upgrading from a version without authentication:

1. **Add session lifetime configuration** to `config.yml`
2. **Update GitHub App** with OAuth settings
3. **Deploy new version**
4. **Users must authenticate** on first access

All existing data remains accessible after authentication.
