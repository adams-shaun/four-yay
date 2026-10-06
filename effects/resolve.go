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
	if strings.EqualFold(sa.ParamStr(cards.PKClearRemembered), "True") {
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
	if strings.EqualFold(sa.ParamStr(cards.PKClearChosenCard), "True") {
		c.Chosen = keepChosenPlayers(c.Chosen)
		if c.Source != 0 {
			if o := h.Game().Obj(c.Source); o != nil && hasChosenCards(o.Chosen) {
				h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "clear-chosen-card"})
				noted = true
			}
		}
	}
	if strings.EqualFold(sa.ParamStr(cards.PKClearChosenPlayer), "True") {
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
// the pre-ask always-change. A decline changes nothing and the chained
// SubAbility$ still runs (the chain is owned by Resolve, never by a decline).
func effSetState(h Host, c *Ctx, sa *cards.SA) {
	optAns := ""

	mode := sa.ParamStr(cards.PKMode)
	turnUp := strings.EqualFold(strings.TrimSpace(mode), "TurnFaceUp")
	turnDown := strings.EqualFold(strings.TrimSpace(mode), "TurnFaceDown")
	unspecialize := strings.EqualFold(strings.TrimSpace(mode), "Unspecialize")
	optional := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True")
	if optional {
		// Pose the yes/no election -- but only when the change would
		// actually do something; with nothing to change, decline and accept
		// are the same, so no ask (the Attach precedent's len(legal) == 0
		// gate). No served answer ends the effect (R-9); the clamp-answered
		// bot path answers option 0 = "yes", so a bot game stays
		// byte-identical to the pre-ask always-change.
		if setStateWouldChange(h, c, sa, turnUp, turnDown, unspecialize) {
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "setstate_optional", ResumeSA: sa,
				Prompt: "Change this permanent's face?",
				Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes", Player: c.Controller},
					{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
				}}
			ans, ok := AskTape(h, d)
			if !ok {
				return
			}
			// The resolution kernel's answer in hand.
			optAns = "no"
			if len(ans) > 0 && ans[0].Kind == "yes" {
				optAns = "yes"
			}
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
				setType := strings.TrimSpace(sa.ParamStr(cards.PKFaceDownSetType))
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
// digRemember (dig.go), and it is Ctx-only, never the persistent
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
	remember := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberCountered)), "True") ||
		strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberCounteredSA)), "True")
	// RememberCounteredCMC$ (task counter-cmc): remember each countered
	// spell's mana VALUE -- Electrosiphon's "an amount of {E} equal to its
	// mana value", Overwhelming Intellect's draw family (14 corpus carriers,
	// every one reading it back through SVar:X:Count$RememberedNumber). The
	// number lands on the Ctx channel above; an ABILITY has no mana value
	// and contributes nothing.
	rememberCMC := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberCounteredCMC)), "True")
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
			switch effCounterCodes.Code(string(dest)) {
			case effCounterZone:
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
				c.Num.RememberedCMC += f.Cmc()
			}
			c.Num.RememberedCMCBound = true
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID,
			From: state.ZStack, To: to, Text: "countered"})
	}
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
	check := strings.TrimSpace(sa.ParamStr(cards.PKRepeatCheckSVar))
	cmp := strings.TrimSpace(sa.ParamStr(cards.PKRepeatSVarCompare))
	defined := strings.TrimSpace(sa.ParamStr(cards.PKRepeatDefined))
	present := strings.TrimSpace(sa.ParamStr(cards.PKRepeatPresent))
	gated := check != "" || defined != ""
	optional := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRepeatOptional)), "True")
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
	name := sa.ParamStr(cards.PKRepeatSubAbility)
	if name == "" || c.SVars == nil {
		return
	}
	sub := cards.ResolveSVar(c.SVars, name)
	if sub == nil {
		return
	}
	for i := int32(0); i < n; i++ {
		mark, marked := eventMark(h)
		asksBefore := askCount(h)
		Resolve(h, c, sub)
		if h.Suspended() {
			// The body opened a resolution-time payment window.
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
			// A yes runs iteration i+1's body, a no (or no answer) ends the loop.
			if !repeatOptionalElectionYes(h, c, sa, i+1) {
				return
			}
			continue
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
// an absent RepeatCompare$ with no check gate falls back to cmp).
func repeatGateEvaluates(h Host, c *Ctx, sa *cards.SA, check, cmp, defined, present string) (holds, evaluated bool) {
	holds, evaluated = repeatGateHolds(h, c, check, cmp)
	if defined == "" {
		return holds, evaluated
	}
	definedCmp := strings.TrimSpace(sa.ParamStr(cards.PKRepeatCompare))
	if definedCmp == "" && check == "" {
		definedCmp = cmp
	}
	definedHolds, definedEvaluated := repeatDefinedGateHolds(h, c, sa, defined, present, definedCmp)
	return holds && definedHolds, evaluated && definedEvaluated
}

// repeatOptionalElectionYes asks the RepeatOptional$ "Repeat this process?"
// election for the iteration next through the resolution kernel (AskTape)
// and reports whether the answer is yes. RepeatOptionalDecider$ Remembered
// routes the ask to the remembered player. No served answer is a stop.
func repeatOptionalElectionYes(h Host, c *Ctx, sa *cards.SA, next int32) bool {
	ans, ok := AskTape(h, repeatOptionalDecision(c, sa, next))
	return ok && len(ans) > 0 && ans[0].Kind == "yes"
}

// repeatOptionalDecision is the RepeatOptional$ election for iteration next.
func repeatOptionalDecision(c *Ctx, sa *cards.SA, next int32) *decision.Decision {
	player := c.Controller
	if strings.TrimSpace(sa.ParamStr(cards.PKRepeatOptionalDecider)) == "Remembered" {
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
		Options: []decision.Option{{Index: 0, Kind: "yes", Label: "Repeat", Player: player},
			{Index: 1, Kind: "no", Label: "Stop", Player: player}}}
	return d
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
	// The predicate census reads the filter part only: an arithmetic suffix
	// (SVar$Wins/LimitMin.Losses, SVar$ChoiceNum/Times.CheckNotPaid) is not a
	// filter, and its dotted operand used to read as an unknown predicate
	// ("Losses"), so the gate never evaluated and the loop stopped after its
	// first iteration. The suffix is judged by the evaluator's own op
	// grammar instead.
	pred, op, hasOp := strings.Cut(body, "/")
	if hasOp && !modelledGateOp(h, c, op, 0) {
		return false, false
	}
	if len(UnknownPredicates(pred)) > 0 {
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
	objects := DefinedSpec(h, c, defined)
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

type effCounterCode uint16

const (
	effCounterZone effCounterCode = iota + 1
)

var effCounterCodes = state.NewStrCodes(
	state.StrEntry[effCounterCode]{Key: "Hand", Val: effCounterZone},
	state.StrEntry[effCounterCode]{Key: "Graveyard", Val: effCounterZone},
	state.StrEntry[effCounterCode]{Key: "Exile", Val: effCounterZone},
)
