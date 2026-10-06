package rules

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// maxSetupSpeed is the top of a seat's speed (CR 702.179b); events.Apply
// clamps to it, so a larger setup value is a scenario typo, not a request.
const maxSetupSpeed = 4

// validateSetupState rejects a seat's counters/speed that the runner cannot
// place: a counters key naming no battlefield card of that seat, a kind with
// no name, a non-positive amount, or a speed outside 0..4. It runs before the
// game is built so a typo fails loudly rather than silently placing nothing.
func validateSetupState(p int, s oracleSeat) error {
	for _, name := range sortedSetupKeys(s.Counters) {
		onField := false
		for _, n := range s.Battlefield {
			onField = onField || cards.NormalizeName(n) == cards.NormalizeName(name)
		}
		if !onField {
			return harnessf("setup: p%d counters name %q, which is not on its battlefield", p, name)
		}
		for _, kind := range sortedSetupKeys(s.Counters[name]) {
			if kind == "" || s.Counters[name][kind] <= 0 {
				return harnessf("setup: p%d counters %q/%q: want a named kind and a positive amount", p, name, kind)
			}
		}
	}
	if s.Speed < 0 || s.Speed > maxSetupSpeed {
		return harnessf("setup: p%d speed %d (want 0..%d)", p, s.Speed, maxSetupSpeed)
	}
	return nil
}

// emitSetupCounters places the seat's setup counters on the battlefield
// placement id (a card named name), as ordinary CounterChange events. Like
// `tapped`, a counters entry applies to every placement of its name. Keys and
// kinds go in sorted order so the log is deterministic.
func emitSetupCounters(emit func(events.Event) events.Event, s oracleSeat, name string, id state.ObjID) {
	for _, key := range sortedSetupKeys(s.Counters) {
		if cards.NormalizeName(key) != cards.NormalizeName(name) {
			continue
		}
		for _, kind := range sortedSetupKeys(s.Counters[key]) {
			emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: s.Counters[key][kind]})
		}
	}
}

// emitSetupSpeed raises the seat's speed to its setup value with one
// SpeedChange event (a fresh seat starts at 0).
func emitSetupSpeed(emit func(events.Event) events.Event, p int, current int32, s oracleSeat) {
	if d := s.Speed - current; d != 0 {
		emit(events.Event{Kind: events.SpeedChange, Player: state.PlayerID(p), Amount: d})
	}
}

// sortedSetupKeys returns m's keys in sorted order, so no map range reaches
// an event.
func sortedSetupKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
