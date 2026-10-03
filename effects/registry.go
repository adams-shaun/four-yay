// Package effects implements the primitives Forge card scripts reference. It
// reaches the engine through the Host interface, so it never imports rules and
// the dependency graph stays acyclic.
package effects

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
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
// doing rules work that belongs in the rules package. It is the union of the
// role interfaces in host_roles.go and declares no method of its own: a new
// method joins the one role it belongs to.
type Host interface {
	HostRead
	HostEmit
	HostBatch
	HostRNG
	HostContinuous
	HostRestrictions
	HostReplacements
	HostTargeting
	HostTriggers
	HostCastLedger
	HostTurnLedger
	HostConditions
	HostAsk
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
	// NoCallSide is non-zero when the suspension happened in a NoCall$
	// True line's DEFERRED outcome calls, after every flip was made:
	// NoCallWin while the heads branch was being called, NoCallLose for the
	// tails branch. NoCallNext is the next call index on that side, and
	// Wins/Losses the flip tallies the deferred calls are counted from. The
	// resumed primitive flips nothing and continues those calls (then the
	// tails side, when the heads side was in progress).
	NoCallSide int8
	NoCallNext int32
	Wins       int32
	Losses     int32
}

// NoCallSide values of FlipRest.
const (
	NoCallWin  int8 = 1
	NoCallLose int8 = 2
)

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

// RepeatContinuation is the scoped continuation of an api:Repeat loop that
// suspended: a RepeatOptional$ election's answer, or ANY Repeat whose body
// posed a mid-resolution ask (CR 608.2c: the remaining iterations still run
// once the answer is applied). It is carried only by the resolving Ctx;
// rules transports it across a mid-resolution ask and it is never event
// state. effRepeat consumes it (the first reader), so a later Repeat on the
// same Ctx starts fresh.
//
// It represents DISTINCT resume states, never conflated (fx42):
//   - Continue false: the player answered a RepeatOptional$ "no" and the
//     loop stops.
//   - Continue true, AfterBody false: a completed RepeatOptional$ election
//     was answered "yes", so the next body to run is iteration Next -- no
//     further election is owed for it.
//   - Continue true, AfterBody true: the body of iteration Next-1 completed
//     after its own suspension (a body ask), so the between-iteration step
//     owed before iteration Next has NOT run yet: the RepeatCheckSVar$/
//     RepeatDefined$ gate, then -- for RepeatOptional$ only -- the do/while
//     election. A non-optional Repeat whose gate holds (or that has none)
//     runs iteration Next's body directly.
//
// Count is the iteration bound the suspended pass resolved (MaxRepeat$/
// RepeatNum$, or the 1000 cap), so the resumed loop keeps the count Forge
// computes once before the first iteration rather than re-reading a value
// the body itself may have changed. Zero means "not recorded" (an election
// answer): the bound is re-evaluated.
type RepeatContinuation struct {
	Continue  bool
	Next      int32
	AfterBody bool
	Count     int32
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

	Source     state.ObjID
	Controller state.PlayerID
	// Grantor is the object that GRANTED the resolving activated ability to
	// Source (state.Object.GrantedBy): Forge's OriginalHost. Zero when the
	// ability is Source's own, in which case OriginalHost is Source.
	Grantor state.ObjID
	// CostUntapped carries permanents untapped as an activation cost into
	// effects whose Defined.Untapped selector refers to that paid target.
	CostUntapped []state.ObjID
	// AffectedObj is the object a static ability is being evaluated FOR --
	// Forge's "affected" card, whose AffectedX amount a static reads relative
	// to it. rules binds it on a cost-modifier static's Amount$ evaluation
	// (modAmountX): the spell or ability source being priced. Cemetery
	// Prowler's Count$TypesSharedWith reads the card types the priced spell
	// shares with the cards exiled with the Prowler. Zero means unbound; a
	// head that reads it falls back to Source (Forge's host card).
	AffectedObj state.ObjID
	// AffectedAbility is the activated ability being priced when AffectedObj
	// is an ability's source (nil for a spell): the in-flight activation a
	// Count$ThisTurnActivated_ gate counts alongside this turn's earlier ones.
	AffectedAbility *cards.SA
	// PromisedGiftOverride is bound only by rules' pre-election target-feasibility
	// census, which must consider either branch before the player elects Gift.
	PromisedGiftOverride *bool
	// NameChoice carries a mid-resolution NameCard answer across re-entry.
	NameChoice string

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
	// Layers is the board's derived-characteristic tables (LayerTables: the
	// layer-3 rename table, the layer-4 type table, the layer-5 colour and
	// layer-6 keyword tables, the static-goad set), published by the
	// resolving Host at the top of every effects.Resolve walk and at each
	// body boundary (layerTablesHost) and bound onto every SpecContext
	// (*Ctx).SpecContext builds, so a resolving effect's filters -- target
	// offer, Count$Valid census, CantTarget -- agree with rules' layer walk
	// instead of the printed face. Immutable DATA, never a pointer from
	// state.Game into rules; an effects test double whose Host does not
	// publish them leaves it zero and reads the printed face.
	Layers LayerTables
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
	// TargetPTLKI is the power/toughness half of the same CR 608.2h
	// look-back: each object target's LAYER-DERIVED power and toughness as it
	// last existed on the battlefield. Captured, overwritten and carried
	// exactly like TargetCountersLKI (rules refreshes it at the departure
	// boundary; the resolution-start capture below is the fallback). Condemn's
	// "its controller gains life equal to its toughness" and Swords to
	// Plowshares' "equal to its power" read it once the target is in the
	// library or exile, where the live object answers only the printed face.
	TargetPTLKI map[state.ObjID]TargetPT
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
	// ExchangeLife publishes numeric riders to Count$RememberedNumber for
	// the remainder of the resolution (not the source's remembered objects).
	// It is a POINTER (see ExchangeMemory) so a Ctx copy -- a RepeatEach
	// iteration's cc := *c, or the fresh Ctx a resume rebuilds -- shares the
	// SAME memory: a value written during one pass (the exchange transaction's
	// settle) stays visible to the chained SubAbility$ reader across a
	// suspension and its Ctx rebuild.
	ExchangeMemory *ExchangeMemory
	// ForgetOtherSnapshot retains the pre-clear IsRemembered candidates across
	// a multi-owner ChangeZone pick/search and its mid-resolution asks. It is
	// resolution-local; only the actual remembered set is event-backed.
	ForgetOtherSnapshot []state.Target
	ForgetOtherOwners   []state.PlayerID
	ForgetOtherReady    bool
	ForgetOtherCleared  bool

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
	// PublishedSVars records, in publication order, the resolution-scoped
	// SVar bindings an effect published into SVars for a chained
	// SubAbility$ to read (DealDamage's ExcessSVar$). It rides a suspended
	// resolution's resume point, so the rebuilt Ctx re-binds them over the
	// face table (RebindPublishedSVars) instead of losing them across an ask.
	PublishedSVars []SVarBinding
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

	DamageSplitDone bool
	// OfferedSA is the SA whose ValidTgts$ targeting the placement or
	// announcement ask actually covered (rules' resolveTop and
	// resumeResolution both set it; chosenTargetsFor skips exactly that SA,
	// matched by SA.Line -- ResolveSVar parses fresh on every call, so
	// pointer identity does not hold between two derivations of the same
	// body, the matching convention rules' charmModeTarget already
	// established).
	OfferedSA *cards.SA
	// TargetAskResume overrides the SA that owns a target ask while an Effect
	// pre-captures the target of its immediately following ChangeZone sub.
	TargetAskResume *cards.SA

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
	// CR 601.2c reprice, the CollectEvidence amount resolution) and, at
	// resolution, by rules for a stack object whose chain-link targets were
	// announced up front (a pre-asked cast chain, a CR 603.3d placement-
	// announced trigger chain: rules' chainTargetUnion). Ctx.Targets stays
	// the resolving SA's OWN targets so ParentTarget keeps its meaning; the
	// AllTargeted count ref and the Defined$ Targeted referent read this when
	// it is non-nil and fall back to Ctx.Targets otherwise (a chain with no
	// sub targets unions to exactly the root's own set).
	AllTargets []state.Target
	// parentLinks is the walk's record of the targets each TARGETING
	// SubAbility$ link chose, in chain order (effects/parent_targets.go): the
	// binding Forge's ParentTarget/ParentTargeted referents name -- the
	// NEAREST targeting parent's targets, not the root's. Resolution scratch,
	// scoped to one Resolve walk; never event-encoded.
	parentLinks [][]state.Target
	// linkAnswer carries a link body's own consumed target answer out to
	// Resolve's recorder when the body asked for it itself (effChangeZone's
	// changeZoneChosenTargets), which the generic pre-ask never sees.
	linkAnswer   []state.Target
	linkAnswered bool
	// ChoiceTarget is the index of the per-player chooser currently being
	// resumed. It keeps multi-player ChooseCard/ChoosePlayer asks from
	// returning to the first chooser after every answer.
	ChoiceTarget int
	// Chosen holds card/player choices for the remaining resolution chain.
	// Unlike Choice it is not the transport for a pending answer; filters such
	// as Creature.nonChosenCard consult it after ChooseCard has returned.
	Chosen      []state.Target
	ChosenValid bool

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

	// CloneETB carries the cast/replacement ETB copy election into DB$ Clone.
	// The answer is event-backed on the entering object, so replacement-time
	// resolution and log-only replay use the same selected permanent.
	CloneETB         bool
	CloneChoice      state.ObjID
	CloneChoiceValid bool
	CloneBecome      state.ObjID
	CloneBecomeValid bool

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

	ManifestDreadPlayer state.PlayerID

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

	// ETBEvenOddRecorded marks the ChooseEvenOdd body of an ETB replacement:
	// the entry boundary already asked and recorded the answer on the entering
	// permanent, so this invocation must not ask a second time.
	ETBEvenOddRecorded bool

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
	ExploreObj state.ObjID

	// LastRoll/LastRollName carry the result of a DB$ RollDice this same
	// resolution just made (effects/dice.go), under the SVar name its
	// ResultSVar$ parameter named (usually "Result" or "X"). evalCountExpr's
	// SVar$ head resolves a body of the form "SVar$<name>" against them, so
	// a chained sub's own SVar body (Velukan Dragon's
	// "SVar:X:SVar$Result/Minus.1") and a ConditionCheckSVar$ can read the
	// roll. Zero/"" on any resolution that did not roll, and the values are
	// never persisted beyond the resolution; a mid-resolution ask carries them
	// across its suspension (effects.RollRide on the resume point). RollPubs is
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
	// Arcane Endeavor's Instant.cmcLEY) reads the roll too. Carried across a
	// suspension with LastRoll (effects.RollRide); the chosen/other
	// publications are rebuilt from the answered decision on the roll resume
	// (Ctx.RollResults/RollPick).
	RollPubs []RollPub

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

// ExchangeMemory is a resolution's ExchangeLife numeric rider (the
// RememberOwnLoss$/RememberDifference$ publication Count$RememberedNumber
// reads). It is held by POINTER on the resolving Ctx for the same reason
// FlipMemory is: the value is written during one pass (the exchange
// transaction's settle, which can run after a side already suspended) but
// read by a chained SubAbility$ of the SAME resolution, which runs after any
// suspension on a freshly rebuilt Ctx. A nil *ExchangeMemory means this
// resolution has performed no exchange rider.
type ExchangeMemory struct {
	// Number is the published rider value: for RememberOwnLoss$ the life the
	// controller actually lost (written by the exchange transaction's settle,
	// rules' finishLifeExchange), for RememberDifference$ the absolute
	// difference of the two exchanged totals (written synchronously by
	// effExchangeLife).
	Number int32
	// Bound is the presence gate: only a published rider is readable, so a
	// plain Count$RememberedNumber in a chain with no ExchangeLife still
	// falls through to the remembered-object count.
	Bound bool
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

// TargetPT is one TargetPTLKI entry: a departed target's last battlefield
// power and toughness.
type TargetPT struct{ Power, Toughness int32 }

// CloneTargetPTLKI returns an independent copy of a target P/T LKI map
// threaded across a suspension (rules' resumePoint), for the reason
// CloneTargetCountersLKI gives.
func CloneTargetPTLKI(m map[state.ObjID]TargetPT) map[state.ObjID]TargetPT {
	if m == nil {
		return nil
	}
	out := make(map[state.ObjID]TargetPT, len(m))
	for id, pt := range m {
		out[id] = pt
	}
	return out
}

// targetPTLKI returns a departed object target's last battlefield
// power/toughness, when this chain captured one and the object is no longer
// on the battlefield; a live permanent (or an uncaptured object) reads live.
func targetPTLKI(c *Ctx, o *state.Object) (TargetPT, bool) {
	if c == nil || c.TargetPTLKI == nil || o == nil || o.Zone == state.ZBattlefield {
		return TargetPT{}, false
	}
	pt, ok := c.TargetPTLKI[o.ID]
	return pt, ok
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

// exchangeMemoryHost is implemented by the rules engine to publish the
// resolving chain's shared ExchangeLife rider memory (Ctx.ExchangeMemory)
// for the whole of the walk, so an ask posed from inside the chain (Host.Ask)
// can capture the pointer onto the pending resume point and re-attach it to
// the fresh Ctx a resume rebuilds. Optional, like flipMemoryHost, so the
// effects test doubles need no method. effExchangeLife re-publishes when it
// lazily allocates the memory.
type exchangeMemoryHost interface {
	SetResolutionExchangeMemory(*ExchangeMemory) *ExchangeMemory
}

// LayerTableSet selects the on-demand tables a layerTablesHost publishes
// beyond the always-published layer-3 rename and layer-4 type tables: the
// static-goad set, the layer-5 colour table and the layer-6 keyword table are
// built on demand by rules, so they are asked for only by a body that can read
// them.
type LayerTableSet uint8

const (
	// LayerGoads asks for LayerTables.StaticGoads.
	LayerGoads LayerTableSet = 1 << iota
	// LayerColors asks for LayerTables.DerivedColors.
	LayerColors
	// LayerKeywords asks for LayerTables.DerivedKeywords.
	LayerKeywords
)

// layerTablesHost is implemented by the rules engine to publish the board's
// derived-characteristic tables (LayerTables) as immutable data: the layer-3
// rename and layer-4 type tables always, and the tables want selects. Resolve
// reads it at the top of every walk and at each body boundary
// (layerTablesFor) and binds the result on the resolving Ctx, so every
// filter call a resolving effect makes through (*Ctx).SpecContext -- and every
// direct Ctx.Layers read -- agrees with rules' layer walk. It is optional,
// like effectFrameHost, so the effects test doubles stay small and a double
// with no tables reads the printed face. It replaced five per-table optional
// interfaces (W1d), so a table added to LayerTables is published here and
// nowhere else.
type layerTablesHost interface {
	LayerTables(want LayerTableSet) LayerTables
}

// layerTablesFor is the tables Resolve binds for body sa: what h publishes,
// with the on-demand tables asked for only when sa can read them (the zero
// LayerTables when h publishes none).
func layerTablesFor(h Host, sa *cards.SA) LayerTables {
	lh, ok := h.(layerTablesHost)
	if !ok {
		return LayerTables{}
	}
	var want LayerTableSet
	if sa != nil {
		if saMentionsGoaded(sa) {
			want |= LayerGoads
		}
		if saMentionsColors(sa) {
			want |= LayerColors
		}
		if saMentionsKeywords(sa) {
			want |= LayerKeywords
		}
	}
	return lh.LayerTables(want)
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

// saMentionsKeywords reports whether any parameter of sa, or of a
// sub-ability chained under it (the walk shares one Ctx), can name a
// with<Keyword>/without<Keyword>/hasKeyword<Keyword> predicate.
func saMentionsKeywords(sa *cards.SA) bool {
	for depth := 0; sa != nil && depth < 32; depth++ {
		for _, v := range sa.Params {
			if strings.Contains(v, "with") || strings.Contains(v, "hasKeyword") {
				return true
			}
		}
		sa = sa.Sub
	}
	return false
}

// saMentionsColors reports whether any parameter of sa can name a colour
// predicate (the capitalised fragments every colour word of the filter
// grammar contains).
func saMentionsColors(sa *cards.SA) bool {
	for _, v := range sa.Params {
		if strings.Contains(v, "Color") || strings.Contains(v, "Black") || strings.Contains(v, "White") ||
			strings.Contains(v, "Blue") || strings.Contains(v, "Red") || strings.Contains(v, "Green") {
			return true
		}
	}
	return false
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

func prefetchRememberedChangeZoneTarget(h Host, c *Ctx, sa *cards.SA) ([]state.Target, bool, bool) {
	if c == nil || sa == nil || sa.API != "Effect" ||
		TargetsOf(sa).Targeted() {
		return nil, false, false
	}
	remembersTargeted := false
	for _, member := range strings.Split(sa.ParamStr(cards.PKRememberObjects), "&") {
		member = strings.TrimSpace(member)
		if member == "Targeted" || member == "ThisTargetedCard" {
			remembersTargeted = true
			break
		}
	}
	if !remembersTargeted {
		return nil, false, false
	}
	childName := strings.TrimSpace(sa.ParamStr(cards.PKSubAbility))
	child := cards.ResolveSVar(c.SVars, childName)
	if child == nil || child.API != "ChangeZone" ||
		!TargetsOf(child).Targeted() {
		return nil, false, false
	}
	// The root Effect has no target of its own, so a generic placement marker
	// cannot mean that this child was offered. Temporarily remove that marker
	// while using ChangeZone's normal chooser and restore it before dispatch.
	offeredSA, targetsOffered := c.OfferedSA, c.TargetsOffered
	previousResume := c.TargetAskResume
	answeredEmpty := false
	c.OfferedSA, c.TargetsOffered, c.TargetAskResume = nil, false, sa
	ts, handled := changeZoneChosenTargets(h, c, child)
	c.OfferedSA, c.TargetsOffered, c.TargetAskResume = offeredSA, targetsOffered, previousResume
	if !handled {
		return nil, false, false
	}
	if ts == nil && !answeredEmpty {
		return nil, false, true
	}
	if ts == nil {
		ts = []state.Target{}
	}
	if c.SubPreAsk == nil {
		c.SubPreAsk = make(map[string][]state.Target)
	}
	c.SubPreAsk[child.Line] = ts
	return ts, true, false
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
	// Publish the chain's shared ExchangeLife rider memory for the whole walk
	// (and restore the enclosing value on return), so an ask inside the chain
	// can capture the pointer. nil when the chain has performed no exchange
	// rider yet; effExchangeLife re-publishes through the same seam when it
	// lazily allocates the memory.
	if c != nil {
		if emh, ok := h.(exchangeMemoryHost); ok {
			previous := emh.SetResolutionExchangeMemory(c.ExchangeMemory)
			defer emh.SetResolutionExchangeMemory(previous)
		}
	}
	if c != nil {
		c.Host = h
		// Publish the board's derived-characteristic tables for the whole of
		// this walk (and every sub-ability, which shares this Ctx): ONE
		// optional host read per walk, never a call from the hot specCtxSVars
		// constructor. The tables are immutable data, so binding them here
		// cannot make the resolving context read another game's board, and a
		// Ctx reused with a different Host (or a double that publishes none)
		// is reset to the printed-face read.
		c.Layers = layerTablesFor(h, sa)
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
		// The P/T half of the same entry capture (the fallback for a
		// departure the host did not see; rules refreshes it at the move).
		if c.TargetPTLKI == nil {
			for _, target := range c.Targets {
				if target.IsPlayer {
					continue
				}
				if object := h.Game().Obj(target.Obj); object != nil && object.Zone == state.ZBattlefield &&
					object.Face() != nil {
					if c.TargetPTLKI == nil {
						c.TargetPTLKI = make(map[state.ObjID]TargetPT)
					}
					c.TargetPTLKI[target.Obj] = TargetPT{Power: h.Power(target.Obj), Toughness: h.Toughness(target.Obj)}
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
	if c != nil {
		// The parent-link record belongs to THIS walk: a nested walk sharing
		// the Ctx (a body resolving its own sub-chain) must not leave its links
		// behind as "parents" of the enclosing chain's later links.
		defer func(n int) { c.parentLinks = c.parentLinks[:n] }(len(c.parentLinks))
	}
	reg := registry.load()
	for d := 0; sa != nil && d < maxChain; d, sa = d+1, sa.Sub {
		// Earlier bodies in this chain may emit events that change the
		// derived characteristics. Refresh at each body boundary, not only at
		// Resolve entry, so the next body's filters see the current snapshot.
		c.Layers = layerTablesFor(h, sa)
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
		resumingLoop := false
		// A DB$ Token re-entered after its parked mint's answer (Ctx.TokenRest)
		// is the rest of the body that already passed its gate on the first
		// pass: the mints it made may have changed what the condition reads.
		resumingTokens := false
		gatePassed := (*cards.SA)(nil) ==
			sa

		if !resumingLoop && !resumingTokens && !gatePassed {
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
		unlessServed := false
		if ActivationOf(sa).Unless() {
			runBody, paid, unlessServed = unlessProceed(h, c, sa)
		}
		// askCount catches the gate ask the host DEFERRED behind an
		// already-suspended resolution (rules' Engine.Ask: a second shock
		// land's pay-2-life ask while the first one's is still pending),
		// which leaves Suspended() unchanged. A tape-served election counts
		// as an ask taken but suspended nothing.
		if !unlessServed && ((!wasSuspended && h.Suspended()) || askCount(h) != asksBefore) {
			// The gate posed the unless-pay ask and suspended the
			// resolution: stop here exactly as an asking effect body
			// would. The resume re-enters THIS SA (the ask's ResumeSA),
			// where the gate consumes the answer and the loop walks
			// sa.Sub — so this loop's own continuation is dropped, like
			// any asking loop's (SuspendContinuation's innermost rule).
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
		// An Effect that remembers Targeted may own a replacement whose
		// referent is chosen by its immediately following ChangeZone sub.
		// Capture that sub's answer before registering the replacement, then
		// retain it for the sub so its ordinary ChangeZone path does not ask
		// twice. Other Effect shapes keep their established ask timing.
		rememberedSubTargets, prefetchedRememberedSub, suspendedForRememberedSub :=
			prefetchRememberedChangeZoneTarget(h, c, sa)
		if suspendedForRememberedSub {
			return
		}
		if ts, done := chosenTargetsFor(h, c, sa, d == 0); done {
			if ts == nil {
				// The ask was posed and suspended the resolution: stop here
				// exactly as an asking body would. The ask's ResumeSA is THIS
				// SA, so the pending frame re-enters it (the innermost rule),
				// the "tgts" arm fills Ctx.TargetsPick, and the re-entered
				// pass consumes the answer and dispatches with it visible to
				// Defined for this SA.
				// The same asking-body-under-UnlessCost$ class as the body
				// path below: when the gate already resolved on THIS pass,
				// record its outcome on the ask's own resume point so the
				// answered re-entry consumes it instead of re-posing the pay
				// ask.
				if ActivationOf(sa).Unless() {
				}
				return
			}
			c.PickedTargets = ts
			// Forge's sub-ability inheritance: a chain body that names no
			// targets of its own shares the chain's target list, so a later
			// body's "Targeted"/TargetedController referents (The Motherlode,
			// Excavator's Destroy sub feeding its RememberObjects$
			// TargetedController DBEffect) read the answer. Seed Ctx.Targets
			// ONLY when the chain carries none AND the NEXT member names no
			// targets of its own: an SA with its own ValidTgts$ never reads the
			// inherited list (it asks or consumes its own answer), and a seed
			// beside such a member would leak into TargetsAlreadyChosen's
			// TargetUnique$ exclusion set -- a fresh resume Ctx carries an empty
			// Ctx.Targets (the accumulator ride stamps TargetsUnique only), so
			// Rider Suspension's middle rider's own answer would enter the set
			// and its TargetUnique$ successor would be offered nobody (the ask
			// silently skipped). The CLOBBER rule above keeps an outer root's
			// targets authoritative (the root's own list is never overwritten),
			// so this cannot repoint a sub's explicit Defined$ Targeted away
			// from what it meant.
			if next := sa.Sub; len(c.Targets) == 0 && next != nil &&
				!TargetsOf(next).Targeted() {
				c.Targets = append([]state.Target(nil), ts...)
			}
			fn(h, c, sa)
			c.PickedTargets = nil
			recordParentLink(c, sa, ts, true)
		} else {
			if prefetchedRememberedSub {
				c.PickedTargets = rememberedSubTargets
			}
			fn(h, c, sa)
			if prefetchedRememberedSub {
				c.PickedTargets = nil
			}
			recordParentLink(c, sa, nil, false)
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
		if !tokensSuspended && strings.EqualFold(sa.ParamStr(cards.PKClearImprinted), "True") && c.Source != 0 {
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
			// The gate had already resolved when the body asked: record the
			// outcome so the answer's re-entry pass consumes it instead of
			// re-posing the pay ask (the asking-body-under-UnlessCost$
			// livelock — Rhystic Study's pay-or-draw was the live carrier).
			if ActivationOf(sa).Unless() {
			}
			return
		}
		// UnlessResolveSubs$ also gates the sub walk when the body RAN: Forge
		// resolves the subs iff (paid && WhenPaid-or-default) or
		// (!paid && WhenNotPaid-or-default), independent of the orientation —
		// a paid unswitched body both runs AND suppresses a WhenNotPaid chain.
		if ActivationOf(sa).Unless() && !unlessSubsRun(sa, paid) {
			return
		}
	}
}
