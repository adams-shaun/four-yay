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
//     damage it guarantees is a lower bound under the WORST-CASE blocking
//     assignment for us: untapped opposing creatures block, an evasive
//     attacker only connects when no untapped blocker can block it (flying
//     needs flying or reach; unblockable always connects; trample carries
//     only the excess over the toughest assigned blocker's toughness), a
//     menace attacker needs two blockers, and every other blocked attacker
//     deals nothing. The bound is deliberately pessimistic: it may miss a
//     kill, but it never claims one that the opponent can prevent.
//
//   - Burn and pump. At a main-phase priority decision the decorator reads
//     the printed IR of each offered ordinary cast (registry.SetCardLookup)
//     and prices a burn that can target a player and a pump that can target
//     one of our own creatures, against the mana the engine already proved
//     payable by offering the cast. A burn is cast when it reaches lethal on
//     its own or together with the guaranteed attack; a pump is cast when it
//     lifts the guaranteed attack to lethal. The following target decision
//     (KTarget) aims the burn at the opponent or the pump at the chosen
//     attacker, so the line's next step is taken without the wrapped seat
//     redirecting it.
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
	doubleStrk  bool
	defender    bool
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
			w.put(creFromCardView(cv))
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
	case "double strike":
		c.doubleStrk = true
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
			g = &group{obj: o.Obj, oppIdx: -1}
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
	// Burn: the best literal damage among offered ordinary casts that can
	// target a player.
	burnIdx, burnDmg := -1, int32(0)
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != "cast" || o.Mode != "" {
			continue
		}
		cd := lookupCard(w, o)
		if cd == nil {
			continue
		}
		if n, ok := spellBurn(cd); ok && n > burnDmg {
			burnDmg, burnIdx = n, o.Index
		}
	}
	if burnIdx >= 0 && (burnDmg >= w.oppLife || projected+burnDmg >= w.oppLife) {
		c.pending = &lethalTarget{burn: true}
		out := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{burnIdx}}
		if d.Validate(out) == nil {
			return out
		}
		c.pending = nil
	}
	// Pump: the best power boost among offered ordinary casts that target
	// our own creature, priced by how much guaranteed attack it adds. It is
	// only considered when our attack is still ahead this turn (our first
	// main phase); otherwise a pump buys no damage the decorator can count.
	if len(atk) > 0 && w.myTurn && w.firstMain {
		pumpIdx, pumpGain, pumpObj := -1, int32(0), state.ObjID(0)
		for i := range d.Options {
			o := &d.Options[i]
			if o.Kind != "cast" || o.Mode != "" {
				continue
			}
			cd := lookupCard(w, o)
			if cd == nil {
				continue
			}
			n, ok := spellPumpOwn(cd)
			if !ok || n <= 0 {
				continue
			}
			if gain, obj := bestPump(atk, blockers, n); gain > pumpGain {
				pumpGain, pumpIdx, pumpObj = gain, o.Index, obj
			}
		}
		if pumpIdx >= 0 && pumpGain > 0 && projected+pumpGain >= w.oppLife {
			c.pending = &lethalTarget{attacker: pumpObj}
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
// cannot redirect the line. Any other target decision, and a pending line
// whose matching option is absent, is delegated.
func (c *lethalCore) target(w lethalSpace, d *decision.Decision, in decision.Intent) decision.Intent {
	p := c.pending
	c.pending = nil
	if p == nil {
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

// guaranteedDamage is a lower bound on the combat damage a set of attackers
// deals against a worst-case blocking assignment for us. Attackers the
// blockers cannot answer (unblockable, or flying with no flying/reach
// blocker) connect in full. The rest are blocked in power order: a menace
// attacker consumes two blockers, and a trampler that is blocked carries
// only its excess over the blockers' combined toughness. A blocked
// non-trampler deals nothing.
func guaranteedDamage(atk, blockers []lethalCre) int32 {
	bs := append([]lethalCre(nil), blockers...)
	sort.Slice(bs, func(i, j int) bool {
		if bs[i].toughness != bs[j].toughness {
			return bs[i].toughness > bs[j].toughness
		}
		return bs[i].obj < bs[j].obj
	})
	hasFly, hasReach := false, false
	for _, b := range bs {
		hasFly = hasFly || b.flying
		hasReach = hasReach || b.reach
	}
	var ground []lethalCre
	var total int32
	for _, a := range atk {
		p := a.power * ptMultiplier(a)
		if a.unblockable || (a.flying && !hasFly && !hasReach) {
			total += p
			continue
		}
		ground = append(ground, a)
	}
	sort.Slice(ground, func(i, j int) bool {
		pi := ground[i].power * ptMultiplier(ground[i])
		pj := ground[j].power * ptMultiplier(ground[j])
		if pi != pj {
			return pi > pj
		}
		return ground[i].obj < ground[j].obj
	})
	bi := 0
	for _, a := range ground {
		p := a.power * ptMultiplier(a)
		need := 1
		if a.menace {
			need = 2
		}
		if bi+need > len(bs) {
			total += p // not enough blockers left: it gets through
			continue
		}
		var soak int32
		for k := 0; k < need; k++ {
			soak += bs[bi+k].toughness
		}
		bi += need
		if a.trample && p > soak {
			total += p - soak
		}
	}
	return total
}

// ptMultiplier is 2 for a double striker (two damage steps) and 1 otherwise.
func ptMultiplier(c lethalCre) int32 {
	if c.doubleStrk {
		return 2
	}
	return 1
}

// bestPump returns the largest guaranteed-attack gain a +n/+n pump on one of
// atk adds, and the object to aim it at (the first maximum in object order).
func bestPump(atk, blockers []lethalCre, n int32) (int32, state.ObjID) {
	bestGain := int32(0)
	var bestID state.ObjID
	for i := range atk {
		a := atk[i]
		pumped := atk[i]
		pumped.power += n
		gain := guaranteedDamage([]lethalCre{pumped}, blockers) - guaranteedDamage([]lethalCre{a}, blockers)
		if gain > bestGain || (gain == bestGain && gain > 0 && (bestID == 0 || a.obj < bestID)) {
			bestGain, bestID = gain, a.obj
		}
	}
	return bestGain, bestID
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
		for sa := a; sa != nil; sa = sa.Sub {
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
	}
	return 0, false
}

// spellPumpOwn reports a spell's printed literal positive power boost on a
// creature the caster controls. Only an explicit own-creature target
// ("Creature.YouCtrl") is accepted: a bare "Creature" could be aimed at an
// opposing creature the caster cannot supply, and a "Defined$ Self" pump
// gives the decorator no attacker to aim, so both are refused.
func spellPumpOwn(c *cards.Card) (int32, bool) {
	f := firstFace(c)
	if f == nil {
		return 0, false
	}
	for _, a := range f.Abilities {
		if a == nil || a.Kind != "SP" {
			continue
		}
		for sa := a; sa != nil; sa = sa.Sub {
			if sa.API != "Pump" && sa.API != "PumpAll" {
				continue
			}
			v := sa.Params["ValidTgts"]
			if !strings.Contains(v, "YouCtrl") {
				continue
			}
			n, ok := literalInt(sa.Params["NumAtt"])
			if !ok || n <= 0 {
				continue
			}
			return n, true
		}
	}
	return 0, false
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
