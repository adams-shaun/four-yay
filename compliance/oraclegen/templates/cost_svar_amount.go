// This file is the SVar-amount vocabulary of the own-spell ReduceCost
// fixtures: the amount bodies a cost static's Amount$ can name that need
// more than the generic count of matching permanents (cost_condition.go's
// costAmountFixture / costCountPrelude), and the cast probe for the one
// whose reduction the cast itself announces.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// costPowerFixtures names, per `with<Keyword>` filter word, a fixture
// creature with a known nonzero printed power. The static probe table's
// flying stand-in (Ornithopter) is 0-power, which a power total cannot use.
var costPowerFixtures = map[string]string{"withflying": "Serra Angel"}

// costPowerAmountFixture serves an Amount$ SVar that reads a board count's
// CardPower total (The Lord of the Eagles' "costs {X} less, where X is the
// total power of creatures you control with flying"). The reduction is the
// placed fixtures' total power: one fixture creature per counted match,
// whose printed power the generator knows, so the probe pays exactly the
// printed cost minus that total. ok is true for the shape whether or not a
// fixture is buildable — an unbuildable one is a named gap, never the
// generic count path, which would place a 0-power stand-in and offer a
// price the static does not give.
func costPowerAmountFixture(reg *cards.Registry, f *cards.Face, amount string) (int, []conditionPrelude, string, bool) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return 0, nil, "", false
	}
	if _, err := strconv.Atoi(amount); err == nil {
		return 0, nil, "", false
	}
	body := strings.TrimSpace(f.SVars[amount])
	if body == "" {
		body = amount
	}
	i := strings.LastIndex(body, "$")
	if i < 0 || !strings.EqualFold(body[i+1:], "CardPower") {
		return 0, nil, "", false
	}
	gap := "cost static condition: SVar (" + body + ")"
	zone, filter := costValidBody(strings.TrimSpace(body[:i]))
	if zone == "" {
		return 0, nil, gap + ": power-total body unread", true
	}
	if !strings.EqualFold(zone, "Battlefield") {
		return 0, nil, gap + ": power fixture in " + zone, true
	}
	if staticOpposing(filter) {
		return 0, nil, gap + ": needs opponent board (" + filter + ")", true
	}
	name := ""
	for _, w := range affectedWords(filter) {
		if c, found := costPowerFixtures[strings.ToLower(w)]; found {
			name = c
			break
		}
	}
	if name == "" {
		// No keyword fixture: the generic count path's own stand-in serves,
		// because a power total IS the placed card's printed power — the
		// probe pays exactly printed minus it (Ghalta, Primal Hunger's
		// "total power of creatures you control" over Llanowar Elves).
		name = staticFixtureFor(filter, 0)
	}
	if name == "" {
		return 0, nil, gap + ": power fixture unavailable (" + filter + ")", true
	}
	power, why := fixturePower(reg, name)
	if why != "" || power < 1 {
		return 0, nil, gap + ": power fixture power unread (" + name + ")", true
	}
	return power, []conditionPrelude{placeNames(name, "Battlefield", filter, 1)}, "", true
}

// fixturePower reads a fixture card's printed power from the registry
// ("4/4" -> 4); why names an absent card or an unread power.
func fixturePower(reg *cards.Registry, name string) (int, string) {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || c.Faces[0] == nil {
		return 0, "absent"
	}
	power, _, found := strings.Cut(c.Faces[0].PT, "/")
	n, err := strconv.Atoi(strings.TrimSpace(power))
	if !found || err != nil {
		return 0, "unread"
	}
	return n, ""
}

// optionalGenericPaidProbes serves a ReduceCost whose Amount$ reads the
// pending cast's own CR 601.2b election of an optional additional cost
// (Bite Down on Crime's "{2} less to cast if evidence was collected" over
// SVar Z = Count$OptionalGenericCostPaid.2.0). gorge prices the election
// into the OFFER (rules' costAmountCtx seeds it through the one cost-amount
// context), so the probe casts the card through the optional-cost variant
// (cast_mode "optionalcost") with the cost's own fixture in place, at the
// EXACT reduced price: the paid branch's generic pips come off the printed
// cost. handled is false for any other amount shape; a recognised election
// whose branches or cost the fixtures cannot pay is a named gap, never the
// generic count path (whose board prelude cannot move the election).
func optionalGenericPaidProbes(reg *cards.Registry, f *cards.Face, st cards.Static, name string, p costProbe, base string) ([]costProbe, string, bool) {
	amount := strings.TrimSpace(st.Params["Amount"])
	if amount == "" {
		return nil, "", false
	}
	body := strings.TrimSpace(f.SVars[amount])
	if body == "" {
		body = amount
	}
	if !strings.Contains(strings.ToLower(body), "optionalgenericcostpaid") {
		return nil, "", false
	}
	gap := "cost static condition: SVar (" + body + ")"
	// The paid branch is the reduction the probe prices. A non-literal
	// branch (Dragon's Fire's paid branch is SVar X = Revealed$CardPower)
	// has no fixture-time value to price from.
	parts := strings.Split(strings.TrimSpace(body), ".")
	if len(parts) < 3 {
		return nil, gap + ": branches unread", true
	}
	reduction, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, gap + ": paid branch (" + parts[1] + ") is not a literal", true
	}
	if reduction < 1 {
		return nil, gap + ": paid branch reduces nothing", true
	}
	for _, key := range []string{"Condition", "IsPresent", "CheckSVar"} {
		if strings.TrimSpace(st.Params[key]) != "" {
			return nil, gap + ": gate (" + st.Params[key] + ") unhandled with the election", true
		}
	}
	// The election needs the card's own OptionalCost static: a cast_mode
	// cannot say WHICH of two to pay, and the cost token names the fixture.
	if n := optionalCostStatics(f); n != 1 {
		return nil, gap + ": " + strconv.Itoa(n) + " OptionalCost statics", true
	}
	tok := ""
	for i := range f.Statics {
		if strings.EqualFold(f.Statics[i].Mode, "OptionalCost") {
			toks := costTokens(f.Statics[i].ParamStr(cards.PKCost))
			if len(toks) != 1 {
				return nil, gap + ": compound optional cost " + f.Statics[i].ParamStr(cards.PKCost), true
			}
			tok = toks[0]
		}
	}
	if _, why := optionalCostSetupFor(tok); why != "" {
		return nil, gap + ": " + why, true
	}
	// Only the CollectEvidence part has a fixture the reduced-price probe
	// can carry in the setup (the graveyard prelude).
	head, _, _ := strings.Cut(tok, "<")
	if head != "CollectEvidence" {
		return nil, gap + ": optional cost " + tok + " has no reduced-price fixture", true
	}
	graveyard := evidenceCostFixtures(tok)
	if len(graveyard) == 0 {
		return nil, gap + ": no graveyard fixture for " + tok, true
	}
	if available := strings.Count(base, "C"); reduction > available {
		return nil, gap + ": exceeds the printed generic", true
	}
	mana, ok := removeGenericMana(base, reduction)
	if !ok {
		return nil, gap + ": exceeds the printed generic", true
	}
	p.castMode = optionalCostCastMode
	p.graveyard = append(p.graveyard, graveyard...)
	p.mana = mana
	p.mustReplay = true
	p.skipReason = "optional-cost cast fixture unavailable (" + tok + ")"
	return []costProbe{p}, "", true
}

// costSacrificePrelude is the turn-history prelude a SacrificedThisTurn
// count reads: one real sacrifice of a fixture whose printed types cover the
// count's spec word. The shared prelude's sacrifice (Grizzly Bears) is a
// creature only, so a spec of Artifact stays false under it — Suspicious
// Detonation's "if you've sacrificed an artifact this turn" gate needs the
// artifact CREATURE Ornithopter (Village Rites' cost sacrifices a creature).
// ok is false for any other spec word, which keeps the generic path.
func costSacrificePrelude(reg *cards.Registry, body string) (conditionPrelude, bool) {
	lower := strings.ToLower(body)
	i := strings.Index(lower, "sacrificedthisturn")
	if i < 0 {
		return conditionPrelude{}, false
	}
	spec := strings.Fields(lower[i+len("sacrificedthisturn"):])
	if len(spec) == 0 || spec[0] != "artifact" {
		return conditionPrelude{}, false
	}
	return sacrificeConditionPreludeOf(reg, "Ornithopter")
}

// announcedSacXProbes serves a ReduceCost whose Amount reads the cast's own
// announced sacrifice count (SVar X = Count$xPaid over an announced
// Sac<X/Spec> additional cost — Rottenmouth Viper's "sacrifice any number of
// nonland permanents; it costs {1} less for each one sacrificed"). The
// reduction equals the announced X, so the probe announces X as the number
// of sacrifice fixtures it holds, sacrifices them and pays the price that
// reduction gives. handled is false for any other shape; a recognized
// Sac<X> cost the fixtures cannot pay is a named gap, never the generic
// count path (whose board prelude can never move Count$xPaid).
func announcedSacXProbes(reg *cards.Registry, f *cards.Face, st cards.Static, name string, p costProbe, base string) ([]costProbe, string, bool) {
	amount := strings.TrimSpace(st.Params["Amount"])
	if amount == "" {
		return nil, "", false
	}
	if body := strings.TrimSpace(f.SVars[amount]); !strings.EqualFold(body, "Count$xPaid") {
		return nil, "", false
	}
	if len(xAnswers(f)) > 0 {
		// A printed {X} in the mana cost shares the one announced X with the
		// Sac<X> part; two X asks would consume the scripted picks in the
		// wrong order. No corpus carrier pairs the two.
		return nil, "cost static condition: SVar (Count$xPaid): printed X also present", true
	}
	for _, key := range []cards.ParamKey{cards.PKCondition, cards.PKIsPresent, cards.PKCheckSVar} {
		if strings.TrimSpace(st.ParamStr(key)) != "" {
			return nil, "cost static condition: SVar (Count$xPaid): gate (" + st.ParamStr(key) + ") unhandled with the announced X", true
		}
	}
	spec := ""
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		for _, tok := range costTokens(sa.ParamStr(cards.PKCost)) {
			payload, ok := bracketPayload(tok)
			if !ok || !strings.HasPrefix(tok, "Sac<") {
				continue
			}
			// The token is `Sac<X/Spec[/Forge description]>`: the count is
			// parts[0], the filter parts[1] (sacGapClass's split), so the
			// description's text never reaches the filter read.
			parts := strings.Split(payload, "/")
			if len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "X") {
				spec = parts[1]
			}
		}
	}
	if spec == "" {
		return nil, "", false
	}
	words := map[string]bool{}
	for _, w := range affectedWords(spec) {
		words[strings.ToLower(w)] = true
	}
	if len(words) != 2 || !words["permanent"] || !words["nonland"] {
		return nil, "cost static condition: SVar (Count$xPaid) over Sac<X/" + spec + ">", true
	}
	// Two token-free nonland permanents: the announcement is payable, the
	// sacrifice settles exactly what was announced and the price the
	// reduction gives stays printable.
	fixtures := []string{"Ornithopter", "Sol Ring"}
	reduction := len(fixtures)
	if available := strings.Count(base, "C"); reduction > available {
		return nil, "cost static condition: SVar (Count$xPaid) exceeds the printed generic", true
	}
	mana, ok := removeGenericMana(base, reduction)
	if !ok {
		return nil, "cost static condition: SVar (Count$xPaid) exceeds the printed generic", true
	}
	p.battlefield = appendUnique(p.battlefield, fixtures...)
	p.mana = mana
	p.mustReplay = true
	p.skipReason = "announced-sacrifice cast unplayable at X=" + strconv.Itoa(reduction) + " (Sac<X/" + spec + ">)"
	p.answers = append(p.answers,
		oraclegen.Answer{Kind: "choose", Pick: []string{sacXPick(reduction)}},
		oraclegen.Answer{Kind: "choose", Pick: sacrificedRefs(fixtures)})
	return []costProbe{p}, "", true
}

// sacXPick is the announcement pick that matches the "Choose a value for X"
// ask's option label ("X = 2") without matching a neighbouring value.
func sacXPick(n int) string { return "X = " + strconv.Itoa(n) }

// sacrificedRefs is the sacrifice ask's picks: the fixtures as p0 object
// refs, so the settlement sacrifices exactly what the announcement counted.
func sacrificedRefs(fixtures []string) []string {
	out := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		out = append(out, "p0:"+f)
	}
	return out
}
