package admin

import (
	"context"
	"database/sql"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// The start radius takes a number of metres in range, refuses one out
// of it, and clears with null.
func TestStartRadiusMeta(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Meet') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('meet', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	radius := func() sql.NullFloat64 {
		var r sql.NullFloat64
		if err := db.QueryRow(`SELECT start_radius_m FROM adventures WHERE slug = 'meet'`).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	if err := UpdateAdventureMetaJSON(ctx, db, "meet", []byte(`{"start_lat": 59.3, "start_lng": 18.0, "start_radius_m": 40}`)); err != nil {
		t.Fatal(err)
	}
	if r := radius(); !r.Valid || r.Float64 != 40 {
		t.Fatalf("after setting 40 m: %+v", r)
	}
	for _, bad := range []string{`{"start_radius_m": 2}`, `{"start_radius_m": 6000}`} {
		if err := UpdateAdventureMetaJSON(ctx, db, "meet", []byte(bad)); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	if r := radius(); r.Float64 != 40 {
		t.Errorf("a refused radius changed it: %+v", r)
	}
	if err := UpdateAdventureMetaJSON(ctx, db, "meet", []byte(`{"start_radius_m": null}`)); err != nil {
		t.Fatal(err)
	}
	if r := radius(); r.Valid {
		t.Errorf("null should clear the requirement: %+v", r)
	}
}

// Null clears the starting point, and an absent field leaves it alone:
// a *Nullable field could not tell the two apart, so clearing it in the
// details dialog silently did nothing.
func TestClearStartPoint(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Clear') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id, start_lat, start_lng) VALUES ('clear', 't', '', '', $1, 59.3, 18.0)`, orgID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	point := func() (lat, lng sql.NullFloat64) {
		if err := db.QueryRow(`SELECT start_lat, start_lng FROM adventures WHERE slug = 'clear'`).Scan(&lat, &lng); err != nil {
			t.Fatal(err)
		}
		return lat, lng
	}
	if err := UpdateAdventureMetaJSON(ctx, db, "clear", []byte(`{"title": "renamed"}`)); err != nil {
		t.Fatal(err)
	}
	if lat, lng := point(); !lat.Valid || !lng.Valid {
		t.Fatalf("an absent start_lat cleared it: %+v %+v", lat, lng)
	}
	if err := UpdateAdventureMetaJSON(ctx, db, "clear", []byte(`{"start_lat": null, "start_lng": null}`)); err != nil {
		t.Fatal(err)
	}
	if lat, lng := point(); lat.Valid || lng.Valid {
		t.Errorf("null should clear the starting point: %+v %+v", lat, lng)
	}
}
