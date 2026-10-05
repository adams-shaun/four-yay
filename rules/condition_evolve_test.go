package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The bare `Condition$ Evolve` trigger clause (MKM Sharp-Eyed Rookie, EOE
// Evolving Adaptive). Forge writes the evolve ability word as a keyword
// (`Evolve$ True`, cards/kw_evolve.go) on most cards, but on these two it is a
// bare Condition$ on an ordinary Mode$ ChangesZone ETB trigger. Before this
// task the shared trigger walk read no bare Condition$ Evolve at all, so the
// trigger fired UNCONDITIONALLY -- a 2/2 entering under Sharp-Eyed Rookie's
// controller put a +1/+1 counter and investigated, although CR 702.99a
// requires the entering creature's power OR toughness to exceed the source's.
//
// The gate is event-relative (it compares the ENTERING creature to the
// source), so it lives in the per-mode ChangesZone matcher beside the
// Evolve$ gate, not in the shared triggerConditionHolds walk -- exactly the
// placement of Condition$ AttackedPlayerWithMostLife. It is fire-time only,
// matching Evolve$'s own treatment (the resolution-time CR 603.4 recheck has
// no entering object to re-derive it from).

// conditionEvolveGame is evolvedGame with the corpus TOKEN registry wired in
// (the shared helper omits it), so Sharp-Eyed Rookie's investigate rider can
// actually mint its Clue token rather than degrade to an unknown-token note.
func conditionEvolveGame(t *testing.T, seed uint64, names ...string) (*Engine, Config, map[string]*cards.Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	byName := make(map[string]*cards.Card, len(names))
	deck := make([]*cards.Card, 0, len(names))
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing corpus card %s", name)
		}
		byName[name] = c
		deck = append(deck, c)
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	return e, cfg, byName
}

// TestSharpEyedRookieEvolvesOnlyForABiggerCreature drives the headline
// carrier end to end. Sharp-Eyed Rookie is 2/2; a 2/2 (Grizzly Bears) enters
// first and must NOT evolve it, then a 6/4 (Craw Wurm) enters and MUST. The
// precondition assertions (the Rookie is on the battlefield with zero
// counters; the two entering creatures really have the sizes the conclusion
// depends on) make a vacuous setup fail loudly.
func TestSharpEyedRookieEvolvesOnlyForABiggerCreature(t *testing.T) {
	t.Parallel()
	const rookieName, equalName, bigName = "Sharp-Eyed Rookie", "Grizzly Bears", "Craw Wurm"
	e, cfg, _ := conditionEvolveGame(t, 71, rookieName, equalName, bigName)

	rookie := enterFromLibrary(t, e, 0, rookieName)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(rookie); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sharp-Eyed Rookie not on the battlefield: %+v", o)
	}
	// The Rookie's own entry must NOT evolve it: it is not strictly bigger
	// than itself, so the bare Condition$ Evolve gate must reject a
	// self-entry exactly as the Evolve$ keyword gate does. Before that gate
	// was read, the trigger fired on its own entry and left a counter here.
	if got := e.G.Obj(rookie).Counter("P1P1"); got != 0 {
		t.Fatalf("Sharp-Eyed Rookie has %d +1/+1 counters after its own entry, want 0", got)
	}

	// The equal-size creature must NOT evolve the Rookie.
	equal := enterFromLibrary(t, e, 0, equalName)
	if e.Power(equal) > e.Power(rookie) || e.Toughness(equal) > e.Toughness(rookie) {
		t.Fatalf("precondition: %s (%d/%d) must not exceed the Rookie (%d/%d)",
			equalName, e.Power(equal), e.Toughness(equal), e.Power(rookie), e.Toughness(rookie))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(rookie).Counter("P1P1"); got != 0 {
		t.Fatalf("Sharp-Eyed Rookie evolved for an equal-size creature: %d counters, want 0", got)
	}
	if n := clueTokenCount(e); n != 0 {
		t.Fatalf("Sharp-Eyed Rookie investigated for an equal-size creature: %d clues, want 0", n)
	}

	// The strictly bigger creature MUST evolve the Rookie.
	big := enterFromLibrary(t, e, 0, bigName)
	if e.Power(big) <= e.Power(rookie) && e.Toughness(big) <= e.Toughness(rookie) {
		t.Fatalf("precondition: %s (%d/%d) must be strictly bigger than the Rookie (%d/%d)",
			bigName, e.Power(big), e.Toughness(big), e.Power(rookie), e.Toughness(rookie))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(rookie).Counter("P1P1"); got != 1 {
		t.Fatalf("Sharp-Eyed Rookie +1/+1 counters = %d after a bigger creature entered, want 1", got)
	}
	if n := clueTokenCount(e); n != 1 {
		t.Fatalf("Sharp-Eyed Rookie clues = %d after evolving, want 1", n)
	}
	noUnimplementedAPI(t, e)
	replayCheck(t, e, cfg)
}

// TestEvolvingAdaptiveEvolvesOnlyForABiggerCreature drives the second corpus
// carrier. Evolving Adaptive enters with an oil counter and its ETB reads
// `ValidCard$ Creature.YouCtrl+Other` with the same `Condition$ Evolve`, so
// the same fix must gate it too. Its counter is OIL (not P1P1), so the test
// proves the gate is shared, not a Sharp-Eyed-Rookie special case.
//
// The equal-size step is load-bearing: before the gate was read the trigger
// fired UNCONDITIONALLY, so an equal-size creature (Llanowar Elves 1/1) also
// added the counter. The strictly-bigger step alone cannot tell the two
// behaviours apart -- both increment the counter -- so the negative assertion
// is the one that fails with the fix reverted.
func TestEvolvingAdaptiveEvolvesOnlyForABiggerCreature(t *testing.T) {
	t.Parallel()
	const adaptiveName, equalName, bigName = "Evolving Adaptive", "Llanowar Elves", "Craw Wurm"
	e, cfg, _ := conditionEvolveGame(t, 72, adaptiveName, equalName, bigName)

	adaptive := enterFromLibrary(t, e, 0, adaptiveName)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(adaptive); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Evolving Adaptive not on the battlefield: %+v", o)
	}
	// It enters with one oil counter (K:etbCounter:OIL:1); its P/T is 0/0
	// plus one per oil counter, so it is 1/1 on entry.
	if got := e.G.Obj(adaptive).Counter("OIL"); got != 1 {
		t.Fatalf("precondition: Evolving Adaptive oil counters = %d, want 1", got)
	}

	// An equal-size creature (1/1) must NOT evolve it: CR 702.99a requires
	// strictly greater power OR toughness. Before the gate was read the
	// trigger fired unconditionally and left a second oil counter here.
	equal := enterFromLibrary(t, e, 0, equalName)
	if e.Power(equal) > e.Power(adaptive) || e.Toughness(equal) > e.Toughness(adaptive) {
		t.Fatalf("precondition: %s (%d/%d) must not exceed Evolving Adaptive (%d/%d)",
			equalName, e.Power(equal), e.Toughness(equal), e.Power(adaptive), e.Toughness(adaptive))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(adaptive).Counter("OIL"); got != 1 {
		t.Fatalf("Evolving Adaptive evolved for an equal-size creature: %d oil counters, want 1", got)
	}

	// A strictly bigger creature MUST evolve it.
	big := enterFromLibrary(t, e, 0, bigName)
	if e.Power(big) <= e.Power(adaptive) && e.Toughness(big) <= e.Toughness(adaptive) {
		t.Fatalf("precondition: %s (%d/%d) must be strictly bigger than Evolving Adaptive (%d/%d)",
			bigName, e.Power(big), e.Toughness(big), e.Power(adaptive), e.Toughness(adaptive))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(adaptive).Counter("OIL"); got != 2 {
		t.Fatalf("Evolving Adaptive oil counters = %d after a bigger creature entered, want 2", got)
	}
	noUnimplementedAPI(t, e)
	replayCheck(t, e, cfg)
}

// clueTokenCount counts Clue tokens on the battlefield (Sharp-Eyed Rookie's
// investigate rider is real evidence that its whole ETB body ran).
func clueTokenCount(e *Engine) int {
	n := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if !o.IsToken || o.Face() == nil {
			continue
		}
		if strings.Contains(o.Face().Name, "Clue") {
			n++
		}
	}
	return n
}

// TestConditionEvolveCensus pins the bare `Condition$ Evolve` class: exactly
// the two corpus files named below carry it, and every one of them must be
// gated by the ChangesZone matcher. A new carrier fails here until it is
// classified; a classified carrier that disappears fails too.
func TestConditionEvolveCensus(t *testing.T) {
	t.Parallel()
	want := []string{"evolving_adaptive.txt", "sharp_eyed_rookie.txt"}
	var got []string
	dir := filepath.Join("..", ".cards", "cardsfolder")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("corpus cardsfolder: %v", err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		sub := filepath.Join(dir, ent.Name())
		files, err := os.ReadDir(sub)
		if err != nil {
			t.Fatalf("corpus dir %s: %v", sub, err)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".txt") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(sub, f.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", f.Name(), err)
			}
			if strings.Contains(string(b), "Condition$ Evolve") {
				got = append(got, f.Name())
			}
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Condition$ Evolve carriers = %v, want %v", got, want)
	}
}
