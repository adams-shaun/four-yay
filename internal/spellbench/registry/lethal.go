package registry

// lethal is a conservative "take the kill" decorator. At the seat's own
// declare-attackers decision and at its main-phase priority decisions it
// asks one question -- is there a line that deals at least the opponent's
// current life total this turn, guaranteed? -- and if so takes that line's
// next step. Every decision the idea does not name, and every decision where
// no guaranteed lethal is found, is delegated to the wrapped seat unchanged.
//
// The line it looks for has two halves:
//
//   - Combat. The engine's own declare-attackers options already name every
//     creature that can legally attack (summoning sickness and haste are the
//     engine's business), so the attacker set is read from those options. The
//     damage it guarantees is a lower bound computed per attacker against the
//     WHOLE untapped opposing pool (see guaranteedDamage): the opponent owns
//     the blocking assignment, so summing per-attacker minima over a superset
//     of each attacker's assignments can only under-count -- the bound is
//     pessimistic by construction and may miss a kill, but it never claims
//     one the opponent can prevent. Untapped opposing creatures block; an
//     evasive attacker only connects when no blocker can block it (flying
//     needs flying or reach; unblockable always connects; horsemanship,
//     shadow and skulk exclude the blockers their keyword denies; a printed
//     CantBlockBy static on the attacker itself excludes each blocker its
//     ValidBlocker$ filter names, when that filter is one the decorator can
//     decide -- the static is read from the attacker's card IR, whose name
//     comes from the projected view or, on the Board surface, the engine's
//     own "Attack with <name> at ..." attacker label; trample carries only
//     the excess over the biggest soak); a
//     menace attacker needs two blockers; a non-striking attacker the pool
//     holds a first/double-striking (or deathtouch) killer for deals nothing;
//     every other blocked attacker deals nothing.
//
//   - Burn and pump. At a main-phase priority decision the decorator reads
//     the printed IR of each offered ordinary cast or non-mana activated
//     ability (registry.SetCardLookup) and prices a burn that can target a
//     player and a pump that can target one of our own creatures (or boost
//     its own source through Defined$ Self), against the mana the engine
//     already proved payable by offering the action. A burn is cast when it
//     reaches lethal on its own or together with the guaranteed attack; a
//     pump is cast when it lifts the guaranteed attack to lethal. The
//     following target decision (KTarget) aims the burn at the opponent or
//     the pump at the chosen attacker, so the line's next step is taken
//     without the wrapped seat redirecting it.
//
// Everything else -- blockers, targets for spells the decorator did not
// choose, a burn/pump that does not reach lethal, a combat whose guaranteed
// damage falls short -- goes through untouched, and the wrapped seat is
// consulted exactly once per decision (so a seed-streamed inner draws the
// same numbers it would have drawn bare). The decorator draws no randomness
// of its own and iterates only slices or sorted maps, so it is deterministic.

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func init() {
	RegisterDecorator("lethal", newLethal)
}

// newLethal wraps inner. As passguard does, the wrapper keeps inner's
// BoardSeat-ness: an inner that answers from a botpolicy.Board (the
// production bot) is wrapped in a BoardSeat, so the engine keeps handing it
// the board instead of a projected View.
func newLethal(inner seat.Seat, _ uint64) seat.Seat {
	core := &lethalCore{}
	base := lethalSeat{inner: inner, core: core}
	if _, ok := inner.(seat.BoardSeat); ok {
		return lethalBoard{base}
	}
	return base
}

// lethalCore carries the decorator's own state: the target of a burn or pump
// the decorator just chose to cast, consumed by the next KTarget. It is
// per-seat (one core per wrapped seat) and draw-free.
type lethalCore struct {
	pending *lethalTarget
}

// lethalTarget names the spell whose target the decorator must arm.
type lethalTarget struct {
	burn     bool
	attacker state.ObjID // pump only
	seq      uint64      // the priority decision's Seq that armed it
}

// lethalSeat is the plain wrapper over a non-BoardSeat inner.
type lethalSeat struct {
	inner seat.Seat
	core  *lethalCore
}

func (s lethalSeat) UnwrapSeat() seat.Seat { return s.inner }

func (s lethalSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return in, err
	}
	return s.core.guard(spaceFromView(v, d.Player), &d, in), nil
}

func (s lethalSeat) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

// lethalBoard is the wrapper over a BoardSeat inner.
type lethalBoard struct {
	lethalSeat
}

func (s lethalBoard) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.(seat.BoardSeat).DecideBoard(ctx, b, d)
	if err != nil {
		return in, err
	}
	return s.core.guard(spaceFromBoard(b, d.Player), &d, in), nil
}

// lethalCre is one battlefield creature as the decorator needs it: the
// projected power/toughness and the few keywords the damage bound reads.
type lethalCre struct {
	obj         state.ObjID
	power       int32
	toughness   int32
	controller  state.PlayerID
	tapped      bool
	sick        bool
	flying      bool
	reach       bool
	menace      bool
	trample     bool
	unblockable bool
	firstStrk   bool
	doubleStrk  bool
	deathtouch  bool
	defender    bool

	// Evasion beyond flying/menace/unblockable. horsemanship, shadow and
	// skulk are whole keywords; cbb carries a printed CantBlockBy static that
	// applies to this card itself, with cbbBlocks holding its ValidBlocker$
	// filter (empty meaning nothing may block it, already folded into
	// unblockable). Every restriction the decorator cannot decide from the
	// public creature picture fails toward "may block" (see canBlock).
	horsemanship bool
	shadow       bool
	skulk        bool
	cbb          bool
	cbbBlocks    string
}

func (c lethalCre) canAttack() bool { return !c.defender && c.power > 0 && !c.tapped && !c.sick }

// lethalSpace is the seat-visible picture both surfaces are normalised into.
// cre and names are lookup tables only, never ranged into a choice; order is
// the deterministic object order a builder fills from sorted keys.
type lethalSpace struct {
	valid     bool
	me, opp   state.PlayerID
	oppLife   int32
	myTurn    bool
	firstMain bool
	cre       map[state.ObjID]lethalCre
	names     map[state.ObjID]string
	order     []state.ObjID
}

func newLethalSpace() lethalSpace {
	return lethalSpace{cre: map[state.ObjID]lethalCre{}, names: map[state.ObjID]string{}}
}

// put records one creature, keeping order sorted by object id.
func (w *lethalSpace) put(c lethalCre) {
	if _, dup := w.cre[c.obj]; !dup {
		w.order = append(w.order, c.obj)
	}
	w.cre[c.obj] = c
}

func (w *lethalSpace) finish() lethalSpace {
	sort.Slice(w.order, func(i, j int) bool { return w.order[i] < w.order[j] })
	return *w
}

// spaceFromView lifts the analysis out of a projected View. me is the
// deciding seat (the decision's Player); only public battlefields, the
// opponent's life and the seat's own zones are read.
func spaceFromView(v view.View, me state.PlayerID) lethalSpace {
	w := newLethalSpace()
	w.me = me
	w.myTurn = v.Active == me
	w.firstMain = v.Phase == "main1"
	found := false
	for i := range v.Players {
		p := &v.Players[i]
		if p.ID != me && !p.Lost && !found {
			w.opp, w.oppLife, found = p.ID, p.Life, true
		}
		for j := range p.Battlefield {
			cv := &p.Battlefield[j]
			if !isCreatureTypeLine(cv.Types) {
				continue
			}
			c := creFromCardView(cv)
			applyPrintedCantBlockBy(cv.Name, &c)
			w.put(c)
		}
		// Own cards in every zone the seat may read, for cast-name
		// resolution (a burn's Obj names the hand card).
		if p.ID == me {
			for _, zone := range [][]view.CardView{p.Battlefield, p.Hand, p.Graveyard, p.Exile} {
				for j := range zone {
					if cv := &zone[j]; cv.Name != "" {
						w.names[cv.ID] = cv.Name
					}
				}
			}
		}
	}
	for i := range v.Stack {
		if sv := &v.Stack[i]; sv.Card != nil && sv.Card.Name != "" {
			w.names[sv.ID] = sv.Card.Name
		}
	}
	if !found {
		return w.finish()
	}
	w.valid = true
	return w.finish()
}

// spaceFromBoard lifts the analysis out of a botpolicy.Board (the
// board-shaped surface the production bot answers from). A summoning-sick
// creature is read from the board's Card census; a creature the census does
// not carry is treated as sick, the conservative direction (it can never
// make a kill appear that the board cannot deliver).
func spaceFromBoard(b botpolicy.Board, me state.PlayerID) lethalSpace {
	w := newLethalSpace()
	w.me = me
	w.myTurn = b.MyTurn
	w.firstMain = b.IsMain && b.FirstMain
	players := make([]state.PlayerID, 0, len(b.Life))
	for p := range b.Life {
		players = append(players, p)
	}
	sort.Slice(players, func(i, j int) bool { return players[i] < players[j] })
	found := false
	for _, p := range players {
		if p != me && !found {
			w.opp, w.oppLife, found = p, b.Life[p], true
		}
	}
	ids := make([]state.ObjID, 0, len(b.Creatures))
	for id := range b.Creatures {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		c := b.Creatures[id]
		lc := lethalCre{obj: id, power: c.Power, toughness: c.Toughness, controller: c.Controller, tapped: c.Tapped}
		if cd, ok := b.Cards[id]; ok {
			lc.sick = cd.Sick
		} else {
			lc.sick = true
		}
		for _, k := range c.Keywords {
			lc.setKeyword(k)
		}
		if lc.power < 0 {
			lc.power = 0
		}
		w.put(lc)
	}
	if !found {
		return w.finish()
	}
	w.valid = true
	return w.finish()
}

func creFromCardView(cv *view.CardView) lethalCre {
	c := lethalCre{
		obj: cv.ID, power: cv.Power, toughness: cv.Toughness, controller: cv.Controller,
		tapped: cv.Tapped, sick: cv.SummonSick,
	}
	for _, k := range cv.Keywords {
		c.setKeyword(k)
	}
	if c.power < 0 {
		c.power = 0
	}
	if c.toughness < 0 {
		c.toughness = 0
	}
	return c
}

func (c *lethalCre) setKeyword(k string) {
	switch strings.ToLower(cards.KeywordHead(k)) {
	case "flying":
		c.flying = true
	case "reach":
		c.reach = true
	case "menace":
		c.menace = true
	case "trample":
		c.trample = true
	case "unblockable":
		c.unblockable = true
	case "horsemanship":
		c.horsemanship = true
	case "shadow":
		c.shadow = true
	case "skulk":
		c.skulk = true
	case "first strike":
		c.firstStrk = true
	case "double strike":
		c.doubleStrk = true
	case "deathtouch":
		c.deathtouch = true
	case "defender":
		c.defender = true
	}
}

// potentialAttackers is the decorator's own inflation of "what could attack
// later this turn": every untapped, non-sick creature we control with power
// above zero that is not a defender. The engine's declare-attackers decision
// is authoritative at combat time; this is the pre-combat estimate the
// priority-side burn and pump reach against.
func (w lethalSpace) potentialAttackers() []lethalCre {
	var out []lethalCre
	for _, id := range w.order {
		c := w.cre[id]
		if c.controller == w.me && c.canAttack() {
			out = append(out, c)
		}
	}
	return out
}

// oppBlockers is the opponent's worst-case defensive pool: every untapped
// creature they control (summoning sickness does not stop a block).
func (w lethalSpace) oppBlockers() []lethalCre {
	var out []lethalCre
	for _, id := range w.order {
		c := w.cre[id]
		if c.controller == w.opp && !c.tapped {
			out = append(out, c)
		}
	}
	return out
}

// guard applies the one idea. A decision the idea does not cover, or one
// where no guaranteed lethal is found, is the intent as inner answered it.
func (c *lethalCore) guard(w lethalSpace, d *decision.Decision, in decision.Intent) decision.Intent {
	if !w.valid {
		return in
	}
	switch d.Kind {
	case decision.KAttackers:
		return c.attackers(w, d, in)
	case decision.KPriority:
		return c.priority(w, d, in)
	case decision.KTarget:
		return c.target(w, d, in)
	}
	return in
}

// attackers takes the attack when the offered attacker set's guaranteed
// damage reaches the opponent's life.
func (c *lethalCore) attackers(w lethalSpace, d *decision.Decision, in decision.Intent) decision.Intent {
	if w.oppLife <= 0 {
		return in
	}
	type group struct {
		obj          state.ObjID
		oppIdx       int
		label        string
		required     bool
		reqElsewhere bool
	}
	byObj := map[state.ObjID]*group{} // lookup only
	var gs []*group
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != "attacker" || o.Obj == 0 {
			continue
		}
		g, ok := byObj[o.Obj]
		if !ok {
			g = &group{obj: o.Obj, oppIdx: -1, label: o.Label}
			byObj[o.Obj] = g
			gs = append(gs, g)
		}
		targetsOpp := o.Player == w.opp && o.Battle == 0
		if o.Required {
			g.required = true
			if !targetsOpp {
				g.reqElsewhere = true
			}
		}
		if targetsOpp && g.oppIdx < 0 {
			g.oppIdx = o.Index
		}
	}
	// A required attacker aimed at something other than the opponent cannot
	// be part of "kill the opponent", and its obligation forces a
	// declaration the decorator does not model: delegate.
	var chosen []int
	var atk []lethalCre
	for _, g := range gs {
		if g.reqElsewhere {
			return in
		}
		if g.oppIdx < 0 {
			continue
		}
		cr, ok := w.cre[g.obj]
		if !ok {
			if g.required {
				return in
			}
			continue
		}
		// The Board path carries no creature names, so a printed CantBlockBy
		// static on an attacker is resolved from the engine's own "Attack
		// with <name> at ..." option label here (the View path already filled
		// w.names). An unresolvable name leaves the attacker fully blockable.
		if name := w.names[g.obj]; name != "" {
			applyPrintedCantBlockBy(name, &cr)
		} else {
			applyPrintedCantBlockBy(attackerOptionName(g.label), &cr)
		}
		chosen = append(chosen, g.oppIdx)
		atk = append(atk, cr)
	}
	if len(chosen) == 0 {
		return in
	}
	if guaranteedDamage(atk, w.oppBlockers()) < w.oppLife {
		return in
	}
	out := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: chosen}
	if d.Validate(out) != nil {
		return in
	}
	return out
}

// priority casts one burn or pump when it reaches guaranteed lethal. It
// handles the whole line one step at a time: after the spell resolves the
// engine poses priority again and the life total (or the attacker's power)
// has moved, so the next step -- another spell, then the attack -- is chosen
// from the fresh state.
func (c *lethalCore) priority(w lethalSpace, d *decision.Decision, in decision.Intent) decision.Intent {
	c.pending = nil
	if w.oppLife <= 0 {
		return in
	}
	atk := w.potentialAttackers()
	blockers := w.oppBlockers()
	projected := int32(0)
	if w.myTurn && w.firstMain {
		projected = guaranteedDamage(atk, blockers)
	}
	// Burn: the best literal damage among offered ordinary casts and non-mana
	// activated abilities that can target a player.
	burnIdx, burnDmg := -1, int32(0)
	for i := range d.Options {
		o := &d.Options[i]
		n, ok := optionBurn(w, o)
		if !ok || n <= burnDmg {
			continue
		}
		burnDmg, burnIdx = n, o.Index
	}
	if burnIdx >= 0 && (burnDmg >= w.oppLife || projected+burnDmg >= w.oppLife) {
		c.pending = &lethalTarget{burn: true, seq: d.Seq}
		out := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{burnIdx}}
		if d.Validate(out) == nil {
			return out
		}
		c.pending = nil
	}
	// Pump: the best power boost among offered ordinary casts and non-mana
	// activated abilities that boost one of our own creatures, priced by how
	// much guaranteed attack it adds. It is only considered when our attack is
	// still ahead this turn (our first main phase); otherwise a pump buys no
	// damage the decorator can count. A "Defined$ Self" ability applies to its
	// own source with no target ask, so it is priced on that attacker and
	// arms no pending target; a targeted pump is armed at the chosen attacker.
	if len(atk) > 0 && w.myTurn && w.firstMain {
		pumpIdx, pumpGain, pumpObj, pumpSelf := -1, int32(0), state.ObjID(0), false
		for i := range d.Options {
			o := &d.Options[i]
			n, self, ok := optionPump(w, o)
			if !ok || n <= 0 {
				continue
			}
			if self {
				g := pumpGainOn(atk, blockers, o.Obj, n)
				if g > pumpGain {
					pumpGain, pumpIdx, pumpObj, pumpSelf = g, o.Index, o.Obj, true
				}
				continue
			}
			if gain, obj := bestPump(atk, blockers, n); gain > pumpGain {
				pumpGain, pumpIdx, pumpObj, pumpSelf = gain, o.Index, obj, false
			}
		}
		if pumpIdx >= 0 && pumpGain > 0 && projected+pumpGain >= w.oppLife {
			if !pumpSelf {
				c.pending = &lethalTarget{attacker: pumpObj, seq: d.Seq}
			}
			out := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pumpIdx}}
			if d.Validate(out) == nil {
				return out
			}
			c.pending = nil
		}
	}
	return in
}

// target arms the burn or pump the decorator just cast, so the wrapped seat
// cannot redirect the line. The pending line is consumed only by a target
// decision posed AFTER the priority that armed it (a greater Seq): a cast
// aborted without a target ask must not let a later unrelated KTarget be
// hijacked and re-aimed. Any other target decision, and a pending line whose
// matching option is absent, is delegated.
func (c *lethalCore) target(w lethalSpace, d *decision.Decision, in decision.Intent) decision.Intent {
	p := c.pending
	c.pending = nil
	if p == nil || d.Seq <= p.seq {
		return in
	}
	for i := range d.Options {
		o := &d.Options[i]
		if p.burn {
			if o.Kind == "player" && o.Player == w.opp && o.Obj == 0 {
				out := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				if d.Validate(out) == nil {
					return out
				}
			}
			continue
		}
		if o.Obj == p.attacker {
			out := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
			if d.Validate(out) == nil {
				return out
			}
		}
	}
	return in
}

// attackerOptionName extracts the creature name from an attacker option's
// "Attack with <name> at <defender>" label (rules/combat.go). The Board path
// carries no other name source, so this is how a printed CantBlockBy static
// on an attacker is resolved there; a label without the prefix or the " at "
// separator yields "" and the attacker stays fully blockable.
func attackerOptionName(label string) string {
	const pfx = "Attack with "
	if !strings.HasPrefix(label, pfx) {
		return ""
	}
	rest := strings.TrimPrefix(label, pfx)
	if i := strings.Index(rest, " at "); i > 0 {
		return rest[:i]
	}
	return ""
}

// guaranteedDamage is a lower bound on the combat damage a set of attackers
// deals this turn against the opponent's own worst-case blocking. The
// opponent owns the assignment, so the bound must never exceed the minimum
// they can force; it is computed as the SUM of a PER-ATTACKER minimum, each
// priced against the WHOLE untapped defensive pool.
//
// That per-attacker-pool relaxation is the structural guarantee. A shared
// greedy over one assignment cannot be a bound: it can spend a blocker on the
// wrong attacker (a killer blocker routed to a harmless attacker, or the
// biggest blocker spent on a non-trampler) and claim damage the opponent
// simply denies by assigning otherwise. Summing minima taken over a superset
// of each attacker's possible assignments is provably <= the best disjoint
// assignment's damage, and it errs only downward -- the decorator may miss a
// kill, never invent one (the file's header invariant).
//
// Per attacker:
//   - unblockable, or flying with no flying/reach blocker in the pool,
//     connects in full (a double striker twice).
//   - a menace attacker needs two blockers, everything else one.
//   - a non-striking attacker the pool holds a KILLER for -- a first/double-
//     striking blocker whose power reaches its toughness, or any such striker
//     with deathtouch -- deals nothing (it dies in the first-strike step).
//   - any other blocked non-trampler deals nothing.
//   - a blocked trampler carries only its excess over the pool's biggest
//     soak: the `need` largest-toughness blockers it could be assigned. A
//     blocked attacker prices a single step -- a first-striking blocker may
//     kill it between the steps, so the double-strike second step is never
//     claimed.
func guaranteedDamage(atk, blockers []lethalCre) int32 {
	var total int32
	for _, a := range atk {
		total += minDamage(a, blockers)
	}
	return total
}

// minDamage is the least damage one attacker can be held to using the whole
// untapped pool. Blocker reuse across attackers is the deliberate relaxation
// documented on guaranteedDamage.
func minDamage(a lethalCre, blockers []lethalCre) int32 {
	if a.unblockable {
		return strikePower(a, false)
	}
	// Lane: a blocker the attacker's evasion shapes exclude cannot block it.
	var pool []lethalCre
	for _, b := range blockers {
		if !a.canBlock(b) {
			continue
		}
		pool = append(pool, b)
	}
	need := 1
	if a.menace {
		need = 2
	}
	if len(pool) < need {
		return strikePower(a, false) // cannot be blocked by any assignment
	}
	// A killer ends a non-striking attacker before it deals damage. One that
	// also strikes first lands its damage in the same step and is safe.
	if !a.firstStrk && !a.doubleStrk {
		for _, b := range pool {
			if killerKills(b, a) {
				return 0
			}
		}
	}
	if !a.trample {
		return 0 // blocked and not trampling
	}
	// Trample: the opponent maximises the soak, so price the `need` biggest
	// blockers in the pool (toughness desc, then object id -- deterministic).
	soak := append([]lethalCre(nil), pool...)
	sort.Slice(soak, func(i, j int) bool {
		if soak[i].toughness != soak[j].toughness {
			return soak[i].toughness > soak[j].toughness
		}
		return soak[i].obj < soak[j].obj
	})
	var sum int32
	for i := 0; i < need; i++ {
		sum += soak[i].toughness
	}
	if p := strikePower(a, true); p > sum {
		return p - sum
	}
	return 0
}

// canBlock reports whether blocker b can legally block attacker a under the
// evasion shapes the decorator models: flying/reach, menace (handled as a
// blocker-count need), unblockable, horsemanship, shadow, skulk, and a printed
// CantBlockBy static that applies to the attacker itself. Every restriction
// the decorator cannot decide from the public creature picture (fear,
// intimidate, protection, landwalk, a CantBlockBy ValidBlocker$ term it does
// not read) leaves the blocker in the pool -- the conservative direction,
// because a blocker wrongly kept can only lower the bound, never invent
// damage (the file's header invariant).
func (a lethalCre) canBlock(b lethalCre) bool {
	if a.unblockable {
		return false
	}
	if a.cbb {
		if a.cbbBlocks == "" {
			return false
		}
		if forbids, ok := cbbForbids(a.cbbBlocks, b); ok && forbids {
			return false
		}
	}
	if a.flying && !b.flying && !b.reach {
		return false
	}
	if a.horsemanship && !b.horsemanship {
		return false
	}
	if a.shadow && !b.shadow {
		return false
	}
	if a.skulk && b.power > a.power {
		return false
	}
	return true
}

// printedCantBlockBy reports whether card cd carries a printed CantBlockBy
// static that applies to the card itself, and its ValidBlocker$ filter (""
// meaning nothing may block it). A static whose ValidAttacker$ names anything
// else -- a lord granting the restriction to other creatures, or a remembered
// object -- is refused: the decorator only reads the restriction printed on
// the attacker it prices, and everything else stays conservative.
func printedCantBlockBy(cd *cards.Card) (string, bool) {
	f := firstFace(cd)
	if f == nil {
		return "", false
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if st.Mode != "CantBlockBy" {
			continue
		}
		switch strings.TrimSpace(st.Params["ValidAttacker"]) {
		case "Creature.Self", "Card.Self":
			return strings.TrimSpace(st.Params["ValidBlocker"]), true
		}
	}
	return "", false
}

// applyPrintedCantBlockBy folds a creature's printed CantBlockBy static into
// its evasion model. A card whose name or IR the decorator cannot resolve is
// left fully blockable (conservative).
func applyPrintedCantBlockBy(name string, c *lethalCre) {
	if name == "" || cardLookup == nil {
		return
	}
	cd := cardLookup(name)
	if cd == nil {
		return
	}
	vb, ok := printedCantBlockBy(cd)
	if !ok {
		return
	}
	if vb == "" {
		c.unblockable = true
		return
	}
	c.cbb = true
	c.cbbBlocks = vb
}

// cbbForbids evaluates a CantBlockBy ValidBlocker$ filter against blocker b.
// Forge filters are comma-separated alternatives of '+'-joined terms. It is
// deliberately fail-closed: only terms the decorator can decide from the
// public creature picture are read, and if no whole alternative is decidable
// the second return is false -- the caller then keeps the blocker, the
// direction that can only lower the bound.
func cbbForbids(filter string, b lethalCre) (bool, bool) {
	decidable := false
	for _, alt := range strings.Split(filter, ",") {
		matches, okAll := true, true
		for _, t := range strings.Split(alt, "+") {
			m, ok := cbbTerm(strings.TrimSpace(t), b)
			if !ok {
				okAll = false
				break
			}
			if !m {
				matches = false
				break
			}
		}
		if !okAll {
			continue
		}
		decidable = true
		if matches {
			return true, true
		}
	}
	return false, decidable
}

// cbbTerm decides one ValidBlocker$ term. The first return is the term's
// truth for blocker b; the second is false when the term names a fact the
// decorator does not carry (a colour, an artifact-ness, a creature type, a
// bare 'Creature.Self'), which callers treat as undecidable.
func cbbTerm(raw string, b lethalCre) (bool, bool) {
	t := strings.TrimPrefix(strings.TrimSpace(raw), "Creature.")
	t = strings.TrimPrefix(t, "Card.")
	low := strings.ToLower(t)
	switch low {
	case "creature", "card":
		return true, true
	case "withflying":
		return b.flying, true
	case "withoutflying":
		return !b.flying, true
	case "withreach":
		return b.reach, true
	case "withoutreach":
		return !b.reach, true
	}
	preds := []struct {
		pfx  string
		want func(int32) bool
	}{
		{"powerle", func(n int32) bool { return b.power <= n }},
		{"powerlt", func(n int32) bool { return b.power < n }},
		{"powerge", func(n int32) bool { return b.power >= n }},
		{"powergt", func(n int32) bool { return b.power > n }},
		{"powereq", func(n int32) bool { return b.power == n }},
	}
	for _, p := range preds {
		if strings.HasPrefix(low, p.pfx) {
			n, ok := literalInt(t[len(p.pfx):])
			if !ok {
				return false, false
			}
			return p.want(n), true
		}
	}
	return false, false
}

// killerKills reports whether blocker b kills attacker a in the first-strike
// step, so that a -- which does not strike first itself -- never deals damage.
// A first/double-striking blocker kills with power reaching a's toughness, or
// with deathtouch at any positive power.
func killerKills(b, a lethalCre) bool {
	if !b.firstStrk && !b.doubleStrk {
		return false
	}
	return b.power >= a.toughness || (b.deathtouch && b.power > 0)
}

// strikePower is the damage an attacker prices per the strike model. A
// double striker with no blocker in front of it deals its power twice (two
// damage steps); once blocked, only one step is counted (see
// guaranteedDamage).
func strikePower(c lethalCre, blocked bool) int32 {
	if c.doubleStrk && !blocked {
		return 2 * c.power
	}
	return c.power
}

// bestPump returns the largest guaranteed-attack gain a +n/+n pump on one of
// atk adds, and the object to aim it at (the first maximum in object order).
// The gain is measured against the COMBINED attack -- guaranteedDamage over
// the whole attacker set with the candidate pumped, minus the same over the
// whole set unpumped -- because the blockers are shared between the
// attackers: an isolated per-attacker delta can exceed the true combined
// delta (pumping one attacker past another in power order re-routes the
// blocking), and a claim built on it prices a lethal the line cannot deliver.
func bestPump(atk, blockers []lethalCre, n int32) (int32, state.ObjID) {
	base := guaranteedDamage(atk, blockers)
	bestGain := int32(0)
	var bestID state.ObjID
	for i := range atk {
		pumped := append([]lethalCre(nil), atk...)
		pumped[i].power += n
		gain := guaranteedDamage(pumped, blockers) - base
		a := atk[i]
		if gain > bestGain || (gain == bestGain && gain > 0 && (bestID == 0 || a.obj < bestID)) {
			bestGain, bestID = gain, a.obj
		}
	}
	return bestGain, bestID
}

// pumpGainOn is bestPump for a pump fixed to one object (a "Defined$ Self"
// activated ability): it is the combined-attack delta of boosting obj, or 0
// when obj is not one of the attackers this turn.
func pumpGainOn(atk, blockers []lethalCre, obj state.ObjID, n int32) int32 {
	base := guaranteedDamage(atk, blockers)
	pumped := append([]lethalCre(nil), atk...)
	found := false
	for i := range pumped {
		if pumped[i].obj == obj {
			pumped[i].power += n
			found = true
			break
		}
	}
	if !found {
		return 0
	}
	return guaranteedDamage(pumped, blockers) - base
}

// lookupCard resolves an offered ordinary cast's card IR. The name comes
// from the projected CardView when the surface carries one (the View path),
// else from the option's own "Cast <name>" label (the Board path); a label
// with any decoration ("(kicked)", " // ") is refused, because the
// decorator only prices the plain cast.
func lookupCard(w lethalSpace, o *decision.Option) *cards.Card {
	if cardLookup == nil {
		return nil
	}
	name := ""
	if n, ok := w.names[o.Obj]; ok {
		name = n
	} else {
		const pfx = "Cast "
		if !strings.HasPrefix(o.Label, pfx) {
			return nil
		}
		name = strings.TrimPrefix(o.Label, pfx)
		if name == "" || strings.ContainsAny(name, "()") || strings.Contains(name, "//") {
			return nil
		}
	}
	if name == "" {
		return nil
	}
	return cardLookup(name)
}

// abilityCard resolves an offered "ability" option's source card IR. The
// name comes from the projected CardView when the surface carries one (the
// View path), else from the option's own "<CardName>: <description>" label
// (the Board path, rules/legal.go's ability label).
func abilityCard(w lethalSpace, o *decision.Option) *cards.Card {
	if cardLookup == nil {
		return nil
	}
	name := ""
	if n, ok := w.names[o.Obj]; ok {
		name = n
	}
	if name == "" {
		if i := strings.Index(o.Label, ": "); i > 0 {
			name = o.Label[:i]
		}
	}
	if name == "" {
		return nil
	}
	return cardLookup(name)
}

// abilitySA resolves an offered "ability" option to the activated ability it
// anchors: Option.Ability is the flat pile index rules/legal.go offered from
// and rules/activate.go re-resolves (PileAbilityAt). A non-mutated permanent's
// pile is exactly its face's ability list, which is what the decorator
// indexes here; an out-of-range or missing ability is refused.
func abilitySA(w lethalSpace, o *decision.Option) (*cards.SA, bool) {
	if o.Ability < 0 {
		return nil, false
	}
	cd := abilityCard(w, o)
	if cd == nil {
		return nil, false
	}
	f := firstFace(cd)
	if f == nil || o.Ability >= len(f.Abilities) || f.Abilities[o.Ability] == nil {
		return nil, false
	}
	return f.Abilities[o.Ability], true
}

// optionBurn prices one offered burn -- an ordinary cast or a non-mana
// activated ability -- as a guaranteed literal player burn.
func optionBurn(w lethalSpace, o *decision.Option) (int32, bool) {
	switch o.Kind {
	case "cast":
		if o.Mode != "" {
			return 0, false
		}
		cd := lookupCard(w, o)
		if cd == nil {
			return 0, false
		}
		return spellBurn(cd)
	case "ability":
		sa, ok := abilitySA(w, o)
		if !ok {
			return 0, false
		}
		return burnFromSA(sa)
	}
	return 0, false
}

// optionPump prices one offered pump. The second return is true when the
// pump applies to its own source through "Defined$ Self" (no target ask), in
// which case the caller prices it on the option's own object.
func optionPump(w lethalSpace, o *decision.Option) (int32, bool, bool) {
	switch o.Kind {
	case "cast":
		if o.Mode != "" {
			return 0, false, false
		}
		cd := lookupCard(w, o)
		if cd == nil {
			return 0, false, false
		}
		n, ok := spellPumpOwn(cd)
		return n, false, ok
	case "ability":
		sa, ok := abilitySA(w, o)
		if !ok {
			return 0, false, false
		}
		n, self, ok := pumpFromSA(sa)
		return n, self, ok
	}
	return 0, false, false
}

// spellBurn reports a spell's printed, literal damage to a player. It reads
// the SP$ ability and its SubAbility chain for a DealDamage whose ValidTgts$
// admits a player and whose NumDmg$ is a positive literal; a variable amount
// or a creature-only target is not a guaranteed number and is refused.
func spellBurn(c *cards.Card) (int32, bool) {
	f := firstFace(c)
	if f == nil {
		return 0, false
	}
	for _, a := range f.Abilities {
		if a == nil || a.Kind != "SP" {
			continue
		}
		if n, ok := burnFromSA(a); ok {
			return n, true
		}
	}
	return 0, false
}

// burnFromSA reports a literal player burn reachable through an effect's
// SubAbility chain. It is shared by the spell (SP$) and activated-ability
// (AB$) paths.
func burnFromSA(root *cards.SA) (int32, bool) {
	for sa := root; sa != nil; sa = sa.Sub {
		if sa.API != "DealDamage" {
			continue
		}
		n, ok := literalInt(sa.Params["NumDmg"])
		if !ok || n <= 0 {
			continue
		}
		if admitsPlayer(sa.Params["ValidTgts"]) {
			return n, true
		}
	}
	return 0, false
}

// spellPumpOwn reports a spell's printed literal positive power boost on a
// creature the caster controls. Only an explicit own-creature target
// ("Creature.YouCtrl") is accepted: a bare "Creature" could be aimed at an
// opposing creature the caster cannot supply, and a "Defined$ Self" pump
// gives a spell no attacker to aim at, so both are refused.
func spellPumpOwn(c *cards.Card) (int32, bool) {
	f := firstFace(c)
	if f == nil {
		return 0, false
	}
	for _, a := range f.Abilities {
		if a == nil || a.Kind != "SP" {
			continue
		}
		if n, self, ok := pumpFromSA(a); ok && !self {
			return n, true
		}
	}
	return 0, false
}

// pumpFromSA reports a literal positive power boost reachable through an
// effect's SubAbility chain, and whether it applies to the ability's own
// source ("Defined$ Self", no target ask) or targets a creature the caster
// controls ("ValidTgts$ ...YouCtrl"). Any other target (a bare "Creature",
// an opposing target, a variable amount) is refused -- the decorator never
// prices a pump it cannot legally aim at one of its own attackers.
func pumpFromSA(root *cards.SA) (int32, bool, bool) {
	for sa := root; sa != nil; sa = sa.Sub {
		if sa.API != "Pump" && sa.API != "PumpAll" {
			continue
		}
		n, ok := literalInt(sa.Params["NumAtt"])
		if !ok || n <= 0 {
			continue
		}
		if strings.Contains(sa.Params["ValidTgts"], "YouCtrl") {
			return n, false, true
		}
		if strings.TrimSpace(sa.Params["Defined"]) == "Self" {
			return n, true, true
		}
	}
	return 0, false, false
}

// admitsPlayer reports whether a ValidTgts$ string admits a player target.
func admitsPlayer(v string) bool {
	if v == "" || strings.Contains(v, "YouCtrl") {
		return false
	}
	return v == "Any" || strings.Contains(v, "Player") || strings.Contains(v, "Opponent")
}

func firstFace(c *cards.Card) *cards.Face {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return nil
	}
	return c.Faces[0]
}

// literalInt parses a Forge literal amount ("3", "+2"); a variable reports
// false.
func literalInt(s string) (int32, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "+"))
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

// isCreatureTypeLine reports whether a joined type line names Creature.
func isCreatureTypeLine(types string) bool {
	for t := range strings.FieldsSeq(types) {
		if strings.EqualFold(t, "Creature") {
			return true
		}
	}
	return false
}
