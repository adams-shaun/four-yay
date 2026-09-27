package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/view"
)

// fixtureDeckSize caps every deck the shared loader serves. The httpapi
// fixtures do not care what the sample decks contain; several of them do run
// whole bot-vs-bot matches to completion (finishedServer, pausedServer,
// undoServer), and how long such a match runs is an accident of the seed's
// rng stream: the CR 103.1 toss (one extra IntN before the shuffles) shifted
// every game's stream at once and took the package from ~2.4s to ~14s with
// the decks at their full 40 cards. A 40-card whelp/bolt game can stall for
// dozens of turns before anyone dies; a 12-card deck decks out in bounded
// time at EVERY seed, so the package's wall time no longer rides on the
// stream. Measured 2026-09-15 at 9065580..HEAD, seed 5, 4 seats: 40-card
// finished matches 0.8-1.2s (75k events); 12-card matches 0.01s (2,988
// events, 23 turns, 3 seats lost, game_over) at every seed probed -- the
// game is still real (combat, kills, decisions), just short by construction.
// finishedServer asserts the cap still holds; raise both deliberately if a
// fixture ever needs a longer game.
const fixtureDeckSize = 12

// fixtureMatchEvents is the generous ceiling finishedServer puts on one
// fixture match (measured ~3k at fixtureDeckSize; the cap exists to catch a
// future change that un-bounds game length, not to constrain the fixtures).
const fixtureMatchEvents = 20000

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

// finishedServer plays one 4-seat match to completion and serves it.
func finishedServer(t *testing.T, o Options) (*httptest.Server, *host.Registry) {
	t.Helper()
	r, err := host.New(host.Options{LoadDeck: loader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := host.TableConfig{ID: "t1", Name: "Table 1", Seats: 4, Decks: []string{"a", "b", "c", "d"}, Seed: 5, Spectator: view.Omniscient}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")
	ms, err := r.Matches("t1")
	if err != nil || len(ms) != 1 || ms[0].State != protocol.MatchFinished {
		t.Fatalf("finishedServer: match did not finish cleanly: err=%v ms=%+v", err, ms)
	}
	// The fixture contract that keeps this package inside its budget: a
	// finishedServer match is short BY CONSTRUCTION (loader truncates every
	// deck to fixtureDeckSize), so its length cannot ride on the seed's rng
	// stream -- see the CR 103.1 note on fixtureDeckSize. If this trips, a
	// fixture match got long again: raise fixtureDeckSize's game cap
	// deliberately (both the constant and this bound), never silently.
	if ms[0].Events > fixtureMatchEvents {
		t.Fatalf("finishedServer: fixture match ran %d events, over the %d cap -- the fixture decks stopped bounding game length", ms[0].Events, fixtureMatchEvents)
	}
	srv := httptest.NewServer(NewHandler(r, o))
	t.Cleanup(srv.Close)
	return srv, r
}

func getJSON(t *testing.T, url string, into any) (int, protocol.ErrorBody) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%s: content-type %q", url, resp.Header.Get("Content-Type"))
	}
	if resp.StatusCode >= 400 {
		var e protocol.ErrorBody
		if err := json.Unmarshal(body, &e); err != nil {
			t.Fatalf("%s: non-JSON error body %q", url, body)
		}
		return resp.StatusCode, e
	}
	if into != nil {
		if err := json.Unmarshal(body, into); err != nil {
			t.Fatalf("%s: %v\n%s", url, err, body)
		}
	}
	return resp.StatusCode, protocol.ErrorBody{}
}

func postJSON(t *testing.T, url string, body any) (int, protocol.ErrorBody) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e protocol.ErrorBody
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return resp.StatusCode, e
	}
	return resp.StatusCode, protocol.ErrorBody{}
}

func TestTablesAndMatches(t *testing.T) {
	srv, _ := finishedServer(t, Options{})
	var tables []protocol.TableInfo
	if code, _ := getJSON(t, srv.URL+"/api/tables", &tables); code != 200 || len(tables) != 1 || tables[0].ID != "t1" {
		t.Fatalf("%d %+v", code, tables)
	}
	var ms []protocol.MatchInfo
	if code, _ := getJSON(t, srv.URL+"/api/tables/t1/matches", &ms); code != 200 || len(ms) != 1 || ms[0].State != protocol.MatchFinished {
		t.Fatalf("%d %+v", code, ms)
	}
	if code, e := getJSON(t, srv.URL+"/api/tables/t9/matches", nil); code != 404 || e.Code != "not_found" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestTableListHidesOnDemandVsBotGames(t *testing.T) {
	r, err := host.New(host.Options{LoadDeck: loader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.AddTable(host.TableConfig{ID: "t1", Name: "Hosted", Seats: 2, Decks: []string{"a", "b"}, Seed: 1, Spectator: view.Omniscient}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(host.TableConfig{ID: "g1", Name: "Play vs bot (constructed)", Seats: 2, Decks: []string{"a", "b"}, Seed: 2,
		PlayerNames: []string{"You", "Bot"}, Spectator: view.Omniscient, Humans: []int{0}, OnDemand: true}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(r, Options{}))
	t.Cleanup(srv.Close)
	var tables []protocol.TableInfo
	if code, _ := getJSON(t, srv.URL+"/api/tables", &tables); code != http.StatusOK || len(tables) != 1 || tables[0].ID != "t1" {
		t.Fatalf("lobby tables: %d %+v", code, tables)
	}
	if got := r.Tables(); len(got) != 2 {
		t.Fatalf("registry lost direct game route: %+v", got)
	}
}

func TestViewAndEvents(t *testing.T) {
	srv, r := finishedServer(t, Options{})
	ms, _ := r.Matches("t1")
	head := uint64(ms[0].Events - 1)
	var v view.View
	if code, _ := getJSON(t, srv.URL+"/api/tables/t1/matches/1/view", &v); code != 200 || !v.Over || v.Visibility != "omniscient" {
		t.Fatalf("view at head: %d %+v", code, v)
	}
	if code, _ := getJSON(t, fmt.Sprintf("%s/api/tables/t1/matches/1/view?seq=%d", srv.URL, head/2), &v); code != 200 || v.Over {
		t.Fatalf("view mid: %d over=%v", code, v.Over)
	}
	code, e := getJSON(t, fmt.Sprintf("%s/api/tables/t1/matches/1/view?seq=%d", srv.URL, head+1), nil)
	if code != 409 || e.Code != "beyond_head" || e.Head != head {
		t.Fatalf("beyond head: %d %+v", code, e)
	}
	if code, e := getJSON(t, srv.URL+"/api/tables/t1/matches/1/view?seq=abc", nil); code != 400 || e.Code != "bad_request" {
		t.Fatalf("bad seq: %d %+v", code, e)
	}
	if code, _ := getJSON(t, srv.URL+"/api/tables/t1/matches/x/view", nil); code != 400 {
		t.Fatalf("bad k: %d", code)
	}
	if code, _ := getJSON(t, srv.URL+"/api/tables/t1/matches/2/view", nil); code != 404 {
		t.Fatalf("unknown match: %d", code)
	}
	var evs []protocol.EventBody
	if code, _ := getJSON(t, fmt.Sprintf("%s/api/tables/t1/matches/1/events?since=%d", srv.URL, head-2), &evs); code != 200 || len(evs) != 3 || evs[0].Event.Seq != head-2 {
		t.Fatalf("events tail: %d %+v", code, evs)
	}
	if code, _ := getJSON(t, srv.URL+"/api/tables/t1/matches/1/events", &evs); code != 200 || len(evs) != int(head)+1 {
		t.Fatalf("all events: %d %d", code, len(evs))
	}
	if code, e := getJSON(t, fmt.Sprintf("%s/api/tables/t1/matches/1/events?since=%d", srv.URL, head+1), nil); code != 409 || e.Head != head {
		t.Fatalf("events beyond head: %d %+v", code, e)
	}
}

// formatsLoader resolves one commander table's two decks and one constructed
// table's four decks: the commander decks carry their commander index exactly
// as gorged's loader attaches it, so a commander TableConfig validates.
func formatsLoader(t *testing.T) func(string) (host.Deck, error) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	byName := map[string]host.Deck{}
	for _, n := range []string{"foundations-calling-all-angels", "foundations-keen-engineering"} {
		f := testutil.RepoDeckFile(t, n)
		d := host.Deck{Name: n, Cards: testutil.RepoDeck(t, reg, n)}
		if f.Commander != "" {
			d.Commanders = []int{f.CommanderIndex()}
		}
		byName[n] = d
	}
	names, decks := testutil.SampleDecks(t, 4)
	for i, nm := range names {
		byName[nm] = host.Deck{Name: nm, Cards: decks[i]}
	}
	return func(name string) (host.Deck, error) {
		d, ok := byName[name]
		if !ok {
			return host.Deck{}, host.ErrNotFound
		}
		return d, nil
	}
}

// TestTableListReportsFormat pins the table list's format wire field, decoded
// from the JSON response (not the Go struct, so the json tag and the
// no-omitempty decision are both exercised): a commandeught table reports
// "commander" and a default (zero-value constructed) table reports
// "constructed", never an empty string.
func TestTableListReportsFormat(t *testing.T) {
	r, err := host.New(host.Options{LoadDeck: formatsLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cmdr := host.TableConfig{ID: "cm", Name: "Commander", Seats: 2,
		Decks: []string{"foundations-calling-all-angels", "foundations-keen-engineering"},
		Seed:  1001, Spectator: view.Omniscient, Format: host.FormatCommander}
	con := host.TableConfig{ID: "co", Name: "Constructed", Seats: 4,
		Decks: []string{"a", "b", "c", "d"}, Seed: 5, Spectator: view.Omniscient}
	if err := r.AddTable(cmdr); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(con); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(r, Options{}))
	t.Cleanup(srv.Close)

	var tables []protocol.TableInfo
	if code, e := getJSON(t, srv.URL+"/api/tables", &tables); code != 200 {
		t.Fatalf("GET /api/tables: %d %+v", code, e)
	}
	byID := map[string]string{}
	for _, tbl := range tables {
		byID[tbl.ID] = tbl.Format
	}
	// Assert on the decoded JSON so tag + no-omitempty are both covered: a
	// zero-format "constructed" would come back empty if the field were
	// omitted (omitempty drops the zero value), so "constructed" here proves
	// the field is always emitted.
	if byID["cm"] != "commander" {
		t.Fatalf("commander table format = %q, want commander", byID["cm"])
	}
	if byID["co"] != "constructed" {
		t.Fatalf("constructed table format = %q, want constructed", byID["co"])
	}
	if len(tables) != 2 {
		t.Fatalf("served %d tables, want 2", len(tables))
	}
}

// TestTableListSeaNames pins seat names on the table list wire field, decoded
// from the JSON response so the omitempty decision is covered: a table with a
// live match reports its seats' deck names in seat order, and a table that has
// never run a match reports the field absent (nil slice omits it, so the
// decoded value is nil and stays that way even when a second call re-decodes).
func TestTableListSeatNames(t *testing.T) {
	r, err := host.New(host.Options{LoadDeck: formatsLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	live := host.TableConfig{ID: "lv", Name: "Live", Seats: 2,
		Decks: []string{"foundations-calling-all-angels", "foundations-keen-engineering"},
		Seed:  1001, Spectator: view.Omniscient, Perpetual: true}
	idle := host.TableConfig{ID: "id", Name: "Idle", Seats: 4,
		Decks: []string{"a", "b", "c", "d"}, Seed: 5, Spectator: view.Omniscient}
	for _, c := range []host.TableConfig{live, idle} {
		if err := r.AddTable(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Start("lv"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(r, Options{}))
	t.Cleanup(srv.Close)

	var tables []protocol.TableInfo
	// Poll until the live table's first match is in flight and its names
	// show: the perpetual loop keeps cur a live match almost always, but the
	// brief gap between matches makes one read racing it flaky.
	deadline := time.Now().Add(5 * time.Second)
	for {
		tables = tables[:0]
		var code int
		var e protocol.ErrorBody
		if code, e = getJSON(t, srv.URL+"/api/tables", &tables); code != 200 {
			t.Fatalf("GET /api/tables: %d %+v", code, e)
		}
		if time.Now().After(deadline) {
			break
		}
		if lv := findTable(tables, "lv"); lv != nil && len(lv.SeatNames) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	lv := findTable(tables, "lv")
	if lv == nil {
		t.Fatalf("live table missing from list: %+v", tables)
	}
	// Match 1 plays Decks[(i+1)%2] in seat order, so its seats are the two
	// decks rotated: seat 0 gets the second deck, seat 1 the first. But
	// SeatNames are the PLAYER names (seat-indexed, independent of the deck
	// rotation), so they are "Player 1"/"Player 2" whatever deck sits where
	// (B3: a player name is not a deck name). The deck identity is on
	// SeatInfo.Deck, not here.
	want := []string{"Player 1", "Player 2"}
	if len(lv.SeatNames) != len(want) {
		t.Fatalf("live table seat_names = %q, want %q", lv.SeatNames, want)
	}
	for i, n := range want {
		if lv.SeatNames[i] != n {
			t.Errorf("live table seat %d = %q, want %q", i, lv.SeatNames[i], n)
		}
	}

	// A table that has never started a match omits the field: the decoded
	// value is nil, proving no "" rows and no "Seat N" placeholders leak.
	id := findTable(tables, "id")
	if id == nil {
		t.Fatalf("idle table missing from list: %+v", tables)
	}
	if id.SeatNames != nil {
		t.Fatalf("idle table seat_names = %q, want absent (nil)", id.SeatNames)
	}
}

func findTable(tables []protocol.TableInfo, id string) *protocol.TableInfo {
	for i := range tables {
		if tables[i].ID == id {
			return &tables[i]
		}
	}
	return nil
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	srv, r := finishedServer(t, Options{})
	s := r.OpenSession()
	if code, _ := postJSON(t, srv.URL+"/api/subscribe", protocol.Subscribe{Session: s.ID, Table: "*", Mode: protocol.ModeOverview}); code != 204 {
		t.Fatalf("subscribe *: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/api/subscribe", protocol.Subscribe{Session: s.ID, Table: "t1", Mode: protocol.ModeFocus}); code != 204 {
		t.Fatalf("subscribe t1: %d", code)
	}
	if code, e := postJSON(t, srv.URL+"/api/subscribe", protocol.Subscribe{Session: "s999", Table: "t1", Mode: protocol.ModeFocus}); code != 404 || e.Code != "not_found" {
		t.Fatalf("unknown session: %d %+v", code, e)
	}
	if code, _ := postJSON(t, srv.URL+"/api/subscribe", protocol.Subscribe{Session: s.ID, Table: "t1", Mode: "sideways"}); code != 400 {
		t.Fatalf("bad mode: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/api/subscribe", protocol.Subscribe{Session: s.ID, Table: "t9", Mode: protocol.ModeFocus}); code != 404 {
		t.Fatalf("unknown table: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/api/unsubscribe", protocol.Unsubscribe{Session: s.ID, Table: "t1"}); code != 204 {
		t.Fatalf("unsubscribe: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/api/unsubscribe", protocol.Unsubscribe{Session: s.ID, Table: "t1"}); code != 404 {
		t.Fatalf("unsubscribe twice: %d", code)
	}
	resp, _ := http.Post(srv.URL+"/api/subscribe", "application/json", bytes.NewReader([]byte("{")))
	if resp.StatusCode != 400 {
		t.Fatalf("malformed JSON: %d", resp.StatusCode)
	}
	resp, _ = http.Get(srv.URL + "/api/subscribe")
	if resp.StatusCode != 405 {
		t.Fatalf("GET subscribe: %d", resp.StatusCode)
	}
}

func TestAuthorizeGatesEveryRoute(t *testing.T) {
	denied := errors.New("no")
	srv, _ := finishedServer(t, Options{Authorize: func(r *http.Request) error {
		if r.Header.Get("X-Ok") == "1" {
			return nil
		}
		return denied
	}})
	for _, path := range []string{"/api/tables", "/api/tables/t1/matches", "/api/tables/t1/matches/1/view", "/api/tables/t1/matches/1/events"} {
		if code, e := getJSON(t, srv.URL+path, nil); code != 401 || e.Code != "unauthorized" {
			t.Fatalf("%s: %d %+v", path, code, e)
		}
	}
	if code, _ := postJSON(t, srv.URL+"/api/subscribe", protocol.Subscribe{}); code != 401 {
		t.Fatalf("subscribe: %d", code)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/tables", nil)
	req.Header.Set("X-Ok", "1")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("authorised request: %d", resp.StatusCode)
	}
}
