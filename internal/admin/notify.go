package admin

import (
	"context"
	"database/sql"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/logx"
)

// notify emails an author about work on an adventure — a mention, a
// step given to them, a suggestion, a review asked for — in the
// adventure's organisation's look. Nobody is told what they did
// themselves (their assistant's doings are news to them), nor what
// they turned off in their email preferences. Best effort: the work is
// done either way.
func (h *Handlers) notify(ctx context.Context, advID int64, me auth.User, viaAssistant bool, uid int64, kind, node string,
	build func(lang, who string, w emailtpl.Work) (string, emailtpl.Content)) {
	if uid == 0 || (uid == me.ID && !viaAssistant) || !h.wantsEmail(ctx, uid, kind) {
		return
	}
	var (
		email, slug, title string
		org                sql.NullInt64
		step, lang         sql.NullString
	)
	// In the language they use the console in, else their organisation's.
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(u.email, ''), a.slug, a.title, a.org_id, COALESCE(u.language, o.language),
		       (SELECT n->>'title' FROM adventure_graph_drafts d, jsonb_array_elements(d.graph->'nodes') n
		         WHERE d.adventure_id = a.id AND n->>'slug' = $3 LIMIT 1)
		FROM users u, adventures a LEFT JOIN organizations o ON o.id = a.org_id WHERE u.id = $1 AND a.id = $2
	`, uid, advID, node).Scan(&email, &slug, &title, &org, &lang, &step)
	if err != nil || email == "" {
		return
	}
	l := h.orgLook(ctx, org.Int64)
	l.Lang = emailtpl.Lang(lang.String)
	who := me.Username
	if viaAssistant {
		who = emailtpl.Assistant(l.Lang, who)
	}
	w := emailtpl.Work{Adventure: title, Step: node}
	if step.Valid && strings.TrimSpace(step.String) != "" {
		w.Step = step.String
	}
	if base := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/"); base != "" {
		w.Link = base + "/app/arrange/#/adventures/" + slug + "/design"
		w.SettingsLink = base + "/app/arrange/#/settings/account"
	}
	subject, c := build(l.Lang, who, w)
	if w.Link == "" {
		c.Buttons = nil
	}
	if err := h.send(ctx, "adventure_"+kind, l.message([]string{email}, subject, c)); err != nil {
		logx.Warn("admin.notify", "kind", kind, "adventure_id", advID, "user_id", uid, "err", err.Error())
	}
}

// stepOwner is who owns a step, or 0.
func (h *Handlers) stepOwner(ctx context.Context, advID int64, node string) int64 {
	var owner sql.NullInt64
	_ = h.DB.QueryRowContext(ctx, `SELECT owner_id FROM adventure_step_status WHERE adventure_id = $1 AND node = $2`, advID, node).Scan(&owner)
	return owner.Int64
}
