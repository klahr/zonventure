package admin

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	mailerpkg "git.sr.ht/~klahr/zv/internal/mailer"
)

type sendTestEmailRequest struct {
	To string `json:"to"`
}

// SendTestEmail dispatches a single canned message via h.Mailer so an
// admin can verify the SMTP config end-to-end without waiting for the
// next signup flow. Admin-only at the route level; the body is fixed
// (no caller-supplied subject/text) so this can't be used to spam
// arbitrary recipients with arbitrary content.
func (h *Handlers) SendTestEmail(w http.ResponseWriter, r *http.Request) {
	var req sendTestEmailRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	to := strings.TrimSpace(req.To)
	if _, err := mail.ParseAddress(to); err != nil {
		httpx.Error(w, http.StatusBadRequest, "to must be a valid email address")
		return
	}

	me, _ := auth.FromContext(r.Context())
	subject := "Zonventure: SMTP test"
	body := "This is a test email from your Zonventure server.\r\n\r\n" +
		"If you can read this, the SMTP configuration is working.\r\n\r\n" +
		"Triggered by " + me.Username + " at " + time.Now().UTC().Format(time.RFC3339) + ".\r\n"

	if err := h.Mailer.Send(r.Context(), mailerpkg.Message{
		To:      []string{to},
		Subject: subject,
		Text:    body,
	}); err != nil {
		logx.Warn("admin.test_email.send_failed", "to", to, "err", err.Error())
		httpx.Error(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	logx.Info("admin.test_email.sent", "to", to, "by", me.Username)
	httpx.JSON(w, http.StatusOK, map[string]string{"to": to, "status": "sent"})
}
