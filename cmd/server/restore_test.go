package main

import (
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// A dump that fails part-way must leave the database as it was: the
// --clean DROPs at its head may not commit on their own.
func TestRunRestore_FailedDumpRollsBack(t *testing.T) {
	if _, err := exec.LookPath("psql"); err != nil {
		t.Skip("psql not on PATH")
	}
	admin := os.Getenv("ZV_TEST_DSN")
	if admin == "" {
		t.Skip("ZV_TEST_DSN not set")
	}
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name

	if _, err := db.Exec(`CREATE TABLE restore_probe (id int); INSERT INTO restore_probe VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	dump := "DROP TABLE IF EXISTS restore_probe;\nSELECT no_such_function();\n"
	if err := runRestore(u.String(), strings.NewReader(dump)); err == nil {
		t.Fatal("restore of a broken dump succeeded")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM restore_probe`).Scan(&n); err != nil {
		t.Fatalf("restore_probe gone after a failed restore: %v", err)
	}
}
