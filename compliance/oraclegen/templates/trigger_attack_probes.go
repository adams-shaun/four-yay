// Picking the attacker a "whenever <filter> attacks" / "whenever you attack"
// trigger needs. The old recipe always attacked with the card itself (or a
// Grizzly Bears when the card is not a creature), so a trigger whose filter
// names a subtype (a Dinosaur, a Mount or Vehicle, a Rat), or whose own source
// cannot attack (a Wall, or a card whose trigger functions from the graveyard),
// never fired and the requirement was skipped "trigger did not fire".
//
// The filter is not parsed here: gorge's own matcher (filterProbe, the same
// effects.MatchesObjectCtx the trigger uses) accepts or rejects each candidate
// creature, so the engine stays the only authority on what the filter means.
// The card itself is kept as the attacker whenever the filter names it
// (Card.Self) or accepts it, so every recipe that already fired is unchanged.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// triggerAttacker returns the attacker a trigger's attack cause should send:
// the card itself when the filter names it or the matcher accepts it and it
// can attack, otherwise a probe creature the filter accepts. extra is the
// probe placed on p0's battlefield. It returns "" when no creature can serve,
// so the caller keeps its own fallback (the Grizzly Bears, or the named
// combat-damage skip).
func triggerAttacker(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (attacker string, extra []string) {
	filter := attackCauseFilter(t)
	// A trigger that functions from its owner's graveyard (Threshold's
	// "whenever you attack with one or more Rats") is not on the battlefield,
	// so it can never be the attacker: a probe creature the filter accepts is.
	sourceAttacks := f.IsCreature() && !attackerWalled(f) && !attacksFromGraveyard(t)
	if filter == "" {
		if sourceAttacks {
			return "p0:" + name, nil
		}
		return "p0:" + bearsProbe, []string{bearsProbe}
	}
	if sourceAttacks && strings.Contains(strings.ToLower(filter), "self") {
		return "p0:" + name, nil
	}
	// The condition-shape machinery already serves a qualifier filter (power,
	// menace, equipped) by adding an extra attacker beside the card, so the
	// card itself stays the base attacker and those additions land on it.
	if sourceAttacks && attackShapeHandles(filter) {
		return "p0:" + name, nil
	}
	fp := newFilterProbe(filter, state.ZBattlefield)
	if card, ok := reg.Lookup(name); ok && sourceAttacks && fp.accepts(card) {
		return "p0:" + name, nil
	}
	for _, p := range fp.victimProbes(reg, name, 4) {
		return "p0:" + p, []string{p}
	}
	return "", nil
}

// attacksFromGraveyard reports a trigger whose TriggerZones$ names the
// graveyard, so its source is not a battlefield permanent and cannot attack.
func attacksFromGraveyard(t *cards.Trigger) bool {
	return strings.Contains(strings.ToLower(t.ParamStr(cards.PKTriggerZones)), "graveyard")
}

// attackShapeHandles reports a filter the condition-shape machinery already
// serves by adding an attacker or a counter beside the card (attackShape's
// powerGE4, withMenace and equipped cases, and conditionShape's counter case).
// The card stays the base attacker so those additions land on top of it rather
// than replacing it.
func attackShapeHandles(filter string) bool {
	f := strings.ToLower(filter)
	return strings.Contains(f, "power") || strings.Contains(f, "withmenace") ||
		strings.Contains(f, "equipped") || strings.Contains(f, "counter")
}

// attackCauseFilter is the filter the trigger's attacker must satisfy: the
// ValidAttackers$ an AttackersDeclared trigger names, else the ValidCard$ an
// Attacks trigger names.
func attackCauseFilter(t *cards.Trigger) string {
	if v := t.ParamStr(cards.PKValidAttackers); v != "" {
		return v
	}
	return t.ParamStr(cards.PKValidCard)
}

// attackerWalled reports a printed keyword that stops the creature from being
// declared as an attacker (CR 702.3b's Defender).
func attackerWalled(f *cards.Face) bool {
	_, ok := f.KeywordParam("Defender")
	return ok
}
