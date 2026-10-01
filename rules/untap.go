package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// untapReplacementSources walks all zones from which an UntapOtherPlayer
// static can apply. The order is stable and also admits the command-zone
// plane shape in the corpus.
func (e *Engine) untapReplacementSources(each func(id state.ObjID)) {
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			each(id)
		}
		for _, id := range e.G.Zone(state.ZCommand, p) {
			each(id)
		}
	}
}

// hasUntapStepChoice recognizes Forge's bare keyword line verbatim. It is
// intentionally a characteristic check, not a card-name list: all 45 corpus
// carriers, including future cards with the same wording, take this path.
func hasUntapStepChoice(o *state.Object) bool {
	if o == nil || o.Face() == nil {
		return false
	}
	for _, k := range o.Face().Keywords {
		if strings.TrimSpace(k) == "You may choose not to untap CARDNAME during your untap step." {
			return true
		}
	}
	return false
}

// untapTurnPermanent is the one turn-step caller of effects.TryUntap. The
// effect helper also owns ability untaps, so stun counters replace either
// kind of untap identically (CR 122.1d).
func (e *Engine) untapTurnPermanent(subject state.ObjID) {
	effects.TryUntap(e, subject)
}

// staticPresentHolds implements a static's IsPresent$/PresentCompare$ gate.
// The default compare is presence. It deliberately uses the same
// deterministic battlefield matcher as trigger intervening-if conditions.
func (e *Engine) staticPresentHolds(st cards.Static, source state.ObjID) bool {
	if !e.classBandGateHolds(st.ParamStr(cards.PKClassBand), source) {
		return false
	}
	spec, ok := st.Params["IsPresent"]
	if !ok {
		return true
	}
	n := e.countPresent(spec, source, e.controllerOf(source))
	cmp := strings.TrimSpace(st.Params["PresentCompare"])
	if cmp == "" {
		return n > 0
	}
	return comparePresent(n, e.presentCompareFor(cmp, source, e.controllerOf(source)))
}

// untapOtherStaticsMatch reports whether subject untaps during a foreign
// player's untap step under a matching UntapOtherPlayer static. ValidCard
// defaults to Card.Self; IsPresent$/PresentCompare$ gates are checked before
// the subject filter (Quest for Renewal's four quest counters shape).
func (e *Engine) untapOtherStaticsMatch(subject state.ObjID) bool {
	matched := false
	e.untapReplacementSources(func(id state.ObjID) {
		if matched {
			return
		}
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		for _, st := range o.Face().Statics {
			if st.Mode != "UntapOtherPlayer" || !e.staticPresentHolds(st, id) {
				continue
			}
			spec := st.Params["ValidCard"]
			if spec == "" {
				spec = "Card.Self"
			}
			if e.matchesSpecFrom(spec, subject, e.controllerOf(id), id) {
				matched = true
				return
			}
		}
	})
	return matched
}
