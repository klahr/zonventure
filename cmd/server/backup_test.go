package main

import (
	"slices"
	"testing"
)

// TestDSNEnv_StripsPasswordFromArgv locks down the round-5 fix: the
// password component of a libpq URL DSN must not end up in pg_dump /
// psql argv (visible to any local user via `ps -ef` /
// /proc/<pid>/cmdline). Instead it travels via PGPASSWORD in the
// child's environment, which is only readable by the same UID.
func TestDSNEnv_StripsPasswordFromArgv(t *testing.T) {
	const secret = "s3cretP@ss"
	dsn := "postgres://zv:" + secret + "@127.0.0.1:5432/zv?sslmode=disable"
	env, urlArg, err := dsnEnv(dsn)
	if err != nil {
		t.Fatalf("dsnEnv: %v", err)
	}
	if urlArg != "" {
		t.Errorf("urlArg = %q, want empty so the tool drops the URL from argv", urlArg)
	}
	if !slices.Contains(env, "PGPASSWORD="+secret) {
		t.Errorf("env missing PGPASSWORD=%s entry; got %v", secret, env)
	}
	for _, e := range env {
		if e == "PGPASSWORD="+secret {
			continue
		}
		if contains(e, secret) {
			t.Errorf("password leaked into env entry %q", e)
		}
	}
	for _, want := range []string{"PGHOST=127.0.0.1", "PGPORT=5432", "PGUSER=zv", "PGDATABASE=zv", "PGSSLMODE=disable"} {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q; got %v", want, env)
		}
	}
}

// TestDSNEnv_KeyValueDSN passes through unchanged: key=value strings
// don't carry the password in URL-form so there's nothing to extract.
func TestDSNEnv_KeyValueDSN(t *testing.T) {
	dsn := "host=db user=zv password=hunter2 dbname=zv sslmode=disable"
	env, urlArg, err := dsnEnv(dsn)
	if err != nil {
		t.Fatalf("dsnEnv: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("expected no extra env for key=value DSN, got %v", env)
	}
	if urlArg != dsn {
		t.Errorf("urlArg = %q, want passthrough", urlArg)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
