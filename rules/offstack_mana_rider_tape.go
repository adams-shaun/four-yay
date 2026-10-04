package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// tapeManaRiderMayAsk reports whether activating obj's mana ability at
// priority may pose a rider ask: one of its available mana abilities carries
// a SubAbility$ chain the text allowlist says may ask (a target, a mode, a
// name, an arrangement). A mana ability never uses the stack (CR 605.3), so
// the rider resolves inside the activating Submit; that Submit is then a
// tape run (StartsResolution) so the rider's ask is posed and answered from
// the tape instead of taking its no-run default. A plain mana ability, or
// one whose rider is ask-free (a painland's damage), is no run at all.
func tapeManaRiderMayAsk(e *Engine, p state.PlayerID, obj state.ObjID) bool {
	o := e.G.Obj(obj)
	if o == nil {
		return false
	}
	free := manaRiderFree(e, o)
	if free && !tapeRiderVerify {
		return false
	}
	if free {
		// Verify the superset claim itself: no available member carries a
		// SubAbility$ at all, asking or not.
		for _, ma := range e.availableManaAbilitiesForWindow(p, obj, true) {
			if ma != nil && ma.Sub != nil {
				panic(fmt.Sprintf("rules: manaRiderFree cleared obj %d, but its available mana ability %q carries a SubAbility$", obj, ma.Line))
			}
		}
		return false
	}
	return tapeManaRiderWalk(e, p, obj, o)
}

// tapeManaRiderWalk is tapeManaRiderMayAsk's eligibility walk.
func tapeManaRiderWalk(e *Engine, p state.PlayerID, obj state.ObjID, o *state.Object) bool {
	f := o.Face()
	var svars map[string]string
	if f != nil {
		svars = f.SVars
	}
	for _, ma := range e.availableManaAbilitiesForWindow(p, obj, true) {
		if ma != nil && ma.Sub != nil && cards.SAChainMayAsk(ma.Sub, svars, f, false) {
			return true
		}
	}
	return false
}

// offStackTapeServed reports whether an ask inside the open off-stack mana
// frame is served from the tape: the frame was opened at the top level of a
// watched run (a priority mana activation tapeManaRiderMayAsk made a run, or
// a resolution activating one) with no payment window or cast open around
// it. Inside a window (cumulative upkeep, a triggered or unless cost) or a
// cast's payment the frame keeps its own routing.
func offStackTapeServed(e *Engine) bool {
	f := e.offStackMana
	return f != nil && e.tape.Watching() && e.cast == nil && !e.tapeWindowAsking &&
		!f.baseUnless && !f.baseCumulative && !f.baseTriggerCost && e.echo == nil
}

// tapeRiderVerify (tests) makes tapeManaRiderMayAsk run its full walk even
// when manaRiderFree answers, so a test can hold the two to agree.
var tapeRiderVerify bool

// manaRiderFree reports that no mana ability obj could have carries a
// SubAbility$ at all, without the eligibility walk: every candidate source
// availableManaAbilitiesForWindow reads -- the object's printed and pile
// faces' mana and ManaReflected abilities, the CR 305.6 intrinsics (never a
// rider), a Continuous AddAbility$ grant and an AddAbilities grant -- is
// rider-free. A grant anywhere on the board answers false (walk instead).
// The gates the walk applies only remove members, so a rider-free superset
// proves the walk's answer is false.
func manaRiderFree(e *Engine, o *state.Object) bool {
	if o.CopyFace != nil && !faceManaRiderFree(o.CopyFace) {
		return false
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f != nil && !faceManaRiderFree(f) {
				return false
			}
		}
	}
	for i := range o.MergedCards {
		if c := o.MergedCards[i].Card; c != nil {
			for _, f := range c.Faces {
				if f != nil && !faceManaRiderFree(f) {
					return false
				}
			}
		}
	}
	if e.activeSummaryOf(e.active()).hasGrants {
		return false
	}
	return len(e.collectAddAbilityCarriers()) == 0
}

// faceManaRiderFree reports that none of f's mana or ManaReflected abilities
// carries a SubAbility$.
func faceManaRiderFree(f *cards.Face) bool {
	for _, ma := range f.ManaAbilities() {
		if ma != nil && ma.Sub != nil {
			return false
		}
	}
	for _, ma := range f.Abilities {
		if ma != nil && ma.API == "ManaReflected" && ma.Sub != nil {
			return false
		}
	}
	return true
}
