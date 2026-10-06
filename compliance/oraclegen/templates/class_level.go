package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Class level prelude (CR 716). Every granted Class body -- a trigger, a
// static, a cost static -- is stamped with ClassBand$ N (cards/kw_class.go)
// and is live only while the source permanent's designated level is at least
// N (rules/class_level.go). A level-B scenario that starts at level 1
// therefore never observes the body. The generator's kw:Class expansion
// already synthesises the level-up activators (AB$ ClassLevelUp | Level$ N |
// IsPresent$ Card.Self+classLevel_EQ<N-1> | SorcerySpeed$ True), and
// oraclegen.XMageAbility maps each to XMage's "{cost}: Level N" selector, so
// a scenario can reach level N itself: activate the level-up abilities 2..N in
// order, each followed by resolve. XMage does the same in its own Class tests
// (activateAbility(..., "{3}{U}")).
//
// The level-up is sorcery speed, so a prelude belongs in turn 1's first main
// phase, before any pass_to. classLevelPrelude is the ONE home for those
// steps, shared by the trigger, static, cost-static and activator families.

// classLevelGainedSub is the level-B sub-family of a "When this Class becomes
// level N" trigger (CR 716.2e), re-exported from compliance/levelb (which owns
// the classifier) so the recipe and the classifier cannot drift apart.
const classLevelGainedSub = levelb.ClassLevelGainedSub

// classLevelPrelude activates f's own Class level-up abilities in order,
// 2..level, each followed by resolve, and returns the XMage rule-text prefix
// parallel to the steps (the activate step's prefix, "" on each resolve). ok
// is false when the face lacks a required level-up ability, its cost has no
// pool, or XMage's selector is ambiguous, so every caller fails closed and
// keeps its named skip.
func classLevelPrelude(f *cards.Face, name string, level int) (steps []oraclegen.Step, xab []string, ok bool) {
	if level < 2 {
		return nil, nil, false
	}
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return nil, nil, false
	}
	for l := 2; l <= level; l++ {
		idx := classLevelUpIndex(f, l)
		if idx < 0 {
			return nil, nil, false
		}
		cost := f.Abilities[idx].ParamStr(cards.PKCost)
		pool, gap := activationCostIn(cost, "battlefield")
		if gap != "" {
			return nil, nil, false
		}
		prefix, okp := prefixes[idx]
		if !okp {
			return nil, nil, false
		}
		i := idx
		steps = append(steps, oraclegen.Step{
			Op: "activate", Seat: 0, Card: "p0:" + name, Mana: pool,
			AbilityIndex: &i, Answers: activationXAnswers(cost),
		})
		steps = append(steps, oraclegen.Step{Op: "resolve"})
		xab = append(xab, prefix, "")
	}
	return steps, xab, true
}

// classLevelUpIndex is the ability index of the Class level-up activator the
// kw:Class expansion stamped with Level$ level, or -1 when the face has none.
func classLevelUpIndex(f *cards.Face, level int) int {
	want := strconv.Itoa(level)
	for i, sa := range f.Abilities {
		if sa.API != "ClassLevelUp" {
			continue
		}
		if strings.TrimSpace(sa.ParamStr(cards.PKLevel)) == want {
			return i
		}
	}
	return -1
}

// classBandLevel is the ClassBand$ N band a granted trigger body carries (0
// for a body with no band).
func classBandLevel(t *cards.Trigger) int {
	return classBandValue(t.ParamStr(cards.PKClassBand))
}

// classStaticBand is classBandLevel for a static.
func classStaticBand(st *cards.Static) int {
	return classBandValue(st.ParamStr(cards.PKClassBand))
}

// classBandValue parses a ClassBand$ value; a missing or malformed band is 0
// (no band), matching rules' classBandGateHolds fail-closed direction.
func classBandValue(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// classLevelPresentEQ returns k from a self "classLevel_EQ<k>" present filter
// (the kw:Class level-up gate), or 0 when the filter carries no such word.
func classLevelPresentEQ(spec string) int {
	for _, group := range strings.Split(spec, ",") {
		for _, w := range affectedWords(group) {
			rest, ok := strings.CutPrefix(w, "classLevel_EQ")
			if !ok {
				continue
			}
			if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && n >= 1 {
				return n
			}
		}
	}
	return 0
}

// growXAbility returns xa extended to n entries, preserving the existing
// prefixes. An Item's XAbility is parallel to its Scenario.Steps, so appending
// steps to a scenario means extending its XAbility with empty entries first.
func growXAbility(xa []string, n int) []string {
	if len(xa) >= n {
		return xa
	}
	return append(xa, make([]string, n-len(xa))...)
}
