package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// engineParked groups the Engine's parked continuation windows (suspended
// casts, mana activations, commander zone, replacement choices). It is
// embedded by value in Engine (rules/engine_struct.go), so every field
// keeps its documented contract comment and every existing e.<field>
// access keeps compiling unchanged through Go's field promotion. Clone's
// per-field copy classes (rules/clone.go) are unchanged by the move.
type engineParked struct {
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
}
