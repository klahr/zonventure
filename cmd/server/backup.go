package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// runBackup streams a logical backup of the database referenced by dsn
// to stdout, by shelling out to pg_dump. The dump is plain SQL with
// --clean --if-exists --no-owner --no-privileges so it can be replayed
// into any Postgres role without ownership conflicts — important for
// self-hosters whose dump-time and restore-time roles differ.
//
// The DSN is passed as the first positional arg; pg_dump accepts a
// libpq URL directly. Stderr is forwarded so the operator sees
// connection errors / permission warnings inline.
func runBackup(dsn string, out io.Writer) error {
	if dsn == "" {
		return errors.New("backup: no DSN — pass -dsn or set DATABASE_URL")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return fmt.Errorf("backup: pg_dump not found on PATH — install postgres client tools (apt install postgresql-client / brew install libpq / pacman -S postgresql)")
	}
	cmd := exec.Command("pg_dump",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		dsn,
	)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w", err)
	}
	return nil
}

// runRestore replays a plain-SQL dump on stdin into the database
// referenced by dsn via psql. ON_ERROR_STOP=1 means any failed
// statement aborts the whole restore so we don't leave the database
// half-restored on a partially-broken dump.
func runRestore(dsn string, in io.Reader) error {
	if dsn == "" {
		return errors.New("restore: no DSN — pass -dsn or set DATABASE_URL")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		return fmt.Errorf("restore: psql not found on PATH — install postgres client tools")
	}
	cmd := exec.Command("psql",
		"--quiet",
		"--variable=ON_ERROR_STOP=1",
		"--file=-",
		dsn,
	)
	cmd.Stdin = in
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql restore failed: %w", err)
	}
	return nil
}
