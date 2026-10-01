package searchbench

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func repeat(label string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = label
	}
	return out
}

// TestPermutationPMatchesUpstream checks PermutationP against leak.py's
// _fisher_2xk on the same samples (values from the pinned clone's Python):
// pyRandom replays random.Random(0).shuffle exactly.
func TestPermutationPMatchesUpstream(t *testing.T) {
	cases := []struct {
		a, b []string
		want float64
	}{
		{append(repeat("Pass", 10), repeat("Cast", 6)...), repeat("Cast", 16), 0.00024993751562109475},
		{append(repeat("Pass", 3), repeat("Cast", 13)...), append(repeat("Pass", 2), repeat("Cast", 14)...), 1.0},
		{repeat("Cast", 16), append(repeat("Cast", 14), repeat("Pass", 2)...), 0.48037990502374406},
		{repeat("Cast", 5), repeat("Cast", 5), 1.0},
	}
	for i, c := range cases {
		if got := PermutationP(c.a, c.b); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("case %d: p = %v, upstream %v", i, got, c.want)
		}
	}
}

// TestAnalyzeLeakRule applies the §2.5 rule to synthetic rows: identical
// members pass with ΔQ exactly 0; a Q gap past 0.05 on gorge's scale (0.1
// on upstream's) fails; a canary in a walked world fails.
func TestAnalyzeLeakRule(t *testing.T) {
	var rows []LeakRow
	for s := 0; s < 8; s++ {
		for _, w := range []string{"X", "Y"} {
			root := []LeakOption{{Label: "Pass", Visits: 40, Q: 0.5}, {Label: "Cast A", Visits: 60, Q: 0.6}}
			rows = append(rows, LeakRow{Pair: "counterspell", World: w, Arm: "pimc-1", Seed: s, Sims: 100, Best: "Cast A", Root: root})
			gap := root
			if w == "X" {
				gap = []LeakOption{{Label: "Pass", Visits: 60, Q: 0.5}, {Label: "Cast A", Visits: 40, Q: 0.4 + 0.001*float64(s)}}
			}
			best := "Cast A"
			if w == "X" {
				best = "Pass"
			}
			rows = append(rows, LeakRow{Pair: "counterspell", World: w, Arm: "clairvoyant-mcts", Seed: s, Sims: 100, Best: best, Root: gap})
			var hidden []string
			if w == "X" && s == 3 {
				hidden = []string{LeakCanary}
			}
			rows = append(rows, LeakRow{Pair: "canary", World: w, Arm: "pimc-1", Seed: s, Sims: 100, Best: "Cast A", Root: root, Hidden: hidden})
		}
	}
	vs := AnalyzeLeak(rows, []string{"counterspell", "canary"}, []string{"clairvoyant-mcts", "pimc-1"})
	if len(vs) != 3 {
		t.Fatalf("%d verdicts", len(vs))
	}
	fair, cv, can := vs[1], vs[0], vs[2]
	if fair.Arm != "pimc-1" || fair.Pair != "counterspell" || !fair.Pass || fair.Identical != 8 || fair.PermP != 1 {
		t.Errorf("fair: %+v", fair)
	}
	for _, o := range fair.Options {
		if o.DQ != 0 || o.CI == nil || *o.CI != [2]float64{0, 0} || o.MaxAbs != 0 {
			t.Errorf("fair option %+v", o)
		}
	}
	if cv.Arm != "clairvoyant-mcts" || cv.Pass || cv.QInside || cv.Worst != "Cast A" || cv.PermP > 0.01 {
		t.Errorf("clairvoyant: %+v", cv)
	}
	// Cast A: X - Y = 2*(0.4+0.001s - 0.6) on upstream's scale.
	if got := cv.Options[0]; got.Label != "Cast A" || math.Abs(got.DQ-2*(0.4035-0.6)) > 1e-9 {
		t.Errorf("clairvoyant Cast A: %+v", got)
	}
	if can.Pair != "canary" || can.Pass || can.CanaryX == nil || *can.CanaryX != 1 || *can.CanaryY != 0 || !can.QInside {
		t.Errorf("canary: %+v", can)
	}
}

func leakDeck(spec ...any) []string {
	var out []string
	for i := 0; i < len(spec); i += 2 {
		out = append(out, repeat(spec[i].(string), spec[i+1].(int))...)
	}
	return out
}

// leakSpec is a synthetic StateSpec in the shape of leak.py's positions
// (no 17lands deck in it): a and b are the seats' fields over defaults.
func leakSpec(t *testing.T, turn int, step string, a, b map[string]any) json.RawMessage {
	t.Helper()
	seat := func(name string, over map[string]any) map[string]any {
		p := map[string]any{"name": name, "life": 16, "decklistSource": "exact", "landsPlayed": 0, "battlefield": []any{}}
		for k, v := range over {
			p[k] = v
		}
		return p
	}
	raw, err := json.Marshal(map[string]any{
		"version": 1, "turn": turn, "activePlayer": "A", "phase": step, "step": step, "enterMode": "PRIORITY_FRESH",
		"players":    map[string]any{"A": seat("PlayerA", a), "B": seat("PlayerB", b)},
		"provenance": map[string]any{"source": "synthetic", "ref": "leak_test.go", "tier": "T0"},
		"labels":     map[string]any{"decisionPlayer": "A"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// syntheticLeakFile is two probes in leak.py's shape, built from basic
// lands and a few FDN cards: canary (B holds Counterspell x2 / Island x2,
// its belief list holds no Counterspell) and cantrip_x (A's next five draws
// are Llanowar Elves / Plains, which its public view drops). Each member's
// eight worlds are its public view, as public_worlds draws them: B's hand a
// count over a belief list, A's library top dropped.
func syntheticLeakFile(t *testing.T) *LeakFile {
	pf := func(n string, c int) map[string]any { return map[string]any{"name": n, "count": c} }
	angelA := map[string]any{"landsPlayed": 1, "hand": []string{"Serra Angel"},
		"decklist":    leakDeck("Plains", 17, "Forest", 17, "Serra Angel", 2, "Helpful Hunter", 2, "Cathar Commando", 2),
		"battlefield": []any{pf("Plains", 3), pf("Forest", 3)}}
	canaryB := func(hand ...string) map[string]any {
		return map[string]any{"hand": hand, "battlefield": []any{pf("Island", 3), pf("Mountain", 2)},
			"decklist": leakDeck("Island", 17, "Mountain", 17, "Counterspell", 2, "Erudite Wizard", 4)}
	}
	beliefB := map[string]any{"handUnknown": 2, "decklistSource": "belief", "battlefield": []any{pf("Island", 3), pf("Mountain", 2)},
		"decklist": leakDeck("Island", 18, "Mountain", 18, "Erudite Wizard", 4)}
	hunterA := func(top string) map[string]any {
		a := map[string]any{"landsPlayed": 1, "hand": []string{"Helpful Hunter", "Cathar Commando"},
			"decklist":    leakDeck("Plains", 15, "Forest", 15, "Llanowar Elves", 5, "Helpful Hunter", 2, "Cathar Commando", 3),
			"battlefield": []any{pf("Plains", 1), pf("Forest", 2)}}
		if top != "" {
			a["libraryTop"] = repeat(top, 5)
		}
		return a
	}
	cantripB := map[string]any{"hand": []string{"Island", "Mountain", "Erudite Wizard"}, "battlefield": []any{pf("Island", 2), pf("Mountain", 2)},
		"decklist": leakDeck("Island", 17, "Mountain", 17, "Erudite Wizard", 6)}
	beliefCantripB := map[string]any{"handUnknown": 3, "decklistSource": "belief", "battlefield": []any{pf("Island", 2), pf("Mountain", 2)},
		"decklist": leakDeck("Island", 17, "Mountain", 17, "Erudite Wizard", 6)}
	member := func(real, world json.RawMessage) LeakMember {
		return LeakMember{Real: real, Worlds: [][]json.RawMessage{repeatRaw(world, WorldCount)}}
	}
	canaryWorld := leakSpec(t, 7, "POSTCOMBAT_MAIN", angelA, beliefB)
	cantripWorld := leakSpec(t, 5, "PRECOMBAT_MAIN", hunterA(""), beliefCantripB)
	return &LeakFile{Format: LeakFormat, Seeds: 1, Pairs: []LeakPair{
		{Name: "canary",
			X: member(leakSpec(t, 7, "POSTCOMBAT_MAIN", angelA, canaryB("Counterspell", "Counterspell")), canaryWorld),
			Y: member(leakSpec(t, 7, "POSTCOMBAT_MAIN", angelA, canaryB("Island", "Island")), canaryWorld)},
		{Name: "cantrip_x",
			X: member(leakSpec(t, 5, "PRECOMBAT_MAIN", hunterA("Llanowar Elves"), cantripB), cantripWorld),
			Y: member(leakSpec(t, 5, "PRECOMBAT_MAIN", hunterA("Plains"), cantripB), cantripWorld)},
	}}
}

func repeatRaw(r json.RawMessage, n int) []json.RawMessage {
	out := make([]json.RawMessage, n)
	for i := range out {
		out[i] = r
	}
	return out
}

// TestLeakProbesFixture runs synthetic probes in leak.py's shape (canary
// and cantrip_x) through gorge: an honest arm's two members
// search identically to the bit and never see the canary, and a planted leak -- PIMC handed the real position as its
// world -- is caught by both the ΔQ and the canary checks.
func TestLeakProbesFixture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	lf := syntheticLeakFile(t)
	// The clairvoyant arm is left to `searchbench leak`: opening its
	// process-wide gate here would break TestClairvoyantArmNeedsTheGate.
	cfg := LeakConfig{Sims: 48, Discount: 0.99, DiscountUnit: azmcts.DiscountPly}
	ctx := context.Background()
	var rows []LeakRow
	for i := range lf.Pairs {
		p := &lf.Pairs[i]
		for _, arm := range []SearchArm{ArmPIMC1, ArmISMCTS} {
			var got [2]LeakRow
			for k, w := range []string{"X", "Y"} {
				r, err := RunLeak(ctx, reg, p, w, arm, 0, cfg)
				if err != nil {
					t.Fatalf("%s/%s %s: %v", p.Name, w, arm, err)
				}
				got[k] = r
				rows = append(rows, r)
			}
			if !slices.Equal(got[0].Options, got[1].Options) || len(got[0].Options) < 2 {
				t.Fatalf("%s %s: options %q / %q", p.Name, arm, got[0].Options, got[1].Options)
			}
			same := got[0].Best == got[1].Best && slices.Equal(got[0].Root, got[1].Root)
			if !same {
				t.Errorf("%s %s: X and Y differ: %+v / %+v", p.Name, arm, got[0].Root, got[1].Root)
			}
			if slices.Contains(got[0].Hidden, LeakCanary) || slices.Contains(got[1].Hidden, LeakCanary) {
				t.Errorf("%s %s: a world held the canary", p.Name, arm)
			}
		}
	}
	// The planted leak: each member's real position as PIMC's world 0.
	var planted LeakPair
	for _, p := range lf.Pairs {
		if p.Name == "canary" {
			planted = p
		}
	}
	for _, m := range []*LeakMember{&planted.X, &planted.Y} {
		ws := slices.Clone(m.Worlds[0])
		ws[0] = m.Real
		m.Worlds = [][]json.RawMessage{ws}
	}
	var leaky []LeakRow
	for _, w := range []string{"X", "Y"} {
		r, err := RunLeak(ctx, reg, &planted, w, ArmPIMC1, 0, cfg)
		if err != nil {
			t.Fatalf("planted %s: %v", w, err)
		}
		leaky = append(leaky, r)
	}
	if !slices.Contains(leaky[0].Hidden, LeakCanary) {
		t.Errorf("planted leak: the canary detector missed X's real world")
	}
	if slices.Equal(leaky[0].Root, leaky[1].Root) {
		t.Errorf("planted leak: X and Y searched identically: %+v", leaky[0].Root)
	}
	maxAbs := func(v LeakVerdict) float64 {
		m := 0.0
		for _, o := range v.Options {
			m = math.Max(m, o.MaxAbs)
		}
		return m
	}
	// One seed has no CI, so no verdict passes; the identical-seed count
	// and the per-seed |ΔQ| are the checks.
	vs := AnalyzeLeak(leaky, nil, nil)
	if len(vs) != 1 || vs[0].Pass || vs[0].Identical != 0 || maxAbs(vs[0]) == 0 || *vs[0].CanaryX != 1 {
		t.Errorf("planted leak not caught: %+v", vs)
	}
	for _, v := range AnalyzeLeak(rows, nil, nil) {
		if v.Identical != 1 || maxAbs(v) != 0 || v.PermP != 1 {
			t.Errorf("%s %s: %+v", v.Pair, v.Arm, v)
		}
	}
}
