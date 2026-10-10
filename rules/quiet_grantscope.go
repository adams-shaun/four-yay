package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Per-seat scope for the proof's grant board flags (design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md §2.2 flags
// 1, 3 and 5, step Q3c).
//
// A grant (AddAbility$, GainsAbilitiesOf$, AddKeyword$, MayPlay$) only
// changes what p is offered when it reaches an object p can act on. The
// board flags used to fire for any grant anywhere; here a grant stops
// blocking p when its Affected$ spec provably cannot match an object p
// controls, AND no granted ability can be activated by a player other than
// the recipient's controller (Activator$). Every read is a cheap
// exactly-or-coarser test: a spec the compiled grammar cannot place, a
// gained face, or an Activator$-bearing body keeps the grant a blocker.

// quietSpecClearsSeat reports that spec, evaluated with You = you and Source
// = src, cannot match any object whose controller is p. Off-battlefield
// objects take their owner's control (events/apply_zone.go), so a card p owns
// in hand, graveyard or exile is p-controlled and is covered by the same test.
func (e *Engine) quietSpecClearsSeat(spec string, you state.PlayerID, src state.ObjID, p state.PlayerID) bool {
	sc := effects.SpecScopeOf(spec)
	if sc == 0 {
		return false
	}
	if sc&effects.SpecScopeYouCtrl != 0 && you != p {
		return true
	}
	if sc&effects.SpecScopeNotYouCtrl != 0 && you == p {
		return true
	}
	if sc&effects.SpecScopeSelf != 0 {
		// Self matches only the source: a source with no object matches
		// nothing, but stay closed on a missing object (stale data).
		if o := e.G.Obj(src); o != nil && o.Controller != p && e.controllerOf(src) != p {
			return true
		}
	}
	if sc&effects.SpecScopeAttached != 0 {
		// The compiled term reads s.AttachedTo == candidate with s a
		// battlefield permanent (effects.attachedBy); anything else matches
		// nothing at all.
		s := e.G.Obj(src)
		if s == nil {
			return false
		}
		if s.Zone != state.ZBattlefield || s.AttachedTo == 0 {
			return true
		}
		if t := e.G.Obj(s.AttachedTo); t != nil && t.Controller != p && e.controllerOf(s.AttachedTo) != p {
			return true
		}
	}
	return false
}

// quietGrantListPlain reports that none of the granted ability names carries
// an Activator$ in its body (cards.SVarGrantHasActivator).
func quietGrantListPlain(svars map[string]string, names []string) bool {
	for _, nm := range names {
		if cards.SVarGrantHasActivator(svars, nm) {
			return false
		}
	}
	return true
}

// quietGrantNamesPlain is quietGrantListPlain over a static's AddAbility$
// value, split the way statList splits it (" & " and ",").
func quietGrantNamesPlain(svars map[string]string, value string) bool {
	for value != "" {
		i := strings.IndexAny(value, ",&")
		part := value
		if i < 0 {
			value = ""
		} else {
			part, value = value[:i], value[i+1:]
		}
		if cards.SVarGrantHasActivator(svars, strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

// quietGrantEffectClears reports that one active ability-granting effect
// (AddAbilities / GainedFaces) cannot give p an offer.
func (e *Engine) quietGrantEffectClears(ce *ContinuousEffect, p state.PlayerID) bool {
	if len(ce.GainedFaces) != 0 {
		// Foreign faces' Activator$ is not read here; stay a blocker.
		return false
	}
	if !e.quietSpecClearsSeat(ce.Affects, ce.Controller, ce.Source, p) {
		return false
	}
	if len(ce.AddAbilities) == 0 {
		return true
	}
	src := e.G.Obj(ce.Source)
	if src == nil || src.Face() == nil {
		// grantedAbilities skips a grant whose source has no face.
		return true
	}
	svars := ce.SVars
	if svars == nil {
		svars = src.Face().SVars
	}
	return quietGrantListPlain(svars, ce.AddAbilities)
}

// quietGrantsReachSeat is board flag 1 for p: some active ability grant is
// not provably out of p's reach.
func (e *Engine) quietGrantsReachSeat(ces []ContinuousEffect, p state.PlayerID) bool {
	for i := range ces {
		ce := &ces[i]
		if len(ce.AddAbilities) == 0 && len(ce.GainedFaces) == 0 {
			continue
		}
		if !e.quietGrantEffectClears(ce, p) {
			return true
		}
	}
	return false
}

// quietKWGrantsReachSeat is board flag 5 for p: an active AddKeyword effect
// granting a head the proof treats as opening an offer is not provably out of
// p's reach.
func (e *Engine) quietKWGrantsReachSeat(ces []ContinuousEffect, p state.PlayerID) bool {
	for i := range ces {
		ce := &ces[i]
		blocks := false
		for _, k := range ce.AddKeywords {
			h := cards.KeywordHead(k)
			if quietActiveKWGrantBlocker(h) || quietGraveRecastGrantHead(h) {
				blocks = true
				break
			}
		}
		if blocks && !e.quietSpecClearsSeat(ce.Affects, ce.Controller, ce.Source, p) {
			return true
		}
	}
	return false
}

// quietAddAbilityStaticClears is quietGrantEffectClears for a printed
// Continuous AddAbility$ static st of o (face sf carries it): the scan's
// per-static test.
func (e *Engine) quietAddAbilityStaticClears(p state.PlayerID, o *state.Object, sf *cards.Face, st *cards.Static) bool {
	spec, _ := cards.ParamSetParam(st.ParamSetOf(), st.Params, cards.PKAffected)
	spec = strings.TrimSpace(spec)
	if spec == "" || sf == nil {
		return false
	}
	if !e.quietSpecClearsSeat(spec, e.controllerOf(o.ID), o.ID, p) {
		return false
	}
	names, _ := cards.ParamSetParam(st.ParamSetOf(), st.Params, cards.PKAddAbility)
	return quietGrantNamesPlain(sf.SVars, strings.TrimSpace(names))
}
