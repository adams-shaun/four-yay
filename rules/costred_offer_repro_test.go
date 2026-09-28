package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// costredPerm puts a permanent onto p's battlefield through a real MoveZone
// so every log-head-keyed cache (mana sources, statics) sees it.
func costredPerm(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

const costredIsland = "Name:Island\nTypes:Basic Land Island\nA:AB$ Mana | Cost$ T | Produced$ U\nOracle:x\n"
const costredMountain = "Name:Mountain\nTypes:Basic Land Mountain\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n"
const costredBear = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// Battlefield Thaumaturge: "Each instant and sorcery spell you cast costs {1}
// less to cast for each creature it targets." A {1}{U} instant that targets a
// creature costs {U}: with only {U} floating the cast must be OFFERED (the
// offer gate's potential-target retry used to run only for spellings it
// recognised, and the Thaumaturge's TargetedObjectsDistinct$ amount was not
// one), and the completed cast must charge exactly {U}.
func TestCostredThaumaturgeOffersReducedCastFromPool(t *testing.T) {
	t.Parallel()
	bolt := card(t, "Name:Test Zap\nManaCost:1 U\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1\nOracle:x\n")
	e := handEngine(t, bolt)
	th := corpusAlternativeCard(t, "Battlefield Thaumaturge")
	o := e.G.AddObject(th, 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	bear := costredPerm(t, e, 1, costredBear)
	e.G.Players[0].Pool[state.MU] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Test Zap not offered with {U} floating + Battlefield Thaumaturge")
	}
	castMode(t, e, spell, "")
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("cast stalled with no decision; spell in %s", e.G.Obj(spell).Zone)
		}
		if d.Kind == decision.KPriority {
			break
		}
		pick := 0
		for _, opt := range d.Options {
			if opt.Obj == bear {
				pick = opt.Index
			}
		}
		submitChoices(t, e, pick)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("Test Zap in %s after cast, want stack; pending %+v", z, e.Pending())
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0 (charged {U})", left)
	}
}

func TestCostredDargoOffersWithThreeSacsFromPool(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Dargo, the Shipwrecker"))
	for i := 0; i < 3; i++ {
		costredPerm(t, e, 0, costredBear)
	}
	e.G.Players[0].Pool[state.MR] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Dargo not offered with three sacrificeable creatures + {R} floating")
	}
}
