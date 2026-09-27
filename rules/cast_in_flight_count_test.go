package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	// frogTestSrc is Leapfrog-shaped: flying while its controller has cast
	// an instant or sorcery this turn (a CheckSVar$ gate on a
	// Count$ThisTurnCast_<spec> count).
	frogTestSrc = "Name:Frog Test\nManaCost:2 U\nTypes:Creature Frog\nPT:3/1\n" +
		"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Flying | CheckSVar$ X | Description$ x\n" +
		"SVar:X:Count$ThisTurnCast_Instant.YouCtrl,Sorcery.YouCtrl\nOracle:x\n"
	// galeReduce is Gust-of-Wind-shaped: {2} less while its caster controls a
	// creature with flying.
	galeReduce = "Name:Gale Test\nManaCost:3 U\nTypes:Sorcery\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 2 | EffectZone$ All | IsPresent$ Creature.YouCtrl+withFlying | Description$ x\n"
)

// TestInFlightCastIsNotYetCastThisTurn pins CR 601.2i for a cost read of the
// this-turn cast counts (round-8 cardfuzz mirror seed 12687133153333408407,
// a_witness pool_after, Gust of Wind beside Leapfrog; story
// thisturncast-counts-spell-in-progress). With no instant or sorcery cast yet,
// the sorcery's own push (CR 601.2a) must not count while its cost is
// composed (withCostCompositionEvent), so the full {3}{U} is charged; once it
// is cast, the frog flies. The exclusion itself was already in place; what
// broke it was active()'s log-head-keyed lists: built at the target ask with
// the push counted (the frog flying), they were re-adopted inside the
// exclusion across the answer's layer-inert DecisionMade, and the reduction
// applied.
func TestInFlightCastIsNotYetCastThisTurn(t *testing.T) {
	e, _, gale := newFixtureDeck(t, 9640, galeReduce+
		"A:SP$ ChangeZone | ValidTgts$ Permanent.nonLand+YouDontCtrl | TgtPrompt$ x | Origin$ Battlefield | Destination$ Hand\nOracle:x\n")
	onBoard(t, e, 1, "Name:Bounce Target\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	frog := onBoard(t, e, 0, frogTestSrc)
	addMana(t, e, 0, "UUUU")
	if e.HasKeyword(frog, "Flying") {
		t.Fatal("precondition: the frog flies before any instant or sorcery is cast")
	}
	submitCastOption(t, e, gale)
	for i := 0; i < 8 && e.G.Obj(gale).Zone == state.ZStack && e.cast != nil; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	spent := int32(0)
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Amount < 0 {
			spent -= ev.Amount
		}
	}
	if spent != 4 {
		t.Errorf("the cast spent %d mana (pool left %v), want 4: the spell's own push counted as an instant or sorcery already cast", spent, e.G.Players[0].Pool)
	}
	if o := e.G.Obj(gale); o.Zone != state.ZStack {
		t.Fatalf("gale zone = %s, want stack", o.Zone)
	}
	if !e.HasKeyword(frog, "Flying") {
		t.Error("once cast (CR 601.2i) the sorcery counts: the frog must fly")
	}
}

// TestStaticRefreshDropsNestedActiveBuild pins refreshStaticContinuous'
// nested-build guard. The static memo is stamped at the head before its
// rescan, so a gate that reads Derived (the frog's CheckSVar$ count matching
// the cast sorcery's type) builds active() from the PREVIOUS static list.
// Entered from staticControlWants, that nested build is an outermost one and
// stamped activeBuf at the head: after an untargeted Gale cast (whose cascade
// scratch walk had just invalidated both lists) the frog's flying grant was
// missing until the next non-bookkeeping event, and the layer-inert verifier
// (layerInertVerify) panicked on the reused list disagreeing with a rebuild.
func TestStaticRefreshDropsNestedActiveBuild(t *testing.T) {
	e, _, gale := newFixtureDeck(t, 9641, galeReduce+"A:SP$ Draw | NumCards$ 1\nOracle:x\n")
	frog := onBoard(t, e, 0, frogTestSrc)
	addMana(t, e, 0, "UUUU")
	submitCastOption(t, e, gale)
	if o := e.G.Obj(gale); o.Zone != state.ZStack {
		t.Fatalf("gale zone = %s, want stack", o.Zone)
	}
	if !e.HasKeyword(frog, "Flying") {
		t.Error("once cast (CR 601.2i) the sorcery counts: the frog must fly")
	}
}
