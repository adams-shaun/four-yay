package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The Q3b part bound (design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md, §6 Q3b):
// the proof-side half. The facts side (rules/quiet_facts.go) stores each
// admitting ability's bounded non-mana cost parts on the face; this file
// evaluates their NECESSARY conditions against live state. Out of scope by
// design: exact satisfiability. A part check answers "could this part be
// paid" over-approximately -- every check counts a superset of the real
// candidates, so an affordable claim is safe and only an unaffordable claim
// needs to be exact (and each one mirrors the offer gate's own reading).
//
// Zero allocation, no maps: every read is a fixed-array walk or a zone scan.

// quietPartsPayable reports whether every bounded part of one ability could
// be paid. The parts are evaluated independently, the way the offer gate's
// per-part checks are: over-counting a shared pool across parts can only
// claim payable, never unpayable, so the conjunction stays sound.
func (e *Engine) quietPartsPayable(p state.PlayerID, src *state.Object, g *quietBoundedAbility) bool {
	for i := uint8(0); i < g.nParts; i++ {
		if !e.quietPartPayable(p, src, &g.parts[i]) {
			return false
		}
	}
	return true
}

// quietPartPayable evaluates one part's necessary condition. Each arm
// mirrors the offer gate the walk runs for the same shape (pay's
// CostPayable family), so a "could not be paid" answer is as exact as that
// gate: when the proof says unpayable, the gate said unpayable too, and the
// walk offered nothing for the ability.
func (e *Engine) quietPartPayable(p state.PlayerID, src *state.Object, part *quietPart) bool {
	switch part.kind {
	case pqLife:
		// ResolveManaWith refuses a Life charge above the payer's life
		// total (rules/pay/solve.go), so this is the gate's own condition.
		return e.G.Players[p].Life >= part.n
	case pqSubCounter:
		// The gate's own read: pay.SubCounterAvailable counts the part's
		// kind on the source ("Any" = the whole counter count). The source
		// IS the object the ability is summarised from, whatever zone it
		// sits in -- the same read NonManaCastableP makes.
		return pay.SubCounterAvailable(src, part.kindStr) >= part.n
	case pqSac:
		if part.self {
			// SacrificeCostCandidates' CARDNAME arm: exactly the source,
			// only while the payer controls it as an existing battlefield
			// permanent.
			return part.n <= 1 && src.Zone == state.ZBattlefield &&
				existsOnBattlefieldQuiet(src) && src.Controller == p
		}
		return e.quietCountZone(p, state.ZBattlefield, part, src, false) >= part.n
	case pqDiscard:
		if part.self {
			// DiscardCandidates' CARDNAME/NICKNAME arm: the source may
			// remain in hand and pay itself (channel, bloodrush).
			return part.n <= 1 && src.Zone == state.ZHand && src.Owner == p
		}
		return e.quietCountZone(p, state.ZHand, part, src, false) >= part.n
	case pqExileGrave:
		if part.self {
			// ExileCostCandidates over the graveyard: the source is the
			// only CARDNAME candidate, and only while it sits in the
			// payer's graveyard.
			return part.n <= 1 && src.Zone == state.ZGraveyard && src.Owner == p
		}
		return e.quietCountZone(p, state.ZGraveyard, part, src, false) >= part.n
	case pqExileHand:
		if part.self {
			return part.n <= 1 && src.Zone == state.ZHand && src.Owner == p
		}
		return e.quietCountZone(p, state.ZHand, part, src, false) >= part.n
	case pqTapPermanent:
		if part.self {
			// TapCostCandidates' spec arm over the battlefield, untapped
			// filter: exactly the source when it is a CARDNAME candidate.
			return part.n <= 1 && src.Zone == state.ZBattlefield &&
				existsOnBattlefieldQuiet(src) && src.Controller == p && !src.Tapped
		}
		return e.quietCountZone(p, state.ZBattlefield, part, src, true) >= part.n
	}
	return false
}

// quietCountZone counts the payer's objects in zone that could pay part:
// an over-approximation of MatchesSpecFrom's answer, so the count is never
// below the gate's candidate count and an unaffordable claim is exact. The
// source is skipped only when the part's spec itself excludes it (".Other").
func (e *Engine) quietCountZone(p state.PlayerID, zone state.Zone, part *quietPart, src *state.Object, untappedOnly bool) int32 {
	var n int32
	for _, id := range e.G.Zone(zone, p) {
		o := e.G.Obj(id)
		if o == nil {
			continue
		}
		if zone == state.ZBattlefield && !existsOnBattlefieldQuiet(o) {
			// A phased-out permanent is treated as though it does not
			// exist (CR 702.25b) and cannot be chosen as a cost.
			continue
		}
		if untappedOnly && o.Tapped {
			continue
		}
		if part.exclSelf && src != nil && id == src.ID {
			continue
		}
		if !quietObjectMatchesMask(o, zone, part) {
			continue
		}
		n++
	}
	return n
}

// quietObjectMatchesMask is quietSpecMask's match test against one object.
// A battlefield permanent's characteristics are its current face's (CR
// 712.2), so only o.Face() is read there. A card in a hidden zone (hand,
// graveyard) is tested against ANY face, which over-counts the double-faced
// shapes and stays sound.
func quietObjectMatchesMask(o *state.Object, zone state.Zone, part *quietPart) bool {
	if part.incl == 0 && part.excl == 0 {
		return true
	}
	if zone == state.ZBattlefield {
		return quietFaceMatchesMask(o.Face(), part)
	}
	card := o.Card
	if card == nil {
		return true
	}
	for _, f := range card.Faces {
		if quietFaceMatchesMask(f, part) {
			return true
		}
	}
	return false
}

// quietFaceMatchesMask applies one part's incl/excl masks to one face. A
// face with no catalog mask (an unbound test fixture) is assumed to match,
// the over-approximating direction.
func quietFaceMatchesMask(f *cards.Face, part *quietPart) bool {
	if f == nil {
		return true
	}
	fm := f.TypeMaskOf()
	if fm == 0 {
		return true
	}
	if part.incl != 0 && fm&part.incl == 0 {
		return false
	}
	if part.excl != 0 && fm&part.excl != 0 {
		return false
	}
	return true
}
