// Package rules is the authoritative rules engine: turn structure, priority,
// the stack, combat and state-based actions. It owns the only Game instance a
// match has and mutates it exclusively through events.Emit.
//
// The event log is NOT a complete match description by itself. Genesis --
// state.NewGame and the initial AddObject calls that build each player's
// deck into the object arena, both in New below -- runs before the log
// exists and legitimately bypasses events. Replaying L.Events alone
// reconstructs everything that happens from that point on (turn structure,
// zone moves, life, damage, priority and so on), but it can never recover
// deck contents or player names: those are never logged. A faithful replay
// needs the original Config together with the log, not the log alone.
package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Format is the game's construction format. FormatConstructed is the zero
// value -- every existing Config that never sets it (all today's fixtures
// and every non-Commander game) is unchanged.
type Format int

const (
	FormatConstructed Format = iota
	FormatCommander
)

type Config struct {
	Seed  uint64
	Names []string
	// PlayerNames is a per-seat display name independent of the deck
	// (the wire's PlayerView.Name carries it; Names is the deck identity
	// the engine uses in event text, which is replay-hashed). nil or a
	// short slice falls back to Names, so every Config that never sets it
	// behaves byte-identically to today.
	PlayerNames []string
	Decks       [][]*cards.Card
	// Sideboards carries each seat's optional sideboard. It is genesis
	// configuration rather than an event, so replay receives the same cards
	// without changing any existing event schema.
	Sideboards [][]*cards.Card
	// PlanarDecks carries each seat's optional Planechase deck. It is genesis
	// configuration, like Sideboards, and a zero value emits no new events.
	PlanarDecks [][]*cards.Card
	// Format names the construction format. Zero means Constructed; the other
	// tasks in the Commander milestone (the tax, CR 903.9, commander damage)
	// read it. This task is plumbing: it reads Commanders and StartingLife
	// only.
	Format Format
	// StartingLife is a game's opening life total. 0 (the zero value) means
	// the existing 20, so every Config that never sets it observes exactly
	// what it always did.
	StartingLife int32
	// Commanders holds, for each seat i, the indices into Decks[i] that are
	// that seat's commanders; nil or absent means none. Genesis places those
	// objects in the command zone instead of the library. An index out of
	// range for its deck is a config error degraded the same way New degrades
	// more decks than seats: the offending entry is skipped, not a crash.
	Commanders [][]int
	// Mulligans is the number of London mulligans each player may take in the
	// pre-game round between the opening deal and turn 1. 0 (the zero value)
	// skips the round entirely, so every Config that never sets it is
	// unchanged (all standalone fixture Configs). It sits in the same Config
	// replay is handed, so a replay reproduces the round (Ruling R-8.4): the
	// acceptance config sets it so the 12-deck suite exercises keep/mulligan
	// and bottoming.
	Mulligans int
	// Tokens is the token definitions the decks in this match can create --
	// cards.Registry.Tokens. Copied onto Game.Tokens in New so
	// events.Apply's TokenCreate case has something to mint from. Replay
	// must pass the same table a live match's Config did.
	Tokens map[string]*cards.Card
	// NameUniverse is the compiled corpus used by NameCard decisions.
	NameUniverse []*cards.Card
	// NameUniverseNames pins a persisted match's sorted name list. A live
	// match leaves it nil and derives it from NameUniverse at genesis.
	NameUniverseNames []string
	// LoopGuard, when non-nil, overrides the livelock watcher's thresholds
	// for this game (rules/livelock.go): how many consecutive events a
	// repeating cycle must run before the engine aborts with a
	// *LivelockError, the longest cycle tracked, and the no-progress
	// runaway backstop. nil (the zero value every existing Config has) is
	// the defaults, so every game that never sets it is byte-identical to
	// an un-watched one. LoopGuard.Disabled (explicitly set) turns the
	// watcher off entirely -- the embedder's own opt-out for a supervised
	// non-terminating game; the host sets it whenever its
	// MaxDecisionsPerTurn opt-out is in force. The watcher is pure
	// observation either way: it emits no event and holds no state the
	// engine reads.
	LoopGuard *LoopGuard

	// Spare, when non-nil, is a finished game's storage (Engine.Release)
	// the new engine reuses for its log and object arena. It never changes
	// the game -- see Spare. It is consumed: New empties *Spare, so a copy
	// of this Config that builds a second engine (a replay, a coverage
	// rebuild) allocates fresh instead of sharing the first engine's arrays.
	Spare *Spare
}

type triggerObjectLKI struct {
	object           *state.Object
	power, toughness int32
	ptValid          bool
}

// counterAddedThisTurn is engine-only provenance for positive object counter
// placements. The snapshot is captured before events.Apply mutates the object.
type counterAddedThisTurn struct {
	actor  state.PlayerID
	kind   string
	amount int32
	object state.Object
}

type Engine struct {
	G             *state.Game
	L             *events.Log
	compiledText  *compiledText
	landTypeWords []string

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

// inFlightDamageSource is the one reader for Damage-event provenance: the
// published override when a damage emitter set one, else the resolution/combat
// source e.damaging carries. Zero when neither is set (a Damage event with no
// recorded source -- emit's protection check treats zero as "never prevent",
// and damageMatches fails the ValidSource$ match).
func (e *Engine) inFlightDamageSource() state.ObjID {
	if e.dmgSrcOverride != 0 {
		return e.dmgSrcOverride
	}
	return e.damaging
}

// SetDamageSource implements effects.Host: publish the damage source for the
// Damage events the calling emitter is about to emit, returning the previous
// value so the emitter restores it. See dmgSrcOverride's field doc for the
// replay/clone discipline.
func (e *Engine) SetDamageSource(id state.ObjID) state.ObjID {
	prev := e.dmgSrcOverride
	e.dmgSrcOverride = id
	return prev
}

// inFlightCounterAdder is the one reader for AddCounter-replacement
// provenance: the player causing the counter placement currently in flight,
// and whether that attribution is known at all. A published override wins
// (the cost/turn-based sites, where no stack cause exists yet). Otherwise the
// controller of actionCause() -- the resolving spell or ability wrapper at
// the top of the stack -- is the adder: a resolving spell, activated ability
// or triggered-ability instruction IS the cause, and the counters it puts are
// put by that ability's controller (Vorinclex's "If YOU would put", Halving
// Season's "If an OPPONENT would put"). Third, a placement a replacement BODY
// makes (replacementBodyCounterAdder below) names the body source's
// controller: by the time the body's nested CounterChange emits, the entry
// move has already applied and the wrapper is off the stack, so the
// actionCause fallback cannot see it. Zero with ok=false when none of the
// three is available (an SBA or other bare placement): the AddCounter matcher
// then fails a ValidSource$ line closed rather than guessing an adder.
func (e *Engine) inFlightCounterAdder() (state.PlayerID, bool) {
	if e.counterAdder != 0 {
		return e.counterAdder - 1, true
	}
	if c := e.actionCause(); c != 0 {
		return e.controllerOf(c), true
	}
	if adder, ok := e.replacementBodyCounterAdder(); ok {
		return adder, true
	}
	return 0, false
}

// replacementBodyCounterAdder is the counter-adder provenance of a placement
// a replacement BODY makes. While a ReplaceWith$ body is resolving
// (applyingReplacement set, the final emit not yet folded), a
// CounterChange/PlayerCounterChange it emits is the body's own instruction:
// CR 614.5 -- the replacement does not use up its event, and the counter its
// instruction places is a NEW event whose cause is that replacement effect.
// The "who is putting these counters" role (ValidSource$) and the "an effect
// would put" wording (EffectOnly$) therefore read the body source's
// controller: for a K:etbCounter entry the source is the entering permanent,
// so its controller is the adder (Doubling Season doubles the entry counters;
// an opponent's Vorinclex halves them). The counterReplacementFold exclusion
// keeps the distinction that matters: the notification-only CounterChange
// records (foldEntryMove's EntryCounterNotice tail, and the AddCounter class's
// own fully-rewritten final emit) are already-settled echoes of a placement
// whose replacement pass has run, not body instructions, and inside them the
// source slot may name an unrelated outer replacement -- attributing the echo
// to it would double-count. Zero with ok=false outside a body or when the
// source has left the game.
func (e *Engine) replacementBodyCounterAdder() (state.PlayerID, bool) {
	if !e.applyingReplacement || e.counterReplacementFold || e.replacingSource == 0 {
		return 0, false
	}
	if e.G.Obj(e.replacingSource) == nil {
		return 0, false
	}
	return e.controllerOf(e.replacingSource), true
}

// counterAdderUnset is SetCounterAdder's opaque "no publication" token. It
// is state.PlayerID(255), a seat no game can hold, so a caller can round-trip
// the previous publication (including the absence of one) through the SAME
// method without a second restore call: seat 0 is a legitimate adder, so a
// bare 0 cannot double as the sentinel.
const counterAdderUnset = state.PlayerID(255)

// SetCounterAdder implements effects.Host: publish the player causing the
// CounterChange/PlayerCounterChange events the caller is about to emit, and
// return the previous publication -- a PlayerID, or counterAdderUnset when
// none was published -- for the caller to pass straight back to restore it
// (the SetDamageSource shape, extended with an explicit unset token so seat 0
// round-trips correctly). rules is the only publisher: effect-resolution
// sites are covered by inFlightCounterAdder's actionCause fallback, and only
// a cost or turn-based placement (which has no stack cause) needs an
// explicit publish.
func (e *Engine) SetCounterAdder(p state.PlayerID) state.PlayerID {
	prev := counterAdderUnset
	if e.counterAdder != 0 {
		prev = e.counterAdder - 1
	}
	if p == counterAdderUnset {
		e.counterAdder = 0
	} else {
		e.counterAdder = p + 1
	}
	return prev
}

// damageKeywordLKI is the derived damage-relevant keyword set of one source,
// snapshotted before it leaves the battlefield. CR 113.7a reads the source's
// last known characteristics for the whole damage rider: the life gain
// (CR 702.15a), the counter form (CR 702.90b) and the deadly mark
// (CR 702.2b) all answer off the same pre-departure state.
type damageKeywordLKI struct {
	lifelink   bool
	infect     bool
	wither     bool
	deathtouch bool
}

func (e *Engine) damageKeywordsOf(id state.ObjID) damageKeywordLKI {
	return damageKeywordLKI{
		lifelink:   e.HasKeyword(id, "Lifelink"),
		infect:     e.HasKeyword(id, "Infect"),
		wither:     e.HasKeyword(id, "Wither"),
		deathtouch: e.HasKeyword(id, "Deathtouch"),
	}
}

// BatchDepartures implements effects.Host: snapshot the derived damage
// keywords of every object the caller is about to move in one destruction
// batch, so each member's departure capture reads the pre-batch state no
// matter where it sits in battlefield order. See batchDamageKeywords' field
// doc for the consumption discipline.
func (e *Engine) BatchDepartures(ids []state.ObjID) {
	e.batchDamageKeywords = make(map[state.ObjID]damageKeywordLKI, len(ids))
	for _, id := range ids {
		e.batchDamageKeywords[id] = e.damageKeywordsOf(id)
	}
}

// EndBatchDepartures closes a destruction/sacrifice batch even if one of its
// proposed moves was prevented or replaced. Without this explicit boundary,
// that survivor's pre-batch LKI could be consumed by an unrelated later move.
func (e *Engine) EndBatchDepartures() { e.batchDamageKeywords = nil }

// chooseFor names the flow a pending KChoose decision belongs to. Task 9
// declares chooseCast (rules/cast.go); Tasks 12 and 18 add the "as this
// enters" and miracle cases in their own files.
type chooseFor uint8

const chooseNone chooseFor = iota

// commanderCardLegal reports whether ONE card may be a commander under
// CR 903.3. It DELEGATES to deck.IsCommanderEligible -- the one
// implementation the deck validator (File.ValidateCommander) also uses -- so
// the validator and the engine can never disagree about which cards seat:
// a legendary creature, a legendary Vehicle, a legendary Spacecraft with a
// printed power/toughness box (Hearthhull, the Worldseed), or a card whose
// printed/Oracle text says it can be your commander (the planeswalker
// commanders and the Partner/choose-a-background cases). The old inline
// predicate here (a legendary creature on Faces[0], commander permission
// read from Keywords only) was the stale half of a two-implementation
// disagreement and rejected decks the validator accepted.
func commanderCardLegal(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 {
		return false
	}
	return deck.IsCommanderEligible(c)
}

// partnerPairOK reports whether two cards may be a commander PAIR: each
// carries a Partner-family ability and either both are plain Partners, or
// each "Partner with" the other by printed name (CR 903.13a/c), or at least
// one carries K:Doctor's companion and the other is a Doctor (the Doctor Who
// cycle's companion clause, which also admits two distinct Doctors that each
// carry it). The check
// itself lives in deck.IsPartnerPair — the same package that owns
// IsCommanderEligible (which commanderCardLegal above already delegates to),
// so the deck-file validator and the engine's seating gate cannot disagree
// about what a legal pair is.
func partnerPairOK(a, b *cards.Card) bool {
	return deck.IsPartnerPair(a, b)
}

// legalCommandersFor validates seat i's configured commander list against
// the deck-construction rules (CR 903.4/903.13) and returns the indices
// that MAY be seated, in Config order: a single commander must be a
// legendary creature or a "can be your commander" card; a two-card seat is
// a legal partner pair (plain Partners, a mutual "Partner with" pair, or a
// Doctor's-companion pair);
// anything else -- a noncommander card, a pair without partner, more than
// two -- is rejected WHOLE, never silently trimmed into a legal-looking
// subset. This is what makes an illegal Config fail in play: the rejected
// seat plays commander-less and the rejection is on the log as a Note (the
// same degrade-don't-crash stance New takes for malformed decks elsewhere --
// New cannot return an error, so the Note is the record).
func (c *Config) legalCommandersFor(i, deckLen int, deck []*cards.Card) []int {
	raw := c.commandersFor(i, deckLen)
	if len(raw) == 0 {
		return nil
	}
	// Construction legality belongs to Commander games. Config.Commanders also
	// intentionally powers constructed-format fixture and compatibility paths
	// (where it merely selects command-zone objects), so preserve that legacy
	// plumbing outside FormatCommander.
	if c.Format != FormatCommander {
		return raw
	}
	bad := func(why string) ([]int, string) {
		names := ""
		for _, idx := range raw {
			if idx >= 0 && idx < len(deck) && deck[idx] != nil {
				names += deck[idx].Faces[0].Name + ", "
			}
		}
		return nil, names + why
	}
	reject := ""
	switch len(raw) {
	case 1:
		idx := raw[0]
		if idx >= 0 && idx < len(deck) && !commanderCardLegal(deck[idx]) {
			_, reject = bad("is not a legendary creature and does not say it can be your commander")
		}
	case 2:
		a, b := raw[0], raw[1]
		inRange := func(x int) bool { return x >= 0 && x < len(deck) && deck[x] != nil }
		if !inRange(a) || !inRange(b) {
			_, reject = bad("is not a card this deck carries")
		} else if !commanderCardLegal(deck[a]) || !commanderCardLegal(deck[b]) || !partnerPairOK(deck[a], deck[b]) {
			_, reject = bad("is not a legal commander pair")
		}
	default:
		_, reject = bad("is not one or two commanders")
	}
	if reject != "" {
		return nil
	}
	return raw
}

// commandersFor returns the VALID commander indices (into deck of length
// deckLen) that Config names for seat i, in Config order. An index out of
// range for the deck, or a seat with no Commanders entry, contributes
// nothing -- the same degrade-don't-crash stance New already takes for more
// decks than seats: the bogus entry is skipped, never a panic.
func (c *Config) commandersFor(i, deckLen int) []int {
	if i < 0 || i >= len(c.Commanders) {
		return nil
	}
	var out []int
	for _, idx := range c.Commanders[i] {
		if idx >= 0 && idx < deckLen {
			out = append(out, idx)
		}
	}
	return out
}

const openingHand = 7

// Spare is a finished game's reusable backing storage -- its event log array
// and its object arena -- handed from Engine.Release to the next game a batch
// runner builds (Config.Spare). Those two arrays are an engine's largest
// per-game allocations (~450 KB and ~200 KB for a 60-card 2-seat game), and a
// runner that plays thousands of games back to back otherwise allocates,
// zeroes and collects them once per game; the intent array and the Derived
// memo tables ride along. Reuse is invisible to the game: events.NewLogInto
// and state.NewGameInto re-cap their arrays to exactly the capacity a fresh
// allocation would have had, so growth points are unchanged; every slot of
// every array is cleared by Release and overwritten before it is read; the
// memo tables are a cache whose capacity no answer depends on; and the
// intent array is only ever appended to (Log.Clone caps it). The zero Spare
// is "none"; TestSpareReuseIsInvisible pins the contract.
type Spare struct {
	events  []events.Event
	objs    []state.Object
	intents []decision.Intent
	// The Derived memo tables (derivedmemo.go): an ObjID index grown to the
	// arena's size plus the slots; cleared by Release, which is exactly the
	// zeroed never-written state derivedMemoTable.slot's growth relies on.
	memo, memoStack derivedMemoTable
}

// Release returns e's log and object-arena arrays as a Spare for the next
// game (pass its address as Config.Spare) and leaves e unusable (its Objs and Events are nil, so a stray later
// use fails loudly rather than reading a recycled array). It must be the
// LAST use of e and of anything sharing its arrays -- a Clone's log shares
// the Events prefix (events.Log.Clone) -- which is why only a batch runner
// that owns the finished engine outright calls it. The arrays are cleared so
// the Spare does not pin the finished game's cards, strings and slices.
func (e *Engine) Release() Spare {
	sp := Spare{
		events:    e.L.Events[:cap(e.L.Events)],
		objs:      e.G.Objs[:cap(e.G.Objs)],
		intents:   e.L.Intents[:cap(e.L.Intents)],
		memo:      e.derivedMemo.release(),
		memoStack: e.derivedMemoStack.release(),
	}
	clear(sp.events)
	clear(sp.objs)
	clear(sp.intents)
	e.L.Events, e.G.Objs, e.L.Intents = nil, nil, nil
	e.derivedMemo, e.derivedMemoStack, e.intentBuf = derivedMemoTable{}, derivedMemoTable{}, nil
	return sp
}

// objectHeadroom is the extra Objs capacity newWithRNG reserves beyond the
// dealt decks and sideboards (see its use there).
const objectHeadroom = 128

func New(cfg Config) *Engine {
	return newWithRNG(cfg, newRNG(cfg.Seed), false)
}

// NewStartingPlayerChoice is the harness-facing constructor that offers CR
// 103.1's second half (rules/starting_player_choice.go): the toss winner
// CHOOSES who takes the first turn. Genesis (the resolved toss folded into
// G.StartingPlayer) is identical to New's, but startPostDealSetup -- the
// London mulligan round, the colour round, turn 1 -- is deferred until the
// choice is answered (Engine.AskStartingPlayer + Submit) or defaulted at the
// first Advance, so the pregame rounds open in the CHOSEN seat's turn order.
// A caller that poses no ask and never advances past genesis sees nothing;
// every other constructor (plain New) is the R-9 no-host fallback: the toss
// winner takes the first turn silently, byte-identical to the pre-choice
// engine. host, mtgsim and the acceptance driver use this constructor.
func NewStartingPlayerChoice(cfg Config) *Engine {
	return newWithRNG(cfg, newRNG(cfg.Seed), true)
}

func newWithRNG(cfg Config, random *rng, tossAsk bool) *Engine {
	life := int32(20)
	if cfg.StartingLife > 0 {
		life = cfg.StartingLife
	}
	initialObjects := 0
	for i, deck := range cfg.Decks {
		if i >= len(cfg.Names) {
			break
		}
		initialObjects += len(deck)
		if i < len(cfg.Sideboards) {
			initialObjects += len(cfg.Sideboards[i])
		}
		if i < len(cfg.PlanarDecks) {
			initialObjects += len(cfg.PlanarDecks[i])
		}
	}
	// Headroom past the dealt cards for the objects a game mints as it plays
	// (tokens, ability objects on the stack, copies): measured over the repo
	// deck matrix (botbench -pairs all, constructed and commander), a game
	// adds a median ~50 and a 99th-percentile ~125 objects to its dealt
	// cards, and without headroom EVERY game regrew Objs (a full doubling of
	// an ~800-byte-per-element array) on its first minted object.
	if initialObjects > 0 {
		initialObjects += objectHeadroom
	}
	var spare Spare
	if cfg.Spare != nil {
		spare, *cfg.Spare = *cfg.Spare, Spare{}
	}
	e := &Engine{
		G:             state.NewGameInto(cfg.Names, life, initialObjects, spare.objs),
		L:             events.NewLogInto(cfg.Seed, spare.events),
		format:        cfg.Format,
		rng:           random,
		loop:          newLivelockWatcher(cfg.LoopGuard),
		turnsTaken:    make([]int32, len(cfg.Names)),
		compiledText:  newCompiledText(cfg),
		landTypeWords: corpusLandTypeWords(cfg.NameUniverse),
		mulligans:     cfg.Mulligans,
		startingLife:  life,
		// The per-turn ManaExpend tally (rules/cast.go) starts empty; payCast
		// stamps and resets it lazily on e.G.Turn.
		manaExpended: make([]int32, len(cfg.Names)),
	}
	// The rest of a Spare: the memo tables start empty over the cleared
	// arrays (derivedMemoizedAt only reslices up into zeroed capacity), and
	// the intent array waits for the first Submit (the log's Intents stays
	// nil until an intent exists, as it always has).
	e.derivedMemo, e.derivedMemoStack = spare.memo, spare.memoStack
	if cap(spare.intents) > 0 {
		e.intentBuf = spare.intents[:0]
	}
	e.G.Tokens = cfg.Tokens
	e.setNameInPool = poolHasSetNameStatic(cfg)
	e.layer4InPool = poolHasLayer4Static(cfg)
	e.G.NameUniverse = cfg.NameUniverse
	e.G.NameUniverseNames = append([]string(nil), cfg.NameUniverseNames...)
	if len(e.G.NameUniverseNames) == 0 && len(cfg.NameUniverse) > 0 {
		e.G.NameUniverseNames = effects.NameUniverseNames(cfg.NameUniverse)
	}
	e.manaExpendedTurn = e.G.Turn
	e.format = cfg.Format
	for i := range e.G.Players {
		if i < len(cfg.PlayerNames) && cfg.PlayerNames[i] != "" {
			e.G.Players[i].PlayerName = cfg.PlayerNames[i]
		}
	}
	e.emit(events.Event{Kind: events.GameStart, Amount: int32(len(cfg.Names))})
	// CR 103.1: the starting player is determined by a random method. Draw
	// the toss HERE, as the FIRST rng consumption of the game, before any
	// per-seat shuffle: the toss value is then a pure function of (seed,
	// seat count), independent of every deck size. The surviving-seat
	// resolution happens after the deal below (the toss draw is uniform over
	// every seat, so conditioned on naming a survivor it is uniform over the
	// survivors -- see the resolution site). CR 103.1's second half -- the
	// toss winner CHOOSES who takes the first turn -- is not implemented;
	// see the "Known approximations" row in AGENTS.md.
	toss := -1
	if len(cfg.Names) > 0 {
		toss = e.rng.IntN(len(cfg.Names))
	}
	// CR 103.1 precedes 103.2-103.4: the public toss announcement is emitted
	// HERE -- before the first shuffle and the opening hand (Forge's
	// GameAction and manabrew's game loop announce the toss before their deal
	// too), so the keep/mulligan decisions are made with the toss already
	// public. The Note names the seat the rng handed the toss to -- the true
	// CR 103.1 winner -- even if the deal below then eliminates them; who
	// actually takes the first turn is resolved after the deal has fixed the
	// survivors. The text carries the deck identity, never the display
	// PlayerName (F3 keeps display names out of the chain); view/describe.go
	// renders this one Note through player(), so a seated human still reads
	// their own name.
	if toss >= 0 {
		e.emit(events.Event{Kind: events.Note, Player: state.PlayerID(toss),
			Text: tossName(e.G, state.PlayerID(toss)) + " won the toss"})
	}
	// Match-wide dense commander indexing for Player.CmdDamage (assigned at
	// genesis): a commander's dense index is the sum of (valid commanders in
	// seats before its owner) + (its own position within its owner's
	// Commanders list, which is the order Commanders is built in the loop
	// below) -- both deterministically derivable from this Config, so no
	// separate index needs storing. Every seat's CmdDamage is sized to total
	// (the whole match's commander count) so it can be indexed by ANY
	// commander's match-wide dense index: seat B's damage holds a slot for
	// seat A's commander at A's commander's dense index. Sized here and
	// never grown; three small copy() calls per seat carry all three across
	// Game.Clone.
	totalCmd := 0
	for i := range cfg.Names {
		if i < len(cfg.Decks) {
			totalCmd += len(cfg.legalCommandersFor(i, len(cfg.Decks[i]), cfg.Decks[i]))
		}
	}
	// Opening hands are dealt as one genesis operation. Defer only the final
	// GameOver event: drawCard still emits losses and runs all other SBAs, but
	// the terminal marker must follow the public toss Note so the host can
	// recognize and persist the complete genesis burst.
	e.deferGameOver = true
	for i, deck := range cfg.Decks {
		if i >= len(cfg.Names) {
			// Ruling T22-m (fix round 2): a malformed Config with more
			// decks than named seats has nowhere to put the rest --
			// state.NewGame above sizes g.zones from len(cfg.Names) alone,
			// so PlayerID(i) here would index outside it and panic
			// (SetZone -> zoneIndex -> an out-of-range g.zones write).
			// Task 25 wires Config from a client, so a malformed one must
			// degrade, not crash the one goroutine running the whole
			// match; the excess decks are simply never dealt, the same
			// spirit as the zero-alive guard below for a Config with no
			// seats at all.
			break
		}
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(deck))
		for _, c := range deck {
			ids = append(ids, e.G.AddObject(c, p).ID)
		}
		e.G.SetZone(state.ZLibrary, p, ids)
		if i < len(cfg.Sideboards) && len(cfg.Sideboards[i]) > 0 {
			sb := make([]state.ObjID, 0, len(cfg.Sideboards[i]))
			for _, c := range cfg.Sideboards[i] {
				o := e.G.AddObject(c, p)
				o.Zone = state.ZSideboard
				sb = append(sb, o.ID)
			}
			e.G.SetZone(state.ZSideboard, p, sb)
		}
		if i < len(cfg.PlanarDecks) && len(cfg.PlanarDecks[i]) > 0 {
			planes := make([]state.ObjID, 0, len(cfg.PlanarDecks[i]))
			for _, c := range cfg.PlanarDecks[i] {
				o := e.G.AddObject(c, p)
				planes = append(planes, o.ID)
			}
			planes = e.shufflePlanarDeck(p, planes)
			e.emit(events.Event{Kind: events.PlanarDeckShuffle, Player: p, IDs: planes, Secret: true})
			if len(planes) > 0 {
				e.emit(events.Event{Kind: events.PlanarReveal, Player: p, Obj: planes[0]})
			}
		}
		// Commanders leave the library for the command zone here, BEFORE the
		// shuffle and BEFORE the opening hand is dealt, so they are neither
		// shuffled into the library nor drawable. Emitted as real MoveZone
		// events (one per commander, in Config order) -- the log is the only
		// source of truth, and replay, which folds the logged events back
		// through this same New, reproduces the identical command zone. For a
		// non-Commander Config commandersFor is empty, so nothing is emitted
		// and the Shuffle below covers the whole library exactly as before.
		var myCmds []state.ObjID
		for _, idx := range cfg.legalCommandersFor(i, len(deck), deck) {
			id := ids[idx]
			myCmds = append(myCmds, id)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZLibrary, To: state.ZCommand})
		}
		// A commander list that failed the deck-construction validation seats
		// nothing; the rejection is on the log (rules/legal... engine.go's
		// legalCommandersFor) so a transcript shows why the command zone is
		// empty.
		if len(myCmds) == 0 && len(cfg.commandersFor(i, len(deck))) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "commander configuration rejected under CR 903.4/903.13"})
		}
		e.G.Players[p].Commanders = myCmds
		if len(myCmds) > 0 {
			e.G.Players[p].CmdCasts = make([]int32, len(myCmds))
		}
		if totalCmd > 0 {
			e.G.Players[p].CmdDamage = make([]int32, totalCmd)
		}
		// Shuffle only what is left in the library -- the commanders have just
		// moved out, so a non-Commander seat's library and the original ids
		// are one and the same and the event is byte-identical to before.
		order := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...)
		order = e.ShuffleLibrary(p, order)
		// Library order is hidden information: the event carries it because the
		// server needs it, and view projection redacts it for everyone else.
		e.emit(events.Event{Kind: events.Shuffle, Player: p, IDs: order, Secret: true})
		for j := 0; j < openingHand; j++ {
			e.drawCard(p)
		}
		if e.G.AliveCount() <= 1 {
			// Preserve T22-c's terminal-deal boundary: once at most one seat
			// remains, do not shuffle or deal a later hand. A later seat whose
			// configured library could not supply seven cards is nevertheless
			// also doomed by this same opening deal; account for that loss so
			// an all-undersized table truthfully reaches CR 104.4a's no-survivor
			// draw rather than accidentally crowning an undealt short deck.
			for next := i + 1; next < len(cfg.Decks) && next < len(cfg.Names); next++ {
				available := len(cfg.Decks[next]) - len(cfg.commandersFor(next, len(cfg.Decks[next])))
				if available < openingHand && !e.G.Players[next].Lost {
					e.emit(events.Event{Kind: events.PlayerLost, Player: state.PlayerID(next), Text: "drew from an empty library"})
				}
			}
			if e.finishTerminalGenesis() {
				return e
			}
		}
	}
	if e.finishTerminalGenesis() {
		return e
	}
	e.deferGameOver = false
	alive := e.G.AliveFrom(0)
	// CR 103.1: the starting seat is the toss result, not seat 0.
	// Resolve the starting seat uniformly over the SURVIVORS. The pre-shuffle
	// toss draw is uniform over every seat, so CONDITIONED on naming a
	// survivor it is already uniform over the survivors -- it is the first
	// candidate and costs no further rng. Only when it named a seat the deal
	// eliminated does rejection sampling draw again: IntN over all seats,
	// retried until a survivor is hit, each round uniform over the survivors
	// once conditioned. In the no-elimination case -- every real game -- the
	// stream is exactly one IntN and the candidate is always the first. A
	// modulo over the survivor count instead would be BIASED: three seats
	// with seat 0 eliminated maps two of the three toss outcomes onto one
	// survivor (measured 395/205 over 600 seeds on the pre-fix code).
	start, _ := e.resolveToss(toss, alive, len(cfg.Names))
	// CR 103.1's SECOND half: the winner of the toss chooses who takes the
	// first turn, and that answer -- not the raw toss draw -- is the
	// starting seat. ONLY a tossAsk constructor offers the choice (see
	// NewStartingPlayerChoice): it is not posed by plain New, because a
	// genesis-genesis decision would appear in every test and fuzz log and
	// the R-9 no-host contract wants a fallback that completes without an
	// answer. The RESOLVED TOSS is folded below UNCONDITIONALLY either way,
	// so genesis (G.StartingPlayer, the view's pregame projection) exists
	// the moment New returns exactly as the pre-choice engine left it; with
	// the choice pending, startPostDealSetup is deferred until the answer
	// (or the default at the first Advance) resolves the choice, because the
	// London mulligan round must open in the CHOSEN seat's turn order
	// (CR 103.5 reads the starting player). A terminal deal (nobody or one
	// survivor) and a toss winner the deal eliminated have no chooser and
	// run startPostDealSetup here, byte-identical to the pre-choice engine.
	choicePending := tossAsk && !e.G.Over && len(alive) > 1 && toss >= 0 && toss < len(e.G.Players) &&
		!e.G.Players[toss].Lost
	if choicePending {
		e.tossChoice = tossChoice{active: true, winner: start}
	}
	// The resolved toss is authoritative genesis state, not merely a Note or
	// the later TurnChange: opening-hand effects and Count$StartingPlayer run
	// before turn one. Fold it through events.Apply without appending a new
	// event: genesis is replayed from Config (including its seeded toss), and
	// preserving the historic event stream keeps recorded matches replayable.
	events.Apply(e.G, events.Event{Kind: events.StartingPlayerChange, Player: start})
	// The starting seat is the toss winner resolved over the survivors --
	// never seat 0 (the pre-toss assumption Ruling T22-f removed) and never a
	// seat the deal eliminated: an early seat that decked out during its own
	// opening draw (Over still false, since other seats remain, but that
	// seat's own Lost is true) must not receive turn 1. A player already out
	// of the game is simply skipped in turn order everywhere else (NextAlive,
	// priority); resolveToss is genesis's own equivalent for the very first
	// turn.
	if !e.G.Over && !choicePending {
		// CR 103.1's resolution, now that the deal has fixed the survivors:
		// beginTurn records start in its ordinary TurnChange. The resolved seat
		// is also state.Game.StartingPlayer now (folded above without a new
		// event: genesis is replayed from Config, including its seeded toss, so
		// preserving the historic event stream keeps recorded matches
		// replayable), which is what view's pregame projection and the
		// Count$StartingPlayer head read. With the choice pending this is
		// deferred to resolveStartingPlayer (rules/starting_player_choice.go).
		e.startPostDealSetup()
	}
	return e
}

// startPostDealSetup opens the pregame rounds between the opening deal and
// turn 1. The CR 903.4b commander colour-choice round runs FIRST when a
// qualifying commander exists (the choice is made "before the game begins",
// and the London mulligan round is also pregame); otherwise it hands straight
// to startMulliganOrTurn. Both genesis and the colour round's end call it, so
// a game with no qualifying commander is byte-identical to the pre-round
// engine.
func (e *Engine) startPostDealSetup() {
	if round := e.newColorRound(); len(round.asks) > 0 {
		e.coloring = true
		e.colorRound = round
		e.stepColorRound()
		return
	}
	e.startMulliganOrTurn()
}

// startMulliganOrTurn opens whichever round follows the colour round: the
// London mulligan round (Config.Mulligans > 0), the optional opening-hand
// effects round, or turn 1 directly.
func (e *Engine) startMulliganOrTurn() {
	if e.mulligans > 0 {
		// Ruling R-8.4: the London mulligan round lives between the deal
		// and turn 1. e.pregame makes step() dispatch to stepPregame
		// (rules/mulligan.go) instead of the ordinary turn steps; the
		// round's end calls beginTurn below. Over is already false (the
		// per-seat deck-out guard above returned early) -- a game that
		// ended during the deal never starts a round.
		// CR 103.5: the starting player declares first, then each other
		// player in turn order -- AliveFrom(e.G.StartingPlayer) is that
		// order, which is also beginTurn's seat at the round's end. The
		// opening-hand effects round runs after this round (a Gemstone
		// Caverns may not be used from a hand its owner later mulliganed
		// away), and an accepted Impatient Iguana there replaces the
		// recorded designation before turn one.
		e.pregame = true
		e.mulligan = newMulliganRound(e.G.AliveFrom(e.G.StartingPlayer), e.mulligans)
	} else {
		e.opening = e.newOpeningRound(e.G.StartingPlayer, 0)
		if len(e.opening.effects) > 0 {
			e.stepOpening()
			return
		}
		e.beginTurn(e.G.StartingPlayer)
	}
}

// finishTerminalGenesis finalizes a game whose opening deal left at most one
// survivor. The toss was already announced before the first shuffle (the
// pre-deal Note in New); terminal genesis therefore needs no additional Note.
// Ruling T22-e: nobody survived genesis is CR 104.4a's draw; one survivor is
// CR 104.2a's winner. GameOver remains the final genesis event for the host's
// persistence/replay burst boundaries.
func (e *Engine) finishTerminalGenesis() bool {
	if e.G.AliveCount() > 1 {
		return false
	}
	e.deferGameOver = false
	e.checkGameOver()
	return true
}

// resolveToss maps the pre-shuffle random determination onto the seats that
// survived the opening deal. The original candidate is already uniform over
// every configured seat; rejection sampling an eliminated candidate preserves
// uniformity over survivors without consuming another draw in ordinary games.
func (e *Engine) resolveToss(toss int, alive []state.PlayerID, seats int) (state.PlayerID, bool) {
	if toss < 0 || len(alive) == 0 || seats <= 0 {
		return 0, false
	}
	candidate := state.PlayerID(toss)
	for {
		for _, p := range alive {
			if p == candidate {
				return candidate, true
			}
		}
		candidate = state.PlayerID(e.rng.IntN(seats))
	}
}

// tossName is the identity the toss Note's text carries: the deck-identity
// Name, else "seat N". F3 (TestPlayerNamesDoNotReachTheChain) keeps the
// per-seat PlayerName -- a display name -- out of the event chain entirely,
// and the Note is event text, so it uses the same deck identity every other
// event text already carries. (view/describe.go's player label may prefer
// PlayerName; that is a view projection, not chain text.)
func tossName(g *state.Game, p state.PlayerID) string {
	pl := g.Players[p]
	if pl.Name != "" {
		return pl.Name
	}
	return "seat " + strconv.Itoa(int(p))
}

// emit is the engine's single mutation entry point. Task 20 inserts
// replacement effects ahead of logging and trigger discovery behind it:
// applyReplacements runs first (skipped, via applyingReplacement, while
// another replacement's own resolution is already in flight, which is what
// keeps a self-replacing event from looping), and if it substitutes the
// event, the original is discarded rather than logged -- the substitute's
// own emit (from inside effects.Resolve) already logged whatever needed
// logging. Otherwise the event is logged and folded into state exactly as
// before, and checkTriggers then looks for anything it just made true.
func (e *Engine) emit(ev events.Event) events.Event {
	// Task 15 protection (CR 702.16d/e): a Damage event dealt to a
	// protection-bearer by a source it is protected from is prevented -- the
	// damage never happens, reported as a Note rather than silently dropped.
	// Same for an Attach whose target is protected from the attachment -- CR
	// 702.16e: attachment points defer to protection before an Equip/Enchant
	// resolves onto a protected permanent. Both are checked BEFORE
	// replacement substitution: prevention is unconditional and must not be
	// handed to card text as if it had actually happened (and the recursive
	// emit for the Note re-enters cleanly because a Note matches neither
	// clause). The damage source is the published override when a
	// DamageSource$ emitter set one, else e.damaging; a zero source (no
	// source recorded) never suppresses a Damage event. A planeswalker's
	// Damage event is protected exactly like any other now -- its CR 306.8
	// loyalty conversion happens one fold later, in events.Apply, so a
	// prevented hit converts nothing. stat:CantPreventDamage (Spider-Punk)
	// overrides protection's own damage-prevention arm exactly like every
	// other prevention path, so the same cantPreventDamage gate applies here.
	if ev.Kind == events.Damage {
		src := e.inFlightDamageSource()
		protected := ev.Obj != 0 && e.protectedFrom(ev.Obj, src)
		if ev.Obj == 0 && int(ev.Player) < len(e.G.Players) {
			protected = e.playerProtectedFrom(ev.Player, src)
		}
		if src != 0 && protected && !e.cantPreventDamage(src, ev.Obj) {
			// Amount rides the stored Note (task dponce1): a prevention is a
			// game action a triggered ability can see, and Mode$
			// DamagePreventedOnce keys on these Notes' Amount.
			return e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
				Amount: ev.Amount, Text: "prevented: protection"})
		}
	}
	if ev.Kind == events.Attach && ev.Obj != 0 && len(ev.IDs) > 0 &&
		e.protectedFrom(ev.IDs[0], ev.Obj) {
		// Task 15 fix round 1 (Important I1): this clause is defence-in-depth.
		// Under the five registered colour protections there is NO reachable
		// path that fires it -- askTarget withholds a coloured Aura from ever
		// targeting a protected permanent (CR 702.16c) so no such Attach is
		// ever offered to resolve, and for Equip the legalTargets fizzle in
		// resolveTop fires first (the equipment's source is colourless, so a
		// colour-protected permanent was never going to be non-legal by
		// protection anyway). The clause stays because it is the engine's one
		// guard for the moment a future task registers type or "everything"
		// protection (or an Aura that enters the battlefield pre-attached, or
		// any Attach emit produced without a targeting step) makes a actually
		// protected permanent the direct object of an Attach; removing it now
		// would silently re-open that hole. Do not test it by driving emit
		// directly -- that would prove only that the if-lookup works, not that
		// a game state reaches it.
		return e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Text: "cannot attach: protected"})
	}
	if ev.Kind == events.Attach && ev.Obj != 0 && ev.Text == "attach to player" {
		if attaching := e.G.Obj(ev.Obj); attaching != nil && e.isAura(attaching) &&
			int(ev.Player) < len(e.G.Players) && e.playerProtectedFrom(ev.Player, ev.Obj) {
			return e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
				Text: "cannot attach: protected"})
		}
	}
	// Role-token exclusivity (the second sentence of every Role token's rules
	// text: "If you control another Role on it, put that one into the
	// graveyard."): a second Role token attaching to a bearer that already
	// carries one puts the old Role into its owner's graveyard BEFORE the new
	// Attach applies. The measured corpus makes the sweep unconditional: every
	// one of the 41 `TokenScript$ role_` carrier lines mints the Role with
	// `TokenOwner$ You` (or omits it, defaulting to the controller), so creator
	// and Role controller are always the same player and the controller-qualified
	// reading is vacuous. This lives here in emit -- beside the protection guard
	// above, the exact same pre-apply, re-entrant Attach interception -- because
	// EVERY attach path (effects/token.go's AttachedTo$ mint and
	// effects/attach.go's Attach SA) funnels through Engine.Emit. The sweep is a
	// plain MoveZone (NOT destruction: no replacement/regeneration path), whose
	// recursive emit lets "leaves the battlefield" triggers on the old Role fire
	// normally; zone slices are copied before iteration because the MoveZone
	// mutates the battlefield while we walk it (the attachmentSBAs discipline).
	if ev.Kind == events.Attach && ev.Obj != 0 && len(ev.IDs) > 0 {
		if attaching := e.G.Obj(ev.Obj); attaching != nil && e.isRole(attaching) {
			for _, p := range e.G.AliveFrom(0) {
				zone := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, p)...)
				for _, id := range zone {
					o := e.G.Obj(id)
					if o == nil || id == ev.Obj || o.AttachedTo != ev.IDs[0] || !e.isRole(o) {
						continue
					}
					e.emit(events.Event{Kind: events.MoveZone, Obj: id,
						From: state.ZBattlefield, To: state.ZGraveyard,
						Text: "another Role on it: the old Role goes to the graveyard"})
				}
			}
		}
	}
	// A CantPutCounter restriction swallows a counter placement outright
	// (task cantputcounter1): the placement never happens, so neither the
	// event nor any AddCounter replacement of it may run. This gate was
	// hoisted here, out of applyReplacementsDispatch, so it runs EVEN while a
	// replacement effect's own ReplaceWith$ body is resolving
	// (applyingReplacement): the guard that stops a replacement from
	// re-matching its own event must not also swallow the prohibition, or a
	// counter an "enters with N counters" body places slips past Solemnity
	// and friends (task addcounter1/2). Looking at it before the replacement
	// pass is harmless: applyReplacementsDispatch used to run it at its own
	// top, before any match was collected.
	//
	// Only a POSITIVE placement of a real counter is subject to the
	// restriction: a removal (Amount <= 0) is not a placement at all, and the
	// engine's own status markers (regeneration's Shield, the Deathtouched
	// mark) are not counters -- the same state.InternalCounterMarker exclusion the
	// AddCounter matcher keeps, so a "counters can't be put on it" static
	// cannot stop a regeneration shield or a removal.
	if (ev.Kind == events.CounterChange || ev.Kind == events.PlayerCounterChange) &&
		ev.Amount > 0 && !state.InternalCounterMarker(ev.Counter) {
		if e.PutCounterBlocked(ev.Counter, ev.Obj, ev.Player, ev.Kind == events.PlayerCounterChange) {
			return events.Event{}
		}
	}
	if e.applyingReplacement {
		ev = events.CarryAction(e.replAction, e.replReplaced, ev)
	}
	// Entry-counter staging (task agent-20260923T084704Z-b2386c25): an entry
	// whose characteristic counters compete under non-commuting AddCounter
	// replacements stages behind CR 616.1's order choice, BEFORE anything
	// folds -- no observer (an ETB trigger, an SBA, a chapter queue) may see
	// the un-replaced entry while the ask is outstanding. The pre-pass sits
	// here, before the replacement dispatch and the whole fold tail, so a
	// staged entry returns the same handled shape a parked replacement does
	// and the re-drive after the answer runs the ordinary emit exactly once.
	// A completed stage returns false and falls through: the fold below
	// consumes it (rules/entry_counters.go).
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield &&
		e.entryCounterOrderParks(ev) {
		return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "entry awaiting counter-replacement-order choice"}
	}
	// A replacement body's counter placement is a NEW event, not the event
	// whose replacement body is resolving. Give it its own AddCounter pass;
	// the rewritten event itself is folded below without another pass.
	if !e.applyingReplacement || ((ev.Kind == events.CounterChange || ev.Kind == events.PlayerCounterChange) && !e.counterReplacementFold) {
		replaced, handled := e.applyReplacements(ev)
		if handled {
			return replaced
		}
		// Not replaced, but possibly REWRITTEN in place (a DamageDone
		// ReplaceEffect body changed the amount): the returned event is what
		// gets logged, not the emit caller's copy.
		ev = replaced
	} else if e.redirectRecheck(ev) {
		replaced, handled := e.applyRedirectReplacements(ev)
		if handled {
			return replaced
		}
		ev = replaced
	}
	// Token replacement effects must settle before entry staging: they may
	// remove the mint or rewrite its script. Final token plans re-enter here
	// under applyingReplacement, so they skip rematching but still stage each
	// finalized mint before its TokenCreate/CardToken fold.
	if (ev.Kind == events.TokenCreate || ev.Kind == events.CardToken) &&
		e.entryCounterOrderParks(ev) {
		return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "entry awaiting counter-replacement-order choice"}
	}
	// CountersRemain is a departure property of the battlefield object. Tag the
	// final, replacement-adjusted MoveZone so events.Apply and replay preserve
	// the counters in the same fold. Hand and library remain explicit reset
	// destinations per the static's rules text.
	if ev.Kind == events.MoveZone && ev.To != state.ZHand && ev.To != state.ZLibrary {
		if o := e.G.Obj(ev.Obj); o != nil && o.Zone == state.ZBattlefield && e.countersRemainApplies(ev.Obj) {
			ev.Counter = events.MarkCountersRemainMove(ev.Counter)
		}
	}
	// DamageDone may rewrite the recipient through ReplaceEvent, while an
	// ordinary hit still needs its initial recipient form classified. Do this
	// after the complete replacement pass so both paths share one rule.
	if ev.Kind == events.Damage {
		e.recomputeInfectMarker(&ev)
		e.recomputeWitherMarker(&ev)
	}
	// CR 306.8's planeswalker loyalty exchange (and CR 120.3e's exception for
	// a permanent that is also a creature) is folded directly into this
	// Damage event by events.Apply below -- AddCounter("LOYALTY", ...) runs
	// in the same Apply call that would otherwise mark damage, so replay
	// derives it from the one logged Damage event and no separate
	// CounterChange is ever emitted for it.
	// LKI (CR 603.10 "look back in time") is captured HERE, before
	// events.Emit runs Apply and mutates the object -- a zone-change trigger
	// needs the object exactly as it was a moment ago (its counters, tapped
	// state, damage, controller, zone), not the reset state Move leaves it
	// in. See effects.Ctx.LKI.
	var lki *state.Object
	var lkiPower, lkiToughness int32
	var lkiPTValid bool
	switch ev.Kind {
	case events.MoveZone, events.Draw, events.PutOnStack, events.ControlChange:
		if o := e.G.Obj(ev.Obj); o != nil {
			cp := o.CloneDeep()
			lki = &cp
			if o.Zone == state.ZBattlefield && o.Face() != nil {
				lkiPower, lkiToughness = e.Power(o.ID), e.Toughness(o.ID)
				lkiPTValid = true
			}
		}
	case events.DoorUnlock:
		// CR 709.5: Mode$ FullyUnlock (rules/trigmatch_room.go) must tell a
		// real locked->unlocked transition from a repeated DoorUnlock on an
		// already-unlocked room (the latter no game action produces, but a
		// direct emit can). Apply flips Unlocked before this event's triggers
		// are matched, so the pre-fold flag has to ride the LKI snapshot.
		if o := e.G.Obj(ev.Obj); o != nil {
			cp := o.CloneDeep()
			lki = &cp
		}
	case events.CounterChange:
		// Vanishing's last-counter trigger must distinguish a real removal
		// from a redundant decrement at zero. Keep the pre-fold TIME count
		// alongside the existing Suspend zero-crossing check below.
		if ev.Amount < 0 && ev.Counter == "TIME" {
			if o := e.G.Obj(ev.Obj); o != nil {
				cp := o.CloneDeep()
				lki = &cp
			}
		}
	}
	departingSource, departingSourceLifelink, departingSourceController := e.captureSourceLifelinkLKI(ev)
	timeBefore := int32(0)
	if lki != nil && ev.Kind == events.CounterChange {
		timeBefore = lki.Counter("TIME")
	}
	// CR 310.11 (battle-defeated): the defeat feed below reads the defense
	// count BEFORE the move fold -- Apply's Move clears o.Counters as the
	// object leaves the battlefield, so a post-move read would call every
	// exiled battle defeated (a blink of a healthy Siege would pose the
	// transformed-cast offer too).
	defenseBefore := int32(0)
	if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield {
		if o := e.G.Obj(ev.Obj); o != nil {
			defenseBefore = o.Counter("DEFENSE")
		}
		// CR 608.2b/h departure boundary: this is the last moment a departing
		// target of the resolving chain still carries the counters the
		// look-back reads, so refresh the chain's Ctx.TargetCountersLKI HERE --
		// after the replacement pass settled the final move, before events.Apply
		// clears the counters. A chained effect that added or removed counters
		// earlier in the same resolution must be read as it was immediately
		// before the zone change, not as the resolution-start snapshot
		// (effects.Resolve's entry capture) recorded it.
		e.snapshotDepartingTargetCounters(ev.Obj)
	}
	stackLen := len(e.G.Stack)
	// Record only the final event after replacement selection. The object
	// snapshot must precede Apply, and unknown adder provenance is not a
	// match for either You or Player. The engine's own status markers (the
	// regeneration Shield, the Deathtouched lethal mark) ride a positive
	// CounterChange but are not counters a player PUT -- and a resolving
	// deathtouch damage ability has an actionCause, so without the marker
	// exclusion its emitted mark would be attributed to that controller and
	// make Count$CountersAddedThisTurn <Any> You Creature spuriously true.
	// The same exclusion rules/replacement.go's doubler gate keeps.
	if ev.Kind == events.CounterChange && ev.Amount > 0 && !state.InternalCounterMarker(ev.Counter) {
		if actor, ok := e.inFlightCounterAdder(); ok {
			if o := e.G.Obj(ev.Obj); o != nil {
				e.counterAddsThisTurn = append(e.counterAddsThisTurn, counterAddedThisTurn{
					actor: actor, kind: ev.Counter, amount: ev.Amount, object: o.CloneDeep(),
				})
			}
		}
	}
	var tokenMintWant state.ObjID
	if ev.Kind == events.TokenCreate || ev.Kind == events.CardToken || ev.Kind == events.CopyToken {
		tokenMintWant = e.G.NextID
	}
	var stackCopyMintWant state.ObjID
	if ev.Kind == events.StackCopy && e.stackCopyMintSink != nil {
		stackCopyMintWant = e.G.NextID
	}
	wasTapped := false
	if ev.Kind == events.Untap {
		if o := e.G.Obj(ev.Obj); o != nil && o.Zone == state.ZBattlefield {
			wasTapped = o.Tapped
		}
	}
	stored, _ := e.foldEntryMove(ev)
	e.expireClonesOnEvent(stored, wasTapped)
	// CR 310.10: every Battle whose recorded protector has just left the game
	// gets a fresh living opponent as its protector. PlayerLost is the one
	// funnel every departure passes through (life, poison, an empty-library
	// draw, a concession), and events.Emit has already marked the seat Lost
	// by the time this returns, so protectorOpponents reads the departure.
	// The re-derive emits a Choose "protector" event (it never poses a
	// decision), so it is safe to run here even mid-resolution.
	if stored.Kind == events.PlayerLost {
		e.rechooseDepartedBattleProtector(stored.Player)
	}
	e.publishTokenEntry(stored, tokenMintWant)
	if stackCopyMintWant != 0 && e.G.Obj(stackCopyMintWant) != nil {
		*e.stackCopyMintSink = append(*e.stackCopyMintSink, stackCopyMintWant)
	}
	if ev.Kind == events.CounterChange && ev.Amount < 0 && ev.Counter == "TIME" && timeBefore > 0 {
		// CR 702.62a/b (counterchoice1): the LAST time counter leaving a
		// suspended card by ANY route — the upkeep tick or a Clockspinning/
		// Amy-Pond-style removal mid-resolution — queues CR 702.62a's may-cast
		// offer; startSuspendedCast drains the queue at the next step(). The
		// ONE home replaces the upkeep tick's own append (which was the only
		// emitter before): a counter removed mid-resolution used to strand a
		// zero-TIME card in exile forever, because the tick skips a card
		// already at zero and nothing else ever re-offered the cast.
		if o := e.G.Obj(ev.Obj); o != nil && o.Zone == state.ZExile &&
			(o.CastFlags&state.FlagSuspend != 0 || o.SuspendGranted) && o.Counter("TIME") == 0 {
			e.suspendedCasts = append(e.suspendedCasts, ev.Obj)
		}
	}
	// CR 310.11 (battle-defeated): a Battle leaving the battlefield for exile
	// with no defense counters was exiled by the defeat SBA (rules/sba.go's
	// battleZeroDefense), so its owner's transformed-cast offer is queued
	// here -- the ONE home every route to that exile shares, so a replayed
	// game re-derives the queue from the same event. defenseBefore (read
	// above, pre-fold) is what makes "no defense counters" precise; FaceIdx 0
	// plus a second face are what the "cast it transformed" half needs.
	if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield && ev.To == state.ZExile &&
		defenseBefore == 0 {
		if o := e.G.Obj(ev.Obj); o != nil && o.Face() != nil && o.Face().IsBattle() &&
			o.FaceIdx == 0 && len(o.Card.Faces) > 1 && o.Counter("DEFENSE") == 0 {
			e.defeatedCasts = append(e.defeatedCasts, ev.Obj)
		}
	}
	// CR 702.90b (kw:Infect): the counters/poison an infect source's damage
	// is dealt in the form of are placed HERE, as real events emitted
	// through this same emit -- so the repl:AddCounter class (a Winding
	// Constrictor doubler, a CantPutCounter lock) and trig:CounterAdded see
	// the placement exactly like any other, and rules/sba.go's CR 704.5b
	// ten-poison loss reads a real PlayerCounterChange fold. The marker was
	// set by the emitter (rules/combat.go, rules/cast.go,
	// rules/resolution.go, effects/damage.go) after it checked HasKeyword on
	// the source; this conversion classifies the form off the event that
	// actually landed (the replaced/prevented hit never reaches here -- a
	// prevention is a Note), and the recipient-creature half of the marker
	// is the emitter's layer-accurate classification the fold reuses.
	if stored.Kind == events.Damage && stored.Amount > 0 &&
		(stored.Counter == "infect" || stored.Counter == "infect+creature") {
		e.convertInfectDamage(stored)
	}
	if stored.Kind == events.Damage && stored.Amount > 0 && stored.Counter == "wither+creature" {
		e.convertWitherDamage(stored)
	}
	// Game-long damage-by-source provenance (the_fallen, diseased_vermin):
	// every landed Damage event appends a DamageProvenance fact so the
	// wasDealtDamageThisGameBy player qualifier and the
	// wasDealtDamageByThisGame object predicate can answer Forge's game-long
	// record. This lives HERE, on the one post-fold tail, because every
	// emitter (effects/damage.go's riders, rules/combat.go's combat batch,
	// rules/cast.go, rules/resolution.go and the cleanup negatives) funnels
	// through emit -- no emitter file has to change. It reads `stored`, the
	// APPLIED event, so post-protection/post-replacement/post-redirect it
	// names the real recipient and the amount that actually landed; a
	// prevented hit is a Note and never reaches here, and a cleanup negative
	// is excluded by the Amount > 0 gate (exactly like the infect/wither
	// conversions above). The source is the same published override / e.damaging
	// reader emit's own protection guard uses; a zero source (no recorded
	// provenance) emits nothing rather than minting a false (0, recipient)
	// fact. The recipient is stored.Obj when nonzero (an object) else
	// stored.Player (a seat), encoded PlayerRef-style so seat 0 is
	// distinguishable from "no recipient".
	if stored.Kind == events.Damage && stored.Amount > 0 {
		if src := e.inFlightDamageSource(); src != 0 {
			var recipient state.ObjID
			if stored.Obj != 0 {
				recipient = stored.Obj
			} else {
				recipient = state.PlayerRef(stored.Player)
			}
			e.emit(events.Event{Kind: events.DamageProvenance, Obj: src,
				IDs: []state.ObjID{recipient}, Amount: stored.Amount})
		}
	}
	if len(e.turnsTaken) == len(e.G.Players) && e.turnsTakenEpoch == len(e.L.Events)-1 {
		if stored.Kind == events.TurnChange && int(stored.Player) < len(e.turnsTaken) {
			e.turnsTaken[stored.Player]++
		}
		e.turnsTakenEpoch++
	} else {
		e.turnsTaken = nil
		e.turnsTakenEpoch = 0
	}
	// The per-turn combat-damage ledger expires with the turn (CR 514.2's
	// "this turn" window): a TurnChange begins a fresh turn, so every hit
	// captured during the turn that just ended is no longer "this turn".
	if stored.Kind == events.TurnChange {
		e.combatHitsThisTurn = nil
		e.counterAddsThisTurn = nil
	}
	e.loop.observeFrom(stored, e.damaging, len(e.G.Objs))
	// setname.go: keep the layer-3 rename table the filter tier reads in step
	// with the board. Gated so a match with no SetName$ carrier pays one
	// branch.
	if e.setNameInPool {
		e.refreshRenames()
	}
	// layer4types.go: keep the layer-4 derived-type table the filter tier reads
	// in step with the board. Gated so a match with no type-changing carrier
	// pays one branch.
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
	if ev.Kind == events.StackCopy && len(e.G.Stack) > stackLen {
		copyID := e.G.Stack[len(e.G.Stack)-1]
		// StackCopy inherits the flat targets in events.Apply; preserve the
		// cast-time declaration split too. Current legality cannot reconstruct
		// which half owned an inherited target after the board has changed.
		if stages, ok := e.fuseTargets[ev.Obj]; ok && len(ev.IDs) == 0 {
			if e.fuseTargets == nil {
				e.fuseTargets = make(map[state.ObjID][][]state.Target)
			}
			cp := make([][]state.Target, len(stages))
			for i, targets := range stages {
				cp[i] = append([]state.Target(nil), targets...)
			}
			e.fuseTargets[copyID] = cp
		}
		if tc, ok := e.triggerContexts[ev.Obj]; ok {
			e.triggerContexts[copyID] = tc
		}
		if ef, ok := e.triggerEffectFrames[ev.Obj]; ok {
			e.triggerEffectFrames[copyID] = ef
		}
		if line, ok := e.triggerLines[ev.Obj]; ok {
			if e.triggerLines == nil {
				e.triggerLines = make(map[state.ObjID]cards.Trigger)
			}
			e.triggerLines[copyID] = line
			if svars, ok := e.triggerLineSVars[ev.Obj]; ok {
				if e.triggerLineSVars == nil {
					e.triggerLineSVars = make(map[state.ObjID]map[string]string)
				}
				e.triggerLineSVars[copyID] = svars
			}
		}
		if lki, ok := e.triggerLKI[ev.Obj]; ok {
			if e.triggerLKI == nil {
				e.triggerLKI = make(map[state.ObjID]triggerObjectLKI)
			}
			if lki.object != nil {
				cp := lki.object.CloneDeep()
				lki.object = &cp
			}
			e.triggerLKI[copyID] = lki
		}
		if link, ok := e.sourceLifelinkLKI[ev.Obj]; ok {
			if e.sourceLifelinkLKI == nil {
				e.sourceLifelinkLKI = make(map[state.ObjID]bool)
			}
			e.sourceLifelinkLKI[copyID] = link
		}
		if controller, ok := e.sourceControllerLKI[ev.Obj]; ok {
			if e.sourceControllerLKI == nil {
				e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
			}
			e.sourceControllerLKI[copyID] = controller
		}
		if lki := e.damageSourceLKI[ev.Obj]; lki != nil {
			if e.damageSourceLKI == nil {
				e.damageSourceLKI = make(map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI)
			}
			e.damageSourceLKI[copyID] = cloneDamageSourceLKI(lki)
		}
	}
	if ev.Kind == events.MoveZone {
		// CR 400.7: an object that changes zones is a new object with no
		// memory of having become tapped this turn.
		delete(e.tappedTurn, ev.Obj)
		// An exploited object's as-sacrificed snapshot is only meaningful
		// while the object is where the exploit left it; a later move (the
		// graveyard card exiled) retires it rather than letting the map grow
		// for the rest of the game.
		delete(e.exploitedLKI, ev.Obj)
	}
	if ev.Kind == events.MoveZone && ev.From == state.ZStack && ev.To != state.ZStack {
		delete(e.triggerContexts, ev.Obj)
		delete(e.triggerEffectFrames, ev.Obj)
		delete(e.triggerLines, ev.Obj)
		delete(e.triggerLineSVars, ev.Obj)
		delete(e.triggerLKI, ev.Obj)
		delete(e.sacrificedLKI, ev.Obj)
		delete(e.castExiled, ev.Obj)
		delete(e.castRevealed, ev.Obj)
		delete(e.fuseTargets, ev.Obj)
		delete(e.copyTargetStage, ev.Obj)
		delete(e.copyAnswerTargets, ev.Obj)
		delete(e.castSubTargets, ev.Obj)
		delete(e.tpCtlChooser, ev.Obj)
		delete(e.charmTargets, ev.Obj)
		delete(e.sourceLifelinkLKI, ev.Obj)
		delete(e.sourceControllerLKI, ev.Obj)
		delete(e.damageSourceLKI, ev.Obj)
	}
	if ev.Kind == events.MoveZone && lki != nil && lki.Zone == state.ZBattlefield {
		// ChangeZone's Duration$ UntilHostLeavesPlay (the Oblivion Ring /
		// Banisher Priest pattern): the exiling permanent has just left the
		// battlefield, so every card it exiled under that duration and that is
		// still in exile returns to the zone it was exiled from, under its
		// owner's control. The sweep runs BEFORE this leave event's own
		// triggers are matched, matching Forge's command semantics (the return
		// is not a triggered ability); the returned cards' own ETB triggers
		// are queued by their return move's emit. events.Move prunes the
		// marker entries when their object leaves exile by any other path, so
		// the sweep can never return a card whose exile was another effect's
		// business, and a card exiled again by something else after it was
		// once returned is equally out of reach.
		if o := e.G.Obj(ev.Obj); o != nil && o.Zone != state.ZBattlefield {
			e.sweepExileReturn(ev.Obj)
		}
	}
	if ev.Kind == events.MoveZone {
		// Effect-created continuous effects' move-driven lifetimes (the
		// ForgetOnMoved$/ExileOnMoved$ sweep) run after the move is applied
		// and before this event's triggers are checked, so a may-play grant's
		// remembered set is already pruned when anything downstream reads it.
		// EVERY move, not just a battlefield departure: the move the sweep
		// exists for is a may-play cast's Exile→Stack move — which rides
		// PutOnStack, the event kind a cast pushes with — and a remembered
		// card can also leave the ForgetOnMoved$ zone from the graveyard or
		// the hand.
		e.effectMoveSweep(ev)
		e.sweepEffectDelayed(ev)
	}
	if ev.Kind == events.PutOnStack {
		e.effectMoveSweep(ev)
		e.sweepEffectDelayed(ev)
	}
	if ev.Kind == events.CounterChange {
		// Effect-created continuous effects' counter-driven lifetime (task
		// vow1; ForgetCounter$): after a counter REMOVAL is applied, a
		// remembered card whose count of the named kind reached zero leaves
		// the effect's Remembered set -- Promise of Loyalty's "for as long
		// as it has a vow counter on it". Applied before this event's own
		// triggers are checked, the same timing effectMoveSweep keeps.
		e.effectCounterSweep(ev)
		e.sweepEffectDelayed(ev)
	}
	// Damage batch (CR 510.4, Forge dealAssignedDamage): DamageDealtOnce/
	// DamageDoneOnce latch once per damage BATCH. A Damage event arriving with
	// no batch already open (combat's damageStep and effects' dealDamage calls
	// open their own; see Host.BeginDamageBatch) is a batch of one -- its own
	// batch, opened and closed around the trigger check, so the Once modes
	// fire per event rather than per turn and the queued referent's amount is
	// already the batch total. A prevented Damage never reaches here (emit
	// returned the prevention Note above), and the deferred cast-trigger arm
	// skips the trigger check entirely, so neither needs a batch.
	onlyEventBatch := false
	if ev.Kind == events.Damage && !e.damageBatchOpen {
		e.openDamageBatch()
		onlyEventBatch = true
	}
	if ev.Kind == events.PutOnStack && e.deferCastTrigger {
		// CR 601.2i: the cast trigger must not fire at the up-front push
		// (601.2a), because the spell is not yet cast -- targets (601.2c) and
		// payment (601.2h) still lie ahead. Hold the event and its LKI so
		// payCast's fireDeferredCastTrigger re-walks it after payment.
		// Deferring here (rather than queueing the trigger and removing it
		// later) means the held event is never double-checked: the first
		// pass skipped it entirely, and exactly one later pass fires it.
		cp, lp := stored, lki
		e.deferredPush, e.deferredPushLKI = &cp, lp
	} else {
		if _, _, loss := lifeLoss(stored); loss && e.lifeLossBatchDepth > 0 {
			e.lifeLossBatch = append(e.lifeLossBatch, stored)
		}
		before := len(e.pendingTriggers)
		e.checkTriggers(stored, lki, lkiPower, lkiToughness, lkiPTValid)
		// A pushed spell proposal's own target choice (CR 601.2c): remember
		// which queue entries it produced so abortCast can drop them if the
		// cast is reversed (CR 733.1 -- see pendingCast.proposalTriggers).
		if stored.Kind == events.TargetsChosen && e.cast != nil && e.cast.pushed &&
			!e.cast.isAbility() && stored.Obj == e.cast.stackObj && len(e.pendingTriggers) > before {
			e.cast.proposalTriggers = append(e.cast.proposalTriggers, [2]int{before, len(e.pendingTriggers)})
		}
	}
	if onlyEventBatch {
		e.closeDamageBatch()
	}
	if stored.Kind == events.PlanarRoll {
		// CR 901.4 (task planar-verbs): a completed planar-dice roll's kept
		// results have their consequences — the planeswalk face walks the
		// roller to the next plane, the chaos face makes chaos ensue on the
		// roller's current plane. The record above is logged, so the nested
		// PlanarWalk/ChaosEnsues events follow it in the log deterministically.
		e.planarRollConsequences(stored)
	}
	if ev.Kind == events.Tap && !e.tapIsEntryState(ev) {
		// Recorded after the triggers above were matched, so a FirstTime$
		// trigger sees whether an EARLIER tap happened this turn.
		if e.tappedTurn == nil {
			e.tappedTurn = make(map[state.ObjID]int32)
		}
		e.tappedTurn[ev.Obj] = e.G.Turn
	}
	e.finishSourceLifelinkLKI(ev, departingSource, departingSourceLifelink, departingSourceController)
	// CR 702.163 ("Start your engines!", rules/speed.go): a loss may raise
	// every eligible opponent's speed (if they have any), and a Start your
	// engines! permanent's battlefield entry starts a speed-less
	// controller's speed at 1. Checked on the FOLDED event, after
	// checkTriggers, so the gain event follows everything the loss itself
	// caused -- and both checks are inert for every other event. The loss
	// reaches here two ways: an explicit LifeChange with a negative amount
	// (life payment, "each player loses N life"), and a player D_DAMAGE --
	// combat damage and spell/ability damage fold straight to the life
	// total (events.Apply's Damage case) without a LifeChange, and any
	// Damage event that reaches emit has already been through prevention
	// (a prevented hit is a Note, never a Damage), so a positive player
	// Damage event here IS the life loss the rule reads.
	if (ev.Kind == events.LifeChange && ev.Amount < 0) ||
		(ev.Kind == events.Damage && ev.Obj == 0 && ev.Amount > 0 &&
			ev.Counter != "infect") {
		e.checkSpeedGain(ev)
	}
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
		e.checkSpeedStart(ev.Obj)
	}
	// Ascend (CR 702.131a): the city's blessing's continuous re-check. A
	// battlefield entry (the ordinary MoveZone), a token mint (TokenCreate/
	// CardToken -- Apply mints those without a MoveZone event) or a control
	// transfer can each push a seat's permanent count over ten; the scan
	// only emits for an unblessed seat that newly qualifies, so every other
	// event reaching here is inert (rules/ascend.go).
	if (ev.Kind == events.MoveZone && ev.To == state.ZBattlefield) ||
		ev.Kind == events.TokenCreate || ev.Kind == events.CardToken ||
		ev.Kind == events.ControlChange {
		e.checkBlessingGrants()
		e.checkEnduringStoryGrants()
	}
	// E2: any genuinely state-changing event proves the game is making
	// progress, so it clears the held-out cast suppression (suppressedCast,
	// see engine.go): a declined card's option comes back the moment the
	// game does anything else, which is exactly when ending the current
	// priority window (a spell resolves, a step or turn changes) and any
	// mana/board change both land. The four kinds a declined-Delve abort
	// emits -- a Priority regrant, the KChoose decision bookkeeping, and the
	// abort Note -- are excluded, so suppression survives only as long as
	// NOTHING else is happening, which is precisely the no-progress-interval
	// it is there to bound.
	if ev.Kind != events.Priority && ev.Kind != events.DecisionAsk &&
		ev.Kind != events.DecisionMade && ev.Kind != events.Note {
		e.suppressedCast = nil
		e.castAborts = nil
		e.inertHeldOut = nil
		// CR 611.2b: a "for as long as" control effect ends the moment its
		// condition stops holding, not at the next state-based check.
		e.expireControl(controlOnEvent)
		// A GainControl$ static (Mind Control) is realized the same way:
		// ending ran above (a static grant's grantEnded reads the fresh
		// wanted set), this registers the transfers the live scan newly
		// wants. Both are no-ops unless such a static is in play.
		e.reconcileControlStatics()
	}
	if stored.Kind == events.MoveZone && stored.To == state.ZBattlefield {
		e.finishLandPlay(stored.Obj)
	}
	return stored
}

// sweepExileReturn implements ChangeZone's Duration$ UntilHostLeavesPlay
// return half: the object named by source has just left the battlefield, so
// every card it exiled under that duration and that is still in exile moves
// back to the zone it was exiled from, under its owner's control (events.Move
// gives a battlefield re-entry its owner's control, and the returned card's
// own ETB triggers queue through its return move's own emit). The marker list
// is snapshotted first: the return moves prune it underneath the loop. Entry
// order -- the order the exiles happened in -- is the return order,
// deterministic.
func (e *Engine) sweepExileReturn(source state.ObjID) {
	src := e.G.Obj(source)
	if src == nil || len(src.ExileReturn) == 0 {
		return
	}
	pending := append([]state.ExileReturnEntry(nil), src.ExileReturn...)
	for _, entry := range pending {
		o := e.G.Obj(entry.Obj)
		if o == nil || o.Zone != state.ZExile {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: entry.Obj, From: state.ZExile, To: entry.From})
	}
}

// captureSourceLifelinkLKI preserves CR 608.2h's pre-departure derived
// lifelink state for every independent ability of the leaving permanent that
// already exists on the stack or in the pending-trigger queue. It is called
// immediately before every rules-layer events.Emit path that can fold a real
// MoveZone, including Updated replacement paths that deliberately bypass
// Engine.emit to avoid matching the same replacement twice. Walking ordered
// slices rather than a map keeps this bookkeeping incapable of changing event
// order. A self-sacrifice activation is not minted until after its departure;
// payCast captures that one sibling before paying the cost.
func (e *Engine) captureSourceLifelinkLKI(ev events.Event) (bool, damageKeywordLKI, state.PlayerID) {
	if ev.Kind != events.MoveZone || ev.From != state.ZBattlefield ||
		ev.To == state.ZBattlefield {
		return false, damageKeywordLKI{}, 0
	}
	// A destruction batch's own pre-state wins (rules.Engine.BatchDepartures,
	// effects.Host): a later batch member must read the lifelink state from
	// immediately before the FIRST departure (CR 603.10a/702.15c -- the
	// destroy-all over a lifelink-granting Equipment and its bearer), not
	// the live layers an earlier member's departure already stripped. The
	// entry is consumed here; BatchDepartures rebuilds the map on its next
	// call, so a straggler for an object that never left cannot outlive one
	// effect call.
	kw, batched := e.batchDamageKeywords[ev.Obj]
	if batched {
		delete(e.batchDamageKeywords, ev.Obj)
	} else {
		kw = e.damageKeywordsOf(ev.Obj)
	}
	controller := e.G.Obj(ev.Obj).Controller
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Ability != nil && o.Source == ev.Obj {
			if e.sourceLifelinkLKI == nil {
				e.sourceLifelinkLKI = make(map[state.ObjID]bool)
			}
			if e.sourceControllerLKI == nil {
				e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
			}
			e.sourceLifelinkLKI[id] = kw.lifelink
			e.sourceControllerLKI[id] = controller
		}
		e.captureNamedDamageSourceLKI(id, ev.Obj, kw, controller)
	}
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].Source == ev.Obj {
			e.pendingTriggers[i].Ctx.SourceLifelinkLKI = kw.lifelink
			e.pendingTriggers[i].Ctx.SourceLifelinkLKIValid = true
			e.pendingTriggers[i].Ctx.SourceControllerLKI = controller
			e.pendingTriggers[i].Ctx.SourceControllerLKIValid = true
		}
		e.capturePendingNamedDamageSourceLKI(&e.pendingTriggers[i].Ctx, ev.Obj, kw, controller)
	}
	return true, kw, controller
}

// finishSourceLifelinkLKI attaches the same pre-departure snapshot to a
// dies/leaves trigger that the event itself just queued. Such a trigger did not
// exist during captureSourceLifelinkLKI's pre-event walk.
func (e *Engine) finishSourceLifelinkLKI(ev events.Event, departing bool, kw damageKeywordLKI, controller state.PlayerID) {
	if !departing {
		return
	}
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].Source == ev.Obj {
			e.pendingTriggers[i].Ctx.SourceLifelinkLKI = kw.lifelink
			e.pendingTriggers[i].Ctx.SourceLifelinkLKIValid = true
			e.pendingTriggers[i].Ctx.SourceControllerLKI = controller
			e.pendingTriggers[i].Ctx.SourceControllerLKIValid = true
		}
		e.capturePendingNamedDamageSourceLKI(&e.pendingTriggers[i].Ctx, ev.Obj, kw, controller)
	}
}

func damageSourceLKIOf(kw damageKeywordLKI, controller state.PlayerID) effects.DamageSourceLKI {
	return effects.DamageSourceLKI{Lifelink: kw.lifelink, Infect: kw.infect,
		Wither: kw.wither, Deathtouch: kw.deathtouch, Controller: controller}
}

func (e *Engine) captureNamedDamageSourceLKI(stack, source state.ObjID, kw damageKeywordLKI, controller state.PlayerID) {
	if e.damageSourceLKI == nil {
		e.damageSourceLKI = make(map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI)
	}
	if e.damageSourceLKI[stack] == nil {
		e.damageSourceLKI[stack] = make(map[state.ObjID]effects.DamageSourceLKI)
	}
	e.damageSourceLKI[stack][source] = damageSourceLKIOf(kw, controller)
}

func (e *Engine) capturePendingNamedDamageSourceLKI(ctx *effects.Ctx, source state.ObjID, kw damageKeywordLKI, controller state.PlayerID) {
	if ctx.DamageSourceLKI == nil {
		ctx.DamageSourceLKI = make(map[state.ObjID]effects.DamageSourceLKI)
	}
	ctx.DamageSourceLKI[source] = damageSourceLKIOf(kw, controller)
}

func cloneDamageSourceLKI(in map[state.ObjID]effects.DamageSourceLKI) map[state.ObjID]effects.DamageSourceLKI {
	out := make(map[state.ObjID]effects.DamageSourceLKI, len(in))
	for id, lki := range in {
		out[id] = lki
	}
	return out
}

func (e *Engine) Pending() *decision.Decision { return e.pending }

// EnsurePaymentActions lazily builds the payment extension for the current
// priority decision. It is a pure derived read: it emits no event and does
// not advance sequence or RNG. The built marker also caches an empty result.
func (e *Engine) EnsurePaymentActions() []decision.PaymentAction {
	d := e.pending
	if d == nil || d.Kind != decision.KPriority {
		return nil
	}
	if !d.PaymentActionsBuilt {
		// The pending Options are the list BaseOptionIndex indexes: ask built
		// them with the same legalActions walk, so the builder reuses them
		// instead of walking again.
		gen := e.derivedMemoGen
		// The build is a pure read of the board ask's offer walk read a
		// moment ago: when that walk's memo tail is still exact (no Submit,
		// no other scope, only ask's DecisionAsk marker logged since --
		// derivedmemo.go), RESUME its generation so the build's own walks
		// are served the Derived results and walk caches ask already built
		// instead of re-deriving the whole board. A dead tail opens a fresh
		// generation exactly as the build's own scope always did.
		e.BeginDerivedReads()
		actions := e.paymentActionsForPriority(d.Player, d.Seq, d.Options)
		e.EndDerivedReads()
		d.PaymentActions = (&decision.Decision{PaymentActions: actions}).Clone().PaymentActions
		d.PaymentActionsBuilt = true
		// A builder walk performs derived reads in its own memo generation.
		// Make that completed read the resumable tail so a later BoardSeat
		// build can still use BeginDerivedReads without reopening the walk.
		// A build that opened no walk (a declined pool) leaves ask's own
		// tail, which is still exact, in place.
		if e.derivedMemoGen != gen {
			e.recordDerivedMemoTail(d)
		}
	}
	return d.PaymentActions
}

// seatFacingName is the seat-facing identity for client-facing prompt and
// option-label text (the priority prompt, the keep/mulligan prompt, the
// attacker, cumulative-upkeep and target option labels). PlayerName is
// supplied by the table and identifies a human even when two players chose
// the same deck; Name is the deterministic fallback for bots or callers
// without display names. Decision prompts and option labels are NOT chain
// content (rules/engine.go's ask emits only DecisionAsk{Kind}), so
// composing them from PlayerName moves no chain head — but event text must
// stay on Name (the F3 invariant, rules/playername_test.go). The final
// fallback is defensive: decision seats originate from AliveFrom, but
// malformed state must not panic while constructing a client decision.
func seatFacingName(g *state.Game, p state.PlayerID) string {
	if g != nil && int(p) < len(g.Players) {
		if name := g.Players[p].PlayerName; name != "" {
			return name
		}
		if name := g.Players[p].Name; name != "" {
			return name
		}
	}
	return fmt.Sprintf("seat %d", p)
}

// BeginLibrarySearch marks the start of one library search's execution: the
// searcher's repl:Moved FoundSearchingLibrary$ replacements apply to exactly
// the moves the search emits.
func (e *Engine) BeginLibrarySearch(owner state.PlayerID) {
	e.searchDepth++
	e.searchingBy = owner
}

// EndLibrarySearch closes the innermost search scope.
func (e *Engine) EndLibrarySearch() {
	if e.searchDepth > 0 {
		e.searchDepth--
	}
	if e.searchDepth == 0 {
		e.searchingBy = 0
	}
}

// searchControlRedirect applies the ControlOpponentsSearchingLibrary$ static
// family (Opposition Agent's "You control your opponents while they're
// searching their libraries"): the search pick's decision is posed to the
// static's controller instead of the searching player. Scope: the pick (and
// any later ask the search flow poses) — the "control" of the searched
// player's every action is the wider grant Forge models; this build
// redirects the decisions the search itself asks, which is what a
// resolution can observe. The first static in activeStatics' deterministic
// APNAP order wins; an Affected$ spec that does not match the searching
// player leaves the static inert.
func (e *Engine) searchControlRedirect(d *decision.Decision) {
	if d.ResumeKind != "search" {
		return
	}
	for _, sv := range e.activeStatics("Continuous") {
		if strings.TrimSpace(sv.Params["ControlOpponentsSearchingLibrary"]) != "You" {
			continue
		}
		if sv.Controller == d.Player {
			continue
		}
		if spec := sv.Params["Affected"]; spec != "" &&
			!effects.MatchesPlayerSpec(e.G, spec, d.Player, sv.Controller) {
			continue
		}
		d.Player = sv.Controller
		return
	}
}

func (e *Engine) GetCurrentEffectFrame() effects.EffectFrame {
	return e.currentEffectFrame
}

func (e *Engine) SetCurrentEffectFrame(frame effects.EffectFrame) {
	e.currentEffectFrame = frame
}

func (e *Engine) ask(d *decision.Decision) {
	// CR 903.9 ordering: a commander's zone change parked mid-chain asks its
	// owner at once (parkCommanderZoneMove), but the chain that parked it
	// keeps running -- Path to Exile's exile parks Rakdos, then the same
	// resolution's "its controller may search" poses its own choice. Posing
	// that choice here would OVERWRITE the unanswered commander-zone ask: the
	// parked move is never emitted, the commander stays on the battlefield,
	// and every later park of it is dropped by the queue's dedup (the
	// botbench Phyrexian Altar livelock, seed 9702). The later ask instead
	// waits behind the owner's answer and is posed by Submit once that
	// answer has been applied (drainDeferredAsks). Only a DIFFERENT decision
	// is deferred: a second commander-zone park never reaches ask while one
	// is pending (parkCommanderZoneMove queues it on cmdZone).
	if e.pending != nil && e.pending.Kind == decision.KCommanderZone && d.Kind != decision.KCommanderZone {
		e.deferredAsks = append(e.deferredAsks, d)
		return
	}
	e.searchControlRedirect(d)
	// Empty-answer-only tripwire (the class the Squadron Hawk fail-to-find
	// search wedged): a decision whose ONLY legal answer is the empty one
	// (Min 0 with Max 0, or no options at all) can never be answered
	// differently by any seat, so posing it strands the game on an ask a
	// client has no control to send. Every asking primitive in effects goes
	// through effects.Ask, which refuses to post the shape and resolves it
	// silently instead; this boundary guard is what fails a test loudly if
	// any construction site -- here or a future one -- ever posts one
	// anyway. Panic rather than quietly fixing: by the time a decision
	// reaches ask the asking caller has already chosen its resolution path,
	// and silently swallowing it here would leave the caller's suspended
	// half-resolution dangling.
	// One mid-resolution decision at a time (the structural guard for the
	// overwrite class findings-sol4 proved on the life-replacement draw
	// loop): a caller reaching ask while a SUSPENDED RESOLUTION is awaiting
	// its answer would overwrite e.pending and orphan that resolution's
	// ask -- no seat can ever answer it, and the surviving ask's answer is
	// applied to the wrong resolution. Every guarded caller checks
	// e.pending/e.Suspended() before asking (effDraw's cursor loop,
	// lifeReplacementDraw's park, advanceStep's draw-step return, the
	// replacement-choice queue's askNextReplacementChoice, DrawFor's
	// suspended degrade); a caller that does not is a bug of exactly the
	// class those guards exist for. Panic, the same stance as the two
	// checks below: the host crashes the match loudly rather than shipping
	// a log with a decision nobody can answer.
	//
	// A pending decision with e.resume == nil is deliberately NOT a panic,
	// and neither is a pose with e.resume != nil but e.pending == nil: the
	// replacement-order flow (handleReplacement) parks its resume point
	// while it finishes the parked event's remaining work -- a combat pass's
	// completeCombatPass then poses the round's priority with e.pending nil,
	// nothing is displaced, and the parked frame resumes when the engine
	// returns to it. What the guard exists for is the OVERWRITE: an ask
	// displacing a decision a seat has not answered yet. In engine flow
	// nothing emits while such a decision is outstanding (Advance is parked
	// on it and handle runs only after Submit cleared it), but the test
	// probes drive e.emit directly while a setup priority ask is pending,
	// and an emit that poses an ask is then the probe's intent -- the
	// priority ask it displaces is re-granted by the same Submit tail. That
	// displacement is engine-unreachable and probe-owned.
	if e.resume != nil && e.pending != nil {
		panic(fmt.Sprintf("rules: ask overwrote a suspended resolution's pending decision (%s, seat %d) with %s for seat %d",
			e.pending.Kind, e.pending.Player, d.Kind, d.Player))
	}
	if effects.OnlyEmptyAnswer(d) {
		panic(fmt.Sprintf("rules: decision %s for seat %d posed with only the empty answer legal (Min %d Max %d, %d options) -- asking primitives must resolve this shape silently (effects.Ask), never post it",
			d.Kind, d.Player, d.Min, d.Max, len(d.Options)))
	}
	// Option.Index/position identity (finding bi). Every decision that can
	// reach a seat flows through ask -- ask is what sets d.Seq and e.pending,
	// so a decision that skipped it is not pending and no seat can answer it
	// -- which makes this the ONE place the invariant is enforced for every
	// construction site at once, including the sites that never call
	// decision.New (all but mulligan's two build the struct literal directly;
	// effects' two mid-resolution asks reach e.pending through Engine.Ask,
	// which calls back into ask below). A mis-indexed list is a programming
	// error, not a bad client answer: Chosen resolves an intent by position,
	// so an option whose Index has drifted off its slot makes the engine
	// resolve a different option than the client named, silently. Panic here
	// rather than return an error because an error hands the caller the
	// choice to swallow it and ship the broken Decision to a seat -- exactly
	// the silent wrong-option failure the invariant exists to make
	// unrepresentable.
	for i := range d.Options {
		if d.Options[i].Index != i {
			panic(fmt.Sprintf("rules: decision option %d has Index %d, want position %d (%s)",
				i, d.Options[i].Index, i, d.Kind))
		}
	}
	d.Seq = uint64(len(e.L.Events))
	// Cache the ordinary offer-walk tail before any opt-in consumer requests
	// the separate payment extension. EnsurePaymentActions may run later, after
	// this ask has returned.
	if d.Kind == decision.KPriority {
		e.recordDerivedMemoTail(d)
	}
	// pendingMintSink records the named mint collector (tokenMintSinkID) this
	// pose runs under, so an as-enters election parked here from inside a
	// resolving DB$ Token's mint emit carries that collector to its answer
	// (rules/turn.go's election arms re-emit the parked entry under
	// withMintSink with it). A pose outside any collector scope records 0 and
	// the arms run unchanged.
	e.pendingMintSink = e.tokenMintSinkID
	e.emit(events.Event{Kind: events.DecisionAsk, Player: d.Player, Text: string(d.Kind)})
	e.pending = d
}

// decisionMadeText is the DecisionMade event text, byte-identical to
// fmt.Sprintf("%s:%v", kind, choices) ("priority:[0 3]") -- the text is
// hash-chained, so its bytes are fixed -- built without fmt's reflection and
// boxing, since every Submit pays it.
func decisionMadeText(kind decision.Kind, choices []int) string {
	var sb strings.Builder
	sb.Grow(len(kind) + 3 + 4*len(choices))
	sb.WriteString(string(kind))
	sb.WriteString(":[")
	var num [20]byte
	for i, c := range choices {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.Write(strconv.AppendInt(num[:0], int64(c), 10))
	}
	sb.WriteByte(']')
	return sb.String()
}

// decisionMadePaymentText is the planned-cast extension of decisionMadeText.
// The legacy spelling is deliberately left entirely alone: these bytes are
// chain-bound.  Action and plan are full canonical V1 digests, so this suffix
// binds the submitted witness without making labels or planner order part of
// replay history.
func decisionMadePaymentText(kind decision.Kind, choices []int, payment *decision.PaymentSelection) string {
	text := decisionMadeText(kind, choices)
	if payment == nil {
		return text
	}
	return text + ";payment:" + payment.ActionID + ":" + payment.Plan.ID
}

// drainDeferredAsks poses the front decision ask deferred behind a
// commander-zone choice, once nothing is pending. A deferred decision is
// posed through ask exactly as it would have been, so its DecisionAsk event,
// Seq and any search-control redirect reflect the moment it is actually put
// to a seat.
func (e *Engine) drainDeferredAsks() {
	for e.pending == nil && len(e.deferredAsks) > 0 {
		d := e.deferredAsks[0]
		e.deferredAsks = e.deferredAsks[1:]
		if len(e.deferredAsks) == 0 {
			e.deferredAsks = nil
		}
		e.ask(d)
	}
}

// Advance runs engine work until a decision is required or the game ends.
// A still-pending CR 103.1 winner-chooses choice (rules/starting_player_choice.go)
// is defaulted HERE, at the loop head -- never inside step(), which the
// resolution machinery calls mid-game; a hand-built mid-game engine must
// never find genesis work waiting for it.
func (e *Engine) Advance() {
	if e.tossChoice.active && e.pending == nil {
		e.resolveTossChoiceDefault()
	}
	for !e.G.Over && e.pending == nil {
		e.step()
	}
}

// Submit applies a client's answer. Anything the engine did not offer is
// rejected, which is what keeps the client rules-ignorant.
func (e *Engine) Submit(in decision.Intent) error {
	if e.derivedMemoDepth != 0 {
		panic("rules: Submit inside a Derived memo scope (BeginDerivedReads promises a pure read)")
	}
	if e.G.Over {
		return fmt.Errorf("game is over")
	}
	d := e.pending
	if d == nil {
		return fmt.Errorf("no decision pending")
	}
	if in.Payment != nil && d.Kind == decision.KPriority {
		e.EnsurePaymentActions()
	}
	if err := d.Validate(in); err != nil {
		return err
	}
	if d.Kind == decision.KAttackers {
		// Ruling m34: the KAttackers option list offers every (attacker,
		// defender) pair, so an intent naming the same creature twice --
		// against two different defenders -- passes Validate's per-index
		// checks while declaring it attacking two players (CR 506.2).
		// Reject before the intent is recorded and the decision consumed,
		// so the pending attackers decision survives for a legal answer.
		if err := e.validateAttackers(d, in); err != nil {
			return err
		}
	}
	if d.Kind == decision.KBlockers {
		// CR 509.1a: the blockers option list similarly offers every legal
		// (blocker, attacker) pair. Reject choosing the same ordinary blocker
		// against multiple attackers while preserving the pending decision.
		if err := e.validateBlockers(d, in); err != nil {
			return err
		}
	}
	if d.Kind == decision.KChoose {
		// The cast flow's Convoke/Harmonize announcement (convokeAsk): the
		// static option list cannot express "only while the outstanding
		// cost can still absorb the contribution", so an over-selection
		// (two white creatures for one {W}) passes Validate's per-index and
		// group checks. Reject it here, before the intent is recorded and
		// the pending decision consumed, so a legal subset can be
		// resubmitted -- the same preserve-and-reject shape as
		// validateAttackers above.
		if err := e.validateCastContributions(d, in); err != nil {
			return err
		}
		// ShareLandType$ True (Myriad Landscape): a hidden-library search's
		// answer must name cards that all share one land type -- the option
		// list cannot express the pairwise constraint, so an answer naming
		// e.g. a Forest and a Mountain is rejected and the pending decision
		// survives for a legal (or smaller) answer. Single-card answers are
		// trivially legal.
		if err := e.validateSearch(d, in); err != nil {
			return err
		}
	}
	if d.Kind == decision.KPriority {
		if in.Payment != nil {
			action, ok := paymentActionFor(d, in.Payment.ActionID)
			if !ok {
				return fmt.Errorf("payment action is not offered") // defensive: Decision.Validate already checked.
			}
			if err := e.ValidateCastPayment(in.Player, action.Cast, in.Payment.Plan); err != nil {
				return err
			}
		}
		// A priority answer whose handler would no-op at its first guard is
		// rejected before it is recorded (rules/priority_guard.go), so a
		// stale or mis-offered option errors instead of spinning.
		if in.Payment == nil {
			if err := e.validatePriorityChoice(d, in); err != nil {
				return err
			}
		}
	}
	if e.L.Intents == nil && e.intentBuf != nil {
		// A recycled intent array (Config.Spare) backs the log from its first
		// intent on; Log.Clone caps Intents, so no clone ever shares its
		// spare capacity.
		e.L.Intents, e.intentBuf = e.intentBuf, nil
	}
	// The caller owns its intent. Keep a private witness before it becomes
	// replay history, so a client-side mutation after Submit cannot alter it.
	in = decision.CloneIntent(in)
	e.L.Intents = append(e.L.Intents, in)
	e.emit(events.Event{Kind: events.DecisionMade, Player: in.Player,
		Text: decisionMadePaymentText(d.Kind, in.Choices, in.Payment)})
	e.pending = nil
	if in.Payment != nil {
		e.paymentStats.recordPlannedSubmission()
		action, _ := paymentActionFor(d, in.Payment.ActionID)
		// Match the ordinary cast priority action exactly, then enter the same
		// cast transaction.  The plan is only an immutable payment continuation;
		// it never represents a second casting implementation.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginCastWithPayment(in.Player, decision.Option{Kind: "cast", Obj: action.Cast.Object}, in.Payment)
	} else {
		e.handle(d, in)
	}
	// A decision posed while a commander-zone choice was outstanding waited
	// behind it (ask's CR 903.9 arm); pose it now that the answer landed,
	// before anything below can treat the engine as idle and advance.
	e.drainDeferredAsks()
	// A mana ability whose cost payment posed a decision (the CR 903.9
	// commander-zone choice for a sacrificed commander) resolves its mana
	// effect once that decision -- and any it handed on to -- is answered.
	if e.pending == nil && e.manaAfterCost != nil {
		e.resumeManaAfterCost()
	}
	// A CR 616.1 competition that arose while THIS decision was outstanding
	// was parked on the queue without an ask (poseLifeReplacementChoice's
	// queued arm, poseDamageReplacementChoice's multi-recipient batch, the
	// AddCounter/token/Updated poses): ask it now, before anything else
	// reads the parked event's unresolved state. A handler that already
	// asked (handleReplacement's own tails) set pending again, and this
	// drain is inert for it.
	if e.pending == nil && !e.Suspended() {
		e.askNextReplacementChoice()
	}
	// A CR 708.6 turn-face-up special action whose cost events parked on a
	// decision (any kind: commander zone, replacement order, madness, an ask
	// inside a replacement body) finishes paying and turns face up once every
	// such decision has landed (rules/morph_turnup.go).
	e.resumeTurnUpAfterCost()
	// An opening-hand round parked behind a decision its own effect posed
	// (an "as this enters" choice of a card beginning the game on the
	// battlefield) steps on now that the engine is idle again.
	e.resumeOpening()
	// CR 704.4: nobody receives priority in the middle of a resolution. A
	// handler may have resumed an effect only far enough to pose another
	// mid-resolution decision; in that case state-based actions wait until
	// the resolution finishes. The answer that finishes it clears resume, so
	// this same boundary performs the deferred check before Advance can grant
	// priority.
	//
	// CR 704.3: state-based actions are checked only when a player would
	// receive priority, and CR 601.2 explicitly withholds priority for the
	// whole cast: a mana window (CR 601.2g) re-enters payCast for each mana
	// ability activated, so an activation that drops the payer to 0 or less
	// (Ancient Tomb, a pain land, a pay-life source) leaves e.cast set with
	// the cast still mid-payment. The loss must wait for the priority
	// boundary the completed cast reaches, never fire between payment steps --
	// otherwise a manual window that taps the costly source first is
	// unpayable at 1 life although the same window in reverse order
	// (TestManaPaymentContinuesBelowZeroLife) settles. A pending activation's
	// cost payment reuses this same pendingCast flow, so e.cast != nil covers
	// both. The settled cast clears e.cast (payCast's tail) and the abort
	// path clears it too, so the check runs at the boundary either way.
	if !e.Suspended() && e.cast == nil {
		e.checkStateBased()
	}
	e.Advance()
	return nil
}

// drawCard draws for the turn structure, sharing effects.DrawFor with the
// Draw primitive so the draw step and a card that says "draw a card" can
// never disagree about what drawing means.
//
// Ruling T22-d: this used to call checkGameOver alone -- correct as far as
// it went (an empty-library draw is itself a loss, via the PlayerLost
// DrawFor emits directly), but it skipped destroyLethalDamage and, now,
// checkLoseConditions' own permanent-removal sweep. checkStateBased runs
// both of those in addition to checkGameOver, so a player who decks out
// here has their battlefield cleaned up the same way one who hits 0 life
// does, rather than only on whatever later checkStateBased call happens to
// come next.
func (e *Engine) drawCard(p state.PlayerID) {
	effects.DrawFor(e, p)
	e.checkStateBased()
}

// cardsKeywordHead lets layers.go strip a keyword's parameters ("Equip:2" ->
// "Equip") without importing cards itself for one call.
func cardsKeywordHead(k string) string { return cards.KeywordHead(k) }
