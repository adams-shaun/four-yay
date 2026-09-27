package botpolicy

import (
	"math/bits"
	"math/rand/v2"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Combat-simulation attacker (bench arm "attack-sim"; never the default
// Decide, but the hosted auto-pay bot builds it: host.NewBotPolicySeatWithAutoPayMana).
//
// The default attacker (chooseAttackersMode) judges every attacker ALONE:
// AR3 vetoes a creature whenever some defender block kills it for less than
// it is worth, even when the defender has one blocker and we attack with
// three -- a blocker is a limited resource, and the per-attacker veto cannot
// see that. It also never weighs the race: it attacks the same way at 20
// life facing an empty board as at 3 life facing a lethal crack-back.
//
// chooseAttackersSim replaces the per-attacker rules with a small
// deterministic search over whole attacking SETS, the process XMage's
// ComputerPlayer6 and Forge's simulation AI use (simulate, then score the
// resulting position with a static evaluator), restricted to combat and to
// public board facts:
//
//  1. Candidates are subsets of the offered attackers (all subsets up to
//     AttackSimParams.MaxEnum free attackers, else a deterministic
//     hill-climb from the default answer and from "everyone"). Required
//     attackers (CR 508.1d) and a commander whose swing closes a defender's
//     commander-damage clock (AR5) are in every candidate.
//  2. The defender's response is PREDICTED with the defender-side block
//     policy (chooseBlockers) on a synthesised block decision over its
//     untapped creatures -- the same rule the default bot blocks with.
//  3. Combat is resolved with blockCombat (first strike, deathtouch, the
//     engine's damage order), plus trample overflow, double strike,
//     lifelink and indestructible.
//  4. With CrackBack set, the opponent's next attack is simulated the same
//     way (its whole board untapped, our attackers still tapped unless they
//     have Vigilance; attack chosen by the default attacker, our blocks by
//     chooseBlockers).
//  5. The final position is scored: Forge-style creature values
//     (creatureSimValue) for both sides plus a concave life value
//     (lifeSimValue, XMage's GameStateEvaluator2 life table shape), with a
//     win/loss sentinel.
//
// The default answer is candidate zero and the incumbent: another set wins
// only by scoring strictly more than it plus Margin, so the arm degrades to
// the default whenever the simulation is indifferent. Everything is a pure
// function of (Board, Decision, params): no rng, no map iteration reaching a
// choice (every list built from a map is sorted by ObjID), ties on the
// lowest candidate mask.
//
// Scope: two-player combat against a player. A decision offering more than
// one defender, or a battle / planeswalker defender, is answered by the
// default attacker unchanged.

// AttackSimParams are the search's knobs. The zero value is invalid; use
// DefaultAttackSimParams.
type AttackSimParams struct {
	// LifeUnit is the value of one life point between 10 and 20 life, in
	// creatureSimValue units (a vanilla 2/2 for two is 140). Below 10 life a
	// point is worth 20/9 as much, above 20 a third as much.
	LifeUnit int64
	// CrackBack simulates the opponent's next attack before scoring.
	CrackBack bool
	// NextTurn (with CrackBack) also simulates our following attack, made
	// by the default attacker, before scoring.
	NextTurn bool
	// NextGreedy makes that following attack the default attacker plus a
	// greedy one-ply improvement (followUpGreedy).
	NextGreedy bool
	// MaxEnum is the most free attackers whose every subset is scored;
	// beyond it (up to maxSimAttackers) the search hill-climbs.
	MaxEnum int
	// Margin is how much a non-default set must out-score the default
	// answer to replace it.
	Margin int64
	// Blocks also answers KBlockers by simulation (chooseBlockersSim).
	Blocks bool
	// BlockPlies is how many later combats the block search simulates
	// before scoring: 0 scores right after this combat, 1 adds our
	// counter-attack, 2 also their next attack.
	BlockPlies int
	// BlockGreedy makes the block search's counter-attack ply our greedy
	// one-ply attacker (followUpGreedy) instead of the default attacker.
	BlockGreedy bool
	// TieAggro breaks an exact score tie toward the set with more
	// attackers (the default answer included), the literature's "rule
	// players are too cautious" correction applied only where the evaluator
	// is indifferent.
	TieAggro bool
}

// DefaultAttackSimParams is the arm's measured configuration (dev seeds
// 40,000,000-40,999,999): two-ply attacks (our combat, their crack-back),
// simulated blocks with one greedy counter-attack ply.
func DefaultAttackSimParams() AttackSimParams {
	return AttackSimParams{LifeUnit: 30, CrackBack: true, MaxEnum: 7, Margin: 0, Blocks: true, BlockPlies: 1, BlockGreedy: true}
}

// maxSimAttackers bounds the search: a board with more free attackers than
// this (token armies) is answered by the default attacker, whose AR9 swarm
// rule exists for exactly that shape.
const maxSimAttackers = 16

// AttackSimDecide is Decide with KAttackers answered by the combat
// simulation (chooseAttackersSim). Every other kind is the default policy.
func AttackSimDecide(b Board, d *decision.Decision, r *rand.Rand, p AttackSimParams) decision.Intent {
	b.attackSim = &p
	return decide(b, d, r, true, false, false)
}

// simUnit is one creature in a simulated combat.
type simUnit struct {
	id    state.ObjID
	c     Creature
	value int64
	dead  bool
}

// simWorld is the two-player board the search mutates per candidate.
type simWorld struct {
	me, opp state.PlayerID
	life    map[state.PlayerID]int32
	units   []simUnit // sorted by id
}

// newSimWorld is the starting world: every battlefield creature of the two
// players, sorted by id, valued once.
func newSimWorld(b Board, me, opp state.PlayerID) *simWorld {
	w := &simWorld{me: me, opp: opp, life: map[state.PlayerID]int32{me: b.Life[me], opp: b.Life[opp]}}
	ids := make([]state.ObjID, 0, len(b.Creatures))
	for id, c := range b.Creatures {
		if c.Controller == me || c.Controller == opp {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		c := b.Creatures[id]
		w.units = append(w.units, simUnit{id: id, c: c, value: creatureSimValue(c, b.Cards[id].CMC)})
	}
	return w
}

func (w *simWorld) clone() *simWorld {
	n := &simWorld{me: w.me, opp: w.opp, life: map[state.PlayerID]int32{w.me: w.life[w.me], w.opp: w.life[w.opp]}}
	n.units = make([]simUnit, len(w.units))
	copy(n.units, w.units)
	return n
}

// board materialises w as the Board the default combat rules read
// (Creatures, Life, Commanders -- nothing else is consulted by
// chooseBlockers/chooseAttackersMode).
func (w *simWorld) board(base Board) Board {
	cr := make(map[state.ObjID]Creature, len(w.units))
	for _, u := range w.units {
		if !u.dead {
			cr[u.id] = u.c
		}
	}
	return Board{
		Creatures:  cr,
		Life:       map[state.PlayerID]int32{w.me: w.life[w.me], w.opp: w.life[w.opp]},
		Commanders: base.Commanders,
		Cards:      base.Cards,
	}
}

func (w *simWorld) index(id state.ObjID) int {
	i := sort.Search(len(w.units), func(i int) bool { return w.units[i].id >= id })
	if i < len(w.units) && w.units[i].id == id {
		return i
	}
	return -1
}

// simCanBlock is canBlockLike plus the evasion keywords the default rule
// does not read (Horsemanship, Shadow), used only inside the simulation so
// a predicted block is one the engine would offer.
func simCanBlock(a, bl Creature) bool {
	if !canBlockLike(a, bl) {
		return false
	}
	if a.hasKeyword("Horsemanship") && !bl.hasKeyword("Horsemanship") {
		return false
	}
	if a.hasKeyword("Shadow") != bl.hasKeyword("Shadow") {
		return false
	}
	return true
}

// withoutKeywords returns kws minus every keyword whose head is one of
// drop, in a fresh slice (the input may be shared with the Board).
func withoutKeywords(kws []string, drop ...string) []string {
	out := make([]string, 0, len(kws))
	for _, k := range kws {
		c := Creature{Keywords: []string{k}}
		keep := true
		for _, d := range drop {
			if c.hasKeyword(d) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, k)
		}
	}
	return out
}

// creatureSimValue is a Forge-style additive creature evaluation: a base,
// power and toughness, mana value, and keyword terms (evasion scaled by
// power). It reads only the public facts on Creature and Card.
func creatureSimValue(c Creature, cmc int32) int64 {
	p, t := int64(c.Power), int64(c.Toughness)
	if p < 0 {
		p = 0
	}
	if t < 0 {
		t = 0
	}
	v := 80 + 15*p + 10*t + 5*int64(cmc)
	if c.hasKeyword("Flying") {
		v += 10 * p
	}
	if c.hasKeyword("Menace") {
		v += 4 * p
	}
	if c.hasKeyword("Double Strike") {
		v += 10 + 15*p
	} else if c.hasKeyword("First Strike") {
		v += 10 + 5*p
	}
	if c.hasKeyword("Deathtouch") {
		v += 25
	}
	if c.hasKeyword("Lifelink") {
		v += 10 * p
	}
	if c.hasKeyword("Trample") && p > 1 {
		v += 5 * (p - 1)
	}
	if c.hasKeyword("Vigilance") {
		v += 5*p + 5*t
	}
	if c.hasKeyword("Indestructible") {
		v += 70
	}
	if c.hasKeyword("Hexproof") {
		v += 35
	}
	if c.hasKeyword("Reach") {
		v += 5
	}
	if c.hasKeyword("Defender") {
		v -= 9*p + 40
	}
	if c.hasKeyword("Undying") || c.hasKeyword("Persist") {
		v += 40
	}
	return v
}

// simLoss / simWin are the terminal sentinels, far outside any board value.
const (
	simWin  int64 = 1 << 40
	simLoss int64 = -(1 << 40)
)

// lifeSimValue is a concave life value (XMage GameStateEvaluator2's life
// table shape): steep at low life, flat above 20. Computed in ninths of
// LifeUnit so it stays integer.
func lifeSimValue(life int32, unit int64) int64 {
	l := int64(life)
	var v9 int64
	switch {
	case l <= 10:
		v9 = 30 + (l-1)*20
	case l <= 20:
		v9 = 210 + (l-10)*9
	default:
		v9 = 300 + (l-20)*3
	}
	return unit * v9 / 9
}

func (w *simWorld) score(unit int64) int64 {
	if w.life[w.opp] <= 0 {
		return simWin
	}
	if w.life[w.me] <= 0 {
		return simLoss
	}
	var s int64
	for _, u := range w.units {
		if u.dead {
			continue
		}
		if u.c.Controller == w.me {
			s += u.value
		} else {
			s -= u.value
		}
	}
	return s + lifeSimValue(w.life[w.me], unit) - lifeSimValue(w.life[w.opp], unit)
}

// predictBlocks synthesises the defender's block decision over the given
// attackers and answers it with the default block rule.
func (w *simWorld) predictBlocks(base Board, defender state.PlayerID, attackers []state.ObjID) map[state.ObjID][]state.ObjID {
	var opts []decision.Option
	var blockers []int
	for i := range w.units {
		u := &w.units[i]
		if !u.dead && u.c.Controller == defender && !u.c.Tapped {
			blockers = append(blockers, i)
		}
	}
	for _, aid := range attackers {
		a := w.units[w.index(aid)].c
		for _, bi := range blockers {
			if simCanBlock(a, w.units[bi].c) {
				opts = append(opts, decision.Option{Index: len(opts), Kind: "block", Obj: w.units[bi].id, Attacker: aid, Player: defender})
			}
		}
	}
	out := make(map[state.ObjID][]state.ObjID, len(attackers))
	if len(opts) == 0 {
		return out
	}
	d := decision.Decision{Kind: decision.KBlockers, Player: defender, Options: opts, Min: 0, Max: len(opts)}
	for _, ci := range w.board(base).chooseBlockers(&d) {
		if ci < 0 || ci >= len(opts) {
			continue
		}
		o := opts[ci]
		out[o.Attacker] = append(out[o.Attacker], o.Obj)
	}
	return out
}

// resolve applies one combat: attackers (controlled by the attacking
// player) against the predicted blocks, then taps the non-Vigilance
// attackers.
func (w *simWorld) resolve(attacker, defender state.PlayerID, attackers []state.ObjID, blocks map[state.ObjID][]state.ObjID) {
	var deaths []int
	for _, aid := range attackers {
		ai := w.index(aid)
		a := w.units[ai].c
		bl := blocks[aid]
		if len(bl) == 0 {
			dmg := a.Power
			if a.hasKeyword("Double Strike") {
				dmg *= 2
			}
			if dmg > 0 {
				w.life[defender] -= dmg
				if a.hasKeyword("Lifelink") {
					w.life[attacker] += dmg
				}
			}
			continue
		}
		team := make([]Creature, len(bl))
		idx := make([]int, len(bl))
		var absorb int32
		for k, bid := range bl {
			idx[k] = w.index(bid)
			team[k] = w.units[idx[k]].c
			absorb += team[k].remTough()
		}
		aDead, dead := blockCombat(a, team)
		allDead := true
		for k := range dead {
			if dead[k] && !team[k].hasKeyword("Indestructible") {
				deaths = append(deaths, idx[k])
			} else {
				allDead = false
			}
			if team[k].hasKeyword("Lifelink") && team[k].Power > 0 {
				w.life[defender] += team[k].Power
			}
		}
		if aDead && !a.hasKeyword("Indestructible") {
			deaths = append(deaths, ai)
		}
		if a.hasKeyword("Lifelink") && a.Power > 0 {
			w.life[attacker] += a.Power
		}
		if allDead && a.hasKeyword("Trample") && a.Power > absorb {
			w.life[defender] -= a.Power - absorb
		}
	}
	for _, i := range deaths {
		u := &w.units[i]
		if u.dead {
			continue
		}
		// Undying / Persist (CR 702.93 / 702.79): the creature returns
		// with a +1/+1 (Undying) or -1/-1 (Persist) counter; the Board
		// cannot see whether it already carries one, so the first death
		// in the simulation returns it once, untapped and undamaged, and
		// strips the keyword.
		if u.c.hasKeyword("Undying") || u.c.hasKeyword("Persist") {
			delta := int32(1)
			if u.c.hasKeyword("Persist") {
				delta = -1
			}
			u.c.Power += delta
			u.c.Toughness += delta
			u.c.Damage = 0
			u.c.Tapped = false
			u.c.Keywords = withoutKeywords(u.c.Keywords, "Undying", "Persist")
			if u.c.Toughness > 0 {
				continue
			}
		}
		u.dead = true
	}
	for _, aid := range attackers {
		i := w.index(aid)
		if !w.units[i].c.hasKeyword("Vigilance") {
			w.units[i].c.Tapped = true
		}
	}
}

// followUp simulates one later combat by side: side's creatures untap and
// all damage wears off (the other side's permanents stay as they are -- its
// attackers are still tapped during side's turn), the default attacker
// picks side's attack against other, and the default block rule answers
// for other. The crack-back is followUp(opp, me); a third ply is our next
// turn's followUp(me, opp).
func (w *simWorld) followUp(base Board, side, other state.PlayerID) {
	for i := range w.units {
		w.units[i].c.Damage = 0
		if w.units[i].c.Controller == side {
			w.units[i].c.Tapped = false
		}
	}
	var opts []decision.Option
	for _, u := range w.units {
		if !u.dead && u.c.Controller == side && u.c.Power > 0 && !u.c.hasKeyword("Defender") {
			opts = append(opts, decision.Option{Index: len(opts), Kind: "attacker", Obj: u.id, Player: other})
		}
	}
	if len(opts) == 0 {
		return
	}
	d := decision.Decision{Kind: decision.KAttackers, Player: side, Options: opts, Min: 0, Max: len(opts)}
	var atks []state.ObjID
	for _, ci := range w.board(base).chooseAttackersMode(&d, true, false) {
		if ci >= 0 && ci < len(opts) {
			atks = append(atks, opts[ci].Obj)
		}
	}
	if len(atks) == 0 {
		return
	}
	sort.Slice(atks, func(i, j int) bool { return atks[i] < atks[j] })
	w.resolve(side, other, atks, w.predictBlocks(base, other, atks))
}

// followUpGreedy is followUp for OUR later attack with a greedy one-ply
// improvement over the default attacker: starting from its set, each
// remaining attacker (ascending id) is added when the single resolved
// combat scores strictly better with it.
func (w *simWorld) followUpGreedy(base Board, side, other state.PlayerID, unit int64) {
	for i := range w.units {
		w.units[i].c.Damage = 0
		if w.units[i].c.Controller == side {
			w.units[i].c.Tapped = false
		}
	}
	var opts []decision.Option
	for _, u := range w.units {
		if !u.dead && u.c.Controller == side && u.c.Power > 0 && !u.c.hasKeyword("Defender") {
			opts = append(opts, decision.Option{Index: len(opts), Kind: "attacker", Obj: u.id, Player: other})
		}
	}
	if len(opts) == 0 {
		return
	}
	d := decision.Decision{Kind: decision.KAttackers, Player: side, Options: opts, Min: 0, Max: len(opts)}
	in := make(map[state.ObjID]bool, len(opts))
	for _, ci := range w.board(base).chooseAttackersMode(&d, true, false) {
		if ci >= 0 && ci < len(opts) {
			in[opts[ci].Obj] = true
		}
	}
	set := func() []state.ObjID {
		var atks []state.ObjID
		for _, o := range opts {
			if in[o.Obj] {
				atks = append(atks, o.Obj)
			}
		}
		return atks
	}
	try := func() int64 {
		atks := set()
		sw := w.clone()
		if len(atks) > 0 {
			sw.resolve(side, other, atks, sw.predictBlocks(base, other, atks))
		}
		return sw.score(unit)
	}
	cur := try()
	for _, o := range opts {
		if in[o.Obj] {
			continue
		}
		in[o.Obj] = true
		if s := try(); s > cur {
			cur = s
		} else {
			in[o.Obj] = false
		}
	}
	atks := set()
	if len(atks) == 0 {
		return
	}
	w.resolve(side, other, atks, w.predictBlocks(base, other, atks))
}

// chooseAttackersSim is the KAttackers answer of the attack-sim arm.
func (b Board) chooseAttackersSim(d *decision.Decision, p *AttackSimParams) []int {
	base := b.chooseAttackersMode(d, true, false)
	if len(d.Options) == 0 {
		return base
	}
	me := d.Player
	defender := d.Options[0].Player
	for i := range d.Options {
		o := &d.Options[i]
		if o.Player != defender || o.Battle != 0 || o.Kind != "attacker" {
			return base
		}
	}
	if _, ok := b.Life[defender]; !ok {
		return base
	}
	if _, ok := b.Life[me]; !ok {
		return base
	}
	// One option per attacker (two-player, player defender).
	optOf := make(map[state.ObjID]int, len(d.Options))
	var free, forced []state.ObjID
	baseSet := make(map[state.ObjID]bool, len(base))
	for _, ci := range base {
		baseSet[d.Options[ci].Obj] = true
	}
	for i := range d.Options {
		o := &d.Options[i]
		if _, dup := optOf[o.Obj]; dup {
			return base
		}
		optOf[o.Obj] = i
		c, ok := b.Creatures[o.Obj]
		if !ok {
			return base
		}
		if o.Required || (baseSet[o.Obj] && b.closesClock(defender, o.Obj, c)) {
			forced = append(forced, o.Obj)
			continue
		}
		if c.Power <= 0 {
			continue
		}
		free = append(free, o.Obj)
	}
	if len(free) == 0 || len(free) > maxSimAttackers {
		return base
	}

	w := newSimWorld(b, me, defender)

	maxAtk := d.Max
	evalMask := func(mask uint32) (int64, bool) {
		atks := make([]state.ObjID, 0, len(forced)+len(free))
		atks = append(atks, forced...)
		for i, id := range free {
			if mask&(1<<uint(i)) != 0 {
				atks = append(atks, id)
			}
		}
		if maxAtk >= 0 && len(atks) > maxAtk {
			return 0, false
		}
		sort.Slice(atks, func(i, j int) bool { return atks[i] < atks[j] })
		sw := w.clone()
		if len(atks) > 0 {
			sw.resolve(me, defender, atks, sw.predictBlocks(b, defender, atks))
		}
		if p.CrackBack && sw.life[defender] > 0 {
			sw.followUp(b, defender, me)
			if p.NextTurn && sw.life[me] > 0 {
				if p.NextGreedy {
					sw.followUpGreedy(b, me, defender, p.LifeUnit)
				} else {
					sw.followUp(b, me, defender)
				}
			}
		}
		return sw.score(p.LifeUnit), true
	}

	var baseMask uint32
	for i, id := range free {
		if baseSet[id] {
			baseMask |= 1 << uint(i)
		}
	}
	baseScore, ok := evalMask(baseMask)
	if !ok {
		return base
	}
	bestMask, bestScore := baseMask, baseScore
	consider := func(mask uint32) {
		if mask == bestMask {
			return
		}
		s, ok := evalMask(mask)
		if !ok {
			return
		}
		if s > bestScore || (p.TieAggro && s == bestScore && bits.OnesCount32(mask) > bits.OnesCount32(bestMask)) {
			bestMask, bestScore = mask, s
		}
	}
	n := len(free)
	if n <= p.MaxEnum {
		for mask := uint32(0); mask < 1<<uint(n); mask++ {
			consider(mask)
		}
	} else {
		all := uint32(1)<<uint(n) - 1
		climb := func(start uint32) {
			cur := start
			curScore, ok := evalMask(cur)
			if !ok {
				return
			}
			if curScore > bestScore {
				bestMask, bestScore = cur, curScore
			}
			for pass := 0; pass < 3; pass++ {
				improved := false
				for i := 0; i < n; i++ {
					m := cur ^ (1 << uint(i))
					s, ok := evalMask(m)
					if ok && s > curScore {
						cur, curScore, improved = m, s, true
						if s > bestScore {
							bestMask, bestScore = m, s
						}
					}
				}
				if !improved {
					break
				}
			}
		}
		climb(baseMask)
		climb(all)
	}
	if bestMask == baseMask || bestScore < baseScore+p.Margin || (bestScore == baseScore+p.Margin && !(p.TieAggro && p.Margin == 0)) {
		return base
	}
	out := make([]int, 0, len(forced)+n)
	for _, id := range forced {
		out = append(out, optOf[id])
	}
	for i, id := range free {
		if bestMask&(1<<uint(i)) != 0 {
			out = append(out, optOf[id])
		}
	}
	sort.Ints(out)
	return out
}
