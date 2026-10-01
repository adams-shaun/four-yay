package searchbench

// Scoring follows upstream's tools/search_bench/analyze.py (score_row, macro,
// chance) and diagnose.py (act_split) at draft-zero b1e0ba68. Each metric is
// a ratio of weighted counts, so a bootstrap resample is scored by weighting
// every item by how many times its game was drawn, which is the same multiset
// upstream builds by concatenating the drawn games' records.

// Metric names, in report order. A value is undefined (and omitted) when its
// denominator is empty, as upstream returns None.
const (
	MetricASet = iota
	MetricAStrict
	MetricASoft
	MetricBalanced
	MetricBalCast
	MetricBalAttack
	MetricBalBlock
	MetricWhichSpell
	MetricWhichBlock
	MetricASetSpell
	MetricASetHold
	MetricASetAttack
	MetricASetBlock
	MetricAStrictSpell
	MetricActSpell
	MetricActHold
	MetricActAttack
	MetricActBlock
	MetricHumanActSpell
	MetricHumanActHold
	MetricHumanActAttack
	MetricHumanActBlock
	MetricPassiveShare
	MetricHumanPassiveShare
	MetricAttackWhenHumanDidNot
	MetricBlockWhenHumanDidNot
	MetricSpellCastHumans
	MetricSpellCastOther
	MetricSpellPass
	numMetrics
)

// MetricNames are the JSON keys of the metrics above, in the same order.
var MetricNames = [numMetrics]string{
	"a_set", "a_strict", "a_soft", "balanced", "bal_cast", "bal_attack", "bal_block", "which_spell", "which_block",
	"a_set_spell", "a_set_hold", "a_set_attack", "a_set_block", "a_strict_spell",
	"act_spell", "act_hold", "act_attack", "act_block",
	"human_act_spell", "human_act_hold", "human_act_attack", "human_act_block",
	"passive_share", "human_passive_share", "attack_when_human_did_not", "block_when_human_did_not",
	"spell_cast_humans", "spell_cast_other", "spell_pass",
}

// DecisionTypes is the fixed report order of the four decision types.
var DecisionTypes = [4]DecisionType{DecisionSpell, DecisionHold, DecisionAttack, DecisionBlock}

func typeIndex(t DecisionType) int {
	for i, v := range DecisionTypes {
		if v == t {
			return i
		}
	}
	return -1
}

// scored is one item's outcome under one result.
type scored struct {
	typ, cluster     int
	choice           int
	set, strict      bool
	soft             float64
	hasSoft          bool
	humanAct, act    bool
	spellHumansCast  bool // spell: the agent cast one of the human's casts
	spellOtherAction bool // spell: the agent acted, but not as the human did
}

// scoreItem scores one validated result against its item.
//
//   - set: the choice is in the lenient label (analyze.py score_row "set").
//   - strict: the choice is in label_strict ("strict").
//   - soft: the root's visit share on the lenient label ("soft"), when the
//     result has root visits.
//   - act: choice != 0. The human act bit is the label's Act, which for a
//     spell item is always true (act_split: "the human cast a spell this
//     turn"), even when the lenient label also accepts Pass.
func scoreItem(it Item, r Result, cluster int) scored {
	c := r.AgentChoices[0]
	s := scored{typ: typeIndex(it.Type), cluster: cluster, choice: c, humanAct: it.Label.Act, act: c != 0}
	s.set = containsChoice(it.Label.Alternatives, c)
	s.strict = containsChoice(it.Label.Strict, c)
	total, on := 0, 0
	for _, o := range r.Root {
		total += o.Visits
		if containsChoice(it.Label.Alternatives, o.Choice) {
			on += o.Visits
		}
	}
	if total > 0 {
		s.soft, s.hasSoft = float64(on)/float64(total), true
	}
	if it.Type == DecisionSpell {
		s.spellHumansCast = s.strict
		s.spellOtherAction = s.act && !s.strict
	}
	return s
}

type confusion struct{ tp, fn, fp, tn int }

func (c *confusion) add(human, agent bool, w int) {
	switch {
	case human && agent:
		c.tp += w
	case human:
		c.fn += w
	case agent:
		c.fp += w
	default:
		c.tn += w
	}
}

// balanced is (recall on act + recall on wait) / 2, undefined unless both
// classes are present (act_split's bal_acc).
func (c confusion) balanced() (float64, bool) {
	if c.tp+c.fn == 0 || c.tn+c.fp == 0 {
		return 0, false
	}
	return (float64(c.tp)/float64(c.tp+c.fn) + float64(c.tn)/float64(c.tn+c.fp)) / 2, true
}

// tally accumulates weighted outcomes. Weights are integers (the number of
// times an item's game was drawn), so a tally is exact.
type tally struct {
	n, set, strict, act, humanAct [4]int
	softSum                       [4]float64
	softN                         [4]int
	bin                           [3]confusion // cast or hold (spell+hold), attack, block
	whichN, whichOK               [2]int       // spell, block
	spellHumans, spellOther       int
	passive, humanPassive, total  int
}

func (t *tally) add(s scored, w int) {
	if w == 0 {
		return
	}
	i := s.typ
	t.n[i] += w
	t.total += w
	if s.set {
		t.set[i] += w
	}
	if s.strict {
		t.strict[i] += w
	}
	if s.act {
		t.act[i] += w
	} else {
		t.passive += w
	}
	if s.humanAct {
		t.humanAct[i] += w
	} else {
		t.humanPassive += w
	}
	if s.hasSoft {
		t.softSum[i] += s.soft * float64(w)
		t.softN[i] += w
	}
	group := 0
	if i == 2 {
		group = 1
	} else if i == 3 {
		group = 2
	}
	t.bin[group].add(s.humanAct, s.act, w)
	// "which", given that both acted (act_split's "what"): the spell among
	// the human's strict casts, and the attacker blocked.
	if s.humanAct && s.act && (i == 0 || i == 3) {
		k := 0
		if i == 3 {
			k = 1
		}
		t.whichN[k] += w
		if (i == 0 && s.strict) || (i == 3 && s.set) {
			t.whichOK[k] += w
		}
	}
	if s.spellHumansCast {
		t.spellHumans += w
	}
	if s.spellOtherAction {
		t.spellOther += w
	}
}

type metricVec struct {
	v  [numMetrics]float64
	ok [numMetrics]bool
}

func (m *metricVec) set(k int, num, den float64) {
	if den > 0 {
		m.v[k], m.ok[k] = num/den, true
	}
}

// macro is analyze.py's macro: the mean of the per-type means over the
// types present.
func macro(num [4]float64, den [4]int) (float64, bool) {
	sum, k := 0.0, 0
	for i := range den {
		if den[i] > 0 {
			sum += num[i] / float64(den[i])
			k++
		}
	}
	if k == 0 {
		return 0, false
	}
	return sum / float64(k), true
}

func ints(v [4]int) [4]float64 {
	var out [4]float64
	for i := range v {
		out[i] = float64(v[i])
	}
	return out
}

func (t tally) metrics() metricVec {
	var m metricVec
	m.v[MetricASet], m.ok[MetricASet] = macro(ints(t.set), t.n)
	m.v[MetricAStrict], m.ok[MetricAStrict] = macro(ints(t.strict), t.n)
	m.v[MetricASoft], m.ok[MetricASoft] = macro(t.softSum, t.softN)
	sum, k := 0.0, 0
	for g, key := range [3]int{MetricBalCast, MetricBalAttack, MetricBalBlock} {
		if v, ok := t.bin[g].balanced(); ok {
			m.v[key], m.ok[key] = v, true
			sum += v
			k++
		}
	}
	// balanced_score: the mean of the balanced accuracies that exist.
	if k > 0 {
		m.v[MetricBalanced], m.ok[MetricBalanced] = sum/float64(k), true
	}
	m.set(MetricWhichSpell, float64(t.whichOK[0]), float64(t.whichN[0]))
	m.set(MetricWhichBlock, float64(t.whichOK[1]), float64(t.whichN[1]))
	for i := 0; i < 4; i++ {
		m.set(MetricASetSpell+i, float64(t.set[i]), float64(t.n[i]))
		m.set(MetricActSpell+i, float64(t.act[i]), float64(t.n[i]))
		m.set(MetricHumanActSpell+i, float64(t.humanAct[i]), float64(t.n[i]))
	}
	m.set(MetricAStrictSpell, float64(t.strict[0]), float64(t.n[0]))
	m.set(MetricPassiveShare, float64(t.passive), float64(t.total))
	m.set(MetricHumanPassiveShare, float64(t.humanPassive), float64(t.total))
	m.set(MetricAttackWhenHumanDidNot, float64(t.bin[1].fp), float64(t.bin[1].fp+t.bin[1].tn))
	m.set(MetricBlockWhenHumanDidNot, float64(t.bin[2].fp), float64(t.bin[2].fp+t.bin[2].tn))
	m.set(MetricSpellCastHumans, float64(t.spellHumans), float64(t.n[0]))
	m.set(MetricSpellCastOther, float64(t.spellOther), float64(t.n[0]))
	m.set(MetricSpellPass, float64(t.n[0]-t.act[0]), float64(t.n[0]))
	return m
}

// Chance is analyze.py's chance(): a uniform pick over the distinct legal
// options matches |label ∩ legal| / |legal| of the time, averaged per type
// and then macro-averaged. It uses the lenient label, as upstream does.
func Chance(items []Item) (macroChance float64, perType [4]float64, ok bool) {
	var num [4]float64
	var den [4]int
	for _, it := range items {
		i := typeIndex(it.Type)
		num[i] += float64(len(it.Label.Alternatives)) / float64(len(it.Options))
		den[i]++
	}
	for i := range den {
		if den[i] > 0 {
			perType[i] = num[i] / float64(den[i])
		}
	}
	macroChance, ok = macro(num, den)
	return macroChance, perType, ok
}
