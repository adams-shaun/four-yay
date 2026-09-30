package rules

// Regression pin: an animated non-creature permanent with toughness <= 0
// must die to the CR 704.5f state-based action, and an animated land with
// lethal damage marked must die to CR 704.5g. rules/sba.go's
// destroyLethalDamage gated BOTH sweeps on the PRINTED face
// (`f.IsCreature()`), so an earthbent Forest with zero +1/+1 counters
// (Toph, Earthbending Master attacking with no experience counters) stayed
// on the battlefield forever as an untapped 0/0 "Basic Land Forest
// Creature", and was immortal to marked damage besides. The gate now reads
// the layer-derived type list (`e.IsCreature`), the same predicate combat
// legality uses and what CR 704.5f actually reads.
//
// The leaf drives the exact report scenario on the real corpus card: Toph
// with zero experience counters attacks, earthbend X=0 targets a Forest,
// and after settling the Forest is put into its owner's graveyard by
// 704.5f and the earthbend return promise brings it back TAPPED as a plain
// land.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestEarthbendZeroToughnessAnimatedLandDiesToSBA is the report scenario:
// Toph attacks with zero experience counters, earthbends the Forest into a
// 0/0 creature, and CR 704.5f must send it to the graveyard; the earthbend
// promise then returns it tapped as a plain land.
func TestEarthbendZeroToughnessAnimatedLandDiesToSBA(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Toph, Earthbending Master", "Forest")

	forest := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	toph := searchMoveByName(t, e, "Toph, Earthbending Master", state.ZBattlefield)
	to := e.G.Obj(toph)
	if to == nil || to.Zone != state.ZBattlefield || to.Controller != 0 {
		t.Fatalf("precondition: Toph not on seat 0's battlefield: %+v", to)
	}
	// The leaf turns on X=0: a stray Experience counter would put counters on
	// the land and it would survive, so the zero is asserted.
	if n := to.Counter("Experience"); n != 0 {
		t.Fatalf("precondition: Toph has %d Experience counters, want 0 (earthbend X must be 0)", n)
	}
	if e.IsCreature(forest) {
		t.Fatal("precondition: the Forest is already a creature")
	}
	// Replay the setup (moves + priority rounds) before declareAttackersOnly,
	// which mutates SummonSick directly rather than through an event.
	replayCheck(t, e, cfg)
	declareAttackersOnly(t, e, 0, 1, toph)

	// Toph's "whenever you attack" trigger earthbends X (0). The trigger is
	// pushed on the next priority round, so settle answering every target
	// ask with the Forest. This is where the animation resolves and the SBA
	// fires.
	before := len(e.L.Events)
	earthbendSettleAnswering(t, e, forest, 60)

	// The precondition the SBA read: the land was destroyed by the
	// zero-toughness sweep specifically -- the MoveZone to the graveyard
	// carries the CR 704.5f casualty text this same gate emits. Without the
	// printed-face fix this event does not exist at all, so the assertion is
	// exactly the defect under test.
	var deathText string
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.MoveZone && ev.Obj == forest && ev.To == state.ZGraveyard {
			deathText = ev.Text
		}
	}
	if deathText != "toughness <= 0" {
		t.Fatalf("animated 0/0 Forest MoveZone-to-graveyard Text = %q, want %q (it never died to CR 704.5f)",
			deathText, "toughness <= 0")
	}

	// The earthbend return promise then brought it back tapped as a plain
	// land.
	o := e.G.Obj(forest)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Forest did not return to the battlefield: %+v", o)
	}
	if !o.Tapped {
		t.Fatal("returned Forest entered untapped; the oracle says tapped")
	}
	if e.IsCreature(forest) {
		t.Fatalf("returned Forest is still a creature (types=%v)", e.Derived(forest).Types)
	}
	if o.EnteredFrom != state.ZGraveyard {
		t.Fatalf("returned Forest entered from %v, want graveyard", o.EnteredFrom)
	}
	if c := o.Counter("P1P1"); c != 0 {
		t.Fatalf("returned Forest kept %d +1/+1 counters; CR 122.2 removes them on leaving", c)
	}
}

// TestAnimatedLandMarkedLethalDamageDiesToSBA is the CR 704.5g half of the
// same gate on a manland-shaped animation: Stalking Stones becomes a
// permanent 3/3 artifact creature (real corpus card, Duration$ Permanent),
// then takes 3 marked damage. With the printed-face gate it survived
// (immortal), which the earthbend test's old comment disclosed as an
// engine-wide gap; the layer-derived gate must destroy it.
func TestAnimatedLandMarkedLethalDamageDiesToSBA(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Stalking Stones")}, []*cards.Card{})
	stones := moveByName(t, e, 0, "Stalking Stones", state.ZBattlefield)
	if o := e.G.Obj(stones); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Stalking Stones not on the battlefield: %+v", o)
	}
	if e.IsCreature(stones) {
		t.Fatal("precondition: Stalking Stones is already a creature")
	}
	addMana(t, e, 0, "CCCCCC")
	submitChoices(t, e, animateAbilityOption(t, e, stones).Index)
	settleActivation(t, e)
	if !e.IsCreature(stones) || e.Toughness(stones) != 3 {
		t.Fatalf("precondition: Stalking Stones not an animated 3/3: types=%v %d/%d",
			e.Derived(stones).Types, e.Power(stones), e.Toughness(stones))
	}

	// 3 marked damage == lethal for a 3/3.
	e.emit(events.Event{Kind: events.Damage, Obj: stones, Amount: 3})
	e.checkStateBased()
	if o := e.G.Obj(stones); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("animated 3/3 with 3 marked damage must die to CR 704.5g: zone=%v", o.Zone)
	}
}
