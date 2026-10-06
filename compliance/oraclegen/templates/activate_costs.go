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
	return activationCostIn(cost, "battlefield")
}

// activationCostIn is activationCost for a source activated from zone
// ("battlefield", "hand" or "graveyard"). The channel-style self costs name
// their own zone: Discard<1/CARDNAME> and ExileFromHand<1/CARDNAME> are
// payable only from the hand, ExileFromGrave<1/CARDNAME> (or one other
// Creature.Other card) only from the graveyard. Anywhere else they stay a
// named cost gap.
func activationCostIn(cost, zone string) (pool, gap string) {
	var mana []string
	for _, tok := range costTokens(cost) {
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
			if sacFixtureSupported(tok) {
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
			} else if graveyardCreatureCost(tok) || graveyardCostFixtures(tok) != nil {
				continue
			}
		case "ExileCtrlOrGrave":
			if craftCostFixtures(tok) != nil {
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
			if selfZoneCost(tok) {
				continue
			}
		case "tapXType":
			if tapXTypeFixtureSupported(tok) {
				continue
			}
			return "", tapXTypeGapClass(tok)
		case "AddCounter", "SubCounter":
			if loyaltyCounter(tok) {
				continue
			}
			if _, _, ok := sourceCounterCost(tok); ok {
				continue
			}
		case "RemoveAnyCounter":
			if _, _, ok := sourceCounterCost(tok); ok {
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
	p, why := oraclegen.PoolFor(strings.Join(mana, " "))
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
	return len(parts) >= 2 && parts[0] == "1" &&
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

func filterCostFixtures(tok string) []string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return nil
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 1 {
		return nil
	}
	filter := strings.ToLower(parts[1])
	var candidates []string
	for _, clause := range strings.Split(filter, "|") {
		clause = strings.TrimSpace(clause)
		switch {
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

func craftCostFixtures(tok string) []string {
	return filterCostFixtures(tok)
}

func graveyardCostFixtures(tok string) []string {
	return filterCostFixtures(tok)
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

func activationCostFixtures(tok string) []string {
	head := tok
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		head = tok[:i]
	}
	switch head {
	case "Discard":
		return discardCostFixtures(tok)
	case "ExileCtrlOrGrave":
		return craftCostFixtures(tok)
	case "ExileFromGrave":
		if graveyardCreatureCost(tok) {
			return []string{"Grizzly Bears"}
		}
		return graveyardCostFixtures(tok)
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

func tapXTypeFixtures(tok string) ([]string, bool) {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil, false
	}
	parts := strings.SplitN(payload, "/", 2)
	if len(parts) != 2 {
		return nil, false
	}
	filter := parts[1]
	threshold := 0
	if i := strings.Index(filter, "+withTotalPowerGE"); i >= 0 {
		raw := strings.TrimSpace(filter[i+len("+withTotalPowerGE"):])
		var err error
		threshold, err = strconv.Atoi(raw)
		if err != nil || threshold < 1 {
			return nil, false
		}
		filter = filter[:i]
	}
	count := 0
	if strings.EqualFold(parts[0], "Any") {
		if threshold == 0 {
			return nil, false
		}
	} else {
		var err error
		count, err = strconv.Atoi(parts[0])
		if err != nil || count < 1 {
			return nil, false
		}
	}

	// Each semicolon-separated clause is a supported alternative. Token-only
	// and other modifiers remain named cost gaps rather than being guessed.
	var choices []string
	for _, clause := range strings.Split(filter, ";") {
		clause = strings.ToLower(clause)
		if strings.Contains(clause, ".token") {
			return nil, false
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
		default:
			return nil, false
		}
	}
	if len(choices) == 0 {
		return nil, false
	}
	choices = uniqueFixtureNames(choices)
	if threshold > 0 {
		// Known power values for the creature fixtures above. Take the highest
		// available bodies first, then add smaller bodies until Crew is paid.
		power := map[string]int{"Grizzly Bears": 2, "Llanowar Elves": 1, "Nessian Asp": 4, "Colossal Dreadmaw": 6, "Craw Wurm": 6, "Siege Wurm": 5}
		var creatures []string
		for _, name := range choices {
			if power[name] > 0 {
				creatures = append(creatures, name)
			}
		}
		// Catalogue order is deterministic; use descending power with name tie-break.
		sort.Slice(creatures, func(i, j int) bool {
			if power[creatures[i]] != power[creatures[j]] {
				return power[creatures[i]] > power[creatures[j]]
			}
			return creatures[i] < creatures[j]
		})
		var selected []string
		total := 0
		for _, name := range creatures {
			selected = append(selected, name)
			total += power[name]
			if total >= threshold {
				break
			}
		}
		if total < threshold {
			return nil, false
		}
		return selected, true
	}
	if len(choices) < count {
		return nil, false
	}
	return choices[:count], true
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

func tapXTypeFixtureSupported(tok string) bool { _, ok := tapXTypeFixtures(tok); return ok }

// tapXTypeGapClass keeps census buckets useful without exposing every raw
// filter payload. Unsupported X counts and token filters are distinct classes;
// remaining parser/filter shapes share the unsupported-filter bucket.
func tapXTypeGapClass(tok string) string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return "tapXType<malformed>"
	}
	parts := strings.SplitN(payload, "/", 2)
	if len(parts) != 2 {
		return "tapXType<malformed>"
	}
	if strings.EqualFold(strings.TrimSpace(parts[0]), "X") {
		return "tapXType<X>"
	}
	if strings.Contains(strings.ToLower(parts[1]), ".token") {
		return "tapXType<token-filter>"
	}
	return "tapXType<unsupported-filter>"
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
		isTapCost := false
		for _, kind := range d.PickKinds {
			if kind == "tapcost" {
				isTapCost = true
				break
			}
		}
		if !isTapCost {
			continue
		}
		// Preserve gorge's exact choice, rather than selecting the catalogue
		// defaults. XAnswersForScenario may encode a batch as one compound
		// answer, while XMage also needs the selected card labels individually.
		for _, pick := range d.Picks {
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
		return
	}
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "tapXType") {
			continue
		}
		picks, ok := tapXTypeFixtures(tok)
		if !ok {
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

func addTapXTypeFixtures(p0 *oraclegen.Seat, cost string) {
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "tapXType") {
			continue
		}
		picks, ok := tapXTypeFixtures(tok)
		if !ok {
			continue
		}
		for _, pick := range picks {
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, pick)
		}
	}
}
