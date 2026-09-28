package builtins

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// This file is sb-tactical's fourth idea group, Archetype: read the
// opponent's mana flavours and playing style off the cards they have made
// public, decide who is the beatdown, and re-weight the existing weights as
// a counter-strategy. It is deliberately NOT a decorator: it must reach the
// priority, target and block scorers, and only TacticalWeights does.
//
// The whole group is switchable (TacticalWeights.Archetype) and every
// counter row is its own sub-switch (ArchBurn ... ArchTempo) so a row can be
// ablated. With Archetype false -- the sb-tactical default -- nothing here
// runs and every weight is exactly the default, so sb-tactical is
// byte-identical.
//
// Constraint: seat-visible information only. Everything comes from the
// projected View (public battlefields, stack, graveyards, exile, revealed
// cards, our own hand) resolved through the card IR; never the opponent's
// hand, library or library order. No card names and no deck ids are read
// anywhere in the classifier or the modulation -- only IR classes, the
// printed mana cost and the projected mana production.

// The eight archetypes the classifier scores. Dense indices, no maps.
const (
	archAggro = iota
	archBurn
	archTempo
	archControl
	archEngine
	archRamp
	archGoWide
	archMidrange
	archCount
)

// archNames labels the score vector for the trace and the report table.
var archNames = [archCount]string{"aggro", "burn", "tempo", "control", "engine", "ramp", "go-wide", "midrange"}

// tArchObs is the opponent's incrementally accumulated public feature set:
// one per opponent seat, updated once per decision as newly public cards
// appear. Dense counters only; the one map is a lookup set.
type tArchObs struct {
	seen map[state.ObjID]bool // lookup only: cards already counted

	colors [6]bool // colour identity (W U B R G C), from costs and production

	creatures   int
	mvSum       int
	fastBodies  int // creatures with MV <= 2
	smallBodies int // creatures with power <= 2
	evasion     int

	burnFace  int // damage / drain that can hit a player
	counters  int // counterspells
	removal   int // destroy / exile / bounce on battlefields
	manaCre   int // creatures with a mana ability
	engines   int // recurring triggers
	artifacts int
	affinity  int
	tokens    int
	flow      int // draw / select / flashback / recursion
	recursion int
	selfMill  int
	ramp      int

	maxTurn int32 // latest engine turn observed, for creatures-per-turn
}

// newArchObs returns an empty accumulator.
func newArchObs() *tArchObs { return &tArchObs{seen: map[state.ObjID]bool{}} }

// tArchState is one decision's classification of one opponent: the
// normalised archetype scores plus the derived strengths the modulation
// reads. A zero value means "unsure": every modulation factor is 1.0.
type tArchState struct {
	score [archCount]float64 // normalised (sums to 1 when anything is known)

	// Derived, in [0,1].
	burnThreat  float64 // aggro/burn: life is being spent
	counterRisk float64 // a counter already seen
	engineRisk  float64 // mana creatures, artifacts, recurring engines
	controlRisk float64 // removal + counters + flow, few creatures
	wideRisk    float64 // many small bodies / token makers
	tempoRisk   float64 // evasive threats, bounce, flash tempo

	known bool // any public opponent card or colour seen
}

// top returns the highest-scoring archetype's name and score, or ("", 0).
func (a tArchState) top() (string, float64) {
	best, bestV := "", 0.0
	for i := 0; i < archCount; i++ {
		if a.score[i] > bestV {
			best, bestV = archNames[i], a.score[i]
		}
	}
	return best, bestV
}

// colourSlot is the index of a single-letter mana symbol in the
// W U B R G C layout shared by state.Mana and cards.ManaProduction.
func colourSlot(b byte) int { return strings.IndexByte("WUBRGC", b) }

// cardColours sets a card's colour identity from its printed mana cost and,
// for a source, its projected mana production. Lands are read from
// production (a Forest's cost is empty); spells and creatures from their
// cost symbols. A source that makes any colour contributes all five, a soft
// signal rather than a rule.
func cardColours(cv *view.CardView) [6]bool {
	var c [6]bool
	if cv == nil {
		return c
	}
	for i := 0; i < len(cv.ManaCost); i++ {
		if slot := colourSlot(cv.ManaCost[i]); slot >= 0 && slot < 5 {
			c[slot] = true
		}
	}
	if cv.Produces != nil {
		listed := cv.Produces.Colour != [6]int32{}
		for i := 0; i < 5; i++ {
			if cv.Produces.Colour[i] > 0 {
				c[i] = true
			}
		}
		if cv.Produces.Any && !listed {
			for i := 0; i < 5; i++ {
				c[i] = true
			}
		}
	}
	return c
}

// archColourPriors turns a colour set into soft weights over the
// archetypes. These are the brief's table, not rules: red is aggro/burn,
// blue tempo/control, green ramp/midrange, white go-wide, black
// removal/drain.
func archColourPriors(c [6]bool) [archCount]float64 {
	var s [archCount]float64
	if c[3] { // R
		s[archAggro] += 1.0
		s[archBurn] += 1.0
	}
	if c[1] { // U
		s[archTempo] += 1.0
		s[archControl] += 1.0
	}
	if c[4] { // G
		s[archRamp] += 1.0
		s[archMidrange] += 0.5
	}
	if c[0] { // W
		s[archGoWide] += 1.0
	}
	if c[2] { // B
		s[archControl] += 0.5
		s[archBurn] += 0.5
		s[archMidrange] += 0.5
	}
	return s
}

// allEffects visits every labelled effect of a profile (the cast chain, the
// enter and dies triggers, and every activated ability EXCEPT mana
// abilities -- a land's or a mana creature's own mana production is a mana
// source, not a ramp spell, and counting it would read every land as ramp).
func (p *tProfile) allEffects(fn func(e *tEffect)) {
	for i := range p.spell {
		fn(&p.spell[i])
	}
	for i := range p.etb {
		fn(&p.etb[i])
	}
	for i := range p.dies {
		fn(&p.dies[i])
	}
	for i := range p.abilities {
		if p.abilities[i].mana {
			continue
		}
		for j := range p.abilities[i].effects {
			fn(&p.abilities[i].effects[j])
		}
	}
}

// accumulate folds one newly public opponent card into the feature set.
// Everything is read from the IR (tProfile) or the projected View facts
// (Types, Keywords) -- never a card name.
func (o *tArchObs) accumulate(cv *view.CardView, p *tProfile, turn int32) {
	if o.seen[cv.ID] {
		return
	}
	o.seen[cv.ID] = true
	if turn > o.maxTurn {
		o.maxTurn = turn
	}
	col := cardColours(cv)
	for i := range o.colors {
		o.colors[i] = o.colors[i] || col[i]
	}
	if p.creature {
		o.creatures++
		o.mvSum += int(p.cmc)
		if p.cmc <= 2 {
			o.fastBodies++
		}
		if max(p.power, 0) <= 2 {
			o.smallBodies++
		}
		if p.manaSource {
			o.manaCre++
		}
	}
	for _, ty := range strings.Fields(cv.Types) {
		if strings.EqualFold(ty, "Artifact") {
			o.artifacts++
		}
	}
	if p.affinity {
		o.affinity++
	}
	if p.engine {
		o.engines++
	}
	if p.flashback {
		o.flow++
	}
	for _, k := range cv.Keywords {
		switch strings.ToLower(cards.KeywordHead(k)) {
		case "flying", "menace", "unblockable":
			o.evasion++
		}
	}
	if p.flying || p.menace {
		o.evasion++
	}
	p.allEffects(func(e *tEffect) {
		switch e.class {
		case effCounter:
			o.counters++
		case effRemoval:
			o.removal++
		case effDamage:
			if e.players || e.oppOnly {
				o.burnFace++
			}
		case effDamageAll:
			if e.players {
				o.burnFace++
			}
		case effDrain:
			o.burnFace++
		case effToken:
			o.tokens += int(max(e.amount, 1))
		case effDraw, effSelect, effTutor:
			o.flow++
		case effRecursion:
			o.recursion++
			o.flow++
		case effMill:
			if !e.players {
				o.selfMill++
			}
		case effRamp:
			o.ramp++
		}
	})
}

// anyColor reports whether any colour has been learned.
func (o *tArchObs) anyColor() bool {
	for _, c := range o.colors {
		if c {
			return true
		}
	}
	return false
}

// empty reports whether the accumulator holds nothing usable at all.
func (o *tArchObs) empty() bool {
	return !o.anyColor() && o.creatures == 0 && o.counters == 0 && o.burnFace == 0 &&
		o.removal == 0 && o.tokens == 0 && o.engines == 0 && o.manaCre == 0 &&
		o.artifacts == 0 && o.ramp == 0 && o.flow == 0 && o.evasion == 0 && o.selfMill == 0
}

// classify produces the normalised archetype score for opponent opp from
// the accumulated observations. With nothing seen it returns the zero value
// (unknown), which leaves every weight at its default.
func (t *tactical) classify(opp state.PlayerID) tArchState {
	var a tArchState
	o := t.observeArchOf(opp)
	if o.empty() {
		return a
	}
	a.known = true
	s := archColourPriors(o.colors)
	// Curve and speed: a low average creature mana value and cheap early
	// bodies are aggro; many big bodies lean midrange.
	if o.creatures > 0 {
		avg := float64(o.mvSum) / float64(o.creatures)
		switch {
		case avg <= 2.5:
			s[archAggro] += 1.5
		case avg >= 4.0:
			s[archMidrange] += 0.8
		default:
			s[archMidrange] += 0.4
		}
		if o.fastBodies*2 >= o.creatures {
			s[archAggro] += 1.0
		}
	}
	if o.burnFace > 0 {
		s[archBurn] += 1.5 * float64(o.burnFace)
		if o.creatures <= 2 {
			s[archBurn] += 1.0
		}
	}
	if o.counters > 0 {
		s[archTempo] += 1.5 * float64(o.counters)
		s[archControl] += 0.8 * float64(o.counters)
	}
	if o.evasion > 0 {
		s[archTempo] += 0.6 * float64(o.evasion)
	}
	if o.removal > 0 {
		s[archControl] += 0.9 * float64(o.removal)
	}
	if o.flow > 0 {
		s[archControl] += 0.4 * float64(o.flow)
	}
	if o.recursion > 0 || o.selfMill > 0 {
		s[archEngine] += 0.9 * float64(o.recursion+o.selfMill)
	}
	if o.manaCre > 0 {
		s[archRamp] += 1.2 * float64(o.manaCre)
		s[archEngine] += 0.6 * float64(o.manaCre)
	}
	if o.ramp > 0 {
		s[archRamp] += 0.9 * float64(o.ramp)
	}
	if o.engines > 0 {
		s[archEngine] += 1.4 * float64(o.engines)
	}
	if o.artifacts > 0 {
		s[archEngine] += 0.5 * float64(o.artifacts)
	}
	if o.affinity > 0 {
		s[archEngine] += 1.0 * float64(o.affinity)
	}
	if o.tokens > 0 {
		s[archGoWide] += 1.4 * float64(o.tokens)
	}
	if o.smallBodies >= 3 {
		s[archGoWide] += 0.8 * float64(o.smallBodies-2)
	}
	sum := 0.0
	for i := 0; i < archCount; i++ {
		if s[i] > 0 {
			sum += s[i]
		}
	}
	if sum <= 0 {
		return a
	}
	for i := 0; i < archCount; i++ {
		if s[i] < 0 {
			s[i] = 0
		}
		a.score[i] = s[i] / sum
	}
	a.burnThreat = clamp01(a.score[archBurn] + 0.7*a.score[archAggro])
	a.engineRisk = clamp01(a.score[archEngine] + 0.7*a.score[archRamp])
	a.controlRisk = clamp01(a.score[archControl] + 0.5*a.score[archTempo])
	a.wideRisk = clamp01(a.score[archGoWide])
	a.tempoRisk = clamp01(a.score[archTempo] + 0.5*a.score[archAggro])
	if o.counters > 0 {
		a.counterRisk = clamp01(0.6 + 0.4*float64(o.counters))
	}
	return a
}

// observeArchOf returns the accumulator for opp, creating it lazily.
func (t *tactical) observeArchOf(opp state.PlayerID) *tArchObs {
	if t.archObs == nil {
		t.archObs = map[state.PlayerID]*tArchObs{}
	}
	o := t.archObs[opp]
	if o == nil {
		o = newArchObs()
		t.archObs[opp] = o
	}
	return o
}

// observeArch folds every public card of every other seat into its
// accumulator, once. Called at the top of every decision, before scoring.
func (t *tactical) observeArch(v view.View) {
	for i := range v.Players {
		p := &v.Players[i]
		if p.ID == v.Viewer {
			continue
		}
		o := t.observeArchOf(p.ID)
		for _, zone := range [][]view.CardView{p.Battlefield, p.Graveyard, p.Exile} {
			for j := range zone {
				cv := &zone[j]
				o.accumulate(cv, t.profile(cv), v.Turn)
			}
		}
		for j := range v.Stack {
			sv := &v.Stack[j]
			if sv.Card != nil && sv.Controller == p.ID {
				o.accumulate(sv.Card, t.profile(sv.Card), v.Turn)
			}
		}
	}
}

// counterThreat reports whether the opponent plausibly holds a counterspell
// now: a counter already seen, or at least two untapped sources that can
// make blue.
func (t *tactical) counterThreat(s *tstate, a tArchState) float64 {
	open := s.oppUntappedBlue() >= 2
	switch {
	case a.counterRisk > 0:
		return a.counterRisk
	case open && a.known:
		return clamp01(0.4 + 0.3*a.score[archTempo] + 0.3*a.score[archControl])
	case open:
		return 0.5
	}
	return 0
}

// oppUntappedBlue counts the opponent's untapped sources that can produce U.
func (s *tstate) oppUntappedBlue() int32 {
	var n int32
	for i := range s.oppP.Battlefield {
		cv := &s.oppP.Battlefield[i]
		if cv.Tapped || cv.Produces == nil {
			continue
		}
		if cv.Produces.Colour[1] > 0 || cv.Produces.Any && cv.Produces.Colour == [6]int32{} {
			n++
		}
	}
	return n
}

// clamp01 clamps x to [0,1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// modulate returns the default weights scaled for this decision's
// counter-strategy. It never switches on a single top archetype: every row
// contributes its own multiplicative change, blended by the score and gated
// by its sub-switch. With an unknown opponent it returns the base weights
// unchanged, exactly 1.0x.
func (t *tactical) modulate(s *tstate) TacticalWeights {
	w := t.base
	a := s.arch
	if !a.known {
		return w
	}
	// Role: +1 we are the beatdown (race), -1 we are control. Blended into
	// the rows that care, and re-evaluated every decision because the clocks
	// move.
	role := beatdownFactor(s)
	if w.ArchBurn {
		b := a.burnThreat
		w.Removal *= 1 + 0.5*b
		w.KeyPiece *= 1 + 0.8*b
		w.FaceAhead *= 1 - 0.35*b
		w.FaceBehind *= 1 - 0.15*b
		w.ArchLife *= 1 + 0.6*b
		w.ArchBlock *= 1 + 0.8*b
	}
	if w.ArchCounter {
		c := t.counterThreat(s, a)
		w.HoldReactive *= 1 + 0.6*c
		w.KeepUp *= 1 + 0.9*c
		w.ArchBait *= 1 + 1.2*c
	}
	if w.ArchEngine {
		e := a.engineRisk
		w.KeyPiece *= 1 + 1.5*e
		w.Removal *= 1 + 0.4*e
		if role > 0 {
			w.FaceAhead *= 1 + 0.25*e
		}
	}
	if w.ArchControl {
		c := a.controlRisk
		w.Card *= 1 + 0.4*c
		w.ArchOverextend *= 1 + 1.0*c
		if role > 0 {
			w.FaceAhead *= 1 + 0.3*c
		}
	}
	if w.ArchWide {
		g := a.wideRisk
		w.Removal *= 1 - 0.25*g
		w.KeyPiece *= 1 + 0.4*g
		w.ArchBlock *= 1 + 0.3*g
	}
	if w.ArchTempo {
		tm := a.tempoRisk
		w.Removal *= 1 + 0.35*tm
		w.KeyPiece *= 1 + 0.4*tm
	}
	return w
}

// beatdownFactor reports how strongly we should take the beatdown role:
// +1 when our clock is clearly faster (we race), -1 when clearly slower
// (we control), 0 when even. The clocks are the Race group's own
// projections, so the role re-evaluates every decision as the board moves.
func beatdownFactor(s *tstate) float64 {
	switch {
	case s.myClock < s.oppClock:
		return 1
	case s.myClock > s.oppClock:
		return -1
	}
	return 0
}

// archAdjustPriority applies the Archetype group's priority-level
// counter-plays to the scored candidates: hold the best threat against open
// counter mana (bait with the lesser spell), and do not overextend into a
// control opponent's sweeper when we are already ahead on board. It changes
// scores only, and not at all when the group is off or the opponent unknown.
func (t *tactical) archAdjustPriority(st *tstate, d *decision.Decision, cands []cand, scores []float64) {
	if !st.arch.known {
		return
	}
	// Counter bait: with open counter mana, the single best cast is held and
	// the lesser casts are preferred this turn. Blended by ArchBait.
	if t.w.ArchBait > 1 {
		bait := t.w.ArchBait - 1
		bestCast, bestCastScore := -1, 0.0
		for i, c := range cands {
			if castObj(d, c) != 0 && scores[i] > bestCastScore {
				bestCast, bestCastScore = i, scores[i]
			}
		}
		if bestCast >= 0 && bestCastScore > 0 {
			scores[bestCast] -= bait * bestCastScore
			for i, c := range cands {
				if i != bestCast && castObj(d, c) != 0 && scores[i] > 0 {
					scores[i] += bait * 0.2 * scores[i]
				}
			}
		}
	}
	// Overextend: against a control opponent, deploying an extra body when
	// we already have board presence is a sweeper's dream. Penalty scaled
	// by ArchOverextend and by how many bodies we already control.
	if t.w.ArchOverextend > 1 && st.arch.controlRisk > 0 {
		extra := t.w.ArchOverextend - 1
		extra *= 1 + 0.5*float64(max(0, len(st.mine)-1))
		for i, c := range cands {
			obj := castObj(d, c)
			if obj == 0 || scores[i] <= 0 || len(st.mine) < 2 {
				continue
			}
			if p := t.profile(st.objs[obj]); p.creature {
				scores[i] -= extra * 0.25 * scores[i]
			}
		}
	}
}

// castObj is the object id a candidate would cast, or 0.
func castObj(d *decision.Decision, c cand) state.ObjID {
	switch {
	case c.opt >= 0:
		if o := &d.Options[c.opt]; o.Kind == "cast" {
			return o.Obj
		}
	case c.plan != nil:
		return c.plan.Cast.Object
	case c.pot != nil && c.pot.kind == "cast":
		return c.pot.obj
	}
	return 0
}

// archTraceLine renders the classifier for the trace.
func (a tArchState) traceLine() string {
	if !a.known {
		return "arch unknown"
	}
	var b strings.Builder
	for i := 0; i < archCount; i++ {
		if a.score[i] <= 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(archNames[i])
		b.WriteByte('=')
		b.WriteString(strconv.FormatFloat(a.score[i], 'f', 2, 64))
	}
	return "arch " + b.String()
}
