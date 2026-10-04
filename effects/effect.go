package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
func effEffect(h Host, c *Ctx, sa *cards.SA) {
	ep := EffectOf(sa)
	noteUnreadParams(h, c, "Effect", ep.Unread)
	rawDur := ep.Duration
	dur := rawDur
	if dur == "" {
		dur = "Permanent"
	}
	what := strings.TrimSpace(ep.StaticAbilities + " " + ep.Triggers)
	// Name$ is the effect's own display name (Sephiroth's emblem, Wrenn and
	// Six's): the log names the effect after it wherever this function would
	// otherwise print a bare mode list, and the registrations below carry it
	// into the continuous-effect registry so Stackable$ can dedup by it.
	effectName := ep.Name
	// Stackable$ False (Wrenn and Six's emblem): the effect does not stack.
	// Forge's EffectEffect.createEffect skips creating a second effect when an
	// un-stackable one already exists. Forge's default is STACKABLE — the
	// corpus carries Stackable$ only as "False" (38 raw lines, no "True"), so
	// the dedup gate fires ONLY on an explicit "False": an absent key keeps
	// the stacking behaviour (en-Kor's "en-Kor Redirection" redirection
	// stacking is the point of the card). The dedup ask goes through
	// Host.ContinuousNamed so the registry, not this resolution, decides
	// whether the same named effect from this controller is active.
	if ep.NotStackable && effectName != "" && h.ContinuousNamed(c.Controller, effectName) {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "effect not stacked (" + effectName + ")"})
		return
	}
	// The two move-driven lifetimes: ForgetOnMoved$ drops a remembered card
	// from the registered effect's set when it moves to the named zone;
	// ExileOnMoved$ ENDS the effect on such a move (Vines of Vastwood's
	// blinked target). Both ride the registrations below.
	forgetOn := ep.ForgetOnMoved
	exileOn := ep.ExileOnMoved
	// ForgetCounter$ <kind> (task vow1): a remembered card whose count of
	// that kind reaches zero after a counter-removal leaves the registered
	// effect's Remembered set. Both this and ForgetOnMoved$ ride every
	// registration below.
	forgetCounter := ep.ForgetCounter
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
	forgetOnCast := ep.ForgetOnCast
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
		forgetOnCast = effectSelfExileOnCastSpec(h, c, ep.Triggers)
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
	if v := ep.ImprintOnHost; v != "" && !ep.ImprintOnHostTrue {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unmodelled Effect ImprintOnHost$ " + v})
	}
	imprintOnHost := ep.ImprintOnHostTrue
	// ForgetOnPhasedIn$ True (CR 702.25, the "phase out until CARDNAME leaves
	// the battlefield" family: Out of Time, Oubliette, The Moment). The
	// Effect's comeback trigger is a printed ChangesZone trigger
	// (Origin$ Battlefield | Destination$ Any | ValidCard$ Card.IsImprinted,
	// Static$ True) whose Duration$ Permanent lifetime has no turn ceiling:
	// it lives until the Effect's own DBExileSelf body runs. The turn-ceiling
	// registration below cannot express that, so a Permanent lifetime is
	// allowed only for this marker and only for a ChangesZone body (the
	// comeback idiom); every other Effect trigger lifetime stays loud.
	forgetOnPhasedIn := ep.ForgetOnPhasedIn
	// RememberLKI$ (Quicksilver Elemental's "RememberLKI$ Targeted"): the
	// effect remembers the TARGETED cards — "Targeted" (and Forge's bare
	// "True", which is Targeted in the corpus's spelling) is exactly the
	// set effectRemembered's default already captures, so the registered
	// grants below see it either way; the read pins the flag's presence so
	// the grant's Remembered does not depend on the RememberObjects$
	// default. Any other value (an LKI grammar this build does not model —
	// the LKI persistence a vanished card would need) is a loud Note.
	if rl := ep.RememberLKI; rl != "" {
		switch effEffectCodes.Code(string(rl)) {
		case effEffectTargeted:
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
	if v := ep.SetChosenNumber; v != "" {
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
	ownerSel := ep.EffectOwner
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
	for name := range strings.FieldsSeq(ep.Triggers) {
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
		tl := readEffectTriggerLine(&tr)
		exec := tl.Execute
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
		oneOff := tl.OneOff &&
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
		optionalSpec := tl.OptionalDecider
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
			if tl.Static {
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
		if v := tl.ThisTurn; v != "" && !strings.EqualFold(v, "True") {
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
		switch tr.ModeKind() {
		case cards.TriggerSpellCast, cards.TriggerChangesZone:
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
		case cards.TriggerPhase:
			// A phase promise fires at the FIRST listed step still ahead
			// (state.EarliestAfter), exactly like the DelayedTrigger SA's
			// multi-step Phase$ reading; ValidPlayer$ rides |VP= so the
			// phase scan gates on it (Necropotence's "YOUR next end step").
			if !resolveOwners() {
				continue
			}
			set, unknown := state.ParsePhases(tl.Phase)
			if len(unknown) > 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "Effect trigger " + name + " at unrecognized phase " + tl.Phase})
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
			text := tl.Phase + expiry + odSuffix
			if vp := tl.ValidPlayer; vp != "" {
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
	for _, name := range strings.FieldsFunc(ep.ReplacementEffects, func(r rune) bool {
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
			if event == "DamageDone" && ep.Duration == "" {
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
	for _, name := range strings.FieldsFunc(ep.StaticAbilities, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		mode, params := parseStaticLine(c.SVars, name)
		switch cards.StaticModeOf(mode) {
		case cards.StaticContinuous:
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
				grant.Chosen, grant.ChosenBound = effectChosenSnapshot(h, c, grant.Affects)
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
				// free-play params plus the ValidAfterStack$ qualifier
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
				grant.Chosen, grant.ChosenBound = effectChosenSnapshot(h, c, grant.Affects)
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
				// combat.staticGoaders -- the reader BOTH routes share -- matches its
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
		case cards.StaticCantTarget, cards.StaticCantRegenerate, cards.StaticCantPreventDamage, cards.StaticCantAttack, cards.StaticCantSacrifice, cards.StaticCantExile, cards.StaticCantPutCounter, cards.StaticCantBlockBy, cards.StaticCanAttackDefender, cards.StaticUnspentMana, cards.StaticCantBlockUnless, cards.StaticCantAttackUnless, cards.StaticMustBlock, cards.StaticNumLoyaltyAct, cards.StaticCantGainLife, cards.StaticCastWithFlash:
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
			if mode == "CastWithFlash" && !LoyaltyFlashParamsReadable(params) {
				// Only the loyalty-timing grant registers (Jace's Machinations'
				// "you may activate loyalty abilities of Jace planeswalkers you
				// control ... any time you could cast an instant"); rules'
				// loyaltyAtInstantSpeed reads it beside the printed S: route.
				// A spell-flash grant keeps the unimplemented Note it always had.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			if mode == "CantGainLife" && !CantGainLifeParamsReadable(params) {
				// An Effect-delivered CantGainLife body (the CR 614.1 lock:
				// Screaming Nemesis, Stigma Lasher, Welcome the Darkness,
				// Skullcrack, Atarka's Command, Call In a Professional, Roiling
				// Vortex) registers as a continuous restriction rules'
				// lifeGainForbidden registered walk reads beside the printed S:
				// route. A body carrying a scoping term this build does not
				// evaluate must not register blanket -- it reports unimplemented
				// instead (the permissive direction for a restriction; the
				// whitelist is CantGainLifeParamsReadable, shared with the
				// rules-side read so the two paths cannot disagree).
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
				registered = true
				break
			}
			ceUntilEOT := effectUntilEOT(h, c.Source, rawDur)
			if absentDurationMeansThisTurn(mode) && ep.Duration == "" {
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
			if mode == "CantAttack" || mode == "CantSacrifice" || mode == "CantBlockBy" || mode == "CantGainLife" {
				// The player half of the remembered capture: Call for Aid's
				// RememberObjects$ TargetedPlayer must reach the registered
				// CantAttack, whose Target$ Player.IsRemembered ("you can't
				// attack that player") resolves against this set at
				// consultation time (rules/layers.go
				// restrictionPlayerSpecMatches) — effectRemembered records
				// objects only, so without this the remembered player would
				// silently vanish. The Motherlode, Excavator is why CantBlockBy
				// joins: its RememberObjects$ TargetedController captures the
				// defending player, and its ValidBlocker$
				// Creature.RememberedPlayerCtrl clause resolves against the
				// same set at the block consultation (rules/statics.go
				// combat.BlockRestricted, via SpecContext.RememberedPlayers).
				//
				// CantGainLife joins for the damage-trigger carriers: Screaming
				// Nemesis's RememberObjects$ Player.IsRemembered (the damaged
				// player, captured from the live Ctx.Remembered by the DealDamage
				// RememberDamaged$ read) and its ValidPlayer$ Player.IsRemembered
				// resolve against the same set at consultation time
				// (rules/replacement_life.go lifeGainForbidden).
				ce.RememberedPlayers = effectRememberedPlayers(h, c, sa)
			}
			if mode == "CantGainLife" && strings.EqualFold(strings.TrimSpace(rawDur), "Permanent") {
				// CR 611.2a: an explicit Duration$ Permanent restriction lasts
				// until end of game REGARDLESS of its source ("for the rest of
				// the game" -- Screaming Nemesis, Stigma Lasher, Welcome the
				// Darkness). effectUntilEOT alone cannot express this: for a
				// battlefield creature source it gives the source-leaves
				// lifetime (the lock would die with the Nemesis), and for a
				// one-shot spell source (Welcome the Darkness's instant) it
				// returns UntilEOT outright, expiring the rest-of-game lock at
				// the turn's cleanup. The Permanent flag is the one
				// registration state continuousLive reads that outlives both,
				// the same read the Continuous case makes for Finale of
				// Revelation; guarded to this mode so no sibling's lifetime
				// changes with it.
				ce.Permanent = true
				ce.UntilEOT = false
			}
			effectContinuous(h, ce)
			registered = true
		case cards.StaticCombatDamageToughness:
			// This assignment static is consumed by rules' existing combat
			// assignment collector. Keep its body parameters and SVar table on
			// the registration so the ordinary static applicability gates run
			// unchanged, with the Effect's remembered targets bound by the
			// assignment view.
			untilEOT := effectUntilEOT(h, c.Source, rawDur)
			if ep.Duration == "" && absentDurationMeansThisTurn(mode) {
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
		case cards.StaticReduceCost, cards.StaticRaiseCost, cards.StaticSetCost, cards.StaticAlternativeCost, cards.StaticManaConvert:
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
		case cards.StaticMustAttack:
			// An Effect-delivered per-player attack REQUIREMENT (Forge's
			// MustAttack$ "that creature attacks that player this combat if
			// able"): Territory Hellkite's DBPump, and the four plain-
			// SubAbility siblings Knight Rampager, Ursine Monstrosity, Raving
			// Dead and Ruhan of the Fomori. It registers like the restriction
			// modes above (combat.AttackRequirements collector reads it from
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
			// combat.requirementDefender). effectRemembered records objects only, so
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

type effEffectCode uint16

const (
	effEffectTargeted effEffectCode = iota + 1
)

var effEffectCodes = state.NewStrCodes(
	state.StrEntry[effEffectCode]{Key: "Targeted", Val: effEffectTargeted},
	state.StrEntry[effEffectCode]{Key: "True", Val: effEffectTargeted},
)
