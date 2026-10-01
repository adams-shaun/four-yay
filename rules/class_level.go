// The Class level band (kw:Class, CR 702.118). The cards package's kwClass
// expansion appends each level's granted body to the face and stamps the body
// with a dedicated ClassBand$ N parameter; this file holds the ONE evaluator
// every gate family calls beside its own IsPresent$ read, so a granted body is
// live exactly while its source Class carries at least N LEVEL counters.
//
// Why a dedicated parameter rather than IsPresent$: the band is a SELF-check
// of the source permanent (it counts the source object, not the battlefield),
// while a body that carries its own IsPresent$ counts an unrelated set. The
// trigger gate once read IsPresent$+IsPresent2$ as a UNION, so folding the
// band into IsPresent2$ ORed it with the body's own condition -- Hunter's
// Talent's level-3 end-step draw fired at level 1 with a power-4 creature out
// -- and replacementConditionHolds reads no IsPresent2$ at all, so the band
// vanished there. (The trigger gate now ANDs the two clauses, but a body may
// carry its own IsPresent2$, so the band still needs its own parameter.) ClassBand$ has no other
// meaning, so every family can AND it in without colliding with a body clause.

package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// classBandGateHolds evaluates a granted body's ClassBand$ band: the source
// object carries at least N LEVEL counters. A body without the parameter is
// ungated (true), so every non-Class static/trigger/replacement is unaffected.
// A malformed or unreadable band fails closed -- a granted body whose level is
// not proven is never live, the family's fail-closed direction.
func (e *Engine) classBandGateHolds(band string, source state.ObjID) bool {
	raw := strings.TrimSpace(band)
	if raw == "" {
		return true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return false
	}
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	return int(o.Counter("LEVEL")) >= n
}
