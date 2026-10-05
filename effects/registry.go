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
	// NameChoice is the card name a mid-resolution NameCard chose, read by
	// the rest of the chain.
	NameChoice string

	// ChosenDirection is a mid-resolution ChooseDirection answer: the
	// "left"/"right" pick (Aminatou's [-6], Order of Succession). It is
	// resolution-scratch like NameChoice -- never event-encoded -- and left
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
	Remembered     []state.Target
	// ExchangeLife publishes numeric riders to Count$RememberedNumber for
	// the remainder of the resolution (not the source's remembered objects).
	// It is a POINTER (see ExchangeMemory) so a Ctx copy -- a RepeatEach
	// iteration's cc := *c, or the fresh Ctx a resume rebuilds -- shares the
	// SAME memory: a value written during one pass (the exchange transaction's
	// settle) stays visible to the chained SubAbility$ reader across a
	// suspension and its Ctx rebuild.
	ExchangeMemory *ExchangeMemory

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
	// LKI is the object a zone-change trigger fired for, as it was just
	// before the move (CR 603.10 "look back in time"): Move resets counters,
	// tapped state and damage on the way out, so a "dies" condition such as
	// Undying's "if it had no +1/+1 counters" must read this, not the live
	// object. nil for every other trigger.
	LKI *state.Object
	// Modes is an announced modal choice (CR 601.2b, 603.3c): the SVar
	// names of the chosen Choices$ sub-abilities, in execution order, so
	// effCharm runs exactly the chosen modes instead of asking.
	Modes []string
	// UnlessPay is the settled unless-pay choice (rules/unless_tape.go):
	// "pay" means rules has already paid the UnlessCost$ and the asking
	// effect proceeds with its body; "decline" means it proceeds as if the
	// player declined. "" when nothing is settled, where the effect poses
	// the ask instead. For Sacrifice's damage-payment shape (Vexing Devil),
	// "pay" additionally means rules has already emitted the accepting
	// opponent's Damage event -- payment events belong to rules, never to
	// the effects layer.
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

	// Play is the answered card a resolved Play effect chose to play from a
	// zone (CR 701.23): the object the controller selected among the offered
	// candidates. rules' resumeResolution sets it from the recorded answer
	// before re-running the suspended effPlay, which then casts/plays it from
	// its own zone. PlayDone distinguishes "answered (possibly with no card)"
	// from the first pass.
	Play     state.ObjID
	PlayDone bool

	// UnlessNext is the index of the UnlessPayer$ payer whose settled
	// unless-pay choice applies (rules/unless_tape.go sets it from the
	// decision's ResumeTarget). The unlessProceed gate (Resolve) consumes and
	// clears it. A decline moves the gate on to payer idx+1, so a
	// multi-payer UnlessPayer$ asks each payer in turn.
	UnlessNext int
	// UnlessDiscarded is the object list the settled unless-payment
	// discarded (the UnlessCost$ Discard<...> component's picks), set by
	// rules/unless_tape.go when the payment completed, so the continuing
	// walk's ConditionDefined$
	// Discarded gates (Argentum Masticore's "When you discard a card this
	// way") see exactly the card(s) the payment discarded. It rides
	// Ctx.UnlessPay's lifetime rather than being cleared at first read: the
	// unless resolution's whole sub-chain may consult the group.
	UnlessDiscarded []state.Target

	// FlipMemory is this resolution's coin-flip memory (nil until a flip
	// happens). It is a POINTER so a Ctx copy -- a RepeatEach iteration's
	// cc := *c -- shares the SAME memory: flips performed in a loop iteration
	// stay visible to the chained reader. effFlipCoin lazily allocates it and
	// mutates it in place (never replacing the pointer). The cumulative-upkeep FlipCoin
	// cost action (rules/cumulative.go) does not go through effFlipCoin and so
	// does not populate it (see AGENTS.md).
	FlipMemory *FlipMemory

	// ClashWon records the resolving controller's CR 701.31 clash outcome:
	// true when their revealed card had the strictly higher mana value, false
	// on a loss and on a tie (no winner). effClash sets it from the reveal
	// comparison and selects its Forge WinSubAbility$/OtherwiseSubAbility$
	// branch through it, so the branch and the emitted events.Clash records
	// cannot disagree about who won. It is resolution-scratch like
	// Targets/SVars -- never event-encoded (the marker carries the same bit
	// in Amount), a replay re-derives the same value.
	ClashWon bool
	// Snap is the last-known-information snapshots a resolution reads (target controllers, counters and P/T, the source's lifelink and controller, damage sources, zone-change records and the source's derived P/T).
	Snap LKISnapshots
	// Repl is the replacement-effect context a replacement's body resolves under (the replaced object, cards, player, the redirect target, source and amount).
	Repl ReplacementInputs
	// CloneEnter is the as-enters Clone inputs rules records for a clone replacement.
	CloneEnter CloneAsEnters
	// Mana is the mana a replacement or reflected-mana effect reads and a Mana colour choice records.
	Mana ManaInputs
	// Kicker is the kicker and gift state a count reads.
	Kicker KickerInputs
	// Vote is a Vote's tally and the RollPub-style publication state its sub-abilities read.
	Vote VoteInputs
	// Roll is the last die roll and the roll publications a roll's sub-abilities read.
	Roll RollInputs
	// Forget is the ForgetOtherRemembered$ pre-clear snapshot a choose walk keeps across its choosers.
	Forget ForgetOtherInputs
	// Choosers is the victim and chooser lists a VillainousChoice or GenericChoice walks.
	Choosers ChooserCursors
	// Search is a library search's per-target index, shuffle answer, moved list and known set.
	Search SearchInputs
	// Num is the chosen and remembered numbers a Count$ reads.
	Num NumberInputs
	// Draw is an Upto$ Draw's per-target answer and the cards drawn so far.
	Draw DrawInputs
	// ETB is which as-enters answers rules has already recorded on the entering object.
	ETB ETBRecords
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
	if c == nil || c.Snap.TargetPT == nil || o == nil || o.Zone == state.ZBattlefield {
		return TargetPT{}, false
	}
	pt, ok := c.Snap.TargetPT[o.ID]
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
// the Resolve chain that is CURRENTLY running, read by rules' tape seams
// (unless payment, Ward, Play) and its departing-target snapshots. It is
// optional so the effects test doubles stay small.
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
	c.OfferedSA, c.TargetsOffered, c.TargetAskResume = nil, false, sa
	ts, handled := changeZoneChosenTargets(h, c, child)
	c.OfferedSA, c.TargetsOffered, c.TargetAskResume = offeredSA, targetsOffered, previousResume
	if !handled {
		return nil, false, false
	}
	if ts == nil {
		return nil, false, true
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
		if c.Snap.TargetController == nil {
			c.Snap.TargetController = make(map[state.ObjID]state.PlayerID)
			for _, target := range c.Targets {
				if target.IsPlayer {
					continue
				}
				if object := h.Game().Obj(target.Obj); object != nil {
					c.Snap.TargetController[target.Obj] = object.Controller
				}
			}
		}
		// Capture target counters at the same instant, for the same reason: a
		// target that leaves the battlefield has its live counters cleared by
		// events.Apply's Move fold, so a chained condition gate or amount read
		// (Dismantle's `ConditionPresent$ Card.HasCounters` and
		// `X:Targeted$CardCounters.ALL`) must read the counters from here.
		if c.Snap.TargetCounters == nil {
			c.Snap.TargetCounters = make(map[state.ObjID][]state.Counter)
			for _, target := range c.Targets {
				if target.IsPlayer {
					continue
				}
				if object := h.Game().Obj(target.Obj); object != nil &&
					object.Zone == state.ZBattlefield && len(object.Counters) > 0 {
					c.Snap.TargetCounters[target.Obj] = append([]state.Counter(nil), object.Counters...)
				}
			}
		}
		// The P/T half of the same entry capture (the fallback for a
		// departure the host did not see; rules refreshes it at the move).
		if c.Snap.TargetPT == nil {
			for _, target := range c.Targets {
				if target.IsPlayer {
					continue
				}
				if object := h.Game().Obj(target.Obj); object != nil && object.Zone == state.ZBattlefield &&
					object.Face() != nil {
					if c.Snap.TargetPT == nil {
						c.Snap.TargetPT = make(map[state.ObjID]TargetPT)
					}
					c.Snap.TargetPT[target.Obj] = TargetPT{Power: h.Power(target.Obj), Toughness: h.Toughness(target.Obj)}
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
		if c.Snap.TargetSpell == nil {
			c.Snap.TargetSpell = make(map[state.ObjID]bool)
			captureTargetSpells := func(ts []state.Target) {
				for _, target := range ts {
					if target.IsPlayer || target.Obj == 0 {
						continue
					}
					if object := h.Game().Obj(target.Obj); object != nil && object.Zone == state.ZStack {
						c.Snap.TargetSpell[target.Obj] = true
					}
				}
			}
			captureTargetSpells(c.Targets)
			if c.AllTargets != nil {
				captureTargetSpells(c.AllTargets)
			}
		}
		// Publish the live Ctx to the host for the whole of this chain
		// (restored on return), so rules' tape seams (unless payment, Ward,
		// Play) read the chain's current Ctx.
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
		// population. An UNMODELLED gate (UnmodelledCondition names a shape
		// this build does not evaluate) fails CLOSED: the sub is skipped and a
		// replay-visible Note records the gap, instead of running the rider
		// unconditionally and diverging from the oracle.
		if sa != nil {
			met, supported := conditionMet(h, c, sa)
			if supported && !met {
				continue
			}
			if !supported {
				if detail, bad := unmodelledConditionDetail(sa); bad {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
						Text: "unmodelled condition " + detail})
					continue
				}
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
		// the gate poses the pay decision, rules charges the cost, and the
		// gate applies the orientation. UnlessResolveSubs$
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
			// The gate opened the unless-payment window: stop here exactly
			// as an asking effect body would.
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
		// skipped. API$ ChangeZone is left to
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
				// No target answer: stop here exactly as an asking body
				// would.
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
			// TargetUnique$ exclusion set (Rider Suspension's middle rider's
			// own answer would enter the set and its TargetUnique$ successor
			// would be offered nobody). The CLOBBER rule above keeps an outer root's
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
			rememberChosenTargets(h, c, sa, ts, true)
		} else {
			if prefetchedRememberedSub {
				c.PickedTargets = rememberedSubTargets
			}
			fn(h, c, sa)
			if prefetchedRememberedSub {
				c.PickedTargets = nil
			}
			recordParentLink(c, sa, nil, false)
			rememberChosenTargets(h, c, sa, nil, false)
		}
		imprint(h, c, sa)
		if strings.EqualFold(sa.ParamStr(cards.PKClearImprinted), "True") && c.Source != 0 {
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
			// A sub-ability in this chain opened a resolution-time payment
			// window: do NOT descend into the rest of the chain.
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
