package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// SacrificedOnceMatches implements Mode$ SacrificedOnce ("Whenever you
// sacrifice one or more Foods ...", CR 701.21; Camellia, the Seedmiser).
// The event under test is the same canonical sacrifice MoveZone marker the
// plain Mode$ Sacrificed reads (events.IsSacrifice), and ValidCard$ /
// ValidPlayer$ filter it identically -- so this delegates to
// sacrificedMatches rather than duplicating the LKI/filter logic. The ONLY
// difference from Sacrificed is cadence: a multi-permanent sacrifice action
// must fire this trigger ONCE, which is the sacrifice-batch latch in
// rules/trigger_match.go, not a property of the matcher.
func SacrificedOnceMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	return sacrificedMatches(e, t, source, ev, lki)
}

func init() {
	registerTrigMatcher(SacrificedOnceMatches, "SacrificedOnce")
}
