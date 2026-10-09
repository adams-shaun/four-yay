package templates

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

var spellCastGE = regexp.MustCompile(`(?i)(?:ManaSpent|cmc)\s*GE\s*(\d+)`)

// spellCastProbeCauses orders causes from the most constrained shape to a
// corpus-ordered fallback. Firing, rather than a second copy of Forge's
// filter grammar, decides whether each candidate is suitable.
func spellCastProbeCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	filter := t.ParamStr(cards.PKValidCard)
	targets := t.ParamStr(cards.PKTargetsValid)
	targetProbe := strings.Contains(targets, "Creature") || strings.Contains(targets, "Self") || strings.Contains(t.ParamStr(cards.PKValidTgts), "Creature")

	count := activatorCastCount(t.ParamStr(cards.PKActivatorThisTurnCast))
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
	// A multicolour filter (Card.MultiColor) needs a multicoloured probe:
	// every default candidate is mono, so no cause could fire.
	multicolour := strings.Contains(strings.ToLower(filter), "multicolor")
	if multicolour {
		candidates = append([]string{multicolourProbe}, candidates...)
	}
	if count == 2 {
		candidates = append([]string{shockProbe}, candidates...)
	}
	all := make([]string, 0, reg.Len())
	for _, c := range reg.AllCards() {
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
	fp := newFilterProbe(filter, state.ZStack)
	candidates = filterAcceptedFirst(reg, candidates, fp)
	for _, probe := range candidates {
		if seen[probe] || probe == name || !oraclegen.XMageKnown(probe) {
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
		// The matcher, when it decided the filter, outranks the type-line
		// heuristic: an Outlaw or a Turtle is a subtype group it cannot see.
		if filter != "" && !fp.decided && !spellProbeMatchesType(face, filter) {
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
		targetPerm := ""
		if targetProbe {
			// The trigger names a permanent the probe spell must target (a
			// creature you control, an artifact or land). The setup places a
			// probe permanent the filter accepts and the spell targets it; the
			// old recipe targeted the source, which is not a permanent of that
			// type, so the cast was illegal and the trigger never fired.
			targetPerm, probeTargets = spellCastTriggerTarget(reg, name, t)
		} else if target := spellProbeTarget(probe, filter, name, t); target != "" {
			// A multicolour filter's probe spell may need a permanent target
			// (Terminate destroys a creature): aim it at a placed creature
			// where the instant/sorcery heuristic would aim the p1 player,
			// which the spell's own ValidTgts$ refuses.
			if multicolour && spellProbePlayerTargetBlocked(face) {
				target = "p0:" + bearsProbe
				targetPerm = bearsProbe
			}
			probeTargets = append(probeTargets, target)
		}
		if count == 1 {
			if c, ok := castCause(reg, name, probe, probeTargets...); ok {
				if targetPerm != "" {
					c.battlefield = append(c.battlefield, targetPerm)
				}
				out = append(out, c)
			}
		} else if c, ok := repeatedCastCause(reg, probe, count, probeTargets); ok {
			out = append(out, c)
		}
		if len(out) == 8 {
			break
		}
	}
	// A provenance-gated trigger (cast from exile / not from hand, an
	// Adventure face) needs its own cause ahead of the ordinary hand probes:
	// the hand cast never satisfies the predicate, so it would only waste a
	// fixture pass before the row skipped. The prepared-copy cast (the
	// CR 722.3c FlagPreparedCopy provenance) is the same shape.
	return append(spellCastPreparedCause(reg, f, name, t),
		append(spellCastProvenanceCauses(reg, f, name, t), out...)...)
}

// filterAcceptedFirst moves the probes gorge's own matcher accepts for the
// trigger's ValidCard$ filter ahead of the rest, keeping each group's order.
// Only eight causes are kept, and a Shock the filter rejects would otherwise
// fill them before the first card that can fire the trigger.
func filterAcceptedFirst(reg *cards.Registry, candidates []string, fp *filterProbe) []string {
	if !fp.decided {
		return candidates
	}
	var accepted, rest []string
	for _, probe := range candidates {
		if card, ok := reg.Lookup(probe); ok && fp.accepts(card) {
			accepted = append(accepted, probe)
		} else {
			rest = append(rest, probe)
		}
	}
	// A filter that reads state the scratch game lacks (a mana value equal to
	// the source's power) rejects every card: that is no verdict, so the
	// type-line heuristic and the old order stand.
	if len(accepted) == 0 {
		fp.decided = false
		return candidates
	}
	return append(accepted, rest...)
}

// activatorCastCount is how many spells the trigger's ActivatorThisTurnCast$
// needs cast in a turn (EQ2 two, GT1 two, EQ3 three), 1 when it is absent or
// not a shape this reads.
func activatorCastCount(cond string) int {
	m := activatorCastRe.FindStringSubmatch(strings.ToUpper(cond))
	if len(m) != 3 {
		return 1
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return 1
	}
	if m[1] == "GT" {
		n++
	}
	if n < 1 || n > 4 {
		return 1
	}
	return n
}

var activatorCastRe = regexp.MustCompile(`^(EQ|GT|GE)(\d+)$`)

// repeatedCastCause casts one probe count times, the later copies named
// probe#2, probe#3 so each step finds its own card in hand.
func repeatedCastCause(reg *cards.Registry, probe string, count int, targets []string) (triggerCause, bool) {
	c := triggerCause{}
	for i := 1; i <= count; i++ {
		step, ok := castProbe(reg, probe, targets...)
		if !ok {
			return triggerCause{}, false
		}
		if i > 1 {
			step.Card += "#" + strconv.Itoa(i)
		}
		c.hand = append(c.hand, probe)
		c.steps = append(c.steps, step)
	}
	return c, true
}

// spellCastTriggerTarget picks the target a spell-cast trigger's probe spell
// must name, and the permanent the setup places for it. The trigger's
// TargetsValid$ names the target filter; gorge's matcher accepts a probe
// permanent of the right type. An opponent target is p1 itself (a burn spell);
// a Self filter keeps the source. It returns ("", ["p1"]) when the target is a
// player and ("", nil) when the filter is blank.
func spellCastTriggerTarget(reg *cards.Registry, source string, t *cards.Trigger) (string, []string) {
	spec := strings.TrimSpace(t.ParamStr(cards.PKTargetsValid))
	if spec == "" {
		spec = strings.TrimSpace(t.ParamStr(cards.PKValidTgts))
	}
	if spec == "" {
		return "", nil
	}
	if strings.Contains(strings.ToLower(spec), "opponent") {
		return "", []string{"p1"}
	}
	fp := newFilterProbe(spec, state.ZBattlefield)
	// Llanowar Elves, not the Grizzly Bears: Baseline already puts a Grizzly
	// Bears on p1, and a p0 target of the same name makes the same-name
	// census's target pick ambiguous.
	for _, n := range []string{"Llanowar Elves", "Ornithopter", "Plains"} {
		card, ok := reg.Lookup(n)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		if fp.accepts(card) {
			return n, []string{"p0:" + n}
		}
	}
	return "", []string{"p0:" + source}
}

// multicolourProbe is the multicoloured instant the Card.MultiColor
// spell-cast filters cast ({B}{R} Terminate, destroy target creature); the
// default candidates are all mono, so a multicolour trigger would otherwise
// never fire.
const multicolourProbe = "Terminate"

func spellProbeTarget(probe, filter, source string, t *cards.Trigger) string {
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "singletarget") || strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSAonCard)), "singletarget") {
		return "p0:" + source
	}
	if probe == shockProbe || strings.Contains(strings.ToLower(filter), "instant") || strings.Contains(strings.ToLower(filter), "sorcery") {
		return "p1"
	}
	return ""
}

// spellProbePlayerTargetBlocked reports whether the probe spell's own
// ValidTgts$ accepts no player, so the instant/sorcery heuristic's p1 target
// is illegal and a permanent target must be bound instead. An empty
// ValidTgts$ is the engine's default Any, which accepts players.
func spellProbePlayerTargetBlocked(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		vt := strings.TrimSpace(sa.ParamStr(cards.PKValidTgts))
		for _, w := range []string{"Any", "Player", "Opponent"} {
			if vt == "" || strings.Contains(vt, w) {
				return false
			}
		}
		return true
	}
	return false
}

func spellCastNarrowSkip(t *cards.Trigger) string {
	filter := strings.ToLower(t.ParamStr(cards.PKValidCard) + "," + t.ParamStr(cards.PKValidSAonCard))
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
	// No OpponentTurn$ branch: levelb routes every OpponentTurn$ True row to
	// the trigger.spell-cast-opponent-turn sub, whose cast-family cause
	// passes to p1's main phase before the cast, so an unserved row here is
	// an ordinary non-firing cast, not a missing opponent-turn cause.
	if unknown := effects.UnknownPredicates(t.ParamStr(cards.PKValidCard)); len(unknown) > 0 {
		return "spell-cast filter predicate gorge does not implement (" + strings.Join(unknown, ",") + ")"
	}
	if t.ParamStr(cards.PKIsPresent) != "" || t.ParamStr(cards.PKIsPresent2) != "" {
		// The attacking presence ("while CARDNAME is attacking") is served by
		// baseTriggerRecipe's attack prelude; the solved Case presence has
		// its own solvedCasePreludes. A row that reaches here unserved is
		// named by what its presence actually needs.
		if solvedSelfSpec(t.ParamStr(cards.PKIsPresent)) || solvedSelfSpec(t.ParamStr(cards.PKIsPresent2)) {
			return "spell-cast IsSolved condition (needs a solved Case)"
		}
		return "spell-cast IsPresent condition"
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKCondition)), "level") || strings.Contains(strings.ToLower(t.ParamStr(cards.PKCheckSVar)), "level") {
		return "spell-cast class-level condition"
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
