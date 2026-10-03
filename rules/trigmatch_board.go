package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// trigBoard is the engine as trigmatch.Board, the read-only view the trigger
// matchers in rules/trigmatch receive (W5 E3). It is a defined type over
// Engine rather than a set of Engine methods so the adapter adds nothing to
// Engine's method set and cannot collide with an Engine method of the same
// name; boardOf(e) is a free pointer conversion (a function, not an Engine
// method, for the same reason), and converting the *trigBoard to the
// interface does not allocate.
//
// Every method is a query that forwards to the engine read it names. None
// emits or writes game state.
type trigBoard Engine

var _ trigmatch.Board = (*trigBoard)(nil)

// boardOf is e as the trigger matchers' read-only view.
func boardOf(e *Engine) *trigBoard { return (*trigBoard)(e) }

func (b *trigBoard) eng() *Engine { return (*Engine)(b) }

func (b *trigBoard) Game() *state.Game { return b.G }
func (b *trigBoard) Log() *events.Log  { return b.L }

func (b *trigBoard) EvalCountOK(ctx *effects.Ctx, expr string) (int32, bool) {
	return effects.EvalCountOK(b.eng(), ctx, expr)
}

func (b *trigBoard) Facts() trigmatch.Facts {
	return trigmatch.Facts{
		Damaging:               b.damaging,
		CombatDamaging:         b.combatDamaging,
		DmgSrcOverride:         b.dmgSrcOverride,
		DeclaredAttackers:      b.declaredAttackers,
		TapObj:                 b.tapObj,
		TapPlayer:              b.tapPlayer,
		TapEntering:            b.tapEntering,
		TappingForMana:         b.tappingForMana,
		TappingManaProduced:    b.tappingManaProduced,
		GoadProbe:              b.goadProbe,
		LifeLossBatch:          b.lifeLossBatch,
		FinishingLifeLossBatch: b.finishingLifeLossBatch,
		TappedTurn:             b.tappedTurn,
	}
}

func (b *trigBoard) ControllerOf(id state.ObjID) state.PlayerID { return b.eng().controllerOf(id) }

func (b *trigBoard) EffectMatchControllerFor(id state.ObjID) (state.PlayerID, bool) {
	return b.eng().effectMatchControllerFor(id)
}

func (b *trigBoard) Chars(id state.ObjID) *effects.Chars { return b.eng().Chars(id) }
func (b *trigBoard) Power(id state.ObjID) int32          { return b.eng().Power(id) }
func (b *trigBoard) Toughness(id state.ObjID) int32      { return b.eng().Toughness(id) }

func (b *trigBoard) FaceDownPrintedHides(o *state.Object) bool {
	return b.eng().faceDownPrintedHides(o)
}

func (b *trigBoard) PlayerSpecCtx(source state.ObjID) effects.PlayerSpecCtx {
	return b.eng().playerSpecCtx(source)
}

// MatchesSpec and MatchesObject build the filter context here, where it stays
// on the stack (specCtx inlines and matchesSpec / MatchesObjectCtx do not
// leak it), and apply the matcher's overrides to it.
func (b *trigBoard) MatchesSpec(spec string, id, source state.ObjID, you state.PlayerID, o trigmatch.SpecOpts) bool {
	e := b.eng()
	sc := e.specCtx(source, you)
	sc.DelayedRemembered, sc.ExtraTypes, sc.Layers.StaticGoads = o.DelayedRemembered, o.ExtraTypes, o.StaticGoads
	return e.matchesSpec(spec, id, sc)
}

func (b *trigBoard) MatchesObject(spec string, obj *state.Object, source state.ObjID, you state.PlayerID, o trigmatch.SpecOpts) bool {
	e := b.eng()
	sc := e.specCtx(source, you)
	sc.DelayedRemembered, sc.ExtraTypes, sc.Layers.StaticGoads = o.DelayedRemembered, o.ExtraTypes, o.StaticGoads
	return effects.MatchesObjectCtx(e.G, spec, obj, sc)
}

func (b *trigBoard) BoardLayers() effects.LayerTables { return b.eng().boardLayers() }

func (b *trigBoard) StaticallyGoadedWithLKI(lki *state.Object) map[state.ObjID]bool {
	return b.eng().staticallyGoadedWithLKI(lki)
}

func (b *trigBoard) CastProvenanceAdmits(spec string, objID state.ObjID, you state.PlayerID) (string, bool) {
	return b.eng().castProvenanceAdmits(spec, objID, you)
}

func (b *trigBoard) ActionCause() state.ObjID { return b.eng().actionCause() }

func (b *trigBoard) InFlightCounterAdder() (state.PlayerID, bool) {
	return b.eng().inFlightCounterAdder()
}

func (b *trigBoard) AbilityCastStackObject(perm state.ObjID) state.ObjID {
	return b.eng().abilityCastStackObject(perm)
}

// LiveActivation reads the open cast flow's pendingCast when it is an
// activation of card's ability: its answered targets and the counters its
// paid SubCounter parts removed (an announced part counts the announced X).
func (b *trigBoard) LiveActivation(card state.ObjID) (trigmatch.Activation, bool) {
	pc := b.cast
	if pc == nil || !pc.isAbility() || pc.card != card {
		return trigmatch.Activation{}, false
	}
	var n int32
	for _, part := range pc.cost.SubCounter {
		if part.Announced {
			n += pc.x
		} else {
			n += part.N
		}
	}
	return trigmatch.Activation{Targets: pc.targets, CountersRemoved: n}, true
}

func (b *trigBoard) TokenSnapshot(ev events.Event) *state.Object { return b.eng().tokenSnapshot(ev) }
func (b *trigBoard) ArrivingPlane(ev events.Event) state.ObjID   { return b.eng().arrivingPlane(ev) }

func (b *trigBoard) ProtectionSource(source state.ObjID) state.ObjID {
	return b.eng().protectionSource(source)
}

func (b *trigBoard) EchoGateHolds(source state.ObjID) bool { return b.eng().echoGateHolds(source) }

func (b *trigBoard) SpellsCastThisTurn(p state.PlayerID) int { return b.eng().spellsCastThisTurn(p) }
func (b *trigBoard) ManaExpendTotal(p state.PlayerID) int32  { return b.eng().manaExpendTotal(p) }

func (b *trigBoard) IsLoyaltyAbility(ab *cards.SA) bool { return isLoyaltyAbility(ab) }
