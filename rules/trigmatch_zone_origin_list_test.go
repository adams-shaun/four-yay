package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Origin$ zone-set coverage for the ChangesZone / ChangesZoneAll matchers
// (ticket agent-20260930T005535Z-b621b37a): a comma-list Origin$ is a zone
// set read with effects.ParseZones, not a single zone word. The corpus
// carriers are Syr Konrad, the Grim ("from anywhere other than the
// battlefield" = Hand,Graveyard,Exile,Stack,Library,Command) and Laelia, the
// Blade Reforged (ChangesZoneAll Origin$ Library,Graveyard). The scripts
// below are written inline from the printed Oracle text -- never read from
// the GPL corpus.
//
// The Konrad watcher carries all three printed clauses so each test can
// assert the TOTAL queue is what the printed card allows, not merely that
// one face fired:
//
//	face 1: Origin$ Hand,Graveyard,Exile,Stack,Library,Command |
//	        Destination$ Graveyard | Creature.Other
//	face 2: Origin$ Battlefield | Destination$ Graveyard | Creature.Other
//	face 3: Origin$ Graveyard | Destination$ Any |
//	        Card.Creature+Other+YouOwn
const konradOriginListScript = `Name:Konrad Origin Watcher
ManaCost:3 B B
Types:Legendary Creature Human Knight
PT:5/4
T:Mode$ ChangesZone | Origin$ Hand,Graveyard,Exile,Stack,Library,Command | Destination$ Graveyard | ValidCard$ Creature.Other | TriggerZones$ Battlefield | Execute$ TrigDmg
T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature.Other | TriggerZones$ Battlefield | Secondary$ True | Execute$ TrigDmg
T:Mode$ ChangesZone | Origin$ Graveyard | Destination$ Any | ValidCard$ Card.Creature+Other+YouOwn | TriggerZones$ Battlefield | Secondary$ True | Execute$ TrigDmg
SVar:TrigDmg:DB$ DealDamage | Defined$ Player.Opponent | NumDmg$ 1
Oracle:x
`

// konradFace1OnlyScript carries ONLY the multi-origin face: a battlefield
// death must queue nothing on it (the negative).
const konradFace1OnlyScript = `Name:Konrad Face1 Watcher
ManaCost:3 B B
Types:Legendary Creature Human Knight
PT:5/4
T:Mode$ ChangesZone | Origin$ Hand,Graveyard,Exile,Stack,Library,Command | Destination$ Graveyard | ValidCard$ Creature.Other | TriggerZones$ Battlefield | Execute$ TrigDmg
SVar:TrigDmg:DB$ DealDamage | Defined$ Player.Opponent | NumDmg$ 1
Oracle:x
`

// The Laelia watcher carries her printed exile clause: ChangesZoneAll over
// Origin$ Library,Graveyard into Exile, one or more cards you own.
const laeliaOriginListScript = `Name:Laelia Origin Watcher
ManaCost:1 R W
Types:Creature Spirit
PT:1/1
T:Mode$ ChangesZoneAll | Origin$ Library,Graveyard | Destination$ Exile | ValidCards$ Card.YouOwn | TriggerZones$ Battlefield | Execute$ TrigPut
SVar:TrigPut:DB$ Pump | Defined$ Self | NumAtt$ 1 | NumDef$ 1
Oracle:x
`

const originListMoverScript = `Name:Grizzly Bear
ManaCost:1 G
Types:Creature Bear
PT:2/2
Oracle:x
`

// placeInZone does the eventless setup placement onBoardCard does for the
// battlefield, generalized to a hidden zone: the object carries the zone and
// sits in that zone's list before the test emits the move.
func placeInZone(t *testing.T, e *Engine, owner state.PlayerID, z state.Zone, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, owner)
	o.Zone = z
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(z, owner, append(e.G.Zone(z, owner), o.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1
	return o.ID
}

// konradWatcher places the full three-face watcher on seat 0's battlefield
// and asserts the preconditions the queue-count assertions stand on.
func konradWatcher(t *testing.T, e *Engine, script string) state.ObjID {
	t.Helper()
	src := onBoardCard(t, e, 0, card(t, script))
	if e.G.Obj(src) == nil || e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatalf("precondition: watcher %d not on the battlefield", src)
	}
	return src
}

// movedFrom emits the move and asserts the mover really started in from
// (the zone the matcher's Origin set is judged against).
func movedFrom(t *testing.T, e *Engine, mover state.ObjID, from, to state.Zone) {
	t.Helper()
	o := e.G.Obj(mover)
	if o == nil {
		t.Fatalf("precondition: mover %d missing before the move", mover)
	}
	if o.Zone != from {
		t.Fatalf("precondition: mover zone=%v, want %v", o.Zone, from)
	}
	if from == to {
		t.Fatalf("precondition: from %v == to %v; the assertion could never bind", from, to)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: mover, From: from, To: to, Player: o.Owner})
}

func queuedTriggers(t *testing.T, e *Engine, before int, want int, why string) {
	t.Helper()
	if got := len(e.pendingTriggers) - before; got != want {
		t.Fatalf("%s: queued %d triggers, want %d", why, got, want)
	}
}

func TestZoneOriginListCreatureToGraveyardFromHandFiresOnce(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	src := konradWatcher(t, e, konradOriginListScript)
	mover := placeInZone(t, e, 1, state.ZHand, card(t, originListMoverScript))
	if src == mover {
		t.Fatal("precondition: watcher and mover must be distinct objects")
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZHand, state.ZGraveyard)
	queuedTriggers(t, e, before, 1, "hand->graveyard creature card")
}

func TestZoneOriginListMilledCreatureIntoGraveyardFiresOnce(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	konradWatcher(t, e, konradOriginListScript)
	mover := placeInZone(t, e, 1, state.ZLibrary, card(t, originListMoverScript))
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZLibrary, state.ZGraveyard)
	queuedTriggers(t, e, before, 1, "library->graveyard (mill) creature card")
}

func TestZoneOriginListCreatureDeathFiresExactlyOnce(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	konradWatcher(t, e, konradOriginListScript)
	mover := placeInZone(t, e, 1, state.ZBattlefield, card(t, originListMoverScript))
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZBattlefield, state.ZGraveyard)
	// A real death queues exactly ONE trigger: the Origin$ Battlefield face.
	// Face 1 (anywhere other than the battlefield) must NOT fire on the same
	// event, so the printed "deals 1 damage" clause stays 1 damage, not 2.
	queuedTriggers(t, e, before, 1, "battlefield->graveyard death")
}

func TestZoneOriginListMultiOriginFaceStaysSilentOnBattlefieldDeath(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	konradWatcher(t, e, konradFace1OnlyScript)
	mover := placeInZone(t, e, 1, state.ZBattlefield, card(t, originListMoverScript))
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZBattlefield, state.ZGraveyard)
	// The negative: a zone NOT in the Origin list stays silent. Before the
	// fix this face degraded to graveyard-origin-only and still could not
	// fire here, so this test alone is not the fix's witness -- the hand and
	// library tests above are; it guards the boundary from regressing the
	// other way (a battlefield death double-draining).
	queuedTriggers(t, e, before, 0, "battlefield->graveyard against the multi-origin face")
}

func TestZoneOriginListCreatureLeavingOwnGraveyardFiresOnce(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	src := konradWatcher(t, e, konradOriginListScript)
	mover := placeInZone(t, e, 0, state.ZGraveyard, card(t, originListMoverScript))
	if owner := e.G.Obj(mover).Owner; owner != e.controllerOf(src) {
		t.Fatalf("precondition: mover owner %d, want the watcher's controller %d (YouOwn)", owner, e.controllerOf(src))
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZGraveyard, state.ZHand)
	// Face 3 (leaves your graveyard): exactly 1. Face 1 (Destination$
	// Graveyard) must not fire on a move INTO the hand.
	queuedTriggers(t, e, before, 1, "graveyard->hand own creature card")
}

func TestLaeliaOriginListExileFromLibraryFiresOnce(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card(t, laeliaOriginListScript))
	if e.G.Obj(src) == nil || e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatalf("precondition: watcher %d not on the battlefield", src)
	}
	mover := placeInZone(t, e, 0, state.ZLibrary, card(t, originListMoverScript))
	if owner := e.G.Obj(mover).Owner; owner != e.controllerOf(src) {
		t.Fatalf("precondition: mover owner %d, want the watcher's controller %d (YouOwn)", owner, e.controllerOf(src))
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZLibrary, state.ZExile)
	queuedTriggers(t, e, before, 1, "library->exile own card")
}

func TestLaeliaOriginListExileFromBattlefieldStaysSilent(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	konradWatcher(t, e, konradOriginListScript) // unrelated board presence
	src := onBoardCard(t, e, 0, card(t, laeliaOriginListScript))
	if e.G.Obj(src) == nil || e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatalf("precondition: watcher %d not on the battlefield", src)
	}
	mover := placeInZone(t, e, 0, state.ZBattlefield, card(t, originListMoverScript))
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZBattlefield, state.ZExile)
	queuedTriggers(t, e, before, 0, "battlefield->exile is outside Origin$ Library,Graveyard")
}
