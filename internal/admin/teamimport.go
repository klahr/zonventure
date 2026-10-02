package admin

import (
	"bytes"
	"io"
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/teamimport"
)

/* ---------------- Team-roster file import (preview) ---------------- */
//
// Two-step flow on purpose:
//
//   1. POST /events/{id}/teams/import — multipart upload. The
//      registry picks an Importer by filename + MIME, parses the
//      file, returns a preview JSON. No DB writes.
//
//   2. The arranger inspects the preview, edits if needed, then
//      POSTs the same shape to /events/{id}/teams/bulk — the
//      pre-existing endpoint that mints the teams + slots,
//      enforces UNIQUE(event_id, name), records audit rows, etc.
//
// The split keeps validation / dedup / audit in one place
// (BulkCreateTeams) and the file-format glue in another (this
// file plus the per-format subpackages). New formats drop in by
// registering their Importer in cmd/server/main.go; nothing in
// this file needs to change.

// teamImportPreview is the shape returned to the client. Format
// is the human-readable importer name so the UI can echo
// "Recognised as Excel (.xlsx) — 4 teams" without us having to
// duplicate the Name() table on the front-end.
type teamImportPreview struct {
	Format string            `json:"format"`
	Teams  []teamimport.Team `json:"teams"`
}

// ImportTeams accepts an uploaded roster file, dispatches to the
// matching format importer, and returns the parsed teams as a
// preview. Permission gates on OpCreateTeam — the bulk endpoint
// the client follows up with does the same check, but a 403 here
// stops a wasted multipart upload before bytes are even buffered.
func (h *Handlers) ImportTeams(w http.ResponseWriter, r *http.Request) {
	if h.TeamImporters == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "no team-import formats registered")
		return
	}
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if !h.requireEventOp(w, r.Context(), eventID, OpCreateTeam) {
		return
	}
	// Same upload budget as the page-image flow — 5 MiB by default,
	// overridable via ZV_MAX_IMAGE_BYTES. Roster files in this
	// product are tens of KB; the cap is mostly a safety net
	// against a hostile or accidental gigabyte upload.
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(1<<20))
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	// io.LimitReader + the post-read length check is the same belt-
	// and-braces pattern AddPageImage uses: hdr.Size is advisory
	// (the client can lie), the LimitReader caps the actual byte
	// count, and the >maxImageBytes check catches the "exactly at
	// the limit + 1 byte" boundary case.
	body, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(body)) > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	imp, err := h.TeamImporters.Resolve(hdr.Filename, mime)
	if err != nil {
		// 415 — we know the request shape, we just don't support
		// the format. Keeping the message specific helps the UI
		// render a useful "supported formats" hint.
		httpx.Error(w, http.StatusUnsupportedMediaType,
			"unsupported file format (extension or MIME)")
		return
	}
	teams, err := imp.Parse(bytes.NewReader(body))
	if err != nil {
		// Parse failures are user-visible — the file came from
		// their machine and they need to know what's wrong with
		// it. Surface the underlying error verbatim; it's a
		// small attack surface (a string from encoding/csv or
		// encoding/xml) and informative.
		httpx.Error(w, http.StatusBadRequest, "could not parse file: "+err.Error())
		return
	}
	if len(teams) == 0 {
		httpx.Error(w, http.StatusBadRequest, "no teams found in file")
		return
	}
	httpx.JSON(w, http.StatusOK, teamImportPreview{
		Format: imp.Name(),
		Teams:  teams,
	})
}
