package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// The visit-distillation mode (M1b, -visits-corpus): train a student --
// policy head plus value head -- on an az visit corpus (cmd/botbench
// -az-corpus, policynet.VisitRecord). The policy target is the teacher
// search's visit distribution over its candidates (-visits-label teacher)
// or a one-hot on the default bot's answer over the SAME states and
// candidates (-visits-label bot, the behaviour-cloning control); the loss is
// the soft cross-entropy of the candidate softmax (policynet.lossVisits),
// whose logits are exactly the az search prior's. The value target is
// (1-b)·outcome + b·root value (-value-blend b, the TD blend of AZ spec §1).
// The split is always by game pair (HoldoutByGame over (deck, seed)), so no
// held-out board state is trained on. Train is reused as is.
//
// -visits-eval scores the trained model (or -init with -epochs 0) on other
// visit corpora, always against the TEACHER visits: policy CE, top-1
// agreement with the teacher's argmax and with the bot's answer, the
// override subset, and the value head's log loss and AUC -- overall, within
// each deck (the within-matchup AUC: every game is a mirror) and by turn
// bucket.

type visitsArgs struct {
	corpora, eval, init, out, report string
	label                            policynet.VisitLabel
	diag                             bool
	maxGames                         int
	temp                             float64
	// tdLambda and window are -visits-td-lambda and -visits-window
	// (visits_td.go); 0 is off.
	tdLambda float64
	window   int
	cfg      Config
}

// VisitEval is one model's readout on one eval corpus.
type VisitEval struct {
	Corpus string `json:"corpus"`
	N      int    `json:"n"`
	// Policy, against the teacher's visits.
	CE          float64 `json:"ce"`
	CEUniform   float64 `json:"ce_uniform"`
	Entropy     float64 `json:"teacher_entropy"`
	Top1Teacher float64 `json:"top1_teacher"`
	Top1Bot     float64 `json:"top1_bot"`
	PicksBot    float64 `json:"picks_bot"`
	TeacherBot  float64 `json:"teacher_keeps_bot"`
	OverrideN   int     `json:"override_n"`
	OverrideTop float64 `json:"override_top1"`
	// Value, against the recorded seat's outcome (draws and unknown left out).
	ValueN       int                `json:"value_n"`
	ValueLogLoss float64            `json:"value_log_loss"`
	BaseLogLoss  float64            `json:"base_log_loss"`
	AUC          float64            `json:"auc"`
	AUCWithin    float64            `json:"auc_within_deck"`
	AUCByDeck    map[string]float64 `json:"auc_by_deck"`
	AUCByTurn    []float64          `json:"auc_by_turn"` // turns 1-6, 7-12, 13+
	// RootAUC is the teacher's own root value's AUC (the ceiling reference).
	RootAUC       float64               `json:"root_auc"`
	RootAUCWithin float64               `json:"root_auc_within_deck"`
	ByKind        map[string][2]float64 `json:"top1_by_kind"` // [n, top1 teacher]
}

func loadVisitCorpora(paths string, o policynet.VisitLoadOptions, stdout io.Writer) ([]policynet.Example, []policynet.VisitRecord, policynet.FeatureSet, error) {
	var exs []policynet.Example
	var recs []policynet.VisitRecord
	var fs policynet.FeatureSet
	for i, path := range splitCorpora(paths) {
		o2 := o
		if o.MaxGames > 0 {
			// The cap is global across the list, in list order.
			o2.MaxGames = o.MaxGames - countGamesRecs(recs)
			if o2.MaxGames <= 0 {
				break
			}
		}
		e, r, st, err := policynet.LoadVisits(path, o2)
		if err != nil {
			return nil, nil, 0, err
		}
		if i > 0 && st.Features != fs {
			return nil, nil, 0, fmt.Errorf("visit corpus %s is feature set %s, earlier ones %s", path, st.Features, fs)
		}
		fs = st.Features
		fmt.Fprintf(stdout, "visit corpus %s: %d records, %d loaded from %d games (%d with outcome, %d teacher overrides %.1f%%), world %s sims %d, features %s\n",
			path, st.Records, st.Loaded, st.Games, st.WithOutcome, st.Overrides, 100*ratio(st.Overrides, st.Loaded), st.World, st.Sims, st.Features)
		exs = append(exs, e...)
		recs = append(recs, r...)
	}
	return exs, recs, fs, nil
}

func countGamesRecs(recs []policynet.VisitRecord) int {
	n := 0
	for i := range recs {
		if i == 0 || recs[i].GameID != recs[i-1].GameID || recs[i].Seed != recs[i-1].Seed {
			n++
		}
	}
	return n
}

func runVisits(a visitsArgs, stdout, stderr io.Writer) int {
	var model *policynet.Model
	if a.cfg.Epochs > 0 {
		if a.out == "" {
			fmt.Fprintln(stderr, "policytrain: visits mode needs -out")
			return 2
		}
		exs, fs, err := loadVisitTrain(a, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		}
		if len(exs) == 0 {
			fmt.Fprintln(stderr, "policytrain: no visit record loaded")
			return 1
		}
		if fs == policynet.FeaturesEntity {
			fmt.Fprintln(stderr, "policytrain: the visits mode trains the mz geometry; an entity corpus needs an entity model (not wired)")
			return 2
		}
		cfg := a.cfg
		cfg.HoldoutBy = HoldoutByGame
		cfg.Log = stdout
		if a.init != "" {
			m, err := policynet.LoadCheckpointFile(a.init)
			if err != nil {
				fmt.Fprintf(stderr, "policytrain: -init %s: %v\n", a.init, err)
				return 1
			}
			if m.Features != fs {
				fmt.Fprintf(stderr, "policytrain: -init %s is feature set %s, the corpus %s\n", a.init, m.Features, fs)
				return 1
			}
			cfg.Init = m
			fmt.Fprintf(stdout, "continuing from %s\n", a.init)
		}
		res, err := Train(exs, cfg)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		}
		model = res.Model
		model.Features = fs
		if v := res.Value; v != nil {
			fmt.Fprintf(stdout, "value head holdout: n %d log loss %.6f brier %.6f | base rate %.4f log loss %.6f\n",
				v.HoldoutN, v.LogLoss, v.Brier, v.BaseRate, v.BaseLogLoss)
		}
		fmt.Fprintf(stdout, "trained on %d examples (label %s), holdout %d\n", res.TrainN, a.label, res.HoldoutN)
		if fs.Diagnostic() {
			fmt.Fprintf(stdout, "feature set %s is a measurement only: no checkpoint written\n", fs)
		} else if err := model.SaveCheckpoint(a.out); err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		} else {
			fmt.Fprintf(stdout, "checkpoint %s: features %s value-hidden %d\n", a.out, fs, model.ValueHidden)
		}
	} else {
		if a.init == "" {
			fmt.Fprintln(stderr, "policytrain: -epochs 0 in visits mode evaluates -init; none given")
			return 2
		}
		m, err := policynet.LoadCheckpointFile(a.init)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: -init %s: %v\n", a.init, err)
			return 1
		}
		model = m
	}
	if a.eval == "" {
		return 0
	}
	var evals []VisitEval
	for _, path := range splitCorpora(a.eval) {
		exs, recs, fs, err := loadVisitCorpora(path, policynet.VisitLoadOptions{Label: policynet.VisitLabelTeacher, Diag: a.diag}, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: eval: %v\n", err)
			return 1
		}
		if fs != model.Features {
			fmt.Fprintf(stderr, "policytrain: eval corpus %s is %s, the model %s\n", path, fs, model.Features)
			return 1
		}
		ev := EvalVisits(model, exs, recs)
		ev.Corpus = path
		evals = append(evals, ev)
		printVisitEval(stdout, ev)
	}
	if a.report != "" {
		raw, err := json.MarshalIndent(evals, "", "  ")
		if err == nil {
			err = os.WriteFile(a.report, append(raw, '\n'), 0o644)
		}
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: -visits-report: %v\n", err)
			return 1
		}
	}
	return 0
}

// visitTurnBucket is the AZ spec's 1-6 / 7-12 / 13+ decision-point buckets.
func visitTurnBucket(t int32) int {
	switch {
	case t <= 6:
		return 0
	case t <= 12:
		return 1
	}
	return 2
}

// EvalVisits scores m on a teacher-labelled visit corpus (see the file
// comment). recs parallel exs.
func EvalVisits(m *policynet.Model, exs []policynet.Example, recs []policynet.VisitRecord) VisitEval {
	ev := VisitEval{N: len(exs), AUCByDeck: map[string]float64{}, ByKind: map[string][2]float64{}}
	type kacc struct{ n, agree int }
	kinds := map[decision.Kind]*kacc{}
	var t1t, t1b, pb, tb, ovr, ovrAgree int
	var vs, rs []float64
	var ws []bool
	var decks []string
	var turns []int32
	rate, rn := 0.0, 0
	for i := range exs {
		if exs[i].HasOutcome && (exs[i].Outcome == 0 || exs[i].Outcome == 1) {
			rate += exs[i].Outcome
			rn++
		}
	}
	if rn > 0 {
		rate /= float64(rn)
	}
	for i, ex := range exs {
		vt := ex.Visits
		q := m.CandidateProbs(ex)
		best := 0
		for c := range q {
			if q[c] > q[best] {
				best = c
			}
		}
		for c, p := range vt.Pi {
			if p > 0 {
				ev.CE -= p * math.Log(math.Max(q[c], 1e-12))
				ev.Entropy -= p * math.Log(p)
			}
		}
		ev.CEUniform += math.Log(float64(len(vt.Pi)))
		if best == vt.Teacher {
			t1t++
		}
		if best == 0 {
			pb++
		}
		if vt.Teacher == 0 {
			tb++
			if best == 0 {
				t1b++
			}
		} else {
			ovr++
			if best == vt.Teacher {
				ovrAgree++
			}
		}
		ka := kinds[ex.Kind]
		if ka == nil {
			ka = &kacc{}
			kinds[ex.Kind] = ka
		}
		ka.n++
		if best == vt.Teacher {
			ka.agree++
		}
		if m.HasValue() && ex.HasOutcome && (ex.Outcome == 0 || ex.Outcome == 1) {
			v := float64(m.Value(ex.State))
			ev.ValueN++
			ev.ValueLogLoss += bceProb(v, ex.Outcome)
			ev.BaseLogLoss += bceProb(rate, ex.Outcome)
			vs = append(vs, v)
			rs = append(rs, recs[i].RootValue)
			ws = append(ws, ex.Outcome == 1)
			decks = append(decks, recs[i].Deck)
			turns = append(turns, ex.Turn)
		}
	}
	n := float64(max(ev.N, 1))
	ev.CE /= n
	ev.Entropy /= n
	ev.CEUniform /= n
	ev.Top1Teacher = ratio(t1t, ev.N)
	ev.PicksBot = ratio(pb, ev.N)
	ev.TeacherBot = ratio(tb, ev.N)
	ev.Top1Bot = ratio(t1b, tb) // on the kept subset: model keeps the bot where the teacher did
	ev.OverrideN = ovr
	ev.OverrideTop = ratio(ovrAgree, ovr)
	if ev.ValueN > 0 {
		ev.ValueLogLoss /= float64(ev.ValueN)
		ev.BaseLogLoss /= float64(ev.ValueN)
		ev.AUC = auc(vs, ws)
		ev.RootAUC = auc(rs, ws)
		ev.AUCWithin, ev.AUCByDeck = aucWithin(vs, ws, decks)
		ev.RootAUCWithin, _ = aucWithin(rs, ws, decks)
		for b := 0; b < 3; b++ {
			var bs []float64
			var bw []bool
			for i := range vs {
				if visitTurnBucket(turns[i]) == b {
					bs, bw = append(bs, vs[i]), append(bw, ws[i])
				}
			}
			ev.AUCByTurn = append(ev.AUCByTurn, auc(bs, bw))
		}
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, string(k))
	}
	sort.Strings(names)
	for _, k := range names {
		ka := kinds[decision.Kind(k)]
		ev.ByKind[k] = [2]float64{float64(ka.n), ratio(ka.agree, ka.n)}
	}
	return ev
}

// aucWithin is the example-weighted mean of the per-group AUCs (groups with
// both a win and a loss), and the per-group values.
func aucWithin(s []float64, w []bool, group []string) (float64, map[string]float64) {
	idx := map[string][]int{}
	var order []string
	for i, g := range group {
		if _, ok := idx[g]; !ok {
			order = append(order, g)
		}
		idx[g] = append(idx[g], i)
	}
	sort.Strings(order)
	out := map[string]float64{}
	sum, wsum := 0.0, 0.0
	for _, g := range order {
		var gs []float64
		var gw []bool
		pos := 0
		for _, i := range idx[g] {
			gs, gw = append(gs, s[i]), append(gw, w[i])
			if w[i] {
				pos++
			}
		}
		if pos == 0 || pos == len(gs) {
			continue
		}
		a := auc(gs, gw)
		out[g] = a
		sum += a * float64(len(gs))
		wsum += float64(len(gs))
	}
	if wsum == 0 {
		return 0.5, out
	}
	return sum / wsum, out
}

func printVisitEval(w io.Writer, ev VisitEval) {
	fmt.Fprintf(w, "eval %s: n %d\n", ev.Corpus, ev.N)
	fmt.Fprintf(w, "  policy vs teacher visits: CE %.4f (uniform %.4f, teacher entropy %.4f); top1 teacher %.4f; picks bot %.4f (teacher keeps bot %.4f); keeps bot where teacher did %.4f; override top1 %.4f over %d\n",
		ev.CE, ev.CEUniform, ev.Entropy, ev.Top1Teacher, ev.PicksBot, ev.TeacherBot, ev.Top1Bot, ev.OverrideTop, ev.OverrideN)
	for _, k := range []string{"priority", "attackers", "blockers", "target"} {
		if v, ok := ev.ByKind[k]; ok {
			fmt.Fprintf(w, "    %-10s n %6.0f top1 teacher %.4f\n", k, v[0], v[1])
		}
	}
	if ev.ValueN > 0 {
		fmt.Fprintf(w, "  value: n %d log loss %.4f (base %.4f); AUC %.4f, within-deck %.4f, by turn 1-6/7-12/13+ %.4f/%.4f/%.4f; teacher root value AUC %.4f within-deck %.4f\n",
			ev.ValueN, ev.ValueLogLoss, ev.BaseLogLoss, ev.AUC, ev.AUCWithin, ev.AUCByTurn[0], ev.AUCByTurn[1], ev.AUCByTurn[2], ev.RootAUC, ev.RootAUCWithin)
	}
}
