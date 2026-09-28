// Package effects implements the primitives Forge card scripts reference. It
// reaches the engine through the Host interface, so it never imports rules and
// the dependency graph stays acyclic.
package effects

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CombatDamageHit is one instance of combat damage dealt to a player this
// turn, as captured by the engine at the combat-damage site. Card/FaceIdx/
// Controller describe the dealing creature as it was at damage time: the
// face pointer is stable (a shared pointer out of the Config's decks), so a
// token that died before the read point is still matchable, exactly the
// shallow-snapshot precedent rules/replacement.go's tokenSnapshot takes.
// Source is the dealing object's id, which anchors Forge's `Card.Self` spec
// to the resolving trigger's source.
type CombatDamageHit struct {
	Player     state.PlayerID
	Source     state.ObjID
	Card       *cards.Card
	FaceIdx    uint8
	Controller state.PlayerID
	Amount     int32
}

// Host is everything an effect may do to a game: read it, and propose events.
// Deliberately tiny — an effect that needs more is a sign the primitive is
// doing rules work that belongs in the rules package.
type Host interface {
	// Game returns the live match state for reading. The returned *state.Game
	// must never be written to directly: every state mutation goes through
	// Emit (which routes through events.Apply), which is what keeps the event
	// log a complete description of the match.
	Game() *state.Game
	// ObjectColors returns the object's live layer-5 colours when it is on the
	// battlefield, and its face/CDA colours in other zones.
	ObjectColors(*state.Object) string
	// ObjectText returns the object's CURRENT derived rules text (CR 613.1d)
	// -- printed Oracle after every layer-3 text effect already registered on
	// it, in timestamp order. rules.Engine implements it through the same
	// Derived.Text render the engine's own Text accessor uses; the effects
	// test double returns the printed Oracle. api:ExchangeTextBox reads it to
	// exchange the text boxes AS THEY EXIST at resolution, so a prior
	// ChangeText substitution is carried across rather than discarded.
	ObjectText(*state.Object) string
	// ObjectKeywords returns the object's CURRENT derived keyword list (CR
	// 613.1f), printed and granted alike, as an owned copy. api:ExchangeTextBox
	// reads it alongside ObjectText so an exchanged text box swaps the
	// keywords the other object's box carries (CR 612.1: a text box includes
	// its abilities), the same AS-THEY-READ-at-resolution capture ObjectText
	// takes; a layer-6 companion then wipes the object's own keywords and
	// grants these. rules.Engine implements it as a copy of the Derived
	// keyword stream (whose scratch buffer must not be aliased); the effects
	// test double returns the printed face's keywords.
	ObjectKeywords(*state.Object) []string
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
	// Rand is the engine's seeded generator. Effects that need randomness must
	// use it and nothing else, or replay breaks.
	Rand(n int) int
	// ShuffleLibrary returns a Fisher-Yates permutation of order. The engine
	// owns hypothetical shuffle planning here; effects still emit the sole
	// state-mutating Shuffle event with the returned order.
	ShuffleLibrary(state.PlayerID, []state.ObjID) []state.ObjID
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
	// TriggerModeSupported keeps Effect-created trigger registrations honest:
	// an unknown Mode$ cannot masquerade as an armed, inert promise.
	TriggerModeSupported(mode string) bool
	// TypeChoices returns the owner-scoped CREATURE-type option list a
	// ChooseType ask offers its chooser for an absent Type$ or Type$ Creature
	// (task ct1) — the SAME list the cast-time "as this enters" type ask
	// builds (rules/etbOptions' "type" arm), so the two asks and the no-ask
	// fallback can never disagree about what a creature-type choice ranges
	// over. The other categories (Basic Land, Card, Land, Planeswalker,
	// Shared, CreatureInTargetedDeck) no longer reach this method: the asking
	// primitive builds their option lists from immutable game state itself
	// (effects/type_choices.go), and an absent or non-creature category here
	// still yields nil as a defensive guard.
	TypeChoices(chooser state.PlayerID, category string) []decision.Option
	// RegisterControl records one GainControl effect with the lifetime its
	// LoseControl$ names (CR 611.2b "for as long as", CR 514.2 end of turn),
	// so the engine can end it through a ControlChange event the moment that
	// duration ends. A grant with no duration is permanent: it supersedes
	// every earlier control effect on the object (CR 613.7 timestamp order).
	RegisterControl(ControlGrant)
	// LegalTargets returns the targets the rules engine would offer for sa.
	// Redirect effects use this shared census rather than duplicating target
	// legality below rules (protection and continuous restrictions included).
	LegalTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA) []state.Target
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
	// ExploreReplaced reports whether a replacement effect replaces the
	// named explorer's explore (R:Event$ Explore — Topography Tracker's
	// "instead it explores, then it explores again", Twists and Turns'
	// "instead you scry 1, then that creature explores") and, when one
	// does, RESOLVES that replacement body in place: the original explore
	// is replaced whole (the caller must not reveal, counter or move
	// anything for it) and the body's own explores run under the
	// replacement guard, so they cannot re-match the same replacement (the
	// same once-per-event discipline the CreateToken path applies).
	// Rules-implemented because replacement matching lives in the rules
	// tier; the effects test double reports false (no engine to consult).
	// No replacement applies when the engine is already inside one (the
	// emit path skips replacement application there, and the body's own
	// explores are fresh events).
	ExploreReplaced(explorer state.ObjID) bool
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
	// RollDiceProposed is the pre-roll replacement boundary effRollDice
	// consults before any die of ONE roll action is rolled (CR 614.4): the
	// host holds a synthetic events.RollDice proposal out to the R:Event$
	// RollDice replacement class and returns the rewritten dice count and
	// ignored-low count (the proposal seeds the ignored-low base from the
	// rolling body's own IgnoreLower$, so a replacement's
	// ReplaceCount$Ignore/Plus.1 is one ADDITIONAL low result). A host with
	// no replacement registry (the effects-package double) returns the
	// proposal unchanged, the same discipline as its Scry above. Neither
	// suspends: the corpus's supported replacement bodies are ReplaceEffect
	// rewrites, which never ask.
	RollDiceProposed(p state.PlayerID, source state.ObjID, amount, ignore int32) (int32, int32)
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
	// BasePower is the layer-7b result: set effects apply, pumps and counters do not.
	BasePower(id state.ObjID) int32
	Toughness(id state.ObjID) int32
	IsCreature(id state.ObjID) bool
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
	// DiscardedInWindow reports the object ids of the COST discards
	// (events.DiscardCost) recorded in the activation window of the
	// resolving object obj — the cost parts obj's own activation paid,
	// read off the event log the way WasCastFromHandByYou reads cast
	// provenance. The ConditionDefined$ Discarded group's cost-discard
	// channel (Moria Scavenger). Empty when the window holds none; a
	// replay derives the same answer from the same log.
	DiscardedInWindow(obj state.ObjID) []state.ObjID
	// ReturnedInWindow reports the object ids of the COST returns
	// (events.IsReturnCost) recorded in the activation window of the
	// resolving object obj — the permanents obj's own Return<N/Spec> cost
	// parts moved to their owner's hand, read off the event log the same way
	// DiscardedInWindow reads cost discards. The ConditionDefined$ Returned
	// group's channel (Wonderscape Sage). Empty when the window holds none;
	// a replay derives the same answer from the same log.
	ReturnedInWindow(obj state.ObjID) []state.ObjID
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
	// SpellsCastThisTurnBy counts the spells put on the stack this turn by
	// player p — the per-caster projection of CastThisTurn, derived from the
	// event log so a replay derives the same number. This is the
	// PlayerCount<group>$Condition<N> SpellsCastThisTurn backing (Ertai's
	// Scorn / Mindbreak Trap / Whiplash Trap: "for each opponent who cast
	// two or more spells this turn"), the per-member property a player-count
	// condition compares (SpellsCastThisTurnMatching cannot answer it because
	// its scope is a Forge spec's You* qualifier, not the counted member).
	SpellsCastThisTurnBy(p state.PlayerID) int
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
	// Ask poses a decision in the middle of a resolution. It sets the host's
	// pending decision, sets the mid-resolution resume state, and returns
	// true. A true return tells the calling effect to stop and wait: the
	// resolution is suspended, and the answered decision re-enters it
	// (rules' Engine.Ask is what runs the resume continuation). false means
	// the host cannot ask now — an effects-package test double, or a
	// rules-internal context with no engine to drive — and the calling
	// effect falls back to its deterministic stand-in (R-9). M2d-2.
	Ask(d *decision.Decision) bool
	// SuspendUnless records the unless-cost outcome of an SA whose BODY
	// suspended on a mid-resolution ask of its own (the gate had already
	// resolved when the body asked): rules re-enters that asking SA with a
	// fresh Ctx, so without this record the gate would re-pose its pay ask
	// and the body would run again from the top — a livelock for any asking
	// body under an UnlessCost$ (Rhystic Study's pay-or-draw, a unless-gated
	// Dig's search). paid is the outcome the suspended pass resolved; the
	// re-entry pass consumes the recorded marker instead of asking again.
	SuspendUnless(sa *cards.SA, paid bool)
	// SetResolutionTargetControllerLKI publishes the target-controller LKI
	// captured at the start of a Resolve chain to the host, and returns the
	// value it replaced so the caller can restore it (rules' Ask copies the
	// published map onto the pending resumePoint). A target may leave the
	// battlefield before a chained TokenOwner$ TargetedController runs;
	// events.Apply resets a departed object's live Controller to Owner, so a
	// resumed continuation -- which rebuilds its Ctx from the stack object's
	// targets -- needs the controller snapshot, not the live object. The
	// publish/restore bracket is what scopes it to the innermost running
	// chain: a nested Resolve with a different Ctx restores the outer map on
	// return. An effects-package test double may keep the no-op form.
	SetResolutionTargetControllerLKI(map[state.ObjID]state.PlayerID) map[state.ObjID]state.PlayerID
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
	// SuspendContinuation reports that a Resolve loop has just suspended (its
	// host reported Suspended() after running the sub-ability at sa) and is
	// about to return, so the chain it was walking must resume at sa.Sub once
	// the pending answer and any deeper continuations are done. effects.Resolve
	// calls it at EVERY loop level that suspends, including the innermost
	// (the level whose sa is the pending ask's ResumeSA): a host records the
	// enclosing levels (sa != the pending ResumeSA) as outer continuations and
	// drops the innermost one, because re-entering the pending ask's own SA
	// already walks sa.Sub. A host that never suspends (an effects-package
	// double, where Ask returns false) never sees this call.
	SuspendContinuation(sa *cards.SA)
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
	// ReplaceEvent applies a ReplaceEffect body's requested change to the
	// event currently being replaced. It is inert outside replacement
	// resolution; rules owns the event and records the resulting delta.
	ReplaceEvent(name, value string, resolved int32)
	// CounterAllowed reports whether a spell or ability may be countered.
	// Counter replacement effects are rules, not a MoveZone replacement: they
	// stop Counter before it emits the move off the stack.
	CounterAllowed(target, cause state.ObjID) bool
	// SuspendRepeatOptional reports that a RepeatOptional$ body suspended at
	// a mid-resolution ask. The host must re-enter the repeat after the body
	// answer completes, preserving the next-iteration cursor.
	SuspendRepeatOptional(sa *cards.SA, next int32)
	// SuspendRepeat reports that one iteration of a RepeatEach loop suspended
	// at a mid-resolution ask. The host must bind the suspended iteration's
	// Remembered to the pending ask (and to the iteration's own continuation
	// frames) and resume the loop at the next subject once they complete,
	// rather than dropping the remaining subjects (CR 608.2c). The Resolve
	// loop enclosing the RepeatEach reports that SA through
	// SuspendContinuation next; the host drops that report, because the loop
	// frame re-enters the RepeatEach itself and so walks its Sub.
	SuspendRepeat(RepeatSuspension)
	// SuspendCharmRest reports that a Charm's mode loop (effCharm's generic
	// loop, charmDistinctTargetRun or charmCrossModeRun) suspended mid-mode:
	// sa is the Charm's own SA and rest the remaining chosen mode names in
	// execution order (EMPTY when the suspended mode was the last chosen
	// one). The host records a continuation that re-enters the Charm with
	// Ctx.Modes = rest once the answered ask's own chain completes — the
	// remaining modes must not run while the suspension is live. An empty
	// rest still reports, so the Charm re-enters (running no further mode)
	// and walks its own Sub rather than the enclosing loop recording a plain
	// continuation at a nil Sub that degrades to a no-sub-ability Note. The
	// Resolve loop enclosing the Charm reports that same SA through
	// SuspendContinuation next; the host drops that report (the charm frame
	// re-enters the Charm itself), which is why the reporter marks it the way
	// SuspendRepeat marks a RepeatEach.
	SuspendCharmRest(sa *cards.SA, rest []string)
	// SuspendVillainousRest reports that a VillainousChoice's chosen body
	// suspended on a nested mid-resolution ask (for example Damocles Base's
	// DBSac sacrifice picker) with victims still to process. sa is the
	// VillainousChoice's own SA and rest carries the ordered Defined$ victim
	// list plus the index of the NEXT victim to ask. The host records a
	// continuation that re-enters the VillainousChoice with that cursor once
	// the answered ask's own chain completes, so the remaining victims are
	// still asked rather than dropped. The Resolve loop enclosing the
	// VillainousChoice reports the same SA through SuspendContinuation next;
	// the host drops that report (the villainous frame re-enters the
	// primitive itself), the SuspendCharmRest convention.
	SuspendVillainousRest(sa *cards.SA, rest VillainousRest)
	// SuspendGenericChoiceRest reports that a multi-player api:GenericChoice's
	// chosen body suspended on a nested mid-resolution ask with choosers still
	// to ask. sa is the GenericChoice's own SA and rest carries the ordered
	// Defined$ chooser list plus the index of the NEXT chooser to ask. The
	// host records a continuation that re-enters the GenericChoice with that
	// cursor once the answered ask's own chain completes, so the remaining
	// choosers are still asked and their chosen bodies run rather than being
	// dropped. The Resolve loop enclosing the GenericChoice reports the same
	// SA through SuspendContinuation next; the host drops that report (the
	// GenericChoice frame re-enters the primitive itself), the
	// SuspendCharmRest convention.
	SuspendGenericChoiceRest(sa *cards.SA, rest GenericChoiceRest)
	// SuspendFlipRest reports that a DB$ FlipCoin loop suspended inside a
	// per-flip sub-ability (FlipUntilYouLose$ or Amount$ > 1) with flips still
	// owed. rest carries the flip cursor: the flippers not yet processed and
	// the current flipper's next iteration. The host records a continuation
	// that re-enters the FlipCoin SA itself with Ctx.FlipRest = rest once the
	// answered ask's own chain completes, so the loop resumes rather than
	// abandoning the remaining flips. The Resolve loop enclosing the FlipCoin
	// reports that same SA through SuspendContinuation next; the host drops
	// that report, the SuspendCharmRest convention.
	SuspendFlipRest(sa *cards.SA, rest FlipRest)
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

// RepeatCursor is a RepeatEach loop re-entered after an iteration suspended:
// the subjects captured when the loop started (never re-derived mid-loop),
// the index of the next subject, and the completed iteration's final
// Remembered so the objects that iteration remembered outlive it.
type RepeatCursor struct {
	SA       *cards.SA
	Subjects []state.Target
	Next     int
	Last     []state.Target
	HasLast  bool
	// Election marks a cursor parked on a RepeatEach
	// RepeatOptionalForEachPlayer$ election rather than on a completed body.
	// Next is the subject whose offer was posed; the answer rides
	// Ctx.RepeatEachOptional on re-entry (Accept false skips that subject's
	// body and continues at Next+1).
	Election bool
	// ChooseOrder marks a cursor parked on a RepeatEach ChooseOrder$ loop's
	// one-before-the-loop ordering ask rather than on a body or an election.
	// Next is 0 (no iteration has run); the answer permutes Subjects before
	// the first body, and the reordered slice then rides every later
	// cursor, so the order a body suspension carries is the chosen one.
	ChooseOrder bool
}

// RepeatSuspension is what effRepeatEach reports when an iteration asks.
// Body is the suspended iteration's Remembered (the loop subject plus
// anything the iteration remembered before asking); Subject is that
// iteration's current subject (the Imprinted binding); Outer and Chosen are
// the RepeatEach resolution's own bindings, restored when the loop re-enters.
type RepeatSuspension struct {
	RepeatCursor
	Body        []state.Target
	Subject     state.Target
	Outer       []state.Target
	Chosen      []state.Target
	ChosenValid bool
	// VoteCounts is a deep copy of the outer resolution's Ctx.VoteCounts at
	// the moment an AmountFromVotes$ iteration suspended. The tally is
	// resolution-local (the api:Vote that built it is a prior chain link), so
	// the fresh Ctx a resume rebuilds would otherwise lose it and every
	// frame that re-derives "Votes" would read an unbound/zero value. The
	// host carries this snapshot on the same continuation frame as the loop
	// cursor, so both the suspended iteration's own body and the still-owed
	// later iterations re-bind the right per-subject tally. Nil when the
	// loop's resolution never published a tally (an ordinary RepeatEach, or
	// one on a vote without StoreVoteNum$), preserving the unbound read.
	VoteCounts []VoteCount
}

// FlipRest is a DB$ FlipCoin loop's continuation once a per-flip sub-ability
// suspended on its own mid-resolution ask (FlipUntilYouLose$ or Amount$ > 1).
// The host re-enters the FlipCoin primitive with this cursor so the remaining
// flips run rather than being abandoned. Plain data, so the host can carry it
// on its own continuation frame and replay re-derives it identically.
type FlipRest struct {
	// Players is the flipper set and PlayerIndex the index of the flipper
	// whose loop is in progress. Iter is the next iteration for that flipper
	// (the number already flipped); Amount is the loop bound for a
	// non-until-lose flip and UntilLose whether the loop runs until the first
	// tail.
	Players     []state.PlayerID
	PlayerIndex int
	Iter        int32
	Amount      int32
	UntilLose   bool
}

// VillainousRest is a VillainousChoice's continuation once its chosen body
// has completed: Victims is the ordered Defined$ player set and Next is the
// index of the victim still to ask (the completed victim's index + 1). The
// host re-enters the VillainousChoice primitive with that cursor, so a body
// that suspended on its own nested ask does not strand the remaining
// victims. Plain data, so the host can carry it on its own continuation
// frame and replay re-derives it identically.
type VillainousRest struct {
	Victims []state.Target
	Next    int
}

// GenericChoiceRest is a multi-player api:GenericChoice's continuation after
// one chooser's chosen body suspended on a nested mid-resolution ask.
// Choosers is the ordered Defined$ player set and Next is the index of the
// chooser still to ask (the completed chooser's index + 1). The host re-enters
// the GenericChoice primitive with that cursor, so a body that suspended on its
// own nested ask does not strand the remaining choosers. Plain data, so the
// host can carry it on its own continuation frame and replay re-derives it
// identically.
type GenericChoiceRest struct {
	Choosers   []state.Target
	Next       int
	Remembered []state.Target
}

// DamageSourceLKI is the pre-departure damage provenance of one object.
// It remains separate from Ctx's own-source fields because DamageSource$ may
// name an object distinct from the resolving spell or ability's source.
//
// Infect and Deathtouch join Lifelink because CR 113.7a reads the source's
// last known characteristics for the whole damage rider, not just the life
// gain: a bearer that left while its ability waited still deals its damage in
// counter form (CR 702.90b) and still marks its hit deadly (CR 702.2b). Rules
// seeds all three from one walk, so this map is their single home -- it is
// populated for the resolution's OWN source as well as a named DamageSource$
// object, and the older own-source Ctx fields stay authoritative only for
// lifelink and controller, whose precedence predates it.
type DamageSourceLKI struct {
	Lifelink   bool
	Infect     bool
	Wither     bool
	Deathtouch bool
	Controller state.PlayerID
}

// Ctx carries the bindings a Forge script refers to during resolution.
// EffectFrame identifies one continuous-effect registration by its source
// and registration timestamp (the same identity rules' replMatch key
// encodes). The zero value means "no frame".
type EffectFrame struct {
	Source state.ObjID
	Stamp  uint32
}

// RepeatOptionalContinuation is the scoped continuation for RepeatOptional$.
// It is carried only by the resolving Ctx; rules transports it across a
// mid-resolution ask and it is never event state.
//
// It represents two DISTINCT resume states, never conflated (fx42):
//   - Continue false: the player answered "no" and the loop stops.
//   - Continue true, AskElection false: a completed election was answered
//     "yes", so the next body to run is iteration Next -- no further
//     election is owed for it.
//   - Continue true, AskElection true: a body of iteration Next-1 completed
//     after its own suspension (a body ask), so the do/while election owed
//     for iteration Next has NOT been posed yet and must be asked before
//     that iteration's body runs.
type RepeatOptionalContinuation struct {
	Continue    bool
	Next        int32
	AskElection bool
}

// RepeatEachOptionalContinuation is the scoped answer of one subject's
// RepeatOptionalForEachPlayer$ election, carried only by the resolving Ctx
// across the mid-resolution ask (rules transports it; it is never event
// state). Next is the subject index whose offer was answered. Accept runs
// that subject's body; a false skips it and continues at Next+1. The subject
// list itself rides the RepeatSuspension/RepeatCursor, exactly as a body
// suspension's does, so the loop never re-derives its subjects mid-flight.
type RepeatEachOptionalContinuation struct {
	Next   int32
	Accept bool
}

type Ctx struct {
	TriggerContext
	// ClashContinuation resumes CR 701.31's saved reveal/winner snapshot after an owner answers.
	ClashContinuation *decision.ClashResume
	ClashTop          bool
	Source            state.ObjID
	Controller        state.PlayerID
	// PromisedGiftOverride is bound only by rules' pre-election target-feasibility
	// census, which must consider either branch before the player elects Gift.
	PromisedGiftOverride *bool
	// NameChoice carries a mid-resolution NameCard answer across re-entry.
	NameChoice string
	// ChangeZoneAlternative carries the owner's answered top/bottom choice
	// across a suspended ChangeZone resolution. It is consumed by effChangeZone.
	ChangeZoneAlternative string
	// ChosenDirection carries a mid-resolution ChooseDirection answer across
	// re-entry: the "left"/"right" pick (Aminatou's [-6], Order of
	// Succession). It is resolution-scratch like NameChoice -- never
	// event-encoded; a replay re-derives it from the recorded intent through
	// rules' "choosedirection" resume arm. Empty on the first pass, and left
	// set for the rest of the chain because the SubAbility$ that consumes it
	// (DBControl / DBGainControl) runs in the same walk.
	ChosenDirection string
	// ResolvedThisTurn is how many times the resolving ability has resolved
	// this turn, INCLUDING the current resolution. The effects layer cannot
	// import rules, so the tally arrives here as bound data: rules reads it
	// from state.Game.ResolvedThisTurn (keyed by source + ability body) at
	// every ability resolution and re-binds it across a mid-resolution ask.
	// It backs Count$ResolvedThisTurn (Sephiroth's fourth-resolution
	// transform, Prowl's second, Victor's first/second/third). Zero on a
	// spell, on a synthetic push, and in any test Ctx that never binds it --
	// a modelled head reading a legitimate zero.
	ResolvedThisTurn int32
	// ActivationsThisTurn is how many times the resolving ACTIVATED ability
	// has been activated this turn, INCLUDING the current activation (its own
	// AbilityPush / ManaActivate marker is already in the log). rules binds
	// it wherever it binds ResolvedThisTurn; zero means unbound (a spell, a
	// trigger, a synthetic Ctx). It backs ConditionActivationLimit$
	// (Farrelite Priest's "if this ability has been activated four or more
	// times this turn").
	ActivationsThisTurn int32
	// EffectiveNames is the layer-3 rename table (SetName$, CR 613.1d) in force
	// on the battlefield, published by the resolving Host at the top of every
	// effects.Resolve walk and bound onto every SpecContext (*Ctx).SpecContext
	// builds. It is immutable DATA -- the same shape SpecContext.EffectiveNames
	// already uses -- so a resolving effect's name filter agrees with rules'
	// layer walk instead of the printed face, without a pointer from state.Game
	// into rules. An effects test double whose Host does not implement
	// nameTableHost leaves it nil and reads the printed face.
	EffectiveNames []ObjectName
	// EffectiveTypes is the layer-4 derived type table (AddTypes$,
	// AddAllCreatureTypes$, RemoveCardTypes$, the CR 708.5 face-down set) in
	// force on the battlefield, published alongside EffectiveNames and bound
	// onto every SpecContext (*Ctx).SpecContext builds. It is immutable DATA
	// -- the same shape SpecContext.DerivedTypes already uses -- so a
	// resolving effect's target offer, Count$Valid census or other filter
	// sees a type a continuous effect granted, without a pointer from
	// state.Game into rules. An effects test double whose Host does not
	// implement typeTableHost leaves it nil and reads the printed face.
	EffectiveTypes []ObjectTypes
	// StaticGoads is the live static-goad table (staticgoad1), published by
	// rules for resolution-time IsGoaded filters.
	StaticGoads map[state.ObjID]bool
	// TargetableObjects is a rules-built immutable legality snapshot for the
	// triggering spell, used by CanBeTargetedByTriggeredSpellAbility.
	TargetableObjects []state.ObjID
	Targets           []state.Target
	// ModeTargets carries the target groups selected for a distinct modal
	// Charm. Each entry is in target-bearing mode order; nil means the
	// historical single-target-list path, including repeatable modes.
	ModeTargets [][]state.Target
	// CharmModeScope is the ONE mode's target group a distinct modal Charm
	// scoped Ctx.Targets to while it dispatches that mode, plus the mode's own
	// SA. charmDistinctTargetRun narrows Ctx.Targets per mode, but a mode that
	// SUSPENDS on a mid-resolution ask re-enters through resumeResolution,
	// which rebuilds Ctx.Targets from the stack object's WHOLE flat list --
	// both modes' targets. A walking primitive then sees one acting target per
	// mode and runs itself once per mode (a Collective Brutality discard asks
	// twice). Engine.Ask captures this onto the pending frame and the resume
	// re-binds it, the same shape rp.fusedTargets uses for a fused half.
	CharmModeScope []state.Target
	CharmModeSA    *cards.SA
	// TargetControllerLKI captures each object target's controller at the
	// start of resolution. A target may leave the battlefield before a
	// chained TokenOwner$ TargetedController is evaluated; events.Apply then
	// resets its live Controller to Owner, so the live object is no longer the
	// CR 608.2h last-known controller.
	TargetControllerLKI map[state.ObjID]state.PlayerID
	// TargetCountersLKI captures each object target's counters for the CR
	// 608.2b/h look-back. The authoritative capture is at the DEPARTURE
	// boundary: rules' Engine.emit refreshes the entry -- overwriting this
	// resolution-start snapshot -- on the MoveZone that actually moves a
	// target off the battlefield, so a chain that changed a target's counters
	// earlier in the same resolution reads the counters as they were
	// immediately before the zone change (Dismantle's DBPutCounter is the
	// corpus shape). Keyed by target ObjID, carried across a suspension the
	// same way as TargetControllerLKI (rules' resumePoint). The
	// resolution-start capture here is the fallback for a departure this
	// host did not see (a test host folding events without Engine.emit):
	// only battlefield objects carrying at least one counter are captured at
	// entry; an object already off the battlefield, or with no counters to
	// look back at, needs no entry.
	TargetCountersLKI map[state.ObjID][]state.Counter
	// TargetSpellLKI records which object targets were SPELLS on the stack at
	// the instant this Resolve chain began. The count ref SpellTargeted (Forge
	// AbilityUtils.calcX's `calcX[0].equals("SpellTargeted")` arm, which reads
	// getDefinedSpellAbilities' target SPELLS) names the target that WAS a
	// spell; a target chosen as a battlefield permanent is not one. The target
	// object's live zone cannot answer that after resolution begins: a Counter
	// or a ChangeZone moves the spell off the stack and events.Apply's Move
	// leaves no "was a spell" marker, so a later sub-ability reading
	// SpellTargeted$CardManaCostLKI (Reject Imperfection's proliferate gate,
	// Gale's Redirection's roll modifier, Press the Enemy's Z) would read the
	// moved target as a non-spell and return 0. The mana VALUE itself survives
	// the move -- a face's converted cost is printed and the object id is
	// stable -- so only the stack-kind needs the snapshot. Captured at Resolve
	// entry (keep an existing map on re-entry: after a move the target is no
	// longer on the stack, so a re-capture would wrongly lose it) and carried
	// across a suspension the same way as TargetControllerLKI (rules'
	// resumePoint). Nil when the chain's targets were never spells.
	TargetSpellLKI map[state.ObjID]bool
	Remembered     []state.Target
	// ForgetOtherSnapshot retains the pre-clear IsRemembered candidates across
	// a multi-owner ChangeZone pick/search and its mid-resolution asks. It is
	// resolution-local; only the actual remembered set is event-backed.
	ForgetOtherSnapshot []state.Target
	ForgetOtherOwners   []state.PlayerID
	ForgetOtherReady    bool
	ForgetOtherCleared  bool
	// RepeatOptional is set only when a RepeatOptional$ answer is being
	// resumed. A nil value means this is the first pass through the Repeat.
	RepeatOptional *RepeatOptionalContinuation
	// RepeatEachOptional is set only when a RepeatEach
	// RepeatOptionalForEachPlayer$ election is being resumed. A nil value
	// means no per-subject election answer is in flight. It is distinct from
	// RepeatOptional: that is the Repeat do/while's own open-ended election,
	// this is one subject's yes/no offer inside a RepeatEach loop.
	RepeatEachOptional *RepeatEachOptionalContinuation
	// TargetsOffered marks that the resolution's OWN ValidTgts$ targeting was
	// already offered at announcement (rules' resolveTop sets it on both the
	// ability and the spell branch, exactly for the SA the placement ask
	// covered). Without it a Min-0 target the chooser elected ZERO of would
	// look identical to a targeting that was never offered (both leave
	// Ctx.Targets empty), and effChangeZone's mid-resolution ask
	// (changeZoneChosenTargets) would pose the same question twice. A fresh
	// ctx rebuilt by a resume does not carry it -- a deeper sub's targeting
	// was genuinely never offered, which is the ask's real population.
	//
	// Boundary, updated by task mvts1: the flag suppresses only the depth-0
	// entry SA of an effects.Resolve call (chosenTargetsFor's atRoot arm) --
	// the SA the placement/announcement ask actually covered. A deeper sub
	// in the SAME resolution that carries its OWN never-offered ValidTgts$
	// now poses its own ask there (the trigger "when you do" family: Mogg
	// Bombers' DealDamage, Kor Outfitter's Attach); before mvts1 it either
	// inherited the outer targets or moved nothing silently. A nested
	// Resolve entry at depth 0 whose SA is genuinely never-covered (a
	// RepeatEach iteration body) is also suppressed while the flag is set --
	// the conservative direction, same as the pre-mvts1 ChangeZone shape.
	TargetsOffered bool
	// TargetsUnique accumulates the targets chosen by earlier `TargetUnique$
	// True` asks in THIS resolution chain, so a later ask in the same chain
	// (Know Evil's three `DB$ Effect` "up to one target opponent" riders, or
	// a root/SubAbility pair like Biomantic Mastery's "another target
	// player") cannot re-offer one of them. Ctx.Targets holds the
	// placement/announcement targets only and is never appended to, so the
	// two are read together by TargetsAlreadyChosen. A fresh Ctx rebuilt by a
	// resume re-binds this field from the pending ask's ride (Decision
	// .ResumeTargetsUnique -> the resume point's targetsUnique), so an
	// intervening suspension between two riders keeps the earlier picks. The
	// ride is not limited to the two asks that stamp it explicitly: the ask
	// boundary (rules' Engine.Ask) also reads this live field off the chain
	// Ctx that Resolve publishes, so a Charm mode election, a ward pay window
	// or a dig/scry/arrange ask carries the same accumulator.
	TargetsUnique []state.Target
	// Captured is the part of Remembered the resolution started with because
	// its trigger, delayed trigger or replacement put the event's object there
	// (this engine's stand-in for Forge's separate TriggeredCard), rather than
	// because a Remember* parameter of the resolution chose it. Forge keeps
	// neither in a host's remembered list, so a RepeatEach over players does
	// not carry these into its iterations.
	Captured []state.Target
	// SourceLifelinkLKI is the source permanent's derived lifelink state at
	// the last moment it existed on the battlefield. The validity bit is
	// separate because "it did not have lifelink" is authoritative LKI too.
	// Rules seeds this on independently resolving abilities; damage uses it
	// only after the source has departed, and continues to read the live
	// derived source while it remains a permanent.
	SourceLifelinkLKI      bool
	SourceLifelinkLKIValid bool
	// SourceControllerLKI is the source permanent's controller immediately
	// before it left the battlefield. Move resets Controller to Owner, so an
	// independently resolving lifelink ability needs this companion snapshot
	// to credit its last controller rather than its owner.
	SourceControllerLKI      state.PlayerID
	SourceControllerLKIValid bool
	// DamageSourceLKI preserves lifelink and controller LKI by object id for
	// a distinct DamageSource$ object that left while this resolution waited.
	// Rules transports it with the stack object; DamageSource$ consults it only
	// after that named object is no longer a battlefield permanent.
	DamageSourceLKI map[state.ObjID]DamageSourceLKI
	// Sacrificed carries the last-known-information snapshot of every object
	// this resolving spell/ability sacrificed, as it was at the instant of the
	// sacrifice (state.SacrificedInfo). Built two ways, feeding one field: a
	// cost-paid sacrifice carries it onto the stack object (rules/cast.go
	// commitCast) and resolution loads it here, while an effect-driven
	// sacrifice (effSacrifice with RememberSacrificed$ True) appends here
	// directly so a SubAbility$ chained after it can read it. The
	// Sacrificed$<Property> heads in count.go read it.
	Sacrificed []state.SacrificedInfo
	// Exiled / Revealed carry the cards PAID as part of this cast's or
	// activation's cost: the `ExileFromHand`/`ExileFromGrave`/`Exile` parts
	// (Forge's CostExile, paid list keyed "Exiled") and the `Reveal` parts
	// (CostReveal, keyed "Revealed"). They are NOT the source's persistent
	// exile association (`Object.ExiledWith`) nor `Remembered`: they name the
	// exact cards this cast's cost removed, in stable cost order. Forge reads
	// them through AbilityUtils.getPaidCards -> SpellAbility.getPaidList, and
	// the `Exiled$<Property>` / `Revealed$<Property>` count refs and the bare
	// `Defined$ Exiled`/`Revealed` selectors both resolve them here (the one
	// shared binding). rules carries them onto the engine keyed by the stack
	// object (engine.castPaid), rebuilt by replay because payCast re-executes;
	// a copy of the spell was never cast and carries none. Empty means "no
	// paid list" -- a legitimate zero, never a fallback to the source or the
	// chosen targets.
	Exiled   []state.ObjID
	Revealed []state.ObjID
	// ChangeZoneLKI is the resolution's last-known-information table for
	// ChangeZoneRememberLKI$ moves: one entry per object the move captured,
	// holding the controller/owner it had at that instant. events.Apply's Move
	// resets a battlefield departure's controller to its owner (CR 400.7), so
	// the live object can no longer answer "the exiled creature's controller"
	// -- exactly Forge's reason for storing a Card LKI copy in Remembered
	// (ChangeZoneEffect's CardCopyService.getLKICopy). A RepeatEach body's
	// TokenOwner$ ImprintedController / Defined$ ImprintedController reads it
	// for the current iteration subject (Curse of the Swine's Boars).
	ChangeZoneLKI []state.LKIObject
	// ResolvingObj is the stack-object WRAPPER of the spell/ability currently
	// resolving -- rules' e.resolvingObj (resolveTop's ability and spell
	// branches) and rp.obj (resumeResolution) -- set at those two ctx
	// construction sites. For an ability resolution Ctx.Source is the source
	// PERMANENT (Ruling T20-b: Defined$ Self must resolve to something with a
	// face), so a ValidStack qualifier that means "not the ability resolving
	// right now" (Ulalek's `Ability.YouCtrl+otherAbility`) cannot anchor on
	// Source: the permanent is not on the stack and excludes nothing. This
	// field is resolution-scratch like Targets/SVars -- never event-encoded,
	// a replay re-derives the same binding -- and zero on contexts built off
	// the resolution path (hand-built test probes), where ValidStack's
	// otherAbility falls back to Ctx.Source. Never widened.
	ResolvingObj state.ObjID
	// SVars is the resolving card's SVar table, and X the value paid for {X}.
	// Both are bound by the rules package when it builds the context.
	SVars map[string]string
	X     int32
	// XAnnounced marks that X above IS a real CR 601.2b/107.3i announcement
	// (the resolving spell or ability paid a {X} cost, possibly zero), set by
	// the rules package at the same sites that bind X from the stack object's
	// CastInfo. Without it an announced-zero X is indistinguishable from
	// never-announced, and an UnlessCost$ X on a zero-X cast (Power Sink
	// announced 0) would stay an unpriceable raw token instead of {0}.
	XAnnounced bool
	// TimesKicked is the pending cast's settled multikicker payment count
	// (CR 702.43), seeded by rules' targetBoundCtx when the spell's OWN
	// announcement ask resolves a Count$TimesKicked bound BEFORE payment has
	// stamped the stack object (Comet Storm's TargetMin/Max$ TargetsNum).
	// Everywhere else it is zero and the TimesKicked count head falls back to
	// the source object's stamped field -- the same priority the xPaid head
	// gives ctx.X over the object read.
	TimesKicked int32
	// PendingKicked is the pending cast's CHOSEN kicked mode (CR 702.4/702.33),
	// seeded by rules' targetBoundCtx when the spell's OWN announcement ask
	// resolves a Count$Kicked body BEFORE payment has stamped the stack
	// object's CastFlags. Tear Asunder's kicked main SA is
	// TargetMin$ X | TargetMax$ X over SVar:X:Count$Kicked.0.1, so pre-payment
	// the object reads Kicked false and X stays 1 -- the ask then demands an
	// artifact/enchantment the kicked spell must not take. The Kicked count
	// head ORs this bit with the object's FlagKicked, so a mid-resolution read
	// (no pending cast, the flag actually stamped) is unaffected. Zero (false)
	// everywhere else; it is derived data, never event-encoded.
	PendingKicked bool
	// ChosenNumber is the Effect's SetChosenNumber$ binding (task
	// wildgrowth1): the number the Effect resolved at creation, threaded into
	// a registered replacement's body Ctx by rules' replCtx so the body's
	// Count$ChosenNumber head (evalCountBody) reads the frozen binding rather
	// than re-deriving. Zero wherever nothing bound -- the same number a
	// failed binding degrades to.
	ChosenNumber int32
	// ChosenNumberBound marks a Ctx whose ChosenNumber IS a real
	// SetChosenNumber$ binding (rules' seedEffectReplCtx sets it exactly when
	// the match is effect-created, m.key != ""). It is the Count$ChosenNumber
	// head's verdict: bound means evaluated (the value reads, zero
	// legitimately), unbound means the head is UNRESOLVED so the
	// EvalCountOK consumers keep their pre-wildgrowth fail direction --
	// CheckSVarHolds fails open, a numeric filter RHS (cmcEQX via
	// resolveNumericRHS) never matches -- instead of enforcing a meaningless
	// zero on the Choose-event corpus population (77 files whose binding
	// lives on state.Object.ChosenNumber via effects/choose.go, never on
	// Ctx). A zero binding with the flag set is still bound (torgal with no
	// Dogs); only the flag distinguishes the two.
	ChosenNumberBound bool
	// RememberedCMC is the mana value the Counter primitive's
	// RememberCounteredCMC$ rider remembered (task counter-cmc: Electrosiphon's
	// "an amount of {E} equal to its mana value", Overwhelming Intellect's
	// draw-equal-to-mana-value family -- 14 corpus carriers). effCounter sums
	// every countered CARD's mana value into it (an ability has none and
	// contributes nothing); the Count$RememberedNumber head reads it in
	// preference to the list-length channel, because the number is a VALUE,
	// not a count of remembered entries. Resolution-scratch like
	// Ctx.Remembered -- never event-encoded; a replay re-derives it by
	// replaying the same resolution.
	RememberedCMC int32
	// RememberedCMCBound marks a Ctx whose RememberedCMC IS a real
	// RememberCounteredCMC$ binding. It is the Count$RememberedNumber head's
	// verdict, the same shape ChosenNumberBound gives Count$ChosenNumber:
	// bound means evaluated (a zero mana value reads as zero), unbound means
	// the head falls through to the list-length read every pre-existing
	// consumer keeps.
	RememberedCMCBound bool
	// PendingDamage holds the damage a DealDamage with DamageMap$ True MARKED
	// for this chain's later DB$ DamageResolve flush instead of dealing it
	// (Forge's mark-then-resolve damage pattern). It is resolution-scratch
	// like Remembered -- never event-encoded; a replay re-derives it by
	// re-running the same resolution -- and rules carries it across a
	// mid-chain ask on the pending frame, the same way Remembered rides
	// ResumeRemembered. The marks are unexported-typed so the rules package
	// holds them opaquely. See effects/damage.go's effDamageResolve.
	PendingDamage []PendingDamage
	// EffectFrame names the Effect-created continuous-effect registration
	// whose replacement body this Ctx is resolving (rules' seedEffectReplCtx
	// sets it from the match's "effect:<source>:<timestamp>" key; zero Source
	// on every printed-replacement, spell and ability resolution). It is the
	// Ctx-side stand-in for Forge's Command-zone effect object: effChangeZone's
	// self-exile idiom ends exactly this registration (Host.EndEffect) instead
	// of attempting a card move no real card can make.
	EffectFrame EffectFrame
	// Host is the engine driving this resolution, bound by effects.Resolve
	// itself (it receives the host as its own parameter, so every walk that
	// can reach a resolution-time filter evaluation has passed through one
	// set here) rather than at every Ctx construction site. Ctx.SpecContext
	// consults it to resolve a numeric filter RHS through the SVar table
	// (EvalCountOK -- Nightmare Unmaking's Creature.powerGTX against
	// SVar:X:Count$ValidHand Card.YouOwn, Whir of Invention's
	// Artifact.cmcLEX against the paid X). It stays nil on contexts that
	// never entered Resolve -- the direct Num/EvalCount probes -- which keeps
	// those read-only and resolver-free exactly as they have always been.
	Host Host
	// numericRHS is the cheap gate SpecContext's resolver install reads:
	// effects.Resolve computes it on entry (a paid X, or any SVar table at
	// all -- the resolver itself decides per name and fails closed on a name
	// with no resolvable body, so the broad flag never widens a match), so
	// the gate at the SpecContext call site is one field read and that call
	// site stays inside the inline budget the warm Derived escape-analysis
	// pin (rules/layers_test.go) enforces. Hand-built contexts (the direct
	// Num/EvalCount probes) leave it false and stay resolver-free.
	numericRHS bool
	// resolvingRHS is the one-level recursion guard on the SVar-body
	// resolution SpecContext installs: an SVar body that itself counts a spec
	// carrying the same numeric RHS (Count$Valid Creature.powerGTX named by
	// the SVar that resolves powerGTX) would otherwise recurse unboundedly
	// through SpecContext -> resolveNumericRHS -> EvalCountOK ->
	// MatchesSpecCtx -> resolveNumericRHS. A re-entrant ask fails closed
	// (never matches), the documented unresolvable-RHS contract. Not
	// event-backed, not state: resolution-scratch like Targets or SVars.
	resolvingRHS bool
	// ExcludeFromBattlefieldCount is the entrant of a battlefield MoveZone
	// replacement. Count$Valid bodies evaluating the Updated entry must use
	// the pre-entry battlefield population (CR 614.12), even though the move
	// has already been folded before the body runs. Zero outside that context.
	ExcludeFromBattlefieldCount state.ObjID
	// Replaced is the object the replaced event was about (Defined$ ReplacedCard):
	// the card a "would go to the graveyard from anywhere, exile it instead"
	// replacement is acting ON. Set by rules/replacement.go on the context it
	// builds for a matching ReplaceWith$; zero outside a replacement, and nil for
	// a zero (or gone) object when Defined resolves it. It is context, not state
	// -- it drives the replacement's own resolution but is never itself persisted
	// to the event log.
	Replaced state.ObjID
	// ReplacedCards is the ordered plural batch a replaced INSTRUCTION was
	// about (Defined$ ReplacedCards / ReplacedCards.<qual>): the cascade
	// instruction's exiled cards, which Averna, the Chaos Bloom picks a land
	// from. It is the plural counterpart of Replaced, set by
	// rules/replacement.go on the ReplaceWith$ context of a Cascade
	// proposal and carried through a suspension; empty outside one, and an
	// empty batch resolves to nobody (fail closed). Context, never state.
	ReplacedCards []state.ObjID
	// ReplacedPlayer is the player a replaced DRAW event was about — the
	// draw-er (Breathstealer's Crypt draws/reveals/discards "that player",
	// Zur's Weirding's other players pay relative to them). Set only on a
	// Draw replacement's own context, like Replaced; zero outside one.
	ReplacedPlayer state.Target
	// ReplacementTarget, ReplacementSource and
	// ReplacementAmount carry the corresponding roles of an in-flight damage
	// ReplacementAmount carry the corresponding roles of an in-flight damage
	// event. They are resolution context, never persisted state; rules seeds
	// them before resolving ReplaceWith$ so ReplacedTarget/ReplacedSource and
	// ReplaceCount$DamageAmount are available to every replacement body API.
	ReplacementTarget state.Target
	ReplacementSource state.ObjID
	ReplacementAmount int32
	// LKI is the object a zone-change trigger fired for, as it was just
	// before the move (CR 603.10 "look back in time"): Move resets counters,
	// tapped state and damage on the way out, so a "dies" condition such as
	// Undying's "if it had no +1/+1 counters" must read this, not the live
	// object. nil for every other trigger.
	LKI *state.Object
	// LKIPower/LKIToughness are that snapshot's derived battlefield P/T,
	// captured before the move removes continuous effects. The validity bit
	// distinguishes a real zero from a non-battlefield/no-characteristic LKI.
	LKIPower, LKIToughness int32
	LKIPTValid             bool
	// Modes is the answered modal choice on a re-entered mid-resolution
	// resolution (M2d-2): the SVar names of the chosen Choices$ sub-abilities,
	// in execution order. rules' resumeResolution sets it from the recorded
	// answer before re-running the suspended sub-ability, so effCharm's
	// re-entry runs exactly the chosen modes instead of asking again. Nil on
	// the first pass and on any non-modes resume.
	Modes []string
	// UnlessPay is the answered unless-pay choice on a re-entered
	// mid-resolution resolution (M2d-2): "pay" means rules' resumeResolution
	// has already paid the UnlessCost$ from the payer's pool and the asking
	// effect proceeds with its body; "decline" means it proceeds as if the
	// player declined (no effect). "" on the first pass, where the effect
	// poses the ask instead. For Sacrifice's damage-payment shape (Vexing
	// Devil), "pay" additionally means the accepting opponent's Damage event
	// has already been emitted by rules' resume arm — payment events belong
	// to rules, never to the effects layer.
	UnlessPay string
	// Discard is the answered "Mode$ RevealYouChoose" discard choice on a
	// re-entered mid-resolution resolution: the object(s) the caster named
	// to be discarded from the target's hand. rules' resumeResolution sets
	// it from the recorded answer before re-running the suspended sub-ability,
	// so effDiscard's re-entry discards exactly the chosen cards instead of
	// asking again. Nil on the first pass and on any non-discard resume. The
	// ask itself carries the caster as the chooser and the target as the
	// discarder, which is why a plain ObjID is not enough state to rebuild:
	// the two player roles are re-derived from Ctx on re-entry.
	Discard []state.ObjID
	// DiscardTarget is the per-target cursor for a mid-resolution discard
	// whose asking walk covers several acting players: the index (into the
	// effect's deterministic acting-player list) of the player whose answer
	// Discard carries. The answer applies to that target alone and every
	// LATER target poses its own ask, so a multi-target discard no longer
	// applies target 0's choice to every other target (which left targets 2..n
	// unasked). It is the RevealPickTarget cursor's discipline applied to
	// discards, consumed and cleared with Discard at the top of effDiscard's
	// walk (fx42 scoping).
	DiscardTarget int
	// DiscardVote is the answered Optional$ True may-discard election: for
	// "Mode$ Hand" (a whole-hand wheel's "each player may discard their
	// hand") "yes" discards that player's whole hand; for TgtChoose (Mox
	// Diamond's "you may discard a land card") "yes" poses the card pick
	// with Min 1. "no" (or an empty answer) declines. It is separate from Discard because the election answers a
	// yes/no, not an object list; the per-target cursor is DiscardTarget.
	DiscardVote string
	// Choice is the selected card(s) or player(s) from ChooseCard,
	// ChoosePlayer, or ChangeTargets. ChoiceDone distinguishes an answered
	// empty optional choice from its first pass.
	Choice     []state.Target
	ChoiceDone bool
	// CopyPermanentChoice is the selected source for the one supported
	// CopyPermanent Choices$/Chooser$ shape. It is deliberately separate
	// from Choice so nested choices cannot consume it.
	CopyPermanentChoice     state.ObjID
	CopyPermanentChoiceDone bool
	// TargetsPick is the answered target set of the generic ValidTgts$
	// pre-ask (chosenTargetsFor, posed inside effects.Resolve's dispatch
	// loop for a sub the placement/announcement ask never covered -- the
	// trigger "when you do" family, task mvts1). rules' "tgts" resume arm
	// fills it on the re-entered pass; the pre-ask consumes and clears it
	// (fx42 scoping: each resume builds a fresh Ctx and re-enters exactly
	// the asking SA, so nothing else can be holding it). The answer rides
	// its own resume kind ("tgts") and its own Ctx transport rather than
	// the shared Choice pair so another KChoose primitive resolving under
	// the same SA can never steal it.
	TargetsPick     []state.Target
	TargetsPickDone bool
	// DamageSplit is the answered allocation of a DealDamage
	// DividedAsYouChoose$ total: DamageSplit[i] is the damage assigned to
	// the i-th target of the resolution's Defined$ list, in that order (the
	// order the ask's one-option-per-target list uses). DamageSplitDone
	// marks the answer present -- an answered allocation, or the silent
	// no-ask path when there was nothing to divide -- so the primitive
	// never re-poses the ask on a later round. rules' "damage_split" resume
	// arm fills it from the answered multiset; a target the answer never
	// picked is absent (zero damage).
	DamageSplit     []int32
	DamageSplitDone bool
	// OfferedSA is the SA whose ValidTgts$ targeting the placement or
	// announcement ask actually covered (rules' resolveTop and
	// resumeResolution both set it; chosenTargetsFor skips exactly that SA,
	// matched by SA.Line -- ResolveSVar parses fresh on every call, so
	// pointer identity does not hold between two derivations of the same
	// body, the matching convention rules' charmModeTarget already
	// established).
	OfferedSA *cards.SA
	// ModesSeen names the chosen modes earlier passes of a CanRepeatModes$
	// Charm's mode walk already ran (rules' charm_rest resume arm seeds it
	// from the consumed prefix of the object's ChosenModes; a first pass has
	// it nil). effCharm's re-entry marks a target-bearing mode as covered by
	// the placement/announcement ask only for its FIRST occurrence across the
	// FULL multiset -- a later occurrence must keep its own ValidTgts$
	// pre-ask instead of inheriting the shared target list again.
	ModesSeen []string
	// PickedTargets is the answering pre-ask's target set, made visible to
	// Defined's ValidTgts$ fallthrough for exactly ONE dispatch (the
	// wrapper clears it when the body returns). It must not be Ctx.Targets:
	// a CLOBBER sub names its parent's target explicitly (Object$
	// ParentTarget, Defined$ Targeted), and overwriting Ctx.Targets would
	// point those referents at the sub's OWN answer instead of the outer
	// target the script meant.
	PickedTargets []state.Target
	// SubPreAsk carries the CAST-time pre-asked target answers for this
	// resolution's SubAbility$ chain (task alltargeted1): Forge asks every
	// targeting SA in the whole chain BEFORE cost payment (CR 601.2c), so
	// the engine pre-asks them in the cast flow and the resolution must
	// use the answers instead of re-posing the asks mid-resolution. The
	// map is keyed by the sub SA's Line (the same matching convention the
	// OfferedSA marker uses). It belongs to Engine.castSubTargets and remains
	// until the stack object leaves: a suspended body can re-enter with a new
	// Ctx and must still see its earlier target answer. Replay re-derives the
	// record identically. Nil for every resolution whose cast pre-asked nothing
	// (triggers, copies, modal spells -- their targeting machinery is
	// unchanged).
	SubPreAsk map[string][]state.Target
	// AllTargets is the whole root/sub-ability target UNION Forge's
	// AllTargeted$ count ref names (task alltargeted1), threaded by the
	// cost-evaluation sites that read it before payment (ownReduceCost's
	// CR 601.2c reprice, the CollectEvidence amount resolution). Ctx.Targets
	// stays the resolving SA's OWN targets so Targeted/ParentTarget keep
	// their meanings; refTargets' AllTargeted case reads this when it is
	// non-nil and falls back to Ctx.Targets otherwise (a chain with no sub
	// targets unions to exactly the root's own set).
	AllTargets []state.Target
	// ChoiceTarget is the index of the per-player chooser currently being
	// resumed. It keeps multi-player ChooseCard/ChoosePlayer asks from
	// returning to the first chooser after every answer.
	ChoiceTarget int
	// Chosen holds card/player choices for the remaining resolution chain.
	// Unlike Choice it is not the transport for a pending answer; filters such
	// as Creature.nonChosenCard consult it after ChooseCard has returned.
	Chosen      []state.Target
	ChosenValid bool
	// Repeat is set only on the re-entry of a suspended RepeatEach loop; the
	// RepeatEach whose SA it names consumes and clears it.
	Repeat *RepeatCursor
	// RepeatSubject is the RepeatEach iteration's current subject — what
	// Forge's UseImprinted$ binds as "Imprinted" for the sub-ability the
	// loop resolves (Heroism's attacking red creature, Stench of Evil's
	// destroyed Plains). effRepeatEach sets it per iteration; the suspension
	// machinery carries it through a resumed ask the way loopRemembered
	// carries the iteration's Remembered. Zero outside a loop iteration, and
	// the Imprinted/ImprintedController selectors fail closed on zero.
	RepeatSubject state.Target
	// VillainousVictims is the ordered Defined$ player set for a
	// VillainousChoice. The index advances only after the current victim's
	// chosen body has completed.
	VillainousVictims []state.Target
	VillainousIndex   int
	// GenericChoosers is the ordered Defined$ player set for a multi-player
	// api:GenericChoice resolution (each opponent chooses one of the same
	// Choices$), and GenericChooserIndex is the index of the chooser being
	// asked. The index advances only after the current chooser's chosen body
	// has completed, so the remaining choosers are asked once the body's own
	// nested ask (if any) finishes. Nil outside the per-player path, which
	// keeps the single-controller Charm/GenericChoice ask unchanged.
	GenericChoosers     []state.Target
	GenericChooserIndex int
	// Sacrifice is an Annihilator sacrifice answer on re-entry.
	Sacrifice []state.ObjID
	// Search is the answered hidden-library KChoose selection on a re-entered
	// ChangeZone resolution. SearchDone distinguishes "answered with no cards"
	// from the first pass; Search preserves the player's answer order. The
	// asking effect consumes and clears both before continuing, so a nested
	// search cannot inherit the outer answer. LibraryTarget binds the answer
	// to the exact owner in a multi-library walk.
	Search     []state.ObjID
	SearchDone bool
	// LibraryTarget is the index in the deterministic per-library target list
	// whose answer is being resumed. Search, KArrange and their follow-up
	// confirms share this cursor so a suspended walk continues with the next
	// library instead of restarting at the first one.
	LibraryTarget int
	// SearchShuffle is the answered ShuffleNonMandatory$ may-shuffle confirm
	// ("yes"/"no") on a re-entered ChangeZone search; SearchShuffleMoved
	// carries the objects the search's first pass moved, so the re-entry can
	// run the LibraryPosition$ placement after the answered shuffle. Both
	// ride the ask (the moved list via Decision.ResumeMoved, the same
	// runtime-continuation class as ResumeRemembered) and are consumed and
	// cleared at the re-entry's top (fx42 scoping), so a nested search poses
	// its own confirm.
	SearchShuffle      string
	SearchShuffleMoved []state.ObjID
	// SearchKnown names, per choosing player, the library cards whose identity
	// that player has legitimately learned during this resolution's search
	// chain (effects/zone.go applyLibrarySearch): a card the head ask publicly
	// revealed is known to every seat, and a card the head ask offered BY NAME
	// is known to the player who picked it. A Cultivate-family placement leg
	// (NoLooking$ True, ChangeType$ ...IsRemembered) must label its options
	// with those real names -- the blind "a card" label would hide information
	// the chooser already holds -- while an option the chooser genuinely never
	// saw stays fail-closed blind. It rides the ask via Decision
	// .ResumeSearchKnown, because the first leg's own suspension rebuilds a
	// fresh Ctx and a plain field would be lost before the second leg asks.
	// Resolution-scratch like Remembered -- never event-encoded; a replay
	// re-derives the same set by replaying the same resolution.
	SearchKnown []state.Target
	// SearchConfirm is the answered Optional$ confirmation for a
	// hidden-library ChangeZone search whose script carries an explicit
	// marker -- Forge's confirmAction gate in
	// ChangeZoneEffect.changeHiddenOriginResolve, which runs BEFORE the fetch
	// list is consulted (so an empty eligible pool still confirms). "yes"
	// accepts this search player's fetch and every other answer declines it;
	// SearchConfirmDone distinguishes "answered" from the first pass, and
	// SearchConfirmTarget is the index in the deterministic per-library
	// target list whose confirmation was answered (the same cursor
	// LibraryTarget carries for the answered pick). A decline skips this
	// player's search, pick and search-specific shuffle/tail with the
	// remembered set untouched. Consumed and cleared at the top of
	// effSearchLibrary (fx42 scoping), so a nested search poses its own
	// confirmation. The marker is read through the ONE shared
	// optionalConfirmMarker the hand and hidden-pick walks use.
	SearchConfirm       string
	SearchConfirmDone   bool
	SearchConfirmTarget int
	// AttachOpt is the answered Optional$ True attach election ("yes"/"no")
	// on a re-entered Attach resolution (Ajani's Chosen's "you may attach it
	// to the token", Cori-Steel Cutter's "you may attach this Equipment to
	// it"): "yes" attaches, anything else declines. It rides the ask (the
	// same runtime-continuation class as ResumeRemembered) and is consumed
	// and cleared at the re-entry's top (fx42 scoping), so a nested Attach
	// poses its own ask.
	AttachOpt string
	// CopyOpt is the answered Optional$ True CopySpellAbility election
	// ("yes"/"no") on a re-entered mid-resolution copy (Sevinne's
	// Reclamation's "if this spell was cast from a graveyard, you may copy
	// this spell"): "yes" makes the copy through the ordinary path,
	// anything else declines and no copy is made. It rides the ask (the
	// same runtime-continuation class as AttachOpt) and is consumed and
	// cleared at the re-entry's top (fx42 scoping), so a nested
	// CopySpellAbility poses its own ask.
	CopyOpt string
	// AttachChoice is the answered Attach object/destination choice on a
	// re-entered Attach resolution (Goldwardens' Gambit's "you may attach an
	// Equipment you control to it", unexpected_request's same shape, Breath of
	// Fury's "attach CARDNAME to a creature you control"): with no Object$
	// the chosen ids name the OBJECT to attach, with Object$ present they name
	// the DESTINATION. AttachChoiceDone distinguishes "answered with nothing
	// chosen" (a Min-0 Optional$ decline) from an unanswered ask; AttachDests
	// carries the destination list the asking pass resolved (a RepeatEach
	// body's Defined$ Imprinted binding does not survive the suspension, so
	// the re-entry must not re-derive it). All three ride the ask (the same
	// runtime-continuation class as ResumeRemembered) and are consumed and
	// cleared at the re-entry's top (fx42 scoping), so a nested Attach poses
	// its own ask.
	AttachChoice     []state.ObjID
	AttachChoiceDone bool
	AttachDests      []state.ObjID
	// AttachPlayer is the answered PlayerChoices$ Attach player on a
	// re-entered Attach resolution (Curse of Leeches' `DB$ Attach | Object$
	// Self | PlayerChoices$ Player`, Lynde's `DB$ Attach | Object$ ChosenCard
	// | PlayerChoices$ Opponent`): the chosen seat the object attaches to.
	// AttachPlayerDone distinguishes "answered" from an unanswered ask. Both
	// ride the ask (the same runtime-continuation class as AttachChoice) and
	// are consumed and cleared at the re-entry's top (fx42 scoping), so a
	// nested Attach poses its own ask.
	AttachPlayer     state.PlayerID
	AttachPlayerDone bool
	// ScryOpt is the answered Optional$ True Scry election ("yes"/"no").
	ScryOpt string
	// PutOpt is the answered Optional$ True put-counter election ("yes"/"no")
	// on a re-entered PutCounter resolution (Talus Paladin's "you may put a
	// +1/+1 counter on CARDNAME", Black Widow's "You may put ... If you
	// don't, ..."): "yes" places the counters through the ordinary path,
	// anything else declines and the chained SubAbility$ still runs. It rides
	// the ask (the same runtime-continuation class as ResumeRemembered) and
	// is consumed and cleared at the re-entry's top (fx42 scoping), so a
	// nested PutCounter poses its own ask.
	PutOpt string
	// SetStateOpt is the answered Optional$ True SetState election
	// ("yes"/"no") on a re-entered SetState resolution (Dowsing Dagger's
	// "you may transform this Equipment", High Marshal Arguel's "you may
	// transform it"): "yes" runs the ordinary face change, anything else
	// declines, changes nothing and still runs the chained SubAbility$. It
	// rides the ask (the same runtime-continuation class as PutOpt) and is
	// consumed and cleared at the re-entry's top (fx42 scoping), so a nested
	// SetState poses its own ask.
	SetStateOpt string
	// EndTurnOpt is the answer to Obeka's Optional$ EndTurn election. It is
	// consumed on re-entry so another EndTurn in the chain asks independently.
	EndTurnOpt string
	// CounterKind is the answered kind for a comma-separated PutCounter list.
	// CounterKindDone distinguishes an answered first-option fallback from the
	// first pass; CounterKinds carries a ChooseDifferent$ multi-answer.
	CounterKind      string
	CounterKindDone  bool
	CounterKinds     []string
	CounterKindsDone bool
	// CounterKindAnswers is the replay-derived per-recipient answer table
	// rules seeds for CounterTypePerDefined$; effPutCounter consumes it at
	// entry so a nested PutCounter cannot inherit it.
	CounterKindAnswers     []string
	CounterKindAnswerIndex int
	CounterKindAnswerSet   bool
	// PlaneswalkOpt is the answered Optional$ True "you may planeswalk"
	// election. It is resolution-local so a nested Planeswalk cannot inherit
	// an outer answer.
	PlaneswalkOpt string
	// Extort is the answered optional {W/B} payment on a re-entered Extort
	// resolution (M2d-2): "pay" means the caster agreed to pay and the drain
	// runs; anything else ("decline", first pass with a host that cannot ask)
	// means no drain. rules' resumeResolution sets it from the recorded answer
	// before re-running the suspended effExtort, and effExtort clears it after
	// reading so a nested Extort below it poses its own ask.
	Extort string
	// Play is the answered card a resolved Play effect chose to play from a
	// zone (CR 701.23): the object the controller selected among the offered
	// candidates. rules' resumeResolution sets it from the recorded answer
	// before re-running the suspended effPlay, which then casts/plays it from
	// its own zone. PlayDone distinguishes "answered (possibly with no card)"
	// from the first pass.
	Play     state.ObjID
	PlayDone bool
	// DrawDone is the number of individual draws a multi-card Draw has already
	// completed. A dredge choice suspends between draws; rules restores this
	// cursor after applying the selected replacement so the enclosing Draw
	// continues rather than restarting or abandoning its remaining cards.
	DrawDone int32
	// Imprint is the selected public-zone ChangeZone card. It is scoped to
	// Imprint$ True so a nested ordinary ChangeZone cannot consume it.
	Imprint     []state.ObjID
	ImprintDone bool
	// Untap is the answered UntapType$ selection. UntapDone distinguishes an
	// answered empty "up to" choice from the first pass and scopes the answer
	// to the Untap primitive that asked.
	Untap     []state.ObjID
	UntapDone bool
	// Dig is the answered Dig look-and-take pick on a re-entered mid-resolution
	// resolution: the object(s) the library's owner picked out of the top
	// DigNum$ window to move to DestinationZone$, in the player's answer
	// order. rules' resumeResolution sets it from the recorded answer before
	// re-running the suspended sub-ability, so effDig's re-entry moves exactly
	// the chosen cards instead of asking again; DigDone distinguishes
	// "answered (possibly with no cards)" from the first pass, and DigTarget
	// identifies the Defined$ target whose library posed that ask. Re-entry
	// skips earlier targets (already processed before suspension), applies the
	// answer at DigTarget, then continues with a fresh ask for each later
	// library. The asking effect
	// consumes and clears all three fields at the top of its own walk (the fx42
	// scoping discipline), so a nested Dig cannot inherit the outer answer.
	Dig       []state.ObjID
	DigDone   bool
	DigTarget int
	// DigUntilMove is the answered DigUntil reveal-until OptionalFoundMove$
	// election (task diguntil1; Songbirds' Blessing's "You may put that card
	// onto the battlefield. If you don't, put it into your hand."): "yes"
	// moves the found card(s) to FoundDestination$, "no" — the decline — to
	// OptionalNoDestination$ when the SA carries one, else the found card
	// joins the revealed pile (RevealedDestination$). rules' resumeResolution
	// sets it from the recorded answer before re-running the suspended
	// sub-ability, and DigUntilMoveDone distinguishes "answered" from the
	// first pass (it also suppresses the reveal Note and the withheld-params
	// Note a re-entry would otherwise re-emit). The asking effect consumes
	// and clears both at the top of its own walk (the fx42 scoping
	// discipline), so a nested DigUntil cannot inherit the outer answer.
	DigUntilMove     string
	DigUntilMoveDone bool
	// DigUntilAuraBearer is the selected bearer for a non-cast Aura entering
	// from DigUntil. DigUntilAuraDone distinguishes an answered bearer choice
	// from the first pass; both are consumed at the top of the effect so a
	// nested DigUntil cannot inherit the outer answer.
	DigUntilAuraBearer state.ObjID
	DigUntilAuraDone   bool
	// Clone is the answered DB$ Clone Optional$ True may-copy election
	// (ticket api-clone-trigger-copy; Sarkhan Soul Aflame's "you may have
	// Sarkhan, Soul Aflame become a copy of it"): "yes" performs the copy,
	// "no" -- the decline -- skips it. rules' resumeResolution sets it from
	// the recorded answer before re-running the suspended sub-ability, and
	// CloneDone distinguishes "answered" from the first pass. The asking
	// effect consumes and clears both at the top of its own walk (the fx42
	// scoping discipline), so a nested Clone cannot inherit the outer
	// answer. A no-host run (AskNoHost) keeps the deterministic take stand-in
	// without setting either field, so the byte-identical pre-election
	// behaviour is preserved for fuzz runs.
	Clone     string
	CloneDone bool
	// CloneETB carries the cast/replacement ETB copy election into DB$ Clone.
	// The answer is event-backed on the entering object, so replacement-time
	// resolution and log-only replay use the same selected permanent.
	CloneETB         bool
	CloneChoice      state.ObjID
	CloneChoiceValid bool
	CloneBecome      state.ObjID
	CloneBecomeValid bool
	// ClonePick is the answered DB$ Clone Choices$ <filter> copy-source pick
	// (the standalone "becomes a copy of any creature" family). rules'
	// resumeResolution sets it from the recorded answer before re-running the
	// suspended ability, and ClonePickDone distinguishes "answered" from the
	// first pass. The asking effect consumes and clears both at the top of its
	// own walk (the fx42 scoping discipline), so a nested Clone cannot inherit
	// the outer answer. A no-host run (AskNoHost) keeps the deterministic
	// first-eligible stand-in without setting either field, so a fuzz run is
	// byte-identical to the pre-ask build.
	ClonePick     state.ObjID
	ClonePickDone bool
	// TwoPiles is the answered Fact or Fiction pile-split pick (task
	// twopiles1): the cards the Separator$ player picked into pile A, in the
	// separator's answer order — the rest of the card set, in the order it
	// was offered, is pile B. rules' resumeResolution sets it from the
	// recorded answer before re-running the suspended sub-ability, and
	// TwoPilesDone distinguishes "answered (possibly empty — piles can be
	// empty)" from the first pass. TwoPilesPick is the answered pile pick:
	// "a" means the chooser takes pile A (the ChosenPile$ body runs on pile
	// A, UnchosenPile$ on pile B), "b" the reverse. TwoPilesPickDone
	// distinguishes the second answer from the split answer. The asking
	// effect consumes and clears all four at the top of its own walk (the
	// fx42 scoping discipline), so a nested TwoPiles cannot inherit the
	// outer answers.
	TwoPiles         []state.ObjID
	TwoPilesDone     bool
	TwoPilesPick     string
	TwoPilesPickDone bool
	// Votes is the answered per-voter choice of a fixed-list Vote (the
	// Choices$ shape): one entry per voting player, in Defined$ order, giving
	// the index into voteChoiceNames' option list that player voted for. It is
	// what a real per-player vote ask will fill (today's deterministic
	// stand-in gives every voter option 0, so no live resolution can tie);
	// until that ask lands it is the seam that makes the tie branch --
	// VoteTiedAbility$ -- reachable and testable against a real compiled SA
	// rather than welded to the stand-in. effVote consumes and clears it at
	// the top of its own walk (the fx42 scoping discipline), so a nested Vote
	// poses its own tally; nil means "use the stand-in".
	Votes []int
	// CounterDist is the answered DividedAsYouChoose$ PutCounter pick
	// (Vastwood Hydra's death trigger): the recipients the chooser picked out
	// of the Choices$-eligible battlefield creatures, in answer order.
	// rules' resume arm sets it before re-running the suspended sub-ability,
	// so effPutCounter's re-entry distributes the CounterNum$ total over
	// exactly the chosen creatures instead of asking again;
	// CounterDistDone distinguishes "answered, possibly with no creatures"
	// (a MinChoiceAmount$ 0 decline) from the first pass. The asking effect
	// consumes and clears both at the top of its own walk (the fx42 scoping
	// discipline), so a nested PutCounter cannot inherit the outer answer.
	CounterDist     []state.ObjID
	CounterDistDone bool
	// CounterPick is the answered bare-Choices$ PutCounter pick (Promise of
	// Loyalty's vow: the chooser picked the creature(s) — WITHOUT a
	// DividedAsYouChoose$ total, so each chosen creature takes the full
	// CounterNum$) out of the Choices$-eligible battlefield creatures, in
	// answer order. rules' resume arm sets it before re-running the
	// suspended sub-ability, so effPutCounter's re-entry places the counters
	// on exactly the chosen creatures instead of asking again;
	// CounterPickDone distinguishes "answered" from the first pass. The
	// asking effect consumes and clears both at the top of its own walk (the
	// fx42 scoping discipline), so a nested PutCounter cannot inherit the
	// outer answer.
	CounterPick     []state.ObjID
	CounterPickDone bool
	// AorElect is the answered AddOrRemoveCounter add/remove election
	// ("remove" or "put"); AorKind names the counter kind the election
	// covered — parsed out of the answer's own option encoding
	// ("aor_remove:<kind>"/"aor_put:<kind>"), because an SA with no
	// CounterType$ (Clockspinning) or an EachExistingCounter$ walk
	// (Dramatist's Puppet) elects per kind. rules' "aor_elect" resume arm
	// sets all three Aor fields before re-running the suspended sub-ability;
	// AorDone distinguishes "answered" from the first pass. effAddOrRemove
	// Counter consumes and clears them at the top of its own walk (the fx42
	// scoping discipline), so a nested AddOrRemoveCounter cannot inherit
	// the outer answer.
	AorElect string
	AorKind  string
	AorDone  bool
	// AorAnswered is the list of counter kinds EARLIER rounds of the same
	// AddOrRemoveCounter resolution already answered an election for (seeded
	// from rules' aorAsk pending map — the moveCounterAsk discipline — since
	// every resume builds a fresh Ctx and an EachExistingCounter$ walk asks
	// one election per kind). Consumed and cleared with the fields above.
	AorAnswered []string
	// Proliferate is the answered Proliferate recipient pick (CR 701.27):
	// the permanents and/or players the resolving controller chose to give
	// another counter of each kind already there, in the player's answer
	// order. An object recipient carries Obj; a player recipient carries
	// Player with IsPlayer true (the same state.Target shape a KChoose's
	// mixed option list decodes to, and why the shared "counter_pick" arm --
	// which reads Obj only -- cannot be reused). rules' resume arm sets it
	// before re-running the suspended sub-ability, so effProliferate's
	// re-entry applies exactly the chosen recipients instead of asking again;
	// ProliferateDone distinguishes "answered" from the first pass, so a
	// Min-0 answer that chose nothing is not mistaken for the first pass and
	// re-asked. The asking effect consumes and clears both at the top of its
	// own walk (the fx42 scoping discipline), so a nested Proliferate cannot
	// inherit the outer answer.
	Proliferate     []state.Target
	ProliferateDone bool
	// MoveCounterKind is the answered CounterType$ Any kind pick of a
	// MoveCounter resolution (task movecounter1): the counter kind the
	// chooser picked to move out of the distinct kinds the origin holds, in
	// the offered (deterministic) order. rules' resume arm sets it before
	// re-running the suspended sub-ability; MoveCounterKindDone distinguishes
	// "answered" from the first pass so an answered pick is never re-asked.
	// MoveCounterN is the answered CounterNum$ Any amount of the same
	// resolution: how many counters of the chosen kind(s) move, and
	// MoveCounterNDone distinguishes "answered (possibly zero -- a Min-0
	// decline)" from the first pass. effMoveCounter consumes and clears all
	// four at the top of its own walk (the fx42 scoping discipline), so a
	// nested MoveCounter cannot inherit the outer answers.
	MoveCounterKind string
	// TimeTravelChoice is the answered per-object add/remove/skip election.
	// TimeTravelObjects is the stable per-round snapshot captured by the rules
	// resume point; it prevents removing a counter from shifting the next
	// object's cursor when the live eligible set is recomputed.
	TimeTravelChoice    string
	TimeTravelObjects   []state.ObjID
	TimeTravelIndex     int
	TimeTravelRound     int
	TimeTravelDone      bool
	MoveCounterKindDone bool
	MoveCounterN        int32
	MoveCounterNDone    bool
	// UnlessNext is the index of the UnlessPayer$ payer whose answered
	// unless-pay choice this re-entry applies (0 on a first pass). The
	// unlessProceed gate (Resolve) consumes and clears it; rules' resume
	// arm copies it off the resume point, where Ask stored the asking
	// decision's ResumeTarget. A decline moves the gate on to payer idx+1,
	// so a multi-payer UnlessPayer$ asks each payer in turn.
	UnlessNext int
	// UnlessDiscarded is the object list the settled unless-payment
	// discarded (the UnlessCost$ Discard<...> component's picks): rules'
	// unless_pay resume arm copies it off the resume point when the
	// payment completed, so the continuing walk's ConditionDefined$
	// Discarded gates (Argentum Masticore's "When you discard a card this
	// way") see exactly the card(s) the payment discarded. It rides
	// Ctx.UnlessPay's lifetime rather than being cleared at first read: the
	// unless resolution's whole sub-chain may consult the group, and the
	// fresh-per-resume Ctx already keeps it from leaking into any other
	// resolution.
	UnlessDiscarded []state.Target
	// SacPicks is the answered per-player sacrifice choice on a re-entered
	// Sacrifice resolution: the object(s) the sacrificing player chose to
	// sacrifice, in the player's answer order. SacDone distinguishes
	// "answered (possibly with nothing)" from the first pass and SacTarget
	// identifies the Defined$ target index whose player posed that ask, so
	// re-entry skips targets already processed before suspension and
	// continues asking later targets. The asking effect consumes and clears
	// all three at the top of its own walk (the fx42 scoping discipline), so
	// a nested sacrifice below it poses its own ask instead of inheriting.
	SacPicks  []state.ObjID
	SacDone   bool
	SacTarget int
	// SacOptional is the answered first step of an Optional$ + StrictAmount$
	// sacrifice: "sacrifice" means its player elected the exact batch and
	// "decline" means they did not. It is separate from SacPicks because a
	// KChoose represents a range, while this Forge shape permits only zero or
	// exactly Amount$. SacOptionalTarget identifies that player's target slot.
	SacOptional       string
	SacOptionalTarget int
	// BlightPicks is the answered per-player blight choice on a re-entered
	// Blight resolution (CR 701.60): the creature the blighting player chose
	// to take the −1/−1 counters, in answer order. BlightDone distinguishes
	// "answered" from the first pass and BlightTarget identifies the Defined$
	// target index whose player posed that ask, so re-entry skips targets
	// already processed before suspension and continues asking later targets
	// (the SacPicks/SacDone/SacTarget discipline). The asking effect consumes
	// and clears all three at the top of its own walk (the fx42 scoping
	// discipline), so a nested blight cannot inherit the outer answer.
	BlightPicks  []state.ObjID
	BlightDone   bool
	BlightTarget int
	// ManifestDreadPick is the chosen library object on a resumed CR 701.61
	// resolution; Done distinguishes an answer from the first pass.
	ManifestDreadPick   state.ObjID
	ManifestDreadPlayer state.PlayerID
	ManifestDreadDone   bool
	// RingBearerPick is the answered CR 701.54a Ring-bearer choice on a
	// re-entered Ring tempts resolution: the creature the tempted player chose
	// to become their Ring-bearer. RingBearerDone distinguishes "answered"
	// from the first pass, so re-entry emits the single RingTemptsYou event
	// exactly once instead of asking again or incrementing the count twice.
	// The asking effect consumes and clears both at the top of its own walk
	// (the fx42 scoping discipline), so a nested Ring tempts cannot inherit
	// the outer answer.
	RingBearerPick state.ObjID
	RingBearerDone bool
	// UnlessElected is the answered UnlessType$ election of a Discard carrying
	// UnlessType$ (Thirst for Knowledge's "discard two cards unless you
	// discard an artifact card"): "unless" means the player elected the
	// one-card-of-the-type alternative, "ordinary" the NumCards$ discard. The
	// re-entered effDiscard consumes and clears it (fx42 scoping discipline);
	// it is separate from Discard because the unless arm's own follow-up ask
	// re-uses the ordinary "discard" resume kind for its one-card pick.
	UnlessElected string
	// Arrange is the answered KArrange decision on a re-entered
	// mid-resolution resolution (Ruling J0): true once rules' handleArrange
	// has applied the answered arrangement and emitted the LibraryOrder
	// event, so effRearrangeTopOfLibrary's re-entry lets the resolution
	// continue (the chained SubAbility$ runs) instead of re-asking. The
	// LibraryTarget cursor identifies which library's arrangement completed.
	// False on
	// the first pass, where the effect poses the ask. The arrangement itself
	// lives on the LibraryOrder event, not on Ctx -- the answer shape is
	// applied by the rules handler, unlike Modes/UnlessPay/Discard where the
	// effect re-reads the answer -- so the field is only a done-marker.
	Arrange bool
	// ScryReplacement is the completed CR 616 order choice for this target.
	// Its count/proceed result is consumed once on re-entry, without proposing
	// the same instruction a second time.
	ScryReplacement bool
	ScryCount       int32
	ScryProceed     bool
	// ArrangeTarget is the Defined$-target index whose arrange was the one
	// answered, carried only for a Dig (whose effDig walks several Defined$
	// targets and must keep the deterministic processing for the ones after
	// the asker on the arrange re-entry; the other arrange consumers are
	// single-target). The re-entered effDig consumes and clears it together
	// with Arrange (fx42 scoping). Zero is a legitimate index -- the marker
	// is Arrange, never this field alone.
	ArrangeTarget int
	// MayShuffle is the answered may-shuffle ask a RearrangeTopOfLibrary
	// carrying MayShuffle$ True (Ponder's "You may shuffle.") poses after its
	// KArrange was applied: "yes" means the player shuffled (rules'
	// arrange_mayshuffle resume arm emitted the Shuffle event before
	// re-entering the effect), "no" means they kept the order. Both values
	// are done-markers: the re-entered pass must not pose the ask again. The
	// field is consumed and cleared by the effect (fx42 scoping discipline).
	MayShuffle string
	// SurveilLookOpt is the answered may-look election a surveil poses when a
	// battlefield stat:SurveilNum static with Optional$ True applies to the
	// surveilling player (Enhanced Surveillance's "You may look at an
	// additional two cards each time you surveil"). Each Optional$ static is
	// an independent may effect, so the election offers one option per
	// optional static and the field carries the ACCEPTED static ordinals as a
	// CSV done-marker ("0" or "0,2"; "no" is the answered decline of every
	// static). Anything else -- including the no-host R-9 decline and an
	// unanswered first pass -- keeps the base count. All values are
	// done-markers -- the re-entered pass must not pose the ask again -- and
	// the field is consumed and cleared by effSurveil (fx42 scoping). The
	// answer applies to the asking player only: a multi-player Surveil's
	// other libraries keep their own base count.
	SurveilLookOpt string
	// Hideaway holds the selected top-library card while the Hideaway
	// replacement resumes to exile it; HideawayPicked distinguishes that
	// selected answer from the first pass. HideawayArranged marks completion
	// of the following bottom-order KArrange ask.
	Hideaway         state.ObjID
	HideawayPicked   bool
	HideawayArranged bool
	// SoulbondPartner is the optional pairing answer. SoulbondDone makes a
	// declined empty choice distinct from the initial pass.
	SoulbondPartner state.ObjID
	SoulbondDone    bool
	// Myriad is one per-opponent optional token decision. MyriadTarget is the
	// index in the deterministic eligible-opponent list that just answered;
	// MyriadDone distinguishes that answer from the first pass, and
	// MyriadCreate says whether it creates that target's token. Re-entry emits
	// the selected token, then asks the next opponent, so each may choice is
	// independent and no answer is retained by a nested Myriad.
	MyriadTarget int
	MyriadDone   bool
	MyriadCreate bool
	// ManaAmount and ManaType are the in-flight unit of mana a ProduceMana
	// replacement modifies. rules seeds them from a ManaAdd event and then
	// emits the transformed event, so ReplaceMana never writes game state
	// directly and replay records the final mana production normally.
	ManaAmount int32
	ManaType   string
	// ManaChoice is the W/U/B/R/G answer to a choice-valued ReplaceMana
	// body (ReplaceType$ Any, ReplaceColor$ Chosen, ReplaceMana$ Any).
	// Rules parks the ManaAdd and supplies this on resume.
	ManaChoice string
	// ManaChoices is the allocation chosen for Produced$ Combo with Amount$ >
	// 1. Each entry is one W/U/B/R/G unit; effMana consumes it with Amount 1
	// so a split such as U,R produces one of each rather than doubling both.
	ManaChoices []string
	// HandMove is the answered Origin$ Hand ChangeZone selection.
	HandMove     []state.ObjID
	HandMoveDone bool
	// HandMoveTarget is the index of the per-owner hidden-hand chooser whose
	// ask was answered (rv2b r2: an owner-SELECTED Origin$ Hand ChangeZone --
	// DefinedPlayer$/ValidTgts$ naming the hands -- asks each hand owner in
	// turn). It keeps a resumed answer attached to the exact owner that
	// asked, so owners before the cursor (already answered on earlier
	// passes) are skipped and owners after it continue the chain, the same
	// continuation effDig's DigTarget carries. Consumed and cleared at the
	// top of the walk with HandMove/HandMoveDone (fx42 scoping).
	HandMoveTarget int
	// HandMoveConfirm is the answered Optional$ confirmation for a hidden-hand
	// ChangeZone whose script carries an Optional$ marker -- Forge's
	// confirmAction gate in ChangeZoneEffect.changeHiddenOriginResolve, which
	// runs BEFORE any card is picked. "yes" accepts the fetch and every other
	// answer declines it; HandMoveConfirmDone distinguishes "answered" from the
	// first pass, and HandMoveConfirmTarget is the index of the hand owner whose
	// confirmation was answered (the same per-owner cursor HandMoveTarget
	// carries). A declined confirmation leaves the remembered set untouched; an
	// accepted one lets the walk clear it exactly once before the pick, even
	// when the pick ends up empty. Consumed and cleared at the top of the walk
	// with HandMove/HandMoveDone (fx42 scoping).
	HandMoveConfirm       string
	HandMoveConfirmDone   bool
	HandMoveConfirmTarget int
	// HiddenPick is the answered Hidden$ True public-origin pick (hiddenpick1):
	// the chooser picked which of the ChangeType$-eligible cards in the
	// origin zone(s) move to Destination$. HiddenPickDone distinguishes
	// "answered, possibly with no cards" from the first pass; HiddenPick
	// preserves the player's answer order. effHiddenPick consumes and
	// clears both at the top of its walk (fx42 scoping), so a nested pick
	// cannot inherit the outer answer.
	HiddenPick     []state.ObjID
	HiddenPickDone bool
	// HiddenPickTarget is the index of the fetch player whose hidden-pick ask
	// was answered, the same continuation HandMoveTarget carries: owners
	// before the cursor are skipped on re-entry, owners after it continue
	// the chain. Consumed and cleared with the pair above.
	HiddenPickTarget int
	// HiddenPickConfirm is the answered Optional$ confirmation for a Hidden$
	// True public-origin ChangeZone pick -- Forge's confirmAction gate in
	// ChangeZoneEffect.changeHiddenOriginResolve, which runs BEFORE the card
	// pick (the same gate the hidden-hand walk's HandMoveConfirm carries).
	// "yes" accepts the fetch and every other answer declines it;
	// HiddenPickConfirmDone distinguishes "answered" from the first pass, and
	// HiddenPickConfirmTarget is the index of the fetch player whose
	// confirmation was answered (the same per-owner cursor HiddenPickTarget
	// carries). A declined confirmation skips the fetch player without a pick
	// ask and clears nothing; an accepted one enters the fetch, whose answered
	// pick (or empty pool) clears exactly as before. Consumed and cleared at
	// the top of effHiddenPick with HiddenPick/HiddenPickDone (fx42 scoping).
	HiddenPickConfirm       string
	HiddenPickConfirmDone   bool
	HiddenPickConfirmTarget int
	// DefinedLibraryMove is the answered Optional$ True choice for an
	// object-valued Defined$ fetch list from Origin$ Library. "yes" moves the
	// list; "no" leaves it in place. It is consumed by
	// moveDefinedLibraryObjects before a nested fetch list can inherit it.
	DefinedLibraryMove string
	// RevealOpt is the answered RevealOptional$ yes/no on a re-entered
	// mid-resolution reveal (task fb-3f1cc033, the Delver of Secrets
	// PeekAndReveal shape): "yes" means the peeking player chose to reveal
	// (the Note is emitted, RememberRevealed$ fires) and "no" means they
	// declined (no Note, Remembered unchanged). "" on the first pass, where
	// the effect poses the ask (or, when the host cannot ask, falls back to
	// the mandatory reveal — the same R-9 degradation Scry/Surveil carry).
	// effReveal consumes and clears it before continuing, so a nested
	// RevealOptional$ peek in the same walk poses its own ask (fx42
	// scoping).
	RevealOpt string
	// RevealOptTarget is the Defined$ target index whose reveal_optional
	// yes/no was answered (the decision's ResumeTarget), the same per-target
	// cursor LookAckTarget and RevealPickTarget carry. Meaningful only while
	// RevealOpt is non-empty: targets before the cursor were fully processed
	// on the pass that suspended and are skipped, the cursor target consumes
	// the answer, and every LATER optional reveal in the walk poses its own
	// yes/no. Without it a reveal_optional resolving over several Defined$
	// players answered for target 0 and then either silently applied that
	// same yes/no to every later target (a non-pickable reveal) or left the
	// later target's ask unposed (a pickable one), because neither a yes nor
	// a no can be attributed to a target it was never asked of. Consumed and
	// cleared with RevealOpt.
	RevealOptTarget int
	// RevealPick is the answered mid-resolution hand-reveal pick (task
	// infernaltutor1): the ids of the hand cards the revealing player chose
	// to reveal. A hand reveal whose eligible pool is strictly larger than
	// the count it must show (Infernal Tutor's "Reveal a card from your
	// hand", or an AnyNumber$/Optional$ miss) is a CHOICE Forge poses to the
	// pool's owner; effReveal poses it as a KChoose with ResumeKind
	// "reveal_pick" and this field carries the answer back. Non-nil means
	// answered (a legitimate empty answer is a non-nil zero-length slice,
	// exactly the Ctx.Discard convention), so an empty answer ("reveal
	// none") is distinguishable from a first pass. effReveal consumes and
	// clears it at the top of its own walk so a nested reveal poses its own
	// ask (fx42 scoping).
	RevealPick []state.ObjID
	// RevealPickTarget is the Defined$ target index whose reveal_pick was
	// answered (the decision's ResumeTarget), the same per-target cursor
	// LookAckTarget carries. Meaningful only while RevealPick is non-nil:
	// targets before the cursor were fully processed on the pass that
	// suspended and are skipped, the cursor target consumes the answer, and
	// every LATER pickable reveal in the walk poses its own ask. Without it,
	// a pickable reveal resolving over several Defined$ players applied the
	// first player's answer to every subsequent player's distinct hand —
	// none of those ids can occur in another hand, so n became 0 and no
	// later player was asked or revealed. Consumed and cleared with
	// RevealPick.
	RevealPickTarget int
	// ChosenType is the answered mid-resolution ChooseType pick (task ct1):
	// the creature type the chooser picked out of the TypeChoices list, set
	// by rules' "choosetype" resume arm before the suspended sub-ability is
	// re-run. effChooseType's re-entry emits the one Choose event the
	// fallback would have emitted, with the answered type instead, so the
	// downstream Card.ChosenType readers see exactly the shape they already
	// read. A valid answer is never empty (the option list's last resort is
	// "Human"), so non-empty IS the answered marker, and the asking effect
	// consumes and clears it at the top of its walk (the fx42 scoping
	// discipline), so a nested ChooseType cannot inherit the outer answer.
	ChosenType string
	// ChosenColor is the answered mid-resolution ChooseColor pick (task
	// cli-20260923T060000Z-choose-color): the option Label (the full colour
	// name, e.g. "Black") the chooser picked out of the fixed WUBRG list,
	// set by rules' "choosecolor" resume arm before the suspended
	// sub-ability is re-run. effChooseColor's re-entry consumes and clears
	// it and emits the one Choose event the deterministic fallback would
	// have emitted, with the answered colour's WUBRG letter, so the
	// downstream o.ChosenColor readers see exactly the shape they already
	// read. A valid answer is never empty (the option list is total -- the
	// last-resort degenerate pick is always offerable), so non-empty IS the
	// answered marker, and the asking effect consumes and clears it at the
	// top of its walk (the fx42 scoping discipline), so a nested
	// ChooseColor cannot inherit the outer answer.
	ChosenColor string
	// ChangeTextFrom/ChangeTextTo are the answered mid-resolution api:ChangeText
	// word asks: the pair of words the chooser picked for the substitution's
	// "from" and "to" halves. rules' "changetext" resume arm sets whichever
	// the answered option's Kind names before the suspended sub-ability is
	// re-run; effChangeText's re-entry consumes and clears both once it has
	// resolved the pair (the fx42 scoping discipline), so a nested ChangeText
	// cannot inherit the outer answer. Non-empty IS the answered marker for
	// each half independently (the option labels are never empty), because the
	// two halves may be asked sequentially across re-entries.
	ChangeTextFrom, ChangeTextTo string
	// ETBColorRecorded marks the ONE ChooseColor invocation that must not
	// ask: the as-enters ENTRY-choice body (K:ETBReplacement:Other:
	// ChooseColor). The entry machinery (rules' applyETBChoiceReplacement ->
	// resumeETBEntry) already posed the entry ask and recorded the answer on
	// the entering object before this body runs at the re-emitted MoveZone,
	// so rules' replCtx flags that invocation and effChooseColor keeps the
	// historical no-op for it alone. Without the flag an unconditional
	// o.ChosenColor guard also suppressed a FRESH resolution-time ask after
	// an earlier ChooseColor had set the field (a second sequential SA in
	// one resolution, or an ability activation on an already-chosen
	// permanent) -- the stale-source-state bug the same ticket's review
	// named. Consumed and cleared by the effect (the fx42 scoping
	// discipline), so a nested ChooseColor deeper in the same chain poses
	// its own fresh ask.
	ETBColorRecorded bool
	// ChosenNumberPick is the answered mid-resolution ChooseNumber pick (task
	// cli-20260923T060000Z-choose-number): the option Amount the chooser picked
	// out of the number list, set by rules' "choosenumber" resume arm before
	// the suspended sub-ability is re-run. effChooseNumber's re-entry consumes
	// and clears it and emits the one Choose event the deterministic fallback
	// would have emitted, with the answered number. A legitimate answer can be
	// ZERO, so ChosenNumberAnswered is the answered marker (a bare int32 could
	// not tell "answered 0" from "never asked"). This field is the
	// mid-resolution pick and is DISTINCT from ChosenNumber, which carries the
	// Effect's frozen SetChosenNumber$ binding (Count$ChosenNumber); the two
	// never alias. Consumed and cleared at the top of the effect's walk (the
	// fx42 scoping discipline), so a nested ChooseNumber cannot inherit the
	// outer answer.
	ChosenNumberPick     int32
	ChosenNumberAnswered bool
	// ETBNumberRecorded marks the ONE ChooseNumber invocation that must not
	// ask: the as-enters ENTRY-choice body (K:ETBReplacement:Other:
	// ChooseNumber). The entry machinery (rules' applyETBChoiceReplacement ->
	// resumeETBEntry) already posed the entry ask and recorded the answer on
	// the entering object before this body runs at the re-emitted MoveZone, so
	// rules' replCtx flags that invocation and effChooseNumber keeps the
	// historical no-op for it alone. Without the flag an unconditional
	// o.ChosenNumber guard also suppressed a FRESH resolution-time ask after an
	// earlier ChooseNumber had set the field -- the stale-source-state bug the
	// sibling colour ticket's review named. The flag is what makes the entry
	// no-op exact even when the recorded entry answer is 0 (the value a bare
	// o.ChosenNumber guard cannot distinguish from unset). Consumed and cleared
	// by the effect (the fx42 scoping discipline), so a nested ChooseNumber
	// deeper in the same chain poses its own fresh ask.
	ETBNumberRecorded bool
	// ManaReflectedColor is the answered mid-resolution AB$ ManaReflected
	// colour pick: the chosen option's structured ManaSymbol ("W"), set by
	// rules' "manareflected" resume arm before the suspended sub-ability is
	// re-run. effManaReflected's re-entry consumes and clears it, accepts the
	// colour only when the resolution still offers it, and emits the one
	// ManaAdd the deterministic fallback would have emitted. Empty on the
	// first pass, where the effect poses the ask (or, on a host that cannot
	// answer, the R-9 stand-in).
	ManaReflectedColor string
	// LookAck is the answered bare-look "Continue" ack (lookack, task
	// fb-20260917T232325Z-35cfca4b): the looker acknowledged the private
	// look a NoReveal$ / mandatory-Look$ Reveal-family effect is about to
	// record, so the Secret Note lands below the modal instead of streaming
	// past ungated. There is no decline — the ask paces the look, it does
	// not permit it — so the resume arm sets it on ANY answer, together with
	// LookAckTarget: the decision's ResumeTarget, the index of the Defined$
	// target whose ack was answered. effReveal consumes and clears BOTH at
	// the top of its own walk (fx42 scoping): targets before LookAckTarget
	// were fully processed on the pass that suspended and are skipped,
	// LookAckTarget itself emits without re-asking, and every LATER bare
	// look in the walk poses its own ack — the per-target cursor (the
	// DigTarget pattern) is what keeps a multi-target bare look (Case the
	// Joint's "look at the top card of each player's library", Defined$
	// Player) terminating with exactly one Continue per target instead of
	// re-asking the earlier targets' notes unboundedly.
	LookAck bool
	// LookAckTarget is the Defined$ target index whose look_ack was answered
	// (the decision's ResumeTarget). Meaningful only while LookAck is set;
	// consumed and cleared with it.
	LookAckTarget int
	// DrawOpt is the answered OptionalDecider$ yes/no on a re-entered
	// mid-resolution Draw (Mystic Remora, Rhystic Study): "yes" draws and
	// "no" declines, the same two-way answer the RevealOpt ask poses. ""
	// on the first pass, where effDraw poses the ask (or, when the host
	// cannot ask, keeps the pre-ask mandatory draw — the R-9 degradation).
	// Consumed and cleared before the draw loop, so a nested optional draw
	// in the same walk poses its own ask (fx42 scoping).
	DrawOpt string
	// DrawUptoIdx/DrawUptoCount/DrawUptoAnswered carry an Upto$ Draw's
	// per-target continuation (Arcane Denial, Truce): Idx is the Defined$
	// target index whose "draw up to N" ask or answered batch is in flight,
	// Count the answered count for it, Answered distinguishes an answered
	// ZERO (draw nothing) from a target not yet asked. rules' draw_upto
	// resume arm sets all three from the recorded answer (Count = the
	// number of chosen card options), and the dredge arm restores them
	// across a Dredge choice parked inside the batch (riding the ask's
	// ResumeUpto rider). effDraw consumes the three as it completes each
	// target, so the next target poses its own ask (fx42 scoping).
	DrawUptoIdx      int32
	DrawUptoCount    int32
	DrawUptoAnswered bool
	// InvestigateOptIdx/InvestigateOpt carry an Optional$ True Investigate's
	// per-player continuation (Will the Wise's "each opponent may
	// investigate", Nick Valentine, Private Eye's "you may investigate"):
	// Idx is the actingPlayers index whose may-investigate ask or answered
	// election is in flight, Opt the answered election ("yes" or "no") for
	// that player. rules' "investigate_optional" resume arm sets both from
	// the recorded answer (Idx = the ask's ResumeTarget cursor). effInvestigate
	// consumes the marker as it completes each player, so the next player
	// poses its own ask (fx42 scoping), and resets Idx when the walk finishes
	// so a chained optional Investigate poses its own elections.
	InvestigateOptIdx int32
	InvestigateOpt    string
	// TapOrUntap is the answered mid-resolution TapOrUntap election
	// (api:TapOrUntap): the kind of the chosen option, "tap" or "untap". ""
	// on the first pass, where effTapOrUntap poses the ask (or, when the host
	// cannot ask, applies option 0 — the state-changing choice — silently,
	// the R-9 stand-in). TapOrUntapObj is the target the answer was elected
	// for (read off the answered option's Obj), and TapOrUntapDone is the
	// answered marker: the ask's two options are both always legal, so the
	// answered state cannot be inferred from the answer alone. Consumed and
	// cleared at the point of application (fx42 scoping), so a later target
	// poses its own ask and a nested TapOrUntap cannot inherit the answer.
	TapOrUntap     string
	TapOrUntapObj  state.ObjID
	TapOrUntapDone bool
	// ExploreObj/ExploreCard/ExploreChoice/ExploreDone carry one pending
	// explore across the LCI destination ask (api:Explore): "...then put
	// the card back or put it into your graveyard" (CR 701.35a). The
	// nonland explore reveals its top card, poses the KChoose (option 0 is
	// the state-changing "graveyard", option 1 "back on top", the
	// TapOrUntap ordering discipline), and parks with ExploreObj the
	// explorer and ExploreCard the revealed card. rules' "explore" resume
	// arm re-enters with ExploreDone set, ExploreChoice the answered kind
	// and ExploreCard/ExploreObj restored from the resume point. Consumed
	// and cleared at the point of application (fx42 scoping), so the
	// pending explorer's remaining explores and every later target pose
	// their own fresh path.
	ExploreObj    state.ObjID
	ExploreCard   state.ObjID
	ExploreChoice string
	ExploreDone   bool
	// ExploreCount is how many of the pending explorer's Num$ explores were
	// complete before the one whose election is answered (the ask's
	// Decision.ResumeExploreDone, restored by the same resume arm). The
	// resumed effExplore continues its count there: restarting it at zero
	// made every resume apply the pending explore and pose a fresh one, so
	// an "explores X times" never stopped on nonland reveals (round-10
	// cardfuzz livelock, Jadelight Spelunker). Cleared with the others.
	ExploreCount int32
	// ConniveObj/ConniveDiscard/ConniveDone carry one pending connive
	// discard (api:Connive, task connive1): ConniveDone marks an ANSWERED
	// discard for the conniver parked in ConniveObj, ConniveDiscard the
	// chosen card ids. rules' "connive" resume arm re-enters with
	// ConniveDone set and both other fields restored from the resume
	// point. Consumed and cleared at the point of application (fx42
	// scoping), so a later conniving target poses its own fresh ask.
	ConniveObj     state.ObjID
	ConniveDiscard []state.ObjID
	ConniveDone    bool
	// LastRoll/LastRollName carry the result of a DB$ RollDice this same
	// resolution just made (effects/dice.go), under the SVar name its
	// ResultSVar$ parameter named (usually "Result" or "X"). evalCountExpr's
	// SVar$ head resolves a body of the form "SVar$<name>" against them, so
	// a chained sub's own SVar body (Velukan Dragon's
	// "SVar:X:SVar$Result/Minus.1") and a ConditionCheckSVar$ can read the
	// roll. Zero/"" on any resolution that did not roll, and the values are
	// never persisted -- a roll that suspends and resumes loses them, the
	// same per-resolution lifetime every other Ctx field has. RollPubs is
	// the general form of the same publication (both are read through
	// effects.dice.go's rollPublished, and this slot stays the primary
	// result's mirror for the existing readers).
	LastRoll     int32
	LastRollName string
	// RollPub is one name→value publication a DB$ RollDice of this
	// resolution made, beyond the primary ResultSVar$ slot above:
	// ChosenSVar$/OtherSVar$ (the Endeavor cycle's choose-one-result), and
	// the MaxRollsResults$/EvenOddResults$ counts ("MaxRolls",
	// "EvenResults", "OddResults" -- Luck Bobblehead). Read by Name's
	// bare-name fallback and evalCountExpr's SVar$ head through
	// rollPublished, and by Ctx.SpecContext's numeric-RHS resolver, so a
	// chained sub's filter spec (Valiant Endeavor's Creature.powerGEX,
	// Arcane Endeavor's Instant.cmcLEY) reads the roll too. Never persisted
	// across a suspension -- the chosen/other publications are rebuilt from
	// the answered decision on the roll resume (Ctx.RollResults/RollPick),
	// the same per-resolution lifetime as LastRoll.
	RollPubs []RollPub
	// RollResults/RollPick/RollDone carry the ANSWERED choose-one-result ask
	// on a re-entered mid-resolution RollDice (rules/resolution.go's "roll"
	// arm): RollResults is the per-die results the asking first pass rolled
	// (carried verbatim on the decision and the resume point), RollPick the
	// dice the player picked (each entry a roll Option's Index), RollDone
	// the answered marker. The re-entered effRollDice publishes
	// ChosenSVar$ = the sum of the picked dice's results and OtherSVar$ =
	// the sum of the rest, then lets Resolve chain the SubAbility$; it
	// consumes and clears all three at its top (the fx42 scoping
	// discipline), so a nested RollDice below this walk poses its own ask.
	RollResults []int32
	RollPick    []int
	RollDone    bool
	// VotePicks/VoteAnswer/VoteDone/VoteTarget carry the per-voter api:Vote
	// PLAYER ballot (VotePlayer$, task votepb1) across a mid-resolution ask.
	// VotePicks is every voter's answer accumulated so far, in voter order
	// (one entry per voter already answered; a zero Target is a voter who
	// cast no vote because the ballot held no admissible entry). VoteTarget
	// is the index into Defined$'s voter list whose ask was just posed, and
	// VoteAnswer the decision's chosen option as a player Target -- rules'
	// "vote" resume arm rebuilds both from the decision's ResumeTarget/
	// ResumeChoices, and effPlayerVote consumes and clears VoteAnswer/
	// VoteDone at the top of its own walk (the fx42 scoping discipline), so
	// a nested Vote poses its own ballot. The same fields the fixed-list
	// shape's Ctx.Votes seam mirrors: VotePicks is the player-ballot answer
	// list where Ctx.Votes is the fixed-list option-index list.
	VotePicks  []state.Target
	VoteAnswer []state.Target
	VoteDone   bool
	VoteTarget int
	// Demonstrate carries the demonstrate trigger's answered asks (CR
	// 702.152) across a mid-resolution ask. DemonstrateStage is which ask
	// was answered -- 0 the may-copy election, 1 the opponent choice --
	// DemonstrateYes the election's answer, DemonstrateOpp the answered
	// opponent (player targets). rules' "demonstrate" resume arm rebuilds
	// all four from the decision's ResumeTarget and answer, and
	// effDemonstrate consumes and clears all four at the top of its walk
	// (the fx42 scoping discipline), so a nested Demonstrate below this
	// walk poses its own asks.
	DemonstrateDone  bool
	DemonstrateStage int
	DemonstrateYes   bool
	DemonstrateOpp   []state.Target
	// Cipher carries the api:Cipher (CR 702.99a) encode ask's answer across a
	// mid-resolution suspension. CipherDone marks the ask answered and
	// CipherPick is the chosen creature (one object target; an empty slice is
	// the decline). rules' "cipher" resume arm rebuilds both from the
	// decision's answer, and effCipher consumes and clears both at the top of
	// its walk (the fx42 scoping discipline), so a nested Cipher poses its own
	// ask.
	CipherDone bool
	CipherPick []state.Target
	// VoteCounts is the per-subject tally the most recent api:Vote left for
	// this resolution's AmountFromVotes$ readers (effects/choose_control.go's
	// effRepeatEach): one entry per ballot subject -- every player the
	// player-ballot universe admitted, or every permanent a card ballot
	// admitted -- with the votes it received. Forge's VoteEffect stores the
	// same tally as VoteNum<SVar>s on the vote ability and RepeatEachEffect's
	// setVoteAmount reads it back per loop subject; this is the engine's
	// per-resolution form of that side channel, read through voteCountFor. It
	// is built on the pass the ballot completes and lives on the resolution
	// Ctx, so the chained SubAbility$ (Mob Verdict's DBRepeatOpp) sees it; a
	// vote with no ballot publishes nothing (the field stays nil).
	VoteCounts []VoteCount
	// VotePublished/VotePublishedSet are the per-iteration binding the
	// AmountFromVotes$ RepeatEach writes before resolving one loop body: the
	// vote count of the iteration's subject, resolved by the reserved name
	// "Votes" through runtimePublished -- the same seam Ctx.RollPubs serves
	// for DB$ RollDice, and the name Forge's setVoteAmount sets
	// (sa.setSVar("Votes", "Number$<n>")). Set only on an
	// AmountFromVotes$ loop's per-iteration Ctx copy, so an ordinary SVar
	// table is never shadowed outside one loop body.
	VotePublished    int32
	VotePublishedSet bool
	// FlipMemory is this resolution's coin-flip memory (nil until a flip
	// happens). It is a POINTER so a Ctx copy -- a RepeatEach iteration's
	// cc := *c, or the fresh Ctx a resume rebuilds -- shares the SAME memory:
	// flips performed before a suspension or in a loop iteration stay visible
	// to the chained reader. effFlipCoin lazily allocates it and mutates it in
	// place (never replacing the pointer), so the value rules' Ask captured
	// onto the pending resume point stays live. The cumulative-upkeep FlipCoin
	// cost action (rules/cumulative.go) does not go through effFlipCoin and so
	// does not populate it (see AGENTS.md).
	FlipMemory *FlipMemory
	// FlipRest is the resume cursor of a DB$ FlipCoin loop re-entered after a
	// per-flip sub-ability suspended (FlipUntilYouLose$ or Amount$ > 1). rules'
	// resumeResolution sets it from the continuation frame before re-running
	// the FlipCoin SA; effFlipCoin consumes and clears it at the top of its own
	// walk (the fx42 scoping discipline), so a nested FlipCoin poses its own
	// loop. Nil on every ordinary first pass.
	FlipRest *FlipRest
	// TokenRest is the resume cursor of a DB$ Token whose mint parked behind
	// a CR 616.1 order ask (effects/token.go). rules' resumeResolution sets it
	// from the continuation frame -- with Parked filled from the objects the
	// answer actually minted -- before re-running the Token SA; effToken
	// consumes and clears it at the top of its walk. Nil on every ordinary
	// first pass.
	TokenRest *TokenRest
	// tokensSuspended is set by effToken when a mint parked and it handed its
	// continuation to the host (TokenRest): Resolve then defers the SA's
	// ImprintCards$/ClearImprinted$ tail to the re-entry that finishes the
	// mints, so it sees (and clears after) the tokens. Consumed by Resolve.
	tokensSuspended bool
	// ClashWon records the resolving controller's CR 701.31 clash outcome:
	// true when their revealed card had the strictly higher mana value, false
	// on a loss and on a tie (no winner). effClash sets it from the reveal
	// comparison and selects its Forge WinSubAbility$/OtherwiseSubAbility$
	// branch through it, so the branch and the emitted events.Clash records
	// cannot disagree about who won. It is resolution-scratch like
	// Targets/SVars -- never event-encoded (the marker carries the same bit
	// in Amount), a replay re-derives the same value.
	ClashWon bool
	// ClashWinner is the seat that won the clash, or the resolving controller
	// on a tie (CR 701.31's no-winner case, where Forge reports"False" to
	// both clashing players). effClash's branch read uses ClashWon; this field
	// is kept so a chained SubAbility$ (or a future Defined$ referent) can
	// name the winner without re-deriving it from the log.
	ClashWinner state.PlayerID
}

// VoteCount is one ballot subject's tally (see Ctx.VoteCounts).
// Subject is a player Target for a player ballot, or an object Target for a
// card ballot.
type VoteCount struct {
	Subject state.Target
	Count   int
}

// RollPub is one name→value publication (see Ctx.RollPubs).
type RollPub struct {
	Name  string
	Value int32
}

// FlipResult is one coin flip a resolution performed (see FlipMemory.Results).
// Player is the flipper, Heads the outcome (Forge's heads = win, tails = lose).
type FlipResult struct {
	Player state.PlayerID
	Heads  bool
}

// FlipMemory is a resolution's coin-flip memory. It is held by POINTER on the
// resolving Ctx so that a Ctx copy (a RepeatEach iteration's cc := *c, or the
// fresh Ctx a resume rebuilds) shares the SAME memory: a flip the iteration or
// the pre-suspension pass performed stays visible to the loop's chained
// SubAbility$ and to the resumed walk. A nil *FlipMemory means this resolution
// has performed no flip.
type FlipMemory struct {
	// Results is every RememberResult$ True flip of this resolution's chain,
	// in flip order, the source of Defined$ FlippedHeads/FlippedTails.
	Results []FlipResult
	// CurWin/CurLoss are the per-flip Wins/Losses SVars Forge's FlipCoinEffect
	// publishes (1 to the side the current flip landed on, 0 to the other), so
	// a per-flip WinSubAbility$'s NumCards$ Wins / TokenAmount$ Wins /
	// CounterNum$ Wins is 1 per winning flip. Set is the presence gate.
	CurWin  int32
	CurLoss int32
	Set     bool
	// RememberNumber/RememberNumberKind are the CUMULATIVE tally of the side
	// RememberNumber$ names (Forge's rememberedNumber, which
	// Count$RememberedNumber reads -- distinct from the per-flip Wins/Losses
	// SVars above).
	RememberNumber     int32
	RememberNumberKind string
}

type Effect func(h Host, c *Ctx, sa *cards.SA)

// atomicMap is a copy-on-write string-keyed map. Writes are rare — native
// primitives register themselves from init() and the only other writer is
// M3's plugin tier overriding one at runtime — so they pay the cost of taking
// a mutex and copying the snapshot. Reads are the hot path: Resolve does one
// lookup per effect resolution, on every match, in its own goroutine, so
// readers do a single atomic load and never block or contend with a writer or
// each other. A writer never mutates a map a reader might already hold: it
// always builds a fresh map and swaps the pointer.
type atomicMap[V any] struct {
	mu  sync.Mutex
	ptr atomic.Pointer[map[string]V]
}

func newAtomicMap[V any]() *atomicMap[V] {
	a := &atomicMap[V]{}
	m := map[string]V{}
	a.ptr.Store(&m)
	return a
}

func (a *atomicMap[V]) load() map[string]V { return *a.ptr.Load() }

// set installs or replaces one entry.
func (a *atomicMap[V]) set(key string, val V) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := *a.ptr.Load()
	next := make(map[string]V, len(old)+1)
	for k, v := range old {
		next[k] = v
	}
	next[key] = val
	a.ptr.Store(&next)
}

// setAll installs or replaces several entries as a single atomic publish.
func (a *atomicMap[V]) setAll(kv map[string]V) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := *a.ptr.Load()
	next := make(map[string]V, len(old)+len(kv))
	for k, v := range old {
		next[k] = v
	}
	for k, v := range kv {
		next[k] = v
	}
	a.ptr.Store(&next)
}

// delete removes the given keys, if present.
func (a *atomicMap[V]) delete(keys ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := *a.ptr.Load()
	next := make(map[string]V, len(old))
	for k, v := range old {
		next[k] = v
	}
	for _, k := range keys {
		delete(next, k)
	}
	a.ptr.Store(&next)
}

type effectRegistrySnapshot struct {
	byName map[string]Effect
	byCode []Effect
}

type effectRegistry struct {
	mu  sync.Mutex
	ptr atomic.Pointer[effectRegistrySnapshot]
}

func newEffectRegistry() *effectRegistry {
	r := &effectRegistry{}
	r.ptr.Store(&effectRegistrySnapshot{
		byName: map[string]Effect{},
		byCode: make([]Effect, int(cards.APICodeCount)),
	})
	return r
}

func (r *effectRegistry) load() *effectRegistrySnapshot { return r.ptr.Load() }

func (r *effectRegistry) set(name string, effect Effect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.ptr.Load()
	next := &effectRegistrySnapshot{
		byName: make(map[string]Effect, len(old.byName)+1),
		byCode: append([]Effect(nil), old.byCode...),
	}
	for key, registered := range old.byName {
		next.byName[key] = registered
	}
	next.byName[name] = effect
	if code := cards.APICodeForName(name); code != cards.APIUnknown {
		next.byCode[int(code)] = effect
	}
	r.ptr.Store(next)
}

func (r *effectRegistry) delete(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.ptr.Load()
	next := &effectRegistrySnapshot{
		byName: make(map[string]Effect, len(old.byName)),
		byCode: append([]Effect(nil), old.byCode...),
	}
	for key, registered := range old.byName {
		next.byName[key] = registered
	}
	for _, name := range names {
		delete(next.byName, name)
		if code := cards.APICodeForName(name); code != cards.APIUnknown {
			next.byCode[int(code)] = nil
		}
	}
	r.ptr.Store(next)
}

var registry = newEffectRegistry()

// Register installs an implementation for a Forge API name. Called from init
// functions in this package; re-registering replaces, which is what lets the
// plugin tier in M3 override a native primitive. Safe to call concurrently
// with Resolve and Supported (and with itself).
func Register(api string, fn Effect) { registry.set(api, fn) }

func unregister(apis ...string) { registry.delete(apis...) }

// Supported reports the primitive set this build implements, in the same
// prefixed form cards.Face.Primitives uses, so it feeds straight into
// cards.Registry.Coverage.
func Supported() map[string]bool {
	reg := registry.load()
	non := supportedNonAPI.load()
	out := make(map[string]bool, len(reg.byName)+len(non))
	for k := range reg.byName {
		out["api:"+k] = true
	}
	for k := range non {
		out[k] = true
	}
	return out
}

// supportedNonAPI holds keyword, trigger, static and replacement primitives,
// which are implemented in rules rather than as effect functions. Tasks 18-20
// fill it in.
var supportedNonAPI = newAtomicMap[bool]()

// RegisterNonAPI records a keyword, trigger, static or replacement primitive as
// implemented. The name must carry its prefix, e.g. "kw:Flying". Safe to call
// concurrently with Resolve and Supported (and with itself).
func RegisterNonAPI(prefixed ...string) {
	kv := make(map[string]bool, len(prefixed))
	for _, p := range prefixed {
		kv[p] = true
	}
	supportedNonAPI.setAll(kv)
}

const maxChain = 32

// CloneTargetControllerLKI returns an independent copy of a target-controller
// LKI map threaded across a suspension (rules' resumePoint). The map is treated
// as immutable once captured -- Resolve never mutates a non-nil one -- so a
// shared reference would be safe, but an explicit copy keeps a cloned engine's
// pending frame from ever aliasing another's.
func CloneTargetControllerLKI(m map[state.ObjID]state.PlayerID) map[state.ObjID]state.PlayerID {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID]state.PlayerID, len(m))
	for id, controller := range m {
		out[id] = controller
	}
	return out
}

// CloneTargetSpellLKI returns an independent copy of a target-spell LKI set
// threaded across a suspension (rules' resumePoint). The map is treated as
// immutable once captured, but an explicit copy keeps a cloned engine's
// pending frame from ever aliasing another's.
func CloneTargetSpellLKI(m map[state.ObjID]bool) map[state.ObjID]bool {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID]bool, len(m))
	for id, ok := range m {
		out[id] = ok
	}
	return out
}

// CloneTargetCountersLKI returns an independent copy of a target-counters LKI
// map threaded across a suspension (rules' resumePoint). The map is treated as
// immutable once captured, but an explicit copy -- inner slices included --
// keeps a cloned engine's pending frame from ever aliasing another's.
func CloneTargetCountersLKI(m map[state.ObjID][]state.Counter) map[state.ObjID][]state.Counter {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID][]state.Counter, len(m))
	for id, cs := range m {
		out[id] = append([]state.Counter(nil), cs...)
	}
	return out
}

// Resolve runs an ability and every sub-ability chained beneath it.
// effectFrameHost is implemented by the rules engine to publish the Effect
// registration identity a resolution is currently running under, so an ask
// posed from anywhere inside that body (Host.Ask) captures it onto the
// decision's resume state and the resumed walk keeps the same registration
// bound. It is optional so the effects test doubles stay small.
type effectFrameHost interface {
	GetCurrentEffectFrame() EffectFrame
	SetCurrentEffectFrame(EffectFrame)
}

// resolutionCtxHost is implemented by the rules engine to publish the Ctx of
// the Resolve chain that is CURRENTLY running, so the ask boundary can stamp
// the chain's live TargetUnique$ accumulator onto EVERY decision it poses
// (Host.Ask copies it onto the decision's resume state, hence onto the
// pending resumePoint). The accumulator is appended to in place as the walk
// runs, so a snapshot taken at Resolve entry would be stale; the LIVE pointer
// is what makes an intervening ask of ANY kind -- a modal election, a ward
// pay, a dig/scry/arrange pick -- carry the picks earlier TargetUnique$
// riders chose. It is optional so the effects test doubles stay small.
type resolutionCtxHost interface {
	SetResolutionCtx(*Ctx) *Ctx
}

// opponentPickHost is the optional Host seam for the TargetingPlayer$
// Opponent controller-selection at a mid-resolution ask site. The two
// mid-resolution ask sites (chosenTargetsFor's "tgts" ask and
// changeZoneChosenTargets' "choice" ask) call it before posing the target
// ask: with two or more living opponents and no answered selection it poses
// the CONTROLLER's which-opponent ask (posing a decision and suspending the
// walk) and reports posed=true, so the caller must return a handled-nil set
// and let the answer re-enter the walk. Every other shape reports posed=false
// with the seat that answers the target ask: the pinned or sole living
// opponent, or c.Controller when the resolver fails closed. A host that does
// not implement this interface (the effects test double) never poses the
// selection: the caller keeps plain ChooserFor, which for the test double is
// the controller (no resolver to consult).
type opponentPickHost interface {
	OpponentPickAsk(c *Ctx, sa *cards.SA) (state.PlayerID, bool)
}

// opponentPick calls the optional seam when the host implements it.
// ok=false means the caller keeps the plain chooser returned by
// Host.ChooserFor; ok=true with posed=false means ch is authoritative (the
// pinned or sole-opponent seat, or the controller fallback).
func opponentPick(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID) (state.PlayerID, bool) {
	if ph, ok := h.(opponentPickHost); ok {
		ch, posed := ph.OpponentPickAsk(c, sa)
		if posed {
			return 0, true
		}
		return ch, false
	}
	return chooser, false
}

// flipMemoryHost is implemented by the rules engine to publish the resolving
// chain's shared coin-flip memory (Ctx.FlipMemory) for the whole of the walk,
// so an ask posed from inside the chain (Host.Ask) can capture the pointer
// onto the pending resume point and re-attach it to the fresh Ctx a resume
// rebuilds. Optional, like effectFrameHost, so the effects test doubles need
// no method. effFlipCoin re-publishes whenever it lazily allocates the memory.
type flipMemoryHost interface {
	SetResolutionFlipMemory(*FlipMemory) *FlipMemory
}

// nameTableHost is implemented by the rules engine to publish its current
// layer-3 rename table (specs SetName$) as immutable data. Resolve reads it
// once at the top of every walk and binds it on the resolving Ctx, so every
// filter call a resolving effect makes through (*Ctx).SpecContext -- and
// every direct Ctx.EffectiveNames read -- agrees with rules' layer walk. It is
// optional, like effectFrameHost, so the effects test doubles stay small and
// a double with no rename table reads the printed face.
type nameTableHost interface {
	EffectiveNames() []ObjectName
}

// typeTableHost is implemented by the rules engine to publish its current
// layer-4 derived type table (CR 613.1d/613.1c) as immutable data, alongside
// the rename table. Resolve reads it once at the top of every walk and binds
// it on the resolving Ctx, so every filter call a resolving effect makes
// through (*Ctx).SpecContext -- and every direct Ctx.EffectiveTypes read --
// agrees with rules' layer walk instead of the printed face. Optional, like
// nameTableHost, so the effects test doubles stay small and a double with no
// derived types reads the printed face.
type typeTableHost interface {
	EffectiveTypes() []ObjectTypes
}

// castProhibitedHost is implemented by the rules engine to expose its
// CantBeCast gate (rules/statics.go castRestricted, CR 601.3) as a read-only
// query: effPlay's Play election filters its candidates against it, so a card
// the eventual beginPlay would refuse is never OFFERED (a refusal there only
// matters after the player has already picked it). Optional, like
// effectFrameHost, so the effects test doubles stay small; without it effPlay
// offers every surviving candidate exactly as before and the beginPlay
// legality recheck remains the sole gate -- the pre-existing behaviour.
type castProhibitedHost interface {
	CastProhibited(p state.PlayerID, id state.ObjID) bool
}

// goadTableHost publishes rules' live static-goad table for resolving filters.
type goadTableHost interface {
	StaticallyGoaded() map[state.ObjID]bool
}

func saMentionsGoaded(sa *cards.SA) bool {
	for _, v := range sa.Params {
		if strings.Contains(v, "IsGoaded") {
			return true
		}
	}
	return false
}

type targetableObjectsHost interface {
	TargetableObjects(triggerCard state.ObjID) []state.ObjID
}

func Resolve(h Host, c *Ctx, sa *cards.SA) {
	// Publish this walk's Effect-created registration frame (set by rules'
	// seedEffectReplCtx on an api:Effect replacement's body Ctx) for the whole
	// of the walk, restoring the enclosing value on exit so nested walks and
	// sub-ability chains keep the outer binding. Only a non-zero frame is
	// published: an inner body resolved with its own zero-valued Ctx (a
	// `` &cc `` copy that did not carry the frame) must inherit the enclosing
	// Effect rather than erase it, which is what a body reached through the
	// Effect's own chain needs.
	if fh, ok := h.(effectFrameHost); ok {
		previous := fh.GetCurrentEffectFrame()
		if c != nil && c.EffectFrame.Source != 0 {
			fh.SetCurrentEffectFrame(c.EffectFrame)
		}
		defer fh.SetCurrentEffectFrame(previous)
	}
	// Publish the chain's shared coin-flip memory for the whole walk (and
	// restore the enclosing value on return), so an ask inside the chain can
	// capture the pointer. nil when the chain has performed no flip yet; an
	// effect that allocates the memory (effFlipCoin) re-publishes through the
	// same seam.
	if c != nil {
		if fh, ok := h.(flipMemoryHost); ok {
			previous := fh.SetResolutionFlipMemory(c.FlipMemory)
			defer fh.SetResolutionFlipMemory(previous)
		}
	}
	if c != nil {
		c.Host = h
		// Publish the current layer-3 rename table for the whole of this walk
		// (and every sub-ability, which shares this Ctx). A FIELD READ of the
		// optional host method once per walk, never a call from the hot
		// specCtxSVars constructor: the table is immutable data, so binding it
		// here cannot make the resolving context read another game's board.
		if nh, ok := h.(nameTableHost); ok {
			c.EffectiveNames = nh.EffectiveNames()
		} else {
			// A Ctx may be reused with a different Host. Never let names
			// published by an earlier rules engine leak into this walk.
			c.EffectiveNames = nil
		}
		// The layer-4 derived type table, the type counterpart of the rename
		// table above: a resolving effect's ordinary filter reads it so a
		// target offer/Count$Valid agrees with the layer walk.
		if th, ok := h.(typeTableHost); ok {
			c.EffectiveTypes = th.EffectiveTypes()
		} else {
			c.EffectiveTypes = nil
		}
		if gh, ok := h.(goadTableHost); ok && sa != nil && saMentionsGoaded(sa) {
			c.StaticGoads = gh.StaticallyGoaded()
		} else {
			c.StaticGoads = nil
		}
		if th, ok := h.(targetableObjectsHost); ok {
			c.TargetableObjects = th.TargetableObjects(c.TriggerCard)
		} else {
			c.TargetableObjects = nil
		}
		c.numericRHS = c.X != 0 || len(c.SVars) > 0
		// Capture target controllers before the first effect can move a target.
		// Keep an existing map on re-entry: it is the earlier battlefield state,
		// not the current (possibly reset) object, that TokenOwner needs.
		if c.TargetControllerLKI == nil {
			c.TargetControllerLKI = make(map[state.ObjID]state.PlayerID)
			for _, target := range c.Targets {
				if target.IsPlayer {
					continue
				}
				if object := h.Game().Obj(target.Obj); object != nil {
					c.TargetControllerLKI[target.Obj] = object.Controller
				}
			}
		}
		// Capture target counters at the same instant, for the same reason: a
		// target that leaves the battlefield has its live counters cleared by
		// events.Apply's Move fold, so a chained condition gate or amount read
		// (Dismantle's `ConditionPresent$ Card.HasCounters` and
		// `X:Targeted$CardCounters.ALL`) must read the counters from here.
		if c.TargetCountersLKI == nil {
			c.TargetCountersLKI = make(map[state.ObjID][]state.Counter)
			for _, target := range c.Targets {
				if target.IsPlayer {
					continue
				}
				if object := h.Game().Obj(target.Obj); object != nil &&
					object.Zone == state.ZBattlefield && len(object.Counters) > 0 {
					c.TargetCountersLKI[target.Obj] = append([]state.Counter(nil), object.Counters...)
				}
			}
		}
		// Capture which object targets are spells on the stack at the same
		// instant: a Counter/ChangeZone later in the chain moves the spell off
		// the stack and no field records that it ever was one, so the
		// SpellTargeted count ref must read this resolution-start snapshot
		// (Reject Imperfection's proliferate gate, Gale's Redirection's roll
		// modifier, Press the Enemy's Z). Keep an existing map on re-entry:
		// a resumed chain's target has already left the stack, so a re-capture
		// would wrongly answer "never a spell". The refTargetUnion set is what
		// SpellTargeted itself enumerates, so both bindings are captured.
		if c.TargetSpellLKI == nil {
			c.TargetSpellLKI = make(map[state.ObjID]bool)
			captureTargetSpells := func(ts []state.Target) {
				for _, target := range ts {
					if target.IsPlayer || target.Obj == 0 {
						continue
					}
					if object := h.Game().Obj(target.Obj); object != nil && object.Zone == state.ZStack {
						c.TargetSpellLKI[target.Obj] = true
					}
				}
			}
			captureTargetSpells(c.Targets)
			if c.AllTargets != nil {
				captureTargetSpells(c.AllTargets)
			}
		}
		// Publish the snapshot to the host for the whole of this chain, so an
		// ask posed by any of its effects (or a nested Resolve that inherits
		// the same Ctx) carries it onto the resumePoint. Restored on return:
		// the map belongs to THIS chain, and an enclosing chain must not see
		// it after a nested one has finished.
		prev := h.SetResolutionTargetControllerLKI(c.TargetControllerLKI)
		defer h.SetResolutionTargetControllerLKI(prev)
		// Publish the live Ctx for the whole of this chain (the same
		// restore-on-return bracket), so any ask posed by any of its effects --
		// or by a nested Resolve that inherits the same Ctx -- carries the
		// chain's TargetUnique$ accumulator onto its resume state. The
		// accumulator is appended to in place during the walk, so the host
		// reads the CURRENT value at ask time, never a stale entry snapshot.
		if rh, ok := h.(resolutionCtxHost); ok {
			prevCtx := rh.SetResolutionCtx(c)
			defer rh.SetResolutionCtx(prevCtx)
		}
	}
	reg := registry.load()
	for d := 0; sa != nil && d < maxChain; d, sa = d+1, sa.Sub {
		// Earlier bodies in this chain may emit events that change the active
		// layer-3 name effects. Refresh at each body boundary, not only at
		// Resolve entry, so the next body's filters see the current snapshot.
		if nh, ok := h.(nameTableHost); ok {
			c.EffectiveNames = nh.EffectiveNames()
		}
		if th, ok := h.(typeTableHost); ok {
			c.EffectiveTypes = th.EffectiveTypes()
		}
		if gh, ok := h.(goadTableHost); ok && saMentionsGoaded(sa) {
			c.StaticGoads = gh.StaticallyGoaded()
		} else {
			c.StaticGoads = nil
		}
		if th, ok := h.(targetableObjectsHost); ok {
			c.TargetableObjects = th.TargetableObjects(c.TriggerCard)
		} else {
			c.TargetableObjects = nil
		}
		// Condition* gate (task fb-3f1cc033): a sub whose supported condition
		// is evaluated and not met is skipped and the chain continues — the
		// per-SA read the corpus's own gated pairs rely on (Gruesome
		// Discovery's morbid pair: the outer gated EQ0, the inner — its
		// SubAbility — gated bare-Morbid; the "instead" branch only runs
		// because the walk continues past a denial). A chain payload that
		// must not run after its gated parent is kept out by its own
		// population: the DigUntil's DB$ Play reads only what the chain
		// remembered (effPlay's trigger-capture exclusion), never the
		// triggering event's capture. An unresolved shape (supported=false)
		// runs unconditionally, the documented pre-gate behaviour — see
		// conditions.go for the exact boundary and the counts behind it. A
		// RepeatEach re-entered at its loop cursor already passed its gate
		// when the loop began; its remaining iterations are part of that
		// same resolution.
		resumingLoop := c.Repeat != nil && c.Repeat.SA == sa
		// A DB$ Token re-entered after its parked mint's answer (Ctx.TokenRest)
		// is the rest of the body that already passed its gate on the first
		// pass: the mints it made may have changed what the condition reads.
		resumingTokens := c.TokenRest != nil && c.TokenRest.SA == sa
		if !resumingLoop && !resumingTokens {
			if met, supported := conditionMet(h, c, sa); supported && !met {
				continue
			}
		}
		var fn Effect
		if code := sa.CompiledAPI(); code != cards.APIUnknown && int(code) < len(reg.byCode) {
			fn = reg.byCode[int(code)]
		}
		if fn == nil {
			fn = reg.byName[sa.API]
		}
		if fn == nil {
			// Unimplemented primitives must be loud but harmless: deck-build
			// validation is supposed to have caught this already.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented API " + sa.API})
			continue
		}
		// UnlessCost$ gate: every API with an UnlessCost$ pays (or declines)
		// before its body runs. This is the one shared unless-cost path —
		// the gate poses the pay decision, rules' resume arm charges the
		// cost, and the re-entry applies the orientation. UnlessResolveSubs$
		// (Forge's AbilityUtils.handleUnlessCost) then gates the SubAbility$
		// walk on the pay outcome: absent/'Always' resolves the subs either
		// way, WhenPaid only when the cost was paid, WhenNotPaid only when it
		// was not. A gate that skips BOTH the body and the subs ends this
		// SA's chain entirely — Forge returns from handleUnlessCost without
		// resolveSubAbilities, so the enclosing chain stops here too.
		runBody, paid := true, false
		// Suspended() is widened by the cumulative-upkeep/triggered-cost
		// payment windows (rules' Suspended() counts e.cumulative and
		// e.triggerCost): a mana ability resolving INSIDE one of those windows
		// must still dispatch, so only a suspension the gate itself caused —
		// the ask poseUnlessAsk posed — stops the loop here. Compare against
		// the pre-gate state instead of the raw predicate.
		wasSuspended := h.Suspended()
		asksBefore := askCount(h)
		if strings.TrimSpace(sa.Params["UnlessCost"]) != "" {
			runBody, paid = unlessProceed(h, c, sa)
		}
		// askCount catches the gate ask the host DEFERRED behind an
		// already-suspended resolution (rules' Engine.Ask: a second shock
		// land's pay-2-life ask while the first one's is still pending),
		// which leaves Suspended() unchanged.
		if (!wasSuspended && h.Suspended()) || askCount(h) != asksBefore {
			// The gate posed the unless-pay ask and suspended the
			// resolution: stop here exactly as an asking effect body
			// would. The resume re-enters THIS SA (the ask's ResumeSA),
			// where the gate consumes the answer and the loop walks
			// sa.Sub — so this loop's own continuation is dropped, like
			// any asking loop's (SuspendContinuation's innermost rule).
			h.SuspendContinuation(sa)
			return
		}
		if !runBody {
			// The body is skipped (paid on an unswitched shape, or every
			// payer declined on a switched one). The Sub chain walks only
			// when UnlessResolveSubs$ says so for this pay outcome.
			if !unlessSubsRun(sa, paid) {
				return
			}
			continue
		}
		// The generic ValidTgts$ pre-ask (task mvts1): an SA the placement/
		// announcement ask never covered -- a sub at depth >= 2 of a trigger's
		// Execute chain (the "when you do" family: Mogg Bombers' DealDamage,
		// Kor Outfitter's Attach, Rhino's second PutCounter) -- poses its own
		// target ask here, before its body reads Defined's ValidTgts$
		// fallthrough. The SA the placement ask covered (Ctx.OfferedSA) is
		// skipped; an ANSWERED ask re-enters this same SA (the pending
		// frame's ResumeSA), so the consumption inside chosenTargetsFor runs
		// before any skip could suppress it. API$ ChangeZone is left to
		// effChangeZone's own mid-resolution ask (changeZoneChosenTargets),
		// which the closed ChangeZone slice owns.
		if ts, done := chosenTargetsFor(h, c, sa, d == 0); done {
			if ts == nil {
				// The ask was posed and suspended the resolution: stop here
				// exactly as an asking body would. The ask's ResumeSA is THIS
				// SA, so the pending frame re-enters it (the innermost rule),
				// the "tgts" arm fills Ctx.TargetsPick, and the re-entered
				// pass consumes the answer and dispatches with it visible to
				// Defined for this SA.
				h.SuspendContinuation(sa)
				// The same asking-body-under-UnlessCost$ class as the body
				// path below: when the gate already resolved on THIS pass,
				// record its outcome on the ask's own resume point so the
				// answered re-entry consumes it instead of re-posing the pay
				// ask.
				if strings.TrimSpace(sa.Params["UnlessCost"]) != "" {
					h.SuspendUnless(sa, paid)
				}
				return
			}
			c.PickedTargets = ts
			fn(h, c, sa)
			c.PickedTargets = nil
		} else {
			fn(h, c, sa)
		}
		// A DB$ Token whose mint parked has not finished: its Imprint/
		// ClearImprinted tail belongs after the mints, so it runs on the
		// TokenRest re-entry that completes them (which may itself park again
		// and defer once more). Every other body keeps the tail here.
		tokensSuspended := c.tokensSuspended
		c.tokensSuspended = false
		if !tokensSuspended {
			imprint(h, c, sa)
		}
		if !tokensSuspended && strings.EqualFold(sa.Params["ClearImprinted"], "True") && c.Source != 0 {
			// Only a real clear is an event (the ClearRemembered$
			// discipline effCleanup documents): clearing lists that are
			// already empty is a no-op, and logging it as a state change hid
			// a no-progress Repeat from effRepeat's guard (Rally the Horde
			// over an empty library, cardfuzz batch8 line 4).
			if o := h.Game().Obj(c.Source); o != nil &&
				(len(o.Imprinted) > 0 || len(o.ImprintTokens) > 0 || len(o.SeekFound) > 0) {
				h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, Text: "clear"})
			}
		}
		if h.Suspended() {
			// A sub-ability in this chain posed a mid-resolution ask and
			// suspended the resolution: do NOT descend into the rest of the
			// chain. The B1 bug was that this loop kept walking sa.Sub
			// unconditionally, so a chained SA ran its SubAbility$ on the
			// initial pass (before the answer existed) AND again when the
			// answered decision re-entered at the asking SA — Thoughtseize's
			// Discard | SubAbility$ DBLoseLife lost 4 life instead of 2. The
			// resume re-enters at THIS asking SA (rules' resumeResolution),
			// which re-runs the asking effect to apply the answer and then
			// continues walking sa.Sub exactly once.
			// Report this loop's suspension point to the host so a NESTED ask
			// (an ask posed from inside this loop's own effect, e.g. the mode
			// a Charm runs) does not lose the chain this loop was still
			// carrying — fx32's defect. The host keeps the enclosing levels as
			// outer continuations and drops this one when it is the asking
			// loop's own level, which re-enters sa.Sub itself.
			h.SuspendContinuation(sa)
			// The gate had already resolved when the body asked: record the
			// outcome so the answer's re-entry pass consumes it instead of
			// re-posing the pay ask (the asking-body-under-UnlessCost$
			// livelock — Rhystic Study's pay-or-draw was the live carrier).
			if strings.TrimSpace(sa.Params["UnlessCost"]) != "" {
				h.SuspendUnless(sa, paid)
			}
			return
		}
		// UnlessResolveSubs$ also gates the sub walk when the body RAN: Forge
		// resolves the subs iff (paid && WhenPaid-or-default) or
		// (!paid && WhenNotPaid-or-default), independent of the orientation —
		// a paid unswitched body both runs AND suppresses a WhenNotPaid chain.
		if strings.TrimSpace(sa.Params["UnlessCost"]) != "" && !unlessSubsRun(sa, paid) {
			return
		}
	}
}
