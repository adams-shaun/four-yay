package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestFoodFellowshipPartnerPairSeatsBothCommanders pins the two-commander
// partner seating end to end on the Food and Fellowship precon's partners —
// real corpus cards Frodo, Adventurous Hobbit + Sam, Loyal Attendant (mutual
// K:Partner with, the CR 903.13c shape the deck-file layer's union-identity
// validator admits): the engine seats BOTH as seat 0's commanders in the
// command zone, and each is castable from there. The deck-file plumbing that
// feeds Config.Commanders is pinned at the deck and cmd layers; this leaf is
// the engine half, over the same pair a partner deck file now names.
func TestFoodFellowshipPartnerPairSeatsBothCommanders(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	frodo := lookup(t, reg, "Frodo, Adventurous Hobbit")
	sam := lookup(t, reg, "Sam, Loyal Attendant")
	if !partnerPairOK(frodo, sam) {
		t.Fatal("fixture drift: the corpus Frodo/Sam pair no longer reads as a legal partner pair")
	}
	cfg := commanderConfig(t, reg, []*cards.Card{frodo, sam}, []int{0, 1})
	e := New(cfg)
	e.Advance()
	cmds := e.G.Players[0].Commanders
	if len(cmds) != 2 {
		t.Fatalf("seat 0 has %d commanders in the command zone, want 2", len(cmds))
	}
	for i, id := range cmds {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZCommand {
			t.Fatalf("commander %d is not in the command zone (obj %v)", i, o)
		}
	}
	if e.G.Players[0].Life != 40 {
		t.Fatalf("commander starting life is %d, want 40", e.G.Players[0].Life)
	}
	// Both partners are castable from the command zone: Frodo is {W}{B},
	// Sam is {1}{G}{W}.
	toMain1(t, e)
	addMana(t, e, 0, "WWB")
	addMana(t, e, 0, "WGG")
	d := e.Pending()
	frodoCast, samCast := false, false
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast Frodo, Adventurous Hobbit" {
			frodoCast = true
		}
		if o.Kind == "cast" && o.Label == "Cast Sam, Loyal Attendant" {
			samCast = true
		}
	}
	if !frodoCast || !samCast {
		t.Fatalf("both partners must be offered as command-zone casts (frodo=%v sam=%v): %+v", frodoCast, samCast, d.Options)
	}
}
