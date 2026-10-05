package graphstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// Draft is the adventure's working copy.
type Draft struct {
	Graph     *graph.Graph
	UpdatedAt time.Time
	// DevSynced says the adventure's dev row took the draft too, so its
	// dev teams should look again (see EnsureDev).
	DevSynced bool
}

// SaveDraft stores g as the adventure's draft, whatever its errors — the
// report is returned for the editor to show, not enforced. Media that is
// not the adventure's is still refused: a draft becomes a release by
// publishing, and there is no point storing what could never publish.
func SaveDraft(ctx context.Context, q Querier, adventureID, userID int64, g *graph.Graph) (Draft, valid.Report, error) {
	return SaveDraftIf(ctx, q, adventureID, userID, g, nil)
}

// ErrDraftChanged is SaveDraftIf's refusal: the draft was saved by
// someone else since the caller loaded it.
var ErrDraftChanged = errors.New("the draft was changed since it was loaded")

// SaveDraftIf is SaveDraft for a caller that loaded the draft saved at
// base and must not overwrite a later save — the designer while an
// assistant edits the same adventure, and the other way round. A nil
// base saves unconditionally, as does a base when there is no draft yet.
func SaveDraftIf(ctx context.Context, q Querier, adventureID, userID int64, g *graph.Graph, base *time.Time) (Draft, valid.Report, error) {
	if err := stampLanguage(ctx, q, adventureID, g); err != nil {
		return Draft{}, valid.Report{}, err
	}
	report := valid.Check(g)
	if err := checkMedia(ctx, q, adventureID, g); err != nil {
		return Draft{}, report, err
	}
	doc, err := json.Marshal(g)
	if err != nil {
		return Draft{}, report, fmt.Errorf("encoding graph: %w", err)
	}
	var user any
	if userID > 0 {
		user = userID
	}
	d := Draft{Graph: g}
	err = q.QueryRowContext(ctx, `
		INSERT INTO adventure_graph_drafts AS d (adventure_id, graph, updated_at, updated_by_user_id)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (adventure_id) DO UPDATE
		SET graph = EXCLUDED.graph, updated_at = EXCLUDED.updated_at, updated_by_user_id = EXCLUDED.updated_by_user_id
		WHERE $4::timestamptz IS NULL OR d.updated_at = $4
		RETURNING updated_at
	`, adventureID, doc, user, base).Scan(&d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, report, ErrDraftChanged
	}
	if err != nil {
		return d, report, err
	}
	// A dev ticket plays the draft as it is edited: its row follows.
	res, err := q.ExecContext(ctx, `UPDATE adventure_graphs SET graph = $2 WHERE adventure_id = $1 AND version = 0`, adventureID, doc)
	if err != nil {
		return d, report, fmt.Errorf("syncing the dev graph: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		d.DevSynced = true
	}
	return d, report, nil
}

// EnsureDev returns the adventure's dev row — version 0 in
// adventure_graphs, which a dev event plays and every draft save
// rewrites — making it from g when there is none. Never a release: it
// is not counted among the versions, and never published.
func EnsureDev(ctx context.Context, q Querier, adventureID int64, g *graph.Graph) (int64, error) {
	if err := stampLanguage(ctx, q, adventureID, g); err != nil {
		return 0, err
	}
	doc, err := json.Marshal(g)
	if err != nil {
		return 0, fmt.Errorf("encoding graph: %w", err)
	}
	var id int64
	err = q.QueryRowContext(ctx, `
		INSERT INTO adventure_graphs (adventure_id, version, graph, label)
		VALUES ($1, 0, $2, 'dev')
		ON CONFLICT (adventure_id, version) DO UPDATE SET graph = EXCLUDED.graph
		RETURNING id
	`, adventureID, doc).Scan(&id)
	return id, err
}

// SaveParty stores the graph a party event plays — its adventure's
// published version as the party adapted it — and returns its id. The
// row is the event's own: version -eventID, so it is never counted among
// the versions, never published, and goes when the event is purged. An
// invalid graph is refused with InvalidError, like any release.
func SaveParty(ctx context.Context, q Querier, adventureID, eventID int64, g *graph.Graph) (int64, error) {
	if r := valid.Check(g); r.HasErrors() {
		return 0, &InvalidError{Report: r}
	}
	doc, err := json.Marshal(g)
	if err != nil {
		return 0, fmt.Errorf("encoding graph: %w", err)
	}
	var id int64
	err = q.QueryRowContext(ctx, `
		INSERT INTO adventure_graphs (adventure_id, version, graph, label)
		VALUES ($1, $2, $3, 'party')
		ON CONFLICT (adventure_id, version) DO UPDATE SET graph = EXCLUDED.graph
		RETURNING id
	`, adventureID, -eventID, doc).Scan(&id)
	return id, err
}

// LoadDraft returns the draft, or ErrNotFound.
func LoadDraft(ctx context.Context, q Querier, adventureID int64) (Draft, error) {
	var (
		d    Draft
		doc  []byte
		lang string
	)
	err := q.QueryRowContext(ctx, `
		SELECT d.graph, d.updated_at, a.language
		FROM adventure_graph_drafts d JOIN adventures a ON a.id = d.adventure_id
		WHERE d.adventure_id = $1
	`, adventureID).Scan(&doc, &d.UpdatedAt, &lang)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	if err != nil {
		return Draft{}, err
	}
	var g graph.Graph
	if err := json.Unmarshal(doc, &g); err != nil {
		return Draft{}, fmt.Errorf("decoding draft: %w", err)
	}
	g.Normalize()
	// The draft follows the adventure's language, even when that changed
	// since it was saved.
	g.Language = lang
	d.Graph = &g
	return d, nil
}

// Publish validates g, writes it as a new version named label, and makes
// it the published one. An invalid graph is refused with InvalidError.
func Publish(ctx context.Context, q Querier, adventureID, userID int64, g *graph.Graph, label string) (Stored, valid.Report, error) {
	s, report, err := saveVersion(ctx, q, adventureID, userID, g, label)
	if err != nil {
		return s, report, err
	}
	return s, report, point(ctx, q, adventureID, s.ID)
}

// PublishVersion makes an existing version the published one — a
// rollback. It is revalidated first: the validator may have learned
// something since the version was written.
func PublishVersion(ctx context.Context, q Querier, adventureID int64, version int) (Stored, error) {
	s, err := ByVersion(ctx, q, adventureID, version)
	if err != nil {
		return s, err
	}
	if r := valid.Check(s.Graph); r.HasErrors() {
		return s, &InvalidError{Report: r}
	}
	return s, point(ctx, q, adventureID, s.ID)
}

func point(ctx context.Context, q Querier, adventureID, graphID int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE adventures SET published_graph_id = $1, published_at = now() WHERE id = $2
	`, graphID, adventureID)
	return err
}

// Published returns the published version, or ErrNotFound.
func Published(ctx context.Context, q Querier, adventureID int64) (Stored, error) {
	return scan(q.QueryRowContext(ctx, `
		SELECT g.id, g.adventure_id, g.version, g.label, g.graph, a.language
		FROM adventures a JOIN adventure_graphs g ON g.id = a.published_graph_id
		WHERE a.id = $1
	`, adventureID))
}

// ByVersion returns one version by its number.
func ByVersion(ctx context.Context, q Querier, adventureID int64, version int) (Stored, error) {
	return scan(q.QueryRowContext(ctx, `
		SELECT id, adventure_id, version, label, graph, `+adventureLanguage+` FROM adventure_graphs
		WHERE adventure_id = $1 AND version = $2 AND version > 0
	`, adventureID, version))
}

// SetLabel names a version.
func SetLabel(ctx context.Context, q Querier, adventureID int64, version int, label string) error {
	res, err := q.ExecContext(ctx, `
		UPDATE adventure_graphs SET label = $1 WHERE adventure_id = $2 AND version = $3 AND version > 0
	`, label, adventureID, version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Version is one entry in the version history.
type Version struct {
	Version   int       `json:"version"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author,omitempty"`
	Published bool      `json:"published,omitempty"`
}

// Lister is Querier plus QueryContext, for reading many rows.
type Lister interface {
	Querier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Versions lists every version, newest first.
func Versions(ctx context.Context, q Lister, adventureID int64) ([]Version, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT g.version, g.label, g.created_at, COALESCE(u.username, ''), g.id = a.published_graph_id
		FROM adventure_graphs g
		JOIN adventures a ON a.id = g.adventure_id
		LEFT JOIN users u ON u.id = g.created_by_user_id
		WHERE g.adventure_id = $1 AND g.version > 0
		ORDER BY g.version DESC
	`, adventureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var v Version
		var published sql.NullBool
		if err := rows.Scan(&v.Version, &v.Label, &v.CreatedAt, &v.Author, &published); err != nil {
			return nil, err
		}
		v.Published = published.Bool
		out = append(out, v)
	}
	return out, rows.Err()
}

// Differs reports whether two graphs differ in anything a team would
// notice. Card positions do not count — moving a card is not a change
// worth publishing — which matches the editor's "unpublished changes".
func Differs(a, b *graph.Graph) bool {
	return canonical(a) != canonical(b)
}

func canonical(g *graph.Graph) string {
	if g == nil {
		return ""
	}
	c := *g
	// Frames are layout, like positions: moving one is not a change to
	// publish.
	c.Frames = nil
	c.Nodes = make([]graph.Node, len(g.Nodes))
	for i, n := range g.Nodes {
		n.X, n.Y = 0, 0
		c.Nodes[i] = n
	}
	b, _ := json.Marshal(c)
	return string(b)
}
