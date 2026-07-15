// Package dbtest provides a shared helper for opening a PostgreSQL test
// database. Store tests skip when AUX_TEST_DATABASE_URL is unset, so `go test`
// still passes locally without a database; CI runs them against a Postgres
// service container.
package dbtest

import (
	"os"
	"testing"

	"gorm.io/gorm"

	"github.com/EmpireForge-ef/aux-app/internal/db"
)

// Open returns a GORM handle to the test database, skipping the test if none is
// configured.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("AUX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AUX_TEST_DATABASE_URL to run database tests")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return gdb
}

// Truncate empties the named tables so each test starts clean.
func Truncate(t *testing.T, gdb *gorm.DB, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		if err := gdb.Exec("TRUNCATE TABLE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}
