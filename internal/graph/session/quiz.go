package session

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// PickKey is where Team.Picks holds the answer to one block.
func PickKey(node, block string) string { return node + "\x00" + block }

// Pick answers a quiz question with 1, X or 2, or a tie-breaker with a
// number, on the step the team is at. An answer is final: a second is
// refused. Nothing moves and nothing scores until the team finishes.
func (t *Team) Pick(node, block, choice string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	i := slices.IndexFunc(n.Blocks(), func(b graph.Block) bool { return b.ID == block })
	if i < 0 {
		return Result{}, nil, ErrWrongNode
	}
	b := n.Blocks()[i]
	if !t.blockShown(n.Slug, b) {
		return Result{}, nil, ErrNotAllowed
	}
	choice = strings.TrimSpace(choice)
	switch b.Kind {
	case graph.BlockQuiz:
		choice = strings.ToUpper(choice)
		if !slices.Contains(graph.QuizChoices, choice) {
			return Result{}, nil, ErrWrongNode
		}
	case graph.BlockTiebreak:
		f, err := strconv.ParseFloat(strings.ReplaceAll(choice, ",", "."), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return Result{}, nil, ErrWrongNode
		}
		choice = strconv.FormatFloat(f, 'f', -1, 64)
	default:
		return Result{}, nil, ErrWrongNode
	}
	key := PickKey(n.Slug, b.ID)
	if t.Picks[key] != "" {
		return Result{}, nil, ErrNotAllowed
	}
	if t.Picks == nil {
		t.Picks = map[string]string{}
	}
	t.Picks[key] = choice
	return Result{}, nil, nil
}

// QuizResult is how a team did on the quiz walk, shown when it finishes.
type QuizResult struct {
	Right  int       `json:"right"`
	Total  int       `json:"total"`
	Points float64   `json:"points"`
	Items  []QuizRow `json:"items"`
	// Tiebreak is the team's guess at the tie-breaker, and the answer.
	Tiebreak *TiebreakRow `json:"tiebreak,omitempty"`
}

// QuizRow is one question: what the team picked and what was right.
type QuizRow struct {
	Node     string   `json:"node"`
	Block    string   `json:"block"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Picked   string   `json:"picked,omitempty"`
	Answer   string   `json:"answer"`
}

type TiebreakRow struct {
	Node     string   `json:"node"`
	Block    string   `json:"block"`
	Question string   `json:"question"`
	Guess    *float64 `json:"guess,omitempty"`
	Answer   float64  `json:"answer"`
}

// Quiz marks the team's quiz walk: every question on a step it reached,
// in graph order. One on a branch it never took does not count against it.
func (t *Team) Quiz() QuizResult {
	var q QuizResult
	for _, n := range t.G.Nodes {
		if !t.Visited(n.Slug) {
			continue
		}
		for _, b := range n.Blocks() {
			picked := t.Picks[PickKey(n.Slug, b.ID)]
			switch b.Kind {
			case graph.BlockQuiz:
				q.Total++
				q.Items = append(q.Items, QuizRow{Node: n.Slug, Block: b.ID, Question: b.Text, Options: b.Options, Picked: picked, Answer: b.Answer})
				if picked != "" && picked == b.Answer {
					q.Right++
					q.Points += float64(max(1, b.Worth))
				}
			case graph.BlockTiebreak:
				if b.Target == nil || q.Tiebreak != nil {
					continue
				}
				row := &TiebreakRow{Node: n.Slug, Block: b.ID, Question: b.Text, Answer: *b.Target}
				if f, err := strconv.ParseFloat(picked, 64); err == nil {
					row.Guess = &f
				}
				q.Tiebreak = row
			}
		}
	}
	return q
}

// TiebreakMiss is how far the team's tie-breaker guess is from the
// answer; +Inf with no guess or no tie-breaker, so it ranks last.
func (t *Team) TiebreakMiss() float64 {
	tb := t.Quiz().Tiebreak
	if tb == nil || tb.Guess == nil {
		return math.Inf(1)
	}
	return math.Abs(*tb.Guess - tb.Answer)
}
