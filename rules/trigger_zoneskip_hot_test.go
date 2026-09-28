package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// hotSubsetBoard is n vanilla creatures plus one watcher whose face carries a
// battlefield trigger -- the mass-token shape (a Krenko doubling beside one
// trigger source). onBoard is eventless, so the walk's zone summary starts
// unclassified and the first walk classifies the battlefield.
func hotSubsetBoard(tb testing.TB, n int) (*Engine, state.ObjID) {
	tb.Helper()
	e := layerEngine(tb)
	for i := 0; i < n; i++ {
		onBoard(tb, e, 0, fmt.Sprintf("Name:Token %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
	}
	w := onBoard(tb, e, 0, "Name:Watcher\nTypes:Creature\nPT:1/1\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigLose\n"+
		"SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You\nOracle:x\n")
	return e, w
}

// TestTriggerWalkVisitsOnlyHotObjects pins the battlefield hot-subset skip
// (trigger_zoneskip.go): with thousands of vanilla tokens beside one trigger
// source, a walk for an event with no referent must visit the source alone,
// not every token. The battlefield carries a summary like the hidden-ish
// zones, and its hot subset is the objects whose face can trigger from it.
//
// Fails without the fix: before the battlefield was summarized and its hot
// subset stored, the walk called fn once per battlefield object, so visited
// equalled the board size.
func TestTriggerWalkVisitsOnlyHotObjects(t *testing.T) {
	t.Parallel()
	const n = 500
	e, watcher := hotSubsetBoard(t, n)
	board := len(e.G.Zone(state.ZBattlefield, 0))
	if board < n {
		t.Fatalf("precondition broken: battlefield holds %d objects, want at least %d", board, n)
	}
	// Precondition: the watcher really is hot (its face can trigger from the
	// battlefield), so a correct walk must visit it. A board that made the
	// watcher cold would make the count assertion vacuous.
	if !e.objectTriggerHotIn(e.G.Obj(watcher), trigZoneSlot(state.ZBattlefield)) {
		t.Fatal("precondition broken: the watcher is not classified hot on the battlefield")
	}
	// Precondition: a vanilla token is cold, so it is a genuine skip target.
	tok := e.G.Zone(state.ZBattlefield, 0)[0]
	if e.objectTriggerHotIn(e.G.Obj(tok), trigZoneSlot(state.ZBattlefield)) {
		t.Fatal("precondition broken: a vanilla token is classified hot")
	}

	var visited int
	e.pendingTriggers = e.pendingTriggers[:0]
	e.forEachTriggerObject(events.Event{Kind: events.Note}, true, func(state.ObjID) { visited++ }, nil)

	if visited >= board {
		t.Fatalf("the walk visited %d of %d battlefield objects; want only the hot subset", visited, board)
	}
	if visited > 4 {
		t.Fatalf("the walk visited %d objects; want the handful of hot ones", visited)
	}
}

// TestTriggerWalkHotSubsetStillFiresTheWatcher is the semantic half: pruning
// cold tokens must never drop the hot watcher's trigger. The watcher's
// enter trigger queues exactly once on its own battlefield entry.
func TestTriggerWalkHotSubsetStillFiresTheWatcher(t *testing.T) {
	t.Parallel()
	e, watcher := hotSubsetBoard(t, 300)
	e.pendingTriggers = e.pendingTriggers[:0]
	e.checkFaceTriggers(e, events.Event{Kind: events.MoveZone, Obj: watcher,
		From: state.ZLibrary, To: state.ZBattlefield}, nil, 0, 0, false, false, false)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("hot watcher queued %d triggers, want exactly its own enter trigger", len(e.pendingTriggers))
	}
	if e.pendingTriggers[0].Source != watcher {
		t.Fatalf("queued trigger source = %d, want the watcher %d", e.pendingTriggers[0].Source, watcher)
	}
}

// TestTriggerWalkStepChangeStillFiresGrantedCumulativeUpkeep is the
// counterweight to the battlefield hot-subset skip: checkGrantedCumulative
// UpkeepTriggers synthesizes a battlefield Phase trigger from a DERIVED
// keyword the face hot test cannot see, so a StepChange must walk the whole
// battlefield and still queue it even when the granted-CU creature sits
// among thousands of cold vanilla tokens. Without the StepChange full-walk,
// the hot-subset skip prunes the granted creature and drops its upkeep.
func TestTriggerWalkStepChangeStillFiresGrantedCumulativeUpkeep(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// A self-granting layer-6 static: the source grants ITSELF the derived
	// keyword, so the filler tokens stay cold.
	const selfGrant = "Name:Cumulus Relic\nManaCost:0\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Cumulative upkeep:2 | Description$ CARDNAME has cumulative upkeep {2}.\nOracle:x\n"
	// n cold vanilla tokens beside the self-granting permanent.
	for i := 0; i < 400; i++ {
		onBoard(t, e, 0, fmt.Sprintf("Name:Filler %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
	}
	bear := onBoard(t, e, 0, selfGrant)
	// Preconditions: the keyword is DERIVED (not printed, so the face hot
	// test cannot see it), the self-grant is live, and the permanent is
	// classified COLD by the face test -- i.e. the hot-subset skip would drop
	// it if the StepChange did not force the full battlefield walk.
	bearObj := e.G.Obj(bear)
	if bearObj == nil || bearObj.Zone != state.ZBattlefield {
		t.Fatal("precondition: the CU-granted permanent is not on the battlefield")
	}
	if bearObj.Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: the permanent prints cumulative upkeep; it must only be granted")
	}
	if !e.HasKeyword(bear, "Cumulative upkeep") {
		t.Fatal("precondition: the granted cumulative upkeep is not in the derived keyword list")
	}
	if e.objectTriggerHotIn(bearObj, trigZoneSlot(state.ZBattlefield)) {
		t.Fatal("precondition: the granted-CU permanent is classified hot by the face test; the test proves nothing")
	}
	e.G.Active, e.G.Priority = 0, 0
	e.pendingTriggers = e.pendingTriggers[:0]
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	var cu int
	for _, pt := range e.pendingTriggers {
		if pt.Cumulative != "" {
			cu++
			if pt.Source != bear {
				t.Fatalf("cumulative-upkeep trigger source = %d, want the granted permanent %d", pt.Source, bear)
			}
		}
	}
	if cu != 1 {
		t.Fatalf("StepChange queued %d granted cumulative-upkeep triggers, want exactly 1", cu)
	}
}
