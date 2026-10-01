// Triggers granted by a static rather than printed on the face.
//
// Conspire, Dethrone, Afflict and Ward reach the stack through a granting static,
// so they are checked here rather than through the per-mode matchers.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// checkGrantedConspireTriggers synthesizes Conspire's copy trigger (CR
// 702.78a's second ability) for a spell that currently HAS the keyword but
// does not print it: a layer-6 grant (Wort, the Raidmother's "each red or
// green instant or sorcery spell you cast has conspire", Raiding Schemes'
// noncreature arm) gives the spell the same rules text as a printed keyword,
// and the printed K:Conspire expansion (cards/keywords.go) only covers
// printed lines. Without this walk the granted spell's cast flow still
// offers the conspired cast, still asks the tap and still pays it (the
// derived-keyword read the offer uses), but nothing copies -- the player pays
// an unrecoverable cost for nothing. The synthesized trigger reuses the
// printed expansion's exact trigger/body shape (Mode$ SpellCast, ValidCard$
// Card.Self, TriggerZones$ Stack; DB$ CopySpellAbility over
// Defined$ TriggeredSpellAbility with Amount$ Count$Conspired), so its
// behaviour is byte-identical to the printed path's: it fires on EVERY cast
// of the granted spell (the deferred CR 601.2i walk, so the pay-time
// FlagConspired CastInfo has already folded Object.Conspired) and resolves
// to a no-op when the tap was declined (Amount 0, effCopySpellAbility's
// loop emits nothing). A copy emits StackCopy, not PutOnStack, so the
// synthesis cannot double-fire on its own output. The walk skips a face that
// PRINTS Conspire (the printed expansion already owns the line -- the same
// grant-identical-to-a-printed-line dedup Afflict keeps). Like Dethrone's
// synthesis this is a read-only derived-characteristics check; granting
// stays in the continuous-effect system. It runs on the faceMayTrigger
// early-return path too (a granted keyword is independent of printed
// triggers, the same shape Dethrone/Afflict are), and its gate is ordered
// cheap-first -- event kind, then object identity, one integer compare each
// on the hot per-event walk -- before the derived keyword scan allocates.
func (e *Engine) checkGrantedConspireTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	if ev.Kind != events.PutOnStack || id != ev.Obj {
		return
	}
	if !e.hasKeywordH(id, kwhConspire) || f.HasKeyword("Conspire") {
		return
	}
	t := cards.Trigger{Mode: "SpellCast", Params: map[string]string{
		"Mode": "SpellCast", "ValidCard": "Card.Self", "TriggerZones": "Stack", "TriggerDescription": "Conspire",
	}, Effect: &cards.SA{Kind: "DB", API: "CopySpellAbility", Params: map[string]string{
		"Defined": "TriggeredSpellAbility", "Amount": "Count$Conspired", "MayChooseTarget": "True",
	}}}
	if observer.triggerMatches(t, id, ev, objLKI) {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] < maxTriggerFires {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     id,
				Controller: o.Controller,
				// The Ward shape: the body rides the push's __kwConspire:
				// payload for events.Apply to rebuild structurally -- a raw
				// SA cannot cross the log, and the TriggerPush -1 index
				// sentinel is Dethrone's own. The cast spell rides
				// Remembered because Defined$ TriggeredSpellAbility reads
				// the triggering spell off it.
				Conspire: true,
				Ctx: effects.Ctx{
					Source:         id,
					Controller:     o.Controller,
					Remembered:     triggerRemembered(ev, id),
					LKI:            objLKI,
					TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
				},
			})
		}
	}
}

// checkGrantedDemonstrateTriggers synthesizes Demonstrate's copy trigger
// (CR 702.152) for a spell that currently HAS the keyword but does not print
// it: a layer-6 grant (Silverquill Lecturer's "Creature spells you cast have
// demonstrate", The Twelfth Doctor's non-hand spell grant, Try-My-Deck
// Elemental's commander grant, the Strixhaven plane's instant/sorcery grant)
// gives the spell the same rules text as a printed keyword, and the printed
// K:Demonstrate expansion (cards/kw_demonstrate.go) only covers printed
// lines. Without this walk the granted spell's trigger never fires -- the
// keyword grant reaches the derived keyword list but no trigger exists for
// it. The synthesized trigger reuses the printed expansion's exact
// trigger/body shape (Mode$ SpellCast, ValidCard$ Card.Self,
// TriggerZones$ Stack; DB$ Demonstrate over Defined$ TriggeredSpellAbility),
// so its behaviour is byte-identical to the printed path's: the may-copy
// election and the opponent choice are the body's own asks
// (effects/demonstrate.go). A copy emits StackCopy, not PutOnStack, so the
// synthesis cannot double-fire on its own output. The walk skips a face that
// PRINTS Demonstrate (the printed expansion already owns the line -- the
// same grant-identical-to-a-printed-line dedup Conspire keeps). Like
// Conspire's synthesis this is a read-only derived-characteristics check;
// granting stays in the continuous-effect system. It runs on the
// faceMayTrigger early-return path too (a granted keyword is independent of
// printed triggers, the same shape Conspire is), and its gate is ordered
// cheap-first -- event kind, then object identity, one integer compare each
// on the hot per-event walk -- before the derived keyword scan allocates.
func (e *Engine) checkGrantedDemonstrateTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	if ev.Kind != events.PutOnStack || id != ev.Obj {
		return
	}
	if !e.hasKeywordH(id, kwhDemonstrate) || f.HasKeyword("Demonstrate") {
		return
	}
	t := cards.Trigger{Mode: "SpellCast", Params: map[string]string{
		"Mode": "SpellCast", "ValidCard": "Card.Self", "TriggerZones": "Stack", "TriggerDescription": "Demonstrate",
	}, Effect: &cards.SA{Kind: "DB", API: "Demonstrate", Params: map[string]string{
		"Defined": "TriggeredSpellAbility",
	}}}
	if observer.triggerMatches(t, id, ev, objLKI) {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] < maxTriggerFires {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     id,
				Controller: o.Controller,
				// The Conspire shape: the body rides the push's
				// __kwDemonstrate: payload for events.Apply to rebuild
				// structurally -- a raw SA cannot cross the log, and the
				// TriggerPush -1 index sentinel is this synthesis's own. The
				// cast spell rides Remembered because Defined$
				// TriggeredSpellAbility reads the triggering spell off it.
				Demonstrate: true,
				Ctx: effects.Ctx{
					Source:         id,
					Controller:     o.Controller,
					Remembered:     triggerRemembered(ev, id),
					LKI:            objLKI,
					TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
				},
			})
		}
	}
}

// checkCipherTriggers synthesizes Cipher's combat-damage trigger (CR 702.99b)
// for the creature that just dealt combat damage to a player, when that
// creature has one or more ENCODED cards. Unlike every checkGranted* walk
// there is no keyword to read: the association is runtime state
// (state.Object.EncodedCards, written by effCipher and folded by the Imprint
// kind's "encoded" discriminator).
//
// It is driven directly from checkTriggers -- NOT from the per-object face
// walk -- because a Damage event dealt to a PLAYER has no object recipient
// (ev.Obj == 0), so the live walk's zone-skip path would not visit the
// dealing creature at all (trigMustVisit names only ev.Obj/ev.IDs/ev.Pairs).
// The dealer is e.damaging, the same source resolution damageEventSource
// uses, and e.combatDamaging is the flag the combat assignment loop sets.
// The scan is over battlefield permanents that actually carry an encoded
// card, and the gate is exact: COMBAT damage (e.combatDamaging), dealt BY
// that creature (e.damaging == id), to a PLAYER (ev.Obj == 0, ev.Player a
// seat). A non-combat Damage event never fires it, and a hit redirected onto
// a permanent (ev.Obj != 0) is not combat damage to a player. The trigger's
// controller is the creature's CURRENT controller, which is what "its
// controller may cast" means.
func (e *Engine) checkCipherTriggers(ev events.Event) {
	if ev.Obj != 0 || ev.Amount <= 0 || !e.combatDamaging || e.damaging == 0 {
		return
	}
	if int(ev.Player) >= len(e.G.Players) {
		return
	}
	id := e.damaging
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || len(o.EncodedCards) == 0 {
		return
	}
	if int(o.Controller) >= len(e.G.Players) || e.G.Players[o.Controller].Lost {
		return
	}
	key := triggerKey{Source: id, Idx: -1}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	for _, card := range o.EncodedCards {
		c := e.G.Obj(card)
		if c == nil || c.Zone != state.ZExile {
			// Defensive: Move already prunes a card that left exile, so a
			// stale link should be impossible -- but encode nothing rather
			// than queue a trigger over a card no longer in exile.
			continue
		}
		if e.triggerFireCount[key] >= maxTriggerFires {
			return // cascade bound: see maxTriggerFires.
		}
		e.triggerFireCount[key]++
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     id,
			Controller: o.Controller,
			Cipher:     card,
			Ctx: effects.Ctx{
				Source:     id,
				Controller: o.Controller,
				Remembered: []state.Target{{Obj: card}},
			},
		})
	}
}

// checkGrantedExploitTriggers synthesizes Exploit's ETB election (CR 702.58a)
// for a creature that currently HAS the keyword but does not print it: a
// layer-6 AddKeyword$ Exploit grant (Colonel Autumn's "Other legendary
// creatures you control have exploit") has the same rules text as a printed
// keyword, and the printed K:Exploit expansion (cards/kw_exploit.go) only
// covers printed lines. Without this walk a granted creature's exploit never
// offers its sacrifice and never fires a trig:Exploited -- the static is
// visible to the layer system but has no trigger to carry it.
//
// The synthesized trigger reuses the ordinary ChangesZone machinery
// (triggerModeEvents' MoveZone entry, zoneGate/phaseGate, the
// changesZoneMatches Origin/Destination/ValidCard$ reads) with ValidCard$
// Card.Self, so its behaviour is byte-identical to the printed path's for the
// granted creature itself. The queue carries the __kwExploitGranted payload
// events.Apply rebuilds the same two-step Sacrifice -> Exploit chain from
// (the Ward/Afflict/Conspire shape; a granted creature has no __kwExploitGranted
// SVar for a TriggerPush face-index to resolve).
//
// The face that PRINTS Exploit is skipped: the printed expansion already owns
// the line for that creature, and firing both would sacrifice twice. The walk
// is reached from both the faceMayTrigger early-return path and the full
// path, the Dethrone/Afflict precedent, so a granted creature whose own
// printed triggers are live for this event still gets its exploit.
func (e *Engine) checkGrantedExploitTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	// Cheap gates first: this walk is invoked for every object the event
	// visits, so reject everything but the entering object before any
	// derived-characteristics read.
	if ev.Kind != events.MoveZone || ev.To != state.ZBattlefield || id != ev.Obj || o.Zone != state.ZBattlefield {
		return
	}
	if f.HasKeyword("Exploit") || !e.hasKeywordH(id, kwhExploit) {
		return
	}
	t := cards.Trigger{Mode: "ChangesZone", Params: map[string]string{
		"Mode": "ChangesZone", "ValidCard": "Card.Self", "Origin": "Any",
		"Destination": "Battlefield", "TriggerZones": "Battlefield",
	}}
	if !observer.triggerMatches(t, id, ev, objLKI) {
		return
	}
	key := triggerKey{Source: id, Idx: -1}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	if e.triggerFireCount[key] >= maxTriggerFires {
		return // cascade bound: see maxTriggerFires.
	}
	e.triggerFireCount[key]++
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source:     id,
		Controller: o.Controller,
		Exploit:    true,
		Ctx: effects.Ctx{
			Source:         id,
			Controller:     o.Controller,
			TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
		},
	})
}

// checkGrantedOffspringTriggers synthesizes Offspring's ETB trigger
// (CR 702.175a) for a creature that currently HAS the keyword but does not
// print it: a layer-6 AddKeyword$ Offspring:<cost> grant (Zinnia, Valley's
// Voice's "Creature spells you cast have offspring {2}") gives the creature
// the same rules text as a printed keyword, and the printed K:Offspring
// expansion (cards/kw_offspring.go) only covers printed lines. Without this
// walk the granted cast still offers and charges the additional cost, but
// nothing mints the 1/1 token copy -- the player pays for nothing.
//
// The synthesized trigger reuses the ordinary ChangesZone machinery
// (triggerModeEvents' MoveZone entry, zoneGate/phaseGate, the
// changesZoneMatches Origin/Destination/ValidCard$ reads) with ValidCard$
// Card.Self, so its behaviour is byte-identical to the printed path's for the
// granted creature itself. The queue carries the __kwOffspringGranted
// payload events.Apply rebuilds the same CopyPermanent body from (the Ward/
// Afflict/Exploit shape; a granted creature has no printed Trigger index for
// an inline body to ride). The body's Count$OffspringPaid reads the entering
// permanent's pay-time provenance, which the stack->battlefield Move
// preserved, so the plain cast mints nothing and the paid cast mints one.
//
// The face that PRINTS Offspring is skipped: the printed expansion already
// owns the line for that creature, and firing both would mint a second copy.
// The walk is reached from both the faceMayTrigger early-return path and the
// full path, the Dethrone/Afflict/Exploit precedent, so a granted creature
// whose own printed triggers are live for this event still gets its copy.
func (e *Engine) checkGrantedOffspringTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	// Cheap gates first: this walk is invoked for every object the event
	// visits, so reject everything but the entering object before any
	// derived-characteristics read.
	if ev.Kind != events.MoveZone || ev.To != state.ZBattlefield || id != ev.Obj || o.Zone != state.ZBattlefield {
		return
	}
	// The grant is evaluated with the STACK-zone override (hasCastOffspring,
	// the derivedWith read the cast offer uses), NOT the live battlefield
	// zone: Zinnia's grant is AffectedZone$ Stack, so the keyword is on the
	// SPELL -- the ETB trigger it grants is part of that spell's ability set
	// and must fire from the permanent the spell became. Reading the live
	// battlefield zone would drop the grant the moment it resolved. The
	// printed check stays on the face: a creature printing K:Offspring
	// already has the expansion trigger and must not fire twice.
	if f.HasKeyword("Offspring") || !e.hasCastOffspring(id) {
		return
	}
	t := cards.Trigger{Mode: "ChangesZone", Params: map[string]string{
		"Mode": "ChangesZone", "ValidCard": "Card.Self", "Origin": "Any",
		"Destination": "Battlefield", "TriggerZones": "Battlefield",
	}}
	if !observer.triggerMatches(t, id, ev, objLKI) {
		return
	}
	key := triggerKey{Source: id, Idx: -1}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	if e.triggerFireCount[key] >= maxTriggerFires {
		return // cascade bound: see maxTriggerFires.
	}
	e.triggerFireCount[key]++
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source:     id,
		Controller: o.Controller,
		Offspring:  true,
		Ctx: effects.Ctx{
			Source:         id,
			Controller:     o.Controller,
			TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
		},
	})
}

// checkGrantedDethroneTriggers synthesizes Dethrone's ordinary attack trigger
// (CR 702.105) for a creature that currently HAS the keyword but does not
// print it: a keyword granted in layer 6 has the same rules text as a printed
// keyword, and keyword expansion only adds triggers for printed K: lines.
// This is deliberately a read-only derived-characteristics check; granting
// and removing the keyword remains entirely in the existing continuous-effect
// system. It must run on the faceMayTrigger early-return path too (a granted
// keyword is independent of printed triggers, the same shape Granted Ward
// is), so it is called once per object before that gate.
func (e *Engine) checkGrantedDethroneTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	// The synthesized trigger is Attacks + ValidCard$ Card.Self, so only an
	// object this declaration names as an attacker can match it. Apply that
	// gate before deriving characteristics for every object in every zone,
	// as the granted Ward walk does; visitation and queue order are unchanged.
	if ev.Kind != events.DeclareAttackers || !slices.Contains(ev.IDs, id) {
		return
	}
	if e.hasKeywordH(id, kwhDethrone) && f.HasKeyword("Dethrone") {
		return
	}
	if !e.hasKeywordH(id, kwhDethrone) || f.HasKeyword("Dethrone") {
		return
	}
	t := cards.Trigger{Mode: "Attacks", Params: map[string]string{
		"Mode": "Attacks", "ValidCard": "Card.Self", "Dethrone": "True",
	}, Effect: &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined": "Self", "CounterType": "P1P1", "CounterNum": "1",
	}}}
	if observer.triggerMatches(t, id, ev, objLKI) {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] < maxTriggerFires {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{Source: id, Controller: o.Controller, Idx: -1, SA: t.Effect,
				Ctx: effects.Ctx{Source: id, Controller: o.Controller, Remembered: triggerRemembered(ev, id), LKI: objLKI,
					TriggerContext: observer.triggerReferents(t, id, ev, objLKI)}})
		}
	}
}

// checkGrantedTrainingTriggers synthesizes Training's ordinary attack trigger
// (CR 702.70) for a creature that currently HAS the keyword but does not
// print it: a layer-6 grant (Elder Arthur Maxson's "Creature tokens you
// control have training") gives the creature the same rules text as a printed
// keyword, and the printed K:Training expansion (cards/kw_training.go) only
// covers printed lines. The synthesized trigger reuses the printed shape
// exactly (Mode$ Attacks, ValidCard$ Card.Self, Training$ True, the P1P1
// PutCounter body), so it is byte-identical to the printed path: the
// event-relative power comparison and the declaration-wide attacker set are
// both read by attacksMatches. It skips the object entirely when its printed
// face already carries Training, so a token printing the keyword and also
// granted it fires once (the Dethrone dedup). Read-only derived
// characteristics; granting stays in the continuous-effect system. Like the
// Afflict/Conspire walks it runs on BOTH the early-return and the live
// printed-trigger paths, so a granted creature with its own triggers still
// trains.
func (e *Engine) checkGrantedTrainingTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	// The synthesized trigger is Attacks + ValidCard$ Card.Self, so only an
	// object this declaration names as an attacker can match it. Apply that
	// gate before deriving characteristics for every object in every zone.
	if ev.Kind != events.DeclareAttackers || !slices.Contains(ev.IDs, id) {
		return
	}
	if !e.hasKeywordH(id, kwhTraining) || f.HasKeyword("Training") {
		return
	}
	t := cards.Trigger{Mode: "Attacks", Params: map[string]string{
		"Mode": "Attacks", "ValidCard": "Card.Self", "Training": "True",
	}, Effect: &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined": "Self", "CounterType": "P1P1", "CounterNum": "1",
	}}}
	if observer.triggerMatches(t, id, ev, objLKI) {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] < maxTriggerFires {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{Source: id, Controller: o.Controller, Idx: -1, SA: t.Effect,
				Ctx: effects.Ctx{Source: id, Controller: o.Controller, Remembered: triggerRemembered(ev, id), LKI: objLKI,
					TriggerContext: observer.triggerReferents(t, id, ev, objLKI)}})
		}
	}
}

// checkGrantedMentorTriggers synthesizes Mentor's ordinary attack trigger
// (CR 702.134) for a creature that currently HAS the keyword but does not
// print it: a layer-6 grant (Aegis of the Legion's "Equipped creature ...
// has mentor", Nyxborn Unicorn's Bestow aura) gives the creature the same
// rules text as a printed keyword, and the printed K:Mentor expansion
// (cards/kw_mentor.go) only covers printed lines. The synthesized trigger
// reuses the printed shape exactly (Mode$ Attacks, ValidCard$ Card.Self, the
// targeted P1P1 PutCounter body carrying the Mentor$ marker), so it is
// byte-identical to the printed path: the target offer/recheck restriction
// is read by rules' mentorAdmits either way. It skips the object entirely
// when its printed face already carries Mentor, so a creature printing the
// keyword and also granted it fires once (the Dethrone dedup). Read-only
// derived characteristics; granting stays in the continuous-effect system.
// Like the Afflict/Conspire/Training walks it runs on BOTH the early-return
// and the live printed-trigger paths, so a granted creature with its own
// triggers still mentors.
func (e *Engine) checkGrantedMentorTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	// The synthesized trigger is Attacks + ValidCard$ Card.Self, so only an
	// object this declaration names as an attacker can match it. Apply that
	// gate before deriving characteristics for every object in every zone.
	if ev.Kind != events.DeclareAttackers || !slices.Contains(ev.IDs, id) {
		return
	}
	if !e.hasKeywordH(id, kwhMentor) || f.HasKeyword("Mentor") {
		return
	}
	t := cards.Trigger{Mode: "Attacks", Params: map[string]string{
		"Mode": "Attacks", "ValidCard": "Card.Self",
	}, Effect: &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"ValidTgts": "Creature.attacking", "TgtPrompt": "Select target attacking creature with lesser power",
		"Mentor": "True", "CounterType": "P1P1", "CounterNum": "1",
	}}}
	if observer.triggerMatches(t, id, ev, objLKI) {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] < maxTriggerFires {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{Source: id, Controller: o.Controller, Idx: -1, Mentor: true, SA: t.Effect,
				Ctx: effects.Ctx{Source: id, Controller: o.Controller, Remembered: triggerRemembered(ev, id), LKI: objLKI,
					TriggerContext: observer.triggerReferents(t, id, ev, objLKI)}})
		}
	}
}

// checkGrantedFirebendingTriggers synthesizes Firebending's attack trigger
// (CR 702.189a) for a creature that currently HAS the keyword but does not
// print it: a layer-6 AddKeyword$ Firebending:<N> grant (Sozin's Comet's
// "Each creature you control gains firebending 5 until end of turn", Fire
// Nation Palace's targeted grant, Iroh, Dragon of the West's counter-gated
// grant) gives the creature the same rules text as a printed keyword, and
// the printed K:Firebending expansion (cards/kw_firebending.go) only covers
// printed lines. The synthesized trigger reuses the printed shape exactly
// (Mode$ Attacks | ValidCard$ Card.Self), so it is byte-identical to the
// printed path: its body is the same DB$ Mana producing the granted N red
// with the end-of-combat exception, which rules.pushTrigger builds and
// events.Apply rebuilds from the KeywordTriggerPush payload alone. It skips
// the object entirely when its printed face already carries Firebending, so
// a creature printing the keyword and also granted it fires once (the
// Dethrone dedup). The amount is the derived keyword's parameter text (a
// literal for every measured grant); a blank or unreadable parameter is not
// an instance this build can price, so it queues nothing. Like the
// Dethrone/Afflict/Mentor walks this is a read-only derived-characteristics
// check; granting stays in the continuous-effect system. Runs on BOTH the
// early-return and the live printed-trigger paths.
func (e *Engine) checkGrantedFirebendingTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	// The synthesized trigger is Attacks + ValidCard$ Card.Self, so only an
	// object this declaration names as an attacker can match it. Apply that
	// gate before deriving characteristics for every object in every zone.
	if ev.Kind != events.DeclareAttackers || !slices.Contains(ev.IDs, id) {
		return
	}
	if !e.hasKeywordH(id, kwhFirebending) || f.HasKeyword("Firebending") {
		return
	}
	param, ok := e.derivedKeywordParamH(id, kwhFirebending)
	if !ok || param == "" {
		return
	}
	t := cards.Trigger{Mode: "Attacks", Params: map[string]string{
		"Mode": "Attacks", "ValidCard": "Card.Self",
	}, Effect: &cards.SA{Kind: "DB", API: "Mana", Params: map[string]string{
		"Produced": "R", "Amount": param, "PersistentUntilEndOfCombat": "True",
	}}}
	if observer.triggerMatches(t, id, ev, objLKI) {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] < maxTriggerFires {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{Source: id, Controller: o.Controller, Idx: -1, Firebending: param, SA: t.Effect,
				Ctx: effects.Ctx{Source: id, Controller: o.Controller, Remembered: triggerRemembered(ev, id), LKI: objLKI,
					TriggerContext: observer.triggerReferents(t, id, ev, objLKI)}})
		}
	}
}

// checkGrantedAfflictTriggers synthesizes Afflict's become-blocked trigger
// (CR 702.130) for a creature that currently HAS the keyword but does not
// print it: a keyword granted in layer 6 (Lost Monarch of Ifnir's
// "Other Zombies you control have afflict 3") has the same rules text as a
// printed keyword, and the printed K:Afflict expansion (cards/keywords.go)
// only covers printed lines. The synthesized trigger reuses the ordinary
// AttackerBlocked machinery -- triggerModeEvents' DeclareBlockers entry,
// zoneGate/phaseGate, attackerBlockedCandidates' dedup and fire-count bound
// -- so its behaviour is byte-identical to the printed path's for
// ValidCard$ Card.Self: one instance per distinct blocking declaration of
// the granted creature itself, the defender captured as
// TriggerContext.DefendingPlayer for the body's
// Defined$ TriggeredDefendingPlayer. The granted amount is the FIRST
// keyword granted in a layer never matches. The walk does NOT skip a card
// that also prints Afflict: the granted amounts are every Afflict entry in
// the derived list whose exact text no printed line already carries, so a
// card printing K:Afflict:1 that is granted "Afflict 3" fires BOTH (printed
// expansion for 1, this walk for 3), while a grant identical to a printed
// line is skipped -- the printed expansion already owns it and the two are
// indistinguishable as strings (a Monarch carrying another Monarch's
// identical grant keeps its one printed instance; the conservative choice).
// Every distinct grant queues its own instance, so two Monarchs on the
// battlefield stack two afflict-3 instances on the same Zombie. Like
// Dethrone's synthesis this is a read-only derived-characteristics check;
// granting stays in the continuous-effect system.
func (e *Engine) checkGrantedAfflictTriggers(id state.ObjID, o *state.Object, f *cards.Face, ev events.Event) {
	if ev.Kind != events.DeclareBlockers {
		return
	}
	// Only this declaration's blocked attackers can fire (the candidate walk
	// below keeps aid == id among ev.Pairs' attackers), so gate on that before
	// deriving characteristics for every object in every zone.
	blocked := false
	for _, pr := range ev.Pairs {
		if pr[0] == id {
			blocked = true
			break
		}
	}
	if !blocked {
		return
	}
	if !e.hasKeywordH(id, kwhAfflict) {
		return
	}
	printed := map[string]bool{}
	for _, k := range f.Keywords {
		printed[strings.ToLower(k)] = true
	}
	var amounts []int
	for _, k := range e.Derived(id).Keywords {
		if !strings.EqualFold(cards.KeywordHead(k), "Afflict") || printed[strings.ToLower(k)] {
			continue
		}
		param := ""
		if j := strings.IndexByte(k, ':'); j >= 0 {
			param = strings.TrimSpace(k[j+1:])
		}
		amount, err := strconv.Atoi(param)
		if err != nil || amount <= 0 {
			continue // a malformed or non-positive grant delivers no trigger.
		}
		amounts = append(amounts, amount)
	}
	if len(amounts) == 0 {
		return
	}
	t := cards.Trigger{Mode: "AttackerBlocked", Params: map[string]string{
		"Mode": "AttackerBlocked", "ValidCard": "Card.Self",
	}}
	if !e.zoneGate(t, id, ev) || !e.phaseGate(t) {
		return
	}
	key := triggerKey{Source: id, Idx: -1}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	if e.triggerFireCount[key] >= maxTriggerFires {
		return // cascade bound: see maxTriggerFires.
	}
	pt := func(p state.PlayerID) state.Target { return state.Target{Player: p, IsPlayer: true} }
	for _, aid := range e.attackerBlockedCandidates(t, id, ev) {
		// ValidCard$ Card.Self admits only the granted creature itself;
		// attackerBlockedCandidates still returns other attackers this event
		// blocked, so skip anything that is not the source.
		if aid != id {
			continue
		}
		defender := pt(0)
		if ao := e.G.Obj(aid); ao != nil {
			defender = pt(ao.Attacking)
		}
		for _, amount := range amounts {
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     id,
				Controller: o.Controller,
				// The Ward shape: the body rides the push's __kwAfflict:
				// payload for events.Apply to rebuild structurally -- a raw
				// SA cannot cross the log, and the TriggerPush -1 index
				// sentinel is Dethrone's own.
				Afflict: strconv.Itoa(amount),
				Ctx: effects.Ctx{
					Source:     id,
					Controller: o.Controller,
					Remembered: []state.Target{{Obj: aid}},
					Captured:   []state.Target{{Obj: aid}},
					TriggerContext: effects.TriggerContext{
						TriggerCard:     aid,
						TriggerSource:   aid,
						AttackingPlayer: pt(e.controllerOf(aid)),
						DefendingPlayer: defender,
					},
				},
			})
		}
	}
}

func (e *Engine) checkGrantedWardTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object, lkiPower, lkiToughness int32, lkiPTValid bool) {
	if e.finishingLifeLossBatch || e.lifeLossBatchDepth > 0 {
		return
	}
	// Every synthesized Ward uses BecomesTarget + Card.Self. Apply that
	// matcher's cheap gates before deriving characteristics for every object
	// in every zone. Keep the caller's visitation/queue order unchanged.
	if ev.Kind != events.TargetsChosen || !slices.Contains(ev.IDs, id) {
		return
	}
	printed := map[string]bool{}
	if !e.faceDownPrintedHides(o) {
		// While the object is face down its printed face does not exist
		// (CR 708.8): a cloaked card whose real face prints Ward must not
		// suppress its own cloak-ward -- the derived list is the only
		// keyword source on this path.
		for _, k := range f.Keywords {
			printed[strings.ToLower(k)] = true
		}
	}
	for _, k := range observer.Derived(id).Keywords {
		if printed[strings.ToLower(k)] {
			continue
		}
		param := ""
		if head := cards.KeywordHead(k); !strings.EqualFold(head, "Ward") {
			continue
		} else if j := strings.IndexByte(k, ':'); j >= 0 {
			param = strings.TrimSpace(k[j+1:])
		}
		if param == "" {
			continue
		}
		t := cards.Trigger{Mode: "BecomesTarget",
			Params: map[string]string{"ValidTarget": "Card.Self", "Ward": "True", "TriggerDescription": "Ward"},
			Effect: &cards.SA{Kind: "DB", API: "Ward",
				Params: map[string]string{"UnlessCost": param, "TriggerDescription": "Ward"}}}
		if !observer.triggerMatches(t, id, ev, objLKI) {
			continue
		}
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] >= maxTriggerFires {
			continue
		}
		e.triggerFireCount[key]++
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     id,
			Controller: o.Controller,
			Ward:       param,
			Ctx: effects.Ctx{
				Source:         id,
				Controller:     o.Controller,
				Remembered:     triggerRemembered(ev, id),
				Captured:       triggerRemembered(ev, id),
				LKI:            objLKI,
				LKIPower:       lkiPower,
				LKIToughness:   lkiToughness,
				LKIPTValid:     objLKI != nil && lkiPTValid,
				TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
			},
		})
	}
}

// checkGrantedStaticTriggers queues the triggered abilities a live static
// grant (AddTrigger$ on a Mode$ Continuous static, e.g. Hearthhull's
// "STATION 8+ Whenever you sacrifice a land") gives this object: the
// checkGrantedWard/checkGrantedDethrone precedent -- a granted triggered
// ability has the same rules text a printed one would, and the face walk
// above only ever scans printed T: lines. The grants live on the memoised
// static scan (active()); each Affected$-matched grant is matched against
// the event exactly as a printed trigger would be (triggerMatches, which
// resolves the granted body's own TriggerZones$/ValidPlayer$/Phase$ clauses)
// and queued with its SVar-resolved SA -- the pendingTrigger shape the
// granted-keyword paths use, pushed through events.GrantTriggerPush.
//
// The queue gate is the live==replay contract: the stack object is minted
// inside events.Apply, which resolves the Execute$ body from the GRANTOR's
// own SVar table (the object carrying the printed static, threaded to the
// event as Amount; 0 = the self-grant shape, where grantor == recipient), so
// the walk links the effect from that same table (the resolveSVarAcrossFaces
// walk mirrored in grantedTriggerExecute) and a grant whose body it cannot
// produce never queues -- the conservative direction, matching the
// replayable-log invariant rather than minting an ability a replay cannot
// rebuild. A self-grant (Hearthhull) trivially satisfies it, and so does a
// cross-object grant (an Aura granting its enchanted creature a trigger): the
// Execute$ SVar lives on the GRANTOR's face, which is ce.Source -- except for
// the Animate route, whose Source is the ANIMATED object and whose grantor
// the effect names via TriggerGrantor (the animating card's table).
//
// Fire-count: like Ward and Dethrone, every granted trigger shares the
// granted slot's triggerKey (Source, Idx -1) -- the cascade bound only, not
// once-per-turn memory, and maxTriggerFires (256) is generous enough that
// the sharing cannot starve a legitimate fire. Like those two the walk is
// deliberately a read over active()'s sorted slice, never a map: the queue
// order stays the scan's deterministic order.
// split/leaving are the face walk's two passes, passed through so the
// leaves-the-battlefield look-back discipline applies to granted triggers
// exactly as to printed ones (see the loop body): the look-back pass runs
// only a battlefield-origin ChangesZone trigger, the live pass only the
// rest -- a gate the first gains round omitted, which queued a gained "dies"
// trigger once per pass and fired it twice.
//
// statics is the event's PREFILTERED grant list (grantedTriggerStaticsFor):
// only the active statics carrying a granted trigger whose Mode$ can observe
// this event's kind, in active()'s order. The prefilter is exact, not a
// heuristic: triggerMatches' first gate is the same triggerModeEvents mask,
// and every check the loops below run before it (Affected$ match, the batch
// and look-back gates, the state-trigger latch, the Execute$ link) is a pure
// read, so a static the prefilter drops could only ever have reached
// triggerMatches to be rejected there -- dropping it queues nothing less.
func (e *Engine) checkGrantedStaticTriggersUsing(observer *Engine, statics []*ContinuousEffect, id state.ObjID, o *state.Object, ev events.Event, objLKI *state.Object, lkiPower, lkiToughness int32, lkiPTValid, split, leaving bool) {
	if len(statics) == 0 {
		return
	}
	// A has-all-abilities-of trigger (Forge's GainsTriggerAbsOf$, task
	// gains1): the recipient gains every triggered ability of each named
	// foreign card's face. The matching discipline is an ordinary trigger's --
	// triggerMatches resolves the granted body's own TriggerZones$/
	// ValidPlayer$/Phase$ clauses against the event -- and the queue carries
	// the face-local Triggers index and the foreign object id so
	// events.Apply mints the face's compiled Trigger.Effect pointer (the
	// MergedTriggerPush reasoning: pointer identity is how the owning-trigger
	// recoveries work). Nothing is linked by name here: a compiled trigger
	// already holds its Effect pointer, so the live queue and a replay mint
	// the identical body. The walk is a read over the memoised static slice
	// and the foreign faces' own deterministic Triggers order, never a map.
	for _, ce := range statics {
		// The TRIGGERED half only (Forge's GainsTriggerAbsOf$): a static that
		// names GainsAbilitiesOf$ alone never fires the foreign card's
		// triggers, because that parameter grants activated abilities and its
		// faces ride GainedFaces, which only grantedAbilities reads.
		if len(ce.GainedTriggerFaces) == 0 {
			continue
		}
		if !observer.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
			continue
		}
		for _, gf := range ce.GainedTriggerFaces {
			if gf.Face == nil {
				continue
			}
			for ti := range gf.Face.Triggers {
				t := gf.Face.Triggers[ti]
				if t.Effect == nil {
					continue
				}
				// The batch discipline (mirrored from the face walk).
				if t.Mode == "LifeLostAll" && e.lifeLossBatchDepth > 0 && !e.finishingLifeLossBatch {
					continue
				}
				if e.finishingLifeLossBatch && t.Mode != "LifeLostAll" {
					continue
				}
				// The leaves-the-battlefield split, mirrored from the face walk:
				// a "from anywhere" graveyard trigger is NOT a
				// leaves-the-battlefield trigger (CR 603.6c) and belongs to the
				// live pass even when this move leaves the battlefield, while a
				// battlefield-origin "dies" trigger looks back and belongs to
				// the look-back pass. Without the gate a gained dies trigger
				// queued once per pass and resolved twice.
				looksBack := t.Mode == "ChangesZone" && t.Params["Origin"] == "Battlefield"
				if split && looksBack != leaving {
					continue
				}
				// CR 603.8's outstanding-instance latch, mirrored from the face
				// walk (a state trigger already queued or on the stack does not
				// re-fire).
				if t.Mode == "Always" && e.stateTriggerOutstanding(id, -1) {
					continue
				}
				if !observer.triggerMatchesWithSVars(t, id, ev, objLKI, gf.Face.SVars) {
					continue
				}
				key := triggerKey{Source: id, Idx: -1}
				if e.triggerFireCount == nil {
					e.triggerFireCount = map[triggerKey]int32{}
				}
				if e.triggerFireCount[key] >= maxTriggerFires {
					continue
				}
				e.triggerFireCount[key]++
				e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
					Source:       id,
					Controller:   o.Controller,
					Idx:          ti,
					SA:           t.Effect,
					Gained:       true,
					GainedFrom:   gf.Obj,
					Execute:      t.Params["Execute"],
					Trigger:      t,
					TriggerSVars: gf.Face.SVars,
					Ctx: effects.Ctx{
						Source:         id,
						Controller:     o.Controller,
						Remembered:     triggerRememberedMode(t, ev, id),
						Captured:       triggerRememberedMode(t, ev, id),
						LKI:            objLKI,
						LKIPower:       lkiPower,
						LKIToughness:   lkiToughness,
						LKIPTValid:     objLKI != nil && lkiPTValid,
						TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
					},
				})
			}
		}
	}
	// The face walk's life-loss-batch discipline, mirrored exactly (the
	// Animate Triggers$ route needs it: a granted DamageDone trigger must
	// fire on the in-batch Damage event the way a printed one does, and the
	// finishing pass must not re-check it -- the old blanket guard silenced
	// every granted trigger for the WHOLE batch including its finishing
	// pass, so a combat-damage DamageDone grant could never fire at all).
	for _, ce := range statics {
		if ce.AddTrigger == nil {
			continue
		}
		if !observer.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
			continue
		}
		t := *ce.AddTrigger
		// The batch discipline (mirrored from the face walk, above).
		if t.Mode == "LifeLostAll" && e.lifeLossBatchDepth > 0 && !e.finishingLifeLossBatch {
			continue
		}
		if e.finishingLifeLossBatch && t.Mode != "LifeLostAll" {
			continue
		}
		// The leaves-the-battlefield split, mirrored from the face walk and
		// the GainedTriggerFaces loop above: a battlefield-origin ChangesZone
		// grant (Relic Vial's "whenever a creature you control dies") looks
		// back (CR 603.10a) and belongs to the look-back pass alone. Without
		// the gate the same grant queued once per pass on every
		// leaves-the-battlefield event -- once against the pre-departure board
		// and once against the live one -- and each opponent lost 2 life
		// where the card says 1.
		if split && (t.Mode == "ChangesZone" && t.Params["Origin"] == "Battlefield") != leaving {
			continue
		}
		// The live==replay gate: link the Execute$ body exactly the way
		// events.Apply will (the GRANTOR's own table -- ce.Source carries the
		// printed static; a self-grant degenerates to the affected object;
		// the Animate route names the animating face via TriggerGrantor,
		// since Source there is the ANIMATED object and the body lives on
		// the animating card's table). A body it cannot resolve never
		// queues, and a same-named body it CAN resolve is by construction
		// the same body a replay would resolve.
		grantorID := ce.Source
		if ce.TriggerGrantor != 0 {
			grantorID = ce.TriggerGrantor
		}
		grantor := observer.G.Obj(grantorID)
		if grantor == nil || grantor.Face() == nil {
			continue
		}
		grantFace := grantedTriggerFace(grantor, t.Params["Execute"])
		if grantFace == nil {
			continue
		}
		t.Effect = cards.ResolveSVar(grantFace.SVars, t.Params["Execute"])
		// CR 603.8's outstanding-instance latch, mirrored from the face walk
		// (a state trigger already queued or on the stack does not re-fire).
		if t.Mode == "Always" && e.stateTriggerOutstanding(id, -1) {
			continue
		}
		if !observer.triggerMatchesWithSVars(t, id, ev, objLKI, grantFace.SVars) {
			continue
		}
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] >= maxTriggerFires {
			continue // cascade bound: see maxTriggerFires.
		}
		e.triggerFireCount[key]++
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:       id,
			Controller:   o.Controller,
			Idx:          -1,
			SA:           t.Effect,
			Granted:      true,
			Grantor:      grantorID,
			Execute:      t.Params["Execute"],
			Trigger:      t,
			TriggerSVars: grantFace.SVars,
			Ctx: effects.Ctx{
				Source:         id,
				Controller:     o.Controller,
				Remembered:     triggerRememberedMode(t, ev, id),
				LKI:            objLKI,
				LKIPower:       lkiPower,
				LKIToughness:   lkiToughness,
				LKIPTValid:     objLKI != nil && lkiPTValid,
				TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
			},
		})
	}
}

// grantedTriggerStaticsFor returns, in active()'s order, pointers to the
// statics whose granted triggers (GainedTriggerFaces with a linked Effect, or
// AddTrigger) include at least one Mode$ whose triggerModeEvents mask admits
// kind. checkFaceTriggers computes it ONCE per event walk instead of running
// every grant's Affected$ spec match for every object in every zone on every
// event; see checkGrantedStaticTriggersUsing for why the drop is exact. buf is
// reused as the result's backing array (it may be nil). The pointers alias
// the memoised active() slice, which is stable for the whole walk (matching
// neither emits nor mutates continuous effects).
func grantedTriggerStaticsFor(statics []ContinuousEffect, kind events.Kind, buf []*ContinuousEffect) []*ContinuousEffect {
	out := buf[:0]
	for i := range statics {
		ce := &statics[i]
		if grantedTriggerStaticObserves(ce, kind) {
			out = append(out, ce)
		}
	}
	return out
}

// grantedTriggerStaticObserves reports whether any trigger ce grants could
// pass triggerMatches' leading event-kind gate for kind.
func grantedTriggerStaticObserves(ce *ContinuousEffect, kind events.Kind) bool {
	if ce.AddTrigger != nil && triggerModeEvents(ce.AddTrigger.Mode).allows(kind) {
		return true
	}
	for _, gf := range ce.GainedTriggerFaces {
		if gf.Face == nil {
			continue
		}
		for ti := range gf.Face.Triggers {
			t := &gf.Face.Triggers[ti]
			if t.Effect != nil && triggerModeEvents(t.Mode).allows(kind) {
				return true
			}
		}
	}
	return false
}

// grantedTriggerExecute mirrors events.Apply's GrantTriggerPush resolution
// (the resolveSVarAcrossFaces walk): the granted body's Execute$ name is
// resolved against the GRANTOR's own SVar table -- current face first, then
// every other face -- so the live queue links exactly the body a replayed
// log will. (Apply resolves from the object Amount names when set; for the
// self-grant shape grantor == recipient, so passing the grantor covers both
// arms.) nil when no face resolves it.
func grantedTriggerExecute(o *state.Object, execute string) *cards.SA {
	if f := grantedTriggerFace(o, execute); f != nil {
		return cards.ResolveSVar(f.SVars, execute)
	}
	return nil
}

// grantedTriggerFace uses the same current-then-other-face order as Apply's
// by-name body lookup. The SVar table of that face owns the entire body,
// including its nested names, even after the grant leaves play.
func grantedTriggerFace(o *state.Object, execute string) *cards.Face {
	if o == nil || execute == "" {
		return nil
	}
	if f := o.Face(); f != nil && cards.ResolveSVar(f.SVars, execute) != nil {
		return f
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if cards.ResolveSVar(f.SVars, execute) != nil {
				return f
			}
		}
	}
	return nil
}

// triggerCastAlt is one surviving ValidCard$ alternative of a Mode$
// SpellCast trigger after the trigger-side self-cast exclusion was applied:
// spec is the token-stripped filter text, exclSelf whether the alternative
// carried the bare !CastSaSource token — its ActivatorThisTurnCastEach$
// tally must also skip the trigger source's own printed-name casts.
type triggerCastAlt struct {
	spec     string
	exclSelf bool
}

// checkGrantedCumulativeUpkeepTriggers synthesizes the beginning-of-upkeep
// cumulative-upkeep trigger (CR 702.24a) for a permanent that currently HAS
// the keyword but does not PRINT it: a layer-6 AddKeyword$ Cumulative
// upkeep:<cost> grant (Breath of Dreams, Mana Chains, Decomposition) or an
// A:AB$ Pump's KW$ Cumulative upkeep:<cost> grant (Balduvian Shaman, Dreams
// of the Dead) gives the permanent the same rules text a printed
// K:Cumulative upkeep line would, and keyword expansion (cards/
// kw_cumulativeupkeep.go) only adds the Phase trigger for printed lines.
// Without this walk the granted permanent never accrues an age counter and
// is never asked to pay or be sacrificed -- the player's upkeep simply
// passes.
//
// The synthesized trigger reuses the printed expansion's exact shape
// (Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | TriggerZones$
// Battlefield; DB$ CumulativeUpkeep | Cost$ <cost>), so its behaviour is
// byte-identical to the printed path's: rules/stack.go's Stack resolution
// reads o.Ability.API == "CumulativeUpkeep" and startCumulativeUpkeep reads
// the Cost$ param, exactly as for a printed line. The union of printed and
// granted keywords is a read-only derived-characteristics check (the
// Dethrone/Afflict/Flanking precedent); granting stays in the
// continuous-effect system, and nothing here mutates state.
//
// Dedup uses the Afflict rule, not the Dethrone one: every DISTINCT granted
// instance whose exact text no printed line already carries is its own
// trigger (CR 702.24a's "if a permanent has multiple instances of cumulative
// upkeep, each triggers separately"), so a printed Cumulative upkeep:1 that
// is also granted Cumulative upkeep:2 fires both. A grant identical to a
// printed line is skipped -- the printed expansion already owns it, and the
// two are indistinguishable as strings.
//
// Runs on BOTH the faceMayTrigger early-return path and the full path (a
// granted keyword is independent of printed triggers, the same both-paths
// rule Afflict/Conspire/Training follow). Cheap gates first: the walk is
// invoked for every object the event visits, so reject everything but a
// battlefield object on a step change before any derived-characteristics
// read.
// atEOTTrigPhase is the synthesized CopyPermanent AtEOTTrig$ trigger's Phase$
// value: Forge's script name for the end step (state.ParsePhases maps
// "End of Turn" to StepEnd). No ValidPlayer$: the token's ability triggers at
// the beginning of the END STEP, any player's -- the wording is "at the
// beginning of the end step, sacrifice this token", not "your next end
// step" (which is the AtEOT$ DELAYED rider's family, not this copiable one).
const atEOTTrigPhase = "End of Turn"

// atEOTTrigSteps and cumulativeUpkeepSteps are the two synthesized step
// triggers' Phase$ values parsed once (state.ParsePhases is pure syntax, the
// same parse parsedPhaseSpec caches per engine), so their early step gates
// and the trigger walk's battlefield test read a bitset instead of a
// string-keyed map per object per step change.
var (
	atEOTTrigSteps        = mustParseSteps(atEOTTrigPhase)
	cumulativeUpkeepSteps = mustParseSteps(cumulativeUpkeepPhase)
)

func mustParseSteps(spec string) state.StepSet {
	set, unknown := state.ParsePhases(spec)
	if len(unknown) != 0 {
		panic("rules: synthesized trigger phase " + spec + " does not parse")
	}
	return set
}

// checkGrantedAtEOTTriggers synthesizes the "At the beginning of the end
// step, sacrifice/exile this token" triggered ability a DB$ CopyPermanent |
// AtEOTTrig$ put on the object as a copiable value (Object.AtEOTTrigBody,
// set by events.Apply's CopyToken case from the mint's body or the source
// object's). It is deliberately NOT the AtEOT$ rider: that schedules a
// one-shot DELAYED trigger keyed to the object's incarnation, so a token
// copy of the minted token would not carry it; here the ability is part of
// the object's copiable values (CR 707.2) and a copy inherits the body name.
//
// The synthesized trigger reuses the printed Phase shape (Mode$ Phase | Phase$
// End of Turn | TriggerZones$ Battlefield) and a builtin SVar body, so its
// behaviour is byte-identical to a printed instance: the queue entry is the
// granted-trigger shape (Granted + the builtin Execute$ name, Grantor 0 =
// the self-grant form), which events.Apply's GrantTriggerPush case resolves
// from the recipient's own table -- where the builtin body is always
// available -- exactly as a replay does. Nothing here mutates state.
//
// Runs on BOTH the faceMayTrigger early-return path and the full path, like
// every other granted trigger (a copiable body is independent of the object's
// printed triggers). Cheap gates first: reject everything but a battlefield
// object with a body on a step change before any match work.
func (e *Engine) checkGrantedAtEOTTriggers(observer *Engine, id state.ObjID, o *state.Object, ev events.Event, objLKI *state.Object) {
	if ev.Kind != events.StepChange || o.Zone != state.ZBattlefield || o.AtEOTTrigBody == "" {
		return
	}
	// The synthesized trigger's Phase$ gate (triggerMatches' phaseGate, the
	// same parsed spec on the same observer) hoisted above the match: on any
	// other step the trigger cannot match, and the body lookup below is a
	// pure read, so skipping it there queues nothing less.
	if !atEOTTrigSteps.Has(observer.G.Step) {
		return
	}
	body := o.AtEOTTrigBody
	sa := grantedTriggerExecute(o, body)
	if sa == nil {
		return
	}
	t := cards.Trigger{Mode: "Phase", Params: map[string]string{
		"Mode": "Phase", "Phase": atEOTTrigPhase, "TriggerZones": "Battlefield", "Execute": body}}
	t.Effect = sa
	if !observer.triggerMatches(t, id, ev, objLKI) {
		return
	}
	key := triggerKey{Source: id, Idx: -1}
	if e.triggerFireCount == nil {
		e.triggerFireCount = map[triggerKey]int32{}
	}
	if e.triggerFireCount[key] >= maxTriggerFires {
		return // cascade bound: see maxTriggerFires.
	}
	e.triggerFireCount[key]++
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source:     id,
		Controller: o.Controller,
		Granted:    true,
		Execute:    body,
		SA:         sa,
		Trigger:    t,
		Ctx: effects.Ctx{
			Source:         id,
			Controller:     o.Controller,
			LKI:            objLKI,
			TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
		},
	})
}

// cumulativeUpkeepPhase is the synthesized cumulative-upkeep trigger's
// Phase$ value, shared by the trigger and its early step gate.
const cumulativeUpkeepPhase = "Upkeep"

func (e *Engine) checkGrantedCumulativeUpkeepTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	if ev.Kind != events.StepChange || o.Zone != state.ZBattlefield {
		return
	}
	// The synthesized trigger's Phase$ Upkeep gate (triggerMatches' phaseGate,
	// the same parsed spec on the same observer) hoisted above the
	// derived-keyword read: on every other step change the trigger cannot
	// match, and grantedCumulativeCosts is a pure read, so skipping it there
	// queues nothing less.
	if !cumulativeUpkeepSteps.Has(observer.G.Step) {
		return
	}
	costs := observer.grantedCumulativeCosts(id, f)
	if len(costs) == 0 {
		return
	}
	t := cards.Trigger{Mode: "Phase", Params: map[string]string{
		"Mode": "Phase", "Phase": cumulativeUpkeepPhase, "ValidPlayer": "You", "TriggerZones": "Battlefield",
	}}
	if !observer.triggerMatches(t, id, ev, objLKI) {
		return
	}
	for _, cost := range costs {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] >= maxTriggerFires {
			return // cascade bound: see maxTriggerFires.
		}
		e.triggerFireCount[key]++
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     id,
			Controller: o.Controller,
			Cumulative: cost,
			Ctx: effects.Ctx{
				Source:         id,
				Controller:     o.Controller,
				LKI:            objLKI,
				TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
			},
		})
	}
}

// grantedCumulativeCosts returns the upkeep cost of every cumulative-upkeep
// instance the object currently HAS but does not PRINT, in deterministic
// derived-keyword order. Each returned string is the Forge Cost$ text with
// its trailing display suffix stripped (cards/kw_cumulativeupkeep.go cuts the
// same way): "Cumulative upkeep:PayLife<1>:Pay 1 life." yields "PayLife<1>",
// "Cumulative upkeep:S" yields "S", and a bare "Cumulative upkeep" yields the
// empty string. A nil result means no granted instance (the whole object is
// skipped, so a printed-only permanent never reaches the synthesis).
func (e *Engine) grantedCumulativeCosts(id state.ObjID, f *cards.Face) []string {
	// The exact negative precheck (keywordmay.go): no printed, intrinsic,
	// status or active-effect keyword seed carries the head, so the derived
	// list cannot either and the layer walk below would find nothing.
	if !e.mayHaveDerivedKeywordH(id, kwhCumulativeUpkeep) {
		if derivedMemoVerify {
			for _, k := range e.Derived(id).Keywords {
				if strings.EqualFold(cards.KeywordHead(k), "Cumulative upkeep") {
					panic(fmt.Sprintf("rules: cumulative-upkeep precheck ruled out obj %d but Derived carries %q", id, k))
				}
			}
		}
		return nil
	}
	var printed map[string]bool
	for _, k := range f.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), "Cumulative upkeep") {
			if printed == nil {
				printed = map[string]bool{}
			}
			printed[strings.ToLower(k)] = true
		}
	}
	var out []string
	for _, k := range e.Derived(id).Keywords {
		if !strings.EqualFold(cards.KeywordHead(k), "Cumulative upkeep") {
			continue
		}
		if printed[strings.ToLower(k)] {
			continue
		}
		param := ""
		if j := strings.IndexByte(k, ':'); j >= 0 {
			param = strings.TrimSpace(k[j+1:])
		}
		cost, _, _ := strings.Cut(param, ":")
		cost = strings.TrimSpace(cost)
		if cost == "" {
			// A bare `Cumulative upkeep` grant (no cost field) still triggers:
			// encode its zero cost as "0" so pendingTrigger.Cumulative is
			// non-empty and the pushTrigger guard can tell it from an absent
			// grant (the Ward/Afflict `!= ""` convention). ParseCost("0") is
			// the same empty cost ParseCost("") is.
			cost = "0"
		}
		out = append(out, cost)
	}
	return out
}
