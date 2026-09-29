package main

// Cross-interface privacy acceptance over the REAL transports (ticket
// seat-deck-03-acceptance, spec
// docs/superpowers/specs/2026-09-29-seat-deck-manifest.md). This file mounts
// the native httpapi handler and the ManaBrew adapter on one mux against one
// registry and proves that, at the SAME match sequence:
//
//   - the owner's native /view own_deck, the negotiated ManaBrew state
//     extension, and the registry's own seat projection all agree on the
//     owner manifest;
//   - the opponent's view, a spectator view, the match metadata and a
//     feedback snapshot carry no owner-private card names, and a feedback
//     snapshot carries only the reporting seat's manifest.
//
// The seat/board/EnvSeat in-process agreement is the rules-package
// acceptance leaf (rules/seatdeck_manifest_acceptance_test.go); this file is
// the transport half, where the negotiated wire is actually mounted.
//
// The fixture is authored inline (Ruling P9): duplicate main-deck names, one
// legendary commander, and a sideboard.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/httpapi"
	"github.com/adams-shaun/gorge/host/manabrewhttp"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The fixture's card names. The commander is deliberately separate from the
// private names because a command zone is public (CR 903.6).
const (
	crossTwinName  = "Cross Acceptance Twin"
	crossSideName  = "Cross Acceptance Sideboard"
	crossCmdName   = "Cross Acceptance Commander"
	crossFiller    = "Cross Acceptance Filler"
	crossTable     = host.TableID("cx1")
	crossDeckOwner = "cx-owner"
	crossDeckOpp   = "cx-opp"
)

func crossParse(t *testing.T, script string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("cross-acceptance.txt", []byte(script))
	if len(diags) != 0 {
		t.Fatalf("parse %q: %v", script, diags)
	}
	c.Link()
	return c
}

// crossLoader serves the compact cross-interface fixture: seat "cx-owner"
// carries duplicate names and a sideboard plus a legendary commander; the
// opponent is plain filler.
func crossLoader(t *testing.T) func(string) (host.Deck, error) {
	t.Helper()
	twin := crossParse(t, "Name:"+crossTwinName+"\nTypes:Creature\nPT:2/2\n")
	cmd := crossParse(t, "Name:"+crossCmdName+"\nTypes:Legendary Creature\nPT:3/3\n")
	filler := crossParse(t, "Name:"+crossFiller+"\nTypes:Creature\nPT:1/1\n")
	side := crossParse(t, "Name:"+crossSideName+"\nTypes:Creature\nPT:1/1\n")
	owner := []*cards.Card{cmd, twin, twin, twin, twin, filler, filler, filler, filler, filler}
	opp := make([]*cards.Card, 10)
	for i := range opp {
		opp[i] = filler
	}
	opp[0] = cmd // every seat in a commander table names a commander
	return func(name string) (host.Deck, error) {
		switch name {
		case crossDeckOwner:
			return host.Deck{Name: name, Cards: owner, Sideboard: []*cards.Card{side, side}, Commanders: []int{0}}, nil
		case crossDeckOpp:
			return host.Deck{Name: name, Cards: opp, Commanders: []int{0}}, nil
		}
		return host.Deck{}, host.ErrNotFound
	}
}

// crossSeatClaim resolves the two seat tokens and leaves everything else a
// spectator, the same claim fence both handlers share.
func crossSeatClaim(r *http.Request) (httpapi.SeatClaim, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch tok {
	case "owner":
		return httpapi.SeatClaim{Table: crossTable, Seat: 0}, true
	case "opp":
		return httpapi.SeatClaim{Table: crossTable, Seat: 1}, true
	}
	return httpapi.SeatClaim{}, false
}

// crossServer mounts the native and ManaBrew handlers on one mux and parks
// the match on the first human decision so every interface is read at one
// stable sequence.
func crossServer(t *testing.T, dir string) (*httptest.Server, *host.Registry) {
	t.Helper()
	r, err := host.New(host.Options{Dir: dir, LoadDeck: crossLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := host.TableConfig{ID: crossTable, Name: "Cross", Seats: 2,
		Decks: []string{crossDeckOpp, crossDeckOwner}, Seed: 31,
		Spectator: view.Omniscient, Humans: []int{0, 1}, Format: host.FormatCommander}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(crossTable); err != nil {
		t.Fatal(err)
	}
	waitPendingAny(t, r)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewHandler(r, httpapi.Options{Seat: crossSeatClaim}))
	mux.Handle("/api/manabrew/v0/tables/", http.StripPrefix("/api/manabrew/v0/tables",
		manabrewhttp.NewHandler(r, manabrewhttp.Options{Seat: crossSeatClaim})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, r
}

func crossGet(t *testing.T, url, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

// TestSeatDeckManifestCrossInterfaceAgreement is the brief's central leaf: the
// native HTTP /view own_deck, the negotiated ManaBrew state extension, and the
// registry's own seat projection all carry the identical owner manifest at the
// same sequence.
func TestSeatDeckManifestCrossInterfaceAgreement(t *testing.T) {
	srv, r := crossServer(t, "")
	head := crossHead(t, r)

	// Native HTTP view for the owner.
	code, raw := crossGet(t, fmt.Sprintf("%s/api/tables/%s/matches/1/view?seat=0&seq=%d", srv.URL, crossTable, head), "owner")
	if code != http.StatusOK {
		t.Fatalf("native view: status %d: %s", code, raw)
	}
	var native struct {
		OwnDeck *struct {
			Name       string          `json:"name"`
			Main       []mb.OwnDeckRow `json:"main"`
			Sideboard  []mb.OwnDeckRow `json:"sideboard"`
			Commanders []string        `json:"commanders"`
		} `json:"own_deck"`
	}
	if err := json.Unmarshal(raw, &native); err != nil {
		t.Fatal(err)
	}
	if native.OwnDeck == nil {
		t.Fatalf("native /view omitted own_deck: %s", raw)
	}

	// ManaBrew negotiated state extension for the owner.
	code, rawMB := crossGet(t, fmt.Sprintf("%s/api/manabrew/v0/tables/%s/matches/1/state", srv.URL, crossTable), "owner")
	if code != http.StatusOK {
		t.Fatalf("manabrew state: status %d: %s", code, rawMB)
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMB, &msgs); err != nil || len(msgs) == 0 {
		t.Fatalf("manabrew state decode: %v (%s)", err, rawMB)
	}
	var em mb.EngineMessage
	if _, err := mb.Decode(msgs[0], &em); err != nil {
		t.Fatal(err)
	}
	su, ok := em.Value.(mb.StateUpdate)
	if !ok {
		t.Fatalf("manabrew first message is %T, want StateUpdate", em.Value)
	}
	ext := su.GameView.OwnDeck
	if ext == nil {
		t.Fatalf("manabrew state omitted x_gorge_own_deck_v1: %s", rawMB)
	}

	// The registry's own seat projection (what ViewAtSeat serves the native
	// client and the seat stream) is the third interface.
	v, err := r.ViewAtSeat(crossTable, 1, head, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.OwnDeck == nil {
		t.Fatal("registry seat projection dropped the manifest")
	}

	// Every interface names the same owner identity and the same rows.
	if v.OwnDeck.Name != crossDeckOwner || native.OwnDeck.Name != crossDeckOwner || ext.Name != crossDeckOwner {
		t.Fatalf("identity mismatch: registry=%q native=%q manabrew=%q", v.OwnDeck.Name, native.OwnDeck.Name, ext.Name)
	}
	if len(native.OwnDeck.Main) != len(ext.Main) || len(native.OwnDeck.Main) != len(v.OwnDeck.Main) {
		t.Fatalf("main row counts differ: native=%d manabrew=%d registry=%d", len(native.OwnDeck.Main), len(ext.Main), len(v.OwnDeck.Main))
	}
	for i := range v.OwnDeck.Main {
		if native.OwnDeck.Main[i].Name != v.OwnDeck.Main[i].Name || native.OwnDeck.Main[i].Count != v.OwnDeck.Main[i].Count {
			t.Fatalf("native main[%d] = %#v, registry = %#v", i, native.OwnDeck.Main[i], v.OwnDeck.Main[i])
		}
		if ext.Main[i].Name != v.OwnDeck.Main[i].Name || ext.Main[i].Count != v.OwnDeck.Main[i].Count {
			t.Fatalf("manabrew main[%d] = %#v, registry = %#v", i, ext.Main[i], v.OwnDeck.Main[i])
		}
	}
	// The fixture's duplicate names collapsed to a counted row, and the
	// sideboard survived into every interface.
	dupes := 0
	for _, row := range v.OwnDeck.Main {
		if row.Count > 1 {
			dupes++
		}
	}
	if dupes == 0 {
		t.Fatalf("precondition: no duplicate-collapsed row: %#v", v.OwnDeck.Main)
	}
	if len(native.OwnDeck.Sideboard) == 0 || len(ext.Sideboard) == 0 || len(v.OwnDeck.Sideboard) == 0 {
		t.Fatalf("sideboard lost: native=%d manabrew=%d registry=%d", len(native.OwnDeck.Sideboard), len(ext.Sideboard), len(v.OwnDeck.Sideboard))
	}
}

// TestSeatDeckManifestCrossInterfacePrivacy pins the privacy surface over the
// real transports.
func TestSeatDeckManifestCrossInterfacePrivacy(t *testing.T) {
	srv, r := crossServer(t, "")
	head := crossHead(t, r)
	privateNames := []string{crossTwinName, crossSideName}

	ownerRaw := crossViewRaw(t, srv, head, 0, "owner")
	if !strings.Contains(ownerRaw, crossTwinName) || !strings.Contains(ownerRaw, `"own_deck"`) {
		t.Fatalf("precondition: owner native view is not a positive control: %s", ownerRaw)
	}

	// Opponent's authenticated view: its OWN manifest, never the owner's.
	oppRaw := crossViewRaw(t, srv, head, 1, "opp")
	for _, n := range privateNames {
		if strings.Contains(oppRaw, n) {
			t.Fatalf("opponent view leaked %q: %s", n, oppRaw)
		}
	}
	if !strings.Contains(oppRaw, crossDeckOpp) {
		t.Fatalf("opponent view omitted its own manifest: %s", oppRaw)
	}

	// Spectator (no claim): no own_deck key at all.
	code, raw := crossGet(t, fmt.Sprintf("%s/api/tables/%s/matches/1/view?seq=%d", srv.URL, crossTable, head), "")
	if code != http.StatusOK {
		t.Fatalf("spectator view: %d %s", code, raw)
	}
	if strings.Contains(string(raw), `"own_deck"`) {
		t.Fatalf("spectator received own_deck: %s", raw)
	}

	// Match metadata (protocol.MatchInfo) carries no private card names.
	ms, err := r.Matches(crossTable)
	if err != nil || len(ms) == 0 {
		t.Fatalf("matches: %v %+v", err, ms)
	}
	meta, err := json.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range privateNames {
		if strings.Contains(string(meta), n) {
			t.Fatalf("match metadata leaked %q: %s", n, meta)
		}
	}

	// Feedback snapshot carries only the reporting seat's manifest.
	seat0 := state.PlayerID(0)
	snap, err := r.SnapshotForFeedback(crossTable, &seat0)
	if err != nil {
		t.Fatal(err)
	}
	if snap.View == nil || snap.View.OwnDeck == nil {
		t.Fatalf("feedback view carries no reporting-seat manifest: %+v", snap)
	}
	if snap.View.OwnDeck.Name != crossDeckOwner {
		t.Fatalf("feedback manifest = %q, want seat 0's %q", snap.View.OwnDeck.Name, crossDeckOwner)
	}
	viewJSON, err := json.Marshal(snap.View)
	if err != nil {
		t.Fatal(err)
	}
	// The reporting seat's own private names are present (it IS its own
	// manifest); the opponent's deck identity must not be.
	if !strings.Contains(string(viewJSON), crossTwinName) {
		t.Fatalf("precondition: reporting-seat manifest absent from feedback view")
	}
	if strings.Contains(string(viewJSON), crossDeckOpp) {
		t.Fatalf("feedback view carried another seat's manifest: %s", viewJSON)
	}
}

// TestSeatDeckManifestSurvivesPersistenceRestore proves the manifest is
// genesis-derived data that a persistence restore rebuilds identically. A
// live match on restore is deliberately recorded aborted (spec: restart
// aborts in-progress matches; M5 resumes), so the restored view is the
// archived match's, still projected at its own sequence.
func TestSeatDeckManifestSurvivesPersistenceRestore(t *testing.T) {
	dir := t.TempDir()
	_, r := crossServer(t, dir)
	head := crossHead(t, r)
	before, err := r.ViewAtSeat(crossTable, 1, head, 0)
	if err != nil {
		t.Fatal(err)
	}
	if before.OwnDeck == nil {
		t.Fatal("precondition: live view has no manifest to restore")
	}
	r.Close() // the cleanup's second Close is harmless

	r2, err := host.New(host.Options{Dir: dir, LoadDeck: crossLoader(t), Sleep: func(time.Duration, <-chan struct{}) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r2.Close() })
	ms, err := r2.Matches(crossTable)
	if err != nil || len(ms) == 0 {
		t.Fatalf("restored matches: %v %+v", err, ms)
	}
	if ms[0].Events < 1 {
		t.Fatalf("restored match has no events: %+v", ms[0])
	}
	head2 := uint64(ms[0].Events - 1)
	after, err := r2.ViewAtSeat(crossTable, 1, head2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if after.OwnDeck == nil {
		t.Fatal("restored view lost the manifest")
	}
	if after.OwnDeck.Name != before.OwnDeck.Name || len(after.OwnDeck.Main) != len(before.OwnDeck.Main) {
		t.Fatalf("restored manifest = %#v, want %#v", after.OwnDeck, before.OwnDeck)
	}
	for i := range before.OwnDeck.Main {
		if after.OwnDeck.Main[i] != before.OwnDeck.Main[i] {
			t.Fatalf("restored main[%d] = %#v, want %#v", i, after.OwnDeck.Main[i], before.OwnDeck.Main[i])
		}
	}
}

func crossViewRaw(t *testing.T, srv *httptest.Server, head uint64, seat state.PlayerID, token string) string {
	t.Helper()
	code, raw := crossGet(t, fmt.Sprintf("%s/api/tables/%s/matches/1/view?seat=%d&seq=%d", srv.URL, crossTable, seat, head), token)
	if code != http.StatusOK {
		t.Fatalf("view seat %d: %d %s", seat, code, raw)
	}
	return string(raw)
}

func crossHead(t *testing.T, r *host.Registry) uint64 {
	t.Helper()
	ms, err := r.Matches(crossTable)
	if err != nil || len(ms) != 1 {
		t.Fatalf("live matches: %v %+v", err, ms)
	}
	if ms[0].Events < 1 {
		t.Fatalf("live match has no events: %+v", ms[0])
	}
	return uint64(ms[0].Events - 1)
}

func waitPendingAny(t *testing.T, r *host.Registry) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("no human seat parked")
		}
		if _, err := r.Pending(crossTable, 1, 0); err == nil {
			return
		}
		if _, err := r.Pending(crossTable, 1, 1); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
