package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Paradigm (the sos set's Lesson mechanic, 5 corpus cards) is a bare keyword:
//
//	K:Paradigm
//
// Oracle: "Then exile this spell. After you first resolve a spell with this
// name, you may cast a copy of it from exile without paying its mana cost at
// the beginning of each of your first main phases."
//
// Two rules-side reads implement it, both keyed off the same hasParadigm
// predicate so a card can never be exiled by one and offered by another:
//
//   - rules/stack.go's spellRestZone sends a resolved Paradigm spell to exile
//     instead of the graveyard (the "Then exile this spell" rider). A spell
//     that FIZZLES does not get this -- spellFizzleZone deliberately omits
//     hasParadigm, so a countered/fizzled Paradigm reaches the graveyard.
//   - paradigmMayPlay is one source in rules/mayplay.go's mayPlayGrantScoped:
//     while the exiled card's owner has already resolved a Paradigm spell of
//     that name and it is that owner's first main phase, the ordinary
//     may-play machinery offers it as a FREE cast from exile. Casting the
//     card moves it to the stack and its own spellRestZone returns it to
//     exile, so the lone physical card stands in for the rules text's "cast a
//     copy ... while the card remains there" without a hand-rolled cast path.
//
// The registration is what removes kw:Paradigm from cards.Registry.Unsupported
// (the census in rules/setaudit_sos_test.go names the five carriers).
func init() { effects.RegisterNonAPI("kw:Paradigm") }

// hasParadigm reports whether o's face carries the Paradigm keyword. The
// object may be a spell on the stack (spellRestZone) or the exiled card
// (paradigmMayPlay); both read the same printed face.
func hasParadigm(o *state.Object) bool {
	return o != nil && o.Face() != nil && o.Face().HasKeyword("Paradigm")
}

// paradigmMayPlay reports whether the exiled Paradigm card o is a free
// castable copy for player p right now: p owns it, a spell with its name has
// already resolved for p, and it is p's first main phase.
func (e *Engine) paradigmMayPlay(p state.PlayerID, o *state.Object) bool {
	if o == nil || o.Zone != state.ZExile || !hasParadigm(o) || o.Face() == nil {
		return false
	}
	if o.Owner != p {
		return false
	}
	if e.G.Step != state.StepMain1 || e.G.Active != p {
		return false
	}
	return e.paradigmResolved(p, o.Face().Name)
}

// paradigmResolved reports whether p has already resolved a Paradigm spell
// named name. Resolve events are the replay-authoritative history: the exiled
// card keeps its id, so reading the object back from a Resolve event names the
// very spell that resolved. A card that reached exile any other way (a mill, an
// opponent's exile effect) has no such event and grants nothing.
func (e *Engine) paradigmResolved(p state.PlayerID, name string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind != events.Resolve {
			continue
		}
		o := e.G.Obj(ev.Obj)
		if o == nil || o.Face() == nil || o.Owner != p {
			continue
		}
		if o.Face().Name == name && hasParadigm(o) {
			return true
		}
	}
	return false
}
