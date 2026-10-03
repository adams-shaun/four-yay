package combat

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Board is everything the combat predicates read about the game: the live
// state, the layer-derived characteristics of an object, the continuous
// restriction registry, the printed statics of a mode and the engine's
// filter-spec matcher. Package rules implements it over its Engine
// (rules/combat_board.go); a predicate here never holds the engine itself.
//
// Every method is a pure read. None emits an event, and the returned slices
// are borrowed views the caller must not retain past its own call or write
// to. The method count is ratcheted (internal/archtest
// TestCombatBoardOnlyShrinks): derive a new fact from an existing method
// rather than adding one.
type Board interface {
	// Game is the live match state, for reading only.
	Game() *state.Game
	// Log is the event log (the ValidAttacked$ attackedYouTheirLastTurn read
	// is a pure log scan).
	Log() []events.Event

	// IsCreature reads the current layer-derived type list.
	IsCreature(id state.ObjID) bool
	// HasKW reports a DERIVED keyword (printed or layer-granted) by its
	// precompiled head.
	HasKW(id state.ObjID, kw Keyword) bool
	// Keywords is the object's current derived keyword list.
	Keywords(id state.ObjID) []string
	// Power is the object's current derived power.
	Power(id state.ObjID) int32
	// ObjectColors is the object's live colour set as WUBRG letters: layer-5
	// derived on the battlefield, the face read elsewhere.
	ObjectColors(o *state.Object) string
	// ProtectedFrom reports whether target has protection from source (CR
	// 702.16).
	ProtectedFrom(target, source state.ObjID) bool

	// Active is the active continuous-effect registry, in registry order.
	Active() []state.ContinuousEffect
	// RestrictionApplies reports whether a registered restriction's
	// ValidCard$/ValidTarget$ (or its remembered set) selects id.
	RestrictionApplies(ce *state.ContinuousEffect, id state.ObjID) bool
	// Statics is every printed S:Mode$ <mode> line on a battlefield
	// permanent, in the engine's deterministic APNAP/battlefield/static
	// order. mode is always a string literal at the call site (the param
	// census attributes the mode's reads through it).
	Statics(mode string) []Static
	// StaticGateHolds evaluates a static's shared condition gate (ClassBand$,
	// IsPresent$/PresentCompare$, Condition$, CheckSVar$), failing closed.
	StaticGateHolds(sv Static) bool
	// MatchesSpec evaluates a live-object filter spec against id with its
	// current derived characteristics, in the spec context of (source, you)
	// with the engine's numeric-RHS resolver and derived tables bound, plus a
	// registration's remembered objects (Card.IsRemembered) and captured
	// players (RememberedPlayer). The context is built on the engine side:
	// it carries a resolver closure that must not cross this interface,
	// where it would escape to the heap on every call.
	MatchesSpec(spec string, id, source state.ObjID, you state.PlayerID, remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool
	// MatchesStaticSpec is MatchesSpec in a printed static's own context:
	// its source, controller and SVar table.
	MatchesStaticSpec(spec string, id state.ObjID, sv Static) bool
	// GoadMatches is MatchesSpec (without captured players) under the
	// static-goad re-entry guard: a goad line's own Affected$ match must not
	// re-derive the goad set.
	GoadMatches(spec string, id, source state.ObjID, you state.PlayerID, remembered []state.ObjID) bool
	// PlayerSpecCtx is the player-filter context of a static source, with the
	// derived tables bound.
	PlayerSpecCtx(source state.ObjID) effects.PlayerSpecCtx
	// LandSpecCtx is the spec context a landwalk filter over the defender's
	// lands is matched in, with the layer-3/4 tables refreshed and bound for
	// the attacker's derivation.
	LandSpecCtx(defender state.PlayerID, attacker state.ObjID) effects.SpecContext
}

// Static is one printed static line on a battlefield permanent, as
// Board.Statics returns it: its source and controller, its raw and compiled
// parameters and the SVar table of the face that carries it.
type Static struct {
	Source     state.ObjID
	Controller state.PlayerID
	Params     map[string]string
	PS         *cards.ParamSet
	SVars      map[string]string
}

// ParamStr is Params[k] ("" when absent), through the compiled set.
func (sv Static) ParamStr(k cards.ParamKey) string {
	v, _ := cards.ParamSetParam(sv.PS, sv.Params, k)
	return v
}

// Param is Params[k] and whether k is present, through the compiled set.
func (sv Static) Param(k cards.ParamKey) (string, bool) {
	return cards.ParamSetParam(sv.PS, sv.Params, k)
}

// HasParam reports whether k is present, through the compiled set.
func (sv Static) HasParam(k cards.ParamKey) bool {
	_, ok := cards.ParamSetParam(sv.PS, sv.Params, k)
	return ok
}

// Keyword names a keyword the combat predicates test. Board.HasKW resolves it
// to the engine's precompiled head, so a keyword read stays a bitset test.
type Keyword uint8

// The keywords the combat predicates read.
const (
	KWDefender Keyword = iota
	KWHaste
	KWUnleash
	KWShadow
	KWFear
	KWIntimidate
	KWHorsemanship
	KWFlying
	KWReach
	KWSkulk
	// NumKeywords is the count of Keyword values (a table size, not a
	// keyword).
	NumKeywords
)
