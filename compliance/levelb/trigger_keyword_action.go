package levelb

import "github.com/adams-shaun/gorge/cards"

// classifyKeywordActionTrigger names the sub-family of a trigger whose cause
// is a keyword action (CR 701): Forage, GiveGift, Explores, CollectEvidence,
// ManifestDread, Discover, ElementalBend, BecomesPlotted and BecomesSaddled.
// The engine's trigmatch matchers already exist for every one of these modes
// (rules/trigmatch), so only the level-B cause was missing; the classifier
// moves each out of its trigger.gap:<Mode> bucket and the recipe supplies the
// cause.
func classifyKeywordActionTrigger(t *cards.Trigger) (sub string, ok bool) {
	switch t.ModeKind() {
	case cards.TriggerForage:
		return "trigger.forage", true
	case cards.TriggerGiveGift:
		return "trigger.give-gift", true
	case cards.TriggerExplores:
		return "trigger.explores", true
	case cards.TriggerCollectEvidence:
		return "trigger.collect-evidence", true
	case cards.TriggerManifestDread:
		return "trigger.manifest-dread", true
	case cards.TriggerDiscover:
		return "trigger.discover", true
	case cards.TriggerElementalBend:
		return "trigger.elemental-bend", true
	case cards.TriggerBecomesPlotted:
		return "trigger.becomes-plotted", true
	case cards.TriggerBecomesSaddled:
		return "trigger.becomes-saddled", true
	}
	return "", false
}
