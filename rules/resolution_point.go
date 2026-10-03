// resolution_point.go holds the resume-point data model: the resumePoint/repeatCursor/fusedRest/contFrame records the suspended mid-resolution asks are carried and cloned in.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// resumePoint is one suspended resolution: which continuation the pending
// decision's answer resumes ("modes" for a Charm modal pick, "unless_pay"
// for a CopySpellAbility may-pay, "discard" for a mid-resolution discard
// choice, "search" for a hidden-library KChoose, "dig" for a Dig
// look-and-take pick, "dig_arrange" for a Dig's ordered-bottom KArrange,
// "connive" for a Connive discard election, and "" for a pure outer
// continuation that carries no answer),
// which stack object's resolution is paused, and the exact sub-ability
// whose effect asked — or, for an outer continuation, the sub-ability to
// resume walking. `outer` is the continuation that must run after this
// point (and everything nested inside it) resolves: the rest of the chain
// the ENCLOSING effect was walking when it launched the nested resolution.
// A nil `outer` means this is the outermost point of the resolution, whose
// completion moves the object off the stack. Plain data, cloned by value
// (the *cards.SA is shared immutable card data, the same class
// Engine.Clone already shares everywhere).
func cloneClashResume(r *decision.ClashResume) *decision.ClashResume {
	if r == nil {
		return nil
	}
	return &decision.ClashResume{Players: append([]state.PlayerID(nil), r.Players...), Revealed: append([]state.ObjID(nil), r.Revealed...), Winner: r.Winner, Cursor: r.Cursor}
}

// `replacement` records whether the suspended resolution is running inside
// a replacement effect's ReplaceWith$ body (fx44). applyReplacements sets
// e.applyingReplacement while it resolves that body and resets it to false
// when the body returns — but a body that SUSPENDS at an ask returns before
// the answer arrives, so the flag is lost across the suspension. The resumed
// body then emits its own completion move with applyingReplacement false,
// and that move is re-intercepted by the SAME replacement it is the product
// of. Capturing the flag at ask time and restoring it for the whole resumed
// resolution closes that loop (see resumeResolution); it is the answer to
// the question "does applyingReplacement survive the suspension".
type resumePoint struct {
	kind        string
	obj         state.ObjID
	event       events.Event
	sa          *cards.SA
	outer       *resumePoint
	replacement bool
	// playFirst is the first card an answered Play began, re-bound as
	// Ctx.Play by the "play_resume" continuation (rules/play_queue.go) so
	// the re-entered effPlay's ForgetPlayed$ reads the same card.
	playFirst state.ObjID
	// replaced is the object the replaced event was about (Ctx.Replaced =
	// ev.Obj), captured at ask time when the ask is posed from inside a
	// replacement body. The resume rebuilds Ctx.Replaced (and Remembered =
	// [that object]) from it, so a ReplaceWith$ body whose completion move
	// is gated on Defined$ ReplacedCard / SVar:X Remembered$Amount finds its
	// subject after the suspension (fx44, Mox Diamond). Zero for an ordinary
	// (non-replacement) ask.
	replaced state.ObjID
	// replacedCards is the plural counterpart of replaced: the ordered
	// replaced-instruction batch (Ctx.ReplacedCards) captured at ask time
	// when the ask is posed from inside a Cascade replacement body. The
	// resume rebuilds Ctx.ReplacedCards from it, so a ReplacedCards.<qual>
	// hidden pick re-resolves against the same exiled batch after the
	// suspension (Averna, the Chaos Bloom). nil for every ordinary ask.
	replacedCards []state.ObjID
	// replacementTarget/replacementSource/replacementAmount are the in-flight
	// Damage event's own target/source/amount (e.replacingEvent), captured so
	// a DB$ ReplaceEffect body that asks mid-resolution can rebuild the same
	// ReplacementTarget/ReplacementSource/ReplacementAmount Ctx fields on
	// resume that Defined$ ReplacedTarget/ReplacedSource and friends read.
	// Zero/empty outside a Damage replacement's body.
	replacementTarget state.Target
	replacementSource state.ObjID
	replacementAmount int32
	// effectFrame is the Effect-created registration the asking body resolved
	// under (Engine.currentEffectFrame, published by effects.Resolve). It is
	// zero for every ordinary ask and non-zero only when the walk belongs to
	// an api:Effect body (rules' seedEffectReplCtx), so a ReplaceWith$ body
	// that suspends on a mid-resolution ask resumes with the same
	// registration bound and its self-exile idiom still ends it.
	effectFrame effects.EffectFrame
	// action is the replaced event's action marker (Engine.replAction),
	// captured with replaced so a body that suspends before its move still
	// labels that move a sacrifice or discard on the resume.
	action string
	// redirect is Engine.replRedirect at ask time: a destination-changing
	// replacement body that suspends (Mox Diamond's optional discard) still
	// gives its later redirect move its CR 616.1f replacement pass.
	redirect *replRedirect
	// timeTravelObjects is the stable object snapshot for a TimeTravel pass,
	// and timeTravelRound the count of repetitions it has already completed
	// (Amount$ 3). The round is its own field, never packed into target: on
	// a 32-bit build an int cannot hold both halves.
	timeTravelObjects  []state.ObjID
	timeTravelRound    int
	repeatOptionalNext int32
	before             *triggerSnapshot // immutable look-back if a batch replacement suspends
	// target is Dig's index into its deterministic Defined$ target list. It
	// keeps a resumed answer attached to the library that actually asked.
	target int
	// The settled Scry instruction after its CR 616 order choice. The
	// re-entered effect consumes it before looking at any library card.
	scryCount   int32
	scryProceed bool
	// player is the decision's owner. Dredge uses it to apply the answered
	// replacement to the player drawing even when the enclosing effect's
	// controller is someone else.
	player state.PlayerID
	name   string
	// chosenDirection carries Ctx.ChosenDirection across a suspension. The
	// answered ChooseDirection ask sets it through the "choosedirection"
	// resume arm, and any LATER ask the chained GainControlVariant poses
	// (Inniaz's / Order of Succession's per-recipient picks) rebuilds a fresh
	// Ctx that would otherwise lose the direction its Sub still needs. Read
	// from the live resolution Ctx at ask time, the same runtime-continuation
	// class as name.
	chosenDirection string
	// direct identifies an effect invoked outside stack resolution (currently
	// an enters-the-battlefield replacement such as Hideaway). It resumes its
	// source directly rather than requiring a stack object.
	direct bool
	// ownResolution marks a replacement-body frame posed while the entering
	// permanent was ITSELF the resolving spell (a permanent spell's own
	// Updated "as this enters" replacement asks -- Banner of Kinship's
	// creature-type choice): the resolution suspended on this ask, so its
	// completion owes the CR 117.3b priority-returns-to-active reset that
	// handlePriority deferred. A land drop's or other non-resolution entry's
	// frame leaves it false and owes nothing.
	ownResolution bool
	// moved is the object list a ShuffleNonMandatory$ search's first pass
	// moved before its may-shuffle confirm suspended, ridden on the ask via
	// Decision.ResumeMoved: the re-entry's LibraryPosition$ placement needs
	// the list the suspension lost. Nil for every other ask.
	moved       []state.ObjID
	choices     []state.Target
	chosenValid bool
	remembered  []state.Target
	// pendingDamage is the DamageMap$ True mark set of the resolution that
	// posed this ask (effects.Ctx.PendingDamage at suspension time): the
	// resumed Ctx is rebuilt from scratch, so without this ride a mark left
	// before a mid-chain ask (a DamageResolve-defeating suspension) would be
	// lost and the later flush would deal nothing. Runtime continuation
	// state, never client input, the same class as remembered.
	pendingDamage []effects.PendingDamage
	// searchKnown rides the effects.Ctx.SearchKnown set of a search chain
	// across a planted placement leg's own suspension (Decision
	// .ResumeSearchKnown): the leg's answer rebuilds a fresh Ctx, and the next
	// leg must still see which library cards the chooser already knew.
	searchKnown                          []state.Target
	forgetOtherSnapshot                  []state.Target
	forgetOtherOwners                    []state.PlayerID
	forgetOtherReady, forgetOtherCleared bool
	digUntilMove                         string
	digUntilMoveDone                     bool
	// clonePick/clonePickDone ride a DB$ Clone's answered Choices$ pick across
	// a later Optional$ may-copy ask in the same walk (the Decision.ResumeClonePick
	// rider, Ask copies them here): the re-entry's Choices$ branch consumes
	// the selection instead of posing a second Choices$ ask. Zero/false for
	// every other ask.
	clonePick     state.ObjID
	clonePickDone bool
	// unlessPay is set only after a nested non-mana unless-cost payment has
	// completed. It prevents the resumed `unless_pay` arm from charging that
	// payment a second time.
	unlessPay string
	// unlessDiscards is the object list the settled unless-payment discarded
	// (the UnlessCost$ Discard<...> component's picks), stashed by
	// finishUnlessPayment beside the unlessPay outcome: the unless_pay arm
	// hands it to the continuing walk as Ctx.UnlessDiscarded, the
	// ConditionDefined$ Discarded group's mid-resolution channel (Argentum
	// Masticore). Nil for every other payment.
	unlessDiscards []state.ObjID
	// uptoIdx/uptoCount ride an Upto$ Draw's in-flight per-target state
	// across a Dredge ask parked inside that target's answered batch (the
	// Decision.ResumeUpto rider, Ask copies them here): the dredge arm
	// restores Ctx.DrawUptoIdx/Count/Answered from them so effDraw's upto
	// branch continues the batch. uptoIdx -1 (the default every non-upto
	// ask leaves) means no upto is in flight.
	uptoIdx   int
	uptoCount int32
	// exploreDone is Decision.ResumeExploreDone: the explores an api:Explore
	// explorer completed before the one whose election suspended. The
	// "explore" arm restores Ctx.ExploreCount from it.
	exploreDone       int32
	villainousVictims []state.Target
	villainousIndex   int
	villainousChoice  string
	// genericChoosers/genericChooserIndex are the per-Defined$-player cursor of
	// a multi-player api:GenericChoice (the SuspendGenericChoiceRest shape),
	// and genericChoice the answered SVar name of the chooser at that index's
	// predecessor. The resumed Ctx re-binds the cursor so the chosen body runs
	// for its chooser and the remaining choosers are still asked.
	genericChoosers     []state.Target
	genericChooserIndex int
	genericRemembered   []state.Target
	genericChoice       string
	// numberPicks rides a multi-chooser secret ChooseNumber election's
	// accumulated answers (Decision.ResumeNumberPicks) across its per-chooser
	// mid-resolution asks; target carries the asked chooser's index. The
	// resumed Ctx re-binds both so the answered pick is appended and the next
	// chooser is asked. Nil for every other ask.
	numberPicks []int32
	// flipCursor is the DB$ FlipCoin loop position a kind "flip_rest" frame
	// re-enters with (the remaining flips a per-flip sub-ability's nested ask
	// left unrun).
	flipCursor effects.FlipRest
	// tokenRest is the DB$ Token continuation a kind "token_rest" frame
	// re-enters with (SuspendTokenRest): the frozen job, the cursor of the
	// parked mint and the collector id its answer mints into.
	tokenRest effects.TokenRest
	// flipMemory is the resolving chain's shared coin-flip memory at the ask
	// (Engine.resolvingFlipMemory, published by effects.Resolve). The resume
	// rebuilds a fresh Ctx, so it must re-attach this same pointer or a chained
	// Defined$ FlippedTails / Wins reader loses every flip performed before the
	// suspension (Goblin Assassin's per-loser sacrifice ask is the live shape).
	flipMemory *effects.FlipMemory
	// exchangeMemory is the resolving chain's shared ExchangeLife rider memory
	// at the ask (Engine.resolvingExchangeMemory, published by effects.Resolve
	// and re-published by effExchangeLife when it lazily allocates the
	// memory). The resume rebuilds a fresh Ctx, so it must re-attach this same
	// pointer or a chained Count$RememberedNumber reader loses the value the
	// exchange transaction settled (Mister Negative's draw rider under a Lich
	// suspension is the live shape). Nil for every non-ExchangeLife ask.
	exchangeMemory *effects.ExchangeMemory
	// villainousRemembered is the VICTIM of the VillainousChoice whose chosen
	// body is resolving, carried on every ask the body's chain poses (the
	// ambient binding Engine.villainousRemembered captures into Ask). The
	// resume binds it as Ctx.Remembered for every frame of the body, so a
	// nested ask's re-entry (DBSac's sacrifice picker) still resolves
	// Defined$ Remembered / Player.IsRemembered to the victim rather than
	// rebuilding the trigger's own (empty) capture. Set only on asks posed
	// inside a villainous chosen body; nil otherwise.
	villainousRemembered    []state.Target
	villainousRememberedSet bool
	// targetsUnique is the TargetUnique$ accumulator of the resolution that
	// suspended (the Decision.ResumeTargetsUnique rider, captured at ask
	// time from the in-flight Ctx): the resumed Ctx re-binds it, so a later
	// TargetUnique$ rider in the same chain still excludes the targets an
	// earlier rider chose. Nil for every non-TargetUnique ask.
	targetsUnique []state.Target
	// parentLinks is the walk's parent-link record (effects/parent_targets.go)
	// captured at ask time from the live Resolve Ctx: the answering targets of
	// every targeting SubAbility$ link walked so far, in chain order. The
	// record is otherwise scoped to one Resolve walk, so a resolution that
	// suspends at a LATER link and re-enters with a fresh Ctx would lose it
	// and a later untargeted link's ParentTarget/ParentTargeted would fall
	// back to Ctx.Targets -- the root's list -- instead of the nearest
	// targeting ancestor. Restored through Ctx.ResumeParentLinks. Nil when no
	// targeting link ran before the ask.
	parentLinks [][]state.Target
	// linkAnswer/linkAnswered are the in-walk answer a body consumed itself
	// (effects.noteLinkAnswer, effChangeZone's changeZoneChosenTargets) but
	// whose recordParentLink has not run yet at ask time: the re-entered walk
	// must still record that link as the parent, so the pending answer rides
	// the frame. linkAnswered distinguishes an empty (Min-0) answer -- a
	// RECORDED empty parent -- from "no answer".
	linkAnswer   []state.Target
	linkAnswered bool
	// unlessResolved is the unless-cost outcome the suspended pass recorded
	// through Host.SuspendUnless (effects.Resolve: the gate had resolved
	// when the SA's own body posed the pending ask). "resolved-pay" and
	// "resolved-decline" re-enter the gate as an already-resolved answer —
	// the re-entry pass consumes the marker instead of re-posing the pay
	// ask, the asking-body-under-UnlessCost$ livelock fix (Rhystic Study).
	unlessResolved string
	// tapPaidX is the count the triggered-cost window's dynamic tapXType<X/
	// Spec> election paid (rules/cumulative.go's triggeredTapAnswer): the
	// number of permanents the payer tapped IS that cost's announced {X} (CR
	// 601.2b through the window). It rides the frame because the trigger
	// object was never paid an X and the source permanent's own X is its
	// cast-time value, never this payment's; resumeResolution seeds Ctx.X
	// from it so the body's Count$xPaid reads answer. Zero elsewhere.
	tapPaidX int32
	// winPaidX is the X the triggered-cost window's X fold announced or
	// fixed (rules/cumulative.go: the payer's choose-X answer, or the face
	// SVar:X's fixed evaluated value) for a body whose `Cost$` carries an
	// unfolded {X} (Elenda and Azor's "pay {X}{W}{U}{B}") or PayLife<X>
	// part (Vizkopa Confessor's "pay any amount of life"). It rides the
	// frame for the same reason tapPaidX does -- the trigger object was
	// never paid an X, and o.X / triggerPaidX can only supply the source
	// permanent's cast-time value, which for an attack, ETB or end-step
	// trigger is nothing to do with this payment -- and it is set at the pay
	// arm only, from the answered announcement decision or the evaluated
	// fixed body, so a replay derives it exactly as tapPaidX does. Zero
	// elsewhere (and zero on a declined window: the body never runs).
	winPaidX int32
	// charmRest carries the remaining chosen mode names of a cross-mode
	// TargetUnique Charm's mode loop (SuspendCharmRest): the frame re-enters
	// the Charm SA itself with Ctx.Modes = charmRest, so effCharm runs the
	// rest — each target-bearing one with its own split target — after the
	// answered ask's chain completes. Nil everywhere else.
	charmRest []string
	// fuseAlt carries the still-unrun halves of a FUSED split spell (CR
	// 702.101b) onto the fuse-rest continuation frame rules/split.go's
	// runFusedHalves chains after the asking half's own continuation chain:
	// the alternate half must run with ITS OWN CR 608.2b-filtered target
	// slice, never the stack object's whole flat target list, so the captured
	// per-half abilities and target slices ride the frame. It is the same
	// pointers resolveFused computed at resolution start (before the Resolve
	// event), so the rest runs exactly as the no-suspension path would have.
	// Engine scratch, rebuilt by re-execution on replay. nil on every other
	// frame.
	fuseAlt *fusedRest
	// fusedTargets carries the resolving fused half's own CR 608.2b-filtered
	// target slice onto a mid-resolution ask posed by ANY frame of that half
	// (rules/split.go's runFusedHalves sets Engine.fusedResolving around the
	// half's whole effects.Resolve, and Ask captures it here).
	// resumeResolution binds Ctx.Targets to it instead of the stack object's
	// whole flat target list, so a half's sub-ability reads its own half's
	// targets -- ParentTargeted$, Targeted, AllTargeted, DamageSource$
	// ParentTarget (Flesh // Blood, Double Jump // Flying Kick) -- never the
	// sum of both halves'. fusedTargetsSet is the presence bit: an empty slice
	// is a real binding (a targetless half), not "unset". Nil/false on every
	// frame outside a fused half's resolution. Engine scratch, rebuilt by
	// re-execution on replay.
	fusedTargets    []state.Target
	fusedTargetsSet bool

	// charmModeScope is the one mode's target group a distinct modal Charm
	// had narrowed Ctx.Targets to when this ask was posed, with the mode's
	// own SA (effects.Ctx.CharmModeScope). The resume rebuilds Ctx.Targets
	// from the stack object's WHOLE flat list, which for a per-mode Charm is
	// every mode's targets; re-binding the narrowed group keeps a resumed
	// walking primitive (Discard's per-target walk) on its own mode's target
	// instead of running once per mode. The fusedTargets shape, per mode.
	charmModeScope []state.Target
	charmModeSA    *cards.SA
	// fusedSVars is the resolving fused half's own SVar table (the ALTERNATE
	// half's when Blood is the frame), captured with fusedTargets. A fused
	// spell keeps FaceIdx 0, so the generic resume would rebuild the FRONT
	// half's table and an alternate half's sub reading its own SVar -- Blood's
	// NumDmg$ Y = SVar:Y:ParentTargeted$CardPower -- would resolve against
	// the wrong table (0). resumeResolution uses it whenever fusedTargetsSet.
	// Nil on every frame outside a fused half's resolution.
	fusedSVars map[string]string
	// targetControllerLKI is the controller snapshot of the resolution's
	// object targets, captured when Resolve began (effects/registry.go) and
	// carried by Ask onto the pending frame. resumeResolution rebuilds its
	// Ctx from the stack object's targets, whose live controllers have been
	// reset to their owners by any move the asking effect already made; the
	// snapshot is what a chained TokenOwner$ TargetedController reads (the
	// Generous Gift shape: Destroy the target, then create the token for its
	// pre-destruction controller). Immutable once captured, cloned with the
	// frame. Nil when the resolution has no object targets.
	targetControllerLKI map[state.ObjID]state.PlayerID
	// targetCountersLKI is the counters half of the same snapshot: the
	// counters each object target had at the start of the resolution, carried
	// by Ask onto the pending frame so a resumed continuation -- whose Ctx is
	// rebuilt from the live objects, already stripped of counters by a
	// completed Destroy -- still reads the CR 608.2b/h look-back value
	// (Dismantle's DBPutCounter). Immutable once captured, cloned with the
	// frame. Nil when the resolution has no countered object targets.
	targetCountersLKI map[state.ObjID][]state.Counter
	// targetPTLKI is the power/toughness half (Ctx.TargetPTLKI), carried
	// and cloned exactly like targetCountersLKI.
	targetPTLKI map[state.ObjID]effects.TargetPT
	// targetSpellLKI is the stack-kind half of the same snapshot: the object
	// targets that were SPELLS on the stack when Resolve began. A resumed
	// continuation rebuilds its Ctx from the stack object's targets, whose
	// live zone has already changed (a Counter/ChangeZone earlier in the
	// chain), so without this the SpellTargeted count ref would lose the
	// spell it names (Reject Imperfection's proliferate gate, Gale's
	// Redirection's roll modifier, Press the Enemy's Z). Immutable once
	// captured, cloned with the frame. Nil when the resolution has no
	// stack-spell object targets.
	targetSpellLKI map[state.ObjID]bool
	// rolls is the per-die results of the RollDice ask whose answer this
	// point resumes (effects/dice.go's ChosenSVar$/OtherSVar$ choose-one-
	// result shape, the Endeavor cycle): the asking first pass carried them
	// on the decision (decision.Decision.Rolls), Ask copies them here, and
	// the "roll" arm hands them to the re-entered effect, which publishes
	// the chosen/other sums WITHOUT re-rolling -- a re-roll would both
	// re-draw the seeded generator and answer a different question. Plain
	// value data, cloned with the point; a replay re-derives the same rolls
	// from the same seeded draws. Nil for every other ask.
	rolls []int32
	clash *decision.ClashResume
	// replSource is the host of the replacement whose body asked (the
	// ReplaceWith$ body's own Ctx.Source); zero outside a replacement.
	replSource state.ObjID
	// replacedPlayer is the draw-er of the replaced Draw event the frame
	// resumes inside (Ctx.ReplacedPlayer); zero outside a Draw replacement.
	replacedPlayer state.Target
	// loopBound frames resume inside a RepeatEach iteration (or at the
	// RepeatEach itself, kind "repeat"): Ctx.Remembered is rebuilt from
	// loopRemembered rather than from the stack object, because the loop
	// binds its current subject there and the stack object never saw it.
	loopBound      bool
	loopRemembered []state.Target
	// repeatSubject is the RepeatEach subject of the loop whose iteration
	// this frame resumes inside (the Imprinted binding). SuspendRepeat
	// captured it here so the subject survives the suspension, and the
	// resume rebuild's loopBound arm below restores it into
	// Ctx.RepeatSubject, so a Defined$ Imprinted / ImprintedController read
	// after a suspension (definedSpec, unlessPayerTargets) re-binds the
	// iteration's subject. Zero on frames outside any iteration.
	repeatSubject state.Target
	// voteCounts is the api:Vote tally the AmountFromVotes$ RepeatEach this
	// frame belongs to published on its resolution Ctx, captured by
	// SuspendRepeat so a resume rebuilds the same table (the vote is a prior
	// chain link; a fresh Ctx cannot re-derive it). Restored into
	// Ctx.VoteCounts before the resumed SA and, for a loop iteration frame,
	// re-bound as the per-iteration Ctx.VotePublished from rp.repeatSubject.
	// Nil for every frame outside such a loop, preserving the unbound read a
	// vote that published no tally must keep.
	voteCounts []effects.VoteCount
	// repeat is a kind "repeat" frame's loop cursor.
	repeat *repeatCursor
	// lifeDraws parks a GainLife→Draw replacement body's remaining draws
	// (replacement.go's lifeReplacementDraw): the loop's DrawFor suspended
	// on a Dredge ask (CR 702.55) mid-replacement, and the answered dredge
	// re-drives the rest from this cursor. Zero for every other frame.
	lifeDraws int32
	// deferredAsk / deferredResume make a kind "deferred_ask" frame: a
	// mid-resolution ask that arrived while an earlier ask of the same pass
	// was still pending (Engine.Ask). Reaching the frame poses deferredAsk
	// and parks on deferredResume (with this frame's outer as its outer).
	deferredAsk    *decision.Decision
	deferredResume *resumePoint
}

// repeatCursor is the loop position a kind "repeat" frame re-enters with.
// last is the final Remembered of the iteration that completed just before
// the frame runs, handed over by that iteration's own frame.
type repeatCursor struct {
	subjects []state.Target
	next     int
	last     []state.Target
	hasLast  bool
	optional bool
	// election marks a RepeatEach frame parked on a per-subject
	// RepeatOptionalForEachPlayer$ offer: next is the subject whose election
	// was posed, and the answer rides Ctx.RepeatEachOptional on re-entry.
	election bool
	// chooseOrder marks a RepeatEach frame parked on a ChooseOrder$ loop's
	// one-before-the-loop ordering ask: next is 0, and the answered order
	// permutes subjects before the loop re-enters (rules' repeat_choose_order
	// resume arm).
	chooseOrder bool
}

// fusedRest is a fuse-rest continuation's captured remainder (CR 702.101b):
// the halves of the fused split spell still to run, from index `from`, with
// the per-half spell abilities and the CR 608.2b-filtered target slices
// resolveFused computed at resolution start. Engine scratch, rebuilt by
// re-execution on replay.
type fusedRest struct {
	from    int
	halves  []*cards.Face
	sas     []*cards.SA
	targets [][]state.Target
}

// contFrame is one enclosing-loop suspension reported during a resolution
// pass: a plain Resolve loop (resume at sa.Sub), a RepeatEach loop
// (repeat != nil; re-enter sa itself at the cursor), or a cross-mode
// TargetUnique Charm's mode loop (charmRest != nil; re-enter sa — the Charm
// SA itself — with Ctx.Modes = the remaining chosen modes).
type contFrame struct {
	sa            *cards.SA
	repeat        *repeatCursor
	charmRest     []string
	bound         bool
	remembered    []state.Target
	repeatSubject state.Target
	// voteCounts is the AmountFromVotes$ tally the frame's loop belongs to,
	// handed over from the RepeatSuspension and stamped onto every
	// resumePoint buildContinuationChain makes from it.
	voteCounts  []effects.VoteCount
	choices     []state.Target
	chosenValid bool
	// villainousRest marks a frame that re-enters a VillainousChoice's own
	// SA (not sa.Sub) with the victim cursor below, continuing with the
	// victims a chosen body's nested ask left unprocessed. The reported sa
	// IS the VillainousChoice SA, so sa.Sub would be nil.
	villainousRest    bool
	villainousVictims []state.Target
	villainousIndex   int
	// deferredAsk marks a frame carrying a mid-resolution ask posed while an
	// earlier ask of the same pass was still pending (Engine.Ask). It becomes
	// a "deferred_ask" resume frame that poses deferredAsk and parks on
	// deferredResume when the chain reaches it. deferredClaimed records that
	// the asking loop's own SuspendContinuation report was already dropped
	// (the deferred frame re-enters sa itself and walks sa.Sub).
	deferredAsk     *decision.Decision
	deferredResume  *resumePoint
	deferredClaimed bool
	// genericChoiceRest marks a frame that re-enters a multi-player
	// api:GenericChoice's own SA (not sa.Sub) with the chooser cursor below,
	// continuing with the choosers a chosen body's nested ask left unasked.
	// The reported sa IS the GenericChoice SA, so sa.Sub would resume the wrong
	// chain.
	genericChoiceRest   bool
	genericChoosers     []state.Target
	genericChooserIndex int
	genericRemembered   []state.Target
	// flipRest marks a frame that re-enters a DB$ FlipCoin's own SA (not
	// sa.Sub) with the flip cursor below, continuing the flips a per-flip
	// sub-ability's nested ask left unrun. The reported sa IS the FlipCoin SA,
	// so sa.Sub would resume the wrong chain.
	flipRest   bool
	flipCursor effects.FlipRest
	// tokenRest marks a frame that re-enters a DB$ Token's own SA (not
	// sa.Sub) once its parked mint's replacement-order answer has minted
	// (SuspendTokenRest); unlessResolved is that SA's already-resolved
	// UnlessCost$ outcome, seeded into the re-entry so the gate is not
	// re-posed (SuspendUnless).
	tokenRest      *effects.TokenRest
	unlessResolved string
}
