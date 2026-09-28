package builtins

import (
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// scoreCand scores one priority candidate. Pass is 0; anything scoring at or
// below 0 loses to it.
func (t *tactical) scoreCand(s *tstate, d *decision.Decision, c cand) float64 {
	kind, obj, mode, abil, alt := "", state.ObjID(0), "", 0, 0
	switch {
	case c.opt >= 0:
		o := &d.Options[c.opt]
		kind, obj, mode, abil, alt = o.Kind, o.Obj, o.Mode, o.Ability, o.AltCostIndex
	case c.plan != nil:
		kind, obj = "cast", c.plan.Cast.Object
	case c.pot != nil:
		kind, obj, mode, abil = c.pot.kind, c.pot.obj, c.pot.mode, c.pot.ability
	}
	switch kind {
	case "pass":
		return 0
	case "play_land":
		return t.w.Land
	case "cast":
		return t.castScore(s, obj, mode, alt, c.pot != nil)
	case "ability", "granted", "station":
		return t.abilityScore(s, obj, abil, c.pot != nil)
	}
	if c.cls == clsSpecial && s.myTurn && s.main {
		return t.w.Special
	}
	return -1
}

// castScore values casting obj now.
func (t *tactical) castScore(s *tstate, obj state.ObjID, mode string, alt int, pot bool) float64 {
	cv := s.objs[obj]
	p := t.profile(cv)
	if cv == nil || !p.known {
		// Unreadable: a small positive, cast like the default bot would.
		return t.w.ManaSpent
	}
	w := &t.w
	if pot && !s.canAfford(cv.ManaCost, kickerExtra(mode)) {
		return -1 // a pursuit would tap sources for a cast it cannot pay
	}
	kicked := strings.HasPrefix(mode, "kicked")
	v := w.ManaSpent * float64(p.cmc)
	if p.xCost {
		// {X}: X is what the board can pay beyond the fixed part.
		x := s.producible - p.cmc
		if x < 2 || mode != "" {
			return -1
		}
		v = w.ManaSpent * float64(p.cmc+x)
		if p.creature {
			q := *p
			q.power, q.toughness = p.power+x, p.toughness+x
			v += w.Body * t.profileCreValue(s, &q, nil)
		}
		return v - t.timingPenaltyIf(s, p, obj, v)
	}
	if p.creature {
		v += w.Body * t.profileCreValue(s, p, cv)
	}
	effs := p.spell
	if p.permanent {
		effs = append(append([]tEffect(nil), p.spell...), p.etb...)
	}
	best := 0.0 // a modal spell (Charm) takes its best mode; other chains add
	modal := len(p.spell) > 0 && (p.spell[0].api == "" || isModal(cv))
	for i := range effs {
		e := &effs[i]
		if e.kicked && !kicked {
			continue
		}
		ev := t.effValue(s, p, e, obj)
		if modal && i < len(p.spell) {
			if ev > best {
				best = ev
			}
			continue
		}
		v += ev
	}
	v += best
	// Additional and alternative costs.
	v -= t.costValue(s, p.spellCost, obj)
	switch {
	case mode == "flashback":
		v -= t.costValue(s, p.flashbackCost, obj)
	case alt > 0 && p.altCost != "":
		v -= t.costValue(s, p.altCost, obj)
	}
	if w.Timing {
		v -= t.timingPenalty(s, p, obj, v)
	}
	return v
}

func isModal(cv *view.CardView) bool {
	return cv != nil && (cv.SpellAPI == "Charm" || cv.SpellAPI == "GenericChoice")
}

// landCost is the cost of sacrificing a land: steep while we are still
// developing, cheap once we have plenty.
func (t *tactical) landCost(s *tstate) float64 {
	switch {
	case s.lands >= 7:
		return t.w.SacLand * 0.25
	case s.lands >= 5:
		return t.w.SacLand * 0.5
	}
	return t.w.SacLand
}

// discardCost is the cost of discarding a card other than obj: cheap when we
// hold surplus lands.
func (t *tactical) discardCost(s *tstate, obj state.ObjID) float64 {
	lands, others := 0, 0
	for i := range s.meP.Hand {
		cv := &s.meP.Hand[i]
		if cv.ID == obj {
			continue
		}
		if isLandView(cv) {
			lands++
		} else {
			others++
		}
	}
	switch {
	case lands+others == 0:
		return 100 // nothing to discard: the cast would fail or is pointless
	case lands > 0 && s.lands >= 4:
		return t.w.DiscardCost * 0.3
	}
	return t.w.DiscardCost
}

// profileCreValue values a creature card about to enter from its printed
// profile (the View's P/T for a card in hand is its printed P/T).
func (t *tactical) profileCreValue(s *tstate, p *tProfile, cv *view.CardView) float64 {
	c := &tcre{p: p, pow: p.power, tough: p.toughness, flying: p.flying, reach: p.reach,
		deathtouch: p.deathtouch, lifelink: p.lifelink, firstStrike: p.firstStrike,
		doubleStrike: p.doubleStrike, trample: p.trample, vigilance: p.vigilance, menace: p.menace,
		hexproof: p.hexproof, indest: p.indest, defender: p.defender, haste: p.haste}
	if cv != nil && cv.Power > 0 {
		c.pow, c.tough = cv.Power, cv.Toughness
	}
	if c.pow < 0 {
		c.pow = 0
	}
	v := s.creValue(c)
	if p.manaSource {
		mul := 1.0
		if t.w.EarlyGame && s.early {
			mul = t.w.EarlyRamp
		}
		v += t.w.Ramp * (mul*s.handPressure() - 1)
	}
	return v
}

// timingPenalty is group 2: how much casting p now loses to casting it in a
// better window. value is the cast's score before the penalty.
func (t *tactical) timingPenalty(s *tstate, p *tProfile, obj state.ObjID, value float64) float64 {
	w := &t.w
	answer := p.has(effCounter) || p.has(effRemoval) || p.has(effDamage) || p.has(effDebuff) ||
		p.has(effPump) || p.has(effFog) || p.has(effTap)
	off := func(base float64) float64 { return max(base, value*w.OffWindow) }
	switch {
	case !p.instant && !p.flash:
		// Sorcery speed (only offered in our own main phase): keep up a held
		// answer; optionally cast a non-haste creature after combat.
		if s.myTurn && s.main {
			pen := t.keepUpPenalty(s, obj, p)
			if s.main1 && p.creature && !p.haste && !p.playMain1 && s.willAttack() {
				pen += w.PostCombat
			}
			return pen
		}
		return 0
	case !s.myTurn && s.step == "end":
		return 0 // the opponent's end step: every instant-speed play's window
	case s.myTurn && s.main:
		// Our own main phase: an instant waits for the opponent's turn,
		// unless it is lethal now or it clears the way for our attack.
		if value >= w.Lethal {
			return 0
		}
		if answer && s.main1 && (p.has(effRemoval) || p.has(effDamage) && s.oppBlockersMatter()) {
			return w.HoldReactive * 0.25
		}
		return off(w.HoldReactive)
	case !s.myTurn:
		// The opponent's turn before its end step: answers go when needed (a
		// counter at a foreign spell, combat answers -- priced by effValue);
		// everything else waits for the end step.
		if p.has(effCounter) && s.foreign != nil || s.combat && answer {
			return 0
		}
		return off(w.WaitEOT)
	default:
		// Our own upkeep, draw, combat or end step: tricks in combat, the
		// rest waits for the opponent's end step.
		if s.combat && answer {
			return 0
		}
		return off(w.WaitEOT)
	}
}

// oppBlockersMatter reports whether the opponent has an untapped creature
// that could block one of our attackers this turn.
func (s *tstate) oppBlockersMatter() bool {
	if !s.willAttack() {
		return false
	}
	for _, c := range s.theirs {
		if !c.tapped {
			return true
		}
	}
	return false
}

// willAttack reports whether we have a creature able to attack this turn.
func (s *tstate) willAttack() bool {
	for _, c := range s.mine {
		if c.canAttack() && !c.tapped && (!c.sick || c.haste) {
			return true
		}
	}
	return false
}

// keepUpPenalty charges a proactive cast that leaves less mana than a held
// reactive card in hand costs, while the opponent has cards to cast.
func (t *tactical) keepUpPenalty(s *tstate, obj state.ObjID, p *tProfile) float64 {
	if s.oppP.HandSize == 0 {
		return 0
	}
	left := s.producible - p.cmc
	worst := 0.0
	for i := range s.meP.Hand {
		cv := &s.meP.Hand[i]
		if cv.ID == obj {
			continue
		}
		rp := t.profile(cv)
		if !rp.known || !rp.reactive() || rp.cmc <= left || rp.cmc > s.producible {
			continue
		}
		str := 2.0
		switch {
		case rp.has(effCounter):
			str = 4
		case rp.has(effRemoval) || rp.has(effDamage):
			str = 3
		}
		if str > worst {
			worst = str
		}
	}
	return t.w.KeepUp * worst
}

// abilityScore values activating ability index abil of obj now.
func (t *tactical) abilityScore(s *tstate, obj state.ObjID, abil int, pot bool) float64 {
	w := &t.w
	cv := s.objs[obj]
	p := t.profile(cv)
	if cv == nil || !p.known || abil < 0 || abil >= len(p.abilities) {
		return w.Unknown
	}
	ab := &p.abilities[abil]
	if ab.mana {
		return -1
	}
	if pot && !s.canAfford(manaPart(ab.cost), 0) {
		return -1
	}
	if cv.ActivatedThisTurn >= 2 && !ab.limit1 {
		return -1 // a loop guard: repeatable abilities twice a turn at most
	}
	zone := s.zoneOf(obj)
	v := 0.0
	switch {
	case ab.ninjutsu:
		v = w.Ninjutsu + w.Body*t.profileCreValue(s, p, cv)*0.3
	case ab.fromHand:
		v = t.cycleValue(s, p, ab)
	default:
		for i := range ab.effects {
			v += t.effValue(s, p, &ab.effects[i], obj)
		}
		if len(ab.effects) == 0 {
			v = w.Unknown
		}
	}
	// Costs.
	if ab.tapCost && zone == "battlefield" && p.creature {
		if c := s.cre[obj]; c != nil && s.myTurn && s.main1 && c.canAttack() && (!c.sick || c.haste) {
			v -= w.TapAttacker * float64(c.pow)
		}
	}
	if ab.sacSelf && zone == "battlefield" {
		if c := s.cre[obj]; c != nil {
			v -= s.creValue(c)
		} else if !isLandView(cv) {
			v -= 0.5 // a spent artifact / token
		}
	}
	v -= t.costValue(s, ab.cost, obj)
	if cv.AttachedTo != 0 && len(ab.effects) > 0 && ab.effects[0].class == effAttach {
		return -1 // equip onto the creature it already equips: a no-op
	}
	if !ab.sorcery && !ab.ninjutsu {
		combat := false
		for i := range ab.effects {
			switch ab.effects[i].class {
			case effPump, effDebuff, effRemoval, effDamage, effDamageAll, effTap, effUntap, effCounter, effFog:
				combat = true
			}
		}
		eot := !s.myTurn && s.step == "end"
		landNow := ab.fromHand && s.myTurn && s.main && !s.landInHand()
		switch {
		case combat || eot || landNow:
		case w.Timing:
			// Card flow (loot, clue, treasure, cycling) belongs at the
			// opponent's end step, when the mana has nothing better to do.
			v -= w.WaitEOT
		case s.myTurn && !s.main && !s.combat && ab.manaCost > 0:
			// Sanity, not tuning: never tap mana before our own main phase
			// for card flow (it empties before we can cast).
			v -= 5
		}
	}
	return v
}

func (s *tstate) landInHand() bool {
	for i := range s.meP.Hand {
		if isLandView(&s.meP.Hand[i]) {
			return true
		}
	}
	return false
}

// cycleValue values a from-hand ability (cycling / landcycling): the card
// found versus the card spent.
func (t *tactical) cycleValue(s *tstate, p *tProfile, ab *tAbility) float64 {
	w := &t.w
	landsInHand := 0
	for i := range s.meP.Hand {
		if isLandView(&s.meP.Hand[i]) {
			landsInHand++
		}
	}
	gain := w.Card * 0.6
	for i := range ab.effects {
		if e := ab.effects[i]; e.class == effTutor || e.class == effRamp {
			if landsInHand == 0 && s.lands < 6 {
				gain = w.Card * 1.3
			}
		}
	}
	// Keeping the card is worth its cast unless it is far off the curve.
	keep := w.Card
	if p.cmc > s.lands+int32(landsInHand)+1 {
		keep = w.Card * 0.3
	}
	return gain - keep + 0.5
}

// effValue values one effect of a cast / activation before its target is
// known: the best available target's worth.
func (t *tactical) effValue(s *tstate, p *tProfile, e *tEffect, src state.ObjID) float64 {
	w := &t.w
	earlyDraw, earlyRamp := 1.0, 1.0
	if w.EarlyGame && s.early {
		earlyDraw, earlyRamp = w.EarlyDraw, w.EarlyRamp
	}
	amount := float64(e.amount)
	if !e.known {
		if n, ok := s.countExpr(e.xExpr); ok {
			amount = float64(n)
		}
	}
	if amount <= 0 && e.known || amount <= 0 && e.xExpr == "" {
		amount = 1
	}
	switch e.class {
	case effDraw:
		if s.deckingRisk() {
			return -w.Card * amount // a stall is a deck-out race: do not draw
		}
		// Cards past the hand-size limit are discarded at cleanup: they
		// are worth only the selection.
		room := float64(max(0, 8-s.meP.HandSize))
		kept := min(amount, room)
		return (w.Card*kept + 0.2*w.Card*(amount-kept)) * earlyDraw
	case effSelect:
		return w.Select * earlyDraw
	case effTutor:
		if s.deckingRisk() {
			return 0
		}
		return w.Card * 0.8 * amount * earlyDraw
	case effRamp:
		if e.api == "Mana" {
			return w.ManaSpent * 2 // ritual mana on entry: a refund
		}
		return w.Ramp * earlyRamp * s.handPressure()
	case effToken:
		return w.Token * amount
	case effClue:
		return w.Clue * amount
	case effInitiative:
		return w.Initiative
	case effRemoval, effDamage, effDebuff, effTap:
		return t.bestTargetValue(s, e, src, nil)
	case effDamageAll:
		return t.damageAllValue(s, e, p, src)
	case effDrain:
		return s.faceValue(t.damageAmount(s, e, p))
	case effCounter:
		return t.counterValue(s, e, src)
	case effPump:
		if e.self || !e.targeted {
			return t.selfPumpValue(s, e, src)
		}
		return t.bestTargetValue(s, e, src, nil)
	case effPumpAll:
		return t.pumpAllValue(s, e)
	case effLifeGain:
		return s.lifeValue(s.myLife, int32(amount)) * 0.8
	case effDiscard:
		if s.oppP.HandSize > 0 {
			return w.Discard * amount
		}
	case effLoot:
		return -t.discardCost(s, src) * 0.5 * amount
	case effUntap:
		if e.lands {
			return w.ManaSpent * amount
		}
		return 0.3
	case effRecursion:
		if e.toBattlefield {
			return t.reanimateValue(s)
		}
		n := 0
		for i := range s.meP.Graveyard {
			if isCreatureTypes(s.meP.Graveyard[i].Types) {
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return w.Card * 0.8 * min(amount, float64(n))
	case effGraveHate:
		return 0.3
	case effFog:
		return t.fogValue(s)
	case effAttach:
		for _, c := range s.mine {
			if c.id != src {
				return 1.5
			}
		}
		return -1
	}
	return 0
}

// damageAmount is an effect's damage, resolving the metalcraft shape and
// defaulting a variable amount to 2.
func (t *tactical) damageAmount(s *tstate, e *tEffect, p *tProfile) int32 {
	if e.known && e.amount > 0 {
		return e.amount
	}
	if n, ok := s.countExpr(e.xExpr); ok {
		return n
	}
	return 2
}

func (s *tstate) artifacts() int {
	n := 0
	for i := range s.meP.Battlefield {
		if strings.Contains(s.meP.Battlefield[i].Types, "Artifact") {
			n++
		}
	}
	return n
}

// counterValue values a counterspell at the top foreign spell.
func (t *tactical) counterValue(s *tstate, e *tEffect, src state.ObjID) float64 {
	f := s.foreign
	// Nothing of theirs to counter: a counterspell would hit our own spell.
	// A counter TRIGGER (Spellstutter Sprite) with no spell on the stack at
	// all simply has no target and costs nothing.
	miss := -50.0
	if e.trigger && s.stackEmpty {
		miss = 0
	}
	if f == nil || f.Card == nil {
		return miss
	}
	cv := f.Card
	if !counterAdmits(s, e, cv, src) {
		return miss
	}
	mv := float64(t.profile(cv).cmc)
	v := t.w.Counter + t.w.CounterMV*mv
	if isCreatureTypes(cv.Types) {
		v += 0.5 * t.w.Body * float64(cv.Power+cv.Toughness)
	}
	if e.unless {
		// A soft counter: worthless into open mana.
		if s.oppUntappedMana() >= e.unlessN {
			return -5
		}
	}
	return v
}

// counterAdmits checks the few ValidTgts shapes counterspells print.
func counterAdmits(s *tstate, e *tEffect, cv *view.CardView, src state.ObjID) bool {
	valid := e.valid
	switch {
	case valid == "Instant":
		return strings.Contains(cv.Types, "Instant")
	case strings.Contains(valid, "nonCreature"):
		return !isCreatureTypes(cv.Types)
	case strings.Contains(valid, "cmcLEX"):
		// "mana value X or less", X the card's own count SVar (Spellstutter
		// Sprite: Count$Valid Faerie.YouCtrl), counting the source itself
		// when it is still being cast from hand.
		x, ok := s.countExpr(e.xExpr)
		if !ok {
			return true
		}
		if s.zoneOf(src) == "hand" {
			x++
		}
		return botpolicyCMC(cv.ManaCost) <= x
	}
	return true
}

// oppUntappedMana counts the opponent's untapped mana sources.
func (s *tstate) oppUntappedMana() int32 {
	var n int32
	for i := range s.oppP.Battlefield {
		cv := &s.oppP.Battlefield[i]
		if !cv.Tapped && cv.Produces != nil {
			n++
		}
	}
	return n
}

// damageAllValue values a sweeper: opposing creatures killed minus ours, plus
// damage to each opponent when it names players.
func (t *tactical) damageAllValue(s *tstate, e *tEffect, p *tProfile, src state.ObjID) float64 {
	dmg := t.damageAmount(s, e, p)
	v := 0.0
	for _, c := range append(append([]*tcre(nil), s.mine...), s.theirs...) {
		tg := ttarget{pid: s.opp, obj: c.id, cv: c.cv, c: c}
		if c.mine {
			tg.pid = s.me
		}
		if e.valid != "" && !validMatch(s, e.valid, &tg) || c.indest || c.remTough() > dmg {
			continue
		}
		cv := s.creValue(c)
		if c.mine {
			v -= cv
		} else {
			v += cv*t.w.Removal + s.keyPiece(c)
		}
	}
	if strings.Contains(e.valid, "Opponent") || e.players {
		v += s.faceValue(dmg)
	}
	return v
}

// selfPumpValue values a pump / counter onto the source itself.
func (t *tactical) selfPumpValue(s *tstate, e *tEffect, src state.ObjID) float64 {
	if e.api == "PutCounter" {
		return 0.8 * float64(e.att+e.def)
	}
	return 0.3
}

// pumpAllValue values a team pump: extra damage this turn when we attack
// (Goblin Bushwhacker's +1/+0 and haste; Rally's haste).
func (t *tactical) pumpAllValue(s *tstate, e *tEffect) float64 {
	if !s.myTurn || !s.main1 {
		return 0
	}
	haste := strings.Contains(e.kw, "Haste")
	v := 0.0
	for _, c := range s.mine {
		if c.tapped || c.defender {
			continue
		}
		if c.sick && !c.haste && !haste {
			continue
		}
		v += float64(e.att) + 0.5*float64(c.pow)*b2f(c.sick && haste)
	}
	return v * t.w.Trick
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func botpolicyCMC(mc string) int32 { return botpolicy.CmcOf(mc) }

// reanimateValue is the best creature card in our graveyard put onto the
// battlefield: its body plus its enter-the-battlefield effects (Lotleth
// Giant's damage per creature card in our graveyard).
func (t *tactical) reanimateValue(s *tstate) float64 {
	best := 0.0
	for i := range s.meP.Graveyard {
		cv := &s.meP.Graveyard[i]
		if !isCreatureTypes(cv.Types) {
			continue
		}
		p := t.profile(cv)
		v := t.w.Body * t.profileCreValue(s, p, cv)
		for j := range p.etb {
			v += t.effValue(s, p, &p.etb[j], cv.ID)
		}
		if v > best {
			best = v
		}
	}
	return best
}

// kickerExtra is the extra generic mana a kicked pursuit needs (an estimate:
// one).
func kickerExtra(mode string) int32 {
	if strings.HasPrefix(mode, "kicked") {
		return 1
	}
	return 0
}

// manaPart strips an ability cost's non-mana parts ("2 U T Sac<1/X>" ->
// "2 U").
func manaPart(cost string) string {
	var out []string
	for _, f := range strings.Fields(cost) {
		if strings.ContainsAny(f, "<>") || f == "T" || f == "Q" {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

func (t *tactical) timingPenaltyIf(s *tstate, p *tProfile, obj state.ObjID, v float64) float64 {
	if !t.w.Timing {
		return 0
	}
	return t.timingPenalty(s, p, obj, v)
}

// fogValue values preventing this combat's damage: only on the opponent's
// turn once blockers are declared, the unblocked damage we would take plus
// our blockers that would die.
func (t *tactical) fogValue(s *tstate) float64 {
	if s.myTurn || !s.combat || !s.blocksDone {
		return -1
	}
	var dmg int32
	v := 0.0
	for _, a := range s.theirs {
		if !a.attacking {
			continue
		}
		if !a.blocked {
			dmg += a.pow * (1 + int32(b2f(a.doubleStrike)))
			continue
		}
		var bs []*tcre
		for _, id := range a.cv.BlockedBy {
			if b := s.cre[id]; b != nil {
				bs = append(bs, b)
			}
		}
		r := fightOutcome(a, bs, 0, 0, false)
		for i, dead := range r.foeDies {
			if dead {
				v += s.creValue(bs[i])
			}
		}
	}
	v += s.lifeValue(s.myLife, dmg)
	if dmg >= s.myLife {
		v += t.w.Lethal
	}
	return v
}
