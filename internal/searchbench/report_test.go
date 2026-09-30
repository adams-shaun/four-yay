package searchbench

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBindResultsRejectsBadRows(t *testing.T) {
	m := testManifest()
	good := []Result{
		{ManifestDigest: m.Digest, Arm: "pimc-1", ItemID: "dev-0001", AgentChoices: []int{0}},
		{ManifestDigest: m.Digest, Arm: "pimc-1", ItemID: "test-0001", AgentChoices: []int{3}, AgentAct: true},
	}
	run, err := BindResults(m, SplitTest, good)
	if err != nil || run.Arm != "pimc-1" || len(run.Items) != 1 || run.Items[0].ID != "test-0001" {
		t.Fatalf("BindResults = %+v, %v", run, err)
	}
	if _, err := BindResults(m, SplitTest, good[1:]); err != nil {
		t.Fatalf("a test-only file was refused: %v", err)
	}
	mut := func(f func(*Result)) []Result {
		rows := append([]Result(nil), good...)
		rows[1].AgentChoices = append([]int(nil), rows[1].AgentChoices...)
		f(&rows[1])
		return rows
	}
	for name, rows := range map[string][]Result{
		"missing":          good[:1],
		"duplicate":        {good[0], good[1], good[1]},
		"cross-manifest":   mut(func(r *Result) { r.ManifestDigest = strings.Repeat("c", 64) }),
		"unknown item":     mut(func(r *Result) { r.ItemID = "test-9999" }),
		"mixed arms":       mut(func(r *Result) { r.Arm = "pimc-4" }),
		"no arm":           mut(func(r *Result) { r.Arm = "" }),
		"no choice":        mut(func(r *Result) { r.AgentChoices = nil }),
		"two choices":      mut(func(r *Result) { r.AgentChoices = []int{1, 3} }),
		"choice too big":   mut(func(r *Result) { r.AgentChoices = []int{4} }),
		"act mismatch":     mut(func(r *Result) { r.AgentAct = false }),
		"completed > sims": mut(func(r *Result) { r.Sims, r.Completed = 1, 2 }),
		"root unsorted":    mut(func(r *Result) { r.Root = []RootOption{{Choice: 2}, {Choice: 1}} }),
		"root out of range": mut(func(r *Result) {
			r.Root = []RootOption{{Choice: 7, Visits: 1}}
		}),
		"nan Q":           mut(func(r *Result) { r.Root = []RootOption{{Choice: 1, Q: math.NaN()}} }),
		"negative timing": mut(func(r *Result) { r.CoreSeconds = -1 }),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BindResults(m, SplitTest, rows); err == nil {
				t.Fatal("BindResults accepted it")
			}
		})
	}
	if _, err := BindResults(m, "train", good); err == nil {
		t.Fatal("unknown split accepted")
	}
}

func TestReadResultsIsStrict(t *testing.T) {
	m := testManifest()
	dir := t.TempDir()
	good := filepath.Join(dir, "r.jsonl")
	rows := []Result{{ManifestDigest: m.Digest, Arm: "a", ItemID: "test-0001", AgentChoices: []int{1}, AgentAct: true, Root: []RootOption{{Choice: 1, Visits: 3, Q: .5}}}}
	if err := WriteResults(good, rows); err != nil {
		t.Fatal(err)
	}
	back, err := ReadResults(good)
	if err != nil || len(back) != 1 || back[0].Root[0].Visits != 3 {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	b, _ := os.ReadFile(good)
	line := strings.TrimSpace(string(b))
	for name, payload := range map[string]string{
		"unknown field": line[:len(line)-1] + `,"Extra":1}` + "\n",
		"trailing":      line + " {}\n",
		"blank line":    line + "\n\n" + line + "\n",
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(p, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadResults(p); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestQGapKnownAnswer(t *testing.T) {
	m := testManifest() // test-0001 spell: options Pass, A, B, C; label {Pass, A, C}
	rows := []Result{{ManifestDigest: m.Digest, Arm: "a", ItemID: "test-0001", AgentChoices: []int{2}, AgentAct: true, Sims: 10, Completed: 10,
		Root: []RootOption{{Choice: 0, Visits: 1, Q: .40}, {Choice: 1, Visits: 2, Q: .45}, {Choice: 2, Visits: 6, Q: .47}, {Choice: 3, Visits: 0, Q: .9}}}}
	g := qGap(bindT(t, m, rows), 2)
	// Choice B (Q .47) against the human's best visited option A (.45); C is
	// unvisited and has no Q. On upstream's scale: (0.47 - 0.45) * 2 = 0.04.
	if g.N != 1 || math.Abs(g.Median-.04) > 1e-12 || g.Below002 != 0 || g.Below005 != 1 || g.Above015 != 0 || math.Abs(g.HumanVisitShare-3.0/9) > 1e-12 {
		t.Fatalf("q gap %+v", g)
	}
	rows[0].AgentChoices, rows[0].AgentAct = []int{1}, true
	if g := qGap(bindT(t, m, rows), 2); g.Disagreements != 0 || g.N != 0 {
		t.Fatalf("agreement counted as a gap: %+v", g)
	}
}

func TestAnalyzeAndCompareReports(t *testing.T) {
	m := benchManifest(80, 17)
	arms, err := Baselines(m, 1)
	if err != nil {
		t.Fatal(err)
	}
	runs := []Run{bindT(t, m, benchArm(m, "pimc-1-b100", .5, 1)), bindT(t, m, benchArm(m, "is-mcts-b100", .6, 2)), bindT(t, m, arms[BaselinePassive])}
	rep, err := Analyze(m, runs, []string{"a", "b", "c"}, DefaultAnalyzeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if rep.References.Items != 160 || rep.References.Games != 80 || rep.Coverage == nil || rep.Coverage.Runs != 2 {
		t.Fatalf("references %+v coverage %+v", rep.References, rep.Coverage)
	}
	r0 := rep.Runs[0]
	if r0.Budget != 100 || r0.QGap == nil || *r0.MeanLeafPlies != 5 || *r0.EnvStepsPerSim != 1.2 || math.Abs(r0.CoreSecondsPerDecision-.01) > 1e-12 {
		t.Fatalf("run report %+v", r0)
	}
	if rep.Runs[2].QGap != nil || rep.Runs[2].MeanLeafPlies != nil {
		t.Fatal("a baseline has search diagnostics")
	}
	var md bytes.Buffer
	if err := rep.WriteMarkdown(&md); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| pimc-1-b100 | 160 |", "baseline-passive", "Q-gap", "Coverage over the 2 search runs"} {
		if !strings.Contains(md.String(), want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md.String())
		}
	}
	if _, err := json.Marshal(rep); err != nil {
		t.Fatal(err)
	}
	c, err := Compare(m, runs[0], runs[1], DefaultAnalyzeOptions())
	if err != nil {
		t.Fatal(err)
	}
	md.Reset()
	if err := c.WriteMarkdown(&md); err != nil || !strings.Contains(md.String(), "| same_choice |") {
		t.Fatalf("compare markdown %v:\n%s", err, md.String())
	}
}

// TestDumpUpstreamFixture writes a synthetic manifest and two arms for the
// upstream cross-check (scripts/searchbench/crosscheck_upstream.py) when
// SEARCHBENCH_FIXTURE_DIR is set.
func TestDumpUpstreamFixture(t *testing.T) {
	dir := os.Getenv("SEARCHBENCH_FIXTURE_DIR")
	if dir == "" {
		t.Skip("SEARCHBENCH_FIXTURE_DIR not set")
	}
	m := upstreamFixtureManifest()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, arm := range []struct {
		name  string
		agree float64
		seed  uint64
	}{{"a", .55, 21}, {"b", .35, 22}} {
		if err := WriteResults(filepath.Join(dir, arm.name+".jsonl"), benchArm(m, arm.name, arm.agree, arm.seed)); err != nil {
			t.Fatal(err)
		}
	}
}

func upstreamFixtureManifest() Manifest { return benchManifest(300, 23) }

// TestMatchesUpstreamAnalyzePy pins the numbers upstream's own analyze.py and
// diagnose.py (draft-zero b1e0ba68: bootstrap, macro, score_row, paired,
// balanced_score/act_split) compute on the synthetic fixture above, 1,000
// resamples with seed 0 in every call. The balanced point is compared to
// 1e-3 because act_split rounds each part to three decimals.
//
// The balanced CI is the one place we deliberately differ. balanced_score
// scores each resample through a dict ({i: R[i] for i in ids}), so a game
// drawn k times counts once: it is not a bootstrap, and its CI is about 0.77×
// as wide as it should be (upstream prints [0.776, 0.829] on this fixture).
// We keep the draw multiplicity, as bootstrap() does for A_set; the golden
// CI is act_split's arithmetic over the drawn multiset, computed in Python.
func TestMatchesUpstreamAnalyzePy(t *testing.T) {
	m := upstreamFixtureManifest()
	a, b := bindT(t, m, benchArm(m, "a", .55, 21)), bindT(t, m, benchArm(m, "b", .35, 22))
	ea := DefaultBootstrap.Estimates(a)
	d, err := DefaultBootstrap.Paired(a, b)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, e Estimate, want [3]float64, tol float64) {
		t.Helper()
		if e.Value == nil || e.CI == nil || math.Abs(*e.Value-want[0]) > tol || math.Abs(e.CI[0]-want[1]) > tol || math.Abs(e.CI[1]-want[2]) > tol {
			t.Errorf("%s = %v %v, upstream %v", name, e.Value, e.CI, want)
		}
	}
	check("a_set", ea["a_set"], upstreamASet, 1e-12)
	check("a_strict", ea["a_strict"], upstreamAStrict, 1e-12)
	check("a_soft", ea["a_soft"], upstreamASoft, 1e-12)
	check("balanced", ea["balanced"], [3]float64{upstreamBalanced[0], ea["balanced"].CI[0], ea["balanced"].CI[1]}, 1e-3)
	if ci := ea["balanced"].CI; math.Abs(ci[0]-upstreamBalancedCIMultiset[0]) > 1e-12 || math.Abs(ci[1]-upstreamBalancedCIMultiset[1]) > 1e-12 {
		t.Errorf("balanced CI %v, multiset bootstrap of upstream's arithmetic %v", ci, upstreamBalancedCIMultiset)
	}
	check("paired a_set", d["a_set"], upstreamPairedASet, 1e-12)
	c, _, _ := Chance(m.Items)
	if math.Abs(c-upstreamChance) > 1e-12 {
		t.Errorf("chance %v, upstream %v", c, upstreamChance)
	}
}

// Upstream's values on the fixture: value, CI low, CI high. Printed by
// scripts/searchbench/crosscheck_upstream.py against draft-zero b1e0ba68.
var (
	upstreamASet     = [3]float64{0.765, 0.7324508713385243, 0.7976226727123079}
	upstreamAStrict  = [3]float64{0.755, 0.7211221393286091, 0.7882946511938109}
	upstreamASoft    = [3]float64{0.6515696768380637, 0.6279705185274895, 0.674711306223616}
	upstreamBalanced = [3]float64{0.8027, 0.776, 0.8293} // upstream's own (deduplicated) CI, for the record
	// act_split's arithmetic over each resample's multiset of games.
	upstreamBalancedCIMultiset = [2]float64{0.7669645172829181, 0.835655167042976}
	upstreamPairedASet         = [3]float64{0.10833333333333334, 0.06302977454743858, 0.15667385127599287}
	upstreamChance             = 0.4607222222222222
)
