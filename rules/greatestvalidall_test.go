package rules

// Task agent-20260918T222201Z-e27469dd: the two Desert-Bloom/OTC deck cards
// the brief names, pinned end to end on the REAL corpus cards (never inline
// scripts -- these are corpus pins, so the corpus registry is the source of
// truth and a corpus pin change fails loudly).
//
// WHAT PROVES WHICH ROUND (r2 review finding 1): the extreme-property
// GRAMMAR itself was merged in `1a16e76e`, an ancestor of this worktree's
// base -- so leaf 1 (TestReturnOfTheWildspeakerDrawMode…) exercises ONLY
// that merged grammar (`Count$Valid …$GreatestCardPower`) and PASSES with
// this round's diff reverted: it is regression coverage for `1a16e76e`, not
// this round's proof. This round's proof is leaf 2
// (TestCactusPreserveAnimatesAtGreatestCommanderManaValue -- the ValidAll
// all-zones scan, unknown before r1) and the effects-level
// TestEvalCountValidAllScansEveryCardZone / TestEvalCountValidZoneScanIsAllocationFree
// pins: the Cactus Preserve leaf and the alloc pin FAIL on the r1-base
// diff (verified by scratch-revert); TestEvalCountValidAllScansEveryCardZone
// passes on r1's count.go too (r1 already had the ValidAll branch -- only
// the pre-r1 base kills it), so it pins the head's zone semantics, not the
// round boundary.
//
// 1. Return of the Wildspeaker's draw mode sizes from
//    `Count$Valid Creature.YouCtrl+nonHuman$GreatestCardPower`: with two
//    tied 5-power non-Humans and a 6-power HUMAN on the battlefield the
//    spell draws exactly 5 -- the greatest non-Human power, the tie folded
//    deterministically and the Human excluded.
// 2. Cactus Preserve's animate sizes from
//    `Count$ValidAll Card.IsCommander+YouOwn$GreatestCardManaCost`: the
//    commanders sit in the COMMAND zone, so only the ValidAll all-zones
//    scan can see them; the land animates X/X at the greatest commander
//    mana value.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusFiller is n real corpus Mountains -- deck filler that triggers
// nothing, so the pins' draws and activations are the only moving parts.
func corpusFiller(t *testing.T, reg *cards.Registry, n int) []*cards.Card {
	t.Helper()
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	out := make([]*cards.Card, n)
	for i := range out {
		out[i] = m
	}
	return out
}

// greatestWildspeakerEngine builds a two-seat game seeded with the real
// Return of the Wildspeaker plus the named creature extras (all real corpus
// cards), returns the engine, its Config (for replayCheck) and the
// Wildspeaker moved to seat 0's hand.
func greatestWildspeakerEngine(t *testing.T, reg *cards.Registry, extras ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	deck := []*cards.Card{lookup(t, reg, "Return of the Wildspeaker")}
	for _, name := range extras {
		deck = append(deck, lookup(t, reg, name))
	}
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	fill := make([]*cards.Card, 40-len(deck))
	for i := range fill {
		fill[i] = m
	}
	cfg := seatZeroStart(Config{Seed: 311, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append(deck, fill...), corpusFiller(t, reg, 40)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Return of the Wildspeaker", state.ZHand)
	return e, cfg, id
}

func TestReturnOfTheWildspeakerDrawModeDrawsGreatestNonHumanPower(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name      string
		creatures []string
		powers    []int32
		want      int32
	}{
		// Two tied 5-power non-Humans (Krumar Bond-Kin, 5/3 Orc Warrior)
		// and a 6-power HUMAN (Kamahl, Pit Fighter, 6/1). The greatest
		// non-Human power is 5; had the nonHuman qualifier been ignored
		// the answer would read Kamahl's 6, had the reduction degraded to
		// the sum it would read 10, and a broken count would read 0.
		{name: "tie_at_five_human_excluded", creatures: []string{"Krumar Bond-Kin", "Krumar Bond-Kin", "Kamahl, Pit Fighter"}, powers: []int32{5, 5, 6}, want: 5},
		// A second board strength: the 6/6 Dreadmaw lifts the greatest
		// non-Human power to 6 -- a hard-coded 5 cannot pass both.
		{name: "six_power_board", creatures: []string{"Krumar Bond-Kin", "Kamahl, Pit Fighter", "Colossal Dreadmaw"}, powers: []int32{5, 6, 6}, want: 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, spell := greatestWildspeakerEngine(t, reg, tc.creatures...)
			for i, name := range tc.creatures {
				id := moveByName(t, e, 0, name, state.ZBattlefield)
				if got := e.Power(id); got != tc.powers[i] {
					t.Fatalf("precondition: %s power = %d, want %d", name, got, tc.powers[i])
				}
			}
			// The baseline is measured AFTER the creatures are placed (they
			// may have come out of the hand): the draw delta must be exactly
			// the spell's X, nothing else.
			before := len(e.G.Zone(state.ZHand, 0))
			addMana(t, e, 0, "GGGGG")
			d := castFixture(t, e, spell, -1)
			if d == nil || d.Kind != decision.KModes {
				t.Fatalf("expected the modal KModes ask, got %+v", d)
			}
			drawIdx := -1
			for _, o := range d.Options {
				if o.Kind == "mode" && len(o.Label) >= len("Draw cards equal to the greatest power") &&
					o.Label[:len("Draw cards equal to the greatest power")] == "Draw cards equal to the greatest power" {
					drawIdx = o.Index
				}
			}
			if drawIdx < 0 {
				t.Fatalf("draw mode not offered: %+v", d.Options)
			}
			submitChoices(t, e, drawIdx)
			passUntilStackEmpty(t, e, 20)
			after := len(e.G.Zone(state.ZHand, 0))
			// The spell itself left the hand for the graveyard, so the net
			// delta is X - 1.
			if after != before-1+int(tc.want) {
				t.Fatalf("hand %d -> %d, want %d (the spell leaves the hand, then it draws %d cards equal to the greatest non-Human power)", before, after, before-1+int(tc.want), tc.want)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestCactusPreserveAnimatesAtGreatestCommanderManaValue pins the ValidAll
// carrier: the land animates X/X, X the greatest mana value among the seat's
// commanders -- who sit in the COMMAND zone, the zone a battlefield-only
// scan never sees. The commanders are named through Config.Commanders deck
// indices, so genesis seats them event-sourced and a log-only replay
// rebuilds the whole setup. Both commanders are hybrid-free faces, so the
// mana values are unambiguous; a two-colour hybrid pip contributes one mana
// value.
func TestCactusPreserveAnimatesAtGreatestCommanderManaValue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name       string
		commanders []string
		cmc        []int32
		want       int32
	}{
		// Atla Palani, Nest Tender: {1}{R}{G}{W}, mana value 4.
		{name: "one_commander_mv4", commanders: []string{"Atla Palani, Nest Tender"}, cmc: []int32{4}, want: 4},
		// Toxrill, the Corrosive: {5}{B}{B}, mana value 7 -- the GREATEST of
		// the two, not the sum (11) and not the first (4).
		{name: "two_commanders_greatest_seven", commanders: []string{"Atla Palani, Nest Tender", "Toxrill, the Corrosive"}, cmc: []int32{4, 7}, want: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preserve := lookup(t, reg, "Cactus Preserve")
			deck := []*cards.Card{preserve}
			for _, name := range tc.commanders {
				deck = append(deck, lookup(t, reg, name))
			}
			cmdIdx := make([]int, len(tc.commanders))
			for i := range cmdIdx {
				cmdIdx[i] = 1 + i
			}
			cfg := seatZeroStart(Config{Seed: 97, Names: []string{"a", "b"}, Tokens: reg.Tokens,
				Commanders: [][]int{cmdIdx, nil},
				Decks:      [][]*cards.Card{append(deck, corpusFiller(t, reg, 40-len(deck))...), corpusFiller(t, reg, 40)}})
			e := New(cfg)
			e.Advance()
			toMain1(t, e)
			id := moveByName(t, e, 0, "Cactus Preserve", state.ZBattlefield)
			// Precondition: genesis seated exactly the named commanders in
			// the command zone, with the mana values this scenario's X
			// claims to fold.
			if got := len(e.G.Players[0].Commanders); got != len(tc.commanders) {
				t.Fatalf("precondition: %d commanders seated, want %d", got, len(tc.commanders))
			}
			for i, cid := range e.G.Players[0].Commanders {
				if got := e.G.Obj(cid).Face().Cmc(); got != tc.cmc[i] {
					t.Fatalf("precondition: commander %s mana value = %d, want %d", e.G.Obj(cid).Face().Name, got, tc.cmc[i])
				}
			}
			addMana(t, e, 0, "CCC")
			opt := animateAbilityOption(t, e, id)
			submitChoices(t, e, opt.Index)
			settleActivation(t, e)
			d := e.Derived(id)
			if d.Power != tc.want || d.Toughness != tc.want {
				t.Fatalf("animated Cactus Preserve = %d/%d, want %d/%d (the greatest mana value among the commanders)", d.Power, d.Toughness, tc.want, tc.want)
			}
			if !e.IsCreature(id) || !slices.Contains(d.Types, "Plant") || !slices.Contains(d.Types, "Land") {
				t.Fatalf("animated types = %v, want Creature+Plant (still a Land)", d.Types)
			}
			if !e.HasKeyword(id, "Reach") {
				t.Fatal("animated Cactus Preserve lacks reach")
			}
			replayCheck(t, e, cfg)
		})
	}
}
