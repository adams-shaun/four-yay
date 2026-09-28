package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The pile-B order (Intent.Rest) tests: an arrange answer may carry a second
// ordered index list naming the complement of its chosen set, in the order
// the player wants pile B (CR 701.17's "in any order" for a scry's bottom
// pile, and the same freedom for a surveil's graveyard pile). Absent Rest,
// the legacy contract stands: the complement in OFFERED order.

// surveil3Src is a sorcery with a plain Surveil 3 -- three cards in the
// window so a two-card graveyard pile has a non-trivial order to choose.
const surveil3Src = "Name:Surveil3Me\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Surveil | Defined$ You | Amount$ 3\nOracle:x\n"

// surveil3Fixture builds an engine whose seat 0 can cast surveil3Src.
func surveil3Fixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, surveil3Src)
	addMana(t, e, 0, "U")
	return e, cfg, id
}

// submitRest answers the pending decision with the given choices and rest.
func submitRest(t *testing.T, e *Engine, choices, rest []int) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices, Rest: rest}
	if err := e.Submit(in); err != nil {
		t.Fatalf("submit choices %v rest %v: %v", choices, rest, err)
	}
}

// TestScryRestOrdersTheBottomPile is the pile-B order leaf for Scry: the
// answer keeps option 0 on top and names the bottom pile's order as
// option 2 then option 1, so the library's bottom tail must be exactly
// [option2, option1] -- NOT the offered order [option1, option2].
func TestScryRestOrdersTheBottomPile(t *testing.T) {
	t.Parallel()
	e, cfg, id := scryFixture(t, 210)
	d := scryDecision(t, e, id)
	if !d.Restable {
		t.Fatal("scry ask does not advertise Restable: a rules-ignorant client cannot learn the pile-B order exists")
	}
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	if top[1] == top[2] {
		t.Fatal("precondition failed: options 1 and 2 are the same object; the two pile-B orders are indistinguishable")
	}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	remainder := libBefore[3:]
	if len(remainder) < 1 {
		t.Fatalf("remainder after the top 3 is %d card(s), need >= 1 for the bottom tail to be distinguishable", len(remainder))
	}

	submitRest(t, e, []int{0}, []int{2, 1})

	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := make([]state.ObjID, 0, len(libBefore))
	want = append(want, top[0])
	want = append(want, remainder...)
	want = append(want, top[2], top[1])
	if !sameObjIDs(want, libAfter) {
		t.Fatalf("library after choices [0] rest [2,1] = %v, want %v (the bottom pile must follow the answer's order, not the offered order)", libAfter, want)
	}
	if n := countLibraryOrder(e); n != 1 {
		t.Fatalf("LibraryOrder events = %d, want exactly 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestScryWithoutRestKeepsOfferedOrder is the backward-compatibility leaf: a
// scry ask now advertises Restable, but an answer in the legacy shape (no
// Rest) must behave exactly as before -- the unchosen pile in OFFERED order.
func TestScryWithoutRestKeepsOfferedOrder(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 211)
	d := scryDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)

	submitRest(t, e, []int{2, 0}, nil)

	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := append([]state.ObjID(nil), top[2], top[0])
	want = append(want, libBefore[3:]...)
	want = append(want, top[1])
	if !sameObjIDs(want, libAfter) {
		t.Fatalf("library after a rest-less [2,0] = %v, want %v (the legacy offered-order contract must survive the new flag)", libAfter, want)
	}
}

// TestSurveilRestOrdersTheGraveyardPile is the pile-B order leaf for Surveil:
// the answer keeps option 0 on top and names the graveyard pile's order as
// option 2 then option 1, so the graveyard's tail must be exactly
// [option2, option1] -- NOT the offered order -- while the library keeps
// option 0 on top of the untouched remainder.
func TestSurveilRestOrdersTheGraveyardPile(t *testing.T) {
	t.Parallel()
	e, cfg, id := surveil3Fixture(t, 212)
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected a pending KArrange decision, got %+v", d)
	}
	if !d.Restable {
		t.Fatal("surveil ask does not advertise Restable: a rules-ignorant client cannot learn the pile-B order exists")
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want 3", len(d.Options))
	}
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	if top[1] == top[2] {
		t.Fatal("precondition failed: options 1 and 2 are the same object; the two graveyard orders are indistinguishable")
	}
	gyBefore := len(e.G.Zone(state.ZGraveyard, 0))
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)

	submitRest(t, e, []int{0}, []int{2, 1})

	libAfter := e.G.Zone(state.ZLibrary, 0)
	wantLib := append([]state.ObjID(nil), top[0])
	wantLib = append(wantLib, libBefore[3:]...)
	if !sameObjIDs(wantLib, libAfter) {
		t.Fatalf("library after choices [0] rest [2,1] = %v, want %v (option 0 on top, remainder untouched)", libAfter, wantLib)
	}
	gy := e.G.Zone(state.ZGraveyard, 0)
	// The resolving spell itself enters the graveyard after the surveil, so
	// assert on the moved cards' positions, not the zone's total growth: both
	// must have been appended after the pre-existing cards, adjacent, in the
	// answer's order.
	at := func(id state.ObjID) int {
		for i, c := range gy {
			if c == id {
				return i
			}
		}
		return -1
	}
	i1, i2 := at(top[1]), at(top[2])
	if i1 < gyBefore || i2 < gyBefore {
		t.Fatalf("graveyard positions of the moved cards: option1@%d option2@%d with %d pre-existing cards; both must be appended by the surveil", i1, i2, gyBefore)
	}
	if i2 != i1-1 {
		t.Fatalf("graveyard order: option1@%d option2@%d, want option2 immediately before option1 (the answer's order, not the offered order)", i1, i2)
	}
	// The event contract is unchanged in shape: one LibraryOrder, then one
	// MoveZone per pile-B card -- but the MoveZones now follow the answer's
	// order.
	sawOrder, sawMoves := false, 0
	var movedIDs []state.ObjID
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.LibraryOrder:
			sawOrder = true
			if sawMoves > 0 {
				t.Fatal("LibraryOrder emitted after the MoveZones: the reorder must come first (the replay contract)")
			}
		case events.MoveZone:
			// Only the surveil's own library→graveyard moves; the resolving
			// spell's own stack→graveyard move shares the Kind.
			if ev.From != state.ZLibrary || ev.To != state.ZGraveyard {
				continue
			}
			if sawMoves == 0 && !sawOrder {
				t.Fatal("MoveZone emitted before the LibraryOrder: the reorder must come first (the replay contract)")
			}
			sawMoves++
			movedIDs = append(movedIDs, ev.Obj)
		}
	}
	if !sawOrder || sawMoves != 2 {
		t.Fatalf("event shape: LibraryOrder seen=%v MoveZones=%d, want true/2", sawOrder, sawMoves)
	}
	wantMoves := []state.ObjID{top[2], top[1]}
	if !sameObjIDs(wantMoves, movedIDs) {
		t.Fatalf("MoveZone order = %v, want %v (the moves must follow the answer's graveyard order)", movedIDs, wantMoves)
	}
	replayCheck(t, e, cfg)
}

// TestSurveilRestFullPermutation is the whole-window leaf: an answer that
// keeps NOTHING on top (Choices empty) but orders the entire graveyard pile
// through Rest puts all three cards into the graveyard in that order.
func TestSurveilRestFullPermutation(t *testing.T) {
	t.Parallel()
	e, _, id := surveil3Fixture(t, 213)
	d := castFixture(t, e, id, -1)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	gyBefore := len(e.G.Zone(state.ZGraveyard, 0))

	submitRest(t, e, nil, []int{1, 2, 0})

	gy := e.G.Zone(state.ZGraveyard, 0)
	// Position-based (the resolving spell joins the graveyard after the pile):
	// the three moved cards must appear adjacent, in the answer's order.
	pos := map[state.ObjID]int{}
	for i, c := range gy {
		pos[c] = i
	}
	seq := []state.ObjID{top[1], top[2], top[0]}
	for j, id := range seq {
		if pos[id] < gyBefore {
			t.Fatalf("graveyard position of option %d = %d with %d pre-existing cards; the surveil must have appended it", j, pos[id], gyBefore)
		}
		if j > 0 && pos[id] != pos[seq[j-1]]+1 {
			t.Fatalf("graveyard order around option %d: positions %v, want the answer's order %v (adjacent, in sequence)", j, pos, seq)
		}
	}
}

// TestRestRejectedKeepsDecisionPending is the validation leaf, engine-side:
// a Rest that is not the exact ordered complement of Choices is rejected at
// Submit, and the pending scry decision SURVIVES for a legal answer (the
// no-livelock shape: a rejected intent must not consume the ask).
func TestRestRejectedKeepsDecisionPending(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 214)
	d := scryDecision(t, e, id)
	cases := []struct {
		name    string
		choices []int
		rest    []int
	}{
		{"short rest", []int{0}, []int{2}},
		{"overlapping rest", []int{0}, []int{0, 1}},
		{"out-of-range rest", []int{0}, []int{1, 5}},
		{"duplicate rest", []int{0}, []int{1, 1}},
		{"rest on a full answer", []int{0, 1, 2}, []int{0}},
	}
	for _, tc := range cases {
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tc.choices, Rest: tc.rest}
		if err := e.Submit(in); err == nil {
			t.Fatalf("%s: Submit accepted choices %v rest %v; the partition rule must reject it", tc.name, tc.choices, tc.rest)
		}
		if p := e.Pending(); p == nil {
			t.Fatalf("%s: pending decision was consumed by a rejected intent -- a rejected answer must leave the ask alive", tc.name)
		} else if p.Seq != d.Seq {
			t.Fatalf("%s: pending decision seq = %d, want %d -- a rejected answer must leave the SAME ask alive", tc.name, p.Seq, d.Seq)
		}
	}
	// The ask itself is still answerable the ordinary way after the
	// rejections: no wedged state.
	submitRest(t, e, []int{0}, []int{2, 1})
	libAfter := e.G.Zone(state.ZLibrary, 0)
	if libAfter[0] != d.Options[0].Obj {
		t.Fatalf("library top after the recovery answer = %v, want option 0 still on top", libAfter[0])
	}
}
