package templates

import (
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
)

// legendaryCreatures are cheap, inert legendary creatures, each a distinct name
// (the legend rule would put a second copy in the graveyard). A "legendary
// creature you control" count takes them in order.
var legendaryCreatures = []string{"Isamaru, Hound of Konda", "Squee, Goblin Nabob", "Kamahl, Pit Fighter", "Mirri, Cat Warrior"}

// opponentHandSizes and opponentLandSizes are the sizes tried for a trigger
// that compares the opponent's hand or lands with p0's own (which setup leaves
// empty, bar the cause's probe).
var (
	opponentHandSizes = []int{2, 4}
	opponentLandSizes = []int{1, 3}
)

var namedCardRE = regexp.MustCompile(`named([^+]+)`)

// triggerConditionFixtures builds board and count candidates from the trigger's
// own predicates. The activation helpers do the filter/count interpretation;
// the trigger fire probe remains the authority on whether a candidate works.
func triggerConditionFixtures(reg *cards.Registry, f *cards.Face, t *cards.Trigger) []conditionPrelude {
	var out []conditionPrelude
	zone := t.ParamStr(cards.PKPresentZone)
	compare := t.ParamStr(cards.PKPresentCompare)
	selfDefined := strings.EqualFold(t.ParamStr(cards.PKPresentDefined), "Self")
	for _, key := range []cards.ParamKey{cards.PKIsPresent, cards.PKIsPresent2} {
		spec := t.ParamStr(key)
		if strings.TrimSpace(spec) == "" {
			continue
		}
		if unknown := effects.UnknownPredicates(spec); len(unknown) > 0 {
			continue
		}
		var rest []string
		for _, group := range strings.Split(spec, ",") {
			if cands, ok := groupFixtures(reg, f, group, zone, staticCountFrom(compare), selfDefined); ok {
				out = append(out, cands...)
				continue
			}
			rest = append(rest, group)
		}
		if len(rest) > 0 {
			built, _ := activationPresentPrelude(reg, strings.Join(rest, ","), zone, compare)
			out = append(out, built...)
		}
	}
	if check := t.ParamStr(cards.PKCheckSVar); check != "" {
		built, _ := activationSVarPrelude(reg, f, check, t.ParamStr(cards.PKSVarCompare))
		out = append(out, built...)
		out = append(out, opponentComparisonFixtures(f, check, t.ParamStr(cards.PKSVarCompare))...)
	}
	return out
}

// groupFixtures builds the setup for one IsPresent alternative whose state the
// shared activation helper cannot give: the source's own tapped state or
// counters, a card named by the filter, or a count of legendary creatures. ok
// reports that the alternative is handled here, even with no candidate.
func groupFixtures(reg *cards.Registry, f *cards.Face, group, zone string, n int, selfDefined bool) ([]conditionPrelude, bool) {
	words := affectedWords(group)
	lower := strings.ToLower(group)
	if selfDefined || hasWord(words, "Self") {
		var out []conditionPrelude
		if hasWord(words, "tapped") && f.IsCreature() {
			// A setup-tapped permanent untaps before the checkpoint: the
			// source attacks and stays tapped through main2.
			out = append(out, conditionPrelude{steps: []oraclegen.Step{
				{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + f.Name}},
				{Op: "pass_to", Step: "main2"},
			}})
		}
		if m := counterFilterRE.FindStringSubmatch(lower); m != nil {
			out = append(out, conditionPrelude{counters: map[string]map[string]int{"__SOURCE__": {strings.ToUpper(m[2]): counterAmount(lower)}}})
		}
		return out, len(out) > 0
	}
	if m := namedCardRE.FindStringSubmatch(group); m != nil {
		card := strings.TrimSpace(strings.ReplaceAll(m[1], ";", ","))
		if _, ok := reg.Lookup(card); !ok || staticOpposing(group) {
			return nil, false
		}
		return []conditionPrelude{{battlefield: []string{card}}}, true
	}
	if hasWord(words, "Legendary") && hasWord(words, "Creature") && !staticOpposing(group) && (zone == "" || strings.EqualFold(zone, "Battlefield")) {
		pool := existingCards(reg, legendaryCreatures)
		if n < 1 {
			n = 1
		}
		if n > len(pool) {
			return nil, true
		}
		return []conditionPrelude{{battlefield: pool[:n]}}, true
	}
	return nil, false
}

// opponentComparisonFixtures builds the opponent-side setup an "an opponent has
// more cards in hand / controls more lands than you" gate needs. The check
// SVar, or the SVar the compare names (GTCardsYou), counts the opponent.
func opponentComparisonFixtures(f *cards.Face, check, compare string) []conditionPrelude {
	bodies := []string{svarBody(f, check)}
	if len(compare) > 2 {
		bodies = append(bodies, svarBody(f, compare[2:]))
	}
	var out []conditionPrelude
	for _, body := range bodies {
		lower := strings.ToLower(body)
		if strings.Contains(lower, "playercountopponents$highestcardsinhand") {
			for _, k := range opponentHandSizes {
				out = append(out, conditionPrelude{opponentHand: oraclegen.Repeat("Wastes", k)})
			}
		}
		if _, filter, ok := strings.Cut(lower, "playercountopponents$highestvalid "); ok {
			filter, _, _ = strings.Cut(filter, "/")
			for _, k := range opponentLandSizes {
				var lands []string
				for i := 0; i < k; i++ {
					card := staticFixtureFor(filter, i)
					if card == "" {
						lands = nil
						break
					}
					lands = append(lands, card)
				}
				if len(lands) > 0 {
					out = append(out, conditionPrelude{opponentBattlefield: lands})
				}
			}
		}
	}
	return out
}

// svarBody is the SVar's body, or the name itself when it is an inline body.
func svarBody(f *cards.Face, name string) string {
	if body, ok := f.SVars[name]; ok {
		return body
	}
	return name
}

// solvedCaseCondition reports an IsPresent filter that needs a solved Case,
// which no setup reaches: a Case is solved by its own end-step trigger.
func solvedCaseCondition(t *cards.Trigger) bool {
	for _, key := range []cards.ParamKey{cards.PKIsPresent, cards.PKIsPresent2} {
		if strings.Contains(strings.ToLower(t.ParamStr(key)), "issolved") {
			return true
		}
	}
	return false
}

// attackPowerNeeded is the total attacking power a Count$Valid ...attacking
// $CardPower gate asks for, 0 when the trigger asks for none.
func attackPowerNeeded(t *cards.Trigger, text string) int {
	if !strings.Contains(text, "creature.attacking$cardpower") {
		return 0
	}
	return staticCountFrom(t.ParamStr(cards.PKSVarCompare))
}

// attackPowerFillers are plain attackers with their printed power, biggest
// first, so a total-power gate is reached with few attackers.
var attackPowerFillers = []struct {
	card  string
	power int
}{{"Gigantosaurus", 10}, {"Nessian Asp", 4}, {"Grizzly Bears", 2}, {"Llanowar Elves", 1}, {"Elvish Mystic", 1}}

// powerAttackers names the fillers whose power sums to at least need.
func powerAttackers(need int) []string {
	var out []string
	for _, p := range attackPowerFillers {
		if need <= 0 {
			break
		}
		out = append(out, p.card)
		need -= p.power
	}
	return out
}
