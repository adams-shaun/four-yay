package v1agent

import (
	"sort"
	"strings"
)

// Board is the policy-facing model of one decision: the acting seat's view.
// Full when the engine attached x_kernel_v5; otherwise Thin is set and only
// the state_summary counts and candidate references are known.
type Board struct {
	Seat, OppSeat string
	Me, Opp       int
	Turn          int
	Phase         string // kernel phase names: main1, declare_attackers, ...
	MyTurn        bool
	Life          [2]int
	HandCount     [2]int
	LibCount      [2]int
	Mine, Theirs  []*KCard
	MyGrave       []KCard
	TheirGrave    []KCard
	Hand          []KHandCard
	Stack         []KStackItem
	Combat        KCombat
	Selection     *KCombatSelection
	LandsPlayed   int
	// Purpose is the pending effect choice's purpose (card_selection,
	// search_result, library_order, effect_targets, ...), "" when none.
	Purpose string
	Thin    bool
	byArena map[uint32]*KCard
}

// phaseFromSummary maps a v1 phase_step onto the kernel's phase names.
var phaseFromSummary = map[string]string{
	"untap": "untap", "upkeep": "upkeep", "draw": "draw", "precombat_main": "main1",
	"beginning_of_combat": "begin_combat", "declare_attackers": "declare_attackers",
	"declare_blockers": "declare_blockers", "combat_damage": "combat_damage",
	"end_of_combat": "end_combat", "postcombat_main": "main2", "end_step": "end", "cleanup": "cleanup",
}

// NewBoard builds the board for d.
func NewBoard(d *Decision) *Board {
	b := &Board{Seat: d.ActingSeat, OppSeat: d.Opponent(), byArena: map[uint32]*KCard{}}
	b.Me, b.Opp = seatIndex(b.Seat), seatIndex(b.OppSeat)
	if d.Kernel == nil {
		b.Thin = true
		b.Turn = d.Summary.Turn
		b.Phase = phaseFromSummary[d.Summary.PhaseStep]
		b.MyTurn = d.Summary.ActiveSeat == b.Seat
		for i, seat := range []string{"p0", "p1"} {
			s := d.Summary.Seat(seat)
			b.Life[i], b.HandCount[i], b.LibCount[i] = s.Life, s.HandCount, s.LibraryCount
		}
		return b
	}
	p := &d.Kernel.Obs.Projection
	b.Turn, b.Phase = p.Turn, p.Phase
	b.MyTurn = p.ActivePlayer == b.Seat
	b.Life, b.HandCount, b.LibCount = p.Life, p.HandCounts, p.LibraryCounts
	for i := range p.Battlefield[b.Me] {
		c := &p.Battlefield[b.Me][i]
		b.Mine = append(b.Mine, c)
		b.byArena[c.Stable.ArenaID] = c
	}
	for i := range p.Battlefield[b.Opp] {
		c := &p.Battlefield[b.Opp][i]
		b.Theirs = append(b.Theirs, c)
		b.byArena[c.Stable.ArenaID] = c
	}
	b.MyGrave, b.TheirGrave = p.Graveyards[b.Me], p.Graveyards[b.Opp]
	b.Hand = d.Kernel.Obs.OwnHand
	b.Stack = p.Stack
	b.Combat = p.Combat
	b.Selection = p.SurfaceContext.Selection
	b.LandsPlayed = p.Status[b.Me].LandsPlayed
	if pe := p.EngineContext.PendingEffect; pe != nil && pe.Choice != nil {
		b.Purpose = pe.Choice.Purpose
	}
	return b
}

// Card returns the battlefield object with arena id a (nil if none).
func (b *Board) Card(a uint32) *KCard { return b.byArena[a] }

// Creatures returns the creatures among cs.
func Creatures(cs []*KCard) []*KCard {
	var out []*KCard
	for _, c := range cs {
		if c.IsCreature() {
			out = append(out, c)
		}
	}
	return out
}

// Lands counts lands among cs.
func Lands(cs []*KCard) int {
	n := 0
	for _, c := range cs {
		if c.IsLand() {
			n++
		}
	}
	return n
}

// UntappedLands counts untapped lands among cs.
func UntappedLands(cs []*KCard) int {
	n := 0
	for _, c := range cs {
		if c.IsLand() && !c.Tapped {
			n++
		}
	}
	return n
}

// Kw is c's effective keywords.
func Kw(c *KCard) *KKeywords { return &c.Characteristics.Keywords }

// CreatureValue scores a creature on the board (roughly "cards worth").
func CreatureValue(c *KCard) float64 {
	if c == nil || !c.IsCreature() {
		return 0
	}
	k := Kw(c)
	p, t := float64(c.Power()), float64(c.Toughness())
	v := 1 + p + t/2
	if k.Flying {
		v += 0.3 + p*0.5
	}
	if k.Deathtouch {
		v += 1.5
	}
	if k.FirstStrike || k.DoubleStrike {
		v += 1
	}
	if k.Lifelink {
		v += p * 0.3
	}
	if k.Trample {
		v += 0.3
	}
	if k.Hexproof {
		v += 0.5
	}
	if k.Indestructible {
		v += 2
	}
	if k.Defender {
		v -= p * 0.8
	}
	if c.IsToken {
		v -= 0.5
	}
	v += hintFor(c.Name).bonus
	return v
}

// CanBlock reports whether blocker may block attacker by evasion keywords.
func CanBlock(blocker, attacker *KCard) bool {
	if blocker.Tapped || !blocker.IsCreature() {
		return false
	}
	ak, bk := Kw(attacker), Kw(blocker)
	if ak.Flying && !(bk.Flying || bk.Reach) {
		return false
	}
	if ak.ProtMonocolored && popcount(blocker.Characteristics.ColorMask) == 1 {
		return false
	}
	if hintFor(blocker.Name).cantBlock {
		return false
	}
	return true
}

func popcount(x uint8) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}

// strikeDamage is the damage a deals to a creature in one fight.
func strikeDamage(a *KCard) int {
	p := a.Power()
	if p < 0 {
		return 0
	}
	if Kw(a).DoubleStrike {
		return 2 * p
	}
	return p
}

// Kills reports whether a's combat damage destroys d.
func Kills(a, d *KCard) bool {
	if Kw(d).Indestructible {
		return false
	}
	dmg := strikeDamage(a)
	if dmg <= 0 {
		return false
	}
	return Kw(a).Deathtouch || dmg >= d.Remaining()
}

// Fight resolves a one-on-one combat: whether a dies and whether b dies,
// with first strike.
func Fight(a, b *KCard) (aDies, bDies bool) {
	aFS := Kw(a).FirstStrike || Kw(a).DoubleStrike
	bFS := Kw(b).FirstStrike || Kw(b).DoubleStrike
	switch {
	case aFS && !bFS:
		bDies = Kills(a, b)
		aDies = !bDies && Kills(b, a)
	case bFS && !aFS:
		aDies = Kills(b, a)
		bDies = !aDies && Kills(a, b)
	default:
		aDies, bDies = Kills(b, a), Kills(a, b)
	}
	return
}

// normName folds the few non-ASCII letters pool names carry, so kernel
// spellings ("Lorien Revealed") and catalog spellings ("Lórien Revealed")
// meet.
func normName(n string) string {
	r := strings.NewReplacer("ó", "o", "û", "u", "Ó", "O", "é", "e", "á", "a", "í", "i", "ú", "u")
	return r.Replace(n)
}

var normFacts = func() map[string]*CardFact {
	m := map[string]*CardFact{}
	for n, f := range cardFacts {
		m[normName(n)] = f
	}
	return m
}()

// FactN looks a card up by folded name.
func FactN(name string) *CardFact { return normFacts[normName(name)] }

// sortByValueDesc orders creatures by CreatureValue, highest first
// (stable on arena id for determinism).
func sortByValueDesc(cs []*KCard) {
	sort.SliceStable(cs, func(i, j int) bool {
		vi, vj := CreatureValue(cs[i]), CreatureValue(cs[j])
		if vi != vj {
			return vi > vj
		}
		return cs[i].Stable.ArenaID < cs[j].Stable.ArenaID
	})
}
