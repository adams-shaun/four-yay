package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// engineScratch groups the Engine's per-walk and per-emit scratch state
// that a clone deliberately leaves zero. It is embedded by value in Engine
// (rules/engine_struct.go), so every field keeps its documented contract
// comment and every existing e.<field> access keeps compiling unchanged
// through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineScratch struct {
	// charsWalk is the rules/chars layer walk's per-call state (chars.Scratch):
	// Derived's scratch keyword and type buffers (KW/Types, rewritten in place
	// so repeated derived-characteristic reads do not allocate), their re-entry
	// guard (Depth: a nested Derived mid-build owns private buffers instead of
	// clobbering the outer build's), the in-progress layer-7 snapshots exposed
	// to effects-side Count$Valid scans (PTFrames), and the finished layer-5
	// colour stash for the object mid-build (ColorsSet/ColorsID/Colors: Colors
	// serves it to a layer-7 pump expression that counts the object's own
	// colours, instead of re-entering Derived and recursing forever). All pure
	// per-call scratch, rebuilt every call, carrying no cross-call state beyond
	// capacity; Clone() copies none of it (see clone.go), so a cloned engine
	// grows its own -- never aliasing the original's mutable scratch, exactly
	// the A2 buffer / C3 digest precedent.
	charsWalk chars.Scratch `clone:"reset"`
	// charsScratch is the record Engine.Chars answers with outside a Derived
	// memo scope (host_read.go): one Derived build copied here so the query
	// can hand out a pointer. Pure per-call scratch; Clone copies none.
	charsScratch Derived `clone:"reset"`
	// combatStatics is combatBoard.Statics' per-mode conversion buffer
	// (rules/combat_board.go): activeStatics(mode) re-viewed as combat.Static
	// values for the rules/combat predicates. Pure per-call scratch, rewritten
	// in full on every call; Clone copies none.
	combatStatics []combatStaticsBuf `clone:"reset"`

	// derivedMemo / derivedMemoDepth / derivedMemoGen are Derived's per-object
	// memo for ONE legal-actions walk (rules/derivedmemo.go): derivedMemoDepth
	// is the scope counter legalActionsPriced raises, derivedMemoGen is bumped
	// on every outermost scope entry so no entry outlives the walk that built
	// it, and derivedMemo (keyed by ObjID) owns each cached result's slices.
	// Pure per-walk scratch: Clone copies none of it (a clone starts with an
	// empty memo and generation 0, which no entry ever matches).
	derivedMemo      derivedMemoTable `clone:"reset,pool=memo,release=.release"`
	derivedMemoStack derivedMemoTable `clone:"reset,pool=memoStack,release=.release"`
	derivedMemoDepth int              `clone:"reset"`
	derivedMemoGen   uint64           `clone:"reset"`
	// derivedMemoTail / derivedMemoAlias* carry the priority walk's memo
	// across the decision boundary into a BeginDerivedReads scope
	// (rules/derivedmemo.go). Validated on every use; Clone copies none.
	derivedMemoTail      derivedMemoTail `clone:"reset"`
	derivedMemoAliasFrom int             `clone:"reset"`
	derivedMemoAliasTo   int             `clone:"reset"`
	// manaConvCache is a walk-scoped cache keyed like the Derived memo
	// (rules/walkcache.go). Pure per-walk scratch: Clone copies none of it.
	boardStaticsCache  boardStaticsCache    `clone:"reset"`
	activeStaticsCache []activeStaticsEntry `clone:"reset"`
	// activeStaticsScan records the lists the last fused activeStatics scan
	// walked (static_scan_reuse.go). Pure scratch: Clone copies none.
	activeStaticsScan staticScanRec `clone:"reset"`
	// actIndex is the incremental activation-count folds
	// (activation_count_index.go). Pure scratch: Clone copies none.
	actIndex activationIndex `clone:"reset"`
	// boardScanBuf is the printed board scan's gathered zone lists
	// (static_scan_reuse.go: gatherBoardScan). Pure scratch.
	boardScanBuf  []boardScanList `clone:"reset"`
	mayPlaysCache []mayPlaysEntry `clone:"reset"`
	// paymentPlanQuery is the payment planner's per-query scratch (the
	// zone-entry index and source census, rules/payment_plan_search.go),
	// installed for one query and validated against the log on every read.
	// Pure per-query scratch: Clone copies none of it.
	paymentPlanQuery *paymentPlanQuery `clone:"reset"`
	// paymentPlanQueryKept / paymentPlanQueryKeptStamp are the offer
	// builder's query scope kept at its posed decision for the decision's
	// other pure payment readers (paymentPlanQueryResumeBegin). Pure scratch:
	// Clone copies none of it.
	paymentPlanQueryKept      *paymentPlanQuery `clone:"reset"`
	paymentPlanQueryKeptStamp potentialStamp    `clone:"reset"`
	// paymentPlanQueryFree is the last finished query scope, reset and
	// reused by the next (paymentPlanQueryBegin). Clone copies none.
	paymentPlanQueryFree *paymentPlanQuery `clone:"reset"`
	// zoneEntry is the incremental zone-entry index paymentSourceZoneSeq
	// reads (payment_zone_entry.go), validated against the log on every
	// read. Pure scratch over the log: Clone copies none of it.
	zoneEntry zoneEntryIndex `clone:"reset"`
	// paymentPlanRelaxed is PotentialPaymentPlans' transient proof mode
	// (rules/potential_plan.go paymentPlanRelaxProof): relaxed, never
	// executed alternatives for the mana abilities the planner census does
	// not price, appended to every search while it is set. Pure per-query
	// scratch: Clone copies none of it.
	paymentPlanRelaxed [][]pay.Alt `clone:"reset"`
	// paymentPlanRelaxedFee is the generic the relaxed proof charges on top
	// of every planned cost for the paid relaxed abilities it admits.
	paymentPlanRelaxedFee int32 `clone:"reset"`
	// paymentPlanPotentialPool marks a PotentialPaymentPlans query
	// (paymentPlanPoolAccepted). Pure per-query scratch: Clone copies none.
	paymentPlanPotentialPool bool `clone:"reset"`
	// potentialWalk is one posed priority decision's PotentialMana and the
	// legal-offer walk priced against it, shared by the offer builder, the
	// PotentialActions projection and PotentialPaymentPlans
	// (potential_walk_cache.go). potentialAskSerial (bumped by every ask and
	// every Submit) keys it to the decision; potentialWalkDepth bypasses it
	// inside its own computation; potentialFullDemand records that a
	// full-walk reader asked on this engine. Pure scratch: Clone copies none.
	potentialWalk       potentialWalkCache `clone:"reset"`
	potentialAskSerial  uint64             `clone:"reset"`
	potentialWalkDepth  int                `clone:"reset"`
	potentialFullDemand bool               `clone:"reset"`
	// crossWalkRetires counts retireCrossWalkMemo calls (derivedmemo.go), so
	// activeBuildSeq minus it counts active()'s real rebuilds. Clone copies
	// none (a clone's activeBuildSeq restarts too).
	crossWalkRetires uint64 `clone:"reset"`
	// priorityWalk is the posed priority decision's own offer walk, which
	// the potential readers use while PotentialMana adds nothing to the pool
	// (potential_walk_cache.go). Clone copies none.
	priorityWalk priorityWalkTail `clone:"reset"`
	// walkRec is the priority walk's pool-independent block record and
	// walkReuse the record armed for the next potential walk
	// (walk_block_reuse.go). Clone copies none.
	walkRec   walkBlockRec  `clone:"reset"`
	walkReuse *walkBlockRec `clone:"reset"`
	// walkRecDemand: the payment offer builder has run on this engine, so
	// its potential walk follows priority walks and they record
	// (walk_block_reuse.go). Clone copies none.
	walkRecDemand bool `clone:"reset"`
	// potentialManaRec is the record armed for the next PotentialMana's
	// membership walk (walk_block_reuse.go potentialMembers). Clone copies
	// none.
	potentialManaRec *walkBlockRec `clone:"reset"`
	// walkBlocksServed counts the blocks a potential walk served from the
	// record (a test-visible diagnostic, like legalActionWalks).
	walkBlocksServed uint64 `clone:"reset"`
	// walkMembersServed counts PotentialMana membership lists served from
	// the record (the same kind of diagnostic).
	walkMembersServed uint64 `clone:"reset"`
	// graveCandBuf is the offer walk's graveyard-candidate scratch
	// (legal_walk_grave_skip.go), taken for the section. Not cloned.
	graveCandBuf []state.ObjID `clone:"reset"`

	// secretVoteBallots is emission-scoped scratch, visible only while the
	// public, ballot-free completion Note is scanned for Vote triggers.
	// It is never stored on Game or in the event log.
	secretVoteBallots []effects.VoteBallot `clone:"reset"`
	// triggerBefore is the immutable pre-departure board for an SBA death
	// batch. Scoped to its emission/resumption, never carried as live state.
	triggerBefore *triggerSnapshot `clone:"reset"`
	// snapPool recycles the object arenas of trigger-window snapshots no
	// record retained (trigger_snapshot_pool.go). Owned by exactly one
	// engine (snapshotPool.owner): a by-value Engine copy (entryPreview's
	// preview) carries the pointer but never uses it, and Clone leaves it
	// nil for the clone to build its own.
	snapPool *snapshotPool `clone:"reset"`
	// lookBack is checkTriggers' reusable look-back observer Engine: the
	// observer lives for one checkTriggers call and never emits, so one
	// struct serves every call, rebuilt from zero each time (a fresh
	// observer's exact state, minus the allocation). lookBackOwner is the
	// engine that allocated it, so a by-value Engine copy never reuses the
	// original's; lookBackBusy guards against a nested use.
	lookBack *Engine `clone:"reset"`
	// decArena backs posed priority decisions for an engine whose decisions
	// all die with it (SetDecisionArena, decision_arena.go); nil or off
	// everywhere else.
	decArena      *decisionArena `clone:"reset"`
	lookBackOwner *Engine        `clone:"reset"`
	lookBackBusy  bool           `clone:"reset"`
	// preview is entryPreview's reusable preview Engine struct, owned and
	// guarded exactly like lookBack (previewOwner, previewBusy): a preview
	// lives for one entry's plan and is zeroed when released
	// (releaseEntryPreview).
	preview      *Engine `clone:"reset"`
	previewOwner *Engine `clone:"reset"`
	previewBusy  bool    `clone:"reset"`
	// A shallow read-only observer of a recurring Effect trigger overrides
	// controllerOf for its creating source. The Effect's controller is the
	// registration's owner, even when its source card belongs to another seat.
	// Only the observer sets this; live Engine and Game state are unchanged.
	effectMatchSource     state.ObjID    `clone:"reset"`
	effectMatchController state.PlayerID `clone:"reset"`
	effectMatchRemembered []state.Target `clone:"reset"`
	effectMatchOverride   bool           `clone:"reset"`
	// lifeLossBatch holds the events in one simultaneous life-loss operation.
	// It is scoped to one synchronous effect/combat pass, so it is always nil
	// at an intent boundary and does not need log encoding or Clone state.
	lifeLossBatch          []events.Event `clone:"reset"`
	lifeLossBatchDepth     int            `clone:"reset"`
	finishingLifeLossBatch bool           `clone:"reset"`

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
	foreachBuf   []state.ObjID `clone:"reset"`
	foreachDepth int           `clone:"reset"`

	// trigZeroNoopEp/Objs/Ver key checkFaceTriggers' zero-interest no-op
	// memo: the log length, arena size and registry version after the last
	// zero-interest walk that visited no object (0: none). Clone leaves it
	// zero.
	trigZeroNoopEp   int `clone:"reset"`
	trigZeroNoopObjs int `clone:"reset"`
	trigZeroNoopVer  int `clone:"reset"`
	// atkOffers is attackOffers' last list and atkOffersEp/Ver/Objs/Active
	// its key (log length -- 0: none --, registry version, arena size,
	// active player); reused across a layer-inert run. Clone leaves it zero.
	atkOffers       []attackOffer  `clone:"share,if=cloneCarriesAtkOffers"`
	atkOffersEp     int            `clone:"deep,if=cloneCarriesAtkOffers"`
	atkOffersVer    int            `clone:"deep,if=cloneCarriesAtkOffers,rekey=now"`
	atkOffersObjs   int            `clone:"deep,if=cloneCarriesAtkOffers"`
	atkOffersActive state.PlayerID `clone:"deep,if=cloneCarriesAtkOffers"`

	// trigZeroNoopKinds is the set of event kinds the memo holds: the walk
	// is narrowed per kind (trigger_kinds.go), so an empty walk for one
	// zero-interest kind says nothing about another.
	trigZeroNoopKinds trigKinds `clone:"reset"`

	// legalOptBuf is legalActionsPriced's scratch option list. The walk
	// appends into it (so the doubling growth that used to reallocate the
	// list several times per walk settles at the largest walk seen) and
	// returns an exactly-sized COPY: the returned slice is owned by the
	// caller -- it becomes a pending Decision's Options, which seats, views,
	// traces and search forks retain -- so the scratch never escapes. The
	// walk takes the buffer (leaving nil) for its duration, so a re-entrant
	// walk allocates its own rather than clobbering the outer one. Owned by
	// this Engine alone: Clone leaves it nil, like foreachBuf.
	legalOptBuf []decision.Option `clone:"reset,pool=legalOpts"`
	// targetCensusBuf is candidatesCountForLimit's scratch list (taken for
	// the call; Clone leaves it nil).
	targetCensusBuf []targetCandidate `clone:"reset"`
	// manaAbScratch is the priority mana member-set scratch list
	// (activateManaFor, priorityManaAbilityCount; taken for the call,
	// Clone leaves it nil).
	manaAbScratch []*cards.SA `clone:"reset"`
	// legalScratch is the offer walk's incremental log-derived indexes and
	// their watermarks (legal_walk_scratch.go). Clone carries it
	// (cloneLegalWalkScratch): copy-on-write, so nothing is shared mutably.
	legalScratch legalWalkScratch `clone:"share,copy=cloneLegalWalkScratch"`
	// legalActionWalks counts every legalActionsPriced call (test-visible
	// only; unexported, bumped unconditionally, no event and no effect on
	// determinism or chain heads -- a plain monotonic read-only diagnostic
	// counter). It lets a test count legal-action walks directly now that
	// paymentActionsForPriority opens ONE derived-memo scope around the whole
	// offer build, which pins derivedMemoGen's delta at 1 regardless of how
	// many walks run inside. Clone leaves it zero, like the scratch fields.
	legalActionWalks uint64 `clone:"reset"`
	// manaAbBuf is the offer walk's per-object mana-ability scratch list
	// (legal.go), and manaLabels its "Activate <name> for mana" label cache
	// (manaActivateLabel; a pure function of the name, only ever looked up,
	// never ranged). Both are Engine-owned scratch: Clone leaves them nil.
	manaAbBuf  []*cards.SA       `clone:"reset,pool=manaAb"`
	manaLabels map[string]string `clone:"reset"`
	// activeSum is active()'s per-build digest for the mana walk and
	// grantedAbilities (active_summary.go). Clone leaves it zero.
	activeSum activeSummary `clone:"reset"`
	// lossProof is the sticky no-"loses all abilities" proof
	// (abilityloss.go). Clone and the look-back observer copy it.
	lossProof abilityLossProof `clone:"deep"`
	// lossMemo is abilityLoss's per-active()-build answer cache
	// (abilityloss_memo.go). Clone leaves it zero.
	lossMemo abilityLossMemo `clone:"reset,pool=lossMemo,release=.recycled"`
	// layer5Colors is the on-demand layer-5 derived-colour table
	// (layer5colors.go) and its key. Clone leaves it zero.
	layer5Colors   []effects.ObjectColors `clone:"reset"`
	colorsEpoch    int                    `clone:"reset"`
	colorsVersion  int                    `clone:"reset"`
	colorsObjs     int                    `clone:"reset"`
	colorsValid    bool                   `clone:"reset"`
	colorsBuilding bool                   `clone:"reset"`
	// charsSum is active()'s per-build digest for Characteristics' printed
	// fast path (derived_printed.go). Clone leaves it zero.
	charsSum charsSummary `clone:"reset"`
	// faceScans memoises per-face text-scan verdicts (face_scan_memo.go).
	// Clone leaves it nil.
	faceScans map[*cards.Face]faceScan `clone:"reset"`
	// intentBuf is a recycled intent array from Config.Spare, installed as
	// the log's Intents on the first Submit (see there). Not cloned.
	intentBuf []decision.Intent `clone:"reset"`
	// sbaIDBuf is the battlefield-snapshot scratch attachmentSBAs and
	// checkSagas range (taken for the walk, restored after). Not cloned.
	sbaIDBuf []state.ObjID `clone:"reset"`
	// hypSpares recycles the hypothetical clones' storage (hypclone.go).
	// Owner-guarded like decArena; Clone leaves it nil.
	hypSpares *hypSparePool `clone:"reset"`
}
