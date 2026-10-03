package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func effCleanup(h Host, c *Ctx, sa *cards.SA) {
	// Forge's CleanUpEffect: ClearRemembered$ True clears the host card's
	// remembered list (the persistent list the next resolution of this card
	// reads -- without this an activated ability that remembers would
	// accumulate across activations). The ctx-level list is cleared with it:
	// every consumer downstream of this point in the chain (and the next
	// resolution) must see an empty list, which is what Forge's host
	// list clear produces. The clear is recorded as a real event ONLY when
	// the source's list actually held entries -- clearing an empty list is
	// a no-op, and emitting for it would move every chain head that carries
	// a ClearRemembered$ cleanup for no observable change. (Delver of
	// Secrets' DBCleanup used to be the measured empty-list case; since
	// effReveal's RememberRevealed$ arm writes the source list too
	// (count:Plus.<SVarName>), Delver's cleanup holds a real entry and does
	// emit -- that is what moved the 4- and 6-seat heads.)
	noted := false
	if strings.EqualFold(sa.Params["ClearRemembered"], "True") {
		c.Remembered = nil
		if c.Source != 0 {
			if o := h.Game().Obj(c.Source); o != nil && len(o.Remembered) > 0 {
				// A real clear: the event is what a replay folds, so the next
				// resolution of this card sees the empty list.
				h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "clear-remembered"})
				noted = true
			}
		}
	}
	// ClearChosenCard$ / ClearChosenPlayer$ (Party Thrasher's DBClearChosen,
	// Wishclaw Talisman's DBCleanup, Vial Smasher's): the same discipline as
	// ClearRemembered for Forge's chosen-card / chosen-player fields -- the
	// persistent lists a later resolution's Card.ChosenCard/ChosenPlayer
	// predicates read. Only a real clear emits; an empty-list clear (the
	// overwhelmingly common case for one-shot effects) stays a no-op so no
	// golden game gains an event for nothing.
	if strings.EqualFold(sa.Params["ClearChosenCard"], "True") {
		c.Chosen = keepChosenPlayers(c.Chosen)
		if c.Source != 0 {
			if o := h.Game().Obj(c.Source); o != nil && hasChosenCards(o.Chosen) {
				h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "clear-chosen-card"})
				noted = true
			}
		}
	}
	if strings.EqualFold(sa.Params["ClearChosenPlayer"], "True") {
		c.Chosen = keepChosenCards(c.Chosen)
		if c.Source != 0 {
			if o := h.Game().Obj(c.Source); o != nil && hasChosenPlayers(o.Chosen) {
				h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "clear-chosen-player"})
				noted = true
			}
		}
	}
	// The cosmetic fallback (an empty-list clear, or a Cleanup with nothing
	// to clear): the Note main has always emitted, byte-for-byte, so golden
	// games whose cleanups run on empty lists replay identically. This also
	// covers main's independent Valakut concern: Valakut's DBCleanup runs
	// after DBEffect captured the dig's RememberChanged list into the
	// registered Effect, so the end-step trigger's own X=Remembered$Amount
	// must count only what IT moved -- c.Remembered is unconditionally
	// cleared above regardless of whether the source object held a
	// persisted list to clear too.
	if !noted {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "clears remembered/imprinted objects"})
	}
}

// hasChosenCards reports whether the chosen list holds any object entries.
func hasChosenCards(ts []state.Target) bool {
	for _, t := range ts {
		if !t.IsPlayer {
			return true
		}
	}
	return false
}

// hasChosenPlayers reports whether the chosen list holds any player entries.
func hasChosenPlayers(ts []state.Target) bool {
	for _, t := range ts {
		if t.IsPlayer {
			return true
		}
	}
	return false
}

// effSetState flips a double-faced target to its other face. Mode$ Transform
// marks the FlipFace event for CR 701.26 triggers; Flip/Meld still change faces
// without transforming. The face walk advances to the next face, wrapping to
// 0, which is correct for the
// overwhelmingly common two-face case and a no-op for anything with fewer
// than two faces (a token, or a single-faced card).
//
// Mode$ Unspecialize is the other face-SELECTING mode: Forge's Specialize
// alternate mode (Bloomburrow Commander's Lukamina family) exits back to the
// card's FRONT face -- index 0 -- from whatever later face it wears, never
// the next face in the walk.
//
// Mode$ TurnFaceUp is the one exception: it is not a face change at all but
// CR 708.6's reveal of a face-down battlefield permanent's printed face, so
// it emits events.TurnFaceUp (which clears the face-down marker in Apply)
// instead of a FlipFace. This is the effect-driven turn-up the corpus's
// `AB$ SetState | Mode$ TurnFaceUp` lines carry (Woolly Loxodon and its 22
// siblings); a non-face-down permanent is left alone, matching the marker's
// own battlefield gate.
//
// Optional$ True is a real may election (Dowsing Dagger's "you may transform
// this Equipment", High Marshal Arguel's "you may transform it"): the ask is
// posed before the change -- but only when at least one Defined$ object would
// actually change, so a no-op shape asks nothing (the Attach/PutCounter
// len(legal) == 0 gate). Option 0 is "yes" and option 1 "no", so the
// deterministic bot clamp answers "yes" and bot games stay byte-identical to
// the pre-ask always-change. The answer rides Ctx.SetStateOpt (fx42 scoping:
// consumed and cleared at the top); a decline changes nothing and the chained
// SubAbility$ still runs (the chain is owned by Resolve, never by a decline).
func effSetState(h Host, c *Ctx, sa *cards.SA) {
	// fx42 scoping: consume and clear the answered Optional$ election at the
	// top, so a nested SetState in the same chain poses its own ask.
	optAns := c.SetStateOpt
	c.SetStateOpt = ""
	mode := sa.ParamStr(cards.PKMode)
	turnUp := strings.EqualFold(strings.TrimSpace(mode), "TurnFaceUp")
	turnDown := strings.EqualFold(strings.TrimSpace(mode), "TurnFaceDown")
	unspecialize := strings.EqualFold(strings.TrimSpace(mode), "Unspecialize")
	optional := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True")
	if optional && optAns == "" {
		// Unanswered: pose the yes/no election -- but only when the change
		// would actually do something; with nothing to change, decline and
		// accept are the same, so no ask (the Attach precedent's
		// len(legal) == 0 gate). AskAsked suspends; the answer re-enters
		// with Ctx.SetStateOpt set. AskNoHost is the deterministic decline
		// stand-in (R-9): the clamp-answered bot path answers option 0 =
		// "yes", so a bot game stays byte-identical to the pre-ask
		// always-change.
		if setStateWouldChange(h, c, sa, turnUp, turnDown, unspecialize) {
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "setstate_optional", ResumeSA: sa,
				ResumeRemembered: copyTargets(c.Remembered),
				Prompt:           "Change this permanent's face?",
				Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes", Player: c.Controller},
					{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
				}}
			_ = Ask(h, d)
			return
		}
	}
	if optional && optAns != "" && optAns != "yes" {
		// Answered "no" (or any non-affirmative marker): the decline. No
		// face change and no Note is emitted; the chained SubAbility$ STILL
		// RUNS -- the chain is owned by Resolve, not by this body (the
		// PutCounter/Attach.Optional precedent).
		return
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		if turnUp {
			if o.Card != nil && o.Zone == state.ZBattlefield && o.FaceDown {
				h.Emit(events.Event{Kind: events.TurnFaceUp, Obj: o.ID})
				setstateRememberChanged(c, sa, o.ID)
			}
			continue
		}
		if turnDown {
			if o.Zone == state.ZBattlefield && !o.FaceDown {
				setType := strings.TrimSpace(sa.Params["FaceDownSetType"])
				power, hasPower := NumResolved(h, c, sa, "FaceDownPower", 0)
				toughness, hasToughness := NumResolved(h, c, sa, "FaceDownToughness", 0)
				h.Emit(events.Event{Kind: events.TurnFaceDown, Obj: o.ID,
					Counter: events.FaceDownEntryCounterFor(setType, power, toughness, hasPower || hasToughness)})
				setstateRememberChanged(c, sa, o.ID)
			}
			continue
		}
		if unspecialize {
			// Restore the FRONT face (index 0), whatever face the object
			// wears now -- never the generic next-face walk, which from a
			// later face lands on a DIFFERENT specialization (or wraps).
			// Event-sourced like the rest of this primitive: the same
			// FlipFace fold events.Apply reconstructs the face from, so a
			// replay of the emitted stream shows the front face.
			if o.Card == nil || len(o.Card.Faces) < 2 || o.FaceIdx == 0 {
				continue
			}
			h.Emit(events.Event{Kind: events.Note, Obj: o.ID,
				Text: "flips to face 0 (Unspecialize)"})
			h.Emit(events.Event{Kind: events.FlipFace, Obj: o.ID, Amount: 0})
			setstateRememberChanged(c, sa, o.ID)
			continue
		}
		if o.Card == nil || len(o.Card.Faces) < 2 {
			continue
		}
		next := (int(o.FaceIdx) + 1) % len(o.Card.Faces)
		h.Emit(events.Event{Kind: events.Note, Obj: o.ID,
			Text: "flips to face " + strconv.Itoa(next) + " (" + mode + ")"})
		flip := events.Event{Kind: events.FlipFace, Obj: o.ID, Amount: int32(next)}
		if strings.EqualFold(strings.TrimSpace(mode), "Transform") && o.Zone == state.ZBattlefield {
			flip.Text = "Transformed"
		}
		h.Emit(flip)
		setstateRememberChanged(c, sa, o.ID)
	}
}

// setstateRememberChanged honours a SetState body's RememberChanged$ True: each
// object the loop above actually emitted a face change for joins the
// resolution's Remembered, where the chained SubAbility$ reads it -- Megatron,
// Tyrant's DBMana (ConditionDefined$ Remembered), Soul Seizer's DB$ Attach, the
// Enduring Angel lose-game gate, Lukamina's DBReturn. It is the Dig precedent,
// digRemember (cardflow.go), and it is Ctx-only, never the persistent
// eventRemember half: every measured consumer reads the list inside the same
// chain and each of those chains ends in ClearRemembered$ True. Absent the
// parameter (the corpus default) the walk adds nothing, so every pre-existing
// game replays byte-identically. Decline and no-op paths never reach an emit,
// so they remember nothing -- Forge remembers the objects whose state CHANGED.
func setstateRememberChanged(c *Ctx, sa *cards.SA, id state.ObjID) {
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberChanged)), "True") {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
}

// setStateWouldChange reports whether the SetState resolution would change at
// least one of its Defined$ objects' faces -- the gate that keeps an Optional$
// ask from being posed when decline and accept are the same outcome. It is the
// exact per-object predicate the emitting loop below applies, so the gate can
// never disagree with what the loop would do.
func setStateWouldChange(h Host, c *Ctx, sa *cards.SA, turnUp, turnDown, unspecialize bool) bool {
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		if turnUp {
			if o.Card != nil && o.Zone == state.ZBattlefield && o.FaceDown {
				return true
			}
			continue
		}
		if turnDown {
			if o.Zone == state.ZBattlefield && !o.FaceDown {
				return true
			}
			continue
		}
		if unspecialize {
			// The would-change read shares Unspecialize's own semantics (ONE
			// home with effSetState's branch above): only a multi-faced
			// object not already on its front face changes.
			if o.Card != nil && len(o.Card.Faces) >= 2 && o.FaceIdx != 0 {
				return true
			}
			continue
		}
		if o.Card != nil && len(o.Card.Faces) >= 2 {
			return true
		}
	}
	return false
}

// effCounter removes the targeted spell from the stack to its owner's
// graveyard. This is CR 608.2b's canonical case: if the target is no longer
// on the stack by the time this resolves (already resolved, or itself
// countered by an earlier effect in the same response), it is simply skipped
// rather than moved from wherever it now sits.
//
// Task 9 fix round 1 (Important 2): a spell cast for its flashback cost is
// exiled "any time it would leave the stack" (CR 702.33b), which includes
// being countered -- spellRestZone (rules/stack.go) covers every exit in
// resolveTop, but this primitive hard-coded the graveyard, so a countered
// flashbacked spell (Force of Will / Daze / Counterspell against Cabal
// Therapy) returned to the graveyard and could be flashbacked again. The
// cast flags are already readable here (effects/filter.go reads them the
// same way), so the destination is chosen the same way spellRestZone does.
//
// UnlessCost$ (Mana Leak, Spell Pierce, Daze, Rust Tick, Runeboggle) is the
// "counter target spell unless its controller pays {N}" shape. It rides the
// ONE shared unless gate (effects.Resolve's unlessProceed dispatch, shared
// by every API): the payer comes from UnlessPayer$ (effects.UnlessPayers
// resolves every corpus selector form; a named selector whose binding is
// unavailable declines rather than asking an unrelated player), and the
// unqualified default — the first target's controller per CR 119 — is
// exactly what the corpus's 12 targeted Counter lines name (Targeted
// Controller x9, ThisTargetedController x3). The pay/decline labels for a
// Counter's ask live in poseUnlessAsk's Counter arm. "pay" means the spell
// is NOT countered; "decline" — including an affordable-looking "pay" the
// payment path could not cover — counters it.
//
// UnlessSwitched$ True inverts the whole ask — paying CAUSES the counter —
// and is real switched semantics through the same gate; the orientation is
// read from the SA, not hardcoded here.
func effCounter(h Host, c *Ctx, sa *cards.SA) {
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberCountered"]), "True") ||
		strings.EqualFold(strings.TrimSpace(sa.Params["RememberCounteredSA"]), "True")
	// RememberCounteredCMC$ (task counter-cmc): remember each countered
	// spell's mana VALUE -- Electrosiphon's "an amount of {E} equal to its
	// mana value", Overwhelming Intellect's draw family (14 corpus carriers,
	// every one reading it back through SVar:X:Count$RememberedNumber). The
	// number lands on the Ctx channel above; an ABILITY has no mana value
	// and contributes nothing.
	rememberCMC := strings.EqualFold(strings.TrimSpace(sa.Params["RememberCounteredCMC"]), "True")
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZStack {
			continue
		}
		// AddsNoCounter$ mana (Cavern of Souls): a spell paid with that mana
		// carries state.FlagNoCounter and can't be countered — it stays on the
		// stack and resolves (CR 608.2b's removal never happens). The spell is
		// still a legal TARGET (CR: "can't be countered" does not stop
		// targeting), so the record is one loud Note naming the object, and
		// the Counter's remaining targets (and SubAbility$ chain) run on.
		if o.CastFlags&state.FlagNoCounter != 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: o.ID, Text: "can't be countered"})
			continue
		}
		if !h.CounterAllowed(o.ID, c.Source) {
			h.Emit(events.Event{Kind: events.Note, Obj: o.ID, Text: "counter prevented"})
			if h.Suspended() {
				return // replacement order must settle before any later target/SA
			}
			continue
		}
		if state.StackKindOf(h.Game(), o) != state.StackKindSpell {
			// CR 701.5a: to counter a spell or ability is to cancel it,
			// removing it from the stack so it never resolves. An ability is
			// not a card and has no graveyard to move to -- this is the same
			// "ceases to exist" rest every resolved ability already takes
			// (CR 608.2m, rules/stack.go's ability tail parks it in exile),
			// so a countered ability moves there, never to the graveyard.
			if remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
				eventRemember(h, c, o.ID)
			}
			h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID,
				From: state.ZStack, To: state.ZExile, Text: "countered"})
			continue
		}
		// Destination$ (Remand's "into its owner's hand instead of into that
		// player's graveyard", Force of Will's explicit Graveyard): the zone a
		// countered CARD goes to instead of the default graveyard. Only the
		// three plain hand-off zones are honoured -- Battlefield (Desertion's
		// take-control), Library and the TopOfLibrary/BottomOfLibrary forms
		// (Memory Lapse) need control/library-position machinery a plain move
		// cannot express, so those record a Note and take the default rather
		// than moving a spell somewhere the card text never asked for.
		to := state.ZGraveyard
		if dest := strings.TrimSpace(sa.ParamStr(cards.PKDestination)); dest != "" {
			switch dest {
			case "Hand", "Graveyard", "Exile":
				to, _ = parseZone(dest)
			default:
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "counter destination " + dest + " is not a plain hand-off zone; the card goes to the graveyard"})
			}
		}
		// CR 702.34a: a flashback spell is exiled instead of going anywhere
		// else when it leaves the stack -- but an explicit non-graveyard
		// destination (Remand's hand) is that anywhere-else, so the override
		// applies only on the graveyard/default path. CR 702.85a: the same
		// "then exile it" covers an Aftermath half's cast, CR 702.84a a
		// jump-start cast, and harmonize's "exile it instead of putting it
		// into your graveyard" -- every way the spell leaves the stack,
		// including being countered. One shared predicate (state.
		// ExilesLeavingStack) so a new keyword in this family cannot be
		// added to rules' readers and missed here.
		if state.ExilesLeavingStack(o.CastFlags) && to == state.ZGraveyard {
			to = state.ZExile
		}
		if remember {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			eventRemember(h, c, o.ID)
		}
		if rememberCMC {
			if f := o.Face(); f != nil {
				c.RememberedCMC += f.Cmc()
			}
			c.RememberedCMCBound = true
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID,
			From: state.ZStack, To: to, Text: "countered"})
	}
}

// effDelayedTrigger implements Mode$ Phase delayed triggers -- the
// "at the beginning of the next end step, return it" shape (Flickerwisp and
// its family, CR 603.7). It registers a delayed trigger by emitting a
// DelayedRegister event, which events.Apply folds into state.Game.Delayed so
// the registration survives replay (a delayed trigger is registered during
// one resolution and fires later, in general a different turn). The engine
// then, on entering the registered phase, mints a triggered-ability stack
// object for it through events.DelayedPush and it resolves like any other
// triggered ability.
//
// The registration carries the source object (whose face's SVar table holds
// the Execute$ sub-ability), the controller, the phase to fire in, the
// Execute$ SVar name, and the Remembered captured at registration -- which
// is what a later Defined$ DelayTriggerRememberedLKI resolves against when
// the delayed trigger fires (Flickerwisp's DelTrig remembers the exiled
// permanent via the ChangeZone's RememberChanged$ True, so TrigBounce knows
// which object to return).
//
// Mode$ Phase and the event-matched delayed modes all use the same
// registration event. Event-matched bodies are stored inline because a
// DelayedTrigger SA is not itself an SVar that events.Apply could resolve.
func effDelayedTrigger(h Host, c *Ctx, sa *cards.SA) {
	mode := strings.TrimSpace(sa.ParamStr(cards.PKMode))
	if mode == "SpellCast" {
		effDelayedTriggerSpellCast(h, c, sa)
		return
	}
	if mode != "Phase" && !effectOneShotDelayedMode(mode) {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "registers a delayed trigger at " + mode + " (not implemented)"})
		return
	}
	var step state.Step
	if mode == "Phase" {
		set, unknown := state.ParsePhases(sa.ParamStr(cards.PKPhase))
		if len(unknown) > 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed trigger at unrecognized phase " + sa.ParamStr(cards.PKPhase)})
			return
		}
		// Register the first listed phase still ahead; firing consumes the
		// registration, so a multi-step Phase$ value remains one-shot.
		var ok bool
		step, ok = state.EarliestAfter(set, h.Game().Step)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed trigger with no Phase"})
			return
		}
	}
	// An absent RememberObjects$ (and the bare RememberedLKI spelling) keeps
	// the resolving chain's capture. Any other recognised value REPLACES that
	// capture, including with an empty set: the delayed body acts on the
	// objects its own parameter names, not also on the card/player that led to
	// this chain (Kharasha Foothills and Shredder, Shadow Master). An unknown
	// value is loud and preserves the historical chain-capture fallback.
	remembered := c.Remembered
	replacedRemembered := false
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKRememberObjects)); spec != "" && spec != "RememberedLKI" {
		if ts, known := knownDefinedTargets(h, c, spec); known {
			remembered = copyTargets(ts)
			replacedRemembered = true
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unmodelled DelayedTrigger RememberObjects$ " + spec})
		}
	}
	// NextTurn$ True (Mishra's/Urza's/Lodestone Bauble's slowtrip: "draw a
	// card at the beginning of the NEXT turn's upkeep"): the one-shot fires
	// in a LATER turn only. The decode folds Amount into the registration's
	// MinTurn (the same bound the ExtraTurn grant's registration carries),
	// so the first Upkeep still inside the current turn does not consume the
	// registration — the exact defect a bauble activated during its own
	// upkeep would otherwise hit. An absent flag keeps the unbounded fire
	// every earlier registration had (Amount zero).
	amount := int32(0)
	if strings.EqualFold(strings.TrimSpace(sa.Params["NextTurn"]), "True") {
		amount = int32(h.Game().Turn + 1)
	}
	// The registration's ValidPlayer$ rides the event's Text next to the
	// phase: "<Phase>|VP=<value>". The rules-side delayed scan gates the
	// fire on it at the phase occurrence (Necropotence's "YOUR next end
	// step" -- a phase the gate fails leaves the one-shot registration
	// pending for the first later occurrence that matches), and the view
	// layer strips the suffix for display.
	text := sa.ParamStr(cards.PKPhase)
	if vp := strings.TrimSpace(sa.ParamStr(cards.PKValidPlayer)); vp != "" {
		text += "|VP=" + vp
	}
	// The Phase registration's IsPresent$/PresentZone$/PresentCompare$
	// condition (Bank Job's "at the beginning of the next end step, if that
	// card is still exiled") rides the same Text as further pipe suffixes.
	// The rules-side delayed scan gates the fire on it at the phase
	// occurrence, evaluating Card.IsTriggerRemembered against the
	// registration's own remembered capture; a registration with no
	// IsPresent$ carries none of the spelling and fires ungated exactly as
	// before. The values are Forge tokens with no "|", so the decode's
	// LastIndex strips are exact.
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKIsPresent)); spec != "" {
		text += "|IP=" + spec
		if zone := strings.TrimSpace(sa.ParamStr(cards.PKPresentZone)); zone != "" {
			text += "|PZ=" + zone
		}
		if cmp := strings.TrimSpace(sa.ParamStr(cards.PKPresentCompare)); cmp != "" {
			text += "|PC=" + cmp
		}
	}
	// RememberChain$ False (this repo's own generated-SA param, the
	// Annihilator$-marker precedent: no raw corpus card carries it, only
	// cards/keywords.go's generated Mobilize delay SVar does): the
	// registration keeps only what THIS resolving chain itself remembered
	// beyond the referents its triggering event captured -- Ctx.Captured is
	// exactly the part of Remembered the trigger put there (the attacking
	// creature, the defending player), RememberTokens$ True put the minted
	// tokens in the chain part -- so Mobilize's end-step sacrifice touches
	// the Warrior tokens and never the creature that merely triggered. The
	// default (absent) keeps the whole-chain capture every earlier
	// registration had, byte for byte.
	if !replacedRemembered && strings.EqualFold(strings.TrimSpace(sa.Params["RememberChain"]), "False") {
		chain := make([]state.Target, 0, len(c.Remembered))
		for _, t := range c.Remembered {
			captured := false
			for _, cp := range c.Captured {
				if cp == t {
					captured = true
					break
				}
			}
			if !captured {
				chain = append(chain, t)
			}
		}
		remembered = chain
	}
	if mode != "Phase" {
		exec := strings.TrimSpace(sa.ParamStr(cards.PKExecute))
		if exec == "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed " + mode + " trigger with no Execute"})
			return
		}
		eventText := mode + ":" + delayedTriggerBody(sa)
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKThisTurn)), "True") {
			eventText += "|TT=" + strconv.Itoa(int(h.Game().Turn))
		}
		h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
			Player: c.Controller, Step: h.Game().Step, Counter: exec,
			IDs: encodeRemembered(remembered), Text: eventText})
		return
	}
	exec := strings.TrimSpace(sa.ParamStr(cards.PKExecute))
	if exec == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "registers a delayed trigger with no Execute"})
		return
	}
	h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
		Player: c.Controller, Step: step, Counter: exec, Amount: amount,
		IDs: encodeRemembered(remembered), Text: text})
}

// delayedTriggerBody serializes the trigger parameters in a fixed order. A
// map iteration here would make the event bytes (and therefore replay heads)
// nondeterministic.
func delayedTriggerBody(sa *cards.SA) string {
	// Literal keys at both the read and append sites keep the parameter census
	// attributable; call order fixes the registration's replay-visible bytes.
	parts := []string{"Mode$ " + strings.TrimSpace(sa.ParamStr(cards.PKMode))}
	add := func(prefix, value string) {
		if v := strings.TrimSpace(value); v != "" {
			parts = append(parts, prefix+v)
		}
	}
	add("ValidCard$ ", sa.ParamStr(cards.PKValidCard))
	add("ValidCards$ ", sa.ParamStr(cards.PKValidCards))
	add("Origin$ ", sa.ParamStr(cards.PKOrigin))
	add("Destination$ ", sa.ParamStr(cards.PKDestination))
	add("ExcludedOrigins$ ", sa.Params["ExcludedOrigins"])
	add("ValidSource$ ", sa.ParamStr(cards.PKValidSource))
	add("ValidTarget$ ", sa.ParamStr(cards.PKValidTarget))
	add("CombatDamage$ ", sa.Params["CombatDamage"])
	add("ValidAttackers$ ", sa.Params["ValidAttackers"])
	add("ValidAttackersAmount$ ", sa.Params["ValidAttackersAmount"])
	add("AttackingPlayer$ ", sa.Params["AttackingPlayer"])
	add("AttackedTarget$ ", sa.Params["AttackedTarget"])
	add("ValidPlayer$ ", sa.ParamStr(cards.PKValidPlayer))
	add("ValidOriginalController$ ", sa.Params["ValidOriginalController"])
	add("ValidActivatingPlayer$ ", sa.Params["ValidActivatingPlayer"])
	add("PlayerTurn$ ", sa.ParamStr(cards.PKPlayerTurn))
	add("ValidSA$ ", sa.ParamStr(cards.PKValidSA))
	add("TriggerZones$ ", sa.ParamStr(cards.PKTriggerZones))
	add("ActiveZones$ ", sa.ParamStr(cards.PKActiveZones))
	add("ThisTurn$ ", sa.ParamStr(cards.PKThisTurn))
	add("Static$ ", sa.ParamStr(cards.PKStatic))
	add("IsPresent$ ", sa.ParamStr(cards.PKIsPresent))
	add("PresentDefined$ ", sa.ParamStr(cards.PKPresentDefined))
	add("PresentCompare$ ", sa.ParamStr(cards.PKPresentCompare))
	add("PresentZone$ ", sa.ParamStr(cards.PKPresentZone))
	return strings.Join(parts, " | ")
}

// effDelayedTriggerSpellCast registers the event-matched delayed shape: a
// Mode$ SpellCast DelayedTrigger (Mistrise Village's "{U}, {T}: The next
// spell you cast this turn can't be countered") fires on a spell's
// PutOnStack exactly like checkEventDelayedTriggers' keyword-minted
// registrations do. The registration is one-shot (the DelayedPush that
// fires it removes it), so "the NEXT spell" is exactly one spell. The
// SA's own trigger clauses (ValidCard$, ValidActivatingPlayer$) are stored
// INLINE in the event's Text — a face Ability's DelayedTrigger has no SVar
// name of its own for the decode to reference — and the fire-time matcher
// re-parses them against the actual cast. ThisTurn$ True (Mistrise) bounds
// the registration to the CURRENT turn ("...you cast THIS TURN"): the
// expiry rides "|TT=<turn>" and folds into state.DelayedTrigger.MaxTurn;
// a turn that ends with the registration unfired leaves it inert forever
// (skipped, never removed — removal would need its own event). Static$
// True (the corpus's only value, 8 raw DelayedTrigger lines) marks Forge's
// static-style registration; every registration here is already
// source-independent once created (CR 603.7), so the gate below documents
// the carrier and a future non-True value gets the loud Note the
// fail-closed convention takes.
func effDelayedTriggerSpellCast(h Host, c *Ctx, sa *cards.SA) {
	if st := strings.TrimSpace(sa.ParamStr(cards.PKStatic)); st != "" && !strings.EqualFold(st, "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unmodelled DelayedTrigger Static$ " + st})
	}
	exec := strings.TrimSpace(sa.ParamStr(cards.PKExecute))
	if exec == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "registers a delayed SpellCast trigger with no Execute"})
		return
	}
	body := "Mode$ SpellCast"
	// Each clause unrolled over its explicit key: the census's rot guard
	// refuses a dynamic Params key it cannot attribute, and four explicit
	// reads cannot hide one. Static$ rides the body too — Forge's static
	// delayed trigger resolves its Execute IMMEDIATELY at fire time (no
	// stack push), which is what makes Mistrise's promise active before the
	// opponent can respond.
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidCard)); v != "" {
		body += " | ValidCard$ " + v
	}
	if v := strings.TrimSpace(sa.Params["ValidActivatingPlayer"]); v != "" {
		body += " | ValidActivatingPlayer$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidPlayer)); v != "" {
		body += " | ValidPlayer$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKPlayerTurn)); v != "" {
		body += " | PlayerTurn$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidSA)); v != "" {
		body += " | ValidSA$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKStatic)); v != "" {
		body += " | Static$ " + v
	}
	text := "SpellCast:" + body
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKThisTurn)), "True") {
		text += "|TT=" + strconv.Itoa(int(h.Game().Turn))
	}
	h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
		Player: c.Controller, Step: h.Game().Step, Counter: exec, Text: text})
}

// encodeRemembered turns a Remembered target list into the []ObjID an event
// carries, PlayerRef-encoding a player target the same way rules.pushTrigger
// does (FL-41) so events.Apply's rememberedFrom decodes it back to a player
// target rather than a zero object id. It mirrors the rule in effects since
// effects cannot import rules.
func encodeRemembered(remembered []state.Target) []state.ObjID {
	var out []state.ObjID
	for _, t := range remembered {
		if t.IsPlayer {
			out = append(out, state.PlayerRef(t.Player))
			continue
		}
		out = append(out, t.Obj)
	}
	return out
}

// effRepeat runs RepeatSubAbility$ MaxRepeat$ times -- the fetched corpus's
// real parameter name. RepeatNum$ (the Task 18 brief's name, which real
// cards never use) is still honoured, as a fallback for anything that
// predates MaxRepeat$. Either way the run count goes through Num(), so an
// SVar-indirected Count$ works for either name. It is capped at 1000 so a
// malformed or absurdly large repeat can never spin the engine.
//
// A Repeat carrying RepeatCheckSVar$/RepeatSVarCompare$ (Forge's
// repeat-while gate; 28 corpus files) is gate-governed instead: the gate is
// the between-iteration condition (repeatGateHolds below), re-evaluated
// after every iteration because the body rewrites the named SVar
// (StoreSVar's accumulator) or grows the remembered set it reads
// (RememberMilled$) -- Grist's [+1] and Scalpelexis both loop on exactly
// that. MaxRepeat$ (when present and resolvable) is then the CAP, and
// Forge's unbounded default is clamped to the same 1000. A gate the
// evaluator cannot read stops the loop after the iteration just run -- the
// pre-gate single-iteration behaviour, never a spin: an unevaluated gate
// must not stand in for "the condition holds" (a count body whose filter
// predicates fail closed to 0 under an EQ0 compare would otherwise loop to
// the cap on a number the engine cannot honestly compute). Grindstone's and
// Sphinx's Tutelage's `Remembered$Valid ...SharesColorWithOther Remembered`
// gate is evaluated (wordSharesColorOther), as is The Tale of Tamiyo's
// sharesCardTypeWithOther one, so those repeat while two milled cards share
// a colour (a card type), capped by MaxRepeat$ CardsInLibrary.
func effRepeat(h Host, c *Ctx, sa *cards.SA) {
	check := strings.TrimSpace(sa.Params["RepeatCheckSVar"])
	cmp := strings.TrimSpace(sa.Params["RepeatSVarCompare"])
	defined := strings.TrimSpace(sa.Params["RepeatDefined"])
	present := strings.TrimSpace(sa.Params["RepeatPresent"])
	gated := check != "" || defined != ""
	optional := strings.EqualFold(strings.TrimSpace(sa.Params["RepeatOptional"]), "True")
	n := Num(h, c, sa, "MaxRepeat", -1)
	if n < 0 {
		if gated {
			// Gate-governed: Forge's default cap is unbounded (the gate
			// decides when to stop); clamp to the same 1000-iteration cap a
			// malformed MaxRepeat takes.
			n = 1000
		} else if optional {
			// RepeatOptional$ is an open-ended do/while election. The cap is
			// only a malformed-input guard; the player decides when to stop.
			n = 1000
		} else {
			n = Num(h, c, sa, "RepeatNum", 1)
		}
	}
	if n < 0 {
		n = 0
	}
	if n > 1000 {
		n = 1000
	}
	name := sa.Params["RepeatSubAbility"]
	if name == "" || c.SVars == nil {
		return
	}
	sub := cards.ResolveSVar(c.SVars, name)
	if sub == nil {
		return
	}
	start := int32(0)
	// askElection marks a resume that must FIRST pose the repeat election for
	// `start`, then run that iteration's body only if the player says yes. It
	// is the state a RepeatOptional$ BODY suspension leaves behind: the body
	// of iteration start-1 completed after its ask was answered, so the
	// do/while election owed for iteration start has not been posed yet. It
	// is distinct from a completed election answered yes, which begins the
	// next body with no further election (see RepeatOptionalContinuation).
	askElection := false
	if c.RepeatOptional != nil {
		if !c.RepeatOptional.Continue {
			return
		}
		start = c.RepeatOptional.Next
		askElection = c.RepeatOptional.AskElection
	}
	for i := start; i < n; i++ {
		if askElection {
			// The previous iteration's body completed after suspending. Its
			// between-iteration gate is owed before the repeat election, just
			// like the ordinary post-body path below: a false or unreadable
			// gate stops the do/while without offering another iteration.
			if gated {
				holds, evaluated := repeatGateEvaluates(h, c, sa, check, cmp, defined, present)
				if !evaluated || !holds {
					return
				}
			}
			// Pose the repeat election that iteration i's body has not yet
			// earned (CR 608.2c's do/while). The election concerns iteration
			// i, so a yes resumes the body at i, not i+1.
			askElection = false
			if !poseRepeatOptionalElection(h, c, sa, i) {
				return // R-9: a host that cannot answer stops here.
			}
			return
		}
		mark, marked := eventMark(h)
		asksBefore := askCount(h)
		Resolve(h, c, sub)
		if h.Suspended() {
			// A RepeatOptional body can itself ask (Forbidden Ritual's
			// sacrifice/choice chain is the corpus example). Preserve the loop
			// cursor so the answered body re-enters the repeat and poses the
			// repeat election for the NEXT iteration instead of falling
			// through to Repeat.Sub.
			if optional {
				h.SuspendRepeatOptional(sa, i+1)
			}
			return
		}
		if gated {
			holds, evaluated := repeatGateEvaluates(h, c, sa, check, cmp, defined, present)
			if !evaluated || !holds {
				break
			}
			if !optional && marked && askCount(h) == asksBefore && !stateChangedSince(h, mark) {
				// A gate-governed repeat whose iteration posed no decision and
				// changed nothing re-runs the identical body from the identical
				// state: the gate holds identically forever, so every further
				// iteration up to the cap is the same no-op (Rally the Horde
				// over an empty library: nothing is exiled, "the last card
				// exiled isn't a land" keeps holding -- 1000 empty passes, a
				// livelock to the watcher; cardfuzz batch8 line 4). End the
				// loop here, the optional arm's CR 732.2a shortcut below.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "the repeated process changed nothing; it is not repeated again"})
				break
			}
		}
		if optional {
			if i+1 >= n {
				return
			}
			if marked && askCount(h) == asksBefore && !stateChangedSince(h, mark) {
				// The iteration posed no decision and changed nothing (only
				// Notes: Forbidden Ritual's "sacrifice a nontoken permanent"
				// once none is left, its GenericChoice gated off, its Cleanup
				// clearing an empty Remembered). A body with no decision run
				// from an unchanged state is the same no-op every time, so
				// every number of further repeats yields this same state (CR
				// 732.2a's shortcut): end the do/while instead of offering an
				// election whose "repeat" answer can only loop forever.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "the repeated process changed nothing; it is not offered again"})
				return
			}
			if !poseRepeatOptionalElection(h, c, sa, i+1) {
				return // R-9: a host that cannot answer stops after one pass.
			}
			return
		}
		if !gated {
			continue
		}
		// The gate was evaluated before the optional election.
	}
}

// repeatGateEvaluates evaluates a Repeat's full between-iteration gate: the
// RepeatCheckSVar$/RepeatSVarCompare$ pair and, when the line names one, the
// RepeatDefined$/RepeatPresent$ pair (RepeatCompare$ overrides the compare;
// an absent RepeatCompare$ with no check gate falls back to cmp). Both the
// ordinary post-body path and the AskElection resume path call it, so a
// gated optional repeat cannot skip its gate by suspending inside the body.
func repeatGateEvaluates(h Host, c *Ctx, sa *cards.SA, check, cmp, defined, present string) (holds, evaluated bool) {
	holds, evaluated = repeatGateHolds(h, c, check, cmp)
	if defined == "" {
		return holds, evaluated
	}
	definedCmp := strings.TrimSpace(sa.Params["RepeatCompare"])
	if definedCmp == "" && check == "" {
		definedCmp = cmp
	}
	definedHolds, definedEvaluated := repeatDefinedGateHolds(h, c, sa, defined, present, definedCmp)
	return holds && definedHolds, evaluated && definedEvaluated
}

// poseRepeatOptionalElection asks the RepeatOptional$ "Repeat this process?"
// election for the iteration `next` whose body a yes would run, parking the
// loop cursor on it (ResumeRepeatNext = next). RepeatOptionalDecider$
// Remembered routes the ask to the remembered player when the line names
// one. It returns h.Ask(d): false when the host cannot answer, the R-9
// deterministic stop after one pass.
func poseRepeatOptionalElection(h Host, c *Ctx, sa *cards.SA, next int32) bool {
	player := c.Controller
	if strings.TrimSpace(sa.Params["RepeatOptionalDecider"]) == "Remembered" {
		for _, t := range c.Remembered {
			if t.IsPlayer {
				player = t.Player
				break
			}
		}
	}
	d := &decision.Decision{Player: player, Kind: decision.KChoose,
		Min: 1, Max: 1, Prompt: "Repeat this process?", Source: c.Source,
		ResumeKind: "repeat_optional", ResumeSA: sa,
		ResumeRepeatNext: next,
		Options: []decision.Option{{Index: 0, Kind: "yes", Label: "Repeat", Player: player},
			{Index: 1, Kind: "no", Label: "Stop", Player: player}}}
	return h.Ask(d)
}

// repeatGateHolds evaluates one Repeat's between-iteration gate -- the
// RepeatCheckSVar$/RepeatSVarCompare$ pair. holds is the compare's answer;
// evaluated is false when the gate cannot be read here: the named SVar (the
// ctx table first, then the source face's own -- the same lookup
// CheckSVarHolds makes) resolves to a body whose filter predicates this
// build does not know (UnknownPredicates -- the same unresolved guard
// conditions.go's present-count gates take), or whose count/compare
// EvalCountOK does not model. An absent cmp is Forge's GE1 default;
// CheckSVarHolds reads an empty compare as "nonzero", the same answer for
// every count.
func repeatGateHolds(h Host, c *Ctx, check, cmp string) (holds, evaluated bool) {
	if check == "" {
		return true, true // no gate; the loop's own run count governs
	}
	if _, ok := sourceRuntimeSVar(h.Game(), c, check); ok {
		// A StoreSVar write shadows the printed body, so the printed
		// body's predicates are irrelevant (CheckSVarHolds reads the store).
		return CheckSVarHolds(h, c, check, cmp)
	}
	body := check
	if c.SVars != nil {
		if b, ok := c.SVars[check]; ok {
			body = b
		}
	}
	if body == check && c.Source != 0 {
		if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
			if b, ok := o.Face().SVars[check]; ok {
				body = b
			}
		}
	}
	if len(UnknownPredicates(body)) > 0 {
		return false, false
	}
	return CheckSVarHolds(h, c, check, cmp)
}

// repeatDefinedGateHolds evaluates the RepeatDefined$/RepeatPresent$ gate.
// Only the measured Remembered and Imprinted selectors are admitted: unlike
// ordinary Defined resolution, an unknown selector must not fall back to the
// source object and accidentally make an EQ0 gate repeat forever.
func repeatDefinedGateHolds(h Host, c *Ctx, sa *cards.SA, defined, present, compare string) (holds, evaluated bool) {
	if defined != "Remembered" && defined != "Imprinted" {
		return false, false
	}
	copySA := *sa
	copySA.Params = map[string]string{"Defined": defined}
	objects := Defined(h, c, &copySA)
	if present != "" && len(UnknownPredicates(present)) != 0 {
		return false, false
	}
	sc := c.SpecContext(c.Controller)
	count := 0
	for _, target := range objects {
		if target.IsPlayer {
			continue
		}
		o := h.Game().Obj(target.Obj)
		if o == nil {
			return false, false
		}
		if present == "" || MatchesObjectCtx(h.Game(), present, o, sc) {
			count++
		}
	}
	return evalConditionCount(count, compare)
}

// CharmRepeatModes reports whether a Charm's CanRepeatModes$ True grants
// CR 601.2b's "you may choose the same mode more than once": the mode pick
// becomes an ordered multiset over the distinct Choices$ modes, so the same
// mode may fill several of the CharmNum$ slots. Measured at the corpus pin:
// 23 files, every one api:Charm, every one the literal "True" (the Confluence
// cycle, Fiery Confluence, Moment of Reckoning, the Commands cycle).
