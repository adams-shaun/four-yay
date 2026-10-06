package rules

// resolve_land_mayask.go is the ask-free predicate of a land play (the tape
// run a play_land intent begins): the land's entry is the one place the play
// can pose a decision (an as-enters choice, a shock land's pay-life
// election, Hideaway, a CR 616.1 order choice among entry replacements).

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tapeLandMayAsk reports whether playing land obj by p may pose a decision,
// judged from the face's entry text, the replacements on the board, and the
// optional-mana-conversion board gate. Land plays use the ordinary cast flow,
// but do not pay a spell mana cost and therefore cannot ask that election. true
// is always safe.
func tapeLandMayAsk(e *Engine, p state.PlayerID, obj state.ObjID) bool {
	o := e.G.Obj(obj)
	if o == nil {
		return true
	}
	f := o.Face()
	if f == nil || cards.FaceEntryMayAsk(f) {
		return true
	}
	if o.Card != nil {
		// A modal double-faced land may be played as its other face.
		for _, cf := range o.Card.Faces {
			if cf != nil && cards.FaceEntryMayAsk(cf) {
				return true
			}
		}
	}
	return tapeLandReplMayAsk(e, obj)
}

// tapeLandReplMayAsk is tapeReplMayAsk(e, "Moved") narrowed to the lines that
// can apply to land obj's own entry: a line movedLineRejects for that move
// (a self-only ValidCard$ on another object -- an ETB-tapped land or an
// etbCounter creature sitting in a library or hand -- or another
// Destination$) never matches the event, so it neither elects nor competes
// in a CR 616.1 order choice. Every board in a deck with two such cards
// otherwise checkpointed every land play.
func tapeLandReplMayAsk(e *Engine, obj state.ObjID) bool {
	n := 0
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		if ce := &ceL[ceI]; ce.ReplacementEvent == "Moved" {
			if n++; n > 1 || cards.ReplParamsMayElect(ce.ReplacementParams) {
				return true
			}
		}
	}
	ev := events.Event{Obj: obj, To: state.ZBattlefield}
	if !replArenaAskFor(e, replAskMovedOther) {
		// No arena object carries a Moved line another object's entry can
		// meet: only obj's own lines count (replArenaMask.ask).
		ask := tapeOwnEntryReplMayAsk(e, obj, ev, n)
		if replZoneSkipVerify && ask != tapeLandReplWalk(e, ev, n) {
			panic("rules: replacement ask class skipped a land entry walk that disagrees")
		}
		return ask
	}
	return tapeLandReplWalk(e, ev, n)
}

// tapeOwnEntryReplMayAsk is tapeLandReplWalk over obj's own face alone.
func tapeOwnEntryReplMayAsk(e *Engine, obj state.ObjID, ev events.Event, n int) bool {
	o := e.G.Obj(obj)
	if o == nil {
		return false
	}
	f := o.Face()
	if f == nil {
		return false
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event != "Moved" || movedLineRejects(r, obj, ev) {
			continue
		}
		if n++; n > 1 || cards.ReplMayElect(r) {
			return true
		}
	}
	return false
}

// tapeLandReplWalk is tapeLandReplMayAsk's walk over every replacement
// source, n lines already counted.
func tapeLandReplWalk(e *Engine, ev events.Event, n int) bool {
	ask := false
	e.forEachReplacementSourceFor(replEventBits[cards.ReplMoved], func(id state.ObjID) {
		o := e.G.Obj(id)
		if ask || o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event != "Moved" || movedLineRejects(r, id, ev) {
				continue
			}
			if n++; n > 1 || cards.ReplMayElect(r) {
				ask = true
				return
			}
		}
	})
	return ask
}

// tapeStepMayAsk reports whether the pass that ends the current step (empty
// stack) begins a turn-based action that may pose a decision: the draw step's
// draw, which a Dredge card in a graveyard replaces with its election.
func tapeStepMayAsk(e *Engine) bool {
	switch e.G.Step {
	case state.StepUpkeep:
		return tapeDredgeMayAsk(e)
	case state.StepDeclareBlockers, state.StepCombatDamage:
		// The pass into the combat damage step: a DamageDone replacement
		// that elects asks as the damage is dealt.
		return tapeReplMayAsk(e, "DamageDone") || tapeAnyReplBodyMayAsk(e)
	}
	return false
}

// tapeAnyReplBodyMayAsk reports whether any replacement source on the board
// (or in a replacement-bearing zone) carries a ReplaceWith$ body that may
// ask, or an Optional$ election, for any event: the broad board gate of a
// turn-based action whose emissions can run any replacement (combat damage
// and the deaths it causes).
func tapeAnyReplBodyMayAsk(e *Engine) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		if ce := &ceL[ceI]; ce.ReplacementEvent != "" && (cards.ReplParamsMayElect(ce.ReplacementParams) || effectReplBodyMayAsk(e, ce)) {
			return true
		}
	}
	if !replArenaAskFor(e, replAskBody) {
		// No arena object carries a line the walk below could count
		// (replArenaMask.ask).
		if replZoneSkipVerify && tapeAnyReplBodyWalk(e) {
			panic("rules: replacement ask class skipped a body walk that asks")
		}
		return false
	}
	return tapeAnyReplBodyWalk(e)
}

// tapeAnyReplBodyWalk is tapeAnyReplBodyMayAsk's walk over every
// replacement source.
func tapeAnyReplBodyWalk(e *Engine) bool {
	ask := false
	e.forEachReplacementSourceFor(^uint32(0), func(id state.ObjID) {
		o := e.G.Obj(id)
		if ask || o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event == "Moved" {
				continue // entries are asked where they happen (the land play, a resolution)
			}
			if cards.ReplMayElect(r) || (r.With != nil && (cards.SAChainMayAsk(r.With, f.SVars, nil, false) ||
				// An ask-free body can still open a board gate: Lich's
				// GainLife -> Draw body meets a Dredge card's election.
				mayAskOnBoard(e, mayAskKnown|uint32(cards.SAChainBoardGates(r.With, f.SVars))<<mayAskGateShift))) {
				ask = true
				return
			}
		}
	})
	return ask
}

// effectReplBodyMayAsk reports whether an Effect-created replacement's
// ReplaceWith$ body chain may ask (an UnlessCost$ rider on the body's
// SubAbility$, the torgal_a_fine_hound / communal_brewing self-exile shape).
// The body is plain text on the effect, resolved under its source's SVar
// context, so it is judged here rather than through a face's facts record.
// Unlike a printed Moved line, an Effect-created one has no face entry text
// the resolving object's own predicate could see, so Moved is not skipped.
func effectReplBodyMayAsk(e *Engine, ce *state.ContinuousEffect) bool {
	if ce.ReplacementBody == "" {
		return false
	}
	with := replacementBodySA(ce.ReplacementBody)
	if with == nil {
		return false
	}
	var svars map[string]string
	if src := e.G.Obj(ce.Source); src != nil {
		if f := src.Face(); f != nil {
			svars = f.SVars
		}
	}
	with.Sub = cards.ResolveSVar(svars, with.ParamStr(cards.PKSubAbility))
	return cards.SAChainMayAsk(with, svars, nil, false)
}
