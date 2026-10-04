package pay

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// parts.go holds pure reads the unless-cost and announced-cost payment
// windows share (lasagna spec W5 E7): which part of an UnlessCost$ a window
// row pays, the owed remainder of an announced payment, the pool counter a
// mana unit spends from, and the bipartite matching an any-of payment needs.

// MaxBipartiteMatch returns the maximum matching size between left slots and
// the distinct right-side candidates, in deterministic slot/candidate order.
func MaxBipartiteMatch(cands [][]state.ObjID) int {
	matchOf := map[state.ObjID]int{} // candidate -> slot
	var try func(slot int, seen map[state.ObjID]bool) bool
	try = func(slot int, seen map[state.ObjID]bool) bool {
		for _, c := range cands[slot] {
			if seen[c] {
				continue
			}
			seen[c] = true
			if prev, ok := matchOf[c]; !ok || try(prev, seen) {
				matchOf[c] = slot
				return true
			}
		}
		return false
	}
	n := 0
	for i := range cands {
		if try(i, map[state.ObjID]bool{}) {
			n++
		}
	}
	return n
}

// UnlessPartAt returns the cost component at flat index i across the
// unlessPayment's Sac, Discard, Reveal, Behold, Return and Exile lists,
// together with the zone its candidates come from and the option kind the wire
// carries. The
// option kind "revealcost" is the cast flow's own reveal-cost string
// (rules/cast.go), and "exilecost" its exile-cost string, so a client sees
// the same vocabulary for both paths.
func UnlessPartAt(cost Cost, i int) (cost.CostPart, state.Zone, string) {
	nSac, nDisc, nRev, nBeh := len(cost.Sac), len(cost.Discard), len(cost.Reveal), len(cost.Behold)
	nRet := len(cost.Return)
	switch {
	case i < nSac:
		return cost.Sac[i], state.ZBattlefield, "sacrifice"
	case i < nSac+nDisc:
		return cost.Discard[i-nSac], state.ZHand, "discard"
	case i < nSac+nDisc+nRev:
		return cost.Reveal[i-nSac-nDisc], state.ZHand, "revealcost"
	case i < nSac+nDisc+nRev+nBeh:
		// The zone field is the primary (battlefield) scan; the candidate
		// enumeration for a beholdcost part deliberately scans BOTH the
		// battlefield and the hand (CR 702.176).
		return cost.Behold[i-nSac-nDisc-nRev], state.ZBattlefield, "beholdcost"
	case i < nSac+nDisc+nRev+nBeh+nRet:
		return cost.Return[i-nSac-nDisc-nRev-nBeh], state.ZBattlefield, "returncost"
	default:
		part := cost.Exile[i-nSac-nDisc-nRev-nBeh-nRet]
		return part, UnlessExileZone(part), "exilecost"
	}
}

// UnlessExileZone is the zone an Exile cost part draws from. cost.CostPart.Zone's
// zero value means the hand (the same reading the cast-cost parser's
// FromHand arm leaves behind), so a FromGrave/AnyGrave part carries
// state.ZGraveyard and a FromHand part reads as state.ZHand.
func UnlessExileZone(part cost.CostPart) state.Zone {
	if part.Zone == 0 {
		return state.ZHand
	}
	return part.Zone
}

// PaymentOwed is what the floating pool does not yet cover of cost, for the
// window readout (spec §4.1): each coloured and {C} pip is covered only by
// its own pool slot, generic by whatever remains. It is exact for the V1
// cost shapes an announce admits.
func PaymentOwed(c Cost, pool state.Mana) decision.PaymentCost {
	var owed decision.PaymentCost
	left := pool
	for i := range c.Colored {
		need := c.Colored[i]
		if need <= 0 {
			continue
		}
		use := min(need, max(left[i], 0))
		left[i] -= use
		owed.Mana[i] = uint32(need - use)
	}
	gen := c.Generic
	for i := range left {
		if gen <= 0 {
			break
		}
		take := min(gen, max(left[i], 0))
		gen -= take
	}
	if gen > 0 {
		owed.Generic = uint32(gen)
	}
	return owed
}

// PlainOrSnowManaCounter admits a bare WUBRGC counter (or the empty default)
// and a snow "S<colour>" counter; a typed-producer counter is not reversed.
func PlainOrSnowManaCounter(c string) bool {
	switch len(c) {
	case 0:
		return true
	case 1:
		return strings.Contains("WUBRGC", c)
	case 2:
		return c[0] == 'S' && strings.ContainsRune("WUBRGC", rune(c[1]))
	}
	return false
}

// ManaCounterSlot is the pool slot of a plain or snow ManaAdd counter.
func ManaCounterSlot(c string) int {
	switch len(c) {
	case 1:
		return state.ManaIndex(c[0])
	case 2:
		return state.ManaIndex(c[1])
	}
	return state.MC
}
