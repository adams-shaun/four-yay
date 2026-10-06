package trigmatch

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// abilityTriggeredMatches implements Mode$ AbilityTriggered ("whenever X
// causes a triggered ability of Y to trigger"; Firebender Ascension, Aboleth
// Spawn, Historian's Boon, Strict Proctor). The event is the
// events.AbilityTriggered marker: Obj the ability's source, Counter the
// CAUSING trigger's mode (ValidMode$), Amount 1 for the source's own ability
// (TriggeredOwnAbility$ True). ValidDestination$ and ValidSpellAbility$
// (three of the four carriers) are not modelled by the marker, so a line
// carrying them fails closed rather than firing wide.
func abilityTriggeredMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.AbilityTriggered {
		return false
	}
	if t.ParamStr(cards.PKValidDestination) != "" || t.ParamStr(cards.PKValidSpellAbility) != "" {
		return false
	}
	if v := t.ParamStr(cards.PKValidMode); v != "" {
		found := false
		for m := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(m), ev.Counter) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if ParamTrue(t, cards.PKTriggeredOwnAbility) && ev.Amount != 1 {
		return false
	}
	if v := t.ParamStr(cards.PKValidSource); v != "" &&
		!e.MatchesSpec(v, ev.Obj, source, e.ControllerOf(source), SpecOpts{}) {
		return false
	}
	return true
}

func init() { registerTrigMatcher(abilityTriggeredMatches, "AbilityTriggered") }

// AttackCausedBySource reports whether source is itself one of the attackers
// whose declaration (ev) caused its own attack trigger line t to trigger, the
// "own ability" half of the AbilityTriggered marker. handled is false when t
// is not an attack-declaration line over a DeclareAttackers event, so the
// caller reads the causing object elsewhere.
//
// DeclareAttackers names every attacker (and a batch line the whole
// declaration), not just the ones the line is about, so membership alone is
// not causation: the creature must satisfy the line's own filter -- ValidCard$
// for Mode$ Attacks (none: only the source itself), ValidAttackers$ for the
// AttackersDeclared pair (none: any attacker) -- exactly as the matchers read
// them. A "Creature.Other+YouCtrl" watcher is therefore never its own cause.
func AttackCausedBySource(e Board, t cards.Trigger, source state.ObjID, ev events.Event) (own, handled bool) {
	if ev.Kind != events.DeclareAttackers {
		return false, false
	}
	ctrl := e.ControllerOf(source)
	switch t.ModeKind() {
	case cards.TriggerAttacks:
		if !slices.Contains(ev.IDs, source) {
			return false, true
		}
		spec, ok := t.Param(cards.PKValidCard)
		if !ok {
			return firstAttackOK(e, t, source), true
		}
		opts := SpecOpts{ExtraTypes: e.Chars(source).Types}
		return e.MatchesSpec(spec, source, source, ctrl, opts) && firstAttackOK(e, t, source), true
	case cards.TriggerAttackersDeclared, cards.TriggerAttackersDeclaredOneTarget:
		ids := ev.IDs
		if batch := e.Facts().DeclaredAttackers; AttackersDeclaredBatch(t) && len(batch) > 0 {
			ids = batch
		}
		if !slices.Contains(ids, source) {
			return false, true
		}
		v := t.ParamStr(cards.PKValidAttackers)
		return v == "" || e.MatchesSpec(v, source, source, ctrl, SpecOpts{}), true
	}
	return false, false
}
