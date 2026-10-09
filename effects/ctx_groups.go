package effects

import "github.com/adams-shaun/gorge/state"

// LKISnapshots is the last-known-information snapshots a resolution reads (target controllers, counters and P/T, the source's lifelink and controller, damage sources, zone-change records and the source's derived P/T).
type LKISnapshots struct {
	// TargetControllerLKI captures each object target's controller at the
	// start of resolution. A target may leave the battlefield before a
	// chained TokenOwner$ TargetedController is evaluated; events.Apply then
	// resets its live Controller to Owner, so the live object is no longer the
	// CR 608.2h last-known controller.
	TargetController map[state.ObjID]state.PlayerID
	// TargetCountersLKI captures each object target's counters for the CR
	// 608.2b/h look-back. The authoritative capture is at the DEPARTURE
	// boundary: rules' Engine.emit refreshes the entry -- overwriting this
	// resolution-start snapshot -- on the MoveZone that actually moves a
	// target off the battlefield, so a chain that changed a target's counters
	// earlier in the same resolution reads the counters as they were
	// immediately before the zone change (Dismantle's DBPutCounter is the
	// corpus shape). Keyed by target ObjID. The
	// resolution-start capture here is the fallback for a departure this
	// host did not see (a test host folding events without Engine.emit):
	// only battlefield objects carrying at least one counter are captured at
	// entry; an object already off the battlefield, or with no counters to
	// look back at, needs no entry.
	TargetCounters map[state.ObjID][]state.Counter
	// TargetPTLKI is the power/toughness half of the same CR 608.2h
	// look-back: each object target's LAYER-DERIVED power and toughness as it
	// last existed on the battlefield. Captured, overwritten and carried
	// exactly like TargetCountersLKI (rules refreshes it at the departure
	// boundary; the resolution-start capture below is the fallback). Condemn's
	// "its controller gains life equal to its toughness" and Swords to
	// Plowshares' "equal to its power" read it once the target is in the
	// library or exile, where the live object answers only the printed face.
	TargetPT map[state.ObjID]TargetPT
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
	// entry (an existing map is kept: after a move the target is no longer on
	// the stack, so a re-capture would wrongly lose it). Nil when the chain's
	// targets were never spells.
	TargetSpell map[state.ObjID]bool
	// TargetManaSpentLKI is the CR 608.2b/h look-back for the mana actually
	// spent to cast a targeted spell: each object target's cast-time spend
	// breakdown (Object.ManaSpent / ManaSnowSpent / the typed captures) as it
	// was on the stack. events.Apply's Move zeroes those captures when the
	// spell leaves the stack (CR 400.7), so a Counter earlier in a chain makes
	// the live read a legitimate-looking 0. EOE Unravel's chained
	// "if the amount of mana spent to cast that spell was less than its mana
	// value, you draw a card" reads Targeted$CastTotalManaSpent after exactly
	// that move. Captured at Resolve entry for every object target on the
	// stack (including a zero spend -- a convoke-only cast is a legitimate 0,
	// not an absent snapshot); an existing map is kept on re-entry, since the
	// target has by then already left the stack. Keyed by target ObjID.
	TargetManaSpent map[state.ObjID]castManaSpentTotals
	// SourceLifelinkLKI is the source permanent's derived lifelink state at
	// the last moment it existed on the battlefield. The validity bit is
	// separate because "it did not have lifelink" is authoritative LKI too.
	// Rules seeds this on independently resolving abilities; damage uses it
	// only after the source has departed, and continues to read the live
	// derived source while it remains a permanent.
	SourceLifelink      bool
	SourceLifelinkValid bool
	// SourceControllerLKI is the source permanent's controller immediately
	// before it left the battlefield. Move resets Controller to Owner, so an
	// independently resolving lifelink ability needs this companion snapshot
	// to credit its last controller rather than its owner.
	SourceController      state.PlayerID
	SourceControllerValid bool
	// DamageSourceLKI preserves lifelink and controller LKI by object id for
	// a distinct DamageSource$ object that left while this resolution waited.
	// Rules transports it with the stack object; DamageSource$ consults it only
	// after that named object is no longer a battlefield permanent.
	DamageSource map[state.ObjID]DamageSourceLKI
	// ChangeZoneLKI is the resolution's last-known-information table for
	// ChangeZoneRememberLKI$ moves: one entry per object the move captured,
	// holding the controller/owner it had at that instant. events.Apply's Move
	// resets a battlefield departure's controller to its owner (CR 400.7), so
	// the live object can no longer answer "the exiled creature's controller"
	// -- exactly Forge's reason for storing a Card LKI copy in Remembered
	// (ChangeZoneEffect's CardCopyService.getLKICopy). A RepeatEach body's
	// TokenOwner$ ImprintedController / Defined$ ImprintedController reads it
	// for the current iteration subject (Curse of the Swine's Boars).
	ChangeZone []state.LKIObject
	// LKIPower/LKIToughness are that snapshot's derived battlefield P/T,
	// captured before the move removes continuous effects. The validity bit
	// distinguishes a real zero from a non-battlefield/no-characteristic LKI.
	Power, Toughness int32
	PTValid          bool
}

// ReplacementInputs is the replacement-effect context a replacement's body resolves under (the replaced object, cards, player, the redirect target, source and amount).
type ReplacementInputs struct {
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
	// proposal; empty outside one, and an
	// empty batch resolves to nobody (fail closed). Context, never state.
	Cards []state.ObjID
	// ReplacedPlayer is the player a replaced DRAW event was about — the
	// draw-er (Breathstealer's Crypt draws/reveals/discards "that player",
	// Zur's Weirding's other players pay relative to them). Set only on a
	// Draw replacement's own context, like Replaced; zero outside one.
	Player state.Target
	// ReplacementTarget, ReplacementSource and
	// ReplacementAmount carry the corresponding roles of an in-flight damage
	// ReplacementAmount carry the corresponding roles of an in-flight damage
	// event. They are resolution context, never persisted state; rules seeds
	// them before resolving ReplaceWith$ so ReplacedTarget/ReplacedSource and
	// ReplaceCount$DamageAmount are available to every replacement body API.
	Target state.Target
	Source state.ObjID
	Amount int32
}

// CloneAsEnters is the as-enters Clone inputs rules records for a clone replacement.
type CloneAsEnters struct {
	// CloneETB carries the cast/replacement ETB copy election into DB$ Clone.
	// The answer is event-backed on the entering object, so replacement-time
	// resolution and log-only replay use the same selected permanent.
	ETB         bool
	Choice      state.ObjID
	ChoiceValid bool
	Become      state.ObjID
	BecomeValid bool
}

// ManaInputs is the mana a replacement or reflected-mana effect reads and a Mana colour choice records.
type ManaInputs struct {
	// ManaAmount and ManaType are the in-flight unit of mana a ProduceMana
	// replacement modifies. rules seeds them from a ManaAdd event and then
	// emits the transformed event, so ReplaceMana never writes game state
	// directly and replay records the final mana production normally.
	Amount int32
	Type   string
	// ManaChoice is the W/U/B/R/G answer to a choice-valued ReplaceMana
	// body (ReplaceType$ Any, ReplaceColor$ Chosen, ReplaceMana$ Any).
	// Rules parks the ManaAdd and supplies this on resume.
	Choice string
	// ManaChoices is the allocation chosen for Produced$ Combo with Amount$ >
	// 1. Each entry is one W/U/B/R/G unit; effMana consumes it with Amount 1
	// so a split such as U,R produces one of each rather than doubling both.
	Choices []string
}

// KickerInputs is the kicker and gift state a count reads.
type KickerInputs struct {
	// PromisedGiftOverride is bound only by rules' pre-election target-feasibility
	// census, which must consider either branch before the player elects Gift.
	PromisedGiftOverride *bool
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
	// PendingTeamwork is the cast's elected intention to pay its optional
	// Teamwork cost, available during CR 601.2c target announcement before
	// payCast stamps TeamworkPaid on the stack object (CR 702.194b-c).
	PendingTeamwork bool
}

// VoteInputs is a Vote's tally and the RollPub-style publication state its sub-abilities read.
type VoteInputs struct {
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
	Counts []VoteCount
	// VotePublished/VotePublishedSet are the per-iteration binding the
	// AmountFromVotes$ RepeatEach writes before resolving one loop body: the
	// vote count of the iteration's subject, resolved by the reserved name
	// "Votes" through runtimePublished -- the same seam Ctx.RollPubs serves
	// for DB$ RollDice, and the name Forge's setVoteAmount sets
	// (sa.setSVar("Votes", "Number$<n>")). Set only on an
	// AmountFromVotes$ loop's per-iteration Ctx copy, so an ordinary SVar
	// table is never shadowed outside one loop body.
	Published    int32
	PublishedSet bool
}

// RollInputs is the last die roll and the roll publications a roll's sub-abilities read.
type RollInputs struct {
	// LastRoll/LastRollName carry the result of a DB$ RollDice this same
	// resolution just made (effects/dice.go), under the SVar name its
	// ResultSVar$ parameter named (usually "Result" or "X"). evalCountExpr's
	// SVar$ head resolves a body of the form "SVar$<name>" against them, so
	// a chained sub's own SVar body (Velukan Dragon's
	// "SVar:X:SVar$Result/Minus.1") and a ConditionCheckSVar$ can read the
	// roll. Zero/"" on any resolution that did not roll, and the values are
	// never persisted beyond the resolution. RollPubs is
	// the general form of the same publication (both are read through
	// effects.dice.go's rollPublished, and this slot stays the primary
	// result's mirror for the existing readers).
	Last     int32
	LastName string
	// RollPub is one name→value publication a DB$ RollDice of this
	// resolution made, beyond the primary ResultSVar$ slot above:
	// ChosenSVar$/OtherSVar$ (the Endeavor cycle's choose-one-result), and
	// the MaxRollsResults$/EvenOddResults$ counts ("MaxRolls",
	// "EvenResults", "OddResults" -- Luck Bobblehead). Read by Name's
	// bare-name fallback and evalCountExpr's SVar$ head through
	// rollPublished, and by Ctx.SpecContext's numeric-RHS resolver, so a
	// chained sub's filter spec (Valiant Endeavor's Creature.powerGEX,
	// Arcane Endeavor's Instant.cmcLEY) reads the roll too.
	Pubs []RollPub
}

// ForgetOtherInputs is the ForgetOtherRemembered$ pre-clear snapshot a choose walk keeps across its choosers.
type ForgetOtherInputs struct {
	// ForgetOtherSnapshot retains the pre-clear IsRemembered candidates across
	// a multi-owner ChangeZone pick/search and its mid-resolution asks. It is
	// resolution-local; only the actual remembered set is event-backed.
	Snapshot []state.Target
	Owners   []state.PlayerID
	Ready    bool
	Cleared  bool
}

// ChooserCursors is the victim and chooser lists a VillainousChoice or GenericChoice walks.
type ChooserCursors struct {
	// VillainousVictims is the ordered Defined$ player set for a
	// VillainousChoice. The index advances only after the current victim's
	// chosen body has completed.
	Victims     []state.Target
	VictimIndex int
	// GenericChoosers is the ordered Defined$ player set for a multi-player
	// api:GenericChoice resolution (each opponent chooses one of the same
	// Choices$), and GenericChooserIndex is the index of the chooser being
	// asked. The index advances only after the current chooser's chosen body
	// has completed, so the remaining choosers are asked once the body's own
	// nested ask (if any) finishes. Nil outside the per-player path, which
	// keeps the single-controller Charm/GenericChoice ask unchanged.
	Choosers     []state.Target
	ChooserIndex int
}

// SearchInputs is a library search's per-target index and known set.
type SearchInputs struct {
	// LibraryTarget is the index in the deterministic per-library target list
	// currently being walked. Search, KArrange and their follow-up confirms
	// share this cursor (the asks' ResumeTarget).
	Target int
	// SearchKnown names, per choosing player, the library cards whose identity
	// that player has legitimately learned during this resolution's search
	// chain (effects/zone.go applyLibrarySearch): a card the head ask publicly
	// revealed is known to every seat, and a card the head ask offered BY NAME
	// is known to the player who picked it. A Cultivate-family placement leg
	// (NoLooking$ True, ChangeType$ ...IsRemembered) must label its options
	// with those real names -- the blind "a card" label would hide information
	// the chooser already holds -- while an option the chooser genuinely never
	// saw stays fail-closed blind.
	// Resolution-scratch like Remembered -- never event-encoded; a replay
	// re-derives the same set by replaying the same resolution.
	Known []state.Target
}

// NumberInputs is the chosen and remembered numbers a Count$ reads.
type NumberInputs struct {
	// ChosenNumber is the Effect's SetChosenNumber$ binding (task
	// wildgrowth1): the number the Effect resolved at creation, threaded into
	// a registered replacement's body Ctx by rules' replCtx so the body's
	// Count$ChosenNumber head (evalCountBody) reads the frozen binding rather
	// than re-deriving. Zero wherever nothing bound -- the same number a
	// failed binding degrades to.
	Chosen int32
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
	ChosenBound bool
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
	// OptionalCostElected is the pending cast's CR 601.2b election of its
	// optional additional cost, seeded by rules' costAmountCtx (the ONE
	// cost-amount context) when the composition prices the optional-cost
	// cast variant. The Count$OptionalGenericCostPaid head reads it ahead of
	// state.Object.OptionalCostPaid because at OFFER time the card sits in
	// hand and the pay-time flag is not yet folded onto the stack object
	// (events.Apply's CastInfo carries it only after CR 601.2a's push) --
	// without the seed every such reduction evaluated its unpaid branch and
	// the variant was only ever offered at the unreduced price (Bite Down on
	// Crime's "{2} less to cast if evidence was collected"). Seeding
	// resolution-scratch like the struct's other fields, never
	// event-encoded: the election rides the cast option's Mode, which IS in
	// the log, so a replay re-derives it. Only the optional-cost variant
	// seeds it; the plain cast, every other cast mode and activated abilities
	// keep the unpaid object-flag read.
	OptionalCostElected bool
	// RememberedCMCBound marks a Ctx whose RememberedCMC IS a real
	// RememberCounteredCMC$ binding. It is the Count$RememberedNumber head's
	// verdict, the same shape ChosenNumberBound gives Count$ChosenNumber:
	// bound means evaluated (a zero mana value reads as zero), unbound means
	// the head falls through to the list-length read every pre-existing
	// consumer keeps.
	RememberedCMCBound bool
}

// DrawInputs is an Upto$ Draw's per-target answer and the cards drawn so far.
type DrawInputs struct {
	// DrawDone is the number of individual draws a multi-card Draw has already
	// completed, so a dredge choice between draws lets the enclosing Draw
	// continue rather than restarting or abandoning its remaining cards.
	Done int32
	// DrawUptoIdx/DrawUptoCount/DrawUptoAnswered carry an Upto$ Draw's
	// per-target continuation (Arcane Denial, Truce): Idx is the Defined$
	// target index whose "draw up to N" ask or answered batch is in flight,
	// Count the answered count for it, Answered distinguishes an answered
	// ZERO (draw nothing) from a target not yet asked. effDraw sets all
	// three from the answer (Count = the number of chosen card options) and
	// resets them as it completes each target, so the next target poses its
	// own ask.
	UptoIdx      int32
	UptoCount    int32
	UptoAnswered bool
}

// ETBRecords is which as-enters answers rules has already recorded on the entering object.
type ETBRecords struct {
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
	// named. Consumed and cleared by the effect, so a nested ChooseColor
	// deeper in the same chain poses its own fresh ask.
	ColorRecorded bool
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
	// by the effect, so a nested ChooseNumber deeper in the same chain poses
	// its own fresh ask.
	NumberRecorded bool
	// ETBEvenOddRecorded marks the ChooseEvenOdd body of an ETB replacement:
	// the entry boundary already asked and recorded the answer on the entering
	// permanent, so this invocation must not ask a second time.
	EvenOddRecorded bool
}
