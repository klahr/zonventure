package admin

import (
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	mailerpkg "git.sr.ht/~klahr/zv/internal/mailer"
)

type sendTestEmailRequest struct {
	To string `json:"to"`
}

// testEmailLimits caps how often /api/admin/send-test-email may fire
// per-(admin user) and per-IP. Without these a compromised admin
// session could loop the endpoint to spam an arbitrary recipient
// (the body is fixed, but the recipient is caller-supplied), and the
// 200/502 distinguishability would also expose an SMTP-deliverability
// oracle to whoever held the session.
//
// The values are deliberately generous so a real operator iterating
// on SMTP config doesn't get throttled mid-debug. They exist to put a
// ceiling on abuse, not to slow down day-one setup.
const (
	testEmailWindow      = time.Hour
	testEmailPerUserMax  = 20
	testEmailPerIPMax    = 40
)

type testEmailLimiter struct {
	mu      sync.Mutex
	perUser map[int64][]time.Time
	perIP   map[string][]time.Time
}

var testEmailLimit = testEmailLimiter{
	perUser: map[int64][]time.Time{},
	perIP:   map[string][]time.Time{},
}

// allow returns (true, 0) when the call may proceed. When false, the
// returned duration is the wait the caller should surface as
// Retry-After. A successful call appends a timestamp to both buckets;
// failures (bad address, mailer error) are not counted so a noisy
// debug session doesn't burn the operator's budget on misfires.
func (l *testEmailLimiter) allow(now time.Time, userID int64, ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-testEmailWindow)
	uts := trimBefore(l.perUser[userID], cutoff)
	its := trimBefore(l.perIP[ip], cutoff)
	l.perUser[userID] = uts
	l.perIP[ip] = its
	if len(uts) >= testEmailPerUserMax {
		return false, ceilTestEmailWait(uts[0].Add(testEmailWindow).Sub(now))
	}
	if len(its) >= testEmailPerIPMax {
		return false, ceilTestEmailWait(its[0].Add(testEmailWindow).Sub(now))
	}
	return true, 0
}

// record stamps a successful send. Held separately from allow() so a
// failed Mailer.Send doesn't burn against the cap.
func (l *testEmailLimiter) record(now time.Time, userID int64, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.perUser[userID] = append(l.perUser[userID], now)
	l.perIP[ip] = append(l.perIP[ip], now)
}

// sweep drops empty entries from both maps so a long-lived process
// doesn't accumulate an entry per (admin user, IP) ever seen.
// Entries that still hold timestamps inside the rolling window are
// kept; everything else is removed. Cheap O(N) walk.
func (l *testEmailLimiter) sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-testEmailWindow)
	for k, ts := range l.perUser {
		ts = trimBefore(ts, cutoff)
		if len(ts) == 0 {
			delete(l.perUser, k)
		} else {
			l.perUser[k] = ts
		}
	}
	for k, ts := range l.perIP {
		ts = trimBefore(ts, cutoff)
		if len(ts) == 0 {
			delete(l.perIP, k)
		} else {
			l.perIP[k] = ts
		}
	}
}

func trimBefore(in []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(in) && in[i].Before(cutoff) {
		i++
	}
	if i == 0 {
		return in
	}
	return append(in[:0], in[i:]...)
}

func ceilTestEmailWait(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Second
	}
	if r := d % time.Second; r != 0 {
		d += time.Second - r
	}
	return d
}

// SendTestEmail dispatches a single canned message via h.Mailer so an
// admin can verify the SMTP config end-to-end without waiting for the
// next signup flow. Admin-only at the route level; the body is fixed
// (no caller-supplied subject/text) so this can't be used to spam
// arbitrary recipients with arbitrary content.
//
// Rate-limited per-(admin user) and per-IP — see testEmailLimiter.
// The counter increments only on successful sends, so an operator
// chasing an SMTP misconfig isn't penalised for it.
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
	ip := auth.ClientIP(r)
	if ok, wait := testEmailLimit.allow(time.Now(), me.ID, ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
		httpx.Error(w, http.StatusTooManyRequests, "too many test emails — try again later")
		return
	}

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
		httpx.ServerErr(w, http.StatusBadGateway, "send failed", err)
		return
	}
	testEmailLimit.record(time.Now(), me.ID, ip)
	logx.Info("admin.test_email.sent", "to", to, "by", me.Username)
	httpx.JSON(w, http.StatusOK, map[string]string{"to": to, "status": "sent"})
}
