package host

// Task cli-20260922T150843Z-42f8f6dc, fix round sol2 finding 2: the NameCard
// decision is a match-semantic MODE, not merely data. A match played with a
// card-name universe poses a real name decision in its log; a match played
// without one (any pre-feature binary) never did. host/viewat.go rebuilds a
// finished match's replay Config from its sidecar ALONE (R-8.4), so the mode
// must persist: a pre-feature sidecar has no field, reads false, and its log
// — which recorded no name decision — must still replay. The gate is
// sidecar.NameUniverse, and TestNameUniverseModeGatesReplay is its proof:
// flipping the field on a real universe-backed log makes the replay diverge,
// so the flag is load-bearing, not decorative.

import (
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/view"
)

// parseNameLand is testutil's own parseCard shape (testutil cannot export it
// to host without widening that API for one test): parse, link the SVar /
// trigger chain, apply the intrinsics the engine expects.
func parseNameLand(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("host_namecard.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("parsing name-land: %v", diags)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c
}

// nameLandLoader serves a two-seat table of name-lands: a land whose own
// enters-the-battlefield trigger resolves a NameCard. The bot plays a land at
// sorcery speed on turn 1, so the match deterministically reaches a real
// mid-resolution name decision with no cast target to line up — the
// corpus-free analogue of Cabal Therapy, but driven through the whole host
// loop. The trigger's ValidCards$ Card.nonLand matches the real cards the
// brief names (Cabal Therapy, Phyrexian Revoker).
func nameLandLoader(t *testing.T) func(string) (Deck, error) {
	t.Helper()
	land := parseNameLand(t, "Name:Name Land\nTypes:Land\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigName | TriggerDescription$ choose a nonland name\n"+
		"SVar:TrigName:DB$ NameCard | Defined$ You | ValidCards$ Card.nonLand\nOracle:x\n")
	return func(name string) (Deck, error) {
		if name != "names" {
			return Deck{}, ErrNotFound
		}
		deck := make([]*cards.Card, 40)
		for i := range deck {
			deck[i] = land
		}
		return Deck{Name: "names", Cards: deck}, nil
	}
}

// nameUniverseTable is a two-seat table of name-lands, seeded the same as the
// mulligan table.
func nameUniverseTable(id TableID) TableConfig {
	return TableConfig{ID: id, Name: "Table " + string(id), Seats: 2, Decks: []string{"names", "names"},
		Seed: 99, Pace: 0, Spectator: view.Omniscient, Perpetual: false}
}

// TestNameUniverseModeGatesReplay is the R-8.4 regression for the new
// NameCard decision. It plays a corpus-backed match (opts.NameUniverse set)
// whose log carries a real name decision, then:
//
//   - the live sidecar records NameUniverse true and replays to the live head
//     with the universe restored (the post-feature restart);
//   - the SAME log replayed with NameUniverse false must take the pre-feature
//     no-universe path, so matchForLog must NOT inject the universe — asserted
//     on the rebuilt Config — and the replay must diverge (the log recorded a
//     name decision the no-universe path never poses). That the flip breaks
//     the replay is what proves the field, not its absence, selects the mode.
func TestNameUniverseModeGatesReplay(t *testing.T) {
	t.Parallel()
	takeMatchSlot(t)
	reg := testutil.CorpusRegistry(t)
	if reg.Len() == 0 {
		t.Fatal("precondition: corpus universe is empty")
	}
	r, err := New(Options{
		LoadDeck:     nameLandLoader(t),
		NameUniverse: reg.Universe(),
		Sleep:        func(d time.Duration, stop <-chan struct{}) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(nameUniverseTable("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.RLock()
	m := tb.history[0]
	tb.mu.RUnlock()
	if m == nil || m.state != protocol.MatchFinished {
		t.Fatalf("name-universe table did not finish: %+v", m)
	}
	// Precondition: the live log really carried a name decision, or the
	// divergence assertion below is vacuous.
	asks := 0
	for _, ev := range m.e.L.Events {
		if ev.Kind == events.DecisionAsk {
			asks++
		}
	}
	if asks == 0 {
		t.Fatalf("precondition: live log carries no decision; the match never reached a name ask")
	}
	liveHead := m.e.L.Head()

	sc := m.sidecar()
	if !sc.NameUniverse {
		t.Fatal("sidecar lost the name-universe mode: a corpus-backed match must record NameUniverse true")
	}

	// Post-feature restart: the universe is restored and the log replays.
	sm, err := r.matchForLog(tb, sc, m.e.L)
	if err != nil {
		t.Fatalf("universe-backed match does not replay: %v", err)
	}
	if sm.cfg.NameUniverse == nil {
		t.Fatal("matchForLog dropped the universe the sidecar recorded")
	}
	if got, want := sm.e.L.Head(), liveHead; got != want {
		t.Fatalf("universe replay head %s, live head %s", got, want)
	}

	// Pre-feature configuration: the sidecar field absent (false), as an old
	// binary wrote it. matchForLog must not inject the universe, and the
	// recorded name decision is what makes that observable.
	legacy := sc
	legacy.NameUniverse = false
	smLegacy, err := r.matchForLog(tb, legacy, m.e.L)
	if err == nil {
		// It replayed: matchForLog injected the universe despite the sidecar
		// saying the match had none, so the mode field is not load-bearing.
		t.Fatal("a log carrying a name decision replayed with NameUniverse false; matchForLog injected the universe anyway, so the sidecar mode gate is not load-bearing")
	}
	if smLegacy != nil {
		t.Fatalf("matchForLog returned a match alongside its error: %+v", smLegacy)
	}
}
