// Counter-added trigger recipes (Level B, ticket
// cli-20261006T144107Z-0a30b609). The "whenever one or more counters are put
// on <recipient>" family (Forge Mode$ CounterAdded, CounterAddedOnce,
// CounterAddedAll, CounterPlayerAddedAll, CounterTypeAddedAll) has one cause
// shape a p0-only turn-1 scenario can produce: a counter-placing probe spell
// aimed at the recipient.
//
//   - A +1/+1 (or kind-less) trigger gets Battlegrowth -- {G}, "Put a +1/+1
//     counter on target creature" -- aimed at the card itself when the
//     trigger's filter is Card.Self, or at a plain creature the trigger's own
//     filter accepts (the engine's matcher is the arbiter, exactly as
//     trigger_filter_probe does for the dying-victim recipes). Dragonscale
//     Boon and Travel Preparations are the fallbacks.
//   - A CounterAdded$ PLAN$ EQn trigger on the card itself holds n-1 plan
//     counters in setup and casts Steady Progress ({2}{U}, proliferate), whose
//     single proliferate pick adds the plan counter that crosses n. The
//     setup counters are the triggerCause.counters fixture
//     (rules/oracle_run.go's setupCounters); the proliferate pick is the
//     cast's own recorded choose answer.
//
// A recipient filter with no probe candidate the engine accepts reports a
// named skip rather than a bare "did not fire".
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// counterPlacingProbes are the "put a +1/+1 counter on target creature"
// spells, tried in order (Battlegrowth is the cheapest and the one the level-A
// corpus already knows). Trav Preparations has two optional targets, so the
// recipe can always aim it at the single recipient.
var counterPlacingProbes = []string{"Battlegrowth", "Dragonscale Boon", "Travel Preparations"}

// counterProbeRecipients are the plain creatures a recipient filter is offered
// as a target, in order: Grizzly Bears (no abilities, accepted by every
// creature filter), then a Goblin, two Heroes and a Dinosaur so a type filter
// has a candidate. The trigger's own matcher decides, not this list.
var counterProbeRecipients = []string{
	"Grizzly Bears", "Goblin Piker", "Brave Brawler", "Pet Avengers",
	"Guerrilla Gorilla", "Orazca Frillback",
}

// counterProbe is the proliferate spell for the PLAN threshold recipe: it adds
// one counter of each kind a chosen recipient already has, and draws a card.
const counterProbe = "Steady Progress"

// proliferatePermanents are the permanents with a Proliferate activated
// ability the PLAN recipe can place in setup and activate on turn 1, tried in
// order. An activation never casts a spell, so a card's own spell-cast sibling
// trigger (Death to Our Enemies) does not fire before the proliferate does.
// Karn's Bastion is first: a land, so setup places it without a cast.
var proliferatePermanents = []string{"Karn's Bastion", "Contagion Engine", "Throne of Geth"}

// counterAddedRecipe builds the causes for the counter-added family. ok is
// false for every other sub-family.
func counterAddedRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (causes []triggerCause, why string, ok bool) {
	if !isCounterAddedMode(t.ModeKind()) {
		return nil, "", false
	}
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKCounterType)), "PLAN") {
		if causes, why, done := planCounterCauses(reg, name, t); done {
			if why != "" {
				return nil, why, true
			}
			return causes, "", true
		}
	}
	recipient := strings.TrimSpace(counterRecipientSpec(t))
	selfTarget := counterNamesSelf(recipient)
	if recipient == "" {
		// A kind-less mode with no recipient filter names every object; a
		// plain Bears is the safest probe.
		recipient = "Creature"
	}
	probe := ""
	if !selfTarget {
		probe = counterAcceptedProbe(reg, recipient, t, name)
		if probe == "" {
			return nil, "counter-added: no probe the recipient filter accepts", true
		}
		if probe == name {
			return nil, "counter-added: recipient probe is the card", true
		}
	}
	for _, spell := range counterPlacingProbes {
		target := "p0:" + name
		c := triggerCause{hand: []string{spell}}
		if !selfTarget {
			target = "p0:" + probe
			c.battlefield = []string{probe}
		}
		st, castable := castProbe(reg, spell, target)
		if !castable {
			continue
		}
		c.steps = []oraclegen.Step{st}
		causes = append(causes, c)
	}
	if len(causes) == 0 {
		return nil, "counter-added: no counter-placing probe in corpus", true
	}
	return causes, "", true
}

// planCounterCauses builds the PLAN-threshold causes for a CounterAdded trigger
// on the card itself: n-1 plan counters in setup, then a proliferate. Two
// causes are offered, tried in order: Steady Progress cast, then a permanent's
// Proliferate activation. done is false when the trigger is not the PLAN-self
// shape, so the caller falls back to the +1/+1 recipe.
func planCounterCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	if t.ModeKind() != cards.TriggerCounterAdded || !counterNamesSelf(t.ParamStr(cards.PKValidCard)) {
		return nil, "", false
	}
	n, ok := planCounterThreshold(t)
	if !ok {
		return nil, "counter-added: PLAN threshold is not a literal EQn", true
	}
	var causes []triggerCause
	if st, castable := castProbe(reg, counterProbe); castable {
		// The proliferate ask is posed while Steady Progress resolves. Two
		// pass_to steps reach it and answer it: the first drives the cast's
		// priority passes until the choose is pending, the second answers the
		// choose (its own step answers) and stops at the priority where the
		// counter-added trigger sits on the stack. A single `resolve` step would
		// drain the trigger and leave no snapshot showing it.
		causes = append(causes, triggerCause{
			hand:     []string{counterProbe},
			counters: planCounterSetup(n),
			steps: []oraclegen.Step{st,
				{Op: "pass_to", Decision: "choose", Seat: 0},
				{Op: "pass_to", Decision: "priority", Seat: 0, Answers: planCounterAnswer(name)}},
		})
	}
	if c, _, done := proliferateActivationCause(reg, name, n); done {
		causes = append(causes, c)
	}
	if len(causes) == 0 {
		return nil, "counter-added: no proliferate probe in corpus", true
	}
	return causes, "", true
}

// planCounterSetup seeds the source one plan counter short of the threshold.
func planCounterSetup(n int) map[string]map[string]int {
	return map[string]map[string]int{"__SOURCE__": {"PLAN": n - 1}}
}

// planCounterAnswer is the proliferate step's answer: the source card, the
// only battlefield permanent holding a counter in the PLAN recipe's setup.
func planCounterAnswer(name string) []oraclegen.Answer {
	return []oraclegen.Answer{{Kind: "choose", Pick: []string{name}}}
}

// proliferateActivationCause places a permanent with a payable Proliferate
// activated ability and activates it. done is false when no candidate is in
// the corpus with a payable cost, a resolvable XMage ability text and no
// targets. The activation resolves the same two pass_to steps the spell cause
// uses.
func proliferateActivationCause(reg *cards.Registry, name string, n int) (triggerCause, string, bool) {
	for _, probe := range proliferatePermanents {
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 || !oraclegen.XMageKnown(probe) {
			continue
		}
		f := card.Faces[0]
		prefixes, xwhy := oraclegen.XMageAbility(f)
		if xwhy != "" {
			continue
		}
		for i, sa := range f.Abilities {
			if !strings.EqualFold(strings.TrimSpace(sa.API), "Proliferate") || sa.ParamStr(cards.PKValidTgts) != "" {
				continue
			}
			if zone := sa.ParamStr(cards.PKActivationZone); zone != "" && !strings.EqualFold(zone, "Battlefield") {
				continue
			}
			mana, gap := activationCostIn(sa.ParamStr(cards.PKCost), "battlefield")
			if gap != "" {
				continue
			}
			prefix, okp := prefixes[i]
			if !okp {
				continue
			}
			idx := i
			return triggerCause{
				battlefield: []string{probe},
				counters:    planCounterSetup(n),
				steps: []oraclegen.Step{
					{Op: "activate", Seat: 0, Card: "p0:" + probe, Mana: mana, AbilityIndex: &idx},
					{Op: "pass_to", Decision: "choose", Seat: 0},
					{Op: "pass_to", Decision: "priority", Seat: 0, Answers: planCounterAnswer(name)},
				},
				xability: []string{prefix, "", ""},
			}, "", true
		}
	}
	return triggerCause{}, "", false
}

// planCounterThreshold reads the EQn of a CounterAdded CounterAmount$ ("when
// the fourth plan counter is put on this"). ok is false for any other
// comparator, a non-literal or a threshold below 2 (setup must hold at least
// one counter, and the proliferate supplies the last).
func planCounterThreshold(t *cards.Trigger) (int, bool) {
	v := strings.TrimSpace(t.ParamStr(cards.PKCounterAmount))
	upper := strings.ToUpper(v)
	if !strings.HasPrefix(upper, "EQ") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v[len("EQ"):]))
	if err != nil || n < 2 {
		return 0, false
	}
	return n, true
}

// counterRecipientSpec is the recipient filter a counter mode carries, from
// whichever parameter that mode reads: ValidCard$ for CounterAdded(Once),
// Valid$ for CounterAddedAll, ValidObject$ for CounterPlayerAddedAll and
// CounterTypeAddedAll. Empty when the trigger names no recipient (every
// object).
func counterRecipientSpec(t *cards.Trigger) string {
	for _, k := range []cards.ParamKey{cards.PKValidCard, cards.PKValid, cards.PKValidObject} {
		if v := strings.TrimSpace(t.ParamStr(k)); v != "" {
			return v
		}
	}
	return ""
}

// counterAcceptedProbe returns the first plain creature the trigger's own
// matcher accepts as the counter recipient, or "" when none does. The probe is
// placed in the battlefield zone and given the +1/+1 counter the probe spell
// would put on it, so a filter that reads counters sees the post-placement
// state.
func counterAcceptedProbe(reg *cards.Registry, spec string, t *cards.Trigger, name string) string {
	fp := newFilterProbe(spec, state.ZBattlefield)
	if kindIsP1P1(t) {
		fp.p1p1 = true
	}
	for _, cand := range counterProbeRecipients {
		if cand == name {
			continue
		}
		card, ok := reg.Lookup(cand)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		if fp.accepts(card) {
			return cand
		}
	}
	return ""
}

// isCounterAddedMode reports the five counter-placing trigger modes.
func isCounterAddedMode(m cards.TriggerMode) bool {
	switch m {
	case cards.TriggerCounterAdded, cards.TriggerCounterAddedOnce,
		cards.TriggerCounterAddedAll, cards.TriggerCounterPlayerAddedAll,
		cards.TriggerCounterTypeAddedAll:
		return true
	}
	return false
}

// kindIsP1P1 reports a trigger that wants a +1/+1 counter (or any counter, an
// absent kind), the only kind the probe spells place.
func kindIsP1P1(t *cards.Trigger) bool {
	kind := strings.TrimSpace(t.ParamStr(cards.PKCounterType))
	return kind == "" || strings.EqualFold(kind, "P1P1")
}

// counterNamesSelf reports a card filter that selects the source card itself,
// for a counter trigger's own recipient field.
func counterNamesSelf(filter string) bool {
	for _, alt := range strings.Split(filter, ",") {
		for _, tok := range strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' }) {
			if strings.EqualFold(strings.TrimSpace(tok), "Self") {
				return true
			}
		}
	}
	return false
}
