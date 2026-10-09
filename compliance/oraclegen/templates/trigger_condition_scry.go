// Turn-history preludes for "you've scried this turn" and "you've surveilled
// this turn" (Proctor of Potential's CheckSVar$ Count$YouScryThisTurn/Plus.Y).
// The shared history table (trigger_condition_history.go) has no head for
// these; a scry or surveil event is a cast and resolve, exactly the shape
// historyCasts already builds. The probe lists are the event recipes'
// (trigger_recipes_events.go).
package templates

import (
	"github.com/adams-shaun/gorge/cards"
)

// scryHistory casts n distinct scry spells and resolves them.
func scryHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	return castCountHistory(reg, src, scryProbes, n)
}

// surveilHistory casts n distinct surveil spells and resolves them.
func surveilHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	return castCountHistory(reg, src, surveilProbes, n)
}

// castCountHistory casts the first n distinct probes (skipping src) and
// resolves each. nil when the pool is short or a probe is not castable.
func castCountHistory(reg *cards.Registry, src string, pool []string, n int) []conditionPrelude {
	spells := firstN(reg, pool, src, n)
	if spells == nil {
		return nil
	}
	steps, ok := historyCasts(reg, spells, nil)
	if !ok {
		return nil
	}
	return []conditionPrelude{{hand: spells, steps: steps}}
}
