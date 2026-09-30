package mcp

// upload_adventure_media: the graph editor's media upload, for an
// assistant. The returned {id, kind} is what a block, hint or answer
// references. attach_image / attach_audio remain for lane adventures.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
)

func (s *Server) registerMediaTools() {
	s.tools.register(ToolDescriptor{
		Name:  "upload_adventure_media",
		Scope: scopeWrite,
		Description: "Upload an image, audio or video file to a graph adventure and get back its media {id, kind, mime_type, size_bytes}. " +
			`Reference it with edit_adventure_graph: a block takes the id ({"id":"b2","kind":"image","media":<id>,"alt":"…"}); a hint's media and an answer's image take {"id":<id>,"kind":"image"}. ` +
			"Bytes travel base64-encoded in the call. Images are re-encoded and shrunk; audio and video types are sniffed from the bytes. " +
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
