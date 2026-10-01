package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// curveTProduction builds a one-unit production of the given colour slot
// (state.MW..MC).
func curveTProduction(c int) cards.ManaProduction {
	var p cards.ManaProduction
	p.Colour[c] = 1
	return p
}

// curveTHand is a hand/battlefield card fixture.
func curveTHand(id state.ObjID, types, manaCost string, kw ...string) view.CardView {
	return view.CardView{ID: id, Types: types, ManaCost: manaCost, Keywords: kw}
}

// curveTSource is a battlefield mana source.
func curveTSource(id state.ObjID, c int, tapped bool) view.CardView {
	p := curveTProduction(c)
	return view.CardView{ID: id, Types: "Land", Tapped: tapped, Produces: &p}
}

// curveTView builds the deciding seat's view: own hand and battlefield, the
// given phase and stack depth, and them on turn.
func curveTView(me state.PlayerID, hand, bf []view.CardView, phase string, stack int) view.View {
	v := view.View{Viewer: me, Active: me, Phase: phase}
	for i := 0; i <= int(me); i++ {
		p := view.PlayerView{ID: state.PlayerID(i)}
		if state.PlayerID(i) == me {
			p.Hand = hand
			p.Battlefield = bf
		}
		v.Players = append(v.Players, p)
	}
	v.Stack = make([]view.StackView, stack)
	return v
}

func curveTDecision(me state.PlayerID, opts ...decision.Option) decision.Decision {
	return decision.Decision{Kind: decision.KPriority, Seq: 9, Player: me, Options: opts}
}

func curveTCast(i int, id state.ObjID) decision.Option {
	return decision.Option{Index: i, Kind: "cast", Obj: id}
}

func curveTLand(i int, id state.ObjID) decision.Option {
	return decision.Option{Index: i, Kind: "play_land", Obj: id}
}

func curveTPass() decision.Option { return decision.Option{Index: 0, Kind: "pass"} }

// TestCurveTakesTheLandDrop: on our own empty-stack main phase the offered
// land play is taken before any cast, and the chosen land is the one that
// unlocks the most castable hand spells.
func TestCurveTakesTheLandDrop(t *testing.T) {
	// Hand: an Island, a Mountain, and a {U} creature. The only
	// battlefield source is an untapped Mountain, so the {U} spell is
	// not yet castable and the Island is the land that unlocks it.
	island := curveTHand(20, "Land", "")
	island.Produces = ptrProduction(curveTProduction(state.MU))
	mountain := curveTHand(21, "Land", "")
	mountain.Produces = ptrProduction(curveTProduction(state.MR))
	uu := curveTHand(22, "Creature", "U")
	bf := []view.CardView{curveTSource(30, state.MR, false)}

	v := curveTView(0, []view.CardView{island, mountain, uu}, bf, "main1", 0)
	// Precondition: the {U} spell is not payable yet (no blue source)
	// and the Island is the source that would make it so.
	pre := curveFactsFromView(0, v)
	if pre.countCastable(curveAvailabilityOf(pre.untapped)) != 0 {
		t.Fatal("precondition: a hand spell is already castable with no blue source")
	}
	withIsland := curveAvailabilityOf(pre.untapped)
	withIsland.add(*island.Produces)
	if !withIsland.pay(curveParseDemand(uu.ManaCost)) {
		t.Fatal("precondition: the Island does not make {U} payable")
	}

	// Options: pass, a payable cast, and the two lands. The land drop must
	// win, and it must be the Island (index 2), not the Mountain (index 3).
	d := curveTDecision(0, curveTPass(), curveTCast(1, 22), curveTLand(2, 20), curveTLand(3, 21))
	inner := &stubSeat{answer: passIntent}
	wrapped := newCurve(inner, 5)
	got, err := wrapped.Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 2 {
		t.Fatalf("choices = %v, want the Island land play [2]", got.Choices)
	}
}

// TestCurveCastsTopOfTheKnapsackAndLeavesInstantsAlone: among sorcery-speed
// spells the decorator casts the highest-mana-value member of the payable
// set, and an instant -- even one that is payable and would top the set if
// it were eligible -- is never cast by the decorator.
func TestCurveCastsTopOfTheKnapsackAndLeavesInstantsAlone(t *testing.T) {
	instant := curveTHand(10, "Instant", "3")
	big := curveTHand(11, "Creature", "2")
	small := curveTHand(12, "Sorcery", "1")
	// Three untapped Plains: every single card is payable, and so are the
	// two sorceries together. If the instant were eligible, the best set
	// would be {instant} alone (3 mana, tie with {2,1} but a higher single
	// value) and the cast would be option 1.
	bf := []view.CardView{
		curveTSource(30, state.MW, false),
		curveTSource(31, state.MW, false),
		curveTSource(32, state.MW, false),
	}
	v := curveTView(0, []view.CardView{instant, big, small}, bf, "main1", 0)
	d := curveTDecision(0, curveTPass(),
		curveTCast(1, 10), curveTCast(2, 11), curveTCast(3, 12))
	inner := &stubSeat{answer: passIntent}
	got, err := newCurve(inner, 5).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: the instant really is offered and really is payable.
	pre := curveFactsFromView(0, v)
	if avail := curveAvailabilityOf(pre.untapped); !avail.pay(curveParseDemand(instant.ManaCost)) {
		t.Fatal("precondition: the instant is not payable, so exclusion is untested")
	}
	if len(got.Choices) != 1 || got.Choices[0] != 2 {
		t.Fatalf("choices = %v, want the mana-value-2 sorcery [2] (instant must be left alone)", got.Choices)
	}
}

// TestCurveDelegatesEverythingUnchanged: every decision the idea does not
// own -- and its own cases with no play -- returns the inner seat's answer
// unchanged after exactly one consultation.
func TestCurveDelegatesEverythingUnchanged(t *testing.T) {
	// A board with one castable instant and one source: the idea's own
	// decision shape, but it has no sorcery-speed play and no land.
	instant := curveTHand(10, "Instant", "1")
	bf := []view.CardView{curveTSource(30, state.MW, false)}
	main := curveTView(0, []view.CardView{instant}, bf, "main1", 0)
	ownMain := curveTDecision(0, curveTPass(), curveTCast(1, 10))

	oppTurn := curveTView(0, []view.CardView{instant}, bf, "main1", 0)
	oppTurn.Active = 1
	oppTurn.Players = append(oppTurn.Players, view.PlayerView{ID: 1})

	cases := []struct {
		name string
		v    view.View
		d    decision.Decision
	}{
		{"non-priority decision", main, decision.Decision{Kind: decision.KTarget, Seq: 9, Player: 0}},
		{"non-main phase", curveTView(0, []view.CardView{instant}, bf, "combat", 0), ownMain},
		{"non-empty stack", curveTView(0, []view.CardView{instant}, bf, "main1", 1), ownMain},
		{"opponent's turn", oppTurn, ownMain},
		{"no land and only an instant", main, ownMain},
		{"no options at all", main, curveTDecision(0)},
	}
	for _, tc := range cases {
		inner := &stubSeat{answer: passIntent}
		got, err := newCurve(inner, 5).Decide(context.Background(), tc.v, tc.d)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if inner.decided != 1 {
			t.Fatalf("%s: inner consulted %d times, want 1", tc.name, inner.decided)
		}
		if len(got.Choices) != 1 || got.Choices[0] != 0 {
			t.Fatalf("%s: choices = %v, want the inner's pass [0] unchanged", tc.name, got.Choices)
		}
	}
}

// TestCurveDelegatesOnInnerError: a failing inner is never answered for.
func TestCurveDelegatesOnInnerError(t *testing.T) {
	inner := &stubSeat{err: errCurveRefused}
	v := curveTView(0, nil, nil, "main1", 0)
	d := curveTDecision(0, curveTPass())
	_, err := newCurve(inner, 5).Decide(context.Background(), v, d)
	if err != errCurveRefused {
		t.Fatalf("err = %v, want the inner's refusal", err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
}

// TestCurveBoardSurfaceTakesTheLandDrop: the board path answers the same
// claimed case through DecideBoard, and the wrapper keeps the inner's
// BoardSeat-ness (passguard's contract).
func TestCurveBoardSurfaceTakesTheLandDrop(t *testing.T) {
	land := botpolicy.Card{OnBattlefield: false, Produces: curveTProduction(state.MU)}
	spell := botpolicy.Card{OnBattlefield: false, Castable: true, ManaCost: "1", CMC: 1}
	b := botpolicy.Board{
		IsMain: true, MyTurn: true,
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{20: land, 22: spell}),
	}
	d := curveTDecision(0, curveTPass(), curveTLand(1, 20), curveTCast(2, 22))
	inner := &stubSeat{answer: passIntent}
	wrapped := newCurve(inner, 5)
	if _, ok := wrapped.(interface {
		DecideBoard(context.Context, botpolicy.Board, decision.Decision) (decision.Intent, error)
	}); !ok {
		t.Fatal("wrapper over a BoardSeat inner is not a seat.BoardSeat")
	}
	got, err := wrapped.(interface {
		DecideBoard(context.Context, botpolicy.Board, decision.Decision) (decision.Intent, error)
	}).DecideBoard(context.Background(), b, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times via DecideBoard, want 1", inner.boarded)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the land play [1]", got.Choices)
	}
}

// TestCurveKeepsThePlainSurface: a non-BoardSeat inner stays a plain seat.
func TestCurveKeepsThePlainSurface(t *testing.T) {
	plain := &stubPlain{answer: passIntent}
	wrapped := newCurve(plain, 5)
	if _, ok := wrapped.(interface {
		DecideBoard(context.Context, botpolicy.Board, decision.Decision) (decision.Intent, error)
	}); ok {
		t.Fatal("wrapper over a non-BoardSeat inner implements DecideBoard")
	}
	v := curveTView(0, nil, nil, "main1", 0)
	if _, err := wrapped.Decide(context.Background(), v, curveTDecision(0, curveTPass())); err != nil {
		t.Fatal(err)
	}
	if plain.decided != 1 {
		t.Fatalf("plain inner consulted %d times, want 1", plain.decided)
	}
}

// TestCurveResolvesThroughTheRegistry: the spec `sb-first+curve` builds a
// decorated seat.
func TestCurveResolvesThroughTheRegistry(t *testing.T) {
	if !Has("sb-first+curve") {
		t.Fatal("registry does not know the curve decorator")
	}
	wrapped, err := Build("sb-first+curve", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wrapped.(Unwrapper); !ok {
		t.Fatal("sb-first+curve does not expose the wrapped seat")
	}
}

// errCurveRefused is a sentinel inner error.
var errCurveRefused = errors.New("refused")

// ptrProduction returns a pointer to a copy of p (view.CardView.Produces is
// a *cards.ManaProduction, a fresh value per card).
func ptrProduction(p cards.ManaProduction) *cards.ManaProduction { return &p }
