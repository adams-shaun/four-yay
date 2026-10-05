package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Task mvts1's three pins, end to end on real corpus cards. A trigger chain's
// deeper ValidTgts$ sub -- the Execute$ body's SubAbility$ chain -- never got
// a target ask: pushTrigger's placement ask covers only the depth-1 body, so
// the sub either read an empty target set (silent no-op) or inherited the
// outer SA's targets from Ctx.Targets. effects.Resolve's dispatch loop now
// poses a generic pre-ask (chosenTargetsFor) for any ValidTgts$-declaring,
// Defined$-less SA the placement/announcement ask never covered.

// chainAskDeck seats seat 0 a deck opening with fixtures, fills with basics,
// and returns the engine parked at seat 0's Main 1 with the toss forced to
// seat 0.
func chainAskDeck(t *testing.T, reg *cards.Registry, fixtures ...string) *Engine {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 7712, Names: []string{"protagonist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e
}

// TestMoggBombersSubTargetAsked is the CLEAN shape: the ETB trigger's Execute
// body (TrigSac, DB$ Sacrifice) declares NO ValidTgts$ -- no root ask --
// and its DealDamage SUB carries ValidTgts$ Player,Planeswalker. CR 603.3d
// asks that sub target as the trigger is put on the stack (KTarget with
// ResumeKind "trig_sub"), not mid-resolution: the answered player takes the
// damage exactly once and the sacrifice (the chain root) still happened.
func TestMoggBombersSubTargetAsked(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Mogg Bombers", "Grizzly Bears")
	searchMoveByName(t, e, "Mogg Bombers", state.ZBattlefield)
	searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life

	// The bear entering fires Mogg's "when another creature enters" trigger;
	// its Execute body declares no targets, so the FIRST ask anywhere is the
	// sub's own placement one (CR 603.3d).
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("ask = %+v, want the sub's placement KTarget with ResumeKind trig_sub", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("bounds %d..%d, want 1..1", d.Min, d.Max)
	}
	oppIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			oppIdx = o.Index
		}
	}
	if oppIdx < 0 {
		t.Fatalf("the opponent was not offered: %+v", d.Options)
	}
	submitChoices(t, e, oppIdx)

	// The sub's placement answer is delivered at resolution (CR 603.3d);
	// drive the trigger to resolve before asserting its effect.
	drivePastResolution(t, e, 1)

	// Damage once, to the chosen player; the sacrifice ran (chain root).
	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("opponent life %d -> %d, want -3", life1, got)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("own life %d -> %d, want untouched", life0, got)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mogg Bombers" {
			t.Fatalf("Mogg Bombers still on the battlefield (obj %d), want the chain-root sacrifice", id)
		}
	}
}

// TestKorOutfitterAttachSubAsksItsOwnCreature is the CLOBBER shape: the outer
// Execute body (DB$ Pump, ValidTgts$ Equipment.YouCtrl) IS placement-asked,
// and its Attach SUB (Object$ ParentTarget, ValidTgts$ Creature.YouCtrl) must
// pose its own creature ask instead of inheriting the outer equipment target
// (which used to refuse as "cannot attach: no legal target" -- the equipment
// targeting itself). The answered creature is what the equipment attaches to.
func TestKorOutfitterAttachSubAsksItsOwnCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Kor Outfitter", "Sword of Light and Shadow")
	sword := searchMoveByName(t, e, "Sword of Light and Shadow", state.ZBattlefield)
	kor := searchMoveByName(t, e, "Kor Outfitter", state.ZBattlefield)
	if kor == 0 || sword == 0 {
		t.Fatalf("fixtures not in hand: kor %d sword %d", kor, sword)
	}

	// Placement: the outer body's equipment target.
	dt := passUntilAsk(t, e)
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("placement ask = %+v, want the KTarget for the equipment", dt)
	}
	swordIdx := -1
	for _, o := range dt.Options {
		if o.Obj == sword {
			swordIdx = o.Index
		}
	}
	if swordIdx < 0 {
		t.Fatalf("the sword was not offered: %+v", dt.Options)
	}
	submitChoices(t, e, swordIdx)

	// CR 603.5: the optional trigger's election at resolution.
	dopt := passUntilAsk(t, e)
	if dopt == nil || dopt.Kind != decision.KTriggerOptional {
		t.Fatalf("optional ask = %+v, want KTriggerOptional", dopt)
	}
	submitChoices(t, e, 0) // yes

	// The Attach sub's own creature ask -- the ask this task exists for.
	dc := passUntilAsk(t, e)
	if dc == nil || dc.Kind != decision.KChoose || dc.ResumeKind != "tgts" {
		t.Fatalf("sub ask = %+v, want the Attach sub's KChoose with ResumeKind tgts", dc)
	}
	if len(dc.Options) == 0 || dc.Options[0].Obj != kor {
		t.Fatalf("options %+v, want Kor Outfitter offered", dc.Options)
	}
	submitChoices(t, e, dc.Options[0].Index)

	if got := e.G.Obj(sword); got == nil || got.AttachedTo != kor {
		t.Fatalf("sword AttachedTo = %+v, want attached to Kor Outfitter (%d)", got, kor)
	}
	drivePastResolution(t, e, 1)
}

// TestRhinoPutCounterMinZeroDeclineNotReasked is the Min-0 decline: Rhino's
// second sub (DB$ PutCounter, ValidTgts$ Creature.Other, TargetMin$ 0 /
// TargetMax$ 3) poses its own up-to-three ask after the placement-asked
// Destroy; electing ZERO must consume the election once -- no re-posed ask
// -- and the DBPump tail (Defined$ ParentTarget) must still run against the
// OUTER placement target, not the empty election.
func TestRhinoPutCounterMinZeroDeclineNotReasked(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Rhino, Terrible Trampler", "Grizzly Bears",
		"Sword of Light and Shadow")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	sword := searchMoveByName(t, e, "Sword of Light and Shadow", state.ZBattlefield)
	searchMoveByName(t, e, "Rhino, Terrible Trampler", state.ZBattlefield)

	// Placement: the Destroy body's artifact-or-land target.
	dt := passUntilAsk(t, e)
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("placement ask = %+v, want the Destroy KTarget", dt)
	}
	swordIdx := -1
	for _, o := range dt.Options {
		if o.Obj == sword {
			swordIdx = o.Index
		}
	}
	if swordIdx < 0 {
		t.Fatalf("the sword was not offered: %+v", dt.Options)
	}
	submitChoices(t, e, swordIdx)

	// The PutCounter sub's own ask: up to three OTHER creatures, Min 0. CR
	// 603.3d asks it at placement (KTarget/trig_sub), before the Destroy body
	// resolves.
	dc := passUntilAsk(t, e)
	if dc == nil || dc.Kind != decision.KTarget || dc.ResumeKind != "trig_sub" {
		t.Fatalf("sub ask = %+v, want the PutCounter sub's placement KTarget with ResumeKind trig_sub", dc)
	}
	if dc.Min != 0 || dc.Max < 1 || dc.Max > 3 {
		t.Fatalf("bounds %d..%d, want Min 0 with a Max clamped into 1..3", dc.Min, dc.Max)
	}
	if len(dc.Options) == 0 || dc.Options[0].Obj != bear {
		t.Fatalf("options %+v, want the bear offered", dc.Options)
	}
	// Elect ZERO of the up-to-three targets.
	submitChoices(t, e)

	// No re-posed ask anywhere on the way to the opponent's turn; the bear
	// took no counters.
	drivePastResolution(t, e, 1)
	if got := e.G.Obj(bear); got == nil || len(got.Counters) != 0 {
		t.Fatalf("bear = %+v, want untouched by the elected-zero PutCounter", got)
	}
}
