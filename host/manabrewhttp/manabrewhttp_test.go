package manabrewhttp

// MB-10's Done-means fixtures: a tiny 4-seat table (bots fill every non-human
// slot) served over the ManaBrew transport through an httptest.Server. Every
// test drives the real host.Registry — no fakes — because the ticket's own
// review risk is goroutine lifetimes, SSE flushing and lock ordering against
// the live match loop, which only a real engine can exercise honestly.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/testutil"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// fixtureDeckSize caps every deck the shared loader serves, the same
// bounded-game trick host/httpapi's own fixtures use (rest_test.go): short
// decks keep a bot-vs-bot game finite without changing what is exercised.
const fixtureDeckSize = 12

func loader(t *testing.T) func(string) (host.Deck, error) {
	t.Helper()
	names, decks := testutil.SampleDecks(t, 4)
	by := map[string][]*cards.Card{}
	for i, n := range names {
		by[n] = decks[i][:fixtureDeckSize]
	}
	return func(n string) (host.Deck, error) {
		cs, ok := by[n]
		if !ok {
			return host.Deck{}, host.ErrNotFound
		}
		return host.Deck{Name: n, Cards: cs}, nil
	}
}

// testServer bundles a live table behind the ManaBrew handler. token "s<N>"
// claims table t1, seat N, for every seat in humans.
type testServer struct {
	h   *handler
	srv *httptest.Server
	reg *host.Registry
}

func newTestServer(t *testing.T, humans []int, thinkTimeout time.Duration, mulligans int) *testServer {
	t.Helper()
	r, err := host.New(host.Options{LoadDeck: loader(t), Sleep: func(time.Duration, <-chan struct{}) {}, ThinkTimeout: thinkTimeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := host.TableConfig{ID: "t1", Name: "T1", Seats: 4, Decks: []string{"a", "b", "c", "d"}, Seed: 7,
		Spectator: view.Omniscient, Humans: humans, Mulligans: mulligans, AutoMana: true}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	byToken := map[string]SeatClaim{}
	for _, s := range humans {
		byToken["s"+strconv.Itoa(s)] = SeatClaim{Table: "t1", Seat: state.PlayerID(s)}
	}
	h, mux := newHandler(r, Options{Seat: func(req *http.Request) (SeatClaim, bool) {
		tok := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		c, ok := byToken[tok]
		return c, ok
	}})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testServer{h: h, srv: srv, reg: r}
}

// waitForPending blocks until seat has a decision parked, returning it. It is
// a readiness probe on the registry itself (never the HTTP layer), matching
// host/httpapi's own parkedSeat helper.
func waitForPending(t *testing.T, r *host.Registry, table host.TableID, k int, seat state.PlayerID) *decision.Decision {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if d, err := r.Pending(table, k, seat); err == nil {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("seat %d never parked on a decision", seat)
		}
		time.Sleep(time.Millisecond)
	}
}

// sendTo posts one ClientToServerMessage to path with token's bearer claim,
// returning the status and, for a non-204 reply, the decoded ProtocolError.
func sendTo(t *testing.T, srv *httptest.Server, token, path string, msg mb.ClientMessage) (int, mb.ProtocolError) {
	t.Helper()
	raw, err := mb.Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var pe mb.ProtocolError
	if resp.StatusCode != http.StatusNoContent && len(body) > 0 {
		_ = json.Unmarshal(body, &pe)
	}
	return resp.StatusCode, pe
}

func send(t *testing.T, ts *testServer, token string, msg mb.ClientMessage) (int, mb.ProtocolError) {
	t.Helper()
	return sendTo(t, ts.srv, token, "/t1/matches/1/send", msg)
}

// pollState is a client for GET .../state: the first element must be a
// StateUpdate (§5.2's contract), returned decoded.
func pollState(t *testing.T, ts *testServer, token string) mb.GameViewDto {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+"/t1/matches/1/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state poll: %d", resp.StatusCode)
	}
	var raws []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raws); err != nil {
		t.Fatal(err)
	}
	if len(raws) == 0 {
		t.Fatal("state poll returned no messages")
	}
	var msg mb.EngineMessage
	if _, err := mb.Decode(raws[0], &msg); err != nil {
		t.Fatal(err)
	}
	su, ok := msg.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("first poll message is %T, want StateUpdate", msg.Value)
	}
	return su.GameView
}

// sseStream is an open GET .../stream connection.
type sseStream struct {
	resp   *http.Response
	br     *bufio.Reader
	cancel context.CancelFunc
}

func openStream(t *testing.T, srv *httptest.Server, path, token string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return &sseStream{resp: resp, br: bufio.NewReader(resp.Body), cancel: cancel}
}

func (s *sseStream) close() {
	s.cancel()
	_ = s.resp.Body.Close()
}

// next reads the next "data: ..." SSE line, skipping blank lines and
// keep-alive comments, and decodes it as an EngineMessage.
func (s *sseStream) next(t *testing.T) mb.EngineMessage {
	t.Helper()
	for {
		line, err := s.br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var msg mb.EngineMessage
		if _, err := mb.Decode([]byte(data), &msg); err != nil {
			t.Fatalf("decoding SSE message %q: %v", data, err)
		}
		return msg
	}
}

// TestStreamSendsStateThenPrompt pins §5.2's connect contract: the very
// first two frames are a StateUpdate carrying this match's gameId, then the
// PromptMessage for the decision already parked on the claimed seat, with a
// matching promptId and decidingPlayerId.
func TestStreamSendsStateThenPrompt(t *testing.T) {
	ts := newTestServer(t, []int{0}, 0, 0)
	d := waitForPending(t, ts.reg, "t1", 1, 0)

	s := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s.close()

	m1 := s.next(t)
	su, ok := m1.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("first message is %T, want StateUpdate", m1.Value)
	}
	if su.GameView.GameID != "t1/1" {
		t.Fatalf("gameId = %q, want \"t1/1\"", su.GameView.GameID)
	}

	m2 := s.next(t)
	pm, ok := m2.Value.(mb.PromptMessage)
	if !ok {
		t.Fatalf("second message is %T, want PromptMessage", m2.Value)
	}
	if pm.DecidingPlayerID != "player-0" {
		t.Fatalf("decidingPlayerId = %q, want player-0", pm.DecidingPlayerID)
	}
	if pm.PromptID != int64(d.Seq) {
		t.Fatalf("promptId = %d, want %d (the parked decision's Seq)", pm.PromptID, d.Seq)
	}
	if _, ok := pm.Input.Value.(*mb.ChooseActionInput); !ok {
		t.Fatalf("priority prompt input is %T, want *ChooseActionInput", pm.Input.Value)
	}
}

// TestOtherSeatTokenForbidden pins the claim boundary MB-10 owns: a token
// bound to a different table is refused before any match state is
// consulted, on every route, and an unresolved token is 401. There is no
// ?seat= to cross here at all — the seat comes only from the claim
// (invariant 5) — so the analogous httpapi audit becomes a table-mismatch
// check on this package's own three routes.
func TestOtherSeatTokenForbidden(t *testing.T) {
	ts := newTestServer(t, []int{0}, 0, 0)
	waitForPending(t, ts.reg, "t1", 1, 0)

	get := func(path, token string) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := get("/t2/matches/1/stream", "s0"); code != http.StatusForbidden {
		t.Fatalf("cross-table stream: %d, want 403", code)
	}
	if code := get("/t2/matches/1/state", "s0"); code != http.StatusForbidden {
		t.Fatalf("cross-table state: %d, want 403", code)
	}
	concede := mb.ClientMessage{Value: mb.ClientDirective{Directive: mb.DirectiveInput{Type: "concede"}}}
	if code, _ := sendTo(t, ts.srv, "s0", "/t2/matches/1/send", concede); code != http.StatusForbidden {
		t.Fatalf("cross-table send: %d, want 403", code)
	}

	if code := get("/t1/matches/1/state", "nobody"); code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d, want 401", code)
	}
	if code := get("/t1/matches/1/state", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d, want 401", code)
	}

	// The claim's own table still works: the boundary rejects only the
	// mismatch, never the legitimate request.
	if code := get("/t1/matches/1/state", "s0"); code != http.StatusOK {
		t.Fatalf("table-A state with table-A claim: %d, want 200", code)
	}
}

// assertNoForeignHand is §8 item 7's core property: viewer's own gameView
// never carries a visible card in foreign's hand zone entry, only a count.
func assertNoForeignHand(t *testing.T, gv mb.GameViewDto, viewer, foreign state.PlayerID) {
	t.Helper()
	fid := "player-" + strconv.Itoa(int(foreign))
	found := false
	for _, z := range gv.Zones {
		if z.Zone != mb.ZoneHand || z.OwnerID != fid {
			continue
		}
		found = true
		if len(z.Cards) != 0 {
			t.Fatalf("viewer %d sees %d card entries in seat %d's hand zone (want 0)", viewer, len(z.Cards), foreign)
		}
		if z.Count == 0 {
			t.Fatalf("viewer %d sees a zero-count hand for seat %d", viewer, foreign)
		}
	}
	if !found {
		t.Fatalf("viewer %d's gameView carries no hand zone for seat %d at all", viewer, foreign)
	}
}

// TestNoForeignHandEverVisible pins §8 item 7's privacy fence over both the
// poll route and the stream's own first frame, in both directions: neither
// human seat's gameView ever exposes the other's hand as visible cards.
func TestNoForeignHandEverVisible(t *testing.T) {
	ts := newTestServer(t, []int{0, 1}, 0, 0)
	waitForPending(t, ts.reg, "t1", 1, 0) // readiness: hands are dealt and the match is live

	assertNoForeignHand(t, pollState(t, ts, "s0"), 0, 1)
	assertNoForeignHand(t, pollState(t, ts, "s1"), 1, 0)

	s := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s.close()
	m := s.next(t)
	su, ok := m.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("first stream message is %T, want StateUpdate", m.Value)
	}
	assertNoForeignHand(t, su.GameView, 0, 1)
}

// TestStaleAfterCaretaker pins the caretaker/stale contract (§5.2): once a
// HumanSeat's ThinkTimeout fires and its caretaker turns the decision, a
// client answering the old promptId gets stalePrompt, and the prompt it
// names stays exactly what it was — nothing in the caretaker path lets a
// late answer land on a decision that already moved on.
func TestStaleAfterCaretaker(t *testing.T) {
	ts := newTestServer(t, []int{0}, 20*time.Millisecond, 0)
	d := waitForPending(t, ts.reg, "t1", 1, 0)
	staleSeq := d.Seq

	deadline := time.Now().Add(10 * time.Second)
	for {
		nd, err := ts.reg.Pending("t1", 1, 0)
		if err == nil && nd.Seq != staleSeq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the caretaker never advanced the parked decision")
		}
		time.Sleep(time.Millisecond)
	}

	resp := mb.ClientMessage{Value: mb.ClientResponse{PromptID: int64(staleSeq), Action: mb.PromptOutput{
		Type: "chooseAction", Output: mb.PromptOutputData{Value: mb.PassOutput{}}}}}
	code, pe := send(t, ts, "s0", resp)
	if code != http.StatusConflict {
		t.Fatalf("late response after caretaker: status %d, want 409", code)
	}
	if pe.Code != mb.CodeStalePrompt {
		t.Fatalf("late response after caretaker: code %q, want stalePrompt", pe.Code)
	}
}

// TestConcedeQueuedToPriority pins G-2: a concede directive sent while the
// seat's open decision is NOT priority (here, the London mulligan ask) is
// queued rather than rejected or silently dropped, and the transport itself
// — not the client — answers the seat's very next priority decision with
// its concede option, purely from a live stream's own wake-up loop.
func TestConcedeQueuedToPriority(t *testing.T) {
	ts := newTestServer(t, []int{1}, 0, 1) // seat 1 human, one London mulligan allowed
	d := waitForPending(t, ts.reg, "t1", 1, 1)
	if d.Kind != decision.KMulligan {
		t.Fatalf("first decision for seat 1 is %s, want mulligan (Mulligans=1)", d.Kind)
	}

	// A live stream is what actually drains the queued concede (the
	// transport's auto-answer runs inside the stream's wake-up loop, never
	// on the send path alone).
	s := openStream(t, ts.srv, "/t1/matches/1/stream", "s1")
	defer s.close()
	_ = s.next(t) // state
	_ = s.next(t) // the mulligan prompt

	concede := mb.ClientMessage{Value: mb.ClientDirective{Directive: mb.DirectiveInput{Type: "concede"}}}
	code, pe := send(t, ts, "s1", concede)
	if code != http.StatusNoContent {
		t.Fatalf("queuing a concede behind a mulligan ask: status %d body %+v, want 204", code, pe)
	}

	// Keep the hand: this is the ordinary mulligan answer, unrelated to the
	// queued concession, and it is what lets the match reach seat 1's first
	// PRIORITY decision, where the queued concession is due.
	keep := mb.ClientMessage{Value: mb.ClientResponse{PromptID: int64(d.Seq), Action: mb.PromptOutput{
		Type: "mulligan", Output: mb.PromptOutputData{Value: mb.MulliganDecision{Keep: true}}}}}
	if code, pe := send(t, ts, "s1", keep); code != http.StatusNoContent {
		t.Fatalf("keeping the opening hand: status %d body %+v, want 204", code, pe)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		v, err := ts.reg.ViewAtSeat("t1", 1, headSeqFor(t, ts.reg, "t1", 1), 1)
		if err == nil {
			for _, p := range v.Players {
				if p.ID == 1 && p.Lost {
					return // the queued concession fired, unaided by any further client action
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("seat 1's queued concession never turned into a loss")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func headSeqFor(t *testing.T, r *host.Registry, table host.TableID, k int) uint64 {
	t.Helper()
	seq, err := headSeq(r, table, k)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

// TestReconnectIdempotent pins §5.2's reconnect contract: a fresh stream
// after a disconnect, with nothing answered in between, gets the identical
// promptId — derived from the decision's own Seq, never a per-connection
// counter — so a client that drops and reopens the connection can tell it
// is looking at the SAME open decision rather than guessing from content.
func TestReconnectIdempotent(t *testing.T) {
	ts := newTestServer(t, []int{0}, 0, 0)
	waitForPending(t, ts.reg, "t1", 1, 0)

	s1 := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	_ = s1.next(t) // state
	pm1, ok := s1.next(t).Value.(mb.PromptMessage)
	if !ok {
		t.Fatal("first connect's second message is not a PromptMessage")
	}
	s1.close()

	s2 := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s2.close()
	_ = s2.next(t) // state
	pm2, ok := s2.next(t).Value.(mb.PromptMessage)
	if !ok {
		t.Fatal("reconnect's second message is not a PromptMessage")
	}

	if pm1.PromptID != pm2.PromptID {
		t.Fatalf("reconnect promptId changed: %d -> %d", pm1.PromptID, pm2.PromptID)
	}
	if pm1.DecidingPlayerID != pm2.DecidingPlayerID {
		t.Fatalf("reconnect decidingPlayerId changed: %q -> %q", pm1.DecidingPlayerID, pm2.DecidingPlayerID)
	}
}

// Narrower cases (restoreSnapshot on a two-human table, a malformed body's
// exact 400 shape, the write-timeout path) are left to a follow-up: the six
// Done-means tests above are this ticket's gate.
