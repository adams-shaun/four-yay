// ItemFor addresses a verdict row by (card, template), the one lookup
// rule/triage/refreeze use so they reach a level-B row as well as a level-A
// one (docs/superpowers/specs/2026-10-05-compliance-level-b.md section 4).
//
// A level-A template string has no "#": it returns Generate's item only when
// the generator still makes that template, so a row whose scenario is stale
// reports the reason rather than being silently re-scored. A level-B string
// is a requirement key: it finds the card's matching levelb.Requirement and
// asks GenerateB for its scenario.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// ItemFor builds the scenario a (card, template) verdict row is for, or says
// why it cannot: the card is not in the corpus, the level-A template is no
// longer the one the generator makes, or the string names no level-B
// requirement of the card.
func ItemFor(reg *cards.Registry, name, template string) (oraclegen.Item, *oraclegen.Skip) {
	if !strings.Contains(template, "#") {
		it, skip := Generate(reg, name)
		if skip != nil {
			return oraclegen.Item{}, skip
		}
		if it.Template != template {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "template is now " + it.Template}
		}
		return it, nil
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == template {
			return GenerateB(reg, name, req)
		}
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "unknown level-B key " + template}
}
