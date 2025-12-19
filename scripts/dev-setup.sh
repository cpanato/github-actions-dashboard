#!/bin/bash
# Local development setup script

set -e

echo "🚀 Setting up local development environment"
echo ""

# Check if config file exists
if [ ! -f "config/local.yml" ]; then
    echo "❌ config/local.yml not found"
    echo "   Create it from config/local.yml or use DASHBOARD_* environment variables"
    exit 1
fi

# Initialize SQLite database
echo "📦 Initializing SQLite database..."
echo "   Database will be created at: ./dashboard.db"

# Build the application
echo ""
echo "🔨 Building application..."
go build -o dashboard .

# Check if GitHub App credentials are set
echo ""
echo "🔍 Checking GitHub App credentials..."
if [ -z "$DASHBOARD_GITHUB_APP_INTEGRATION_ID" ]; then
    echo "⚠️  DASHBOARD_GITHUB_APP_INTEGRATION_ID not set"
    echo "   Set your GitHub App credentials:"
    echo "   export DASHBOARD_GITHUB_APP_INTEGRATION_ID=<your-app-id>"
    echo "   export DASHBOARD_GITHUB_APP_PRIVATE_KEY=<your-private-key>"
    echo "   export DASHBOARD_GITHUB_APP_WEBHOOK_SECRET=<your-webhook-secret>"
fi

echo ""
echo "✅ Setup complete!"
echo ""
echo "To start the server:"
echo "  ./dashboard"
echo ""
echo "To use a custom config:"
echo "  ./dashboard -config config/local.yml"
echo ""
echo "Database will be automatically initialized on first run."
echo ""
echo "For Cloud SQL (PostgreSQL) instead:"
echo "  export DASHBOARD_DATABASE_DRIVER=postgres"
echo "  export DASHBOARD_DATABASE_URL='postgres://user:pass@localhost:5432/dashboard?sslmode=disable'"
