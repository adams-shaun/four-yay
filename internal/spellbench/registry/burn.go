// Package registry — burn is the race-aware burn-targeting decorator.
//
// burn rewrites the answer to a single-pick target decision whose effect the
// decision payload already describes as DealDamage with a statically known
// amount (decision.TargetEffect.Damage.Amount, published by the engine at
// the ask). Everything else is delegated to the wrapped seat unchanged.
//
// The policy, from the brief:
//
//   - Race clock. Each side's turns-to-kill is the defender's life divided
//     by the attacker's board power that can connect. A creature connects
//     when the defender cannot stop it: a flyer (or shadow creature) gets
//     through unless the defender keeps an untapped blocker that can block
//     it (Flying or Reach), a menace creature gets through unless two
//     untapped blockers remain, and a ground creature gets through only
//     when no untapped blocker remains at all. Attack-capable means untapped
//     with power > 0.
//   - Kill a creature when the damage kills it AND (its removal moves a
//     clock by at least one turn, OR it is its controller's best creature
//     by power times an evasion multiplier) AND the damage is not already
//     lethal to the face.
//   - Otherwise target the face when the damage is lethal, or leaves the
//     opponent within reach of our next attack, or we are ahead in the race.
//   - Never target our own creatures or ourselves with harmful damage.
//   - Delegate when the amount is not statically known, the payload is
//     absent or names another API, the ask is not a single pick, or no
//     modelled candidate is offered.
//
// The decision's TargetEffect payload is engine-published at the ask
// (rules/stack.go describeTargetEffect), so the decorator reads the card
// IR through it without importing the engine: exactly the information a
// seat is handed on the wire.
//
// Controller identity. The asked player (Decision.Player) is the effect's
// controller for every ordinary target ask. On the view surface the
// decorator additionally verifies the source object where the view can see
// it (a stack object's controller, the viewer's own hand card, a
// battlefield permanent) and delegates when it provably is not ours. The
// board surface carries no stack, so there the asked player is taken as the
// controller -- the one shape this cannot see is a TargetingPlayer$-
// redirected ask, which the gauntlet decks do not carry.
//
// The two surfaces (view.View and botpolicy.Board) are filled with the same
// public facts, so the clocks are computed from exactly the board each
// surface hands over; a summoning-sick creature is counted as an attacker
// because the Board surface carries no summon-sick flag -- the clocks are a
// heuristic, not a promise, and both halves read the same facts.

package registry

import (
	"context"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func init() {
	RegisterDecorator("burn", newBurn)
}

// newBurn wraps inner. The wrapper keeps inner's BoardSeat-ness (the
// passguard contract): the engine hands a BoardSeat inner a botpolicy.Board
// and a plain inner a projected View, and the wrapper never changes which
// surface inner sees.
func newBurn(inner seat.Seat, seed uint64) seat.Seat {
	base := &burnSeat{inner: inner, rng: builtins.NewSplitMix64(seed)}
	if _, ok := inner.(seat.BoardSeat); ok {
		return &burnBoard{base}
	}
	return base
}

// burnSeat is the wrapper over a non-BoardSeat inner.
type burnSeat struct {
	inner seat.Seat
	// rng is the decorator's own stream, seeded from the seat's per-game
	// seed; it breaks exact ties between equally scored picks.
	rng builtins.SplitMix64
}

// UnwrapSeat exposes the wrapped seat (the registry's Unwrapper contract).
func (s *burnSeat) UnwrapSeat() seat.Seat { return s.inner }

// WantsPaymentActions delegates the payment-plan opt-in to inner.
func (s *burnSeat) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

func (s *burnSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return in, err
	}
	f, ours := burnFactsFromView(v, d)
	if idx, ok := burnGuard(f, ours, d, &s.rng); ok {
		in.Choices = []int{idx}
		in.Rest = nil
	}
	return in, nil
}

// burnBoard is the wrapper over a BoardSeat inner.
type burnBoard struct {
	*burnSeat
}

func (s *burnBoard) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.(seat.BoardSeat).DecideBoard(ctx, b, d)
	if err != nil {
		return in, err
	}
	if idx, ok := burnGuard(burnFactsFromBoard(b, d), true, d, &s.rng); ok {
		in.Choices = []int{idx}
		in.Rest = nil
	}
	return in, nil
}

// burnGuard applies the one documented replacement to a target decision;
// ok reports whether the inner's answer should be replaced with the option
// index idx. factsOK false (the view proved the effect is not ours) never
// replaces.
func burnGuard(f burnFacts, factsOK bool, d decision.Decision, rng *builtins.SplitMix64) (int, bool) {
	dmg, ok := burnModelledAsk(d)
	if !ok || !factsOK {
		return 0, false
	}
	me := d.Player
	// Classify the offered options into the faces and creatures the policy
	// models. Candidates the policy may score are OPPONENT-side only: our own
	// face and our own creatures are never picked (harmful damage, the
	// brief's Never rule), and this single gate at classification is what
	// keeps them out of every scoring class below -- own creatures can never
	// enter `cre`, so `me` can never enter oppOrder and the per-opponent loop
	// never scores our own board. An option whose object is not a creature
	// on some battlefield (a planeswalker, a non-permanent) is unmodelled
	// and skipped.
	face := map[state.PlayerID]int{}  // opponent seat -> option index
	cre := map[int]state.ObjID{}      // option index -> opponent creature id
	opps := map[state.PlayerID]bool{} // seats named by a modelled candidate
	var oppOrder []state.PlayerID     // ascending, deterministic
	addOpp := func(q state.PlayerID) {
		if !opps[q] {
			opps[q] = true
			oppOrder = append(oppOrder, q)
		}
	}
	for _, o := range d.Options {
		switch o.Kind {
		case "player":
			if o.Player == me {
				continue
			}
			if _, seen := face[o.Player]; !seen {
				face[o.Player] = o.Index
				addOpp(o.Player)
			}
		case "permanent":
			c, ok := f.cre[o.Obj]
			if !ok || c.ctl == me {
				continue
			}
			cre[o.Index] = o.Obj
			addOpp(c.ctl)
		}
	}
	if len(face) == 0 && len(cre) == 0 {
		return 0, false
	}
	sort.Slice(oppOrder, func(i, j int) bool { return oppOrder[i] < oppOrder[j] })

	// Score every modelled action and keep the best class; ties break on
	// score, then on the seeded stream, then on option index.
	type action struct {
		cls   int // 0 lethal face, 1 clock-moving kill, 2 best-creature kill, 3 face within reach, 4 face while ahead
		idx   int
		score int64
	}
	var best []action
	bestCls := 5
	consider := func(a action) {
		if a.cls < bestCls {
			bestCls, best = a.cls, []action{a}
			return
		}
		if a.cls > bestCls {
			return
		}
		if len(best) == 0 || a.score > best[0].score {
			best = []action{a}
			return
		}
		if a.score == best[0].score {
			best = append(best, a)
		}
	}
	for _, q := range oppOrder {
		myLife, oppLife := int64(f.life[me]), int64(f.life[q])
		conn := f.connect(me, q, 0)
		theirConn := f.connect(q, me, 0)
		ttkUs, ttkThem := burnTtk(oppLife, conn), burnTtk(myLife, theirConn)
		// 0: the damage is lethal to the face.
		if fi, ok := face[q]; ok && oppLife <= int64(dmg) {
			consider(action{cls: 0, idx: fi, score: 0})
		}
		// 1 and 2: the damage kills one of their creatures. The walk follows
		// the offered option order, never a map range, so the tie-break pool
		// is built deterministically.
		for _, o := range d.Options {
			obj, ok := cre[o.Index]
			if !ok || o.Kind != "permanent" {
				continue
			}
			c := f.cre[obj]
			if c.ctl != q {
				continue
			}
			rem := c.toughness - c.dmg
			if rem < 0 {
				rem = 0
			}
			if int64(rem) > int64(dmg) || oppLife <= int64(dmg) {
				continue // not killed, or the damage is needed for lethal
			}
			afterUs := burnTtk(oppLife, f.connect(me, q, obj))
			afterThem := burnTtk(myLife, f.connect(q, me, obj))
			imp := ttkUs - afterUs
			if d := afterThem - ttkThem; d > imp {
				imp = d
			}
			if imp >= 1 {
				consider(action{cls: 1, idx: o.Index, score: imp})
			}
			if bs := f.bestCreatureScore(q); bs > 0 && bs == c.evScore() {
				consider(action{cls: 2, idx: o.Index, score: bs})
			}
		}
		// 3: the damage leaves them within reach of our next attack.
		if fi, ok := face[q]; ok && conn > 0 && oppLife > int64(dmg) && oppLife-int64(dmg) <= conn {
			consider(action{cls: 3, idx: fi, score: conn - (oppLife - int64(dmg))})
		}
		// 4: we are ahead in the race.
		if fi, ok := face[q]; ok && ttkUs < ttkThem {
			consider(action{cls: 4, idx: fi, score: ttkThem - ttkUs})
		}
	}
	if len(best) == 0 {
		return 0, false
	}
	pick := best[0]
	if len(best) > 1 {
		pick = best[rng.Index(len(best))]
	}
	if !optionOffered(d, pick.idx) {
		return 0, false // cannot happen by construction; delegate rather than guess
	}
	return pick.idx, true
}

// burnModelledAsk reports the statically known damage a target decision
// would deal, when the ask is exactly the shape the policy models: one pick
// over a DealDamage effect with a published positive amount.
func burnModelledAsk(d decision.Decision) (int32, bool) {
	if d.Kind != decision.KTarget || d.Max != 1 || d.TargetEffect == nil {
		return 0, false
	}
	te := d.TargetEffect
	if te.API != "DealDamage" || te.Damage == nil || te.Damage.Amount == nil {
		return 0, false
	}
	if n := *te.Damage.Amount; n > 0 {
		return int32(n), true
	}
	return 0, false
}

// optionOffered reports whether idx names an option of d.
func optionOffered(d decision.Decision, idx int) bool {
	for _, o := range d.Options {
		if o.Index == idx {
			return true
		}
	}
	return false
}

// burnFacts is the seat-visible board snapshot the policy reads: every
// player's life total and every battlefield creature's public combat facts.
// Both seat surfaces fill exactly these facts, so the two halves of the
// wrapper reason over the same board.
type burnFacts struct {
	life map[state.PlayerID]int32
	cre  map[state.ObjID]burnCre
}

// burnCre is one battlefield creature's public combat facts.
type burnCre struct {
	ctl                           state.PlayerID
	power, toughness, dmg         int32
	tapped                        bool
	flying, reach, menace, shadow bool
}

// evScore is the creature's "best creature" score: power times an evasion
// multiplier (evasive creatures are twice as good at closing a race).
func (c burnCre) evScore() int64 {
	m := int64(1)
	if c.flying || c.shadow || c.menace {
		m = 2
	}
	return int64(c.power) * m
}

// burnFactsFromView lifts the snapshot off a projected View. The viewer is
// the asked player (the engine hands each seat its own view), so the
// source-ours check below reads only facts that seat legally sees. The
// second return is false when the view proves the effect is NOT ours —
// the decorator then delegates.
func burnFactsFromView(v view.View, d decision.Decision) (burnFacts, bool) {
	f := burnFacts{life: map[state.PlayerID]int32{}, cre: map[state.ObjID]burnCre{}}
	for _, p := range v.Players {
		f.life[p.ID] = p.Life
		for i := range p.Battlefield {
			cv := &p.Battlefield[i]
			if cv.FaceDown || !strings.Contains(cv.Types, "Creature") {
				continue
			}
			f.cre[cv.ID] = burnCre{
				ctl: cv.Controller, power: cv.Power, toughness: cv.Toughness,
				dmg: cv.Damage, tapped: cv.Tapped,
				flying: kwHas(cv.Keywords, "Flying"), reach: kwHas(cv.Keywords, "Reach"),
				menace: kwHas(cv.Keywords, "Menace"), shadow: kwHas(cv.Keywords, "Shadow"),
			}
		}
	}
	// The source must provably be ours where the view can say so: a stack
	// object's controller, the viewer's own hand card (only the viewer's
	// hand is ever in its view), or a battlefield permanent. A source the
	// view cannot locate (an ability object not minted yet, Source 0; the
	// asked player's own stack object in an unseen snapshot) falls back to
	// the asked player, who is the effect's controller for every ordinary
	// ask.
	if src := d.Source; src != 0 {
		for _, sv := range v.Stack {
			if sv.ID == src && sv.Controller != d.Player {
				return burnFacts{}, false
			}
		}
		for _, p := range v.Players {
			for i := range p.Battlefield {
				if bv := &p.Battlefield[i]; bv.ID == src && bv.Controller != d.Player {
					return burnFacts{}, false
				}
			}
			if p.ID != d.Player {
				// A hand card in somebody else's view is never projected, so
				// a Hand list on another seat's slice would be a view bug;
				// only the viewer's own hand can legitimately name the cast
				// being aimed.
				continue
			}
			for i := range p.Hand {
				if p.Hand[i].ID == src {
					// The cast-time ask's source is the caster's own card.
					return f, true
				}
			}
		}
	}
	return f, true
}

// burnFactsFromBoard lifts the snapshot off a botpolicy.Board. The board
// carries no stack, so the asked player is taken as the effect's controller
// (see the package comment's controller-identity note).
func burnFactsFromBoard(b botpolicy.Board, d decision.Decision) burnFacts {
	f := burnFacts{life: map[state.PlayerID]int32{}, cre: map[state.ObjID]burnCre{}}
	for id, life := range b.Life.All() {
		f.life[id] = life
	}
	for id, c := range b.Creatures.All() {
		f.cre[id] = burnCre{
			ctl: c.Controller, power: c.Power, toughness: c.Toughness,
			dmg: c.Damage, tapped: c.Tapped,
			flying: kwHas(c.Keywords, "Flying"), reach: kwHas(c.Keywords, "Reach"),
			menace: kwHas(c.Keywords, "Menace"), shadow: kwHas(c.Keywords, "Shadow"),
		}
	}
	_ = d
	return f
}

// kwHas reports whether the keyword list carries the named head.
func kwHas(kws []string, head string) bool {
	for _, k := range kws {
		if cards.KeywordHead(k) == head {
			return true
		}
	}
	return false
}

// burnInfTtk stands in for "this side cannot kill the other": a sentinel
// larger than any real turn count, so sentinel-vs-finite comparisons behave
// like infinity.
const burnInfTtk = int64(1) << 30

// burnTtk converts a life total and connectable damage per attack into
// turns to kill.
func burnTtk(life, conn int64) int64 {
	if life <= 0 {
		return 0
	}
	if conn <= 0 {
		return burnInfTtk
	}
	return (life + conn - 1) / conn
}

// bestCreatureScore is the highest evasion-weighted power among a seat's
// creatures (0 when it has none).
func (f burnFacts) bestCreatureScore(q state.PlayerID) int64 {
	best := int64(0)
	for _, c := range f.cre {
		if c.ctl != q {
			continue
		}
		if s := c.evScore(); s > best {
			best = s
		}
	}
	return best
}

// connect estimates how much damage from's attack-capable creatures push
// through to's untapped blockers this turn, excluding one object (the
// creature a kill would remove, from either side's census). Attack-capable:
// untapped, power > 0. A flyer or shadow creature connects unless a
// Flying-or-Reach blocker remains, and each blocker that stays home stops
// the strongest flyer; a menace creature connects unless two blockers
// remain; a ground creature connects only when no blocker remains.
func (f burnFacts) connect(from, to state.PlayerID, exclude state.ObjID) int64 {
	type unit struct {
		id state.ObjID
		c  burnCre
	}
	var atks []unit
	for id, c := range f.cre {
		if c.ctl != from || id == exclude || c.tapped || c.power <= 0 {
			continue
		}
		atks = append(atks, unit{id, c})
	}
	sort.Slice(atks, func(i, j int) bool {
		if atks[i].c.power != atks[j].c.power {
			return atks[i].c.power > atks[j].c.power
		}
		return atks[i].id < atks[j].id
	})
	// Blockers: strongest first, so "the strongest flyer gets blocked" and
	// "menace spends the two weakest" are both deterministic.
	var blockers []unit
	for id, c := range f.cre {
		if c.ctl == to && id != exclude && !c.tapped {
			blockers = append(blockers, unit{id, c})
		}
	}
	sort.Slice(blockers, func(i, j int) bool {
		if blockers[i].c.toughness != blockers[j].c.toughness {
			return blockers[i].c.toughness > blockers[j].c.toughness
		}
		return blockers[i].id < blockers[j].id
	})
	dropBlocker := func(i int) {
		blockers = append(blockers[:i], blockers[i+1:]...)
	}
	conn := int64(0)
	for _, a := range atks {
		switch {
		case a.c.flying || a.c.shadow:
			blocked := false
			for i, b := range blockers {
				if b.c.flying || b.c.reach {
					dropBlocker(i)
					blocked = true
					break
				}
			}
			if !blocked {
				conn += int64(a.c.power)
			}
		case a.c.menace:
			if len(blockers) >= 2 {
				dropBlocker(len(blockers) - 1) // the weakest
				dropBlocker(len(blockers) - 1)
			} else {
				conn += int64(a.c.power)
			}
		default:
			if len(blockers) == 0 {
				conn += int64(a.c.power)
			}
		}
	}
	return conn
}
