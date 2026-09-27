package httpapi

import (
	"fmt"
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

func TestPaymentPlanSelectorIsFencedAndNeverCrashesTheMatch(t *testing.T) {
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
	sel := &decision.PaymentSelection{ActionID: strings.Repeat("a", 64), Plan: decision.PaymentPlan{Version: decision.PaymentPlanV1, ID: strings.Repeat("b", 64)}}
	in := decision.Intent{Seq: d.Seq, Player: parked, Payment: sel}
	url := srv.URL + "/api/tables/t1/matches/1/intent"

	// Another seat's claim cannot submit the parked seat's selector.
	if code, e, _ := seatReq(t, http.MethodPost, url, os, in); code < 400 || code == http.StatusInternalServerError {
		t.Fatalf("other claim posting a payment selector: %d %+v", code, e)
	}
	// An unoffered selector, an unknown version and a mixed selector are all
	// refused with a 409 and do not unpark the seat.
	unknown := in
	unknown.Payment = &decision.PaymentSelection{ActionID: sel.ActionID, Plan: decision.PaymentPlan{Version: 2}}
	mixed := in
	mixed.Choices = []int{0}
	for name, bad := range map[string]decision.Intent{"unoffered": in, "unknown version": unknown, "mixed": mixed} {
		if code, e, _ := seatReq(t, http.MethodPost, url, ps, bad); code != http.StatusConflict {
			t.Fatalf("%s payment selector: %d %+v, want 409", name, code, e)
		}
	}
	again, err := r.Pending("t1", 1, parked)
	if err != nil || again.Seq != d.Seq {
		t.Fatalf("rejected selectors moved the game: %+v %v", again, err)
	}
	ms, err := r.Matches("t1")
	if err != nil || len(ms) != 1 || ms[0].State != protocol.MatchLive {
		t.Fatalf("match after rejected selectors: %+v %v", ms, err)
	}
	// The other seat's projected view never carries the parked seat's
	// decision or its payment extension.
	if code, e, raw := seatReq(t, http.MethodGet, fmt.Sprintf("%s/api/tables/t1/matches/1/view?seat=%d", srv.URL, other), os, nil); code != http.StatusOK {
		t.Fatalf("other seat view: %d %+v", code, e)
	} else if strings.Contains(string(raw), "payment_actions") || strings.Contains(string(raw), "payment_fallback") {
		t.Fatal("other seat's view carries a payment extension")
	}
	// The other seat's own pending read cannot reach the parked decision
	// (and therefore its PaymentActions).
	if code, _, _ := seatReq(t, http.MethodGet, fmt.Sprintf("%s/api/tables/t1/matches/1/pending?seat=%d", srv.URL, other), os, nil); code == http.StatusOK {
		t.Fatal("other seat read the parked seat's pending decision")
	}
}
