// Tap trigger modes.
//
// Mode$ Taps and TapsForMana, which share one matcher behind a forMana flag.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package trigmatch

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
func tapsMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, forMana bool) bool {
	// A permanent entering tapped did not become tapped (CR 603.2e): neither
	// a library search's Tapped$ True entry nor an ETB$ True replacement body
	// can fire a Taps trigger.
	if ev.Kind != events.Tap || ev.Obj == 0 || TapIsEntryState(e, ev) ||
		(forMana && e.Facts().TappingForMana != ev.Obj) {
		return false
	}
	// Only the elected Teamwork cost taps carry this replay-visible reason;
	// ordinary Taps triggers still see every real tap as before.
	if teamwork, ok := t.ParamCode(cards.PKTeamwork); !forMana && ok && teamwork == 1 && ev.Counter != events.TapTeamworkCounter {
		return false
	}
	actor := TapActor(e, ev)
	if v := t.ParamStr(cards.PKActivator); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, actor, e.ControllerOf(source)) {
		return false
	}
	if v := t.ParamStr(cards.PKAttacker); v != "" {
		want, err := strconv.ParseBool(v)
		if err != nil || e.Game().Obj(ev.Obj) == nil || e.Game().Obj(ev.Obj).IsAttacking != want {
			return false
		}
	}
	// FirstTime$ True: the permanent had not already become tapped this turn
	// (Forge's tappedThisTurn == 0). emit records the tap only after this
	// event's triggers are matched.
	if strings.EqualFold(t.ParamStr(cards.PKFirstTime), "True") && becameTappedThisTurn(e, ev.Obj) {
		return false
	}
	if forMana && !tapsForManaProduced(t.ParamStr(cards.PKProduced), e.Facts().TappingManaProduced) {
		return false
	}
	return eventCardAndPlayerMatch(e, t, source, ev.Obj, actor)
}

// untapsMatches observes a real Untap event for the permanent that became
// untapped. Unlike Taps, Untap carries no tapper or mana-activation provenance.
func untapsMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Untap || ev.Obj == 0 {
		return false
	}
	return eventCardAndPlayerMatch(e, t, source, ev.Obj, e.ControllerOf(ev.Obj))
}

// TapIsEntryState reports whether a Tap event only gives a permanent the
// tapped state it enters with: a library search's Tapped$ True entry (marked
// in the replayed payload) or an ETB$ True replacement body (marked in
// emitTap's context, the payload being the ordinary Tap).
func TapIsEntryState(e Board, ev events.Event) bool {
	if ev.Text == "entered tapped" {
		return true
	}
	f := e.Facts()
	return f.TapObj == ev.Obj && f.TapEntering
}

// TapActor is the player who tapped ev's permanent. A Tap emitted without
// emitTap provenance falls back to the permanent's controller.
func TapActor(e Board, ev events.Event) state.PlayerID {
	if f := e.Facts(); f.TapObj == ev.Obj && ev.Obj != 0 {
		return f.TapPlayer
	}
	return e.ControllerOf(ev.Obj)
}

// becameTappedThisTurn reports whether obj already became tapped this turn.
func becameTappedThisTurn(e Board, obj state.ObjID) bool {
	turn, ok := e.Facts().TappedTurn[obj]
	return ok && turn == e.Game().Turn
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

// aggregateTapSpec is the ValidCards$/ValidCard$ filter of the aggregate tap
// modes: the PLURAL key all three corpus carriers use (Rewrite History,
// Deeproot Pilgrimage, The Millennium Calendar), with the singular fallback
// the shared Taps/Untaps matcher reads. Returns ok=false when the line names
// neither (no card filter).
func aggregateTapSpec(t cards.Trigger) (string, bool) {
	if v, ok := t.Param(cards.PKValidCards); ok {
		return v, true
	}
	return t.Param(cards.PKValidCard)
}

// tapAllMatches matches a Mode$ TapAll trigger ("whenever one or more ...
// become tapped") against one Tap event. It is the per-event half of the
// batch reading: the dispatcher (rules/trigger_match.go) collapses the events
// of one tapping action into a single instance, and this matcher still has to
// return true for EVERY matching event so the latch can count them. The
// clauses are the Taps matcher's (entry-state exclusion, the tapper's
// ValidPlayer$/Activator$, the Attacker$ gate, and the card filter) except
// the card filter reads the plural ValidCards$ the aggregate modes print.
func tapAllMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Tap || ev.Obj == 0 || TapIsEntryState(e, ev) {
		return false
	}
	ctrl := e.ControllerOf(source)
	actor := TapActor(e, ev)
	if v := t.ParamStr(cards.PKActivator); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, actor, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, actor, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKAttacker); v != "" {
		want, err := strconv.ParseBool(v)
		if err != nil || e.Game().Obj(ev.Obj) == nil || e.Game().Obj(ev.Obj).IsAttacking != want {
			return false
		}
	}
	if spec, ok := aggregateTapSpec(t); ok && strings.TrimSpace(spec) != "" {
		if !e.MatchesSpec(spec, ev.Obj, source, ctrl, SpecOpts{}) {
			return false
		}
	}
	return true
}

// untapAllMatches matches a Mode$ UntapAll trigger ("whenever one or more ...
// become untapped") against one Untap event, the untap twin of tapAllMatches:
// the same plural-then-singular card filter, and ValidPlayer$ naming the
// player who untapped the permanent (its controller), exactly as Untaps does.
func untapAllMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Untap || ev.Obj == 0 {
		return false
	}
	ctrl := e.ControllerOf(source)
	actor := e.ControllerOf(ev.Obj)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, actor, ctrl) {
		return false
	}
	if spec, ok := aggregateTapSpec(t); ok && strings.TrimSpace(spec) != "" {
		if !e.MatchesSpec(spec, ev.Obj, source, ctrl, SpecOpts{}) {
			return false
		}
	}
	return true
}

func init() {
	cards.RegisterParamCoder(cards.PKTeamwork, "trigmatch.Teamwork", func(v string) uint16 {
		if enabled, err := strconv.ParseBool(v); err == nil && enabled {
			return 1
		}
		return 0
	})
	// Taps and TapsForMana are one matcher behind a forMana flag.
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return tapsMatches(e, t, source, ev, false)
	}, "Taps")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return tapsMatches(e, t, source, ev, true)
	}, "TapsForMana")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return untapsMatches(e, t, source, ev)
	}, "Untaps")
	// TapAll/UntapAll (task cli-20261005T075020Z-05241a06) are the batch
	// siblings: one trigger per tapping/untapping ACTION, not per permanent.
	// Their per-event matchers read the plural ValidCards$ the aggregate modes
	// print; the "one or more" cadence is the dispatcher's batch latch
	// (rules/trigger_match.go, keyed on the trigger line inside the open
	// zone/action bracket), which still requires the matcher to return true
	// for every matching event so the latch can accumulate its count.
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
		return tapAllMatches(e, t, source, ev, lki)
	}, "TapAll")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
		return untapAllMatches(e, t, source, ev, lki)
	}, "UntapAll")
}
