package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// chars_board.go is package rules' side of the rules/chars seam (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §9, W5
// step E4): charsBoard implements chars.Board over the Engine, so the layer
// walk reads the game without holding the engine. Every method is a pure
// read that forwards to the engine reader the walk called before it moved,
// so every derived characteristic -- and every decision, event and chain
// head built from one -- is unchanged.
//
// charsBoard is the Engine itself under another method set: asChars is a
// pointer conversion, so passing it as a chars.Board neither allocates nor
// copies, and its methods are not Engine methods. The walk's per-call
// buffers and re-entry guard live in Engine.charsWalk (engine_scratch.go),
// which every walk is handed alongside the Board.
type charsBoard Engine

var _ chars.Board = (*charsBoard)(nil)

// asChars views e as the chars package's read-only Board.
func asChars(e *Engine) *charsBoard { return (*charsBoard)(e) }

func (b *charsBoard) Game() *state.Game { return b.G }

func (b *charsBoard) Active() []state.ContinuousEffect {
	e := (*Engine)(b)
	return e.active()
}

// Matches is matchesWithCharsPT: the one applicability seam every layer's
// match shares (phasing, the Card.Self early-out and its verify mode, the
// cast-provenance gate, and the spec context built on this side with its
// resolver closure and the walk's bound lists).
func (b *charsBoard) Matches(ce *state.ContinuousEffect, id state.ObjID, types, keywords []string, atStack state.Zone, pt chars.PTBind) bool {
	e := (*Engine)(b)
	return e.matchesWithCharsPT(ce, id, types, keywords, atStack, pt.Power, pt.Toughness, pt.BasePower, pt.BaseToughness, pt.Has)
}

func (b *charsBoard) StaticAmount(ce *state.ContinuousEffect, expr string, anchor state.ObjID) int32 {
	e := (*Engine)(b)
	return e.staticAmountOn(ce, expr, anchor)
}

func (b *charsBoard) ControllerOf(id state.ObjID) state.PlayerID {
	e := (*Engine)(b)
	return e.controllerOf(id)
}

func (b *charsBoard) LandTypeWords() []string { return b.landTypeWords }

func (b *charsBoard) CDAContext(o *state.Object, f *cards.Face) *effects.Ctx {
	e := (*Engine)(b)
	return e.cdaEvalCtx(o, f)
}

func (b *charsBoard) Host() effects.Host { return (*Engine)(b) }
