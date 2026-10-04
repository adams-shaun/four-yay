package pay

// The payment planner's search (spec 5, amended 2026-09-26): a rank-aware,
// bounded, deterministic search over CLASSES of interchangeable source units.
// Like the rest of the planner it is a pure read: no event, no state write,
// no RNG.

import (
	"cmp"
	"fmt"
	"math/bits"
	"slices"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Class is one equivalence class of source units: units whose
// alternative lists are identical in everything the rank and the witness read
// except the source itself (ability identity, produced vector, tier,
// consequence, creature flag, flexibility and colour census, in the same
// alternative order). Thirty Mountains are one class; the search chooses how
// MANY of a class to use for each alternative, never which ones, and the
// witness is materialised afterwards in the canonical key-8 order.
type Class struct {
	Members []int // unit indices (positions in the query's unit list), ascending
	alts    []Alt
	// pick is the class's alternative indices in witness-step order (the
	// key-8 compare with the source left out), so materialisation hands each
	// chosen unit the smallest alternative still owed.
	pick []int
	// irr is each alternative's irreversible cost (rank key 1).
	irr []int64
	// pain is each alternative's life + damage (the lethal guard's measure).
	pain []int64
}

// sameClass reports whether two units' alternative lists are
// interchangeable for the search: equal, alternative by alternative, in
// every field except the source's own identity (Source, SourceZoneSeq) and
// the concrete ability pointers execution uses.
func sameClass(a, b []Alt) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for i := range a {
		x, y := &a[i], &b[i]
		if x.Activation.Ability != y.Activation.Ability || x.Activation.Produces != y.Activation.Produces ||
			x.Mana != y.Mana || x.Creature != y.Creature || x.Flex != y.Flex || x.Colours != y.Colours ||
			x.Tier != y.Tier || x.Consequence != y.Consequence {
			return false
		}
	}
	return true
}

// PhaseChoices keeps, per unit, the alternatives of at least
// minTier (spec 5): phase 1 passes TierNormal and sees normal sources
// only. Phase 2's table (normal and last-resort alternatives together, with
// the phase-2 flexibility and the one-step lethal filter) is
// paymentPlanLastResortChoices. Unit positions are preserved, so a unit with
// nothing left is simply absent from every class.
func PhaseChoices(choices [][]Alt, minTier Tier) [][]Alt {
	out := make([][]Alt, len(choices))
	for i, alts := range choices {
		if !slices.ContainsFunc(alts, func(a Alt) bool { return a.Tier < minTier }) {
			out[i] = alts
			continue
		}
		for _, a := range alts {
			if a.Tier >= minTier {
				out[i] = append(out[i], a)
			}
		}
	}
	return out
}

// Classes groups units into classes, in order of each class's
// first member. Grouping is a linear scan with an explicit equality, so the
// result never depends on map iteration.
func Classes(choices [][]Alt) []Class {
	var out []Class
	for i, alts := range choices {
		if len(alts) == 0 {
			continue
		}
		k := slices.IndexFunc(out, func(c Class) bool { return sameClass(c.alts, alts) })
		if k < 0 {
			out = append(out, Class{alts: alts})
			k = len(out) - 1
		}
		out[k].Members = append(out[k].Members, i)
	}
	for k := range out {
		c := &out[k]
		c.pick = make([]int, len(c.alts))
		c.irr = make([]int64, len(c.alts))
		c.pain = make([]int64, len(c.alts))
		for a := range c.alts {
			c.pick[a] = a
			c.irr[a] = ConsequenceCost(c.alts[a].Consequence, c.alts[a].Creature)
			c.pain[a] = ConsequencePain(c.alts[a].Consequence)
		}
		slices.SortStableFunc(c.pick, func(x, y int) int {
			sx := RankStep{Act: c.alts[x].Activation, Consequence: c.alts[x].Consequence}
			sy := RankStep{Act: c.alts[y].Activation, Consequence: c.alts[y].Consequence}
			sx.Act.Source, sy.Act.Source = 0, 0
			return CompareSteps([]RankStep{sx}, []RankStep{sy})
		})
	}
	return out
}

// level is one search level: how many units of one class take one
// alternative.
type level struct {
	class, alt int
}

// boundStats summarise what the units still selectable at or
// after a level can contribute, per unit: the most mana of each colour
// subset (bit i = mana index i, so mask 63 is the total), and the least
// amount, irreversible cost, creature flag and flexibility. They drive the
// feasibility check and the branch-and-bound lower bound.
type boundStats struct {
	most     [64]int32
	least    int32
	irr      int64
	creature int
	flex     int
	any      bool
}

func (b *boundStats) add(a *Alt, irr int64) {
	var sub [64]int32
	for mask := 1; mask < 64; mask++ {
		low := bits.TrailingZeros(uint(mask))
		sub[mask] = sub[mask&(mask-1)] + a.Mana[low]
		b.most[mask] = max(b.most[mask], sub[mask])
	}
	creature := 0
	if a.Creature {
		creature = 1
	}
	if !b.any {
		b.least, b.irr, b.creature, b.flex, b.any = sub[63], irr, creature, a.Flex, true
		return
	}
	b.least = min(b.least, sub[63])
	b.irr = min(b.irr, irr)
	b.creature = min(b.creature, creature)
	b.flex = min(b.flex, a.Flex)
}

// merge folds o into b as the union of two unit populations, weighting the
// per-unit maxima into capacities is the caller's job.
func (b boundStats) merge(o boundStats) boundStats {
	if !o.any {
		return b
	}
	if !b.any {
		return o
	}
	for m := range b.most {
		b.most[m] = max(b.most[m], o.most[m])
	}
	b.least = min(b.least, o.least)
	b.irr = min(b.irr, o.irr)
	b.creature = min(b.creature, o.creature)
	b.flex = min(b.flex, o.flex)
	return b
}

// Env is what the search needs from the engine's mana solver.
type Env struct {
	// Settle pays cost out of pool (the pool plus everything the chosen
	// units produced) with life available and reports the pool left.
	Settle func(c Cost, pool state.Mana, life int32) (after state.Mana, ok bool)
	// Verify turns on the count-rank cross-check (the walk-cache verify mode).
	Verify bool
}

// Search is one phase's search state.
type Search struct {
	// sc is the working storage the search was drawn from
	// (SearchInto); materialize builds its witness steps there.
	sc      *SearchScratch
	env     Env
	cost    Cost
	pool    state.Mana
	life    int32
	ctx     RankContext
	choices [][]Alt
	classes []Class
	levels  []level
	// levelStats[l] covers the current class's alternatives from level l to
	// the class's last level; laterCap[l] / laterStats[l] cover every class
	// after it (whose units are all still unused when level l is reached).
	levelStats []boundStats
	laterStats []boundStats
	laterCap   [][64]int32
	counts     [][]int32 // [class][alternative] units chosen
	used       []int32   // [class] units chosen
	produced   state.Mana
	sources    int
	creatures  int
	flex       int
	irr        int64
	pain       int64 // summed life + damage of the chosen units

	Nodes    int
	Limited  bool
	Best     *decision.PaymentPlan
	BestRank Rank
}

// Run runs one phase over choices (already filtered to the
// phase's tiers) and their classes (Classes(choices), which it
// only reads). ctx is the query's rank context. The result is the
// rank-best complete plan under Rank.less -- the same plan an
// exhaustive walk over every subset and alternative would return -- unless
// the node budget runs out first, when it is the best complete plan found
// (Reason "search_limit"), or none.
//
// A count vector whose summed life + damage is at least life is never a plan
// (the lethal guard, spec 5): pain only grows as units are added, so such a
// branch is cut outright, and no count is taken past the pain the caster can
// still afford.
//
// Exactness rests on three rank-free or rank-monotone facts:
//
//   - a set that pays is never extended: a superset has strictly more
//     sources and no smaller irreversible cost or creature count, so it
//     ranks strictly lower (keys 1-3);
//   - a level never takes more units of one alternative than the remaining
//     deficit can use (the total, or a colour it produces): with one fewer
//     the plan would still pay, so the larger count is a non-minimal
//     superset (the same argument);
//   - a branch is cut only when no completion can pay (a Hall-style
//     capacity check over every subset of the deficit colours) or when a
//     bound on every completion's rank is already lexicographically worse
//     than the best complete plan: a componentwise lower bound on the
//     monotone prefix (irreversible cost, creatures, sources, surplus,
//     flexibility; keys 1-5), then keys 6 and 7 (hand-reserve coverage and
//     remainder diversity), whose current values bound every completion's
//     from above because both only fall as units are added (see reserve).
//
// Branches that tie the bound are explored, and each complete count vector
// is materialised as its key-8-least witness, so ties on keys 1-7 resolve
// exactly as the exhaustive order does. The node budget is the only source
// of inexactness: a search that reaches it returns the best plan found.
func Run(cost Cost, pool state.Mana, life int32, ctx RankContext, choices [][]Alt, classes []Class, env Env) *Search {
	return SearchInto(nil, cost, pool, life, ctx, choices, classes, env)
}

// SearchScratch is a search's reusable working storage: the
// level order and its suffix statistics, the reordered classes and the
// unit counters. A search is a pure function of its arguments that calls
// back into nothing, so one engine-owned scratch serves every search the
// engine runs; the returned search's best plan never points into it.
type SearchScratch struct {
	search     Search
	classes    []Class
	levels     []level
	levelStats []boundStats
	laterStats []boundStats
	laterCap   [][64]int32
	counts     [][]int32
	countsFlat []int32
	used       []int32
	// materialize's working storage: the witness steps (read by the
	// caller's witness and rank builders, which copy them, before the next
	// materialize), the owed counts and the per-class cursors.
	chosen      []Alt
	owed        []int32
	owedAltFlat []int32
	owedAlt     [][]int32
	next        []int
}

// SearchInto is Run with its working storage drawn
// from sc (nil allocates it). The search reads only slots it wrote first:
// every array is cleared or fully assigned before the walk.
func SearchInto(sc *SearchScratch, cost Cost, pool state.Mana, life int32, ctx RankContext, choices [][]Alt, classes []Class, env Env) *Search {
	if sc == nil {
		sc = &SearchScratch{}
	}
	// The search struct itself is the scratch's too: the caller reads only
	// its result fields before the next search starts.
	s := &sc.search
	*s = Search{sc: sc, env: env, cost: cost, pool: pool, life: life, ctx: ctx, choices: choices, classes: classes}
	s.order(sc)
	total := 0
	for k := range s.classes {
		total += len(s.classes[k].alts)
	}
	sc.countsFlat = resizeCleared(sc.countsFlat, total)
	sc.counts = resizeCleared(sc.counts, len(s.classes))
	s.counts = sc.counts
	at := 0
	for k := range s.classes {
		n := len(s.classes[k].alts)
		s.counts[k] = sc.countsFlat[at : at+n : at+n]
		at += n
	}
	sc.used = resizeCleared(sc.used, len(s.classes))
	s.used = sc.used
	s.walk(0)
	return s
}

// order fixes the level order: pips first, cheapest first. Classes whose
// alternatives can pay a colour the pool leaves short go ahead of generic-
// only classes of the same irreversible cost and creature flag; within that,
// the least flexible class first, then battlefield order. Inside a class the
// alternatives producing a short colour come first. The order only steers
// how early a good plan is found; exactness never depends on it.
func (s *Search) order(sc *SearchScratch) {
	short, _ := s.deficit()
	shortMask := 0
	for i, n := range short {
		if n > 0 {
			shortMask |= 1 << i
		}
	}
	pips := func(a *Alt) bool {
		for i, n := range a.Mana {
			if n > 0 && shortMask&(1<<i) != 0 {
				return true
			}
		}
		return false
	}
	type classKey struct {
		irr      int64
		creature bool
		pips     bool
		flex     int
		first    int
	}
	keys := make([]classKey, len(s.classes))
	for k := range s.classes {
		c := &s.classes[k]
		key := classKey{irr: slices.Min(c.irr), creature: c.alts[0].Creature, flex: c.alts[0].Flex, first: c.Members[0]}
		for a := range c.alts {
			key.pips = key.pips || pips(&c.alts[a])
		}
		keys[k] = key
	}
	flag := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	idx := make([]int, len(s.classes))
	for k := range idx {
		idx[k] = k
	}
	slices.SortFunc(idx, func(x, y int) int {
		a, b := keys[x], keys[y]
		return cmp.Or(cmp.Compare(a.irr, b.irr), cmp.Compare(flag(a.creature), flag(b.creature)),
			cmp.Compare(flag(b.pips), flag(a.pips)), cmp.Compare(a.flex, b.flex), cmp.Compare(a.first, b.first))
	})
	classes := resizeCleared(sc.classes, len(idx))
	for k, from := range idx {
		classes[k] = s.classes[from]
	}
	s.classes, sc.classes = classes, classes
	s.levels = sc.levels[:0]

	var classStart []int
	for k := range s.classes {
		c := &s.classes[k]
		alts := make([]int, len(c.alts))
		for a := range alts {
			alts[a] = a
		}
		slices.SortStableFunc(alts, func(x, y int) int {
			return cmp.Or(cmp.Compare(flag(pips(&c.alts[y])), flag(pips(&c.alts[x]))), cmp.Compare(c.irr[x], c.irr[y]))
		})
		classStart = append(classStart, len(s.levels))
		for _, a := range alts {
			s.levels = append(s.levels, level{class: k, alt: a})
		}
	}
	// Suffix statistics. levelStats runs backwards inside each class;
	// laterStats/laterCap accumulate whole classes from the end.
	sc.levels = s.levels
	sc.levelStats = resizeCleared(sc.levelStats, len(s.levels))
	sc.laterStats = resizeCleared(sc.laterStats, len(s.levels))
	sc.laterCap = resizeCleared(sc.laterCap, len(s.levels))
	s.levelStats, s.laterStats, s.laterCap = sc.levelStats, sc.laterStats, sc.laterCap
	var later boundStats
	var laterCap [64]int32
	for k := len(s.classes) - 1; k >= 0; k-- {
		c := &s.classes[k]
		end := len(s.levels)
		if k+1 < len(classStart) {
			end = classStart[k+1]
		}
		var inClass boundStats
		for l := end - 1; l >= classStart[k]; l-- {
			inClass.add(&c.alts[s.levels[l].alt], c.irr[s.levels[l].alt])
			s.levelStats[l] = inClass
			s.laterStats[l] = later
			s.laterCap[l] = laterCap
		}
		n := int32(len(c.Members))
		for m := range laterCap {
			laterCap[m] += n * inClass.most[m]
		}
		later = later.merge(inClass)
	}
}

// deficit is what the pool and the produced mana still leave unpaid: each
// coloured (and true colourless) requirement, and the total.
func (s *Search) deficit() (state.Mana, int32) {
	var short state.Mana
	for i := range short {
		if d := s.cost.Colored[i] - s.pool[i] - s.produced[i]; d > 0 {
			short[i] = d
		}
	}
	total := s.cost.Generic + s.cost.Colored.Total() - s.pool.Total() - s.produced.Total()
	return short, max(total, 0)
}

func ceilDiv(a, b int32) int32 {
	return (a + b - 1) / b
}

func (s *Search) walk(level int) {
	if s.Nodes >= decision.MaxPaymentPlanSearchNodes {
		s.Limited = true
		return
	}
	s.Nodes++
	if s.pain > 0 && s.pain >= int64(s.life) {
		return // lethal, and every extension is too
	}
	short, total := s.deficit()
	if total == 0 && short == (state.Mana{}) && s.complete() {
		return
	}
	if level == len(s.levels) || s.sources >= decision.MaxPaymentActivations {
		return
	}
	if !s.promising(level, short, total) {
		return
	}
	lv := s.levels[level]
	c := &s.classes[lv.class]
	a := &c.alts[lv.alt]
	// A count past what the deficit can use is a non-minimal superset.
	var useful int32
	if amt := a.Mana.Total(); amt > 0 {
		useful = ceilDiv(total, amt)
	}
	for i, n := range a.Mana {
		if n > 0 && short[i] > 0 {
			useful = max(useful, ceilDiv(short[i], n))
		}
	}
	hi := min(int32(len(c.Members))-s.used[lv.class], int32(decision.MaxPaymentActivations-s.sources), useful)
	if pain := c.pain[lv.alt]; pain > 0 {
		// Only counts that keep the summed pain below life can be a plan.
		hi = min(hi, int32(max(0, (int64(s.life)-1-s.pain)/pain)))
	}
	for n := hi; n >= 0; n-- {
		s.take(lv, n)
		s.walk(level + 1)
		s.take(lv, -n)
		if s.Limited {
			return
		}
	}
}

// take adds (n > 0) or removes (n < 0) n units of level lv's alternative.
func (s *Search) take(lv level, n int32) {
	if n == 0 {
		return
	}
	a := &s.classes[lv.class].alts[lv.alt]
	s.counts[lv.class][lv.alt] += n
	s.used[lv.class] += n
	for i := range s.produced {
		s.produced[i] += n * a.Mana[i]
	}
	s.sources += int(n)
	if a.Creature {
		s.creatures += int(n)
	}
	s.flex += int(n) * a.Flex
	s.irr += int64(n) * s.classes[lv.class].irr[lv.alt]
	s.pain += int64(n) * s.classes[lv.class].pain[lv.alt]
}

// promising reports whether some completion from level can still pay and
// could still rank at least as well as the best complete plan so far.
func (s *Search) promising(level int, short state.Mana, total int32) bool {
	lv := s.levels[level]
	here := s.levelStats[level]
	avail := int32(len(s.classes[lv.class].Members)) - s.used[lv.class]
	if avail == 0 {
		here = boundStats{}
	}
	rest := here.merge(s.laterStats[level])
	if !rest.any {
		return false
	}
	shortMask := 0
	for i, n := range short {
		if n > 0 {
			shortMask |= 1 << i
		}
	}
	capOf := func(mask int) int32 { return avail*here.most[mask] + s.laterCap[level][mask] }
	// Feasibility, and the fewest further units any completion needs.
	if capOf(63) < total || rest.most[63] == 0 {
		return false
	}
	need := max(int32(1), ceilDiv(total, rest.most[63]))
	for sub := shortMask; sub > 0; sub = (sub - 1) & shortMask {
		var want int32
		for i := range short {
			if sub&(1<<i) != 0 {
				want += short[i]
			}
		}
		if capOf(sub) < want || rest.most[sub] == 0 {
			return false
		}
		need = max(need, ceilDiv(want, rest.most[sub]))
	}
	if s.sources+int(need) > decision.MaxPaymentActivations {
		return false
	}
	if s.Best == nil {
		return true
	}
	surplus := s.pool.Total() + s.produced.Total() + need*rest.least - s.cost.Generic - s.cost.Colored.Total()
	lb := Rank{
		Cost:      s.irr + int64(need)*rest.irr,
		Creatures: s.creatures + int(need)*rest.creature,
		Sources:   s.sources + int(need),
		Surplus:   max(surplus, 0),
		Flex:      s.flex + int(need)*rest.flex,
	}
	// Keys 6 and 7 (hand-reserve coverage and remainder diversity, both
	// MORE first) are read from the current counts: every completion only
	// consumes further sources, so neither can rise, and the current values
	// bound every completion's from above.
	lb.HandReserve, lb.Remainder = s.reserve()
	return compareRankKeys(lb, s.BestRank) <= 0
}

// reserve is rank keys 6 and 7 for the current counts, exactly as
// RankPlan computes them from a witness: remaining[c] is the census
// of normal sources able to make colour c minus the chosen units able to
// (a class shares its colours, so the counts decide it); key 7 counts the
// colours still made, key 6 packs min(remaining[c], handDemand[c]) in the
// demand order.
//
// Why the current value bounds every completion from above: adding units
// only decrements remaining[c], so each coverage digit
// min(remaining[c], handDemand[c]) is non-increasing. Every digit lies in
// [0, handDemand[c]] and reserveBase is max demand + 1, so the packing is a
// positional base-reserveBase number whose order is the lexicographic order
// of its digits; digitwise non-increase therefore makes the packed key
// non-increasing, and so is the count of colours with remaining[c] > 0.
// Both keys prefer MORE, so a completion can never beat the current value
// on either, and it is a valid optimistic bound for pruning.
func (s *Search) reserve() (handReserve, remainder int) {
	remaining := s.ctx.ColourSources
	for k := range s.classes {
		if s.used[k] == 0 {
			continue
		}
		colours := s.classes[k].alts[0].Colours
		for c := range remaining {
			if colours&(1<<c) != 0 {
				remaining[c] -= int(s.used[k])
			}
		}
	}
	for _, left := range remaining {
		if left > 0 {
			remainder++
		}
	}
	if s.ctx.ReserveBase > 0 {
		for _, c := range s.ctx.DemandOrder {
			handReserve = handReserve*s.ctx.ReserveBase + min(remaining[c], s.ctx.HandDemand[c])
		}
	}
	return handReserve, remainder
}

// compareRankPrefix compares keys 1-5, the monotone prefix.
func compareRankPrefix(a, b Rank) int {
	return cmp.Or(cmp.Compare(a.Cost, b.Cost), cmp.Compare(a.Creatures, b.Creatures), cmp.Compare(a.Sources, b.Sources),
		cmp.Compare(a.Surplus, b.Surplus), cmp.Compare(a.Flex, b.Flex))
}

// countRank is keys 1-7 of the current complete count vector, read from the
// counts alone (every unit of a class shares them).
func (s *Search) countRank() Rank {
	r := Rank{Cost: s.irr, Creatures: s.creatures, Sources: s.sources, Flex: s.flex,
		Surplus: s.pool.Total() + s.produced.Total() - s.cost.Generic - s.cost.Colored.Total()}
	r.HandReserve, r.Remainder = s.reserve()
	return r
}

// compareRankKeys compares keys 1-7 in Rank.less's
// directions (keys 6 and 7 MORE first).
func compareRankKeys(a, b Rank) int {
	return cmp.Or(compareRankPrefix(a, b), cmp.Compare(b.HandReserve, a.HandReserve), cmp.Compare(b.Remainder, a.Remainder))
}

// complete records the current count vector as a candidate when it can beat
// the best plan. It reports false only if the ordinary solver refuses to
// settle a count vector the deficit arithmetic calls paid (never expected);
// the walk then keeps extending it.
func (s *Search) complete() bool {
	quick := s.countRank()
	if !s.env.Verify && s.Best != nil && compareRankKeys(quick, s.BestRank) > 0 {
		return true
	}
	chosen := s.materialize()
	after, ok := s.env.Settle(s.cost, ManaAdd(s.pool, s.produced), s.life)
	if !ok {
		return false
	}
	plan := Witness(s.cost, s.pool, s.produced, chosen, after)
	r := RankPlan(s.ctx, plan, chosen, after)
	if s.env.Verify && compareRankKeys(quick, r) != 0 {
		panic(fmt.Sprintf("payment plan search: count rank %+v differs from the witness rank %+v", quick, r))
	}
	if s.Best == nil || r.Less(s.BestRank) {
		s.Best, s.BestRank = &plan, r
	}
	return true
}

// materialize turns the current count vector into its key-8-least witness:
// steps in unit order, each the smallest (source ID first) step that still
// leaves every class enough later units for what it owes. The step compare
// leads with the source ID and every unit's ID is distinct, so the greedy
// choice at each position is forced and the result is the least witness of
// this count vector.
func (s *Search) materialize() []Alt {
	sc := s.sc
	out := slices.Grow(sc.chosen[:0], s.sources)
	owed := append(sc.owed[:0], s.used...)
	flat := sc.owedAltFlat[:0]
	for k := range s.counts {
		flat = append(flat, s.counts[k]...)
	}
	owedAlt := resizeCleared(sc.owedAlt, len(s.counts))
	at := 0
	for k := range s.counts {
		n := len(s.counts[k])
		owedAlt[k] = flat[at : at+n : at+n]
		at += n
	}
	next := resizeCleared(sc.next, len(s.classes)) // first member after the last step

	// limit(k) is the latest unit position class k's next step may take and
	// still leave owed[k]-1 later members; another class's step must come
	// strictly before it.
	limit := func(k int) int {
		m := s.classes[k].Members
		return m[len(m)-int(owed[k])]
	}
	const none = int(^uint(0) >> 1)
	for len(out) < s.sources {
		// The two smallest limits give "min over the other classes" per class.
		low1, low2, lowK := none, none, -1
		for k := range s.classes {
			if owed[k] == 0 {
				continue
			}
			if l := limit(k); l < low1 {
				low1, low2, lowK = l, low1, k
			} else if l < low2 {
				low2 = l
			}
		}
		bestK, bestPos := -1, -1
		var bestID state.ObjID
		for k := range s.classes {
			if owed[k] == 0 {
				continue
			}
			upper := limit(k)
			other := low1
			if k == lowK {
				other = low2
			}
			if other != none {
				upper = min(upper, other-1)
			}
			m := s.classes[k].Members
			for j := next[k]; j < len(m) && m[j] <= upper; j++ {
				id := s.choices[m[j]][0].Activation.Source
				if bestK < 0 || id < bestID {
					bestK, bestPos, bestID = k, m[j], id
				}
			}
		}
		c := &s.classes[bestK]
		alt := -1
		for _, a := range c.pick {
			if owedAlt[bestK][a] > 0 {
				alt = a
				break
			}
		}
		out = append(out, s.choices[bestPos][alt])
		owed[bestK]--
		owedAlt[bestK][alt]--
		for k := range s.classes {
			m := s.classes[k].Members
			for next[k] < len(m) && m[next[k]] <= bestPos {
				next[k]++
			}
		}
	}
	// The arrays are kept for the next witness; out is read (and copied) by
	// the caller before then.
	sc.chosen, sc.owed, sc.owedAltFlat, sc.owedAlt, sc.next = out[:0], owed[:0], flat[:0], owedAlt[:0], next[:0]
	return out
}

// resizeCleared returns s resized to n elements, all zero, reusing its
// storage when it is big enough.
func resizeCleared[T any](s []T, n int) []T {
	if cap(s) >= n {
		s = s[:n]
		clear(s)
		return s
	}
	return make([]T, n)
}

// ManaAdd returns a+b elementwise.
func ManaAdd(a, b state.Mana) state.Mana {
	var m state.Mana
	for i := range m {
		m[i] = a[i] + b[i]
	}
	return m
}
