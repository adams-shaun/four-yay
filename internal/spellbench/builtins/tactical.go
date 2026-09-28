package builtins

import (
	"fmt"
	"io"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// sb-tactical (Policy Tactical) is a hand-written scored heuristic built for
// SpellBench's observation constraint: every decision is scored from what
// the seat can legally see -- its projected View (public battlefields,
// stack, graveyards, life totals, hand SIZES; its own hand and pool) and
// the decision's own options -- plus printed card facts its card names
// resolve to in gorge's card IR (tactical_ir.go). It never reads the
// opponent's hand or any library.
//
// Structure: seat-visible feature extraction (tactical_state.go: creatures
// with derived P/T/keywords, the race clocks, the mana on hand) -> a score
// per candidate, with every weight in TacticalWeights. Four idea groups,
// each switchable for ablation:
//
//  1. EarlyGame: development and card advantage early (land every turn,
//     ramp and draw weighted up in the first EarlyTurns own turns), and
//     removal aimed at the opponent's key pieces (mana creatures, engines,
//     evasive threats).
//  2. Timing: instant-speed discipline. A reactive card (counterspell,
//     instant removal/burn, combat trick, flash creature, instant draw) is
//     held in the seat's own main phase (HoldReactive) and spent on the
//     opponent's turn: counters only at a foreign spell, removal on declared
//     attackers or at the end step, tricks after blocks, draw at the end
//     step (WaitEOT); a proactive cast that would tap below a held reactive
//     card's cost pays KeepUp; a non-haste creature waits for the second
//     main phase when the seat will attack (PostCombat).
//  3. Race: projected turns-to-kill both ways (attackDamage/clock) drive
//     face-vs-creature burn (FaceAhead/FaceBehind/ClockTurn), attacks (an
//     alpha strike when the whole team is lethal through the best blocks,
//     otherwise gorge's combat simulation) and blocks (chump only when the
//     race is otherwise lost, double-block a big attacker when the pair
//     kills it profitably, and no chump when we are ahead and keep our
//     attackers).
//
// Every effect is labelled with a direction (tactical_ir.go), so harm is
// pointed at the opponent's side and help at our own (tactical_target.go),
// with deliberate exceptions scored rather than special-cased: a harmful
// effect on an indestructible own permanent costs nothing, so a rider that
// benefits the target's controller (Cleansing Wildfire on our own Bridge)
// makes the own target the best one.
//
// Everything else (modes, X, sacrifice and discard choices, trigger order,
// mana colour asks outside a pursuit) is gorge's default bot policy
// (botpolicy.Decide) over the same seat-visible Board (seat.BoardFromView),
// seeded from the seat's own stream.
//
// Determinism: the maps here are lookup tables (profiles by name, objects by
// id); every choice iterates slices in view or option order with index
// tie-breaks.

// CardLookup resolves a card (or token) name to its compiled card, nil when
// unknown. It must be safe for concurrent use.
type CardLookup func(name string) *cards.Card

// NewRegistryLookup builds a CardLookup over reg: real cards by name, then
// token scripts by their face name (first stem in sorted order wins).
func NewRegistryLookup(reg *cards.Registry) CardLookup {
	if reg == nil {
		return nil
	}
	stems := make([]string, 0, len(reg.Tokens))
	for k := range reg.Tokens {
		stems = append(stems, k)
	}
	sort.Strings(stems)
	tokens := make(map[string]*cards.Card, len(stems)) // lookup only
	for _, k := range stems {
		c := reg.Tokens[k]
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		n := cards.NormalizeName(c.Faces[0].Name)
		if _, ok := tokens[n]; !ok {
			tokens[n] = c
		}
	}
	return func(name string) *cards.Card {
		if c, ok := reg.Lookup(name); ok {
			return c
		}
		return tokens[cards.NormalizeName(name)]
	}
}

// TacticalWeights is the one place sb-tactical is tuned. Units are "points":
// a card in hand is Card, a vanilla 2/2 on the battlefield about 5.
type TacticalWeights struct {
	// Idea groups (ablation switches).
	EarlyGame bool
	Timing    bool
	Race      bool
	// Archetype is the fourth idea group: read the opponent's colours and
	// playing style off public cards and re-weight the counters. It defaults
	// false, so the plain sb-tactical arm is byte-identical.
	Archetype bool
	// The Archetype group's counter rows, each switchable for ablation. All
	// default true and are read only when Archetype is on.
	ArchBurn, ArchCounter, ArchEngine, ArchControl, ArchWide, ArchTempo bool

	// Base values.
	Land        float64 // a land drop (always first in a main phase)
	Card        float64 // one card drawn / tutored
	ManaSpent   float64 // per mana value spent casting (tempo)
	Body        float64 // multiplier on a cast creature's value
	Token       float64 // per creature token
	Clue        float64 // per clue / blood / food / map style token
	Select      float64 // scry / surveil / look
	Ramp        float64 // a land or mana source beyond the land drop
	Removal     float64 // multiplier on a removed creature's value
	Counter     float64 // countering a spell (base)
	CounterMV   float64 // + per mana value of the countered spell
	Discard     float64 // per card the opponent discards
	Trick       float64 // multiplier on a combat trick's swing
	Unknown     float64 // an ability the IR does not label
	Special     float64 // plot / unlock / other special actions
	Ninjutsu    float64 // a ninjutsu swap
	Initiative  float64 // taking the initiative
	MillOpp     float64 // milling the opponent (scaled up as their library shrinks)
	SacPerm     float64 // cost: a nontoken permanent sacrificed
	SacToken    float64 // cost: a token sacrificed
	SacLand     float64 // cost: a land sacrificed / returned
	DiscardCost float64 // cost: a card discarded
	TapAttacker float64 // cost: tapping a creature that would attack (per power)

	// Group 1: early game.
	EarlyTurns int     // own turns counted "early"
	EarlyDraw  float64 // multiplier on draw/select early
	EarlyRamp  float64 // multiplier on ramp early
	KeyPiece   float64 // removal bonus for a mana creature / engine / evasive threat

	// Group 2: timing.
	HoldReactive float64 // penalty: a reactive card cast in our own main phase
	WaitEOT      float64 // penalty: a proactive instant-speed play before the opponent's end step
	KeepUp       float64 // penalty: a main-phase cast that taps below a held reactive card
	PostCombat   float64 // penalty: a non-haste creature in main 1 when we will attack
	KillAttacker float64 // bonus: removal on an attacking creature
	OffWindow    float64 // an instant-speed play outside its window loses at least this share of its value

	// Group 3: race.
	FaceAhead  float64 // burn-to-face multiplier when we win the race
	FaceBehind float64 // ... when we lose it
	ClockTurn  float64 // bonus when face damage takes a turn off our clock
	Lethal     float64 // bonus for lethal damage

	// Group 4: archetype. Multiplicative modulators (default 1.0) the
	// classifier scales; a weight no existing field fits gets its own.
	ArchLife       float64 // multiplier on OUR life value (burn spends it)
	ArchBlock      float64 // multiplier on chumping early (aggro/burn)
	ArchBait       float64 // how much to hold the best spell / bait a counter
	ArchOverextend float64 // penalty for deploying an extra body when ahead
}

// DefaultTacticalWeights is sb-tactical's configuration.
func DefaultTacticalWeights() TacticalWeights {
	return TacticalWeights{
		EarlyGame: true, Timing: true, Race: true,

		Land: 100, Card: 4, ManaSpent: 0.8, Body: 1.0, Token: 2.5, Clue: 1.5, Select: 1.0,
		Ramp: 2.0, Removal: 1.2, Counter: 3, CounterMV: 1.5, Discard: 3, Trick: 1.0,
		Unknown: -0.5, Special: 0.2, Ninjutsu: 6, Initiative: 6, MillOpp: 3,
		SacPerm: 3, SacToken: 0.5, SacLand: 6, DiscardCost: 3, TapAttacker: 0.8,

		EarlyTurns: 4, EarlyDraw: 1.5, EarlyRamp: 2.5, KeyPiece: 3,

		HoldReactive: 8, WaitEOT: 4, KeepUp: 1.0, PostCombat: 0, KillAttacker: 3, OffWindow: 0.6,

		FaceAhead: 1.3, FaceBehind: 0.6, ClockTurn: 4, Lethal: 1000,

		ArchBurn: true, ArchCounter: true, ArchEngine: true,
		ArchControl: true, ArchWide: true, ArchTempo: true,
		ArchLife: 1.0, ArchBlock: 1.0, ArchBait: 1.0, ArchOverextend: 1.0,
	}
}

// tactical is the per-seat state of a Tactical builtin.
type tactical struct {
	w      TacticalWeights
	base   TacticalWeights // the seat's configured weights, unmodulated
	lookup CardLookup
	// archObs is the per-opponent public feature accumulation the Archetype
	// group reads (lookup only; one small entry per other seat).
	archObs map[state.PlayerID]*tArchObs
	cache   map[string]*tProfile // lookup only
	rng     *rand.Rand
	sim     botpolicy.AttackSimParams
	trace   io.Writer // debugging: nil in play

	// tapAll marks a pursuit of an {X} spell (pursue taps every source).
	tapAll bool
	// force is the pick a searching wrapper imposed on the next fresh
	// priority choice (ForcePriority), nil in plain play.
	force *PriorityKey
	// reanimating guards reanimateValue against a creature whose own ETB
	// reanimates (it would recurse without end).
	reanimating bool
	// The opponent's observed blocking: our attacks it could have blocked
	// (one per turn) and how many it did block. A seat that never blocks
	// is raced, not simulated (tactical_combat.go).
	blockChances, blocks int
	lastBlockTurn        int32
}

// SetTrace makes a Tactical seat write its scored priority candidates and
// its combat and target answers to w (debugging; nil turns it off).
func (s *Seat) SetTrace(w io.Writer) {
	if s.tac != nil {
		s.tac.trace = w
	}
}

// NewTactical returns an sb-tactical seat. lookup resolves card names to
// gorge's card IR (nil: every card is read from its View facts only).
func NewTactical(m ManaMode, seed uint64, lookup CardLookup, w TacticalWeights) *Seat {
	s := New(Tactical, m, seed)
	s.tac = &tactical{
		w: w, base: w, lookup: lookup, cache: map[string]*tProfile{},
		rng: rand.New(rand.NewPCG(seed, seed^0x7ac71ca1)),
		sim: botpolicy.DefaultAttackSimParams(),
	}
	return s
}

// profile resolves a CardView's printed profile, cached per name.
func (t *tactical) profile(cv *view.CardView) *tProfile {
	if cv == nil || cv.FaceDown || cv.Name == "" {
		return &tProfile{}
	}
	if p, ok := t.cache[cv.Name]; ok {
		return p
	}
	var c *cards.Card
	if t.lookup != nil {
		c = t.lookup(cv.Name)
	}
	p := profileOf(c)
	if !p.known {
		p.name = cv.Name
		p.creature = isCreatureTypes(cv.Types)
		p.power, p.toughness = cv.Power, cv.Toughness
		p.cmc = botpolicy.CmcOf(cv.ManaCost)
	}
	t.cache[cv.Name] = p
	return p
}

// decide answers every decision kind for a Tactical seat. Priority runs
// through the builtin candidate machinery (Seat.priority) with the tactical
// pick; combat and targets are the tactical rules; everything else is
// gorge's default policy on the seat-visible Board.
func (t *tactical) decide(s *Seat, v view.View, d *decision.Decision) decision.Intent {
	in := t.decideKind(s, v, d)
	if d.Kind != decision.KPriority {
		t.traceAnswer(v, d, in)
	}
	return in
}

func (t *tactical) decideKind(s *Seat, v view.View, d *decision.Decision) decision.Intent {
	t.observe(v, d.Player)
	t.observeArch(v)
	if t.base.Archetype {
		st := t.newState(&v, d.Player)
		st.arch = t.classify(st.opp)
		t.w = t.modulate(st)
	} else if t.w != t.base {
		t.w = t.base
	}
	if s.pursuit == nil {
		t.tapAll = false
	}
	switch d.Kind {
	case decision.KPriority:
		return s.priority(v, d, 0)
	case decision.KAttackers:
		return t.attackers(v, d)
	case decision.KBlockers:
		return t.blockers(v, d)
	case decision.KTarget:
		if in, ok := t.targets(v, d); ok {
			return in
		}
	case decision.KChoose, decision.KModes:
		if in, ok := t.discard(v, d); ok {
			return in
		}
		if in, ok := t.sacrifice(v, d); ok {
			return in
		}
		if d.Kind == decision.KModes {
			break
		}
		if s.pursuit != nil {
			if i, ok := pursuitColour(v, d); ok {
				return one(d, i)
			}
		}
		if len(d.Options) > 0 && d.Options[0].Kind == "x" {
			// {X}: the largest payable value.
			best := d.Options[0]
			for _, o := range d.Options {
				if o.Amount > best.Amount {
					best = o
				}
			}
			return one(d, best.Index)
		}
	}
	return t.fallback(v, d)
}

// fallback is gorge's default bot policy on the seat-visible Board.
func (t *tactical) fallback(v view.View, d *decision.Decision) decision.Intent {
	brd := seat.BoardFromView(v)
	in := botpolicy.Decide(brd, d, t.rng)
	return repaired(d, in)
}

// pickPriority is the tactical priority pick over the builtin candidate
// list: the highest-scoring candidate, pass (score 0) when nothing scores
// above it, ties to the earlier candidate.
func (t *tactical) pickPriority(v view.View, d *decision.Decision, cands []cand) int {
	st, scores := t.scorePriority(v, d, cands)
	var line []string
	best, bestScore := 0, -1e18
	for i, sc := range scores {
		if sc > bestScore {
			best, bestScore = i, sc
		}
		if t.trace != nil {
			line = append(line, fmt.Sprintf("%s=%.1f", t.candLabel(st, d, cands[i]), sc))
		}
	}
	if t.force != nil {
		// A searching wrapper (ForcePriority) named this decision's pick;
		// a key no candidate carries leaves the scored pick standing.
		k := *t.force
		t.force = nil
		for i, c := range cands {
			if candKey(d, c) == k {
				best = i
				break
			}
		}
	}
	if c := cands[best]; c.pot != nil && c.pot.kind == "cast" {
		if cv := st.objs[c.pot.obj]; cv != nil && t.profile(cv).xCost {
			t.tapAll = true
		}
	}
	if t.trace != nil && len(cands) > 1 {
		fmt.Fprintf(t.trace, "T%d %s p%d life %d/%d lib %d/%d clock %.0f/%.0f %s | pick %s | %s\n", v.Turn, v.Step, d.Player,
			st.myLife, st.oppLife, st.meP.LibrarySize, st.oppP.LibrarySize, st.myClock, st.oppClock, st.arch.traceLine(),
			t.candLabel(st, d, cands[best]), strings.Join(line, " "))
	}
	return best
}

// scorePriority is the tactical score of every candidate (pickPriority's
// ranking; TacticalPriority exposes it).
func (t *tactical) scorePriority(v view.View, d *decision.Decision, cands []cand) (*tstate, []float64) {
	st := t.newState(&v, d.Player)
	scores := make([]float64, len(cands))
	for i, c := range cands {
		scores[i] = t.scoreCand(st, d, c)
	}
	t.archAdjustPriority(st, d, cands, scores)
	return st, scores
}

// zoneOf reports which of our zones holds obj ("hand", "graveyard",
// "exile", "battlefield") or "" when none.
func (s *tstate) zoneOf(obj state.ObjID) string {
	for _, z := range []struct {
		name string
		cs   []view.CardView
	}{{"hand", s.meP.Hand}, {"graveyard", s.meP.Graveyard}, {"exile", s.meP.Exile}, {"battlefield", s.meP.Battlefield}} {
		for i := range z.cs {
			if z.cs[i].ID == obj {
				return z.name
			}
		}
	}
	return ""
}

// candLabel names a candidate for the trace.
func (t *tactical) candLabel(s *tstate, d *decision.Decision, c cand) string {
	switch {
	case c.opt >= 0:
		o := &d.Options[c.opt]
		if o.Kind == "pass" {
			return "pass"
		}
		name := ""
		if cv := s.objs[o.Obj]; cv != nil {
			name = cv.Name
		}
		return fmt.Sprintf("%s[%s%s#%d]", o.Kind, name, o.Mode, o.Ability)
	case c.plan != nil:
		name := ""
		if cv := s.objs[c.plan.Cast.Object]; cv != nil {
			name = cv.Name
		}
		return "plan[" + name + "]"
	case c.pot != nil:
		name := ""
		if cv := s.objs[c.pot.obj]; cv != nil {
			name = cv.Name
		}
		return fmt.Sprintf("pot-%s[%s%s#%d]", c.pot.kind, name, c.pot.mode, c.pot.ability)
	}
	return "?"
}

// traceAnswer writes a non-priority answer to the trace.
func (t *tactical) traceAnswer(v view.View, d *decision.Decision, in decision.Intent) {
	if t.trace == nil {
		return
	}
	var labels []string
	for _, i := range in.Choices {
		if i >= 0 && i < len(d.Options) {
			labels = append(labels, d.Options[i].Label)
		}
	}
	fmt.Fprintf(t.trace, "T%d %s p%d %s %q -> %s\n", v.Turn, v.Step, d.Player, d.Kind, d.Prompt, strings.Join(labels, "; "))
}

// observe records the opponent's blocking behaviour from the public
// combat state: once per turn of ours, after blockers are declared, whether
// any of our attackers is blocked.
func (t *tactical) observe(v view.View, me state.PlayerID) {
	if v.Active != me || t.lastBlockTurn == v.Turn {
		return
	}
	if v.Step != "declare-blockers" && v.Step != "combat-damage" && v.Step != "end-combat" {
		return
	}
	attacked, blocked := false, false
	for i := range v.Players {
		if v.Players[i].ID != me {
			continue
		}
		for _, cv := range v.Players[i].Battlefield {
			if cv.Attacking {
				attacked = true
				if len(cv.BlockedBy) > 0 {
					blocked = true
				}
			}
		}
	}
	if !attacked {
		return
	}
	t.lastBlockTurn = v.Turn
	t.blockChances++
	if blocked {
		t.blocks++
	}
}

// oppNeverBlocks reports an opponent observed declining every block.
func (t *tactical) oppNeverBlocks() bool { return t.blockChances >= 2 && t.blocks == 0 }

// discard answers a discard ask (a cost, looting, the hand-size limit): the
// Min cards of our hand we would least like to keep.
func (t *tactical) discard(v view.View, d *decision.Decision) (decision.Intent, bool) {
	if !strings.Contains(strings.ToLower(d.Prompt), "discard") || len(d.Options) == 0 {
		return decision.Intent{}, false
	}
	st := t.newState(&v, d.Player)
	type kv struct {
		idx int
		v   float64
	}
	var ks []kv
	for i := range d.Options {
		o := &d.Options[i]
		if st.zoneOf(o.Obj) != "hand" {
			return decision.Intent{}, false
		}
		ks = append(ks, kv{o.Index, t.keepValue(st, st.objs[o.Obj])})
	}
	sort.SliceStable(ks, func(i, j int) bool {
		if ks[i].v != ks[j].v {
			return ks[i].v < ks[j].v
		}
		return ks[i].idx < ks[j].idx
	})
	var chosen []int
	for _, k := range ks {
		if len(chosen) >= d.Min {
			break
		}
		if admissible(d, chosen, &d.Options[k.idx]) {
			chosen = append(chosen, k.idx)
		}
	}
	return repaired(d, decision.Intent{Choices: chosen}), true
}

// keepValue is how much we want to keep a hand card: lands while we still
// need them, spells by their cast value, discounted when far off the curve
// and for cards that work from the graveyard (flashback, madness).
func (t *tactical) keepValue(s *tstate, cv *view.CardView) float64 {
	if cv == nil {
		return 0
	}
	lands := s.lands
	for i := range s.meP.Hand {
		if isLandView(&s.meP.Hand[i]) {
			lands++
		}
	}
	if isLandView(cv) {
		if lands-1 < 5 {
			return 8
		}
		return 1
	}
	p := t.profile(cv)
	v := t.w.Card + t.w.ManaSpent*float64(p.cmc)
	if p.creature {
		v += t.w.Body * t.profileCreValue(s, p, cv)
	}
	if p.cmc > lands+1 {
		v *= 0.5
	}
	for _, k := range cv.Keywords {
		switch strings.ToLower(cards.KeywordHead(k)) {
		case "flashback", "madness":
			v *= 0.3
		}
	}
	return v
}

// sacrifice answers a sacrifice-as-a-cost ask: the Min permanents we lose
// least by (consumables and tokens first, lands last while we develop).
func (t *tactical) sacrifice(v view.View, d *decision.Decision) (decision.Intent, bool) {
	if !strings.HasPrefix(d.Prompt, "Sacrifice") || len(d.Options) == 0 || d.Min < 1 {
		return decision.Intent{}, false
	}
	st := t.newState(&v, d.Player)
	type kv struct {
		idx int
		v   float64
	}
	var ks []kv
	for i := range d.Options {
		o := &d.Options[i]
		if st.zoneOf(o.Obj) != "battlefield" {
			return decision.Intent{}, false
		}
		ks = append(ks, kv{o.Index, t.permValue(st, st.objs[o.Obj])})
	}
	sort.SliceStable(ks, func(i, j int) bool {
		if ks[i].v != ks[j].v {
			return ks[i].v < ks[j].v
		}
		return ks[i].idx < ks[j].idx
	})
	var chosen []int
	for _, k := range ks {
		if len(chosen) >= d.Min {
			break
		}
		if admissible(d, chosen, &d.Options[k.idx]) {
			chosen = append(chosen, k.idx)
		}
	}
	return repaired(d, decision.Intent{Choices: chosen}), true
}
