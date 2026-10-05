package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
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

// millReplacementHost is the narrow replacement seam needed by a held mill
// instruction; the caller owns the game and the decision tape.
type millReplacementHost interface {
	replacementMatches(cards.Repl, state.ObjID, events.Event) bool
	replCtx(replMatch, events.Event) *effects.Ctx
	runReplaceWith(*effects.Ctx, state.ObjID, *cards.SA, *events.Event)
	emit(events.Event) events.Event
}

// millReplacementAsk keeps the decision-tape callback outside the already
// ceiling-limited replacement dispatch.
func millReplacementAsk(h effects.Host) func(*decision.Decision) []decision.Option {
	return func(d *decision.Decision) []decision.Option {
		chosen, _ := effects.AskTape(h, d)
		return chosen
	}
}

// continueMillReplacements rewrites the count of one proposed Mill instruction.
// The only supported bodies are ReplaceEffect count rewrites of Number; other
// bodies fail closed and are surfaced with a Note.
func continueMillReplacements(h millReplacementHost, g *state.Game, ask func(*decision.Decision) []decision.Option, ev events.Event, matches []replMatch) (events.Event, bool) {
	used := make([]bool, len(matches))
	unsupportedNoted := make([]bool, len(matches))
	for {
		var applicable []int
		for i, m := range matches {
			if used[i] || !h.replacementMatches(*m.repl, m.id, ev) {
				continue
			}
			ctx := h.replCtx(m, ev)
			if m.repl.With != nil && millCountBodySupported(ctx, m.repl.With) {
				applicable = append(applicable, i)
			} else if !unsupportedNoted[i] {
				unsupportedNoted[i] = true
				h.emit(events.Event{Kind: events.Note, Obj: m.id, Text: "unimplemented Mill replacement"})
			}
		}
		if len(applicable) == 0 {
			return ev, true
		}
		i := applicable[0]
		if len(applicable) > 1 && int(ev.Player) < len(g.Players) && !g.Players[ev.Player].Lost {
			d := millReplacementDecision(g, ev, matches, applicable)
			chosen := ask(d)
			if len(chosen) == 1 && chosen[0].Index >= 0 && chosen[0].Index < len(applicable) {
				i = applicable[chosen[0].Index]
			}
		}
		used[i] = true
		m := matches[i]
		ctx := h.replCtx(m, ev)
		h.runReplaceWith(ctx, ev.Obj, m.repl.With, &ev)
	}
}

func millReplacementDecision(g *state.Game, ev events.Event, matches []replMatch, applicable []int) *decision.Decision {
	d := &decision.Decision{Player: ev.Player, Kind: decision.KReplacement, Min: 1, Max: 1,
		Source: ev.Obj, ResumeKind: "mill_replacement",
		Prompt: "Several replacement effects would modify this mill: choose which applies next."}
	for _, i := range applicable {
		m := matches[i]
		label := "Apply a replacement"
		if o := g.Obj(m.id); o != nil && o.Face() != nil {
			label = "Apply " + o.Face().Name + "'s replacement"
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replacement", Obj: m.id, Label: label})
	}
	return d
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
