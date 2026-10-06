package admin

import (
	"slices"
	"testing"
)

func TestMentionsIn(t *testing.T) {
	t.Parallel()
	people := []userRef{{ID: 1, Name: "anna"}, {ID: 2, Name: "bob@example.org"}, {ID: 3, Name: "Carl.D"}}
	cases := []struct {
		body      string
		ids       []int64
		assistant bool
	}{
		{"@anna can you look?", []int64{1}, false},
		{"Thanks, @bob@example.org.", []int64{2}, false},
		{"@carl.d and @ANNA, @anna again", []int64{3, 1}, false},
		{"@assistant make the hint harder", nil, true},
		{"mail anna@anna.se, not a mention; @nobody either", nil, false},
		{"(@anna)", []int64{1}, false},
	}
	for _, c := range cases {
		ids, assistant := mentionsIn(c.body, people)
		if !slices.Equal(ids, c.ids) || assistant != c.assistant {
			t.Errorf("mentionsIn(%q) = %v, %v; want %v, %v", c.body, ids, assistant, c.ids, c.assistant)
		}
	}
}
