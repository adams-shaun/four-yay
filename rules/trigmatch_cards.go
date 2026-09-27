// Card-flow trigger modes.
//
// Mode$ Drawn, Discarded, Cycled, Explores and Investigated, with the
// cause-admission and first-of-turn bookkeeping they need.
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
func (e *Engine) cycledMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if _, ok := events.IsCyclingDiscard(ev); !ok {
		return false
	}
	o := lki
	if o == nil {
		o = e.G.Obj(ev.Obj)
	}
	if o == nil {
		return false
	}
	return e.eventCardAndPlayerMatch(t, source, ev.Obj, o.Controller)
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
func (e *Engine) exploresMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Explore || len(ev.IDs) == 0 {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if v := t.Params["ValidExplored"]; v != "" &&
		!e.matchesSpec(v, ev.IDs[0], e.specCtx(source, ctrl)) {
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
func (e *Engine) connivesMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Connive || ev.Obj == 0 {
		return false
	}
	return e.eventCardAndPlayerMatch(t, source, ev.Obj, ev.Player)
}

// searchedLibraryMatches handles the four corpus SearchedLibrary carriers.
// applyLibrarySearch emits one marker per completed searched library, separate
// from individual card moves, so both empty and successful searches fire once.
func (e *Engine) searchedLibraryMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.SearchedLibrary {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
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
func (e *Engine) investigatedMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Investigate {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") && !e.firstInvestigateThisTurn(ev.Player) {
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
func (e *Engine) giveGiftMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.GiveGift {
		return false
	}
	if int(ev.Player) >= len(e.G.Players) {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
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
func (e *Engine) discoverMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Discover {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
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
func (e *Engine) seekAllMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Seek {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
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
func (e *Engine) surveilMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Surveil {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") &&
		!e.firstMarkerThisTurn(events.Surveil, ev.Player) {
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
func (e *Engine) scryMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Scry {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
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
func (e *Engine) firstMarkerThisTurn(kind events.Kind, p state.PlayerID) bool {
	seenCurrent := false
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
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
func (e *Engine) firstInvestigateThisTurn(p state.PlayerID) bool {
	return e.firstMarkerThisTurn(events.Investigate, p)
}

func (e *Engine) discardedMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if !events.IsDiscard(ev) ||
		!e.eventCardAndPlayerMatch(t, source, ev.Obj, e.controllerOf(ev.Obj)) {
		return false
	}
	if spec := t.Params["ValidCause"]; spec != "" && !e.discardCauseAdmits(spec, source, ev) {
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
func (e *Engine) discardedAllMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if !events.IsDiscard(ev) {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidPlayer"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if v := t.Params["ValidCard"]; v != "" && !e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if spec := t.Params["ValidCause"]; spec != "" && !e.discardCauseAdmits(spec, source, ev) {
		return false
	}
	// FirstTime$ True (Veronica, Rielle: "for the first time each turn") is a
	// BATCH-level fact the dispatcher's latch enforces, because batch identity
	// lives in the latch and firstMarkerThisTurn's per-EVENT log scan cannot
	// tell one discard batch from the next. The param is read HERE, in the
	// matcher REGISTERED for DiscardedAll, rather than in the shared
	// checkFaceTriggers dispatcher, so the parameter census attributes the read
	// to mode DiscardedAll alone; the dispatcher captures this scratch value
	// immediately after the match call and applies it at the queue point.
	e.discardAllFirstTime = strings.EqualFold(t.Params["FirstTime"], "True")
	return true
}

// discardCauseAdmits evaluates a ValidCause$ stack spec against the spell or
// ability that caused discard ev, from source's controller's perspective. It
// serves both the Discarded trigger and a Discard$ True replacement.
func (e *Engine) discardCauseAdmits(spec string, source state.ObjID, ev events.Event) bool {
	// A discard paid as a cost has no causing spell or ability. In
	// particular, do not misattribute it to an unrelated object that was
	// already on the stack when a player activated in response.
	if events.IsDiscardCost(ev) {
		return false
	}
	cause := e.actionCause()
	if cause == 0 {
		return false
	}
	o := e.G.Obj(cause)
	return o != nil && state.StackKindAdmits(state.StackKindTokens(spec), state.StackKindOf(e.G, o), o,
		o.Controller, e.controllerOf(source))
}

// cyclingCauseKeywords is the CR 702.28 cycling family: the plain Cycling
// keyword and its typed form (TypeCycling), both of which Forge's `Cycling`
// stack spec names. Exact membership -- a substring test would let
// TypeCycling's own "Cycling" suffix admit the plain spec through a false
// positive, and vice versa is impossible because the values are distinct.
var cyclingCauseKeywords = map[string]bool{"Cycling": true, "TypeCycling": true}

// drawnMatches implements Mode$ Drawn. A Draw event moves exactly one card
// from a library to its controller's hand, so ValidCard$ is tested against the
// drawn object and TriggeredPlayer is that event's Player. FirstCardInDrawStep$
// is derived from the ordered log after the event has landed: only the first
// Draw between entry to the draw step and its next StepChange qualifies.
func (e *Engine) drawnMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.Draw {
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidCard"]; ok && !e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if v := t.Params["Number"]; v != "" {
		want, err := strconv.Atoi(v)
		if err != nil || e.drawNumberThisTurn(ev.Player) != want {
			return false
		}
	}
	// PlayerTurn$ is the trigger controller's turn, not the drawing player's:
	// Keranos's "on each of your turns" must reject an opponent's first draw.
	if strings.EqualFold(t.Params["PlayerTurn"], "True") && e.G.Active != ctrl {
		return false
	}
	if v, ok := t.Params["FirstCardInDrawStep"]; ok {
		first := e.firstCardInDrawStep(ev.Player)
		if (strings.EqualFold(v, "True") && !first) || (strings.EqualFold(v, "False") && first) {
			return false
		}
	}
	return true
}

// drawCauseAdmits evaluates a ValidCause$ stack spec against the spell or
// ability that caused Draw event ev, from source's controller's perspective.
// It serves the Draw replacement arm (Unpredictable Cyclone, the corpus's
// only Draw ValidCause$ carrier: "If a cycling ability of another nonland
// card would cause you to draw a card, instead ...").
//
// The base kind and the controller / instant-sorcery restrictions are read
// through state's shared classifier (StackKindTokenOf + StackKindAdmits), so
// this cannot drift from TargetType$/ValidStack. But that classifier
// DELIBERATELY ignores every other qualifier (its doc comment records the
// widening), which is fine for a target offer but wrong here: a replacement
// scoped by `Activated.Cycling+nonLand` must not apply to a draw caused by
// ANY activated ability. The qualifiers this helper adds are the ones the one
// Draw carrier needs -- `Cycling` (the cause ability carries the keyword) and
// a card predicate such as `nonLand` (matched on the cause's SOURCE card
// through the ordinary filter grammar). Any qualifier this helper does not
// recognise FAILS CLOSED: a cause spec it cannot evaluate must never admit
// the replacement (the repo's standing filter contract).
//
// Comma-separated alternatives are OR, matching ValidTgts$/ValidCause$
// semantics elsewhere.
func (e *Engine) drawCauseAdmits(spec string, source state.ObjID, ev events.Event) bool {
	cause := e.actionCause()
	if cause == 0 {
		return false
	}
	o := e.G.Obj(cause)
	if o == nil {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		if e.drawCauseTokenAdmits(strings.TrimSpace(alt), o, source) {
			return true
		}
	}
	return false
}

// drawCauseTokenAdmits evaluates ONE comma-separated token of a Draw
// ValidCause$ spec (drawCauseAdmits's per-alternative worker). It recognises
// a valid stack base, the shared controller / instant-sorcery qualifiers, the
// `Cycling` keyword qualifier and a card-predicate qualifier (evaluated
// against the cause's source card). Every other qualifier fails closed.
func (e *Engine) drawCauseTokenAdmits(token string, o *state.Object, source state.ObjID) bool {
	tok, ok := state.StackKindTokenOf(token)
	if !ok {
		return false
	}
	_, rest, _ := strings.Cut(strings.TrimSpace(token), ".")
	for q := range strings.SplitSeq(rest, "+") {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		switch q {
		case "YouCtrl", "OppCtrl", "Instant", "Sorcery":
			// Read by StackKindAdmits below.
		case "Cycling":
			if o.Ability == nil || !cyclingCauseKeywords[o.Ability.Params["Keyword"]] {
				return false
			}
		default:
			// A card-predicate qualifier (nonLand, a colour, a type word,
			// ...) on the cause's SOURCE card. Classify it with the SAME
			// shared recognizer UnknownPredicates uses, so an unrecognised
			// token fails closed rather than widening the match.
			src := e.G.Obj(o.Source)
			if src == nil {
				return false
			}
			pred := "Card." + q
			if len(effects.UnknownPredicates(pred)) != 0 {
				return false
			}
			if !e.matchesSpec(pred, src.ID, e.withNames(effects.SpecContext{
				You: e.controllerOf(source), Source: source,
			})) {
				return false
			}
		}
	}
	return state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindOf(e.G, o), o,
		o.Controller, e.controllerOf(source))
}

// drawNumberThisTurn counts p's draws in the current turn. The log is the
// replay-stable source of this per-turn fact; each player has its own ordinal
// because "their second card" must not count another seat's draw. Callers
// differ on whether the Draw currently being matched is already logged:
// trigger matching runs POST-emit (the event is in the log), while
// replacement matching runs PRE-emit (it is not), so a replacement matcher
// must add the pending draw's own applicability itself.
func (e *Engine) drawNumberThisTurn(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
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
func (e *Engine) firstCardInDrawStep(p state.PlayerID) bool {
	if e.G.Step != state.StepDraw {
		return false
	}
	draws := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.Draw && ev.Player == p {
			draws++
		}
		if ev.Kind == events.StepChange {
			return ev.Step == state.StepDraw && draws == 1
		}
	}
	return false
}

// pendingDrawIsFirstInDrawStep reports whether a Draw about to be logged for
// p is the first p draws since this turn entered its draw step. It is the
// pre-emit twin of firstCardInDrawStep: replacement matching runs from
// Engine.emit BEFORE the proposed Draw is appended to e.L.Events, so the
// pending event itself is the "next" draw (draw count 0) rather than a
// logged one (draw count 1). It requires p to be the ACTIVE player as well,
// because the exempt draw CR 504.1 grants is that player's own turn-based
// draw: a non-active player drawing during someone else's draw step is not
// the first one they draw in each of their own draw steps, so Notion Thief
// and Hullbreacher must still replace it. firstCardInDrawStep deliberately
// omits that active-player test (a trigger reads whoever drew); the two
// cannot share a body, so they are kept adjacent with identical log-scan
// shapes to stop the pair drifting.
func (e *Engine) pendingDrawIsFirstInDrawStep(p state.PlayerID) bool {
	if e.G.Step != state.StepDraw || p != e.G.Active {
		return false
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.Draw && ev.Player == p {
			return false
		}
		if ev.Kind == events.StepChange {
			return ev.Step == state.StepDraw
		}
	}
	return false
}

// extraDrawsThisTurn counts p's draws in the current turn that are NOT the
// CR 504.1 turn-based draw -- the first card p draws in p's OWN draw step
// while p is the active player. That is the draw Reed Richards' "except the
// first card you draw during each of your draw steps" clause exempts, so a
// FirstExtraCardDrawnThisTurn$ True replacement must apply only when this
// count is zero (CR 614.1a: one replacement per occasion, and only the first
// such occasion each turn).
//
// It runs from replacement matching, which is PRE-emit: the pending Draw is
// not yet in e.L.Events, so the caller adds the pending draw's own
// applicability separately (see pendingDrawIsFirstInDrawStep, the pre-emit
// twin of the exempt-draw test). The log -- not a mutable counter -- is the
// source of this per-turn fact, so cloning and replay rebuild it without an
// event-schema change.
func (e *Engine) extraDrawsThisTurn(p state.PlayerID) int {
	n := 0
	start := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if e.L.Events[i].Kind == events.TurnChange {
			start = i
			break
		}
	}
	// step is the step the scan is currently inside; 255 is the uint8 sentinel
	// for "before any StepChange this turn", which no real step equals.
	step := state.Step(255)
	drawsInStep := 0
	for i := start; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		switch ev.Kind {
		case events.StepChange:
			step = ev.Step
			if ev.Step == state.StepDraw {
				drawsInStep = 0
			}
		case events.Draw:
			if ev.Player != p {
				continue
			}
			if step == state.StepDraw && p == e.G.Active && drawsInStep == 0 {
				drawsInStep++
				continue
			}
			n++
		}
	}
	return n
}

// actionCause is the stack object whose resolving effect caused a synchronous
// action event. Costs are paid before an activated ability exists on the stack,
// so they deliberately have no cause and cannot satisfy ValidCause$. This is
// replay-safe: action triggers are checked synchronously inside emit, while
// the resolving object is still at the top of the replayed stack.
func (e *Engine) actionCause() state.ObjID {
	// causePin is an entry-settle preview's stand-in for the live action
	// cause (rules/entry_counters.go): the preview's Apply folds the entry
	// move, which takes the entrant off the cloned stack, so the bare read
	// would report no cause for a cast spell's own entry. Only a preview
	// engine ever carries one; live engines read the stack.
	if e.causePin != 0 {
		return e.causePin
	}
	if len(e.G.Stack) == 0 {
		return 0
	}
	return e.G.Stack[len(e.G.Stack)-1]
}

// causeSpecAdmits evaluates a CantSacrifice static's ValidCause$ stack spec
// (task vc-static1) against the in-flight sacrifice cause -- actionCause(),
// the resolving wrapper at the top of the stack for the whole effect-driven
// call. The rules COST sites never reach it: SacrificeBlocked's forCost
// callers skip every ValidCause-carrying static before this helper runs,
// because a cost payment has no causing object (actionCause would name
// whatever unrelated spell was already on the stack when the player paid --
// the exact misattribution discardCauseAdmits guards against with its
// IsDiscardCost check).
//
// The classifier is the shared StackKindTokenOf + StackKindAdmits pair the
// Discarded/Drawn cause matchers use, so the static path cannot drift from
// the trigger path. Two deliberate departures from the plural StackKindTokens
// helper, both fail closed: a comma alternative naming no stack kind (e.g.
// `Creature`) contributes nothing rather than falling into the plural
// helper's Spell-only default, and an alternative carrying a qualifier the
// classifier silently ignores (singleTarget, numTargets, ...) admits nothing
// rather than the widening the target-offer path documents -- a cause spec
// this build cannot evaluate exactly must never blanket-block a sacrifice.
// Comma alternatives are OR, matching ValidCause$ semantics elsewhere; the
// spec matches when SOME alternative admits the cause.
func (e *Engine) causeSpecAdmits(spec string, source state.ObjID) bool {
	cause := e.actionCause()
	if cause == 0 {
		return false
	}
	o := e.G.Obj(cause)
	if o == nil {
		return false
	}
	you := e.controllerOf(source)
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		tok, ok := state.StackKindTokenOf(alt)
		if !ok {
			continue // a non-stack alternative never contributes (fail closed)
		}
		if !causeSpecQualifiersKnown(alt) {
			continue // an unmodelled qualifier admits nothing (fail closed)
		}
		if state.StackKindAdmits([]state.StackKindToken{tok}, state.StackKindOf(e.G, o),
			o, o.Controller, you) {
			return true
		}
	}
	return false
}

// causeSpecQualifiersKnown reports whether every dot qualifier of one stack
// spec alternative is one the shared classifier actually reads. The
// classifier's own qualifier loop (state/stackkind.go StackKindTokenOf)
// silently DROPS an unknown qualifier -- sound for a target offer (the
// documented widening) but wrong for a cause restriction, where the dropped
// qualifier would widen the block. The known set is exactly what that loop
// consumes: YouCtrl/OppCtrl (controller scoping) and Instant/Sorcery (the
// Spell kind's card-type restriction).
func causeSpecQualifiersKnown(alt string) bool {
	_, rest, _ := strings.Cut(alt, ".")
	for q := range strings.SplitSeq(rest, ".") {
		switch q {
		case "", "YouCtrl", "OppCtrl", "Instant", "Sorcery":
		default:
			return false
		}
	}
	return true
}

// causeCostAdmits evaluates a CantSacrifice static's ValidCause$ spec on the
// COST path (task cantsac1) against the pending activation's cause. It is
// causeSpecAdmits' cost-side sibling and shares its fail-closed discipline,
// but not its input: a cost payment has no resolving stack object to
// classify (pushCast pays an ability's costs before its AbilityPush, and a
// mana ability never reaches the stack at all), so the cause comes from
// sacrificeBlockedForCost's caller, which knows what the payer is
// casting/activating.
//
// A cost site's cause is the ability the payment is made to (the cantsac1
// r2 semantics table on costCause): a spell cast (Spell), an ability
// activation (Activated), a ward or upkeep trigger's demand (Triggered) or
// an unless resolution election (Resolution). Those four -- None stays
// inadmissible, a no-cause payment names nothing -- are the readable
// grammar; every corpus ForCost$ True carrier is a bare `Spell,Activated`
// (angel_of_jubilation, yasharn_implacable_earth) and therefore scopes to
// the cast/activation sites only, never to a ward, unless or upkeep
// payment. A qualified base (Spell.Instant, Spell.OppCtrl) or any other
// base (SpellAbility, Ability) names a cause this path cannot exactly
// evaluate, so it fails closed -- the permissive direction for a
// restriction, and no corpus line is affected.
func causeCostAdmits(spec string, cause costCause) bool {
	if cause == costCauseNone || cause == costCauseResolution {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		if rest != "" {
			continue // a qualified cost cause is not modelled (fail closed)
		}
		switch base {
		case "Spell":
			if cause == costCauseSpell {
				return true
			}
		case "Activated":
			if cause == costCauseActivated {
				return true
			}
		case "Triggered":
			if cause == costCauseTriggered {
				return true
			}
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
func (e *Engine) exploitedMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Exploit || len(ev.IDs) == 0 || ev.IDs[0] == 0 {
		return false
	}
	ctrl := e.controllerOf(source)
	sc := e.specCtx(source, ctrl)
	if v := t.Params["ValidSource"]; v != "" &&
		!e.matchesSpec(v, ev.Obj, sc) {
		return false
	}
	if v := t.Params["ValidCard"]; v != "" &&
		!e.matchesSpec(v, ev.IDs[0], sc) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
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
func (e *Engine) evolvedMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Evolved {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	return true
}

// clashMatches implements Mode$ Clashed (CR 701.31: "Whenever you win/lose a
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
func (e *Engine) clashMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Clash {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidPlayer"]; v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
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
	registerTrigMatcher((*Engine).cycledMatches, "Cycled")
	registerTrigMatcher((*Engine).exploitedMatches, "Exploited")
	registerTrigMatcher((*Engine).exploresMatches, "Explores")
	registerTrigMatcher((*Engine).connivesMatches, "Connives")
	registerTrigMatcher((*Engine).investigatedMatches, "Investigated")
	registerTrigMatcher((*Engine).giveGiftMatches, "GiveGift")
	registerTrigMatcher((*Engine).evolvedMatches, "Evolved")
	registerTrigMatcher((*Engine).clashMatches, "Clashed")
	registerTrigMatcher((*Engine).chaosEnsuesMatches, "ChaosEnsues")
	registerTrigMatcher((*Engine).planeswalkedToMatches, "PlaneswalkedTo")
	registerTrigMatcher((*Engine).planeswalkedFromMatches, "PlaneswalkedFrom")
	registerTrigMatcher((*Engine).searchedLibraryMatches, "SearchedLibrary")
	registerTrigMatcher((*Engine).discoverMatches, "Discover")
	registerTrigMatcher((*Engine).seekAllMatches, "SeekAll")
	registerTrigMatcher((*Engine).surveilMatches, "Surveil")
	registerTrigMatcher((*Engine).scryMatches, "Scry")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.discardedMatches(t, source, ev)
	}, "Discarded")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.discardedAllMatches(t, source, ev)
	}, "DiscardedAll")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.drawnMatches(t, source, ev)
	}, "Drawn")
	effects.RegisterNonAPI("trig:DiscardedAll")
}
