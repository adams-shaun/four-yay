// keyword_cost_param_rules_test.go — the two live charge bugs from handing
// ParseCost the whole KeywordParam remainder of a cost-bearing keyword.
//
// Forge's KeywordWithCost reads a cost as the FIRST colon-field
// (KeywordWithCost.java:22), so "Disguise:5 R:X:This cost is reduced by …"
// costs {5}{R} and "Flashback:8 U U:ReduceCost$ X:…" costs {8}{U}{U}. Before
// cards.Face.KeywordCostParam these two readers saw the trailing fields too:
// Fugitive Codebreaker's Disguise turn-up parsed as 20 phantom generic plus
// Unknown -> morphFaceUpCost failed closed and withheld the special action, and
// Visions of Duplicity's flashback cost parsed as 36 generic + {U}.
//
// Both carriers are real corpus cards (no Forge text committed here). The
// helpers come from morph_turnup_test.go, morph_test.go, manifest_test.go,
// cast_test.go and search_library_test.go.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestFugitiveCodebreakerDisguiseTurnFaceUpOffersAndChargesFullCost is the
// ticket's primary symptom: a disguised Fugitive Codebreaker on the
// battlefield offers the turn_face_up special action once the full printed
// {5}{R} is payable, and choosing it pays exactly {5}{R} and flips the card
// face up. The ReduceCost$ rider ("{1} less per instant/sorcery in your
// graveyard") is deliberately not honoured -- there is no mana discount here.
func TestFugitiveCodebreakerDisguiseTurnFaceUpOffersAndChargesFullCost(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Fugitive Codebreaker")
	// Fund {3} for the face-down cast and nothing more (RRR), so the pool is
	// empty after it and no {5}{R} turn-up is payable yet.
	id := morphDownCast(t, e, "Fugitive Codebreaker", "disguised", "RRR", 0)

	// Preconditions: the real must differ from the compared value -- the
	// card is on the battlefield face down, and the printed Disguise cost
	// parses to the {5}{R} charge (not the polluted 20-generic parse).
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || !o.FaceDown || o.Controller != 0 {
		t.Fatalf("precondition: Fugitive Codebreaker zone=%s faceDown=%v controller=%d, want a face-down battlefield object seat 0",
			o.Zone, o.FaceDown, o.Controller)
	}
	f := o.Face()
	raw, ok := f.KeywordCostParam("Disguise")
	if !ok || raw != "5 R" {
		t.Fatalf("precondition: KeywordCostParam(Disguise) = %q %v, want %q", raw, ok, "5 R")
	}
	if whole, ok := f.KeywordParam("Disguise"); !ok || whole == "5 R" {
		t.Fatalf("precondition: KeywordParam(Disguise) = %q %v, want the whole remainder (so the two reads differ)", whole, ok)
	}
	mf, ok := morphFaceUpCost(o)
	if !ok || mf.cost.Generic != 5 || mf.cost.Colored[state.MR] != 1 || len(mf.cost.Unknown) != 0 {
		t.Fatalf("precondition: morphFaceUpCost = %+v ok=%v, want generic 5 + one red, no Unknown", mf.cost, ok)
	}

	// The pool is empty, so the turn-up must not be offered yet.
	if turnFaceUpOptionPresent(t, e, id) {
		t.Fatal("turn_face_up offered with an empty pool, want withheld")
	}

	// Fund exactly {5}{R} (6 mana); now the offer must appear and the action
	// pay it.
	addMana(t, e, 0, "RRRRRR")
	if !turnFaceUpOptionPresent(t, e, id) {
		t.Fatal("turn_face_up not offered after funding {5}{R}")
	}
	mark := len(e.L.Events)
	before := e.G.Players[0].Pool.Total()
	submitChoices(t, e, turnFaceUpIndex(t, e, id))

	// The turn-up is a special action: the CARD itself never reaches the
	// stack (assertTurnUpEventOnce checks no PutOnStack for id). Fugitive's
	// own TurnFaceUp trigger may sit on the stack afterwards.
	assertTurnUpEventOnce(t, e, id, mark)
	if got, want := e.G.Players[0].Pool.Total(), before-6; got != want {
		t.Fatalf("pool after turn-up = %d, want %d (exactly the printed {5}{R})", got, want)
	}
	if o := e.G.Obj(id); o.FaceDown {
		t.Fatal("Fugitive Codebreaker FaceDown=true after the turn-up, want false")
	}
	replayCheck(t, e, cfg)
}

// TestVisionsOfDuplicityFlashbackCostIsPrintedNotPolluted pins the second live
// charge bug: the flashback cost parses as {8}{U}{U}, not the polluted
// 36-generic + {U} the whole-remainder read produced.
func TestVisionsOfDuplicityFlashbackCostIsPrintedNotPolluted(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Visions of Duplicity")
	// The flashback permission is read off the card in the graveyard.
	id := searchMoveByName(t, e, "Visions of Duplicity", state.ZGraveyard)

	o := e.G.Obj(id)
	if o.Zone != state.ZGraveyard || o.Face() == nil {
		t.Fatalf("precondition: Visions of Duplicity zone=%s face=%v, want graveyard with a face", o.Zone, o.Face())
	}
	f := o.Face()
	raw, ok := f.KeywordCostParam("Flashback")
	if !ok || raw != "8 U U" {
		t.Fatalf("precondition: KeywordCostParam(Flashback) = %q %v, want %q", raw, ok, "8 U U")
	}
	if whole, ok := f.KeywordParam("Flashback"); !ok || whole == "8 U U" {
		t.Fatalf("precondition: KeywordParam(Flashback) = %q %v, want the whole remainder (so the two reads differ)", whole, ok)
	}

	c := e.flashbackCost(id)
	if c.Generic != 8 || c.Colored[state.MU] != 2 {
		t.Fatalf("flashbackCost = generic %d coloured %v, want {8}{U}{U}", c.Generic, c.Colored)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("flashbackCost reported Unknown %v, want none", c.Unknown)
	}
	// The cost string the offer/charge path uses is the same printed field.
	if got, want := e.flashbackCostString(f), "8 U U"; got != want {
		t.Fatalf("flashbackCostString = %q, want %q", got, want)
	}
	replayCheck(t, e, cfg)
}

// TestKeywordCostParamFlashbackReaderLeavesOtherHeadsAlone is the boundary
// check asked for by the brief: the new accessor is a KeywordWithCost read,
// and the heads Forge leads with a value rather than a cost (Suspend,
// Impending, Kicker) keep their own readers. It asserts the accessor would
// return the wrong field for those lines, which is exactly why they must not
// call it, and that the real readers still resolve the cost after the change.
func TestKeywordCostParamFlashbackReaderLeavesOtherHeadsAlone(t *testing.T) {
	t.Parallel()
	c, _ := cards.ParseBytes("k.txt", []byte(
		"Name:K\nTypes:Sorcery\n"+
			"K:Suspend:5:W\n"+
			"K:Impending:4:2 B\n"+
			"K:Kicker:1 R:1 G\n"+
			"Oracle:x\n"))
	f := c.Faces[0]
	// The accessor's first-field read is the WRONG field for these families --
	// the value, not the cost -- which is the documented reason each has its
	// own reader (rules/cast_altcost.go suspendInfo / keywordAltCost /
	// twoPartKickerCosts).
	if got, _ := f.KeywordCostParam("Suspend"); got != "5" {
		t.Fatalf("KeywordCostParam(Suspend) = %q, want the leading time field %q (so Suspend must not use it)", got, "5")
	}
	if got, _ := f.KeywordCostParam("Impending"); got != "4" {
		t.Fatalf("KeywordCostParam(Impending) = %q, want the leading count field %q (so Impending must not use it)", got, "4")
	}
	one, two, both := twoPartKickerCosts(f)
	if !both || one.Generic != 1 || one.Colored[state.MR] != 1 || two.Generic != 1 || two.Colored[state.MG] != 1 {
		t.Fatalf("twoPartKickerCosts = one %+v two %+v both %v, want {1}{R} + {1}{G}", one, two, both)
	}
}
