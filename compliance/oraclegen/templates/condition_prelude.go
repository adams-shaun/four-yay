package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// conditionPrelude is one deliberately cheap way to make a conditional
// trigger's gate plausible. The fire probe, not this classifier, decides
// whether the condition actually held.
type conditionPrelude struct {
	hand        []string
	battlefield []string
	tapped      []string
	graveyard   []string
	counters    map[string]map[string]int
	steps       []oraclegen.Step
}

// conditionPreludes offers condition setup candidates in stable order. It
// classifies only parameter/SVar text; the rules engine remains authoritative.
func conditionPreludes(reg *cards.Registry, params, svars map[string]string) []conditionPrelude {
	text := strings.ToLower(strings.Join(conditionText(params, svars), " "))
	var out []conditionPrelude
	add := func(c conditionPrelude) { out = append(out, c) }
	contains := func(xs ...string) bool {
		for _, x := range xs {
			if strings.Contains(text, strings.ToLower(x)) {
				return true
			}
		}
		return false
	}
	cast := func(card string) (oraclegen.Step, bool) { return castProbe(reg, card) }
	resolvedCast := func(card string) []oraclegen.Step {
		st, ok := cast(card)
		if !ok {
			return nil
		}
		return []oraclegen.Step{st, {Op: "resolve"}}
	}
	// Board predicates: use ordinary permanents that can legally be placed
	// during setup, and let the trigger matcher test counts/power/types.
	if contains("ispresent", "count$valid creature.youctrl", "count$valid artifact.youctrl", "count$valid land.youctrl", "count$valid town.youctrl") {
		if contains("equipment.youctrl") {
			add(conditionPrelude{battlefield: []string{"Bonesplitter"}})
		}
		if contains("town.youctrl") {
			add(conditionPrelude{battlefield: []string{"Town of Orazca", "Plains", "Island", "Swamp", "Mountain"}})
		}
		if contains("tapped") {
			add(conditionPrelude{battlefield: []string{"Grizzly Bears", "Llanowar Elves"}, tapped: []string{"__SOURCE__", "Grizzly Bears"}})
		}
		if contains("powerge4", "creature.youctrl ge", "count$valid creature.youctrl") {
			add(conditionPrelude{battlefield: []string{"Nessian Asp", "Grizzly Bears", "Llanowar Elves"}})
		}
		if contains("artifact.youctrl") {
			add(conditionPrelude{battlefield: []string{"Sol Ring", "Arcane Signet"}})
		}
		if contains("land.youctrl") {
			add(conditionPrelude{battlefield: []string{"Plains", "Island", "Swamp", "Mountain", "Forest"}})
		}
		if contains("creature.youctrl") {
			add(conditionPrelude{battlefield: []string{"Grizzly Bears", "Llanowar Elves", "Nessian Asp", "Elvish Mystic"}})
		}
	}
	// Graveyard and delirium gates are offered a nonempty graveyard; broad
	// card type requirements are covered by a creature card fixture.
	if contains("validgraveyard", "presentzone$ graveyard", "delirium") {
		add(conditionPrelude{graveyard: []string{"Llanowar Elves", "Island", "Shock", "Sol Ring"}})
	}
	// Turn-history gates. Each line is a real cast/resolve prelude, before
	// the phase checkpoint; a failed candidate is simply discarded.
	if contains("count$lifeyougainedthisturn") {
		if steps := resolvedCast("Angel's Mercy"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Angel's Mercy"}, steps: steps})
		}
	}
	if contains("count$youdrewthisturn") {
		if steps := resolvedCast("Divination"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Divination"}, steps: steps})
		}
	}
	if contains("count$thisturncast", "count$void") {
		if steps := resolvedCast("Grizzly Bears"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Grizzly Bears"}, steps: steps})
		}
	}
	if contains("count$attackersdeclared") {
		add(conditionPrelude{battlefield: []string{"Grizzly Bears"}, steps: []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Grizzly Bears"}}, {Op: "pass_to", Step: "main2"}}})
	}
	if contains("revolt", "sacrificedthisturn", "thisturnentered_graveyard_from_battlefield_creature", "morbid") {
		steps := resolvedCast("Grizzly Bears")
		if len(steps) > 0 {
			if destroy, ok := castProbe(reg, "Murder", "p0:Grizzly Bears"); ok {
				steps = append(steps, destroy, oraclegen.Step{Op: "resolve"})
				add(conditionPrelude{hand: []string{"Grizzly Bears", "Murder"}, steps: steps})
			}
		}
	}
	// Self-state gates with direct setup representations.
	if contains("card.self+tapped", "self+tapped") {
		add(conditionPrelude{tapped: []string{"__SOURCE__"}})
	}
	if strings.Contains(text, "counters_ge1_m1m1") {
		add(conditionPrelude{counters: map[string]map[string]int{"__SOURCE__": {"M1M1": 1}}})
	}
	if len(out) > 1 {
		combined := conditionPrelude{}
		for _, candidate := range out {
			combined.hand = append(combined.hand, candidate.hand...)
			combined.battlefield = append(combined.battlefield, candidate.battlefield...)
			combined.tapped = append(combined.tapped, candidate.tapped...)
			combined.graveyard = append(combined.graveyard, candidate.graveyard...)
			combined.steps = append(combined.steps, candidate.steps...)
			for card, kinds := range candidate.counters {
				if combined.counters == nil {
					combined.counters = map[string]map[string]int{}
				}
				if combined.counters[card] == nil {
					combined.counters[card] = map[string]int{}
				}
				for kind, count := range kinds {
					combined.counters[card][kind] += count
				}
			}
		}
		out = append(out, combined)
	}
	return out
}

func conditionText(params, svars map[string]string) []string {
	out := make([]string, 0, len(params)+len(svars))
	for key, value := range params {
		out = append(out, key+" "+value)
		if sv, ok := svars[value]; ok {
			out = append(out, sv)
		}
	}
	for _, value := range svars {
		out = append(out, value)
	}
	return out
}
