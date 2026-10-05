package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type loseManaRuntime interface {
	controllerOf(state.ObjID) state.PlayerID
	runReplaceWith(*effects.Ctx, state.ObjID, *cards.SA, *events.Event)
	replacementMatches(cards.Repl, state.ObjID, events.Event) bool
}

func loseManaReplacementApplies(ev events.Event, playerMatches, conditionHolds bool) bool {
	return ev.Kind == events.ManaClear && playerMatches && conditionHolds
}

func convertLoseManaColor(m replMatch, color byte, runtime loseManaRuntime) byte {
	ctx := effects.NewCtxPtr(m.id, runtime.controllerOf(m.id), effects.CtxInit{})
	ctx.Mana.Amount, ctx.Mana.Type = 1, string(color)
	if m.face != nil {
		effects.SetSVars(ctx, m.face.SVars)
	}
	runtime.runReplaceWith(ctx, m.id, m.repl.With, nil)
	if len(ctx.Mana.Type) != 1 {
		return 0
	}
	return ctx.Mana.Type[0]
}

func emitLoseManaClear(ev events.Event, emit func(events.Event) events.Event, setApplying func(bool)) {
	setApplying(true)
	defer setApplying(false)
	emit(ev)
}

func handleLoseManaChoice(rc replChoice, selected int, player state.PlayerID, before *triggerSnapshot,
	game *state.Game, runtime loseManaRuntime, emit func(events.Event) events.Event,
	setApplying func(bool), setBefore func(*triggerSnapshot), pose func(events.Event, []replMatch)) {
	if selected < 0 || selected >= len(rc.cands) || int(rc.ev.Player) >= len(game.Players) {
		setBefore(before)
		emit(events.Event{Kind: events.Note, Player: player, Text: "mana replacement-order answer out of range"})
		return
	}
	m, p := rc.cands[selected], game.Players[rc.ev.Player]
	applyLoseManaReplacement(rc.ev, m, p,
		func(color byte) byte { return convertLoseManaColor(m, color, runtime) },
		func(clear events.Event) { emitLoseManaClear(clear, emit, setApplying) }, emit)

	// Applying one replacement rewrites the still-unspent pool. CR 616.1
	// then asks again if another unused effect applies to that resulting loss.
	remaining := make([]replMatch, 0, len(rc.cands)-1)
	p = game.Players[rc.ev.Player]
	if p.Pool.Total() > p.PersistentMana.Total() {
		for i, candidate := range rc.cands {
			if i != selected && runtime.replacementMatches(*candidate.repl, candidate.id, rc.ev) {
				remaining = append(remaining, candidate)
			}
		}
	}
	if len(remaining) > 1 {
		pose(rc.ev, remaining)
	} else if len(remaining) == 1 {
		m = remaining[0]
		applyLoseManaReplacement(rc.ev, m, p,
			func(color byte) byte { return convertLoseManaColor(m, color, runtime) },
			func(clear events.Event) { emitLoseManaClear(clear, emit, setApplying) }, emit)
	}
}

func applyLoseManaBoundary(ev events.Event, matches []replMatch, game *state.Game, runtime loseManaRuntime,
	emit func(events.Event) events.Event, setApplying func(bool), pose func(events.Event, []replMatch)) (events.Event, bool) {
	if len(matches) > 1 {
		pose(ev, matches)
		return ev, true
	}
	if len(matches) == 0 || int(ev.Player) >= len(game.Players) {
		return ev, false
	}
	m, p := matches[0], game.Players[ev.Player]
	return applyLoseManaReplacement(ev, m, p,
		func(color byte) byte { return convertLoseManaColor(m, color, runtime) },
		func(clear events.Event) { emitLoseManaClear(clear, emit, setApplying) }, emit)
}

func convertedManaCounter(counter string, color byte) string {
	if len(counter) == 2 && counter[0] == 'S' {
		return "S" + string(color)
	}
	if tag, _, ok := state.TypedManaCounter(counter); ok {
		return state.ManaUnitTags[tag] + string(color)
	}
	return string(color)
}

func restrictedBatchesForSlot(restrictions []state.ManaRestriction, slot int, limit int32) []state.ManaRestriction {
	var batches []state.ManaRestriction
	for _, batch := range restrictions {
		if batch.Persistent || state.ManaSlot(batch.Color) != slot || limit <= 0 {
			continue
		}
		if batch.Amount > limit {
			batch.Amount = limit
		}
		batches = append(batches, batch)
		limit -= batch.Amount
	}
	return batches
}

// applyLoseManaReplacement applies one conversion to the mana a boundary
// would remove. Engine-specific work is supplied as callbacks so this helper
// owns only the pool calculation and event sequence, not the Engine surface.
func applyLoseManaReplacement(ev events.Event, m replMatch, p state.Player,
	convert func(byte) byte, emitClear func(events.Event), emitMana func(events.Event) events.Event) (events.Event, bool) {
	if m.repl == nil || m.repl.With == nil {
		return ev, false
	}
	chars := [...]byte{'W', 'U', 'B', 'R', 'G', 'C'}
	var converted []events.Event
	for slot, color := range chars {
		if strings.ContainsRune(ev.Text, rune(color)) {
			continue
		}
		available := p.Pool[slot] - p.PersistentMana[slot]
		if available <= 0 {
			continue
		}
		out := convert(color)
		if out == 0 {
			continue
		}
		// Partition tagged units in the same order as ManaClear's drain.
		snow := p.Snow[slot]
		var typed [7]int32
		for tag := range typed {
			base, _ := state.ManaUnitTypes(tag)
			if tag >= state.TypedArtifactTreasure {
				typed[tag] = p.ArtifactTyped[base][slot]
			} else {
				typed[tag] = p.TypedMana[base][slot]
				if tag < state.TypedArtifact {
					typed[tag] -= p.ArtifactTyped[base][slot]
				}
			}
		}
		for _, batch := range restrictedBatchesForSlot(p.RestrictedMana, slot, available) {
			text := events.ManaRestrictionTextNC(batch.Valid, batch.Source, batch.NoCounter)
			text = events.ManaAddsCountersText(text, batch.AddsCounters)
			converted = append(converted, events.Event{Kind: events.ManaAdd, Player: ev.Player,
				Amount: batch.Amount, Counter: convertedManaCounter(batch.Color, out), Text: text})
			if len(batch.Color) == 2 && batch.Color[0] == 'S' {
				snow -= batch.Amount
			} else if tag, _, ok := state.TypedManaCounter(batch.Color); ok {
				typed[tag] -= batch.Amount
			}
			available -= batch.Amount
		}
		for tag, n := range typed {
			if n > available {
				n = available
			}
			if n > 0 {
				converted = append(converted, events.Event{Kind: events.ManaAdd, Player: ev.Player,
					Amount: n, Counter: state.ManaUnitTags[tag] + string(out)})
				available -= n
			}
		}
		if snow > available {
			snow = available
		}
		if snow > 0 {
			converted = append(converted, events.Event{Kind: events.ManaAdd, Player: ev.Player,
				Amount: snow, Counter: "S" + string(out)})
			available -= snow
		}
		if available > 0 {
			converted = append(converted, events.Event{Kind: events.ManaAdd, Player: ev.Player,
				Amount: available, Counter: string(out), Text: "converted from unspent mana"})
		}
	}
	emitClear(ev)
	for _, event := range converted {
		emitMana(event)
	}
	return ev, true
}
