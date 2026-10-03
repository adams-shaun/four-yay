package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// realTgtChooseSA loads a REAL compiled corpus card and returns the first
// Discard SA on it whose Mode$ is TgtChoose. The brief forbids hand-written
// map[string]string fixtures because one silently passed while the real
// Wrath of God failed on an unread parameter; these tests therefore pin the
// behaviour on an SA the corpus actually compiles, and name the card.
func realTgtChooseSA(t *testing.T, cardName string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(cardName)
	if !ok {
		t.Fatalf("corpus has no card %q", cardName)
	}
	for _, f := range c.Faces {
		if sa := findDiscardMode(f, "TgtChoose"); sa != nil {
			return sa
		}
	}
	t.Fatalf("card %q has no TgtChoose Discard ability", cardName)
	return nil
}

// findDiscardMode walks every ability position (printed abilities, trigger
// effects, replacement effects and SVar-referenced bodies) and returns the
// first SA whose API is Discard and whose Mode$ param equals mode. This is
// the same traversal cards.Face.Primitives uses, so a Discard reached only
// through an on-demand SVar (a Charm or Repeat choice) is still found.
func findDiscardMode(f *cards.Face, mode string) *cards.SA {
	var walk func(sa *cards.SA, d int) *cards.SA
	walk = func(sa *cards.SA, d int) *cards.SA {
		if sa == nil || d > 32 {
			return nil
		}
		if sa.API == "Discard" && sa.Params["Mode"] == mode {
			return sa
		}
		return walk(sa.Sub, d+1)
	}
	for _, sa := range f.Abilities {
		if got := walk(sa, 0); got != nil {
			return got
		}
	}
	for _, tr := range f.Triggers {
		if got := walk(tr.Effect, 0); got != nil {
			return got
		}
	}
	for _, rp := range f.Repls {
		if got := walk(rp.With, 0); got != nil {
			return got
		}
	}
	return nil
}

// suspendHost is a fakeHost whose Ask records the posed decision, returns
// true and — unlike askHost — leaves the host Suspended, which is the
// engine contract effects.Resolve relies on to stop descending into a
// chained SubAbility after an ask. It is the effects-package stand-in for
// rules.Engine: the test clears Suspended and attaches the answer to the
// Ctx (Ctx.Discard) to simulate the resume pass.
type suspendHost struct {
	fakeHost
	asked     *decision.Decision
	suspended bool
}

func (h *suspendHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asked = &cp
	h.suspended = true
	return true
}

func (h *suspendHost) Suspended() bool { return h.suspended }

// countLifeLoss reports how many negative LifeChange events the target seat
// received — the "the sub-ability ran N times" metric, mirroring
// rules/discard_subability_test.go's countLoseLife.
func countLifeLoss(h *fakeHost, p state.PlayerID) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.LifeChange && ev.Player == p && ev.Amount < 0 {
			n++
		}
	}
	return n
}

// TestDiscardTgtChooseFallsBackToFrontEligibleWhenHostCannotAsk is R-9's
// no-engine contract for the TgtChoose shape: a host whose Ask returns false
// must still resolve deterministically — take the FRONT OF THE FILTERED hand
// (the eligible cards), not an arbitrary card — and record the Note that
// names the stand-in. Mind Rot's DiscardValid is empty (default "Card"), so
// the front two eligible cards are the front two hand cards.
func TestDiscardTgtChooseFallsBackToFrontEligibleWhenHostCannotAsk(t *testing.T) {
	sa := realTgtChooseSA(t, "Mind Rot")
	h, c, ids := discardBoard(t, creature(t, "Frog"), creature(t, "Bird"), creature(t, "Cat"))
	// Plain fakeHost: Ask returns false.
	plain := &fakeHost{g: h.g, log: h.log}

	effDiscard(plain, c, sa)

	if !inZone(plain.g, state.ZGraveyard, 1, ids[0]) {
		t.Fatal("no-ask host did not discard the front eligible card (frog)")
	}
	if !inZone(plain.g, state.ZGraveyard, 1, ids[1]) {
		t.Fatal("no-ask host did not discard the second eligible card (bird)")
	}
	if inZone(plain.g, state.ZGraveyard, 1, ids[2]) {
		t.Fatal("no-ask host discarded a third card — more than NumCards$ 2")
	}
	foundNote := false
	for _, ev := range plain.log {
		if ev.Kind == events.Note && ev.Text == "discards its first card (no engine host to ask)" {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatal("no-ask fallback did not record the stand-in Note")
	}
}

// TestDiscardTgtChooseFewerEligibleThanNumCardsAsksNothing pins the "no real
// choice" contract: when the hand holds fewer DiscardValid$-eligible cards
// than NumCards$ demands, the effect discards what the player owns and asks
// no decision — a decision nobody could answer differently would be noise.
//
// This is pinned on a real corpus card with a non-trivial DiscardValid$
// filter (Gutmorn, Pactbound Servant: DiscardValid$ Card.nonLand, Defined$
// Player), because it is the honest way to make the two implementations
// differ: the old default took the raw front-of-hand card (here a land, which
// is not eligible), whereas the new path respects the filter and discards the
// single eligible creature while asking nothing.
func TestDiscardTgtChooseFewerEligibleThanNumCardsAsksNothing(t *testing.T) {
	sa := realTgtChooseSA(t, "Gutmorn, Pactbound Servant")
	ah, _, ids := discardBoard(t, land(t, "Islet"), creature(t, "Frog"))

	// Ctx.Controller is seat 0 but Gutmorn's Defined$ Player is
	// controller-relative; only seat 1 is given a hand in this board, so the
	// observable target is seat 1. NumCards$ is absent for Gutmorn (default
	// 1) and its hand holds exactly one eligible non-land, so no choice is a
	// real one and nothing must be asked.
	effDiscard(ah, &Ctx{Source: 1, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa)

	if ah.asked != nil {
		t.Fatal("TgtChoose asked a decision when the hand had no real choice")
	}
	if !inZone(ah.g, state.ZGraveyard, 1, ids[1]) {
		t.Fatal("the single eligible non-land (frog) was not discarded")
	}
	if !inZone(ah.g, state.ZHand, 1, ids[0]) {
		t.Fatal("a land was discarded despite DiscardValid$ Card.nonLand — the filter was ignored")
	}
}
