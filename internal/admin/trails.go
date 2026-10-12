package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/orgslug"
	"github.com/klahr/zonventure/internal/trail"
)

// Quiz trails in the console: an adventure written in the simple creator
// as cards (internal/trail). Every save stores the cards and makes the
// adventure's draft from them; the draft is published when it is used —
// "Done" in the creator, or a hosted game opening — so typing makes no
// versions. A draft changed in the designer no longer matches the
// fingerprint the creator left (adventures.trail_graph), and the creator
// then leaves the adventure alone. See migration 0111.

// trailLang is the language a trail's graph is written in.
func trailLang(language string) string {
	if strings.HasPrefix(language, "sv") {
		return "sv"
	}
	return "en"
}

// trailTemplateDTO is one template the creator offers.
type trailTemplateDTO struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Kids  bool   `json:"kids"`
	Cards int    `json:"cards"`
	// Person: the template is about someone, whose name and age a new
	// draft can be given.
	Person bool `json:"person"`
}

// ListTrailTemplates lists the templates a trail starts from, in lang.
//
// GET /api/admin/trail-templates?lang=sv
func (h *Handlers) ListTrailTemplates(w http.ResponseWriter, r *http.Request) {
	lang := trailLang(r.URL.Query().Get("lang"))
	out := []trailTemplateDTO{}
	for _, name := range trail.TemplateNames {
		t, _ := trail.NewFromTemplate(name, lang)
		out = append(out, trailTemplateDTO{Name: name, Title: t.Title, Kids: t.Kids, Cards: len(t.Cards), Person: t.Person})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"templates": out})
}

type createTrailRequest struct {
	Template string `json:"template"`
	Language string `json:"language"`
	Title    string `json:"title"`
}

// CreateTrail makes a quiz trail of the caller's org from a template.
//
// POST /api/admin/trails
func (h *Handlers) CreateTrail(w http.ResponseWriter, r *http.Request) {
	var req createTrailRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	me, _ := auth.FromContext(r.Context())
	org := callerOrgID(me)
	if !org.Valid || !canAuthor(me, org) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	lang := trailLang(req.Language)
	t, ok := trail.NewFromTemplate(req.Template, lang)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "no such template")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = t.Title
	}
	title, err := validateName(title, MaxNameLen)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	_, slug, err := createTrailAdventure(ctx, tx, org.Int64, me.ID, t, title, lang, false)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{"slug": slug})
}

// createTrailAdventure makes org's adventure of a trail from template t,
// with its draft, in tx; hidden keeps it out of the org's lists (a
// public-door trail). It returns the adventure's id and slug.
func createTrailAdventure(ctx context.Context, tx *sql.Tx, orgID, userID int64, t trail.Template, title, lang string, hidden bool) (int64, string, error) {
	slug, err := freeAdventureSlug(ctx, tx, title)
	if err != nil {
		return 0, "", err
	}
	cards, err := json.Marshal(t.Cards)
	if err != nil {
		return 0, "", err
	}
	var user any
	if userID > 0 {
		user = userID
	}
	var advID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO adventures (slug, title, author, language, org_id, created_by_user_id, trail_cards, trail_template, trail_kids, trail_hidden)
		VALUES ($1, $2, '', $3, $4, $5, $6, $7, $8, $9) RETURNING id
	`, slug, title, lang, orgID, user, cards, t.Name, t.Kids, hidden).Scan(&advID); err != nil {
		return 0, "", err
	}
	if err := saveTrailDraft(ctx, tx, advID, userID, t.Cards, lang); err != nil {
		return 0, "", err
	}
	return advID, slug, nil
}

// freeAdventureSlug is a slug for title no adventure has.
func freeAdventureSlug(ctx context.Context, db graphstore.Querier, title string) (string, error) {
	base := orgslug.Slugify(title)
	if base == "" {
		base = "trail"
	}
	for i := 0; ; i++ {
		slug := base
		if i > 0 {
			slug = base + "-" + trail.NewID()[:4]
		}
		var taken bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM adventures WHERE slug = $1)`, slug).Scan(&taken); err != nil {
			return "", err
		}
		if !taken && validSlug(slug) {
			return slug, nil
		}
		if i > 10 {
			return "", errors.New("no free slug")
		}
	}
}

// saveTrailDraft makes the adventure's draft from cards and remembers its
// fingerprint, as the draft reads back.
func saveTrailDraft(ctx context.Context, q graphstore.Querier, advID, userID int64, cards []trail.Card, lang string) error {
	if _, _, err := graphstore.SaveDraft(ctx, q, advID, userID, trail.Compile(cards, lang)); err != nil {
		return err
	}
	d, err := graphstore.LoadDraft(ctx, q, advID)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE adventures SET trail_graph = $2 WHERE id = $1`, advID, graphstore.Fingerprint(d.Graph))
	return err
}

// trailGraph is the graph cards make, as a stored one reads back:
// normalised, in the adventure's language.
func trailGraph(cards []trail.Card, language string) *graph.Graph {
	g := trail.Compile(cards, trailLang(language))
	g.Language = language
	g.Normalize()
	return g
}

// trailDTO is the creator's view of a trail.
type trailDTO struct {
	Slug string `json:"slug"`
	// Theme is the starter theme the look was taken from; empty, the
	// default look (trail_theme.go).
	Theme    string          `json:"theme"`
	Title    string          `json:"title"`
	Language string          `json:"language"`
	Template string          `json:"template"`
	Kids     bool            `json:"kids"`
	Cards    []trail.Card    `json:"cards"`
	Revision int             `json:"revision"`
	Problems []trail.Problem `json:"problems"`
	// Editable is false once the draft was changed in the designer.
	Editable bool `json:"editable"`
	// Published is whether the trail has a published version, and
	// Unpublished whether the cards say more than it.
	Published   bool `json:"published"`
	Unpublished bool `json:"unpublished"`
	// Printed is whether the cards were printed, and Reprint whether
	// they changed since.
	Printed bool `json:"printed"`
	Reprint bool `json:"reprint"`
}

// loadedTrail is a trail as the database has it.
type loadedTrail struct {
	ID       int64
	dto      trailDTO
	printed  sql.NullInt64
	graphFP  string
	hasCards bool
}

var errNotTrail = errors.New("this adventure is not a party adventure")

func loadTrail(ctx context.Context, q graphstore.Querier, slug string, forUpdate bool) (loadedTrail, error) {
	var (
		t   loadedTrail
		raw []byte
	)
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, `
		SELECT id, slug, title, language, trail_template, trail_kids, trail_cards, trail_revision, trail_graph, trail_printed, trail_theme
		FROM adventures WHERE slug = $1`+lock, slug).Scan(&t.ID, &t.dto.Slug, &t.dto.Title, &t.dto.Language,
		&t.dto.Template, &t.dto.Kids, &raw, &t.dto.Revision, &t.graphFP, &t.printed, &t.dto.Theme)
	if err != nil {
		return t, err
	}
	if raw == nil {
		return t, errNotTrail
	}
	t.hasCards = true
	if err := json.Unmarshal(raw, &t.dto.Cards); err != nil {
		return t, err
	}
	if t.dto.Cards == nil {
		t.dto.Cards = []trail.Card{}
	}
	return t, nil
}

// describe fills in what the creator shows besides the cards.
func (t *loadedTrail) describe(ctx context.Context, q graphstore.Querier) error {
	t.dto.Problems = trail.Problems(t.dto.Cards)
	if t.dto.Problems == nil {
		t.dto.Problems = []trail.Problem{}
	}
	d, err := graphstore.LoadDraft(ctx, q, t.ID)
	if err != nil && !errors.Is(err, graphstore.ErrNotFound) {
		return err
	}
	t.dto.Editable = err == nil && graphstore.Fingerprint(d.Graph) == t.graphFP
	pub, err := graphstore.Published(ctx, q, t.ID)
	switch {
	case errors.Is(err, graphstore.ErrNotFound):
		t.dto.Unpublished = true
	case err != nil:
		return err
	default:
		t.dto.Published = true
		t.dto.Unpublished = graphstore.Differs(pub.Graph, trailGraph(t.dto.Cards, t.dto.Language))
	}
	t.dto.Printed = t.printed.Valid
	t.dto.Reprint = t.printed.Valid && int(t.printed.Int64) != t.dto.Revision
	return nil
}

// trailFor loads the trail in the URL for an author of its org, writing
// the error when it can't.
func (h *Handlers) trailFor(w http.ResponseWriter, r *http.Request) (loadedTrail, bool) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return loadedTrail{}, false
	}
	t, err := loadTrail(r.Context(), h.DB, slug, false)
	switch {
	case errors.Is(err, errNotTrail):
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "not_trail"})
		return t, false
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "trail lookup failed", err)
		return t, false
	}
	return t, true
}

func (h *Handlers) writeTrail(w http.ResponseWriter, ctx context.Context, t loadedTrail, status int) {
	if err := t.describe(ctx, h.DB); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "trail lookup failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, t.dto)
}

// GetTrail is the creator's view of a trail.
//
// GET /api/admin/adventures/{slug}/cards
func (h *Handlers) GetTrail(w http.ResponseWriter, r *http.Request) {
	t, ok := h.trailFor(w, r)
	if !ok {
		return
	}
	h.writeTrail(w, r.Context(), t, http.StatusOK)
}

type putTrailRequest struct {
	Title    *string      `json:"title,omitempty"`
	Cards    []trail.Card `json:"cards"`
	Revision int          `json:"revision"`
}

// PutTrail saves the cards — finished or not — and makes the draft from
// them. Revision must be the one the creator loaded: a 409 with the
// trail as it is now means someone else saved meanwhile.
//
// PUT /api/admin/adventures/{slug}/cards
func (h *Handlers) PutTrail(w http.ResponseWriter, r *http.Request) {
	var req putTrailRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if _, ok := h.trailFor(w, r); !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if t, ok := h.saveTrail(w, r.Context(), chi.URLParam(r, "slug"), me.ID, req); ok {
		h.writeTrail(w, r.Context(), t, http.StatusOK)
	}
}

// saveTrail saves req to the trail slug, as PutTrail does for every
// door, and returns the trail as it is then. When it can't, it writes
// why and returns false.
func (h *Handlers) saveTrail(w http.ResponseWriter, ctx context.Context, slug string, userID int64, req putTrailRequest) (loadedTrail, bool) {
	if err := trail.Check(req.Cards); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return loadedTrail{}, false
	}
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	defer tx.Rollback() //nolint:errcheck
	t, err := loadTrail(ctx, tx, slug, true)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	if err := t.describe(ctx, tx); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	if !t.dto.Editable {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "the trail was changed in the designer", "code": "designer"})
		return loadedTrail{}, false
	}
	if req.Revision != t.dto.Revision {
		httpx.JSON(w, http.StatusConflict, map[string]any{"error": "the trail was saved elsewhere meanwhile", "code": "changed", "trail": t.dto})
		return loadedTrail{}, false
	}
	title := t.dto.Title
	if req.Title != nil {
		if title, err = validateName(*req.Title, MaxNameLen); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return loadedTrail{}, false
		}
	}
	cards, err := json.Marshal(req.Cards)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE adventures SET trail_cards = $2, trail_revision = trail_revision + 1, title = $3 WHERE id = $1
	`, t.ID, cards, title); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	if err := saveTrailDraft(ctx, tx, t.ID, userID, req.Cards, trailLang(t.dto.Language)); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	t, err = loadTrail(ctx, h.DB, t.dto.Slug, false)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return loadedTrail{}, false
	}
	return t, true
}

// PublishTrail publishes the cards as they are, when they can be played.
//
// POST /api/admin/adventures/{slug}/cards/publish
func (h *Handlers) PublishTrail(w http.ResponseWriter, r *http.Request) {
	t, ok := h.trailFor(w, r)
	if !ok {
		return
	}
	if p := trail.Problems(t.dto.Cards); len(p) > 0 {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "the trail " + p[0].Message, "code": "problems", "problems": p})
		return
	}
	me, _ := auth.FromContext(r.Context())
	if _, err := publishTrail(r.Context(), h.DB, t.ID, me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "publish failed", err)
		return
	}
	h.writeTrail(w, r.Context(), t, http.StatusOK)
}

// publishTrail publishes adventure advID's cards when it is a trail the
// creator still owns, they can be played, and they say something the
// published version doesn't; it reports whether it did.
func publishTrail(ctx context.Context, q graphstore.Querier, advID, userID int64) (bool, error) {
	var slug string
	if err := q.QueryRowContext(ctx, `SELECT slug FROM adventures WHERE id = $1`, advID).Scan(&slug); err != nil {
		return false, err
	}
	t, err := loadTrail(ctx, q, slug, false)
	if errors.Is(err, errNotTrail) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := t.describe(ctx, q); err != nil {
		return false, err
	}
	if !t.dto.Editable || len(t.dto.Problems) > 0 || !t.dto.Unpublished {
		return false, nil
	}
	if _, _, err := graphstore.Publish(ctx, q, t.ID, userID, trail.Compile(t.dto.Cards, trailLang(t.dto.Language)), ""); err != nil {
		return false, err
	}
	return true, nil
}

// OpenTrailInDesigner hands a trail to the designer for good: its cards
// go, and the graph is the designer's from now on.
//
// POST /api/admin/adventures/{slug}/cards/designer
func (h *Handlers) OpenTrailInDesigner(w http.ResponseWriter, r *http.Request) {
	t, ok := h.trailFor(w, r)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventures SET trail_cards = NULL, trail_graph = '', trail_printed = NULL WHERE id = $1
	`, t.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "handover failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
