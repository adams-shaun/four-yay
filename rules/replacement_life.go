package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// lifeExchangeTransaction is one ExchangeLife carried across the replacement
// machinery. Clone copies a parked one through cloneRemap.lifeExchange (every
// field the clone's own, the rider memory re-pointed at the clone's copy).
type lifeExchangeTransaction struct {
	source       state.ObjID    `clone:"deep"`
	controller   state.PlayerID `clone:"deep"`
	oldLife      int32          `clone:"deep"`
	player       state.PlayerID `clone:"deep"`
	lifeBefore   int32          `clone:"deep"`
	setPower     bool           `clone:"deep"`
	setToughness bool           `clone:"deep"`
	// second is the proposed second side, emitted as-is: an event value is
	// never written in place (its IDs/Pairs are shared exactly as the log's
	// are), so the clone shares it.
	second       events.Event `clone:"share"`
	stage        uint8        `clone:"deep"`
	rememberLoss bool         `clone:"deep"`
	// rememberMemory is the resolving chain's ExchangeLife rider memory
	// (Ctx.ExchangeMemory at the exchange), the only part of the Ctx the
	// settle reads: holding the memory rather than the whole Ctx lets Clone
	// re-point it at the clone's own copy (cloneRemap).
	rememberMemory *effects.ExchangeMemory `clone:"deep"`
	controllerLife int32                   `clone:"deep"`
	staged         []events.Event          `clone:"deep"`
	// done marks a settled transaction: under the resolution kernel a CR
	// 616.1 life replacement is answered in place, inside the very emit the
	// exchange is waiting on, so the answer's settle and the exchange's own
	// tail both reach finishLifeExchange; only the first settles.
	done bool `clone:"deep"`
}

// applyLifeReplacements evaluates the GainLife and LifeReduced replacements
// that apply to a proposed life gain or life loss before it reaches the log.
// The transformations are read from the replacement body's ReplaceCount$
// grammar, not card names, so Archives, Reflection, Cleric Class, Bloodletter
// and their corpus siblings share one path.
//
// CR 616.1: when more than one replacement would modify the event, the
// affected player chooses one to apply, then applicability is re-checked
// against the modified event (CR 616.1e) and the choice repeats until none is
// left. Each replacement applies at most once to the event (CR 614.5). The
// order choice reuses the engine's one KReplacement decision path
// (poseLifeReplacementChoice / handleReplacement); it is skipped only when
// every competing replacement is the same commuting operator (all doublers,
// all "plus N", all prevention), where every order produces the same event.
func (e *Engine) applyLifeReplacements(ev events.Event) (events.Event, bool) {
	if ev.Kind == events.LifeChange && ev.Amount > 0 && e.lifeGainForbidden(ev.Player) {
		e.consumeExchangeLifeSide(ev)
		return e.emit(events.Event{Kind: events.Note, Player: ev.Player, Text: "prevented: cannot gain life"}), true
	}
	return e.continueLifeReplacements(ev, nil)
}

// continueLifeReplacements applies the remaining applicable replacements to
// ev, which the replacements in applied have already modified. It returns
// handled=true whenever anything replaced the event or a choice was parked.
func (e *Engine) continueLifeReplacements(ev events.Event, applied []replMatch) (events.Event, bool) {
	for {
		cands := e.lifeReplacementCandidates(ev, applied)
		if len(cands) == 0 {
			if e.lifeExchange != nil && e.lifeExchange.second.Kind != 0 {
				return e.stageExchangeLife(ev), true
			}
			if len(applied) == 0 {
				return ev, false
			}
			return e.emitLifeReplacement(ev)
		}
		if len(cands) > 1 && !e.lifeReplacementsCommute(ev, cands) && e.poseLifeReplacementChoice(ev, cands, applied) {
			return ev, true
		}
		m := cands[0]
		next, consumed := e.applyLifeReplacement(ev, m)
		if consumed {
			e.consumeExchangeLifeSide(ev)
			return ev, true
		}
		ev = next
		applied = append(applied[:len(applied):len(applied)], m)
	}
}

// poseLifeReplacementChoice parks a life event whose competing replacements
// do not commute and asks the affected player (CR 616.1: the player whose life
// total the event changes) which applies first. It declines, and the caller
// applies the first candidate in deterministic scan order, only where no
// choice can be made: the player has left the game (CR 800.4a -- they make
// no choices, and the event must still apply). While another decision is
// outstanding the competition parks on the queue BEHIND it and is asked when
// the queue drains (Submit's tail) -- never overwritten, never applied
// silently in its shadow.
func (e *Engine) poseLifeReplacementChoice(ev events.Event, cands, applied []replMatch) bool {
	p := ev.Player
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost {
		return false
	}
	rc := replChoice{ev: ev, cands: cands, before: e.retainTriggerBefore(), life: true,
		exchange:     e.lifeExchange,
		appliedRepls: applied, damaging: e.damaging, combatDamaging: e.combatDamaging,
		dmgSrcOverride: e.dmgSrcOverride, inResolution: e.resolvingObj != 0 || e.answerInResolution}
	if e.pending == nil {
		// The front of the queue is the competition being asked. A life choice
		// is asked immediately (pending is nil), so it goes first.
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		e.askReplacementChoice(p)
	} else {
		// CR 616.1 with the queue: the competition parks behind the
		// outstanding decision and the parked event stays in hand (applyLife
		// Replacements returns handled=true) until the answer.
		e.replChoices = append(e.replChoices, rc)
	}
	return true
}

// lifeReplacementCandidates collects, in forEachObject's deterministic scan
// order, every replacement not yet applied to ev that would modify it now.
// A gain is only ever modified by GainLife replacements and a loss only by
// LifeReduced ones, so a replacement that turns a gain into a loss (Tainted
// Remedy) leaves every other GainLife replacement inapplicable.
func (e *Engine) lifeReplacementCandidates(ev events.Event, applied []replMatch) []replMatch {
	event, p, loss := "", state.PlayerID(0), int32(0)
	if ev.Kind == events.LifeChange && ev.Amount > 0 {
		event, p = "GainLife", ev.Player
	} else if q, amount, ok := lifeLoss(ev); ok {
		event, p, loss = "LifeReduced", q, amount
	} else {
		return nil
	}
	var out []replMatch
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		for i := range o.Face().Repls {
			r := &o.Face().Repls[i]
			if r.Event != event || !replacementActive(e, id, r) || !replacementPlayerMatches(e, id, r, p) ||
				lifeReplacementApplied(applied, replMatch{id: id, repl: r}) {
				continue
			}
			if e.lifeReplacementApplies(ev, id, r, p, loss) {
				out = append(out, replMatch{id: id, repl: r})
			}
		}
	})
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.ReplacementEvent != event || ce.ReplacementBody != "" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParam(cards.PKPrevent)), "True") {
			continue
		}
		r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
		m := replMatch{id: ce.Source, repl: r, remembered: ce.Remembered,
			rememberedPlayers: ce.RememberedPlayers,
			key:               "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))}
		if lifeReplacementApplied(applied, m) ||
			!e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) {
			continue
		}
		if e.lifeReplacementApplies(ev, ce.Source, r, p, loss) {
			out = append(out, m)
		}
	}
	return out
}

func lifeReplacementApplied(applied []replMatch, candidate replMatch) bool {
	for _, m := range applied {
		if candidate.key != "" || m.key != "" {
			if m.key != "" && m.key == candidate.key {
				return true
			}
			continue
		}
		if m.id == candidate.id && m.repl == candidate.repl {
			return true
		}
	}
	return false
}

// lifeReplacementApplies reports whether replacement r of source would do
// something to ev. A replacement whose body this engine cannot perform is not
// a candidate, so it never occupies an order choice.
func (e *Engine) lifeReplacementApplies(ev events.Event, source state.ObjID, r *cards.Repl, p state.PlayerID, loss int32) bool {
	if r.Event == "GainLife" {
		if strings.EqualFold(r.ParamStr(cards.PKPrevent), "True") {
			return true
		}
		if !e.replacementCondition(source, r) || r.ParamStr(cards.PKValidSource) != "" {
			return false
		}
		if _, ok := e.replaceCount(source, r, "LifeGained", ev.Amount); ok {
			return true
		}
		return r.With != nil && (r.With.API == "LoseLife" || r.With.API == "Draw")
	}
	if strings.EqualFold(r.ParamStr(cards.PKIsDamage), "True") && ev.Kind != events.Damage {
		return false
	}
	if strings.EqualFold(r.ParamStr(cards.PKPlayerTurn), "True") && e.G.Active != e.controllerOf(source) {
		return false
	}
	if !e.replacementCondition(source, r) {
		return false
	}
	if result := r.ParamStr(cards.PKResult); result != "" && !compareLife(e.G.Players[p].Life-loss, result) {
		return false
	}
	if _, ok := e.replaceCount(source, r, "Amount", loss); ok {
		return true
	}
	// A non-ReplaceEffect body (Enduring Angel's transform then SetLife)
	// wholly replaces the loss.
	o := e.G.Obj(source)
	return r.With != nil && o != nil && o.Face() != nil
}

// lifeReplacementsCommute reports whether every order of cands yields the same
// event: all prevent the gain, all double, or all add a constant, and none
// gates its own applicability on the running amount (Result$).
func (e *Engine) lifeReplacementsCommute(ev events.Event, cands []replMatch) bool {
	name := "Amount"
	if ev.Kind == events.LifeChange && ev.Amount > 0 {
		name = "LifeGained"
	}
	kind := ""
	for _, m := range cands {
		if m.repl.ParamStr(cards.PKResult) != "" {
			return false
		}
		k := ""
		switch op, ok := e.replaceCountOp(m.id, m.repl, name); {
		case m.repl.Event == "GainLife" && strings.EqualFold(m.repl.ParamStr(cards.PKPrevent), "True"):
			k = "prevent"
		case ok && op == "/Twice":
			k = "twice"
		case ok && strings.HasPrefix(op, "/Plus."):
			k = "plus"
		default:
			return false
		}
		if kind != "" && k != kind {
			return false
		}
		kind = k
	}
	return true
}

// applyLifeReplacement applies one chosen replacement to ev. It returns the
// modified event, or consumed=true when the replacement's own body wholly
// replaced the event (prevention, a draw or a primitive chain instead).
func (e *Engine) applyLifeReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	r := m.repl
	if r.Event == "GainLife" {
		if strings.EqualFold(r.ParamStr(cards.PKPrevent), "True") {
			e.emit(events.Event{Kind: events.Note, Player: ev.Player, Text: "prevented: cannot gain life"})
			return ev, true
		}
		if amount, ok := e.replaceCount(m.id, r, "LifeGained", ev.Amount); ok {
			ev.Amount = amount
			return ev, false
		}
		switch applyLifeReplacementCodes.Code(string(r.With.API)) {
		case applyLifeReplacementLoseLife:
			// That player loses that much life instead: the event is now a
			// loss, so only LifeReduced replacements can modify it further.
			ev.Amount = -ev.Amount
			return ev, false
		case applyLifeReplacementDraw:
			e.lifeReplacementDraw(ev.Player, ev.Amount)
			return ev, true
		}
		return ev, false
	}
	_, loss, _ := lifeLoss(ev)
	if amount, ok := e.replaceCount(m.id, r, "Amount", loss); ok {
		if ev.Kind == events.Damage {
			ev.Amount = amount
		} else {
			ev.Amount = -amount
		}
		return ev, false
	}
	o := e.G.Obj(m.id)
	if o == nil || o.Face() == nil || r.With == nil {
		// The source no longer exists (a parked choice answered after it
		// left): its replacement has nothing left to perform.
		return ev, false
	}
	e.runReplaceWith(effects.NewCtxPtr(m.id, o.Controller, effects.CtxInit{SVars: o.Face().SVars}), 0, r.With, nil)
	return ev, true
}

// lifeReplacementDraw draws n cards for a GainLife→Draw replacement body
// (Lich's "If you would gain life, draw that many cards instead"),
// suspension-aware: each DrawFor may pose a Dredge ask (CR 702.55) and
// suspend. The loop parks the remaining count on the ask's resume point
// (resolution.go) and returns, instead of looping on -- looping on would
// pose a SECOND ask while the first is outstanding, orphaning it and losing
// the remaining draws (findings-sol4 MAJOR). The answered dredge re-drives
// the rest from handleModes' direct arm (stack empty) or resumeResolution's
// dredge arm (a resolving object on the stack), both of which drain any
// replacement-order queue the interrupted pass left behind.
func (e *Engine) lifeReplacementDraw(p state.PlayerID, n int32) {
	for i := int32(0); i < n; i++ {
		effects.DrawFor(e, p)
	}
}

// emitLifeReplacement logs a fully transformed event without starting a new
// replacement pass: every applicable replacement has had its one opportunity.
// A gain reduced to nothing (LimitMax of zero) is no event at all.
func (e *Engine) emitLifeReplacement(ev events.Event) (events.Event, bool) {
	if e.lifeExchange != nil && e.lifeExchange.second.Kind != 0 {
		return e.stageExchangeLife(ev), true
	}
	if ev.Kind == events.LifeChange && ev.Amount == 0 {
		return ev, true
	}
	saved := e.applyingReplacement
	e.applyingReplacement = true
	stored := e.emit(ev)
	e.applyingReplacement = saved
	return stored, true
}

// lifeGainForbidden checks active CantGainLife statics against the player who
// would gain life. R:Event$ GainLife Prevent$ True lines are replacement
// effects and compete in the CR 616.1 order choice instead. The static's
// ValidPlayer$ scope is read here, in the static's own parameter bucket.
//
// Beside the printed battlefield statics, the walk reads the REGISTERED
// CantGainLife restrictions the Effect-delivered route creates (effEffect's
// StaticAbilities$ case: Screaming Nemesis, Stigma Lasher, Welcome the
// Darkness, Skullcrack, Atarka's Command, Call In a Professional, Roiling
// Vortex). The scope read goes through restrictionPlayerSpecMatches -- the
// one choke point PutCounterBlocked's ValidPlayer$ read already uses -- so an
// IsRemembered clause (Player.IsRemembered, the two damage-trigger carriers)
// resolves against the registered effect's captured player set exactly as
// CantAttack/CantPutCounter do, and the two reader paths cannot drift. An
// empty spec means all players, the printed loop's own convention.
func (e *Engine) lifeGainForbidden(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("CantGainLife") {
		if spec := sv.ParamStr(cards.PKValidPlayer); spec == "" ||
			effects.MatchesPlayerSpec(e.G, spec, p, sv.Controller) {
			return true
		}
	}
	for _, ce := range e.active() {
		if ce.Restriction != "CantGainLife" {
			continue
		}
		if spec := strings.TrimSpace(ce.RestrictParam(cards.PKValidPlayer)); spec != "" &&
			!restrictionPlayerSpecMatches(e.G, spec, p, ce.Controller, ce.Source, ce.RememberedPlayers) {
			continue
		}
		return true
	}
	return false
}

// drawForbidden checks active CantDraw statics against the player who would
// draw a card (CR 121.6). It is lifeGainForbidden's sibling: the same
// battlefield-static collector, the same ValidPlayer$ scope read in the
// static's own parameter bucket, consulted by the same replacement pass.
//
// DrawLimit$ N is a per-turn count cap (CR 121.6): draws below N are allowed,
// while the next draw is prevented. Invalid count values fail closed by
// skipping that static, as with other unread static parameters in this file.
func (e *Engine) drawForbidden(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("CantDraw") {
		if spec := sv.ParamStr(cards.PKValidPlayer); spec != "" &&
			!effects.MatchesPlayerSpec(e.G, spec, p, sv.Controller) {
			continue
		}
		if limit, hasLimit := sv.Param(cards.PKDrawLimit); hasLimit {
			n, err := strconv.ParseInt(strings.TrimSpace(limit), 10, 32)
			if err != nil || n < 0 {
				continue
			}
			if e.CardsDrawnThisTurn(p) >= int32(n) {
				return true
			}
			continue
		}
		return true
	}
	return false
}

type applyLifeReplacementCode uint16

const (
	applyLifeReplacementLoseLife applyLifeReplacementCode = iota + 1
	applyLifeReplacementDraw
)

var applyLifeReplacementCodes = state.NewStrCodes(
	state.StrEntry[applyLifeReplacementCode]{Key: "LoseLife", Val: applyLifeReplacementLoseLife},
	state.StrEntry[applyLifeReplacementCode]{Key: "Draw", Val: applyLifeReplacementDraw},
)
