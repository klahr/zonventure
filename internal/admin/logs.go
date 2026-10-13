package admin

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// LogsLevel returns the current log level and the available names so
// the UI can render its dropdown without hardcoding them.
func (h *Handlers) LogsLevel(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"level":  logx.GetLevel().String(),
		"levels": []string{"DEBUG", "INFO", "WARN", "ERROR"},
	})
}

// SetLogsLevel moves the threshold. Request body: {"level": "INFO"}.
func (h *Handlers) SetLogsLevel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	logx.SetLevel(logx.ParseLevel(req.Level))
	// Record the change through the log itself so operators can see it
	// in the stream — and so it's reproducible from the transcript.
	logx.Info("log.level.changed", "level", logx.GetLevel().String())
	noteAction(r, logx.GetLevel().String())
	httpx.JSON(w, http.StatusOK, map[string]any{"level": logx.GetLevel().String()})
}

// ListLogs returns the buffered entries since (optional) ?since=<seq>.
// The UI uses this for the initial backfill and after a WebSocket
// reconnect to catch up on anything that dropped.
func (h *Handlers) ListLogs(w http.ResponseWriter, r *http.Request) {
	var minSeq uint64
	if q := r.URL.Query().Get("since"); q != "" {
		if n, err := strconv.ParseUint(q, 10, 64); err == nil {
			minSeq = n
		}
	}
	entries := logx.Snapshot(minSeq)
	if hidesMailBodies(r) {
		entries = slices.DeleteFunc(entries, isMailBody)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"level":   logx.GetLevel().String(),
		"entries": entries,
	})
}

// hidesMailBodies is true for Support: with no SMTP the log mailer
// writes every mail in full (mail.body), sign-in and reset links
// included, which only a full admin may read.
func hidesMailBodies(r *http.Request) bool {
	me, _ := auth.FromContext(r.Context())
	return !isAdmin(me)
}

func isMailBody(e logx.Entry) bool { return e.Msg == "mail.body" }

// WSLogs streams new log entries in real time. Each push is a single
// Entry JSON. No per-event topic: there's one global log stream.
func (h *Handlers) WSLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.Accept(w, r)
	if err != nil {
		return
	}
	defer conn.Close()
	sub, cancel := logx.Subscribe()
	defer cancel()
	hide := hidesMailBodies(r)

	// Send a tiny hello so the client knows the socket is live even
	// on a quiet log stream.
	conn.SendKind("ready", map[string]any{"level": logx.GetLevel().String()})

	for {
		select {
		case <-conn.Context().Done():
			return
		case e, ok := <-sub:
			if !ok {
				return
			}
			if hide && isMailBody(e) {
				continue
			}
			// Entry's MarshalJSON builds the friendly shape the UI
			// expects; SendKind handles the {kind,data} envelope.
			conn.SendKind("entry", e)
		}
	}
}
