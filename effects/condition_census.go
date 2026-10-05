package effects

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// condition_census.go is the ONE classifier of which Condition* shapes this
// build evaluates. Two consumers read it:
//
//   - UnmodelledCondition, which the resolve walk (effects/registry.go) uses
//     to fail CLOSED with a replay-visible Note on a sub whose gate names a
//     shape this build cannot evaluate, instead of running it unconditionally;
//   - TestConditionCorpusCensus (condition_census_test.go), the ratchet over
//     the whole card corpus: every Condition* key and ConditionDefined$/
//     Condition$ value that appears anywhere in .cards/cardsfolder must be
//     either evaluated or listed here as unmodelled. A new carrier fails the
//     test until it is classified, so the gap can never silently grow.
//
// The evaluated-key set is conditionEvaluatedKeys (activation_params.go); the
// value sets below are the supported ConditionDefined$ groups and bare
// Condition$ values. Everything the corpus carries that is not in those sets
// is enumerated in the two unmodelled sets, so the census is exhaustive by
// construction.

// conditionNames is census data, not a dispatch table: entries carry only
// classification, while the evaluator owns the behavior of each group.
type conditionNames map[string]bool

func (names conditionNames) Has(name string) bool { return names[name] }

func newConditionNames(values ...string) conditionNames {
	result := make(conditionNames, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

// conditionSupportedDefined is the set of ConditionDefined$ groups
// conditionMetCore enumerates. ThisTargetedCard is the resolving ability's own
// answered targets, the same group Targeted reads (Throw from the Saddle,
// Joust, Malamet Battle Glyph).
var conditionSupportedDefined = newConditionNames(
	"Remembered", "Self", "TriggeredCard", "TriggeredCardLKICopy",
	"Imprinted", "Discarded", "Targeted", "Returned", "ChosenCard",
	"TriggeredSourceLKICopy", "RememberedLKI", "ParentTarget", "Sacrificed",
	"ThisTargetedCard", "TriggeredSpellAbility",
	"Collected", "CastSA>Collected",
)

// conditionUnmodelledDefined is every ConditionDefined$ group the corpus
// carries that conditionMetCore cannot enumerate. Each entry is a real
// carrier (counts measured 2026-10-04); a new value fails the census test.
var conditionUnmodelledDefined = newConditionNames(
	"TriggeredAttackerLKICopy",  // 7
	"DelayTriggerRememberedLKI", // 6
	"Equipped",                  // 5
	"DelayTriggerRemembered",    // 5
	"TriggeredTargetLKICopy",    // 4
	"TriggeredNewCardLKICopy",   // 4
	"ReplacedSource",            // 3
	"TopOfLibrary",              // 2
	"RememberedCard",            // 2
	"ExiledWithSource",          // 2
	"ExiledWith",                // 2
	"Enchanted",                 // 2
	"Tapped",                    // 1
	"OriginalHost",              // 1
)

// conditionSupportedBare is every bare Condition$ value conditionMetCore
// evaluates (the case-insensitive switch in its bare branch).
var conditionSupportedBare = newConditionNames(
	"Kicked", "Foretold", "Revolt", "Delirium", "Metalcraft", "Blessing",
)

// conditionUnmodelledBare is every bare Condition$ value the corpus carries
// that conditionMetCore does not evaluate. Counts measured 2026-10-04.
var conditionUnmodelledBare = newConditionNames(
	"PlayerTurn", "Threshold", "MaxSpeed", "NotPlayerTurn", "Hellbent",
	"OptionalCost", "EnduringStory", "Bargain", "NoOpponentHasMoreLifeThanAttacked",
	"Monarch", "FatefulHour", "Evolve", "Surge", "Sacrificed", "Night",
	"LifePaid", "Ferocious", "ExtraTurn", "AttackerHasUnattackedOpp",
	"AttackedPlayerWithMostLife", "Add",
)

// conditionUnmodelledKeys is every Condition* key the corpus carries that
// conditionMetCore does not read (the keys are read through the raw Params
// map; the evaluated set is conditionEvaluatedKeys). Counts measured
// 2026-10-04.
var conditionUnmodelledKeys = newConditionNames(
	"ConditionManaSpent",            // 34
	"ConditionOptionalPaid",         // 10
	"ConditionPlayerDefined",        // 9
	"ConditionPlayerContains",       // 9
	"ConditionManaNotSpent",         // 5
	"ConditionChosenColor",          // 5
	"ConditionLifeTotal",            // 4
	"ConditionLifeAmount",           // 4
	"ConditionTargetValidTargeting", // 3
	"ConditionGameTypes",            // 3
	"ConditionTargetsSingleTarget",  // 2
	"ConditionYouCastThisTurn",      // 1
	"ConditionWouldDestroy",         // 1
	"ConditionOpponentTurn",         // 1
	"ConditionNoDifferentColors",    // 1
)

// unmodelledConditionKey returns the first (sorted, deterministic) Condition*
// key sa carries that conditionMetCore does not evaluate, or "" if every key
// is evaluated. ConditionDescription$ is display text, never evaluation.
func unmodelledConditionKey(sa *cards.SA) string {
	if sa == nil {
		return ""
	}
	var bad []string
	for k := range sa.Params {
		if !strings.HasPrefix(k, "Condition") || k == "ConditionDescription" {
			continue
		}
		if !conditionEvaluatedKeys.Has(k) {
			bad = append(bad, k)
		}
	}
	sort.Strings(bad)
	if len(bad) == 0 {
		return ""
	}
	return bad[0]
}

// UnmodelledCondition reports whether sa carries a Condition* shape this
// build cannot evaluate, and names the first unmodelled key/value for a
// replay-visible Note. It is the fail-closed classifier the resolve walk
// consults: a sub with such a shape is skipped with a Note rather than run
// unconditionally. A sub with NO condition key at all (the ungated shape) is
// unmodelled=false -- it is not gated, so there is nothing to fail closed on.
func UnmodelledCondition(sa *cards.SA) (string, bool) {
	if sa == nil {
		return "", false
	}
	if k := unmodelledConditionKey(sa); k != "" {
		return k, true
	}
	cp := &ActivationOf(sa).Cond
	if cp.Defined != "" && !conditionSupportedDefined.Has(cp.Defined) {
		return "ConditionDefined$ " + cp.Defined, true
	}
	if cp.Bare != "" && !conditionSupportedBare.Has(cp.Bare) {
		return "Condition$ " + cp.Bare, true
	}
	return "", false
}

// unmodelledConditionDetail adds value-level filter gaps to the static census.
// A valid, recognized group can still fail to resolve when its filter contains
// an unknown predicate; genuinely pending groups (for example a target ask not
// answered yet) remain unresolved without being mislabeled as unsupported.
func unmodelledConditionDetail(sa *cards.SA) (string, bool) {
	if detail, bad := UnmodelledCondition(sa); bad {
		return detail, true
	}
	if sa == nil {
		return "", false
	}
	cp := &ActivationOf(sa).Cond
	if cp.Zone != "" {
		if _, ok := parseZone(cp.Zone); !ok {
			return "ConditionZone$ " + cp.Zone, true
		}
	}
	for _, item := range []struct{ key, value string }{
		{key: "ConditionCompare$", value: cp.Compare.Text},
		{key: "ConditionCompare2$", value: cp.Compare2.Text},
	} {
		if item.value != "" {
			if _, _, ok := parseConditionCompare(item.value); !ok {
				return "unresolved " + item.key + " " + item.value, true
			}
		}
	}
	for _, spec := range []string{cp.Present.Text, cp.Present2.Text, cp.NotPresent} {
		if len(UnknownPredicates(spec)) > 0 {
			return "unresolved Condition* predicate", true
		}
	}
	return "", false
}
