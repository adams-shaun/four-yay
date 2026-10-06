package adopt

import (
	"fmt"
	"sort"
)

// CheckRatchetB compares the sets measured at level B with the committed
// level-B floors. Only entries that carry an OutstandingB floor are checked
// (a nil floor means "never measured at B", so any value passes and the set
// is not part of the claim yet). A measured B count above its floor is a
// hard regression; one below it is slack and is logged, exactly as
// CheckRatchet treats the level-A count. CheckRatchetB never fails a set
// missing from the ratchet: only the level-A ratchet requires every
// committed set, and the B claim grows set by set.
func CheckRatchetB(measured []SetStatus, ratchet map[string]RatchetEntry) (fails, slack []string) {
	have := map[string]SetStatus{}
	for _, s := range measured {
		have[s.Set] = s
	}
	keys := make([]string, 0, len(ratchet))
	for k := range ratchet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := ratchet[k]
		if e.OutstandingB == nil {
			continue // not measured at B: no floor to hold
		}
		s, ok := have[k]
		if !ok {
			continue // not measured in this run
		}
		if s.Outstanding > *e.OutstandingB {
			fails = append(fails, fmt.Sprintf("%s: %d outstanding at level B, ratchet %d -- per-set outstanding only shrinks (oraclediff status -set %s -level B)", k, s.Outstanding, *e.OutstandingB, k))
		} else if s.Outstanding < *e.OutstandingB {
			slack = append(slack, fmt.Sprintf("%s: %d -> %d", k, *e.OutstandingB, s.Outstanding))
		}
	}
	return fails, slack
}
