package admin

// The author's own position, for building an adventure on site:
//
//	PUT /api/admin/me/position   {lat, lng, accuracy_m}   the phone reports a fix
//	GET /api/admin/me/position                             the last fix, with its age
//
// Only the latest fix is kept. An assistant reads it to place a zone
// where the author stands; nothing else does.

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

type creatorPosition struct {
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	AccuracyM float64   `json:"accuracy_m"`
	At        time.Time `json:"at"`
	// AgeSeconds is how old the fix was when read, so a caller can tell a
	// live position from one left behind when the phone went to sleep.
	AgeSeconds int `json:"age_seconds"`
}

func (h *Handlers) PutMyPosition(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var req struct {
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
		AccuracyM float64 `json:"accuracy_m"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 || req.AccuracyM < 0 {
		httpx.Error(w, http.StatusBadRequest, "position out of range")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO creator_positions(user_id, lat, lng, accuracy_m, at) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id) DO UPDATE SET lat = $2, lng = $3, accuracy_m = $4, at = now()
	`, me.ID, req.Lat, req.Lng, req.AccuracyM); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving position failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) GetMyPosition(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var p creatorPosition
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT lat, lng, accuracy_m, at, EXTRACT(EPOCH FROM now() - at)::int
		FROM creator_positions WHERE user_id = $1
	`, me.ID).Scan(&p.Lat, &p.Lng, &p.AccuracyM, &p.At, &p.AgeSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no position reported yet: open Field mode on the phone")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading position failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}
