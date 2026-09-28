package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// endStepRememberedNumberProbe is an inline (never corpus) card for the
// consolidated acceptance clause of task rememberedcapture1: a trigger body
// reading Count$RememberedNumber under a ConditionCheckSVar$ gate must not
// count the trigger's own fire-time capture. Shaped after
// endStepCaptureProbe, but the gating SVar is Count$RememberedNumber
// (Forge's remembered-object count head) instead of Remembered$Amount --
// the two readers must agree on the capture-excluded set.
const endStepRememberedNumberProbe = "Name:ProbeRN\nManaCost:1 R\nTypes:Enchantment\n" +
	"T:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigTarget | TriggerDescription$ At the beginning of your end step, target creature may be sacrificed; if not, draw a card.\n" +
	"SVar:TrigTarget:DB$ Pump | ValidTgts$ Creature | SubAbility$ DBSac\n" +
	"SVar:DBSac:DB$ Sacrifice | Defined$ TargetedController | SacValid$ TargetedCard.Self | Optional$ True | RememberSacrificed$ True | SubAbility$ TrigPayoff\n" +
	"SVar:TrigPayoff:DB$ Draw | NumCards$ 1 | ConditionCheckSVar$ RN | ConditionSVarCompare$ EQ0 | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"SVar:RN:Count$RememberedNumber\n" +
	"Oracle:At the beginning of your end step, target creature's controller may sacrifice it. If they don't, draw a card.\n"

// forcedRememberedNumberProbe is the contrast: a mandatory sacrifice really
// remembers the targeted creature, so Count$RememberedNumber reads 1 and the
// EQ0 gate stays skipped.
const forcedRememberedNumberProbe = "Name:ProbeRN2\nManaCost:1 R\nTypes:Enchantment\n" +
	"T:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigTarget | TriggerDescription$ At the beginning of your end step, target creature's controller sacrifices it; if nothing was sacrificed, draw a card.\n" +
	"SVar:TrigTarget:DB$ Pump | ValidTgts$ Creature | SubAbility$ DBSac\n" +
	"SVar:DBSac:DB$ Sacrifice | Defined$ TargetedController | SacValid$ Creature | RememberSacrificed$ True | SubAbility$ TrigPayoff\n" +
	"SVar:TrigPayoff:DB$ Draw | NumCards$ 1 | ConditionCheckSVar$ RN | ConditionSVarCompare$ EQ0 | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"SVar:RN:Count$RememberedNumber\n" +
	"Oracle:At the beginning of your end step, target creature's controller sacrifices it; if nothing was sacrificed, draw a card.\n"

// TestRememberedNumberCaptureExcludedInTriggerBody drives the
// Count$RememberedNumber probe end to end at the engine. The trigger's
// fire-time capture must NOT count, so an unsacrificed resolution leaves the
// count at 0 and the EQ0-gated Draw fires; the forced-sacrifice contrast
// remembers the sacrificed card and the leg stays skipped.
func TestRememberedNumberCaptureExcludedInTriggerBody(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, probeSrc string) (int, state.Zone) {
		t.Helper()
		probeCard := card(t, probeSrc)
		victimCard := card(t, captureVictimCard)
		cfg := seatZeroStart(Config{Seed: 742, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{probeCard}, mountainDeck(t, 39)...),
				append([]*cards.Card{victimCard}, mountainDeck(t, 39)...),
			}})
		e := New(cfg)
		e.Advance()
		if e.G.Active != 0 {
			t.Fatalf("precondition: seat 0 is not the active player")
		}
		probe := moveSeeded(t, e, 0, probeSrc, state.ZBattlefield)
		victim := moveSeeded(t, e, 1, captureVictimCard, state.ZBattlefield)
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
		e.pending = nil
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("queued triggers = %d, want the probe's end-step trigger", len(e.pendingTriggers))
		}
		e.putTriggersOnStack()
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

	draws, victimZone := run(t, endStepRememberedNumberProbe)
	if victimZone != state.ZBattlefield {
		t.Fatalf("precondition: optional probe victim zone = %s, want battlefield (nothing sacrificed)", victimZone)
	}
	if draws != 1 {
		t.Fatalf("optional-sacrifice probe drew %d, want 1 (capture must not inflate Count$RememberedNumber)", draws)
	}
	draws, victimZone = run(t, forcedRememberedNumberProbe)
	if victimZone != state.ZGraveyard {
		t.Fatalf("precondition: forced probe victim zone = %s, want graveyard (the sacrifice must really happen)", victimZone)
	}
	if draws != 0 {
		t.Fatalf("forced-sacrifice probe drew %d, want 0 (Count$RememberedNumber must count the real memory)", draws)
	}
}
