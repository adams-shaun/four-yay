package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestPlayerVoteNoHostRecordsTheR9Note pins the no-host fallback of
// effPlayerVote's per-voter ask (review round 2, MINOR): the deterministic
// first-ballot-entry pick is the R-9 degradation and carries the
// "(no engine host to ask)" Note every other asking site records, so the
// transcript keeps the marker. The picks themselves still land (both voters
// take the first admissible entry, the same one the pre-ask stand-in made),
// the tally is published, and the reveal Notes carry the honest picks.
func TestPlayerVoteNoHostRecordsTheR9Note(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0}
	Resolve(h, c, sa(t, "SP$ Vote | Defined$ Player | VotePlayer$ Other | StoreVoteNum$ True"))

	r9 := 0
	reveal := 0
	for _, ev := range h.log {
		switch {
		case ev.Kind == events.Note && strings.Contains(ev.Text, "(no engine host to ask)"):
			r9++
		case ev.Kind == events.Note && strings.HasPrefix(ev.Text, "votes for "):
			reveal++
		}
	}
	if r9 != 2 {
		t.Fatalf("R-9 no-host Notes = %d, want one per voter (2)", r9)
	}
	if reveal != 2 {
		t.Fatalf("reveal Notes = %d, want one per voter (2)", reveal)
	}
	// The tally is published: `Other` is the universe-everyone spelling, so
	// both seats are entries; each voter voted for the first OTHER entry --
	// seat 0 votes seat 1, seat 1 votes seat 0 -- one vote each.
	if len(c.Vote.Counts) != 2 {
		t.Fatalf("published tally = %+v, want one entry per universe seat", c.Vote.Counts)
	}
	for _, vc := range c.Vote.Counts {
		if vc.Count != 1 {
			t.Fatalf("tally entry %+v = %d votes, want 1 (one ballot, two voters, other-spelling)", vc, vc.Count)
		}
	}
}
