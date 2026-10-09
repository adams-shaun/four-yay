package templates

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
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
	// opponent* are p1's setup, which only a trigger gate that compares the
	// opponent's hand or lands with p0's offers.
	opponentHand        []string
	opponentBattlefield []string
	// xability is parallel to steps: the XMage rule-text prefix of a prelude
	// activate step (a Class level-up), "" on every other prelude step. It
	// lets the activate and trigger templates label the prelude's activations
	// for XMage exactly as they label their own.
	xability []string
	// life is p0's setup life total when nonzero: an activation gated on
	// "at least N life" (Count$YourLifeTotal against a literal or a
	// starting-life offset) sets it rather than gaining the life by a cast.
	life int32
	// solvedCase marks a prelude whose steps end with the solve sequence
	// (pass to the end step, resolve the "To solve" trigger): the phase
	// recipe also emits the pass_to that waits for the row trigger's own p0
	// phase, which a plain prelude must not move.
	solvedCase bool
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
			if contains("presentcompare ge8") {
				add(conditionPrelude{battlefield: existingCards(reg, manyLands)})
			}
		}
		if contains("token+youctrl", "permanent.token") {
			if steps := resolvedCast("Raise the Alarm"); len(steps) > 0 {
				add(conditionPrelude{hand: []string{"Raise the Alarm"}, steps: steps})
			}
		}
		for _, tp := range typedBoardProbes {
			if contains(tp.word + ".youctrl") {
				for _, probe := range existingCards(reg, tp.probes) {
					add(conditionPrelude{battlefield: []string{probe}})
				}
			}
		}
		if contains("creature.youctrl") {
			add(conditionPrelude{battlefield: []string{"Grizzly Bears", "Llanowar Elves", "Nessian Asp", "Elvish Mystic"}})
		}
	}
	// Graveyard and delirium gates are offered a nonempty graveyard; broad
	// card type requirements are covered by a creature card fixture.
	if contains("validgraveyard", "presentzone$ graveyard", "delirium", "threshold") {
		add(conditionPrelude{graveyard: []string{"Llanowar Elves", "Island", "Shock", "Sol Ring"}})
	}
	// Four card types (delirium), seven cards (threshold) and eight permanent
	// cards (descend 8) in one graveyard, distinct so that setup keeps each.
	if contains("delirium", "threshold", "permanent.youown", "validgraveyard") {
		add(conditionPrelude{graveyard: bigGraveyard})
	}
	if contains("lesson.youown") {
		add(conditionPrelude{graveyard: []string{"Environmental Sciences"}})
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
		if contains("count_ge2", conditionParamText("SVarCompare", "GE2"), "celebration", "thisturnentered_battlefield_creature.youctrl", "thisturnentered_battlefield_permanent.nonland") {
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
		if contains(conditionParamText("SVarCompare", "GE3"), "count$youdrewthisturn/", "count$youdrewthisturn") {
			if a, ok := cast("Divination"); ok {
				if b, ok := cast("Concentrate"); ok {
					add(conditionPrelude{hand: []string{"Divination", "Concentrate"}, steps: []oraclegen.Step{a, {Op: "resolve"}, b, {Op: "resolve"}}})
				}
			}
		}
	}
	if contains("thisturncast_card.cmcge4") {
		if steps := resolvedCast("Angel's Mercy"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Angel's Mercy"}, steps: steps})
		}
	}
	if contains("count$thisturncast", "count$void") {
		if steps := resolvedCast("Grizzly Bears"); len(steps) > 0 {
			add(conditionPrelude{hand: []string{"Grizzly Bears"}, steps: steps})
		}
		if contains("count$thisturncast_card.noncreature", "count$thisturncast_instant", "count$thisturncast_sorcery", conditionParamText("SVarCompare", "GE2")) {
			if a, ok := cast("Shock"); ok {
				if b, ok := cast("Lightning Bolt"); ok {
					// Distinct names: a second cast of one name is an
					// ambiguous reference to the first.
					add(conditionPrelude{hand: []string{"Shock", "Lightning Bolt"}, steps: []oraclegen.Step{a, {Op: "resolve"}, b, {Op: "resolve"}}})
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
	if contains("sacrificedthisturn") {
		if prelude, ok := sacrificeConditionPrelude(reg); ok {
			add(prelude)
		}
	}
	if contains("revolt", "thisturnentered_graveyard_from_battlefield_creature", "morbid", "count$void") {
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

// triggerConditionSkip names the known gate class when no offered fixture made
// it true. This keeps an unavailable setup condition distinct from a trigger
// that had no recognized condition at all.
func triggerConditionSkip(t *cards.Trigger, svars map[string]string) string {
	for _, spec := range []string{t.ParamStr(cards.PKIsPresent), t.ParamStr(cards.PKIsPresent2)} {
		if unknown := effects.UnknownPredicates(spec); len(unknown) > 0 {
			return "condition: engine predicate unread (" + strings.Join(unknown, ",") + ")"
		}
	}
	if solvedCaseCondition(t) {
		return "condition: needs a solved Case"
	}
	check := t.ParamStr(cards.PKCheckSVar)
	if check != "" {
		body, hasBody := svars[check]
		if !hasBody {
			body = check
		}
		if strings.Contains(strings.ToLower(body), "validself") {
			if unknown := effects.UnknownPredicates(body); len(unknown) > 0 {
				return "condition: engine predicate unread (" + strings.Join(unknown, ",") + ")"
			}
		}
		label := check
		if hasBody {
			head := strings.Fields(body)
			if len(head) > 0 {
				label = strings.SplitN(head[0], ".", 2)[0]
			}
		}
		lowerBody := strings.ToLower(body)
		switch {
		case strings.Contains(lowerBody, "count$") && strings.Contains(lowerBody, "validgraveyard"), strings.Contains(lowerBody, "count$validgraveyard"):
			return "condition: graveyard contents (" + label + ")"
		case strings.Contains(lowerBody, "count$") && (strings.Contains(lowerBody, "valid ") || strings.Contains(lowerBody, "valid$")):
			return "condition: board count (" + label + ")"
		case strings.Contains(lowerBody, "count$"), strings.Contains(lowerBody, "playercountproperty"), strings.Contains(lowerBody, "playercountopponents"):
			if gap := historyNamedGap(lowerBody); gap != "" {
				return "condition: turn history, " + gap + " (" + label + ")"
			}
			return "condition: turn history (" + label + ")"
		default:
			return "condition: SVar gate (" + label + ")"
		}
	}
	if t.ParamStr(cards.PKRevolt) != "" {
		return "condition: revolt history"
	}
	if t.ParamStr(cards.PKDelirium) != "" || t.ParamStr(cards.PKThreshold) != "" {
		return "condition: graveyard contents"
	}
	if t.ParamStr(cards.PKValidAttackersAmount) != "" {
		return "condition: attacker count"
	}
	if t.ParamStr(cards.PKNumber) != "" {
		return "condition: nth draw"
	}
	present := t.ParamStr(cards.PKIsPresent)
	if present == "" {
		present = t.ParamStr(cards.PKIsPresent2)
	}
	if present != "" {
		if strings.Contains(strings.ToLower(present), "card.self") || strings.EqualFold(t.ParamStr(cards.PKPresentDefined), "Self") {
			return "condition: self state"
		}
		return "condition: board presence"
	}
	if valid := strings.ToLower(t.ParamStr(cards.PKValidCard) + " " + t.ParamStr(cards.PKValidAttackers)); strings.Contains(valid, "counters_") || strings.Contains(valid, "hascounters") {
		return "condition: counters"
	} else if strings.Contains(valid, "equipped") || strings.Contains(valid, "issuspected") || strings.Contains(valid, "powerge") || strings.Contains(valid, "withmenace") {
		return "condition: attacker property"
	}
	return ""
}

// sacrificeConditionPrelude gives the engine an actual sacrifice event. The
// Bears are distinct from the trigger source, and the explicit choice prevents
// the deterministic fallback from sacrificing the source instead.
func sacrificeConditionPrelude(reg *cards.Registry) (conditionPrelude, bool) {
	return sacrificeConditionPreludeOf(reg, "Grizzly Bears")
}

// sacrificeConditionPreludeOf is sacrificeConditionPrelude with the sacrificed
// fixture named: a count that filters its sacrifices by type needs a fixture
// whose printed types cover the spec (cost_svar_amount.go's artifact read).
func sacrificeConditionPreludeOf(reg *cards.Registry, sacrificee string) (conditionPrelude, bool) {
	cast, ok := castProbe(reg, "Village Rites")
	if !ok {
		return conditionPrelude{}, false
	}
	cast.Answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{sacrificee}}}
	return conditionPrelude{
		hand:        []string{"Village Rites"},
		battlefield: []string{sacrificee},
		steps:       []oraclegen.Step{cast, {Op: "resolve"}},
	}, true
}

// scriptPreludeSacrifice carries a prelude step's scripted sacrifice choice to
// XMage. Gorge's own decision log drops a sole sacrifice candidate as a forced
// ask, but XMage still poses the TargetControlledPermanent ask for it, so the
// prelude's explicit pick is exported as the step's choice answer (the shape
// the activation Sac cost path records). The prelude opens the scenario, so a
// prelude step index is the scenario step index.
func scriptPreludeSacrifice(xa [][]oraclegen.XAnswer, prelude []oraclegen.Step, steps int) [][]oraclegen.XAnswer {
	for i, st := range prelude {
		if i >= steps {
			break
		}
		for _, a := range st.Answers {
			if a.Kind != "choose" || len(a.Pick) != 1 {
				continue
			}
			if xa == nil {
				xa = make([][]oraclegen.XAnswer, steps)
			}
			have := false
			for _, x := range xa[i] {
				have = have || x.Kind == "choice" && x.Value == a.Pick[0]
			}
			if !have {
				xa[i] = append(xa[i], oraclegen.XAnswer{Seat: st.Seat, Kind: "choice", Value: a.Pick[0]})
			}
		}
	}
	return xa
}

// conditionParamText is shared by the emitted text and parameter needles.
func conditionParamText(key, value string) string {
	return key + " " + value
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
		out = append(out, conditionParamText(key, value))
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
