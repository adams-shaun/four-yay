package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
)

// triggerConditionFixtures builds board and count candidates from the trigger's
// own predicates. The activation helpers do the filter/count interpretation;
// the trigger fire probe remains the authority on whether a candidate works.
func triggerConditionFixtures(reg *cards.Registry, f *cards.Face, t *cards.Trigger) []conditionPrelude {
	var out []conditionPrelude
	zone := t.ParamStr(cards.PKPresentZone)
	compare := t.ParamStr(cards.PKPresentCompare)
	for _, key := range []cards.ParamKey{cards.PKIsPresent, cards.PKIsPresent2} {
		spec := t.ParamStr(key)
		if strings.TrimSpace(spec) == "" {
			continue
		}
		if unknown := effects.UnknownPredicates(spec); len(unknown) > 0 {
			continue
		}
		built, _ := activationPresentPrelude(reg, spec, zone, compare)
		out = append(out, built...)
		if strings.Contains(strings.ToLower(spec), "card.self") && strings.Contains(strings.ToLower(spec), "tapped") && f.IsCreature() {
			out = append(out, conditionPrelude{steps: []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + f.Name}}, {Op: "pass_to", Step: "main2"}}})
		}
	}
	check := t.ParamStr(cards.PKCheckSVar)
	if check != "" {
		built, _ := activationSVarPrelude(reg, f, check, t.ParamStr(cards.PKSVarCompare))
		out = append(out, built...)
	}
	return out
}
