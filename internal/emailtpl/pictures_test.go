package emailtpl

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

func TestPictureRenders(t *testing.T) {
	for _, th := range Themes {
		wide := Content{Heading: "Hi", Picture: Picture{URL: "https://example.org/assets/email/header.jpg", Alt: "A route", Wide: true}}
		h, text := Render(th.ID, Platform(), "s", wide)
		if !strings.Contains(h, `src="https://example.org/assets/email/header.jpg"`) || !strings.Contains(h, `alt="A route"`) || !strings.Contains(h, "width=\"") {
			t.Errorf("%s: banner missing its address, alt or width", th.ID)
		}
		if strings.Contains(text, "header.jpg") {
			t.Errorf("%s: the text version shows the picture", th.ID)
		}
		spot := Content{Heading: "Hi", Picture: Picture{URL: "https://example.org/assets/email/check-email.jpg", Alt: "An envelope"}}
		if h, _ := Render(th.ID, Platform(), "s", spot); !strings.Contains(h, `width="140"`) || !strings.Contains(h, `alt="An envelope"`) {
			t.Errorf("%s: spot missing its size or alt", th.ID)
		}
		if h, _ := Render(th.ID, Platform(), "s", Content{Heading: "Hi"}); strings.Contains(h, "/assets/email/") {
			t.Errorf("%s: a picture shows without one", th.ID)
		}
	}
}

func TestPicturesNeedABase(t *testing.T) {
	if p := House("", "en", PictureVerify); p != (Picture{}) {
		t.Errorf("House without a base = %+v", p)
	}
	if p := coverOf("", "en", "x", "pirate", true); p != (Picture{}) {
		t.Errorf("coverOf without a base = %+v", p)
	}
	p := House("https://zv.example/", "sv", PictureVerify)
	if p.URL != "https://zv.example/assets/email/check-email.jpg" || p.Wide || p.Alt == "" || p.Alt == housePictures["check-email"].alt {
		t.Errorf("House = %+v, want the Swedish spot at the base", p)
	}
}

func TestCoverOfPrefersTheAdventuresOwn(t *testing.T) {
	base := "https://zv.example"
	if p := coverOf(base, "en", "harbour", "pirate", true); p.URL != base+"/adventure-assets/cover/harbour" || !p.Wide {
		t.Errorf("own cover = %+v", p)
	}
	if p := coverOf(base, "en", "harbour", "pirate", false); p.URL != base+"/assets/email/covers/pirate.jpg" {
		t.Errorf("theme cover = %+v", p)
	}
	if p := coverOf(base, "en", "harbour", "", false); p.URL != base+"/assets/email/header.jpg" {
		t.Errorf("no theme = %+v, want the house banner", p)
	}
	if p := coverOf(base, "en", "harbour", "no-such-theme", false); p.URL != base+"/assets/email/header.jpg" {
		t.Errorf("unknown theme = %+v, want the house banner", p)
	}
}

func TestEveryPictureHasAFile(t *testing.T) {
	for _, f := range HouseFiles() {
		if _, err := os.Stat("../server/web/assets/email/" + f); err != nil {
			t.Errorf("house picture %s: %v", f, err)
		}
	}
	for _, id := range CoverThemes {
		if _, err := os.Stat("../server/web/assets/email/covers/" + id + ".jpg"); err != nil {
			t.Errorf("theme cover %s: %v", id, err)
		}
	}
}

func TestCoverForReadsTheAdventure(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	org := dbtest.PublisherOrg(t, db)
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id, trail_theme) VALUES ('harbour', 't', '', '', $1, 'pirate')`, org); err != nil {
		t.Fatal(err)
	}
	base, ctx := "https://zv.example", context.Background()
	if p := CoverFor(ctx, db, base, "en", "harbour"); p.URL != base+"/assets/email/covers/pirate.jpg" {
		t.Errorf("themed adventure = %+v", p)
	}
	if p := CoverFor(ctx, db, base, "en", "no-such-adventure"); p.URL != base+"/assets/email/header.jpg" {
		t.Errorf("missing adventure = %+v, want the house banner", p)
	}
}
