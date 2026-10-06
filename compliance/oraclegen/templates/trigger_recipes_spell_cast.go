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
	if strings.Contains(targets, "Creature") || strings.Contains(targets, "Self") || strings.Contains(t.ParamStr(cards.PKValidTgts), "Creature") {
		if c, ok := castCause(reg, name, growthProbe, "p0:"+name); ok {
			return []triggerCause{c}
		}
	}

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
		if !spellProbeMatchesType(face, filter) {
			continue
		}
		if count == 1 {
			if c, ok := castCause(reg, name, probe, spellProbeTarget(probe, filter, t)); ok {
				return []triggerCause{c}
			}
			continue
		}
		first, ok1 := castProbe(reg, probe)
		second, ok2 := castProbe(reg, probe)
		if !ok1 || !ok2 {
			continue
		}
		second.Card += "#2"
		return []triggerCause{{hand: []string{probe, probe}, steps: []oraclegen.Step{first, second}}}
	}
	return out
}

func spellProbeTarget(probe, filter string, t *cards.Trigger) string {
	if probe == shockProbe || strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "singletarget") || strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSAonCard)), "singletarget") || strings.Contains(strings.ToLower(filter), "instant") || strings.Contains(strings.ToLower(filter), "sorcery") {
		return "p1"
	}
	return ""
}

func spellProbeMatchesType(f *cards.Face, filter string) bool {
	lower := strings.ToLower(filter)
	if strings.Contains(lower, "noncreature") && f.IsCreature() {
		return false
	}
	// Enforce the type heads we can identify safely. Subtypes are matched
	// against printed type lines; unrecognised Forge expressions are left to
	// the engine's trigger matcher when the candidate is tried.
	for _, typ := range []string{"artifact", "creature", "instant", "sorcery", "enchantment", "lesson", "legendary", "villain", "outlaw", "lizard"} {
		if strings.Contains(lower, typ) && !strings.Contains(lower, "non"+typ) {
			matched := false
			for _, printed := range f.Types {
				if strings.EqualFold(printed, typ) {
					matched = true
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}
