package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// TestSpellbenchGameSeedsMatchArena pins sbGameSeed to SpellBench's
// derive_game_seed (python/spellbench/arena/runner.py); the first three are
// also the published pauper-kernel ledger's m0000p0000, m0001p0000 and
// m0000p0001 seeds.
func TestSpellbenchGameSeedsMatchArena(t *testing.T) {
	for _, tc := range []struct {
		m, p int
		want uint64
	}{
		{0, 0, 3077497140376949},
		{1, 0, 6920715974369554},
		{0, 1, 470744788133123},
		{3, 17, 7894190567072193},
	} {
		if got := sbGameSeed(20260926, tc.m, tc.p); got != tc.want {
			t.Errorf("game_seed(m=%d, p=%d) = %d, want %d", tc.m, tc.p, got, tc.want)
		}
	}
}

func TestSpellbenchSchedule(t *testing.T) {
	bots := []string{"a", "b", "c"}
	pool := []string{"D1", "D2"}
	s := sbSchedule(bots, pool, 2, 1)
	// 3 matchups x (2 pairs/deck x 2 decks) pairs x 2 games.
	if len(s) != 3*4*2 {
		t.Fatalf("%d games", len(s))
	}
	g0, g1 := s[0], s[1]
	if g0.seed != g1.seed || g0.deck != g1.deck || g0.seats != [2]string{"a", "b"} || g1.seats != [2]string{"b", "a"} {
		t.Fatalf("pair not seat-swapped on one seed: %+v %+v", g0, g1)
	}
	var decks []string
	for _, g := range s[:8] {
		if g.game == 0 {
			decks = append(decks, g.deck)
		}
	}
	if !reflect.DeepEqual(decks, []string{"D1", "D2", "D1", "D2"}) {
		t.Fatalf("pair p plays pool[p %% len(pool)]: %v", decks)
	}
	if s[8].id != "m0001p0000g0" || s[8].seats != [2]string{"a", "c"} {
		t.Fatalf("second matchup starts %+v", s[8])
	}
}

// TestSpellbenchBuiltinsPlayMirrors plays every sb-* policy against sb-first
// on two catalog mirrors and checks each game finishes, replays exactly
// from its seed, and needs no refused-answer fallback on these seeds.
func TestSpellbenchBuiltinsPlayMirrors(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, deckID := range []string{"Burn", "Faeries"} {
		deck, err := spellbench.Deck(reg, spellbench.PauperKernel, deckID)
		if err != nil {
			t.Fatal(err)
		}
		for _, pol := range []string{"sb-uniform", "sb-heuristic", "sb-first", "sb-uniform-manual", "sb-heuristic-manual",
			"sb-uniform-planned", "sb-heuristic-planned"} {
			g := sbGame{id: "t", seed: sbGameSeed(20260926, 0, 0), deck: deckID, seats: [2]string{pol, "sb-heuristic"}}
			a := sbPlay(g, deck, reg, 200, 20000)
			b := sbPlay(g, deck, reg, 200, 20000)
			if a.err != nil {
				t.Fatalf("%s on %s: %v", pol, deckID, a.err)
			}
			if a.outcome != b.outcome {
				t.Fatalf("%s on %s: not deterministic: %+v vs %+v", pol, deckID, a.outcome, b.outcome)
			}
			if !reflect.DeepEqual(a.stats, b.stats) {
				t.Fatalf("%s on %s: seat stats not deterministic: %+v vs %+v", pol, deckID, a.stats, b.stats)
			}
			if strings.HasSuffix(pol, "-planned") {
				// The planned seat (p0) casts through lowered plans, and
				// lowers activated abilities from the planner's witness.
				// Every lowering is accounted for: it reached its cast, its
				// ability, or a named abort (none is left open at a
				// finished game). Almost every cast lowering reaches its
				// cast.
				st := a.stats[0]
				castLowerings := st.Lowerings - st.AbilityLowerings
				if st.Lowerings != st.LoweredCasts+st.LoweredAbilities+st.Aborts {
					t.Fatalf("%s on %s: unattributed lowering: lowerings %d = cast %d + ability %d + aborts %d (%v)?",
						pol, deckID, st.Lowerings, st.LoweredCasts, st.LoweredAbilities, st.Aborts, st.AbortsByCause)
				}
				if castLowerings == 0 || st.LoweredCasts*10 < castLowerings*9 {
					t.Fatalf("%s on %s: cast lowerings %d, cast %d, aborts %v", pol, deckID, castLowerings, st.LoweredCasts, st.AbortsByCause)
				}
			}
			if a.outcome.IsStalled() {
				t.Fatalf("%s on %s: stalled %+v", pol, deckID, a.outcome)
			}
			t.Logf("%-20s %-8s %s turns=%d intents=%d fallbacks=%v", pol, deckID, sbResultLabel(a), a.outcome.Turns, a.outcome.Intents, a.fallbacks)
		}
	}
}

// TestSpellbenchFDNCatalogMirrors plays two FDN Limited catalog mirrors
// (sb-first against sb-uniform) and checks each finishes without an engine
// halt and records the fdn-limited ledger format.
func TestSpellbenchFDNCatalogMirrors(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cat, err := spellbench.CatalogByID("fdn")
	if err != nil {
		t.Fatal(err)
	}
	pool := cat.Pool[:2]
	for _, g := range sbSchedule([]string{"sb-first", "sb-uniform"}, pool, 1, 20260928) {
		if g.game != 0 {
			continue // one game per deck
		}
		deck, err := spellbench.Deck(reg, cat.Dir, g.deck)
		if err != nil {
			t.Fatal(err)
		}
		r := sbPlay(g, deck, reg, 60, 20000)
		if r.err != nil {
			t.Fatalf("%s %s: %v", g.id, g.deck, r.err)
		}
		row := sbLedgerRow(g, r, map[string]string{}, cat.Format)
		if row["format"] != "fdn-limited-bo1" {
			t.Fatalf("format %v", row["format"])
		}
		t.Logf("%s %s: %s turns=%d intents=%d", g.id, g.deck, sbResultLabel(r), r.outcome.Turns, r.outcome.Intents)
	}
}

// TestSpellbenchTacticalDeterministic plays sb-tactical (auto-pay and
// planned) against sb-heuristic twice from the same seed on four catalog
// mirrors and requires the same event-log chain head (the game digest), a
// finished game and no refused-answer fallback.
func TestSpellbenchTacticalDeterministic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	setTacticalRegistry(reg)
	for _, deckID := range []string{"Burn", "Faeries", "Elves", "CawGates"} {
		deck, err := spellbench.Deck(reg, spellbench.PauperKernel, deckID)
		if err != nil {
			t.Fatal(err)
		}
		for _, pol := range []string{"sb-tactical", "sb-tactical-planned"} {
			seed := sbGameSeed(20260926, 3, 1)
			play := func() (string, gbench.Outcome, [2]int) {
				var res sbResult
				seats := []seat.Seat{policies[pol](seed ^ 1), policies["sb-heuristic"](seed ^ 2)}
				cfg := rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deck, deck},
					Tokens: reg.Tokens, NameUniverse: reg.Cards}
				o, e, err := gbench.PlayGame(cfg, seats, 200, 20000, gbench.Hooks{Submit: sbSubmitWithFallback(seats, &res)})
				if err != nil {
					t.Fatalf("%s on %s: %v", pol, deckID, err)
				}
				return e.L.Head(), o, res.fallbacks
			}
			h1, o1, fb := play()
			h2, o2, _ := play()
			if h1 != h2 || o1 != o2 {
				t.Fatalf("%s on %s: same seed, different games: %s %+v vs %s %+v", pol, deckID, h1, o1, h2, o2)
			}
			if o1.IsStalled() || fb != [2]int{} {
				t.Fatalf("%s on %s: stalled %+v or fallbacks %v", pol, deckID, o1, fb)
			}
			t.Logf("%-20s %-8s head %s winner p%d turns %d", pol, deckID, h1, o1.WinnerSeat, o1.Turns)
		}
	}
}
