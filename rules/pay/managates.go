package pay

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// GainedManaRef is the has-all-abilities-of identity of one mana ability
// activation (Forge's GainsAbilitiesOf$, rules/legal.go's grantedAbilities):
// the FOREIGN card the ability belongs to, its index in that card's face
// Abilities, and the face itself. A zero value (face == nil) is an ordinary
// printed or SVar-granted mana ability. It is threaded through the mana
// activation and every one of its choice continuations (colour, unless-pay,
// discard/exile cost) because the SA pointer does not survive them -- a
// Produced$ rewrite or an unless-gate strip resolves a COPY -- and the
// identity is what (a) resolves the body's SVars (Amount$ X) against the
// foreign face, not the recipient's, and (b) records the replayable
// ManaActivate marker (IDs[0] = foreign card, Amount = index) that the
// GainsAbilitiesLimitPerTurn$ cap counts (gainedActivationsThisTurn).
type GainedManaRef struct {
	From state.ObjID
	Idx  int
	Face *cards.Face
}

// SVars returns the SVar table a gained mana ability resolves against, or
// fallback for an ordinary ability.
func (r GainedManaRef) SVars(fallback map[string]string) map[string]string {
	if r.Face != nil {
		return r.Face.SVars
	}
	return fallback
}

// ManaSVarGateOK is the mana walks' CheckSVar$/SVarCompare$ activation gate:
// the same shared evaluator sVarGateOK (rules/legal.go) applies to every
// non-mana activation offer, so a gated ability (Glistening Sphere's
// Corrupted "Activate only if an opponent has three or more poison
// counters") reads one member set across the priority offer, the payment
// windows and the V1 payment planner. Only the merged face is walk-specific:
// pileAbilityRefOf resolves it for a printed SA (a mutated pile's under-card
// face reads its own SVar table); a granted or static-granted body is not a
// pile member and reads merged 0, the top face's table. sVarGateOK's
// fail-OPEN on an unevaluable body is the mana contract too -- an unreadable
// gate never silently removes a card's activation.
func ManaSVarGateOK(e Engine, o *state.Object, p state.PlayerID, id state.ObjID, ma *cards.SA) bool {
	if _, ok := ma.Param(cards.PKCheckSVar); !ok {
		return true
	}
	merged := 0
	if _, m, found := chars.PileAbilityRef(o, ma); found {
		merged = m
	}
	return e.Chars().SVarGate(p, id, ma, merged)
}

// TapCostSick is CR 302.6's shared source-cost predicate for {T}/{Q}.
// Tapping another permanent to pay a cost is intentionally not checked here.
func TapCostSick(e Engine, source state.ObjID, cost *costvocab.Cost) bool {
	return TapFlagsSick(e, source, cost.Tap, cost.Untap)
}

// TapFlagsSick is tapCostSick reading only the cost's {T}/{Q} flags, so a
// caller holding a shared compiled cost (costRef) passes two bools rather
// than copying the whole Cost.
func TapFlagsSick(e Engine, source state.ObjID, tap, untap bool) bool {
	o := e.Game().Obj(source)
	if o == nil || (!tap && !untap) || o.Zone != state.ZBattlefield || !o.SummonSick {
		return false
	}
	return slices.Contains(e.Chars().DerivedTypes(source), "Creature") && !e.Chars().HasKeyword(source, chars.KWHaste)
}

func ManaAbilityTapSick(e Engine, source state.ObjID, ma *cards.SA) bool {
	if ma == nil {
		return false
	}
	c := CostRef(e, ma.ParamStr(cards.PKCost))
	return TapFlagsSick(e, source, c.Tap, c.Untap)
}

// GainedManaRefFor reports the has-all-abilities-of identity of mana ability
// sa activated from source, measured against sa's ORIGINAL compiled pointer
// (callers pass the ability before any Produced$ rewrite). A printed ability
// on source's own pile wins -- the ordinary case, answered without walking
// the grants -- so a recipient that happens to share the foreign card's
// compiled face keeps the printed identity. Otherwise the first live gained
// grant in grantedAbilities' deterministic order whose foreign SA is sa is
// the identity; grantedAbilities has already applied the grant's
// GainsValidAbilities$ filter and GainsAbilitiesLimitPerTurn$ cap, so a
// capped grant never supplies it.
func GainedManaRefFor(e Engine, p state.PlayerID, source state.ObjID, sa *cards.SA) GainedManaRef {
	o := e.Game().Obj(source)
	if o == nil || sa == nil {
		return GainedManaRef{}
	}
	if _, _, printed := chars.PileAbilityRef(o, sa); printed {
		return GainedManaRef{}
	}
	for _, ga := range e.Chars().GrantedAbilities(p, source) {
		if ga.Gained && ga.SA == sa && ga.GainedFace != nil {
			return GainedManaRef{From: ga.GainedFrom, Idx: ga.GainedIdx, Face: ga.GainedFace}
		}
	}
	return GainedManaRef{}
}

// ManaActivationGateHolds evaluates a plain AB$ Mana ability's IsPresent$/
// PresentCompare$ existence gate (the same shape manaReflectedPresentHolds is
// for a reflected ability):
//
//   - IsPresent$ <spec> with PresentCompare$ <op><n>: the count of objects
//     matching <spec> (Shrine of the Forsaken Gods' "Activate only if you
//     control seven or more lands"). PresentCompare$ absent means GE1.
//
// An Activation$ <mechanic> rides rules/legal.go's shared
// activationConditionOK instead (main's vocabulary: Hellbent, Threshold,
// Metalcraft, Delirium), so the two gates compose rather than duplicate.
//
// A gate this build cannot price fails closed: the ability is withheld from
// the offer, the payment window and the activation alike, never widened.
func ManaActivationGateHolds(e Engine, p state.PlayerID, id state.ObjID, ma *cards.SA) bool {
	// ActivationPhases$ and its rider qualifiers (PlayerTurn$,
	// OpponentTurn$, ActivationFirstCombat$, ActivationAfterBlockers$) are
	// the same offer-time window a non-mana ability is gated by. This walk
	// is a mana ability's ONLY eligibility gate, so reading the window here
	// is what keeps one AB$ Mana carrier (a charge-counter source whose
	// "any player may activate ... only during their turn before the end
	// step" line was previously offered outside its window) bound to it.
	if !e.Chars().ActivationPhasesOK(p, ma) {
		return false
	}
	if spec, ok := ma.Param(cards.PKIsPresent); ok && strings.TrimSpace(spec) != "" {
		return e.Chars().PresentGate(strings.TrimSpace(spec), strings.TrimSpace(ma.ParamStr(cards.PKPresentCompare)), id, p)
	}
	return true
}
