package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// This file owns the face-aware plumbing for a MUTATED pile (CR 702.140d).
// state.Object's PileAbilityCount/At, PileFaceCount/At and PileStaticCount/At
// give the one flat view of a permanent's rules text (index-based, because
// every one of those walks runs per object per legal-actions pass and the
// repo's allocation budgets forbid a per-object slice); these helpers thread the owning face's SVar table
// through the rules-side reads that used to assume the top face, and resolve a
// pendingCast's flat ability index back to its SA and owning face.
//
// The merged ordinal convention is state's: 0 is the top face, i+1 is the i-th
// under-card.

// pileSVars returns the SVar table a pile position resolves against, with any
// AddSVar$ grant layered under the printed table (the same precedence
// sVarGateOK documents: a printed SVar of the same name wins). merged 0 is the
// top face, so every non-mutated read is unchanged -- grantedSVarsFor is only
// consulted for the top face, exactly as before.
func (e *Engine) pileSVars(id state.ObjID, merged int) map[string]string {
	o := e.G.Obj(id)
	if o == nil {
		return nil
	}
	if merged <= 0 {
		f := o.Face()
		if f == nil {
			return nil
		}
		svars := f.SVars
		if gr := e.grantedSVarsFor(id); gr != nil {
			merged := make(map[string]string, len(gr)+len(f.SVars))
			for k, v := range gr {
				merged[k] = v
			}
			for k, v := range f.SVars {
				merged[k] = v
			}
			svars = merged
		}
		return svars
	}
	return o.PileSVars(merged)
}

// pileFaceForSA recovers the face that carries an activated ability of the
// permanent source, by SA pointer identity, walking the pile top-first. It is
// the activated-ability counterpart of findTriggerForAbilityFace: a resolving
// stack object carries o.Ability (the exact SA pointer events.Apply minted
// from the pile), and the face that owns it is the face whose SVar table the
// ability's body must resolve against. ok is false when the SA is not a
// printed ability of the source (a granted ability, a trigger body, or a
// source that left the battlefield) -- callers then keep today's top-face
// fallback.
func (e *Engine) pileFaceForSA(source state.ObjID, sa *cards.SA) (*cards.Face, bool) {
	if sa == nil {
		return nil, false
	}
	o := e.G.Obj(source)
	if o == nil {
		return nil, false
	}
	for i := 0; i < o.PileFaceCount(); i++ {
		pf, ok := o.PileFaceAt(i)
		if !ok {
			continue
		}
		for _, ab := range pf.Face.Abilities {
			if ab == sa {
				return pf.Face, true
			}
		}
	}
	// A has-all-abilities-of GRANTED activated ability (Forge's
	// GainsAbilitiesOf$): the resolving body is a compiled AB$ on a FOREIGN
	// card's face, so its owning SVar table is that face's, not the
	// recipient's. Measured against the minted provenance first -- the
	// resolving wrapper sits on the stack carrying the exact foreign face
	// events.Apply stamped (gainedOwnedFace), which survives the granting
	// static ending between the activation and the resolution, the case the
	// live-grant scan below cannot answer -- and only then against the live
	// grants (a grant that ended with its static is no owner), in active()'s
	// deterministic order. Any stack wrapper with this (source, ability) pair
	// carries the same provenance -- the foreign face is a property of the
	// compiled SA pointer, not of the instance -- so the scan is
	// deterministic and instance-independent.
	for _, wid := range e.G.Stack {
		wo := e.G.Obj(wid)
		if wo == nil || wo.Ability != sa || wo.Source != source {
			continue
		}
		if f := gainedOwnedFace(wo); f != nil {
			for _, ab := range f.Abilities {
				if ab == sa {
					return f, true
				}
			}
		}
	}
	for _, gf := range e.gainedFacesForSource(source) {
		if gf.Face == nil {
			continue
		}
		for _, ab := range gf.Face.Abilities {
			if ab == sa {
				return gf.Face, true
			}
		}
	}
	return nil, false
}

// gainedOwnedFace returns the foreign face a HAS-ALL-ABILITIES-OF ability
// wrapper was minted from: the per-stack-instance provenance events.Apply
// folds onto the wrapper at GainedAbilityPush/GainedTriggerPush time (r3).
// The granting static can END between the push and the resolution -- the
// foreign card leaves the scoped zone, the static's named set re-derives --
// and findTriggerForAbilityFace / this file's live-grant scan then find no
// owner and fall back to the recipient's face, whose SVar table the foreign
// body never meant. The wrapper's own field is the answer: immutable, set by
// events.Apply from the exact face that provided the compiled SA, and
// reproduced by every replay because Apply re-runs. A nil return means "not a
// gained wrapper" (every ordinary wrapper leaves the field nil) or a face
// that no longer carries the pointer, and the caller keeps its existing
// chain. The identity scan is a guard, not a search: Apply only ever stamps
// the face it took the pointer from, so the hit is the first element at
// worst.
func gainedOwnedFace(o *state.Object) *cards.Face {
	if o == nil || o.GainedFace == nil || o.Ability == nil {
		return nil
	}
	f := o.GainedFace
	for _, ab := range f.Abilities {
		if ab == o.Ability {
			return f
		}
	}
	for i := range f.Triggers {
		if f.Triggers[i].Effect == o.Ability {
			return f
		}
	}
	return nil
}

// pileAbilityRefOf is chars.PileAbilityRef.
func pileAbilityRefOf(o *state.Object, sa *cards.SA) (idx, merged int, ok bool) {
	return chars.PileAbilityRef(o, sa)
}

// grantedSAFrom resolves an SVar-anchored GRANTED ability body (the
// `AddAbility$ <name>` grant a Continuous static delivers) from the grantor's
// rules text, falling back to the recipient when the grantor has left.
//
// The grantor may itself be a mutated pile whose GRANTING STATIC sits on an
// under-card (CR 702.140d): the named body then lives on that under-card's own
// SVar table, which is exactly where the offer side reads it -- legal.go's
// grantedAbilities resolves the name against the emitting
// ContinuousEffect.SVars, and rules/layers.go stamps a merged face's table
// onto the effects it emits. If the ACTIVATION side read only the grantor's
// active face, an option that was legally offered would resolve to nil and
// the activation would silently no-op. So the faces are walked in the pile's
// own deterministic order, top face first: a non-mutated grantor resolves
// byte-identically to the old Face().SVars read, and the first face that
// names the SVar with an AB body wins.
//
// Nothing here is an index decode: a granted activation carries ability == -1
// and is anchored by NAME, so the flat pile-ability index never applies to it.
func (e *Engine) grantedSAFrom(grantor, recipient state.ObjID, svar string) *cards.SA {
	o := e.G.Obj(grantor)
	if o == nil || o.Face() == nil {
		o = e.G.Obj(recipient)
	}
	if o == nil || o.Face() == nil {
		return nil
	}
	for i := 0; i < o.PileFaceCount(); i++ {
		pf, ok := o.PileFaceAt(i)
		if !ok {
			continue
		}
		if ab := cards.ResolveSVar(pf.Face.SVars, svar); ab != nil && ab.Kind == "AB" {
			return ab
		}
	}
	return nil
}
