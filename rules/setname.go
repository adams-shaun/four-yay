package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// This file is the ONE bridge between rules' layer walk and the effects tier's
// name comparisons (Forge's named<X>, notnamed<X>, sameName and NamedCard).
//
// A layer-3 SetName$ effect (CR 613.1d) replaces a permanent's name, and only
// the layer walk can say which effect applies and which of two competing
// effects wins on timestamp. The effects tier cannot import rules and must not
// re-derive any of that from a battlefield scan. So rules computes the renames
// itself and hands them down as DATA on the SpecContext the filter call already
// carries -- the same shape SpecContext.ExtraTypes and ExtraKeywords use, and
// deliberately not a callable resolver field (which poisons escape analysis) or
// a back-pointer from state.Game into a live engine (which would make an
// authoritative Game depend on something outside its event fold, and would
// survive a Game.Clone aimed at a different board).
//
// The table is a FIELD, refreshed after each emitted event, and specCtx binds
// it with a plain field read. That is load-bearing, not a style choice: calling
// a function from inside specCtxSVars pushes it over the inline budget, its
// Resolve closure then escapes, and every SpecContext construction on the
// Derived/legal-actions hot path allocates
// (TestDerivedWithContinuousEffectsDoesNotAllocate,
// TestLegalActionsReusesActionStaticMembership). The refresh itself is gated on
// setNameInPool so a match whose cards carry no SetName$ static -- almost every
// match -- pays one predictable branch per event and nothing else.
//
// A filter call with no rules-built context -- effects.MatchesSpecFrom from
// code that owns no resolution Ctx (an Aura entry bearer scan), a bare
// *state.Game, a unit test -- carries no names and reads the printed face.
// That is the same reach ExtraTypes has (see the "Layer-4 type grants reach
// only the layer walk" row in AGENTS.md), and it is pinned by
// TestBareGameNameFilterReadsThePrintedName. Every RESOLVING effect's filter
// call DOES see the names: rules publishes the table to effects at the top of
// every effects.Resolve walk (Engine.EffectiveNames), which binds it on the
// resolving Ctx and propagates it through (*Ctx).SpecContext and Ctx.MatchSpec
// -- see setName_filter_scope_test.go's resolving-effect probe.

// poolHasSetNameStatic reports whether any card this match can put on the
// battlefield prints a SetName$ static. Computed once, at genesis, over the
// decks and the token table: a card outside the pool can never register one of
// these statics from its printed text. The per-card probe is cards.SetsName --
// card-data introspection, deliberately not a statics-parameter read on a
// rules path (rules/paramcensus_test.go attributes those to the primitive
// whose activeStatics root reaches them, and this probe has no such root
// because it runs before any board exists).
func poolHasSetNameStatic(cfg Config) bool {
	for _, deck := range cfg.Decks {
		for _, c := range deck {
			if c.SetsName() {
				return true
			}
		}
	}
	for _, c := range cfg.Tokens {
		if c.SetsName() {
			return true
		}
	}
	return false
}

// refreshRenames recomputes the rename table if the board or the continuous
// effects moved since the last build. The key is active()'s own -- the log head
// plus the continuous-effect version -- because the rename set is a pure
// function of exactly those two inputs.
func (e *Engine) refreshRenames() {
	if e.renameBuilding {
		// Re-entry: a Derived() call below reached something that re-emitted.
		// The half-built table must never be published; the outer call
		// finishes it.
		return
	}
	if e.renameEpoch == len(e.L.Events) && e.renameVersion == e.continuousVersion {
		return
	}
	// Layer-inert reuse (layercache.go), refreshDerivedTypes' twin: only
	// priority bookkeeping moved the log since the table was built, and
	// neither the continuous registry nor the object arena moved, so every
	// Derived name the table holds is what a rebuild would read again.
	if e.renameVersion == e.continuousVersion && e.renameObjs == len(e.G.Objs) && e.layerInertSince(e.renameEpoch) {
		e.renameEpoch = len(e.L.Events)
		if layerInertVerify {
			e.verifyInertRenames()
		}
		return
	}
	// Derived-transparent reuse (derived_transparent.go): a table built at
	// the derivedSeq active() still holds read names that no battlefield
	// derivation has changed since -- a transparent rebuild spans no event
	// that moves an object onto or off the battlefield, flips a face or
	// appends an object, so the battlefield membership and every printed name
	// the table compares against are unchanged too (an off-battlefield move's
	// own object is never on the battlefield). A transparent rebuild that DID
	// move an object across the battlefield boundary (or out of exile) moves
	// derivedBFSeq instead, which the table keys on as well.
	if e.renameVersion == e.continuousVersion && e.renameObjs == len(e.G.Objs) && e.renameDSeq != 0 {
		e.active()
		if e.renameDSeq == e.derivedSeq && e.renameBFSeq == e.derivedBFSeq {
			e.renameEpoch = len(e.L.Events)
			if layerInertVerify {
				e.verifyInertRenames()
			}
			return
		}
	}
	e.renameEpoch, e.renameVersion, e.renameObjs = len(e.L.Events), e.continuousVersion, len(e.G.Objs)
	buf := e.renames[:0]
	if !e.anySetNameActive() {
		e.renames = buf[:0]
		e.renameDSeq, e.renameBFSeq = e.derivedSeq, e.derivedBFSeq
		return
	}
	e.renameDSeq, e.renameBFSeq = e.derivedSeq, e.derivedBFSeq
	e.renameBuilding = true
	defer func() { e.renameBuilding = false }()
	// A name differs from the printed one only through a live SetName
	// effect (derivedName), so when every such effect names a bounded object
	// set (Witness Protection's Creature.EnchantedBy), only those objects can
	// carry an entry. They are visited in id order -- e.G.Objs order, the
	// dense arena -- so the table is the whole walk's.
	var arr [layer4MaxSelfSources]state.ObjID
	if reach, ok := e.setNameBoundedReach(arr[:0]); ok {
		slices.Sort(reach)
		for _, id := range reach {
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
				continue
			}
			if name := e.derivedName(id); name != "" && name != o.Face().Name {
				buf = append(buf, effects.ObjectName{ID: id, Name: name})
			}
		}
		e.renames = buf
		if layerInertVerify {
			e.verifyBoundedRenames()
		}
		return
	}
	// e.G.Objs is append-ordered, so this walk is deterministic; only the
	// battlefield is scanned because a layer effect applies nowhere else.
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield {
			continue
		}
		f := o.Face()
		if f == nil {
			continue
		}
		name := e.derivedName(o.ID)
		if name == "" || name == f.Name {
			continue
		}
		buf = append(buf, effects.ObjectName{ID: o.ID, Name: name})
	}
	e.renames = buf
}

// setNameBoundedReach is layer4BoundedReach for the live SetName effects:
// the distinct objects they can reach (appended to buf, unsorted) and true,
// or false when some SetName effect's Affected$ spec is unbounded
// (affectsReach) or the list would overflow.
func (e *Engine) setNameBoundedReach(buf []state.ObjID) ([]state.ObjID, bool) {
	act := e.active()
	for i := range act {
		ce := &act[i]
		if ce.Layer != LText || ce.SetName == "" {
			continue
		}
		bits := affectsReach(ce.Affects)
		if bits == 0 {
			return buf, false
		}
		fit := true
		if bits&reachSelf != 0 && ce.Source != 0 {
			buf, fit = appendReach(buf, ce.Source)
		}
		if fit && bits&reachAttached != 0 {
			if s := e.G.Obj(ce.Source); s != nil && s.Zone == state.ZBattlefield && s.AttachedTo != 0 {
				buf, fit = appendReach(buf, s.AttachedTo)
			}
		}
		if !fit {
			return buf, false
		}
	}
	return buf, true
}

// verifyBoundedRenames rebuilds the rename table with the whole-battlefield
// walk and panics if the bounded build disagrees.
func (e *Engine) verifyBoundedRenames() {
	var want []effects.ObjectName
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield || o.Face() == nil {
			continue
		}
		if name := e.derivedName(o.ID); name != "" && name != o.Face().Name {
			want = append(want, effects.ObjectName{ID: o.ID, Name: name})
		}
	}
	if !slices.Equal(want, e.renames) {
		panic(fmt.Sprintf("rules: bounded rename table at log %d is %v, the whole walk %v", len(e.L.Events), e.renames, want))
	}
}

// verifyInertRenames is refreshRenames' layer-inert reuse check: it rebuilds
// the table into fresh storage and compares.
func (e *Engine) verifyInertRenames() {
	cached := append([]effects.ObjectName(nil), e.renames...)
	e.renames = nil
	e.renameEpoch, e.renameDSeq = -1, 0
	e.refreshRenames()
	fresh := e.renames
	if !slices.Equal(cached, fresh) {
		panic(fmt.Sprintf("rules: layer-inert rename table reuse at log %d disagrees with a rebuild (%v vs %v)", len(e.L.Events), cached, fresh))
	}
}

// continuousChanged is the single mutator tail for e.continuous: bump the
// version active() keys on, then keep the rename table in step, since a
// continuous-effect change alone moves no log head and would otherwise leave a
// stale table readable until the next emitted event.
func (e *Engine) continuousChanged() {
	e.continuousVersion++
	if e.setNameInPool {
		e.refreshRenames()
	}
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
}

// anySetNameActive reports whether any active continuous effect sets a name.
// Without this gate every refresh would pay for a full battlefield Derived()
// walk on a board whose SetName$ carrier is still in a library.
func (e *Engine) anySetNameActive() bool {
	act := e.active()
	for i := range act {
		if ce := &act[i]; ce.Layer == LText && ce.SetName != "" {
			return true
		}
	}
	return false
}

// withNames binds the current layer-3 renames AND the layer-4 derived types on
// a SpecContext built outside specCtxSVars, so every hand-built rules context
// agrees with the layer walk. It is the ONE seam for those literals -- cast
// legality, cost sites, may-play, replacement, the target offer -- so a new
// such context cannot silently miss a layer table the way one more site of
// this ticket's class would.
func (e *Engine) withNames(sc effects.SpecContext) effects.SpecContext {
	sc.Layers.EffectiveNames = e.renames
	sc.Layers.DerivedTypes = e.layer4Types
	return sc
}

// EffectiveNames publishes the current layer-3 rename table to the effects
// tier, which reads it once at the top of every effects.Resolve walk
// (effects' layerTablesHost) and binds it on the resolving Ctx. That is what
// makes a resolving effect's own filter calls -- the (*Ctx).SpecContext calls
// effects/zone.go, counter and damage primitives already make, plus the
// Ctx.MatchSpec sites -- agree with the layer walk instead of reading the
// printed face. It is a plain value-slice read, never a live engine pointer:
// effects answer name filters during a resolution without a back-pointer on
// state.Game, and a cloned game cannot read another game's board. The table is
// refreshed by emit and continuousChanged, so it is current at Resolve entry.
func (e *Engine) EffectiveNames() []effects.ObjectName { return e.renames }
