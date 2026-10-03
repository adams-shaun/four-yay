package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// engineScratch groups the Engine's per-walk and per-emit scratch state
// that a clone deliberately leaves zero. It is embedded by value in Engine
// (rules/engine_struct.go), so every field keeps its documented contract
// comment and every existing e.<field> access keeps compiling unchanged
// through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineScratch struct {
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
	// activeStaticsScan records the lists the last fused activeStatics scan
	// walked (static_scan_reuse.go). Pure scratch: Clone copies none.
	activeStaticsScan staticScanRec
	// actIndex is the incremental activation-count folds
	// (activation_count_index.go). Pure scratch: Clone copies none.
	actIndex activationIndex
	// boardScanBuf is the printed board scan's gathered zone lists
	// (static_scan_reuse.go: gatherBoardScan). Pure scratch.
	boardScanBuf  []boardScanList
	mayPlaysCache []mayPlaysEntry
	// paymentPlanQuery is the payment planner's per-query scratch (the
	// zone-entry index and source census, rules/payment_plan_search.go),
	// installed for one query and validated against the log on every read.
	// Pure per-query scratch: Clone copies none of it.
	paymentPlanQuery *paymentPlanQuery
	// paymentPlanQueryKept / paymentPlanQueryKeptStamp are the offer
	// builder's query scope kept at its posed decision for the decision's
	// other pure payment readers (paymentPlanQueryResumeBegin). Pure scratch:
	// Clone copies none of it.
	paymentPlanQueryKept      *paymentPlanQuery
	paymentPlanQueryKeptStamp potentialStamp
	// paymentPlanQueryFree is the last finished query scope, reset and
	// reused by the next (paymentPlanQueryBegin). Clone copies none.
	paymentPlanQueryFree *paymentPlanQuery
	// zoneEntry is the incremental zone-entry index paymentSourceZoneSeq
	// reads (payment_zone_entry.go), validated against the log on every
	// read. Pure scratch over the log: Clone copies none of it.
	zoneEntry zoneEntryIndex
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
	// potentialWalk is one posed priority decision's PotentialMana and the
	// legal-offer walk priced against it, shared by the offer builder, the
	// PotentialActions projection and PotentialPaymentPlans
	// (potential_walk_cache.go). potentialAskSerial (bumped by every ask and
	// every Submit) keys it to the decision; potentialWalkDepth bypasses it
	// inside its own computation; potentialFullDemand records that a
	// full-walk reader asked on this engine. Pure scratch: Clone copies none.
	potentialWalk       potentialWalkCache
	potentialAskSerial  uint64
	potentialWalkDepth  int
	potentialFullDemand bool
	// crossWalkRetires counts retireCrossWalkMemo calls (derivedmemo.go), so
	// activeBuildSeq minus it counts active()'s real rebuilds. Clone copies
	// none (a clone's activeBuildSeq restarts too).
	crossWalkRetires uint64
	// priorityWalk is the posed priority decision's own offer walk, which
	// the potential readers use while PotentialMana adds nothing to the pool
	// (potential_walk_cache.go). Clone copies none.
	priorityWalk priorityWalkTail
	// walkRec is the priority walk's pool-independent block record and
	// walkReuse the record armed for the next potential walk
	// (walk_block_reuse.go). Clone copies none.
	walkRec   walkBlockRec
	walkReuse *walkBlockRec
	// walkRecDemand: the payment offer builder has run on this engine, so
	// its potential walk follows priority walks and they record
	// (walk_block_reuse.go). Clone copies none.
	walkRecDemand bool
	// potentialManaRec is the record armed for the next PotentialMana's
	// membership walk (walk_block_reuse.go potentialMembers). Clone copies
	// none.
	potentialManaRec *walkBlockRec
	// walkBlocksServed counts the blocks a potential walk served from the
	// record (a test-visible diagnostic, like legalActionWalks).
	walkBlocksServed uint64
	// walkMembersServed counts PotentialMana membership lists served from
	// the record (the same kind of diagnostic).
	walkMembersServed uint64
	// graveCandBuf is the offer walk's graveyard-candidate scratch
	// (legal_walk_grave_skip.go), taken for the section. Not cloned.
	graveCandBuf []state.ObjID

	// derivingColorsSet/ID/Colors: the finished layer-5 colour answer for the
	// object whose Derived is mid-build (set by derivedWith before its layer-7
	// P/T walk, restored on the way out). Colors serves it to a layer-7 pump
	// expression that counts the object's own colours, instead of re-entering
	// Derived and recursing forever. Pure per-call scratch exactly like
	// derivedDepth — Clone copies none of it (clone.go's scratch precedent).
	derivingColorsSet bool
	derivingColorsID  state.ObjID
	derivingColors    string

	// secretVoteBallots is emission-scoped scratch, visible only while the
	// public, ballot-free completion Note is scanned for Vote triggers.
	// It is never stored on Game or in the event log.
	secretVoteBallots []effects.VoteBallot
	// triggerBefore is the immutable pre-departure board for an SBA death
	// batch. Scoped to its emission/resumption, never carried as live state.
	triggerBefore *triggerSnapshot
	// snapPool recycles the object arenas of trigger-window snapshots no
	// record retained (trigger_snapshot_pool.go). Owned by exactly one
	// engine (snapshotPool.owner): a by-value Engine copy (entryPreview's
	// preview) carries the pointer but never uses it, and Clone leaves it
	// nil for the clone to build its own.
	snapPool *snapshotPool
	// lookBack is checkTriggers' reusable look-back observer Engine: the
	// observer lives for one checkTriggers call and never emits, so one
	// struct serves every call, rebuilt from zero each time (a fresh
	// observer's exact state, minus the allocation). lookBackOwner is the
	// engine that allocated it, so a by-value Engine copy never reuses the
	// original's; lookBackBusy guards against a nested use.
	lookBack *Engine
	// decArena backs posed priority decisions for an engine whose decisions
	// all die with it (SetDecisionArena, decision_arena.go); nil or off
	// everywhere else.
	decArena      *decisionArena
	lookBackOwner *Engine
	lookBackBusy  bool
	// preview is entryPreview's reusable preview Engine struct, owned and
	// guarded exactly like lookBack (previewOwner, previewBusy): a preview
	// lives for one entry's plan and is zeroed when released
	// (releaseEntryPreview).
	preview      *Engine
	previewOwner *Engine
	previewBusy  bool
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

	// trigZeroNoopEp/Objs/Ver key checkFaceTriggers' zero-interest no-op
	// memo: the log length, arena size and registry version after the last
	// zero-interest walk that visited no object (0: none). Clone leaves it
	// zero.
	trigZeroNoopEp   int
	trigZeroNoopObjs int
	trigZeroNoopVer  int
	// atkOffers is attackOffers' last list and atkOffersEp/Ver/Objs/Active
	// its key (log length -- 0: none --, registry version, arena size,
	// active player); reused across a layer-inert run. Clone leaves it zero.
	atkOffers       []attackOffer
	atkOffersEp     int
	atkOffersVer    int
	atkOffersObjs   int
	atkOffersActive state.PlayerID

	// trigZeroNoopKinds is the set of event kinds the memo holds: the walk
	// is narrowed per kind (trigger_kinds.go), so an empty walk for one
	// zero-interest kind says nothing about another.
	trigZeroNoopKinds trigKinds

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
	// targetCensusBuf is candidatesCountForLimit's scratch list (taken for
	// the call; Clone leaves it nil).
	targetCensusBuf []targetCandidate
	// manaAbScratch is the priority mana member-set scratch list
	// (activateManaFor, priorityManaAbilityCount; taken for the call,
	// Clone leaves it nil).
	manaAbScratch []*cards.SA
	// legalScratch is the offer walk's incremental log-derived indexes and
	// their watermarks (legal_walk_scratch.go). Clone carries it
	// (cloneLegalWalkScratch): copy-on-write, so nothing is shared mutably.
	legalScratch legalWalkScratch
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
	// lossProof is the sticky no-"loses all abilities" proof
	// (abilityloss.go). Clone and the look-back observer copy it.
	lossProof abilityLossProof
	// layer5Colors is the on-demand layer-5 derived-colour table
	// (layer5colors.go) and its key. Clone leaves it zero.
	layer5Colors   []effects.ObjectColors
	colorsEpoch    int
	colorsVersion  int
	colorsObjs     int
	colorsValid    bool
	colorsBuilding bool
	// charsSum is active()'s per-build digest for Characteristics' printed
	// fast path (derived_printed.go). Clone leaves it zero.
	charsSum charsSummary
	// faceScans memoises per-face text-scan verdicts (face_scan_memo.go).
	// Clone leaves it nil.
	faceScans map[*cards.Face]faceScan
	// intentBuf is a recycled intent array from Config.Spare, installed as
	// the log's Intents on the first Submit (see there). Not cloned.
	intentBuf []decision.Intent
	// sbaIDBuf is the battlefield-snapshot scratch attachmentSBAs and
	// checkSagas range (taken for the walk, restored after). Not cloned.
	sbaIDBuf []state.ObjID
	// hypSpares recycles the hypothetical clones' storage (hypclone.go).
	// Owner-guarded like decArena; Clone leaves it nil.
	hypSpares *hypSparePool
}
