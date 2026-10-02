package player

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// What a team shows of itself — the players' names, its name and its
// picture — is the players' to set until the team begins (play.TeamBegun),
// and fixed for them after, so the leaderboard, the organisers' view and
// the recap keep one name and one picture throughout. Organisers can
// still change them from the event page.

// maxTeamAvatarBytes caps a team picture, as the share page and the
// organisers' upload do.
const maxTeamAvatarBytes = 5 * 1024 * 1024

// CodeTeamStarted is the refusal code once the team has begun.
const CodeTeamStarted = "team_started"

// refuseOnceBegun writes the refusal and reports true when the team has
// begun.
func (h *Handlers) refuseOnceBegun(w http.ResponseWriter, r *http.Request, teamID int64) bool {
	begun, err := play.TeamBegun(r.Context(), h.DB, teamID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
		return true
	}
	if begun {
		httpx.JSON(w, http.StatusConflict, map[string]string{
			"error": "names and the team picture are fixed once your team has begun",
			"code":  CodeTeamStarted,
		})
	}
	return begun
}

// teamAvatarURL is where the team's picture is served, or "" without one.
func teamAvatarURL(tc teamContext) string {
	if !tc.HasAvatar {
		return ""
	}
	return fmt.Sprintf("/adventure-assets/team-avatar/%d", tc.TeamID)
}

// SetMyTeamAvatar stores a picture for the calling player's team: a
// multipart `file`, an image, at most 5 MB, shrunk as every upload is.
func (h *Handlers) SetMyTeamAvatar(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	if h.refuseOnceBegun(w, r, slot.TeamID) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTeamAvatarBytes+(1<<20))
	if err := r.ParseMultipartForm(maxTeamAvatarBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTeamAvatarBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxTeamAvatarBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	if !strings.HasPrefix(hdr.Header.Get("Content-Type"), "image/") {
		httpx.Error(w, http.StatusBadRequest, "only image uploads are accepted")
		return
	}
	data, mime, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	if err := h.Storage.PutTeamAvatar(r.Context(), slot.TeamID, data, mime); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving the picture failed", err)
		return
	}
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"url": fmt.Sprintf("/adventure-assets/team-avatar/%d", slot.TeamID)})
}

// DeleteMyTeamAvatar removes the calling player's team picture.
func (h *Handlers) DeleteMyTeamAvatar(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	if h.refuseOnceBegun(w, r, slot.TeamID) {
		return
	}
	if err := h.Storage.DeleteTeamAvatar(r.Context(), slot.TeamID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "removing the picture failed", err)
		return
	}
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}
