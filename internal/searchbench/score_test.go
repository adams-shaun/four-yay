package searchbench

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

// benchManifest is a synthetic, sealed, test-only manifest with two items per
// game and label rates near sb-v1's (spell Pass lenient ~60%, attack 46%,
// block 20%).
func benchManifest(games int, seed uint64) Manifest {
	sha := strings.Repeat("a", 64)
	rng := rand.New(rand.NewPCG(seed, 1))
	m := Manifest{
		Kind: ManifestKind, SchemaVersion: ManifestSchemaVersion,
		Dataset:   Dataset{Name: "synthetic", License: "CC BY 4.0", URI: "https://example.invalid", SHA256: sha},
		Corpus:    Corpus{ForgeRef: strings.Repeat("b", 40), CompilerFingerprint: "unit-test"},
		Selection: Selection{Test: 2 * games, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2},
	}
	for g := 0; g < games; g++ {
		for k := 0; k < 2; k++ {
			n := len(m.Items)
			it := Item{ID: fmt.Sprintf("test-%05d", n), GameID: fmt.Sprintf("g%04d", g), DraftID: fmt.Sprintf("d%04d", g), Row: 17 + 36*g,
				Split: SplitTest, Type: DecisionTypes[(g+k*3)%4], Turn: 3 + n%10, Sequence: uint64(n + 1), Tier: "T0",
				PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha, WorldSeeds: []uint64{1, 2, 3, 4, 5, 6, 7, 8}}
			switch it.Type {
			case DecisionSpell:
				casts := 1 + rng.IntN(4)
				it.Options = []string{"Pass"}
				for c := 1; c <= casts; c++ {
					it.Options = append(it.Options, fmt.Sprintf("Cast %d", c))
				}
				first := 1 + rng.IntN(casts)
				it.Label.Strict = [][]int{{first}}
				if first < casts && rng.IntN(3) == 0 {
					it.Label.Strict = append(it.Label.Strict, []int{casts})
				}
				it.Label.Alternatives = it.Label.Strict
				if rng.IntN(10) < 6 {
					it.Label.Alternatives = append([][]int{{0}}, it.Label.Strict...)
				}
				it.Label.Act = true
			case DecisionHold:
				it.Options = []string{"Pass"}
				for c := 0; c <= rng.IntN(3); c++ {
					it.Options = append(it.Options, fmt.Sprintf("Cast %d", c+1))
				}
				it.Label = Label{Alternatives: [][]int{{0}}, Strict: [][]int{{0}}}
			case DecisionAttack:
				it.Focus = "Bear"
				it.Options = []string{"no", "yes"}
				it.Label = Label{Alternatives: [][]int{{0}}, Strict: [][]int{{0}}}
				if rng.IntN(100) < 46 {
					it.Label = Label{Alternatives: [][]int{{1}}, Strict: [][]int{{1}}, Act: true}
				}
			case DecisionBlock:
				it.Focus = "Wall"
				it.Options = []string{"no block", "Ogre"}
				if rng.IntN(2) == 0 {
					it.Options = append(it.Options, "Goblin")
				}
				it.Label = Label{Alternatives: [][]int{{0}}, Strict: [][]int{{0}}}
				if rng.IntN(100) < 20 {
					c := 1 + rng.IntN(len(it.Options)-1)
					it.Label = Label{Alternatives: [][]int{{c}}, Strict: [][]int{{c}}, Act: true}
				}
			}
			m.Items = append(m.Items, it)
		}
	}
	if err := m.Seal(); err != nil {
		panic(err)
	}
	return m
}

// benchArm answers each item: the human's first strict alternative with
// probability agree, otherwise a uniform option; it also fills a root table.
func benchArm(m Manifest, arm string, agree float64, seed uint64) []Result {
	rng := rand.New(rand.NewPCG(seed, 2))
	var out []Result
	for _, it := range m.Items {
		c := rng.IntN(len(it.Options))
		if rng.Float64() < agree {
			c = it.Label.Strict[0][0]
		}
		r := Result{ManifestDigest: m.Digest, Arm: arm, ItemID: it.ID, Seed: seed, AgentChoices: []int{c}, AgentAct: c != 0, Sims: 100, Completed: 100,
			MeanLeafPlies: 5, MeanLeafEdges: 3, MeanTurnsCrossed: .25, EnvSteps: 120, CoreSeconds: .01}
		for o := range it.Options {
			v := 1 + rng.IntN(20)
			if o == c {
				v += 40
			}
			r.Root = append(r.Root, RootOption{Choice: o, Visits: v, Q: .4 + .2*rng.Float64()})
		}
		out = append(out, r)
	}
	return out
}

func bindT(t *testing.T, m Manifest, rows []Result) Run {
	t.Helper()
	run, err := BindResults(m, SplitTest, rows)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func val(t *testing.T, e map[string]Estimate, k string) float64 {
	t.Helper()
	if e[k].Value == nil {
		t.Fatalf("%s undefined", k)
	}
	return *e[k].Value
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestConstantArmsScoreBalancedExactlyHalf(t *testing.T) {
	m := benchManifest(120, 3)
	arms, err := Baselines(m, 9)
	if err != nil {
		t.Fatal(err)
	}
	for _, arm := range []string{BaselinePassive, BaselineActive} {
		e := DefaultBootstrap.Estimates(bindT(t, m, arms[arm]))
		for _, k := range []string{"balanced", "bal_cast", "bal_attack", "bal_block"} {
			if got := val(t, e, k); !near(got, .5) {
				t.Fatalf("%s %s = %v, want exactly 0.5", arm, k, got)
			}
			if ci := e[k].CI; ci == nil || !near(ci[0], .5) || !near(ci[1], .5) {
				t.Fatalf("%s %s CI = %v, want [0.5, 0.5]", arm, k, ci)
			}
		}
	}
	e := DefaultBootstrap.Estimates(bindT(t, m, arms[BaselinePassive]))
	for _, k := range []string{"act_spell", "act_hold", "act_attack", "act_block"} {
		if val(t, e, k) != 0 {
			t.Fatalf("passive %s = %v", k, val(t, e, k))
		}
	}
	if e["which_spell"].Value != nil || e["which_block"].Value != nil {
		t.Fatal("passive arm has a which-score, but it never acts")
	}
	// A_set of always-passive is analytic: the share of items whose lenient
	// label accepts option 0, macro over types (analyze.py references()).
	var num [4]float64
	var den [4]int
	for _, it := range m.Items {
		i := typeIndex(it.Type)
		den[i]++
		if containsChoice(it.Label.Alternatives, 0) {
			num[i]++
		}
	}
	want, _ := macro(num, den)
	if got := val(t, e, "a_set"); !near(got, want) {
		t.Fatalf("passive A_set = %v, want %v", got, want)
	}
	if got := val(t, e, "a_set_hold"); got != 1 {
		t.Fatalf("passive hold A_set = %v, want 1", got)
	}
}

func TestStrictDropsTheLenientPass(t *testing.T) {
	m := testManifest() // test-0001: spell, label {Pass, Cast A, Cast C}, strict {Cast A, Cast C}
	for _, tc := range []struct {
		choice             int
		set, strict, which float64
		whichDefined       bool
	}{
		{choice: 0, set: 1, strict: 0},
		{choice: 1, set: 1, strict: 1, which: 1, whichDefined: true},
		{choice: 2, set: 0, strict: 0, which: 0, whichDefined: true},
	} {
		rows := []Result{
			{ManifestDigest: m.Digest, Arm: "a", ItemID: "dev-0001", AgentChoices: []int{0}},
			{ManifestDigest: m.Digest, Arm: "a", ItemID: "test-0001", AgentChoices: []int{tc.choice}, AgentAct: tc.choice != 0},
		}
		e := Bootstrap{}.Estimates(bindT(t, m, rows))
		if val(t, e, "a_set") != tc.set || val(t, e, "a_strict") != tc.strict || val(t, e, "a_set_spell") != tc.set || val(t, e, "a_strict_spell") != tc.strict {
			t.Fatalf("choice %d: set/strict = %v/%v", tc.choice, val(t, e, "a_set"), val(t, e, "a_strict"))
		}
		if (e["which_spell"].Value != nil) != tc.whichDefined || (tc.whichDefined && val(t, e, "which_spell") != tc.which) {
			t.Fatalf("choice %d: which_spell = %v", tc.choice, e["which_spell"])
		}
		// A spell item's human always acted, even when Pass is accepted, so
		// cast-or-hold has no waiting class here and is undefined.
		if e["bal_cast"].Value != nil || e["balanced"].Value != nil {
			t.Fatalf("choice %d: one-class balanced accuracy is defined", tc.choice)
		}
		if val(t, e, "human_act_spell") != 1 {
			t.Fatal("spell human act rate is not 1")
		}
	}
}

// TestHandComputedMetrics checks every metric on a hand-sized set.
func TestHandComputedMetrics(t *testing.T) {
	sha := strings.Repeat("a", 64)
	mk := func(id string, row int, typ DecisionType, opts []string, alts, strict [][]int, act bool) Item {
		focus := ""
		if typ == DecisionAttack || typ == DecisionBlock {
			focus = "X"
		}
		return Item{ID: id, GameID: fmt.Sprint("g", row), DraftID: fmt.Sprint("d", row), Row: row, Split: SplitTest, Type: typ, Turn: 3, Sequence: 1, Tier: "T0",
			Options: opts, Focus: focus, PrefixDigest: sha, PublicStateDigest: sha, LegalOptionsDigest: sha, Label: Label{Alternatives: alts, Strict: strict, Act: act}, WorldSeeds: []uint64{1, 2, 3, 4, 5, 6, 7, 8}}
	}
	spell := []string{"Pass", "Cast A", "Cast B", "Cast C"}
	m := Manifest{Kind: ManifestKind, SchemaVersion: ManifestSchemaVersion,
		Dataset:   Dataset{Name: "hand", License: "CC BY 4.0", URI: "https://example.invalid", SHA256: sha},
		Corpus:    Corpus{ForgeRef: strings.Repeat("b", 40), CompilerFingerprint: "unit-test"},
		Selection: Selection{Test: 8, MinimumGameWinRate: .6, MinimumGames: 100, MaximumItemsPerGame: 2},
		Items: []Item{
			mk("t1", 1, DecisionSpell, spell, [][]int{{0}, {1}}, [][]int{{1}}, true),
			mk("t2", 1, DecisionSpell, spell, [][]int{{2}}, [][]int{{2}}, true),
			mk("t3", 2, DecisionHold, []string{"Pass", "Cast A"}, [][]int{{0}}, [][]int{{0}}, false),
			mk("t4", 2, DecisionHold, []string{"Pass", "Cast A", "Cast B"}, [][]int{{0}}, [][]int{{0}}, false),
			mk("t5", 3, DecisionAttack, []string{"no", "yes"}, [][]int{{1}}, [][]int{{1}}, true),
			mk("t6", 3, DecisionAttack, []string{"no", "yes"}, [][]int{{0}}, [][]int{{0}}, false),
			mk("t7", 4, DecisionBlock, []string{"no", "Ogre", "Goblin"}, [][]int{{2}}, [][]int{{2}}, true),
			mk("t8", 4, DecisionBlock, []string{"no", "Ogre", "Goblin"}, [][]int{{0}}, [][]int{{0}}, false),
		}}
	if err := m.Seal(); err != nil {
		t.Fatal(err)
	}
	choices := map[string]int{"t1": 0, "t2": 3, "t3": 1, "t4": 0, "t5": 1, "t6": 1, "t7": 1, "t8": 0}
	var rows []Result
	for _, it := range m.Items {
		c := choices[it.ID]
		rows = append(rows, Result{ManifestDigest: m.Digest, Arm: "hand", ItemID: it.ID, AgentChoices: []int{c}, AgentAct: c != 0})
	}
	e := Bootstrap{}.Estimates(bindT(t, m, rows))
	want := map[string]float64{
		// spell 1/2 (t1 Pass lenient), hold 1/2, attack 1/2, block 1/2.
		"a_set": .5, "a_set_spell": .5, "a_set_hold": .5, "a_set_attack": .5, "a_set_block": .5,
		// strict: spell 0/2.
		"a_strict": (0 + .5 + .5 + .5) / 4, "a_strict_spell": 0,
		// cast or hold: human act t1,t2 (agent pass, cast) -> recall 1/2; human wait t3,t4 (cast, pass) -> 1/2.
		"bal_cast": .5,
		// attack: t5 yes/yes, t6 no/yes -> (1 + 0)/2. block: t7 yes/yes(wrong attacker), t8 no/no -> (1+1)/2.
		"bal_attack": .5, "bal_block": 1, "balanced": (.5 + .5 + 1) / 3,
		// which: spell t2 both act, Cast C not in strict {Cast B}: 0. block t7 both act, Ogre != Goblin: 0.
		"which_spell": 0, "which_block": 0,
		"act_spell": .5, "act_hold": .5, "act_attack": 1, "act_block": .5,
		"human_act_spell": 1, "human_act_hold": 0, "human_act_attack": .5, "human_act_block": .5,
		"passive_share": 3.0 / 8, "human_passive_share": .5,
		"attack_when_human_did_not": 1, "block_when_human_did_not": 0,
		"spell_cast_humans": 0, "spell_cast_other": .5, "spell_pass": .5,
	}
	for k, w := range want {
		if got := val(t, e, k); !near(got, w) {
			t.Errorf("%s = %v, want %v", k, got, w)
		}
	}
	if e["a_soft"].Value != nil {
		t.Error("a_soft defined without root tables")
	}
	c, per, ok := Chance(m.Items)
	// spell (2/4 + 1/4)/2, hold (1/2 + 1/3)/2, attack 1/2, block 1/3.
	wantPer := [4]float64{.375, (.5 + 1.0/3) / 2, .5, 1.0 / 3}
	if !ok || !near(per[0], wantPer[0]) || !near(per[1], wantPer[1]) || !near(per[2], wantPer[2]) || !near(per[3], wantPer[3]) || !near(c, (wantPer[0]+wantPer[1]+wantPer[2]+wantPer[3])/4) {
		t.Fatalf("chance = %v %v", c, per)
	}
}

func TestUniformRandomBaselineApproachesChance(t *testing.T) {
	m := benchManifest(1500, 5)
	arms, err := Baselines(m, 11)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Baselines(m, 11)
	for i := range arms[BaselineRandom] {
		if arms[BaselineRandom][i].AgentChoices[0] != again[BaselineRandom][i].AgentChoices[0] {
			t.Fatal("random baseline is not deterministic")
		}
	}
	chance, _, _ := Chance(m.Items)
	e := Bootstrap{}.Estimates(bindT(t, m, arms[BaselineRandom]))
	if got := val(t, e, "a_set"); math.Abs(got-chance) > .03 {
		t.Fatalf("random A_set %v far from chance %v", got, chance)
	}
	for _, r := range arms[BaselineActive] {
		if r.AgentChoices[0] != 1 {
			t.Fatal("active baseline did not pick option 1")
		}
	}
}
