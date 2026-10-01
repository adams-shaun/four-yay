package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFaceTriggerZonesIsZoneGatesSpec pins the per-face zone mask to
// zoneGate's own spec resolution for a non-referent source: TriggerZones$,
// then ActiveZones$, then the Battlefield default; an unresolvable Phase$
// marks every zone (its diagnostic is zone-independent). The battlefield is
// itself a summarized zone (trigger_zoneskip.go) so a battlefield-default
// trigger sets its bit rather than reading cold everywhere.
func TestFaceTriggerZonesIsZoneGatesSpec(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	const lib, hand, bf, gy, ex, stack, cmd = 1 << 0, 1 << 1, 1 << 2, 1 << 3, 1 << 4, 1 << 5, 1 << 6
	for _, tc := range []struct {
		trig string
		want uint8
	}{
		{"Mode$ Phase | Phase$ Upkeep | Execute$ X", bf},
		{"Mode$ Phase | Phase$ Upkeep | TriggerZones$ Graveyard | Execute$ X", gy},
		{"Mode$ Phase | Phase$ Upkeep | TriggerZones$ Library,Exile | Execute$ X", lib | ex},
		{"Mode$ Phase | Phase$ Upkeep | ActiveZones$ Hand | Execute$ X", hand},
		{"Mode$ Phase | Phase$ Upkeep | TriggerZones$ Battlefield, | Execute$ X", bf | gy}, // the literal Battlefield plus a phantom empty part that parses to the graveyard, as in zoneGate
		{"Mode$ Phase | Phase$ Upkeep | TriggerZones$ Command | Execute$ X", cmd},
		{"Mode$ Phase | Phase$ NoSuchStep | Execute$ X", lib | hand | bf | gy | ex | stack | cmd},
	} {
		c := card(t, "Name:Probe\nManaCost:B\nTypes:Creature\nPT:1/1\nT:"+tc.trig+"\nSVar:X:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
		if got := e.faceTriggerZones(c.Faces[0]); got != tc.want {
			t.Errorf("%q: zones = %07b, want %07b", tc.trig, got, tc.want)
		}
	}
}

// TestTrigZoneSkipLibrariesColdGraveyardHot: plain libraries are cold and
// skipped, while a graveyard holding a TriggerZones$ Graveyard card is hot
// and still fires (the skip never hides a live trigger).
func TestTrigZoneSkipLibrariesColdGraveyardHot(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, `Name:Ghoul
ManaCost:B
Types:Creature Zombie
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | TriggerZones$ Graveyard | Execute$ TrigLose | TriggerDescription$ x
SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the graveyard trigger", e.G.Stack)
	}
	for p := range e.G.Players {
		s := e.trigZones[p*trigZoneSlots+trigZoneSlot(state.ZLibrary)]
		if !s.valid || s.hot {
			t.Errorf("seat %d library summary = valid %v hot %v, want a valid cold summary", p, s.valid, s.hot)
		}
	}
	if s := e.trigZones[0*trigZoneSlots+trigZoneSlot(state.ZGraveyard)]; !s.valid || !s.hot {
		t.Errorf("seat 0 graveyard summary = valid %v hot %v, want hot", s.valid, s.hot)
	}
}

// TestTrigZoneSkipWalksReferentInColdZone: a cold zone still walks the
// event's own object -- zoneGate's source == ev.Obj admissions (here a
// "when you discard this card" with no TriggerZones$) live there.
func TestTrigZoneSkipWalksReferentInColdZone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	c := e.G.AddObject(card(t, `Name:Orvarish
ManaCost:B
Types:Creature
PT:1/1
T:Mode$ Discarded | ValidCard$ Card.Self | Execute$ TrigLose | TriggerDescription$ x
SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`), 0)
	c.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), c.ID))
	e.emit(events.Discard(c.ID, 0))
	if s := e.trigZones[0*trigZoneSlots+trigZoneSlot(state.ZGraveyard)]; !s.valid || s.hot {
		t.Fatalf("graveyard summary = valid %v hot %v, want cold (the face's trigger is battlefield-default)", s.valid, s.hot)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the discard trigger of the event's own object", e.G.Stack)
	}
}

// libraryTriggerFace is a face whose trigger functions from the library.
const libraryTriggerSrc = `Name:Lurker
ManaCost:B
Types:Creature
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | TriggerZones$ Library | Execute$ TrigLose | TriggerDescription$ x
SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`

// TestTrigZoneSkipSeesInPlaceChangeThroughReferent: an in-place face change
// of a library card (every such Apply write names the object in the event)
// drops the library's cold summary, so the now-live trigger fires.
func TestTrigZoneSkipSeesInPlaceChangeThroughReferent(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDraw})
	lib := e.G.Zone(state.ZLibrary, 0)
	if s := e.trigZones[trigZoneSlot(state.ZLibrary)]; !s.valid || s.hot {
		t.Fatalf("library summary before = valid %v hot %v, want cold", s.valid, s.hot)
	}
	o := e.G.Obj(lib[len(lib)/2])
	o.SetCopyFace(card(t, libraryTriggerSrc).Faces[0])
	e.emit(events.Event{Kind: events.Note, Obj: o.ID, Text: "in-place face change"})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the library trigger", e.G.Stack)
	}
}

// TestTrigZoneSkipVerifyCatchesUnreferencedWrite proves verify mode is live
// in the rules test binary: a direct in-place write no event names -- the
// one input the summary argument cannot see -- trips it rather than
// silently dropping the trigger.
func TestTrigZoneSkipVerifyCatchesUnreferencedWrite(t *testing.T) {
	t.Parallel()
	if !trigZoneSkipVerify {
		t.Fatal("trigger zone skip verify mode is off in the rules test binary")
	}
	e := layerEngine(t)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDraw})
	lib := e.G.Zone(state.ZLibrary, 0)
	e.G.Obj(lib[len(lib)/2]).SetCopyFace(card(t, libraryTriggerSrc).Faces[0])
	defer func() {
		r := recover()
		if s, ok := r.(string); !ok || !strings.Contains(s, "trigger zone skip passed over") {
			t.Fatalf("verify did not flag the skipped live trigger: %v", r)
		}
	}()
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
}

// TestTrigZoneSummariesCarryAcrossClone: a clone starts with the parent's
// summaries in its own arrays and catch-up position, keeps them valid
// through its first walk (a content match against its own lists), and the
// parent's are untouched by the clone's.
func TestTrigZoneSummariesCarryAcrossClone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDraw})
	lib := trigZoneSlot(state.ZLibrary)
	if !e.trigZones[lib].valid {
		t.Fatal("parent library summary not built")
	}
	c := e.Clone()
	if len(c.trigZones) != len(e.trigZones) || c.trigZonesEp != e.trigZonesEp || !c.trigZones[lib].valid {
		t.Fatal("clone did not carry the summaries")
	}
	if len(c.trigZones[lib].ids) > 0 && &c.trigZones[lib].ids[0] == &e.trigZones[lib].ids[0] {
		t.Fatal("clone shares the parent's summary ids")
	}
	c.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	if s := c.trigZones[lib]; !s.valid || !sameZoneList(s.live, c.G.Zone(state.ZLibrary, 0)) {
		t.Fatal("clone's walk did not confirm the carried library summary against its own list")
	}
	if sameZoneList(e.trigZones[lib].live, c.G.Zone(state.ZLibrary, 0)) {
		t.Fatal("the clone's walk wrote the parent's summary")
	}
}
