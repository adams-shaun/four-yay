// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import (
	"strconv"
	"strings"
)

func kwAffinity(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.41a: affinity for <spec> is a cost-reduction static, not an
	// ability, so its idempotence keys on the minted static itself through
	// the shared has("S", ...) arm (which reads f.Statics), carrying the
	// same KeywordLine tag the other cases set. Without it a second Link()
	// (cards/registry.go re-runs f.link() on cached faces that predate a
	// newly added expansion) would append a SECOND reduction and double the
	// discount, replay-visibly; and keying it on the shared has() rather
	// than a local scan is what lets two identical K:Affinity: lines each
	// expand, the CR 702.2 instance rule the other keywords follow.
	if has("S", k) {
		return
	}
	// param is "<spec>", occasionally followed by a human-readable
	// description after a second colon ("Land.Snow:snow land",
	// "Permanent.token:token", "Creature.Artifact:artifact creature" --
	// 3 corpus lines): only the first field is the count spec, exactly
	// the trailing-field strip the etbCounter and Equip cases do.
	spec, desc, _ := strings.Cut(param, ":")
	if desc == "" {
		desc = spec
	}
	sv := "__kwAffinity" + strconv.Itoa(i)
	// Count$Valid counts BATTLEFIELD objects (effects/count.go's countZone
	// maps "Valid" to ZBattlefield), so the "you control" qualifier lives
	// inside the spec. The joining separator is load-bearing: the matcher
	// splits base from predicates on the FIRST dot
	// (effects/filter.go MatchesObjectCtx), so a dot-less spec joined with
	// '+' ("Food+YouCtrl") reads the whole thing as one base type word and
	// fails closed to 0 -- but a spec that already carries a dot
	// ("Land.Snow", "Permanent.token") must join with '+' (the corpus's
	// measured-working "Swamp.Snow+YouCtrl" shape), because a second dot
	// would glue "YouCtrl" onto the previous predicate token, which is
	// unknown and fails closed just as hard.
	sep := "."
	if strings.ContainsRune(spec, '.') {
		sep = "+"
	}
	f.setSVar(sv, "Count$Valid "+spec+sep+"YouCtrl")
	p := parseParams("Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | EffectZone$ All | Amount$ " + sv +
		" | Description$ This spell costs {1} less to cast for each " + desc + " you control.")
	// EffectZone$ All keeps the reduction live from hand/library (the
	// Ghalta precedent in rules/statics.go's collectCostStatics doc);
	// no Color$ (affinity reduces generic only -- CR 702.41a) and no
	// Relative$ (that flag is for X-dependent amounts).
	p["KeywordLine"] = k
	f.Statics = append(f.Statics, Static{Mode: "ReduceCost", Params: p})
}

func init() { registerKeyword(kwAffinity, "Affinity") }
