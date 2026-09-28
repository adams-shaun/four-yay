package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// balloonGoblin is the reported no-op activation shape: an activated ability
// whose whole effect is an idempotent keyword grant ({R}: gains flying until
// end of turn), the exact Goblin Balloon Brigade the user saw on the stack
// three times.
func balloonGoblin(t *testing.T) *Engine {
	t.Helper()
	gb := card(t, "Name:Goblin Balloon Brigade\nManaCost:R\nTypes:Creature Goblin Warrior\nPT:1/1\n"+
		"A:AB$ Pump | Cost$ R | KW$ Flying | Defined$ Self | SpellDescription$ CARDNAME gains flying until end of turn.\nOracle:x\n")
	e := handEngine(t)
	o := e.G.AddObject(gb, 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	e.G.Players[0].Pool[state.MR] = 1
	return e
}

func keywordGrantOption(opts []decision.Option) *decision.Option {
	for i := range opts {
		if opts[i].Kind == "ability" {
			return &opts[i]
		}
	}
	return nil
}

// TestAbilityGrantIdempotentKeywordFlag pins the engine-side half of the B1
// no-op: legalActions fills the ability option's Grant flag for an
// activation whose whole effect is a pure idempotent keyword grant, so the
// bot policy (ability.go's grantNoOp) can see it. A fresh balloon brigade
// grants flying it does not yet have and has no identical activation pending,
// so Grant is non-nil with Keywords {Flying} and both redundant halves false.
func TestAbilityGrantIdempotentKeywordFlag(t *testing.T) {
	t.Parallel()
	e := balloonGoblin(t)
	op := keywordGrantOption(e.legalActions(0))
	if op == nil {
		t.Fatal("no ability option offered for the balloon brigade")
	}
	if op.Grant == nil {
		t.Fatal("a pure keyword-grant ability must carry a grant flag, found nil")
	}
	if len(op.Grant.Keywords) != 1 || op.Grant.Keywords[0] != "Flying" {
		t.Fatalf("grant keywords = %v, want [Flying]", op.Grant.Keywords)
	}
	if op.Grant.Already || op.Grant.Duplicate {
		t.Fatalf("fresh grant must be neither already-in-effect nor duplicated: %+v", op.Grant)
	}
}

// TestAbilityGrantAlreadyInEffect pins the resolved-board half: when the
// granting permanent already has the granted keyword (an earlier resolution
// this turn gave it flying), the Grant flag's Already half is true, so the
// bot declines a second activation that gains nothing. The gate removes the
// HasKeyword read in abilityGrant (and this test fails: Already stays false).
func TestAbilityGrantAlreadyInEffect(t *testing.T) {
	t.Parallel()
	e := balloonGoblin(t)
	o := e.G.Zone(state.ZBattlefield, 0)[0]
	e.AddContinuous(ContinuousEffect{Source: o, Timestamp: 1, Layer: LAbilities,
		Affects: "Card.Self", Controller: 0, AddKeywords: []string{"Flying"}})
	if !e.HasKeyword(o, "Flying") {
		t.Fatal("test setup: the brigade should see flying granted")
	}
	op := keywordGrantOption(e.legalActions(0))
	if op == nil || op.Grant == nil {
		t.Fatal("expected a grant flag on the ability option")
	}
	if !op.Grant.Already {
		t.Fatalf("grant Already = false, want true — the brigade already flies, so re-granting gains nothing")
	}
}

// TestAbilityGrantDuplicatePending pins the STACK half — the half that
// actually failed in the reported game: when an identical activation from
// the same source is already on the stack unresolved, the Grant flag's
// Duplicate half is true even though the permanent does not yet have the
// keyword. The gate removes the grantPending stack walk (and this test
// fails: Duplicate stays false).
func TestAbilityGrantDuplicatePending(t *testing.T) {
	t.Parallel()
	e := balloonGoblin(t)
	o := e.G.Zone(state.ZBattlefield, 0)[0]
	src := e.G.Obj(o)
	// Mint an ability object on the stack from the same source, carrying the
	// same pure keyword-grant SA — exactly what an earlier activation pushed.
	ab := e.G.AddObject(nil, 0)
	ab.Ability = src.Face().Abilities[0]
	ab.Source = o
	events.Move(e.G, ab.ID, state.ZLibrary, state.ZStack)

	// The brigade does NOT have flying yet, so Already must be false — only
	// the pending duplicate marks it redundant.
	if e.HasKeyword(o, "Flying") {
		t.Fatal("test setup: the brigade must not already fly")
	}
	op := keywordGrantOption(e.legalActions(0))
	if op == nil || op.Grant == nil {
		t.Fatal("expected a grant flag on the ability option")
	}
	if !op.Grant.Duplicate {
		t.Fatalf("grant Duplicate = false, want true — an identical activation is already on the stack")
	}
	if op.Grant.Already {
		t.Fatalf("grant Already = true, want false (the brigade does not yet fly; the duplicate is the marker)")
	}
}
