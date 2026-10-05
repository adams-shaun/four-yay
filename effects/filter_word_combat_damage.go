package effects

import "github.com/adams-shaun/gorge/state"

// dealtCombatDamageBySource answers Forge's
// ControllerDealtCombatDamageBySource predicate: the candidate's controller was
// dealt combat damage by the filter's source this turn. During resolution the
// source is the ability stack object, so its source permanent is the card that
// dealt the damage. Extracted from wordMatches to keep that switch within its
// frozen code-shape ceiling.
func dealtCombatDamageBySource(g *state.Game, o *state.Object, sc SpecContext, source state.ObjID) bool {
	if source == 0 || o.Controller < 0 {
		return false
	}
	if ability := g.Obj(source); ability != nil && ability.Ability != nil {
		source = ability.Source
	}
	for _, hit := range sc.CombatDamageHits {
		if hit.Source == source && hit.Player == o.Controller {
			return true
		}
	}
	return false
}
