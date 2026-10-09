package session

import (
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// PickKey is where Team.Picks holds the answer to one block.
func PickKey(node, block string) string { return node + "\x00" + block }

// Pick answers a quiz question with 1, X or 2, or a tie-breaker with a
// number, on the step the team is at. The team may change its mind until
// it moves on: once it has left the step the answer is final, even if it
// comes back. Nothing moves and nothing scores until the team finishes.
//
// It also keeps what the team struck on a suspect board — the cards'
// indexes, comma-separated, any time — and deals a draw's card: the
// choice is ignored, and a second draw keeps the first card.
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
	case graph.BlockSuspects:
		struck, ok := cardList(choice, len(b.Cards))
		if !ok {
			return Result{}, nil, ErrWrongNode
		}
		t.setPick(n.Slug, b.ID, struck)
		return Result{}, nil, nil
	case graph.BlockDraw:
		if len(b.Options) == 0 {
			return Result{}, nil, ErrWrongNode
		}
		if t.Picked(n.Slug, b.ID) == "" {
			t.setPick(n.Slug, b.ID, strconv.Itoa(rand.IntN(len(b.Options))))
		}
		return Result{}, nil, nil
	default:
		return Result{}, nil, ErrWrongNode
	}
	if t.PickFinal(n.Slug) {
		return Result{}, nil, ErrNotAllowed
	}
	t.setPick(n.Slug, b.ID, choice)
	return Result{}, nil, nil
}

func (t *Team) setPick(node, block, choice string) {
	if t.Picks == nil {
		t.Picks = map[string]string{}
	}
	t.Picks[PickKey(node, block)] = choice
}

// cardList reads a suspect board's struck cards, "2,0", as the sorted
// list of distinct indexes below n; empty strikes none.
func cardList(s string, n int) (string, bool) {
	var idx []int
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		i, err := strconv.Atoi(f)
		if err != nil || i < 0 || i >= n {
			return "", false
		}
		if !slices.Contains(idx, i) {
			idx = append(idx, i)
		}
	}
	slices.Sort(idx)
	out := make([]string, len(idx))
	for j, i := range idx {
		out[j] = strconv.Itoa(i)
	}
	return strings.Join(out, ","), true
}

// PickFinal reports whether the team's answers on a step are final:
// it has left the step, by continuing or any other way out.
func (t *Team) PickFinal(node string) bool {
	r := t.Nodes[node]
	return r != nil && (r.Solved || r.HasOutcome)
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
// A typed one (see graph.QuizPuzzles) has no Options: Answer is the
// first accepted answer, and Picked what the team typed when right.
type QuizRow struct {
	Node     string   `json:"node"`
	Block    string   `json:"block"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Picked   string   `json:"picked,omitempty"`
	Answer   string   `json:"answer"`
	Right    bool     `json:"right"`
	Typed    bool     `json:"typed,omitempty"`
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
		if row, ok := t.typedRow(n); ok {
			q.Total++
			q.Items = append(q.Items, row)
			if row.Right {
				q.Right++
			}
		}
		for _, b := range n.Blocks() {
			picked := t.Picks[PickKey(n.Slug, b.ID)]
			switch b.Kind {
			case graph.BlockQuiz:
				right := picked != "" && picked == b.Answer
				q.Total++
				q.Items = append(q.Items, QuizRow{Node: n.Slug, Block: b.ID, Question: b.Text, Options: b.Options, Picked: picked, Answer: b.Answer, Right: right})
				if right {
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

// typedRow is a puzzle answered by typing, as a quiz walk row, when the
// graph lists those (graph.QuizPuzzles). Its question is its first text,
// and it is right when the team left it by an accepted answer.
func (t *Team) typedRow(n graph.Node) (QuizRow, bool) {
	answer, ok := t.G.QuizAnswer(n)
	if !ok {
		return QuizRow{}, false
	}
	row := QuizRow{Node: n.Slug, Question: n.Title, Answer: answer, Typed: true}
	if i := slices.IndexFunc(n.Blocks(), func(b graph.Block) bool { return b.Kind == graph.BlockText && b.Text != "" }); i >= 0 {
		b := n.Blocks()[i]
		row.Block, row.Question = b.ID, b.Text
	}
	row.Picked, row.Right = t.AnswerTo(n.Slug)
	return row, true
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
