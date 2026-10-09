// Self-state gates the setup-placed source cannot show but a freshly cast one
// can (Level B). A trigger whose CheckSVar$ counts a self attribute fails at
// the probe's checkpoint for one of two reasons: the setup card's own turn-1
// behaviour already consumed the attribute its upkeep trigger grants
// (prepared: the "becomes prepared" trigger fired during the setup drive at
// turn 1's upkeep, so the gate's unprepared side is spent), or setup placement
// drops the enter trigger that would have granted it (suspected: CR 708 applies
// to a placed permanent, not a cast one). Casting the source on turn 1 gives a
// fresh self whose attribute state is unset, so the gate reads true at the
// checkpoint. The fire probe, not this classifier, decides.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// selfCastAttribute reports the self-attribute a CheckSVar$ Count$ValidSelf
// gate reads that casting the source on turn 1 can satisfy but the
// setup-placed source cannot, or "" when the gate is not such a gate. The
// setup card's own triggers must grant the attribute (the trigger under test
// for prepared, an enter trigger for suspected), so the fresh cast really
// starts without it.
func selfCastAttribute(f *cards.Face, t *cards.Trigger) string {
	check := t.ParamStr(cards.PKCheckSVar)
	if check == "" {
		return ""
	}
	body, ok := f.SVars[check]
	if !ok {
		body = check
	}
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "validself") {
		return ""
	}
	switch {
	case strings.Contains(lower, "!isprepared") && selfTriggerGrants(f, t, "prepared"):
		return "prepared"
	case strings.Contains(lower, "issuspected") && !strings.Contains(lower, "!issuspected") && selfTriggerGrants(f, t, "suspected"):
		return "suspected"
	}
	return ""
}

// selfTriggerGrants reports that one of the card's own triggers grants the
// AlterAttribute on itself: the trigger under test's own body (the upkeep
// "becomes prepared" grant) or an enter trigger's (the suspect grant).
// Defined$ defaults to the source for an attribute grant, so an absent
// Defined$ on the trigger under test still counts.
func selfTriggerGrants(f *cards.Face, t *cards.Trigger, attr string) bool {
	need := "attributes$ " + attr
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		body := strings.ToLower(f.SVars[tr.ParamStr(cards.PKExecute)])
		if !strings.Contains(body, "alterattribute") || !strings.Contains(body, need) {
			continue
		}
		if tr == t || strings.Contains(body, "defined$ self") {
			return true
		}
	}
	return false
}

// selfActivatedAttributePreludes is the setup for an IsPresent$ Card.Self+<attr>
// gate the source's own activated ability grants (The Mind Stone's "{5}{W},
// {T}: Harness"): the prelude activates that ability in turn 1's first main
// phase, before the trigger's own checkpoint. A cost with no payable gap is
// served: its fixtures (the creature an Exile<1/Creature> cost exiles) travel
// on the prelude's own setup fields and its choices are scripted the way the
// activate cause path scripts them (the stateTriggerRecipe shape).
func selfActivatedAttributePreludes(reg *cards.Registry, f *cards.Face, group string) []conditionPrelude {
	attr := ""
	for _, w := range affectedWords(group) {
		lower := strings.ToLower(w)
		if lower == "self" || counterFilterRE.MatchString(lower) {
			continue
		}
		attr = lower
	}
	if attr == "" {
		return nil
	}
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return nil
	}
	var out []conditionPrelude
	for i, sa := range f.Abilities {
		if !sa.IsActivated() {
			continue
		}
		body := strings.ToLower(sa.Line)
		if !strings.Contains(body, "alterattribute") || !strings.Contains(body, "attributes$ "+attr) {
			continue
		}
		if defined := sa.ParamStr(cards.PKDefined); defined != "" && !strings.EqualFold(defined, "Self") {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield", "")
		if gap != "" {
			continue
		}
		prefix, exists := prefixes[i]
		if !exists {
			continue
		}
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, f.Name, cost)
		// A cost fixture must land in a zone the prelude setup carries: the
		// seat fields addActivationCostFixtures writes (hand, battlefield,
		// graveyard) are exactly the prelude's own. Anything else keeps the
		// named gap rather than emitting a scenario that cannot play.
		if len(setup.Exile) != 0 || len(setup.Library) != 0 || len(setup.BackFace) != 0 {
			continue
		}
		idx := i
		out = append(out, conditionPrelude{
			hand:           append([]string(nil), setup.Hand...),
			battlefield:    append([]string(nil), setup.Battlefield...),
			graveyard:      append([]string(nil), setup.Graveyard...),
			steps:          []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + f.Name, Mana: mana, AbilityIndex: &idx, Answers: activationXAnswers(cost)}},
			xability:       []string{prefix},
			activationCost: cost,
		})
	}
	return out
}

// selfCastGateCauses appends a cast-self variant of each base cause when the
// trigger's gate is such a self attribute: the source is cast on turn 1 (its
// enter trigger grants the attribute, or its upkeep firing has not yet consumed
// it) and the cause then runs at the checkpoint. triggerSteps already prepends
// the cast and its resolve for a castSelfX cause, so the variant carries no
// steps of its own.
func selfCastGateCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, base []triggerCause) []triggerCause {
	if selfCastAttribute(f, t) == "" || len(base) == 0 {
		return nil
	}
	out := make([]triggerCause, 0, len(base))
	for _, b := range base {
		if b.castSelfX {
			continue
		}
		c := b
		c.castSelfX = true
		out = append(out, c)
	}
	return out
}
