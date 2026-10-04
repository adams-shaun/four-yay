package chars

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Reader is the read direction of the characteristics layer (lasagna spec
// §9.2): the layer-derived characteristics of an object and the activation
// gates every ability shares, for a package above L4 (rules/pay) that may
// not hold the engine. Package rules implements it over its Engine
// (rules/chars_reader.go) on a pointer conversion, so handing it out
// allocates nothing; each method forwards to the engine query the code
// called before it moved, so every answer is unchanged.
//
// Every method is a pure read: none emits an event, and a returned slice is
// a borrowed view valid until the next event, never to be retained or
// written. The method count is a budgeted ratchet (internal/archtest
// TestCharsReaderBudget): a new read is named here and counted.
type Reader interface {
	// DerivedTypes is id's current layer-4 type line (card types, subtypes
	// and supertypes), nil for a missing or faceless object.
	DerivedTypes(id state.ObjID) []string
	// HasKeyword reports whether id's current layer-6 keyword list carries
	// the keyword head kw.
	HasKeyword(id state.ObjID, kw KW) bool
	// SVarGate is the CheckSVar$/SVarCompare$ activation gate p's activation
	// of ab on id must pass, with merged naming the pile face whose SVar
	// table the gate reads (fail open on an unevaluable body).
	SVarGate(p state.PlayerID, id state.ObjID, ab *cards.SA, merged int) bool
	// ActivationPhasesOK is the ActivationPhases$ window (and its PlayerTurn$
	// / OpponentTurn$ / combat riders) p's activation of sa must fall in.
	ActivationPhasesOK(p state.PlayerID, sa *cards.SA) bool
	// PresentGate is an IsPresent$ existence gate: the count of objects
	// matching spec from source's perspective (you its controller) compared
	// by cmp (a PresentCompare$ value; empty means at least one).
	PresentGate(spec, cmp string, source state.ObjID, you state.PlayerID) bool
	// GrantedAbilities is the activated abilities the active AddAbilities
	// grants give id right now, as p would activate them, in the active
	// list's deterministic order.
	GrantedAbilities(p state.PlayerID, id state.ObjID) []Granted
	// SameColorRevealSets is p's hand cards (source excluded when
	// excludeSource) a RevealSameColor cost part may reveal together, with
	// each card's current colour words.
	SameColorRevealSets(p state.PlayerID, source state.ObjID, excludeSource bool) ([]state.ObjID, [][]string)
	// TapPower is the value creature id contributes when it is tapped to pay
	// an action of kind saKind's tap-power amount (Station, Crew, Saddle):
	// its layer-derived power, as any stat:TapPowerValue static adjusts it.
	TapPower(id state.ObjID, saKind string) int32
}

// KW is a keyword head with its interned cards.KeywordHeadID, compiled once
// at package init so a hot keyword read is a bitset test on the face rather
// than a KeywordHead split and case-folded compare per keyword line.
type KW struct {
	S  string
	ID cards.KeywordHeadID
}

// NewKW compiles a literal keyword head (package init only: it interns).
func NewKW(s string) KW { return KW{S: s, ID: cards.InternKeywordHead(s)} }

// Granted is one ability a continuous ability grant (CR 613.1f,
// state.ContinuousEffect.AddAbilities -- a Saga chapter's Animate) gives an
// object right now: the parsed AB and the SVar name on the granting face's
// table that re-resolves it.
type Granted struct {
	SA *cards.SA
	// Source is the object the grant came from (state.ContinuousEffect.Source):
	// the static's own permanent, which need not be the affected object the
	// ability is activated from. It is threaded into decision.Option.GrantSource
	// so the activation resolves the SVar body from here while the minted
	// ability's Source stays the recipient.
	Source state.ObjID
	SVar   string
	// Gained marks an ability granted off a FOREIGN card's compiled face
	// (state.ContinuousEffect.GainedFaces): SA is that card's own ability,
	// GainedFrom is the foreign object id (in the scoped zone) and
	// GainedIdx is the index of SA in that face's Abilities. The activation
	// mints through GainedAbilityPush, which names both so a replay
	// re-resolves the identical SA; a zero GainedFrom means the ordinary
	// SVar-anchored grant. The grant's GainsValidAbilities$ filter and
	// GainsAbilitiesLimitPerTurn$ cap are applied at collection (inside
	// rules' grantedAbilities), the one home both the offer loop and the
	// mana collector read, so no consumer can widen the grant.
	Gained     bool
	GainedFrom state.ObjID
	GainedIdx  int
	// GainedFace is the foreign face SA was compiled on (gained only): the
	// SVar table a gained mana ability resolves against.
	GainedFace *cards.Face
}

// PileAbilityRef maps a printed ability pointer back to its flat pile index
// and merged ordinal. It is the inverse of Object.PileAbilityAt used by the
// mana path, which carries SA pointers rather than indices. ok is false for
// an SA the source does not print.
func PileAbilityRef(o *state.Object, sa *cards.SA) (idx, merged int, ok bool) {
	if o == nil || sa == nil {
		return 0, 0, false
	}
	for i, n := 0, o.PileAbilityCount(); i < n; i++ {
		pa, at := o.PileAbilityAt(i)
		if !at {
			continue
		}
		if pa.SA == sa {
			return i, pa.Merged, true
		}
	}
	return 0, 0, false
}

// KWHaste is the Haste head (CR 302.6's summoning-sickness exemption).
var KWHaste = NewKW("Haste")
