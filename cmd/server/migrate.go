package main

// CLI fallback for the org-bundle export / import flow. Mirrors the
// `-backup` / `-restore` shape: open the DB, run the operation, exit.
// The same code paths back the GUI surface (admin handlers in
// internal/admin/migrate.go); these flags are the headless / scripted
// alternative for operators without browser access during cutover.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/db"
)

// runExportOrg streams the bundle for orgID to out. The caller has
// already resolved the DSN; we open + close the pool here so the
// flag handler can invoke before the long-lived server connection
// pool is created.
func runExportOrg(dsn string, orgID int64, out io.Writer) error {
	if dsn == "" {
		return errors.New("export-org: no DSN — pass -dsn or set DATABASE_URL")
	}
	if orgID <= 0 {
		return errors.New("export-org: org id must be a positive integer")
	}
	sqlDB, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("export-org: open db: %w", err)
	}
	defer sqlDB.Close()

	h := &admin.Handlers{DB: sqlDB}
	env, files, err := h.ExportOrgEnvelope(context.Background(), orgID)
	if err != nil {
		return fmt.Errorf("export-org: build bundle: %w", err)
	}
	if err := admin.WriteOrgBundle(out, env, files); err != nil {
		return fmt.Errorf("export-org: write bundle: %w", err)
	}
	return nil
}

// runImportOrg reads a bundle zip from `in` and creates a fresh org
// on the destination. Echoes the import summary to stderr so a
// scripted operator sees what landed without having to parse the
// commit logs.
func runImportOrg(dsn string, in io.Reader) error {
	if dsn == "" {
		return errors.New("import-org: no DSN — pass -dsn or set DATABASE_URL")
	}
	sqlDB, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("import-org: open db: %w", err)
	}
	defer sqlDB.Close()

	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("import-org: read bundle: %w", err)
	}
	summary, err := admin.ImportOrgBundle(context.Background(), sqlDB, data)
	if err != nil {
		return fmt.Errorf("import-org: %w", err)
	}
	fmt.Fprintf(os.Stderr,
		"imported org %d: %d adventures, %d events, %d teams, %d slots, %d ticket purchases, %d assignments\n",
		summary.OrgID, summary.Adventures, summary.Events, summary.Teams,
		summary.Slots, summary.Purchases, summary.Assignments)
	return nil
}
