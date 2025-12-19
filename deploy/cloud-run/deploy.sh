#!/bin/bash
# Deploy to Cloud Run with Cloud SQL
#
# Prerequisites:
# - gcloud CLI installed and authenticated
# - Project and Cloud SQL instance created
# - GitHub App configured

set -e

# Configuration
PROJECT_ID="${GCP_PROJECT_ID:-your-project-id}"
REGION="${GCP_REGION:-us-central1}"
SERVICE_NAME="${SERVICE_NAME:-github-actions-dashboard}"
CLOUD_SQL_INSTANCE="${CLOUD_SQL_INSTANCE:-your-project-id:us-central1:dashboard-db}"

# Container Registry
IMAGE_NAME="gcr.io/${PROJECT_ID}/${SERVICE_NAME}"

echo "🚀 Deploying GitHub Actions Dashboard to Cloud Run"
echo "Project: ${PROJECT_ID}"
echo "Region: ${REGION}"
echo "Service: ${SERVICE_NAME}"
echo "Cloud SQL: ${CLOUD_SQL_INSTANCE}"
echo ""

# Build container image
echo "📦 Building container image..."
gcloud builds submit \
  --project="${PROJECT_ID}" \
  --tag="${IMAGE_NAME}" \
  .

# Deploy to Cloud Run
echo "🌐 Deploying to Cloud Run..."
gcloud run deploy "${SERVICE_NAME}" \
  --project="${PROJECT_ID}" \
  --region="${REGION}" \
  --image="${IMAGE_NAME}" \
  --platform=managed \
  --allow-unauthenticated \
  --add-cloudsql-instances="${CLOUD_SQL_INSTANCE}" \
  --set-env-vars="DASHBOARD_DATABASE_CLOUD_SQL_INSTANCE=${CLOUD_SQL_INSTANCE}" \
  --set-env-vars="DASHBOARD_DATABASE_NAME=dashboard" \
  --set-env-vars="DASHBOARD_DATABASE_USER=dashboard" \
  --set-secrets="DASHBOARD_DATABASE_PASSWORD=dashboard-db-password:latest" \
  --set-secrets="DASHBOARD_GITHUB_APP_PRIVATE_KEY=github-app-private-key:latest" \
  --set-secrets="DASHBOARD_GITHUB_APP_WEBHOOK_SECRET=github-webhook-secret:latest" \
  --set-secrets="DASHBOARD_GITHUB_OAUTH_CLIENT_SECRET=github-oauth-secret:latest" \
  --min-instances=1 \
  --max-instances=10 \
  --memory=512Mi \
  --cpu=1 \
  --timeout=300 \
  --concurrency=80 \
  --port=8080

# Get the service URL
SERVICE_URL=$(gcloud run services describe "${SERVICE_NAME}" \
  --project="${PROJECT_ID}" \
  --region="${REGION}" \
  --format="value(status.url)")

echo ""
echo "✅ Deployment complete!"
echo "Service URL: ${SERVICE_URL}"
echo ""
echo "📝 Next steps:"
echo "1. Update your GitHub App webhook URL to: ${SERVICE_URL}/api/github/hook"
echo "2. Update OAuth callback URL to: ${SERVICE_URL}/api/github/auth"
echo "3. Verify health: ${SERVICE_URL}/api/health"
