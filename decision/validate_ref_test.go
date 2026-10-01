package decision

// The map-based Validate, validateRest and groupCapExceeded exactly as they
// stood before the map-free rewrite (perf: Validate runs per candidate answer
// in search). validateRef is the reference the equivalence tests hold the
// live Validate to, error text included.

import (
	"fmt"
	"math/rand/v2"
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
	for _, c := range in.Choices {
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
	if d.MinSum > 0 {
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
			CostTaps: r.IntN(2), TapPoolCost: r.IntN(2)}
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
		d.Options = append(d.Options, Option{Index: i, Group: string(rune('a' + i/2))})
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
