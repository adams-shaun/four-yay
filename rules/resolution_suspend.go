// resolution_suspend.go holds the Suspend* family: what records a suspension (SuspendUnless, SuspendContinuation, the repeat/charm/villainous/generic/flip rests) and the per-kind seeded asks (move-counter, targets pick, AOR election).

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

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
	return resumed || e.unlessPayment != nil || e.cumulative != nil || e.triggerCost != nil || e.echo != nil
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
	e.contChain = append(e.contChain, contFrame{sa: sa, ctx: e.resolutionCtx})
}

// SuspendRepeatBody implements effects.Host.SuspendRepeatBody. The body of
// api:Repeat iteration next-1 owns the pending ask; this frame runs only
// after that body resumes and completes, and it re-enters the Repeat at the
// between-iteration step for iteration next: the gate, then the do/while
// election for a RepeatOptional$ (never that iteration's body directly --
// the do/while owes the player the election after every process), or
// iteration next's body for a counted or gated Repeat. count is the bound the
// suspended pass resolved, so the re-entered loop keeps it.
func (e *Engine) SuspendRepeatBody(sa *cards.SA, next, count int32) {
	if e.resume == nil {
		return
	}
	e.contChain = append(e.contChain, contFrame{
		sa: sa, ctx: e.resolutionCtx,
		repeat: &repeatCursor{next: int(next), count: int(count), body: true},
	})
	e.repeatReported = sa
}

// SuspendRepeat implements effects.Host.SuspendRepeat. Everything recorded so
// far in this pass -- the pending ask and the continuation frames of loops
// nested inside the iteration -- resumes inside that iteration, so each is
// bound to the iteration's Remembered unless a deeper loop already bound it.
// The loop's own frame follows them, bound to the RepeatEach's Remembered.
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
	cursor := rest
	cursor.Players = append([]state.PlayerID(nil), rest.Players...)
	e.contChain = append(e.contChain, contFrame{sa: sa, flipRest: true, flipCursor: cursor})
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
