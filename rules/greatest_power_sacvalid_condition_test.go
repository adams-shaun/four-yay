package rules

// Regression coverage for the two `greatestPower` filter seams the round-t2
// review measured as still unbound: effSacrifice's SacValid$ pool walk
// (effects/zone.go) and conditionMet's defined-group walk /
// conditionNotPresentMet (effects/conditions.go). Both compare a candidate
// against its peers, so a continuous pump on a SMALLER creature must be able
// to displace the printed-greatest one. Each test asserts its own
// precondition (both creatures on the battlefield, derived and printed
// orderings in opposite directions) so a vacuous setup fails loudly.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGreatestPowerSacValidPoolReadsDerivedPower drives Consume's exact
// Sacrifice shape (`ValidTgts$ Player | SacValid$
// Creature.greatestPowerControlledByTargeted`) through effSacrifice's pool
// walk with a real LPT/SubModify +5/+5 pump on the 2/2 Grizzly Bears. The
// pumped bear is the derived-greatest (7 vs the Hill Giant's 3), so the pool
// must admit only the bear and sacrifice it. Before the fix the pool sized
// the comparison at printed-plus-counter power, admitted the printed-greatest
// Hill Giant and sacrificed it instead.
func TestGreatestPowerSacValidPoolReadsDerivedPower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	giantCard := mustCorpusCard(t, reg, "Hill Giant")
	bearCard := mustCorpusCard(t, reg, "Grizzly Bears")
	e := New(Config{Seed: 12, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(giantCard, 0)
	giant := e.G.AddObject(giantCard, 1)
	bear := e.G.AddObject(bearCard, 1)
	for _, o := range []*state.Object{src, giant, bear} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	e.AddContinuous(state.ContinuousEffect{Source: bear.ID, Controller: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.Self", AddPower: 5, AddToughness: 5})
	// Preconditions: both comparison creatures are battlefield permanents,
	// the pump really moved the derived read, and the printed faces still
	// order the other way -- otherwise the assertion cannot distinguish
	// derived from printed.
	if e.G.Obj(giant.ID).Zone != state.ZBattlefield || e.G.Obj(bear.ID).Zone != state.ZBattlefield {
		t.Fatal("precondition: both comparison creatures must be on the battlefield")
	}
	if e.Power(bear.ID) != 7 || e.Power(giant.ID) != 3 {
		t.Fatalf("precondition: derived powers bear=%d giant=%d, want 7 and 3",
			e.Power(bear.ID), e.Power(giant.ID))
	}
	if bp, gp := e.G.Obj(bear.ID).Face().Power(), e.G.Obj(giant.ID).Face().Power(); bp >= gp {
		t.Fatalf("precondition: printed powers bear=%d giant=%d, want bear < giant", bp, gp)
	}
	// Consume's own ability line, as an SA: the target player sacrifices the
	// creature with the greatest power among creatures they control.
	sa := &cards.SA{Kind: "SP", API: "Sacrifice", Line: "test:Consume", Params: map[string]string{
		"ValidTgts": "Player",
		"SacValid":  "Creature.greatestPowerControlledByTargeted",
	}}
	// The cast-time target answer, as the engine's own pre-ask would record
	// it (Ctx.SubPreAsk keyed by the SA line): the target player.
	ctx := &effects.Ctx{Source: src.ID, Controller: 0,
		Targets:   []state.Target{{Player: 1, IsPlayer: true}},
		SubPreAsk: map[string][]state.Target{sa.Line: {{Player: 1, IsPlayer: true}}}}
	effects.Resolve(e, ctx, sa)
	if e.resume != nil {
		e.resume.outer = e.buildContinuationChain(e.contChain, src.ID, nil)
	}
	if z := e.G.Obj(bear.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("derived-greatest Grizzly Bears zone = %s, want Graveyard (it must be the sacrifice)", z)
	}
	if z := e.G.Obj(giant.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("Hill Giant zone = %s, want Battlefield (printed-greatest must not be chosen)", z)
	}
}

// TestGreatestPowerConditionDefinedGroupReadsDerivedPower drives Getaway
// Glamer's destroy condition (`ConditionDefined$ Targeted | ConditionPresent$
// Creature.greatestPower`) with a real LPT/SubModify +5/+5 pump on the 2/2
// Grizzly Bears so the pumped bear (derived 7) outranks the targeted 6/6
// Colossal Dreadmaw. The condition means "no other creature has greater
// power", so it must FAIL and the Dreadmaw must survive; before the fix the
// defined-group walk matched members with an unbound context, read the bear
// at printed 2, and destroyed the Dreadmaw. The unpumped positive control
// proves the resolution really reaches effDestroy and the condition really
// gates it (a test that passes only because the pre-ask suspended would be
// vacuous).
func TestGreatestPowerConditionDefinedGroupReadsDerivedPower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	srcCard := mustCorpusCard(t, reg, "Hill Giant")
	bearCard := mustCorpusCard(t, reg, "Grizzly Bears")
	bigCard := mustCorpusCard(t, reg, "Colossal Dreadmaw")

	// board builds the two-creature board, optionally pumping the bear, and
	// resolves Getaway Glamer's destroy-mode SA targeting the Dreadmaw.
	board := func(pump bool) (*Engine, state.ObjID, state.ObjID) {
		t.Helper()
		e := New(Config{Seed: 12, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
		src := e.G.AddObject(srcCard, 0)
		bear := e.G.AddObject(bearCard, 1)
		big := e.G.AddObject(bigCard, 1)
		for _, o := range []*state.Object{src, bear, big} {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		}
		if e.G.Obj(bear.ID).Zone != state.ZBattlefield || e.G.Obj(big.ID).Zone != state.ZBattlefield {
			t.Fatal("precondition: both comparison creatures must be on the battlefield")
		}
		if pump {
			e.AddContinuous(state.ContinuousEffect{Source: bear.ID, Controller: 1, Layer: LPT, Sub: SubModify,
				Affects: "Creature.Self", AddPower: 5, AddToughness: 5})
		}
		if bp, dp := e.G.Obj(bear.ID).Face().Power(), e.G.Obj(big.ID).Face().Power(); bp >= dp {
			t.Fatalf("precondition: printed powers bear=%d dreadmaw=%d, want bear < dreadmaw", bp, dp)
		}
		sa := &cards.SA{Kind: "SP", API: "Destroy", Line: "test:GetawayGlamer", Params: map[string]string{
			"ValidTgts":        "Creature",
			"ConditionDefined": "Targeted",
			"ConditionPresent": "Creature.greatestPower",
		}}
		ctx := &effects.Ctx{Source: src.ID, Controller: 0,
			Targets:   []state.Target{{Obj: big.ID}},
			SubPreAsk: map[string][]state.Target{sa.Line: {{Obj: big.ID}}}}
		effects.Resolve(e, ctx, sa)
		if e.resume != nil {
			e.resume.outer = e.buildContinuationChain(e.contChain, src.ID, nil)
		}
		return e, bear.ID, big.ID
	}

	// Positive control: without the pump the Dreadmaw IS the greatest, so the
	// condition holds and the destroy lands -- proving the resolution reaches
	// effDestroy with the target bound.
	e0, _, big0 := board(false)
	if z := e0.G.Obj(big0).Zone; z != state.ZGraveyard {
		t.Fatalf("positive control: unpumped targeted Colossal Dreadmaw zone = %s, want Graveyard (condition must hold)", z)
	}

	// The pumped case: the bear's derived 7 > the Dreadmaw's 6, so the
	// condition must fail and the Dreadmaw must survive.
	e1, bear1, big1 := board(true)
	if e1.Power(bear1) != 7 || e1.Power(big1) != 6 {
		t.Fatalf("precondition: derived powers bear=%d dreadmaw=%d, want 7 and 6",
			e1.Power(bear1), e1.Power(big1))
	}
	if z := e1.G.Obj(big1).Zone; z != state.ZBattlefield {
		t.Fatalf("pumped targeted Colossal Dreadmaw zone = %s, want Battlefield (a pumped greater creature must defeat the condition)", z)
	}
}
