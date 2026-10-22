package play

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
)

// graphCache keeps decoded graphs by id, as every view, action and ping
// asks again. A release or a party's own copy never changes; a dev row
// (version 0) follows the draft, saved over in place, so it is kept
// with its row's version (xmin) — and its adventure's language, which a
// draft stored without one takes (graphstore.ByID) — and read again once
// either moves.
type graphCache struct {
	mu   sync.Mutex
	m    map[int64]*cachedGraph
	tick uint64
}

type cachedGraph struct {
	g *graph.Graph
	// splits are g's, worked out once for every request (graph.Splits.Warm),
	// and walked whether any condition in it asks how far a team walked:
	// kept here, with the graph, and let go with it.
	splits *graph.Splits
	walked bool
	// mark is a dev row's version and its adventure's language as they
	// were read; "" for one that never changes.
	mark string
	used uint64
}

// maxGraphs bounds the cache; past it, the graph used longest ago goes.
const maxGraphs = 64

// get is the stored graph id, with what is worked out of it once, from
// the cache while it is as stored.
func (c *graphCache) get(ctx context.Context, q Querier, id int64) (*cachedGraph, error) {
	c.mu.Lock()
	hit := c.m[id]
	if hit != nil && hit.mark == "" {
		c.touch(hit)
		c.mu.Unlock()
		return hit, nil
	}
	c.mu.Unlock()
	// A dev row, or a graph not read yet: its version first, so a draft
	// saved while it is read is read again next time.
	var (
		version int
		mark    string
	)
	if err := q.QueryRowContext(ctx, `
		SELECT version, xmin::text || ':' || COALESCE((SELECT language FROM adventures WHERE adventures.id = adventure_graphs.adventure_id), '')
		FROM adventure_graphs WHERE id = $1`, id).Scan(&version, &mark); err != nil {
		return nil, err
	}
	if version != 0 {
		mark = ""
	}
	if hit != nil && hit.mark == mark {
		c.mu.Lock()
		c.touch(hit)
		c.mu.Unlock()
		return hit, nil
	}
	st, err := graphstore.ByID(ctx, q, id)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[int64]*cachedGraph{}
	}
	if _, ok := c.m[id]; !ok && len(c.m) >= maxGraphs {
		oldest, least := int64(0), uint64(0)
		for k, e := range c.m {
			if least == 0 || e.used < least {
				oldest, least = k, e.used
			}
		}
		delete(c.m, oldest)
	}
	e := &cachedGraph{g: st.Graph, splits: st.Graph.Splits().Warm(), walked: usesWalked(st.Graph), mark: mark}
	c.touch(e)
	c.m[id] = e
	return e, nil
}

// touch marks e used now; c.mu is held.
func (c *graphCache) touch(e *cachedGraph) {
	c.tick++
	e.used = c.tick
}

// Graph is the stored graph id, from the engine's cache: what tracks and
// the organisers' console read a team's graph by, as the engine does.
// One graph serves every request at once, so no one changes it: a
// variant (a party's, played as stations) is a copy (Graph.WithOverrides,
// Graph.AsStations), and graphstore normalises it before it is cached.
func (e *Engine) Graph(ctx context.Context, id int64) (*graph.Graph, error) {
	return e.graph(ctx, e.DB, id)
}

// Splits is the stored graph id's splits, worked out once and shared
// (graph.Splits.Warm), its graph their Graph: for the console to read a
// team's groups as the engine does.
func (e *Engine) Splits(ctx context.Context, id int64) (*graph.Splits, error) {
	c, err := e.loadGraph(ctx, e.DB, id)
	if err != nil {
		return nil, err
	}
	return c.splits, nil
}

func (e *Engine) graph(ctx context.Context, q Querier, id int64) (*graph.Graph, error) {
	c, err := e.loadGraph(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return c.g, nil
}

// loadGraph is graph, with what is worked out of it once: its splits,
// shared (Team.ShareSplits), and whether it asks how far a team walked.
func (e *Engine) loadGraph(ctx context.Context, q Querier, id int64) (*cachedGraph, error) {
	c, err := e.graphs.get(ctx, q, id)
	if errors.Is(err, graphstore.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoGraph
	}
	return c, err
}
