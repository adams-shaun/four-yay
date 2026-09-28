package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Manifest (api:Manifest) pins: a manifest is a real library→battlefield
// card move (never a token mint) that lands FACE DOWN (CR 708.5) — a 2/2
// creature with no name, types, keywords or colours, even when the card is a
// land — redacted to everyone but its controller by the view's FaceDown
// rule, and revealed by the FaceDown clear when it leaves the battlefield
// (CR 708.9). The helpers come from search_library_test.go: the decks are
// built from compiled corpus cards only, so no Forge script text is
// committed here.
//
// No repo deck carries a Manifest carrier (Reality Shift and Whisperwood
// Elemental appear in none), so these tests do not move the golden heads.

// manifestEngine deals a seatZeroStart two-seat game where seat 0's hand
// holds one copy of each fixture name and seat 1's deck is bears so a Grizzly
// Bears is available to move onto seat 1's battlefield. All corpus cards.
func manifestEngine(t *testing.T, reg *cards.Registry, hand ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 10; i++ {
		opp = append(opp, bear)
	}
	for len(opp) < 40 {
		opp = append(opp, forest)
	}
	cfg := seatZeroStart(Config{Seed: 7701, Names: []string{"manifest", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// oppBear moves one of seat 1's Grizzly Bears onto seat 1's battlefield with
// a logged MoveZone and returns its id.
func oppBear(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 1) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				return id
			}
		}
	}
	t.Fatal("no Grizzly Bears in seat 1's hand/library")
	return 0
}

// castAtOpponent casts the named card in seat 0's hand, answering the target
// decision with the option for the named object (option 0 when the id is 0),
// and returns the pending decision after both seats passed.
func castAtOpponent(t *testing.T, e *Engine, name string, target state.ObjID, mana string) *decision.Decision {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, mana)
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after casting %s: %+v, want the creature target ask", name, d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("target %d not offered: %+v", target, d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
	return e.Pending()
}

// manifestedOn finds the face-down battlefield object seat p controls.
func manifestedOn(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.FaceDown {
			return id
		}
	}
	t.Fatalf("seat %d controls no face-down permanent", p)
	return 0
}

// noUnimplementedManifest fails if the log carries the loud fallback note.
func noUnimplementedManifest(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API Manifest") {
			t.Fatalf("log carries the unimplemented-API note: %q", ev.Text)
		}
	}
}

// TestRealityShiftManifestsFaceDown is the ticket's carrier end to end:
// Reality Shift exiles its target creature and its controller manifests the
// top card of their library — the real card object (captured off the library
// before the cast), face down, derived 2/2 and Creature-only, redacted to
// the opponent by the view, and the whole game replays from the log.
func TestRealityShiftManifestsFaceDown(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Reality Shift")
	bear := oppBear(t, e)
	top := e.G.Zone(state.ZLibrary, 1)[0]
	topName := e.G.Obj(top).Face().Name

	d := castAtOpponent(t, e, "Reality Shift", bear, "UU")
	if d != nil && d.Kind != decision.KPriority {
		t.Fatalf("pending after resolution = %+v, want priority (no ask)", d)
	}
	if got := e.G.Obj(bear).Zone; got != state.ZExile {
		t.Fatalf("target creature zone = %s, want exile", got)
	}
	if got := e.G.Obj(top).Zone; got != state.ZBattlefield {
		t.Fatalf("manifested card (%s) zone = %s, want battlefield", topName, got)
	}
	o := e.G.Obj(top)
	if !o.FaceDown || o.Controller != 1 || o.Owner != 1 {
		t.Fatalf("manifested card: FaceDown=%v Controller=%d Owner=%d", o.FaceDown, o.Controller, o.Owner)
	}
	der := e.Derived(top)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("manifested P/T = %d/%d, want 2/2", der.Power, der.Toughness)
	}
	if len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("manifested types = %v, want exactly [Creature]", der.Types)
	}
	if der.Keywords != nil && len(der.Keywords) != 0 {
		t.Fatalf("manifested keywords = %v, want none", der.Keywords)
	}
	if der.Colors != "" {
		t.Fatalf("manifested colours = %q, want colourless", der.Colors)
	}
	if len(der.Types) == 1 && !e.IsCreature(top) {
		t.Fatal("manifested card is not a creature per IsCreature")
	}
	noUnimplementedManifest(t, e)

	// The view redacts the face-down card to seat 0 and shows it to its
	// controller (CR 708.5: the owner of a face-down permanent may look at
	// it; the redaction is the same one hideaway's face-down exile uses).
	oppView := view.Project(e.G, e, 0, nil)
	var blank, own *view.CardView
	for i := range oppView.Players[1].Battlefield {
		if oppView.Players[1].Battlefield[i].ID == top {
			blank = &oppView.Players[1].Battlefield[i]
		}
	}
	if blank == nil {
		t.Fatal("manifested card missing from seat 0's battlefield view")
	}
	if blank.Name != "" || !blank.FaceDown || blank.Controller != 1 || blank.Owner != 1 {
		t.Fatalf("opponent's view of the manifested card: %+v, want the redacted blank", blank)
	}
	ctrlView := view.Project(e.G, e, 1, nil)
	for i := range ctrlView.Players[1].Battlefield {
		if ctrlView.Players[1].Battlefield[i].ID == top {
			own = &ctrlView.Players[1].Battlefield[i]
		}
	}
	if own == nil || own.Name != topName || !own.FaceDown {
		t.Fatalf("controller's view of its own manifested card: %+v, want name %q", own, topName)
	}

	// The manifest move is Secret: another seat's transcript names no card.
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == top && ev.Counter == "entered_face_down" {
			if !ev.Secret || ev.Player != 1 {
				t.Fatalf("manifest move Secret=%v Player=%d, want Secret for seat 1", ev.Secret, ev.Player)
			}
		}
	}

	replayCheck(t, e, cfg)
}

// TestManifestedLandIsA22Creature pins CR 708.5's exact text: even a
// manifested LAND is a 2/2 creature with no land types while face down.
func TestManifestedLandIsA22Creature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Reality Shift")
	bear := oppBear(t, e)
	top := e.G.Zone(state.ZLibrary, 1)[0]
	topName := e.G.Obj(top).Face().Name
	if topName != "Forest" {
		t.Fatalf("fixture wants a Forest on top of seat 1's library, got %q", topName)
	}
	castAtOpponent(t, e, "Reality Shift", bear, "UU")
	if got := e.G.Obj(top).Zone; got != state.ZBattlefield || !e.G.Obj(top).FaceDown {
		t.Fatalf("manifested Forest: zone=%s FaceDown=%v", got, e.G.Obj(top).FaceDown)
	}
	der := e.Derived(top)
	if der.Power != 2 || der.Toughness != 2 || len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("manifested land derived %+v, want a 2/2 [Creature]", der)
	}
	if e.IsCreature(top) != true {
		t.Fatal("manifested land is not a creature")
	}
	noUnimplementedManifest(t, e)
}

// TestWhisperwoodManifestsOnEndStep pins the bare `SVar:TrigManifest:DB$
// Manifest` default shape: Whisperwood Elemental's end-step trigger
// manifests the top card of its controller's own library — the defaults
// (controller, top card, one card) with no parameters named at all.
func TestWhisperwoodManifestsOnEndStep(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Whisperwood Elemental")
	id := searchMoveByName(t, e, "Whisperwood Elemental", state.ZBattlefield)
	top := e.G.Zone(state.ZLibrary, 0)[0]

	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(top).Zone; got != state.ZBattlefield {
		t.Fatalf("Whisperwood's manifested card zone = %s, want battlefield", got)
	}
	o := e.G.Obj(top)
	if !o.FaceDown || o.Controller != 0 {
		t.Fatalf("Whisperwood's manifested card: FaceDown=%v Controller=%d", o.FaceDown, o.Controller)
	}
	der := e.Derived(top)
	if der.Power != 2 || der.Toughness != 2 || len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("Whisperwood's manifested card derived %+v, want a 2/2 [Creature]", der)
	}
	if id == 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("Whisperwood Elemental itself missing from the battlefield")
	}
	noUnimplementedManifest(t, e)
	replayCheck(t, e, cfg)
}

// TestSultaiEmissaryDeathTriggerManifests pins the same primitive through a
// death trigger: the manifested card is a real battlefield object while the
// Emissary is in the graveyard, and the face-down Emissary-shaped death
// trigger of a MANIFESTED card fires nothing (CR 708.8: while face down its
// printed triggers do not exist).
func TestSultaiEmissaryDeathTriggerManifests(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Sultai Emissary", "Grizzly Bears")
	id := searchMoveByName(t, e, "Sultai Emissary", state.ZBattlefield)
	top := e.G.Zone(state.ZLibrary, 0)[0]

	// Lethal damage: the death trigger fires through the ordinary drain.
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 3, Player: 0})
	e.checkStateBased()
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("Sultai Emissary zone = %s, want graveyard", got)
	}
	if got := e.G.Obj(top).Zone; got != state.ZBattlefield || !e.G.Obj(top).FaceDown {
		t.Fatalf("Emissary's manifested card: zone=%s FaceDown=%v", got, e.G.Obj(top).FaceDown)
	}
	noUnimplementedManifest(t, e)
	replayCheck(t, e, cfg)
}

// TestManifestedCardRevealsWhenItLeaves pins CR 708.9: a face-down permanent
// that leaves the battlefield is revealed — FaceDown clears and its identity
// is visible to every viewer again.
func TestManifestedCardRevealsWhenItLeaves(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Reality Shift")
	bear := oppBear(t, e)
	top := e.G.Zone(state.ZLibrary, 1)[0]
	topName := e.G.Obj(top).Face().Name
	castAtOpponent(t, e, "Reality Shift", bear, "UU")
	manifestedOn(t, e, 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: top,
		From: state.ZBattlefield, To: state.ZGraveyard})
	o := e.G.Obj(top)
	if o.FaceDown {
		t.Fatal("FaceDown did not clear when the manifested card left the battlefield")
	}
	if o.Zone != state.ZGraveyard {
		t.Fatalf("zone = %s, want graveyard", o.Zone)
	}
	pubView := view.Project(e.G, e, 0, nil)
	found := false
	for i := range pubView.Players {
		for _, cv := range pubView.Players[i].Graveyard {
			if cv.ID == top {
				found = true
				if cv.Name != topName {
					t.Fatalf("graveyard view name = %q, want %q (revealed)", cv.Name, topName)
				}
			}
		}
	}
	if !found {
		t.Fatal("revealed card missing from the public graveyard view")
	}
	replayCheck(t, e, cfg)
}

// TestManifestOutOfScopeShapesStayLoud pins the fail-loud contract: a
// Manifest whose shape this build does not implement (RememberManifested$
// True — the wildcall/cloudform family) emits the SAME "unimplemented API
// Manifest" note the unimplemented fallback always emitted, and moves
// nothing: no library→battlefield manifest move, no silent wrong-card move.
func TestManifestOutOfScopeShapesStayLoud(t *testing.T) {
	t.Parallel()
	src := "Name:Loud Manifest\nManaCost:0\nTypes:Instant\n" +
		"A:SP$ Manifest | RememberManifested$ True\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 4401, src)
	addMana(t, e, 0, "C")
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the fixture: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if !hasNote(e, "unimplemented API Manifest") {
		t.Fatal("out-of-scope Manifest shape was silent: no fallback note")
	}
	manifestedOnBf := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary &&
			ev.To == state.ZBattlefield && ev.Counter == "entered_face_down" {
			manifestedOnBf = true
		}
	}
	if manifestedOnBf {
		t.Fatal("out-of-scope Manifest shape moved a card")
	}
	if len(e.G.Zone(state.ZLibrary, 0)) != len(libBefore) {
		t.Fatal("library size changed on a failed manifest")
	}
	replayCheck(t, e, cfg)
}
