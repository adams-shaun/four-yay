package botpolicy

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// This file is the BLK block-assignment policy: an opt-in KBlockers policy
// (botpolicy.BlocksDecide, exposed to cmd/botbench as the "blocks" policy and
// to nothing else — never the hosted or production bot) that considers the
// whole defender assignment instead of choosing chump/trade per blocker the
// way chooseBlockers does. The rules, each stated for what it reads:
//
//   - B0 (legality first): the only (blocker, attacker) pairs considered are
//     the offered ones (the engine's askBlockers already encodes every
//     legality gate — tap, Flying/Reach, protection, can't-block statics —
//     by omitting the illegal pairs), and one blocker is never assigned to
//     two attackers (CR 509.1a; the wire's Group exclusivity). A Menace
//     attacker is blocked only by teams of at least two (CR 702.111b, which
//     the engine's validateBlockers enforces over the whole declaration).
//   - B1 (survive first): when the unblocked attacking power reaches the
//     defender's known life (or a commander's unblocked swing would close
//     the CR 903.10 clock on this defender — the same second lethal line
//     chooseBlockers' BR3 honours), the assignment is chosen to keep the
//     most life: every attacker is answered biggest-first, each with the
//     cheapest killing block among the offered pairs (lone blocker or a
//     team, every killing prefix simulated), and when nothing kills it a
//     chump block still absorbs damage — a non-trample attacker's whole
//     power lands on its blockers (rules/combat.go's damageStep caps every
//     blocker but the last at its own need, and the last absorbs the
//     remainder), so even a dying chump keeps that power off the defender.
//   - B2 (value trades otherwise): a block is taken only when it is a free
//     kill (the blocker kills the attacker and survives — a lone blocker, or
//     for a Menace attacker a team in which every member survives) or an
//     even-or-up trade (the blocker kills the attacker and dies, and the
//     attacker's power is at least the blocker's — for a team, at least the
//     dead blockers' combined power). A chump block that neither kills nor
//     survives is never taken while not facing lethal.
//   - B3 (determinism): attackers sort by power descending with a
//     clock-closing commander ranked as power 21 (BR4's ordering, shared so
//     both policies answer the same threat first), ties by the attacker's
//     first offered option; blockers sort by pt ascending then ObjID
//     ascending. No rng is consumed and no map iteration order reaches the
//     answer.
//
// The saved-life arithmetic is blockSaved, a direct restatement of the
// engine's damage-assignment loop; like blockCombat it only ever orders a
// decision, never mutates game state.
func (b Board) chooseBlockAssignment(d *decision.Decision) []int {
	if len(d.Options) == 0 {
		return nil
	}
	me := d.Player
	myLife, hasLife := b.Life[me]

	// Group the offered options by attacker, preserving the engine's
	// enumeration order (first-seen position is the deterministic tiebreak)
	// — the same grouping chooseBlockers runs.
	type atk struct {
		id   state.ObjID
		a    Creature
		pos  int
		opts []int
	}
	var attackers []*atk
	byID := make(map[state.ObjID]*atk)
	for i := range d.Options {
		o := &d.Options[i]
		at, ok := byID[o.Attacker]
		if !ok {
			at = &atk{id: o.Attacker, a: b.Creatures[o.Attacker], pos: i}
			byID[o.Attacker] = at
			attackers = append(attackers, at)
		}
		at.opts = append(at.opts, i)
	}
	sort.SliceStable(attackers, func(i, j int) bool {
		clock := func(at *atk) int32 {
			if b.closesClock(me, at.id, at.a) {
				return 21
			}
			return 0
		}
		pi, pj := attackers[i].a.Power+clock(attackers[i]), attackers[j].a.Power+clock(attackers[j])
		if pi != pj {
			return pi > pj // biggest threat first
		}
		return attackers[i].pos < attackers[j].pos
	})

	unblocked := int32(0)
	clockOpen := false
	for _, at := range attackers {
		if at.a.Power <= 0 {
			continue
		}
		unblocked += at.a.Power
		if b.closesClock(me, at.id, at.a) {
			clockOpen = true
		}
	}
	// B1's trigger: the unblocked attack is lethal to a known life total, or
	// a clock is open. An unknown life fact is never treated as zero.
	lethal := (hasLife && unblocked >= myLife) || clockOpen

	used := make(map[state.ObjID]bool)
	var ch []int
	for _, at := range attackers {
		// An attacker with no power deals nothing (a factless option reads
		// as a 0/0), and a dying attacker's remTough <= 0 cannot be killed
		// — both are passed over, exactly like chooseBlockers.
		if at.a.Power <= 0 || at.a.remTough() <= 0 {
			continue
		}
		urgent := lethal
		var picks []int
		if urgent {
			picks = b.surviveBlockFor(d, at.id, at.a, at.opts, used)
		} else {
			picks = b.valueBlockFor(d, at.id, at.a, at.opts, used)
		}
		if len(picks) > 0 {
			for _, oi := range picks {
				ch = append(ch, oi)
				used[d.Options[oi].Obj] = true
			}
			unblocked -= blockSaved(at.a, picks, d, b)
		}
	}
	return ch
}

// blkCand is one offered (blocker, attacker) pair usable in a block: the
// option's position in d.Options, the blocker's object id and its creature
// facts.
type blkCand struct {
	pos int
	id  state.ObjID
	c   Creature
}

// blockCandidates collects the offered options for one attacker whose
// blocker is still unassigned, in the decision's own (offered) order. The
// blocker's creature facts come from the Board's census (both adapters fill
// it from public battlefield facts).
func (b Board) blockCandidates(d *decision.Decision, opts []int, used map[state.ObjID]bool) []blkCand {
	var out []blkCand
	for _, oi := range opts {
		o := &d.Options[oi]
		if used[o.Obj] {
			continue
		}
		out = append(out, blkCand{pos: oi, id: o.Obj, c: b.Creatures[o.Obj]})
	}
	return out
}

// bestKillingBlock searches the offered candidates for the cheapest block
// (lone or team, every pt-ascending prefix simulated) that kills the
// attacker, honouring Menace's at-least-two rule. It returns the chosen
// positions in declaration order (cheapest blockers first, the order the
// engine's damage assignment reads — every blocker but the last capped at
// its own need, the last absorbing the remainder, which is the order
// blockCombat simulated). Ties prefer fewer blockers then the lowest first
// ObjID, all deterministic.
func bestKillingBlock(a Creature, cands []blkCand) []blkCand {
	if len(cands) == 0 {
		return nil
	}
	sorted := make([]blkCand, len(cands))
	copy(sorted, cands)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].c.pt() != sorted[j].c.pt() {
			return sorted[i].c.pt() < sorted[j].c.pt()
		}
		return sorted[i].id < sorted[j].id
	})
	menace := a.hasKeyword("Menace")
	var best []blkCand
	var bestCost int32 = -1
	var team []blkCand
	for k := 1; k <= len(sorted); k++ {
		if menace && k < 2 {
			continue
		}
		team = append(team[:0], sorted[:k]...)
		creatures := make([]Creature, k)
		for i := range team {
			creatures[i] = team[i].c
		}
		aDead, dead := blockCombat(a, creatures)
		if !aDead {
			continue
		}
		var cost int32
		for i := range dead {
			if dead[i] {
				cost += team[i].c.pt()
			}
		}
		if bestCost == -1 || cost < bestCost ||
			(cost == bestCost && len(team) < len(best)) {
			best = append([]blkCand(nil), team...)
			bestCost = cost
		}
	}
	return best
}

// chumpBlockFor picks the single absorb block for an attacker nothing can
// kill: the blocker with the most remaining toughness against a trample
// attacker (its remTough is what the spill arithmetic subtracts — an
// adequate one soaks the whole overflow), the cheapest one otherwise (its
// remTough is irrelevant: the attacker's whole power lands on blockers
// regardless). Ties break on pt then ObjID. Returns nil when no unused
// candidate exists.
func chumpBlockFor(a Creature, cands []blkCand) []blkCand {
	if len(cands) == 0 {
		return nil
	}
	trample := a.hasKeyword("Trample")
	best := -1
	for i := range cands {
		if best == -1 {
			best = i
			continue
		}
		bc, bb := cands[i].c, cands[best].c
		if trample {
			if bc.remTough() > bb.remTough() ||
				(bc.remTough() == bb.remTough() && (bc.pt() < bb.pt() ||
					(bc.pt() == bb.pt() && cands[i].id < cands[best].id))) {
				best = i
			}
		} else if bc.pt() < bb.pt() || (bc.pt() == bb.pt() && cands[i].id < cands[best].id) {
			best = i
		}
	}
	return []blkCand{cands[best]}
}

// surviveBlockFor is B1's per-attacker answer: the cheapest killing block
// among the offered pairs, and when nothing kills it a chump (two blockers
// for a Menace attacker, which a lone declaration would never satisfy).
func (b Board) surviveBlockFor(d *decision.Decision, _ state.ObjID, a Creature, opts []int, used map[state.ObjID]bool) []int {
	cands := b.blockCandidates(d, opts, used)
	if len(cands) == 0 {
		return nil
	}
	if picks := bestKillingBlock(a, cands); picks != nil {
		return positions(picks)
	}
	if a.hasKeyword("Menace") {
		// A Menace attacker needs at least two blockers even as a chump;
		// the two cheapest still absorb (blockSaved's non-trample arm takes
		// the whole power when the blockers cannot spill it back).
		if len(cands) < 2 {
			return nil
		}
		sorted := make([]blkCand, len(cands))
		copy(sorted, cands)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].c.pt() != sorted[j].c.pt() {
				return sorted[i].c.pt() < sorted[j].c.pt()
			}
			return sorted[i].id < sorted[j].id
		})
		return positions(sorted[:2])
	}
	return positions(chumpBlockFor(a, cands))
}

// valueBlockFor is B2's per-attacker answer: a free kill first (the blocker
// kills the attacker and survives), then an even-or-up trade (the blocker
// kills and dies, and the attacker's power is at least the blocker's — a
// team trade reads the dead blockers' combined power). Never a chump. A
// Menace attacker can only be answered by a team of two or more, so its
// lone candidates are skipped and the team search runs instead.
func (b Board) valueBlockFor(d *decision.Decision, _ state.ObjID, a Creature, opts []int, used map[state.ObjID]bool) []int {
	cands := b.blockCandidates(d, opts, used)
	menace := a.hasKeyword("Menace")
	var kill, trade *blkCand
	for i := range cands {
		if menace {
			break // a lone block against a Menace attacker is illegal
		}
		c := cands[i]
		aDead, dead := blockCombat(a, []Creature{c.c})
		if !aDead {
			continue
		}
		if !dead[0] {
			if kill == nil || c.c.pt() < kill.c.pt() ||
				(c.c.pt() == kill.c.pt() && c.id < kill.id) {
				k := c
				kill = &k
			}
		} else if a.Power >= c.c.Power {
			if trade == nil || c.c.pt() < trade.c.pt() ||
				(c.c.pt() == trade.c.pt() && c.id < trade.id) {
				tr := c
				trade = &tr
			}
		}
	}
	if kill != nil {
		return []int{kill.pos}
	}
	if trade != nil {
		return []int{trade.pos}
	}
	if menace {
		if picks := bestKillingBlock(a, cands); picks != nil {
			// Classify the team the same way a lone block is: every member
			// surviving is a free kill; otherwise it is a trade only when
			// the dead blockers' combined power does not exceed the
			// attacker's.
			creatures := make([]Creature, len(picks))
			for i := range picks {
				creatures[i] = picks[i].c
			}
			aDead, dead := blockCombat(a, creatures)
			if aDead {
				var deadPower int32
				anyDead := false
				for i := range dead {
					if dead[i] {
						anyDead = true
						deadPower += picks[i].c.Power
					}
				}
				if !anyDead || deadPower <= a.Power {
					return positions(picks)
				}
			}
		}
	}
	return nil
}

// positions maps the candidate slice to its option positions in declaration
// order.
func positions(cands []blkCand) []int {
	out := make([]int, len(cands))
	for i := range cands {
		out[i] = cands[i].pos
	}
	return out
}

// blockSaved restates the engine's damage-assignment arithmetic
// (rules/combat.go damageStep's blocked branch) for one attacker and one
// block declared in the given order: a non-trample attacker's whole power
// lands on the blockers (the last absorbs whatever remains), so the
// defender takes none of it; a trample attacker caps each blocker at its
// own need (1 under Deathtouch) and spills the remainder to the defender.
// First strike is deliberately not modelled here: a first-strike blocker
// killing the attacker before the regular step saves MORE than this
// reports, which errs toward blocking — the safe direction for the
// defender.
func blockSaved(a Creature, picks []int, d *decision.Decision, b Board) int32 {
	if len(picks) == 0 || a.Power <= 0 {
		return 0
	}
	if !a.hasKeyword("Trample") {
		return a.Power
	}
	dt := a.hasKeyword("Deathtouch")
	absorbed := int32(0)
	remaining := a.Power
	for _, oi := range picks {
		if remaining <= 0 {
			break
		}
		bl, ok := b.Creatures[d.Options[oi].Obj]
		if !ok {
			continue
		}
		need := bl.remTough()
		if dt {
			need = 1
		}
		give := remaining
		if give > need {
			give = need
		}
		absorbed += give
		remaining -= give
	}
	return absorbed
}

// LegalBlockChoices is legalBlockChoices exported for callers outside the
// bot that build their own KBlockers answers (the search teacher's candidate
// builder) and must submit only what the engine accepts. It is a thin
// wrapper: the unexported guard stays the implementation, so the bot's own
// behaviour is unchanged.
func LegalBlockChoices(b Board, d *decision.Decision, choices []int) []int {
	return legalBlockChoices(b, d, choices)
}

// legalBlockChoices drops any chosen (blocker, attacker) pair that would
// leave its attacker's block count outside the CR 509.1a MinMaxBlocker
// bounds the engine published on the offered options
// (Option.MinBlockers/MaxBlockers), and any pair whose block charge the
// defender cannot pay (CR 509.1b's blocking costs, published on the option
// as Value/mana against MaxSum, CostLife/life against the defender's life
// total, and CostTaps/tap obligations). The per-pair option list cannot
// express a whole-declaration constraint, and the engine REJECTS an illegal
// count or an unpayable charge -- a rejected bot intent crashes the match --
// so every KBlockers policy routes its answer through this one guard rather
// than each learning the restriction.
//
// An attacker whose chosen count falls below Min has ALL its chosen blocks
// dropped: 0 is always legal for Min$ (the attacker is simply unblocked), and
// a partial team is not. A count above Max is trimmed to Max, keeping the
// earliest-declared pairs (the engine reads declaration order for CR 510.1c
// damage assignment). A tap-costed option is dropped outright: the policy
// sees only the obligation count, never the eligible-permanent pool the
// engine's deterministic tap plan resolves, and leaving the attacker
// unblocked is always legal (CR 509.1a -- blocking is never mandatory).
// Output order is the input order, so the guard consumes no randomness and
// never ranges a map into the result.
func legalBlockChoices(b Board, d *decision.Decision, choices []int) []int {
	if len(choices) == 0 {
		return choices
	}
	// Affordability pre-filter (CR 509.1b), in input order: a pair whose
	// charge would push the running mana over MaxSum or the running life
	// over the defender's total is dropped, earliest kept -- the same
	// earliest-kept convention the Max trim below uses. MaxSum == 0 means no
	// mana budget was published (no mana-priced option was offered), so the
	// mana term is skipped. The rule itself lives in the shared
	// decision.ChargeOptionConstraints, so the attack and block arms cannot
	// drift.
	choices = decision.ChargeOptionConstraints(d, choices, b.Life[d.Player], d.MaxSum)
	if len(choices) == 0 {
		return choices
	}
	bounds := make(map[state.ObjID][2]int)
	attacker := make(map[int]state.ObjID, len(choices))
	for _, ci := range choices {
		if ci < 0 || ci >= len(d.Options) {
			continue
		}
		o := &d.Options[ci]
		attacker[ci] = o.Attacker
		if o.MinBlockers != 0 || o.MaxBlockers != 0 {
			bounds[o.Attacker] = [2]int{o.MinBlockers, o.MaxBlockers}
		}
	}
	count := make(map[state.ObjID]int)
	for _, aid := range attacker {
		count[aid]++
	}
	// CR 702.111b: Menace is a Min$ 2 floor. The engine publishes it on the
	// option (MinBlockers), but a Board census that knows the keyword is
	// read too, so an adapter whose options predate the published floor
	// still never submits a lone blocker the engine rejects.
	for aid := range count {
		if c, ok := b.Creatures[aid]; ok && c.hasKeyword("Menace") {
			bd := bounds[aid]
			if bd[0] < 2 {
				bd[0] = 2
			}
			bounds[aid] = bd
		}
	}
	drop := make(map[state.ObjID]bool)
	for aid, bd := range bounds {
		if bd[0] != 0 && (count[aid] < bd[0] || (bd[1] != 0 && bd[1] < bd[0])) {
			drop[aid] = true
		}
	}
	out := make([]int, 0, len(choices))
	trimmed := make(map[state.ObjID]int)
	for _, ci := range choices {
		aid, ok := attacker[ci]
		if !ok {
			out = append(out, ci)
			continue
		}
		if drop[aid] {
			continue
		}
		if bd := bounds[aid]; bd[1] != 0 {
			trimmed[aid]++
			if trimmed[aid] > bd[1] {
				continue
			}
		}
		out = append(out, ci)
	}
	return out
}

// LegalAttackChoices is legalAttackChoices exported for callers outside the
// bot that build their own KAttackers answers (the search teacher's candidate
// builder). It is a thin wrapper: the unexported guard stays the
// implementation.
func LegalAttackChoices(b Board, d *decision.Decision, choices []int) []int {
	return legalAttackChoices(b, d, choices)
}

// legalAttackChoices is the KAttackers sibling of legalBlockChoices: it drops
// any chosen attacker option whose non-mana charge the acting player cannot
// jointly pay. The published fields are the only channel -- Option.CostTaps
// is the tapXType obligation count (the policy never sees the eligible-
// permanent pool the engine's deterministic plan resolves, so a positive
// value is dropped outright, exactly as the KBlockers guard does) and
// Option.CostLife is the life charge (summed against the acting player's
// life total). The engine's validateAttackers rejects a declaration whose
// joint combatChargeAffordable check fails, and a rejected bot intent crashes
// the match, so every KAttackers policy routes its answer through this one
// guard before Clamp repairs it. Output order is the input order, so the
// guard consumes no randomness and never ranges a map into the result.
func legalAttackChoices(b Board, d *decision.Decision, choices []int) []int {
	// The shared published-field charge rule (decision.ChargeOptionConstraints):
	// a tapXType obligation the wire cannot verify is dropped, and the
	// cumulative CostLife is bounded by the acting player's life total. The
	// mana budget is left to Clamp (maxSum 0).
	return decision.ChargeOptionConstraints(d, choices, b.Life[d.Player], 0)
}
