#!/bin/bash
# Setup GitHub App secrets in Secret Manager
#
# This script helps you store GitHub App credentials securely

set -e

PROJECT_ID="${GCP_PROJECT_ID:-your-project-id}"

echo "🔐 Setting up GitHub App secrets in Secret Manager"
echo "Project: ${PROJECT_ID}"
echo ""

# Function to create or update secret
create_secret() {
  local secret_name=$1
  local description=$2
  
  echo "Setting up: ${secret_name}"
  echo "${description}"
  
  # Check if secret exists
  if gcloud secrets describe "${secret_name}" --project="${PROJECT_ID}" &>/dev/null; then
    echo "Secret ${secret_name} already exists. Creating new version..."
    read -sp "Enter value: " secret_value
    echo
    echo -n "${secret_value}" | gcloud secrets versions add "${secret_name}" \
      --project="${PROJECT_ID}" \
      --data-file=-
  else
    echo "Creating new secret ${secret_name}..."
    read -sp "Enter value: " secret_value
    echo
    echo -n "${secret_value}" | gcloud secrets create "${secret_name}" \
      --project="${PROJECT_ID}" \
      --data-file=-
  fi
  
  # Grant access to Cloud Run service account
  PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
  SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
  
  gcloud secrets add-iam-policy-binding "${secret_name}" \
    --project="${PROJECT_ID}" \
    --member="serviceAccount:${SERVICE_ACCOUNT}" \
    --role="roles/secretmanager.secretAccessor" \
    --quiet &>/dev/null
  
  echo "✓ ${secret_name} configured"
  echo ""
}

# GitHub App Integration ID (not sensitive, can be plain text)
echo "GitHub App Integration ID (App ID):"
read -p "Enter value: " APP_ID
echo -n "${APP_ID}" | gcloud secrets create github-app-id \
  --project="${PROJECT_ID}" \
  --data-file=- 2>/dev/null || \
echo -n "${APP_ID}" | gcloud secrets versions add github-app-id \
  --project="${PROJECT_ID}" \
  --data-file=-

# GitHub App Private Key
echo ""
echo "GitHub App Private Key"
echo "Paste the ENTIRE private key (including -----BEGIN/END----- lines)"
echo "Press Ctrl+D when done:"
private_key=$(cat)
echo -n "${private_key}" | gcloud secrets create github-app-private-key \
  --project="${PROJECT_ID}" \
  --data-file=- 2>/dev/null || \
echo -n "${private_key}" | gcloud secrets versions add github-app-private-key \
  --project="${PROJECT_ID}" \
  --data-file=-

# Webhook Secret
echo ""
create_secret "github-webhook-secret" "GitHub App webhook secret (random string, 20+ characters)"

# OAuth Client ID (not very sensitive)
echo ""
echo "GitHub OAuth Client ID:"
read -p "Enter value: " CLIENT_ID
echo -n "${CLIENT_ID}" | gcloud secrets create github-oauth-client-id \
  --project="${PROJECT_ID}" \
  --data-file=- 2>/dev/null || \
echo -n "${CLIENT_ID}" | gcloud secrets versions add github-oauth-client-id \
  --project="${PROJECT_ID}" \
  --data-file=-

# OAuth Client Secret
echo ""
create_secret "github-oauth-secret" "GitHub OAuth client secret"

# Grant Cloud Run access to all secrets
echo ""
echo "Granting Cloud Run service account access to secrets..."
PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

for secret in github-app-id github-app-private-key github-webhook-secret github-oauth-client-id github-oauth-secret; do
  gcloud secrets add-iam-policy-binding "${secret}" \
    --project="${PROJECT_ID}" \
    --member="serviceAccount:${SERVICE_ACCOUNT}" \
    --role="roles/secretmanager.secretAccessor" \
    --quiet &>/dev/null || true
done

echo ""
echo "✅ All secrets configured!"
echo ""
echo "Stored secrets:"
echo "  - github-app-id"
echo "  - github-app-private-key"
echo "  - github-webhook-secret"
echo "  - github-oauth-client-id"
echo "  - github-oauth-secret"
echo ""
echo "📝 These will be automatically injected into Cloud Run as environment variables"
