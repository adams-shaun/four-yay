package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// api:TapOrUntap (Merrow Reejerey's "whenever you cast a Merfolk spell, you
// may tap or untap target permanent"): the trigger is a real corpus card,
// the election is a real mid-resolution KChoose, and the answered election
// moves the target exactly once. The tapped shape and the untapped shape are
// separate tests; both drive the same corpus flow — cast the Merfolk spell,
// accept the optional trigger, target the bear, answer the election.

// taporuntapSetup builds the game: Merrow Reejerey on seat 0's battlefield,
// a Runeclaw Bear on the same battlefield (tapped when preTapped), and the
// {U}{U} Merfolk Trickster in hand with the mana to cast it.
func taporuntapSetup(t *testing.T, preTapped bool) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Merrow Reejerey", "Merfolk Trickster", "Runeclaw Bear")
	ree := searchMoveByName(t, e, "Merrow Reejerey", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Runeclaw Bear", state.ZBattlefield)
	mfst := searchMoveByName(t, e, "Merfolk Trickster", state.ZHand)
	if ree == 0 || bear == 0 || mfst == 0 {
		t.Fatalf("fixtures missing: reejerey %d bear %d merfolk %d", ree, bear, mfst)
	}
	if preTapped {
		e.emit(events.Event{Kind: events.Tap, Obj: bear})
	}
	addMana(t, e, 0, "UU")
	return e, ree, bear, mfst
}

// taporuntapCast drives the cast and the optional trigger through to the
// tap-or-untap election, returning the election decision with the bear's
// option indexes resolved.
func taporuntapCast(t *testing.T, e *Engine, mfst, bear state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == mfst {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the Merfolk: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// The trigger's placement target ask is posed when the trigger fires;
	// the optional election ("you may tap or untap") follows it at
	// resolution, the same order the Kor Outfitter flow observes.
	dt := passUntilAsk(t, e)
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("target ask = %+v, want KTarget", dt)
	}
	bearIdx := -1
	for _, o := range dt.Options {
		if o.Obj == bear {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("the bear was not offered as a target: %+v", dt.Options)
	}
	submitChoices(t, e, bearIdx)

	// CR 603.5: the optional trigger's election ("you may tap or untap").
	dopt := passUntilAsk(t, e)
	if dopt == nil || dopt.Kind != decision.KTriggerOptional {
		t.Fatalf("optional ask = %+v, want KTriggerOptional", dopt)
	}
	submitChoices(t, e, 0) // yes

	// The tap-or-untap election itself.
	de := passUntilAsk(t, e)
	if de == nil || de.Kind != decision.KChoose || de.ResumeKind != "taporuntap" {
		t.Fatalf("election = %+v, want KChoose with ResumeKind taporuntap", de)
	}
	if de.Min != 1 || de.Max != 1 || len(de.Options) != 2 {
		t.Fatalf("election bounds/options = %d..%d, %+v, want 1..1 over two options", de.Min, de.Max, de.Options)
	}
	return de
}

func taporuntapOption(t *testing.T, d *decision.Decision, kind string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	t.Fatalf("no %q option in %+v", kind, d.Options)
	return -1
}

// TestMerrowReejereyTapOrUntapPosesTheChoice is the untap shape: the bear is
// tapped, the election's option 0 is the state-changing "untap" (the no-host
// and bot-clamp mirror), answering it untaps the bear exactly once, and the
// Merfolk spell still resolves beneath the trigger (it enters the
// battlefield). The election is replay-stable: a clone answered identically
// produces a byte-identical event stream.
func TestMerrowReejereyTapOrUntapPosesTheChoice(t *testing.T) {
	t.Parallel()
	e, _, bear, mfst := taporuntapSetup(t, true)
	if !e.G.Obj(bear).Tapped {
		t.Fatal("pre-setup: the bear should be tapped")
	}
	de := taporuntapCast(t, e, mfst, bear)
	untapIdx := taporuntapOption(t, de, "untap")
	tapIdx := taporuntapOption(t, de, "tap")
	if de.Options[0].Kind != "untap" {
		t.Fatalf("option 0 = %+v, want the state-changing untap on a tapped target", de.Options[0])
	}
	if untapIdx != 0 || tapIdx != 1 {
		t.Fatalf("option order = %+v, want untap(0) then tap(1)", de.Options)
	}

	clone := e.Clone()
	for _, engine := range []*Engine{e, clone} {
		// The clone carries the same pending election (same Seq and options);
		// both engines answer identically and must produce identical streams.
		submitChoices(t, engine, untapIdx)
		drivePastResolution(t, engine, 1)
		if engine.G.Obj(bear).Tapped {
			t.Fatal("the answered untap election left the bear tapped")
		}
		// The cast spell resolves beneath the trigger: the Merfolk enters.
		if got := engine.G.Obj(mfst); got == nil || got.Zone != state.ZBattlefield {
			t.Fatalf("Merfolk zone = %+v, want the cast spell to resolve onto the battlefield", got)
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the tap-or-untap election")
	}
	drivePastResolution(t, e, 1)
}

// TestTapOrUntapElectionTapsTheUntappedTarget is the tap shape: the bear is
// untapped, option 0 is the state-changing "tap", answering it taps the bear.
func TestTapOrUntapElectionTapsTheUntappedTarget(t *testing.T) {
	t.Parallel()
	e, _, bear, mfst := taporuntapSetup(t, false)
	de := taporuntapCast(t, e, mfst, bear)
	if de.Options[0].Kind != "tap" {
		t.Fatalf("option 0 = %+v, want the state-changing tap on an untapped target", de.Options[0])
	}
	submitChoices(t, e, taporuntapOption(t, de, "tap"))
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the answered tap election left the bear untapped")
	}
	drivePastResolution(t, e, 1)
}
