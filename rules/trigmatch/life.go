// Life-total trigger modes.
//
// Mode$ LifeLost, LifeLostAll and LifeGained.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package trigmatch

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// LifeLoss names the player and positive magnitude of an event that lowers a
// player's life total. Damage to a player and a negative LifeChange are both
// loss of life; damage to an object is not. Infect-marked player damage is
// NOT: CR 702.90b deals it as that many poison counters instead, so neither
// the LifeLost/LifeLostAll triggers nor the repl:LifeReduced machinery (both
// read through here) may treat it as a loss -- the same exclusion
// Engine.emit's checkSpeedGain arm already applies.
func LifeLoss(ev events.Event) (state.PlayerID, int32, bool) {
	switch ev.Kind {
	case events.Damage:
		if ev.Obj == 0 && ev.Amount > 0 && ev.Counter != "infect" {
			return ev.Player, ev.Amount, true
		}
	case events.LifeChange:
		if ev.Amount < 0 {
			return ev.Player, -ev.Amount, true
		}
	}
	return 0, 0, false
}

// lifeGain names the player and positive magnitude of an event that raises a
// player's life total (the mirror of lifeLoss). Damage can only lower life,
// so only a positive LifeChange qualifies.
func lifeGain(ev events.Event) (state.PlayerID, int32, bool) {
	if ev.Kind == events.LifeChange && ev.Amount > 0 {
		return ev.Player, ev.Amount, true
	}
	return 0, 0, false
}

// lifeLostMatches implements Mode$ LifeLost and LifeLostAll. LifeLost sees
// each losing player. LifeLostAll is deferred by Begin/EndLifeLossBatch and
// matches exactly once after adding every serialized loss for each player in
// the simultaneous group. A combat assignment may serialize two one-damage
// hits to one player, but that player lost two life in the one event.
func lifeLostMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	ctrl := e.ControllerOf(source)
	if f := e.Facts(); t.Mode == "LifeLostAll" && f.FinishingLifeLossBatch {
		amounts := make([]int32, len(e.Game().Players))
		for _, be := range f.LifeLossBatch {
			p, amount, ok := LifeLoss(be)
			if ok && int(p) < len(amounts) {
				amounts[p] += amount
			}
		}
		matched := false
		for p, amount := range amounts {
			if amount == 0 {
				continue
			}
			player := state.PlayerID(p)
			if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, player, ctrl) {
				continue
			}
			if v := t.Params["ValidAmountEach"]; v != "" && !CompareLife(amount, v) {
				return false
			}
			matched = true
		}
		return matched
	}
	p, amount, ok := LifeLoss(ev)
	if !ok {
		return false
	}
	if v, ok := t.Param(cards.PKValidPlayer); ok && !effects.MatchesPlayerSpec(e.Game(), v, p, ctrl) {
		return false
	}
	if v, ok := t.Params["ValidAmountEach"]; ok && !CompareLife(amount, v) {
		return false
	}
	// On LifeLost, LifeAmount$ describes the amount just lost, not the
	// LifeTotal$ intervening-if grammar used by other trigger modes.
	if v := t.Params["LifeAmount"]; v != "" && !CompareLife(amount, v) {
		return false
	}
	if strings.EqualFold(t.ParamStr(cards.PKPlayerTurn), "True") && e.Game().Active != ctrl {
		return false
	}
	if v := t.ParamStr(cards.PKValidCause); v != "" && !lifeLossCauseMatches(e, v, ctrl) {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") && !firstLifeLossThisTurn(e, p) {
		return false
	}
	return true
}

// lifeGainedMatches implements Mode$ LifeGained ("whenever you gain life",
// 99 raw corpus carrier files over 91 blocked cards; Prize Pig's ribbon
// payoff is the pin): the event is a LifeChange with a POSITIVE Amount.
// Damage can only lower life, so unlike LifeLost there is no damage arm --
// a Damage event never gains life. ValidPlayer$ names the gainer (the same
// MatchesPlayerSpec read lifeLostMatches uses); ValidAmountEach$ and
// LifeAmount$ compare the gained magnitude the same way their LifeLost
// twins do (On LifeGained, LifeAmount$ is likewise the amount just gained).
// PlayerTurn$ True and FirstTime$ True mirror the gates lifeLostMatches
// implements: PlayerTurn$ (5 corpus lines) fires only during the source
// controller's turn; FirstTime$ (8 lines over 7 files, e.g. Attended Healer's
// "for the first time each turn") admits only the FIRST life-gain event of
// that player this turn. Both are additionally covered by the generic
// actionTriggerModes gates (PlayerTurn$ at queue time, ActivationLimit$),
// since the mode joined that set.
func lifeGainedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.LifeChange || ev.Amount <= 0 {
		return false
	}
	p := ev.Player
	if int(p) < 0 || int(p) >= len(e.Game().Players) {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v, ok := t.Param(cards.PKValidPlayer); ok && !effects.MatchesPlayerSpec(e.Game(), v, p, ctrl) {
		return false
	}
	if v, ok := t.Params["ValidAmountEach"]; ok && !CompareLife(ev.Amount, v) {
		return false
	}
	if v := t.Params["LifeAmount"]; v != "" && !CompareLife(ev.Amount, v) {
		return false
	}
	if strings.EqualFold(t.ParamStr(cards.PKPlayerTurn), "True") && e.Game().Active != ctrl {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") && !firstLifeGainThisTurn(e, p) {
		return false
	}
	return true
}

// lifeLossCauseMatches recognizes the spell/ability cause grammar carried by
// LifeLost triggers. Events intentionally do not encode an extra source field,
// so a synchronous trigger read uses the Engine's in-flight resolving source;
// it is set around every stack resolution and cleared afterward, and thus
// replay derives the same answer. Combat damage is a creature cause, not a
// SpellAbility cause.
func lifeLossCauseMatches(e Board, spec string, you state.PlayerID) bool {
	f := e.Facts()
	if f.CombatDamaging || f.Damaging == 0 {
		return false
	}
	cause := e.Game().Obj(f.Damaging)
	if cause == nil {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		base, qualifier, qualified := strings.Cut(strings.TrimSpace(alt), ".")
		if base != "SpellAbility" {
			continue
		}
		if !qualified || qualifier == "" {
			return true
		}
		switch qualifier {
		case "YouCtrl":
			if cause.Controller == you {
				return true
			}
		case "OppCtrl":
			if cause.Controller != you {
				return true
			}
		}
	}
	return false
}

// firstLifeLossThisTurn is true only for the newest life-loss event of p in
// the current turn. TurnChange is the logged reset boundary for every other
// per-turn fact, so scanning back to it is replay-stable and cannot leak a
// mutable counter across Clone.
func firstLifeLossThisTurn(e Board, p state.PlayerID) bool {
	seenCurrent := false
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			return seenCurrent
		}
		q, _, ok := LifeLoss(ev)
		if !ok || q != p {
			continue
		}
		if seenCurrent {
			return false
		}
		seenCurrent = true
	}
	return seenCurrent
}

// firstLifeGainThisTurn is the LifeGained mirror of firstLifeLossThisTurn:
// true only for the newest life-GAIN event of p in the current turn, so a
// FirstTime$ True trigger (8 corpus lines) admits exactly the first gain of
// that player's turn. Same replay-stable log scan, no mutable counter.
func firstLifeGainThisTurn(e Board, p state.PlayerID) bool {
	seenCurrent := false
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			return seenCurrent
		}
		q, _, ok := lifeGain(ev)
		if !ok || q != p {
			continue
		}
		if seenCurrent {
			return false
		}
		seenCurrent = true
	}
	return seenCurrent
}

func init() {
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return lifeLostMatches(e, t, source, ev)
	}, "LifeLost", "LifeLostAll")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return lifeGainedMatches(e, t, source, ev)
	}, "LifeGained")
}
