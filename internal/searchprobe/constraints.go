package searchprobe

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// zeroBig is the shared zero count returns for a constraint-failed branch.
// It is NEVER written (big.Int's zero value is already canonical), and count
// returns it from before its memo write, so it is never stored as a memo
// value either. Every count caller only reads the result (a Cmp, a Sub
// source, total.Set, or a Mul ARGUMENT with a fresh receiver), so sharing one
// immutable zero is safe and removes a heap allocation on the hottest return
// in the search seat.
var zeroBig = new(big.Int)

type proposalCard struct {
	ID   state.ObjID
	Name string
	// Known marks a physical card the replay's observer had already
	// introduced when the shuffle holding it was planned. Only !Known cards
	// may fill an Unseen position: drawing a known duplicate there would
	// emit no new observation identity, so the sampled world could never
	// match the observed history's identity counts.
	Known bool
}

type positionConstraint struct {
	Index int
	Name  string
	Obj   state.ObjID
	// Unseen requires the position to hold a card the replay's observer has
	// not introduced yet. Mutually exclusive with Obj: a position cannot
	// simultaneously be pinned to an exact physical card and be unseen.
	Unseen bool
}

type deadlineConstraint struct {
	Through int
	Name    string
	Count   int
	// idx is nameIndex[Name], precomputed at construction. constraintsHold
	// runs once per count call, millions of times on the search seat; a
	// string-map lookup there was a measurable slice of mapaccess_faststr.
	idx int
}

// exclusionConstraint is the deadline's dual: at most Cap copies of Name may
// occupy shuffle positions below Through. Cap 0 is the sampler's
// PolicyCompetition lever -- the named card must not be drawn early enough to
// fill the hand a bot prefers over the observed cast -- and Cap > 0 leaves
// room for the copies that are already known to be in hand at plan time.
// It is a hard feasibility constraint counted exactly like a deadline, so the
// permutation sampler re-draws rather than merely penalising such an order.
type exclusionConstraint struct {
	Through int
	Name    string
	Cap     int
	// idx is nameIndex[Name], precomputed at construction (see
	// deadlineConstraint.idx).
	idx int
}

type proposalRandom interface {
	Uint64() uint64
}

type constraintCounter struct {
	n         int
	stop      int
	names     []string
	nameIndex map[string]int
	fixedObj  map[int]proposalCard
	fixedName map[int]string
	// fixedNameIdx is nameIndex[fixedName[pos]] precomputed per fixed-name
	// position, sparing count's hot name branch a string-map lookup.
	fixedNameIdx map[int]int
	// unseenIndex records, per position, whether the position is an unseen
	// one; unseenTracked records, per relevant name, whether the name is
	// carried by at least one unseen position. The per-name unseen-remaining
	// dimension only exists for tracked names, so a constraint problem with
	// no unseen positions produces exactly the pre-fix counts, weights and
	// unranking order.
	unseenIndex   map[int]bool
	unseenTracked []bool
	exactPrefix   [][]int
	initialFree   []int
	initialUnseen []int
	deadlines     []deadlineConstraint
	exclusions    []exclusionConstraint
	memo          map[countKeyT]*big.Int
	memoStr       map[string]*big.Int
	factorials    []*big.Int
	// smallBig[i] is the immutable big.Int for i (0..n). count multiplies by a
	// multiplicity (a known-copy or unseen-copy count, or the irrelevant-copy
	// count) on every branch; those multiplicities are bounded by the card
	// count, so one shared table replaces a big.NewInt per branch -- the
	// allocation the profile charged to math/big.nat.make.
	smallBig []*big.Int
}

type constrainedPermutation struct {
	cards      []proposalCard
	positions  []positionConstraint
	deadlines  []deadlineConstraint
	exclusions []exclusionConstraint
	counter    *constraintCounter
	available  []proposalCard
	total      *big.Int
	weight     float64
}

type constraintPlanCache struct {
	plans []*constrainedPermutation
}

func newConstraintPlanCache() *constraintPlanCache { return &constraintPlanCache{} }

func (c *constraintPlanCache) get(cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint) (*constrainedPermutation, error) {
	return c.getWithExclusions(cards, positions, deadlines, nil)
}

func (c *constraintPlanCache) getWithExclusions(cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint, exclusions []exclusionConstraint) (*constrainedPermutation, error) {
	for _, plan := range c.plans {
		if slices.Equal(plan.cards, cards) && slices.Equal(plan.positions, positions) && slices.Equal(plan.deadlines, deadlines) && slices.Equal(plan.exclusions, exclusions) {
			return plan, nil
		}
	}
	plan, err := newConstrainedPermutation(cards, positions, deadlines, exclusions)
	if err != nil {
		return nil, err
	}
	c.plans = append(c.plans, plan)
	return plan, nil
}

func newConstrainedPermutation(cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint, exclusions []exclusionConstraint) (*constrainedPermutation, error) {
	counter, available, err := newConstraintCounterWithExclusions(cards, positions, deadlines, exclusions)
	if err != nil {
		return nil, err
	}
	total := counter.total(available)
	logFactorial, _ := math.Lgamma(float64(len(cards) + 1))
	return &constrainedPermutation{
		cards: append([]proposalCard(nil), cards...), positions: append([]positionConstraint(nil), positions...),
		deadlines: append([]deadlineConstraint(nil), deadlines...), exclusions: append([]exclusionConstraint(nil), exclusions...),
		counter:   counter,
		available: available, total: total, weight: logBigInt(total) - logFactorial,
	}, nil
}

func sampleConstrainedPermutation(cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint, r proposalRandom) ([]state.ObjID, float64, bool, error) {
	plan, err := newConstrainedPermutation(cards, positions, deadlines, nil)
	if err != nil {
		return nil, 0, false, err
	}
	return plan.sample(r)
}

func (p *constrainedPermutation) sample(r proposalRandom) ([]state.ObjID, float64, bool, error) {
	total := p.total
	if total.Sign() == 0 {
		return nil, 0, false, nil
	}
	if r == nil {
		return nil, 0, false, fmt.Errorf("missing permutation randomness")
	}
	rank := randomBigBelow(r, total)
	counter := p.counter
	order := make([]state.ObjID, counter.n)
	remaining := append([]proposalCard(nil), p.available...)
	counts := append([]int(nil), counter.initialFree...)
	unseen := append([]int(nil), counter.initialUnseen...)
	other := len(remaining) - sumInts(counts)
	for pos := 0; pos < counter.n; pos++ {
		if card, ok := counter.fixedObj[pos]; ok {
			order[pos] = card.ID
			continue
		}
		mustBeUnseen := counter.unseenIndex[pos]
		chosen := -1
		for i, card := range remaining {
			if name := counter.fixedName[pos]; name != "" && card.Name != name {
				continue
			}
			countIndex, relevant := counter.nameIndex[card.Name]
			// A card sits in the unseen class exactly when its name is tracked
			// and the attempt's observer has not introduced it; count() splits
			// a name's copies the same way, so the two unrank identical totals.
			unseenClass := relevant && !card.Known && counter.unseenTracked[countIndex]
			// A position that must hold an unseen card admits only the unseen
			// class; every other position admits both.
			if mustBeUnseen && !unseenClass {
				continue
			}
			nextOther := other
			if relevant {
				counts[countIndex]--
			} else {
				nextOther--
			}
			if unseenClass {
				unseen[countIndex]--
			}
			completions := counter.count(pos+1, counts, unseen, nextOther)
			if rank.Cmp(completions) < 0 {
				chosen = i
				other = nextOther
				break
			}
			if relevant {
				counts[countIndex]++
			}
			if unseenClass {
				unseen[countIndex]++
			}
			rank.Sub(rank, completions)
		}
		if chosen < 0 {
			return nil, 0, false, fmt.Errorf("constraint unranking exhausted at position %d", pos)
		}
		order[pos] = remaining[chosen].ID
		remaining = append(remaining[:chosen], remaining[chosen+1:]...)
	}
	return order, p.weight, true, nil
}

func newConstraintCounter(cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint) (*constraintCounter, []proposalCard, error) {
	return newConstraintCounterWithExclusions(cards, positions, deadlines, nil)
}

func newConstraintCounterWithExclusions(cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint, exclusions []exclusionConstraint) (*constraintCounter, []proposalCard, error) {
	c := &constraintCounter{n: len(cards), fixedObj: make(map[int]proposalCard), fixedName: make(map[int]string), unseenIndex: make(map[int]bool), memo: make(map[countKeyT]*big.Int), memoStr: make(map[string]*big.Int)}
	c.factorials = make([]*big.Int, len(cards)+1)
	c.factorials[0] = big.NewInt(1)
	for i := 1; i <= len(cards); i++ {
		c.factorials[i] = new(big.Int).Mul(c.factorials[i-1], big.NewInt(int64(i)))
	}
	c.smallBig = make([]*big.Int, len(cards)+1)
	for i := range c.smallBig {
		c.smallBig[i] = big.NewInt(int64(i))
	}
	byID := make(map[state.ObjID]proposalCard, len(cards))
	relevant := make(map[string]bool)
	for _, card := range cards {
		if card.ID == 0 || byID[card.ID].ID != 0 {
			return nil, nil, fmt.Errorf("invalid or duplicate physical card %d", card.ID)
		}
		byID[card.ID] = card
	}
	usedObjects := make(map[state.ObjID]bool)
	for _, p := range positions {
		if p.Index < 0 || p.Index >= len(cards) || p.Name == "" && p.Obj == 0 {
			return nil, nil, fmt.Errorf("invalid position constraint %+v", p)
		}
		// A position cannot simultaneously be pinned to an exact physical card
		// and be required to hold an unseen one.
		if p.Unseen && p.Obj != 0 {
			return nil, nil, fmt.Errorf("invalid position constraint %+v", p)
		}
		card := proposalCard{}
		if p.Obj != 0 {
			var ok bool
			card, ok = byID[p.Obj]
			if !ok || usedObjects[p.Obj] || p.Name != "" && p.Name != card.Name {
				return nil, nil, fmt.Errorf("invalid position constraint %+v", p)
			}
			usedObjects[p.Obj] = true
		}
		if old, ok := c.fixedObj[p.Index]; ok && old.ID != card.ID || c.fixedName[p.Index] != "" && p.Name != "" && c.fixedName[p.Index] != p.Name {
			return nil, nil, fmt.Errorf("conflicting position constraint %+v", p)
		}
		if old, seen := c.unseenIndex[p.Index]; seen && old != p.Unseen {
			return nil, nil, fmt.Errorf("conflicting position constraint %+v", p)
		}
		if p.Obj != 0 {
			if name := c.fixedName[p.Index]; name != "" && name != card.Name {
				return nil, nil, fmt.Errorf("conflicting position constraint %+v", p)
			}
			c.fixedObj[p.Index] = card
			c.fixedName[p.Index] = card.Name
			c.unseenIndex[p.Index] = false
		} else {
			if old, ok := c.fixedObj[p.Index]; ok && old.Name != p.Name {
				return nil, nil, fmt.Errorf("conflicting position constraint %+v", p)
			}
			c.fixedName[p.Index] = p.Name
			c.unseenIndex[p.Index] = p.Unseen
		}
		relevant[c.fixedName[p.Index]] = true
		if p.Index+1 > c.stop {
			c.stop = p.Index + 1
		}
	}
	for _, d := range deadlines {
		if d.Through < 0 || d.Through > len(cards) || d.Name == "" || d.Count < 0 {
			return nil, nil, fmt.Errorf("invalid deadline constraint %+v", d)
		}
		relevant[d.Name] = true
		if d.Through > c.stop {
			c.stop = d.Through
		}
	}
	for _, e := range exclusions {
		if e.Through < 0 || e.Through > len(cards) || e.Name == "" || e.Cap < 0 {
			return nil, nil, fmt.Errorf("invalid exclusion constraint %+v", e)
		}
		relevant[e.Name] = true
		if e.Through > c.stop {
			c.stop = e.Through
		}
	}
	c.names = make([]string, 0, len(relevant))
	for name := range relevant {
		c.names = append(c.names, name)
	}
	sort.Strings(c.names)
	c.nameIndex = make(map[string]int, len(c.names))
	for i, name := range c.names {
		c.nameIndex[name] = i
	}
	c.fixedNameIdx = make(map[int]int, len(c.fixedName))
	for pos, name := range c.fixedName {
		c.fixedNameIdx[pos] = c.nameIndex[name]
	}
	c.exactPrefix = make([][]int, len(cards)+1)
	for i := range c.exactPrefix {
		c.exactPrefix[i] = make([]int, len(c.names))
	}
	for pos := 0; pos < len(cards); pos++ {
		copy(c.exactPrefix[pos+1], c.exactPrefix[pos])
		if card, ok := c.fixedObj[pos]; ok {
			c.exactPrefix[pos+1][c.nameIndex[card.Name]]++
		}
	}
	// Only names carried by an unseen position need the unseen-remaining
	// dimension; for every other name it stays zero and the count branches
	// collapse to the pre-fix single-class form.
	unseenNames := make([]bool, len(c.names))
	for pos, isUnseen := range c.unseenIndex {
		if !isUnseen {
			continue
		}
		if i, ok := c.nameIndex[c.fixedName[pos]]; ok {
			unseenNames[i] = true
		}
	}
	c.unseenTracked = unseenNames
	available := make([]proposalCard, 0, len(cards)-len(c.fixedObj))
	c.initialFree = make([]int, len(c.names))
	c.initialUnseen = make([]int, len(c.names))
	for _, card := range cards {
		if usedObjects[card.ID] {
			continue
		}
		available = append(available, card)
		if i, ok := c.nameIndex[card.Name]; ok {
			c.initialFree[i]++
			if !card.Known && c.unseenTracked[i] {
				c.initialUnseen[i]++
			}
		}
	}
	c.deadlines = append([]deadlineConstraint(nil), deadlines...)
	c.exclusions = append([]exclusionConstraint(nil), exclusions...)
	for i := range c.deadlines {
		c.deadlines[i].idx = c.nameIndex[c.deadlines[i].Name]
	}
	for i := range c.exclusions {
		c.exclusions[i].idx = c.nameIndex[c.exclusions[i].Name]
	}
	return c, available, nil
}

func (c *constraintCounter) total(available []proposalCard) *big.Int {
	remaining := append([]int(nil), c.initialFree...)
	unseen := append([]int(nil), c.initialUnseen...)
	return c.count(0, remaining, unseen, len(available)-sumInts(remaining))
}

// count walks the shuffle positions left of the epoch's constraints and
// returns the number of completions. remaining[i] is the total number of free
// copies of name i still to be placed, unseen[i] how many of those are cards
// the replay observer has not introduced yet (only tracked for names carried
// by an unseen position), and other the number of irrelevant copies. A placed
// unseen card spends both dimensions, a placed known card only the total one:
// that split is what keeps a later Unseen position from being satisfied by a
// known duplicate.
func (c *constraintCounter) count(pos int, remaining []int, unseen []int, other int) *big.Int {
	// Past every constraint's Through the remaining suffix is unconstrained:
	// a deadline is already satisfied or not, and an exclusion's window has
	// closed. Returning here (before the checks below) keeps an unranking
	// call at a position beyond stop from re-evaluating a closed window.
	if pos > c.stop {
		return c.factorials[c.n-pos]
	}
	if !c.constraintsHold(pos, remaining) {
		return zeroBig
	}
	if pos >= c.stop {
		return c.factorials[c.n-pos]
	}
	key, keyOK := countKey(pos, remaining, unseen, other)
	if keyOK {
		if cached := c.memo[key]; cached != nil {
			return cached
		}
	} else if cached := c.memoStr[countKeyStr(pos, remaining, unseen, other)]; cached != nil {
		return cached
	}
	total := new(big.Int)
	// scratch is the reused receiver for every branch product. count runs
	// millions of times on the search seat, and a fresh new(big.Int) per branch
	// was the allocator's largest single line; a branch product is consumed by
	// the Add on the next line and never stored, so one scratch per call is
	// safe.
	scratch := new(big.Int)
	if _, ok := c.fixedObj[pos]; ok {
		total.Set(c.count(pos+1, remaining, unseen, other))
	} else if name := c.fixedName[pos]; name != "" {
		i := c.fixedNameIdx[pos]
		if c.unseenIndex[pos] {
			if unseen[i] > 0 {
				multiplicity := unseen[i]
				remaining[i]--
				unseen[i]--
				total.Mul(c.count(pos+1, remaining, unseen, other), c.smallBig[multiplicity])
				unseen[i]++
				remaining[i]++
			}
		} else if remaining[i] > 0 {
			knownCopies := remaining[i] - unseen[i]
			if knownCopies > 0 {
				remaining[i]--
				c.mulAdd(total, scratch, c.count(pos+1, remaining, unseen, other), knownCopies)
				remaining[i]++
			}
			if unseen[i] > 0 {
				multiplicity := unseen[i]
				remaining[i]--
				unseen[i]--
				c.mulAdd(total, scratch, c.count(pos+1, remaining, unseen, other), multiplicity)
				unseen[i]++
				remaining[i]++
			}
		}
	} else {
		for i := range remaining {
			if remaining[i] == 0 {
				continue
			}
			knownCopies := remaining[i] - unseen[i]
			if knownCopies > 0 {
				remaining[i]--
				c.mulAdd(total, scratch, c.count(pos+1, remaining, unseen, other), knownCopies)
				remaining[i]++
			}
			if unseen[i] > 0 {
				multiplicity := unseen[i]
				remaining[i]--
				unseen[i]--
				c.mulAdd(total, scratch, c.count(pos+1, remaining, unseen, other), multiplicity)
				unseen[i]++
				remaining[i]++
			}
		}
		if other > 0 {
			c.mulAdd(total, scratch, c.count(pos+1, remaining, unseen, other-1), other)
		}
	}
	if keyOK {
		c.memo[key] = total
	} else {
		c.memoStr[countKeyStr(pos, remaining, unseen, other)] = total
	}
	return total
}

// mulAdd accumulates total += count * c.smallBig[multiplicity], writing the
// product into scratch. The branch product is consumed immediately and never
// stored, so the same scratch is reused across every branch of one count call.
func (c *constraintCounter) mulAdd(total, scratch, count *big.Int, multiplicity int) {
	scratch.Mul(count, c.smallBig[multiplicity])
	total.Add(total, scratch)
}

func (c *constraintCounter) constraintsHold(pos int, remaining []int) bool {
	for _, d := range c.deadlines {
		if pos < d.Through {
			continue
		}
		i := d.idx
		placed := c.exactPrefix[pos][i] + c.initialFree[i] - remaining[i]
		if placed < d.Count {
			return false
		}
	}
	for _, e := range c.exclusions {
		// An exclusion is anti-monotone: "at most Cap copies before Through"
		// can only be judged at exactly Through, because at a later position
		// the placed count also includes cards placed after the window. The
		// DFS visits every position up to stop, so each exclusion is judged
		// once, at its own boundary.
		if pos != e.Through {
			continue
		}
		i := e.idx
		placed := c.exactPrefix[pos][i] + c.initialFree[i] - remaining[i]
		if placed > e.Cap {
			return false
		}
	}
	return true
}

// countKeyMaxNames bounds the names a packed count key can hold. A shuffle's
// name list is the distinct card names its positions, deadlines and exclusions
// mention. A 40-game constructed search-seat histogram put almost every call
// at 12-13 names, none at 23-29, and the run's maximum (30) on a single call;
// Commander search poses no constrained-permutation ask at all. 64 covers the
// measured distribution with a wide margin and, at 5+2*64 = 133 bytes, sits
// just past Go's 128-byte inline-key threshold: the runtime then stores keys
// off-bucket, which a three-way search-seat A/B (alloc_space, 3 runs each)
// showed is the most compact memo layout -- 4.02 GB total versus 4.36 GB for a
// 32-name key (69 B, inline) and 5.14 GB for a 60-name one (126 B, inline),
// because an inline key inflates every map bucket while an indirect one does
// not. The memo was the counter's dominant allocation. A problem past the
// bound falls back to the string key below rather than failing, which is also
// why the constructor does not reject one.
const countKeyMaxNames = 64

// countKeyT is countKey's packed form: a fixed-size, comparable map key with
// no backing allocation. remaining[i] and unseen[i] never exceed the card
// count of a shuffle (<= 60 in a constructed deck, and the sampler's own
// budget), so a uint8 per name is enough; countKey reports keyOK=false past
// either bound so the constructor never has to reject a problem.
type countKeyT struct {
	pos   uint16
	other uint16
	n     uint8
	rem   [countKeyMaxNames]uint8
	uns   [countKeyMaxNames]uint8
}

// countKey packs (pos, remaining, unseen, other) into a comparable struct, or
// reports keyOK=false when the name list is past countKeyMaxNames (the string
// form is then used).
func countKey(pos int, remaining []int, unseen []int, other int) (countKeyT, bool) {
	if len(remaining) > countKeyMaxNames || len(unseen) > countKeyMaxNames || other > 0xffff || pos > 0xffff {
		return countKeyT{}, false
	}
	k := countKeyT{pos: uint16(pos), other: uint16(other), n: uint8(len(remaining))}
	for i, v := range remaining {
		if v > 0xff {
			return countKeyT{}, false
		}
		k.rem[i] = uint8(v)
	}
	for i, v := range unseen {
		if v > 0xff {
			return countKeyT{}, false
		}
		k.uns[i] = uint8(v)
	}
	return k, true
}

// countKeyStr is countKey's unbounded fallback: the varint string form the
// memo used before the packed key, kept only for a name list past
// countKeyMaxNames.
func countKeyStr(pos int, remaining []int, unseen []int, other int) string {
	var b strings.Builder
	b.Grow(len(remaining) + len(unseen) + 3)
	var encoded [binary.MaxVarintLen64]byte
	write := func(value int) {
		n := binary.PutUvarint(encoded[:], uint64(value))
		_, _ = b.Write(encoded[:n])
	}
	write(pos)
	write(len(remaining))
	for _, n := range remaining {
		write(n)
	}
	for _, n := range unseen {
		write(n)
	}
	write(other)
	return b.String()
}

func randomBigBelow(r proposalRandom, limit *big.Int) *big.Int {
	if limit.Cmp(big.NewInt(1)) == 0 {
		return new(big.Int)
	}
	bits := limit.BitLen()
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	for {
		out := new(big.Int)
		for generated := 0; generated < bits; generated += 64 {
			out.Lsh(out, 64)
			out.Or(out, new(big.Int).SetUint64(r.Uint64()))
		}
		out.And(out, mask)
		if out.Cmp(limit) < 0 {
			return out
		}
	}
}

func logBigInt(n *big.Int) float64 {
	f := new(big.Float).SetInt(n)
	mantissa := new(big.Float)
	exponent := f.MantExp(mantissa)
	m, _ := mantissa.Float64()
	return math.Log(m) + float64(exponent)*math.Ln2
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
