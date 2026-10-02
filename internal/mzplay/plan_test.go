package mzplay

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPlanGames(t *testing.T) {
	poolA := []string{"a0.dck", "a1.dck", "a2.dck", "a3.dck", "a4.dck"}
	poolB := []string{"b0.dck", "b1.dck", "b2.dck"}
	cfg := Config{Games: 40}
	cfg.A.DeckPoolMode, cfg.B.DeckPoolMode = "random", "random"
	p1, err := PlanGames(cfg, 99, poolA, poolB)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := PlanGames(cfg, 99, poolA, poolB)
	if !reflect.DeepEqual(p1, p2) {
		t.Fatal("one run seed planned two different runs")
	}
	p3, _ := PlanGames(cfg, 100, poolA, poolB)
	if reflect.DeepEqual(p1, p3) {
		t.Fatal("two run seeds planned one run")
	}
	seeds, decksA := map[uint64]bool{}, map[string]bool{}
	for i, g := range p1 {
		if g.Index != i || g.Seed != GameSeed(99, i) {
			t.Fatalf("game %d: %+v", i, g)
		}
		seeds[g.Seed] = true
		decksA[g.DeckA] = true
		if !strings.HasPrefix(g.DeckA, "a") || !strings.HasPrefix(g.DeckB, "b") {
			t.Fatalf("game %d drew %s / %s from the wrong pools", i, g.DeckA, g.DeckB)
		}
	}
	if len(seeds) != 40 || len(decksA) != len(poolA) {
		t.Fatalf("%d distinct seeds, %d of %d A decks used in 40 games", len(seeds), len(decksA), len(poolA))
	}
	// A prefix of a longer run is the same games: a game's plan depends on
	// its index, not on how many games the run has.
	cfg.Games = 10
	p4, _ := PlanGames(cfg, 99, poolA, poolB)
	if !reflect.DeepEqual(p4, p1[:10]) {
		t.Fatal("a shorter run of the same seed plays different games")
	}
	// sequential: the eval's pairing, line i against line i.
	cfg.A.DeckPoolMode, cfg.B.DeckPoolMode = "sequential", "sequential"
	seq, _ := PlanGames(cfg, 99, []string{"x", "y"}, []string{"y", "x"})
	for i, g := range seq {
		if g.DeckA != []string{"x", "y"}[i%2] || g.DeckB != []string{"y", "x"}[i%2] {
			t.Fatalf("sequential game %d: %s vs %s", i, g.DeckA, g.DeckB)
		}
	}
}

// The line draft-zero's stats.parse_summaries reads: the tag, then one JSON
// object with exactly upstream's keys.
func TestSummaryLine(t *testing.T) {
	r := GameResult{Seed: 0xfedcba9876543210, First: 1, Winner: 0, Turns: 9}
	r.Drawn[0] = []string{"Forest", "Llanowar Elves"}
	line := SummaryOf(3, r, "FDN_top_00001_WB", "FDN_top_00002_BR").Line()
	if !strings.HasPrefix(line, "GAME_SUMMARY {") {
		t.Fatalf("line %q", line)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line[len(SummaryTag):]), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"game": 3.0, "seed": float64(int64(-81985529216486896)), "deck_a": "FDN_top_00001_WB", "deck_b": "FDN_top_00002_BR",
		"first": "B", "winner": "A", "turns": 9.0, "drawn_a": []any{"Forest", "Llanowar Elves"}, "drawn_b": []any{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary:\n got %v\nwant %v", got, want)
	}
	if w := SummaryOf(1, GameResult{Winner: NoWinner, First: 0}, "a", "b"); w.Winner != "draw" || w.First != "A" {
		t.Fatalf("no winner: %+v", w)
	}
	if w := SummaryOf(1, GameResult{Winner: 1}, "a", "b"); w.Winner != "B" {
		t.Fatalf("B wins: %+v", w)
	}
}
