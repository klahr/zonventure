package admin

import (
	"net/http"
	"strconv"

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
	httpx.JSON(w, http.StatusOK, map[string]any{
		"level":   logx.GetLevel().String(),
		"entries": entries,
	})
}

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
			// Entry's MarshalJSON builds the friendly shape the UI
			// expects; SendKind handles the {kind,data} envelope.
			conn.SendKind("entry", e)
		}
	}
}
