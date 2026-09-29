package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Mana", effMana)
	Register("ReplaceMana", effReplaceMana)
	Register("Effect", effEffect)
	Register("Cleanup", effCleanup)
	Register("SetState", effSetState)
	Register("Counter", effCounter)
	Register("DelayedTrigger", effDelayedTrigger)
	Register("Repeat", effRepeat)
	Register("Charm", effCharm)
	Register("GenericChoice", effCharm)
	Register("VillainousChoice", effVillainousChoice)
	Register("Vote", effVote)
	Register("BecomeMonarch", effBecomeMonarch)
	Register("RingTemptsYou", effRingTemptsYou)
	Register("RestartGame", effRestartGame)
	Register("Goad", effGoad)
	Register("AlterAttribute", effAlterAttribute)
	Register("Ward", effWard)
}

// effGoad records each independently-lived goad relationship. Duration and
// source are event payload so replay can expire conditional goads identically.
//
// RememberGoaded$ True (2 corpus files: Havoc Eater, Kaima the Fractured
// Calm) makes the resolution remember each goaded creature, so the chained
// SubAbility$ (both carriers' DB$ PutCounter reading SVar:Y:Remembered$
// CardPower — "X +1/+1 counters, where X is the total power of creatures
// goaded this way") reads exactly what was goaded. Ctx is threaded by
// pointer through Resolve, so appending here is visible to the sub-ability
// without any state write — the same per-resolution ctx Remembered the
// RememberDamaged$ arm of DealDamage takes (damage.go), replay re-derived by
// re-running the resolution. Only the goad-granting arm remembers; a
// NoLonger$ release remembers nothing (its corpus shape never pairs the
// rider with a release).
func effGoad(h Host, c *Ctx, sa *cards.SA) {
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberGoaded"]), "True")
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
			if strings.EqualFold(sa.Params["NoLonger"], "True") {
				h.Emit(events.Event{Kind: events.Goad, Obj: o.ID, Amount: -1})
				continue
			}
			duration := sa.Params["Duration"]
			if duration == "" {
				duration = "UntilYourNextTurn"
			}
			h.Emit(events.Event{Kind: events.Goad, Obj: o.ID, Player: c.Controller,
				Text: duration, IDs: []state.ObjID{c.Source}, Amount: int32(o.Controller) + 1})
			if remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			}
		}
	}
}

// effAlterAttribute applies Forge's AlterAttribute effect: it flips a
// designation attribute on each resolved target (task alterattr1). Targets
// come through the ordinary Defined path, so a body with no Defined$ asks
// its ValidTgts$ targets the way every other targeting primitive does --
// Nelly Borca's "whenever it attacks, suspect target creature" gets its ask
// from the trigger's placement ask, Hot Pursuit's ETB the same way, and a
// deeper sub (the DBDebuff family) through Resolve's generic pre-ask.
//
// The engine models TWO attributes: Suspected (CR 702.157, the Blame Game
// precon family) and Prepared (CR 722.3a, the Secrets of Strixhaven
// preparation cards). Both designations live on state.Object behind the
// events.AlterAttribute fold; Suspected's two end conditions (leaves the
// battlefield, another player gains control) and Prepared's (leaves the
// battlefield, or an unprepare effect) are events.Apply's folds -- Prepared
// deliberately keeps its designation across a control change, because CR
// 722.3c ties the copy to the permanent, not to a controller.
// A body naming any other attribute (Solved, Plotted, Saddled, Commander,
// Harnessed -- the corpus's remaining populations) emits the loud
// unsupported-attribute Note and moves nothing, exactly like the
// Manifest/Cloak out-of-scope shapes: registration claims the API, the Note
// claims the gap.
//
// Activate$ False is Forge's removal spelling ("becomes unprepared"); for
// Suspected it removes the designation (the DBDebuff family's
// "un-suspect an opponent's suspected creature" shape).
func effAlterAttribute(h Host, c *Ctx, sa *cards.SA) {
	attr := strings.TrimSpace(sa.Params["Attributes"])
	if attr == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "AlterAttribute names no Attributes$"})
		return
	}
	activate := !strings.EqualFold(strings.TrimSpace(sa.Params["Activate"]), "False")
	for _, name := range strings.FieldsFunc(attr, func(r rune) bool { return r == ',' || r == ' ' }) {
		prepared := strings.EqualFold(name, "Prepared")
		if !strings.EqualFold(name, "Suspected") && !prepared {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "AlterAttribute: attribute " + name + " not modelled"})
			continue
		}
		amount := int32(1)
		if !activate {
			amount = -1
		}
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				continue
			}
			o := h.Game().Obj(t.Obj)
			if o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			// CR 722.3a: only a permanent that HAS a prepare spell can gain
			// the prepared designation; an unprepare (Activate$ False) always
			// applies, exactly like Suspected's removal.
			if prepared && activate && !o.HasPrepareSpell() {
				continue
			}
			text := "Suspected"
			if prepared {
				text = "Prepared"
			}
			h.Emit(events.Event{Kind: events.AlterAttribute, Obj: o.ID,
				Text: text, Amount: amount})
		}
	}
}

// effWard is the resolution half of the Ward keyword trigger. The triggering
// spell/ability is held in TriggerSource; after a declined payment it is
// countered and an ability is parked in exile (CR 608.2m).
func effWard(h Host, c *Ctx, sa *cards.SA) {
	cause := c.TriggerStack
	o := h.Game().Obj(cause)
	if o == nil || o.Zone != state.ZStack {
		return
	}
	if c.UnlessPay == "" {
		cost := sa.Params["UnlessCost"]
		d := &decision.Decision{Player: o.Controller, Kind: decision.KModes, Min: 1, Max: 1,
			Prompt: "Pay " + cost + " for ward?", ResumeKind: "unless_pay", ResumeSA: sa,
			Options: []decision.Option{{Index: 0, Kind: "mode", Label: "Pay " + cost, Player: o.Controller, Mode: decision.ModeUnlessPay}, {Index: 1, Kind: "mode", Label: "Don't pay", Player: o.Controller, Mode: decision.ModeUnlessDecline}}}
		h.Ask(d)
		return
	}
	paid := c.UnlessPay == "pay"
	c.UnlessPay = ""
	if paid {
		return
	}
	to := state.ZGraveyard
	if o.Face() == nil {
		to = state.ZExile
	}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: cause, From: state.ZStack, To: to, Text: "countered by ward"})
}

// CopySpellAbility is NOT registered. It needs to create a brand new game
// object mid-match (a copy of a spell or ability already on the stack), and
// every state mutation in this engine goes through events.Apply -- there is
// no Apply case yet that mints an ID and decides how a copy's Card/FaceIdx/
// Ability/Targets carry over. Token had the identical shape (see the Task
// 18 report for the original analysis of both) and closed it in Task 13 via
// events.TokenCreate, whose Apply case mints the new object from
// Game.Tokens; see token.go. CopySpellAbility's own object comes from
// wherever it is on the stack already, not a registry, so it needs its own
// event kind (StackCopy exists as of Task 12) wired up rather than reusing
// TokenCreate's shape verbatim. Registering it as a Note-only stub would
// make Supported()/Coverage claim a card is playable when it cannot
// actually do what its text says. Left unregistered, Resolve's existing
// "unimplemented API" Note fallback applies, and Coverage correctly
// excludes any card that needs it.

// effEffect creates a lasting effect object holding StaticAbilities$ for
// Duration$. This is the part of Task ce1 that turns the M1 Note into a real
// registration: a StaticAbilities$ entry naming a RESTRICTION static
// (CantTarget for Vines of Vastwood, CantRegenerate for Incinerate) is
// registered into the engine's continuous-effect registry (rules' layer
// system, reached through Host.AddContinuous) so the rule it modifies is
// actually consulted rather than left as a silent Note.
//
// Registration is deliberately scoped: only the CantTarget and CantRegenerate
// modes become real effects this round. Every other StaticAbilities$ mode ---
// and every Triggers$ entry (Palace Jailer's "exile until an opponent becomes
// the monarch" is a command-zone trigger this build does not model) --- is
// still recorded as a Note, so nothing silently no-ops into looking supported
// when it is not. The registry entry the engine (rules/layers.go active())
// expires is the same until-end-of-turn / source-leaves discipline every other
// continuous effect uses: an Effect from an instant or sorcery (a one-shot
// spell), an absent Duration$, or carrying an explicit this-turn Duration$ is
// UntilEOT, dropped at end-of-turn cleanup; an explicit Permanent (and other
// source-relative durations) persists while its source stays on the battlefield.
// effectContinuous registers one continuous effect created by the api:Effect
// primitive, marking it Effect-created (state.ContinuousEffect.FromEffect).
// The marker is what the source-scoped form of the one-shot self-exile ender
// (Host.EndEffectSource) keys on, so a printed static of the SAME source is
// never ended by an Effect's self-exile.
func effectContinuous(h Host, ce state.ContinuousEffect) {
	ce.FromEffect = true
	h.AddContinuous(ce)
}

func effEffect(h Host, c *Ctx, sa *cards.SA) {
	rawDur := sa.Params["Duration"]
	dur := rawDur
	if dur == "" {
		dur = "Permanent"
	}
	what := strings.TrimSpace(sa.Params["StaticAbilities"] + " " + sa.Params["Triggers"])
	// Name$ is the effect's own display name (Sephiroth's emblem, Wrenn and
	// Six's): the log names the effect after it wherever this function would
	// otherwise print a bare mode list, and the registrations below carry it
	// into the continuous-effect registry so Stackable$ can dedup by it.
	effectName := strings.TrimSpace(sa.Params["Name"])
	// Stackable$ False (Wrenn and Six's emblem): the effect does not stack.
	// Forge's EffectEffect.createEffect skips creating a second effect when an
	// un-stackable one already exists. Forge's default is STACKABLE — the
	// corpus carries Stackable$ only as "False" (38 raw lines, no "True"), so
	// the dedup gate fires ONLY on an explicit "False": an absent key keeps
	// the stacking behaviour (en-Kor's "en-Kor Redirection" redirection
	// stacking is the point of the card). The dedup ask goes through
	// Host.ContinuousNamed so the registry, not this resolution, decides
	// whether the same named effect from this controller is active.
	if stackable, present := sa.Params["Stackable"]; present &&
		strings.EqualFold(strings.TrimSpace(stackable), "False") &&
		effectName != "" && h.ContinuousNamed(c.Controller, effectName) {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "effect not stacked (" + effectName + ")"})
		return
	}
	// The two move-driven lifetimes: ForgetOnMoved$ drops a remembered card
	// from the registered effect's set when it moves to the named zone;
	// ExileOnMoved$ ENDS the effect on such a move (Vines of Vastwood's
	// blinked target). Both ride the registrations below.
	forgetOn := strings.TrimSpace(sa.Params["ForgetOnMoved"])
	exileOn := strings.TrimSpace(sa.Params["ExileOnMoved"])
	// ForgetCounter$ <kind> (task vow1): a remembered card whose count of
	// that kind reaches zero after a counter-removal leaves the registered
	// effect's Remembered set. Both this and ForgetOnMoved$ ride every
	// registration below.
	forgetCounter := strings.TrimSpace(sa.Params["ForgetCounter"])
	// ForgetOnCast$ <spec> (task param:api:Effect.ForgetOnCast): the
	// cast-driven lifetime -- the first qualifying spell cast ENDS the whole
	// effect ("the next spell you cast this turn ...", Marshland
	// Bloodcaster's alternative cost, Dark Apostle's one-cast cascade). The
	// spec is a card spec over the cast spell, You-relative to the effect's
	// controller; rules' effectCastSweep matches it at the deferred re-walk
	// of the cast's PutOnStack (payCast, after payment), so an ABORTED
	// proposal (reversed before payment, CR 733.1) never consumes the grant
	// while a completed cast -- even one later countered -- does. Forge's
	// explicit False is the no-forget default and degrades to the absent
	// read; it rides every registration below that can actually expire this
	// way (the cost-static and cascade-grant arms).
	forgetOnCast := strings.TrimSpace(sa.Params["ForgetOnCast"])
	if strings.EqualFold(forgetOnCast, "False") {
		forgetOnCast = ""
	}
	// An Effect's Triggers$ list can spell the same cast-driven lifetime the
	// ForgetOnCast$ parameter does: a SpellCast trigger whose Execute$ body
	// self-exiles the Effect (Forge's effect token leaving the Command zone --
	// TARDIS's "the next spell you cast this turn has cascade and you may
	// planeswalk"). This build has no effect-token object, so the trigger
	// cannot run as a delayed promise; it IS the Effect's cast-driven
	// lifetime, read here as ForgetOnCast$ with the trigger's own ValidCard$
	// spec, so the cast sweep ends the grant on the next qualifying cast
	// instead of letting EVERY qualifying spell that turn cascade. The same
	// scan runs inside the Triggers$ loop below to skip the trigger's
	// delayed registration (it has no effect token to exile).
	if forgetOnCast == "" {
		forgetOnCast = effectSelfExileOnCastSpec(h, c, sa.Params["Triggers"])
	}
	// ImprintOnHost$ True (task param:api:Effect.ImprintOnHost): Forge's
	// EffectEffect imprints the CREATED EFFECT TOKEN on the host card and
	// moves the token to the Command zone -- the imprint is the link "this
	// effect belongs to this card", never the remembered card itself. The
	// corpus's dig-and-play family (Superior Foes of Spider-Man, Furious
	// Rise, Unstable Amulet) then ends the previous effect through its
	// trigger's `DB$ ChangeZone | Defined$ Imprinted | Origin$ Command |
	// Destination$ Exile` (exiling the imprinted token is exiling the
	// effect -- the "until you exile another card" lifetime), and Word of
	// Command / Semester's End run the same idiom inside one chain. This
	// build has no effect-token object, so the marker rides every
	// registration this call creates (state.ContinuousEffect.ImprintOnHost)
	// and the idiom ends exactly those through Host.EndImprintedEffects
	// (rules' EndImprintedEffect). Any other value is a loud unmodelled
	// read, the RememberLKI$ convention.
	if v := strings.TrimSpace(sa.Params["ImprintOnHost"]); v != "" && !strings.EqualFold(v, "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unmodelled Effect ImprintOnHost$ " + v})
	}
	imprintOnHost := strings.EqualFold(strings.TrimSpace(sa.Params["ImprintOnHost"]), "True")
	// ForgetOnPhasedIn$ True (CR 702.25, the "phase out until CARDNAME leaves
	// the battlefield" family: Out of Time, Oubliette, The Moment). The
	// Effect's comeback trigger is a printed ChangesZone trigger
	// (Origin$ Battlefield | Destination$ Any | ValidCard$ Card.IsImprinted,
	// Static$ True) whose Duration$ Permanent lifetime has no turn ceiling:
	// it lives until the Effect's own DBExileSelf body runs. The turn-ceiling
	// registration below cannot express that, so a Permanent lifetime is
	// allowed only for this marker and only for a ChangesZone body (the
	// comeback idiom); every other Effect trigger lifetime stays loud.
	forgetOnPhasedIn := strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOnPhasedIn"]), "True")
	// RememberLKI$ (Quicksilver Elemental's "RememberLKI$ Targeted"): the
	// effect remembers the TARGETED cards — "Targeted" (and Forge's bare
	// "True", which is Targeted in the corpus's spelling) is exactly the
	// set effectRemembered's default already captures, so the registered
	// grants below see it either way; the read pins the flag's presence so
	// the grant's Remembered does not depend on the RememberObjects$
	// default. Any other value (an LKI grammar this build does not model —
	// the LKI persistence a vanished card would need) is a loud Note.
	if rl := strings.TrimSpace(sa.Params["RememberLKI"]); rl != "" {
		switch rl {
		case "Targeted", "True":
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unmodelled Effect RememberLKI$ " + rl})
		}
	}
	remembered := effectRemembered(h, c, sa)
	// Capture the effect's own subjects before discarding older source memory.
	forgetOtherRemembered(h, c, sa)
	if imprintOnHost && len(remembered) > 0 {
		// ImprintOnHost$ retains the objects captured by this Effect on its
		// host card. Keep the association event-backed so replay and later
		// Defined$ Imprinted reads observe the same imprint.
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: append([]state.ObjID(nil), remembered...)})
	}
	// SetChosenNumber$ binds the Effect's number ONCE, here at creation,
	// against THIS resolution's own context: the trigger-time board (Torgal's
	// Count$Valid Dog.YouCtrl,Wolf.YouCtrl, Communal Brewing's
	// Count$CardCounters.INGREDIENT) or the fire-time snapshot (Wildgrowth
	// Archaic's TriggeredCard$Converge, tconverge1). The registered
	// replacement's body later reads the frozen number through the
	// Count$ChosenNumber head; a live re-read at entry time would answer a
	// different question. An unresolvable value is the fail-closed loud Note
	// plus a zero binding (which reads as zero everywhere).
	chosenNumber := int32(0)
	if v := strings.TrimSpace(sa.Params["SetChosenNumber"]); v != "" {
		n, ok := resolveCountOperand(h, c, v, 0)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unresolvable SetChosenNumber$ " + v})
		}
		chosenNumber = n
	}
	registered := false
	// The effect's OWNER (CR 611.2 / CR 903.9): EffectOwner$ names the seat(s)
	// the created effect's event/phase triggers belong to, which need not be
	// the creating card's controller -- Valiant Batrider's "that player gets
	// a one-time boon" is EffectOwner$ TriggeredTarget, an opening-hand
	// Chancellor's is Opponent. Resolved LAZILY on the first arm that needs
	// it, so an EffectOwner$ this build cannot resolve fails CLOSED (nothing
	// registered) with a loud Note rather than silently defaulting to the
	// source controller. The BecomeMonarch arm below deliberately does NOT
	// call this: its registration controller is the monarch relation's
	// anchor, which stays the source's controller (see its comment).
	ownerSel := strings.TrimSpace(sa.Params["EffectOwner"])
	owners := []state.PlayerID(nil)
	ownersResolved := false
	resolveOwners := func() bool {
		if ownersResolved {
			return len(owners) > 0
		}
		ownersResolved = true
		if ownerSel == "" {
			owners = []state.PlayerID{c.Controller}
			return true
		}
		ps, ok := EffectOwnerPlayers(h, c, ownerSel)
		if !ok || len(ps) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unresolvable EffectOwner$ " + ownerSel + " (Triggers$ not registered)"})
			registered = true
			return false
		}
		owners = ps
		return true
	}
	// An Effect's Triggers$ list names SVar trigger bodies the Effect arms as
	// one-shot delayed promises (CR 603.7) -- "until end of turn, whenever a
	// creature enters, draw a card" (Beck), "whenever a player casts an
	// instant or sorcery" (Bonus Round). Register every mode the replayable
	// delayed-trigger machinery resolves -- SpellCast and ChangesZone through
	// rules.checkEventDelayedTriggers, Phase through checkDelayedTriggers,
	// plus the Palace Jailer BecomeMonarch shape -- so an Effect-delivered
	// trigger genuinely reaches the trigger registry instead of a bare Note.
	//
	// A body the machinery cannot carry fails LOUDLY rather than registering
	// something that behaves differently from the card text:
	//   - a mode with no registered matcher (Attacks, TapsForMana, LifeGained,
	//     Blocks, PlaneswalkedTo, ... -- the broader printed-trigger gap) is
	//     named in the Note here, not silently dropped. Every matcher-backed
	//     mode (DamageDone included) registers through the generic matcher arm
	//     below (landed under cli-20260922T225138Z-504a0e97).
	//   - an OptionalDecider$ body (Beck's "you may draw a card") IS
	//     registered: registering it without the election would fire the
	//     effect MANDATORILY, the opposite of the card text, so the spec
	//     rides the registration ("|OD=<spec>") and rules' resolveTop poses
	//     the yes/no to the named decider when the minted ability resolves.
	//   - a body with no Execute$ has nothing to resolve.
	for name := range strings.FieldsSeq(sa.Params["Triggers"]) {
		raw := ""
		if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
			raw = o.Face().SVars[name]
		}
		if raw == "" {
			// The resolving ability's OWN SVar table. A DB$ Effect reached
			// from a face that is not the object's current one -- Sephiroth,
			// One-Winged Angel's `R:Event$ Transform | ReplaceWith$ DBEffect`
			// resolves while the object still shows the front face -- cannot
			// find its trigger body on Face().SVars at all.
			raw = c.SVars[name]
		}
		tr, ok := cards.ParseTriggerLine(raw)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unparseable Effect trigger " + name})
			registered = true
			continue
		}
		if effectSelfExileOnCastTrigger(h, c, tr) {
			// The Effect's cast-driven lifetime, already consumed by the
			// effectSelfExileOnCastSpec scan above. It is not a delayed
			// promise this build can run: there is no effect-token object
			// for the body's `Origin$ Command` self-exile to move, so
			// registering it would only add an inert DelayedTrigger.
			continue
		}
		exec := strings.TrimSpace(tr.Params["Execute"])
		if exec == "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "Effect trigger " + name + " names no Execute"})
			registered = true
			continue
		}
		// OneOff$ True (CR 603.7's "when you next ..." promise): the body is
		// CONSUMED by its firing, not a recurring trigger for the Effect's
		// lifetime. Register it one-shot exactly for the modes the delayed
		// machinery has a non-repeat dispatch for (effectOneShotDelayedMode,
		// the same set effDelayedTrigger admits): dropping the |EF marker is
		// what makes the registration state.DelayedTrigger.EffectRepeat
		// false, so its first firing ends it. Any other mode keeps the
		// recurring form -- events.Apply cannot decode a non-|EF registration
		// for it and rules.checkEventDelayedTriggers has no non-repeat arm, so
		// forcing one-shot there would make the body inert rather than
		// one-shot. (Mode$ Phase is inherently one-shot: its registration
		// carries no |EF and its Phase arm emits none, so the DelayedPush that
		// fires it consumes it whether or not the body says OneOff$.)
		oneOff := strings.EqualFold(strings.TrimSpace(tr.Params["OneOff"]), "True") &&
			effectOneShotDelayedMode(tr.Mode)
		efMarker := "|EF"
		if oneOff {
			efMarker = ""
		}
		// An Effect trigger body's OptionalDecider$ is the card's own "you
		// may" election (Beck's "whenever a creature enters this turn, you
		// may draw a card"). The trigger is registered like any other
		// Effect trigger and the election is posed when the minted ability
		// resolves (rules' resolveTop, the CR 603.5 optional gate), never
		// withheld: withholding it would fire the body mandatorily, the
		// opposite of the card text. The spec rides the registration Text
		// as "|OD=<spec>" so a Mode$ Phase registration, whose body is not
		// re-parsed at fire time, still names its decider (events.Apply's
		// DelayedRegister decode -> state.DelayedTrigger.OptionalSpec ->
		// checkDelayedTriggers/checkEventDelayedTriggers ->
		// effects.TriggerContext.OptionalSpec).
		optionalSpec := strings.TrimSpace(tr.Params["OptionalDecider"])
		odSuffix := ""
		if optionalSpec != "" {
			// A Static$ True body cannot carry the election: rules'
			// checkEventDelayedTriggers resolves a static-marked delayed
			// registration INLINE at fire time -- never minting a stack
			// object -- so there is no resolution gate to pose the CR 603.5
			// yes/no at, and registering it would execute the "you may"
			// mandatorily. Withheld loudly (cli-20260923T060218Z round 2);
			// the static firing arm carries a matching fail-closed guard so
			// no future "|OD=" minter can misexecute there either.
			if strings.TrimSpace(tr.Params["Static"]) != "" {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "unmodelled Effect trigger Static$ with OptionalDecider$ " + optionalSpec + " (not registered)"})
				registered = true
				continue
			}
			odSuffix = "|OD=" + optionalSpec
		}
		if tr.Mode == "BecomeMonarch" {
			// Palace Jailer's one-shot command-zone promise. It is CONSUMED
			// by its own firing, not retired by a turn ceiling, and its
			// `Duration$ Permanent | ForgetOnMoved$ Exile` lifetime is
			// exactly what the promise already means, so the turn-bound
			// lifetime guard below does not apply to it.
			//
			// This arm is deliberately NOT owned by EffectOwner$. Its
			// registration controller is the monarch relation's ANCHOR
			// (rules.checkEventDelayedTriggers reads `Player.OpponentOf
			// Remembered` against dt.Controller), and that anchor is the
			// SOURCE's controller -- the Jailer's player, whose opponent
			// the oracle's "until an opponent becomes the monarch" names.
			// Palace Jailer carries `EffectOwner$ TargetedOwner`, but
			// resolving it here would move the anchor to the exiled
			// creature's owner and return the creature when the Jailer's
			// OWN controller takes the crown -- the opposite of the card
			// text. rules/monarch_jailer_multiseat_test.go pins both
			// halves of that relation. (EffectOwner$ still applies to
			// every other arm; TargetedOwner itself is a supported
			// Defined$ selector.)
			h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
				Player: c.Controller, Step: h.Game().Step, Counter: exec,
				IDs: encodeRemembered(c.Remembered), Text: "BecomeMonarch:" + name + odSuffix})
			registered = true
			continue
		}
		// The "until CARDNAME leaves the battlefield" comeback idiom (CR
		// 702.25): a Duration$ Permanent Effect owns a ChangesZone trigger
		// that fires when the imprinted host leaves. It carries no turn
		// ceiling -- |TT= is deliberately omitted so the registration survives
		// every TurnChange -- and its retirement is the Effect's own
		// DBExileSelf body (Host.EndEffectSource), never a turn boundary.
		permanentComeback := forgetOnPhasedIn && tr.Mode == "ChangesZone" &&
			strings.EqualFold(strings.TrimSpace(rawDur), "Permanent")
		// Only lifetimes the delayed registry can retire may arm a trigger.
		// ForgetOnPhasedIn's comeback idiom retains its independent one-shot
		// lifetime: the host's departure, not the source's departure, fires it.
		dur := strings.ToLower(strings.TrimSpace(rawDur))
		supported := effectTriggerThisTurnDuration(rawDur) || permanentComeback ||
			dur == "permanent" || IsNextTurnDuration(rawDur) ||
			dur == "untilendofcombat" || dur == "untilyournextendstep"
		_, forgetZoneOK := ParseZoneWord(forgetOn)
		_, exileZoneOK := ParseZoneWord(exileOn)
		if !supported || forgetOnPhasedIn && !permanentComeback ||
			forgetOn != "" && !forgetZoneOK || exileOn != "" && !exileZoneOK {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unmodelled Effect trigger lifetime (Duration$ " + rawDur + "; not registered)"})
			registered = true
			continue
		}
		// The Effect's own this-turn lifetime bounds ALL registered modes,
		// not merely those with a ThisTurn$ rider on the trigger body.
		if v := strings.TrimSpace(tr.Params["ThisTurn"]); v != "" && !strings.EqualFold(v, "True") {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unmodelled Effect trigger ThisTurn$ " + v + " (not registered)"})
			registered = true
			continue
		}
		expiry := "|TT=" + strconv.Itoa(int(h.Game().Turn))
		if permanentComeback {
			expiry = ""
		} else if !effectTriggerThisTurnDuration(rawDur) && dur != "untilyournextendstep" {
			expiry = "|DU=" + dur
		}
		// A Duration$ Permanent Effect trigger's source-relative ending
		// (CR 611.2) applies only when the source IS a battlefield
		// permanent at registration. An opening-hand Effect (Chancellor of
		// the Annex, source still in hand) or an emblem/command-zone source
		// has no battlefield incarnation to lose, so its Permanent promise
		// is unbounded; the |SB marker tells rules' liveness predicate
		// whether the battlefield rule applies at all. Appended LAST (below,
		// after every value-bearing suffix) so the decoder's HasSuffix strip
		// sees it at the tail.
		// The continuous side folds UntilYourNextEndStep into this-turn;
		// use its same boundary rather than silently giving it permanence.
		if !permanentComeback {
			if forgetOn != "" {
				expiry += "|FM=" + forgetOn
			}
			if exileOn != "" {
				expiry += "|XM=" + exileOn
			}
			if forgetCounter != "" {
				expiry += "|FK=" + forgetCounter
			}
			if forgetOnCast != "" {
				expiry += "|FC=" + forgetOnCast
			}
			if imprintOnHost {
				expiry += "|IH"
			}
		}
		if dur == "permanent" && !permanentComeback &&
			h.Game().Obj(c.Source) != nil && h.Game().Obj(c.Source).Zone == state.ZBattlefield {
			expiry += "|SB"
		}
		// The Effect's own capture is what an Effect-owned trigger's
		// `Defined$ Remembered` names. Register it so the comeback body
		// phases the Effect's memory (Oubliette's `RememberObjects$
		// Targeted`, Out of Time's `RememberObjects$ Remembered`) rather than
		// the host that just left. Every other Effect trigger keeps its
		// existing registration set untouched.
		regIDs := c.Remembered
		if permanentComeback || forgetOn != "" || exileOn != "" || forgetCounter != "" {
			regIDs = make([]state.Target, 0, len(remembered))
			for _, id := range remembered {
				regIDs = append(regIDs, state.Target{Obj: id})
			}
		}
		// The comeback body matches the HOST card leaving: the printed
		// `ValidCard$ Card.IsImprinted` pairs with the Effect's
		// `ImprintCards$ Self`, which in Forge imprints the host on its own
		// effect. This build's Card.IsImprinted predicate is deliberately
		// exile-scoped (the dig-and-play association: a linked card stops
		// matching once it leaves exile), so a host still on the battlefield
		// as it leaves would never match. Normalise exactly this comeback
		// body to Card.Self -- the same object the Self-imprint names -- and
		// leave the predicate's exile rule untouched for every other carrier.
		regTrigger := name
		if permanentComeback && strings.Contains(raw, "Card.IsImprinted") {
			regTrigger = strings.ReplaceAll(raw, "Card.IsImprinted", "Card.Self")
		}
		switch tr.Mode {
		case "SpellCast", "ChangesZone":
			// Fire-time match re-parses the named body on the source face.
			// An Effect's "whenever you cast a spell" / "whenever a creature
			// enters" is an ordinary REPEATABLE trigger for the Effect's
			// lifetime, not a one-shot DelayedTrigger promise, so it carries
			// |EF like every other Effect-delivered mode: DelayedPush keeps
			// the registration and the |TT= turn bound retires it. A
			// OneOff$ True body is the exception (efMarker is empty): its
			// first firing consumes it.
			if !resolveOwners() {
				continue
			}
			for _, owner := range owners {
				h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
					Player: owner, Step: h.Game().Step, Counter: exec,
					IDs: encodeRemembered(regIDs), Text: tr.Mode + ":" + regTrigger + expiry + odSuffix + efMarker})
			}
			registered = true
		case "Phase":
			// A phase promise fires at the FIRST listed step still ahead
			// (state.EarliestAfter), exactly like the DelayedTrigger SA's
			// multi-step Phase$ reading; ValidPlayer$ rides |VP= so the
			// phase scan gates on it (Necropotence's "YOUR next end step").
			if !resolveOwners() {
				continue
			}
			set, unknown := state.ParsePhases(tr.Params["Phase"])
			if len(unknown) > 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "Effect trigger " + name + " at unrecognized phase " + tr.Params["Phase"]})
				registered = true
				continue
			}
			step, future := state.EarliestAfter(set, h.Game().Step)
			if !future {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "Effect trigger " + name + " has no future phase"})
				registered = true
				continue
			}
			text := tr.Params["Phase"] + expiry + odSuffix
			if vp := strings.TrimSpace(tr.Params["ValidPlayer"]); vp != "" {
				text += "|VP=" + vp
			}
			for _, owner := range owners {
				h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
					Player: owner, Step: step, Counter: exec,
					IDs: encodeRemembered(regIDs), Text: text})
			}
			registered = true
		default:
			// ChangesController is a delayed-event mode (including its
			// remembered-object and original-controller filters), but is not
			// a printed-trigger matcher. Admit only this explicitly handled
			// delayed mode here; all other unknown modes remain fail-closed.
			if tr.Mode != "ChangesController" && !h.TriggerModeSupported(tr.Mode) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect trigger " + tr.Mode + " unimplemented"})
				registered = true
				continue
			}
			// All event modes share the trigger registry's matcher. The |EF
			// marker distinguishes this recurring Effect grant from a one-shot
			// DelayedTrigger and makes its mode self-describing for replay.
			if !resolveOwners() {
				continue
			}
			for _, owner := range owners {
				h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
					Player: owner, Step: h.Game().Step, Counter: exec,
					IDs: encodeRemembered(regIDs), Text: tr.Mode + ":" + name + expiry + odSuffix + efMarker})
			}
			registered = true
		}
	}
	// Effect can also create a replacement rather than a layer restriction.
	// Forge stores its R: body behind an SVar name in ReplacementEffects$.
	// Keep the parsed event data in state (which cannot import cards) and the
	// body text for rules to resolve under this Effect's source context.
	for _, name := range strings.FieldsFunc(sa.Params["ReplacementEffects"], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		event, params := parseReplacementLine(c.SVars, name)
		body := ""
		if with := replacementLineWith(params); with != "" {
			body = c.SVars[with]
		}
		// A LIVE replacement registration: a body this build's replacement
		// dispatcher actually resolves. DamageDone is the Taii Wakeen shape
		// (the body is a DB$ ReplaceEffect damage rewrite); Event$ Moved with
		// a PutCounter body is the "that creature enters with an additional
		// +1/+1 counter for each ..." family (torgal_a_fine_hound,
		// communal_brewing, wildgrowth_archaic, task wildgrowth1): the
		// Updated-shaped MoveZone dispatch already applies the original move,
		// fires entry triggers, then runs the body, and effPutCounter handles
		// ETB$ True on the entered object. Event$ CreateToken with a ReplaceToken
		// body is the third live class (Crafty Cutpurse's Type$ ReplaceController
		// OppCreatEnters, Kaya, Geist Hunter's Type$ Amount doubler): the token
		// replacement path (rules/replacement.go's continueCreateTokenReplacements)
		// collects Effect-created matches through the same replMatch shape and
		// re-checks each body's ValidToken$ per plan mint, so a registered
		// ReplaceToken body is fully resolved there and no replaced mint is lost.
		// Event$ AddCounter with a ReplaceCounter body is the fourth live class
		// (Brad Boimler, Eager Ensign's tap trigger, the corpus's sole
		// CounterReplace Effect carrier): the body is a DB$ ReplaceCounter, the
		// same body API printed R: AddCounter lines resolve through
		// rules/replacement.go's applyAddCounterReplacements, which collects
		// Effect-created matches through the same replMatch shape and prices the
		// body's Amount$ itself (ReplaceCount$CounterNum/Plus.1 -> placed+1).
		// Nothing is discarded by the registration -- the replacement only
		// rewrites the CounterChange amount -- so unlike the Moved class the
		// replaced result cannot lose an object, and a body this build cannot
		// price is skipped by the dispatcher, never read as zero. Every OTHER
		// AddCounter body keeps its loud Note.
		// Every OTHER Moved body (the
		// destination-changing ChangeZone/Tap/Clone family, 44 measured
		// files) and every Draw/ProduceMana body keeps its loud
		// Note: a half-modelled Replaced-result could LOSE the moved object.
		// The effect's own capture state rides every live registration:
		// Remembered (the trigger's RememberObjects$ card, what the body's
		// IsRemembered/Remembered$ specs and Count$ChosenNumber's neighbours
		// read), the two move-driven lifetimes (ExileOnMoved$ Stack ends the
		// effect exactly after the one entry it upgrades -- load-bearing:
		// without it the effect would upgrade EVERY later creature cast this
		// turn), and the frozen SetChosenNumber$ binding.
		if body != "" && (event == "DamageDone" ||
			(event == "Moved" && replacementBodyAPI(body) == "PutCounter") ||
			(event == "Moved" && replacementRedirectsToExile(params, body, c.SVars)) ||
			(event == "CreateToken" && replacementBodyAPI(body) == "ReplaceToken") ||
			(event == "AddCounter" && replacementBodyAPI(body) == "ReplaceCounter")) {
			effectContinuous(h, state.ContinuousEffect{
				Source: c.Source, Controller: c.Controller,
				UntilEOT: effectUntilEOT(h, c.Source, rawDur), Duration: dur,
				Name:              effectName,
				Remembered:        remembered,
				ForgetOnMoved:     forgetOn,
				ExileOnMoved:      exileOn,
				ForgetCounter:     forgetCounter,
				ImprintOnHost:     imprintOnHost,
				ChosenNumber:      chosenNumber,
				RememberedPlayers: effectRememberedPlayers(h, c, sa),
				ReplacementEvent:  event, ReplacementParams: params, ReplacementBody: body,
			})
			registered = true
		} else if event != "" && body == "" && (replacementLineCantHappen(params) ||
			((event == "DamageDone" || event == "GainLife") && replacementLinePrevents(params))) {
			// The bodyless CantHappen form (Mistrise Village's AntiMagic: the
			// Event$ Counter | ValidCard$ Card.IsRemembered | Layer$ CantHappen
			// R: the delayed Effect registers): stopping the event is the
			// complete replacement, the same shape printed R: lines take —
			// rules' effect-created scan matches it With-less. The remembered
			// set (the cast spell the trigger captured) rides the registration,
			// so the ValidCard$ IsRemembered gate scopes the promise to the
			// exact spell.
			// The bodyless Prevent$ True DamageDone form is the same idiom for
			// damage: full prevention IS the complete replacement (Selfless
			// Squire's RPrevent, and the Fog family's DB$ Effect bodies -- 131
			// measured carriers). The shared damage dispatch prevents through
			// damageReplacementPrevents and stores the prevention Note whose
			// Amount Mode$ DamagePreventedOnce triggers read.
			untilEOT := effectUntilEOT(h, c.Source, rawDur)
			if event == "DamageDone" && sa.Params["Duration"] == "" {
				// This family's oracle text is always "this turn" (Selfless
				// Squire, Kurbis, the Fog spells) and none of its bodyless lines
				// names Duration$: a prevent from a PERMANENT source with no
				// explicit Duration$ is a this-turn grant, not the Permanent
				// default the other shapes keep. An explicit Duration$ wins.
				untilEOT = true
			}
			effectContinuous(h, state.ContinuousEffect{
				Source: c.Source, Controller: c.Controller,
				UntilEOT: untilEOT, Duration: dur,
				Name:              effectName,
				Remembered:        remembered,
				RememberedPlayers: effectRememberedPlayers(h, c, sa),
				ImprintOnHost:     imprintOnHost,
				ReplacementEvent:  event, ReplacementParams: params,
			})
			registered = true
		} else if name != "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "continuous replacement unimplemented (" + name + ")"})
		}
	}
	for _, name := range strings.FieldsFunc(sa.Params["StaticAbilities"], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		mode, params := parseStaticLine(c.SVars, name)
		switch mode {
		case "Continuous":
			// GainsAbilitiesOfDefined$ is the dynamic Defined-set spelling of
			// the has-all-activated-abilities grant. Resolve it while the
			// Effect's captured context is still available; unlike the printed
			// card-filter spelling this must not scan a zone or lose the foreign
			// object's identity.
			if ce, ok := effectGainsAbilitiesOfDefined(h, c, params, remembered); ok {
				ce.Name = effectName
				ce.UntilEOT = effectUntilEOT(h, c.Source, rawDur)
				ce.Duration = dur
				ce.Remembered = remembered
				ce.ForgetOnMoved = forgetOn
				ce.ExileOnMoved = exileOn
				ce.ForgetCounter = forgetCounter
				ce.ImprintOnHost = imprintOnHost
				effectContinuous(h, ce)
				registered = true
			} else if grant, ok := mayPlayGrantFromLine(params); ok {
				// A may-play-from-zone grant delivered by an Effect SA (Atsushi's
				// "you may play those cards" STPlay static): registered like the
				// S: static shape, with the Effect's Remembered set seeding the
				// grant so the Affected$ Card.IsRemembered spec matches the cards
				// the resolution exiled/remembered (rules' grant walk matches
				// through a SpecContext that carries this list). mayPlayEffectParams
				// is this path's whitelist -- the printed route's stricter
				// MayPlayStaticParams plus the ValidAfterStack$ qualifier, which
				// rides MayPlayValidAfterStack for rules to evaluate with the
				// derived stack view; a rider this build does not read fails
				// closed here too.
				grant.Source = c.Source
				grant.Controller = c.Controller
				grant.Name = effectName
				grant.UntilEOT = effectUntilEOT(h, c.Source, rawDur)
				grant.Remembered = remembered
				grant.Duration = dur
				grant.ForgetOnMoved = forgetOn
				grant.ExileOnMoved = exileOn
				grant.ForgetCounter = forgetCounter
				grant.ImprintOnHost = imprintOnHost
				effectContinuous(h, grant)
				registered = true
			} else if grant, ok := mayPlayFreeGrantFromLine(params); ok {
				// The FREE-cast may-play grant delivered by an Effect SA (Dauthi
				// Voidwalker's "you may play it this turn without paying its mana
				// cost", Idol of Endurance, Nicol Bolas, God-Pharaoh): the same
				// registration shape the plain grant above uses, with the
				// MayPlayWithoutManaCost$ True rider carried as the MayPlayFree
				// field rules' grant walk reads for the free half. mayPlayEffectFreeParams
				// keeps this path honest the same way -- the printed route's
				// MayPlayFreeStaticParams plus the ValidAfterStack$ qualifier
				// (Nahiri's STPlay2: free Equipment casts gated on
				// Spell.Equipment): a rider this build does not read fails
				// closed here too. The lifetime fields are exactly the plain
				// grant's.
				grant.Source = c.Source
				grant.Controller = c.Controller
				grant.Name = effectName
				grant.UntilEOT = effectUntilEOT(h, c.Source, rawDur)
				grant.Remembered = remembered
				grant.Duration = dur
				grant.ForgetOnMoved = forgetOn
				grant.ExileOnMoved = exileOn
				grant.ForgetCounter = forgetCounter
				grant.ImprintOnHost = imprintOnHost
				effectContinuous(h, grant)
				registered = true
			} else if kws, affected, zone, ok := cascadeKeywordGrantFromLine(params); ok {
				// AddKeyword$ Cascade (task cascade1): the Effect-delivered
				// cascade grant (TARDIS's GrantCascade, Dark Apostle's, Bigger
				// on the Inside's), registered as a layer-6 keyword grant the
				// same walk the printed S: statics feed (rules/layers.go's
				// derivedWith), so rules' hasCastCascade — the one read both
				// routes share — picks it up. The line must be fully readable:
				// only AddKeyword$ values that are entirely Cascade, with no
				// condition gate this registration path cannot evaluate, make
				// it past the whitelist; anything else fails closed to the
				// unimplemented Note below. The grant's lifetime is the Effect's
				// own (the source-leaves/UntilEOT discipline every registration
				// here uses) — and when the SA carries ForgetOnCast$, the cast
				// sweep (rules' effectCastSweep) ends the grant on the first
				// qualifying cast, which is the "the NEXT spell" precision the
				// corpus's GrantCascade riders (Dark Apostle, Bigger on the
				// Inside, World War Hulk, Sloppity Bilepiper) write.
				ce := state.ContinuousEffect{
					Source:        c.Source,
					Controller:    c.Controller,
					Layer:         state.LAbilities,
					Affects:       affected,
					AffectedZone:  zone,
					AddKeywords:   kws,
					ImprintOnHost: imprintOnHost,
					Name:          effectName,
					UntilEOT:      effectUntilEOT(h, c.Source, rawDur),
					Duration:      dur,
					Remembered:    remembered,
					ForgetOnMoved: forgetOn,
					ExileOnMoved:  exileOn,
					ForgetCounter: forgetCounter,
					ForgetOnCast:  forgetOnCast,
				}
				effectContinuous(h, ce)
				registered = true
			} else if val, affected, zone, ok := setMaxHandSizeGrantFromLine(params); ok {
				// SetMaxHandSize$ (the Effect-delivered "you have no maximum
				// hand size" family: Finale of Revelation's STHandSize, Wrenn
				// and Seven's UnlimitedHand emblem, Enter the Infinite's).
				// Registered as a rules-mod the CR 514.1 consultation reads
				// (rules' maxHandSizeFor), the same way the printed S: static
				// route is read, so the two cannot disagree. The line must be
				// fully readable -- only an Affected$ spec plus a
				// SetMaxHandSize$ value, no condition gate this registration
				// path cannot evaluate -- or it fails closed to the
				// unimplemented Note below.
				//
				// Lifetime: absent Duration$ is Forge's end-of-turn default for
				// every source kind. An explicit Duration$ Permanent (Finale of
				// Revelation's "for the rest of the game", Wrenn and Seven's
				// emblem) is flagged Permanent so it outlives its one-shot source
				// (CR 611.2a); UntilYourNextTurn (Enter the Infinite) gets its
				// real turn boundary from AddContinuous. The Permanent flag must
				// inspect rawDur: dur is normalized for the duration machinery, but
				// an absent value must not become Permanent here.
				ce := state.ContinuousEffect{
					Source:         c.Source,
					Controller:     c.Controller,
					Affects:        affected,
					AffectedZone:   zone,
					SetMaxHandSize: val,
					ImprintOnHost:  imprintOnHost,
					Name:           effectName,
					UntilEOT:       effectUntilEOT(h, c.Source, rawDur),
					Permanent:      strings.EqualFold(strings.TrimSpace(rawDur), "Permanent"),
					Duration:       dur,
					Remembered:     remembered,
					ForgetOnMoved:  forgetOn,
					ExileOnMoved:   exileOn,
					ForgetCounter:  forgetCounter,
				}
				effectContinuous(h, ce)
				registered = true
			} else if goadStaticGrantReadable(params) {
				// A Goad$ True static delivered by the Effect (staticgoad1:
				// Hot Pursuit's IsGoaded body, Immortal Obligation's Static --
				// `Mode$ Continuous | Affected$ Creature.IsRemembered |
				// Goad$ True`). Registered into the continuous registry as a
				// Restriction ("Goad") the same shape the MustAttack and
				// CanAttackDefender requirement grants use, so rules'
				// staticGoaders -- the reader BOTH routes share -- matches its
				// Affected$ spec against the registered Remembered set exactly
				// like the layer walk binds one. The line must be entirely
				// readable (Goad$ literal True, no condition gate, no extra
				// grant parameter) or it falls through to the honest
				// unimplemented Note below rather than registering a half-read
				// goad. Lifetime is the Effect's own: UntilHostLeavesPlay is the
				// source-leaves rule for a battlefield source (effectUntilEOT
				// returns false for it, and active() drops the unit when Hot
				// Pursuit leaves), an explicit EOT spelling or a one-shot
				// source keeps the ordinary UntilEOT read.
				h.AddContinuous(state.ContinuousEffect{
					Source: c.Source, Controller: c.Controller,
					Restriction:    "Goad",
					RestrictParams: params,
					Name:           effectName,
					UntilEOT:       effectUntilEOT(h, c.Source, rawDur),
					Duration:       dur,
					Remembered:     remembered,
					ForgetOnMoved:  forgetOn,
					ExileOnMoved:   exileOn,
					ForgetCounter:  forgetCounter,
					ImprintOnHost:  imprintOnHost,
				})
				registered = true
			} else if g, affects, gok := parseStaticEffectGrant(params, false); gok && effectStaticGrantReadable(params, g) {
				// The general Mode$ Continuous case: a layer grant
				// (AddKeyword$/AddType$/AddPower$/SetColor$/RemoveAllAbilities$
				// and the rest of the parser's vocabulary) delivered by an
				// api:Effect reaches the SAME layer walk a printed S: static
				// feeds, through the SAME builder the StaticEffect$ move rider
				// uses (registerStaticEffectGrant) -- one layer split, so the two
				// delivery routes cannot disagree about what a body grants.
				// Source is the effect's own source, NOT the remembered cards,
				// and Affected$ rides verbatim: an `Affected$ Card.IsRemembered`
				// spec is answered by the layer walk against this effect's
				// registered Remembered set (rules matchesWithChars binds it).
				//
				// Lifetime is the EFFECT's, not the body's: the api:Effect line's
				// rawDur overrides the parser's read of the body's own Duration$
				// (Forge puts the lifetime on the Effect, and the corpus's
				// Effect-delivered Continuous bodies carry none), so an
				// instant/sorcery source or an absent Duration$ keeps the
				// this-turn default and an explicit Permanent/next-turn spelling
				// keeps its real boundary through AddContinuous.
				g.duration = dur
				g.permanent = strings.EqualFold(strings.TrimSpace(rawDur), "Permanent")
				g.untilEOT = effectUntilEOT(h, c.Source, rawDur)
				lt := staticGrantLifetime{
					Name:          effectName,
					Remembered:    remembered,
					ForgetOnMoved: forgetOn,
					ExileOnMoved:  exileOn,
					ForgetCounter: forgetCounter,
					ImprintOnHost: imprintOnHost,
					ForgetOnCast:  forgetOnCast,
					ChosenNumber:  chosenNumber,
					FromEffect:    true,
				}
				if registerStaticEffectGrant(h, c, c.Source, affects, g, lt) {
					registered = true
				} else {
					// The body carried only parameters this build does not read
					// (AddHiddenKeyword$, AdjustLandPlays$, ...): nothing registered,
					// so keep the honest unimplemented Note rather than claim a
					// grant went live.
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
						Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
					registered = true
				}
			} else if len(params) > 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
			}
		case "CantTarget", "CantRegenerate", "CantPreventDamage", "CantAttack", "CantSacrifice", "CantExile", "CantPutCounter", "CantBlockBy", "CanAttackDefender", "UnspentMana", "CantBlockUnless", "CantAttackUnless", "MustBlock", "NumLoyaltyAct":
			// A COMPOUND IsRemembered spec (Card.IsRemembered+Creature) resolves
			// faithfully through the general filter now that it implements
			// IsRemembered (rules/layers.go restrictionApplies consults the
			// same matcher with the registered remembered set bound), so the
			// old "reject compounds, keep the Note" guard is gone: the
			// restriction registers for real.
			//
			// CantAttack/CantSacrifice additionally gate on the same parameter
			// whitelist the face-static readers (rules/layers.go
			// cantRestrictionParamsReadable) enforce: a body carrying a
			// condition or scoping this build does not evaluate (UnlessCost$,
			// ValidCause$, ForCost$, IsPresent$, ...) must not register
			// blanket — it is reported unimplemented instead, so the two
			// registration paths cannot disagree about what is readable.
			if (mode == "CantAttack" || mode == "CantSacrifice") && !CantRestrictionParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CantExile" && !CantRestrictionParamsReadable(params) {
				// Mirror CantAttack/CantSacrifice above: the Effect-delivered
				// continuous path cannot evaluate a cause, so a body carrying
				// ValidCause$/ForCost$ (or any other unread scoping term) must not
				// register blanket -- a `ForCost$ False | ValidCause$ Triggered`
				// body registered here would over-restrict every exile, not just a
				// triggered one. The wide cause-aware whitelist is reserved for the
				// rules-side face-static walk (rules/layers.go exileBlocked), exactly
				// as CantSacrificeRestrictionParamsReadable is for CantSacrifice.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CanAttackDefender" && !CanAttackDefenderGrantParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CantPutCounter" && !CantPutCounterParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CantBlockBy" && !CantBlockByRestrictionParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CantBlockUnless" && !CantBlockUnlessRestrictionParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CantAttackUnless" && !CantAttackUnlessRestrictionParamsReadable(params) {
				// The attack-prop sibling of CantBlockUnless: Sivitri, Dragon
				// Master's +1, Forbidding Spirit, Summon: Yojimbo and War Tax
				// deliver this body through an Effect's StaticAbilities$ entry.
				// The delivered registration and rules' attackPairCharge
				// consultation share this one whitelist, so the two paths cannot
				// disagree about what is readable; a body carrying a scoping term
				// this build does not evaluate reports unimplemented instead of
				// registering a blanket tax.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "UnspentMana" && !UnspentManaParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "NumLoyaltyAct" && !NumLoyaltyActParamsReadable(params) {
				// An Effect-delivered NumLoyaltyAct body (Kaito, Dancing
				// Shadow's PWTwice, Comet, Stellar Pup's LoyaltyAbs, Urza
				// Assembles the Titans' PWTwice) registers as a continuous
				// restriction rules' loyaltyAbilityLimit reads alongside the
				// printed S: route. A body carrying a scoping term this build
				// does not evaluate must not register blanket -- it reports
				// unimplemented instead (the shipped-statics convention every
				// other whitelist arm here keeps).
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			ceUntilEOT := effectUntilEOT(h, c.Source, rawDur)
			if absentDurationMeansThisTurn(mode) && sa.Params["Duration"] == "" {
				// A restriction body whose oracle lifetime is THIS TURN but whose
				// script writes no inline Duration$ gets UntilEOT, matching the
				// general absent-Duration default. For a restriction the Permanent reading is the
				// non-permissive direction: the lock/permission would outlive the
				// turn the card text names and apply to every later turn too.
				//
				// cantputcounter1-r2 (Melira, the Living Cure's "you can't get
				// additional poison counters this turn") fixed this for
				// CantPutCounter one mode at a time; canattackdefender1-r2 hit
				// the identical shape on CanAttackDefender (Krotiq Nestguard's
				// "{2}{G}: This creature can attack this turn ...", Wakestone
				// Gargoyle's team grant), so the class now has ONE home --
				// absentDurationMeansThisTurn below names every mode whose
				// absent Duration$ is this-turn, and the next sibling joins that
				// list instead of growing another copy of this branch.
				//
				// CantBlockBy: the whole absent-Duration family is "... can't
				// be blocked this turn" (K-9 Mark I, Key to the City, Infiltrate,
				// Rikku Resourceful Guardian, and the 240-odd `Unblockable`
				// activated/triggered bodies) -- a Permanent default left the
				// bearer unblockable for the rest of the game.
				//
				// An EXPLICIT Duration$ keeps the ordinary reading (Permanent
				// stays permanent, this-turn spellings were already UntilEOT
				// through effectUntilEOT). The DamageDone prevent precedent
				// (this function) made the same absent-Duration read for the
				// same reason.
				ceUntilEOT = true
			}
			ce := state.ContinuousEffect{
				Source:         c.Source,
				Controller:     c.Controller,
				Name:           effectName,
				UntilEOT:       ceUntilEOT,
				Restriction:    mode,
				RestrictParams: params,
				// The body's own SVar table (a CantBlockUnless Cost$ naming an
				// SVar on the granting face -- War Cadence's XChosen) and the
				// frozen SetChosenNumber$ binding (Count$ChosenNumber). Read only
				// by the block-prop consultation (rules' blockPairCharge); every
				// other restriction consumer ignores both fields.
				RestrictSVars: c.SVars,
				ChosenNumber:  chosenNumber,
				ImprintOnHost: imprintOnHost,
				Remembered:    remembered,
				Duration:      dur,
				ForgetOnMoved: forgetOn,
				ExileOnMoved:  exileOn,
				ForgetCounter: forgetCounter,
			}
			if mode == "CantAttack" || mode == "CantSacrifice" {
				// The player half of the remembered capture: Call for Aid's
				// RememberObjects$ TargetedPlayer must reach the registered
				// CantAttack, whose Target$ Player.IsRemembered ("you can't
				// attack that player") resolves against this set at
				// consultation time (rules/layers.go
				// restrictionPlayerSpecMatches) — effectRemembered records
				// objects only, so without this the remembered player would
				// silently vanish.
				ce.RememberedPlayers = effectRememberedPlayers(h, c, sa)
			}
			effectContinuous(h, ce)
			registered = true
		case "CombatDamageToughness":
			// This assignment static is consumed by rules' existing combat
			// assignment collector. Keep its body parameters and SVar table on
			// the registration so the ordinary static applicability gates run
			// unchanged, with the Effect's remembered targets bound by the
			// assignment view.
			untilEOT := effectUntilEOT(h, c.Source, rawDur)
			if sa.Params["Duration"] == "" && absentDurationMeansThisTurn(mode) {
				untilEOT = true
			}
			ce := state.ContinuousEffect{
				Source: c.Source, Controller: c.Controller, Name: effectName,
				UntilEOT:             untilEOT,
				AssignmentStaticMode: mode, AssignmentStaticParams: params,
				AssignmentStaticSVars: c.SVars,
				Remembered:            remembered, Duration: dur,
				ForgetOnMoved: forgetOn, ExileOnMoved: exileOn,
				ForgetCounter: forgetCounter, ImprintOnHost: imprintOnHost,
			}
			effectContinuous(h, ce)
			registered = true
		case "ReduceCost", "RaiseCost", "SetCost", "AlternativeCost", "ManaConvert":
			// An Effect-delivered cost-modifier or ManaConvert static (task
			// param:api:Effect.ForgetOnCast; Marshland Bloodcaster's "Rather
			// than pay the mana cost of the next spell you cast this turn, you
			// may pay life equal to that spell's mana value", plus the 11
			// Effect-delivered Mode$ ReduceCost carriers -- Kaza, Roil Chaser
			// et al). Registered into the continuous registry with the line's
			// own parameter map; the cost path reads it through the SAME
			// readers the printed S: static route feeds (rules'
			// collectCostStatics for the Raise/Reduce/Set modes, rules'
			// alternativeCosts for AlternativeCost), so the two registration
			// paths cannot disagree about what applies. The whitelist is the
			// keys the cost chain's own gates evaluate plus display text: a
			// line carrying a scoping parameter this build does not evaluate
			// must not register blanket -- it is reported unimplemented
			// instead (the permissive direction for a grant).
			if (mode == "ManaConvert" && !ManaConvertParamsReadable(params)) ||
				(mode != "ManaConvert" && !CostStaticParamsReadable(params)) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			// Lifetime. The Duration$ spellings the corpus's cost carriers
			// write are exactly two (measured 13/13 at the corpus pin):
			// absent -- every one of whose texts says "this turn", so the
			// grant is this-turn even from a battlefield source (the plain
			// effectUntilEOT read would give a creature source the
			// source-leaves lifetime and let the grant survive past the
			// turn it was granted) -- and explicit Permanent (xho_cai,
			// draconic_debut, stonehide, commander_liara's "the next ...",
			// no "this turn"), which pairs with ForgetOnCast$ on every
			// carrier: the lifetime is entirely forget-driven, CR 611.2a's
			// Permanent read keeps the grant past its (already gone) spell
			// source until the cast sweep ends it. Any other spelling falls
			// to the shared effectUntilEOT read every other registration
			// here uses.
			untilEOT := effectUntilEOT(h, c.Source, rawDur)
			permanent := false
			switch {
			case forgetOnCast != "" && strings.EqualFold(strings.TrimSpace(rawDur), "Permanent"):
				untilEOT, permanent = false, true
			case strings.TrimSpace(rawDur) == "":
				untilEOT = true
			}
			ce := state.ContinuousEffect{
				Source:           c.Source,
				Controller:       c.Controller,
				Name:             effectName,
				UntilEOT:         untilEOT,
				Permanent:        permanent,
				Duration:         dur,
				Remembered:       remembered,
				ForgetOnMoved:    forgetOn,
				ExileOnMoved:     exileOn,
				ForgetCounter:    forgetCounter,
				ForgetOnCast:     forgetOnCast,
				CostStaticMode:   mode,
				CostStaticSVars:  c.SVars,
				CostStaticParams: params,
				ChosenNumber:     chosenNumber,
			}
			effectContinuous(h, ce)
			registered = true
		case "MustAttack":
			// An Effect-delivered per-player attack REQUIREMENT (Forge's
			// MustAttack$ "that creature attacks that player this combat if
			// able"): Territory Hellkite's DBPump, and the four plain-
			// SubAbility siblings Knight Rampager, Ursine Monstrosity, Raving
			// Dead and Ruhan of the Fomori. It registers like the restriction
			// modes above (rules' attackRequirements collector reads it from
			// the continuous-effect registry beside the face statics), with the
			// same readable-parameter gate so a conditional line fails closed
			// instead of over-requiring. The chosen-/remembered-player binding
			// the MustAttack$ reference resolves against rides the plain
			// Source (ChosenPlayer reads the source object's event-backed
			// Chosen list) and the captured players (effectRememberedPlayers),
			// so no extra registration state is needed.
			if !MustAttackParamsReadable(params) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			ce := state.ContinuousEffect{
				Source:         c.Source,
				Controller:     c.Controller,
				Name:           effectName,
				UntilEOT:       effectUntilEOT(h, c.Source, rawDur),
				Restriction:    mode,
				RestrictParams: params,
				Remembered:     remembered,
				Duration:       dur,
				ForgetOnMoved:  forgetOn,
				ExileOnMoved:   exileOn,
				ForgetCounter:  forgetCounter,
			}
			// The PLAYER half of the remembered capture: a MustAttack$ line
			// whose reference is a remembered player (RememberedPlayer /
			// Remembered.NonActive -- the token-then-effect carriers For Each
			// of You a Gift, Furygale Flocking, City of the Daleks, Rotted
			// Ones Lay Siege, The Brothers War) resolves it from
			// ce.RememberedPlayers at consultation time (rules/combat.go
			// requirementDefender). effectRemembered records objects only, so
			// without this the captured player would silently vanish and the
			// requirement would never be counted. Same read the adjacent
			// CantAttack/CantSacrifice case makes.
			ce.RememberedPlayers = effectRememberedPlayers(h, c, sa)
			effectContinuous(h, ce)
			registered = true
		default:
			// A resolvable but unsupported mode is reported honestly; an
			// unresolvable name (mode "") falls through to the generic Note
			// below rather than emitting an empty-mode message.
			if mode != "" {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
			}
		}
	}
	// Nothing registered (an unsupported StaticAbilities$ mode, or a
	// Triggers$-only effect such as Palace Jailer's command-zone trigger):
	// keep the original Note wording so a card whose effect this build still
	// does not make real does not move the chain for a purely cosmetic
	// reason. The registry is the feature; a Note that names what was asked
	// for is the honest stand-in until the mode is implemented.
	if !registered {
		if effectName != "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a continuous effect " + effectName + " (" + what + ") for " + dur})
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a continuous effect (" + what + ") for " + dur})
		}
		return
	}
	// The Effect is now a live registration; a self-exile idiom later in the
	// SAME resolution chain (a `DB$ Effect ... SubAbility$ ... | Origin$
	// Command | Destination$ Exile`, and every Effect-created delayed trigger
	// already bound by rules) resolves under this source's Effect identity and
	// must end it. Stamp zero is the SOURCE-SCOPED frame: the chain has no
	// per-registration (source, timestamp) identity to name, so the ender drops
	// every Effect-created registration from the source (Host.EndEffectSource)
	// while leaving the source's printed statics alone.
	c.EffectFrame = EffectFrame{Source: c.Source}
}

// mayPlayGrantFromLine builds the may-play ContinuousEffect from one parsed
// SVar static line an Effect SA registers. ok=false is the fail-closed grant:
// nothing is registered rather than a half-read grant going live. Unlike the
// printed S: route's MayPlayStaticParams, this path may carry a
// ValidAfterStack$ spell-characteristic qualifier (Nahiri, Forged in Fury's
// STPlay2), returned verbatim on MayPlayValidAfterStack for rules to evaluate.
//
// cascadeKeywordGrantFromLine is the AddKeyword$ Cascade twin (task
// cascade1): ok only when the line's AddKeyword$ value is entirely Cascade
// tokens (the whitelist — a mixed Cascade & Haste grant or any other keyword
// fails closed to the unimplemented Note) and carries no condition gate this
// registration path cannot evaluate. Returns the granted keyword list (all
// "Cascade", one entry per instance), the Affected$ spec (Forge's omitted
// default is the controller's own cards, the same Card.Self default the
// layer walk's static scan applies) and the AffectedZone$ value verbatim.
func cascadeKeywordGrantFromLine(params map[string]string) (kws []string, affected, zone string, ok bool) {
	raw := strings.TrimSpace(params["AddKeyword"])
	if raw == "" {
		return nil, "", "", false
	}
	for _, k := range cards.SplitKeywordList(raw) {
		if !strings.EqualFold(cards.KeywordHead(k), "Cascade") {
			return nil, "", "", false
		}
		kws = append(kws, "Cascade")
	}
	if len(kws) == 0 {
		return nil, "", "", false
	}
	for _, key := range []string{"Condition", "CheckSVar", "SVarCompare", "IsPresent", "IsPresent2", "PresentCompare"} {
		if strings.TrimSpace(params[key]) != "" {
			return nil, "", "", false
		}
	}
	affected = strings.TrimSpace(params["Affected"])
	if affected == "" {
		affected = "Card.Self"
	}
	return kws, affected, strings.TrimSpace(params["AffectedZone"]), true
}

// setMaxHandSizeGrantFromLine reports whether a Mode$ Continuous static body
// (an S: line or an SVar static an Effect SA registers) carries the
// SetMaxHandSize$ grant this build implements, and resolves its readable
// fields. The implemented shape is Affected$ plus SetMaxHandSize$ Unlimited
// or a plain non-negative integer, with only display/placement metadata
// alongside; a value this build cannot price (an SVar name like X or Y, the
// numeric-SVar carriers) fails closed, exactly the way the printed-static
// reader's value read does, so the two routes agree. A condition gate
// (Condition$/CheckSVar$/IsPresent$/...) is not evaluated on this
// registration path, so a line carrying one is refused rather than applied
// blanket -- the permissive direction for a grant.
func setMaxHandSizeGrantFromLine(params map[string]string) (val, affected, zone string, ok bool) {
	val = strings.TrimSpace(params["SetMaxHandSize"])
	if val == "" {
		return "", "", "", false
	}
	if _, ok := HandSizeValueOK(val); !ok {
		return "", "", "", false
	}
	for _, key := range []string{"Condition", "CheckSVar", "SVarCompare", "IsPresent", "IsPresent2", "PresentCompare"} {
		if strings.TrimSpace(params[key]) != "" {
			return "", "", "", false
		}
	}
	affected = strings.TrimSpace(params["Affected"])
	if affected == "" {
		affected = "Card.Self"
	}
	return val, affected, strings.TrimSpace(params["AffectedZone"]), true
}

// HandSizeValueOK is the ONE SetMaxHandSize$ value grammar both the
// printed-static read (rules' maxHandSizeFor) and the Effect-delivery
// whitelist (setMaxHandSizeGrantFromLine) consult, so the two registration
// paths cannot disagree about what is readable. It accepts the literal word
// Unlimited (any casing) or a plain non-negative decimal integer and returns
// the priced maximum; a dynamic value (an SVar name like X or Y) reports
// false, matching the fail-closed direction the printed read already took.
func HandSizeValueOK(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if strings.EqualFold(raw, "Unlimited") {
		return UnlimitedHandSize, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// UnlimitedHandSize is the value SetMaxHandSize$ Unlimited maps to on the
// effects side of the shared grammar: far above any hand a game can assemble,
// so the CR 514.1 discard never triggers. rules' maxHandSizeFor keeps its own
// copy (unlimitedHandSize) because rules must not reach into effects for a
// constant; both are tested to agree by TestHandSizeValueGrammarIsShared.
const UnlimitedHandSize = 1 << 20

func mayPlayGrantFromLine(params map[string]string) (state.ContinuousEffect, bool) {
	ignoreColor, ignoreType, limit, playerTurn, afterStack, ok := mayPlayEffectParams(params, false)
	if !ok {
		return state.ContinuousEffect{}, false
	}
	return state.ContinuousEffect{
		Affects:                params["Affected"],
		AffectedZone:           strings.TrimSpace(params["AffectedZone"]),
		MayPlay:                true,
		MayPlayIgnoreColor:     ignoreColor,
		MayPlayIgnoreType:      ignoreType,
		MayPlayLimit:           limit,
		MayPlayPlayerTurn:      playerTurn,
		MayPlayValidAfterStack: afterStack,
	}, true
}

// mayPlayFreeGrantFromLine builds the FREE-cast may-play ContinuousEffect
// from one parsed static line: MayPlay$ True plus MayPlayWithoutManaCost$
// True (the "you may cast/play it this turn without paying its mana cost"
// shape -- Dauthi Voidwalker, Idol of Endurance, Nicol Bolas, God-Pharaoh,
// Fire Lord Ozai). ok=false is the fail-closed grant: nothing is registered
// rather than a half-read grant going live. The value rides a separate
// ContinuousEffect flag (MayPlayFree) because the printed-S: battlefield
// route's grant entries carry no free read -- the free-cast MayPlay static
// CHANGES what the cast costs, and the field is consumed exactly where the
// plain grant's cost is (rules/mayplay.go's mayPlayGrant).
func mayPlayFreeGrantFromLine(params map[string]string) (state.ContinuousEffect, bool) {
	limit, playerTurn, afterStack, ok := mayPlayEffectFreeParams(params)
	if !ok {
		return state.ContinuousEffect{}, false
	}
	return state.ContinuousEffect{
		Affects:                params["Affected"],
		AffectedZone:           strings.TrimSpace(params["AffectedZone"]),
		MayPlay:                true,
		MayPlayFree:            true,
		MayPlayLimit:           limit,
		MayPlayPlayerTurn:      playerTurn,
		MayPlayValidAfterStack: afterStack,
	}, true
}

// MayPlayStaticParams reports whether a Mode$ Continuous static body (an S:
// line or an SVar static an Effect SA registers) carries the may-play grant
// this build implements, and resolves its readable riders. The
// implemented shape is MayPlay$ True plus an Affected$/AffectedZone$ pair and
// only display/placement metadata; MayPlayIgnoreColor$ (mana as any colour),
// MayPlayIgnoreType$ (mana as any type -- Rakdos, the Muscle's rider: the
// colour widening plus {C} pips payable by any colour), MayPlayLimit$ (an
// integer once-per-turn cap) and Condition$ PlayerTurn
// ("during each of your turns", the Kess/Karador family) are read.
// MayPlayWithoutManaCost$ is the FREE-cast shape, read by its own whitelist
// (MayPlayFreeStaticParams below), never by this one. Anything else --
// MayPlayText$ (it changes what the cast IS, not just where it may come
// from), a Condition$ whose value is not PlayerTurn, a
// ValidAfterStack$/Secondary$ qualifier (it changes when the grant lives),
// or a MayPlayLimit$ value that is not a non-negative integer -- fails
// closed:
func MayPlayStaticParams(params map[string]string) (ignoreColor, ignoreType bool, limit int32, playerTurn bool, ok bool) {
	v, okv := params["MayPlay"]
	if !okv || !strings.EqualFold(strings.TrimSpace(v), "True") {
		return false, false, 0, false, false
	}
	ignoreColor, ignoreType, limit, playerTurn, _, ok = mayPlayParamsScan(params, false, false)
	return ignoreColor, ignoreType, limit, playerTurn, ok
}

// mayPlayEffectParams is the Effect-delivery sibling of MayPlayStaticParams:
// the same MayPlay$ True grammar, but the DB$ Effect registration path
// (mayPlayGrantFromLine) may additionally carry a ValidAfterStack$
// spell-characteristic qualifier (Nahiri, Forged in Fury's
// MayPlay$ True + ValidAfterStack$ Spell.Equipment static). The value is
// returned verbatim to ride state.ContinuousEffect.MayPlayValidAfterStack and
// is NOT interpreted here -- effects must not reach into rules' spec matcher.
// The printed-S: route keeps the stricter MayPlayStaticParams, so
// rules/layers.go's printed grant (which carries no such field) never
// silently drops the qualifier and widens the offer.
func mayPlayEffectParams(params map[string]string, allowFree bool) (ignoreColor, ignoreType bool, limit int32, playerTurn bool, validAfterStack string, ok bool) {
	v, okv := params["MayPlay"]
	if !okv || !strings.EqualFold(strings.TrimSpace(v), "True") {
		return false, false, 0, false, "", false
	}
	return mayPlayParamsScan(params, allowFree, true)
}

// mayPlayEffectFreeParams is the free-cast Effect-delivery sibling of
// MayPlayFreeStaticParams: MayPlay$ True + MayPlayWithoutManaCost$ True plus
// the optional ValidAfterStack$ qualifier the effect route may carry (Nahiri's
// STPlay2: free Equipment casts gated on Spell.Equipment).
func mayPlayEffectFreeParams(params map[string]string) (limit int32, playerTurn bool, validAfterStack string, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(params["MayPlayWithoutManaCost"]), "True") {
		return 0, false, "", false
	}
	_, _, limit, playerTurn, validAfterStack, ok = mayPlayParamsScan(params, true, true)
	return limit, playerTurn, validAfterStack, ok
}

// MayPlayFreeStaticParams reports whether a Mode$ Continuous static body
// carries the FREE-cast may-play grant: MayPlay$ True plus
// MayPlayWithoutManaCost$ True. The free rider changes what the cast costs
// (the mana part is free, CR 118.9), so the PLAIN whitelist above keeps
// refusing it -- the two grants must never be conflated. Everything else is
// the same grammar, read through the ONE shared key scan (mayPlayParams),
// so a rider the plain path rejects is rejected here too: MayPlayText$, a
// Condition$ whose value is not PlayerTurn, a ValidAfterStack$/Secondary$
// qualifier, a MayPlayLimit$ value that is not a non-negative integer, a
// MayPlayPlayer$/IgnoreColor/IgnoreType value (the free shape carries none
// of them in the corpus -- the key scan still rejects them) -- all fail
// closed. MayPlayDontGrantZonePermissions$ cannot co-occur meaningfully
// with WithoutManaCost$ (a DontGrant static only exempts costs); the scan
// rejects it, and MayPlayAltManaCost$/RaiseCost$ likewise -- the free cast
// cannot also carry an alternative cost this registration path cannot
// charge.
func MayPlayFreeStaticParams(params map[string]string) (limit int32, playerTurn bool, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(params["MayPlayWithoutManaCost"]), "True") {
		return 0, false, false
	}
	_, _, limit, playerTurn, _, ok = mayPlayParamsScan(params, true, false)
	return limit, playerTurn, ok
}

// mayPlayParamsScan is the ONE parameter scan every may-play whitelist
// shares. allowFree widens the key whitelist by exactly
// MayPlayWithoutManaCost$ (the caller has already required it to be True);
// allowAfterStack widens it by exactly ValidAfterStack$ (the
// Effect-delivery path accepts the qualifier and carries it verbatim on the
// registered effect for rules to evaluate). Every other unknown key fails
// closed. The ValidAfterStack$ value is returned unread -- effects must not
// interpret rules' spec grammar -- so an unsupported value is fail-closed at
// the matcher, never here.
func mayPlayParamsScan(params map[string]string, allowFree, allowAfterStack bool) (ignoreColor, ignoreType bool, limit int32, playerTurn bool, validAfterStack string, ok bool) {
	for key := range params {
		switch key {
		case "Mode", "MayPlay", "MayPlayIgnoreColor", "MayPlayIgnoreType",
			"MayPlayLimit", "Condition", "Affected", "AffectedZone", "Description", "EffectZone":
			// The keys the implemented grant (and only it) carries.
		case "MayPlayWithoutManaCost":
			if !allowFree {
				return false, false, 0, false, "", false
			}
		case "ValidAfterStack":
			if !allowAfterStack || strings.TrimSpace(params[key]) == "" {
				return false, false, 0, false, "", false
			}
		default:
			return false, false, 0, false, "", false
		}
	}
	limit = 0
	if raw, okv := params["MayPlayLimit"]; okv {
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
		if err != nil || n < 0 {
			// A MayPlayLimit$ value this build cannot enforce must not
			// silently become "unlimited".
			return false, false, 0, false, "", false
		}
		limit = int32(n)
	}
	playerTurn = strings.EqualFold(strings.TrimSpace(params["Condition"]), "PlayerTurn")
	if cond, okv := params["Condition"]; okv && !playerTurn {
		// A Condition$ other than PlayerTurn changes when the grant lives;
		// never register it half-read.
		_, _ = cond, okv
		return false, false, 0, false, "", false
	}
	ignoreColor = strings.EqualFold(strings.TrimSpace(params["MayPlayIgnoreColor"]), "True")
	ignoreType = strings.EqualFold(strings.TrimSpace(params["MayPlayIgnoreType"]), "True")
	if allowAfterStack {
		validAfterStack = strings.TrimSpace(params["ValidAfterStack"])
	}
	return ignoreColor, ignoreType, limit, playerTurn, validAfterStack, true
}

// parseStaticLine parses an S: static body an SVar holds ("Mode$ CantTarget |
// ValidTarget$ Card.IsRemembered | ...") into its mode and parameter map. The
// body has no SP$/AB$/DB$ head, so cards' parseSA is the wrong shape; this is
// the S: line's own grammar (cards/parse.go's "S" case). An empty or
// malformed body degrades to "" mode and a nil map, which the switch in
// effEffect treats as unimplemented rather than as a registration.
// parseReplacementLine parses an Effect's SVar replacement body ("Event$
// DamageDone | ...") using the same key/value grammar as parseStaticLine.
func parseReplacementLine(svars map[string]string, name string) (string, map[string]string) {
	body := strings.TrimSpace(svars[name])
	if body == "" {
		return "", nil
	}
	params := make(map[string]string)
	for seg := range strings.SplitSeq(body, "|") {
		key, val, ok := strings.Cut(strings.TrimSpace(seg), "$")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return params["Event"], params
}

// staticLineParams is a parsed SVar static body, deliberately distinct from
// cards.SA.Params: it is metadata carried by a DB$ Effect's StaticAbilities$
// reference, not a card primitive's parameter map.
type staticLineParams map[string]string

func effectGainsLimitPerTurn(params staticLineParams) int {
	n, err := strconv.Atoi(strings.TrimSpace(params["GainsAbilitiesLimitPerTurn"]))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// effectGainsAbilitiesOfDefined converts an Effect-delivered Continuous
// static's Defined set into the existing activated-ability grant payload.
// Remembered is copied into the resolving context because an Effect's capture
// is persisted on its registration as object ids.
func effectGainsAbilitiesOfDefined(h Host, c *Ctx, params staticLineParams, remembered []state.ObjID) (state.ContinuousEffect, bool) {
	spec := strings.TrimSpace(params["GainsAbilitiesOfDefined"])
	if spec == "" {
		return state.ContinuousEffect{}, false
	}
	definedCtx := *c
	definedCtx.Remembered = make([]state.Target, 0, len(remembered))
	for _, id := range remembered {
		definedCtx.Remembered = append(definedCtx.Remembered, state.Target{Obj: id})
	}
	faces := GainedFacesOfDefined(h, &definedCtx, spec)
	if len(faces) == 0 {
		return state.ContinuousEffect{}, false
	}
	affected := strings.TrimSpace(params["Affected"])
	if affected == "" && strings.TrimSpace(params["AffectedDefined"]) != "" {
		affected = "Card.Self"
	}
	return state.ContinuousEffect{
		Source: c.Source, Controller: c.Controller, Layer: state.LAbilities,
		Affects: affected, AffectedZone: strings.TrimSpace(params["AffectedZone"]),
		GainedFaces: faces, GainsValidAbilities: strings.TrimSpace(params["GainsValidAbilities"]),
		GainsLimitPerTurn: effectGainsLimitPerTurn(params),
	}, true
}

func parseStaticLine(svars map[string]string, name string) (string, staticLineParams) {
	body := strings.TrimSpace(svars[name])
	if body == "" {
		return "", nil
	}
	params := make(staticLineParams)
	mode := ""
	for seg := range strings.SplitSeq(body, "|") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		key, val, ok := strings.Cut(seg, "$")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		params[key] = val
		if key == "Mode" {
			mode = val
		}
	}
	return mode, params
}

// ParseStaticLine is the exported form of parseStaticLine: rules reads a
// granted static's SVar body for the whitelist gates that must agree with
// the registration path (staticgoad1's etbCloneWhitelist AddStaticAbilities$
// value check), so the two cannot disagree about the body grammar. One
// parser, two tiers.
func ParseStaticLine(svars map[string]string, name string) (string, map[string]string) {
	mode, params := parseStaticLine(svars, name)
	return mode, params
}

// goadStaticGrantReadable reports whether a Mode$ Continuous static body is
// an entirely readable Goad$ True line: the literal True (any other value —
// Forge's Yes spellings included — is unmodelled), and NO parameter outside
// the display/selector whitelist. A body carrying a condition gate
// (CheckSVar$, IsPresent$) or an additional grant parameter must not
// register blanket — it fails closed to the caller's honest unimplemented
// Note (the shipped-statics convention the EffEffect whitelist arms keep).
// The same gate drives the DB$ Clone AddStaticAbilities$ route (effClone)
// and rules' ETB-clone whitelist value check (rules/cast.go), so all three
// delivery paths agree on what a readable goad grant is.
func goadStaticGrantReadable(params map[string]string) bool {
	if !strings.EqualFold(strings.TrimSpace(params["Goad"]), "True") {
		return false
	}
	for key := range params {
		switch key {
		case "Mode", "Affected", "Description", "Goad":
		default:
			return false
		}
	}
	return true
}

// NumLoyaltyActParamsReadable reports whether an Effect-delivered
// Mode$ NumLoyaltyAct static body is entirely readable: the line's selector
// (ValidCard$), its two grant parameters (Twice$ True, Additional$ N) and
// the display text are the only keys this build evaluates. OnlySourceAbs$
// True is accepted as source-scoping already implied by the corpus's
// `ValidCard$ Card.EffectSource` selector (Comet, Stellar Pup's LoyaltyAbs).
// Any other key is a condition gate or scoping term this build does not
// evaluate, so the caller fails closed to its honest unimplemented Note.
func NumLoyaltyActParamsReadable(params map[string]string) bool {
	for key := range params {
		switch key {
		case "Mode", "ValidCard", "Twice", "Additional", "OnlySourceAbs", "Description":
		default:
			return false
		}
	}
	return true
}

// GoadStaticGrantReadable is the exported form of goadStaticGrantReadable:
// rules' etbCloneWhitelist value check (staticgoad1) reads a granted
// AddStaticAbilities$ body through it, so the ETB offer and the effClone
// registration cannot disagree about what a supported goad grant is.
func GoadStaticGrantReadable(params map[string]string) bool {
	return goadStaticGrantReadable(params)
}

// effectRemembered resolves RememberObjects$ into the concrete object ids the
// Effect captured. "Targeted"/"ParentTarget" remember the chosen targets;
// "Remembered" (and creature-flavoured spellings) remember the objects the
// resolution already had, while "Imprinted" reads the source's persistent
// imprint list. An ABSENT RememberObjects$ defaults to "Targeted" (the chosen
// targets), NOT to the source; an UNRECOGNISED member contributes nothing.
// Objects only: a player-only remember yields
// an empty slice, which a restriction whose ValidCard$ is Card.IsRemembered
// then applies to nothing. The player half of the same capture lives in
// effectRememberedPlayers below.
func effectRemembered(h Host, c *Ctx, sa *cards.SA) []state.ObjID {
	ro := sa.Params["RememberObjects"]
	if ro == "" {
		ro = "Targeted"
	}
	var out []state.ObjID
	for _, member := range strings.Split(ro, "&") {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}
		// A "Valid <filter>" member is a WHOLE member: its filter grammar uses
		// the comma as OR (Kill Switch's "Valid Artifact.Other", the
		// "Creature.blockedBySource,Creature.blockingSource" pair), so the
		// comma split below must not cut it into unknown fragments. Route it
		// through the same fail-closed resolver definedSpec uses.
		if member == "Valid" || strings.HasPrefix(member, "Valid ") {
			if ts, ok := knownDefinedTargets(h, c, member); ok {
				out = appendEffectRememberedObjects(h, out, ts)
			}
			continue
		}
		for _, part := range strings.Split(member, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			switch part {
			case "You", "Self", "Source":
				out = append(out, c.Source)
			case "Targeted", "ThisTargetedCard":
				targets := c.Targets
				if c.PickedTargets != nil {
					targets = c.PickedTargets
				}
				out = appendEffectRememberedObjects(h, out, targets)
			case "ParentTarget":
				out = appendEffectRememberedObjects(h, out, c.Targets)
			case "Remembered", "Remembered.Creature", "Remembered.Permanent", "RememberedCard":
				out = appendEffectRememberedObjects(h, out, c.Remembered)
			case "Imprinted":
				// Effect RememberObjects$ Imprinted captures the source's persistent
				// Dig/ChangeZone imprint list (Synth Eradicator's may-play rider).
				if o := h.Game().Obj(c.Source); o != nil {
					for _, id := range o.Imprinted {
						if h.Game().Obj(id) != nil {
							out = append(out, id)
						}
					}
				}
			case "ReplacedCard":
				// The card the enclosing replacement acted on (Opposition Agent's
				// RepExile → DBEffect: the found card the replacement just exiled
				// is the one the may-play grant remembers). Outside a replacement
				// (c.Replaced zero) or after the object ceased to exist, nothing.
				if c.Replaced != 0 && h.Game().Obj(c.Replaced) != nil {
					out = append(out, c.Replaced)
				}
			case "TriggeredCard", "TriggeredObject", "TriggeredObjectLKICopy":
				// The card the firing trigger's event captured (Mistrise Village's
				// Effect RememberObjects$ TriggeredCard: the spell the can't-be-
				// countered promise covers). The SpellCast referent capture binds
				// c.TriggerCard to the cast stack object; a stale id (the spell
				// already resolved) remembers nothing, the same live-object
				// discipline the cases above apply. TriggeredObject(LKICopy) is the
				// same capture under the CounterPlayerAddedAll batch triggers'
				// spelling (Rikku's RememberObjects$ TriggeredObjectLKICopy: the
				// creature the counters landed on).
				if c.TriggerCard != 0 && h.Game().Obj(c.TriggerCard) != nil {
					out = append(out, c.TriggerCard)
				}
			case "ChosenCard":
				// Dauthi Voidwalker and the wider ChooseCard -> Effect family do
				// not set RememberChosen$: the chosen card lives in Ctx.Chosen, or
				// on the event-backed source when a later ability reads it.
				chosen := c.Chosen
				if len(chosen) == 0 {
					if o := h.Game().Obj(c.Source); o != nil {
						chosen = o.Chosen
					}
				}
				out = appendEffectRememberedObjects(h, out, chosen)
			case "RememberedLKI", "TriggeredAttackerLKICopy", "TriggeredTargetLKICopy", "DelayTriggerRemembered":
				// Object selectors this helper previously left unresolved. Each is
				// a name definedSpec/knownDefinedTargets already resolves, so read
				// the ONE shared resolver rather than re-deriving the referent
				// here: RememberedLKI is the capture-excluding LKI group (never the
				// raw Remembered slice), TriggeredTargetLKICopy prefers the Attached
				// bearer role, TriggeredAttackerLKICopy is the trigger's captured
				// attacker, and DelayTriggerRemembered is the delayed
				// registration's own capture. knownDefinedTargets is fail-closed: an
				// unrecognised spelling answers ok=false and contributes nothing.
				if ts, ok := knownDefinedTargets(h, c, part); ok {
					out = appendEffectRememberedObjects(h, out, ts)
				}
			}
		}
	}
	return out
}

// appendEffectRememberedObjects appends the live object entries of ts to out,
// dropping player targets, the zero id and any id no longer in the game.
// effectRemembered records objects only, and a missing or stale referent must
// contribute nothing rather than the source or a guessed id.
func appendEffectRememberedObjects(h Host, out []state.ObjID, ts []state.Target) []state.ObjID {
	for _, t := range ts {
		if t.IsPlayer || t.Obj == 0 || h.Game().Obj(t.Obj) == nil {
			continue
		}
		out = append(out, t.Obj)
	}
	return out
}

// effectRememberedPlayers resolves RememberObjects$ into the concrete PLAYER
// ids the Effect captured — the player half of effectRemembered, which
// deliberately records objects only (a player-only remember yields an empty
// slice there). The player-flavoured RememberObjects$ spellings are read:
// "TargetedPlayer"/"Targeted" (the chosen player targets — Call for Aid's
// "target opponent" and The Brothers' War's "choose two target players",
// whose remembered selves the registered restrictions then resolve) and
// "RememberedPlayer"/"RememberedPlayers"/"Remembered" (the resolution's
// remembered players — the per-opponent token-then-effect carriers For Each
// of You a Gift, Furygale Flocking, City of the Daleks and Rotted Ones Lay
// Siege bind the RepeatEach loop's current player into Ctx.Remembered, which
// their DBEff's `RememberObjects$ Remembered` then captures). Anything else
// contributes no player, so an effect whose remember the helper cannot read
// registers a restriction with an empty player set (its IsRemembered target
// clauses match nobody — fail closed). Deduplicated, first-capture order.
func effectRememberedPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	ro := sa.Params["RememberObjects"]
	if ro == "" {
		return nil
	}
	var out []state.PlayerID
	seen := make(map[state.PlayerID]bool)
	add := func(p state.PlayerID) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, part := range strings.FieldsFunc(ro, func(r rune) bool {
		return r == '&' || r == ',' || r == ' '
	}) {
		part = strings.TrimSpace(part)
		switch part {
		case "TargetedPlayer", "Targeted":
			targets := c.Targets
			if part == "Targeted" && c.PickedTargets != nil {
				targets = c.PickedTargets
			}
			for _, t := range targets {
				if t.IsPlayer {
					add(t.Player)
				}
			}
		case "TargetedOrController":
			targets := c.Targets
			if c.PickedTargets != nil {
				targets = c.PickedTargets
			}
			for _, t := range targets {
				if t.IsPlayer {
					add(t.Player)
				} else if o := h.Game().Obj(t.Obj); o != nil {
					add(o.Controller)
				}
			}
		case "RememberedPlayer", "RememberedPlayers", "Remembered":
			for _, t := range c.Remembered {
				if t.IsPlayer {
					add(t.Player)
				}
			}
		}
	}
	return out
}

// CantRestrictionParamsReadable is the parameter whitelist a CantAttack /
// CantSacrifice static must pass before this build enforces it — used BOTH by
// the face-static readers (rules/layers.go's SacrificeBlocked/attackBlocked
// activeStatics walks) and by effEffect's registration case (an Effect body
// carrying an unreadable parameter must not register blanket, so the two
// registration paths cannot disagree about what is readable): Mode$, the
// ValidCard$ object spec, the Target$ player spec, and display text only.
// A static carrying any other parameter (UnlessDefender$, IsPresent$,
// Cost$, CheckSVar$, ValidSA$, ...) names a condition or scoping this build
// does not evaluate; enforcing it blanket would OVER-restrict — a "can't
// attack unless ..." would become "can't attack at all", and a creature a
// MustAttack static requires could be left without a single legal pair — so
// the static is skipped/reported, which is the pre-registration behaviour and
// the permissive direction for a restriction. Secondary$ is allowed: it marks
// a Forge-side duplicate for modifier composition, and a boolean restriction
// cannot be applied twice.
func CantRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Target", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// CantAttackParamsReadableForRules is the FACE S:-line whitelist for a
// CantAttack static: the shared CantRestrictionParamsReadable core EXTENDED by
// exactly the conditional parameter family rules' attackBlocked reads --
// UnlessDefender$ (through effects.UnlessDefenderHolds) and the shared
// rules-side continuous gate rules/layers.go continuousGateHolds, which
// evaluates CheckSVar$ / SVarCompare$ / Condition$ / ClassBand$ and the
// IsPresent$ / IsPresent2$ / PresentCompare$ / PresentZone$ count family
// (PresentZone$ Battlefield/Graveyard/Exile/Hand/Stack; see
// countStaticPresent). It lives here, beside CantRestrictionParamsReadable and
// mirrors MustAttackParamsReadableForRules below, so the face whitelist and the
// gate evaluators cannot drift apart unseen.
//
// The present family joined this list with compound-statics1. The earlier
// measurement (271 `Mode$ CantAttack` files, 63 raw lines carrying the gate
// family, NONE pairing it with IsPresent$/PresentCompare$) predated commit
// f81f996e ("split compound S:Mode$ comma lists into one static per mode"):
// the split makes a compound line's CantAttack half inherit the SHARED Params
// map, so an `S:Mode$ CantAttack,CantBlock | ... | IsPresent$ Creature.YouCtrl
// | PresentCompare$ LE2` line (Bast, Panther Goddess) now reaches
// attackBlocked as a CantAttack static carrying IsPresent. The gate machinery
// already evaluates it, so excluding the keys only skipped the attack half
// whole while the block half (blockRestricted's CantBlock loop, which runs
// continuousGateHolds with no whitelist) bound at runtime -- the asymmetry
// this ticket fixes.
//
// Measured over the corpus: of the 24 real `Mode$ CantAttack` files carrying
// this family, 20 spell the battlefield IsPresent$/PresentCompare$ count shape
// (Desperate Castaways, Gadrak the Crown-Scourge, War Falcon, ...), one a bare
// IsPresent$ "if" shape (Wirecat, Shauku Endbringer with PresentCompare$ GT1),
// one PresentZone$ Hand (Kefnet the Mindful) and one PresentZone$ Exile
// (Ketramose, the New Dawn); IsPresent2$/ClassBand$ have zero CantAttack
// carriers but are whitelisted anyway because continuousGateHolds evaluates
// them, the same fail-closed principle MinMaxBlocker's whitelist states. None
// is a repo-deck card.
//
// A line carrying PresentCompare$ WITHOUT IsPresent$/IsPresent2$ is rejected:
// presentGate is the only reader of PresentCompare and it runs only when a
// present spec is present, so an orphan compare would fall through the gate
// unread and restrict blanket, over-restricting. A present KEY whose spec is
// empty or whitespace-only is rejected the same way (and even with no compare):
// countPresent("") matches nothing, so the "gate" reads count 0 forever and an
// EQ0 compare would hold unconditionally. Measured 0 corpus rows for both
// shapes; the guards keep it that way.
//
// A present spec carrying a predicate this build's matcher does not recognise
// is rejected too: countPresent counts through the matcher, so an unparseable
// spec matches nothing and an EQ0 compare ("restrict unless X is ABSENT")
// would read count 0 unconditionally and blanket-restrict. The one corpus row
// (Flowering Lumberknot, `IsPresent$ Creature.PairedWith+withSoulbond |
// PresentCompare$ EQ0`) now resolves through the paired creature's Soulbond
// keyword, rather than treating the predicate as unread.
// UnknownPredicates (this package) is the same census the matcher's
// recognisedPredicate classifier drives, so the check cannot drift from what
// countPresent really resolves.
//
// The Effect-delivered registration gate (effEffect, which keeps the narrower
// CantRestrictionParamsReadable for BOTH modes) cannot share this list: its
// continuous path reads neither evaluator, so a gate-bearing body must not
// register blanket -- a gated "can't attack" would become unconditional,
// over-restricting, and could leave a MustAttack creature with no legal pair.
// A static carrying any OTHER parameter (ValidCause$, ForCost$, ValidSA$,
// Cost$, ...) still fails the whitelist and is skipped whole, the deliberate
// permissive direction. Iterating the params map only yields a boolean, so map
// order never reaches an event/option/view -- determinism is preserved.
func CantAttackParamsReadableForRules(params map[string]string) bool {
	var present1, present2 string
	hasCmp, has1, has2 := false, false, false
	for k, v := range params {
		switch k {
		case "PresentCompare":
			hasCmp = true
		case "IsPresent":
			present1, has1 = v, true
		case "IsPresent2":
			present2, has2 = v, true
		}
		switch k {
		case "Mode", "ValidCard", "Target", "Description", "Secondary",
			"CheckSVar", "SVarCompare", "Condition", "UnlessDefender",
			"IsPresent", "IsPresent2", "PresentCompare", "PresentZone", "ClassBand":
		default:
			return false
		}
	}
	// presentGate is dispatched on the KEY being present, not on the value:
	// an empty/whitespace `IsPresent$` still calls countPresent(""), which
	// matches nothing (count 0). So a blank present spec is not "no gate" --
	// it is a gate that can never see its object, and `PresentCompare$ EQ0`
	// would hold unconditionally and restrict blanket. Reject a present key
	// whose spec is blank, whichever key carries the compare (and whether or
	// not one does).
	spec1 := strings.TrimSpace(present1)
	spec2 := strings.TrimSpace(present2)
	if (has1 && spec1 == "") || (has2 && spec2 == "") {
		return false
	}
	// An orphan compare (PresentCompare$ with no present spec at all) would
	// never be evaluated: presentGate is its only reader and it runs only when
	// a present key is set. Admit the line only when a NON-BLANK spec is there.
	if hasCmp && spec1 == "" && spec2 == "" {
		return false
	}
	// An unread present spec would match nothing, so an EQ0 compare would hold
	// unconditionally and over-restrict; keep such a line skipped whole.
	if spec1 != "" && len(UnknownPredicates(spec1)) != 0 {
		return false
	}
	if spec2 != "" && len(UnknownPredicates(spec2)) != 0 {
		return false
	}
	return true
}

// UnlessDefenderHolds evaluates a CantAttack static's UnlessDefender$ predicate
// (Forge StaticAbilityCantAttackBlock: `unlessDefender.hasProperty(type,
// hostCard.getController(), hostCard, stAb)`, where the defending player is the
// subject and You is the static's controller). It reports whether the SHORT
// predicate on the defending player holds; a CantAttack static is bypassed (the
// creature may attack) exactly when it does. The grammar is Forge's
// PlayerProperty conditional family, the subset the corpus spells, and every
// unsupported property FAILS CLOSED (returns false) so the restriction is
// enforced rather than silently dropped -- the same deny direction every other
// unread static gate takes. A leading `!` negates the whole predicate.
//
// Supported properties:
//
//   - `controls<objSpec>[_<cmp><n>]`: the defender controls at least one (or,
//     with a trailing count token, the compared number of) battlefield object
//     matching objSpec as an object filter. The comma in a spec such as
//     `controlsEnchantment,Permanent.enchanted` is part of the OBJECT spec
//     (Forge's getValidCards list), never a predicate separator.
//   - `hasFewer<Type>sIn<Play|Yard>ThanYou`: the defender controls (or holds in
//     its graveyard) strictly fewer objects of that type than You.
//   - `HasCardsIn<zone>_<type>_<cmp><n>`: the defender's named zone holds the
//     compared number of cards of that type (`Card` counts every card).
//   - `IsPoisoned`: the defender has a poison counter.
//   - `isMonarch`: the defender is the monarch (CR 716.2).
//
// source is the static's source object (the object-spec matcher's Source
// binding) and you is the static's controller. The evaluation is a pure read:
// no event, no state write, and the battlefield/zone walks are the engine's
// one deterministic scan order.
func UnlessDefenderHolds(g *state.Game, spec string, defender, you state.PlayerID, source state.ObjID) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	neg := strings.HasPrefix(spec, "!")
	if neg {
		spec = strings.TrimSpace(spec[1:])
	}
	holds, known := unlessDefenderProperty(g, spec, defender, you, source)
	if !known {
		return false
	}
	if neg {
		return !holds
	}
	return holds
}

// unlessDefenderProperty evaluates ONE UnlessDefender$ property (no `!`). An
// unknown or malformed property returns known=false so a leading `!` cannot
// turn an unread predicate into permission to attack.
func unlessDefenderProperty(g *state.Game, property string, defender, you state.PlayerID, source state.ObjID) (holds, known bool) {
	if int(defender) >= len(g.Players) {
		return false, false
	}
	switch {
	case property == "isMonarch":
		return g.IsMonarch(defender), true
	case property == "IsPoisoned":
		return g.Players[defender].Counter("Poison") > 0, true
	case strings.HasPrefix(property, "controls"):
		// The object spec is everything after "controls"; an optional trailing
		// `_<cmp><n>` narrows an existential read to a count compare.
		objSpec, op, want, counted := splitCountCompare(strings.TrimSpace(property[len("controls"):]))
		if objSpec == "" {
			return false, false
		}
		sc := SpecContext{You: you, Source: source}
		n := int32(0)
		for _, id := range g.Zone(state.ZBattlefield, defender) {
			if MatchesObjectCtx(g, objSpec, g.Obj(id), sc) {
				n++
			}
		}
		if !counted {
			return n > 0, true
		}
		return playerCompare(n, op, want), true
	case strings.HasPrefix(property, "HasCardsIn"):
		// HasCardsIn[zone]_[type]_[comparator]
		parts := strings.Split(strings.TrimPrefix(property, "HasCardsIn"), "_")
		if len(parts) != 3 {
			return false, false
		}
		z, ok := unlessDefenderZone(parts[0])
		if !ok {
			return false, false
		}
		op, want, ok := unlessDefenderCompare(parts[2])
		if !ok {
			return false, false
		}
		return playerCompare(unlessDefenderTypeCount(g, z, defender, parts[1]), op, want), true
	case strings.HasPrefix(property, "hasFewer"):
		// hasFewer[Type]sIn[Play|Yard]ThanYou
		if int(you) >= len(g.Players) {
			return false, false
		}
		body := strings.TrimPrefix(property, "hasFewer")
		i := strings.Index(body, "sIn")
		if i < 0 {
			return false, false
		}
		cardType, tail := body[:i], body[i+len("sIn"):]
		if cardType == "" {
			return false, false
		}
		z := state.ZBattlefield
		switch {
		case strings.HasPrefix(tail, "PlayThan"):
		case strings.HasPrefix(tail, "YardThan"):
			z = state.ZGraveyard
		default:
			return false, false
		}
		return unlessDefenderTypeCount(g, z, defender, cardType) < unlessDefenderTypeCount(g, z, you, cardType), true
	}
	return false, false
}

// unlessDefenderZone maps a Forge zone word in a HasCardsIn property to a
// state zone, failing closed on any zone this build cannot name.
func unlessDefenderZone(word string) (state.Zone, bool) {
	switch word {
	case "Battlefield":
		return state.ZBattlefield, true
	case "Graveyard":
		return state.ZGraveyard, true
	case "Hand":
		return state.ZHand, true
	case "Library":
		return state.ZLibrary, true
	case "Exile":
		return state.ZExile, true
	}
	return 0, false
}

// unlessDefenderCompare parses a Forge comparator token ("GE7") into an
// operator and a threshold.
func unlessDefenderCompare(tok string) (string, int32, bool) {
	if len(tok) < 3 {
		return "", 0, false
	}
	op := tok[:2]
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
	default:
		return "", 0, false
	}
	n, err := strconv.ParseInt(tok[2:], 10, 32)
	if err != nil {
		return "", 0, false
	}
	return op, int32(n), true
}

// unlessDefenderTypeCount counts objects of a Forge type word in a player's
// zone. `Card`/`Any` is the universal type (every object counts), matching
// CardLists.getType's treatment of the base card type; any other word rides
// the shared hasType predicate.
func unlessDefenderTypeCount(g *state.Game, z state.Zone, p state.PlayerID, cardType string) int32 {
	n := int32(0)
	for _, id := range g.Zone(z, p) {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		if strings.EqualFold(cardType, "Card") || strings.EqualFold(cardType, "Any") || hasType(o, cardType) {
			n++
		}
	}
	return n
}

// CantBlockByRestrictionParamsReadable is the parameter whitelist an
// Effect-registered CantBlockBy static must pass before this build enforces
// it (task cbb1; the same discipline CantRestrictionParamsReadable enforces
// for CantAttack/CantSacrifice, so the registration and consultation paths
// cannot disagree): the two-side specs the continuous consultation reads
// (rules/statics.go blockRestricted's registered-effects walk: ValidAttacker$
// against the ATTACKER, ValidBlocker$ against the would-be blocker, the
// historical ValidCard$ fallback), plus display text. A body carrying a
// condition or scoping parameter this build's continuous path does not
// evaluate (Condition$, IsPresent$, CheckSVar$, the Relative$ spellings,
// space_beleren's ValidBlockerRelative$ sector grammar, ...) must not
// register blanket -- a gated "can't be blocked by ..." would become an
// UNCONDITIONAL one, over-restricting -- so it stays the unimplemented Note;
// enforcing it blanket would make a conditional "can't be blocked"
// unconditional, the over-restricting direction for a restriction. Measured
// over the 594 CantBlockBy corpus files (619 raw lines): 48 carry an
// IsPresent$/PresentCompare$/CheckSVar$/SVarCompare$/Condition$ gate or a
// Relative$/ValidDefender$/ValidBlockerRelative$/PresentZone$/EffectZone$
// scoping and stay loud Notes; the other 571 read only the whitelisted
// parameters. Secondary$ is allowed: it marks a Forge-side duplicate for
// modifier composition, and a boolean restriction cannot be applied twice.
func CantBlockByRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidAttacker", "ValidBlocker", "ValidCard", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// MustAttackParamsReadable is the parameter whitelist a MustAttack line must
// pass before effEffect registers it as an Effect-delivered per-player attack
// REQUIREMENT (Territorial Hellkite's `DB$ Effect | StaticAbilities$
// AttackChosen`, and the four plain-SubAbility siblings Knight Rampager,
// Ursine Monstrosity, Raving Dead and Ruhan of the Fomori). It mirrors
// CantRestrictionParamsReadable's shape, with ValidCreature$ in place of
// ValidCard$/Target$ (Forge's MustAttack names the required creature with
// ValidCreature$) and the MustAttack$ player reference itself. A line
// carrying any other parameter (IsPresent$, PresentCompare$, Condition$,
// CheckSVar$, AffectedZone$, ValidPlayer$, ...) names a condition this
// registration path does not evaluate; registering it blanket would
// OVER-require -- the non-permissive direction for a requirement -- so it
// fails closed and is reported unimplemented, which is the pre-registration
// behaviour. Secondary$ is allowed: it marks a Forge-side duplicate for
// modifier composition, and a boolean requirement cannot be applied twice.
func MustAttackParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCreature", "MustAttack", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// MustAttackParamsReadableForRules is the FACE S:-line whitelist: the shared
// MustAttackParamsReadable core EXTENDED by exactly the condition-gate keys
// the rules package's shared continuous gate (rules/layers.go
// continuousGateHolds) evaluates -- IsPresent$, IsPresent2$, PresentCompare$,
// PresentZone$, CheckSVar$, SVarCompare$, Condition$ and ClassBand$. It
// lives here, beside MustAttackParamsReadable, so the two lists cannot drift
// apart unseen: the face route (rules' attackRequirements) CAN evaluate those
// gates -- the evaluator, continuousGateHolds, is rules-side, which is why
// this function cannot simply be MustAttackParamsReadable -- while the
// Effect-delivered route (effEffect's registration above) cannot, so its
// whitelist stays at the core set: registering a gate-bearing line as an
// Effect requirement would apply it blanket and OVER-require, the
// non-permissive direction for a requirement. The superset direction
// (every effect-readable line is face-readable) and the gate-key divergence
// are pinned by rules' TestMustAttackFaceAndEffectWhitelistsAgree.
func MustAttackParamsReadableForRules(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCreature", "MustAttack", "Description", "Secondary",
			"IsPresent", "IsPresent2", "PresentCompare", "PresentZone",
			"CheckSVar", "SVarCompare", "Condition", "ClassBand":
		default:
			return false
		}
	}
	return true
}

// CantBlockUnlessRestrictionParamsReadable is the parameter whitelist a
// CantBlockUnless static must pass before this build enforces it -- used by
// BOTH delivery routes that can register one (effEffect's restriction case
// and registerAnimateStaticAbilities' staticAbilities$ grant), so the two
// paths cannot disagree about what is readable. The readable parameters are
// the mode, the two combat specs the block-prop reader resolves (ValidCard$
// against the blocker, Attacker$ against the attacker), the Cost$ the reader
// prices (rules' blockUnlessCharge), the gate parameters the shared
// continuousGateHolds grammar evaluates, and display text. A static carrying
// any other parameter names a condition or scoping this build does not
// evaluate -- enforcing it blanket would OVER-restrict, the permissive
// direction for a restriction -- so it is skipped/reported instead.
func CantBlockUnlessRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Attacker", "Cost", "Description", "Secondary",
			"IsPresent", "IsPresent2", "CheckSVar", "SVarCompare", "Condition":
		default:
			return false
		}
	}
	return true
}

// CantAttackUnlessRestrictionParamsReadable is the parameter whitelist a
// CantAttackUnless static must pass before this build enforces it -- used by
// BOTH delivery routes that can register one (effEffect's restriction case
// and registerAnimateStaticAbilities' staticAbilities$ grant) AND by rules'
// attackPairCharge pricing read, so the three cannot disagree about what is
// readable. The readable parameters are the mode, the two combat specs the
// attack-prop reader resolves (ValidCard$ against the attacking creature,
// Target$ against the defending player/planeswalker -- the same list
// restrictionPlayerTargetMatches reads), the Cost$ the reader prices
// (rules' attackUnlessCharge), the gate parameters the shared
// continuousGateHolds grammar evaluates, RememberingAttacker$ (which binds
// the attacking creature into the pricing SVar context), and display text
// (Description$ and the TriggerDescription$ an oracle-triggered DB$ Effect
// body writes -- Sivitri's SVar carries TriggerDescription$, not
// Description$). A static carrying any other parameter names a condition or
// scoping this build does not evaluate -- enforcing it blanket would
// OVER-restrict, the permissive direction for a restriction -- so it is
// reported unimplemented instead.
func CantAttackUnlessRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Target", "Cost", "Description", "TriggerDescription", "Secondary", "Attacker",
			"IsPresent", "IsPresent2", "CheckSVar", "SVarCompare", "Condition",
			"RememberingAttacker":
		default:
			return false
		}
	}
	return true
}

// CantSacrificeRestrictionParamsReadable is the parameter whitelist a face
// CantSacrifice static must pass before rules' SacrificeBlocked enforces it
// (task vc-static1). It is the CantAttack list above PLUS the two
// cause-scoping parameters SacrificeBlocked itself evaluates -- ValidCause$
// against actionCause() through the shared stack-kind classifier
// (rules/layers.go causeSpecAdmits) and ForCost$ against the
// cost-driven/effect-driven split of the Host method's callers -- so a line
// carrying them is enforced, not skipped. The Master, Multiplied's
// `ValidCard$ Creature.YouCtrl+token | ValidCause$ Triggered.YouCtrl |
// ForCost$ False` is the filing carrier.
//
// It DELIBERATELY diverges from CantRestrictionParamsReadable: effEffect's
// registration gate keeps the narrower list for BOTH modes, because the
// continuous-effect path (rules' restrictionApplies) reads neither
// parameter and registering such a body would over-restrict blanket -- a
// Cause-scoped CantSacrifice delivered by a DB$ Effect stays an
// unimplemented Note. The asymmetry is the permissive direction for a
// restriction on the path that cannot evaluate the cause.
func CantSacrificeRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Target", "Description", "Secondary", "ValidCause", "ForCost":
		default:
			return false
		}
	}
	return true
}

// CantExileRestrictionParamsReadable is the parameter whitelist a face
// CantExile static must pass before rules' ExileBlocked enforces it -- the
// CantSacrifice list above with the exile restriction's own object scope.
// Readable: the mode, the object spec (ValidCard$, the corpus's dominant
// `Creature.YouCtrl+token` shape; the ValidCards$ plural and the
// ValidObject$ alias are the other spellings the shared spec reader resolves),
// the player spec ValidTarget$ the CantAttack reader shares, the two
// cause-scoping parameters exileBlocked itself evaluates -- ValidCause$
// against actionCause() through the shared stack-kind classifier
// (rules/layers.go causeSpecAdmits) and ForCost$ against the
// cost-driven/effect-driven split of the ExileBlocked callers -- and display
// text. The Master, Multiplied's `ValidCard$ Creature.YouCtrl+token |
// ValidCause$ Triggered.YouCtrl | ForCost$ False` is the corpus's one
// carrier.
//
// It is used by the rules-side face-static walk (rules/layers.go
// exileBlocked) alone; effEffect's registration gate keeps the NARROWER
// CantRestrictionParamsReadable for CantExile, the same asymmetry
// CantSacrificeRestrictionParamsReadable documents -- the cause-scoped body is
// read on the face route that evaluates ValidCause$/ForCost$, while an
// Effect-delivered body carrying those keys stays an unimplemented Note
// rather than registering a blanket prohibition the continuous path cannot
// scope. A line carrying any other parameter names a condition or scoping
// this build does not evaluate; enforcing it blanket would OVER-restrict --
// the permissive direction for a restriction -- so the static is
// skipped/reported instead. Secondary$ is allowed: it marks a Forge-side
// duplicate for modifier composition, and a boolean restriction cannot be
// applied twice.
func CantExileRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidCards", "ValidObject", "ValidTarget", "Target", "Description", "Secondary", "ValidCause", "ForCost":
		default:
			return false
		}
	}
	return true
}

// CantPutCounterParamsReadable is the parameter whitelist a CantPutCounter
// static must pass before this build enforces it -- used BOTH by the
// face-static reader (rules/layers.go's PutCounterBlocked activeStatics walk)
// and by effEffect's registration case, so the two paths cannot disagree about
// what is readable. The readable parameters are the restriction's own mode and
// scope (Mode$, the object spec ValidCard$/ValidObject$, the player spec
// ValidPlayer$, the counter kind CounterType$), the AffectedZone$ rider the
// Solemnity object line carries, and display text. Duration$ is readable: the
// lock's own lifetime, consumed by effEffect's CantPutCounter arm (an absent
// Duration$ there is the THIS-TURN lock the corpus's one Effect-delivered
// carrier writes -- see that arm). A static carrying any other
// parameter names a condition or scoping this build does not evaluate
// (ActiveZones$, IsPresent$, CheckSVar$, ...) -- enforcing it blanket would
// OVER-restrict, the permissive direction for a restriction -- so it is
// skipped/reported. Secondary$ is allowed: a Forge-side duplicate for modifier
// composition, and a boolean restriction cannot be applied twice.
func CantPutCounterParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidObject", "ValidPlayer", "CounterType", "AffectedZone", "Duration", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// UnspentManaParamsReadable is the parameter whitelist an UnspentMana static
// must pass before this build enforces it -- used BOTH by the face-static
// reader (rules/statics.go's unspentManaKeep activeStatics walk) and by
// effEffect's registration case, so the two paths cannot disagree about what
// is readable. The readable parameters are the mode, the player scope
// (ValidPlayer$), the colour scope (ManaType$, a comma-separated colour-word
// list effects.ColorLetters parses; absent protects every slot, the Upwelling
// spelling) and display text. Duration$ rides the registration only through
// effEffect's own effectUntilEOT read (an instant/sorcery source's grant is
// the UntilEOT lifetime the oracle's "until end of turn" states), so it is
// NOT readable here: an UnspentMana body naming Duration$ explicitly would
// need a lifetime the whitelist cannot vouch for. A static carrying any other
// parameter names a condition or scoping this build does not evaluate
// (IsPresent$, CheckSVar$, ActiveZones$, ...) -- enforcing it blanket would
// OVER-protect mana that should empty, so it is skipped/reported. Secondary$
// is allowed: a Forge-side duplicate for modifier composition, and a boolean
// keep cannot be applied twice.
func UnspentManaParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidPlayer", "ManaType", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// CanAttackDefenderGrantParamsReadable is the parameter whitelist an
// Effect-granted CanAttackDefender body (a StaticAbilities$ CanAttack grant
// such as Assault Formation's SVar:CanAttack) must pass before effEffect
// registers it as a CanAttackDefender restriction. Readable: the mode, the
// object spec (ValidCard$ — the corpus's dominant Card.EffectSource and
// IsRemembered shapes — plus the ValidCards$ spelling one carrier uses), the
// ValidTarget$ alias, the ValidAttacked$ player gate the face read evaluates,
// and display text. The gate family (IsPresent$/CheckSVar$/Condition$/...)
// is DELIBERATELY excluded: the continuous-effect path cannot evaluate a
// gate, and registering such a body would grant blanket — a Defender
// creature the gate should still wall would attack. The asymmetry with
// CanAttackDefenderParamsReadable below is the same documented divergence
// CantRestrictionParamsReadable vs CantSacrificeRestrictionParamsReadable
// carries: the grant path keeps the narrower list.
func CanAttackDefenderGrantParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidCards", "ValidTarget", "ValidAttacked", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// ManaConvertParamsReadable is the deliberately narrow whitelist for an
// Effect-delivered ManaConvert static. Unknown qualifiers fail closed rather
// than granting a conversion with a scope the payment path cannot evaluate.
// AffectedZone$ is admitted because the real corpus carrier (Abstruse
// Appropriation's `ManaConvert | ValidCard$ Card.IsRemembered | ValidSA$
// Spell.MayPlaySource | AffectedZone$ Exile`) names the zone the remembered
// card is cast FROM; rules/mana_convert.go enforces that scope against the
// cast's origin zone, so admitting it here is not a blanket grant.
func ManaConvertParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidSA", "ValidPlayer", "ManaConversion", "Optional", "EffectZone", "AffectedZone", "Description", "SpellDescription":
		default:
			return false
		}
	}
	return true
}

// IsGrantableCostStaticMode names the cost-modifier modes a GRANT delivers
// (state.ContinuousEffect.CostStaticGranted): the three rules' cost
// collector composes. AlternativeCost/OptionalCost grants are not collected
// through that route and stay on each carrier's unimplemented path.
func IsGrantableCostStaticMode(mode string) bool {
	return mode == "ReduceCost" || mode == "RaiseCost" || mode == "SetCost"
}

// CostStaticParamsReadable is the parameter whitelist an Effect-delivered
// cost-modifier static (Mode$ ReduceCost/RaiseCost/SetCost/AlternativeCost
// behind an AB$ Effect's StaticAbilities$ entry, task
// param:api:Effect.ForgetOnCast) must pass before effEffect registers it
// into the continuous registry. It lists exactly the keys the cost chain's
// own gates evaluate -- rules' costStaticApplies (Type$, ValidCard$,
// ValidSpell$, ValidTarget$, AffectedZone$, IsPresent$, OnlyFirstSpell$,
// RaiseTo$, Secondary$, Relative$, CheckSVar$ + SVarCompare$, Condition$ --
// whose unread values fail closed), costActorMatches (Activator$/Caster$),
// costModifiers' own reads (Amount$, Cost$, Color$, MinMana$,
// IgnoreGeneric$), alternativeCostScopeOK (ValidSA$, ValidPlayer$,
// IsPresent$, EffectZone$, CheckSVar$/CheckSecondSVar$ + their compares,
// ClassBand$), presentGate's PresentZone$/PresentCompare$ and the Announce$
// X binding -- plus the display-only Description$ keys. A line carrying any
// other key would register blanket where that key was meant to scope, so it
// is refused: the unimplemented Note is the permissive direction for a
// grant, exactly the whitelist discipline every other registration arm
// here keeps.
func CostStaticParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "Type", "ValidCard", "ValidSA", "ValidPlayer", "ValidSpell",
			"ValidTarget", "Activator", "Caster", "Amount", "Cost", "Announce",
			"Color", "MinMana", "IgnoreGeneric", "RaiseTo", "OnlyFirstSpell",
			"IsPresent", "PresentZone", "PresentCompare", "CheckSVar", "SVarCompare",
			"CheckSecondSVar", "SecondSVarCompare", "Condition", "EffectZone",
			"AffectedZone", "Secondary", "Relative", "ClassBand",
			"UnlessValidTarget", "PlayerTurn", "Phases",
			"Description", "SpellDescription":
		default:
			return false
		}
	}
	return true
}

// CanAttackDefenderParamsReadable is the parameter whitelist a FACE
// CanAttackDefender static must pass before rules' attacker-legality read
// (rules/attack_defender.go attackAllowedThroughDefender) enforces it. It is
// the grant list above PLUS the gate family (IsPresent$/IsPresent2$/
// CheckSVar$/SVarCompare$/Condition$), which the face read evaluates through
// the shared continuousGateHolds grammar — the same shape
// cantAttackUnlessParamsReadable carries for CantAttackUnless. A static
// carrying any other parameter names a scoping this build does not evaluate;
// skipping it is the conservative direction for a permission (the creature
// stays walled, today's behaviour), never the wrong-wide one.
func CanAttackDefenderParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidCards", "ValidTarget", "ValidAttacked", "Description", "Secondary",
			"IsPresent", "IsPresent2", "CheckSVar", "SVarCompare", "Condition":
		default:
			return false
		}
	}
	return true
}

// absentDurationMeansThisTurn is the ONE home for the restriction modes whose
// Effect-granted bodies write no inline Duration$ yet whose card text names a
// THIS-TURN lifetime. effEffect now gives every absent Duration$ the Forge
// end-of-turn default; this helper records the mode-specific corpus audit and
// keeps the intent explicit at the registration site. An explicit Permanent
// remains a game-lasting effect, while treating these absent values as
// Permanent would outlive the turn the card names, the non-permissive direction
// for a restriction.
//
// The membership test is structural, not per-card: add a mode here only when
// its absent Duration$ is this-turn by the corpus's own oracle text, and the
// registration branch below picks it up without a new copy of the expiry
// logic. An EXPLICIT Duration$ always takes precedence over this list.
//
//   - CantPutCounter: Melira, the Living Cure's Effect-delivered lock is "you
//     can't get additional poison counters this turn"; with no Duration$ the
//     registration must be UntilEOT (cantputcounter1-r2).
//   - CanAttackDefender: the Effect-granted permission family is uniformly
//     "can attack this turn as though it didn't have defender" -- measured,
//     ALL 22 corpus StaticAbilities$ CanAttack bodies write no inline
//     Duration$ (18 Card.EffectSource self-grants, Assault Formation's
//     Creature.IsRemembered, Wakestone Gargoyle's Creature.YouCtrl+withDefender
//     team grant). Krotiq Nestguard's activated grant and Wakestone Gargoyle's
//     both broke before canattackdefender1-r2 (canattackdefender1-r2).
//   - CantBlockBy: Forge's Effect SA with NO Duration$ is a THIS-TURN effect
//     -- the corpus's own convention proves it: every no-Duration unblockable
//     grant is an activated/triggered ability whose oracle says "this turn"
//     (Suspicious Bookcase, Kaito Cunning Infiltrator's +1, Kappa Cannoneer's
//     counter trigger; 108 activated carriers), while the "for as long as"
//     shapes spell Duration$ UntilHostLeavesPlayOrEOT out explicitly and the
//     forever shapes spell Duration$ Permanent (cbb1, joined from main).
//
// DELIBERATELY ABSENT: CantAttack (42 bodies -- "Creatures can't attack you"
// and the "during your next turn" shapes are not this-turn), CantTarget (5 --
// "Players and Permanents can't be the targets" is a permanent lock),
// CantPreventDamage (14 -- "Damage can't be prevented" is a permanent lock),
// and CantSacrifice (5 -- "This permanent can't be sacrificed" is a static).
// A mode joins this list only when EVERY corpus Effect body of it is this-turn;
// adding one on a mixed population would expire a genuinely permanent
// restriction a turn early, the wrong-wide direction.
func absentDurationMeansThisTurn(mode string) bool {
	switch mode {
	case "CantPutCounter", "CantBlockBy", "CanAttackDefender", "NumLoyaltyAct", "CombatDamageToughness":
		return true
	}
	return false
}

// IsNextTurnDuration reports whether a Duration$ value names the
// controller's NEXT-turn lifetime (UntilYourNextTurn, UntilTheEndOfYourNextTurn)
// -- the two largest non-Permanent durations the wave survey measured. Such
// an effect is neither UntilEOT (which would expire it a full turn early, at
// the end of the current turn) nor source-leaves (which never expires it);
// it gets a real turn-boundary lifetime via ContinuousEffect.UntilTurn,
// computed in rules.Engine.AddContinuous. Exported so rules/layers.go can
// recognise the same spelling effEffect saw; the two largest values by far
// (105 + 70 raw lines), so this closes most of the turn-spanning gap.
func IsNextTurnDuration(dur string) bool {
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "untilyournextturn", "untiltheendofyournextturn":
		return true
	}
	return false
}

// IsUntilYourNextTurn distinguishes the start-of-next-turn boundary from
// UntilTheEndOfYourNextTurn, which lasts through that turn's cleanup.
func IsUntilYourNextTurn(dur string) bool {
	return strings.EqualFold(strings.TrimSpace(dur), "UntilYourNextTurn")
}

// replacementLineWith reads ReplaceWith$ off a parseReplacementLine-built
// static line -- the SVar name of the R: body's own ReplaceWith$ body, not a
// card Params map. Factored into its own function so the paramcensus rot
// guard can classify the read through a tracked helper parameter rather than
// an unclassified local.
func replacementLineWith(params map[string]string) string {
	return params["ReplaceWith"]
}

// replacementRedirectsToExile reports whether an Effect-delivered Event$
// Moved replacement is the plain "exile it instead" redirect: a
// ReplaceWith$ body `DB$ ChangeZone | Defined$ ReplacedCard | Destination$
// Exile` (Origin$/Hidden$ riders only), optionally chaining the one-shot
// self-exile idiom `DB$ ChangeZone | Defined$ Self | Origin$ Command |
// Destination$ Exile` (effects/zone.go ends the registration on it). That is
// exactly the body shape a PRINTED R: line resolves through the same
// replacement dispatcher (Rest in Peace, Leyline of the Void), so the
// Effect-created registration cannot lose the moved object: the replaced
// move is discarded and the body moves the same card to exile.
//
// The line's own parameters must be ones the Moved matcher reads (the
// Fizzle$/Optional$/Layer$ riders and any other stay on the loud Note path),
// and every other body shape -- a library/battlefield destination, counters,
// a RememberChanged$/plot/delayed-trigger chain -- keeps its loud Note.
//
// Before this, Mavinda, Students' Advocate's "if that spell would be put into
// your graveyard, exile it instead" was a Note: the recast card went back to
// the graveyard while the MayPlay grant still named it, so a {0} sorcery
// (Indicate) was recast forever inside one turn (cardfuzz batch5 line 7).
// The same promise is the whole point of Jace, Vryn's Prodigy, Dire Fleet
// Daredevil, Gaea's Will and the other ~20 carriers of this shape.
func replacementRedirectsToExile(params map[string]string, body string, svars map[string]string) bool {
	for k := range params {
		switch k {
		case "Event", "ValidCard", "ValidLKI", "Origin", "Destination", "ActiveZones",
			"EffectZone", "ReplaceWith", "Description":
		default:
			return false
		}
	}
	if replacementBodyAPI(body) != "ChangeZone" {
		return false
	}
	return redirectExileBody(replacementBodyParams(body), svars)
}

// redirectExileBody is replacementRedirectsToExile's body gate: body is the
// Key$ Value map of the ReplaceWith$ SVar body (a parsed SVar line, not a
// card Params map) and svars the face's SVar table its SubAbility$ names.
func redirectExileBody(body map[string]string, svars map[string]string) bool {
	if body["Defined"] != "ReplacedCard" || body["Destination"] != "Exile" {
		return false
	}
	for k, v := range body {
		switch k {
		case "DB", "Defined", "Destination", "Origin", "StackDescription":
		case "Hidden":
			if !strings.EqualFold(v, "True") {
				return false
			}
		case "SubAbility":
			line := svars[v]
			if replacementBodyAPI(line) != "ChangeZone" || !selfExileIdiom(replacementBodyParams(line)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// selfExileIdiom reports whether sub (a parsed SVar body) is exactly the
// one-shot `DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$
// Exile` idiom that ends the Effect's registration (effects/zone.go).
func selfExileIdiom(sub map[string]string) bool {
	return len(sub) == 4 && sub["Defined"] == "Self" && sub["Origin"] == "Command" && sub["Destination"] == "Exile"
}

// replacementBodyParams splits one SVar body line into its Key$ Value map.
func replacementBodyParams(body string) map[string]string {
	params := make(map[string]string)
	for seg := range strings.SplitSeq(body, "|") {
		key, val, ok := strings.Cut(strings.TrimSpace(seg), "$")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return params
}

// replacementLineCantHappen reports whether a parseReplacementLine-built
// replacement body declares Layer$ CantHappen with no body of its own -- the
// complete replacement is stopping the event (Mistrise Village's AntiMagic).
// Factored into its own function so the paramcensus rot guard can classify
// the read through a tracked helper parameter rather than an unclassified
// local, the same shape replacementLineWith takes.
func replacementLineCantHappen(params map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(params["Layer"]), "CantHappen")
}

// replacementLinePrevents reports whether a parseReplacementLine-built
// replacement body is the bodyless full-prevention form: Prevent$ True with
// no ReplaceWith$ body of its own -- the complete replacement is stopping
// the damage (Selfless Squire's RPrevent, task dponce1). Factored into its
// own function so the paramcensus rot guard can classify the read through a
// tracked helper parameter rather than an unclassified local, the same shape
// replacementLineCantHappen takes.
func replacementLinePrevents(params map[string]string) bool {
	return strings.EqualFold(params["Prevent"], "True")
}

// replacementBodyAPI names the API a retained replacement body's head
// resolves to ("DB$ PutCounter | Defined$ ReplacedCard | ..." ->
// "PutCounter"), so an effEffect registration gate can admit exactly the
// body shapes the rules dispatcher handles without hard-coding card names.
// An unparsable body returns "" (and the gate declines it).
func replacementBodyAPI(body string) string {
	head, _, _ := strings.Cut(body, "|")
	kind, api, ok := strings.Cut(strings.TrimSpace(head), "$")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(api) == "" {
		return ""
	}
	return strings.TrimSpace(api)
}

// effectOneShotDelayedMode names the event modes with a non-repeat delayed
// dispatch end to end: events.Apply's DelayedRegister decode recognizes the
// mode prefix without the |EF marker, and rules.checkEventDelayedTriggers has
// a non-EffectRepeat arm that fires it and removes it (one-shot). It is ONE
// home shared by effEffect's OneOff$ True decision and effDelayedTrigger's
// mode admission, so a mode can never be one-shot on one path and inert on
// the other. Mode$ Phase is deliberately absent: a phase registration is
// inherently one-shot (no |EF, consumed by its DelayedPush) and takes the
// Phase arm, not this event-mode one.
func effectOneShotDelayedMode(mode string) bool {
	switch mode {
	case "SpellCast", "ChangesZone", "ChangesController", "DamageDone", "AttackersDeclared":
		return true
	}
	return false
}

// Delayed registrations can express a turn ceiling, but not a continuous
// Effect's source-relative or next-turn lifetime. Reject those forms rather
// than register a promise that can fire after the Effect expires.
func effectTriggerThisTurnDuration(dur string) bool {
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "", "eot", "endofturn", "untilendofturn", "end of turn", "this turn":
		return true
	}
	return false
}

// effectTriggerBody resolves an Effect Triggers$ entry's SVar body: the
// source's own face table first, else the resolving ability's SVar table
// (the lookup the Effect trigger registration loop uses).
func effectTriggerBody(h Host, c *Ctx, name string) string {
	if name == "" {
		return ""
	}
	if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
		if raw := o.Face().SVars[name]; raw != "" {
			return raw
		}
	}
	return c.SVars[name]
}

// effectSelfExileOnCastTrigger reports whether tr is the Effect self-exile
// idiom: a SpellCast trigger whose Execute$ body moves the Effect itself out
// of the Command zone into exile (Forge's effect token; TARDIS's
// `SVar:ExileEffect:Mode$ SpellCast | EffectZone$ Command | ValidCard$
// Card.YouCtrl | Execute$ RemoveEffect | Static$ True` with
// `RemoveEffect:DB$ ChangeZone | Origin$ Command | Destination$ Exile |
// Defined$ Self`). The EffectZone$ Command marker plus the ChangeZone body
// identify it.
func effectSelfExileOnCastTrigger(h Host, c *Ctx, tr cards.Trigger) bool {
	if tr.Mode != "SpellCast" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(tr.Params["EffectZone"]), "Command") {
		return false
	}
	exec := strings.TrimSpace(tr.Params["Execute"])
	if exec == "" {
		return false
	}
	body := strings.ReplaceAll(effectTriggerBody(h, c, exec), " ", "")
	return strings.Contains(body, "ChangeZone") &&
		strings.Contains(body, "Origin$Command") &&
		strings.Contains(body, "Destination$Exile")
}

// effectSelfExileOnCastSpec scans an Effect's Triggers$ name list for the
// self-exile-on-cast idiom above and returns the cast spec that ends the
// Effect: the trigger's ValidCard$ (the spell that consumes the Effect), or
// "Card" for a body that names none. Empty when no entry has the shape, so a
// Triggers$-only Effect keeps its existing registrations untouched.
func effectSelfExileOnCastSpec(h Host, c *Ctx, names string) string {
	for name := range strings.FieldsSeq(names) {
		raw := effectTriggerBody(h, c, name)
		if raw == "" {
			continue
		}
		tr, ok := cards.ParseTriggerLine(raw)
		if !ok || !effectSelfExileOnCastTrigger(h, c, tr) {
			continue
		}
		if spec := strings.TrimSpace(tr.Params["ValidCard"]); spec != "" {
			return spec
		}
		return "Card"
	}
	return ""
}

// effectUntilEOT decides expiry for an Effect registration: a one-shot spell
// (instant/sorcery) source, an absent Duration$, or an explicit this-turn
// Duration$ is UntilEOT and is dropped at end-of-turn cleanup
// (rules' EndOfTurnCleanup). An explicit Permanent or source-relative form
// persists while its source stays on the battlefield, the same rule the layer
// effects use. A Duration$ that spans the controller's NEXT turn is NOT
// UntilEOT (it would expire a turn early); it is instead given a real
// turn-boundary lifetime (state.ContinuousEffect.UntilTurn) computed in
// rules.Engine.AddContinuous, so effectUntilEOT returns false for it.
func effectUntilEOT(h Host, source state.ObjID, dur string) bool {
	if strings.TrimSpace(dur) == "" {
		return true
	}
	if IsNextTurnDuration(dur) {
		return false
	}
	if o := h.Game().Obj(source); o != nil {
		if f := o.Face(); f != nil && (f.IsInstant() || f.IsSorcery()) {
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "eot", "endofturn", "untilendofturn", "untilyournextendstep",
		"untilhostleavesplayoreot", "untilendofcombat", "end of turn",
		"this turn", "thisturnandnextturn":
		return true
	}
	return false
}

// effCleanup is "DB$ Cleanup | ClearRemembered$ True": nothing in this build
// persists a Remembered/Imprinted list on an object yet (Ctx.Remembered is a
// per-resolution parameter, not stored state), so there is nothing to
// actually clear. The Note records that the step ran.
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
	mode := sa.Params["Mode"]
	turnUp := strings.EqualFold(strings.TrimSpace(mode), "TurnFaceUp")
	turnDown := strings.EqualFold(strings.TrimSpace(mode), "TurnFaceDown")
	unspecialize := strings.EqualFold(strings.TrimSpace(mode), "Unspecialize")
	optional := strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True")
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
	if strings.EqualFold(strings.TrimSpace(sa.Params["RememberChanged"]), "True") {
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
		if dest := strings.TrimSpace(sa.Params["Destination"]); dest != "" {
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
	mode := strings.TrimSpace(sa.Params["Mode"])
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
		set, unknown := state.ParsePhases(sa.Params["Phase"])
		if len(unknown) > 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed trigger at unrecognized phase " + sa.Params["Phase"]})
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
	if spec := strings.TrimSpace(sa.Params["RememberObjects"]); spec != "" && spec != "RememberedLKI" {
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
	text := sa.Params["Phase"]
	if vp := strings.TrimSpace(sa.Params["ValidPlayer"]); vp != "" {
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
	if spec := strings.TrimSpace(sa.Params["IsPresent"]); spec != "" {
		text += "|IP=" + spec
		if zone := strings.TrimSpace(sa.Params["PresentZone"]); zone != "" {
			text += "|PZ=" + zone
		}
		if cmp := strings.TrimSpace(sa.Params["PresentCompare"]); cmp != "" {
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
		exec := strings.TrimSpace(sa.Params["Execute"])
		if exec == "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed " + mode + " trigger with no Execute"})
			return
		}
		eventText := mode + ":" + delayedTriggerBody(sa)
		if strings.EqualFold(strings.TrimSpace(sa.Params["ThisTurn"]), "True") {
			eventText += "|TT=" + strconv.Itoa(int(h.Game().Turn))
		}
		h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
			Player: c.Controller, Step: h.Game().Step, Counter: exec,
			IDs: encodeRemembered(remembered), Text: eventText})
		return
	}
	exec := strings.TrimSpace(sa.Params["Execute"])
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
	parts := []string{"Mode$ " + strings.TrimSpace(sa.Params["Mode"])}
	add := func(prefix, value string) {
		if v := strings.TrimSpace(value); v != "" {
			parts = append(parts, prefix+v)
		}
	}
	add("ValidCard$ ", sa.Params["ValidCard"])
	add("ValidCards$ ", sa.Params["ValidCards"])
	add("Origin$ ", sa.Params["Origin"])
	add("Destination$ ", sa.Params["Destination"])
	add("ExcludedOrigins$ ", sa.Params["ExcludedOrigins"])
	add("ValidSource$ ", sa.Params["ValidSource"])
	add("ValidTarget$ ", sa.Params["ValidTarget"])
	add("CombatDamage$ ", sa.Params["CombatDamage"])
	add("ValidAttackers$ ", sa.Params["ValidAttackers"])
	add("ValidAttackersAmount$ ", sa.Params["ValidAttackersAmount"])
	add("AttackingPlayer$ ", sa.Params["AttackingPlayer"])
	add("AttackedTarget$ ", sa.Params["AttackedTarget"])
	add("ValidPlayer$ ", sa.Params["ValidPlayer"])
	add("ValidOriginalController$ ", sa.Params["ValidOriginalController"])
	add("ValidActivatingPlayer$ ", sa.Params["ValidActivatingPlayer"])
	add("PlayerTurn$ ", sa.Params["PlayerTurn"])
	add("ValidSA$ ", sa.Params["ValidSA"])
	add("TriggerZones$ ", sa.Params["TriggerZones"])
	add("ActiveZones$ ", sa.Params["ActiveZones"])
	add("ThisTurn$ ", sa.Params["ThisTurn"])
	add("Static$ ", sa.Params["Static"])
	add("IsPresent$ ", sa.Params["IsPresent"])
	add("PresentDefined$ ", sa.Params["PresentDefined"])
	add("PresentCompare$ ", sa.Params["PresentCompare"])
	add("PresentZone$ ", sa.Params["PresentZone"])
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
	if st := strings.TrimSpace(sa.Params["Static"]); st != "" && !strings.EqualFold(st, "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unmodelled DelayedTrigger Static$ " + st})
	}
	exec := strings.TrimSpace(sa.Params["Execute"])
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
	if v := strings.TrimSpace(sa.Params["ValidCard"]); v != "" {
		body += " | ValidCard$ " + v
	}
	if v := strings.TrimSpace(sa.Params["ValidActivatingPlayer"]); v != "" {
		body += " | ValidActivatingPlayer$ " + v
	}
	if v := strings.TrimSpace(sa.Params["ValidPlayer"]); v != "" {
		body += " | ValidPlayer$ " + v
	}
	if v := strings.TrimSpace(sa.Params["PlayerTurn"]); v != "" {
		body += " | PlayerTurn$ " + v
	}
	if v := strings.TrimSpace(sa.Params["ValidSA"]); v != "" {
		body += " | ValidSA$ " + v
	}
	if v := strings.TrimSpace(sa.Params["Static"]); v != "" {
		body += " | Static$ " + v
	}
	text := "SpellCast:" + body
	if strings.EqualFold(strings.TrimSpace(sa.Params["ThisTurn"]), "True") {
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
// the cap on a number the engine cannot honestly compute). For the four
// MaxRepeat carriers whose gate names such a body (Helm of Obedience,
// Grindstone, Sphinx's Tutelage, The Tale of Tamiyo) this trades the
// pre-gate loop's whole-library mill -- MaxRepeat$ is CardsInLibrary there
// -- for one iteration, the conservative direction; every one of them sits
// outside every repo deck and golden game.
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
func CharmRepeatModes(sa *cards.SA) bool {
	return sa != nil && strings.EqualFold(strings.TrimSpace(sa.Params["CanRepeatModes"]), "True")
}

// CharmModeBounds resolves a Charm's selectable range. Forge defaults
// MinCharmNum$ to CharmNum$, but an explicit MinCharmNum$ permits choosing
// fewer modes. Both values use Num so literal, SVar, and inline Count$ forms
// share the same evaluation in spell, trigger, and resolution paths.
// Optional$ True (Shadrix Silverquill's "you may choose two") lowers the
// minimum to 0: the election is real, and choosing nothing is a legal answer
// at every site that asks (the placement ask, the cast announcement and
// effCharm's own mid-resolution ask all share this one helper).
//
// The third result is CanRepeatModes$: when it is set, max is NOT clamped to
// the number of distinct modes (a repeatable CharmNum$ 5 over 3 modes is
// legal -- the Commands cycle), and the caller must mark its decision
// Repeatable so Decision.Validate permits the repeated index. The clamp is
// what makes a non-repeatable CharmNum$ greater than its mode count degrade
// to "pick every distinct mode" rather than demand an impossible answer.
func CharmModeBounds(h Host, c *Ctx, sa *cards.SA, choices int) (min, max int, repeat bool) {
	repeat = CharmRepeatModes(sa)
	max = int(Num(h, c, sa, "CharmNum", 1))
	if max < 1 {
		max = 1
	}
	min = max
	if _, ok := sa.Params["MinCharmNum"]; ok {
		min = int(Num(h, c, sa, "MinCharmNum", int32(min)))
	}
	if strings.EqualFold(sa.Params["Optional"], "True") {
		min = 0
	}
	if !repeat && max > choices {
		max = choices
	}
	if min < 0 {
		min = 0
	}
	return min, max, repeat
}

// CharmChoiceRestriction returns a Charm SA's ChoiceRestriction$ value
// ("ThisTurn", "ThisGame", "YourLastCombat"), or "" when the SA carries
// none. It is the ONE reader the ask sites filter through and the answer
// handler records through, so they cannot disagree about the scope.
func CharmChoiceRestriction(sa *cards.SA) string {
	if sa == nil {
		return ""
	}
	return strings.TrimSpace(sa.Params["ChoiceRestriction"])
}

// CharmEligibleModes filters a Charm's Choices$ SVar names against the mode
// picks already recorded on source (state.Object.ModeChoices) under the SA's
// ChoiceRestriction$ scope -- the "choose one that hasn't been chosen this
// turn" rule. The returned slice keeps the input's order, so the caller's
// option indices stay dense and map back to the same SVar names.
//
// ThisTurn, ThisGame and YourLastCombat are read from the source object's
// event-folded pick log. Unknown non-empty scopes fail open (with one Note per
// ask), rather than silently withholding a potentially legal mode.
func CharmEligibleModes(h Host, source state.ObjID, sa *cards.SA, choices []string) []string {
	scope := CharmChoiceRestriction(sa)
	if scope == "" || source == 0 {
		return choices
	}
	if scope != state.ModeScopeThisTurn && scope != state.ModeScopeThisGame && scope != state.ModeScopeYourLastCombat {
		h.Emit(events.Event{Kind: events.Note, Obj: source, Text: "unmodelled ChoiceRestriction$ scope: " + scope})
		return choices
	}
	o := h.Game().Obj(source)
	if o == nil || len(o.ModeChoices) == 0 {
		return choices
	}
	excluded := make(map[string]bool, len(choices))
	for _, mc := range o.ModeChoices {
		if mc.Scope != scope {
			continue
		}
		if scope == state.ModeScopeYourLastCombat &&
			mc.Turn == o.CurCombatTurn && mc.Combat == o.CurCombatCombat {
			// A pick made in the current combat does not constrain another
			// trigger in that same combat; YourLastCombat means the preceding
			// combat, whose picks were retained at BeginCombat rotation.
			continue
		}
		excluded[mc.Mode] = true
	}
	if len(excluded) == 0 {
		return choices
	}
	out := make([]string, 0, len(choices))
	for _, name := range choices {
		if !excluded[name] {
			out = append(out, name)
		}
	}
	return out
}

// RecordCharmChoices emits one events.Choose marker per answered mode name so
// a LATER offer of the same Charm on the same source sees the pick through
// CharmEligibleModes. It records all three supported corpus scopes; unknown
// scopes are noted at offer time and remain unrecorded. The scope is encoded
// in the event's Counter (state.ModeChoiceCounterPrefix + scope) and the mode
// name in Text; events.Apply stamps combat identity from its own clock, so a
// replay derives the same log.
func RecordCharmChoices(h Host, source state.ObjID, sa *cards.SA, names []string) {
	scope := CharmChoiceRestriction(sa)
	if (scope != state.ModeScopeThisTurn && scope != state.ModeScopeThisGame && scope != state.ModeScopeYourLastCombat) || source == 0 || len(names) == 0 {
		return
	}
	counter := state.ModeChoiceCounterPrefix + scope
	for _, name := range names {
		if name == "" {
			continue
		}
		h.Emit(events.Event{Kind: events.Choose, Obj: source, Counter: counter, Text: name})
	}
}

// CharmUniqueNone/Supported/Unsupported classify a Charm's chosen-mode set
// against the cross-mode "each mode must target a different player" family
// (Shadrix Silverquill, the Tarkir/Ninja duo cycle, Balor, Vindictive Lich,
// Chaos Balor -- 8 corpus files, all trigger-side DB$ Charm).
type CharmUniqueStatus int

const (
	// CharmUniqueNone: fewer than two target-bearing chosen modes, or none
	// of them carries TargetUnique$ — the ordinary shared-target narrowing
	// applies, byte-identically to the pre-family engine.
	CharmUniqueNone CharmUniqueStatus = iota
	// CharmUniqueSupported: at least two target-bearing chosen modes, every
	// one of them single-target (no TargetMin$/TargetMax$ beyond 1), every
	// one targeting the SAME player-kind spec ("Player" or "Opponent"),
	// and at least one carrying TargetUnique$ True. The combined
	// different-player target ask is posed and the per-mode split applies.
	CharmUniqueSupported
	// CharmUniqueUnsupported: TargetUnique$ is present on the chosen modes
	// but some member the combined ask cannot serve — differing ValidTgts$
	// specs, a non-player spec, or multi-target bounds. The ordinary
	// narrowing keeps and a loud Note names the shape (never silent).
	CharmUniqueUnsupported
)

// charmUniquePlayerSpec reports whether a ValidTgts$ spec names players in
// the exact form every corpus carrier of the family uses. Wider player
// grammars are not served: a "You" spec could never satisfy two different
// players anyway, and a compound spec's candidates are not all players.
func charmUniquePlayerSpec(spec string) bool {
	return spec == "Player" || spec == "Opponent"
}

// charmUniqueBounds mirrors rules' targetBounds for the single-target check:
// absent TargetMin$/TargetMax$ mean 1..1 (the M1 single-target contract).
// Anything a caller set explicitly beyond 1..1 keeps the mode out of the
// combined ask.
func charmUniqueBounds(sa *cards.SA) (min, max int) {
	min, max = 1, 1
	if v, ok := sa.Params["TargetMin"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			min = n
		}
	}
	if v, ok := sa.Params["TargetMax"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			max = n
		}
	}
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// CharmCrossModeShape classifies a Charm's mode list for the cross-mode
// TargetUnique family, with a reason fragment for the Unsupported loud Note.
// It is deliberately a property of the CHARM's full
// Choices$ list, not of whichever subset a particular answer selected: the
// classification must be stable across the charm's whole lifetime, because
// the per-mode target split (effCharm) and the suspension re-entry
// (rules' resumeResolution) re-derive it after a mid-mode ask — and a
// continuation can carry only a suffix of the original chosen order.
// Measured at the corpus pin: every charm outside the 8-file family carries
// TargetUnique$ on NONE of its target-bearing modes (the anyUnique trigger
// below never fires for them), so they classify None and keep today's
// narrowing byte-identically.
func CharmCrossModeShape(svars map[string]string, modes []string) (CharmUniqueStatus, string) {
	var spec string
	tbms, anyUnique, oneSpec, boundsOK := 0, false, true, true
	for _, name := range modes {
		sub := cards.ResolveSVar(svars, strings.TrimSpace(name))
		if sub == nil {
			continue
		}
		s := strings.TrimSpace(sub.Params["ValidTgts"])
		if s == "" {
			continue
		}
		tbms++
		if strings.EqualFold(sub.Params["TargetUnique"], "True") {
			anyUnique = true
		}
		if spec == "" {
			spec = s
		} else if s != spec {
			oneSpec = false
		}
		if min, max := charmUniqueBounds(sub); min != 1 || max != 1 {
			boundsOK = false
		}
	}
	if tbms < 2 || !anyUnique {
		return CharmUniqueNone, ""
	}
	if !oneSpec {
		return CharmUniqueUnsupported, "differing ValidTgts$ specs across the target-bearing modes"
	}
	if !charmUniquePlayerSpec(spec) {
		return CharmUniqueUnsupported, "ValidTgts$ " + spec + " is not a player-kind spec"
	}
	if !boundsOK {
		return CharmUniqueUnsupported, "a target-bearing mode declares multi-target bounds"
	}
	return CharmUniqueSupported, ""
}

// charmCrossModeRun is effCharm's cross-mode TargetUnique family runner. It
// runs the chosen modes in order, giving each target-bearing mode its OWN
// target — the positional slice of Ctx.Targets the combined placement ask
// recorded — instead of the one-undivided target list every mode shared
// before. A mode that suspends (donnie's and mikey's hidden graveyard pick)
// stops the run and reports the remaining modes as a continuation
// (Host.SuspendCharmRest), so the rest re-enter through the answered ask's
// chain rather than running while the suspension is still outstanding.
// Non-target-bearing modes keep the shared context exactly as before.
// Returns false when the shape does not apply and the caller must keep the
// historical shared-target loop.
func charmCrossModeRun(h Host, c *Ctx, sa *cards.SA, names []string) bool {
	choices := strings.Split(sa.Params["Choices"], ",")
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	if status, _ := CharmCrossModeShape(c.SVars, choices); status != CharmUniqueSupported {
		return false
	}
	var tbmIdx []int
	for i, name := range names {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			tbmIdx = append(tbmIdx, i)
		}
	}
	k := len(tbmIdx)
	if k == 0 || len(c.Targets) < k {
		// The running mode set carries target-bearing modes the placement ask
		// could not serve (an insufficient-candidate fallback, or a spell-side
		// single-target ask): keep the shared list, exactly the historical
		// narrowing, rather than inventing an assignment the ask never made.
		return false
	}
	// Assignment: the j-th target-bearing mode of the RUNNING list takes
	// targets[len(targets)-k+j]. On a full run that is targets[j] — the
	// combined ask's answer order, which is the chosen-mode order. On a
	// suffix continuation the remaining target-bearing modes are the last
	// ones of the original order, so the last k targets are theirs.
	base := len(c.Targets) - k
	ti := 0
	for i, name := range names {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		if ti < k && tbmIdx[ti] == i {
			saved := c.Targets
			savedOffered := c.OfferedSA
			c.Targets = []state.Target{c.Targets[base+ti]}
			// The combined placement ask covered THIS mode's targeting (its
			// assignment is positional); mark it so the generic ValidTgts$
			// pre-ask does not re-pose the cross-mode question per mode --
			// both on the initial pass (where the resolution-level marker's
			// bool would also suppress it) and on a charm_rest resume, where
			// the resume ctx carries only the FIRST chosen mode as OfferedSA
			// (task mvts1).
			c.OfferedSA = sub
			Resolve(h, c, sub)
			c.OfferedSA = savedOffered
			c.Targets = saved
			ti++
		} else {
			Resolve(h, c, sub)
		}
		if h.Suspended() {
			// The mode's own chain posed a mid-resolution ask: stop here. The
			// remaining modes resume through SuspendCharmRest's continuation
			// once the answer lands — never while the suspension is live (the
			// historical loop ran them immediately, before the answered mode
			// had even completed). An empty rest (this was the last mode) is
			// still reported, so the Charm re-enters and walks its own Sub
			// instead of the enclosing loop recording a plain continuation
			// that resumes at a nil Sub and emits a false degradation Note.
			h.SuspendCharmRest(sa, names[i+1:])
			return true
		}
	}
	return true
}

// effVillainousChoice makes the player named by Defined$ choose one of the
// supplied ability bodies. Unlike a modal trigger's placement choice, the
// victim's choice happens during resolution: the victim is remembered before
// the chosen body runs, so Defined$ Remembered and Player.IsRemembered in the
// body refer to the victim.
func effVillainousChoice(h Host, c *Ctx, sa *cards.SA) {
	choices := strings.Split(sa.Params["Choices"], ",")
	if len(choices) == 0 || c.SVars == nil {
		return
	}
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	// A resumed answer is scoped to the current victim. Once its body has
	// completed, advance to the next Defined$ player and pose a fresh ask.
	if c.Modes != nil {
		names := c.Modes
		c.Modes = nil
		for _, name := range names {
			if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
				Resolve(h, c, sub)
			}
			if h.Suspended() {
				// The chosen body posed a nested mid-resolution ask (DBSac's
				// sacrifice picker is the live carrier). Record this
				// primitive's own continuation so the remaining victims are
				// still asked once that ask's chain completes, instead of
				// being stranded: the enclosing Resolve loop would otherwise
				// resume only sa.Sub (nil for a VillainousChoice) and the
				// outer levels would degrade to no-sub-ability Notes.
				h.SuspendVillainousRest(sa, VillainousRest{
					Victims: append([]state.Target(nil), c.VillainousVictims...),
					Next:    c.VillainousIndex + 1})
				return
			}
		}
		c.VillainousIndex++
	}
	if c.VillainousVictims == nil {
		for _, target := range Defined(h, c, sa) {
			if target.IsPlayer {
				c.VillainousVictims = append(c.VillainousVictims, target)
			}
		}
		// The trigger's original Remembered can already end in its victim
		// (Attacks supplies the defender). That is not proof of a resumed
		// body: the explicit VillainousRest cursor handles nested asks.
	}
	for c.VillainousIndex < len(c.VillainousVictims) {
		victim := c.VillainousVictims[c.VillainousIndex]
		// The body is evaluated against this victim, not an earlier victim.
		c.Remembered = []state.Target{victim}
		d := &decision.Decision{Player: victim.Player, Kind: decision.KModes,
			Min: 1, Max: 1, Source: c.Source, ResumeKind: "villainous",
			ResumeSA: sa, ResumeModes: append([]string(nil), choices...),
			ResumeRemembered:        append([]state.Target(nil), c.Remembered...),
			ResumeVillainousVictims: append([]state.Target(nil), c.VillainousVictims...),
			ResumeVillainousIndex:   c.VillainousIndex,
			Prompt:                  "Choose a villainous option"}
		for i, name := range choices {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode",
				Label: CharmModeLabel(cards.ResolveSVar(c.SVars, name), name),
				Obj:   c.Source, Player: victim.Player})
		}
		if Ask(h, d) == AskAsked {
			return
		}
		// R-9: an effects-only host has no chooser, so deterministically take
		// the first option and continue to the next victim.
		if sub := cards.ResolveSVar(c.SVars, choices[0]); sub != nil {
			Resolve(h, c, sub)
		}
		if h.Suspended() {
			return
		}
		c.VillainousIndex++
	}
}

// charmDistinctTargetRun runs a distinct modal Charm with one target group
// per selected target-bearing mode. ModeTargets is aligned to those modes;
// non-targeting modes still run with the ordinary shared context.
func charmDistinctTargetRun(h Host, c *Ctx, sa *cards.SA, names []string) bool {
	if len(c.ModeTargets) < 2 {
		return false
	}
	offset := 0
	for _, name := range c.ModesSeen {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			offset++
		}
	}
	for i, name := range names {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		savedTargets, savedOffered, savedMarker := c.Targets, c.OfferedSA, c.TargetsOffered
		savedScope, savedScopeSA := c.CharmModeScope, c.CharmModeSA
		if strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			if offset >= len(c.ModeTargets) {
				return false
			}
			c.Targets = c.ModeTargets[offset]
			c.OfferedSA = sub
			c.TargetsOffered = true
			// Publish the narrowed group so a mid-resolution ask posed
			// anywhere under this mode re-enters scoped to it instead of to
			// the stack object's whole flat list (Ctx.CharmModeScope).
			c.CharmModeScope = c.Targets
			c.CharmModeSA = sub
			offset++
		}
		Resolve(h, c, sub)
		c.Targets, c.OfferedSA, c.TargetsOffered = savedTargets, savedOffered, savedMarker
		c.CharmModeScope, c.CharmModeSA = savedScope, savedScopeSA
		if h.Suspended() {
			h.SuspendCharmRest(sa, names[i+1:])
			return true
		}
	}
	return true
}

// charmGenericPlayers is the per-Defined$-player api:GenericChoice path: each
// player the SA's Defined$ names chooses one of the same Choices$ in turn.
// With TempRemember$ Chooser the chooser is bound as Ctx.Remembered for ITS
// chosen body (the binding Seize the Spotlight's Fame/Fortune bodies read as
// Defined$ Remembered) and unbound once that body finishes; without the param
// Remembered is left untouched, as Forge leaves it. A choice whose
// UnlessCost$ the chooser cannot presently pay is not offered, and when no
// choice is payable FallbackAbility$ is resolved for that chooser instead of
// asking (Forge ChooseGenericEffect). The outer SubAbility$ runs once, after
// every chooser has answered.
//
// It returns true when it owns the resolution. It deliberately declines the
// shapes the existing controller ask already serves correctly:
//
//   - a Charm (never defined$-per-player);
//   - a GenericChoice with no Choices$;
//   - a GenericChoice whose Defined$ is absent, resolves to exactly the
//     resolving controller (Defined$ You), or resolves to anything that is
//     not all players (Defined$ Targeted on a spell's creature, Defined$
//     Valid <filter>): those keep the existing ask, so this ticket cannot
//     change an object-defined carrier.
//
// A GenericChoice whose Defined$ is one of the PLAYER-role selectors
// (Opponent / Player / Player.Opponent / Player.Other / You) but resolves to NO
// players (an unbound TriggeredPlayer, an empty Remembered) does NOT fall back
// to the controller: the ask would invent a chooser the card never named, so
// the resolution is a loud no-op instead. Any other selector resolving empty is
// left to the existing path.
func charmGenericPlayers(h Host, c *Ctx, sa *cards.SA) bool {
	if sa.API != "GenericChoice" {
		return false
	}
	choices := strings.Split(sa.Params["Choices"], ",")
	if len(choices) == 0 {
		return false
	}
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	// A re-entry for an answered/continued chooser carries the cursor; the SA
	// may be reached mid-resolution with c.Modes already naming the answer.
	if c.GenericChoosers != nil {
		return charmGenericPlayersRun(h, c, sa, choices)
	}
	defined := strings.TrimSpace(sa.Params["Defined"])
	if defined == "" {
		return false
	}
	resolved := Defined(h, c, sa)
	var players []state.Target
	for _, t := range resolved {
		if !t.IsPlayer {
			// A mixed or object-defined set (Defined$ Targeted, Defined$
			// Valid <filter>): not the per-player shape, so keep the existing
			// controller ask untouched.
			return false
		}
		players = append(players, t)
	}
	if len(players) == 1 && players[0].Player == c.Controller {
		// Defined$ You: the existing controller ask is already exact. Leave it
		// (and its ResumeKind "modes") alone.
		return false
	}
	if len(players) == 0 {
		if !playerRoleDefined(defined) {
			// A non-player selector that happened to resolve to nothing: leave
			// it to the existing path rather than changing its behaviour.
			return false
		}
		// A player-role selector that named no player: asking the controller
		// would choose for a player the card never named. Record why and do
		// nothing.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "GenericChoice Defined$ " + defined + " resolved no players"})
		return true
	}
	c.GenericChoosers = players
	c.GenericChooserIndex = 0
	return charmGenericPlayersRun(h, c, sa, choices)
}

// playerRoleDefined reports whether a GenericChoice Defined$ selector names a
// PLAYER ROLE (so an empty resolution is a real "no choosers", not an
// object-definition that must keep the existing path). The qualified
// Player.Opponent / Player.Other spellings are included, as are the trigger
// roles that always resolve to a player. Every ambiguous selector (Targeted,
// TriggeredTarget, ParentTarget, Valid <filter>, Remembered, ...) is NOT, so an
// empty resolution there keeps the existing path unchanged.
func playerRoleDefined(defined string) bool {
	switch defined {
	case "Opponent", "Player", "Player.Opponent", "Player.Other", "You",
		"TriggeredPlayer", "TriggeredDefendingPlayer":
		return true
	}
	return false
}

// charmGenericPlayersRun drives the chooser loop. Ctx.GenericChooserIndex is
// the chooser still to ask; a non-nil Ctx.Modes is the answer for the chooser
// at index-1, whose chosen body has not yet run (resumeResolution advanced the
// index PAST the answered chooser, so running it here and then continuing the
// loop asks the next chooser exactly once). A nested ask inside a chosen body
// reports SuspendGenericChoiceRest so the remaining choosers survive it; a
// nested GenericChoice in that body sees a nil cursor for the body's walk
// (cleared around Resolve, the fx41 discipline) and resolves its own Defined$
// rather than inheriting this one.
func charmGenericPlayersRun(h Host, c *Ctx, sa *cards.SA, choices []string) bool {
	// TempRemember$ (Forge ChooseGenericEffect): when present, the chooser is
	// the Remembered object for ITS chosen body (Defined$/UnlessPayer$
	// Remembered name that player), and the binding is dropped after the
	// chooser's body finishes so it does not leak to the next chooser or the
	// outer SubAbility$. Without the param Forge leaves Remembered untouched,
	// so a body that did not ask for the binding must not get one.
	tempRemember := strings.TrimSpace(sa.Params["TempRemember"]) != ""
	baselineRemembered := append([]state.Target(nil), c.Remembered...)
	// FallbackAbility$ (Forge ChooseGenericEffect): the ability resolved for a
	// chooser when NONE of the Choices$ is payable RIGHT NOW -- Forge drops a
	// choice whose UnlessCost$ the chooser cannot pay, and when that empties
	// the list it runs the fallback instead of asking.
	fallback := strings.TrimSpace(sa.Params["FallbackAbility"])
	// runBody runs one chosen SVar with the chooser cursor cleared (a nested
	// GenericChoice resolves its own Defined$, never inheriting this cursor).
	// It reports whether the body suspended on a nested ask.
	runBody := func(name string) bool {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			return false
		}
		savedChoosers, savedIndex := c.GenericChoosers, c.GenericChooserIndex
		c.GenericChoosers, c.GenericChooserIndex = nil, 0
		Resolve(h, c, sub)
		c.GenericChoosers, c.GenericChooserIndex = savedChoosers, savedIndex
		return h.Suspended()
	}
	if c.Modes != nil {
		names := c.Modes
		c.Modes = nil
		chooser := c.GenericChoosers[c.GenericChooserIndex-1]
		for _, name := range names {
			if tempRemember {
				c.Remembered = []state.Target{chooser}
			}
			suspended := runBody(name)
			c.Remembered = append([]state.Target(nil), baselineRemembered...)
			if !suspended {
				continue
			}
			// Preserve the enclosing remembered set as well as the chooser
			// cursor while the chosen body's nested ask is suspended.
			h.SuspendGenericChoiceRest(sa, GenericChoiceRest{
				Choosers:   append([]state.Target(nil), c.GenericChoosers...),
				Next:       c.GenericChooserIndex,
				Remembered: append([]state.Target(nil), baselineRemembered...)})
			return true
		}
	}
	for c.GenericChooserIndex < len(c.GenericChoosers) {
		chooser := c.GenericChoosers[c.GenericChooserIndex]
		// The chosen body reads Defined$ Remembered as THIS chooser, and only
		// when TempRemember$ asked for that binding.
		if tempRemember {
			c.Remembered = []state.Target{chooser}
		}
		available := genericChoiceAvailable(h, c, chooser.Player, choices)
		c.Remembered = append([]state.Target(nil), baselineRemembered...)
		if len(available) == 0 {
			// No choice is payable. Forge runs FallbackAbility$ instead of
			// asking; with the chooser still bound (TempRemember$) its body
			// reads Defined$ Remembered as that player.
			if fallback != "" {
				if tempRemember {
					c.Remembered = []state.Target{chooser}
				}
				suspended := runBody(fallback)
				c.Remembered = append([]state.Target(nil), baselineRemembered...)
				if suspended {
					h.SuspendGenericChoiceRest(sa, GenericChoiceRest{
						Choosers:   append([]state.Target(nil), c.GenericChoosers...),
						Next:       c.GenericChooserIndex + 1,
						Remembered: append([]state.Target(nil), baselineRemembered...)})
					return true
				}
			} else {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "GenericChoice no payable choice and no FallbackAbility"})
			}
			c.GenericChooserIndex++
			continue
		}
		d := &decision.Decision{Player: chooser.Player, Kind: decision.KModes,
			Min: 1, Max: 1, Source: c.Source, ResumeKind: "generic_players", ResumeSA: sa,
			ResumeModes:               append([]string(nil), available...),
			ResumeRemembered:          append([]state.Target(nil), c.Remembered...),
			ResumeGenericChoosers:     append([]state.Target(nil), c.GenericChoosers...),
			ResumeGenericChooserIndex: c.GenericChooserIndex,
			Prompt:                    "Choose 1 to 1 mode(s)"}
		for i, name := range available {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode",
				Label: CharmModeLabel(cards.ResolveSVar(c.SVars, name), name),
				Obj:   c.Source, Player: chooser.Player})
		}
		if Ask(h, d) == AskAsked {
			return true
		}
		// R-9: an effects-only host has no chooser, so deterministically take
		// the first option for this chooser and continue to the next.
		if tempRemember {
			c.Remembered = []state.Target{chooser}
		}
		suspended := runBody(available[0])
		c.Remembered = append([]state.Target(nil), baselineRemembered...)
		if suspended {
			// Preserve both cursor and outer remembered set across the nested ask.
			h.SuspendGenericChoiceRest(sa, GenericChoiceRest{
				Choosers:   append([]state.Target(nil), c.GenericChoosers...),
				Next:       c.GenericChooserIndex + 1,
				Remembered: append([]state.Target(nil), baselineRemembered...)})
			return true
		}
		c.GenericChooserIndex++
	}
	if tempRemember {
		// Forge restores the complete remembered set that preceded the
		// temporary chooser binding, including any enclosing player remembers.
		c.Remembered = append([]state.Target(nil), baselineRemembered...)
	}
	return true
}

// genericChoiceAvailable filters a GenericChoice's Choices$ to the names the
// chooser can actually select, per Forge ChooseGenericEffect: a choice whose
// UnlessCost$ the chooser cannot presently pay is not offered (the same
// reachability gate the unless ask itself uses, so the option list and the pay
// decision cannot disagree). A choice with no UnlessCost$ is always available;
// a host that cannot price costs (an effects-only embedder) keeps the historic
// full list, so R-9 still degrades deterministically rather than silently
// dropping choices. FORGE ALSO drops a choice failing getRestrictions(); the
// engine's cards.SA carries no such field, and no corpus GenericChoice choice
// uses Restrictions$ today.
func genericChoiceAvailable(h Host, c *Ctx, payer state.PlayerID, choices []string) []string {
	checker, priced := h.(interface {
		UnlessCostPayableFromCtx(state.PlayerID, string, *Ctx) bool
	})
	out := make([]string, 0, len(choices))
	for _, name := range choices {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		if !priced {
			out = append(out, name)
			continue
		}
		raw := strings.TrimSpace(sub.Params["UnlessCost"])
		if raw == "" || checker.UnlessCostPayableFromCtx(payer, raw, c) {
			out = append(out, name)
		}
	}
	return out
}

// effCharm runs the selected Choices$ sub-abilities in chosen order.
// Cast spells (CR 601.2b) and triggered abilities (CR 603.3c) arrive with
// Ctx.Modes pre-seeded from their earlier announcement. A Charm reached only
// during resolution still poses KModes and suspends until resumeResolution
// re-enters it with Ctx.Modes. A host that cannot ask retains the deterministic
// first-mode stand-in and records why with a Note.
func effCharm(h Host, c *Ctx, sa *cards.SA) {
	if c.SVars == nil {
		return
	}
	if charmGenericPlayers(h, c, sa) {
		return
	}
	choices := strings.Split(sa.Params["Choices"], ",")
	if len(choices) == 0 {
		return
	}
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	// Re-entry after the modal choice was answered: Ctx.Modes already names
	// the chosen SVars in execution order, so run exactly those and do not
	// ask again.
	if c.Modes != nil {
		// fx41: take the names into a local and clear c.Modes BEFORE running
		// them. The same Ctx is handed to Resolve for every mode AND to the
		// Charm's own SubAbility$, and nothing else in the walk reads Modes,
		// so an uncleared field would leak the OUTER Charm's answered modes
		// into a NESTED Charm reached anywhere below it -- that inner
		// effCharm sees Modes != nil, takes this re-entry branch, and "runs"
		// the outer's mode names against its own SVars instead of posing its
		// own ask (or, when a name resolves back to a chain containing it,
		// re-resolves itself endlessly). Clearing here confines the answer
		// to the Charm that asked for it.
		names := c.Modes
		c.Modes = nil
		if charmDistinctTargetRun(h, c, sa, names) {
			return
		}
		if charmCrossModeRun(h, c, sa, names) {
			return
		}
		// Task mvts1, two guards the generic ValidTgts$ pre-ask needs here.
		//
		// Coverage: a placement-announced modal resolution asked every CHOSEN
		// target-bearing mode's targeting in its placement ask (the combined
		// per-mode ask), but a resume ctx carries only the FIRST of them as
		// Ctx.OfferedSA (rules' offeredTargetSA returns the first
		// target-bearing chosen mode). modalOffered detects that derivation
		// -- OfferedSA set and NOT the Charm root itself -- and marks each
		// target-bearing mode as covered while it dispatches, so the pre-ask
		// cannot re-pose the placement question per mode. A mid-resolution
		// Charm (its own KModes answered) has no modal derivation -- its
		// OfferedSA is nil or the root's own covered SA -- and its
		// target-bearing modes keep their real asks.
		modalOffered := c.OfferedSA != nil && c.OfferedSA.Line != sa.Line
		// CanRepeatModes$ (CR 601.2b): the covering ask -- the cast
		// announcement's or the placement ask's ONE target list -- covers the
		// FIRST occurrence of each target-bearing mode only. A later occurrence
		// of the same mode must keep its own targeting: OfferedSA is dropped
		// for the dispatch (chosenTargetsFor's Line match would otherwise skip
		// it) and the root TargetsOffered marker is shed for it (both pre-ask
		// gates read it at depth 0), so the mode's own mid-resolution ask --
		// chosenTargetsFor's for every API, changeZoneChosenTargets's for an
		// API$ ChangeZone body, which also needs the shared list out of sight
		// (its len(c.Targets) > 0 placement guard) -- poses for THIS instance.
		// "Return target creature to its owner's hand" chosen three times then
		// asks three targets and returns three creatures, instead of silently
		// re-running the mode against the one shared target. The seen-set is
		// seeded from Ctx.ModesSeen (rules' charm_rest arm): after a suspension
		// the re-entry walks only the REST of the multiset, so "first occurrence
		// in this walk" alone cannot see the instances the earlier passes
		// already ran.
		seen := make(map[string]bool, len(names))
		for _, n := range c.ModesSeen {
			seen[n] = true
		}
		for i, name := range names {
			if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
				savedOffered, savedTargets, savedMark := c.OfferedSA, c.Targets, c.TargetsOffered
				first := !seen[name]
				seen[name] = true
				if modalOffered && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
					if first {
						c.OfferedSA = sub
					} else {
						c.OfferedSA = nil
						c.TargetsOffered = false
						if sub.CompiledAPI() == cards.APIChangeZone || sub.API == "ChangeZone" {
							c.Targets = nil
						}
					}
				}
				Resolve(h, c, sub)
				c.OfferedSA, c.Targets, c.TargetsOffered = savedOffered, savedTargets, savedMark
			}
			if h.Suspended() {
				// A mode's own chain posed a mid-resolution ask: never run the
				// remaining modes while a decision is pending (Engine.ask
				// panics on the overwrite). Report the rest as a charm-rest
				// continuation, the same report the cross-mode runner makes,
				// so they run once the answer lands. An empty rest (this was
				// the LAST mode) is reported too, so the Charm re-enters to
				// walk its own Sub instead of the enclosing loop recording a
				// plain continuation that degrades to a false no-sub-ability
				// Note.
				h.SuspendCharmRest(sa, names[i+1:])
				return
			}
		}
		return
	}
	// Subs is resolved once per choice, so the label (SpellDescription$ on
	// the choice's own SVar body) and the mode-run share one parse; the
	// ordering of options mirrors Choices$ order, which is also how the
	// engine maps a chosen index back to an SVar name.
	subs := make([]*cards.SA, len(choices))
	for i, name := range choices {
		subs[i] = cards.ResolveSVar(c.SVars, name)
	}
	// ChoiceRestriction$ ("choose one that hasn't been chosen this turn / this
	// game"): drop the modes the source already chose under the same scope
	// before posing the ask. The filtered list is what the bounds clamp and the
	// options are built from, so the answer's indices map straight back to
	// eligible SVar names (d.ResumeModes). When every mode is exhausted the
	// ordinary min-over-modes decline below makes the Charm do nothing, which
	// is exactly the oracle's "if you can't choose, nothing happens".
	if eligible := CharmEligibleModes(h, c.Source, sa, choices); len(eligible) != len(choices) {
		filteredSubs := make([]*cards.SA, len(eligible))
		for i, name := range eligible {
			filteredSubs[i] = cards.ResolveSVar(c.SVars, name)
		}
		choices, subs = eligible, filteredSubs
	}
	min, max, repeat := CharmModeBounds(h, c, sa, len(choices))
	if min > len(choices) && !repeat {
		// Forge declines a Charm whose required minimum exceeds its available
		// modes. A repeatable Charm can always fill its slots by repeating a
		// single mode, so it never declines on this ground. A no-engine host
		// must likewise make no arbitrary choice.
		return
	}
	// param:api:Charm.Random (Random$ True / Random$ Compare with
	// RandomCompareSVar$/RandomCompare$): a Charm whose mode is picked AT
	// RANDOM rather than asked. The direction is the card oracle's, not the
	// brief's gloss: Typhoid Mary, Fractured ("choose one at random. If you
	// discarded a card this turn, you choose one instead", RandomCompare$
	// LT1 over SVar Y = CardsDiscardedThisTurn) is random exactly while the
	// comparison HOLDS, and a failed comparison reverts to the ordinary
	// KModes ask below. An unresolvable comparison (a missing
	// RandomCompareSVar$, an unmodelled count head, an unparseable
	// comparator) fails to the ask too -- never to a fake random, the
	// permissive direction. Only the single-slot shape is picked: a Random$
	// Charm whose CharmNum$ fills several slots keeps the ordinary ask
	// (measured corpus-unreachable -- every Random$ carrier, 5 files, is
	// single-slot), because a multi-pick cannot share this suspension-free
	// path.
	if CharmRandomChosen(h, c, sa) && min == 1 && max == 1 && !repeat {
		idx := h.Rand(len(choices))
		label := charmModeLabel(choices, subs, idx)
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "chose a mode at random: " + label})
		if subs[idx] != nil {
			Resolve(h, c, subs[idx])
		}
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KModes,
		Min: min, Max: max, Source: c.Source, Repeatable: repeat,
		ResumeKind: "modes", ResumeSA: sa,
		Prompt: "Choose " + strconv.Itoa(min) + " to " + strconv.Itoa(max) + " mode(s)"}
	for i, name := range choices {
		d.Options = append(d.Options, decision.Option{
			Index: i, Kind: "mode", Label: CharmModeLabel(subs[i], name),
			Obj: c.Source, Player: c.Controller})
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters this effect with Ctx.Modes set.
	}
	// Fuzz/no-engine host: the deterministic first-mode default (R-9), with
	// the Note that records why the richer path did not run. (AskEmpty is
	// unreachable by construction -- charmNum is clamped to >= 1 and
	// strings.Split never yields fewer than one choice -- but the shared
	// helper owns the guard either way.)
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "chose its first mode (no engine host to ask)"})
	if subs[0] != nil {
		Resolve(h, c, subs[0])
	}
}

// CharmRandomChosen reports whether the Charm's mode is chosen AT RANDOM
// rather than asked (param:api:Charm.Random):
//
//   - `Random$ True` is always random (Outlaws' Merriment, Cult of Skaro,
//     Umaru the Raging Yeti, Summon the Magus Sisters);
//   - `Random$ Compare` is random exactly while the RandomCompareSVar$
//     comparison holds -- the card oracle's direction, NOT the brief's
//     gloss. Typhoid Mary, Fractured's own oracle quote ("choose one at
//     random. If you discarded a card this turn, you choose one instead")
//     with RandomCompare$ LT1 over Y = CardsDiscardedThisTurn reads: zero
//     discards (LT1 holds) -> random; a discard this turn (LT1 fails) ->
//     the player chooses.
//
// The comparison rides the shared CheckSVarHolds evaluator (the SVar table,
// then the source face's own, through EvalCountOK), so every head that
// evaluates for CheckSVar$/SVarCompare$ gates evaluates here too. A
// comparison that does not EVALUATE (missing RandomCompareSVar$, an
// unmodelled count head, an unparseable comparator) reports false -- the
// ordinary ask keeps the choice, never a fake random. Any other Random$
// value is unread: the ordinary ask applies.
//
// Both mode-ask SITES consult this beside effCharm itself: the trigger
// placement ask (rules' askTriggerModes) and the cast-time announcement
// (rules' castModeAsk) skip their ask for a random Charm, so the pick (or
// the failed comparison's ask) happens once, at resolution, in effCharm --
// the rng draw stays in the replay-exact resolution path instead of
// split-braining a placement-time pick with a resolution-time run.
func CharmRandomChosen(h Host, c *Ctx, sa *cards.SA) bool {
	switch strings.TrimSpace(sa.Params["Random"]) {
	case "True":
		return true
	case "Compare":
		holds, evaluated := CheckSVarHolds(h, c, sa.Params["RandomCompareSVar"], sa.Params["RandomCompare"])
		return evaluated && holds
	}
	return false
}

// CharmModeLabel is the printed display label of one Charm mode whose
// resolved body is sub: the mode body's own SpellDescription$ when it
// carries one, else the first SpellDescription$ found walking the body's
// SubAbility$ chain, else fallback -- the raw SVar name. In the corpus the
// chain-only shape is exactly the printed bullet text of the card's Oracle
// line: What Must Be Done's Release Juno mode carries its description one
// hop down (on DBChangeZone), and Varchild's War-Riders' two upkeep modes
// carry theirs on SurvivorDistribution and Sacrifice, so a mode whose
// SpellDescription$ rides a sub is still labelled by the card's printed
// words, not its SVar name. Body-first precedence keeps every mode that
// already labelled by its own SpellDescription$ byte-identical. The chain
// is linked by cards' resolver (ResolveSVar/link, depth-capped at
// maxSVarDepth), so the walk is a plain pointer walk: no re-resolution and
// no new cycle risk. A nil sub returns the fallback.
func CharmModeLabel(sub *cards.SA, fallback string) string {
	if sub == nil {
		return fallback
	}
	if d := strings.TrimSpace(sub.Params["SpellDescription"]); d != "" {
		return d
	}
	for s := sub.Sub; s != nil; s = s.Sub {
		if d := strings.TrimSpace(s.Params["SpellDescription"]); d != "" {
			return d
		}
	}
	return fallback
}

// charmModeLabel is the display label of choice slot idx: CharmModeLabel of
// the slot's resolved body, falling back to the SVar name -- the same label
// the KModes decision's options carry, so the random-pick Note names the
// mode exactly as an answered ask would.
func charmModeLabel(choices []string, subs []*cards.SA, idx int) string {
	if idx < 0 || idx >= len(choices) {
		return ""
	}
	return CharmModeLabel(subs[idx], choices[idx])
}

// effVote records one Note per voting player. Two shapes:
//
//   - the fixed-list shape ("Will of the Planeswalkers", Expropriate):
//     Choices$ names an SVar per ballot option, each player votes for the
//     first (the deterministic stand-in), Notes record it, and the WINNING
//     option's SVar runs. A tie runs VoteTiedAbility$ when the SA carries
//     one (the path cycle's DBChaos), else the first tied option's SVar.
//     Before this the fixed-list shape resolved nothing at all, so a Path
//     of the Ghosthunter vote recorded its Notes and then did nothing --
//     the "chosen outcome" the brief expected to hit Planeswalk/
//     ChaosEnsues never ran. The tie branch takes its tally from Ctx.Votes
//     when a caller has answered one (the seam a real per-player ask fills,
//     and what lets the tie be pinned against a real compiled SA); absent,
//     the deterministic stand-in applies.
//   - the card-ballot shape (Council's Judgment): VoteCard$ is a permanent
//     filter, so the ballot is the battlefield permanents matching it
//     (matched from the spell's controller: "a nonland permanent YOU don't
//     control"), each Defined$ player votes, and every permanent with the
//     most votes or tied for most lands in the resolution's Remembered set
//     for VoteSubAbility$ (DBExile's ChangeZone Defined$ Remembered).
//
// Fixed and card ballots use the real per-voter ask path below; a host that
// cannot answer retains the R-9 first-option fallback. Both VoteCard$ and
// VoteSubAbility$ are genuinely read on the ballot path.
func effVote(h Host, c *Ctx, sa *cards.SA) {
	if ballot := strings.TrimSpace(sa.Params["VoteCard"]); ballot != "" {
		effCardVote(h, c, sa, ballot)
		return
	}
	// The PLAYER ballot (task votepb1): VotePlayer$ with no Choices$ list
	// names the ballot entries as players (Mob Verdict's `VotePlayer$ Other`).
	// Choices$ keeps its precedence -- Forge's VoteEffect reads Choices first,
	// then VoteCard$, then VotePlayer$ -- so this fires only when the vote
	// carries no fixed option list.
	if vp := strings.TrimSpace(sa.Params["VotePlayer"]); vp != "" && len(voteChoiceNames(sa)) == 0 {
		effPlayerVote(h, c, sa)
		return
	}
	choices := voteChoiceNames(sa)
	voters := definedPlayers(h, c, sa)
	// A live fixed-list ballot uses the same private, per-voter KChoose path as
	// VotePlayer$. Keep Ctx.Votes as the small direct seam used by unit tests;
	// real answers travel only through the decision's ResumeChoices.
	if c.Votes == nil {
		picks, complete := askFixedVote(h, c, sa, choices, voters)
		if !complete {
			return
		}
		for i, t := range voters {
			label := ""
			if i < len(picks) && picks[i].Obj > 0 && int(picks[i].Obj-1) < len(choices) {
				label = choices[picks[i].Obj-1]
			}
			if !strings.EqualFold(strings.TrimSpace(sa.Params["Secretly"]), "True") {
				h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
			}
		}
		counts := make([]int, len(choices))
		for _, p := range picks {
			if p.Obj > 0 && int(p.Obj-1) < len(choices) {
				counts[p.Obj-1]++
			}
		}
		if len(choices) > 0 && len(voters) > 0 {
			resolveVoteOutcomes(h, c, sa, choices, counts)
		}
		ballots := make([]VoteBallot, len(voters))
		for i, t := range voters {
			ballots[i] = VoteBallot{Player: t, Pick: int(picks[i].Obj) - 1}
		}
		emitVoteFinished(h, c, ballots, len(choices) > 0, strings.EqualFold(strings.TrimSpace(sa.Params["Secretly"]), "True"))
		return
	}
	// Ctx.Votes is the answered per-voter choice list (a real per-player
	// ask's result, or a test seam): one option index per voter, in voter
	// order. It is consumed and cleared at the top of the walk so a nested
	// Vote cannot inherit it (fx42), the same scoping every other asking
	// primitive uses. Absent, the deterministic stand-in applies: every
	// voter takes the first option.
	answered := c.Votes
	c.Votes = nil
	counts := make([]int, len(choices))
	// picks records each voter's answered option index (-1: an out-of-range
	// answer, i.e. a vote for nothing) so the canonical vote-finished Note's
	// same/diff split below reads the votes that were actually cast -- the
	// same data the tally uses, never a second answer source.
	picks := make([]int, len(voters))
	for i := range picks {
		picks[i] = -1
	}
	for i, t := range voters {
		choice := 0
		if answered != nil && i < len(answered) {
			choice = answered[i]
		}
		label := ""
		if choice >= 0 && choice < len(choices) {
			label = choices[choice]
			counts[choice]++
			picks[i] = choice
		}
		if !strings.EqualFold(strings.TrimSpace(sa.Params["Secretly"]), "True") {
			h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
		}
	}
	if len(choices) > 0 && len(voters) > 0 {
		resolveVoteOutcomes(h, c, sa, choices, counts)
	}
	// The canonical vote-finished carrier (trig:Vote, effects/vote.go):
	// emitted AFTER the winning outcome resolved -- the vote (outcome
	// included) finishes, then "whenever players finish voting" sees it. It
	// carries the RAW ballots, not a pre-split: the List$ referent sets are
	// relative to the TRIGGER SOURCE'S controller, which is only known on the
	// rules side (rules/trigger_referents' Vote case re-splits with
	// effects.VoteSplit against e.controllerOf(source)). It is emitted even
	// when there was no ballot and/or no voter, the same always-fire reading
	// the card-ballot shape takes; "whenever players finish voting" has no
	// intervening-if. ballotExisted is false for an empty Choices$ ballot,
	// which binds neither set.
	ballots := make([]VoteBallot, len(voters))
	for i, t := range voters {
		ballots[i] = VoteBallot{Player: t, Pick: picks[i]}
	}
	emitVoteFinished(h, c, ballots, len(choices) > 0, strings.EqualFold(strings.TrimSpace(sa.Params["Secretly"]), "True"))
}

// resolveVoteOutcomes executes the winning option normally. StoreVoteNum$ is
// the multi-outcome form: publish each option's tally as VoteNum in a private
// copy of the source SVar table, then resolve every option body so its numeric
// effects consume that option's count (including zero).
func resolveVoteOutcomes(h Host, c *Ctx, sa *cards.SA, choices []string, counts []int) {
	if strings.EqualFold(strings.TrimSpace(sa.Params["StoreVoteNum"]), "True") {
		for i, name := range choices {
			count := 0
			if i < len(counts) {
				count = counts[i]
			}
			svars := make(map[string]string, len(c.SVars)+1)
			for key, body := range c.SVars {
				svars[key] = body
			}
			// Forge reuses the SVar name VoteNum for each outcome body, binding
			// that option's tally while resolving it.
			svars["VoteNum"] = "Number$" + strconv.Itoa(count)
			cc := *c
			cc.SVars = svars
			if sub := cards.ResolveSVar(cc.SVars, name); sub != nil {
				Resolve(h, &cc, sub)
			}
		}
		return
	}
	best, tied := voteWinner(counts)
	name := choices[best]
	if tied {
		if alt := strings.TrimSpace(sa.Params["VoteTiedAbility"]); alt != "" {
			name = alt
		}
	}
	if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
		Resolve(h, c, sub)
	}
}

// askFixedVote poses one private KChoose per voter. The answer is encoded as
// ObjID(index+1), avoiding a second answer channel while keeping ResumeChoices
// decision-scoped. A host that cannot answer takes option zero (R-9).
func askFixedVote(h Host, c *Ctx, sa *cards.SA, choices []string, voters []state.PlayerID) ([]state.Target, bool) {
	picks := append([]state.Target(nil), c.VotePicks...)
	i := c.VoteTarget
	if c.VoteDone {
		if len(c.VoteAnswer) > 0 {
			picks = append(picks, c.VoteAnswer[0])
		} else {
			picks = append(picks, state.Target{})
		}
		c.VoteDone, c.VoteAnswer = false, nil
		i++
	}
	for ; i < len(voters); i++ {
		voter := voters[i]
		min := 1
		if strings.EqualFold(strings.TrimSpace(sa.Params["UpTo"]), "True") {
			min = 0
		}
		prompt := strings.TrimSpace(sa.Params["VoteMessage"])
		if prompt == "" {
			prompt = "Vote for an option"
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source,
			Min: min, Max: 1, ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i,
			ResumeChoices: append([]state.Target(nil), picks...), Prompt: prompt}
		for j, name := range choices {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "vote", Label: name, Obj: state.ObjID(j + 1)})
		}
		if len(d.Options) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if Ask(h, d) == AskAsked {
			return nil, false
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Obj: 1})
	}
	c.VotePicks, c.VoteTarget, c.VoteDone, c.VoteAnswer = nil, 0, false, nil
	return picks, true
}

// voteWinner returns the index of the highest count and whether that count is
// shared by more than one option. It is a separate function (rather than
// inline in effVote) so the tie branch is testable on its own: the current
// deterministic stand-in gives every vote to option 0, so a real tie cannot
// arise from a live resolution yet, and an untested branch would be dead code
// waiting to rot. The first highest index wins the tie, matching the
// oracle's "if X gets more votes" over "or the vote is tied" ordering.
func voteWinner(counts []int) (int, bool) {
	if len(counts) == 0 {
		return 0, false
	}
	best := 0
	for i, n := range counts {
		if n > counts[best] {
			best = i
		}
	}
	tied := 0
	for _, n := range counts {
		if n == counts[best] {
			tied++
		}
	}
	return best, tied > 1
}

// voteChoiceNames splits a Vote's Choices$ into its SVar names, trimmed and
// with empty entries dropped. Shared by both vote shapes so the option list
// the tally indexes is parsed one way.
func voteChoiceNames(sa *cards.SA) []string {
	raw := sa.Params["Choices"]
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// effCardVote is effVote's card-ballot half: the battlefield permanents
// VoteCard$ admits are the options, each voting player answers a private ask,
// and the most-voted -- every
// member of the tie -- is remembered for VoteSubAbility$, which runs once
// at the end (Council's Judgment's "exile each permanent with the most
// votes or tied for most votes").
func askCardVote(h Host, c *Ctx, sa *cards.SA, options []state.ObjID, voters []state.PlayerID) ([]state.ObjID, bool) {
	picks := append([]state.Target(nil), c.VotePicks...)
	i := c.VoteTarget
	if c.VoteDone {
		if len(c.VoteAnswer) > 0 {
			picks = append(picks, c.VoteAnswer[0])
		} else {
			picks = append(picks, state.Target{})
		}
		c.VoteDone, c.VoteAnswer = false, nil
		i++
	}
	for ; i < len(voters); i++ {
		voter := voters[i]
		min := 1
		if strings.EqualFold(strings.TrimSpace(sa.Params["UpTo"]), "True") {
			min = 0
		}
		prompt := strings.TrimSpace(sa.Params["VoteMessage"])
		if prompt == "" {
			prompt = "Vote for a permanent"
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source, Min: min, Max: 1,
			ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i, ResumeChoices: append([]state.Target(nil), picks...), Prompt: prompt}
		for j, id := range options {
			label := "permanent"
			var controller state.PlayerID
			if o := h.Game().Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
				// The subject's controller is public information (CR 400.2) and
				// the one fact the voter's policy needs to prefer a foreign
				// permanent over its own: Council's Judgment's ballot excludes
				// only the CASTER's permanents, so a 3+ seat ballot offers a
				// voter both its own and an opponent's permanents. Option.Player
				// already carries exactly this subject-controller convention for
				// player targets, so no new wire field is needed.
				controller = o.Controller
			}
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "vote_card", Label: label, Obj: id, Player: controller})
		}
		if len(d.Options) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if Ask(h, d) == AskAsked {
			return nil, false
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "card vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Obj: options[0]})
	}
	c.VotePicks, c.VoteTarget, c.VoteDone, c.VoteAnswer = nil, 0, false, nil
	out := make([]state.ObjID, len(picks))
	for j, p := range picks {
		out[j] = p.Obj
	}
	return out, true
}

func effCardVote(h Host, c *Ctx, sa *cards.SA, ballot string) {
	g := h.Game()
	var options []state.ObjID
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(i)) {
			if o := g.Obj(id); o != nil && c.MatchSpec(g, ballot, id, c.Controller) {
				options = append(options, id)
			}
		}
	}
	counts := map[state.ObjID]int{}
	max := 0
	voters := definedPlayers(h, c, sa)
	var picks []int
	if c.Votes != nil {
		// Direct seam retained for effects tests and replay-independent callers.
		picks = append([]int(nil), c.Votes...)
		c.Votes = nil
	} else {
		answered, complete := askCardVote(h, c, sa, options, voters)
		if !complete {
			return
		}
		picks = make([]int, len(answered))
		for i, id := range answered {
			picks[i] = -1
			if id != 0 {
				for j, option := range options {
					if option == id {
						picks[i] = j
						break
					}
				}
			}
		}
	}
	for i, t := range voters {
		label := "nothing"
		if i < len(picks) && picks[i] >= 0 && picks[i] < len(options) {
			id := options[picks[i]]
			if o := g.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			counts[id]++
			if counts[id] > max {
				max = counts[id]
			}
		}
		if !strings.EqualFold(strings.TrimSpace(sa.Params["Secretly"]), "True") {
			h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
		}
	}
	// The card ballot's per-subject tally, for the chained AmountFromVotes$
	// reader (task votepb1): one entry per ballot permanent, published behind
	// StoreVoteNum$ True -- the parameter Forge requires before it stores its
	// VoteNum<card> SVars. Forge's StoreVoteNum branch (a card ballot has no
	// Choices$) is authoritative on what the resolution REMEMBERS as well:
	// when the vote stores its per-subject tallies, the most-votes remember
	// path does not run at all, and the only remember is
	// RememberVotedObjects$'s `host.addRemembered(votes.keySet())` -- exactly
	// the objects that RECEIVED a vote, each once. Without StoreVoteNum$ the
	// most-votes append stands (Council's Judgment's "exile each permanent
	// with the most votes or tied for most votes", feeding VoteSubAbility$);
	// there a bare RememberVotedObjects$ beside it dedupes against the
	// most-votes set instead of duplicating it (no corpus carrier combines
	// the two without StoreVoteNum$, so the dedupe is the structural guard,
	// not a behaviour change any carrier can see).
	storeVoteNum := strings.EqualFold(strings.TrimSpace(sa.Params["StoreVoteNum"]), "True")
	rememberVoted := strings.EqualFold(strings.TrimSpace(sa.Params["RememberVotedObjects"]), "True")
	if storeVoteNum {
		publishVoteCounts(c, voteCountsForObjects(options, counts))
	} else if max > 0 {
		for _, id := range options {
			if counts[id] == max {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
	}
	if rememberVoted {
		// Exactly the objects that received a vote, each once: on the
		// non-StoreVoteNum path the most-votes append above may already hold a
		// voted object, so the voted set never duplicates it.
		mostVoted := map[state.ObjID]bool{}
		if !storeVoteNum && max > 0 {
			for _, id := range options {
				if counts[id] == max {
					mostVoted[id] = true
				}
			}
		}
		for _, id := range options {
			if counts[id] > 0 && !mostVoted[id] {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
	}
	// VoteSubAbility$ resolves AFTER the tally publish and the remember, so a
	// chained body sees Votes bound and the remembered set complete (fx42's
	// consumers read before their own re-entries).
	if sub := strings.TrimSpace(sa.Params["VoteSubAbility"]); sub != "" {
		if resolved := cards.ResolveSVar(c.SVars, sub); resolved != nil {
			Resolve(h, c, resolved)
		}
	}
	// The canonical vote-finished carrier (trig:Vote, effects/vote.go),
	// emitted after VoteSubAbility$ ran -- the same after-the-vote point the
	// fixed-list shape emits at. Like the fixed-list shape it carries the RAW
	// ballots and the rules side re-splits against the carrier controller.
	// The deterministic stand-in gives every voter the ballot's FIRST option,
	// so a controller who voted sees every other voter in the same set. A
	// vote with no ballot option at all (an empty battlefield) had nobody
	// vote for anything, so ballotExisted=false binds neither set -- the
	// trigger still fires and its same/diff bodies act on nobody, the same
	// always-fire reading the fixed-list shape takes.
	ballots := make([]VoteBallot, len(voters))
	for i, t := range voters {
		ballots[i] = VoteBallot{Player: t, Pick: picks[i]}
	}
	emitVoteFinished(h, c, ballots, len(options) > 0, strings.EqualFold(strings.TrimSpace(sa.Params["Secretly"]), "True"))
}

// effBecomeMonarch records the game-level designation as an event so a
// conditional trigger observes it identically in the live game and on replay.
//
// The event is a TRANSITION (CR 720.2: a player "becomes" the monarch only
// when the designation moves to them), so a resolution that names the
// reigning monarch as its target is a no-op: the designation does not move
// and no "whenever a player becomes the monarch" trigger may fire. This is
// load-bearing for events.MonarchChange's one reader, rules'
// becomeMonarchMatches -- it sees only the post-fold designation, so an
// unconditional emit here would queue trig:BecomeMonarch for a repeat
// BecomeMonarch (Custodi Lich resolving twice, two Peacekeeper Colossi, etc.).
// Suppressing at the source rather than inventing a previous-monarch field
// keeps events.Event's encoding untouched and replay-exact.
func effBecomeMonarch(h Host, c *Ctx, sa *cards.SA) {
	targets := Defined(h, c, sa)
	if len(targets) == 0 {
		return
	}
	players := definedPlayers(h, c, sa)
	if len(players) == 0 {
		return
	}
	p := players[0]
	if g := h.Game(); g != nil && g.IsMonarch(p) {
		return
	}
	h.Emit(events.Event{Kind: events.MonarchChange, Player: p})
}

// effRestartGame ends the game as a draw. Actually restarting (leaving
// exiled permanents in play under the restarting player's control, per the
// real card text) is out of M1's scope; ending the match honestly rather
// than hanging or silently no-op-ing is the closest correct degradation.
//
// Ruling T22-k (fix round 2): Amount: 1 is required, not cosmetic --
// rules/sba.go's checkGameOver is not the only GameOver emitter in this
// tree, and Amount is the shape discriminator events.Apply's GameOver case
// reads (0 = win, 1 = draw; Task 22 fix round 1). Left at its zero value,
// this event's own Amount reads as "Amount 0", a win -- and Player is also
// left at its zero value, which validates as seat 0 -- so despite this
// function's name, its own comment and its own Text all saying "draw", the
// event it actually emitted a win for seat 0. Every other RestartGame-style
// primitive in this file already carries no Player of its own, so seat 0
// winning was never a deliberate choice anywhere in this file; it was
// simply the one call site nobody had reason to re-examine once Amount
// became meaningful.
func effRestartGame(h Host, c *Ctx, sa *cards.SA) {
	// RestrictFromZone$/RestrictFromValid$ (Karn Liberated's [-14]): the
	// objects the restart would KEEP. Forge's RestartGame discards everything
	// in RestrictFromZone that matches RestrictFromValid and carries the rest
	// into the restarted game — Karn keeps exactly the non-Aura permanents
	// exiled with him (his ReturnFromExile sub-ability acts on that keep-set).
	// A full restart needs game-loop machinery this engine does not have (see
	// the draw degradation below), but the keep-set is real game state this
	// build can name, so the log records it instead of leaving both keys
	// silently inert.
	if zonesRaw := strings.TrimSpace(sa.Params["RestrictFromZone"]); zonesRaw != "" {
		if spec := strings.TrimSpace(sa.Params["RestrictFromValid"]); spec != "" {
			zones, all, valid := ParseZones(zonesRaw)
			g := h.Game()
			if !valid {
				all = true
			}
			if all {
				zones = []state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield,
					state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand}
			}
			var kept []string
			for _, z := range zones {
				// The shared stack is named once, under the first alive seat
				// (state/game.go Zone), so a stack card cannot appear in the
				// keep-set note multiple times on an N-seat table.
				for si, p := range g.AliveFrom(0) {
					if z == state.ZStack && si > 0 {
						continue
					}
					for _, id := range append([]state.ObjID(nil), g.Zone(z, p)...) {
						// RestrictFromValid$ names what the restart DISCARDS; the
						// complement inside the named zone is what it keeps.
						if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
							if o := g.Obj(id); o != nil && o.Face() != nil {
								kept = append(kept, o.Face().Name)
							}
						}
					}
				}
			}
			if len(kept) > 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "restart would keep in " + zonesRaw + ": " + strings.Join(kept, ", ")})
			}
		}
	}
	h.Emit(events.Event{Kind: events.GameOver, Amount: 1, Text: "game restarted: ended as a draw"})
}

// effMana implements "AB$ Mana": add Amount mana of Produced's colour(s) to
// the pool of each ManaRecipients player (the activating player unless
// Defined$ names another). Absorbed from Task 14's stopgap: the
// negative-Amount clamp is Ruling T14-f, kept verbatim for the same reason as
// DealDamage's -- events.Apply's ManaAdd case is a plain "+=", so an
// unclamped negative would drop the pool below zero instead of doing
// nothing.
//
// A resolution-time host now gets the same CR 106.6 colour choice as the
// activation path. A host without a decision channel keeps the R-9 fallback:
// Any becomes colourless and an unasked Combo list retains its old full-listed
// output. A dual-producing ability such as "Add {R}{R}" is walked one symbol
// at a time rather than split on whitespace, since Produced$ carries no spaces
// of its own.
//
// The one thing this primitive must NEVER do is walk a value it does not
// understand. A "Combo R G" reaches effMana from a path with no colour
// chooser (the activation path substitutes the chosen colour in first), and
// walking it one rune at a time turned "Combo R G" into five stray
// colourless plus a red and a green -- o, m, b are not mana symbols. An
// unrecognised Produced$ value therefore emits nothing and records a Note
// naming it, following this repo's fail-closed convention (an unknown token
// never invents a value).
// effReplaceMana rewrites one in-flight ManaAdd event for a ProduceMana
// replacement. The surrounding rules code supplies the amount and colour in
// Ctx, then logs the rewritten ManaAdd; this effect itself has no game-state
// mutation to emit. ReplaceAmount multiplies the whole production.
// ReplaceType/ReplaceColor preserve its amount and replace only its colour;
// ReplaceMana is Forge's "one mana instead of any other type and amount"
// form (Damping Sphere, Contamination), so it sets the amount to exactly one
// as well as replacing the colour. For a choice-valued replacement
// (Any/Chosen), rules parks the ManaAdd and supplies the player's W/U/B/R/G
// answer in Ctx.ManaChoice; without a valid answer this pure effect fails
// closed rather than inventing colourless mana.
func effReplaceMana(_ Host, c *Ctx, sa *cards.SA) {
	if c == nil {
		return
	}
	if only := strings.TrimSpace(sa.Params["ReplaceOnly"]); only != "" && only != c.ManaType {
		return
	}
	if n := Num(nil, c, sa, "ReplaceAmount", 1); n > 0 {
		c.ManaAmount *= n
	}
	kind := strings.TrimSpace(sa.Params["ReplaceMana"])
	if kind != "" {
		c.ManaAmount = 1
	}
	if kind == "" {
		kind = strings.TrimSpace(sa.Params["ReplaceType"])
	}
	if kind == "" {
		kind = strings.TrimSpace(sa.Params["ReplaceColor"])
	}
	if kind == "" {
		return
	}
	switch strings.ToLower(kind) {
	case "white":
		kind = "W"
	case "blue":
		kind = "U"
	case "black":
		kind = "B"
	case "red":
		kind = "R"
	case "green":
		kind = "G"
	case "any", "chosen":
		kind = c.ManaChoice
	}
	if len(kind) == 1 && strings.ContainsRune(ManaSymbols, rune(kind[0])) {
		c.ManaType = kind
	}
}

var manaRuneNormalizer = strings.NewReplacer("{", "", "}", "", " ", "")

var manaChoiceColours = []string{"W", "U", "B", "R", "G"}

func validManaChoice(s string) bool {
	return len(s) == 1 && strings.ContainsRune("WUBRG", rune(s[0]))
}

// substituteManaChosen replaces the source's as-enters colour in a raw
// Produced$ value.  This is deliberately local to effects: the activation
// path has its own equivalent in rules, while a triggered or nested Mana
// effect reaches effMana without passing through that activation code.
func substituteManaChosen(produced, chosen string) string {
	if !validManaChoice(chosen) {
		return produced
	}
	if produced == "ComboChosen" {
		return "Combo " + chosen
	}
	parts := strings.Fields(produced)
	for i, part := range parts {
		if part == "Chosen" || part == "ChosenColor" {
			parts[i] = chosen
		}
	}
	return strings.Join(parts, " ")
}

// askManaChoice gives a resolution-time Mana effect the same colour-choice
// boundary as an activated mana ability. A Combo's Amount$ is an allocation:
// each selected option is one unit, allowing {U}{R} from Combo Any Amount 2.
// A false Ask is the R-9 no-host path: Any remains the historical colourless
// fallback and a raw Combo list remains the historical full-listed-colours
// fallback. A real host gets a KChoose and rules carries the answer back in
// Ctx.ManaChoice or Ctx.ManaChoices.
func askManaChoice(h Host, c *Ctx, sa *cards.SA, produced string) (string, bool) {
	var colours []string
	switch produced {
	case "Any", "Combo Any":
		colours = manaChoiceColours
	default:
		if parsed, ok := ComboColours(produced); ok && len(parsed) > 1 {
			colours = parsed
		}
	}
	if len(colours) <= 1 {
		return produced, false
	}
	amount := Num(h, c, sa, "Amount", 1)
	allocation := strings.HasPrefix(produced, "Combo ") && amount > 1
	if amount <= 0 {
		return produced, false
	}
	min, max := 1, 1
	if allocation {
		min, max = int(amount), int(amount)
	}
	chooser := c.Controller
	if recipients := ManaRecipients(h, c, sa); len(recipients) == 1 {
		chooser = recipients[0]
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: min, Max: max,
		Source: c.Source, ResumeKind: "mana_color", ResumeSA: sa,
		Prompt: "Choose a color for the mana"}
	for unit := 0; unit < max; unit++ {
		for _, colour := range colours {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana",
				Label: "Add " + colour, ManaSymbol: colour, Obj: c.Source, Player: chooser})
		}
	}
	if Ask(h, d) == AskAsked {
		return produced, true
	}
	return produced, false
}

// ManaProducerTag encodes producer provenance in one exclusive unit tag.
// The Artifact bit composes with Treasure/Cave/Desert rather than replacing
// their historical type: a Treasure Artifact emits ArtifactTreasure, while
// Sol Ring emits Artifact. All mana producers use this same helper (effMana,
// effManaReflected, and cumulative-upkeep AddMana). Snow/typed overlap is
// unmeasured at the corpus pin and retains typed precedence.
func ManaProducerTag(h Host, source state.ObjID) (tag string, snow bool) {
	o := h.Game().Obj(source)
	if o == nil || o.Face() == nil {
		return "", false
	}
	artifact := false
	for _, t := range o.Face().Types {
		if t == "Artifact" {
			artifact = true
			break
		}
	}
	for _, tagWord := range state.TypedManaTags {
		if tagWord == "Artifact" {
			continue
		}
		for _, t := range o.Face().Types {
			if t == tagWord {
				if artifact {
					return "Artifact" + tagWord, false
				}
				return tagWord, false
			}
		}
	}
	if artifact {
		return "Artifact", false
	}
	for _, t := range o.Face().Types {
		if t == "Snow" {
			return "", true
		}
	}
	return "", false
}

func effMana(h Host, c *Ctx, sa *cards.SA) {
	produced := strings.TrimSpace(sa.Params["Produced"])
	// A resumed Combo allocation supplies one concrete symbol per unit.
	// Consume it before walking the SA so the same choice is not posed again;
	// its units carry Amount 1 below rather than being multiplied again.
	allocation := len(c.ManaChoices) > 0
	if allocation {
		produced = strings.Join(c.ManaChoices, "")
		c.ManaChoices = nil
		// A resumed colour ask supplies one concrete symbol. Consume the answer
		// before walking the SA so the same choice is not posed again on re-entry.
	} else if validManaChoice(c.ManaChoice) {
		produced = c.ManaChoice
		c.ManaChoice = ""
	} else if o := h.Game().Obj(c.Source); o != nil {
		// Chosen is normally stamped by an as-enters ChooseColor event.  A
		// triggered/nested Mana effect does not pass through rules' activation
		// substitution, so read that same event-backed value here.
		produced = substituteManaChosen(produced, o.ChosenColor)
	}
	if produced == "Any" || produced == "Combo Any" {
		if _, asked := askManaChoice(h, c, sa, produced); asked {
			return
		}
		if produced == "Any" || produced == "Combo Any" {
			// R-9: an effects host without a decision channel retains the
			// historical deterministic colourless result.
			produced = "C"
		}
	} else if _, ok := ComboColours(produced); ok {
		if _, asked := askManaChoice(h, c, sa, produced); asked {
			return
		}
	}
	if produced == "" {
		produced = "C"
	}
	// A Combo head is a colour choice, not a request to add every named
	// colour.  A host that could not ask has already taken the R-9 fallback;
	// a resumed answer has been rewritten to one plain symbol above.  Leave a
	// raw Combo list intact here only for the no-host fallback below.
	//
	// Special LastNotedType (Jeweled Amulet: "Add one mana of CARDNAME's
	// last noted type"): the production resolves to the colour the source's
	// last RememberCostMana$ activation paid with (events.Choose's
	// "noted-mana" marker folded into state.Object.LastNotedMana). With no
	// note yet the executor fails closed — the loud Note and no mana the
	// unhandled-Produced arm emits — which for the Amulet is unreachable
	// (its production cost removes the charge counter the noted activation
	// created).
	if strings.EqualFold(produced, "Special LastNotedType") {
		noted := ""
		if o := h.Game().Obj(c.Source); o != nil {
			noted = strings.TrimSpace(o.LastNotedMana)
		}
		if noted == "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unhandled Produced$ Special LastNotedType: no mana noted yet"})
			return
		}
		produced = noted
	}
	// Special EachColorAmong_Valid <spec> (Faeburrow Elder, Tarnation Vista's
	// second ability: "For each color among [matching] permanents you control,
	// add one mana of that color"): a deterministic BATCH, not a choice. The
	// matching permanents' colours (ColorsOf: mana cost or Colors: line,
	// Devoid-aware) are unioned and rendered in fixed WUBRG order, and the
	// tail's per-rune loop adds one unit per distinct colour. The spec is
	// evaluated over the battlefield exactly as ManaReflectedCandidates
	// evaluates its Valid$: MatchesSpecFrom with the resolving source as
	// Self and its controller as You. An empty colour set is a legitimate
	// deterministic no-op ("for each color" over none adds nothing) -- no
	// Note, no mana, like ChangeNum$ 0 Dig. Special EachColorAmong_ExiledWith
	// (Sunbird Effigy: the colours among the cards exiled with the source)
	// resolves the same way over the exiledWithSet. DoubleManaInPool is
	// handled just below. Every OTHER Special selector (EnchantedManaCost,
	// EachColoredManaSymbol_Milled) still falls through to the rune gate's
	// loud Note below.
	// Special DoubleManaInPool (Doubling Cube: "Double the amount of each
	// type of unspent mana you have"): the activator's pool AFTER the cost
	// was paid (a mana ability resolves right after its payment), rendered
	// as one symbol per unit in fixed WUBRGC order, so the per-rune tail
	// adds exactly one more of each unit. An empty pool is a deterministic
	// no-op. The doubled units are ordinary mana: a spend restriction,
	// snow/typed producer provenance or persistence on the original units is
	// a property of how THOSE units were produced, not of their type.
	if strings.EqualFold(produced, "Special DoubleManaInPool") {
		g := h.Game()
		if int(c.Controller) >= len(g.Players) {
			return
		}
		pool := g.Players[c.Controller].Pool
		var b strings.Builder
		for i := 0; i < len(ManaSymbols); i++ {
			for n := pool[state.ManaIndex(ManaSymbols[i])]; n > 0; n-- {
				b.WriteByte(ManaSymbols[i])
			}
		}
		if b.Len() == 0 {
			return
		}
		produced = b.String()
	}
	if strings.EqualFold(produced, "Special EachColorAmong_ExiledWith") {
		syms := eachColorAmongExiledWith(h, c)
		if syms == "" {
			// An empty set is a deterministic no-op, exactly like the
			// EachColorAmong_Valid empty set: "for each color among none"
			// adds nothing, and no card is promised mana it cannot get.
			return
		}
		produced = syms
	}
	if sel, ok := strings.CutPrefix(produced, "Special EachColorAmong_Valid "); ok {
		syms := eachColorAmongValid(h, c, strings.TrimSpace(sel))
		if syms == "" {
			return
		}
		produced = syms
	}
	produced = strings.TrimSpace(strings.TrimPrefix(produced, "Combo "))
	// Strip braces and spaces, then validate every remaining rune before any
	// of them reaches the pool: ComboChosen/ChosenColor/Special ... values
	// that do not name plain mana symbols fail closed instead of splitting
	// into garbage.
	runes := manaRuneNormalizer.Replace(produced)
	for _, r := range runes {
		if !strings.ContainsRune(ManaSymbols, r) {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unhandled Produced$ " + produced})
			return
		}
	}
	amt := Num(h, c, sa, "Amount", 1)
	if allocation {
		amt = 1
	}
	if amt < 0 {
		amt = 0
	}
	// RestrictValid$ (Master of Dark Rites' "Spend this mana only to cast
	// Vampire, Cleric, and/or Demon spells", Eldrazi Temple, Cavern of
	// Souls, Giada, Shrine of the Forsaken Gods): the produced mana carries
	// its spend restriction on the ManaAdd event itself, so the pool retains
	// the provenance per colour slot and the payment path
	// (manaAvailableFor / restrictValidMatches) can admit it only to
	// matching payments — the same event-level provenance the ManaReflected
	// family and the Tazri batch already ride. A class the payment path
	// cannot evaluate (anything but Spell./Activated.) is still retained --
	// it matches no payment, so the mana is never spendable, the
	// fail-closed direction.
	restriction := strings.TrimSpace(sa.Params["RestrictValid"])
	// AddsNoCounter$ (Cavern of Souls' "that spell can't be countered",
	// Boseiju, Delighted Halfling — 3 corpus files): the produced mana carries
	// its can't-be-countered provenance on the same ManaAdd restriction batch
	// the payment path already reads, so a cast that spends one of these units
	// is marked can't-be-countered at payment time (rules/stack.go captures
	// the consumption, rules/cast.go folds state.FlagNoCounter into the
	// pay-time CastInfo). "True" is the plain flag; Forge's conditional
	// "!Permanent" (Boseiju's instant-or-sorcery mana) is recognised as the
	// NotPermanent condition, evaluated against the paying spell's face. Any
	// other value is a loud Note and NO protection — an unrecognised condition
	// must not silently promise something the engine cannot model.
	noCounter := ""
	switch strings.TrimSpace(sa.Params["AddsNoCounter"]) {
	case "":
	case "True":
		noCounter = "True"
	case "!Permanent":
		noCounter = "NotPermanent"
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled AddsNoCounter$ " + strings.TrimSpace(sa.Params["AddsNoCounter"]) + "; the mana is ordinary"})
	}
	// PersistentMana$ True (Rousing Refrain, Savage Ventmaw, Klauth, Kessig
	// Naturalist: 23 corpus files / 24 raw lines, every occurrence the
	// literal True): the mana does not empty as steps and phases end (CR
	// 500.4 with the card's exception) until the turn ends. The marker rides
	// the ManaAdd event's Text suffix (events.ManaPersistentText) so
	// events.Apply can keep the units through ManaClear and expire them at
	// TurnChange; it composes with the restriction encoding (Klauth pairs it
	// with RestrictValid$). Any other value is a loud Note and ordinary
	// mana.
	persistent := false
	switch strings.TrimSpace(sa.Params["PersistentMana"]) {
	case "":
	case "True":
		persistent = true
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled PersistentMana$ " + strings.TrimSpace(sa.Params["PersistentMana"]) + "; the mana is ordinary"})
	}
	// PersistentUntilEndOfCombat$ True is the CR 702.189a Firebending
	// exception ("Until end of combat, you don't lose this mana as steps and
	// phases end"): the units are persistent (they survive the step
	// boundaries within the combat phase) AND carry the combat tally that
	// events.Apply demotes as the end-of-combat step is left. It is implied
	// by, and composes with, the printed keyword expansion (cards/
	// kw_firebending.go); any other value is a loud Note and ordinary mana.
	combat := false
	switch strings.TrimSpace(sa.Params["PersistentUntilEndOfCombat"]) {
	case "":
	case "True":
		combat, persistent = true, true
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled PersistentUntilEndOfCombat$ " + strings.TrimSpace(sa.Params["PersistentUntilEndOfCombat"]) + "; the mana is ordinary"})
	}
	// CR 107.4h: mana produced by a SNOW permanent is snow mana. A snow unit
	// is tagged in the pool event itself — Counter "S<colour>" — so the pool
	// slot and the parallel snow tally move through one event and a replay
	// derives both identically. The {S} pips a cost may carry are paid only
	// from that tally (rules/mana.go's resolveMana).
	//
	// Task castfilter2: mana produced by a Treasure/Cave/Desert permanent is
	// likewise tagged — Counter "<Tag><colour>" — into Player.TypedMana so
	// the filtered Count$CastTotalManaSpent Treasure/Cave/Desert heads can
	// read the per-unit producer provenance (Marut, Bat Colony, Cataclysmic
	// Prospecting). Artifact provenance composes with those tags in the
	// per-unit counter; it never replaces the Treasure/Cave/Desert type or
	// contributes a second pool unit. An untyped Artifact uses ArtifactC.
	tag, snow := ManaProducerTag(h, c.Source)
	// TriggersWhenSpent$ is retained alongside any spend restriction: the
	// ManaRestriction event encoding carries both the restriction and source
	// provenance, so spendability and the later trigger attribution compose.
	// The spend path dispatches the named rider after the payment completes.
	triggersWhenSpent := strings.TrimSpace(sa.Params["TriggersWhenSpent"])
	// AddsCounters$ (Opal Palace's "If you spend this mana to cast your
	// commander, it enters with ... counters", Biophagus, Animal Attendant,
	// Guildmages' Forum: 4 corpus files) is retained the same way: rules'
	// entry-counter plan re-reads the rider from the producing source's face
	// at the cast spell's battlefield entry, and the source rides the
	// batch's provenance text -- so a rider with no RestrictValid$ still
	// needs a source-bearing batch, which the provenanceOnly gate emits.
	// The rider string itself is not decoded here (the plan owns the
	// grammar); an absent or empty value is not a rider and produces ordinary
	// mana, the fail-closed direction.
	addsCounters := strings.TrimSpace(sa.Params["AddsCounters"])
	provenanceOnly := (triggersWhenSpent != "" || addsCounters != "") && restriction == "" && noCounter == ""
	for _, p := range ManaRecipients(h, c, sa) {
		var emitted [256]bool
		for _, r := range runes {
			// An allocation records one rune per selected unit. Coalesce equal
			// selections into the same ManaAdd batch (four selected W units are
			// one Amount:4 W batch), retaining provenance/restriction semantics
			// while a split U/R still emits one batch for each colour.
			if allocation && emitted[byte(r)] {
				continue
			}
			emitted[byte(r)] = true
			unitAmount := amt
			if allocation {
				unitAmount = 0
				for _, selected := range runes {
					if selected == r {
						unitAmount++
					}
				}
			}
			counter := string(r)
			switch {
			case tag != "":
				counter = tag + counter
			case snow:
				counter = "S" + counter
			}
			ev := events.Event{Kind: events.ManaAdd, Player: p,
				Counter: counter, Amount: unitAmount}
			if noCounter != "" {
				ev.Text = events.ManaRestrictionTextNC(restriction, c.Source, noCounter)
			} else if restriction != "" {
				ev.Text = events.ManaRestrictionText(restriction, c.Source)
			} else if provenanceOnly {
				ev.Text = events.ManaRestrictionText("", c.Source)
			}
			// AddsCounters$ rides the SAME provenance encoding as the restriction
			// (the " ac=" segment ManaAddsCountersText appends), so a replay
			// rebuilds the batch with the producing ABILITY's rider snapshot.
			// Composes with a restriction and with AddsNoCounter$; empty for
			// every non-rider ability, keeping those events byte-identical.
			ev.Text = events.ManaAddsCountersText(ev.Text, addsCounters)
			if combat {
				ev.Text = events.ManaCombatPersistentText(ev.Text)
			} else if persistent {
				ev.Text = events.ManaPersistentText(ev.Text)
			}
			h.Emit(ev)
		}
	}
}

// eachColorAmongValid resolves a Produced$ Special EachColorAmong_Valid <spec>
// selector: the union of the matching battlefield permanents' colours, in the
// controller-relative battlefield scan order (AliveFrom(0) x zone order, the
// same scan ManaReflectedCandidates uses), rendered in fixed WUBRG order via
// ColorMask's table. Colourless contributes nothing ("each color" never
// includes colourless). The spec is evaluated with the resolving source as
// Self and its controller as You, so a bare Permanent.YouCtrl always matches
// the resolving permanent itself while it is on the battlefield. No map
// iteration reaches the result: the union is a bitmask and the output order
// is the fixed WUBRG table.
func eachColorAmongValid(h Host, c *Ctx, spec string) string {
	g := h.Game()
	var mask ColorMask
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if c.MatchSpec(g, spec, id, c.Controller) {
				mask |= ColorMaskOf(g.Obj(id))
			}
		}
	}
	return mask.String()
}

// eachColorAmongExiledWith resolves a Produced$ Special
// EachColorAmong_ExiledWith selector (Sunbird Effigy's "for each color among
// the exiled cards used to craft it"): the union of the colours among the
// cards exiled WITH the resolving source (state.Object.ExiledWith naming it,
// the same exiledWithSet the Defined$ ExiledWith referent reads), rendered in
// fixed WUBRG order. An empty set returns "" -- a deterministic no-op, the
// eachColorAmongValid convention.
func eachColorAmongExiledWith(h Host, c *Ctx) string {
	g := h.Game()
	var mask ColorMask
	for _, t := range exiledWithSet(g, c) {
		mask |= ColorMaskOf(g.Obj(t.Obj))
	}
	return mask.String()
}

// ManaRecipients is the player or players a Mana SA adds its mana for. Forge's
// ManaEffect adds to getDefinedPlayersOrTargeted: with no Defined$ that is the
// activating player; with Defined$ it is each player the selector names, so
// Vernal Bloom's Defined$ TriggeredCardController gives the extra {G} to the
// tapped Forest's controller rather than to the enchantment's. An object
// selector names that object's controller (PlayerOf). A Defined$ that resolves
// to nobody adds nothing, as in Forge (SpellAbilityEffect.getDefinedPlayers has
// no activator fallback): Valleymaker's Defined$ ChosenPlayer must not hand the
// mana to its controller when no player was chosen.
func ManaRecipients(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	if strings.TrimSpace(sa.Params["Defined"]) == "" {
		return []state.PlayerID{c.Controller}
	}
	// definedPlayers applies Forge's getDefinedPlayers rule: a remembered CARD
	// contributes a seat only for the RememberedController/Owner spellings,
	// never for the plain Remembered family (a RepeatEach loop's subject).
	return definedPlayers(h, c, sa)
}
