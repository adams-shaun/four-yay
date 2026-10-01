package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// engineEmitCtx groups the Engine's synchronous emit context. It is
// embedded by value in Engine (rules/engine_struct.go), so every field
// keeps its documented contract comment and every existing e.<field>
// access keeps compiling unchanged through Go's field promotion. Clone's
// per-field copy classes (rules/clone.go) are unchanged by the move.
type engineEmitCtx struct {
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
	// replacements (valid while stepLeavingSet); parked choices own a value
	// copy. Held by value so a step change allocates nothing.
	stepLeaving    state.Step
	stepLeavingSet bool

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
	manaTapMark int
	tapObj      state.ObjID
	tapPlayer   state.PlayerID
	tapEntering bool

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
}
