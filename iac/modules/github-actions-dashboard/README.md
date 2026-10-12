# GCP dashboard module

Deploys the dashboard on Cloud Run with private networking, Cloud SQL PostgreSQL,
a Cloud SQL proxy, a runtime service account and Secret Manager resources. It uses
Chainguard infrastructure modules (`1.59.3`).
Providers and deployment-specific inputs are configured by the caller.

The reusable module declares minimum tested provider versions with `>=`. The
caller example pins exact versions for all providers, including transitive
dependencies, so upgrades are controlled by the deployment configuration. When
updating, check the latest stable provider releases and validate the new versions.
Generated lockfiles are ignored in the examples.

See [the caller example](../../examples/gcp/main.tf). From `iac/gcp`, the module
source is `../modules/github-actions-dashboard` and the application checkout is
`abspath("${path.root}/../..")`. Keep project IDs, IAM identities, GitHub App IDs,
allowlists and the public URL in your untracked caller configuration. The module
has no credential inputs and exports no secret values.

## Prerequisites

- Terraform >= 1.15, Google Application Default Credentials and permission
  to manage networking, Cloud Run, Cloud SQL, IAM, Secret Manager and Monitoring.
- Enable Compute Engine, Service Networking, Cloud Run, Cloud SQL Admin, Secret
  Manager, Cloud Monitoring and the registry APIs required by your ko repository.
- Configure the caller's `google` and `google-beta` providers for `project_id`, and
  the `ko` provider with a writable container repository. Image builds/signing
  require the corresponding registry and cosign authentication.
- Supply exactly one region and at least one GitHub user or organization allowlist.
  Cloud Run runs one always-on instance because sessions and realtime state are in
  memory. It allows public invocation for OAuth and GitHub webhooks; the app checks
  authentication on dashboard routes. The optional upstream telemetry sidecar is
  disabled by default; set `enable_otel_sidecar` to enable it with the required
  image/configuration.

## Secret bootstrap

The module creates four empty GitHub secret containers. It does not create
placeholder values, read local credentials or manage GitHub secret payloads.
The `secret_version_adder` IAM member can upload versions outside Terraform.
The generated database password is stored in Secret Manager **and Terraform
state**. Use an access-controlled, encrypted backend and never commit state,
plans or variable-value files.

For a new environment:

1. Run `terraform init`, then bootstrap the four containers with a targeted apply:
   `terraform apply -target=module.dashboard.module.gh_dashboard_webhook_secret -target=module.dashboard.module.gh_dashboard_gh_private_key_secret -target=module.dashboard.module.gh_dashboard_gh_oauth_client_id_secret -target=module.dashboard.module.gh_dashboard_gh_oauth_client_secret`.
   This target operation is only for the initial bootstrap; use full plans/applies
   for subsequent deployment.
2. Upload a webhook secret, PEM private-key contents, OAuth client ID and OAuth
   client secret using Secret Manager. Upload from local files or stdin, outside
   Terraform. The container IDs are `gh-dashboard-webhook-secret`,
   `gh-dashboard-gh-private-key-secret`, `gh-dashboard-gh-oauth-client-id-secret`
   and `gh-dashboard-gh-oauth-client-secret`.
   Use one module deployment per project.
3. Run a full `terraform plan` and review it before `terraform apply`.
4. Set the GitHub App webhook to `<public_url>/api/github/hook` and its OAuth
   callback to `<public_url>/login/callback`.

The runtime reads the latest version of each secret. Redeploy Cloud Run after
rotating secrets supplied as environment variables. Supply the actual external
HTTPS origin as `public_url`; the module does not derive a region-specific URL.
