package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
)

// remapGraphMedia rewrites every adventure_media id g refers to through
// ids. An id not in ids is left as it is.
func remapGraphMedia(g *graph.Graph, ids map[int64]int64) {
	id := func(old int64) int64 {
		if n, ok := ids[old]; ok {
			return n
		}
		return old
	}
	list := func(ms []graph.Media) {
		for i := range ms {
			ms[i].ID = id(ms[i].ID)
		}
	}
	one := func(m *graph.Media) {
		if m != nil {
			m.ID = id(m.ID)
		}
	}
	blocks := func(bs []graph.Block) {
		for i := range bs {
			bs[i].Media = id(bs[i].Media)
		}
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if c := n.PuzzleCfg; c != nil {
			blocks(c.Blocks)
			list(c.LegacyMedia)
			for j := range c.Hints {
				list(c.Hints[j].Media)
			}
			if a := c.Answer; a != nil {
				one(a.Image)
				for j := range a.Symbols {
					one(a.Symbols[j].Image)
				}
			}
		}
		if c := n.StoryCfg; c != nil {
			blocks(c.Blocks)
			list(c.LegacyMedia)
		}
		if c := n.EndCfg; c != nil {
			blocks(c.Blocks)
			list(c.LegacyMedia)
		}
		if c := n.HubCfg; c != nil {
			for j := range c.Branches {
				one(c.Branches[j].Image)
			}
		}
	}
	for i := range g.Journal {
		list(g.Journal[i].Media)
	}
}

// copyGraphMedia copies the source adventure's media rows named by ids
// into dstAdvID and returns old id → new id. Id 0 (a block with no file
// yet) is skipped.
func copyGraphMedia(ctx context.Context, q graphstore.Querier, srcAdvID, dstAdvID, userID int64, ids []int64) (map[int64]int64, error) {
	var user any
	if userID > 0 {
		user = userID
	}
	out := make(map[int64]int64, len(ids))
	for _, old := range ids {
		if old == 0 {
			continue
		}
		var id int64
		if err := q.QueryRowContext(ctx, `
			INSERT INTO adventure_media (adventure_id, kind, mime_type, size_bytes, sha256, content, created_by_user_id)
			SELECT $1, kind, mime_type, size_bytes, sha256, content, $2
			FROM adventure_media WHERE id = $3 AND adventure_id = $4
			ON CONFLICT (adventure_id, sha256) DO UPDATE SET adventure_id = EXCLUDED.adventure_id
			RETURNING id
		`, dstAdvID, user, old, srcAdvID).Scan(&id); err != nil {
			return nil, fmt.Errorf("copy media %d: %w", old, err)
		}
		out[old] = id
	}
	return out, nil
}

// insertGraphMedia stores content as one of the adventure's media rows,
// once per content like putMedia, inside the caller's transaction.
func insertGraphMedia(ctx context.Context, q graphstore.Querier, advID, userID int64, kind graph.MediaKind, mime string, content []byte) (int64, error) {
	sum := sha256.Sum256(content)
	var user any
	if userID > 0 {
		user = userID
	}
	var id int64
	err := q.QueryRowContext(ctx, `
		INSERT INTO adventure_media (adventure_id, kind, mime_type, size_bytes, sha256, content, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (adventure_id, sha256) DO UPDATE SET adventure_id = EXCLUDED.adventure_id
		RETURNING id
	`, advID, string(kind), mime, int64(len(content)), hex.EncodeToString(sum[:]), content, user).Scan(&id)
	return id, err
}
