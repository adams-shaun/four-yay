package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// endStepCaptureProbe is an inline (never corpus) card shaped after
// Enchanter's Bane minus the imprint, for task rememberedcapture1: an end-step
// phase trigger targets a creature, its controller may sacrifice it, and a
// chained leg is gated `ConditionCheckSVar$ Y | ConditionSVarCompare$ EQ0`
// with `SVar:Y:Remembered$Amount`. With the capture excluded, an unsacrificed
// resolution leaves the remembered set empty, Y reads 0 and EQ0 HOLDS (the leg
// fires). The forced-sacrifice variant writes the sacrificed card into
// Remembered (`RememberSacrificed$ True`), so the same gate must deny -- the
// contrast that proves the read distinguishes "remembered nothing" from
// "remembered one".
//
// The observable is the gated leg's `DB$ Draw`: the probe's controller draws a
// card when and only when the gate holds. The card carries no Imprinted
// reference, so it does not depend on the separate Defined$
// ImprintedController defect (see the report's ## Issues): Enchanter's Bane's
// own damage leg stays dead for that second reason even once this condition
// leg is correct, which is why the acceptance here is the condition semantics,
// not the Bane's damage.
//
// The optional ask in this shape does NOT pose (measured; see the report's
// ## Issues): the engine takes the deterministic decline, so the declined case
// is the no-ask path the real Enchanter's Bane also drives through in triage.
const endStepCaptureProbe = "Name:Probe\nManaCost:1 R\nTypes:Enchantment\n" +
	"T:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigTarget | TriggerDescription$ At the beginning of your end step, target creature may be sacrificed; if not, draw a card.\n" +
	"SVar:TrigTarget:DB$ Pump | ValidTgts$ Creature | SubAbility$ DBSac\n" +
	"SVar:DBSac:DB$ Sacrifice | Defined$ TargetedController | SacValid$ TargetedCard.Self | Optional$ True | RememberSacrificed$ True | SubAbility$ TrigPayoff\n" +
	"SVar:TrigPayoff:DB$ Draw | NumCards$ 1 | ConditionCheckSVar$ Y | ConditionSVarCompare$ EQ0 | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"SVar:Y:Remembered$Amount\n" +
	"Oracle:At the beginning of your end step, target creature's controller may sacrifice it. If they don't, draw a card.\n"

// forcedSacrificeProbe is the contrast: the sacrifice is mandatory, so the
// resolution reliably sacrifices and remembers the targeted creature. The
// same EQ0 gate then reads Remembered$Amount = 1 and must skip the Draw.
const forcedSacrificeProbe = "Name:Probe2\nManaCost:1 R\nTypes:Enchantment\n" +
	"T:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigTarget | TriggerDescription$ At the beginning of your end step, target creature's controller sacrifices it; if nothing was sacrificed, draw a card.\n" +
	"SVar:TrigTarget:DB$ Pump | ValidTgts$ Creature | SubAbility$ DBSac\n" +
	"SVar:DBSac:DB$ Sacrifice | Defined$ TargetedController | SacValid$ Creature | RememberSacrificed$ True | SubAbility$ TrigPayoff\n" +
	"SVar:TrigPayoff:DB$ Draw | NumCards$ 1 | ConditionCheckSVar$ Y | ConditionSVarCompare$ EQ0 | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"SVar:Y:Remembered$Amount\n" +
	"Oracle:At the beginning of your end step, target creature's controller sacrifices it; if nothing was sacrificed, draw a card.\n"

const captureVictimCard = "Name:Victim\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestRememberedCaptureExcludedInTriggerBody drives the probe end to end at
// the engine: the trigger's fire-time capture must NOT count as something the
// resolution remembered, so an unsacrificed resolution leaves
// Remembered$Amount at 0 and the EQ0-gated Draw leg fires. Before the fix the
// phase trigger's ctx carried Remembered=[source], Amount read 1, and the leg
// was silently skipped -- exactly Enchanter's Bane's dead damage leg (triage
// measured zero Damage events).
func TestRememberedCaptureExcludedInTriggerBody(t *testing.T) {
	t.Parallel()
	// run drives one probe card and returns (draws, victimZone).
	run := func(t *testing.T, probeSrc string) (int, state.Zone) {
		t.Helper()
		probeCard := card(t, probeSrc)
		victimCard := card(t, captureVictimCard)
		cfg := seatZeroStart(Config{Seed: 741, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{probeCard}, mountainDeck(t, 39)...),
				append([]*cards.Card{victimCard}, mountainDeck(t, 39)...),
			}})
		e := New(cfg)
		e.Advance()
		if e.G.Active != 0 {
			t.Fatalf("precondition: seat 0 is not the active player")
		}
		// Seeded-card placement (logged MoveZone), not the eventless onBoard
		// helper: replayCheck reconstructs the game from cfg.Decks and
		// replays the log, so a directly AddObject'ed card would have no
		// replay trace.
		probe := moveSeeded(t, e, 0, probeSrc, state.ZBattlefield)
		victim := moveSeeded(t, e, 1, captureVictimCard, state.ZBattlefield)
		// Preconditions: both permanents are on the battlefield, the probe's
		// phase trigger is registered, and the victim is controlled by seat 1
		// (so the sacrifice's defined controller and the ability's controller
		// differ, the shape Enchanter's Bane drives).
		if e.G.Obj(probe).Zone != state.ZBattlefield || e.G.Obj(victim).Zone != state.ZBattlefield {
			t.Fatalf("precondition: probe/victim not on the battlefield")
		}
		if got := e.G.Obj(probe).Face().Triggers[0].Mode; got != "Phase" {
			t.Fatalf("precondition: probe trigger mode = %q, want Phase", got)
		}
		if e.G.Obj(victim).Controller != 1 {
			t.Fatalf("precondition: victim controller = %d, want seat 1", e.G.Obj(victim).Controller)
		}

		before := len(e.G.Zone(state.ZHand, 0))
		// Clear the step-driving priority, then enter the end step so the
		// probe's own Phase trigger is queued and placed through the real
		// path (putTriggersOnStack asks the trigger's targets).
		e.pending = nil
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("queued triggers = %d, want the probe's end-step trigger", len(e.pendingTriggers))
		}
		e.putTriggersOnStack()
		// The target ask: the targeted creature is the seat-1 victim.
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("probe did not ask for a target: %+v", d)
		}
		answered := false
		for _, o := range d.Options {
			if o.Obj == victim {
				submitChoices(t, e, o.Index)
				answered = true
				break
			}
		}
		if !answered {
			t.Fatalf("target ask did not offer the victim: %+v", d.Options)
		}
		// Drain the rest (priority windows, and any optional-sacrifice ask
		// this shape might pose) until the stack empties.
		for i := 0; i < 50 && len(e.G.Stack) > 0; i++ {
			d := e.Pending()
			if d == nil {
				break
			}
			switch d.Kind {
			case decision.KChoose, decision.KTriggerOptional:
				submitChoices(t, e)
			case decision.KPriority:
				passFirst(t, e)
			default:
				t.Fatalf("unexpected decision %q while draining: %+v", d.Kind, d.Options)
			}
		}
		replayCheck(t, e, cfg)
		return len(e.G.Zone(state.ZHand, 0)) - before, e.G.Obj(victim).Zone
	}

	// Optional sacrifice: the engine takes the deterministic decline, so
	// nothing is remembered and the EQ0-gated Draw fires. The capture alone
	// would make Remembered$Amount read 1 and silently skip it.
	draws, victimZone := run(t, endStepCaptureProbe)
	if victimZone != state.ZBattlefield {
		t.Fatalf("precondition: optional probe victim zone = %s, want battlefield (nothing sacrificed)", victimZone)
	}
	if draws != 1 {
		t.Fatalf("optional-sacrifice probe drew %d, want 1 (capture must not inflate Remembered$Amount)", draws)
	}
	// Contrast: the mandatory sacrifice remembers the sacrificed card, so
	// Remembered$Amount reads 1 and the EQ0 leg stays skipped.
	draws, victimZone = run(t, forcedSacrificeProbe)
	if victimZone != state.ZGraveyard {
		t.Fatalf("precondition: forced probe victim zone = %s, want graveyard (the sacrifice must really happen)", victimZone)
	}
	if draws != 0 {
		t.Fatalf("forced-sacrifice probe drew %d, want 0 (Remembered$Amount must count the real memory)", draws)
	}
}
