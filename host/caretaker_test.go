package host

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestHumanCaretakerUsesConfiguredPolicy(t *testing.T) {
	o := testOptions(t)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(TableConfig{ID: "t1", Seats: 2, Decks: []string{"a", "b"}, Spectator: view.Public,
		Humans: []int{0}, BotPolicy: LethalPressurePolicy}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	var hs *HumanSeat
	deadline := time.Now().Add(2 * time.Second)
	for hs == nil && time.Now().Before(deadline) {
		r.mu.RLock()
		tab := r.tables["t1"]
		r.mu.RUnlock()
		tab.mu.RLock()
		cur := tab.cur
		tab.mu.RUnlock()
		if cur != nil {
			// play() installs slots under the match's own lock, not the table's.
			cur.mu.RLock()
			if len(cur.slots) > 0 {
				hs, _ = cur.slots[0].(*HumanSeat)
			}
			cur.mu.RUnlock()
		}
		if hs == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if hs == nil {
		t.Fatal("human slot was not installed")
	}
	hs.mu.Lock()
	caretaker := hs.caretaker
	hs.mu.Unlock()
	bot, ok := caretaker.(seat.BoardSeat)
	if !ok {
		t.Fatalf("caretaker %T does not expose the board adapter", caretaker)
	}
	board := botpolicy.NewBoard(2)
	board.Life.Set(1, 10)
	board.Creatures.Set(1, botpolicy.Creature{Power: 10, Toughness: 1, Controller: 0})
	board.Creatures.Set(2, botpolicy.Creature{Power: 1, Toughness: 1, Controller: 1})
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Max: 1,
		Options: []decision.Option{{Index: 0, Obj: state.ObjID(1), Player: 1}}}
	in, err := bot.DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(in.Choices, []int{0}) {
		t.Fatalf("configured lethal-pressure caretaker chose %v, want [0]", in.Choices)
	}
}

// TestThinkTimeoutCaretakerTurnsADecisionAndReplays is Task M2b-3's Step-1
// end-to-end proof: a table whose seat 0 is a HumanSeat with a short
// ThinkTimeout, never answered from outside, still advances (the caretaker
// turns every parked slot-0 decision), and the resulting Log replays back to
// the identical chain head (determinism, D3). The game cannot advance past a
// slot-0 decision without the caretaker, because we never call SubmitIntent,
// so the log head advancing is itself proof the caretaker fired.
func TestThinkTimeoutCaretakerTurnsADecisionAndReplays(t *testing.T) {
	o := testOptions(t)
	o.ThinkTimeout = 30 * time.Millisecond
	o.Seats = func(names []string, seed uint64) []seat.Seat {
		out := make([]seat.Seat, len(names))
		for i := range out {
			if i == 0 {
				out[i] = NewHumanSeat() // slot 0 is human; never answered below
			} else {
				out[i] = seat.NewBot(seed ^ uint64(i+1))
			}
		}
		return out
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		r.Close()
		t.Fatal(err)
	}

	// Wait until the live match's event count grows past its initial value:
	// with never a human answer, only the caretaker can turn slot 0, so this
	// proves both that slot 0 parked and that ThinkTimeout converted it.
	deadline := time.Now().Add(10 * time.Second)
	var initial int
	for {
		ms, merr := r.Matches("t1")
		if merr == nil && len(ms) > 0 {
			ev := ms[len(ms)-1].Events
			if initial == 0 {
				initial = ev
			} else if ev > initial {
				break
			}
		}
		if time.Now().After(deadline) {
			r.Close()
			t.Fatal("caretaker never turned a decision: the log head did not advance within 10s")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Stop the match cleanly (abort), then replay the committed log it left
	// behind. replay.Replay returns nil only when it reproduced every event
	// byte for byte and the rebuilt chain head matches — the determinism the
	// caretaker is meant to guarantee.
	r.Close()
	r.Wait("t1")
	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.RLock()
	var m *match
	if len(tb.history) > 0 {
		m = tb.history[0]
	}
	tb.mu.RUnlock()
	if m == nil {
		t.Fatal("close left no match in table history")
	}
	if m.state != protocol.MatchAborted {
		t.Fatalf("match state %s, want aborted", m.state)
	}
	e, err := replay.Replay(m.e.L, m.cfg)
	if err != nil {
		t.Fatalf("replay of the caretaker game diverged: %v", err)
	}
	if got, want := e.L.Head(), m.e.L.Head(); got != want {
		t.Fatalf("replayed chain head %s, recorded %s", got, want)
	}
}

// TestTimeoutTurnsDecisionThenNextDecisionAcceptsHuman pins judgment point 2:
// after the caretaker answers a timed-out decision, the seat is clean — no
// leftover slot, no buffered intent — and a normal human answer to the NEXT
// decision works exactly as before (D2, the slot has moved on).
func TestTimeoutTurnsDecisionThenNextDecisionAcceptsHuman(t *testing.T) {
	hs := NewHumanSeat()
	hs.configure(5*time.Millisecond, seat.NewBot(42))
	d1 := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass"}, {Index: 1, Kind: "cast"}}}
	got1 := make(chan decision.Intent, 1)
	errc1 := make(chan error, 1)
	go func() {
		in, err := hs.Decide(context.Background(), view.View{}, d1)
		if err != nil {
			errc1 <- err
		} else {
			got1 <- in
		}
	}()
	var in1 decision.Intent
	select {
	case in1 = <-got1:
	case err := <-errc1:
		t.Fatalf("caretaker failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("ThinkTimeout did not fire the caretaker")
	}
	if in1.Seq != d1.Seq || len(in1.Choices) == 0 {
		t.Fatalf("caretaker intent %+v for decision %+v", in1, d1)
	}

	// Caretaker answer must leave no slot behind...
	if ok, got := hs.pending(); ok {
		t.Fatalf("pending() = (%v, %+v) after the caretaker answered; want no pending decision", ok, got)
	}
	// ...so a late human answer to the timed-out decision is rejected.
	if err := hs.submit(decision.Intent{Seq: d1.Seq, Player: 0, Choices: []int{0}}); err == nil {
		t.Fatal("late submit for the timed-out decision was accepted")
	}

	// The NEXT decision parks fresh and is answered by a normal submit. Re-arm
	// the seat to zero timeout so only a real human submit can wake it in this
	// phase — the point is a clean seat answering interactively, not the
	// caretaker firing again.
	hs.configure(0, nil)
	d2 := decision.Decision{Seq: 2, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass"}}}
	got2 := make(chan decision.Intent, 1)
	go func() { in, _ := hs.Decide(context.Background(), view.View{}, d2); got2 <- in }()
	select {
	case <-got2:
		t.Fatal("next decision answered without a submit")
	case <-time.After(30 * time.Millisecond):
	}
	if err := hs.submit(decision.Intent{Seq: d2.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit d2: %v", err)
	}
	select {
	case in2 := <-got2:
		if in2.Seq != d2.Seq || len(in2.Choices) != 1 || in2.Choices[0] != 0 {
			t.Fatalf("d2 intent %+v", in2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next decision not answered by the human submit")
	}
}

// TestZeroThinkTimeoutMeansNoTimeout pins judgment point 3: a bare HumanSeat,
// or a zero ThinkTimeout, never turns a decision on its own — it blocks until
// a human submit or ctx cancellation, exactly as before M2b-3.
func TestZeroThinkTimeoutMeansNoTimeout(t *testing.T) {
	hs := NewHumanSeat() // not configured: timeout 0, no caretaker
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass"}}}
	got := make(chan decision.Intent, 1)
	go func() { in, _ := hs.Decide(context.Background(), view.View{}, d); got <- in }()
	select {
	case <-got:
		t.Fatal("a zero-ThinkTimeout seat answered without a submit")
	case <-time.After(50 * time.Millisecond):
	}
	if err := hs.submit(decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	select {
	case in := <-got:
		if in.Seq != 7 || len(in.Choices) != 1 || in.Choices[0] != 0 {
			t.Fatalf("intent %+v", in)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("seat did not answer after a submit under zero ThinkTimeout")
	}
}

// TestCaretakerAnswersOnContextCancelWithZeroTimeout is the "0 means caretaker
// only on ctx cancel" half of judgment point 3: a configured caretaker with a
// zero ThinkTimeout waits forever for a submit, but a ctx cancellation is
// converted to the caretaker's intent rather than erroring (the FL-17 exit: a
// disconnected human must not wedge play).
func TestCaretakerAnswersOnContextCancelWithZeroTimeout(t *testing.T) {
	hs := NewHumanSeat()
	hs.configure(0, seat.NewBot(42)) // zero timeout, but a caretaker installed
	d := decision.Decision{Seq: 3, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass"}}}
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan decision.Intent, 1)
	errc := make(chan error, 1)
	go func() {
		in, err := hs.Decide(ctx, view.View{}, d)
		if err != nil {
			errc <- err
		} else {
			got <- in
		}
	}()
	select {
	case <-got:
		t.Fatal("seat answered before cancel with a zero ThinkTimeout")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case in := <-got:
		if in.Seq != d.Seq || len(in.Choices) == 0 {
			t.Fatalf("caretaker-on-cancel intent %+v", in)
		}
	case err := <-errc:
		t.Fatalf("cancel errored instead of answering with the caretaker: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not fall back to the caretaker")
	}
}
