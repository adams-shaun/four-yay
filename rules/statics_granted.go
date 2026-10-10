package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// grantedStatics collects every AddStaticAbility$-granted static of the named
// Mode$ that is live on the battlefield right now -- the depth-1 half of the
// grant grammar rules/layers.go's staticEffectsWalk materializes for
// Continuous effects, read here for the mode-scoped scans that never see a
// registered effect (panharmoniconEchoes, the combat board's Statics).
//
// The GRANTED static's source is the object the OUTER Affected$ spec matches
// (its HOST): Masamune's `Affected$ Creature.EquippedBy | AddStaticAbility$
// Masarmonicon` makes the equipped creature the source of the Panharmonicon
// static, so a ValidCard$ Card.Self clause resolves against that creature --
// the same host-relative read the walk's emission gives the inner Affected$
// gate. A self-hosted grant (Affected$ Card.Self, the siege modes) degenerates
// to the granting permanent itself.
//
// Only battlefield objects are scanned, in scanActiveStatics' deterministic
// APNAP/pile order; every gate the walk applies to the OUTER static before it
// enqueues the inner one is applied here too: the room-door and
// abilities-gone gates, the outer EffectZone$ admission and the shared
// "as long as" gate (Condition$, IsPresent$, ClassBand$, CheckSVar$). A gate
// this build cannot evaluate fails closed, exactly as it does in the walk.
func (e *Engine) grantedStatics(mode string) []staticView {
	var out []staticView
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.PhasedOut {
				continue
			}
			f := o.Face()
			if f == nil || e.faceDownPrintedHides(o) {
				continue
			}
			gone, goneRead := false, false
			for si, sn := 0, o.PileStaticCount(); si < sn && !gone; si++ {
				pst, ok := o.PileStaticAt(si)
				if !ok {
					continue
				}
				st := pst.Static
				if isRoom(o) && pst.Face == o.Face() && !o.DoorUnlocked(int(o.FaceIdx)) {
					continue
				}
				name := strings.TrimSpace(st.ParamStr(cards.PKAddStaticAbility))
				if name == "" {
					continue
				}
				if !goneRead {
					if gone, goneRead = e.printedAbilitiesGone(o), true; gone {
						continue
					}
				}
				if !e.stackSelfStaticOK(st, o) && !chars.StaticZoneAdmitsStatic(st, o.Zone) {
					continue
				}
				if st.MayHaveAnyParam(continuousGateKeys) &&
					!e.continuousGateHolds(staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars}) {
					continue
				}
				inners, ok := cards.ParseStaticLines(pst.Face.SVars[name])
				if !ok {
					continue
				}
				affects := st.ParamStr(cards.PKAffected)
				if affects == "" {
					affects = "Card.Self"
				}
				for _, inner := range inners {
					if inner.Mode != mode {
						continue
					}
					out = e.grantedStaticHosts(out, affects, id, o.Controller, pst.Face.SVars, inner)
				}
			}
		}
	}
	return out
}

// grantedStaticHosts appends one staticView for every battlefield object the
// outer Affected$ spec matches, with the host as the granted static's Source
// and its controller as the view's controller.
func (e *Engine) grantedStaticHosts(out []staticView, affects string, source state.ObjID, controller state.PlayerID, svars map[string]string, inner cards.Static) []staticView {
	for _, hp := range e.G.AliveFrom(0) {
		for _, hid := range e.G.Zone(state.ZBattlefield, hp) {
			ho := e.G.Obj(hid)
			if ho == nil || ho.PhasedOut {
				continue
			}
			if !e.matchesSpecFrom(affects, hid, controller, source) {
				continue
			}
			out = append(out, staticView{Source: hid, Controller: ho.Controller, Params: inner.Params, PS: inner.ParamSetOf(), SVars: svars})
		}
	}
	return out
}
