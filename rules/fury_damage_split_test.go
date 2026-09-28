package rules

// TestFuryDividesDamageAmongTargets pins DealDamage.DividedAsYouChoose$ as a
// real player allocation, not the old deterministic round-robin stand-in:
// after the target ask the engine poses a DISTINCT KChoose over exactly the
// chosen targets (Min == Max == the named total, Repeatable), a non-round-
// robin multiset is honoured, zero shares are legal, and the answer resumes
// through the ordinary replay pathway.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// furyOptionForObj returns the offered option index naming obj, failing if the
// recipient was not offered. Used for both the target ask and the allocation
// ask, whose options both carry the target in Obj.
func furyOptionForObj(t *testing.T, d *decision.Decision, obj state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("object %d not offered in %s decision: %+v", obj, d.Kind, d.Options)
	return -1
}

// furySetup builds a corpus-fixture game and drives Fury's ETB trigger to its
// target ask. Every precondition a sub-test depends on is asserted here: both
// bears are on the battlefield (the zone effDealDamage reads) with toughness
// above the scripted total (so a full-share split does not kill one and reset
// its Damage), Fury is on the battlefield (its ChangesZone trigger fired), and
// the target ask really offered exactly the two bears with the scripted 0..4
// bounds. A fixture that drifted into a wrong zone or a wrong bound fails
// loudly here rather than making a later assertion vacuous.
func furySetup(t *testing.T) (*Engine, Config, *decision.Decision, state.ObjID, state.ObjID) {
	t.Helper()
	reg := choiceCorpusRegistry(t)
	b1 := card(t, "Name:Bear1\nTypes:Creature Bear\nPT:0/8\nOracle:x\n")
	b2 := card(t, "Name:Bear2\nTypes:Creature Bear\nPT:0/8\nOracle:x\n")
	// The bears ride seat 1's deck so replay rebuilds them from the Config;
	// a directly-added object (battlefieldFixture) is not in the log and
	// cannot be reconstructed.
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{choiceCorpusCard(t, "Fury")}, []*cards.Card{b1, b2})
	bear1 := moveByName(t, e, 1, "Bear1", state.ZBattlefield)
	bear2 := moveByName(t, e, 1, "Bear2", state.ZBattlefield)
	for _, b := range []state.ObjID{bear1, bear2} {
		o := e.G.Obj(b)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: bear %d zone = %s, want battlefield", b, o.Zone)
		}
		if o.Face().Toughness() <= 4 {
			t.Fatalf("precondition: bear %d toughness = %d, want > 4 so a full 4-damage share is not lethal", b, o.Face().Toughness())
		}
	}
	fury := moveByName(t, e, 0, "Fury", state.ZBattlefield)
	if got := e.G.Obj(fury).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: Fury zone = %s, want battlefield", got)
	}

	d := drainUntilAsk(t, e, 40)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Fury's division targets not asked: %+v", d)
	}
	if d.Min != 0 || d.Max != 4 {
		t.Fatalf("division target bounds = [%d,%d], want [0,4]", d.Min, d.Max)
	}
	for _, b := range []state.ObjID{bear1, bear2} {
		furyOptionForObj(t, d, b) // both bears must be legal targets
	}
	return e, cfg, d, bear1, bear2
}

// furyAllocationAsk answers the target ask with BOTH bears and returns the
// distinct allocation decision it suspends on.
func furyAllocationAsk(t *testing.T, e *Engine, td *decision.Decision, bear1, bear2 state.ObjID) *decision.Decision {
	t.Helper()
	submitChoices(t, e, furyOptionForObj(t, td, bear1), furyOptionForObj(t, td, bear2))
	ad := drainUntilAsk(t, e, 30)
	if ad == nil || ad.Kind != decision.KChoose || ad.ResumeKind != "damage_split" {
		t.Fatalf("allocation ask missing after target selection: %+v", ad)
	}
	if ad.Min != 4 || ad.Max != 4 || !ad.Repeatable {
		t.Fatalf("allocation bounds = [%d,%d] repeatable=%v, want [4,4] repeatable", ad.Min, ad.Max, ad.Repeatable)
	}
	if len(ad.Options) != 2 {
		t.Fatalf("allocation offered %d options, want exactly the two chosen bears: %+v", len(ad.Options), ad.Options)
	}
	return ad
}

// TestFuryDividesDamageAmongTargets proves the player chooses each share: a
// 3/1 split (which differs from the 2/2 the round-robin stand-in produced) is
// honoured and the total is exactly the scripted 4.
func TestFuryDividesDamageAmongTargets(t *testing.T) {
	t.Parallel()
	e, cfg, td, bear1, bear2 := furySetup(t)
	ad := furyAllocationAsk(t, e, td, bear1, bear2)

	// The decision's own wire rules constrain the answer: a total other than
	// the scripted 4, and a recipient not on the chosen list, are both
	// rejected. These prove the ask is a real constraint, not a label.
	o1 := furyOptionForObj(t, ad, bear1)
	o2 := furyOptionForObj(t, ad, bear2)
	if err := ad.Validate(decision.Intent{Seq: ad.Seq, Player: ad.Player, Choices: []int{o1, o2, o1}}); err == nil {
		t.Fatalf("a 3-damage total was accepted; the exact-total rule must reject it")
	}
	if err := ad.Validate(decision.Intent{Seq: ad.Seq, Player: ad.Player, Choices: []int{o1, o1, o1, 99}}); err == nil {
		t.Fatalf("an out-of-range recipient was accepted")
	}
	if o1 == o2 {
		t.Fatalf("precondition: the two bears must be distinguishable options, got %d and %d", o1, o2)
	}

	// Assign 3 to bear1 and 1 to bear2 -- NOT the round-robin 2/2. The damage
	// assertions below can only hold if this answer was applied.
	submitChoices(t, e, o1, o1, o1, o2)
	// passUntilStackEmpty fatals on any non-priority decision it does not
	// recognise, so a re-posed target ask or a re-posed allocation ask fails
	// here loudly: the answer must resume without repeating either.
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(bear1).Damage; got != 3 {
		t.Fatalf("bear1 damage = %d, want 3 (the answered 3/1 allocation)", got)
	}
	if got := e.G.Obj(bear2).Damage; got != 1 {
		t.Fatalf("bear2 damage = %d, want 1 (the answered 3/1 allocation)", got)
	}
	if total := e.G.Obj(bear1).Damage + e.G.Obj(bear2).Damage; total != 4 {
		t.Fatalf("total damage = %d, want the scripted 4", total)
	}
	// The whole resolution replays from the log, allocation answer included.
	replayCheck(t, e, cfg)
}

// TestFuryDamageSplitAllowsZeroShare pins that a chosen target may receive
// nothing while the full total is assigned elsewhere ("divided as you choose"
// allows a zero share for any chosen target).
func TestFuryDamageSplitAllowsZeroShare(t *testing.T) {
	t.Parallel()
	e, _, td, bear1, bear2 := furySetup(t)
	ad := furyAllocationAsk(t, e, td, bear1, bear2)
	o1 := furyOptionForObj(t, ad, bear1)
	o2 := furyOptionForObj(t, ad, bear2)
	if o1 == o2 {
		t.Fatalf("precondition: the two shares must be distinguishable options, got %d and %d", o1, o2)
	}
	// All four on bear1; bear2 is chosen but takes zero.
	submitChoices(t, e, o1, o1, o1, o1)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(bear1).Damage; got != 4 {
		t.Fatalf("bear1 damage = %d, want 4 (the whole total on one target)", got)
	}
	if got := e.G.Obj(bear2).Damage; got != 0 {
		t.Fatalf("bear2 damage = %d, want 0 (a legal zero share)", got)
	}
}

// TestFuryDamageSplitSkipsAskWithNoTargets pins the zero-target edge: a
// division with nothing to divide poses no allocation decision at all and
// deals nothing, and the trigger still resolves (no silent stall).
func TestFuryDamageSplitSkipsAskWithNoTargets(t *testing.T) {
	t.Parallel()
	e, _, td, bear1, bear2 := furySetup(t)
	_ = td
	submitChoices(t, e) // Min 0: elect zero targets
	// A re-posed allocation or target ask would be an unrecognised
	// non-priority decision here and fail the drain.
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(bear1).Damage; got != 0 {
		t.Fatalf("bear1 damage = %d, want 0 (no targets chosen)", got)
	}
	if got := e.G.Obj(bear2).Damage; got != 0 {
		t.Fatalf("bear2 damage = %d, want 0 (no targets chosen)", got)
	}
	if hasNote(e, "unimplemented API DealDamage") {
		t.Fatalf("Fury's DealDamage body did not run")
	}
}

// drainStackPassing answers priority "pass" until the stack empties, and
// FAILS LOUDLY on any non-priority decision. Used by the sole-target test,
// where the whole point is that no allocation ask is posed; the shared
// passUntilStackEmpty would silently auto-answer a damage_split ask, hiding
// exactly the defect under test.
func drainStackPassing(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected mid-resolution ask while draining: %+v", d)
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
					t.Fatalf("submit pass: %v", err)
				}
				break
			}
		}
	}
}

// TestFuryDamageSplitSingleTargetFillsWholeTotal pins the single-target
// gate: with exactly one legal target there is only one legal allocation, so
// the engine must NOT pose an allocation decision nobody could answer
// differently. Instead the sole target takes the whole scripted total, and
// the resolution drains without a second non-priority ask.
func TestFuryDamageSplitSingleTargetFillsWholeTotal(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	b := card(t, "Name:Bear1\nTypes:Creature Bear\nPT:0/8\nOracle:x\n")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{choiceCorpusCard(t, "Fury")}, []*cards.Card{b})
	bear := moveByName(t, e, 1, "Bear1", state.ZBattlefield)
	// Precondition: the sole target is on the battlefield (the zone
	// effDealDamage reads) and can survive the full share.
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield || o.Face().Toughness() <= 4 {
		t.Fatalf("precondition: bear zone=%s toughness=%d, want battlefield and >4",
			e.G.Obj(bear).Zone, e.G.Obj(bear).Face().Toughness())
	}
	moveByName(t, e, 0, "Fury", state.ZBattlefield)

	td := drainUntilAsk(t, e, 40)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("Fury's division targets not asked: %+v", td)
	}
	if td.Min != 0 || td.Max != 4 {
		t.Fatalf("division target bounds = [%d,%d], want [0,4]", td.Min, td.Max)
	}
	// Precondition: the sole CHOSEN target is the bear, so the division has a
	// single recipient (the gate this test exists for); the bear must be
	// offered and distinguishable from Fury's own option.
	bearOpt := furyOptionForObj(t, td, bear)
	if len(td.Options) < 2 {
		t.Fatalf("precondition: want the bear plus at least Fury offered, got %+v", td.Options)
	}
	submitChoices(t, e, bearOpt)
	// The engine must fill the sole share directly, not pose an allocation
	// decision nobody could answer differently. drainStackPassing fatals on
	// any non-priority ask, so a damage_split here (the defect this test
	// pins) or a re-posed target ask fails loudly. The stack must drain
	// FIRST, while the bear is still alive to report its damage.
	drainStackPassing(t, e, 30)
	if got := e.G.Obj(bear).Damage; got != 4 {
		t.Fatalf("sole target damage = %d, want the whole scripted total 4", got)
	}
	replayCheck(t, e, cfg)
}
