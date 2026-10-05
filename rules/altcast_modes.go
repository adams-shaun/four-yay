package rules

// altcast_modes.go is the ONE descriptor of the alternative-cost keyword
// family: the cast modes whose cost is a printed keyword's parameter paid in
// place of the mana cost (CR 118.9). Every rules site that used to name one
// of these modes by its own string -- the cast-mode table, modeFlags, the
// potential-plan base cost, the hand and command-zone offer loops, the
// cost-static scope predicates and the per-mode pending-cast checks -- reads
// this table instead, so a new keyword of the family is one row here.
// TestAltCastModeLiteralsLiveInTheDescriptor (altcast_modes_test.go) freezes
// the string literals naming a row's mode anywhere else in rules/.
//
// The entry riders (evoke's sacrifice, dash's return, blitz's draw and
// sacrifice, warp's exile, impending's counters) stay in altcast.go's
// altCostEnter: they key on the pay-time CastFlags bit each row names, never
// on the mode string.

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// altCastID indexes altCastModes.
type altCastID uint8

const (
	altEvoke altCastID = iota
	altDash
	altOverload
	altWarp
	altImpending
	altMadness
	altBestow
	altBlitz
	altCastCount
)

// altCastCostKind says how a row's cost is read.
type altCastCostKind uint8

const (
	// altCostKeyword: keywordAltCost(face, head) -- the printed parameter
	// (Impending's two-field N:cost form included).
	altCostKeyword altCastCostKind = iota
	// altCostBestow: bestowCost, which cuts the colon metadata suffix and
	// withholds an unpriceable token.
	altCostBestow
	// altCostBlitzInstance: the ID-aware e.blitzCosts list (printed Blitz
	// plus layer-6 grants), matched by the selected offer mode.
	altCostBlitzInstance
)

// altCastMode is one row of the family.
type altCastMode struct {
	// id is the row's index, so a reader with only a *altCastMode (altCastFor,
	// the offer-loop rows) can address its sidecar entry in walkFaceFacts.
	id altCastID
	// mode is the decision.Option.Mode word the offer carries and beginCast
	// reads (the past participle the wire has always used).
	mode string
	// head is the printed K: keyword whose parameter is the cost.
	head string
	// flag is the pay-time CastFlags bit modeFlags records (0: none --
	// madness's provenance rides the discard trigger, not a flag).
	flag uint64
	cost altCastCostKind
	// ph gates the hand walk's offer loop; handLoop/cmdLoop name the rows
	// that loop offers, in table order.
	ph       printedHeads
	handLoop bool
	cmdLoop  bool
	// untargeted rows are offered without the plain spell's target check
	// (overload's "each" replaces "target", CR 702.96b).
	untargeted bool
	// potential rows are priced by potentialModeBaseCost.
	potential bool
}

// altCastModes is the family, indexed by altCastID. Row order is the order
// the hand and command-zone loops offer the modes, so it is part of the
// replayed option list: do not reorder rows.
var altCastModes = [altCastCount]altCastMode{
	altEvoke:     {id: altEvoke, mode: "evoked", head: "Evoke", flag: state.FlagEvoked, ph: phEvoke, handLoop: true, cmdLoop: true, potential: true},
	altDash:      {id: altDash, mode: "dashed", head: "Dash", flag: state.FlagDashed, ph: phDash, handLoop: true, cmdLoop: true, potential: true},
	altOverload:  {id: altOverload, mode: "overloaded", head: "Overload", flag: state.FlagOverloaded, ph: phOverload, handLoop: true, cmdLoop: true, untargeted: true, potential: true},
	altWarp:      {id: altWarp, mode: "warped", head: "Warp", flag: state.FlagWarped, ph: phWarp, handLoop: true, potential: true},
	altImpending: {id: altImpending, mode: "impended", head: "Impending", flag: state.FlagImpending, ph: phImpending, handLoop: true, cmdLoop: true, potential: true},
	altMadness:   {id: altMadness, mode: "madness", head: "Madness", potential: true},
	altBestow:    {id: altBestow, mode: "bestowed", head: "Bestow", flag: state.FlagBestowed, cost: altCostBestow, potential: true},
	altBlitz:     {id: altBlitz, mode: "blitzed", head: "Blitz", flag: state.FlagBlitzed, cost: altCostBlitzInstance},
}

// altMode is row id's mode word.
func altMode(id altCastID) string { return altCastModes[id].mode }

// altCastFor is the row whose mode is mode, or nil. A Blitz grant instance
// ("blitzed_grant_N") is canonicalised to "blitzed" by beginCast before any
// reader here sees it.
func altCastFor(mode string) *altCastMode {
	for i := range altCastModes {
		if altCastModes[i].mode == mode {
			return &altCastModes[i]
		}
	}
	return nil
}

// altCastIs reports whether mode is row id's mode.
func altCastIs(mode string, id altCastID) bool { return mode == altCastModes[id].mode }

// altFaceCost is one row's compiled printed cost on a face: walkFaceFacts'
// sidecar entry. ok is faceCostRaw's ok at compile time.
type altFaceCost struct {
	cost Cost
	ok   bool
}

// faceCostRaw is the row's printed cost on f, parsed; ok is false when the
// face does not carry the keyword (or carries an unpriceable bestow). A
// Blitz row's grants are ID-aware: beginCast and the walks read e.blitzCosts
// for it. This is the per-face compile's source (computeWalkFaceFacts) and
// faceCost's fallback for a face with no current sidecar.
func (m *altCastMode) faceCostRaw(f *cards.Face) (Cost, bool) {
	if m.cost == altCostBestow {
		return bestowCost(f)
	}
	return keywordAltCost(f, m.head)
}

// faceCost is faceCostRaw read from the sidecar ff (walkFaceFacts) when its
// keyword half is current for f, and parsed fresh otherwise. The parameter
// is a pure function of the face, so the offer walk, the command-zone offer,
// the potential-plan pricing and the charge all share ONE compiled parse per
// face instead of reparsing on every offer.
func (m *altCastMode) faceCost(f *cards.Face, ff *walkFaceFacts) (Cost, bool) {
	if ff != nil && ff.keywordsCurrent(f) {
		c := ff.altCosts[m.id]
		return c.cost, c.ok
	}
	return m.faceCostRaw(f)
}

// altCastModeEntries are castModeCodes' rows for the family, all sharing the
// alternative-cost keyword arm.
func altCastModeEntries() []state.StrEntry[castModeCode] {
	out := make([]state.StrEntry[castModeCode], 0, len(altCastModes))
	for i := range altCastModes {
		out = append(out, state.StrEntry[castModeCode]{Key: altCastModes[i].mode, Val: castModeAltCostKeyword})
	}
	return out
}
