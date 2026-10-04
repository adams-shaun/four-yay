package rules

// Kernel-era restorations of the flipcoin_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestFlippedCoinKarplusanMinotaur is the integration proof: the cumulative
// upkeep FlipCoin<1> cost action's flip — the ONE canonical result encoding
// shared with api:FlipCoin (rules/cumulative.go calls effects.FlipCoinNote) —
// fires Karplusan Minotaur's FlippedCoin triggers. Three upkeeps give 1+2+3
// flips; each flip Note queues exactly one of the two ValidResult$ triggers
// (Win → TrigYouDmg, Lose → TrigOppDmg), each resolving as one 1-damage hit.
func TestFlippedCoinKarplusanMinotaur(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := kr6KarplusanScenario(t, reg)
	wins, losses := 0, 0
	for _, win := range flipNotes(e) {
		if win {
			wins++
		} else {
			losses++
		}
	}
	if wins == 0 || losses == 0 {
		t.Fatalf("the six flips did not cover both ValidResult$ sides: %d wins, %d losses", wins, losses)
	}
}

// TestFlipCoinReplaysDeterministically pins the replayability contract: the
// same seed produces the same flip results, the same event chain and the same
// RNG-draw count — and the recorded log alone replays byte-identically.
func TestFlipCoinReplaysDeterministically(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e1, cfg := kr6KarplusanScenario(t, reg)
	e2, _ := kr6KarplusanScenario(t, reg)
	if !reflect.DeepEqual(e1.L.Events, e2.L.Events) {
		t.Fatalf("same seed produced different event chains")
	}
	if e1.RNGDraws() != e2.RNGDraws() {
		t.Fatalf("RNG draws differ: %d vs %d", e1.RNGDraws(), e2.RNGDraws())
	}
	if len(flipNotes(e1)) != 6 {
		t.Fatalf("want 6 flips across three upkeeps, got %d", len(flipNotes(e1)))
	}
	replayCheck(t, e1, cfg)
}

// kr6KarplusanScenario is karplusanScenario (flipcoin_test.go) for the
// kernel era: each upkeep's cumulative-upkeep trigger resolves as a kernel
// probe (kr6ResolveTop). It drives seat 0's Karplusan Minotaur through three upkeeps
// (1+2+3 = six cumulative-upkeep flips), paying every upkeep and resolving
// every FlippedCoin trigger. Returns the engine and the config a same-seed
// rerun can be compared against.
func kr6KarplusanScenario(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	e, cfg := flipEngine(t, reg, 42,
		[]*cards.Card{lookup(t, reg, "Karplusan Minotaur")}, []*cards.Card{})
	moveByName(t, e, 0, "Karplusan Minotaur", state.ZBattlefield)
	id := firstCreature(t, e, 0)
	// chosenTotal accumulates, per target identity, how many triggers have
	// chosen it across every turn so far. Every trigger deals exactly one
	// 1-damage hit to its chosen target, so the all-time hit count on that
	// target must equal this running total -- which is what ties each
	// trigger's damage to the target ITS side aimed (win and lose both aim
	// "any target", but only the lose side carries TargetingPlayer$
	// Opponent).
	chosenTotal := map[[2]int32]int{}
	for turn := int32(2); turn <= 4; turn++ {
		e.G.Turn = turn
		e.beginTurn(0)
		e.priorityRound()
		if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability.API != "CumulativeUpkeep" {
			t.Fatalf("turn %d: cumulative upkeep not placed: %v", turn, e.G.Stack)
		}
		kr6ResolveTop(e)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
			d.Options[0].Kind != "cumulative_pay" {
			t.Fatalf("turn %d: expected the pay-or-sacrifice ask, got %+v", turn, d)
		}
		before := len(flipNotes(e))
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("pay upkeep: %v", err)
		}
		flips := flipNotes(e)[before:]
		if len(flips) != int(turn)-1 {
			t.Fatalf("turn %d: want %d flips (1 per age counter), got %d",
				turn, turn-1, len(flips))
		}
		dmgBefore := countDamageAmount(e, 1)
		execs := drainFlippedCoinTriggers(t, e, len(flips))
		execMultisetMatches(t, execs, flips)
		if got := countDamageAmount(e, 1) - dmgBefore; got != len(flips) {
			t.Fatalf("turn %d: the triggers dealt %d one-damage hits, want %d",
				turn, got, len(flips))
		}
		// Tie each trigger's damage to the target that side's own target ask
		// was answered with. Several triggers may be answered with the SAME
		// target, so count the triggers per target and assert that target's
		// all-time 1-damage total equals the running total of triggers that
		// chose it -- each resolved body dealt its 1 to the target IT chose,
		// not merely that len(flips) hits happened somewhere.
		chosen := map[[2]int32]int{}
		for _, x := range execs {
			key := [2]int32{int32(x.target.Obj), int32(x.target.Player)}
			if x.target.Obj == 0 {
				key[0] = -1
			}
			chosen[key]++
			chosenTotal[key]++
		}
		keys := make([][2]int32, 0, len(chosenTotal))
		for key := range chosenTotal {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		for _, key := range keys {
			if got, want := hitsOn1(e, key), chosenTotal[key]; got != want {
				t.Fatalf("turn %d: target %v has taken %d one-damage hits, want %d (one per trigger that chose it)",
					turn, key, got, want)
			}
		}
	}
	if got := e.G.Obj(id).Counter("AGE"); got != 3 {
		t.Fatalf("three upkeeps left AGE=%d, want 3", got)
	}
	return e, cfg
}
