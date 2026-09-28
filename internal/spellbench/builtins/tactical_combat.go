package builtins

import (
	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// attackers answers KAttackers (group 3, Race). Without the Race group it
// is gorge's default attacker. With it: an alpha strike when the whole team
// is lethal through the defender's best blocks, else gorge's combat
// simulation (botpolicy.AttackSimDecide: whole attacking sets, predicted
// blocks and the crack-back, scored on public board facts only).
func (t *tactical) attackers(v view.View, d *decision.Decision) decision.Intent {
	brd := seat.BoardFromView(v)
	if !t.w.Race {
		return repaired(d, botpolicy.Decide(brd, d, t.rng))
	}
	s := t.newState(&v, d.Player)
	var atk []*tcre
	var all []int
	for _, g := range groupByObj(d) {
		c := s.cre[g.obj]
		pick := -1
		for _, i := range g.opts {
			o := &d.Options[i]
			if o.Battle == 0 && o.Player == s.opp {
				pick = i
				break
			}
		}
		if c == nil || pick < 0 {
			continue
		}
		atk = append(atk, c)
		all = append(all, pick)
	}
	if len(atk) > 0 && t.oppNeverBlocks() {
		// An opponent that never blocks is a pure race: swing with
		// everything when our all-in clock is no slower than theirs.
		mine, theirs := 0.0, 0.0
		for _, c := range atk {
			mine += float64(c.pow) * (1 + b2f(c.doubleStrike))
		}
		for _, c := range s.theirs {
			if c.canAttack() {
				theirs += float64(c.pow) * (1 + b2f(c.doubleStrike))
			}
		}
		if clock(s.oppLife, mine) <= clock(s.myLife, theirs) {
			return repaired(d, decision.Intent{Choices: botpolicy.LegalAttackChoices(brd, d, all)})
		}
	}
	if len(atk) > 0 && throughDamage(atk, s.theirs, true) >= float64(s.oppLife) {
		in := decision.Intent{Choices: botpolicy.LegalAttackChoices(brd, d, all)}
		return repaired(d, in)
	}
	return repaired(d, botpolicy.AttackSimDecide(brd, d, t.rng, t.sim))
}

// throughDamage is the damage attackers deal when the defenders block to
// minimise it: each untapped defender (untapped only when live is true)
// blocks the attacker that would deal the most, fliers only by fliers or
// reach, menace needing two; trample carries the excess over the blocker.
func throughDamage(atk, def []*tcre, live bool) float64 {
	as := append([]*tcre(nil), atk...)
	sortCre(as, func(x, y *tcre) bool { return x.pow > y.pow || (x.pow == y.pow && x.id < y.id) })
	var blockers []*tcre
	for _, b := range def {
		if live && b.tapped {
			continue
		}
		blockers = append(blockers, b)
	}
	used := make([]bool, len(blockers))
	total := 0.0
	for _, a := range as {
		p := float64(a.pow)
		if a.doubleStrike {
			p *= 2
		}
		need := 1
		if a.menace {
			need = 2
		}
		var got []int
		if !a.unblockable {
			for i, b := range blockers {
				if used[i] || a.flying && !(b.flying || b.reach) {
					continue
				}
				got = append(got, i)
				if len(got) == need {
					break
				}
			}
		}
		if len(got) < need {
			total += p
			continue
		}
		soak := 0.0
		for _, i := range got {
			used[i] = true
			soak += float64(max(blockers[i].remTough(), 0))
		}
		if a.trample && p > soak {
			total += p - soak
		}
	}
	return total
}

// blockers answers KBlockers (group 3, Race). Without the Race group it is
// gorge's default blocker. With it the incumbent is gorge's simulated block
// search, then three race rules adjust it:
//
//  1. survive: if the damage getting through is lethal, chump the biggest
//     unblocked attackers with our least valuable free creatures;
//  2. keep attackers: when we win the race and the hit is survivable, drop
//     chump blocks (our blocker dies, the attacker lives) so our attack
//     keeps its bodies;
//  3. double-block: a big unblocked attacker two free blockers kill
//     together is blocked by both when what it kills is worth less.
func (t *tactical) blockers(v view.View, d *decision.Decision) decision.Intent {
	brd := seat.BoardFromView(v)
	if !t.w.Race {
		return repaired(d, botpolicy.Decide(brd, d, t.rng))
	}
	base := botpolicy.AttackSimDecide(brd, d, t.rng, t.sim)
	s := t.newState(&v, d.Player)
	for i := range d.Options {
		if d.Options[i].MinBlockers != 0 || d.Options[i].MaxBlockers != 0 {
			return repaired(d, base) // blocker-count bounds: keep the incumbent
		}
	}
	// pair -> option index
	type pair struct{ blk, atk state.ObjID }
	opt := map[pair]int{} // lookup only
	var attackers []*tcre
	seenA := map[state.ObjID]bool{} // lookup only
	var myBlk []*tcre
	seenB := map[state.ObjID]bool{} // lookup only
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != "block" {
			return repaired(d, base)
		}
		a, b := s.cre[o.Attacker], s.cre[o.Obj]
		if a == nil || b == nil {
			return repaired(d, base)
		}
		opt[pair{o.Obj, o.Attacker}] = o.Index
		if !seenA[a.id] {
			seenA[a.id] = true
			attackers = append(attackers, a)
		}
		if !seenB[b.id] {
			seenB[b.id] = true
			myBlk = append(myBlk, b)
		}
	}
	// Attackers no option names (unblockable) still hit us.
	for _, c := range s.theirs {
		if c.attacking && !seenA[c.id] {
			attackers = append(attackers, c)
			seenA[c.id] = true
		}
	}
	blocksOf := map[state.ObjID][]*tcre{} // attacker -> our blockers (lookup only)
	usedB := map[state.ObjID]bool{}       // lookup only
	for _, i := range base.Choices {
		o := &d.Options[i]
		blocksOf[o.Attacker] = append(blocksOf[o.Attacker], s.cre[o.Obj])
		usedB[o.Obj] = true
	}
	incoming := func() int32 {
		var n int32
		for _, a := range attackers {
			bs := blocksOf[a.id]
			p := a.pow
			if a.doubleStrike {
				p *= 2
			}
			if len(bs) == 0 {
				n += p
				continue
			}
			if a.trample {
				var soak int32
				for _, b := range bs {
					soak += max(b.remTough(), 0)
				}
				n += max(p-soak, 0)
			}
		}
		return n
	}
	free := func() []*tcre {
		var out []*tcre
		for _, b := range myBlk {
			if !usedB[b.id] {
				out = append(out, b)
			}
		}
		sortCre(out, func(x, y *tcre) bool {
			vx, vy := s.creValue(x), s.creValue(y)
			return vx < vy || (vx == vy && x.id < y.id)
		})
		return out
	}
	// Group 4: against burn/aggro, life is a resource they are spending, so
	// chump earlier than the pure race rules would: spend our least valuable
	// free blocker on the biggest unblocked attacker even when the hit is not
	// lethal. Scaled by ArchBlock, which is 1.0 when the group is off.
	if (s.arch.burnThreat > 0.15 || s.arch.wideRisk > 0.3) && t.w.ArchBlock > 1 && incoming() < s.myLife {
		as := append([]*tcre(nil), attackers...)
		sortCre(as, func(x, y *tcre) bool { return x.pow > y.pow || (x.pow == y.pow && x.id < y.id) })
		for _, a := range as {
			if len(blocksOf[a.id]) > 0 || a.menace || a.pow < 2 {
				continue
			}
			for _, b := range free() {
				if _, ok := opt[pair{b.id, a.id}]; ok {
					blocksOf[a.id] = append(blocksOf[a.id], b)
					usedB[b.id] = true
					break
				}
			}
			break
		}
	}
	// Rule 1: survive.
	if incoming() >= s.myLife {
		as := append([]*tcre(nil), attackers...)
		sortCre(as, func(x, y *tcre) bool { return x.pow > y.pow || (x.pow == y.pow && x.id < y.id) })
		for _, a := range as {
			if incoming() < s.myLife {
				break
			}
			if len(blocksOf[a.id]) > 0 || a.menace {
				continue
			}
			for _, b := range free() {
				if _, ok := opt[pair{b.id, a.id}]; ok {
					blocksOf[a.id] = append(blocksOf[a.id], b)
					usedB[b.id] = true
					break
				}
			}
		}
	} else if s.ahead() {
		// Rule 2: keep attackers when the race is ours and the hit is safe.
		for _, a := range attackers {
			bs := blocksOf[a.id]
			if len(bs) != 1 {
				continue
			}
			r := fightOutcome(a, bs, 0, 0, false)
			if !r.foeDies[0] || r.cDies {
				continue
			}
			p := a.pow
			if a.doubleStrike {
				p *= 2
			}
			after := s.myLife - incoming() - p
			if after <= 0 || clock(after, s.oppDmg) <= s.myClock {
				continue
			}
			delete(blocksOf, a.id)
			usedB[bs[0].id] = false
		}
	}
	// Rule 3: double-block a big unblocked attacker.
	for _, a := range attackers {
		if len(blocksOf[a.id]) > 0 {
			continue
		}
		fr := free()
		bestV, b1, b2 := 0.5, -1, -1
		for i := 0; i < len(fr); i++ {
			if _, ok := opt[pair{fr[i].id, a.id}]; !ok {
				continue
			}
			for j := i + 1; j < len(fr); j++ {
				if _, ok := opt[pair{fr[j].id, a.id}]; !ok {
					continue
				}
				pairB := []*tcre{fr[i], fr[j]}
				// a assigns damage; fightOutcome from the attacker's side.
				r := fightOutcome(a, pairB, 0, 0, false)
				if !r.cDies {
					continue
				}
				val := s.creValue(a)
				for k, dead := range r.foeDies {
					if dead {
						val -= s.creValue(pairB[k])
					}
				}
				if val > bestV {
					bestV, b1, b2 = val, i, j
				}
			}
		}
		if b1 >= 0 {
			blocksOf[a.id] = []*tcre{fr[b1], fr[b2]}
			usedB[fr[b1].id], usedB[fr[b2].id] = true, true
		}
	}
	var choices []int
	for _, a := range attackers {
		for _, b := range blocksOf[a.id] {
			if i, ok := opt[pair{b.id, a.id}]; ok {
				choices = append(choices, i)
			}
		}
	}
	choices = botpolicy.LegalBlockChoices(brd, d, choices)
	return repaired(d, decision.Intent{Choices: choices})
}
