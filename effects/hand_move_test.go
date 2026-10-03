package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
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

// TestHandMoveChangeZoneAsksWhenMoreEligibleThanChangeNum is the ask leaf: a
// hand with STRICTLY more ChangeType$-eligible cards than ChangeNum poses a
// real KChoose to the controller -- Min ChangeNum, Max ChangeNum, the
// eligible cards only, in hand order -- and the resolution suspends with
// nothing moved until the answer arrives.
func TestHandMoveChangeZoneAsksWhenMoreEligibleThanChangeNum(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, handAskSA))
	if h.asked == nil {
		t.Fatal("no decision was posed: a hand with more eligible cards than ChangeNum must ask")
	}
	d := h.asked
	if d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("decision = %+v, want a Min==1/Max==1 KChoose for the controller", d)
	}
	if d.ResumeKind != "hand_move" {
		t.Fatalf("ResumeKind = %q, want \"hand_move\"", d.ResumeKind)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "hand_move" ||
		d.Options[0].Obj != ids[1] || d.Options[1].Obj != ids[2] {
		t.Fatalf("options = %+v, want the two Isles in hand order, Kind \"hand_move\"", d.Options)
	}
	for _, o := range d.Options {
		if o.Label != "Isle" {
			t.Fatalf("option label = %q, want the face name", o.Label)
		}
	}
	// Nothing moved while suspended.
	for i, id := range ids {
		if o := h.g.Obj(id); o.Zone != state.ZHand {
			t.Fatalf("ids[%d] moved during suspension: %s", i, o.Zone)
		}
	}
}

// TestHandMoveChangeZoneReentryMovesExactlyTheAnswer is the re-entry
// contract: Ctx.HandMove/HandMoveDone set (here the SECOND eligible card, to
// prove the answer — not a first-eligible default — is honoured) moves
// exactly that card, the fx42 scoping discipline clears both fields, and a
// stray answer naming a card no longer in hand moves nothing.
func TestHandMoveChangeZoneReentryMovesExactlyTheAnswer(t *testing.T) {
	h, ids := handAskFixture(t)
	c := &Ctx{Controller: 0, HandMove: []state.ObjID{ids[2], ids[0]}, HandMoveDone: true}
	Resolve(h, c, sa(t, handAskSA))
	if o := h.g.Obj(ids[2]); o.Zone != state.ZBattlefield {
		t.Fatalf("the answered card is on %s, want battlefield", o.Zone)
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZHand {
		t.Fatalf("the ineligible answer was honoured: bear is on %s", o.Zone)
	}
	if o := h.g.Obj(ids[1]); o.Zone != state.ZHand {
		t.Fatalf("the unchosen eligible card moved: on %s", o.Zone)
	}
	if c.HandMove != nil || c.HandMoveDone {
		t.Fatalf("Ctx.HandMove/HandMoveDone not cleared (fx42 scoping): %+v/%v", c.HandMove, c.HandMoveDone)
	}
	// A stray answer naming a card that left the hand moves nothing.
	h2, ids2 := handAskFixture(t)
	h2.g.Obj(ids2[2]).Zone = state.ZExile // pretend it left between ask and answer
	Resolve(h2, &Ctx{Controller: 0, HandMove: []state.ObjID{ids2[2]}, HandMoveDone: true}, sa(t, handAskSA))
	if o := h2.g.Obj(ids2[2]); o.Zone != state.ZExile {
		t.Fatalf("a card outside the hand was moved: on %s", o.Zone)
	}
}

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

// TestHandMoveChangeZoneExileAppliesTimeCounters pins the exile half of the
// counterDestination contract: a ChangeZone to exile parses
// WithCountersType$ TIME and puts the counters on the exiled card (the
// suspend shape Rory Williams' trigger uses). Unlike battlefield-only
// counters, exile must carry them too, so this path is exercised end to end
// through the hand-move settle path.
func TestHandMoveChangeZoneExileAppliesTimeCounters(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[1]}, HandMoveDone: true}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Land | WithCountersType$ TIME | WithCountersAmount$ 3"))
	o := h.g.Obj(ids[1])
	if o.Zone != state.ZExile {
		t.Fatalf("answered land is on %s, want exile", o.Zone)
	}
	if got := o.Counter("TIME"); got != 3 {
		t.Fatalf("exiled land has %d TIME counters, want 3", got)
	}
	var counter bool
	for _, ev := range h.log {
		if ev.Kind == events.CounterChange && ev.Obj == ids[1] && ev.Counter == "TIME" {
			counter = true
		}
		if ev.Kind == events.Note {
			t.Fatalf("literal exile counter amount emitted Note: %+v", ev)
		}
	}
	if !counter {
		t.Fatalf("no TIME CounterChange for the exiled land: %+v", h.log)
	}
}

// TestHandMoveChangeZoneExileDynamicCounterAmountIsLoud pins the dynamic half
// of the same contract: a hand-origin move to exile with a dynamic
// WithCountersAmount$ now PARSES the amount (exile is counter-bearing), so an
// unresolvable X surfaces the deterministic malformed-amount Note and falls
// back to the safe default 1 rather than being silently ignored. This is the
// contract that replaced the old battlefield-only reading.
func TestHandMoveChangeZoneExileDynamicCounterAmountIsLoud(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[1]}, HandMoveDone: true}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Land | WithCountersType$ TIME | WithCountersAmount$ X"))
	o := h.g.Obj(ids[1])
	if o.Zone != state.ZExile {
		t.Fatalf("answered land is on %s, want exile", o.Zone)
	}
	if got := o.Counter("TIME"); got != 1 {
		t.Fatalf("dynamic exile counter amount defaulted to %d, want 1", got)
	}
	var note string
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			note = ev.Text
		}
	}
	if want := "malformed WithCountersAmount X"; note != want {
		t.Fatalf("Note.Text = %q, want %q", note, want)
	}
}

// TestHandMoveChangeZoneCounterlessDestinationStaysSilent pins the other side
// of counterDestination: a destination that cannot carry counters (hand ->
// graveyard) neither parses the amount nor emits anything, so a dynamic
// amount there is never a malformed-amount Note either.
func TestHandMoveChangeZoneCounterlessDestinationStaysSilent(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[1]}, HandMoveDone: true}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Graveyard | ChangeType$ Land | WithCountersType$ TIME | WithCountersAmount$ X"))
	o := h.g.Obj(ids[1])
	if o.Zone != state.ZGraveyard {
		t.Fatalf("answered land is on %s, want graveyard", o.Zone)
	}
	if got := o.Counter("TIME"); got != 0 {
		t.Fatalf("counterless destination got %d TIME counters, want 0", got)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			t.Fatalf("counterless destination emitted Note: %+v", ev)
		}
		if ev.Kind == events.CounterChange && ev.Obj == ids[1] {
			t.Fatalf("counterless destination emitted CounterChange: %+v", ev)
		}
	}
}

// TestHandMoveChangeZoneHonoursRememberChanged pins the one settle extra
// the brief authorised: RememberChanged$ True joins the moved card to
// Ctx.Remembered in answer order. Tapped$ is deliberately NOT read on this
// path (unread on the object path too -- a recorded follow-up, not a
// handmove1 behaviour).
func TestHandMoveChangeZoneHonoursRememberChanged(t *testing.T) {
	h, ids := handAskFixture(t)
	c := &Ctx{Controller: 0, HandMove: []state.ObjID{ids[2]}, HandMoveDone: true}
	Resolve(h, c, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | RememberChanged$ True"))
	o := h.g.Obj(ids[2])
	if o.Zone != state.ZBattlefield {
		t.Fatalf("moved land zone = %s, want battlefield", o.Zone)
	}
	if o.Tapped {
		t.Fatalf("moved land is tapped: Tapped$ must stay unread on the hand path")
	}
	for i := range h.log {
		if h.log[i].Kind == events.Tap && h.log[i].Obj == ids[2] {
			t.Fatalf("a Tap event was emitted on the hand path: %+v", h.log[i])
		}
	}
	if len(c.Remembered) != 1 || c.Remembered[0].Obj != ids[2] {
		t.Fatalf("Remembered = %+v, want exactly the moved card", c.Remembered)
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

// TestHandMoveChangeZonePutsBackOnTopInAnswerOrder is the placement leaf for
// the Forge default: Destination$ Library with no LibraryPosition$ puts the
// chosen cards on TOP of the library, in the player's answer order (one
// Secret LibraryOrder; Brainstorm's Reorder$ True "in any order").
func TestHandMoveChangeZonePutsBackOnTopInAnswerOrder(t *testing.T) {
	h, ids := handAskFixture(t)
	// The answer names the SECOND eligible card first: the order the answer
	// gives, not hand order, must be the order on top.
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[2], ids[1]}, HandMoveDone: true},
		sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 2 | Mandatory$ True | Reorder$ True"))
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 2 || lib[0] != ids[2] || lib[1] != ids[1] {
		t.Fatalf("library = %v, want [%d %d] in answer order on top", lib, ids[2], ids[1])
	}
	sawOrder := false
	for _, ev := range h.log {
		if ev.Kind == events.LibraryOrder && ev.Player == 0 {
			sawOrder = true
		}
	}
	if !sawOrder {
		t.Fatalf("no LibraryOrder placement event in %+v", h.log)
	}
	for _, id := range []state.ObjID{ids[0], ids[3]} {
		if o := h.g.Obj(id); o.Zone != state.ZHand {
			t.Fatalf("unchosen card %d moved to %s", id, o.Zone)
		}
	}
}

// TestHandMoveChangeZoneLibraryPositionZeroIsTop pins the explicit top
// spelling (Jace, the Mind Sculptor's [0]: LibraryPosition$ 0).
func TestHandMoveChangeZoneLibraryPositionZeroIsTop(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[1]}, HandMoveDone: true},
		sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeType$ Card | ChangeNum$ 2 | LibraryPosition$ 0 | Mandatory$ True"))
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 1 || lib[0] != ids[1] {
		t.Fatalf("library = %v, want the chosen card on top", lib)
	}
}

// TestHandMoveChangeZoneLibraryPositionMinusOneIsBottom pins the bottom
// spelling (Sawtooth Loon, Amass the Components: LibraryPosition$ -1); the
// Move itself appends at the bottom, so the placement re-affirms the chosen
// order there and the rest of the library stays above.
func TestHandMoveChangeZoneLibraryPositionMinusOneIsBottom(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[2], ids[1]}, HandMoveDone: true},
		sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Library | LibraryPosition$ -1 | ChangeNum$ 2 | Mandatory$ True"))
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 2 || lib[0] != ids[2] || lib[1] != ids[1] {
		t.Fatalf("library = %v, want [%d %d] at the bottom in answer order", lib, ids[2], ids[1])
	}
}

// TestHandMoveChangeZoneOptionalTakeAsksWithMinZero is the explicit
// Optional$ leaf: the marker poses Forge's confirm-before-pick gate first
// (confirmAction), and the ACCEPTED confirmation's pick lowers its Min to 0
// -- an empty answer there moves nothing. Markerless scripts are classified
// from their real card text below; they are never assumed optional and never
// confirm.
func TestHandMoveChangeZoneOptionalTakeAsksWithMinZero(t *testing.T) {
	for _, saLine := range []string{
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Optional$ You",
	} {
		h, _ := handAskFixture(t)
		Resolve(h, &Ctx{Controller: 0}, sa(t, saLine))
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" || confirm.Min != 1 || confirm.Max != 1 ||
			len(confirm.Options) != 2 || confirm.Options[0].Kind != "yes" || confirm.Options[1].Kind != "no" {
			t.Fatalf("%s: first ask = %+v, want the hand_move_confirm yes/no gate", saLine, confirm)
		}
		// The accepted confirmation's pick lowers the take's Min to 0.
		Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
			HandMoveConfirmTarget: 0}, sa(t, saLine))
		if h.asked == nil || h.asked.ResumeKind != "hand_move" || h.asked.Min != 0 || h.asked.Max != 1 {
			t.Fatalf("%s: accepted-confirmation pick = %+v, want a Min 0 / Max 1 hand_move", saLine, h.asked)
		}
	}
	// The empty pick answer is legal: HandMoveDone with no ids moves nothing
	// (the confirm was consumed on the earlier pass, so the re-entry applies
	// the pick directly).
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: nil, HandMoveDone: true}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Optional$ You"))
	for _, id := range ids {
		if o := h.g.Obj(id); o.Zone != state.ZHand {
			t.Fatalf("an empty optional answer moved ids[%d] to %s", id, o.Zone)
		}
	}
	// Mandatory$ True keeps the take required: after the accepted confirmation
	// the pick's Min is ChangeNum, not 0.
	const mandatoryLine = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Optional$ You | Mandatory$ True"
	h2, _ := handAskFixture(t)
	Resolve(h2, &Ctx{Controller: 0}, sa(t, mandatoryLine))
	if h2.asked == nil || h2.asked.ResumeKind != "hand_move_confirm" {
		t.Fatalf("Mandatory$ True first ask = %+v, want the confirmation gate", h2.asked)
	}
	Resolve(h2, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
		HandMoveConfirmTarget: 0}, sa(t, mandatoryLine))
	if h2.asked == nil || h2.asked.Min != 1 {
		t.Fatalf("Mandatory$ True accepted pick = %+v, want Min 1 (the take is required)", h2.asked)
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

// TestHandMoveChangeZoneShuffleParamShufflesTheLibrary pins Shuffle$ True on
// a hand put-back (Slowtrip's "shuffle a card from your hand into your
// library"): the move happens, then one Shuffle randomises the library, and
// no LibraryPosition$ placement rides on top of it.
func TestHandMoveChangeZoneShuffleParamShufflesTheLibrary(t *testing.T) {
	h, ids := handAskFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{ids[1]}, HandMoveDone: true}, sa(t,
		"DB$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeType$ Card | Shuffle$ True | RememberChanged$ True"))
	if o := h.g.Obj(ids[1]); o.Zone != state.ZLibrary {
		t.Fatalf("moved card on %s, want library", o.Zone)
	}
	sawShuffle, sawOrder := false, false
	for _, ev := range h.log {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			sawShuffle = true
		}
		if ev.Kind == events.LibraryOrder {
			sawOrder = true
		}
	}
	if !sawShuffle || sawOrder {
		t.Fatalf("Shuffle$ True: shuffle %v, placement %v (want shuffle, no placement)", sawShuffle, sawOrder)
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

// TestOviyaRealScriptFilteredHandPutBack runs the real compiled Oviya,
// Automech Artisan ability (Cost$ G T | Origin$ Hand | Destination$
// Battlefield | ChangeType$ Creature,Vehicle | ChangeNum$ 1, neither
// marker but oracle text saying "You may put"): the ask offers ONLY the
// creature and Vehicle cards, never the land or the instant beside them, and
// Min is 0 (the take is optional).
func TestOviyaRealScriptFilteredHandPutBack(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	oviya, ok := reg.Lookup("Oviya, Automech Artisan")
	if !ok {
		t.Fatal("corpus has no Oviya, Automech Artisan")
	}
	var ab *cards.SA
	for _, a := range oviya.Faces[0].Abilities {
		if a.API == "ChangeZone" && a.Params["Origin"] == "Hand" {
			ab = a
		}
	}
	if ab == nil {
		t.Fatal("Oviya has no Origin$ Hand ChangeZone ability")
	}
	g := state.NewGame(names(2))
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	mtn := corpusObject(t, reg, g, "Mountain")
	bolt := corpusObject(t, reg, g, "Lightning Bolt")
	copter := corpusObject(t, reg, g, "Smuggler's Copter")
	ids := []state.ObjID{bear.ID, mtn.ID, bolt.ID, copter.ID}
	g.SetZone(state.ZHand, 0, ids)
	for _, id := range ids {
		g.Obj(id).Zone = state.ZHand
	}

	source := corpusObject(t, reg, g, "Oviya, Automech Artisan")
	h := &askHost{}
	h.g = g
	Resolve(h, &Ctx{Controller: 0, Source: source.ID}, ab)
	if h.asked == nil {
		t.Fatal("no decision posed: two eligible cards (bear, copter) strictly exceed ChangeNum 1")
	}
	d := h.asked
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 0/1 (Forge's optional default)", d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %+v, want exactly the bear and the Copter", d.Options)
	}
	for _, o := range d.Options {
		if o.Obj != bear.ID && o.Obj != copter.ID {
			t.Fatalf("option %+v is neither the creature nor the Vehicle", o)
		}
	}
	// Nothing moved while suspended.
	for _, id := range ids {
		if g.Obj(id).Zone != state.ZHand {
			t.Fatalf("id %d moved during suspension", id)
		}
	}
}

// TestHERBIEScoutUnitHandMoveEntersTapped runs the real compiled H.E.R.B.I.E.
// Scout Unit hand mover. Tapped$ True on a hand-origin ChangeZone is now a
// real entry state (the shared settle emits the same "entered tapped" Tap
// every other mover emits), so the land enters TAPPED and the old
// "not implemented" fallback Note is gone -- this test pins the retirement in
// both directions.
func TestHERBIEScoutUnitHandMoveEntersTapped(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("H.E.R.B.I.E. Scout Unit")
	if !ok {
		t.Fatal("corpus has no H.E.R.B.I.E. Scout Unit")
	}
	db := cards.ResolveSVar(card.Faces[0].SVars, "DBChangeZone")
	if db == nil || db.Params["Origin"] != "Hand" || db.Params["Destination"] != "Battlefield" || db.Params["Tapped"] != "True" {
		t.Fatalf("H.E.R.B.I.E. Scout Unit hand mover drifted: %+v", db)
	}
	g := state.NewGame(names(2))
	source := corpusObject(t, reg, g, "H.E.R.B.I.E. Scout Unit")
	land := corpusObject(t, reg, g, "Forest")
	g.SetZone(state.ZHand, 0, []state.ObjID{land.ID})
	land.Zone = state.ZHand
	h := &askHost{}
	h.g = g
	Resolve(h, &Ctx{Source: source.ID, Controller: 0,
		HandMove: []state.ObjID{land.ID}, HandMoveDone: true}, db)
	if land.Zone != state.ZBattlefield || !land.Tapped {
		t.Fatalf("land = zone %s tapped %v, want it on the battlefield TAPPED", land.Zone, land.Tapped)
	}
	taps := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Tapped$ True on a hand ChangeZone is not implemented") {
			t.Fatalf("the retired hand Tapped$ fallback Note is back: %+v", ev)
		}
		if ev.Kind == events.Tap && ev.Obj == land.ID {
			taps++
		}
	}
	if taps != 1 {
		t.Fatalf("hand move logged %d entry Taps for the land, want exactly 1: %+v", taps, h.log)
	}
	// The land carries no Attacking$ rider, so nothing marks it attacking.
	if land.IsAttacking {
		t.Fatalf("land entered attacking without an Attacking$ rider: %+v", land)
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

// Kastral's optional Bird picker must offer both hand and graveyard cards in
// one search, without falling back to the old mixed-origin Note.
func TestKastralMixedHandOriginOffersBothZones(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kastral, ok := reg.Lookup("Kastral, the Windcrested")
	if !ok {
		t.Fatal("corpus has no Kastral, the Windcrested")
	}
	db := cards.ResolveSVar(kastral.Faces[0].SVars, "DBChangeZone")
	if db == nil || db.Params["Origin"] != "Hand,Graveyard" ||
		db.Params["ChangeType"] != "Creature.Bird+YouOwn" {
		t.Fatalf("Kastral's compiled mixed-origin picker drifted: %+v", db)
	}

	g := state.NewGame(names(2))
	source := corpusObject(t, reg, g, "Kastral, the Windcrested")
	handBird := corpusObject(t, reg, g, "Storm Crow")
	graveBird := corpusObject(t, reg, g, "Storm Crow")
	g.SetZone(state.ZHand, 0, []state.ObjID{handBird.ID})
	g.SetZone(state.ZGraveyard, 0, []state.ObjID{graveBird.ID})
	handBird.Zone, graveBird.Zone = state.ZHand, state.ZGraveyard

	h := &askHost{}
	h.g = g
	if handBird.Zone == graveBird.Zone || handBird.ID == graveBird.ID {
		t.Fatal("hand and graveyard candidates must differ")
	}
	Resolve(h, &Ctx{Source: source.ID, Controller: 0}, db)
	// Kastral's ChangeZone carries Optional$ You, so Forge's confirmAction
	// gate runs before the fetch list: the confirmation is the first ask, and
	// the mixed-origin search is the second.
	if h.asked == nil || h.asked.ResumeKind != "search_confirm" {
		t.Fatalf("mixed-origin search confirmation = %+v, want the Optional$ You gate", h.asked)
	}
	if len(h.asked.Options) != 2 || h.asked.Options[0].Kind != "yes" || h.asked.Options[1].Kind != "no" {
		t.Fatalf("mixed-origin confirmation is not a yes/no gate: %+v", h.asked.Options)
	}
	h.asked = nil
	Resolve(h, &Ctx{Source: source.ID, Controller: 0, SearchConfirmDone: true, SearchConfirm: "yes",
		SearchConfirmTarget: 0}, db)
	if h.asked == nil || h.asked.ResumeKind != "search" {
		t.Fatalf("mixed-origin search = %+v", h.asked)
	}
	seen := map[state.ObjID]bool{}
	for _, opt := range h.asked.Options {
		seen[opt.Obj] = true
	}
	if !seen[handBird.ID] || !seen[graveBird.ID] {
		t.Fatalf("mixed-origin options = %+v, want both Birds", h.asked.Options)
	}
	if handBird.Zone != state.ZHand || graveBird.Zone != state.ZGraveyard {
		t.Fatalf("search moved cards before an answer: hand=%s grave=%s", handBird.Zone, graveBird.Zone)
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

// TestHandMoveOwnersChainsOneAskPerPlayer is the finding's core leaf, on the
// Kynaios and Tiro shape (DefinedPlayer$ Player, ChangeNum$ 1, ChangeType$
// Land): the explicit Optional$ marker poses the confirmation gate FIRST
// (Forge's confirmAction), for owner 0; the accepted confirmation's card pick
// follows, and after that answer is consumed the walk continues -- owner 1's
// OWN confirmation and pick chain behind it, bound by ResumeTarget. Neither
// ask ever names the other owner's cards.
func TestHandMoveOwnersChainsOneAskPerPlayer(t *testing.T) {
	const kynaios = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | DefinedPlayer$ Player | ChangeNum$ 1 | RememberChanged$ True | Optional$ True"
	h, hand0, hand1 := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, kynaios))
	d := h.asked
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 0 || d.ResumeTarget != 0 ||
		len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("first ask = %+v, want owner 0's hand_move_confirm yes/no gate", d)
	}
	// Accept: the owner 0 pick follows, Min 0 / Max 1 over its own two Isles.
	Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
		HandMoveConfirmTarget: 0}, sa(t, kynaios))
	d = h.asked
	if d == nil || d.ResumeKind != "hand_move" || d.Player != 0 || d.Min != 0 || d.Max != 1 || d.ResumeTarget != 0 {
		t.Fatalf("owner 0 pick = %+v, want Player 0 Min/Max 0/1 target 0 (owner 0 answers its own optional take)", d)
	}
	for _, o := range d.Options {
		if o.Player != 0 || o.Obj != hand0[1] && o.Obj != hand0[2] {
			t.Fatalf("owner 0 pick option %+v is not one of owner 0's Isles", o)
		}
	}
	if len(d.Options) != 2 {
		t.Fatalf("owner 0 pick options = %d, want owner 0's two Isles", len(d.Options))
	}
	// Re-entry (the engine's contract): owner 0's pick answer, cursor 0.
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{hand0[2]}, HandMoveDone: true,
		HandMoveTarget: 0, Remembered: []state.Target{{Obj: hand0[2]}}}, sa(t, kynaios))
	d2 := h.asked
	if d2 == nil || d2.ResumeKind != "hand_move_confirm" || d2.Player != 1 || d2.ResumeTarget != 1 {
		t.Fatalf("second ask = %+v, want owner 1's hand_move_confirm (Player 1 target 1)", d2)
	}
	// Accept owner 1's confirmation: its own pick follows, over its two Isles.
	Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
		HandMoveConfirmTarget: 1, Remembered: []state.Target{{Obj: hand0[2]}}}, sa(t, kynaios))
	d3 := h.asked
	if d3 == nil || d3.ResumeKind != "hand_move" || d3.Player != 1 || d3.ResumeTarget != 1 {
		t.Fatalf("owner 1 pick = %+v, want Player 1 target 1 (owner 1 asks its own hand)", d3)
	}
	if len(d3.Options) != 2 || d3.Options[0].Obj != hand1[0] || d3.Options[1].Obj != hand1[2] {
		t.Fatalf("owner 1 pick options = %+v, want owner 1's two Isles in hand order", d3.Options)
	}
	if o := h.g.Obj(hand0[2]); o.Zone != state.ZBattlefield {
		t.Fatalf("owner 0's answered land on %s, want battlefield", o.Zone)
	}
	// Owner 1's second answer, cursor 1: nothing else asks.
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{hand1[2]}, HandMoveDone: true,
		HandMoveTarget: 1, Remembered: []state.Target{{Obj: hand0[2]}}}, sa(t, kynaios))
	if h.asked != nil {
		t.Fatalf("a third decision was posed: %+v", h.asked)
	}
	if o := h.g.Obj(hand1[2]); o.Zone != state.ZBattlefield {
		t.Fatalf("owner 1's answered land on %s, want battlefield", o.Zone)
	}
	if o := h.g.Obj(hand1[1]); o.Zone != state.ZHand {
		t.Fatalf("owner 1's bear moved: on %s", o.Zone)
	}
}

// TestHandMoveOwnersOptionalSingleEligibleCanDecline pins the Kynaios-shaped
// owner-selected optional move where an owner has exactly one eligible land.
// The confirmation gate runs BEFORE the pick: declining owner 0's
// confirmation poses no pick at all, and owner 1 -- whose pool is empty --
// still receives its OWN confirmation, because Forge's gate runs before the
// fetch list is consulted. Accepting owner 1's confirmation reaches the
// entered-fetch empty continuation: no pick, nothing moved.
func TestHandMoveOwnersOptionalSingleEligibleCanDecline(t *testing.T) {
	const kynaios = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | DefinedPlayer$ Player | ChangeNum$ 1 | Optional$ True"
	h, hand0, hand1 := ownersFixture(t)
	h.g.SetZone(state.ZHand, 0, []state.ObjID{hand0[1]}) // exactly one Isle
	h.g.SetZone(state.ZHand, 1, []state.ObjID{hand1[1]}) // no eligible land

	Resolve(h, &Ctx{Controller: 0}, sa(t, kynaios))
	d := h.asked
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 0 || d.ResumeTarget != 0 {
		t.Fatalf("first ask = %+v, want owner 0's confirmation gate", d)
	}

	// Decline owner 0's confirmation: no pick is posed, and owner 1's own
	// confirmation follows despite its empty pool.
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "no",
		HandMoveConfirmTarget: 0}, sa(t, kynaios))
	d = h.asked
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 1 || d.ResumeTarget != 1 {
		t.Fatalf("owner 1 confirmation = %+v, want Player 1 target 1 despite the empty pool", d)
	}
	// Accept owner 1's (empty-pool) confirmation: the entered fetch completes
	// without a pick ask, and nothing moved anywhere.
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, HandMoveConfirmDone: true, HandMoveConfirm: "yes",
		HandMoveConfirmTarget: 1}, sa(t, kynaios))
	if h.asked != nil {
		t.Fatalf("an accepted empty-pool fetch posed a pick: %+v", h.asked)
	}
	if o := h.g.Obj(hand0[1]); o.Zone != state.ZHand {
		t.Fatalf("declined owner land is on %s, want hand", o.Zone)
	}
}

// TestHandMoveOwnersSkipAnsweredOwnersOnReentry pins the cursor's skip
// contract: on re-entry for owner 1, owner 0 (already answered and moved on
// the earlier pass) must not move a second time.
func TestHandMoveOwnersSkipAnsweredOwnersOnReentry(t *testing.T) {
	const kynaios = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | DefinedPlayer$ Player | ChangeNum$ 1 | Optional$ True"
	h, hand0, _ := ownersFixture(t)
	Resolve(h, &Ctx{Controller: 0, HandMove: []state.ObjID{hand0[1]}, HandMoveDone: true,
		HandMoveTarget: 0}, sa(t, kynaios))
	d := h.asked
	// Owner 1 is unconfirmed, so its confirmation gate -- not its pick -- is
	// the next ask.
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.ResumeTarget != 1 {
		t.Fatalf("re-entry did not continue to owner 1's confirmation: %+v", d)
	}
	if o := h.g.Obj(hand0[1]); o.Zone != state.ZBattlefield {
		t.Fatalf("owner 0's land on %s, want battlefield (moved exactly once)", o.Zone)
	}
	if o := h.g.Obj(hand0[2]); o.Zone != state.ZHand {
		t.Fatalf("owner 0's second land moved on re-entry: on %s", o.Zone)
	}
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
