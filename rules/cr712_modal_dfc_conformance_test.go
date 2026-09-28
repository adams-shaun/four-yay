package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// modalLandFixture puts the real Boggart Trawler // Boggart Bog in hand and
// leaves seat zero at its first main phase with its land drop unused.
func modalLandFixture(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Boggart Trawler")
	id := searchMoveByName(t, e, "Boggart Trawler", state.ZHand)
	toMain1(t, e)
	return e, cfg, id
}

func modalLandOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority decision = %+v", d)
	}
	var found []decision.Option
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == id {
			found = append(found, o)
		}
	}
	if len(found) != 1 {
		t.Fatalf("modal land options = %+v, want exactly one", found)
	}
	return found[0]
}

func playModalLand(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := modalLandOption(t, e, id)
	if o.Mode != "modal_land" || o.Label != "Play Boggart Bog" {
		t.Fatalf("modal land option = %+v", o)
	}
	submitChoices(t, e, o.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
		// The fixture's real as-enters unless-pay choice is answered with the
		// deterministic decline; either branch must still enter as the land.
		submitChoices(t, e, 0)
	}
}

// TestCR712ModalDFCLandLandOffersBothFaces pins that the back-land offer is
// additive when the front face is also a land (CR 712.8).
func TestCR712ModalDFCLandLandOffersBothFaces(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Branchloft Pathway")
	id := searchMoveByName(t, e, "Branchloft Pathway", state.ZHand)
	toMain1(t, e)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.FaceIdx != 0 || o.Card == nil ||
		o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 ||
		o.Card.Faces[0] == nil || o.Card.Faces[1] == nil ||
		!o.Card.Faces[0].IsLand() || !o.Card.Faces[1].IsLand() {
		t.Fatalf("fixture faces do not prove Modal land/land precondition: %+v", o)
	}
	var lands []decision.Option
	for _, option := range e.Pending().Options {
		if option.Kind == "play_land" && option.Obj == id {
			lands = append(lands, option)
		}
	}
	if len(lands) != 2 || lands[0].Mode != "" || lands[0].Label != "Play Branchloft Pathway" ||
		lands[1].Mode != "modal_land" || lands[1].Label != "Play Boulderloft Pathway" {
		t.Fatalf("front/back land options = %+v", lands)
	}
}

// TestCR712ModalDFCLandBackIsOfferedPlayedAndReplays pins CR 712.8/712.4d:
// the hand action selects the land face, which enters face up as that land.
func TestCR712ModalDFCLandBackIsOfferedPlayedAndReplays(t *testing.T) {
	t.Parallel()
	e, cfg, id := modalLandFixture(t)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.FaceIdx != 0 || o.Card == nil {
		t.Fatalf("fixture object precondition = %+v", o)
	}
	if o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 ||
		o.Card.Faces[0] == nil || o.Card.Faces[1] == nil ||
		o.Card.Faces[0].IsLand() || !o.Card.Faces[1].IsLand() {
		t.Fatalf("fixture faces do not prove Modal land-back precondition")
	}
	if e.G.Step != state.StepMain1 || e.G.Active != 0 || e.G.Players[0].LandsPlayed != 0 {
		t.Fatalf("timing/land-drop precondition: step=%s active=%d drops=%d", e.G.Step, e.G.Active, e.G.Players[0].LandsPlayed)
	}
	playModalLand(t, e, id)
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).FaceIdx != 1 || e.G.Obj(id).Face().Name != "Boggart Bog" {
		t.Fatalf("played modal land = zone %s face %d name %s", e.G.Obj(id).Zone, e.G.Obj(id).FaceIdx, e.G.Obj(id).Face().Name)
	}
	flipped, moved, lands := -1, -1, 0
	for i, ev := range e.L.Events {
		if ev.Kind == events.FlipFace && ev.Obj == id && ev.Amount == 1 {
			flipped = i
		}
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			moved = i
		}
		if ev.Kind == events.LandPlayed && ev.Player == 0 {
			lands++
		}
	}
	if flipped < 0 || moved < 0 || flipped >= moved {
		t.Fatalf("flip/move order = %d/%d", flipped, moved)
	}
	if lands != 1 || len(e.G.Zone(state.ZHand, 0)) == 0 {
		t.Fatalf("land accounting = %d", lands)
	}
	for _, x := range e.legalActionsPriced(0, nil) {
		if x.Kind == "play_land" {
			t.Fatalf("second land offered: %+v", x)
		}
	}
	replayCheck(t, e, cfg)
}

// TestCR712ModalDFCLandBackResetsToFrontOnExit pins the battlefield boundary:
// leaving the land face returns the object to its front face in hand.
func TestCR712ModalDFCLandBackResetsToFrontOnExit(t *testing.T) {
	t.Parallel()
	e, cfg, id := modalLandFixture(t)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Card == nil || o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 || o.Card.Faces[0].IsLand() == o.Card.Faces[1].IsLand() {
		t.Fatal("fixture does not prove two differing Modal land faces in hand")
	}
	playModalLand(t, e, id)
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).FaceIdx != 1 {
		t.Fatal("land did not enter on back face")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(id); got.FaceIdx != 0 || got.Face().Name != "Boggart Trawler" {
		t.Fatalf("returned object = face %d %s", got.FaceIdx, got.Face().Name)
	}
	// A land drop is per turn. Pass the deterministic two-seat table to the
	// next seat-zero main phase so the re-offer proves the real gate, rather
	// than mutating LandsPlayed behind the event log.
	for i := 0; i < 160 && !(e.G.Turn > 1 && e.G.Active == 0 && e.G.Step == state.StepMain1); i++ {
		if d := e.Pending(); d != nil && d.Kind == decision.KPriority {
			submitPass(t, e)
		} else {
			submitChoices(t, e, 0)
		}
	}
	if e.G.Active != 0 || e.G.Step != state.StepMain1 || e.G.Players[0].LandsPlayed != 0 {
		t.Fatalf("did not reach a fresh land-drop window: turn=%d active=%d step=%s drops=%d", e.G.Turn, e.G.Active, e.G.Step, e.G.Players[0].LandsPlayed)
	}
	// Return it to hand in the fresh turn, then rebuild priority. This avoids
	// the cleanup hand-size decision while still exercising a real zone return.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZHand})
	e.priorityRound()
	modalLandOption(t, e, id)
	replayCheck(t, e, cfg)
}
