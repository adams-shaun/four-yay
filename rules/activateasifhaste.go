package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// stat:ActivateAbilityAsIfHaste (CR 302.6's activation exception) — "You may
// activate abilities of creatures you control as though those creatures had
// haste." Shang-Chi, Master of Kung Fu is the Standard carrier; Dynaheir,
// Invoker of Terrors, Thousand-Year Elixir and Tyvar Kell are the corpus-wide
// rest, in two shapes that share the same grammar: a ValidCard$ creature scope
// (Creature.YouCtrl, sometimes +inZoneBattlefield or Creature.Other+YouCtrl).
//
// It is a timing exception, NOT a haste grant: it lets the static's controller
// pay a {T}/{Q} activation cost with a summoning-sick creature, but the
// creature still cannot attack or block (CR 302.6's combat half is untouched).
// It is read at the ONE activation-sickness predicate, pay.TapFlagsSick, via
// the bool the callers thread in, so every {T}/{Q} cost site -- the generic
// battlefield activation walk and the mana-activation walk alike -- is covered
// by construction.
//
// actsAsIfHaste takes the narrow activateAsIfHasteEngine interface rather than
// *Engine so it is not counted by the engineSurface/engineMethodCount
// shrink-only ratchets (it is a free function, not an Engine method).

// activateAsIfHasteEngine is the slice of Engine the exemption read needs.
type activateAsIfHasteEngine interface {
	activeStatics(mode string) []staticView
	staticGateHolds(sv staticView) bool
	matchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool
	staticSpecCtx(sv staticView) effects.SpecContext
}

// activatesAsIfHaste reports whether an active Mode$ ActivateAbilityAsIfHaste
// static lets id's controller activate a {T}/{Q} ability of id as though id
// had haste. A static whose Condition$/IsPresent$ gate does not hold, or whose
// ValidCard$ does not select id, is not a match. A static with no ValidCard$
// is skipped (fail closed): the corpus always scopes the grant, and an
// unscoped reading would exempt every permanent on the board.
func activatesAsIfHaste(e activateAsIfHasteEngine, id state.ObjID) bool {
	for _, sv := range e.activeStatics("ActivateAbilityAsIfHaste") {
		if !e.staticGateHolds(sv) {
			continue
		}
		spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard))
		if spec == "" {
			continue
		}
		if e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}
