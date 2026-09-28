package v2shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand/v2"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/payexec"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Modes of the shadow policy.
const (
	// ModeTactical answers with sb-tactical on the shadow.
	ModeTactical = "tactical"
	// ModeSearch is sb-search (internal/spellbench/sbsearch) on the shadow:
	// sb-tactical's pick is played unless one of its next-best candidates,
	// rolled out by sb-tactical for both seats in the same redealt worlds of
	// the shadow, scores more than a margin higher.
	ModeSearch = "search"
	// ModeFallback answers every decision with the fallback policy (a
	// control arm: the same agent plumbing without the shadow).
	ModeFallback = "fallback"
	// ModeGeneric is route (a): the hint-free v1 policy (sb-generic)
	// ported to v2 observations, choosing priority plays and combat
	// declarations among the shadow's options (generic.go).
	ModeGeneric = "generic"
)

// Decision classes.
const (
	ClassPriority = "priority"
	ClassAttack   = "attack"
	ClassBlock    = "block"
	ClassChoice   = "choice"
)

// Classify names a v2 decision's class.
func Classify(d *v2agent.Decision) string {
	for i := range d.Candidates {
		switch d.Candidates[i].Semantic.Kind {
		case "declare_attack":
			return ClassAttack
		case "declare_block":
			return ClassBlock
		}
	}
	if d.Seat != nil && d.Seat.Context.Kind == "priority" {
		return ClassPriority
	}
	return ClassChoice
}

// Config configures a Policy.
type Config struct {
	Reg  *cards.Registry
	Mode string
	// Seed is XORed into every game's agent_seed.
	Seed uint64
	// Weights are sb-tactical's weights (zero value: the defaults).
	Weights *builtins.TacticalWeights
	// Fallback answers what the shadow cannot (default: the v2 heuristic).
	Fallback v2agent.Policy
	// Search is ModeSearch's budget (zero Worlds: sbsearch.DefaultConfig).
	Search sbsearch.Config
	// Clock (monotonic ms) and BudgetMS are the search's clock guard: no
	// further world is dealt once BudgetMS has elapsed in a decision (at
	// least MinWorlds are). It is the only clock read, it never fires on
	// a normal position, and every firing is counted (Stats.ClockStops): a
	// game where it fired is not reproducible. Nil Clock or 0 disables it.
	Clock     func() float64
	BudgetMS  float64
	MinWorlds int
	// Trace receives one line per decision when set.
	Trace io.Writer
}

// Stats counts every decision by class and by who answered it; nothing is
// silent.
type Stats struct {
	Decisions int `json:"decisions"`
	// Answered[class][source]: "shadow" (the gorge policy answered the
	// decision itself), "plan" (a follow-up answered from the plan the
	// gorge policy made at its root decision), "single" (one candidate),
	// "fallback".
	Answered map[string]map[string]int `json:"answered"`
	// Reasons counts fallbacks by class and reason.
	Reasons map[string]int `json:"fallback_reasons"`
	// Refusals counts gorge plays with no wire counterpart (re-asked).
	Refusals int `json:"refusals"`
	Panics   int `json:"panics"`
	// Builds counts shadow builds; Lossy their lossy reasons (one count
	// per build per reason).
	Builds int            `json:"builds"`
	Lossy  map[string]int `json:"lossy"`
	// Lowerings of planner-paid casts onto the manual mana surface:
	// started, completed (cast selected), taps answered, and aborts by
	// payexec reason (the decision is then decided afresh).
	Lowerings      int            `json:"lowerings"`
	LoweredCasts   int            `json:"lowered_casts"`
	LoweringTaps   int            `json:"lowering_taps"`
	LoweringAborts map[string]int `json:"lowering_aborts"`
	// Withheld counts shadow priority options with no wire counterpart,
	// withdrawn before the gorge policy chose.
	Withheld int `json:"withheld"`
	// Searched counts the decisions handed to sb-search; ClockStops the
	// decisions whose world loop the clock guard cut.
	Searched   int `json:"searched"`
	ClockStops int `json:"clock_stops"`
}

func newStats() Stats {
	return Stats{Answered: map[string]map[string]int{}, Reasons: map[string]int{}, Lossy: map[string]int{},
		LoweringAborts: map[string]int{}}
}

func (s *Stats) answered(class, src string) {
	m := s.Answered[class]
	if m == nil {
		m = map[string]int{}
		s.Answered[class] = m
	}
	m[src]++
}

// Fallbacks is the number of decisions the fallback answered.
func (s *Stats) Fallbacks() int {
	n := 0
	for _, m := range s.Answered {
		n += m["fallback"]
	}
	return n
}

// Policy is the v2 shadow policy (a v2agent.Policy).
type Policy struct {
	cfg   Config
	fb    v2agent.Policy
	look  builtins.CardLookup
	setup *Setup
	tr    *Tracker
	seat  string
	me    state.PlayerID
	seed  uint64
	gs    *builtins.Seat
	srch  *sbsearch.Seat
	gen   *genericState
	// manual: the engine poses mana abilities (no engine_autopay), so a
	// planner-paid cast is lowered here (low) onto the wire's taps.
	manual bool
	low    *lowering
	plan   *followPlan
	// curDecision is the wire decision being answered (route (a) reads it).
	curDecision *v2agent.Decision
	// t0 is the current decision's start (Clock), clocked the budget.
	t0      float64
	clocked bool
	// Combat plans per wire group.
	atkGroup, blkGroup int64
	atkPlan            map[string]*v2agent.TargetRef // attacker v2 id -> defender
	blkPlan            map[string][]string           // blocker v2 id -> attackers
	blkUsed            map[string]int
	Stats              Stats
	// LastErr is the last setup error (diagnostics).
	LastErr error
}

// New builds a Policy.
func New(cfg Config) (*Policy, error) {
	switch cfg.Mode {
	case ModeTactical, ModeSearch, ModeFallback, ModeGeneric:
	default:
		return nil, fmt.Errorf("v2shadow: unknown mode %q", cfg.Mode)
	}
	if cfg.Reg == nil && cfg.Mode != ModeFallback {
		return nil, fmt.Errorf("v2shadow: no card registry")
	}
	if cfg.Search.Worlds == 0 && cfg.Search.TopK == 0 {
		cfg.Search = sbsearch.DefaultConfig()
	}
	if cfg.MinWorlds <= 0 {
		cfg.MinWorlds = 2
	}
	fb := cfg.Fallback
	if fb == nil {
		fb = &v2agent.Heuristic{}
	}
	p := &Policy{cfg: cfg, fb: fb, Stats: newStats()}
	if cfg.Reg != nil {
		p.look = builtins.NewRegistryLookup(cfg.Reg)
	}
	return p, nil
}

// GameStart implements v2agent.Policy.
func (p *Policy) GameStart(g *v2agent.GameStart) {
	p.fb.GameStart(g)
	p.seat = g.Seat
	p.me = seatID(g.Seat)
	h := fnv.New64a()
	h.Write([]byte(g.GameID))
	h.Write([]byte(g.Seat))
	p.seed = g.Seed() ^ h.Sum64() ^ p.cfg.Seed
	p.plan, p.atkPlan, p.blkPlan, p.low = nil, nil, nil, nil
	p.atkGroup, p.blkGroup = -1, -1
	p.tr = NewTracker()
	p.setup, p.LastErr = nil, nil
	if p.cfg.Mode != ModeFallback {
		if s, err := NewSetup(p.cfg.Reg, g); err != nil {
			p.LastErr = err
		} else {
			p.setup = s
		}
	}
	p.manual = true
	if v := g.EngineProfile.EngineDefaults["mana_payment"]; v != nil && *v == "engine_autopay" {
		p.manual = false
	}
	w := builtins.DefaultTacticalWeights()
	if p.cfg.Weights != nil {
		w = *p.cfg.Weights
	}
	// sb-tactical always plays AutoPay in the shadow: a planner-paid cast
	// is its answer, lowered here onto a manual engine's taps.
	p.gs = builtins.NewTactical(builtins.AutoPay, p.seed^0x7461632d, p.look, w)
	p.srch, p.gen = nil, nil
	switch p.cfg.Mode {
	case ModeSearch:
		p.srch = sbsearch.New(p.gs, p.seed^0x73726368, p.cfg.Search)
	case ModeGeneric:
		p.gen = newGenericState(g)
	}
}

// GameOver implements v2agent.Policy.
func (p *Policy) GameOver(g *v2agent.GameOver) { p.fb.GameOver(g) }

func (p *Policy) decisionSeed(d *v2agent.Decision) uint64 {
	step := uint64(0)
	if d.Seat != nil {
		step = uint64(d.Seat.SeatStep)
	}
	return azmcts.DecisionSeed(p.seed, step)
}

// Choose implements v2agent.Policy. It never returns an error: every
// failure is the fallback's answer, counted.
func (p *Policy) Choose(d *v2agent.Decision) (pick int, err error) {
	p.Stats.Decisions++
	p.curDecision = d
	if p.cfg.Clock != nil {
		p.t0, p.clocked = p.cfg.Clock(), false
	}
	class := Classify(d)
	fbPick, ferr := p.fb.Choose(d)
	if ferr != nil || fbPick < 0 || fbPick >= len(d.Candidates) {
		fbPick = 0
	}
	src, why := "fallback", ""
	defer func() {
		if r := recover(); r != nil {
			p.Stats.Panics++
			pick, src, why = fbPick, "fallback", fmt.Sprintf("panic: %v", r)
			if p.cfg.Trace != nil {
				why += "\n" + string(debug.Stack())
			}
		}
		if pick < 0 || pick >= len(d.Candidates) {
			pick, src, why = fbPick, "fallback", "out of range"
		}
		p.Stats.answered(class, src)
		if src == "fallback" {
			p.Stats.Reasons[class+": "+why]++
			if p.cfg.Trace != nil && class == ClassChoice {
				why += " | " + p.plan.describe() + " | cands:" + candsText(d)
			}
		}
		p.trace(d, class, src, why, pick, fbPick)
	}()
	pick = fbPick
	if p.cfg.Mode == ModeFallback {
		why = "mode"
		return
	}
	if p.setup == nil {
		why = "no setup"
		if p.LastErr != nil {
			why += ": " + p.LastErr.Error()
		}
		return
	}
	var ok bool
	switch class {
	case ClassPriority:
		pick, ok, why = p.priority(d)
		src = "shadow"
	case ClassAttack:
		pick, ok, why = p.attack(d)
		src = "shadow"
	case ClassBlock:
		pick, ok, why = p.block(d)
		src = "shadow"
	default:
		if k, hit := p.plan.answer(d); hit {
			pick, ok, src = k, true, "plan"
		} else if len(d.Candidates) == 1 {
			pick, ok, src = 0, true, "single"
		} else if k, hit, w := p.repose(d); hit {
			pick, ok, src = k, true, "repose"
		} else if k, hit := p.smartChoice(d); hit {
			pick, ok, src, why = k, true, "smart", w
		} else {
			why = w
		}
	}
	if !ok {
		pick, src = fbPick, "fallback"
	}
	return
}

func (p *Policy) trace(d *v2agent.Decision, class, src, why string, pick, fb int) {
	if p.cfg.Trace == nil {
		return
	}
	desc := func(i int) string {
		if i < 0 || i >= len(d.Candidates) {
			return "?"
		}
		return candText(&d.Candidates[i])
	}
	step := int64(0)
	if d.Seat != nil {
		step = d.Seat.SeatStep
	}
	obs := d.Observation()
	fmt.Fprintf(p.cfg.Trace, "%s t%d %s #%d %s %s n=%d pick=[%s] fb=[%s] %s\n", p.seat, obs.Turn, obs.PhaseStep, step, class, src,
		len(d.Candidates), desc(pick), desc(fb), why)
}

func candsText(d *v2agent.Decision) string {
	var out []string
	for i := range d.Candidates {
		out = append(out, "["+candText(&d.Candidates[i])+"]")
	}
	return strings.Join(out, " ")
}

func candText(c *v2agent.Candidate) string {
	s := &c.Semantic
	out := s.Kind
	name := func(r *v2agent.ObjectRef) string {
		if r == nil || r.CardName == nil {
			return "?"
		}
		return *r.CardName
	}
	if s.Source != nil {
		out += " " + name(s.Source)
	}
	if s.Attacker != nil {
		out += " atk=" + name(s.Attacker)
	}
	if s.Blocker != nil {
		out += " blk=" + name(s.Blocker)
	}
	if s.Target != nil {
		if s.Target.Player != nil {
			out += " ->" + *s.Target.Player
		} else if s.Target.Object != nil {
			out += " ->" + name(s.Target.Object)
		}
	}
	if len(s.Choice) > 0 {
		var t v2agent.TargetRef
		if json.Unmarshal(s.Choice, &t) == nil && t.Object != nil {
			out += " choice=" + name(t.Object) + "@" + t.Object.Zone
		}
	}
	if s.Candidate != nil {
		out += " cand=" + name(s.Candidate)
	}
	if s.Card != nil {
		out += " card=" + name(s.Card) + " dest=" + s.Destination
	}
	if s.Item != nil && s.Item.Object != nil {
		out += " item=" + name(s.Item.Object)
	}
	if s.Defender != nil && s.Defender.Player != nil {
		out += " def=" + *s.Defender.Player
	}
	if s.ManaChoice != nil {
		out += " " + *s.ManaChoice
	}
	if len(s.Value) > 0 {
		out += " v=" + string(s.Value)
	}
	if s.Color != "" {
		out += " " + s.Color
	}
	return out
}

// build stages d's observation.
func (p *Policy) build(d *v2agent.Decision, o Options) *Shadow {
	o.Seed = p.decisionSeed(d)
	sh := p.setup.Build(d.Observation(), p.tr, o)
	p.Stats.Builds++
	seen := map[string]bool{} // lookup only
	for _, l := range sh.Lossy {
		k := lossyKey(l)
		if !seen[k] {
			seen[k] = true
			p.Stats.Lossy[k]++
		}
	}
	return sh
}

// lossyKey drops a lossy reason's specifics (quoted names, numbers).
func lossyKey(s string) string {
	if i := strings.IndexAny(s, "\"0123456789"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// gorgeView projects the shadow for the acting seat.
func gorgeView(sh *Shadow, pd *decision.Decision) view.View {
	v := view.Project(sh.E.G, sh.E, sh.Me, pd)
	v.Round = view.RoundOf(sh.E.G, sh.E.L.Events)
	return v
}

// ask asks sb-tactical at the shadow's pending decision.
func (p *Policy) ask(sh *Shadow) (decision.Intent, error) {
	if p.gs.WantsPaymentActions() && sh.E.Pending().Kind == decision.KPriority {
		sh.E.EnsurePaymentActions()
	}
	pd := sh.E.Pending()
	p.gs.SetPlanner(sh.E)
	return p.gs.Decide(context.Background(), gorgeView(sh, pd), *pd)
}

// ---- priority ----

func (p *Policy) priority(d *v2agent.Decision) (int, bool, string) {
	p.plan = nil
	sh := p.build(d, Options{Priority: true})
	if sh.Fatal != "" {
		p.low = nil
		return 0, false, "stage: " + sh.Fatal
	}
	pd := sh.E.Pending()
	if pd.Kind != decision.KPriority {
		p.low = nil
		return 0, false, "staged kind " + string(pd.Kind)
	}
	if p.low != nil {
		if k, why := p.lowerStep(sh, d); k >= 0 {
			return k, true, ""
		} else {
			p.Stats.LoweringAborts[why]++
			p.low = nil
			// The shadow is untouched (lowerStep only reads it): decide
			// afresh on it.
		}
	}
	sh.E.EnsurePaymentActions()
	pd = sh.E.Pending()
	rd := p.reduce(sh, d, pd)
	in, err := p.rootAnswer(sh, rd)
	if err != nil {
		return 0, false, "policy: " + err.Error()
	}
	why := ""
	for tries := 0; tries < 8; tries++ {
		if tries > 0 {
			p.Stats.Refusals++
			p.Stats.Reasons["refusal: "+lossyKey(why)]++
			if p.cfg.Trace != nil {
				fmt.Fprintf(p.cfg.Trace, "  refusal: %s | cands: %s\n", why, candsText(d))
			}
			refused := in
			in = p.gs.Refused(gorgeView(sh, rd), *rd, refused)
			rd = withoutAnswer(rd, refused)
		}
		if sh.StackGap && sorcerySpeed(sh, pd, in) {
			why = "sorcery-speed play over an unstaged stack item"
			continue
		}
		if in.Payment != nil && p.manual {
			k, w := p.startLowering(sh, d, rd, in)
			if k >= 0 {
				return k, true, ""
			}
			why = "lowering: " + w
			continue
		}
		k, kick, w := mapPriority(sh, d, pd, in)
		if k >= 0 {
			p.plan = p.makePlan(sh, pd, in, kick)
			return k, true, ""
		}
		why = w
	}
	return 0, false, "unmapped: " + why
}

// reduce is pd without the options the wire does not offer (an ability
// already used this turn, a play the real stack forbids) and without every
// payment plan that taps a withheld mana source: each would only be
// refused, and a lowering must not start on one. Options keep their Index
// (the builtins look options up by Index).
func (p *Policy) reduce(sh *Shadow, d *v2agent.Decision, pd *decision.Decision) *decision.Decision {
	gone := map[state.ObjID]bool{} // lookup only
	var keep []decision.Option
	for i := range pd.Options {
		o := &pd.Options[i]
		if o.Kind != "pass" && o.Kind != "concede" {
			if k, _, _ := mapOption(sh, d, pd, o); k < 0 {
				p.Stats.Withheld++
				if o.Kind == "activate" {
					gone[o.Obj] = true
				}
				continue
			}
		}
		keep = append(keep, *o)
	}
	if len(keep) == len(pd.Options) {
		return pd
	}
	c := pd.CloneValue()
	c.Options = keep
	if len(gone) > 0 {
		c.PaymentActions = c.PaymentActions[:0]
		for _, a := range pd.PaymentActions {
			a = decision.ClonePaymentAction(a)
			plans := a.Plans[:0]
			for _, pl := range a.Plans {
				ok := true
				for _, act := range pl.Activations {
					if gone[act.Source] {
						ok = false
					}
				}
				if ok {
					plans = append(plans, pl)
				}
			}
			a.Plans = plans
			if len(a.Plans) > 0 {
				c.PaymentActions = append(c.PaymentActions, a)
			}
		}
	}
	return &c
}

// withoutAnswer is d without the option or payment action in names (a
// refused answer stays withdrawn for every later retry).
func withoutAnswer(d *decision.Decision, in decision.Intent) *decision.Decision {
	c := d.CloneValue()
	drop := map[int]bool{} // lookup only
	for _, x := range in.Choices {
		drop[x] = true
	}
	c.Options = c.Options[:0]
	for _, o := range d.Options {
		if !drop[o.Index] || o.Kind == "pass" {
			c.Options = append(c.Options, o)
		}
	}
	if in.Payment != nil {
		c.PaymentActions = c.PaymentActions[:0]
		for _, a := range d.PaymentActions {
			if a.ID != in.Payment.ActionID {
				c.PaymentActions = append(c.PaymentActions, decision.ClonePaymentAction(a))
			}
		}
	}
	return &c
}

// sorcerySpeed reports a play that needs an empty stack: a land, or a cast
// of a card that is neither an instant nor has flash.
func sorcerySpeed(sh *Shadow, pd *decision.Decision, in decision.Intent) bool {
	var obj state.ObjID
	switch {
	case in.Payment != nil:
		for i := range pd.PaymentActions {
			if pd.PaymentActions[i].ID == in.Payment.ActionID {
				obj = pd.PaymentActions[i].Cast.Object
			}
		}
	case len(in.Choices) == 1:
		o, _ := optByIndex(pd, in.Choices[0])
		if o == nil {
			return false
		}
		if o.Kind == "play_land" {
			return true
		}
		if o.Kind != "cast" {
			return false
		}
		obj = o.Obj
	}
	ob := sh.E.G.Obj(obj)
	if ob == nil || ob.Face() == nil {
		return false
	}
	f := ob.Face()
	for _, t := range f.Types {
		if t == "Instant" {
			return false
		}
	}
	for _, k := range f.Keywords {
		if k == "Flash" {
			return false
		}
	}
	return true
}

// rootAnswer is the gorge policy's answer to rd (the shadow's pending root
// decision, priority or attackers, possibly reduced): sb-search when
// searching, else sb-tactical.
func (p *Policy) rootAnswer(sh *Shadow, rd *decision.Decision) (decision.Intent, error) {
	sh.E.EnsurePaymentActions()
	pd := sh.E.Pending()
	if rd == nil {
		rd = pd
	}
	p.gs.SetPlanner(sh.E)
	if p.gen != nil && p.curDecision != nil {
		var in decision.Intent
		var err error
		switch pd.Kind {
		case decision.KPriority:
			in, err = p.genericPriority(sh, p.curDecision, rd)
		case decision.KAttackers:
			in, err = p.genericAttack(sh, p.curDecision, pd)
		case decision.KBlockers:
			in, err = p.genericBlock(sh, p.curDecision, pd)
		default:
			err = fmt.Errorf("kind %s", pd.Kind)
		}
		if err == nil {
			return in, nil
		}
		p.Stats.Reasons["generic: "+lossyKey(err.Error())]++
	}
	if p.srch == nil || (pd.Kind != decision.KPriority && pd.Kind != decision.KAttackers && pd.Kind != decision.KBlockers) {
		return p.gs.Decide(context.Background(), gorgeView(sh, rd), *rd)
	}
	p.Stats.Searched++
	return p.srch.DecideWorlds(context.Background(), sh.E, p.dealer(sh), *rd)
}

// dealer deals the search's worlds from the shadow: a hypothetical clone
// with the opponent's hand and both libraries redealt (Redeal).
func (p *Policy) dealer(sh *Shadow) sbsearch.Dealer {
	return func(w int, seed [2]uint64) (*rules.Engine, string) {
		if p.cfg.Clock != nil && p.cfg.BudgetMS > 0 && w >= p.cfg.MinWorlds && p.cfg.Clock()-p.t0 > p.cfg.BudgetMS {
			if !p.clocked {
				p.clocked = true
				p.Stats.ClockStops++
			}
			return nil, "clock guard"
		}
		world := sh.E.CloneHypothetical(seed[1])
		Redeal(world, sh, rand.New(rand.NewPCG(seed[0], seed[1])))
		return world, ""
	}
}

// lowering is a planner-paid cast being lowered onto the wire's manual
// mana surface: one tap per priority decision, colour asks answered from
// the witness, then the cast (payexec).
type lowering struct {
	x    *payexec.Execution
	turn int32
	step state.Step
}

// startLowering begins lowering the payment answer in and answers d with
// its first step.
func (p *Policy) startLowering(sh *Shadow, d *v2agent.Decision, pd *decision.Decision, in decision.Intent) (int, string) {
	var act *decision.PaymentAction
	for i := range pd.PaymentActions {
		if pd.PaymentActions[i].ID == in.Payment.ActionID {
			act = &pd.PaymentActions[i]
		}
	}
	if act == nil {
		return -1, "payment action not found"
	}
	a := decision.ClonePaymentAction(*act)
	x := payexec.Start(sh.Me, &a, payexec.PoolOf(sh.E, sh.Me))
	if x.Status() != payexec.InProgress {
		return -1, "start: " + x.Reason
	}
	p.Stats.Lowerings++
	p.low = &lowering{x: x, turn: sh.E.G.Turn, step: sh.E.G.Step}
	if p.cfg.Trace != nil {
		name := "?"
		if o := sh.E.G.Obj(a.Cast.Object); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		fmt.Fprintf(p.cfg.Trace, "  lowering %s: %d steps, pool %v\n", name, len(x.Steps()), payexec.PoolOf(sh.E, sh.Me))
	}
	k, why := p.lowerStep(sh, d)
	if k < 0 {
		p.Stats.LoweringAborts[why]++
		p.low = nil
	}
	return k, why
}

// lowerStep answers priority decision d with the lowering's next step (a
// tap, a wait or the cast) mapped onto the wire; -1 aborts it.
func (p *Policy) lowerStep(sh *Shadow, d *v2agent.Decision) (int, string) {
	l := p.low
	if sh.E.G.Turn != l.turn || sh.E.G.Step != l.step {
		return -1, "step_changed"
	}
	pd := sh.E.Pending()
	in, st := l.x.StepOn(pd, payexec.SurfaceOf(sh.E, sh.Me))
	switch st {
	case payexec.Aborted:
		return -1, l.x.Reason
	case payexec.Yield:
		return -1, "yield_at_priority"
	}
	k, ki, why := mapPriority(sh, d, pd, in)
	if k < 0 {
		return -1, "unmapped_step: " + why
	}
	if st == payexec.Done {
		p.Stats.LoweredCasts++
		p.low = nil
		p.plan = p.makePlan(sh, pd, in, ki)
	} else {
		p.Stats.LoweringTaps++
		p.plan = p.makePlan(sh, pd, in, kickInfo{})
	}
	return k, ""
}

// castMethods maps gorge's "cast" option modes to spec 7.4 methods (the
// engine's own table, v2engine translate.go).
var castMethods = map[string]string{
	"": "normal", "mayplay": "normal", "mayflash": "normal", "flashback": "flashback",
	"plot_cast": "plot", "bestowed": "alternative", "surged": "alternative", "blitzed": "alternative",
	"emerged": "alternative", "mutated": "alternative", "escape": "escape", "madness": "madness", "miracle": "miracle",
	"foretell_cast": "foretell", "adventure_alt": "adventure", "split_alt": "split_right", "fuse": "fuse",
	"suspend_cast": "suspend", "modal_spell": "mdfc_back", "evoke": "evoke", "overload": "overload",
	"disturb": "disturb", "disguise": "disguise", "morph": "morph", "prototype": "prototype", "cascade": "cascade",
	"discover": "discover", "rebound": "rebound", "free": "free",
}

// optionalCostModes are the cast modes posed as cast_spell plus an
// optional_cost follow-up.
var optionalCostModes = map[string]bool{"kicked": true, "buyback": true, "entwined": true,
	"conspired": true, "casualty": true, "offspring": true}

// kickInfo is a mapped cast's optional-cost answer.
type kickInfo struct{ has, pay bool }

// mapPriority maps a gorge priority answer onto a wire candidate.
func mapPriority(sh *Shadow, d *v2agent.Decision, pd *decision.Decision, in decision.Intent) (int, kickInfo, string) {
	var ki kickInfo
	if in.Payment != nil || in.Announce != nil {
		id := ""
		if in.Payment != nil {
			id = in.Payment.ActionID
		} else {
			id = in.Announce.ActionID
		}
		for i := range pd.PaymentActions {
			a := &pd.PaymentActions[i]
			if a.ID != id {
				continue
			}
			if a.BaseOptionIndex != nil {
				if o, _ := optByIndex(pd, *a.BaseOptionIndex); o != nil {
					return mapOption(sh, d, pd, o)
				}
			}
			v2, ok := sh.ObjToV2[a.Cast.Object]
			if !ok {
				return -1, ki, "payment object not staged"
			}
			for j := range d.Candidates {
				s := &d.Candidates[j].Semantic
				if s.Kind == "cast_spell" && s.Source != nil && s.Source.ObjectID == v2 {
					ki.has = true
					return j, ki, ""
				}
			}
			return -1, ki, "no cast_spell for the payment"
		}
		return -1, ki, "payment action not found"
	}
	if len(in.Choices) != 1 {
		return -1, ki, fmt.Sprintf("%d choices", len(in.Choices))
	}
	o, _ := optByIndex(pd, in.Choices[0])
	if o == nil {
		return -1, ki, "option not found"
	}
	return mapOption(sh, d, pd, o)
}

func mapOption(sh *Shadow, d *v2agent.Decision, pd *decision.Decision, o *decision.Option) (int, kickInfo, string) {
	var ki kickInfo
	if o.Kind == "pass" {
		for j := range d.Candidates {
			if d.Candidates[j].Semantic.Kind == "pass" {
				return j, ki, ""
			}
		}
		return -1, ki, "no pass"
	}
	v2, ok := sh.ObjToV2[o.Obj]
	if !ok {
		return -1, ki, o.Kind + " source not staged"
	}
	var hits []int
	for j := range d.Candidates {
		s := &d.Candidates[j].Semantic
		if s.Source == nil || s.Source.ObjectID != v2 {
			continue
		}
		switch {
		case o.Kind == "play_land" && s.Kind == "play_land":
			face := uint32(0)
			if o.Mode == "modal_land" {
				face = 1
			}
			if s.Face == face {
				return j, ki, ""
			}
			hits = append(hits, j)
		case o.Kind == "cast" && o.Mode == "plot" && s.Kind == "special_action":
			return j, ki, ""
		case o.Kind == "cast" && s.Kind == "cast_spell":
			m, known := castMethods[o.Mode]
			if optionalCostModes[o.Mode] {
				m, known = "normal", true
				ki = kickInfo{has: true, pay: true}
			} else {
				ki = kickInfo{has: true}
			}
			if !known {
				m = "other"
			}
			if o.Mode == "" && o.AltCostIndex > 0 {
				m = "alternative"
			}
			if s.Method != nil && *s.Method == m {
				return j, ki, ""
			}
			hits = append(hits, j)
		case o.Kind == "activate" && s.Kind == "activate_mana_ability":
			idx := manaAbilityIndex(sh, o)
			sym := ""
			if len(o.ManaSymbol) == 1 && strings.Contains("WUBRGC", o.ManaSymbol) {
				sym = o.ManaSymbol
			}
			got := ""
			if s.ManaChoice != nil {
				got = *s.ManaChoice
			}
			if got == sym && (idx < 0 || int(s.AbilityIndex) == idx) {
				return j, ki, ""
			}
			if got == sym {
				hits = append(hits, j)
			}
		case (o.Kind == "ability" || o.Kind == "granted" || o.Kind == "station") && s.Kind == "activate_ability":
			idx := abilityIndex(sh, o, pd)
			if int(s.AbilityIndex) == idx {
				return j, ki, ""
			}
			hits = append(hits, j)
		case s.Kind == "special_action" && o.Kind != "cast" && o.Kind != "play_land" && o.Kind != "activate" &&
			o.Kind != "ability" && o.Kind != "granted" && o.Kind != "station":
			hits = append(hits, j)
		}
	}
	if len(hits) == 1 {
		return hits[0], ki, ""
	}
	if len(hits) > 1 {
		return hits[0], ki, ""
	}
	return -1, ki, fmt.Sprintf("no %s candidate (%q sym=%q ability=%d)", o.Kind, o.Label, o.ManaSymbol, o.Ability)
}

// manaAbilityIndex is o's index among its face's mana abilities (-1 when
// unknown: granted).
func manaAbilityIndex(sh *Shadow, o *decision.Option) int {
	ob := sh.E.G.Obj(o.Obj)
	if ob == nil {
		return -1
	}
	f := ob.Face()
	if f == nil || o.Ability < 0 || o.Ability >= len(f.Abilities) || f.Abilities[o.Ability].API != "Mana" {
		return -1
	}
	n := 0
	for k := 0; k < o.Ability; k++ {
		if f.Abilities[k].API == "Mana" {
			n++
		}
	}
	return n
}

// abilityIndex is o's index among its face's non-mana activated abilities,
// else its ordinal among the object's ability options.
func abilityIndex(sh *Shadow, o *decision.Option, pd *decision.Decision) int {
	if ob := sh.E.G.Obj(o.Obj); ob != nil && o.Kind == "ability" && o.SVar == "" && o.Keyword == "" {
		if f := ob.Face(); f != nil && o.Ability >= 0 && o.Ability < len(f.Abilities) {
			n := 0
			for k := 0; k < o.Ability; k++ {
				if f.Abilities[k].Kind == "AB" && f.Abilities[k].API != "Mana" {
					n++
				}
			}
			return n
		}
	}
	n := 0
	for i := range pd.Options {
		q := &pd.Options[i]
		if q.Index == o.Index {
			break
		}
		if q.Obj == o.Obj && (q.Kind == "ability" || q.Kind == "granted" || q.Kind == "station") {
			n++
		}
	}
	return n
}

// makePlan submits in to the shadow and plays on (runFollowUps),
// recording the gorge policy's answers to our own follow-up decisions.
func (p *Policy) makePlan(sh *Shadow, pd *decision.Decision, in decision.Intent, ki kickInfo) *followPlan {
	fp := &followPlan{kicker: ki.pay, hasKicker: ki.has}
	ok := func() (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				ok = false
			}
		}()
		return sh.E.Submit(in) == nil
	}()
	if ok {
		p.runFollowUps(sh, fp)
	}
	if len(fp.steps) == 0 && !fp.hasKicker {
		return nil
	}
	return fp
}

// runFollowUps plays the shadow forward from its current state: our own
// decisions are answered by sb-tactical (a lowering's colour asks by the
// lowering) and recorded, the opponent's priority is passed (the likeliest
// response), until our next priority, attack or block decision, another
// opponent decision, a turn change or the step cap. Decisions in a later
// step than the root are answered by a rollout copy of the seat, so the
// real seat's step-scoped state (its pursuit, its lowering) is untouched.
func (p *Policy) runFollowUps(sh *Shadow, fp *followPlan) {
	e := sh.E
	turn, step := e.G.Turn, e.G.Step
	var clone *builtins.Seat
	defer func() { _ = recover() }()
	for steps := 0; steps < 48 && !e.G.Over; steps++ {
		nd := e.Pending()
		if nd == nil || e.G.Turn != turn {
			return
		}
		if nd.Player != sh.Me {
			if nd.Kind != decision.KPriority {
				return
			}
			pass := decision.Intent{Seq: nd.Seq, Player: nd.Player}
			for _, o := range nd.Options {
				if o.Kind == "pass" {
					pass.Choices = []int{o.Index}
					break
				}
			}
			if len(pass.Choices) == 0 || e.Submit(pass) != nil {
				return
			}
			continue
		}
		if (nd.Kind == decision.KPriority && nd.ManaPayment == nil) || nd.Kind == decision.KAttackers || nd.Kind == decision.KBlockers {
			return
		}
		var ans decision.Intent
		answered := false
		if p.low != nil && nd.Kind != decision.KPriority {
			a, st := p.low.x.StepOn(nd, payexec.SurfaceOf(e, sh.Me))
			switch st {
			case payexec.InProgress, payexec.Done:
				ans, answered = a, true
			case payexec.Aborted:
				p.Stats.LoweringAborts[p.low.x.Reason]++
				p.low = nil
			}
		}
		if !answered {
			seat := p.gs
			if e.G.Step != step {
				if clone == nil {
					clone = p.gs.RolloutClone(p.seed^uint64(steps+1)*0x9e37, nil, true)
				}
				seat = clone
			}
			if seat.WantsPaymentActions() && nd.Kind == decision.KPriority {
				e.EnsurePaymentActions()
				nd = e.Pending()
			}
			seat.SetPlanner(e)
			a, err := seat.Decide(context.Background(), gorgeView(sh, nd), *nd)
			if err != nil {
				return
			}
			ans = a
		}
		fp.record(sh, nd, ans)
		if err := e.Submit(ans); err != nil {
			return
		}
	}
}

// repose re-poses a choice decision the plan does not answer on a fresh
// shadow: the observation is staged and the engine advanced, passing
// priority for either seat, until it poses a decision of ours of the same
// family (a spell or ability resolving, the cleanup discard); sb-tactical
// answers it and the plan is recorded from there.
func (p *Policy) repose(d *v2agent.Decision) (int, bool, string) {
	sh := p.build(d, Options{})
	if sh.Fatal != "" && !strings.HasPrefix(sh.Fatal, "pending decision is the opponent's") {
		return 0, false, "repose stage: " + sh.Fatal
	}
	e := sh.E
	wk := wireKinds(d)
	turn := e.G.Turn
	for i := 0; i < 8 && !e.G.Over && e.G.Turn == turn; i++ {
		nd := e.Pending()
		if nd == nil {
			return 0, false, "repose: no decision"
		}
		if nd.Player == sh.Me && familyMatches(wk, nd) && sourceMatches(sh, d, nd) {
			fp := &followPlan{}
			p.runFollowUps(sh, fp)
			if k, ok := fp.answer(d); ok {
				p.plan = fp
				return k, true, ""
			}
			if p.cfg.Trace != nil {
				fmt.Fprintf(p.cfg.Trace, "  repose plan: %s\n", fp.describe())
				var kn, top []string
				for _, k := range d.Observation().Known {
					pos := -1
					if k.PositionFromTop != nil {
						pos = int(*k.PositionFromTop)
					}
					kn = append(kn, fmt.Sprintf("%s/%s/%s@%d", k.OwnerSeat, k.Zone, k.CardName, pos))
				}
				lib := sh.MyLib
				for i := 0; i < len(lib) && i < 6; i++ {
					if o := e.G.Obj(lib[i]); o != nil && o.Face() != nil {
						top = append(top, o.Face().Name)
					}
				}
				fmt.Fprintf(p.cfg.Trace, "  known %v mylib-top %v\n", kn, top)
			}
			return 0, false, "repose: answer unmatched (" + string(nd.Kind) + ")"
		}
		if nd.Kind != decision.KPriority {
			return 0, false, "repose: posed " + string(nd.Kind)
		}
		pass := decision.Intent{Seq: nd.Seq, Player: nd.Player}
		for _, o := range nd.Options {
			if o.Kind == "pass" {
				pass.Choices = []int{o.Index}
				break
			}
		}
		if len(pass.Choices) == 0 || e.Submit(pass) != nil {
			return 0, false, "repose: pass refused"
		}
	}
	return 0, false, "repose: not reached"
}

func wireKinds(d *v2agent.Decision) map[string]bool {
	k := map[string]bool{} // lookup only
	for i := range d.Candidates {
		k[d.Candidates[i].Semantic.Kind] = true
	}
	return k
}

// familyMatches reports whether gorge decision nd can be the physical
// decision behind a wire decision with candidate kinds wk.
func familyMatches(wk map[string]bool, nd *decision.Decision) bool {
	switch {
	case wk["choose_target"] || wk["finish_target_selection"]:
		return nd.Kind == decision.KTarget
	case wk["arrange_card"]:
		return nd.Kind == decision.KArrange
	case wk["order_pick"]:
		return nd.Kind == decision.KTriggerOrder || nd.Kind == decision.KArrange
	case wk["select_object"] || wk["finish_selection"] || wk["choose_cost_target"]:
		return nd.Kind == decision.KChoose || nd.Kind == decision.KModes
	case wk["choose_spell_mode"] || wk["optional_cost"]:
		return nd.Kind == decision.KModes
	case wk["choose_boolean"]:
		return nd.Kind == decision.KChoose || nd.Kind == decision.KTriggerOptional || nd.Kind == decision.KReplacement
	case wk["choose_number"] || wk["choose_color"] || wk["choose_name"]:
		return nd.Kind == decision.KChoose || nd.Kind == decision.KReplacement
	case wk["choose_option"]:
		return nd.Kind != decision.KPriority && nd.Kind != decision.KAttackers && nd.Kind != decision.KBlockers
	}
	return false
}

// sourceMatches reports whether the wire decision's source (context or
// candidate) names the same card as nd's source, when both are known.
func sourceMatches(sh *Shadow, d *v2agent.Decision, nd *decision.Decision) bool {
	want := ""
	if d.Seat != nil && d.Seat.Context.Source != nil && d.Seat.Context.Source.CardName != nil {
		want = *d.Seat.Context.Source.CardName
	}
	for i := 0; want == "" && i < len(d.Candidates); i++ {
		if s := d.Candidates[i].Semantic.Source; s != nil && s.CardName != nil {
			want = *s.CardName
		}
	}
	if want == "" || nd.Source == 0 {
		return true
	}
	o := sh.E.G.Obj(nd.Source)
	for o != nil && o.Face() == nil && o.Source != 0 {
		o = sh.E.G.Obj(o.Source)
	}
	if o == nil || o.Face() == nil {
		return true
	}
	if fold(o.Face().Name) == fold(want) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f != nil && fold(f.Name) == fold(want) {
				return true
			}
		}
	}
	return false
}

// smartChoice answers a few choice families the shadow cannot re-pose,
// from the wire alone: a mana colour (the lowering's witness when one is
// running, else the colour our hand wants most).
func (p *Policy) smartChoice(d *v2agent.Decision) (int, bool) {
	if k, ok := smartTarget(d); ok {
		return k, true
	}
	if k, ok := smartDiscard(d); ok {
		return k, true
	}
	var colours []int
	for i := range d.Candidates {
		if d.Candidates[i].Semantic.Kind == "choose_color" {
			colours = append(colours, i)
		}
	}
	if len(colours) == 0 || len(colours) != len(d.Candidates) {
		return 0, false
	}
	if p.low != nil && p.low.x.Taps > 0 && p.low.x.Taps <= len(p.low.x.Steps()) {
		prod := p.low.x.Steps()[p.low.x.Taps-1].Produces
		for _, i := range colours {
			if c := colourIndex(d.Candidates[i].Semantic.Color); c >= 0 && prod[c] > 0 {
				return i, true
			}
		}
	}
	want := map[string]int{} // lookup only
	if me := d.Observation().Me(); me != nil {
		for _, h := range me.Hand {
			if h.Characteristics == nil || h.Characteristics.HasType("land") {
				continue
			}
			for _, c := range h.Characteristics.Colors {
				want[c]++
			}
		}
	}
	best := colours[0]
	for _, i := range colours {
		if want[d.Candidates[i].Semantic.Color] > want[d.Candidates[best].Semantic.Color] {
			best = i
		}
	}
	return best, true
}

// smartTarget answers a choose_target the shadow could not re-pose (its
// source is off the stack: a dungeon room, a rules-level trigger) without
// knowing the effect: a player target is the opponent; among objects, our
// own best creature when one is offered (the rooms and riders that target
// "a creature" are mostly beneficial), else the opponent's best.
func smartTarget(d *v2agent.Decision) (int, bool) {
	obs := d.Observation()
	best, bestV := -1, -1.0
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		if s.Kind != "choose_target" || s.Target == nil {
			continue
		}
		v := 0.0
		switch {
		case s.Target.Player != nil:
			if *s.Target.Player != obs.Viewer {
				v = 50
			}
		case s.Target.Object != nil:
			r := obs.Object(s.Target.Object.ObjectID)
			if r == nil {
				continue
			}
			v = 1 + objValue(r)
			if r.ControllerSeat == obs.Viewer {
				v += 100
			}
		}
		if v > bestV {
			best, bestV = i, v
		}
	}
	return best, best >= 0
}

// objValue is a rough value of an object: power plus toughness for a
// creature, its mana value otherwise.
func objValue(r *v2agent.ObjectRecord) float64 {
	c := r.Characteristics
	if c == nil {
		return 0
	}
	if c.Power != nil && c.Toughness != nil {
		return float64(*c.Power) + float64(*c.Toughness)
	}
	return float64(c.ManaValue) / 2
}

// smartDiscard answers a discard selection the shadow could not re-pose:
// a land when we have plenty of them (on the battlefield and in hand),
// else the most expensive card we cannot cast soon.
func smartDiscard(d *v2agent.Decision) (int, bool) {
	obs := d.Observation()
	me := obs.Me()
	if me == nil || d.Seat == nil || d.Seat.Context.Purpose == nil || *d.Seat.Context.Purpose != "discard" {
		return 0, false
	}
	lands := 0
	for _, r := range me.Battlefield {
		if r.Characteristics != nil && r.Characteristics.HasType("land") {
			lands++
		}
	}
	handLands := 0
	for _, r := range me.Hand {
		if r.Characteristics != nil && r.Characteristics.HasType("land") {
			handLands++
		}
	}
	best, bestV := -1, -1e9
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		if s.Kind != "select_object" || len(s.Choice) == 0 {
			continue
		}
		var t v2agent.TargetRef
		if json.Unmarshal(s.Choice, &t) != nil || t.Object == nil {
			continue
		}
		r := obs.Object(t.Object.ObjectID)
		if r == nil || r.Characteristics == nil {
			continue
		}
		v := 0.0
		if r.Characteristics.HasType("land") {
			if lands+handLands >= 5 {
				v = 10
			} else {
				v = -10
			}
		} else {
			v = float64(r.Characteristics.ManaValue) - float64(lands)
		}
		if v > bestV {
			best, bestV = i, v
		}
	}
	return best, best >= 0
}

// colourIndex is a colour word's index in decision.ManaAmount (W U B R G C).
func colourIndex(c string) int {
	switch c {
	case "white":
		return 0
	case "blue":
		return 1
	case "black":
		return 2
	case "red":
		return 3
	case "green":
		return 4
	}
	return -1
}

// ---- combat ----

func (p *Policy) attack(d *v2agent.Decision) (int, bool, string) {
	gid := d.Seat.Group.GroupID
	if gid != p.atkGroup || p.atkPlan == nil {
		p.atkGroup, p.atkPlan = gid, nil
		p.plan = nil
		sh := p.build(d, Options{})
		if sh.Fatal != "" {
			return 0, false, "stage: " + sh.Fatal
		}
		pd := sh.E.Pending()
		if pd.Kind != decision.KAttackers {
			return 0, false, "staged kind " + string(pd.Kind)
		}
		in, err := p.rootAnswer(sh, nil)
		if err != nil {
			return 0, false, "policy: " + err.Error()
		}
		plan := map[string]*v2agent.TargetRef{}
		for _, c := range in.Choices {
			o, _ := optByIndex(pd, c)
			if o == nil {
				continue
			}
			v2, ok := sh.ObjToV2[o.Obj]
			if !ok {
				continue
			}
			if o.Battle != 0 {
				if bv, ok := sh.ObjToV2[o.Battle]; ok {
					plan[v2] = &v2agent.TargetRef{Object: &v2agent.ObjectRef{ObjectID: bv}}
				}
				continue
			}
			s := SeatName(o.Player)
			plan[v2] = &v2agent.TargetRef{Player: &s}
		}
		p.atkPlan = plan
	}
	var attacker string
	for i := range d.Candidates {
		if a := d.Candidates[i].Semantic.Attacker; a != nil {
			attacker = a.ObjectID
			break
		}
	}
	want := p.atkPlan[attacker]
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		if s.Kind != "declare_attack" {
			continue
		}
		switch {
		case want == nil && s.Defender == nil:
			return i, true, ""
		case want != nil && s.Defender != nil && want.Player != nil && s.Defender.Player != nil && *want.Player == *s.Defender.Player:
			return i, true, ""
		case want != nil && s.Defender != nil && want.Object != nil && s.Defender.Object != nil && want.Object.ObjectID == s.Defender.Object.ObjectID:
			return i, true, ""
		}
	}
	return 0, false, "attack substep unmatched"
}

func (p *Policy) block(d *v2agent.Decision) (int, bool, string) {
	gid := d.Seat.Group.GroupID
	if gid != p.blkGroup || p.blkPlan == nil {
		p.blkGroup, p.blkPlan, p.blkUsed = gid, nil, map[string]int{}
		p.plan = nil
		sh := p.build(d, Options{})
		if sh.Fatal != "" {
			return 0, false, "stage: " + sh.Fatal
		}
		pd := sh.E.Pending()
		if pd.Kind != decision.KBlockers {
			return 0, false, "staged kind " + string(pd.Kind)
		}
		in, err := p.rootAnswer(sh, nil)
		if err != nil {
			return 0, false, "policy: " + err.Error()
		}
		plan := map[string][]string{}
		for _, c := range in.Choices {
			o, _ := optByIndex(pd, c)
			if o == nil {
				continue
			}
			b, ok1 := sh.ObjToV2[o.Obj]
			a, ok2 := sh.ObjToV2[o.Attacker]
			if ok1 && ok2 {
				plan[b] = append(plan[b], a)
			}
		}
		p.blkPlan = plan
	}
	var blocker string
	for i := range d.Candidates {
		if b := d.Candidates[i].Semantic.Blocker; b != nil {
			blocker = b.ObjectID
			break
		}
	}
	wants := p.blkPlan[blocker]
	used := p.blkUsed[blocker]
	if used < len(wants) {
		for i := range d.Candidates {
			s := &d.Candidates[i].Semantic
			if s.Kind == "declare_block" && s.Attacker != nil && s.Attacker.ObjectID == wants[used] {
				p.blkUsed[blocker]++
				return i, true, ""
			}
		}
	}
	for i := range d.Candidates {
		s := &d.Candidates[i].Semantic
		if s.Kind == "declare_block" && s.Attacker == nil {
			return i, true, ""
		}
	}
	if used < len(wants) {
		return 0, false, "planned block not offered"
	}
	return 0, false, "block substep unmatched"
}

// Summary renders the stats as one JSON-able map (the -stats line).
func (p *Policy) Summary() map[string]any {
	reasons := make([]string, 0, len(p.Stats.Reasons))
	for k := range p.Stats.Reasons {
		reasons = append(reasons, k)
	}
	sort.Strings(reasons)
	out := map[string]any{"shadow": p.Stats, "mode": p.cfg.Mode}
	if p.gs != nil {
		out["tactical"] = p.gs.Stats
	}
	return out
}
