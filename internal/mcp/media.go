package mcp

// upload_adventure_media: the graph editor's media upload, for an
// assistant. The returned {id, kind} is what a block, hint or answer
// references.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
)

func (s *Server) registerMediaTools() {
	s.tools.register(ToolDescriptor{
		Name:     "get_upload_link",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: "Get a link for adding a file without sending its bytes through this chat — the way to add pictures from a phone. " +
			"for: picture (default) adds a file to the adventure's media: a step's picture, sound or video, or a print material's PDF (the page offers pictures, sounds and videos; send a PDF with curl); icon sets the adventure's icon; event_cover sets an event's cover (event_id). " +
			"Write the link in the chat: the user taps it, picks the file and comes back. Then, for a picture, list_adventure_media gives its id to use in a block. " +
			"With a shell you can also send the file yourself: curl -sS -F file=@/path/to/photo.jpg '<link>'. The link expires after 30 minutes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure slug; for picture and icon."},
				"for":      map[string]any{"type": "string", "enum": []string{"picture", "icon", "event_cover"}, "description": "What the file is for; picture when left out."},
				"event_id": map[string]any{"type": "integer", "description": "The event, for event_cover."},
			},
		},
	}, getUploadLink)

	s.tools.register(ToolDescriptor{
		Name:     "list_adventure_media",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "The adventure's uploaded pictures, sounds, videos and PDFs, newest first: {id, kind, mime_type, size_bytes, uploaded_at}. " +
			"Use it to find the id of a file the user just uploaded through get_upload_link.",
		InputSchema: slugOnly,
	}, listAdventureMedia)

	s.tools.register(ToolDescriptor{
		Name:     "upload_adventure_media",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: "Upload an image, audio, video or PDF file to a graph adventure and get back its media {id, kind, mime_type, size_bytes}. " +
			`Reference it with edit_adventure_graph: a block takes the id ({"id":"b2","kind":"image","media":<id>,"alt":"…"}); a hint's media and an answer's image take {"id":<id>,"kind":"image"}; a PDF goes only in a print material (put_print_material). ` +
			"Bytes travel base64-encoded in the call, which is only reliable for small files: prefer get_upload_link. " +
			"Images are re-encoded and shrunk; audio and video types are sniffed from the bytes. " +
			"Size caps are the server's ZV_MAX_IMAGE_BYTES / ZV_MAX_AUDIO_BYTES / ZV_MAX_VIDEO_BYTES.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "data_b64"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure slug."},
				"data_b64": map[string]any{"type": "string", "description": "The file, base64-encoded."},
				"filename": map[string]any{"type": "string", "description": "Optional original file name."},
			},
		},
	}, uploadAdventureMedia)
}

func uploadAdventureMedia(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug     string `json:"slug"`
		DataB64  string `json:"data_b64"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if a.Slug == "" || a.DataB64 == "" {
		return errorResult("slug and data_b64 are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	data, err := base64.StdEncoding.DecodeString(a.DataB64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(a.DataB64)
	}
	if err != nil {
		return errorResult("data_b64 is not valid base64: " + err.Error()), nil
	}
	if a.Filename == "" {
		a.Filename = "upload"
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", a.Filename)
	if err != nil {
		return errorResult("encoding upload: " + err.Error()), nil
	}
	_, _ = fw.Write(data)
	if err := mw.Close(); err != nil {
		return errorResult("encoding upload: " + err.Error()), nil
	}

	// callAdmin speaks JSON only; this is the same shim for a multipart body.
	req := httptest.NewRequest("POST", "/api/admin/adventures/"+a.Slug+"/media", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", a.Slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.Admin.UploadAdventureMedia(rec, req)
	return adminCallResult(rec.Code, rec.Body.Bytes()), nil
}

func getUploadLink(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug    string `json:"slug"`
		For     string `json:"for"`
		EventID int64  `json:"event_id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	purpose := map[string]string{"": admin.UploadMedia, "picture": admin.UploadMedia, "icon": admin.UploadIcon, "event_cover": admin.UploadEventCover}[a.For]
	switch {
	case purpose == "":
		return errorResult("for is picture, icon or event_cover"), nil
	case purpose == admin.UploadEventCover && a.EventID <= 0:
		return errorResult("event_id is required for an event cover"), nil
	case purpose != admin.UploadEventCover && a.Slug == "":
		return errorResult("slug is required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	base := strings.TrimRight(s.Admin.PublicURL, "/")
	if base == "" {
		return errorResult("this server has no public URL (ZV_PUBLIC_URL), so it cannot hand out upload links; use upload_adventure_media"), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	secret, _, err := admin.CreateUploadLink(ctx, s.DB, me, purpose, a.Slug, a.EventID)
	if err != nil {
		return errorResult("cannot make an upload link: " + err.Error()), nil
	}
	link := base + "/api/media-upload/" + secret
	what := map[string]string{admin.UploadMedia: "a picture, sound, video or PDF", admin.UploadIcon: "the adventure's icon", admin.UploadEventCover: "the event's cover"}[purpose]
	return textResult(fmt.Sprintf("Upload link for %s (30 minutes): %s\nWrite it in the chat for the user to tap; or, with a shell: curl -sS -F file=@/path/to/file '%s'", what, link, link)), nil
}

func listAdventureMedia(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" {
		return errorResult("slug is required"), nil
	}
	advID, _, bad := authoriseEdit(ctx, s, a.Slug)
	if bad != nil {
		return *bad, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, kind, mime_type, size_bytes, created_at FROM adventure_media
		WHERE adventure_id = $1 ORDER BY created_at DESC, id DESC LIMIT 50
	`, advID)
	if err != nil {
		return errorResult("listing media: " + err.Error()), nil
	}
	defer rows.Close()
	type item struct {
		ID         int64     `json:"id"`
		Kind       string    `json:"kind"`
		MimeType   string    `json:"mime_type"`
		SizeBytes  int64     `json:"size_bytes"`
		UploadedAt time.Time `json:"uploaded_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Kind, &it.MimeType, &it.SizeBytes, &it.UploadedAt); err != nil {
			return errorResult("listing media: " + err.Error()), nil
		}
		out = append(out, it)
	}
	b, _ := json.Marshal(map[string]any{"media": out})
	return textResult(string(b)), nil
}
