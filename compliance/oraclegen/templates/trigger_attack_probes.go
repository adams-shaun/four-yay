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

// triggerAttacker returns the attackers a trigger's attack cause should send:
// the card itself when the filter names it or the matcher accepts it and it
// can attack, otherwise a probe creature the filter accepts. extra is the
// probe placed on p0's battlefield. When no creature can serve, the Grizzly
// Bears fallback is returned (the caller's own combat-damage skip is the one
// case that never reaches here).
func triggerAttacker(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (attackers []string, extra []string) {
	filter := attackCauseFilter(t)
	// A trigger that functions from its owner's graveyard (Threshold's
	// "whenever you attack with one or more Rats") is not on the battlefield,
	// so it can never be the attacker: a probe creature the filter accepts is.
	sourceAttacks := f.IsCreature() && !attackerWalled(f) && !attacksFromGraveyard(t)
	// A trigger whose own IsPresent$ spec requires the source to be attacking
	// this combat (Tolsimir, Midnight's Light's "if CARDNAME attacked this
	// combat") keeps the source among the attackers even when the filter names
	// another creature to attack with: a lone probe satisfies the filter but
	// not the source's own condition.
	joinSelf := func(atk []string) []string {
		if !sourceAttacks || !sourceAttackCondition(t) || atk[0] == "p0:"+name {
			return atk
		}
		return append([]string{"p0:" + name}, atk...)
	}
	if filter == "" {
		if sourceAttacks {
			return []string{"p0:" + name}, nil
		}
		return joinSelf([]string{"p0:" + bearsProbe}), []string{bearsProbe}
	}
	if sourceAttacks && strings.Contains(strings.ToLower(filter), "self") {
		return []string{"p0:" + name}, nil
	}
	// The condition-shape machinery already serves a qualifier filter (power,
	// menace, equipped) by adding an extra attacker beside the card, so the
	// card itself stays the base attacker and those additions land on it.
	if sourceAttacks && attackShapeHandles(filter) {
		return []string{"p0:" + name}, nil
	}
	fp := newFilterProbe(filter, state.ZBattlefield)
	if card, ok := reg.Lookup(name); ok && sourceAttacks && fp.accepts(card) {
		return []string{"p0:" + name}, nil
	}
	// A filter that names a creature type the probe table carries wants that
	// plain creature, not a Changeling the engine matcher happens to accept:
	// the emitted scenario's attacker is read back by its printed types.
	lower := strings.ToLower(filter)
	for _, tp := range attackerTypeProbes {
		if strings.Contains(lower, tp.word) {
			if _, ok := reg.Lookup(tp.probe); ok {
				return joinSelf([]string{"p0:" + tp.probe}), []string{tp.probe}
			}
		}
	}
	for _, p := range fp.victimProbes(reg, name, 4) {
		return joinSelf([]string{"p0:" + p}), []string{p}
	}
	return joinSelf([]string{"p0:" + bearsProbe}), []string{bearsProbe}
}

// sourceAttackCondition reports a trigger whose IsPresent$ (or IsPresent2$)
// spec requires the source itself to be attacking this combat
// (Card.Self+attackedThisCombat, Card.Self+attacking). The bare self filters
// the recipe already serves (Card.Self, Card.StrictlySelf) and the solved-Case
// and untapped/tapped self specs carry no attack predicate, so they read
// false.
func sourceAttackCondition(t *cards.Trigger) bool {
	present := strings.ToLower(t.ParamStr(cards.PKIsPresent) + " " + t.ParamStr(cards.PKIsPresent2))
	if !strings.Contains(present, "card.self") {
		return false
	}
	return strings.Contains(present, "attacked") || strings.Contains(present, "attacking")
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
