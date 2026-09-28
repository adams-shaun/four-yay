package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// eventCaptureProbe is an inline (never corpus) card for task
// rememberedcapture1, covering the EVENT-OBJECT capture path that
// rules.resolvingRemembered does NOT mask. resolvingRemembered (added on main
// for Tombstone Stairwell) rewrites only a printed Mode$ Phase trigger's
// resolution ctx, where the capture is the trigger's own source. An
// event-object trigger -- here a ChangesZone trigger whose fire-time capture is
// the entering CREATURE (ev.Obj), distinct from this enchantment source --
// still reaches effects with Remembered == Captured == [entering creature], so
// a plain Remembered$Amount read counts it unless the shared capture-exclusion
// helper removes it. The gated leg is a DB$ Draw: it fires when and only when
// the EQ0 gate holds, i.e. when nothing was actually remembered.
const eventCaptureProbe = "Name:EventProbe\nManaCost:1 R\nTypes:Enchantment\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature.Other | TriggerZones$ Battlefield | Execute$ TrigPayoff | TriggerDescription$ Whenever another creature enters, if nothing was remembered, draw a card.\n" +
	"SVar:TrigPayoff:DB$ Draw | NumCards$ 1 | ConditionCheckSVar$ Y | ConditionSVarCompare$ EQ0 | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"SVar:Y:Remembered$Amount\n" +
	"Oracle:Whenever another creature enters, if nothing was remembered, draw a card.\n"

const eventCaptureVictim = "Name:EventVictim\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestRememberedEventCaptureExcludedInTriggerBody drives an
// EVENT-OBJECT-capture trigger end to end: the entering creature (not this
// source) is the fire-time capture, so before the fix the resolution ctx
// carried Remembered == [entering creature], Remembered$Amount read 1, the EQ0
// gate failed and the Draw leg was silently skipped. resolvingRemembered does
// not apply here (the mode is ChangesZone, not Phase and the capture is not the
// source), so unlike the Phase probe this one is a live discriminator for the
// effects-side exclusion on current main.
func TestRememberedEventCaptureExcludedInTriggerBody(t *testing.T) {
	t.Parallel()
	probeCard := card(t, eventCaptureProbe)
	victimCard := card(t, eventCaptureVictim)
	cfg := seatZeroStart(Config{Seed: 917, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{probeCard}, mountainDeck(t, 39)...),
			append([]*cards.Card{victimCard}, mountainDeck(t, 39)...),
		}})
	e := New(cfg)
	e.Advance()
	if e.G.Active != 0 {
		t.Fatalf("precondition: seat 0 is not the active player")
	}
	probe := moveSeeded(t, e, 0, eventCaptureProbe, state.ZBattlefield)
	// Precondition: the trigger source is on the battlefield in a non-Phase
	// mode whose capture is an event object, never the source.
	if e.G.Obj(probe).Zone != state.ZBattlefield {
		t.Fatalf("precondition: probe not on the battlefield")
	}
	if got := e.G.Obj(probe).Face().Triggers[0].Mode; got != "ChangesZone" {
		t.Fatalf("precondition: probe trigger mode = %q, want ChangesZone", got)
	}
	e.pendingTriggers = nil

	before := len(e.G.Zone(state.ZHand, 0))
	// The entering creature is the event object; its id differs from the
	// enchantment source, which is exactly what makes resolvingRemembered
	// fall through and the effects-side exclusion load-bearing.
	victim := moveSeeded(t, e, 1, eventCaptureVictim, state.ZBattlefield)
	if victim == probe {
		t.Fatalf("precondition: event object equals the trigger source")
	}
	if e.G.Obj(victim).Zone != state.ZBattlefield {
		t.Fatalf("precondition: victim not on the battlefield")
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("queued triggers = %d, want the probe's ChangesZone trigger", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	// A targetless trigger asks nothing at placement, so drive its resolution
	// directly (the same shape baloth_prime_stun_untap_test.go uses).
	e.resolveTop()
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
	if draws := len(e.G.Zone(state.ZHand, 0)) - before; draws != 1 {
		t.Fatalf("event-capture probe drew %d, want 1 (the entering creature must not count as remembered)", draws)
	}
}
