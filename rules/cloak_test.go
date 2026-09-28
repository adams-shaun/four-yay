package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// api:Cloak pins (CR 708.5's cloak variant): a cloak is a real card move onto
// the battlefield FACE DOWN as a 2/2 creature WITH WARD {2} -- the shared
// face-down machinery api:Manifest already landed (FaceDown state, the view's
// redaction, the derived faceDownBasis) plus the two cloak-only pieces: the
// Cloaked state marker (folded from the MoveZone Counter value
// "entered_cloaked", no new event kind) and the ward, which is part of the
// cloak STATUS itself and reaches the targeting ask through the
// derived-keyword granted-ward walk. The helpers come from manifest_test.go
// and search_library_test.go: the decks are built from compiled corpus cards
// only, so no Forge script text is committed here.
//
// None of the 11 corpus Cloak carriers is in any repo deck (measured), so
// these tests do not move the golden heads.

// cloakedOn finds the cloaked face-down battlefield object seat p controls.
func cloakedOn(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Cloaked && o.FaceDown {
			return id
		}
	}
	t.Fatalf("seat %d controls no cloaked permanent", p)
	return 0
}

// noUnimplementedCloak fails if the log carries the loud fallback note.
func noUnimplementedCloak(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API Cloak") {
			t.Fatalf("log carries the unimplemented-API note: %q", ev.Text)
		}
	}
}

// cloakTop moves the top card of seat p's library onto p's battlefield with
// the exact logged MoveZone the effCloak primitive emits (the direct-event
// setup oppBear's precedent uses), returning the id.
func cloakTop(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		t.Fatalf("seat %d has no library to cloak from", p)
	}
	top := lib[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: top, Player: p,
		From: state.ZLibrary, To: state.ZBattlefield,
		Counter: "entered_cloaked", Secret: true})
	return top
}

// cloakBear moves one of seat p's Grizzly Bears onto p's battlefield with
// the exact logged MoveZone the effCloak primitive emits (the direct-event
// setup oppBear's precedent uses), returning the id. The bear is chosen
// deliberately: an ordinary target filter reads the PRINTED face (the
// documented layer-4 limitation a manifested/cloaked card shares --
// AGENTS.md's manifest row), so a creature-targeting spell sees the cloaked
// bear but would not see a cloaked Forest.
func cloakBear(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, Player: p,
					From: z, To: state.ZBattlefield,
					Counter: "entered_cloaked", Secret: true})
				return id
			}
		}
	}
	t.Fatalf("seat %d has no Grizzly Bears to cloak", p)
	return 0
}

// bearOn moves one of seat p's Grizzly Bears onto p's battlefield with a
// logged MoveZone and returns its id (oppBear generalized over the seat).
func bearOn(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				return id
			}
		}
	}
	t.Fatalf("seat %d has no Grizzly Bears in hand/library", p)
	return 0
}

// castVanilla casts the named card in seat 0's hand with no target and
// resolves it (pass priority both seats until the stack empties).
func castVanilla(t *testing.T, e *Engine, name string, mana string) {
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
	passUntilStackEmpty(t, e, 20)
}

// counterCount reads one counter kind's total off an object's counter slice.
func counterCount(o *state.Object, kind string) int32 {
	for i := range o.Counters {
		if o.Counters[i].Kind == kind {
			return o.Counters[i].N
		}
	}
	return 0
}

// passUntilAsk passes priority decisions until a non-priority decision of
// the wanted kind is pending (a resolution suspended on its ask), and returns
// it -- passUntilStackEmpty's stopping analogue for a mid-resolution ask.
func passUntilAskKind(t *testing.T, e *Engine, want decision.Kind, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending while looking for the ask")
		}
		if d.Kind != decision.KPriority {
			if d.Kind == want {
				return d
			}
			t.Fatalf("unexpected non-priority decision %+v (want %s)", d, want)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}
	t.Fatalf("no %s decision within the pass budget", want)
	return nil
}

// castAtUntilAsk casts the named card in seat 0's hand at the given target
// and passes priority until the resolution suspends on a KModes ask (the
// ward pay-or-counter question), returning it. castAtOpponent's shape minus
// the drain-to-empty, which a ward ask interrupts.
func castAtUntilAsk(t *testing.T, e *Engine, name string, target state.ObjID, mana string) *decision.Decision {
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
		t.Fatalf("after casting %s: %+v, want the target ask", name, d)
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
	return passUntilAskKind(t, e, decision.KModes, 20)
}

// cloakedDerived is the shared CR 708.5 cloak-status assertion block.
func cloakedDerived(t *testing.T, e *Engine, id state.ObjID, controller state.PlayerID) {
	t.Helper()
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || !o.FaceDown || !o.Cloaked {
		t.Fatalf("cloaked card: zone %s FaceDown %v Cloaked %v", o.Zone, o.FaceDown, o.Cloaked)
	}
	if o.Controller != controller || o.Owner != controller {
		t.Fatalf("cloaked card Controller=%d Owner=%d, want %d", o.Controller, o.Owner, controller)
	}
	der := e.Derived(id)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("cloaked P/T = %d/%d, want 2/2", der.Power, der.Toughness)
	}
	if len(der.Types) != 1 || der.Types[0] != "Creature" {
		t.Fatalf("cloaked types = %v, want exactly [Creature]", der.Types)
	}
	hasWard := false
	for _, k := range der.Keywords {
		if k == "Ward:2" {
			hasWard = true
		}
	}
	if !hasWard {
		t.Fatalf("cloaked keywords = %v, want Ward:2", der.Keywords)
	}
	if !e.IsCreature(id) {
		t.Fatal("cloaked card is not a creature per IsCreature")
	}
}

// TestVeiledAscensionUpkeepCloaksFaceDown is the ticket's primary carrier end
// to end: Veiled Ascension's upkeep trigger (OptionalDecider$ You -- the ask
// is real) resolves DB$ Cloak | Amount$ 1 | Defined$ TopOfLibrary, the top
// card of the controller's library lands face down as a cloaked 2/2 with the
// derived ward, and no unimplemented-API note is emitted.
func TestVeiledAscensionUpkeepCloaksFaceDown(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Veiled Ascension")
	castVanilla(t, e, "Veiled Ascension", "WWWW")

	// The trigger's Phase$ Upkeep | ValidPlayer$ You: drive to seat 0's next
	// upkeep (turn 3 -- seat 1 takes turn 2 in between).
	driveToStep(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	d := passUntilAskKind(t, e, decision.KTriggerOptional, 30)
	if d.Player != 0 {
		t.Fatalf("optional ask player = %d, want the decider seat 0", d.Player)
	}
	yes := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yes = o.Index
		}
	}
	if yes < 0 {
		t.Fatalf("no yes option on the optional ask: %+v", d.Options)
	}
	top := e.G.Zone(state.ZLibrary, 0)[0]
	topName := e.G.Obj(top).Face().Name
	submitChoices(t, e, yes)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(top).Zone; got != state.ZBattlefield {
		t.Fatalf("cloaked card (%s) zone = %s, want battlefield", topName, got)
	}
	cloakedDerived(t, e, top, 0)
	if got := e.G.Obj(top).Controller; got != 0 {
		t.Fatalf("cloaked card controller = %d, want 0", got)
	}
	noUnimplementedManifest(t, e)
	noUnimplementedCloak(t, e)

	// The cloak move is Secret and carries the cloak marker.
	sawMarker := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == top && ev.Counter == "entered_cloaked" {
			sawMarker = true
			if !ev.Secret || ev.Player != 0 {
				t.Fatalf("cloak move Secret=%v Player=%d, want Secret for seat 0", ev.Secret, ev.Player)
			}
		}
	}
	if !sawMarker {
		t.Fatal("no entered_cloaked MoveZone in the log")
	}

	// The view redacts the cloaked card exactly like a manifested one (the
	// FaceDown rule covers both variants; the brief forbids touching it).
	oppView := view.Project(e.G, e, 1, nil)
	var blank *view.CardView
	for i := range oppView.Players[0].Battlefield {
		if oppView.Players[0].Battlefield[i].ID == top {
			blank = &oppView.Players[0].Battlefield[i]
		}
	}
	if blank == nil || blank.Name != "" || !blank.FaceDown {
		t.Fatalf("opponent's view of the cloaked card: %+v, want the redacted blank", blank)
	}

	replayCheck(t, e, cfg)
}

// TestVeiledAscensionFlyingCountersFaceDownCreatures exercises the
// faceDown filter predicate through the card's own ETB trigger: with a
// face-down creature already under seat 0's control, Veiled Ascension's
// "put a flying counter on each face-down creature you control"
// (ValidCards$ Creature.faceDown+YouCtrl) lands the counter on the cloaked
// 2/2. Before the predicate existed the spec failed closed and the counter
// never landed.
func TestVeiledAscensionFlyingCountersFaceDownCreatures(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Veiled Ascension", "Grizzly Bears")
	// A bear face, not a Forest: the ETB trigger's ValidCards$ spec and every
	// ordinary target filter read the PRINTED face (the documented layer-4
	// limitation AGENTS.md's manifest row carries), so the faceDown
	// predicate is only reachable on a card whose printed face is a creature.
	top := cloakBear(t, e, 0)
	castVanilla(t, e, "Veiled Ascension", "WWWW")

	o := e.G.Obj(top)
	if o.Zone != state.ZBattlefield || !o.FaceDown || !o.Cloaked {
		t.Fatalf("setup cloaked card: zone %s FaceDown %v Cloaked %v", o.Zone, o.FaceDown, o.Cloaked)
	}
	if n := counterCount(o, "Flying"); n != 1 {
		t.Fatalf("cloaked card flying counters = %d, want 1 (the ETB trigger's faceDown sweep)", n)
	}
	noUnimplementedCloak(t, e)
	replayCheck(t, e, cfg)
}

// TestCloakedWardDeclinedCountersTheSpell pins the ward {2} end to end
// through the granted-ward walk: targeting a cloaked 2/2 meets the
// pay-or-counter ask, and declining counters the targeting spell with no
// effect while the cloaked card stays.
func TestCloakedWardDeclinedCountersTheSpell(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Lightning Bolt")
	top := cloakBear(t, e, 1)
	cloakedDerived(t, e, top, 1)

	d := castAtUntilAsk(t, e, "Lightning Bolt", top, "R")
	if d == nil || d.Kind != decision.KModes || d.Player != 0 {
		t.Fatalf("after targeting the cloaked 2/2: %+v, want seat 0's ward pay ask", d)
	}
	decline := -1
	for _, o := range d.Options {
		if o.Label == "Don't pay" {
			decline = o.Index
		}
	}
	if decline < 0 {
		t.Fatalf("no decline option on the ward ask: %+v", d.Options)
	}
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(top).Zone; got != state.ZBattlefield {
		t.Fatalf("cloaked card zone after the declined ward = %s, want still battlefield", got)
	}
	countered := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj != top && ev.Text == "countered by ward" {
			countered = true
		}
	}
	if !countered {
		t.Fatal("the targeting spell was not countered by ward")
	}
	noUnimplementedCloak(t, e)
	replayCheck(t, e, cfg)
}

// TestCloakedWardPaidLetsTheSpellResolve pins the pay half: a payer with the
// floating mana chooses Pay and the spell resolves normally, killing the 2/2
// (which also pins the leave-battlefield reveal for the combat-damage-free
// lethal-damage path).
func TestCloakedWardPaidLetsTheSpellResolve(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Lightning Bolt")
	top := cloakBear(t, e, 1)
	topName := "Grizzly Bears"

	d := castAtUntilAsk(t, e, "Lightning Bolt", top, "RCC")
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("after targeting the cloaked 2/2: %+v, want the ward pay ask", d)
	}
	pay := -1
	for _, o := range d.Options {
		if o.Index == 0 && o.Label == "Pay 2" {
			pay = o.Index
		}
	}
	if pay < 0 {
		t.Fatalf("no Pay option on the ward ask: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(top).Zone; got != state.ZGraveyard {
		t.Fatalf("lethally damaged cloaked card zone = %s, want graveyard", got)
	}
	o := e.G.Obj(top)
	if o.FaceDown || o.Cloaked {
		t.Fatalf("revealed card: FaceDown %v Cloaked %v, want both cleared (CR 708.9)", o.FaceDown, o.Cloaked)
	}
	if o.Face() == nil || o.Face().Name != topName {
		t.Fatalf("revealed face = %+v, want %q", o.Face(), topName)
	}
	noUnimplementedCloak(t, e)
	replayCheck(t, e, cfg)
}

// TestManifestedCardGetsNoWard pins the gate change's boundary: the
// face-down early return still suppresses the granted-ward walk for a PLAIN
// manifested card -- only the cloak status revives it.
func TestManifestedCardGetsNoWard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Lightning Bolt")
	// Manifest seat 1's Grizzly Bears with the pre-existing marker value (a
	// creature face, so the bolt's printed-face Creature filter offers it).
	var top state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 1) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				top = id
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, Player: 1,
					From: z, To: state.ZBattlefield,
					Counter: "entered_face_down", Secret: true})
				break
			}
		}
		if top != 0 {
			break
		}
	}
	if top == 0 {
		t.Fatal("seat 1 has no Grizzly Bears to manifest")
	}
	if e.G.Obj(top).Cloaked {
		t.Fatal("a manifested card must not carry Cloaked")
	}
	for _, k := range e.Derived(top).Keywords {
		if k == "Ward:2" {
			t.Fatal("a manifested card must not derive ward")
		}
	}

	d := castAtOpponent(t, e, "Lightning Bolt", top, "R")
	if d != nil && d.Kind == decision.KModes {
		t.Fatalf("targeting a manifested card met a ward ask: %+v", d)
	}
	if got := e.G.Obj(top).Zone; got != state.ZGraveyard {
		t.Fatalf("bolted manifested card zone = %s, want graveyard (the bolt resolved, no ward)", got)
	}
	if got := e.G.Obj(top); got.FaceDown || got.Cloaked {
		t.Fatalf("revealed card: FaceDown %v Cloaked %v, want both cleared (CR 708.9)", got.FaceDown, got.Cloaked)
	}
	noUnimplementedCloak(t, e)
	replayCheck(t, e, cfg)
}

// TestCloakedLeavesBattlefieldReveals pins CR 708.9 for the cloak variant: a
// cloaked card that leaves the battlefield clears BOTH status flags and its
// identity is visible again.
func TestCloakedLeavesBattlefieldReveals(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg)
	top := cloakTop(t, e, 0)
	topName := e.G.Obj(top).Face().Name
	cloakedDerived(t, e, top, 0)

	e.emit(events.Event{Kind: events.MoveZone, Obj: top, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	o := e.G.Obj(top)
	if o.FaceDown || o.Cloaked {
		t.Fatalf("in the graveyard: FaceDown %v Cloaked %v, want both cleared", o.FaceDown, o.Cloaked)
	}
	if o.Face() == nil || o.Face().Name != topName {
		t.Fatalf("revealed face = %+v, want %q", o.Face(), topName)
	}
	// The view shows the card unredacted in the graveyard.
	v := view.Project(e.G, e, 1, nil)
	for _, c := range v.Players[0].Graveyard {
		if c.ID == top && c.Name != topName {
			t.Fatalf("opponent's view of the revealed card: %+v, want %q", c, topName)
		}
	}
	noUnimplementedCloak(t, e)
	replayCheck(t, e, cfg)
}

// TestCloakOutOfScopeShapesStayLoud pins the fail-loud contract: a Cloak
// whose shape this build does not implement (the Choices$ cloak-from-hand
// chooser, Defined$ ValidLibrary) emits the SAME "unimplemented API Cloak"
// note the unimplemented fallback always emitted, and moves nothing.
func TestCloakOutOfScopeShapesStayLoud(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, sa string }{
		{"choices", "A:SP$ Cloak | Choices$ Card.YouCtrl"},
		{"valid-library", "A:SP$ Cloak | Defined$ ValidLibrary Card.TopLibrary"},
	} {
		src := "Name:Loud Cloak\nManaCost:0\nTypes:Instant\n" + tt.sa + "\nOracle:x\n"
		e, cfg, id := newFixtureDeck(t, 4410, src)
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
		if !hasNote(e, "unimplemented API Cloak") {
			t.Fatalf("%s: out-of-scope Cloak shape was silent: no fallback note", tt.name)
		}
		for i, id := range e.G.Zone(state.ZLibrary, 0) {
			if id != libBefore[i] {
				t.Fatalf("%s: library order changed: %v -> %v", tt.name, libBefore, e.G.Zone(state.ZLibrary, 0))
			}
		}
		for _, ev := range e.L.Events {
			if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield &&
				ev.Counter == "entered_cloaked" {
				t.Fatalf("%s: out-of-scope Cloak moved a card onto the battlefield", tt.name)
			}
		}
		replayCheck(t, e, cfg)
	}
}

// TestUnexplainedAbsenceCloaksEachControllerTop is the per-player carrier end
// to end: Unexplained Absence exiles up to one nonland permanent each player
// controls (TargetsForEachPlayer$ True, implemented) and its
// "Defined$ TopOfLibrary | DefinedPlayer$ RememberedController" sub-cloak
// takes each listed controller's OWN top card -- never the caster's.
func TestUnexplainedAbsenceCloaksEachControllerTop(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Unexplained Absence", "Grizzly Bears")
	bear0 := bearOn(t, e, 0)
	bear1 := oppBear(t, e)
	top0 := e.G.Zone(state.ZLibrary, 0)[0]
	top1 := e.G.Zone(state.ZLibrary, 1)[0]

	id := searchMoveByName(t, e, "Unexplained Absence", state.ZHand)
	addMana(t, e, 0, "WWWW")
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("no cast option for Unexplained Absence: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after casting: %+v, want the target ask", d)
	}
	pick := func(target state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == target {
				return o.Index
			}
		}
		t.Fatalf("target %d not offered: %+v", target, d.Options)
		return -1
	}
	submitChoices(t, e, pick(bear0), pick(bear1))
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(bear0).Zone; got != state.ZExile {
		t.Fatalf("seat 0's bear zone = %s, want exile", got)
	}
	if got := e.G.Obj(bear1).Zone; got != state.ZExile {
		t.Fatalf("seat 1's bear zone = %s, want exile", got)
	}
	cloakedDerived(t, e, top0, 0)
	cloakedDerived(t, e, top1, 1)
	noUnimplementedManifest(t, e)
	noUnimplementedCloak(t, e)
	replayCheck(t, e, cfg)
}
