package admin

// Adventure export / import — moves a single adventure (metadata,
// lanes, pages, hints, accept lists, image + audio binaries, and the
// avatar) between deployments as a zip archive.
//
// File shape:
//
//	zonventure-adventure-v1.zip
//	├── adventure.json     manifest (everything except the binaries)
//	├── avatar.<ext>       optional adventure cover
//	├── p<i>_img<j>.<ext>  per-page image J of page I (idx-based)
//	├── p<i>_aud<j>.<ext>  per-page audio J of page I
//	├── p<i>_h<k>_img<j>.<ext>
//	├── p<i>_h<k>_aud<j>.<ext>
//	└── g<id>.<ext>        graph media, by the exporting instance's id
//
// A graph adventure also carries its graph (the published version, or
// the draft when none is published) as a v2 envelope under "graph",
// with "graph_media" naming the file each of its media ids is in. The
// field is optional: a lane-only export imports as it always has.
//
// The manifest never carries IDs; everything is keyed by idx so the
// importer can mint fresh primary keys without surprises. Accept
// lists, lane mode, language, strict_assets, start_*, intro/outro and
// the colour palette all round-trip; what stays behind: the org id,
// shares, assignments, events / runs (those are deployment-local).

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// adventureExportFormat tags the manifest so future format bumps are
// detectable. The importer rejects unknown values up front rather
// than partially-parsing an alien shape.
const adventureExportFormat = "zonventure-adventure-v1"

// maxImportZipBytes caps the multipart upload so a malicious operator
// can't OOM the server with an arbitrarily-large archive. 50 MiB is
// well above any reasonable adventure (puzzle media downscale to
// ~50 KB each via imageutil.Shrink, audio at a few MB at most), and
// the Shrink-on-import path enforces the same per-image cap as
// authoring uploads anyway.
const maxImportZipBytes = 50 << 20 // 50 MiB

/* ---------------- Manifest shapes ---------------- */

type exportEnvelope struct {
	Format     string             `json:"format"`
	ExportedAt string             `json:"exported_at"`
	Adventure  exportAdventureDTO `json:"adventure"`
	// Graph is the adventure's graph, when it has one. Its media ids
	// are the exporting instance's; GraphMedia maps each to its file.
	Graph      *graph.Envelope    `json:"graph,omitempty"`
	GraphMedia []exportGraphMedia `json:"graph_media,omitempty"`
}

// exportGraphMedia is one adventure_media row the graph refers to.
type exportGraphMedia struct {
	ID       int64           `json:"id"`
	Kind     graph.MediaKind `json:"kind"`
	File     string          `json:"file"`
	MimeType string          `json:"mime_type"`
}

type exportAdventureDTO struct {
	Slug                 string          `json:"slug"`
	Title                string          `json:"title"`
	Description          string          `json:"description"`
	Author               string          `json:"author,omitempty"`
	IntroText            string          `json:"intro_text,omitempty"`
	OutroText            string          `json:"outro_text,omitempty"`
	Language             string          `json:"language,omitempty"`
	StrictAssets         bool            `json:"strict_assets,omitempty"`
	StartLat             *float64        `json:"start_lat,omitempty"`
	StartLng             *float64        `json:"start_lng,omitempty"`
	StartLabel           string          `json:"start_label,omitempty"`
	AccentColor          string          `json:"accent_color,omitempty"`
	AccentSecondaryColor string          `json:"accent_secondary_color,omitempty"`
	BackgroundColor      string          `json:"background_color,omitempty"`
	TextColor            string          `json:"text_color,omitempty"`
	SubmitBgColor        string          `json:"submit_bg_color,omitempty"`
	Avatar               *exportAssetRef `json:"avatar,omitempty"`
	Lanes                []exportLane    `json:"lanes"`
	Pages                []exportPage    `json:"pages"`
}

type exportLane struct {
	Idx  int    `json:"idx"`
	Mode string `json:"mode"`
	Name string `json:"name,omitempty"`
}

type exportPage struct {
	Idx      int                     `json:"idx"`
	LaneIdx  int                     `json:"lane_idx"`
	Title    string                  `json:"title"`
	Prompt   string                  `json:"prompt,omitempty"`
	Accept   []adventure.AcceptEntry `json:"accept,omitempty"`
	Lat      float64                 `json:"lat"`
	Lng      float64                 `json:"lng"`
	RadiusM  *float64                `json:"radius_m,omitempty"`
	Disabled bool                    `json:"disabled,omitempty"`
	Notes    string                  `json:"notes,omitempty"`
	Hints    []exportHint            `json:"hints,omitempty"`
	Images   []exportAsset           `json:"images,omitempty"`
	Audio    []exportAsset           `json:"audio,omitempty"`
}

type exportHint struct {
	Idx          int           `json:"idx"`
	Text         string        `json:"text"`
	DelaySeconds int           `json:"delay_seconds"`
	Images       []exportAsset `json:"images,omitempty"`
	Audio        []exportAsset `json:"audio,omitempty"`
}

type exportAsset struct {
	Idx         int    `json:"idx"`
	File        string `json:"file"`
	MimeType    string `json:"mime_type,omitempty"`
	AltText     string `json:"alt_text,omitempty"`
	Description string `json:"description,omitempty"`
}

type exportAssetRef struct {
	File     string `json:"file"`
	MimeType string `json:"mime_type"`
}

/* ---------------- Export ---------------- */

// ExportAdventure streams a zip archive of the named adventure.
// Auth: requireEditAdventure — only callers who can edit the
// adventure can export it (export carries the answer key + every
// hint body).
//
// GET /api/admin/adventures/{slug}/export
func (h *Handlers) ExportAdventure(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	env, files, err := h.buildExport(r.Context(), advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "export failed", err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+sanitiseFilename(slug)+`.zvadv.zip"`)
	zw := zip.NewWriter(w)
	mw, err := zw.Create("adventure.json")
	if err != nil {
		logx.Warn("admin.export.zip_manifest", "err", err.Error())
		return
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		logx.Warn("admin.export.encode_manifest", "err", err.Error())
		return
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			logx.Warn("admin.export.zip_file", "name", f.name, "err", err.Error())
			return
		}
		if _, err := fw.Write(f.bytes); err != nil {
			logx.Warn("admin.export.write_file", "name", f.name, "err", err.Error())
			return
		}
	}
	if err := zw.Close(); err != nil {
		logx.Warn("admin.export.zip_close", "err", err.Error())
	}
}

type exportFile struct {
	name  string
	bytes []byte
}

// buildExport runs every query the manifest needs and stages the
// asset bytes for the caller to stream into the zip. Pure DB-side;
// no http.ResponseWriter access so the caller can decide error
// shape.
func (h *Handlers) buildExport(ctx context.Context, advID int64) (exportEnvelope, []exportFile, error) {
	var (
		env exportEnvelope
		adv exportAdventureDTO
	)
	env.Format = adventureExportFormat
	env.ExportedAt = time.Now().UTC().Format(time.RFC3339)

	var (
		accent, accent2, bg, ink, submitBG sql.NullString
		startLat, startLng                 sql.NullFloat64
		avatarMime                         sql.NullString
		avatarContent                      []byte
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
		       language, strict_assets,
		       start_lat, start_lng, start_label,
		       avatar_mime_type, avatar_content
		FROM adventures WHERE id = $1
	`, advID).Scan(&adv.Slug, &adv.Title, &adv.Description, &adv.Author,
		&adv.IntroText, &adv.OutroText,
		&accent, &accent2, &bg, &ink, &submitBG,
		&adv.Language, &adv.StrictAssets,
		&startLat, &startLng, &adv.StartLabel,
		&avatarMime, &avatarContent)
	if err != nil {
		return env, nil, fmt.Errorf("load adventure: %w", err)
	}
	adv.AccentColor = accent.String
	adv.AccentSecondaryColor = accent2.String
	adv.BackgroundColor = bg.String
	adv.TextColor = ink.String
	adv.SubmitBgColor = submitBG.String
	if startLat.Valid {
		v := startLat.Float64
		adv.StartLat = &v
	}
	if startLng.Valid {
		v := startLng.Float64
		adv.StartLng = &v
	}

	files := []exportFile{}
	if avatarMime.Valid && len(avatarContent) > 0 {
		ext := extForMime(avatarMime.String)
		name := "avatar" + ext
		files = append(files, exportFile{name: name, bytes: avatarContent})
		adv.Avatar = &exportAssetRef{File: name, MimeType: avatarMime.String}
	}

	// Lanes — keyed by idx in the manifest. Build a lane-id → idx
	// map so per-page lane references can be resolved without
	// extra round-trips.
	laneRows, err := h.DB.QueryContext(ctx, `
		SELECT id, idx, mode, name
		FROM adventure_lanes WHERE adventure_id = $1 ORDER BY idx
	`, advID)
	if err != nil {
		return env, nil, fmt.Errorf("load lanes: %w", err)
	}
	laneIdxByID := map[int64]int{}
	for laneRows.Next() {
		var (
			id   int64
			idx  int
			mode string
			name string
		)
		if err := laneRows.Scan(&id, &idx, &mode, &name); err != nil {
			laneRows.Close()
			return env, nil, fmt.Errorf("scan lane: %w", err)
		}
		laneIdxByID[id] = idx
		adv.Lanes = append(adv.Lanes, exportLane{Idx: idx, Mode: mode, Name: name})
	}
	if err := laneRows.Err(); err != nil {
		laneRows.Close()
		return env, nil, fmt.Errorf("rows lanes: %w", err)
	}
	laneRows.Close()

	// Pages — load all pages, then hints + images + audio per page.
	pageRows, err := h.DB.QueryContext(ctx, `
		SELECT id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, lane_id
		FROM adventure_pages WHERE adventure_id = $1 ORDER BY idx
	`, advID)
	if err != nil {
		return env, nil, fmt.Errorf("load pages: %w", err)
	}
	type pageRow struct {
		id      int64
		idx     int
		laneID  sql.NullInt64
		page    exportPage
		acceptJ string
	}
	var pages []pageRow
	for pageRows.Next() {
		var (
			pr       pageRow
			radius   sql.NullFloat64
			disabled int
			laneID   sql.NullInt64
		)
		if err := pageRows.Scan(&pr.id, &pr.idx, &pr.page.Title, &pr.page.Prompt,
			&pr.acceptJ, &pr.page.Lat, &pr.page.Lng, &radius, &disabled,
			&pr.page.Notes, &laneID); err != nil {
			pageRows.Close()
			return env, nil, fmt.Errorf("scan page: %w", err)
		}
		pr.page.Idx = pr.idx
		if radius.Valid {
			v := radius.Float64
			pr.page.RadiusM = &v
		}
		pr.page.Disabled = disabled != 0
		pr.laneID = laneID
		pages = append(pages, pr)
	}
	if err := pageRows.Err(); err != nil {
		pageRows.Close()
		return env, nil, fmt.Errorf("rows pages: %w", err)
	}
	pageRows.Close()

	for i := range pages {
		// Resolve lane reference.
		if pages[i].laneID.Valid {
			if idx, ok := laneIdxByID[pages[i].laneID.Int64]; ok {
				pages[i].page.LaneIdx = idx
			}
		}
		// Decode accept JSON via the same tolerant parser the loader
		// uses, so a legacy []string row exports as a clean
		// []AcceptEntry.
		if accepts, perr := adventure.ParseAcceptJSON([]byte(pages[i].acceptJ)); perr == nil {
			pages[i].page.Accept = accepts
		}
		// Hints.
		hintRows, err := h.DB.QueryContext(ctx, `
			SELECT id, idx, text, delay_seconds
			FROM adventure_page_hints WHERE page_id = $1 ORDER BY idx
		`, pages[i].id)
		if err != nil {
			return env, nil, fmt.Errorf("load hints: %w", err)
		}
		type hintRow struct {
			id   int64
			idx  int
			hint exportHint
		}
		var hints []hintRow
		for hintRows.Next() {
			var hr hintRow
			if err := hintRows.Scan(&hr.id, &hr.idx, &hr.hint.Text, &hr.hint.DelaySeconds); err != nil {
				hintRows.Close()
				return env, nil, fmt.Errorf("scan hint: %w", err)
			}
			hr.hint.Idx = hr.idx
			hints = append(hints, hr)
		}
		hintRows.Close()
		// Page images.
		imgs, imgFiles, err := h.exportPageImages(ctx, pages[i].id, pages[i].idx)
		if err != nil {
			return env, nil, err
		}
		pages[i].page.Images = imgs
		files = append(files, imgFiles...)
		// Page audio.
		auds, audFiles, err := h.exportPageAudio(ctx, pages[i].id, pages[i].idx)
		if err != nil {
			return env, nil, err
		}
		pages[i].page.Audio = auds
		files = append(files, audFiles...)
		// Per-hint images / audio.
		for hi := range hints {
			himgs, hImgFiles, err := h.exportHintImages(ctx, hints[hi].id, pages[i].idx, hints[hi].idx)
			if err != nil {
				return env, nil, err
			}
			hints[hi].hint.Images = himgs
			files = append(files, hImgFiles...)
			hauds, hAudFiles, err := h.exportHintAudio(ctx, hints[hi].id, pages[i].idx, hints[hi].idx)
			if err != nil {
				return env, nil, err
			}
			hints[hi].hint.Audio = hauds
			files = append(files, hAudFiles...)
		}
		for _, hr := range hints {
			pages[i].page.Hints = append(pages[i].page.Hints, hr.hint)
		}
		adv.Pages = append(adv.Pages, pages[i].page)
	}

	env.Adventure = adv

	g, err := exportableGraph(ctx, h.DB, advID)
	if err != nil {
		return env, nil, err
	}
	if g != nil {
		env.Graph = &graph.Envelope{Format: graph.FormatV2, Graph: *g}
		for _, id := range g.MediaIDs() {
			if id == 0 {
				continue
			}
			var (
				kind    string
				mime    string
				content []byte
			)
			if err := h.DB.QueryRowContext(ctx, `
				SELECT kind, mime_type, content FROM adventure_media
				WHERE id = $1 AND adventure_id = $2
			`, id, advID).Scan(&kind, &mime, &content); err != nil {
				return env, nil, fmt.Errorf("load graph media %d: %w", id, err)
			}
			name := fmt.Sprintf("g%d%s", id, extForMime(mime))
			files = append(files, exportFile{name: name, bytes: content})
			env.GraphMedia = append(env.GraphMedia, exportGraphMedia{
				ID: id, Kind: graph.MediaKind(kind), File: name, MimeType: mime,
			})
		}
	}
	return env, files, nil
}

// exportableGraph is the graph an export carries: the published one, or
// the draft when none is published, or nil for a lane-only adventure.
func exportableGraph(ctx context.Context, db *sql.DB, advID int64) (*graph.Graph, error) {
	s, err := graphstore.Published(ctx, db, advID)
	if err == nil {
		return s.Graph, nil
	}
	if !errors.Is(err, graphstore.ErrNotFound) {
		return nil, fmt.Errorf("load published graph: %w", err)
	}
	d, err := graphstore.LoadDraft(ctx, db, advID)
	if errors.Is(err, graphstore.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load draft graph: %w", err)
	}
	return d.Graph, nil
}

func (h *Handlers) exportPageImages(ctx context.Context, pageID int64, pageIdx int) ([]exportAsset, []exportFile, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT idx, mime_type, content, COALESCE(alt_text, '')
		FROM adventure_page_images WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, nil, fmt.Errorf("load page images: %w", err)
	}
	defer rows.Close()
	var assets []exportAsset
	var files []exportFile
	for rows.Next() {
		var (
			idx     int
			mime    string
			content []byte
			alt     string
		)
		if err := rows.Scan(&idx, &mime, &content, &alt); err != nil {
			return nil, nil, fmt.Errorf("scan page image: %w", err)
		}
		name := fmt.Sprintf("p%d_img%d%s", pageIdx, idx, extForMime(mime))
		files = append(files, exportFile{name: name, bytes: content})
		assets = append(assets, exportAsset{
			Idx: idx, File: name, MimeType: mime, AltText: alt,
		})
	}
	return assets, files, rows.Err()
}

func (h *Handlers) exportPageAudio(ctx context.Context, pageID int64, pageIdx int) ([]exportAsset, []exportFile, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT idx, mime_type, content, COALESCE(description, '')
		FROM adventure_page_audio WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, nil, fmt.Errorf("load page audio: %w", err)
	}
	defer rows.Close()
	var assets []exportAsset
	var files []exportFile
	for rows.Next() {
		var (
			idx     int
			mime    string
			content []byte
			desc    string
		)
		if err := rows.Scan(&idx, &mime, &content, &desc); err != nil {
			return nil, nil, fmt.Errorf("scan page audio: %w", err)
		}
		name := fmt.Sprintf("p%d_aud%d%s", pageIdx, idx, extForMime(mime))
		files = append(files, exportFile{name: name, bytes: content})
		assets = append(assets, exportAsset{
			Idx: idx, File: name, MimeType: mime, Description: desc,
		})
	}
	return assets, files, rows.Err()
}

func (h *Handlers) exportHintImages(ctx context.Context, hintID int64, pageIdx, hintIdx int) ([]exportAsset, []exportFile, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT idx, mime_type, content, COALESCE(alt_text, '')
		FROM adventure_page_hint_images WHERE hint_id = $1 ORDER BY idx
	`, hintID)
	if err != nil {
		return nil, nil, fmt.Errorf("load hint images: %w", err)
	}
	defer rows.Close()
	var assets []exportAsset
	var files []exportFile
	for rows.Next() {
		var (
			idx     int
			mime    string
			content []byte
			alt     string
		)
		if err := rows.Scan(&idx, &mime, &content, &alt); err != nil {
			return nil, nil, fmt.Errorf("scan hint image: %w", err)
		}
		name := fmt.Sprintf("p%d_h%d_img%d%s", pageIdx, hintIdx, idx, extForMime(mime))
		files = append(files, exportFile{name: name, bytes: content})
		assets = append(assets, exportAsset{
			Idx: idx, File: name, MimeType: mime, AltText: alt,
		})
	}
	return assets, files, rows.Err()
}

func (h *Handlers) exportHintAudio(ctx context.Context, hintID int64, pageIdx, hintIdx int) ([]exportAsset, []exportFile, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT idx, mime_type, content, COALESCE(description, '')
		FROM adventure_page_hint_audio WHERE hint_id = $1 ORDER BY idx
	`, hintID)
	if err != nil {
		return nil, nil, fmt.Errorf("load hint audio: %w", err)
	}
	defer rows.Close()
	var assets []exportAsset
	var files []exportFile
	for rows.Next() {
		var (
			idx     int
			mime    string
			content []byte
			desc    string
		)
		if err := rows.Scan(&idx, &mime, &content, &desc); err != nil {
			return nil, nil, fmt.Errorf("scan hint audio: %w", err)
		}
		name := fmt.Sprintf("p%d_h%d_aud%d%s", pageIdx, hintIdx, idx, extForMime(mime))
		files = append(files, exportFile{name: name, bytes: content})
		assets = append(assets, exportAsset{
			Idx: idx, File: name, MimeType: mime, Description: desc,
		})
	}
	return assets, files, rows.Err()
}

/* ---------------- Import ---------------- */

// ImportAdventure accepts a multipart form upload of a previously
// exported zip and creates a new adventure under the caller's org.
// The caller must be admin or org_admin (anyone with canAuthor on
// the destination org). Slug is taken from the manifest; ?slug=
// query-string overrides for collision avoidance.
//
// POST /api/admin/adventures/import
func (h *Handlers) ImportAdventure(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxImportZipBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxImportZipBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "archive too large")
		return
	}
	// Buffer the upload — archive/zip needs ReaderAt + Size.
	data, err := io.ReadAll(io.LimitReader(f, maxImportZipBytes+1))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "read failed", err)
		return
	}
	if int64(len(data)) > maxImportZipBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "archive too large")
		return
	}
	manifestJSON, assets, err := splitImportZip(data)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	me, _ := auth.FromContext(r.Context())
	advID, slug, title, err := importEnvelopeJSON(r.Context(), h.DB, manifestJSON, r.URL.Query().Get("slug"), me, assets, false)
	if err != nil {
		// Validation failures and missing-asset references read as
		// "the client sent a bad archive" — 400 carries that. Any
		// other error is a server-side failure (DB hiccup, tx
		// commit) and reads as 500.
		var ie *importError
		if errors.As(err, &ie) {
			httpx.Error(w, ie.status, ie.Error())
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "import failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"slug":  slug,
		"id":    advID,
		"title": title,
	})
}

// ImportEnvelopeJSON imports an adventure from a parsed JSON manifest
// (the bytes that would otherwise sit at adventure.json inside a
// .zvadv.zip export). Used by callers that produce structured
// adventures programmatically — most notably the MCP server, where
// an LLM generates the manifest and binary assets get attached
// later through the arranger UI.
//
// overrideSlug, when non-empty, replaces the envelope's own slug
// (mirrors the multipart handler's ?slug= collision-avoidance
// path). Pass "" to use whatever the manifest carries.
//
// Asset references in the manifest (page images, hint audio,
// avatar) cannot be satisfied without a zip — page / hint asset
// references hard-fail the import; an avatar reference is silently
// skipped, matching the multipart handler's behaviour for an
// otherwise valid zip whose avatar entry is missing.
//
// Returns the new adventures.id, the slug it was written under,
// and the title from the manifest. On validation failure (bad
// slug, missing format tag, asset-missing) the error is an
// *importError carrying the appropriate HTTP status; the
// multipart handler maps that back to a 400, callers outside HTTP
// land can ignore the status and treat any error as fatal.
func ImportEnvelopeJSON(ctx context.Context, db *sql.DB, manifestJSON []byte, overrideSlug string, user auth.User) (advID int64, slug, title string, err error) {
	return importEnvelopeJSON(ctx, db, manifestJSON, overrideSlug, user, nil, false)
}

// ValidateEnvelopeJSON runs the full import pipeline against a
// throwaway transaction and rolls back instead of committing. Used
// by the MCP validate_adventure tool so the LLM can catch slug
// collisions, language-tag typos, lane-mode invalid values, and
// every other constraint the importer enforces — without leaving
// a half-baked adventure behind on each retry.
//
// Returns the same error shape ImportEnvelopeJSON does (an
// *importError with the corresponding HTTP status, or a wrapped
// DB error). nil means "this manifest would import cleanly".
func ValidateEnvelopeJSON(ctx context.Context, db *sql.DB, manifestJSON []byte, overrideSlug string, user auth.User) error {
	_, _, _, err := importEnvelopeJSON(ctx, db, manifestJSON, overrideSlug, user, nil, true)
	return err
}

// ExportAdventureEnvelopeJSON returns the manifest JSON for the
// named adventure — same shape as adventure.json inside a
// .zvadv.zip export, ready to be fed back into ImportEnvelopeJSON.
// The binary asset blobs are NOT included (the model can't
// usefully consume them, and round-tripping megabytes of image
// content through every MCP read would be wasteful).
//
// Used by the MCP get_adventure tool. Caller is responsible for
// authorizing the slug first (AuthorizeAdventureRead).
func ExportAdventureEnvelopeJSON(ctx context.Context, db *sql.DB, advID int64) ([]byte, error) {
	h := &Handlers{DB: db}
	env, _, err := h.buildExport(ctx, advID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(env)
}

// importError carries an HTTP status alongside the message so the
// multipart handler can preserve its current 400 / 409 / 500
// distinctions while sharing the import core with non-HTTP callers.
type importError struct {
	status int
	msg    string
}

func (e *importError) Error() string { return e.msg }

func badRequestf(format string, a ...any) *importError {
	return &importError{status: http.StatusBadRequest, msg: fmt.Sprintf(format, a...)}
}

func forbiddenf(format string, a ...any) *importError {
	return &importError{status: http.StatusForbidden, msg: fmt.Sprintf(format, a...)}
}

func conflictf(format string, a ...any) *importError {
	return &importError{status: http.StatusConflict, msg: fmt.Sprintf(format, a...)}
}

// splitImportZip walks the buffered upload, validates the entries
// have flat (non-traversing) names, and separates the manifest
// from the asset blobs. Asset bytes are pre-read into memory so
// the importer doesn't hold the *zip.Reader open across the
// transaction — matters for callers that pass the assets map
// through goroutines or layer further.
func splitImportZip(data []byte) (manifestJSON []byte, assets map[string][]byte, err error) {
	zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if zerr != nil {
		return nil, nil, badRequestf("not a valid zip archive")
	}
	assets = map[string][]byte{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		// Reject path traversal — zip names can carry "../" segments
		// that would point at arbitrary locations on extraction. We
		// don't extract to disk, but the same name lands as a map
		// key the manifest references, so a crafted name could
		// shadow legitimate files. Reject anything that looks
		// non-flat.
		if strings.ContainsAny(zf.Name, `\`) || strings.Contains(zf.Name, "..") || strings.HasPrefix(zf.Name, "/") {
			return nil, nil, badRequestf("archive contains a non-flat path")
		}
		b, rerr := readZipFile(zf, maxImportZipBytes)
		if rerr != nil {
			return nil, nil, badRequestf("read %s: %v", zf.Name, rerr)
		}
		if zf.Name == "adventure.json" {
			manifestJSON = b
		} else {
			assets[zf.Name] = b
		}
	}
	if manifestJSON == nil {
		return nil, nil, badRequestf("missing adventure.json in archive")
	}
	return manifestJSON, assets, nil
}

// importEnvelopeJSON is the shared core. The exported
// ImportEnvelopeJSON wraps the assets-less call (MCP); the
// multipart handler passes the zip-derived asset map.
//
// dryRun, when true, skips the final Commit so the deferred
// Rollback wipes every insert. Used by ValidateEnvelopeJSON to
// surface validation failures (slug uniqueness, bad language tag,
// missing lane refs) without persisting the partial work — same
// code path, just no commit.
func importEnvelopeJSON(ctx context.Context, db *sql.DB, manifestJSON []byte, overrideSlug string, user auth.User, assets map[string][]byte, dryRun bool) (int64, string, string, error) {
	dec := json.NewDecoder(bytes.NewReader(manifestJSON))
	dec.DisallowUnknownFields()
	var env exportEnvelope
	if err := dec.Decode(&env); err != nil {
		return 0, "", "", badRequestf("manifest parse: %s", err.Error())
	}
	if env.Format != adventureExportFormat {
		return 0, "", "", badRequestf("unknown export format: %s", env.Format)
	}
	slug := strings.TrimSpace(overrideSlug)
	if slug == "" {
		slug = strings.TrimSpace(env.Adventure.Slug)
	}
	if !validSlug(slug) {
		return 0, "", "", badRequestf("slug may only contain a-z 0-9 and hyphens")
	}
	if _, err := validateName(env.Adventure.Title, MaxNameLen); err != nil {
		return 0, "", "", badRequestf("title %s", err.Error())
	}
	org := callerOrgID(user)
	if !canAuthor(user, org) {
		return 0, "", "", forbiddenf("forbidden")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", "", fmt.Errorf("tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	advID, err := importAdventureRow(ctx, tx, slug, env.Adventure, org, user.ID)
	if err != nil {
		return 0, "", "", conflictf("import: %s", err.Error())
	}
	if env.Adventure.Avatar != nil && env.Adventure.Avatar.File != "" {
		if b, ok := assets[env.Adventure.Avatar.File]; ok {
			if _, err := tx.ExecContext(ctx,
				`UPDATE adventures SET avatar_mime_type = $1, avatar_content = $2 WHERE id = $3`,
				env.Adventure.Avatar.MimeType, b, advID); err != nil {
				return 0, "", "", fmt.Errorf("write avatar: %w", err)
			}
		}
	}
	laneIDByIdx, err := importLanes(ctx, tx, advID, env.Adventure.Lanes)
	if err != nil {
		return 0, "", "", fmt.Errorf("import lanes: %w", err)
	}
	if err := importPages(ctx, tx, advID, env.Adventure.Pages, laneIDByIdx, assets); err != nil {
		return 0, "", "", badRequestf("import pages: %s", err.Error())
	}
	if env.Graph != nil {
		if err := importGraph(ctx, tx, advID, user.ID, env.Graph, env.GraphMedia, assets); err != nil {
			return 0, "", "", err
		}
	}
	if dryRun {
		// Deferred Rollback wipes the tx; ids/slug/title returned
		// here describe what WOULD have landed and are useful to
		// the caller for logging. Validate callers that don't care
		// about the values just discard them.
		return advID, slug, env.Adventure.Title, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, "", "", fmt.Errorf("commit: %w", err)
	}
	return advID, slug, env.Adventure.Title, nil
}

// importAdventureRow inserts the adventures row and returns the id.
// Slug uniqueness violations bubble up as a generic conflict — the
// caller can retry with ?slug=<other>.
func importAdventureRow(ctx context.Context, tx *sql.Tx, slug string, adv exportAdventureDTO, org sql.NullInt64, userID int64) (int64, error) {
	var orgArg any
	if org.Valid {
		orgArg = org.Int64
	}
	lang := strings.TrimSpace(adv.Language)
	if lang == "" {
		lang = "en"
	}
	if !validLanguageTag(lang) {
		return 0, errors.New("invalid language tag")
	}
	var startLat, startLng any
	if adv.StartLat != nil {
		startLat = *adv.StartLat
	}
	if adv.StartLng != nil {
		startLng = *adv.StartLng
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO adventures(slug, title, description, author, intro_text, outro_text,
		                       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
		                       language, strict_assets,
		                       start_lat, start_lng, start_label,
		                       org_id, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id
	`, slug, adv.Title, adv.Description, adv.Author, adv.IntroText, adv.OutroText,
		nullIfEmpty(adv.AccentColor), nullIfEmpty(adv.AccentSecondaryColor),
		nullIfEmpty(adv.BackgroundColor), nullIfEmpty(adv.TextColor), nullIfEmpty(adv.SubmitBgColor),
		lang, adv.StrictAssets,
		startLat, startLng, adv.StartLabel,
		orgArg, userID).Scan(&id)
	return id, err
}

func importLanes(ctx context.Context, tx *sql.Tx, advID int64, lanes []exportLane) (map[int]int64, error) {
	out := map[int]int64{}
	for _, ln := range lanes {
		mode := ln.Mode
		if mode != "linear" && mode != "shuffled" {
			mode = "linear"
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_lanes (adventure_id, idx, mode, name)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, advID, ln.Idx, mode, ln.Name).Scan(&id); err != nil {
			return nil, fmt.Errorf("insert lane idx=%d: %w", ln.Idx, err)
		}
		out[ln.Idx] = id
	}
	// If the manifest carried no lanes, mint a default linear one
	// at idx 0 so any pages with lane_idx=0 still find a parent.
	if len(out) == 0 {
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_lanes (adventure_id, idx, mode)
			VALUES ($1, 0, 'linear')
			RETURNING id
		`, advID).Scan(&id); err != nil {
			return nil, fmt.Errorf("insert default lane: %w", err)
		}
		out[0] = id
	}
	return out, nil
}

func importPages(ctx context.Context, tx *sql.Tx, advID int64, pages []exportPage, laneIDByIdx map[int]int64, assets map[string][]byte) error {
	for _, p := range pages {
		laneID, ok := laneIDByIdx[p.LaneIdx]
		if !ok {
			// Unknown lane_idx — fall back to lane 0, minting if needed.
			laneID, ok = laneIDByIdx[0]
			if !ok {
				return fmt.Errorf("page idx=%d references unknown lane_idx=%d", p.Idx, p.LaneIdx)
			}
		}
		// `accept` is NOT NULL with default '[]'. The export DTO
		// uses omitempty on the slice, so an empty/nil Accept
		// arrives as nil here and `json.Marshal(nil)` would emit
		// "null" — landing as the string "null" in the column,
		// which the loader can't parse. Force the empty case to
		// the canonical empty array literal.
		var acceptJ []byte
		if len(p.Accept) == 0 {
			acceptJ = []byte("[]")
		} else {
			var err error
			acceptJ, err = json.Marshal(p.Accept)
			if err != nil {
				return fmt.Errorf("marshal accept: %w", err)
			}
		}
		var radius any
		if p.RadiusM != nil {
			radius = *p.RadiusM
		}
		disabled := 0
		if p.Disabled {
			disabled = 1
		}
		var pageID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, lane_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id
		`, advID, p.Idx, p.Title, p.Prompt, string(acceptJ), p.Lat, p.Lng, radius, disabled, p.Notes, laneID).Scan(&pageID); err != nil {
			return fmt.Errorf("insert page idx=%d: %w", p.Idx, err)
		}
		// Page images.
		for _, img := range p.Images {
			if err := importImageRow(ctx, tx, "adventure_page_images", "page_id", pageID, img, assets); err != nil {
				return err
			}
		}
		// Page audio.
		for _, au := range p.Audio {
			if err := importAudioRow(ctx, tx, "adventure_page_audio", "page_id", pageID, au, assets); err != nil {
				return err
			}
		}
		// Hints + their images / audio.
		for _, hint := range p.Hints {
			var hintID int64
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
				VALUES ($1, $2, $3, $4)
				RETURNING id
			`, pageID, hint.Idx, hint.Text, hint.DelaySeconds).Scan(&hintID); err != nil {
				return fmt.Errorf("insert hint p%d.h%d: %w", p.Idx, hint.Idx, err)
			}
			for _, img := range hint.Images {
				if err := importImageRow(ctx, tx, "adventure_page_hint_images", "hint_id", hintID, img, assets); err != nil {
					return err
				}
			}
			for _, au := range hint.Audio {
				if err := importAudioRow(ctx, tx, "adventure_page_hint_audio", "hint_id", hintID, au, assets); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// importGraph stores the envelope's media as the new adventure's, points
// the graph at them, and saves it as the draft and publishes it. A graph
// that does not validate — an exported draft may not — stays a draft.
func importGraph(ctx context.Context, tx *sql.Tx, advID, userID int64, env *graph.Envelope, media []exportGraphMedia, assets map[string][]byte) error {
	if env.Format != graph.FormatV2 {
		return badRequestf("unknown graph format: %s", env.Format)
	}
	g := &env.Graph
	g.Normalize()
	ids := make(map[int64]int64, len(media))
	for _, m := range media {
		switch m.Kind {
		case graph.MediaImage, graph.MediaAudio, graph.MediaVideo:
		default:
			return badRequestf("graph media %q has unknown kind %q", m.File, m.Kind)
		}
		if !strings.HasPrefix(m.MimeType, string(m.Kind)+"/") {
			return badRequestf("graph media %q: mime type %q is not %s", m.File, m.MimeType, m.Kind)
		}
		b, ok := assets[m.File]
		if !ok {
			return badRequestf("graph media %q missing from archive", m.File)
		}
		id, err := insertGraphMedia(ctx, tx, advID, userID, m.Kind, m.MimeType, b)
		if err != nil {
			return fmt.Errorf("insert graph media %q: %w", m.File, err)
		}
		ids[m.ID] = id
	}
	// Every id must be one the archive supplied: an unmapped one could
	// happen to name a row just inserted, and show the wrong file.
	for _, id := range g.MediaIDs() {
		if _, ok := ids[id]; !ok && id != 0 {
			return badRequestf("graph refers to media %d not in graph_media", id)
		}
	}
	remapGraphMedia(g, ids)
	if _, _, err := graphstore.SaveDraft(ctx, tx, advID, userID, g); err != nil {
		return fmt.Errorf("save graph draft: %w", err)
	}
	var invalid *graphstore.InvalidError
	if _, _, err := graphstore.Publish(ctx, tx, advID, userID, g, ""); err != nil && !errors.As(err, &invalid) {
		return fmt.Errorf("publish graph: %w", err)
	}
	return nil
}

func importImageRow(ctx context.Context, tx *sql.Tx, table, parentCol string, parentID int64, asset exportAsset, assets map[string][]byte) error {
	b, ok := assets[asset.File]
	if !ok {
		return fmt.Errorf("image asset %q missing from archive", asset.File)
	}
	mime := asset.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}
	q := fmt.Sprintf(`INSERT INTO %s(%s, idx, mime_type, size_bytes, content, alt_text)
		VALUES ($1, $2, $3, $4, $5, $6)`, table, parentCol)
	if _, err := tx.ExecContext(ctx, q, parentID, asset.Idx, mime, int64(len(b)), b, asset.AltText); err != nil {
		return fmt.Errorf("insert image %q: %w", asset.File, err)
	}
	return nil
}

func importAudioRow(ctx context.Context, tx *sql.Tx, table, parentCol string, parentID int64, asset exportAsset, assets map[string][]byte) error {
	b, ok := assets[asset.File]
	if !ok {
		return fmt.Errorf("audio asset %q missing from archive", asset.File)
	}
	mime := asset.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}
	q := fmt.Sprintf(`INSERT INTO %s(%s, idx, mime_type, size_bytes, content, description)
		VALUES ($1, $2, $3, $4, $5, $6)`, table, parentCol)
	if _, err := tx.ExecContext(ctx, q, parentID, asset.Idx, mime, int64(len(b)), b, asset.Description); err != nil {
		return fmt.Errorf("insert audio %q: %w", asset.File, err)
	}
	return nil
}

/* ---------------- Helpers ---------------- */

func readZipFile(zf *zip.File, max int64) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, max))
}

func extForMime(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	switch m {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/mp4", "audio/m4a":
		return ".m4a"
	}
	return ""
}

func sanitiseFilename(s string) string {
	out := strings.Builder{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			out.WriteRune(r)
		default:
			out.WriteRune('_')
		}
	}
	if out.Len() == 0 {
		return "adventure"
	}
	return out.String()
}

// noUnused keeps strconv referenced — dropped if a future edit
// brings it back. Detected lint warnings otherwise.
var _ = strconv.Itoa
