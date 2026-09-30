package manabrewhttp

// MBX-2's Done-means test: a real 2-seat game (Goblin Guide's mandatory
// "defending player reveals the top card of their library" on every attack;
// Krazy Kow's "at the beginning of your upkeep, roll a six-sided die") over
// the ManaBrew transport proves, end to end through the real HTTP layer and
// the real engine:
//
//  1. the synthetic prompts (revealCards, diceRolled) are delivered over SSE
//     to both seats, in the disjoint negative id space;
//  2. an ack POSTed to /send is accepted (204) and changes nothing in the
//     engine log (the head event count is identical around it -- an
//     intent-shaped anything would move it);
//  3. never acking anything (seat 1's policy acks nothing) does not stall
//     play: the game still runs to the end;
//  4. a finished game delivers exactly one gameOver prompt per seat, and the
//     once-per-seat latch means a later poll of the same seat sees no second
//     one.
//
// The decks are real corpus cards (the same GPL-text rule as everywhere
// else: they are looked up from the gitignored .cards/ cache, never written
// out inline), so the reveal Note and the per-die/batch dice Notes are the
// ones effects/cardflow.go and effects/dice.go actually emit. Both decks are
// half their key card, so the key card is (with the pinned Seed 7, near-
// certainly and in this run observably) in each opening hand, and the
// driver's cast policy keeps exactly one copy of each creature on the
// battlefield at a time -- one Guide attacking means ~2 damage a turn from
// combat, which puts the Guide lethal well after the Kow's first upkeep
// roll, and a roll of 1 (Kow sacrifices itself and pings everyone for 3)
// recasts from the copy-heavy deck rather than ending the rolling.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/internal/manabrew/mbtest"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

const (
	guideDeckName = "gg-guide"
	kowDeckName   = "kk-kow"
)

// syntheticCollector accumulates one seat's stream frames in the background.
// The reading goroutine may outlive the stream, so every read is mutex-
// guarded and the goroutine never calls t.Fatal (that is only legal on the
// test goroutine): it records the first read failure instead.
type syntheticCollector struct {
	mu   sync.Mutex
	msgs []mb.EngineMessage
	err  error
	done bool
	seen int
}

// syntheticOf returns the synthetic PromptMessages seen so far and how many
// are NEW since the caller last looked (so the driver can pick one to ack).
func (c *syntheticCollector) syntheticOf() (cur []mb.PromptMessage, fresh int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur = make([]mb.PromptMessage, 0, len(c.msgs))
	for _, m := range c.msgs {
		pm, ok := m.Value.(mb.PromptMessage)
		if !ok {
			continue
		}
		switch pm.Input.Value.(type) {
		case mb.RevealCardsInput, mb.DiceRolledInput:
			cur = append(cur, pm)
		}
	}
	fresh = len(cur) - c.seen
	if fresh < 0 {
		fresh = 0
	}
	c.seen = len(cur)
	return cur, fresh
}

// gameOvers counts the terminal prompts seen so far.
func (c *syntheticCollector) gameOvers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if pm, ok := m.Value.(mb.PromptMessage); ok {
			if _, isGO := pm.Input.Value.(mb.GameOverInput); isGO {
				n++
			}
		}
	}
	return n
}

// failed reports whether the reader goroutine died, with its error.
func (c *syntheticCollector) failed() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err != nil, c.err
}

// readFrames is the collector's loop: the same "data: ..." line discipline
// sseStream.next enforces, minus the t.Fatal (a non-test goroutine may not
// call it).
func readFrames(s *sseStream, c *syntheticCollector) {
	for {
		line, err := s.br.ReadString('\n')
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.done = true
			c.mu.Unlock()
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var m mb.EngineMessage
		if _, derr := mb.Decode([]byte(data), &m); derr != nil {
			c.mu.Lock()
			c.err = derr
			c.done = true
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.msgs = append(c.msgs, m)
		c.mu.Unlock()
	}
}

// syntheticServer is newTestServer with the two specific decks above and a
// 2-seat table whose every seat is human (no bot answer can move the engine
// log while the driver is parked, so the head-stability assertion around an
// ack is exact).
func syntheticServer(t *testing.T) *testServer {
	t.Helper()
	corpus := testutil.CorpusRegistry(t)
	guide, ok := corpus.Lookup("Goblin Guide")
	if !ok {
		t.Fatal("corpus has no Goblin Guide; the fixture cannot build")
	}
	kow, ok := corpus.Lookup("Krazy Kow")
	if !ok {
		t.Fatal("corpus has no Krazy Kow; the fixture cannot build")
	}
	land, ok := corpus.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus has no Mountain; the fixture cannot build")
	}
	decks := map[string][]*cards.Card{
		guideDeckName: append(repeatCard(guide, 30), repeatCard(land, 30)...),
		kowDeckName:   append(repeatCard(kow, 30), repeatCard(land, 30)...),
	}
	loader := func(n string) (host.Deck, error) {
		cs, ok := decks[n]
		if !ok {
			return host.Deck{}, fmt.Errorf("no deck named %q", n)
		}
		return host.Deck{Name: n, Cards: cs}, nil
	}
	r, err := host.New(host.Options{LoadDeck: loader, Sleep: func(time.Duration, <-chan struct{}) {}, ThinkTimeout: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := host.TableConfig{ID: "t1", Name: "T1", Seats: 2, Decks: []string{guideDeckName, kowDeckName}, Seed: 7,
		Spectator: view.Omniscient, Humans: []int{0, 1}, Mulligans: 0, AutoMana: true}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	byToken := map[string]SeatClaim{"s0": {Table: "t1", Seat: 0}, "s1": {Table: "t1", Seat: 1}}
	h, mux := newHandler(r, Options{Seat: func(req *http.Request) (SeatClaim, bool) {
		tok := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		c, ok := byToken[tok]
		return c, ok
	}})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testServer{h: h, srv: srv, reg: r}
}

func repeatCard(c *cards.Card, n int) []*cards.Card {
	out := make([]*cards.Card, n)
	for i := range out {
		out[i] = c
	}
	return out
}

// synthDriver answers one seat's prompts with a deterministic policy: cast
// its creature when the hand has one and the battlefield has none yet, else
// play a land, else pass; attack with everything (seat 0); never block
// (seat 1); every other kind delegates to mbtest's first-legal client
// (deterministic, and it answers only from the prompt's own advertised
// legality). It NEVER acks a synthetic prompt -- only the test's explicit
// ack ritual does -- which is what assertion 3 exercises.
type synthDriver struct {
	token    string
	castName string
	base     *mbtest.MockClient
}

// actFor picks the priority action: the driver creature's cast if it is
// offered (the hand card named castName that the action's CardID names, and
// no copy of that creature is on the battlefield yet), else a land drop,
// else pass.
func actFor(t *testing.T, v *view.View, in mb.ChooseActionInput, castName string) mb.PromptOutputValue {
	t.Helper()
	me := &v.Players[v.Viewer]
	want := ""
	placed := false
	for _, c := range me.Hand {
		if c.Name == castName {
			want = fmt.Sprintf("o%d", c.ID)
		}
	}
	for _, c := range me.Battlefield {
		if c.Name == castName {
			placed = true
		}
	}
	if want != "" && !placed {
		for _, a := range in.Actions {
			if a.Type == "cast" && a.CardID == want {
				return mb.ActOutput{ActionID: a.ID}
			}
		}
	}
	for _, a := range in.Actions {
		if a.Type == "cast" && a.Mode == "play" {
			return mb.ActOutput{ActionID: a.ID}
		}
	}
	return mb.PassOutput{}
}

// answer translates d for its seat and POSTs one answer to /send.
func (drv *synthDriver) answer(t *testing.T, ts *testServer, tr *manabrew.Translator, k int, d *decision.Decision) {
	t.Helper()
	seq, err := headSeq(ts.reg, "t1", k)
	if err != nil {
		t.Fatalf("headSeq: %v", err)
	}
	v, err := ts.reg.ViewAtSeat("t1", k, seq, d.Player)
	if err != nil {
		t.Fatalf("ViewAtSeat: %v", err)
	}
	pm, perr := tr.Prompt(d, &v)
	if perr != nil {
		t.Fatalf("decision kind %s has no ManaBrew translation: %v", d.Kind, perr)
	}
	var out mb.PromptOutputValue
	switch in := pm.Input.Value.(type) {
	case mb.ChooseActionInput:
		out = actFor(t, &v, in, drv.castName)
	case mb.ChooseAttackersInput:
		var assignments []mb.AttackerAssignment
		for _, a := range in.Attackers {
			if len(a.ValidTargetIDs) == 0 {
				continue
			}
			assignments = append(assignments, mb.AttackerAssignment{AttackerID: a.AttackerID, TargetID: a.ValidTargetIDs[0]})
		}
		out = mb.DeclareAttackersDecision{Assignments: assignments}
	case mb.ChooseBlockersInput:
		out = mb.DeclareBlockersDecision{}
	default:
		msg, aerr := drv.base.Answer(pm)
		if aerr != nil {
			t.Fatalf("first-legal fallback for %s: %v", pm.Input.Value.PromptType(), aerr)
		}
		if code, pe := send(t, ts, drv.token, msg); code != 204 {
			t.Fatalf("answering %s: status %d, %+v", pm.Input.Value.PromptType(), code, pe)
		}
		return
	}
	resp := mb.ClientMessage{Value: mb.ClientResponse{PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: pm.Input.Value.PromptType(), Output: mb.PromptOutputData{Value: out}}}}
	if code, pe := send(t, ts, drv.token, resp); code != 204 {
		t.Fatalf("answering %s: status %d, %+v", pm.Input.Value.PromptType(), code, pe)
	}
}

// syntheticAckBody builds the response payload for a synthetic prompt's ack.
func syntheticAckBody(pm mb.PromptMessage) mb.ClientMessage {
	typ := pm.Input.Value.PromptType()
	var out mb.PromptOutputValue
	switch typ {
	case "diceRolled":
		out = mb.DiceRolledAcknowledged{}
	default:
		out = mb.RevealCardsAcknowledged{}
	}
	return mb.ClientMessage{Value: mb.ClientResponse{PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: typ, Output: mb.PromptOutputData{Value: out}}}}
}

// parkedAny waits until the match is live and some human seat is parked on
// a decision -- the game really started.
func parkedAny(t *testing.T, r *host.Registry) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if ms, err := r.Matches("t1"); err == nil && len(ms) == 1 && ms[0].State == protocol.MatchLive {
			for _, seat := range []state.PlayerID{0, 1} {
				if d, perr := r.Pending("t1", 1, seat); perr == nil && d != nil {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the fixture game never started: no seat parked on the genesis ask")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSyntheticPromptsOverStream is the brief's whole Done-means list in one
// deterministic game.
func TestSyntheticPromptsOverStream(t *testing.T) {
	ts := syntheticServer(t)
	tr := manabrew.New("t1", 1, nil)

	// Preconditions the assertions below depend on (a vacuous setup must
	// fail loudly): the match is live, some human seat is parked on the
	// game's first ask (CR 103.1's toss-winner starting-player choice -- the
	// only ask the genesis poses; the loser waits), and the deck assignment
	// is the one this policy expects (host assigns table Decks to seats in
	// reverse order: seat 0 got Decks[1]). The reveal and dice counts
	// themselves are the proof the two fixture cards entered play, so
	// nothing stronger is asserted here.
	parkedAny(t, ts.reg)
	ms, merr := ts.reg.Matches("t1")
	if merr != nil || len(ms) != 1 {
		t.Fatalf("fixture precondition broken: %d matches for t1 (%v)", len(ms), merr)
	}
	if ms[0].Seats[0].DeckID != kowDeckName || ms[0].Seats[1].DeckID != guideDeckName {
		t.Fatalf("fixture precondition broken: seat deck assignment moved: %+v (want %s, %s)", ms[0].Seats, kowDeckName, guideDeckName)
	}

	c0 := &syntheticCollector{}
	c1 := &syntheticCollector{}
	s0 := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s0.close()
	s1 := openStream(t, ts.srv, "/t1/matches/1/stream", "s1")
	defer s1.close()
	go readFrames(s0, c0)
	go readFrames(s1, c1)

	// seat 0 owns the Kows (the dice) and seat 1 the Guide (the reveals);
	// host assigns table Decks to seats in reverse order (asserted above).
	drivers := map[state.PlayerID]*synthDriver{
		0: {token: "s0", castName: "Krazy Kow", base: mbtest.NewFirstLegalClient()},
		1: {token: "s1", castName: "Goblin Guide", base: mbtest.NewFirstLegalClient()},
	}
	answered := map[state.PlayerID]uint64{}
	var acked mb.PromptMessage
	ackPicked, ackDone := false, false
	reveals := map[state.PlayerID]int{}
	dices := map[state.PlayerID]int{}

	deadline := time.Now().Add(90 * time.Second)
	over := false
	for !over && time.Now().Before(deadline) {
		// Drive every parked decision exactly once.
		progressed := false
		for seat := state.PlayerID(0); seat <= 1; seat++ {
			d, perr := ts.reg.Pending("t1", 1, seat)
			if perr != nil || d == nil || d.Seq == answered[seat] {
				continue
			}
			answered[seat] = d.Seq
			drivers[seat].answer(t, ts, tr, 1, d)
			progressed = true
		}

		// Absorb the streams' new synthetic prompts.
		for seat, c := range map[state.PlayerID]*syntheticCollector{0: c0, 1: c1} {
			syns, fresh := c.syntheticOf()
			for _, pm := range syns[len(syns)-fresh:] {
				if pm.PromptID >= 0 {
					t.Fatalf("seat %d's synthetic %s carries promptId %d, outside the disjoint negative space", seat, pm.Input.Value.PromptType(), pm.PromptID)
				}
				switch pm.Input.Value.(type) {
				case mb.RevealCardsInput:
					reveals[seat]++
				case mb.DiceRolledInput:
					dices[seat]++
				}
			}
			if seat == 0 && !ackPicked && fresh > 0 {
				acked = syns[len(syns)-1]
				ackPicked = true
			}
		}

		// The ack ritual (assertion 2): only while a decision is parked, so
		// the single engine goroutine is provably idle and the head cannot
		// move for any other reason.
		if ackPicked && !ackDone {
			parked := false
			for seat := state.PlayerID(0); seat <= 1; seat++ {
				if d, perr := ts.reg.Pending("t1", 1, seat); perr == nil && d != nil {
					parked = true
				}
			}
			if parked {
				before := headSeqFor(t, ts.reg, "t1", 1)
				if code, pe := send(t, ts, "s0", syntheticAckBody(acked)); code != 204 {
					t.Fatalf("synthetic ack (id %d, %s): status %d, %+v", acked.PromptID, acked.Input.Value.PromptType(), code, pe)
				}
				if after := headSeqFor(t, ts.reg, "t1", 1); after != before {
					t.Fatalf("an accepted synthetic ack moved the engine log: head %d -> %d", before, after)
				}
				// A duplicate ack of the same id must fall through to the
				// ordinary response path and read as stale (the prompt is no
				// longer open).
				if code, pe := send(t, ts, "s0", syntheticAckBody(acked)); code == 204 {
					t.Fatal("a second ack of the same synthetic prompt was accepted; the ack-once gate moved")
				} else if pe.Code != mb.CodeStalePrompt {
					t.Fatalf("duplicate synthetic ack: status %d code %q, want stalePrompt", code, pe.Code)
				}
				ackDone = true
			}
		}

		// Game over? (Read straight off the registry -- an HTTP /state poll
		// here would consume this seat's terminal prompt out from under its
		// stream, which is exactly the once-per-seat latch under test.)
		seq, herr := headSeq(ts.reg, "t1", 1)
		if herr == nil {
			if v, verr := ts.reg.ViewAtSeat("t1", 1, seq, 0); verr == nil && v.Over {
				over = true
			}
		}
		if !progressed {
			time.Sleep(2 * time.Millisecond)
		}
	}

	if !over {
		t.Fatalf("the game never ended; not-acking the synthetic prompts stalled play (assertion 3 failed): reveals=%v dices=%v", reveals, dices)
	}
	if !ackDone {
		t.Fatalf("no synthetic prompt was ever acked; the ack ritual never found a parked decision (fixture broken): reveals=%v dices=%v ackPicked=%v", reveals, dices, ackPicked)
	}

	// Assertion 1: both prompt kinds reached both seats over SSE, every id
	// inside the disjoint negative space.
	for seat, c := range map[state.PlayerID]*syntheticCollector{0: c0, 1: c1} {
		if failed, ferr := c.failed(); failed {
			t.Fatalf("seat %d's stream broke: %v", seat, ferr)
		}
		if reveals[seat] == 0 {
			t.Fatalf("seat %d never saw a revealCards synthetic prompt over SSE", seat)
		}
		if dices[seat] == 0 {
			t.Fatalf("seat %d never saw a diceRolled synthetic prompt over SSE", seat)
		}
	}

	// Assertion 4: exactly ONE terminal prompt per seat, and the once-per-
	// seat latch means a later poll of the same seat carries no second one.
	for seat, c := range map[state.PlayerID]*syntheticCollector{0: c0, 1: c1} {
		gDeadline := time.Now().Add(10 * time.Second)
		for c.gameOvers() == 0 && time.Now().Before(gDeadline) && !c.done1() {
			time.Sleep(2 * time.Millisecond)
		}
		if n := c.gameOvers(); n != 1 {
			t.Fatalf("seat %d's stream delivered %d gameOver prompts, want exactly 1", seat, n)
		}
	}
	for _, tok := range []string{"s0", "s1"} {
		for _, raw := range rawStatePoll(t, ts, tok) {
			if !strings.Contains(string(raw), "\"gameOver\"") {
				continue
			}
			var m mb.EngineMessage
			if _, derr := mb.Decode(raw, &m); derr != nil {
				t.Fatal(derr)
			}
			if pm, ok := m.Value.(mb.PromptMessage); ok {
				if _, isGO := pm.Input.Value.(mb.GameOverInput); isGO {
					t.Fatalf("%s's post-over state poll carried a second gameOver prompt: %s", tok, raw)
				}
			}
		}
	}
}

// done1 reports whether the collector's reader goroutine has finished.
func (c *syntheticCollector) done1() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}
