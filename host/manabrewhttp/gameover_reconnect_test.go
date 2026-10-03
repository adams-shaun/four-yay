//go:build manabrew

package manabrewhttp

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/internal/manabrew/mbtest"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
)

// Answer-A pin for agent-20260930T235845Z-1d63329c (ManaBrew gameOver
// delivery semantics). The scoping brief asked the operator to choose
// between once-per-seat (Answer A) and once-per-connection (Answer B); no
// answer was recorded, so the brief's stated default applies: per-seat is
// correct, matching the scoping spec's Appendix A row ("end of match
// (View.Over) -> gameOver prompt, no response", per-seat language) and
// MBX-2's asserted "exactly one gameOver prompt per seat".
//
// The defect this test closes is not the semantics but a claim about them:
// state.go's comment used to say "reconnect-based clients get it from the
// stream's own latch". That is false -- the latch is seatConn.gameOverSent,
// created once per seat (connFor; a seatConn is never removed), so a
// reconnecting stream shares the ALREADY CONSUMED latch and receives
// nothing. This test makes the real rule explicit and enforceable: a stream
// connected ACROSS match end gets the terminal prompt once; a stream that
// connects AFTER match end is deliberately state-only (the re-served state
// already carries GameViewDto.gameOver = true), and no second terminal
// prompt is ever delivered to the seat.
func TestReconnectGameOverIsDeliberatelyStateOnly(t *testing.T) {
	ts := syntheticServer(t)

	// Precondition the whole test depends on: the match is live and some
	// human seat is parked on the genesis ask. A vacuous setup (no game)
	// must fail loudly rather than let the assertions below pass silently.
	parkedAny(t, ts.reg)

	// Seat 0 keeps its stream open ACROSS match end -- this is what earns
	// the terminal prompt the pin contrasts against. Seat 1 is the conceder
	// and needs a live stream so its queued concede is drained at its first
	// priority (G-2).
	s0 := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s0.close()
	c0 := &syntheticCollector{}
	go readFrames(s0, c0)

	s1 := openStream(t, ts.srv, "/t1/matches/1/stream", "s1")
	defer s1.close()

	// One concession in a 2-seat game ends it immediately; the other seat
	// wins. The directive queues behind non-priority asks (here the genesis
	// starting-player pick), so seat 1's live stream answers its first
	// PRIORITY decision with the concede, unaided by any driver.
	concede := mb.ClientMessage{Value: mb.ClientDirective{Directive: mb.DirectiveInput{Type: "concede"}}}
	if code, pe := send(t, ts, "s1", concede); code != http.StatusNoContent {
		t.Fatalf("queuing seat 1's concede: status %d body %+v, want 204", code, pe)
	}

	// Drive seat 0's genesis asks with first-legal answers so the game
	// reaches seat 1's first priority, where the queued concede fires. Seat
	// 1's non-priority asks are answered too; a priority ask is left to the
	// stream's own drain. A genesis ask that another path already resolved
	// reads back as stalePrompt and is simply skipped -- reaching Over is the
	// precondition this loop is there to establish, not an assertion itself.
	base := mbtest.NewFirstLegalClient()
	tr := manabrew.New("t1", 1, nil)
	answerFirstLegal := func(seat state.PlayerID, d *decision.Decision) {
		seq := headSeqFor(t, ts.reg, "t1", 1)
		v, verr := ts.reg.ViewAtSeat("t1", 1, seq, seat)
		if verr != nil {
			t.Fatalf("ViewAtSeat seat %d: %v", seat, verr)
		}
		pm, perr := tr.Prompt(d, &v)
		if perr != nil {
			t.Fatalf("decision kind %s has no ManaBrew translation: %v", d.Kind, perr)
		}
		msg, aerr := base.Answer(pm)
		if aerr != nil {
			t.Fatalf("first-legal answer for %s: %v", pm.Input.Value.PromptType(), aerr)
		}
		if code, pe := send(t, ts, "s"+strconv.Itoa(int(seat)), msg); code != http.StatusNoContent && pe.Code != mb.CodeStalePrompt {
			t.Fatalf("answering %s (seat %d): status %d, %+v", pm.Input.Value.PromptType(), seat, code, pe)
		}
	}
	answered := map[state.PlayerID]uint64{}
	deadline := time.Now().Add(60 * time.Second)
	over := false
	for !over && time.Now().Before(deadline) {
		progressed := false
		for seat := state.PlayerID(0); seat <= 1; seat++ {
			d, perr := ts.reg.Pending("t1", 1, seat)
			if perr != nil || d == nil || d.Seq == answered[seat] {
				continue
			}
			// Leave seat 1's priority to the stream's queued-concede drain;
			// answering it here would race the transport and could resolve
			// the game as a pass instead of the concession under test.
			if seat == 1 && d.Kind == decision.KPriority {
				continue
			}
			answered[seat] = d.Seq
			answerFirstLegal(seat, d)
			progressed = true
		}
		if v, err := ts.reg.ViewAtSeat("t1", 1, headSeqFor(t, ts.reg, "t1", 1), 0); err == nil && v.Over {
			over = true
		}
		if !progressed {
			time.Sleep(2 * time.Millisecond)
		}
	}
	if !over {
		t.Fatal("the game never reached Over: the queued concede did not end the 2-seat match (fixture broken)")
	}

	// Assertion 1: the stream connected across match end delivered exactly
	// one terminal prompt to seat 0. (This is the delivery whose absence on
	// a later connect is deliberate, not a bug.)
	gd := time.Now().Add(10 * time.Second)
	for c0.gameOvers() == 0 && time.Now().Before(gd) {
		time.Sleep(2 * time.Millisecond)
	}
	if n := c0.gameOvers(); n != 1 {
		t.Fatalf("seat 0's live-across-end stream delivered %d gameOver prompts, want exactly 1", n)
	}

	// Reconnect AFTER match end: a brand-new stream for the same seat.
	// readFrames is the ONLY reader of s2 -- mixing it with s2.next would
	// race two goroutines on one bufio.Reader. The collector records every
	// frame under its mutex, so firstFrame() inspects the reconnect's first
	// frame without a second reader.
	s2 := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s2.close()
	c2 := &syntheticCollector{}
	go readFrames(s2, c2)

	// Assertion 2: the reconnect's first frame is the final state, and that
	// state already carries the game-over flag -- the client is not left
	// without the information; only the redundant terminal prompt is absent.
	var first mb.EngineMessage
	suDeadline := time.Now().Add(10 * time.Second)
	for {
		if f, ok := c2.firstFrame(); ok {
			first = f
			break
		}
		if time.Now().After(suDeadline) {
			t.Fatal("post-over reconnect delivered no frames; the premise of this pin is broken")
		}
		time.Sleep(2 * time.Millisecond)
	}
	su, ok := first.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("post-over reconnect's first frame is %T, want StateUpdate", first.Value)
	}
	if !su.GameView.GameOver {
		t.Fatal("post-over reconnect's state does not carry GameOver=true; the premise of this pin is broken")
	}

	// Assertion 3 (the pin): the reconnect is deliberately state-only. Give
	// the new stream a beat to deliver anything else it might, then assert
	// no terminal prompt arrived.
	settle := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(settle) {
		if c2.gameOvers() > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if n := c2.gameOvers(); n != 0 {
		// Under Answer B (per-connection) this is exactly the assertion that
		// would flip to `want 1`; naming the rule here keeps the next reader
		// from re-filing the report as a defect.
		t.Fatalf("post-over reconnect delivered %d gameOver prompts; the rule is once per SEAT, so a reconnect after Over is deliberately state-only", n)
	}
}
