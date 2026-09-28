package rules

// Synth Infiltrator is the regression pin for the bare `Choices$ Creature`
// ETB-copy selector (follow-up to etbclone1): a real corpus card whose
// K:ETBReplacement:Copy body's filter admits ANY creature -- including, on
// the pre-etbclone1 standalone route, the entering object itself. The pin
// asserts the entry-boundary election the dependency owns: the candidate
// list comes off the battlefield scan (the entering spell is on the stack,
// so it can never be a template), the accepted copy carries the Bears 2/2
// body plus the AddTypes$ Artifact/Creature/Synth exceptions, and the event
// stream never shows a self-source ClonePermanent.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// synthFixture drives a real-corpus Synth Infiltrator (in seat 0's hand) to
// its entry-boundary copy election with a real Grizzly Bears on seat 0's
// battlefield, and returns the engine, replay config, both ids and the
// pending election. It asserts every precondition the caller's assertions
// stand on: the Bears template is a distinct 2/2 on the battlefield the
// copy-arm scan reads, Synth's printed 0/0 face is distinguishable from it,
// and the pending ask is the machinery's own etb election.
func synthFixture(t *testing.T) (*Engine, Config, state.ObjID, state.ObjID, *decision.Decision) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Synth Infiltrator")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	synth := searchMoveByName(t, e, "Synth Infiltrator", state.ZHand)

	// Preconditions: the template the scan will read is a distinct 2/2 on
	// the battlefield, the entering card is a 0/0 in the hand, and the two
	// faces are distinguishable in both name and P/T -- otherwise the
	// "copied the Bears, not itself" assertions below pass vacuously.
	bo, so := e.G.Obj(bear), e.G.Obj(synth)
	if bo == nil || so == nil {
		t.Fatalf("fixture missing: bear=%+v synth=%+v", bo, so)
	}
	if bear == synth {
		t.Fatal("bear and synth resolved to the same object id")
	}
	if bo.Zone != state.ZBattlefield || bo.Face() == nil || bo.Face().Name != "Grizzly Bears" {
		t.Fatalf("bear fixture wrong: zone=%s face=%+v", bo.Zone, bo.Face())
	}
	if so.Zone != state.ZHand || so.Face() == nil || so.Face().Name != "Synth Infiltrator" {
		t.Fatalf("synth fixture wrong: zone=%s face=%+v", so.Zone, so.Face())
	}
	if bd := e.Derived(bear); bd.Power != 2 || bd.Toughness != 2 {
		t.Fatalf("bear template is %d/%d, want the printed 2/2", bd.Power, bd.Toughness)
	}
	if so.Face().PT != "0/0" {
		t.Fatalf("synth printed PT %q, want 0/0 (distinguishable from the 2/2 template)", so.Face().PT)
	}

	addMana(t, e, 0, "CCCUU")
	castCardByName(t, e, synth)
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || d.Player != 0 {
		t.Fatalf("Synth's entry did not pose its etb election: %+v", d)
	}
	return e, cfg, synth, bear, d
}

// TestSynthInfiltratorEntryCopyElectionCopiesTheBear is the accept leaf: the
// election offers the battlefield bear and NOT the entering Synth; the
// accepted copy has the Bears 2/2 body with the Artifact/Creature/Synth
// exceptions riding AddTypes$; and no self-source ClonePermanent is emitted.
func TestSynthInfiltratorEntryCopyElectionCopiesTheBear(t *testing.T) {
	t.Parallel()
	e, cfg, synth, bear, d := synthFixture(t)

	bearIdx, declineIdx := -1, -1
	for _, o := range d.Options {
		if o.Kind != "clone" {
			continue
		}
		if o.Obj == bear {
			bearIdx = o.Index
		}
		if o.Obj == synth {
			t.Fatalf("the election offered the entering Synth itself as a template: %+v", d.Options)
		}
		if o.Obj == 0 && o.Label == "Enter as itself" {
			declineIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("the election did not offer the battlefield bear: %+v", d.Options)
	}
	if declineIdx < 0 {
		t.Fatalf("the Optional carrier's decline is not offered: %+v", d.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 40)

	// The copy: Synth sits on the battlefield with the Bears 2/2 body, the
	// Bear subtype, and the AddTypes$ exceptions on top.
	o := e.G.Obj(synth)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("synth after the accepted copy: %+v, want on the battlefield", o)
	}
	der := e.Derived(synth)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("copy P/T %d/%d, want the Bears' 2/2", der.Power, der.Toughness)
	}
	for _, want := range []string{"Bear", "Artifact", "Creature", "Synth"} {
		if !slices.Contains(der.Types, want) {
			t.Fatalf("copy types = %v, want %s among them (AddTypes$ exceptions + copied subtype)", der.Types, want)
		}
	}

	// Exactly no self-source ClonePermanent: every clone fold naming Synth
	// as the become object copied the bear, never Synth itself.
	sawCopy := false
	for _, ev := range e.L.Events {
		if ev.Kind != events.ClonePermanent || ev.Obj != synth {
			continue
		}
		if len(ev.IDs) > 0 && ev.IDs[0] == synth {
			t.Fatalf("self-source ClonePermanent emitted: %+v", ev)
		}
		if len(ev.IDs) > 0 && ev.IDs[0] == bear {
			sawCopy = true
		}
	}
	if !sawCopy {
		t.Fatal("no ClonePermanent folded the accepted bear copy")
	}
	if hasNote(e, "unimplemented API") {
		t.Fatal("the ETB copy fell back to the unimplemented-API note")
	}
	replayCheck(t, e, cfg)
}

// TestSynthInfiltratorEntryCopyDeclineEntersAsItself is the decline leaf:
// the Optional decline enters Synth as its printed 0/0 self, the toughness
// SBA removes it to the graveyard, and the event stream shows the
// self-entry and the SBA move with NO clone event manufactured.
func TestSynthInfiltratorEntryCopyDeclineEntersAsItself(t *testing.T) {
	t.Parallel()
	e, cfg, synth, _, d := synthFixture(t)

	declineIdx := -1
	for _, o := range d.Options {
		if o.Obj == synth {
			t.Fatalf("the election offered the entering Synth itself as a template: %+v", d.Options)
		}
		if o.Kind == "clone" && o.Obj == 0 && o.Label == "Enter as itself" {
			declineIdx = o.Index
		}
	}
	if declineIdx < 0 {
		t.Fatalf("decline option not offered: %+v", d.Options)
	}
	submitChoices(t, e, declineIdx)
	passUntilStackEmpty(t, e, 60)

	if hasEvent(e, events.ClonePermanent, synth) {
		t.Fatal("the declined election emitted ClonePermanent")
	}
	if hasNote(e, "unimplemented API") {
		t.Fatal("the decline fell back to the unimplemented-API note")
	}
	if o := e.G.Obj(synth); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("declined 0/0 should be in the graveyard (SBA), got %+v", o)
	}
	entry, sba := false, false
	for _, ev := range e.L.Events {
		if ev.Kind != events.MoveZone || ev.Obj != synth {
			continue
		}
		if ev.From == state.ZStack && ev.To == state.ZBattlefield {
			entry = true
		}
		if ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			sba = true
		}
	}
	if !entry || !sba {
		t.Fatalf("event stream missing the self-entry and/or SBA move: entry=%v sba=%v", entry, sba)
	}
	replayCheck(t, e, cfg)
}
