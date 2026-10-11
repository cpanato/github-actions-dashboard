package handler

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/go-github/v92/github"
)

// withInstallation serializes lifecycle changes and event storage by installation.
func (b *Base) withInstallation(ctx context.Context, id int64, change func(*sql.Tx, bool) error) error {
	if b.DB == nil {
		return errors.New("database not available")
	}
	if id <= 0 {
		return errors.New("missing installation ID")
	}
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO installations (id) VALUES ($1) ON CONFLICT DO NOTHING`, id); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT active FROM installations WHERE id=$1 FOR UPDATE`, id).Scan(&active); err != nil {
		return err
	}
	if err := change(tx, active); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertRepository(ctx context.Context, tx *sql.Tx, installationID int64, repo *github.Repository) error {
	if repo.GetID() <= 0 || repo.GetFullName() == "" || repo.GetName() == "" {
		return errors.New("incomplete repository")
	}
	owner := repo.GetOwner().GetLogin()
	if owner == "" {
		owner, _, _ = strings.Cut(repo.GetFullName(), "/")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO repositories (id,owner,name,full_name,default_branch,installation_id)
 VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO UPDATE SET
 owner=EXCLUDED.owner, name=EXCLUDED.name, full_name=EXCLUDED.full_name,
 default_branch=COALESCE(NULLIF(EXCLUDED.default_branch,''),repositories.default_branch),
 installation_id=EXCLUDED.installation_id, updated_at=CURRENT_TIMESTAMP`,
		repo.GetID(), owner, repo.GetName(), repo.GetFullName(), repo.GetDefaultBranch(), installationID)
	return err
}

func (b *Base) withRepository(ctx context.Context, installationID int64, repo *github.Repository, store func(*sql.Tx) error) (bool, error) {
	if repo.GetID() <= 0 {
		return false, errors.New("missing repository ID")
	}
	stored := false
	err := b.withInstallation(ctx, installationID, func(tx *sql.Tx, active bool) error {
		if !active {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO repository_access (installation_id,repository_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, installationID, repo.GetID()); err != nil {
			return err
		}
		var allowed bool
		if err := tx.QueryRowContext(ctx, `SELECT active FROM repository_access WHERE installation_id=$1 AND repository_id=$2`, installationID, repo.GetID()).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return nil
		}
		if err := upsertRepository(ctx, tx, installationID, repo); err != nil {
			return err
		}
		if err := store(tx); err != nil {
			return err
		}
		stored = true
		return nil
	})
	return stored, err
}
