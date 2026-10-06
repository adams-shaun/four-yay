package decision

// The map-based Validate, validateRest and groupCapExceeded exactly as they
// stood before the map-free rewrite (perf: Validate runs per candidate answer
// in search). validateRef is the reference the equivalence tests hold the
// live Validate to, error text included.

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// ValidateRef exposes the reference to the external real-decision test.
func ValidateRef(d *Decision, in Intent) error { return d.validateRef(in) }

// GroupCapExceededRef and GroupCapExceeded expose the pair to that test.
func GroupCapExceededRef(d *Decision, choices []int) bool { return d.groupCapExceededRef(choices) }
func GroupCapExceeded(d *Decision, choices []int) bool    { return d.groupCapExceeded(choices) }

func (d *Decision) validateRef(in Intent) error {
	if in.Seq != d.Seq {
		return fmt.Errorf("intent seq %d, pending decision seq %d", in.Seq, d.Seq)
	}
	if in.Player != d.Player {
		return fmt.Errorf("intent from player %d, decision is for player %d", in.Player, d.Player)
	}
	if in.Announce != nil {
		return d.validateAnnounce(in)
	}
	if in.Payment != nil {
		return d.validatePayment(in)
	}
	if len(in.Choices) < d.Min || len(in.Choices) > d.Max {
		return fmt.Errorf("expected %d..%d choices, got %d", d.Min, d.Max, len(in.Choices))
	}
	seen := make(map[int]bool, len(in.Choices))
	seenGroups := make(map[string]int, len(in.Choices))
	groupCount := make(map[string]int, len(in.Choices))
	var controller state.PlayerID
	haveController := false
	if d.TargetsWithSameController {
		for _, c := range in.Choices {
			if c < 0 || c >= len(d.Options) {
				continue
			}
			got := d.Options[c].Controller
			if !haveController {
				controller, haveController = got, true
			} else if got != controller {
				return fmt.Errorf("choices do not share one controller")
			}
		}
	}
	// The target-set property constraint (Decision.SetPropMode): the same
	// incremental rule (SetPropAdmits/SetPropMerge) botpolicy's repair uses,
	// so a repair can never return an answer Validate rejects. The accumulator
	// is the running intersection (shared) or union (distinct) of the chosen
	// options' SetProps.
	if d.SetPropMode != SetPropNone {
		var acc []string
		for _, c := range in.Choices {
			if c < 0 || c >= len(d.Options) {
				continue
			}
			add := d.Options[c].SetProps
			if !SetPropAdmits(d.SetPropMode, acc, add) {
				if d.SetPropMode == SetPropShared {
					return fmt.Errorf("choices do not all share a required property")
				}
				return fmt.Errorf("choices share a property the set requires to differ")
			}
			acc = SetPropMerge(d.SetPropMode, acc, add)
		}
	}
	for choicePos, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return fmt.Errorf("choice %d out of range (%d options)", c, len(d.Options))
		}
		if seen[c] && !d.Repeatable {
			return fmt.Errorf("duplicate choice %d", c)
		}
		seen[c] = true
		// The exclusivity rule: two options sharing one non-empty Group are
		// mutually exclusive, so an intent must not select both. This is a
		// general wire contract, not a combat rule -- the group field says
		// nothing about what its members are, only that they are exclusive.
		if g := d.Options[c].Group; g != "" {
			// The per-Group cap: at most GroupCapFor(g) options of one Group may
			// be selected together. At the default cap of 1 this is the historical
			// mutual-exclusion rule with its historical message; a raised cap
			// (EACH's per-type ChangeNum) reports the count it refused. The
			// incremental admission test is decision.GroupAdmits, the same rule
			// orderTargetOptions' prefix walks apply, so the offered prefix and
			// this fence cannot drift.
			if !d.GroupAdmits(groupCount, g) {
				limit := d.GroupCapFor(g)
				if limit == 1 {
					return fmt.Errorf("choices %d and %d are mutually exclusive (group %q)", seenGroups[g], c, g)
				}
				return fmt.Errorf("choice %d exceeds the per-group limit of %d (group %q)", c, limit, g)
			}
			if groupCount[g] == 0 {
				seenGroups[g] = c
			}
			groupCount[g]++
		}
		if d.Kind == KBlockers && !d.BlockPairAdmits(in.Choices[:choicePos], c) {
			return fmt.Errorf("blocker %d cannot block multiple attackers outside BlockAllDefined pairs", d.Options[c].Obj)
		}
	}
	// The cumulative-budget rule (Decision.MaxSum): the chosen options'
	// Value fields sum to at most MaxSum. This is a general wire contract --
	// the field says nothing about cards or mana values, only that the picked
	// set's total price is capped -- so a client can enforce it without
	// learning any rules.
	if d.HasBudget() {
		sum := 0
		for _, c := range in.Choices {
			sum += d.Options[c].Value
		}
		if sum > d.MaxSum {
			return fmt.Errorf("choices total %d exceeds the budget %d", sum, d.MaxSum)
		}
	}
	if d.HasBudget2() {
		sum := 0
		for _, c := range in.Choices {
			sum += d.Options[c].Value2
		}
		if sum > d.MaxSum2 {
			return fmt.Errorf("choices total %d exceeds the second budget %d", sum, d.MaxSum2)
		}
	}
	// The cumulative-floor rule (Decision.MinSum): the chosen options'
	// Value fields sum to at least MinSum. The mirror of the budget above,
	// and the same general wire contract: the field says nothing about
	// creatures or power, only that the picked set's total must reach a
	// floor -- so a rules-ignorant client can enforce it without learning
	// any rules.
	if d.MinSum > 0 && !(d.AllowNone && len(in.Choices) == 0) {
		sum := 0
		for _, c := range in.Choices {
			sum += d.Options[c].Value
		}
		if sum < d.MinSum {
			return fmt.Errorf("choices total %d is below the required sum %d", sum, d.MinSum)
		}
	}
	// The declaration-dependent tap rule (Decision.ChargeTapPool): a
	// declaration must leave the tapXType obligation a payable pool after the
	// attackers it commits are set aside. This is a general wire contract, not
	// a combat rule -- the field says nothing about creatures or attacking,
	// only that the picked set draws on one shared pool (ChargeTapPool) and
	// each pick occupies Option.TapPoolCost of it -- so a rules-ignorant client
	// can enforce it from the published fields alone. ChargeTapPoolFit is the
	// one home; the engine's board-aware validateAttackers reads it too.
	if !d.ChargeTapPoolFit(in.Choices) {
		return fmt.Errorf("choices %v exhaust the tap obligation's candidate pool (%d)", in.Choices, d.ChargeTapPool)
	}
	if len(in.Rest) > 0 {
		if d.Kind != KArrange {
			return fmt.Errorf("rest is only accepted on an arrange answer, not %s", d.Kind)
		}
		if err := d.validateRestRef(in.Choices, in.Rest); err != nil {
			return err
		}
	}
	return nil
}

func (d *Decision) validateRestRef(choices, rest []int) error {
	if len(rest) != len(d.Options)-len(choices) {
		return fmt.Errorf("rest names %d of the %d unchosen options", len(rest), len(d.Options)-len(choices))
	}
	seen := make(map[int]bool, len(choices)+len(rest))
	for _, c := range choices {
		seen[c] = true
	}
	for _, r := range rest {
		if r < 0 || r >= len(d.Options) {
			return fmt.Errorf("rest choice %d out of range (%d options)", r, len(d.Options))
		}
		if seen[r] {
			return fmt.Errorf("rest choice %d is also chosen or repeated", r)
		}
		seen[r] = true
	}
	return nil
}

func (d *Decision) groupCapExceededRef(choices []int) bool {
	counts := make(map[string]int, len(choices))
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		g := d.Options[c].Group
		if g == "" {
			continue
		}
		if !d.GroupAdmits(counts, g) {
			return true
		}
		counts[g]++
	}
	return false
}

// AnswerVariants returns the intents the equivalence tests submit to both
// validators for one decision: the given answer, then n random mutations of
// it and of fresh random picks -- duplicates, out-of-range and negative
// indices, sizes outside Min..Max, group collisions, Rest lists both exact
// and broken, and a wrong seq or player now and then.
func AnswerVariants(d *Decision, base Intent, r *rand.Rand, n int) []Intent {
	out := []Intent{base}
	nOpt := len(d.Options)
	pick := func() int {
		switch r.IntN(12) {
		case 0:
			return -1 - r.IntN(2)
		case 1:
			return nOpt + r.IntN(2)
		}
		if nOpt == 0 {
			return 0
		}
		return r.IntN(nOpt)
	}
	for i := 0; i < n; i++ {
		in := Intent{Seq: d.Seq, Player: d.Player}
		switch r.IntN(40) {
		case 0:
			in.Seq++
		case 1:
			in.Player++
		}
		if i%2 == 0 && len(base.Choices) > 0 {
			in.Choices = append([]int(nil), base.Choices...)
			switch r.IntN(4) {
			case 0:
				in.Choices = append(in.Choices, in.Choices[r.IntN(len(in.Choices))])
			case 1:
				in.Choices = append(in.Choices, pick())
			case 2:
				in.Choices[r.IntN(len(in.Choices))] = pick()
			default:
				in.Choices = in.Choices[:r.IntN(len(in.Choices)+1)]
			}
		} else {
			k := r.IntN(nOpt + 3)
			if r.IntN(3) == 0 && d.Max >= d.Min {
				k = d.Min + r.IntN(d.Max-d.Min+1)
			}
			if r.IntN(2) == 0 && nOpt > 0 {
				// A permutation prefix: distinct and in range.
				perm := r.Perm(nOpt)
				if k > nOpt {
					k = nOpt
				}
				in.Choices = perm[:k]
			} else {
				for j := 0; j < k; j++ {
					in.Choices = append(in.Choices, pick())
				}
			}
		}
		if r.IntN(3) == 0 {
			// Rest: the exact complement in a random order, or a broken one.
			chosen := map[int]bool{}
			for _, c := range in.Choices {
				chosen[c] = true
			}
			for j := 0; j < nOpt; j++ {
				if !chosen[j] {
					in.Rest = append(in.Rest, j)
				}
			}
			r.Shuffle(len(in.Rest), func(a, b int) { in.Rest[a], in.Rest[b] = in.Rest[b], in.Rest[a] })
			switch r.IntN(5) {
			case 0:
				in.Rest = append(in.Rest, pick())
			case 1:
				if len(in.Rest) > 0 {
					in.Rest[r.IntN(len(in.Rest))] = pick()
				}
			case 2:
				if len(in.Rest) > 0 {
					in.Rest = in.Rest[1:]
				}
			}
		}
		out = append(out, in)
	}
	return out
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// CheckValidateEquivalent holds Validate and groupCapExceeded to the
// map-based reference on every variant; it returns the first disagreement.
func CheckValidateEquivalent(d *Decision, ins []Intent) error {
	for _, in := range ins {
		if got, want := errText(d.Validate(in)), errText(d.validateRef(in)); got != want {
			return fmt.Errorf("decision %s %+v intent %+v: Validate %q, reference %q", d.Kind, d, in, got, want)
		}
		if got, want := d.groupCapExceeded(in.Choices), d.groupCapExceededRef(in.Choices); got != want {
			return fmt.Errorf("decision %s intent %+v: groupCapExceeded %v, reference %v", d.Kind, in, got, want)
		}
		if got, want := d.RequiredChosen(in.Choices), d.requiredChosenRef(in.Choices); got != want {
			return fmt.Errorf("decision %s %+v intent %+v: RequiredChosen %v, reference %v", d.Kind, d.Options, in, got, want)
		}
	}
	if len(d.Options) <= 16 { // the block team search is exponential
		if got, want := d.requiredCore(), d.requiredCoreRef(); !reflect.DeepEqual(got, want) {
			return fmt.Errorf("decision %s %+v: requiredCore %#v, reference %#v", d.Kind, d.Options, got, want)
		}
		if got, want := d.blockRequiredCore(), d.blockRequiredCoreRef(); !reflect.DeepEqual(got, want) {
			return fmt.Errorf("decision %s %+v: blockRequiredCore %#v, reference %#v", d.Kind, d.Options, got, want)
		}
	}
	return nil
}

func randomDecision(r *rand.Rand) *Decision {
	kinds := []Kind{KTarget, KAttackers, KBlockers, KArrange, KChoose, KModes}
	d := &Decision{Seq: uint64(r.IntN(50)), Player: state.PlayerID(r.IntN(3)), Kind: kinds[r.IntN(len(kinds))]}
	n := r.IntN(14)
	if r.IntN(40) == 0 {
		n = 500 + r.IntN(200) // past the 512-option stack bitset
	}
	groups := []string{"", "", "a", "b", "c", "d"}
	props := []string{"x", "y", "z"}
	for i := 0; i < n; i++ {
		o := Option{Index: i, Kind: "pick", Group: groups[r.IntN(len(groups))],
			Controller: state.PlayerID(r.IntN(2)), Value: r.IntN(5), Value2: r.IntN(5),
			CostTaps: r.IntN(2), TapPoolCost: r.IntN(2),
			Obj: state.ObjID(1 + r.IntN(5)), Attacker: state.ObjID(20 + r.IntN(3)),
			Required: r.IntN(5) == 0, BlockMust: r.IntN(8) == 0, AttackMust: r.IntN(8) == 0}
		if r.IntN(6) == 0 {
			o.MinBlockers = 2
		}
		if r.IntN(6) == 0 {
			o.MaxBlockers = 1 + r.IntN(2)
		}
		if r.IntN(8) == 0 {
			o.CostLife = 1 + r.IntN(3)
		}
		for _, p := range props {
			if r.IntN(3) == 0 {
				o.SetProps = append(o.SetProps, p)
			}
		}
		d.Options = append(d.Options, o)
	}
	d.Min = r.IntN(3)
	d.Max = d.Min + r.IntN(n+2)
	d.Repeatable = r.IntN(4) == 0
	d.GroupLimit = r.IntN(4)
	if r.IntN(3) == 0 {
		d.GroupLimits = map[string]int{groups[2+r.IntN(4)]: r.IntN(4)}
	}
	d.TargetsWithSameController = r.IntN(4) == 0
	d.SetPropMode = []SetPropMode{SetPropNone, SetPropNone, SetPropShared, SetPropDistinct}[r.IntN(4)]
	if r.IntN(3) == 0 {
		d.MaxSum = r.IntN(10)
		d.Budgeted = r.IntN(2) == 0
	}
	if r.IntN(4) == 0 {
		d.MaxSum2 = r.IntN(10)
		d.Budgeted2 = r.IntN(2) == 0
	}
	if r.IntN(4) == 0 {
		d.MinSum = r.IntN(8)
	}
	if r.IntN(3) == 0 {
		d.ChargeTapPool = r.IntN(4)
	}
	if r.IntN(3) == 0 {
		d.PayerLife = int32(1 + r.IntN(6))
	}
	return d
}

// TestValidateMatchesMapReference: the map-free Validate (and the
// groupCapExceeded fast path) agree with the map-based reference -- verdict
// and error text -- over randomized decisions exercising every rule the
// validator applies.
func TestValidateMatchesMapReference(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 20000; i++ {
		d := randomDecision(r)
		if err := CheckValidateEquivalent(d, AnswerVariants(d, Intent{Seq: d.Seq, Player: d.Player}, r, 12)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestValidateAllocatesNothing pins the point of the rewrite: validating an
// answer to an ordinary decision allocates nothing.
func TestValidateAllocatesNothing(t *testing.T) {
	d := &Decision{Seq: 3, Player: 1, Kind: KBlockers, Min: 0, Max: 4}
	for i := 0; i < 8; i++ {
		d.Options = append(d.Options, Option{Index: i, Obj: state.ObjID(i + 1), Group: string(rune('a' + i/2))})
	}
	ok := Intent{Seq: 3, Player: 1, Choices: []int{0, 2, 4, 7}}
	if err := d.Validate(ok); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _ = d.Validate(ok) }); n != 0 {
		t.Fatalf("Validate allocated %v times per call, want 0", n)
	}
	arr := &Decision{Seq: 3, Player: 1, Kind: KArrange, Min: 0, Max: 3, Options: d.Options}
	in := Intent{Seq: 3, Player: 1, Choices: []int{1, 3}, Rest: []int{0, 2, 4, 5, 6, 7}}
	if err := arr.Validate(in); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _ = arr.Validate(in) }); n != 0 {
		t.Fatalf("arrange Validate allocated %v times per call, want 0", n)
	}
}

// The map-based Required counters and cores as they stood before the
// map-free rewrite and the no-requirement fast paths.
func (d *Decision) requiredChosenRef(choices []int) int {
	if d.Kind == KBlockers {
		return d.blockRequirementsSatisfiedRef(choices)
	}
	seen := make(map[state.ObjID]bool, len(choices)) // membership only.
	n := 0
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) || !d.Options[c].Required {
			continue
		}
		if obj := d.Options[c].Obj; !seen[obj] {
			seen[obj] = true
			n++
		}
	}
	return n
}
func (d *Decision) blockRequirementsSatisfiedRef(choices []int) int {
	seenBlocker := make(map[state.ObjID]bool)
	seenAttacker := make(map[state.ObjID]bool)
	n := 0
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		o := d.Options[c]
		if (o.BlockMust || o.Required) && !seenBlocker[o.Obj] {
			seenBlocker[o.Obj] = true
			n++
		}
		if o.AttackMust && !seenAttacker[o.Attacker] {
			seenAttacker[o.Attacker] = true
			n++
		}
	}
	return n
}

func (d *Decision) requiredCoreRef() []int {
	type pick struct{ idx, pos int }
	best := make(map[state.ObjID]*pick) // membership/lookup only -- never ranged.
	var order []*pick
	better := func(a, b int) bool {
		// Lower Value first; on a tie the lower non-mana life charge, then
		// the earlier option. Deterministic: no map iteration reaches the
		// comparison.
		oa, ob := &d.Options[a], &d.Options[b]
		if oa.Value != ob.Value {
			return oa.Value < ob.Value
		}
		ca := oa.chargeLifeCost()
		cb := ob.chargeLifeCost()
		return ca < cb
	}
	for i := range d.Options {
		o := &d.Options[i]
		if !o.Required {
			continue
		}
		p, ok := best[o.Obj]
		if !ok {
			p = &pick{idx: i, pos: i}
			best[o.Obj] = p
			order = append(order, p)
			continue
		}
		if better(i, p.idx) {
			p.idx = i
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		va, vb := d.Options[order[a].idx].Value, d.Options[order[b].idx].Value
		if va != vb {
			return va < vb
		}
		return order[a].pos < order[b].pos
	})
	life := d.PayerLifeBound()
	if life < 0 {
		// No published charge bound: the ascending-Value prefix greedy is
		// optimal for the count (the pre-charge contract) and byte-identical.
		// Per-Group caps: a full Group's pick is replaced by the Obj's
		// cheapest Required option in a Group with room (ties: lowest index).
		var core []int
		counts := map[string]int{}
		admits := func(g string) bool { return g == "" || d.GroupAdmits(counts, g) }
		sum := 0
		for _, p := range order {
			if len(core) >= d.maxChoices() {
				break
			}
			idx := p.idx
			if !admits(d.Options[idx].Group) {
				idx = -1
				for j := range d.Options {
					o := &d.Options[j]
					if !o.Required || o.Obj != d.Options[p.idx].Obj || !admits(o.Group) ||
						(d.HasBudget() && sum+o.Value > d.MaxSum) {
						continue
					}
					if idx < 0 || o.Value < d.Options[idx].Value {
						idx = j
					}
				}
				if idx < 0 {
					continue
				}
			} else if v := d.Options[idx].Value; d.HasBudget() && sum+v > d.MaxSum {
				break // ascending: nothing later fits either
			}
			sum += d.Options[idx].Value
			if g := d.Options[idx].Group; g != "" {
				counts[g]++
			}
			core = append(core, idx)
		}
		return core
	}

	// With a published life bound, this is a two-resource, multiple-choice
	// knapsack: choose at most one Required option per Obj, maximize count,
	// and respect life and (when present) Value budgets. Keep the cheapest
	// Value for each exact (life,count) state; for equal costs, keep the
	// lexicographically first option sequence. The sparse DP is
	// pseudopolynomial in the bounded life capacity and polynomial in the
	// number of objects/options, unlike exhaustive subset search.
	type key struct {
		life  int32
		count int
	}
	type candidate struct {
		value int
		picks []int
	}
	states := map[key]candidate{{}: {}}
	maxCount := d.maxChoices()
	lessPicks := func(a, b []int) bool {
		for i := 0; i < len(a) && i < len(b); i++ {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return len(a) < len(b)
	}
	groups := make([][]int, 0, len(order))
	gpos := make(map[state.ObjID]int, len(order))
	for i := range d.Options {
		o := &d.Options[i]
		if !o.Required {
			continue
		}
		g, ok := gpos[o.Obj]
		if !ok {
			g = len(groups)
			gpos[o.Obj] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], i)
	}
	for _, group := range groups {
		next := make(map[key]candidate, len(states)*(len(group)+1))
		for k, c := range states {
			// Skipping this Obj is always an available transition. Preserve
			// the same minimum-Value/lexicographic dominance as option picks:
			// map iteration can otherwise overwrite a cheaper candidate.
			if old, exists := next[k]; !exists || c.value < old.value ||
				(c.value == old.value && lessPicks(c.picks, old.picks)) {
				next[k] = c
			}
			if k.count >= maxCount {
				continue
			}
			for _, ci := range group {
				o := &d.Options[ci]
				cost := o.chargeLifeCost()
				if cost > life-k.life || (d.HasBudget() && (o.Value > d.MaxSum-c.value)) {
					continue
				}
				nk := key{life: k.life + cost, count: k.count + 1}
				nc := candidate{value: c.value + o.Value, picks: append(append([]int(nil), c.picks...), ci)}
				old, exists := next[nk]
				if !exists || nc.value < old.value || (nc.value == old.value && lessPicks(nc.picks, old.picks)) {
					next[nk] = nc
				}
			}
		}
		states = next
	}

	var bestKey key
	var bestCandidate candidate
	found := false
	for k, c := range states {
		if !found || k.count > bestKey.count ||
			(k.count == bestKey.count && (k.life < bestKey.life ||
				(k.life == bestKey.life && (c.value < bestCandidate.value ||
					(c.value == bestCandidate.value && lessPicks(c.picks, bestCandidate.picks)))))) {
			bestKey, bestCandidate, found = k, c, true
		}
	}
	// Drop picks past their Group's cap, in pick order.
	counts := map[string]int{}
	var kept []int
	for _, ci := range bestCandidate.picks {
		g := d.Options[ci].Group
		if g != "" && !d.GroupAdmits(counts, g) {
			continue
		}
		if g != "" {
			counts[g]++
		}
		kept = append(kept, ci)
	}
	if len(kept) == len(bestCandidate.picks) {
		return bestCandidate.picks
	}
	return kept
}

func (d *Decision) blockRequiredCoreRef() []int {
	type blocker struct {
		id       state.ObjID
		opts     []int
		required bool
	}
	var blocks []blocker
	pos := make(map[state.ObjID]int)
	minAttackers := make(map[state.ObjID]bool)
	// atkReq is the set of attackers carrying an attacker-oriented
	// requirement. Each is satisfied once by ANY chosen option naming it.
	atkReq := make(map[state.ObjID]bool)
	for _, o := range d.Options {
		if o.AttackMust {
			atkReq[o.Attacker] = true
		}
		if (o.BlockMust || o.Required || o.AttackMust) && o.MinBlockers > 1 {
			minAttackers[o.Attacker] = true
		}
	}
	for i, o := range d.Options {
		p, ok := pos[o.Obj]
		if !ok {
			p = len(blocks)
			pos[o.Obj] = p
			blocks = append(blocks, blocker{id: o.Obj})
		}
		blocks[p].required = blocks[p].required || o.BlockMust || o.Required
		blocks[p].opts = append(blocks[p].opts, i)
	}
	// Required creatures first; ordinary blockers need only be considered
	// as helpers for a required block with a multi-blocker minimum, or as a
	// satisfier of an attacker requirement.
	var candidates []blocker
	for _, b := range blocks {
		if b.required {
			candidates = append(candidates, b)
		}
	}
	requiredCount := len(candidates)
	for _, b := range blocks {
		if b.required {
			continue
		}
		var helper blocker
		helper.id = b.id
		for _, i := range b.opts {
			o := d.Options[i]
			if minAttackers[o.Attacker] || atkReq[o.Attacker] {
				helper.opts = append(helper.opts, i)
			}
		}
		if len(helper.opts) != 0 {
			candidates = append(candidates, helper)
		}
	}
	counts := make(map[state.ObjID]int)
	satAtk := make(map[state.ObjID]bool)
	lifeBound := d.PayerLifeBound()
	var chosen, best []int
	bestRequired := -1
	bestLength := int(^uint(0) >> 1)
	var search func(int, int, int, int32)
	search = func(at, satisfied, spent int, spentLife int32) {
		if bestRequired == requiredCount+len(atkReq) {
			return
		}
		remaining := requiredCount - at
		if remaining < 0 {
			remaining = 0
		}
		// Over-estimate the satisfaction still reachable from here: every
		// unsatisfied attacker requirement may still be satisfied ahead. An
		// over-estimate only costs search, never prunes a branch that could
		// beat the best team.
		if unsat := len(atkReq) - len(satAtk); unsat > 0 {
			remaining += unsat
		}
		if satisfied+remaining < bestRequired {
			return
		}
		if at == len(candidates) {
			for _, ci := range chosen {
				o := d.Options[ci]
				if o.MinBlockers > counts[o.Attacker] {
					return
				}
			}
			if satisfied > bestRequired || (satisfied == bestRequired && len(chosen) < bestLength) {
				bestRequired, bestLength = satisfied, len(chosen)
				best = append([]int(nil), chosen...)
			}
			return
		}
		b := candidates[at]
		try := func(ci int) {
			o := d.Options[ci]
			if len(chosen) >= d.maxChoices() || (d.HasBudget() && spent+o.Value > d.MaxSum) {
				return
			}
			// The combined non-mana LIFE charge bound is enforced INSIDE the
			// team search, not by pruning a finished team afterwards: a team
			// member dropped after the fact can break a Min$ team minimum (the
			// remaining members no longer form a legal declaration). One
			// option priced through the shared chargeLifeCost reader, so this
			// bound and the attack path's quota price a pip identically.
			cost := o.chargeLifeCost()
			if lifeBound >= 0 && spentLife+cost > lifeBound {
				return
			}
			if o.MaxBlockers > 0 && counts[o.Attacker] >= o.MaxBlockers {
				return
			}
			counts[o.Attacker]++
			chosen = append(chosen, ci)
			add := 0
			if b.required {
				add = 1
			}
			// An attacker-oriented requirement is satisfied by the FIRST
			// chosen option naming that attacker; later blockers of the same
			// attacker add nothing. Only the option that SET the flag clears
			// it on backtrack -- a second blocker of the same attacker must
			// not delete the first one's satisfaction.
			setAtk := o.AttackMust && !satAtk[o.Attacker]
			if setAtk {
				satAtk[o.Attacker] = true
				add++
			}
			search(at+1, satisfied+add, spent+o.Value, spentLife+cost)
			if setAtk {
				delete(satAtk, o.Attacker)
			}
			chosen = chosen[:len(chosen)-1]
			counts[o.Attacker]--
		}
		if b.required {
			// Prefer a legal singleton over a multi-blocker team when both
			// discharge the same requirement; this also keeps a Min$ attacker
			// available to an actual team if another required blocker needs it.
			for _, ci := range b.opts {
				if d.Options[ci].MinBlockers <= 1 {
					try(ci)
				}
			}
			for _, ci := range b.opts {
				if d.Options[ci].MinBlockers > 1 {
					try(ci)
				}
			}
			search(at+1, satisfied, spent, spentLife)
		} else {
			search(at+1, satisfied, spent, spentLife)
			for _, ci := range b.opts {
				try(ci)
			}
		}
	}
	search(0, 0, 0, 0)
	return best
}
