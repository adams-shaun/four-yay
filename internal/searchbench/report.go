package searchbench

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// DefaultQScale converts a gorge Q difference to upstream's value scale.
// gorge's leaf values lie in [0,1]; MageZero's BenchSearch backs up values in
// [-1,1], and docs/016 §8.4's thresholds (0.02, 0.05, 0.15) are on that
// scale, so gorge's gaps are doubled before they are compared.
const DefaultQScale = 2.0

// AnalyzeOptions controls a report.
type AnalyzeOptions struct {
	Split     Split
	Bootstrap Bootstrap
	QScale    float64
}

func DefaultAnalyzeOptions() AnalyzeOptions {
	return AnalyzeOptions{Split: SplitTest, Bootstrap: DefaultBootstrap, QScale: DefaultQScale}
}

// References are the item-set properties that need no result (analyze.py
// references()).
type References struct {
	Items         int                `json:"items"`
	PerType       map[string]int     `json:"per_type"`
	Games         int                `json:"games"`
	Chance        float64            `json:"chance"`
	ChancePerType map[string]float64 `json:"chance_per_type"`
	MeanOptions   map[string]float64 `json:"mean_options"`
}

// QGap is docs/016 §8.4 (diagnose.py §3): over the items where the choice is
// not in the human's label and the root has a Q for the choice and for at
// least one of the human's options, the Q of the choice minus the best Q
// among the human's options, on upstream's value scale.
type QGap struct {
	N                int     `json:"n"`
	Median           float64 `json:"median"`
	Below002         float64 `json:"lt_0.02"`
	Below005         float64 `json:"lt_0.05"`
	Above015         float64 `json:"gt_0.15"`
	HumanVisitShare  float64 `json:"human_visit_share"`
	Disagreements    int     `json:"disagreements"`
	WithoutRootTable int     `json:"without_root_q"`
}

// RunReport is one result file's row.
type RunReport struct {
	Arm                    string              `json:"arm"`
	File                   string              `json:"file,omitempty"`
	N                      int                 `json:"n"`
	Budget                 int                 `json:"budget"`
	SimsMean               float64             `json:"sims_mean"`
	CompletedMean          float64             `json:"completed_mean"`
	CoreSecondsPerDecision float64             `json:"core_seconds_per_decision"`
	EnvStepsPerSim         *float64            `json:"env_steps_per_sim"`
	MeanLeafPlies          *float64            `json:"mean_leaf_plies"`
	MeanLeafEdges          *float64            `json:"mean_leaf_edges"`
	MeanTurnsCrossed       *float64            `json:"mean_turns_crossed"`
	Fallbacks              map[string]int      `json:"fallbacks,omitempty"`
	Metrics                map[string]Estimate `json:"metrics"`
	QGap                   *QGap               `json:"q_gap,omitempty"`
}

// Coverage is diagnose.py §6: per type, the share of items that no run, some
// runs, or every run answers within the label.
type Coverage struct {
	Runs   int                           `json:"runs"`
	ByType map[string]map[string]float64 `json:"by_type"`
}

// Report is `searchbench analyze`'s JSON.
type Report struct {
	ManifestDigest string      `json:"manifest_digest"`
	Split          Split       `json:"split"`
	Bootstrap      string      `json:"bootstrap"`
	Resamples      int         `json:"resamples"`
	Seed           uint64      `json:"seed"`
	QScale         float64     `json:"q_scale"`
	References     References  `json:"references"`
	Runs           []RunReport `json:"runs"`
	Coverage       *Coverage   `json:"coverage,omitempty"`
}

const bootstrapMethod = "game-cluster percentile bootstrap (analyze.py bootstrap): resample Item.Row with replacement, CPython random.Random(seed).choice"

func references(items []Item) References {
	ref := References{Items: len(items), PerType: map[string]int{}, ChancePerType: map[string]float64{}, MeanOptions: map[string]float64{}}
	_, ref.Games = clusters(items)
	c, per, _ := Chance(items)
	ref.Chance = c
	var opts [4]int
	var n [4]int
	for _, it := range items {
		i := typeIndex(it.Type)
		n[i]++
		opts[i] += len(it.Options)
	}
	for i, t := range DecisionTypes {
		if n[i] == 0 {
			continue
		}
		ref.PerType[string(t)] = n[i]
		ref.ChancePerType[string(t)] = per[i]
		ref.MeanOptions[string(t)] = float64(opts[i]) / float64(n[i])
	}
	return ref
}

func qGap(run Run, scale float64) *QGap {
	var gaps, shares []float64
	g := &QGap{}
	for i, it := range run.Items {
		r := run.Results[i]
		c := r.AgentChoices[0]
		if containsChoice(it.Label.Alternatives, c) {
			continue
		}
		g.Disagreements++
		q := map[int]float64{}
		total, human := 0, 0
		for _, o := range r.Root {
			total += o.Visits
			if o.Visits > 0 {
				q[o.Choice] = o.Q
			}
			if containsChoice(it.Label.Alternatives, o.Choice) {
				human += o.Visits
			}
		}
		qc, ok := q[c]
		best, have := math.Inf(-1), false
		for _, a := range it.Label.Alternatives {
			if v, ok := q[a[0]]; ok {
				best, have = math.Max(best, v), true
			}
		}
		if !ok || !have {
			g.WithoutRootTable++
			continue
		}
		gaps = append(gaps, (qc-best)*scale)
		if total == 0 {
			total = 1
		}
		shares = append(shares, float64(human)/float64(total))
	}
	g.N = len(gaps)
	if g.N == 0 {
		return g
	}
	sort.Float64s(gaps)
	g.Median = median(gaps)
	sum := 0.0
	for i, v := range gaps {
		if v < 0.02 {
			g.Below002++
		}
		if v < 0.05 {
			g.Below005++
		}
		if v > 0.15 {
			g.Above015++
		}
		sum += shares[i]
	}
	k := float64(g.N)
	g.Below002 /= k
	g.Below005 /= k
	g.Above015 /= k
	g.HumanVisitShare = sum / k
	return g
}

func runReport(run Run, opt AnalyzeOptions) RunReport {
	rep := RunReport{Arm: run.Arm, N: len(run.Items), Metrics: opt.Bootstrap.Estimates(run)}
	var sims, completed, steps int
	var core, plies, edges, turns float64
	for _, r := range run.Results {
		if r.Sims > rep.Budget {
			rep.Budget = r.Sims
		}
		sims += r.Sims
		completed += r.Completed
		steps += r.EnvSteps
		core += r.CoreSeconds
		plies += r.MeanLeafPlies * float64(r.Completed)
		edges += r.MeanLeafEdges * float64(r.Completed)
		turns += r.MeanTurnsCrossed * float64(r.Completed)
		if r.Fallback != "" {
			if rep.Fallbacks == nil {
				rep.Fallbacks = map[string]int{}
			}
			rep.Fallbacks[r.Fallback]++
		}
	}
	n := float64(len(run.Results))
	rep.SimsMean, rep.CompletedMean, rep.CoreSecondsPerDecision = float64(sims)/n, float64(completed)/n, core/n
	// Upstream's horizon figures are totals over totals (edgeVisits / sims,
	// engineSteps / sims), so per-item means are weighted by completed
	// simulations here.
	if sims > 0 {
		rep.EnvStepsPerSim = ptr(float64(steps) / float64(sims))
	}
	if completed > 0 {
		c := float64(completed)
		rep.MeanLeafPlies, rep.MeanLeafEdges, rep.MeanTurnsCrossed = ptr(plies/c), ptr(edges/c), ptr(turns/c)
	}
	hasRoot := false
	for _, r := range run.Results {
		if len(r.Root) > 0 {
			hasRoot = true
			break
		}
	}
	if hasRoot {
		rep.QGap = qGap(run, opt.QScale)
	}
	return rep
}

// Analyze reports every run over the same manifest split.
func Analyze(m Manifest, runs []Run, files []string, opt AnalyzeOptions) (Report, error) {
	if len(runs) == 0 {
		return Report{}, fmt.Errorf("searchbench: no runs")
	}
	rep := Report{ManifestDigest: m.Digest, Split: opt.Split, Bootstrap: bootstrapMethod, Resamples: opt.Bootstrap.Resamples, Seed: opt.Bootstrap.Seed, QScale: opt.QScale}
	rep.References = references(runs[0].Items)
	for i, run := range runs {
		if err := sameItems(runs[0], run); err != nil {
			return Report{}, err
		}
		rr := runReport(run, opt)
		if i < len(files) {
			rr.File = files[i]
		}
		rep.Runs = append(rep.Runs, rr)
	}
	rep.Coverage = coverage(runs)
	return rep, nil
}

func isBaseline(arm string) bool { return strings.HasPrefix(arm, "baseline-") }

func coverage(runs []Run) *Coverage {
	var search []Run
	for _, r := range runs {
		if !isBaseline(r.Arm) {
			search = append(search, r)
		}
	}
	if len(search) < 2 {
		return nil
	}
	cov := &Coverage{Runs: len(search), ByType: map[string]map[string]float64{}}
	var n [4]int
	var never, always [4]int
	for i, it := range search[0].Items {
		k := 0
		for _, r := range search {
			if containsChoice(it.Label.Alternatives, r.Results[i].AgentChoices[0]) {
				k++
			}
		}
		t := typeIndex(it.Type)
		n[t]++
		if k == 0 {
			never[t]++
		} else if k == len(search) {
			always[t]++
		}
	}
	for i, t := range DecisionTypes {
		if n[i] == 0 {
			continue
		}
		d := float64(n[i])
		cov.ByType[string(t)] = map[string]float64{"never": float64(never[i]) / d, "some": float64(n[i]-never[i]-always[i]) / d, "always": float64(always[i]) / d}
	}
	return cov
}

// CompareReport is `searchbench compare`'s JSON: a − b.
type CompareReport struct {
	ManifestDigest string              `json:"manifest_digest"`
	Split          Split               `json:"split"`
	Bootstrap      string              `json:"bootstrap"`
	Resamples      int                 `json:"resamples"`
	Seed           uint64              `json:"seed"`
	ArmA           string              `json:"a"`
	ArmB           string              `json:"b"`
	N              int                 `json:"n"`
	Diff           map[string]Estimate `json:"diff"`
}

func Compare(m Manifest, a, b Run, opt AnalyzeOptions) (CompareReport, error) {
	d, err := opt.Bootstrap.Paired(a, b)
	if err != nil {
		return CompareReport{}, err
	}
	return CompareReport{ManifestDigest: m.Digest, Split: opt.Split, Bootstrap: bootstrapMethod, Resamples: opt.Bootstrap.Resamples, Seed: opt.Bootstrap.Seed, ArmA: a.Arm, ArmB: b.Arm, N: len(a.Items), Diff: d}, nil
}

// ---------------------------------------------------------------- markdown

func pct(e Estimate) string {
	if e.Value == nil {
		return "—"
	}
	s := fmt.Sprintf("%.1f%%", *e.Value*100)
	if e.CI != nil {
		s += fmt.Sprintf(" [%.1f, %.1f]", e.CI[0]*100, e.CI[1]*100)
	}
	return s
}

func pctBare(e Estimate) string {
	if e.Value == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *e.Value*100)
}

func dec3(e Estimate, ci bool) string {
	if e.Value == nil {
		return "—"
	}
	s := fmt.Sprintf("%.3f", *e.Value)
	if ci && e.CI != nil {
		s += fmt.Sprintf(" [%.3f, %.3f]", e.CI[0], e.CI[1])
	}
	return s
}

func optF(v *float64, format string) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf(format, *v)
}

// WriteMarkdown renders the report as docs/016-style tables.
func (r Report) WriteMarkdown(w io.Writer) error {
	var b strings.Builder
	ref := r.References
	fmt.Fprintf(&b, "Manifest `%s`, split %s: %d items from %d games (", r.ManifestDigest, r.Split, ref.Items, ref.Games)
	var parts []string
	for _, t := range DecisionTypes {
		if n, ok := ref.PerType[string(t)]; ok {
			parts = append(parts, fmt.Sprintf("%s %d, %.2f options, chance %.1f%%", t, n, ref.MeanOptions[string(t)], ref.ChancePerType[string(t)]*100))
		}
	}
	fmt.Fprintf(&b, "%s). Chance (uniform over the distinct options, macro): %.1f%%.\n", strings.Join(parts, "; "), ref.Chance*100)
	fmt.Fprintf(&b, "95%% CIs: %s, %d resamples, seed %d.\n\n", r.Bootstrap, r.Resamples, r.Seed)

	b.WriteString("| arm | n | A_set | strict | balanced | cast or hold | attack or not | block or not | which spell | which blocked | core-s/decision |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, x := range r.Runs {
		m := x.Metrics
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %.4f |\n", x.Arm, x.N, pct(m["a_set"]), pct(m["a_strict"]), dec3(m["balanced"], true),
			dec3(m["bal_cast"], true), dec3(m["bal_attack"], true), dec3(m["bal_block"], true), pct(m["which_spell"]), pct(m["which_block"]), x.CoreSecondsPerDecision)
	}
	b.WriteString("\nA_set by decision type:\n\n| arm | spell | spell, strict | hold | attack | block | A_soft |\n|---|---|---|---|---|---|---|\n")
	for _, x := range r.Runs {
		m := x.Metrics
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", x.Arm, pct(m["a_set_spell"]), pct(m["a_strict_spell"]), pct(m["a_set_hold"]), pct(m["a_set_attack"]), pct(m["a_set_block"]), pct(m["a_soft"]))
	}
	if len(r.Runs) > 0 {
		m := r.Runs[0].Metrics
		fmt.Fprintf(&b, "\nActivity (docs/016 §8.2). Top players: attack %s, block %s, cast on holds %s, do nothing on %s of decisions.\n\n",
			pctBare(m["human_act_attack"]), pctBare(m["human_act_block"]), pctBare(m["human_act_hold"]), pctBare(m["human_passive_share"]))
	}
	b.WriteString("| arm | attacks | blocks | casts on holds | spell: a human's cast / other / pass | attacks when the human didn't | blocks when the human didn't | does nothing |\n|---|---|---|---|---|---|---|---|\n")
	for _, x := range r.Runs {
		m := x.Metrics
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s / %s / %s | %s | %s | %s |\n", x.Arm, pctBare(m["act_attack"]), pctBare(m["act_block"]), pctBare(m["act_hold"]),
			pctBare(m["spell_cast_humans"]), pctBare(m["spell_cast_other"]), pctBare(m["spell_pass"]), pctBare(m["attack_when_human_did_not"]), pctBare(m["block_when_human_did_not"]), pctBare(m["passive_share"]))
	}
	b.WriteString("\nDiagnostics (docs/016 §8.4, §8.5). Q gaps on upstream's [-1,1] scale (gorge Q × q_scale).\n\n| arm | budget | sims | Q-gap n | median gap | <0.02 | <0.05 | >0.15 | human options' visit share | leaf plies | leaf edges | turns crossed | env steps/sim | fallbacks |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, x := range r.Runs {
		qg := "— | — | — | — | — | —"
		if x.QGap != nil && x.QGap.N > 0 {
			g := x.QGap
			qg = fmt.Sprintf("%d | %.3f | %.0f%% | %.0f%% | %.0f%% | %.0f%%", g.N, g.Median, g.Below002*100, g.Below005*100, g.Above015*100, g.HumanVisitShare*100)
		}
		fb := 0
		for _, v := range x.Fallbacks {
			fb += v
		}
		fmt.Fprintf(&b, "| %s | %d | %.1f | %s | %s | %s | %s | %s | %d |\n", x.Arm, x.Budget, x.SimsMean, qg, optF(x.MeanLeafPlies, "%.2f"), optF(x.MeanLeafEdges, "%.2f"), optF(x.MeanTurnsCrossed, "%.2f"), optF(x.EnvStepsPerSim, "%.2f"), fb)
	}
	if r.Coverage != nil {
		fmt.Fprintf(&b, "\nCoverage over the %d search runs (docs/016 §8 item 6):\n\n| type | never | some | always |\n|---|---|---|---|\n", r.Coverage.Runs)
		for _, t := range DecisionTypes {
			if c, ok := r.Coverage.ByType[string(t)]; ok {
				fmt.Fprintf(&b, "| %s | %.0f%% | %.0f%% | %.0f%% |\n", t, c["never"]*100, c["some"]*100, c["always"]*100)
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func signedPct(e Estimate) string {
	if e.Value == nil {
		return "—"
	}
	s := fmt.Sprintf("%+.1f", *e.Value*100)
	if e.CI != nil {
		s += fmt.Sprintf(" (%+.1f, %+.1f)", e.CI[0]*100, e.CI[1]*100)
		if e.CI[0] > 0 || e.CI[1] < 0 {
			s = "**" + s + "**"
		}
	}
	return s
}

// WriteMarkdown renders every paired difference; bold marks a CI that
// excludes zero. Rates are in points, balanced scores ×100 too.
func (c CompareReport) WriteMarkdown(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s − %s, %d %s items, paired over the same resampled games (%d resamples, seed %d). Points (×100), 95%% CI.\n\n| metric | a − b |\n|---|---|\n", c.ArmA, c.ArmB, c.N, c.Split, c.Resamples, c.Seed)
	for _, name := range MetricNames {
		fmt.Fprintf(&b, "| %s | %s |\n", name, signedPct(c.Diff[name]))
	}
	b.WriteString("\n| same choice | share |\n|---|---|\n")
	for _, name := range []string{"same_choice", "same_choice_spell", "same_choice_hold", "same_choice_attack", "same_choice_block"} {
		fmt.Fprintf(&b, "| %s | %s |\n", name, pct(c.Diff[name]))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
