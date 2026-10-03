package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// abilityGrant builds the server-side decision.Grant no-op flag for an
// activated ability ab on the permanent id (the option's Obj), only when the
// ability's whole effect is a pure, idempotent keyword grant. It returns nil
// for every other ability, so an additive or non-grant activation is never
// mistaken for a repeatable no-op. For a pure keyword grant it sets the two
// independent redundant halves the bot policy's no-op rule (A1) reads:
// Already (the granting permanent currently has every granted keyword, so
// the grant is already in effect) and Duplicate (an identical grant from the
// same source is already on the stack unresolved).
func (e *Engine) abilityGrant(id state.ObjID, ab *cards.SA) *decision.Grant {
	kw := pureGrantKeywords(ab)
	if len(kw) == 0 {
		return nil
	}
	g := &decision.Grant{Keywords: kw}
	all := true
	for _, k := range kw {
		if !e.HasKeyword(id, k) {
			all = false
			break
		}
	}
	g.Already = all
	g.Duplicate = e.grantPending(id, kw)
	return g
}

// pureGrantKeywords returns the keywords an SA grants when its whole effect is
// a pure, idempotent keyword grant AIMED AT ITS OWN SOURCE: a Pump adding only
// keywords (a non-empty KW$), with no additive stat change (NumAtt/NumDef), no
// SubAbility$ chain, and no colon-parametrised keyword. nil means the
// activation is not such a grant, so it stacks and must never be treated as a
// no-op. This is the engine-side definition of "idempotent keyword grant" that
// decision.Grant is built from.
//
// Every clause here excludes a corpus population the caller cannot reason
// about, measured over .cards/cardsfolder with /usr/bin/grep. Of the 588 raw
// A:AB$ Pump/PumpAll lines carrying a KW$ and no NumAtt/NumDef:
//
//   - 252 are TARGETED (ValidTgts$). The recipient is the target, not the
//     source, and the activation option carries no target at all -- targets
//     arrive at a later KTarget decision -- so neither "the source already has
//     this keyword" nor "an identical grant from this source is pending" says
//     anything about the creature that would actually receive it. Worse, the
//     pending check would refuse to aim a second copy at a DIFFERENT creature.
//   - 66 are PumpAll, granting to a filtered set rather than to Obj.
//   - 66 carry a SubAbility$ whose sub-effect is additive (DBUntap,
//     DBPutCounter, DBDealDamage), so suppressing the activation would forfeit
//     an untap, a counter or damage -- a worse bug than the one being fixed.
//   - 29 carry a colon-parametrised KW$, 20 of them Landwalk:<type>.
//     cards.KeywordHead strips at the colon, so Landwalk:Forest and
//     Landwalk:Island collapse to the same head on both sides of HasKeyword:
//     a creature with islandwalk would decline gaining forestwalk.
//
// Narrowing to the self-aimed Pump was measured to move no acceptance head and
// no botbench golden, so the wider form bought nothing it could be trusted on.
func pureGrantKeywords(ab *cards.SA) []string {
	if ab == nil || ab.API != "Pump" {
		return nil
	}
	if ab.Sub != nil {
		return nil
	}
	switch ab.ParamStr(cards.PKDefined) {
	case "Self", "Parent":
	case "":
		if effects.TargetsOf(ab).Has(effects.TgtValidPresent) {
			return nil
		}
	default:
		return nil
	}
	pp := effects.PumpOf(ab)
	if pp.NumAtt.Present || pp.NumDef.Present {
		return nil
	}
	if strings.Contains(pp.Grant.KWText, ":") {
		return nil
	}
	return grantKeywords(pp.Grant.KWText)
}

// grantKeywords splits a KW$ parameter's keyword list into head-stripped
// words through cards.SplitKeywordList -- the shared Forge parser -- because
// this package cannot re-express the grammar without drifting from it. The
// Forge list separator is ampersand; a comma remains part of a parameter.
func grantKeywords(kw string) []string {
	var out []string
	for _, p := range cards.SplitKeywordList(kw) {
		out = append(out, cards.KeywordHead(p))
	}
	return out
}

// grantPending reports whether an identical keyword grant (the same granted
// keyword set) from the same source permanent id is already on the stack
// unresolved. It walks the shared stack order -- never a map -- and matches
// an ability object whose Source is id and whose own whole effect is the
// same pure keyword grant, so a stack spell, a trigger, or an additive
// activation from id never counts as a duplicate of an idempotent grant.
func (e *Engine) grantPending(id state.ObjID, kw []string) bool {
	for _, oid := range e.G.Zone(state.ZStack, 0) {
		o := e.G.Obj(oid)
		if o == nil || o.Source != id {
			continue
		}
		if grantSetsEqual(pureGrantKeywords(o.Ability), kw) {
			return true
		}
	}
	return false
}

// grantSetsEqual reports whether two granted keyword sets are the same,
// order-independently and case-insensitively.
func grantSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if strings.EqualFold(x, y) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// faceHasActivationZone reports whether any activated ability printed on f
// names zone in its ActivationZone$.
func faceHasActivationZone(f *cards.Face, zone string) bool {
	for _, ab := range f.Abilities {
		if ab != nil && ab.Kind == "AB" && strings.TrimSpace(ab.ParamStr(cards.PKActivationZone)) == zone {
			return true
		}
	}
	return false
}
