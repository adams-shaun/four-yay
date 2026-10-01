package manabrewhttp

// MBX-4's HTTP leg: the ManaBrew wire is a lossless transport for a real bot
// over the real transport. For each seed the registry hosts TWO tables with
// identical config (same decks, same Seed, same mulligans, no auto-mana) and
// the same production default bot policy:
//
//   - a native table with no human seats: the hosted bots answer every
//     decision in-process;
//   - a ManaBrew wire table whose every seat is human: each seat is a Go
//     client that reads its prompts from host/manabrewhttp's SSE stream,
//     reconstructs the engine's current (view, decision) the same way the
//     transport does, asks the production bot (seat.NewBot at the hosted
//     bot's own seed) for the native intent it would submit, converts that
//     intent to a ManaBrew response with mbtest.ResponseForIntent (MBX-3's
//     inverse helper) and POSTs it to /send.
//
// Both matches must finish with the same event-log chain head. The
// production bot on the wire table sees the same projected view the
// hosted-bot path would hand a plain seat (BoardFromView is pinned equal to
// BoardFromGame by seat's adapter tests), so a head mismatch names a
// transport defect, not a bot defect.
//
// Auto-pass and concede policies are never set by these clients: the bot
// answers every prompt itself, so the stream's silent auto-pass path never
// engages (the native table's bots do not auto-pass either).

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/internal/manabrew/mbtest"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// paritySeedCases is the smoke-sized HTTP set: two seeds per seat count over
// the MB-8 seed scheme (seats*1000+g), Legacy repo decks throughout. Games
// are ~2-5k events each; the whole test stays well under a minute.
var paritySeedCases = []struct {
	seats int
	seeds []uint64
}{
	{2, []uint64{2000, 2001}},
	{4, []uint64{4000, 4001}},
}

// parityDeadline bounds one seed case's whole run (both tables). The hosted
// games finish in well under a second; the deadline exists so a stalled
// client fails the case with its error instead of hanging the suite.
const parityDeadline = 2 * time.Minute

// parityWait polls the registry until match k of the table is finished and
// returns its chain head. Match indexes are 1-based (run starts at t.k+1)
// and Matches lists only the matches that exist, so the search is by
// MatchInfo.Match, not by slice position.
func parityWait(t *testing.T, reg *host.Registry, id host.TableID, k int) string {
	t.Helper()
	deadline := time.Now().Add(parityDeadline)
	for {
		ms, err := reg.Matches(id)
		if err == nil {
			for _, m := range ms {
				if m.Match != k {
					continue
				}
				if m.State == protocol.MatchFinished {
					if m.Head == "" {
						t.Fatalf("table %s match %d finished with an empty chain head", id, k)
					}
					return m.Head
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("table %s match %d did not finish within %v (state %v)", id, k, parityDeadline, ms)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// parityTables builds the loader and adds both tables of one seed case.
func parityTables(t *testing.T, reg *host.Registry, seats int, tableSeed uint64) (native, wire host.TableID) {
	t.Helper()
	names := testutil.LegacyDeckNames()
	deckNames := make([]string, seats)
	for i := 0; i < seats; i++ {
		deckNames[i] = names[(int(tableSeed)+i)%len(names)]
	}
	cfgNative := host.TableConfig{ID: "parity-native", Name: "parity-native", Seats: seats,
		Decks: deckNames, Seed: tableSeed, Mulligans: 0, Spectator: view.Omniscient}
	cfgWire := cfgNative
	cfgWire.ID = "parity-wire"
	cfgWire.Name = "parity-wire"
	cfgWire.Humans = make([]int, seats)
	for i := range cfgWire.Humans {
		cfgWire.Humans[i] = i
	}
	if err := reg.AddTable(cfgNative); err != nil {
		t.Fatalf("AddTable(native): %v", err)
	}
	if err := reg.AddTable(cfgWire); err != nil {
		t.Fatalf("AddTable(wire): %v", err)
	}
	return cfgNative.ID, cfgWire.ID
}

// parityRegistry builds the host registry over the repo decks.
func parityRegistry(t *testing.T) *host.Registry {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	r, err := host.New(host.Options{
		LoadDeck: func(name string) (host.Deck, error) {
			cs, err := testutil.LoadRepoDeck(reg, name)
			if err != nil {
				return host.Deck{}, err
			}
			return host.Deck{Name: name, Cards: cs}, nil
		},
		Tokens:       reg.Tokens,
		NameUniverse: reg.Cards,
		Sleep:        func(time.Duration, <-chan struct{}) {},
		ThinkTimeout: 0, // no caretaker may answer on a bot's behalf
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// TestManaBrewNativeParityHTTP runs the HTTP leg over the seed cases.
func TestManaBrewNativeParityHTTP(t *testing.T) {
	for _, tc := range paritySeedCases {
		for _, tableSeed := range tc.seeds {
			t.Run(fmt.Sprintf("seats%d-seed%d", tc.seats, tableSeed), func(t *testing.T) {
				runParityCase(t, tc.seats, tableSeed)
			})
		}
	}
}

// runParityCase hosts one seed case's two tables and compares heads.
func runParityCase(t *testing.T, seats int, tableSeed uint64) {
	t.Helper()
	r := parityRegistry(t)
	native, wire := parityTables(t, r, seats, tableSeed)

	// Native reference first: the hosted bots play it out unattended.
	if err := r.Start(native); err != nil {
		t.Fatalf("Start(native): %v", err)
	}

	// The wire table's handler: one bearer token per seat, all bound to the
	// wire table (the same token scheme the shipped handler's fixture uses).
	tokens := make(map[string]SeatClaim, seats)
	for i := 0; i < seats; i++ {
		tokens[fmt.Sprintf("parity-s%d", i)] = SeatClaim{Table: wire, Seat: state.PlayerID(i)}
	}
	h, mux := newHandler(r, Options{Seat: func(req *http.Request) (SeatClaim, bool) {
		c, ok := tokens[strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")]
		return c, ok
	}})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if err := r.Start(wire); err != nil {
		t.Fatalf("Start(wire): %v", err)
	}

	// One client goroutine per seat: read the SSE stream, answer prompts
	// with the production bot through the ManaBrew mapping.
	matchSeed := host.MatchSeed(tableSeed, 0)
	errs := &parityErrors{}
	done := make(chan struct{}, seats)
	for i := 0; i < seats; i++ {
		c := &parityClient{
			srv:    srv,
			h:      h,
			table:  wire,
			match:  1,
			token:  fmt.Sprintf("parity-s%d", i),
			player: state.PlayerID(i),
			bot:    seat.NewBot(matchSeed ^ uint64(i+1)),
			errs:   errs,
		}
		go func() {
			c.run()
			done <- struct{}{}
		}()
	}

	// Registry match indexes are 1-based (run starts at t.k+1); a match 0
	// lookup is ErrNotFound, which is what stalled the first draft.
	errs.fail(t, "wire client (early check before the head wait)")
	headNative := parityWait(t, r, native, 1)
	// Wait for every client to see the gameOver prompt and stop.
	timeout := time.After(parityDeadline)
	for i := 0; i < seats; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("wire clients did not finish within the deadline")
		}
	}
	headWire := parityWait(t, r, wire, 1)
	errs.fail(t, "wire client")

	// The registry is per-case; close it now so this case's tables stop
	// rolling matches while the next case runs. Close is idempotent, so the
	// t.Cleanup close at test end is a no-op.
	_ = r.Close()

	if headNative != headWire {
		t.Fatalf("seed %d seats %d: chain heads differ over HTTP: native %s, wire %s (native %d events, wire %d events)",
			tableSeed, seats, headNative, headWire, eventsOf(r, native, 0), eventsOf(r, wire, 0))
	}
}

// eventsOf reports a finished match's event count, for failure messages.
// eventsOf reports a finished match's event count, for failure messages.
// Match indexes are 1-based; the search is by MatchInfo.Match.
func eventsOf(r *host.Registry, id host.TableID, k int) int {
	ms, err := r.Matches(id)
	if err != nil {
		return -1
	}
	for _, m := range ms {
		if m.Match == k {
			return m.Events
		}
	}
	return -1
}

// parityErrors collects the clients' failures; t.Fatal is only legal on the
// test goroutine, so a client goroutine records instead.
type parityErrors struct {
	mu   sync.Mutex
	list []string
}

func (e *parityErrors) add(err error) {
	e.mu.Lock()
	e.list = append(e.list, err.Error())
	e.mu.Unlock()
}

// fail turns any recorded client error into a test failure, prefix-tagged.
func (e *parityErrors) fail(t *testing.T, tag string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.list {
		t.Errorf("%s: %s", tag, s)
	}
}

// parityClient is one seat's ManaBrew client: SSE reader + production bot.
type parityClient struct {
	srv    *httptest.Server
	h      *handler
	table  host.TableID
	match  int
	token  string
	player state.PlayerID
	bot    *seat.Bot
	errs   *parityErrors

	ctx     context.Context
	cancel  context.CancelFunc
	lastSeq int64
}

// run reads the seat's SSE stream until the gameOver prompt or an error, and
// answers every ordinary prompt through the bot + ManaBrew mapping. The
// connection is bounded by parityDeadline; a keep-alive ping or any frame
// wakes the answer path.
func (c *parityClient) run() {
	c.ctx, c.cancel = context.WithTimeout(context.Background(), parityDeadline)
	defer c.cancel()
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet,
		c.srv.URL+fmt.Sprintf("/%s/matches/%d/stream", c.table, c.match), nil)
	if err != nil {
		c.errs.add(err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.errs.add(fmt.Errorf("stream: %w", err))
		return
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if c.ctx.Err() != nil {
				return // the deadline closed the stream; the waiter owns the failure
			}
			c.errs.add(fmt.Errorf("reading SSE: %w", err))
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
			c.errs.add(fmt.Errorf("decoding frame: %w", derr))
			return
		}
		switch v := m.Value.(type) {
		case mb.ErrorMessage:
			c.errs.add(fmt.Errorf("server pushed an error frame: %s: %s", v.Error.Code, v.Error.Message))
			return
		case mb.PromptMessage:
			switch v.Input.Value.(type) {
			case mb.GameOverInput:
				return // terminal prompt seen: this seat is done
			case mb.RevealCardsInput:
				if aerr := c.ack(v, mb.RevealCardsAcknowledged{}); aerr != nil {
					c.errs.add(aerr)
					return
				}
			case mb.DiceRolledInput:
				if aerr := c.ack(v, mb.DiceRolledAcknowledged{}); aerr != nil {
					c.errs.add(aerr)
					return
				}
			default:
				if aerr := c.answer(v); aerr != nil {
					c.errs.add(aerr)
					return
				}
			}
		}
	}
}

// ack POSTs a synthetic prompt's acknowledgement (204, engine log untouched).
func (c *parityClient) ack(pm mb.PromptMessage, out mb.PromptOutputValue) error {
	msg := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: pm.Input.Value.PromptType(), Output: mb.PromptOutputData{Value: out}}}}
	raw, err := mb.Encode(msg)
	if err != nil {
		return fmt.Errorf("encoding ack: %w", err)
	}
	return c.post(raw)
}

// answer answers one ordinary prompt with the production bot's native
// intent, expressed on the ManaBrew wire.
func (c *parityClient) answer(pm mb.PromptMessage) error {
	// The same reconstruction the transport itself does at /send time: the
	// seat's current (view, decision) at the log head.
	v, d, err := c.h.pollView(c.table, c.match, c.player)
	if err != nil {
		return fmt.Errorf("poll view: %w", err)
	}
	if d == nil {
		return nil // stale frame; the next wake-up carries the current prompt
	}
	if int64(d.Seq) != pm.PromptID {
		return nil // the frame names a decision that has since been answered
	}
	if int64(d.Seq) == c.lastSeq {
		return nil // already answered this decision
	}
	in, err := c.bot.Decide(c.ctx, v, *d)
	if err != nil {
		return fmt.Errorf("bot decide (seq %d %s): %w", d.Seq, d.Kind, err)
	}
	pending := &manabrew.Pending{Prompt: pm, Decision: d, View: v}
	msg, err := mbtest.ResponseForIntent(pending, in)
	if err != nil {
		return fmt.Errorf("the bot's native answer to the %s ask (seq %d) is not expressible on the ManaBrew wire: %w", d.Kind, d.Seq, err)
	}
	raw, err := mb.Encode(msg)
	if err != nil {
		return fmt.Errorf("encoding response: %w", err)
	}
	if perr := c.post(raw); perr != nil {
		return perr
	}
	c.lastSeq = int64(d.Seq)
	return nil
}

// post sends one client message to /send and requires 204.
func (c *parityClient) post(raw []byte) error {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodPost,
		c.srv.URL+fmt.Sprintf("/%s/matches/%d/send", c.table, c.match), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /send: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("POST /send: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
