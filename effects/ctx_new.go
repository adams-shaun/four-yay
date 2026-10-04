package effects

import "github.com/adams-shaun/gorge/state"

// This file is the ONE home of effects.Ctx and effects.SpecContext
// construction (rules-engine refactor spec W1c,
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section
// 5). Every other file builds a context through the constructors below; the
// shrink-only ratchet in internal/codeshape (ctxLiterals, specContextLiterals)
// counts the Ctx{...} / SpecContext{...} composite literals anywhere else and
// fails the build if one comes back.
//
// The reason is RC2: a context that is DERIVED from another (a resolution's
// filter context, a per-object sub-evaluation) used to be a hand-written
// literal copying the fields its author knew about, so a field added to Ctx
// later -- a layer table, an LKI record -- silently did not reach it. Now a
// field that must propagate is added in exactly one place:
//
//   - a resolution-wide table every sub-evaluation must see goes in
//     (*Ctx).Child, (*Ctx).TableSpecContext and (*Ctx).SpecContext;
//   - a field every fresh context is commonly seeded with goes in CtxInit.
//
// Constructors return VALUES and take a value-struct initializer, never
// functional options: they are small enough to inline, so a constructed
// context lives wherever the old literal lived (a stack slot, an arena
// slot, a pendingTrigger field) and the constructors add no allocation on
// the statics walk, trigger matching or the legal-action walk.
//
// None of them binds Ctx.Host. No construction site ever set it: Resolve binds
// it on entry, and a pre-bound Host would change which numeric-RHS fallback
// (*Ctx).resolveNumericRHS takes for a context that never resolves. A site
// that needs it (rules' lazily built count Ctx) assigns it explicitly.

// CtxInit seeds a fresh Ctx with the fields construction sites commonly set
// beyond the source and controller. Any other field is assigned on the
// constructed value at the call site, where the reader can see it.
type CtxInit struct {
	// TriggerContext is the firing event's referents (rules'
	// triggerReferents, or a synthesized trigger's hand-built record).
	TriggerContext TriggerContext
	// SVars is the script's SVar table the context's counts resolve against.
	SVars map[string]string
	// X is the announced or paid {X}.
	X int32
	// Targets are the resolving object's chosen targets.
	Targets []state.Target
	// Remembered and Captured are the event-backed remembered set and the
	// registration's captured referents.
	Remembered []state.Target
	Captured   []state.Target
	// LKI is the source's last-known snapshot and its LKI power/toughness.
	LKI *state.Object
	// Snap carries the snapshot's derived P/T (Snap.Power, Toughness,
	// PTValid).
	Snap LKISnapshots
	// EffectFrame is the Effect registration frame the body resolves under.
	EffectFrame EffectFrame
	// ChosenNumber/ChosenNumberBound are an Effect-delivered static's
	// SetChosenNumber$ binding, which Count$ChosenNumber reads in place of
	// the source's own logged choice.
	Num NumberInputs
}

// NewCtx is a fresh context for source, controlled by controller, seeded
// from in. It inherits nothing: a context derived from a resolving one is
// built with (*Ctx).Child.
func NewCtx(source state.ObjID, controller state.PlayerID, in CtxInit) Ctx {
	return Ctx{
		TriggerContext: in.TriggerContext,
		Source:         source,
		Controller:     controller,
		SVars:          in.SVars,
		X:              in.X,
		Targets:        in.Targets,
		Remembered:     in.Remembered,
		Captured:       in.Captured,
		LKI:            in.LKI,
		Snap:           in.Snap,
		EffectFrame:    in.EffectFrame,
		Num:            in.Num,
	}
}

// NewCtxPtr is NewCtx for a site that needs a *Ctx (an argument to EvalCount,
// Defined, CheckSVarHolds...). It inlines, so the context escapes exactly
// when the old &Ctx{...} literal did.
func NewCtxPtr(source state.ObjID, controller state.PlayerID, in CtxInit) *Ctx {
	c := NewCtx(source, controller, in)
	return &c
}

// Child is a fresh context re-anchored on source/controller for a
// sub-evaluation inside c's resolution (a per-object count, a per-source
// Defined$ walk): it carries c's script table (SVars) and the
// resolution-wide derived tables Resolve published on c -- the layer-3
// rename table, the layer-4 type table, the layer-5/6 colour and keyword
// tables, the static-goad set and the targetable-object set -- so the child's
// filters agree with the layer walk exactly as c's do. It inherits nothing
// else: no targets, no remembered set, no ask cursor, no LKI.
func (c *Ctx) Child(source state.ObjID, controller state.PlayerID) Ctx {
	return Ctx{
		Source:            source,
		Controller:        controller,
		SVars:             c.SVars,
		Layers:            c.Layers,
		TargetableObjects: c.TargetableObjects,
	}
}

// ForTrigger is the context of a triggered ability minted by c's resolution
// (a CR 603.12 reflexive "when you do" trigger), with tc as its trigger
// referents. It keeps c's source, controller and Effect frame, and what c
// knows about the source that the minted ability's counts read: the LKI
// snapshot and LKI power/toughness, the damage-source lifelink and
// controller LKI, and private copies of the cost-paid lists (Sacrificed,
// Exiled, Revealed). It does not inherit c's targets, remembered set or any
// ask cursor; the caller sets Remembered to the instance's own set.
func (c *Ctx) ForTrigger(tc TriggerContext) Ctx {
	return Ctx{
		TriggerContext: tc,
		Source:         c.Source,
		Controller:     c.Controller,
		EffectFrame:    c.EffectFrame,
		LKI:            c.LKI,
		Snap: LKISnapshots{
			Power: c.Snap.Power, Toughness: c.Snap.Toughness, PTValid: c.Snap.PTValid,
			SourceLifelink: c.Snap.SourceLifelink, SourceLifelinkValid: c.Snap.SourceLifelinkValid,
			SourceController: c.Snap.SourceController, SourceControllerValid: c.Snap.SourceControllerValid,
		},
		Sacrificed: append([]state.SacrificedInfo(nil), c.Sacrificed...),
		Exiled:     append([]state.ObjID(nil), c.Exiled...),
		Revealed:   append([]state.ObjID(nil), c.Revealed...),
	}
}

// NewSpecContext is a filter context for perspective seat you and source
// object source, and nothing else: no layer table, no resolver. Rules binds
// its layer tables through Engine.withNames / specCtxSVars; a context derived
// from a resolving Ctx comes from (*Ctx).SpecContext or
// (*Ctx).TableSpecContext.
func NewSpecContext(you state.PlayerID, source state.ObjID) SpecContext {
	return SpecContext{You: you, Source: source}
}

// TableSpecContext is the filter context of c's source with only the
// resolution's published derived tables bound (layer-3 names, layer-4
// types, layer-5/6 colours and keywords, static goads): MatchesSpecFrom's
// You/Source grammar plus the layer walk's view of the board. It carries no
// trigger referents, no remembered/chosen sets and no numeric-RHS resolver;
// (*Ctx).SpecContext is the full resolution context built on top of it.
func (c *Ctx) TableSpecContext(you state.PlayerID) SpecContext {
	return SpecContext{You: you, Source: c.Source, Layers: c.Layers}
}

// SpecContext binds a resolution's filter without adding a numeric resolver
// that the old MatchesSpecFrom call sites did not have. That grammar is
// independent of trigger provenance.
func (c *Ctx) SpecContext(you state.PlayerID) SpecContext {
	// The first line is TableSpecContext's field set, spelled out rather
	// than called: the call costs this method its inlining (budget 80), and
	// the inlining is pinned (see below). TestSpecContextCarriesTableSpecContext
	// holds the two to the same tables. They are the layer tables rules
	// published at Resolve entry, so a resolving effect's filter -- target
	// offer, Count$Valid, CantTarget -- agrees with the layer walk: field
	// copies of immutable data, no callable, no back-pointer.
	sc := SpecContext{You: you, Source: c.Source, Layers: c.Layers,
		TriggerContext: c.TriggerContext, ResolutionTargets: c.Targets, Remembered: c.Remembered, Chosen: c.Chosen, ChosenValid: c.ChosenValid, Resolving: true,
		TargetableObjects:           c.TargetableObjects,
		ExcludeFromBattlefieldCount: c.Repl.ExcludeFromBattlefieldCount}
	// Numeric-RHS resolution for a resolution-time filter spec, in priority
	// order:
	//
	//  1. a DB$ RollDice publication of this same resolution
	//     (effects/dice.go) -- Valiant Endeavor's Creature.powerGEX (destroy
	//     each creature with power greater than or equal to the CHOSEN roll)
	//     and Arcane Endeavor's Instant.cmcLEY (cast for free up to the OTHER
	//     roll) read the published roll through here.
	//  2. the bare name "X": the two-shape SVar:X reading fixLifeXCost
	//     established (rules/mana.go) -- body Count$xPaid (Whir of
	//     Invention) is the paid X itself; any OTHER resolvable body
	//     (Nightmare Unmaking's SVar:X:Count$ValidHand Card.YouOwn) is a
	//     fixed value evaluated through EvalCountOK with the resolving Host;
	//     no SVar:X at all is the paid X (0 when unpaid), the same reading
	//     the roll closure always gave. An unresolvable body fails closed:
	//     the recognised-shape-never-matches contract, never a guessed zero.
	//  3. any other name: the SVar table -> EvalCountOK (resolveNumericRHS),
	//     same fail-closed verdict.
	//
	// Wired only when something can resolve -- a roll published, or the
	// context came through effects.Resolve with a paid X or an SVar table
	// (the numericRHS flag Resolve computes on entry; the resolver itself
	// decides per name and fails closed on a name with no resolvable body,
	// so the broad flag never widens a match) -- so every other card keeps
	// building the plain resolver-free SpecContext it always built.
	// Hand-built contexts (the direct Num/EvalCount probes) never carry the
	// flag. The gate must also stay inline-budget small AND the resolver
	// must be installed through a func literal that calls the method (never
	// the method value c.resolveNumericRHS itself): the escape-analysis pin
	// this caller answers to (rules/layers_test.go's warm Derived pin, zero
	// heap allocations per Derived call) needs (*Ctx).SpecContext to remain
	// inlinable, and the method value both blows the cost budget and leaks
	// the receiver. See also the hot statics/layer walk, which builds its
	// SpecContexts directly and never routes through here.
	if c.Roll.LastName != "" || len(c.Roll.Pubs) > 0 || c.numericRHS {
		sc.Resolve = func(name string) (int32, bool) {
			return c.resolveNumericRHS(name)
		}
	}
	return sc
}
