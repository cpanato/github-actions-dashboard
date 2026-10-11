// Package testdb creates disposable schemas for PostgreSQL integration tests.
package testdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq" // Registers the PostgreSQL driver for test schema connections.
)

// Open creates a unique schema in an explicitly configured loopback test database.
// Tests skip when DASHBOARD_TEST_DATABASE_URL is absent.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("DASHBOARD_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set DASHBOARD_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		(u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") ||
		!strings.HasSuffix(u.Path, "_test") {
		t.Fatal("integration tests require a loopback PostgreSQL URL with a database name ending in _test")
	}
	root, err := sql.Open("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "dashboard_test_" + hex.EncodeToString(suffix[:])
	// #nosec G202 -- schema is a fixed prefix plus random hex, never external input.
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// #nosec G202 -- schema is a fixed prefix plus random hex.
		if _, err := root.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
