package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The handmove1 effects-level leaves for the "choose N cards matching
// ChangeType$ from Origin$ Hand" ChangeZone shape, using the askHost double
// (primitives_test.go) whose Ask captures the posed decision and suspends,
// and the embedded fakeHost whose Ask returns false -- the R-9 no-host
// stand-in. The rules-package halves (the real-engine end to end through
// Burgeoning's trigger) live in rules/hand_move_test.go.

// handAskFixture seats [bear, isle, isle, bear] in seat 0's HAND and returns
// (host, ids). SetZone moves the zone slice only; each object's own Zone
// field must be set to match (the same two-step every effects hand fixture
// uses).
func handAskFixture(t *testing.T) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(bear, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
	}
	h.g.SetZone(state.ZHand, 0, ids)
	for _, id := range ids {
		h.g.Obj(id).Zone = state.ZHand
	}
	return h, ids
}

// handNoHostFixture is handAskFixture over the bare fakeHost (Ask false --
// the R-9 no-host stand-in) rather than the capturing askHost.
func handNoHostFixture(t *testing.T) (*fakeHost, []state.ObjID) {
	t.Helper()
	h := &fakeHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
	}
	h.g.SetZone(state.ZHand, 0, ids)
	for _, id := range ids {
		h.g.Obj(id).Zone = state.ZHand
	}
	return h, ids
}

const handAskSA = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Mandatory$ True"

// TestHandMoveChangeZoneNoChoiceMovesDeterministically is the required
// no-choice leaf: with exactly ChangeNum eligible cards (and with fewer), a
// Mandatory$ True take is deterministic and no decision is posed.
func TestHandMoveChangeZoneNoChoiceMovesDeterministically(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 2 | Mandatory$ True"))
	if h.asked != nil {
		t.Fatalf("a decision was posed with eligible == ChangeNum: %+v", h.asked)
	}
	if o := h.g.Obj(ids[1]); o.Zone != state.ZBattlefield {
		t.Fatalf("eligible card 1 on %s, want battlefield", o.Zone)
	}
	if o := h.g.Obj(ids[2]); o.Zone != state.ZBattlefield {
		t.Fatalf("eligible card 2 on %s, want battlefield", o.Zone)
	}
	for _, id := range []state.ObjID{ids[0], ids[3]} {
		if o := h.g.Obj(id); o.Zone != state.ZHand {
			t.Fatalf("ineligible bear moved: on %s", o.Zone)
		}
	}
}

// TestHandMoveChangeZoneZeroEligibleIsASilentNoOp is the zero-eligible leaf:
// nothing matching in hand resolves doing nothing -- no ask, no move, no
// event of any kind.
func TestHandMoveChangeZoneZeroEligibleIsASilentNoOp(t *testing.T) {
	h, ids := handAskFixture(t)
	h.g.SetZone(state.ZHand, 0, []state.ObjID{ids[0], ids[3]}) // bears only
	before := len(h.log)
	Resolve(h, &Ctx{Controller: 0}, sa(t, handAskSA))
	if h.asked != nil {
		t.Fatalf("a decision was posed with zero eligible cards: %+v", h.asked)
	}
	if len(h.log) != before {
		t.Fatalf("events emitted with zero eligible cards: %+v", h.log[before:])
	}
}

// TestHandMoveChangeZoneNoHostTakesFirstEligible is the R-9 fallback leaf: a
// host without a decision channel takes the first ChangeNum eligible cards in
// the decision's own deterministic option order, with the Note recording why.
func TestHandMoveChangeZoneNoHostTakesFirstEligible(t *testing.T) {
	h, ids := handNoHostFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Land | ChangeNum$ 1 | Mandatory$ True"))
	var note *events.Event
	for i := range h.log {
		if h.log[i].Kind == events.Note {
			note = &h.log[i]
		}
	}
	if note == nil || note.Player != 0 {
		t.Fatalf("no R-9 Note for the controller in %+v", h.log)
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZExile {
		t.Fatalf("first eligible card on %s, want exile", o.Zone)
	}
	if o := h.g.Obj(ids[1]); o.Zone != state.ZHand {
		t.Fatalf("second eligible card moved: on %s", o.Zone)
	}
}

// TestHandMoveChangeZoneUnknownChangeNumEmitsNoteAndStaysSilent pins the
// loud fallback for a count expression Num cannot resolve. It must not fall
// through to Defined's source default and silently do nothing.
func TestHandMoveChangeZoneUnknownChangeNumEmitsNoteAndStaysSilent(t *testing.T) {
	for _, num := range []string{"CountAuras", "HandX", "2147483648"} {
		h, ids := handAskFixture(t)
		before := len(h.log)
		Resolve(h, &Ctx{Controller: 0}, sa(t,
			"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ "+num))
		if h.asked != nil {
			t.Fatalf("ChangeNum$ %s posed a decision: %+v", num, h.asked)
		}
		for _, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZHand {
				t.Fatalf("ChangeNum$ %s moved ids[%d] to %s", num, id, o.Zone)
			}
		}
		notes := 0
		for _, ev := range h.log[before:] {
			if ev.Kind == events.Note {
				notes++
			}
		}
		if notes != 1 {
			t.Fatalf("ChangeNum$ %s emitted %d Notes, want exactly 1: %+v", num, notes, h.log[before:])
		}
	}
}

// TestHandMoveChangeZoneEvaluatesWholeHandCounts proves the whole-hand route
// shares handMoveCountOf with the owner-selected route: NumInHand, an SVar
// count and the equivalent inline Count$ expression all use the count as the
// bound rather than being rejected merely because it is non-literal.
func TestHandMoveChangeZoneEvaluatesWholeHandCounts(t *testing.T) {
	for _, tc := range []struct {
		name, count string
		svars       map[string]string
	}{
		{"NumInHand", "NumInHand", nil},
		{"SVar", "X", map[string]string{"X": "Count$ValidHand Land.YouCtrl"}},
		{"inline", "Count$ValidHand Land.YouCtrl", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, ids := handAskFixture(t)
			Resolve(h, &Ctx{Controller: 0, SVars: tc.svars}, sa(t,
				"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ "+tc.count+" | Mandatory$ True"))
			if h.asked != nil {
				t.Fatalf("ChangeNum$ %s posed a decision: %+v", tc.count, h.asked)
			}
			for _, id := range []state.ObjID{ids[1], ids[2]} {
				if o := h.g.Obj(id); o.Zone != state.ZBattlefield {
					t.Fatalf("ChangeNum$ %s left eligible land %d on %s, want battlefield", tc.count, id, o.Zone)
				}
			}
			for _, ev := range h.log {
				if ev.Kind == events.Note {
					t.Fatalf("ChangeNum$ %s emitted unsupported-count Note: %+v", tc.count, ev)
				}
			}
		})
	}
}

// TestHandMoveChangeZoneRejectsOutOfRangeChangeNum proves a literal that
// cannot fit Decision.Min/Max's int32 count routes through the non-literal
// Note path: no ask, no move, no silent no-op.
func TestHandMoveChangeZoneRejectsOutOfRangeChangeNum(t *testing.T) {
	const overflow = "2147483648"
	if n, ok := handChangeNum(ChangeZoneOf(sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ "+overflow))); ok || n != 0 {
		t.Fatalf("handChangeNum(%s) = %d, %v; want 0, false", overflow, n, ok)
	}

	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ "+overflow))
	if h.asked != nil {
		t.Fatalf("out-of-range ChangeNum posed a decision: %+v", h.asked)
	}
	for _, id := range ids {
		if o := h.g.Obj(id); o.Zone != state.ZHand {
			t.Fatalf("out-of-range ChangeNum moved ids[%d] to %s", id, o.Zone)
		}
	}
	sawNote := false
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("out-of-range ChangeNum emitted no Note: %+v", h.log)
	}
}

// TestHandMoveChangeZoneUntypedSpecDefaultsToWholeHand is the rv2b core
// leaf: with NO ChangeType$ the eligible pool is the whole hand --
// Brainstorm's "put two cards from your hand on top" must offer every card
// in hand, not silently no-op the way the pre-rv2b object path did.
func TestHandMoveChangeZoneUntypedSpecDefaultsToWholeHand(t *testing.T) {
	h, _ := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 2 | Mandatory$ True | Reorder$ True"))
	if h.asked == nil {
		t.Fatal("no decision was posed: the untyped put-back must offer the whole hand")
	}
	d := h.asked
	if d.Min != 2 || d.Max != 2 || len(d.Options) != 4 {
		t.Fatalf("decision = Min %d Max %d with %d options, want 2/2 and the whole hand", d.Min, d.Max, len(d.Options))
	}
	for _, o := range d.Options {
		if o.Label != "Bear" && o.Label != "Isle" {
			t.Fatalf("option label %q is not a hand card", o.Label)
		}
	}
}

// TestHandMoveTextOptionalKeepsTheMayInItsOwnSentence prevents a card with
// several instructions from borrowing an unrelated optional one. The later
// hand-to-library instruction is Volrath's Dungeon's mandatory grammar.
func TestHandMoveTextOptionalKeepsTheMayInItsOwnSentence(t *testing.T) {
	optional, known := handMoveTextOptional(
		"You may put a token onto the battlefield. Target player puts a card from their hand on top of their library.", state.ZLibrary)
	if !known || optional {
		t.Fatalf("optional/known = %v/%v, want false/true", optional, known)
	}
	optional, known = handMoveTextOptional(
		"Target player reveals their hand. You may put a creature card from it onto the battlefield.", state.ZBattlefield)
	if !known || !optional {
		t.Fatalf("hand-reveal optional/known = %v/%v, want true/true", optional, known)
	}
	optional, known = handMoveTextOptional(
		"Target player reveals their hand. You choose a card from it. Exile that card.", state.ZExile)
	if !known || optional {
		t.Fatalf("hand-choice required optional/known = %v/%v, want false/true", optional, known)
	}
}

// TestHandMoveChangeZoneMarkerlessWithoutTextIsLoud prevents a synthetic or
// otherwise unannotated ChangeZone from inventing a "may" choice. Forge's
// parameters do not encode the default; only real card/script wording does.
func TestHandMovePromptNamesOtherPlayersLibrary(t *testing.T) {
	line := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Library | LibraryPosition$ -1")
	got := handMovePromptFor(ChangeZoneOf(line), state.ZLibrary, 1, false)
	if !strings.Contains(got, "that player's hand") || !strings.Contains(got, "the bottom of that player's library") {
		t.Fatalf("prompt = %q, want the other player's hand and library", got)
	}
}

func TestHandMoveChangeZoneMarkerlessWithoutTextIsLoud(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 1"))
	if h.asked != nil {
		t.Fatalf("markerless, textless move posed a choice: %+v", h.asked)
	}
	if len(h.log) != 1 || h.log[0].Kind != events.Note ||
		!strings.Contains(h.log[0].Text, "cannot determine whether") {
		t.Fatalf("log = %+v, want one optionality Note", h.log)
	}
	for _, id := range ids {
		if h.g.Obj(id).Zone != state.ZHand {
			t.Fatalf("markerless, textless move changed card %d to %s", id, h.g.Obj(id).Zone)
		}
	}
}

// --- rv2b: the two remaining brief leaves on the real corpus scripts. ---

// brainstormSubAbility returns the real compiled ChangeZoneDB sub-ability of
// the real corpus Brainstorm card (the Draw's SubAbility$ chain), so the
// whole-hand mandatory put-back is exercised on the script the card ships.
func brainstormSubAbility(t *testing.T, reg *cards.Registry) *cards.SA {
	t.Helper()
	brainstorm, ok := reg.Lookup("Brainstorm")
	if !ok {
		t.Fatal("corpus has no Brainstorm")
	}
	for _, ab := range brainstorm.Faces[0].Abilities {
		if ab.API == "Draw" && ab.Sub != nil && ab.Sub.API == "ChangeZone" {
			return ab.Sub
		}
	}
	t.Fatal("Brainstorm's Draw has no ChangeZone sub-ability in the compiled corpus")
	return nil
}

// TestBrainstormRealScriptMandatoryPutBackWithFewerEligibleCards runs the
// REAL compiled Brainstorm sub-ability (ChangeNum$ 2, Mandatory$ True, no
// ChangeType$) with only ONE eligible card in hand: the mandatory put-back
// takes it without an ask (a decision nobody could answer differently), and
// the card lands on top (Forge's absent-LibraryPosition$ default).
func TestBrainstormRealScriptMandatoryPutBackWithFewerEligibleCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	db := brainstormSubAbility(t, reg)
	if db.Params["ChangeNum"] != "2" || db.Params["Mandatory"] != "True" || db.Params["ChangeType"] != "" {
		t.Fatalf("Brainstorm's compiled sub-ability drifted: %+v", db.Params)
	}
	g := state.NewGame(names(2))
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	g.SetZone(state.ZHand, 0, []state.ObjID{bear.ID})
	bear.Zone = state.ZHand

	h := &askHost{}
	h.g = g
	Resolve(h, &Ctx{Controller: 0}, db)
	if h.asked != nil {
		t.Fatalf("a decision was posed with eligible (1) < ChangeNum (2): %+v", h.asked)
	}
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 1 || lib[0] != bear.ID {
		t.Fatalf("library = %v, want exactly the bear on top", lib)
	}
	if o := h.g.Obj(bear.ID); o.Zone != state.ZLibrary {
		t.Fatalf("bear on %s, want library", o.Zone)
	}
}

// TestVolrathsDungeonRealScriptMarkerlessPutBackIsRequired proves that
// absent Optional$/Mandatory$ is not itself a decline. Volrath's Dungeon's
// oracle says the target "puts a card"; with one hand card it therefore moves
// deterministically to the top of that target's library rather than posing a
// 0..1 chooser.
func TestVolrathsDungeonRealScriptMarkerlessPutBackIsRequired(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Volrath's Dungeon")
	if !ok {
		t.Fatal("corpus has no Volrath's Dungeon")
	}
	var ab *cards.SA
	for _, candidate := range card.Faces[0].Abilities {
		if candidate.API == "ChangeZone" && candidate.Params["Origin"] == "Hand" {
			ab = candidate
			break
		}
	}
	if ab == nil || ab.Params["Mandatory"] != "" || ab.Params["Optional"] != "" {
		t.Fatalf("Volrath's Dungeon compiled ability drifted: %+v", ab)
	}
	g := state.NewGame(names(2))
	dungeon := corpusObject(t, reg, g, "Volrath's Dungeon")
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	bear.Owner, bear.Controller = 1, 1
	g.SetZone(state.ZHand, 1, []state.ObjID{bear.ID})
	bear.Zone = state.ZHand
	h := &askHost{}
	h.g = g
	Resolve(h, &Ctx{Source: dungeon.ID, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, ab)
	if h.asked != nil {
		t.Fatalf("one-card mandatory hand move posed a decision: %+v", h.asked)
	}
	lib := g.Zone(state.ZLibrary, 1)
	if len(lib) != 1 || lib[0] != bear.ID || g.Obj(bear.ID).Zone != state.ZLibrary {
		t.Fatalf("target library = %v, bear zone = %s; want the required top-deck", lib, g.Obj(bear.ID).Zone)
	}
}

// TestLostHoursUnsupportedLibraryPositionEmitsNote runs the one corpus
// hand-move spelling with LibraryPosition$ 2 (third from the top). The
// hidden-hand mover cannot place a card at an arbitrary library index, so it
// takes the deterministic top placement but makes that narrowing visible.
func TestLostHoursUnsupportedLibraryPositionEmitsNote(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Lost Hours")
	if !ok {
		t.Fatal("corpus has no Lost Hours")
	}
	var ab *cards.SA
	for _, candidate := range card.Faces[0].Abilities {
		if candidate.API == "ChangeZone" && candidate.Params["Origin"] == "Hand" {
			ab = candidate
			break
		}
	}
	if ab == nil || ab.Params["LibraryPosition"] != "2" {
		t.Fatalf("Lost Hours compiled ability drifted: %+v", ab)
	}
	g := state.NewGame(names(2))
	source := corpusObject(t, reg, g, "Lost Hours")
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	bear.Owner, bear.Controller, bear.Zone = 1, 1, state.ZHand
	g.SetZone(state.ZHand, 1, []state.ObjID{bear.ID})
	h := &askHost{}
	h.g = g
	Resolve(h, &Ctx{Source: source.ID, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, ab)
	if h.asked != nil {
		t.Fatalf("one-card required Lost Hours move posed a decision: %+v", h.asked)
	}
	if got := g.Zone(state.ZLibrary, 1); len(got) != 1 || got[0] != bear.ID {
		t.Fatalf("target library = %v, want the deterministic top placement of %d", got, bear.ID)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "LibraryPosition$ 2 is not implemented") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unsupported LibraryPosition$ 2 emitted no Note: %+v", h.log)
	}
}

// TestDreamCacheDestinationAlternativeEmitsNote pins the one genuinely
// unsupported hidden-origin shape's loudness: Dream Cache's "both on top of
// your library or both on the bottom" (DestinationAlternative$/
// LibraryPositionAlternative$) cannot be asked yet, so the alternative is
// named in a Note and the primary destination (top) is taken
// deterministically -- never a silent no-op.
func TestDreamCacheDestinationAlternativeEmitsNote(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Dream Cache")
	if !ok {
		t.Fatal("corpus has no Dream Cache")
	}
	var db *cards.SA
	for _, f := range card.Faces {
		for _, ab := range f.Abilities {
			if ab.API == "Draw" && ab.Sub != nil && ab.Sub.API == "ChangeZone" {
				db = ab.Sub
			}
		}
	}
	if db == nil || db.Params["DestinationAlternative"] == "" {
		t.Fatalf("Dream Cache's compiled sub-ability drifted: %+v", db)
	}
	g := state.NewGame(names(2))
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	mtn := corpusObject(t, reg, g, "Mountain")
	g.SetZone(state.ZHand, 0, []state.ObjID{bear.ID, mtn.ID})
	bear.Zone, mtn.Zone = state.ZHand, state.ZHand

	h := &askHost{}
	h.g = g
	Resolve(h, &Ctx{Controller: 0}, db)
	if h.asked != nil {
		t.Fatalf("a decision was posed with eligible (2) == ChangeNum (2): %+v", h.asked)
	}
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 2 {
		t.Fatalf("library = %v, want both hand cards on top", lib)
	}
	sawNote := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "DestinationAlternative$") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("no DestinationAlternative$ Note in %+v", h.log)
	}
}

// --- rv2b r2: the owner-SELECTED hidden-hand shapes (DefinedPlayer$ /
// ValidTgts$ name the hand owners; Chooser$ names who answers). ---

// ownersFixture seats two hands: seat 0 [bear, isle, isle], seat 1
// [isle, bear]. Returns the host and both hands' ids in zone order.
func ownersFixture(t *testing.T) (*askHost, []state.ObjID, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	hand0 := []state.ObjID{
		h.g.AddObject(bear, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
	}
	hand1 := []state.ObjID{
		h.g.AddObject(land, 1).ID,
		h.g.AddObject(bear, 1).ID,
		h.g.AddObject(land, 1).ID,
	}
	h.g.SetZone(state.ZHand, 0, hand0)
	h.g.SetZone(state.ZHand, 1, hand1)
	for _, id := range append(append([]state.ObjID(nil), hand0...), hand1...) {
		h.g.Obj(id).Zone = state.ZHand
	}
	return h, hand0, hand1
}

// TestHandMoveOwnersValidTgtsChooserTargeted is the Karn Liberated shape
// (ValidTgts$ Player, Chooser$ Targeted, Mandatory$ True): the hand owners
// are the chosen targets and the target answers for its own hand.
func TestHandMoveOwnersValidTgtsChooserTargeted(t *testing.T) {
	h, _, _ := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ValidTgts$ Player | ChangeType$ Card | ChangeNum$ 1 | Chooser$ Targeted | Mandatory$ True"))
	if h.asked == nil {
		t.Fatal("no decision was posed for the targeted hand owner")
	}
	d := h.asked
	if d.Player != 1 || d.Min != 1 || d.Max != 1 || d.ResumeTarget != 0 {
		t.Fatalf("ask = Player %d Min/Max %d/%d, want 1, 1/1 (the target exiles from its own hand, mandatory)", d.Player, d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want the whole target hand", len(d.Options))
	}
}

// TestHandMoveOwnersChooserYouPicksFromTheTargetHand is the Kitesail
// Freebooter shape (DefinedPlayer$ Targeted, Chooser$ You): the CASTING
// controller answers, over the target's hand, and the prompt names whose
// hand it is.
func TestHandMoveOwnersChooserYouPicksFromTheTargetHand(t *testing.T) {
	h, _, hand1 := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card | DefinedPlayer$ Targeted | Chooser$ You | ChangeNum$ 1 | Mandatory$ True"))
	if h.asked == nil {
		t.Fatal("no decision was posed")
	}
	d := h.asked
	if d.Player != 0 {
		t.Fatalf("ask Player = %d, want 0 (Chooser$ You: the caster picks)", d.Player)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want the target's whole hand", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Player != 1 || (o.Obj != hand1[0] && o.Obj != hand1[1] && o.Obj != hand1[2]) {
			t.Fatalf("option %+v is not a card of the TARGET's hand", o)
		}
	}
	if !strings.Contains(d.Prompt, "that player's hand") {
		t.Fatalf("prompt %q, want it naming that player's hand", d.Prompt)
	}
}

// TestHandMoveOwnersNumInHandTakesAllEligibleWithoutAsk is the
// Eradicate/Extirpate shape (ChangeNum$ NumInHand): the per-owner bound is
// the owner's own eligible count, so every matching card in the hand moves
// and no ask exists (a decision nobody could answer differently).
func TestHandMoveOwnersNumInHandTakesAllEligibleWithoutAsk(t *testing.T) {
	h, _, hand1 := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Creature | DefinedPlayer$ Targeted | ChangeNum$ NumInHand"))
	if h.asked != nil {
		t.Fatalf("a decision was posed under NumInHand: %+v", h.asked)
	}
	if o := h.g.Obj(hand1[1]); o.Zone != state.ZExile {
		t.Fatalf("the hand's creature on %s, want exile", o.Zone)
	}
	if o := h.g.Obj(hand1[0]); o.Zone != state.ZHand {
		t.Fatalf("the hand's land moved: on %s", o.Zone)
	}
}

// TestHandMoveOwnersPaidXCountIsTheBound pins ChangeNum$ X resolving to the
// resolution's paid X (CR 107.3i).
func TestHandMoveOwnersPaidXCountIsTheBound(t *testing.T) {
	h, _, _ := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0, X: 1,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card | DefinedPlayer$ Targeted | Chooser$ You | ChangeNum$ X | Mandatory$ True"))
	if h.asked == nil || h.asked.Max != 1 {
		t.Fatalf("X=1 bound: asked = %+v, want Max 1", h.asked)
	}
}

// TestHandMoveOwnersUnmodelledSelectorAndChooserAreLoud pins the loudness
// floor: a player selector this build does not model, and a Chooser$ value it
// does not model, each emit exactly one Note and move NOTHING -- never the
// silent no-op the object path used to be, and never a guessed seat.
func TestHandMoveOwnersUnmodelledSelectorAndChooserAreLoud(t *testing.T) {
	for _, tc := range []struct{ name, sa string }{
		{"CardOwner selector", "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | DefinedPlayer$ CardOwner | ChangeNum$ 1"},
		{"unknown chooser", "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card | DefinedPlayer$ Targeted | Chooser$ Remembered | ChangeNum$ 1"},
		{"unresolvable count", "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card | DefinedPlayer$ Targeted | ChangeNum$ CountAuras"},
	} {
		h, _, hand1 := ownersFixture(t)
		before := len(h.log)
		ctx := &Ctx{Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}}
		Resolve(h, ctx, sa(t, tc.sa))
		if h.asked != nil {
			t.Fatalf("%s: a decision was posed: %+v", tc.name, h.asked)
		}
		notes := 0
		for _, ev := range h.log[before:] {
			if ev.Kind == events.Note {
				notes++
			}
		}
		if notes != 1 {
			t.Fatalf("%s: %d Notes, want exactly 1: %+v", tc.name, notes, h.log[before:])
		}
		for _, id := range hand1 {
			if o := h.g.Obj(id); o.Zone != state.ZHand {
				t.Fatalf("%s: hand card %d moved to %s", tc.name, id, o.Zone)
			}
		}
	}
}

// TestHandMoveOwnersAtRandomPicksThroughTheSeededRng is the Elkin Lair shape
// (AtRandom$ True): the ENGINE picks, not a player -- no ask, one random
// eligible card per owner (the fakeHost's Rand always answers 0, so the pick
// is the first eligible; the rules-level determinism rides the seeded rng).
func TestHandMoveOwnersAtRandomPicksThroughTheSeededRng(t *testing.T) {
	h, hand0, _ := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Creature | DefinedPlayer$ Player | AtRandom$ True | Mandatory$ True | ChangeNum$ 1"))
	if h.asked != nil {
		t.Fatalf("a decision was posed under AtRandom$: %+v", h.asked)
	}
	if o := h.g.Obj(hand0[0]); o.Zone != state.ZExile {
		t.Fatalf("the picked creature on %s, want exile", o.Zone)
	}
}

// TestHandMoveOwnersChooserTriggeredPlayer is the Widespread Panic shape
// (DefinedPlayer$ TriggeredPlayer, Chooser$ TriggeredPlayer): the causing
// event's bound player both owns the hand and answers.
func TestHandMoveOwnersChooserTriggeredPlayer(t *testing.T) {
	h, _, _ := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0,
		TriggerContext: TriggerContext{TriggerPlayer: state.Target{Player: 1, IsPlayer: true}}}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Library | LibraryPosition$ 0 | DefinedPlayer$ TriggeredPlayer | Chooser$ TriggeredPlayer | ChangeType$ Card | ChangeNum$ 1 | Mandatory$ True"))
	if h.asked == nil {
		t.Fatal("no decision was posed")
	}
	d := h.asked
	if d.Player != 1 || d.ResumeTarget != 0 {
		t.Fatalf("ask = Player %d target %d, want 1/0 (the triggered player asks and answers)", d.Player, d.ResumeTarget)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want the triggered player's whole hand", len(d.Options))
	}
}
