package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// engineCastWindows groups the Engine's cast payment, drain-await and
// mana-spend capture state. It is embedded by value in Engine
// (rules/engine_struct.go), so every field keeps its documented contract
// comment and every existing e.<field> access keeps compiling unchanged
// through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineCastWindows struct {
	// Synchronous Scry proposal's continuation identity (never carried across
	// a decision: the parked resume point owns its SA and target).
	scrySA     *cards.SA
	scryTarget int
	// untapResume is set only around one Untap emission from finishUntapStep.
	// If that event parks an Untap replacement choice, it moves into the queue.
	untapResume *untapStep
	// untapChoiceObj is the permanent whose permanent-specific untap-step
	// election is pending. The answer is folded onto the object before this
	// cursor resumes, so clones and replay preserve the same choice.
	untapChoiceObj state.ObjID
	// madnessChoices parks discard moves while the card's owner decides whether
	// to apply Madness's optional hand-to-exile replacement.
	madnessChoices []events.Event
	// madnessSuspended marks that the FRONT madness ask was posed through
	// Engine.Ask and so suspended the stack resolution whose discard it
	// interrupted (e.resume is that suspension's frame). The last answer of
	// the queue resumes it. A bool rather than the frame pointer so a clone,
	// whose resume chain is deep-copied, still resumes its own frame.
	madnessSuspended bool
	// applyingMadnessChoice suppresses only the Madness interposition while an
	// answered choice emits its selected destination.
	applyingMadnessChoice bool

	// suppressedCast holds the card object ids whose cast option is held out
	// of the current priority window because their cast attempt aborted
	// unpayable with no state change (E2 round 2, tightened by F05-2/CR
	// 733.2). This is the no-progress answer for a hash-chained, replayable
	// engine: instead of counting no-progress aborts and killing the match,
	// suppress the exact card that produced one, so the re-offer loop cannot
	// begin an unbounded second iteration -- nobody's match dies, and the
	// seat may still do anything else. F05-2 (CR 733.2) lets a reversed
	// illegal action be redone legally, so suppression engages only on the
	// SECOND identical no-progress abort of the same card in the same window
	// (the per-card castAborts count), never the first. Cleared on any
	// genuinely state-changing event (see emit), so a declined card's option
	// comes back the moment the window ends or the mana/board changes. The
	// id already names the one seat that holds it, so two different cards'
	// declines never interact and two seats' never do either.
	suppressedCast map[state.ObjID]bool

	// castAborts counts the no-progress cast/activation aborts per card so far
	// in the current priority window (F05-2, CR 733.2): the held-out
	// suppression above engages only on the SECOND identical no-progress abort
	// of the same card, so a merely-reversed illegal action (CR 733.2) may be
	// redone legally once -- the first abort leaves the option offered -- while
	// a deliberate repeat still cannot spin the engine. It is cleared by the
	// same state-changing-event rule that clears suppressedCast, so the count
	// and the held-out set have identical lifetimes. Like suppressedCast it is
	// transient window bookkeeping on the Engine, never an event or a
	// state.Game field, so a replay re-derives it by re-running the same
	// aborts rather than reading it from the log.
	castAborts map[state.ObjID]int32

	// inertHeldOut holds the priority options the inert backstop
	// (rules/priority_guard.go) caught changing nothing: each is left out of
	// the re-offer until the next state-changing event, exactly the
	// suppressedCast lifetime (cleared beside it in emit). Transient window
	// bookkeeping like suppressedCast: a replay re-derives it by re-running
	// the same inert answer, whose Note is in the log.
	inertHeldOut map[inertKey]bool

	// drainAwaitsTarget is true while a decision asked from inside the trigger
	// drain is pending, so its answer resumes the drain rather than granting
	// priority. Task 7 sets it for a TargetMin/TargetMax-bearing triggered
	// ability's own KTarget decision (pushTrigger, cleared by handleTarget);
	// Task 18 sets it for a Miracle cast's own X/Delve/Sac decision
	// (castMiracle, cleared by handleChoose once the cast commits). When set,
	// the handler for the pending decision calls resumeTriggerDrain (the same
	// continuation handleTriggerOrder uses) instead of granting the caster
	// priority, so a later, unrelated trigger in the same batch is still
	// placed before any player acts. Plain scalar, so Clone copies it like
	// every other field here. (Tasks 7, 18.)
	drainAwaitsTarget bool

	// drainAwaitsModes is the CR 603.3c twin of drainAwaitsTarget: true while
	// a modal triggered ability's KModes decision asked at placement
	// (pushTrigger) is pending, so its answer records the chosen modes onto
	// the stack object and resumes the drain (handleModes) rather than
	// granting priority. Plain scalar, Clone copies it, and a replay re-derives
	// the same branch from the same recorded answer. It is set only when the
	// ask actually posed a decision (askTriggerModes can return true without
	// asking -- a ChoiceRestriction$ that has exhausted every eligible mode,
	// or a CharmNum$ above an unrepeatable mode count -- and a stale true
	// would misroute the next unrelated KModes ask through the placement
	// branch), matching the invariant its name states.
	drainAwaitsModes bool

	// deferCastTrigger is set only around the up-front cast push (CR 601.2a)
	// emit in pushCast. While it is true, emit HOLDS the PutOnStack event's
	// cast trigger back instead of running checkTriggers for it, because the
	// spell is not yet actually cast: the "when you cast" trigger (601.2i)
	// fires only after the target choice (601.2c) and payment (601.2h). The
	// held event is stored in deferredPush and re-checked by payCast's
	// fireDeferredCastTrigger. A bool (not a count) is safe because emit is
	// single-threaded and the push's Apply/transformation path never re-enters
	// emit for another PutOnStack; a replacement that fires here (as for any
	// PutOnStack) recurses on the OTHER kind, which falls to checkTriggers
	// normally. Zero whenever no cast push is in flight, so Clone copies it.
	deferCastTrigger bool

	// deferredPush holds the up-front PutOnStack event of an in-flight cast
	// whose cast trigger (CR 601.2i) is held back until the cast is complete
	// (see deferCastTrigger). Zero when nothing is deferred. payCast's
	// fireDeferredCastTrigger re-walks it once the spell is paid for; an
	// aborted proposal drops it. Each is a pointer so a Clone taken with a
	// cast in flight copies the held event (a replay re-derives the same
	// trigger from the recorded PutOnStack).
	deferredPush *events.Event

	// deferredPushLKI is the LKI snapshot captured for deferredPush's own
	// Obj when pushCast emitted it, threaded into the trigger walk so a
	// ChangesZone trigger fired by the cast (see spellCastMatches) can read
	// the card as it was just before the stack move.
	deferredPushLKI *state.Object

	// noCounterSpend is the transient capture of emitRestrictedManaSpend: the
	// id of the SPELL whose payment just consumed a batch carrying
	// AddsNoCounter$ provenance (Cavern of Souls' "that spell can't be
	// countered"), zero when none. payManaCast's caller (payCast) reads it
	// once, synchronously, right after the payment — no ask can suspend
	// between the spend and the read (emitRestrictedManaSpend emits, never
	// asks) — and folds state.FlagNoCounter into the pay-time CastInfo, so
	// replay re-derives the flag from the recorded event exactly like every
	// other cast flag. Zero whenever no such spend is in flight, so Clone
	// copies nothing of it.
	noCounterSpend state.ObjID

	// manaSpentSources is the transient capture of emitRestrictedManaSpend's
	// SPELL arm: the deduplicated Source of every restriction batch consumed
	// by the payment, in insertion order. payCast reads it once, synchronously,
	// right after the payment and queues each source's TriggersWhenSpent$
	// rider (Path of Ancestry's "when that mana is spent to cast ..."). Empty
	// Valid provenance batches -- the Boseiju shape effMana emits for a rider'd
	// mana ability -- are what make the attribution exact: emitRestrictedManaSpend
	// consumes batches before ordinary mana. Nothing can suspend between the
	// capture and the read (it emits, never asks), and Clone copies nothing of
	// it (like noCounterSpend), so a replay re-derives the same list from the
	// recorded ManaAdd events.
	manaSpentSources []state.ObjID

	// manaSpentAddsCounters is the transient capture of emitRestrictedManaSpend's
	// SPELL/ACTIVATED arm for the AddsCounters$ rider: every consumed
	// restriction batch that carries a rider (state.ManaRestriction.AddsCounters,
	// the producing ability's snapshot) contributes its spent unit count as one
	// grant record, in insertion order. Unlike manaSpentSources this is NOT
	// deduplicated by source: two units from the same permanent's rider ability
	// are two grants, and two different abilities of the same permanent keep
	// their own rider snapshots. payCast reads it once, synchronously, right
	// after the payment. Nothing can suspend between the capture and the read
	// (it emits, never asks), and Clone copies nothing of it, so a replay
	// re-derives the same grants from the recorded ManaAdd/ManaRestriction
	// events.
	manaSpentAddsCounters []state.ManaAddsCounterGrant

	// stackGrantCast is the in-flight cast whose OWN stack-grant walk is
	// running (queueCascadeTriggers' cascadeInstances read, the only
	// consumer): set around that one walk and cleared before it returns —
	// never set at rest, so Clone copies nothing of it and no ask can
	// suspend inside the walk (cascadeInstances is a pure derived read).
	// While it is set, SpellsCastThisTurnMatching excludes the in-flight
	// cast's own event from every count, so the "first spell you cast each
	// turn" statics' EQ0 gates (the twelve AffectedZone$ Stack SVarCompare$
	// lines in the corpus — Rain of Riches, Wild-Magic Sorcerer, Anhelo,
	// the Doctor Who cycle) read the PRIOR casts the Affected$ half does
	// not evaluate, instead of never granting (the in-flight cast's own
	// PutOnStack is already in the log at queue time and an inclusive read
	// would make EQ0 fail for the very cast the grant is for). Counts read
	// anywhere else stay inclusive (Vengevine's EQ2 "second creature
	// spell" gate).
	stackGrantCast state.ObjID

	// manaExpended is the per-seat, per-turn tally of mana spent CASTING
	// spells this turn (trig:ManaExpend's "as you spend your Nth total mana
	// to cast spells during a turn"). It is engine scratch, NOT event state,
	// because it must count EVERY cast of the turn -- including casts made
	// before a ManaExpend carrier entered the battlefield, which emit no
	// FlagManaExpendCast event (the emission gate keeps games without a
	// carrier byte-identical, heads safety). payCast updates it
	// unconditionally on every paid cast; manaExpendMatches reads it for the
	// crossing test. manaExpendedTurn is the e.G.Turn the slice belongs to:
	// payCast zeroes the slice and re-stamps when the turn has moved on (the
	// tally is rebuilt by replay's payCast re-execution in the same order, so
	// it is deterministic), and Clone copies both so an intent-boundary clone
	// resumes mid-turn with the original's tally. The window is any turn, not
	// "your turn": an instant cast on an opponent's turn accumulates too.
	manaExpended     []int32
	manaExpendedTurn int32
}
