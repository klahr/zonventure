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
		skipUnlessCI(t, "psql not on PATH")
	}
	admin := os.Getenv("ZV_TEST_DSN")
	if admin == "" {
		skipUnlessCI(t, "ZV_TEST_DSN not set")
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

// skipUnlessCI mirrors dbtest's policy: a missing prerequisite is a
// skip on a workstation but fatal under $CI, where a green run that
// silently skipped would look like coverage it does not have.
func skipUnlessCI(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf("%s (skipping is disabled under CI)", why)
	}
	t.Skip(why)
}
