package host

// The rv2a mulligan-starter contract at the web boundary. The seated client
// renders the mulligan prompt from the seat-scoped view the product fetches —
// Registry.ViewAtSeat at head, whose `decision` field is the engine's own
// answerable Decision (M2b-4) — so a prompt change is only proven if the bytes
// that reach SeatPanel come from the real engine, not from a hand-written
// fixture the panel test could pass even with keepMulliganPrompt reverted.
//
// TestTheMulliganStarterFixtureIsTheEngineSOwnWireBytes drives a real
// two-human table (duplicate decks "a"/"a", PlayerNames Alice/Bob, table seed
// 1 — with the round on, seat 1 wins the CR 103.1 toss and is asked first),
// captures the exact ViewAtSeat-at-head JSON twice (the first keep/mulligan
// ask, and the spent-allowance keep-only re-ask after one mulligan), and
// byte-compares both with the committed fixture at
// web/src/fixtures/mulligan-starter.json, which the SeatPanel web test imports
// and renders. Regenerate with GORGE_UPDATE_WEB_FIXTURES=1.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// mulliganFixturePath is the committed fixture the web test imports, relative
// to this package's directory (go test runs with cwd set to it).
const mulliganFixturePath = "../web/src/fixtures/mulligan-starter.json"

// mulliganStarterFixture is the wire shape a seated client holds at the
// pending mulligan: the live match's seat list (from the snapshot body, the
// same protocol.SeatInfo slice the match start / snapshot frames carry) and
// the seat's own ViewAtSeat-at-head view for each captured ask.
type mulliganStarterFixture struct {
	Seats    []protocol.SeatInfo `json:"seats"`
	FirstAsk view.View           `json:"first_ask"`
	SpentAsk view.View           `json:"spent_ask"`
}

// waitNextPending polls Pending until the seat's parked decision is no longer
// the one named by prev: after a SubmitIntent the slot clears asynchronously
// (the match goroutine consumes the intent, submits to the engine and installs
// the next park), so the first Pending that still succeeds may be the OLD
// decision — waiting for a different Seq is the only race-free shape. A
// missing match (ErrNotFound: the table goroutine has not installed a current
// match yet, e.g. immediately after Start) is retried like an empty slot; only
// any other error is fatal.
func waitNextPending(t *testing.T, r *Registry, id TableID, k int, p state.PlayerID, prev uint64) *decision.Decision {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		d, err := r.Pending(id, k, p)
		if err == nil && d.Seq != prev {
			return d
		}
		if err != nil && !strings.Contains(err.Error(), "no decision pending") && !errors.Is(err, ErrNotFound) {
			t.Fatalf("pending for player %d: %v", p, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending for player %d never advanced past seq %d", p, prev)
		}
		time.Sleep(time.Millisecond)
	}
}

// starterFixtureTable is the duplicate-deck two-human table: both decks are
// named "a", so only PlayerName Alice/Bob can tell a seated human who plays
// first, and table seed 1 starts seat 1 (Bob) with the round on.
func starterFixtureTable(id TableID) TableConfig {
	return TableConfig{ID: id, Name: "starter-fixture", Seats: 2, Decks: []string{"a", "a"},
		Seed: 1, Pace: 0, Spectator: view.Omniscient, Perpetual: false, Mulligans: 1,
		Humans: []int{0, 1}, PlayerNames: []string{"Alice", "Bob"}}
}

func TestTheMulliganStarterFixtureIsTheEngineSOwnWireBytes(t *testing.T) {
	t.Parallel()
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(starterFixtureTable("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}

	// CR 103.1 first: the hosted game opens with the toss winner's
	// winner-chooses ask, whose answer is Bob naming himself (the seat's own
	// option). Only then does the London round ask seat 1 first.
	d0 := waitPending(t, r, "t1", 1, 1)
	if d0.Kind != decision.KStartingPlayer || d0.Player != 1 {
		t.Fatalf("first pending = seat %d kind %v, want seat 1's CR 103.1 toss-choice ask", d0.Player, d0.Kind)
	}
	self := -1
	for _, o := range d0.Options {
		if o.Player == 1 {
			self = o.Index
		}
	}
	if self < 0 {
		t.Fatalf("toss ask offers no option for seat 1: %+v", d0.Options)
	}
	if err := r.SubmitIntent("t1", 1, 1, decision.Intent{Seq: d0.Seq, Player: 1, Choices: []int{self}}); err != nil {
		t.Fatalf("SubmitIntent(toss choice): %v", err)
	}

	// First ask: the toss winner's keep/mulligan. The preconditions keep the
	// fixture honest — if the engine ever stops asking seat 1 first here, or
	// the prompt stops naming the display player, the fixture would silently
	// pin the wrong thing — so fail loudly instead.
	d1 := waitNextPending(t, r, "t1", 1, 1, d0.Seq)
	if d1.Player != 1 || d1.Kind != decision.KMulligan {
		t.Fatalf("first pending = seat %d kind %v, want seat 1's mulligan ask", d1.Player, d1.Kind)
	}
	if !strings.Contains(d1.Prompt, "Bob plays first.") {
		t.Fatalf("first ask prompt %q does not name the display player", d1.Prompt)
	}
	firstView := r.seatViewAtHead(t, "t1", 1, 1)

	// Take the mulligan. CR 103.5 completes this declaration pass by asking
	// Alice before Bob's spent-allowance re-ask begins the next pass. The web
	// fixture still captures Bob's two shapes, but drives that intervening
	// keep through the real host rather than preserving the old repeat-seat
	// bug in a fixture.
	if err := r.SubmitIntent("t1", 1, 1, decision.Intent{Seq: d1.Seq, Player: 1, Choices: []int{1}}); err != nil {
		t.Fatalf("SubmitIntent(mulligan): %v", err)
	}
	intervening := waitPending(t, r, "t1", 1, 0)
	if intervening.Kind != decision.KMulligan || intervening.Player != 0 {
		t.Fatalf("after Bob's mulligan, pending = %+v, want Alice's same-pass declaration", intervening)
	}
	if err := r.SubmitIntent("t1", 1, 0, decision.Intent{Seq: intervening.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("SubmitIntent(Alice keep): %v", err)
	}
	d2 := waitNextPending(t, r, "t1", 1, 1, intervening.Seq)
	if d2.Kind != decision.KMulligan || len(d2.Options) != 1 || d2.Options[0].Kind != "keep" {
		t.Fatalf("after the mulligan, pending = %+v, want the spent-allowance keep-only re-ask", d2)
	}
	if !strings.Contains(d2.Prompt, "Bob plays first.") {
		t.Fatalf("re-ask prompt %q does not name the display player", d2.Prompt)
	}

	fix := mulliganStarterFixture{
		Seats:    r.snapshotBodySeats("t1", 1),
		FirstAsk: firstView,
		SpentAsk: r.seatViewAtHead(t, "t1", 1, 1),
	}
	raw, err := json.MarshalIndent(fix, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')

	if os.Getenv("GORGE_UPDATE_WEB_FIXTURES") == "1" {
		if err := os.WriteFile(mulliganFixturePath, raw, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return
	}
	committed, err := os.ReadFile(mulliganFixturePath)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture %s is missing: run GORGE_UPDATE_WEB_FIXTURES=1 go test ./host/ -run TestTheMulliganStarterFixtureIsTheEngineSOwnWireBytes to write it", mulliganFixturePath)
	} else if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, raw) {
		t.Fatalf("committed fixture %s does not match the engine's own wire bytes; "+
			"if the mulligan prompt or the seat view legitimately changed, regenerate with "+
			"GORGE_UPDATE_WEB_FIXTURES=1 go test ./host/ -run TestTheMulliganStarterFixtureIsTheEngineSOwnWireBytes "+
			"and review the diff (the web SeatPanel tests render exactly this file)", mulliganFixturePath)
	}
}

// seatViewAtHead is the seat-scoped view the seated client fetches (GET view
// at head with ?seat=): Registry.ViewAtSeat at the live head, which is where
// the engine's pending Decision rides on the wire.
func (r *Registry) seatViewAtHead(t *testing.T, id TableID, k int, seat state.PlayerID) view.View {
	t.Helper()
	tt, m, err := r.lookup(id, k)
	if err != nil {
		t.Fatalf("lookup %s/%d: %v", id, k, err)
	}
	v, err := r.ViewAtSeat(id, k, head(m), seat)
	if err != nil {
		t.Fatalf("ViewAtSeat(%s, %d, head, %d) on table %+v: %v", id, k, seat, tt.cfg.ID, err)
	}
	return v
}

// snapshotBodySeats is the live match's seat list, the Seats field the
// snapshot frame carries (fanout.go's snapshotBody), read the same way.
func (r *Registry) snapshotBodySeats(id TableID, k int) []protocol.SeatInfo {
	tt, m, err := r.lookup(id, k)
	if err != nil {
		return nil
	}
	return append([]protocol.SeatInfo{}, r.snapshotBody(tt, m).Seats...)
}
