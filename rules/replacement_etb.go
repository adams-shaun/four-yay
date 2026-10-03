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

// applyReplacementsDispatch is applyReplacements' common match-collection
// and per-event dispatch, shared by every replacement-eligible event kind
// (Moved/Untap/BeginPhase/Transform/ProduceMana/DamageDone). Factored out so
// applyReplacements can wrap a player-targeted Damage event with the
// repl:LifeReduced fallback above without duplicating this body.
// bloodthirstEntryMatch builds the synthetic Moved replacement a permanent
// with bloodthirst enters by (CR 702.54: "Bloodthirst N means 'If an
// opponent was dealt damage this turn, this permanent enters the battlefield
// with N +1/+1 counters on it.'"). The keyword is read from the entering
// object's DERIVED keyword list (derivedKeywordParam), so a printed
// K:Bloodthirst:<N> and a layer-6 `AddKeyword$ Bloodthirst:<N>` grant are
// ONE identical shape -- the grant path is the shape a cards-side expansion
// could never see (Twins of Discord; the primitive ratchet counts only
// Face.Primitives()'s printed-keyword walk, so the grant was previously a
// silent no-op the census could not even name).
//
// A fixed N gates on the existing CheckSVar$/SVarCompare$ pair -- the SVar
// name is an INLINE Count body (replacementCheckValue falls through to
// effects.EvalCount for a name no face SVar table defines), reading the
// DamageOppsTakenThisTurn head compared GT0. Bloodthirst X has no gate: the
// count IS the amount, so the body's CounterNum$ is the same inline Count
// body. A param that is neither a positive literal nor X (unmeasured in the
// corpus, all 23 printed lines spell <N> or X) fails closed to no match --
// the conservative direction for a counter put.
func (e *Engine) bloodthirstEntryMatch(ev events.Event) *replMatch {
	param, ok := e.derivedKeywordParamH(ev.Obj, kwhBloodthirst)
	if !ok {
		return nil
	}
	body := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined":     "Self",
		"CounterType": "P1P1",
		"ETB":         "True",
	}}
	r := &cards.Repl{Event: "Moved", Params: map[string]string{
		"Destination":       "Battlefield",
		"ValidCard":         "Card.Self",
		"ReplacementResult": "Updated",
		"Keyword":           "Bloodthirst",
		"KeywordLine":       "Bloodthirst:" + param,
	},
		With: body,
	}
	if param == "X" {
		body.Params["CounterNum"] = "Count$DamageOppsTakenThisTurn"
	} else {
		n, err := strconv.Atoi(param)
		if err != nil || n <= 0 {
			return nil
		}
		body.Params["CounterNum"] = strconv.Itoa(n)
		r.Params["CheckSVar"] = "Count$DamageOppsTakenThisTurn"
		r.Params["SVarCompare"] = "GT0"
	}
	return &replMatch{id: ev.Obj, repl: r}
}

// sunburstEntryMatch builds the synthetic Moved replacement a permanent with
// sunburst enters by (CR 702.47: "This object enters with a +1/+1 counter on
// it for each color of mana spent to cast it. If it isn't a creature, it
// instead enters with that many charge counters on it.").
//
// The keyword is read from the entering object's DERIVED keyword list
// (derivedKeywordParam), but this synthetic covers the layer-6
// `DB$ Animate | Keywords$ Sunburst` GRANT shape only (Solar Array, Lux
// Artillery): a PRINTED K:Sunburst line is expanded cards-side
// (cards/kw_sunburst.go) onto the face's own Repls, which the face-Repl scan
// above already collects, so the printed-face check below skips it -- a
// synthetic on top of the expansion would put the entry counters twice. The
// counter KIND follows Forge's own Sunburst expansion
// (CardFactoryUtil: `host.isCreature() ? P1P1 : CHARGE`), decided from the
// entering object's PRINTED face (CR 702.47a's "if it isn't a creature" is
// evaluated on the card's own types, ignoring type-changing effects), so a
// creature gets +1/+1 counters and an artifact gets charge counters.
//
// The count is the existing CR 107.4f converge head: the number of DISTINCT
// colours spent to cast the spell, carried on the object as ConvergeColours
// by the pay-time FlagConverged CastInfo (rules/cast.go's faceWantsConverge
// gate, widened to cover sunburst's cast faces). An inline Count body keeps
// this a one-line body with no SVar minted on the face.
func (e *Engine) sunburstEntryMatch(ev events.Event) *replMatch {
	if _, ok := e.derivedKeywordParamH(ev.Obj, kwhSunburst); !ok {
		return nil
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil {
		return nil
	}
	if o.Face().HasKeyword("Sunburst") {
		// Printed K:Sunburst: already expanded cards-side (cards/kw_sunburst.go);
		// the face-Repl scan collected it. The grant shape's printed face never
		// carries the line, so this gate admits only the granted case.
		return nil
	}
	kind := "CHARGE"
	if o.Face().IsCreature() {
		kind = "P1P1"
	}
	body := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined":     "Self",
		"CounterType": kind,
		"CounterNum":  "Count$Converge",
		"ETB":         "True",
	}}
	r := &cards.Repl{Event: "Moved", Params: map[string]string{
		"Destination":       "Battlefield",
		"ValidCard":         "Card.Self",
		"ReplacementResult": "Updated",
		"Keyword":           "Sunburst",
		"KeywordLine":       "Sunburst",
	}, With: body}
	return &replMatch{id: ev.Obj, repl: r}
}

// applyETBChoiceReplacement parks an entry before it is folded into the
// battlefield and asks through the ordinary mid-resolution suspension path.
// This is the CR 614.12 boundary: the chooser sees the question only when the
// permanent would enter, whether the move came from a resolving spell, a land
// play, reanimation or a search. etbMove/etbNext are the only transient
// continuation state; the answer itself is the existing Choose event.
func (e *Engine) applyETBChoiceReplacement(ev events.Event) bool {
	if ev.Kind != events.MoveZone || ev.To != state.ZBattlefield || e.pending != nil ||
		events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	ordinal := 0
	if e.etbMove != nil {
		if e.etbMove.Obj != ev.Obj {
			return false
		}
		ordinal = e.etbNext
	}
	choice, ok := e.entryETBChoice(ev, ordinal)
	if !ok {
		e.etbMove = nil
		e.etbNext = 0
		return false
	}
	move := ev
	e.etbMove = &move
	e.etbNext = ordinal + 1
	d := &decision.Decision{Player: e.G.Obj(ev.Obj).Controller, Kind: decision.KChoose,
		Min: 1, Max: 1, ResumeKind: "etb", Source: ev.Obj,
		Prompt: "Choose" + choice.promptText(), Options: choice.options}
	e.choosing = chooseETBEntry
	if in, ok := etbTapeAnswer(e, d, ev.Obj); ok {
		// The resolution kernel's answer in hand (W3 step 4c): the entry is
		// never parked. Re-emit it with the answer now, exactly as the legacy
		// chooseETBEntry arm does, and let the interrupted code carry on from
		// this emit -- the continuation the legacy arm hands back to the
		// parked frame (continueAfterETBEntry).
		e.choosing = chooseNone
		chosen := d.Chosen(in)
		e.withMintSink(e.pendingMintSink, func() { e.resumeETBEntry(chosen) })
		return true
	}
	e.Ask(d)
	return true
}

// applyRiotReplacement is retained as a defensive fallback for an entry that
// bypasses the shared ETB-choice walker. Ordinary entries are handled by
// applyETBChoiceReplacement above, so this path is not used by normal casts.
func (e *Engine) applyRiotReplacement(ev events.Event) bool {
	// A battlefield entry reached while another decision is outstanding must
	// not overwrite it (the orphaned-pending failure poseLifeReplacementChoice
	// already guards, and the same class findings-sol4 proved on the
	// life-replacement draw loop): let the entry happen verbatim rather than
	// parking on an ask that can never be posed. Unreachable in the corpus
	// today; the guard keeps a future caller from shipping the overwrite.
	if ev.To != state.ZBattlefield || e.riotMove != nil || e.pending != nil {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone == state.ZBattlefield || o.Face() == nil ||
		!o.Face().HasKeyword("Riot") || o.RiotChoice != "" {
		return false
	}
	// A face-down entry (manifest or cloak, CR 708.5) is a vanilla 2/2
	// creature: no riot choice is posed for it, and no public Choose "riot"
	// event may leak the hidden card. The guard MUST sit BEFORE the parking
	// assignment below -- a face-down entry that parked its move and then
	// returned false would leak a stale e.riotMove that is never emitted and
	// never cleared (chooseRiot's answer arm cannot fire for it), so every
	// later non-cast Riot entry would hit the parked-move guard above and
	// never ask again. The FaceDown state is folded by Apply's Move AFTER
	// this dispatch, so the incoming event's counter, not o.FaceDown, is
	// what names the face-down entry (the Siege guard's exact shape, in the
	// Siege guard's exact place -- before every return-past-parking; unleash
	// carries the identical guard in the identical place).
	if events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	move := ev
	e.riotMove = &move
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: o.ID, Prompt: "Choose how this creature enters (counter or haste)",
		Options: []decision.Option{
			{Index: 0, Kind: "riot", Label: "Enter with a +1/+1 counter", Obj: o.ID, Player: o.Controller},
			{Index: 1, Kind: "riot", Label: "Gain haste", Obj: o.ID, Player: o.Controller},
		}}
	e.choosing = chooseRiot
	e.ask(d)
	return true
}

// applySiegeProtector parks every non-cast Battle entry while its controller
// makes the protector choice. CR 310.10: "As a battle enters, its controller
// chooses an opponent to protect it; that player is its protector." The
// choice is a construct rule, not a card script -- none of the 37 real Battle
// cards carries a GenericChoice/ChosenMode script -- so the engine poses it
// here for every entry path, exactly as applyRiotReplacement does for Riot.
// The rule is general over the Battle type, not the Siege subtype: the
// protector is what the CR 310.7 combat defender and the effects-side
// OppProtect predicate read, so every Battle gets one rather than Siege
// alone (all 37 real corpus Battles happen to print the Siege subtype, so
// this is reachable today only for a synthetic or a future non-Siege card).
// The parked move is emitted after handleChoose logs the choice (a Choose
// "protector" event), so a log-only replay re-derives the protector from the
// same event stream. Only the controller's LIVING opponents are offered
// (protectorOpponents is the single eligibility home, shared with the
// re-derive after a protector leaves); a controller with no living opponent
// (a battle entering after everyone else lost -- unreachable in a real match)
// is recorded with no protector rather than parking on an unanswerable ask.
type attachedChoice struct {
	move   events.Event
	source state.ObjID
	stage  int
	// body is the replacement body's API (NameCard, ChooseCard, ChooseColor),
	// so the resume in rules/turn.go dispatches on the primitive that posed
	// the ask rather than on the card. NameCard carries two stages (name then
	// the paired creature type); ChooseCard and ChooseColor are single-stage.
	body string
}

// attachedBodyPoses reports whether an Attached replacement body is one this
// engine can honestly pose. It is the ONE eligibility home: applyAttachedReplacement
// selects only poseable bodies, and a body whose parameter shape cannot be
// honored is not selected, so it keeps today's untouched-Attach fallback
// rather than silently choosing an unrelated object or colour.
func attachedBodyPoses(sa *cards.SA) bool {
	if sa == nil {
		return false
	}
	switch sa.API {
	case "NameCard":
		return true
	case "ChooseCard":
		// The only corpus Attached ChooseCard is Pick-Axe's exiled-craft-card
		// pick; its pool must be the source's own exile association. Any other
		// DefinedCards$ role is a different pool this path does not read.
		return strings.EqualFold(effects.DefinedOf(sa).Cards.Text, "ExiledWith")
	case "ChooseColor":
		return true
	default:
		return false
	}
}

// applyAttachedReplacement handles the Attached replacement bodies that pose
// an election before the Attach applies: Psychic Paper's ChooseName
// (NameCard), Pick-Axe's exiled-craft-card ChooseCard, and Sanctuary Blade's
// ChooseColor. It parks the Attach before events.Apply and records the
// answers on the source; the resume in rules/turn.go releases the parked
// Attach exactly once through emitAttachedMove.
func (e *Engine) applyAttachedReplacement(ev events.Event) bool {
	if e.attachedChoice != nil || e.pending != nil || len(ev.IDs) == 0 {
		return false
	}
	var source state.ObjID
	var repl *cards.Repl
	e.forEachReplacementSource(func(id state.ObjID) {
		if source != 0 {
			return
		}
		f := e.replacementFace(id, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			// Key on the replacement body's own API: `ReplaceWith$ ChooseName`
			// resolves to an SVar whose body IS `DB$ NameCard` (never the SVar
			// name), and the siblings are `DB$ ChooseCard` / `DB$ ChooseColor`.
			// The API is the primitive that poses the ask, so a future card
			// reusing one of these bodies is covered by the same dispatch.
			if r.Event == "Attached" && attachedBodyPoses(r.With) && e.replacementMatches(*r, id, ev) {
				source, repl = id, r
				return
			}
		}
	})
	if source == 0 || repl == nil {
		return false
	}
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	ch := &attachedChoice{move: ev, source: source, body: repl.With.API}
	e.attachedChoice = ch
	switch repl.With.API {
	case "ChooseCard":
		return e.askAttachedCard(o, repl)
	case "ChooseColor":
		return e.askAttachedColor(o, repl)
	default: // NameCard
		return e.askAttachedName(o, repl)
	}
}

// askAttachedName poses the NameCard body's first stage (the name). It is the
// extracted Psychic Paper path, unchanged in behaviour.
func (e *Engine) askAttachedName(o *state.Object, repl *cards.Repl) bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	// ValidDescription$ rides along exactly as it does at the cast-time ETB
	// site (rules/cast.go): it is Forge prompt text, read by
	// effects.NameChoices only as a safety fallback when ValidCards$ is absent.
	opts := e.etbOptions(o.Controller, ch.source, "name", repl.With.ParamStr(cards.PKValidCards), repl.With.ParamStr(cards.PKValidDescription), "", "")
	if len(opts) <= 1 {
		if len(opts) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "name", Text: opts[0].Label})
		}
		return e.askAttachedType()
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose a creature card name", Options: opts}
	e.choosing = chooseAttached
	parkAsk(e, d)
	return true
}

// askAttachedCard poses the ChooseCard body's card ask over the pool its
// DefinedCards$ role names -- Pick-Axe's `DefinedCards$ ExiledWith`, the
// source's own ChangeZone exile association -- further restricted to the
// ChoiceZone$ set. The answer is recorded by the resume as the event-backed
// Choose "chosen" fold on the source (state.Object.Chosen), which is what
// `Defined$ ChosenCard` reads. A pool with no eligible card records nothing
// and releases the Attach (a mandatory choice that finds nothing legal is the
// fail-to-find shape, never a silent pick); a single eligible card is forced
// and recorded without an ask (the effChooseType strict-superset convention
// the sibling stages already use).
func (e *Engine) askAttachedCard(o *state.Object, repl *cards.Repl) bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	opts := e.attachedCardOptions(ch.source, repl)
	if len(opts) == 1 {
		e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "chosen", IDs: []state.ObjID{opts[0].Obj}})
		e.releaseAttachedChoice()
		return true
	}
	if len(opts) == 0 {
		e.releaseAttachedChoice()
		return true
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose an exiled card", Options: opts}
	e.choosing = chooseAttached
	parkAsk(e, d)
	return true
}

// attachedCardOptions builds the card options a ChooseCard Attached body
// offers: the source's ExiledWith association, filtered to the ChoiceZone$
// zones. The pool is read from the event-backed ExiledCards list, never from
// the shared exile zone, so a card this source did not exile is not offered.
func (e *Engine) attachedCardOptions(source state.ObjID, repl *cards.Repl) []decision.Option {
	src := e.G.Obj(source)
	if src == nil || !strings.EqualFold(effects.DefinedOf(repl.With).Cards.Text, "ExiledWith") {
		return nil
	}
	zones := attachedChoiceZones(repl.With.ParamStr(cards.PKChoiceZone))
	out := make([]decision.Option, 0, len(src.ExiledCards))
	for _, id := range src.ExiledCards {
		co := e.G.Obj(id)
		if co == nil || co.Face() == nil {
			continue
		}
		if zones != nil && !zones[co.Zone] {
			continue
		}
		out = append(out, decision.Option{Index: len(out), Kind: "card", Obj: id, Label: e.Name(id)})
	}
	return out
}

// attachedChoiceZones parses a ChooseCard body's ChoiceZone$ restriction into
// the zone set it names; a nil result means unrestricted. Unknown zone tokens
// contribute nothing (fail closed), never a widened pool.
func attachedChoiceZones(raw string) map[state.Zone]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[state.Zone]bool{}
	for z := range strings.SplitSeq(raw, ",") {
		switch strings.TrimSpace(z) {
		case "Battlefield":
			out[state.ZBattlefield] = true
		case "Hand":
			out[state.ZHand] = true
		case "Library":
			out[state.ZLibrary] = true
		case "Graveyard":
			out[state.ZGraveyard] = true
		case "Exile":
			out[state.ZExile] = true
		case "Stack":
			out[state.ZStack] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// askAttachedColor poses the ChooseColor body's colour ask (Sanctuary Blade's
// `Defined$ You`). The option list is the same WUBRG list the cast-time
// as-enters colour ask offers, so the two can never disagree about what a
// colour choice ranges over. The chooser is the source's controller (the
// corpus body's `Defined$ You`); the answer is recorded by the resume as the
// event-backed Choose "color" fold on the source (state.Object.ChosenColor).
func (e *Engine) askAttachedColor(o *state.Object, repl *cards.Repl) bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	opts := e.etbOptions(o.Controller, ch.source, "color", "", "", "", "")
	if len(opts) <= 1 {
		if len(opts) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "color", Text: etbColourLetter(opts[0].Label)})
		}
		e.releaseAttachedChoice()
		return true
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose a color", Options: opts}
	e.choosing = chooseAttached
	parkAsk(e, d)
	return true
}

// releaseAttachedChoice drops the parked Attach continuation and re-emits the
// stored Attach through emitAttachedMove. It is the ONE release site shared
// by the no-ask siblings of every body's poser.
func (e *Engine) releaseAttachedChoice() {
	ch := e.attachedChoice
	if ch == nil {
		return
	}
	move := ch.move
	e.attachedChoice = nil
	e.choosing = chooseNone
	e.emitAttachedMove(move)
}

func (e *Engine) askAttachedType() bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	o := e.G.Obj(ch.source)
	if o == nil {
		e.attachedChoice = nil
		return false
	}
	ch.stage = 1
	opts := e.creatureTypeOptions(o.Controller)
	if len(opts) <= 1 {
		if len(opts) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "type", Text: opts[0].Label})
		}
		move := ch.move
		e.attachedChoice = nil
		e.choosing = chooseNone
		e.emitAttachedMove(move)
		return true
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose a creature type", Options: opts}
	e.choosing = chooseAttached
	parkAsk(e, d)
	return true
}

func (e *Engine) emitAttachedMove(move events.Event) {
	e.attachedApplying = true
	e.emit(events.Event{Kind: events.Attach, Obj: move.Obj, IDs: append([]state.ObjID(nil), move.IDs...)})
	e.attachedApplying = false
}

// protectorOpponents lists the living opponents who may protect a battle
// whose controller is controller, in the deterministic seat order
// AliveFrom(0) yields. This is the ONE home for protector eligibility:
// applySiegeProtector's ask, its two-player auto-record and
// rechooseDepartedBattleProtector all read it, so eligibility cannot drift
// between the entry ask and the re-derive after a protector leaves.
// CR 310.10: a battle's controller chooses an opponent to be its protector;
// a player who has left the game is no longer an opponent.
func (e *Engine) protectorOpponents(controller state.PlayerID) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.G.AliveFrom(0) {
		if p == controller {
			continue
		}
		out = append(out, p)
	}
	return out
}

// rechooseDepartedBattleProtector gives every Battle whose recorded
// protector has just left the game a fresh living opponent as its protector
// (CR 310.10: "If a battle's protector leaves the game, that battle's
// controller chooses a new protector"). The choice rides the same Choose
// "protector" event the entry ask records, so the protector stays
// replay-derived and no new event kind or state field is introduced.
//
// The replacement is DERIVED, not re-posed: this runs from the PlayerLost
// event inside emit, which is very often mid-resolution or inside the
// state-based-action fixed point (a concession, a zero-life sweep, an
// empty-library draw), and the engine has no way to park a decision there.
// protectorOpponents is the deterministic fallback: the first living
// opponent in seat order. A controller with no living opponent is the last
// player in the game, so the stale protector is left in place rather than
// re-pointed at nobody.
func (e *Engine) rechooseDepartedBattleProtector(departed state.PlayerID) {
	for _, controller := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, controller) {
			o := e.G.Obj(id)
			if o == nil || !o.ProtectorValid || o.Protector != departed || o.Face() == nil || !o.Face().IsBattle() {
				continue
			}
			if next := e.protectorOpponents(o.Controller); len(next) > 0 {
				e.emit(events.Event{Kind: events.Choose, Obj: o.ID,
					Counter: "protector", Player: next[0]})
			}
		}
	}
}

func (e *Engine) applySiegeProtector(ev events.Event) bool {
	// Same overwrite guard applyRiotReplacement documents: never park on an ask
	// while another decision is outstanding.
	if ev.To != state.ZBattlefield || e.siegeMove != nil || e.pending != nil {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone == state.ZBattlefield || o.Face() == nil {
		return false
	}
	// A face-down entry is a vanilla 2/2 creature (CR 708.5), not a Battle;
	// the entry grant grants it no defense counters, so it must not be parked
	// on the CR 310.10 protector ask either -- and must not emit the
	// Choose "protector" event at all, which is not Secret and would name the
	// hidden card in the public transcript. The FaceDown state is folded by
	// Apply's Move AFTER this replacement dispatch runs, so the incoming
	// event's counter -- not o.FaceDown -- is what names the face-down entry.
	// events.IsFaceDownEntry is the shared predicate covering BOTH markers,
	// the manifest/FaceDown$ one and Cloak's, so this guard and Apply's own
	// fold cannot disagree about which entries are face down.
	if events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	if !o.Face().IsBattle() {
		return false
	}
	// A protector already recorded (a re-entering object keeps none -- Move
	// resets it -- but an object parked twice in one entry sequence must not
	// ask twice).
	if o.ProtectorValid {
		return false
	}
	var opts []decision.Option
	for idx, p := range e.protectorOpponents(o.Controller) {
		opts = append(opts, decision.Option{Index: idx, Kind: "protector",
			Label: e.G.Players[p].Name, Obj: o.ID, Player: p})
	}
	if len(opts) == 0 {
		return false
	}
	// Strict-supersets convention: a decision nobody could answer differently
	// is never posed. In a two-player game exactly one opponent is legal, so
	// record it through the same Choose "protector" event without an ask.
	if len(opts) == 1 {
		e.emit(events.Event{Kind: events.Choose, Obj: o.ID,
			Counter: "protector", Player: opts[0].Player})
		return false
	}
	move := ev
	e.siegeMove = &move
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose,
		Min: 1, Max: 1, Source: o.ID,
		Prompt:  "Choose an opponent to protect this battle",
		Options: opts}
	e.choosing = chooseSiege
	e.ask(d)
	return true
}

// etbTapeAnswer serves an as-enters choice from the tape. The resolving
// spell's own entry is the last thing its resolution does; an entry an
// effect makes mid-chain (a reanimation, a mass return, a token mint) is a
// park-and-continue ask (W3 step 5, lasagna spec §7.2): the legacy park lets
// the moving effect keep running past the parked entry, while the kernel
// answers it in place and the entry happens where the effect made it.
func etbTapeAnswer(e *Engine, d *decision.Decision, obj state.ObjID) (decision.Intent, bool) {
	n := len(e.G.Stack)
	if n > 0 && e.G.Stack[n-1] == obj {
		return e.TapeAnswer(d)
	}
	return parkTapeAnswer(e, d)
}
