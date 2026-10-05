package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
)

// millScryProposalMatches scopes either held instruction proposal by player
// and the shared condition gate. The outer matcher owns ActiveZones$.
func millScryProposalMatches(r cards.Repl, ev events.Event, playerMatches func(string) bool, conditionHolds func() bool) bool {
	if r.EventKind() == cards.ReplMill {
		if ev.Kind != events.MillProposal {
			return false
		}
	} else if ev.Kind != events.Scry {
		return false
	}
	if v, ok := r.Param(cards.PKValidPlayer); ok && !playerMatches(v) {
		return false
	}
	return conditionHolds()
}

// continueMillReplacements rewrites the count of one proposed Mill instruction.
// The only supported bodies are ReplaceEffect count rewrites of Number; other
// bodies fail closed and are surfaced with a Note.
func (e *Engine) continueMillReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		if !e.replacementMatches(*m.repl, m.id, ev) {
			continue
		}
		ctx := e.replCtx(m, ev)
		if m.repl.With == nil || !millCountBodySupported(ctx, m.repl.With) {
			e.emit(events.Event{Kind: events.Note, Obj: m.id, Text: "unimplemented Mill replacement"})
			continue
		}
		before := ev.Amount
		e.runReplaceWith(ctx, ev.Obj, m.repl.With, &ev)
		if ev.Amount == before {
			e.emit(events.Event{Kind: events.Note, Obj: m.id, Text: "unimplemented Mill replacement"})
		}
	}
	return ev, true
}

const millReplaceCountNumberPrefix = "ReplaceCount$Number/"

var supportedMillCountOps = map[string]struct{}{"Plus.4": {}, "Twice": {}}

func millCountBodySupported(ctx *effects.Ctx, with *cards.SA) bool {
	for s := with; s != nil; s = s.Sub {
		if s.APIKind() != cards.APIReplaceEffect {
			return false
		}
		rp := effects.ReplaceEffectOf(s)
		if replaceEventFieldCodes.Code(rp.VarName) != replaceEventFieldNumber {
			return false
		}
		expr := rp.VarValue.Text
		if ctx.SVars != nil {
			if body, ok := ctx.SVars[expr]; ok {
				expr = body
			}
		}
		if !strings.HasPrefix(expr, millReplaceCountNumberPrefix) {
			return false
		}
		if _, ok := supportedMillCountOps[strings.TrimPrefix(expr, millReplaceCountNumberPrefix)]; !ok {
			return false
		}
	}
	return true
}
