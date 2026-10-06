package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// classifyZoneChangeTrigger names the zone-change shapes with turn-one causes.
// The existing dies/self-ETB classifiers retain ownership of their narrower
// cases; unsupported filters remain ordinary named mode gaps.
func classifyZoneChangeTrigger(t *cards.Trigger) (string, bool) {
	mode := t.ModeKind()
	origin := t.ParamStr(cards.PKOrigin)
	dest := t.ParamStr(cards.PKDestination)
	filter := ZoneChangeFilter(t)
	if mode == cards.TriggerChangesZoneAll {
		if namesSelf(filter) {
			return "", false
		}
		switch {
		case strings.EqualFold(dest, "Battlefield"):
			return "trigger.etb-other", true
		case strings.EqualFold(origin, "Graveyard"):
			return "trigger.leaves-graveyard", true
		case strings.Contains(strings.ToLower(origin), "battlefield") && !strings.EqualFold(dest, "Graveyard"):
			return "trigger.ltb-other", true
		case !strings.EqualFold(dest, "Graveyard"), !strings.Contains(strings.ToLower(origin), "battlefield"):
			return "trigger.zone-change-residue", true
		}
		return "", false
	}
	if mode != cards.TriggerChangesZone {
		return "", false
	}
	if strings.Contains(strings.ToLower(filter), "chosencardstrict") ||
		(strings.EqualFold(origin, "Library") && strings.EqualFold(dest, "Graveyard")) ||
		(strings.EqualFold(origin, "Any") && strings.EqualFold(dest, "Graveyard")) {
		return "trigger.zone-change-residue", true
	}
	if strings.EqualFold(origin, "Graveyard") && !strings.EqualFold(dest, "Battlefield") && !namesSelf(filter) {
		return "trigger.leaves-graveyard", true
	}
	if strings.EqualFold(origin, "Battlefield") && !namesSelf(filter) && !strings.EqualFold(dest, "Graveyard") {
		return "trigger.ltb-other", true
	}
	if selfLeavesBattlefield(t) && !strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return "trigger.ltb-self", true
	}
	return "", false
}

// selfLeavesBattlefield reports a ChangesZone trigger on the card itself
// leaving the battlefield for somewhere other than exactly the battlefield or
// the graveyard (the self-dies shape trigger.dies serves).
func selfLeavesBattlefield(t *cards.Trigger) bool {
	dest := t.ParamStr(cards.PKDestination)
	return t.ModeKind() == cards.TriggerChangesZone &&
		strings.EqualFold(t.ParamStr(cards.PKOrigin), "Battlefield") &&
		namesSelf(t.ParamStr(cards.PKValidCard)) &&
		!strings.EqualFold(dest, "Battlefield") && !strings.EqualFold(dest, "Graveyard")
}

// ZoneChangeFilter is the card filter of a ChangesZone (ValidCard$) or
// ChangesZoneAll (ValidCards$) trigger: the one reader of that rule, shared
// with the oraclegen recipes.
func ZoneChangeFilter(t *cards.Trigger) string {
	if t.ModeKind() == cards.TriggerChangesZoneAll {
		return t.ParamStr(cards.PKValidCards)
	}
	return t.ParamStr(cards.PKValidCard)
}
