package templates

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

var spellCastGE = regexp.MustCompile(`(?i)(?:ManaSpent|cmc)\s*GE\s*(\d+)`)

// spellCastProbeCauses orders causes from the most constrained shape to a
// corpus-ordered fallback. Firing, rather than a second copy of Forge's
// filter grammar, decides whether each candidate is suitable.
func spellCastProbeCauses(reg *cards.Registry, name string, t *cards.Trigger) []triggerCause {
	filter := t.ParamStr(cards.PKValidCard)
	targets := t.ParamStr(cards.PKTargetsValid)
	targetProbe := strings.Contains(targets, "Creature") || strings.Contains(targets, "Self") || strings.Contains(t.ParamStr(cards.PKValidTgts), "Creature")

	count := 1
	if strings.Contains(strings.ToUpper(t.ParamStr(cards.PKActivatorThisTurnCast)), "EQ2") {
		count = 2
	}
	minCMC := 0
	for _, key := range []cards.ParamKey{cards.PKValidSA, cards.PKValidSAonCard} {
		if m := spellCastGE.FindStringSubmatch(t.ParamStr(key)); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > minCMC {
				minCMC = n
			}
		}
	}
	if m := spellCastGE.FindStringSubmatch(filter); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > minCMC {
			minCMC = n
		}
	}

	// Preferred known probes make the common numeric and cast-count forms
	// readable; the sorted corpus pass extends coverage to type/subtype filters.
	candidates := []string{"Shock", "Angel's Mercy", "Air Elemental", "Murder", "Divination", "Giant Growth", "Grizzly Bears", "Mind Stone"}
	if minCMC >= 4 {
		candidates = []string{"Angel's Mercy", "Air Elemental", "Shock", "Murder", "Divination", "Giant Growth", "Grizzly Bears", "Mind Stone"}
	}
	if strings.Contains(strings.ToLower(filter), "creature") && minCMC >= 4 {
		candidates = append([]string{"Air Elemental"}, candidates...)
	}
	if strings.Contains(strings.ToLower(filter), "artifact") {
		candidates = append([]string{"Mind Stone"}, candidates...)
	}
	if count == 2 {
		candidates = append([]string{shockProbe}, candidates...)
	}
	all := make([]string, 0, len(reg.Cards))
	for _, c := range reg.Cards {
		if len(c.Faces) != 0 {
			all = append(all, c.Faces[0].Name)
		}
	}
	sort.Strings(all)
	candidates = append(candidates, all...)
	seen := make(map[string]bool, len(candidates))
	var out []triggerCause
	if targetProbe {
		candidates = append([]string{growthProbe}, candidates...)
	}
	for _, probe := range candidates {
		if seen[probe] || probe == name {
			continue
		}
		seen[probe] = true
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		face := card.Faces[0]
		if face.IsLand() || face.IsCreature() && strings.Contains(strings.ToLower(filter), "noncreature") || int(face.Cmc()) < minCMC {
			continue
		}
		if filter != "" && !spellProbeMatchesType(face, filter) {
			continue
		}
		validSA := t.ParamStr(cards.PKValidSA)
		if validSA != "" && !spellProbeMatchesType(face, strings.ReplaceAll(validSA, "Spell.", "")) {
			continue
		}
		validSAOnCard := t.ParamStr(cards.PKValidSAonCard)
		if validSAOnCard != "" && !spellProbeMatchesType(face, strings.ReplaceAll(validSAOnCard, "Spell.", "")) {
			continue
		}
		probeTargets := []string(nil)
		if targetProbe {
			probeTargets = append(probeTargets, spellCastTriggerTarget(name, t))
		} else if target := spellProbeTarget(probe, filter, name, t); target != "" {
			probeTargets = append(probeTargets, target)
		}
		if count == 1 {
			if c, ok := castCause(reg, name, probe, probeTargets...); ok {
				out = append(out, c)
			}
		} else {
			first, ok1 := castProbe(reg, probe, probeTargets...)
			second, ok2 := castProbe(reg, probe, probeTargets...)
			if ok1 && ok2 {
				second.Card += "#2"
				out = append(out, triggerCause{hand: []string{probe, probe}, steps: []oraclegen.Step{first, second}})
			}
		}
		if len(out) == 8 {
			break
		}
	}
	return out
}

func spellCastTriggerTarget(source string, t *cards.Trigger) string {
	targetFilter := strings.ToLower(t.ParamStr(cards.PKTargetsValid) + "," + t.ParamStr(cards.PKValidTgts))
	if strings.Contains(targetFilter, "opponent") {
		return "p1"
	}
	return "p0:" + source
}

func spellProbeTarget(probe, filter, source string, t *cards.Trigger) string {
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "singletarget") || strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSAonCard)), "singletarget") {
		return "p0:" + source
	}
	if probe == shockProbe || strings.Contains(strings.ToLower(filter), "instant") || strings.Contains(strings.ToLower(filter), "sorcery") {
		return "p1"
	}
	return ""
}

func spellCastNarrowSkip(t *cards.Trigger) string {
	filter := strings.ToLower(t.ParamStr(cards.PKValidCard))
	for _, shape := range []struct{ text, reason string }{
		{"wascastfromexile", "cast-from-exile provenance"},
		{"wascastfromyourhand", "cast-from-hand provenance"},
		{"youdontown", "ownership provenance"},
		{"adventure", "adventure-cast provenance"},
	} {
		if strings.Contains(strings.ReplaceAll(filter, " ", ""), shape.text) {
			return "spell-cast unsupported " + shape.reason
		}
	}
	if strings.EqualFold(t.ParamStr(cards.PKOpponentTurn), "True") {
		return "spell-cast opponent-turn condition"
	}
	if t.ParamStr(cards.PKIsPresent) != "" || t.ParamStr(cards.PKIsPresent2) != "" {
		return "spell-cast IsPresent condition"
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKCondition)), "level") || strings.Contains(strings.ToLower(t.ParamStr(cards.PKCheckSVar)), "level") {
		return "spell-cast class-level condition"
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "singletarget") {
		return "spell-cast singleTarget condition (engine matcher unsupported)"
	}
	return ""
}

func spellProbeMatchesType(f *cards.Face, filter string) bool {
	// Forge's comma-separated type heads are alternatives (e.g.
	// Instant.singleTarget,Sorcery.singleTarget), not conjunctions.
	known := []string{"artifact", "creature", "instant", "sorcery", "enchantment", "lesson", "legendary", "villain", "outlaw", "lizard"}
	for _, alt := range strings.Split(strings.ToLower(filter), ",") {
		if strings.Contains(alt, "noncreature") && f.IsCreature() {
			continue
		}
		constrained, matched := false, false
		for _, typ := range known {
			if !strings.Contains(alt, typ) || strings.Contains(alt, "non"+typ) {
				continue
			}
			constrained = true
			for _, printed := range f.Types {
				if strings.EqualFold(printed, typ) {
					matched = true
				}
			}
		}
		if !constrained || matched {
			return true
		}
	}
	return filter == ""
}
