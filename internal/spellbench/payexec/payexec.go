// Package payexec lowers a gorge payment plan onto the manual mana surface.
//
// gorge has two mana surfaces. On the manual one every mana ability is an
// ordinary priority option ("activate", the SpellBench engines'
// activate_mana_ability) and a spell's "cast" option is offered once the
// floating pool pays it. On the auto-pay one a priority decision also
// carries Decision.PaymentActions: per castable spell, ordered PaymentPlans
// whose Activations are an explicit witness of which source to activate for
// which mana. A seat that wants the planner's tapping on a surface that
// exposes raw taps -- a bot playing an engine that poses mana taps as
// decision candidates -- chooses "cast X", takes gorge's plan for X, and
// LOWERS it: answers successive manual decisions by activating each plan
// source in order (answering any mana-ability or colour ask with the
// witness's production), then selects the ordinary cast option for X.
//
// An Execution is that lowering. It reads only decisions and the payer's
// floating pool, never an engine, so it runs equally against a live engine
// (Run, ActionFor), a hosted seat's View (PoolFromView) or a client's shadow
// state that mirrors gorge's wire decisions.
//
// Every step is verified, never guessed:
//
//   - at each priority decision the payer's pool must equal the pool the plan
//     predicts (the pool at Start plus every completed activation's
//     Produces), so a source that made something else is caught before the
//     next source is spent;
//   - the "activate" option for the next source, a mana-ability/colour ask
//     option producing exactly the witness's mana, and finally the play's
//     own option (the ordinary cast, Mode "" and AltCostIndex 0, or the
//     printed activated ability) must each be offered.
//
// Steps run in the plan's order except that last-resort steps (a disclosed
// Consequence: a sacrifice, life, damage) run after every other step. A
// sacrifice can change what a later source makes (Tinder Wall sacrificed
// before Overgrown Battlement counts its defenders) and can trigger (Writhing
// Chrysalis on an Eldrazi Spawn sacrifice), so the plain taps go first.
//
// Two things the lowering survives instead of aborting (a manual-tap surface
// always shows them, and neither is a divergence):
//
//   - a decision that is not the lowering's -- a trigger to order, a
//     trigger's target, an optional trigger -- is a Yield: the caller
//     answers it and the lowering resumes at the next priority decision,
//     pool checked as always;
//   - at the final step, a play that is not offered while the stack is not
//     empty (a mana source's trigger now sits above a sorcery-speed play) is
//     WAITED for: the lowering passes priority, the floating mana stays in
//     the pool until the step ends, and once the stack resolves the play is
//     offered again. The caller reports the stack through Surface.
//
// Any other divergence aborts the Execution with a Reason; the caller then
// answers that decision itself (and may re-plan from the pool it now holds).
// The plan's own final PoolAfter is the engine's business once the play is
// submitted; the pool check before it covers everything the lowering
// controls.
//
// Scope: gorge's cast planner (Decision.PaymentActions) plans only ordinary
// casts; rules.PotentialPaymentPlans adds witnesses for the mana part of
// printed activated abilities, mode casts (flashback, bestow, kicker ...),
// {X} and hybrid casts (StartPlay). A granted ability has no witness to
// lower.
//
// Determinism: every answer is a pure function of the plan, the decisions
// and the surfaces shown; option lists are scanned in engine order.
package payexec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Status is an Execution's state after a Step.
type Status int

const (
	// InProgress: the returned intent answers the decision; more steps follow.
	InProgress Status = iota
	// Done: the returned intent selects the planned cast; the lowering is over.
	Done
	// Aborted: the manual surface diverged from the plan. The returned
	// intent is empty and the caller answers the decision itself.
	Aborted
	// Yield: the decision is not the lowering's (a trigger to order, a
	// target for a trigger). The returned intent is empty, the caller
	// answers it, and the lowering continues (Status stays InProgress).
	Yield
)

func (s Status) String() string {
	switch s {
	case InProgress:
		return "in_progress"
	case Done:
		return "done"
	case Aborted:
		return "aborted"
	case Yield:
		return "yield"
	}
	return "unknown"
}

// Abort reasons (Execution.Reason).
const (
	ReasonNoPlan             = "no_plan"              // the action offers no plan
	ReasonWrongPlayer        = "wrong_player"         // a decision for another seat arrived mid-lowering
	ReasonPoolMismatch       = "pool_mismatch"        // the pool is not what the plan predicts
	ReasonActivateNotOffered = "activate_not_offered" // the next source's activate option is missing
	ReasonManaUnmatched      = "mana_option_unmatched"
	ReasonCastNotOffered     = "cast_not_offered" // every source ran, yet the play's option is not offered
	ReasonWaitExhausted      = "wait_exhausted"   // the play stayed unoffered through maxWaits stack passes
	ReasonUnexpected         = "unexpected_decision"
	ReasonFinished           = "already_finished" // Step after Done/Aborted
)

// Play names the option a lowering ends by selecting: a cast (Kind "cast":
// the ordinary cast with Mode "", or a cast in Mode -- flashback, bestowed,
// kicked ... -- never an alternative cost index) or a printed activated
// ability (Kind "ability": the pile index Ability, no SVar/Keyword/gained
// anchor, no alternative cost).
type Play struct {
	Kind    string
	Obj     state.ObjID
	Ability int
	Mode    string
}

// Matches reports whether o is the play's own option.
func (p Play) Matches(o *decision.Option) bool {
	if o.Kind != p.Kind || o.Obj != p.Obj || o.Mode != p.Mode || o.AltCostIndex != 0 {
		return false
	}
	if p.Kind == "ability" {
		return o.Ability == p.Ability && o.SVar == "" && o.Keyword == "" && o.GainedSource == 0
	}
	return true
}

// Surface is what a Step reads beside the decision: the payer's floating
// pool and how many objects are on the stack.
type Surface struct {
	Pool  decision.ManaAmount
	Stack int
}

// maxWaits bounds the stack passes one lowering makes waiting for its play
// (package doc): each pass resolves an object or ends the step, so the bound
// only guards a stack that keeps refilling.
const maxWaits = 16

// Execution lowers one plan. Create it with Start or StartPlay, then feed it
// every decision the payer is posed until it reports Done or Aborted.
type Execution struct {
	Player state.PlayerID
	Cast   decision.PlannedCast // the planned cast (Start); zero for StartPlay
	Play   Play
	Plan   decision.PaymentPlan

	steps  []decision.PaymentActivation // Plan.Activations, last-resort steps last
	next   int                          // index of the next activation to start
	cur    int                          // activation whose asks may be posed now, -1 none
	expect decision.ManaAmount          // pool predicted at the next priority decision
	status Status

	// Reason names the divergence once Aborted.
	Reason string
	// Detail is a human-readable elaboration of Reason.
	Detail string
	// Taps counts the activate options selected; Asks the mana/colour asks
	// answered; Yields the foreign decisions handed back; Waits the stack
	// passes made waiting for the play.
	Taps, Asks, Yields, Waits int
}

// SelectPlan is the deterministic plan choice: the first offered plan none
// of whose steps discloses a Consequence (a sacrifice, life, damage...), else
// the first plan. gorge ranks plans, so the first consequence-free plan is
// the planner's own best non-last-resort witness.
func SelectPlan(a *decision.PaymentAction) (decision.PaymentPlan, bool) {
	if a == nil || len(a.Plans) == 0 {
		return decision.PaymentPlan{}, false
	}
	for _, p := range a.Plans {
		if !HasConsequence(p) {
			return decision.ClonePaymentPlan(p), true
		}
	}
	return decision.ClonePaymentPlan(a.Plans[0]), true
}

// HasConsequence reports whether some step of p is a last-resort step.
func HasConsequence(p decision.PaymentPlan) bool {
	for _, a := range p.Activations {
		if a.Consequence != nil {
			return true
		}
	}
	return false
}

// Start begins lowering a's selected plan for player, whose pool now holds
// pool. It never fails for an offered action with a plan; with none it
// returns an already-aborted Execution (ReasonNoPlan).
func Start(player state.PlayerID, a *decision.PaymentAction, pool decision.ManaAmount) *Execution {
	x := &Execution{Player: player, cur: -1, expect: pool}
	if a != nil {
		x.Cast = a.Cast
		x.Play = Play{Kind: "cast", Obj: a.Cast.Object}
	}
	plan, ok := SelectPlan(a)
	if !ok {
		x.abort(ReasonNoPlan, "")
		return x
	}
	x.setPlan(plan)
	return x
}

// StartPlay begins lowering plan, a witness for play's mana part (an
// activated ability's, rules.PotentialPaymentPlans), for player, whose pool
// now holds pool.
func StartPlay(player state.PlayerID, play Play, plan decision.PaymentPlan, pool decision.ManaAmount) *Execution {
	x := &Execution{Player: player, cur: -1, expect: pool, Play: play}
	x.setPlan(decision.ClonePaymentPlan(plan))
	return x
}

// setPlan records plan and its execution order: every plain step in plan
// order, then every last-resort step in plan order (package doc).
func (x *Execution) setPlan(plan decision.PaymentPlan) {
	x.Plan = plan
	x.steps = make([]decision.PaymentActivation, 0, len(plan.Activations))
	for _, a := range plan.Activations {
		if a.Consequence == nil {
			x.steps = append(x.steps, a)
		}
	}
	for _, a := range plan.Activations {
		if a.Consequence != nil {
			x.steps = append(x.steps, a)
		}
	}
}

// Steps is the execution order of the plan's activations.
func (x *Execution) Steps() []decision.PaymentActivation { return x.steps }

// Status reports the current state.
func (x *Execution) Status() Status { return x.status }

// Remaining is the number of plan activations not yet started.
func (x *Execution) Remaining() int { return len(x.steps) - x.next }

func (x *Execution) abort(reason, detail string) {
	x.status, x.Reason, x.Detail = Aborted, reason, detail
}

// Step is StepOn with an empty stack.
func (x *Execution) Step(d *decision.Decision, pool decision.ManaAmount) (decision.Intent, Status) {
	return x.StepOn(d, Surface{Pool: pool})
}

// StepOn answers d, a decision posed to the payer, on surface s. On
// InProgress and Done the intent answers d (Seq and Player set); on Yield
// and Aborted it is empty and the caller must answer d itself (after a
// Yield the lowering continues).
func (x *Execution) StepOn(d *decision.Decision, s Surface) (decision.Intent, Status) {
	if x.status != InProgress {
		if x.Reason == "" {
			x.abort(ReasonFinished, "")
		}
		return decision.Intent{}, Aborted
	}
	if d == nil || d.Player != x.Player {
		x.abort(ReasonWrongPlayer, "")
		return decision.Intent{}, Aborted
	}
	switch {
	case d.Kind == decision.KPriority:
		return x.priority(d, s)
	case d.Kind == decision.KChoose && x.cur >= 0 && manaAsk(d, x.steps[x.cur].Source):
		return x.manaAsk(d)
	}
	// Not the lowering's decision: a trigger its activation put on the
	// stack (to order, to target), an optional trigger. The caller answers;
	// the next priority decision resumes the lowering with the pool check.
	x.cur = -1
	x.Yields++
	return decision.Intent{}, Yield
}

func (x *Execution) priority(d *decision.Decision, s Surface) (decision.Intent, Status) {
	if s.Pool != x.expect {
		x.abort(ReasonPoolMismatch, fmt.Sprintf("before step %d: pool %v, plan predicts %v", x.next, s.Pool, x.expect))
		return decision.Intent{}, Aborted
	}
	x.cur = -1
	if x.next < len(x.steps) {
		act := x.steps[x.next]
		i := findOption(d, func(o *decision.Option) bool { return o.Kind == "activate" && o.Obj == act.Source })
		if i < 0 {
			x.abort(ReasonActivateNotOffered, fmt.Sprintf("step %d source %d", x.next, act.Source))
			return decision.Intent{}, Aborted
		}
		x.cur = x.next
		x.next++
		for c := range x.expect {
			x.expect[c] += act.Produces[c]
		}
		x.Taps++
		return one(d, i), InProgress
	}
	i := findOption(d, x.Play.Matches)
	if i < 0 {
		if s.Stack > 0 {
			// The play waits for the stack (package doc): pass, keep the pool.
			if x.Waits >= maxWaits {
				x.abort(ReasonWaitExhausted, fmt.Sprintf("%s %d after %d passes", x.Play.Kind, x.Play.Obj, x.Waits))
				return decision.Intent{}, Aborted
			}
			if p := findOption(d, func(o *decision.Option) bool { return o.Kind == "pass" }); p >= 0 {
				x.Waits++
				return one(d, p), InProgress
			}
		}
		x.abort(ReasonCastNotOffered, fmt.Sprintf("%s %d, pool %v, stack %d, last-resort plan %v", x.Play.Kind, x.Play.Obj, s.Pool, s.Stack, HasConsequence(x.Plan)))
		return decision.Intent{}, Aborted
	}
	x.status = Done
	return one(d, i), Done
}

// manaAsk answers a stage-1 "choose a mana ability" wheel or a stage-2
// colour ask for the current source with the option(s) producing exactly
// the witness's Produces.
func (x *Execution) manaAsk(d *decision.Decision) (decision.Intent, Status) {
	act := x.steps[x.cur]
	choices, ok := matchMana(d, act)
	if !ok {
		x.abort(ReasonManaUnmatched, fmt.Sprintf("source %d wants %v: %s", act.Source, act.Produces, d.Prompt))
		return decision.Intent{}, Aborted
	}
	x.Asks++
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}, InProgress
}

func one(d *decision.Decision, i int) decision.Intent {
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}
}

func findOption(d *decision.Decision, pred func(*decision.Option) bool) int {
	for i := range d.Options {
		if pred(&d.Options[i]) {
			return d.Options[i].Index
		}
	}
	return -1
}

// manaAsk reports whether d is a mana ask about source: every option is a
// "mana" option naming it (rules' activateManaFor wheel and askManaColor).
func manaAsk(d *decision.Decision, source state.ObjID) bool {
	if len(d.Options) == 0 {
		return false
	}
	for _, o := range d.Options {
		if o.Kind != "mana" || o.Obj != source {
			return false
		}
	}
	return true
}

const syms = "WUBRGC"

func symIndex(s string) int {
	if len(s) != 1 {
		return -1
	}
	return strings.IndexByte(syms, s[0])
}

func total(m decision.ManaAmount) int {
	n := 0
	for _, v := range m {
		n += int(v)
	}
	return n
}

func singleColour(m decision.ManaAmount) (int, bool) {
	idx := -1
	for i, n := range m {
		if n == 0 {
			continue
		}
		if idx >= 0 {
			return -1, false
		}
		idx = i
	}
	return idx, idx >= 0
}

// matchMana picks the answer to a mana ask that makes act.Produces.
//
// A colour allocation (Min == Max > 1: a Combo ability's per-unit colour
// picks, laid out unit-major) takes, per unit, the option naming the next
// witness colour. A single pick tries, in order: an option whose label's
// "Add ..." tail is exactly the witness production; an option whose
// ManaSymbol is the witness's single colour; an any-colour or "X or Y"
// option that can make it (its colour ask follows). Within a class the
// option whose cost prefix ("Sacrifice ...: Add B") agrees with whether the
// step discloses a Consequence leads, then engine order.
func matchMana(d *decision.Decision, act decision.PaymentActivation) ([]int, bool) {
	want := act.Produces
	if d.Min > 1 && d.Min == d.Max {
		return matchAllocation(d, want)
	}
	col, single := singleColour(want)
	classes := []func(o *decision.Option) bool{
		func(o *decision.Option) bool {
			amt, any, combo, ok := labelProduction(o.Label)
			return ok && !any && combo == nil && amt == want
		},
		func(o *decision.Option) bool {
			if !single || o.ManaSymbol == "" {
				return false
			}
			c := symIndex(o.ManaSymbol)
			return c == col && want[col] == 1
		},
		func(o *decision.Option) bool {
			if !single || o.ManaSymbol != "" {
				return false
			}
			_, any, combo, ok := labelProduction(o.Label)
			if !ok {
				return false
			}
			if any {
				return anyLabelFits(o.Label, want)
			}
			return want[col] == 1 && containsInt(combo, col)
		},
	}
	costly := act.Consequence != nil
	for _, cls := range classes {
		best := -1
		for i := range d.Options {
			o := &d.Options[i]
			if !cls(o) {
				continue
			}
			if best < 0 {
				best = o.Index
			}
			if hasCostPrefix(o.Label) == costly {
				best = o.Index
				break
			}
		}
		if best >= 0 {
			return []int{best}, true
		}
	}
	return nil, false
}

// matchAllocation answers a unit-major colour allocation of Min units.
func matchAllocation(d *decision.Decision, want decision.ManaAmount) ([]int, bool) {
	n := d.Min
	if total(want) != n || len(d.Options)%n != 0 {
		return nil, false
	}
	per := len(d.Options) / n
	var units []int
	for c, k := range want {
		for j := uint32(0); j < k; j++ {
			units = append(units, c)
		}
	}
	choices := make([]int, 0, n)
	for u, c := range units {
		found := -1
		for _, o := range d.Options[u*per : (u+1)*per] {
			if symIndex(o.ManaSymbol) == c {
				found = o.Index
				break
			}
		}
		if found < 0 {
			return nil, false
		}
		choices = append(choices, found)
	}
	return choices, true
}

func hasCostPrefix(label string) bool {
	i := strings.LastIndex(label, "Add ")
	return i > 0 && strings.Contains(label[:i], ":")
}

// labelProduction parses the "Add ..." tail of a rules mana-option label
// (rules/mana_activation.go manaAbilityLabel/askManaColor): an exact pip
// run ("Add W", "Add CC"), an any-colour wording (any), or an "X or Y"
// combo list (combo).
func labelProduction(label string) (amt decision.ManaAmount, any bool, combo []int, ok bool) {
	i := strings.LastIndex(label, "Add ")
	if i < 0 {
		return amt, false, nil, false
	}
	tail := strings.TrimSpace(label[i+len("Add "):])
	if tail == "any color" {
		return amt, true, nil, true
	}
	if _, isAny := labeledAnyCount(tail); isAny {
		return amt, true, nil, true
	}
	if strings.Contains(tail, " or ") {
		for _, part := range strings.FieldsFunc(tail, func(r rune) bool { return r == ',' || r == ' ' }) {
			if c := symIndex(part); c >= 0 {
				combo = append(combo, c)
			}
		}
		return amt, false, combo, len(combo) > 0
	}
	if tail == "" {
		return amt, false, nil, false
	}
	for _, r := range tail {
		c := strings.IndexRune(syms, r)
		if c < 0 {
			return amt, false, nil, false
		}
		amt[c]++
	}
	return amt, false, nil, true
}

var numberWords = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
	"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}

// labeledAnyCount reads "three mana of any one color" (3) and "21 mana in
// any combination of colors" (21).
func labeledAnyCount(tail string) (int, bool) {
	for _, suffix := range []string{" mana of any one color", " mana in any combination of colors"} {
		amount, ok := strings.CutSuffix(tail, suffix)
		if !ok {
			continue
		}
		amount = strings.TrimSpace(amount)
		if n, err := strconv.Atoi(amount); err == nil && n > 0 {
			return n, true
		}
		for i, w := range numberWords {
			if w == amount {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// anyLabelFits: a label naming a literal amount makes exactly that many;
// "Add any color" names none and always fits.
func anyLabelFits(label string, want decision.ManaAmount) bool {
	i := strings.LastIndex(label, "Add ")
	if i < 0 {
		return true
	}
	n, ok := labeledAnyCount(strings.TrimSpace(label[i+len("Add "):]))
	return !ok || n == total(want)
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
