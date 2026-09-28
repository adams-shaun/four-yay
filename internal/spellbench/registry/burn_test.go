// burn_test.go pins the burn decorator's contract on hand-built boards: the
// documented race-aware replacement is taken on a matching single-pick
// DealDamage target ask, and every other decision -- every other kind, every
// unmodelled ask, an opponent's effect, an error from the inner -- is
// delegated unchanged, with the inner consulted exactly once per decision.

package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

var errRefused = errors.New("refused")

func intp(n int) *int { return &n }

// burnCV builds one battlefield CardView.
func burnCV(id state.ObjID, types string, power, tough, dmg int32, ctl state.PlayerID, tapped bool, kws ...string) view.CardView {
	return view.CardView{ID: id, Name: "C", Types: types, Power: power,
		Toughness: tough, Damage: dmg, Tapped: tapped, Controller: ctl, Owner: ctl, Keywords: kws}
}

// burnPlayer builds a PlayerView with a life total and a battlefield.
func burnPlayer(id state.PlayerID, life int32, bf ...view.CardView) view.PlayerView {
	return view.PlayerView{ID: id, Name: "p", Life: life, Battlefield: bf}
}

// burnView builds a two-seat view; seat 0 is the deciding seat (me), seat 1
// the opponent.
func burnView(myLife, oppLife int32, myBF, oppBF []view.CardView) view.View {
	return view.View{
		Viewer: 0,
		Players: []view.PlayerView{
			burnPlayer(0, myLife, myBF...),
			burnPlayer(1, oppLife, oppBF...),
		},
	}
}

// burnD builds the modelled ask: a single-pick KTarget decision whose payload
// names a DealDamage of the given amount. amount nil builds the unmodelled
// shape (no published damage).
func burnD(amount *int, opts ...decision.Option) decision.Decision {
	d := decision.Decision{Kind: decision.KTarget, Seq: 7, Player: 0, Min: 1, Max: 1,
		Prompt: "Choose a target", Source: 99}
	if amount != nil {
		a := *amount
		d.TargetEffect = &decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{Amount: &a}}
	}
	d.Options = opts
	return d
}

func burnFaceOpt(idx int, q state.PlayerID) decision.Option {
	return decision.Option{Index: idx, Kind: "player", Label: "player", Player: q}
}

func burnCreOpt(idx int, obj state.ObjID) decision.Option {
	return decision.Option{Index: idx, Kind: "permanent", Label: "creature", Obj: obj}
}

// burnDecide runs one decision through the decorated seat and asserts the
// answer is legal and the inner was consulted exactly once.
func burnDecide(t *testing.T, inner *stubSeat, v view.View, d decision.Decision) (decision.Intent, int) {
	t.Helper()
	got, err := newBurn(inner, 5).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if err := d.Validate(got); err != nil {
		t.Fatalf("decorated answer does not validate: %v (answer %v)", err, got.Choices)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("choices = %v, want one", got.Choices)
	}
	return got, got.Choices[0]
}

// burnViewBoard lifts the test view's facts and asserts the fixture's own
// preconditions, so a mis-built board fails loudly instead of making a pick
// for the wrong reason.
func burnViewBoard(t *testing.T, v view.View, d decision.Decision) burnFacts {
	t.Helper()
	f, ours := burnFactsFromView(v, d)
	if !ours {
		t.Fatal("fixture: view says the effect is not ours")
	}
	if len(f.life) != 2 {
		t.Fatalf("fixture: %d life totals, want 2", len(f.life))
	}
	return f
}

func TestBurnTakesLethalFace(t *testing.T) {
	// Opp at 3, bolt for 3, an evasive attacker already connects 3 a turn:
	// the face is lethal, and lethal outranks the killable 2/2.
	v := burnView(20, 3,
		[]view.CardView{burnCV(10, "Creature", 3, 3, 0, 0, false, "Flying")},
		[]view.CardView{burnCV(20, "Creature", 2, 2, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	f := burnViewBoard(t, v, d)
	if conn := f.connect(0, 1, 0); conn != 3 {
		t.Fatalf("fixture: our connectable power = %d, want 3", conn)
	}
	if got := f.cre[20]; got.toughness-2 > 3 {
		t.Fatal("fixture: the 2/2 is not killable by 3")
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{1}}}
	_, pick := burnDecide(t, inner, v, d)
	if pick != 0 {
		t.Fatalf("pick = option %d, want the face (option 0)", pick)
	}
}

func TestBurnKillsClockMovingCreature(t *testing.T) {
	// One ground attacker of ours, one untapped blocker of theirs: nothing
	// connects (both clocks infinite). Killing the blocker with the bolt
	// opens our clock from never to 7 turns, so the creature is taken.
	v := burnView(20, 20,
		[]view.CardView{burnCV(10, "Creature", 3, 3, 0, 0, false)},
		[]view.CardView{burnCV(20, "Creature", 3, 3, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	f := burnViewBoard(t, v, d)
	if conn := f.connect(0, 1, 0); conn != 0 {
		t.Fatalf("fixture: our connectable power = %d, want 0 (blocked)", conn)
	}
	if after := f.connect(0, 1, 20); after != 3 {
		t.Fatalf("fixture: connectable power after the kill = %d, want 3", after)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
	_, pick := burnDecide(t, inner, v, d)
	if pick != 1 {
		t.Fatalf("pick = option %d, want the blocker (option 1)", pick)
	}
}

func TestBurnKillsBestCreature(t *testing.T) {
	// Two of their untapped blockers: killing either still leaves one, so no
	// clock moves -- but the 3/3 is their best creature by power, so the bolt
	// takes it anyway (and the damage is not needed for lethal).
	v := burnView(20, 20,
		nil,
		[]view.CardView{burnCV(20, "Creature", 3, 3, 0, 1, false), burnCV(21, "Creature", 2, 2, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20), burnCreOpt(2, 21))
	f := burnViewBoard(t, v, d)
	if best := f.bestCreatureScore(1); best != 3 {
		t.Fatalf("fixture: their best-creature score = %d, want 3", best)
	}
	if conn := f.connect(0, 1, 20); conn != 0 {
		t.Fatalf("fixture: killing the 3/3 must not open the clock, got %d", conn)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{2}}}
	_, pick := burnDecide(t, inner, v, d)
	if pick != 1 {
		t.Fatalf("pick = option %d, want their best creature (option 1)", pick)
	}
}

func TestBurnFaceWithinReach(t *testing.T) {
	// A 2-power flyer connects every turn; 5 life minus 3 bolt damage leaves
	// them within one attack of death, so the face is taken.
	v := burnView(20, 5,
		[]view.CardView{burnCV(10, "Creature", 2, 2, 0, 0, false, "Flying")},
		nil)
	d := burnD(intp(3), burnFaceOpt(0, 1))
	f := burnViewBoard(t, v, d)
	if conn := f.connect(0, 1, 0); conn != 2 {
		t.Fatalf("fixture: our connectable power = %d, want 2", conn)
	}
	if reach := 5 - 3; reach > 2 {
		t.Fatalf("fixture: opponent not within reach (%d left)", reach)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
	_, pick := burnDecide(t, inner, v, d)
	if pick != 0 {
		t.Fatalf("pick = option %d, want the face", pick)
	}
}

func TestBurnFaceWhenAhead(t *testing.T) {
	// Our flyer connects 3 a turn (4 turns to kill) while their 5/5 ground
	// creature can never get through our untapped flyer as a blocker: we are
	// ahead in the race, and the 5/5 is not killable by the bolt, so the face
	// is taken.
	v := burnView(20, 12,
		[]view.CardView{burnCV(10, "Creature", 3, 3, 0, 0, false, "Flying")},
		[]view.CardView{burnCV(20, "Creature", 5, 5, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	f := burnViewBoard(t, v, d)
	if conn := f.connect(0, 1, 0); conn != 3 {
		t.Fatalf("fixture: our connectable power = %d, want 3", conn)
	}
	if theirConn := f.connect(1, 0, 0); theirConn != 0 {
		t.Fatalf("fixture: their connectable power = %d, want 0", theirConn)
	}
	if c := f.cre[20]; c.toughness <= 3 {
		t.Fatal("fixture: the 5/5 must not be killable by 3")
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{1}}}
	_, pick := burnDecide(t, inner, v, d)
	if pick != 0 {
		t.Fatalf("pick = option %d, want the face", pick)
	}
}

func TestBurnDelegatesWhenUseless(t *testing.T) {
	// No clock moves (one blocker each way), the 5/5 is not killable, we are
	// not ahead: the bolt is useless, so the inner's answer stands unchanged.
	v := burnView(20, 20,
		[]view.CardView{burnCV(10, "Creature", 2, 2, 0, 0, false)},
		[]view.CardView{burnCV(20, "Creature", 5, 5, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	f := burnViewBoard(t, v, d)
	if conn := f.connect(0, 1, 0); conn != 0 {
		t.Fatalf("fixture: our connectable power = %d, want 0", conn)
	}
	if theirConn := f.connect(1, 0, 0); theirConn != 0 {
		t.Fatalf("fixture: their connectable power = %d, want 0", theirConn)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{1}}}
	got, pick := burnDecide(t, inner, v, d)
	if pick != 1 || len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the inner's [1] unchanged", got.Choices)
	}
}

func TestBurnNeverTargetsOwn(t *testing.T) {
	// Every kind of own-side candidate is offered -- our face, our own
	// 2/2, an unkillable opponent creature on a deadlocked board -- and the
	// inner answers the OPPONENT'S face. Nothing the policy models is
	// taken, and the inner's answer stands unchanged: the decorator must
	// not replace a legal face answer with our own creature (its inner's
	// own-creature index would make the assertion vacuous, so the canned
	// answer deliberately differs from every own-side option).
	v := burnView(20, 20,
		[]view.CardView{burnCV(10, "Creature", 2, 2, 0, 0, false)},
		[]view.CardView{burnCV(20, "Creature", 8, 8, 0, 1, false)})
	d := burnD(intp(3),
		burnFaceOpt(0, 0), burnFaceOpt(1, 1), burnCreOpt(2, 10), burnCreOpt(3, 20))
	f := burnViewBoard(t, v, d)
	if c := f.cre[10]; c.ctl != 0 {
		t.Fatal("fixture: option 2's object is not our own creature")
	}
	if conn := f.connect(0, 1, 0); conn != 0 {
		t.Fatalf("fixture: our connectable power = %d, want 0", conn)
	}
	if theirConn := f.connect(1, 0, 0); theirConn != 0 {
		t.Fatalf("fixture: their connectable power = %d, want 0", theirConn)
	}
	if rem := int32(8) - 3; rem <= 3 {
		t.Fatal("fixture: the 8/8 must not be killable by 3")
	}
	if best := f.bestCreatureScore(0); best != 2 {
		t.Fatalf("fixture: our own best-creature score = %d, want 2", best)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{1}}}
	got, pick := burnDecide(t, inner, v, d)
	if pick != 1 || got.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the inner's [1] (opponent face) unchanged", got.Choices)
	}
}

func TestBurnNeverBoltsOwnCreatureOnARaceBoard(t *testing.T) {
	// A winning race where our own 4/3 happens to be our board's best
	// evasion-weighted creature: the opponent is at 5 and the bolt for 3
	// leaves them within reach of our next attack, so the face is taken --
	// never our own creature, however good its class-2 score would be.
	v := burnView(20, 5,
		[]view.CardView{burnCV(10, "Creature", 2, 2, 0, 0, false, "Flying"),
			burnCV(11, "Creature", 4, 3, 0, 0, false)},
		nil)
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 11))
	f := burnViewBoard(t, v, d)
	if c := f.cre[11]; c.ctl != 0 || c.power != 4 || c.toughness != 3 {
		t.Fatal("fixture: option 1 is not our own 4/3")
	}
	if conn := f.connect(0, 1, 0); conn != 6 {
		t.Fatalf("fixture: our connectable power = %d, want 6 (no blockers)", conn)
	}
	if reach := 5 - 3; reach > 6 {
		t.Fatalf("fixture: opponent not within reach (%d left)", reach)
	}
	if best := f.bestCreatureScore(0); best != 4 || f.cre[11].evScore() != 4 {
		t.Fatalf("fixture: own best score = %d, own 4/3 evScore = %d, want 4/4",
			best, f.cre[11].evScore())
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
	_, pick := burnDecide(t, inner, v, d)
	if pick != 0 {
		t.Fatalf("pick = option %d, want the opponent's face (option 0), never our own creature", pick)
	}
}

func TestBurnDelegatesUnmodelledAsks(t *testing.T) {
	v := burnView(20, 20, nil, []view.CardView{burnCV(20, "Creature", 3, 3, 0, 1, false)})
	cases := []struct {
		name string
		d    decision.Decision
	}{
		{"no published damage", burnD(nil, burnFaceOpt(0, 1))},
		{"zero amount", burnD(intp(0), burnFaceOpt(0, 1))},
		{"other damage API", func() decision.Decision {
			d := burnD(intp(3), burnFaceOpt(0, 1))
			d.TargetEffect.API = "DamageAll"
			return d
		}()},
		{"no target payload at all", func() decision.Decision {
			d := burnD(intp(3), burnFaceOpt(0, 1))
			d.TargetEffect = nil
			return d
		}()},
		{"no modelled candidate", burnD(intp(3), burnFaceOpt(0, 0))},
	}
	for _, tc := range cases {
		inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
		got, pick := burnDecide(t, inner, v, tc.d)
		if pick != 0 || got.Choices[0] != 0 {
			t.Fatalf("%s: choices = %v, want the inner's [0] unchanged", tc.name, got.Choices)
		}
	}
}

func TestBurnDelegatesMultiPickAsk(t *testing.T) {
	// A two-target ask is not the shape the policy models; the inner's
	// two-pick answer stands unchanged.
	v := burnView(20, 20, nil, []view.CardView{burnCV(20, "Creature", 3, 3, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	d.Min, d.Max = 2, 2
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0, 1}}}
	got, err := newBurn(inner, 5).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(got); err != nil {
		t.Fatalf("answer does not validate: %v", err)
	}
	if len(got.Choices) != 2 || got.Choices[0] != 0 || got.Choices[1] != 1 {
		t.Fatalf("choices = %v, want the inner's [0 1] unchanged", got.Choices)
	}
}

func TestBurnOpponentSourceDelegates(t *testing.T) {
	// The view can name the resolving object on the stack, and its
	// controller is the opponent: the effect is not ours, so the decorator
	// delegates even though the ask itself is modelled.
	v := burnView(20, 20, nil, []view.CardView{burnCV(20, "Creature", 2, 2, 0, 1, false)})
	v.Stack = []view.StackView{{ID: 99, Kind: "spell", Name: "Bolt", Controller: 1}}
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	if sv := v.Stack[0]; sv.Controller == d.Player {
		t.Fatal("fixture: the source controller must not be the asked player")
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
	got, pick := burnDecide(t, inner, v, d)
	if pick != 0 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the inner's [0] unchanged", got.Choices)
	}
}

func TestBurnDelegatesOtherKinds(t *testing.T) {
	kinds := []decision.Decision{
		{Kind: decision.KPriority, Seq: 7, Player: 0, Options: []decision.Option{{Index: 0, Kind: "pass"}, {Index: 1, Kind: "cast_spell"}}},
		{Kind: decision.KAttackers, Seq: 7, Player: 0, Options: []decision.Option{{Index: 0, Kind: "attack"}}},
		{Kind: decision.KBlockers, Seq: 7, Player: 0, Options: []decision.Option{{Index: 0, Kind: "block"}}},
		{Kind: decision.KMulligan, Seq: 7, Player: 0, Options: []decision.Option{{Index: 0, Kind: "keep"}}},
	}
	for _, d := range kinds {
		inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{1}}}
		got, err := newBurn(inner, 5).Decide(context.Background(), view.View{Viewer: 0}, d)
		if err != nil {
			t.Fatalf("%s: %v", d.Kind, err)
		}
		if inner.decided != 1 {
			t.Fatalf("%s: inner consulted %d times, want 1", d.Kind, inner.decided)
		}
		if len(got.Choices) != 1 || got.Choices[0] != 1 {
			t.Fatalf("%s: choices = %v, want the inner's [1] unchanged", d.Kind, got.Choices)
		}
	}
}

func TestBurnPassesErrorsThrough(t *testing.T) {
	// An inner refusal propagates: the decorator never answers for a failing
	// inner and never applies its own pick on top of an error.
	v := burnView(20, 3, nil, nil)
	d := burnD(intp(3), burnFaceOpt(0, 1))
	inner := &stubSeat{err: errRefused}
	_, err := newBurn(inner, 5).Decide(context.Background(), v, d)
	if err == nil || err.Error() != "refused" {
		t.Fatalf("err = %v, want the inner's refusal", err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
}

func TestBurnBoardSurfaceMatches(t *testing.T) {
	// The BoardSeat surface computes the same pick off the same public facts:
	// the lethal-face case through DecideBoard.
	b := botpolicy.Board{
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 3, Toughness: 3, Controller: 0, Keywords: []string{"Flying"}},
			20: {Power: 2, Toughness: 2, Controller: 1},
		},
		Life: map[state.PlayerID]int32{0: 20, 1: 3},
	}
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	f := burnFactsFromBoard(b, d)
	if conn := f.connect(0, 1, 0); conn != 3 {
		t.Fatalf("fixture: board connectable power = %d, want 3", conn)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{1}}}
	wrapped := newBurn(inner, 5)
	bs, ok := wrapped.(interface {
		DecideBoard(context.Context, botpolicy.Board, decision.Decision) (decision.Intent, error)
	})
	if !ok {
		t.Fatal("wrapper over a BoardSeat inner does not implement DecideBoard")
	}
	got, err := bs.DecideBoard(context.Background(), b, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(got); err != nil {
		t.Fatalf("board-surface answer does not validate: %v", err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.boarded)
	}
	if got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the face [0]", got.Choices)
	}
}

func TestBurnBoardSurfaceKillsBlocker(t *testing.T) {
	// The clock-moving kill on the board surface, matching the view surface.
	b := botpolicy.Board{
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 3, Toughness: 3, Controller: 0},
			20: {Power: 3, Toughness: 3, Controller: 1},
		},
		Life: map[state.PlayerID]int32{0: 20, 1: 20},
	}
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20))
	f := burnFactsFromBoard(b, d)
	if conn := f.connect(0, 1, 0); conn != 0 {
		t.Fatalf("fixture: board connectable power = %d, want 0", conn)
	}
	inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
	wrapped := newBurn(inner, 5)
	got, err := wrapped.(interface {
		DecideBoard(context.Context, botpolicy.Board, decision.Decision) (decision.Intent, error)
	}).DecideBoard(context.Background(), b, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.boarded)
	}
	if got.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the blocker [1]", got.Choices)
	}
}

func TestBurnKeepsThePlainSurface(t *testing.T) {
	// A non-BoardSeat inner must NOT be wrapped into a BoardSeat -- the
	// engine would then hand it a board instead of a projected View.
	plain := &stubPlain{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
	wrapped := newBurn(plain, 5)
	if _, isBoard := wrapped.(seat.BoardSeat); isBoard {
		t.Fatal("wrapper over a non-BoardSeat inner implements seat.BoardSeat")
	}
	v := burnView(20, 3, nil, nil)
	d := burnD(intp(3), burnFaceOpt(0, 1))
	got, err := wrapped.Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if plain.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", plain.decided)
	}
	if got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the face [0]", got.Choices)
	}
}

func TestBurnWantsPaymentActions(t *testing.T) {
	with := &stubSeat{wantsPayment: true}
	without := &stubSeat{}
	if !newBurn(with, 5).(*burnBoard).WantsPaymentActions() {
		t.Fatal("wrapper did not pass the inner's payment opt-in through")
	}
	if newBurn(without, 5).(*burnBoard).WantsPaymentActions() {
		t.Fatal("wrapper opted into payment plans for a non-consumer inner")
	}
}

func TestBurnUnwrapsThroughTheRegistry(t *testing.T) {
	wrapped, err := Build("sb-first+burn", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wrapped.(*builtins.Seat); ok {
		t.Fatal("sb-first+burn built the bare base seat")
	}
	if _, ok := UnwrapSeat(wrapped).(*builtins.Seat); !ok {
		t.Fatalf("UnwrapSeat reached %T, want *builtins.Seat", UnwrapSeat(wrapped))
	}
}

func TestBurnTieBreakIsSeeded(t *testing.T) {
	// Two identical blockers: both are the best creature, both are tied.
	// The pick must be one of them and stable for the same seed.
	v := burnView(20, 20,
		nil,
		[]view.CardView{burnCV(20, "Creature", 3, 3, 0, 1, false), burnCV(21, "Creature", 3, 3, 0, 1, false)})
	d := burnD(intp(3), burnFaceOpt(0, 1), burnCreOpt(1, 20), burnCreOpt(2, 21))
	var picks []int
	for i := 0; i < 2; i++ {
		inner := &stubSeat{answer: decision.Intent{Seq: 7, Choices: []int{0}}}
		_, pick := burnDecide(t, inner, v, d)
		picks = append(picks, pick)
	}
	if picks[0] != picks[1] {
		t.Fatalf("same seed gave different picks: %v", picks)
	}
	if picks[0] != 1 && picks[0] != 2 {
		t.Fatalf("pick = %d, want one of the two tied blockers", picks[0])
	}
}
