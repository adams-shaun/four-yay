package searchbench

// E1, the hidden-information test (docs/012 §2.5, docs/016 §3; upstream
// tools/search_bench/leak.py): pairs of positions that look the same to the
// deciding seat and differ only in cards it cannot see. A fair search
// decides the same in both members of a pair.
//
// The pairs and the fair arms' belief worlds are upstream's own
// (scripts/searchbench/leak_prep.py imports leak.py's probes() and
// public_worlds()); everything that touches an engine is here. Seeds pair
// across the two members exactly as upstream's request() does: seed s
// searches with 1000+s, the clairvoyant arm's real world is built with
// 5000+s, and the honest arms' worlds are public_worlds(real, 7+s) built
// with seeds derived from s alone -- so an honest arm's worlds, and so its
// whole search, are the same in X and Y unless something hidden leaks in.

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// LeakFormat is leak_prep.py's file format.
const LeakFormat = "sbrep-leak/v1"

// LeakCanary is the canary pair's card: outside FDN, so no belief can deal
// it, and held by B in the canary pair's X member.
const LeakCanary = "Counterspell"

// LeakFile is leak_prep.py's output.
type LeakFile struct {
	Format     string          `json:"format"`
	Upstream   json.RawMessage `json:"upstream"`
	SearchSeed int             `json:"search_seed"`
	Seeds      int             `json:"seeds"`
	Pairs      []LeakPair      `json:"pairs"`
}

// LeakPair is one probe: its two members.
type LeakPair struct {
	Name string     `json:"name"`
	X    LeakMember `json:"X"`
	Y    LeakMember `json:"Y"`
}

// Member is X or Y.
func (p *LeakPair) Member(w string) *LeakMember {
	if w == "X" {
		return &p.X
	}
	return &p.Y
}

// LeakMember is one position: its real spec, and per seed the eight belief
// worlds upstream's public_worlds draws from its public view.
type LeakMember struct {
	Real   json.RawMessage     `json:"real"`
	Worlds [][]json.RawMessage `json:"worlds"`
}

// ReadLeakFile reads a leak_prep.py file (gzipped JSON).
func ReadLeakFile(path string) (*LeakFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var lf LeakFile
	dec := json.NewDecoder(bufio.NewReader(zr))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lf); err != nil {
		return nil, fmt.Errorf("searchbench: leak probes %s: %w", path, err)
	}
	if lf.Format != LeakFormat {
		return nil, fmt.Errorf("searchbench: leak probes %s: format %q, want %q", path, lf.Format, LeakFormat)
	}
	for _, p := range lf.Pairs {
		for _, w := range []string{"X", "Y"} {
			if n := len(p.Member(w).Worlds); n < lf.Seeds {
				return nil, fmt.Errorf("searchbench: leak probes %s: %s/%s has %d seeds' worlds, want %d", path, p.Name, w, n, lf.Seeds)
			}
		}
	}
	return &lf, nil
}

// LeakCardNames lists, sorted and distinct, every card name the file's
// specs seat: the pool a subset registry must hold.
func LeakCardNames(lf *LeakFile) ([]string, error) {
	seen := map[string]bool{} // membership only; the output is sorted
	add := func(n string) {
		if n != "" {
			seen[n] = true
			if i := strings.Index(n, " // "); i > 0 {
				seen[n[:i]] = true
			}
		}
	}
	for _, p := range lf.Pairs {
		for _, w := range []string{"X", "Y"} {
			m := p.Member(w)
			raws := []json.RawMessage{m.Real}
			for _, ws := range m.Worlds {
				raws = append(raws, ws...)
			}
			for _, raw := range raws {
				spec, err := statespec.Parse(raw)
				if err != nil {
					return nil, fmt.Errorf("searchbench: %s/%s: %w", p.Name, w, err)
				}
				specCardNames(spec, add)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	slices.Sort(out)
	return out, nil
}

// Upstream's request() seeds: the search seed, the clairvoyant real world's
// build seed, and (gorge's, since upstream's bridge seeds its own world
// builds) the honest arms' world build seeds -- functions of s alone, so
// identical in both members of a pair.
func LeakSearchSeed(s int) uint64 { return 1000 + uint64(s) }
func LeakRealSeed(s int) uint64   { return 5000 + uint64(s) }
func LeakWorldSeeds(s, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = splitMix64(0x6c65616b<<32 ^ uint64(s)<<8 ^ uint64(i))
	}
	return out
}

// PositionLeak materialises a probe position: the real spec (realSeed) at
// its staged priority, its canonical options, and the first n of worlds
// (worldSeeds). Every world is parsed whatever n, so the library sizes (and
// so the real engine) do not depend on how many worlds an arm reads, as in
// PositionItem. The probes are PRIORITY_FRESH positions, so the item
// decision is the staged decision itself: no Reach walk.
func PositionLeak(reg *cards.Registry, real json.RawMessage, worlds []json.RawMessage, realSeed uint64, worldSeeds []uint64, n int) (*Positioned, error) {
	spec, err := statespec.Parse(real)
	if err != nil {
		return nil, &ItemRefusal{StageBuild, refuse("spec decode", "%v", err)}
	}
	wspecs := make([]*statespec.Spec, len(worlds))
	for i, raw := range worlds {
		if wspecs[i], err = statespec.Parse(raw); err != nil {
			return nil, &ItemRefusal{StageWorlds, refuse("spec decode", "world %d: %v", i, err)}
		}
	}
	equalLibraries(append([]*statespec.Spec{spec}, wspecs...))
	dp := SeatID("A") // leak.py's decisionPlayer
	m, err := Materialize(reg, spec, realSeed)
	if err != nil {
		return nil, &ItemRefusal{StageBuild, err}
	}
	r, err := reachStaged(m.Engine, dp, int32(spec.Turn))
	if err != nil {
		return nil, &ItemRefusal{StageReach, err}
	}
	p := &Positioned{Real: Position{M: m, Reached: r}, seat: SeatName(dp), specs: wspecs}
	if p.Canon, err = BuildCanon(m, r); err != nil {
		return nil, &ItemRefusal{StageCanon, err}
	}
	if n > len(wspecs) || n > len(worldSeeds) {
		return nil, &ItemRefusal{StageWorlds, refuse("count", "%d worlds, %d seeds, want %d", len(wspecs), len(worldSeeds), n)}
	}
	for i := 0; i < n; i++ {
		wm, err := Materialize(reg, wspecs[i], worldSeeds[i])
		if err != nil {
			return nil, &ItemRefusal{StageWorlds, fmt.Errorf("world %d: %w", i, err)}
		}
		wr, err := reachStaged(wm.Engine, dp, int32(spec.Turn))
		if err != nil {
			return nil, &ItemRefusal{StageWorlds, fmt.Errorf("world %d: %w", i, err)}
		}
		p.Worlds = append(p.Worlds, Position{M: wm, Reached: wr})
	}
	if err := CheckRoots(m.Engine, p.WorldEngines()); err != nil {
		return nil, &ItemRefusal{StageWorlds, refuse("observation", "%v", err)}
	}
	return p, nil
}

// reachStaged is the staged priority of a PRIORITY_FRESH probe: dp's
// priority in turn with an empty stack, read as a spell decision.
func reachStaged(e *rules.Engine, dp state.PlayerID, turn int32) (*Reached, error) {
	d := e.Pending()
	if d == nil {
		if err := e.AdvanceHypothetical(); err != nil {
			return nil, refuse("chance", "%v", err)
		}
		d = e.Pending()
	}
	switch {
	case d == nil || e.G.Over:
		return nil, refuse("no decision", "turn %d", e.G.Turn)
	case d.Kind != decision.KPriority || d.Player != dp || e.G.Turn != turn || len(e.G.Stack) != 0:
		return nil, refuse("reached", "%s for %s at %s turn %d", strings.ToUpper(string(d.Kind)), SeatName(d.Player), StepName(e.G.Step), e.G.Turn)
	}
	return &Reached{Kind: DecisionSpell, Decision: d}, nil
}

// LeakConfig is one probe search's knobs.
type LeakConfig struct {
	Sims         int
	Discount     float64
	DiscountUnit azmcts.DiscountUnit
	NodeCache    int
}

// LeakOption is one canonical option's root statistics.
type LeakOption struct {
	Label  string  `json:"label"`
	Visits int     `json:"visits"`
	Q      float64 `json:"q"` // gorge's [0,1] scale
}

// LeakRow is one search of one member of one pair.
type LeakRow struct {
	Pair     string  `json:"pair"`
	World    string  `json:"world"`
	Arm      string  `json:"arm"`
	Seed     int     `json:"seed"`
	Sims     int     `json:"sims"`
	Discount float64 `json:"discount"`
	Unit     string  `json:"unit"`
	// Options are the canonical labels; Best is the chosen one.
	Options  []string     `json:"options"`
	Best     string       `json:"best"`
	Searched bool         `json:"searched"`
	Fallback string       `json:"fallback,omitempty"`
	Root     []LeakOption `json:"root"`
	// Hidden lists, sorted and distinct, every card name in a zone the
	// deciding seat cannot see (the opponent's hand, either library) of
	// every world the search walked: the real engine for the clairvoyant
	// arm; for the honest arms every world they were given and every
	// world a simulation was dealt (OnWorld).
	Hidden    []string `json:"hidden"`
	Completed int      `json:"completed"`
	EnvSteps  int64    `json:"env_steps"`
	// CoreSeconds is filled at the command boundary.
	CoreSeconds float64 `json:"core_seconds"`
}

// hiddenNames records hidden-zone card names from actor's seat.
type hiddenNames struct {
	actor state.PlayerID
	seen  map[string]bool // membership only; listed sorted
}

func (h *hiddenNames) add(e *rules.Engine) {
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Card == nil {
			continue
		}
		if o.Zone == state.ZLibrary || o.Zone == state.ZHand && o.Owner != h.actor {
			if f := o.Face(); f != nil {
				h.seen[f.Name] = true
			}
		}
	}
}

func (h *hiddenNames) list() []string {
	out := make([]string, 0, len(h.seen))
	for n := range h.seen {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// RunLeak runs one arm on one member of one pair at seed s.
func RunLeak(ctx context.Context, reg *cards.Registry, pair *LeakPair, world string, arm SearchArm, s int, cfg LeakConfig) (LeakRow, error) {
	row := LeakRow{Pair: pair.Name, World: world, Arm: string(arm), Seed: s, Sims: cfg.Sims, Discount: cfg.Discount, Unit: cfg.DiscountUnit.String()}
	m := pair.Member(world)
	if s >= len(m.Worlds) {
		return row, fmt.Errorf("searchbench: %s/%s has no seed %d", pair.Name, world, s)
	}
	worlds := m.Worlds[s]
	pos, err := PositionLeak(reg, m.Real, worlds, LeakRealSeed(s), LeakWorldSeeds(s, len(worlds)), WorldsFor(arm))
	if err != nil {
		return row, fmt.Errorf("searchbench: %s/%s seed %d: %w", pair.Name, world, s, err)
	}
	real := pos.Real.M.Engine
	row.Options = pos.Canon.Labels()
	opts := BenchOptions(cfg.Sims)
	opts.Discount, opts.DiscountUnit, opts.NodeCache = cfg.Discount, cfg.DiscountUnit, cfg.NodeCache
	seed := LeakSearchSeed(s)
	h := &hiddenNames{actor: real.Pending().Player, seen: map[string]bool{}}
	in := ArmInput{Arm: arm, Options: opts, Seed: seed, Real: real, Worlds: pos.WorldEngines()}
	if arm == ArmClairvoyant {
		h.add(real)
	} else {
		for _, w := range in.Worlds {
			h.add(w)
		}
		in.OnWorld = h.add
	}
	res, err := RunArm(ctx, in)
	if err != nil {
		return row, fmt.Errorf("searchbench: %s/%s seed %d %s: %w", pair.Name, world, s, arm, err)
	}
	var choice int
	if res.Searched && azmcts.IsMacroKey(res.Key) {
		choice, _, err = pos.Canon.projectMacro(res.Key)
	} else {
		choice, _, err = pos.Canon.Project(real, res.Intent)
	}
	if err != nil && IsManaActivation(real.Pending(), res.Intent) {
		choice, _, err = followBot(pos.Canon, real, res.Intent, splitMix64(seed^0x626f742d61727365))
	}
	if err != nil {
		return row, fmt.Errorf("searchbench: %s/%s seed %d %s: projecting the answer: %w", pair.Name, world, s, arm, err)
	}
	row.Best = row.Options[choice]
	row.Searched = res.Searched
	if !res.Searched {
		row.Fallback = fallbackReason(res)
	}
	row.Completed, row.EnvSteps = res.Stats.Completed, int64(res.Stats.EnvSteps)
	if len(res.Table) > 0 {
		root, err := projectRoot(pos, res.Table)
		if err != nil {
			return row, fmt.Errorf("searchbench: %s/%s seed %d %s: projecting the root: %w", pair.Name, world, s, arm, err)
		}
		for _, o := range root {
			row.Root = append(row.Root, LeakOption{Label: row.Options[o.Choice], Visits: o.Visits, Q: o.Q})
		}
	}
	row.Hidden = h.list()
	return row, nil
}

// LeakQScale converts gorge's [0,1] values to upstream's [-1,1] (Q' = 2Q-1,
// so a difference doubles): the ±0.1 rule is on upstream's scale.
const LeakQScale = 2

// LeakOptionStat is one option's paired difference over the seeds.
type LeakOptionStat struct {
	Label string `json:"label"`
	N     int    `json:"n"`
	// DQ is the mean X-Y difference and CI its normal 95% interval
	// (1.96 sd/sqrt(n), upstream's), on upstream's [-1,1] scale; CI is
	// nil (unbounded, so outside ±0.1) with fewer than two seeds.
	DQ float64     `json:"dQ"`
	CI *[2]float64 `json:"ci"`
	// MaxAbs is the largest |X-Y| of any one seed (upstream scale): 0
	// exactly when every seed's searches agree to the bit.
	MaxAbs float64 `json:"max_abs"`
}

// LeakVerdict is one (pair, arm) of the report.
type LeakVerdict struct {
	Pair    string           `json:"pair"`
	Arm     string           `json:"arm"`
	Sims    int              `json:"sims"`
	Seeds   int              `json:"seeds"`
	Pass    bool             `json:"pass"`
	QInside bool             `json:"q_inside"`
	PermP   float64          `json:"perm_p"`
	Worst   string           `json:"worst"`
	Options []LeakOptionStat `json:"options"`
	ChosenX map[string]int   `json:"chosen_x"`
	ChosenY map[string]int   `json:"chosen_y"`
	// Identical counts the seeds whose X and Y searches gave the same
	// choice and the same root table (visits and Q) to the bit.
	Identical int `json:"identical"`
	// CanaryX/CanaryY count, for the canary pair only, the searches whose
	// walked worlds held the canary card in a hidden zone.
	CanaryX *int `json:"canary_x,omitempty"`
	CanaryY *int `json:"canary_y,omitempty"`
}

// AnalyzeLeak applies docs/012 §2.5's rule to rows: for each (pair, arm,
// sims), over the seeds both members answered, each option's paired ΔQ =
// Q(X) - Q(Y) (options both roots hold), its 95% CI, and the chosen
// actions' permutation test (leak.py _fisher_2xk). A pair passes when
// every option's CI lies inside ±0.1, the choices do not differ
// (p > 0.01), and -- for the canary -- no search of either member walked a
// world holding the canary. Verdicts are sorted by pair order, then arm.
func AnalyzeLeak(rows []LeakRow, pairOrder []string, armOrder []string) []LeakVerdict {
	type key struct {
		pair, arm string
		sims      int
	}
	type mk struct {
		world string
		seed  int
	}
	by := map[key]map[mk]*LeakRow{} // grouped, then visited in sorted key order
	for i := range rows {
		r := &rows[i]
		k := key{r.Pair, r.Arm, r.Sims}
		if by[k] == nil {
			by[k] = map[mk]*LeakRow{}
		}
		by[k][mk{r.World, r.Seed}] = r
	}
	keys := make([]key, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	rank := func(list []string, v string) int {
		if i := slices.Index(list, v); i >= 0 {
			return i
		}
		return len(list)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if ra, rb := rank(pairOrder, a.pair), rank(pairOrder, b.pair); ra != rb {
			return ra < rb
		}
		if a.pair != b.pair {
			return a.pair < b.pair
		}
		if ra, rb := rank(armOrder, a.arm), rank(armOrder, b.arm); ra != rb {
			return ra < rb
		}
		if a.arm != b.arm {
			return a.arm < b.arm
		}
		return a.sims < b.sims
	})
	var out []LeakVerdict
	for _, k := range keys {
		d := by[k]
		var seeds []int
		for m := range d {
			if m.world == "X" && d[mk{"Y", m.seed}] != nil {
				seeds = append(seeds, m.seed)
			}
		}
		if len(seeds) == 0 {
			continue
		}
		slices.Sort(seeds)
		v := LeakVerdict{Pair: k.pair, Arm: k.arm, Sims: k.sims, Seeds: len(seeds), QInside: true,
			ChosenX: map[string]int{}, ChosenY: map[string]int{}}
		diffs := map[string][]float64{} // lookup only; labels listed sorted
		var cx, cy []string
		canX, canY := 0, 0
		for _, s := range seeds {
			x, y := d[mk{"X", s}], d[mk{"Y", s}]
			qy := map[string]float64{}
			for _, o := range y.Root {
				if o.Visits > 0 {
					qy[o.Label] = o.Q
				}
			}
			for _, o := range x.Root {
				if q, ok := qy[o.Label]; ok && o.Visits > 0 {
					diffs[o.Label] = append(diffs[o.Label], LeakQScale*(o.Q-q))
				}
			}
			v.ChosenX[x.Best]++
			v.ChosenY[y.Best]++
			cx, cy = append(cx, x.Best), append(cy, y.Best)
			if x.Best == y.Best && slices.Equal(x.Root, y.Root) {
				v.Identical++
			}
			if slices.Contains(x.Hidden, LeakCanary) {
				canX++
			}
			if slices.Contains(y.Hidden, LeakCanary) {
				canY++
			}
		}
		labels := make([]string, 0, len(diffs))
		for l := range diffs {
			labels = append(labels, l)
		}
		slices.Sort(labels)
		worst := 0.0
		for _, l := range labels {
			xs := diffs[l]
			n := float64(len(xs))
			mu, maxAbs := 0.0, 0.0
			for _, x := range xs {
				mu += x
				maxAbs = math.Max(maxAbs, math.Abs(x))
			}
			mu /= n
			st := LeakOptionStat{Label: l, N: len(xs), DQ: mu, MaxAbs: maxAbs}
			if len(xs) > 1 {
				ss := 0.0
				for _, x := range xs {
					ss += (x - mu) * (x - mu)
				}
				half := 1.96 * math.Sqrt(ss/(n-1)) / math.Sqrt(n)
				st.CI = &[2]float64{mu - half, mu + half}
			}
			if st.CI == nil || !(st.CI[0] >= -0.1 && st.CI[1] <= 0.1) {
				v.QInside = false
			}
			if v.Worst == "" || math.Abs(mu) > math.Abs(worst) {
				v.Worst, worst = l, mu
			}
			v.Options = append(v.Options, st)
		}
		v.PermP = PermutationP(cx, cy)
		v.Pass = v.QInside && v.PermP > 0.01
		if k.pair == "canary" {
			v.CanaryX, v.CanaryY = &canX, &canY
			v.Pass = v.Pass && canX == 0 && canY == 0
		}
		out = append(out, v)
	}
	return out
}

// PermutationP is leak.py's _fisher_2xk: a permutation test on the
// chi-square statistic of two samples of chosen actions, 4000 shuffles by
// CPython's random.Random(0) (pyRandom), p = (hits+1)/4001; 1 when the
// statistic is 0.
func PermutationP(a, b []string) float64 {
	count := func(xs []string) map[string]int {
		c := map[string]int{}
		for _, x := range xs {
			c[x]++
		}
		return c
	}
	ca, cb := count(a), count(b)
	set := map[string]bool{}
	for _, x := range append(slices.Clone(a), b...) {
		set[x] = true
	}
	labels := make([]string, 0, len(set))
	for l := range set {
		labels = append(labels, l)
	}
	slices.Sort(labels)
	var xs []string
	for _, l := range labels {
		for i := 0; i < ca[l]; i++ {
			xs = append(xs, l)
		}
	}
	for _, l := range labels {
		for i := 0; i < cb[l]; i++ {
			xs = append(xs, l)
		}
	}
	na := len(a)
	chi := func(sa, sb []string) float64 {
		ka, kb := count(sa), count(sb)
		n := float64(len(sa) + len(sb))
		s := 0.0
		for _, l := range labels {
			tot := float64(ka[l] + kb[l])
			for _, cm := range [][2]float64{{float64(ka[l]), float64(len(sa))}, {float64(kb[l]), float64(len(sb))}} {
				if e := tot * cm[1] / n; e > 0 {
					s += (cm[0] - e) * (cm[0] - e) / e
				}
			}
		}
		return s
	}
	obs := chi(xs[:na], xs[na:])
	if obs == 0 {
		return 1
	}
	rng := newPyRandom(0)
	hit := 0
	for k := 0; k < 4000; k++ {
		for i := len(xs) - 1; i > 0; i-- { // random.shuffle
			j := rng.below(i + 1)
			xs[i], xs[j] = xs[j], xs[i]
		}
		if chi(xs[:na], xs[na:]) >= obs-1e-12 {
			hit++
		}
	}
	return float64(hit+1) / 4001
}
