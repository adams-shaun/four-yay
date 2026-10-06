package cards

import (
	"strconv"
)

func kwUndaunted(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// Like Affinity, Undaunted mints a cost-reduction static rather than an
	// ability, so its idempotence rides the shared has("S", ...) arm
	// (which reads f.Statics) on the KeywordLine tag. One shared check, so
	// a second Link() of a cached face does not double the discount and
	// two identical K:Undaunted lines each expand (CR 702.2).
	if has("S", k) {
		return
	}
	sv := "__kwUndaunted" + strconv.Itoa(i)
	f.setSVar(sv, "PlayerCountOpponents")
	p := parseParams("Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | EffectZone$ All | Amount$ " + sv +
		" | Description$ This spell costs {1} less to cast for each opponent you have.")
	p["KeywordLine"] = k
	f.Statics = append(f.Statics, Static{Mode: "ReduceCost", Params: p})
}

func init() { registerKeyword(kwUndaunted, "Undaunted") }
