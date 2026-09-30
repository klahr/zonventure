package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	imgpng "image/png"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// An assistant with a shell uploads through a link, never through the
// conversation: the link works for its adventure and nothing else.
func TestMediaUploadURL(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('pics', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	h := &admin.Handlers{DB: db, PublicURL: "https://zv.example/"}
	s := &Server{DB: db, Admin: h, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})

	res, _ := getMediaUploadURL(ctx, s, json.RawMessage(`{"slug":"pics"}`))
	if res.IsError {
		t.Fatalf("get url: %s", textOf(res))
	}
	var out struct {
		URL string `json:"upload_url"`
	}
	_ = json.Unmarshal([]byte(textOf(res)), &out)
	ticket, ok := strings.CutPrefix(out.URL, "https://zv.example/api/media-upload/")
	if !ok || len(ticket) < 32 {
		t.Fatalf("upload_url = %q", out.URL)
	}

	upload := func(ticket string) (int, string) {
		var png, body bytes.Buffer
		_ = imgpng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 4, 4)))
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "door.png")
		_, _ = fw.Write(png.Bytes())
		_ = mw.Close()
		req := httptest.NewRequest("POST", "/api/media-upload/"+ticket, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("ticket", ticket)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.UploadMediaByTicket(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := upload(ticket); code >= 300 || !strings.Contains(body, `"kind":"image"`) {
		t.Fatalf("upload: %d %s", code, body)
	}
	if code, _ := upload("not-a-ticket"); code != 404 {
		t.Errorf("an unknown link should be 404, got %d", code)
	}
	if _, err := db.Exec(`UPDATE media_upload_tickets SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if code, _ := upload(ticket); code != 404 {
		t.Errorf("an expired link should be 404, got %d", code)
	}

	// Someone who cannot edit the adventure gets no link.
	other := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: new(int64)})
	if res, _ := getMediaUploadURL(other, s, json.RawMessage(`{"slug":"pics"}`)); !res.IsError {
		t.Error("a user outside the org should be refused a link")
	}
}
