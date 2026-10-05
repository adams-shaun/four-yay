package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// engineTriggerBatches groups the Engine's trigger-fire latches,
// simultaneous batch windows and zone-skip caches. It is embedded by value
// in Engine (rules/engine_struct.go), so every field keeps its documented
// contract comment and every existing e.<field> access keeps compiling
// unchanged through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineTriggerBatches struct {
	// triggerFireCount and the damage-batch fields below are trigger_match.go's
	// own bookkeeping (the cascade bound and the DamageDealtOnce/DamageDoneOnce
	// once-per-damage-batch gate); see there.
	triggerFireCount map[triggerKey]int32 `clone:"deep"`
	// unblockedOnceFired latches an AttackerUnblockedOnce trigger to ONE fire
	// per combat (rules.trigger_match.go's checkAttackerUnblockedOnceTriggers):
	// Forge's Mode$ AttackerUnblockedOnce fires once for the whole
	// declare-blockers round complete even when several attackers match, and
	// the Once means once per COMBAT, not per game -- an extra combat fires it
	// again. The stamp is (Turn, CombatsThisTurn), the event-folded per-turn
	// combat count, so it uniquely names a combat and needs no reset hook.
	unblockedOnceFired map[triggerKey]combatFires `clone:"deep"`
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
	unblockedRoundChecked combatFires `clone:"deep"`
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
	attackersDeclaredFired map[triggerKey]combatFires `clone:"deep"`
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
	damageBatchOpen  bool                   `clone:"deep"`
	damageBatchDepth int                    `clone:"deep"`
	damageBatchIdx   map[damageBatchKey]int `clone:"deep"`
	damageBatchLog   []damageBatchEntry     `clone:"deep"`
	// Pre-fold lethal threshold per recipient, fixed at its first hit in the
	// batch so earlier simultaneous hits never lower the threshold.
	excessDamageBaseline map[state.ObjID]int32 `clone:"deep"`
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
	zoneBatchOpen  bool                 `clone:"deep"`
	zoneBatchDepth int                  `clone:"deep"`
	zoneBatchIdx   map[zoneBatchKey]int `clone:"deep"`
	zoneBatchLog   []zoneBatchEntry     `clone:"deep"`
	// millBatch is the shared "one or more" ACTION batch: one api:Mill
	// resolution (Mode$ MilledAll), one api TapAll/UntapAll resolution or
	// turn-based tapping/untapping action (Mode$ TapAll/UntapAll, task
	// cli-20261005T075020Z-05241a06) is ONE action, so the batch mode fires
	// once for the whole call, not once per event. The damage/zone batches'
	// shape, but keyed by trigger LINE alone (the DamageAll "one or more"
	// reading): the first matching event queues the single instance and every
	// later matching event accumulates into the entry's COUNT -- the number
	// of cards/permanents this way, which the bodies read through
	// TriggerCount$Amount (The Wise Mothman's X, Magmakin Artillerist's X,
	// The Millennium Calendar's X). Only events matching THIS line's
	// ValidCard$/ValidCards$ count, exactly as DamageAll only accumulates
	// matching pairs. It is opened by effects/action_batch.go's
	// beginActionBatch (effMill, effTapAll, effUntapAll) and by rules-side
	// action boundaries (rules/combat.go's declare-attackers taps,
	// rules/turn.go's untap step, rules/pay's BatchTap cost bracket). The
	// untap-step bracket is held open across a pending untap choice or
	// replacement (rules/turn.go's finishUntapStep suspends mid-step), so the
	// bracket state is clone:"deep" (the discard bracket's shape): a clone or
	// snapshot taken at that pending decision must carry the open bracket, or
	// a resume in the clone would re-derive per-event triggers instead of the
	// single batched one. Never opened across a drain: pendingTriggers is
	// append-only while the batch is open, so the recorded index stays valid.
	millBatchOpen  bool               `clone:"deep"`
	millBatchDepth int                `clone:"deep"`
	millBatchIdx   map[triggerKey]int `clone:"deep"`
	millBatchLog   []millBatchEntry   `clone:"deep"`
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
	discardBatchOpen  bool                `clone:"deep"`
	discardBatchDepth int                 `clone:"deep"`
	discardBatchIdx   map[triggerKey]int  `clone:"deep"`
	discardBatchLog   []discardBatchEntry `clone:"deep"`
	// discardAllTurn is the Mode$ DiscardedAll FirstTime$ latch: one trigger
	// LINE's most recent batch turn, so "for the first time each turn" admits
	// only the first qualifying discard batch per turn. Recorded at queue
	// time (when the batch's single instance is created) and cleared by the
	// turn boundary, the triggerTurnFires shape. Per trigger line is exact
	// for every corpus carrier, whose ValidPlayer$ is the source's own
	// controller ("You"); a line naming another player's discard would need a
	// per-player key, which no current carrier has.
	discardAllTurn map[triggerKey]int32 `clone:"deep"`
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
	targetBatchOpen  bool                `clone:"reset"`
	targetBatchFired map[triggerKey]bool `clone:"reset"`
	// phaseUnknownNoted memoizes the Phase$ specs whose names this engine has
	// already reported as unresolvable (rules/trigmatch/actions.go's PhaseMatches
	// reporting), so one spec emits exactly one Note per game no matter how
	// often its trigger is walked. Cloned like the other bookkeeping maps so
	// a branch that becomes live cannot re-emit the same Note.
	phaseUnknownNoted map[string]bool `clone:"deep"`
	// disableTriggersNoted memoizes the DisableTriggers statics whose unread
	// parameter grammar this engine has already reported (rules/disable_triggers.go's
	// unmodelled diagnostic), keyed by the static's source so one static emits
	// exactly one Note per game. Cloned like phaseUnknownNoted so a branch that
	// becomes live cannot re-emit it.
	disableTriggersNoted map[string]bool `clone:"deep"`
	// phaseSpecs caches pure Phase$ parsing for both diagnostics and matching.
	// It is scratch, not replay bookkeeping: clones start with an empty cache.
	phaseSpecs map[string]parsedPhase `clone:"reset"`
	// triggerEventMasks caches only immutable syntax for unbound fixture faces,
	// not live source membership. Bound corpus faces use their catalog-owned
	// trigger interests. Like phaseSpecs, clones own fresh writable scratch.
	triggerEventMasks map[*cards.Face]triggerEventMask `clone:"reset"`
	// triggerObjectMasks is the dense object-walk form of triggerEventMasks.
	// Entries validate their immutable face pointer and are scratch owned by
	// one Engine, so hypothetical clones never share writable cache storage.
	triggerObjectMasks []objectTriggerEventMasks `clone:"reset"`
	// trigZones / trigZonesEp / trigFaceZones are the live trigger walk's
	// per-player hidden-zone summaries (rules/trigger_zoneskip.go): pure
	// scratch validated on every use, so Clone copies none of them.
	trigZones     []trigZoneSummary     `clone:"deep,pool=trigZones,copy=copyTrigZones,release=releasedTrigZones"`
	trigZonesEp   int                   `clone:"deep"`
	trigFaceZones map[*cards.Face]uint8 `clone:"reset"`
	// trigFaceKinds is faceTrigKinds' cache for faces outside the compiled
	// face table (trigger_kinds.go): immutable syntax, scratch like
	// trigFaceZones.
	trigFaceKinds map[*cards.Face]faceTrigCache `clone:"reset"`
	// trigZoneGen counts summary changes (any write to a trigZones entry
	// beyond a no-op confirmation); trigPlan is the whole-board walk plan
	// keyed to it (trigger_plan.go). Scratch: a clone starts with no plan.
	trigZoneGen uint64   `clone:"reset"`
	trigPlan    trigPlan `clone:"reset"`
	// trigGrant proves that no active() entry can carry a granted trigger
	// (trigger_grantfree.go), so the trigger walk's granted-static list is
	// empty without building active().
	trigGrant trigGrantProof `clone:"deep"`
	// trigWalkUnion is the last live walk's whole-board signature union,
	// valid when trigWalkUnionOK (the zero-interest memo widens with it).
	trigWalkUnion   trigSig `clone:"reset"`
	trigWalkUnionOK bool    `clone:"reset"`
	// replZones / replZonesEp are the replacement-source walk's per-seat
	// zone summaries (rules/repl_zoneskip.go): pure scratch validated on
	// every use, so Clone copies neither.
	replZones   []replZoneSummary `clone:"deep,pool=replZones,copy=copyReplZones,release=releasedReplZones"`
	replZonesEp int               `clone:"deep"`
	// replArena is the whole arena's replacement event-bit union
	// (rules/repl_arena_mask.go); Clone carries it with the board.
	replArena replArenaMask `clone:"deep"`
	// staticZones / staticZonesEp are the off-battlefield static-source
	// walks' per-seat zone summaries (rules/static_zoneskip.go): pure scratch
	// validated on every use, so Clone copies neither.
	staticZones   []staticZoneSummary `clone:"reset"`
	staticZonesEp int                 `clone:"deep"`
	// walkObjCls is the offer walk's per-object class cache
	// (rules/walk_objclass.go), indexed by ObjID-1 and kept exact by the
	// staticZones catch-up: pure scratch, so Clone copies none.
	walkObjCls []walkObjClass `clone:"deep"`
	// walkClsOwner is the engine walkObjCls belongs to: a by-value Engine
	// copy sees another owner and starts its own (ownWalkClasses).
	walkClsOwner *Engine `clone:"reset"`
	// offerProbeDepth counts the open face and cast probes (faceprobe.go,
	// castprobe.go), whose no-event writes the class cache must not record.
	offerProbeDepth int `clone:"reset"`
	// staticTouchGen counts the catch-up's touches of objects that are, or
	// were, static-hot (walk_objclass.go: walkClassTouch): the static scans'
	// cross-walk reuse keys on it (walkcache.go). Never cloned.
	staticTouchGen uint64 `clone:"reset"`
	// staticZoneVerified is verify-mode scratch (static_zoneskip.go's
	// staticZoneSkipVerifyOnce): the (cur, hot) slices each summary slot was
	// verified against inside the current verifyBoardStatics call
	// (staticZoneVerifyScope set). Clone copies none of it.
	staticZoneVerified    []staticZoneVerifiedAt `clone:"reset"`
	staticZoneVerifyScope bool                   `clone:"reset"`

	tappedTurn       map[state.ObjID]int32    `clone:"deep"`
	triggerTurnFires map[triggerKey]turnFires `clone:"deep"`
	// triggerGameFires is the lifetime queue count for GameActivationLimit$.
	// Unlike triggerTurnFires it is never reset at TurnChange.
	triggerGameFires map[triggerKey]gameFires `clone:"deep"`
	// triggerTurnResolved is ResolvedLimit$'s per-turn resolution count,
	// keyed by the trigger's SOURCE object (not its triggerKey): Forge's
	// TriggeredAbility.resolvedThisTurn caps how many times a T: line may
	// RESOLVE each turn, and a ResolvedLimit$ card's paired lines (the
	// corruption_of_towashi halves of one printed ability) must share it.
	triggerTurnResolved map[state.ObjID]turnFires `clone:"deep"`
	// triggerTurnDice is the RolledDie Number$ gate's per-turn die-roll count,
	// keyed by the trigger line (triggerKey) so each "whenever you roll your
	// third die each turn" line counts its own rolls. It self-resets when the
	// turn changes, exactly as triggerTurnFires does.
	triggerTurnDice map[triggerKey]turnFires `clone:"deep"`
	// triggerTurnDiceTurn is the turn triggerTurnDice was last reset for;
	// a lookup in any later turn replaces the map, bounding its size.
	triggerTurnDiceTurn int32                            `clone:"deep"`
	dmgSrcOverride      state.ObjID                      `clone:"reset"`
	batchDamageKeywords map[state.ObjID]damageKeywordLKI `clone:"reset"`
	// batchWindow is the pre-departure board BatchDepartures parked in
	// triggerBefore for an effect's simultaneous departure batch, and
	// batchWindowDepth the open BatchDepartures calls (nested batches share
	// the outermost board). Engine-transient like triggerBefore: nil/zero at
	// every intent boundary, never cloned.
	batchWindow      *triggerSnapshot `clone:"reset"`
	batchWindowDepth int              `clone:"reset"`
}
