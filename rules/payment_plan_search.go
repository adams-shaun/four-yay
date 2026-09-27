package rules

// The payment planner's search (spec 5, amended 2026-09-26): a rank-aware,
// bounded, deterministic search over CLASSES of interchangeable source units.
// Like the rest of the planner it is a pure read: no event, no state write,
// no RNG.

import (
	"cmp"
	"fmt"
	"math/bits"
	"reflect"
	"slices"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// paymentPlanClass is one equivalence class of source units: units whose
// alternative lists are identical in everything the rank and the witness read
// except the source itself (ability identity, produced vector, tier,
// consequence, creature flag, flexibility and colour census, in the same
// alternative order). Thirty Mountains are one class; the search chooses how
// MANY of a class to use for each alternative, never which ones, and the
// witness is materialised afterwards in the canonical key-8 order.
type paymentPlanClass struct {
	members []int // unit indices (positions in the query's unit list), ascending
	alts    []plannedManaActivation
	// pick is the class's alternative indices in witness-step order (the
	// key-8 compare with the source left out), so materialisation hands each
	// chosen unit the smallest alternative still owed.
	pick []int
	// irr is each alternative's irreversible cost (rank key 1).
	irr []int64
}

// paymentPlanSameClass reports whether two units' alternative lists are
// interchangeable for the search: equal, alternative by alternative, in
// every field except the source's own identity (Source, SourceZoneSeq) and
// the concrete ability pointers execution uses.
func paymentPlanSameClass(a, b []plannedManaActivation) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for i := range a {
		x, y := &a[i], &b[i]
		if x.activation.Ability != y.activation.Ability || x.activation.Produces != y.activation.Produces ||
			x.mana != y.mana || x.creature != y.creature || x.flex != y.flex || x.colours != y.colours ||
			x.tier != y.tier || x.consequence != y.consequence {
			return false
		}
	}
	return true
}

// paymentPlanPhaseChoices keeps, per unit, the alternatives one search phase
// may use (spec 5): phase 1 passes paymentTierNormal and sees normal sources
// only; phase 2 (wired by aph-last-resort-plans) passes paymentTierLastResort
// and sees normal and last-resort alternatives together. Unit positions are
// preserved, so a unit with nothing left is simply absent from every class.
func paymentPlanPhaseChoices(choices [][]plannedManaActivation, minTier paymentAbilityTier) [][]plannedManaActivation {
	out := make([][]plannedManaActivation, len(choices))
	for i, alts := range choices {
		if !slices.ContainsFunc(alts, func(a plannedManaActivation) bool { return a.tier < minTier }) {
			out[i] = alts
			continue
		}
		for _, a := range alts {
			if a.tier >= minTier {
				out[i] = append(out[i], a)
			}
		}
	}
	return out
}

// paymentPlanClasses groups units into classes, in order of each class's
// first member. Grouping is a linear scan with an explicit equality, so the
// result never depends on map iteration.
func paymentPlanClasses(choices [][]plannedManaActivation) []paymentPlanClass {
	var out []paymentPlanClass
	for i, alts := range choices {
		if len(alts) == 0 {
			continue
		}
		k := slices.IndexFunc(out, func(c paymentPlanClass) bool { return paymentPlanSameClass(c.alts, alts) })
		if k < 0 {
			out = append(out, paymentPlanClass{alts: alts})
			k = len(out) - 1
		}
		out[k].members = append(out[k].members, i)
	}
	for k := range out {
		c := &out[k]
		c.pick = make([]int, len(c.alts))
		c.irr = make([]int64, len(c.alts))
		for a := range c.alts {
			c.pick[a] = a
			c.irr[a] = paymentPlanConsequenceCost(c.alts[a].consequence, c.alts[a].creature)
		}
		slices.SortStableFunc(c.pick, func(x, y int) int {
			sx := paymentPlanRankStep{act: c.alts[x].activation, consequence: c.alts[x].consequence}
			sy := paymentPlanRankStep{act: c.alts[y].activation, consequence: c.alts[y].consequence}
			sx.act.Source, sy.act.Source = 0, 0
			return comparePaymentPlanSteps([]paymentPlanRankStep{sx}, []paymentPlanRankStep{sy})
		})
	}
	return out
}

// paymentPlanLevel is one search level: how many units of one class take one
// alternative.
type paymentPlanLevel struct {
	class, alt int
}

// paymentPlanBoundStats summarise what the units still selectable at or
// after a level can contribute, per unit: the most mana of each colour
// subset (bit i = mana index i, so mask 63 is the total), and the least
// amount, irreversible cost, creature flag and flexibility. They drive the
// feasibility check and the branch-and-bound lower bound.
type paymentPlanBoundStats struct {
	most     [64]int32
	least    int32
	irr      int64
	creature int
	flex     int
	any      bool
}

func (b *paymentPlanBoundStats) add(a *plannedManaActivation, irr int64) {
	var sub [64]int32
	for mask := 1; mask < 64; mask++ {
		low := bits.TrailingZeros(uint(mask))
		sub[mask] = sub[mask&(mask-1)] + a.mana[low]
		b.most[mask] = max(b.most[mask], sub[mask])
	}
	creature := 0
	if a.creature {
		creature = 1
	}
	if !b.any {
		b.least, b.irr, b.creature, b.flex, b.any = sub[63], irr, creature, a.flex, true
		return
	}
	b.least = min(b.least, sub[63])
	b.irr = min(b.irr, irr)
	b.creature = min(b.creature, creature)
	b.flex = min(b.flex, a.flex)
}

// merge folds o into b as the union of two unit populations, weighting the
// per-unit maxima into capacities is the caller's job.
func (b paymentPlanBoundStats) merge(o paymentPlanBoundStats) paymentPlanBoundStats {
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

// paymentPlanSearch is one phase's search state.
type paymentPlanSearch struct {
	cost    Cost
	pool    state.Mana
	life    int32
	ctx     paymentPlanRankContext
	choices [][]plannedManaActivation
	classes []paymentPlanClass
	levels  []paymentPlanLevel
	// levelStats[l] covers the current class's alternatives from level l to
	// the class's last level; laterCap[l] / laterStats[l] cover every class
	// after it (whose units are all still unused when level l is reached).
	levelStats []paymentPlanBoundStats
	laterStats []paymentPlanBoundStats
	laterCap   [][64]int32
	counts     [][]int32 // [class][alternative] units chosen
	used       []int32   // [class] units chosen
	produced   state.Mana
	sources    int
	creatures  int
	flex       int
	irr        int64

	nodes    int
	limited  bool
	best     *decision.PaymentPlan
	bestRank paymentPlanRank
}

// searchPaymentPlan runs one phase over choices (already filtered to the
// phase's tiers) and their classes (paymentPlanClasses(choices), which it
// only reads). ctx is the query's rank context. The result is the
// rank-best complete plan under paymentPlanRank.less -- the same plan an
// exhaustive walk over every subset and alternative would return -- unless
// the node budget runs out first, when it is the best complete plan found
// (Reason "search_limit"), or none.
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
//     flexibility; keys 1-5), then key 6 (the hand-reserve placeholder,
//     always 0 -- a real key 6 needs its own bound before key 7 may prune,
//     and the verify-mode rank check in complete trips if it lands
//     without one), then key 7's upper bound (remainder diversity only
//     falls as units are added).
//
// Branches that tie the bound are explored, and each complete count vector
// is materialised as its key-8-least witness, so ties on keys 1-7 resolve
// exactly as the exhaustive order does. The node budget is the only source
// of inexactness: a search that reaches it returns the best plan found.
func searchPaymentPlan(cost Cost, pool state.Mana, life int32, ctx paymentPlanRankContext, choices [][]plannedManaActivation, classes []paymentPlanClass) *paymentPlanSearch {
	s := &paymentPlanSearch{cost: cost, pool: pool, life: life, ctx: ctx, choices: choices, classes: classes}
	s.order()
	s.counts = make([][]int32, len(s.classes))
	for k := range s.classes {
		s.counts[k] = make([]int32, len(s.classes[k].alts))
	}
	s.used = make([]int32, len(s.classes))
	s.walk(0)
	return s
}

// order fixes the level order: pips first, cheapest first. Classes whose
// alternatives can pay a colour the pool leaves short go ahead of generic-
// only classes of the same irreversible cost and creature flag; within that,
// the least flexible class first, then battlefield order. Inside a class the
// alternatives producing a short colour come first. The order only steers
// how early a good plan is found; exactness never depends on it.
func (s *paymentPlanSearch) order() {
	short, _ := s.deficit()
	shortMask := 0
	for i, n := range short {
		if n > 0 {
			shortMask |= 1 << i
		}
	}
	pips := func(a *plannedManaActivation) bool {
		for i, n := range a.mana {
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
		key := classKey{irr: slices.Min(c.irr), creature: c.alts[0].creature, flex: c.alts[0].flex, first: c.members[0]}
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
	classes := make([]paymentPlanClass, len(idx))
	for k, from := range idx {
		classes[k] = s.classes[from]
	}
	s.classes = classes

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
			s.levels = append(s.levels, paymentPlanLevel{class: k, alt: a})
		}
	}
	// Suffix statistics. levelStats runs backwards inside each class;
	// laterStats/laterCap accumulate whole classes from the end.
	s.levelStats = make([]paymentPlanBoundStats, len(s.levels))
	s.laterStats = make([]paymentPlanBoundStats, len(s.levels))
	s.laterCap = make([][64]int32, len(s.levels))
	var later paymentPlanBoundStats
	var laterCap [64]int32
	for k := len(s.classes) - 1; k >= 0; k-- {
		c := &s.classes[k]
		end := len(s.levels)
		if k+1 < len(classStart) {
			end = classStart[k+1]
		}
		var inClass paymentPlanBoundStats
		for l := end - 1; l >= classStart[k]; l-- {
			inClass.add(&c.alts[s.levels[l].alt], c.irr[s.levels[l].alt])
			s.levelStats[l] = inClass
			s.laterStats[l] = later
			s.laterCap[l] = laterCap
		}
		n := int32(len(c.members))
		for m := range laterCap {
			laterCap[m] += n * inClass.most[m]
		}
		later = later.merge(inClass)
	}
}

// deficit is what the pool and the produced mana still leave unpaid: each
// coloured (and true colourless) requirement, and the total.
func (s *paymentPlanSearch) deficit() (state.Mana, int32) {
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

func (s *paymentPlanSearch) walk(level int) {
	if s.nodes >= decision.MaxPaymentPlanSearchNodes {
		s.limited = true
		return
	}
	s.nodes++
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
	if amt := a.mana.Total(); amt > 0 {
		useful = ceilDiv(total, amt)
	}
	for i, n := range a.mana {
		if n > 0 && short[i] > 0 {
			useful = max(useful, ceilDiv(short[i], n))
		}
	}
	hi := min(int32(len(c.members))-s.used[lv.class], int32(decision.MaxPaymentActivations-s.sources), useful)
	for n := hi; n >= 0; n-- {
		s.take(lv, n)
		s.walk(level + 1)
		s.take(lv, -n)
		if s.limited {
			return
		}
	}
}

// take adds (n > 0) or removes (n < 0) n units of level lv's alternative.
func (s *paymentPlanSearch) take(lv paymentPlanLevel, n int32) {
	if n == 0 {
		return
	}
	a := &s.classes[lv.class].alts[lv.alt]
	s.counts[lv.class][lv.alt] += n
	s.used[lv.class] += n
	for i := range s.produced {
		s.produced[i] += n * a.mana[i]
	}
	s.sources += int(n)
	if a.creature {
		s.creatures += int(n)
	}
	s.flex += int(n) * a.flex
	s.irr += int64(n) * s.classes[lv.class].irr[lv.alt]
}

// promising reports whether some completion from level can still pay and
// could still rank at least as well as the best complete plan so far.
func (s *paymentPlanSearch) promising(level int, short state.Mana, total int32) bool {
	lv := s.levels[level]
	here := s.levelStats[level]
	avail := int32(len(s.classes[lv.class].members)) - s.used[lv.class]
	if avail == 0 {
		here = paymentPlanBoundStats{}
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
	if s.best == nil {
		return true
	}
	surplus := s.pool.Total() + s.produced.Total() + need*rest.least - s.cost.Generic - s.cost.Colored.Total()
	lb := paymentPlanRank{
		cost:      s.irr + int64(need)*rest.irr,
		creatures: s.creatures + int(need)*rest.creature,
		sources:   s.sources + int(need),
		surplus:   max(surplus, 0),
		flex:      s.flex + int(need)*rest.flex,
		// Key 7 only falls as units are added, so the current remainder
		// bounds every completion's from above (more is better).
		remainder: s.remainder(),
	}
	return comparePaymentPlanRankKeys(lb, s.bestRank) <= 0
}

// remainder is rank key 7 for the current counts: the colours some unused
// source can still produce.
func (s *paymentPlanSearch) remainder() int {
	remaining := s.ctx.colourSources
	for k := range s.classes {
		if s.used[k] == 0 {
			continue
		}
		colours := s.classes[k].alts[0].colours
		for c := range remaining {
			if colours&(1<<c) != 0 {
				remaining[c] -= int(s.used[k])
			}
		}
	}
	n := 0
	for _, left := range remaining {
		if left > 0 {
			n++
		}
	}
	return n
}

// comparePaymentPlanRankPrefix compares keys 1-5, the monotone prefix.
func comparePaymentPlanRankPrefix(a, b paymentPlanRank) int {
	return cmp.Or(cmp.Compare(a.cost, b.cost), cmp.Compare(a.creatures, b.creatures), cmp.Compare(a.sources, b.sources),
		cmp.Compare(a.surplus, b.surplus), cmp.Compare(a.flex, b.flex))
}

// countRank is keys 1-7 of the current complete count vector, read from the
// counts alone (every unit of a class shares them).
func (s *paymentPlanSearch) countRank() paymentPlanRank {
	return paymentPlanRank{cost: s.irr, creatures: s.creatures, sources: s.sources, flex: s.flex,
		surplus:   s.pool.Total() + s.produced.Total() - s.cost.Generic - s.cost.Colored.Total(),
		remainder: s.remainder()}
}

// comparePaymentPlanRankKeys compares keys 1-7.
func comparePaymentPlanRankKeys(a, b paymentPlanRank) int {
	return cmp.Or(comparePaymentPlanRankPrefix(a, b), cmp.Compare(a.handReserve, b.handReserve), cmp.Compare(b.remainder, a.remainder))
}

// complete records the current count vector as a candidate when it can beat
// the best plan. It reports false only if the ordinary solver refuses to
// settle a count vector the deficit arithmetic calls paid (never expected);
// the walk then keeps extending it.
func (s *paymentPlanSearch) complete() bool {
	quick := s.countRank()
	if !walkCacheVerify && s.best != nil && comparePaymentPlanRankKeys(quick, s.bestRank) > 0 {
		return true
	}
	chosen := s.materialize()
	paid, ok := s.cost.resolveManaWith(manaAdd(s.pool, s.produced), state.Mana{}, [7]state.Mana{}, s.life, false, pipRider{}, nil)
	if !ok {
		return false
	}
	plan := paymentWitness(s.cost, s.pool, s.produced, chosen, paid.pool)
	r := rankPaymentPlan(s.ctx, plan, chosen, paid.pool)
	if walkCacheVerify && comparePaymentPlanRankKeys(quick, r) != 0 {
		panic(fmt.Sprintf("payment plan search: count rank %+v differs from the witness rank %+v", quick, r))
	}
	if s.best == nil || r.less(s.bestRank) {
		s.best, s.bestRank = &plan, r
	}
	return true
}

// materialize turns the current count vector into its key-8-least witness:
// steps in unit order, each the smallest (source ID first) step that still
// leaves every class enough later units for what it owes. The step compare
// leads with the source ID and every unit's ID is distinct, so the greedy
// choice at each position is forced and the result is the least witness of
// this count vector.
func (s *paymentPlanSearch) materialize() []plannedManaActivation {
	out := make([]plannedManaActivation, 0, s.sources)
	owed := slices.Clone(s.used)
	owedAlt := make([][]int32, len(s.counts))
	for k := range s.counts {
		owedAlt[k] = slices.Clone(s.counts[k])
	}
	next := make([]int, len(s.classes)) // first member after the last step
	// limit(k) is the latest unit position class k's next step may take and
	// still leave owed[k]-1 later members; another class's step must come
	// strictly before it.
	limit := func(k int) int {
		m := s.classes[k].members
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
			m := s.classes[k].members
			for j := next[k]; j < len(m) && m[j] <= upper; j++ {
				id := s.choices[m[j]][0].activation.Source
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
			m := s.classes[k].members
			for next[k] < len(m) && m[next[k]] <= bestPos {
				next[k]++
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Zone-entry index (spec 5 amended: source_zone_seq from one index per query).

// paymentZoneSeqIndex answers paymentSourceZoneSeq for every object that was
// on the battlefield when it was built, from ONE backward pass over the log
// instead of one pass per alternative. Each object's answer is found with the
// scan's own predicate (the latest MoveZone into the object's current zone,
// or on the battlefield a TokenCreate naming it), so it is exactly the scan's
// answer; an object outside the indexed set, or any read after the log or
// the object's zone changed, falls back to the scan. The pass runs lazily, on
// the query's first read, so a query that reads no source never pays for it.
type paymentZoneSeqIndex struct {
	log   []events.Event
	built bool
	ans   []paymentZoneSeqAnswer // indexed by ObjID
}

type paymentZoneSeqAnswer struct {
	zone    state.Zone
	indexed bool
	seq     uint64
}

// newPaymentZoneSeqIndex returns an index over e's current log; its pass
// runs on the first lookup.
func (e *Engine) newPaymentZoneSeqIndex() *paymentZoneSeqIndex {
	return &paymentZoneSeqIndex{log: e.L.Events}
}

// buildPaymentZoneSeqIndex returns an index with its pass already run.
func (e *Engine) buildPaymentZoneSeqIndex() *paymentZoneSeqIndex {
	x := e.newPaymentZoneSeqIndex()
	x.build(e)
	return x
}

func (x *paymentZoneSeqIndex) build(e *Engine) {
	x.built = true
	n := len(e.G.Objs)
	x.ans = make([]paymentZoneSeqAnswer, n+1)
	resolved := make([]bool, n+1)
	pending := 0
	for _, pl := range e.G.Players {
		for _, id := range e.G.Zone(state.ZBattlefield, pl.ID) {
			if o := e.G.Obj(id); o != nil && !x.ans[id].indexed {
				x.ans[id] = paymentZoneSeqAnswer{zone: o.Zone, indexed: true, seq: decision.GenesisZoneSeq}
				pending++
			}
		}
	}
	for i := len(x.log) - 1; i >= 0 && pending > 0; i-- {
		ev := &x.log[i]
		if ev.Obj == 0 || int(ev.Obj) > n || resolved[ev.Obj] || !x.ans[ev.Obj].indexed {
			continue
		}
		a := &x.ans[ev.Obj]
		if ev.Kind == events.MoveZone && ev.To == a.zone || ev.Kind == events.TokenCreate && a.zone == state.ZBattlefield {
			a.seq = ev.Seq
			resolved[ev.Obj] = true
			pending--
		}
	}
}

// valid reports whether the index still describes e's log.
func (x *paymentZoneSeqIndex) valid(e *Engine) bool {
	if x == nil {
		return false
	}
	n := len(e.L.Events)
	return n == len(x.log) && (n == 0 || &e.L.Events[0] == &x.log[0])
}

func (x *paymentZoneSeqIndex) lookup(e *Engine, id state.ObjID) uint64 {
	o := e.G.Obj(id)
	if o == nil {
		return decision.GenesisZoneSeq
	}
	if x.valid(e) {
		if !x.built {
			x.build(e)
		}
		if int(id) < len(x.ans) {
			if a := x.ans[id]; a.indexed && a.zone == o.Zone {
				return a.seq
			}
		}
	}
	return e.paymentSourceZoneSeqScan(id)
}

// paymentPlanQuery is the per-query scratch of one planner query, or of one
// PaymentActionsForPriority build around every candidate's query: the
// zone-entry index, each player's source census (paymentPlanManaUnits) and
// each source's alternatives (paymentPlanUnitAlternatives), which depend on
// the board and the payer but never on the cast. It is
// installed for the query's duration and every read checks it still
// describes the engine's log; Clone copies none of it.
type paymentPlanQuery struct {
	zoneSeqs *paymentZoneSeqIndex
	units    map[state.PlayerID][]windowManaUnit
	alts     map[state.ObjID][]plannedManaActivation
	classes  map[paymentPlanClassesKey][]paymentPlanClass
}

type paymentPlanClassesKey struct {
	payer   state.PlayerID
	minTier paymentAbilityTier
}

func (q *paymentPlanQuery) valid(e *Engine) bool {
	return q != nil && q.zoneSeqs.valid(e)
}

// paymentPlanQueryScope installs a query scope and returns the function that
// removes it. A still-valid enclosing scope is reused.
func (e *Engine) paymentPlanQueryScope() func() {
	if e.paymentPlanQuery.valid(e) {
		return func() {}
	}
	prev := e.paymentPlanQuery
	e.paymentPlanQuery = &paymentPlanQuery{zoneSeqs: e.newPaymentZoneSeqIndex()}
	return func() { e.paymentPlanQuery = prev }
}

// paymentPlanQueryUnits is paymentPlanManaUnits, computed once per query
// scope and player. The returned units are shared: callers only read them.
func (e *Engine) paymentPlanQueryUnits(p state.PlayerID) []windowManaUnit {
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return e.paymentPlanManaUnits(p)
	}
	if units, ok := q.units[p]; ok {
		if walkCacheVerify && !paymentPlanSameUnits(units, e.paymentPlanManaUnits(p)) {
			panic(fmt.Sprintf("payment plan query: cached source census for player %d is stale", p))
		}
		return units
	}
	units := e.paymentPlanManaUnits(p)
	if q.units == nil {
		q.units = map[state.PlayerID][]windowManaUnit{}
	}
	q.units[p] = units
	return units
}

// paymentPlanQueryAlternatives is paymentPlanUnitAlternatives, computed once
// per query scope and source. The returned alternatives are shared: callers
// only read them.
func (e *Engine) paymentPlanQueryAlternatives(u windowManaUnit) []plannedManaActivation {
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return e.paymentPlanUnitAlternatives(u)
	}
	if alts, ok := q.alts[u.id]; ok {
		if walkCacheVerify && !slices.EqualFunc(alts, e.paymentPlanUnitAlternatives(u), paymentPlanSameAlternative) {
			panic(fmt.Sprintf("payment plan query: cached alternatives for source %d are stale", u.id))
		}
		return alts
	}
	alts := e.paymentPlanUnitAlternatives(u)
	if q.alts == nil {
		q.alts = map[state.ObjID][]plannedManaActivation{}
	}
	q.alts[u.id] = alts
	return alts
}

// paymentPlanQueryClasses is paymentPlanClasses over one phase's choices,
// computed once per query scope, payer and phase: within a scope the
// choices are the cached census's, so every candidate cast groups them the
// same way. The classes are shared: the search only reads them.
func (e *Engine) paymentPlanQueryClasses(p state.PlayerID, minTier paymentAbilityTier, choices [][]plannedManaActivation) []paymentPlanClass {
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return paymentPlanClasses(choices)
	}
	key := paymentPlanClassesKey{payer: p, minTier: minTier}
	if classes, ok := q.classes[key]; ok {
		if walkCacheVerify && !reflect.DeepEqual(paymentPlanClassMembers(classes), paymentPlanClassMembers(paymentPlanClasses(choices))) {
			panic(fmt.Sprintf("payment plan query: cached classes for player %d are stale", p))
		}
		return classes
	}
	classes := paymentPlanClasses(choices)
	if q.classes == nil {
		q.classes = map[paymentPlanClassesKey][]paymentPlanClass{}
	}
	q.classes[key] = classes
	return classes
}

// paymentPlanClassMembers is each class's member list (the verify-mode view
// of a class grouping).
func paymentPlanClassMembers(classes []paymentPlanClass) [][]int {
	out := make([][]int, len(classes))
	for k := range classes {
		out[k] = classes[k].members
	}
	return out
}

// paymentPlanSameAlternative compares two computations of one alternative.
// A choice-shaped exec is a fresh withProduced copy per computation, derived
// from ma and the recorded production (both compared), so exec itself is
// compared only for presence.
func paymentPlanSameAlternative(a, b plannedManaActivation) bool {
	if (a.exec == nil) != (b.exec == nil) {
		return false
	}
	a.exec, b.exec = nil, nil
	return a == b
}

// paymentPlanSameUnits compares two source censuses unit by unit and
// alternative by alternative (the verify-mode check of the query cache).
func paymentPlanSameUnits(a, b []windowManaUnit) bool {
	return slices.EqualFunc(a, b, func(x, y windowManaUnit) bool {
		return x.id == y.id && x.freeCount == y.freeCount && slices.Equal(x.alts, y.alts)
	})
}
