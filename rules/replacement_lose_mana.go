package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func loseManaReplacementApplies(ev events.Event, playerMatches, conditionHolds bool) bool {
	return ev.Kind == events.ManaClear && playerMatches && conditionHolds
}

// applyLoseManaReplacement applies the corpus's ReplaceMana conversion to
// each unit that the pending ManaClear would actually remove. The clear is
// logged first, then converted units are added, so persistent and
// stat:UnspentMana-protected units retain their existing event-fold behavior.
func (e *Engine) applyLoseManaReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	if m.repl == nil || m.repl.With == nil || int(ev.Player) >= len(e.G.Players) {
		return ev, false
	}
	pool := e.G.Players[ev.Player].Pool
	persistent := e.G.Players[ev.Player].PersistentMana
	var converted state.Mana
	chars := [...]byte{'W', 'U', 'B', 'R', 'G', 'C'}
	for slot, color := range chars {
		if strings.ContainsRune(ev.Text, rune(color)) {
			continue
		}
		count := pool[slot] - persistent[slot]
		if count <= 0 {
			continue
		}
		ctx := effects.NewCtxPtr(m.id, e.controllerOf(m.id), effects.CtxInit{})
		ctx.Mana.Amount, ctx.Mana.Type = 1, string(color)
		if m.face != nil {
			effects.SetSVars(ctx, m.face.SVars)
		}
		e.runReplaceWith(ctx, m.id, m.repl.With, nil)
		if len(ctx.Mana.Type) == 1 {
			converted[state.ManaIndex(ctx.Mana.Type[0])] += count
		}
	}
	// The original clear is still the event that empties the at-risk units.
	// applyingReplacement prevents it re-entering this same replacement.
	saved := e.applyingReplacement
	e.applyingReplacement = true
	e.emit(ev)
	e.applyingReplacement = saved
	for slot, n := range converted {
		if n > 0 {
			e.emit(events.Event{Kind: events.ManaAdd, Player: ev.Player, Amount: n, Counter: string(chars[slot]), Text: "converted from unspent mana"})
		}
	}
	return ev, true
}
