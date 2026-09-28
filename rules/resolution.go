// The mid-resolution ask: an effect that needs a player's decision in the
// middle of resolving the top of the stack (a nested Charm pick, or
// effCopySpellAbility's UnlessCost$ may-pay) poses it through
// effects.Host.Ask, and this file owns what suspends and what resumes.
//
// Suspension is a stack object staying put: resolveTop (rules/stack.go)
// runs the object's effects and, when e.resume is set after the pass,
// leaves the object on the stack and returns with a decision pending, so
// nothing before the ask gets a second chance to run and nothing moves
// until the answer arrives. Resumption re-runs the suspended sub-ability —
// the exact one that asked, never the chain prefix before it — with the
// answer attached to the Ctx, and then moves the fully-resolved object off
// the stack exactly as resolveTop's own tail would have.
//
// fx32's defect: an ask NESTED inside a mode (a Charm whose chosen mode is
// itself an asking primitive, and where the Charm — or that mode — carries
// its own SubAbility$ continuation) used to record ONE resume point and
// overwrite it at each nested ask, so the OUTER continuation the enclosing
// effect was still carrying was dropped. The resolution mechanism is now a
// chain of resume points: the innermost point holds the answer the player
// is being asked, and each point links an `outer` continuation that must
// run once everything inside it resolves. effects.Resolve reports each
// suspended loop through effects.Host.SuspendContinuation, and
// resumeResolution links those reports into the chain, so a nested ask
// finishes its own continuation AND then continues outward until the chain
// is empty — every suspended continuation runs, each exactly once.
//
// All resume state is plain value/pointer data (kind/obj plus *cards.SA
// into the shared, immutable compiled corpus, and the linked outer chain is
// rebuilt deterministically from the same stack object and the same
// recorded answers), never a closure, so Engine.Clone carries it like
// cast/choosing and a replay re-derives the same branch from the same
// recorded intent.
package rules

import (
	"sort"
	"strconv"
	"strings"

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

// Ask implements effects.Host.Ask (rules' side of the interface, and the
// only place a mid-resolution decision is born). It records the resume
// point — the suspended object is always the top of stack, because a
// decision is pending from this moment until it is answered and Advance's
// loop never runs while one is, so nothing in between can resolve or move —
// and hands the decision to the ordinary ask path. Its `outer` is nil here:
// if a resume re-entry posed this nested ask, the enclosing resumeResolution
// (which owns the continuation of the SA it was re-entering) links it once
// effects.Resolve returns. Always returns true: this engine can always ask.
func (e *Engine) Ask(d *decision.Decision) bool {
	// An ask posed inside an off-stack mana resolution has no stack object to
	// park on: carry it through the mana activation's own continuation instead.
	if e.askOffStackMana(d) {
		return true
	}
	obj := state.ObjID(0)
	direct := false
	if n := len(e.G.Stack); n > 0 {
		obj = e.G.Stack[n-1]
	} else {
		obj = d.Source
		direct = true
	}
	kind := d.ResumeKind
	if kind == "" {
		kind = "modes"
	}
	if e.resume != nil && e.pending != nil && e.contChainOwners > 0 {
		// A SECOND mid-resolution ask while an earlier one of the same
		// resolution pass is still unanswered: the first ask was posed from
		// inside a move the asking effect made (a ReplaceWith$ body -- a
		// shock land's "you may pay 2 life" -- suspends the resolution, but
		// the effect that moved the land keeps running its own body: the
		// search reaches its may-shuffle confirm, a mass return moves the
		// next shock land). Posing it now would overwrite the pending
		// decision (Engine.ask's guard). Defer it instead: the fully built
		// resume point rides a "deferred_ask" continuation frame appended to
		// this pass's contChain, so it is posed exactly when the answered
		// earlier frame (and every continuation reported before this ask)
		// has run, and before the continuations the enclosing loops report
		// after it. The asking caller sees an ordinary suspended ask.
		rp := e.buildAskResume(d, obj, direct, kind)
		e.contChain = append(e.contChain, contFrame{sa: d.ResumeSA, deferredAsk: d, deferredResume: rp})
		e.lastDeferred = rp
		e.askCount++
		return true
	}
	e.ask(d)
	e.resume = e.buildAskResume(d, obj, direct, kind)
	e.lastDeferred = nil
	e.askCount++
	return true
}

// AskCount implements effects' optional askCounter seam: the number of
// mid-resolution asks this engine has taken (posed or deferred). effects.
// Resolve's UnlessCost$ gate compares it across the gate, because a gate ask
// deferred behind an already-suspended resolution leaves Suspended()
// unchanged. Engine scratch, never logged.
func (e *Engine) AskCount() uint64 { return e.askCount }

// EventMark implements effects' optional eventMarker seam: the event log's
// current length, a mark StateChangedSince compares against.
func (e *Engine) EventMark() int { return len(e.L.Events) }

// StateChangedSince implements effects' optional eventMarker seam: whether
// any event other than a Note was logged after mark. A Note is the log's
// commentary and folds into no game state (events.Apply), so a span holding
// only Notes left the game exactly as it found it. A ZERO-amount Damage
// event is the same kind of no-op: CR 120.8 says 0 damage is never dealt,
// and the fold marks nothing for it (a DamageAll whose NumDmg$ counts an
// empty Remembered set -- Kindle the Carnage repeated over an empty hand
// logs one per creature per pass, and counting it as progress let a bot's
// "Repeat" answer loop forever, cardfuzz batch8 line 1). A ZERO-amount
// LifeChange likewise folds into nothing (events.Apply adds 0 to the life
// total; CR 119.9's "gains 0 life" is no life-gain event, and a 0 loss the
// same): Ad Nauseam repeated over an empty library loses life equal to the
// mana value of no card. AFLifeLost is always published, even for that zero
// loss: a StoreSVar only changes state when it replaces a different value.
func (e *Engine) StateChangedSince(mark int) bool {
	if mark < 0 {
		mark = 0
	}
	for i := mark; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.Note || (ev.Kind == events.Damage && ev.Amount == 0) ||
			(ev.Kind == events.LifeChange && ev.Amount == 0) {
			continue
		}
		if ev.Kind == events.StoreSVar {
			// Read the value immediately before this event, not the final
			// object value: intermediate changes still count as progress.
			unchanged := false
			for j := i - 1; j >= 0; j-- {
				prev := e.L.Events[j]
				// CR 400.7 resets RuntimeSVars on a zone change: an
				// earlier write to that ObjID is no longer its old value.
				if prev.Obj == ev.Obj && (prev.Kind == events.MoveZone || prev.Kind == events.Draw || prev.Kind == events.PutOnStack) {
					break
				}
				if prev.Kind == events.StoreSVar && prev.Obj == ev.Obj && prev.Text == ev.Text {
					unchanged = prev.Amount == ev.Amount
					break
				}
			}
			if unchanged {
				continue
			}
		}
		return true
	}
	return false
}

// chosenDirectionForResume reads the live resolution Ctx's chosen direction
// for a resume point (empty when no chain is published, or none was chosen
// yet). It is how a suspended mid-resolution ask keeps the ChooseDirection
// answer its chained SubAbility reads after a freshly rebuilt Ctx.
func (e *Engine) chosenDirectionForResume() string {
	if e.resolutionCtx == nil {
		return ""
	}
	return e.resolutionCtx.ChosenDirection
}

// buildAskResume builds the resume point of the mid-resolution ask d from
// the engine's ambient resolution state at the moment the ask is posed (or
// deferred). See Engine.Ask.
func (e *Engine) buildAskResume(d *decision.Decision, obj state.ObjID, direct bool, kind string) *resumePoint {
	var replacementTarget state.Target
	var replacementAmount int32
	if e.replacingEvent != nil && e.replacingEvent.Kind == events.Damage {
		replacementTarget = state.Target{Obj: e.replacingEvent.Obj}
		if e.replacingEvent.Obj == 0 {
			replacementTarget = state.Target{Player: e.replacingEvent.Player, IsPlayer: true}
		}
		replacementAmount = e.replacingEvent.Amount
	}
	replacementSource := e.protectionSource(e.damaging)
	if e.replacingEvent == nil && e.applyingReplacement && e.resolutionCtx != nil &&
		e.resolutionCtx.ReplacementTarget != (state.Target{}) {
		// A replacement body re-entered from a resume frame (a second ask of
		// a resumed body, or a body queued behind an earlier ask by
		// resolveReplacementBody) runs with no live replacingEvent: the
		// frame's rebuilt Ctx is the only record of what the body replaced,
		// so this ask's resume must carry it on, or the answered re-entry
		// (Nefarious Lich's DefinedPlayer$ ReplacedTarget pick) resolves
		// against nobody and moves nothing.
		replacementTarget = e.resolutionCtx.ReplacementTarget
		replacementAmount = e.resolutionCtx.ReplacementAmount
		replacementSource = e.resolutionCtx.ReplacementSource
	}
	// Capture whether the ask is being posed from inside a replacement
	// effect's ReplaceWith$ body (fx44). e.applyingReplacement is true for
	// the whole of that body's resolution, so an ask posed from within it
	// must resume still under the flag — see the resumePoint field's
	// comment and resumeResolution's restore of it.
	//
	// A replacement body resumes from the object whose resolution it
	// interrupted -- normally still on top of the stack (a sorcery that
	// reanimates Mox Diamond), so the rest of that spell's chain and its
	// completion still run after the answer. The one exception is a permanent
	// spell whose own Updated entry replacement asks (Sower of Discord): the
	// move has already taken the resolving object off the stack, so the top
	// of the stack is some unrelated object and the resume must rebuild from
	// the entering permanent itself. replSource keeps the replacement's host
	// for the resumed body's own Source either way. The one further
	// exception this build adds is an effect invoked outside stack
	// resolution (direct), whose resume must find its own source.
	var replSource state.ObjID
	if e.applyingReplacement && d.Source != 0 {
		replSource = d.Source
		// The resume must rebuild from the replacement's host in exactly two
		// shapes: a permanent spell whose own Updated entry replacement asks
		// (Sower of Discord — d.Source IS e.resolvingObj but the move has
		// already taken it off the stack, so the top of the stack is some
		// unrelated object), and a permanent that entered by a replacement with
		// NO stack resolution in flight at all (a land drop: Hallowed Fountain's
		// "you may pay 2 life. If you don't, it enters tapped" — e.resolvingObj
		// is 0, so the original d.Source == e.resolvingObj gate never fired and
		// the answer degraded to a no-op). Every other shape — a replacement
		// asking while a DIFFERENT spell is resolving (Mox Diamond reanimated by
		// a sorcery: d.Source is the mox, already on the battlefield, but the
		// spell whose chain the replacement interrupted is still the top of the
		// stack and MUST own the resume) — keeps the ordinary top-of-stack
		// resume. resolveTop keeps e.resolvingObj == the stack top for the whole
		// of a spell's resolution, so "top of stack is the interrupted spell" is
		// exactly "e.resolvingObj != 0 && d.Source != e.resolvingObj".
		if so := e.G.Obj(d.Source); so != nil && so.Zone != state.ZStack &&
			(e.resolvingObj == 0 || d.Source == e.resolvingObj) {
			obj = d.Source
		}
	}
	return &resumePoint{kind: kind, obj: obj, sa: d.ResumeSA, replSource: replSource,
		replacement: e.applyingReplacement, replaced: e.replReplaced, action: e.replAction,
		replacedCards:     append([]state.ObjID(nil), e.replReplacedCards...),
		redirect:          e.replRedirect,
		replacedPlayer:    e.replReplacedPlayer,
		replacementTarget: replacementTarget, replacementSource: replacementSource,
		replacementAmount: replacementAmount,
		effectFrame:       e.currentEffectFrame,
		before:            e.triggerBefore, target: d.ResumeTarget, player: d.Player,
		chosenDirection: e.chosenDirectionForResume(),
		direct:          direct, rolls: d.Rolls, clash: cloneClashResume(d.ResumeClash),
		choices:     append([]state.Target(nil), d.ResumeChoices...),
		chosenValid: d.ResumeChosenValid, remembered: append([]state.Target(nil), d.ResumeRemembered...),
		pendingDamage:       effects.ClonePendingDamage(e.resolutionPendingDamage()),
		searchKnown:         append([]state.Target(nil), d.ResumeSearchKnown...),
		forgetOtherSnapshot: append([]state.Target(nil), d.ResumeForgetOtherSnapshot...),
		forgetOtherOwners:   append([]state.PlayerID(nil), d.ResumeForgetOtherOwners...),
		forgetOtherReady:    d.ResumeForgetOtherReady, forgetOtherCleared: d.ResumeForgetOtherCleared,
		digUntilMove: d.ResumeDigUntilMove, digUntilMoveDone: d.ResumeDigUntilMoveDone,
		clonePick: d.ResumeClonePick, clonePickDone: d.ResumeClonePickDone,
		moved:   append([]state.ObjID(nil), d.ResumeMoved...),
		uptoIdx: d.ResumeUptoIdx, uptoCount: d.ResumeUptoCount, exploreDone: d.ResumeExploreDone,
		villainousVictims:       append([]state.Target(nil), d.ResumeVillainousVictims...),
		villainousIndex:         d.ResumeVillainousIndex,
		genericChoosers:         append([]state.Target(nil), d.ResumeGenericChoosers...),
		genericChooserIndex:     d.ResumeGenericChooserIndex,
		villainousRemembered:    append([]state.Target(nil), e.villainousRemembered...),
		villainousRememberedSet: e.villainousRememberedSet,
		targetsUnique:           e.targetsUniqueRide(d),
		fusedTargets:            append([]state.Target(nil), e.fusedResolving...),
		fusedTargetsSet:         e.fusedResolvingSet,
		charmModeScope:          e.charmModeScope(),
		charmModeSA:             e.charmModeScopeSA(),
		fusedSVars:              e.fusedResolvingSVars,
		winPaidX:                e.windowPaidX,
		timeTravelObjects:       append([]state.ObjID(nil), d.ResumeObjects...),
		timeTravelRound:         d.ResumeRound,
		repeatOptionalNext:      d.ResumeRepeatNext,
		// The pre-move controller snapshot of this chain's object targets,
		// published by effects.Resolve around the whole chain. Captured onto
		// the pending frame so a resumed continuation (which rebuilds its Ctx
		// from objects whose live controllers may already have been reset to
		// their owners) still sees the CR 608.2h last-known controller.
		targetControllerLKI: effects.CloneTargetControllerLKI(e.resolvingTargetControllerLKI),
		targetCountersLKI:   resolutionTargetCounters(e.resolutionCtx),
		targetSpellLKI:      resolutionTargetSpells(e.resolutionCtx),
		flipMemory:          e.resolvingFlipMemory}
}

// Suspended implements effects.Host.Suspended: the resolution is suspended
// when a mid-resolution ask set e.resume and the answer has not yet arrived
// to clear it. effects.Resolve checks this after every sub-ability so that a
// suspended ask stops the SubAbility chain instead of running what sits
// beneath it (B1). It is the rules-side half of the pairing with Ask: Ask
// sets e.resume, and handleModes clears it the moment the answer lands, so
// the resume pass re-enters the chain with nothing suspended and walks the
// rest of it exactly once.
//
// SetResolutionFlipMemory implements effects' flipMemoryHost (an optional
// seam, so the effects test doubles need no method): effects.Resolve publishes
// the resolving chain's shared coin-flip memory around the whole walk and
// restores the enclosing value on return, and effFlipCoin re-publishes when it
// lazily allocates that memory. Returns the previous value for the defer
// restore. Engine-transient scratch like resolvingTargetControllerLKI.
func (e *Engine) SetResolutionFlipMemory(m *effects.FlipMemory) *effects.FlipMemory {
	prev := e.resolvingFlipMemory
	e.resolvingFlipMemory = m
	return prev
}

// SetResolutionTargetControllerLKI implements
// effects.Host.SetResolutionTargetControllerLKI: effects.Resolve publishes
// the target-controller snapshot of the chain it is about to walk, and
// restores the previous value on return, so the scratch holds exactly the
// innermost running chain's map. Engine.Ask consumes it onto the pending
// resumePoint. Writes engine scratch, never e.resume, so it is not a writer
// of the resume state the archtest guards.
func (e *Engine) SetResolutionTargetControllerLKI(m map[state.ObjID]state.PlayerID) map[state.ObjID]state.PlayerID {
	prev := e.resolvingTargetControllerLKI
	e.resolvingTargetControllerLKI = m
	return prev
}

// SetResolutionCtx implements effects.Host's optional resolutionCtxHost
// interface: effects.Resolve publishes the live Ctx of the chain it is about
// to walk, and restores the previous value on return, so the scratch holds
// exactly the innermost running chain's Ctx. Engine.Ask consumes it as the
// fallback source of the chain's TargetUnique$ accumulator. Writes engine
// scratch, never e.resume, so it is not a writer of the resume state the
// archtest guards.
func (e *Engine) SetResolutionCtx(c *effects.Ctx) *effects.Ctx {
	prev := e.resolutionCtx
	e.resolutionCtx = c
	return prev
}

// resolutionTargetCounters is the target-counters snapshot a pending ask
// carries onto its resume point: the live Resolve chain's snapshot (published
// through SetResolutionCtx), cloned so the frame owns its storage. Nil outside
// a chain (a combat or mulligan ask) or when the chain captured none.
func resolutionTargetCounters(c *effects.Ctx) map[state.ObjID][]state.Counter {
	if c == nil {
		return nil
	}
	return effects.CloneTargetCountersLKI(c.TargetCountersLKI)
}

// resolutionTargetSpells is the target-spell snapshot a pending ask carries
// onto its resume point: the live Resolve chain's resolution-start set of
// object targets that were spells on the stack, cloned so the frame owns its
// storage. Nil outside a chain (a combat or mulligan ask) or when the chain
// captured none. Without it a resumed Ctx -- rebuilt from the stack object's
// targets, whose live zone a completed Counter/ChangeZone has already
// changed -- would answer SpellTargeted$CardManaCostLKI as zero.
func resolutionTargetSpells(c *effects.Ctx) map[state.ObjID]bool {
	if c == nil {
		return nil
	}
	return effects.CloneTargetSpellLKI(c.TargetSpellLKI)
}

// resolutionPendingDamage is the DamageMap$ True mark set a pending ask
// carries onto its resume point: the live Resolve chain's marks (published
// through SetResolutionCtx), cloned so the frame owns its storage. Nil
// outside a chain (a combat or mulligan ask) or when the chain has marked
// nothing.
func (e *Engine) resolutionPendingDamage() []effects.PendingDamage {
	if e.resolutionCtx == nil {
		return nil
	}
	return effects.ClonePendingDamage(e.resolutionCtx.PendingDamage)
}

// snapshotDepartingTargetCounters refreshes the resolving chain's target-
// counters look-back (Ctx.TargetCountersLKI) at the DEPARTURE boundary, the
// authoritative capture point for the CR 608.2b/h read: oid is about to leave
// the battlefield (its MoveZone is in flight, pre-Apply), so if it is an
// object target of the published chain Ctx its counters are captured NOW --
// immediately before events.Apply's Move fold clears them -- overwriting
// effects.Resolve's resolution-start snapshot. A chain that changed the
// target's counters earlier in the same resolution (Dismantle-style destroy
// preceded by a counter swing) must look back to the counters as they were at
// the zone change, not to the stale entry capture. An object that departs
// with no counters drops its entry, so the read fails closed to the live
// (already-cleared) zero. Non-targets are never captured: the look-back is
// read only through the chain's target groups.
func (e *Engine) snapshotDepartingTargetCounters(oid state.ObjID) {
	c := e.resolutionCtx
	if c == nil {
		return
	}
	targeted := false
	for _, t := range c.Targets {
		if !t.IsPlayer && t.Obj == oid {
			targeted = true
			break
		}
	}
	if !targeted && c.PickedTargets != nil {
		for _, t := range c.PickedTargets {
			if !t.IsPlayer && t.Obj == oid {
				targeted = true
				break
			}
		}
	}
	if !targeted {
		return
	}
	o := e.G.Obj(oid)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	if len(o.Counters) == 0 {
		delete(c.TargetCountersLKI, oid)
		return
	}
	if c.TargetCountersLKI == nil {
		c.TargetCountersLKI = make(map[state.ObjID][]state.Counter)
	}
	c.TargetCountersLKI[oid] = append([]state.Counter(nil), o.Counters...)
}

// targetsUniqueRide is the TargetUnique$ accumulator a decision resumes with:
// the accumulator the asking site already stamped onto the decision when it
// carried one, else the LIVE accumulator of the Resolve chain currently
// running (published through SetResolutionCtx). It is the one home of the
// ride, so an ask of ANY kind -- a modal election, a ward pay, a
// dig/scry/arrange pick -- parked between two TargetUnique$ riders preserves
// the picks earlier riders chose, rather than only the shared target pre-ask
// and the unless gate that stamped it explicitly. Nil outside a chain (a
// combat or mulligan ask), where there is no accumulator to carry.
func (e *Engine) targetsUniqueRide(d *decision.Decision) []state.Target {
	if len(d.ResumeTargetsUnique) > 0 {
		return append([]state.Target(nil), d.ResumeTargetsUnique...)
	}
	if e.resolutionCtx == nil {
		return nil
	}
	return append([]state.Target(nil), e.resolutionCtx.TargetsUnique...)
}

// SuspendUnless implements effects.Host.SuspendUnless: the unless-cost
// outcome of an SA whose BODY posed the pending ask (the gate had resolved
// before the body suspended). The pending ask's resume point re-enters that
// SA (resumeResolution's effects.Resolve(e, ctx, rp.sa)), so the recorded
// marker rides exactly that resume point; a deeper frame's continuation
// re-enters sa.Sub (buildContinuationChain), which bypasses the gate, so
// those frames carry nothing.
func (e *Engine) SuspendUnless(sa *cards.SA, paid bool) {
	marker := "resolved-decline"
	if paid {
		marker = "resolved-pay"
	}
	if e.resume == nil {
		return
	}
	// The most recent ask owns the marker: a DEFERRED one (Engine.Ask) when
	// this pass deferred one after the pending ask was posed.
	target := e.resume
	if e.lastDeferred != nil {
		target = e.lastDeferred
	}
	if target.sa == sa {
		target.unlessResolved = marker
	}
	// A Token whose mint parked re-enters ITSELF through its own
	// continuation frame (SuspendTokenRest), which must carry the outcome.
	if n := len(e.contChain); n > 0 && e.contChain[n-1].tokenRest != nil && e.contChain[n-1].sa == sa {
		e.contChain[n-1].unlessResolved = marker
	}
}

func (e *Engine) Suspended() bool {
	if f := e.offStackMana; f != nil {
		return f.suspended(e)
	}
	resumed := e.resume != nil && !(e.resume == e.answerParked && e.pending == nil)
	return resumed || e.unlessPayment != nil || e.cumulative != nil || e.triggerCost != nil
}

// SuspendContinuation implements effects.Host.SuspendContinuation: an
// effects.Resolve loop stopped because the resolution suspended at a
// mid-resolution ask and is reporting its own suspension point `sa`, so its
// chain must later resume at sa.Sub. The innermost loop (the one whose `sa`
// IS the pending ask's ResumeSA) is dropped: the pending frame re-enters
// that SA itself, which already walks sa.Sub, so recording it would run it
// twice. Every enclosing loop is recorded, in unwind order — inner loops
// report before outer ones, which is also the order their continuations run
// once the innermost resolves.
func (e *Engine) SuspendContinuation(sa *cards.SA) {
	if e.resume == nil {
		return
	}
	for i := len(e.contChain) - 1; i >= 0; i-- {
		if cf := &e.contChain[i]; cf.deferredAsk != nil && !cf.deferredClaimed && cf.sa == sa {
			// The loop that posed a DEFERRED ask (Engine.Ask): the deferred
			// frame re-enters sa itself and walks sa.Sub, so this loop's own
			// continuation is dropped exactly as the pending ask's is below.
			// Checked first: two copies of one card share their SA pointers,
			// so a second shock land's body loop also matches e.resume.sa.
			cf.deferredClaimed = true
			return
		}
	}
	if sa == e.resume.sa {
		return // this loop is the one that asked; its own re-entry walks sa.Sub.
	}
	if sa == e.repeatReported {
		// The RepeatEach just recorded its own loop frame, whose re-entry runs
		// the remaining iterations and then walks sa.Sub itself.
		e.repeatReported = nil
		return
	}
	e.contChain = append(e.contChain, contFrame{sa: sa})
}

// SuspendRepeat implements effects.Host.SuspendRepeat. Everything recorded so
// far in this pass -- the pending ask and the continuation frames of loops
// nested inside the iteration -- resumes inside that iteration, so each is
// bound to the iteration's Remembered unless a deeper loop already bound it.
// The loop's own frame follows them, bound to the RepeatEach's Remembered.
// SuspendRepeatOptional implements effects.Host.SuspendRepeatOptional. The
// body of iteration next-1 owns the pending ask; this frame runs only after
// that body resumes and completes, and it re-enters RepeatOptional$ to pose
// the repeat election for iteration next (never that iteration's body
// directly -- the do/while owes the player the election after every process).
func (e *Engine) SuspendRepeatOptional(sa *cards.SA, next int32) {
	if e.resume == nil {
		return
	}
	e.contChain = append(e.contChain, contFrame{
		sa: sa, repeat: &repeatCursor{next: int(next), optional: true},
	})
	e.repeatReported = sa
}

func (e *Engine) SuspendRepeat(s effects.RepeatSuspension) {
	if e.resume == nil {
		return
	}
	body := append([]state.Target(nil), s.Body...)
	// The AmountFromVotes$ tally snapshot (nil outside such a loop) is bound
	// to the same frames the iteration's Remembered is: the pending ask's own
	// frame and every continuation frame of loops nested inside the
	// iteration. A later iteration frame carries it too, so effRepeatEach
	// re-derives each remaining subject's Votes from the restored table.
	votes := cloneVoteCounts(s.VoteCounts)
	if !e.resume.loopBound {
		e.resume.loopBound, e.resume.loopRemembered, e.resume.repeatSubject = true, body, s.Subject
		e.resume.voteCounts = cloneVoteCounts(votes)
	}
	for i := range e.contChain {
		if !e.contChain[i].bound {
			e.contChain[i].bound, e.contChain[i].remembered, e.contChain[i].repeatSubject = true, body, s.Subject
			e.contChain[i].voteCounts = cloneVoteCounts(votes)
		}
	}
	e.contChain = append(e.contChain, contFrame{
		sa:          s.SA,
		repeat:      &repeatCursor{subjects: append([]state.Target(nil), s.Subjects...), next: s.Next, election: s.Election, chooseOrder: s.ChooseOrder},
		bound:       true,
		remembered:  append([]state.Target(nil), s.Outer...),
		voteCounts:  cloneVoteCounts(votes),
		choices:     append([]state.Target(nil), s.Chosen...),
		chosenValid: s.ChosenValid,
	})
	e.repeatReported = s.SA
}

// cloneVoteCounts deep-copies a vote tally snapshot at an ownership boundary
// (the effects/rules payload, each continuation frame, the engine clone). The
// entries are plain values, so a fresh slice is a full copy; nil stays nil so
// a loop on a vote that published no tally keeps its unbound read.
func cloneVoteCounts(in []effects.VoteCount) []effects.VoteCount {
	if in == nil {
		return nil
	}
	return append([]effects.VoteCount(nil), in...)
}

// SuspendCharmRest implements effects.Host.SuspendCharmRest: a Charm's mode
// loop suspended mid-mode (effCharm's generic loop, charmDistinctTargetRun or
// charmCrossModeRun). The frame re-enters the Charm SA itself with Ctx.Modes =
// rest once the answered ask's chain completes; it runs AFTER the inner
// continuations the suspended mode's own chain reported, which is exactly the
// append order here. Setting repeatReported to the Charm's SA suppresses the
// enclosing Resolve loop's own SuspendContinuation report of the same SA (the
// innermost rule: this frame re-enters the Charm itself, so a second frame
// would re-run CharmSA.Sub — nil — and degrade to a spurious no-sub-ability
// Note).
//
// An EMPTY rest (the suspended mode was the LAST chosen one) still reports:
// it appends the same re-entry frame with a non-nil empty mode list, so the
// Charm re-enters, runs no further mode, and walks its own Sub — exactly what
// the suppressed plain continuation would have done, minus the spurious Note
// (the plain frame resumes at CharmSA.Sub == nil and degrades). Ctx.Modes must
// stay non-nil so effCharm's re-entry branch is taken rather than its ask.
func (e *Engine) SuspendCharmRest(sa *cards.SA, rest []string) {
	if e.resume == nil {
		return
	}
	e.contChain = append(e.contChain, contFrame{sa: sa,
		charmRest: append([]string{}, rest...)})
	e.repeatReported = sa
}

// SuspendVillainousRest implements effects.Host.SuspendVillainousRest: a
// VillainousChoice's chosen body suspended on a nested mid-resolution ask
// with victims still to process. The frame re-enters the VillainousChoice
// SA itself with the victim cursor restored once the answered ask's chain
// completes; the reported sa IS the VillainousChoice's own SA, so folding it
// into sa.Sub (the plain-frame shape) would resume nothing. Setting
// repeatReported to that SA suppresses the enclosing Resolve loop's own
// SuspendContinuation report of the same SA, exactly as SuspendCharmRest
// does for a Charm.
func (e *Engine) SuspendVillainousRest(sa *cards.SA, rest effects.VillainousRest) {
	if e.resume == nil || len(rest.Victims) == 0 {
		return
	}
	e.contChain = append(e.contChain, contFrame{sa: sa, villainousRest: true,
		villainousVictims: append([]state.Target(nil), rest.Victims...),
		villainousIndex:   rest.Next})
	e.repeatReported = sa
}

// SuspendGenericChoiceRest implements effects.Host.SuspendGenericChoiceRest: a
// multi-player api:GenericChoice's chosen body suspended on a nested
// mid-resolution ask with choosers still to ask. The frame re-enters the
// GenericChoice SA itself with the chooser cursor restored once the answered
// ask's chain completes; the reported sa IS the GenericChoice's own SA, so
// folding it into sa.Sub (the plain-frame shape) would resume nothing.
// Setting repeatReported to that SA suppresses the enclosing Resolve loop's
// own SuspendContinuation report of the same SA, exactly as
// SuspendVillainousRest does for a VillainousChoice.
func (e *Engine) SuspendGenericChoiceRest(sa *cards.SA, rest effects.GenericChoiceRest) {
	if e.resume == nil || len(rest.Choosers) == 0 {
		return
	}
	e.contChain = append(e.contChain, contFrame{sa: sa, genericChoiceRest: true,
		genericChoosers:     append([]state.Target(nil), rest.Choosers...),
		genericChooserIndex: rest.Next,
		genericRemembered:   append([]state.Target(nil), rest.Remembered...)})
	e.repeatReported = sa
}

// SuspendFlipRest implements effects.Host.SuspendFlipRest: a DB$ FlipCoin
// loop suspended inside a per-flip sub-ability's mid-resolution ask
// (FlipUntilYouLose$ or Amount$ > 1) with flips still owed. The frame
// re-enters the FlipCoin SA ITSELF (not sa.Sub) with Ctx.FlipRest restored
// once the answered ask's own chain completes, so the remaining flips run
// rather than being abandoned. Setting repeatReported to the FlipCoin SA
// suppresses the enclosing Resolve loop's own SuspendContinuation report of
// the same SA (the SuspendCharmRest convention: this frame re-enters the
// primitive itself, so a second frame would re-run its Sub chain).
func (e *Engine) SuspendFlipRest(sa *cards.SA, rest effects.FlipRest) {
	if e.resume == nil {
		return
	}
	e.contChain = append(e.contChain, contFrame{sa: sa, flipRest: true,
		flipCursor: effects.FlipRest{
			Players:     append([]state.PlayerID(nil), rest.Players...),
			PlayerIndex: rest.PlayerIndex,
			Iter:        rest.Iter,
			Amount:      rest.Amount,
			UntilLose:   rest.UntilLose,
		}})
	e.repeatReported = sa
}

// moveCounterPending is one MoveCounter resolution's answered asks, stored
// under the resolving stack object's id (Engine.moveCounterAsk) so a later
// resume round of the SAME SA can re-seed them into its fresh Ctx. A
// MoveCounter sub the placement/announcement ask never covered asks twice --
// its own ValidTgts$ target set (the mvts1 pre-ask, answered through the
// "tgts" arm) and, for CounterType$/CounterNum$ Any, a kind and an amount
// through its own arms -- and every resume re-enters the SA from its top
// with a fresh Ctx, so the earlier round's answer is otherwise lost and the
// asks alternate forever (the movecounter1 livelock: Nesting Grounds, Rikku,
// Goldberry's second ability). The arms record here; seedMoveCounterAsk
// fills the fresh Ctx's still-unanswered fields before effects.Resolve
// re-enters; the entry is deleted when the resolution completes.
type moveCounterPending struct {
	targets []state.Target // the answered ValidTgts$ pre-ask set
	kind    string         // the answered CounterType$ Any pick
	kindSet bool
	n       int32 // the answered CounterNum$ Any amount (0 = a decline)
	nSet    bool
}

// counterTypePending is the replay-derived continuation for one
// CounterTypePerDefined$ SA. It is keyed below the resolving stack object and
// then owned by this exact immutable SA, so a chained PutCounter cannot see
// another PutCounter's answers.
type counterTypePending struct {
	sa      *cards.SA
	answers []string // recipient index -> answered individual kind
}

// moveCounterEntry returns (creating if needed) the pending state for a
// resolving MoveCounter stack object.
func (e *Engine) moveCounterEntry(obj state.ObjID) *moveCounterPending {
	if e.moveCounterAsk == nil {
		e.moveCounterAsk = make(map[state.ObjID]*moveCounterPending)
	}
	p := e.moveCounterAsk[obj]
	if p == nil {
		p = &moveCounterPending{}
		e.moveCounterAsk[obj] = p
	}
	return p
}

// seedMoveCounterAsk fills a fresh resume Ctx with the answers earlier rounds
// of this MoveCounter resolution already recorded, leaving anything the
// current round's own arm already answered (its Done flag is authoritative)
// alone. The targets ride the generic pre-ask transport (Ctx.TargetsPick),
// which chosenTargetsFor consumes exactly like a just-answered ask, so the
// re-entered SA does not re-pose its target ask; the kind and amount ride
// their own pairs, which effMoveCounter consumes-and-clears (fx42).
func (e *Engine) seedCounterTypeAsk(obj state.ObjID, sa *cards.SA, ctx *effects.Ctx) {
	p := e.counterTypeAsk[obj]
	if p == nil || p.sa != sa || len(p.answers) == 0 {
		return
	}
	ctx.CounterKindAnswers = append([]string(nil), p.answers...)
	for i := len(p.answers) - 1; i >= 0; i-- {
		if p.answers[i] != "" {
			ctx.CounterKindAnswerIndex, ctx.CounterKindAnswerSet = i, true
			break
		}
	}
}

func (e *Engine) seedMoveCounterAsk(obj state.ObjID, ctx *effects.Ctx) {
	p := e.moveCounterAsk[obj]
	if p == nil {
		return
	}
	if !ctx.TargetsPickDone && len(p.targets) > 0 {
		ctx.TargetsPick = append([]state.Target(nil), p.targets...)
		ctx.TargetsPickDone = true
	}
	if !ctx.MoveCounterKindDone && p.kindSet {
		ctx.MoveCounterKind, ctx.MoveCounterKindDone = p.kind, true
	}
	if !ctx.MoveCounterNDone && p.nSet {
		ctx.MoveCounterN, ctx.MoveCounterNDone = p.n, true
	}
}

// recordTargetsPick stores one answered generic ValidTgts$ pre-ask under the
// resolving stack object and this exact SA's Line, so a LATER suspension of
// the same SA re-seeds it instead of re-posing the ask (the general form of
// the movecounter1 fix; see Engine.targetsPickAsk). A nil SA -- a resume
// point with no sub-ability -- records nothing: there is nothing to key on.
func (e *Engine) recordTargetsPick(obj state.ObjID, sa *cards.SA, targets []state.Target) {
	if sa == nil {
		return
	}
	if e.targetsPickAsk == nil {
		e.targetsPickAsk = make(map[state.ObjID]map[string][]state.Target)
	}
	byLine := e.targetsPickAsk[obj]
	if byLine == nil {
		byLine = make(map[string][]state.Target)
		e.targetsPickAsk[obj] = byLine
	}
	byLine[sa.Line] = append([]state.Target(nil), targets...)
}

// seedTargetsPick re-seeds a fresh resume Ctx with the pre-ask answer an
// EARLIER round of this same SA's resolution already recorded. The current
// round's own arm is authoritative: a just-answered "tgts" set has
// TargetsPickDone already true and is left alone. An empty recorded answer
// (a Min-0 pre-ask the player declined) is re-seeded just the same -- the
// done marker, not the set, is what stops the re-pose.
func (e *Engine) seedTargetsPick(obj state.ObjID, sa *cards.SA, ctx *effects.Ctx) {
	if sa == nil || ctx.TargetsPickDone {
		return
	}
	byLine := e.targetsPickAsk[obj]
	if byLine == nil {
		return
	}
	picked, ok := byLine[sa.Line]
	if !ok {
		return
	}
	ctx.TargetsPick = append([]state.Target(nil), picked...)
	ctx.TargetsPickDone = true
}

// forgetTargetsPick drops one SA's recorded pre-ask answer once that SA's
// resolution has completed without suspending, so a later re-entry of the
// same body (a Repeat loop, a second activation of the same object) poses
// its own ask. Only this SA's entry goes: a sibling frame of the same stack
// object still carrying its own answer keeps it.
func (e *Engine) forgetTargetsPick(obj state.ObjID, sa *cards.SA) {
	if sa == nil {
		return
	}
	byLine := e.targetsPickAsk[obj]
	if byLine == nil {
		return
	}
	delete(byLine, sa.Line)
	if len(byLine) == 0 {
		delete(e.targetsPickAsk, obj)
	}
}

// aorEntry returns (creating if needed) the answered-kind cursor for a
// resolving AddOrRemoveCounter stack object.
func (e *Engine) aorEntry(obj state.ObjID) map[string]bool {
	if e.aorAsk == nil {
		e.aorAsk = make(map[state.ObjID]map[string]bool)
	}
	set := e.aorAsk[obj]
	if set == nil {
		set = make(map[string]bool)
		e.aorAsk[obj] = set
	}
	return set
}

// seedAorAsk fills a fresh resume Ctx with the kinds this AddOrRemoveCounter
// resolution has already answered an election for — INCLUDING the current
// round's own answer, which the "aor_elect" arm recorded before this runs
// (the map is therefore always a superset of the walk's own skip guards; the
// walk also skips the current kind by the AorElect/AorKind pair, so double
// coverage is harmless). This is what keeps an EachExistingCounter$ walk
// from re-asking an already-answered PUT kind, whose counter count is still
// positive and therefore still enumerates (counterchoice1 — the
// movecounter1 livelock's exact class).
func (e *Engine) seedAorAsk(obj state.ObjID, ctx *effects.Ctx) {
	set := e.aorAsk[obj]
	if set == nil {
		return
	}
	kinds := make([]string, 0, len(set))
	for k := range set {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds) // deterministic: map iteration order never reaches a Ctx
	ctx.AorAnswered = kinds
}

// handleModes applies an answered KModes decision. ResumeKind and the trigger
// drain flag distinguish three lifetimes: a modal spell's CR 601.2b cast
// proposal, a modal trigger's CR 603.3c placement, and an effect suspended in
// mid-resolution (including unless-pay). Every branch records ModeChosen; the
// first two also cache the chosen SVar names on the stack object so resolution
// executes the announcement without asking again.
func (e *Engine) handleModes(d *decision.Decision, in decision.Intent) {
	if ma, rp := e.takeOffStackManaRider(); ma != nil {
		e.resume = rp
		template := *ma
		template.nestedResume = nil
		asked := e.withOffStackMana(template, func() { e.handleModes(d, in) })
		e.finishOffStackManaRider(ma, asked)
		return
	}
	// An activated mana ability resolves outside the stack. Its UnlessCost$
	// answer is therefore owned by the mana activation flow rather than an
	// effects resume point, but is still recorded like every KModes answer.
	if d.ResumeKind == "villainous" {
		if e.resume == nil {
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "villainous choice answered with no resolution suspended"})
			return
		}
		rp := e.resume
		e.resume = nil
		chosen := d.Chosen(in)
		if len(chosen) > 0 && chosen[0].Index >= 0 && chosen[0].Index < len(d.ResumeModes) {
			rp.villainousChoice = d.ResumeModes[chosen[0].Index]
		}
		e.emit(events.Event{Kind: events.ModeChosen, Obj: rp.obj, Player: in.Player,
			Text: strings.Join(chosenModeLabels(chosen), ",")})
		e.resumeResolution(rp, chosen)
		return
	}
	// A multi-player api:GenericChoice's per-chooser KModes answer. Like the
	// villainous arm, the answer is scoped to one chooser: record the chosen
	// SVar name and resume the GenericChoice SA, which runs that chooser's body
	// and then asks the next Defined$ chooser. The cursor itself rides the
	// resume point (ResumeGenericChoosers/Index), so it is not re-derived here.
	if d.ResumeKind == "generic_players" {
		if e.resume == nil {
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "generic choice answered with no resolution suspended"})
			return
		}
		rp := e.resume
		e.resume = nil
		chosen := d.Chosen(in)
		if len(chosen) > 0 && chosen[0].Index >= 0 && chosen[0].Index < len(d.ResumeModes) {
			rp.genericChoice = d.ResumeModes[chosen[0].Index]
		}
		e.emit(events.Event{Kind: events.ModeChosen, Obj: rp.obj, Player: in.Player,
			Text: strings.Join(chosenModeLabels(chosen), ",")})
		e.resumeResolution(rp, chosen)
		return
	}
	if d.ResumeKind == "mana_unless" {
		chosen := d.Chosen(in)
		labels := chosenModeLabels(chosen)
		e.emit(events.Event{Kind: events.ModeChosen, Obj: d.Source, Player: in.Player,
			Text: strings.Join(labels, ",")})
		e.choosing = chooseNone
		e.answerManaUnless(chosen)
		return
	}

	// CR 601.2b cast branch: the spell is already provisionally on the stack,
	// but no targets have been selected and no cost has been paid. Record the
	// answer on that spell, then resume the cast transaction at target choice.
	// CR 702.55 dredge: a draw-step draw replaced by a graveyard dredge is
	// NOT a stack object, so the ordinary mid-resolution resume path (which
	// re-enters a suspended stack-object resolution) does not apply. Handle
	// the answered dredge here: option 0 mills the dredge card's N and returns
	// it to hand (the ordinary draw is already skipped by the ask's
	// suspension), option 1 (or an empty answer) lets the draw happen, which
	// the suspended DrawFor re-runs as the ordinary draw.
	if d.ResumeKind == "dredge" && (e.resume == nil || e.resume.direct) {
		// A turn-based draw has no enclosing stack resolution to re-enter.
		// A Draw API on a resolving spell/ability instead falls through to
		// resumeResolution below, which restores its cursor and finishes every
		// remaining draw and SubAbility$ exactly once.
		ch := d.Chosen(in)
		if len(ch) > 0 && ch[0].Kind == "dredge" {
			e.applyDredge(in.Player, ch[0].Obj)
		} else {
			e.resumeOrdinaryDraw(in.Player)
		}
		// A GainLife→Draw replacement body parked its remaining draws on this
		// ask (replacement.go's lifeReplacementDraw): the answer resolved the
		// draw that asked, so re-drive the rest -- which may park again on
		// the next dredge ask -- and then drain any replacement-order queue
		// the interrupted pass left behind.
		if rp := e.resume; rp != nil && rp.lifeDraws > 0 {
			rest := rp.lifeDraws
			e.resume = nil
			e.lifeReplacementDraw(in.Player, rest)
			e.askNextReplacementChoice()
			return
		}
		e.resume = nil
		return
	}
	if d.ResumeKind == "cast_modes" {
		e.applyCastModes(d, in.Player, d.Chosen(in))
		return
	}

	// CR 603.3c placement branch: this KModes decision was asked by the
	// trigger drain (pushTrigger's askTriggerModes) rather than posed
	// mid-resolution by an effect. There is no suspension to resume -- the
	// answer is recorded onto the trigger's stack object (ChosenModes, a
	// cache of the logged answer) so resolveTop builds Ctx.Modes from it and
	// effCharm runs exactly the chosen modes instead of asking again -- and
	// the drain resumes through the same continuation every other trigger
	// drain answer uses. drainAwaitsModes identifies the branch; it is false
	// for a mid-resolution ask, which falls through to the resume path below.
	if e.drainAwaitsModes {
		e.drainAwaitsModes = false
		chosen := d.Chosen(in)
		labels := chosenModeLabels(chosen)
		names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes)
		if len(e.G.Stack) > 0 {
			id := e.G.Stack[len(e.G.Stack)-1]
			var so *state.Object
			if o := e.G.Obj(id); o != nil {
				so = o
				// modeChoiceNames is non-nil even for zero chosen modes,
				// so a zero-mode placement resolves as nothing.
				o.ChosenModes = names
			}
			// ChoiceRestriction$: record the placement pick on the trigger's
			// SOURCE (the permanent), not on the transient stack object, so the
			// next trigger instance of the same Charm sees it -- including when
			// a second instance is already waiting in the queue. A no-op unless
			// the SA carries the param.
			if so != nil {
				effects.RecordCharmChoices(e, so.Source, d.ResumeSA, names)
			}
			e.emit(events.Event{Kind: events.ModeChosen, Obj: id, Player: in.Player,
				Text: strings.Join(labels, ",")})
			// A trigger Charm's targeting lives INSIDE its modes (the
			// corpus pairs ValidTgts$ on the chosen mode's SVar body,
			// never on the ability — Charming Scoundrel's DBToken), so the
			// modes the player just chose ask their targets now, at the same
			// placement moment CR 603.3c puts the mode choice. The cross-mode
			// TargetUnique family (Shadrix Silverquill, the duo cycle, Balor,
			// Vindictive Lich, Chaos Balor) asks ONE combined KTarget over the
			// shared player pool with per-player Option.Group exclusivity —
			// Decision.Validate's mutual-exclusion rule IS the "each mode must
			// target a different player" rule, enforced on the wire — and the
			// answers attribute to the target-bearing modes positionally in
			// chosen-mode order (ask order == chosen order == replay order).
			// Distinct single-target modes use their own grouped options and
			// target bindings. Other declarations keep their legacy ask path.
			// The answer lands on the stack object through handleTarget's
			// ordinary record; effToken (TokenOwner$ ThisTargetedPlayer) and
			// friends read the mode's own c.Targets.
			if so != nil && so.Ability != nil {
				if src := e.G.Obj(so.Source); src != nil && src.Face() != nil {
					svars := src.Face().SVars
					var tbms []*cards.SA
					for _, name := range names {
						if sub := cards.ResolveSVar(svars, name); sub != nil &&
							strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
							tbms = append(tbms, sub)
						}
					}
					choices := strings.Split(so.Ability.Params["Choices"], ",")
					for i := range choices {
						choices[i] = strings.TrimSpace(choices[i])
					}
					status, why := effects.CharmCrossModeShape(svars, choices)
					if status == effects.CharmUniqueSupported && len(tbms) >= 2 {
						e.drainAwaitsTarget = true
						if e.askCrossModeCharmTargets(in.Player, id, tbms) {
							return
						}
						// Fewer legal candidates than target-bearing modes: the
						// combined different-player ask is unsatisfiable. Fall
						// through to the historical first-mode ask below, whose
						// own bounds/insufficiency handling governs.
					} else if status == effects.CharmUniqueUnsupported {
						e.emit(events.Event{Kind: events.Note, Obj: id,
							Text: "cross-mode TargetUnique$ Charm shape unimplemented: " + why})
					} else if len(tbms) >= 2 {
						asked, infeasible := e.askCharmModeTargets(in.Player, id, svars, so.Ability, names)
						if infeasible {
							// CR 603.3c: a triggered ability whose announced modes
							// cannot all acquire mandatory targets cannot resolve.
							// Remove it instead of posing an unanswerable decision
							// or silently targeting only the first mode.
							e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack,
								To: state.ZExile, Text: "countered: no legal modal targets"})
							e.ensureLeftTheStack(id, state.ZExile, "a replacement discarded an untargetable modal ability's move")
							e.drainAwaitsTarget = false
							e.resumeTriggerDrain()
							return
						}
						if asked {
							e.drainAwaitsTarget = true
							return
						}
					}
					for _, sub := range tbms {
						e.drainAwaitsTarget = true
						e.askTarget(in.Player, id, sub)
						return
					}
				}
			}
		}
		e.resumeTriggerDrain()
		return
	}
	if e.resume == nil {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "modes answered with no resolution suspended"})
		return
	}
	rp := e.resume
	e.resume = nil
	chosen := d.Chosen(in)
	labels := chosenModeLabels(chosen)
	if d.ResumeSA != nil && strings.EqualFold(d.ResumeSA.Params["SetChosenMode"], "True") && len(chosen) == 1 {
		// An as-enters GenericChoice records its mode on the permanent via
		// the event fold; the ModeChosen marker alone stores no object state.
		if names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes); len(names) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: d.Source, Counter: "mode", Text: names[0]})
		}
	}
	e.emit(events.Event{Kind: events.ModeChosen, Obj: rp.obj, Player: in.Player,
		Text: strings.Join(labels, ",")})
	// ChoiceRestriction$: a mid-resolution Charm's pick is recorded on its
	// source as well, so a later instance is restricted against it.
	if o := e.G.Obj(rp.obj); o != nil {
		effects.RecordCharmChoices(e, o.Source, d.ResumeSA,
			modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes))
	}
	e.resumeResolution(rp, chosen)
}

// resumeETBEntry is the resolution-owned continuation for an as-enters
// choice. Keeping the e.resume write here preserves the structural invariant
// that only resolution machinery consumes a suspended frame.
func (e *Engine) resumeETBEntry(chosen []decision.Option) state.ObjID {
	// handleChoose owns clearing e.resume; this continuation only consumes the
	// parked entry, keeping the archtest's single ownership rule intact.
	if e.etbMove == nil || len(chosen) != 1 {
		e.etbMove = nil
		e.etbNext = 0
		e.choosing = chooseNone
		return 0
	}
	move := *e.etbMove
	opt := chosen[0]
	switch opt.Kind {
	case "name":
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "name", Text: opt.Label})
	case "type":
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "type", Text: opt.Label})
	case "number":
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "number", Amount: int32(opt.Amount)})
	case "color":
		if letter := etbColourLetter(opt.Label); letter != "" {
			e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "color", Text: letter})
		}
	case "riot":
		choice := "haste"
		if opt.Index == 0 {
			choice = "counter"
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "riot", Text: choice})
	case "unleash":
		choice := "plain"
		if opt.Index == 0 {
			choice = "counter"
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "unleash", Text: choice})
	case "clone":
		// The ETB-copy election (K:ETBReplacement:Copy). The chosen template
		// rides the event's IDs; the decline ("Enter as itself") carries no
		// object, so the fold records an answered-but-empty choice and the
		// ETBReplacement body -- effects' effClone, reached at the re-emitted
		// move below -- leaves the object entering as itself.
		ids := []state.ObjID(nil)
		if opt.Obj != 0 {
			ids = []state.ObjID{opt.Obj}
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "clone", IDs: ids})
	case "paylife":
		// The announced life payment of an "as CARDNAME enters, pay any amount
		// of life" replacement (Minion of the Wastes / Phyrexian Processor /
		// Nameless Race). The announced X is recorded as a Choose "number"
		// entry (the same fold a ChooseNumber uses), bound onto the object as
		// its paid X (events.XChange, so replCtx's `X: o.X` hands it to the
		// replacement body's Count$xPaid), and paid as one LifeChange before
		// the move is re-emitted -- the body then stores the paid amount
		// through events.StoreSVar. CR 118.3 (paying life), CR 601.2b
		// (announcing X).
		x := int32(opt.Amount)
		if x < 0 {
			x = 0
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "number", Amount: x})
		e.emit(events.Event{Kind: events.XChange, Obj: move.Obj, Amount: x})
		if x > 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: opt.Player, Amount: -x})
		}
	}
	e.choosing = chooseNone
	e.emit(move)
	// The answered entry has been re-emitted: if it was a land play and the
	// entry was fully replaced (or replaced again after another as-enters
	// answer), settle the land play here rather than leaving the continuation
	// armed for an unrelated later entry to consume.
	e.settleLandPlayIfDone(move.Obj)
	return move.Obj
}

// continueAfterETBEntry hands an as-enters entry choice's answer back to the
// resolution that entry interrupted. Engine.Ask parks the resolving object on
// every mid-resolution ask, and applyETBChoiceReplacement's ask is posed from
// inside emit, so the frame it parks is whatever effect was moving the object
// onto the battlefield (a reanimation, a blink, Retether's mass Aura return).
// resumeETBEntry has already completed the entry itself, so the frame resumes
// with no answer: its recorded continuation (rp.outer) runs and the stack
// object is finished, instead of being left on the stack for resolveTop to
// resolve a second time.
//
// Direct frames have no interrupted stack resolution, and frames whose object
// has left the stack independently have no remaining resolution to finish. A
// permanent spell's own entry is different: its parked move is re-emitted by
// the answer, so resolveTop never reached its completion tail. Finish that
// spell here, from the same resolution-owned continuation used by other
// suspended resolutions.
func (e *Engine) continueAfterETBEntry(rp *resumePoint, entry state.ObjID) {
	if rp == nil || rp.direct || rp.obj == 0 || e.pending != nil {
		return
	}
	if rp.obj == entry {
		e.finishResumption(rp.obj)
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
		return
	}
	if o := e.G.Obj(rp.obj); o == nil || o.Zone != state.ZStack {
		return
	}
	e.resumeResolution(rp, nil)
}

// resumeResolution re-enters a suspended resolution with its answer. It
// rebuilds the same Ctx resolveTop built for the object on its first pass
// (Source/Controller/Targets/Remembered and the SVar table are all
// re-derivable from the stack object, which has not moved), attaches the
// answer, and re-runs the suspended sub-ability — effects.Resolve walks
// from it through the rest of the chain, which is precisely the
// continuation that had not run yet. If that continuation asks again the
// new pending point is linked after this one's own continuation and the
func unlessPayChoice(chosen []decision.Option) (decision.Option, bool) {
	for _, option := range chosen {
		if option.Mode == decision.ModeUnlessPay {
			return option, true
		}
	}
	return decision.Option{}, false
}

// object stays on the stack; otherwise — once the re-entry and any outer
// continuation it carries have all completed — the fully-resolved object
// goes where resolveTop's own tail would have sent it.
func (e *Engine) resumeResolution(rp *resumePoint, chosen []decision.Option) {
	// A card-name answer is carried on the frame itself: every NameCard
	// resume reads ctx.NameChoice from rp.name, and binding it here is the
	// ONE home for that interpretation. Routing the answer through any
	// handler that consumes a resume point (handleChoose's general KChoose
	// arm, the off-stack mana rider's answerManaColor, a trigger-optional
	// re-entry) therefore all bind it identically; a path that skipped this
	// re-posed the same NameCard ask forever (the off-stack mana NameCard
	// livelock), because resumed NameCard reads an empty NameChoice and asks
	// again.
	if rp.kind == "name" && len(chosen) == 1 {
		rp.name = chosen[0].Label
	}
	if rp.kind == "turn_face_up_event" {
		prior := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(rp.event)
		e.applyingReplacement = prior
		e.resume = nil
		if rp.outer != nil {
			e.resumeResolution(rp.outer, nil)
		}
		return
	}
	if rp.kind == "deferred_ask" {
		// A deferred second ask (Engine.Ask): everything chained before it
		// has run, so pose it now and park on the resume point captured when
		// it was asked. Its answer continues at this frame's outer.
		inner := rp.deferredResume
		inner.outer = rp.outer
		e.lastDeferred = nil
		e.ask(rp.deferredAsk)
		e.resume = inner
		return
	}
	if rp.kind == "copy_targets" {
		e.resume = nil
		if rp.obj != 0 {
			// A multi-declaration copy (a fused half, a modal declaration) is
			// asked once per declaration. Each answer is ACCUMULATED here and
			// the whole flat list is recorded ONCE, after the LAST
			// declaration -- recording the first stage's answer immediately
			// would REPLACE o.Targets and destroy the later declarations'
			// inherited keep-current slots. copyTargetStage[obj] is the NEXT
			// stage, so the stage just answered is one less.
			stage := e.copyTargetStage[rp.obj] - 1
			if stage < 0 {
				stage = 0
			}
			if e.copyAnswerTargets == nil {
				e.copyAnswerTargets = make(map[state.ObjID][][]decision.Option)
			}
			ans := e.copyAnswerTargets[rp.obj]
			for len(ans) <= stage {
				ans = append(ans, nil)
			}
			ans[stage] = append(ans[stage][:0], chosen...)
			e.copyAnswerTargets[rp.obj] = ans
			// More declarations still owe an ask: re-enter resolveTop, whose
			// AskCopyTargets guard reads copyTargetStage and poses the next
			// one. Nothing is recorded yet.
			decls := e.copyTargetDeclarations(e.G.Obj(rp.obj))
			if e.copyTargetStage[rp.obj] < len(decls) {
				e.resolveTop()
				return
			}
			// Every declaration answered: record the flattened list in
			// declaration order (option 0 replaces, the rest append) and
			// publish the per-declaration split for a fused copy, so
			// resolveFused hands each half exactly the targets chosen FOR it
			// (the same split a cast publishes at payment).
			flat := make([]decision.Option, 0, len(chosen))
			stages := make([][]state.Target, len(ans))
			for i, sl := range ans {
				flat = append(flat, sl...)
				stages[i] = targetOptions(sl)
			}
			if len(flat) == 0 {
				flat = chosen
			}
			e.recordChosenTargets(rp.obj, flat, false)
			if o := e.G.Obj(rp.obj); o != nil && o.CastFlags&state.FlagFused != 0 {
				if ff, _ := fusedSplitFaces(o); ff != nil {
					if sa := ff.SpellAbility(); sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
						stages = append([][]state.Target{nil}, stages...)
					}
				}
				if e.fuseTargets == nil {
					e.fuseTargets = make(map[state.ObjID][][]state.Target)
				}
				e.fuseTargets[rp.obj] = stages
			}
			delete(e.copyAnswerTargets, rp.obj)
		}
		e.resolveTop()
		return
	}
	// A GainLife→Draw replacement body parked its remaining draws on this
	// ask (replacement.go's lifeReplacementDraw). The body is not a stack
	// resolution: there is no sub-ability to re-enter (rp.sa is nil -- the
	// loop called DrawFor directly, which poses its own dredge ask). The
	// signature is exact: DrawFor is the only sa==nil dredge asker (effDraw's
	// frames carry ResumeSA, and a turn-based draw's direct frame never
	// reaches here -- handleModes routes those to its own arm). The answer
	// resolved the draw that asked, whether or not draws remain parked
	// (lifeDraws == 0 is the FINAL draw of the body -- findings-sol5: the old
	// lifeDraws > 0 gate dropped that frame's answer into the no-sub-ability
	// Note below); apply it and re-drive the rest (which may park again on
	// the next dredge ask). The frame's rp.outer continuation and the
	// completion tail below still run after it -- the same order the body ran
	// in before it suspended -- and the drain at this cascade's true end
	// picks up any replacement-order queue the interrupted pass left behind.
	parkedDraws := false
	if rp.kind == "dredge" && rp.sa == nil {
		parkedDraws = true
		if !e.G.Players[rp.player].Lost {
			// CR 800.4f: a departed player makes no choice and draws
			// nothing; the outer continuation below still runs.
			if len(chosen) > 0 && chosen[0].Kind == "dredge" {
				e.applyDredge(rp.player, chosen[0].Obj)
			} else {
				e.resumeOrdinaryDraw(rp.player)
			}
			e.lifeReplacementDraw(rp.player, rp.lifeDraws)
			if e.Suspended() || e.pending != nil {
				// The re-drive parked on the next dredge ask: that frame's
				// own resume arms carry the rest. The new pending point is
				// fresh (outer nil -- Ask builds it bare, and nothing in this
				// re-drive reports continuations), so link the interrupted
				// frame's own continuation onto it -- the same fx32 linking
				// discipline the e.resume != nil branch below practises --
				// or the cascade's last frame would complete the object
				// without ever running what this interrupted resolution was
				// still carrying (findings-sol5: the sub-ability after the
				// GainLife never ran). Every later park re-links the same
				// chain, so rp.outer survives until the cascade truly ends.
				if e.resume != nil {
					e.resume.outer = rp.outer
				}
				return
			}
		}
	}
	before := e.triggerBefore
	e.triggerBefore = rp.before
	defer func() { e.triggerBefore = before }()
	savedResolving := e.resolvingObj
	e.resolvingObj = rp.obj
	defer func() { e.resolvingObj = savedResolving }()
	o := e.G.Obj(rp.obj)
	if o == nil || (o.Zone != state.ZStack && !rp.replacement && !rp.direct) {
		// The suspended object left the stack while the decision was
		// outstanding. Nothing but the answer can un-freeze the engine, so
		// this is unreachable in a well-formed match; it degrades to a
		// no-op rather than panicking, the same totality stance as every
		// other resolution exit.
		return
	}
	if rp.fuseAlt != nil {
		// A fuse-rest continuation (CR 702.101b): run the captured remaining
		// halves of the fused split spell, each with its own filtered target
		// slice (rules/split.go's runFusedHalves). A further suspension parks
		// below with the rest already chained; when the last half ran
		// unsuspended, this frame's shared completion tail runs -- the same
		// finishResumption + priority-reset shape every outermost frame takes.
		if cont, suspended := e.runFusedHalves(o, rp.fuseAlt.halves, rp.fuseAlt.sas, rp.fuseAlt.targets,
			rp.fuseAlt.from, rp.outer); suspended {
			if e.resume != nil {
				e.resume.outer = cont
			}
			return
		}
		if rp.outer != nil {
			e.resumeResolution(rp.outer, nil)
			return
		}
		e.finishResumption(rp.obj)
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
		return
	}
	// A chain can park between two pre-asked subs. Recheck the stored
	// answers against the live board before the resumed effects walk uses
	// them; the first pass in resolveTop already checked those reached earlier.
	if o.Ability != nil {
		e.recheckCastSubTargets(rp.obj, o.Ability, o.Controller, o.Source)
	} else if f := o.Face(); f != nil {
		e.recheckCastSubTargets(rp.obj, f.SpellAbility(), o.Controller, rp.obj)
	}
	ctx := &effects.Ctx{Source: rp.obj, Controller: o.Controller, NameChoice: rp.name, ChosenDirection: rp.chosenDirection, Targets: o.Targets,
		// Forge's Count$ResolvedThisTurn: a chain that suspended at a
		// mid-resolution ask and so re-enters HERE instead of through
		// resolveTop must keep the tally its first pass read. The Resolve event
		// was emitted once, so the tally is unchanged across the ask, but a
		// fresh Ctx defaults the field to zero and a gate would then read the
		// wrong ordinal (Sephiroth would transform a turn early on the
		// resumed pass). resolvedAbilityTally is the same read resolveTop's
		// ability branch makes, in one home.
		ResolvedThisTurn:    e.resolvedAbilityTally(o),
		ClashContinuation:   cloneClashResume(rp.clash),
		ActivationsThisTurn: e.activationsThisTurnFor(o.Source, o.Ability),
		// alltargeted1: a re-entered walk keeps consuming the cast flow's
		// pre-asked sub-ability target answers (kept until the stack object
		// leaves, so both a later sub and a suspended body can use theirs).
		// A modal root is excluded from the pre-ask whole
		// (collectSubTargetPreAsks), so this map is empty exactly where
		// ModeTargets carries the per-mode groups instead: the two bindings
		// are disjoint by construction, never competing for one chain.
		SubPreAsk:   e.castSubTargets[rp.obj],
		ModeTargets: cloneCharmTargetGroups(e.charmTargets[rp.obj]),
		Chosen:      append([]state.Target(nil), rp.choices...), ChosenValid: rp.chosenValid,
		DigUntilMove: rp.digUntilMove, DigUntilMoveDone: rp.digUntilMoveDone,
		ClonePick: rp.clonePick, ClonePickDone: rp.clonePickDone,
		VillainousVictims: append([]state.Target(nil), rp.villainousVictims...),
		VillainousIndex:   rp.villainousIndex,
		ChoiceTarget:      rp.target,
		// The pre-move controller snapshot of this resolution's object
		// targets, carried across the suspension: a resumed frame's Ctx is
		// rebuilt from the LIVE objects (whose controllers any completed
		// Destroy has already reset to their owners), so without this a
		// chained TokenOwner$ TargetedController sees the wrong seat. The
		// map is keyed by target ObjID, so a frame whose Targets are later
		// narrowed (a fused half's slice, a Charm mode's target) still
		// resolves the entries it names.
		TargetControllerLKI: effects.CloneTargetControllerLKI(rp.targetControllerLKI),
		TargetCountersLKI:   effects.CloneTargetCountersLKI(rp.targetCountersLKI),
		TargetSpellLKI:      effects.CloneTargetSpellLKI(rp.targetSpellLKI),
		// The resolving stack-object wrapper, same anchor resolveTop's
		// branches set: a SUSPENDED-then-resumed ability (Ulalek's pay ask is
		// exactly such a suspension) keeps the ValidStack otherAbility
		// exclusion pointed at its own wrapper on re-entry. The replacement
		// arm below may rebind ctx.Source to the replacement's host;
		// ResolvingObj stays rp.obj -- the wrapper whose resolution this
		// frame is.
		ResolvingObj: rp.obj, EffectFrame: rp.effectFrame,
		// The shared coin-flip memory the chain had at the ask. Re-attached so
		// a chained Defined$ FlippedTails / Wins reader keeps the flips
		// performed before the suspension (Goblin Assassin's per-loser
		// sacrifice asks once after the flips; without this the second loser's
		// read saw an empty set and never sacrificed).
		FlipMemory: rp.flipMemory}
	// The plural replaced-instruction batch (Ctx.ReplacedCards) follows the
	// same rule as the singular Replaced below: a resumed frame that carries
	// one restores it, so a Cascade body's hidden pick re-resolves
	// ReplacedCards.<qual> against the same exiled batch. Set unconditionally
	// when present, because a continuation frame of a replacement body's
	// resolution (the CascadeResidue SA) runs with rp.replacement false yet
	// still needs the batch.
	if len(rp.replacedCards) > 0 {
		ctx.ReplacedCards = append([]state.ObjID(nil), rp.replacedCards...)
	}
	// Publish this rebuilt Ctx for the whole of the resumed resolution (the
	// same restore-on-return bracket effects.Resolve uses), so an ask posed
	// from RULES machinery before the effects.Resolve re-entry -- the Ward
	// pay window's beginWardPayment, a charm-mode continuation -- carries the
	// chain's TargetUnique$ accumulator (restored from rp.targetsUnique
	// above) onto its own resume point instead of dropping it. The nested
	// effects.Resolve re-publishes the same Ctx and restores to this value on
	// return, so the LIFO restore order is exact.
	prevCtx := e.SetResolutionCtx(ctx)
	defer e.SetResolutionCtx(prevCtx)
	if rp.kind == "repeat_optional" {
		ctx.RepeatOptional = &effects.RepeatOptionalContinuation{
			Continue: len(chosen) > 0 && chosen[0].Kind == "yes",
			Next:     rp.repeatOptionalNext,
		}
	}
	// Cost-sacrificed objects are engine-only LKI keyed by the stack object.
	// Re-entry must restore the same snapshot so an SVar such as Mausoleum
	// Wanderer's Sacrificed$CardPower does not collapse to zero after the
	// unless-pay answer suspends resolution.
	ctx.Sacrificed = e.sacrificedLKI[rp.obj]
	ctx.Exiled = e.castExiled[rp.obj]
	ctx.Revealed = e.castRevealed[rp.obj]
	// CR 107.3i: X is the value paid for the object's {X}, preserved on the
	// stack object by CastInfo -- the same binding resolveTop's spell and
	// ability branches now carry. A spell whose resolution suspends on a
	// mid-resolution ask (modes, discard, dig, unless-pay) keeps the paid
	// X for the rest of the walk instead of resuming with 0. Set here for
	// every resume; the replacement arm below has no other X to restore.
	// For a trigger that suspends, CR 107.3m's binding applies exactly as
	// resolveTop's ability branch does it: the trigger object was never
	// paid an X, so the causing event's card supplies the value.
	ctx.X = o.X
	// CR 601.2b/107.3i: the stack object's own X — an announced, possibly
	// zero payment — binds through the suspension exactly as the value does,
	// so an UnlessCost$ X on a zero-X cast resolves to {0} at the resumed
	// gate instead of staying a raw unpriceable token.
	ctx.XAnnounced = stackXAnnounced(o)
	if rp.replacement {
		// fx44: this suspended frame is a ReplaceWith$ body, so restore the
		// replacement context applyReplacements seeded for it. Ctx.Replaced is
		// the object the replaced event was about (ev.Obj, threaded via
		// rp.replaced), so a Defined$ ReplacedCard resolution still finds its
		// subject after the suspension. Without it the completed move (Mox
		// Diamond's MoveToBattlefield) targets nothing and the object never
		// leaves the stack. Ctx.Remembered is NOT re-seeded with the replaced
		// object: Remembered$Amount inside a replacement body counts the
		// body's own rider-remembered cards (Mox's discarded land, Scorched
		// Ruins' sacrificed two), never the replaced card itself — the same
		// empty-start rule replCtx (rules/replacement.go) documents. The
		// body's own Remembered from before the ask rides rp.remembered. The replacement arm has no other X to restore, so
		// triggerPaidX's fallback below is skipped for it.
		ctx.Replaced = rp.replaced
		ctx.ReplacementTarget = rp.replacementTarget
		ctx.ReplacementSource = rp.replacementSource
		ctx.ReplacementAmount = rp.replacementAmount
		// Remembered is NOT seeded with the replaced/damaged object: no corpus
		// replacement body (Damage ones included — measured, zero use
		// Remembered in a Damage body) reads it, and seeding it made every
		// Remembered$Amount gate or count inside a replacement one too high
		// (Mox Diamond's EQ0/EQ1 discard split broke). The body's own
		// rider-remembered cards ride rp.remembered below.
	} else if ctx.X == 0 {
		ctx.X = e.triggerPaidX(rp.obj, o)
	}
	// The triggered-cost window's dynamic tapXType<X/Spec> payment (the
	// Battlesphere/yotia shape): the election's tap count is the cost's
	// announced X, carried on the resume point. It wins over both reads above
	// -- the trigger object's own X is 0 and the source permanent's X is its
	// cast-time value, not this payment's.
	if rp.tapPaidX != 0 {
		ctx.X = rp.tapPaidX
		ctx.XAnnounced = true
	}
	// The trigger-cost window's X fold (the {X}/{PayLife<X>} announcement:
	// Elenda and Azor, Vizkopa Confessor, Necrodominance): the announced or
	// fixed value binds exactly like the dyn-tap count above, so the body's
	// Count$xPaid / NumCards$ X / TokenPower$ X reads this payment.
	if rp.winPaidX != 0 {
		ctx.X = rp.winPaidX
		ctx.XAnnounced = true
	}
	var svars map[string]string
	if o.Ability != nil {
		// A triggered or activated ability: mirror resolveTop's ability
		// branch — Source is the source permanent, Remembered carries what
		// the trigger captured, and the SVar table comes from that
		// permanent's face.
		ctx.Source = o.Source
		if !rp.replacement {
			ctx.TriggerContext = e.triggerContexts[rp.obj]
		}
		ctx.Remembered = o.Remembered
		ctx.Captured = o.Remembered
		if lki, ok := e.triggerLKI[rp.obj]; ok {
			ctx.LKI = lki.object
			ctx.LKIPower, ctx.LKIToughness, ctx.LKIPTValid =
				lki.power, lki.toughness, lki.ptValid
		}
		if link, ok := e.sourceLifelinkLKI[rp.obj]; ok {
			ctx.SourceLifelinkLKI = link
			ctx.SourceLifelinkLKIValid = true
		}
		if controller, ok := e.sourceControllerLKI[rp.obj]; ok {
			ctx.SourceControllerLKI = controller
			ctx.SourceControllerLKIValid = true
		}
		if lki := e.damageSourceLKI[rp.obj]; lki != nil {
			ctx.DamageSourceLKI = cloneDamageSourceLKI(lki)
		}
		// CR 603.3c: keep the placement-announced mode choice across the
		// suspension, so the resumed resolution of a modal trigger runs
		// exactly the modes chosen when the ability was put on the stack
		// rather than re-asking. The switch below overrides it (with the
		// NESTED answer) only for a nested "modes" resume, which is the
		// correct scoping -- a nested Charm below this one poses its own ask.
		ctx.Modes = o.ChosenModes
		if rp.kind == "villainous" {
			if rp.villainousChoice != "" {
				ctx.Modes = []string{rp.villainousChoice}
			}
			ctx.Remembered = append([]state.Target(nil), rp.remembered...)
		}
		// A frame of the chosen body of a VillainousChoice (or of a nested ask
		// IT posed): the victim is this body's Remembered, not the ability's
		// own trigger capture. Without this a nested ask's re-entry (DBSac's
		// sacrifice picker resolves Defined$ Remembered) rebuilds an empty
		// set and drops the answered sacrifice, and a multi-victim choice
		// stops after the first nested choice. It wins over the o.Ability
		// seed above deliberately -- that is the exact binding the choice
		// needs.
		if rp.villainousRememberedSet {
			ctx.Remembered = append([]state.Target(nil), rp.villainousRemembered...)
			ctx.Captured = append([]state.Target(nil), rp.villainousRemembered...)
		}
		// The same owning-face read resolveTop's ability branch makes: a
		// mutated pile's under-card ability (CR 702.140d) must resume on the
		// UNDER-CARD's SVar table, not the pile's top card's. An ordinary
		// trigger's owning face IS the top face, and an activated ability
		// matches no trigger and falls through to Face(), so both are
		// unchanged.
		if src := e.G.Obj(o.Source); src != nil {
			if _, mf, ok := e.findTriggerForAbilityFace(o.Source, o.Ability); ok && mf != nil {
				svars = mf.SVars
			} else if mf, ok := e.pileFaceForSA(o.Source, o.Ability); ok && mf != nil {
				// An under-card ACTIVATED ability whose resolution SUSPENDED (an
				// asking sub-ability): the resume reads the under-card's own SVar
				// table, the same owning-face rule resolveTop's ability branch
				// applies -- never the pile top's.
				svars = mf.SVars
			} else if sf := src.Face(); sf != nil {
				svars = sf.SVars
			}
		}
	} else if f := o.Face(); f != nil {
		svars = f.SVars
	}
	if rp.replacement {
		// fx44: this suspended frame is a ReplaceWith$ body, so restore the
		// replacement context applyReplacements seeded for it. Ctx.Replaced is
		// the object the replaced event was about (ev.Obj, threaded via
		// rp.replaced), so a Defined$ ReplacedCard resolution and an
		// SVar:X Remembered$Amount gate find their subject after the
		// suspension. Without these the completed move (Mox Diamond's
		// MoveToBattlefield) targets nothing and the object never leaves the
		// stack. (Captured, not Remembered, carries the replaced object on
		// the non-draw path — the same empty-Remembered rule replCtx
		// (rules/replacement.go) documents.)
		ctx.Replaced = rp.replaced
		ctx.ReplacedPlayer = rp.replacedPlayer
		if rp.replacedPlayer.IsPlayer {
			// A Draw replacement body: the draw-er's binding is the whole
			// seed, and the body's own RememberDrawn$ records what it draws —
			// a MoveZone-shaped Remembered seed would pollute the reveal and
			// the discard condition with a stale would-be-drawn entry.
			ctx.Remembered, ctx.Captured = nil, nil
		} else {
			// Ctx.Remembered is NOT re-seeded with the replaced object: a
			// Remembered$Amount gate inside a replacement body counts the
			// body's own rider-remembered cards, never the replaced card
			// itself (seeding it made every such gate one too high).
			ctx.Captured = []state.Target{{Obj: rp.replaced}}
		}
		if rp.remembered != nil {
			ctx.Remembered = append([]state.Target(nil), rp.remembered...)
		}
		// The body's own Source is the replacement's host, which differs from
		// the resolving object when that object caused another permanent's
		// replacement (a reanimation spell and Mox Diamond's discard): the
		// body's choices and SVars belong to the host.
		if rs := rp.replSource; rs != 0 && rs != rp.obj {
			if src := e.G.Obj(rs); src != nil {
				ctx.Source, ctx.Controller, ctx.X = rs, src.Controller, src.X
				ctx.TriggerContext = effects.TriggerContext{}
				svars = nil
				if f := src.Face(); f != nil {
					svars = f.SVars
				}
			}
		}
	}
	if len(rp.villainousVictims) > 0 {
		ctx.VillainousVictims = append([]state.Target(nil), rp.villainousVictims...)
		ctx.VillainousIndex = rp.villainousIndex
	}
	// The multi-player GenericChoice chooser cursor: restored so the chosen
	// body runs for its chooser (Remembered below) and the remaining choosers
	// are still asked. An answered chooser's frame carries genericChoice and
	// the index of the NEXT chooser (advanced by the resume switch below); a
	// continuation frame carries no answer and resumes the loop as it stands.
	if len(rp.genericChoosers) > 0 {
		ctx.GenericChoosers = append([]state.Target(nil), rp.genericChoosers...)
		ctx.GenericChooserIndex = rp.genericChooserIndex
	}
	// The AmountFromVotes$ tally the loop read its per-iteration binding from:
	// the vote is a PRIOR chain link, so the fresh Ctx can only re-derive
	// "Votes" from this restored table. Nil stays nil (a vote that published
	// no tally keeps its unbound/zero read).
	if rp.voteCounts != nil {
		ctx.VoteCounts = cloneVoteCounts(rp.voteCounts)
	}
	if rp.loopBound {
		ctx.Remembered = append([]state.Target(nil), rp.loopRemembered...)
		if rp.repeatSubject != (state.Target{}) {
			ctx.RepeatSubject = rp.repeatSubject
		}
		// Re-bind this iteration's per-subject tally on the rebuilt Ctx, the
		// same scalar effRepeatEach writes before resolving a first-pass body:
		// the resumed body reads "Votes" through runtimePublished, which the
		// restored table alone does not serve. A subject with no tally binds 0
		// (Forge's unset VoteNum read), exactly as the first pass does.
		if rp.voteCounts != nil {
			ctx.VotePublished = 0
			if n, ok := effects.VoteCountForTarget(ctx.VoteCounts, rp.repeatSubject); ok {
				ctx.VotePublished = int32(n)
			}
			ctx.VotePublishedSet = true
		}
	}
	// A mid-resolution ask that rode the walk's Remembered (the hidden-library
	// search sets ResumeRemembered -- a cast spell's Remembered lives only in
	// the resolving Ctx frame, so without the ride the resume rebuilds an
	// empty set and the re-entered primitive's eligibility recheck and the
	// chain's later sub-abilities see nothing, and Card.IsRemembered /
	// Defined$ Remembered in a chained hidden-origin ChangeZone would see an
	// empty list on re-entry too). The loop and replacement branches above
	// are authoritative when they fire; this applies only to the ordinary
	// frames, which never carry rp.remembered otherwise.
	if rp.remembered != nil && !rp.replacement && !rp.loopBound {
		ctx.Remembered = append([]state.Target(nil), rp.remembered...)
	}
	// The search chain's known-card set (Decision.ResumeSearchKnown): a
	// planted placement leg's answer rebuilds a fresh Ctx, and the NEXT leg of
	// the same chain must still label its options with the names the chooser
	// already learned. Runtime continuation state of the search walk, the same
	// class as rp.remembered above.
	if rp.searchKnown != nil {
		ctx.SearchKnown = append([]state.Target(nil), rp.searchKnown...)
	}
	ctx.ForgetOtherSnapshot = append([]state.Target(nil), rp.forgetOtherSnapshot...)
	ctx.ForgetOtherOwners = append([]state.PlayerID(nil), rp.forgetOtherOwners...)
	ctx.ForgetOtherReady, ctx.ForgetOtherCleared = rp.forgetOtherReady, rp.forgetOtherCleared
	// The TargetUnique$ accumulator, captured at ask time: the resumed Ctx
	// re-binds it so a LATER TargetUnique$ rider in the same chain still
	// excludes the targets earlier riders chose (a fresh Ctx would otherwise
	// rebuild the accumulator empty). ctx.Targets itself re-binds from the
	// stack object's flat list above, so the parent-target half of the
	// exclusion set survives the suspension untouched.
	if len(rp.targetsUnique) > 0 {
		ctx.TargetsUnique = append(ctx.TargetsUnique, rp.targetsUnique...)
	}
	// The DamageMap$ True mark set, captured at ask time from the live
	// resolution Ctx: a chain that marks damage and then suspends on a
	// mid-resolution ask before its DB$ DamageResolve re-enters here, and the
	// rebuilt Ctx must still carry the marks the flush is owed (a fresh Ctx
	// would drop them and the flush would silently deal nothing).
	if len(rp.pendingDamage) > 0 {
		ctx.PendingDamage = effects.ClonePendingDamage(rp.pendingDamage)
	}
	// Task mvts1: carry the SA whose targeting the placement/announcement
	// ask covered, exactly as resolveTop's first pass does. An optional
	// trigger's yes (Kor Outfitter) re-enters through here, and without
	// this the re-entered ROOT would re-pose its placement target ask
	// under the generic ValidTgts$ pre-ask.
	if offeredSA := offeredTargetSA(o, svars); offeredSA != nil {
		ctx.OfferedSA = offeredSA
	}
	// A per-mode Charm's mode suspended on its own mid-resolution ask. The
	// generic bind above gave ctx.Targets the stack object's whole flat list
	// -- every selected mode's targets -- so the resumed mode would walk all
	// of them. Re-bind the narrowed group Ask captured, and restore the mode
	// SA as the covered targeting so the pre-ask does not re-pose it.
	if len(rp.charmModeScope) > 0 {
		ctx.Targets = append([]state.Target(nil), rp.charmModeScope...)
		ctx.CharmModeScope = ctx.Targets
		ctx.CharmModeSA = rp.charmModeSA
		if rp.charmModeSA != nil {
			ctx.OfferedSA = rp.charmModeSA
			ctx.TargetsOffered = true
		}
	}
	// A fused half's own mid-resolution ask re-enters here. The generic ctx
	// above binds Targets from the stack object's WHOLE flat target list, and
	// OfferedSA from the front face alone -- for a fused spell the flat list
	// carries BOTH halves' targets and the front face's SA is the other
	// half's, so any frame of the re-entered half would read the other half's
	// targets: a half ROOT re-posing its own ValidTgts$ pre-ask (the spurious
	// "Choose target" after the answered sacrifice, Far // Away) and, just as
	// wrong, a half's SUB-ABILITY reading ParentTargeted$CardPower off the
	// flat list (Flesh // Blood's DBPutCounter counting the sum of both
	// halves' chosen targets). Ask captured the resolving half's own
	// rechecked slice onto this frame (Engine.fusedResolving), so bind it as
	// Targets for EVERY frame of the half. When rp.sa is the half's ROOT its
	// targeting WAS covered by the cast's stage ask, so mark it offered and
	// skip the pre-ask; a sub-ability's targeting was never covered, so its
	// own pre-ask still fires against the half's slice as its parent list.
	if rp.fusedTargetsSet {
		ctx.Targets = rp.fusedTargets
		// The half's OWN SVar table: a fused spell keeps FaceIdx 0, so the
		// generic svars above is the front half's; the re-entered alternate
		// half's sub must resolve its own SVars (Blood's Y).
		svars = rp.fusedSVars
		if _, isRoot := fusedHalfRoot(o, rp.sa); isRoot {
			ctx.OfferedSA = rp.sa
			ctx.TargetsOffered = true
		}
	}
	effects.SetSVars(ctx, svars)
	// An accepted optional trigger may itself carry Cost$ (Mana Vault's
	// "you may pay {4}; if you do" untap). The optional answer chooses to
	// attempt the effect; payment is a separate resolution-time window with
	// mana-ability opportunities. Direct mandatory triggers enter the same
	// window from resolveTop.
	//
	// A Draw-bearing Cost$ joins the two named shapes (Hordewing Skaab's
	// OptionalDecider$ on "you may draw cards ... If you do, discard that
	// many"): the yes answer re-enters here and pays the draw through the
	// same window, rather than running the body for free.
	//
	// trigcost1: the shape test is the shared broadened gate
	// (triggerBodyNeedsCostWindow -- any non-Mandatory Cost$ except Mana /
	// CopySpellAbility), so a Kalastria Highborn `Cost$ B` pays through this
	// arm instead of executing free. CopySpellAbility keeps its own
	// event-role disjunct below.
	tc := e.triggerContexts[rp.obj]
	armed := rp.kind == "optional" && rp.sa != nil &&
		(e.triggerBodyNeedsCostWindow(rp.sa) ||
			// abcopy1: an OptionalDecider$ copy trigger's AB$ CopySpellAbility
			// with a real Cost$ (Rings of Brighthearth, Battlemages' Bracers,
			// Mirari) pays through the same window whenever the trigger context
			// carries an event role -- the activation role TriggerAbility, or
			// the spell-cast arm's TriggerCard (a SpellCast fires on PutOnStack,
			// whose Obj IS the cast spell; no ability wrapper is minted). Only
			// a context-less synthetic push keeps the free-executor semantics.
			(rp.sa.API == "CopySpellAbility" && rp.sa.Params["Cost"] != "" &&
				(tc.TriggerAbility != 0 || tc.TriggerCard != 0)))
	if armed {
		e.startTriggeredEffectCost(rp, ctx.Source)
		return
	}
	if rp.sa != nil {
		switch rp.kind {
		case "dredge":
			// CR 702.55 replaces exactly the one draw that asked. The enclosing
			// Draw cursor advances only after the replacement (or declined
			// ordinary draw) completes; effDraw then re-enters at that cursor
			// and performs all remaining draws before its SubAbility$.
			if len(chosen) > 0 && chosen[0].Kind == "dredge" {
				e.applyDredge(rp.player, chosen[0].Obj)
			} else {
				e.resumeOrdinaryDraw(rp.player)
			}
			ctx.DrawDone = int32(rp.target + 1)
			if rp.uptoIdx >= 0 {
				// An Upto$ Draw's answered batch parked on this Dredge ask: the
				// rider (Decision.ResumeUptoIdx/Count -> the resume point)
				// restores the in-flight target so effDraw's upto branch
				// continues it instead of re-asking its decision.
				ctx.DrawUptoIdx = int32(rp.uptoIdx)
				ctx.DrawUptoCount = rp.uptoCount
				ctx.DrawUptoAnswered = true
			}
		case "mana_color":
			// A resolution-time Mana effect asked for one colour, or an
			// allocation of Combo's produced units. The answer is carried in
			// the chosen options' structured ManaSymbol and consumed by
			// effMana on re-entry; no event kind is needed because the
			// resulting ManaAdd is the replayable state mutation.
			for _, option := range chosen {
				// The chosen colour is structured data (Option.ManaSymbol);
				// labels are presentation-only.
				colour := option.ManaSymbol
				if len(colour) != 1 || !strings.Contains("WUBRG", colour) {
					continue
				}
				if len(chosen) == 1 {
					ctx.ManaChoice = colour
				} else {
					ctx.ManaChoices = append(ctx.ManaChoices, colour)
				}
			}
		case "repeat":
			// A RepeatEach loop re-entered after one of its iterations
			// suspended: no answer, just the cursor (CR 608.2c).
			if cur := rp.repeat; cur != nil {
				ctx.Repeat = &effects.RepeatCursor{SA: rp.sa, Subjects: cur.subjects, Next: cur.next,
					Last: cur.last, HasLast: cur.hasLast}
			}
		case "repeat_choose_order":
			// A RepeatEach ChooseOrder$ loop's before-the-loop ordering ask was
			// answered. Its loop frame is rp.outer (SuspendRepeat parked it with
			// ChooseOrder set); consume it here so the loop is re-entered exactly
			// once, with the subject order the answer named, rather than a
			// second time through the outer recursion. Each option's Index is
			// the subject's position in the offered (selector/scan) order, so
			// the answer is applied by permuting the cursor's subject slice --
			// the subjects are never re-derived after the ask, and every later
			// mid-loop suspension copies the reordered slice. The loop's own
			// accumulated bindings ride the frame exactly as the
			// repeat_each_optional arm carries them.
			if lf := rp.outer; lf != nil && lf.kind == "repeat" && lf.repeat != nil && lf.repeat.chooseOrder {
				ordered := append([]state.Target(nil), lf.repeat.subjects...)
				// A well-formed answer is a permutation (Min == Max == len); a
				// malformed one (unreachable past Decision.Validate) keeps the
				// offered order rather than dropping or duplicating a subject.
				if len(chosen) == len(ordered) {
					seen := make([]bool, len(ordered))
					ok := true
					for pos, o := range chosen {
						if o.Index < 0 || o.Index >= len(ordered) || seen[o.Index] {
							ok = false
							break
						}
						seen[o.Index] = true
						ordered[pos] = lf.repeat.subjects[o.Index]
					}
					if !ok {
						ordered = append([]state.Target(nil), lf.repeat.subjects...)
					}
				}
				ctx.Repeat = &effects.RepeatCursor{SA: rp.sa, Subjects: ordered,
					Next: lf.repeat.next, Last: lf.repeat.last, HasLast: lf.repeat.hasLast}
				if lf.loopBound {
					rp.loopBound = true
					rp.loopRemembered = append([]state.Target(nil), lf.loopRemembered...)
				}
				if lf.voteCounts != nil {
					rp.voteCounts = cloneVoteCounts(lf.voteCounts)
				}
				rp.outer = lf.outer
			}
		case "repeat_each_optional":
			// A RepeatEach RepeatOptionalForEachPlayer$ election was answered.
			// Its loop frame is rp.outer (SuspendRepeat parked it with Election
			// set); consume it here so the loop is re-entered exactly once, at
			// the OFFERED subject, rather than a second time through the outer
			// recursion. The loop's own accumulated Remembered (the frame's
			// loopRemembered, i.e. the suspension's Outer) replaces the
			// iteration Body SuspendRepeat bound to this head, so the re-entered
			// loop continues from the same bindings the first pass had. The
			// answer itself rides Ctx.RepeatEachOptional: effects runs subject
			// Next's body on a yes and skips it on a no.
			if lf := rp.outer; lf != nil && lf.kind == "repeat" && lf.repeat != nil && lf.repeat.election {
				ctx.Repeat = &effects.RepeatCursor{SA: rp.sa,
					Subjects: append([]state.Target(nil), lf.repeat.subjects...),
					Next:     lf.repeat.next, Last: lf.repeat.last, HasLast: lf.repeat.hasLast}
				ctx.RepeatEachOptional = &effects.RepeatEachOptionalContinuation{
					Next:   int32(lf.repeat.next),
					Accept: len(chosen) > 0 && chosen[0].Kind == "yes",
				}
				if lf.loopBound {
					rp.loopBound = true
					rp.loopRemembered = append([]state.Target(nil), lf.loopRemembered...)
				}
				if lf.voteCounts != nil {
					rp.voteCounts = cloneVoteCounts(lf.voteCounts)
				}
				rp.outer = lf.outer
			}
		case "repeat_optional_loop":
			if cur := rp.repeat; cur != nil {
				// The body of iteration cur.next-1 completed after its own
				// suspension: the repeat election for cur.next has not been
				// posed, so AskElection re-enters the loop at the election
				// rather than running the body directly.
				ctx.RepeatOptional = &effects.RepeatOptionalContinuation{Continue: true, Next: int32(cur.next),
					AskElection: true}
			}
		case "unless_pay":
			payOption, chosePay := unlessPayChoice(chosen)
			if rp.unlessPay != "" {
				ctx.UnlessPay = rp.unlessPay
				ctx.UnlessNext = rp.target
				if len(rp.unlessDiscards) > 0 {
					ctx.UnlessDiscarded = make([]state.Target, 0, len(rp.unlessDiscards))
					for _, id := range rp.unlessDiscards {
						ctx.UnlessDiscarded = append(ctx.UnlessDiscarded, state.Target{Obj: id})
					}
				}
				break
			}
			// The payer chose the explicitly marked pay option or declined.
			// Payment happens HERE, in rules, because payMana owns the cost grammar and
			// emits the ManaAdd events — so a replay re-derives the identical
			// payment. An answer to pay from a payer that cannot cover the cost
			// is a decline: the effect's body runs (or not) per its orientation,
			// Ward has non-mana payment forms (sacrifice, discard, tap and
			// several keyword-specific costs). Its payment handler owns those
			// choices; ordinary unless-pay effects retain the shared mana path.
			if rp.sa.API == "Ward" {
				if chosePay {
					paid, asked := e.beginWardPayment(rp, ctx)
					if asked {
						return
					}
					if paid {
						ctx.UnlessPay = "pay"
					} else {
						ctx.UnlessPay = "decline"
					}
				} else {
					ctx.UnlessPay = "decline"
				}
				break
			}
			// Sacrifice's damage-payment offer (Vexing Devil's UnlessCost$
			// DamageYou<4>, UnlessPayer$ Opponent, UnlessSwitched$ True):
			// "paying" is TAKING THE DAMAGE, which the mana path below cannot
			// express — ParseCost silently substitutes an unknown spelling for
			// a flat {1} and would charge one floating mana for four damage.
			// Payment happens HERE, in rules (the same split that owns
			// payMana's events): the accepting opponent's Damage event is
			// emitted from the offering permanent, and the answered
			// UnlessPay re-enters effSacrifice, which then sacrifices (the
			// switched orientation: paying CAUSES the sacrifice). The resume
			// point's target cursor travels with the answer so the effect can
			// offer the next opponent after a decline.
			if rp.sa.API == "Sacrifice" {
				if n, dmg := effects.ParseDamageUnlessCost(rp.sa.Params["UnlessCost"]); dmg {
					if chosePay {
						e.payUnlessDamageCost(ctx, payOption.Player, n)
						ctx.UnlessPay = "pay"
					} else {
						ctx.UnlessPay = "decline"
					}
					// The answering payer's cursor travels in UnlessNext (the
					// same field every other unless-pay answer uses): a decline
					// re-entry resumes the offer at payers[idx+1], and the last
					// decline ends the ask. (UnlessPayTarget was a vestigial
					// second cursor nothing read — its one write is this line —
					// so a second opponent's decline re-offered payers[1]
					// forever.)
					ctx.UnlessNext = rp.target
					break
				}
				// A plain-mana UnlessCost$ (the echo / cumulative-upkeep
				// family) falls through to the shared mana path below, exactly
				// like a Counter's: paid spares the permanent, decline
				// sacrifices it. The unimplemented non-mana shapes never
				// reach the ask, so they never reach this arm.
			}
			// deterministically.
			// UnlessCostResolved first: an UnlessCost$ naming an SVar whose
			// count body resolves folds its numeric result into a generic
			// amount (Feather, Radiant Arbiter's SVar:CopyCost:Count$ChosenSize/
			// Times.2 -- "{2} for each of those creatures"), the same string
			// unlessProceed's ask label showed, so the offer and the charge can
			// never disagree. An SVar the ctx's table lacks or whose body does
			// not resolve passes through raw and lands in the same hard
			// decline as before.
			rawUnlessCost := effects.UnlessCostResolved(e, ctx, rp.sa)
			paid, ok := ParseUnlessCost(rawUnlessCost)
			if !ok {
				// I-5: an unless-cost the payment API cannot price is a hard
				// DECLINE. ParseCost("X") is {Generic:0, X:1}; payMana never
				// charges the unfolded X, so an empty pool "pays" it for free
				// and the counterspell stays inert. An unpriceable cost must
				// decline, never resolve at zero. This is the conservative
				// correct behaviour: a cleared counter is closer to the card
				// than a no-op. The named dynamic shapes close against the
				// resolution context: the announced-X binding (CR 601.2b,
				// including an announced zero), the resolvable SVar bodies (the
				// Counter/CopySpellAbility folds and every other API), the
				// DefinedCost_/DefinedSACost_ card-anchored mana values, the
				// energy parts (PayEnergy<N>/<X>, charged from the payer's
				// counters), the Return<N/Spec> choice parts (the payer's pick,
				// the beginUnlessPayment continuation) and LifeTotalHalfUp (the
				// payer's own life, folded at the gate and the charge).
				// ParseUnlessCost is still the strict parser: ExileFromGrave<...>,
				// Behold<...>, tapXType<...>, CopyCost, and every remaining
				// dynamic or unmodelled token declines here rather than
				// ParseCost's flat {1} substitution buying it for one generic.
				// The ask is still posed to the payer (the answer is recorded by
				// ModeChosen) but cannot succeed. Declining here (rather than
				// suppressing the ask in effects, which cannot import rules'
				// cost type) keeps the decision on the wire for hosts to observe
				// while never letting an empty pool satisfy it.
				ctx.UnlessPay = "decline"
			} else if chosePay && e.unlessCostPayable(payOption.Player, rawUnlessCost, ctx, rp.obj) {
				if len(paid.Sac) > 0 || len(paid.Discard) > 0 || len(paid.Reveal) > 0 || len(paid.RevealChosen) > 0 || len(paid.Return) > 0 {
					// Sacrifice, discard, reveal and return are choice-bearing
					// costs.
					// Park this resume before any mutation and let the payer
					// select every component; finishUnlessPayment re-enters
					// with unlessPay set, so this arm never charges it twice.
					e.beginUnlessPayment(payOption.Player, paid, ctx, rp.obj, rp)
					return
				}
				// CR 601.2g: a mana-only unless cost gives the payer the same
				// chance to activate mana abilities before the charge as a cast
				// or a Ward does, so a converted colour (stat:ManaConvert) can be
				// produced by tapping. The window only opens when the pool
				// (under the payment's conversion) cannot already pay and an
				// untapped source exists; otherwise the charge below is
				// unchanged.
				if e.unlessManaWindowNeeded(payOption.Player, paid, rp.obj) {
					e.askUnlessWardMana(payOption.Player, paid, rp)
					return
				}
				if e.payUnlessCost(payOption.Player, paid, ctx, rp.obj) {
					ctx.UnlessPay = "pay"
				} else if paid.hasManaPayment() && len(e.windowManaUnits(payOption.Player)) > 0 {
					// A failed pool-only attempt is not a decline: open the
					// CR 601.2g mana-ability window and resume this exact frame
					// after the payer has assembled enough floating mana. The
					// offer gate proved the budget reachable before Pay was
					// offered, so sources remain while the charge is unmet.
					e.beginUnlessPayment(payOption.Player, paid, ctx, rp.obj, rp)
					return
				} else {
					ctx.UnlessPay = "decline"
				}
			} else {
				ctx.UnlessPay = "decline"
			}
			// The payer whose answer this is (the unlessProceed gate moves a
			// decline on to the next UnlessPayer$ payer, and a pay ends the
			// ask), threaded through the decision's ResumeTarget via the
			// resume point — the same channel the "choice" and "dig" arms use.
			ctx.UnlessNext = rp.target
		case "sacrifice_optional":
			// Optional$ + StrictAmount$ is a disjoint choice (decline, or
			// exactly Amount) that KChoose cannot represent. Its first KModes
			// answer records only the election; effSacrifice then asks an exact
			// KChoose if several complete batches are available.
			ctx.SacOptional = "decline"
			if len(chosen) > 0 && chosen[0].Index == 0 {
				ctx.SacOptional = "sacrifice"
			}
			ctx.SacOptionalTarget = rp.target
		case "sacrifice":
			// A player-targeted Sacrifice's KChoose (CR 701.21a: the
			// sacrificing player chooses which of their permanents) was
			// answered. The chosen options carry the object in Obj (the same
			// shape the "discard" and "dig" arms read), so the id list goes
			// straight to Ctx.SacPicks in the player's answer order; SacDone
			// distinguishes "answered, possibly with nothing" (an Optional$
			// decline) from the first pass, and SacTarget keeps the answer
			// attached to the exact Defined$ target that asked. effSacrifice
			// consumes and clears all three at the top of its own walk, so a
			// nested sacrifice cannot inherit the outer answer.
			ctx.SacPicks = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.SacPicks = append(ctx.SacPicks, o.Obj)
				}
			}
			ctx.SacDone = true
			ctx.SacTarget = rp.target
		case "ward_mana":
			if e.answerWardMana(rp, chosen, ctx) {
				return
			}
		case "unless_mana":
			if e.answerWardMana(rp, chosen, ctx) {
				return
			}
		case "ward_alt":
			// The Discard<...>:<mana> Ward alternative can choose its mana
			// half even when it is not already floating; it receives the same
			// CR 702.21a activation window as an ordinary numeric Ward.
			if len(chosen) == 1 && chosen[0].Kind == "ward_mana" {
				_, manaRaw, _ := strings.Cut(rp.sa.Params["UnlessCost"], ">:")
				cost := e.parseCost(manaRaw)
				if e.payMana(chosen[0].Player, cost) {
					ctx.UnlessPay = "pay"
				} else if cost.hasManaPayment() && e.hasUntappedManaSource(chosen[0].Player) {
					e.askWardMana(rp, &wardManaPayment{payer: chosen[0].Player, cost: cost,
						resumeKind: "ward_mana", prompt: "Activate mana abilities to pay Ward"})
					return
				} else {
					ctx.UnlessPay = "decline"
				}
				break
			}
			fallthrough
		case "ward_blight", "ward_evidence", "ward_waterbend", "ward_tap", "ward_sac", "ward_discard":
			if e.settleWardPayment(rp.kind, rp.sa, ctx, chosen) {
				ctx.UnlessPay = "pay"
			} else {
				ctx.UnlessPay = "decline"
			}
		case "discard":
			// A mid-resolution discard choice ("Mode$ RevealYouChoose"
			// Thoughtseize/Duress — the CASTER picks out of the target's
			// revealed hand; or "Mode$ TgtChoose" Mind Rot / Faithless
			// Looting — the DISCARDING player picks out of their own hand)
			// was answered. The chosen options carry the object in Obj (the
			// same Obj a cleanup-step discard option carries), so the id
			// list is read straight off them — the one place a
			// mid-resolution answer moves an object by identity rather than
			// an SVar name, which is why Ctx carries a Discard []ObjID
			// rather than a Modes []string.
			// Counter's UnlessCost$ and RearrangeTopOfLibrary (Ponder) can
			// both reuse this same answer-shape and resume retrofitted onto
			// their own asking primitive — see task-dc1-brief scope.
			ids := make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ids = append(ids, o.Obj)
				}
			}
			ctx.Discard = ids
			// The per-target cursor: DiscardTarget is the index (into the
			// effect's deterministic acting-player list) of the player whose
			// hand this answer was about, so the re-entered effDiscard applies
			// it to exactly that target and poses a fresh ask for every later
			// target (the RevealPickTarget/RevealOptTarget pattern). Without
			// it, a multi-target TgtChoose answered for target 0 re-asked
			// target 1 forever — the answer was always consumed at target 0's
			// cursor (0), whose hand never held target 1's chosen cards.
			ctx.DiscardTarget = rp.target
		case "discard_hand", "discard_may":
			// A "Mode$ Hand | Optional$ True" may-discard election (the
			// whole-hand wheel's "each player may discard their hand"), or a
			// TgtChoose Optional$ True one ("you may discard a land card",
			// Mox Diamond; "discard up to two cards"), was answered: option 0
			// is yes, anything else — option 1, an empty or malformed answer
			// — is a decline, the conservative read of an ambiguous one. The re-entered effDiscard applies the answer to
			// exactly the acting player this ask was posed for (the cursor)
			// and poses a fresh election for every later player; a declined
			// election discards nothing.
			ctx.DiscardVote = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.DiscardVote = "yes"
			}
			ctx.DiscardTarget = rp.target
		case "choosetype":
			// A mid-resolution ChooseType ask (task ct1: SP$/AB$/DB$ ChooseType
			// resolving outside the cast-time "as this enters" choice —
			// Haunting Voyage's "Choose a creature type. Return ...") was
			// answered. The chosen option is the cast-time ask's own "type"
			// wire shape, so the Label IS the creature type the chooser
			// picked. The re-entered effChooseType emits the one Choose event
			// the fallback emits, with the answered type, so events.Apply
			// records o.ChosenType exactly the way every downstream reader
			// (Card.ChosenType / IsNotChosenType filters) already reads. The
			// effect consumes and clears the field (fx42 scoping), so a nested
			// ChooseType below poses its own ask.
			if len(chosen) > 0 {
				ctx.ChosenType = chosen[0].Label
			}
		case "choosecolor":
			// A mid-resolution ChooseColor ask (task
			// cli-20260923T060000Z-choose-color: SP$/AB$/DB$ ChooseColor
			// resolving outside the cast-time "as this enters" choice -- Wash
			// Out's "Return all permanents of the color of your choice") was
			// answered. The chosen option is the cast-time ask's own "color"
			// wire shape, so the Label IS the full colour name the chooser
			// picked. The re-entered effChooseColor emits the one Choose event
			// the fallback emits, with the answered colour's WUBRG letter, so
			// events.Apply records o.ChosenColor exactly the way every
			// downstream reader (Card.ChosenColor filters, devotion) already
			// reads. The effect consumes and clears the field (fx42 scoping),
			// so a nested ChooseColor below poses its own ask.
			if len(chosen) > 0 {
				ctx.ChosenColor = chosen[0].Label
			}
		case "changetext":
			// A mid-resolution api:ChangeText word ask (the Choose/
			// ChooseCreatureType/ChooseBasicLandType halves of a
			// ChangeColorWord$/ChangeTypeWord$ substitution) was answered.
			// Each answered option's Kind says which half it is
			// ("changetext_from"/"changetext_to"), so a one-pick ask and a
			// future combined ask use the same transport. The re-entered
			// effChangeText consumes and clears whichever halves it now has
			// (fx42 scoping), so a nested ChangeText asks its own words.
			for _, o := range chosen {
				switch o.Kind {
				case "changetext_from":
					ctx.ChangeTextFrom = o.Label
				case "changetext_to":
					ctx.ChangeTextTo = o.Label
				}
			}
		case "choosenumber":
			// A mid-resolution ChooseNumber ask (task
			// cli-20260923T060000Z-choose-number: SP$/AB$/DB$ ChooseNumber
			// resolving outside the cast-time "as this enters" choice -- Void's
			// "Choose a number. Destroy all artifacts and creatures with mana
			// value equal to that number") was answered. The chosen option is
			// the number list's own "number" wire shape, so the option's Amount
			// IS the number the chooser picked. The re-entered effChooseNumber
			// emits the one Choose event the fallback emits, with the answered
			// number, so events.Apply records o.ChosenNumber exactly the way
			// every downstream reader (Card.ChosenNumber filters, Void's
			// Count$ChosenNumber X) already reads. ZERO is a legal answer, so the
			// answered marker is a separate bool. The effect consumes and clears
			// both (fx42 scoping), so a nested ChooseNumber below poses its own
			// ask.
			ctx.ChosenNumberAnswered = true
			if len(chosen) > 0 {
				ctx.ChosenNumberPick = int32(chosen[0].Amount)
			} else {
				// A malformed empty answer (the ask is Min 1/Max 1): keep the
				// deterministic fallback 0 rather than inventing a number the
				// option list never offered.
				ctx.ChosenNumberPick = 0
			}
		case "manareflected":
			// A standalone AB$ ManaReflected colour ask (the mid-resolution
			// choice effManaReflected poses when a DB$/SP$ body reflecting
			// several colours resolves outside the mana-activation path) was
			// answered. The chosen option's structured ManaSymbol carries the
			// picked colour; the re-entered effManaReflected consumes and
			// clears it and emits
			// the answered ManaAdd, so a nested ManaReflected poses its own ask.
			// An empty answer (malformed -- the ask is Min 1/Max 1 over a set of
			// two or more) leaves the field empty, and the effect's re-entry
			// degrades to its deterministic first candidate.
			if len(chosen) > 0 {
				// The structured mana symbol travels with the chosen option;
				// the option label is presentation-only.
				ctx.ManaReflectedColor = chosen[0].ManaSymbol
			}
		case "taporuntap":
			// A TapOrUntap's tap-vs-untap election (api:TapOrUntap, Merrow
			// Reejerey / Twiddle) was answered. Each offered option carries the
			// target it elected for in Obj and its choice in Kind ("tap" or
			// "untap"), so the answer is read straight off option 0. An empty or
			// malformed answer still sets the Done marker (the effect's Min 1/
			// Max 1 ask always has a legal single-option answer, so an empty one
			// is malformed, never a decline) and degrades to "tap" with no target
			// named — the conservative read, which the re-entered effect applies
			// to its first pending target. The effect consumes and clears all
			// three fields at the point of application (fx42 scoping), so a
			// later target poses its own ask.
			ctx.TapOrUntapDone = true
			if len(chosen) > 0 {
				ctx.TapOrUntapObj = chosen[0].Obj
				ctx.TapOrUntap = chosen[0].Kind
			}
		case "explore":
			// An Explore's LCI destination election (api:Explore, CR 701.35a:
			// "put the card back or put it into your graveyard") was answered.
			// The resume point carries the pending explorer (decision.ResumeTarget
			// = the explorer's id) and the answered option carries the revealed
			// card in Obj and the choice in Kind ("graveyard"/"top"), so the
			// re-entered effExplore applies the counter and the destination move
			// together, in CR order, then emits the record. An empty answer
			// (malformed — the ask's two options are always legal, Min 1/Max 1)
			// still sets the Done marker with no card: the effect's application
			// path guards the card's absence, so the record never names a stale
			// id. The effect consumes and clears all four fields at the point of
			// application (fx42 scoping), so the pending explorer's remaining
			// explores and every later target pose their own fresh path.
			ctx.ExploreDone = true
			ctx.ExploreObj = state.ObjID(rp.target)
			ctx.ExploreCount = rp.exploreDone
			if len(chosen) > 0 {
				ctx.ExploreChoice = chosen[0].Kind
				ctx.ExploreCard = chosen[0].Obj
			}
		case "connive":
			// A Connive's discard election (api:Connive, CR 702.59: draw N,
			// then discard N — the ask fires only when the hand holds more
			// than N cards, the strict-supersets discipline) was answered.
			// The resume point carries the pending conniver
			// (decision.ResumeTarget = the conniver's id) and every chosen
			// option carries the card to discard in Obj (the same shape the
			// "discard" arm reads), so the re-entered effConnive applies the
			// discards, the per-nonland +1/+1 counters and the record, then
			// every later conniving target poses its own fresh ask. An empty
			// answer (malformed — the ask's Min is N >= 1 over a hand larger
			// than N) still sets the Done marker with no picks: the effect's
			// application path guards the absence, so the record names only
			// what actually moved. The effect consumes and clears all three
			// fields at the point of application (fx42 scoping).
			ctx.ConniveDone = true
			ctx.ConniveObj = state.ObjID(rp.target)
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.ConniveDiscard = append(ctx.ConniveDiscard, o.Obj)
				}
			}
		case "copypermanent_choice":
			// CopyPermanent's sole Choices$/Chooser$ shape has its own
			// transport so a nested ordinary Choice cannot consume the answer.
			ctx.CopyPermanentChoiceDone = true
			if len(chosen) > 0 {
				ctx.CopyPermanentChoice = chosen[0].Obj
			}
		case "clone_choice":
			// A DB$ Clone Choices$ <filter> copy-source pick was answered:
			// the chooser named one object for the copy source. The effect
			// consumes and clears the field at the top of its walk (fx42
			// scoping), so a nested Clone cannot inherit the outer answer. A
			// malformed or empty answer leaves a zero id, which the re-entered
			// effect records as one loud Note and resolves as no copy -- never
			// a silent fall-through to an object the chooser did not name.
			ctx.ClonePickDone = true
			if len(chosen) > 0 {
				ctx.ClonePick = chosen[0].Obj
			}
		case "changezone_alternative":
			// The card owner chose the primary or alternative library position.
			// Carry the offered label through the re-entered ChangeZone body.
			if len(chosen) > 0 {
				ctx.ChangeZoneAlternative = chosen[0].Label
			}
		case "choosedirection":
			// A mid-resolution ChooseDirection ask (Aminatou's [-6], Order of
			// Succession) was answered. The chosen option's Label is the
			// direction word ("left"/"right"), so it is carried verbatim:
			// the re-entered effChooseDirection consumes it and the shared
			// Ctx then lets the chain's SubAbility$ (GainControl's
			// NextPlayerInChosenDirection / ChooseNextPlayerInChosenDirection)
			// read the same direction for the rest of the walk.
			if len(chosen) > 0 {
				ctx.ChosenDirection = chosen[0].Label
			}
		case "choice":
			// ChooseCard, ChoosePlayer and ChangeTargets all use KChoose. Keep
			// the concrete target shape rather than just an ObjID because player
			// zero is a real target too.
			ctx.Choice = make([]state.Target, 0, len(chosen))
			for _, o := range chosen {
				if o.Kind == "player" {
					ctx.Choice = append(ctx.Choice, state.Target{Player: o.Player, IsPlayer: true})
				} else if o.Obj != 0 {
					ctx.Choice = append(ctx.Choice, state.Target{Obj: o.Obj})
				}
			}
			ctx.ChoiceDone = true
		case "vote":
			// api:Vote's PLAYER ballot (task votepb1): the answer to one
			// voter's "vote for a player" KChoose. The accumulated picks ride
			// the decision's ResumeChoices (rp.choices) and the answered voter
			// index its ResumeTarget (rp.target); effPlayerVote consumes both,
			// appends this answer, and asks the next voter -- or completes and
			// publishes Ctx.VoteCounts for the chained AmountFromVotes$
			// reader. The transport is decision-scoped (never Ctx.Chosen), so
			// a nested vote cannot inherit an outer ballot's picks.
			ctx.VotePicks = append([]state.Target(nil), rp.choices...)
			ctx.VoteTarget = rp.target
			ctx.VoteDone = true
			for _, o := range chosen {
				if o.Kind == "player" {
					ctx.VoteAnswer = append(ctx.VoteAnswer, state.Target{Player: o.Player, IsPlayer: true})
				} else if o.Obj != 0 {
					ctx.VoteAnswer = append(ctx.VoteAnswer, state.Target{Obj: o.Obj})
				}
			}
		case "demonstrate":
			// The demonstrate trigger's answered ask (CR 702.152): which ask
			// rides the decision's ResumeTarget (rp.target -- 0 the may-copy
			// election, 1 the opponent choice); the election's yes/no answer
			// and the opponent pick are the chosen options. effDemonstrate
			// consumes and clears all four fields at the top of its walk (the
			// fx42 scoping discipline), so a nested Demonstrate below this
			// one poses its own asks.
			ctx.DemonstrateDone = true
			ctx.DemonstrateStage = rp.target
			for _, o := range chosen {
				switch o.Kind {
				case "yes":
					ctx.DemonstrateYes = true
				case "player":
					ctx.DemonstrateOpp = append(ctx.DemonstrateOpp, state.Target{Player: o.Player, IsPlayer: true})
				}
			}
		case "cipher":
			// The api:Cipher encode ask (CR 702.99a) was answered: an empty
			// answer declines, a picked creature is the encode host.
			// effCipher consumes and clears both fields at the top of its walk
			// (the fx42 scoping discipline), so a nested Cipher poses its own
			// ask.
			ctx.CipherDone = true
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.CipherPick = append(ctx.CipherPick, state.Target{Obj: o.Obj})
				}
			}
		case "opp_pick":
			// The TargetingPlayer$ Opponent controller-selection ask
			// (agent-20260925T085158Z-c861188d): the chosen "player" option
			// names the opponent who answers the re-entered ask. Recorded
			// under the SA's line; the walk's chooser read (midChooserCore,
			// reached through Engine.ChooserFor) consumes it. The arm sets
			// no Ctx fields: the re-entry re-runs the ask construction and
			// the pin redirects it to the chosen seat.
			if len(chosen) > 0 && chosen[0].Kind == "player" && rp.sa != nil {
				if e.oppPicksMid == nil {
					e.oppPicksMid = make(map[string]state.PlayerID)
				}
				e.oppPicksMid[rp.sa.Line] = chosen[0].Player
			}
		case "tgts":
			// The generic ValidTgts$ pre-ask (task mvts1) posed inside
			// effects.Resolve's dispatch loop. Same KChoose answer shape as
			// "choice", on its own resume kind and its own Ctx transport
			// (Ctx.TargetsPick) so another KChoose primitive resolving under
			// the same SA can never consume this answer. A MoveCounter SA
			// additionally RECORDS the set: its own kind/amount asks will
			// suspend it again later, and the re-entry after THOSE must
			// re-seed this answer or the target ask re-fires forever (the
			// movecounter1 livelock; seedMoveCounterAsk).
			ctx.TargetsPick = make([]state.Target, 0, len(chosen))
			for _, o := range chosen {
				if o.Kind == "player" {
					ctx.TargetsPick = append(ctx.TargetsPick, state.Target{Player: o.Player, IsPlayer: true})
				} else if o.Obj != 0 {
					ctx.TargetsPick = append(ctx.TargetsPick, state.Target{Obj: o.Obj})
				}
			}
			ctx.TargetsPickDone = true
			if rp.sa != nil && rp.sa.API == "MoveCounter" {
				e.moveCounterEntry(rp.obj).targets = append([]state.Target(nil), ctx.TargetsPick...)
			}
			// Every OTHER API records through the general cursor: the body
			// this answer is about to run may itself suspend (Kozilek's
			// Command's Scry poses its KArrange), and the resume after THAT
			// rebuilds the Ctx from scratch. Without the record the pre-ask
			// fires again and the two asks alternate forever.
			e.recordTargetsPick(rp.obj, rp.sa, ctx.TargetsPick)
		case "damage_split":
			// DealDamage's DividedAsYouChoose$ allocation answer: the KChoose
			// offered one option per chosen target (Min == Max == the named
			// total, Repeatable), so the answer is a multiset whose per-option
			// multiplicity is the damage that option's target receives. Every
			// option index is the target's position in the resolution's
			// Defined$ order, so the re-entered effDealDamage reads the shares
			// positionally. A target the answer never picked is simply absent
			// (zero damage), which the primitive's positional read treats as
			// nothing.
			n := 0
			for _, o := range chosen {
				if o.Index+1 > n {
					n = o.Index + 1
				}
			}
			ctx.DamageSplit = make([]int32, n)
			for _, o := range chosen {
				if o.Index >= 0 && o.Index < len(ctx.DamageSplit) {
					ctx.DamageSplit[o.Index]++
				}
			}
			ctx.DamageSplitDone = true
		case "search":
			// A hidden-library KChoose answer is an ordered subset. Preserve
			// that order for ChangeZone's MoveZone sequence, and set a separate
			// marker so choosing no cards still means "answered; do not re-ask".
			ctx.Search = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.Search = append(ctx.Search, o.Obj)
				}
			}
			ctx.SearchDone = true
			ctx.LibraryTarget = rp.target
		case "search_confirm":
			// An Optional$ confirmation on a hidden-library ChangeZone
			// search was answered (Forge's confirmAction gate, which runs
			// before the fetch list is consulted): option zero accepts this
			// search player's fetch, every other answer declines it.
			// effSearchLibrary consumes and clears these at the top of its
			// walk (fx42 scoping), so a nested search poses its own
			// confirmation. ResumeTarget is the per-library cursor, the same
			// one LibraryTarget carries for the answered pick.
			ctx.SearchConfirmDone = true
			ctx.SearchConfirmTarget = rp.target
			ctx.SearchConfirm = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.SearchConfirm = "yes"
			}
		case "search_mayshuffle":
			// A ChangeZone search carrying ShuffleNonMandatory$ True (Path to
			// Exile, Stoneforge Mystic, Boggart Harbinger) asked its searcher
			// "Shuffle your library?" after the search's moves landed. The
			// answer is a bare yes/no, recorded here as a marker the
			// re-entered effect consumes and clears (fx42 scoping): "yes"
			// emits the same Secret events.Shuffle every library shuffle
			// emits, "no" -- the information-mercy Forge's flag names -- keeps
			// the library order. The moved list rides the ask
			// (Decision.ResumeMoved -> rp.moved) so the re-entry can finish
			// with the LibraryPosition$ placement after the answered shuffle.
			// A malformed or empty answer keeps the order, the conservative
			// read of an ambiguous one.
			ctx.SearchShuffle = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.SearchShuffle = "yes"
			}
			ctx.SearchShuffleMoved = append([]state.ObjID(nil), rp.moved...)
			ctx.LibraryTarget = rp.target
		case "attach_optional":
			// An Optional$ True Attach's yes/no election (Ajani's Chosen's
			// "you may attach it to the token") was answered. The answer is a
			// bare yes/no, recorded here as a marker the re-entered effect
			// consumes and clears (fx42 scoping): "yes" attaches the resolved
			// object to the first legal Defined$ target, "no" -- the decline --
			// emits no Attach and the chained SubAbility$ still runs. A
			// malformed or empty answer keeps the decline, the conservative
			// read of an ambiguous one.
			ctx.AttachOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.AttachOpt = "yes"
			}
		case "investigate_optional":
			// An Optional$ True Investigate's per-player may-investigate
			// election (Will the Wise's "each opponent may investigate",
			// Nick Valentine's "you may investigate") was answered for the
			// player the ask named: Decision.ResumeTarget is the cursor index
			// into the actingPlayers walk the re-entered effInvestigate
			// re-derives (the same-Remembered ride keeps a Remembered-valued
			// Defined$ deriving the SAME list the cursor indexes). The answer
			// is a bare yes/no, recorded here as the marker the re-entered
			// effect consumes and clears at the point of application (fx42
			// scoping): "yes" mints the Num$ Clues and, with
			// RememberInvestigatingPlayers$ True, remembers the acceptor;
			// "no" — the decline — does neither, and the chained SubAbility$
			// still runs either way. A malformed or empty answer keeps the
			// decline, the conservative read draw_optional and attach_optional
			// take.
			ctx.InvestigateOptIdx = int32(rp.target)
			ctx.InvestigateOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.InvestigateOpt = "yes"
			}
		case "copy_optional":
			// An Optional$ True CopySpellAbility's may-copy election
			// (Sevinne's Reclamation's "you may copy this spell", and the
			// corpus's wider may-copy family) was answered. Option 0 is "yes";
			// anything else (option 1, an empty or malformed answer) is the
			// decline -- the conservative read of an ambiguous answer. The
			// re-entered effCopySpellAbility consumes and clears Ctx.CopyOpt:
			// "yes" makes the copy through the ordinary path, "no" makes
			// none and the chained SubAbility$ still runs.
			ctx.CopyOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.CopyOpt = "yes"
			}
		case "surveil_look_optional":
			// The stat:SurveilNum optional "you may look at an additional N
			// cards each time you surveil" election (Enhanced Surveillance)
			// was answered. Each Optional$ static is an independent may effect,
			// so the ask offered one option per optional static and the answer
			// is the ACCEPTED subset: the accepted ordinals ride
			// Ctx.SurveilLookOpt as a CSV done-marker the re-entered effSurveil
			// consumes and clears (fx42 scoping), each accepted ordinal adding
			// that static's Num$ to THE ASKING PLAYER's surveil count. An empty
			// answer is the real decline of every static ("no", the Min-0
			// Optional answer); a malformed one keeps the decline, the
			// conservative read attach_optional takes.
			ctx.SurveilLookOpt = "no"
			if len(chosen) > 0 {
				parts := make([]string, 0, len(chosen))
				for _, o := range chosen {
					parts = append(parts, strconv.Itoa(o.Index))
				}
				ctx.SurveilLookOpt = strings.Join(parts, ",")
			}
		case "attach_choice":
			// A Choices$ Attach's card choice was answered (Goldwardens'
			// Gambit's "for each of those tokens, you may attach an Equipment
			// you control to it", unexpected_request's "you may attach an
			// Equipment you control", Breath of Fury's "attach CARDNAME to a
			// creature you control"). The chosen card ids are recorded for the
			// re-entered effect to consume and clear (fx42 scoping): with no
			// Object$ the ids name the OBJECT to attach, with Object$ present
			// they name the DESTINATION. An empty answer on the Min-0 Optional
			// shape is a real decline, so AttachChoiceDone distinguishes it
			// from an unanswered ask (the ctx.Search/SearchDone discipline).
			// The asking pass's resolved destination list rides back in
			// rp.choices (Decision.ResumeChoices) -- a RepeatEach body's
			// Defined$ Imprinted binding does not survive the suspension, so
			// the re-entry must not re-derive it.
			ctx.AttachChoice = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.AttachChoice = append(ctx.AttachChoice, o.Obj)
				}
			}
			ctx.AttachChoiceDone = true
			ctx.AttachDests = make([]state.ObjID, 0, len(rp.choices))
			for _, t := range rp.choices {
				if !t.IsPlayer && t.Obj != 0 {
					ctx.AttachDests = append(ctx.AttachDests, t.Obj)
				}
			}
		case "attach_player_choice":
			// A PlayerChoices$ Attach's player choice was answered (Curse of
			// Leeches' `DB$ Attach | Object$ Self | PlayerChoices$ Player`,
			// Lynde's `DB$ Attach | Object$ ChosenCard | PlayerChoices$
			// Opponent`). The chosen seat is recorded for the re-entered
			// effect to consume and clear (fx42 scoping); the re-entry
			// re-checks it against the live pool the asking pass derived, so a
			// stale answer attaches nothing. AttachPlayerDone distinguishes
			// "answered" from an unanswered ask, so a Min-1 mandatory ask that
			// somehow produced an empty answer is a real no-attach rather than
			// a re-ask livelock.
			ctx.AttachPlayerDone = true
			for _, o := range chosen {
				if o.Kind == "player" {
					ctx.AttachPlayer = o.Player
					break
				}
			}
		case "planeswalk_optional":
			// An Optional$ True Planeswalk election is a KChoose yes/no. The
			// effect is a no-op without a planar deck, but its election is still
			// recorded by the effect and the normal Resolve walk continues into
			// any SubAbility$.
			ctx.PlaneswalkOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.PlaneswalkOpt = "yes"
			}
		case "scry_optional":
			ctx.ScryOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.ScryOpt = "yes"
			}
			ctx.Arrange = true
			ctx.LibraryTarget = rp.target - 1
		case "put_optional":
			// An Optional$ True PutCounter's yes/no election (Talus Paladin's
			// "you may put a +1/+1 counter on CARDNAME", Black Widow's "You
			// may put ... If you don't, ...") was answered. The answer is a
			// bare yes/no, recorded here as a marker the re-entered effect
			// consumes and clears (fx42 scoping): "yes" places the counters
			// through the ordinary path, "no" -- the decline -- places nothing
			// and the chained SubAbility$ still runs (the attach_optional
			// convention). A malformed or empty answer keeps the decline, the
			// conservative read of an ambiguous one.
			ctx.PutOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.PutOpt = "yes"
			}
		case "setstate_optional":
			// An Optional$ True SetState's yes/no election (Dowsing Dagger's
			// "you may transform this Equipment", High Marshal Arguel's "you
			// may transform it") was answered. The answer is a bare yes/no,
			// recorded here as a marker the re-entered effect consumes and
			// clears (fx42 scoping): "yes" runs the ordinary face change,
			// "no" -- the decline -- changes nothing and the chained
			// SubAbility$ still runs (the put_optional convention). A
			// malformed or empty answer keeps the decline, the conservative
			// read of an ambiguous one.
			ctx.SetStateOpt = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.SetStateOpt = "yes"
			}
		case "imprint":
			// An Imprint$ True public-zone choice. The effect consumes this
			// answer on re-entry and emits the persistent Imprint event.
			ctx.Imprint = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.Imprint = append(ctx.Imprint, o.Obj)
				}
			}
			ctx.ImprintDone = true
		case "untap":
			ctx.Untap = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.Untap = append(ctx.Untap, o.Obj)
				}
			}
			ctx.UntapDone = true
		case "dig":
			// A Dig look-and-take pick was answered: the library owner chose
			// which of the window's ChangeValid$-eligible cards to move to
			// DestinationZone$. The chosen options carry the object in Obj
			// (the same shape the "search" and "discard" arms read), so the
			// id list is read straight off them, in the player's answer order.
			// DigDone distinguishes "answered, possibly with no cards" (an
			// Optional$ decline) from the first pass. DigTarget keeps that
			// answer attached to the exact Defined$ target that asked, even
			// when earlier targets completed before suspension. effDig consumes
			// and clears all three at the top of its own walk, so a nested Dig
			// cannot inherit the outer answer.
			ctx.Dig = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.Dig = append(ctx.Dig, o.Obj)
				}
			}
			ctx.DigDone = true
			ctx.DigTarget = rp.target
		case "clash_placement":
			ctx.ClashContinuation = cloneClashResume(rp.clash)
			ctx.ClashTop = len(chosen) > 0 && chosen[0].Kind == "top"
		case "twopiles_split":
			// A TwoPiles pile split was answered (task twopiles1, Fact or
			// Fiction): the separator picked pile A out of the card set, in
			// answer order — the options carry the object in Obj (the same
			// shape the "dig" arm reads). An empty answer is the legal "piles
			// can be empty" answer, so TwoPilesDone is the answered marker,
			// not len(chosen). The full card set rides the decision's
			// ResumeRemembered (the ctx rebuild picks it up below, so the
			// re-entered effect re-derives pile B); pile A re-rides the pick
			// ask's ResumeChoices. effTwoPiles consumes and clears both fields
			// at the top of its own walk (fx42 scoping).
			ctx.TwoPiles = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.TwoPiles = append(ctx.TwoPiles, o.Obj)
				}
			}
			ctx.TwoPilesDone = true
		case "twopiles_pick":
			// A TwoPiles pile pick was answered: the chooser picked which pile
			// is the chosen one — option 0 Kind "pile-a" (pile A, the split
			// ask's answer, rides ResumeChoices back as Ctx.TwoPiles), option 1
			// Kind "pile-b". The ChosenPile$ body then runs on the chosen
			// pile and UnchosenPile$ on the other. A malformed or empty answer
			// keeps pile A (the deterministic clamp answer), the same
			// conservative read the malformed yes/no answers take.
			ctx.TwoPilesPick = "a"
			if len(chosen) > 0 && chosen[0].Kind == "pile-b" {
				ctx.TwoPilesPick = "b"
			}
			ctx.TwoPilesPickDone = true
			for _, t := range rp.choices {
				if !t.IsPlayer && t.Obj != 0 {
					ctx.TwoPiles = append(ctx.TwoPiles, t.Obj)
				}
			}
		case "clone":
			// A DB$ Clone Optional$ True may-copy election was answered
			// (ticket api-clone-trigger-copy; Sarkhan Soul Aflame). The answer
			// is a bare yes/no recorded as a marker the re-entered effect
			// consumes and clears (fx42 scoping): "yes" performs the copy,
			// "no" -- the decline -- leaves the permanent alone. A malformed
			// or empty answer keeps the decline, the same conservative read
			// the diguntil_move and attach_optional answers take.
			ctx.Clone = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.Clone = "yes"
			}
			ctx.CloneDone = true
		case "diguntil_move":
			// A DigUntil reveal-until's OptionalFoundMove$ yes/no election was
			// answered (task diguntil1; Songbirds' Blessing). The answer is a
			// bare yes/no recorded as a marker the re-entered effect consumes
			// and clears (fx42 scoping): "yes" moves the found card(s) to
			// FoundDestination$, "no" — the decline — to OptionalNoDestination$
			// or the revealed pile. A malformed or empty answer keeps the
			// decline, the conservative read of an ambiguous one (the same
			// attach_optional convention). DigUntilMoveDone also suppresses the
			// re-entry's reveal Note, which the first pass already recorded.
			ctx.DigUntilMove = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.DigUntilMove = "yes"
			}
			ctx.DigUntilMoveDone = true
		case "diguntil_aura":
			// CR 303.4f: an Aura entering without being cast chooses a
			// permanent to enchant. The option's object is revalidated by
			// effDigUntil against the current eligible bearer list.
			if len(chosen) > 0 {
				ctx.DigUntilAuraBearer = chosen[0].Obj
			}
			ctx.DigUntilAuraDone = true
		case "counter_dist":
			// A DividedAsYouChoose$ PutCounter distribution pick was answered
			// (Vastwood Hydra): the chooser picked which of the Choices$
			// eligible battlefield creatures receive the CounterNum$ total, in
			// answer order. CounterDistDone distinguishes "answered, possibly
			// with no creatures" (a MinChoiceAmount$ 0 decline) from the first
			// pass. effPutCounter consumes and clears both at the top of its
			// own walk (the fx42 scoping discipline), so a nested PutCounter
			// cannot inherit the outer answer.
			ctx.CounterDist = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.CounterDist = append(ctx.CounterDist, o.Obj)
				}
			}
			ctx.CounterDistDone = true
		case "counter_kind":
			ctx.CounterKind = ""
			if len(chosen) > 0 {
				ctx.CounterKind = chosen[0].Label
			}
			ctx.CounterKindDone = true
			// A bare Choices$ recipient pick precedes its comma-list kind
			// question. Carry that completed pick only through THIS re-entry;
			// effPutCounter consumes it at entry before a nested SA can see it.
			if len(rp.choices) > 0 {
				for _, t := range rp.choices {
					if !t.IsPlayer && t.Obj != 0 {
						ctx.CounterPick = append(ctx.CounterPick, t.Obj)
					}
				}
				ctx.CounterPickDone = true
			}
			if rp.sa != nil && strings.EqualFold(strings.TrimSpace(rp.sa.Params["CounterTypePerDefined"]), "True") {
				if e.counterTypeAsk == nil {
					e.counterTypeAsk = make(map[state.ObjID]*counterTypePending)
				}
				p := e.counterTypeAsk[rp.obj]
				if p == nil || p.sa != rp.sa {
					p = &counterTypePending{sa: rp.sa}
					e.counterTypeAsk[rp.obj] = p
				}
				for len(p.answers) <= rp.target {
					p.answers = append(p.answers, "")
				}
				p.answers[rp.target] = ctx.CounterKind
				ctx.CounterKindAnswerIndex, ctx.CounterKindAnswerSet = rp.target, true
			}
		case "counter_kinds":
			ctx.CounterKinds = nil
			for _, o := range chosen {
				ctx.CounterKinds = append(ctx.CounterKinds, o.Label)
			}
			ctx.CounterKindsDone = true
		case "counter_pick":
			// A bare-Choices$ PutCounter pick was answered (task vow1;
			// Promise of Loyalty's vow): the chooser picked the creature(s)
			// that take the full CounterNum$, in answer order.
			// CounterPickDone distinguishes "answered" from the first pass.
			// effPutCounter consumes and clears both at the top of its own
			// walk (the fx42 scoping discipline), so a nested PutCounter
			// cannot inherit the outer answer.
			ctx.CounterPick = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.CounterPick = append(ctx.CounterPick, o.Obj)
				}
			}
			ctx.CounterPickDone = true
		case "proliferate":
			// A Proliferate any-number recipient pick was answered (CR 701.27):
			// the resolving controller chose which permanents and/or players
			// take another counter of each kind already there. Unlike the
			// "counter_pick" arm, the option list is MIXED -- an object
			// recipient carries Obj, a player recipient carries Player with
			// Obj 0 -- so both halves are decoded here into the state.Target
			// shape Ctx.Proliferate carries. ProliferateDone distinguishes
			// "answered, possibly with nothing" (a Min-0 decline) from the
			// first pass, so a decline is not re-asked. effProliferate consumes
			// and clears both at the top of its own walk (the fx42 scoping
			// discipline), so a nested Proliferate cannot inherit the outer
			// answer.
			ctx.Proliferate = make([]state.Target, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.Proliferate = append(ctx.Proliferate, state.Target{Obj: o.Obj})
					continue
				}
				ctx.Proliferate = append(ctx.Proliferate,
					state.Target{Player: o.Player, IsPlayer: true})
			}
			ctx.ProliferateDone = true
		case "move_counter_kind":
			// A MoveCounter CounterType$ Any kind pick was answered: the
			// chooser picked which counter kind to move, out of the distinct
			// kinds the origin holds. The option's Label (the kind string) is
			// the answer; an empty or malformed answer keeps the deterministic
			// first-kind stand-in, the conservative read of an ambiguous one.
			// effMoveCounter consumes and clears both fields at the top of its
			// own walk (fx42 scoping), so a nested MoveCounter cannot inherit
			// the answer. The answer is ALSO recorded on the pending state:
			// this round's re-entry may suspend again on the amount ask, and
			// that later re-entry must still see the kind (seedMoveCounterAsk).
			ctx.MoveCounterKind = ""
			if len(chosen) > 0 {
				ctx.MoveCounterKind = chosen[0].Label
			}
			ctx.MoveCounterKindDone = true
			if rp.sa != nil && rp.sa.API == "MoveCounter" {
				p := e.moveCounterEntry(rp.obj)
				p.kind, p.kindSet = ctx.MoveCounterKind, true
			}
		case "time_travel":
			// Time Travel asks one optional add/remove/skip election per
			// affected object. ResumeTarget is the object's index into the
			// repetition's snapshot and ResumeRound the repetition itself,
			// so the re-entered effect continues at the exact object.
			ctx.TimeTravelChoice = "time_travel_skip"
			if len(chosen) > 0 {
				ctx.TimeTravelChoice = chosen[0].Kind
			}
			ctx.TimeTravelDone = true
			ctx.TimeTravelRound = rp.timeTravelRound
			ctx.TimeTravelIndex = rp.target
			ctx.TimeTravelObjects = append([]state.ObjID(nil), rp.timeTravelObjects...)
		case "move_counter":
			// A MoveCounter CounterNum$ Any amount pick was answered: how many
			// counters of the chosen kind to move. The option's Amount carries
			// the number (0 is a legitimate decline); a malformed answer moves
			// nothing. effMoveCounter consumes and clears both fields at the top
			// of its own walk (fx42 scoping). Recorded on the pending state for
			// the same later-resume reason as the kind above.
			ctx.MoveCounterN = 0
			if len(chosen) > 0 {
				ctx.MoveCounterN = int32(chosen[0].Amount)
			}
			ctx.MoveCounterNDone = true
			if rp.sa != nil && rp.sa.API == "MoveCounter" {
				p := e.moveCounterEntry(rp.obj)
				p.n, p.nSet = ctx.MoveCounterN, true
			}
		case "aor_elect":
			// An AddOrRemoveCounter add/remove election was answered
			// (counterchoice1). The election's kind is parsed out of the
			// answer's own option encoding ("aor_remove:<kind>"/
			// "aor_put:<kind>"), so the fresh Ctx carries everything the
			// re-entered walk needs without a second transport. A malformed
			// answer elects the deterministic first option (remove), the same
			// conservative read every KChoose arm takes. The answered kind is
			// ALSO recorded on the pending state (the moveCounterAsk
			// discipline): this round's re-entry may suspend again on the next
			// kind's election, and that later re-entry must not re-ask an
			// already-answered PUT kind (its counter count is still positive,
			// so the kinds enumeration still lists it).
			ctx.AorDone = true
			ctx.AorElect, ctx.AorKind = "remove", ""
			if len(chosen) > 0 {
				if chosen[0].Kind == "aor_skip" {
					// The combined absent-kind election has one skip option,
					// unlike the per-kind form's aor_skip:<kind>.
					ctx.AorElect = "skip"
				} else {
					if strings.HasPrefix(chosen[0].Kind, "aor_put:") {
						ctx.AorElect = "put"
					} else if strings.HasPrefix(chosen[0].Kind, "aor_skip:") {
						ctx.AorElect = "skip"
					}
					if _, k, found := strings.Cut(chosen[0].Kind, ":"); found {
						ctx.AorKind = k
					}
				}
			}
			if rp.sa != nil && rp.sa.API == "AddOrRemoveCounter" && ctx.AorKind != "" {
				e.aorEntry(rp.obj)[ctx.AorKind] = true
			}
		case "manifest_dread":
			if len(chosen) > 0 {
				ctx.ManifestDreadPick = chosen[0].Obj
			}
			ctx.ManifestDreadPlayer = rp.player
			ctx.ManifestDreadDone = true
		case "ring_bearer":
			// A Ring tempts you Ring-bearer choice (CR 701.54a: the tempted
			// player chooses a creature they control) was answered. The chosen
			// option carries the object in Obj (the same shape the "sacrifice"
			// and "blight" arms read). RingBearerDone distinguishes "answered"
			// from the first pass, so the re-entered effRingTemptsYou skips the
			// ask and emits the single RingTemptsYou event once -- a suspension
			// can never increment the count twice. effRingTemptsYou consumes and
			// clears both at the top of its own walk, so a nested Ring tempts
			// cannot inherit the outer answer.
			if len(chosen) > 0 {
				ctx.RingBearerPick = chosen[0].Obj
			}
			ctx.RingBearerDone = true
		case "blight":
			// A Blight's per-player KChoose (CR 701.60: the blighting player
			// chooses which of their own creatures takes the −1/−1 counters)
			// was answered. The chosen options carry the object in Obj (the
			// same shape the "sacrifice" and "counter_pick" arms read), so the
			// id list goes straight to Ctx.BlightPicks in the player's answer
			// order; BlightDone distinguishes "answered" from the first pass
			// and BlightTarget keeps the answer attached to the exact Defined$
			// target that asked. effBlight consumes and clears all three at the
			// top of its own walk, so a nested blight cannot inherit the outer
			// answer.
			ctx.BlightPicks = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.BlightPicks = append(ctx.BlightPicks, o.Obj)
				}
			}
			ctx.BlightDone = true
			ctx.BlightTarget = rp.target
		case "roll":
			// A RollDice choose-one-result answer (effects/dice.go's
			// ChosenSVar$/OtherSVar$ shape, the Endeavor cycle): the chosen
			// options' Index values name the dice (into the ask's own per-die
			// results, rp.rolls) the player picked. The re-entered effRollDice
			// publishes ChosenSVar$ = the sum of the picked dice's results,
			// OtherSVar$ = the sum of the rest, and consumes and clears all
			// three Ctx fields at its top (the fx42 scoping discipline).
			ctx.RollResults = rp.rolls
			pick := make([]int, 0, len(chosen))
			for _, o := range chosen {
				pick = append(pick, o.Index)
			}
			ctx.RollPick = pick
			ctx.RollDone = true
		case "hand_move":
			// A "choose N cards matching ChangeType$ from Origin$ Hand" pick was
			// answered (handmove1): the hand's owner chose which of the
			// ChangeType$-eligible cards to move to Destination$. The chosen
			// options carry the object in Obj (the same shape the "search",
			// "discard" and "dig" arms read), so the id list is read straight
			// off them, in the player's answer order. HandMoveDone distinguishes
			// "answered, possibly with no cards" from the first pass.
			// effChangeZoneHand consumes and clears both at the top of its own
			// walk (the fx42 scoping discipline), so a nested hand move cannot
			// inherit the outer answer.
			ctx.HandMove = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.HandMove = append(ctx.HandMove, o.Obj)
				}
			}
			ctx.HandMoveDone = true
			// The owner-selected shape (rv2b r2) chains one ask per hand owner:
			// the answer belongs to the exact owner that asked, and the
			// re-entered walk skips owners before the cursor and continues with
			// the owners after it (the same continuation DigTarget carries).
			ctx.HandMoveTarget = rp.target
		case "hand_move_confirm":
			// An Optional$ confirmation on a hidden-hand ChangeZone was
			// answered (Forge's confirmAction gate, which runs before the card
			// pick): option zero accepts the fetch, every other answer declines
			// it. effChangeZone...handMoveOwnersWalk consumes and clears these
			// at the top of its walk (fx42 scoping), so a nested hand move
			// poses its own confirmation.
			ctx.HandMoveConfirmDone = true
			ctx.HandMoveConfirmTarget = rp.target
			ctx.HandMoveConfirm = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.HandMoveConfirm = "yes"
			}
		case "hidden_pick":
			// A Hidden$ True public-origin pick was answered (hiddenpick1): the
			// chooser picked which of the ChangeType$-eligible cards in the
			// origin zone(s) move to Destination$. The chosen options carry the
			// object in Obj (the same shape the "search", "dig" and
			// "hand_move" arms read), so the id list is read straight off them,
			// in the player's answer order. HiddenPickDone distinguishes
			// "answered, possibly with no cards" from the first pass, and the
			// cursor keeps the answer attached to the exact fetch player that
			// asked -- the same continuation HandMoveTarget carries.
			// effHiddenPick consumes and clears all three at its top (the fx42
			// scoping discipline), so a nested pick cannot inherit the outer
			// answer.
			ctx.HiddenPick = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.HiddenPick = append(ctx.HiddenPick, o.Obj)
				}
			}
			ctx.HiddenPickDone = true
			ctx.HiddenPickTarget = rp.target
		case "hidden_pick_confirm":
			// An Optional$ confirmation on a Hidden$ True public-origin
			// ChangeZone pick was answered (the same confirmAction gate the
			// "hand_move_confirm" arm decodes): option zero accepts the fetch,
			// every other answer declines it. effHiddenPick consumes and clears
			// these at the top of its walk (fx42 scoping), so a nested pick
			// poses its own confirmation.
			ctx.HiddenPickConfirmDone = true
			ctx.HiddenPickConfirmTarget = rp.target
			ctx.HiddenPickConfirm = "no"
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.HiddenPickConfirm = "yes"
			}
		case "scry_replacement":
			ctx.ScryReplacement = true
			ctx.ScryCount, ctx.ScryProceed = rp.scryCount, rp.scryProceed
			ctx.LibraryTarget = rp.target
		case "arrange", "dig_arrange":
			// Ruling J0: rules' handleArrange already applied the answered
			// arrangement and emitted the LibraryOrder event before calling
			// resumeResolution, so the re-entered effect needs only to know
			// not to re-ask -- the arrangement lives on the LibraryOrder
			// event, not on Ctx, so this is a done-marker rather than an
			// answer the effect re-reads. A Dig additionally carries the
			// asking target's index (ArrangeTarget, only on its own
			// "dig_arrange" continuation), because its walk must keep the
			// deterministic processing of the LATER Defined$ targets on the
			// arrange re-entry instead of dropping them.
			ctx.Arrange = true
			if rp.kind == "dig_arrange" {
				ctx.ArrangeTarget = rp.target
			}
			// The shared per-player cursor every multi-library walk (search,
			// arrange, scry/surveil) reads to resume at the NEXT library; a
			// Dig's own arrange continuation additionally carries
			// ArrangeTarget so effDig's walk skips through it.
			ctx.LibraryTarget = rp.target
		case "arrange_mayshuffle":
			// A RearrangeTopOfLibrary carrying MayShuffle$ True (Ponder) asked
			// "you may shuffle?" on its arrange re-entry pass. The effect
			// re-enters here third: the arrange itself was applied two passes
			// ago, so ctx.Arrange stays the done-marker; ctx.MayShuffle carries
			// the answer as the ask-again marker the effect consumes and
			// clears. The shuffle itself is emitted HERE, before the
			// re-entered walk continues to the chained SubAbility$ (Ponder's
			// draw comes after the shuffle) -- the same Fisher-Yates over the
			// engine rng and the same Secret events.Shuffle the genesis deal
			// and every effects shuffle emit, with rp.player the library's
			// owner (the arranging player the ask was posed to).
			ctx.Arrange = true
			ctx.MayShuffle = "no"
			ctx.LibraryTarget = rp.target
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.MayShuffle = "yes"
				order := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, rp.player)...)
				e.rng.Shuffle(order)
				e.emit(events.Event{Kind: events.Shuffle, Player: rp.player, IDs: order, Secret: true})
			}
		case "discard_unless":
			// A Discard carrying UnlessType$ (Thirst for Knowledge) asked its
			// election: "discard one card of the type instead" vs "discard
			// NumCards$". The re-entered effDiscard reads the election off
			// ctx.UnlessElected (consumed and cleared there, fx42 scoping);
			// an empty or malformed answer elects the ordinary discard, the
			// conservative read of an ambiguous one.
			ctx.UnlessElected = "ordinary"
			if len(chosen) > 0 && chosen[0].Kind == "unless" {
				ctx.UnlessElected = "unless"
			}
		case "hideaway_pick":
			// Hideaway's first ask chooses exactly one of the looked-at cards.
			// The effect validates it remains in the library before moving it,
			// then asks the separate ordered-bottom question for the remainder.
			ctx.HideawayPicked = true
			if len(chosen) == 1 {
				ctx.Hideaway = chosen[0].Obj
			}
		case "hideaway_arrange":
			ctx.HideawayArranged = true
		case "soulbond":
			// Soulbond is a may choice: no option is a legitimate decline.
			ctx.SoulbondDone = true
			if len(chosen) == 1 {
				ctx.SoulbondPartner = chosen[0].Obj
			}
		case "myriad":
			// CR 702.109 makes a separate may choice for each eligible opponent.
			// ResumeTarget is that opponent's stable index in effMyriad's
			// deterministic list; only its explicit yes option creates the copy.
			ctx.MyriadDone = true
			ctx.MyriadTarget = rp.target
			ctx.MyriadCreate = len(chosen) == 1 && chosen[0].Kind == "yes"
		case "defined_library_optional":
			// An Optional$ object-valued Defined$ library fetch list (Kenessos's
			// DBBottom): option zero accepts the whole direct move; every other
			// answer declines it. The effect consumes this marker before any
			// nested optional fetch can see it.
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.DefinedLibraryMove = "yes"
			} else {
				ctx.DefinedLibraryMove = "no"
			}
		case "reveal_optional":
			// Task fb-3f1cc033 (Delver of Secrets): the peeking player's
			// RevealOptional$ yes/no was answered. Option 0 is "yes"; anything
			// else (option 1, an empty or malformed answer) is a decline — the
			// conservative read of an ambiguous answer is "no reveal". The
			// re-entered effReveal applies the answer: "yes" emits the Note
			// (which names the cards) and fires RememberRevealed$; "no" does
			// neither, so the chained ConditionDefined$ Remembered gate does
			// not fire either.
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.RevealOpt = "yes"
			} else {
				ctx.RevealOpt = "no"
			}
			// The per-target cursor: RevealOptTarget is the index of the
			// Defined$ target whose yes/no this answer was, so the re-entered
			// effReveal applies it to exactly that target and poses a fresh ask
			// for every later target (the LookAckTarget/RevealPickTarget
			// pattern). Without it, a multi-target optional reveal answered
			// for target 0 and then applied that same answer to every later
			// target — a yes silently revealed the rest, a no silently
			// declined them.
			ctx.RevealOptTarget = rp.target
		case "reveal_pick":
			// Task infernaltutor1: a mid-resolution hand-reveal pick (Infernal
			// Tutor's "Reveal a card from your hand", an AnyNumber$/Optional$
			// reveal) was answered. Every chosen option carries the revealed
			// card in Obj (the same shape the "discard" arm reads), so the id
			// list is read straight off them; the re-entered effReveal filters
			// it against the rebuilt pool and emits the reveal plus the
			// RememberRevealed$ capture for exactly those cards, which is what
			// the chained ChangeType$ Remembered.sameName sub then reads. The
			// slice is built non-nil (make, not nil) so a legitimate
			// "reveal none" answer is distinguishable from a first pass -- the
			// Ctx.Discard convention. effReveal consumes and clears it at the
			// top of its walk (fx42 scoping).
			ctx.RevealPick = make([]state.ObjID, 0, len(chosen))
			for _, o := range chosen {
				if o.Obj != 0 {
					ctx.RevealPick = append(ctx.RevealPick, o.Obj)
				}
			}
			// The per-target cursor: RevealPickTarget is the index of the
			// Defined$ target whose pick this answer was, so the re-entered
			// effReveal applies it to exactly that target's pool and poses a
			// fresh ask for every later target (the LookAckTarget pattern).
			ctx.RevealPickTarget = rp.target
		case "look_ack":
			// The bare private look's pacing ack (lookack, task
			// fb-20260917T232325Z-35cfca4b, Mishra's Bauble / Gitaxian Probe):
			// the looker clicked Continue on the "You look at ..." modal. There
			// is NO decline — the ask gates only the pacing, and CR 701.20e
			// requires the look itself to happen — so ANY answer (the single
			// Continue option; a malformed empty one included) acknowledges.
			// The answer is addressed by the per-target cursor (the DigTarget
			// pattern): LookAckTarget carries rp.target, the index of the
			// Defined$ target whose ack was answered, so the re-entered
			// effReveal emits exactly that target's note, skips the targets
			// already processed on the pass that suspended, and every later
			// bare look in the walk poses its own ack — without the cursor a
			// multi-target bare look (Case the Joint's Defined$ Player)
			// re-posed the last target's ack forever.
			ctx.LookAck = true
			ctx.LookAckTarget = rp.target
		case "draw_upto":
			// Upto$ Draw (Arcane Denial, Truce): the per-target count ask was
			// answered — the count is the number of chosen card options (the
			// options are the top cards of the target's own library; an empty
			// answer, legal at Min 0, is a draw-nothing decline, the point of
			// Upto$). The re-entered effDraw consumes the answer for exactly
			// the target the ask named (ResumeTarget, fx42 scoping), draws it,
			// then poses the next target's own ask.
			ctx.DrawUptoIdx = int32(rp.target)
			ctx.DrawUptoCount = int32(len(chosen))
			ctx.DrawUptoAnswered = true
		case "draw_optional":
			// OptionalDecider$ Draw (Mystic Remora, Rhystic Study): the
			// decider's yes/no was answered. Option 0 is "yes" (draw the
			// NumCards the unless arm did not price); anything else — option
			// 1, an empty or malformed answer — is a decline, the same
			// conservative read the reveal_optional arm takes. The re-entered
			// effDraw consumes the answer before its draw loop (fx42), so a
			// chained sub-Draw poses its own ask.
			if len(chosen) > 0 && chosen[0].Kind == "yes" {
				ctx.DrawOpt = "yes"
			} else {
				ctx.DrawOpt = "no"
			}
		case "play":
			// A Play effect (Conduit of Worlds, Spinerock Knoll) was answered:
			// each chosen option's Obj is a card to play from its current zone,
			// in answer order. An empty answer is a DECLINE of an Optional$
			// Play (effPlay now offers Min 0) -- the answer is consumed with
			// nothing begun. Only the effect's own WithoutManaCost$ grants a
			// free cast: Conduit has no such parameter, while Spinerock Knoll
			// does. A PlayCost$ alternative (task playcost1: Amped Raptor's
			// PayEnergy<ConvertedManaCost>, Anrakyr's PayLife<ConvertedManaCost>,
			// Blue Mage's Cane's fixed {3}, Cruelclaw's Discard<1/Card>) is
			// priced per chosen card -- ConvertedManaCost substitutes the
			// card's own mana value -- inside beginPlay, which hard-declines an
			// unpriceable or unpayable alternative with a loud Note instead of
			// charging full mana. An Amount$ All / N answer may name several
			// cards; each is begun in turn, and the loop stops at the first cast
			// that cannot commit synchronously (an ask inside the cast
			// transaction -- an ETB choice, a target, a mana window -- parks the
			// resolution on that cast's question, and the not-yet-begun cards
			// are dropped with a Note rather than wedging; the corpus Amount$ All
			// shapes are without-mana-cost creature/spell plays, which commit
			// synchronously). ctx.Play/PlayDone are set so the re-entered
			// effPlay sees the answer as consumed either way.
			free := strings.EqualFold(rp.sa.Params["WithoutManaCost"], "True")
			playCost := strings.TrimSpace(rp.sa.Params["PlayCost"])
			// ReplaceGraveyard$ Exile (task replplay1): the Play SA's own
			// rider — "if that spell would be put into your graveyard this
			// turn, exile it instead" — stamps the played spell's pay-time
			// CastInfo with state.FlagReplaceGraveyard so spellRestZone (and
			// spellFizzleZone for a fizzled/countered play) exiles it. The
			// conditional sibling ReplaceGraveyardValid$ (2 corpus files:
			// Bilbo, Thief in the Night; Scholar of the Lost Trove) restricts
			// the exile to named types and is unread — fail closed, keep the
			// graveyard resting place for those.
			replaceGraveyard := strings.EqualFold(strings.TrimSpace(rp.sa.Params["ReplaceGraveyard"]), "Exile") &&
				strings.TrimSpace(rp.sa.Params["ReplaceGraveyardValid"]) == ""
			// CopyCard$ True casts an event-minted copy of the selected card,
			// leaving the original in its source zone. False or absent keeps
			// the ordinary Play path.
			copyCard := strings.EqualFold(strings.TrimSpace(rp.sa.Params["CopyCard"]), "True")
			// ImprintPlayed$ True (task imprintplayed: Rashmi and Ragavan,
			// Kefka, Beseech the Mirror, Soundwave, Smuggler's Buggy — 5 corpus
			// files): every card the Play actually BEGINS to play is recorded
			// as imprinted on the resolution's source (events.Imprint, the
			// same association Chrome Mox's Imprint$ writes), so the chained
			// ConditionDefined$ Imprinted gate (DBEffect's "did you cast it
			// this way?" arm) reads a real answer. "Actually begins" is read
			// from the card's zone: a begun cast pushes the card onto the
			// stack (CR 601.2a) or moves it onward, while a declined Play — an
			// unpayable alternative, a stale answer, a reversed cast — leaves
			// it in its zone, and an aborted cast reverses it back to exactly
			// the zone it started in. The emission sits before the suspension
			// break so a cast suspended mid-transaction (a target ask inside
			// the free cast) is still recorded as played.
			imprintPlayed := strings.EqualFold(rp.sa.Params["ImprintPlayed"], "True")
			// The play's own CONTROLLER rides the answer's options (effPlay set
			// each option's Player to the Controller$-resolved seat): a
			// Controller$ Play (Word of Command's TargetedPlayer, Wild
			// Evocation's TriggeredPlayer, Spell Queller's RememberedOwner) is
			// begun BY that seat, not by the resolving ability's controller.
			// An option carrying no seat (a hand-built context, or every
			// historical Controller$-absent game whose options named the
			// resolving controller anyway) keeps ctx.Controller, the pre-
			// Controller$ read.
			player := ctx.Controller
			if len(chosen) > 0 && int(chosen[0].Player) < len(e.G.Players) {
				player = chosen[0].Player
			}
			// ShowCards$ (Sunbird's Invocation -- the corpus's one carrier):
			// the play's public reveal rider. The population it names is a
			// card filter over the walk's remembered set ("Card.IsRemembered"
			// = the window the PeekAndReveal revealed and remembered); the
			// reveal is ONE public Note (the ids payload view.Describe
			// renders) emitted BEFORE the first cast begins, while the cards
			// still sit in their hidden zones. A decline plays nothing and
			// reveals nothing. The R-9 no-host path (effPlay's deterministic
			// first candidate) never reaches this arm and stays untouched --
			// the same boundary ForgetPlayed$ keeps.
			var toPlay []state.ObjID
			for _, ch := range chosen {
				if ch.Obj != 0 {
					toPlay = append(toPlay, ch.Obj)
				}
			}
			if show := strings.TrimSpace(rp.sa.Params["ShowCards"]); show != "" && len(toPlay) > 0 {
				sc := ctx.SpecContext(player)
				var ids []state.ObjID
				seenShow := map[state.ObjID]bool{}
				for _, t := range ctx.Remembered {
					if t.IsPlayer || t.Obj == 0 || seenShow[t.Obj] {
						continue
					}
					if o := e.G.Obj(t.Obj); o != nil && e.matchesSpec(show, t.Obj, sc) {
						seenShow[t.Obj] = true
						ids = append(ids, t.Obj)
					}
				}
				if len(ids) > 0 {
					e.emit(events.Event{Kind: events.Note, Player: player, IDs: ids})
				}
			}
			ctx.PlayDone = true
			for i, id := range toPlay {
				if i == 0 {
					ctx.Play = id
				}
				from := state.Zone(0)
				if o := e.G.Obj(id); o != nil {
					from = o.Zone
				}
				e.beginPlay(player, id, free, playCost, replaceGraveyard, copyCard)
				if imprintPlayed && from.Valid() {
					if o := e.G.Obj(id); o != nil && o.Zone != from {
						e.emit(events.Event{Kind: events.Imprint, Obj: ctx.Source,
							IDs: []state.ObjID{id}})
					}
				}
				if e.Suspended() || e.cast != nil {
					if rest := toPlay[i+1:]; len(rest) > 0 {
						e.emit(events.Event{Kind: events.Note, Obj: rp.obj,
							Text: "Play stopped after a suspended cast; the remaining cards stay unplayed"})
					}
					break
				}
			}
		case "extort":
			// Extort's optional {W/B} payment was answered. Option 0 is "pay";
			// anything else is a decline. The hybrid pip is charged from the
			// caster's pool as one W or B when available; a pool lacking both
			// colours deterministically declines (the drain never runs without
			// the mana being genuinely paid). The re-entered effExtort reads
			// Ctx.Extort and runs the drain only on "pay".
			if len(chosen) > 0 && chosen[0].Index == 0 {
				if e.payExtortPip(chosen[0].Player) {
					ctx.Extort = "pay"
				} else {
					ctx.Extort = "decline"
				}
			} else {
				ctx.Extort = "decline"
			}
		case "effect_paid":
			// The trigger's Cost$ was paid by triggeredCostAnswer; run the
			// parked effect without opening the payment window a second time.
		case "charm_rest":
			// A cross-mode TargetUnique Charm's mode loop suspended mid-mode;
			// the remaining chosen modes re-enter effCharm exactly as the
			// placement answer did: Ctx.Modes names them (overriding the
			// o.Ability branch's full ChosenModes seed), effCharm's split
			// assigns each target-bearing one the last targets of the original
			// positional assignment, and nothing re-asks.
			// Non-nil even when the rest is empty (the last chosen mode
			// suspended): effCharm's re-entry branch is keyed on Ctx.Modes !=
			// nil, so an empty but present list runs no mode and resumes the
			// Charm's own Sub rather than re-posing the mode ask.
			ctx.Modes = append([]string{}, rp.charmRest...)
			// CanRepeatModes$ (CR 601.2b): the rest is a suffix of the object's
			// full ChosenModes (the walk only ever truncates a suffix), so the
			// names the earlier passes consumed are derivable exactly. Seed
			// them so effCharm's first-occurrence covered-marking knows which
			// modes already ran -- a repeated target-bearing mode's later
			// instance keeps its own ValidTgts$ pre-ask instead of inheriting
			// the shared list a second time.
			if o := e.G.Obj(rp.obj); o != nil && len(o.ChosenModes) > len(rp.charmRest) {
				ctx.ModesSeen = append(ctx.ModesSeen,
					o.ChosenModes[:len(o.ChosenModes)-len(rp.charmRest)]...)
			}
		case "villainous_rest":
			// A VillainousChoice's chosen body suspended on its own nested ask
			// (DBSac's sacrifice picker) and that ask's chain has completed.
			// Re-enter the primitive with the victim cursor restored so the
			// remaining Defined$ victims are still asked. The modes seed the
			// ability branch applied must be cleared: this frame carries no
			// answered mode (the first victim's was consumed long ago) and a
			// stale ChosenModes must not make effVillainousChoice re-run a
			// previously chosen body.
			ctx.Modes = nil
			ctx.VillainousVictims = append([]state.Target(nil), rp.villainousVictims...)
			ctx.VillainousIndex = rp.villainousIndex
		case "generic_players":
			// A multi-player api:GenericChoice's per-chooser KModes answer: the
			// chosen SVar name is scoped to the chooser whose body has not run
			// yet. Ctx.Modes names it (overriding any stale ChosenModes seed)
			// and the cursor advances PAST that chooser, so effCharm runs the
			// body once and then asks the next Defined$ chooser. Ctx.Remembered
			// is the chooser (resumeResolution bound rp.remembered), which is
			// what the body's Defined$ Remembered reads.
			ctx.Modes = []string{rp.genericChoice}
			ctx.GenericChoosers = append([]state.Target(nil), rp.genericChoosers...)
			ctx.GenericChooserIndex = rp.genericChooserIndex + 1
		case "generic_players_rest":
			// Re-enter after the chosen body's nested ask, restoring both the
			// remaining chooser cursor and the enclosing remembered set.
			ctx.Modes = nil
			ctx.GenericChoosers = append([]state.Target(nil), rp.genericChoosers...)
			ctx.GenericChooserIndex = rp.genericChooserIndex
			ctx.Remembered = append([]state.Target(nil), rp.genericRemembered...)
		case "token_rest":
			// A DB$ Token's mint parked behind a replacement-order ask and
			// the answer has minted it: re-enter the Token with its frozen
			// job, the parked mint's objects (the collector the answer
			// minted into) and the cursor of the mints still owed.
			ctx.Modes = nil
			rest := rp.tokenRest.Clone()
			rest.Parked = e.takeMintSink(rest.SinkID)
			ctx.TokenRest = &rest
		case "flip_rest":
			// A DB$ FlipCoin loop's per-flip sub-ability suspended on its own
			// mid-resolution ask (Mirror March's copy choice, say) and that
			// ask's chain has completed. Re-enter effFlipCoin with the flip
			// cursor restored so the remaining flips run. Ctx.Modes is
			// cleared for the same reason as villainous_rest: this frame
			// carries no answered mode.
			ctx.Modes = nil
			cursor := rp.flipCursor
			ctx.FlipRest = &cursor
		case "optional":
			// CR 603.5: the decider answered yes to applying this optional
			// triggered ability's effect. The answer is a yes/no, not a mode
			// choice, so nothing is written to Ctx.Modes -- it was already
			// seeded from the stack object's ChosenModes by the o.Ability
			// branch above, exactly as resolveTop's own first pass would
			// have. The re-entry below just runs the ability's effect.
			//
			// ResolvedLimit$: an ACCEPTED optional trigger is one the effect
			// runs for, so it consumes the per-turn resolution count here. A
			// decline (handleTriggerOptional's finishResumption branch) never
			// reaches resumeResolution and so never increments, exactly as the
			// oracle's "you may ... do this only once" requires. The eligibility
			// check is on the RESOLVED line's own param (the trigger
			// findTriggerForAbility matches for the resumed ability), never on
			// the source's other lines -- accepting a sibling optional trigger
			// (Tidus, Yuna's Guardian's non-RL BeginCombat line) must not spend
			// the ResolvedLimit$ line's count.
			if o != nil {
				if t, ok := e.triggerForAbilityObject(rp.obj, o); ok {
					if _, limited := resolvedLimitValue(t); limited {
						e.noteTriggerResolved(o.Source)
					}
				}
			}
		default: // "modes", and "" (a pure outer continuation with no answer)
			// A KWChoice$ pump's modes are keyword labels, not SVar names:
			// when the asking SA carries no Choices$ but a KWChoice$, the
			// chosen indexes map against THAT list (effects' effPump re-entry
			// consumes them as the granted keywords).
			eligible := []string(nil)
			if strings.TrimSpace(rp.sa.Params["Choices"]) == "" {
				if kw := strings.TrimSpace(rp.sa.Params["KWChoice"]); kw != "" {
					eligible = strings.Split(kw, ",")
					for i := range eligible {
						eligible[i] = strings.TrimSpace(eligible[i])
					}
				}
			}
			ctx.Modes = modeChoiceNames(rp.sa, chosen, eligible)
		}
		src := rp.obj
		if o.Ability != nil {
			src = o.Source
		}
		if rp.replacement && rp.replSource != 0 {
			src = rp.replSource
		}
		e.damaging = src
		// A fresh re-entry: reset the enclosing-loop continuation reports the
		// loops of THIS effects.Resolve call will accumulate. This reset is
		// safe even though resumeResolution recurses through rp.outer below:
		// an enclosing frame can only reach that recursion after its own
		// effects.Resolve returned with NO nested ask (e.resume == nil, else
		// the nested-ask branch above consumes contChain into e.resume.outer
		// and returns without recursing), at which point its contChain was
		// never needed again — so a deeper frame's reset discards only
		// reports no live frame still needs (measured: the full rules suite
		// runs no path where a recursive reset clobbers a needed report).
		e.contChain = e.contChain[:0]
		e.repeatReported = nil
		// fx44: restore the replacement context the suspended body was
		// resolving under. applyReplacements reset e.applyingReplacement to
		// false when the body suspended, so without this the resumed body's
		// own completion move is re-intercepted by the same replacement it is
		// the product of — the re-asked discard loop. It is saved and restored
		// (not just set) so a resume that reaches here already inside a
		// replacement keeps the outer context intact, exactly the discipline
		// ensureLeftTheStack and applyReplacements already practise.
		savedReplacement := e.applyingReplacement
		e.applyingReplacement = rp.replacement
		e.replReplaced, e.replAction, e.replReplacedPlayer = rp.replaced, rp.action, rp.replacedPlayer
		savedRedirect := e.replRedirect
		e.replRedirect = rp.redirect
		// The body's own re-entry (Host.SuspendUnless): the gate of THIS SA
		// had already resolved when the body posed the pending ask, so the
		// recorded marker re-enters it as an already-resolved answer — the
		// unless gate consumes it and never re-poses the pay ask (the
		// asking-body-under-UnlessCost$ livelock fix). A unless_pay resume
		// point never carries the marker: that ask was posed by the gate
		// itself, before any body ran. The arm's own authoritative answer
		// (the Ward arm's beginWardPayment outcome, the generic arm's
		// payUnlessCost outcome) wins over the marker — the gate-posed ask's
		// own suspension also records a marker (the SA carries UnlessCost$),
		// and letting it clobber the arm's answer counted a PAID ward as a
		// decline (the Kitesail Larcenist regression).
		if rp.unlessResolved != "" && ctx.UnlessPay == "" {
			ctx.UnlessPay = rp.unlessResolved
		}
		// Cross-mode TargetUnique attribution (the family runner effCharm's
		// charmCrossModeRun): a frame whose SA is (or is inside) one of the
		// chosen target-bearing modes' chains re-enters with that mode's OWN
		// target, not the stack object's undivided list — the split the
		// initial pass applied does not survive this ctx rebuild, so it is
		// re-derived here from the same deterministic inputs (ChosenModes,
		// Targets, the face's SVar table). The match is by the SA's Line (the
		// SVar body text parseSA stores): ResolveSVar parses fresh on every
		// call, so pointer identity never holds across a resume.
		if one := e.charmModeTarget(rp.obj, rp.sa); one != nil {
			ctx.Targets = one
		}
		// A MoveCounter resolution whose EARLIER rounds already answered the
		// target pre-ask, the kind pick or the amount pick re-seeds them here:
		// without this the fresh Ctx re-poses the target pre-ask on the way
		// back into the body, which re-poses the kind/amount ask, forever
		// (the movecounter1 livelock).
		if rp.sa.API == "MoveCounter" {
			e.seedMoveCounterAsk(rp.obj, ctx)
		}
		if rp.sa.API == "AddOrRemoveCounter" {
			e.seedAorAsk(rp.obj, ctx)
		}
		if rp.sa.API == "PutCounter" {
			e.seedCounterTypeAsk(rp.obj, rp.sa, ctx)
		}
		// The general form of the three seeds above: an answered generic
		// ValidTgts$ pre-ask for THIS SA, recorded by the "tgts" arm on an
		// earlier round of this same resolution. Runs last so a cursor one of
		// the API-specific seeds already set (MoveCounter writes both) wins,
		// and it never overwrites the current round's own answer.
		e.seedTargetsPick(rp.obj, rp.sa, ctx)
		// A frame of a fused half's resolution re-enters here: restore the
		// half's own target binding as the AMBIENT resolving target for the
		// whole of this re-entry (its root or sub-ability, and every frame
		// reachable through it), so a further nested ask posed below captures
		// the same half slice rather than the flat list. buildContinuationChain
		// below runs while it is still set, so the frames it stamps inherit it
		// too. Saved/restored like the replacement context just above: a
		// re-entry nested inside another fused half (never in the corpus, but
		// structural) keeps the outer binding intact.
		savedFused, savedFusedSet := e.fusedResolving, e.fusedResolvingSet
		savedSVars := e.fusedResolvingSVars
		savedWinX := e.windowPaidX
		e.fusedResolving, e.fusedResolvingSet = rp.fusedTargets, rp.fusedTargetsSet
		e.fusedResolvingSVars = rp.fusedSVars
		e.windowPaidX = rp.winPaidX
		// The VillainousChoice victim of the body this frame is resolving,
		// published as ambient state for the same reason and by the same
		// discipline as fusedResolving above: a nested ask the body poses (or
		// one a later frame in its chain poses) captures it through Ask, so
		// the nested re-entry still reads Defined$ Remembered as the victim.
		// A villainous frame's own victim is rp.remembered (the modes answer
		// recorded it); every other frame carries what its ask captured.
		savedVill, savedVillSet := e.villainousRemembered, e.villainousRememberedSet
		if rp.kind == "villainous" {
			e.villainousRemembered, e.villainousRememberedSet = rp.remembered, len(rp.remembered) > 0
		} else {
			e.villainousRemembered, e.villainousRememberedSet = rp.villainousRemembered, rp.villainousRememberedSet
		}
		// Restore only when this whole re-entry (and every rp.outer
		// continuation it recurses into) has finished: buildContinuationChain
		// in the nested-ask branch below stamps frames that must inherit the
		// same half binding, and an rp.outer recursion saves/restores its own
		// copy on top, so the deferred restore lands the original back.
		defer func() {
			e.fusedResolving, e.fusedResolvingSet, e.fusedResolvingSVars = savedFused, savedFusedSet, savedSVars
			e.windowPaidX = savedWinX
			e.villainousRemembered, e.villainousRememberedSet = savedVill, savedVillSet
		}()
		e.contChainOwners++
		effects.Resolve(e, ctx, rp.sa)
		e.contChainOwners--
		e.replReplaced, e.replAction, e.replReplacedPlayer = 0, "", state.Target{}
		e.replRedirect = savedRedirect
		e.applyingReplacement = savedReplacement
		e.damaging = 0
		// A resolution is still suspended when EITHER the ordinary
		// mid-resolution ask (e.resume) or an off-stack mana rider ask is
		// pending. The latter parks on the mana activation and sets
		// e.choosing == chooseManaColor instead of e.resume
		// (mana_activation.go's askOffStackMana), so the per-resolution answer
		// caches below must test BOTH or the next re-entry of this same SA
		// re-poses an already-answered ask -- a targeted rider that then asks
		// another question (Witch-Engine-shaped DB$ NameCard | ValidTgts$
		// Opponent) alternated tgts/name forever because the target record was
		// dropped while the name ask was still pending.
		parked := e.resume != nil || e.choosing == chooseManaColor
		if rp.sa.API == "MoveCounter" && !parked {
			// The MoveCounter resolution completed this round (nothing
			// suspended): its pending state is spent.
			delete(e.moveCounterAsk, rp.obj)
		}
		if rp.sa.API == "AddOrRemoveCounter" && !parked {
			// The AddOrRemoveCounter resolution completed this round (nothing
			// suspended): its pending state is spent -- delete it so a stale
			// entry can never seed a later resolution of the same object (the
			// moveCounterAsk discipline).
			delete(e.aorAsk, rp.obj)
		}
		if rp.sa.API == "PutCounter" && !parked {
			delete(e.counterTypeAsk, rp.obj)
		}
		if !parked {
			// This SA's resolution completed this round (nothing suspended),
			// so its recorded pre-ask answer is spent -- drop it so a later
			// re-entry of the same body asks afresh.
			e.forgetTargetsPick(rp.obj, rp.sa)
		}
		if e.resume != nil {
			// The re-entry posed a nested mid-resolution ask. The new
			// pending point (e.resume) has no outer yet: it must, once its
			// own answer is applied, continue at the rest of THIS re-entry's
			// chain (the loops effects.Resolve reported via
			// SuspendContinuation this pass) and then at rp.outer — the
			// continuation the frame we were re-entering was itself carrying.
			// Linking them now means the nested ask, when answered, resumes
			// every suspended continuation rather than dropping the outer
			// ones (fx32).
			if rp.loopBound {
				// Still inside the loop iteration this frame resumed: whatever
				// suspended at this level continues with its Remembered.
				e.bindLoopFrames(ctx.Remembered, ctx.VoteCounts)
			}
			e.resume.outer = e.buildContinuationChain(e.contChain, rp.obj, rp.outer)
			// The continuation chain now owns the reported frames. Keep this
			// per-pass scratch empty at the suspension boundary.
			e.contChain = e.contChain[:0]
			return
		}
		e.contChain = e.contChain[:0]
	} else if !parkedDraws && rp.kind != "replacement" && rp.kind != "etb" && rp.fuseAlt == nil {
		// A resume with no sub-ability recorded: normally reachable only from
		// a hand-built Ask (every real asking primitive sets ResumeSA). Three
		// deliberate exceptions need no Note either: a parked GainLife→Draw
		// frame whose answer was applied above, an as-enters entry choice
		// (resumeETBEntry has already re-emitted the entry this frame was
		// parked on, so the continuation likewise begins at rp.outer), and a
		// replacement-order
		// decision — the intercepted event has already completed, and the
		// continuation begins at rp.outer rather than re-running the effect
		// that proposed it. The resolution still finishes — the object leaves
		// the stack with no effect, the same degrade-to-nothing stance as an
		// unrecognised choice, rather than stalling the match forever.
		e.emit(events.Event{Kind: events.Note, Obj: rp.obj,
			Text: "mid-resolution answer resumed with no sub-ability recorded"})
	}
	if rp.replacement && o.Zone != state.ZStack {
		// An Updated ETB replacement has already completed the spell's move.
		// Its answer resumes only the replacement body; there is no stack
		// object to finish or priority round to create here.
		//
		// The suspended body may equally have been a fully-replaced entry that
		// never puts the land on the battlefield: this re-entry posed no new
		// ask (the nested-ask branch above returns), so the entry is done
		// either way and the land play settles here.
		e.settleLandPlayIfDone(rp.replaced)
		if rp.outer != nil {
			// The body interrupted a stack resolution whose frame was chained
			// behind this one (settleReplacementQueue: a replacement-order
			// answer whose chosen body asked, e.g. a shock land's UnlessCost
			// under a mass return). The body is done; the interrupted
			// resolution continues -- dropping it here left the resolving
			// object on the stack for resolveTop to re-resolve from the top.
			e.resumeResolution(rp.outer, nil)
		}
		return
	}
	if rp.outer != nil { // No nested ask this pass and the frame itself completed: continue
		// outward through the runner-up continuations this frame carried.
		if rp.loopBound {
			// Hand this frame's Remembered to the next frame. A loop frame
			// folds in what the finished iteration remembered. Any other next
			// frame runs at this frame's level -- the rest of the same
			// iteration, or (after a loop frame) the rest of the chain that
			// enclosed the loop -- and takes it as is, so what the loop
			// remembered is not lost to the stack object's stale Remembered.
			if next := rp.outer; next.kind == "repeat" && next.repeat != nil {
				next.repeat.last = append([]state.Target(nil), ctx.Remembered...)
				next.repeat.hasLast = true
			} else {
				next.loopBound = true
				next.loopRemembered = append([]state.Target(nil), ctx.Remembered...)
			}
		}
		e.resumeResolution(rp.outer, nil)
		if parkedDraws {
			e.askNextReplacementChoice()
		}
		return
	}
	e.finishResumption(rp.obj)
	// CR 117.3b / the counterpart of handlePriority's pass-branch grant: when
	// a SUSPENDED resolution completes (this is the outermost frame -- a
	// nested ask returns in the e.resume != nil branch above, and an outer
	// continuation recurses before reaching this line, so a resolution that
	// suspends more than once still reaches here exactly once), the pass
	// count resets and priority returns to the active player. The now-
	// suppressed unconditional emit in handlePriority's pass branch used to
	// log this at suspension time while the engine was parked on a
	// mid-resolution question, i.e. while nobody had priority; this emit puts
	// the reset and the "back to active" marker at the resolution's true end.
	// The answering Submit's own tail then grants the next priority round (its
	// grantPriority reads the passes this emit has just reset to zero), which
	// is the same two-event shape an unsuspended resolution already produces
	// (pass-branch grant + grantPriority), so the suspended path now matches.
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
	if parkedDraws {
		// The cascade's true end: any replacement-order choice the
		// interrupted pass left queued is asked now, after the resolution's
		// completion marker, never before it.
		e.askNextReplacementChoice()
	}
}

// charmModeTarget re-derives, for a frame of a cross-mode TargetUnique
// Charm's resolution (see SuspendCharmRest / the charm_rest kind), the ONE
// target the SA's chain belongs to: the j-th positional entry of the stack
// object's Targets, where j is the frame's position among the chosen
// target-bearing modes. Returns nil whenever the frame is not part of such a
// charm's resolution — including every non-family shape (classification None
// or Unsupported) and every insufficient-candidate fallback (fewer recorded
// targets than the chosen target-bearing modes need) — so those keep the
// shared list byte-identically. sa == nil is the no-answer continuation
// shape the Line-match cannot serve; the charm_rest frame itself carries the
// Charm SA, whose body text never equals a mode body's, so it matches
// nothing and effCharm's own split handles it.
func (e *Engine) charmModeTarget(obj state.ObjID, sa *cards.SA) []state.Target {
	if sa == nil {
		return nil
	}
	o := e.G.Obj(obj)
	if o == nil || o.Ability == nil || len(o.ChosenModes) == 0 || len(o.Targets) == 0 {
		return nil
	}
	src := e.G.Obj(o.Source)
	if src == nil || src.Face() == nil {
		return nil
	}
	svars := src.Face().SVars
	choices := strings.Split(o.Ability.Params["Choices"], ",")
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	if status, _ := effects.CharmCrossModeShape(svars, choices); status != effects.CharmUniqueSupported {
		return nil
	}
	var tbms []*cards.SA
	for _, name := range o.ChosenModes {
		if sub := cards.ResolveSVar(svars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			tbms = append(tbms, sub)
		}
	}
	if len(tbms) < 2 || len(o.Targets) < len(tbms) {
		return nil
	}
	for j, sub := range tbms {
		for w := sub; w != nil; w = w.Sub {
			if w.Line != "" && w.Line == sa.Line {
				return []state.Target{o.Targets[j]}
			}
		}
	}
	return nil
}

// buildContinuationChain turns the enclosing-loop suspension points reported
// for one re-entry into a linked run of pure-continuation frames (kind ""),
// in report order — inner continuations first, outer last — and chains the
// given `tail` (the outer continuation of the frame being re-entered) onto
// the end. The resulting head is the what the new pending point must run
// after its own answer, or nil if there is nothing left to continue.
func (e *Engine) buildContinuationChain(frames []contFrame, obj state.ObjID, tail *resumePoint) *resumePoint {
	var head, prev *resumePoint
	for _, cf := range frames {
		sa := cf.sa
		// fx44: a continuation frame is the rest of the same resolution that
		// just suspended, so it carries the replacement context too — a body
		// that asks again and then continues must keep emitting under the
		// replacement guard, not re-interposed by the replacement it is the
		// product of, and must keep addressing the object it replaced (the
		// replaced/id carried by this frame comes from the engine's active
		// replacement context).
		f := &resumePoint{obj: obj, sa: sa.Sub, replacement: e.applyingReplacement,
			replaced: e.replReplaced, action: e.replAction, replacedPlayer: e.replReplacedPlayer,
			redirect:  e.replRedirect,
			before:    e.triggerBefore,
			loopBound: cf.bound, loopRemembered: cf.remembered, repeatSubject: cf.repeatSubject,
			voteCounts: cloneVoteCounts(cf.voteCounts),
			// Every continuation the loop of THIS re-entry reported belongs to
			// the same resolution, so a fused half's target binding is inherited
			// verbatim: the frames build while Engine.fusedResolving is set (the
			// callers set it around effects.Resolve and keep it set through this
			// build), and a frame that re-enters a loop body inside the half
			// must bind the half's slice, not the flat list (Flesh // Blood's
			// SubAbility reached through an enclosing loop).
			fusedTargets:    append([]state.Target(nil), e.fusedResolving...),
			fusedTargetsSet: e.fusedResolvingSet,
			fusedSVars:      e.fusedResolvingSVars,
			winPaidX:        e.windowPaidX,
			// The VillainousChoice victim ambient (the fusedResolving pattern):
			// a continuation frame of the chosen body's chain keeps the victim
			// so an ask posed by a later frame of that chain still reads
			// Defined$ Remembered as the victim. A frame built outside a
			// chosen body inherits nil/absent and binds nothing.
			villainousRemembered:    append([]state.Target(nil), e.villainousRemembered...),
			villainousRememberedSet: e.villainousRememberedSet}
		// Every continuation frame re-enters a loop of the SAME resolution as
		// the pending ask, rebuilding its Ctx from the stack object's targets;
		// inherit that resolution's pre-move controller snapshot so a
		// chained TokenOwner$ TargetedController reached through an enclosing
		// loop still sees it (the pending point was built by Ask from the
		// same chain's published map).
		if e.resume != nil {
			f.targetControllerLKI = effects.CloneTargetControllerLKI(e.resume.targetControllerLKI)
			f.targetCountersLKI = effects.CloneTargetCountersLKI(e.resume.targetCountersLKI)
			f.targetSpellLKI = effects.CloneTargetSpellLKI(e.resume.targetSpellLKI)
			f.replacedCards = append([]state.ObjID(nil), e.resume.replacedCards...)
		}
		// The same-resolution flip memory (Engine.Ask captured it off
		// Engine.resolvingFlipMemory onto the pending point): a continuation
		// frame of the same resolution carries the same shared pointer, so a
		// resumed FlipCoin cursor re-entry (the "flip_rest" frame) and any
		// chained Defined$ FlippedHeads / FlippedTails / Count$RememberedNumber
		// reader in a later frame still see the flips performed before the
		// suspension. The memory is mutated in place (effects.flipRecord), so a
		// pointer copy — never a value clone — keeps every frame live.
		if e.resume != nil {
			f.flipMemory = e.resume.flipMemory
		}
		if e.replacingEvent != nil && e.replacingEvent.Kind == events.Damage {
			f.replacementTarget = state.Target{Obj: e.replacingEvent.Obj}
			if e.replacingEvent.Obj == 0 {
				f.replacementTarget = state.Target{Player: e.replacingEvent.Player, IsPlayer: true}
			}
			f.replacementAmount = e.replacingEvent.Amount
			f.replacementSource = e.protectionSource(e.damaging)
		}
		if cf.deferredAsk != nil {
			// A deferred second ask (Engine.Ask): its own resume point was
			// fully built when it was asked; this frame only poses it.
			f = &resumePoint{kind: "deferred_ask", obj: obj,
				deferredAsk: cf.deferredAsk, deferredResume: cf.deferredResume}
		} else if cf.charmRest != nil {
			// The Charm re-enters ITSELF (rp.sa = the Charm SA, not sa.Sub — a
			// Charm body has no SubAbility$ chain of its own to resume) with
			// Ctx.Modes = the remaining chosen modes.
			f.kind, f.sa, f.charmRest = "charm_rest", sa, cf.charmRest
		} else if cf.villainousRest {
			// The VillainousChoice re-enters ITSELF (rp.sa = the VillainousChoice
			// SA, not sa.Sub — a VillainousChoice body has no SubAbility$ chain
			// of its own to resume) with the victim cursor restored.
			f.kind, f.sa = "villainous_rest", sa
			f.villainousVictims = append([]state.Target(nil), cf.villainousVictims...)
			f.villainousIndex = cf.villainousIndex
		} else if cf.genericChoiceRest {
			// The GenericChoice re-enters ITSELF (rp.sa = the GenericChoice SA,
			// not sa.Sub — its own SubAbility$ chain runs only after every
			// chooser has answered) with the chooser cursor restored.
			f.kind, f.sa = "generic_players_rest", sa
			f.genericChoosers = append([]state.Target(nil), cf.genericChoosers...)
			f.genericChooserIndex = cf.genericChooserIndex
			f.genericRemembered = append([]state.Target(nil), cf.genericRemembered...)
		} else if cf.flipRest {
			// The FlipCoin re-enters ITSELF (rp.sa = the FlipCoin SA, not
			// sa.Sub — a FlipCoin body has no SubAbility$ chain of its own to
			// resume) with the flip cursor restored.
			f.kind, f.sa = "flip_rest", sa
			f.flipCursor = cf.flipCursor
		} else if cf.tokenRest != nil {
			// The Token re-enters ITSELF (rp.sa = the Token SA): the parked
			// mint's riders, the mints after it and the post-loop work run
			// there, and the re-entry's own walk then continues at sa.Sub.
			f.kind, f.sa = "token_rest", sa
			f.tokenRest = cf.tokenRest.Clone()
			f.unlessResolved = cf.unlessResolved
		} else if cf.repeat != nil {
			f.kind, f.sa, f.repeat = "repeat", sa, cf.repeat
			if cf.repeat.optional {
				f.kind, f.sa = "repeat_optional_loop", sa
			}
			f.choices, f.chosenValid = cf.choices, cf.chosenValid
		}
		if head == nil {
			head = f
		} else {
			prev.outer = f
		}
		prev = f
	}
	if prev != nil {
		prev.outer = tail
	} else {
		head = tail
	}
	return head
}

// finishResumption is the shared tail of resolveTop and resumeResolution: a
// fully resolved spell leaves the stack for the battlefield when it is a
// permanent (CR 608.3), otherwise to its resting zone (exile for a
// Flashback cast or a copy, the graveyard for the rest).
// ensureLeftTheStack then guards the same replacement-discarded-the-move
// corner both callers already guard, so a resolution can never leave its
// object resolving forever.
func (e *Engine) finishResumption(id state.ObjID) {
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		return // the continuation already moved it (or it ceased to exist).
	}
	if e.G.Obj(id).Ability != nil {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
		e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this resolved "+
			"ability's own move off the stack without relocating it anywhere; sent to exile "+
			"instead of re-resolving forever")
		return
	}
	e.moveResolvedOffStack(e.G.Obj(id))
}

// chosenModeLabels returns the human-facing labels in answer order.
func chosenModeLabels(chosen []decision.Option) []string {
	labels := make([]string, 0, len(chosen))
	for _, o := range chosen {
		labels = append(labels, o.Label)
	}
	return labels
}

// modeDecision builds the shared KModes option vocabulary used by spell
// announcement and triggered-ability placement. min and max are resolved by
// effects.CharmModeBounds against the caller's complete effects context, and
// repeat is its CanRepeatModes$ result: when set, the decision permits the
// same mode index more than once and max is NOT clamped to the distinct-mode
// count.
func modeDecision(p state.PlayerID, source state.ObjID, sa *cards.SA, svars map[string]string, min, max int, repeat bool) *decision.Decision {
	choices := strings.Split(sa.Params["Choices"], ",")
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	return modeDecisionForChoices(p, source, sa, svars, choices, min, max, repeat)
}

// modeDecisionForChoices is modeDecision over an explicit eligible subset.
// Casting uses it to omit modes whose mandatory targets cannot be chosen;
// ResumeModes preserves the SVar vocabulary server-side while Index stays
// dense for the wire. repeat marks a CanRepeatModes$ Charm: the same eligible
// mode may fill several slots, and the max clamp is skipped so a CharmNum$
// larger than the eligible count is still satisfiable (by repetition).
func modeDecisionForChoices(p state.PlayerID, source state.ObjID, sa *cards.SA, svars map[string]string, choices []string, min, max int, repeat bool) *decision.Decision {
	if !repeat && max > len(choices) {
		max = len(choices)
	}
	d := &decision.Decision{Player: p, Kind: decision.KModes, Min: min, Max: max,
		Source: source, Repeatable: repeat,
		ResumeKind: "modes", ResumeSA: sa,
		ResumeModes: append([]string(nil), choices...),
		Prompt:      "Choose " + strconv.Itoa(min) + " to " + strconv.Itoa(max) + " mode(s)"}
	for i, name := range choices {
		label := name
		if sub := cards.ResolveSVar(svars, name); sub != nil {
			if desc := strings.TrimSpace(sub.Params["SpellDescription"]); desc != "" {
				label = desc
			}
		}
		d.Options = append(d.Options, decision.Option{
			Index: i, Kind: "mode", Label: label, Obj: source, Player: p})
	}
	return d
}

// modeLabels maps SVar names to their human-facing option labels. abortCast
// uses it for the reverse ModeChosen marker that accompanies restoring the
// pre-proposal ChosenModes cache.
func modeLabels(sa *cards.SA, svars map[string]string, names []string) []string {
	labels := make([]string, 0, len(names))
	for _, name := range names {
		label := name
		if sub := cards.ResolveSVar(svars, name); sub != nil {
			if desc := strings.TrimSpace(sub.Params["SpellDescription"]); desc != "" {
				label = desc
			}
		}
		labels = append(labels, label)
	}
	return labels
}

// modeChoiceNames maps the chosen modal options back to the SVar names of
// the Choices$ sub-abilities they pick, in the order chosen — the answer
// effCharm's re-entry reads (Ctx.Modes). eligible carries a filtered cast
// decision's server-only vocabulary; nil falls back to the SA's full Choices$
// list for placement and mid-resolution decisions. Out-of-range indices drop.
func modeChoiceNames(sa *cards.SA, chosen []decision.Option, eligible []string) []string {
	if sa == nil {
		return nil
	}
	choices := eligible
	if choices == nil {
		choices = strings.Split(sa.Params["Choices"], ",")
		for i := range choices {
			choices[i] = strings.TrimSpace(choices[i])
		}
	}
	names := make([]string, 0, len(chosen))
	for _, o := range chosen {
		if o.Index >= 0 && o.Index < len(choices) {
			names = append(names, choices[o.Index])
		}
	}
	return names
}

// moveResolvedOffStack is the shared tail of resolveTop and
// resumeResolution: a fully resolved spell leaves the stack for the
// battlefield when it is a permanent (CR 608.3), otherwise to its resting
// zone (exile for a Flashback cast or a copy, the graveyard for the rest).
// ensureLeftTheStack then guards the same replacement-discarded-the-move
// corner both callers already guard, so a resolution can never leave its
// object resolving forever.
func (e *Engine) moveResolvedOffStack(o *state.Object) {
	if o == nil || o.Zone != state.ZStack {
		return
	}
	id := o.ID
	if f := o.Face(); f != nil && f.IsPermanent() {
		// Morph / Megamorph / Disguise (CR 708.5): a face-down cast's spell
		// enters the battlefield face down. The entry marker re-carries the
		// same payload events.Apply folded onto the stack object at the
		// PutOnStack, so the battlefield entry takes the manifest decode's
		// face-down fold: CR 708.5's 2/2 colourless creature, with the cloak
		// marker adding the ward {2} a Disguise entry carries. The morph
		// flags (not the stack object's transient FaceDown bit) are the
		// gate, so a stack COPY of a face-down spell -- it inherits the
		// flags, the StackCopy way -- enters face down too, and every
		// ordinary permanent's entry stays byte-identical.
		if morph := o.CastFlags & (state.FlagMorphed | state.FlagMegamorphed | state.FlagDisguised); morph != 0 {
			marker := events.FaceDownEntryCounter
			if morph&state.FlagDisguised != 0 {
				marker = events.CloakEntryCounter
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZBattlefield, Counter: marker})
		} else {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZBattlefield})
		}
		// An as-enters choice parks this move through the mid-resolution ask
		// path. Keep the object on the stack until the answer re-emits it.
		if e.pending != nil || e.resume != nil {
			return
		}
		e.ensureLeftTheStack(id, spellRestZone(o), "an ETB replacement fully replaced this "+
			"permanent's entry to the battlefield without moving it anywhere; sent to its "+
			"resting zone instead of re-resolving forever")
		return
	}
	rest := spellRestZone(o)
	// CR 702.95a: a hand-cast Rebound spell is exiled as it resolves and
	// leaves a delayed promise to recast it at its controller's next upkeep.
	// Both the flag and the controller are captured before the MoveZone:
	// events.Apply resets CastFlags on the stack->exile move, and the emit
	// below runs synchronously. The registration is created only when the
	// card actually reached exile -- a replacement that redirected the move
	// leaves the promise uncreated, so no stale permission can outlive it.
	// FlagRebound is a cast-provenance bit, so a stack COPY -- put on the
	// stack, never cast (CR 707.10) -- carries none at the mint and registers
	// nothing. (No corpus card grants Rebound to a permanent, so the
	// permanent branch above's lack of a registration site stays
	// corpus-unreachable.)
	rebound := o.CastFlags&state.FlagRebound != 0
	controller := o.Controller
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: rest})
	if rebound && rest == state.ZExile {
		if cur := e.G.Obj(id); cur != nil && cur.Zone == state.ZExile {
			e.registerRebound(id, controller)
		}
	}
	e.ensureLeftTheStack(id, rest, "a replacement fully discarded this resolved "+
		"spell's own move off the stack without relocating it anywhere; sent to its "+
		"resting zone instead of re-resolving forever")
}

// registerRebound creates the CR 702.95a delayed trigger for a Rebound spell
// that was just exiled: at the controller's next upkeep, that player may cast
// the card from exile without paying its mana cost. The registration's source
// IS the exiled card, so the builtin __kwReboundCast body's Card.Self names
// it, and ValidZone$ Exile keeps the offer to the exile zone even if the card
// has left it (or later returns). ValidPlayer$ You gates the fire to the
// registration controller's own upkeep, and the one-shot DelayedPush consumes
// the registration on its first firing, so the permission never outlives the
// next upkeep.
func (e *Engine) registerRebound(id state.ObjID, controller state.PlayerID) {
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id, Player: controller,
		Step: state.StepUpkeep, Counter: "__kwReboundCast", Text: "Upkeep|VP=You"})
}

// payUnlessDamageCost lands the damage an accepting opponent chose to take
// from Sacrifice's damage-payment offer (Vexing Devil's "any opponent may
// have it deal 4 damage to them"). rules owns payment events, so the Damage
// event is emitted here — never in effects — exactly the split payMana's
// ManaAdd events already follow. The source is the offering permanent (the
// resolving ability object unwrapped to its source, the same rule
// effects.resolveSourceObject applies), published through SetDamageSource so
// the emit-side protection check (CR 702.16d) and DamageDone trigger
// matching see the real source, and the lifelink rider (CR 702.15a) is paid
// for its controller when the hit actually landed (a prevention or other
// replacement that substituted the event pays no life, the same gate
// rules/combat.go's rider uses).
func (e *Engine) payUnlessDamageCost(ctx *effects.Ctx, payer state.PlayerID, n int) {
	if n <= 0 {
		return
	}
	source := ctx.Source
	if o := e.G.Obj(source); o != nil && o.Ability != nil && o.Source != 0 {
		source = o.Source
	}
	keywords := e.damageKeywordsOf(source)
	controller := ctx.Controller
	if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield {
		controller = o.Controller
	} else if lki, ok := ctx.DamageSourceLKI[source]; ok {
		keywords = damageKeywordLKI{lifelink: lki.Lifelink, infect: lki.Infect,
			wither: lki.Wither, deathtouch: lki.Deathtouch}
		controller = lki.Controller
	}
	prev := e.SetDamageSource(source)
	dam := events.Event{Kind: events.Damage, Player: payer, Amount: int32(n)}
	if keywords.infect {
		dam.Counter = "infect"
	}
	ev := e.emit(dam)
	e.SetDamageSource(prev)
	if ev.Kind != events.Damage || !keywords.lifelink {
		return
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: controller, Amount: int32(n)})
}

// bindLoopFrames binds the pending ask and every continuation frame recorded
// this pass that no deeper RepeatEach already bound to remembered. votes is
// the AmountFromVotes$ tally the current loop frame carries (nil outside such
// a loop); it is bound to the same frames so a nested ask posed after the
// resume restores the table too.
func (e *Engine) bindLoopFrames(remembered []state.Target, votes []effects.VoteCount) {
	snap := append([]state.Target(nil), remembered...)
	if e.resume != nil && !e.resume.loopBound {
		e.resume.loopBound, e.resume.loopRemembered = true, snap
		e.resume.voteCounts = cloneVoteCounts(votes)
	}
	for i := range e.contChain {
		if !e.contChain[i].bound {
			e.contChain[i].bound, e.contChain[i].remembered = true, snap
			e.contChain[i].voteCounts = cloneVoteCounts(votes)
		}
	}
}

// charmModeScope and charmModeScopeSA read the per-mode Charm narrowing off
// the live resolution Ctx at ask time (effects.Ctx.CharmModeScope). Nil
// whenever the resolution is not inside a distinct modal Charm's mode.
func (e *Engine) charmModeScope() []state.Target {
	if e.resolutionCtx == nil {
		return nil
	}
	return append([]state.Target(nil), e.resolutionCtx.CharmModeScope...)
}

func (e *Engine) charmModeScopeSA() *cards.SA {
	if e.resolutionCtx == nil {
		return nil
	}
	return e.resolutionCtx.CharmModeSA
}

// applyCastModes records a CR 601.2b cast-time mode announcement (the
// "cast_modes" answer) on the proposed spell and re-enters continueCast. It is
// the answer handler's body, shared with castModeAsk's no-ask path: a modal
// spell whose only legal announcement is the empty one ("choose up to two"
// with no mode that has a legal target) announces zero modes without posting
// a decision nobody could answer differently.
func (e *Engine) applyCastModes(d *decision.Decision, player state.PlayerID, chosen []decision.Option) {
	pc := e.cast
	if pc == nil || pc.ability >= 0 || pc.stackObj == 0 {
		e.emit(events.Event{Kind: events.Note, Player: player,
			Text: "cast modes answered with no spell proposal pending"})
		return
	}
	names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes)
	labels := chosenModeLabels(chosen)
	// ChoiceRestriction$: log each announced mode on the SPELL object so a
	// later Charm of the same source sees the pick. A no-op unless the SA
	// carries the param.
	effects.RecordCharmChoices(e, pc.card, d.ResumeSA, names)
	if o := e.G.Obj(pc.stackObj); o != nil {
		if !pc.modeChosen {
			pc.preModes = state.CloneChosenModes(o.ChosenModes)
			pc.modeChosen = true
		}
		// Non-nil even for a zero-mode announcement: resolution must run
		// no mode, not re-pose the modal ask (state.CloneChosenModes).
		o.ChosenModes = append(make([]string, 0, len(names)), names...)
	}
	// CR 702.171b: a Spree/Tiered cast pays each chosen mode's own
	// ModeCost$ on top of the printed cost -- the same additional-cost
	// composition beginCast folds for Kicker, but per chosen mode and so
	// only known once the CR 601.2b mode answer is in. Folded into pc.cost
	// here (once; the guard survives a Clone) so the CR 601.2g mana window,
	// the cost modifiers and the final payment all see the composed total.
	// An unaffordable total aborts through the ordinary payment-reversal
	// path (CR 733.1) -- this branch never silently discounts or drops a
	// chosen mode.
	if !pc.modeCostsDone {
		pc.modeCostsDone = true
		pc.cost = pc.cost.Plus(modeCostTotal(e.G.Obj(pc.card).Face(), names))
	}
	// Escalate (the modal additional cost): a cast choosing N modes pays
	// the escalate cost N-1 times. Folded into pc.cost once, exactly like
	// the ModeCost$ fold above, so the tap/discard part asks the
	// continueCast re-entry below walks ask for the extra resources and
	// the payment window charges the composed total. An unpriceable
	// parameter (ParseCost's degraded Unknown tokens) is a loud no-charge,
	// never a fabricated generic. A one-mode cast folds nothing and stays
	// byte-identical.
	if pc.escalateSet && !pc.escalateDone && len(names) > 1 {
		pc.escalateDone = true
		if esc := ParseCost(pc.escalateParam); len(esc.Unknown) == 0 {
			for i := 1; i < len(names); i++ {
				pc.cost = pc.cost.Plus(esc)
			}
		} else {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				Text: "escalate cost unpriceable; casting without the escalate charge"})
		}
	}
	e.emit(events.Event{Kind: events.ModeChosen, Obj: pc.stackObj, Player: player,
		Text: strings.Join(labels, ",")})
	e.continueCast()
}
