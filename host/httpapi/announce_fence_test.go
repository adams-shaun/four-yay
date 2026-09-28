package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/view"
)

// The announce-then-pay selector (Intent.Announce) rides the ordinary intent
// route: it decodes, it is fenced to the parked seat's claim, and a selector
// the decision does not offer is a 409 that neither unparks the seat nor
// crashes the match.
func TestAnnounceSelectorIsFencedAndNeverCrashesTheMatch(t *testing.T) {
	r, err := host.New(host.Options{LoadDeck: loader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.AddTable(host.TableConfig{ID: "t1", Name: "Table 1", Seats: 4, Decks: []string{"a", "b", "c", "d"},
		Seed: 5, Spectator: view.Omniscient, Humans: []int{0, 1}, AutoMana: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(r, Options{Seat: claimResolver(seatClaims)}))
	t.Cleanup(srv.Close)
	parked, other := parkedSeat(t, r)
	ps, os := "s"+strconv.Itoa(int(parked)), "s"+strconv.Itoa(int(other))
	d, err := r.Pending("t1", 1, parked)
	if err != nil {
		t.Fatal(err)
	}
	in := decision.Intent{Seq: d.Seq, Player: parked, Choices: []int{}, Announce: &decision.AnnounceSelection{ActionID: strings.Repeat("a", 64)}}
	url := srv.URL + "/api/tables/t1/matches/1/intent"
	if code, e, _ := seatReq(t, http.MethodPost, url, os, in); code < 400 || code == http.StatusInternalServerError {
		t.Fatalf("other claim posting an announce: %d %+v", code, e)
	}
	mixed := in
	mixed.Choices = []int{0}
	for name, bad := range map[string]decision.Intent{"unoffered": in, "mixed": mixed} {
		if code, e, _ := seatReq(t, http.MethodPost, url, ps, bad); code != http.StatusConflict {
			t.Fatalf("%s announce: %d %+v, want 409", name, code, e)
		}
	}
	again, err := r.Pending("t1", 1, parked)
	if err != nil || again.Seq != d.Seq {
		t.Fatalf("rejected announces moved the game: %+v %v", again, err)
	}
	ms, err := r.Matches("t1")
	if err != nil || len(ms) != 1 || ms[0].State != protocol.MatchLive {
		t.Fatalf("match after rejected announces: %+v %v", ms, err)
	}
}
