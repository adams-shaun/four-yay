package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestExchangeControlChainPreAskCarriesSecondSide pins the real transport a
// `Defined$ ParentTarget` ExchangeControl sub's own target rides on. Gauntlets
// of Chaos is a two-target exchange across a root SubAbility: the root asks
// for a permanent you control and the DB sub asks for an opponent's permanent
// that shares a type. The sub is a target-REUSE shape, so
// chosenTargetsFor's definedIsTargetReuse guard suppresses the generic
// mid-resolution pre-ask -- the answer exists only in the cast/activation-time
// chain record (collectSubTargetPreAsks -> installSubPreAsk ->
// Ctx.SubPreAsk). Driving the real effects.Resolve walk with that record must
// exchange the two permanents, and the same walk WITHOUT the record must be a
// no-op rather than swapping a duplicated parent target.
func TestExchangeControlChainPreAskCarriesSecondSide(t *testing.T) {
	card := corpusCard(t, "Gauntlets of Chaos")
	face := card.Faces[0]
	root := face.Abilities[0]
	sub := cards.ResolveSVar(face.SVars, "DBExchange")
	// Preconditions: the fixture really is the chain shape this test names.
	if root == nil || sub == nil {
		t.Fatal("precondition: Gauntlets of Chaos lost its root ability or DBExchange SVar")
	}
	if sub.API != "ExchangeControl" || sub.Params["Defined"] != "ParentTarget" || sub.Params["ValidTgts"] == "" {
		t.Fatalf("precondition: DBExchange is not the ParentTarget+ValidTgts exchange: %+v", sub)
	}
	collected := false
	for _, sa := range newSeats(t, 2).collectSubTargetPreAsks(root) {
		if sa.Line == sub.Line {
			collected = true
		}
	}
	if !collected {
		t.Fatal("precondition: collectSubTargetPreAsks dropped the exchange sub, so no cast-time answer can exist")
	}

	e := newSeats(t, 2)
	src := onBoardCard(t, e, 0, card)
	mine := putBattlefield(t, e, 0, "Name:Mine\nTypes:Creature Artifact\nPT:1/1\nOracle:x\n")
	theirs := putBattlefield(t, e, 1, "Name:Theirs\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(mine).Controller != 0 || e.G.Obj(theirs).Controller != 1 {
		t.Fatal("precondition: fixture requires the pair to start on opposite seats")
	}
	if e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatal("precondition: exchange source must be a battlefield permanent")
	}

	// The no-record pass first: with no PickedTargets and no SubPreAsk entry,
	// the handler must not duplicate the parent target into the second side.
	noop := &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars,
		Targets: []state.Target{{Obj: mine}}}
	effects.Resolve(e, noop, sub)
	if e.G.Obj(mine).Controller != 0 || e.G.Obj(theirs).Controller != 1 {
		t.Fatalf("exchange without a second-side answer changed control: mine=%d theirs=%d",
			e.G.Obj(mine).Controller, e.G.Obj(theirs).Controller)
	}

	// The real record: the cast-time chain answer for exactly this sub line.
	ctx := &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars,
		Targets:   []state.Target{{Obj: mine}},
		SubPreAsk: map[string][]state.Target{sub.Line: {{Obj: theirs}}}}
	effects.Resolve(e, ctx, sub)
	if e.G.Obj(mine).Controller != 1 || e.G.Obj(theirs).Controller != 0 {
		t.Fatalf("chained exchange controllers = mine:%d theirs:%d, want 1,0 (SubPreAsk answer not honoured)",
			e.G.Obj(mine).Controller, e.G.Obj(theirs).Controller)
	}
	// RememberExchanged$ True is consumed by the chain's own DBDestroyAll /
	// DBCleanup tail (which clears the list by design), so it is asserted in
	// the effects-level TestExchangeControlGauntletsRememberedFollowUp rather
	// than here.
}
