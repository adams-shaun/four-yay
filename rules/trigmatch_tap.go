// Tap trigger modes.
//
// Mode$ Taps and TapsForMana, which share one matcher behind a forMana flag.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tapsMatches handles both becomes-tapped and tapped-for-mana triggers. A
// mana activation marks its cost Tap in the engine's synchronous context;
// ordinary Tap events deliberately do not, so attacking and a spell that taps
// a permanent never masquerade as producing mana.
//
// ValidPlayer$ (Taps) and Activator$ (TapsForMana) name the player who tapped
// the permanent -- Forge Card.tap's tapper -- not its controller: "whenever
// you tap an untapped creature an opponent controls" (Icewrought Sentry,
// Solitary Sanctuary, Hylda, Sharae) is about an opponent's creature that YOU
// tapped. emitTap supplies the tapper for every producer.
func (e *Engine) tapsMatches(t cards.Trigger, source state.ObjID, ev events.Event, forMana bool) bool {
	// A permanent entering tapped did not become tapped (CR 603.2e): neither
	// a library search's Tapped$ True entry nor an ETB$ True replacement body
	// can fire a Taps trigger.
	if ev.Kind != events.Tap || ev.Obj == 0 || e.tapIsEntryState(ev) ||
		(forMana && e.tappingForMana != ev.Obj) {
		return false
	}
	actor := e.tapActor(ev)
	if v := t.ParamStr(cards.PKActivator); v != "" && !effects.MatchesPlayerSpec(e.G, v, actor, e.controllerOf(source)) {
		return false
	}
	if v := t.Params["Attacker"]; v != "" {
		want, err := strconv.ParseBool(v)
		if err != nil || e.G.Obj(ev.Obj) == nil || e.G.Obj(ev.Obj).IsAttacking != want {
			return false
		}
	}
	// FirstTime$ True: the permanent had not already become tapped this turn
	// (Forge's tappedThisTurn == 0). emit records the tap only after this
	// event's triggers are matched.
	if strings.EqualFold(t.Params["FirstTime"], "True") && e.becameTappedThisTurn(ev.Obj) {
		return false
	}
	if forMana && !tapsForManaProduced(t.ParamStr(cards.PKProduced), e.tappingManaProduced) {
		return false
	}
	return e.eventCardAndPlayerMatch(t, source, ev.Obj, actor)
}

// untapsMatches observes a real Untap event for the permanent that became
// untapped. Unlike Taps, Untap carries no tapper or mana-activation provenance.
func (e *Engine) untapsMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Untap || ev.Obj == 0 {
		return false
	}
	return e.eventCardAndPlayerMatch(t, source, ev.Obj, e.controllerOf(ev.Obj))
}

// tapIsEntryState reports whether a Tap event only gives a permanent the
// tapped state it enters with: a library search's Tapped$ True entry (marked
// in the replayed payload) or an ETB$ True replacement body (marked in
// emitTap's context, the payload being the ordinary Tap).
func (e *Engine) tapIsEntryState(ev events.Event) bool {
	return ev.Text == "entered tapped" || (e.tapObj == ev.Obj && e.tapEntering)
}

// tapActor is the player who tapped ev's permanent. A Tap emitted without
// emitTap provenance falls back to the permanent's controller.
func (e *Engine) tapActor(ev events.Event) state.PlayerID {
	if e.tapObj == ev.Obj && ev.Obj != 0 {
		return e.tapPlayer
	}
	return e.controllerOf(ev.Obj)
}

// becameTappedThisTurn reports whether obj already became tapped this turn.
func (e *Engine) becameTappedThisTurn(obj state.ObjID) bool {
	turn, ok := e.tappedTurn[obj]
	return ok && turn == e.G.Turn
}

// tapsForManaProduced matches a TapsForMana Produced$ restriction against
// the activating ability's Produced$ declaration the way Forge does against
// the produced mana: the trigger fires when that mana CONTAINS the named
// type, so Forsaken Monument's Produced$ C fires for "C C" and "C U" as well
// as "C". A declaration whose output is a player's choice (Any, Combo ...,
// Chosen) never produces colourless mana, and the chosen colour is not known
// at the Tap boundary, so a colour-choice declaration matches nothing; a
// ChosenColor restriction (the trigger's own chosen colour) is likewise
// unsupported and fails closed.
// tapsForManaBraces is built once: a Replacer is safe for concurrent use.
var tapsForManaBraces = strings.NewReplacer("{", " ", "}", " ")

func tapsForManaProduced(want, produced string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	if len(want) != 1 || !strings.Contains(effects.ManaSymbols, want) {
		return false
	}
	for field := range strings.FieldsSeq(tapsForManaBraces.Replace(produced)) {
		for _, r := range field {
			if !strings.ContainsRune(effects.ManaSymbols, r) {
				return false // a choice word: Any, Combo, Chosen, ...
			}
		}
		if strings.Contains(field, want) {
			return true
		}
	}
	return false
}

func init() {
	// Taps and TapsForMana are one matcher behind a forMana flag.
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.tapsMatches(t, source, ev, false)
	}, "Taps")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.tapsMatches(t, source, ev, true)
	}, "TapsForMana")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.untapsMatches(t, source, ev)
	}, "Untaps")
}
