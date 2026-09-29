package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestNativeViewExposesOnlyAuthenticatedOwnersManifest(t *testing.T) {
	srv, r := parkedSeatServer(t, seatClaims)
	parkedSeat(t, r)
	seat0, seat1 := state.PlayerID(0), state.PlayerID(1)
	head := liveHead(t, r)
	request := func(seat state.PlayerID, claim string) (view.View, string) {
		t.Helper()
		var got view.View
		path := fmt.Sprintf("%s/api/tables/t1/matches/1/view?seat=%d&seq=%d", srv.URL, seat, head)
		code, err, raw := seatReq(t, http.MethodGet, path, claim, nil)
		if code != http.StatusOK {
			t.Fatalf("seat %d view: %d %+v", seat, code, err)
		}
		if e := json.Unmarshal(raw, &got); e != nil {
			t.Fatal(e)
		}
		return got, string(raw)
	}
	v0, raw0 := request(seat0, "s0")
	v1, raw1 := request(seat1, "s1")
	if v0.OwnDeck == nil || v1.OwnDeck == nil {
		t.Fatalf("owner manifests missing: seat0=%#v seat1=%#v", v0.OwnDeck, v1.OwnDeck)
	}
	if v0.OwnDeck.Name != "b" || v1.OwnDeck.Name != "c" {
		t.Fatalf("wrong owner manifest: seat0=%#v seat1=%#v", v0.OwnDeck, v1.OwnDeck)
	}
	if strings.Contains(raw0, "Plains") || strings.Contains(raw1, "Forest") {
		t.Fatalf("seat response included another seat's manifest: seat0=%s seat1=%s", raw0, raw1)
	}
	if code, err, raw := seatReq(t, http.MethodGet, srv.URL+"/api/tables/t1/matches/1/view", "", nil); code != http.StatusOK {
		t.Fatalf("spectator view: %d %+v", code, err)
	} else if strings.Contains(string(raw), "own_deck") {
		t.Fatalf("omniscient spectator received a manifest: %s", raw)
	}
	if code, err, _ := seatReq(t, http.MethodGet, fmt.Sprintf("%s/api/tables/t1/matches/1/view?seat=%d", srv.URL, seat1), "s0", nil); code != http.StatusForbidden || err.Code != "forbidden" {
		t.Fatalf("seat 0 could request seat 1's manifest: %d %+v", code, err)
	}
	if v0.Visibility != view.Seat.String() || v1.Visibility != view.Seat.String() {
		t.Fatalf("seat visibility = %q / %q", v0.Visibility, v1.Visibility)
	}
}
