package kshadow

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Modes of the shadow policy.
const (
	// ModeTactical answers with sb-tactical (gorge-native, AutoPay) on the
	// shadow.
	ModeTactical = "tactical"
	// ModeAZ is azmcts.Search over redealt shadow worlds, the gorge bot's
	// answer as candidate 0 (az-redeal on the shadow).
	ModeAZ = "az"
	// ModeAZTac is ModeAZ with v1agent.Tactical's own (kernel-native)
	// answer, mapped into the shadow, as candidate 0.
	ModeAZTac = "aztac"
	// ModeRoll is the rollout search (RollConfig) over the kernel's own
	// candidates, v1agent.Tactical's pick as the default that the search
	// overrides only past a margin.
	ModeRoll = "roll"
)

// Config configures a Policy.
type Config struct {
	Reg  *cards.Registry
	Mode string
	// Sims and Worlds are the search budget (az modes): simulations per
	// searched decision and the number of dealt worlds (0: one per sim).
	Sims, Worlds int
	// Kinds are the searched kinds (az modes; zero value: all).
	Kinds azmcts.Kinds
	// Seed is the agent seed (every decision seed derives from it and the
	// game's own identity).
	Seed uint64
	// Trace receives one line per decision when set.
	Trace io.Writer
	// Fallback overrides the fallback policy (default v1agent.Tactical).
	Fallback v1agent.Policy
	// Roll is ModeRoll's budget.
	Roll RollConfig
}

// Stats counts every decision by class and by who answered it; nothing is
// silent.
type Stats struct {
	Decisions int
	// Answered[class][source]: source is "gorge" (the shadow policy
	// answered the decision itself), "plan" (a follow-up answered from the
	// plan the shadow policy made at its root decision) or "fallback".
	Answered map[string]map[string]int
	// Fallback reasons.
	Reasons map[string]int
	// Agree counts gorge answers equal to the fallback's (by class).
	Agree map[string]int
	// Build/decide wall time is measured by the caller (the agent binary);
	// here only counts.
	Panics int
}

func newStats() Stats {
	return Stats{Answered: map[string]map[string]int{}, Reasons: map[string]int{}, Agree: map[string]int{}}
}

func (s *Stats) answered(class, src string) {
	m := s.Answered[class]
	if m == nil {
		m = map[string]int{}
		s.Answered[class] = m
	}
	m[src]++
}

// Policy is the kernel-shadow v1 policy.
type Policy struct {
	cfg   Config
	fb    v1agent.Policy
	setup *Setup
	seat  string
	game  string
	me    state.PlayerID
	gs    *builtins.Seat
	// plan holds follow-up answers the shadow policy chose at its last
	// root decision (targets, X, modes), keyed by the source's arena id.
	plan *followPlan
	// Combat plans, per kernel decision group.
	attackGroup int64
	attackPlan  map[uint32]bool
	blockGroup  int64
	blockPlan   map[[2]uint32]bool
	seq         uint64
	assigned    map[uint32]state.ObjID
	Stats       Stats
	Roll        RollStats
	look        builtins.CardLookup
	lastRoll    string
	// LastErr is the last setup error (diagnostics).
	LastErr error
}

// New builds a Policy.
func New(cfg Config) (*Policy, error) {
	switch cfg.Mode {
	case ModeTactical, ModeAZ, ModeAZTac, ModeRoll:
	default:
		return nil, fmt.Errorf("kshadow: unknown mode %q", cfg.Mode)
	}
	if cfg.Reg == nil {
		return nil, fmt.Errorf("kshadow: no card registry")
	}
	if cfg.Roll.Worlds == 0 {
		cfg.Roll = DefaultRoll()
	}
	if cfg.Kinds == (azmcts.Kinds{}) {
		cfg.Kinds = azmcts.AllKinds()
	}
	fb := cfg.Fallback
	if fb == nil {
		fb = v1agent.NewTactical(v1agent.TacticalOptions{})
	}
	return &Policy{cfg: cfg, fb: fb, Stats: newStats(), attackGroup: -1, blockGroup: -1}, nil
}

// GameStart implements v1agent.Policy.
func (p *Policy) GameStart(g *v1agent.GameStart) {
	p.fb.GameStart(g)
	p.seat, p.game = g.Seat, g.GameID
	p.me = seatID(g.Seat)
	p.plan, p.attackPlan, p.blockPlan = nil, nil, nil
	p.attackGroup, p.blockGroup = -1, -1
	p.seq = 0
	p.setup = nil
	if len(g.CatalogIDs) == 2 && g.CatalogIDs[0] != "" && g.CatalogIDs[1] != "" {
		s, err := NewSetup(p.cfg.Reg, [2]string{g.CatalogIDs[0], g.CatalogIDs[1]})
		if err != nil {
			p.LastErr = err
		} else {
			p.setup = s
		}
	}
	p.gs = builtins.NewTactical(builtins.AutoPay, p.gameSeed()^0x7461632d, builtins.NewRegistryLookup(p.cfg.Reg), builtins.DefaultTacticalWeights())
}

// GameOver implements v1agent.Policy.
func (p *Policy) GameOver(t *v1agent.Terminal) { p.fb.GameOver(t) }

func (p *Policy) gameSeed() uint64 {
	h := fnv.New64a()
	h.Write([]byte(p.game))
	h.Write([]byte(p.seat))
	return h.Sum64() ^ p.cfg.Seed
}

// Choose implements v1agent.Policy.
func (p *Policy) Choose(d *v1agent.Decision) (pick int) {
	p.Stats.Decisions++
	p.seq++
	fbPick := p.fb.Choose(d)
	class := Classify(d)
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
		} else if pick == fbPick {
			p.Stats.Agree[class]++
		}
		p.trace(d, class, src, why, pick, fbPick)
	}()
	pick = fbPick
	if len(d.Candidates) == 1 {
		why = "single candidate"
		return
	}
	if d.Kernel == nil {
		why = "no x_kernel_v5"
		return
	}
	if p.setup == nil {
		why = "no setup"
		return
	}
	var ok bool
	switch class {
	case ClassAttack:
		pick, ok, why = p.attack(d)
	case ClassBlock:
		pick, ok, why = p.block(d)
	case ClassPriority:
		pick, ok, why = p.priority(d, fbPick)
	default:
		pick, ok, why = p.follow(d)
		if ok {
			src = "plan"
		}
		if !ok {
			pick = fbPick
		}
		return
	}
	if !ok {
		pick = fbPick
		return
	}
	src = "gorge"
	return
}

func (p *Policy) trace(d *v1agent.Decision, class, src, why string, pick, fb int) {
	if p.cfg.Trace == nil {
		return
	}
	desc := func(i int) string {
		if i < 0 || i >= len(d.Candidates) {
			return "?"
		}
		c := &d.Candidates[i]
		s := c.Kind()
		if o := c.Semantic.Source(); o != nil {
			s += " " + o.Name()
		}
		for _, f := range []string{"attacker", "blocker"} {
			if o := c.Semantic.Obj(f); o != nil {
				s += " " + f + "=" + o.Name()
			}
		}
		if t := c.Semantic.Target("target"); t != nil {
			if t.Player != nil {
				s += " ->" + *t.Player
			} else if t.Object != nil {
				s += " ->" + t.Object.Name()
			}
		}
		if raw, ok := c.Semantic.Fields["include"]; ok {
			s += " include=" + string(raw)
		}
		return s
	}
	fmt.Fprintf(p.cfg.Trace, "%s %s #%d %s %s pick=[%s] fb=[%s] %s%s\n", p.game, p.seat, d.Step, class, src, desc(pick), desc(fb), why, p.lastRoll)
	p.lastRoll = ""
}

// decisionSeed derives this decision's seed.
func (p *Policy) decisionSeed(d *v1agent.Decision) uint64 {
	return azmcts.DecisionSeed(p.gameSeed(), uint64(d.Step))
}

// build stages the decision's observation.
func (p *Policy) build(d *v1agent.Decision) *Shadow {
	return p.setup.Build(&d.Kernel.Obs, Options{Seed: p.decisionSeed(d), Priority: Classify(d) == ClassPriority})
}

// throughMana plays a gorge answer that activates a mana ability (a
// pursuit of a play gorge offers only once mana floats) on the shadow and
// asks again, until the answer is a real play: the kernel pays mana
// automatically, so the play is what maps. ask answers the shadow's
// pending decision.
func throughMana(sh *Shadow, in decision.Intent, ask func() (decision.Intent, error)) (decision.Intent, *decision.Decision, error) {
	pd := sh.E.Pending()
	for n := 0; n < 16; n++ {
		if in.Payment != nil || in.Announce != nil || len(in.Choices) != 1 {
			return in, pd, nil
		}
		o := optionByIndex(pd, in.Choices[0])
		if o == nil || !isManaOption(o) {
			return in, pd, nil
		}
		if err := sh.E.Submit(in); err != nil {
			return in, pd, err
		}
		nd := sh.E.Pending()
		if nd == nil || nd.Player != sh.Me {
			return in, pd, fmt.Errorf("mana activation left no decision of ours")
		}
		var err error
		if in, err = ask(); err != nil {
			return in, pd, err
		}
		pd = sh.E.Pending()
		if pd.Kind != decision.KPriority {
			// A mana ability's own choice (a colour): keep answering.
			if err := sh.E.Submit(in); err != nil {
				return in, pd, err
			}
			if in, err = ask(); err != nil {
				return in, pd, err
			}
			pd = sh.E.Pending()
		}
	}
	return in, pd, fmt.Errorf("mana pursuit did not end")
}

// gorgeView projects the shadow for the acting seat.
func gorgeView(sh *Shadow, pd *decision.Decision) view.View {
	v := view.Project(sh.E.G, sh.E, sh.Me, pd)
	v.Round = view.RoundOf(sh.E.G, sh.E.L.Events)
	return v
}

// tacticalAnswer asks sb-tactical at the shadow's pending decision.
func (p *Policy) tacticalAnswer(sh *Shadow) (decision.Intent, error) {
	if p.gs.WantsPaymentActions() && sh.E.Pending().Kind == decision.KPriority {
		sh.E.EnsurePaymentActions()
	}
	pd := sh.E.Pending()
	p.gs.SetPlanner(sh.E)
	return p.gs.Decide(context.Background(), gorgeView(sh, pd), *pd)
}

// ---- priority ----

func (p *Policy) priority(d *v1agent.Decision, fbPick int) (int, bool, string) {
	p.plan = nil
	sh := p.build(d)
	if sh.Fatal != "" {
		return 0, false, "stage: " + sh.Fatal
	}
	pd := sh.E.Pending()
	if pd.Kind != decision.KPriority {
		return 0, false, "staged kind " + string(pd.Kind)
	}
	if p.cfg.Mode == ModeRoll {
		return p.rollPriority(sh, d, fbPick)
	}
	var in decision.Intent
	var err error
	switch p.cfg.Mode {
	case ModeTactical:
		in, err = p.tacticalAnswer(sh)
		if err == nil {
			in, pd, err = throughMana(sh, in, func() (decision.Intent, error) { return p.tacticalAnswer(sh) })
		}
	default:
		in, err = p.search(sh, d, fbPick)
	}
	if err != nil {
		return 0, false, "policy: " + err.Error()
	}
	k, why := kernelIndexPriority(sh, d, pd, in)
	if k < 0 {
		return 0, false, "unmapped: " + why
	}
	// Plan the follow-ups (targets, X, modes) the kernel will ask next.
	p.plan = p.makePlan(sh, pd, in, p.decisionSeed(d))
	return k, true, ""
}

// kernelIndexPriority maps a gorge priority answer onto a kernel candidate.
func kernelIndexPriority(sh *Shadow, d *v1agent.Decision, pd *decision.Decision, in decision.Intent) (int, string) {
	var opt *decision.Option
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
				opt = optionByIndex(pd, *a.BaseOptionIndex)
			}
			if opt == nil {
				// A payment action for a cast: map by object.
				arena, ok := sh.arenaOf(a.Cast.Object)
				if !ok {
					return -1, "payment object not staged"
				}
				return candidateFor(d, "cast_spell", arena, -1)
			}
		}
		if opt == nil {
			return -1, "payment action not found"
		}
	} else {
		if len(in.Choices) != 1 {
			return -1, fmt.Sprintf("%d choices", len(in.Choices))
		}
		opt = optionByIndex(pd, in.Choices[0])
		if opt == nil {
			return -1, "option not found"
		}
	}
	switch {
	case opt.Kind == "pass":
		return candidateFor(d, "pass", 0, -1)
	case opt.Kind == "play_land":
		a, ok := sh.arenaOf(opt.Obj)
		if !ok {
			return -1, "land not staged"
		}
		return candidateFor(d, "play_land", a, -1)
	case opt.Kind == "cast":
		a, ok := sh.arenaOf(opt.Obj)
		if !ok {
			return -1, "spell not staged"
		}
		return candidateFor(d, "cast_spell", a, -1)
	case isManaOption(opt):
		return -1, "mana ability"
	case opt.Kind == "ability" || opt.Kind == "granted":
		a, ok := sh.arenaOf(opt.Obj)
		if !ok {
			return -1, "ability source not staged"
		}
		// The ordinal of this option among the object's non-mana ability
		// options picks among several kernel candidates of the source.
		ord := 0
		for i := range pd.Options {
			o := &pd.Options[i]
			if o.Index == opt.Index {
				break
			}
			if o.Obj == opt.Obj && (o.Kind == "ability" || o.Kind == "granted") {
				ord++
			}
		}
		return candidateFor(d, "activate_ability", a, ord)
	}
	return -1, "option kind " + opt.Kind
}

func optionByIndex(pd *decision.Decision, idx int) *decision.Option {
	for i := range pd.Options {
		if pd.Options[i].Index == idx {
			return &pd.Options[i]
		}
	}
	return nil
}

// candidateFor finds the kernel candidate of kind with source arena (any
// for pass). ord >= 0 picks the ord-th such candidate (several abilities of
// one source); a missing ord-th falls back to the only one when unique.
func candidateFor(d *v1agent.Decision, kind string, arena uint32, ord int) (int, string) {
	var hits []int
	for i := range d.Candidates {
		if d.Candidates[i].Kind() != kind {
			continue
		}
		if kind == "pass" {
			return i, ""
		}
		ka := kernelRef(d, i)
		if ka == nil || ka.Source == nil || ka.Source.ArenaID != arena {
			continue
		}
		hits = append(hits, i)
	}
	switch {
	case len(hits) == 0:
		return -1, "no kernel " + kind + " candidate"
	case ord < 0 || len(hits) == 1:
		return hits[0], ""
	case ord < len(hits):
		return hits[ord], ""
	}
	return -1, "ability ordinal"
}

// ---- follow-up plan ----

// followPlan is what the shadow policy chose after its priority answer, in
// the shadow: the object answers of every targeting decision, the numbers
// and the modes, consumed by the kernel's follow-up decisions.
type followPlan struct {
	source  uint32 // kernel arena id of the cast/activated source
	targets []planTarget
	numbers []int
	modes   []string
}

type planTarget struct {
	player bool
	seat   string
	arena  uint32
	used   bool
}

// makePlan submits in to the shadow and answers its own follow-up
// decisions with the same policy until priority (or anything else) comes
// back, recording targets, numbers and modes.
func (p *Policy) makePlan(sh *Shadow, pd *decision.Decision, in decision.Intent, seed uint64) *followPlan {
	fp := &followPlan{}
	if in.Payment != nil {
		for i := range pd.PaymentActions {
			if pd.PaymentActions[i].ID == in.Payment.ActionID {
				fp.source, _ = sh.arenaOf(pd.PaymentActions[i].Cast.Object)
			}
		}
	} else if len(in.Choices) == 1 {
		if o := optionByIndex(pd, in.Choices[0]); o != nil {
			if o.Kind == "pass" || o.Kind == "play_land" {
				return nil
			}
			fp.source, _ = sh.arenaOf(o.Obj)
		}
	}
	e := sh.E
	func() {
		defer func() { _ = recover() }()
		if err := e.Submit(in); err != nil {
			return
		}
		bot := seat.NewBot(seed ^ 0xb07)
		brd := botpolicy.NewBoard(2)
		for steps := 0; steps < 24 && !e.G.Over; steps++ {
			nd := e.Pending()
			if nd == nil || nd.Player != sh.Me {
				return
			}
			if nd.Kind == decision.KPriority && nd.ManaPayment == nil {
				return
			}
			var ans decision.Intent
			var err error
			if p.cfg.Mode == ModeTactical {
				ans, err = p.tacticalAnswer(sh)
			} else {
				b := botpolicy.BoardFromGameInto(e.G, e, sh.Me, &brd)
				ans, err = bot.DecideBoard(context.Background(), b, *nd)
			}
			if err != nil {
				return
			}
			fp.record(sh, nd, ans)
			if err := e.Submit(ans); err != nil {
				return
			}
		}
	}()
	if len(fp.targets) == 0 && len(fp.numbers) == 0 && len(fp.modes) == 0 {
		return nil
	}
	return fp
}

func (fp *followPlan) record(sh *Shadow, nd *decision.Decision, ans decision.Intent) {
	switch nd.Kind {
	case decision.KTarget:
		for _, c := range ans.Choices {
			o := optionByIndex(nd, c)
			if o == nil {
				continue
			}
			if o.Obj == 0 {
				fp.targets = append(fp.targets, planTarget{player: true, seat: seatName(o.Player)})
			} else if a, ok := sh.arenaOf(o.Obj); ok {
				fp.targets = append(fp.targets, planTarget{arena: a})
			}
		}
	case decision.KModes:
		for _, c := range ans.Choices {
			if o := optionByIndex(nd, c); o != nil {
				fp.modes = append(fp.modes, o.Label)
			}
		}
	case decision.KChoose:
		if nd.Prompt != "" && strings.Contains(strings.ToLower(nd.Prompt), "x") {
			for _, c := range ans.Choices {
				if o := optionByIndex(nd, c); o != nil {
					var n int
					if _, err := fmt.Sscanf(o.Label, "%d", &n); err == nil {
						fp.numbers = append(fp.numbers, n)
					}
				}
			}
		}
	}
}

func seatName(p state.PlayerID) string {
	if p == 1 {
		return "p1"
	}
	return "p0"
}

// follow answers a non-priority kernel decision from the plan.
func (p *Policy) follow(d *v1agent.Decision) (int, bool, string) {
	fp := p.plan
	if fp == nil {
		return 0, false, "no plan"
	}
	kinds := map[string]bool{}
	for i := range d.Candidates {
		kinds[d.Candidates[i].Kind()] = true
	}
	switch {
	case kinds["choose_target"]:
		for ti := range fp.targets {
			t := &fp.targets[ti]
			if t.used {
				continue
			}
			for i := range d.Candidates {
				if d.Candidates[i].Kind() != "choose_target" {
					continue
				}
				ka := kernelRef(d, i)
				if ka == nil || ka.Target == nil {
					continue
				}
				if t.player && ka.Target.Kind == "player" && ka.Target.Player == t.seat ||
					!t.player && ka.Target.Object != nil && ka.Target.Object.ArenaID == t.arena {
					t.used = true
					return i, true, ""
				}
			}
		}
		// Every planned target used: finish when offered.
		for i := range d.Candidates {
			if d.Candidates[i].Kind() == "finish_target_selection" {
				return i, true, ""
			}
		}
		return 0, false, "planned target not offered"
	case kinds["choose_number"]:
		if len(fp.numbers) == 0 {
			return 0, false, "no planned number"
		}
		want := fp.numbers[0]
		for i := range d.Candidates {
			if d.Candidates[i].Kind() == "choose_number" && int(d.Candidates[i].Semantic.Int("value")) == want {
				fp.numbers = fp.numbers[1:]
				return i, true, ""
			}
		}
		return 0, false, "planned number not offered"
	}
	return 0, false, "kind not planned: " + strings.Join(sortedKeys(kinds), ",")
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- combat ----

func (p *Policy) attack(d *v1agent.Decision) (int, bool, string) {
	if d.Group.GroupID != p.attackGroup || p.attackPlan == nil {
		p.attackGroup, p.attackPlan = d.Group.GroupID, nil
		sh := p.build(d)
		if sh.Fatal != "" {
			return 0, false, "stage: " + sh.Fatal
		}
		pd := sh.E.Pending()
		if pd.Kind != decision.KAttackers {
			return 0, false, "staged kind " + string(pd.Kind)
		}
		in, err := p.combatAnswer(sh, d)
		if err != nil {
			return 0, false, "policy: " + err.Error()
		}
		plan := map[uint32]bool{}
		for _, c := range in.Choices {
			if o := optionByIndex(pd, c); o != nil {
				if a, ok := sh.arenaOf(o.Obj); ok {
					plan[a] = true
				}
			}
		}
		p.attackPlan = plan
	}
	for i := range d.Candidates {
		ka := kernelRef(d, i)
		if ka == nil || ka.Attacker == nil {
			continue
		}
		if d.Candidates[i].Semantic.Bool("include") == p.attackPlan[ka.Attacker.ArenaID] {
			return i, true, ""
		}
	}
	return 0, false, "attack substep unmatched"
}

func (p *Policy) block(d *v1agent.Decision) (int, bool, string) {
	if d.Group.GroupID != p.blockGroup || p.blockPlan == nil {
		p.blockGroup, p.blockPlan = d.Group.GroupID, nil
		sh := p.build(d)
		if sh.Fatal != "" {
			return 0, false, "stage: " + sh.Fatal
		}
		pd := sh.E.Pending()
		if pd.Kind != decision.KBlockers {
			return 0, false, "staged kind " + string(pd.Kind)
		}
		in, err := p.combatAnswer(sh, d)
		if err != nil {
			return 0, false, "policy: " + err.Error()
		}
		plan := map[[2]uint32]bool{}
		for _, c := range in.Choices {
			o := optionByIndex(pd, c)
			if o == nil {
				continue
			}
			b, ok1 := sh.arenaOf(o.Obj)
			a, ok2 := sh.arenaOf(o.Attacker)
			if ok1 && ok2 {
				plan[[2]uint32{b, a}] = true
			}
		}
		p.blockPlan = plan
	}
	for i := range d.Candidates {
		ka := kernelRef(d, i)
		if ka == nil || ka.Attacker == nil || ka.Blocker == nil {
			continue
		}
		want := p.blockPlan[[2]uint32{ka.Blocker.ArenaID, ka.Attacker.ArenaID}]
		if d.Candidates[i].Semantic.Bool("include") == want {
			return i, true, ""
		}
	}
	return 0, false, "block substep unmatched"
}

// combatAnswer is the combat declaration: sb-tactical, the search, or the
// rollout search over candidate declarations.
func (p *Policy) combatAnswer(sh *Shadow, d *v1agent.Decision) (decision.Intent, error) {
	switch p.cfg.Mode {
	case ModeTactical:
		return p.tacticalAnswer(sh)
	case ModeRoll:
		return p.rollCombat(sh, d)
	}
	return p.search(sh, d, -1)
}

// ---- search ----

// search runs azmcts.Search over redealt shadow worlds at sh's pending
// decision. Candidate 0 is the gorge bot's answer (ModeAZ) or, when it maps
// into the shadow, v1agent.Tactical's (ModeAZTac, priority only).
func (p *Policy) search(sh *Shadow, d *v1agent.Decision, fbPick int) (decision.Intent, error) {
	e := sh.E
	pd := e.Pending()
	brd := botpolicy.NewBoard(2)
	b := botpolicy.BoardFromGameInto(e.G, e, sh.Me, &brd)
	seed := p.decisionSeed(d)
	botIn, err := seat.NewBot(seed^0x626f74).DecideBoard(context.Background(), b, *pd)
	if err != nil {
		return decision.Intent{}, err
	}
	if p.cfg.Mode == ModeAZTac && fbPick >= 0 && pd.Kind == decision.KPriority {
		if in, ok := gorgeIntentFor(sh, d, pd, fbPick); ok {
			botIn = in
		}
	}
	opts := azmcts.DefaultOptions()
	opts.Sims = p.cfg.Sims
	opts.Kinds = p.cfg.Kinds
	opts.Seed = seed
	obs := searchprobe.NewCollector(sh.Me)
	src := &shadowWorlds{sh: sh, obs: obs, seed: seed, k: p.cfg.Worlds}
	res, err := azmcts.Search(azmcts.Root{Engine: e, Decision: pd, Bot: botIn, Observer: obs}, src, nil, opts)
	if err != nil {
		return botIn, err
	}
	return res.Intent, nil
}

// gorgeIntentFor maps kernel candidate k onto the shadow's pending priority
// decision (plain option choice, no payment witness).
func gorgeIntentFor(sh *Shadow, d *v1agent.Decision, pd *decision.Decision, k int) (decision.Intent, bool) {
	kind := d.Candidates[k].Kind()
	ka := kernelRef(d, k)
	for j := range pd.Options {
		o := &pd.Options[j]
		if isManaOption(o) || isConcede(o) {
			continue
		}
		if priorityMatch(sh, kind, ka, o) {
			return decision.Intent{Seq: pd.Seq, Player: pd.Player, Choices: []int{o.Index}}, true
		}
	}
	return decision.Intent{}, false
}

// Summary renders the stats as one JSON object (the -stats line).
func (p *Policy) Summary() map[string]any {
	return map[string]any{
		"shadow_decisions": p.Stats.Decisions, "answered": p.Stats.Answered, "fallback_reasons": p.Stats.Reasons,
		"agree_with_fallback": p.Stats.Agree, "panics": p.Stats.Panics,
		"roll_searched": p.Roll.Searched, "roll_overrides": p.Roll.Overrides,
		"roll_rollouts": p.Roll.Rollouts, "roll_failed": p.Roll.Failed,
	}
}
