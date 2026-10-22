package admin

import (
	"context"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// An adventure's facts replace the whole set, refuse one out of bounds,
// and come back with the access note in the details dialog's load.
func TestAdventureFacts(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Facts') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('facts', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := UpdateAdventureMetaJSON(ctx, db, "facts", []byte(`{"facts": {"minutes": 90, "min_age": 10, "access": "step_free"}, "access_note": " Ramp at the gate "}`)); err != nil {
		t.Fatal(err)
	}
	a, err := loadAdventureDTO(ctx, db, "facts")
	if err != nil {
		t.Fatal(err)
	}
	if a.Facts.Minutes != 90 || a.Facts.MinAge != 10 || a.Facts.Access != "step_free" || a.AccessNote != "Ramp at the gate" {
		t.Fatalf("after setting: %+v %q", a.Facts, a.AccessNote)
	}
	for _, bad := range []string{`{"facts": {"minutes": -5}}`, `{"facts": {"difficulty": "brutal"}}`, `{"facts": {"access": "ramps"}}`} {
		if _, err := UpdateAdventureMetaJSON(ctx, db, "facts", []byte(bad)); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	if _, err := UpdateAdventureMetaJSON(ctx, db, "facts", []byte(`{"facts": {"difficulty": "hard"}}`)); err != nil {
		t.Fatal(err)
	}
	if a, err = loadAdventureDTO(ctx, db, "facts"); err != nil {
		t.Fatal(err)
	}
	if a.Facts.Minutes != 0 || a.Facts.Difficulty != "hard" || a.AccessNote != "Ramp at the gate" {
		t.Errorf("facts should be replaced whole and leave the note: %+v %q", a.Facts, a.AccessNote)
	}
}
