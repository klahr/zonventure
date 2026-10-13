package admin

// Working on an adventure together, beside the graph rather than in it:
// conversations on steps (threads of comments, @mentions, resolved when
// done), where each step is in the work (status and owner), and what
// each author last saw, so the designer can say what changed since.
//
//	GET    /api/admin/adventures/{slug}/collab                 threads, steps, people
//	POST   /api/admin/adventures/{slug}/threads                {node, block_id, body, suggestion?}
//	POST   /api/admin/adventures/{slug}/threads/{threadID}/comments  {body}
//	PATCH  /api/admin/adventures/{slug}/threads/{threadID}     {resolved} or {suggestion_state}
//	POST   /api/admin/adventures/{slug}/reactions               {target: "comment:<id>", emoji}: toggles the caller's
//	PATCH  /api/admin/adventures/{slug}/comments/{commentID}   {body}, its author only
//	DELETE /api/admin/adventures/{slug}/comments/{commentID}   its author only
//	PUT    /api/admin/adventures/{slug}/steps/{node}/status    {status?, owner_id?}
//	GET    /api/admin/adventures/{slug}/seen                   the draft as the caller last saw it
//	POST   /api/admin/adventures/{slug}/seen                   the caller has seen the draft as it is
//
// Every change publishes collab_changed on the adventure's topic, so the
// designers open on it reload what changed.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/ws"
)

// maxCommentLen bounds one comment: a conversation, not a document.
const maxCommentLen = 4000

// AssistantMention is what @-mentions the assistant: such a thread is a
// task for it until resolved.
const AssistantMention = "assistant"

var stepStatuses = map[string]bool{"": true, "idea": true, "draft": true, "review": true, "ready": true}

type userRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

type commentDTO struct {
	ID        int64      `json:"id"`
	Author    *userRef   `json:"author,omitempty"`
	Assistant bool       `json:"assistant"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	EditedAt  *time.Time `json:"edited_at,omitempty"`
	Mentions  []int64    `json:"mentions"`
}

type threadDTO struct {
	ID           int64        `json:"id"`
	Node         string       `json:"node"`
	BlockID      string       `json:"block_id,omitempty"`
	CreatedBy    *userRef     `json:"created_by,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	ResolvedAt   *time.Time   `json:"resolved_at,omitempty"`
	ResolvedBy   *userRef     `json:"resolved_by,omitempty"`
	ForAssistant bool         `json:"for_assistant"`
	Comments     []commentDTO `json:"comments"`
	// Suggestion is {base, proposed}: the step as the suggester found it
	// and as they would have it. SuggestionState is open, accepted or
	// rejected; both are empty for a plain conversation.
	Suggestion      json.RawMessage `json:"suggestion,omitempty"`
	SuggestionState string          `json:"suggestion_state,omitempty"`
}

type reactionDTO struct {
	Target string  `json:"target"`
	Emoji  string  `json:"emoji"`
	Users  []int64 `json:"users"`
}

type stepStatusDTO struct {
	Node      string    `json:"node"`
	Status    string    `json:"status"`
	Owner     *userRef  `json:"owner,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type collabDTO struct {
	Threads []threadDTO     `json:"threads"`
	Steps   []stepStatusDTO `json:"steps"`
	// People are who can be mentioned and given a step: the authors of
	// the adventure's organisation.
	People    []userRef     `json:"people"`
	Reactions []reactionDTO `json:"reactions"`
}

// Reactions are a small, fixed set: a nod, not a vocabulary.
var reactionEmoji = map[string]bool{"👍": true, "❤️": true, "🔥": true, "😕": true, "🎉": true, "👀": true}

// maxSuggestionBytes bounds a suggestion: one step, as before and after.
const maxSuggestionBytes = 256 << 10

// collabTarget authorises the caller to work on the adventure, as the
// graph routes do, and returns its id.
func (h *Handlers) collabTarget(w http.ResponseWriter, r *http.Request) (int64, auth.User, bool) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return 0, auth.User{}, false
	}
	me, _ := auth.FromContext(r.Context())
	return advID, me, true
}

func ref(id sql.NullInt64, name sql.NullString) *userRef {
	if !id.Valid {
		return nil
	}
	return &userRef{ID: id.Int64, Name: name.String}
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// GetCollab returns an adventure's threads, step statuses and people.
func (h *Handlers) GetCollab(w http.ResponseWriter, r *http.Request) {
	advID, _, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	out, err := h.loadCollab(r.Context(), advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the conversation failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handlers) loadCollab(ctx context.Context, advID int64) (collabDTO, error) {
	out := collabDTO{Threads: []threadDTO{}, Steps: []stepStatusDTO{}, People: []userRef{}}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.node, t.block_id, t.created_by, cu.username, t.created_at,
		       t.resolved_at, t.resolved_by, ru.username, t.for_assistant,
		       t.suggestion, t.suggestion_state
		FROM adventure_threads t
		LEFT JOIN users cu ON cu.id = t.created_by
		LEFT JOIN users ru ON ru.id = t.resolved_by
		WHERE t.adventure_id = $1
		ORDER BY t.created_at, t.id
	`, advID)
	if err != nil {
		return out, err
	}
	index := map[int64]int{}
	for rows.Next() {
		var (
			t            threadDTO
			cID, rID     sql.NullInt64
			cName, rName sql.NullString
			resolvedAt   sql.NullTime
			suggestion   []byte
		)
		if err := rows.Scan(&t.ID, &t.Node, &t.BlockID, &cID, &cName, &t.CreatedAt, &resolvedAt, &rID, &rName, &t.ForAssistant, &suggestion, &t.SuggestionState); err != nil {
			rows.Close()
			return out, err
		}
		t.CreatedBy, t.ResolvedBy, t.ResolvedAt = ref(cID, cName), ref(rID, rName), nullTime(resolvedAt)
		if len(suggestion) > 0 {
			t.Suggestion = suggestion
		}
		t.Comments = []commentDTO{}
		index[t.ID] = len(out.Threads)
		out.Threads = append(out.Threads, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	rows, err = h.DB.QueryContext(ctx, `
		SELECT c.id, c.thread_id, c.author_id, u.username, c.assistant, c.body, c.created_at, c.edited_at,
		       COALESCE((SELECT string_agg(m.user_id::text, ',' ORDER BY m.user_id) FROM adventure_mentions m WHERE m.comment_id = c.id), '')
		FROM adventure_comments c
		JOIN adventure_threads t ON t.id = c.thread_id
		LEFT JOIN users u ON u.id = c.author_id
		WHERE t.adventure_id = $1
		ORDER BY c.created_at, c.id
	`, advID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var (
			c        commentDTO
			threadID int64
			aID      sql.NullInt64
			aName    sql.NullString
			edited   sql.NullTime
			mentions string
		)
		if err := rows.Scan(&c.ID, &threadID, &aID, &aName, &c.Assistant, &c.Body, &c.CreatedAt, &edited, &mentions); err != nil {
			rows.Close()
			return out, err
		}
		c.Author, c.EditedAt, c.Mentions = ref(aID, aName), nullTime(edited), []int64{}
		for _, f := range strings.Split(mentions, ",") {
			if id, err := strconv.ParseInt(f, 10, 64); err == nil {
				c.Mentions = append(c.Mentions, id)
			}
		}
		if i, ok := index[threadID]; ok {
			out.Threads[i].Comments = append(out.Threads[i].Comments, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	rows, err = h.DB.QueryContext(ctx, `
		SELECT s.node, s.status, s.owner_id, u.username, s.updated_at
		FROM adventure_step_status s LEFT JOIN users u ON u.id = s.owner_id
		WHERE s.adventure_id = $1
		ORDER BY s.node
	`, advID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var (
			s     stepStatusDTO
			oID   sql.NullInt64
			oName sql.NullString
		)
		if err := rows.Scan(&s.Node, &s.Status, &oID, &oName, &s.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		s.Owner = ref(oID, oName)
		out.Steps = append(out.Steps, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	rows, err = h.DB.QueryContext(ctx, `
		SELECT target, emoji, string_agg(user_id::text, ',' ORDER BY created_at)
		FROM adventure_reactions WHERE adventure_id = $1 AND target LIKE 'comment:%'
		GROUP BY target, emoji ORDER BY target, MIN(created_at)
	`, advID)
	if err != nil {
		return out, err
	}
	out.Reactions = []reactionDTO{}
	for rows.Next() {
		var (
			r     reactionDTO
			users string
		)
		if err := rows.Scan(&r.Target, &r.Emoji, &users); err != nil {
			rows.Close()
			return out, err
		}
		for _, f := range strings.Split(users, ",") {
			if id, err := strconv.ParseInt(f, 10, 64); err == nil {
				r.Users = append(r.Users, id)
			}
		}
		out.Reactions = append(out.Reactions, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	people, err := h.adventurePeople(ctx, advID)
	if err != nil {
		return out, err
	}
	out.People = people

	// Everyone named gets their picture, in one read.
	var refs []*userRef
	for i := range out.Threads {
		th := &out.Threads[i]
		refs = append(refs, th.CreatedBy, th.ResolvedBy)
		for j := range th.Comments {
			refs = append(refs, th.Comments[j].Author)
		}
	}
	for i := range out.Steps {
		refs = append(refs, out.Steps[i].Owner)
	}
	for i := range out.People {
		refs = append(refs, &out.People[i])
	}
	var ids []int64
	for _, r := range refs {
		if r != nil {
			ids = append(ids, r.ID)
		}
	}
	avatars := auth.AvatarURLs(ctx, h.DB, ids)
	for _, r := range refs {
		if r != nil {
			r.Avatar = avatars[r.ID]
		}
	}
	return out, nil
}

// adventurePeople are who may author the adventure: its organisation's
// admins and writers. They are who can be mentioned and given a step.
func (h *Handlers) adventurePeople(ctx context.Context, advID int64) ([]userRef, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT u.id, u.username
		FROM adventures a
		JOIN org_memberships m ON m.org_id = a.org_id AND m.role IN ('org_admin', 'org_writer')
		JOIN users u ON u.id = m.user_id
		WHERE a.id = $1
		ORDER BY LOWER(u.username)
	`, advID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []userRef{}
	for rows.Next() {
		var p userRef
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// mentionRe finds @mentions. A username may itself be an email, so the
// name runs on through dots and at-signs; trailing punctuation is the
// sentence's, not the name's.
var mentionRe = regexp.MustCompile(`(?:^|[^\w@.])@([\w.+@-]+)`)

// mentionsIn returns who of people body mentions, and whether it
// mentions the assistant.
func mentionsIn(body string, people []userRef) (ids []int64, assistant bool) {
	byName := map[string]int64{}
	for _, p := range people {
		byName[strings.ToLower(p.Name)] = p.ID
	}
	seen := map[int64]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		name := strings.ToLower(strings.TrimRight(m[1], ".,;:!?)"))
		if name == AssistantMention {
			assistant = true
			continue
		}
		if id, ok := byName[name]; ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, assistant
}

func readBody(w http.ResponseWriter, body string) (string, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "say something", "code": "comment_empty"})
		return "", false
	}
	if len([]rune(body)) > maxCommentLen {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("a comment is at most %d characters", maxCommentLen), "code": "comment_too_long"})
		return "", false
	}
	return body, true
}

type newThreadRequest struct {
	Node    string `json:"node"`
	BlockID string `json:"block_id"`
	Body    string `json:"body"`
	// Suggestion, when set, makes the thread propose a change to the
	// step: {base, proposed}, each the whole step.
	Suggestion json.RawMessage `json:"suggestion,omitempty"`
}

// readSuggestion checks a suggestion is {base, proposed}, two steps with
// the thread's slug, and that it proposes something.
func readSuggestion(w http.ResponseWriter, raw json.RawMessage, node string) (any, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	refuse := func(msg string) (any, bool) {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": msg, "code": "suggestion_invalid"})
		return nil, false
	}
	if len(raw) > maxSuggestionBytes {
		return refuse("a suggestion is one step; this is too large")
	}
	if node == "" {
		return refuse("a suggestion is about a step")
	}
	var sg struct {
		Base     map[string]json.RawMessage `json:"base"`
		Proposed map[string]json.RawMessage `json:"proposed"`
	}
	if err := json.Unmarshal(raw, &sg); err != nil || sg.Base == nil || sg.Proposed == nil {
		return refuse("a suggestion is {base, proposed}")
	}
	for _, n := range []map[string]json.RawMessage{sg.Base, sg.Proposed} {
		var slug string
		if json.Unmarshal(n["slug"], &slug) != nil || slug != node {
			return refuse("a suggestion is about its thread's step")
		}
	}
	if b, p := canonicalJSON(sg.Base), canonicalJSON(sg.Proposed); b == p {
		return refuse("the suggestion changes nothing")
	}
	return string(raw), true
}

// canonicalJSON re-encodes a decoded object so key order does not count.
func canonicalJSON(m map[string]json.RawMessage) string {
	var v any
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &v)
	out, _ := json.Marshal(v)
	return string(out)
}

// CreateThread starts a conversation on a step, a block of one, or the
// adventure (node "").
func (h *Handlers) CreateThread(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	var req newThreadRequest
	// A suggestion's body (DecodeJSON caps a body at 1 MiB) may be no
	// more than a note; without one it says what it is.
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	suggestion, ok := readSuggestion(w, req.Suggestion, strings.TrimSpace(req.Node))
	if !ok {
		return
	}
	if suggestion != nil && strings.TrimSpace(req.Body) == "" {
		req.Body = "Suggested a change."
	}
	body, ok := readBody(w, req.Body)
	if !ok {
		return
	}
	if len(req.Node) > 200 || len(req.BlockID) > 200 {
		httpx.Error(w, http.StatusBadRequest, "node or block too long")
		return
	}
	viaAssistant := auth.ViaAPIToken(r.Context())
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "starting the thread failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var threadID int64
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO adventure_threads (adventure_id, node, block_id, created_by, suggestion, suggestion_state)
		VALUES ($1, $2, $3, $4, $5::jsonb, CASE WHEN $5::jsonb IS NULL THEN '' ELSE 'open' END) RETURNING id
	`, advID, strings.TrimSpace(req.Node), strings.TrimSpace(req.BlockID), me.ID, suggestion).Scan(&threadID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "starting the thread failed", err)
		return
	}
	commentID, mentioned, err := h.addComment(r.Context(), tx, advID, threadID, me, viaAssistant, body)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "starting the thread failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "starting the thread failed", err)
		return
	}
	kind := "comment"
	if suggestion != nil {
		kind = "suggestion"
		note := body
		if note == "Suggested a change." {
			note = ""
		}
		node := strings.TrimSpace(req.Node)
		h.notify(r.Context(), advID, me, viaAssistant, h.stepOwner(r.Context(), advID, node), "suggestion", node, func(lang, who string, w emailtpl.Work) (string, emailtpl.Content) {
			return emailtpl.SuggestionMade(lang, who, w, note)
		})
	}
	h.afterComment(r, advID, me, viaAssistant, kind, strings.TrimSpace(req.Node), body, mentioned)
	httpx.JSON(w, http.StatusCreated, map[string]int64{"thread_id": threadID, "comment_id": commentID})
}

type newCommentRequest struct {
	Body string `json:"body"`
}

// ReplyToThread adds a comment to a thread. Replying to a resolved
// thread opens it again: the conversation is not over.
func (h *Handlers) ReplyToThread(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	threadID, node, ok := h.threadOf(w, r, advID)
	if !ok {
		return
	}
	var req newCommentRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	body, ok := readBody(w, req.Body)
	if !ok {
		return
	}
	viaAssistant := auth.ViaAPIToken(r.Context())
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "replying failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	commentID, mentioned, err := h.addComment(r.Context(), tx, advID, threadID, me, viaAssistant, body)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE adventure_threads SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, threadID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "replying failed", err)
		return
	}
	h.afterComment(r, advID, me, viaAssistant, "comment", node, body, mentioned)
	httpx.JSON(w, http.StatusCreated, map[string]int64{"thread_id": threadID, "comment_id": commentID})
}

// addComment stores a comment and its mentions. A person mentioning the
// assistant makes the thread a task for it.
func (h *Handlers) addComment(ctx context.Context, tx *sql.Tx, advID, threadID int64, me auth.User, viaAssistant bool, body string) (int64, []int64, error) {
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO adventure_comments (thread_id, author_id, assistant, body) VALUES ($1, $2, $3, $4) RETURNING id
	`, threadID, me.ID, viaAssistant, body).Scan(&id); err != nil {
		return 0, nil, err
	}
	people, err := h.adventurePeople(ctx, advID)
	if err != nil {
		return 0, nil, err
	}
	mentioned, assistant := mentionsIn(body, people)
	for _, uid := range mentioned {
		if _, err := tx.ExecContext(ctx, `INSERT INTO adventure_mentions (comment_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid); err != nil {
			return 0, nil, err
		}
	}
	if assistant && !viaAssistant {
		if _, err := tx.ExecContext(ctx, `UPDATE adventure_threads SET for_assistant = true WHERE id = $1`, threadID); err != nil {
			return 0, nil, err
		}
	}
	return id, mentioned, nil
}

// afterComment tells the designers open on the adventure, and mails who
// was mentioned — never the writer themselves.
func (h *Handlers) afterComment(r *http.Request, advID int64, me auth.User, viaAssistant bool, kind, node, body string, mentioned []int64) {
	h.publishCollab(advID, me, viaAssistant, kind, node)
	for _, uid := range mentioned {
		if uid != me.ID {
			h.notify(r.Context(), advID, me, viaAssistant, uid, "mention", node, func(lang, who string, w emailtpl.Work) (string, emailtpl.Content) {
				return emailtpl.Mention(lang, who, w, body)
			})
		}
	}
}

func (h *Handlers) publishCollab(advID int64, me auth.User, viaAssistant bool, kind, node string) {
	h.Hub.Publish(ws.AdventureTopic(advID), "collab_changed", map[string]any{
		"kind": kind, "node": node, "by": me.Username, "user_id": me.ID, "assistant": viaAssistant,
	})
}

// threadOf reads {threadID} and checks it belongs to the adventure.
func (h *Handlers) threadOf(w http.ResponseWriter, r *http.Request, advID int64) (int64, string, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "threadID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad thread id")
		return 0, "", false
	}
	var node string
	err = h.DB.QueryRowContext(r.Context(), `SELECT node FROM adventure_threads WHERE id = $1 AND adventure_id = $2`, id, advID).Scan(&node)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such thread")
		return 0, "", false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the thread failed", err)
		return 0, "", false
	}
	return id, node, true
}

type threadPatch struct {
	Resolved *bool `json:"resolved"`
	// SuggestionState decides a suggestion: accepted (its author has
	// merged it into the step) or rejected. Either resolves the thread.
	SuggestionState string `json:"suggestion_state,omitempty"`
}

// UpdateThread resolves a thread, or opens it again.
func (h *Handlers) UpdateThread(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	threadID, node, ok := h.threadOf(w, r, advID)
	if !ok {
		return
	}
	var req threadPatch
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.SuggestionState != "" {
		h.decideSuggestion(w, r, advID, me, threadID, node, req.SuggestionState)
		return
	}
	if req.Resolved == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to change")
		return
	}
	var err error
	if *req.Resolved {
		_, err = h.DB.ExecContext(r.Context(), `UPDATE adventure_threads SET resolved_at = now(), resolved_by = $2 WHERE id = $1 AND resolved_at IS NULL`, threadID, me.ID)
	} else {
		_, err = h.DB.ExecContext(r.Context(), `UPDATE adventure_threads SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, threadID)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "updating the thread failed", err)
		return
	}
	kind := "reopened"
	if *req.Resolved {
		kind = "resolved"
	}
	h.publishCollab(advID, me, auth.ViaAPIToken(r.Context()), kind, node)
	w.WriteHeader(http.StatusNoContent)
}

// decideSuggestion accepts or rejects an open suggestion and resolves its
// thread. Accepting records the decision; the designer has merged the
// change into the draft, as an edit like any other.
func (h *Handlers) decideSuggestion(w http.ResponseWriter, r *http.Request, advID int64, me auth.User, threadID int64, node, state string) {
	if state != "accepted" && state != "rejected" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "a suggestion is accepted or rejected", "code": "suggestion_state_invalid"})
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventure_threads SET suggestion_state = $2, resolved_at = now(), resolved_by = $3
		WHERE id = $1 AND suggestion_state = 'open'
	`, threadID, state, me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "deciding failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "that is not an open suggestion", "code": "suggestion_decided"})
		return
	}
	h.publishCollab(advID, me, auth.ViaAPIToken(r.Context()), state, node)
	var suggester sql.NullInt64
	_ = h.DB.QueryRowContext(r.Context(), `SELECT created_by FROM adventure_threads WHERE id = $1`, threadID).Scan(&suggester)
	h.notify(r.Context(), advID, me, auth.ViaAPIToken(r.Context()), suggester.Int64, "suggestion", node, func(lang, who string, w emailtpl.Work) (string, emailtpl.Content) {
		return emailtpl.SuggestionDecided(lang, who, state == "accepted", w)
	})
	w.WriteHeader(http.StatusNoContent)
}

type reactionRequest struct {
	Target string `json:"target"`
	Emoji  string `json:"emoji"`
}

// ToggleReaction adds the caller's emoji to a comment ("comment:<id>"),
// or takes it back. Only comments: a reaction acknowledges something
// said; on a step it said nothing a status or a comment did not.
func (h *Handlers) ToggleReaction(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	var req reactionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !reactionEmoji[req.Emoji] {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "not one of the reactions", "code": "reaction_invalid"})
		return
	}
	if !strings.HasPrefix(req.Target, "comment:") {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "a reaction is on a comment: comment:<id>", "code": "reaction_invalid"})
		return
	}
	var node string
	id, err := strconv.ParseInt(req.Target[8:], 10, 64)
	if err == nil {
		err = h.DB.QueryRowContext(r.Context(), `
			SELECT t.node FROM adventure_comments c JOIN adventure_threads t ON t.id = c.thread_id WHERE c.id = $1 AND t.adventure_id = $2
		`, id, advID).Scan(&node)
	}
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such comment")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `DELETE FROM adventure_reactions WHERE adventure_id = $1 AND target = $2 AND user_id = $3 AND emoji = $4`, advID, req.Target, me.ID, req.Emoji)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reacting failed", err)
		return
	}
	on := false
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO adventure_reactions (adventure_id, target, user_id, emoji) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, advID, req.Target, me.ID, req.Emoji); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "reacting failed", err)
			return
		}
		on = true
	}
	kind := "unreacted"
	if on {
		kind = "reaction"
	}
	h.publishCollab(advID, me, auth.ViaAPIToken(r.Context()), kind, node)
	httpx.JSON(w, http.StatusOK, map[string]bool{"on": on})
}

// commentOf reads {commentID}, checks it belongs to the adventure and
// that the caller wrote it.
func (h *Handlers) commentOf(w http.ResponseWriter, r *http.Request, advID int64, me auth.User) (int64, int64, string, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad comment id")
		return 0, 0, "", false
	}
	var (
		threadID int64
		author   sql.NullInt64
		node     string
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT c.thread_id, c.author_id, t.node FROM adventure_comments c JOIN adventure_threads t ON t.id = c.thread_id
		WHERE c.id = $1 AND t.adventure_id = $2
	`, id, advID).Scan(&threadID, &author, &node)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such comment")
		return 0, 0, "", false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the comment failed", err)
		return 0, 0, "", false
	}
	if !author.Valid || author.Int64 != me.ID {
		httpx.Error(w, http.StatusForbidden, "only its author can change a comment")
		return 0, 0, "", false
	}
	return id, threadID, node, true
}

// EditComment rewrites a comment of the caller's own.
func (h *Handlers) EditComment(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	id, _, node, ok := h.commentOf(w, r, advID, me)
	if !ok {
		return
	}
	var req newCommentRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	body, ok := readBody(w, req.Body)
	if !ok {
		return
	}
	people, err := h.adventurePeople(r.Context(), advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "editing failed", err)
		return
	}
	mentioned, _ := mentionsIn(body, people)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "editing failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	_, err = tx.ExecContext(r.Context(), `UPDATE adventure_comments SET body = $2, edited_at = now() WHERE id = $1`, id, body)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM adventure_mentions WHERE comment_id = $1`, id)
	}
	for _, uid := range mentioned {
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO adventure_mentions (comment_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid)
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "editing failed", err)
		return
	}
	h.publishCollab(advID, me, auth.ViaAPIToken(r.Context()), "edited", node)
	w.WriteHeader(http.StatusNoContent)
}

// DeleteComment removes a comment of the caller's own; a thread left
// without comments goes with it.
func (h *Handlers) DeleteComment(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	id, threadID, node, ok := h.commentOf(w, r, advID, me)
	if !ok {
		return
	}
	_, err := h.DB.ExecContext(r.Context(), `DELETE FROM adventure_comments WHERE id = $1`, id)
	if err == nil {
		_, err = h.DB.ExecContext(r.Context(), `DELETE FROM adventure_reactions WHERE adventure_id = $1 AND target = $2`, advID, "comment:"+strconv.FormatInt(id, 10))
	}
	if err == nil {
		_, err = h.DB.ExecContext(r.Context(), `
			DELETE FROM adventure_threads t WHERE t.id = $1 AND NOT EXISTS (SELECT 1 FROM adventure_comments c WHERE c.thread_id = t.id)
		`, threadID)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "deleting failed", err)
		return
	}
	h.publishCollab(advID, me, auth.ViaAPIToken(r.Context()), "deleted", node)
	w.WriteHeader(http.StatusNoContent)
}

type stepStatusRequest struct {
	Status *string `json:"status"`
	// OwnerID gives the step to someone; 0 takes it from whoever had it.
	OwnerID *int64 `json:"owner_id"`
}

// SetStepStatus sets how far along a step is and who has it. Fields
// left out stay as they are.
func (h *Handlers) SetStepStatus(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	node := strings.TrimSpace(chi.URLParam(r, "node"))
	if node == "" || len(node) > 200 {
		httpx.Error(w, http.StatusBadRequest, "bad step")
		return
	}
	var req stepStatusRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Status == nil && req.OwnerID == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to change")
		return
	}
	if req.Status != nil && !stepStatuses[*req.Status] {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "status is one of idea, draft, review, ready, or empty", "code": "status_invalid"})
		return
	}
	var owner any
	if req.OwnerID != nil && *req.OwnerID != 0 {
		people, err := h.adventurePeople(r.Context(), advID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
			return
		}
		found := false
		for _, p := range people {
			found = found || p.ID == *req.OwnerID
		}
		if !found {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the owner must be one of the adventure's authors", "code": "owner_invalid"})
			return
		}
		owner = *req.OwnerID
	}
	status := ""
	if req.Status != nil {
		status = *req.Status
	}
	var prevStatus string
	var prevOwner sql.NullInt64
	_ = h.DB.QueryRowContext(r.Context(), `SELECT status, owner_id FROM adventure_step_status WHERE adventure_id = $1 AND node = $2`, advID, node).Scan(&prevStatus, &prevOwner)
	// One upsert: a field left out keeps its stored value ($5/$6 say
	// which were sent).
	_, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_step_status AS s (adventure_id, node, status, owner_id, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $7, now())
		ON CONFLICT (adventure_id, node) DO UPDATE SET
		  status = CASE WHEN $5 THEN EXCLUDED.status ELSE s.status END,
		  owner_id = CASE WHEN $6 THEN EXCLUDED.owner_id ELSE s.owner_id END,
		  updated_by = EXCLUDED.updated_by,
		  updated_at = now()
	`, advID, node, status, owner, req.Status != nil, req.OwnerID != nil, me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.publishCollab(advID, me, auth.ViaAPIToken(r.Context()), "status", node)
	viaAssistant := auth.ViaAPIToken(r.Context())
	newOwner := prevOwner.Int64
	if req.OwnerID != nil {
		newOwner = *req.OwnerID
		if newOwner != prevOwner.Int64 {
			h.notify(r.Context(), advID, me, viaAssistant, newOwner, "step_assigned", node, func(lang, who string, w emailtpl.Work) (string, emailtpl.Content) {
				return emailtpl.StepAssigned(lang, who, w)
			})
		}
	}
	if status == "review" && req.Status != nil && prevStatus != "review" {
		h.notify(r.Context(), advID, me, viaAssistant, newOwner, "review_requested", node, func(lang, who string, w emailtpl.Work) (string, emailtpl.Content) {
			return emailtpl.ReviewRequested(lang, who, w)
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

type seenDTO struct {
	SeenAt time.Time       `json:"seen_at"`
	Graph  json.RawMessage `json:"graph"`
}

// GetSeen returns the draft as the caller last saw it, or 204 for an
// author who never opened it.
func (h *Handlers) GetSeen(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	var s seenDTO
	var doc []byte
	err := h.DB.QueryRowContext(r.Context(), `SELECT graph, seen_at FROM adventure_seen WHERE adventure_id = $1 AND user_id = $2`, advID, me.ID).Scan(&doc, &s.SeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading failed", err)
		return
	}
	s.Graph = doc
	httpx.JSON(w, http.StatusOK, s)
}

// MarkSeen records that the caller has seen the draft as it now is. The
// server copies the stored draft, so no graph travels with the request.
func (h *Handlers) MarkSeen(w http.ResponseWriter, r *http.Request) {
	advID, me, ok := h.collabTarget(w, r)
	if !ok {
		return
	}
	if auth.ViaAPIToken(r.Context()) {
		// An assistant's reads are not its person's.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_seen (adventure_id, user_id, graph, seen_at)
		SELECT $1, $2, d.graph, now() FROM adventure_graph_drafts d WHERE d.adventure_id = $1
		ON CONFLICT (adventure_id, user_id) DO UPDATE SET graph = EXCLUDED.graph, seen_at = EXCLUDED.seen_at
	`, advID, me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
