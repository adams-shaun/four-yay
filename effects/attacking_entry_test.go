package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func attackingFixture(t *testing.T) (*fakeHost, *Ctx, state.ObjID) {
	h, c := fixtureHost(t)
	id := state.ObjID(1)
	o := h.g.Obj(id)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
	return h, c, id
}

func TestAttackingEntryMarksDefender(t *testing.T) {
	h, c, id := attackingFixture(t)
	c.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	rider := classifyAttackingEntry(c, &cards.SA{Params: map[string]string{"Attacking": "True"}}, state.ZBattlefield)
	rider.apply(h, c, id, 0, state.ZBattlefield)
	o := h.g.Obj(id)
	if o == nil || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("object attacking=%v defender=%d, want defender 1", o.IsAttacking, o.Attacking)
	}
	if len(h.log) != 2 || h.log[1].Kind != events.TokenAttacks {
		t.Fatalf("log = %+v, want one TokenAttacks after setup", h.log)
	}
}

func TestAttackingEntryWithoutDefenderDegradesOnce(t *testing.T) {
	h, c, id := attackingFixture(t)
	rider := classifyAttackingEntry(c, &cards.SA{Params: map[string]string{"Attacking": "True"}}, state.ZBattlefield)
	rider.apply(h, c, id, 0, state.ZBattlefield)
	o := h.g.Obj(id)
	if o == nil || !o.Tapped || o.IsAttacking {
		t.Fatalf("object = %+v, want it tapped and not attacking", o)
	}
	notes := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Attacking$") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("Attacking notes = %d, want 1; log=%+v", notes, h.log)
	}
}

// The shared settle path and the movers that route through it: a hand-origin
// settle emits the tapped entry (the retired loud Note's replacement), and
// the hidden-pick and Defined-library movers -- both routed through shared
// code that now applies the classified rider -- deliver the attacking entry on
// top of their own Tap. The fake host cannot ask, so every pick runs through
// the R-9 deterministic stand-in; the entry rider is what is under test.
func TestAttackingEntryRidesTheSharedSettleAndMovers(t *testing.T) {
	// (a) hidden-pick mover: effHiddenPick's settle call carries the entry.
	h, c := fixtureHost(t)
	c.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: 2, From: state.ZBattlefield, To: state.ZGraveyard})
	sa := &cards.SA{Params: map[string]string{"Origin": "Graveyard", "ChangeType": "Card",
		"Mandatory": "True", "Tapped": "True", "Attacking": "True"}}
	effHiddenPick(h, c, sa, ChangeZoneOf(sa), state.ZBattlefield, []state.Zone{state.ZGraveyard}, false, true, "Graveyard")
	if o := h.g.Obj(2); o == nil || o.Zone != state.ZBattlefield || !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("hidden pick mover left object = %+v, want battlefield, tapped, attacking 1", o)
	}
	if attacks := countTokenAttacks(h, 2); attacks != 1 {
		t.Fatalf("hidden pick mover logged %d TokenAttacks, want exactly 1 (the settle is the only emitter)", attacks)
	}

	// (b) hand-origin settle, Tapped$ True with no Attacking$: the retired
	// Note's tapped half is the real entry state now -- exactly one Tap.
	h2, c2 := fixtureHost(t)
	h2.Emit(events.Event{Kind: events.MoveZone, Obj: 2, From: state.ZBattlefield, To: state.ZHand})
	sa2 := &cards.SA{Params: map[string]string{"Tapped": "True"}}
	settleChangeZoneMoveAs(h2, c2, sa2, ChangeZoneOf(sa2),
		2, state.ZHand, state.ZBattlefield, "", 0, 1, true, nil)
	if o := h2.g.Obj(2); o == nil || o.Zone != state.ZBattlefield || !o.Tapped || o.IsAttacking {
		t.Fatalf("hand settle left object = %+v, want battlefield, tapped, NOT attacking", o)
	}
	taps := 0
	for _, ev := range h2.log {
		if ev.Kind == events.Tap && ev.Text == "entered tapped" {
			taps++
		}
	}
	if taps != 1 {
		t.Fatalf("hand settle logged %d entry Taps, want exactly 1; log=%+v", taps, h2.log)
	}

	// (c) hand-origin settle with both riders: Tap + TokenAttacks, the
	// attacking half naming the trigger defender.
	h3, c3 := fixtureHost(t)
	c3.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	h3.Emit(events.Event{Kind: events.MoveZone, Obj: 2, From: state.ZBattlefield, To: state.ZHand})
	sa3 := &cards.SA{Params: map[string]string{"Tapped": "True", "Attacking": "True"}}
	rider3 := classifyAttackingEntry(c3, sa3, state.ZBattlefield)
	settleChangeZoneMoveAs(h3, c3, sa3, ChangeZoneOf(sa3), 2, state.ZHand, state.ZBattlefield, "", 0, 1, true, &rider3)
	if o := h3.g.Obj(2); o == nil || !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("hand settle with Attacking$ left object = %+v, want tapped and attacking 1", o)
	}
	var attacks int
	for _, ev := range h3.log {
		if ev.Kind == events.TokenAttacks && ev.Obj == 2 {
			attacks++
		}
	}
	if attacks != 1 {
		t.Fatalf("hand settle logged %d TokenAttacks, want exactly 1; log=%+v", attacks, h3.log)
	}

	// (d) the Defined$ library mover: the fetch path carries the rider too.
	h4, c4 := fixtureHost(t)
	c4.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	c4.Remembered = []state.Target{{Obj: 2}}
	h4.Emit(events.Event{Kind: events.MoveZone, Obj: 2, From: state.ZBattlefield, To: state.ZLibrary})
	sa4 := &cards.SA{Params: map[string]string{
		"Defined": "Remembered", "Tapped": "True", "Attacking": "True"}}
	if !moveDefinedLibraryObjects(h4, c4, sa4, ChangeZoneOf(sa4), state.ZBattlefield) {
		t.Fatal("moveDefinedLibraryObjects declined the Remembered fetch")
	}
	if o := h4.g.Obj(2); o == nil || o.Zone != state.ZBattlefield || !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("library fetch mover left object = %+v, want battlefield, tapped, attacking 1", o)
	}
	if attacks := countTokenAttacks(h4, 2); attacks != 1 {
		t.Fatalf("library fetch mover logged %d TokenAttacks, want exactly 1 (settleChangeZoneMove routes through the settle's own emitter)", attacks)
	}

	// (e) the ChangeZoneAll sweep: emitMove carries the rider too.
	h5, c5 := fixtureHost(t)
	c5.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	h5.Emit(events.Event{Kind: events.MoveZone, Obj: 2, From: state.ZBattlefield, To: state.ZGraveyard})
	effChangeZoneAll(h5, c5, &cards.SA{Params: map[string]string{"Origin": "Graveyard",
		"Destination": "Battlefield", "Tapped": "True", "Attacking": "True"}})
	if o := h5.g.Obj(2); o == nil || o.Zone != state.ZBattlefield || !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("ChangeZoneAll sweep left object = %+v, want battlefield, tapped, attacking 1", o)
	}
	if attacks := countTokenAttacks(h5, 2); attacks != 1 {
		t.Fatalf("ChangeZoneAll sweep logged %d TokenAttacks, want exactly 1", attacks)
	}
}

// countTokenAttacks counts the TokenAttacks events naming obj in the host log.
func countTokenAttacks(h *fakeHost, obj state.ObjID) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.TokenAttacks && ev.Obj == obj {
			n++
		}
	}
	return n
}

func TestAttackingEntryNonTrueSelectorDegradesOnce(t *testing.T) {
	h, c, id := attackingFixture(t)
	rider := classifyAttackingEntry(c, &cards.SA{Params: map[string]string{"Attacking": "Remembered"}}, state.ZBattlefield)
	rider.apply(h, c, id, 0, state.ZBattlefield)
	if h.g.Obj(id).IsAttacking {
		t.Fatal("unsupported selector made the object attack")
	}
	if len(h.log) != 2 || h.log[1].Kind != events.Note {
		t.Fatalf("log = %+v, want one Note after setup", h.log)
	}
	if !strings.Contains(h.log[1].Text, "Remembered") {
		t.Fatalf("Note = %q, want unsupported selector Remembered", h.log[1].Text)
	}
}

// countAttackingNotes counts the rider's diagnostic Notes in h's log.
func countAttackingNotes(h *fakeHost) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Attacking$") {
			n++
		}
	}
	return n
}

// The degrade is a property of the CALL, not of each moved object: a sweep
// that moves two permanents under a rider it cannot deliver emits ONE Note,
// the same single diagnostic effects/token.go's single-mint read gets for
// free. Both halves (no defender in context, and an unsupported selector
// value) are pinned, because both used to emit once per moved object.
func TestAttackingEntryMultiObjectNoDefenderNotesOnce(t *testing.T) {
	h, c := fixtureHost(t)
	for _, id := range []state.ObjID{1, 2} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	}
	effChangeZoneAll(h, c, &cards.SA{Params: map[string]string{"Origin": "Graveyard",
		"Destination": "Battlefield", "Attacking": "True"}})
	for _, id := range []state.ObjID{1, 2} {
		o := h.g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || !o.Tapped || o.IsAttacking {
			t.Fatalf("object %d = %+v, want battlefield, tapped, NOT attacking", id, o)
		}
	}
	if notes := countAttackingNotes(h); notes != 1 {
		t.Fatalf("no-defender Notes = %d over a two-object sweep, want exactly 1; log=%+v", notes, h.log)
	}
}

func TestAttackingEntryMultiObjectSelectorNotesOnce(t *testing.T) {
	h, c := fixtureHost(t)
	c.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	for _, id := range []state.ObjID{1, 2} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	}
	effChangeZoneAll(h, c, &cards.SA{Params: map[string]string{"Origin": "Graveyard",
		"Destination": "Battlefield", "Attacking": "Remembered"}})
	for _, id := range []state.ObjID{1, 2} {
		if o := h.g.Obj(id); o == nil || o.IsAttacking {
			t.Fatalf("object %d = %+v, want the unsupported selector to leave it non-attacking", id, o)
		}
	}
	if notes := countAttackingNotes(h); notes != 1 {
		t.Fatalf("selector Notes = %d over a two-object sweep, want exactly 1; log=%+v", notes, h.log)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Attacking$") &&
			!strings.Contains(ev.Text, "Remembered") {
			t.Fatalf("Note = %q, want it to name the unsupported selector", ev.Text)
		}
	}
}

// The Dig window shares the classification: a two-card Dig putting both cards
// onto the battlefield under a defenderless rider is still one Note.
func TestAttackingEntryDigNoDefenderNotesOnce(t *testing.T) {
	h, c := fixtureHost(t)
	card := mkCard(t, "Name:Digged\nTypes:Creature\nPT:1/1\nOracle:x\n")
	var dug []state.ObjID
	for i := 0; i < 2; i++ {
		o := h.g.AddObject(card, 0)
		dug = append(dug, o.ID)
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZLibrary, Player: 0})
	}
	effDig(h, c, &cards.SA{Params: map[string]string{"Defined": "You", "DigNum": "2", "ChangeNum": "2",
		"DestinationZone": "Battlefield", "Attacking": "True"}})
	for _, id := range dug {
		o := h.g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || !o.Tapped || o.IsAttacking {
			t.Fatalf("dug object %d = %+v, want battlefield, tapped, NOT attacking", id, o)
		}
	}
	if notes := countAttackingNotes(h); notes != 1 {
		t.Fatalf("Dig no-defender Notes = %d over a two-card take, want exactly 1; log=%+v", notes, h.log)
	}
}
