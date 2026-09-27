package paymirror

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/events"
)

// routeEvent reports whether an event records the ROUTE rather than the game:
// the decision bookkeeping (DecisionAsk / DecisionMade, whose count and Text
// differ because the manual route answers one decision per activation and the
// planned route's DecisionMade carries the payment suffix) and the priority
// pass-count resets every non-pass priority action emits (one per manual
// activation). Their game-visible consequence -- who holds priority and the
// pass count -- is compared as state (G.Priority, G.Passes).
func routeEvent(ev events.Event) bool {
	switch ev.Kind {
	case events.DecisionAsk, events.DecisionMade, events.Priority:
		return true
	}
	return false
}

// eventKey renders every field of an event except Seq, the log position,
// which legitimately shifts by the route's extra decision events.
func eventKey(ev events.Event) string {
	var sb strings.Builder
	sb.WriteString(ev.Kind.String())
	sb.WriteString(" p=" + strconv.Itoa(int(ev.Player)))
	if ev.Obj != 0 {
		sb.WriteString(" obj=" + strconv.FormatUint(uint64(ev.Obj), 10))
	}
	if ev.From != 0 || ev.To != 0 {
		sb.WriteString(" " + strconv.Itoa(int(ev.From)) + "->" + strconv.Itoa(int(ev.To)))
	}
	if ev.Amount != 0 {
		sb.WriteString(" amt=" + strconv.FormatInt(int64(ev.Amount), 10))
	}
	if ev.Step != 0 {
		sb.WriteString(" step=" + strconv.Itoa(int(ev.Step)))
	}
	if ev.Counter != "" {
		sb.WriteString(" ctr=" + strconv.Quote(ev.Counter))
	}
	if ev.Text != "" {
		sb.WriteString(" text=" + strconv.Quote(ev.Text))
	}
	if len(ev.IDs) > 0 {
		sb.WriteString(" ids=[")
		for i, id := range ev.IDs {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(strconv.FormatUint(uint64(id), 10))
		}
		sb.WriteString("]")
	}
	if len(ev.Pairs) > 0 {
		sb.WriteString(" pairs=[")
		for i, p := range ev.Pairs {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(strconv.FormatUint(uint64(p[0]), 10) + ":" + strconv.FormatUint(uint64(p[1]), 10))
		}
		sb.WriteString("]")
	}
	if ev.Secret {
		sb.WriteString(" secret")
	}
	return sb.String()
}

// gameEvents is the route-independent part of an event stream, in order.
func gameEvents(evs []events.Event) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		if routeEvent(ev) {
			continue
		}
		out = append(out, eventKey(ev))
	}
	return out
}

// eventDiff compares the two game-event streams recorded since the fork. The
// multiset comparison is the equivalence criterion: the float-then-cast route
// activates its sources BEFORE the cast begins (at priority), where the
// planned route activates them inside the cast's CR 601.2g window, so the
// same events legitimately arrive in a different order. orderDiffers reports
// the stricter ordered comparison as information.
type eventDiff struct {
	OnlyA        []string
	OnlyB        []string
	OrderDiffers bool
}

const maxEventDiffs = 24

func compareEvents(a, b []events.Event) eventDiff {
	ga, gb := gameEvents(a), gameEvents(b)
	var out eventDiff
	if len(ga) == len(gb) {
		for i := range ga {
			if ga[i] != gb[i] {
				out.OrderDiffers = true
				break
			}
		}
	} else {
		out.OrderDiffers = true
	}
	count := make(map[string]int, len(ga))
	for _, k := range ga {
		count[k]++
	}
	for _, k := range gb {
		count[k]--
	}
	keys := make([]string, 0, len(count))
	for k, n := range count {
		if n != 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := count[k]
		for ; n > 0 && len(out.OnlyA) < maxEventDiffs; n-- {
			out.OnlyA = append(out.OnlyA, k)
		}
		for ; n < 0 && len(out.OnlyB) < maxEventDiffs; n++ {
			out.OnlyB = append(out.OnlyB, k)
		}
	}
	return out
}

// eventKindOf extracts the kind name an eventKey starts with.
func eventKindOf(key string) string {
	if i := strings.IndexByte(key, ' '); i >= 0 {
		return key[:i]
	}
	return key
}
