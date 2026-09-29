package manabrewhttp

// The x_gorge_own_deck_v1 extension end to end (seat-deck-manifest spec,
// interface mapping item 4 and its "Security and regression requirements"):
// the seat's own genesis deck manifest rides every state path this transport
// serves -- one-shot poll, stream connect, reconnect, state after an intent
// -- as exactly the native view's own_deck manifest rendered as name/count
// rows, and never anyone else's. The translator-level payload shape is
// pinned in internal/manabrew/own_deck_test.go.

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
)

// expectedOwnDeck re-derives what the extension MUST contain from the
// registry's own seat view (the genesis manifest rules.New built), through
// an independent row mapping written here rather than through the
// translator's, so agreement between the two is a real cross-check.
func expectedOwnDeck(t *testing.T, ts *testServer, seat state.PlayerID) *mb.OwnDeckExtension {
	t.Helper()
	v, err := ts.reg.ViewAtSeat("t1", 1, headSeqFor(t, ts.reg, "t1", 1), seat)
	if err != nil {
		t.Fatal(err)
	}
	if v.OwnDeck == nil {
		t.Fatalf("seat %d's registry view carries no own-deck manifest; the core contract moved", seat)
	}
	m := *v.OwnDeck
	if len(m.Main) == 0 {
		t.Fatalf("seat %d's manifest has an empty main; the fixture must exercise real rows", seat)
	}
	ext := &mb.OwnDeckExtension{Name: m.Name, Main: make([]mb.OwnDeckRow, 0, len(m.Main))}
	for _, row := range m.Main {
		ext.Main = append(ext.Main, mb.OwnDeckRow{Name: row.Name, Count: row.Count})
	}
	if len(m.Sideboard) > 0 {
		ext.Sideboard = make([]mb.OwnDeckRow, 0, len(m.Sideboard))
		for _, row := range m.Sideboard {
			ext.Sideboard = append(ext.Sideboard, mb.OwnDeckRow{Name: row.Name, Count: row.Count})
		}
	}
	if len(m.Commanders) > 0 {
		ext.Commanders = append([]string{}, m.Commanders...)
	}
	return ext
}

// assertStateCarriesOwnDeck checks one polled state: the extension present,
// equal to the registry's own manifest for that seat.
func assertStateCarriesOwnDeck(t *testing.T, ts *testServer, token string, seat state.PlayerID) mb.GameViewDto {
	t.Helper()
	gv := pollState(t, ts, token)
	if gv.OwnDeck == nil {
		t.Fatalf("seat %d's polled state omitted x_gorge_own_deck_v1", seat)
	}
	if want := expectedOwnDeck(t, ts, seat); !reflect.DeepEqual(gv.OwnDeck, want) {
		t.Fatalf("seat %d's polled extension = %#v, want %#v", seat, gv.OwnDeck, want)
	}
	return gv
}

// rawStatePoll is pollSeat0Raw with an explicit token: the response's
// messages as the server wrote them.
func rawStatePoll(t *testing.T, ts *testServer, token string) []json.RawMessage {
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
		t.Fatalf("state poll for %s: status %d", token, resp.StatusCode)
	}
	var raws []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raws); err != nil {
		t.Fatal(err)
	}
	return raws
}

// TestOwnDeckOnEveryStatePath proves poll, stream connect, reconnect and
// state-after-intent all carry the SAME manifest (the genesis list, immune
// to draws, shuffles and whatever the answered intent did).
func TestOwnDeckOnEveryStatePath(t *testing.T) {
	ts := newTestServer(t, []int{0}, 0, 0)
	waitForPending(t, ts.reg, "t1", 1, 0)

	// One-shot poll.
	gv := assertStateCarriesOwnDeck(t, ts, "s0", 0)

	// Stream connect: the very first frame.
	s := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s.close()
	m1 := s.next(t)
	su, ok := m1.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("first stream frame is %T, want StateUpdate", m1.Value)
	}
	if su.GameView.OwnDeck == nil {
		t.Fatal("stream connect state omitted x_gorge_own_deck_v1")
	}
	if !reflect.DeepEqual(su.GameView.OwnDeck, gv.OwnDeck) {
		t.Fatalf("stream connect manifest differs from the poll manifest:\n stream %#v\n poll   %#v", su.GameView.OwnDeck, gv.OwnDeck)
	}

	// State after an intent: answer the open decision, then re-poll. The
	// manifest is genesis data, so the answer must not move a row.
	d := waitForPending(t, ts.reg, "t1", 1, 0)
	v, err := ts.reg.ViewAtSeat("t1", 1, headSeqFor(t, ts.reg, "t1", 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	pm, perr := manabrew.New("t1", 1, nil).Prompt(d, &v)
	if perr != nil {
		t.Fatalf("decision kind %s has no ManaBrew translation: %v", d.Kind, perr)
	}
	typ, out := firstLegalAnswer(t, pm)
	resp := mb.ClientMessage{Value: mb.ClientResponse{
		PromptID: pm.PromptID,
		Action:   mb.PromptOutput{Type: typ, Output: mb.PromptOutputData{Value: out}},
	}}
	if code, pe := send(t, ts, "s0", resp); code != 204 {
		t.Fatalf("answering %s: status %d, %+v", d.Kind, code, pe)
	}
	after := assertStateCarriesOwnDeck(t, ts, "s0", 0)
	if !reflect.DeepEqual(after.OwnDeck, gv.OwnDeck) {
		t.Fatalf("manifest moved across an intent:\n after %#v\n before %#v", after.OwnDeck, gv.OwnDeck)
	}

	// Reconnect: a fresh stream sees the identical manifest.
	s2 := openStream(t, ts.srv, "/t1/matches/1/stream", "s0")
	defer s2.close()
	m2 := s2.next(t)
	su2, ok := m2.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("reconnect first frame is %T, want StateUpdate", m2.Value)
	}
	if !reflect.DeepEqual(su2.GameView.OwnDeck, gv.OwnDeck) {
		t.Fatalf("reconnect manifest differs from the poll manifest:\n reconnect %#v\n poll      %#v", su2.GameView.OwnDeck, gv.OwnDeck)
	}
}

// TestOwnDeckExtensionIsOwnerOnly proves a seat's state carries exactly its
// own manifest and never another seat's: each human seat's extension equals
// the registry's manifest for ITS seat, the two are genuinely distinct in
// the fixture (so an accidental hardcode of seat 0's list onto seat 1's
// state fails loudly), and no prompt frame in either response carries the
// member (existing prompt messages are unchanged).
func TestOwnDeckExtensionIsOwnerOnly(t *testing.T) {
	ts := newTestServer(t, []int{0, 1}, 0, 0)
	waitForPending(t, ts.reg, "t1", 1, 0)
	// Seat 1 is NOT required to have a parked decision here: while seat 0's
	// human answer is outstanding, seat 1's state poll is a state-only
	// response, which is exactly the shape the owner-only check needs.

	gv0 := assertStateCarriesOwnDeck(t, ts, "s0", 0)
	gv1 := assertStateCarriesOwnDeck(t, ts, "s1", 1)
	if gv0.OwnDeck.Name == gv1.OwnDeck.Name {
		t.Fatalf("fixture precondition broken: seats 0 and 1 serve the same deck name %q, so an owner-only violation would be invisible", gv0.OwnDeck.Name)
	}
	if reflect.DeepEqual(gv0.OwnDeck, gv1.OwnDeck) {
		t.Fatal("fixture precondition broken: seats 0 and 1 have identical manifests")
	}

	// No prompt frame in either seat's response gains the member: the
	// extension is state-only, so existing prompt messages are unchanged.
	for _, tok := range []string{"s0", "s1"} {
		for _, raw := range rawStatePoll(t, ts, tok) {
			if !strings.Contains(string(raw), "x_gorge_own_deck_v1") {
				continue
			}
			var m mb.EngineMessage
			if _, derr := mb.Decode(raw, &m); derr != nil {
				t.Fatal(derr)
			}
			if _, ok := m.Value.(mb.PromptMessage); ok {
				t.Fatalf("%s's prompt message carries the own-deck extension: %s", tok, raw)
			}
		}
	}
}
