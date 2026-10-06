// Level-B item identity. A level-B scenario is named by its requirement key
// ("activate#0.2") rather than a template id, so a card can hold one verdict
// row per requirement with the row key still `card -> template`
// (docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2).
//
// NewItem (gen.go) is deliberately NOT touched: it hard-codes CR 601.2 and
// the level-A Why, and the verdict's ScenarioSHA is the sha of the whole
// item, so any change to those bytes stales every committed level-A row.
package oraclegen

import "fmt"

// NewLevelBItem names a level-B template's scenario. key is the level-B
// requirement key (<family>#<face>.<slot>), version the serving template's
// version, cr the family's rules citations and sc the scenario body. The
// version is part of the id and the scenario name, so bumping one template
// stales only that template's verdicts.
func NewLevelBItem(card, key string, version int, cr []string, sc Scenario) Item {
	sc.Name = fmt.Sprintf("gen%d-%s", version, key)
	sc.CR = cr
	sc.Why = "generated level-B scenario"
	return Item{ID: fmt.Sprintf("%s/%s/v%d", card, key, version), Card: card, Template: key, Scenario: sc}
}
