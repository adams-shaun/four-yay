package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) resolveTop() {
	id := e.G.Stack[len(e.G.Stack)-1]
	o := e.G.Obj(id)
	if o != nil && o.IsCopy && (o.CopyMayChooseTarget || (e.copyTargetStage != nil && e.copyTargetStage[id] > 0)) {
		if e.AskCopyTargets() {
			return
		}
	}
	savedResolving := e.resolvingObj
	e.resolvingObj = id
	defer func() { e.resolvingObj = savedResolving }()

	if o.Ability != nil {
		// A keyword trigger that refers to one particular permanent incarnation
		// (Evoke's "sacrifice it") loses track when that permanent changes
		// zones. The ability still resolves and leaves the stack, but does
		// nothing to the new object now sharing its stable ObjID (CR 400.7).
		if o.SourceIncarnation != 0 {
			src := e.G.Obj(o.Source)
			if src == nil || src.Incarnation != o.SourceIncarnation {
				e.emit(events.Event{Kind: events.Resolve, Obj: id})
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
				return
			}
		}
		// A triggered or activated ability with no printed card: Ruling
		// T14-c / F3 -- Face() returns nil for these, so this branch must
		// run before anything below touches it. Task 20 is what actually
		// puts objects like this on the stack.
		//
		// CR 603.4 (intervening-if): a triggered ability's condition is
		// checked both when it would trigger AND again as it resolves; if
		// it no longer holds, the ability is removed from the stack and does
		// nothing. The trigger was queued because triggerConditionHolds was
		// true at trigger time (triggerMatches), but Scute Mob's "whenever
		// you control five or more lands" can be false by the time the
		// ability resolves -- here a real instant response destroyed one of
		// those lands. findTriggerForAbility identifies the T: line from the
		// SA pointer: it is false for an activated ability (whose SA comes
		// from Abilities, not a Triggers entry) so this recheck never
		// applies to one, and false for a source whose face has changed or
		// gone so a trigger-only rule never fires on unknown provenance.
		// The fizzle move is the same exile rest the ability branch uses for
		// CR 608.2b's no-legal-targets case: an ability "ceases to exist"
		// (608.2m) rather than moving to a card zone, and this build parks
		// such objects in exile. Ordered first because it decides whether
		// the ability does anything at all.
		if t, ok := e.triggerForAbilityObject(id, o); ok {
			// NoResolvingCheck$ True (Ugin's Mastery, Werewolf Pack Leader,
			// Love on the Battlefield, ...): the condition was checked only
			// when the trigger fired, and the transient state it counted (a
			// bounced attacker, drained power) must not fizzle the ability
			// here (triggerResolvingCheckHolds in rules/trigger_condition.go).
			// The captured event roles travel with the ability; passing them
			// is what lets an event-relative clause (Condition$
			// AttackedPlayerWithMostLife) be re-checked with the defender the
			// trigger queued against, which no current state can re-derive.
			tc := e.triggerContexts[id]
			if !e.triggerResolvingCheckHolds(t, o.Source, o.Controller, &tc, e.triggerLineSVars[id]) {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZStack, To: state.ZExile, Text: "fizzled: intervening-if no longer holds"})
				e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this "+
					"ability's 'intervening-if' move without relocating it anywhere; sent to exile "+
					"instead of re-resolving forever")
				return
			}
		}
		// No triggered or activated ability this build produces ever
		// actually populates Targets (only Remembered): Task 20's
		// checkTriggers never calls askTarget, which is the only place
		// TargetsChosen is ever emitted from. So this is unreachable in
		// practice today, but a stack object is a stack object, and CR
		// 608.2b's "spell or ability" covers this shape too if a later
		// task ever gives a triggered ability a player-chosen target.
		targets := o.Targets
		// alltargeted1 and the per-mode charm groups compose rather than
		// compete: collectSubTargetPreAsks excludes a modal (Charm) root
		// whole, so a charm-handled object has NO pre-asked sub answers and
		// recheckCastSubTargets returns (0, 0) for it; conversely a
		// non-modal chain never reaches recheckCharmTargets' groups.
		subChosen, subLegal := e.recheckCastSubTargets(id, o.Ability, o.Controller, o.Source)
		charmModeTargets, charmFlatTargets, charmHandled := e.recheckCharmTargets(o)
		if charmHandled {
			targets = charmFlatTargets
		}
		// Fix round 2 (re-review N1): the gate is `spec != ""` -- "this
		// ability declares a targeting requirement" -- not `len(targets) > 0`
		// -- "this ability happens to have targets right now". The old form
		// used the latter as a proxy for the former, so an ability that NEEDS
		// a target but has none recorded skipped CR 608.2b's recheck entirely
		// and resolved. Zero recorded targets is zero LEGAL targets, which is
		// exactly what 608.2b counters.
		// Requirement N2: an ability that MAY target zero things (TargetMin$ 0)
		// and has none recorded resolves untargeted rather than fizzling --
		// targetMin(o.Ability)==0 && len(targets)==0 is the exemption.
		if !charmHandled {
			if spec := o.Ability.Params["ValidTgts"]; spec != "" && !(e.resolvedTargetMin(o.Controller, id, o.Ability, 0) == 0 && len(targets) == 0) {
				legal := e.legalTargets(targets, o.Ability, targetZones(o.Ability), o.Controller, o.Source, id)
				// subLegal > 0 keeps a chain alive whose ROOT targets all
				// became illegal but whose pre-asked sub target did not
				// (alltargeted1); charmHandled cannot reach here, so the two
				// survival rules never overlap.
				if len(legal) == 0 && subLegal == 0 {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id,
						From: state.ZStack, To: state.ZExile, Text: "fizzled: no legal targets remain"})
					e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this "+
						"ability's 'fizzled: no legal targets' move without relocating it anywhere; "+
						"sent to exile instead of re-resolving forever")
					return
				}
				targets = legal
			}
		}
		if len(targets) == 0 && subChosen > 0 && subLegal == 0 {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZStack, To: state.ZExile, Text: "fizzled: no legal targets remain"})
			e.ensureLeftTheStack(id, state.ZExile, "all cast-time sub targets became illegal")
			return
		}
		// CR 608.2m: a resolved ability just ceases to exist rather than
		// moving to a card zone. This build has no "ceases to exist" zone,
		// so it is parked in exile as the closest existing approximation.
		e.emit(events.Event{Kind: events.Resolve, Obj: id})
		// CR 702.35b: the mandatory, respondable madness trigger makes its
		// cast-or-graveyard choice only as it resolves. Stifle reaches this
		// object before this branch; if the exiled card has moved meanwhile,
		// the ability simply finishes with no choice.
		if o.Ability.API == "MadnessCast" {
			if e.askMadnessCast(o) {
				return
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
			return
		}
		// CR 603.5: an optional triggered ability goes on the stack regardless
		// (putTriggersOnStack pushes it unconditionally), and its controller
		// -- or whatever seat its OptionalDecider$ names -- chooses whether to
		// apply the effect as the ability resolves. With the Resolve event
		// already logged, pose that yes/no now and SPEND the resolution: a
		// yes re-enters it (resumeResolution runs the effect, exactly as the
		// tail below would have), a no lets the ability leave the stack
		// having done nothing. A decider who has left the game is nobody to
		// apply an effect to, so the ability ceases to exist (CR 800.4a) and
		// is parked in exile like the other ceased-to-exist rests. Activated
		// abilities and mandatory triggers (findTriggerForAbility returns
		// false for the former, or an OptionalDecider-less trigger for the
		// latter) fall straight through to their effect below.
		rt, triggered := e.triggerForAbilityObject(id, o)
		// resSpec is the OptionalDecider$ spec this ability must ask about.
		// A printed trigger's comes off its face T: line (findTriggerForAbility
		// recovered it). An Effect-created delayed trigger has no face T: line:
		// its Ability is an Execute$ SVar sub-ability, so findTriggerForAbility
		// reports false and the spec rides effects.TriggerContext.OptionalSpec
		// from the registration instead (effects/misc.go effEffect ->
		// state.DelayedTrigger.OptionalSpec -> checkDelayedTriggers /
		// checkEventDelayedTriggers -> this map). Without the fallback an Effect
		// trigger with OptionalDecider$ (Beck's "you may draw a card") would
		// resolve mandatorily, the opposite of the card text.
		resSpec := ""
		// An Effect registration's spec wins even when the trigger-line
		// provenance (abcopy) now recognizes the delayed body as triggered.
		if spec := e.triggerContexts[id].OptionalSpec; spec != "" {
			resSpec = spec
		} else if triggered {
			resSpec = rt.Params["OptionalDecider"]
		}
		if resSpec != "" {
			who, askable := e.deciderFromSpec(resSpec, o.Controller, o.Remembered, e.triggerContexts[id])
			if !askable {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZStack, To: state.ZExile, Text: "ceased to exist: its optional decider left the game"})
				e.ensureLeftTheStack(id, state.ZExile, "the optional decider of this ability left the game, so the "+
					"ability ceased to exist (CR 800.4a) and was parked in exile")
				return
			}
			label := e.abilityLabel(o, cards.Trigger{})
			if triggered {
				label = e.abilityLabel(o, rt)
			}
			e.askOptionalAtResolution(who, o, o.Ability, label, e.triggerContexts[id].OptionalSpec != "")
			return
		}
		// ResolvedLimit$ ("Do this only once each turn."): a MANDATORY
		// trigger that reaches this point is one whose effect is about to run
		// (the optional gate above returned for every OptionalDecider$ shape),
		// so consume its per-turn resolution count now -- before the
		// CumulativeUpkeep/Echo/Cost$ dispatch below, which may open a
		// pay/decline window but is still this ability resolving. A freshly
		// accepted optional trigger is counted at resumeResolution's
		// "optional" arm instead. The eligibility check is on the RESOLVED
		// line's own param (rt), not on the source's other lines: a mixed-line
		// carrier (Cosmic Crucible's mandatory Main1 mana trigger, no
		// ResolvedLimit$) resolving first must not spend its sibling's limit.
		if triggered {
			if _, limited := resolvedLimitValue(rt); limited {
				e.noteTriggerResolved(o.Source)
			}
		}
		// Cumulative upkeep is an ordinary trigger through placement, but its
		// age/payment resolution needs rules' cost machinery. Mana Vault's
		// triggered Untap is the one ordinary effect shape authorized to use
		// that window; unrelated Cost$-bearing trigger effects retain their
		// established executor semantics. ImmediateTrigger joins Untap: its AB
		// shape is Forge's "you may pay <Cost$>. When you do, ..." idiom (Speed,
		// Young Avenger's TrigImmediateTrig -- the only repo-deck carrier), so
		// the ordinary triggered-cost window poses the pay/decline ask before
		// the body runs; a decline leaves the body unexecuted exactly as CR
		// 603.5's "when you do" promises. The DB shape's optional payment stays
		// the UnlessCost$ gate's (effects.unlessProceed); a plain Cost$ on a DB
		// ImmediateTrigger remains the established free-executor semantics.
		//
		// A trigger effect whose Cost$ carries a Draw component joins them: the
		// corpus's Draw<X/Spec> family (Champion of Wits' "you may draw cards
		// equal to its power. If you do, discard two cards" -- Cost$ Draw<X/You>)
		// is the same "you may pay; when you do" idiom, and without the window
		// the body ran for free and no draw happened. The window's pay arm
		// settles the draw, the decline arm leaves the body unexecuted.
		//
		// trigcost1: the gate is no longer an API allowlist. ANY Cost$-bearing
		// trigger body enters the window (Kalastria Highborn's `Cost$ B`,
		// Elenda and Azor's `Cost$ PayLife<4>`, ...): Forge's Cost$ on a trigger
		// body is the "you may pay; if you do" idiom, so a body that never asks
		// executes for free. The window keeps the split -- Priceable cost offers
		// a real pay, everything else lands decline-only. Mandatory-prefixed
		// costs and Mana/CopySpellAbility bodies are carved out inside
		// triggerBodyNeedsCostWindow.
		if o.Ability.API == "CumulativeUpkeep" {
			e.startCumulativeUpkeep(id, o.Source, o.Ability)
			return
		}
		// Echo (kw:Echo, CR 702.35a) is the same keyword-expansion shape: an
		// ordinary Phase trigger whose DB$ Echo body needs rules' payment
		// window and the pay-or-sacrifice election (rules/echo.go). The
		// intervening-if was already applied at trigger time (triggerMatches's
		// Echo$ branch), so everything reaching here is owed.
		if o.Ability.API == "Echo" {
			e.startEcho(id, o.Source, o.Ability)
			return
		}
		if _, triggered := e.triggerForAbilityObject(id, o); triggered &&
			e.triggerBodyNeedsCostWindow(o.Ability) {
			e.startTriggeredEffectCost(&resumePoint{kind: "effect_cost", obj: id, sa: o.Ability}, o.Source)
			return
		}
		// The ability-cast copy family (abcopy1): an AB$ CopySpellAbility
		// execute carrying a real Cost$ (Rings of Brighthearth {2}, Kurkesh
		// {R}, Battlemages' Bracers {1}, Chandra's Regulator {1}) must never
		// copy for free. When the firing trigger's context carries an event
		// role -- the activation role TriggerAbility, or the spell arm's
		// TriggerCard (a SpellCast fires on PutOnStack, whose Obj IS the spell;
		// no ability wrapper is minted) -- route the same pay/decline window
		// the Untap/ImmediateTrigger shapes use: a decline leaves the trigger
		// unexecuted, a pay charges the cost and then runs the copy. An
		// unpriceable cost (Verrak's PayLife<X>, Mica's Sac<1/Artifact>) poses
		// the ask but offers no answerable "pay" -- the ParseUnlessCost
		// hard-decline convention. A context-less synthetic push (no role)
		// keeps the free-executor semantics.
		tc := e.triggerContexts[id]
		if _, triggered := e.triggerForAbilityObject(id, o); triggered &&
			o.Ability.API == "CopySpellAbility" &&
			o.Ability.Params["Cost"] != "" &&
			(tc.TriggerAbility != 0 || tc.TriggerCard != 0) {
			e.startTriggeredEffectCost(&resumePoint{kind: "effect_cost", obj: id, sa: o.Ability}, o.Source)
			return
		}
		// The ability object itself has no Face, so its SVar table (needed
		// for Num's SVar indirection, e.g. Goblin Piledriver's "NumAtt$ +X")
		// comes from the permanent that granted it (o.Source) instead.
		// Printed face SVars are static card-script text, but a granted
		// trigger's owner may be a different card (or its grant may have ended).
		// For those wrappers the recorded line carries its owning SVar table.
		// A source that has since left the battlefield
		// (or ceased to exist) has nothing to read here and degrades to a
		// nil SVar table, same as before this ability object existed at
		// all, rather than panicking.
		//
		// A mutated pile (CR 702.140d) makes "the source's current Face" the
		// wrong table for an UNDER-card's ability: Face() on a pile is always
		// its TOP card, whose SVar table the under-card's body never meant --
		// Huntmaster Liger mutated under a Grizzly Bears read the Bears' (
		// empty) table for its own "NumAtt$ +X | SVar:X:Count$TimesMutated"
		// and pumped by 0. The owning face is the one that carries the
		// resolving trigger, which findTriggerForAbilityFace recovers by the
		// compiled trigger pointer -- the whole reason MergedTriggerPush mints
		// f.Triggers[i].Effect rather than a freshly parsed SVar body. An
		// ordinary trigger finds its own (top) face there, so its table is
		// unchanged, and an activated ability finds no trigger at all and
		// falls through to Face() exactly as before.
		var svars map[string]string
		if src := e.G.Obj(o.Source); src != nil {
			if _, mf, ok := e.findTriggerForAbilityFace(o.Source, o.Ability); ok && mf != nil {
				svars = mf.SVars
			} else if mf, ok := e.pileFaceForSA(o.Source, o.Ability); ok && mf != nil {
				// An activated ability of a MUTATED pile (CR 702.140d): o.Ability
				// is the under-card's SA, so the table its body reads is the
				// under-card's own -- Porcuparrot's `NumDmg$ X` resolves X from
				// the pile's Count$TimesMutated on ITS face, not the top card's.
				svars = mf.SVars
			} else if sf := src.Face(); sf != nil {
				svars = sf.SVars
			}
		}
		if owned, ok := e.triggerLineSVars[id]; ok {
			svars = owned
		}
		// Ruling T20-b: Source must be o.Source (the permanent that has this
		// ability), not id (the transient stack-object wrapper) -- Defined$
		// Self, the most common Defined$ value in real trigger scripts,
		// resolves to Ctx.Source, and a wrapper ID means "Self" refers to a
		// stack object with no Face() that leaves play the instant this
		// resolves, so the effect would silently apply to nothing. The SVar
		// lookup two lines above already gets this right by reading from
		// o.Source; this was a one-line inconsistency, not a second design.
		ctx := &effects.Ctx{Source: o.Source, Controller: o.Controller,
			Targets: targets, ModeTargets: charmModeTargets, Remembered: e.resolvingRemembered(o), Captured: o.Remembered, TriggerContext: e.triggerContexts[id],
			// Forge's Count$ResolvedThisTurn reads the per-ability tally the
			// Resolve event's Apply folded: the count INCLUDES this resolution,
			// because the Resolve event is emitted above before this Ctx is
			// built (the Sephiroth "if this is the fourth time" gate).
			ResolvedThisTurn:    e.resolvedAbilityTally(o),
			ActivationsThisTurn: e.activationsThisTurnFor(o.Source, o.Ability),
			// An Effect-created delayed trigger body resolves under the Effect's
			// source-scoped frame (queued by rules' delayed-trigger fire), so the
			// one-shot self-exile idiom it may run ends the Effect. Zero for every
			// ordinary printed trigger.
			EffectFrame: e.triggerEffectFrames[id],
			// The resolving stack-object wrapper: ValidStack's otherAbility
			// exclusion (Ulalek's sub-copy) anchors here, not on Source --
			// Source is the source permanent (Ruling T20-b), which is not on
			// the stack and would exclude nothing.
			ResolvingObj: id,
			// alltargeted1: the cast flow's pre-asked SubAbility$ target
			// answers, consumed line by line by chosenTargetsFor.
			SubPreAsk: e.castSubTargets[id]}
		// CR 702.49b: a K:Ninjutsu permanent enters attacking the same player
		// (planeswalker or battle) the returned creature was attacking. The
		// activator captured that defender when the Return cost was paid
		// (rules/cast.go's returncost arm) and it rides the AbilityPush event's
		// IDs, which events.Apply folded into o.Remembered as a player target.
		// Re-bind it here so effects/zone.go's Attacking$ True rider (which
		// reads Ctx.DefendingPlayer) places the permanent against the right
		// defender. Only a ninjutsu activation carries the tag, so no other
		// resolution's Remembered player is reinterpreted as a defending
		// player.
		if ab := o.Ability; ab != nil && saHasKeyword(ab, "Ninjutsu") {
			for _, rem := range o.Remembered {
				if rem.IsPlayer {
					ctx.DefendingPlayer = rem
					continue
				}
				// The AbilityPush IDs carry the planeswalker/battle object as a
				// real id after the player (events.Apply's rememberedFrom decodes
				// it to a {Obj} target). Surface it so the Attacking$ True rider
				// can place the permanent attacking the same object (CR
				// 702.49b). An unrelated remembered object (none exists on a
				// ninjutsu activation) would leak here, but only a ninjutsu
				// activation carries the tag and only the capture writes a
				// non-player id.
				if rem.Obj != 0 {
					ctx.DefendingBattle = rem.Obj
				}
			}
		}
		// The SA whose targeting the placement ask actually offered, not
		// blindly the resolving SA: for a non-modal ability that is the outer
		// SA's own ValidTgts$ (pushTrigger's askTarget), for a modal one it is
		// the first target-bearing CHOSEN MODE's sub -- handleModes' placement
		// branch asks the mode sub and skips the outer ask entirely (a Charm's
		// ValidTgts$ lives inside its modes, Kami of Restless Shadows'
		// RaiseScoundrel). Deriving the marker from the outer SA alone left
		// the modal shape unmarked, so a Min-0 mode target the chooser elected
		// ZERO of was re-posed by effChangeZone's mid-resolution ask at
		// resolution -- the exact duplicate-ask defect the marker exists to
		// stop.
		offeredSA := offeredTargetSA(o, svars)
		if offeredSA != nil {
			ctx.TargetsOffered = true
			ctx.OfferedSA = offeredSA
		}
		if lki, ok := e.triggerLKI[id]; ok {
			ctx.LKI = lki.object
			ctx.LKIPower, ctx.LKIToughness, ctx.LKIPTValid =
				lki.power, lki.toughness, lki.ptValid
		}
		// CR 107.3i: X is the value the activator chose for a Cost$ carrying
		// {X} (recorded on the ability stack object by commitCast's CastInfo,
		// emitted right after the AbilityPush). Zero for a trigger, which was
		// never paid an X -- and for a trigger CR 107.3m rebinds X to the
		// spell that became the permanent (an ETB trigger) or the spell the
		// trigger fired on (a cast/magecraft trigger), which triggerPaidX
		// reads off the causing event's card.
		ctx.X = o.X
		ctx.XAnnounced = stackXAnnounced(o) || ctx.X != 0
		if ctx.X == 0 {
			ctx.X = e.triggerPaidX(id, o)
		}
		// A cost-paid sacrifice carried its objects' LKI snapshot on the
		// engine (rules/cast.go commitCast), keyed by this stack object id;
		// load it so the ability's Sacrificed$<Property> heads resolve against
		// what it sacrificed. Mirror of triggerContexts: engine-only.
		ctx.Sacrificed = e.sacrificedLKI[id]
		ctx.Exiled = e.castExiled[id]
		ctx.Revealed = e.castRevealed[id]
		if link, ok := e.sourceLifelinkLKI[id]; ok {
			ctx.SourceLifelinkLKI = link
			ctx.SourceLifelinkLKIValid = true
		}
		if controller, ok := e.sourceControllerLKI[id]; ok {
			ctx.SourceControllerLKI = controller
			ctx.SourceControllerLKIValid = true
		}
		if lki := e.damageSourceLKI[id]; lki != nil {
			ctx.DamageSourceLKI = cloneDamageSourceLKI(lki)
		}
		effects.SetSVars(ctx, svars)
		// The Evolve keyword's own counter trigger (CR 702.99a) is identified
		// so its resolution can announce the completed evolve action (CR
		// 702.99b, trig:Evolved) once the counter lands below. Capturing the
		// pre-resolution +1/+1 count is what keeps a replaced or skipped
		// placement from firing the mode: only a real increase counts.
		var evolveWatch bool
		var evolveCountersBefore int32
		if triggered && rt.Params["Evolve"] != "" {
			if src := e.G.Obj(o.Source); src != nil {
				evolveWatch = true
				evolveCountersBefore = src.Counter("P1P1")
			}
		}
		// CR 603.3c: the mode choice was announced at placement (pushTrigger
		// asked KModes and handleModes recorded the answer into ChosenModes).
		// Pre-seeding Ctx.Modes makes effCharm take its re-entry branch and
		// run exactly the chosen modes rather than asking again at
		// resolution. Nil for a non-modal trigger, for an activated ability,
		// and for any trigger the placement ask never reached.
		ctx.Modes = o.ChosenModes
		e.damaging = o.Source
		e.contChain = e.contChain[:0]
		e.repeatReported = nil
		e.contChainOwners++
		e.endTurnRequested = false
		effects.Resolve(e, ctx, o.Ability)
		e.contChainOwners--
		e.damaging = 0
		if e.endTurnRequested {
			e.finishEndTurn()
			return
		}
		// CR 702.99b (task trig:Evolved): when this resolving ability is the
		// Evolve keyword's own counter trigger (its line carries Evolve$ True)
		// and it actually put the +1/+1 counter, announce the completed evolve
		// action so a sibling T:Mode$ Evolved ability on the same creature
		// fires. The marker is emitted AFTER the body so it names only an
		// evolve whose counter really landed: a source that left the
		// battlefield before resolution (effPutCounter skips a non-battlefield
		// recipient) places nothing and is not an evolve. A suspension
		// (e.resume != nil) is left to the resumed pass, which reaches this
		// same tail again. The counter read is the body's own P1P1 kind
		// (cards/kw_evolve.go's CounterType$ P1P1).
		if evolveWatch && e.resume == nil {
			if src := e.G.Obj(o.Source); src != nil && src.Counter("P1P1") > evolveCountersBefore {
				e.emit(events.Event{Kind: events.Evolved, Obj: o.Source, Player: o.Controller})
			}
		}
		if e.resume != nil {
			// A placement-announced modal ability can reach a nested ask during
			// this initial pass. Preserve every enclosing continuation exactly as
			// resumeResolution does for a nested ask reached on re-entry.
			e.resume.outer = e.buildContinuationChain(e.contChain, id, nil)
			e.contChain = e.contChain[:0]
			// A mid-resolution ask (M2d-2): the effect that asked has set a
			// decision pending and recorded a resume point. The object stays
			// on the stack waiting for the answer -- entering the exile exit
			// below would discard it mid-resolution. The answered decision
			// re-enters the suspended effect through resumeResolution
			// (rules/resolution.go), which runs the rest of this same tail.
			return
		}
		e.contChain = e.contChain[:0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
		e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this resolved "+
			"ability's own move off the stack without relocating it anywhere; sent to exile "+
			"instead of re-resolving forever")
		return
	}

	f := o.Face()
	sa := f.SpellAbility()
	// Bestow (CR 702.114a): a cast paid for with the bestow cost resolves
	// as the synthesized Aura attach spell -- the creature face itself has
	// no SP -- so the whole ordinary Aura tail below runs unchanged:
	// effAttach emits events.Attach while the spell is still on the stack,
	// and moveResolvedOffStack enters the permanent attached (the entry
	// keeps an Attach set on the stack). The flag is the pay-time CastInfo
	// provenance modeFlags("bestowed") rode.
	if o.CastFlags&state.FlagBestowed != 0 {
		sa = bestowedAttachSA()
	}
	// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a via
	// CR 708.4): a face-down cast resolves as a vanilla creature spell --
	// the face-down spell has no printed spell abilities and no targets.
	// The provenance flag is the pay-time CastInfo's modeFlags(fam) family
	// bit; moveResolvedOffStack re-carries the face-down entry marker so
	// the permanent enters face down (CR 708.5's 2/2 creature, the cloak
	// marker's ward {2} for Disguise). A stack copy inherits the flag (the
	// FlagFused way -- not a CastProvenanceFlags bit) and resolves the
	// same vanilla way.
	if o.CastFlags&(state.FlagMorphed|state.FlagMegamorphed|state.FlagDisguised) != 0 {
		sa = nil
	}
	// Mutate (CR 702.140d): a spell cast for its mutate cost does not become
	// an independent permanent. It merges into its target, so resolution is
	// diverted BEFORE the ordinary spell-block tail (which would move it to
	// the battlefield): resolveMutate emits the Mutate fold, which parks this
	// object off the stack. A mutate card carries no SP, so the spell block
	// below would resolve nothing anyway.
	if o.CastFlags&state.FlagMutated != 0 {
		e.resolveMutate(o, o.Targets)
		return
	}
	// Fuse (CR 702.101b): a fused split spell is one spell whose BOTH halves'
	// spell abilities resolve in sequence. The card stays at its front face;
	// resolveFused owns the per-half target recheck, the Resolve event, the
	// Ascend blessing and the off-stack move.
	if o.CastFlags&state.FlagFused != 0 {
		// A half that suspended on a mid-resolution ask hands back the
		// continuation (the rest of that half plus a fuse-rest frame for any
		// unrun half); link it onto the ask's fresh resume point here, the
		// resolution machinery's own write (ruling T21-e).
		if cont, suspended := e.resolveFused(o); suspended && e.resume != nil {
			e.resume.outer = cont
		}
		return
	}
	targets := o.Targets
	charmModeTargets, charmFlatTargets, charmHandled := e.recheckCharmTargets(o)
	if charmHandled {
		targets = charmFlatTargets
	}
	// targetSA is the SA whose ValidTgts$ the cast-flow target ask offered
	// (the modal declaration for a Charm, the SpellAbility itself otherwise);
	// hoisted so the resolution ctx can carry the TargetsOffered marker and
	// the mvts1 pre-ask's OfferedSA skip.
	targetSA := modalTargetSA(f, sa, o.ChosenModes)
	// An overloaded spell affects the matching set as it resolves, never as
	// targets chosen during announcement. This fresh non-target census means
	// protection/hexproof do not apply and objects entering or changing
	// controller in response are included correctly. Effect primitives keep
	// their generic Ctx.Targets recipient API; only the source of that list is
	// different.
	overloaded := o.CastFlags&state.FlagOverloaded != 0
	if overloaded && sa != nil {
		if targetSA != nil {
			for _, cand := range e.affectedCandidates(o.Controller, id, id, targetSA) {
				if cand.kind == "player" {
					targets = append(targets, state.Target{Player: cand.player, IsPlayer: true})
				} else {
					targets = append(targets, state.Target{Obj: cand.obj})
				}
			}
		}
	}
	// The spell branch's twin of the ability branch's composition: a modal
	// root carries no pre-asked sub answers, so this returns (0, 0) exactly
	// where charmHandled owns the recheck instead.
	subChosen, subLegal := e.recheckCastSubTargets(id, sa, o.Controller, id)
	if sa != nil && !overloaded && !charmHandled {
		// A modal spell's target declaration lives on its announced mode SVar,
		// not the outer Charm SA. Use the same selected declaration targetAsk
		// used during CR 601.2c, so its targets receive the ordinary CR 608.2b
		// legality recheck at resolution. (targetSA is hoisted above.)
		// Fix round 2 (re-review N1), the same correction as the ability
		// branch above, and the one that was actually reachable. Widening the
		// departed-player release hook in fix round 1 turned a stall into a
		// spell that RESOLVES with no targets at all: the caster is
		// eliminated while their target decision is outstanding, the hook
		// clears it so the match can continue, and the spell is left on the
		// stack with Targets nil. Under the old `len(targets) > 0` gate that
		// skipped the recheck and ran the whole script -- untargeted riders
		// included -- gaining the ELIMINATED caster 7 life in the re-review's
		// own reproduction. CR 608.2b counters a spell with no legal targets;
		// CR 800.4a says a departed player's spell ceases to exist. Neither
		// permits it to resolve.
		// Requirement N2, the same exemption as the ability branch: an
		// untargeted-with-Min-0 spell resolves rather than fizzling.
		if spec := targetSA.Params["ValidTgts"]; spec != "" && !(e.resolvedTargetMin(o.Controller, id, targetSA, 0) == 0 && len(targets) == 0) {
			legal := e.legalTargets(targets, targetSA, targetZones(targetSA), o.Controller, id, id)
			if len(legal) == 0 && subLegal == 0 {
				// CR 608.2b: every target became illegal. This spell does
				// not resolve -- no Resolve event, no script runs -- it goes
				// straight to its normal resting place, the same zone it
				// would reach after an ordinary resolution (an Aura instead
				// reaching the battlefield would be wrong here: CR 704.5m's
				// "nothing legal to attach to" is exactly this case for
				// that spell shape, and the resting zone is where it
				// belongs) -- exile instead of the graveyard for one cast
				// via Flashback (CR 702.32b).
				rest := spellFizzleZone(o)
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZStack, To: rest, Text: "fizzled: no legal targets remain"})
				e.ensureLeftTheStack(id, rest, "a replacement fully discarded this "+
					"spell's 'fizzled: no legal targets' move without relocating it anywhere; "+
					"sent to its resting zone instead of re-resolving forever")
				return
			}
			targets = legal
		}
	}
	if len(targets) == 0 && subChosen > 0 && subLegal == 0 {
		rest := spellFizzleZone(o)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: rest,
			Text: "fizzled: no legal targets remain"})
		e.ensureLeftTheStack(id, rest, "all cast-time sub targets became illegal")
		return
	}
	e.emit(events.Event{Kind: events.Resolve, Obj: id, Text: f.Name})
	// Ascend (CR 702.131a, the non-permanent case): an instant/sorcery with
	// K:Ascend grants its controller the blessing BEFORE the spell's own
	// body and condition checks read the latch (Forge's "do blessing there
	// before condition checks"; rules/ascend.go). Permanent faces are
	// excluded -- their grant is the emit-side continuous scan.
	e.grantSpellBlessing(o, f)
	// CR 702.168b: a promised gift on an INSTANT OR SORCERY resolves BEFORE
	// the spell's other effects (the gift's own "before its other effects").
	// The body is the face's GiftAbility SVar; it is spliced as the HEAD of
	// the spell's own chain so the ordinary suspension/continuation machinery
	// handles a mid-gift ask and then runs the rest of the spell, and it
	// shares the spell's Ctx so Defined$ Promised / TokenOwner$ Promised read
	// the promise. The events.GiveGift marker is emitted just before the gift
	// body runs. The splice is a SHALLOW COPY of the resolved gift SA, never
	// a mutation of the card's parsed table.
	//
	// A PERMANENT's promised gift is NOT spliced here: CR 702.168c makes it a
	// "when this permanent enters" triggered ability, queued after entry by
	// altCostEnter and pushed onto the stack by pushTrigger -- so it can be
	// responded to and ordered against the card's own printed ETB.
	resolveSA := sa
	if o.CastFlags&state.FlagPromisedGift != 0 && !f.IsPermanent() {
		if gift := cards.ResolveSVar(f.SVars, "GiftAbility"); gift != nil {
			e.emit(events.Event{Kind: events.GiveGift, Player: o.Controller, Obj: id})
			head := *gift
			tail := &head
			for tail.Sub != nil {
				tail = tail.Sub
			}
			tail.Sub = sa
			resolveSA = &head
		}
	}
	// Cipher (CR 702.99a): the encode is the resolving spell's own tail
	// instruction, not a triggered ability. rules/cipher.go appends it to a
	// copy of the chain so the ordinary suspension/continuation machinery runs
	// it after the spell's effects and before the move off the stack below. A
	// stack COPY (o.IsCopy) is not a card, so it cannot be encoded.
	if !o.IsCopy {
		if withCipher := cipherTailFor(f, resolveSA); withCipher != nil {
			resolveSA = withCipher
		}
	}
	if resolveSA != nil {
		e.damaging = id
		ctx := &effects.Ctx{Source: id, Controller: o.Controller, Targets: targets,
			ModeTargets: charmModeTargets, ResolvingObj: id,
			// alltargeted1: the cast flow's pre-asked SubAbility$ target
			// answers, consumed line by line by chosenTargetsFor. Disjoint
			// from ModeTargets: a modal root is never pre-asked.
			SubPreAsk: e.castSubTargets[id]}
		// Same marker as the ability branch: the cast-flow target ask
		// (targetAsk's targetSA) offered exactly this spell's targeting.
		if targetSA != nil && strings.TrimSpace(targetSA.Params["ValidTgts"]) != "" {
			ctx.TargetsOffered = true
			ctx.OfferedSA = targetSA
		}
		// CR 107.3i: X is the value the caster chose for the mana cost's {X},
		// recorded on the stack object by commitCast's CastInfo (the same
		// value the ETB/replacement path already reads as o.X). Without this
		// every numeric parameter that reads the paid X (TokenAmount$ X,
		// Amount$ X, SVar:X:Count$xPaid, ...) resolves to 0 and the spell's
		// body does nothing -- Entreat the Angels resolved to the graveyard
		// having created zero Angels.
		ctx.X = o.X
		ctx.XAnnounced = stackXAnnounced(o)
		// Same as the ability branch: carry the sacrifice LKI (engine-keyed)
		// onto resolution so Sacrificed$<Property> heads resolve against what
		// this spell sacrificed.
		ctx.Sacrificed = e.sacrificedLKI[id]
		ctx.Exiled = e.castExiled[id]
		ctx.Revealed = e.castRevealed[id]
		effects.SetSVars(ctx, f.SVars)
		// CR 601.2b: a modal spell's choice was recorded on its proposal before
		// targets and payment. Pre-seeding Modes makes effCharm execute exactly
		// that announcement instead of posing its old resolution-time ask.
		ctx.Modes = o.ChosenModes
		e.contChain = e.contChain[:0]
		e.repeatReported = nil
		e.contChainOwners++
		e.endTurnRequested = false
		effects.Resolve(e, ctx, resolveSA)
		e.contChainOwners--
		e.damaging = 0
		if e.endTurnRequested {
			e.finishEndTurn()
			return
		}
		if e.resume != nil {
			// The cast-announced outer mode may itself contain an asking effect.
			// This is an initial resolution pass rather than a resume re-entry,
			// but its enclosing SubAbility continuations have the same lifetime.
			e.resume.outer = e.buildContinuationChain(e.contChain, id, nil)
			e.contChain = e.contChain[:0]
			// A mid-resolution ask (M2d-2): same as the ability branch above
			// — the resolution is suspended with the object still on the
			// stack, and the answered decision re-enters it through
			// resumeResolution instead of this tail.
			return
		}
		e.contChain = e.contChain[:0]
	}
	e.moveResolvedOffStack(o)
}

// spellRestZone is where a spell goes AFTER IT RESOLVES. Buyback is a
// resolution replacement (CR 702.27a), not a replacement for being
// countered, so only this resolved-spell helper may return it to hand.
func spellRestZone(o *state.Object) state.Zone {
	if o != nil && (state.ExilesLeavingStack(o.CastFlags) ||
		o.IsCopy || hasParadigm(o) || o.CastFlags&state.FlagAdventure != 0 ||
		o.CastFlags&state.FlagReplaceGraveyard != 0 ||
		o.CastFlags&state.FlagRebound != 0) {
		return state.ZExile
	}
	if o != nil && o.CastFlags&state.FlagBuyback != 0 {
		return state.ZHand
	}
	return state.ZGraveyard
}

// spellFizzleZone is the resting place when a spell never resolved. Flashback,
// Harmonize, Aftermath, the ReplaceGraveyard$ Play rider and copies still use
// exile, but Buyback does not apply and the card reaches its owner's graveyard.
func spellFizzleZone(o *state.Object) state.Zone {
	if o != nil && (state.ExilesLeavingStack(o.CastFlags) ||
		o.IsCopy || o.CastFlags&state.FlagReplaceGraveyard != 0) {
		return state.ZExile
	}
	return state.ZGraveyard
}

// ensureLeftTheStack is CR 608.2m housekeeping, not a further game action:
// every one of resolveTop's five exits (the permanent-ETB Move above, the
// instant/sorcery Move above, and the three fizzle/ability-resolution Moves
// earlier in this function) emits a MoveZone meant to take id off the stack
// for good. If a ValidCard$-matching, ReplacementResult$-absent (this
// build's "Replaced") R:Event$ Moved replacement on some OTHER object
// intercepts that specific MoveZone and its own ReplaceWith$ does not itself
// relocate the card (e.g. it only gains life), nothing else ever removes id
// from e.G.Stack: the next priority round finds the same object on top and
// resolves it again, forever. Note this is NOT the shipped corpus's own
// Rest in Peace / Dryad Militant / Leyline of the Void shape: that shape's
// ReplaceWith$ names Defined$ ReplacedCard, which effects/context.go's
// Defined DOES model, and with ChangeZone's Origin$ All parsed as a real
// wildcard (effects/zone.go's ParseZones) it genuinely relocates the card
// to exile -- so a working Rest-in-Peace-shaped replacement takes the object
// off the stack itself and the guard never engages. The guard is for the
// replacements whose ReplaceWith$ truly leaves the card where it was.
//
// Originally added by Task 29 (Ruling T26-a) for the permanent-ETB exit
// alone; the final whole-branch review (Critical C1) measured the identical
// hazard, independently reachable, on the other four exits -- a two-card
// deck (an instant or an ability-granting permanent, plus a Rest-in-Peace-
// shaped enchantment already on the battlefield) reliably hung a real match
// at 100 000+ intents, never terminating, on turn 1. 24 corpus cards this
// build's own coverage gate already calls fully playable carry the shape
// (Rest in Peace, Dryad Militant, Leyline of the Void, ...), so this is
// reachable from ordinary deck-building, not adversarial card text. This
// helper is what makes every exit safe with one implementation instead of
// five copies of the same six lines.
//
// Held under applyingReplacement (saved and restored, not just set, in case
// a future caller ever reaches here already inside one) for the same reason
// the original ETB guard was (Task 29 review finding I-1): "ceases to
// exist" is engine bookkeeping, not a game event a card's own replacement
// gets to intercept a second time. why becomes the Note's Text, tailored by
// each call site to name which of the five moves was actually being
// guarded.
func (e *Engine) ensureLeftTheStack(id state.ObjID, to state.Zone, why string) {
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		return
	}
	// fx44: a SUSPENDED replacement is legitimately still on the stack,
	// waiting for its answer — the ask it posed decides where the object goes
	// (Mox Diamond's ETB replacement parks the card exactly here). The guard
	// must distinguish "the replacement finished and moved nothing" (the
	// ordinary re-resolve hazard below, where e.resume is nil) from "the
	// replacement is waiting for an answer" (where the object must stay put
	// so resumeResolution can act on it). Deferring the park while suspended
	// leaves the object on the stack; the answered resume performs the real
	// move, and finishResumption's own o.Zone != state.ZStack check skips
	// this guard entirely once that move has happened.
	if e.Suspended() {
		return
	}
	saved := e.applyingReplacement
	e.applyingReplacement = true
	e.emit(events.Event{Kind: events.Note, Obj: id, Text: why})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: to})
	e.applyingReplacement = saved
}

// legalTargets is CR 608.2b's recheck, applied at resolution: the subset of
// targets that are still legal right now. An object target must still be in
// a zone the spec legitimately targets -- zones, computed by the same
// targetZones the offering askTarget used, is what no-longer-hardcodes the
// battlefield -- because matchesBase's own bare-type predicates (effects/
// filter.go), e.g. "Creature", read printed types straight off the Face with
// no zone check of their own, so a creature that died and is sitting in a
// graveyard would otherwise still look like a match. The old form required
// o.Zone == state.ZBattlefield, which made every non-battlefield legal
// target offered by askTarget (a stack spell for a counterspell; a
// Graveyard card for the Snapcaster shape) fizzle at resolution. It still
// satisfies spec, via the same effects.MatchesSpec call askTarget used to
// offer it as an option in the first place: no self-relative source,
// matching askTarget's own simplification, so a spec that would filter on
// Self/Other is exactly as (im)precise here as it was at cast time. A player
// target is legal for as long as they are still in the game AND still match
// the spec's player filter (playerTargetSpecMatches -- the shared
// MatchesPlayerSpecFrom grammar plus the trigger-role alternatives the ask's
// own TriggerContext carries -- from the controller's perspective, against
// the same source the offer judged) -- the same judge the offer applies, so
// offer and recheck cannot disagree (the one-definition rule). A target
// whose qualifier the filter cannot evaluate was never offered and is
// rejected here too, fail closed.
// recheckCastSubTargets uses the same legality judge as the root target at
// resolution. Answers remain in the map through suspended re-entries, so a
// body resumed after an unrelated choice still uses its announced target.
// Preserve an answered-empty entry as a non-nil slice so the effects walk
// does not mistake it for an outstanding mid-resolution ask.
func (e *Engine) recheckCastSubTargets(id state.ObjID, root *cards.SA, controller state.PlayerID, source state.ObjID) (chosen, legal int) {
	answers := e.castSubTargets[id]
	if len(answers) == 0 {
		return 0, 0
	}
	for _, sa := range e.collectSubTargetPreAsks(root) {
		ts, ok := answers[sa.Line]
		if !ok {
			continue
		}
		chosen += len(ts)
		kept := e.legalTargets(ts, sa, targetZones(sa), controller, source, id)
		legal += len(kept)
		if kept == nil {
			kept = []state.Target{}
		}
		answers[sa.Line] = kept
	}
	return chosen, legal
}

func (e *Engine) legalTargets(targets []state.Target, sa *cards.SA, zones []state.Zone, you state.PlayerID, source state.ObjID, self state.ObjID) []state.Target {
	spec := ""
	if sa != nil {
		spec = sa.Params["ValidTgts"]
	}
	var legal []state.Target
	// The resolution recheck, unlike a target offer, has this stack object's
	// Targets available. Targeted* predicates may read precisely this binding;
	// setting it here keeps their self-reference unavailable at announcement.
	// The source rides in too, the same object askTarget's own offer filter
	// sees (candidatesFor's sc.Source): a source-reading predicate
	// (CanEnchantEquippedBy -- Mantle of the Ancients' recheck) judges the
	// chosen target at resolution exactly as the offer judged it at
	// placement, Critical C2's one-definition rule. Before this, the recheck
	// built its SpecContext with source 0 and every source-reading predicate
	// failed closed there -- a target the placement offer had just certified
	// fizzled at resolution.
	sc := e.targetSpecContext(source, self, you)
	defer e.releaseSpecEnv()
	sc.ResolutionTargets = targets
	sc.Resolving = true
	// TargetingPlayerControls$ (tpc1): the restriction's answering seat is
	// resolved through the same derivation the offer census used, with the
	// answered-ask record taking precedence -- it is keyed by self, the
	// RESOLVING stack object the answer was recorded on, so a selection pin
	// consumed since the ask cannot change the answer here. An SA whose
	// record is missing (a recheck that never had its own ask -- a copy's
	// inherited targets, a modal group) falls back to the derivation; with
	// the multi-opponent selection unrecoverable that is the union over the
	// controller's living opponents, the same shape the offer census used
	// while the selection was owed.
	tpChooser, tpState := e.targetControlsChooser(you, source, sa)
	if tpState != tpNone {
		if rec, ok := e.tpCtlChooser[self]; ok && (sa == nil || sa.Line == "" || rec.line == "" || rec.line == sa.Line) {
			tpChooser, tpState = rec.player, tpResolved
		}
	}
	controllerProp := ""
	nonTriggeredController := false
	triggeredCardController := state.PlayerID(0)
	triggeredCardControllerOK := false
	if sa != nil {
		controllerProp = strings.TrimSpace(sa.Params["TargetsWithControllerProperty"])
		if strings.TrimSpace(sa.Params["TargetsWithDefinedController"]) == "NonTriggeredCardController" {
			nonTriggeredController = true
			triggeredCardController, triggeredCardControllerOK = effects.TriggeredCardController(e.G, sc.TriggerContext, sc.Remembered)
		}
	}
	// TargetsWithSharedCardType$ is the reference-relative sibling of the
	// controller predicate: the SAME sharedCardTypeAdmits the census post-filter
	// (filterTargetsWithSharedCardType) applies is judged here, so a target the
	// mid-resolution offer certified cannot fizzle the recheck and vice versa.
	// The key present with an unresolved reference (sharedRef 0) fails every
	// candidate closed; the key absent admits everything.
	hasSharedRef := false
	sharedRef := state.ObjID(0)
	var sharedWhitelist []string
	if ref := sharedCardTypeRef(sa); ref != "" {
		hasSharedRef = true
		sharedRef = e.sharedCardTypeReference(ref, source, sc)
		sharedWhitelist = sharedTypesWhitelist(sa)
	}
	for _, t := range targets {
		if t.IsPlayer {
			// CR 702.18 / CR 702.11 / CR 702.16c for players: a target that
			// GAINED player shroud, hexproof or protection between placement
			// and resolution is dropped here, exactly as the object arm below
			// drops a permanent that gained them -- the same judge the offer
			// (candidatesFor's player loop) applies, so offer and recheck
			// cannot disagree (the one-definition rule). Players have no zone:
			// no CR 604.3 gate is consulted on the player arm.
			if int(t.Player) < len(e.G.Players) && !e.G.Players[t.Player].Lost &&
				e.playerTargetSpecMatches(sc, spec, t.Player, you, source) &&
				!e.playerShroudBlocksTarget(t.Player) &&
				!e.playerHexproofBlocksTarget(t.Player, you, e.protectionSource(source)) &&
				!e.playerProtectedFrom(t.Player, e.protectionSource(source)) {
				legal = append(legal, t)
			}
			continue
		}
		// CR 115.5: the resolving spell or ability is an illegal target for
		// itself. self is the stack object being resolved (not source, which
		// for an ability is the source PERMANENT and so is a legal target of
		// its own ability -- e.g. a creature's "target creature" ability on
		// itself). A target chosen at cast time for a different spell -- a
		// different copy of the same card, or a permanent -- is unaffected.
		// This is why the exclusion is keyed on the resolving object id, and
		// mirrors askTarget's own withholding so the two sites always agree.
		if t.Obj == self {
			continue
		}
		// CR 702.16c: a permanent that became protected from the resolving
		// source's qualities since the target was chosen is no longer a legal
		// target, exactly as askTarget withheld it at cast time -- so a
		// previously-offered target that gained matching protection mid-race
		// is dropped from resolution too (CR 608.2b). The source is resolved
		// through protectionSource so an ability fizzling here judges "the
		// source" as its Source permanent, the same object askTarget's own
		// filter has now been made to see (Critical C2 -- one definition).
		// CR 702.14 rides the same recheck: a target that GAINED shroud
		// between placement and resolution (Lightning Greaves equipping in
		// response) is dropped here, and a target with no other legal target
		// left fizzles the whole spell/ability through the existing fizzle
		// machinery upstream of this recheck.
		if o := e.G.Obj(t.Obj); o != nil && zoneIn(o.Zone, zones) {
			// The offer post-filter's own predicate, the SAME definition: a
			// target that was offered under NonTriggeredCardController cannot
			// be inconsistent with this CR 608.2b recheck, and one whose
			// controller changed to the triggering controller (or that left
			// the battlefield) is dropped here exactly as it was withheld at
			// announcement.
			if nonTriggeredController && !nonTriggeredControllerAdmits(o, triggeredCardController, triggeredCardControllerOK) {
				continue
			}
			if o.Zone == state.ZStack && (sa == nil || !e.stackKindAdmits(
				stackTargetKindTokens(sa.Params["TargetType"]), e.stackObjKind(o), o, o.Controller, you)) {
				continue
			}
			// The cast-provenance split at the resolution recheck too
			// (wascastfrom): the token evaluates against the target's cast
			// log before the ordinary filter, so offer and recheck cannot
			// disagree about a spec carrying one.
			tspec, ok := e.castProvenanceAdmits(targetSpecForZone(spec, o.Zone), t.Obj, you)
			if ok && e.matchesSpec(tspec, t.Obj, sc) &&
				e.mentorAdmits(sa, source, t.Obj) &&
				(controllerProp == "" || e.targetControllerPropertyAdmits(controllerProp, t.Obj)) &&
				(!hasSharedRef || e.sharedCardTypeAdmits(t.Obj, sharedRef, sharedWhitelist)) &&
				(tpState == tpNone || e.tpControlsAdmits(o, you, tpChooser, tpState)) &&
				!(o.Zone == state.ZBattlefield && e.restrictionBlocksTarget(t.Obj, you)) &&
				!(o.Zone == state.ZBattlefield && e.shroudBlocksTarget(t.Obj)) &&
				!(o.Zone == state.ZBattlefield && e.hexproofBlocksTarget(t.Obj, you, e.protectionSource(source))) &&
				!e.protectedFrom(t.Obj, e.protectionSource(source)) {
				legal = append(legal, t)
			}
		}
	}
	// CR 608.2b / CR 601.2c: the per-controller targeting requirement is
	// rechecked on the surviving set too -- see narrowDifferentControllers.
	if sa != nil && strings.EqualFold(sa.Params["TargetsWithDifferentControllers"], "True") {
		legal = e.narrowDifferentControllers(legal)
	}
	if sa != nil && strings.EqualFold(sa.Params["TargetsWithSameController"], "True") {
		legal = e.narrowSameController(legal)
	}
	legal = e.narrowSetProps(sa, legal)
	x := int32(0)
	if o := e.G.Obj(self); o != nil {
		x = o.X
	}
	if capCMC, capped := e.maxTotalTargetCMC(you, source, sa, x); capped {
		total := 0
		for _, target := range legal {
			if target.IsPlayer {
				continue
			}
			if o := e.G.Obj(target.Obj); o != nil && o.Face() != nil {
				total += int(o.Face().ManaValue())
			}
		}
		if total > capCMC {
			return nil
		}
	}
	return legal
}

// targetSpecForZone preserves Forge's distinction between a battlefield
// permanent and a permanent card in another zone. Forge spells both with a
// `Permanent` base (Conduit of Worlds is a real `TgtZone$ Graveyard` example),
// while the general matcher correctly treats a bare Permanent as a battlefield
// object. Rewrite only the leading base token, retaining every qualifier, so
// all target offer and target-legality callers share this rule.
func targetSpecForZone(spec string, z state.Zone) string {
	if z == state.ZBattlefield || z == state.ZStack {
		return spec
	}
	if spec == "Permanent" {
		return "PermanentCard"
	}
	if len(spec) > len("Permanent") && spec[:len("Permanent")] == "Permanent" {
		next := spec[len("Permanent")]
		if next == '.' || next == '+' || next == ',' {
			return "PermanentCard" + spec[len("Permanent"):]
		}
	}
	return spec
}

// zoneIn reports whether z is one of the zones in the set.
func zoneIn(z state.Zone, zones []state.Zone) bool {
	for _, candidate := range zones {
		if candidate == z {
			return true
		}
	}
	return false
}

// resolveAbility walks an SA chain, running each API's implementation. svars
// is the resolving face's SVar table (nil for an ability-only stack object),
// which is what lets Num's SVar indirection and primitives like Charm and
// Repeat -- which run a sub-ability named by SVar rather than the
// auto-linked "SubAbility$" -- actually resolve something outside a test.
// resolveAbility resolves one ability body off the stack or as a mana
// ability's effect.
func (e *Engine) resolveAbility(source state.ObjID, controller state.PlayerID,
	targets []state.Target, sa *cards.SA, svars map[string]string) {
	e.resolveAbilitySacrificing(source, controller, targets, sa, svars, nil)
}

// resolveAbilitySacrificing is resolveAbility with the permanents an
// ABILITY'S OWN activation cost sacrificed (a <T>/Sac mana ability's
// Sac<...>, the Phyrexian Altar shape). They are bound to Ctx.Sacrificed so
// an Amount$/count SVar over Sacrificed$CardManaCost (Priest of Yawgmoth,
// Soldevi Adnate, Furgul Quag Nurturer) prices the object it just paid away
// rather than reading zero. Spell and triggered-ability callers bind the
// same list from their own captured LKI (rules/stack.go, rules/resolution.go);
// this is the mana ability path's one home for it.
func (e *Engine) resolveAbilitySacrificing(source state.ObjID, controller state.PlayerID,
	targets []state.Target, sa *cards.SA, svars map[string]string, sacs []state.ObjID) {
	ctx := &effects.Ctx{Source: source, Controller: controller, Targets: targets}
	for _, id := range sacs {
		ctx.Sacrificed = append(ctx.Sacrificed, state.SacrificedInfoOf(e.G, id))
	}
	// Forge's Count$ResolvedThisTurn: the same (source, root Ability$ body)
	// tally resolveTop's ability branch binds, so a DBTransform gated on the
	// fourth resolution of the turn reads it here too. Zero for a synthetic
	// direct resolution that never went through the stack (the map carries no
	// entry for it), the modelled-head zero the effects case gives.
	ctx.ResolvedThisTurn = e.resolvedAbilityTallyFor(source, sa)
	ctx.ActivationsThisTurn = e.activationsThisTurnFor(source, sa)
	// The caller supplies the chosen targets -- the announcement or placement
	// ask's answer -- so the generic ValidTgts$ pre-ask must not re-pose it
	// for an SA that declares targets (task mvts1).
	if sa != nil && strings.TrimSpace(sa.Params["ValidTgts"]) != "" {
		ctx.TargetsOffered = true
	}
	effects.SetSVars(ctx, svars)
	effects.Resolve(e, ctx, sa)
}

// Game, Emit, Rand and ShuffleLibrary satisfy effects.Host, which is how effects reach the
// engine without importing it. AddContinuous (layers.go), HasKeyword
// (layers.go) and Ask (resolution.go) round out the interface -- HasKeyword
// already existed for the layer system's own callers before effects.Host
// grew a method of the same name, and needed no change to satisfy it.
