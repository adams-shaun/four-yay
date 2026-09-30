package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"strconv"
	"strings"
)

// applyAddCounterReplacements rewrites a CounterChange/PlayerCounterChange
// event's Amount through every applicable R:Event$ AddCounter replacement,
// then returns the event UNHANDLED so emit's ordinary path logs and folds the
// rewritten amount -- the in-place-rewrite shape the DamageDone ReplaceDamage
// bodies use, one event kind over. Each match applies at most once, and each
// body's Amount$ reads the amount the earlier matches produced (the running
// total, CR 616.1e), so Hardened Scales then Branching Evolution composes
// 1 -> +1 -> double = 4 exactly as the two cards' combined oracle reads. No
// predicate re-check is needed between modifiers: this class's gates
// (ValidCounterType$/ValidCard$/ValidObject$/ValidPlayer$) never depend on
// the amount, unlike CreateToken's per-mint ValidToken$ re-check.
//
// CR 616.1's order choice: two or more applicable candidates whose bodies do
// not all commute and an affected player who can still decide park the event
// on the queue and ask (continueAddCounterReplacements applies the answer
// and re-drives; the pose is a queue append, so a competition that arrived
// while another decision was outstanding parks behind it and is asked when
// the queue drains, never overwriting it). A competition whose affected
// player has left the game makes no choices (CR 800.4a) and takes the
// deterministic scan-order composition.
//
// A body whose Amount$ this build cannot price, or whose resolved value is
// negative, leaves the event verbatim -- never a silent erase. A resolved
// zero IS applied, though: "instead put zero" is a legitimate replacement
// result (Vizier of Remedies' Minus.1 on a single -1/-1 counter resolves to
// zero, and the oracle's "that many minus one" then places none). An
// unpriceable body is skipped, never read as zero.
func (e *Engine) applyAddCounterReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	var cands []replMatch
	for _, m := range matches {
		body := m.repl.With
		if body == nil || body.API != "ReplaceCounter" {
			continue
		}
		if _, ok := e.priceAddCounterBody(ev, m, ev.Amount); ok {
			cands = append(cands, m)
		}
	}
	if len(cands) == 0 {
		return ev, false
	}
	if p, ok := e.addCounterAffectedPlayer(ev); ok && !e.G.Players[p].Lost &&
		len(cands) > 1 && !e.addCounterReplacementsCommute(cands) {
		e.poseAddCounterOrderChoice(ev, cands, p)
		return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "counter change awaiting replacement-order choice"}, true
	}
	amount := ev.Amount
	changed := false
	for _, m := range cands {
		n, ok := e.applyAddCounterBody(ev, m, amount)
		if !ok {
			continue
		}
		if n != amount {
			amount = n
			changed = true
		}
	}
	if !changed {
		return ev, false
	}
	ev.Amount = amount
	return ev, false
}

// addCounterAffectedPlayer is the CR 616.1 affected player of a counter
// event: the counter's recipient for the player form (ev.Player), the
// affected object's controller for the object form.
func (e *Engine) addCounterAffectedPlayer(ev events.Event) (state.PlayerID, bool) {
	if ev.Kind == events.PlayerCounterChange {
		return ev.Player, int(ev.Player) < len(e.G.Players)
	}
	return e.moveAffectedPlayer(ev)
}

// poseAddCounterOrderChoice parks a counter event whose competing AddCounter
// replacements do not all commute and asks the affected player (CR 616.1)
// which applies first. The synchronous-damage context the event was proposed
// under rides the park, the way the life competition's does, so the resumed
// emit sees the same provenance. The counter ADDER (the player putting the
// counters, the ValidSource$/EffectOnly$ role) is captured here too: the
// answer may arrive after the proposing window has closed -- a body's
// CounterChange poses from inside the replacement body, whose
// applyingReplacement/replacingSource provenance is gone by resume time --
// so the adder cannot be re-derived at the final emit.
func (e *Engine) poseAddCounterOrderChoice(ev events.Event, cands []replMatch, p state.PlayerID) {
	adderPlusOne := state.PlayerID(0)
	if a, ok := e.inFlightCounterAdder(); ok {
		adderPlusOne = a + 1
	}
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceAddCounter,
		ev: ev, cands: cands, before: e.triggerBefore, player: p,
		damaging: e.damaging, combatDamaging: e.combatDamaging, dmgSrcOverride: e.dmgSrcOverride,
		inResolution: e.resolvingObj != 0 || e.answerInResolution, counterAdderPlusOne: adderPlusOne})
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

// addCounterReplacementsCommute reports whether the candidate ReplaceCounter
// bodies compose order-insensitively: the same op family throughout, where
// the family is one of the arithmetically safe ones (identity, Plus, Minus,
// Twice, Thrice, HalfDown, HalfUp each commute with itself -- two Plus.1
// bodies land the same total either order). A MIXED set (Hardened Scales'
// Plus.1 and Branching Evolution's Twice: 1 -> 2 -> 4 one way, 1 -> 2 -> 3
// the other) does not commute; neither does a body whose Amount$ resolves to
// something the CounterNum grammar cannot name (a literal, another count
// head) or one that carries a SubAbility$ chain (side-effect riders do not
// commute with anything).
func (e *Engine) addCounterReplacementsCommute(cands []replMatch) bool {
	family := ""
	for _, m := range cands {
		body := m.repl.With
		if body == nil || body.API != "ReplaceCounter" {
			return false
		}
		if body.Sub != nil {
			return false
		}
		op, ok := e.counterReplaceOp(m.id, body)
		if !ok {
			return false
		}
		if op == "" {
			// A bare ReplaceCount$CounterNum identity body applies nothing and
			// commutes with any order.
			continue
		}
		k := strings.TrimPrefix(op, "/")
		if i := strings.IndexByte(k, '.'); i >= 0 {
			k = k[:i]
		}
		if family != "" && k != family {
			return false
		}
		family = k
	}
	return family != ""
}

// counterReplaceOp resolves a DB$ ReplaceCounter body's Amount$ to its
// ReplaceCount$CounterNum op suffix ("/Plus.1", "/Twice", ...). The Amount$
// names an SVar (Hardened Scales' X:ReplaceCount$CounterNum/Plus.1); a value
// that is not a CounterNum ReplaceCount body (a literal, another count head,
// or an SVar name with no face entry) returns false -- the conservative
// not-known-to-commute verdict.
func (e *Engine) counterReplaceOp(source state.ObjID, body *cards.SA) (string, bool) {
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return "", false
	}
	expr := strings.TrimSpace(body.Params["Amount"])
	if v, ok := o.Face().SVars[expr]; ok {
		expr = v
	}
	const prefix = "ReplaceCount$CounterNum"
	if !strings.HasPrefix(expr, prefix) {
		return "", false
	}
	return strings.TrimPrefix(expr, prefix), true
}

// priceAddCounterBody is the side-effect-free applicability verdict shared by
// the order offer and the application. A candidate whose amount cannot be
// resolved (or would remove counters) must never be offered as an effect that
// can apply first. Price again after each rewrite against the running amount.
func (e *Engine) priceAddCounterBody(ev events.Event, m replMatch, amount int32) (int32, bool) {
	body := m.repl.With
	if body == nil || body.API != "ReplaceCounter" {
		return amount, false
	}
	if ct := strings.TrimSpace(body.Params["ValidCounterType"]); ct != "" && ct != ev.Counter {
		return amount, false
	}
	hold := ev
	hold.Amount = amount
	ctx := e.replCtx(m, hold)
	n, ok := e.replaceCounterAmount(body, ctx, amount)
	// A negative result would be a counter REMOVAL, which this class
	// does not express; leave the event verbatim. An unpriceable body
	// (!ok) is likewise skipped, never read as zero.
	if !ok || n < 0 {
		return amount, false
	}
	return n, true
}

// applyAddCounterBody applies a priced body and then resolves its riders.
func (e *Engine) applyAddCounterBody(ev events.Event, m replMatch, amount int32) (int32, bool) {
	n, ok := e.priceAddCounterBody(ev, m, amount)
	if !ok {
		return amount, false
	}
	body := m.repl.With
	ctx := e.replCtx(m, ev)
	ctx.ReplacementAmount = amount
	// The body APPLIES from here on. A sub-ability chain on a ReplaceCounter
	// body is part of the replacement (Forge resolves it as the replaced
	// event happens): Melira, the Living Cure's lock ("and you can't get
	// additional poison counters this turn") rides SVar:OnlyOnePoison's
	// SubAbility$ DBImmediateTrigger, an
	// ImmediateTrigger | Execute$ TrigEffect | StaticAbilities$ CantPutCounter
	// that registers the real CantPutCounter restriction. Running the chain
	// here -- through the same runReplaceWith / resolveReplacementWith machine
	// every other ReplaceWith$ rider rides -- is what makes the lock real;
	// its DBImmediateTrigger resolves the Effect inline, so the lock is
	// installed before this function returns and before the replacement
	// event's own fold. A body that only rewrites without a chain (Hardened
	// Scales, Branching Evolution, Vizier of Remedies) is unchanged.
	if body.Sub != nil {
		e.runReplaceWith(ctx, m.id, body.Sub, nil)
	}
	return n, true
}

// sameReplMatchIn reports whether the applied set already holds m: identity
// by source id plus the repl pointer (the same value identity the damage
// path's alreadyUsed uses).
func sameReplMatchIn(applied []replMatch, m replMatch) bool {
	for _, u := range applied {
		if u.id == m.id && (u.repl == m.repl || (m.key != "" && u.key == m.key)) {
			return true
		}
	}
	return false
}

// continueAddCounterReplacements drives a parked AddCounter competition after
// one order answer: the chosen body already applied, so the remaining
// candidates re-check (CR 616.1e), a live non-commuting remainder re-poses
// at the queue's front, and the fully rewritten event is emitted once none
// is left (the emitLifeReplacement convention -- no new replacement pass).
// The adder captured when the competition was posed (rc.counterAdderPlusOne)
// rides this final emit so its observers see the placement attributed even
// though the proposing window has closed by now.
func (e *Engine) continueAddCounterReplacements(rc replChoice) {
	e.driveAddCounterCompetition(rc,
		func(ev events.Event, _ []replMatch) { e.emitAddCounterReplacement(ev, rc.counterAdderPlusOne) },
		e.reposeAddCounterCompetition)
}

// reposeAddCounterCompetition is the live re-pose of a non-commuting
// remainder: the competition returns to the FRONT of the queue (the same
// event, partially applied) and the affected player is asked again.
func (e *Engine) reposeAddCounterCompetition(rc replChoice, p state.PlayerID) {
	e.replChoices = append([]replChoice{rc}, e.replChoices...)
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

// driveAddCounterCompetition is the CR 616.1e loop shared by the live
// counter path (continueAddCounterReplacements) and the staged-entry resume
// (rules/entry_counters.go): it re-prices the unapplied candidates against
// the running amount, applies the first remaining one until the competition
// empties or a live non-commuting remainder must be ordered, and reports the
// fully rewritten event through complete (with the bodies applied, in
// answer order). repose parks a remainder that needs another answer; the
// live path re-queues at the front, the staged path mirrors a real pose.
func (e *Engine) driveAddCounterCompetition(rc replChoice,
	complete func(events.Event, []replMatch), repose func(replChoice, state.PlayerID)) {
	ev := rc.ev
	applied := rc.appliedRepls
	for {
		var remaining []replMatch
		for _, m := range rc.cands {
			if !sameReplMatchIn(applied, m) {
				if _, ok := e.priceAddCounterBody(ev, m, ev.Amount); ok {
					remaining = append(remaining, m)
				}
			}
		}
		if len(remaining) == 0 {
			complete(ev, applied)
			return
		}
		if p, ok := e.addCounterAffectedPlayer(ev); ok && !e.G.Players[p].Lost &&
			len(remaining) > 1 && !e.addCounterReplacementsCommute(remaining) {
			rc.ev, rc.appliedRepls = ev, applied
			repose(rc, p)
			return
		}
		m := remaining[0]
		applied = append(applied[:len(applied):len(applied)], m)
		n, ok := e.applyAddCounterBody(ev, m, ev.Amount)
		if ok && n != ev.Amount {
			ev.Amount = n
		}
	}
}

// emitAddCounterReplacement logs a fully rewritten counter event without
// starting a new replacement pass: every candidate has had its one
// opportunity (the emitLifeReplacement convention). adderPlusOne is the
// competition's captured adder (PLUS ONE; 0 = unknown), published for the
// emit so a trigger or the per-turn ledger reads the same "who put these"
// role the original placement carried -- the counterReplacementFold pairing
// below deliberately disables replacement-body inference for this settled
// echo, so an unpublished adder would leave the placement attributed to
// nobody.
func (e *Engine) emitAddCounterReplacement(ev events.Event, adderPlusOne state.PlayerID) {
	savedAdder := state.PlayerID(counterAdderUnset)
	if adderPlusOne != 0 {
		savedAdder = e.SetCounterAdder(adderPlusOne - 1)
	}
	saved, folded := e.applyingReplacement, e.counterReplacementFold
	e.applyingReplacement, e.counterReplacementFold = true, true
	e.emit(ev)
	e.applyingReplacement, e.counterReplacementFold = saved, folded
	if adderPlusOne != 0 {
		e.SetCounterAdder(savedAdder)
	}
}

// replaceCounterAmount resolves a DB$ ReplaceCounter body's new counter count
// against the amount the event would place. Forge's corpus expresses it as
// Amount$ X with X:ReplaceCount$CounterNum/Plus.1 (Hardened Scales, +1) or
// X:ReplaceCount$CounterNum/Twice (Branching Evolution, double); the shared
// numeric grammar resolves both once CounterNum is a recognised ReplaceCount
// field (effects/count.go). The base is the HELD amount, not the original
// event's, so a chain of modifiers reads the running total (CR 616.1e).
// NumResolved's verdict distinguishes an unmodelled frame (fail the match)
// from a legitimate zero.
func (e *Engine) replaceCounterAmount(body *cards.SA, ctx *effects.Ctx, base int32) (int32, bool) {
	ctx.ReplacementAmount = base
	return effects.NumResolved(e, ctx, body, "Amount", base)
}

// CounterAllowed implements effects.Host. A Counter event is the attempted
// removal of a stack object, not a CounterChange event, so it is checked at
// Counter's sole stack-removal path before the MoveZone is emitted.
func (e *Engine) CounterAllowed(target, cause state.ObjID) bool {
	matches := e.counterReplacementMatchesAll(target, cause)
	switch len(matches) {
	case 0:
		return true
	case 1:
		e.applyCounterReplacement(target, matches[0])
		return false
	default:
		t := e.G.Obj(target)
		if t == nil || int(t.Controller) >= len(e.G.Players) || e.G.Players[t.Controller].Lost {
			e.applyCounterReplacement(target, matches[0])
			return false
		}
		e.replChoices = append(e.replChoices, replChoice{
			kind: replChoiceCounter,
			ev:   events.Event{Obj: target}, cands: matches, before: e.triggerBefore,
			player: t.Controller, cause: cause,
		})
		if e.pending == nil {
			e.askReplacementChoice(t.Controller)
		}
		return false
	}
}

func (e *Engine) counterReplacementMatchesAll(target, cause state.ObjID) []replMatch {
	var matches []replMatch
	// Effect-created Counter replacements (Mistrise Village's AntiMagic:
	// "the next spell you cast this turn can't be countered", a delayed
	// Effect whose body is a bodyless Layer$ CantHappen R:): the continuous
	// registry is the only place these live, so the Counter path — whose
	// ordinary scan reads printed face Repls — matches them here through the
	// same remembered-scoped matcher the general replacement scan uses, plus
	// the shared ValidSA$ subset gate. Stopping the Counter event (the
	// With-less form) is the complete replacement.
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.ReplacementEvent != "Counter" || ce.ReplacementBody != "" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Layer"]), "CantHappen") {
			continue
		}
		r := cards.Repl{Event: "Counter", Params: ce.ReplacementParams}
		if !e.replacementMatchesEffectCreated(r, ce.Source, events.Event{Obj: target}, ce.Remembered, ce.RememberedPlayers) {
			continue
		}
		t := e.G.Obj(target)
		src := e.G.Obj(ce.Source)
		if t == nil || src == nil {
			continue
		}
		if spec := r.Params["ValidSA"]; spec != "" && !e.counterValidSA(t, spec, e.controllerOf(ce.Source), ce.Source) {
			continue
		}
		matches = append(matches, replMatch{id: ce.Source, repl: &r,
			remembered: ce.Remembered, chosen: ce.ChosenNumber,
			key: "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))})
	}
	e.forEachObject(func(source state.ObjID) {
		o := e.G.Obj(source)
		if o == nil || o.Face() == nil {
			return
		}
		for i := range o.Face().Repls {
			r := &o.Face().Repls[i]
			if r.Event == "Counter" && e.counterReplacementMatches(*r, source, target, cause) {
				matches = append(matches, replMatch{id: source, repl: r})
			}
		}
	})
	return matches
}

func (e *Engine) applyCounterReplacement(target state.ObjID, m replMatch) {
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, events.Event{Obj: target}), target, m.repl.With, nil)
	}
}

func (e *Engine) applyChosenCounterReplacement(rc replChoice, selected int) {
	m := rc.cands[selected]
	// CR 616.1e: applicability is checked against the event as it exists when
	// the answer is applied. No state can normally change while the choice is
	// pending, but recomputing keeps this path correct for released/departed
	// decisions and mirrors damage replacement ordering.
	if !e.counterReplacementMatches(*m.repl, m.id, rc.ev.Obj, rc.cause) {
		matches := e.counterReplacementMatchesAll(rc.ev.Obj, rc.cause)
		if len(matches) == 0 {
			return
		}
		m = matches[0]
	}
	e.applyCounterReplacement(rc.ev.Obj, m)
}

func (e *Engine) counterReplacementMatches(r cards.Repl, source, target, cause state.ObjID) bool {
	o := e.G.Obj(source)
	t := e.G.Obj(target)
	if o == nil || t == nil || t.Zone != state.ZStack {
		return false
	}
	if !e.commandReplZoneAdmits(r, source) {
		return false
	}
	if active := r.Params["ActiveZones"]; active != "" && !zoneSpecContains(active, o.Zone) {
		return false
	}
	if v := r.Params["ValidCard"]; v != "" &&
		!e.matchesSpecFrom(v, target, o.Controller, source) {
		return false
	}
	if v := r.Params["ValidCause"]; v != "" && !e.replacementCauseMatches(v, source, cause) {
		return false
	}
	if !e.replacementConditionHolds(r, source, o.Controller) {
		return false
	}
	return e.counterValidSA(t, r.Params["ValidSA"], o.Controller, source)
}

// counterValidSA is the Spell/Activated/Triggered subset used by R:Event$
// Counter. A qualifier scopes the stack object's controller relative to the
// replacement source; an unrecognised qualifier fails closed.
func (e *Engine) counterValidSA(target *state.Object, spec string, you state.PlayerID, source state.ObjID) bool {
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		kind, quals, _ := strings.Cut(strings.TrimSpace(alt), ".")
		isKind := (kind == "Spell" && target.Ability == nil) ||
			(kind == "SpellAbility") ||
			(kind == "Activated" && target.Ability != nil && !isTriggered(e.G, target)) ||
			(kind == "Triggered" && target.Ability != nil && isTriggered(e.G, target))
		if !isKind {
			continue
		}
		if quals == "" {
			return true
		}
		// Spell qualifiers are card characteristics plus controller-relative
		// predicates. Reuse the ordinary object-filter grammar rather than a
		// hand-maintained qualifier allowlist, so Creature/Instant/colour/P/T
		// and future recognised predicates cannot drift from targeting.
		if target.Ability == nil && e.counterSpellQualifiers(target, quals, you, source) {
			return true
		}
		// Ability objects have no card face; their corpus qualifiers are the
		// controller-relative forms, evaluated explicitly against the wrapper.
		if target.Ability != nil {
			switch quals {
			case "YouCtrl":
				if target.Controller == you {
					return true
				}
			case "OppCtrl", "YouDontCtrl":
				if target.Controller != you {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) counterSpellQualifiers(target *state.Object, quals string, you state.PlayerID, source state.ObjID) bool {
	var ordinary []string
	for q := range strings.SplitSeq(quals, "+") {
		switch q {
		case "hasKeywordFlash":
			if target.Face() == nil || !target.Face().HasKeyword("Flash") {
				return false
			}
		case "wasCastByYou":
			if target.Controller != you {
				return false
			}
		default:
			ordinary = append(ordinary, q)
		}
	}
	if len(ordinary) == 0 {
		return true
	}
	return e.matchesSpecFrom("Card."+strings.Join(ordinary, "+"), target.ID, you, source)
}
