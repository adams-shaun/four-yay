package templates

import (
	"sort"
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
			// Setup-tapped permanents untap before the phase checkpoint. Attack
			// after the untap step so these creatures remain tapped at end step.
			add(conditionPrelude{battlefield: []string{"Grizzly Bears", "Llanowar Elves"}, steps: []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Grizzly Bears", "p0:Llanowar Elves"}}, {Op: "pass_to", Step: "main2"}}})
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
	// Entered-this-turn predicates need actual cast/resolve events: setup
	// permanents are deliberately not counted as having entered this turn.
	if contains("count$thisturnentered_battlefield") {
		card := "Grizzly Bears"
		if contains("_land", "land.youctrl") {
			card = "Plains"
		} else if contains("_artifact", "artifact.youctrl") {
			card = "Sol Ring"
		}
		if steps := resolvedCast(card); len(steps) > 0 {
			add(conditionPrelude{hand: []string{card}, steps: steps})
		}
		if contains("count_ge2", "svarcompare:ge2", "celebration", "thisturnentered_battlefield_creature.youctrl", "thisturnentered_battlefield_permanent.nonland") {
			card2 := "Llanowar Elves"
			if card == "Plains" {
				card2 = "Island"
			}
			if card == "Sol Ring" {
				card2 = "Arcane Signet"
			}
			if a, ok := cast(card); ok {
				if b, ok := cast(card2); ok {
					add(conditionPrelude{hand: []string{card, card2}, steps: []oraclegen.Step{a, {Op: "resolve"}, b, {Op: "resolve"}}})
				}
			}
		}
	}
	if contains("count$youdrewthisturn") {
		if steps := resolvedCast("Divination"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Divination"}, steps: steps})
		}
		if contains("svarcompare:ge3", "count$youdrewthisturn/", "count$youdrewthisturn") {
			if a, ok := cast("Divination"); ok {
				if b, ok := cast("Concentrate"); ok {
					add(conditionPrelude{hand: []string{"Divination", "Concentrate"}, steps: []oraclegen.Step{a, {Op: "resolve"}, b, {Op: "resolve"}}})
				}
			}
		}
	}
	if contains("count$thisturncast", "count$void") {
		if steps := resolvedCast("Grizzly Bears"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Grizzly Bears"}, steps: steps})
		}
		if contains("count$thisturncast_card.noncreature", "count$thisturncast_instant", "count$thisturncast_sorcery", "svarcompare:ge2") {
			if a, ok := cast("Shock"); ok {
				if b, ok := cast("Shock"); ok {
					add(conditionPrelude{hand: []string{"Shock", "Shock"}, steps: []oraclegen.Step{a, {Op: "resolve"}, b, {Op: "resolve"}}})
				}
			}
		}
		if contains("creature.youctrl/limitmax.1", "card.noncreature+youctrl/limitmax.1") {
			if a, ok := cast("Grizzly Bears"); ok {
				if b, ok := cast("Shock"); ok {
					add(conditionPrelude{hand: []string{"Grizzly Bears", "Shock"}, steps: []oraclegen.Step{a, {Op: "resolve"}, b, {Op: "resolve"}}})
				}
			}
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
			counterCards := make([]string, 0, len(candidate.counters))
			for card := range candidate.counters {
				counterCards = append(counterCards, card)
			}
			sort.Strings(counterCards)
			for _, card := range counterCards {
				kinds := candidate.counters[card]
				if combined.counters == nil {
					combined.counters = map[string]map[string]int{}
				}
				if combined.counters[card] == nil {
					combined.counters[card] = map[string]int{}
				}
				counterKinds := make([]string, 0, len(kinds))
				for kind := range kinds {
					counterKinds = append(counterKinds, kind)
				}
				sort.Strings(counterKinds)
				for _, kind := range counterKinds {
					combined.counters[card][kind] += kinds[kind]
				}
			}
		}
		out = append(out, combined)
	}
	return out
}

// phaseConditionSkip names the known gate class when no offered fixture made
// it true. This keeps an unavailable setup condition distinct from a trigger
// that had no recognized condition at all.
func phaseConditionSkip(t *cards.Trigger, svars map[string]string) string {
	check := t.ParamStr(cards.PKCheckSVar)
	if check != "" {
		body := strings.ToLower(svars[check])
		switch {
		case strings.Contains(body, "count$") && strings.Contains(body, "validgraveyard"), strings.Contains(body, "count$validgraveyard"):
			return "condition: graveyard contents (" + check + ")"
		case strings.Contains(body, "count$") && (strings.Contains(body, "valid ") || strings.Contains(body, "valid$")):
			return "condition: board count (" + check + ")"
		case strings.Contains(body, "count$"), strings.Contains(body, "playercountproperty"), strings.Contains(body, "playercountopponents"):
			return "condition: turn history (" + check + ")"
		default:
			return "condition: SVar gate (" + check + ")"
		}
	}
	if t.ParamStr(cards.PKRevolt) != "" {
		return "condition: revolt history"
	}
	present := t.ParamStr(cards.PKIsPresent)
	if present == "" {
		present = t.ParamStr(cards.PKIsPresent2)
	}
	if present != "" {
		if strings.Contains(strings.ToLower(present), "card.self") {
			return "condition: self state"
		}
		return "condition: board presence"
	}
	return ""
}

func conditionText(params, svars map[string]string) []string {
	out := make([]string, 0, len(params)+len(svars))
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := params[key]
		out = append(out, key+" "+value)
		if sv, ok := svars[value]; ok {
			out = append(out, sv)
		}
	}
	keys = keys[:0]
	for key := range svars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, svars[key])
	}
	return out
}
