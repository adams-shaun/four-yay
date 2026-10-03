// Card-flow trigger modes.
//
// Mode$ Drawn, Discarded, Cycled, Explores and Investigated, with the
// cause-admission and first-of-turn bookkeeping they need.
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

// cycledMatches implements the "when you cycle [this card]" trigger (CR
// 702.29d's cycling trigger, Forge Mode$ Cycled -- Dismantling Wave, 77
// corpus files). The engine's cycle activation discards the card as its
// cost, so the causing event is that cost discard (events.DiscardCost's
// canonical hand-to-graveyard move), tagged with the CYCLING ABILITY that
// paid it (events.DiscardCostCycling). The tag, not the moved card's printed
// face, is what makes a cost discard a cycle: an ability whose cycling is
// granted in a layer (Rhet-Tomb Mystic, Tectonic Reformation, Homing Sliver)
// tags its discard just like a printed K:Cycling, while an ordinary discard
// (a Wheel effect) -- or a cost discard paid for a different ability --
// carries no tag and does not match even when the card prints Cycling. The
// ability's keyword head ("Cycling" / "TypeCycling") travels as the cause,
// so the event records which named ability was cycled. ValidCard$ is matched
// against the moved card's LKI -- the card is already in its destination zone
// when triggers are checked, exactly like Sacrificed. The cycler is the moved
// card's controller: a card in a hand is controlled by its owner, and
// DiscardCost carries no player field to read instead.
func cycledMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if _, ok := events.IsCyclingDiscard(ev); !ok {
		return false
	}
	o := lki
	if o == nil {
		o = e.Game().Obj(ev.Obj)
	}
	if o == nil {
		return false
	}
	return eventCardAndPlayerMatch(e, t, source, ev.Obj, o.Controller)
}

// exploresMatches implements the "Whenever a creature you control explores
// ..." trigger family (Forge Mode$ Explores, task explore1 — Merfolk
// Cave-Diver, Nicanzil Current Conductor, Wildgrowth Walker, Lurking
// Chupacabra, Shadowed Caravel; 5 files / 6 raw lines at the corpus pin).
// The causing event is the completed events.Explore record: Obj is the
// EXPLORER (what ValidCard$ matches, with the explorer's controller as the
// event player — the same eventCardAndPlayerMatch read Sacrificed applies)
// and IDs[0] is the card the process revealed, which ValidExplored$ narrows
// ("explores a land card" / "explores a nonland card" — Nicanzil's pair).
// The revealed card is matched in whatever zone the explore left it in (hand
// or graveyard, or back on top): the plain type predicates both carriers use
// are zone-independent, and the reveal Note that precedes the record already
// made the card public, so no LKI capture is needed. A record with no
// revealed card is unreachable (an empty library records nothing).
func exploresMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Explore || len(ev.IDs) == 0 {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v := t.Params["ValidExplored"]; v != "" &&
		!e.MatchesSpec(v, ev.IDs[0], source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

// connivesMatches implements the "Whenever a creature you control connives
// ..." trigger family (Forge Mode$ Connives, task connive1 -- Iron Monger
// Sadistic Tycoon, Glorious Purpose, Ultron Unlimited; 3 files / 3 raw lines
// at the corpus pin). The causing event is the completed events.Connive
// record (a pure Apply no-op marker emitted by effConnive after each
// conniver's draws, discards and counters): Obj is the CONNIVER (what
// ValidCard$ matched, with the conniver's controller as the event player --
// the same eventCardAndPlayerMatch read the Explores family applies) and
// IDs are the discarded cards in discard order. The record is one per
// completed connive action, never one per discarded card.
func connivesMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Connive || ev.Obj == 0 {
		return false
	}
	return eventCardAndPlayerMatch(e, t, source, ev.Obj, ev.Player)
}

// searchedLibraryMatches handles the four corpus SearchedLibrary carriers.
// applyLibrarySearch emits one marker per completed searched library, separate
// from individual card moves, so both empty and successful searches fire once.
func searchedLibraryMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.SearchedLibrary {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	return true
}

// investigatedMatches implements the "whenever you investigate" trigger
// family (Forge Mode$ Investigated, task investtrig1 -- Erdwal Illuminator,
// Val, Marooned Surveyor; 2 files / 2 raw lines at the corpus pin). The
// causing event is the completed events.Investigate record (a pure Apply
// no-op marker emitted by effInvestigate beside each Clue mint, so a plain
// Clue-token creation never fires it): Player is the investigating seat
// (what ValidPlayer$ matches -- Erdwal's and Val's `ValidPlayer$ You`), Obj
// the resolving source permanent (what a ValidCard$ spec would match; no
// corpus carrier uses one, but the grammar is the exploresMatches shape).
// FirstTime$ True is the per-player per-turn gate -- Erdwal's "for the
// first time each turn" -- read from the log the firstLifeLossThisTurn way
// so a log-only replay reconstructs the same answer (no side-map).
func investigatedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Investigate {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") && !firstInvestigateThisTurn(e, ev.Player) {
		return false
	}
	return true
}

// giveGiftMatches implements the "whenever you give a gift" trigger family
// (Forge Mode$ GiveGift, CR 702.168; Jolly Gerbils -- the one corpus carrier).
// The causing event is the completed events.GiveGift record, a pure Apply
// no-op marker the spell's resolution emits beside a promised gift (the
// Investigate/Explore shape -- a dedicated Kind, not a Note, so an unrelated
// draw or token creation never fires this mode). Player is the giver (the
// resolving spell's controller), which ValidPlayer$ You matches; Obj is the
// resolving source, which a ValidCard$ spec would match (no corpus carrier
// carries one, but the grammar is read so a future line is not silently
// inert). A gift marker with no giver never matches.
func giveGiftMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.GiveGift {
		return false
	}
	if int(ev.Player) >= len(e.Game().Players) {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

// discoverMatches implements the "whenever you discover" trigger family
// (Forge Mode$ Discover, task trigdisc1 -- Val, Marooned Surveyor, Curator of
// Sun's Creation; 2 corpus files / 2 raw lines at the corpus pin). The causing
// event is the completed events.Discover record (a pure Apply no-op marker the
// api:Discover primitive will emit beside each completed discover action, one
// per ACTION not per exiled card -- CR 701.57's exile-many-reveal-one shape is
// ONE discover): Player is the discovering seat (what ValidPlayer$ matches --
// both carriers' `ValidPlayer$ You`), Obj the resolving source permanent (what
// a ValidCard$ spec would match; no corpus carrier uses one, but the grammar
// is the investigatesMatches shape). FirstTime$ is not read -- no corpus
// carrier carries it; the per-turn shape these modes use is ActivationLimit$,
// enforced at queue time through actionTriggerModes membership (Curator's
// ActivationLimit$ 1).
func discoverMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Discover {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	return true
}

// seekAllMatches implements the "whenever you seek one or more cards" trigger
// family (Forge Mode$ SeekAll, task trigdisc1 -- Vexyr, Ich-Tekik's Heir; Val,
// Marooned Surveyor; Lurker in the Deep; 3 corpus files / 3 raw lines at the
// corpus pin). The causing event is the completed events.Seek record, ONE per
// seek ACTION: a seek of three cards is one marker and one trigger (the
// "one or more cards" of the oracle text is the number sought, not the trigger
// count), and the emitter's contract is to emit only when the seek found at
// least one card. Player is the seeking seat (what ValidPlayer$ matches -- all
// three carriers' `ValidPlayer$ You`), Obj the resolving source permanent;
// Lurker's `PlayerTurn$ True` rides the ordinary actionTriggerModes queue-time
// gate, not this matcher.
func seekAllMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Seek {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	return true
}

// surveilMatches implements the "whenever you surveil" trigger family
// (Forge Mode$ Surveil, task trig-surveil: 12 corpus files / 12 raw lines at
// the corpus pin -- Mirko, Obsessive Theorist; Dimir Spybug; Thoughtbound
// Phantasm; Whispering Snitch; Copy Catchers; Disinformation Campaign;
// Blood Operative; and the five Secondary$ scry-paired lines). The causing
// event is the completed events.Surveil record (a pure Apply no-op marker
// api:Surveil's effSurveil emits beside each surveil instruction, one per
// acting player): Player is the surveiling seat (what ValidPlayer$ matches --
// eleven carriers' `ValidPlayer$ You` and River Song's `ValidPlayer$
// Opponent`), Obj the resolving source permanent (what a ValidCard$ spec
// would match; no corpus carrier uses one, the discoverMatches shape).
// FirstTime$ is read the LifeLost/Investigated way: the log scan admits
// exactly the acting player's first surveil of the turn (Whispering
// Snitch's "for the first time each turn").
func surveilMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Surveil {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") &&
		!firstMarkerThisTurn(e, events.Surveil, ev.Player) {
		return false
	}
	return true
}

// scryMatches implements the "whenever you scry" / "whenever you choose to
// put one or more cards on the bottom while scrying" family (Forge Mode$
// Scry, task scrybottom). The causing event is the completed events.Scry
// record rules' handleArrange emits once the KArrange answer is known (a
// pure Apply no-op marker): Player is the scrying seat (what ValidPlayer$
// matches), Obj the resolving source permanent, and Amount the number of
// cards actually put on the BOTTOM of the library -- 0 when every looked-at
// card was kept on top. `ToBottom$ True` (The Temporal Anchor, the corpus's
// one carrier at the pin) is the "one or more" gate: the event fires even
// for a bottom-less scry, so the matcher, not the emitter, is where the
// "one or more" is enforced. No other Scry parameter is read here (ScryNum$
// count bodies stay the separate unmodelled TriggerCount$ScryNum head).
func scryMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Scry {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	// "one or more cards": a scry that bottomed none must not fire a
	// ToBottom$ True trigger, however many cards were looked at.
	if strings.EqualFold(t.Params["ToBottom"], "True") && ev.Amount <= 0 {
		return false
	}
	return true
}

// firstMarkerThisTurn is the shared replay-stable log scan behind the
// FirstTime$ gates over pure marker Kinds: true only when the event being
// matched is the player's FIRST record of `kind` in the current turn. The
// current event is already in the log when triggers match (the
// firstLifeLossThisTurn contract), so scanning back past TurnChange and
// finding exactly one record for p means this is the first. TurnChange is
// the logged reset boundary for every other per-turn fact, so the scan
// cannot leak a mutable counter across Clone. firstInvestigateThisTurn's
// identical scan now reads this helper (byte-identical behaviour).
func firstMarkerThisTurn(e Board, kind events.Kind, p state.PlayerID) bool {
	seenCurrent := false
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			return seenCurrent
		}
		if ev.Kind != kind || ev.Player != p {
			continue
		}
		if seenCurrent {
			return false
		}
		seenCurrent = true
	}
	return seenCurrent
}

// firstInvestigateThisTurn is true only when the investigate event being
// matched is the investigating player's first of the current turn: a
// FirstTime$ True gate (CR 701.36a-family), evaluated through the shared
// firstMarkerThisTurn scan (the contract comment there).
func firstInvestigateThisTurn(e Board, p state.PlayerID) bool {
	return firstMarkerThisTurn(e, events.Investigate, p)
}

func DiscardedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if !events.IsDiscard(ev) ||
		!eventCardAndPlayerMatch(e, t, source, ev.Obj, e.ControllerOf(ev.Obj)) {
		return false
	}
	if spec := t.ParamStr(cards.PKValidCause); spec != "" && !DiscardCauseAdmits(e, spec, source, ev) {
		return false
	}
	return true
}

// discardedAllMatches implements Mode$ DiscardedAll, the BATCH "whenever you
// discard one or more [filtered] cards" trigger family (CR 701.8; 21 corpus
// T: lines over 21 files plus Pure Intentions' one SVar-carried body at the
// pin). It fires on the discard MoveZone event events.Discard emits -- the
// same action marker IsDiscard/IsSacrifice/IsMill use -- so the discard
// (effects' api:Discard, effDiscard) and the payoff agree on what a discard
// is.
//
// The cadence -- once for the WHOLE discard action, not once per card -- is
// the dispatcher's job, not this matcher's: a latch in checkFaceTriggers
// (rules/trigger_match.go) keys on the trigger line inside effDiscard's open
// discard batch, and closeDiscardBatch patches the matched-card COUNT into
// the queued trigger's TriggerAmount and their set into its Remembered/
// Captured. This matcher therefore still returns true for EVERY matching
// discarded card, because the latch accumulates its count from each accepted
// event. Outside a batch (a cost discard, a cleanup discard) the mode
// fires per event with the referent below already carrying count 1.
//
// Parameters matched here, in the corpus's own spelling:
//
//   - ValidPlayer$ names whose discard counts. ev.Player is the discarding
//     player, and "you" is the trigger source's controller, so the corpus's
//     `ValidPlayer$ You` admits only the controller's own discard while
//     `ValidPlayer$ Player` (Hostile Investigator, Tinybones) admits any
//     player's. An absent clause matches any player.
//   - ValidCard$ is the discarded card's filter (Card.nonLand on Veronica
//     and Conspiracy Theorist, Card.Artifact on Mishra/Urza and the two
//     Arena rebalances, Land on Doctor Doom, the plain Card on Inti and
//     Diviner). It is matched against ev.Obj, the discarded card, with the
//     trigger source's controller as the filter perspective.
//   - ValidCause$ (Pure Intentions' one SVar body, the only carrier) is read
//     through the shared discardCauseAdmits, exactly as the per-card
//     Discarded matcher reads it.
//
// FirstTime$ True (Veronica, Rielle) is deliberately NOT read here:
// firstMarkerThisTurn's per-EVENT log scan cannot tell one discard BATCH
// from the next, and "for the first time each turn" is a batch-level fact.
// The latch in checkFaceTriggers enforces it instead, at the one point that
// knows batch identity (the Engine.discardAllTurn stamp). ActivationLimit$
// is likewise enforced at queue time through actionTriggerModes membership.
func discardedAllMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if !events.IsDiscard(ev) {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if spec := t.ParamStr(cards.PKValidCause); spec != "" && !DiscardCauseAdmits(e, spec, source, ev) {
		return false
	}
	return true
}

// DiscardedAllFirstTime reports a Mode$ DiscardedAll line's FirstTime$ True
// clause (Veronica, Rielle: "for the first time each turn"). It is a
// BATCH-level fact the dispatcher's latch enforces, because batch identity
// lives in the latch and firstMarkerThisTurn's per-EVENT log scan cannot tell
// one discard batch from the next, so checkFaceTriggers asks this at the
// queue point. It is a pure read of the trigger line: discardedAllMatches
// used to record the clause as engine scratch for the dispatcher to capture,
// which made the matcher write; the matcher is now read-only. FirstTime$ is
// already in the parameter census's shared trigger read set (measured: it is
// attributed to ChangesZone and Attacks too), so moving this read out of the
// matcher changes no census attribution.
func DiscardedAllFirstTime(t cards.Trigger) bool {
	return strings.EqualFold(t.Params["FirstTime"], "True")
}

// DiscardCauseAdmits evaluates a ValidCause$ stack spec against the spell or
// ability that caused discard ev, from source's controller's perspective. It
// serves both the Discarded trigger and a Discard$ True replacement.
func DiscardCauseAdmits(e Board, spec string, source state.ObjID, ev events.Event) bool {
	// A discard paid as a cost has no causing spell or ability. In
	// particular, do not misattribute it to an unrelated object that was
	// already on the stack when a player activated in response.
	if events.IsDiscardCost(ev) {
		return false
	}
	cause := e.ActionCause()
	if cause == 0 {
		return false
	}
	o := e.Game().Obj(cause)
	return o != nil && state.StackKindAdmits(state.StackKindTokens(spec), state.StackKindOf(e.Game(), o), o,
		o.Controller, e.ControllerOf(source))
}

// drawnMatches implements Mode$ Drawn. A Draw event moves exactly one card
// from a library to its controller's hand, so ValidCard$ is tested against the
// drawn object and TriggeredPlayer is that event's Player. FirstCardInDrawStep$
// is derived from the ordered log after the event has landed: only the first
// Draw between entry to the draw step and its next StepChange qualifies.
func drawnMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Draw {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v, ok := t.Param(cards.PKValidCard); ok && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v := t.Params["Number"]; v != "" {
		want, err := strconv.Atoi(v)
		if err != nil || drawNumberThisTurn(e, ev.Player) != want {
			return false
		}
	}
	// PlayerTurn$ is the trigger controller's turn, not the drawing player's:
	// Keranos's "on each of your turns" must reject an opponent's first draw.
	if strings.EqualFold(t.ParamStr(cards.PKPlayerTurn), "True") && e.Game().Active != ctrl {
		return false
	}
	if v, ok := t.Params["FirstCardInDrawStep"]; ok {
		first := firstCardInDrawStep(e, ev.Player)
		if (strings.EqualFold(v, "True") && !first) || (strings.EqualFold(v, "False") && first) {
			return false
		}
	}
	return true
}

// drawNumberThisTurn counts p's draws in the current turn. The log is the
// replay-stable source of this per-turn fact; each player has its own ordinal
// because "their second card" must not count another seat's draw. Callers
// differ on whether the Draw currently being matched is already logged:
// trigger matching runs POST-emit (the event is in the log), while
// replacement matching runs PRE-emit (it is not), so a replacement matcher
// must add the pending draw's own applicability itself.
func drawNumberThisTurn(e Board, p state.PlayerID) int {
	n := 0
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// firstCardInDrawStep reports whether the most recently emitted Draw for p is
// the first draw since this turn entered its draw step. The log, rather than a
// mutable counter, is the source of this ephemeral fact so cloning and replay
// rebuild it without an event-schema change.
func firstCardInDrawStep(e Board, p state.PlayerID) bool {
	if e.Game().Step != state.StepDraw {
		return false
	}
	draws := 0
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.Draw && ev.Player == p {
			draws++
		}
		if ev.Kind == events.StepChange {
			return ev.Step == state.StepDraw && draws == 1
		}
	}
	return false
}

// exploitedMatches implements Mode$ Exploited (CR 702.58c: "Whenever a
// creature exploits a creature, ..." -- 24 corpus lines / 24 files at the
// pin). The causing event is the events.Exploit marker the K:Exploit
// expansion's marker SA emits (effects/exploit.go), the same pure-marker
// shape trig:Explores/trig:Investigated fire on: Obj is the EXPLOITING
// creature, IDs[0] the EXPLOITED one, Player the exploiting creature's
// controller.
//
//   - ValidSource$ names the exploiting creature and is matched against
//     ev.Obj through the ordinary spec grammar with the trigger's own source
//     as Self -- so Graf Reaver's `ValidSource$ Card.Self` compares the
//     exploiter to Graf Reaver, and Colonel Autumn's `ValidSource$
//     Creature.YouCtrl` admits any creature its controller controls.
//   - ValidCard$ names the exploited creature and is matched against
//     ev.IDs[0] -- Henry Wu's `Creature.nonHuman`, Silent-Blade Oni's
//     `Creature.!token` and the plain `Creature` of every other carrier.
//   - ValidPlayer$ names the exploiting player (no corpus carrier carries
//     one; the gate is read anyway so a future line is not silently inert).
//
// Every corpus line carries BOTH ValidSource$ and ValidCard$, so the two
// reads are the whole matcher. A marker with no exploited id (a malformed
// chain) never matches; the marker is emitted only after the sacrifice's own
// MoveZone, so the exploited card is readable in its graveyard.
func exploitedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Exploit || len(ev.IDs) == 0 || ev.IDs[0] == 0 {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidSource); v != "" &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidCard); v != "" &&
		!e.MatchesSpec(v, ev.IDs[0], source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	return true
}

// evolvedMatches implements "whenever this creature evolves" (Forge Mode$
// Evolved, CR 702.99b, task trig:Evolved; Watchful Radstag and Renegade Krasis
// -- the 2 corpus carriers at the pin). The causing event is the completed
// events.Evolved marker rules' resolveTop emits after the Evolve keyword
// ability actually put its +1/+1 counter (a pure Apply no-op marker, the
// GiveGift/Investigate shape -- a dedicated Kind, not a CounterChange rider,
// so an unrelated +1/+1 counter never fires the mode). Obj is the evolving
// permanent, which ValidCard$ Card.Self names; Player its controller, which
// ValidPlayer$ would match. A marker naming no permanent never matches.
func evolvedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Evolved {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	return true
}

// ClashMatches implements Mode$ Clashed (CR 701.31: "Whenever you win/lose a
// clash ..."). The causing event is one events.Clash marker per clashing
// player (effClash emits one per participant, matching Forge's
// ClashEffect, which runs the Clashed trigger once per player): Obj is the
// resolving source permanent, Player the clashing seat this record reports,
// and Amount 1 when that player won and 0 when they lost or tied. The
// corpus's four carriers (Marvo, Deep Operative; Entangling Trap; Rebellion
// of the Flamekin; Sylvan Echoes -- 6 raw lines) all gate on
// `ValidPlayer$ You` plus the `Won$ True`/`Won$ False` orientation, so the
// two reads are the whole matcher: ValidPlayer$ is matched against ev.Player
// through the ordinary player-spec grammar with the trigger source's
// controller as You, and Won$ requires Amount's win bit to equal it.
// Entangling Trap and Rebellion of the Flamekin carry a pair of lines (True
// and its `Secondary$ True` False sibling), which Forge fires one of via the
// Won$ split; this matcher keeps each line's gate independent, so exactly
// the matching one fires.
func ClashMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Clash {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if w := strings.TrimSpace(t.Params["Won"]); w != "" {
		want := strings.EqualFold(w, "True")
		if (ev.Amount != 0) != want {
			return false
		}
	}
	return true
}

func init() {
	registerTrigMatcher(cycledMatches, "Cycled")
	registerTrigMatcher(exploitedMatches, "Exploited")
	registerTrigMatcher(exploresMatches, "Explores")
	registerTrigMatcher(connivesMatches, "Connives")
	registerTrigMatcher(investigatedMatches, "Investigated")
	registerTrigMatcher(giveGiftMatches, "GiveGift")
	registerTrigMatcher(evolvedMatches, "Evolved")
	registerTrigMatcher(ClashMatches, "Clashed")
	registerTrigMatcher(chaosEnsuesMatches, "ChaosEnsues")
	registerTrigMatcher(planeswalkedToMatches, "PlaneswalkedTo")
	registerTrigMatcher(planeswalkedFromMatches, "PlaneswalkedFrom")
	registerTrigMatcher(searchedLibraryMatches, "SearchedLibrary")
	registerTrigMatcher(discoverMatches, "Discover")
	registerTrigMatcher(seekAllMatches, "SeekAll")
	registerTrigMatcher(surveilMatches, "Surveil")
	registerTrigMatcher(scryMatches, "Scry")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return DiscardedMatches(e, t, source, ev)
	}, "Discarded")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return discardedAllMatches(e, t, source, ev)
	}, "DiscardedAll")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return drawnMatches(e, t, source, ev)
	}, "Drawn")
	effects.RegisterNonAPI("trig:DiscardedAll")
}
