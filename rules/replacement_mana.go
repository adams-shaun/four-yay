package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"strings"
)

// continueManaReplacements implements CR 616.1 for one in-flight ManaAdd.
// Each replacement may apply once. After every rewrite the full candidate set
// is re-checked against the NEW amount/type; if several apply, the player
// receiving the mana chooses which is applied next. A lone applicable effect
// is automatic. Only the final rewritten ManaAdd enters the event log, so a
// log-only replay needs no transient provenance or replacement state.
func (e *Engine) continueManaReplacements(ev events.Event, candidates []replMatch,
	applied []bool, changed, tapped bool, producer state.ObjID) (events.Event, bool) {
	if applied == nil {
		applied = make([]bool, len(candidates))
	}
	// A colour/order answer resumes after resolveManaEffectColor restored its
	// synchronous scratch. Rebind producer for every applicability recheck so
	// a parked replacement still sees the permanent that produced this mana.
	savedProducer := e.manaProducer
	e.manaProducer = producer
	defer func() { e.manaProducer = savedProducer }()
	for {
		var applicable []int
		savedTap := e.manaFromTap
		e.manaFromTap = tapped
		for i, m := range candidates {
			if !applied[i] && e.replacementMatches(*m.repl, m.id, ev) {
				applicable = append(applicable, i)
			}
		}
		e.manaFromTap = savedTap
		if len(applicable) == 0 {
			if !changed {
				return ev, false
			}
			stored := events.Emit(e.G, e.L, ev)
			e.loop.observeFrom(&stored, e.damaging, len(e.G.Objs))
			e.checkTriggers(&stored, nil, 0, 0, false)
			return stored, true
		}
		if len(applicable) > 1 && int(ev.Player) < len(e.G.Players) && !e.G.Players[ev.Player].Lost {
			e.poseManaReplacementChoice(ev, candidates, applied, applicable, changed, tapped, producer)
			return ev, true
		}
		// A sole applicable replacement is mandatory. A choice-valued colour
		// still belongs to the affected player, so park the in-flight event
		// before applying it. A departed player cannot answer and deterministically
		// takes the first colour, just as it takes the first competing effect.
		i := applicable[0]
		if manaReplacementNeedsColor(candidates[i]) && int(ev.Player) < len(e.G.Players) &&
			!e.G.Players[ev.Player].Lost {
			e.poseManaColorReplacementChoice(ev, candidates, applied, i, changed, tapped, producer)
			return ev, true
		}
		choice := ""
		if manaReplacementNeedsColor(candidates[i]) {
			choice = "W"
		}
		ev = e.applyOneManaReplacement(ev, candidates[i], choice)
		applied[i] = true
		changed = true
	}
}

// applyOneManaReplacement applies a body while its producer is bound in
// Engine scratch by continueManaReplacements (or by the resume wrapper).
func (e *Engine) applyOneManaReplacement(ev events.Event, m replMatch, color string) events.Event {
	if m.repl.With == nil {
		return ev
	}
	ctx := &effects.Ctx{Source: m.id, Controller: e.controllerOf(m.id),
		ManaAmount: ev.Amount, ManaType: ev.Counter, ManaChoice: color}
	if m.face != nil {
		effects.SetSVars(ctx, m.face.SVars)
	}
	// The producer is contextual (not ManaAdd.Obj), but ReplaceWith$ still
	// resolves against that object for Defined$/Remembered$ references.
	e.runReplaceWith(ctx, e.manaProducer, m.repl.With, nil)
	ev.Amount, ev.Counter = ctx.ManaAmount, ctx.ManaType
	return ev
}

// applyOneManaReplacementWithProducer restores the contextual producer for
// the one rewrite that occurs immediately after an answered replacement
// decision; continueManaReplacements then rebinds it for later rechecks.
func (e *Engine) applyOneManaReplacementWithProducer(ev events.Event, m replMatch, color string, producer state.ObjID) events.Event {
	saved := e.manaProducer
	e.manaProducer = producer
	defer func() { e.manaProducer = saved }()
	return e.applyOneManaReplacement(ev, m, color)
}

// manaReplacementNeedsColor identifies every choice-valued spelling the
// ReplaceMana primitive accepts. It follows effReplaceMana's precedence
// (ReplaceMana, then ReplaceType, then ReplaceColor), so an ignored lower-
// precedence parameter cannot accidentally pose a second choice.
func manaReplacementNeedsColor(m replMatch) bool {
	if m.repl == nil || m.repl.With == nil {
		return false
	}
	p := m.repl.With.Params
	kind := strings.TrimSpace(p["ReplaceMana"])
	if kind == "" {
		kind = strings.TrimSpace(p["ReplaceType"])
	}
	if kind == "" {
		kind = strings.TrimSpace(p["ReplaceColor"])
	}
	return strings.EqualFold(kind, "Any") || strings.EqualFold(kind, "Chosen")
}

// poseManaReplacementChoice parks a partially rewritten mana event until the
// player receiving it chooses the next applicable effect (CR 616.1). The full
// candidate set and applied bitmap survive the choice so applicability can be
// re-evaluated after the selected rewrite, including effects newly enabled by
// a changed ManaAmount$.
func (e *Engine) poseManaReplacementChoice(ev events.Event, candidates []replMatch,
	applied []bool, applicable []int, changed, tapped bool, producer state.ObjID) {
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceMana, ev: ev,
		cands: candidates, applied: append([]bool(nil), applied...),
		applicable: append([]int(nil), applicable...), changed: changed,
		manaTapped: tapped, manaProducer: producer, before: e.retainTriggerBefore()})
	if e.pending == nil {
		e.askReplacementChoice(ev.Player)
	}
}

// poseManaColorReplacementChoice parks a partially rewritten mana event while
// the receiving player chooses W/U/B/R/G for one choice-valued ReplaceMana
// body. Candidate state and the applied bitmap are retained so the answer can
// resume the same CR 616.1 applicability loop.
func (e *Engine) poseManaColorReplacementChoice(ev events.Event, candidates []replMatch,
	applied []bool, selected int, changed, tapped bool, producer state.ObjID) {
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceManaColor, ev: ev,
		cands: candidates, applied: append([]bool(nil), applied...), selected: selected,
		changed: changed, manaTapped: tapped, manaProducer: producer, before: e.retainTriggerBefore()})
	if e.pending == nil {
		e.askReplacementChoice(ev.Player)
	}
}
