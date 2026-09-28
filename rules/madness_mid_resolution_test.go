package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMadnessDiscardSuspendsTheResolvingRepeat pins cardfuzz batch8 line 1
// on real corpus cards: Kindle the Carnage ("Discard a card at random ...
// You may repeat this process any number of times") discards Violent
// Eruption, whose Madness replacement asks its owner whether to exile it.
// The madness ask was a plain e.ask, so the Repeat body kept running past
// the park and its "Repeat this process?" election displaced the madness
// question. The answer was never taken: the parked discard never moved,
// Violent Eruption stayed in hand, and every repeat "discarded" it again
// (4 damage to each creature per pass) -- a bot saying yes looped forever.
//
// The madness ask must suspend the resolution: it is the pending decision,
// its answer moves the card, and only then does the Repeat election come.
func TestMadnessDiscardSuspendsTheResolvingRepeat(t *testing.T) {
	t.Parallel()
	kindle := tokenReplCorpusCard(t, "Kindle the Carnage")
	eruption := tokenReplCorpusCard(t, "Violent Eruption")
	bear := tokenReplCorpusCard(t, "Grizzly Bears")
	e, cfg := tokenReplGame(t, 9181, kindle, eruption, bear)
	bearID := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
	kindleID := moveSeededCard(t, e, 0, kindle, state.ZHand)
	eruptionID := moveSeededCard(t, e, 0, eruption, state.ZHand)
	e.priorityRound()
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	// Empty the rest of the hand so the random discard can only take
	// Violent Eruption.
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...) {
		if id != kindleID && id != eruptionID {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}

	floatMana(t, e, 0, "RRR")
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == kindleID {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Kindle the Carnage not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	for i := 0; i < 4; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		passPriorityOnce(t, e)
	}

	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) == 0 || d.Options[0].Kind != "madness_exile" {
		t.Fatalf("pending after the random discard = %+v, want the madness replacement ask", d)
	}
	graveyard := -1
	for _, o := range d.Options {
		if o.Kind == "madness_graveyard" {
			graveyard = o.Index
		}
	}
	submitChoices(t, e, graveyard)
	if o := e.G.Obj(eruptionID); o.Zone != state.ZGraveyard {
		t.Fatalf("Violent Eruption zone = %v after declining madness, want graveyard", o.Zone)
	}
	if got := e.G.Obj(bearID).Damage; got != 4 {
		t.Fatalf("Grizzly Bears damage = %d, want 4 (Violent Eruption's mana value)", got)
	}

	// The Repeat election comes only now; saying yes with an empty hand
	// changes nothing, so the process ends and the spell leaves the stack.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Repeat this process?" {
		t.Fatalf("pending after the madness answer = %+v, want the Repeat election", d)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(kindleID); o.Zone != state.ZGraveyard {
		t.Fatalf("Kindle the Carnage zone = %v, want it resolved into the graveyard; pending %+v", o.Zone, e.Pending())
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty", e.G.Stack)
	}
	replayCheck(t, e, cfg)
}

// TestGatedRepeatStopsWhenAnIterationChangesNothing pins cardfuzz batch8
// line 4 on the real corpus card: Rally the Horde ("Exile the top card of
// your library" three times; "If the last card exiled isn't a land card,
// repeat this process") resolved over an EMPTY library. Nothing is exiled,
// so the land-count gate keeps holding, and every pass logged only a Note
// and an Imprint "clear" of an already-empty list -- 1000 identical passes,
// which the livelock watcher killed. An empty clear is no longer an event,
// and a gated repeat whose pass posed nothing and changed nothing stops.
func TestGatedRepeatStopsWhenAnIterationChangesNothing(t *testing.T) {
	t.Parallel()
	rally := tokenReplCorpusCard(t, "Rally the Horde")
	e, cfg := tokenReplGame(t, 9182, rally)
	rallyID := moveSeededCard(t, e, 0, rally, state.ZHand)
	e.priorityRound()
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	floatMana(t, e, 0, "RRRRRR")
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == rallyID {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Rally the Horde not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	mark := len(e.L.Events)
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		passPriorityOnce(t, e)
	}
	if o := e.G.Obj(rallyID); o.Zone != state.ZGraveyard || len(e.G.Stack) != 0 {
		t.Fatalf("Rally the Horde zone = %v, stack %v: want it resolved", o.Zone, e.G.Stack)
	}
	stops, clears := 0, 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && ev.Text == "the repeated process changed nothing; it is not repeated again" {
			stops++
		}
		if ev.Kind == events.Imprint && ev.Text == "clear" && ev.Obj == rallyID {
			clears++
		}
	}
	if stops != 1 || clears != 0 {
		t.Fatalf("no-progress stops = %d, empty imprint clears = %d; want 1 and 0", stops, clears)
	}
	replayCheck(t, e, cfg)
}
