package db

import (
	"strings"
	"testing"
)

// TestCheckUniquePrefixes_RejectsNewDuplicates is the C5 regression
// guard: two sibling migrations sharing a 4-digit prefix must fail
// the boot-time sanity check unless they're explicitly grandfathered
// in `allowedDuplicatePrefixes`.
func TestCheckUniquePrefixes_RejectsNewDuplicates(t *testing.T) {
	t.Run("clean run", func(t *testing.T) {
		err := checkUniquePrefixes([]string{
			"0001_init.sql",
			"0002_add_users.sql",
			"0003_drop_thing.sql",
		})
		if err != nil {
			t.Fatalf("clean prefixes flagged: %v", err)
		}
	})

	t.Run("new duplicate is rejected", func(t *testing.T) {
		err := checkUniquePrefixes([]string{
			"0050_add_foo.sql",
			"0050_add_bar.sql",
		})
		if err == nil {
			t.Fatal("duplicate prefix accepted; want error")
		}
		if !strings.Contains(err.Error(), "0050") {
			t.Errorf("error missing offending prefix: %v", err)
		}
	})

	t.Run("grandfathered duplicate is allowed", func(t *testing.T) {
		// 0033 is the historical exception — both files exist in
		// the embed and have been applied to every deployed DB. The
		// guard must let them through.
		err := checkUniquePrefixes([]string{
			"0033_drop_adventure_template.sql",
			"0033_max_teams_to_max_players.sql",
		})
		if err != nil {
			t.Fatalf("grandfathered duplicate rejected: %v", err)
		}
	})
}

// TestEmbeddedMigrations_PrefixesAreUnique boots the same check the
// runtime path uses against the actual embedded migration filenames.
// Catches a developer who adds a new file that collides with an
// existing prefix without intending to.
func TestEmbeddedMigrations_PrefixesAreUnique(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if err := checkUniquePrefixes(names); err != nil {
		t.Errorf("embedded migrations have a non-grandfathered duplicate: %v", err)
	}
}
