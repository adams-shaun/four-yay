package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// activationCost classifies one activated ability's Cost$ under the v1 token
// whitelist. pool is the mana part's pool letters (PoolFor), or "" for a
// cost with no mana. gap names the offending token with its payload folded.
func activationCost(cost string) (pool, gap string) {
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
			if sacSelf(tok) || sacOtherFixtureSupported(tok) {
				continue
			}
		case "Discard":
			if discardFixtureSupported(tok) {
				continue
			}
		case "Exile":
			if selfZoneCost(tok) {
				continue
			}
		case "tapXType":
			if tapXTypeFixtureSupported(tok) {
				continue
			}
		case "AddCounter", "SubCounter":
			if loyaltyCounter(tok) {
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

// tapXTypeFixtures is deliberately a small fixture catalogue, not a general
// Forge filter evaluator. Ordered names also make selection deterministic.
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
		for i := 0; i < len(creatures); i++ {
			for j := i + 1; j < len(creatures); j++ {
				if power[creatures[j]] > power[creatures[i]] || (power[creatures[j]] == power[creatures[i]] && creatures[j] < creatures[i]) {
					creatures[i], creatures[j] = creatures[j], creatures[i]
				}
			}
		}
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

func addTapXTypeAnswers(answers [][]oraclegen.XAnswer, step int, cost string) {
	if step < 0 || step >= len(answers) {
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
			answers[step] = append(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: pick})
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
