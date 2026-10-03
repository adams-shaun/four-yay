package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Board is the read-only view of the engine a trigger matcher reads. rules
// implements it (rules/trigmatch_board.go); a matcher receives it in place of
// the *rules.Engine it used to be a method on.
//
// Every method is a query: none emits an event or changes game state. Game
// and Log hand out the live match state and event log for reading only --
// the same contract as effects.HostRead.Game -- and a matcher must never
// write through them. Host is the one escape hatch, there only because the
// effects count and player-filter evaluators (effects.EvalCountOK,
// effects.MatchesPlayerSpecWithSVars) take a whole effects.Host; a matcher
// passes it to those evaluators and calls nothing on it itself.
//
// The method set is ratcheted shrink-only (internal/codeshape,
// trigmatchBoardMethods). Derive a new fact from an existing method -- Chars
// carries every characteristic, Facts every per-emit trigger context value --
// before adding one.
type Board interface {
	// Game is the live match state, for reading.
	Game() *state.Game
	// Log is the live event log, for reading (the look-back scans walk
	// Log().Events backwards to the turn boundary).
	Log() *events.Log
	// Facts is the synchronous per-emit trigger context: the provenance rules
	// keeps beside the event being matched rather than on it (damage source,
	// tapper, the declare-attackers batch, the life-loss batch).
	Facts() Facts
	// Host is the engine as an effects.Host, for the effects evaluators that
	// take one. Read-only use only.
	Host() effects.Host

	// ControllerOf is the object's current controller (rules' controllerOf).
	ControllerOf(id state.ObjID) state.PlayerID
	// EffectMatchControllerFor is the recurring-Effect matching overlay's
	// virtual controller for id, when the overlay is armed for it.
	EffectMatchControllerFor(id state.ObjID) (state.PlayerID, bool)
	// Chars is the object's current layer-derived characteristics, with
	// effects.HostRead.Chars's lifetime rule: valid until the next Chars call
	// or emit, and never two Chars reads in one expression.
	Chars(id state.ObjID) *effects.Chars
	// Power and Toughness are the derived P/T fast paths.
	Power(id state.ObjID) int32
	Toughness(id state.ObjID) int32
	// FaceDownPrintedHides reports whether o's printed characteristics are
	// hidden by a face-down state (CR 708.2).
	FaceDownPrintedHides(o *state.Object) bool

	// SpecCtx is the filter context for source controlled by you, with
	// rules' layer tables bound.
	SpecCtx(source state.ObjID, you state.PlayerID) effects.SpecContext
	// PlayerSpecCtx is the player-filter context for source.
	PlayerSpecCtx(source state.ObjID) effects.PlayerSpecCtx
	// MatchesSpec evaluates a ValidCard$-style filter against id.
	MatchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool
	// MatchesSpecFrom evaluates spec against id from you's perspective with
	// source as the filter's self.
	MatchesSpecFrom(spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool
	// BoardLayers is the current layer tables (names, types, static goads).
	BoardLayers() effects.LayerTables
	// StaticallyGoadedWithLKI is the statically goaded set, with lki standing
	// in for its object where given.
	StaticallyGoadedWithLKI(lki *state.Object) map[state.ObjID]bool
	// CastProvenanceAdmits evaluates a cast-provenance filter term
	// (wasCastFrom..., the cast-history family) for objID; the string is the
	// residual spec.
	CastProvenanceAdmits(spec string, objID state.ObjID, you state.PlayerID) (string, bool)

	// ActionCause is the stack object whose resolution caused the synchronous
	// action event being matched (0 when none, e.g. a cost).
	ActionCause() state.ObjID
	// InFlightCounterAdder is the player causing the counter placement in
	// flight, and whether that attribution is known at all.
	InFlightCounterAdder() (state.PlayerID, bool)
	// AbilityCastStackObject is the topmost non-trigger ability wrapper on the
	// stack whose source is perm (0 when none).
	AbilityCastStackObject(perm state.ObjID) state.ObjID
	// LiveActivation is the in-flight activation of card's ability, while the
	// cast flow for it is still open.
	LiveActivation(card state.ObjID) (Activation, bool)
	// TokenSnapshot is the would-be token a TokenCreate event would mint, as a
	// read-side snapshot never added to the game (nil for an unknown token).
	TokenSnapshot(ev events.Event) *state.Object
	// ArrivingPlane is the plane a PlanarWalk event arrived at.
	ArrivingPlane(ev events.Event) state.ObjID
	// ProtectionSource is the object whose qualities a targeting check reads
	// for source: an ability wrapper's source permanent, else source itself.
	ProtectionSource(source state.ObjID) state.ObjID
	// EchoGateHolds is the CR 702.35a echo intervening-if for source.
	EchoGateHolds(source state.ObjID) bool
	// SpellsCastThisTurn and ManaExpendTotal are the per-player turn tallies.
	SpellsCastThisTurn(p state.PlayerID) int
	ManaExpendTotal(p state.PlayerID) int32

	// IsLoyaltyAbility and IsManaAbilityAPI are rules' ability classifiers
	// (a planeswalker loyalty ability; the mana-ability APIs).
	IsLoyaltyAbility(ab *cards.SA) bool
	IsManaAbilityAPI(api string) bool
}

// Facts is the per-emit trigger context rules keeps beside the event being
// matched: values set around one emit and consumed by the trigger check it
// runs, never replayed (each is rebuilt by the same synchronous emit on
// replay). The slices and the map are rules-owned and read-only.
type Facts struct {
	// Damaging is the source of the damage being emitted and CombatDamaging
	// whether it is combat damage; DmgSrcOverride, when set, overrides both.
	Damaging       state.ObjID
	CombatDamaging bool
	DmgSrcOverride state.ObjID
	// DeclaredAttackers is the whole declare-attackers batch while its events
	// are emitted.
	DeclaredAttackers []state.ObjID
	// TapObj, TapPlayer and TapEntering are emitTap's provenance for the Tap
	// being matched: who tapped it, and whether it is a tapped entry state.
	TapObj      state.ObjID
	TapPlayer   state.PlayerID
	TapEntering bool
	// TappingForMana is the permanent tapped for mana right now and
	// TappingManaProduced the mana it produced.
	TappingForMana      state.ObjID
	TappingManaProduced string
	// GoadProbe is the static-goad derivation's re-entry guard: above zero a
	// matcher must not bind the static goad set into an IsGoaded filter.
	GoadProbe int
	// LifeLossBatch is the open life-loss batch's events, and
	// FinishingLifeLossBatch whether the batch is being closed (the one point
	// Mode$ LifeLostAll matches).
	LifeLossBatch          []events.Event
	FinishingLifeLossBatch bool
	// TappedTurn maps a permanent to the turn it last became tapped.
	TappedTurn map[state.ObjID]int32
}

// Activation is an in-flight activation's facts while its cast flow is open:
// its answered targets and the counters its cost removed (a fixed SubCounter
// part counts its N, an announced part the announced X).
type Activation struct {
	Targets         []state.Target
	CountersRemoved int32
}
