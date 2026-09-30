package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// engineContinuation groups the Engine's continuation, ask-parking and round-ask state. It is embedded by value in
// Engine (rules/engine_struct.go), so every field keeps its documented
// contract comment and every existing e.<field> access keeps compiling
// unchanged through Go's field promotion. Clone's per-field copy
// classes (rules/clone.go) are unchanged by the move.
type engineContinuation struct {
	// choosing says which flow is waiting on the current KChoose decision
	// (Task 8). It is plain data, not a closure, so Engine.Clone (a sibling
	// branch, not yet in this worktree) can copy it like any other field --
	// a closure captured over this Engine's own pointers would not survive a
	// clone at all. handleChoose (turn.go) switches on it; Task 9 adds
	// chooseCast in cast.go, and Tasks 12 and 18 add the "as this enters" and
	// miracle cases in their own files.
	choosing chooseFor

	// oppSel (rules/stack.go) is the TargetingPlayer$ Opponent
	// controller-selection ask's flow record: set when poseOpponentPick
	// posts the which-opponent ask at a rules-tier ask site, flipped to done
	// by answerOppPick, consumed when the re-posed target ask reads it.
	// Plain scalars, so Clone carries it like the blockerRound class.
	oppSel oppSelectState

	// oppPicksMid is the effects-tier answered-selection store, keyed by the
	// asking SA's line: the "opp_pick" resume arm records the controller's
	// chosen opponent there and the re-entered walk's ChooserFor consumes it.
	// The entry only lives between the arm and the synchronous read, so no
	// entry can outlive the ask it belongs to. Clone copies it.
	oppPicksMid map[string]state.PlayerID

	// tpCtlChooser (rules/stack.go) is the TargetingPlayerControls$ answered
	// record (tpc1): the seat that answered a target ask whose SA carries
	// `TargetingPlayerControls$ True`, keyed by the RESOLVING stack object
	// (pc.stackObj for a cast/activation, the TriggerPush object for a
	// placement ask) and carrying the asking SA's line. The entry lives from
	// the ask's answer until the object leaves the stack, so the CR 608.2b
	// recheck (legalTargets, which reads it via its self parameter) judges
	// the restriction against exactly the seat that answered. Clone copies it.
	tpCtlChooser map[state.ObjID]tpCtlAnswer

	// resume is non-nil while a mid-resolution decision is pending: an effect
	// (a nested effCharm pick, effCopySpellAbility's UnlessCost$ may-pay,
	// effDiscard's mode choices — M2d-2) asked through effects.Host.Ask and
	// the resolution of the top-of-stack object is suspended with the object
	// still on the stack. It chains every suspended continuation, innermost
	// first, via resumePoint.outer (fx34): a nested ask no longer overwrites
	// its enclosing continuation, so the outer chain runs once everything
	// inside it resolves. It is plain value/pointer data (kind, obj, the
	// shared-immutable *cards.SA and the linked outer chain), never a
	// closure, so Clone copies it like cast/choosing and a replay re-derives
	// the same branch. resolveTop checks it after each resolution pass;
	// handleModes clears it and calls resumeResolution (rules/resolution.go)
	// with the recorded answer. Nil whenever no resolution is suspended.
	resume *resumePoint

	// contChain accumulates the enclosing-loop suspension points reported by
	// effects.Resolve during the current resumeResolution re-entry (through
	// effects.Host.SuspendContinuation), so resumeResolution can link them as
	// outer continuations. It is transient engine state: rebuilt on every
	// re-entry, drained into the resume chain as soon as that re-entry
	// suspends again, and nil whenever no re-entry is in flight — so a Clone
	// need not carry it (the same resolution re-derives the same chain).
	contChain []contFrame
	// contChainOwners counts the resolution passes in flight whose contChain
	// will be drained into the pending resume chain when they suspend (the
	// initial stack passes, a fused half, a resumeResolution re-entry). A
	// second mid-resolution ask is deferred onto contChain (Engine.Ask) only
	// while one is, so a deferred ask can never be stranded on a chain nobody
	// consumes; outside one the overwrite guard in Engine.ask still fires.
	// Transient, zero between intents.
	contChainOwners int
	// answerParked is the resolution frame an ANSWER handler found parked on
	// e.resume while it applies a ReplaceWith$ body outside any resolution
	// pass (resolveReplacementBody): a CR 616.1 order choice posed mid-
	// resolution parks the resolution that proposed the event, and the
	// chosen body runs from handleReplacement with that frame still there.
	// While it is the only suspension (e.resume == answerParked, nothing
	// pending) Suspended reports false, so the body walks its own SubAbility$
	// chain instead of stopping after its head as if it had asked.
	// Transient, nil between intents.
	answerParked *resumePoint
	// askCount counts the mid-resolution asks Engine.Ask took, posed or
	// deferred (effects' askCounter seam). Transient scratch, never logged.
	askCount uint64
	// lastDeferred is the resume point of the most recent DEFERRED ask of the
	// running pass (nil once a posed ask follows it), so SuspendUnless marks
	// the ask that was actually just taken. Transient scratch.
	lastDeferred *resumePoint
	// resolvingObj is the stack object whose resolution is running (resolveTop
	// or a resumed resolution), kept through its final move off the stack so
	// an entry replacement that asks can tell whether it interrupted that
	// resolution's own move. Zero outside a resolution.
	resolvingObj state.ObjID
	// repeatReported is the RepeatEach SA whose loop frame SuspendRepeat
	// just recorded, so the enclosing Resolve loop's report of the same SA
	// is not recorded a second time as a plain continuation.
	repeatReported *cards.SA

	// cast holds the in-progress cast-flow state while choosing ==
	// chooseCast (Task 9, rules/cast.go). Nil whenever no cast is mid-flow.
	cast *pendingCast
	// costCompositionEvent is the one PutOnStack event excluded from
	// cast-count statics while the current cast's cost modifiers are composed
	// after PutOnStack. Stored as event index + 1 (zero means none), so an
	// earlier cast of the same object remains visible.
	costCompositionEvent int
	// turnUp holds the CR 708.6 morph-family turn-face-up special action's
	// payment flow while choosing == chooseTurnUp (rules/morph_turnup.go).
	// Nil whenever no turn-up is mid-payment. A plain-value struct with no
	// closures, so Clone copies it like cast/choosing and a replay
	// re-derives it from the recorded intents.
	turnUp *turnUpPay
	// etbMove parks a battlefield entry while its as-enters choice is answered
	// through the mid-resolution decision path. etbNext is the ordinal of the
	// next choice on that entry; both are plain data so a clone at the decision
	// boundary preserves the entry exactly.
	etbMove *events.Event
	etbNext int
	// turnUpMove parks the events.TurnFaceUp marker of a morph-family
	// turn-up while an "as this is turned face up" replacement body's own
	// answer is outstanding (task cli-20260924T031747Z-6d0658fc): the
	// transition must not fold until the body finishes, or the body and the
	// turn-up's triggers would observe a face the transition already
	// revealed. Unlike etbMove the park is consumed synchronously inside
	// resolveReplacementBody -- before any decision can be answered -- so it
	// need not survive a clone; the re-emit frame it installs on the body's
	// suspension chain is what crosses the decision boundary.
	turnUpMove *events.Event
	// causePin is the action cause an entry-settle preview pins while its
	// cloned stack no longer holds the entrant (rules/entry_counters.go).
	// Zero on every live engine.
	causePin state.ObjID
	// etbLandPlay identifies the land whose LandPlayed event must wait for its
	// final battlefield entry. A replacement can suspend and later re-emit the
	// move, so the object id is needed to avoid consuming this continuation on
	// a different move the replacement body emits first.
	etbLandPlay   bool
	etbLandObj    state.ObjID
	etbLandPlayer state.PlayerID
	// riotMove parks a non-cast battlefield entry while its controller makes
	// Riot's as-enters choice. The event is emitted only after Choose records
	// the answer, so every entry path reaches events.Move with RiotChoice set.
	riotMove *events.Event
	// siegeMove parks a non-cast Battle entry while its controller makes the
	// unleashMove parks a non-cast battlefield entry while its controller
	// makes Unleash's as-enters choice (CR 702.86, rules/unleash.go). Same
	// discipline as riotMove: the MoveZone is emitted only after the Choose
	// "unleash" event records the answer, so every entry path reaches
	// events.Move with UnleashChoice set. Clone-copied (clone.go).
	unleashMove *events.Event
	// CR 310.10 Siege protector choice. Same discipline as riotMove: the
	// MoveZone is emitted only after the Choose "protector" event records the
	// answer, so every entry path records the protector beside the entry and a
	// log-only replay re-derives it. Clone-copied (clone.go).
	siegeMove *events.Event
	// entryStageDone holds an entry stage (rules/entry_counters.go) whose
	// characteristic-counter order competition has fully resolved and whose
	// move is being re-emitted for its fold: the fold consumes the finalized
	// amounts from it. Set immediately before the completion re-emit,
	// consumed by the very fold that emit reaches -- the same set-then-
	// consume window the parked-move fields use. Clone-copied (clone.go).
	entryStageDone *entryCounterStage
	// attachedChoice parks an Attach event while an Attached replacement asks
	// for its name and creature type.
	attachedChoice   *attachedChoice
	attachedApplying bool
	// tokenChoice parks a CreateToken replacement plan while the chosen-copy
	// body (Type$ ReplaceToken | TokenScript$ Chosen -- Esix, Moonlit
	// Meditation, Mirrormind Crown) asks its controller which creature to
	// copy. Same discipline as siegeMove/attachedChoice: the plan's mints are
	// emitted only after the answered election is applied, so the log's
	// CopyToken events carry the choice and a log-only replay re-derives the
	// mints. Clone-copied (clone.go).
	tokenChoice *tokenChoiceState
	// specEnvs/specEnvDepth: targetSpecContext's reusable Resolve records,
	// used as a stack (trigger_referents.go, acquireSpecEnv). Scratch that is
	// free at every intent boundary, so Clone starts a fresh one.
	specEnvs     []*specResolveEnv
	specEnvDepth int
}
