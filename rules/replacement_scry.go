package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"strconv"
	"strings"
)

// planarDieFaceName names one planar-die roll result (CR 901.3a): the die
// is a six-sided die with four blank faces, one planeswalk face and one
// chaos face. Forge rolls it as an ordinary d6 with the 5/6 split.
func planarDieFaceName(result int32) string {
	switch result {
	case 5:
		return "planeswalk"
	case 6:
		return "chaos"
	default:
		return "blank"
	}
}

// continuePlanarRollReplacements applies every applicable planar-dice
// replacement (the Ichor Elixir class: "if you would roll one or more
// planar dice, instead roll that many planar dice plus one and ignore
// one") to one PlanarRoll event, then performs the roll itself — the
// emitted event is only the proposal (its Amount is the pre-replacement
// count), so the dispatch is where the dice actually roll, through the
// engine rng exactly like effRollDice's dice.
//
// Each match applies in deterministic scan order with a fresh recheck
// (CR 616.1e, the applyNonMoveReplacements discipline), its ReplaceWith$
// chain rewriting the held event's Number (Amount) and Ignore (Counter)
// through ReplaceEvent. The dice then roll: one Note per die ("rolls the
// planar die: chaos", the transcript's die roll), the ignored count is
// recorded on the event's Counter and the KEPT results — the FIRST
// count-ignore rolls, a deterministic stand-in for the roller's choice
// (CR 901.4's ignore choice is vacuous here: no plane deck exists, so no
// roll result differs in effect from any other) — ride IDs in roll order.
// The completed event returns handled=false so the ordinary emit path
// logs it with its full trigger treatment; the per-die Notes and the
// ignore Note are the log's other witnesses. A bodyless match (a
// CantHappen planar replacement) has no corpus carrier and is skipped —
// documented inertness, not modelled cancellation.

// continueExploreReplacements is the events.Explore replacement dispatch.
// Two event shapes reach it:
//
//   - the SYNTHETIC PROPOSAL effects/explore.go's ExploreReplaced hook builds
//     (no revealed card yet — IDs empty): this is CR 701.35a's "would
//     explore" moment, exactly the window CR 614.4 puts replacement
//     effects in, and a matching replacement's ReplaceWith$ body replaces
//     the whole explore process (reveal, counter, move) with its own
//     resolution — run synchronously inside the hook's call, under the
//     applyingReplacement guard (so the body's own fresh explores cannot
//     re-match the same replacement, the CreateToken once-per-event
//     discipline). The proposal is never logged, and the caller learns
//     "replaced" from the hook's true return.
//   - the COMPLETED RECORD (IDs carry the revealed card): the explore
//     already happened, so nothing is replaceable — the record returns
//     unhandled so the ordinary emit path logs it and trig:Explores
//     matches it with its full trigger treatment.
//
// Multiple competing Explore replacements apply in deterministic scan order
// (the first match wins), the same no-CR-616.1-order-choice stand-in the
// CreateToken path documents; the corpus carries no competing pair.
func (e *Engine) continueExploreReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	if len(ev.IDs) > 0 {
		return ev, false
	}
	if len(matches) == 0 {
		return ev, false
	}
	m := matches[0]
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	return ev, true
}

// ExploreReplaced is the effects.Host hook effects/explore.go consults before
// it would process one explorer's explore (CR 614.4: the replacement window
// is before the process). It builds the synthetic Explore proposal — Obj the
// explorer, Player its controller, no revealed card (the replacee never
// reveals) — and runs it through the ordinary replacement collection and
// dispatch: a matching R:Event$ Explore replacement's body resolves
// synchronously inside this call and the hook returns true, telling the
// effect its explore was replaced whole. Mirrors emit's own guard: while a
// replacement body is already resolving (applyingReplacement), no replacement
// applies — the body's own explores are fresh, un-replaced events.
func (e *Engine) ExploreReplaced(explorer state.ObjID) bool {
	if e.applyingReplacement {
		return false
	}
	o := e.G.Obj(explorer)
	if o == nil {
		return false
	}
	_, handled := e.applyReplacements(events.Event{Kind: events.Explore, Obj: explorer, Player: o.Controller})
	return handled
}

// Scry is the effects.Host hook effects/cardflow.go's effLookAndArrange
// consults at the scry instruction boundary, BEFORE any card of the player's
// library is looked at (CR 614.4: an R:Event$ Scry replacement applies to the
// scry action itself). It builds the synthetic instruction PROPOSAL, applies
// every matching R:Event$ Scry replacement to it, and returns the surviving
// instruction's count. proceed is false when a replacement replaced the scry
// whole (Eligeth, Crossroads Augur: "draw that many cards instead") -- the
// caller must then look at and arrange nothing.
//
// The proposal is NEVER logged; it exists only to give the replacement
// matcher a held event. The completed scry's own events.Scry record is
// emitted later, by handleArrange, carrying the number of cards actually put
// on the bottom (the count trig:Scry's ToBottom$ gate reads) and outside the
// replacement pass, because a finished action is nothing left to replace.
// That split is why the same events.Scry kind serves two roles: a proposal is
// never emitted, a record is never matched (emitScryRecord).
func (e *Engine) Scry(p state.PlayerID, source state.ObjID, count int32, sa *cards.SA, target int) (int32, bool, bool) {
	if count < 0 {
		count = 0
	}
	if e.applyingReplacement {
		// Inside another replacement's own resolution no further replacement
		// applies (the emit guard's rule); the instruction stands.
		return count, true, false
	}
	// The proposal carries its SA/target only through this synchronous call;
	// the parked choice owns plain value data for the later continuation.
	oldSA, oldTarget := e.scrySA, e.scryTarget
	e.scrySA, e.scryTarget = sa, target
	defer func() { e.scrySA, e.scryTarget = oldSA, oldTarget }()
	ev, handled := e.applyReplacements(events.Event{Kind: events.Scry, Player: p, Obj: source, Amount: count})
	if !handled {
		return count, true, false // no replacement matched
	}
	if e.pending != nil && len(e.replChoices) > 0 && e.replChoices[0].kind == replChoiceScry {
		return 0, false, true // proposal parked; do not inspect the library
	}
	if ev.Kind != events.Scry {
		return 0, false, false // replaced whole: nothing is looked at
	}
	return ev.Amount, true, false
}

// continueScryReplacements applies the collected R:Event$ Scry matches to the
// held instruction proposal. Two corpus shapes:
//
//   - DB$ ReplaceEffect | VarName$ Num (Kenessos, Priest of Thassa): the
//     proposed count is rewritten in place ("scry that many cards plus
//     one"), the instruction survives and the caller arranges the new count;
//   - DB$ Draw | Defined$ You | NumCards$ <that many> (Eligeth, Crossroads
//     Augur): the whole scry is replaced by a draw and the instruction is
//     consumed -- returned as a zero event so the caller (Scry, above) sees
//     proceed=false and never looks at a library.
//
// Every count expression is evaluated against the HELD instruction's own
// count through the ReplaceCount$Num grammar only the replacement context has
// (scryReplacementCount), never a global Count read. An unmodelled body emits
// the loud unimplemented Note and leaves the instruction intact -- the
// conservative direction, never a silent whole-scry drop.
//
// Each applicable effect can apply once. A competition parks the proposal
// for the affected scrying player's order choice, even if the sources have
// different controllers. Recheck candidates after each count rewrite.
func (e *Engine) continueScryReplacements(ev events.Event, matches []replMatch, used []bool, sa *cards.SA, target int) (events.Event, bool) {
	if used == nil {
		used = make([]bool, len(matches))
	}
	for {
		var applicable []int
		for i, m := range matches {
			if !used[i] && e.scryReplacementMatches(m, ev) {
				applicable = append(applicable, i)
			}
		}
		if len(applicable) == 0 {
			return ev, true
		}
		if len(applicable) > 1 && int(ev.Player) < len(e.G.Players) && !e.G.Players[ev.Player].Lost {
			if sa == nil {
				sa, target = e.scrySA, e.scryTarget
			}
			e.replChoices = append([]replChoice{{kind: replChoiceScry, ev: ev, cands: matches,
				applied: used, applicable: applicable, before: e.triggerBefore, player: ev.Player}}, e.replChoices...)
			if e.pending == nil {
				d := e.scryReplacementDecision(e.replChoices[0], sa, target)
				if e.resume == nil {
					e.Ask(d)
				} else {
					e.ask(d)
				}
			}
			return ev, true
		}
		i := applicable[0]
		used[i] = true
		m := matches[i]
		// CR 616.1e: the recheck uses the same matcher class the collection
		// used -- an Effect-created match is never re-gated on ActiveZones$.
		if m.repl.With == nil {
			continue
		}
		with := m.repl.With
		ctx := e.replCtx(m, ev)
		switch with.API {
		case "ReplaceEffect":
			if with.Params["VarName"] != "Num" {
				break
			}
			if n, ok := e.scryReplacementCount(ctx, with.Params["VarValue"], ev.Amount); ok {
				ev.Amount = n
				continue
			}
		case "Draw":
			// "Instead": the draw must be the scrying player's own
			// (Defined$ You, or absent = the controller). Any other Defined$
			// is unmodelled and fails loud below rather than drawing for the
			// wrong seat.
			if d := strings.TrimSpace(with.Params["Defined"]); d != "" && !strings.EqualFold(d, "You") {
				break
			}
			if n, ok := e.scryReplacementCount(ctx, with.Params["NumCards"], ev.Amount); ok {
				e.lifeReplacementDraw(ev.Player, n)
				return events.Event{}, true
			}
		}
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented Scry replacement"})
	}
}

func (e *Engine) scryReplacementMatches(m replMatch, ev events.Event) bool {
	if m.key != "" {
		return e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
	}
	return e.replacementMatches(*m.repl, m.id, ev)
}

// scryReplacementCount resolves a Scry replacement body's count expression
// against the held instruction's own count. Forge writes "that many" in terms
// of the held event as ReplaceCount$Num (Kenessos' SVar X ->
// ReplaceCount$Num/Plus.1; Eligeth's NumCards$ X -> ReplaceCount$Num), a
// grammar only the replacement context carries -- the ordinary Count$
// evaluator does not know it. A plain literal or Count$ body falls through to
// the shared evaluator, so a future `NumCards$ 2` shape works unchanged.
func (e *Engine) scryReplacementCount(ctx *effects.Ctx, raw string, base int32) (int32, bool) {
	expr := strings.TrimSpace(raw)
	if ctx.SVars != nil {
		if body, ok := ctx.SVars[expr]; ok {
			expr = strings.TrimSpace(body)
		}
	}
	if expr == "ReplaceCount$Num" {
		return base, true
	}
	if op, ok := strings.CutPrefix(expr, "ReplaceCount$Num/"); ok {
		return replCountOp(base, op), true
	}
	if strings.HasPrefix(expr, "ReplaceCount$") {
		// A held-event field other than the instruction's own count is not
		// bindable here; fail closed rather than guess.
		return 0, false
	}
	return effects.EvalCountOK(e, ctx, expr)
}

// continueRollDiceReplacements applies the collected R:Event$ RollDice
// matches to the held roll proposal (CR 616.1e: each applicable effect
// applies once, in deterministic scan order, with a fresh recheck against
// the rewritten proposal after every application -- the discipline the
// planar-dice class keeps). The corpus's supported shape is the
// DB$ ReplaceEffect body whose chain rewrites the held proposal's Number
// (the dice count) and Ignore (the ignored-low count) in place --
// ReplaceCount$Number/Plus.1 / ReplaceCount$Ignore/Plus.1 (Wyll, Blade of
// Frontiers; Barbarian Class; Pixie Guide); the rewrite goes through the
// same ReplaceEvent seam the planar arm uses, so the two roll classes
// cannot drift. A match whose body chain steps off ReplaceEffect, or whose
// ReplaceEffect rewrite touches no modelled field (SwapRoll's
// DicePTExchanges), cannot be modelled: the match is skipped LOUDLY (the
// roll proceeds unmodified, the conservative direction) and the shape is
// named here for the next ticket, never silently widened.
func (e *Engine) continueRollDiceReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		// CR 616.1e: the recheck uses the same matcher class the collection
		// used -- an Effect-created match is never re-gated on ActiveZones$.
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if m.repl.With == nil || !rollDiceBodyChainSupported(m.repl.With) {
			e.emit(events.Event{Kind: events.Note, Obj: m.id,
				Text: "unimplemented RollDice replacement"})
			continue
		}
		before := ev
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, &ev)
		if ev.Amount == before.Amount && ev.Counter == before.Counter {
			// The body resolved but rewrote nothing this build models (the
			// SwapRoll exchange): loud inertness, the roll proceeds
			// unmodified.
			e.emit(events.Event{Kind: events.Note, Obj: m.id,
				Text: "unimplemented RollDice replacement"})
		}
	}
	return ev, true
}

// rollDiceBodyChainSupported reports whether a replacement body's whole
// SubAbility$ chain is ReplaceEffect rewrites -- the only shape that both
// never asks (so the pre-roll hook can never suspend the roll it belongs to)
// and only ever rewrites the modelled proposal fields. A chain that steps
// off ReplaceEffect fails closed.
func rollDiceBodyChainSupported(with *cards.SA) bool {
	for s := with; s != nil; s = s.Sub {
		if s.API != "ReplaceEffect" {
			return false
		}
	}
	return true
}

// RollDiceProposed is the effects.Host hook effRollDice consults before any
// die of one roll action is rolled (CR 614.4). It builds the synthetic
// events.RollDice PROPOSAL -- Player the roller, Obj the rolling source,
// Amount the proposed dice count, Counter the proposed ignored-low count --
// and runs it through the ordinary replacement collection and dispatch (the
// Scry proposal's discipline): a matching R:Event$ RollDice replacement's
// body rewrites the proposal synchronously inside this call and the
// rewritten Amount/Counter come back to the caller. The proposal is NEVER
// logged. Mirrors emit's own guard: while a replacement body is already
// resolving (applyingReplacement), no replacement applies -- a body's own
// rolls are fresh, un-replaced events.
func (e *Engine) RollDiceProposed(p state.PlayerID, source state.ObjID, amount, ignore int32) (int32, int32) {
	if e.applyingReplacement {
		return amount, ignore
	}
	if ignore < 0 {
		ignore = 0
	}
	ev, _ := e.applyReplacements(events.Event{Kind: events.RollDice, Player: p, Obj: source,
		Amount: amount, Counter: strconv.FormatInt(int64(ignore), 10)})
	n, err := strconv.Atoi(ev.Counter)
	if err != nil || n < 0 {
		n = 0
	}
	if ev.Amount < 1 {
		ev.Amount = 1
	}
	return ev.Amount, int32(n)
}

// emitScryRecord logs a completed scry instruction's events.Scry record
// OUTSIDE the replacement pass: the record is a finished action's marker, so
// no R:Event$ Scry replacement can apply to it (CR 614.4's window is before
// the action). It still folds and queues triggers normally, so a Mode$ Scry
// trigger fires from exactly this record. Called from handleArrange for the
// Scry verb only (a plain RearrangeTopOfLibrary shares the Option.Kind but
// must record nothing), carrying the number of cards actually put on the
// bottom -- the count trig:Scry's ToBottom$ gate reads.
func (e *Engine) emitScryRecord(ev events.Event) {
	saved := e.applyingReplacement
	e.applyingReplacement = true
	e.emit(ev)
	e.applyingReplacement = saved
}

func (e *Engine) continuePlanarRollReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		// CR 616.1e: the recheck uses the same matcher class the collection
		// used — an Effect-created match is never re-gated on ActiveZones$.
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if m.repl.With == nil {
			continue
		}
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, &ev)
	}
	count := ev.Amount
	if count < 0 {
		count = 0
	}
	ignore := int32(0)
	if n, err := strconv.Atoi(ev.Counter); err == nil && n > 0 {
		ignore = int32(n)
	}
	if ignore > count {
		ignore = count
	}
	keep := count - ignore
	results := make([]state.ObjID, 0, keep)
	for i := int32(0); i < count; i++ {
		die := int32(e.Rand(6)) + 1
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj,
			Text: "rolls the planar die: " + planarDieFaceName(die)})
		if i < keep {
			results = append(results, state.ObjID(die))
		}
	}
	if ignore > 0 {
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj,
			Text: "ignores " + strconv.FormatInt(int64(count-keep), 10) + " planar-dice result(s)"})
	}
	ev.IDs = results
	return ev, false
}

func (e *Engine) scryReplacementDecision(rc replChoice, sa *cards.SA, target int) *decision.Decision {
	d := &decision.Decision{Player: rc.ev.Player, Kind: decision.KReplacement, Min: 1, Max: 1,
		Source: rc.ev.Obj, ResumeKind: "scry_replacement", ResumeSA: sa, ResumeTarget: target,
		Prompt: "Several replacement effects would modify this scry: choose which applies next."}
	for _, i := range rc.applicable {
		m := rc.cands[i]
		label := "Apply a replacement"
		if o := e.G.Obj(m.id); o != nil && o.Face() != nil {
			label = "Apply " + o.Face().Name + "'s replacement"
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replacement", Obj: m.id, Label: label})
	}
	return d
}
