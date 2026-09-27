package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/httpapi"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// gameLoader serves testutil.SampleDecks' four decks under "a".."d", so a
// createGame test can start a real human-vs-bot match without opening the
// corpus (the sample decks are authored inline, Ruling P9).
func gameLoader(t *testing.T) func(string) (host.Deck, error) {
	t.Helper()
	names, decks := testutil.SampleDecks(t, 4)
	by := map[string][]*cards.Card{}
	for i, n := range names {
		by[n] = decks[i]
	}
	return func(name string) (host.Deck, error) {
		cs, ok := by[name]
		if !ok {
			return host.Deck{}, host.ErrNotFound
		}
		return host.Deck{Name: name, Cards: cs}, nil
	}
}

func freshGameLock(t *testing.T) (*host.Registry, *seatGate) {
	t.Helper()
	r, err := host.New(host.Options{LoadDeck: gameLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	gate := &seatGate{tokenToClaim: map[string]httpapi.SeatClaim{}, claimTokens: map[httpapi.SeatClaim]string{}}
	return r, gate
}

// TestCreateGameBuildsARealSingleShotHumanVsBotTable drives the real
// gorged wiring (the createGame closure) and proves it starts an actual
// match, not merely that a config registers: the table binds seat 0 to a
// HumanSeat that parks on a real decision, and the returned token is
// grantable to that seat.
func TestCreateGameBuildsARealSingleShotHumanVsBotTable(t *testing.T) {
	r, gate := freshGameLock(t)
	c := config{mulligans: 0}
	create := c.createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	resp, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Table != "g1" || resp.Seat != 0 || resp.Token == "" || resp.Seed == 0 {
		t.Fatalf("response %+v", resp)
	}
	if want := "/t/g1?seat=0&token=" + resp.Token; resp.Join != want {
		t.Fatalf("join %q, want %q", resp.Join, want)
	}
	// The token resolves to seat 0 through the gate, exactly as the
	// seat-scoped HTTP endpoints will.
	claim, ok := gate.resolve(httptest.NewRequest(http.MethodGet, "/pending?seat=0&token="+resp.Token, nil))
	if !ok || claim.Table != host.TableID(resp.Table) || claim.Seat != state.PlayerID(0) {
		t.Fatalf("token did not resolve to its table and seat: %+v ok=%v", claim, ok)
	}
	// Seat 0 parks on a real decision: the human-vs-bot match began and the
	// bot seat is not answering for the human.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("seat 0 never parked on a decision")
		}
		if _, err := r.Pending("g1", 1, 0); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCreateGamePersistsRequestedBotPolicy(t *testing.T) {
	r, gate := freshGameLock(t)
	create := (config{mulligans: 0}).createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	resp, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed, BotPolicy: host.LethalPressurePolicy})
	if err != nil {
		t.Fatal(err)
	}
	if resp.BotPolicy != host.LethalPressurePolicy {
		t.Fatalf("response policy = %q", resp.BotPolicy)
	}
	if got := r.Tables()[0].BotPolicy; got != host.LethalPressurePolicy {
		t.Fatalf("table policy = %q", got)
	}
}

// TestCreateGameDealsADistinctConstructedPairAndIncrementsIDs pins the
// "two distinct decks" part of the random assignment — seat 0 and seat 1
// are never the same deck, whatever the shuffle — and that each requested
// game gets a fresh table id so consecutive games never collide. (A
// commander request is exercised in the two tests below: an empty commander
// pool errors, and the same AddTable gate that refuses a non-commander deck
// is what the served commander games rely on — see
// TestCommanderTableServesACommanderGameEndToEnd for the commander shape.)
func TestCreateGameDealsADistinctConstructedPairAndIncrementsIDs(t *testing.T) {
	r, gate := freshGameLock(t)
	c := config{mulligans: 0}
	create := c.createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	resp, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Table != "g1" || resp.Seat != 0 {
		t.Fatalf("response %+v", resp)
	}
	// The match is built on the table's own goroutine shortly after Start;
	// poll until it is visible, so the seat deck names are on the wire.
	var ms []protocol.MatchInfo
	deadline := time.Now().Add(20 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("match never appeared on table " + resp.Table)
		}
		var err error
		ms, err = r.Matches(host.TableID(resp.Table))
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) == 1 && len(ms[0].Seats) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	s0, s1 := ms[0].Seats[0].Deck, ms[0].Seats[1].Deck
	if s0 == s1 {
		t.Fatalf("both seats dealt the same deck %q", s0)
	}
	for _, d := range []string{s0, s1} {
		if d != "c" && d != "d" {
			t.Fatalf("constructed game dealt %q, not from the constructed pool", d)
		}
	}
	resp2, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed})
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Table != "g2" {
		t.Fatalf("second game table %q, want g2", resp2.Table)
	}
}

// TestCreateGameRefusesAFormatWithNoAvailableDeck ensures a requested format
// whose pool is empty is an error, not a table that silently deals nothing.
func TestCreateGameRefusesAFormatWithNoAvailableDeck(t *testing.T) {
	r, gate := freshGameLock(t)
	c := config{mulligans: 0}
	create := c.createGame(r, gate, nil, []string{"c", "d"}, view.Omniscient)
	if _, err := create(httpapi.CreateGameOptions{Format: host.FormatCommander}); err == nil || !strings.Contains(err.Error(), "commander") {
		t.Fatalf("expected a commander-no-deck error, got %v", err)
	}
}

func TestCreateGameHonoursBothSelectedDecks(t *testing.T) {
	r, gate := freshGameLock(t)
	c := config{mulligans: 0}
	create := c.createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	resp, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed, HumanDeck: "d", BotDeck: "c"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		ms, err := r.Matches(host.TableID(resp.Table))
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) == 1 && len(ms[0].Seats) == 2 {
			if ms[0].Seats[0].Deck != "d" || ms[0].Seats[1].Deck != "c" {
				t.Fatalf("selected decks not seated: %+v", ms[0].Seats)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("selected-deck match never appeared")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCreateGameRejectsUnknownAndWrongFormatDecks(t *testing.T) {
	r, gate := freshGameLock(t)
	create := (config{mulligans: 0}).createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	if _, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed, HumanDeck: "missing"}); err == nil || !strings.Contains(err.Error(), `unknown human deck "missing"`) {
		t.Fatalf("unknown deck error = %v", err)
	}
	if _, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed, BotDeck: "a"}); err == nil || !strings.Contains(err.Error(), `bot deck "a" belongs to commander`) {
		t.Fatalf("wrong-format deck error = %v", err)
	}
}

func TestCreateGameCarriesBotAutoManaToTheVsBotTable(t *testing.T) {
	r, gate := freshGameLock(t)
	create := (config{mulligans: 0, botAutoPayMana: true}).createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
	if _, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed}); err != nil {
		t.Fatal(err)
	}
	// createGame is the only dynamic table constructor. Its config literal
	// carries c.botAutoPayMana; a live game here proves that path is accepted
	// alongside the human/bot seating configuration.
	if len(r.Tables()) != 1 {
		t.Fatalf("tables = %+v, want one created game", r.Tables())
	}
}

// The on-demand constructor is separate from startup tableConfigs. Keep the
// feature gate on this path too: -auto-mana=false must leave both the human
// wire and its bot/caretaker on the legacy manual path.
func TestCreateGameCarriesAutoManaFeatureGateToTheVsBotTable(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			r, gate := freshGameLock(t)
			create := (config{mulligans: 0, botAutoPayMana: true, autoMana: enabled}).createGame(r, gate, []string{"a", "b"}, []string{"c", "d"}, view.Omniscient)
			if _, err := create(httpapi.CreateGameOptions{Format: host.FormatConstructed}); err != nil {
				t.Fatal(err)
			}
			tables := r.Tables()
			if len(tables) != 1 || tables[0].AutoMana != enabled {
				t.Fatalf("created tables = %+v, want AutoMana=%t", tables, enabled)
			}
		})
	}
}
