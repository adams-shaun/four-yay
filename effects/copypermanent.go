package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("CopyPermanent", effCopyPermanent) }

// dedupeTargets removes repeated entries from a resolved target list by
// OBJECT identity (state.ObjID), preserving first-seen order. It is the
// shared rule for the two lists effCopyPermanent builds that can carry the
// same object twice: the mint list (targets/destinations) and the
// TokenRemembered$ memory. A repeated entry is always the trigger capture +
// RememberChanged$ re-remember duplication (Hofri Ghostforge's
// [bearer, bearer]), never a copy instruction -- NumCopies$ is the spelling
// for two copies of one permanent. Two DIFFERENT objects are both kept
// (Myrkul's [self, reanimated]). Player entries (IsPlayer) are keyed by
// player id and never collapse with an object entry. Nil/empty in, nil out,
// so the common no-spec path allocates nothing.
func dedupeTargets(ts []state.Target) []state.Target {
	if len(ts) < 2 {
		return ts
	}
	seen := make(map[state.Target]bool, len(ts))
	out := make([]state.Target, 0, len(ts))
	for _, t := range ts {
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// effCopyPermanent implements DB$ CopyPermanent (task copyp1): create a token
// that is a copy of the object each resolved source names. The mint is one
// events.CopyToken + one MoveZone per copy -- the MyriadCopy precedent, whose
// two-event shape keeps the token's battlefield entry a
// ChangesZone-matchable event every "a creature enters" trigger observes.
//
// The copy carries the copied card's PRINTED characteristics (Card + FaceIdx
// snapshot in the event fold), never the original's counters, attachments,
// damage or current combat state -- CR 706.2's copied-permanent rule, the
// same reading MyriadCopy's comment records.
//
// Source resolution, in order:
//
//   - Populate$ True with no Defined$/ValidTgts$: "a creature token you
//     control" (CR 701.27a), scanned through the ordinary Valid filter
//     grammar. A single eligible token is copied exactly; several keep the
//     deterministic first-candidate stand-in under one Note (the R-9 no-host
//     contract -- the controller's real choice needs an ask this primitive
//     cannot pose); zero eligible tokens is a legitimate no-op (populate does
//     nothing), never a Note.
//   - Defined$: resolved through knownDefinedTargets, FAIL-CLOSED -- an
//     unresolvable selector (ChosenMap, TriggeredSpellAbilityTargets, ...)
//     is one Note and NO mint, never a silent fall-through to the source or
//     the chosen targets (a wrong-copy is worse than no-copy here).
//   - ValidTgts$ (no Defined$): the answered target ask (Ctx.Targets / the
//     mvts1 PickedTargets arm) -- the Flamerush Rider shape.
//   - neither: no copy (defensive; the measured corpus carries no such line
//     outside the mint-blocked families above).
//
// Riders: TokenTapped$ True and TokenAttacking$ True (literal-True arms; the
// TokenAttacking$ True-no-defender and non-True selector values follow
// effToken's degrade -- the copy enters but does not attack, one Note),
// RememberTokens$ True (append each mint to Ctx.Remembered + the event-backed
// remember, so a chained SubAbility$ reads it), AtEOT$ Exile/Sacrifice
// (the builtin delayed-trigger bodies -- __kwWarpExile/__kwEncoreSacrifice --
// registered on the token itself, so Defined$ Self in those bodies IS the
// copy: "exile/sacrifice it at the beginning of the next end step"), AtEOT$
// ExileCombat (the CopyTokenExileCombat bit: the copy is flagged IsMyriad and
// the existing MyriadCleanup sweep exiles it at end of combat), and
// Controller$ You/Targeted*/Remembered*/TriggeredCardController/Opponent and
// the per-player NonRememberedController families. Any other Controller$ or
// AtEOT$ value is one loud Note per call.
func effCopyPermanent(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()

	// A re-entry after one copy's battlefield entry parked behind an
	// entry-counter order ask and was answered (TokenRest, the continuation
	// effToken uses): the parked copy is owed only its post-entry riders and
	// the copies after it are still owed. The source selection, copy count
	// and controllers are the first pass's (frozen in the rest), and the
	// first pass's one-per-call Notes are not repeated.
	rest := resumingMint(c, sa)
	// preNotes keeps the Notes emitted before the Choices$ ask: the legacy
	// "copypermanent_choice" re-entry re-runs this walk from its first line
	// and so emits them again, which the resolution kernel's tape branch
	// reproduces.
	var preNotes []events.Event
	emitNote := func(ev events.Event) {
		if rest == nil {
			h.Emit(ev)
			preNotes = append(preNotes, ev)
		}
	}

	// One loud Note per call naming every skipped family (never per mint --
	// a NumCopies$ 2 copy must not say it twice). Literally keyed reads only:
	// the param census rejects a dynamic Params key it cannot attribute.
	//
	//   - The mint-blocking families are the source-selection parameters
	//     whose copy is a mid-call choice this build cannot pose from an
	//     effect (the mvts1 sub-ask machinery only covers a body's own
	//     ValidTgts$): the SA carrying one mints NOTHING rather than silently
	//     falling back to a wrong source. Measured population over the 249
	//     raw DB$ CopyPermanent lines: Choices$ 9, DefinedName$ 8, Pawprint$ 1,
	//     RandomCopied$+RandomNum$ 1 (one RandomCopied line),
	//     ValidSupportedCopy$ 1.
	//   - The unblocking families are the characteristic modifications: when
	//     the source IS reachable the copy mints and these are applied to it.
	//     AddTypes$, SetPower$, SetToughness$, SetColor$, SetCreatureTypes$,
	//     RemoveCardTypes$, RemoveCreatureTypes$, AddKeywords$, PumpKeywords$,
	//     RemoveKeywords$ and NonLegendary$ ARE implemented (applied to the
	//     mint as tracked continuous effects below); the remaining
	//     modifications that need real ability/name/attachment machinery --
	//     AddTriggers$, AddSVars$, AddAbilities$, WithDifferentNames$,
	//     AttachedTo$, Chooser$ -- are noted and the copy keeps the original's
	//     printed characteristics. RemoveSubTypes$ is subsumed by
	//     RemoveCardTypes$ (state.ContinuousEffect's strip keeps only
	//     supertypes, so a subtype is already gone) and is accepted without a
	//     note.
	cp := CopyPermanentOf(sa)
	if rest == nil {
		noteUnreadParams(h, c, "CopyPermanent", cp.Unread)
	}
	if cp.SkippedNote != "" {
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: cp.SkippedNote})
	}
	if cp.Blocked {
		// The source itself is unreachable: nothing to copy.
		return
	}
	supportsChoice := cp.SupportsChoice

	// AtEOT$ that resolves to a shape this round does not implement: the copy
	// still mints and simply stays (one Note per call, never a silent
	// mislaid expiry).
	atEOT := cp.AtEOT
	if atEOT != "" && atEOT != "Exile" && atEOT != "Sacrifice" && atEOT != "ExileCombat" {
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "AtEOT$ " + atEOT + " is not implemented; the copy stays on the battlefield"})
	}

	// AtEOTTrig$ is NOT the AtEOT$ rider above: it is a triggered ability the
	// copy carries as part of its copiable values ("except it has 'At the
	// beginning of the end step, sacrifice this token'"), so unlike the
	// one-shot delayed AtEOT$ registration a token COPY of the minted token
	// inherits it (CR 707.2). The body name rides the CopyToken event's
	// Counter; events.Apply stores it on the mint -- inheriting the source
	// object's body when the spell carries none -- and rules'
	// checkGrantedAtEOTTriggers puts the Phase/EndStep trigger on the stack for
	// every battlefield object carrying one. `Sacrifice` and `Exile` are the
	// two bodies the measured corpus uses; any other value (Gut Fanatical
	// Priestess's `You_Sacrifice`, a "your next end step" DELAYED trigger this
	// copiable-ability shape is not) stays LOUD -- one Note per call, never a
	// silent drop.
	atEOTTrigBody := ""
	switch atEOTTrig := cp.AtEOTTrig; atEOTTrig {
	case "":
		// No rider: a copy of the minted token still inherits the SOURCE
		// object's copiable body (events.Apply's fallback).
	case "Sacrifice":
		atEOTTrigBody = "__cpAtEOTSacrifice"
	case "Exile":
		atEOTTrigBody = "__cpAtEOTExile"
	default:
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "AtEOTTrig$ " + atEOTTrig + " is not implemented; the copy gets no end-step trigger"})
	}

	// Characteristic modifications (the Embalm/Eternalize family and the
	// wider CopyPermanent mod census): AddTypes$, SetColor$, SetPower$ and
	// SetToughness$. Each is applied as a tracked continuous effect sourced
	// to the minted token itself (the effToken TokenPower$/TokenToughness$
	// precedent), after the mint, so a replay re-derives the identical
	// characteristics from the same AddContinuous calls. A value this build
	// cannot resolve is one loud Note per call and the modification is
	// skipped -- never a silent wrong characteristic.
	var addTypes []string
	if raw, ok := cp.AddTypes.Text, cp.AddTypes.Present; ok {
		// Forge's multi-type separator is " & " inside a comma-list element
		// (rules/layers.go's statList is the established reader of the same
		// parameter), so split both ways and trim each part: "Creature &
		// Fractal" is TWO types, not one garbage word.
		addTypes = copyTypeList(raw)
		if len(addTypes) == 0 {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "AddTypes$ " + strings.TrimSpace(raw) + " resolved to no type; no type added"})
		}
	}
	var addColors []string
	setColor := false
	if raw, ok := cp.SetColor.Text, cp.SetColor.Present; ok {
		cols, parsed := colorLetters(raw)
		if parsed {
			setColor = true
			addColors = cols
		} else {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "SetColor$ " + strings.TrimSpace(raw) + " is not a colour; the copy keeps its printed colours"})
		}
	}
	var setPow, setTgh int32
	var hasSetPow, hasSetTgh bool
	if raw, ok := cp.SetPower.Text, cp.SetPower.Present; ok {
		if v, resolved := numResolvedText(h, c, cp.SetPower, 0); resolved {
			setPow, hasSetPow = v, true
		} else {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "SetPower$ " + strings.TrimSpace(raw) + " is not resolvable; the copy keeps its printed power"})
		}
	}
	if raw, ok := cp.SetToughness.Text, cp.SetToughness.Present; ok {
		if v, resolved := numResolvedText(h, c, cp.SetToughness, 0); resolved {
			setTgh, hasSetTgh = v, true
		} else {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "SetToughness$ " + strings.TrimSpace(raw) + " is not resolvable; the copy keeps its printed toughness"})
		}
	}

	// SetCreatureTypes$ replaces the copy's creature types exactly (Croaking
	// Counterpart's "except it's a Frog", The Eleventh Hour's Alien): the
	// value is split like AddTypes$ (comma / " & ") and applied as ONE LType
	// effect that strips the printed creature subtypes BEFORE adding the named
	// list, which is what makes the result exactly the named list rather than
	// an addition. An unresolvable value (nothing after the split) keeps the
	// loud-Note-skip so no silent wrong type lands.
	var setCreatureTypes []string
	if raw, ok := cp.SetCreatureTypes.Text, cp.SetCreatureTypes.Present; ok {
		setCreatureTypes = copyTypeList(raw)
		if len(setCreatureTypes) == 0 {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "SetCreatureTypes$ " + strings.TrimSpace(raw) + " resolved to no type; no creature type set"})
		}
	}
	// RemoveCardTypes$ / RemoveSubTypes$ / RemoveCreatureTypes$ are always
	// True in the corpus. RemoveSubTypes$ is subsumed by RemoveCardTypes$
	// (its strip keeps only supertypes, so every subtype is already gone),
	// but a RemoveSubTypes$ WITHOUT RemoveCardTypes$ still needs the creature
	// strip below, so both feed RemoveCreatureTypes when the types are being
	// set or removed. A non-True value is one loud note and no strip. Each
	// parameter is read through its LITERAL Params key (the param census
	// rejects a dynamic key read), the value then tested by the shared
	// stripTrue helper.
	stripTrue := func(label, raw string) bool {
		if strings.EqualFold(strings.TrimSpace(raw), "True") {
			return true
		}
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: label + " " + strings.TrimSpace(raw) + " is not implemented; the copy keeps its printed types"})
		return false
	}
	removeCardTypes := false
	if raw, ok := cp.RemoveCardTypes.Text, cp.RemoveCardTypes.Present; ok {
		removeCardTypes = stripTrue("RemoveCardTypes$", raw)
	}
	removeCreatureTypes := len(setCreatureTypes) > 0
	if raw, ok := cp.RemoveCreatureTypes.Text, cp.RemoveCreatureTypes.Present; ok {
		removeCreatureTypes = stripTrue("RemoveCreatureTypes$", raw) || removeCreatureTypes
	}
	if raw, ok := cp.RemoveSubTypes.Text, cp.RemoveSubTypes.Present; ok {
		removeCreatureTypes = stripTrue("RemoveSubTypes$", raw) || removeCreatureTypes
	}
	// NonLegendary$ True drops just the Legendary supertype (Multiversal
	// Recruitment's "except it's not legendary"); always True in the corpus.
	removeLegendary := false
	if raw, ok := cp.NonLegendary.Text, cp.NonLegendary.Present; ok {
		removeLegendary = stripTrue("NonLegendary$", raw)
	}

	// AddKeywords$ and PumpKeywords$ (the temporary-keyword body): both are
	// ampersand-joined keyword lists cards.SplitKeywordList reads ("Flying &
	// Haste" is two). RemoveKeywords$ names keywords lost at layer 6. All three
	// ride ONE LAbilities effect per mint so RemoveKeywords applies BEFORE the
	// same effect's AddKeywords, whatever the timestamps order neighbours.
	addKeywords := cards.SplitKeywordList(cp.AddKeywords)
	pumpKeywords := cards.SplitKeywordList(cp.PumpKeywords)
	removeKeywords := cards.SplitKeywordList(cp.RemoveKeywords)
	if ok := cp.AddKeywordsPresent; ok && len(addKeywords) == 0 {
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "AddKeywords$ " + strings.TrimSpace(cp.AddKeywords) + " resolved to no keyword; none added"})
	}
	// PumpDuration$ governs the PumpKeywords$ lifetime: absent means "for as
	// long as the copy exists" (Permanent); EOT/EndOfTurn is dropped at this
	// turn's cleanup; the next-turn forms get the ordinary UntilTurn boundary.
	// An unresolvable duration is one loud note and the grant is Permanent.
	pumpDuration := cp.PumpDuration
	pumpPermanent := len(pumpKeywords) == 0
	pumpUntilEOT := false
	if len(pumpKeywords) > 0 {
		switch {
		case pumpDuration == "":
			pumpPermanent = true
		case IsNextTurnDuration(pumpDuration):
			// AddContinuous derives the UntilTurn boundary from the live rotation.
		case strings.EqualFold(pumpDuration, "EOT") || strings.EqualFold(pumpDuration, "EndOfTurn") ||
			strings.EqualFold(pumpDuration, "UntilEndOfTurn"):
			pumpUntilEOT = true
		default:
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "PumpDuration$ " + pumpDuration + " is not implemented; the copy keeps the keyword"})
			pumpPermanent = true
		}
	}

	// WithCountersType$/WithCountersAmount$ (littjara_mirrorlake's "a token
	// that's a copy ... enters with an additional +1/+1 counter on it",
	// Ochre Jelly's split half "enters with half that many +1/+1 counters"):
	// every copy this call mints enters with that many of the named counter
	// kind, one CounterChange per mint right after the CopyToken+MoveZone --
	// the ChangeZone entry counters' exact shape, so AddCounter replacements
	// and CounterAdded triggers see the copy's entry counter the way they see
	// any other placement. The amount resolves through the ordinary Num
	// grammar (absent WithCountersAmount$ = 1 -- littjara's shape; an SVar
	// name -- Ochre Jelly's WithCountersAmount$ Y over
	// SVar:Y:TriggerRemembered$CardCounters.P1P1/HalfDown); an unresolvable
	// value is one loud Note per call and the counters are skipped -- the
	// copy enters without them, never a silent wrong count.
	withKind := cp.WithCountersType
	var withAmt int32
	var withOK bool
	if withKind != "" {
		if cp.WithCountersAmount.Present {
			if v, ok := numResolvedText(h, c, cp.WithCountersAmount, 1); ok {
				withAmt, withOK = v, true
			} else {
				emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "WithCountersAmount$ " + strings.TrimSpace(cp.WithCountersAmount.Text) +
						" is not implemented; the copy enters with no " + withKind + " counters"})
			}
		} else {
			withAmt, withOK = 1, true
		}
	}

	// Entry-state riders.
	tapped := false
	if v := cp.TokenTapped; v != "" {
		if strings.EqualFold(v, "True") {
			tapped = true
		} else {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "TokenTapped$ " + v + " is not implemented; the copy enters untapped"})
		}
	}
	var attacking bool
	var defender state.PlayerID
	if attack := cp.TokenAttacking; attack != "" {
		attacking, defender = tokenAttackingRider(h, c, attack, "copy")
	}

	// Copy count: a literal or resolvable NumCopies$ is honoured; an
	// unresolvable one (X/Y/Wins without a binding) is one copy under a Note.
	n := int32(1)
	if raw, ok := cp.NumCopies.Text, cp.NumCopies.Present; ok {
		if v, resolved := numResolvedText(h, c, cp.NumCopies, 1); resolved {
			n = v
		} else {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "NumCopies$ " + strings.TrimSpace(raw) + " is not implemented; one copy"})
		}
	}
	if rest != nil {
		n = rest.Amount
	}
	if n <= 0 {
		return
	}

	// Copy source.
	spec := cp.Defined
	hasTgts := cp.HasTgts
	populate := cp.Populate
	var targets []state.Target
	switch {
	case rest != nil:
		for _, id := range rest.Objs {
			targets = append(targets, state.Target{Obj: id})
		}
	case populate && spec == "" && !hasTgts:
		cands := DefinedSpec(h, c, "Valid Creature.token+YouCtrl")
		if len(cands) > 1 {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Populate$ with several eligible creature tokens copies the first (no engine host to ask)"})
		}
		if len(cands) == 0 {
			// CR 701.27a: with no creature token you control, populate does
			// nothing. A legitimate no-op, not a defect.
			return
		}
		targets = cands[:1]
	case supportsChoice:
		chooser := c.Controller
		for _, t := range c.Remembered {
			if t.IsPlayer {
				chooser = t.Player
				break
			}
			if o := g.Obj(t.Obj); o != nil {
				chooser = o.Controller
				break
			}
		}
		// Choices$ names a CARD FILTER, not a Defined$ selector (Forge's
		// CopyPermanent Choices$ is "the pool the chooser picks from"), so
		// it is spelled with the established `Defined$ Valid <filter>` form
		// and resolved through the battlefield filter sweep -- the
		// RememberedPlayerCtrl predicate then matches creatures controlled
		// by the remembered friend. Passing the bare filter as a Defined$
		// selector would fall through Defined's per-member fallback to the
		// resolving SOURCE, offering the chooser the spell itself.
		pick := DefinedSpec(h, c, "Valid Creature.RememberedPlayerCtrl")
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
			Source: c.Source, ResumeKind: "copypermanent_choice", ResumeSA: sa,
			Prompt: "Choose a creature to copy"}
		for i, t := range pick {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			d.Options = append(d.Options, decision.Option{Index: i, Obj: t.Obj, Kind: "permanent"})
		}
		if len(d.Options) == 0 {
			return
		}
		if cp.TokenAttacking == "" {
			// (A TokenAttacking$ rider emits its own Notes before the ask,
			// outside preNotes, so that shape stays on the legacy path.)
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the
				// "copypermanent_choice" re-entry, after the Notes its re-run
				// emits again; a zero pick copies nothing.
				for _, ev := range preNotes {
					h.Emit(ev)
				}
				if len(ans) == 0 || ans[0].Obj == 0 {
					return
				}
				targets = []state.Target{{Obj: ans[0].Obj}}
				break
			}
		}
		if !askUnposable(d) {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
				Text: "CopyPermanent Choices$ has no engine host; copying the first eligible creature"})
		}
		targets = []state.Target{{Obj: d.Options[0].Obj}}
	case false:

		return

	case spec == "Remembered":
		// The resolution's OWN remembered set, never the trigger's event
		// capture (see rememberedWrittenByResolution): Dedicated Dollmaker's
		// "exile up to one target ..., create a copy of it" otherwise also
		// copied the Dollmaker whose ETB fired, and with no target copied it
		// alone -- the new token's ETB then did the same, forever.
		targets = rememberedWrittenByResolution(c)
	case spec != "":
		ts, ok := knownDefinedTargets(h, c, spec)
		if !ok {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "CopyPermanent source " + spec + " is not resolvable; no copy"})
			return
		}
		targets = ts
	case hasTgts:
		targets = Defined(h, c, sa)
	default:
		return
	}
	// A repeated entry in the resolved target list is never intentional:
	// NumCopies$ is the spelling for "two copies of one permanent" and rides
	// the n loop below. The trigger's fire-time capture and a sub-ability's
	// RememberChanged$ append can both land the same object in Ctx.Remembered,
	// so a Defined$ TriggeredCardLKICopy selector yields [bearer, bearer] and
	// the mint loop below would emit one CopyToken per duplicate. Dedupe by
	// OBJECT identity, preserving first-seen order -- two DIFFERENT remembered
	// objects (Myrkul's [self, reanimated]) must both survive.
	targets = dedupeTargets(targets)

	// Controller$ of the copy.
	owner := c.Controller
	var owners []state.PlayerID
	multiOwner := false
	switch effCopyPermanent5251Codes.Code(string(cp.Controller)) {
	case effCopyPermanent5251Empty:
	case effCopyPermanent5251Targeted:
		if ps := controllersOf(g, targets); len(ps) > 0 {
			owner = ps[0].Player
		}
	case effCopyPermanent5251Remembered:
		if ps := controllersOf(g, rememberedWrittenByResolution(c)); len(ps) > 0 {
			owner = ps[0].Player
		}
	case effCopyPermanent5251TriggeredCardController:
		if p, ok := TriggeredCardController(g, c.TriggerContext, c.Remembered); ok {
			owner = p
		}
	case effCopyPermanent5251Opponent:
		for _, p := range g.AliveFrom(c.Controller) {
			if p != c.Controller {
				owner = p
				break
			}
		}
	case effCopyPermanent5251NonRememberedController:
		multiOwner = true
		ts, _ := definedSpec(h, c, cp.Controller)
		for _, t := range ts {
			if t.IsPlayer {
				owners = append(owners, t.Player)
			}
		}
	default:
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "Controller$ " + cp.Controller +
				" is not implemented; the copy is controlled by the resolving controller"})
	}

	if !multiOwner {
		owners = []state.PlayerID{owner}
	}
	if rest != nil {
		owners = append([]state.PlayerID(nil), rest.Players...)
	}
	remember := cp.RememberTokens
	var amount int32
	if tapped {
		amount |= events.CopyTokenTapped
	}
	if attacking {
		amount |= events.CopyTokenAttacking
	}
	if atEOT == "ExileCombat" {
		amount |= events.CopyTokenExileCombat
	}
	var ids []state.ObjID
	if attacking {
		ids = []state.ObjID{state.ObjID(defender)}
	}
	// The same capture+re-remember duplication reaches TokenRemembered$
	// Remembered (resolvedRemembered returns Ctx.Remembered raw when the
	// source owns no persistent list), which would double each copy's
	// memory entry. Dedupe it with the same rule the mint list uses.
	tokenMemory := dedupeTargets(tokenRememberedTargets(h, c, cp.TokenRemembered))

	// Resolve the named attachment endpoint before minting. The endpoint is
	// intentionally a destination selector, not a bearer-choice feature. It
	// must be an object ON THE BATTLEFIELD: an attachment point that is not a
	// permanent (a card in a graveyard, an exiled object, an object that has
	// already left) is not a legal endpoint, and emitting Attach at one would
	// fasten the copy to a non-battlefield object the attachment SBAs cannot
	// reason about. The check is the same battlefield gate effAttach applies.
	var attachTo state.ObjID
	attachedToRaw := cp.AttachedTo
	attachedToNamed := attachedToRaw != ""
	if raw := attachedToRaw; raw != "" {
		for _, t := range DefinedSpec(h, c, raw) {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			o := g.Obj(t.Obj)
			if o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			attachTo = t.Obj
			break
		}
		if attachTo == 0 {
			emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "AttachedTo$ " + raw + " resolved to no legal battlefield permanent; the copy enters unattached"})
		}
	}
	// Copy the resolving source face's named SVar entries. The maps are
	// populated from the comma lists, never ranged, so trigger order is stable.
	sourceSVars := c.SVars
	if sourceSVars == nil {
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			sourceSVars = o.Face().SVars
		}
	}
	var grantTriggers []*cards.Trigger
	for name := range strings.SplitSeq(cp.AddTriggers, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if raw, ok := sourceSVars[name]; ok {
			if tr, ok := cards.ParseTriggerLine(raw); ok {
				// Resolve the trigger's Execute$ body against the SAME source
				// table (linkGrantedTrigger).
				linkGrantedTrigger(&tr, sourceSVars)
				x := tr
				grantTriggers = append(grantTriggers, &x)
			}
		}
	}
	grantSVars := make(map[string]string)
	for name := range strings.SplitSeq(cp.AddSVars, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			if raw, ok := sourceSVars[name]; ok {
				grantSVars[name] = raw
			}
		}
	}
	var grantAbilities []string
	for name := range strings.SplitSeq(cp.AddAbilities, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			if _, ok := sourceSVars[name]; ok {
				grantAbilities = append(grantAbilities, name)
			}
		}
	}
	// AddStaticAbilities$ names static bodies on the same source table the
	// copy gains ("except it has 'This Equipment's equip abilities cost {2}
	// less to activate.'" -- Firion, Wild Rose Warrior). A cost-modifier body
	// the cost chain can read is registered as a granted cost static bound
	// to the copy (rules' appendGrantedCostStatic), the route the Animate
	// staticAbilities$ and Continuous AddStaticAbility$ grants share; any
	// other mode or an unread scoping key is one loud Note.
	type costGrant struct {
		mode   string
		params map[string]string
	}
	var grantCostStatics []costGrant
	var unreadStatics []string
	for _, name := range strings.FieldsFunc(cp.AddStaticAbilities, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if _, ok := sourceSVars[name]; !ok {
			continue // reported with the other unresolved grant names below
		}
		mode, params := parseStaticLine(sourceSVars, name)
		if IsGrantableCostStaticMode(mode) && CostStaticParamsReadable(params) {
			grantCostStatics = append(grantCostStatics, costGrant{mode: mode, params: params})
			continue
		}
		unreadStatics = append(unreadStatics, name)
	}
	if len(unreadStatics) > 0 {
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "CopyPermanent AddStaticAbilities$ " + strings.Join(unreadStatics, ", ") +
				" is not a static this engine grants; the copy does not gain it"})
	}
	// A grant name that does not resolve is one loud Note per call -- the
	// AddKeywords$ precedent -- never a silent drop.
	var lostGrants []string
	for _, raw := range []string{cp.AddTriggers, cp.AddSVars, cp.AddAbilities, cp.AddStaticAbilities} {
		for name := range strings.SplitSeq(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := sourceSVars[name]; !ok {
				lostGrants = append(lostGrants, name)
			}
		}
	}
	if len(lostGrants) > 0 {
		emitNote(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "CopyPermanent grant " + strings.Join(lostGrants, ", ") +
				" does not resolve; the copy does not gain it"})
	}

	type copyDestination struct {
		owner  state.PlayerID
		target state.Target
	}
	var destinations []copyDestination
	for _, owner := range owners {
		for _, target := range targets {
			destinations = append(destinations, copyDestination{owner: owner, target: target})
		}
	}
	// postEntry is one copy's post-entry work: every rider that reads the
	// copy as a permanent. It runs only once the copy's battlefield entry has
	// completed -- on the first pass for an uncontested entry, or on the
	// TokenRest re-entry after a parked entry-counter order is answered.
	var minted []state.ObjID
	if rest != nil {
		minted = append(minted, rest.Minted...)
	}
	postEntry := func(owner state.PlayerID, want state.ObjID) {
		minted = append(minted, want)
		if attachTo != 0 {
			// The shared Attach emission: it publishes Unattached first
			// when the copy was already attached to a different bearer,
			// so a re-attach cannot drop the Mode$ Unattached family.
			target := g.Obj(attachTo)
			if o := g.Obj(want); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo == attachTo {
				// Already attached as it entered, or withheld from the
				// battlefield (an Aura copy the engine settled,
				// rules/aura_entry.go).
				target = nil
			}
			if target != nil && target.Zone == state.ZBattlefield && Attachable(g, want, attachTo) {
				emitAttach(h, want, attachTo)
			}
		}
		if withOK {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: want, Counter: withKind, Amount: withAmt})
		}
		// Characteristic modifications, scoped to the copy itself
		// (Affects Card.Self, Source the token). Permanent so the effect
		// outlives its one-shot resolution and lasts as long as the token;
		// the layer system re-derives them from the same calls on replay.
		// One LType effect carries every type modification so the
		// strip-before-add order is guaranteed within the effect: the
		// printed creature subtypes leave BEFORE SetCreatureTypes$/AddTypes$
		// land, and RemoveCardTypes$/RemoveLegendary strip the base first.
		allTypes := append(append([]string(nil), addTypes...), setCreatureTypes...)
		if len(allTypes) > 0 || removeCardTypes || removeCreatureTypes || removeLegendary {
			h.AddContinuous(state.ContinuousEffect{
				Source: want, Controller: owner, Affects: "Card.Self",
				Layer: state.LType, AddTypes: allTypes,
				RemoveCardTypes: removeCardTypes, RemoveCreatureTypes: removeCreatureTypes,
				RemoveLegendary: removeLegendary, Permanent: true,
			})
		}
		if setColor {
			h.AddContinuous(state.ContinuousEffect{
				Source: want, Controller: owner, Affects: "Card.Self",
				Layer: state.LColor, AddColors: addColors, OverwriteColors: true, Permanent: true,
			})
		}
		if hasSetPow || hasSetTgh {
			pow, tgh := int32(0), int32(0)
			if f := g.Obj(want).Face(); f != nil {
				pow, tgh = int32(f.Power()), int32(f.Toughness())
			}
			if hasSetPow {
				pow = setPow
			}
			if hasSetTgh {
				tgh = setTgh
			}
			h.AddContinuous(state.ContinuousEffect{
				Source: want, Controller: owner, Affects: "Card.Self",
				Layer: state.LPT, Sub: state.SubSet,
				SetPower: pow, SetToughness: tgh, HasSet: true, Permanent: true,
			})
		}
		// Keywords: RemoveKeywords$ applies BEFORE AddKeywords$ within
		// this one effect (CR 613.1f), so Mirage Phalanx's copy loses
		// Soulbond and gains Haste. PumpKeywords$ is the temporary body:
		// its own effect carries the PumpDuration$ lifetime.
		kwGrant := addKeywords
		if len(kwGrant) > 0 || len(removeKeywords) > 0 {
			h.AddContinuous(state.ContinuousEffect{
				Source: want, Controller: owner, Affects: "Card.Self",
				Layer: state.LAbilities, AddKeywords: kwGrant,
				RemoveKeywords: removeKeywords, Permanent: true,
			})
		}
		if len(pumpKeywords) > 0 {
			h.AddContinuous(state.ContinuousEffect{
				Source: want, Controller: owner, Affects: "Card.Self",
				Layer: state.LAbilities, AddKeywords: pumpKeywords,
				Duration: pumpDuration, Permanent: pumpPermanent, UntilEOT: pumpUntilEOT,
			})
		}
		if remember {
			c.Remembered = append(c.Remembered, state.Target{Obj: want})
			eventRemember(h, c, want)
		}
		switch effCopyPermanent5252Codes.Code(string(atEOT)) {
		case effCopyPermanent5252Exile:
			// The registration's source IS the token, so the builtin
			// body's Defined$ Self resolves to it -- the dash/warp
			// precedent. TrackSource rides the __kwWarp prefix, so a copy
			// that left the battlefield and returned as a new incarnation
			// is not exiled by a stale promise (the same one-shot consume
			// warp's end-step exile already had).
			h.Emit(events.Event{Kind: events.DelayedRegister, Obj: want,
				Player: owner, Step: state.StepEnd, Counter: "__kwWarpExile"})
		case effCopyPermanent5252Sacrifice:
			// __kwEncoreSacrifice is exactly the body this needs
			// ("DB$ Sacrifice | Defined$ Self"); the token is its own
			// registration source. Untracked: a sacrificed-then-returned
			// copy keeps the promise, the same semantics encore's group
			// registration holds.
			h.Emit(events.Event{Kind: events.DelayedRegister, Obj: want,
				Player: owner, Step: state.StepEnd, Counter: "__kwEncoreSacrifice"})
		}
	}
	targetObjs := make([]state.ObjID, 0, len(targets))
	for _, t := range targets {
		if !t.IsPlayer {
			targetObjs = append(targetObjs, t.Obj)
		}
	}
	// Creating a token copy is creating a token (CR 111.1, 706.2), so the
	// CreateToken replacements (Doubling Season's "twice that many", ...)
	// size each destination's copy count. The host's proposal is taken once
	// per creation: a resumed pass reads the first pass's counts back from
	// its TokenRest, so the cursor below indexes the same units and a
	// replacement's scripted extra mints are never created twice.
	counts := make([]int32, len(destinations))
	if rest != nil && len(rest.Counts) == len(destinations) {
		copy(counts, rest.Counts)
	} else {
		for d, destination := range destinations {
			counts[d] = n
			if t := destination.target; !t.IsPlayer && g.Obj(t.Obj) != nil {
				counts[d] = proposeCopyTokens(h, destination.owner, t.Obj, n)
			}
		}
	}
	base := 0
	for d, destination := range destinations {
		owner, t := destination.owner, destination.target
		first := base
		base += int(counts[d])
		for i := int32(0); i < counts[d]; i++ {
			// unit is this copy's position in the call's deterministic
			// destination x copy-count order: the TokenRest cursor.
			unit := first + int(i)
			if rest != nil && unit < rest.Next {
				continue
			}
			if rest != nil && unit == rest.Next {
				for _, id := range rest.Parked {
					if g.Obj(id) != nil {
						postEntry(owner, id)
					}
				}
				continue
			}
			if t.IsPlayer || g.Obj(t.Obj) == nil {
				continue
			}
			// want is the ID the mint will get if Apply's CopyToken case
			// actually mints one (state.Game.AddObject assigns NextID then
			// increments it) -- the effToken/effMyriad prediction pattern.
			want := g.NextID
			h.Emit(events.Event{Kind: events.CopyToken, Obj: t.Obj, Player: owner,
				Amount: amount, IDs: ids, Counter: atEOTTrigBody})
			if g.Obj(want) == nil {
				continue
			}
			if len(tokenMemory) > 0 {
				remembered := make([]state.ObjID, 0, len(tokenMemory))
				for _, rememberedTarget := range tokenMemory {
					if rememberedTarget.IsPlayer {
						remembered = append(remembered, state.PlayerRef(rememberedTarget.Player))
					} else if rememberedTarget.Obj != 0 {
						remembered = append(remembered, rememberedTarget.Obj)
					}
				}
				if len(remembered) > 0 {
					h.Emit(events.Event{Kind: events.Choose, Obj: want, Counter: "remembered", IDs: remembered})
				}
			}
			// Register the named-ability/trigger grants BEFORE the copy enters
			// the battlefield: a granted "when this creature enters" trigger
			// must already be live when the entry MoveZone is applied, or the
			// trigger walk sees no grant for the entering object and the copy
			// silently lacks its text. The object id is already known (the
			// want prediction), and every grant is self-scoped (Card.Self),
			// so registering first changes nothing but the trigger's visibility.
			if len(grantTriggers) > 0 || len(grantSVars) > 0 || len(grantAbilities) > 0 {
				h.AddContinuous(state.ContinuousEffect{Source: want, Controller: owner,
					Affects: "Card.Self", Layer: state.LAbilities, Permanent: true,
					SVars: sourceSVars, AddSVars: cloneStringMap(grantSVars), AddAbilities: append([]string(nil), grantAbilities...),
					TriggerGrantor: c.Source, AbilityGrantor: c.Source})
				for _, tr := range grantTriggers {
					trCopy := *tr
					h.AddContinuous(state.ContinuousEffect{Source: want, Controller: owner,
						Affects: "Card.Self", Layer: state.LAbilities, Permanent: true,
						AddTrigger: &trCopy, TriggerGrantor: c.Source,
						SVars: sourceSVars})
				}
			}
			// The granted cost statics are the copy's own for as long as it
			// exists: Permanent, Source the copy, bound to it at collection.
			for _, cg := range grantCostStatics {
				h.AddContinuous(state.ContinuousEffect{Source: want, Controller: owner,
					Affects: "Card.Self", Permanent: true,
					CostStaticMode: cg.mode, CostStaticParams: cg.params,
					CostStaticSVars: sourceSVars, CostStaticGranted: true})
			}
			// The entry goes through EmitTokenCreate: the emit tail publishes
			// the copy only once this MoveZone has actually folded onto the
			// battlefield (rules' publishTokenEntry), and reports a park when
			// the entry staged behind an entry-counter order ask.
			wasSuspended := h.Suspended()
			entry := events.Event{Kind: events.MoveZone, Obj: want,
				From: state.ZLibrary, To: state.ZBattlefield}
			if attachedToNamed {
				// AttachedTo$ names the copy's bearer: a copied Aura is
				// settled by the engine as it enters (CR 303.4f/g) --
				// attached to it, or not created when it is no longer
				// something the Aura can enchant -- and postEntry then
				// finds it already attached.
				if o := g.Obj(want); o != nil && hasType(o, "Aura") {
					var named []state.ObjID
					if attachTo != 0 {
						named = []state.ObjID{attachTo}
					}
					events.MarkNamedAttachEntry(&entry, named)
				}
			}
			entered := h.EmitTokenCreate(entry)
			if !wasSuspended && h.Suspended() && len(entered) == 0 {
				if suspendMint(h, c, TokenRest{SA: sa, Next: unit, Minted: minted,
					Players: owners, Objs: targetObjs, Amount: n, Counts: counts}) {
					return
				}
			}
			postEntry(owner, want)
		}
	}
	// ImprintTokens$ True records the created tokens on the SOURCE, the same
	// event-backed association api:Token writes (state.Object.ImprintTokens);
	// the copy path previously skipped it entirely, so a DelTrig reading
	// RememberObjects$ ImprintedLKI found nothing (Kharasha Foothills,
	// Shredder, Shadow Master).
	if cp.ImprintTokens && c.Source != 0 {
		ids := make([]state.ObjID, 0, len(minted))
		for _, id := range minted {
			if g.Obj(id) != nil {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: ids, Text: "imprint-tokens"})
		}
	}
}

// copyTypeList parses Forge's multi-type grammar the way rules' statList
// reads AddTypes$: comma separates list elements and " & " separates
// alternatives inside one element, so "Creature & Fractal, Artifact" is three
// type words. Whitespace is trimmed and empty members dropped; an absent or
// empty list yields nil. (SplitKeywordList alone would keep a comma as part
// of the same word, which is right for keywords and wrong here.)
func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyTypeList(list string) []string {
	var out []string
	for part := range strings.SplitSeq(list, ",") {
		out = append(out, cards.SplitKeywordList(part)...)
	}
	return out
}

// rememberedWrittenByResolution is Ctx.Remembered without the leading
// fire-time event capture (Ctx.Captured) a trigger resolution is seeded
// with -- this engine's stand-in for Forge's separate TriggeredCard, which
// Forge never puts in the host's remembered list. What remains is what the
// resolution's own Remember* riders (RememberChanged$, RememberLKI$, ...)
// wrote. It strips only an exact leading prefix: a writer that re-remembers
// the captured object (Myrkul's RememberChanged$ on the dying creature)
// appends a second entry, which survives; a ctx whose Remembered no longer
// starts with the capture (a Cleanup, a RepeatEach subject) is returned
// unchanged, as is a RepeatEach iteration's. A delayed trigger's RememberObjects$ capture IS Forge's
// remembered list, so a CopyPermanent body under one would lose it -- no
// corpus CopyPermanent carrier reads Defined$ Remembered that way (measured:
// every carrier's Remembered comes from a same-chain writer or RepeatEach).
func rememberedWrittenByResolution(c *Ctx) []state.Target {
	n := len(c.Captured)
	if n == 0 || len(c.Remembered) < n || c.RepeatSubject != (state.Target{}) {
		// A RepeatEach iteration binds its subject as Remembered; never
		// strip it even when it happens to equal the capture.
		return c.Remembered
	}
	for i, t := range c.Captured {
		if c.Remembered[i] != t {
			return c.Remembered
		}
	}
	return c.Remembered[n:]
}

const (
	effCopyPermanent5251Empty                   uint16 = 1 // "", "You"
	effCopyPermanent5251Targeted                uint16 = 2 // "Targeted", "TargetedController", "TargetedPlayer"
	effCopyPermanent5251Remembered              uint16 = 3 // "Remembered", "RememberedController"
	effCopyPermanent5251TriggeredCardController uint16 = 4 // "TriggeredCardController"
	effCopyPermanent5251Opponent                uint16 = 5 // "Opponent"
	effCopyPermanent5251NonRememberedController uint16 = 6 // "NonRememberedController", "OppNonRememberedController"
)

var effCopyPermanent5251Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "", Val: effCopyPermanent5251Empty},
	state.StrEntry[uint16]{Key: "You", Val: effCopyPermanent5251Empty},
	state.StrEntry[uint16]{Key: "Targeted", Val: effCopyPermanent5251Targeted},
	state.StrEntry[uint16]{Key: "TargetedController", Val: effCopyPermanent5251Targeted},
	state.StrEntry[uint16]{Key: "TargetedPlayer", Val: effCopyPermanent5251Targeted},
	state.StrEntry[uint16]{Key: "Remembered", Val: effCopyPermanent5251Remembered},
	state.StrEntry[uint16]{Key: "RememberedController", Val: effCopyPermanent5251Remembered},
	state.StrEntry[uint16]{Key: "TriggeredCardController", Val: effCopyPermanent5251TriggeredCardController},
	state.StrEntry[uint16]{Key: "Opponent", Val: effCopyPermanent5251Opponent},
	state.StrEntry[uint16]{Key: "NonRememberedController", Val: effCopyPermanent5251NonRememberedController},
	state.StrEntry[uint16]{Key: "OppNonRememberedController", Val: effCopyPermanent5251NonRememberedController},
)

const (
	effCopyPermanent5252Exile     uint16 = 1 // "Exile"
	effCopyPermanent5252Sacrifice uint16 = 2 // "Sacrifice"
)

var effCopyPermanent5252Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Exile", Val: effCopyPermanent5252Exile},
	state.StrEntry[uint16]{Key: "Sacrifice", Val: effCopyPermanent5252Sacrifice},
)
