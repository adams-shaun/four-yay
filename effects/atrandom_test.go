package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// askFixture is fixtureHost behind the asking double, so a posed decision
// is observable (h.asked) instead of degrading to the R-9 stand-in.
func askFixture(t *testing.T) (*askHost, *Ctx) {
	t.Helper()
	fh, c := fixtureHost(t)
	return &askHost{fakeHost: *fh}, c
}

// `AtRandom$ Urza` (Urza, Academy Headmaster) draws only among choices
// whose mandatory ValidTgts$ has a legal target: with no creature to
// destroy, the lone feasible GainLife is taken without an ask and without
// consuming the rng (fakeHost.Rand always answers 0, which would otherwise
// draw the Destroy).
func TestGenericChoiceUrzaSkipsChoicesWithNoLegalTarget(t *testing.T) {
	h, c := askFixture(t)
	c.SVars = map[string]string{
		"Kill": "DB$ Destroy | ValidTgts$ Creature.OppCtrl+powerGE9",
		"Gain": "DB$ GainLife | LifeAmount$ 3",
	}
	before := h.Game().Players[0].Life
	Resolve(h, c, &cards.SA{Kind: "DB", API: "GenericChoice", Params: map[string]string{
		"AtRandom": "Urza", "Choices": "Kill,Gain",
	}})
	if h.asked != nil {
		t.Fatalf("AtRandom$ Urza asked: %+v", h.asked)
	}
	if h.n != 0 {
		t.Fatalf("rng draws = %d, want 0 (one feasible choice)", h.n)
	}
	if got := h.Game().Players[0].Life; got != before+3 {
		t.Fatalf("life = %d, want %d (the feasible GainLife)", got, before+3)
	}
}

// The per-Defined$-player path draws once per chooser and never asks.
func TestGenericChoiceAtRandomPerPlayerNeverAsks(t *testing.T) {
	h, c := askFixture(t)
	c.SVars = map[string]string{
		"Two":  "DB$ GainLife | LifeAmount$ 2",
		"Five": "DB$ GainLife | LifeAmount$ 5",
	}
	before := h.Game().Players[0].Life
	Resolve(h, c, &cards.SA{Kind: "DB", API: "GenericChoice", Params: map[string]string{
		"AtRandom": "True", "Defined": "Player", "Choices": "Two,Five",
	}})
	if h.asked != nil {
		t.Fatalf("AtRandom$ True per-player GenericChoice asked: %+v", h.asked)
	}
	if h.n != 2 {
		t.Fatalf("rng draws = %d, want one per chooser (2)", h.n)
	}
	if got := h.Game().Players[0].Life; got != before+4 {
		t.Fatalf("life = %d, want %d (index 0 drawn for each chooser)", got, before+4)
	}
}
