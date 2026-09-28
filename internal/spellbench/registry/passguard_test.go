package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// stubSeat is an inner seat that counts how the wrapper consults it and
// answers with a canned intent. It implements BoardSeat and
// PaymentPlanConsumer so both wrapper paths and the payment opt-in are
// observable.
type stubSeat struct {
	decided, boarded int
	answer           decision.Intent
	err              error
	wantsPayment     bool
}

func (s *stubSeat) Decide(_ context.Context, _ view.View, d decision.Decision) (decision.Intent, error) {
	s.decided++
	return s.answer, s.err
}

func (s *stubSeat) DecideBoard(_ context.Context, _ botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	s.boarded++
	return s.answer, s.err
}

func (s *stubSeat) WantsPaymentActions() bool { return s.wantsPayment }

// stubPlain answers Decide only: a non-BoardSeat inner. It deliberately
// does NOT embed stubSeat, whose DecideBoard would make it a BoardSeat.
type stubPlain struct {
	decided int
	answer  decision.Intent
}

func (s *stubPlain) Decide(_ context.Context, _ view.View, d decision.Decision) (decision.Intent, error) {
	s.decided++
	return s.answer, nil
}

// priorityD builds a priority decision: the pass option first, then the
// named candidate kinds, in order.
func priorityD(kinds ...string) decision.Decision {
	opts := []decision.Option{{Index: 0, Kind: "pass"}}
	for i, k := range kinds {
		opts = append(opts, decision.Option{Index: i + 1, Kind: k})
	}
	return decision.Decision{Kind: decision.KPriority, Seq: 7, Options: opts}
}

func targetD() decision.Decision {
	return decision.Decision{Kind: decision.KTarget, Seq: 7, Options: []decision.Option{{Index: 0, Kind: "target"}}}
}

var passIntent = decision.Intent{Seq: 7, Choices: []int{0}}

// TestPassguardDelegatesEverything pins the contract's other half: every
// decision the documented case does not name goes through untouched, and
// the inner seat is consulted exactly once per decision (a seed-streamed
// inner draws the same numbers it would have drawn bare).
func TestPassguardDelegatesEverything(t *testing.T) {
	cases := []struct {
		name string
		d    decision.Decision
		in   decision.Intent
	}{
		{"non-priority decision", targetD(), decision.Intent{Seq: 7, Choices: []int{0}}},
		{"non-pass answer", priorityD("play_land", "cast_spell"), decision.Intent{Seq: 7, Choices: []int{2}}},
		{"pass with two land plays", priorityD("play_land", "play_land"), passIntent},
		{"pass with a lone non-land candidate", priorityD("cast_spell"), passIntent},
		{"pass with a lone non-pass candidate of an unknown kind", priorityD("activate_mana_ability"), passIntent},
		{"pass with no candidates at all", priorityD(), passIntent},
	}
	for _, tc := range cases {
		inner := &stubSeat{answer: tc.in}
		wrapped := newPassguard(inner, 5)
		if _, isBoard := wrapped.(seat.BoardSeat); !isBoard {
			t.Fatalf("%s: wrapper is not a seat.BoardSeat over a BoardSeat inner", tc.name)
		}
		got, err := wrapped.(seat.BoardSeat).DecideBoard(context.Background(), botpolicy.Board{}, tc.d)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if inner.boarded != 1 {
			t.Fatalf("%s: inner consulted %d times, want 1", tc.name, inner.boarded)
		}
		if got.Choices == nil && len(tc.in.Choices) == 0 {
			continue
		}
		if len(got.Choices) != len(tc.in.Choices) || (len(tc.in.Choices) > 0 && got.Choices[0] != tc.in.Choices[0]) {
			t.Fatalf("%s: choices = %v, want the inner's %v (unchanged)", tc.name, got.Choices, tc.in.Choices)
		}
	}
}

// TestPassguardReplacesLoneLandPlay pins the one documented replacement:
// the decision really offers a pass and exactly one other candidate, that
// candidate really is a land play, and the pass answer comes back naming it.
func TestPassguardReplacesLoneLandPlay(t *testing.T) {
	d := priorityD("play_land")
	if len(d.Options) != 2 || d.Options[1].Kind != "play_land" {
		t.Fatalf("fixture: decision offers %d options, want pass + one play_land", len(d.Options))
	}
	inner := &stubSeat{answer: passIntent}
	wrapped := newPassguard(inner, 5).(seat.BoardSeat)
	got, err := wrapped.DecideBoard(context.Background(), botpolicy.Board{}, d)
	if err != nil {
		t.Fatal(err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.boarded)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the lone land play's option index [1]", got.Choices)
	}
}

// TestPassguardPassesErrorsThrough: the wrapper never answers for a failing
// inner, and it does not apply the guard to an error return.
func TestPassguardPassesErrorsThrough(t *testing.T) {
	inner := &stubSeat{err: errors.New("refused")}
	wrapped := newPassguard(inner, 5).(seat.BoardSeat)
	_, err := wrapped.DecideBoard(context.Background(), botpolicy.Board{}, priorityD("play_land"))
	if err == nil || err.Error() != "refused" {
		t.Fatalf("err = %v, want the inner's refusal", err)
	}
	if inner.boarded != 1 {
		t.Fatalf("inner consulted %d times, want 1", inner.boarded)
	}
}

// TestPassguardKeepsThePlainSurface: a non-BoardSeat inner must NOT be
// wrapped into a BoardSeat -- the engine would then hand it a board instead
// of a projected View, changing which surface inner sees.
func TestPassguardKeepsThePlainSurface(t *testing.T) {
	plain := &stubPlain{answer: passIntent}
	wrapped := newPassguard(plain, 5)
	if _, isBoard := wrapped.(seat.BoardSeat); isBoard {
		t.Fatal("wrapper over a non-BoardSeat inner implements seat.BoardSeat")
	}
	got, err := wrapped.Decide(context.Background(), view.View{}, priorityD("play_land"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.decided != 1 {
		t.Fatalf("inner consulted %d times, want 1", plain.decided)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the land play [1]", got.Choices)
	}
}

// TestPassguardWantsPaymentActions: the payment opt-in is the inner's, not
// the wrapper's own.
func TestPassguardWantsPaymentActions(t *testing.T) {
	with := &stubSeat{wantsPayment: true}
	without := &stubSeat{}
	if !newPassguard(with, 5).(seat.PaymentPlanConsumer).WantsPaymentActions() {
		t.Fatal("wrapper did not pass the inner's payment opt-in through")
	}
	if newPassguard(without, 5).(seat.PaymentPlanConsumer).WantsPaymentActions() {
		t.Fatal("wrapper opted into payment plans for a non-consumer inner")
	}
}

// TestPassguardResolvesThroughTheRegistry: the spec `sb-first+passguard`
// builds through Build and is a decorated seat, not the bare base.
func TestPassguardResolvesThroughTheRegistry(t *testing.T) {
	bare, err := Build("sb-first", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.(*builtins.Seat); !ok {
		t.Fatalf("sb-first built %T, want *builtins.Seat (precondition)", bare)
	}
	wrapped, err := Build("sb-first+passguard", 19)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wrapped.(*builtins.Seat); ok {
		t.Fatal("sb-first+passguard built the bare base seat")
	}
}
