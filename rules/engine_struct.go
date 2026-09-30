package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type Engine struct {
	G                *state.Game
	deckManifests    []deck.Manifest
	endTurnRequested bool
	L                *events.Log
	compiledText     *compiledText
	landTypeWords    []string

	// ManaAbilityHook, when non-nil, is called once per mana ability
	// activation the engine resolves (resolveManaAbilityRefOriginal, the one
	// choke point every activation path -- the priority "activate" option,
	// the cast payment window, the unless-cost and attack/block-cost windows
	// -- funnels through), after the activation is judged payable and before
	// its cost and effect are applied, and once per triggered mana ability
	// (CR 605.1b, a Static$ True TapsForMana trigger) the batch after it
	// resolves off the stack (resolveTriggeredManaAbilities). sa is the
	// ability's compiled identity: the printed Face().Abilities pointer
	// (never the colour-pinned copy a Combo pick resolves through), the
	// foreign card's pointer for a gained ability, or the printed
	// Trigger.Effect body for a triggered one. It is a harness-only
	// OBSERVER (cmd/cardfuzz credits mana-ability use with it, because a
	// mana ability never uses the stack and ManaAdd carries no source): it
	// emits nothing, mutates nothing, is
	// not copied by Clone, and a nil hook -- every host, replay and test --
	// leaves the event stream and every chain head byte-identical.
	ManaAbilityHook func(p state.PlayerID, source state.ObjID, sa *cards.SA)

	// paymentStats is the optional auto-pay diagnostics sink
	// (SetPaymentPlanStats, rules/payment_plan_stats.go). Like
	// ManaAbilityHook it is a harness-only observer: nil by default, it emits
	// nothing, never changes an offer, and Clone deliberately does not copy
	// it (spec §7: no pointer is shared across engines).
	paymentStats *PaymentPlanStats

	// ascend is checkBlessingGrants' incremental "could anything carry
	// Ascend" arena scan (rules/ascend.go); a pure cache, zero = rescan.
	ascend ascendScan

	// storied is checkEnduringStoryGrants' incremental "could anything carry
	// Storied" arena scan (rules/storied.go); a pure cache, zero = rescan.
	storied storiedScan

	// turnsTaken caches the TurnChange census used by Count$TurnsThisGame.
	// turnsTakenEpoch is the log length represented by the cache; emit advances
	// both together, while an Engine assembled around an existing log lazily
	// rebuilds on its first query.
	turnsTaken      []int32
	turnsTakenEpoch int

	// turnStartTurns caches, per player, the sorted turn numbers at which that
	// player's turn began (rules.turnStartsFor). delayedRegistrationLive reads
	// it for the next-turn lifetime boundary; turnStartEpoch is the log length
	// it represents, so an Engine assembled around an existing log lazily
	// rebuilds it once rather than rescanning the log per registration. Clone
	// copies it like turnsTaken.
	turnStartTurns [][]int32
	turnStartEpoch int

	// combatHitsThisTurn is the per-turn combat-damage-to-players ledger
	// captured at the combat-damage site (rules/combat.go's
	// runCombatAssignments). It is engine-side, NO-EVENT state -- deliberately
	// not a new events.Kind, which would move every chain head and diverge
	// every STORED log at its first combat assignment. Every rebuild path
	// (replay, undo, DVR, restart) re-executes the engine and so re-derives
	// the same slice, and emit clears it on TurnChange (the turnsTaken
	// cache-advance site below). It carries only damage that LANDED and only
	// damage to a PLAYER; the object branch of runCombatAssignments records
	// nothing. See effects.Host's CombatDamageToPlayersThisTurn.
	combatHitsThisTurn  []effects.CombatDamageHit
	counterAddsThisTurn []counterAddedThisTurn
	// activationsThisTurn and crimeSeatsThisTurn are two more per-turn
	// NO-EVENT ledgers of the same kind (rules/turn_ledgers.go): this turn's
	// activated-ability stack objects with the targets they chose, and the
	// seats that committed a crime (CR 700.13). Re-derived by every rebuild,
	// cleared on TurnChange, copied by Clone.
	activationsThisTurn []activationThisTurn
	crimeSeatsThisTurn  uint64
	bendSeatsThisTurn   [64]uint8

	// format is the construction format New was configured with (Config.
	// Format). It is the explicit gate the Commander rules (the tax, CR
	// 903.9, commander damage) check -- "in a non-Commander game none of
	// this runs at all" -- rather than inferring the format from the
	// incidental shape of the zones. It is read long after New returns: at
	// cast time for the CR 903.8 tax, at combat-damage time and in the
	// state-based-action pass for CR 903.10. Plain Format value; Clone
	// copies it so a cloned Commander engine still gates its command-zone
	// rules and keeps its commander-damage loss condition.
	format Format

	rng     *rng
	pending *decision.Decision

	// deferGameOver is true only while New processes the opening deal. A
	// library-empty draw still emits PlayerLost and runs every other SBA, but
	// checkGameOver waits until New has recorded the CR 103.1 toss. That keeps
	// GameOver as the final event of a terminal genesis burst (the host's
	// persisted-boundary contract) and lets an all-undersized opening deal
	// reach the truthful no-survivor draw instead of accidentally crowning an
	// undealt short deck.
	deferGameOver bool

	// continuous holds every registered continuous effect, live or expired.
	// The layer system (layers.go) is the only reader and writer.
	continuous   []ContinuousEffect
	lifeExchange *lifeExchangeTransaction
	// pendingLifeExchange parks an exchange transaction whose first or second
	// life change suspended on a decision that is NOT a replacement-order ask
	// (a consumed GainLife→Draw body that itself parked a Dredge ask). No
	// static caller references the transaction once the synchronous emit
	// returns, so without this slot it is orphaned and NEITHER side ever
	// applies. settlePendingLifeExchange re-drives it from every engine-idle
	// drain, and finishLifeExchange re-parks it whenever it suspends again.
	// It is only ever non-nil while a decision or a replacement-order queue is
	// outstanding, so (like resume/replChoices/pending) no clone boundary can
	// observe it.
	pendingLifeExchange *lifeExchangeTransaction
	// controlGrants holds the GainControl effects that can still end (see
	// rules/control.go). It is engine continuation state only; every take and
	// return is a ControlChange event, so the log alone rebuilds Game state.
	controlGrants []controlGrant
	// expiringControl guards expireControl against re-entry through the
	// ControlChange events it emits.
	expiringControl bool
	// reconcilingControlStatics guards reconcileControlStatics (rules/
	// control_static.go) against re-entry through the ControlChange events
	// IT emits; the same intent-boundary discipline as expiringControl.
	reconcilingControlStatics bool

	// mulligans is Config.Mulligans carried past genesis: the colour round's
	// end (rules/commander_color.go) must re-enter the same mulligan/opening
	// hand-off the genesis branch would have taken, and cfg is not otherwise
	// retained. Plain int, so Clone copies it.
	mulligans int
	// windowDiagnostics is the default-off, observer-only priority sidecar
	// gate copied from genesis Config.
	windowDiagnostics bool
	// startingLife is Config.StartingLife with the 0-means-20 convention
	// already resolved at genesis — the value state.NewGameLife opened the
	// game with. It is the effects.Host StartingLife backing (the
	// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife read
	// behind Anya, Merciless Angel's and Game Over's relative
	// half-starting-life thresholds). Plain int32, so Clone copies it.
	startingLife int32
	// pregame is true while the London mulligan round runs, between the
	// opening deal and turn 1. startPostDealSetup sets it when
	// Config.Mulligans > 0 (at New for a plain constructor; for the
	// CR 103.1 choice constructor at the choice's resolution -- see
	// tossChoice); step() dispatches to stepPregame (rules/mulligan.go)
	// while it is true, and the round's end clears it and hands to
	// beginTurn. Bool field, so Clone copies it like every other value field.
	pregame bool
	// coloring is true while the CR 903.4b commander colour-choice round runs,
	// BEFORE the London mulligan round (the choice is made "before the game
	// begins", and the mulligan round is also pregame). New sets it only when
	// a seat's commander carries the characteristic-defining chosen-colour
	// static; step() dispatches to stepColorRound (rules/commander_color.go)
	// while it is true, and the round's end opens the mulligan/opening round
	// exactly as if the colour round were absent. Bool field, so Clone copies
	// it like pregame does.
	coloring bool
	// colorRound is the colour round's plain-value state (rules/
	// commander_color.go): one qualifying (seat, commander) ask per entry and
	// a cursor. Never a closure, so Clone copies it like the mulligan round.
	colorRound colorRound
	// tossChoice is CR 103.1's second half's plain-value state (rules/
	// starting_player_choice.go): the toss winner may still choose who takes
	// the first turn. Only a tossAsk constructor (NewStartingPlayerChoice)
	// sets it; plain New folds the resolved toss and runs startPostDealSetup
	// exactly as the pre-choice engine did. active marks that the pregame
	// rounds are still deferred until the choice is answered or defaulted.
	// Never a closure, so Clone copies it.
	tossChoice tossChoice
	// mulligan is the round's plain-value state (rules/mulligan.go) -- seats,
	// kept/taken counts and the phase cursor. Never a closure, so Clone copies
	// it like cast/choosing.
	mulligan mulliganRound
	// opening is the optional opening-hand effects round, after the London
	// mulligan round (a Gemstone Caverns may not be used from a hand its owner
	// later mulliganed away) and before turn one. It holds only object IDs and parsed SVar names, so replay and
	// Clone reproduce the same pregame choices without ambient state.
	// Impatient Iguana's accepted BecomeStartingPlayer$ Reveal resolves here
	// and folds the designation into state.Game through events.StartingPlayer
	// Change (effects/cardflow.go), so Count$StartingPlayer and the view's
	// pregame projection read it before turn one.
	opening openingRound
	// blockerRound is the declare-blockers step's per-defender cursor
	// (rules/combat.go, Task m34): an attack may be split across several
	// defending players, and each declares its own blocks, one KBlockers
	// decision at a time. Plain-value state (a defender list plus an index),
	// never a closure, so Clone copies it like the mulligan round.
	blockerRound blockerRound

	// exertAskState is the declare-attackers exert election's resumable
	// state (rules/combat.go, task exert1): the deterministic offer list
	// (attacking creatures carrying an offerable stat:OptionalAttackCost
	// static, in declaration option order) plus the cursor of the ask
	// currently outstanding. Plain-value state, so Clone copies it like
	// blockerRound; a log-driven replay re-derives the same list when it
	// re-runs the recorded KAttackers answer through handleAttackers.
	exertAskState exertAsk

	// enlistAskState is the declare-attackers enlist election's resumable
	// state (rules/enlist.go, task enlist1): the answered KAttackers
	// declaration, the declaring player, the deterministic offer list
	// (attacking creatures with `K:Enlist` that have at least one eligible
	// creature to tap, in declaration option order) plus the cursor of the
	// ask currently outstanding. Plain value, so Clone copies it like
	// exertAskState; a log-driven replay re-derives the same list when it
	// re-runs the recorded KAttackers answer through handleAttackers.
	enlistAskState enlistAsk

	// stationing is the spacecraft a pending Station tap pick (rules/
	// station.go) belongs to: the "station" priority option's object, held
	// across the KChoose so the answer's charge counters land on the right
	// permanent. Plain value, so Clone copies it like blockerRound; zero
	// whenever no station ask is outstanding.
	stationing state.ObjID

	// combatRound is the combat damage step's continuation state
	// (rules/combat.go, Task jj-cmb): which damage passes are done, and any
	// controller damage-division choices still being collected or awaiting an
	// answer (CR 510.1c multi-block division, CR 510.3/4 double-strike).
	// Plain-value data (slices plus scalars), never a closure, so Clone
	// copies it like blockerRound and a log-driven replay re-derives the same
	// branch. Zero whenever the combat damage step is not in progress.
	combatRound combatRound

	// staticContinuous memoizes the S:Mode$ Continuous statics on battlefield
	// permanents (layers.go's staticEffects), keyed on staticEpoch. staticEpoch
	// is the log length at the last build; the memo refreshes once per emitted
	// event rather than once per Derived() call, because most events
	// (Priority/Damage/Mana) leave the battlefield permanent set untouched
	// while Derived is the hottest path in a turn (every legal action, combat
	// step and trigger predicate reads it). Clone() leaves both fields zero, so
	// a cloned engine rebuilds the memo on its first Derived -- staticEffects
	// is a pure function of the current board, so the rebuilt result is
	// identical and deterministic. Rebuilds reuse the outer slice's capacity,
	// clearing obsolete slots when it shrinks, but never reuse the nested
	// keyword/type slices. activeBuf copies the effect values into distinct
	// storage before sorting; neither buffer may alias a clone's scratch.
	staticContinuous []ContinuousEffect
	staticEpoch      int
	// staticVersion/staticObjs are continuousVersion and len(e.G.Objs) at the
	// last staticEffects build: layerInertSince's reuse across a run of
	// layer-inert events (layercache.go) additionally requires both unchanged.
	staticVersion int
	staticObjs    int
	// staticMemoGated records whether the last full staticEffects build
	// encountered any Continuous static carrying a continuousGateKeys param
	// (IsPresent$/IsPresent2$/Condition$/CheckSVar$/ClassBand$), whether or
	// not the gate currently passes. A gate that passes now can be flipped
	// off by a later battlefield-composition change (a token entering), so
	// the re-stamp admission in layercache.go's staticSafeSince must be
	// refused whenever this is true: only a gate-free build's output is
	// invariant under a static-cold token entry. Reset at the top of each
	// full staticEffectsWalk and set at the one gate site.
	staticMemoGated bool
	// staticBuildSeq counts staticEffects REBUILDS (never a layer-inert
	// re-stamp or an exact hit). The memo is refreshable OUTSIDE active() --
	// staticControlWants (control_static.go) calls refreshStaticContinuous
	// directly -- so the sorted activeBuf can be left describing an older
	// static list at the same log head and continuousVersion. activeStaticSeq
	// records the value active() built its buffer with, and both of active()'s
	// hit paths require the pair to match, exactly as the Derived memo keys on
	// activeBuildSeq. Never cloned: a clone's zero value rebuilds both.
	staticBuildSeq  uint64
	activeStaticSeq uint64

	// sbaQuiet is the state-based-action quiet key (rules/sbaquiet.go): the
	// board at which the last checkStateBased pass loop applied nothing.
	// sbaUnquiet is that loop's scratch flag for a no-op that depended on a
	// non-event input. Clone() leaves both zero, so a clone never skips its
	// first pass loop.
	sbaQuiet   sbaQuietKey
	sbaUnquiet bool

	// staticQueueBuf is staticEffects' AddStaticAbility$ work queue's reused
	// backing array: truncated to zero at every scan, grown only when a
	// static-grant fires (the warm-rescan allocation budget,
	// static_effects_buffer_test, is why it is reused rather than re-made).
	// Per-scan scratch, never cloned: a clone starts nil and grows its own.
	staticQueueBuf []staticWork

	// activeBuf is the cached, fully CR-613-sorted result of layers.go's
	// active(), the effect list every Derived() call ranges over for every
	// object of every board build and projection. Rebuilding that sorted list
	// once per emitted event instead of once per Derived() call is the whole
	// saving here -- active() used to allocate a fresh slice per call, and
	// Derived is the hottest path in a turn. The cache key is the pair
	// (activeEpoch, activeVersion): activeEpoch is the log length at the last
	// build and activeVersion the continuousVersion (bumped by AddContinuous
	// and EndOfTurnCleanup), because the effect list is a pure function of the
	// current board plus e.continuous, and those are exactly the two inputs
	// the key captures -- every board change moves the log head (emit), and
	// e.continuous changes through exactly the two mutators above. activeDepth
	// is a re-entry guard (Task A2's forEachObject pattern): it lets a nested
	// Derived (HasKeyword inside a MatchesSpecFrom) atomically share the
	// cached list and, on the never-happens-in-practice rebuild-mid-range
	// path, build a private list instead of clobbering the outer call's.
	// Clone() copies none of these fields (see clone.go); a cloned engine
	// starts with a zero key and rebuilds identically on its first Derived.
	activeBuf []ContinuousEffect
	// activeKWHeads is the deduplicated KeywordHead of every AddKeywords
	// entry across activeBuf, rebuilt with it (layers.go's active()) and read
	// by keywordmay.go's exact Derived-keyword precheck. Never cloned, like
	// activeBuf: a clone's zero key rebuilds both together.
	activeKWHeads []string
	// activeBuildSeq counts active()'s REBUILDS (never its exact or
	// layer-inert hits). derivedmemo.go's cross-walk reuse keys on it: an
	// unchanged count means no non-inert event, continuous-registry write,
	// object-count change or explicit invalidation has reached active() since.
	// Never cloned: a clone starts at zero with an empty memo.
	activeBuildSeq uint64
	activeEpoch    int
	activeVersion  int
	activeDepth    int
	// activeObjs is len(e.G.Objs) at the last active() build, read only by
	// the layer-inert reuse (layercache.go).
	activeObjs int
	// goadProbe is the static-goad derivation's re-entry guard (staticgoad1):
	// staticallyGoaded matches each candidate's Affected$ spec through
	// matchesSpec, and a spec that itself consults the IsGoaded predicate
	// would derive the set again — an infinite walk. While the counter is
	// above zero the IsGoaded binding in matchesSpec stands down and the
	// predicate answers the event-backed half alone, so a (hypothetical)
	// IsGoaded-conditioned goad static degrades instead of looping. Never
	// cloned (clone.go copies none of the derivation caches).
	goadProbe int
	// renames is the layer-3 rename table (setname.go) the effects tier's
	// name filters read through SpecContext.EffectiveNames. It is refreshed
	// after each emitted event, under active()'s own (epoch, version) key,
	// and only when setNameInPool says this match has a SetName$ carrier at
	// all. It is a FIELD rather than a lazily-called derivation because
	// specCtxSVars must stay inlinable: a call there makes its Resolve
	// closure escape and allocates on every hot-path context construction.
	// Clone copies the table (the clone's board is identical at the clone
	// boundary) and the two key fields with it.
	renames        []effects.ObjectName
	renameEpoch    int
	renameVersion  int
	renameBuilding bool
	// derivedTypes is the layer-4 derived type table (layer4types.go) the
	// effects tier's ordinary type filters read through SpecContext.
	// DerivedTypes. Exactly the shape (and rationale) of renames above: a
	// field refreshed after each emitted event under active()'s key, gated on
	// layer4InPool, and bound by a plain field read so specCtxSVars stays
	// inlinable. Clone copies the table and its key fields.
	layer4Types   []effects.ObjectTypes
	typesEpoch    int
	typesVersion  int
	typesObjs     int
	typesBuilding bool
	// The incremental layer-4 state (layer4types.go). typesIncrReady is set
	// once a whole-board build has repopulated it; every refresh then first
	// tries refreshDerivedTypesIncremental, which folds the events logged
	// since the last build into typesMayDiffer (the candidate slice) and
	// re-derives only the self-only source list -- never a whole-board scan
	// (the staticsMayChangeTypes precheck has its own probe cache below).
	// Every fallback is toward the whole-board rebuild, never away from it.
	// Clone deliberately copies none of these: the clone's board is
	// identical at the boundary, so the carried table + key above stays
	// valid for key hits, and the clone's first real rebuild repopulates
	// the incremental state from scratch (the activeEpoch precedent).
	typesIncrReady bool
	typesSelfOnly  bool
	typesSrcs      []state.ObjID
	typesMayDiffer []state.ObjID
	typesTouch     []state.ObjID
	// typesAct is the reusable live-LType-effect buffer the incremental build
	// passes to typeCharacteristicsActive; typesVisited counts the objects the
	// last build examined (the whole board on a full rebuild, the candidate
	// set on an incremental one). The scaling pin reads typesVisited; the
	// engine never does.
	typesAct     []ContinuousEffect
	typesVisited int
	// The staticsMayChangeTypes probe cache: per-object probe answers with
	// the count of true ones, maintained by the same event-referent catch-up
	// (see layer4types.go).
	typesProbe        map[state.ObjID]bool
	typesProbeTrue    int
	typesProbeEpoch   int
	typesProbeVersion int
	typesProbeObjs    int
	// typesIncrBuilds counts successful incremental rebuilds; the scaling
	// tests read it to prove the incremental path (not the whole-board
	// fallback) served a refresh. Never read by the engine itself.
	typesIncrBuilds int
	// setNameInPool is a genesis-time fact: does any card this match can put
	// on the battlefield print a SetName$ static? False for almost every
	// match, which reduces the per-event refresh to one predictable branch.
	setNameInPool bool
	// layer4InPool is the same genesis-time fact for a layer-4 type-changing
	// effect (cards.ChangesTypes). False for most matches, which reduces the
	// per-event refresh to one predictable branch.
	layer4InPool bool
	// continuousVersion is bumped by every direct mutation of e.continuous
	// (layers.go's AddContinuous and EndOfTurnCleanup). It stands in for the
	// events a board change would signal through the log head: while
	// AddContinuous also emits a ClockTick, EndOfTurnCleanup rewrites
	// e.continuous in place with no event, and the active() cache must see
	// that drop (an UntilEOT pump expiring) even though the log head did not
	// move. A zero value is never taken as a valid cache hit across rebuilds
	// because active() guards hits on version as well.
	continuousVersion int

	// searchingBy tracks the library search in flight, for the Opposition
	// Agent class: repl:Moved's FoundSearchingLibrary$ matches only while a
	// search's own moves are being emitted, and the search-control static
	// redirects the search pick's decision. The depth counter keeps a nested
	// search's flag alive until the outer search leaves applyLibrarySearch.
	// Synchronous engine-runtime state (set and cleared around one
	// synchronous applyLibrarySearch), never folded from an event and never
	// read across a suspension.
	searchingBy state.PlayerID
	searchDepth int

	// loop is the livelock watcher (rules/livelock.go): pure observation of
	// the event stream this engine is logging, configured by Config.
	// LoopGuard. It reads nothing and is read by nothing else; it panics
	// with a *LivelockError when the stream looks non-terminating. Clone
	// copies the guard thresholds and resets the run/quiet state (a clone
	// only happens at an intent boundary, where the watcher is idle
	// anyway), so the clone and the original watch their own streams
	// independently.
	loop livelockWatcher

	// derivedKW / derivedTypes are Derived's scratch keyword and type buffers
	// (rules/layers.go): the full Derived(struct) build rewrites them in place
	// so repeated derived-characteristic reads do not allocate. They are pure
	// per-call scratch, rebuilt from the face and active() every call, so they
	// carry no cross-call state beyond capacity; Clone() copies none of them
	// (see clone.go), so a cloned engine grows its own — never aliasing the
	// original's mutable scratch, exactly the A2 buffer / C3 digest precedent.
	// derivedDepth is the re-entry guard for the reuse (the A2/active()
	// pattern): a nested Derived mid-build owns private buffers instead of
	// clobbering the outer build's.
	derivedKW    []string
	derivedTypes []string
	derivedDepth int
	// derivedPTFrames are the in-progress layer-7 snapshots exposed to
	// effects-side Count$Valid scans, including nested candidate derivations.
	derivedPTFrames []derivedPTSnapshot

	// derivedMemo / derivedMemoDepth / derivedMemoGen are Derived's per-object
	// memo for ONE legal-actions walk (rules/derivedmemo.go): derivedMemoDepth
	// is the scope counter legalActionsPriced raises, derivedMemoGen is bumped
	// on every outermost scope entry so no entry outlives the walk that built
	// it, and derivedMemo (keyed by ObjID) owns each cached result's slices.
	// Pure per-walk scratch: Clone copies none of it (a clone starts with an
	// empty memo and generation 0, which no entry ever matches).
	derivedMemo      derivedMemoTable
	derivedMemoStack derivedMemoTable
	derivedMemoDepth int
	derivedMemoGen   uint64
	// derivedMemoTail / derivedMemoAlias* carry the priority walk's memo
	// across the decision boundary into a BeginDerivedReads scope
	// (rules/derivedmemo.go). Validated on every use; Clone copies none.
	derivedMemoTail      derivedMemoTail
	derivedMemoAliasFrom int
	derivedMemoAliasTo   int
	// manaConvCache is a walk-scoped cache keyed like the Derived memo
	// (rules/walkcache.go). Pure per-walk scratch: Clone copies none of it.
	boardStaticsCache  boardStaticsCache
	activeStaticsCache []activeStaticsEntry
	mayPlaysCache      []mayPlaysEntry
	// paymentPlanQuery is the payment planner's per-query scratch (the
	// zone-entry index and source census, rules/payment_plan_search.go),
	// installed for one query and validated against the log on every read.
	// Pure per-query scratch: Clone copies none of it.
	paymentPlanQuery *paymentPlanQuery
	// paymentPlanRelaxed is PotentialPaymentPlans' transient proof mode
	// (rules/potential_plan.go paymentPlanRelaxProof): relaxed, never
	// executed alternatives for the mana abilities the planner census does
	// not price, appended to every search while it is set. Pure per-query
	// scratch: Clone copies none of it.
	paymentPlanRelaxed [][]plannedManaActivation
	// paymentPlanRelaxedFee is the generic the relaxed proof charges on top
	// of every planned cost for the paid relaxed abilities it admits.
	paymentPlanRelaxedFee int32
	// paymentPlanPotentialPool marks a PotentialPaymentPlans query
	// (paymentPlanPoolAccepted). Pure per-query scratch: Clone copies none.
	paymentPlanPotentialPool bool

	// derivingColorsSet/ID/Colors: the finished layer-5 colour answer for the
	// object whose Derived is mid-build (set by derivedWith before its layer-7
	// P/T walk, restored on the way out). Colors serves it to a layer-7 pump
	// expression that counts the object's own colours, instead of re-entering
	// Derived and recursing forever. Pure per-call scratch exactly like
	// derivedDepth — Clone copies none of it (clone.go's scratch precedent).
	derivingColorsSet bool
	derivingColorsID  state.ObjID
	derivingColors    string

	// pendingTriggers holds matched triggers not yet placed on the stack.
	// checkTriggers appends; putTriggersOnStack drains. Task 20 (trigger.go).
	pendingTriggers []pendingTrigger
	// secretVoteBallots is emission-scoped scratch, visible only while the
	// public, ballot-free completion Note is scanned for Vote triggers.
	// It is never stored on Game or in the event log.
	secretVoteBallots []effects.VoteBallot
	// triggerBefore is the immutable pre-departure board for an SBA death
	// batch. Scoped to its emission/resumption, never carried as live state.
	triggerBefore *triggerSnapshot
	// A shallow read-only observer of a recurring Effect trigger overrides
	// controllerOf for its creating source. The Effect's controller is the
	// registration's owner, even when its source card belongs to another seat.
	// Only the observer sets this; live Engine and Game state are unchanged.
	effectMatchSource     state.ObjID
	effectMatchController state.PlayerID
	effectMatchRemembered []state.Target
	effectMatchOverride   bool
	// lifeLossBatch holds the events in one simultaneous life-loss operation.
	// It is scoped to one synchronous effect/combat pass, so it is always nil
	// at an intent boundary and does not need log encoding or Clone state.
	lifeLossBatch          []events.Event
	lifeLossBatchDepth     int
	finishingLifeLossBatch bool
	// Per-stack-instance trigger provenance, derived while queuing/placing
	// triggers, cloned at intent boundaries and removed when the stack object
	// leaves. Never encoded in events or inferred from a resolving source.
	triggerContexts map[state.ObjID]effects.TriggerContext
	// triggerEffectFrames carries the source-scoped Effect frame an
	// Effect-created delayed trigger body resolves under, keyed by the stack
	// instance the trigger was placed into (the same key triggerContexts
	// uses). A non-static Effect trigger's body is minted by events.Apply's
	// DelayedPush from game state alone, so the frame the trigger queued with
	// must ride this scratch map to the resolution Ctx; the static fire arm
	// needs no map because it resolves the body inline. Resolution-scratch
	// like triggerContexts: never event-encoded, cloned at intent boundaries
	// and removed when the stack object leaves.
	triggerEffectFrames map[state.ObjID]effects.EffectFrame
	// triggerLines maps a stack object id to the granted/delayed trigger line
	// whose Execute$ body it resolves to. A granted (AddTrigger$) or delayed
	// (Effect Triggers$) body is an SVar-named *cards.SA, and cards.ResolveSVar
	// parses a FRESH pointer on every call -- so the pointer identity
	// findTriggerForAbilityFace uses for compiled Face.Triggers bodies can never
	// match one. This map carries the line from the push (which already records
	// triggerContexts) to resolution, so OptionalDecider$, the Cost$ window,
	// ResolvedLimit$, the intervening-if recheck and the label all see it.
	// Replay-derived exactly like triggerContexts: pushTrigger folds the same
	// lines in the same order. Appended to (not a redefinition of) the existing
	// map fields so a zero Engine stays valid.
	triggerLines map[state.ObjID]cards.Trigger
	// triggerLineSVars snapshots the owning script table of each recorded line.
	// The recipient's face is not necessarily the grantor's, and a grant can
	// disappear before the stack object resolves.
	triggerLineSVars map[state.ObjID]map[string]string
	// currentEffectFrame is the Effect-created continuous-effect registration
	// the effects.Resolve walk currently running belongs to. effects.Resolve
	// publishes it (through the optional effectFrameHost interface) for the
	// whole of a body walk and restores the enclosing value on exit, and
	// Ask captures it onto the resume point so a body that suspends on a
	// mid-resolution ask resumes still bound to its registration. It is
	// resolution-scratch like the trigger contexts -- never event-encoded, and
	// a replay re-derives it by re-running the same walk -- and it is zero
	// outside an Effect-created body, so every ordinary resolution is
	// unchanged.
	currentEffectFrame effects.EffectFrame
	// triggerLKI preserves the causing event's object snapshot from trigger
	// match through placement and resolution. TriggerPush can log Remembered
	// ids but not the pre-move object value (whose counters Move clears), so
	// this replay-derived map is the LKI analogue of triggerContexts.
	triggerLKI map[state.ObjID]triggerObjectLKI
	// sacrificedLKI maps a stack object id to the last-known-information
	// snapshot of every permanent that object sacrificed (as a cost), captured
	// at the instant of the sacrifice (Task sac1). It is engine-only, never
	// written to a state.Object, because a log-only state.Game reconstruction
	// (replayFromLog) rebuilds the stack object from the AbilityPush event
	// alone and would not reproduce an object field we set in cast.go -- the
	// same reason triggerContexts is engine-only. Resolution reads it and
	// builds effects.Ctx.Sacrificed; the entry is removed when the stack
	// object leaves, mirroring triggerContexts.
	sacrificedLKI map[state.ObjID][]state.SacrificedInfo
	// castExiled / castRevealed map a stack object id to the cards its own
	// cast/activation COST removed: the `ExileFromHand`/`ExileFromGrave`/
	// `Exile` parts (Forge's CostExile, paid-list key "Exiled") and the
	// `Reveal` parts (CostReveal, key "Revealed"), in stable cost order.
	// Engine-only scratch in the sacrificedLKI discipline: a log-only
	// reconstruction rebuilds it because payCast re-executes, cloned with the
	// engine at intent boundaries, read by the spell's own resolution Ctx
	// (effects.Ctx.Exiled/Revealed) so the `Exiled$<Property>` /
	// `Revealed$<Property>` count refs and `Defined$ Exiled`/`Revealed` read
	// the exact paid cards, and removed with the stack object. A stack COPY
	// inherits neither map -- referenced here is the deliberate reason the
	// StackCopy branch does not carry them, unlike fuseTargets: a copy was
	// never cast and paid no cost (CR 707.10).
	castExiled   map[state.ObjID][]state.ObjID
	castRevealed map[state.ObjID][]state.ObjID
	// fuseTargets maps a fused (FlagFused) stack object id to its two target
	// stages' own chosen targets (index 0 the front half's, index 1 the
	// alternate half's). Recorded by payCast at payment, read by resolveFused
	// (split.go) so each half resolves exactly the targets chosen FOR it:
	// re-deriving the split from the object's flat target list through each
	// half's ValidTgts spec mis-assigns any target a half's spec merely
	// overlaps (Turn // Burn's Creature vs Any). Engine-only scratch like
	// sacrificedLKI: rebuilt by replay because payCast re-executes, cloned
	// with the engine at intent boundaries, removed with the stack object.
	// StackCopy inherits this split alongside its flat targets when available;
	// resolveFused uses its spec fallback only for copies without provenance.
	fuseTargets map[state.ObjID][][]state.Target
	// copyTargetStage tracks the in-progress per-declaration copy-target
	// election (CR 707.10c), keyed on the copying stack object: the value is
	// the index of the NEXT declaration AskCopyTargets must ask. The
	// TargetsChosen fold clears Object.CopyMayChooseTarget after the FIRST
	// declaration's answer, so this scratch is what carries the election
	// across the stage that follows -- a fused copy's second half, exactly as
	// the cast's pendingCast.targetStage carries it for a cast. Engine-only
	// scratch like fuseTargets: rebuilt by replay (the ask re-executes on the
	// re-entered resolveTop), cloned with the engine, and removed with the
	// stack object so a later object reusing the id never reads a stale stage.
	copyTargetStage map[state.ObjID]int
	// copyAnswerTargets accumulates a multi-declaration copy-target election's
	// PER-DECLARATION answers until every declaration has been asked, at which
	// point the flattened list is recorded in one replace. Recording each
	// stage as it arrives would replace (or duplicate) the flat list mid-
	// election and lose a later declaration's inherited keep-current slots.
	// Engine-only scratch, rebuilt by replay, cloned with the engine, removed
	// with the stack object.
	copyAnswerTargets map[state.ObjID][][]decision.Option
	// castSubTargets carries a cast or activation's CAST-TIME pre-asked
	// SubAbility$ target answers (task alltargeted1), keyed by the stack
	// object that will resolve the chain and then by the sub SA's Line.
	// Forge asks every targeting SA in the chain BEFORE cost payment
	// (CR 601.2c); the engine pre-asks them in the cast flow (cast.go's
	// subTargetAsk) and installs the answers here at payment, so the
	// resolution consumes them (effects' chosenTargetsFor, through
	// Ctx.SubPreAsk) instead of re-posing the asks mid-resolution. An EMPTY
	// recorded set is a real answer (a Min-0 sub elected zero or had no
	// candidate at cast time) and still consumes its line. Engine-only
	// scratch in the fuseTargets discipline: rebuilt by replay because the
	// cast flow re-executes, cloned with the engine at intent boundaries,
	// removed when the stack object leaves the stack. A stack COPY of the
	// spell has no entry and falls back to the mid-resolution asking path.
	castSubTargets map[state.ObjID]map[string][]state.Target
	// charmTargets maps a modal stack object to the selected distinct modes'
	// target groups, in target-bearing mode order. It is engine scratch like
	// fuseTargets: the cast/placement target answer rebuilds it during replay.
	charmTargets map[state.ObjID][][]state.Target
	// fusedResolving is the target slice of the fused half whose resolution is
	// CURRENTLY running (rules/split.go's runFusedHalves), set around the
	// whole of that half's effects.Resolve -- the half's root SA and every
	// sub-ability in its chain -- and restored afterwards. fusedResolvingSet
	// is the presence bit: a half whose own ValidTgts$ produced an empty
	// slice is still a fused half whose sub-abilities must read that empty
	// list, never the stack object's flat one. Ask captures the pair onto the
	// pending resumePoint, so a mid-resolution ask posed by ANY frame of the
	// half (its root, a SubAbility$, a loop body) resumes with the half's own
	// targets rather than both halves' (Flesh // Blood's DBPutCounter reads
	// ParentTargeted$CardPower off this binding). Transient scratch, cleared
	// when the half's resolve returns: rebuilt identically by replay.
	fusedResolving    []state.Target
	fusedResolvingSet bool
	// fusedResolvingSVars is the SVar table of the fused half whose resolution
	// is currently running -- the ALTERNATE half's table when Blood is the
	// frame, never the object's front-face table. A fused spell keeps FaceIdx
	// 0, so o.Face().SVars is the FRONT half's table and a resumed alternate
	// half's sub reading its own SVar (Blood's NumDmg$ Y = Y:ParentTargeted$
	// CardPower) would resolve against the wrong table. Set and restored
	// alongside fusedResolving, captured by Ask onto the resumePoint. Nil
	// outside a fused half's resolution.
	fusedResolvingSVars map[string]string
	// resolvingTargetControllerLKI is the target-controller snapshot of the
	// Resolve chain whose effect is CURRENTLY running, published by
	// effects.Resolve through Host.SetResolutionTargetControllerLKI around
	// the whole chain and restored on return. Ask captures it onto the
	// pending resumePoint (Engine.Ask), so a resumed continuation -- which
	// rebuilds its Ctx from the already-reset live objects -- restores the
	// controller a target had at the start of resolution (a target destroyed
	// before a chained TokenOwner$ TargetedController resolves). Transient
	// scratch: rebuilt identically by replay, nil outside a chain.
	resolvingTargetControllerLKI map[state.ObjID]state.PlayerID
	// resolutionCtx is the live Ctx of the Resolve chain whose effect is
	// CURRENTLY running, published by effects.Resolve through the optional
	// resolutionCtxHost interface around the whole chain and restored on
	// return. It is the one home of the chain's in-flight TargetUnique$
	// accumulator: Engine.Ask reads resolutionCtx.TargetsUnique and stamps it
	// onto every decision whose own resume state did not carry it, so an
	// intervening ask of ANY kind (a modal election, a ward pay, a
	// dig/scry/arrange pick) preserves the picks earlier TargetUnique$ riders
	// chose at the resumed Ctx's rebuild. Transient scratch: rebuilt
	// identically by replay, nil outside a chain (combat, mulligan and other
	// non-resolution asks).
	resolutionCtx *effects.Ctx
	// resolvingFlipMemory is the coin-flip memory of the Resolve chain whose
	// effect is CURRENTLY running, published by effects.Resolve (and by
	// effFlipCoin when it lazily allocates the memory) through the optional
	// Host.SetResolutionFlipMemory seam and restored on return. Ask captures it
	// onto the pending resumePoint, so a resumed continuation re-attaches the
	// SAME pointer and a chained Defined$ FlippedTails / Wins reader keeps
	// every flip performed before the suspension. Transient scratch: rebuilt
	// identically by replay, nil outside a chain or before any flip.
	resolvingFlipMemory *effects.FlipMemory
	// resolvingExchangeMemory is the ExchangeLife rider memory of the Resolve
	// chain whose effect is CURRENTLY running, published by effects.Resolve
	// (and by effExchangeLife when it lazily allocates the memory) through
	// the optional Host.SetResolutionExchangeMemory seam and restored on
	// return. Ask captures it onto the pending resumePoint, so a resumed
	// continuation re-attaches the SAME pointer and a chained
	// Count$RememberedNumber reader keeps the value the exchange transaction
	// settled after the suspension. Transient scratch: rebuilt identically by
	// replay, nil outside a chain or before any exchange rider.
	resolvingExchangeMemory *effects.ExchangeMemory
	// villainousRemembered is the victim of the VillainousChoice whose chosen
	// body is CURRENTLY resolving, kept as ambient engine state for the
	// duration of that body's effects.Resolve — the fusedResolving pattern.
	// A nested ask the body poses captures it through Ask onto the pending
	// resumePoint (and buildContinuationChain stamps it onto the body's
	// continuation frames), so the nested ask's re-entry still resolves
	// Defined$ Remembered / Player.IsRemembered to the victim rather than
	// rebuilding the trigger's own capture. villainousRememberedSet is the
	// presence bit (a victim set is never empty, but the bit keeps the "no
	// villainous body in flight" case explicit). Transient scratch,
	// restored with the same defer discipline as fusedResolving; rebuilt
	// identically by replay.
	villainousRemembered    []state.Target
	villainousRememberedSet bool
	// windowPaidX is the X the triggered-cost window's payment announced
	// (rules/cumulative.go's X fold, tc.xPaid at the pay arm), kept as AMBIENT
	// engine state while the paid body resolves — the fusedResolving pattern:
	// rules/resolution.go's resumeResolution arms it from the frame's
	// rp.winPaidX around the re-entry's effects.Resolve, Ask captures it onto
	// every pending resumePoint it poses, and buildContinuationChain stamps it
	// onto the continuation frames — so a body that suspends on a
	// mid-resolution ask (Leyline Tyrant's "pay any amount of {R}" death
	// trigger, whose DB$ DealDamage target pick is exactly such an ask)
	// resumes with its X instead of rebuilding ctx.X from a trigger object
	// that was never paid one (0). Transient scratch, restored with the same
	// defer discipline as fusedResolving; rebuilt identically by replay.
	windowPaidX int32
	// exploitedLKI maps an EXPLOITED creature's object id to the LKI snapshot
	// of it at the instant it was sacrificed to pay an exploit (CR 702.58a),
	// published by effects/exploit.go through Host.RememberExploitedLKI while
	// the resolving marker holds Ctx.Sacrificed. The events.Exploit marker
	// carries only the exploited id, and Move has already cleared the
	// creature's counters and battlefield layers by emit time, so this map is
	// what lets a trig:Exploited body read TriggeredExploited$CardPower/
	// CardToughness as last-known information (rules' attachExploitedLKI).
	// Engine-only and replay-derived like the other LKI maps: replay re-runs
	// the same effect resolution, so it repopulates identically, and the entry
	// is removed when the exploited object leaves a zone.
	exploitedLKI map[state.ObjID]state.SacrificedInfo
	// sourceLifelinkLKI maps an independently resolving ability's stack object
	// to its source permanent's derived lifelink state at the last moment that
	// source existed on the battlefield. The map's presence is the validity
	// bit: false is authoritative LKI too. It is captured before a source is
	// sacrificed as its own activation cost, refreshed for already-stacked and
	// pending abilities when their source later departs, cloned at intent
	// boundaries, and removed with the stack object. Resolution copies it into
	// effects.Ctx; effects uses it only when the source is no longer live.
	sourceLifelinkLKI map[state.ObjID]bool
	// sourceControllerLKI is the matching pre-departure controller snapshot.
	// It is separate from sourceLifelinkLKI because false lifelink is still a
	// valid snapshot, and a controller may be seat zero.
	sourceControllerLKI map[state.ObjID]state.PlayerID
	// damageSourceLKI carries snapshots keyed first by the waiting stack
	// object and then by a departed named DamageSource$ object. Unlike the
	// own-source maps above, every waiting resolution receives departures: the
	// named source can be TriggeredCard, Targeted, or Remembered.
	damageSourceLKI map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI
	// moveCounterAsk carries a MoveCounter resolution's ANSWERED asks across
	// the later suspensions of the same SA (the movecounter1 livelock fix).
	// A MoveCounter sub the placement/announcement ask never covered poses
	// its own ValidTgts$ target ask (the mvts1 pre-ask) AND, for
	// CounterType$ Any / CounterNum$ Any, asks of its own; every resume
	// builds a fresh Ctx and re-enters the SA from its top, so an earlier
	// round's answer (the target set, the chosen kind, the chosen amount)
	// must be re-seeded into that Ctx or the two asks alternate forever and
	// the resolution never drains (Nesting Grounds, Rikku, Goldberry's
	// second ability). rules/resolution.go's "tgts", "move_counter_kind"
	// and "move_counter" arms store their answers here and the re-entry
	// seeds them into the fresh Ctx before effects.Resolve; the entry is
	// deleted when the resolution completes. Decision-derived engine
	// scratch, in the triggerLKI discipline: replay re-submits the recorded
	// Intents through the same arms, so the map re-derives identically and
	// no event carries it. Never nil-checked on read outside recordAsk
	// (which lazy-inits).
	moveCounterAsk map[state.ObjID]*moveCounterPending
	// aorAsk carries an AddOrRemoveCounter resolution's ANSWERED per-kind
	// elections across the later suspensions of the same SA (the
	// moveCounterAsk discipline — counterchoice1). An EachExistingCounter$
	// walk (Dramatist's Puppet, Quarry Hauler) asks one add/remove election
	// per counter kind; every resume builds a fresh Ctx, so without this map
	// an already-answered PUT kind (whose counter count is still positive and
	// therefore still enumerates) would be re-asked forever. rules/
	// resolution.go's "aor_elect" arm records the answered kind here and the
	// re-entry seeds it into Ctx.AorAnswered; the entry is deleted when the
	// resolution completes. Decision-derived engine scratch, in the
	// moveCounterAsk discipline: replay re-submits the recorded Intents
	// through the same arms, so the map re-derives identically and no event
	// carries it.
	aorAsk map[state.ObjID]map[string]bool
	// counterTypeAsk carries per-recipient comma-list PutCounter answers across
	// suspensions. It is replay-derived engine scratch, never game state.
	counterTypeAsk map[state.ObjID]*counterTypePending
	// targetsPickAsk carries an ANSWERED generic ValidTgts$ pre-ask (the
	// mvts1 "tgts" arm) across a LATER suspension of the same SA, for every
	// API -- the general form of the moveCounterAsk cursor above, which
	// solved exactly this for MoveCounter alone. chosenTargetsFor CONSUMES
	// Ctx.TargetsPick before dispatching the body (fx42 scoping, so a nested
	// SA cannot inherit it), and every resume builds a FRESH Ctx; so if the
	// body then suspends on an ask of its own, the next resume re-enters the
	// SA from its top with no answer, re-poses the pre-ask, and the two asks
	// alternate forever. Kozilek's Command is the live carrier: its Charm
	// picks DBScry alongside another targeting mode, so the stack object's
	// one undivided target list is not DBScry's player, the pre-ask fires at
	// resolution, and the Scry's own KArrange is the second ask that loops
	// (arrange -> tgts -> arrange ...). Keyed by resolving stack object and
	// then by the SA's Line -- ResolveSVar parses fresh on every call, so
	// pointer identity never holds across a resume, the same matching
	// convention charmModeTarget and chosenTargetsFor's OfferedSA check use.
	// The per-SA key keeps one sub's answer off another sub's ask, and the
	// entry is deleted when THAT SA's resolution completes so a later
	// re-entry (a Repeat loop) asks afresh. Decision-derived engine scratch
	// in the moveCounterAsk discipline: replay re-submits the recorded
	// Intents through the same arm, so the map re-derives identically and no
	// event carries it.
	targetsPickAsk map[state.ObjID]map[string][]state.Target
	// orderedTriggers is how many LEADING entries of pendingTriggers have
	// already had their order settled by an answered KTriggerOrder decision
	// (or, for a lone trigger, by there being nothing to decide). It is the
	// whole of Task 27's resumable-drain state, and it exists because the
	// queue can grow while a controller is being asked: Submit runs handle,
	// then checkStateBased, then Advance, and checkStateBased (sba.go) is a
	// fixed-point loop whose PlayerLost/MoveZone/GameOver emits each run
	// checkTriggers. Appends land at the END; decisions are always about the
	// FRONT; and while this is non-zero putTriggersOnStack does not re-sort,
	// so a trigger that arrives mid-drain can neither be shuffled into a
	// group the player has already ordered nor make them order the same
	// triggers twice. Zero whenever pendingTriggers is empty.
	orderedTriggers int
	// applyingReplacement guards re-entrancy for the replaced event. Fresh
	// counter placements emitted by its body still receive their own
	// AddCounter replacement pass (unless already folded below).
	applyingReplacement bool
	// counterReplacementFold marks the already-rewritten event's final emit;
	// new counter events from a replacement body still take their own pass.
	counterReplacementFold bool
	// tokenMintSink, when non-nil, collects every object the TokenCreate or
	// CardToken event currently being emitted actually created
	// (EmitTokenCreate, and a parked mint's answer through withMintSink). It is a
	// stack discipline: a nested token creation saves and restores the outer
	// sink, so the outer effect's rider loop sees only its own mints. Nil on
	// every ordinary Emit, so no other emit pays for the collection.
	tokenMintSink *[]state.ObjID
	// mintParkFrom is EmitTokenCreate's report to SuspendTokenRest (rules/
	// token_rest.go): 1 + the replacement-choice queue length before an emit
	// that parked the resolution behind a replacement-order ask, else 0.
	// mintSinks are the collectors those parked mints' answers mint into,
	// keyed by an id from mintSinkSeq and consumed by the "token_rest" frame.
	// mintParkElection is the same report for the OTHER park: an as-enters
	// election (etbMove/riotMove/unleashMove/siegeMove) posed from inside the
	// emit parked the mint's own entry, so the election -- not a queued
	// competition -- is the continuation the collector rides (SuspendTokenRest
	// tags it through pendingMintSink).
	mintParkFrom     int
	mintParkElection bool
	mintSinks        []mintSink
	mintSinkSeq      uint64
	// tokenMintSinkID is the named collector (a mintSinks key) the current
	// tokenMintSink scope belongs to: withMintSink sets it to its collector's
	// id for the scope's duration and EmitTokenCreate resets it to 0 (its own
	// sink is a local buffer, never a named collector), both restored after.
	// An ask posed inside the scope records it as pendingMintSink, so an
	// as-enters election the scope's entry parked on carries the collector to
	// its answer. Engine scratch, never logged; Clone-copied (clone.go).
	tokenMintSinkID uint64
	// pendingMintSink is the collector id the PENDING mid-resolution ask's
	// answer must run under when that ask is an as-enters election that
	// parked a resolving DB$ Token's mint entry: Engine.ask records it from
	// tokenMintSinkID at pose time, and the election answer arms (rules/
	// turn.go's chooseETBEntry, chooseRiot, chooseUnleash, chooseSiege)
	// re-emit the parked entry under withMintSink with it, so publishTokenEntry
	// lands the minted id in the waiting collector. 0 = no collector. It is
	// overwritten at every pose, never cleared: a stale id names a collector
	// takeMintSink has already consumed, and withMintSink(0-or-spent) runs
	// unchanged. Engine scratch, never logged; Clone-copied (clone.go).
	pendingMintSink uint64
	// copyMintsPending are the CopyToken mints (a chosen-copy token plan's,
	// a DB$ CopyPermanent's) whose battlefield MoveZone has not completed
	// yet: the object exists in the library but has not entered. The emit
	// tail (publishTokenEntry) publishes such an id to tokenMintSink only
	// when its entry actually folds onto the battlefield -- directly, or on
	// the re-drive after a parked entry-counter order or as-enters election
	// is answered -- and drops it when the move lands anywhere else.
	copyMintsPending []state.ObjID
	// answerInResolution is set for the synchronous extent of an answer to a
	// competition or CreateToken election that suspended a stack resolution
	// (handleReplacement for an inResolution competition, tokenReplAnswer for
	// an election carrying a parkedResume). resolvingObj is 0 there, yet an
	// entry the answer stages still belongs to that resolution: its order
	// competition must resume the suspended frame when answered, not drop it
	// as a cast-window pose's bookkeeping. Never set between calls.
	answerInResolution bool
	// stackCopyMintSink, when non-nil, collects the object the StackCopy
	// event currently being emitted actually minted (EmitStackCopy). Same
	// stack discipline as tokenMintSink: a nested stack copy saves and
	// restores the outer sink. A StackCopy never spawns more than one object
	// (this engine has no CopySpell replacement), so the slice holds at most
	// one id. Nil on every ordinary Emit, so no other emit pays for it.
	stackCopyMintSink *[]state.ObjID
	// replReplaced is the ev.Obj of the replacement applyReplacements is
	// currently resolving — the object the replaced event was about. It is
	// seeded by applyReplacements (Ctx.Replaced = ev.Obj) and read by Ask to
	// thread that object across a mid-resolution suspension: a ReplaceWith$
	// body that poses an ask needs Replaced (and Remembered = [that object],
	// and the X value) restored on the resume, or its Defined$ ReplacedCard
	// resolution and SVar:X Remembered$Amount gating see nothing and the
	// completed move never happens (fx44, Mox Diamond). Zero whenever no
	// replacement is in flight.
	replReplaced state.ObjID
	// replReplacedCards is the ordered plural batch (Ctx.ReplacedCards) of the
	// replacement currently resolving -- the cascade instruction's exiled
	// cards, the counterpart of replReplaced for Averna's Defined$
	// ReplacedCards selector. Ask captures it onto the resume point so a
	// ReplaceWith$ body that suspends at its hidden pick re-resolves
	// ReplacedCards.<qual> against the same batch. nil outside a Cascade
	// replacement.
	replReplacedCards []state.ObjID
	// cascadeResidue is the synthetic SA effCascade wants run after a Cascade
	// replacement body (bottom the non-found exiled cards, then the free-cast
	// election). It is scoped to one ProposeCascadeReplacement call, the same
	// scratch pattern as scrySA/scryTarget; nil outside one.
	cascadeResidue *cards.SA
	// replacingEvent is the in-flight Damage event a DB$ ReplaceEffect body's
	// ReplaceEvent call may rewrite (Amount/Affected). It exists only during
	// emit, before the event is logged, so it is never part of
	// cloned/replayed engine state.
	replacingEvent  *events.Event
	replacingSource state.ObjID
	// replRemembered is the in-flight replacement body's remembered referents,
	// visible to that one body and restored right after it, the same scratch
	// pattern as replacingEvent. ReplaceEvent carries no Ctx, so the
	// VarValue$ Remembered rewrite reads its binding here. Never part of
	// cloned/replayed engine state: it lives only during the body run, before
	// the held event is logged.
	replRemembered []state.Target
	// replAction is the action marker (events.ActionMarker) of the event the
	// in-flight destination-changing replacement discarded: "sacrificed",
	// "discarded" or "discarded as a cost". emit re-labels the replacement
	// body's move of replReplaced with it (events.CarryAction), so a
	// sacrifice or discard redirected by a replacement is still seen as that
	// action by Sacrificed/Discarded triggers. Empty whenever no such
	// replacement is in flight; threaded across a suspension by resumePoint.
	replAction string
	// replReplacedPlayer is the player a replaced DRAW event was about (the
	// draw-er), threaded the same way replReplaced threads the replaced
	// object: a ReplaceWith$ body over R:Event$ Draw poses mid-resolution
	// asks (Breathstealer's Crypt's unless-pay discard) and the resume must
	// restore Ctx.ReplacedPlayer. Only a Draw replacement sets it.
	replReplacedPlayer state.Target
	// replRedirect is the destination-changing ("Replaced") move replacement
	// whose ReplaceWith$ body is resolving, with every replacement already
	// applied to that event (CR 614.5). A body move of the same object to a
	// DIFFERENT zone is the modified event of CR 616.1f and gets one more
	// replacement pass that skips those (Engine.emit). Immutable once set;
	// threaded across a suspension by resumePoint.redirect; nil at every
	// intent boundary outside a suspended body.
	replRedirect *replRedirect
	// replExclude is the applied set replRedirect carried into that one
	// recheck pass: applyReplacementsDispatch drops those matches and
	// applyReplacement extends it for a nested redirect. Nil otherwise.
	replExclude []string
	// triggerFireCount and the damage-batch fields below are trigger_match.go's
	// own bookkeeping (the cascade bound and the DamageDealtOnce/DamageDoneOnce
	// once-per-damage-batch gate); see there.
	triggerFireCount map[triggerKey]int32
	// unblockedOnceFired latches an AttackerUnblockedOnce trigger to ONE fire
	// per combat (rules.trigger_match.go's checkAttackerUnblockedOnceTriggers):
	// Forge's Mode$ AttackerUnblockedOnce fires once for the whole
	// declare-blockers round complete even when several attackers match, and
	// the Once means once per COMBAT, not per game -- an extra combat fires it
	// again. The stamp is (Turn, CombatsThisTurn), the event-folded per-turn
	// combat count, so it uniquely names a combat and needs no reset hook.
	unblockedOnceFired map[triggerKey]combatFires
	// unblockedRoundChecked stamps the (Turn, CombatsThisTurn) combat whose
	// declare-blockers round-complete trigger walk (checkAttackerUnblocked-
	// Triggers / checkAttackerUnblockedOnceTriggers, rules/turn.go step) has
	// already run. step() re-enters the StepDeclareBlockers arm every time
	// nothing is pending -- after an aborted cast (CR 733.1 reversal) no
	// handler re-grants priority, so the Advance loop calls step() again --
	// and "attacks and isn't blocked" is ONE event per combat (CR 509.2): a
	// second walk queued Senu, Keen-Eyed Protector's trigger again on every
	// aborted cast attempt, and the TriggerPush it drained cleared the F05-2
	// held-out cast suppression, so the no-progress abort re-offered forever
	// (cardfuzz batch10). The zero value names no combat (CombatsThisTurn is
	// at least 1 inside combat), so it needs no reset hook.
	unblockedRoundChecked combatFires
	// attackersDeclaredFired latches a BATCH trig:AttackersDeclared trigger
	// (Mode$ AttackersDeclared with no per-defender AttackedTarget$) to ONE
	// fire per declare step (rules.trigger_match.go's checkFaceTriggers;
	// CR 508.1). The engine emits one DeclareAttackers event per defending
	// player, but declaring attackers is ONE turn-based action, so a
	// "whenever you attack" trigger must fire exactly once even when the
	// attack is split across several defenders. The stamp is the same
	// (Turn, CombatsThisTurn) pair unblockedOnceFired uses -- one
	// declare-attackers step per combat -- so it needs no reset hook. The
	// per-defender shapes (Mode$ AttackersDeclaredOneTarget and any
	// AttackersDeclared line carrying AttackedTarget$) are never stamped and
	// keep firing per defender.
	attackersDeclaredFired map[triggerKey]combatFires
	// A damage batch is the set of Damage events dealt simultaneously: one
	// combat-damage pass (rules/combat.go damageStep), or the Damage events
	// one dealDamage-style effect call deals (effects/damage.go brackets each
	// of those with Host.BeginDamageBatch/EndDamageBatch), or — when neither
	// brackets it — one single Damage event, opened implicitly in emit.
	// DamageDealtOnce/DamageDoneOnce latch once per batch per trigger and
	// referent (dealing source / damaged object); the entries here carry the
	// accumulated batch amount the queued trigger's referent is patched to at
	// batch close. Never opened across a drain: pendingTriggers is append-only
	// while a batch is open, so the batch entries' recorded indices stay valid.
	// damageBatchDepth counts nested brackets, so an inner effect cannot close
	// its caller's simultaneous batch early.
	damageBatchOpen  bool
	damageBatchDepth int
	damageBatchIdx   map[damageBatchKey]int
	damageBatchLog   []damageBatchEntry
	// zoneBatch (RepeatEach's ChangeZoneTable$ True): the zone changes every
	// loop iteration's body causes are ONE ChangesZoneAll batch, presented
	// once after the loop completes. Same shape as the damage batch above:
	// the open bracket is engine memory (no event schema change; a replay
	// folds the same events through the same loop brackets and re-derives
	// the same entries), the entries record the queued trigger line's index
	// and the deduplicated moved set closeZoneBatch patches into the queued
	// trigger's Remembered/Captured plural capture. Never opened across a
	// drain, for the same reason as the damage batch. Outside a
	// ChangeZoneTable loop the bracket is never open, so the per-move
	// batch-of-one reading is untouched.
	zoneBatchOpen  bool
	zoneBatchDepth int
	zoneBatchIdx   map[zoneBatchKey]int
	zoneBatchLog   []zoneBatchEntry
	// millBatch (effects' api:Mill): one api:Mill resolution is ONE mill
	// action, so the Mode$ MilledAll "whenever one or more cards are milled"
	// trigger fires once for the whole call, not once per milled card. The
	// damage/zone batches' shape, but keyed by trigger LINE alone (the
	// DamageAll "one or more" reading): the first matching milled card queues
	// the single instance and every later matching card accumulates into the
	// entry's COUNT -- the number of cards milled this way, which the bodies
	// read through TriggerCount$Amount (The Wise Mothman's X, Screeching
	// Scorchbeast's "that many tokens"). Only the cards matching THIS line's
	// ValidCard$ count, exactly as DamageAll only accumulates matching pairs.
	// Never opened across a drain: pendingTriggers is append-only while the
	// batch is open, so the recorded index stays valid.
	millBatchOpen  bool
	millBatchDepth int
	millBatchIdx   map[triggerKey]int
	millBatchLog   []millBatchEntry
	// discardBatch (effects' api:Discard): one api:Discard resolution is ONE
	// discard action, so the Mode$ DiscardedAll "whenever you discard one or
	// more cards" trigger fires once for the whole resolution, not once per
	// discarded card. The millBatch's shape exactly: keyed by trigger LINE
	// alone (the "one or more" reading), the first matching discarded card
	// queues the single instance and every later matching card accumulates
	// into the entry's COUNT -- the number of cards discarded this way, which
	// the bodies read through TriggerCount$Amount (Magmakin Artillerist's X)
	// -- plus the deduplicated discarded-card set closeDiscardBatch patches
	// into Remembered/Captured. Only cards matching THIS line's ValidCard$
	// count. Unlike the mill bracket, api:Discard can SUSPEND mid-resolution
	// for a player's choice, so the bracket is opened on the first pass and
	// closed only on the pass that completes without suspending (effects/
	// cardflow.go's effDiscard) -- a suspension must not split one discard
	// action into two batches. Never opened across a drain: pendingTriggers
	// is append-only while the batch is open, so the recorded index stays
	// valid.
	discardBatchOpen  bool
	discardBatchDepth int
	discardBatchIdx   map[triggerKey]int
	discardBatchLog   []discardBatchEntry
	// discardAllTurn is the Mode$ DiscardedAll FirstTime$ latch: one trigger
	// LINE's most recent batch turn, so "for the first time each turn" admits
	// only the first qualifying discard batch per turn. Recorded at queue
	// time (when the batch's single instance is created) and cleared by the
	// turn boundary, the triggerTurnFires shape. Per trigger line is exact
	// for every corpus carrier, whose ValidPlayer$ is the source's own
	// controller ("You"); a line naming another player's discard would need a
	// per-player key, which no current carrier has.
	discardAllTurn map[triggerKey]int32
	// discardAllFirstTime is the Mode$ DiscardedAll FirstTime$ param scoped to
	// the matcher: discardedAllMatches (rules/trigmatch_cards.go), the ONLY
	// reader of the DiscardedAll line's FirstTime$, records the parsed clause
	// here as its receiver's transient scratch, and checkFaceTriggers captures
	// it immediately after the match call (before secondaryYields, which also
	// drives this same observer). It is scratch, not bookkeeping: it starts
	// false and is only meaningful for the instant between that matcher call
	// and the capture, so it is neither cloned nor replayed. Keeping the
	// `t.Params["FirstTime"]` literal inside the DiscardedAll-registered
	// matcher is what scopes the param-census read (rules/
	// paramcensus_test.go) to mode DiscardedAll instead of attributing it to
	// every trigger mode through the shared dispatcher.
	discardAllFirstTime bool
	// targetBatch brackets ONE targeting action's TargetsChosen events for the
	// Mode$ BecomesTargetOnce "one or more" latch (Forge's
	// TriggerBecomesTargetOnce fires once per spell/ability, after it has
	// chosen its targets). recordChosenTargets (rules/stack.go) opens the
	// bracket, emits one TargetsChosen per chosen target, and closes it;
	// checkFaceTriggers records the trigger LINES already queued in the open
	// batch in targetBatchFired, so a second matching target of the same
	// action cannot queue a second instance. Both the open flag and the map
	// are per-batch scratch, cleared on open and close, so no state survives
	// a targeting action and none needs cloning or a turn reset (the bracket
	// is entirely within one emit sequence, never across a drain).
	targetBatchOpen  bool
	targetBatchFired map[triggerKey]bool
	// phaseUnknownNoted memoizes the Phase$ specs whose names this engine has
	// already reported as unresolvable (rules.trigger_match.go's phaseMatches
	// reporting), so one spec emits exactly one Note per game no matter how
	// often its trigger is walked. Cloned like the other bookkeeping maps so
	// a branch that becomes live cannot re-emit the same Note.
	phaseUnknownNoted map[string]bool
	// phaseSpecs caches pure Phase$ parsing for both diagnostics and matching.
	// It is scratch, not replay bookkeeping: clones start with an empty cache.
	phaseSpecs map[string]parsedPhase
	// triggerEventMasks caches only immutable syntax for unbound fixture faces,
	// not live source membership. Bound corpus faces use their catalog-owned
	// trigger interests. Like phaseSpecs, clones own fresh writable scratch.
	triggerEventMasks map[*cards.Face]triggerEventMask
	// triggerObjectMasks is the dense object-walk form of triggerEventMasks.
	// Entries validate their immutable face pointer and are scratch owned by
	// one Engine, so hypothetical clones never share writable cache storage.
	triggerObjectMasks []objectTriggerEventMasks
	// trigZones / trigZonesEp / trigFaceZones are the live trigger walk's
	// per-player hidden-zone summaries (rules/trigger_zoneskip.go): pure
	// scratch validated on every use, so Clone copies none of them.
	trigZones     []trigZoneSummary
	trigZonesEp   int
	trigFaceZones map[*cards.Face]uint8
	// replZones / replZonesEp are the replacement-source walk's per-seat
	// zone summaries (rules/repl_zoneskip.go): pure scratch validated on
	// every use, so Clone copies neither.
	replZones   []replZoneSummary
	replZonesEp int
	// staticZones / staticZonesEp are the off-battlefield static-source
	// walks' per-seat zone summaries (rules/static_zoneskip.go): pure scratch
	// validated on every use, so Clone copies neither.
	staticZones   []staticZoneSummary
	staticZonesEp int
	// staticZoneVerified is verify-mode scratch (static_zoneskip.go's
	// staticZoneSkipVerifyOnce): the (cur, hot) slices each summary slot was
	// verified against inside the current verifyBoardStatics call
	// (staticZoneVerifyScope set). Clone copies none of it.
	staticZoneVerified    []staticZoneVerifiedAt
	staticZoneVerifyScope bool

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

	// suspendedCasts is the mandatory "cast it if able" trigger created when
	// a real suspended card loses its final TIME counter. IDs are appended in
	// exile order and consumed before priority; it is plain replayable engine
	// continuation state, not an inference from arbitrary exile cards.
	suspendedCasts []state.ObjID
	// defeatedCasts is the CR 310.11 "may cast it transformed without paying
	// its mana cost" offer for every battle the zero-defense SBA exiled
	// (rules/sba.go's battleZeroDefense). Fed by Engine.emit's battlefield→
	// exile feed (the ONE home every defeat route shares), drained before
	// priority by startDefeatedCast; plain replayable continuation state like
	// suspendedCasts above.
	defeatedCasts []state.ObjID
	// manaActivation is non-nil while a source with several available mana
	// abilities waits for its controller to select one. manaColorActivation
	// similarly holds an already-paid Produced$ Any ability, and
	// manaDiscardActivation holds an ability whose discard cost is being
	// chosen. All are plain data so Clone preserves an offered activation.
	manaActivation        *manaActivation
	manaColorActivation   *manaColorActivation
	manaDiscardActivation *manaDiscardActivation
	manaUnlessActivation  *manaUnlessActivation
	// offStackMana is the transient frame of the off-stack mana resolution
	// currently running synchronously (rules/mana_activation.go's
	// offStackManaFrame). It is nil between Submits, so Clone never sees it.
	offStackMana *offStackManaFrame
	// manaAfterCost is a mana ability whose cost is fully paid but whose
	// payment posed a decision -- a sacrificed or discarded commander's
	// CR 903.9 command-zone choice parks the move and asks its owner. The
	// mana effect (and its own colour choice) waits here until that answer
	// lands; Submit resumes it once nothing is pending (resumeManaAfterCost).
	// Plain data, deep-copied by Clone like its siblings.
	manaAfterCost *manaAfterCost
	// deferredAsks holds decisions posed while a CR 903.9 commander-zone
	// choice was pending (see ask): they are posed in order, one at a time,
	// once nothing is pending (drainDeferredAsks, from Submit). Deep-copied
	// by Clone like pending.
	deferredAsks []*decision.Decision
	// unlessPayment carries an in-progress non-mana unless-cost payment. It
	// keeps the enclosing resolution suspended while the payer chooses the
	// sacrifice/discard objects that pay it.
	unlessPayment *unlessPayment
	// Resolution-time payment windows. cumulative belongs to the replayable
	// keyword trigger; triggerCost belongs to an ordinary triggered effect
	// carrying Cost$ (Mana Vault). Both are plain data and Clone-copied.
	cumulative  *cumulativeUpkeep
	triggerCost *triggeredEffectCost
	// echo (rules/echo.go, kw:Echo): the pay-or-sacrifice election of a
	// resolving echo keyword trigger. Same plain-data class as the two
	// above; Clone-copied.
	echo *echoFlow

	// wardMana holds a CR 702.21a mana-payment window while a Ward trigger
	// is resolving, and (one shared owner, ruling T21-e) the same CR 601.2g
	// window for a mid-resolution UnlessCost$ (the `unless_pay` resume arm),
	// so a payer with an untapped source -- and a stat:ManaConvert conversion
	// -- can pay a cost its floating pool cannot cover. It is plain data so
	// Clone preserves the suspended choice.
	wardMana *wardManaPayment

	// attackPay holds the declare-attackers attack-cost payment window
	// (rules/attack_cost.go): the answered KAttackers declaration, its payer
	// and the outstanding charge, while the payer taps mana sources to cover
	// a CantAttackUnless prop. Same plain-data class as wardMana; Clone
	// copies the pointer.
	attackPay *attackPayWindow
	// blockPay holds the declare-blockers CantBlockUnless payment window.
	blockPay *blockPayWindow

	// cmdZone is the queue of parked commander zone changes (CR 903.9, Task
	// m32, rules/replacement.go): MoveZone events a commander is about to
	// undergo, deferred until its owner answers the KCommanderZone decision
	// for the FRONT entry. Multiple commanders can be parked by one burst (a
	// board wipe, one state-based-action pass), but only one decision can be
	// pending at a time, so the queue hands from one answer to the next
	// (handleCmdZone asks the new front after emitting the old one). Entries
	// are deduplicated by object id, so a re-offered SBA pass can never
	// enqueue the same commander twice (the no-progress failure shape a
	// replacement must not spin on). Never mutated while e.pending is nil
	// except by a park that also asks; Clone deep-copies it (clone.go) so a
	// clone taken with a decision outstanding carries the same queue. It is
	// always empty in a non-Commander game: nothing ever parks there.
	cmdZone []cmdZoneMove
	// legendBatch is the parked CR 704.5j legend-rule application (rules/
	// sba.go): the duplicate legendary set whose controller is choosing which
	// member to keep, the lethal-damage casualties found in the same SBA pass,
	// and the pre-batch look-back board. Parked and asked atomically by
	// parkLegendChoice; cleared and applied by legendAnswer. Parked ONLY
	// together with its ask (the pose is one step), so a clone taken at an
	// intent boundary either sees the zero value or a batch whose decision is
	// outstanding -- and must carry the batch, or answering the copied
	// decision would find nothing parked. Clone deep-copies it (clone.go),
	// the cmdZone class. Always nil outside an outstanding legend choice.
	legendBatch *legendBatch
	// replChoices is the queue of parked replacement choices (see replChoice /
	// handleReplacement in replacement.go): CR 616.1 ordering for MoveZone,
	// Untap, ProduceMana and BeginPhase, replacement-time mana-colour choices,
	// an Optional$ BeginPhase yes/no, and the AddCounter/CreateToken/Updated
	// competitions. Plain value entries are deep-copied by
	// Clone, so every in-flight event survives an intent boundary.
	replChoices []replChoice
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

	// costProvenanceSeen is the transient capture of the last cost-modifier
	// pass (castprov3): true when that pass evaluated a cost static whose
	// ValidCard$ carries a cast-provenance token (Bilbo's
	// "!wasCastFromYourHand" ReduceCost) — such a static is unresolvable
	// pre-push, so the pass denied it and the pending cast's payment needs
	// the post-push re-price continueCast runs right after CR 601.2a's push.
	// Set inside costStaticApplies (inside the costModifiers attribution
	// roots, so the param census sees no new read), cleared at the top of
	// every costModifiersWithTargets[ X]Using pass. Like noCounterSpend it
	// is synchronous computation state: every read of it (the option-
	// selection sites and continueCast's post-push re-price) happens in the
	// same driven flow as the pass that set it, and no ask suspends between
	// the pass and the read. Like noCounterSpend, Clone copies nothing of
	// it.
	costProvenanceSeen bool

	// damaging names the source object responsible for the damage emit
	// currently in flight (CR 609.7a): the resolution source for a spell or
	// ability being resolved, or the dealing creature for a combat
	// assignment. Task 15 sets it around resolveTop's two resolution
	// calls and combat's assignment loop, and emit consults it to prevent
	// damage to a protection-bearer whose protecting quality the source
	// carries (CR 702.16d). Zero when nothing is resolving/assigning damage;
	// a zero damaging never suppresses a Damage event.
	//
	// Not copied by Clone (Task 15 fix round 1, M5): Clone runs only at an
	// intent boundary, after New/Advance/Submit has returned, at which point
	// every resolution and damage-step that EVER sets damaging has completed
	// and reset it to zero -- an unset non-zero damaging would mean an emit
	// was still in flight, which is exactly the boundary Clone is prohibited
	// from crossing. So the field is always zero at a clone boundary and
	// copying it would copy a constant.
	damaging state.ObjID

	// combatDamaging distinguishes a combat-damage Damage event from a
	// noncombat one at trigger-match time (CombatDamage$ True/False, CR
	// 702.1x names the combat damage step's own assignments) WITHOUT touching
	// events.Event: the event struct's binary encoding is hash-chained and
	// replayed, so per-event context lives in engine state and is rebuilt by
	// replay because replay re-executes the same setter (the pg2 precedent).
	// dealCombatDamage (combat.go) sets it true alongside e.damaging for the
	// length of each assignment and clears it with the same reset; triggers
	// are checked synchronously inside emit (checkTriggers on the stored
	// event), so the flag is valid at match time. Every non-combat Damage
	// site -- the resolving ability in resolveTop/resumeResolution and the
	// effects/damage.go primitives those wrap -- leaves it false, and a
	// DealDamage cast during the combat damage step is still NOT combat
	// damage. Not copied by Clone, for the same reason as damaging above:
	// always zero at a clone boundary.
	combatDamaging bool

	// declaredAttackers is the WHOLE of the current declare-attackers
	// declaration: handleAttackers groups the chosen (attacker, defender)
	// pairs into one DeclareAttackers event PER DEFENDER and emits them in
	// turn order, so a trigger matched against one of those events sees only
	// that defender's attackers in ev.IDs. CR 702.70's Training compares the
	// attacking creature's power against ANOTHER creature attacking "with"
	// it -- which spans every defender in the same declaration. Like
	// combatDamaging this is engine scratch rather than an events.Event field
	// (the event encoding is hash-chained): handleAttackers sets it from the
	// chosen set before emitting, triggers are checked synchronously inside
	// emit, and replay re-executes handleAttackers, rebuilding it
	// deterministically. Not copied by Clone, for the same reason as
	// damaging/combatDamaging above: it is always set-and-consumed inside one
	// intent's driven flow, so it is stale-or-empty at a clone boundary.
	declaredAttackers []state.ObjID
	// Distinct opponents chosen in this declaration, ordered by first attack.
	// Like declaredAttackers this exists only during finishAttackers' emits;
	// the Melee trigger captures player refs into its logged stack object.
	declaredDefenders []state.PlayerID

	// manaFromTap and manaProducer identify the mana ability currently
	// resolving. They are synchronous context rather than ManaAdd fields.
	manaFromTap  bool
	manaProducer state.ObjID
	// paymentPlanCarriers memoises the objects whose faces carry a
	// Taps/TapsForMana trigger or a ProduceMana replacement -- the only
	// printed text the payment-plan source-interference check must run its
	// matchers over (rules/payment_plan_interference.go). The key is the
	// object-arena size plus the log head: a face or zone only changes through
	// an event or a new object. A pure derived memo, never copied by Clone.
	paymentPlanCarriers       []state.ObjID
	paymentPlanCarriersObjs   int
	paymentPlanCarriersEvents int
	paymentPlanCarriersValid  bool
	// stepLeaving is the step transition currently offered to BeginPhase
	// replacements; parked choices own a value copy.
	stepLeaving *state.Step

	// Tapping and damage provenance are likewise synchronous event context.
	tappingForMana      state.ObjID
	tappingManaProduced string
	// manaTapMark is the event-log length just after the most recent
	// activated mana ability's Tap (rules/mana_activation.go's emitManaTap).
	// resolveTriggeredManaAbilities scans e.L.Events from here for the
	// ManaAdd batch the activation produced, to bind each CR 605.3b
	// triggered mana ability's produced-type set (effects.TriggerContext.
	// TriggerMana, the ReflectProperty$ Produced read). It is set before the
	// mana effect resolves -- the actual ManaAdd events do not exist yet at
	// trigger-match time -- and consumed by the first batch resolution.
	// Transient engine scratch, zero at every intent boundary (Clone builds a
	// fresh Engine and never copies it). Zero means no pending activated tap.
	manaTapMark      int
	tapObj           state.ObjID
	tapPlayer        state.PlayerID
	tapEntering      bool
	tappedTurn       map[state.ObjID]int32
	triggerTurnFires map[triggerKey]turnFires
	// triggerGameFires is the lifetime queue count for GameActivationLimit$.
	// Unlike triggerTurnFires it is never reset at TurnChange.
	triggerGameFires map[triggerKey]int32
	// triggerTurnResolved is ResolvedLimit$'s per-turn resolution count,
	// keyed by the trigger's SOURCE object (not its triggerKey): Forge's
	// TriggeredAbility.resolvedThisTurn caps how many times a T: line may
	// RESOLVE each turn, and a ResolvedLimit$ card's paired lines (the
	// corruption_of_towashi halves of one printed ability) must share it.
	triggerTurnResolved map[state.ObjID]turnFires
	// triggerTurnDice is the RolledDie Number$ gate's per-turn die-roll count,
	// keyed by the trigger line (triggerKey) so each "whenever you roll your
	// third die each turn" line counts its own rolls. It self-resets when the
	// turn changes, exactly as triggerTurnFires does.
	triggerTurnDice map[triggerKey]turnFires
	// triggerTurnDiceTurn is the turn triggerTurnDice was last reset for;
	// a lookup in any later turn replaces the map, bounding its size.
	triggerTurnDiceTurn int32
	dmgSrcOverride      state.ObjID
	batchDamageKeywords map[state.ObjID]damageKeywordLKI

	// counterAdder is the player causing the CounterChange/PlayerCounterChange
	// events currently in flight (the repl:AddCounter class's "who would put
	// these counters" role), stored PLUS ONE so zero means "not published" --
	// seat 0 is a valid adder, so a bare zero cannot double as absence. Read
	// by inFlightCounterAdder, published by rules' cost/turn-based emitters
	// through SetCounterAdder. Not copied by Clone, for exactly the reason
	// the dmgSrcOverride/damaging fields above document: every publisher
	// restores its previous value before returning, so the field is always
	// the unpublished zero at a clone boundary. Replay rebuilds it because
	// replay re-executes the same setters.
	counterAdder state.PlayerID

	// foreachBuf is forEachObject's (trigger_match.go) scratch snapshot
	// buffer. forEachObject copies each zone into it before walking it -- fn
	// may move objects between zones (a trigger match putting something on
	// the stack), so iterating the live, mutating zone slice would be a bug.
	// append(buf[:0], zone...) grows it in place, so it settles at the size
	// of the largest zone seen and then stops allocating -- a fresh zone
	// copy per zone per event used to be forEachObject's 11.51 GB allocation
	// footprint (Task A2), the single largest allocator in this package.
	// Owned by this Engine alone: Clone leaves both fields zero, so a clone
	// and the original share no snapshot mid-walk (the clone just lets it
	// grow again). foreachDepth guards re-entry (see forEachObject): zero
	// outside a walk, one inside the depth-0 walk, higher inside a
	// re-entrant nested walk.
	foreachBuf   []state.ObjID
	foreachDepth int

	// legalOptBuf is legalActionsPriced's scratch option list. The walk
	// appends into it (so the doubling growth that used to reallocate the
	// list several times per walk settles at the largest walk seen) and
	// returns an exactly-sized COPY: the returned slice is owned by the
	// caller -- it becomes a pending Decision's Options, which seats, views,
	// traces and search forks retain -- so the scratch never escapes. The
	// walk takes the buffer (leaving nil) for its duration, so a re-entrant
	// walk allocates its own rather than clobbering the outer one. Owned by
	// this Engine alone: Clone leaves it nil, like foreachBuf.
	legalOptBuf []decision.Option
	// legalActionWalks counts every legalActionsPriced call (test-visible
	// only; unexported, bumped unconditionally, no event and no effect on
	// determinism or chain heads -- a plain monotonic read-only diagnostic
	// counter). It lets a test count legal-action walks directly now that
	// paymentActionsForPriority opens ONE derived-memo scope around the whole
	// offer build, which pins derivedMemoGen's delta at 1 regardless of how
	// many walks run inside. Clone leaves it zero, like the scratch fields.
	legalActionWalks uint64
	// manaAbBuf is the offer walk's per-object mana-ability scratch list
	// (legal.go), and manaLabels its "Activate <name> for mana" label cache
	// (manaActivateLabel; a pure function of the name, only ever looked up,
	// never ranged). Both are Engine-owned scratch: Clone leaves them nil.
	manaAbBuf  []*cards.SA
	manaLabels map[string]string
	// activeSum is active()'s per-build digest for the mana walk and
	// grantedAbilities (active_summary.go). Clone leaves it zero.
	activeSum activeSummary
	// faceScans memoises per-face text-scan verdicts (face_scan_memo.go).
	// Clone leaves it nil.
	faceScans map[*cards.Face]faceScan
	// intentBuf is a recycled intent array from Config.Spare, installed as
	// the log's Intents on the first Submit (see there). Not cloned.
	intentBuf []decision.Intent
	// sbaIDBuf is the battlefield-snapshot scratch attachmentSBAs and
	// checkSagas range (taken for the walk, restored after). Not cloned.
	sbaIDBuf []state.ObjID
}
