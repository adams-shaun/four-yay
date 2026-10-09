// Causes for the zone-change residue shapes (ticket g17, wave 3 "trigger
// with no recipe"): a "when the chosen creature leaves" trigger
// (ChosenCardStrict), a library-to-graveyard trigger, and a
// "from anywhere" put-into-the-graveyard trigger. Each has a turn-1 cause:
//
//   - library to graveyard: a mill probe aimed at p0 puts land cards from
//     p0's library into its graveyard.
//   - from anywhere to the graveyard: a destroy probe sends a p0 permanent
//     there; a trigger whose IsPresent$ demands a counter on the source
//     starts with it.
//
// The engine stays the authority on every filter; a cause that cannot fire
// keeps the residue's named skip.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/effects"
)

// millProbes are the spells that put cards from p0's library into p0's
// graveyard; the scenario library is the runner's Wastes fill, so a mill
// lands land cards.
var millProbes = []string{"Tome Scour"}

// residueCauses builds the causes of one zone-change-residue shape. ok is
// false when the trigger is none of them (the named skip stands). A
// ChosenCardStrict filter keeps its named skip: the trigger matcher's filter
// context binds no chosen set, so the predicate fails closed and the
// trigger cannot fire (ticket g17 follow-up, engine side).
func residueCauses(reg *cards.Registry, name string, t *cards.Trigger, filter string) ([]triggerCause, string, bool) {
	origin := strings.ToLower(t.ParamStr(cards.PKOrigin))
	dest := strings.ToLower(t.ParamStr(cards.PKDestination))
	switch {
	case strings.Contains(origin, "library") && strings.Contains(dest, "graveyard"):
		return libraryToGraveyardCauses(reg, name, t)
	case strings.Contains(dest, "graveyard"):
		return anyToGraveyardCauses(reg, name, t)
	}
	return nil, "", false
}

// libraryToGraveyardCauses mills p0 with the mill probes, putting land cards
// from the library into p0's graveyard. It refuses a trigger whose body's
// ChangeType$ spec carries a predicate the filter grammar does not know: the
// trigger would fire and its body would silently move nothing, so the
// scenario would compare a no-op against XMage's move. (Hedge Shredder's
// Card.TriggeredCards was the historical example; it is a registered
// predicate since commit 8e9f1097c and generates its mill cause.)
func libraryToGraveyardCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	for _, body := range triggerBodyLines(t) {
		for _, spec := range changeTypeSpecs(body) {
			if len(effects.UnknownPredicates(spec)) > 0 {
				return nil, "", false
			}
		}
	}
	var out []triggerCause
	for _, p := range millProbes {
		if c, ok := castCause(reg, name, p, "p0"); ok {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "", false
	}
	return out, "", true
}

// triggerBodyLines is the trigger's Execute body's SVar text and its inline
// effect line.
func triggerBodyLines(t *cards.Trigger) []string {
	var out []string
	if t.Effect != nil && t.Effect.Line != "" {
		out = append(out, t.Effect.Line)
	}
	if ex := t.ParamStr(cards.PKExecute); ex != "" {
		out = append(out, ex)
	}
	return out
}

// changeTypeSpecs extracts each ChangeType$ value of a DB$ line.
func changeTypeSpecs(line string) []string {
	var out []string
	for _, field := range strings.Split(line, "|") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(field), "ChangeType$ "); ok {
			out = append(out, v)
		}
	}
	return out
}

// anyToGraveyardCauses destroys a p0 permanent, putting it into p0's
// graveyard from anywhere. A trigger whose IsPresent$ gates on a counter on
// the source may already satisfy the gate with what the source enters with
// (its own ETB counters); the cause without setup counters runs first so a
// source that would die at the gate is not killed by them.
func anyToGraveyardCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	counters := map[string]map[string]int{}
	if kind, n, gated := levelb.SelfCounterGate(t.ParamStr(cards.PKIsPresent)); gated {
		counters = map[string]map[string]int{"__SOURCE__": {kind: n}}
	}
	var out []triggerCause
	for _, withCounters := range []bool{false, true} {
		for _, p := range destroyProbes {
			c, ok := castCause(reg, name, p, "p0:"+bearsProbe)
			if !ok {
				continue
			}
			c.battlefield = []string{bearsProbe}
			if withCounters {
				c.counters = counters
			}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "", false
	}
	return out, "", true
}
