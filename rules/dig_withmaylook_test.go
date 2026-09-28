package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestIxhelFaceDownExileIsVisibleOnlyToTheDigController pins api:Dig's
// WithMayLook$ True on the real corpus carrier Ixhel, Scion of Atraxa
// (ticket agent-20260919T181318Z-631ddc68).
//
// Ixhel's corrupted end-step trigger runs
//
//	SVar:TrigExile:DB$ Dig | DigNum$ 1 | ChangeNum$ All |
//	  Defined$ Opponent.IsCorrupted | DestinationZone$ Exile |
//	  ExileFaceDown$ True | WithMayLook$ True | RememberChanged$ True | ...
//
// so the OPPONENT exiles the top card of their own library face down, and the
// rules text lets Ixhel's controller look at it. Before WithMayLook$ was read,
// the view granted the face to Object.Controller -- which after a zone move is
// the card's OWNER -- so the exiled card was visible to the wrong seat and
// hidden from the effect's controller, exactly inverted. The marker now records
// the looker (the Dig's controller) and the projection admits only that seat.
//
// The test resolves the compiled SVar directly on an engine host (the
// corrupted_poison_readers_test.go pattern), asserts the exiled card's durable
// state and both projections, and replay-checks the whole log.
func TestIxhelFaceDownExileIsVisibleOnlyToTheDigController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ixhel := mustCorpusCard(t, reg, "Ixhel, Scion of Atraxa")
	fodder := mustCorpusCard(t, reg, "Lightning Bolt")

	// Seat 1's library is real corpus basics plus the named fodder, so the top
	// card the trigger exiles is identifiable by name after the move. The
	// named fodder lives in seat 1's opening hand; the library top is captured
	// at runtime below (the deal moves the first seven cards to the hand).
	cfg := seatZeroStart(Config{Seed: 4242, Names: []string{"ixhel", "opponent"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{ixhel}, mountainDeck(t, 39)...),
			append([]*cards.Card{fodder}, mountainDeck(t, 39)...),
		}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// Precondition: a known, printed card really sits on top of seat 1's
	// library after the deal; capture its name so the later view assertions
	// compare against a face that exists.
	if lib := e.G.Zone(state.ZLibrary, 1); len(lib) == 0 {
		t.Fatal("precondition: seat 1 library is empty")
	} else if top := e.G.Obj(lib[0]); top == nil || top.Face() == nil || top.Face().Name == "" {
		t.Fatalf("precondition: seat 1 library top has no printed face: %+v", top)
	}
	fodderName := e.G.Obj(e.G.Zone(state.ZLibrary, 1)[0]).Face().Name

	ixhelID := moveCorpusCard(t, e, "Ixhel, Scion of Atraxa", 0, state.ZBattlefield)
	if o := e.G.Obj(ixhelID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Ixhel not on seat 0's battlefield: %+v", o)
	}
	// Corrupted: the opponent holds three poison counters, so
	// Defined$ Opponent.IsCorrupted selects seat 1 and only seat 1.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 3})
	if e.G.Players[1].Counter("POISON") != 3 || e.G.Players[0].Counter("POISON") != 0 {
		t.Fatalf("precondition: poison = %d/%d, want 0/3",
			e.G.Players[0].Counter("POISON"), e.G.Players[1].Counter("POISON"))
	}

	sa := resolveSVarOf(t, ixhel.Faces[0], "TrigExile")
	if sa == nil || sa.API != "Dig" || sa.Params["WithMayLook"] != "True" ||
		sa.Params["ExileFaceDown"] != "True" || sa.Params["Defined"] != "Opponent.IsCorrupted" {
		t.Fatalf("Ixhel TrigExile shape drifted: %+v", sa)
	}

	effects.Resolve(e, &effects.Ctx{Source: ixhelID, Controller: 0}, sa)
	e.Advance()

	// The fodder card is now exiled face down. Its printed face exists and the
	// compared values actually differ: the owner (seat 1) is NOT the looker.
	var exiledID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == fodderName {
			exiledID = id
		}
	}
	if exiledID == 0 {
		t.Fatalf("precondition: %q was not exiled from seat 1's library", fodderName)
	}
	o := e.G.Obj(exiledID)
	if o.Zone != state.ZExile || !o.FaceDown {
		t.Fatalf("exiled fodder zone=%s faceDown=%v, want exile/true", o.Zone, o.FaceDown)
	}
	if o.Owner != 1 || o.Controller != 1 {
		t.Fatalf("exiled fodder owner/controller = %d/%d, want 1/1 (the non-looker's seat, so the assertion can fail)", o.Owner, o.Controller)
	}
	if !o.HasMayLook || o.MayLookPlayer != 0 {
		t.Fatalf("exiled fodder HasMayLook=%v MayLookPlayer=%d, want true/0 (the Dig's controller); a WithMayLook$ True dig must record its looker", o.HasMayLook, o.MayLookPlayer)
	}
	if o.ExiledWith != ixhelID {
		t.Fatalf("exiled fodder ExiledWith=%d, want the exiling source %d", o.ExiledWith, ixhelID)
	}

	// Controller (seat 0) sees the real face of the face-down exile, which
	// sits in SEAT 1's exile zone (the owner's), projected into
	// Players[1].Exile of seat 0's view.
	ctrl := view.Project(e.G, e, 0, nil)
	var shown *view.CardView
	for i := range ctrl.Players[1].Exile {
		if ctrl.Players[1].Exile[i].ID == exiledID {
			shown = &ctrl.Players[1].Exile[i]
		}
	}
	if shown == nil {
		t.Fatal("failed precondition: the face-down exile is missing from seat 0's exile projection")
	}
	if shown.Name != fodderName || !shown.FaceDown {
		t.Fatalf("controller's view of the face-down exile = %+v, want the printed face %q with FaceDown set", shown, fodderName)
	}

	// Owner (seat 1) must NOT read the name: the looker is authoritative and
	// the owner was never granted the look.
	opp := view.Project(e.G, e, 1, nil)
	var hidden *view.CardView
	for i := range opp.Players[1].Exile {
		if opp.Players[1].Exile[i].ID == exiledID {
			hidden = &opp.Players[1].Exile[i]
		}
	}
	if hidden == nil {
		t.Fatal("failed precondition: the face-down exile is missing from seat 1's exile projection")
	}
	if hidden.Name == fodderName || hidden.Name != "" {
		t.Fatalf("owner's view of the face-down exile leaks the printed name %q; WithMayLook$ authorises only the Dig's controller", hidden.Name)
	}
	if !hidden.FaceDown {
		t.Fatal("owner's view lost the face_down marker; the redaction must hide the face, not the card")
	}

	replayCheck(t, e, cfg)
}
