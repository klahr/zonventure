package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"git.sr.ht/~klahr/zv/internal/auth"
)

// uploadFile builds a multipart/form-data request with a single
// "file" field and POSTs it through the test client's cookie jar.
// Mirrors the shape AddPageImage / SetTeamAvatar tests use, just
// scoped down to the field this handler reads.
func uploadFile(t *testing.T, c *client, path, filename, contentType string, body []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{
		`form-data; name="file"; filename="` + filename + `"`,
	}
	if contentType != "" {
		hdr["Content-Type"] = []string{contentType}
	}
	w, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	req, err := http.NewRequest("POST", c.ts.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if tok := c.csrfCookie(); tok != "" {
		req.Header.Set(auth.CSRFHeaderName, tok)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}

// TestImportTeams_CSVPreviewParses round-trips a CSV body through
// the import handler and asserts the preview shape is what the
// front-end expects: a "format" string + the parsed teams.
func TestImportTeams_CSVPreviewParses(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, _, eventID := setupRun(t, ts, assignmentID)

	body := []byte("Alpha,alice,bob\nBeta,carol,dave,ezra\n")
	status, raw := uploadFile(t, arr,
		"/api/admin/events/"+itoa(eventID)+"/teams/import",
		"roster.csv", "text/csv", body)
	if status != 200 {
		t.Fatalf("import = %d, body=%s", status, raw)
	}
	var preview struct {
		Format string `json:"format"`
		Teams  []struct {
			Name        string   `json:"name"`
			MemberNames []string `json:"member_names"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatalf("unmarshal preview: %v", err)
	}
	if preview.Format != "CSV" {
		t.Errorf("Format = %q, want CSV", preview.Format)
	}
	if len(preview.Teams) != 2 {
		t.Fatalf("teams = %+v", preview.Teams)
	}
	if preview.Teams[0].Name != "Alpha" || len(preview.Teams[0].MemberNames) != 2 {
		t.Errorf("Alpha = %+v", preview.Teams[0])
	}
	if preview.Teams[1].Name != "Beta" || len(preview.Teams[1].MemberNames) != 3 {
		t.Errorf("Beta = %+v", preview.Teams[1])
	}
}

// TestImportTeams_RejectsUnknownFormat confirms the handler 415s
// (rather than 5xx) when neither the filename nor the MIME maps
// to a registered importer. The UI uses the 415 to surface a
// clear "we don't know this format" message.
func TestImportTeams_RejectsUnknownFormat(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, _, eventID := setupRun(t, ts, assignmentID)

	status, _ := uploadFile(t, arr,
		"/api/admin/events/"+itoa(eventID)+"/teams/import",
		"roster.lol", "application/octet-stream", []byte("nonsense"))
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", status)
	}
}

// TestImportTeams_IOFXMLParses round-trips an IOF XML EntryList
// payload through the handler. Shorter sample than the package
// test — the integration concern here is that the registry picks
// the .xml file up and dispatches to iofxml, not the parser
// internals.
func TestImportTeams_IOFXMLParses(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, _, eventID := setupRun(t, ts, assignmentID)

	body := []byte(`<EntryList>
  <TeamEntry>
    <Name>Alpha</Name>
    <TeamEntryPerson><Person><Name><Given>A</Given><Family>B</Family></Name></Person></TeamEntryPerson>
  </TeamEntry>
</EntryList>`)
	status, raw := uploadFile(t, arr,
		"/api/admin/events/"+itoa(eventID)+"/teams/import",
		"entries.xml", "application/xml", body)
	if status != 200 {
		t.Fatalf("import = %d, body=%s", status, raw)
	}
	var preview struct {
		Format string `json:"format"`
		Teams  []struct {
			Name        string   `json:"name"`
			MemberNames []string `json:"member_names"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if preview.Format != "IOF XML" {
		t.Errorf("Format = %q, want IOF XML", preview.Format)
	}
	if len(preview.Teams) != 1 || preview.Teams[0].Name != "Alpha" {
		t.Errorf("teams = %+v", preview.Teams)
	}
}
