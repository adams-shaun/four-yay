package botpolicy

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// chooseBlockersSim is the attack-sim arm's KBlockers answer (opt-in with
// AttackSimParams.Blocks): the default block rule (chooseBlockers) is the
// incumbent, and a deterministic coordinate search over whole block
// assignments may replace it when the simulated position scores strictly
// better. Each assignment is resolved with the same combat model the
// attack search uses (attacksim.go: blockCombat plus trample, double
// strike, lifelink, indestructible), then BlockPlies later combats are
// simulated with the default rules (our counter-attack while the
// attackers are still tapped, then their next attack), and the position is
// scored with simWorld.score.
//
// The default block rule trades only even-or-up and chumps only against
// lethal damage; the literature (Ward & Cowling 2009, Forge's
// AiBlockController "life in danger" trade/chump resets) finds such rules
// too cautious. The search lets a trade or chump through exactly when the
// simulated race says it pays.
//
// Scope: two players. Attackers no option names (unblockable ones) are
// invisible to the Board, so their damage is a candidate-independent
// constant the search does not see. A decision naming more than one
// attacking player, or a blocker option with Min/MaxBlockers bounds, is
// answered by the default rule unchanged.
func (b Board) chooseBlockersSim(d *decision.Decision, p *AttackSimParams) []int {
	base := b.chooseBlockers(d)
	if len(d.Options) == 0 {
		return base
	}
	me := d.Player
	var opp state.PlayerID
	oppSet := false
	type pairKey struct{ blk, atk state.ObjID }
	optOf := make(map[pairKey]int, len(d.Options))
	var atkOrder, blkOrder []state.ObjID
	seenA := make(map[state.ObjID]bool)
	seenB := make(map[state.ObjID]bool)
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != "block" || o.MinBlockers != 0 || o.MaxBlockers != 0 {
			return base
		}
		a, ok := b.Creatures.Lookup(o.Attacker)
		if !ok {
			return base
		}
		if _, ok := b.Creatures.Lookup(o.Obj); !ok {
			return base
		}
		if !oppSet {
			opp, oppSet = a.Controller, true
		} else if a.Controller != opp {
			return base
		}
		optOf[pairKey{o.Obj, o.Attacker}] = i
		if !seenA[o.Attacker] {
			seenA[o.Attacker] = true
			atkOrder = append(atkOrder, o.Attacker)
		}
		if !seenB[o.Obj] {
			seenB[o.Obj] = true
			blkOrder = append(blkOrder, o.Obj)
		}
	}
	if opp == me {
		return base
	}
	if _, ok := b.Life.Lookup(me); !ok {
		return base
	}
	if _, ok := b.Life.Lookup(opp); !ok {
		return base
	}
	if len(blkOrder) > maxSimAttackers || len(atkOrder) > maxSimAttackers {
		return base
	}
	// choices[k] is, for blocker blkOrder[k], the attackers it may block (in
	// option order); assignment value -1 means "does not block".
	choices := make([][]state.ObjID, len(blkOrder))
	bpos := make(map[state.ObjID]int, len(blkOrder))
	for k, id := range blkOrder {
		bpos[id] = k
	}
	for i := range d.Options {
		o := &d.Options[i]
		k := bpos[o.Obj]
		choices[k] = append(choices[k], o.Attacker)
	}
	sortedAtk := append([]state.ObjID(nil), atkOrder...)
	sort.Slice(sortedAtk, func(i, j int) bool { return sortedAtk[i] < sortedAtk[j] })

	w := newSimWorld(b, me, opp)
	eval := func(assign []int) (int64, bool) {
		blocks := make(map[state.ObjID][]state.ObjID, len(atkOrder))
		for k, ai := range assign {
			if ai >= 0 {
				a := choices[k][ai]
				blocks[a] = append(blocks[a], blkOrder[k])
			}
		}
		for _, aid := range sortedAtk {
			bl := blocks[aid]
			if len(bl) == 1 && b.Creatures.Get(aid).hasKeyword("Menace") {
				return 0, false
			}
			sort.Slice(bl, func(i, j int) bool { return bl[i] < bl[j] })
		}
		sw := w.clone()
		sw.resolve(opp, me, sortedAtk, blocks)
		if sw.life[me] > 0 && p.BlockPlies >= 1 {
			if p.BlockGreedy {
				sw.followUpGreedy(b, me, opp, p.LifeUnit)
			} else {
				sw.followUp(b, me, opp)
			}
			if sw.life[opp] > 0 && p.BlockPlies >= 2 {
				sw.followUp(b, opp, me)
			}
		}
		return sw.score(p.LifeUnit), true
	}

	baseAssign := make([]int, len(blkOrder))
	for k := range baseAssign {
		baseAssign[k] = -1
	}
	for _, ci := range base {
		if ci < 0 || ci >= len(d.Options) {
			continue
		}
		o := &d.Options[ci]
		k := bpos[o.Obj]
		for ai, a := range choices[k] {
			if a == o.Attacker {
				baseAssign[k] = ai
			}
		}
	}
	baseScore, ok := eval(baseAssign)
	if !ok {
		return base
	}
	best := append([]int(nil), baseAssign...)
	bestScore := baseScore
	climb := func(start []int) {
		cur := append([]int(nil), start...)
		curScore, ok := eval(cur)
		if !ok {
			curScore = simLoss - 1
		}
		for pass := 0; pass < 3; pass++ {
			improved := false
			for k := range cur {
				keep := cur[k]
				for alt := -1; alt < len(choices[k]); alt++ {
					if alt == keep {
						continue
					}
					cur[k] = alt
					if s, ok := eval(cur); ok && s > curScore {
						keep, curScore, improved = alt, s, true
					}
				}
				cur[k] = keep
			}
			if !improved {
				break
			}
		}
		if curScore > bestScore {
			best, bestScore = append([]int(nil), cur...), curScore
		}
	}
	climb(baseAssign)
	none := make([]int, len(blkOrder))
	for k := range none {
		none[k] = -1
	}
	climb(none)
	if bestScore <= baseScore+p.Margin {
		return base
	}
	var out []int
	for k, ai := range best {
		if ai >= 0 {
			out = append(out, optOf[pairKey{blkOrder[k], choices[k][ai]}])
		}
	}
	sort.Ints(out)
	return out
}
