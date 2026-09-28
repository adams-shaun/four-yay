package rules

// Enchanter's Bane's damage leg, end to end on the engine: the end-step
// trigger targets an opposing enchantment (DB$ Pump | ImprintCards$ Targeted
// records the association), the chained optional sacrifice is declined, and
// TrigDamage's `Defined$ ImprintedController` must read the controller of the
// imprinted BATTLEFIELD permanent. The card imprints a battlefield permanent,
// so the CR 607.2a exile gate that ordinary Defined$ Imprinted keeps would
// drop the referent entirely -- the damage leg's controller selector reads the
// source's persistent imprint association instead. Before
// effects/context.go's definedSpec learned that read, `Defined$
// ImprintedController` outside a RepeatEach iteration resolved to nobody and
// the damage leg dealt nothing.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const probeAuraFixture = "Name:Probe Aura\nManaCost:2 W\nTypes:Enchantment\nOracle:x\n"

// enchantersBaneFixture seats an Enchanter's Bane (the real corpus card; it
// has no ETB trigger, so entering it without a cast exercises the end-step
// Phase trigger exactly as a cast would) for seat 0 and a probe enchantment
// on seat 1's battlefield, both through real MoveZone events (moveSeededCard)
// so the whole setup replays from the log. Seat 0 is the starting player by
// construction (seatZeroStart). Returns the engine, the config for
// replayCheck, and Bane's and the aura's ids.
func enchantersBaneFixture(t *testing.T) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	bane := searchCorpusCard(t, reg, "Enchanter's Bane")
	cfg := seatZeroStart(Config{Seed: 71, Names: []string{"bane", "cursed"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{bane}, mountainDeck(t, 40)...),
			append([]*cards.Card{card(t, probeAuraFixture)}, mountainDeck(t, 40)...),
		},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	baneID := moveSeededCard(t, e, 0, bane, state.ZBattlefield)
	auraID := moveSeededCard(t, e, 1, card(t, probeAuraFixture), state.ZBattlefield)
	return e, cfg, baneID, auraID
}

func TestEnchantersBaneDamageLegReachesImprintController(t *testing.T) {
	t.Parallel()
	e, cfg, baneID, auraID := enchantersBaneFixture(t)

	// Preconditions the damage leg reads: the imprinted target stands on the
	// battlefield under the OPPONENT's control (mana value 3 -> 3 damage), the
	// Bane stands under seat 0's control, and the two controllers plus the
	// 20-point life totals are the values the assertion distinguishes.
	aura := e.G.Obj(auraID)
	if aura == nil || aura.Zone != state.ZBattlefield || aura.Controller != 1 || aura.Face().ManaValue() != 3 {
		t.Fatalf("precondition: probe aura = %+v, want a battlefield enchantment controlled by seat 1 with MV 3", aura)
	}
	bane := e.G.Obj(baneID)
	if bane == nil || bane.Zone != state.ZBattlefield || bane.Controller != 0 {
		t.Fatalf("precondition: Enchanter's Bane = %+v, want a battlefield permanent controlled by seat 0", bane)
	}
	if e.G.Active != 0 {
		t.Fatalf("precondition: active seat = %d, want 0 (ValidPlayer$ You arms only Bane's controller's end step)", e.G.Active)
	}
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: life = %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}

	e.setStep(state.StepEnd)
	e.priorityRound()
	drainBaneAsks(t, e, auraID)

	// The damage leg reached the imprinted enchantment's controller: exactly
	// one 3-point Damage event against seat 1 (the aura's MV), life 20 -> 17.
	damage := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 1 && ev.Amount == 3 {
			damage++
		}
	}
	if damage != 1 {
		t.Fatalf("logged %d Damage(seat 1, 3) events, want exactly 1: the damage leg must reach Defined$ ImprintedController (the opponent aura's controller)", damage)
	}
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("seat 1 life = %d, want 17 (one 3-point hit from the imprinted aura's mana value)", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("seat 0 life = %d, want 20 (Bane's controller is not the target of the leg)", got)
	}
	// The sacrifice was declined, so the aura stayed; the cleanup leg cleared
	// the imprint association.
	if o := e.G.Obj(auraID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("probe aura left the battlefield after the declined sacrifice: %+v", o)
	}
	if o := e.G.Obj(baneID); o != nil && len(o.Imprinted) != 0 {
		t.Fatalf("Bane's imprint association = %v, want empty after DBCleanup's ClearImprinted$", o.Imprinted)
	}
	replayCheck(t, e, cfg)
}

// drainBaneAsks answers exactly the decisions the Bane chain poses: the pump's
// target ask (choosing wantObj), the optional sacrifice ask (declined -- the
// empty answer Min 0 makes legal, which is what gates the damage leg), and
// priority passes. Anything else is a test failure.
func drainBaneAsks(t *testing.T, e *Engine, wantObj state.ObjID) {
	t.Helper()
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch {
		case d.Kind == decision.KPriority && len(e.G.Stack) == 0:
			return
		case d.Kind == decision.KPriority:
			passFirst(t, e)
		case d.Kind == decision.KTarget:
			targetObject(t, e, wantObj)
		case d.Kind == decision.KChoose && d.ResumeKind == "sacrifice":
			submitChoices(t, e)
		default:
			t.Fatalf("unexpected decision %q while draining the Bane chain: %+v", d.Kind, d)
		}
	}
	t.Fatal("drainBaneAsks never emptied the stack")
}
