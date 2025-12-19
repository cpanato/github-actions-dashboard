#!/bin/bash
# Setup Cloud SQL PostgreSQL instance for GitHub Actions Dashboard
#
# This script creates and configures a Cloud SQL instance with proper settings

set -e

# Configuration
PROJECT_ID="${GCP_PROJECT_ID:-your-project-id}"
REGION="${GCP_REGION:-us-central1}"
INSTANCE_NAME="${INSTANCE_NAME:-dashboard-db}"
DB_NAME="${DB_NAME:-dashboard}"
DB_USER="${DB_USER:-dashboard}"
DB_VERSION="${DB_VERSION:-POSTGRES_15}"

echo "🗄️  Setting up Cloud SQL PostgreSQL instance"
echo "Project: ${PROJECT_ID}"
echo "Region: ${REGION}"
echo "Instance: ${INSTANCE_NAME}"
echo ""

# Create Cloud SQL instance
echo "Creating Cloud SQL instance..."
gcloud sql instances create "${INSTANCE_NAME}" \
  --project="${PROJECT_ID}" \
  --database-version="${DB_VERSION}" \
  --tier=db-f1-micro \
  --region="${REGION}" \
  --network=default \
  --no-assign-ip \
  --database-flags=max_connections=100 \
  --backup-start-time=03:00 \
  --maintenance-window-day=SUN \
  --maintenance-window-hour=4 \
  --maintenance-release-channel=production

echo "✓ Instance created"

# Generate secure password
DB_PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-25)

# Set root password
echo "Setting root password..."
gcloud sql users set-password postgres \
  --project="${PROJECT_ID}" \
  --instance="${INSTANCE_NAME}" \
  --password="${DB_PASSWORD}"

echo "✓ Root password set"

# Create application database
echo "Creating database ${DB_NAME}..."
gcloud sql databases create "${DB_NAME}" \
  --project="${PROJECT_ID}" \
  --instance="${INSTANCE_NAME}"

echo "✓ Database created"

# Create application user
echo "Creating user ${DB_USER}..."
gcloud sql users create "${DB_USER}" \
  --project="${PROJECT_ID}" \
  --instance="${INSTANCE_NAME}" \
  --password="${DB_PASSWORD}"

echo "✓ User created"

# Store credentials in Secret Manager
echo "Storing credentials in Secret Manager..."

# Check if secrets exist, delete if they do
for secret in "dashboard-db-password" "dashboard-db-connection"; do
  if gcloud secrets describe "${secret}" --project="${PROJECT_ID}" &>/dev/null; then
    echo "Deleting existing secret ${secret}..."
    gcloud secrets delete "${secret}" --project="${PROJECT_ID}" --quiet
  fi
done

# Create new secrets
echo -n "${DB_PASSWORD}" | gcloud secrets create dashboard-db-password \
  --project="${PROJECT_ID}" \
  --data-file=-

CONNECTION_NAME="${PROJECT_ID}:${REGION}:${INSTANCE_NAME}"
echo -n "${CONNECTION_NAME}" | gcloud secrets create dashboard-db-connection \
  --project="${PROJECT_ID}" \
  --data-file=-

# Grant Cloud Run service account access to secrets
PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

for secret in "dashboard-db-password" "dashboard-db-connection"; do
  gcloud secrets add-iam-policy-binding "${secret}" \
    --project="${PROJECT_ID}" \
    --member="serviceAccount:${SERVICE_ACCOUNT}" \
    --role="roles/secretmanager.secretAccessor" \
    --quiet
done

echo "✓ Secrets stored"

# Get connection details
CONNECTION_STRING="postgres://${DB_USER}:[PASSWORD]@/${DB_NAME}?host=/cloudsql/${CONNECTION_NAME}"

echo ""
echo "✅ Cloud SQL setup complete!"
echo ""
echo "Connection details:"
echo "  Instance: ${CONNECTION_NAME}"
echo "  Database: ${DB_NAME}"
echo "  User: ${DB_USER}"
echo "  Password: [Stored in Secret Manager: dashboard-db-password]"
echo ""
echo "Connection string for Cloud Run:"
echo "  ${CONNECTION_STRING}"
echo ""
echo "To connect locally via Cloud SQL Proxy:"
echo "  cloud-sql-proxy ${CONNECTION_NAME}"
echo "  psql 'host=localhost user=${DB_USER} dbname=${DB_NAME}'"
echo ""
echo "📝 Next steps:"
echo "1. Run database migrations"
echo "2. Deploy your application with deploy/cloud-run/deploy.sh"
