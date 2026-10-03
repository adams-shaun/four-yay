package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// This file holds rules' own effects.Ctx / effects.SpecContext constructors:
// the ones that bind ENGINE-owned tables (the layer-3 rename table, the
// layer-4 derived type table, the compiled predicate programs) which the
// effects tier cannot reach. Like effects/ctx_new.go it is a designated
// constructor file of the W1c ratchet (internal/codeshape ctxLiterals /
// specContextLiterals): every other rules file builds a context through
// effects.NewCtx / NewCtxPtr / NewSpecContext, the (*Ctx) derivations, or
// one of these.
//
// These keep their composite literals because each sits on a hot path that
// depends on being INLINED (budget 80): specCtxSVars on the statics walk and
// every rules-side filter match (its Resolve closure is heap-allocated the
// moment it stops inlining into staticSpecCtx), cdaEvalCtx on the layer-7a
// CDA read and manaAmountCtx on the payment planner (each returns a *Ctx that
// stays on the caller's stack only while inlined). A constructor call plus
// field assignments costs more inline budget than the literal and measurably
// pushed all three over it.

// specCtxSVars is specCtx with an explicit SVar table: a static carried by a
// card merged beneath a mutated pile's top resolves its Chosen*/SVar* terms
// against that under-card's own table. A nil svars falls back to the source
// object's top face, so every pre-existing caller is unchanged.
func (e *Engine) specCtxSVars(source state.ObjID, you state.PlayerID, svars map[string]string) effects.SpecContext {
	var predicates *effects.PredicatePrograms
	if e.compiledText != nil {
		predicates = e.compiledText.predicates
	}
	sc := effects.SpecContext{
		You:               you,
		Source:            source,
		PredicatePrograms: predicates,
		// setname.go: the layer-3 rename set, so a name filter rules
		// evaluates agrees with the layer walk instead of the printed face.
		// setname.go's layer-3 rename table. A FIELD READ, never a call: a
		// call here breaks this constructor's inlining and heap-allocates the
		// Resolve closure on every hot-path construction.
		EffectiveNames: e.renames,
		// layer4types.go's layer-4 derived type table. The same field-read
		// discipline as EffectiveNames above: it makes the ordinary filter
		// grammar (target offer, cost site, Count$Valid, CantTarget) see a
		// type a continuous effect granted.
		DerivedTypes: e.layer4Types,
		Resolve: func(name string) (int32, bool) {
			o := e.G.Obj(source)
			if o == nil {
				return 0, false
			}
			if name == "Chosen" {
				return o.ChosenNumber, true
			}
			table := svars
			if table == nil {
				f := o.Face()
				if f == nil {
					return 0, false
				}
				table = f.SVars
			}
			if body, ok := table[name]; ok {
				return effects.EvalCount(e, effects.NewCtxPtr(source, you, effects.CtxInit{SVars: table}), body), true
			}
			return 0, false
		},
	}
	// A recurring Effect's matcher reads the registration's captured objects,
	// not the creating card's (possibly unrelated) event-backed memory. The
	// override exists only on the read-only observer for that registration.
	if e.effectMatchOverride && source == e.effectMatchSource {
		sc.Remembered = e.effectMatchRemembered
	}
	return sc
}

// cdaEvalCtx builds the Ctx a CDA's value expression resolves through. It
// carries the published layer-4 derived-type table (Engine.EffectiveTypes,
// the same one effects.Resolve binds via the typeTableHost) so a type-based
// Count$Valid value sees types other continuous effects GRANTED (CR 604.3/
// 613.1f: a CDA is evaluated against the characteristics the object actually
// has, and the layer-4 type grant is already in effect at layer 7a). Without
// it the count falls through to the printed face. Every CDA Ctx construction
// site goes through this helper so the reads cannot diverge.
func (e *Engine) cdaEvalCtx(o *state.Object, f *cards.Face) *effects.Ctx {
	return &effects.Ctx{Source: o.ID, Controller: o.Controller, SVars: f.SVars, EffectiveTypes: e.EffectiveTypes()}
}

// manaAmountCtx is the Ctx a mana ability's Amount$ is priced in outside its
// own resolution (the payment planner's castWindowAmount, a Combo
// allocation's manaEffectAmount). It binds the same layer-3 name and layer-4
// type tables effects.Resolve binds at the top of the ability's actual walk,
// so a count over a type or name (Cloudpost's Count$Valid Locus reading
// Planar Nexus's "every nonbasic land type") prices exactly what effMana will
// add.
func (e *Engine) manaAmountCtx(p state.PlayerID, source state.ObjID) *effects.Ctx {
	return &effects.Ctx{Source: source, Controller: p,
		EffectiveNames: e.EffectiveNames(), EffectiveTypes: e.EffectiveTypes()}
}
