package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"strconv"
	"strings"
)

// damageReplacementPrevents reports whether this replacement is a
// PREVENTION body: either the legacy Prevent$ True shape or a DB$
// ReplaceDamage body, which subtracts its Amount from the held damage event
// and prevents exactly that much (the Thunderstaff/Battletide shield
// family). stat:CantPreventDamage must exclude BOTH shapes, so every
// damage-replacement selection and application path classifies prevention
// through this one predicate and cannot drift apart.
func damageReplacementPrevents(r cards.Repl) bool {
	// Case-insensitive (dponce1 r2): the registration (effEffect's
	// replacementLinePrevents) and the collection
	// (applyReplacementsDispatch) read the param with EqualFold, so this
	// classifier must too — a non-canonical `Prevent$ true` bodyless
	// registration would otherwise be admitted to the competition and then
	// silently erased by the With==nil CantHappen drop arm.
	if strings.EqualFold(r.ParamStr(cards.PKPrevent), "True") {
		return true
	}
	return r.With != nil && r.With.API == "ReplaceDamage"
}

func (e *Engine) applicableDamageReplacements(ev events.Event, matches []replMatch) []replMatch {
	out := matches[:0]
	for _, m := range matches {
		// CR 616.1's recheck must use the same matcher class the initial
		// collection used: an Effect-created match's lifetime is active()'s,
		// not its source's zone (task wildgrowth1), so re-gating it on
		// ActiveZones$ here would silently drop every Effect-granted
		// DamageDone replacement the scan just admitted (Taii Wakeen).
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if damageReplacementPrevents(*m.repl) && e.cantPreventDamage(e.damaging, ev.Obj) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (e *Engine) damageAffectedPlayer(ev events.Event) (state.PlayerID, bool) {
	if ev.Obj == 0 {
		return ev.Player, int(ev.Player) < len(e.G.Players)
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Controller) >= len(e.G.Players) {
		return 0, false
	}
	return o.Controller, true
}

// replaceDamageAmount resolves a DB$ ReplaceDamage body's Amount$ in the
// replacement source's context: the corpus's prevention-shield family prices
// it with a literal (Thunderstaff's 1), an SVar (Battletide Alchemist's
// AlchemicX, the card-defined ShieldAmount of Forcefield's "prevent all but
// 1") or an inline expression. The bool distinguishes an unresolvable value
// frame (Power Leak's PaidAmount) -- which the match gate turns into a
// non-match -- from a resolvable amount of zero, which legitimately prevents
// nothing.
func (e *Engine) replaceDamageAmount(ev events.Event, m replMatch) (int32, bool) {
	if m.repl.With == nil || m.repl.With.API != "ReplaceDamage" {
		return 0, false
	}
	return effects.NumResolved(e, e.replCtx(m, ev), m.repl.With, "Amount", 0)
}

// applyReplaceDamageBody applies a DB$ ReplaceDamage body to the held damage
// event: it subtracts the body's Amount from the event's remaining amount and
// reports whether the event is TERMINAL (fully prevented -- the prevention
// Note this records is the log's witness, and no reduced Damage event is
// emitted) or still stands with its reduced amount for the next modifier in
// the chain (CR 616.1e: each later opportunity reads the changed event). A
// resolvable amount of zero prevents nothing and leaves the event standing.
// The body's SubAbility$ chain is deliberately NOT run here: every corpus
// body that carries one (Divine Deflection's counter-deal, Forcefield's
// self-exile) is an Effect-shield shape whose sub reads bindings this
// per-event application does not have, and running them would fire a
// wrong-outcome rider -- the prevention itself is the correct core.
func (e *Engine) applyReplaceDamageBody(ev *events.Event, m replMatch) bool {
	n, ok := e.replaceDamageAmount(*ev, m)
	if !ok || n <= 0 {
		return false
	}
	prevented := n
	if prevented > ev.Amount {
		prevented = ev.Amount
	}
	ev.Amount -= prevented
	who := "a replacement effect"
	if o := e.G.Obj(m.id); o != nil && o.Face() != nil && o.Face().Name != "" {
		who = o.Face().Name
	}
	e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
		Amount: prevented,
		Text:   who + " prevented " + strconv.Itoa(int(prevented)) + " of the damage"})
	// The shield bookkeeping (effects' PreventDamage registration): deplete
	// the matched shield's pool by what this application prevented and run
	// its registered PreventionSubAbility$ rider. A PRINTED ReplaceDamage
	// body (Thunderstaff) carries no pool and no rider: m.key is empty.
	e.applyReplaceDamageTail(m, prevented, ev)
	// Obj carries the damaged object (0 for a player hit) like the full-
	// prevention arm's Note above, not the preventing source: the log text
	// names the preventer, and Mode$ DamagePreventedOnce triggers key their
	// ValidTarget$ on the damaged side. (Prevention Notes carried no Amount
	// before dponce1 and zero prevention-text events are logged in the
	// golden-shape games, so the field's presence is stream-neutral there.)
	return ev.Amount <= 0
}

// applyReplaceDamageTail is the bookkeeping an Effect-created prevention
// shield owes after one application (a printed ReplaceDamage body carries no
// pool and no rider and never reaches here -- m.key is empty):
//
//   - DEPLETION: "prevent the next N" is a total across events (CR 615), so
//     the matched shield's ChosenNumber pool — the binding its body's
//     Amount$ Count$ChosenNumber reads both at match time and at application
//     time — is decremented by what this application prevented, and the
//     shield is dropped from the registry the moment the pool is spent. The
//     mutation is engine-runtime (like every ContinuousEffect field),
//     deterministic, and rebuilt identically by replay's re-execution; the
//     continuousVersion bump keeps active()'s cache honest for the CR 616.1e
//     rechecks the same emit may still run.
//
//   - THE RIDER: a shield registered with PreventionSubAbility$ (Acolyte's
//     Reward, Vengeful Archon) runs that sub once per application, with the
//     amount this application prevented bound as NumDmg$ PreventedDamage and
//     the parent SA's targets (ShieldEffectTarget$ ParentTarget) bound as
//     the resolution's Remembered list (the sub's Defined$ ShieldEffectTarget
//     is rewritten to the known Remembered selector). The sub runs inside
//     the replacement re-entrancy guard, so its own emissions (the
//     retribution DealDamage) are ordinary events: replacements and triggers
//     see them, and a shield scoped to the rider's own recipient terminates
//     because the pool it just spent does not refill.
func (e *Engine) applyReplaceDamageTail(m replMatch, prevented int32, ev *events.Event) {
	if m.key == "" || prevented <= 0 {
		return
	}
	source, ts, ok := parseEffectKey(m.key)
	if !ok {
		return
	}
	rider := ""
	var objs []state.ObjID
	var players []state.PlayerID
	idx := -1
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Source != source || ce.Timestamp != ts || ce.ReplacementEvent != "DamageDone" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["PreventionShield"]), "True") {
			continue
		}
		idx = i
		ce.ChosenNumber -= prevented
		rider = strings.TrimSpace(ce.ReplacementParams["PreventionSubAbility"])
		objs = append([]state.ObjID(nil), ce.ShieldTargets...)
		players = append([]state.PlayerID(nil), ce.ShieldTargetPlayers...)
		break
	}
	if idx < 0 {
		return
	}
	if e.continuous[idx].ChosenNumber <= 0 {
		e.continuous = append(e.continuous[:idx], e.continuous[idx+1:]...)
	}
	e.continuousVersion++
	if rider == "" {
		return
	}
	e.runPreventionShieldRider(m, rider, objs, players, prevented, ev)
}

// runPreventionShieldRider resolves one PreventionSubAbility$ application.
// Only the corpus's DB$ DealDamage rider is resolved (both carriers:
// Acolyte's Retribution, Archon's Vengeance); any other API is loud and the
// shield's prevention itself stands.
func (e *Engine) runPreventionShieldRider(m replMatch, name string,
	objs []state.ObjID, players []state.PlayerID, prevented int32, ev *events.Event) {
	f := m.face
	if f == nil {
		if o := e.G.Obj(m.id); o != nil {
			f = o.Face()
		}
	}
	if f == nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented PreventionSubAbility$ " + name + " (source face gone)"})
		return
	}
	sub := cards.ResolveSVar(f.SVars, name)
	if sub == nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented PreventionSubAbility$ " + name + " (SVar unresolved)"})
		return
	}
	if sub.API != "DealDamage" {
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented PreventionSubAbility$ " + name + " (" + sub.API + ")"})
		return
	}
	// ResolveSVar parses fresh on every call, so the rewrite below cannot
	// corrupt a shared parsed graph; the copy keeps that guarantee explicit.
	rsub := *sub
	rsub.Params = make(map[string]string, len(sub.Params))
	for k, v := range sub.Params {
		rsub.Params[k] = v
	}
	if strings.TrimSpace(rsub.ParamStr(cards.PKNumDmg)) == "PreventedDamage" {
		rsub.Params["NumDmg"] = strconv.Itoa(int(prevented))
	}
	if strings.TrimSpace(rsub.ParamStr(cards.PKDefined)) == "ShieldEffectTarget" {
		rsub.Params["Defined"] = "Remembered"
	}
	ctx := e.replCtx(m, *ev)
	ctx.Remembered = nil
	for _, id := range objs {
		ctx.Remembered = append(ctx.Remembered, state.Target{Obj: id})
	}
	for _, p := range players {
		ctx.Remembered = append(ctx.Remembered, state.Target{Player: p, IsPlayer: true})
	}
	// The rider's own emissions are ordinary events; nil ev (no action
	// marker, no held-event rewrite surface) keeps them from reading the
	// damage event this shield was applying to.
	e.runReplaceWith(ctx, ev.Obj, &rsub, nil)
}

// damageReplacementMatches applies the damage-specific R: filters before the
// common active-zone gate: source and target are the actual damage source and
// recipient, and IsCombat$/DamageAmount$ describe this in-flight event. The
// remembered/rememberedPlayers lists are an EFFECT-created match's own capture
// (nil/nil for every printed line); a shield scopes by them directly, every
// other filter evaluates as before.
func (e *Engine) damageReplacementMatches(r cards.Repl, source state.ObjID, ev events.Event,
	remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool {
	ctrl := e.controllerOf(source)
	// A prevention shield (effects' PreventDamage registration, marker
	// PreventionShield) scopes by ITS OWN captured recipients: membership in
	// the registration's Remembered (objects) / RememberedPlayers (players)
	// lists, never a ValidTarget$ filter spec. The filter grammar is
	// deliberately bypassed — its IsRemembered predicate UNIONs the source's
	// event-backed remembered list with the registration's capture, which
	// would let unrelated remembered state widen the promise. A shield with
	// neither list (unreachable from the registering primitive) fails closed.
	if strings.EqualFold(strings.TrimSpace(r.Params["PreventionShield"]), "True") {
		if ev.Obj != 0 {
			for _, id := range remembered {
				if id == ev.Obj {
					return true
				}
			}
			return false
		}
		for _, p := range rememberedPlayers {
			if p == ev.Player {
				return true
			}
		}
		return false
	}
	if v := r.ParamStr(cards.PKValidCause); v != "" && !e.replacementCauseMatches(v, source, e.damaging) {
		return false
	}
	// A DB$ ReplaceDamage body must RESOLVE its Amount$ before this
	// replacement may match: the body is Forge's "prevent N of that damage"
	// idiom, and a match this build cannot price would previously be applied
	// as a silent FULL prevention (the body's emissions were supposed to
	// replace the event, so the engine discarded it -- and the body emitted
	// nothing). Amount$ values resolvable through the shared numeric grammar
	// (a literal; an SVar such as Battletide's AlchemicX or a card-defined
	// ShieldAmount; an inline Count$/ReplaceCount$ expression) match; an
	// unmodelled value frame (Power Leak's PaidAmount, an undefined name)
	// fails closed and leaves the damage untouched, per CR 616.1's "only
	// applicable replacements apply".
	if r.With != nil && r.With.API == "ReplaceDamage" {
		if _, ok := e.replaceDamageAmount(ev, replMatch{id: source, repl: &r}); !ok {
			return false
		}
	}
	// An Optional$ True damage replacement whose OptionalDecider$ names a
	// frame this build does not resolve also fails closed: asking the damaged
	// player would answer a "may" that belongs to somebody else (the
	// Battletide Alchemist round-2 finding). An ABSENT parameter keeps the
	// historical default, where the affected player answers.
	if strings.EqualFold(r.ParamStr(cards.PKOptional), "True") {
		if v := strings.TrimSpace(r.ParamStr(cards.PKOptionalDecider)); v != "" && v != "You" {
			return false
		}
	}
	if v := r.ParamStr(cards.PKValidSource); v != "" {
		// The source filter is evaluated through the shared remembered/chosen
		// context, not a bare MatchesSpecFrom: a ChooseSource replacement names
		// the chosen damage source with a ChosenCard/ChosenCardStrict predicate
		// (Deflecting Palm's `Card.ChosenCardStrict,Emblem.ChosenCard`), which
		// reads the chosen list the Choose event recorded on the replacement's
		// OWN source object. Source-specific rather than the resolution's
		// Ctx.Chosen: the damage replacement fires while some later object
		// resolves, and the promise belongs to the object that chose.
		if e.damaging == 0 ||
			// nil remembered: only the chosen half is added here, so an
			// Effect-created `ValidSource$ Card.IsRemembered` line keeps the
			// exact match it had before ChooseSource landed.
			!e.matchesSpec(v, e.damaging, e.rememberedSpecContext(ctrl, source, nil)) {
			return false
		}
	}
	if v := r.ParamStr(cards.PKValidTarget); v != "" {
		if ev.Obj != 0 {
			if !e.matchesSpecFrom(v, ev.Obj, ctrl, source) {
				return false
			}
		} else if !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
			return false
		}
	}
	if combat := strings.TrimSpace(r.Params["IsCombat"]); combat != "" &&
		((strings.EqualFold(combat, "True") && !e.combatDamaging) ||
			(strings.EqualFold(combat, "False") && e.combatDamaging)) {
		return false
	}
	return e.replacementAmountMatches(r.Params["DamageAmount"], ev.Amount, e.replCtx(replMatch{id: source, repl: &r}, ev))
}

// askReplacementChoice poses the CR 616.1 order choice for the FRONT parked
// competition to the affected controller: Min == Max == 1 over one option
// per competing replacement, in the deterministic scan order the engine found
// them in (the order the player reorders, never a coincidence of map
// iteration). Only the front of the queue is ever asked -- see
// handleReplacement's resumption for how the queue hands from one choice to
// the next.
func (e *Engine) poseDamageReplacementChoice(ev events.Event, matches []replMatch, p state.PlayerID) {
	source := e.protectionSource(e.damaging)
	e.replChoices = append(e.replChoices, replChoice{
		kind: replChoiceDamage, ev: ev, cands: matches, before: e.retainTriggerBefore(), player: p,
		damaging: source, combat: e.combatDamaging,
		lifelink: e.hasKeywordH(source, kwhLifelink), deadly: e.hasKeywordH(source, kwhDeathtouch),
		toxic: e.ToxicValue(source),
	})
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

// handleDamageReplacementChoice returns false only when recomputation leaves
// another genuine order choice pending; true means the parked damage event is
// fully prevented/replaced or has landed with all riders.
func (e *Engine) handleDamageReplacementChoice(rc replChoice, selected int) bool {
	savedDamaging, savedCombat := e.damaging, e.combatDamaging
	e.damaging, e.combatDamaging = rc.damaging, rc.combat
	defer func() { e.damaging, e.combatDamaging = savedDamaging, savedCombat }()
	var m replMatch
	if selected == len(rc.cands) {
		// This is the explicit "do not apply" answer for an Optional$ True
		// replacement. Mark every currently applicable optional replacement as
		// used so recomputation cannot immediately pose the same question again;
		// non-optional replacements remain eligible and still apply.
		for _, m := range rc.cands {
			if strings.EqualFold(m.repl.ParamStr(cards.PKOptional), "True") {
				rc.used = append(rc.used, m)
			}
		}
	} else {
		m := rc.cands[selected]
		rc.used = append(rc.used, m)
		if e.applyChosenDamageReplacement(&rc.ev, m) {
			return true
		}
	}
	for {
		rc.cands = e.remainingDamageReplacements(rc.ev, rc.used)
		switch len(rc.cands) {
		case 0:
			e.finishChosenDamage(rc)
			return true
		case 1:
			m = rc.cands[0]
			rc.used = append(rc.used, m)
			if e.applyChosenDamageReplacement(&rc.ev, m) {
				return true
			}
		default:
			// The first modification can leave several effects applicable. Ask
			// again over exactly that recomputed set (CR 616.1e), preserving
			// the already-modified amount and the original damage rider
			// metadata. CR 616.1e also recomputes the AFFECTED player: after a
			// redirection the choice belongs to the new recipient's controller,
			// never the original one, so re-derive it from the modified event.
			if p, ok := e.damageAffectedPlayer(rc.ev); ok && !e.G.Players[p].Lost {
				rc.player = p
				e.replChoices = append([]replChoice{rc}, e.replChoices...)
				if e.pending == nil {
					e.askReplacementChoice(p)
				}
				return false
			}
			// CR 800.4a: the recomputed affected player is lost or gone and
			// makes no choices, so the remaining candidates apply in
			// deterministic scan order -- the same fallback the initial pose
			// takes -- and the parked event settles here.
			for {
				if len(rc.cands) == 0 {
					e.finishChosenDamage(rc)
					return true
				}
				m = rc.cands[0]
				rc.used = append(rc.used, m)
				if e.applyChosenDamageReplacement(&rc.ev, m) {
					return true
				}
				rc.cands = e.remainingDamageReplacements(rc.ev, rc.used)
			}
		}
	}
}

func (e *Engine) remainingDamageReplacements(ev events.Event, used []replMatch) []replMatch {
	var out []replMatch
	alreadyUsed := func(m replMatch) bool {
		for _, u := range used {
			if u.id == m.id && (u.repl == m.repl || (m.key != "" && u.key == m.key)) {
				return true
			}
		}
		return false
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.ReplacementEvent == "" {
			continue
		}
		if with := replacementBodySA(ce.ReplacementBody); with != nil {
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams, With: with}
			m := replMatch{id: ce.Source, repl: r,
				remembered: ce.Remembered, rememberedPlayers: ce.RememberedPlayers,
				chosen: ce.ChosenNumber,
				key:    "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))}
			if !alreadyUsed(m) && e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) &&
				!(damageReplacementPrevents(*r) && e.cantPreventDamage(e.damaging, ev.Obj)) {
				out = append(out, m)
			}
		} else if ce.ReplacementBody == "" && ce.ReplacementEvent == "DamageDone" &&
			strings.EqualFold(ce.ReplacementParams["Prevent"], "True") {
			// Mirror applyReplacementsDispatch's bodyless-Prevent admission
			// (dponce1 r2): after a first NONTERMINAL application (a partial
			// DB$ ReplaceDamage body reduced the held event), the recomputed
			// CR 616.1e candidate set must still hold the bodyless "prevent
			// all" effect — otherwise the remaining damage the registration
			// exists to prevent lands silently.
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
			m := replMatch{id: ce.Source, repl: r,
				remembered: ce.Remembered, rememberedPlayers: ce.RememberedPlayers,
				key: "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))}
			if !alreadyUsed(m) && e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) &&
				!(damageReplacementPrevents(*r) && e.cantPreventDamage(e.damaging, ev.Obj)) {
				out = append(out, m)
			}
		}
	}
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		for i := range o.Face().Repls {
			r := &o.Face().Repls[i]
			if alreadyUsed(replMatch{id: id, repl: r}) || !e.replacementMatches(*r, id, ev) {
				continue
			}
			if damageReplacementPrevents(*r) && e.cantPreventDamage(e.damaging, ev.Obj) {
				continue
			}
			out = append(out, replMatch{id: id, repl: r})
		}
	})
	for _, m := range e.grantedPreventMatches(ev) {
		if !alreadyUsed(m) && !e.cantPreventDamage(e.damaging, ev.Obj) {
			out = append(out, m)
		}
	}
	return out
}

// applyChosenDamageReplacement modifies ev in place. It reports terminal when
// the chosen effect prevented/replaced the damage entirely; ReplaceEffect is
// nonterminal and lets applicability be recomputed against its new amount.
func (e *Engine) applyChosenDamageReplacement(ev *events.Event, m replMatch) bool {
	// Defense in depth: both candidate-producing paths already filter
	// prevention bodies out under CantPreventDamage, but the application
	// point re-checks so a future selection path cannot reintroduce the
	// leak. A skipped body is nonterminal, so the caller recomputes the
	// remaining candidates against the still-standing event (m is in
	// rc.used, so it cannot be picked twice).
	if damageReplacementPrevents(*m.repl) && e.cantPreventDamage(e.damaging, ev.Obj) {
		return false
	}
	if strings.EqualFold(m.repl.ParamStr(cards.PKPrevent), "True") {
		// The ordered path's full prevention is terminal — the held event
		// never lands — so this re-entrant Note is the prevention's only log
		// record, the same shape applyNonMoveReplacements' Prevent$ arm
		// stores (dponce1 r2: silently returning terminal recorded nothing,
		// so no DamagePreventedOnce trigger could fire off a chosen
		// prevention). Amount is the held event's REMAINING amount: a
		// partial DB$ ReplaceDamage body may already have reduced it (CR
		// 616.1e), and TriggerCount$DamageAmount reads the amount THIS
		// prevention prevented.
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Amount: ev.Amount, Text: "damage prevented by replacement effect"})
		return true
	}
	if m.repl.With == nil {
		return true
	}
	if m.repl.With.API == "ReplaceDamage" {
		// The body subtracts its Amount from the held event; a fully
		// prevented event is terminal, a reduced one stands for the
		// recomputation below (CR 616.1e).
		return e.applyReplaceDamageBody(ev, m)
	}
	e.runReplaceWith(e.replCtx(m, *ev), ev.Obj, m.repl.With, ev)
	return m.repl.With.API != "ReplaceEffect"
}

func (e *Engine) finishChosenDamage(rc replChoice) {
	savedDamaging, savedCombat, savedApplying := e.damaging, e.combatDamaging, e.applyingReplacement
	e.damaging, e.combatDamaging, e.applyingReplacement = rc.damaging, rc.combat, true
	applied := e.emit(rc.ev)
	e.damaging, e.combatDamaging, e.applyingReplacement = savedDamaging, savedCombat, savedApplying
	if applied.Kind != events.Damage || applied.Amount <= 0 {
		return
	}
	if rc.deadly && applied.Obj != 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: applied.Obj,
			Counter: "Deathtouched", Amount: 1})
	}
	if rc.lifelink {
		e.emit(events.Event{Kind: events.LifeChange, Player: e.controllerOf(rc.damaging), Amount: applied.Amount})
	}
	if rc.combat && applied.Obj == 0 {
		if e.format == FormatCommander {
			e.tallyCmdDamage(applied.Player, rc.damaging, applied.Amount)
		}
		// The combat-damage ledger's SECOND append site, mirroring the
		// commander tally's established twin path: runCombatAssignments parks
		// any player-targeted combat damage whose CR 616.1 competition is
		// posed (len(matches) > 1, or ANY Optional$ True damage replacement —
		// Battletide Alchemist's "you may prevent X" alone) and never reaches
		// its own append, so the parked event's resolution must record the hit
		// here or a player who WAS dealt combat damage never enters the ledger
		// and Lost Monarch of Ifnir's intervening-if reads 0. All terminal
		// paths of handleDamageReplacementChoice route through here; a fully
		// prevented/replaced event returned above (applied.Kind != Damage or
		// Amount <= 0), and a redirect ONTO a permanent zeroes nothing but
		// fails the Obj == 0 guard exactly as the capture site's guard does.
		e.combatHitsThisTurn = append(e.combatHitsThisTurn, e.combatHit(applied.Player, rc.damaging, applied.Amount))
		// CR 702.164's SECOND poison site, mirroring the ledger append's twin
		// path: a parked player-targeted combat hit never reaches
		// runCombatAssignments' synchronous toxic emit, so the landed event
		// must place the cached toxic poison here or a dealt player keeps 0
		// poison (Battletide Alchemist's optional prevention, declined or
		// applying a 0-amount prevent, both land here). The rc.combat flag and
		// the Obj == 0 guard are the same ones the capture site guards with: a
		// non-combat Damage event or a redirect ONTO a permanent means no
		// player was dealt combat damage, so no poison is placed. Toxic rides
		// the LANDED amount (the early return above already skipped a fully
		// prevented/replaced event, where CR 702.164b's trigger never met).
		if rc.toxic > 0 {
			e.emit(events.Event{Kind: events.PlayerCounterChange,
				Player: applied.Player, Counter: "POISON", Amount: int32(rc.toxic)})
		}
	}
}
