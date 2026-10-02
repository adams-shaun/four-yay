package mzbridge

import "github.com/adams-shaun/gorge/cards"

// ActivatedRule is the rule text of face f's activated ability at
// Face.Abilities index (decision.Option.Ability): the string the encoder
// names that ability's sub-node by, which stands in for XMage's
// Ability.getRule() -- and, for an activated ability, for the
// ability.toString() upstream's ActionEncoder looks a priority action up by
// (ActivatedAbilityImpl has no toString of its own; AbilityImpl.toString is
// getRule()). ok is false when the face has no activated ability there.
func (enc *Encoder) ActivatedRule(f *cards.Face, index int) (string, bool) {
	if f == nil || index < 0 {
		return "", false
	}
	for _, a := range enc.face(f).abilities {
		if a.kind == abActivated && a.index == index {
			return a.rule, a.rule != ""
		}
	}
	return "", false
}
