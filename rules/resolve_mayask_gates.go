package rules

// resolve_mayask_gates.go is the ask-free predicate's board-gate census: one
// decision per replaceable event kind (cards.ReplEvent). A resolution whose
// text is ask-free can still pose a decision through a replacement on an
// event it proposes -- an Optional$ election, or two replacements competing
// for one event (the CR 616.1 order choice) -- and text cannot see the
// board. cards.AskFreeAPIEvents names the events each allowlisted API can
// propose; every such event has a gate here that reads the replacement
// sources, and every event no allowlisted API proposes says why.
// TestReplEventGateCensus holds the table complete in both directions.

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// replEventGate is one event kind's decision: ask, the board gate an
// ask-free resolution proposing the event meets, or unreachable, why no
// ask-free resolution proposes it.
type replEventGate struct {
	ask         func(e *Engine) bool
	unreachable string
}

var replEventGates = [cards.ReplEventCount]replEventGate{
	cards.ReplMoved:   {ask: tapeMovedReplMayAsk},
	cards.ReplUntap:   {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "Untap") }},
	cards.ReplCounter: {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "Counter") }},
	// AddCounter is gated for every resolution (tapeBoardCompetes); reading
	// it again here keeps the census uniform.
	cards.ReplAddCounter:  {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "AddCounter") }},
	cards.ReplGainLife:    {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "GainLife") }},
	cards.ReplLifeReduced: {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "LifeReduced") }},
	cards.ReplPayLife:     {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "PayLife") }},
	cards.ReplDamageDone:  {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "DamageDone") }},
	cards.ReplCreateToken: {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "CreateToken") }},
	cards.ReplAttached:    {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "Attached") }},
	// A draw: competing Draw/DrawCards replacements, or a Dredge card in a
	// graveyard offering its election (CR 702.55).
	cards.ReplDraw:      {ask: func(e *Engine) bool { return tapeReplMayAsk(e, "Draw") || tapeDredgeMayAsk(e) }},
	cards.ReplDrawCards: {unreachable: "an alias of Draw: replEventBit shares Draw's bit and the Draw gate counts both"},

	cards.ReplBeginPhase:     {unreachable: "turn structure; no allowlisted API begins a phase"},
	cards.ReplBeginTurn:      {unreachable: "turn structure; no allowlisted API begins a turn"},
	cards.ReplTransform:      {unreachable: "no allowlisted API transforms (SetState is not allowlisted)"},
	cards.ReplProduceMana:    {unreachable: "mana abilities are not allowlisted; a mana rider has its own tape gate (offstack_mana_rider_tape.go)"},
	cards.ReplCascade:        {unreachable: "cascade is a cast trigger body outside the allowlist"},
	cards.ReplExplore:        {unreachable: "no allowlisted API explores"},
	cards.ReplGameLoss:       {unreachable: "a game loss is a state-based action after the resolution, or a LosesGame body outside the allowlist"},
	cards.ReplGameWin:        {unreachable: "a WinsGame body is outside the allowlist"},
	cards.ReplRollDice:       {unreachable: "RollDice is not allowlisted"},
	cards.ReplRollPlanarDice: {unreachable: "planar dice are a special action, never a resolution"},
	cards.ReplScry:           {unreachable: "Scry is not allowlisted"},
	cards.ReplTurnFaceUp:     {unreachable: "a turn-up is a special action; its tape boundary is turnup_tape.go"},
	cards.ReplLoseMana:       {unreachable: "turn-structure ManaClear is not proposed by an allowlisted resolution"},
}

// tapeMovedReplMayAsk is the Moved gate of an otherwise ask-free resolution
// that moves objects (a destroy, a bounce, a mill, a countered spell, a
// token's entry): two Moved replacements that can apply to one of its moves
// (Rest in Peace and Leyline of the Void competing for a destroyed card, or
// a commander's own replacement), or one that elects. A permanent's own
// entry line (a self-only ValidCard$ with Destination$ Battlefield: an
// ETB-tapped land in a hand or on the battlefield) is skipped: the only
// entries an ask-free chain makes are new tokens and the source returning
// itself, whose own entry text the text half judges (FaceEntryMayAsk).
func tapeMovedReplMayAsk(e *Engine) bool {
	n := 0
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		if ce := &ceL[ceI]; ce.ReplacementEvent == "Moved" {
			if n++; n > 1 || cards.ReplParamsMayElect(ce.ReplacementParams) {
				return true
			}
		}
	}
	ask := false
	e.forEachReplacementSourceFor(replEventBit("Moved"), func(id state.ObjID) {
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
			if r.EventKind() != cards.ReplMoved {
				continue
			}
			if movedOwnEntryLine(r, id) {
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

// movedOwnEntryLine reports whether a Moved line is a permanent's own entry
// replacement: a self-only ValidCard$ with Destination$ Battlefield. It reads
// the line only through movedLineRejects (the dispatch's own prefilter): the
// line rejects another object's entry, admits its source's entry, and
// rejects its source's move to a graveyard (so Destination$ is present and
// is the battlefield).
func movedOwnEntryLine(r *cards.Repl, source state.ObjID) bool {
	return movedLineRejects(r, source, events.Event{Obj: 0, To: state.ZBattlefield}) &&
		!movedLineRejects(r, source, events.Event{Obj: source, To: state.ZBattlefield}) &&
		movedLineRejects(r, source, events.Event{Obj: source, To: state.ZGraveyard})
}
