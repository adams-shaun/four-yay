package rules

// Hedge Shredder's milled-lands batch trigger:
// "Whenever one or more land cards are put into your graveyard from your
// library, put them onto the battlefield tapped." Its body is
// `DB$ ChangeZoneAll | ChangeType$ Card.TriggeredCards | Origin$ Graveyard |
// Destination$ Battlefield | Tapped$ True` — the sweep must move exactly the
// land cards the trigger CAPTURED (Ctx.Remembered), and only those: an
// unrelated land already sitting in the same graveyard must stay put, because
// the captured set (not the origin zone) bounds the sweep. This file is the
// engine-level carrier for the ChangeZoneAll ChangeType$ Card.TriggeredCards
// referent.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// hedgeShredderEngine seats the REAL compiled Hedge Shredder on seat 0's
// battlefield in a two-seat game, with the given real corpus land cards in
// seat 0's library and an unrelated real corpus land already in seat 0's
// graveyard. The Shredder is placed with NO enter event, so its attack trigger
// stays quiet: the only trigger this test drives is the real
// library->graveyard move.
func hedgeShredderEngine(t *testing.T, seed uint64, libraryLands, graveLands []string) (*Engine, state.ObjID, []state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	shredder := mustCorpusCard(t, reg, "Hedge Shredder")

	// Precondition the whole test rests on: the card really prints the
	// ChangeType$ Card.TriggeredCards ChangeZoneAll body.
	var body *cards.SA
	for _, face := range shredder.Faces {
		for name := range face.SVars {
			if sa := cards.ResolveSVar(face.SVars, name); sa != nil &&
				sa.API == "ChangeZoneAll" && sa.Params["ChangeType"] == "Card.TriggeredCards" {
				body = sa
			}
		}
	}
	if body == nil {
		t.Fatal("corpus pin moved: Hedge Shredder no longer carries ChangeZoneAll ChangeType$ Card.TriggeredCards")
	}
	if body.Params["Tapped"] != "True" {
		t.Fatalf("corpus pin moved: Hedge Shredder's body Tapped$ = %q, want True", body.Params["Tapped"])
	}

	e := New(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	sh := e.G.AddObject(shredder, 0)
	sh.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{sh.ID})

	lib := e.G.Zone(state.ZLibrary, 0)
	var libIDs []state.ObjID
	for _, name := range libraryLands {
		c := mustCorpusCard(t, reg, name)
		if !c.Faces[0].IsLand() {
			t.Fatalf("precondition failed: %s is not a land card", name)
		}
		o := e.G.AddObject(c, 0)
		libIDs = append(libIDs, o.ID)
	}
	e.G.SetZone(state.ZLibrary, 0, append(libIDs, lib...))

	var graveIDs []state.ObjID
	for _, name := range graveLands {
		c := mustCorpusCard(t, reg, name)
		if !c.Faces[0].IsLand() {
			t.Fatalf("precondition failed: %s is not a land card", name)
		}
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZGraveyard
		graveIDs = append(graveIDs, o.ID)
	}
	e.G.SetZone(state.ZGraveyard, 0, graveIDs)

	// Preconditions: the source permanent is where the trigger reads it, each
	// milled land really is in the library (the origin zone the trigger scans),
	// and the unrelated land really is in the graveyard (same destination zone,
	// so only captured-set membership can separate them).
	if o := e.G.Obj(sh.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Hedge Shredder zone %v, want battlefield", o)
	}
	for _, id := range libIDs {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZLibrary || o.Owner != 0 {
			t.Fatalf("precondition failed: milled land %d zone=%v owner=%d, want library/0", id, o.Zone, o.Owner)
		}
	}
	for _, id := range graveIDs {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZGraveyard || o.Owner != 0 {
			t.Fatalf("precondition failed: unrelated land %d zone=%v owner=%d, want graveyard/0", id, o.Zone, o.Owner)
		}
	}
	return e, sh.ID, libIDs, graveIDs
}

// hsDrain passes priority until the stack and the pending-trigger queue are
// empty, so the mandatory ChangeZoneAll bodies are placed on the stack and
// resolve. A simultaneous-trigger order ask (the per-card instances a
// multi-card mill pushes) is answered in option order; any other non-priority
// decision is a shape the test does not expect.
func hsDrain(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 40 && !e.G.Over; i++ {
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack %d, pending %d)", len(e.G.Stack), len(e.pendingTriggers))
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KTriggerOrder:
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		default:
			t.Fatalf("unexpected %v while draining the mandatory trigger: %+v", d.Kind, d)
		}
	}
	t.Fatal("engine still had a pending decision after 40 priority passes")
}

// TestHedgeShredderMilledLandEntersTapped drives one real library->graveyard
// land move and pins the leg: the milled land enters the battlefield tapped,
// while an unrelated land already in the graveyard is NOT moved (the captured
// set, not the origin zone, bounds the sweep).
func TestHedgeShredderMilledLandEntersTapped(t *testing.T) {
	t.Parallel()
	e, sh, libIDs, graveIDs := hedgeShredderEngine(t, 917, []string{"Forest"}, []string{"Island"})

	// The triggering event, as a real logged move.
	e.emit(events.Event{Kind: events.MoveZone, Obj: libIDs[0], From: state.ZLibrary, To: state.ZGraveyard})
	e.Advance()
	hsDrain(t, e)

	if got := e.G.Obj(libIDs[0]).Zone; got != state.ZBattlefield {
		t.Fatalf("milled land zone = %v, want battlefield", got)
	}
	if !e.G.Obj(libIDs[0]).Tapped {
		t.Fatal("milled land entered UNTAPPED, want tapped (Tapped$ True)")
	}
	if got := e.G.Obj(graveIDs[0]).Zone; got != state.ZGraveyard {
		t.Fatalf("unrelated graveyard land zone = %v, want graveyard (the captured set bounds the sweep)", got)
	}
	if got := e.G.Obj(sh).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken by resolution: Hedge Shredder zone %v, want battlefield", got)
	}
}

// TestHedgeShredderTwoMilledLandsBothEnterTapped drives two real
// library->graveyard land moves and pins that BOTH milled lands enter tapped,
// the per-card trigger instances each carrying their own captured card.
func TestHedgeShredderTwoMilledLandsBothEnterTapped(t *testing.T) {
	t.Parallel()
	e, _, libIDs, graveIDs := hedgeShredderEngine(t, 918,
		[]string{"Forest", "Mountain"}, []string{"Island"})

	e.emit(events.Event{Kind: events.MoveZone, Obj: libIDs[0], From: state.ZLibrary, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: libIDs[1], From: state.ZLibrary, To: state.ZGraveyard})
	e.Advance()
	hsDrain(t, e)

	for i, id := range libIDs {
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("milled land #%d zone = %v, want battlefield", i, o.Zone)
		}
		if !o.Tapped {
			t.Fatalf("milled land #%d entered UNTAPPED, want tapped", i)
		}
	}
	if got := e.G.Obj(graveIDs[0]).Zone; got != state.ZGraveyard {
		t.Fatalf("unrelated graveyard land zone = %v, want graveyard", got)
	}
}
