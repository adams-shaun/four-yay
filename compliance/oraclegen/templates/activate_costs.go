package templates

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// activationCost classifies one activated ability's Cost$ under the v1 token
// whitelist. pool is the mana part's pool letters (PoolFor), or "" for a
// cost with no mana. gap names the offending token with its payload folded.
func activationCost(cost string) (pool, gap string) {
	return activationCostIn(cost, "battlefield", "")
}

// activationCostIn is activationCost for a source activated from zone
// ("battlefield", "hand" or "graveyard"). The channel-style self costs name
// their own zone: Discard<1/CARDNAME> and ExileFromHand<1/CARDNAME> are
// payable only from the hand, ExileFromGrave<1/CARDNAME> (or one other
// Creature.Other card) only from the graveyard. Anywhere else they stay a
// named cost gap.
func activationCostIn(cost, zone, name string, xMin ...int) (pool, gap string) {
	var mana []string
	x := activationX(cost, xMin...)
	for _, tok := range costTokens(cost) {
		if m, ok := keywordCostMana(tok, x); ok {
			if m != "" {
				mana = append(mana, m)
			}
			continue
		}
		head := tok
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			head = tok[:i]
		}
		switch head {
		case "T", "Q":
			continue
		case "PayLife":
			if isNumericBracket(tok) {
				continue
			}
		case "Sac":
			if sacFixtureSupported(tok, x) {
				continue
			}
			return "", sacGapClass(tok)
		case "Discard":
			if discardFixtureSupported(tok) || discardZoneFixtureSupported(tok) || (zone == "hand" && selfZoneCost(tok)) {
				continue
			}
		case "ExileFromHand":
			if zone == "hand" && selfZoneCost(tok) {
				continue
			}
		case "ExileFromGrave":
			if selfZoneCost(tok) {
				if zone == "graveyard" {
					continue
				}
			} else if graveyardCreatureCost(tok) || len(filterCostFixturesX(tok, x)) > 0 {
				continue
			} else if n, selfNamed := namedSelfCount(tok, name, x); selfNamed && n > 0 {
				continue
			}
		case "ExileCtrlOrGrave":
			if craftCostFixturesX(tok, x) != nil {
				continue
			}
		case "CollectEvidence":
			if evidenceCostFixtures(tok) != nil {
				continue
			}
		case "Exile":
			if selfZoneCost(tok) || exileCreatureCostFixtures(tok) != nil {
				continue
			}
		case "Return":
			if selfZoneCost(tok) || returnCreatureFixture(tok) != "" {
				continue
			}
		case "tapXType":
			if tapXTypeFixtureSupported(tok, x) || tokenCostSupported(tok) {
				continue
			}
			return "", tapXTypeGapClass(tok)
		case "AddCounter", "SubCounter":
			if loyaltyCounter(tok) || announcedSourceCounterX(tok) {
				continue
			}
			if _, _, ok := sourceCounterCost(tok); ok {
				continue
			}
			if _, _, _, ok := otherCounterCost(tok); ok {
				continue
			}
		case "RemoveAnyCounter":
			if _, _, ok := sourceCounterCost(tok); ok {
				continue
			}
			if _, _, _, ok := otherCounterCost(tok); ok {
				continue
			}
		}
		if _, why := oraclegen.PoolFor(tok); why == "" {
			mana = append(mana, tok)
			continue
		}
		return "", costHead(tok)
	}
	if len(mana) == 0 {
		return "", ""
	}
	p, why := oraclegen.PoolFor(strings.Join(substituteX(mana, x), " "))
	if why != "" {
		return "", costHead(strings.Join(mana, " "))
	}
	return p, ""
}

// discardZoneFixtureSupported recognizes the exact hand/land discard shapes
// served by this template; it is not a general Forge filter evaluator.
func discardZoneFixtureSupported(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	return len(parts) >= 2 && (parts[0] == "1" || parts[0] == "0" && strings.EqualFold(parts[1], "Hand")) &&
		(strings.EqualFold(parts[1], "Hand") || strings.EqualFold(parts[1], "Land"))
}

func discardCostFixtures(tok string) []string {
	if !discardZoneFixtureSupported(tok) {
		return nil
	}
	payload, _ := bracketPayload(tok)
	parts := strings.Split(payload, "/")
	if strings.EqualFold(parts[1], "Land") {
		return []string{"Wastes"}
	}
	return []string{"Wastes"}
}

func filterCostFixtures(tok string) []string { return filterCostFixturesX(tok, 0) }

// filterCostFixturesX is filterCostFixtures with the announced X: an
// `<X/filter>` count (Craft's XMin<N> ExileCtrlOrGrave<X/...>) reads x.
func filterCostFixturesX(tok string, x int) []string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return nil
	}
	n, err := strconv.Atoi(parts[0])
	if parts[0] == "X" {
		n, err = x, nil
	}
	if err != nil || n < 1 {
		return nil
	}
	filter := strings.ToLower(parts[1])
	var candidates []string
	for _, clause := range strings.Split(filter, "|") {
		clause = strings.TrimSpace(clause)
		switch {
		case strings.HasPrefix(clause, "permanent.other"):
			candidates = append(candidates, permanentCostFixtures(clause)...)
		case strings.HasPrefix(clause, "dinosaur"):
			candidates = append(candidates, "Colossal Dreadmaw")
		case strings.Contains(clause, "merfolk"):
			candidates = append(candidates, "Merfolk Wayfinder")
		case strings.Contains(clause, "pirate"):
			candidates = append(candidates, "Kari Zev, Skyship Raider")
		case strings.Contains(clause, "vampire"):
			candidates = append(candidates, "Sengir, the Dark Baron")
		case strings.Contains(clause, "card"):
			candidates = append(candidates, "Colossal Dreadmaw", "Sol Ring", "Grizzly Bears", "Wastes")
		case strings.Contains(clause, "artifact"):
			candidates = append(candidates, "Sol Ring", "Ornithopter")
		case strings.Contains(clause, "creature"):
			candidates = append(candidates, "Colossal Dreadmaw", "Craw Wurm", "Siege Wurm", "Nessian Asp", "Grizzly Bears", "Llanowar Elves")
		case strings.Contains(clause, "island"):
			candidates = append(candidates, "Island")
		case strings.Contains(clause, "land"):
			candidates = append(candidates, "Evolving Wilds", "Wastes", "Island")
		case strings.Contains(clause, "instant"):
			candidates = append(candidates, "Lightning Bolt", "Cancel")
		case strings.Contains(clause, "sorcery"):
			candidates = append(candidates, "Lava Spike", "Divination")
		case strings.Contains(clause, "enchantment"):
			candidates = append(candidates, "Pacifism", "Oblivion Ring")
		case strings.Contains(clause, "cave"):
			candidates = append(candidates, "Captivating Cave")
		default:
			return nil
		}
	}
	candidates = uniqueFixtureNames(candidates)
	if len(candidates) < n {
		return nil
	}
	return candidates[:n]
}

func craftCostFixtures(tok string) []string { return craftCostFixturesX(tok, 0) }

func craftCostFixturesX(tok string, x int) []string {
	return filterCostFixturesX(tok, x)
}

func graveyardCostFixtures(tok string) []string {
	return filterCostFixtures(tok)
}

// returnCreatureFixture names the tapped catalogue creature that pays a
// Return<1/Creature.tapped> cost (Urban Retreat's "Return a tapped creature
// you control to its owner's hand"); "" when the shape is unsupported.
func returnCreatureFixture(tok string) string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return ""
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != "1" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(parts[1])) {
	case "creature.tapped", "creature.youctrl.tapped", "creature.other.tapped":
		return "Grizzly Bears"
	}
	return ""
}

// namedSelfCount reports n when an ExileFromGrave token's filter names the
// source card itself ("...+namedCARDNAME", Say Its Name's second exile
// count): the payment's fixtures are copies of the source in the graveyard,
// which no catalogue stand-in can serve. n is the token count or the
// announced X.
func namedSelfCount(tok, name string, x int) (int, bool) {
	if name == "" {
		return 0, false
	}
	payload, ok := bracketPayload(tok)
	if !ok {
		return 0, false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return 0, false
	}
	n := 0
	if parts[0] == "X" {
		n = x
	} else {
		n, _ = strconv.Atoi(parts[0])
	}
	if n < 1 {
		return 0, false
	}
	if !strings.Contains(strings.ToLower(parts[1]), "named"+strings.ToLower(name)) {
		return 0, false
	}
	return n, true
}

func exileCreatureCostFixtures(tok string) []string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 || !(strings.EqualFold(parts[1], "Creature") || strings.EqualFold(parts[1], "Creature.Other")) {
		return nil
	}
	return filterCostFixtures(tok)
}

func activationCostFixtures(tok string) []string { return activationCostFixturesX(tok, 0) }

// activationCostFixturesX is activationCostFixtures with the cost's announced
// X (activationX), which an `<X/filter>` exile count reads.
func activationCostFixturesX(tok string, x int) []string {
	head := tok
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		head = tok[:i]
	}
	switch head {
	case "Discard":
		return discardCostFixtures(tok)
	case "ExileCtrlOrGrave":
		return craftCostFixturesX(tok, x)
	case "ExileFromGrave":
		if graveyardCreatureCost(tok) {
			return []string{"Grizzly Bears"}
		}
		// The `<X/filter>` grave-exile count (Winter, Cursed Rider's
		// ExileFromGrave<X/Artifact>) reads the announced X: x grave fixtures.
		return filterCostFixturesX(tok, x)
	case "CollectEvidence":
		return evidenceCostFixtures(tok)
	case "Exile":
		return exileCreatureCostFixtures(tok)
	}
	return nil
}

func evidenceCostFixtures(tok string) []string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil
	}
	threshold, err := strconv.Atoi(strings.TrimSpace(payload))
	if err != nil || threshold < 0 {
		return nil
	}
	// Take the fewest catalogue cards whose total mana value covers the
	// requested threshold. Keep the ordered values local and deterministic.
	candidates := []struct {
		name string
		mv   int
	}{{"Colossal Dreadmaw", 6}, {"Craw Wurm", 6}, {"Siege Wurm", 6}, {"Grizzly Bears", 2}, {"Sol Ring", 1}}
	var selected []string
	total := 0
	for _, candidate := range candidates {
		selected = append(selected, candidate.name)
		total += candidate.mv
		if total >= threshold {
			return selected
		}
	}
	return nil
}

// tapXTypeFixtures returns the catalogue permanents that pay a tapXType cost.
func tapXTypeFixtures(tok string, x int) ([]string, bool) {
	picks, gap := tapXTypeResolve(tok, x)
	return picks, gap == ""
}

// tapXTypePowers are the known power values of the creature fixtures a
// tapXType total-power (Crew-shaped) cost taps.
var tapXTypePowers = map[string]int{"Grizzly Bears": 2, "Llanowar Elves": 1, "Nessian Asp": 4, "Colossal Dreadmaw": 6, "Craw Wurm": 6, "Siege Wurm": 5}

// tapXTypeResolve is tapXTypeFixtures with the cause: it returns the picks, or
// the gap class naming why no catalogue permanent pays the cost. The payload
// is `count/filter[/description]`; the Forge description is display text and
// is dropped, so a described filter reads exactly as an undescribed one.
// tapXTypeResolve names the catalogue permanents a tapXType cost taps and the
// gap class when it cannot. The announced `<X/filter>` count (Secluded
// Starforge's tapXType<X/Artifact>, whose X is the tap election's own
// selection) reads x, defaulting to 1 untapped permanent.
func tapXTypeResolve(tok string, x int) ([]string, string) {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil, "tapXType<malformed>"
	}
	parts := strings.SplitN(payload, "/", 3)
	if len(parts) < 2 {
		return nil, "tapXType<malformed>"
	}
	tapX := strings.EqualFold(strings.TrimSpace(parts[0]), "X")
	if tapX && x < 1 {
		return nil, "tapXType<X>"
	}
	filter := parts[1]
	threshold := 0
	if i := strings.Index(filter, "+withTotalPowerGE"); i >= 0 {
		raw := strings.TrimSpace(filter[i+len("+withTotalPowerGE"):])
		var err error
		threshold, err = strconv.Atoi(raw)
		if err != nil || threshold < 1 {
			return nil, "tapXType<unsupported-filter>"
		}
		filter = filter[:i]
	}
	count := 0
	if tapX {
		count = x
	} else if strings.EqualFold(parts[0], "Any") {
		if threshold == 0 {
			return nil, "tapXType<unsupported-filter>"
		}
	} else {
		var err error
		count, err = strconv.Atoi(parts[0])
		if err != nil || count < 1 {
			return nil, "tapXType<unsupported-filter>"
		}
	}

	// Each semicolon-separated clause is a supported alternative. Token-only
	// and other modifiers remain named cost gaps rather than being guessed.
	var choices []string
	for _, clause := range strings.Split(filter, ";") {
		clause = strings.ToLower(clause)
		if strings.Contains(clause, ".token") {
			return nil, "tapXType<token-filter>"
		}
		switch {
		case clause == "artifact" || clause == "artifact.other":
			choices = append(choices, "Ornithopter", "Sol Ring")
		case clause == "creature" || clause == "creature.other":
			choices = append(choices, "Grizzly Bears", "Llanowar Elves", "Nessian Asp", "Colossal Dreadmaw", "Craw Wurm", "Siege Wurm")
		case clause == "elf" || clause == "elf.other":
			choices = append(choices, "Llanowar Elves", "Elvish Mystic", "Elvish Visionary")
		case clause == "ally" || clause == "ally.other":
			choices = append(choices, "Hada Freeblade", "Kazandu Blademaster")
		case clause == "permanent" || clause == "permanent.other":
			choices = append(choices, "Llanowar Elves", "Ornithopter", "Forest")
		case clause == "vehicle" || clause == "vehicle.other":
			choices = append(choices, "Smuggler's Copter")
		case clause == "mount" || clause == "mount.other":
			// XMage's driver cannot pick a Mount card (candidates.go
			// subtypeBattlefield), so a Mount alternative adds no fixture; an
			// alternative beside it (Vehicle) pays the cost.
		default:
			return nil, "tapXType<unsupported-filter>"
		}
	}
	if len(choices) == 0 {
		return nil, "tapXType<unsupported-filter>"
	}
	choices = uniqueFixtureNames(choices)
	if threshold > 0 {
		// Take the highest available bodies first, then add smaller bodies
		// until Crew is paid.
		var creatures []string
		for _, name := range choices {
			if tapXTypePowers[name] > 0 {
				creatures = append(creatures, name)
			}
		}
		// Catalogue order is deterministic; use descending power with name tie-break.
		sort.Slice(creatures, func(i, j int) bool {
			if tapXTypePowers[creatures[i]] != tapXTypePowers[creatures[j]] {
				return tapXTypePowers[creatures[i]] > tapXTypePowers[creatures[j]]
			}
			return creatures[i] < creatures[j]
		})
		var selected []string
		total := 0
		for _, name := range creatures {
			selected = append(selected, name)
			total += tapXTypePowers[name]
			if total >= threshold {
				break
			}
		}
		if total < threshold {
			return nil, "tapXType<count-above-catalogue>"
		}
		return selected, ""
	}
	if len(choices) < count {
		return nil, "tapXType<count-above-catalogue>"
	}
	return choices[:count], ""
}

func uniqueFixtureNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func tapXTypeFixtureSupported(tok string, x int) bool { _, ok := tapXTypeFixtures(tok, x); return ok }

// tapXTypeGapClass keeps census buckets useful without exposing every raw
// filter payload. Unsupported X counts, token filters and counts above the
// catalogue are distinct classes; remaining parser/filter shapes share the
// unsupported-filter bucket.
func tapXTypeGapClass(tok string) string {
	_, gap := tapXTypeResolve(tok, 0)
	return gap
}

func addTapXTypeAnswers(answers [][]oraclegen.XAnswer, step int, cost string, decisions []rules.OracleDecision) {
	if step < 0 || step >= len(answers) {
		return
	}
	// XAnswersForScenario already translates gorge's selected permanent
	// identities into XMage's answers. Never append a catalogue selection on
	// top of those observed picks: the filter may admit the source itself.
	for _, d := range decisions {
		if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" || len(d.ObjectPicks) == 0 {
			continue
		}
		if !hasPickKind(d, "tapcost") {
			continue
		}
		// Preserve gorge's exact choice, rather than selecting the catalogue
		// defaults. XMage pays the cost from the selected card labels
		// individually; the compound answer XAnswersForScenario encodes for the
		// same batch is dropped by dropCostCompound.
		var tokenPicks []string
		for k := range d.Picks {
			pick := observedCostPick(d, k)
			if isTokenPick(pick) {
				tokenPicks = append(tokenPicks, pick)
				continue
			}
			present := false
			for _, answer := range answers[step] {
				if answer.Seat == 0 && answer.Kind == "choice" && strings.EqualFold(answer.Value, pick) {
					present = true
					break
				}
			}
			if !present {
				answers[step] = append(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: pick})
			}
		}
		answers[step] = appendTokenAnswers(answers[step], 0, tokenPicks...)
		return
	}
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "tapXType") {
			continue
		}
		picks, ok := tapXTypeFixtures(tok, 0)
		if !ok {
			if needs, isToken := tokenCostNeeds(tok); isToken {
				// The tokens a prelude made: one answer per tapped token.
				answers[step] = appendTokenAnswers(answers[step], 0, tokenCostAnswerNames(needs)...)
			}
			continue
		}
		for _, pick := range picks {
			present := false
			for _, answer := range answers[step] {
				if answer.Seat == 0 && answer.Kind == "choice" && strings.EqualFold(answer.Value, pick) {
					present = true
					break
				}
			}
			if !present {
				answers[step] = append(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: pick})
			}
		}
	}
}

// dropCostCompound removes the joined "^" answer XAnswersForScenario encodes
// for an observed multi-pick tapcost or sacrifice batch. The cost answer
// paths (addTapXTypeAnswers, the Sac arm of addActivationCostAnswers) script
// the same picks one answer each, and XMage's cost consumes those singles:
// Kithkeeper's, Rat King's and Magda's verdicts each found the compound as the
// one unused choice. Left in the queue, the compound is consumed by the next
// dialog: Supportive Parents' "Add one mana of any color" threw "Choice key
// [Supportive Parents^Grizzly Bears] not found in [White, Blue, Black, Red,
// Green]".
func dropCostCompound(answers [][]oraclegen.XAnswer, step int, decisions []rules.OracleDecision) {
	if step < 0 || step >= len(answers) {
		return
	}
	for _, d := range decisions {
		if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" || len(d.Picks) < 2 || !allCostPicks(d) {
			continue
		}
		// The scenario spells a same-name pick by its exact ref
		// (oraclegen.JoinedChoice); the plain-name join covers the rest.
		compound, joined := strings.Join(d.Picks, "^"), oraclegen.JoinedChoice(d)
		for i, answer := range answers[step] {
			if answer.Seat == 0 && answer.Kind == "choice" && (strings.EqualFold(answer.Value, compound) || strings.EqualFold(answer.Value, joined)) {
				answers[step] = append(answers[step][:i], answers[step][i+1:]...)
				break
			}
		}
	}
}

// allCostPicks reports a decision whose every pick pays a tap or sacrifice
// cost, the batches the cost answer paths script as singles.
func allCostPicks(d rules.OracleDecision) bool {
	if len(d.PickKinds) == 0 {
		return false
	}
	for _, k := range d.PickKinds {
		if k != "tapcost" && k != "sacrifice" {
			return false
		}
	}
	return true
}

// hasPickKind reports whether any of the decision's picks is of kind.
func hasPickKind(d rules.OracleDecision, kind string) bool {
	for _, k := range d.PickKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func addTapXTypeFixtures(p0 *oraclegen.Seat, cost string, x int) {
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "tapXType") {
			continue
		}
		picks, ok := tapXTypeFixtures(tok, x)
		if !ok {
			continue
		}
		for _, pick := range picks {
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, pick)
		}
	}
}
