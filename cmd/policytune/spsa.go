// SPSA core for cmd/policytune: simultaneous-perturbation stochastic
// approximation over a cast-weight profile, with common random numbers.
//
// The fitter is deliberately small and injection-friendly: the objective
// arrives as an Evaluator (the real one plays engine games on the dev suite;
// a test injects a synthetic concave function), so the optimization loop can
// be proven to converge without paying for a single game. Nothing here reads
// the wall clock, ranges over a map whose order can reach the output, or uses
// the global math/rand -- a fit is a pure function of its flags.
package main

import (
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
)

// Weights is the cast-weight profile the fitter moves.
type Weights = botpolicy.CastWeights

// EvalResult is one evaluation's outcome: how often the "+" candidate beat
// the "-" candidate over the dev suite, and over how many games that rate is
// taken (stalled games are excluded, exactly as the bench excludes them).
type EvalResult struct {
	PlusWins int
	Games    int
}

// WinRate is the "+" side's win rate over non-stalled games. A suite whose
// every game stalled has no rate at all; 0.5 (break-even) is returned so the
// gradient contributes nothing rather than a spurious extreme.
func (r EvalResult) WinRate() float64 {
	if r.Games <= 0 {
		return 0.5
	}
	return float64(r.PlusWins) / float64(r.Games)
}

// Evaluator plays one head-to-head evaluation. baseSeed is the common-random-
// numbers block the evaluation must use for ALL of its games, so the "+" and
// "-" profiles see identical per-game seeds (the variance reduction SPSA
// depends on). Implementations must be deterministic: the same
// (plus, minus, baseSeed) always yields the same result.
type Evaluator interface {
	Eval(plus, minus Weights, baseSeed uint64) (EvalResult, error)
}

// SPSASchedule is the gain schedule. a_k = A/(k+1)^Alpha shrinks the step;
// c_k = max(1, round(C/(k+1)^Gamma)) shrinks the perturbation but never below
// 1, because the weights are integers: a c_k below 1 would round both
// w+cΔ and w-cΔ back to w and the iteration would probe nothing.
type SPSASchedule struct {
	A, Alpha float64
	C, Gamma float64
}

// DefaultSchedule uses the "practical" SPSA exponents (Spall 1998): a decays
// 0.602, c decays 0.101. A is sized to the objective's scale, not to 1: a
// head-to-head win rate of 0.52 at c=4 gives |ĝ| = 0.005 in scaled units, and
// A=100 turns that into a first step of half a scaled unit -- the smallest
// move that changes a rounded weight of scale 1.
var DefaultSchedule = SPSASchedule{A: 100, Alpha: 0.602, C: 4.0, Gamma: 0.101}

// Schedule returns the (a, c) pair for iteration k (0-based).
func (s SPSASchedule) At(k int) (a, c float64) {
	denomA := math.Pow(float64(k+1), s.Alpha)
	denomC := math.Pow(float64(k+1), s.Gamma)
	a = s.A / denomA
	c = math.Round(s.C / denomC)
	if c < 1 {
		c = 1
	}
	return a, c
}

// Rademacher returns the perturbation signs for one iteration: one ±1 per
// fitted weight, drawn from a PRNG seeded by (seed, iter). Deriving the
// stream from (seed, iter) rather than carrying one PRNG across iterations
// makes iteration k's perturbation a pure function of the two, so a fit is
// reproducible even if only some iterations are re-run.
func Rademacher(seed uint64, iter, n int) []int {
	r := rand.New(rand.NewPCG(seed, uint64(iter)))
	out := make([]int, n)
	for i := range out {
		if r.Uint64()&1 == 0 {
			out[i] = 1
		} else {
			out[i] = -1
		}
	}
	return out
}

// TraceWriter is the optional per-iteration CSV trajectory.
type TraceWriter interface {
	WriteRow(iter int, a, c float64, res EvalResult, w Weights) error
	Close() error
}

// BenchFn, when non-nil, benches the current weights against the production
// bot and returns the win rate. RunSPSA calls it every BenchEvery iterations.
type BenchFn func(iter int, w Weights) (float64, error)

// Config configures one fit. Fit names the CastWeights fields to tune; the
// rest stay frozen at their init values (a zero-length Fit means every field).
type Config struct {
	Iters      int
	Seed       uint64
	Stride     uint64 // seeds consumed per evaluation (pairs * games)
	Schedule   SPSASchedule
	Fit        []string
	BenchEvery int
	Trace      TraceWriter
	Log        io.Writer
}

// Result is a completed fit's outcome.
type Result struct {
	Weights Weights
	History []Iteration
}

// Iteration is one recorded SPSA step, for the trace and for tests.
type Iteration struct {
	Index  int
	A, C   float64
	Result EvalResult
	Plus   Weights
	Minus  Weights
	Next   Weights
}

// RunSPSA runs the fit: iteration k draws Rademacher Δ from (seed, k), builds
// round(θ + c s Δ) and round(θ - c s Δ) as INTEGER profiles (s_i the fixed
// per-weight scale), evaluates them head-to-head on the same per-game seeds
// (baseSeed = seed + k*Stride), and steps the real-valued iterate
//
//	ĝ_i = (winrate - 0.5) * 2 / (2 c Δ_i)
//	θ_i ← θ_i + a s_i ĝ_i,   w_i = round(θ_i)
//
// Every BenchEvery iterations (when > 0) it also benches the current weights
// against the bot and logs the rate. The returned weights are the final
// profile; History carries every step.
func RunSPSA(cfg Config, init Weights, eval Evaluator, bench BenchFn) (Result, error) {
	if cfg.Iters < 1 {
		return Result{Weights: init}, fmt.Errorf("-iters must be at least 1, got %d", cfg.Iters)
	}
	fields, err := resolveFitFields(cfg.Fit)
	if err != nil {
		return Result{}, err
	}
	if len(fields) == 0 {
		return Result{}, fmt.Errorf("-fit names no weights")
	}

	// theta is the real-valued iterate; the integer profile the engine plays is
	// its rounding. Rounding the iterate itself after every step (the first
	// version) discarded every update smaller than half a unit, and a realistic
	// head-to-head gap of a few percent gives |a*ĝ| ≈ 0.005 per step -- the
	// weights never moved. Accumulating in float and rounding only what is
	// played keeps small, consistent gradients.
	//
	// scale puts the weights on a common footing: fitting CreatureBase (30)
	// and CurveFit (0) with the same ±c probe either drowns the small weights
	// or never moves the large ones. Each field is perturbed and stepped in
	// units of scale_i = max(1, |init_i|/4), fixed from the initial profile so
	// the schedule stays a pure function of the flags.
	theta := make([]float64, len(fields))
	scale := make([]float64, len(fields))
	for i, name := range fields {
		theta[i] = float64(getWeight(init, name))
		scale[i] = math.Max(1, math.Abs(theta[i])/4)
	}
	w := init
	res := Result{Weights: init}
	for k := 0; k < cfg.Iters; k++ {
		a, c := cfg.Schedule.At(k)
		delta := Rademacher(cfg.Seed, k, len(fields))
		plus, minus := w, w
		for i, name := range fields {
			step := c * scale[i] * float64(delta[i])
			p := clampInt32(int64(math.Round(theta[i] + step)))
			m := clampInt32(int64(math.Round(theta[i] - step)))
			if p == m {
				// c >= 1 and scale >= 1 make this unreachable; the guard keeps
				// the two probes distinct no matter how the rounding lands.
				if delta[i] > 0 {
					p++
				} else {
					m++
				}
			}
			setWeight(&plus, name, p)
			setWeight(&minus, name, m)
		}

		baseSeed := cfg.Seed + uint64(k)*cfg.Stride
		ev, err := eval.Eval(plus, minus, baseSeed)
		if err != nil {
			return res, fmt.Errorf("iteration %d evaluation: %w", k, err)
		}

		// ĝ_i is the gradient in scaled units; the step returns to weight units
		// through scale_i.
		next := w
		for i, name := range fields {
			g := (ev.WinRate() - 0.5) * 2 / (2 * c * float64(delta[i]))
			theta[i] += a * scale[i] * g
			if theta[i] > math.MaxInt32 {
				theta[i] = math.MaxInt32
			} else if theta[i] < math.MinInt32 {
				theta[i] = math.MinInt32
			}
			setWeight(&next, name, clampInt32(int64(math.Round(theta[i]))))
		}

		res.History = append(res.History, Iteration{
			Index: k, A: a, C: c, Result: ev, Plus: plus, Minus: minus, Next: next,
		})
		res.Weights = next
		if cfg.Trace != nil {
			if err := cfg.Trace.WriteRow(k, a, c, ev, next); err != nil {
				return res, fmt.Errorf("writing trace row %d: %w", k, err)
			}
		}
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "iter %d: a=%.4f c=%.0f winrate(plus vs minus)=%.4f games=%d\n",
				k, a, c, ev.WinRate(), ev.Games)
		}
		if bench != nil && cfg.BenchEvery > 0 && (k+1)%cfg.BenchEvery == 0 {
			rate, err := bench(k, next)
			if err != nil {
				return res, fmt.Errorf("bench at iteration %d: %w", k, err)
			}
			if cfg.Log != nil {
				fmt.Fprintf(cfg.Log, "iter %d: bench vs bot winrate=%.4f\n", k, rate)
			}
		}
		w = next
	}
	return res, nil
}

// clampInt32 saturates at the int32 boundary rather than wrapping, so a
// runaway step cannot flip a weight's sign.
func clampInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// weightFields lists every int32 CastWeights field the fitter can tune, in
// Go struct order (deterministic), each with a typed accessor.
// TestWeightFieldsCoverCastWeights fails when CastWeights gains an int32
// field this table misses.
var weightFields = []struct {
	name string
	ptr  func(*Weights) *int32
}{
	{"CreatureBase", func(w *Weights) *int32 { return &w.CreatureBase }},
	{"CreaturePower", func(w *Weights) *int32 { return &w.CreaturePower }},
	{"NonCreatureCMC", func(w *Weights) *int32 { return &w.NonCreatureCMC }},
	{"Kicked", func(w *Weights) *int32 { return &w.Kicked }},
	{"Flashback", func(w *Weights) *int32 { return &w.Flashback }},
	{"ReserveScale", func(w *Weights) *int32 { return &w.ReserveScale }},
	{"CommanderTaxScale", func(w *Weights) *int32 { return &w.CommanderTaxScale }},
	{"CastThreshold", func(w *Weights) *int32 { return &w.CastThreshold }},
	{"CreatureToughness", func(w *Weights) *int32 { return &w.CreatureToughness }},
	{"CurveFit", func(w *Weights) *int32 { return &w.CurveFit }},
	{"ManaLeft", func(w *Weights) *int32 { return &w.ManaLeft }},
	{"Precombat", func(w *Weights) *int32 { return &w.Precombat }},
	{"InstantOnOwnTurn", func(w *Weights) *int32 { return &w.InstantOnOwnTurn }},
	{"OppCreatures", func(w *Weights) *int32 { return &w.OppCreatures }},
	{"OwnCreatures", func(w *Weights) *int32 { return &w.OwnCreatures }},
	{"LifeDelta", func(w *Weights) *int32 { return &w.LifeDelta }},
	{"CreaturePrecombat", func(w *Weights) *int32 { return &w.CreaturePrecombat }},
	{"CreatureOppCreatures", func(w *Weights) *int32 { return &w.CreatureOppCreatures }},
	{"NonCreatureOppCreatures", func(w *Weights) *int32 { return &w.NonCreatureOppCreatures }},
	{"CreatureLifeDelta", func(w *Weights) *int32 { return &w.CreatureLifeDelta }},
	{"InstantSpeedOffTurnHold", func(w *Weights) *int32 { return &w.InstantSpeedOffTurnHold }},
	{"InstantOwnPreMain", func(w *Weights) *int32 { return &w.InstantOwnPreMain }},
	{"InstantOwnCombat", func(w *Weights) *int32 { return &w.InstantOwnCombat }},
	{"InstantOppTurn", func(w *Weights) *int32 { return &w.InstantOppTurn }},
	{"InstantOppEnd", func(w *Weights) *int32 { return &w.InstantOppEnd }},
	{"SetValue", func(w *Weights) *int32 { return &w.SetValue }},
}

// weightFieldNames lists the CastWeights fields the fitter can tune, in Go
// struct order (deterministic).
func weightFieldNames() []string {
	names := make([]string, len(weightFields))
	for i, f := range weightFields {
		names[i] = f.name
	}
	return names
}

// weightPtr returns the field named name, or nil for an unknown name.
func weightPtr(w *Weights, name string) *int32 {
	for _, f := range weightFields {
		if f.name == name {
			return f.ptr(w)
		}
	}
	return nil
}

// resolveFitFields turns a -fit comma list into the field names to tune,
// validating every name against the struct and preserving the list's order so
// the Rademacher stream's meaning is stable across runs. An empty list tunes
// every int32 field.
func resolveFitFields(fit []string) ([]string, error) {
	valid := make(map[string]bool)
	for _, n := range weightFieldNames() {
		valid[n] = true
	}
	var out []string
	seen := make(map[string]bool)
	for _, tok := range fit {
		name := strings.TrimSpace(tok)
		if name == "" {
			continue
		}
		if !valid[name] {
			known := weightFieldNames()
			sort.Strings(known)
			return nil, fmt.Errorf("-fit: unknown weight %q (known: %s)", name, strings.Join(known, ", "))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return weightFieldNames(), nil
	}
	return out, nil
}

// getWeight reads one int32 field by name. The caller validated the name.
func getWeight(w Weights, name string) int32 {
	return *weightPtr(&w, name)
}

// setWeight writes one int32 field by name. The caller validated the name.
func setWeight(w *Weights, name string, val int32) {
	*weightPtr(w, name) = val
}
