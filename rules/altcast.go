// altcast.go implements the alternative-cost keyword family: Evoke, Dash,
// Overload, Warp, Madness and Encore, plus the AlternateAdditionalCost
// either-or additional cost. The casting options themselves live where the
// other cast modes do (rules/legal.go offers, rules/cast.go's beginCast
// switch); this file owns the post-cast machinery the keywords imply:
//
//   - Evoke (CR 702.79a): a mandatory ETB follow-up is queued by
//     altCostEnter and KeywordTriggerPush mints its builtin sacrifice as a
//     real respondable triggered ability. The MH3 ExileFromHand costs are
//     paid as Exile cost parts during the cast itself.
//   - Dash (CR 702: "it gains haste, and it's returned from the battlefield
//     to its owner's hand at the beginning of the next end step"): the haste
//     is a UntilEOT layer-6 continuous grant; the return is a Mode$ Phase
//     delayed trigger registered here and resolved through the ordinary
//     DelayedPush machinery with the __kwDashReturn builtin SVar
//     (cards/link.go).
//   - Blitz (CR 702.152c-d): "it gains haste and 'When this creature dies,
//     draw a card.' Sacrifice it at the beginning of the next end step." The
//     haste is the Dash UntilEOT grant, the dies-draw is a runtime-granted
//     AddTrigger$ (rules/blitz.go's blitzEnter, the builtin __kwBlitzDraw
//     body) and the sacrifice is a delayed trigger with the builtin
//     __kwBlitzSacrifice body.
//   - Warp: "exile this creature at the beginning of the next end step,
//     then you may cast it from exile on a later turn" -- a delayed trigger
//     registers the exile (__kwWarpExile), and legal.go's exile walk offers
//     the recast only while the most recent exile transition is the move
//     caused by that exact delayed trigger.
//   - Madness (CR 702.35a-b): discarding normally proposes a hand-to-graveyard
//     move. The card's owner may replace that move with exile; doing so queues
//     a genuine keyword-triggered ability. Players may respond to or counter
//     that ability, and only as it resolves does the owner choose whether to
//     cast the card for its madness cost or put it into the graveyard.
//
// All of it is rules-layer on purpose: the pieces need ParseCost, payMana,
// AddContinuous and the pendingTrigger queue, none of which effects may
// reach, and every step is event-emitted (or queue-appended, the same
// memory-only channel checkTriggers already uses) so a replayed game
// re-derives it identically.
package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// altCostEnter is called from checkTriggers for every MoveZone onto the
// battlefield. Three keyword follow-ups key off the CastFlags the spell
// carried onto the permanent (a zone change preserves them from the stack):
// an evoked creature queues its mandatory sacrifice trigger, a dashed
// creature gains haste until end of turn and registers its end-step return,
// and a warped creature registers its end-step exile. Each delayed
// registration is its own DelayedRegister event, so replay rebuilds it.
//
// Called inside emit (checkTriggers's own context), so the ClockTick
// AddContinuous emits for the haste grant lands between the entering
// MoveZone and whatever follows -- the same nesting emit's Note re-entrancy
// already tolerates.
func (e *Engine) altCostEnter(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil {
		return
	}
	if int(o.Controller) >= len(e.G.Players) || e.G.Players[o.Controller].Lost {
		return
	}
	if o.CastFlags&state.FlagPromisedGift != 0 && o.Face().IsPermanent() &&
		cards.ResolveSVar(o.Face().SVars, "GiftAbility") != nil {
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source: ev.Obj, Controller: o.Controller, Gift: true,
			// CR 702.168c: the promised receiver is snapshotted NOW, while
			// the entering object still carries GiftPromisedTo. The trigger
			// resolves after its source may have left the battlefield (CR
			// 112.7a) -- the response window its respondability creates --
			// and events.Move clears the live promise on any zone change, so
			// the receiver rides the push payload instead of being re-read at
			// resolution.
			GiftTo: o.GiftPromisedTo,
			Ctx:    effects.Ctx{Source: ev.Obj, Controller: o.Controller},
		})
	}
	if o.CastFlags&state.FlagEvoked != 0 {
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     ev.Obj,
			Controller: o.Controller,
			Evoke:      true,
			Ctx: effects.Ctx{
				Source:     ev.Obj,
				Controller: o.Controller,
			},
		})
	}
	if o.CastFlags&state.FlagDashed != 0 {
		// CR 702: a dashed spell gains haste. A layer-6 grant scoped to the
		// object itself (the effPump shape), UntilEOT: it leaves with the
		// permanent at the next end step anyway, and the turn boundary
		// drops the grant for the pathological case that it stays.
		e.AddContinuous(state.ContinuousEffect{
			Source: ev.Obj, Affects: "Card.Self", Controller: o.Controller,
			Layer: state.LAbilities, AddKeywords: []string{"Haste"}, UntilEOT: true,
		})
		e.emit(events.Event{Kind: events.DelayedRegister, Obj: ev.Obj,
			Player: o.Controller, Step: state.StepEnd, Counter: "__kwDashReturn"})
	}
	if o.CastFlags&state.FlagBlitzed != 0 {
		// CR 702.152c-d: a blitzed creature gains haste and the dies-draw
		// trigger, and is sacrificed at the beginning of the next end step.
		e.blitzEnter(ev.Obj, o.Controller)
	}
	if o.CastFlags&state.FlagWarped != 0 {
		e.emit(events.Event{Kind: events.DelayedRegister, Obj: ev.Obj,
			Player: o.Controller, Step: state.StepEnd, Counter: "__kwWarpExile"})
	}
	// Unearth (CR 702.84a, rules/unearth.go): the activation is a graveyard
	// ability, not a cast, so there is no CastFlags bit to read -- the
	// entered_unearthed Counter effects/zone.go stamped on the MoveZone is
	// the provenance. The hook grants haste and registers the end-step exile.
	if ev.Counter == events.UnearthEntryCounter {
		e.unearthEnter(ev.Obj)
	}
	if o.CastFlags&state.FlagMayFlashSac != 0 {
		// K:MayFlashSac (CR 702.8): cast off-sorcery through the keyword's
		// flash permission, so the permanent it became is sacrificed at the
		// beginning of the next cleanup step. The pay-time flag is the
		// replayable provenance; a sorcery-timed cast of the same card
		// carries none and registers nothing.
		e.mayFlashSacEnter(ev.Obj, o.Controller)
	}
}

// escapeCost is id's Escape cost (CR 702.42a): the printed K:Escape
// parameter, or the keyword a continuous-effect grant delivered (Underworld
// Breach's "each nonland card in your graveyard has escape" AddKeyword$
// grant). The granted text names the card's own mana cost with the
// CardManaCost placeholder token (Forge's CardManaCost property in the grant
// line); the printed text spells the mana symbols out. Both end in
// ExileFromGrave<N/Spec> parts ParseCost already models. A cost this parse
// cannot price (the X-exile form ExileFromGrave<X/Card.Other+withTypesGE4>,
// one corpus line) is never offered rather than charged wrong.
func (e *Engine) escapeCost(id state.ObjID) (Cost, bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}, false
	}
	raw, ok := e.derivedKeywordParam(id, "Escape")
	if !ok {
		return Cost{}, false
	}
	var toks []string
	for tok := range strings.FieldsSeq(raw) {
		if strings.EqualFold(tok, "CardManaCost") {
			toks = append(toks, strings.Fields(o.Face().ManaCost)...)
			continue
		}
		toks = append(toks, tok)
	}
	c := ParseCost(strings.Join(toks, " "))
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

func (e *Engine) madnessReplacementApplies(ev events.Event) bool {
	if ev.To != state.ZGraveyard || !events.IsDiscard(ev) {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil {
		return false
	}
	_, ok := o.Face().KeywordParam("Madness")
	return ok
}

func (e *Engine) parkMadnessDiscard(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil {
		return
	}
	owner := o.Owner
	if int(owner) >= len(e.G.Players) || e.G.Players[owner].Lost {
		e.applyingMadnessChoice = true
		e.emit(ev)
		e.applyingMadnessChoice = false
		return
	}
	e.madnessChoices = append(e.madnessChoices, ev)
	if e.pending == nil {
		e.askMadnessReplacement(owner)
	}
}

func (e *Engine) askMadnessReplacement(owner state.PlayerID) {
	ev := e.madnessChoices[0]
	name := "this card"
	if o := e.G.Obj(ev.Obj); o != nil && o.Face() != nil {
		name = o.Face().Name
	}
	d := &decision.Decision{Player: owner, Kind: decision.KReplacement,
		Min: 1, Max: 1, Source: ev.Obj,
		Prompt: "Exile " + name + " instead of discarding it to use madness?",
		Options: []decision.Option{
			{Index: 0, Kind: "madness_exile", Label: "Exile it (madness)", Obj: ev.Obj},
			{Index: 1, Kind: "madness_graveyard", Label: "Discard it normally", Obj: ev.Obj},
		}}
	// A discard made by a RESOLVING spell or ability (Kindle the Carnage's
	// random discard, a Repeat body) must suspend that resolution on the
	// madness ask, exactly as askReplacementChoice and tokenElectionAsk do.
	// A bare e.ask left the effect chain running past the park: the next
	// sub-ability's own question (Kindle's "Repeat this process?") went
	// through Engine.Ask and displaced the madness ask, whose answer was
	// then never taken -- the parked discard never moved, the card stayed
	// in hand and was "discarded" again on every repeat, and the bot
	// repeated forever (cardfuzz batch8 line 1). Through Engine.Ask the
	// effects.Resolve loops record their continuation, and the last madness
	// answer resumes it (handleMadnessReplacement). A pose already under a
	// resume record, or with nothing resolving, keeps the plain ask.
	if e.resume == nil && e.pending == nil && (e.resolvingObj != 0 || e.applyingReplacement) {
		d.ResumeKind = "replacement"
		e.Ask(d)
		e.madnessSuspended = e.resume != nil
		return
	}
	e.ask(d)
}

func (e *Engine) handleMadnessReplacement(d *decision.Decision, in decision.Intent) {
	if len(e.madnessChoices) == 0 {
		return
	}
	ev := e.madnessChoices[0]
	e.madnessChoices = e.madnessChoices[1:]
	chosen := d.Chosen(in)
	if len(chosen) == 1 && chosen[0].Kind == "madness_exile" {
		ev.To = state.ZExile
		ev.Text = "discarded (madness exile)"
	}
	e.applyingMadnessChoice = true
	e.emit(ev)
	e.applyingMadnessChoice = false
	e.askNextReplacementChoice()
	// The last madness answer of a queue whose first ask suspended a
	// resolution resumes it (askMadnessReplacement). While more choices are
	// outstanding the frame stays parked on e.resume.
	if e.madnessSuspended && e.pending == nil && len(e.madnessChoices) == 0 && len(e.replChoices) == 0 {
		e.madnessSuspended = false
		// The token election's answer tail is the same settle: resume the
		// frame still parked on e.resume once nothing else is outstanding.
		e.settleTokenElection(e.resume)
	}
}

// offerMadness queues the triggered ability created by accepting madness's
// optional discard replacement (CR 702.35a-b). The accepted replacement emits
// a marked hand-to-exile move; matching both that marker and the zone pair
// prevents an unrelated exile from creating the cast window.
func (e *Engine) offerMadness(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil {
		return
	}
	if _, ok := o.Face().KeywordParam("Madness"); !ok {
		return
	}
	if ev.Text != "discarded (madness exile)" {
		return
	}
	owner := o.Owner
	if int(owner) >= len(e.G.Players) || e.G.Players[owner].Lost {
		return
	}
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source:     ev.Obj,
		Controller: owner,
		Madness:    true,
		Ctx: effects.Ctx{
			Source:     ev.Obj,
			Controller: owner,
		},
	})
}

// castMadness is the yes answer while the respondable madness ability is
// resolving. The ability has already left the stack; the card must still be
// in the exile zone the replacement put it in. beginCast's ordinary flow
// charges the printed madness cost and records the spell on the stack.
func (e *Engine) castMadness(p state.PlayerID, id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZExile || o.Owner != p {
		return false
	}
	e.beginCast(p, decision.Option{Kind: "cast", Obj: id, Mode: "madness"})
	return true
}

func (e *Engine) askMadnessCast(ability *state.Object) bool {
	if ability == nil {
		return false
	}
	card := e.G.Obj(ability.Source)
	if card == nil || card.Zone != state.ZExile || card.Face() == nil ||
		int(card.Owner) >= len(e.G.Players) || e.G.Players[card.Owner].Lost {
		return false
	}
	cost, ok := keywordAltCost(card.Face(), "Madness")
	if !ok {
		return false
	}
	name := card.Face().Name
	d := &decision.Decision{Player: card.Owner, Kind: decision.KTriggerOptional,
		Min: 1, Max: 1, Source: card.ID, ResumeKind: "madness",
		Prompt: "Cast " + name + " for its madness cost?"}
	if e.offerCastable(card.Owner, card.ID, cost, spellScope("madness"), false) {
		raw, _ := card.Face().KeywordParam("Madness")
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "yes",
			Label: "Cast " + name + " for " + raw, Obj: card.ID})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "no",
		Label: "Put " + name + " into its owner's graveyard", Obj: card.ID})
	e.ask(d)
	e.resume = &resumePoint{kind: "madness", obj: ability.ID}
	return true
}

func (e *Engine) resolveMadnessChoice(rp *resumePoint, yes bool) {
	ability := e.G.Obj(rp.obj)
	if ability == nil {
		return
	}
	card, owner := ability.Source, ability.Controller
	e.finishResumption(rp.obj)
	if yes && e.castMadness(owner, card) {
		return
	}
	e.madnessDeclined(card)
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
}

func (e *Engine) madnessDeclined(id state.ObjID) {
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZExile {
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone,
			To: state.ZGraveyard, Text: "madness not cast"})
	}
}

// warpRecastAvailable reports whether the warp card id in exile may be cast
// from exile on a later turn (CR 702: "exile this creature at the beginning
// of the next end step, then you may cast it from exile on a later turn").
// It is derived entirely from the log, never from mutable per-object state,
// because the FlagWarped that would carry the provenance resets when the
// permanent left the battlefield at the exile itself. The shape it looks
// for, walking backwards from the log's end:
//
//  1. the most recent MoveZone taking id to exile (the end-step warp exile),
//  2. at least one TurnChange strictly after it ("on a later turn"),
//  3. a DelayedPush of id before that exile (the end-step trigger fired it),
//  4. a CastInfo of id flagged warped before that DelayedPush (it was
//     warp-cast).
//
// A card exiled by anything other than its own warp trigger fails step 3 and
// gets no offer. Everything is a fixed-order walk of the log, so a replayed
// game derives the same answer.
// warpGraveyardAllowed is the separate permission to use Warp from a
// graveyard. Warp itself grants hand use only; Timeline Culler is the one
// corpus card whose Continuous static explicitly grants Spell.Warp from its
// own graveyard.
func warpGraveyardAllowed(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, st := range f.Statics {
		if st.Mode == "Continuous" && st.Params["MayPlay"] == "True" &&
			strings.Contains(st.Params["ValidSA"], "Spell.Warp") &&
			strings.Contains(st.Params["AffectedZone"], "Graveyard") &&
			strings.Contains(st.Params["EffectZone"], "Graveyard") {
			return true
		}
	}
	return false
}

func (e *Engine) warpRecastAvailable(id state.ObjID) bool {
	log := e.L.Events
	exileIdx := -1
	for i := len(log) - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZExile {
			exileIdx = i
			break
		}
	}
	if exileIdx < 0 {
		return false
	}
	laterTurn := false
	for i := exileIdx + 1; i < len(log); i++ {
		if log[i].Kind == events.TurnChange {
			laterTurn = true
			break
		}
	}
	if !laterTurn {
		return false
	}
	pushIdx := -1
	for i := exileIdx - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind == events.MoveZone && ev.Obj == id {
			// The latest exile is entitled only when no incarnation/zone move
			// intervenes between the warp trigger and that exile. An unrelated
			// later exile therefore cannot reuse historical warp provenance.
			return false
		}
		if ev.Kind == events.DelayedPush && ev.Obj == id && ev.Counter == "__kwWarpExile" {
			pushIdx = i
			break
		}
	}
	if pushIdx < 0 {
		return false
	}
	for i := pushIdx - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind == events.CastInfo && ev.Obj == id &&
			events.FlagsFrom(ev.Counter)&state.FlagWarped != 0 {
			return true
		}
	}
	return false
}

// foretellCastAvailable reports whether the foretold card id in exile may be
// cast from exile on a later turn (CR 702.126a: "Cast it on a later turn for
// its foretell cost"). It is derived entirely from the log, never from
// mutable per-object state, because the FlagForetold the {2} action stamps
// onto the exiled card says WHICH provenance but cannot say WHEN -- the same
// log-scan shape warpRecastAvailable takes. The shape it looks for, walking
// backwards from the log's end:
//
//  1. the most recent MoveZone taking id to exile (the {2} action's own
//     hand->exile move),
//  2. a CastInfo of id flagged foretold before that exile, with no
//     intervening move of id between the two (only the action emits both,
//     back to back -- a card exiled by anything else fails here),
//  3. at least one TurnChange strictly after the exile ("on a later turn").
//
// A foretold card already cast (the later cast's own flag rides its
// resolution) re-enters this scan only if it is back in exile, and any move
// of id out of exile between the CastInfo and the latest exile -- the
// resolution's own stack move -- fails step 2, so a re-exiled foretell card
// never inherits the old provenance. Everything is a fixed-order walk of the
// log, so a replayed game derives the same answer.
// mayhemCastCost is id's Mayhem cast cost (the Doom Prevails keyword): the
// K:Mayhem colon parameter is the ALTERNATIVE mana cost the graveyard cast
// pays in place of the card's mana cost (Abomination, World Ravager's
// "for {4}{R}"). The bare parameterless K:Mayhem -- Oscorp Industries, the
// one corpus line -- is the separate "you may PLAY this card from your
// graveyard" land shape, not a cast, so it is withheld here. A cost the
// parser cannot price (no corpus carrier today) is withheld rather than
// charged wrong, the escapeCost convention. Read off the DERIVED keyword
// list, so a continuous-effect grant would count exactly where a printed
// K:Mayhem line does.
func (e *Engine) mayhemCastCost(id state.ObjID) (Cost, bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}, false
	}
	raw, ok := e.derivedKeywordParam(id, "Mayhem")
	if !ok || strings.TrimSpace(raw) == "" {
		return Cost{}, false
	}
	// Norman Osborn, Green Goblin grants Mayhem:CardManaCost. Expand that
	// Forge placeholder before parsing, exactly as escapeCost does for
	// Underworld Breach's granted Escape cost.
	var toks []string
	for tok := range strings.FieldsSeq(raw) {
		if strings.EqualFold(tok, "CardManaCost") {
			toks = append(toks, strings.Fields(o.Face().ManaCost)...)
			continue
		}
		toks = append(toks, tok)
	}
	c := ParseCost(strings.Join(toks, " "))
	if len(c.Unknown) > 0 || c.X > 0 {
		return Cost{}, false
	}
	return c, true
}

// mayhemDiscardedThisTurn is Mayhem's provenance gate ("if you discarded it
// this turn"): some events.IsDiscard move of id since the last TurnChange
// naming p as the discarder -- the ordinary discard by its Player field, the
// cost form (events.DiscardCost carries no Player) by the card's owner,
// since a cost discard is paid from the payer's own hand (CR 118.2a, the
// same read CardsDiscardedThisTurn takes). Log-derived so a replayed game
// derives the same answer, like warpRecastAvailable and
// foretellCastAvailable; the last TurnChange bounds the window, so a card
// discarded LAST turn has no offer even while it still sits in the
// graveyard.
func (e *Engine) mayhemDiscardedThisTurn(p state.PlayerID, id state.ObjID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			return false
		}
		if ev.Kind != events.MoveZone || ev.Obj != id || !events.IsDiscard(ev) {
			continue
		}
		if events.IsDiscardCost(ev) {
			if o := e.G.Obj(id); o != nil && o.Owner == p {
				return true
			}
			continue
		}
		if ev.Player == p {
			return true
		}
	}
	return false
}

func (e *Engine) foretellCastAvailable(id state.ObjID) bool {
	log := e.L.Events
	exileIdx := -1
	for i := len(log) - 1; i >= 0; i-- {
		if ev := log[i]; ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZExile {
			exileIdx = i
			break
		}
	}
	if exileIdx < 0 {
		return false
	}
	// Effect-granted foretelling uses one MoveZone event whose counter carries
	// the designation; it must survive a battlefield departure's flag reset.
	if log[exileIdx].Counter == "exiled_with_face_down_foretold" {
		for i := exileIdx + 1; i < len(log); i++ {
			if log[i].Kind == events.TurnChange {
				return true
			}
		}
		return false
	}
	foretoldIdx := -1
	for i := exileIdx - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind == events.MoveZone && ev.Obj == id {
			// An intervening move between the flag stamp and this exile:
			// the exile is not the foretell action's own, whatever preceded it.
			return false
		}
		if ev.Kind == events.CastInfo && ev.Obj == id &&
			events.FlagsFrom(ev.Counter)&state.FlagForetold != 0 {
			foretoldIdx = i
			break
		}
	}
	if foretoldIdx < 0 {
		return false
	}
	for i := exileIdx + 1; i < len(log); i++ {
		if log[i].Kind == events.TurnChange {
			return true
		}
	}
	return false
}
