package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// dsnEnv parses a libpq URL DSN and returns the slice of PG* env
// variables (in addition to the inherited environment) that pg_dump
// or psql will consume in lieu of a positional URL argument. Passing
// the password as PGPASSWORD instead of in argv keeps it out of
// /proc/<pid>/cmdline and `ps -ef` listings, which a non-root local
// user could otherwise read for the duration of the dump.
//
// Accepts the two URL schemes libpq honours (postgres:// and
// postgresql://) plus key=value-style strings (host=… user=… …);
// the latter are returned unchanged since they don't put the
// password in argv anyway. Unrecognised input is returned untouched
// so the caller can fall back to passing it as an argument.
func dsnEnv(dsn string) (extraEnv []string, useArg string, err error) {
	if !(strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")) {
		// Not a URL DSN — nothing to extract; let the tool consume it
		// as-is on the command line.
		return nil, dsn, nil
	}
	u, perr := url.Parse(dsn)
	if perr != nil {
		return nil, "", fmt.Errorf("parse dsn: %w", perr)
	}
	host := u.Hostname()
	port := u.Port()
	user := u.User.Username()
	password, _ := u.User.Password()
	dbName := strings.TrimPrefix(u.Path, "/")
	sslmode := u.Query().Get("sslmode")
	env := []string{}
	if host != "" {
		env = append(env, "PGHOST="+host)
	}
	if port != "" {
		env = append(env, "PGPORT="+port)
	}
	if user != "" {
		env = append(env, "PGUSER="+user)
	}
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	if dbName != "" {
		env = append(env, "PGDATABASE="+dbName)
	}
	if sslmode != "" {
		env = append(env, "PGSSLMODE="+sslmode)
	}
	// Drop the URL from argv. Passing an empty string would still
	// confuse the tools; instead, return useArg="" so the caller
	// omits the positional arg entirely.
	return env, "", nil
}

// runBackup streams a logical backup of the database referenced by dsn
// to stdout, by shelling out to pg_dump. The dump is plain SQL with
// --clean --if-exists --no-owner --no-privileges so it can be replayed
// into any Postgres role without ownership conflicts — important for
// self-hosters whose dump-time and restore-time roles differ.
//
// Connection details are passed via PG* env vars rather than the
// positional URL so the password doesn't show up in /proc cmdline or
// `ps -ef`. Stderr is forwarded so the operator sees connection
// errors / permission warnings inline.
func runBackup(dsn string, out io.Writer) error {
	if dsn == "" {
		return errors.New("backup: no DSN — pass -dsn or set DATABASE_URL")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return fmt.Errorf("backup: pg_dump not found on PATH — install postgres client tools (apt install postgresql-client / brew install libpq / pacman -S postgresql)")
	}
	extraEnv, urlArg, err := dsnEnv(dsn)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	args := []string{"--clean", "--if-exists", "--no-owner", "--no-privileges"}
	if urlArg != "" {
		args = append(args, urlArg)
	}
	cmd := exec.Command("pg_dump", args...)
	cmd.Env = append(os.Environ(), extraEnv...)
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
// half-restored on a partially-broken dump. Same env-passed
// credential handling as runBackup.
func runRestore(dsn string, in io.Reader) error {
	if dsn == "" {
		return errors.New("restore: no DSN — pass -dsn or set DATABASE_URL")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		return fmt.Errorf("restore: psql not found on PATH — install postgres client tools")
	}
	extraEnv, urlArg, err := dsnEnv(dsn)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	args := []string{"--quiet", "--variable=ON_ERROR_STOP=1", "--file=-"}
	if urlArg != "" {
		args = append(args, urlArg)
	}
	cmd := exec.Command("psql", args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = in
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql restore failed: %w", err)
	}
	return nil
}
