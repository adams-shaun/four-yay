// trigger_match.go is the read-only half of the split: checkTriggers walks
// every object once per event and triggerMatches (with its per-kind helpers
// below -- zoneGate, zoneChangeMatches, spellCastMatches, attacksMatches,
// damageMatches, becomesTargetMatches, landPlayedMatches, phaseMatches)
// decides whether a given T: line fires for it. init() registers those
// trigger kinds, and repl:Moved (replacement.go's own kind), as non-API so
// effects.Supported does not mistake them for something a card SVar could
// invoke directly.
//
// Triggered abilities (T: lines) and replacement effects (R: lines). Every
// state mutation still goes through events.Emit -- engine.go's emit wraps it
// with applyReplacements ahead of logging and checkTriggers behind it, so
// this file's job is entirely about *deciding* what fires and *ordering*
// what goes on the stack, never about writing state directly.
package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// pendingTrigger is a matched trigger waiting to be placed on the stack.
//
// Idx is Source's own Face().Triggers index for the matched T: line. SA is
// kept alongside it (the brief's own interface names both Source and SA)
// even though putTriggersOnStack ends up re-deriving the ability from
// Source+Idx rather than using SA directly: Ruling T20-a means the eventual
// stack object is created inside events.Apply, which cannot carry a raw
// *cards.SA pointer through the log -- only data (an ObjID and a small
// index) that lets Apply find the same *cards.SA a live engine already
// holds.
type pendingTrigger struct {
	Source     state.ObjID
	Controller state.PlayerID
	Idx        int
	SA         *cards.SA
	// Miracle marks a keyword offer (Task 18) rather than a matched T: line: a
	// Miracle drawing queued with Idx/SA unset, which the drain treats as an
	// optional trigger whose decider is the owner and routes a yes through
	// castMiracle (miracle.go) instead of minting a triggered-ability stack
	// object. optionalDecider, triggerLabel and pushTrigger all special-case it.
	// Madness marks the mandatory triggered ability created after its owner
	// accepts the optional discard-to-exile replacement (CR 702.35a-b). Unlike
	// Miracle it is pushed unconditionally and asks whether to cast only when
	// the respondable ability resolves.
	Miracle bool
	Madness bool
	// Evoke marks the CR 702.79a mandatory sacrifice follow-up queued by
	// altCostEnter. It has no yes/no choice; pushTrigger mints a real
	// respondable keyword-triggered ability on the stack.
	Evoke bool
	// Gift marks the CR 702.168c triggered ability created by a promised
	// permanent's entry. Its body is the source face's GiftAbility SVar.
	// GiftTo is the promised receiver, snapshotted HERE at queue time from
	// the entering object's GiftPromisedTo: the ability resolves
	// independently of its source (CR 112.7a), and events.Move clears the
	// live promise the moment the permanent leaves the battlefield, so the
	// receiver travels with the KeywordTriggerPush payload (IDs) onto the
	// resolving stack object instead of being re-read from it.
	Gift   bool
	GiftTo state.PlayerID
	// Delayed marks a Mode$ Phase delayed trigger registration (CR 603.7)
	// rather than a matched T: line. It is queued by checkDelayedTriggers when
	// the registered phase is entered, and pushTrigger routes it to a
	// DelayedPush event instead of a TriggerPush (whose Ability derives from a
	// face Triggers index -- a delayed trigger's effect is an SVar-named
	// sub-ability, not a face trigger). DelayedID is the registration's
	// state.DelayedTrigger.ID (so the fired event can remove exactly it), and
	// Execute is the Execute$ SVar name (which events.Apply's DelayedPush case
	// resolves from the source's SVar table).
	Delayed   bool
	DelayedID uint32
	// MonarchDraw is the CR 724.2a beginning-of-end-step triggered draw.
	// It is represented as a real stack ability through the existing delayed
	// push event, rather than as an immediate turn action.
	MonarchDraw    bool
	RadiationDrain bool
	// Merged marks a mutated pile's under-card trigger (CR 702.140d): like
	// a delayed trigger its Ability is the Execute$ SVar-named body, but the
	// push must resolve that name against the UNDER-CARD's own face, never
	// the pile's top face (whose same-named SVar -- Forge's canonical
	// TrigToken -- would otherwise steal the body). The value is the
	// under-card's pile index PLUS ONE, the same encoding triggerFace.merged
	// uses, so the zero value always means "not a merged trigger". It rides
	// the MergedTriggerPush event as Amount (minus one). It and Delayed are
	// never both set.
	Merged int
	// Granted marks a static-grant's trigger (AddTrigger$ on a Mode$
	// Continuous static, e.g. Hearthhull's "STATION 8+ Whenever you sacrifice
	// a land"): like a delayed trigger its Ability is an SVar-named body (the
	// Execute$ name rides the GrantTriggerPush event for events.Apply to
	// resolve from the GRANTOR's SVar table), but unlike a delayed
	// registration nothing is consumed -- the grant lives exactly as long as
	// its granting static. Grantor is the object carrying the printed static
	// (0 = the self-grant shape, where grantor == recipient); it rides the
	// event's Amount so Apply resolves the Execute$ body from the same table
	// this walk linked it from.
	Granted bool
	Grantor state.ObjID
	Execute string
	// Trigger is the granted/delayed trigger LINE this pending trigger came
	// from (its OptionalDecider$, Cost$, Condition$ and ResolvedLimit$ live
	// here, not on the Execute$ body). It is zero for every printed trigger,
	// whose line is recovered from Face.Triggers by pointer, and is carried
	// through pushTrigger into Engine.triggerLines keyed by the minted stack
	// object -- see that field. The Gained arm sets it from the foreign face's
	// compiled Trigger, the Granted arm from the AddTrigger$ static and the
	// Delayed arm from the registration's re-parsed body.
	Trigger cards.Trigger
	// TriggerSVars is the owning face's immutable script table captured with
	// the granted/delayed line. Source may be a different card, and the grant
	// can end before this ability resolves.
	TriggerSVars map[string]string
	// Gained marks a has-all-abilities-of trigger (Forge's
	// GainsTriggerAbsOf$ on a Mode$ Continuous static, the Idris, Soul of the
	// TARDIS shape): the ability is a compiled trigger on a FOREIGN card's
	// face, so unlike a Granted trigger there is no SVar body and no grantor
	// table -- the fired event names the foreign card (GainedFrom, carried as
	// the event's IDs[0]) and that face's own Triggers index (Idx), and
	// events.Apply mints the face's compiled Trigger.Effect pointer. Execute
	// still carries the trigger's Execute$ name as readable provenance for
	// Apply's tamper check. Gained and Granted are never both set.
	Gained     bool
	GainedFrom state.ObjID
	// Ward is a GRANTED ward keyword (a layer-6 AddKeyword$ Ward:<cost>, e.g.
	// Hexing Squelcher's "Other creatures you control have 'Ward—Pay 2
	// life.'"): the trigger exists only in the layer system, never on the
	// face, so the queue carries the ward COST text and the drain pushes a
	// KeywordTriggerPush whose __kwWard: payload events.Apply rebuilds the
	// same DB$ Ward ability from. Idx and SA are unset for it.
	Ward string
	// Melee marks a layer-6 granted instance without a printed trigger index.
	Melee bool
	// Afflict is a GRANTED afflict keyword (a layer-6 AddKeyword$
	// Afflict:<N>, e.g. Lost Monarch of Ifnir's "Other Zombies you control
	// have afflict 3"): the same shape as Ward -- the queue carries the life
	// amount and the drain pushes a KeywordTriggerPush whose __kwAfflict:
	// payload events.Apply rebuilds into the same DB$ LoseLife body the
	// printed K:Afflict expansion carries. Idx and SA are unset for it.
	Afflict string
	// Conspire is a GRANTED conspire keyword (a layer-6 AddKeyword$
	// Conspire -- Wort, the Raidmother's "each red or green instant or
	// sorcery spell you cast has conspire", Raiding Schemes' noncreature
	// arm): the same shape as Ward/Afflict -- the queue carries no
	// parameter (the copy trigger has none) and the drain pushes a
	// KeywordTriggerPush whose __kwConspire: payload events.Apply rebuilds
	// into the same DB$ CopySpellAbility body the printed K:Conspire
	// expansion carries, with the cast spell riding IDs as Remembered.
	// Idx and SA are unset for it.
	Conspire bool
	// Casualty is queued only for a paid sacrifice at the completed cast
	// boundary. The keyword trigger body is rebuilt from its logged push.
	Casualty bool
	// CasPayload is the copy body's rider payload, in the StackCopy Counter
	// grammar (events.StackCopyCounter): the Casualty:X carrier's script
	// riders (Ob Nixilis, the Adversary's NonLegendary$ True |
	// SetLoyalty$ Casualty:...), so the __kwCasualty: rebuild mints the
	// identical copy body on replay. Empty for every numeric Casualty, whose
	// copy is an exact copy. Plain data carried to the push's Counter.
	CasPayload string
	// Demonstrate is a GRANTED demonstrate keyword (a layer-6 AddKeyword$
	// Demonstrate -- Silverquill Lecturer's "Creature spells you cast have
	// demonstrate", The Twelfth Doctor's non-hand grant, Try-My-Deck
	// Elemental's commander grant, the Strixhaven plane's instant/sorcery
	// grant): the same shape as Conspire -- the queue carries no parameter
	// (the body has none) and the drain pushes a KeywordTriggerPush whose
	// __kwDemonstrate: payload events.Apply rebuilds into the same
	// DB$ Demonstrate body the printed K:Demonstrate expansion carries,
	// with the cast spell riding IDs as Remembered. Idx and SA are unset
	// for it.
	Demonstrate bool
	// Cipher is the combat-damage-to-player trigger a creature's ENCODED
	// card grants (CR 702.99b: "Whenever that creature deals combat damage
	// to a player, its controller may cast a copy of the encoded card
	// without paying its mana cost"). It has no printed trigger index --
	// the association that grants it is runtime state (state.Object
	// .EncodedCards) -- so this field carries the encoded CARD's object id
	// (0 = not a cipher trigger, so the zero value is never a real one).
	// The drain pushes a KeywordTriggerPush whose __kwCipher: payload
	// events.Apply rebuilds into the same DB$ Play body that copies and
	// casts the exiled card, with the encoded card riding IDs as
	// Remembered. Idx and SA are unset for it.
	Cipher state.ObjID
	// Cascade is a printed-or-granted cascade keyword (CR 702.85, task
	// cascade1): the queue carries no parameter (the trigger body is the
	// same DB$ Cascade body whichever route granted the keyword) and the
	// drain pushes a KeywordTriggerPush whose __kwCascade: payload
	// events.Apply rebuilds structurally -- the Ward shape. The trigger's
	// Source is the CAST SPELL (the stack object), whose face's mana value
	// the effect reads at resolution. Idx and SA are unset for it.
	Cascade bool
	// Exploit is a GRANTED exploit keyword (a layer-6 AddKeyword$ Exploit,
	// e.g. Colonel Autumn's "Other legendary creatures you control have
	// exploit"): the same shape as Ward/Afflict -- the queue carries no
	// parameter (the trigger body is the same two-step Sacrifice -> Exploit
	// chain the printed K:Exploit expansion carries) and the drain pushes a
	// KeywordTriggerPush whose __kwExploitGranted payload events.Apply rebuilds
	// structurally. The trigger's Source is the GRANTED creature that just
	// entered, so the marker names it as the exploiter. Idx and SA are unset
	// for it.
	Exploit bool
	// Offspring is a GRANTED offspring keyword (a layer-6 AddKeyword$
	// Offspring:<cost>, e.g. Zinnia, Valley's Voice's "Creature spells you
	// cast have offspring {2}"): the same Ward/Afflict/Exploit shape -- the
	// queue carries no parameter (the trigger body is the same
	// DB$ CopyPermanent | Defined$ Self | NumCopies$ Count$OffspringPaid |
	// SetPower$ 1 | SetToughness$ 1 body the printed K:Offspring expansion
	// carries) and the drain pushes a KeywordTriggerPush whose
	// __kwOffspringGranted payload events.Apply rebuilds structurally. The
	// trigger's Source is the GRANTED creature that just entered, so the
	// Count$OffspringPaid read resolves against its pay-time provenance.
	// Idx and SA are unset for it.
	Offspring bool
	// Flanking is a GRANTED flanking instance (CR 702.25a via a layer-6
	// AddKeyword$ Flanking -- Agility, Flanking Licid, Sidewinder Sliver,
	// Cavalry Master): the Ward/Conspire shape. A creature granted flanking
	// has no printed K:Flanking trigger to carry the pump body, so the drain
	// pushes a KeywordTriggerPush whose __kwFlanking: payload events.Apply
	// rebuilds into the same DB$ Pump | Defined$ TriggeredBlockerLKICopy |
	// NumAtt$ -1 | NumDef$ -1 body the printed expansion carries. The blocked
	// creature rides IDs as Remembered (the printed path's own slot), which
	// Defined$ TriggeredBlockerLKICopy reads at resolution. Idx and SA are
	// unset for it.
	Flanking bool
	// Mentor is a GRANTED mentor keyword (CR 702.134 via a layer-6
	// AddKeyword$ Mentor -- Aegis of the Legion's "Equipped creature has
	// mentor", Nyxborn Unicorn's Bestow aura): the Ward/Afflict/Flanking
	// shape. A creature granted mentor has no printed K:Mentor trigger to
	// carry the targeted counter body, so checkGrantedMentorTriggers
	// synthesizes the ordinary Attacks trigger and the drain pushes a
	// KeywordTriggerPush whose __kwMentorGranted: payload events.Apply
	// rebuilds into the same DB$ PutCounter | ValidTgts$ Creature.attacking |
	// Mentor$ True body the printed expansion carries, then poses that body's
	// ordinary target ask (unlike Ward/Afflict the body is targeted). Idx and
	// SA are unset for it.
	Mentor bool
	// Cumulative is a GRANTED cumulative-upkeep cost (CR 702.24 via a layer-6
	// AddKeyword$ Cumulative upkeep:<cost> -- Breath of Dreams, Mana Chains,
	// Decomposition -- or an A:AB$ Pump's KW$ Cumulative upkeep:<cost> --
	// Balduvian Shaman, Dreams of the Dead): the Ward/Afflict/Flanking shape.
	// A permanent granted the keyword has no printed K:Cumulative upkeep
	// expansion trigger to carry the beginning-of-upkeep age-counter and
	// pay/sacrifice window, so checkGrantedCumulativeUpkeepTriggers
	// synthesizes the ordinary Phase trigger and the drain pushes a
	// KeywordTriggerPush whose __kwCumulativeUpkeepGranted:<cost> payload
	// events.Apply rebuilds into the same DB$ CumulativeUpkeep | Cost$ <cost>
	// ability the printed K:Cumulative upkeep expansion carries. The field is
	// the parsed-out upkeep COST text (the display suffix after a second colon
	// stripped, exactly as cards/kw_cumulativeupkeep.go strips it). Idx and SA
	// are unset for it; unlike Ward/Afflict it carries no Ctx roles (the
	// trigger reads only the permanent and its controller).
	Cumulative string
	// RingEmblem is one of the Ring emblem's four level abilities (CR
	// 701.54c), queued by checkRingEmblemTriggers. The emblem has no face
	// and no object in any zone, so like Ward/Afflict this entry carries
	// only the LEVEL: pushTrigger mints a RingEmblemPush whose __ring:<level>
	// payload events.Apply rebuilds the ability from. Source is 0 (there is
	// no permanent), Controller the tempted seat.
	RingEmblem int
	Ctx        effects.Ctx
}

// triggerKey identifies one T: line: the object that carries it, plus that
// object's own Triggers index (a card can have more than one). Used both for
// the cascade bound and for DamageDealtOnce/DamageDoneOnce's once-per-batch
// gate.
type triggerKey struct {
	Source state.ObjID
	Idx    int
	// Face distinguishes a Room's two faces (rules/rooms.go): an unlocked
	// room's ALTERNATE face (index 1) carries its own triggers whose
	// fire-count and once-per-turn memory must never share an entry with
	// the cast face's same-index trigger. Zero for every ordinary read --
	// the zero value keeps the field invisible to every existing key build.
	Face uint8
}

// damageBatchKey identifies one DamageDealtOnce/DamageDoneOnce/DamageAll
// trigger's referent within one damage batch. DamageDealtOnce latches per
// DEALING source (Forge GameAction.triggerDamageDoneOnce's dealt half: one
// trigger per source per batch, its referent amount the total that source
// dealt in the batch); DamageDoneOnce latches per DAMAGED object (the done
// half: one trigger per target, its referent amount the total that target
// took); DamageAll latches per TRIGGER LINE -- the "one or more" batch mode
// (Forge GameAction.triggerDamageAll): the FIRST (source, target) pair in the
// batch whose both halves match queues the single instance, and every later
// matching pair accumulates into it -- the key carries NO referent for the
// all mode, so damage landing on several targets in one batch is still ONE
// instance. The embedded triggerKey keeps two T: lines of one card -- and the
// same line on two cards -- independent.
type damageBatchKey struct {
	triggerKey
	dealt  bool           // true: referent is the dealing source (DamageDealtOnce)
	all    bool           // true: the batch-level DamageAll latch (no referent)
	obj    state.ObjID    // the referent object (dealing source, or damaged object)
	player state.PlayerID // the referent player when the damage went to a player
}

// damageBatchEntry records one (trigger, referent) pair already queued inside
// the open damage batch: the pendingTriggers index it queued at (the queue is
// append-only while a batch is open, so the index is stable until batch close)
// and the batch amount accumulated so far, which closeDamageBatch patches into
// the queued trigger's TriggerAmount referent. A DamageAll entry (key.all)
// additionally accumulates the batch's matching source and target SETS --
// every (source, target) pair this line matched, deduplicated in first-seen
// order -- which closeDamageBatch patches into the queued trigger's
// TriggerDamageSources/TriggerDamageTargets capture, the referents the
// TriggeredPlayersTargets$Amount count and the Defined$ TriggeredTargets /
// TriggeredSourcesController plural selectors read at resolution.
type damageBatchEntry struct {
	key     damageBatchKey
	idx     int
	amount  int32
	sources []state.ObjID
	targets []state.Target
}

// zoneBatchKey identifies one ChangesZoneAll trigger line already queued
// inside an open zone batch. The batch-level zone mode latches per TRIGGER
// LINE -- the "one or more" reading (Forge RepeatEachEffect's CardZoneTable:
// the loop's accumulated zone changes reach triggerChangesZoneAll ONCE, so
// the trigger fires once for the whole table, not once per move).
type zoneBatchKey struct {
	triggerKey
}

// zoneBatchEntry records one ChangesZoneAll line already queued inside the
// open zone batch: the pendingTriggers index it queued at (the queue is
// append-only while a batch is open, so the index is stable until batch
// close) and the batch's moved-object set -- every object this line matched,
// deduplicated in first-seen order -- which closeZoneBatch patches into the
// queued trigger's Remembered/Captured, the plural capture the "for each of
// them" bodies (Defined$ TriggeredObjectLKICopy, RememberedLKI) resolve at
// resolution.
type zoneBatchEntry struct {
	key   triggerKey
	idx   int
	moved []state.Target
}

// millBatchEntry records one MilledAll trigger line already queued inside the
// open mill batch: the pendingTriggers index it queued at (the queue is
// append-only while a batch is open) plus the number of cards this line's
// ValidCard$ matched and their deduplicated set. closeMillBatch patches the
// count into the queued trigger's TriggerAmount (the TriggerCount$Amount
// head) and the set into its Remembered/Captured plural capture.
type millBatchEntry struct {
	key    triggerKey
	idx    int
	amount int32
	milled []state.Target
}

// discardBatchEntry records one DiscardedAll trigger line already queued
// inside the open discard batch: the pendingTriggers index it queued at (the
// queue is append-only while a batch is open) plus the number of cards this
// line's ValidCard$ matched and their deduplicated set. closeDiscardBatch
// patches the count into the queued trigger's TriggerAmount (the
// TriggerCount$Amount head Magmakin Artillerist's X reads) and the set into
// its Remembered/Captured plural capture (Pure Intentions' Defined$
// TriggeredCards).
type discardBatchEntry struct {
	key       triggerKey
	idx       int
	amount    int32
	discarded []state.Target
}

// turnFires is one T: line's trigger count within the turn it last
// triggered (Engine.triggerTurnFires).
type turnFires struct {
	Turn int32
	N    int32
}

// combatFires is one T: line's once-per-combat latch stamp
// (Engine.unblockedOnceFired): the (turn, combat-of-that-turn) pair that
// uniquely names the combat the line last fired in. A new turn or a new
// combat phase within it changes the stamp and re-arms the trigger.
type combatFires struct {
	Turn   int32
	Combat int32
}

// actionTriggerModes are the event-trigger modes the sacrifice/discard/tap/
// crime/attack-declaration ticket registered. The trigger-level parameters
// only they honour -- ActivationLimit$, PlayerTurn$, and a CheckDefinedPlayer$
// predicate this build cannot evaluate failing closed -- are scoped to these
// modes, so no trigger of another mode that fired before stops firing or
// fires less often.
var actionTriggerModes = map[string]bool{
	// Clashed joins them for the same reason: it is an event mode registered
	// with its own marker Kind (events.Clash, task clash1), so the
	// trigger-level parameters Forge scopes to every event mode --
	// PlayerTurn$, ActivationLimit$, and an unevaluable CheckDefinedPlayer$
	// predicate failing closed -- apply from day one. Its own ValidPlayer$ /
	// Won$ gate is read by clashMatches, not by this map.
	"Clashed":                    true,
	"AttackersDeclaredOneTarget": true, "AttackersDeclared": true, "AttackerUnblocked": true, "Sacrificed": true, "Discarded": true,
	"CommitCrime": true, "Taps": true, "TapsForMana": true, "Untaps": true,
	// BecomesTargetOnce joins them for the same reason: it is an event mode
	// registered from the start as an event-mode sibling of BecomesTarget
	// (rules/trigmatch_misc.go's becomesTargetOnceMatches), so the trigger-
	// level parameters Forge scopes to every event mode -- PlayerTurn$,
	// ActivationLimit$ (Professor Hojo's "This ability triggers only once
	// each turn"), and an unevaluable CheckDefinedPlayer$ predicate failing
	// closed -- apply from day one. The mode's own once-per-targeting-ACTION
	// cadence is the target-batch latch, not this set.
	"BecomesTargetOnce": true,
	// DamagePreventedOnce joins them for the same reason: it is an event mode
	// registered from the start (rules/trigger_match.go's
	// damagePreventedMatches), so the trigger-level parameters Forge scopes
	// to every event mode -- PlayerTurn$, ActivationLimit$, and an
	// unevaluable CheckDefinedPlayer$ predicate failing closed -- apply from
	// day one. No Once latch rides it: each stored prevention Note is one
	// occurrence (prevention happens per Damage event, no batching concept --
	// the DamageDealtOnce/DamageDoneOnce batch latch exists because combat
	// batches several Damage events).
	// TokenCreated/TokenCreatedOnce are event modes registered from the start
	// (rules/trigger_match.go's tokenCreatedMatches), so the trigger-level
	// parameters Forge scopes to every event mode -- PlayerTurn$,
	// ActivationLimit$, and an unevaluable CheckDefinedPlayer$ predicate
	// failing closed -- apply from day one, and the Once mode's implicit
	// once-per-turn latch rides the same queue-time gate.
	"TokenCreated": true, "TokenCreatedOnce": true,
	// ChangesZoneAll joins them for the trigger-level parameters Forge scopes
	// to every event mode: ActivationLimit$ ("triggers only once each turn"
	// on 40 of the 126 corpus ChangesZoneAll lines) MUST be enforced, and the
	// mode's 7 PlayerTurn$ True lines want the same requirement as every
	// other event mode. Membership also makes an unevaluable CheckDefinedPlayer$
	// predicate fail closed for the mode, which is the conservative direction
	// for a mode registered from the start.
	// FlippedCoin joins them for the same reason: it is an event mode
	// registered from the start (rules/trigger_match.go's
	// flippedCoinMatches, firing off the canonical coin-flip result Note
	// both api:FlipCoin and the cumulative-upkeep cost action emit), so the
	// trigger-level parameters Forge scopes to every event mode --
	// PlayerTurn$, ActivationLimit$, and an unevaluable CheckDefinedPlayer$
	// predicate failing closed -- apply from day one. No Once latch: each
	// flip result Note is one occurrence.
	"FlippedCoin": true,
	// RolledDie/RolledDieOnce join them for the same reason: both are event
	// modes registered from the start (rules/trigmatch_misc.go's
	// rolledDieMatches / rolledDieOnceMatches, firing off the canonical roll
	// Notes effects/dice.go emits), so the trigger-level parameters Forge
	// scopes to every event mode -- PlayerTurn$, ActivationLimit$, and an
	// unevaluable CheckDefinedPlayer$ predicate failing closed -- apply from
	// day one. RolledDieOnce's own batch cadence is the event, not a Once
	// latch: each batch Note is one occurrence.
	"RolledDie": true, "RolledDieOnce": true,
	"ChangesZoneAll": true,
	// Attached is an event mode registered from the start
	// (attachedMatches over events.Attach), so the trigger-level parameters
	// Forge scopes to every event mode -- PlayerTurn$, ActivationLimit$, and
	// an unevaluable CheckDefinedPlayer$ predicate failing closed -- apply
	// from day one (Inchblade Companion carries ActivationLimit$ 1).
	"Attached": true,
	// LifeGained joins them for the same reason: it is an event mode
	// registered from the start (lifeGainedMatches over events.LifeChange,
	// the api:RemoveCounter ticket's Prize Pig pin), so the trigger-level
	// parameters Forge scopes to every event mode -- PlayerTurn$ (5 corpus
	// lines: Vampire Scrivener, Wax//Wane Witness, Moonstone Harbinger,
	// Cat Collector), ActivationLimit$ (2 lines) and an unevaluable
	// CheckDefinedPlayer$ predicate failing closed -- apply. lifeGainedMatches
	// itself reads FirstTime$ (8 lines over 7 files: Attended Healer,
	// Deathless Knight, Vanguard Seraph, Gourmand's Talent, ...), the
	// once-per-turn latch lifeLostMatches implements without the map.
	// LifeGained joins them for the same reason: it is an event mode
	// registered from the start (lifeGainedMatches over events.LifeChange,
	// the api:RemoveCounter ticket's Prize Pig pin), so the trigger-level
	// parameters Forge scopes to every event mode -- PlayerTurn$ (5 corpus
	// lines: Vampire Scrivener, Wax//Wane Witness, Moonstone Harbinger,
	// Cat Collector), ActivationLimit$ (2 lines) and an unevaluable
	// CheckDefinedPlayer$ predicate failing closed -- apply. lifeGainedMatches
	// itself reads FirstTime$ (8 lines over 7 files: Attended Healer,
	// Deathless Knight, Vanguard Seraph, Gourmand's Talent, ...), the
	// once-per-turn latch lifeLostMatches implements without the map.
	"LifeGained": true,
	// Discover and SeekAll join them: both are event modes registered with
	// their own marker Kinds (events.Discover/events.Seek, task trigdisc1)
	// whose corpus carriers carry the trigger-level parameters Forge scopes
	// to every event mode -- Curator of Sun's Creation's ActivationLimit$ 1
	// on its Discover line ("This ability triggers only once each turn")
	// and Lurker in the Deep's PlayerTurn$ True on its SeekAll line -- so
	// both gates must apply from day one.
	"Discover": true, "SeekAll": true,
	// Surveil joins them for the same reason: it is an event mode registered
	// from the start (surveilMatches over events.Surveil, the marker Kind
	// api:Surveil emits, task trig-surveil), so the trigger-level parameters
	// Forge scopes to every event mode -- PlayerTurn$, ActivationLimit$
	// (Prudent Fateseer's "This ability triggers only once each turn" on its
	// scry-or-surveil line) and an unevaluable CheckDefinedPlayer$ predicate
	// failing closed -- apply from day one. surveilMatches itself reads
	// FirstTime$ (Whispering Snitch's "for the first time each turn")
	// through the shared firstMarkerThisTurn log scan.
	"Surveil": true,
	// Milled/MilledAll join them for the same reason: both are event modes
	// registered from the start (milledMatches over events.Mill, task
	// trig-milled), so the trigger-level parameters Forge scopes to every
	// event mode apply from day one -- ActivationLimit$ (Mirelurk Queen's
	// "This ability triggers only once each turn" on its MilledAll line),
	// PlayerTurn$ and an unevaluable CheckDefinedPlayer$ predicate failing
	// closed. MilledAll's once-per-turn cadence is its own batch latch (the
	// mill entry), not this map; the map only makes ActivationLimit$ count
	// the batch's single queue slot.
	"Milled": true, "MilledAll": true,
	// DiscardedAll joins them for the same reason: it is an event mode
	// registered from the start (discardedAllMatches over the discard
	// MoveZone marker, task DiscardedAll), so the trigger-level parameters
	// Forge scopes to every event mode apply from day one -- ActivationLimit$
	// ("This ability triggers only once each turn" on a DiscardedAll line),
	// PlayerTurn$ and an unevaluable CheckDefinedPlayer$ predicate failing
	// closed. DiscardedAll's own FirstTime$ cadence is its batch latch (the
	// trigger_match.go entry), not this map; the map only makes ActivationLimit$
	// count the batch's single queue slot.
	"DiscardedAll": true,
	// Proliferate joins them for the same reason: it is an event mode whose
	// marker Kind (events.Proliferate, task trig-proliferate) api:Proliferate
	// emits, and the corpus carriers scope the trigger-level parameters Forge
	// applies to every event mode -- Contagion Dispenser's line carries BOTH
	// PlayerTurn$ True and ActivationLimit$ 1 ("Whenever you proliferate
	// during your turn, ... triggers only once each turn"), so without
	// membership it could fire on an opponent's turn and repeatedly on its
	// controller's. Membership also makes an unevaluable CheckDefinedPlayer$
	// predicate fail closed for the mode.
	"Proliferate": true,
}

// triggerGameLimitFor parses a trigger's GameActivationLimit$ param (the
// per-GAME "this ability triggers only once" sibling of ActivationLimit$).
// present is true only when the param is there; a malformed or negative value
// reports (0, true) so the gate denies it (fail closed), matching
// triggerActivationLimitAllows's malformed handling.
func triggerGameLimitFor(t cards.Trigger) (limit int, present bool) {
	raw, present := t.Params["GameActivationLimit"]
	if !present {
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit < 0 {
		return 0, true // malformed: present, denies
	}
	return limit, true
}

// triggerGameActivationLimitAllows is the READ half of the GameActivationLimit$
// trigger gate: has this line's lifetime queue count (Engine.triggerGameFires)
// reached the limit? It must never mutate. A matched event can still be
// rejected by a later gate -- the per-turn ActivationLimit$ gate on a
// combined-parameter line, a dedicated combat hook's empty candidate scan,
// the Damage batch latch, a nil Execute$ body -- and such an event must not
// consume a use; the count is therefore committed by
// reserveTriggerGameActivationLimit at the queue point only.
func (e *Engine) triggerGameActivationLimitAllows(t cards.Trigger, key triggerKey) bool {
	limit, present := triggerGameLimitFor(t)
	if !present {
		return true
	}
	return int(e.triggerGameFires[key]) < limit
}

// reserveTriggerGameActivationLimit commits the lifetime queue count for a
// trigger that passed every gate and is actually being queued. Callers MUST
// have run the read half (triggerGameActivationLimitAllows) first; a commit
// past a reached limit is refused defensively so a drifted caller cannot
// exceed the limit.
func (e *Engine) reserveTriggerGameActivationLimit(t cards.Trigger, key triggerKey) {
	limit, present := triggerGameLimitFor(t)
	if !present {
		return
	}
	if e.triggerGameFires == nil {
		e.triggerGameFires = map[triggerKey]int32{}
	}
	if int(e.triggerGameFires[key]) >= limit {
		return
	}
	e.triggerGameFires[key]++
}

// triggerTurnLimitFor parses a trigger's effective per-turn limit: the
// ActivationLimit$ param, or the implicit once-per-turn limit of Mode$
// TokenCreatedOnce (Akim, the Soaring Wind: "whenever you create one or more
// tokens for the first time each turn"). present is true only when a limit
// applies; a malformed or negative value reports (0, true) so the gate denies
// it (fail closed).
func triggerTurnLimitFor(t cards.Trigger) (limit int, present bool) {
	raw, ok := t.Params["ActivationLimit"]
	if !ok {
		// The Once mode's implicit limit reuses triggerTurnFires (which Clone
		// already deep-copies) instead of a second latch field, keeping the two
		// per-turn trigger counts structurally identical.
		if t.Mode == "TokenCreatedOnce" {
			return 1, true
		}
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit < 0 {
		return 0, true // malformed: present, denies
	}
	// The Once mode's meaning is once per turn; an explicit ActivationLimit$
	// above 1 on a TokenCreatedOnce line cannot raise it.
	if t.Mode == "TokenCreatedOnce" && limit > 1 {
		limit = 1
	}
	return limit, true
}

// triggerActivationLimitAllows is the READ half of the per-turn
// ActivationLimit$ gate ("this ability triggers only once each turn", Forge
// Trigger.checkActivationLimit): has this line queued fewer than N triggers
// this turn? It must never mutate -- a matched event rejected by a later gate
// must not consume a use; the count is committed by
// reserveTriggerActivationLimit at the queue point only.
func (e *Engine) triggerActivationLimitAllows(t cards.Trigger, key triggerKey) bool {
	limit, present := triggerTurnLimitFor(t)
	if !present {
		return true
	}
	f := e.triggerTurnFires[key]
	if f.Turn != e.G.Turn {
		f = turnFires{Turn: e.G.Turn}
	}
	return int(f.N) < limit
}

// reserveTriggerActivationLimit commits the per-turn queue count for a
// trigger that passed every gate and is actually being queued. Callers MUST
// have run the read half (triggerActivationLimitAllows) first; a commit past
// a reached limit is refused defensively.
func (e *Engine) reserveTriggerActivationLimit(t cards.Trigger, key triggerKey) {
	limit, present := triggerTurnLimitFor(t)
	if !present {
		return
	}
	if e.triggerTurnFires == nil {
		e.triggerTurnFires = map[triggerKey]turnFires{}
	}
	f := e.triggerTurnFires[key]
	if f.Turn != e.G.Turn {
		f = turnFires{Turn: e.G.Turn}
	}
	if int(f.N) >= limit {
		return
	}
	f.N++
	e.triggerTurnFires[key] = f
}

// reserveTriggerLimits commits BOTH trigger-limit counts for a queue that is
// happening, so every queue site reserves the same pair under the same
// actionTriggerModes scoping (GameActivationLimit$ on every mode,
// ActivationLimit$ only on the action modes) and a future queue site cannot
// forget one half.
func (e *Engine) reserveTriggerLimits(t cards.Trigger, key triggerKey) {
	e.reserveTriggerGameActivationLimit(t, key)
	if actionTriggerModes[t.Mode] {
		e.reserveTriggerActivationLimit(t, key)
	}
}

// dieRollNumberAllows enforces a RolledDie trigger's Number$ N ("whenever
// you roll your third die each turn", Resolute Veggiesaur): the line queues
// only on the Nth matching die of the turn, and the per-turn counter lives
// per trigger line (triggerTurnDice, keyed by triggerKey) so several roll
// triggers on one permanent do not consume each other's count. An absent
// Number$ always allows; a malformed or non-positive one denies (fail
// closed), matching triggerActivationLimitAllows's handling. The count self-
// resets when the turn changes, exactly as triggerTurnFires does, and the
// whole map is dropped the first time it is consulted in a new turn, so it
// holds only the current turn's lines rather than growing for the match.
func (e *Engine) dieRollNumberAllows(t cards.Trigger, key triggerKey) bool {
	raw, present := t.Params["Number"]
	if !present {
		return true
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return false
	}
	if e.triggerTurnDice == nil || e.triggerTurnDiceTurn != e.G.Turn {
		e.triggerTurnDice = map[triggerKey]turnFires{}
		e.triggerTurnDiceTurn = e.G.Turn
	}
	f := e.triggerTurnDice[key]
	if f.Turn != e.G.Turn {
		f = turnFires{Turn: e.G.Turn}
	}
	f.N++
	e.triggerTurnDice[key] = f
	return int(f.N) == n
}

// resolvedLimitValue parses a trigger's ResolvedLimit$ param. ok is true only
// when the param is present; a malformed value is reported as (0, true) so
// the gate denies it (fail closed), matching triggerActivationLimitAllows's
// malformed handling.
func resolvedLimitValue(t cards.Trigger) (int, bool) {
	raw, present := t.Params["ResolvedLimit"]
	if !present {
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit < 0 {
		return 0, true // malformed: present, denies
	}
	return limit, true
}

// noteTriggerResolved increments source's per-turn resolution count for the
// ResolvedLimit$ gate. Callers MUST have verified that the trigger line being
// resolved itself carries ResolvedLimit$ (resolvedLimitValue on the trigger
// findTriggerForAbility returned for the resolving ability) -- a mixed-line
// source's OTHER trigger lines must never consume this line's limit (the
// round-2 defect: sourceHasResolvedLimit potted any line of the source and
// killed Cosmic Crucible's copy trigger every turn its mandatory Main1 mana
// trigger resolved). The count self-resets when the turn changes, exactly as
// triggerTurnFires does, so no reset hook is needed.
func (e *Engine) noteTriggerResolved(source state.ObjID) {
	if e.triggerTurnResolved == nil {
		e.triggerTurnResolved = map[state.ObjID]turnFires{}
	}
	f := e.triggerTurnResolved[source]
	if f.Turn != e.G.Turn {
		f = turnFires{Turn: e.G.Turn}
	}
	f.N++
	e.triggerTurnResolved[source] = f
}

// maxTriggerFires bounds how many times a single (source, trigger index)
// pair may queue a pending trigger over the life of a match. Ordinary play
// stays far below this -- even a trigger that fires every turn for a hundred
// turns is two orders of magnitude under it. The pathological case this
// guards is "a trigger that fires in response to its own effect": nothing in
// this build can hang or overflow the stack resolving any *one* ability
// (Resolve's own maxChain bounds a sub-ability chain, and every stack
// resolution requires a fresh external Submit round-trip -- resolveTop is
// only ever called from handlePriority's "pass" case), but nothing stops a
// naive auto-pass driver from sustaining pop-one/push-one-again forever
// across many such round-trips, tying up the match's one goroutine
// indefinitely. This cap is what makes that terminate: once a specific
// trigger has fired this many times, it simply stops matching, so the loop
// runs dry instead of running forever.
const maxTriggerFires = 256

// forEachObject walks every object currently in the game exactly once, in a
// fixed, deterministic order: living seats in ascending order from seat 0,
// then zone in Zone's own declared order (library, hand, battlefield,
// graveyard, exile, stack), then position within that zone's slice.
// checkTriggers and applyReplacements both need this same walk for their own
// discovery to be deterministic, so it is factored out here rather than
// duplicated. Game.Zone ignores its player argument for ZStack (the stack is
// shared across controllers, not per-seat), so that zone is visited only
// once, on the first living seat, rather than once per living player.
//
// Memory: the live zone slice is copied into e.foreachBuf (engine.go) before
// the walk, because fn can move objects between zones (a trigger match
// putting something on the stack), so iterating the live, mutating slice
// would be a bug. The copy is kept and grown with append(buf[:0], zone...),
// so it settles at the size of the largest zone seen and stops allocating;
// a fresh []state.ObjID allocation per zone per event used to be this
// package's single largest allocation site (Task A2). fn is NEVER called
// after the zone it is walking mutates.
//
// Re-entry safety: each rendering passes its snapshot buffer separately --
// the depth-0 call (from checkTriggers or applyReplacements, always) reuses
// e.foreachBuf; a re-entrant call (fn reaching forEachObject again, directly
// or through emit -> checkTriggers) takes a fresh local buffer instead, so
// the inner walk can never overwrite the outer walk's snapshot mid-range.
// That re-entry is not reachable today -- the only two callers' fns are
// checkTriggers' and applyReplacements', neither of which calls emit or
// forEachObject -- but the guard makes the shape safe if one ever does,
// which is why this file does not rely on "re-entry cannot happen" to keep a
// single shared buffer correct.
func (e *Engine) forEachObject(fn func(id state.ObjID)) {
	e.foreachDepth++
	defer func() { e.foreachDepth-- }()
	buf := e.foreachBuf
	if e.foreachDepth > 1 {
		// Re-entrant: own a private snapshot, never the shared field.
		buf = nil
	}
	for si, p := range e.G.AliveFrom(0) {
		for z := state.ZLibrary; z <= state.ZStack; z++ {
			if z == state.ZStack && si != 0 {
				continue
			}
			// Reassign (not append inline) so the grown snapshot is carried into
			// e.foreachBuf: buf[:0] then append-in-place reuses the backing array
			// from the previous zone / previous call, settling at the largest
			// zone and stopping allocation. Walking the returned slice, not a
			// throwaway append expression, keeps the range over the persisted
			// snapshot rather than a discarded temporary.
			buf = append(buf[:0], e.G.Zone(z, p)...)
			for _, id := range buf {
				fn(id)
			}
		}
	}
	if e.foreachDepth <= 1 {
		// Keep the grown buffer on the Engine for the next depth-0 walk; a
		// re-entrant call's private buffer is discarded on return.
		e.foreachBuf = buf
	}
}

// effectMatchControllerFor reports the recurring-Effect matching overlay's
// virtual controller for id. The overlay is a read-only matcher scope that
// makes a registering Effect's trigger read the player the Effect belongs to
// -- its registration owner -- rather than the controller of the creating
// card (see checkEventDelayedTriggers). It is true only for the source id the
// overlay was armed for; every other id takes the ordinary state.Object read.
//
// This is the ONE home for that check: every source-controller read must go
// through it (via controllerOf, or directly where an LKI look-back would
// otherwise clobber the overlay), so the next consumer cannot re-derive it
// wrong.
func (e *Engine) effectMatchControllerFor(id state.ObjID) (state.PlayerID, bool) {
	if e.effectMatchOverride && id == e.effectMatchSource {
		return e.effectMatchController, true
	}
	return 0, false
}

// controllerOf is a nil-safe Object.Controller read: a nonexistent ObjID
// (stale data, a malformed trigger source) degrades to seat 0 rather than
// panicking. A recurring-Effect registration's source resolves the Effect's
// owner instead of the creating card's controller.
func (e *Engine) controllerOf(id state.ObjID) state.PlayerID {
	if c, ok := e.effectMatchControllerFor(id); ok {
		return c
	}
	if o := e.G.Obj(id); o != nil {
		return o.Controller
	}
	return 0
}

func (e *Engine) snapshotTriggerBoard() *triggerSnapshot {
	return &triggerSnapshot{game: e.G.Clone(), continuous: append([]ContinuousEffect(nil), e.continuous...)}
}

func (e *Engine) checkTriggers(ev events.Event, lki *state.Object,
	lkiPower, lkiToughness int32, lkiPTValid bool) {
	batch := e.triggerBefore != nil && ev.Kind == events.MoveZone &&
		ev.From == state.ZBattlefield && ev.To != state.ZBattlefield
	if batch {
		// Only leaves-the-battlefield triggers look back. Always and other
		// event modes continue to read the live board, not an obsolete state.
		observer := &Engine{G: e.triggerBefore.game, L: e.L,
			continuous: e.triggerBefore.continuous, continuousVersion: e.continuousVersion,
			setNameInPool: e.setNameInPool, layer4InPool: e.layer4InPool}
		// The observer reads the PRE-departure board from its own Game clone,
		// so it derives its own layer-3 rename and layer-4 derived-type tables
		// (setname.go, layer4types.go) rather than inheriting the live
		// engine's: a name or type filter here must see the snapshot's
		// characteristics, not the post-departure ones.
		if observer.setNameInPool {
			observer.refreshRenames()
		}
		if observer.layer4InPool {
			observer.refreshDerivedTypes()
		}
		obj := observer.G.Obj(ev.Obj)
		var power, toughness int32
		valid := obj != nil && obj.Zone == state.ZBattlefield && obj.Face() != nil
		if valid {
			power, toughness = observer.Power(ev.Obj), observer.Toughness(ev.Obj)
		}
		e.checkFaceTriggers(observer, ev, obj, power, toughness, valid, true, true)
	}
	e.checkFaceTriggers(e, ev, lki, lkiPower, lkiToughness, lkiPTValid, batch, false)
	if ev.Kind == events.Damage {
		e.checkCipherTriggers(ev)
	}
	if ev.Kind == events.TurnChange {
		e.collectExpiredDelayedTriggers()
	}
	// An Effect-created trigger may name any registered event mode (damage,
	// attacks, taps, etc.), not only the delayed modes with a matcher of
	// their own, so a live EffectRepeat registration widens the event gate.
	if ev.Kind == events.PutOnStack || ev.Kind == events.MoveZone || ev.Kind == events.MonarchChange ||
		ev.Kind == events.ControlChange || ev.Kind == events.Damage || ev.Kind == events.DeclareAttackers ||
		e.hasEffectRepeatDelayed() {
		e.checkEventDelayedTriggers(ev, lki)
	}
	// Sagas (kw:Chapter): a lore counter's chapter ability queues off the one
	// event that places a lore counter -- the LORE CounterChange (the entry
	// grant rules' foldEntryMove places and the draw-step half alike; task
	// addcounter1/2 moved the entry grant onto a real CounterChange, so the
	// old MoveZone arm double-queued the entry chapter).
	e.checkChapterTriggers(ev)
	if ev.Kind == events.Draw {
		e.offerMiracle(ev)
	}
	// The alternative-cost keyword family's event hooks (altcast.go): a
	// battlefield entry is where an evoked creature queues its pay-or-sacrifice
	// follow-up and a dashed/warped creature registers its end-step delayed
	// trigger; a discard that exiled a madness card queues its cast offer.
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
		e.altCostEnter(ev)
	}
	if ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZExile {
		e.offerMadness(ev)
	}
	if ev.Kind == events.StepChange {
		e.checkDelayedTriggers(ev)
	}
	// Become-blocked triggers (trig:AttackerBlocked; She-Hulk, Wallbreaker):
	// one queue entry per blocked attacker, which the ordinary per-trigger
	// face scan cannot express (see checkAttackerBlockedTriggers).
	if ev.Kind == events.DeclareBlockers {
		e.checkAttackerBlockedTriggers(ev)
		e.checkBlocksTriggers(ev)
	}
	// Rooms (CR 309.5): the unlocked half's "When you unlock this door"
	// trigger queues off the DoorUnlock event itself -- its face is the
	// alternate face (FaceIdx 1), which the ordinary face scan above does
	// not walk.
	if ev.Kind == events.DoorUnlock {
		e.checkUnlockTriggers(ev)
	}
	// Exert's Trigger$ rider (task exert1, CR 702.100a): the static's named
	// SVar body queues off the Exert event itself, with Source = the
	// exerted permanent. The Amount -1 consume marker fires nothing: it is
	// the untap-step scan's own bookkeeping fold.
	if ev.Kind == events.Exert && ev.Amount >= 0 {
		e.checkExertTriggers(ev)
	}
	// The Ring emblem's four level abilities (CR 701.54c): engine-side
	// "whenever" abilities with no corpus script text and no object in any
	// zone, so the ordinary per-face walk above can never see them. The
	// scan reads state.Player.RingTempted (the fold RingTemptsYou just made
	// renders here BEFORE any trigger check, so level 4 sees its own
	// temptation) and queues one entry per firing seat.
	e.checkRingEmblemTriggers(ev)
	// A chaos-ensues marker (CR 901.9, task planar-verbs): the current
	// plane's Mode$ ChaosEnsues ability. The plane lives in ZPlanarDeck,
	// which the per-face walk above never visits, so this synthetic scan
	// queues it -- the checkRingEmblemTriggers precedent.
	e.checkChaosEnsuesTriggers(ev)
	// A planeswalk (CR 901.8, task planar-verbs): the arrived-at plane's
	// Mode$ PlaneswalkedTo and the left plane's Mode$ PlaneswalkedFrom. Both
	// planes live in the private ZPlanarDeck zone, so this is the same
	// synthetic-scan shape as the chaos marker above.
	e.checkPlaneswalkTriggers(ev)
}

// The Ring emblem's four level gates (CR 701.54c). Level N is active iff the
// tempted seat's RingTempted >= N; the count is uncapped, so lower levels
// stay active as it rises.
const (
	ringEmblemLevelDraw      = 1 // whenever your Ring-bearer attacks, draw a card
	ringEmblemLevelBlocked   = 2 // whenever your Ring-bearer becomes blocked, discard a card; if you can't, sacrifice it
	ringEmblemLevelCombatHit = 3 // whenever your Ring-bearer deals combat damage to a player, sacrifice it
	ringEmblemLevelTempted   = 4 // whenever the Ring tempts you, each opponent loses 1 life
)

// checkRingEmblemTriggers queues the Ring emblem's level abilities (CR
// 701.54c) for one event. The emblem is not an object in any zone, so the
// ordinary per-face walk cannot reach it; this is the per-event synthetic
// scan site (the checkAttackerBlockedTriggers / checkChapterTriggers
// precedent). Each level names one event shape:
//
//	1  DeclareAttackers whose attacker list holds a seat's Ring-bearer
//	2  DeclareBlockers whose blocked-attacker pairs hold a seat's Ring-bearer
//	3  a COMBAT Damage event whose source is a seat's Ring-bearer and whose
//	   recipient is a player (Obj 0 -- a hit redirected onto a permanent is
//	   not "damage to a player")
//	4  the RingTemptsYou event itself, for the tempted seat
//
// Because RingTemptsYou's own fold runs before checkTriggers, level 4's gate
// reads the POST-fold count: the 4th temptation fires it and the 3rd does
// not. The entries carry no Source (there is no permanent): pushTrigger mints
// a RingEmblemPush whose payload events.Apply rebuilds the ability from.
func (e *Engine) checkRingEmblemTriggers(ev events.Event) {
	switch ev.Kind {
	case events.DeclareAttackers, events.DeclareBlockers, events.Damage, events.RingTemptsYou:
	default:
		return
	}
	for p := state.PlayerID(0); int(p) < len(e.G.Players); p++ {
		if e.G.Players[p].Lost {
			continue
		}
		tempted := e.G.Players[p].RingTempted
		if tempted <= 0 {
			continue
		}
		bearer := e.G.Players[p].RingBearer
		switch ev.Kind {
		case events.RingTemptsYou:
			if ev.Player == p && tempted >= ringEmblemLevelTempted {
				e.queueRingEmblem(p, ringEmblemLevelTempted, bearer)
			}
		case events.DeclareAttackers:
			if tempted >= ringEmblemLevelDraw && bearer != 0 && objIDIn(ev.IDs, bearer) {
				e.queueRingEmblem(p, ringEmblemLevelDraw, bearer)
			}
		case events.DeclareBlockers:
			if tempted >= ringEmblemLevelBlocked && bearer != 0 && blockedAttackerIn(ev.Pairs, bearer) {
				e.queueRingEmblem(p, ringEmblemLevelBlocked, bearer)
			}
		case events.Damage:
			if tempted >= ringEmblemLevelCombatHit && bearer != 0 && ev.Obj == 0 &&
				e.combatDamaging && e.damaging == bearer {
				e.queueRingEmblem(p, ringEmblemLevelCombatHit, bearer)
			}
		}
	}
}

// queueRingEmblem appends one emblem level ability to the pending queue. The
// TriggerCard role names the Ring-bearer the firing event was about so a
// chained body that ever wants it can read it; the hand-built bodies read
// only Defined$ You/Opponent and the live designation.
func (e *Engine) queueRingEmblem(p state.PlayerID, level int, bearer state.ObjID) {
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Controller: p,
		RingEmblem: level,
		Ctx: effects.Ctx{
			Controller:     p,
			TriggerContext: effects.TriggerContext{TriggerCard: bearer},
		},
	})
}

// checkExertTriggers queues the Trigger$ rider of every offerable
// stat:OptionalAttackCost static carried by the exerted permanent (task
// exert1, CR 702.100a "When you do, ..."): a real triggered ability the
// ordinary APNAP drain places with Source = the exerted permanent, resolving
// with the registered APIs every rider body uses (UntapAll, AddPhase, Pump,
// ... -- the triage census). The queue entry is the granted-trigger shape
// (checkGrantedStaticTriggersUsing's precedent): the stack object is minted
// inside events.Apply from the Execute$ SVar name resolved against the
// exerted permanent's own table -- the same body grantedTriggerExecute links
// here, so live queue and replayed log carry the identical SA. The gate
// (ValidCard$, IsPresent$/...) is re-read per static exactly as the offer
// walk (rules/combat.go exertOfferHolds) read it; the static's parameters a
// fire-time re-check cannot evaluate fail closed and queue nothing. The walk
// is the deterministic activeStatics order, never a map, and the cascade
// bound is the granted slot's triggerKey (Source, Idx -1), the same key the
// granted walk shares -- cascade bound only, never once-per-turn memory.
func (e *Engine) checkExertTriggers(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return
	}
	for _, sv := range e.activeStatics("OptionalAttackCost") {
		if sv.Source != ev.Obj {
			continue
		}
		if vc := sv.Params["ValidCard"]; vc != "" &&
			!e.matchesSpecFrom(vc, ev.Obj, o.Controller, sv.Source) {
			continue
		}
		exec := sv.Params["Trigger"]
		if exec == "" {
			continue
		}
		sa := grantedTriggerExecute(o, exec)
		if sa == nil {
			continue
		}
		key := triggerKey{Source: ev.Obj, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] >= maxTriggerFires {
			continue // cascade bound: see maxTriggerFires.
		}
		e.triggerFireCount[key]++
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     ev.Obj,
			Controller: o.Controller,
			Idx:        -1,
			SA:         sa,
			Granted:    true,
			Execute:    exec,
			Ctx: effects.Ctx{
				Source:     ev.Obj,
				Controller: o.Controller,
			},
		})
	}
}

// checkFaceTriggers separates the read-only matching board from the live
// queue and firing limits. Both walks use deterministic seat/zone/slice order;
// the ordinary APNAP drain still asks each controller to order their triggers.
func (e *Engine) checkFaceTriggers(observer *Engine, ev events.Event, lki *state.Object,
	lkiPower, lkiToughness int32, lkiPTValid, split, leaving bool) {
	// phaseNotes collects the unresolvable Phase$ specs this walk encountered
	// (live walks only -- the leaves-the-battlefield look-back observer is a
	// scratch Engine that must never emit), each with the source that carries
	// them; they are emitted once, after the walk, so a Note emission's own
	// recursive checkTriggers can never interleave with the walk's matching.
	type phaseNote struct {
		id   state.ObjID
		spec string
	}
	var phaseNotes []phaseNote
	// Granted triggers inspect the same active-static list for every object
	// this event visits. Matching cannot emit or mutate continuous effects;
	// phase diagnostics emit only after the walk, so this snapshot is stable
	// for its full deterministic traversal. It is narrowed ONCE here to the
	// grants whose trigger Mode$ can observe this event kind (exact: see
	// checkGrantedStaticTriggersUsing), so the per-object walk never runs an
	// Affected$ spec match for a grant that cannot fire on this event.
	var grantedBuf [8]*ContinuousEffect
	grantedStatics := grantedTriggerStaticsFor(observer.active(), ev.Kind, grantedBuf[:0])
	// The event's compiled-interest test, hoisted out of the per-object walk:
	// compiledTriggerInterestAllows(interests, ev.Kind) is exactly
	// evAll || interests&evMask != 0 (see objectFaceMayTriggerHoisted).
	evAll, evMask := compiledTriggerInterestEvent(ev.Kind)
	visit := func(id state.ObjID) {
		o := observer.G.Obj(id)
		if o == nil || o.PhasedOut {
			return
		}
		f := o.Face()
		if f == nil {
			// Ruling F3: an ability or token object has no Face and
			// therefore no printed Triggers to check -- only real cards
			// carry triggered abilities.
			return
		}
		// objLKI is the whole-event LKI snapshot, hoisted here because every
		// trigger this loop matches for this event shares it (lki.ID == ev.Obj
		// always holds when lki != nil -- see checkTriggers's own doc above; a
		// defensive belt-and-braces check against a future emit change that
		// might one day pass a mismatched lki). triggerMatches and the matched
		// trigger's Ctx both read it. Hoisted above the face-down gate too: the
		// cloaked ward walk below reads it as well.
		var objLKI *state.Object
		if lki != nil && lki.ID == ev.Obj {
			objLKI = lki
		}
		if e.faceDownPrintedHides(o) {
			// CR 708.8: a face-down permanent's printed triggers (and any
			// granted walk keyed to it) do not exist while it is face down --
			// a manifested Sultai Emissary that dies reveals itself as a card
			// in the graveyard and fires nothing (the leaves-battlefield
			// look-back observer reads the pre-move state, where it is still
			// face down; the live walk matches leaves-triggers only through
			// that observer or a TriggerZones the departed card no longer
			// occupies).
			// A CLOAKED face-down permanent is the one exception: its ward {2}
			// is part of the cloak status itself (CR 708.5's cloak variant),
			// not a printed ability -- the synthesized Ward trigger in
			// checkGrantedWardTriggers (built from the derived keyword list
			// layers.go appends for Cloaked objects) is what turns targeting
			// it into the pay-or-counter ask. Only the granted-ward walk
			// revives; the printed-face walk stays suppressed.
			if o.Cloaked && ev.Kind == events.TargetsChosen {
				e.checkGrantedWardTriggers(observer, id, o, f, ev, objLKI, lkiPower, lkiToughness, lkiPTValid)
			}
			return
		}
		// Ordinary cards need no face-walk setup when their printed triggers
		// cannot observe this event. An unlocked Room may still have an
		// eligible alternate face. Granted Ward is independent of both -- and
		// so is a static-grant's trigger (AddTrigger$): the granted walk below
		// runs on BOTH paths, like Ward and Dethrone do.
		if !o.Unlocked && len(o.MergedCards) == 0 && !e.objectFaceMayTriggerHoisted(id, o.FaceIdx, f, ev.Kind, evAll, evMask) {
			if grantedKeywordTriggerEvent(ev.Kind) {
				switch ev.Kind {
				case events.TargetsChosen:
					e.checkGrantedWardTriggers(observer, id, o, f, ev, objLKI, lkiPower, lkiToughness, lkiPTValid)
				case events.DeclareAttackers:
					e.checkGrantedDethroneTriggers(observer, id, o, f, ev, objLKI)
					e.checkGrantedTrainingTriggers(observer, id, o, f, ev, objLKI)
					e.checkGrantedMeleeTriggers(observer, id, o, f, ev, objLKI)
					e.checkGrantedMentorTriggers(observer, id, o, f, ev, objLKI)
				case events.DeclareBlockers:
					e.checkGrantedAfflictTriggers(id, o, f, ev)
				case events.PutOnStack:
					e.checkGrantedConspireTriggers(observer, id, o, f, ev, objLKI)
					e.checkGrantedDemonstrateTriggers(observer, id, o, f, ev, objLKI)
				case events.MoveZone:
					e.checkGrantedExploitTriggers(observer, id, o, f, ev, objLKI)
					e.checkGrantedOffspringTriggers(observer, id, o, f, ev, objLKI)
				case events.StepChange:
					e.checkGrantedCumulativeUpkeepTriggers(observer, id, o, f, ev, objLKI)
				}
			}
			if len(grantedStatics) > 0 {
				e.checkGrantedStaticTriggersUsing(observer, grantedStatics, id, o, ev, objLKI, lkiPower, lkiToughness, lkiPTValid, split, leaving)
			}
			return
		}
		// Enchantment Rooms (rules/rooms.go): an UNLOCKED room's alternate
		// face is live too, so its triggers walk in the same scan. The face
		// index rides the triggerKey (Face field) so the alternate face's
		// fire-count and once-per-turn memory never share an entry with the
		// cast face's same-index trigger. roomTriggerFaces returns the faces
		// to walk, cast face first.
		faces, n := roomTriggerFaces(o, f)
		walk := faces[:n]
		if len(o.MergedCards) > 0 {
			// CR 702.140d: a mutated permanent has all abilities of the cards
			// beneath its top card, so their printed triggers must be walked
			// too. triggerFacesWithMerged marks them active=false, which routes
			// each through the by-name push (the Room alternate-face path) --
			// TriggerPush can only name a trigger INDEX into the top face, so an
			// under-card trigger must resolve its Execute$ SVar by name instead.
			// Ordinary (unmutated) objects keep the allocation-free [2]array
			// path above.
			walk = triggerFacesWithMerged(o, faces[:n])
		}
		for _, fc := range walk {
			if o.Unlocked && !e.objectFaceMayTrigger(id, fc.faceIdx, fc.face, ev.Kind) {
				continue
			}
			for ti, t := range fc.face.Triggers {
				// LifeLostAll is evaluated once at the end of a simultaneous
				// life-loss batch. Do not queue it once per serialized Damage/
				// LifeChange event, and do not let the finishing pass re-check
				// unrelated trigger modes.
				if t.Mode == "LifeLostAll" && e.lifeLossBatchDepth > 0 && !e.finishingLifeLossBatch {
					continue
				}
				if e.finishingLifeLossBatch && t.Mode != "LifeLostAll" {
					continue
				}
				if !leaving {
					// Phase$ is a common Forge trigger gate, not a Mode$ Phase
					// parameter: ChangesZone, SpellCast, and every other supported
					// trigger mode may carry it. Report an unresolvable value once
					// per engine per spec, while triggerMatches rejects it on every
					// event. Keeping reporting outside the scratch look-back walk
					// means a Note is a real event, never an observer side effect.
					spec := t.Params["Phase"]
					if strings.TrimSpace(spec) != "" {
						if !e.parsedPhaseSpec(spec).valid {
							if e.phaseUnknownNoted == nil {
								e.phaseUnknownNoted = map[string]bool{}
							}
							if !e.phaseUnknownNoted[spec] {
								e.phaseUnknownNoted[spec] = true
								phaseNotes = append(phaseNotes, phaseNote{id: id, spec: spec})
							}
						}
					}
				}
				// A "from anywhere" graveyard trigger is NOT a leaves-the-
				// battlefield trigger (CR 603.6c), even when this particular
				// move happens to leave the battlefield. Only the explicit
				// battlefield-origin shape looks back; destination triggers
				// still use the post-event source/zone in the live walk.
				looksBack := t.Mode == "ChangesZone" && t.Params["Origin"] == "Battlefield"
				if split && looksBack != leaving {
					continue
				}
				// CR 603.8 state trigger: its condition is checked against the
				// current state, not against the event under test, and it fires
				// at most once per outstanding instance. A trigger that already
				// has an instance queued or on the stack does not re-fire, so a
				// condition that stays true cannot enqueue an unbounded run
				// (the concise standing caveat against naively re-firing Always
				// on every bookkeeping event).
				if t.Mode == "Always" && e.stateTriggerOutstanding(id, ti) {
					continue
				}
				if !observer.triggerMatches(t, id, ev, objLKI) {
					continue
				}
				// Capture the DiscardedAll FirstTime$ clause the matcher just parsed
				// (discardedAllMatches records it on its own receiver) BEFORE
				// secondaryYields below -- which also drives this observer -- can
				// overwrite the scratch value.
				discardedAllFirstTime := t.Mode == "DiscardedAll" && observer.discardAllFirstTime
				// Forge's Secondary$ True: a marked secondary is the second
				// half of one card text, and it does not fire when the same
				// event already fired its card's paired primary (the
				// two-halves idiom -- Sower of Discord's complementary
				// DamageDoneOnce pair, Wooden Stake's blocks-or-is-blocked-by
				// pair). A secondary whose primary did not fire for this event
				// still fires on its own. See secondaryYields for the pairing.
				if strings.EqualFold(t.Params["Secondary"], "True") &&
					e.secondaryYields(observer, fc.face, ti, t, id, ev, objLKI) {
					continue
				}
				if (t.Mode == "DamageDealtOnce" || t.Mode == "DamageDoneOnce" || t.Mode == "DamageAll") && ev.Amount <= 0 {
					continue
				}
				key := triggerKey{Source: id, Idx: ti, Face: fc.faceIdx}
				// CR 508.1: declaring attackers is ONE turn-based action. The
				// engine emits one DeclareAttackers event per defending player,
				// but a BATCH "whenever you attack" trigger is ONE triggering
				// condition for the whole declaration, so latch it to the
				// (Turn, CombatsThisTurn) stamp of the declare step -- exactly
				// the way AttackerUnblockedOnce latches to one combat. The
				// stamp is committed at the queue point below, not here: the
				// gates between must be able to reject the line without
				// consuming the latch. Per-defender shapes
				// (AttackersDeclaredOneTarget, or AttackersDeclared carrying
				// AttackedTarget$) are never stamped and keep firing per
				// defender.
				if attackersDeclaredBatch(t) {
					stamp := combatFires{Turn: e.G.Turn, Combat: e.G.CombatsThisTurn}
					if e.attackersDeclaredFired[key] == stamp {
						continue // already fired this declare step.
					}
				}
				if e.triggerFireCount == nil {
					e.triggerFireCount = map[triggerKey]int32{}
				}
				if e.triggerFireCount[key] >= maxTriggerFires {
					continue // cascade bound: see maxTriggerFires.
				}
				if !e.triggerGameActivationLimitAllows(t, key) {
					continue // GameActivationLimit$: already triggered enough this game.
				}
				if actionTriggerModes[t.Mode] && !e.triggerActivationLimitAllows(t, key) {
					continue // ActivationLimit$: already triggered enough this turn.
				}
				// ResolvedLimit$ ("Do this only once each turn."): scoped to EVERY
				// trigger mode, not just actionTriggerModes -- ChangesZone and
				// SpellCast, the two largest groups, are not in that set. The
				// count is keyed by the SOURCE object so a card's paired lines
				// share one limit; a malformed value denies (fail closed).
				if limit, present := resolvedLimitValue(t); present {
					f := e.triggerTurnResolved[id]
					if f.Turn != e.G.Turn {
						f = turnFires{Turn: e.G.Turn}
					}
					if int(f.N) >= limit {
						continue // ResolvedLimit$: already resolved enough this turn.
					}
				}
				if t.Mode == "DamageDealtOnce" || t.Mode == "DamageDoneOnce" || t.Mode == "DamageAll" {
					// The "Once" gate latches once per DAMAGE BATCH, not per turn
					// (CR 510.4; Forge PhaseHandler.dealAssignedDamage fires
					// triggerDamageDoneOnce once per damage step, and one
					// dealDamage call's damage per batch): a double striker's
					// bearer triggers Jitte twice (two damage steps), and two
					// separate damage events in one turn trigger an Enrage creature
					// twice -- while two blockers hitting back at the same creature
					// in ONE batch trigger it once, with the referent carrying the
					// batch's accumulated total. Within an open batch the entry
					// below IS the latch (a second matching event accumulates into
					// it); no batch open means every Damage event is its own batch,
					// so there is nothing to latch across and the trigger fires per
					// event with its own event amount already the batch total.
					//
					// DamageAll (Forge GameAction.triggerDamageAll) is the
					// batch-level "one or more" mode: it must fire ONCE for the
					// whole batch if at least one matching SOURCE dealt damage to
					// at least one matching TARGET, not once per pair. Its latch
					// therefore keys on the trigger line ALONE (all=true, no
					// referent): damageMatches already requires BOTH ValidSource$
					// and ValidTarget$ to match the SAME event, so the first such
					// event queues the single instance and every later matching
					// pair in the batch accumulates into it -- the "one or more"
					// reading.
					// Non-positive amounts (the negative-amount Damage events the
					// cleanup/regeneration repair paths emit to clear marked
					// damage) are not damage and never latch or queue a Once
					// trigger.
					if ev.Amount > 0 {
						bk := damageBatchKey{triggerKey: key, dealt: t.Mode == "DamageDealtOnce", all: t.Mode == "DamageAll"}
						var allSrc state.ObjID
						var allTgt state.Target
						if bk.dealt {
							// Combat identifies the actual attacker/blocker in damaging;
							// an effect batch's shared source is its published override
							// or its resolving stack object otherwise. A watcher can
							// match several sources in one batch, so never collapse
							// non-combat sources onto ObjID zero. Same priority order as
							// the ValidSource$ match above: an explicit override always
							// wins, e.damaging is combat-only, damageSource is the
							// non-combat fallback. Through the ONE shared resolution
							// (damageEventSource) so the latch and the match agree.
							bk.obj = e.damageEventSource()
						} else if !bk.all {
							// DamageDoneOnce: the per-damaged-referent latch key.
							if ev.Obj != 0 {
								bk.obj = ev.Obj
							} else {
								bk.player = ev.Player
							}
						} else {
							// DamageAll's line-only key carries no referent; instead
							// the matching (source, target) pair of EVERY matching
							// event accumulates into the entry's deduplicated batch
							// sets, the capture the plural corpus readers resolve.
							// The source read is the ONE shared dealer resolution, so
							// a captured set can never contain a source the
							// ValidSource$ match did not match (the match and the
							// capture cannot drift).
							allSrc = e.damageEventSource()
							if ev.Obj != 0 {
								allTgt = state.Target{Obj: ev.Obj}
							} else {
								allTgt = state.Target{Player: ev.Player, IsPlayer: true}
							}
						}
						if e.damageBatchOpen {
							if e.damageBatchIdx == nil {
								e.damageBatchIdx = map[damageBatchKey]int{}
							}
							if entIdx, ok := e.damageBatchIdx[bk]; ok {
								ent := &e.damageBatchLog[entIdx]
								ent.amount += ev.Amount
								if bk.all {
									ent.sources = batchAppendSource(ent.sources, allSrc)
									ent.targets = batchAppendTarget(ent.targets, allTgt)
								}
								continue // already queued once for this batch and referent.
							}
							e.damageBatchIdx[bk] = len(e.damageBatchLog)
							ent := damageBatchEntry{
								key: bk, idx: len(e.pendingTriggers), amount: ev.Amount,
							}
							if bk.all {
								ent.sources = batchAppendSource(ent.sources, allSrc)
								ent.targets = batchAppendTarget(ent.targets, allTgt)
							}
							e.damageBatchLog = append(e.damageBatchLog, ent)
							// Fall through: the trigger queues now, at the same point
							// in the stream it queued at before this gate was
							// batch-scoped; closeDamageBatch patches its referent
							// amount to the batch total.
						}
					}
				}
				// ChangesZoneAll inside a RepeatEach ChangeZoneTable$ True loop is
				// the batch-level zone mode: the loop's accumulated zone changes
				// reach the trigger ONCE (Forge RepeatEachEffect's CardZoneTable
				// calling triggerChangesZoneAll after the loop), so it fires ONCE
				// for the whole batch if at least one matching move happened,
				// not once per move. PhaseOutAll (The War Doctor's "whenever one
				// or more other permanents phase out") shares the bracket: one
				// api:Phases resolution that phases out N permanents opens one
				// batch and the latency fires once. The latch keys on the trigger
				// line ALONE (DamageAll's shape): the first matching phase-out
				// queues the single instance and every later matching phase-out
				// in the batch accumulates its object into the entry's
				// deduplicated set, which closeZoneBatch patches into the queued
				// trigger's Remembered/Captured. No batch open (every ordinary
				// context) means every zone change is its own batch-of-one: the
				// mode queues per move exactly as before this gate existed, and
				// Mode$ ChangesZone is never batch-scoped at all.
				if (t.Mode == "ChangesZoneAll" || t.Mode == "PhaseOutAll") && e.zoneBatchOpen {
					if e.zoneBatchIdx == nil {
						e.zoneBatchIdx = map[zoneBatchKey]int{}
					}
					if entIdx, ok := e.zoneBatchIdx[zoneBatchKey{triggerKey: key}]; ok {
						ent := &e.zoneBatchLog[entIdx]
						if ev.Obj != 0 {
							ent.moved = batchAppendTarget(ent.moved, state.Target{Obj: ev.Obj})
						}
						continue // already queued once for this batch.
					}
					ent := zoneBatchEntry{key: key, idx: len(e.pendingTriggers)}
					if ev.Obj != 0 {
						ent.moved = batchAppendTarget(ent.moved, state.Target{Obj: ev.Obj})
					}
					e.zoneBatchIdx[zoneBatchKey{triggerKey: key}] = len(e.zoneBatchLog)
					e.zoneBatchLog = append(e.zoneBatchLog, ent)
					// Fall through: the trigger queues now; closeZoneBatch patches
					// its plural capture to the batch's moved set.
				}
				// MilledAll inside an open mill batch (one api:Mill resolution,
				// effects/cardflow.go's effMill) is the mill twin of the
				// ChangesZoneAll latch above: the batch's milled cards reach the
				// trigger ONCE, not once per card, so it fires ONCE for the whole
				// mill action if at least one matching card was milled. The latch
				// keys on the trigger line ALONE (DamageAll's shape); the first
				// matching milled card queues the single instance and every later
				// matching card accumulates into the entry's COUNT, which
				// closeMillBatch patches into the queued trigger's TriggerAmount
				// (the TriggerCount$Amount head The Wise Mothman's X and
				// Screeching Scorchbeast's "that many" read). Only cards this
				// line's ValidCard$ matched are counted, exactly as DamageAll
				// only accumulates matching pairs. No batch open (a mill that is
				// not an api:Mill, e.g. a cost mill) means every mill is its own
				// batch-of-one and the per-event referent below already carries
				// count 1.
				if t.Mode == "MilledAll" && e.millBatchOpen {
					if e.millBatchIdx == nil {
						e.millBatchIdx = map[triggerKey]int{}
					}
					if entIdx, ok := e.millBatchIdx[key]; ok {
						ent := &e.millBatchLog[entIdx]
						ent.amount++
						if ev.Obj != 0 {
							ent.milled = batchAppendTarget(ent.milled, state.Target{Obj: ev.Obj})
						}
						continue // already queued once for this mill action.
					}
					ent := millBatchEntry{key: key, idx: len(e.pendingTriggers), amount: 1}
					if ev.Obj != 0 {
						ent.milled = batchAppendTarget(ent.milled, state.Target{Obj: ev.Obj})
					}
					e.millBatchIdx[key] = len(e.millBatchLog)
					e.millBatchLog = append(e.millBatchLog, ent)
					// Fall through: the trigger queues now; closeMillBatch patches
					// its count and plural capture to the mill action's totals.
				}
				// DiscardedAll inside an open discard batch (one api:Discard
				// resolution, effects/cardflow.go's effDiscard) is the discard twin
				// of the MilledAll latch above: the batch's discarded cards reach
				// the trigger ONCE, not once per card, so it fires ONCE for the
				// whole discard action if at least one matching card was discarded.
				// The latch keys on the trigger line ALONE (DamageAll's shape);
				// the first matching discarded card queues the single instance and
				// every later matching card accumulates into the entry's COUNT,
				// which closeDiscardBatch patches into the queued trigger's
				// TriggerAmount (the TriggerCount$Amount head Magmakin
				// Artillerist's X reads). Only cards this line's ValidCard$
				// matched are counted, exactly as MilledAll only counts matching
				// cards and DamageAll only accumulates matching pairs.
				//
				// FirstTime$ True (Veronica's "for the first time each turn") is
				// the batch-level once-per-turn latch: a line that already queued
				// a batch this turn queues nothing for a later batch, and the
				// LATER batch is not accumulated into the earlier open entry
				// either (the check is before the entry lookup). It is read here,
				// not in the matcher, because batch identity lives in the latch;
				// firstMarkerThisTurn's per-EVENT scan cannot tell one batch from
				// the next. The stamp is the trigger line's turn only, exact for
				// every corpus carrier (their ValidPlayer$ is the source's own
				// controller).
				//
				// No batch open (a discard that is not an api:Discard, e.g. a cost
				// discard or a cleanup discard) means every discard is its own
				// batch-of-one and the per-event referent below already carries
				// count 1.
				if t.Mode == "DiscardedAll" {
					// FirstTime$ True (Veronica, Rielle: "for the first time each
					// turn") is the batch-level once-per-turn gate. It was read by
					// discardedAllMatches and captured above as
					// discardedAllFirstTime; it is decided when a NEW batch entry is
					// created and recorded then, so a later batch this turn queues
					// nothing while the SAME batch's further matching cards still
					// accumulate into the count (Rielle's "draw that many" needs a
					// multi-card first batch's full size). Batch identity lives in
					// the latch; firstMarkerThisTurn's per-EVENT scan cannot tell one
					// discard batch from the next. The stamp is the trigger line's
					// turn only, exact for every corpus carrier (their ValidPlayer$
					// is the source's own controller).
					firstTime := discardedAllFirstTime
					if e.discardBatchOpen {
						if entIdx, ok := e.discardBatchIdx[key]; ok {
							ent := &e.discardBatchLog[entIdx]
							ent.amount++
							if ev.Obj != 0 {
								ent.discarded = batchAppendTarget(ent.discarded, state.Target{Obj: ev.Obj})
							}
							continue // already queued once for this discard action.
						}
						if firstTime && e.discardAllTurn[key] == e.G.Turn {
							continue // FirstTime$: this line already fired a batch this turn.
						}
						if e.discardBatchIdx == nil {
							e.discardBatchIdx = map[triggerKey]int{}
						}
						ent := discardBatchEntry{key: key, idx: len(e.pendingTriggers), amount: 1}
						if ev.Obj != 0 {
							ent.discarded = batchAppendTarget(ent.discarded, state.Target{Obj: ev.Obj})
						}
						e.discardBatchIdx[key] = len(e.discardBatchLog)
						e.discardBatchLog = append(e.discardBatchLog, ent)
						if firstTime {
							if e.discardAllTurn == nil {
								e.discardAllTurn = map[triggerKey]int32{}
							}
							e.discardAllTurn[key] = e.G.Turn
						}
						// Fall through: the trigger queues now; closeDiscardBatch
						// patches its count and plural capture to the action's totals.
					} else {
						// No open batch (a cost or cleanup discard): this event is
						// its own batch-of-one and the referent below already carries
						// count 1. FirstTime$ is still enforced and recorded, so a
						// later single discard this turn is rejected too.
						if firstTime && e.discardAllTurn[key] == e.G.Turn {
							continue // FirstTime$: already fired this turn.
						}
						if firstTime {
							if e.discardAllTurn == nil {
								e.discardAllTurn = map[triggerKey]int32{}
							}
							e.discardAllTurn[key] = e.G.Turn
						}
					}
				}
				// RolledDie's Number$ N ("your third die each turn"): gated
				// LAST, at the queue point, so a speculative matcher call or a
				// later-rejected trigger never advances the count. Keyed by the
				// trigger line, so each roll trigger counts its own dice.
				// A line whose Execute$ never resolved (t.Effect == nil) can
				// never run, so it keeps no die count at all.
				if t.Mode == "RolledDie" && t.Effect != nil && !e.dieRollNumberAllows(t, key) {
					continue
				}
				e.triggerFireCount[key]++
				if t.Effect == nil {
					// Execute$ named an SVar this face never defined (or one
					// that failed to parse): the trigger matched, but there is
					// nothing to run.
					continue
				}
				// Mode$ BecomesTargetOnce (Forge TriggerBecomesTargetOnce): the
				// "whenever one or more ... become the target" BATCH reading of
				// BecomesTarget, so one targeting action fires the line once even
				// when it names several matching targets. The target batch is
				// open for exactly the TargetsChosen events recordChosenTargets
				// emits; the first matching event queues, every later matching
				// event of the same batch is absorbed. Outside an open batch (a
				// hand-built fixture emit) every event is its own batch-of-one,
				// the DamageAll/ChangesZoneAll reading of a missing bracket.
				// Gated at the queue point, after every later-rejected gate, so
				// a matched-but-unqueueable event does not consume the batch.
				if t.Mode == "BecomesTargetOnce" && e.targetBatchOpen {
					if e.targetBatchFired[key] {
						// Already queued once for this targeting action. Note the
						// triggerFireCount bump above has already run for this
						// absorbed event; that counter is only the maxTriggerFires
						// runaway guard, so an off-by-one per batch is harmless and
						// must not be "fixed" into a behavioural change.
						continue
					}
					if e.targetBatchFired == nil {
						e.targetBatchFired = map[triggerKey]bool{}
					}
					e.targetBatchFired[key] = true
				}
				// GameActivationLimit$/ActivationLimit$ counts commit HERE, at the
				// queue point, after every later-rejected gate (a nil Execute$ body,
				// the Damage batch latch, the RolledDie Number$ gate): a matched
				// event that ends up queueing nothing must not consume a use of
				// either limit, and a combined-parameter line whose second same-turn
				// match the per-turn ActivationLimit$ rejects must keep its other
				// game use for the next turn.
				e.reserveTriggerLimits(t, key)
				if attackersDeclaredBatch(t) {
					if e.attackersDeclaredFired == nil {
						e.attackersDeclaredFired = map[triggerKey]combatFires{}
					}
					e.attackersDeclaredFired[key] = combatFires{Turn: e.G.Turn, Combat: e.G.CombatsThisTurn}
				}
				// CR 603.3a/603.10a: a leaves-the-battlefield ability's source
				// is controlled by whoever controlled it as it left, not by the
				// owner the move has since reset it to (a stolen creature's own
				// dies trigger belongs to the player who stole it).
				controller := o.Controller
				if objLKI != nil && id == ev.Obj && leftBattlefield(ev) {
					controller = objLKI.Controller
				}
				// TriggerController$ TriggeredCardController assigns the
				// ChangesZone ability to the controller of the card whose move
				// caused it. For a permanent leaving the battlefield, use that
				// event's LKI: the live object has already returned to its owner.
				if (t.Mode == "ChangesZone" || t.Mode == "ChangesZoneAll") &&
					t.Params["TriggerController"] == "TriggeredCardController" &&
					objLKI != nil && leftBattlefield(ev) {
					controller = objLKI.Controller
				}
				// The non-active face of an unlocked Room must be minted through
				// the delayed-shape push: TriggerPush re-derives an ability from
				// the object's active Face(), while the delayed push resolves the
				// other face's Execute$ SVar directly. This is independent of
				// whether CR 309.4b cast face 0 or face 1.
				//
				// A MERGED under-card face (CR 702.140d) must NOT take that
				// by-name path: resolveSVarAcrossFaces resolves top-face-first,
				// so the pile's top card -- which can be ANY creature, and whose
				// own token triggers canonically name their SVar TrigToken --
				// would steal the under-card's body whenever both faces define
				// the same name (Cubwarden mutated under Everquill Phoenix: the
				// under-card's "create two Cats" resolved to the top Phoenix's
				// "create a Feather"). MergedTriggerPush instead carries the
				// under-card's pile index so events.Apply resolves the name
				// against THAT face's own SVar table.
				pt := pendingTrigger{
					Source:     id,
					Controller: controller,
					Idx:        ti,
					SA:         t.Effect,
					Ctx: effects.Ctx{
						Source:         id,
						Controller:     controller,
						Remembered:     e.triggerRememberedFor(t, ev, id),
						Captured:       e.triggerRememberedFor(t, ev, id),
						LKI:            objLKI,
						LKIPower:       lkiPower,
						LKIToughness:   lkiToughness,
						LKIPTValid:     objLKI != nil && lkiPTValid,
						TriggerContext: observer.triggerReferents(t, id, ev, objLKI),
					},
				}
				switch {
				case fc.merged > 0:
					pt.Merged = fc.merged
					pt.Execute = t.Params["Execute"]
				case !fc.active:
					pt.Delayed = true
					pt.DelayedID = ^uint32(0)
					pt.Execute = t.Params["Execute"]
				}
				if t.Mode == "Exploited" {
					// CR 702.58c's "that creature" is the exploited one, not the
					// marker's causing object (the exploiter), so its LKI P/T -- not
					// the exploiter's -- is what TriggeredExploited$CardPower and
					// CardToughness must read.
					e.attachExploitedLKI(&pt, ev)
				}
				e.pendingTriggers = append(e.pendingTriggers, pt)
				// stat:Panharmonicon (CR 702.109): "If a triggered ability of a
				// ... permanent you control triggers, that ability triggers an
				// additional time." Each battlefield Panharmonicon-shaped static
				// whose ValidCard$ matches the triggering object appends ONE extra
				// copy of this trigger immediately after the original, in scan
				// order -- deterministic, and the extra copy is an ordinary queue
				// entry that resolves like any other (the doubling does not fire on
				// the copy again: the copy is not an event). Panharmonicon's own
				// trigger is excluded by the spec's Other predicate, which is
				// relative to the Panharmonicon permanent itself.
				for k := 0; k < e.panharmoniconEchoes(observer, id, ev); k++ {
					e.pendingTriggers = append(e.pendingTriggers, pt)
				}
			}
		}
		if len(grantedStatics) > 0 {
			e.checkGrantedStaticTriggersUsing(observer, grantedStatics, id, o, ev, objLKI, lkiPower, lkiToughness, lkiPTValid, split, leaving)
		}
		// A granted Afflict must fire even when the object's own printed
		// triggers are live for this event (a Zombie with its own become-blocked
		// trigger carrying the Monarch's grant) -- the early-return path above
		// reaches this object through checkGrantedAfflictTriggers's own call.
		e.checkGrantedAfflictTriggers(id, o, f, ev)
		// A granted Conspire must fire even when the object's own printed
		// triggers are live for this event (a spell with its own cast trigger
		// carrying a Conspire grant) -- the early-return path above reaches this
		// object through checkGrantedConspireTriggers's own call.
		e.checkGrantedConspireTriggers(observer, id, o, f, ev, objLKI)
		// A granted Demonstrate must fire even when the object's own printed
		// triggers are live for this event (a spell with its own cast trigger
		// carrying a Demonstrate grant) -- the same both-paths rule Conspire
		// follows.
		e.checkGrantedDemonstrateTriggers(observer, id, o, f, ev, objLKI)
		// A granted Exploit must fire when its creature enters even when the
		// object's own printed triggers are live for this event -- the same
		// both-paths rule Afflict and Conspire follow.
		e.checkGrantedExploitTriggers(observer, id, o, f, ev, objLKI)
		// A granted Offspring must fire when its creature enters even when the
		// object's own printed triggers are live for this event -- the same
		// both-paths rule Afflict, Conspire, Exploit and Training follow.
		e.checkGrantedOffspringTriggers(observer, id, o, f, ev, objLKI)
		// A granted Training must fire even when the object's own printed
		// triggers are live for this event (an attacking token with its own
		// trigger carrying the training grant) -- the early-return path above
		// reaches this object through checkGrantedTrainingTriggers's own call.
		e.checkGrantedTrainingTriggers(observer, id, o, f, ev, objLKI)
		e.checkGrantedMeleeTriggers(observer, id, o, f, ev, objLKI)
		// A granted Mentor must fire even when the object's own printed
		// triggers are live for this event -- the same both-paths rule
		// Afflict, Conspire, Exploit, Offspring and Training follow.
		e.checkGrantedMentorTriggers(observer, id, o, f, ev, objLKI)
		// A granted cumulative upkeep must fire at the beginning of the
		// controller's upkeep even when the object's own printed triggers are
		// live for this step change -- the same both-paths rule Afflict,
		// Conspire, Exploit, Offspring and Training follow.
		e.checkGrantedCumulativeUpkeepTriggers(observer, id, o, f, ev, objLKI)
	}
	// The live walk skips a hidden zone none of whose objects can act on
	// any event (rules/trigger_zoneskip.go); the look-back observer and any
	// event a static-granted trigger observes walk everything.
	skip := observer == e && len(grantedStatics) == 0
	var verify func(state.ObjID)
	if skip && trigZoneSkipVerify {
		verify = e.trigSkipVerifier(ev, visit, func() int { return len(phaseNotes) })
	}
	observer.forEachTriggerObject(ev, skip, visit, verify)
	for _, n := range phaseNotes {
		e.emit(events.Event{Kind: events.Note, Obj: n.id,
			Text: "Phase$ " + n.spec + " names no engine step; the trigger never fires"})
	}
}

// triggerFace is one face's trigger walk: its printed index keys fire-count
// memory, and active says whether TriggerPush can re-derive it through the
// object's current Face() (the other unlocked Room face must use a delayed
// shape because Apply cannot select it). merged is 0 for every real face;
// a MERGED under-card face carries its pile index PLUS ONE so the zero
// value can never alias pile index 0 (a forgotten field must always mean
// "not merged", never "the first under-card").
type triggerFace struct {
	face    *cards.Face
	faceIdx uint8
	active  bool
	merged  int
}

// roomTriggerFaces returns the faces whose Triggers a scan walks for object
// o: its cast face always, plus the other face once unlocked (CR 309.6).
// FaceIdx need not be zero: CR 309.4b permits casting either Room door.
func roomTriggerFaces(o *state.Object, active *cards.Face) ([2]triggerFace, int) {
	// At most two faces, returned by value so the ordinary single-face walk
	// never allocates a backing slice per object per event. merged is left 0
	// ("not merged") on both.
	out := [2]triggerFace{{face: active, faceIdx: o.FaceIdx, active: true}}
	if o.Unlocked && isRoom(o) && len(o.Card.Faces) == 2 && int(o.FaceIdx) < len(o.Card.Faces) {
		other := uint8(1 - int(o.FaceIdx))
		out[1] = triggerFace{face: o.Card.Faces[other], faceIdx: other}
		return out, 2
	}
	return out, 1
}

// triggerFacesWithMerged extends the room faces with every card stacked
// beneath a mutated permanent's top card (CR 702.140d's "all abilities of
// the cards beneath it"). It allocates only for a mutated pile, so the
// ordinary trigger scan keeps roomTriggerFaces' allocation-free fast path.
// A merged face is marked active=false AND carries its pile index (+1, the
// encoding triggerFace.merged documents): the queue routes it through the
// MergedTriggerPush push, which carries the pile index so events.Apply
// resolves the under-card's Execute$ SVar against THAT face's table -- the
// top face's same-named SVar must never steal the body. faceIdx 2+i sits
// outside the two real card-face slots so its triggerKey/fire-count entries
// never collide with the top card's; a pile big enough to overflow the byte
// (254+ merged cards -- no real game reaches it; each merged card is one
// full mutate cast and resolution) stops being walked rather than colliding.
func triggerFacesWithMerged(o *state.Object, base []triggerFace) []triggerFace {
	out := make([]triggerFace, 0, len(base)+len(o.MergedCards))
	out = append(out, base...)
	for i := range o.MergedCards {
		if 2+i > 255 {
			break
		}
		if face := o.MergedFaceAt(i); face != nil {
			out = append(out, triggerFace{face: face, faceIdx: uint8(2 + i), merged: i + 1})
		}
	}
	return out
}

// openDamageBatch opens a damage batch: the Damage events emitted until the
// matching closeDamageBatch are one simultaneous batch for the
// DamageDealtOnce/DamageDoneOnce latch (CR 510.4; Forge dealAssignedDamage).
// Reentrant brackets belong to the same simultaneous batch: depth makes an
// inner close consume only its own begin, so it cannot close the outer batch
// early.
func (e *Engine) openDamageBatch() {
	if e.damageBatchDepth == 0 {
		e.damageBatchOpen = true
		e.damageBatchIdx = nil
		e.damageBatchLog = nil
	}
	e.damageBatchDepth++
}

// closeDamageBatch closes the open damage batch: every entry's queued trigger
// gets its referent patched to the batch's accumulated total (two blockers
// hitting one Enrage creature is ONE trigger whose amount is the sum), then
// the batch bookkeeping is dropped. The Once latch lives entirely inside the
// open batch -- entries are the latch, and closing clears them -- so nothing
// persists between batches and no per-turn latch remains. The pendingTriggers
// index recorded at queue time is re-checked against the trigger it was
// recorded for before patching; a mismatch (impossible today -- the queue is
// append-only while a batch is open, and nothing drains mid-batch) falls back
// to the first event's amount rather than patching a stranger.
func (e *Engine) closeDamageBatch() {
	if e.damageBatchDepth == 0 {
		return
	}
	e.damageBatchDepth--
	if e.damageBatchDepth != 0 {
		return
	}
	e.damageBatchOpen = false
	for _, ent := range e.damageBatchLog {
		if ent.idx >= len(e.pendingTriggers) {
			continue
		}
		// Every queued copy of this (trigger, referent) entry is patched: the
		// Panharmonicon echo copies appended right after the original share
		// the queue slot sequence and would otherwise resolve a partial
		// amount and no batch sets.
		for i := ent.idx; i < len(e.pendingTriggers); i++ {
			pt := &e.pendingTriggers[i]
			if pt.Source != ent.key.Source || pt.Idx != ent.key.Idx {
				break
			}
			pt.Ctx.TriggerContext.TriggerAmount = ent.amount
			if ent.key.all {
				if len(ent.sources) > 0 {
					pt.Ctx.TriggerContext.TriggerDamageSources = append([]state.ObjID(nil), ent.sources...)
				}
				if len(ent.targets) > 0 {
					pt.Ctx.TriggerContext.TriggerDamageTargets = append([]state.Target(nil), ent.targets...)
				}
			}
		}
	}
	e.damageBatchIdx = nil
	e.damageBatchLog = nil
}

// batchAppendSource appends id to a DamageAll entry's deduplicated source
// set unless it is already present (or zero -- an unresolved dealer is
// nothing to name). First-seen order is the set's order.
func batchAppendSource(ts []state.ObjID, id state.ObjID) []state.ObjID {
	if id == 0 {
		return ts
	}
	for _, have := range ts {
		if have == id {
			return ts
		}
	}
	return append(ts, id)
}

// batchAppendTarget appends t to a DamageAll entry's deduplicated target set
// unless an equal target is already present. Equality compares kind and
// identity together, so a player target and an object target never merge.
func batchAppendTarget(ts []state.Target, t state.Target) []state.Target {
	for _, have := range ts {
		if have.IsPlayer == t.IsPlayer && have.Player == t.Player && have.Obj == t.Obj {
			return ts
		}
	}
	return append(ts, t)
}

// BeginDamageBatch/EndDamageBatch are effects.Host's damage-batch bracket
// (effects/damage.go calls them around each dealDamage-style call); they are
// openDamageBatch/closeDamageBatch on the engine. See there.
func (e *Engine) BeginDamageBatch() { e.openDamageBatch() }
func (e *Engine) EndDamageBatch()   { e.closeDamageBatch() }

// BeginZoneBatch/EndZoneBatch are the zone twin of the damage bracket above:
// effects/choose_control.go's effRepeatEach opens them around a whole
// RepeatEach loop whose ChangeZoneTable$ True asks for the batch. See
// openZoneBatch/closeZoneBatch.
func (e *Engine) BeginZoneBatch() { e.openZoneBatch() }
func (e *Engine) EndZoneBatch()   { e.closeZoneBatch() }

// BeginMillBatch/EndMillBatch are effects.Host's mill-batch bracket (effects/
// cardflow.go's effMill opens them around one api:Mill resolution): the mill
// MoveZone events emitted until the matching EndMillBatch are one mill action
// for the Mode$ MilledAll latch. Reentrant brackets nest by depth so an inner
// mill cannot close its caller's batch early.
func (e *Engine) BeginMillBatch() { e.openMillBatch() }
func (e *Engine) EndMillBatch()   { e.closeMillBatch() }

// openMillBatch opens a mill batch: the mill MoveZone events emitted until
// the matching closeMillBatch are one mill action for Mode$ MilledAll.
func (e *Engine) openMillBatch() {
	if e.millBatchDepth == 0 {
		e.millBatchOpen = true
		e.millBatchIdx = nil
		e.millBatchLog = nil
	}
	e.millBatchDepth++
}

// closeMillBatch closes the open mill batch: every entry's queued trigger
// gets its TriggerAmount patched to the count of matching cards and its
// Remembered/Captured plural capture to their set, then the bookkeeping is
// dropped. The latch lives entirely inside the open batch -- entries are the
// latch, and closing clears them -- so nothing persists between mills. The
// pendingTriggers index is re-checked against the trigger it was recorded for
// before patching (same guard as closeZoneBatch/closeDamageBatch: a trigger
// whose Execute$ never resolved queued nothing).
func (e *Engine) closeMillBatch() {
	if e.millBatchDepth == 0 {
		return
	}
	e.millBatchDepth--
	if e.millBatchDepth != 0 {
		return
	}
	e.millBatchOpen = false
	for _, ent := range e.millBatchLog {
		if ent.idx >= len(e.pendingTriggers) {
			continue
		}
		for i := ent.idx; i < len(e.pendingTriggers); i++ {
			pt := &e.pendingTriggers[i]
			if pt.Source != ent.key.Source || pt.Idx != ent.key.Idx {
				break
			}
			pt.Ctx.TriggerContext.TriggerAmount = ent.amount
			if len(ent.milled) > 0 {
				pt.Ctx.Remembered = append([]state.Target(nil), ent.milled...)
				pt.Ctx.Captured = append([]state.Target(nil), ent.milled...)
			}
		}
	}
	e.millBatchIdx = nil
	e.millBatchLog = nil
}

// BeginDiscardBatch/EndDiscardBatch are effects.Host's discard-batch bracket
// (effects/cardflow.go's effDiscard opens them around one api:Discard
// resolution): the discard MoveZone events emitted until the matching
// EndDiscardBatch are one discard action for the Mode$ DiscardedAll latch.
// Reentrant brackets nest by depth so an inner discard (a rider, or a
// SubAbility$ reachable from the resolving body) cannot close its caller's
// batch early.
func (e *Engine) BeginDiscardBatch() { e.openDiscardBatch() }
func (e *Engine) EndDiscardBatch()   { e.closeDiscardBatch() }

// openDiscardBatch opens a discard batch: the discard MoveZone events emitted
// until the matching closeDiscardBatch are one discard action for Mode$
// DiscardedAll. Unlike the mill bracket, api:Discard's resolution may suspend
// mid-action for a player's choice, so effDiscard opens this on the FIRST pass
// and closes it only on the pass that completes without suspending; a
// re-entrant (resumed) pass must not open a second batch for the action it is
// continuing.
func (e *Engine) openDiscardBatch() {
	if e.discardBatchDepth == 0 {
		e.discardBatchOpen = true
		e.discardBatchIdx = nil
		e.discardBatchLog = nil
	}
	e.discardBatchDepth++
}

// closeDiscardBatch closes the open discard batch: every entry's queued
// trigger gets its TriggerAmount patched to the count of matching cards and
// its Remembered/Captured plural capture to their set, then the bookkeeping
// is dropped. The latch lives entirely inside the open batch -- entries are
// the latch, and closing clears them -- so nothing persists between discard
// actions. The pendingTriggers index is re-checked against the trigger it was
// recorded for before patching (the same guard as closeMillBatch:
// pendingTriggers is append-only while a batch is open, so a trigger whose
// Execute$ never resolved queued nothing).
func (e *Engine) closeDiscardBatch() {
	if e.discardBatchDepth == 0 {
		return
	}
	e.discardBatchDepth--
	if e.discardBatchDepth != 0 {
		return
	}
	e.discardBatchOpen = false
	for _, ent := range e.discardBatchLog {
		if ent.idx >= len(e.pendingTriggers) {
			continue
		}
		for i := ent.idx; i < len(e.pendingTriggers); i++ {
			pt := &e.pendingTriggers[i]
			if pt.Source != ent.key.Source || pt.Idx != ent.key.Idx {
				break
			}
			pt.Ctx.TriggerContext.TriggerAmount = ent.amount
			if len(ent.discarded) > 0 {
				pt.Ctx.Remembered = append([]state.Target(nil), ent.discarded...)
				pt.Ctx.Captured = append([]state.Target(nil), ent.discarded...)
			}
		}
	}
	e.discardBatchIdx = nil
	e.discardBatchLog = nil
}

// openZoneBatch opens a zone batch: the zone-change events (MoveZone,
// Draw, PutOnStack -- the kinds zoneChangeMatches consults) emitted until
// the matching closeZoneBatch are one simultaneous batch for ChangesZoneAll.
// Reentrant brackets belong to the same nested-batch discipline as the
// damage batch: depth makes an inner close consume only its own begin. A
// ChangeZoneTable loop that suspends mid-body leaves the bracket open
// across the resume -- the re-entry pass re-enters with the same SA on the
// same engine and closes it when the loop completes, so the bracket is
// balanced however many resumes interleave.
func (e *Engine) openZoneBatch() {
	if e.zoneBatchDepth == 0 {
		e.zoneBatchOpen = true
		e.zoneBatchIdx = nil
		e.zoneBatchLog = nil
	}
	e.zoneBatchDepth++
}

// closeZoneBatch closes the open zone batch: every entry's queued trigger
// gets its Remembered/Captured patched to the batch's deduplicated moved set
// (the plural capture), then the batch bookkeeping is dropped. The latch
// lives entirely inside the open batch -- entries are the latch, and closing
// clears them -- so nothing persists between batches. The pendingTriggers
// index recorded at queue time is re-checked against the trigger it was
// recorded for before patching (same guard as closeDamageBatch: a trigger
// whose Execute$ never resolved queued nothing, so a stale index must not
// patch a stranger).
func (e *Engine) closeZoneBatch() {
	if e.zoneBatchDepth == 0 {
		return
	}
	e.zoneBatchDepth--
	if e.zoneBatchDepth != 0 {
		return
	}
	e.zoneBatchOpen = false
	for _, ent := range e.zoneBatchLog {
		if ent.idx >= len(e.pendingTriggers) {
			continue
		}
		for i := ent.idx; i < len(e.pendingTriggers); i++ {
			pt := &e.pendingTriggers[i]
			if pt.Source != ent.key.Source || pt.Idx != ent.key.Idx {
				break
			}
			if len(ent.moved) > 0 {
				pt.Ctx.Remembered = append([]state.Target(nil), ent.moved...)
				pt.Ctx.Captured = append([]state.Target(nil), ent.moved...)
			}
		}
	}
	e.zoneBatchIdx = nil
	e.zoneBatchLog = nil
}

// triggerRemembered is what a matched trigger's Ctx.Remembered holds: the
// object the triggering event was actually about (the card that changed
// zones, was cast, or is being targeted), or -- for an event with no object
// of its own, such as a step change, or a player-only Damage event -- the
// trigger's own source. Most of the eight M1 modes' Execute$ abilities in
// the acceptance deck don't read Remembered at all (they use Defined$
// Self/You), so this is deliberately one simple, general rule rather than a
// mode-specific one -- except DeclareAttackers, which carries every attacker
// declared against one defending player in Event.IDs rather than a single
// Event.Obj (see attacksMatches): Remembered there is every declared
// attacker, in order, followed by one more entry for that event's defending
// player. ev.Player is set by handleAttackers (rules/combat.go); since Task
// m34 an attack may split across several defenders, so the engine emits ONE
// DeclareAttackers event per defending player and each event's Player is
// that event's own defender -- a trigger matching one of its attackers
// resolves TriggeredDefendingPlayer against the opponent that creature is
// actually attacking, the same value a two-player game always produced.
//
// effects.context.go's Defined already recognises Defined$
// TriggeredDefendingPlayer/TriggeredPlayer (Task 5's playersOf(Remembered),
// merged after this task started). The trailing entry only matters once it
// survives onto the stack, which needed FL-41's fix: pushTrigger
// (trigger_queue.go) could not just write tgt.Obj for this entry into the
// logged TriggerPush event's IDs (always 0 for a player target) -- Apply
// would rebuild it as {Obj: 0}, and playersOf (effects/context.go) filters
// that entry out, so Defined$ TriggeredDefendingPlayer resolves to nothing
// and the effect silently no-ops (PlayerOf is never reached). state.PlayerRef
// is what lets a player reference survive that log-and-replay round trip
// intact.
//
// The one acceptance-game behaviour this trailing entry actually changes is
// Knight of Infamy's intrinsic Exalted keyword trigger (cards/keywords.go
// expands it to Mode$ Attacks | ValidCard$ Creature.YouCtrl | Alone$ True),
// which fires twice in the 4-seat game (mono-black-aggro is dealt in at
// seat 3 there). Exalted's DB$ Pump | Defined$ TriggeredAttacker resolves
// through effects/context.go's objectsOf(c.Remembered): Remembered is every
// declared attacker (plus the defending player). On top of FL-48 (task 16),
// attacksMatches honours Alone$ True, so the trigger now fires only when
// exactly one attacker is declared and pumps that single attacker -- which is
// the measured cause of the four-seat chain-head move 81a8a100641b5442's
// successor (see commit 75be2a3's merge note); before FL-48, Remembered listed
// several attackers and attacksMatches, ignoring Alone$, pumped every one.
// Goblin Guide and Goblin Piledriver never fire in the 8-seat game, and
// Ulamog is in the tron deck, which is never dealt at 2/4/6/8 seats: neither
// is a cause.
func triggerRemembered(ev events.Event, source state.ObjID) []state.Target {
	if ev.Kind == events.DeclareAttackers {
		out := make([]state.Target, 0, len(ev.IDs)+1)
		for _, id := range ev.IDs {
			out = append(out, state.Target{Obj: id})
		}
		return append(out, state.Target{Player: ev.Player, IsPlayer: true})
	}
	// Mode$ Unattached's referent is the FORMER BEARER, not the event's Obj
	// (the attachment that became unattached): the corpus's Execute bodies read
	// Defined$ TriggeredObjectLKICopy (Grafted Exoskeleton's sacrifice, the
	// whole cycle), and on the bearer-leaves path that object is already off
	// the battlefield. Carrying it as Remembered is what lets the spellings in
	// effects/context.go's TriggeredObjectLKICopy case resolve it -- whether it
	// is still a permanent (the under-protection/no-longer-a-creature detach)
	// or a graveyard LKI reference (the bearer-left detach). An emit with no
	// bearer (the zero-value event a test might build) has nothing to bind, so
	// it falls through to the ordinary source fallback below.
	if ev.Kind == events.Unattached && len(ev.IDs) > 0 {
		return []state.Target{{Obj: ev.IDs[0]}}
	}
	if ev.Obj != 0 {
		return []state.Target{{Obj: ev.Obj}}
	}
	return []state.Target{{Obj: source}}
}

// resolvingRemembered is the Remembered set a resolving ability object's
// effect chain starts from: the object's own (the trigger capture, a
// copied/pushed list), except for a printed Mode$ Phase trigger. A step
// change carries no object, so triggerRemembered's last fallback captured
// the trigger's SOURCE as its "remembered" card -- and Forge's Remembered in
// a phase trigger is the host card's own remembered list, never the host.
// Read literally, every phase trigger whose body names Remembered acted on
// its own source: Tombstone Stairwell's "at the beginning of each end step,
// destroy all tokens created with it" (DestroyAll Card.IsRemembered)
// destroyed the Stairwell itself every end step (cardfuzz coverage audit:
// its upkeep token trigger had never once resolved in ~200 casts). The
// capture itself -- the logged TriggerPush IDs -- is unchanged, so no event
// moves; only the resolution reads the source's durable Remembered instead.
func (e *Engine) resolvingRemembered(o *state.Object) []state.Target {
	if o.Card != nil || o.Ability == nil || len(o.Remembered) != 1 || o.Remembered[0].IsPlayer || o.Remembered[0].Obj != o.Source {
		return o.Remembered
	}
	t, _, ok := e.findTriggerForAbilityFace(o.Source, o.Ability)
	if !ok || t.Mode != "Phase" {
		return o.Remembered
	}
	src := e.G.Obj(o.Source)
	if src == nil {
		return nil
	}
	return append([]state.Target(nil), src.Remembered...)
}

// triggerRememberedFor is triggerRemembered with the one mode-aware override a
// BATCH trig:AttackersDeclared line needs: because the declaration is one
// turn-based action (CR 508.1) whose events are grouped per defender, a batch
// "whenever you attack" trigger's Remembered -- and so Defined$
// TriggeredAttackers, which effects/context.go resolves from it -- must name
// EVERY declared attacker, not just the defenders of the event that happened
// to latch the trigger. Engine.declaredAttackers is that whole set; an empty
// scratch (a direct synthetic emit) falls through to the ordinary event
// shapes. Every other mode, and every per-defender attack trigger, is
// unchanged.
func (e *Engine) triggerRememberedFor(t cards.Trigger, ev events.Event, source state.ObjID) []state.Target {
	if ev.Kind == events.DeclareAttackers && t.Params["Keyword"] == "Melee" {
		return e.meleeRemembered(ev)
	}
	if ev.Kind == events.DeclareAttackers && attackersDeclaredBatch(t) && len(e.declaredAttackers) > 0 {
		out := make([]state.Target, 0, len(e.declaredAttackers)+1)
		for _, id := range e.declaredAttackers {
			out = append(out, state.Target{Obj: id})
		}
		return append(out, state.Target{Player: ev.Player, IsPlayer: true})
	}
	return triggerRemembered(ev, source)
}

// trigMatcher answers whether one trigger fires for ev. lki is the event's LKI
// snapshot (the moving object as it was a moment ago); a matcher that does not
// need it ignores the parameter. The uniform signature is what lets every mode
// live behind one table.
type trigMatcher func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool

// trigMatchers maps Mode$ to its matcher. A mode with no entry never fires,
// which is exactly what the switch this replaced did by falling off its end
// with matched still false -- there was no default arm, and there must not be
// one now.
var trigMatchers = map[string]trigMatcher{}

// registerTrigMatcher installs fn for each named mode. Called from init() in
// the per-mode trigmatch_*.go files.
//
// A duplicate registration panics rather than silently replacing. The whole
// point of the split is that many tickets edit different files at once; two
// files claiming one mode is the merge accident that costs, so it must be loud
// at startup and not a matcher that quietly stopped being reached.
func registerTrigMatcher(fn trigMatcher, modes ...string) {
	for _, mode := range modes {
		if _, dup := trigMatchers[mode]; dup {
			panic("rules: duplicate trigger matcher registered for Mode$ " + mode)
		}
		trigMatchers[mode] = fn
	}
}

// triggerMatches decides whether one cards.Trigger fires for ev. lki is the
// event's LKI snapshot (the moving object as it was a moment ago), threaded
// to a ChangesZone trigger so its ValidCard$ -- a "dies" condition such as
// Undying's counters_EQ0_P1P1 -- can see the object as it was before Move
// reset it, not the live object already in the destination zone.
func (e *Engine) triggerMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	return e.triggerMatchesWithSVars(t, source, ev, lki, nil)
}

// triggerMatchesWithSVars is triggerMatches with the trigger LINE's owning SVar
// table supplied explicitly. It exists for the granted-trigger paths, whose
// trigger line (and every SVar its CheckSVar$/SVarCompare$ condition names)
// belongs to the GRANTOR, not to the affected recipient that source names: a
// cross-object AddTrigger$ (level_up's Aura granting its enchanted creature an
// attacking trigger carrying "CheckSVar$ X") must evaluate that condition
// against the grantor's table, the same table events.Apply resolves the body
// from. A nil ownedSVars is every printed-trigger call and keeps the
// printed-face walk byte-for-byte (see triggerConditionHoldsWithSVars).
func (e *Engine) triggerMatchesWithSVars(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object, ownedSVars map[string]string) bool {
	// The scanner has already run its diagnostic/batch gates. Reject an
	// impossible event before consulting dynamic zone and phase predicates.
	if !triggerModeEvents(t.Mode).allows(ev.Kind) {
		return false
	}
	if !e.zoneGate(t, source, ev) || !e.phaseGate(t) {
		return false
	}
	// NotThisAbility$ True (task nta1): the event being matched must not
	// have been caused by the resolution of THIS very trigger instance
	// (Kodama of the East Tree -- the corpus's one carrier, 1 of 38000+ card
	// files -- is the shape: its "whenever another permanent you control
	// enters, if it wasn't put onto the battlefield with this ability"
	// rider would otherwise re-fire on the permanent its own Execute$
	// ChangeZone just placed, a self-feeding loop). The cause is read the
	// way every synchronous action matcher reads it -- actionCause(), the
	// resolving wrapper at the top of the stack inside emit -- and compared
	// by identity: the wrapper's Source is the trigger's source permanent
	// and its Ability pointer is the exact compiled trigger line (the
	// pointer TriggerPush mints the wrapper from), so two copies of the
	// same card distinguish correctly (each wrapper names ITS source) and
	// a sibling trigger line of the same card does not suppress its
	// neighbour. The comparison is replay-safe with no event-schema
	// change: trigger checks run synchronously inside emit, and a replay
	// re-derives the same stack and the same compiled pointers. A cause
	// that cannot be identified (no resolving wrapper, a keyword-granted
	// trigger whose Effect is re-synthesized per scan) fails OPEN -- the
	// trigger fires, the pre-fix behaviour -- so the gate only ever
	// narrows when it can PROVE the cause is this ability.
	if e.notThisAbilityExcludes(t, source) {
		return false
	}
	var matched bool
	if fn := trigMatchers[t.Mode]; fn != nil {
		matched = fn(e, t, source, ev, lki)
	}
	if !matched {
		return false
	}
	// FirstCombat$ True -- the "if it's the first combat phase of the turn"
	// trigger gate (8 corpus T: lines: hexplate_wallbreaker, genji_glove,
	// finest_hour, balthier_and_fran, raph_leo_sibling_rivals on Mode$ Attacks,
	// karlach_fury_of_avernus on Mode$ AttackersDeclared,
	// zariel_archduke_of_avernus and swinging_ship on Mode$ Phase) restricts
	// the trigger to the FIRST combat phase begun this turn, so a trigger an
	// extra combat (DB$ AddPhase) creates must not re-fire. The count is the
	// event-folded state.Game.CombatsThisTurn (one increment per BeginCombat
	// entry, reset at TurnChange), evaluated on the fold of the matching
	// event: during the first combat it is 1 (its BeginCombat already
	// folded), and every extra combat's BeginCombat has raised it to 2 -- so
	// the test is count == 1, never 0. This is a SHARED gate in
	// triggerMatches rather than a per-mode one because the key rides three
	// different modes and means the same thing on every one; it is distinct
	// from the Execute-side ConditionFirstCombat$ gate
	// (effects/conditions.go, Raiyuu), which suppresses the BODY, not the
	// trigger.
	if strings.EqualFold(strings.TrimSpace(t.Params["FirstCombat"]), "True") && e.G.CombatsThisTurn != 1 {
		return false
	}
	// PlayerTurn$ True: only during the turn of the source's controller
	// (Forge Trigger.requirementsCheck), scoped to actionTriggerModes.
	if actionTriggerModes[t.Mode] && strings.EqualFold(t.Params["PlayerTurn"], "True") &&
		e.G.Active != e.controllerOf(source) {
		return false
	}
	// CR 603.4 intervening-if: a trigger whose condition is false at the
	// moment the trigger event occurs does not trigger at all. This gate is
	// applied uniformly to every mode so the same T: line grammar (a
	// LifeAmount$ or IsPresent$+PresentCompare$ clause on the trigger) is
	// honoured wherever it appears.
	if !e.triggerConditionHoldsWithSVars(t, source, e.controllerOf(source), nil, ownedSVars) {
		return false
	}
	return true
}

// notThisAbilityExcludes implements NotThisAbility$ True: whether the event
// now being matched was caused by the resolution of THIS very trigger
// (same source permanent, same compiled trigger line). See the gate's
// comment in triggerMatches for the full reasoning; this helper is the
// read, kept beside actionCause's home package conventions. An absent or
// non-True parameter is a cheap no-op (the corpus's 99.99% case).
func (e *Engine) notThisAbilityExcludes(t cards.Trigger, source state.ObjID) bool {
	if !strings.EqualFold(strings.TrimSpace(t.Params["NotThisAbility"]), "True") {
		return false
	}
	cause := e.actionCause()
	if cause == 0 {
		return false
	}
	co := e.G.Obj(cause)
	return co != nil && co.Source == source && co.Ability == t.Effect
}

// secondaryYields implements Forge's Secondary$ True (TriggerHandler,
// Trigger.isSecondary): the marked trigger yields when the SAME event would
// also fire its card's paired primary, so one card text never becomes two
// triggers. The pairing is strictly by shared Execute$ SVar: the corpus's
// paired halves share the one SVar their two conditions resolve into (the
// "enters or attacks" family -- Grave Titan, Sun Titan, Tome of Legends,
// Kindred Discovery, Zoraline's "enters or attacks" half -- plus the
// Eminence family's command-zone/battlefield halves, which the zone gate
// keeps mutually exclusive anyway). A secondary whose paired primary did not
// fire for this event still fires on its own -- Grave Titan's Attacks half
// fires for the attack although its ETB half did not.
//
// A Mode-equality fallback was tried here and REMOVED (r2 review): pairing
// any same-Mode sibling suppresses independent co-firing abilities, not just
// complementary halves. Zoraline, Cosmos Caller carries two Attacks triggers
// on one face -- her printed "Whenever a Bat you control attacks, gain 1
// life" and the Secondary$-marked "enters or attacks" half -- and for one
// DeclareAttackers event both match, so the fallback lost her printed
// trigger (Vengeful Ancestor and Ashling, Rimebound were the suspected
// trace-level victims of the same shape). The corpus-measured picture for
// same-Mode pairs with distinct Execute$ SVars is exactly the two shapes the
// fallback conflated: the genuinely-complementary halves it was built for
// (Wooden Stake's blocks-or-is-blocked-by pair, Sower of Discord's two
// DamageDoneOnce halves, the Clashed Won$ True/False pairs) have disjoint
// Valid halves and never double-fire without a yield, while the pairs that
// DO co-fire on one event -- Zoraline's two Attacks triggers, Sephiroth's
// printed attack trigger beside its marked "enters or attacks" half,
// Ashling, Rimebound's Main1 mana burst beside its transform offer -- are
// independent card texts that SHOULD both fire. No distinct-Execute pair
// needs a yield; none gets one.
func (e *Engine) secondaryYields(observer *Engine, face *cards.Face, ti int, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	for j, sib := range face.Triggers {
		if j == ti || strings.EqualFold(sib.Params["Secondary"], "True") {
			continue
		}
		if sib.Params["Execute"] != t.Params["Execute"] {
			continue
		}
		if observer.triggerMatches(sib, source, ev, lki) {
			return true
		}
	}
	return false
}

// compareIntCount evaluates Forge's <OP><N> comparison grammar (EQ1, GT1,
// EQ0, ...) against n. A value that is not a literal comparison (an X, a
// bare word, an unknown operator) fails closed: a trigger condition the
// engine cannot evaluate must stay silent, never fire wide.
func compareIntCount(n int32, expr string) bool {
	expr = strings.TrimSpace(expr)
	for _, cand := range []struct {
		op string
		fn func(a, b int32) bool
	}{{"EQ", func(a, b int32) bool { return a == b }},
		{"NE", func(a, b int32) bool { return a != b }},
		{"GE", func(a, b int32) bool { return a >= b }},
		{"LE", func(a, b int32) bool { return a <= b }},
		{"GT", func(a, b int32) bool { return a > b }},
		{"LT", func(a, b int32) bool { return a < b }}} {
		if rest := strings.TrimPrefix(expr, cand.op); rest != expr {
			v, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return false
			}
			return cand.fn(n, int32(v))
		}
	}
	return false
}

// phaseOutAllMatches matches a Mode$ PhaseOutAll trigger (The War Doctor's
// "whenever one or more other permanents phase out") against one PhaseOut
// event. Only a phase-OUT (Amount >= 1) counts; a phase-IN (Amount -1) is the
// opposite event. The line's ValidCards$/ValidCard$ filter is evaluated
// against the phasing permanent with the trigger's source bound the way every
// object matcher binds it, so `Permanent.phasedOutOther` can exclude the
// source itself. The once-per-batch cadence is the latch in triggerMatches,
// not this matcher.
func phaseOutAllMatches(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.PhaseOut || ev.Amount < 1 || ev.Obj == 0 {
		return false
	}
	spec := strings.TrimSpace(t.Params["ValidCards"])
	if spec == "" {
		spec = strings.TrimSpace(t.Params["ValidCard"])
	}
	if spec == "" {
		return true
	}
	return e.matchesSpec(spec, ev.Obj, e.specCtx(source, e.controllerOf(source)))
}

func init() {
	// CR 603.8 state trigger: the event under test is irrelevant; the trigger
	// fires when its condition holds (see triggerConditionHolds) and no
	// instance is outstanding (the checkTriggers latch).
	registerTrigMatcher(func(*Engine, cards.Trigger, state.ObjID, events.Event, *state.Object) bool {
		return true
	}, "Always")

	// PhaseOutAll (CR 702.25, phaseoutall1): the batch-level "whenever one or
	// more other permanents phase out" trigger (The War Doctor). It matches
	// the events.PhaseOut marker the api:Phases primitive emits, which existed
	// before the mode did; the mode itself is new to the table. Registered
	// through a func literal calling the pack-level matcher so the census can
	// read the callee (registerTrigMatcher takes a method expression or a
	// literal whose first call names the matcher).
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
		return phaseOutAllMatches(e, t, source, ev, lki)
	}, "PhaseOutAll")

	effects.RegisterNonAPI(
		"trig:ChangesZone", "trig:ChangesZoneAll", "trig:SpellCast", "trig:Attacks", "trig:AttackersDeclaredOneTarget",
		"trig:AttackersDeclared", "trig:AttackerBlocked", "trig:AttackerBlockedByCreature", "trig:AttackerUnblocked", "trig:AttackerUnblockedOnce", "trig:Blocks", "trig:Cycled", "trig:CounterAdded", "trig:CounterAddedOnce", "trig:CounterRemoved", "trig:CounterRemovedOnce", "trig:CounterPlayerAddedAll",
		"trig:Sacrificed", "trig:Discarded", "trig:CommitCrime", "trig:Taps", "trig:TapsForMana", "trig:Untaps",
		"trig:ClassLevelGained", "trig:BecomeMonstrous",
		"trig:TokenCreated", "trig:TokenCreatedOnce",
		"trig:DamageDone", "trig:DamageDealtOnce", "trig:DamageDoneOnce", "trig:DamageAll", "trig:Drawn", "trig:LifeLost", "trig:LifeLostAll",
		"trig:LifeGained",
		"trig:BecomesTarget", "trig:BecomesTargetOnce", "trig:LandPlayed", "trig:Phase", "trig:Attached", "trig:Unattached", "trig:FlippedCoin",
		"trig:Vote", "trig:RolledDie", "trig:RolledDieOnce",
		"trig:Explores", "trig:Exerted", "trig:Investigated", "trig:SearchedLibrary",
		"trig:Exploited",
		// trig:TurnFaceUp (task cli-20260924T031747Z-6d0658fc): "When
		// CARDNAME is turned face up" (CR 702.36e for morph/megamorph,
		// CR 708.6 for manifest/cloak; Master of Pearls, Kheru
		// Spellsnatcher and the mode's 125 corpus carriers). Matched by
		// turnFaceUpMatches (rules/trigmatch_faceup.go) off the
		// events.TurnFaceUp marker the morph-family special action and
		// effSetState's Mode$ TurnFaceUp arm emit; proved by
		// rules/turnup_replacement_test.go's
		// TestMasterOfPearlsTurnFaceUpSelfTriggerFiresOnce.
		"trig:TurnFaceUp",
		"trig:ManaExpend",
		"trig:Connives",
		"trig:Discover", "trig:SeekAll",
		"trig:Surveil", "trig:Scry",
		"trig:PhaseOutAll",
		// trig-proliferate: "Whenever you proliferate ..." (CR 701.27; the 6
		// corpus carriers at the pin, Voidwing Hybrid and Ezuri Stalker of
		// Spheres among them). Matched by proliferateMatches on the
		// events.Proliferate marker effProliferate emits once per completed
		// action.
		"trig:Proliferate",
		"trig:AbilityCast", "trig:SpellAbilityCast", "trig:Always",
		// The cast-or-copy pair: SpellCopy matches a copy put on the stack and
		// SpellCastOrCopy matches either half (magecraft). Both are matched
		// above and gated in trigger_eligibility.go -- proved by
		// TestMagecraftSpellCastOrCopyFiresOnACast, ...FiresOnACopy and
		// TestSpellCopyPrimarySilentOnAPlainCast. Implemented but never
		// registered, so the ratchet read them as gaps.
		"trig:SpellCopy", "trig:SpellCastOrCopy",
		// The rest of the same registration class, found by sweeping the
		// modes triggerModeEvents maps against this list: each matches and
		// carries its own proof file, and none was registered.
		// trig:DamagePreventedOnce (damagePreventedMatches,
		// damage_prevented_once_test.go: TestDamagePreventedOnceFiresPer
		// Prevention) and trig:RingTemptsYou (ring_test.go:
		// TestCR701RingTemptsYouCallOfTheRingUpkeep).
		"trig:DamagePreventedOnce", "trig:RingTemptsYou",
		"repl:Moved",
		// Task 16 keyword triggers, expanded by cards/keywords.go into ordinary
		// ChangesZone / Attacks / SpellCast triggers routed through the modes
		// above: Undying and Evolve are ChangesZone triggers, Exalted is
		// (Alone$) Attacks, Prowess is SpellCast.
		// Persist (CR 702.77) is Undying's mirror: the same ChangesZone
		// dies-trigger shape, reading counters_EQ0_M1M1 off the LKI and
		// returning the permanent with a -1/-1 counter.
		"kw:Undying", "kw:Persist", "kw:Evolve", "kw:Exalted", "kw:Dethrone", "kw:Prowess", "kw:Riot", "kw:Hideaway", "kw:Extort", "kw:Myriad", "kw:Soulbond", "kw:Dredge",
		// CR 702.70 Training: an Attacks trigger (cards/kw_training.go) whose
		// "with another creature with greater power" condition is read by
		// attacksMatches (the Dethrone precedent), with a granted-keyword
		// synthesis (checkGrantedTrainingTriggers) for the layer-6 grant.
		"kw:Training",
		// CR 702.121 Melee: snapshot distinct attacked opponents at declaration
		// for each printed or granted Attacks trigger instance.
		"kw:Melee",
		// CR 702.134 Mentor: an Attacks trigger (cards/kw_mentor.go) whose
		// targeted PutCounter body carries the Mentor$ marker, enforced by
		// mentorAdmits at both the target offer and the recheck, with a
		// granted-keyword synthesis (checkGrantedMentorTriggers) for the
		// layer-6 grant.
		"kw:Mentor",
		// Task 17: Storm's expansion (cards/keywords.go) is a SpellCast
		// trigger whose effect is CopySpellAbility -- the expansion existed
		// since Task 11; registering the keyword here completes its
		// semantics now that api:CopySpellAbility is implemented.
		"kw:Storm", "kw:Ward", "kw:Annihilator", "kw:Mobilize",
		// Afflict's expansion (cards/keywords.go) is a become-blocked trigger
		// (trig:AttackerBlocked) whose body drains the defender; the granted
		// (layer-6 AddKeyword$) form is synthesized by
		// checkGrantedAfflictTriggers, the Dethrone precedent.
		"kw:Afflict",
		// Flanking's expansion (cards/keywords.go) is a become-blocked trigger
		// on the new AttackerBlockedByCreature mode (CR 702.25a), one instance
		// per non-flanking blocker, debuffing it -1/-1 until EOT via Pump.
		"kw:Flanking",
		// Afterlife's expansion (cards/keywords.go) is a ChangesZone death
		// trigger whose effect mints the wb_1_1_spirit_flying tokens.
		"kw:Afterlife",
		// Fabricate's expansion (cards/keywords.go) is a ChangesZone ETB
		// trigger whose Charm elects counters or Servo tokens.
		"kw:Fabricate",
		// Battle cry (CR 702.33) expands to an ordinary Attacks trigger that
		// pumps the other creatures attacking in the declare-attackers batch.
		"kw:Battle cry",
		// Exploit's expansion (cards/kw_exploit.go) is a ChangesZone ETB
		// trigger whose effect is the optional DB$ Sacrifice -> DB$ Exploit
		// chain (CR 702.58a). The layer-6 AddKeyword$ Exploit grant (Colonel
		// Autumn's "Other legendary creatures you control have exploit") is
		// synthesized by checkGrantedExploitTriggers, the Afflict/Dethrone
		// precedent; the marker half is the api:Exploit primitive registered
		// by effects/exploit.go.
		"kw:Exploit",
		// Mass effects, extra turns and new-set mechanics (the
		// inbox-engine-gap-mass-turn-new-mechanics ticket):
		//   - trig:UnlockDoor: a Room's unlock trigger (rules/rooms.go),
		//     queued off the DoorUnlock event the unlock activation emits.
		//   - kw:Station: the Spacecraft station activation (rules/station.go).
		//   - kw:Chapter: the Saga mechanic (rules/saga.go).
		//   - kw:Start your engines: the speed mechanic (rules/speed.go).
		//   - stat:Panharmonicon: the trigger-doubling static (rules'
		//     panharmoniconEchoes, consulted in checkFaceTriggers).
		//   - kw:Partner, kw:Partner with and "kw:CARDNAME can be your
		//     commander." are DECK-CONSTRUCTION keywords (CR 903.13a/b, the
		//     CR 903.13c named-pair alias, and CR 903.4-style commander
		//     eligibility): the engine already seats and casts commanders
		//     per Config -- partnerPairOK -> deck.IsPartnerPair checks the
		//     mutual named pair -- so the registrations assert the corpus
		//     shape is understood. kw:Partner with ALSO prints real rules
		//     text (CR 702.128's ETB may-search for the named partner), which
		//     cards/kw_partner_with.go expands into an ordinary trigger; the
		//     bare kw:Partner line and "kw:CARDNAME..." remain play-free.
		"trig:UnlockDoor", "trig:FullyUnlock", "kw:Station", "kw:Chapter", "kw:Start your engines",
		"stat:Panharmonicon", "kw:Partner", "kw:Partner with",
		"kw:CARDNAME can be your commander.",
		// kw:Doctor's companion (the WHO two-commander keyword, the
		// Doctor's-companion analogue of Companion CR 702.139) is DECK
		// CONSTRUCTION too: "You can have two commanders if the other is the
		// Doctor." Nothing in play reads it -- the corpus's in-play uses of
		// the keyword are the `withDoctor's companion` FILTER predicate
		// (rose_noble's trigger, an_unearthly_child's DigUntil), which is a
		// filter-grammar shape, not this keyword -- so the registration
		// asserts the corpus shape is understood, not that the second-commander
		// seating exists (the deck.IsPartnerPair extension is a separate
		// ticket; the Partner precedent above is the same class).
		"kw:Doctor's companion",
		// kw:Companion is the deck-construction keyword (CR 702.139), in the
		// same registration class as Partner above. This asserts the corpus
		// shape is understood; the chosen-companion pregame pick and the
		// outside-the-game activation are separate play-side work.
		"kw:Companion",
		// Graft (CR 702.57): the optional move-counter trigger is expanded
		// from the keyword line in cards/kw_graft.go.
		"kw:Graft",
	)
}
