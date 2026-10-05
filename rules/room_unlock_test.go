package rules

// trig:FullyUnlock's leaf (task agent-20260919T191104Z-95f1e316): the Eerie
// enchantments' "whenever ... you fully unlock a Room" half, driven by the
// REAL corpus Fear of Sleep Paralysis and the real two-door Defiled Crypt /
// Cadaver Lab. The tests live in their own file (never appended to the Room
// leaves in mass_primitives_test.go) so a concurrent ticket cannot collide on
// the same test file.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestFearOfSleepParalysisFullyUnlockTapsOnceOnFinalDoor is the leaf: after
// the room's last locked door is unlocked, Fear of Sleep Paralysis's Mode$
// FullyUnlock trigger fires exactly once -- one target ask, one tap and one
// stun counter -- on top of whatever its Eerie enters-the-battlefield half
// did. The two-face Room model has exactly one locked door while locked, so
// "the last locked door" is that door; the test asserts that precondition
// (roomLockedFace non-nil, Unlocked false) before unlocking.
func TestFearOfSleepParalysisFullyUnlockTapsOnceOnFinalDoor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Fear of Sleep Paralysis"), lookup(t, reg, "Dazzling Theater")},
		[]*cards.Card{bear})

	// Prop Room's locked door is {2}{W}, and neither face carries an
	// UnlockDoor trigger, so the only trigger on the final unlock is the
	// FullyUnlock line under test. Fund the unlock up front (at the clean
	// Main1), so every priority decision built after the fixture is placed
	// already offers the unlock.
	addMana(t, e, 0, "WWWW")

	// The room enters first, with NO Eerie permanent yet watching, so the
	// only FullyUnlock carrier in play is added afterwards.
	room := moveByName(t, e, 0, "Dazzling Theater", state.ZBattlefield)
	designateRoomCastFace(e, room)
	bearID := moveByName(t, e, 1, "Bear", state.ZBattlefield)
	fear := moveByName(t, e, 0, "Fear of Sleep Paralysis", state.ZBattlefield)

	// Precondition: the room really is the modeled two-door state -- cast
	// face unlocked, exactly one locked alternate door, not fully unlocked.
	ro := e.G.Obj(room)
	if ro == nil || ro.Zone != state.ZBattlefield || !isRoom(ro) {
		t.Fatalf("room is not a battlefield Room: %+v", ro)
	}
	if ro.Unlocked {
		t.Fatal("precondition: the directly-placed room entered already fully unlocked")
	}
	if roomLockedFace(ro) == nil {
		t.Fatal("precondition: the room has no locked alternate door, so no DoorUnlock can fully unlock it")
	}
	if got := ro.Card.Faces[1-int(ro.FaceIdx)].Name; got != "Prop Room" {
		t.Fatalf("precondition: locked door is %q, want Prop Room", got)
	}
	// Precondition: the trigger source is the Eerie permanent, on the
	// battlefield, and it really carries a Mode$ FullyUnlock line.
	fo := e.G.Obj(fear)
	if fo == nil || fo.Zone != state.ZBattlefield || fo.Face() == nil || fo.Face().Name != "Fear of Sleep Paralysis" {
		t.Fatalf("precondition: Eerie carrier is not the battlefield Fear of Sleep Paralysis: %+v", fo)
	}
	found := false
	for _, tr := range fo.Face().Triggers {
		if tr.Mode == "FullyUnlock" {
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: Fear of Sleep Paralysis carries no Mode$ FullyUnlock trigger")
	}

	// Resolve the Eerie enters-the-battlefield half (the card or another
	// enchantment entering). Decline its optional target, so the Bear is
	// untouched by the entry trigger and the only possible source of a tap
	// is the unlock trigger under test.
	passToKind(t, e, decision.KTarget)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Eerie ETB did not pose its target ask: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("decline the Eerie ETB target: %v", err)
	}
	answerQuiet(t, e, 60)
	// Precondition for the comparison below: the Bear starts untapped with no
	// stun counter, so "tapped with one stun" after the unlock is a real
	// difference the trigger alone produced.
	if b := e.G.Obj(bearID); b == nil || b.Tapped || b.Counter("STUN") != 0 {
		t.Fatalf("precondition: Bear is not an untapped, counter-free target: %+v", b)
	}

	// Unlock the room's last locked door (Prop Room, {2}{W}) as a sorcery.
	pd := e.Pending()
	if pd == nil || pd.Kind != decision.KPriority {
		t.Fatalf("no priority window to unlock the door: %+v", pd)
	}
	unlock := -1
	for _, o := range pd.Options {
		if o.Kind == "unlock" && o.Obj == room && o.Label == "Unlock Prop Room" {
			unlock = o.Index
		}
	}
	if unlock < 0 {
		t.Fatalf("no Prop Room unlock option in %+v", pd.Options)
	}
	if err := e.Submit(decision.Intent{Seq: pd.Seq, Player: pd.Player, Choices: []int{unlock}}); err != nil {
		t.Fatalf("submit Prop Room unlock: %v", err)
	}
	// The unlock's FullyUnlock trigger drains once both seats pass priority;
	// only then is its placement target decision posed.
	passToKind(t, e, decision.KTarget)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("FullyUnlock did not pose its target ask: %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("FullyUnlock did not offer the opposing Bear: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit FullyUnlock target: %v", err)
	}
	answerQuiet(t, e, 60)

	b := e.G.Obj(bearID)
	if !b.Tapped {
		t.Fatal("FullyUnlock did not tap the chosen creature")
	}
	if got := b.Counter("STUN"); got != 1 {
		t.Fatalf("FullyUnlock put %d stun counters on the target, want exactly 1", got)
	}
	if !e.G.Obj(room).Unlocked {
		t.Fatal("the unlock answer did not fully unlock the room")
	}
	if roomLockedFace(e.G.Obj(room)) != nil {
		t.Fatal("a locked door remains after the unlock, so the room is not fully unlocked")
	}

	// The two-face model has no distinct non-final Room-door transition: a
	// single Unlocked bool means the one DoorUnlock of a locked room is the
	// full unlock. The non-triggering case the model DOES have is a repeated
	// DoorUnlock on an already-unlocked room -- no game action emits one (the
	// unlock offer's unlockRoomCost is nil without a locked face), so it is
	// driven directly. It must not fire FullyUnlock again.
	before := len(e.pendingTriggers)
	e.emit(events.Event{Kind: events.DoorUnlock, Obj: room})
	if len(e.pendingTriggers) != before {
		t.Fatalf("a repeated DoorUnlock on the already-unlocked room queued %d trigger(s), want 0",
			len(e.pendingTriggers)-before)
	}
	answerQuiet(t, e, 30)
	if got := e.G.Obj(bearID).Counter("STUN"); got != 1 {
		t.Fatalf("a repeated DoorUnlock fired FullyUnlock again: stun counters now %d, want 1", got)
	}
}
