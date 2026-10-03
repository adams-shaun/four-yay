package azmcts

// Per-creature combat (Options.CombatSteps). Upstream MageZero never
// searches a whole declaration: ComputerPlayer.selectAttackersOneAtATime
// sorts the available attackers by id and asks one chooseUse per creature
// ("attack with: X?"; a yes declares it against the first defender), and
// selectBlockersOneAtATime sorts the available blockers by id and asks each,
// through makeChoice over a TargetAttackingCreature(0, 1), which attacker it
// blocks, Stop Choosing meaning none. Every one of those asks is a tree node
// (MCTSPlayer.chooseUse / makeChoice freeze the simulated game there) and a
// separate root decision of the real game with its own search and its own
// training record (ComputerPlayerMCTS.chooseUse / makeChoice ->
// getNextAction). A use node's children are [no, yes] in that order
// (MCTSNode.createChildren: "always add false before true").
//
// Here a split declaration is a chain of tree points inside one engine
// decision: step k offers creature k's answers -- an attacker's no / yes
// (yes is its attack on the defending player), a blocker's no block / one
// option per attacker it may block -- and the declaration is submitted after
// the last step. Creatures go in object order (upstream's sort by id); the
// candidates in upstream's order, the "no" answer first, never the bot's
// answer first: the bot's per-creature answer is only the fallback a failed
// search plays (Result.Intent). The prior at a step is uniform (no network
// head scores a single creature's answer here; upstream's offline search,
// mzplay's, is uniform too).
//
// The assembled declaration is repaired with the engine's own rules before
// it is submitted, as upstream's engine resolves a per-creature declaration
// it would not accept (CombatDeclaration): an attack that leaves out a
// creature that must attack gains it (decision.FitRequired), a block the
// whole-declaration rules refuse (menace, a block count bound, an unpayable
// block charge) is dropped (botpolicy.LegalBlockChoices). A walk whose
// repaired declaration the engine still rejects is discarded like any
// rejected submit.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// CombatStep is a Search root that is one creature of a split declaration
// (Options.CombatSteps, Root.Combat): Result.Step.
type CombatStep struct {
	// Index is the creature's position in the declaration's step order and
	// Steps the declaration's creature count.
	Index, Steps int
	// Obj is the creature; Block reports a blockers declaration.
	Obj   state.ObjID
	Block bool
	// Answers parallels Result.Keys and Result.Candidates: the option index
	// each candidate answers with, -1 for no attack / no block.
	Answers []int
}

// combatNoKey and combatNoBlockKey are the "no" answers' keys. A step's
// keys need only be distinct among its own candidates (a tree position is
// its path), and the yes answers are option keys.
const (
	combatNoKey      Key = "combat:no-attack"
	combatNoBlockKey Key = "combat:no-block"
)

// combatPlan is one declaration split into creature steps. It is never
// written after it is built.
type combatPlan struct {
	d     *decision.Decision
	block bool
	steps []combatStepPlan
}

type combatStepPlan struct {
	obj state.ObjID
	// answers are the option indices the creature may answer with, -1 (no
	// attack / no block) first; keys parallel them.
	answers []int
	keys    []Key
	// cut reports that the candidate limit cut this step's answers.
	cut bool
}

// combatWalk is a walk's progress through a split declaration.
type combatWalk struct {
	plan    *combatPlan
	answers []int
	// opp marks the opponent's declaration (Options.OpponentNodes).
	opp bool
}

func (c *combatWalk) clone() *combatWalk {
	if c == nil {
		return nil
	}
	out := *c
	out.answers = append([]int(nil), c.answers...)
	return &out
}

// printTag is the walk's progress as a world print extension: the steps
// answered so far. Empty outside a split declaration.
func (c *combatWalk) printTag() string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "combat %d:", c.plan.d.Seq)
	for _, a := range c.answers {
		fmt.Fprintf(&b, " %d", a)
	}
	return b.String()
}

// combatOptions lists, per creature, the options of d it may answer with
// (an attacker's player attack; a blocker's options, one per attacker), the
// creatures in object order. A creature with no answer is left out.
func combatOptions(d *decision.Decision) (objs []state.ObjID, opts [][]int) {
	block := d.Kind == decision.KBlockers
	at := func(obj state.ObjID) int {
		for i, o := range objs {
			if o == obj {
				return i
			}
		}
		return -1
	}
	for i, o := range d.Options {
		if o.Obj == 0 || (block && o.Attacker == 0) {
			continue
		}
		k := at(o.Obj)
		if k < 0 {
			objs = append(objs, o.Obj)
			opts = append(opts, nil)
			k = len(objs) - 1
		}
		if !block {
			// The creature's attack on the defending player (upstream
			// declares every yes against the first defender): its first
			// player attack, else its first option of any kind.
			switch {
			case len(opts[k]) == 0:
				opts[k] = []int{i}
			case d.Options[opts[k][0]].Battle != 0 && o.Battle == 0:
				opts[k][0] = i
			}
			continue
		}
		known := false
		for _, j := range opts[k] {
			known = known || d.Options[j].Attacker == o.Attacker
		}
		if !known {
			opts[k] = append(opts[k], i)
		}
	}
	idx := make([]int, len(objs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return objs[idx[a]] < objs[idx[b]] })
	so, sp := make([]state.ObjID, len(objs)), make([][]int, len(objs))
	for i, j := range idx {
		so[i], sp[i] = objs[j], opts[j]
	}
	return so, sp
}

// optionKey is the semantic key of option i of d alone, through obs (a
// collector for d.Player that has observed d): the key of a one-option copy
// of d, so the single answer needs no whole-declaration legality.
func optionKey(obs *searchprobe.Collector, d *decision.Decision, i int) (Key, error) {
	one := *d
	one.Min, one.Max = 1, 1
	o := d.Options[i]
	o.Index, o.Required, o.Group = 0, false, ""
	one.Options = []decision.Option{o}
	k, err := obs.IntentKey(&one, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}})
	if err != nil {
		return "", err
	}
	return Key(k), nil
}

// newCombatPlan splits attackers or blockers decision d (observed by obs, a
// collector for d.Player) into creature steps, each step's answers capped at
// limit. A plan with no step is returned as nil: the declaration has no
// creature to ask about.
func newCombatPlan(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, limit int) (*combatPlan, error) {
	if d == nil || (d.Kind != decision.KAttackers && d.Kind != decision.KBlockers) {
		return nil, errors.New("azmcts: a combat plan needs an attackers or blockers decision")
	}
	if _, err := obs.ObserveDecision(e, d); err != nil {
		return nil, err
	}
	// The plan keeps its own copy of the decision's options: a walk's
	// engine (and every decision it posed) may be recycled while a node
	// cache state still holds the plan.
	dc := *d
	dc.Options = append([]decision.Option(nil), d.Options...)
	d = &dc
	p := &combatPlan{d: d, block: d.Kind == decision.KBlockers}
	no := combatNoKey
	if p.block {
		no = combatNoBlockKey
	}
	objs, opts := combatOptions(d)
	for k, obj := range objs {
		st := combatStepPlan{obj: obj, answers: []int{-1}, keys: []Key{no}}
		for _, i := range opts[k] {
			if len(st.answers) >= limit {
				st.cut = true
				break
			}
			key, err := optionKey(obs, d, i)
			if err != nil {
				return nil, err
			}
			st.answers = append(st.answers, i)
			st.keys = append(st.keys, key)
		}
		if len(st.answers) >= 2 {
			p.steps = append(p.steps, st)
		}
	}
	if len(p.steps) == 0 {
		return nil, nil
	}
	return p, nil
}

// stepCands is step k's candidates: the answers as one-option intents (no
// choice for the "no" answer).
func (p *combatPlan) stepCands(k int) []cand {
	st := p.steps[k]
	out := make([]cand, len(st.answers))
	for i, a := range st.answers {
		out[i] = cand{key: st.keys[i], in: answerIntent(p.d, a)}
	}
	return out
}

// answerIntent is answer a of a step as an intent on d: the one option, or
// no choice for -1. It is a step's answer, never a submit.
func answerIntent(d *decision.Decision, a int) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if a >= 0 {
		in.Choices = []int{a}
	}
	return in
}

// stepAnswer is the answer a step intent names (answerIntent's inverse).
func stepAnswer(in decision.Intent) int {
	if len(in.Choices) == 1 {
		return in.Choices[0]
	}
	return -1
}

// botAnswer is the bot's declaration bot read as step k's answer: the
// creature's answer among the step's, -1 when the bot leaves it out.
func (p *combatPlan) botAnswer(k int, bot decision.Intent) int {
	st := p.steps[k]
	for _, c := range bot.Choices {
		if c < 0 || c >= len(p.d.Options) || p.d.Options[c].Obj != st.obj {
			continue
		}
		if !p.block {
			return st.answers[1] // the creature attacks: its step's yes
		}
		for _, a := range st.answers {
			if a == c {
				return a
			}
		}
	}
	return -1
}

// point is step k as a tree point.
func (p *combatPlan) point(k int, opp bool) *Point {
	st := p.steps[k]
	keys := append([]Key(nil), st.keys...)
	return &Point{Keys: keys, Prior: uniform(len(keys)), cut: st.cut, opp: opp}
}

// declaration assembles the steps' answers into the declaration to submit,
// repaired by the engine's own rules (combat.go's doc). legal is the block
// legality filter (nil for attackers).
func (p *combatPlan) declaration(answers []int, legal func([]int) []int) (decision.Intent, error) {
	return assembleCombat(p.d, answers, legal)
}

func assembleCombat(d *decision.Decision, answers []int, legal func([]int) []int) (decision.Intent, error) {
	var choices []int
	for _, a := range answers {
		if a >= 0 {
			choices = append(choices, a)
		}
	}
	if d.Kind == decision.KBlockers {
		if legal != nil {
			choices = legal(choices)
		}
		choices = d.FitRequired(choices)
		if legal != nil {
			choices = legal(choices)
		}
	} else {
		choices = d.FitRequired(choices)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if len(choices) > 0 {
		in.Choices = append([]int(nil), choices...)
	}
	if err := d.Validate(in); err != nil {
		return in, fmt.Errorf("%w: the split declaration: %v", ErrSubmit, err)
	}
	if d.RequiredChosen(in.Choices) < d.RequiredQuota() {
		return in, fmt.Errorf("%w: the split declaration misses a required creature", ErrSubmit)
	}
	return in, nil
}

// blockLegal is the block legality filter on e for decision d.
func blockLegal(e *rules.Engine, d *decision.Decision, scratch *boardScratch) func([]int) []int {
	if d.Kind != decision.KBlockers {
		return nil
	}
	return func(choices []int) []int {
		b := boardInto(e, d.Player, scratch)
		return botpolicy.LegalBlockChoices(b, d, choices)
	}
}

// CombatDeclaration is the declaration a split attackers or blockers
// decision d of engine e submits once every creature is answered
// (Options.CombatSteps): answers[k] is creature k's answer (Result.Step's
// Answers entry, -1 for none), and the declaration is repaired by the
// engine's own rules exactly as a simulation's is.
func CombatDeclaration(e *rules.Engine, d *decision.Decision, answers []int) (decision.Intent, error) {
	if d == nil || (d.Kind != decision.KAttackers && d.Kind != decision.KBlockers) {
		return decision.Intent{}, errors.New("azmcts: CombatDeclaration needs an attackers or blockers decision")
	}
	return assembleCombat(d, answers, blockLegal(e, d, nil))
}

// CombatStepCount is the number of creature steps a split declaration of d
// asks (Options.CombatSteps), 0 when it asks none (no creature can attack
// or block: the declaration is the bot's).
func CombatStepCount(d *decision.Decision) int {
	if d == nil || (d.Kind != decision.KAttackers && d.Kind != decision.KBlockers) {
		return 0
	}
	_, opts := combatOptions(d)
	n := 0
	for _, o := range opts {
		if len(o) > 0 {
			n++
		}
	}
	return n
}

// StepAnswer is the creature answer a split step's Result plays: its
// Intent's one option, -1 for none.
func (r Result) StepAnswer() int { return stepAnswer(r.Intent) }

// combatSearched reports whether decision d splits into steps under opts'
// kinds.
func combatSearched(d *decision.Decision, kinds Kinds) bool {
	return d != nil && ((d.Kind == decision.KAttackers && kinds.Attackers) || (d.Kind == decision.KBlockers && kinds.Blockers))
}

// combatPoint is the walk's current step as a tree point, with e.cands set
// to its candidates.
func (e *engineEnv) combatPoint() *Point {
	k := len(e.combat.answers)
	e.cands = e.combat.plan.stepCands(k)
	pt := e.combat.plan.point(k, e.combat.opp)
	if pt.cut {
		e.cfg.stats.Truncated++
	}
	return pt
}

// startCombat splits the searched seat's declaration pd: the walk's first
// step as a point, or false when pd asks about no creature (the bot's
// declaration is then played) or cannot be observed.
func (e *engineEnv) startCombat(pd *decision.Decision, obs *searchprobe.Collector, limit int, opp bool) (*Point, bool) {
	plan, err := newCombatPlan(obs, e.e, pd, limit)
	if err != nil || plan == nil {
		return nil, false
	}
	e.cur, e.combat = pd, &combatWalk{plan: plan, opp: opp}
	return e.combatPoint(), true
}

// playCombat plays step answer k: the next step, or after the last one the
// repaired declaration's submit and the walk on to the next point.
func (e *engineEnv) playCombat(c cand) (*Point, error) {
	e.combat.answers = append(e.combat.answers, stepAnswer(c.in))
	if len(e.combat.answers) < len(e.combat.plan.steps) {
		return e.combatPoint(), nil
	}
	plan, answers := e.combat.plan, e.combat.answers
	e.combat = nil
	in, err := plan.declaration(answers, blockLegal(e.e, plan.d, e.cfg.enumBoard))
	if err != nil {
		return nil, err
	}
	if err := e.submit(e.cur, in); err != nil {
		return nil, err
	}
	e.plies++
	return e.advance()
}
