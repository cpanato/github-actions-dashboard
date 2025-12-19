#!/bin/bash
# Fix missing repository records for existing workflow runs

cd /Users/cpanato/go/src/github.com/cpanato/github-actions-dashboard

# SQLite doesn't support this directly, so we'll need to rebuild
# The safest approach is to delete the DB and let webhooks repopulate it
echo "Backing up current database..."
cp dashboard.db dashboard.db.backup

echo "Removing old database..."
rm dashboard.db

echo "Database reset. Restart the server and trigger some GitHub Actions workflows to repopulate data."
echo "Backup saved as dashboard.db.backup if you need to restore."
