package chars

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Board is everything the layer walk reads about the game that it does not
// compute itself: the live state, the sorted continuous-effect list, the
// engine's Affected$ applicability match, a static's numeric amount, the
// controller read, the corpus land-type vocabulary and the CDA evaluation
// context. Package rules implements it over its Engine
// (rules/chars_board.go); nothing here holds the engine itself.
//
// Every method is a pure read. None emits an event, and the returned slices
// are borrowed views the caller must not retain past its own call or write
// to. The method count is ratcheted (internal/archtest
// TestCharsBoardOnlyShrinks): derive a new fact from an existing method
// rather than adding one.
type Board interface {
	// Game is the live match state, for reading only.
	Game() *state.Game
	// Active is the engine's active continuous-effect list -- the live
	// registered effects plus the memoized static scan, sorted by layer,
	// sublayer and timestamp. It is a cached, idempotent read: two calls
	// with no event between them return the same slice.
	Active() []state.ContinuousEffect
	// Matches reports whether ce applies to id: the Affected$ applicability
	// test every layer shares (phasing, the Card.Self early-out, the
	// cast-provenance qualifiers and the effects filter match). types and
	// keywords are the walk's types-so-far and keywords-so-far lists (a nil
	// list is the effects filter's "unbound, read the printed face"); atStack
	// is the zone override (0 = the object's live zone); pt binds the layer-7
	// walk's in-progress value when pt.Has. The spec context is built on the
	// engine side: it carries a resolver closure that must not cross this
	// interface, where it would escape to the heap on every call.
	Matches(ce *state.ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone, pt PTBind) bool
	// StaticAmount evaluates a static's numeric P/T expression with the
	// evaluation context's source anchored on anchor (the grantor itself, or
	// the affected object for Forge's AffectedX convention) and the SVar
	// table taken from the grantor.
	StaticAmount(ce *state.ContinuousEffect, expr string, anchor state.ObjID) int32
	// ControllerOf is the nil-safe controller read the finished record
	// carries (a recurring Effect's observer resolves the registration's
	// owner).
	ControllerOf(id state.ObjID) state.PlayerID
	// LandTypeWords is the corpus land-subtype vocabulary an
	// AllNonBasicLandType grant expands against (CorpusLandTypeWords over the
	// game's name universe).
	LandTypeWords() []string
	// CDAContext is the evaluation context a characteristic-defining
	// ability's value expression resolves through, with the board's
	// published layer-4 derived type table bound.
	CDAContext(o *state.Object, f *cards.Face) *effects.Ctx
	// Host is the engine as the effects tier's Host, for the CDA count
	// evaluator (effects.EvalCountOK).
	Host() effects.Host
}

// PTBind is the layer-7 walk's in-progress value bound into an Affected$
// match made during that walk (Has false: none). Calling back into the
// derivation for it would recurse through the same active layer scan.
type PTBind struct {
	Power, Toughness, BasePower, BaseToughness int32
	Has                                        bool
}

// PTFrame is one in-progress layer-7 snapshot: the running value of the
// object whose P/T walk is on the stack, exposed to effects-side P/T
// references and Count$Valid scans (rules' InProgressDerivedPT and
// FilterDerivedPT).
type PTFrame struct {
	ID                                         state.ObjID
	Power, Toughness, BasePower, BaseToughness int32
	// PreCounterPower/PreCounterToughness are the same running value WITHOUT
	// the layer-7d counters. A layer-7c static whose amount reads the P/T of
	// an object the walk is currently deriving (Snowblind's AddToughness$
	// -NotAttackingY, sized from the enchanted creature's own toughness) must
	// see the value before this effect; CR 613.4 orders counters after every
	// 7c modify, so that value excludes them while Power/Toughness -- the
	// counter-inclusive pair FilterDerivedPT hands to Count$Valid -- includes
	// them.
	PreCounterPower, PreCounterToughness int32
}

// Scratch is the layer walk's engine-owned per-call state. Package rules
// embeds one in its Engine and passes it to every walk, so nested
// derivations share the re-entry guard and buffers exactly as they did when
// these were Engine fields. It is pure scratch: zero at every intent
// boundary, and a clone starts from a zero Scratch (never aliasing the
// original's buffers).
type Scratch struct {
	// KW / Types are Compute's scratch keyword and type buffers: the build
	// rewrites them in place so repeated derived-characteristic reads do not
	// allocate. Depth is the re-entry guard for the reuse: a nested build
	// mid-walk owns private buffers instead of clobbering the outer build's.
	KW    []string
	Types []string
	Depth int
	// PTFrames are the in-progress layer-7 snapshots, innermost last.
	PTFrames []PTFrame
	// ColorsSet/ColorsID/Colors are the finished layer-5 colour answer for
	// the object whose build is mid-way (set before its layer-7 P/T walk,
	// restored on the way out), so a layer-7 pump expression that counts the
	// object's own colours is served without re-entering the derivation and
	// recursing forever.
	ColorsSet bool
	ColorsID  state.ObjID
	Colors    string
}

// InProgress returns the innermost active layer-7 frame for id; unlike
// rules' FilterDerivedPT it never starts a fresh derivation when no frame
// exists.
func (s *Scratch) InProgress(id state.ObjID) (PTFrame, bool) {
	for i := len(s.PTFrames) - 1; i >= 0; i-- {
		if frame := s.PTFrames[i]; frame.ID == id && id != 0 {
			return frame, true
		}
	}
	return PTFrame{}, false
}
