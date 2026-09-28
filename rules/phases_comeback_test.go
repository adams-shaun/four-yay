package rules

// Phasing come-back (CR 702.25): the "phase out until CARDNAME leaves the
// battlefield" family. These are engine-boundary tests on real corpus cards
// whose printed body carries
// `DB$ Effect | Triggers$ TrigComeBack | Duration$ Permanent |
// ForgetOnPhasedIn$ True`. They prove the Effect's own ChangesZone comeback
// trigger registers with a permanent (non-turn-ceiling) lifetime, fires when
// the host leaves the battlefield, phases the remembered permanents back in
// (TAPPED for Oubliette), and retires the registration through its own
// DBExileSelf body.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// comebackCorpusCard looks a real corpus card up by name.
func comebackCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	return tokenReplCorpusCard(t, name)
}

// comebackEffectRegistrations returns the number of live EffectRepeat delayed
// registrations owned by the object (the `|EF` comeback trigger).
func comebackEffectRegistrations(e *Engine, source state.ObjID) int {
	n := 0
	for _, dt := range e.G.Delayed {
		if dt.Source == source && dt.EffectRepeat {
			n++
		}
	}
	return n
}

// TestOutOfTimeComebackPhasesCreaturesBackIn is the brief's Out of Time
// acceptance: it enters, untaps all creatures then phases them out, is
// genuinely hidden mid-flight, carries a TIME counter per phased-out creature
// from its own Count$RememberedSize, holds WontPhaseInNormal through its
// controller's untap step, and phases the creatures back in from its
// comeback trigger when Out of Time leaves the battlefield.
func TestOutOfTimeComebackPhasesCreaturesBackIn(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 501, "Out of Time", "Grizzly Bears", "Grizzly Bears")
	toMain1(t, e)
	bears := make([]state.ObjID, 0, 2)
	for i := 0; i < 2; i++ {
		bid := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		if o := e.G.Obj(bid); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: bear %d is not a battlefield permanent: %+v", i, o)
		}
		e.emit(events.Event{Kind: events.Tap, Obj: bid})
		if !e.G.Obj(bid).Tapped {
			t.Fatalf("precondition: bear %d is not tapped before Out of Time enters", i)
		}
		bears = append(bears, bid)
	}
	oot := moveByName(t, e, 0, "Out of Time", state.ZBattlefield)
	if o := e.G.Obj(oot); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Out of Time is not on the battlefield: %+v", o)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Out of Time's entry queued %d stack objects, want its printed enters trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if phasesHasNote(e, "unimplemented API Phases") {
		t.Fatal("api:Phases is unregistered (unimplemented fallback Note present)")
	}
	// Precondition: the real printed body ran -- UntapAll untapped them, then
	// Phases phased them out.
	for _, bid := range bears {
		o := e.G.Obj(bid)
		if o == nil || o.Zone != state.ZBattlefield || !o.PhasedOut {
			t.Fatalf("precondition: bear %d not phased out on the battlefield: %+v", bid, o)
		}
		if o.Tapped {
			t.Fatalf("precondition: bear %d stayed tapped: the printed UntapAll did not run", bid)
		}
	}
	// The Effect's comeback trigger is registered with a permanent lifetime.
	if got := comebackEffectRegistrations(e, oot); got != 1 {
		t.Fatalf("Out of Time registered %d Effect comeback triggers, want 1 (the printed TrigComeBack)", got)
	}
	// The real Count$RememberedSize clock: one TIME counter per phased-out
	// creature, no injected counters.
	if got := e.G.Obj(oot).Counter("TIME"); got != 2 {
		t.Fatalf("precondition: Out of Time holds %d TIME counters, want 2 from its printed Count$RememberedSize", got)
	}
	// Mid-flight the creatures are genuinely hidden: not offered as targets,
	// unable to attack.
	sa := &cards.SA{Params: map[string]string{"ValidTgts": "Creature"}}
	for _, bid := range bears {
		if cr702hasObj(e.legalTargetCandidates(0, 0, 0, sa), bid) {
			t.Fatal("CR 702.25b: a phased-out creature was offered as a target")
		}
		if e.canAttack(bid) {
			t.Fatal("CR 702.25b: a phased-out creature was allowed to attack")
		}
	}
	// WontPhaseInNormal: their controller's next untap step does NOT phase
	// them in (the comeback trigger owns that).
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	for _, bid := range bears {
		if o := e.G.Obj(bid); o == nil || !o.PhasedOut {
			t.Fatalf("WontPhaseInNormal: bear %d phased in at the untap step instead of awaiting the comeback: %+v", bid, o)
		}
	}
	// Out of Time leaves -> the comeback trigger fires inline, phases the
	// creatures back in, and the Effect retires itself.
	e.emit(events.Event{Kind: events.MoveZone, Obj: oot, From: state.ZBattlefield, To: state.ZGraveyard})
	if z := e.G.Obj(oot).Zone; z != state.ZGraveyard {
		t.Fatalf("Out of Time zone = %s, want graveyard", z)
	}
	for _, bid := range bears {
		if o := e.G.Obj(bid); o == nil || o.PhasedOut {
			t.Fatalf("comeback: bear %d did not phase back in when Out of Time left: %+v", bid, o)
		}
	}
	if got := comebackEffectRegistrations(e, oot); got != 0 {
		t.Fatalf("the comeback Effect did not retire: %d EffectRepeat registrations remain", got)
	}
	replayCheck(t, e, cfg)
}

// TestOublietteComebackPhasesTargetInTapped pins the targeted carrier:
// Oubliette's ETB offers a creature target, phasing it out; when Oubliette
// leaves, the creature phases back in TAPPED (`Tapped$ True` on the printed
// TrigPhaseIn body).
func TestOublietteComebackPhasesTargetInTapped(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 502, "Oubliette", "Grizzly Bears")
	toMain1(t, e)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: bear is not a phased-in battlefield permanent: %+v", o)
	}
	oot := moveByName(t, e, 0, "Oubliette", state.ZBattlefield)
	if o := e.G.Obj(oot); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Oubliette is not on the battlefield: %+v", o)
	}
	addMana(t, e, 0, "")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Player != 0 {
		t.Fatalf("expected Oubliette's target ask, got %+v", d)
	}
	// Precondition: the phased-in creature IS offered (the offer is real).
	if len(d.Options) == 0 {
		t.Fatal("Oubliette's target ask offered no options")
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == bear {
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: the phased-in creature was not offered by Oubliette's target ask")
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || !o.PhasedOut {
		t.Fatalf("after Oubliette ETB: bear PhasedOut=%v Zone=%v, want phased out on the battlefield", o != nil && o.PhasedOut, o.Zone)
	}
	if got := comebackEffectRegistrations(e, oot); got != 1 {
		t.Fatalf("Oubliette registered %d Effect comeback triggers, want 1", got)
	}
	// Make the phase-in observable: untap the (now hidden) creature's
	// battlefield state is irrelevant while it is phased out; the probe is
	// Tapped after the comeback.
	e.emit(events.Event{Kind: events.Untap, Obj: bear})
	if e.G.Obj(bear).Tapped {
		t.Fatal("precondition: the creature could not be untapped before the comeback")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: oot, From: state.ZBattlefield, To: state.ZGraveyard})
	o := e.G.Obj(bear)
	if o == nil || o.PhasedOut {
		t.Fatalf("comeback: the targeted creature did not phase back in: %+v", o)
	}
	if !o.Tapped {
		t.Fatal("comeback: Oubliette's `Tapped$ True` phase-in did not tap the creature")
	}
	if got := comebackEffectRegistrations(e, oot); got != 0 {
		t.Fatalf("the Oubliette comeback Effect did not retire: %d EffectRepeat registrations remain", got)
	}
	replayCheck(t, e, cfg)
}
