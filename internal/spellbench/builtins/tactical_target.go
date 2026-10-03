package builtins

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ttarget is one prospective target: a player, a battlefield object, or a
// stack object.
type ttarget struct {
	player   bool
	pid      state.PlayerID // the player, or the object's controller
	obj      state.ObjID
	cv       *view.CardView
	c        *tcre // when a battlefield creature
	stackObj bool
}

// validMatch is a conservative reading of a ValidTgts$ string: comma
// alternatives of Type[.qual+qual]. Unknown qualifiers pass.
func validMatch(s *tstate, valid string, tg *ttarget) bool {
	if valid == "" {
		return true
	}
	for _, alt := range strings.Split(valid, ",") {
		alt = strings.TrimSpace(alt)
		typ, quals, _ := strings.Cut(alt, ".")
		if !typeMatch(typ, tg) {
			continue
		}
		ok := true
		for _, q := range strings.Split(quals, "+") {
			switch q {
			case "OppCtrl", "Opponent":
				ok = ok && tg.pid != s.me
			case "YouCtrl", "YouOwn", "You":
				ok = ok && tg.pid == s.me
			case "withoutFlying":
				ok = ok && (tg.c == nil || !tg.c.flying)
			case "nonCreature":
				ok = ok && (tg.cv == nil || !isCreatureTypes(tg.cv.Types))
			case "tapped":
				ok = ok && tg.cv != nil && tg.cv.Tapped
			}
		}
		if typ == "Opponent" {
			ok = ok && tg.pid != s.me
		}
		if ok {
			return true
		}
	}
	return false
}

func typeMatch(typ string, tg *ttarget) bool {
	switch typ {
	case "Any":
		return tg.player || tg.c != nil || (tg.cv != nil && strings.Contains(tg.cv.Types, "Planeswalker"))
	case "Player", "Opponent":
		return tg.player
	case "Card":
		return !tg.player
	case "Permanent":
		return !tg.player && !tg.stackObj
	case "Creature":
		return tg.c != nil
	}
	if tg.player || tg.cv == nil {
		return false
	}
	return strings.Contains(tg.cv.Types, typ)
}

// candidateTargets lists the board's prospective targets for an effect at
// cast time (players, then battlefield permanents in view order).
func (s *tstate) candidateTargets(e *tEffect) []ttarget {
	var out []ttarget
	for _, p := range []*view.PlayerView{s.oppP, s.meP} {
		out = append(out, ttarget{player: true, pid: p.ID})
	}
	for _, p := range []*view.PlayerView{s.oppP, s.meP} {
		for i := range p.Battlefield {
			cv := &p.Battlefield[i]
			out = append(out, ttarget{pid: p.ID, obj: cv.ID, cv: cv, c: s.cre[cv.ID]})
		}
	}
	keep := out[:0]
	for i := range out {
		if validMatch(s, e.valid, &out[i]) && !(out[i].c != nil && out[i].c.hexproof && out[i].pid != s.me) {
			keep = append(keep, out[i])
		}
	}
	return keep
}

// bestTargetValue is the best value, to us, of pointing e at any
// prospective target (riders from chain included). With no target the
// effect does nothing (0).
func (t *tactical) bestTargetValue(s *tstate, e *tEffect, src state.ObjID, chain []tEffect) float64 {
	best, any := 0.0, false
	for _, tg := range s.candidateTargets(e) {
		tg := tg
		v := t.targetValue(s, e, &tg, src, chain, -1)
		if !any || v > best {
			best, any = v, true
		}
	}
	return best
}

// targetValue is the worth to us of e landing on tg: the change to the
// target controller's position, signed by whose it is, plus riders that
// benefit the target's controller (DefinedPlayer$ TargetedController).
// dmgOverride >= 0 is the decision's own damage figure.
func (t *tactical) targetValue(s *tstate, e *tEffect, tg *ttarget, src state.ObjID, chain []tEffect, dmgOverride int32) float64 {
	w := &t.w
	delta := 0.0 // change to the target controller's position
	mine := tg.pid == s.me
	switch {
	case tg.player:
		switch e.class {
		case effDamage, effDrain:
			dmg := t.effDamage(s, e, src, dmgOverride)
			if mine {
				return -s.ownLifeValue(dmg) * 2
			}
			return s.faceValue(dmg)
		case effMill:
			// Milling a library: a real threat to a small library (and
			// Balustrade Spy mills a land-light deck to nothing). Ours is
			// never milled: decking ourselves is a loss.
			if mine {
				return -2 * s.millValue(s.meP)
			}
			return s.millValue(s.oppP)
		case effDiscard, effGraveHate:
			if mine {
				return -w.Discard
			}
			if e.class == effDiscard {
				return w.Discard
			}
			return 0.3
		case effDraw:
			if mine {
				return w.Card
			}
			return -w.Card
		case effLifeGain:
			if mine {
				return s.ownLifeValue(max(e.amount, 1))
			}
			return -1
		}
		if e.class.polarity() < 0 {
			if mine {
				return -1
			}
			return 0.5
		}
		if mine {
			return 0.5
		}
		return -0.5
	case tg.stackObj:
		if e.class == effCounter {
			if mine {
				return -50
			}
			return w.Counter + w.CounterMV*float64(t.profile(tg.cv).cmc)
		}
		if mine {
			return -1
		}
		return 0
	case tg.c != nil:
		delta = t.creatureDelta(s, e, tg.c, src, dmgOverride)
		if !mine && delta < 0 {
			delta -= s.keyPiece(tg.c) // removing a key piece is worth more
			if w.Timing && tg.c.attacking && !s.myTurn {
				delta -= w.KillAttacker
			}
		}
	default:
		delta = t.permanentDelta(s, e, tg)
	}
	// Riders that go to the target's controller.
	rider := 0.0
	for i := range chain {
		r := &chain[i]
		if !r.rider {
			continue
		}
		switch r.class {
		case effRamp:
			if r.lands {
				rider += s.landValue() // a replacement land (Cleansing Wildfire)
			} else {
				rider += w.Ramp
			}
		case effDraw, effTutor:
			rider += w.Card
		}
	}
	if mine {
		return delta + rider
	}
	return -delta - rider
}

// effDamage is e's damage: the decision's figure when known, else the IR's.
func (t *tactical) effDamage(s *tstate, e *tEffect, src state.ObjID, override int32) int32 {
	if override >= 0 {
		return override
	}
	var p *tProfile
	if cv := s.objs[src]; cv != nil {
		p = t.profile(cv)
	}
	return t.damageAmount(s, e, p)
}

// creatureDelta is the change to a creature controller's position when e
// hits creature c (negative = harm), in points.
func (t *tactical) creatureDelta(s *tstate, e *tEffect, c *tcre, src state.ObjID, dmgOverride int32) float64 {
	w := &t.w
	val := s.creValue(c)
	switch e.class {
	case effRemoval:
		if e.api == "Destroy" && c.indest {
			return 0
		}
		if e.bounce {
			return -(val*0.5 + 0.5*float64(t.profileCMC(c)))
		}
		return -val * w.Removal
	case effDamage:
		dmg := t.effDamage(s, e, src, dmgOverride)
		if dmg >= c.remTough() && !c.indest {
			return -val * w.Removal
		}
		return -0.1 * float64(dmg)
	case effDebuff:
		if e.def < 0 && c.remTough()+e.def <= 0 && !c.indest {
			return -val * w.Removal
		}
		return -t.combatSwing(s, c, e.att, e.def, "", true)
	case effPump:
		att, def := e.att, e.def
		if n, ok := s.countExpr(e.xExpr); ok && e.xExpr != "" {
			att, def = n, n
		}
		return t.combatSwing(s, c, att, def, e.kw, false)
	case effTap:
		if c.tapped {
			return 0
		}
		return -0.3 - 0.3*float64(c.pow)
	case effUntap:
		if !c.tapped {
			return 0
		}
		if c.p != nil && c.p.manaSource {
			return w.ManaSpent
		}
		return 0.5
	case effAttach:
		return 1.5
	}
	return 0
}

func (t *tactical) profileCMC(c *tcre) int32 {
	if c.p != nil {
		return c.p.cmc
	}
	return 1
}

// permanentDelta is the change to a noncreature permanent's controller when
// e hits it.
func (t *tactical) permanentDelta(s *tstate, e *tEffect, tg *ttarget) float64 {
	if tg.cv == nil {
		return 0
	}
	cv := tg.cv
	indest := false
	for _, k := range cv.Keywords {
		if strings.EqualFold(k, "Indestructible") {
			indest = true
		}
	}
	val := 1.5 + 0.7*float64(botpolicyCMC(cv.ManaCost))
	if isLandView(cv) {
		val = s.landValue()
	}
	switch e.class {
	case effRemoval:
		if e.api == "Destroy" && indest {
			return 0
		}
		if e.bounce {
			return -val * 0.5
		}
		return -val
	case effTap:
		return -0.2
	case effUntap:
		if cv.Tapped && cv.Produces != nil {
			return t.w.ManaSpent
		}
	case effRecursion:
		// A graveyard card coming back. Every candidate used to score 0,
		// so the lowest option index won the tie.
		if e.toBattlefield && isCreatureTypes(cv.Types) && !t.reanimating {
			t.reanimating = true
			defer func() { t.reanimating = false }()
			return t.reanimateCardValue(s, cv)
		}
		return val
	}
	return 0
}

// combatSwing values a temporary P/T (and keyword) change on c in the
// current combat, from c's controller's side: positive when it helps c's
// side. Outside a combat involving c it is a small constant.
func (t *tactical) combatSwing(s *tstate, c *tcre, att, def int32, kw string, harm bool) float64 {
	sign := 1.0
	if harm {
		att, def = -att, -def
		sign = -1
	}
	_ = sign
	kwDT := strings.Contains(kw, "Deathtouch")
	kwLL := strings.Contains(kw, "Lifelink")
	// Which creatures fight c this combat?
	var foes []*tcre
	if c.attacking {
		for _, id := range c.cv.BlockedBy {
			if f := s.cre[id]; f != nil {
				foes = append(foes, f)
			}
		}
	} else {
		for _, a := range append(append([]*tcre(nil), s.mine...), s.theirs...) {
			if a.attacking && a.mine != c.mine {
				for _, id := range a.cv.BlockedBy {
					if id == c.id {
						foes = append(foes, a)
					}
				}
			}
		}
	}
	if len(foes) == 0 {
		if c.attacking && s.blocksDone && att > 0 {
			// Unblocked: the pump is extra damage to the defender.
			v := 0.0
			if c.mine {
				v = s.faceValue(att)
			} else {
				v = s.ownLifeValue(att)
			}
			if harm {
				return -v
			}
			return v
		}
		if !c.mine && harm && c.attacking {
			// Shrinking an attacker before blocks: damage prevented.
			return float64(-att) * 0.5
		}
		return 0.2 * float64(att+def)
	}
	before := fightOutcome(c, foes, 0, 0, false)
	after := fightOutcome(c, foes, att, def, kwDT)
	v := 0.0
	if before.cDies && !after.cDies {
		v += s.creValue(c)
	}
	if !before.cDies && after.cDies {
		v -= s.creValue(c)
	}
	for i, f := range foes {
		if !before.foeDies[i] && after.foeDies[i] {
			v += s.creValue(f)
		}
		if before.foeDies[i] && !after.foeDies[i] {
			v -= s.creValue(f)
		}
	}
	if kwLL {
		v += 0.3 * float64(c.pow)
	}
	if v == 0 {
		v = 0.1 * float64(att+def)
	}
	return v * t.w.Trick
}

type fightResult struct {
	cDies   bool
	foeDies []bool
}

// fightOutcome resolves c against foes (c's blockers, or the attacker c
// blocks) with c pumped by +att/+def (and deathtouch when dt): first and
// double strike, deathtouch and indestructible; c assigns its damage over
// foes in order, lethal to each before the next.
func fightOutcome(c *tcre, foes []*tcre, att, def int32, dt bool) fightResult {
	r := fightResult{foeDies: make([]bool, len(foes))}
	cp := max(c.pow+att, 0)
	ct := c.remTough() + def
	cdt := c.deathtouch || dt
	foeDmg := make([]int32, len(foes))
	var cDmg int32
	cDT := false
	cStrikes := func() {
		left := cp
		for i, f := range foes {
			if r.foeDies[i] || left <= 0 {
				continue
			}
			need := f.remTough() - foeDmg[i]
			if cdt {
				need = min(need, 1)
			}
			hit := min(left, max(need, 0))
			if i == len(foes)-1 {
				hit = left
			}
			foeDmg[i] += hit
			left -= hit
			if (foeDmg[i] >= f.remTough() || cdt && hit > 0) && !f.indest {
				r.foeDies[i] = true
			}
		}
	}
	foesStrike := func(first bool) {
		for i, f := range foes {
			if r.foeDies[i] {
				continue
			}
			fs := f.firstStrike || f.doubleStrike
			if first && !fs || !first && f.firstStrike && !f.doubleStrike {
				continue
			}
			cDmg += f.pow
			if f.deathtouch && f.pow > 0 {
				cDT = true
			}
		}
	}
	cFirst := c.firstStrike || c.doubleStrike
	// First-strike damage step.
	if cFirst {
		cStrikes()
	}
	foesStrike(true)
	if (cDmg >= ct || cDT) && !c.indest {
		r.cDies = true
	}
	// Regular damage step (simultaneous): foes dead from first strike deal
	// none; c strikes if it survived.
	foesStrike(false)
	if !r.cDies && (!cFirst || c.doubleStrike) {
		cStrikes()
	}
	if (cDmg >= ct || cDT) && !c.indest {
		r.cDies = true
	}
	return r
}

// targets answers a KTarget decision with direction-aware scoring. ok is
// false when the effect cannot be read (the default policy answers).
func (t *tactical) targets(v view.View, d *decision.Decision) (decision.Intent, bool) {
	sc, ok := t.scoreTargets(v, d)
	if !ok {
		return decision.Intent{}, false
	}
	var chosen []int
	for _, x := range sc {
		if len(chosen) >= d.Max {
			break
		}
		if len(chosen) >= d.Min && x.v <= 0 {
			break
		}
		o := &d.Options[x.idx]
		if !admissible(d, chosen, o) {
			continue
		}
		chosen = append(chosen, x.idx)
	}
	return repaired(d, decision.Intent{Choices: chosen}), true
}

// scoredTarget is one KTarget option and its tactical value.
type scoredTarget struct {
	idx int
	v   float64
}

// scoreTargets values every option of a KTarget decision, best first (ties
// to the lower option index). ok is false when the effect cannot be read.
func (t *tactical) scoreTargets(v view.View, d *decision.Decision) ([]scoredTarget, bool) {
	if d.TargetEffect == nil || len(d.Options) == 0 {
		return nil, false
	}
	s := t.newState(&v, d.Player)
	e, chain := t.findEffect(s, d)
	if e == nil {
		return nil, false
	}
	dmg := int32(-1)
	if d.TargetEffect.Damage != nil && d.TargetEffect.Damage.Amount != nil {
		dmg = int32(*d.TargetEffect.Damage.Amount)
	}
	var sc []scoredTarget
	for i := range d.Options {
		o := &d.Options[i]
		tg := ttarget{}
		if o.Kind == "player" {
			tg.player, tg.pid = true, o.Player
		} else {
			tg.obj = o.Obj
			tg.cv = s.objs[o.Obj]
			tg.c = s.cre[o.Obj]
			tg.pid = o.Controller
			if ctl, ok := s.owner[o.Obj]; ok {
				tg.pid = ctl
			}
			for j := range v.Stack {
				if v.Stack[j].ID == o.Obj {
					tg.stackObj = true
					tg.pid = v.Stack[j].Controller
				}
			}
		}
		sc = append(sc, scoredTarget{idx: o.Index, v: t.targetValue(s, e, &tg, d.Source, chain, dmg)})
	}
	sort.SliceStable(sc, func(i, j int) bool {
		if sc[i].v != sc[j].v {
			return sc[i].v > sc[j].v
		}
		return sc[i].idx < sc[j].idx
	})
	return sc, true
}

// findEffect locates the targeted effect a KTarget decision is for: the
// first targeted effect of the source card whose API matches the
// decision's, with the chain it belongs to (for riders).
func (t *tactical) findEffect(s *tstate, d *decision.Decision) (*tEffect, []tEffect) {
	cv := s.objs[d.Source]
	if cv == nil {
		for i := range s.v.Stack {
			sv := &s.v.Stack[i]
			if sv.ID == d.Source || sv.Source == d.Source {
				if sv.Card != nil {
					cv = sv.Card
				} else if src := s.objs[sv.Source]; src != nil {
					cv = src
				}
			}
		}
	}
	p := t.profile(cv)
	if !p.known {
		return nil, nil
	}
	api := d.TargetEffect.API
	chains := [][]tEffect{p.spell, p.etb, p.dies}
	for i := range p.abilities {
		chains = append(chains, p.abilities[i].effects)
	}
	for _, ch := range chains {
		for i := range ch {
			if ch[i].api == api && ch[i].targeted && ch[i].class != effOther {
				return &ch[i], ch
			}
		}
	}
	return nil, nil
}

// landValue is a land's worth on the battlefield: high while developing.
func (s *tstate) landValue() float64 {
	if s.lands >= 6 {
		return 3
	}
	return 5
}

// millValue is the worth of milling player p "until a land": the expected
// cards milled from p's library, estimated from the land share of the
// cards p has shown (battlefield, graveyard, exile) against a 40% prior
// worth three cards. A library the mill empties is a win (p decks out).
func (s *tstate) millValue(p *view.PlayerView) float64 {
	seen, lands := 0.0, 0.0
	for _, z := range [][]view.CardView{p.Battlefield, p.Graveyard, p.Exile} {
		for i := range z {
			seen++
			if isLandView(&z[i]) {
				lands++
			}
		}
	}
	frac := (lands + 0.4*3) / (seen + 3)
	lib := float64(p.LibrarySize)
	left := frac*(lib+seen+float64(p.HandSize)) - lands - frac*float64(p.HandSize)
	if left < 0.5 {
		left = 0.5
	}
	expect := lib / (left + 1)
	if expect >= lib-1 {
		return s.w.MillOpp * 10
	}
	return s.w.MillOpp * (1 + expect/5 + float64(max(0, 30-p.LibrarySize))/10)
}
