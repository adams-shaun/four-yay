package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// firstRedrawEvent returns the index of the first redraw event in the log: a
// hand-to-library MoveZone whose Text is "mulligan" (the redraw's own hand
// shuffle-back). Genesis's draw MoveZone events carry no such Text, so this
// finds only a mulligan redraw. It returns -1 when there is none.
func firstRedrawEvent(evs []events.Event) int {
	for i, ev := range evs {
		if ev.Kind == events.MoveZone && ev.Text == "mulligan" {
			return i
		}
	}
	return -1
}

// drivePregameDeclarations runs the pregame round with a policy that mulligans
// whenever a "mulligan" option is offered and otherwise keeps, bottoming the
// demanded cards. It returns the event index of each mulligan declaration's
// DecisionMade (the declaration the pass is supposed to delay) and the total
// count of redraw card moves. It is a manual drive, not playPregame, because
// the assertions need the event index at which each declaration was recorded.
func drivePregameDeclarations(t *testing.T, e *Engine) (declAt []int, redrawMoves int) {
	t.Helper()
	e.Advance()
	for e.G.Turn == 0 && e.Pending() != nil && !e.G.Over {
		d := e.Pending()
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
		isMulligan := false
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			ch := make([]int, 0, d.Min)
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			in.Choices = ch
		} else {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					in.Choices = []int{o.Index}
					isMulligan = true
					break
				}
			}
		}
		// Submit appends its DecisionMade before handle() runs, so the event
		// index captured before Submit is the declaration's own record.
		before := len(e.L.Events)
		if err := e.Submit(in); err != nil {
			t.Fatalf("pregame intent: %v", err)
		}
		if isMulligan {
			declAt = append(declAt, before)
		}
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Text == "mulligan" {
			redrawMoves++
		}
	}
	return declAt, redrawMoves
}

// TestMulliganRedrawsResolveAtPassEnd closes the AGENTS.md row "a mulligan's
// REDRAW resolves immediately, interleaved with the pass's remaining
// declarations". CR 103.4/103.5 has every mulligan in a declaration pass
// happen SIMULTANEOUSLY: all four seats declare, and only then does any
// redraw resolve. This asserts the event ORDER directly -- every pass-1
// mulligan declaration precedes the pass's first redraw event -- which is the
// only observable difference the row describes.
func TestMulliganRedrawsResolveAtPassEnd(t *testing.T) {
	t.Parallel()
	cfg := fourSeatConfig(t, 60, 1) // Mulligans: 1 -> all four mulligan in pass 1
	e := New(cfg)

	// Precondition: genesis dealt and shuffled each seat once, before the
	// pregame round. If this is wrong the log-walk below reads the wrong
	// region and the test would pass vacuously.
	genesisShuffles := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Shuffle {
			genesisShuffles++
		}
	}
	if genesisShuffles != 4 {
		t.Fatalf("genesis shuffled %d seats, want 4", genesisShuffles)
	}

	declAt, redrawMoves := drivePregameDeclarations(t, e)
	if e.G.Turn != 1 {
		t.Fatalf("round did not reach turn 1, turn %d", e.G.Turn)
	}

	// Precondition: four seats each declared a mulligan in pass 1, and each
	// had its full seven-card hand moved back to the library.
	if len(declAt) != 4 {
		t.Fatalf("recorded %d mulligan declarations, want 4 (all four pass-1 seats)", len(declAt))
	}
	if redrawMoves != 4*openingHand {
		t.Fatalf("recorded %d redraw card moves, want %d (four seats x seven cards)",
			redrawMoves, 4*openingHand)
	}
	firstRedraw := firstRedrawEvent(e.L.Events)
	if firstRedraw < 0 {
		t.Fatal("log holds no redraw shuffle-back event")
	}
	lastDecl := declAt[len(declAt)-1]
	// The fix: the pass completes its declarations before any redraw.
	if firstRedraw < lastDecl {
		t.Errorf("first redraw at event %d precedes the pass's last mulligan declaration at %d: "+
			"a seat's redraw is interleaved with the pass's remaining asks, but CR 103.4/103.5 "+
			"resolves all mulligans in a pass simultaneously",
			firstRedraw, lastDecl)
	}
}

// TestMulliganRedrawWaitsForAllSeats is the mixed-pass shape: seat 0 mulligans
// while the other three keep. CR 103.4/103.5 still defers seat 0's redraw until
// the whole pass has declared, so the redraw lands after the last KEEP
// declaration, not between seat 0's declaration and seat 1's ask.
func TestMulliganRedrawWaitsForAllSeats(t *testing.T) {
	t.Parallel()
	cfg := fourSeatConfig(t, 60, 1)
	e := New(cfg)

	var (
		firstDeclAt = -1 // seat 0's mulligan declaration
		lastKeepAt  = -1 // the pass's last keep declaration
	)
	e.Advance()
	for e.G.Turn == 0 && e.Pending() != nil && !e.G.Over {
		d := e.Pending()
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
		isMulligan := false
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			ch := make([]int, 0, d.Min)
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			in.Choices = ch
		} else if d.Player == 0 {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					in.Choices = []int{o.Index}
					isMulligan = true
					break
				}
			}
		}
		before := len(e.L.Events)
		if err := e.Submit(in); err != nil {
			t.Fatalf("pregame intent: %v", err)
		}
		if isMulligan {
			firstDeclAt = before
		} else if d.Player != 0 && len(d.Options) > 0 && d.Options[0].Kind == "keep" {
			lastKeepAt = before
		}
	}
	if e.G.Turn != 1 {
		t.Fatalf("round did not reach turn 1, turn %d", e.G.Turn)
	}
	// Preconditions: seat 0 did mulligan, and the other seats did keep in the
	// pass whose declaration order we are checking.
	if firstDeclAt < 0 {
		t.Fatal("seat 0 never declared a mulligan")
	}
	if lastKeepAt < 0 {
		t.Fatal("no other seat declared a keep in the pass")
	}
	redraws := firstRedrawEvent(e.L.Events)
	if redraws < 0 {
		t.Fatal("log holds no redraw shuffle-back event")
	}
	if redraws < firstDeclAt {
		t.Fatalf("redraw at event %d precedes seat 0's own declaration at %d", redraws, firstDeclAt)
	}
	if redraws < lastKeepAt {
		t.Errorf("seat 0's redraw at event %d precedes a seat's declaration at %d: the pass "+
			"must finish declaring before any redraw resolves (CR 103.4/103.5)",
			redraws, lastKeepAt)
	}
}
