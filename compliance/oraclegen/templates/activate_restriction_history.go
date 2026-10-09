package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// activateSVarHistoryPreludes builds the CheckSVar$-gate setups the activate
// template's own rows need, on top of the shared conditionPreludes and
// historyPreludes candidates activationSVarPrelude already offers. Deliberately
// activate-local — not a historyKinds row, not a conditionPreludes branch —
// because those shared tables also feed the trigger-condition scenario path,
// and new candidates there change scenario bytes for other cards' rows.
func activateSVarHistoryPreludes(reg *cards.Registry, body string, n int) []conditionPrelude {
	lower := strings.ToLower(body)
	if pre, ok := legendaryCombatDamagePrelude(reg, lower, n); ok {
		return []conditionPrelude{pre}
	}
	if pre, ok := artifactSacrificePrelude(reg, lower); ok {
		return []conditionPrelude{pre}
	}
	return nil
}

// legendaryCombatDamagePrelude serves a gate on "an opponent was dealt combat
// damage by a legendary creature this turn" (Blitzball): a legendary creature
// on p0's battlefield attacks p1, the combat damage resolves, and the
// scenario is back in a main phase. A non-legendary "was dealt combat damage
// by" body is not served here — this helper only exists for the legendary
// shape, which the shared Shock-at-p1 prelude answers with non-combat spell
// damage and the gate correctly rejects.
func legendaryCombatDamagePrelude(reg *cards.Registry, lower string, n int) (conditionPrelude, bool) {
	if !strings.Contains(lower, "wasdealtcombatdamagethisturnby") || !strings.Contains(lower, "legendary") {
		return conditionPrelude{}, false
	}
	legendary := existingCards(reg, []string{"Elanor Gardner"})
	if len(legendary) == 0 || n > 1 {
		return conditionPrelude{}, false
	}
	attackers := make([]string, len(legendary))
	for i, name := range legendary {
		attackers[i] = "p0:" + name
	}
	return conditionPrelude{
		battlefield: legendary,
		steps: []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers},
			{Op: "pass_to", Step: "main2"},
		},
	}, true
}

// artifactSacrificePrelude serves a gate on "you've sacrificed an artifact
// this turn" (Detective's Satchel): the shared sacrifice prelude's Grizzly
// Bears is a creature only, so the gate's Artifact filter stays false under
// it — the artifact CREATURE Ornithopter covers the spec (costSacrificePrelude's
// precedent for the artifact-filtered sacrifice read).
func artifactSacrificePrelude(reg *cards.Registry, lower string) (conditionPrelude, bool) {
	if !strings.Contains(lower, "sacrificedthisturn") {
		return conditionPrelude{}, false
	}
	i := strings.Index(lower, "sacrificedthisturn")
	spec := strings.Fields(lower[i+len("sacrificedthisturn"):])
	if len(spec) == 0 || spec[0] != "artifact" {
		return conditionPrelude{}, false
	}
	return sacrificeConditionPreludeOf(reg, "Ornithopter")
}
