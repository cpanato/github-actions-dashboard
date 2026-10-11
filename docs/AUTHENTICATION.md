# Authentication and authorization

Outside the loopback-only demo profile, users sign in with GitHub OAuth and must
pass an explicit allowlist before receiving an authorized dashboard session.
An empty allowlist prevents server startup. Existing username-only sessions do
not grant access.

```yaml
access:
  allowed_users: [your-github-login]
  allowed_organizations: [your-organization]
sessions:
  lifetime: 28800
  # Defaults to true when server.public_url uses https://.
  secure: true
```

Alternatively set comma-separated `DASHBOARD_ALLOWED_USERS` and
`DASHBOARD_ALLOWED_ORGANIZATIONS`. Environment values replace the corresponding
YAML lists. Blank entries are rejected.

Either an exact user match (case insensitive) or active membership in an allowed
organization grants access to **all tracked repositories, aggregates and
WebSocket messages**. This policy does not isolate repositories by user. Only
allow people entitled to see the entire dashboard dataset.

Organization membership is checked with the authenticated user's GitHub client
at login. Configure the GitHub App's organization Members permission as read and
ensure the app/user token can access the allowed organization. Pending membership
and 404 responses deny access. Other API failures return 503 without creating an
authorized session. Explicitly allowed users do not need an organization lookup.
Membership is checked again at the next login; revocation is bounded by the
session lifetime, not continuously checked for an existing session or socket.

## GitHub App setup

Set the OAuth callback to `https://your-domain.com/login/callback`, and configure
`server.public_url` or `DASHBOARD_PUBLIC_URL` to match. Set these environment values:

- `DASHBOARD_GITHUB_APP_INTEGRATION_ID`
- `DASHBOARD_GITHUB_APP_PRIVATE_KEY` (PEM contents)
- `DASHBOARD_GITHUB_APP_WEBHOOK_SECRET`
- `DASHBOARD_GITHUB_OAUTH_CLIENT_ID`
- `DASHBOARD_GITHUB_OAUTH_CLIENT_SECRET`

The App needs repository Actions, Checks and Metadata read permissions. Subscribe
to workflow_run, check_run, installation and installation_repositories events.
Webhook requests use signature verification rather than user sessions.

## Routes and sessions

Dashboard pages, repository pages, data APIs and `/ws` require an authorized
session. `/api/health`, `/api/github/hook`, `/api/github/auth`, `/login/callback`
and `/static/` are public. Login rotates the session token before storing the
username and authorization result. Cookies are HttpOnly with SameSite=Lax; set
`secure: true` for HTTPS deployments behind a proxy if no public URL is configured.
WebSocket upgrades reject an Origin whose host differs from the request host.
Upgrade errors do not log request headers or session cookies.

The default session store is in memory. Restarts invalidate sessions. Multiple
replicas require a shared session store or routing affinity; shared session
storage is not implemented. Sessions expire after the configured lifetime and
are not extended indefinitely by activity.

## Local use

For real OAuth testing, start PostgreSQL with `docker compose up -d postgres`,
configure `config/local.dev.yml`, set credentials and an allowlist, and run
`go run . -config config/local.dev.yml`. A tunnel may be needed for GitHub webhooks.
Use the same externally reachable public URL for the OAuth callback.

`config/demo.yml` skips OAuth and allowlists, seeds synthetic data and refuses
non-loopback binds or a public URL. It is intended for local demos and E2E tests.

## Realtime session expiry

An authenticated WebSocket closes at the session's absolute expiry, even if the
browser continues responding to heartbeat pings. Reconnecting requires a valid
session and uses the normal login/access checks. Local demo sockets have no
session deadline. Organization membership is still checked at login, so removal
from an allowed organization does not immediately revoke an existing session.
