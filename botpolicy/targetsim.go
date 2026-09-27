package botpolicy

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// chooseTargetsSim is the attack-sim arm's KTarget answer (opt-in with
// AttackSimParams.Targets). It changes one shape only: a single-target
// removal or known-damage effect whose default pick (chooseTargets) is an
// opponent's creature. Every opposing creature option the effect removes
// (a Removal-classified effect always; a damage effect when the amount
// reaches the creature's remaining toughness) is scored by removing it
// from the simulated board, playing the next two combats with the default
// rules (ours first when it is our precombat main phase, theirs first
// otherwise) and scoring the position (simWorld.score). The default pick
// is the incumbent; another creature replaces it only by scoring strictly
// more plus Margin. This is the evaluation-based removal choice of Forge's
// and XMage's simulation players ("kill what most improves the position")
// in place of the fixed threat() ranking.
func (b Board) chooseTargetsSim(d *decision.Decision, p *AttackSimParams) []int {
	base := b.chooseTargets(d)
	if len(base) != 1 || d.Max != 1 || len(d.Options) < 2 {
		return base
	}
	te := d.TargetEffect
	if te == nil {
		return base
	}
	dmg, isDamage := b.effectDamage(d)
	if !isDamage && te.Removal == nil {
		return base
	}
	me := d.Player
	bo := d.Options[base[0]]
	bc, ok := b.Creatures[bo.Obj]
	if bo.Kind == "player" || !ok || bc.Controller == me {
		return base
	}
	opp := bc.Controller
	if _, ok := b.Life[me]; !ok {
		return base
	}
	if _, ok := b.Life[opp]; !ok {
		return base
	}
	for _, c := range b.Creatures {
		if c.Controller != me && c.Controller != opp {
			return base // more than two players on the battlefield
		}
	}
	kills := func(c Creature) bool {
		if isDamage {
			return c.remTough() <= dmg && !c.hasKeyword("Indestructible")
		}
		return true
	}
	if !kills(bc) {
		return base
	}
	type cand struct {
		idx int
		id  state.ObjID
	}
	var cands []cand
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "player" || i == base[0] {
			continue
		}
		c, ok := b.Creatures[o.Obj]
		if !ok || c.Controller != opp || !kills(c) {
			continue
		}
		if o.Group != "" {
			continue
		}
		cands = append(cands, cand{i, o.Obj})
	}
	if len(cands) == 0 {
		return base
	}
	w := newSimWorld(b, me, opp)
	ourFirst := b.MyTurn && b.IsMain && b.FirstMain
	eval := func(id state.ObjID) int64 {
		sw := w.clone()
		if i := sw.index(id); i >= 0 {
			sw.units[i].dead = true
		}
		first, second := opp, me
		if ourFirst {
			first, second = me, opp
		}
		sw.followUp(b, first, second)
		if sw.life[me] > 0 && sw.life[opp] > 0 {
			sw.followUp(b, second, first)
		}
		return sw.score(p.LifeUnit)
	}
	baseScore := eval(bo.Obj)
	best, bestScore := base[0], baseScore
	sort.Slice(cands, func(i, j int) bool { return cands[i].idx < cands[j].idx })
	for _, c := range cands {
		if s := eval(c.id); s > bestScore {
			best, bestScore = c.idx, s
		}
	}
	if best == base[0] || bestScore <= baseScore+p.Margin {
		return base
	}
	return []int{best}
}
