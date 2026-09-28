package v2shadow

import (
	"context"
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
	// manual: the engine poses mana abilities (no engine_autopay), so a
	// planner-paid cast is lowered here (low) onto the wire's taps.
	manual bool
	low    *lowering
	plan   *followPlan
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
	case ModeTactical, ModeSearch, ModeFallback:
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
	p.srch = nil
	if p.cfg.Mode == ModeSearch {
		p.srch = sbsearch.New(p.gs, p.seed^0x73726368, p.cfg.Search)
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
		} else {
			why = "no plan"
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
	in, err := p.rootAnswer(sh)
	if err != nil {
		return 0, false, "policy: " + err.Error()
	}
	pd = sh.E.Pending()
	why := ""
	for tries := 0; tries < 8; tries++ {
		if tries > 0 {
			p.Stats.Refusals++
			in = p.gs.Refused(gorgeView(sh, pd), *pd, in)
		}
		if in.Payment != nil && p.manual {
			k, w := p.startLowering(sh, d, pd, in)
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

// rootAnswer is the gorge policy's answer at the shadow's pending root
// decision (priority or attackers): sb-search when searching, else
// sb-tactical.
func (p *Policy) rootAnswer(sh *Shadow) (decision.Intent, error) {
	pd := sh.E.Pending()
	if p.srch == nil || (pd.Kind != decision.KPriority && pd.Kind != decision.KAttackers) {
		return p.ask(sh)
	}
	sh.E.EnsurePaymentActions()
	pd = sh.E.Pending()
	p.gs.SetPlanner(sh.E)
	p.Stats.Searched++
	return p.srch.DecideWorlds(context.Background(), sh.E, p.dealer(sh), *pd)
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
		return -1, "unmapped_step"
	}
	_ = why
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
	return -1, ki, "no " + o.Kind + " candidate"
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

// makePlan submits in to the shadow and answers its own follow-up
// decisions with sb-tactical until priority (or another seat's decision)
// comes back, recording each answer.
func (p *Policy) makePlan(sh *Shadow, pd *decision.Decision, in decision.Intent, ki kickInfo) *followPlan {
	fp := &followPlan{kicker: ki.pay, hasKicker: ki.has}
	if len(in.Choices) == 1 {
		if o, _ := optByIndex(pd, in.Choices[0]); o != nil && (o.Kind == "pass" || o.Kind == "play_land") {
			return nil
		}
	}
	e := sh.E
	func() {
		defer func() { _ = recover() }()
		if err := e.Submit(in); err != nil {
			return
		}
		for steps := 0; steps < 32 && !e.G.Over; steps++ {
			nd := e.Pending()
			if nd == nil || nd.Player != sh.Me {
				return
			}
			if nd.Kind == decision.KPriority && nd.ManaPayment == nil {
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
				a, err := p.ask(sh)
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
	}()
	if len(fp.steps) == 0 && !fp.hasKicker {
		return nil
	}
	return fp
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
		in, err := p.rootAnswer(sh)
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
		in, err := p.ask(sh)
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
