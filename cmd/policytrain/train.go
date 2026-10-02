package main

import (
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// The training loop for the per-option scorer (plan L9b). Deliberately
// SINGLE-THREADED and deterministic (R1 §3.3): a Hogwild-style sharded
// update is a data race under the Go memory model, and parallel float
// accumulation is order-dependent, so a multi-worker trainer would produce a
// checkpoint that cannot be rebuilt byte-for-byte from its seed and corpus —
// a foreign object in this repo. The -workers idea was considered and
// REJECTED: sharding only the forward pass would keep bit-identity only by
// re-serialising the reduction, and the single-threaded step measured at the
// production geometry (4,201 examples/s, BenchmarkLossGradStep) is not the
// bottleneck for a 14.6k-decision corpus (≈3.5 s/epoch).
//
// The wall clock is deliberately NOT read here (spec D16's clock discipline,
// the cmd/policytune precedent): wall/throughput figures for the report
// come from the test/benchmark harness, never from the trainer, so no run's
// timing can reach a checkpoint byte. Everything here is plain sequential
// float32 SGD over shuffled batches; the only randomness is the
// caller-seeded math/rand/v2 source, and the shuffle order is part of the
// seed's contract (same seed + same corpus ⇒ byte-identical checkpoint,
// pinned by TestTrainerDeterministic).

// Config is one training run's knobs — the cmd/policytrain flag surface.
type Config struct {
	Epochs  int
	Batch   int
	LR      float64
	Seed    int64
	Holdout float64 // fraction of examples held out of the update, [0, 1)
	// HoldoutBy is the split's unit. "" or HoldoutByExample (the default)
	// shuffles EXAMPLES and holds out the first ⌊Holdout·n⌋ -- the historical
	// split, same rng draws, bit for bit. HoldoutByGame groups the examples by
	// game (Pair, Seed, GameIndex), shuffles the GROUPS with the same rng and
	// holds out whole games until at least Holdout·n examples are held, so no
	// board state from a held-out game is ever trained on.
	HoldoutBy string
	Embed     int // H, the shared embedding width
	Hidden    int // hidden layer width
	Mode      policynet.LossMode
	// RankWeight is the ranking term's weight against the value term (a
	// TERM-MIX weight, see policynet.LossConfig). In pure CE mode the rank
	// term is the whole loss, so a non-zero, non-one RankWeight is a uniform
	// scale on a single gradient direction: the per-batch clip normalises
	// it away whenever the cap binds (batches under the cap see it through
	// as an lr rescale on that batch), and with the clip disabled it is
	// equivalent to rescaling lr. Either way it cannot tune anything, so
	// Train WARNS when it is set in CE mode; RankWeight 0 (the off switch)
	// and 1 (the default) are the only settings with a distinct meaning
	// there. In hybrid mode the weight is a real direction knob and is never
	// warned about. Pin: TestCERankWeightInertWhileTheClipBinds.
	RankWeight float64
	HuberDelta float64
	// ResidualInit is the fixed weight of the bot-prior residual head
	// (policynet.Model.ResidualW): an option the bot's own answer contains
	// scores ResidualInit higher for the whole run, so with a positive value
	// the model starts at the bot baseline and the learned head only supplies
	// the overrides. 0 disables the residual (pure CE / hybrid / value). It is
	// a PRIOR, never trained, so it cannot be decayed away by the loss.
	ResidualInit float64
	// Clip is the per-batch GLOBAL-L2 cap on the summed gradient, applied
	// before the lr/batch scale (Grads.Clip; <= 0 disables; the CLI default
	// is 1). Direction-preserving by design: when the cap binds, every
	// uniform loss scale is normalised away and the step's worst-case norm
	// is lr·cap/batchSize (batches already under the cap see a uniform
	// scale through, as an lr rescale on that batch). The cap is not
	// cosmetic: an uncapped CE run drives an
	// exponential embedding→hidden→embedding feedback loop (backprop
	// through the hidden layer multiplies the two blocks' magnitudes into
	// each other), the weights overflow float32 within the epoch budget,
	// and the diverged model's NaN scores tie-break to the first option on
	// every decision — exactly the first-option baseline. Train rejects that
	// divergence instead of checkpointing it. Pin: TestClipZeroRejectsNonFiniteModel.
	Clip float64
	// OverrideWeight multiplies every example whose label records a teacher
	// override of the bot (Example.TeacherChoice != Example.BotIndex): the
	// decisions that carry information. 1 means no reweighting, >1 up-weights
	// the overrides, and <= 0 is treated as 1.
	OverrideWeight float64
	// KindModes overrides Mode for specific decision kinds. The attackers kind
	// is trained with the binary logistic loss (LossBCE) so its score level is
	// calibrated at 0, which is what the seat's per-option admission rule
	// reads; priority keeps argmax CE. A nil map applies Mode to every kind.
	// cmd/policytrain's default is {"attackers": LossBCE} under the global `ce`.
	KindModes map[decision.Kind]policynet.LossMode

	// ExtraW is the experimental per-option augmentation width (Option.Extra):
	// 0 reproduces the shipped encoder geometry byte for byte; a feature-family
	// experiment sets it to the width of the augmentation it built. Such a
	// model cannot be checkpointed (the format has no ExtraW field), which is
	// deliberate -- it is a measurement vehicle, not a deployable scorer.
	ExtraW int

	// The value head (ticket pn08). ValueWeight 0 (the default) trains no
	// value head: the model is built without one (ValueHidden 0 in the
	// checkpoint) and the run is bit-identical to the pre-value-head trainer
	// — the value head's init draws come from a SEPARATE rng
	// (valueRNG), so even a value-on run moves no policy init draw and no
	// shuffle. ValueHidden is the head's width (used only when ValueWeight >
	// 0); ValueBlend b ∈ [0,1] mixes the target (1-b)·Outcome +
	// b·TeacherValue (policynet.LossConfig.ValueTarget).
	ValueHidden int
	ValueWeight float64
	ValueBlend  float64

	// Init, when set, is the model training continues from instead of a
	// fresh one (visits mode's -init); its architecture must be the run's
	// (checkInit). It is trained in place. ScalePolicy and PolicyWeight are
	// policynet.LossConfig's (visits mode's -visits-policy-weight).
	Init         *policynet.Model
	ScalePolicy  bool
	PolicyWeight float64

	// Log receives one line per epoch (nil discards).
	Log io.Writer
}

// EpochStat is one epoch's reported readout.
type EpochStat struct {
	Epoch       int
	TrainLoss   float64 // mean combined loss over the training examples
	TrainTop1   float64 // argmax-in-preferred agreement over eligible examples
	HoldoutLoss float64
	HoldoutTop1 float64
	// The value head's holdout readout (zero when the value head is off):
	// the mean BCE (log loss) and Brier score of V(s) over the holdout
	// examples that have a value target.
	HoldoutValueLogLoss float64
	HoldoutValueBrier   float64
}

// ValueStat is the value head's holdout readout beside the base-rate
// predictor: a constant prediction equal to the TRAIN split's mean target,
// scored over the same holdout examples. The value head earns its place only
// by beating it.
type ValueStat struct {
	TrainN      int     // train-split examples with a value target
	HoldoutN    int     // holdout examples with a value target
	BaseRate    float64 // mean train-split target
	LogLoss     float64 // model's holdout log loss
	Brier       float64 // model's holdout Brier score
	BaseLogLoss float64 // base-rate predictor's holdout log loss
	BaseBrier   float64 // base-rate predictor's holdout Brier score
}

// Result carries the trained model and the run's bookkeeping.
type Result struct {
	Model    *policynet.Model
	Epochs   []EpochStat
	TrainN   int
	HoldoutN int
	Skipped  int // examples with no labelled option (never trained on)
	// ByKind is the honest per-decision-kind readout on the HOLDOUT split:
	// the model's top-1 agreement beside the three baselines computed from
	// the same split. Sorted by kind name. The blended EpochStat.Top1 mixes
	// kinds whose difficulty differs wildly (an attackers decision where a
	// first-index pick already agrees most of the time, a cast decision where
	// it does not), so it must be labelled as blended wherever it is shown.
	ByKind []KindStat
	// Value is the final value-head holdout readout; nil when the value head
	// is off.
	Value *ValueStat
}

// KindStat is one decision kind's holdout agreement and the baselines it
// must beat. Eligible counts the examples with a teacher-preferred option
// (a decision with none can neither agree nor disagree).
type KindStat struct {
	Kind       decision.Kind
	Eligible   int
	ModelTop1  float64 // argmax over labelled options is teacher-preferred
	BotTop1    float64 // pick the bot's candidate: teacher kept the bot
	FirstTop1  float64 // pick the first labelled option
	RandomTop1 float64 // expected agreement of a uniform pick; #pref/#labelled averaged

	// The override subset. On an oracle corpus the teacher keeps the bot on
	// ~98% of decisions, so BotTop1 is ~0.98 and ModelTop1 alone cannot show
	// whether the model learned any override; these split it.
	OverrideN         int     // eligible examples where the teacher overrode the bot (Example.Override)
	ModelOverrideTop1 float64 // model's argmax is teacher-preferred, over the OverrideN override examples
	ModelKeepTop1     float64 // model's argmax is teacher-preferred, over the Eligible-OverrideN kept examples
	ModelPicksBot     float64 // fraction of eligible examples whose model argmax is a BotPick option
}

// Holdout split units (Config.HoldoutBy, the -holdout-by flag).
const (
	HoldoutByExample = "example"
	HoldoutByGame    = "game"
)

type split struct {
	train []int
	hold  []int
}

// Train fits a Model to examples and returns it with the per-epoch report.
// The corpus order is the caller's (the loader's record order); the split
// and the per-epoch batch order are derived from cfg.Seed alone.
func Train(examples []policynet.Example, cfg Config) (*Result, error) {
	switch {
	case len(examples) == 0:
		return nil, fmt.Errorf("policytrain: empty corpus")
	case cfg.Epochs < 1:
		return nil, fmt.Errorf("policytrain: epochs %d < 1", cfg.Epochs)
	case cfg.Batch < 1:
		return nil, fmt.Errorf("policytrain: batch %d < 1", cfg.Batch)
	case cfg.LR <= 0:
		return nil, fmt.Errorf("policytrain: learning rate %g <= 0", cfg.LR)
	case cfg.Embed < 1 || cfg.Hidden < 1:
		return nil, fmt.Errorf("policytrain: geometry embed %d hidden %d", cfg.Embed, cfg.Hidden)
	case cfg.Holdout < 0 || cfg.Holdout >= 1:
		return nil, fmt.Errorf("policytrain: holdout fraction %g outside [0,1)", cfg.Holdout)
	case cfg.HoldoutBy != "" && cfg.HoldoutBy != HoldoutByExample && cfg.HoldoutBy != HoldoutByGame:
		return nil, fmt.Errorf("policytrain: holdout-by %q (want %s or %s)", cfg.HoldoutBy, HoldoutByExample, HoldoutByGame)
	case cfg.ResidualInit < 0:
		return nil, fmt.Errorf("policytrain: residual init %g < 0 (a positive bot-prior weight is the residual; 0 disables)", cfg.ResidualInit)
	case cfg.ValueWeight < 0 || math.IsNaN(cfg.ValueWeight):
		return nil, fmt.Errorf("policytrain: value weight %g < 0 (0 disables the value head)", cfg.ValueWeight)
	case cfg.ValueBlend < 0 || cfg.ValueBlend > 1 || math.IsNaN(cfg.ValueBlend):
		return nil, fmt.Errorf("policytrain: value blend %g outside [0,1]", cfg.ValueBlend)
	case cfg.ValueWeight > 0 && cfg.ValueHidden < 1:
		return nil, fmt.Errorf("policytrain: value weight %g needs a value head (value hidden %d < 1)", cfg.ValueWeight, cfg.ValueHidden)
	}
	mode, err := policynet.ParseLossMode(string(cfg.Mode))
	if err != nil {
		return nil, fmt.Errorf("policytrain: %v", err)
	}
	cfg.Mode = mode

	// The rank-weight/CE contract (ticket policytrain-clip-rankweight-
	// interaction): in pure CE mode a non-trivial RankWeight is a uniform
	// scale on the single gradient direction. Under an active per-batch clip
	// it is normalised away; with the clip DISABLED (-clip 0) it merely
	// rescales the step like lr would, so it is NOT inert there and is not
	// warned about. 0 (the off switch) and 1 (the default) are distinct and
	// never warned about either.
	if cfg.Mode == policynet.LossCE && cfg.Clip > 0 && cfg.RankWeight != 0 && cfg.RankWeight != 1 && cfg.Log != nil {
		fmt.Fprintf(cfg.Log, "policytrain: note: -rank-weight %g is inert in pure CE mode while the per-batch gradient clip is active — the clip normalises any uniform loss scale away whenever the cap binds (batches under the cap see it through as an lr rescale). Tune -lr and -clip instead; RankWeight is a term-mix weight and CE has one term.\n", cfg.RankWeight)
	}

	// Examples with no labelled option cannot contribute to either loss
	// term; drop them up front and count them.
	usable := make([]policynet.Example, 0, len(examples))
	skipped := 0
	for i := range examples {
		labelled := false
		for j := range examples[i].Options {
			if examples[i].Options[j].Target.Labelled {
				labelled = true
				break
			}
		}
		if labelled {
			usable = append(usable, examples[i])
		} else {
			skipped++
		}
	}
	if len(usable) == 0 {
		return nil, fmt.Errorf("policytrain: corpus has no labelled example")
	}

	// math/rand/v2 with an explicit seeded source (the repo forbids v1; the
	// PCG seed pair is a fixed deterministic function of cfg.Seed).
	rng := rand.New(rand.NewPCG(uint64(cfg.Seed), 0x9E3779B97F4A7C15^uint64(cfg.Seed)))
	var sp split
	if cfg.HoldoutBy == HoldoutByGame {
		sp = splitCorpusByGame(usable, cfg.Holdout, rng)
		if len(sp.train) == 0 {
			return nil, fmt.Errorf("policytrain: holdout-by game: holding out %g of %d examples takes every game, leaving nothing to train on", cfg.Holdout, len(usable))
		}
	} else {
		sp = splitCorpus(usable, cfg.Holdout, rng)
	}

	var model *policynet.Model
	if cfg.Init != nil {
		if err := checkInit(cfg.Init, cfg); err != nil {
			return nil, err
		}
		model = cfg.Init
	} else {
		model = policynet.NewModelExtra(policynet.TableRows, cfg.Embed, cfg.Hidden, cfg.ExtraW, rng)
		model.ResidualW = float32(cfg.ResidualInit)
		if cfg.ValueWeight > 0 {
			// After the policy blocks, from its own rng: the main rng's draw
			// sequence (split, policy init, every epoch shuffle) is untouched.
			model.InitValue(cfg.ValueHidden, valueRNG(cfg.Seed))
		}
	}
	grads := model.NewGrads()
	lc := policynet.LossConfig{Mode: cfg.Mode, HuberDelta: cfg.HuberDelta, RankWeight: cfg.RankWeight, OverrideWeight: cfg.OverrideWeight, KindModes: cfg.KindModes,
		ValueWeight: cfg.ValueWeight, ValueBlend: cfg.ValueBlend, ScalePolicy: cfg.ScalePolicy, PolicyWeight: cfg.PolicyWeight}

	res := &Result{Model: model, TrainN: len(sp.train), HoldoutN: len(sp.hold), Skipped: skipped}
	var vbase valueBase
	if model.HasValue() {
		vbase = newValueBase(usable, sp, lc)
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "value head: hidden %d weight %g blend %g; targets on %d train / %d holdout examples; base rate (train mean target) %.4f\n",
				model.ValueHidden, cfg.ValueWeight, cfg.ValueBlend, vbase.trainN, len(vbase.hold), vbase.rate)
		}
	}
	order := make([]int, len(sp.train))
	for epoch := 1; epoch <= cfg.Epochs; epoch++ {
		copy(order, sp.train)
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

		sum, agree, eligible := 0.0, 0, 0
		for base := 0; base < len(order); base += cfg.Batch {
			end := base + cfg.Batch
			if end > len(order) {
				end = len(order)
			}
			grads.Reset()
			batchLoss := 0.0
			for _, ix := range order[base:end] {
				st := model.LossGrad(usable[ix], lc, grads)
				batchLoss += st.Loss
				if st.Eligible {
					eligible++
					if st.Agree {
						agree++
					}
				}
			}
			if cfg.Clip > 0 {
				grads.Clip(cfg.Clip)
			}
			model.ApplyGrads(grads, float32(cfg.LR/float64(end-base)))
			if block, index, value, ok := firstNonFiniteParameter(model); ok {
				return nil, fmt.Errorf("policytrain: non-finite parameter after update at epoch %d batch %d: %s[%d]=%g", epoch, base/cfg.Batch+1, block, index, value)
			}
			sum += batchLoss / float64(end-base)
		}
		batches := (len(order) + cfg.Batch - 1) / cfg.Batch
		trainLoss := sum / float64(batches)
		trainTop1 := ratio(agree, eligible)

		holdLoss, holdTop1 := evaluate(model, usable, sp.hold, lc)
		es := EpochStat{
			Epoch: epoch, TrainLoss: trainLoss, TrainTop1: trainTop1,
			HoldoutLoss: holdLoss, HoldoutTop1: holdTop1,
		}
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "epoch %d/%d train loss %.6f top1 %.3f | holdout loss %.6f top1 %.3f\n",
				epoch, cfg.Epochs, trainLoss, trainTop1, holdLoss, holdTop1)
		}
		if model.HasValue() {
			vs := vbase.evaluate(model, usable)
			es.HoldoutValueLogLoss, es.HoldoutValueBrier = vs.LogLoss, vs.Brier
			res.Value = &vs
			if cfg.Log != nil {
				fmt.Fprintf(cfg.Log, "epoch %d/%d value holdout n %d log loss %.6f brier %.6f | base rate %.4f log loss %.6f brier %.6f\n",
					epoch, cfg.Epochs, vs.HoldoutN, vs.LogLoss, vs.Brier, vs.BaseRate, vs.BaseLogLoss, vs.BaseBrier)
			}
		}
		res.Epochs = append(res.Epochs, es)
	}
	res.ByKind = evaluateByKind(model, usable, sp.hold)
	return res, nil
}

// valueRNG is the value head's init source: seeded from the run seed but a
// different PCG stream from the trainer's main rng, so drawing the value
// blocks moves nothing the main rng draws.
func valueRNG(seed int64) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(seed), 0x76616c7565)) // "value"
}

// valueBase is the value readout's fixed half: the holdout examples that
// have a target (with their targets) and the base-rate predictor, the train
// split's mean target.
type valueBase struct {
	trainN int
	rate   float64
	hold   []int
	target []float64
}

func newValueBase(examples []policynet.Example, sp split, lc policynet.LossConfig) valueBase {
	var vb valueBase
	sum := 0.0
	for _, ix := range sp.train {
		if t, ok := lc.ValueTarget(examples[ix]); ok {
			sum += t
			vb.trainN++
		}
	}
	vb.rate = 0.5
	if vb.trainN > 0 {
		vb.rate = sum / float64(vb.trainN)
	}
	for _, ix := range sp.hold {
		if t, ok := lc.ValueTarget(examples[ix]); ok {
			vb.hold = append(vb.hold, ix)
			vb.target = append(vb.target, t)
		}
	}
	return vb
}

// evaluate scores the model's value head and the base-rate predictor over
// the holdout examples with a target: mean log loss (BCE) and mean Brier.
func (vb valueBase) evaluate(m *policynet.Model, examples []policynet.Example) ValueStat {
	vs := ValueStat{TrainN: vb.trainN, HoldoutN: len(vb.hold), BaseRate: vb.rate}
	if len(vb.hold) == 0 {
		return vs
	}
	for k, ix := range vb.hold {
		t := vb.target[k]
		v := float64(m.Value(examples[ix].State))
		vs.LogLoss += m.ValueLogLoss(examples[ix].State, t)
		vs.Brier += (v - t) * (v - t)
		vs.BaseLogLoss += bceProb(vb.rate, t)
		vs.BaseBrier += (vb.rate - t) * (vb.rate - t)
	}
	n := float64(len(vb.hold))
	vs.LogLoss /= n
	vs.Brier /= n
	vs.BaseLogLoss /= n
	vs.BaseBrier /= n
	return vs
}

// bceProb is the binary cross-entropy of a probability p against a soft
// target t, with p clamped away from 0 and 1 so a degenerate base rate (an
// all-win train split) stays finite.
func bceProb(p, t float64) float64 {
	const eps = 1e-12
	p = math.Min(math.Max(p, eps), 1-eps)
	return -(t*math.Log(p) + (1-t)*math.Log(1-p))
}

// firstNonFiniteParameter scans learned blocks in ApplyGrads order so the
// reported location is deterministic. ResidualW is a fixed prior and is not
// part of the learned model.
func firstNonFiniteParameter(m *policynet.Model) (block string, index int, value float32, ok bool) {
	blocks := []struct {
		name string
		data []float32
	}{
		{"Table", m.Table},
		{"StateW", m.StateW},
		{"StateB", m.StateB},
		{"HidW", m.HidW},
		{"HidB", m.HidB},
		{"OutW", m.OutW},
		{"VHidW", m.VHidW},
		{"VHidB", m.VHidB},
		{"VOutW", m.VOutW},
		{"EntW", m.EntW},
		{"EntB", m.EntB},
		{"EntP", m.EntP},
	}
	for _, b := range blocks {
		for i, v := range b.data {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return b.name, i, v, true
			}
		}
	}
	if math.IsNaN(float64(m.OutB)) || math.IsInf(float64(m.OutB), 0) {
		return "OutB", 0, m.OutB, true
	}
	if math.IsNaN(float64(m.VOutB)) || math.IsInf(float64(m.VOutB), 0) {
		return "VOutB", 0, m.VOutB, true
	}
	return "", 0, 0, false
}

// splitCorpus shuffles the index space with the run's rng and takes the
// first ⌈frac·n⌉ as holdout. Called in the fixed rng sequence (before the
// model's init draws), it is deterministic given the seed.
func splitCorpus(examples []policynet.Example, frac float64, rng *rand.Rand) split {
	n := len(examples)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	rng.Shuffle(n, func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	hn := int(frac * float64(n))
	return split{train: idx[hn:], hold: idx[:hn]}
}

// gameKey identifies one game in a label corpus.
type gameKey struct {
	pair  string
	seed  uint64
	index int
}

// splitCorpusByGame is the by-game holdout: examples are grouped by game
// (Pair, Seed, GameIndex) with the groups numbered in first-seen corpus
// order (the map is only a lookup, never ranged), the GROUP order is
// shuffled with the run's rng, and whole groups are held out until at least
// frac·n examples are held. Every example of one game lands on one side.
// Called at the same point in the rng sequence as splitCorpus, it is
// deterministic given the seed.
func splitCorpusByGame(examples []policynet.Example, frac float64, rng *rand.Rand) split {
	lookup := map[gameKey]int{}
	var groups [][]int
	for i := range examples {
		k := gameKey{examples[i].Pair, examples[i].Seed, examples[i].GameIndex}
		g, ok := lookup[k]
		if !ok {
			g = len(groups)
			lookup[k] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], i)
	}
	rng.Shuffle(len(groups), func(i, j int) { groups[i], groups[j] = groups[j], groups[i] })
	target := frac * float64(len(examples))
	var sp split
	for _, g := range groups {
		if float64(len(sp.hold)) < target {
			sp.hold = append(sp.hold, g...)
		} else {
			sp.train = append(sp.train, g...)
		}
	}
	return sp
}

// evaluate runs the forward-only loss over a set of examples.
func evaluate(m *policynet.Model, examples []policynet.Example, idx []int, lc policynet.LossConfig) (loss float64, top1 float64) {
	if len(idx) == 0 {
		return 0, 0
	}
	sum, agree, eligible := 0.0, 0, 0
	for _, ix := range idx {
		st := m.Loss(examples[ix], lc)
		sum += st.Loss
		if st.Eligible {
			eligible++
			if st.Agree {
				agree++
			}
		}
	}
	return sum / float64(len(idx)), ratio(agree, eligible)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// evaluateByKind computes the honest per-kind holdout readout: the model's
// top-1 agreement and the bot-copy, first-labelled and random-pick baselines,
// all over the same eligible examples. An example is eligible when it has a
// teacher-preferred option (nothing to agree or disagree with otherwise).
// It also splits the model's agreement over the override subset (the
// teacher overrode the bot) and the kept subset, and counts how often the
// model's own pick is a bot-pick option. Kinds are visited in sorted order
// so the output never depends on map iteration order.
func evaluateByKind(m *policynet.Model, examples []policynet.Example, idx []int) []KindStat {
	type acc struct {
		eligible  int
		model     int
		bot       int
		first     int
		random    float64
		overrides int
		modelOvr  int
		modelKeep int
		picksBot  int
	}
	byKind := map[decision.Kind]*acc{}
	for _, ix := range idx {
		ex := examples[ix]
		labelled := make([]int, 0, len(ex.Options))
		for i := range ex.Options {
			if ex.Options[i].Target.Labelled {
				labelled = append(labelled, i)
			}
		}
		prefLabelled, prefTotal := 0, 0
		hasPref := false
		for _, i := range labelled {
			prefTotal++
			if ex.Options[i].Target.Preferred {
				prefLabelled++
				hasPref = true
			}
		}
		if len(labelled) == 0 || !hasPref {
			continue
		}
		a := byKind[ex.Kind]
		if a == nil {
			a = &acc{}
			byKind[ex.Kind] = a
		}
		a.eligible++
		best, _ := m.Argmax(ex) // labelled is non-empty here
		agree := ex.Options[best].Target.Preferred
		if agree {
			a.model++
		}
		if ex.Options[best].BotPick {
			a.picksBot++
		}
		if ex.Override() {
			a.overrides++
			if agree {
				a.modelOvr++
			}
		} else if agree {
			a.modelKeep++
		}
		if ex.TeacherChoice == ex.BotIndex {
			a.bot++
		}
		if ex.Options[labelled[0]].Target.Preferred {
			a.first++
		}
		a.random += ratio(prefLabelled, prefTotal)
	}
	kinds := make([]decision.Kind, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	out := make([]KindStat, 0, len(kinds))
	for _, k := range kinds {
		a := byKind[k]
		out = append(out, KindStat{
			Kind:       k,
			Eligible:   a.eligible,
			ModelTop1:  ratio(a.model, a.eligible),
			BotTop1:    ratio(a.bot, a.eligible),
			FirstTop1:  ratio(a.first, a.eligible),
			RandomTop1: a.random / float64(a.eligible),

			OverrideN:         a.overrides,
			ModelOverrideTop1: ratio(a.modelOvr, a.overrides),
			ModelKeepTop1:     ratio(a.modelKeep, a.eligible-a.overrides),
			ModelPicksBot:     ratio(a.picksBot, a.eligible),
		})
	}
	return out
}
