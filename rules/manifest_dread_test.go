package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestZimoneManifestDreadLooksAndChooses exercises the real corpus trigger:
// Zimone's landfall ability looks at the top two and lets its controller pick.
func TestZimoneManifestDreadLooksAndChooses(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Zimone, Mystery Unraveler")
	zid := searchMoveByName(t, e, "Zimone, Mystery Unraveler", state.ZHand)
	land := searchMoveByName(t, e, "Forest", state.ZHand)
	if zid == 0 || land == 0 {
		t.Fatal("test precondition: Zimone and a land must be in seat 0's hand")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: zid, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(zid).Zone != state.ZBattlefield {
		t.Fatal("test precondition: Zimone did not enter the battlefield")
	}
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) < 2 || lib[0] == lib[1] {
		t.Fatalf("test precondition: need two distinct top library objects, got %v", lib)
	}
	first, second := lib[0], lib[1]
	if e.G.Obj(first).Zone != state.ZLibrary || e.G.Obj(second).Zone != state.ZLibrary {
		t.Fatal("test precondition: both offered cards must be in the library")
	}
	// Play the land through the ordinary action so its real landfall trigger
	// is queued by the engine, rather than simulating a zone change.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("test precondition: expected priority before playing the land, got %+v", d)
	}
	landOption := -1
	for _, option := range d.Options {
		if option.Kind == "play_land" && option.Obj == land {
			landOption = option.Index
		}
	}
	if landOption < 0 {
		t.Fatalf("test precondition: Forest %d is not offered as a land play", land)
	}
	submitChoices(t, e, landOption)
	for attempts := 0; attempts < 20; attempts++ {
		d = e.Pending()
		if d != nil && d.Kind == decision.KChoose && d.ResumeKind == "manifest_dread" {
			break
		}
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("unexpected state while resolving landfall: %+v", d)
		}
		pass := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				pass = option.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("no pass option while resolving landfall: %+v", d.Options)
		}
		submitChoices(t, e, pass)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "manifest_dread" || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("ManifestDread choice = %+v, want controller KChoose over two cards", d)
	}
	selected := d.Options[1].Obj
	if selected != first && selected != second {
		t.Fatalf("chosen option %d was not one of the two preconditioned top cards", selected)
	}
	submitChoices(t, e, d.Options[1].Index)
	if got := e.G.Obj(selected).Zone; got != state.ZBattlefield {
		t.Fatalf("chosen card %d is in %s, want battlefield", selected, got)
	}
	other := first
	if selected == first {
		other = second
	}
	if got := e.G.Obj(other).Zone; got != state.ZGraveyard {
		t.Fatalf("unchosen card %d is in %s, want graveyard", other, got)
	}
	// CR 701.61: the graveyard is a public zone, so the unchosen card's move
	// must survive projection for every seat AND a public spectator -- a
	// Secret move would strip Obj and hide a now-public identity from the
	// transcript. The private look and the face-down move must stay hidden.
	var graveyardMove *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == other && ev.To == state.ZGraveyard {
			graveyardMove = ev
		}
	}
	if graveyardMove == nil {
		t.Fatalf("no graveyard move for unchosen card %d in the log", other)
	}
	if graveyardMove.Secret {
		t.Fatalf("graveyard move for %d is Secret; a public zone move must be public", other)
	}
	for _, viewer := range []state.PlayerID{0, 1} {
		redacted := view.RedactEvent(e.G, *graveyardMove, viewer)
		if redacted.Obj != other {
			t.Fatalf("seat %d projection of the graveyard move = %+v, want Obj %d visible", viewer, redacted, other)
		}
	}
	public := view.RedactEventFor(e.G, *graveyardMove, view.NoSeat, view.Public)
	if public.Obj != other {
		t.Fatalf("public spectator projection of the graveyard move = %+v, want Obj %d visible", public, other)
	}
	var faceDownMove *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == selected && ev.To == state.ZBattlefield && ev.Counter == "entered_face_down" {
			faceDownMove = ev
		}
	}
	if faceDownMove == nil {
		t.Fatalf("no face-down battlefield move for chosen card %d", selected)
	}
	if opponent := view.RedactEvent(e.G, *faceDownMove, 1); opponent.Obj != 0 {
		t.Fatalf("opponent learned the manifested card's identity: %+v", opponent)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "looks at the top two cards of the library" && ev.Player == 0 {
			opponent := view.RedactEvent(e.G, ev, 1)
			if len(opponent.IDs) != 0 {
				t.Fatalf("opponent learned the private look ids: %+v", opponent)
			}
		}
	}
	o := e.G.Obj(selected)
	if !o.FaceDown || o.Controller != 0 {
		t.Fatalf("manifested card FaceDown=%v Controller=%d, want true/0", o.FaceDown, o.Controller)
	}
	der := e.Derived(selected)
	if der.Power != 2 || der.Toughness != 2 || len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("manifested card derived=%+v, want face-down 2/2 Creature", der)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ManifestDread") {
			t.Fatalf("unimplemented fallback recorded: %q", ev.Text)
		}
	}
	lookSeen := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "looks at the top two cards of the library" && ev.Player == 0 {
			lookSeen = true
			if !ev.Secret || len(ev.IDs) != 2 || ev.IDs[0] != first || ev.IDs[1] != second {
				t.Fatalf("private look = %+v, want secret note for exactly the two top cards", ev)
			}
		}
	}
	if !lookSeen {
		t.Fatal("no replay-visible private look was recorded")
	}
	replayCheck(t, e, cfg)
}

// dreadTrimLibrary shapes the library before the trigger resolves. It is the
// prepare callback for the boundary tests below.
func dreadTrimLibrary(t *testing.T, e *Engine, keep int) {
	t.Helper()
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) < keep {
		t.Fatalf("test precondition: library has %d cards, need at least %d", len(lib), keep)
	}
	for _, id := range lib[keep:] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZExile})
	}
	if n := len(e.G.Zone(state.ZLibrary, 0)); n != keep {
		t.Fatalf("test precondition: library has %d cards after trim, want %d", n, keep)
	}
}

// dreadDriveToResolution plays a Forest from seat 0's hand so Zimone's REAL
// landfall trigger (DB$ ManifestDread) resolves, driving priority until the
// outcome probe reports true or the asks/passes run out. A nil outcome just
// drives the fixed number of rounds (the no-op assertions live in the
// caller). The boundary tests expect NO manifest-dread choice: with fewer
// than two cards the engine must not ask, so any KChoose offered fails the
// test.
func dreadDriveToResolution(t *testing.T, e *Engine, outcome func(e *Engine) bool) {
	t.Helper()
	land := searchMoveByName(t, e, "Forest", state.ZHand)
	if land == 0 {
		t.Fatal("test precondition: Forest must be in seat 0's hand")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("test precondition: expected priority before playing the land, got %+v", d)
	}
	landOption := -1
	for _, option := range d.Options {
		if option.Kind == "play_land" && option.Obj == land {
			landOption = option.Index
		}
	}
	if landOption < 0 {
		t.Fatalf("test precondition: Forest %d is not offered as a land play", land)
	}
	submitChoices(t, e, landOption)
	for attempts := 0; attempts < 20; attempts++ {
		if outcome != nil && outcome(e) {
			return
		}
		d = e.Pending()
		if d == nil {
			return
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "manifest_dread" {
			t.Fatalf("a manifest-dread choice was offered for fewer than two cards: %+v", d)
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected state while resolving landfall: %+v", d)
		}
		pass := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				pass = option.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("no pass option while resolving landfall: %+v", d.Options)
		}
		submitChoices(t, e, pass)
	}
	if outcome != nil {
		t.Fatal("trigger resolution never converged after 20 priority rounds")
	}
}

// dreadAssertNoFallback fails when the corpus trigger resolved through the
// unimplemented-API fallback -- the guard that keeps these boundary tests
// from passing with the ManifestDread handler de-registered.
func dreadAssertNoFallback(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ManifestDread") {
			t.Fatalf("unimplemented fallback recorded: %q", ev.Text)
		}
	}
}

// dreadLookCount requires EXACTLY ONE private-look note for seat 0 carrying
// the expected ids, so a regression that omits the look (or records it twice)
// fails instead of silently passing because no note matched.
func dreadLookCount(t *testing.T, e *Engine, wantIDs ...state.ObjID) {
	t.Helper()
	var looks []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "looks at the top two cards of the library" && ev.Player == 0 {
			looks = append(looks, ev)
		}
	}
	if len(looks) != 1 {
		t.Fatalf("private look notes for seat 0 = %d, want exactly 1: %+v", len(looks), looks)
	}
	got := looks[0]
	if !got.Secret || len(got.IDs) != len(wantIDs) {
		t.Fatalf("private look = %+v, want a secret note for exactly %d cards", got, len(wantIDs))
	}
	for i := range wantIDs {
		if got.IDs[i] != wantIDs[i] {
			t.Fatalf("private look ids = %v, want %v", got.IDs, wantIDs)
		}
	}
}

// TestZimoneManifestDreadSingleCardLibrary pins CR 701.61's one-card
// boundary: with exactly one library card to look at, the ability asks
// NOTHING and manifests that card face down deterministically -- its
// controller had only one legal answer.
func TestZimoneManifestDreadSingleCardLibrary(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Zimone, Mystery Unraveler")
	zid := searchMoveByName(t, e, "Zimone, Mystery Unraveler", state.ZHand)
	if zid == 0 {
		t.Fatal("test precondition: Zimone must be in seat 0's hand or library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: zid, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(zid).Zone != state.ZBattlefield {
		t.Fatal("test precondition: Zimone did not enter the battlefield")
	}
	dreadTrimLibrary(t, e, 1)
	top := e.G.Zone(state.ZLibrary, 0)[0]
	if e.G.Obj(top).Zone != state.ZLibrary {
		t.Fatalf("test precondition: offered card %d is in %s, want the library", top, e.G.Obj(top).Zone)
	}
	dreadDriveToResolution(t, e, func(e *Engine) bool {
		o := e.G.Obj(top)
		return o.Zone == state.ZBattlefield && o.FaceDown
	})
	o := e.G.Obj(top)
	if o.Zone != state.ZBattlefield || !o.FaceDown || o.Controller != 0 {
		t.Fatalf("manifested card zone=%s FaceDown=%v Controller=%d, want battlefield/true/0", o.Zone, o.FaceDown, o.Controller)
	}
	der := e.Derived(top)
	if der.Power != 2 || der.Toughness != 2 || len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("manifested card derived=%+v, want face-down 2/2 Creature", der)
	}
	dreadAssertNoFallback(t, e)
	dreadLookCount(t, e, top)
	replayCheck(t, e, cfg)
}

// TestZimoneManifestDreadEmptyLibrary pins the zero-card boundary: an empty
// library does nothing loudly -- no look, no ask, no move -- and never the
// unimplemented-API fallback.
func TestZimoneManifestDreadEmptyLibrary(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Zimone, Mystery Unraveler")
	zid := searchMoveByName(t, e, "Zimone, Mystery Unraveler", state.ZHand)
	if zid == 0 {
		t.Fatal("test precondition: Zimone must be in seat 0's hand or library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: zid, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(zid).Zone != state.ZBattlefield {
		t.Fatal("test precondition: Zimone did not enter the battlefield")
	}
	dreadTrimLibrary(t, e, 0)
	before := len(e.L.Events)
	dreadDriveToResolution(t, e, nil)
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield && ev.Counter == "entered_face_down" {
			t.Fatalf("an empty library still manifested a card: %+v", ev)
		}
		if ev.Kind == events.Note && ev.Text == "looks at the top two cards of the library" {
			t.Fatalf("an empty library still recorded a look: %q", ev.Text)
		}
	}
	dreadAssertNoFallback(t, e)
}

// TestManifestDreadOutOfScopeShapesStayLoud pins the fail-loud contract:
// ManifestDread lines carrying parameters this build does not implement
// (Amount$ 1 — a count other than CR 701.61's top two; DefinedPlayer$ — a
// library other than the resolving controller's; RememberManifested$ True —
// the DBAttach/DBPutCounter rider family needs the manifested object
// remembered; Choices$ — the effManifest chooser forms) emit the SAME
// "unimplemented API ManifestDread" note the unimplemented fallback always
// emitted, and move nothing: no library→battlefield manifest move, no silent
// wrong-count, wrong-player or wrong-card move.
func TestManifestDreadOutOfScopeShapesStayLoud(t *testing.T) {
	for _, tc := range []struct{ name, params string }{
		{"Amount", "Amount$ 1"},
		{"DefinedPlayer", "DefinedPlayer$ TargetedController"},
		{"RememberManifested", "RememberManifested$ True"},
		{"Choices", "Choices$ Creature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Loud Dread\nManaCost:0\nTypes:Instant\n" +
				"A:SP$ ManifestDread | " + tc.params + "\nOracle:x\n"
			e, cfg, id := newFixtureDeck(t, 4402, src)
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
			if !hasNote(e, "unimplemented API ManifestDread") {
				t.Fatal("out-of-scope ManifestDread shape was silent: no fallback note")
			}
			manifestedOnBf := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.MoveZone && ev.From == state.ZLibrary &&
					ev.To == state.ZBattlefield && ev.Counter == "entered_face_down" {
					manifestedOnBf = true
				}
			}
			if manifestedOnBf {
				t.Fatal("out-of-scope ManifestDread shape moved a card")
			}
			if len(e.G.Zone(state.ZLibrary, 0)) != len(libBefore) {
				t.Fatal("library size changed on a failed manifest dread")
			}
			replayCheck(t, e, cfg)
		})
	}
}
