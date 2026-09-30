package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/adams-shaun/gorge/protocol"
)

func getTally(t *testing.T, url string) (int, protocol.DeckTally) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got protocol.DeckTally
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, got
}

func TestDeckStats(t *testing.T) {
	srv, _ := finishedServer(t, Options{})

	// Default floor 10: one game cannot clear it, but the match is counted.
	status, got := getTally(t, srv.URL+"/api/stats/decks")
	if status != http.StatusOK || got.MinGames != 10 || got.MatchesCounted != 1 || got.Decks == nil || len(got.Decks) != 0 {
		t.Fatalf("default: status %d body %+v", status, got)
	}

	// min_games=0 lists every seat's deck: 4 decks, one game each, one winner.
	status, got = getTally(t, srv.URL+"/api/stats/decks?min_games=0")
	if status != http.StatusOK || len(got.Decks) != 4 {
		t.Fatalf("min_games=0: status %d body %+v", status, got)
	}
	wins := 0
	for _, d := range got.Decks {
		if d.Games != 1 || d.Wins+d.Losses+d.Draws != 1 {
			t.Fatalf("row %+v", d)
		}
		wins += d.Wins
	}
	if wins > 1 {
		t.Fatalf("%d winners in one match", wins)
	}

	for _, bad := range []string{"x", "-1"} {
		if status, _ = getTally(t, srv.URL+"/api/stats/decks?min_games="+bad); status != http.StatusBadRequest {
			t.Fatalf("min_games=%s: status %d, want 400", bad, status)
		}
	}
	resp, err := http.Post(srv.URL+"/api/stats/decks", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d, want 405", resp.StatusCode)
	}
}
