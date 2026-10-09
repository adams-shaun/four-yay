// Condition-satisfying causes for the attack, dies, drawn and etb-other
// triggers (Level B). The phase recipe already offers each trigger's
// condition prelude (condition_prelude.go); this file offers the same
// prelude for the other cause shapes and adds the causes that only an attack
// has: how many creatures attack, which attacker has what property, and which
// permanent carries counters or an Equipment.
//
// Like the prelude, nothing here interprets a filter: a variant is one cheap
// guess, and the fire probe (gorge's own trigger matcher) decides whether the
// condition held. A variant that did not help simply does not fire.
package templates

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// conditionTriggerSub reports the sub-families whose recipe is extended with
// condition variants.
func conditionTriggerSub(sub string) bool {
	switch sub {
	case "trigger.attacks", "trigger.attacks-one-target", "trigger.dies", "trigger.dies-other",
		"trigger.drawn", "trigger.etb-other", "trigger.noncombat-damage", "trigger.tapped", "trigger.blocks":
		return true
	}
	return false
}

func attackTriggerSub(sub string) bool {
	return sub == "trigger.attacks" || sub == "trigger.attacks-one-target"
}

// Probe cards for conditions a trigger names, each a plain permanent that
// setup can place (spec hypothesis H4: the host replay confirms each exists in
// XMage's database).
var (
	// bigGraveyard holds four card types, nine cards, eight of them
	// permanents: delirium, threshold and descend 8 are all true.
	bigGraveyard = []string{
		"Llanowar Elves", "Island", "Shock", "Sol Ring", "Elvish Mystic",
		"Grizzly Bears", "Nessian Asp", "Plains", "Arcane Signet",
	}
	// manyLands is eight distinct lands for "eight or more lands you control".
	manyLands = []string{
		"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes",
		"Snow-Covered Plains", "Snow-Covered Island",
	}
	// typedBoardProbes are the creature types a "you control a <type>"
	// condition names, each with probes of that type tried in order. The
	// Heroes avoid Prowess (Agent of Atlas): its trigger sits above a cast
	// cause's spell and hides what the probe is looking for.
	typedBoardProbes = []struct {
		word   string
		probes []string
	}{
		{"dinosaur", []string{"Orazca Frillback"}},
		{"hero", []string{"Brave Brawler", "Pet Avengers", "Guerrilla Gorilla"}},
	}
	// attackerTypeProbes are the types an attack trigger's ValidCard$ or
	// ValidAttackers$ filter names, each with the plain creature probe of
	// that type, in order.
	attackerTypeProbes = []struct{ word, probe string }{
		{"spider", "Giant Spider"},
		{"wolf", "Young Wolf"},
	}
	// powerProbe is a 4-power creature, and menaceProbe a creature with menace.
	powerProbe, menaceProbe = "Nessian Asp", "Boggart Brute"
	// equipProbe is the Equipment attached to the attacker.
	equipProbe = "Bonesplitter"
	// suspectProbe is the creature whose own enter trigger suspects it, cast
	// by a "suspected creature attacks" cause.
	suspectProbe = "Frantic Scapegoat"
	// attackFillers are the plain attackers added to reach an attacker count.
	attackFillers = []string{"Grizzly Bears", "Llanowar Elves", "Elvish Mystic", "Nessian Asp"}
)

// existingCards keeps the names the corpus has.
func existingCards(reg *cards.Registry, names []string) []string {
	var out []string
	for _, n := range names {
		if _, ok := reg.Lookup(n); ok {
			out = append(out, n)
		}
	}
	return out
}

// attackerTypeProbe is the plain creature of the named type the attack filters
// probe, "" when the table has none.
func attackerTypeProbe(word string) string {
	for _, tp := range attackerTypeProbes {
		if tp.word == word {
			return tp.probe
		}
	}
	return ""
}

// etbDiscardFiller is the card to put first in p0's hand when the card under
// test, placed on the battlefield by setup, enters with a connive or discard
// of its own: the runner resolves that trigger at setup and discards the first
// card in hand, which would otherwise be the cause's probe. It returns "" when
// the card has no such trigger.
func etbDiscardFiller(f *cards.Face) string {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if !strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidCard)), "card.self") || !strings.EqualFold(t.ParamStr(cards.PKDestination), "Battlefield") {
			continue
		}
		body := strings.ToLower(f.SVars[t.ParamStr(cards.PKExecute)])
		if strings.Contains(body, "db$ connive") || strings.Contains(body, "db$ discard") {
			return "Wastes"
		}
	}
	return ""
}

// triggerRecipe is baseTriggerRecipe plus, for the condition sub-families,
// variants of each base cause that make the trigger's own condition true.
func triggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) ([]triggerCause, string) {
	causes, why := baseTriggerRecipe(reg, f, name, t, sub)
	if why != "" {
		return causes, why
	}
	if conditionTriggerSub(sub) {
		causes = append(causes, conditionCauses(reg, f, name, t, sub, causes)...)
	}
	// A self-attribute gate the setup-placed source cannot show (prepared
	// consumed by the turn-1 upkeep firing, a suspect grant dropped with the
	// setup's ETB) gets a cast-self variant of every cause so far.
	selfCast := selfCastGateCauses(reg, f, name, t, causes)
	causes = append(causes, selfCast...)
	// A ClassBand$ trigger's granted body is live only from its level on, so
	// every cause first raises the Class to that level. Prepending after the
	// condition variants keeps the sorcery-speed level-up ahead of any
	// condition prelude that might leave turn 1's first main phase.
	return classLevelBandCausePrepends(f, name, t, sub, causes), ""
}

var (
	attackersAmountRE = regexp.MustCompile(`validattackersamount ge(\d+)`)
	counterFilterRE   = regexp.MustCompile(`counters_ge(\d+)_([a-z0-9]+)`)
)

// conditionCauses derives variants of base. A shape variant (attackers,
// counters, Equipment) comes first, bare and then with each prelude; with no
// shape the preludes apply to the base cause itself.
func conditionCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string, base []triggerCause) []triggerCause {
	text := strings.ToLower(strings.Join(conditionText(t.Params, f.SVars), " "))
	var preludes []conditionPrelude
	for _, p := range append(conditionPreludes(reg, t.Params, f.SVars), triggerConditionFixtures(reg, f, t)...) {
		if !preludeAttacks(p) {
			preludes = append(preludes, p)
		}
	}
	var out []triggerCause
	for _, b := range base {
		starts := []triggerCause{b}
		if shaped, ok := conditionShape(reg, f, name, t, sub, text, b); ok {
			out = append(out, shaped)
			starts = []triggerCause{shaped}
		}
		for _, s := range starts {
			for _, p := range preludes {
				out = append(out, applyPrelude(s, p))
			}
		}
	}
	return out
}

// preludeAttacks reports a prelude that itself attacks: it would fire the
// attack trigger early, so only the phase recipe uses it.
func preludeAttacks(p conditionPrelude) bool {
	for _, st := range p.steps {
		if st.Op == "attack" {
			return true
		}
	}
	return false
}

// applyPrelude layers one condition prelude over a cause. The prelude's steps
// run before the cause's. When the merged setup carries a name twice and a
// cause step references it (an attack step's attacker, a cast target), the
// base's own copy is dropped: the prelude's serves both the count and the
// ref, and two distinct offered objects for one emitted pick is exactly the
// same-name ambiguity the answer census measures.
func applyPrelude(base triggerCause, p conditionPrelude) triggerCause {
	c := base
	c.hand = append(append([]string(nil), base.hand...), p.hand...)
	c.battlefield = append(append([]string(nil), base.battlefield...), p.battlefield...)
	c.battlefield = dropRedundantSetup(base.steps, c.battlefield)
	c.tapped = append(append([]string(nil), base.tapped...), p.tapped...)
	c.graveyard = append(append([]string(nil), base.graveyard...), p.graveyard...)
	c.opponentHand = append(append([]string(nil), base.opponentHand...), p.opponentHand...)
	c.opponentBattlefield = append(append([]string(nil), base.opponentBattlefield...), p.opponentBattlefield...)
	c.counters = mergeCounters(base.counters, p.counters)
	c.prelude = append(append([]oraclegen.Step(nil), base.prelude...), p.steps...)
	c.preludeXAbility = append(append([]string(nil), base.preludeXAbility...), p.xability...)
	if p.activationCost != "" {
		c.preludeActivationCost = p.activationCost
	}
	return c
}

// dropRedundantSetup removes one copy of each name the merged setup carries
// twice while a cause step's card reference names it, keeping the first (the
// prelude's, whose count the gate reads).
func dropRedundantSetup(steps []oraclegen.Step, merged []string) []string {
	refs := []string(nil)
	for _, st := range steps {
		refs = append(refs, st.Attackers...)
		refs = append(refs, st.Targets...)
		refs = append(refs, st.Card)
	}
	for _, r := range refs {
		name := r
		if i := strings.IndexByte(r, ':'); i >= 0 {
			name = r[i+1:]
		}
		if name == "" || name == r {
			continue
		}
		have := 0
		for _, n := range merged {
			if n == name {
				have++
			}
		}
		if have < 2 {
			continue
		}
		for i, n := range merged {
			if n == name {
				merged = append(merged[:i:i], merged[i+1:]...)
				break
			}
		}
	}
	return merged
}

func mergeCounters(a, b map[string]map[string]int) map[string]map[string]int {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := map[string]map[string]int{}
	for _, m := range []map[string]map[string]int{a, b} {
		for card, kinds := range m {
			if out[card] == nil {
				out[card] = map[string]int{}
			}
			for kind, n := range kinds {
				out[card][kind] += n
			}
		}
	}
	return out
}

// conditionShape applies every shape the trigger's text asks for to base, or
// reports false when it asks for none.
func conditionShape(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub, text string, base triggerCause) (triggerCause, bool) {
	c := base
	c.battlefield = append([]string(nil), base.battlefield...)
	c.steps = append([]oraclegen.Step(nil), base.steps...)
	c.prelude = append([]oraclegen.Step(nil), base.prelude...)
	changed := false
	creature := f.IsCreature()
	// The object a counter or an Equipment goes on: the attacker, or the
	// creature that dies (the card for dies, the Bears the probe kills for
	// dies-other).
	subject := bearsProbe
	if creature && sub != "trigger.dies-other" {
		subject = name
	}
	counterText := text
	if kind := counterKind(text); kind != "" && kind != "P1P1" && kind != "M1M1" {
		// Any other counter kind is the trigger's own filter, not an SVar
		// elsewhere on the card (Eluge's Y counts flood counters).
		counterText = strings.ToLower(strings.Join(conditionText(t.Params, nil), " "))
	}
	if kind := counterKind(counterText); kind != "" {
		key := subject
		if subject == name {
			key = "__SOURCE__"
		} else {
			c.battlefield = appendFixtureUnique(c.battlefield, subject)
		}
		c.counters = mergeCounters(base.counters, map[string]map[string]int{key: {kind: counterAmount(counterText)}})
		changed = true
	}
	if attackTriggerSub(sub) {
		if attackShape(reg, f, name, t, text, &c, subject) {
			changed = true
		}
	}
	return c, changed
}

// counterKind is the counter a "with a counter" filter wants on its subject.
func counterKind(text string) string {
	if strings.Contains(text, "hascounters") {
		return "P1P1"
	}
	m := counterFilterRE.FindStringSubmatch(text)
	if m != nil {
		return strings.ToUpper(m[2])
	}
	return ""
}

func counterAmount(text string) int {
	m := counterFilterRE.FindStringSubmatch(text)
	if m == nil {
		return 1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// attackShape widens c's attack step to satisfy attacker-count and
// attacker-property conditions, and attaches an Equipment to the attacker for
// an "equipped" one. It reports whether it changed anything.
func attackShape(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, text string, c *triggerCause, subject string) bool {
	at := -1
	for i, st := range c.steps {
		if st.Op == "attack" {
			at = i
		}
	}
	if at < 0 {
		return false
	}
	atk := append([]string(nil), c.steps[at].Attackers...)
	changed := false
	add := func(card string) {
		if _, ok := reg.Lookup(card); !ok || card == name {
			return
		}
		c.battlefield = appendFixtureUnique(c.battlefield, card)
		atk = appendFixtureUnique(atk, "p0:"+card)
		changed = true
	}
	filter := strings.ToLower(t.ParamStr(cards.PKValidCard) + " " + t.ParamStr(cards.PKValidAttackers))
	if strings.Contains(filter, "powerge4") {
		add(powerProbe)
	}
	for _, card := range powerAttackers(attackPowerNeeded(t, text)) {
		add(card)
	}
	if strings.Contains(filter, "withmenace") {
		add(menaceProbe)
	}
	if strings.Contains(text, "attacking+other") {
		add(attackFillers[0])
	}
	// A typed attacker: the trigger's filter names a creature type the table
	// probes (Tolsimir, Midnight's Light's "a Wolf you control attacks").
	filterLower := strings.ToLower(filter)
	for _, tp := range attackerTypeProbes {
		if strings.Contains(filterLower, tp.word) {
			add(tp.probe)
		}
	}
	// A suspected attacker: the suspect comes from Frantic Scapegoat's own
	// enter trigger, cast here rather than placed, because setup placement
	// drops enter triggers and a placed copy is never suspected. The
	// scapegoat then joins the attack step.
	if strings.Contains(filterLower, "issuspected") {
		if st, ok := castProbe(reg, suspectProbe); ok {
			c.hand = appendFixtureUnique(c.hand, suspectProbe)
			c.prelude = append(c.prelude, st, oraclegen.Step{Op: "resolve"})
			atk = appendFixtureUnique(atk, "p0:"+suspectProbe)
			changed = true
		}
	}
	if m := attackersAmountRE.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		typed := attackerTypeProbe(strings.ToLower(t.ParamStr(cards.PKValidAttackers)))
		pool := attackFillers
		if typed != "" {
			pool = []string{typed}
		}
		have := len(atk)
		if typed != "" {
			// Only attackers of the type count, and the card itself is one
			// when it has the type.
			have = 0
			for _, ty := range f.Types {
				if strings.EqualFold(ty, t.ParamStr(cards.PKValidAttackers)) {
					have = 1
				}
			}
		}
		for _, card := range pool {
			if have >= n {
				break
			}
			before := len(atk)
			add(card)
			if len(atk) > before {
				have++
			}
		}
	}
	if strings.Contains(filter, "equipped") || strings.Contains(text, "card.self+equipped") {
		c.battlefield = appendFixtureUnique(c.battlefield, equipProbe)
		c.prelude = append(c.prelude, oraclegen.Step{Op: "attach", Seat: 0, Card: "p0:" + equipProbe, AttachedTo: "p0:" + subject})
		changed = true
	}
	c.steps[at].Attackers = atk
	return changed
}
