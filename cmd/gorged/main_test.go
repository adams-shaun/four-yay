package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/httpapi"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/view"
)

func TestServesTablesOverHTTP(t *testing.T) {
	t.Parallel()
	testutil.CorpusRegistry(t) // Skips when .cards/ is absent
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{cards: "../../.cards", decks: "../../internal/testutil/decks", tables: 1, seats: 2, pace: 0,
		cooldown: 0, dir: t.TempDir(), spectator: "omniscient", seed: 1, perpetual: false, vsbot: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, ln) }()
	url := "http://" + ln.Addr().String()
	var tables []protocol.TableInfo
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url + "/api/tables")
		if err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&tables)
			resp.Body.Close()
			if len(tables) == 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tables served: %v %+v", err, tables)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if tables[0].ID != "t1" || tables[0].Seats != 2 || tables[0].Spectator != "omniscient" {
		t.Fatalf("%+v", tables[0])
	}
	decksResp, err := http.Get(url + "/api/decks")
	if err != nil {
		t.Fatal(err)
	}
	var catalogue []httpapi.DeckInfo
	if err := json.NewDecoder(decksResp.Body).Decode(&catalogue); err != nil {
		decksResp.Body.Close()
		t.Fatal(err)
	}
	decksResp.Body.Close()
	loadedIDs, err := deckFiles(cfg.decks)
	if err != nil {
		t.Fatal(err)
	}
	if decksResp.StatusCode != http.StatusOK || len(catalogue) != len(loadedIDs) {
		t.Fatalf("deck catalogue status %d entries %d, loaded %d", decksResp.StatusCode, len(catalogue), len(loadedIDs))
	}
	byID := make(map[string]httpapi.DeckInfo, len(catalogue))
	for _, d := range catalogue {
		byID[d.ID] = d
	}
	if got := byID["death-n-taxes"]; got.Name != "Death & Taxes" || got.Format != "constructed" || got.Archetype != "hatebears" {
		t.Fatalf("constructed deck metadata %+v", got)
	}
	if got := byID["foundations-calling-all-angels"]; got.Format != "commander" || got.Commander != "Giada, Font of Hope" {
		t.Fatalf("commander deck metadata %+v", got)
	}
	if got := byID["hearthhull-worldseed-landfall"]; got.Name != "Hearthhull, the Worldseed Landfall" || got.Format != "commander" || got.Archetype != "landfall lands" || got.Commander != "Lord Windgrace" {
		t.Fatalf("Hearthhull commander deck metadata %+v", got)
	}
	if got := byID["valgavoth-endless-punishment"]; got.Name != "Valgavoth, Harrower of Souls — Endless Punishment" || got.Format != "commander" || got.Commander != "Valgavoth, Harrower of Souls" || got.Archetype != "rakdos-punisher" {
		t.Fatalf("Valgavoth deck metadata %+v", got)
	}
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"format":"constructed","human_deck":"missing"}`, `unknown human deck "missing"`},
		{`{"format":"constructed","bot_deck":"foundations-calling-all-angels"}`, `belongs to commander, not requested format constructed`},
	} {
		gameResp, err := http.Post(url+"/api/games", "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		var body protocol.ErrorBody
		if err := json.NewDecoder(gameResp.Body).Decode(&body); err != nil {
			gameResp.Body.Close()
			t.Fatal(err)
		}
		gameResp.Body.Close()
		if gameResp.StatusCode != http.StatusBadRequest || !strings.Contains(body.Message, tc.want) {
			t.Fatalf("invalid deck status %d body %+v, want %q", gameResp.StatusCode, body, tc.want)
		}
	}
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Fatalf("/: %d", resp.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestHostThreadsCorpusTokens is the FL-40 required proof that gorged
// populates host.Options.Tokens from the opened corpus. A hosted table that
// does not get the token corpus mints no tokens and every token card is
// silently dead, so opening the corpus but dropping its Tokens onto the
// floor is exactly the failure this test is meant to catch.
func TestHostThreadsCorpusTokens(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg := config{cards: "../../.cards", decks: "../../internal/testutil/decks", dir: t.TempDir()}
	opts := cfg.hostOptions(reg, deckLoader(reg, cfg.decks))
	if len(opts.Tokens) == 0 {
		t.Fatalf("host.Options.Tokens empty: token corpus not threaded from %s", cfg.cards)
	}
	if reg.Tokens == nil {
		t.Fatalf("corpus returned a nil Tokens map")
	}
}

func TestDeckDirectoryListingIsSortedAndComplete(t *testing.T) {
	names, err := deckFiles("../../internal/testutil/decks")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 2 {
		t.Fatalf("expected at least 2 decks, got %v", names)
	}
	// Sortedness is the contract the host's table listing depends on; WHICH
	// deck sorts first is not pinned -- it changes whenever a deck is added
	// (avengers-assemble displaced death-n-taxes once already).
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("deck listing not strictly sorted at %q after %q: %v", names[i], names[i-1], names)
		}
	}
	// The historical deck set must all still be present (the 12 Legacy
	// constructed decks, the foundations-* precons, and the decks added
	// since), and the listing must stay complete in that subset sense: the
	// directory is what the host serves for tables. Decks added later join
	// the listing without breaking this; a RENAMED or removed deck fails here.
	want := []string{
		"avengers-assemble",
		"death-n-taxes", "dimir-tempo", "foundations-calling-all-angels",
		"foundations-keen-engineering", "foundations-reign-of-dragons",
		"foundations-tramplesaurus-rex", "foundations-wretched-ranks",
		"mono-black-aggro", "mono-blue-tempo", "mono-green-stompy",
		"mono-red-goblins", "the-epic-storm", "tron", "ur-delver",
		"uw-control", "uw-tempo",
	}
	for _, w := range want {
		have := false
		for _, n := range names {
			if n == w {
				have = true
				break
			}
		}
		if !have {
			t.Fatalf("deck listing missing %q: %v", w, names)
		}
	}
}

// startServe boots serve on a random loopback port for the given config and
// returns its base URL, cancellation and error channel, mirroring the
// net.Listen("tcp", "127.0.0.1:0") pattern of TestServesTablesOverHTTP —
// no test below binds a fixed port. Every human-seat test here drives a
// real server with the corpus, because the seat-scoped routes are the whole
// point of the task; the corpus guard is the same one the existing tests
// use.
func startServe(t *testing.T, c config) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	testutil.CorpusRegistry(t) // Skips when .cards/ is absent
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if c.cards == "" {
		c.cards = "../../.cards"
	}
	if c.decks == "" {
		c.decks = "../../internal/testutil/decks"
	}
	if c.dir == "" {
		c.dir = t.TempDir()
	}
	if c.spectator == "" {
		c.spectator = "omniscient"
	}
	if c.seed == 0 {
		c.seed = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, c, ln) }()
	return "http://" + ln.Addr().String(), cancel, done
}

// waitTables polls /api/tables until n tables are listed, so a request
// never races AddTable/StartAll on a freshly booted server.
func waitTables(t *testing.T, url string, n int) {
	t.Helper()
	var tables []protocol.TableInfo
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url + "/api/tables")
		if err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&tables)
			resp.Body.Close()
			if len(tables) == n {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %d tables served: %v %+v", n, err, tables)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// decisionOnce fetches one pending decision for a seat: status 200 with the
// decision, or the error status with a zero decision; a transport error is
// surfaced as status 0.
func decisionOnce(t *testing.T, pendingURL, seat, tok string) (decision.Decision, int) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s?seat=%s&token=%s", pendingURL, seat, tok))
	if err != nil {
		return decision.Decision{}, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decision.Decision{}, resp.StatusCode
	}
	var d decision.Decision
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatalf("pending decode: %v", err)
	}
	return d, http.StatusOK
}

// waitDecision polls pending until a decision is actually offered.
func waitDecision(t *testing.T, pendingURL, seat, tok string) decision.Decision {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		d, status := decisionOnce(t, pendingURL, seat, tok)
		if status == http.StatusOK {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("no decision offered to seat %s: last status %d", seat, status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// fastPollDecision is waitDecision with a 5ms poll: the mulligan-allowance
// test waits on ~10 decisions across two created games, and at the shared
// 50ms granularity the poll sleep alone is a visible slice of the package's
// test-time budget. Same harness (decisionOnce), just a tighter tick.
func fastPollDecision(t *testing.T, pendingURL, seat, tok string) decision.Decision {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		d, status := decisionOnce(t, pendingURL, seat, tok)
		if status == http.StatusOK {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("no decision offered to seat %s: last status %d", seat, status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// seatViewOver reports whether match 1's seat-0 view is over, used as the
// "seat view changed" fallback when a game runs to completion instead of
// asking the seat again.
func seatViewOver(t *testing.T, url string) bool {
	t.Helper()
	resp, err := http.Get(url + "/api/tables/t1/matches/1/view?seat=0&token=tok")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v view.View
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return false
	}
	return v.Over
}

// expectStatus polls path until the server answers with the wanted status
// (the only status this endpoint can answer once the server is up), then
// returns the decoded error body. Retrying makes the request immune to the
// server still booting.
func expectStatus(t *testing.T, url, path string, want int) protocol.ErrorBody {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url + path)
		if err == nil {
			var eb protocol.ErrorBody
			_ = json.NewDecoder(resp.Body).Decode(&eb)
			resp.Body.Close()
			if resp.StatusCode == want {
				return eb
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: never answered %d (err %v)", path, want, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestHumanSeatTakesADecisionEndToEnd is the point of the task: a human
// seat answers the one decision a real person meets and the game visibly
// advances. serve runs with the -perpetual default (true) to prove R-E3-2
// forces t1 single-shot anyway, pace 0 so the wait is about the wire, and a
// fixed -seat-token so the test holds a seat without scraping stderr.
func TestHumanSeatTakesADecisionEndToEnd(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, humansRaw: "0", pace: 0, perpetual: true, seatToken: "tok"})
	defer cancel()
	waitTables(t, url, 1)

	pendingURL := url + "/api/tables/t1/matches/1/pending"
	first := waitDecision(t, pendingURL, "0", "tok")

	// Answer with the first Min legal options of the decision actually on
	// offer — never a specific card name or option label; this test is
	// about the wire, not the game, and any Validate-accepted answer moves
	// the engine on.
	in := decision.Intent{Seq: first.Seq, Player: first.Player, Choices: make([]int, first.Min)}
	for i := range in.Choices {
		in.Choices[i] = i
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/api/tables/t1/matches/1/intent", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	// The intent carries the token in the Authorization: Bearer header (the
	// reads use ?token=): R-E3-3 reads the header first, then the query.
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("intent answered %d, want 204", resp.StatusCode)
	}

	// The game must have advanced: the next decision asked of the seat has
	// a strictly higher Seq (the engine stamps Seq from the event count,
	// which only grows). If the match has run to completion instead — the
	// seat is never asked again — a view that is over is that same advance;
	// only a game frozen at seq %d is a failure.
	advanced := false
	deadline := time.Now().Add(15 * time.Second)
	for {
		d, status := decisionOnce(t, pendingURL, "0", "tok")
		if status == http.StatusOK && d.Seq > first.Seq {
			advanced = true
			break
		}
		if time.Now().After(deadline) {
			if seatViewOver(t, url) {
				advanced = true
				break
			}
			t.Fatalf("game did not advance past seq %d (last status %d)", first.Seq, status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !advanced {
		t.Fatal("game did not advance")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestTheHumanSeatIsAskedToMulligan is the product half of M2e-5: with
// -mulligans 1 (the flag default), the London round runs and the human
// seat receives its keep/mulligan choice after any CR 103.1 toss choice.
// Answering "keep" must move the match past the round.
func TestTheHumanSeatIsAskedToMulligan(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, humansRaw: "0", pace: 0, perpetual: true, seatToken: "tok", mulligans: 1})
	defer cancel()
	waitTables(t, url, 1)

	pendingURL := url + "/api/tables/t1/matches/1/pending"
	first := afterTossChoice(t, url, "t1", pendingURL, "tok", waitDecision(t, pendingURL, "0", "tok"))
	if first.Kind != decision.KMulligan {
		t.Fatalf("first decision offered to the human seat is %q, want mulligan", first.Kind)
	}

	// Answer "keep" — the first Min option of the keep/mulligan ask — and
	// confirm the match moves past the round: the next decision asked of the
	// seat has a strictly higher Seq (the engine stamps Seq from the event
	// count, which only grows).
	in := decision.Intent{Seq: first.Seq, Player: first.Player, Choices: make([]int, first.Min)}
	for i := range in.Choices {
		in.Choices[i] = i
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/api/tables/t1/matches/1/intent", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mulligan answer returned %d, want 204", resp.StatusCode)
	}

	advanced := false
	deadline := time.Now().Add(15 * time.Second)
	for {
		d, status := decisionOnce(t, pendingURL, "0", "tok")
		if status == http.StatusOK && d.Seq > first.Seq {
			advanced = true
			break
		}
		if time.Now().After(deadline) {
			if seatViewOver(t, url) {
				advanced = true
				break
			}
			t.Fatalf("game did not advance past the mulligan (last status %d)", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !advanced {
		t.Fatal("game did not advance past the mulligan")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// playDeadline bounds a full human-driven match. The loop's assertions
// never depend on it — the seat view reporting over is the only exit — but
// a wedged drive must fail with what it got, not hang the suite (R-E6-3).
const playDeadline = 60 * time.Second

// playMatchToCompletion drives the whole product: the human seat answers
// every decision of the match through the HTTP surface — the exact URL
// shapes web/src/lib/api.ts builds — until the seat view reports over. It
// returns the census of decision kinds met (one count per kind), the answer
// count, and whether any posted choice named a concede option.
//
// The driver answers each decision with its first Min option indices: never
// a card name, never a label, never the last option. Validate only checks
// range/count/duplicates, and with Min == Max == taken (mulligan bottoming,
// trigger order) the first Min indices are a legal permutation, so this
// prefix is a legal answer to every kind the engine can ask — a
// rules-ignorant client, exactly like the seat panel.
//
// It answers each decision exactly once. A 204 on the intent means only that
// the parked seat received the answer, not that the match goroutine has woken
// and cleared its pending slot (host.HumanSeat.submit returns as soon as the
// send lands; the slot is cleared by the parked await's defer — pinned by
// host's TestPendingStillReportsAnAnsweredDecisionBeforeAwaitWakes). So a GET
// issued immediately after that 204 can hand back the decision just answered.
// Re-posting that echo is a 409 whenever the seat has meanwhile moved on to
// the next decision or has no slot at all, which is the load-dependent flake
// this driver used to fail with. answeredSeq is the exact fence: rules'
// Engine.ask stamps d.Seq with the log length and then appends its own
// DecisionAsk event, so every decision's Seq is strictly greater than the
// previous one's and "Seq <= answeredSeq" can only be the echo, never a live
// decision.
func playMatchToCompletion(t *testing.T, url, seat, tok string) (census map[decision.Kind]int, answers int, conceded bool) {
	t.Helper()
	pendingURL := url + "/api/tables/t1/matches/1/pending"
	intentURL := url + "/api/tables/t1/matches/1/intent"
	census = make(map[decision.Kind]int)
	deadline := time.Now().Add(playDeadline)
	answered := false
	var answeredSeq uint64
	for {
		d, status := decisionOnce(t, pendingURL, seat, tok)
		// The echo of the decision this driver just answered: poll again
		// rather than re-post it. The window is the instant between the
		// intent's 204 and the match goroutine waking, so the retry is short
		// — a 50ms wait per answer would eat the whole playDeadline on a
		// loaded box — and the deadline still governs.
		if status == http.StatusOK && answered && d.Seq <= answeredSeq {
			if time.Now().After(deadline) {
				t.Fatalf("still echoing the answered decision (seq %d) after %s: %d answers, census %v",
					d.Seq, playDeadline, answers, census)
			}
			time.Sleep(time.Millisecond)
			continue
		}
		if status == http.StatusOK {
			in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: make([]int, d.Min)}
			for i := range in.Choices {
				in.Choices[i] = i
			}
			// R-E6-1 (FL-101 in test code): the final option on every
			// priority decision is "concede". Report it if a choice ever
			// names one — the driver must never pick it, and the census is
			// the context that says what the game asked instead.
			for _, c := range in.Choices {
				if d.Options[c].Kind == "concede" {
					conceded = true
				}
			}
			body, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodPost, intentURL, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("intent post: %v", err)
			}
			rejection, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// The intent is built from a decision this driver has not
			// answered before, and the engine parks on it, so a rejection
			// here is a real bug — a wrong player or a bad index — never a
			// transient. The rejection body names the reason, which is the
			// difference between a stale seq and a malformed answer.
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("intent for %s seq %d answered %d, want 204: %s",
					d.Kind, d.Seq, resp.StatusCode, bytes.TrimSpace(rejection))
			}
			census[d.Kind]++
			answers++
			answered, answeredSeq = true, d.Seq
			continue // the engine is waiting on this seat: poll again now
		}
		// No decision parked: the other seat is deciding, or the match is
		// over. The seat view is the oracle for over; only a stalled drive
		// may hit the deadline, and it must report the census it got.
		if seatViewOver(t, url) {
			return census, answers, conceded
		}
		if time.Now().After(deadline) {
			t.Fatalf("match did not reach over within %s: %d answers, census %v (last pending status %d)",
				playDeadline, answers, census, status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestAHumanPlaysAMatchToCompletion is the Ruling FL-102 gate: it plays a
// complete two-seat match through the HTTP surface as a human seat and
// asserts the census of decision kinds a real player actually meets. Every
// other gate measures the engine against itself; this one plays the product
// (the route that found the London mulligan unreachable from gorged). The
// assertions are reachability, not exact counts: a seed, deck or engine
// change that moves a count is not a failure; one that removes a kind a
// player can meet is.
func TestAHumanPlaysAMatchToCompletion(t *testing.T) {
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, humansRaw: "0", pace: 0, perpetual: true, seatToken: "tok", mulligans: 1})
	defer cancel()
	waitTables(t, url, 1)

	// The driver returns only once the seat view reports over, so "reached
	// over" is the loop's contract, asserted by construction.
	census, answers, conceded := playMatchToCompletion(t, url, "0", "tok")

	// A real game, not a stall: the mulligan keep alone is not a game.
	if answers < 2 {
		t.Fatalf("match ended after %d answer(s), census %v: a real game asks the seat more than once", answers, census)
	}
	// R-E6-1: choosing the last option would have conceded on the first
	// priority decision and "passed" a one-answer game.
	if conceded {
		t.Fatalf("a posted choice named a concede option, census %v: the driver must pick 0..Min-1, never the last option", census)
	}
	// The mulligan reachability is the point of the test (finding bb):
	// with -mulligans 1 the keep ask is the first decision of the game, and
	// a failure here means served games silently skip the London round.
	if census[decision.KMulligan] == 0 {
		t.Fatalf("the human met no mulligan decision, census %v: the London round did not reach the served game — is Config.Mulligans threaded into the table config?", census)
	}
	if census[decision.KPriority] == 0 {
		t.Fatalf("the human met no priority decision, census %v", census)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestHumanSeatRefusesTheOtherSeat: a token minted for seat 0 is refused
// when the request names seat 1 — the claim≠requested comparison of M2e-2,
// and the whole reason the resolver cannot be a rubber stamp (R-E3-3).
//
// The startServe-based tests in this file are fully independent — own
// listener on a random port, own tempdirs, no package-global mutated (the
// newServeArtCache seam is only ever swapped by the sequential prewarm
// test) — so the eight of them that drive a served server directly run in
// parallel. That is not decoration: the package's wall-time budget
// (TEST_HISTORY.md, budget_s) was already at zero headroom before the
// serve-level prewarm test was added, and serializing these buys nothing.
func TestHumanSeatRefusesTheOtherSeat(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, humansRaw: "0", pace: 0, seatToken: "tok"})
	defer cancel()
	waitTables(t, url, 1)

	path := "/api/tables/t1/matches/1/pending?seat=1&token=tok"
	eb := expectStatus(t, url, path, http.StatusForbidden)
	if eb.Code != "forbidden" {
		t.Fatalf("code %q, want forbidden", eb.Code)
	}
	// The body names both seats — the claim's seat and the requested one.
	if !strings.Contains(eb.Message, "0") || !strings.Contains(eb.Message, "1") ||
		!strings.Contains(eb.Message, "not the requested seat") {
		t.Fatalf("message %q", eb.Message)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestHumanSeatRefusesAMissingToken: ?seat=0 with no token is a 401 — the
// resolver declines, and claimSeat refuses like an Authorize failure.
func TestHumanSeatRefusesAMissingToken(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, humansRaw: "0", pace: 0, seatToken: "tok"})
	defer cancel()
	waitTables(t, url, 1)

	eb := expectStatus(t, url, "/api/tables/t1/matches/1/pending?seat=0", http.StatusUnauthorized)
	if eb.Code != "unauthorized" {
		t.Fatalf("code %q, want unauthorized", eb.Code)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestHumanSeatRefusesAnUnknownToken: a token nobody was minted is a 401 —
// unknown tokens are refused exactly like absent ones (R-E3-3).
func TestHumanSeatRefusesAnUnknownToken(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, humansRaw: "0", pace: 0, seatToken: "tok"})
	defer cancel()
	waitTables(t, url, 1)

	eb := expectStatus(t, url, "/api/tables/t1/matches/1/pending?seat=0&token=wrong", http.StatusUnauthorized)
	if eb.Code != "unauthorized" {
		t.Fatalf("code %q, want unauthorized", eb.Code)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestBotTablesStayPerpetualBesideAHumanTable: with -tables 2 -humans 0 the
// -perpetual default (true) still applies to t2, while t1 is forced
// single-shot for its human (R-E3-2). Asserted on the TableConfig state
// directly, not by waiting out matches.
func TestBotTablesStayPerpetualBesideAHumanTable(t *testing.T) {
	c := config{seats: 2, tables: 2, seed: 1, perpetual: true, humansRaw: "0"}
	if err := c.applyHumans(); err != nil {
		t.Fatal(err)
	}
	cfgs := c.tableConfigs(nil, nil, view.Omniscient)
	if len(cfgs) != 2 {
		t.Fatalf("want 2 table configs, got %d", len(cfgs))
	}
	t1, t2 := cfgs[0], cfgs[1]
	if t1.ID != "t1" || t1.Perpetual || len(t1.Humans) != 1 || t1.Humans[0] != 0 {
		t.Fatalf("t1 must be a single-shot human table: %+v", t1)
	}
	if t2.ID != "t2" || !t2.Perpetual || t2.Humans != nil {
		t.Fatalf("t2 must stay a perpetual bot table: %+v", t2)
	}
}

// TestNoHumansIsSpectatorOnly pins that without -humans the server behaves
// byte-identically to before the flag existed: a request naming a seat is
// refused 403, spectator-only, because Options.Seat stays nil.
func TestNoHumansIsSpectatorOnly(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, pace: 0, perpetual: true})
	defer cancel()
	waitTables(t, url, 1)

	eb := expectStatus(t, url, "/api/tables/t1/matches/1/pending?seat=0", http.StatusForbidden)
	if eb.Code != "forbidden" || !strings.Contains(eb.Message, "spectator-only") {
		t.Fatalf("code %q message %q", eb.Code, eb.Message)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// --- Task m39: gorged runs Commander tables ---------------------------------

// TestFormatFlagParsing pins the -format flag's two shapes: a single name
// makes every table that format, and a comma-separated list is cycled over
// the tables so one server runs constructed and Commander games side by
// side. A name that is neither is a startup error — before any table is
// added — and the zero config (no flag at all) stays all-constructed,
// byte-identical to every pre-m39 gorged.
func TestFormatFlagParsing(t *testing.T) {
	// Zero config: applyFormats defaults to constructed.
	var c config
	if err := c.applyFormats(); err != nil {
		t.Fatal(err)
	}
	if len(c.formats) != 1 || c.formats[0] != host.FormatConstructed {
		t.Fatalf("zero config formats %v, want [constructed]", c.formats)
	}

	// A single commander makes every table a Commander table.
	c = config{formatsRaw: "commander", tables: 4}
	if err := c.applyFormats(); err != nil {
		t.Fatal(err)
	}
	cfgs := c.tableConfigs(nil, nil, view.Omniscient)
	if len(cfgs) != 4 {
		t.Fatalf("want 4 configs, got %d", len(cfgs))
	}
	for i, cfg := range cfgs {
		if cfg.Format != host.FormatCommander {
			t.Fatalf("t%d format %v, want commander", i+1, cfg.Format)
		}
	}

	// A two-name list cycles: t1 commander, t2 constructed, t3 commander.
	c = config{formatsRaw: "commander,constructed", tables: 3}
	if err := c.applyFormats(); err != nil {
		t.Fatal(err)
	}
	cfgs = c.tableConfigs(nil, nil, view.Omniscient)
	want := []host.Format{host.FormatCommander, host.FormatConstructed, host.FormatCommander}
	for i, w := range want {
		if cfgs[i].Format != w {
			t.Fatalf("t%d format %v, want %v", i+1, cfgs[i].Format, w)
		}
	}

	// Each table deals the deck pool of its format: the commander pool to
	// commander tables, the constructed pool to the rest.
	cfgs = c.tableConfigs([]string{"c1", "c2"}, []string{"x", "y", "z"}, view.Omniscient)
	if len(cfgs[0].Decks) != 2 || cfgs[0].Decks[0] != "c1" {
		t.Fatalf("commander table decks %v, want the commander pool", cfgs[0].Decks)
	}
	if len(cfgs[1].Decks) != 3 || cfgs[1].Decks[0] != "x" {
		t.Fatalf("constructed table decks %v, want the constructed pool", cfgs[1].Decks)
	}

	// A junk name fails at apply time, naming the value.
	c = config{formatsRaw: "constructed,commander,blitz"}
	if err := c.applyFormats(); err == nil || !strings.Contains(err.Error(), "blitz") {
		t.Fatalf("junk format accepted or unnamed: %v", err)
	}
}

// commanderView polls a served table's view — the head when seqs is blank,
// the given sequence otherwise — and returns the first decoded one that
// reports a match, so a request never races the server still booting or
// the first match still starting.
func commanderView(t *testing.T, url, seqs string) view.View {
	t.Helper()
	query := ""
	if seqs != "" {
		query = "?seq=" + seqs
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(url + "/api/tables/t1/matches/1/view" + query)
		if err == nil && resp.StatusCode == http.StatusOK {
			var v view.View
			if err := json.NewDecoder(resp.Body).Decode(&v); err == nil && len(v.Players) > 0 {
				resp.Body.Close()
				return v
			}
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("no commander view served (err %v)", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestCommanderTableServesACommanderGameEndToEnd is the delivery proof for
// the wire: a real gorged server on a Commander table with the five m38
// decks serves a game whose seats start at 40 life (CR 903.6) with their
// commanders in the command zone and a full roster, and whose logged
// commander_casts climb past zero — the m36 command-zone panel on a table
// that will actually exist. The seed is pinned to the m38 cast evidence:
// match 1 seat 0 plays foundations-keen-engineering at engine seed 1001
// (the repoCommanderGames row that casts Sai), so a cast is guaranteed by
// the same evidence the engine milestone was gated on, not hoped for.
func TestCommanderTableServesACommanderGameEndToEnd(t *testing.T) {
	t.Parallel()
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, pace: 0, perpetual: false,
		formatsRaw: "commander", seed: 12833769953399007376})
	defer cancel()
	waitTables(t, url, 1)

	// Genesis (seq 0, the first Advance): every seat at 40 life with its
	// commander in the zone and on the roster — the wire evidence that a
	// Commander game, not a 100-card constructed pile, is being served.
	// A pace-0 bot match finishes in under a second, so the head view
	// would already show the game over; the start is read from the
	// recorded chain like a DVR scrub, which is exactly how the client
	// sees it.
	g := commanderView(t, url, "0")
	for _, p := range g.Players {
		if p.Life != 40 {
			t.Fatalf("seat %s started at %d life, want 40", p.Name, p.Life)
		}
		if len(p.Command) == 0 {
			t.Fatalf("seat %s started with an empty command zone", p.Name)
		}
		if len(p.Commanders) == 0 {
			t.Fatalf("seat %s projected no commanders roster", p.Name)
		}
		if len(p.CommanderCasts) != len(p.Commanders) {
			t.Fatalf("seat %s casts %d parallel to %d commanders", p.Name, len(p.CommanderCasts), len(p.Commanders))
		}
	}

	// The cast: poll until some seat's commander_casts is non-zero (the
	// seeded game casts Sai, but which seat is the first is up to the game
	// — any non-zero cast proves the command zone is being played, not
	// merely displayed). The match also runs to a winner; the cast
	// assertion is the point, so it is checked on every poll and the
	// match-end fallback is only the deadline's last resort.
	couldCast := false
	deadline := time.Now().Add(30 * time.Second)
	for !couldCast {
		if time.Now().After(deadline) {
			t.Fatalf("no seat cast a commander from the command zone within 30s")
		}
		v := commanderView(t, url, "")
		for _, p := range v.Players {
			for _, c := range p.CommanderCasts {
				if c > 0 {
					couldCast = true
				}
			}
		}
		if v.Over {
			// The match ended with no cast: the seed/pod must have drifted
			// from the evidence — fail loudly, don't poll forever.
			if !couldCast {
				t.Fatalf("match ended with no commander cast: the seeded gun died")
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestCommanderTableDealtAnInvalidDeckFailsAtStartup is the startup
// validation: a deck directory whose commander-declared file fails
// deck.ValidateCommander makes serve refuse with the deck named — before
// anything listens — so a Commander table can never half-start on an
// illegal deck. The fixture is a commander-declared Mountain: real corpus
// card names, wrong on eligibility (CR 903.3), and the error names the
// deck, not vaguely "a deck".
func TestCommanderTableDealtAnInvalidDeckFailsAtStartup(t *testing.T) {
	dir := t.TempDir()
	bad := `{"name":"bad-commander","commander":"Mountain","cards":[{"name":"Mountain","count":100}]}`
	if err := os.WriteFile(filepath.Join(dir, "bad-commander.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg := config{cards: "../../.cards", decks: dir, tables: 1, seats: 2, pace: 0,
		cooldown: 0, dir: t.TempDir(), spectator: "omniscient", seed: 1, perpetual: false, formatsRaw: "commander"}
	// serve is driven on a cancellable context so a neutralised validation
	// — the very failure this test exists to catch — fails the test by
	// assertion (serve succeeds and simply keeps serving, instead of
	// refusing), not by hanging the suite.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, cfg, ln) }()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("serve accepted a commander table dealt an invalid deck")
		}
		if !strings.Contains(err.Error(), "bad-commander") {
			t.Fatalf("startup error does not name the deck: %v", err)
		}
		if !strings.Contains(err.Error(), "Mountain") {
			t.Fatalf("startup error does not name the offending card: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("serve did not refuse the invalid commander deck at startup (validation gone?)")
	}
}

// createVsBotGame POSTs the play-vs-bot create flow with the given JSON body
// and returns the served table's base URL parts: the table id and the human
// seat's bearer token, straight from the CreateGameResponse. It is the
// mulligan-allowance test's harness: the flow is the only way a client can
// name its own London allowance (finding fb-20260914T114629Z-6c81e4d6).
func createVsBotGame(t *testing.T, url, body string) (table string, tok string) {
	t.Helper()
	resp, err := http.Post(url+"/api/games", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var g httpapi.CreateGameResponse
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatalf("create-game decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/games status %d body %+v", resp.StatusCode, g)
	}
	return g.Table, g.Token
}

// answerMulligan posts one intent answering the given decision with the
// named option indices, through the same URL shape the client builds.
func answerMulligan(t *testing.T, url, table, tok string, d decision.Decision, choices []int) {
	t.Helper()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/tables/%s/matches/1/intent", url, table), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mulligan answer returned %d, want 204", resp.StatusCode)
	}
}

// afterTossChoice consumes a human winner's CR 103.1 ask, if present, before
// the caller inspects the first post-toss decision. A bot winner answers its
// own ask without exposing it to seat 0. Choose the human winner themselves
// so the mulligan test remains about the allowance, not turn order.
func afterTossChoice(t *testing.T, url, table, pendingURL, tok string, d decision.Decision) decision.Decision {
	t.Helper()
	if d.Kind != decision.KStartingPlayer {
		return d
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 || d.Options[0].Player == d.Options[1].Player {
		t.Fatalf("bad human toss choice: %+v", d)
	}
	self := -1
	for i, opt := range d.Options {
		if opt.Player == d.Player {
			self = i
		}
	}
	if self < 0 {
		t.Fatalf("toss winner %d not among candidates: %+v", d.Player, d.Options)
	}
	answerMulligan(t, url, table, tok, d, []int{self})
	return fastPollDecision(t, pendingURL, "0", tok)
}

// TestVsBotGameHonoursRequestedMulliganAllowance is the product proof for
// finding fb-20260914T114629Z-6c81e4d6: a play-vs-bot game created with
// "mulligans": 3 re-offers the keep/mulligan ask (option 1, "mulligan") until
// the seat has taken 3 — the 4th ask offers ONLY "keep" — and then bottoms
// the 3-card penalty, after which the match proceeds. The omission path is
// the other half: with "mulligans": 0 no pregame ask ever reaches the human
// (the first decision of the game is not a mulligan), and with the field
// absent the server default still runs, exactly as before this field existed.
// Both scenarios share one serve boot: the vsbot flow mints a fresh table per
// POST /api/games, so a second create is free — and the package's test-time
// budget is per-boot, so two boots for one finding would spend it twice.
func TestVsBotGameHonoursRequestedMulliganAllowance(t *testing.T) {
	url, cancel, done := startServe(t, config{tables: 1, seats: 2, pace: 0, perpetual: true, seatToken: "tok", vsbot: true, mulligans: 1})
	defer cancel()

	table, tok := createVsBotGame(t, url, `{"format":"constructed","mulligans":3}`)
	pendingURL := fmt.Sprintf("%s/api/tables/%s/matches/1/pending", url, table)

	// Take three mulligans: option 1 ("mulligan") of each keep/mulligan ask.
	for taken := 1; taken <= 3; taken++ {
		d := fastPollDecision(t, pendingURL, "0", tok)
		if taken == 1 {
			d = afterTossChoice(t, url, table, pendingURL, tok, d)
		}
		if d.Kind != decision.KMulligan {
			t.Fatalf("after %d prior mulligan(s) the seat is asked %q, want mulligan", taken-1, d.Kind)
		}
		if len(d.Options) != 2 {
			t.Fatalf("mulligan ask #%d offers %d options, want keep+mulligan", taken, len(d.Options))
		}
		answerMulligan(t, url, table, tok, d, []int{1})
	}

	// The allowance is spent: the next ask offers only "keep".
	d := fastPollDecision(t, pendingURL, "0", tok)
	if d.Kind != decision.KMulligan || len(d.Options) != 1 || d.Options[0].Kind != "keep" {
		t.Fatalf("4th ask is %q with %d option(s), want mulligan with only \"keep\"", d.Kind, len(d.Options))
	}
	answerMulligan(t, url, table, tok, d, []int{0})

	// The bottoming ask: 3 cards on the bottom (freeMulligans is 0 at two
	// seats). Answer the first three hand indices.
	d = fastPollDecision(t, pendingURL, "0", tok)
	if d.Kind != decision.KMulligan || d.Min != 3 || d.Max != 3 {
		t.Fatalf("bottoming ask is %q Min %d Max %d, want mulligan 3/3", d.Kind, d.Min, d.Max)
	}
	answerMulligan(t, url, table, tok, d, []int{0, 1, 2})

	// The pregame round is over: the seat is asked something else entirely.
	// 5ms tick — this loop usually exits on its first or second poll.
	deadline := time.Now().Add(15 * time.Second)
	for {
		d, status := decisionOnce(t, pendingURL, "0", tok)
		if status == http.StatusOK && d.Kind != decision.KMulligan {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("game did not advance past the 3-mulligan round (last %q status %d)", d.Kind, status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The 0 leaf, on the same boot: an explicit 0 disables the London round
	// entirely — the human seat's first decision is a real game decision,
	// never a mulligan. The pointer wiring in the create flow is what makes
	// the value reachable at all.
	table2, tok2 := createVsBotGame(t, url, `{"format":"constructed","mulligans":0}`)
	pendingURL2 := fmt.Sprintf("%s/api/tables/%s/matches/1/pending", url, table2)
	d2 := afterTossChoice(t, url, table2, pendingURL2, tok2, fastPollDecision(t, pendingURL2, "0", tok2))
	if d2.Kind == decision.KMulligan {
		t.Fatalf("first decision of a mulligans-0 game is %q: the pregame round still ran", d2.Kind)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestDeckLoaderSeatsEveryCommander pins the deck-file → host.Deck plumbing
// for a partner pair (real corpus cards Frodo, Adventurous Hobbit + Sam,
// Loyal Attendant — the Food and Fellowship partners): a two-commander deck
// file seats BOTH flat indices on host.Deck.Commanders, the list genesis
// moves into the command zone, and the same file passes ValidateCommander
// because the identity is the CR 903.5 union {W}{B}{G}.
func TestDeckLoaderSeatsEveryCommander(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	src := `{"name":"pair","format":"commander","commanders":["Frodo, Adventurous Hobbit","Sam, Loyal Attendant"],"cards":[{"name":"Frodo, Adventurous Hobbit","count":1},{"name":"Sam, Loyal Attendant","count":1},{"name":"Plains","count":98}]}`
	if err := os.WriteFile(filepath.Join(dir, "pair.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pair.json"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := deck.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ValidateCommander(reg); err != nil {
		t.Fatalf("real-corpus partner pair rejected by the union-identity validator: %v", err)
	}
	d, err := deckLoader(reg, dir)("pair")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Commanders) != 2 || d.Commanders[0] != 0 || d.Commanders[1] != 1 {
		t.Fatalf("deckLoader seated %v, want both partners [0 1]", d.Commanders)
	}
}

func TestServeFlagBotAutoManaDefaultsOnAndReachesStartupTables(t *testing.T) {
	fs, c := serveFlags()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if !c.botAutoPayMana {
		t.Fatal("-bot-auto-mana defaults off: served bot tables would use manual taps")
	}
	c.tables, c.seats = 2, 2
	cfgs := c.tableConfigs(nil, nil, view.Omniscient)
	for _, cfg := range cfgs {
		if !cfg.BotAutoPayMana {
			t.Fatalf("startup table %s omitted bot auto mana: %+v", cfg.ID, cfg)
		}
	}
	if err := fs.Parse([]string{"-bot-auto-mana=false"}); err != nil {
		t.Fatal(err)
	}
	if c.botAutoPayMana {
		t.Fatal("-bot-auto-mana=false was ignored")
	}
}

func TestServeFlagAutoManaDefaultsOnAndFalseRestoresLegacyTables(t *testing.T) {
	fs, c := serveFlags()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if !c.autoMana {
		t.Fatal("-auto-mana defaults off: served tables would not publish the enabled payment-plan UI")
	}
	c.tables, c.seats = 2, 2
	for _, cfg := range c.tableConfigs(nil, nil, view.Omniscient) {
		if !cfg.AutoMana {
			t.Fatalf("startup table %s omitted auto mana: %+v", cfg.ID, cfg)
		}
	}
	fs, c = serveFlags()
	if err := fs.Parse([]string{"-auto-mana=false"}); err != nil {
		t.Fatal(err)
	}
	if c.autoMana {
		t.Fatal("-auto-mana=false was ignored")
	}
	for _, cfg := range c.tableConfigs(nil, nil, view.Omniscient) {
		if cfg.AutoMana {
			t.Fatalf("startup table %s did not restore the legacy manual path: %+v", cfg.ID, cfg)
		}
	}
}
