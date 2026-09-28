package registry

// Tests for the mull decorator (mull.go). The hands are built from real
// pauper-kernel deck cards -- Galvanic Blast, Krark-Clan Shaman, Thoughtcast,
// Toxin Analysis, Refurbished Familiar, the Affinity artifact lands and the
// Elves mana dorks -- so the printed mana costs and colours the rule reads
// are the corpus's own; the view is hand-built so the test needs no corpus.

import (
	"context"
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// mullStub is a plain inner seat: it counts consultations and answers with a
// canned intent. It is NOT a BoardSeat.
type mullStub struct {
	decided int
	answer  decision.Intent
	err     error
}

func (s *mullStub) Decide(_ context.Context, _ view.View, _ decision.Decision) (decision.Intent, error) {
	s.decided++
	return s.answer, s.err
}

// mullBoardStub is a BoardSeat inner: it must be reached through DecideBoard
// built from the view, never through Decide.
type mullBoardStub struct {
	boarded int
	answer  decision.Intent
	err     error
}

func (s *mullBoardStub) Decide(_ context.Context, _ view.View, _ decision.Decision) (decision.Intent, error) {
	return decision.Intent{}, errors.New("mull: BoardSeat inner must be answered through DecideBoard")
}

func (s *mullBoardStub) DecideBoard(_ context.Context, _ botpolicy.Board, _ decision.Decision) (decision.Intent, error) {
	s.boarded++
	return s.answer, s.err
}

// mullLand builds a hand-card CardView for a basic-like land producing the
// named colours ("R", "WU"), or every colour when colours is "ANY".
func mullLand(id state.ObjID, name, colours string) view.CardView {
	mp := cards.ManaProduction{}
	if colours == "ANY" {
		mp.Any = true
	} else {
		for i := 0; i < len(colours); i++ {
			if c := mullColourIndex(colours[i]); c >= 0 {
				mp.Colour[c] = 1
			}
		}
	}
	return view.CardView{ID: id, Name: name, Types: "Land", Produces: &mp}
}

// mullSpell builds a hand-card CardView for a spell with the given printed
// mana cost.
func mullSpell(id state.ObjID, name, cost string) view.CardView {
	return view.CardView{ID: id, Name: name, Types: "Instant", ManaCost: cost}
}

// mullView wraps a hand in the deciding seat's (seat 0) own view. A nil hand
// is kept nil (a redacted hidden zone) so the "cannot build" path is
// testable.
func mullView(hand []view.CardView) view.View {
	return view.View{Viewer: 0, Players: []view.PlayerView{{ID: 0, Hand: hand}}}
}

// mullKeepDecision is the keep/mulligan shape rules/mulligan.go asks: both
// options, Min == Max == 1.
func mullKeepDecision() decision.Decision {
	return decision.Decision{
		Seq: 5, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "keep", Label: "keep"},
			{Index: 1, Kind: "mulligan", Label: "mulligan"},
		},
	}
}

// mullBottomDecision is the bottoming shape: one "bottom" option per hand
// card, Min == Max == n.
func mullBottomDecision(hand []view.CardView, n int) decision.Decision {
	opts := make([]decision.Option, len(hand))
	for j, cv := range hand {
		opts[j] = decision.Option{Index: j, Kind: "bottom", Label: cv.Name, Obj: cv.ID, Player: 0}
	}
	return decision.Decision{
		Seq: 6, Player: 0, Kind: decision.KMulligan, Min: n, Max: n, Options: opts,
	}
}

// TestMullKeepRule pins every branch of the keep rule: a claimed hand is
// kept, and a hand that fails any clause is mulliganed.
func TestMullKeepRule(t *testing.T) {
	// three red artifact lands: Great Furnace produces {R}.
	red := func(id state.ObjID) view.CardView { return mullLand(id, "Great Furnace", "R") }
	// A hand of exactly total cards: n lands of the named colour followed by
	// the given spell costs. It panics on a miscounted fixture so a test can
	// never silently assert on the wrong hand size.
	hand7 := func(total, lands int, landColour string, costs ...string) []view.CardView {
		if total != lands+len(costs) {
			panic("mull test fixture: hand size != lands + spells")
		}
		var h []view.CardView
		for i := 0; i < lands; i++ {
			h = append(h, mullLand(state.ObjID(len(h)+1), "Great Furnace", landColour))
		}
		for i, cost := range costs {
			h = append(h, mullSpell(state.ObjID(100+i), "Spell", cost))
		}
		return h
	}

	cases := []struct {
		name string
		hand []view.CardView
		keep bool
	}{
		{
			// 3 red lands + four 1-mana red spells: castable, colour ok.
			name: "keep seven_on curve red",
			hand: hand7(7, 3, "R", "R", "R", "R", "R"),
			keep: true,
		},
		{
			// 6 lands is outside 2..5.
			name: "mulligan seven_six lands",
			hand: hand7(7, 6, "R", "R"),
			keep: false,
		},
		{
			// 1 land is below 2.
			name: "mulligan seven_one land",
			hand: hand7(7, 1, "R", "R", "R", "R", "R", "R", "R"),
			keep: false,
		},
		{
			// 3 red lands but every spell needs black: no castable spell.
			name: "mulligan seven_colour mismatch",
			hand: hand7(7, 3, "R", "B", "B", "B", "B"),
			keep: false,
		},
		{
			// 3 red lands, spells too expensive for turn 3.
			name: "mulligan seven_nothing castable by turn three",
			hand: hand7(7, 3, "R", "3 R", "4 R", "3 R", "4 R"),
			keep: false,
		},
		{
			// 3 red lands + 4 colourless-cost spells whose colour the lands
			// do not satisfy: no pip is payable.
			name: "mulligan seven_no colour the lands make",
			hand: hand7(7, 3, "R", "B", "B", "B", "B"),
			keep: false,
		},
		{
			// Any-colour land covers the black spells.
			name: "keep seven_any-colour land",
			hand: []view.CardView{
				mullLand(1, "Drossforge Bridge", "ANY"),
				mullLand(2, "Drossforge Bridge", "ANY"),
				mullLand(3, "Drossforge Bridge", "ANY"),
				mullSpell(4, "Toxin Analysis", "B"),
				mullSpell(5, "Toxin Analysis", "B"),
				mullSpell(6, "Refurbished Familiar", "3 B"),
				mullSpell(7, "Toxin Analysis", "B"),
			},
			keep: true,
		},
		{
			// At six cards, 1 land and anything castable by turn 3 keeps.
			name: "keep six_one land and a cheap spell",
			hand: []view.CardView{
				red(1),
				mullSpell(2, "Galvanic Blast", "R"),
				mullSpell(3, "Thoughtcast", "4 U"),
				mullSpell(4, "Myr Enforcer", "7"),
				mullSpell(5, "Thoughtcast", "4 U"),
				mullSpell(6, "Myr Enforcer", "7"),
			},
			keep: true,
		},
		{
			// At six, no land fails the 1..5 lands clause.
			name: "mulligan six_no land",
			hand: []view.CardView{
				mullSpell(1, "Galvanic Blast", "R"),
				mullSpell(2, "Galvanic Blast", "R"),
				mullSpell(3, "Galvanic Blast", "R"),
				mullSpell(4, "Galvanic Blast", "R"),
				mullSpell(5, "Galvanic Blast", "R"),
				mullSpell(6, "Galvanic Blast", "R"),
			},
			keep: false,
		},
		{
			// At six, six lands is above 5.
			name: "mulligan six_six lands",
			hand: []view.CardView{
				red(1), red(2), red(3), red(4), red(5), red(6),
			},
			keep: false,
		},
		{
			// At six, lands but nothing castable by turn 3.
			name: "mulligan six_nothing castable",
			hand: []view.CardView{
				red(1), red(2), red(3),
				mullSpell(4, "Thoughtcast", "4 U"),
				mullSpell(5, "Myr Enforcer", "7"),
				mullSpell(6, "Thoughtcast", "4 U"),
			},
			keep: false,
		},
		{
			// Five or fewer is always kept, however bad the hand.
			name: "keep five_always",
			hand: []view.CardView{
				mullSpell(1, "Myr Enforcer", "7"),
				mullSpell(2, "Myr Enforcer", "7"),
				mullSpell(3, "Myr Enforcer", "7"),
				mullSpell(4, "Myr Enforcer", "7"),
				mullSpell(5, "Myr Enforcer", "7"),
			},
			keep: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &mullStub{answer: decision.Intent{Seq: 5, Player: 0, Choices: []int{0}}}
			wrapped := newMull(inner, 7)
			d := mullKeepDecision()
			got, err := wrapped.Decide(context.Background(), mullView(tc.hand), d)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if inner.decided != 1 {
				t.Fatalf("inner consulted %d times, want 1", inner.decided)
			}
			if err := d.Validate(got); err != nil {
				t.Fatalf("answer %+v does not validate: %v (the decorator must never submit an illegal answer)", got, err)
			}
			keep := got.Choices[0] == 0
			if keep != tc.keep {
				t.Fatalf("kept = %v (choices %v), want %v for hand %d cards", keep, got.Choices, tc.keep, len(tc.hand))
			}
		})
	}
}

// TestMullKeepKeepsWhenOnlyKeepOffered: a seat out of permitted mulligans is
// offered keep only; a hand the rule would mulligan still answers keep
// rather than submitting an option the decision does not offer.
func TestMullKeepKeepsWhenOnlyKeepOffered(t *testing.T) {
	// 7 cards, one land: the rule says mulligan, but only keep is offered.
	hand := []view.CardView{
		mullLand(1, "Great Furnace", "R"),
		mullSpell(2, "Myr Enforcer", "7"),
		mullSpell(3, "Myr Enforcer", "7"),
		mullSpell(4, "Myr Enforcer", "7"),
		mullSpell(5, "Myr Enforcer", "7"),
		mullSpell(6, "Myr Enforcer", "7"),
		mullSpell(7, "Myr Enforcer", "7"),
	}
	inner := &mullStub{answer: decision.Intent{Seq: 5, Player: 0, Choices: []int{1}}}
	wrapped := newMull(inner, 7)
	d := decision.Decision{
		Seq: 5, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "keep", Label: "keep"}},
	}
	got, err := wrapped.Decide(context.Background(), mullView(hand), d)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(got); err != nil {
		t.Fatalf("answer %+v does not validate: %v", got, err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the only offered keep [0]", got.Choices)
	}
}

// TestMullBottomRule pins every branch of the bottoming rule.
func TestMullBottomRule(t *testing.T) {
	// mullHandOf builds a hand of nlands red lands (ids 1..) then the spells.
	mullHandOf := func(nlands int, spells ...string) []view.CardView {
		var h []view.CardView
		for i := 0; i < nlands; i++ {
			h = append(h, mullLand(state.ObjID(len(h)+1), "Great Furnace", "R"))
		}
		for i, cost := range spells {
			h = append(h, mullSpell(state.ObjID(100+i), "Spell", cost))
		}
		return h
	}

	cases := []struct {
		name string
		hand []view.CardView
		n    int
		want []int // expected bottomed OPTION indices, as a set
	}{
		{
			// Six lands + a spell, bottom 2: the two excess lands, never the
			// spell, leaving four lands.
			name: "excess lands first",
			hand: mullHandOf(6, "R"),
			n:    2,
			want: []int{4, 5},
		},
		{
			// Three lands, an uncastable 6-drop and a castable 1-drop: the
			// expensive uncastable spell goes first.
			name: "uncastable first",
			hand: mullHandOf(3, "6 R", "R"),
			n:    1,
			want: []int{3},
		},
		{
			// Two castable spells: the most expensive goes first.
			name: "most expensive castable",
			hand: mullHandOf(3, "3 R", "R"),
			n:    1,
			want: []int{3},
		},
		{
			// Two lands + five spells, bottom 3: no land is bottomed, since
			// the land count would fall below 2.
			name: "lands kept at two",
			hand: mullHandOf(2, "6 R", "5 R", "4 R", "3 R", "R"),
			n:    3,
			want: []int{2, 3, 4},
		},
		{
			// A single land above nothing: it must not be bottomed while a
			// spell can go instead.
			name: "one land never bottomed",
			hand: mullHandOf(1, "6 R", "5 R"),
			n:    1,
			want: []int{1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &mullStub{answer: decision.Intent{Seq: 6, Player: 0, Choices: []int{0}}}
			wrapped := newMull(inner, 7)
			d := mullBottomDecision(tc.hand, tc.n)
			got, err := wrapped.Decide(context.Background(), mullView(tc.hand), d)
			if err != nil {
				t.Fatal(err)
			}
			if inner.decided != 1 {
				t.Fatalf("inner consulted %d times, want 1", inner.decided)
			}
			if err := d.Validate(got); err != nil {
				t.Fatalf("answer %+v does not validate: %v", got, err)
			}
			if !sameSet(got.Choices, tc.want) {
				t.Fatalf("bottomed %v, want %v", got.Choices, tc.want)
			}
		})
	}
}

// TestMullDelegatesEverything: every non-mulligan decision goes through
// untouched and the inner is consulted exactly once.
func TestMullDelegatesEverything(t *testing.T) {
	d := decision.Decision{Kind: decision.KTarget, Seq: 7, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "target"}}}
	in := decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}
	inner := &mullStub{answer: in}
	wrapped := newMull(inner, 5)
	got, err := wrapped.Decide(context.Background(), view.View{}, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
	if len(got.Choices) != 1 || got.Choices[0] != in.Choices[0] {
		t.Fatalf("choices = %v, want inner's %v unchanged", got.Choices, in.Choices)
	}
}

// TestMullDelegatesWhenRuleCannotBuild: a mulligan decision the rule cannot
// answer from the view (no hand) falls back to the inner's answer, never a
// pass.
func TestMullDelegatesWhenRuleCannotBuild(t *testing.T) {
	inner := &mullStub{answer: decision.Intent{Seq: 5, Player: 0, Choices: []int{0}}}
	wrapped := newMull(inner, 5)
	d := mullKeepDecision()
	// A view with no hand for seat 0 (another seat only / redacted).
	v := view.View{Viewer: 0, Players: []view.PlayerView{{ID: 1}}}
	got, err := wrapped.Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.decided)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want inner's keep [0]", got.Choices)
	}
}

// TestMullPassesErrorsThrough: the wrapper never answers for a failing inner.
func TestMullPassesErrorsThrough(t *testing.T) {
	inner := &mullStub{answer: decision.Intent{Seq: 5, Player: 0, Choices: []int{0}}, err: errors.New("refused")}
	wrapped := newMull(inner, 5)
	hand := []view.CardView{mullLand(1, "Great Furnace", "R"), mullSpell(2, "Galvanic Blast", "R")}
	_, err := wrapped.Decide(context.Background(), mullView(hand), mullKeepDecision())
	if err == nil || err.Error() != "refused" {
		t.Fatalf("err = %v, want the inner's refusal", err)
	}
}

// TestMullBoardSeatDelegation: a BoardSeat inner is answered through
// DecideBoard (built from the view), and the decorator still overrides a
// mulligan decision it can build.
func TestMullBoardSeatDelegation(t *testing.T) {
	inner := &mullBoardStub{answer: decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}}
	wrapped := newMull(inner, 5)

	// A non-mulligan decision: inner's board answer, unchanged.
	d := decision.Decision{Kind: decision.KTarget, Seq: 7, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "target"}}}
	got, err := wrapped.Decide(context.Background(), view.View{}, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.boarded != 1 {
		t.Fatalf("board inner consulted %d times, want 1", inner.boarded)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want inner's [0]", got.Choices)
	}

	// A mulligan decision the rule can build: the decorator's pick wins
	// (3 red lands + a red 1-drop is a keep).
	inner.answer = decision.Intent{Seq: 5, Player: 0, Choices: []int{1}}
	hand := []view.CardView{
		mullLand(1, "Great Furnace", "R"),
		mullLand(2, "Great Furnace", "R"),
		mullLand(3, "Great Furnace", "R"),
		mullSpell(4, "Galvanic Blast", "R"),
		mullSpell(5, "Galvanic Blast", "R"),
		mullSpell(6, "Galvanic Blast", "R"),
		mullSpell(7, "Galvanic Blast", "R"),
	}
	got, err = wrapped.Decide(context.Background(), mullView(hand), mullKeepDecision())
	if err != nil {
		t.Fatal(err)
	}
	if inner.boarded != 2 {
		t.Fatalf("board inner consulted %d times, want 2", inner.boarded)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the decorator's keep [0] over inner's mulligan", got.Choices)
	}
}

// TestMullResolvesThroughRegistry: `sb-first+mull` builds through Build and
// is a decorated seat; UnwrapSeat sees through to the builtin underneath.
func TestMullResolvesThroughRegistry(t *testing.T) {
	bare, err := Build("sb-first", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.(*builtins.Seat); !ok {
		t.Fatalf("sb-first built %T, want *builtins.Seat (precondition)", bare)
	}
	wrapped, err := Build("sb-first+mull", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wrapped.(*builtins.Seat); ok {
		t.Fatal("sb-first+mull built the bare base seat")
	}
	if _, ok := wrapped.(Unwrapper); !ok {
		t.Fatal("mullSeat does not implement registry.Unwrapper")
	}
	b, ok := UnwrapSeat(wrapped).(*builtins.Seat)
	if !ok {
		t.Fatalf("UnwrapSeat(sb-first+mull) = %T, want *builtins.Seat", UnwrapSeat(wrapped))
	}
	if b.Policy() != builtins.First {
		t.Fatalf("unwrapped seat plays %v, want First", b.Policy())
	}
	if !Has("sb-first+mull") {
		t.Fatal("Has rejected a valid spec with the mull decorator")
	}
}

// sameSet reports whether a and b hold the same elements (both are sets of
// small option indices).
func sameSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[int]int, len(a))
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

var _ seat.Seat = (*mullBoardStub)(nil)
var _ seat.BoardSeat = (*mullBoardStub)(nil)
