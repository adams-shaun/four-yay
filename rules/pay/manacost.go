package pay

import (
	"github.com/adams-shaun/gorge/cards"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// ManaCostActivation holds a synchronous mana ability while its discard
// cost is chosen. It is separate from pendingCast so activating mana during a
// spell's CR 601.2g payment window never overwrites the outer cast flow.
type ManaCostActivation struct {
	Player     state.PlayerID
	Source     state.ObjID
	Ability    *cards.SA
	Cost       costvocab.Cost
	Sacs       []state.ObjID
	SacPaid    int
	Discards   []state.ObjID
	Exiles     []state.ObjID
	Taps       []state.ObjID
	SacPart    int
	Part       int
	ExilePart  int
	TapPart    int
	Cast       bool
	Cumulative bool
	Gained     GainedManaRef
	// The announced SubCounter cost parts. SubX is the announced X, set
	// once by manaSubCounterAsk's first decision (the cast path's pc.x) and
	// shared by every announced part; subCounterPays records the removal
	// picks exactly like the cast path's pc.subCounterPays (one entry per
	// counter unit for an "Any" part, one entry for a fixed-kind part).
	SubX           int32
	SubXAnnounced  bool
	SubCounterPays []SubCounterPay
	SubPart        int
	// ForageDone marks the Forage election already posed; forageFood is the
	// object sacrificed for it (zero when the exile-three arm paid, or for a
	// non-interactive caller that took the deterministic arm).
	ForageDone bool
	ForageFood state.ObjID
	ForagePay  bool
	// UntapPart walks the untapYType<N/Spec> parts and untaps the elected
	// permanents (the source's own {Q} untap is cost.Untap and stays
	// separate).
	UntapPart int
	Untaps    []state.ObjID
	// Interactive records whether the caller could pose asks. A caller that
	// cannot (the attack-cost tap window, direct-resolve tests) settles the
	// announced SubCounter, Forage and untapYType parts with deterministic
	// R-9 picks instead of posing an election.
	Interactive bool
}

// SubCounterPicked counts the units already recorded for one part index.
func (md *ManaCostActivation) SubCounterPicked(partIdx int) int32 {
	n := int32(0)
	for _, p := range md.SubCounterPays {
		if p.Part == partIdx {
			n++
		}
	}
	return n
}

// SubCounterKindUsed reports whether a wildcard part already removed a unit of
// this kind from this object, so the picker spreads units across the remaining
// kinds exactly as wildcardCounterAsk does.
func (md *ManaCostActivation) SubCounterKindUsed(partIdx int, obj state.ObjID, kind string) bool {
	for _, p := range md.SubCounterPays {
		if p.Part == partIdx && p.Obj == obj && p.Kind == kind {
			return true
		}
	}
	return false
}

// SubCounterPay is one counter removed to pay a SubCounter cost part: the
// part it belongs to, the object the counter comes off, and (for a wildcard
// "Any" part) the chosen counter kind. A fixed-kind filtered part records a
// single entry with an empty Kind and settles the part's whole amount from
// part.Spec; a wildcard part records one entry per unit, each settling one
// counter of its chosen kind.
type SubCounterPay struct {
	Part int
	Obj  state.ObjID
	Kind string
}
