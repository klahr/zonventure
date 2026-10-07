package server_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/settings"
)

// TestLegalSettings: only the platform admin reads and saves the legal
// values; a bad email is refused; a save publishes /privacy at once,
// dates the pages today, and the preview renders unsaved values.
func TestLegalSettings(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	t.Cleanup(func() { settings.LoadLegal(context.Background(), nil, nil) })

	boss := newClient(t, ts)
	login(t, boss, "boss", "s3cret")
	if s := boss.do("GET", "/api/admin/legal", nil, nil); s != 403 {
		t.Fatalf("org admin GET = %d, want 403", s)
	}
	if r, err := http.Get(ts.URL + "/privacy"); err != nil || r.StatusCode != 503 {
		t.Fatalf("unconfigured /privacy = %v %v, want 503", r.StatusCode, err)
	}

	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	if s := root.do("PUT", "/api/admin/legal", map[string]any{"contact_email": "not an email"}, nil); s != 400 {
		t.Fatalf("bad email = %d, want 400", s)
	}
	var got settings.Legal
	set := map[string]any{"company_name": "Zonventure AB", "contact_email": "hello@zonventure.example", "jurisdiction": "Sweden", "location_retention_days": 21}
	if s := root.do("PUT", "/api/admin/legal", set, &got); s != 200 || got.LastUpdated == "" || got.LocationRetentionDays != 21 {
		t.Fatalf("save = %d %+v", s, got)
	}

	r, err := http.Get(ts.URL + "/privacy")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !strings.Contains(string(body), "Zonventure AB") || !strings.Contains(string(body), "21") {
		t.Fatalf("/privacy after save = %d", r.StatusCode)
	}

	if s := root.doAny("POST", "/api/admin/legal/preview", map[string]any{"page": "terms", "legal": map[string]any{"company_name": "Draft Ltd"}}, nil); s != 200 {
		t.Fatalf("preview = %d", s)
	}
	if settings.CurrentLegal().CompanyName != "Zonventure AB" {
		t.Fatalf("preview saved its values")
	}
}

// TestLegalSettings_FromEnv: the first start copies the old env vars in,
// so a deploy keeps its pages without retyping anything.
func TestLegalSettings_FromEnv(t *testing.T) {
	t.Setenv("ZV_COMPANY_NAME", "Env AB")
	t.Setenv("ZV_AUDIT_RETENTION_DAYS", "60")
	t.Setenv("ZV_LEGAL_REVIEWED", "1")
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	t.Cleanup(func() { settings.LoadLegal(context.Background(), nil, nil) })

	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	var got settings.Legal
	if s := root.do("GET", "/api/admin/legal", nil, &got); s != 200 || got.CompanyName != "Env AB" || got.AuditRetentionDays != 60 || !got.Reviewed || got.LocationRetentionDays != 30 {
		t.Fatalf("imported = %d %+v", s, got)
	}
}
