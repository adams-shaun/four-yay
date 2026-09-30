// api:Dig's zone-batch bracket (task agent-20260930T010244Z-5813227f).
//
// ONE api:Dig resolution is ONE zone-change action even when it moves
// several cards, so Mode$ ChangesZoneAll's "one or more" reading must
// observe the whole resolution as one batch. Before the fix effDig emitted
// one MoveZone event per taken card with no BeginZoneBatch/EndZoneBatch
// bracket, so a ChangesZoneAll payoff queued once per moved card: Wild
// Wasteland's "At the beginning of your upkeep, exile the top two cards of
// your library" put TWO +1/+1 counters on Laelia, the Blade Reforged where
// the printed text puts ONE.
//
// The pin is the real corpus carriers and observer:
//
//   - Wild Wasteland (enchantment), upkeep trigger executing
//     SVar:ExileTwo:DB$ Dig | DigNum$ 2 | ChangeNum$ All | DestinationZone$
//     Exile -- the exact two-card case;
//   - Laelia, the Blade Reforged (Mode$ ChangesZoneAll | Origin$
//     Library,Graveyard | Destination$ Exile), the observer card, plus her
//     OWN attack trigger (DigNum$ 1 exile) as the single-card control: one
//     move is exactly one batch, one counter;
//   - Scion of Halaster (SVar:DBDig:DB$ Dig | DigNum$ 2 | DestinationZone$
//     Graveyard) driving two creature cards into the graveyard under
//     Dreadhound (Mode$ ChangesZone | Origin$ Library | Destination$
//     Graveyard | ValidCard$ Creature), the per-move control: the singular
//     mode is never batch-scoped, so two moves are two firings, two life --
//     even while the Dig's batch bracket is open.
//
// Both observers' trigger lines and the carrier SAs are
// precondition-asserted, so a silent scan cannot make a counter or a life
// count vacuous.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// digBatchBoard builds the board both tests share: seat 0 has Laelia, Wild
// Wasteland, Dreadhound and Scion of Halaster on the battlefield and three
// Grizzly Bears on top of its library (ordered explicitly after setup, so
// Dreadhound's enter trigger cannot consume them); seat 1 is an untouched
// mountain deck whose life total the per-move control reads. The setup
// leaves no pending trigger behind.
func digBatchBoard(t *testing.T, reg *cards.Registry) (e *Engine, cfg Config,
	laelia, wasteland, scion, hound state.ObjID, bears []state.ObjID) {
	t.Helper()
	laeliaCard := mustCorpusCard(t, reg, "Laelia, the Blade Reforged")
	wastelandCard := mustCorpusCard(t, reg, "Wild Wasteland")
	scionCard := mustCorpusCard(t, reg, "Scion of Halaster")
	houndCard := mustCorpusCard(t, reg, "Dreadhound")
	bearCard := searchCorpusCard(t, reg, "Grizzly Bears")

	// The carriers enter the opening hand (drawn from the deck's top), the
	// bears sit at the BOTTOM until the explicit reorder below, so
	// Dreadhound's enter mill cannot touch them.
	deck0 := append([]*cards.Card{laeliaCard, wastelandCard, scionCard, houndCard},
		mountainDeck(t, 35)...)
	deck0 = append(deck0, bearCard, bearCard, bearCard)
	deck1 := mountainDeck(t, 40)
	cfg = seatZeroStart(Config{Seed: 4409, Names: []string{"reforger", "watcher"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e = New(cfg)
	e.Advance()

	place := func(c *cards.Card, p state.PlayerID) state.ObjID {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != p || o.Card != c {
				continue
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
			return o.ID
		}
		t.Fatalf("deck object for %q not found", c.Faces[0].Name)
		return 0
	}
	laelia = place(laeliaCard, 0)
	wasteland = place(wastelandCard, 0)
	scion = place(scionCard, 0)
	hound = place(houndCard, 0)
	// The bears are parked on the battlefield while Dreadhound's enter
	// trigger mills (wherever the opening shuffle left them), then walked
	// back into the library so the explicit reorder below can put them on
	// top. Neither observer matches a battlefield -> library move.
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Card != bearCard {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
		bears = append(bears, o.ID)
	}
	if len(bears) != 3 {
		t.Fatalf("test precondition: found %d Grizzly Bears, want 3", len(bears))
	}
	// Dreadhound's enter trigger mills the top three (mountains at this
	// point); settle it before the bears go back on top.
	drainTriggers(t, e)
	for _, id := range bears {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZLibrary})
	}

	var rest []state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		isBear := false
		for _, b := range bears {
			isBear = isBear || b == id
		}
		if !isBear {
			rest = append(rest, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0,
		IDs: append(append([]state.ObjID(nil), bears...), rest...)})

	// Preconditions the later assertions ride on.
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("test precondition: %d triggers queued by setup, want 0", len(e.pendingTriggers))
	}
	top := e.G.Zone(state.ZLibrary, 0)
	if len(top) < 2 || top[0] != bears[0] || top[1] != bears[1] {
		t.Fatalf("test precondition: library top %v, want bears %v,%v first", top[:min(2, len(top))], bears[0], bears[1])
	}
	for _, id := range []state.ObjID{laelia, wasteland, scion, hound} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("test precondition: %s not on the battlefield", e.G.Obj(id).Face().Name)
		}
	}
	if f := e.G.Obj(laelia).Face(); f == nil {
		t.Fatal("test precondition: Laelia face missing")
	} else {
		found := false
		for _, trig := range f.Triggers {
			if trig.Mode != "ChangesZoneAll" || trig.Params["Origin"] != "Library,Graveyard" ||
				trig.Params["Destination"] != "Exile" || trig.Params["TriggerZones"] != "Battlefield" {
				continue
			}
			found = true
		}
		if !found {
			t.Fatalf("test precondition: Laelia face %+v, want a ChangesZoneAll trigger with Origin Library,Graveyard -> Exile on the battlefield", f)
		}
	}
	if f := e.G.Obj(hound).Face(); f == nil {
		t.Fatal("test precondition: Dreadhound face missing")
	} else {
		found := false
		for _, trig := range f.Triggers {
			if trig.Mode != "ChangesZone" || trig.Params["Origin"] != "Library" ||
				trig.Params["Destination"] != "Graveyard" || trig.Params["TriggerZones"] != "Battlefield" {
				continue
			}
			found = true
		}
		if !found {
			t.Fatalf("test precondition: Dreadhound face %+v, want a per-move ChangesZone trigger Library -> Graveyard on the battlefield", f)
		}
	}
	if n := p1p1Count(e, laelia); n != 0 {
		t.Fatalf("test precondition: Laelia starts with %d +1/+1 counters, want 0", n)
	}
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("test precondition: seat 1 starts at %d life, want 20", life)
	}
	return e, cfg, laelia, wasteland, scion, hound, bears
}

func p1p1Count(e *Engine, id state.ObjID) int {
	if o := e.G.Obj(id); o != nil {
		return int(o.Counter("P1P1"))
	}
	return -1
}

// resolveDigSA resolves one carrier's Dig SVar directly (the zone_table
// fixture's effects.Resolve shape) and asserts the SA really is the Dig the
// test's reading rides on.
func resolveDigSA(t *testing.T, e *Engine, reg *cards.Registry, card, svar string, source state.ObjID,
	wantDigNum, wantChangeNum, wantDest string) {
	t.Helper()
	sa := corpusSA(t, reg, card, svar)
	if sa.API != "Dig" {
		t.Fatalf("precondition: %s SVar %q API = %q, want Dig", card, svar, sa.API)
	}
	if got := strings.TrimSpace(sa.Params["DigNum"]); got != wantDigNum {
		t.Fatalf("precondition: %s SVar %q DigNum = %q, want %q", card, svar, got, wantDigNum)
	}
	if got := strings.TrimSpace(sa.Params["ChangeNum"]); got != wantChangeNum {
		t.Fatalf("precondition: %s SVar %q ChangeNum = %q, want %q", card, svar, got, wantChangeNum)
	}
	if got := strings.TrimSpace(sa.Params["DestinationZone"]); got != wantDest {
		t.Fatalf("precondition: %s SVar %q DestinationZone = %q, want %q", card, svar, got, wantDest)
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0,
		SVars: e.G.Obj(source).Face().SVars}, sa)
}

// TestDigBatchesChangesZoneAll pins the batch: Wild Wasteland's upkeep
// trigger exiles the top TWO cards in one api:Dig resolution, and Laelia's
// ChangesZoneAll payoff (her own printed "whenever one or more cards are
// put into exile from your library") queues ONCE for the whole resolution
// -- one +1/+1 counter. Before the fix the two MoveZone events queued her
// trigger twice: two counters.
func TestDigBatchesChangesZoneAll(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, laelia, wasteland, _, _, bears := digBatchBoard(t, reg)

	resolveDigSA(t, e, reg, "Wild Wasteland", "ExileTwo", wasteland, "2", "All", "Exile")

	// Precondition: the resolution really moved BOTH bears to exile (a
	// silent scan that exiled nothing would make the counter count vacuous).
	for _, id := range bears[:2] {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("test precondition: bear %d in %v, want Exile", id, o)
		}
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "dig" {
		t.Fatalf("test precondition: ChangeNum$ All over exactly two eligible cards must take silently, got dig decision %+v", d)
	}
	drainTriggers(t, e)

	if n := p1p1Count(e, laelia); n != 1 {
		t.Fatalf("Laelia +1/+1 counters = %d, want 1 (a two-card api:Dig resolution is ONE ChangesZoneAll batch)", n)
	}
	replayCheck(t, e, cfg)
}

// TestDigSingleCardIsOneBatchAndChangesZoneStaysPerMove is the control set:
//
//   - Laelia's OWN attack trigger (DigNum$ 1, one card exiled) is exactly
//     one batch -- one counter, single-card control;
//   - Scion of Halaster's two-card Dig into the GRAVEYARD fires Dreadhound's
//     per-move Mode$ ChangesZone ONCE PER MOVE (two moves, two life) even
//     while the Dig's batch bracket is open -- the singular mode is never
//     batch-scoped -- and leaves Laelia untouched (her clause names exile);
//   - Wild Wasteland's two-card Dig into exile then adds exactly ONE more
//     counter to Laelia (two in total), which is the assertion that fails
//     without the fix (three there).
func TestDigSingleCardIsOneBatchAndChangesZoneStaysPerMove(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, laelia, wasteland, scion, _, bears := digBatchBoard(t, reg)

	// Phase A -- the single-card control.
	resolveDigSA(t, e, reg, "Laelia, the Blade Reforged", "TrigExile", laelia, "1", "All", "Exile")
	if o := e.G.Obj(bears[0]); o == nil || o.Zone != state.ZExile {
		t.Fatalf("test precondition: bear %d in %v, want Exile (the DigNum$ 1 take did not move it)", bears[0], o)
	}
	drainTriggers(t, e)
	if n := p1p1Count(e, laelia); n != 1 {
		t.Fatalf("Laelia +1/+1 counters after her own one-card exile = %d, want 1 (one move is exactly one batch)", n)
	}

	// Phase B -- the per-move control: two creatures into the graveyard.
	if top := e.G.Zone(state.ZLibrary, 0); len(top) < 3 || top[0] != bears[1] || top[1] != bears[2] {
		t.Fatalf("test precondition: library top %v, want bears %v,%v on top for the graveyard dig",
			top[:min(2, len(top))], bears[1], bears[2])
	}
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("test precondition: seat 1 at %d life before the graveyard dig, want 20", life)
	}
	resolveDigSA(t, e, reg, "Scion of Halaster", "DBDig", scion, "2", "", "Graveyard")
	for _, id := range bears[1:3] {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("test precondition: bear %d in %v, want Graveyard", id, o)
		}
	}
	drainTriggers(t, e)
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("seat 1 lost %d life, want 2 (Mode$ ChangesZone must fire per move even inside the Dig's open batch bracket)",
			20-life)
	}
	if n := p1p1Count(e, laelia); n != 1 {
		t.Fatalf("Laelia +1/+1 counters = %d, want 1 (her clause reads exile, not the graveyard)", n)
	}

	// Phase C -- the batched two-card exile dig on the same board.
	if top := e.G.Zone(state.ZLibrary, 0); len(top) < 2 {
		t.Fatalf("test precondition: %d cards left in seat 0's library, want 2+ for the exile dig", len(top))
	}
	resolveDigSA(t, e, reg, "Wild Wasteland", "ExileTwo", wasteland, "2", "All", "Exile")
	drainTriggers(t, e)
	if n := p1p1Count(e, laelia); n != 2 {
		t.Fatalf("Laelia +1/+1 counters = %d, want 2 (the second two-card api:Dig resolution is ONE batch: 1 + 1)",
			n)
	}
	replayCheck(t, e, cfg)
}
