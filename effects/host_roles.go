package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// host_roles.go splits effects.Host into role interfaces (rules-engine
// refactor spec W1d, docs/superpowers/specs/2026-10-03-rules-engine-lasagna-
// design.md section 5). Host stays the union of every role, so the change is
// incremental: an effect body still receives a Host, while a new helper takes
// the narrowest role it uses, so its signature says what it touches.
// internal/codeshape ratchets the union's size and the largest role's.

// HostRead is the read role: the live game and the characteristics queries an
// effect reads it through. Chars is the one layer-derived characteristics
// query; the scalar accessors beside it are its allocation-free fast paths
// for the facts hot effect code reads one at a time.
type HostRead interface {
	// Game returns the live match state for reading. The returned *state.Game
	// must never be written to directly: every state mutation goes through
	// Emit (which routes through events.Apply), which is what keeps the event
	// log a complete description of the match.
	Game() *state.Game
	// Chars is the object's current, layer-derived characteristics (CR 613):
	// effects.Chars, the record rules' layer walk builds. The pointer and its
	// Keywords/Types are valid until the next Chars call or emit, whichever
	// comes first; a caller that holds them across either copies them. Never
	// make two Chars reads in one expression (`h.Chars(a).X, h.Chars(b).X`):
	// Go does not order the first field load before the second call. A
	// printed read goes through PrintedChars, never through this query.
	Chars(id state.ObjID) *Chars
	// ObjectColors returns the object's live layer-5 colours when it is on the
	// battlefield, and its face/CDA colours in other zones.
	ObjectColors(*state.Object) string
	// HasKeyword reports a DERIVED keyword — printed or granted by a
	// continuous effect (rules.Engine.HasKeyword). Effects that gate on a
	// keyword (Destroy on Indestructible) must ask this, never the face.
	HasKeyword(id state.ObjID, kw string) bool
	// UmbraArmorAura returns the ObjID of the first attached Aura whose
	// DERIVED keyword set carries "Umbra armor" (CR 702.90), in
	// deterministic AliveFrom(0) × battlefield-slice order, or 0 if bearer id
	// wears none. Derived, never the printed face: Umbra Mystic's and Dog
	// Umbra's layer-6 grants must be seen. Consulted by ReplaceUmbraArmor.
	UmbraArmorAura(id state.ObjID) state.ObjID
	// Power, Toughness and IsCreature are current derived characteristics.
	// Damage/count effects must not read a printed face when layers modify P/T
	// or make a planeswalker a creature.
	Power(id state.ObjID) int32
	Toughness(id state.ObjID) int32
	IsCreature(id state.ObjID) bool
}

// HostEmit is the emit role: every proposal of an event, the one way an effect
// changes the game.
type HostEmit interface {
	Emit(events.Event)
	// EmitPlayerLost proposes a PlayerLost event for p, gated by any live
	// `R:Event$ GameLoss | Layer$ CantHappen` replacement for that player and
	// cause (CR 104.3 / 704.5a-c, task fdn-repl-cant-lose). reason is the
	// Forge GameLossReason spelling the replacement's ValidLoseReason$
	// discriminator compares ("Milled" for an empty-library draw, "Effect"
	// for api:LosesGame); an unmodelled value fails closed in the rules gate.
	// A loss the replacement stops emits nothing and the caller must not
	// report the state change -- the deck-out draw and api:LosesGame both use
	// this instead of Emit so neither can bypass the gate.
	EmitPlayerLost(p state.PlayerID, reason, text string)
	// EmitGameWin proposes a GameOver win for p (the api:WinsGame family),
	// gated by any live `R:Event$ GameWin | Layer$ CantHappen` replacement for
	// that player (task fdn-repl-cant-lose). A prevented win emits nothing.
	EmitGameWin(p state.PlayerID, text string)
	// EmitTokenCreate emits a token-creation event and returns every object
	// it actually created, in mint order. A token-creation replacement may
	// rewrite one would-be token into several mints (Divine Visitation's one
	// Angel, Doubling Season's doubled pair, Xorn's original-plus-one), and
	// CR 111's per-token riders (Tapped, counters, AttachedTo, P/T, AtEOT)
	// belong to EVERY mint, not just the first. effects/token.go calls this
	// instead of Emit so its rider loop runs once per mint. The ordinary,
	// unreplaced event returns the single token it minted (empty when nothing
	// was created). Only tokens whose battlefield entry has COMPLETED are
	// returned: a mint parked behind an entry-counter order ask returns
	// nothing (its answer publishes it to the parked-mint continuation), and
	// a CopyToken mint's battlefield MoveZone may be emitted through this
	// call too -- it returns the copy only once that entry has folded.
	EmitTokenCreate(events.Event) []state.ObjID
	// EmitStackCopy emits a StackCopy event and returns the object it actually
	// minted, if any. The copy object is created inside events.Apply's
	// StackCopy fold (AddObject assigns it the pre-emit NextID), so an effect
	// cannot read the minted id off its own event; effects/copy.go's
	// RememberCopies$ rider (Forge's card.addRemembered(copies)) calls this
	// instead of Emit to append the copy to the remembered set. The empty
	// return covers the fold's early breaks (no source, source already left
	// the stack) -- a proposed copy that minted nothing.
	EmitStackCopy(events.Event) []state.ObjID
	// EmitDamage emits a Damage event and returns the event that actually
	// landed after replacement effects. A prevention returns a non-Damage
	// result; an amount-changing replacement returns Damage with the applied
	// amount. Damage riders (lifelink/deathtouch/commander damage) must consume
	// this result rather than the proposed event.
	EmitDamage(events.Event) events.Event
	// EmitTap taps the permanent obj with the synchronous provenance a Taps
	// trigger reads but the replayed Tap event does not carry: tapper is the
	// player who tapped it (Forge Card.tap's tapper -- the resolving
	// ability's activator, a cost's payer), and entering marks a permanent
	// being given its tapped entry state by a DB$ Tap | ETB$ True replacement
	// body, which CR 603.2e says never "becomes tapped" (Forge TapEffect's
	// ETB branch sets the state without running Taps triggers). The emitted
	// event is exactly Emit(events.Event{Kind: events.Tap, Obj: obj}), so the
	// hash chain is unaffected; rules.Engine keeps the provenance as event
	// context while the event's triggers are matched.
	EmitTap(obj state.ObjID, tapper state.PlayerID, entering bool)
	// EmitScryRecord logs a COMPLETED scry instruction's events.Scry record
	// (task scrybottom): the marker trig:Scry's ToBottom$ gate reads, carrying
	// in Amount the number of cards actually put on the bottom. rules' ordinary
	// completion site is handleArrange, which emits it through emitScryRecord
	// -- OUTSIDE the replacement pass, because a finished action is nothing
	// left to replace (CR 614.4's R:Event$ Scry window is before the scry, and
	// re-matching the completed record as a fresh proposal would let Kenessos
	// rewrite or Eligeth consume an action that already happened). The asking
	// primitive's own stand-in (no-host, or the never-posted empty KArrange an
	// empty library or ScryNum$ 0 produces) completes the scry in effects, so
	// it records its zero-card bottom pile through this method instead of
	// plain Emit -- the event, not the route, is identical to handleArrange's.
	EmitScryRecord(events.Event)
}

// HostBatch is the batch and provenance role: the brackets that group an effect's
// events into one simultaneous batch, and the provenance (damage source,
// counter adder, LKI) rules attaches to the events inside them.
type HostBatch interface {
	// BeginDamageBatch/EndDamageBatch bracket the Damage events one
	// dealDamage-style call deals simultaneously (Forge dealDamage, GameAction
	// AddDamage/triggerDamageDoneOnce): within the bracket, the
	// DamageDealtOnce/DamageDoneOnce triggers latch once per batch per
	// referent (per dealing source / per damaged object) and the queued
	// trigger's referent amount is the batch's accumulated total -- Fireball
	// splitting among three creatures is ONE batch to a DamageDealtOnce
	// trigger on the source, not three. rules.Engine implements both; the
	// effects-package test double reports no-ops. Neither suspends.
	BeginDamageBatch()
	EndDamageBatch()
	// BeginZoneBatch/EndZoneBatch bracket the PhaseOut events one api:Phases
	// resolution emits (CR 702.25a's "permanents phase out one at a time"
	// still emits one event each, but the group is ONE batch for the
	// batch-level "whenever one or more permanents phase out" trigger,
	// Mode$ PhaseOutAll). Within the bracket the first matching PhaseOut event
	// queues the single instance and every later one accumulates into it.
	// rules.Engine implements the bracket with its zone-batch machinery (the
	// same depth/reentrancy discipline ChangesZoneAll uses); the effects-package
	// test double reports no-ops. Neither suspends.
	BeginZoneBatch()
	EndZoneBatch()
	// SetDamageSource overrides the in-flight damage source for the Damage
	// events the caller is about to emit: the provenance rules' emit-side
	// protection check (CR 702.16d) and DamageDone trigger matching read
	// for every Damage event. It returns the previous override so the
	// caller restores it before returning; zero restores "no override".
	// The override is engine-transient state exactly like the resolution
	// source it wraps: replay re-executes the same setter, and Clone never
	// copies it because an emitter always restores before returning
	// (DealDamage/DamageAll never ask mid-loop, so nothing suspends inside
	// the override window).
	SetDamageSource(id state.ObjID) state.ObjID
	// SetCounterAdder publishes the player causing the CounterChange /
	// PlayerCounterChange events the caller is about to emit, so the
	// repl:AddCounter class's ValidSource$ scope can be read. It mirrors
	// SetDamageSource exactly: the return value is the previous (opaque)
	// publication and the caller restores it before returning; zero restores
	// "no override". The override is engine-transient state rebuilt by replay
	// and never copied by Clone. Only a cost or turn-based placement publishes
	// explicitly -- an effect-resolution placement is attributed to the
	// resolving ability's controller by the engine's own fallback.
	SetCounterAdder(p state.PlayerID) state.PlayerID
	// BatchDepartures declares that the caller is about to emit MoveZone
	// events for every object in ids as one simultaneous destruction batch
	// (CR 704.3): the engine snapshots each object's derived lifelink
	// state NOW, before any of the moves fold, so a later batch member's
	// CR 603.10a departure capture reads the batch's own pre-state rather
	// than whatever an earlier member's departure already stripped (a
	// destroy-all over a lifelink-granting Equipment and its bearer: the
	// bearer's lifelink LKI must not depend on battlefield order). Entries
	// are consumed by the matching departure capture. EndBatchDepartures
	// clears any remaining entry after the effect loop, including a member
	// regeneration kept on the battlefield.
	BatchDepartures(ids []state.ObjID)
	EndBatchDepartures()
	// RememberExploitedLKI publishes the last-known-information snapshot of
	// one creature a resolving exploit ability just sacrificed (CR 702.58a).
	// The events.Exploit marker names the exploited creature by id, but Move
	// has by then cleared its counters and dropped its battlefield layers, so
	// a later trig:Exploited body reading TriggeredExploited$CardPower/
	// CardToughness would see the graveyard card's printed face instead of its
	// as-sacrificed P/T. effects/exploit.go publishes the snapshot here, and
	// the engine attaches it to the trig:Exploited pending trigger's Ctx.LKI
	// (rules' attachExploitedLKI), where evalRefProperty already reads an
	// object's LKI P/T for every other trigger. Rules-implemented and
	// replay-derived exactly like the other LKI maps; the effects test double
	// records it for its own assertions.
	RememberExploitedLKI(state.SacrificedInfo)
}

// HostRNG is the randomness role: the engine's seeded, logged RNG.
type HostRNG interface {
	// Rand is the engine's seeded generator. Effects that need randomness must
	// use it and nothing else, or replay breaks.
	Rand(n int) int
	// ShuffleLibrary returns a Fisher-Yates permutation of order. The engine
	// owns hypothetical shuffle planning here; effects still emit the sole
	// state-mutating Shuffle event with the returned order.
	ShuffleLibrary(state.PlayerID, []state.ObjID) []state.ObjID
}

// HostContinuous is the continuous-effect role: registering, querying and ending
// continuous effects and control grants.
type HostContinuous interface {
	// AddContinuous registers one continuous effect against the CR 613 layer
	// system (rules.Engine.AddContinuous). This is how Pump, PumpAll, Animate
	// and Protection reach the layer system without effects importing rules,
	// which would be an import cycle (effects sits below rules). Task 19c.
	AddContinuous(state.ContinuousEffect)
	// ContinuousNamed reports whether an ACTIVE continuous effect registered
	// by controller carries the given Name — the ask effEffect's Stackable$
	// False dedup makes before it would register a second copy of the same
	// named effect (Wrenn and Six's emblem: a second [-7] activation does not
	// stack a second instance). Implemented by rules.Engine against its
	// continuous-effect registry; the effects test double scans its own
	// recorded slice.
	ContinuousNamed(controller state.PlayerID, name string) bool
	// RegisterControl records one GainControl effect with the lifetime its
	// LoseControl$ names (CR 611.2b "for as long as", CR 514.2 end of turn),
	// so the engine can end it through a ControlChange event the moment that
	// duration ends. A grant with no duration is permanent: it supersedes
	// every earlier control effect on the object (CR 613.7 timestamp order).
	RegisterControl(ControlGrant)
	// EndEffect ends the one continuous-effect registration named by its
	// (source, timestamp) identity -- the analogue of Forge's implicit
	// Command-zone effect object being exiled. It backs the corpus's one-shot
	// idiom `DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$
	// Exile` run from inside an Effect-created replacement's own body (the
	// ChooseSource prevention family's RPreventNextFromSource: "the NEXT time
	// ... prevent that damage"). effChangeZone reaches it only through
	// Ctx.EffectFrame, so a body that is not an Effect-created replacement's
	// never ends anything. rules.Engine implements it as an in-place drop of
	// its registry; the effects test double drops from its recorded slice.
	EndEffect(source state.ObjID, stamp uint32)
	// EndEffectSource ends every Effect-created continuous-effect registration
	// from the named source -- the source-scoped form of EndEffect the
	// self-exile idiom run from an Effect's OWN Triggers$ body or the
	// registering spell's chain uses, where the body has no per-registration
	// (source, timestamp) identity to name (Ctx.EffectFrame carries the source
	// with a zero Stamp). Only registrations created by api:Effect are ended
	// (state.ContinuousEffect.FromEffect); the same source's printed statics
	// are untouched.
	EndEffectSource(source state.ObjID)
	// EndImprintedEffects ends every live continuous-effect registration
	// that an ImprintOnHost$ True Effect imprinted on the named host card
	// (state.ContinuousEffect.ImprintOnHost): the analogue of Forge's
	// `DB$ ChangeZone | Defined$ Imprinted | Origin$ Command | Destination$
	// Exile` exiling the imprinted effect token from the Command zone
	// (Superior Foes of Spider-Man's "until you exile another card with
	// this creature" -- the second dig's trigger exiles the FIRST effect's
	// token before the new dig's Effect registers). rules.Engine implements
	// it as an in-place drop of its registry, rebuilt by re-execution on
	// replay; the effects test double mirrors it.
	EndImprintedEffects(source state.ObjID)
}

// HostRestrictions is the restriction role: the rules-side "can't" gates an effect
// consults before it acts.
type HostRestrictions interface {
	// RegenerationDisallowed reports whether an Effect-registered
	// CantRegenerate restriction makes id unable to be regenerated (Incinerate's
	// "can't be regenerated this turn"). Consulted by ReplaceDestruction before
	// it would consume a shield, so a banned regeneration is never honoured.
	// Implemented by rules.Engine against its continuous-effect registry; the
	// effects test double reports false (no engine to consult). Task ce1.
	RegenerationDisallowed(id state.ObjID) bool
	// SacrificeBlocked reports whether id is forbidden from being sacrificed
	// at all this turn — an Effect-registered CantSacrifice restriction (Call
	// for Aid's "You can't sacrifice those creatures this turn") or a face
	// CantSacrifice static (the simple Card.Self carriers). Consulted at every
	// sacrifice candidate choke point (effSacrifice's eligible pool and
	// object-target paths, effSacrificeAll, the cast/activation/mana/ward/unless
	// Sac-cost candidate walks) so a blocked permanent is never offered and
	// never taken. forCost (vc-static1) is the call site's provenance: the
	// cost-driven Sac-cost walks pass true, the effect-driven paths (this
	// package's callers) false, so a static's ForCost$/ValidCause$ scoping can
	// read the split. The rules-side cost walks call the engine's cause-aware
	// sacrificeBlockedForCost instead (task cantsac1), which carries the
	// pending cast/activation so a cost-path ValidCause$ can be evaluated.
	// Implemented by rules.Engine (rules/layers.go); the
	// effects test double reports false (no engine to consult).
	SacrificeBlocked(id state.ObjID, forCost bool) bool
	// ExileBlocked reports whether id is forbidden from being exiled by the
	// given cause -- an Effect-registered CantExile restriction or a face
	// CantExile static (The Master, Multiplied: "Triggered abilities you
	// control can't cause you to ... exile creature tokens you control").
	// Consulted at every effect-driven exile candidate choke point
	// (effChangeZone's object path, effChangeZoneAll's sweep and the shared
	// ChangeZone settle) so a blocked permanent is never exiled. forCost is
	// the call site's provenance exactly as on SacrificeBlocked: the
	// effect-driven paths (this package's callers) pass false, so a static's
	// ForCost$ False scoping reads the split -- a cost-driven battlefield
	// exile calls the engine's cause-aware exileBlockedForCost instead.
	// Implemented by rules.Engine (rules/layers.go); the effects test double
	// reports false (no engine to consult).
	ExileBlocked(id state.ObjID, forCost bool) bool
	// CounterAllowed reports whether a spell or ability may be countered.
	// Counter replacement effects are rules, not a MoveZone replacement: they
	// stop Counter before it emits the move off the stack.
	CounterAllowed(target, cause state.ObjID) bool
}

// HostReplacements is the replacement role: the CR 614 replacement and modification
// hooks an effect routes its action through.
type HostReplacements interface {
	// SurveilLookExtra reports the additional cards a surveil performed by
	// player p looks at, from the battlefield statics with Mode$ SurveilNum
	// whose ValidPlayer$ admits p ("You may look at an additional two cards
	// each time you surveil"). mandatory is added to the count
	// unconditionally. optional holds ONE entry per OPTIONAL static -- each
	// entry is that static's own Num$ -- in deterministic activeStatics
	// order: each Optional$ True static is an independent may effect the
	// surveilling player accepts or declines on its own (effSurveil poses one
	// multi-select election over the entries), never an all-or-nothing sum.
	// The read reuses rules' canonical activeStatics collector, so a
	// face-down, merged-pile or EffectZone-scoped static is read exactly as
	// every other static mode is. Implemented by rules.Engine
	// (rules/statics.go); the effects test double reports zero/nil (no engine
	// static registry to consult).
	SurveilLookExtra(p state.PlayerID) (mandatory int32, optional []int32)
	// ActionReplaced checks a synthetic pre-action proposal (currently Explore
	// and Connive). The proposal is never logged; true means the whole action
	// was replaced in place. Rules implements replacement matching and runs
	// the body's own action under the replacement guard.
	ActionReplaced(proposal events.Event) bool
	// Scry proposes one scry instruction BEFORE any card of the player's
	// library is looked at (CR 614.4: an R:Event$ Scry replacement applies to
	// the scry action itself), so the proposed count can be adjusted
	// (Kenessos, Priest of Thassa: "scry that many cards plus one") or the
	// whole instruction replaced (Eligeth, Crossroads Augur: "draw that many
	// cards instead"). It returns the surviving instruction's count and
	// proceed=false when a replacement replaced the scry whole -- the caller
	// must then look at and arrange NOTHING. The proposal is never logged;
	// the completed scry's own events.Scry record (carrying the number of
	// cards actually put on the bottom) is emitted later by the rules tier.
	// Rules-implemented because replacement matching lives in the rules tier;
	// the effects test double reports (count, true) unchanged (no engine to
	// consult).
	Scry(p state.PlayerID, source state.ObjID, count int32, sa *cards.SA, target int) (countAfter int32, proceed, pending bool)
	// CascadeReplacement proposes the cascade INSTRUCTION as one replaceable
	// event (CR 614.4; Averna, the Chaos Bloom's R:Event$ Cascade) after the
	// exile-until batch is complete and before any card is bottomed or
	// offered for casting. batch is this invocation's ordered exiled object
	// ids, bound as the body's Defined$ ReplacedCards; residue is the
	// continuation the caller wants run AFTER the replacement body (bottom
	// the rest, then the free-cast election), chained onto the body so a body
	// that suspends at a mid-resolution ask resumes into it. It reports
	// whether a replacement matched (a host with no replacement registry --
	// the effects test double -- reports false, the same discipline as its
	// Scry above). The proposal is never logged.
	CascadeReplacement(source state.ObjID, controller state.PlayerID, batch []state.ObjID, residue *cards.SA) bool
	// CountReplacementProposed holds one synthetic count-bearing action
	// proposal through the replacement registry and returns the rewritten
	// proposal. It is used for the Mill instruction count and ordinary dice
	// roll count; the test double returns it unchanged.
	CountReplacementProposed(ev events.Event) events.Event
	// ReplaceEvent applies a ReplaceEffect body's requested change to the
	// event currently being replaced. It is inert outside replacement
	// resolution; rules owns the event and records the resulting delta.
	ReplaceEvent(name, value string, resolved int32)
}

// HostTargeting is the targeting role: legal-target offers and chooser resolution.
type HostTargeting interface {
	// LegalTargets returns the targets the rules engine would offer for sa.
	// Redirect effects use this shared census rather than duplicating target
	// legality below rules (protection and continuous restrictions included).
	LegalTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA) []state.Target
	// LegalSubTargets is LegalTargets for a SubAbility$'s OWN ValidTgts$ asked
	// while its parent resolves: parent is the parent ability's already-chosen
	// target list, which the census binds for the Targeted*/ParentTarget
	// referents (SpecContext.ParentTargets) -- "exile up to one target
	// Equipment attached to THAT creature".
	LegalSubTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA, parent []state.Target) []state.Target
	// ChooserFor resolves the seat that answers a target ask declared by sa,
	// per Forge's TargetingPlayer$ ("an opponent chooses the target"). The
	// mid-resolution ValidTgts$ asks (chosenTargetsFor, changeZoneChosenTargets)
	// have no rules-tier ask site to consult, so this seam carries the same
	// resolver cast/trigger asks use (rules.Engine.targetAskChooser /
	// targetChooserFromSpec). It is read for the DECISION's Player only: the
	// caller keeps c.Controller as the legality census reference. A host with
	// no resolver (the effects test double) returns c.Controller, the same
	// fail-closed default as an unknown spec.
	ChooserFor(c *Ctx, sa *cards.SA) state.PlayerID
}

// HostTriggers is the trigger role: trigger-mode support and reflexive triggers.
type HostTriggers interface {
	// TriggerModeSupported keeps Effect-created trigger registrations honest:
	// an unknown Mode$ cannot masquerade as an armed, inert promise.
	TriggerModeSupported(mode string) bool
	// QueueReflexiveTrigger puts one CR 603.12 reflexive triggered ability
	// ("When you do, ...": DB$/AB$ ImmediateTrigger's Execute$ body) into the
	// trigger queue, so it goes on the stack with its own targets the next
	// time a player would receive priority instead of resolving inside the
	// ability that spawned it. remembered is the instance's remembered set.
	// It reports false when the host cannot mint the ability from the log
	// (the body is not the resolving source's own Execute$ SVar); the caller
	// then keeps the inline resolution.
	QueueReflexiveTrigger(c *Ctx, execute string, body *cards.SA, remembered []state.Target) bool
}

// HostCastLedger is the cast-history half of the ledger role: what was cast this
// turn, how, and from where.
type HostCastLedger interface {
	// CastThisTurn counts the spells cast this turn by anyone, derived from
	// the event log so a replay that rebuilds the game arrives at the same
	// number (Task 17's Count$ThisTurnCast backing — a copy/Storm count
	// must be replay-derivable, never a live-only engine counter).
	CastThisTurn() int
	// SpellsCastThisTurnMatching counts the spells put on the stack this turn
	// whose caster is you (when the Forge spec carries a You* qualifier) or
	// anyone, and whose object matches the spec. Derived from the event log
	// like CastThisTurn, so a replay derives the same number. This is the
	// Count$ThisTurnCast_<spec> backing (the "first/second spell you cast"
	// cost modifiers and triggers).
	SpellsCastThisTurnMatching(you state.PlayerID, spec string) int
	// SpellsCastThisTurnMatchingExcluding is SpellsCastThisTurnMatching with
	// one object's own cast excluded from the count -- the bare !CastSaSource
	// qualifier's engine reading. Every bare-form carrier's oracle says
	// other/another (Hotheaded Giant's "unless you've cast another red spell
	// this turn", Dream Thief's "another blue spell", Storm Entity's "each
	// other spell cast this turn"), and the resolving spell's own
	// PutOnStack is unavoidably in the window when an ETB gate reads the
	// count, so the qualifier is the count's exclusion of its own ctx source.
	// Derived from the event log like SpellsCastThisTurnMatching.
	SpellsCastThisTurnMatchingExcluding(you state.PlayerID, spec string, exclude state.ObjID) int
	// EachSpellCastThisTurnMatching is the ARGUMENTED !CastSaSource forms'
	// engine side (task castprov2): the object ids of the spells put on the
	// stack this turn matching spec (with the same You*-qualifier scoping
	// and the same single-object exclusion as
	// SpellsCastThisTurnMatchingExcluding), in reverse log order (newest
	// first) — the order is irrelevant to the aggregate reads (a sum).
	// Derived from the event log like SpellsCastThisTurnMatching.
	EachSpellCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID
	// CommanderCastsFromCommandZone counts how many times player p has cast
	// one of THEIR OWN commanders from the command zone this game — the
	// same provenance the CR 903.8 commander tax counts (rules/cast.go's
	// recordCmdCast maintains the parallel CmdCasts slice from the same
	// PutOnStack events). Whole-game scope, log-derived, so a replay that
	// rebuilds the log arrives at the same number. This backs the
	// Count$TotalCommanderCastFromCommandZone head (Thunderclap Drake's
	// copy count, Commanders Insignia's P/T, Henzie's blitz discount; 17
	// corpus carriers) — never a live-only engine counter.
	CommanderCastsFromCommandZone(p state.PlayerID) int32
	// WasCastFromHandByYou reports whether card obj was cast from ITS OWN
	// CONTROLLER's hand by that controller — the Count$wasCastFromYourHandByYou
	// branch head backing (the Myojin cycle's etbCounter CheckSVar$ gate:
	// "enters with a divinity counter on it if you cast it from your hand")
	// and the Card.wasCastFromYourHandByYou filter predicate the corpus's
	// "if you cast it from your hand" ETB trigger specs read. An ordinary
	// hand-origin cast carries no CastFlags bit (the flags mark alternative
	// costs and origins only), so the answer is derived from the event log:
	// the object's latest PutOnStack event names the cast that put it on the
	// stack, whose From is the zone it was cast FROM and whose Player is the
	// caster. Derived from the log like CastThisTurn, so a replay derives
	// the same answer; a card never put on the stack (cheated into play)
	// reads false.
	WasCastFromHandByYou(obj state.ObjID, p state.PlayerID) bool
	// CostMovesInWindow reports the object ids of the cost actions of kind k
	// (events.CostMoveKind) recorded in the activation window of the resolving
	// object obj — the cost parts obj's own activation paid, read off the
	// event log the way WasCastFromHandByYou reads cast provenance. It is the
	// channel behind the ConditionDefined$ Discarded (events.CostMoveDiscard —
	// Moria Scavenger), Returned (events.CostMoveReturn — Wonderscape Sage)
	// and Collected (events.CostMoveEvidence — Analyze the Pollen, Crimestopper
	// Sprite) groups. One method, not one per kind, so a new cost-provenance
	// window reuses the walk instead of widening Host again. Empty when the
	// window holds none; a replay derives the same answer from the same log.
	CostMovesInWindow(obj state.ObjID, k events.CostMoveKind) []state.ObjID
	// WasCastFromHand reports whether card obj's LATEST cast came from a
	// hand — ANY caster's hand — the bare wasCastFromYourHand filter family's
	// backing (task castprov3: the "from anywhere other than your hand"
	// carriers whose scripts spell the predicate without the ByYou suffix —
	// Vega the Watcher's trigger, Otterball Antics' ConditionPresent$ gate,
	// See the Truth's Count$ branch head, Approach of the Second Sun's
	// Count$ValidStack). Every carrier that needs player scoping supplies it
	// elsewhere (ValidActivatingPlayer$ You, YouCtrl, wasCastByYou in the
	// same spec), measured over the 46 raw carrier files. Derived from the
	// event log like WasCastFromHandByYou: the object's latest PutOnStack
	// event names the cast that put it on the stack, whose From is the zone
	// it was cast FROM; a copy was never cast (the same IsCopy guard the
	// ByYou read takes); a card never put on the stack (cheated into play)
	// reads false; latest-cast-wins.
	WasCastFromHand(obj state.ObjID) bool
	// WasCastFromExile reports whether card obj's LATEST cast came from
	// EXILE — the Count$wasCastFromExile branch head's backing (task
	// wascastfrom: the "if this spell was cast from exile" carriers —
	// Delayed Blast Fireball's 5-instead-of-2, Ultimate Magic's
	// prevent-effect gate, Lifestream's Blessing's doubled life gain).
	// Foretell, warp and may-play-from-exile casts carry no origin CastFlags
	// bit, so the provenance is the event log: the object's latest
	// PutOnStack event names the cast, whose From is the zone it was cast
	// FROM; a copy was never cast; a card never put on the stack reads
	// false; latest-cast-wins — the same discipline the hand reads take.
	// Derived from the log, so a replay derives the same answer.
	WasCastFromExile(obj state.ObjID) bool
	// WasCast reports whether card obj is a CAST SPELL in the Forge
	// Card.wasCast() sense (castFrom != null) -- the third conjunct of the
	// Count$IfCastInOwnMainPhase branch head (task ifcastmain1). A card
	// moved to the stack as part of casting is cast; a copy (IsCopy) is
	// never cast; a permanent cheated into play reads false. Unlike the
	// hand-provenance reads, an announced-but-not-yet-pushed cast IS cast:
	// Forge sets castFrom BEFORE setupTargets evaluates TargetMax$, and the
	// pending CR 601.2c announcement ask must therefore read true (the
	// engine's pending-cast field covers that window). Derived from the event
	// log plus the live pending cast, so a replay derives the same answer.
	WasCast(obj state.ObjID) bool
	// SpellsCastThisTurnBy counts the spells put on the stack this turn by
	// player p — the per-caster projection of CastThisTurn, derived from the
	// event log so a replay derives the same number. This is the
	// PlayerCount<group>$Condition<N> SpellsCastThisTurn backing (Ertai's
	// Scorn / Mindbreak Trap / Whiplash Trap: "for each opponent who cast
	// two or more spells this turn"), the per-member property a player-count
	// condition compares (SpellsCastThisTurnMatching cannot answer it because
	// its scope is a Forge spec's You* qualifier, not the counted member).
	SpellsCastThisTurnBy(p state.PlayerID) int
}

// HostTurnLedger is the turn-history half of the ledger role: life, damage, card and
// counter movement, and combat history this turn and last.
type HostTurnLedger interface {
	// LifeLostThisTurn reports the total life player p lost THIS TURN — the
	// sum of every LifeChange below zero since the last TurnChange, derived
	// from the event log so a replay derives the same number. This is the
	// Count$LifeOppsLostThisTurn backing (Rakdos, Lord of Riots' cost
	// reduction): the Count$ head sums it over the controller's opponents.
	LifeLostThisTurn(p state.PlayerID) int32
	// DamageTakenThisTurn reports the total damage player p was dealt THIS
	// TURN — the sum of every player-targeted Damage event (Kind Damage
	// with the recipient in Player and Obj 0) since the last TurnChange,
	// derived from the event log so a replay derives the same number. This
	// is the TargetedPlayer$DamageThisTurn backing (Knollspine Dragon's
	// "draw cards equal to the damage dealt to target opponent this turn");
	// damage a redirect moved onto a PERMANENT (ev.Obj != 0) reads nowhere
	// here, exactly as it should not.
	DamageTakenThisTurn(p state.PlayerID) int32
	// LifeGainedThisTurn reports the total life player p GAINED this turn —
	// the sum of every LifeChange above zero since the last TurnChange,
	// derived from the event log so a replay derives the same number. This is
	// the Count$LifeYouGainedThisTurn backing (the "At the beginning of each
	// end step, if you gained 4 or more life this turn" family — Angelic
	// Accord, Resplendent Angel, Valkyrie Harbinger — whose CheckSVar$ gate
	// reads the count), the mirror of LifeLostThisTurn.
	LifeGainedThisTurn(p state.PlayerID) int32
	// CountersRemovedThisTurn reports how many counters of kind player p PAID
	// OR LOST this turn — the sum of every negative-Amount PlayerCounterChange
	// naming the kind since the last TurnChange, derived from the event log so
	// a replay derives the same number. This is the Count$CountersRemovedThisTurn
	// backing (Blaster Hulk's per-{E} cast discount, Izzet Generatorium's
	// "activate only if you've paid or lost four or more {E} this turn" gate):
	// a payment and a loss both leave the player's pool through the ONE event
	// shape a grant uses — a negative PlayerCounterChange (rules/mana.go's
	// PayEnergy settle) — so the removals are log-visible exactly like the
	// life totals LifeLostThisTurn folds. Kind matching is case-insensitive
	// (the same read the YourCounters heads take). Object-counter removals (a
	// permanent losing counters) are NOT folded here — the head's object-spec
	// form is a separate, unimplemented shape.
	CountersRemovedThisTurn(p state.PlayerID, kind string) int32
	// CountersAddedThisTurn sums final positive object-counter placements this
	// turn matching the count head's kind, actor and object specifications.
	CountersAddedThisTurn(kind, actorSpec, objectSpec string, sc SpecContext) int32
	// CombatDamageToPlayersThisTurn reports every instance of combat damage
	// dealt to a PLAYER so far this turn, in assignment order. It is the
	// PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy
	// backing (Lost Monarch of Ifnir's "if a player was dealt combat damage
	// by a Zombie this turn", Estinien Varlineau's "the number of your
	// opponents who were dealt combat damage by CARDNAME or a Dragon this
	// turn", Blitzball's legendary-creature activation gate).
	//
	// A Damage event to a player carries no source (events.Event has no
	// source field -- see the CmdDamage Kind's own note), so unlike the
	// log-folded LifeLostThisTurn this cannot be derived from the event log;
	// the engine captures it at the combat-damage site (rules/combat.go's
	// runCombatAssignments), engine-side and NO-EVENT, and re-derives it
	// identically on every rebuild path (replay, undo, DVR) because those
	// re-execute the engine. Only damage that LANDED is recorded (a
	// protection Note is not damage), and only the PLAYER branch: combat
	// damage to a permanent is not the property any carrier reads.
	CombatDamageToPlayersThisTurn() []CombatDamageHit
	// CardsDiscardedThisTurn reports how many cards player p discarded THIS
	// TURN — every events.IsDiscard move since the last TurnChange, the cost
	// form (events.DiscardCost) included, derived from the event log so a
	// replay derives the same number. This is the
	// PlayerCountPropertyYou$CardsDiscardedThisTurn backing (Ambergris
	// Citadel Agent's "X = cards you discarded this turn" behind a
	// Cost$ Discard<1/Hand> Draw<2/You> body). A cost-form discard event
	// carries no Player field, so the fold reads the discarded object's
	// owner there — a cost discard is paid from the payer's own hand (CR
	// 118.2a), so the owner is the discarder.
	CardsDiscardedThisTurn(p state.PlayerID) int32
	// CardsDrawnThisTurn reports how many cards player p DREW this turn —
	// every events.Draw naming p since the last TurnChange, derived from the
	// event log so a replay derives the same number. This is the
	// PlayerCount<group>$Condition<N> CardsDrawn backing (Smuggler's Share's
	// "draw a card for each opponent who drew two or more cards this turn")
	// and the per-player property read a player-count condition compares.
	// Draws by effect, by the draw step and by an opening hand all emit the
	// same event, so an opening-hand draw inside the first turn's window is
	// counted, exactly as Forge's cardsDrawnThisTurn list is.
	CardsDrawnThisTurn(p state.PlayerID) int32
	// ScriedThisTurn / SurveilledThisTurn report how many scry / surveil
	// instructions player p completed this turn -- every events.Scry /
	// events.Surveil record naming p since the last TurnChange, derived from
	// the event log so a replay derives the same number. They back
	// Count$YouScryThisTurn and Count$YouSurveilThisTurn.
	ScriedThisTurn(p state.PlayerID) int32
	SurveilledThisTurn(p state.PlayerID) int32
	// WasDealtNoncombatDamageThisTurn / WasDealtNoncombatDamageLastTurn
	// report whether player p was dealt noncombat damage (any landed damage
	// outside a combat damage assignment) during the current turn / the
	// previous turn. They back the player properties
	// HasPropertywasDealtNonCombatDamageThisTurn / ...LastTurn (Grim
	// Repriser, Whiplash Wordsmith, Command the Stage).
	WasDealtNoncombatDamageThisTurn(p state.PlayerID) bool
	WasDealtNoncombatDamageLastTurn(p state.PlayerID) bool
	// StartingLife reports this game's opening life total (Config's
	// 0-means-20 convention already resolved at genesis). It is the
	// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife
	// backing (Anya, Merciless Angel's per-opponent "less than half their
	// starting life total" and Game Over's relative half-starting-life
	// threshold), the ONE game-wide value the relative-player property reads.
	// Captured at genesis, so a replay derives the same number.
	StartingLife() int32
	// TurnsTaken reports how many of the game's turns have begun with p as
	// the active player, INCLUDING the turn in progress when it is p's —
	// Forge's Player.getTurns backing (Serra Avenger's
	// Count$YourTurns: "your first, second, or third turns of the game").
	// Derived from the event log like LifeLostThisTurn, so a replay derives
	// the same number; a turn that begins is one TurnChange event naming p.
	TurnsTaken(p state.PlayerID) int32
	// AttackersThisTurn counts the attackers declared THIS turn — the sum of
	// every DeclareAttackers event's attacker list since the last TurnChange,
	// derived from the event log so a replay derives the same number. This is
	// the Count$AttackersDeclared backing (the Raid family's "attacked this
	// turn" read: Bloodsoaked Champion's CheckSVar$ activation gate and ten
	// ConditionCheckSVar$ bodies).
	AttackersThisTurn() int
	// AttackersDeclaredThisTurn lists the objects declared as attackers THIS
	// turn, each once, in declaration order -- the same DeclareAttackers log
	// fold AttackersThisTurn counts (Count$CreaturesAttackedThisTurn).
	AttackersDeclaredThisTurn() []state.ObjID
	// LifeLostLastTurn reports the total life player p lost during the
	// PREVIOUS turn: the negative LifeChanges between the last two
	// TurnChange events of the log (PlayerCount*$LifeLostLastTurn). Zero on
	// the first turn.
	LifeLostLastTurn(p state.PlayerID) int32
	// AttackedDuringLastTurn reports whether player q declared an attack on
	// player defender during q's most recent COMPLETED turn (a
	// DeclareAttackers naming defender inside that turn's TurnChange window
	// of the log) -- Forge's attackedYouTheirLastTurn player property
	// (Avenge's "if a player attacked you during their last turn").
	AttackedDuringLastTurn(q, defender state.PlayerID) bool
}

// HostConditions is the condition role: the named ability-word conditions rules
// evaluates for an effect.
type HostConditions interface {
	// CommanderIdentityColourCount reports how many colours seat p's
	// commander colour identity names (the WUBRG-ordered union of every
	// commander's Card.ColourIdentity, read off state.Player.Commanders —
	// genesis bookkeeping the replay rebuilds in Config order, so the count
	// is replay-derivable like TurnsTaken). This is the Count$ColorsColorIdentity
	// backing (War Room's fixed "Pay life equal to the number of colors in
	// your commanders' color identity"); an empty identity (no commander,
	// or a colourless one) is a real, resolvable 0.
	CommanderIdentityColourCount(p state.PlayerID) int
	// RevoltHolds reports CR 702.38's ability-word state: a permanent the
	// controller CONTROLLED (not owned) left the battlefield this turn. This
	// is the bare `Condition$ Revolt` gate (Decommission's DB$ GainLife) and
	// the Count$Revolt.<yes>.<no> branch head (Lifecraft Cavalry's etbCounter
	// gate, Fatal Push's destroy bound) backing; rules.Engine implements it
	// as the same revoltThisTurn event-log scan its own replacement/trigger
	// Revolt$ clauses read, so every spelling answers identically and a
	// replay derives it from the log like the other this-turn helpers.
	RevoltHolds(p state.PlayerID) bool
	// DeliriumHolds reports the Delirium ability-word state: the controller's
	// graveyard holds four or more distinct core card types (Artifact,
	// Battle, Creature, Enchantment, Instant, Kindred, Land, Planeswalker,
	// Sorcery -- the same census the "Delirium —" cost prompts count). This
	// is the bare `Condition$ Delirium` gate (Descend upon the Sinful's
	// DB$ Token) backing; rules.Engine implements it as the same
	// graveyardCardTypeCount census its replacement Delirium$ clause, the
	// Continuous static gate (rules/layers.go) and the ability-offer gate
	// (rules/legal.go) read, so every Delirium spelling answers identically
	// and a replay derives it from the folded state like the other
	// zone-census helpers.
	DeliriumHolds(p state.PlayerID) bool
	// MetalcraftHolds reports whether the controller has three or more artifacts;
	// bare Condition$ gates share rules.Engine's census with static and offer gates.
	MetalcraftHolds(p state.PlayerID) bool
}

// HostAsk is the ask role: posing a decision and suspending the resolution
// that waits on its answer.
type HostAsk interface {
	// Ask poses a decision in the middle of a resolution. It sets the host's
	// pending decision, sets the mid-resolution resume state, and returns
	// true. A true return tells the calling effect to stop and wait: the
	// resolution is suspended, and the answered decision re-enters it
	// (rules' Engine.Ask is what runs the resume continuation). false means
	// the host cannot ask now — an effects-package test double, or a
	// rules-internal context with no engine to drive — and the calling
	// effect falls back to its deterministic stand-in (R-9). M2d-2.
	Ask(d *decision.Decision) bool
	// TypeChoices returns the owner-scoped CREATURE-type option list a
	// ChooseType ask offers its chooser for an absent Type$ or Type$ Creature
	// (task ct1) — the SAME list the cast-time "as this enters" type ask
	// builds (rules/etbOptions' "type" arm), so the two asks and the no-ask
	// fallback can never disagree about what a creature-type choice ranges
	// over. validTypes/invalidTypes carry the SA's ValidTypes$/InvalidTypes$
	// filters (Dawn-Blessed Pennant's Type$ Creature + ValidTypes$ restricts
	// the choice to its eight named types); both empty means the whole
	// vocabulary. The other categories (Basic Land, Card, Land, Planeswalker,
	// Shared, CreatureInTargetedDeck) no longer reach this method: the asking
	// primitive builds their option lists from immutable game state itself
	// (effects/type_choices.go), and an absent or non-creature category here
	// still yields nil as a defensive guard.
	TypeChoices(chooser state.PlayerID, category, validTypes, invalidTypes string) []decision.Option
	// Suspended reports whether the resolution is currently suspended on a
	// mid-resolution ask — Ask returned true and set the host's resume state,
	// which has not yet been cleared by the answer arriving. effects.Resolve
	// calls it after every sub-ability in a chain so that a suspended ask
	// STOPS the chain rather than running the sub-abilities beneath it (B1: a
	// chained SA such as Thoughtseize's Discard | SubAbility$ DBLoseLife must
	// not fire its SubAbility on the initial pass, before the answer exists,
	// nor again on the resume pass — the resume re-enters at the asking SA
	// and walks the rest of the chain exactly once). An effects-package test
	// double that cannot suspend reports false; the rules engine reports
	// e.resume != nil, which Ask sets and the handled answer clears.
	Suspended() bool
}
