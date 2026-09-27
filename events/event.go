// Package events defines every way state can change and is the only package
// permitted to mutate state.Game. The log is therefore a complete description
// of a match, and replay is state reconstruction rather than re-simulation.
package events

import (
	"encoding/base64"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// UntapReplacedByStunNotice marks CR 122.1d's CounterChange substitution.
// Like EntryCounterNotice, it uses the existing Text field; Apply still places
// the counter because only EntryCounterNotice suppresses placement.
const UntapReplacedByStunNotice = "__untap_replaced_by_stun_notice"

type Kind uint8

const (
	GameStart Kind = iota
	Shuffle
	MoveZone
	Draw
	LifeChange
	Damage
	Tap
	Untap
	StepChange
	TurnChange
	Priority
	PutOnStack
	Resolve
	ManaAdd
	ManaClear
	CounterChange
	DeclareAttackers
	DeclareBlockers
	PlayerLost
	GameOver
	DecisionAsk
	DecisionMade
	Note
	LandPlayed
	// TargetsChosen carries a spell or ability's chosen targets. Appended here
	// (Ruling T14-b) rather than inserted near PutOnStack/Resolve so every
	// earlier kind's numeric value, and therefore the hash chain and the
	// golden replays Tasks 9-13 already locked in, is unaffected.
	//
	// Amount is the discriminator between the two target shapes: 0 means
	// object targets (read from IDs), 1 means a player target (read from
	// Player). An empty IDs with a zero Player would otherwise be
	// indistinguishable from "player 0 was targeted" -- PlayerID 0 is a real
	// seat as well as the zero value.
	TargetsChosen
	// FlipFace changes an object's active face. Task 18's SetState primitive
	// is the only source of this event. Appended here rather than inserted
	// nearer Tap/Untap (Ruling T18-a, following TargetsChosen's own T14-b
	// precedent) so every earlier Kind's numeric value -- and therefore the
	// hash chain and any golden replay already locked in -- is unaffected.
	//
	// Amount carries the destination FaceIdx directly, not a delta: SetState
	// resolves Mode$ (Transform/Flip/...) to a concrete index itself, so
	// Apply does not need to know the object's current face to act on it.
	FlipFace
	// ClockTick advances Game.Clock by one. Ruling T19-a: rules.AddContinuous
	// used to increment g.Clock with a direct field write to stamp a
	// zero-Timestamp ContinuousEffect, the same bug class Ruling T11-a fixed
	// for Passes/Priority -- Object.Timestamp is assigned from Clock whenever
	// a permanent enters the battlefield (see Move below), so a live game
	// that advances Clock outside Apply leaves every later Timestamp off by
	// one in a fresh reconstruction folded from the log alone. AddContinuous
	// now emits this and reads the result instead of writing Clock itself.
	// It carries no fields: the tick's only effect is the increment, so
	// nothing else needs to survive the encode/decode round trip. Appended
	// here (following TargetsChosen's T14-b and FlipFace's T18-a precedent)
	// so every earlier Kind's numeric value, the hash chain, and any golden
	// replay already locked in are unaffected.
	ClockTick
	// TriggerPush creates a triggered ability's stack object and places it
	// on the stack, in one event. Ruling T20-a: a live rules.Engine used to
	// mint this object with a direct, unlogged Game.AddObject call and then
	// emit a plain MoveZone naming its (already-assigned) ObjID -- but a log
	// replayed with no Engine behind it never learns that ID exists, so
	// events.Move's "if o == nil { return }" guard silently no-ops and the
	// replayed stack permanently diverges from the live one. This event
	// carries what Apply needs to recreate the object itself: Player is its
	// controller, Obj is the permanent whose trigger fired (Object.Source),
	// Amount is that permanent's Face().Triggers index (so Apply re-derives
	// the *cards.SA to run -- a raw pointer cannot be logged, only the data
	// that lets it be found again), and IDs is the triggering event's
	// Remembered object(s). No new Event field was added: Player/Obj/Amount/
	// IDs already exist and are reused, exactly as the brief's Ruling
	// requires, so the hash chain and every earlier golden replay are
	// unaffected by this Kind's own shape -- only its ordinal, appended here
	// after ClockTick, is new.
	TriggerPush
	// EndCombatReset clears every object's IsAttacking/BlockedBy fields
	// when Obj is zero. Nonzero Obj removes only that permanent from combat,
	// retaining zero blocker-list tombstones so attackers stay blocked.
	// Ruling T21-e (Task 21 fix round 1): rules.setStep used to do this with
	// a direct loop over e.G.Objs when entering StepEndCombat or
	// StepCleanup, instead of emitting anything -- a violation of "all state
	// mutation goes through events.Apply" that was structurally present
	// before Task 21 but stayed inert (IsAttacking/BlockedBy were never
	// actually set to a non-zero value by anything) until real combat made
	// it observable: a log-only reconstruction, having never been told
	// combat ended, kept a surviving attacker/blocker marked IsAttacking/
	// BlockedBy forever, while the live game correctly cleared both.
	// Carries no fields (same shape as ClockTick): the reset, applied to
	// every object in the arena, is its whole effect. Appended here,
	// following TriggerPush's own precedent, so every earlier Kind's
	// ordinal, the hash chain, and any golden replay already locked in are
	// unaffected.
	EndCombatReset
	// CastInfo records how a spell was cast: Amount is the value chosen for
	// {X} (state.Object.X), and Counter is a comma-separated list of flag
	// names (FlagsFrom/FlagsString) folded into state.Object.CastFlags. Task
	// 4. Since the paid-{X} Ctx binding (CR 107.3i), commitCast's ability arm
	// also emits it on the AbilityPush-minted stack object to record the {X}
	// an activated ability's Cost$ was paid with -- Obj is that ability
	// object, never the source permanent -- so Amount's meaning is "the {X}
	// value paid for this cast or activation". Appended here, after
	// EndCombatReset, following every prior Kind's
	// own append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	CastInfo
	// Choose records an answer to an "as this enters/resolves, choose ..."
	// effect. Counter discriminates the shape ("name", "type", "number");
	// Text carries a chosen name or creature type, Amount a chosen number.
	// An unrecognized Counter is a no-op, not an error, the same totality
	// stance as every other case in Apply's switch.
	Choose
	// TokenCreate mints a token onto the battlefield. Player is its owner
	// and controller; Text is the key into Game.Tokens (a Forge token
	// script stem, e.g. "r_1_1_goblin") -- token definitions live on the
	// game, set at genesis, never in the event itself, so Apply stays a
	// pure function of (g, e) with no card data smuggled through Text.
	TokenCreate
	// StackCopy duplicates the spell or ability named by Obj (which must
	// currently be on the stack) and places the copy on top of the stack.
	// Player is the copy's controller (CR 707.10a: a copy's controller is
	// whoever the copy effect says, not necessarily the original's).
	// IDs, when non-empty, names the copy's chosen targets (object ids):
	// the copy's target list is REPLACED with those object targets instead
	// of inheriting the original's -- Forge's CopySpellAbilityEffect
	// DefinedTarget$ route (Feather, Radiant Arbiter's "for each of those
	// creatures, copy that spell. The copy targets that creature"). Every
	// pre-existing emitter carries no IDs and keeps the inherited-target
	// reading, so replaying an older log is unchanged.
	StackCopy
	// Attach records or clears what Aura/Equipment permanent Obj is
	// attached to. IDs holds zero or one entries: empty detaches (sets
	// AttachedTo to 0), one attaches to that id.
	Attach
	// AbilityPush creates an activated ability's stack object and places it
	// on the stack, in one event -- the same shape TriggerPush already
	// uses for triggered abilities, for the same reason (Ruling T20-a): a
	// log-only replay must be able to recreate the object itself, not rely
	// on a live Engine's direct, unlogged AddObject call. Player is the
	// ability's controller, Obj is the source permanent, Amount is that
	// permanent's Face().Abilities index, and IDs is whatever the
	// activation remembered (mirrors TriggerPush's own IDs usage).
	AbilityPush
	// ModeChosen records a KModes answer: spell announcement (CR 601.2b),
	// triggered-ability placement (CR 603.3c), or a mid-resolution mode or
	// unless-pay choice. Obj is the stack object, Player the chooser, and Text
	// the chosen option labels (csv, in execution order). It is a marker like
	// Note: the engine caches placement/announcement names or carries a
	// resolution answer in its continuation, while Apply remains inert. The
	// log lets replay re-derive the same branch. Appended
	// here, after AbilityPush, following every prior Kind's own append-only
	// precedent, so no earlier ordinal, hash chain or golden replay is
	// affected (M2d-2).
	ModeChosen
	// CmdDamage records a commander dealing combat damage to a player (CR
	// 903.10, Task m33). Player is the damaged player, Obj the source
	// commander (whose Object ID is stable across zone moves), and Amount
	// the damage that actually landed (prevented/replaced damage is never
	// emitted). events.Apply folds it into that player's cumulative
	// commander-damage tally (state.Player.CmdDamage), keyed by the source
	// commander's match-wide dense index derived from g.Players[].Commanders.
	// Carried as a dedicated event rather than derived from the existing
	// Damage events because those do not record which commander the source
	// was -- this event is what lets a reconstruction starting from the log
	// alone rebuild the per-commander tally. Appended after ModeChosen,
	// following every prior Kind's append-only precedent, so no earlier
	// ordinal, hash chain or golden replay is affected. Only ever emitted in
	// a Commander-format game.
	CmdDamage
	// DelayedRegister records a delayed-trigger registration (CR 603.7,
	// effects.effDelayedTrigger's Mode$ Phase branch). A delayed trigger is
	// registered during one resolution and fires later -- in general a
	// different turn -- so the registration is game state, folded here into
	// state.Game.Delayed so a log-only replay rebuilds the same set. Event
	// field reuse: Obj is the source object (whose face's SVar table carries
	// the Execute$ ability), Player the controller, Step the phase to fire
	// in, Counter the Execute$ SVar name, IDs the Remembered object(s)
	// captured at registration (PlayerRef-encoded, as TriggerPush does), and
	// Text the Forge Phase$ string for description -- or, for an event-matched
	// (non-phase) registration, "<Mode$ value>:<trigger SVar name>", the
	// encoding events.Apply's DelayedRegister case decodes into
	// state.DelayedTrigger's EventMode/Trigger pair. Appended here, after
	// CmdDamage, following every prior Kind's own append-only precedent, so
	// no earlier ordinal, hash chain or golden replay is affected.
	DelayedRegister
	// DelayedPush mints a delayed triggered ability's stack object and fires
	// it -- the "goes on the stack like any other triggered ability" half of
	// a Mode$ Phase delayed trigger, created when the registered phase is
	// entered. It is a sibling of TriggerPush/AbilityPush and exists for the
	// same reason (Ruling T20-a): the ability object is minted inside Apply,
	// so a log-only replay creates the exact object a live game did. Unlike
	// TriggerPush (whose Ability re-derives from a face Triggers index), a
	// delayed trigger's Ability is an SVar-named sub-ability on the source's
	// face, so the Execute$ name is carried in Counter and Apply resolves it
	// via cards.ResolveSVar. Obj is the source, Player the controller,
	// Amount the registration's ID (state.DelayedTrigger.ID) so Apply can
	// remove exactly the registration it fired, and IDs the Remembered
	// objects (PlayerRef-encoded). Firing is one-shot: Apply's case both
	// mints the object and drops the registration. Appended after
	// DelayedRegister, following every prior Kind's append-only precedent.
	DelayedPush
	// LibraryOrder records a player having set a complete new order on
	// their library -- the state change behind a library-arranging effect
	// (RearrangeTopOfLibrary, Ponder; later Scry/Surveil/Dig) and the
	// discard half of a bottoming ask. Player names whose library, IDs the
	// COMPLETE new order (the top cards reordered, then the untouched
	// remainder beneath them). Secret is always true: the exact order of a
	// hidden zone must not leak to another seat, so view redaction drops the
	// payload for anyone but Player.
	//
	// Appended here, after DelayedPush, following the named precedents of
	// TargetsChosen (Ruling T14-b), FlipFace (T18-a) and ClockTick (T19-a)
	// rather than inserted anywhere earlier, so every earlier Kind's numeric
	// value, and therefore the hash chain and every golden replay already
	// locked in, is unaffected.
	LibraryOrder
	// ExtraTurn records one grant or consumption of an extra turn (CR
	// 500.7). Player is the seat the turn belongs to and Amount the delta:
	// an api:AddTurn effect emits +NumTurns$, and the turn structure (rules
	// advanceStep) emits -1 exactly when it hands that seat a repeat turn
	// instead of moving to the next living seat. The state
	// (state.Game.ExtraTurns) is folded here, so a log-only reconstruction
	// arrives at the same pending counts the live game held. When the
	// granting ability carries Forge's ExtraTurnDelayedTrigger$ (Final
	// Fortune's "At the beginning of that turn's end step, you lose the
	// game"), Counter names the Execute$ SVar and Obj the granting source,
	// and this case ALSO registers the delayed trigger for the extra turn's
	// end step: a delayed registration whose MinTurn is the extra turn's
	// number (g.Turn+1 at grant time), so the ordinary delayed-trigger
	// firing skips the CURRENT turn's end step and fires exactly once, in
	// the granted turn. Appended here, after LibraryOrder, following every
	// prior Kind's append-only precedent, so no earlier ordinal, hash chain
	// or golden replay is affected.
	ExtraTurn
	// DoorUnlock records the unlock of an Enchantment Room's locked door
	// (CR 309.5): the player paid the locked half's mana cost as a sorcery.
	// Obj is the room permanent; Apply sets its Unlocked flag, which makes
	// the alternate face's rules text live (rules' trigger/static/ability
	// scans) and is what a Mode$ UnlockDoor trigger matches against. The
	// unlock trigger itself is queued rules-side on this event (rules
	// checkTriggers), so this event is the whole state delta. Appended
	// after ExtraTurn, same append-only precedent.
	DoorUnlock
	// SpeedChange records one increment of a seat's speed (CR 702.163,
	// "Start your engines!"). Player is the seat whose speed rises and
	// Amount the delta (always +1 today; the engine caps the grant at max
	// speed 4 and at once per turn before ever emitting). Folded into
	// state.Player.Speed here, so a reconstruction rebuilds it. Appended
	// after DoorUnlock, same append-only precedent.
	SpeedChange
	// MonarchChange gives the designation to Player. It is a state transition,
	// not a Note, so conditional "if you're the monarch" triggers replay from
	// the same state as the live match. Appended after LibraryOrder to preserve
	// every prior event ordinal.
	MonarchChange
	// ControlChange transfers control of a permanent or a stack object. Obj is
	// the controlled object and Player its new controller. It is deliberately a
	// distinct event: control is neither ownership nor a zone change, and a
	// replay must retain it when the object later moves.
	ControlChange
	// CardToken mints a battlefield token that is a copy of the card object Obj
	// names. Appending after main's existing events preserves their ordinals.
	CardToken
	// KeywordTriggerPush mints a mandatory keyword-provided triggered ability.
	KeywordTriggerPush
	// Goad applies CR 701.38's attack requirement to Obj. Player is the
	// goading player, Text its Forge duration, IDs[0] its source, and Amount
	// the target's controller plus one. Amount -1 removes every relationship.
	Goad
	// PlayerCounterChange changes a counter on Player. Counter names the kind
	// and Amount the delta; Ward's AddCounterYou<.../POISON> is its first use.
	PlayerCounterChange
	// Imprint updates one source-card association. Obj is the source and IDs
	// are the cards to add: ordinary text is Forge's imprintedCards list,
	// Text "exiled-with" is its distinct exiledCards list, Text
	// "until-host-leaves" is ChangeZone's Duration$ UntilHostLeavesPlay
	// association (Amount carries the zone the cards were exiled from, which
	// their return moves them back to; rules sweepExileReturn consumes it),
	// and Text "clear" clears only imprintedCards. Reusing Text avoids
	// changing Event's layout.
	// It is append-only so prior event ordinals and replay hashes stay stable.
	Imprint
	// StartingPlayerChange records CR 103.1's starting-player designation.
	// It is emitted by genesis's toss resolution (folded without appending at
	// genesis, since genesis is replayed from Config) and by an opening-hand
	// effect such as Impatient Iguana, rather than being inferred from
	// TurnChange, because Count$StartingPlayer must read its result before
	// turn one. Appended after main's kinds so the merge preserves main's
	// ordinals (log.json serializes kind numerically; a committed fixture's
	// replay pins them).
	StartingPlayerChange
	// Pair records a Soulbond pairing (CR 702.103): Obj is the pairing
	// permanent and IDs[0] is its chosen partner. Appended after
	// StartingPlayerChange (the merge kept main's kinds at their main
	// ordinals), following the same append-only precedent, so all earlier
	// Kinds keep their numeric values and the hash chain and golden replays
	// are unaffected.
	Pair
	// MyriadCopy records one Myriad (CR 702.109) attacker token: a copy of
	// the attack-creature Obj that enters tapped and attacking the opponent
	// named by Player. Appended after Pair, same append-only precedent.
	MyriadCopy
	// MyriadCleanup exiles every Myriad token still on the battlefield as the
	// end-of-combat step ends (CR 702.109a). Its event-sourced arena scan keeps
	// live play and log replay in lockstep without adding one delayed trigger
	// per token.
	MyriadCleanup
	// GrantTriggerPush mints a static-grant's triggered ability (AddTrigger$
	// on a Mode$ Continuous static, e.g. Hearthhull's "STATION 8+ Whenever you
	// sacrifice a land") and places it on the stack, in one event. It mirrors
	// DelayedPush's shape -- the Ability is not a face Triggers index but the
	// granted trigger's Execute$ SVar-named body -- minus the registration
	// bookkeeping: a granted trigger is consumed by nothing and lives exactly
	// as long as its granting static. The Execute$ body lives on the GRANTOR's
	// face (the card carrying the static), while Obj is the AFFECTED
	// recipient; for a cross-object grant the grantor's object id rides
	// Amount, and Apply resolves the name from the grantor's SVar table when
	// it is set (0 = the historical self-grant shape, resolved from the
	// affected object's own table). Rules' queue gate links the body from the
	// same table, so a replay reproduces the same stack object. Appended after
	// MyriadCleanup, following every prior Kind's append-only precedent, so no
	// earlier ordinal, hash chain or golden replay is affected.
	GrantTriggerPush
	// ManaActivate records one activation of an AB$ Mana ability. It is a
	// MARKER like Note: effMana's own ManaAdd events carry the mana that
	// landed but never attribute it to a source object (Event.Obj is unset
	// on them), and populating it would re-encode every existing ManaAdd
	// event and move every golden replay -- so the marker, emitted only by
	// resolveManaAbility for abilities that actually carry an
	// ActivationLimit$ parameter (Vivi Ornitier's "only once each turn"),
	// is what lets rules' activationLimitReached event-log scan count mana
	// activations by source and ability index, the same way it already
	// counts AbilityPush for non-mana abilities. Obj is the source permanent,
	// Player the activator, Amount the ability's index in the face's
	// Abilities slice. A marker carrying IDs is a GAINED mana activation
	// (Forge's GainsAbilitiesOf$, rules' gainedManaRef): IDs[0] names the
	// FOREIGN card the ability belongs to and Amount indexes that card's face
	// Abilities, which is what the GainsAbilitiesLimitPerTurn$ cap counts; it
	// is emitted for every gained mana activation, limit or not, and the
	// printed-limit scan skips it. Appended here, after GrantTriggerPush, following every
	// prior Kind's own append-only precedent, so no earlier ordinal, hash
	// chain or golden replay is affected.
	ManaActivate
	// TokenAttacks marks an already-existing permanent that entered the
	// battlefield TAPPED AND ATTACKING (TokenAttacking$ or a move body's
	// Attacking$ True rider). It is NOT a mint: events.Apply's TokenCreate or
	// MoveZone case already made the object, and Obj here is that battlefield
	// object, Player its controller and IDs[0] the defender it attacks.
	// MyriadCopy must not be reused for this: it
	// mints a copy of the SOURCE card and flags IsMyriad, which
	// MyriadCleanup exiles at end of combat -- wrong semantics for a script
	// token a Sacrifice at the next end step owns. Appended here, after
	// ManaActivate, following every prior Kind's own append-only precedent,
	// so no earlier ordinal, hash chain or golden replay is affected.
	TokenAttacks
	// XChange records a mid-resolution effect rewriting the {X} a stack
	// object was cast or activated with (Unbound Flourishing's "double the
	// value of X", Glava's "the value of X becomes 5" -- DB$ ChangeX). Obj
	// is the stack object whose {X} was rewritten, Amount the new value. A
	// plain CastInfo could not carry this: its Apply case resets CastFlags
	// from Counter unconditionally (wiping Kicked/Flashback on a flagged X
	// spell) and would shadow adventure's first-CastInfo backward log scan,
	// and neither FlagConverged nor FlagReplicated may alias a real X
	// value. Appended here, after TokenAttacks, following every prior
	// Kind's own append-only precedent, so no earlier ordinal, hash chain
	// or golden replay is affected.
	XChange
	// NoteNumber records a number a trigger's Execute$ body NOTED onto a
	// card (DB$ Pump | NoteNumber$ <expr> -- Lupine Harbingers' exile
	// trigger noting Count$YourTurns, the corpus's one NoteNumber$
	// carrier): Obj is the card, Amount the noted value, and Apply folds it
	// into Object.NotedNumber for Count$NotedNumber to read at the later
	// ETB. A plain Note could not carry this: it is transcript text with no
	// numeric payload and no Apply behaviour, and the value must be
	// event-backed so a replay derives the identical count. Appended here,
	// after XChange, following every prior Kind's own append-only
	// precedent, so no earlier ordinal, hash chain or golden replay is
	// affected.
	NoteNumber
	// ExtraPhase records one Forge AddPhaseEffect message (DB$ AddPhase:
	// "after this phase, there is an additional combat phase"; 56 corpus SA
	// lines). Three forms, split on Amount, mirroring the ExtraTurn
	// precedent one level up: +1 is a grant (Step the splice point
	// AfterStep -- the phase after which the extra phase is inserted, either
	// the parsed AfterPhase$ or, when omitted, the phase the grant resolved
	// in so the fold agrees with the live splice; IDs[0] the extra phase's
	// entry step, IDs[1] an explicit FollowedBy$ resume point when present;
	// Counter the forwarded Execute$ SVar name; Text the ExtraPhaseRiders
	// marker when the granting SA carries ExtraPhaseDelayedTrigger$); -1
	// CONSUMES one grant at the turn boundary it splices at -- the queue
	// entry is marked consumed and, when it carries the delayed rider, the
	// one-shot delayed trigger registers HERE (MinTurn = the current turn:
	// the extra phase begins in this turn, unlike an extra turn's Turn+1);
	// -2 COMPLETES one consumed grant when the walk leaves the extra
	// phase's last step. The fold lives in state.Game.ExtraPhases (cleared
	// at TurnChange), and the consumer is rules/turn.go's advanceStep tail.
	// Appended after NoteNumber (main's own later append), still after every
	// earlier Kind, so no earlier ordinal, hash chain or golden replay is
	// affected.
	ExtraPhase
	// CopyToken mints a battlefield token that is a copy of the CARD object
	// Obj names (DB$ CopyPermanent: Flamerush Rider, Molten Echoes, the
	// populate family -- task copyp1). Like MyriadCopy it only MINTS the
	// object, in the untracked ZLibrary state AddObject leaves it in; the
	// caller follows with a genuine MoveZone, so the copy's battlefield
	// entry stays a ChangesZone-matchable event every "a creature enters"
	// trigger observes (the CardToken shape folds its own move, which the
	// Myriad comment above records as entry-invisible). Player is the copy's
	// controller and Amount is the entry-state rider bitmask the
	// CopyToken* constants name; bit CopyTokenAttacking takes the defender
	// it attacks from IDs[0] (a player number, the MyriadCopy/TokenAttacks
	// precedent). Appended after ExtraPhase, still after every earlier
	// Kind, so no earlier ordinal, hash chain or golden replay is affected.
	CopyToken
	// Exert records CR 702.100's exert election (task exert1): Obj is the
	// permanent the controller exerted and Player is the controller at exert
	// time. Amount >= 0 is the exert itself; Amount == -1 is the
	// consumed-at-use marker rules/turn.go's untap-step scan emits when it
	// passes an exerted permanent -- CR 702.100b's "won't untap during your
	// next untap step" window closes there, so the fold clears the object's
	// skip flag. Appended here, after CopyToken, following every prior Kind's
	// own append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	Exert
	// PlanarRoll records one planar-dice roll (CR 901.3, task rollplanar1):
	// Player is the roller, Obj the rolling source, Amount the number of
	// dice rolled AFTER any planar-dice replacement rewrote the count, IDs
	// the KEPT results (1-4 blank, 5 planeswalk, 6 chaos) in roll order and
	// Counter the ignored-result count the replacement wrote ("" = none).
	// It is an Apply no-op marker: no plane deck exists in this build, so a
	// roll has no state to fold — the log line IS the record (one event per
	// roll action), and replay re-derives the same rolls from the seeded rng.
	// Appended here, after Exert, following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	PlanarRoll
	// Explore records one completed explore action (CR 701.35a, task
	// explore1): Obj is the exploring permanent, Player its controller
	// (whose library was explored), IDs[0] the card the process revealed,
	// and Amount the outcome -- 1 when the revealed card was a land and
	// went to its owner's hand, 0 when it was a nonland (the +1/+1 counter
	// went on the explorer and the card went back on top or into the
	// graveyard per the LCI wording the corpus spells out). It is an Apply
	// no-op marker, exactly like PlanarRoll: the explore's own state changes
	// are their own MoveZone/CounterChange events, and the record is what
	// trig:Explores matches and what makes the explore trigger- and
	// replay-visible. Appended here, after PlanarRoll, following every prior
	// Kind's own append-only precedent, so no earlier ordinal, hash chain or
	// golden replay is affected.
	Explore
	// CombatRetarget re-points an already-attacking creature at a new defender
	// mid-combat (api:ChangeCombatants's Attacking$ True shape -- Misleading
	// Signpost, Portal Mage, Windshaper Planetar): Obj is the attacker, Player
	// the NEW defender. It deliberately is NOT DeclareAttackers, whose Apply
	// case increments AttacksThisTurn and would refire every Attacks trigger --
	// a reselect changes no declaration, only which seat the existing attack
	// is pointed at (CR 506.3b: only within the attacker's controller's own
	// combat, which is why the same event also clears BlockedBy: Forge's
	// removeFromCombat + addAttacker leaves the old blockers behind, and the
	// re-pointed attack is unblocked -- the blocker lists of the OLD blockers
	// are attacker-side only, so clearing BlockedBy is the whole unlink).
	// Appended here, after Explore, following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	CombatRetarget
	// RingTemptsYou records one "the Ring tempts you" action (CR 701.54a:
	// each time the Ring tempts you, choose a creature you control; it
	// becomes your Ring-bearer). Player is the tempted seat, Obj the
	// designated Ring-bearer (0 when the player controls no creature — CR
	// 701.54d: the "Whenever the Ring tempts you" trigger still fires when
	// the actions complete even if some were impossible), and Amount the
	// NEW tempt count, carried as a replay-visible marker. Apply folds the
	// count increment and the designation; the designation's two clears (a
	// control change, CR 701.54b, and the permanent leaving the battlefield,
	// CR 400.7/701.54e) are derived in Apply's own ControlChange and
	// MoveZone cases, so no second event is needed. Appended here, after
	// CombatRetarget, following every prior Kind's own append-only
	// precedent, so no earlier ordinal, hash chain or golden replay is
	// affected.
	RingTemptsYou
	// RingEmblemPush mints one of the Ring emblem's four level abilities
	// (CR 701.54c), which are engine-side abilities with no corpus script
	// text and no object in any zone -- the temptation count folded by
	// RingTemptsYou is their only state (state.Player.RingTempted). Player
	// is the emblem's owner (the tempted seat), Amount the level (1..4) and
	// Counter the canonical "__ring:<level>" payload events.Apply rebuilds
	// the ability from, exactly as the granted ward/afflict
	// KeywordTriggerPush payloads are rebuilt ("the same DB$ ... a printed
	// trigger would have carried"). Obj carries the Ring-bearer the firing
	// event named (0 for a level whose body needs no bearer). The mint lives
	// in Apply because a direct unlogged Game.AddObject call would name an
	// ObjID a log-only replay never learns about (Ruling T20-a). Appended
	// here, after RingTemptsYou, following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	RingEmblemPush
	// GrantAbilityPush mints an activated ability GRANTED to one permanent
	// by another (CR 613.1f): a printed `S:Mode$ Continuous | Affected$
	// <spec> | AddAbility$ <SVar>` static (Ichormoon Gauntlet's "Planeswalkers
	// you control have [0]: Proliferate", a lord granting an activated
	// ability, an Equipment granting "{T}: deal 1 damage") whose grantor is
	// the static's source and whose recipient is the affected permanent.
	// Obj is the ability's own SOURCE -- the RECIPIENT, so the minted stack
	// object's Source (and every `Defined$ Self`/`CARDNAME` body that reads
	// it) is the affected permanent, not the granting static. Counter names
	// the SVar body on the GRANTOR's face; IDs[0] is the granting object id,
	// resolved from there by Apply. The DelayedPush precedent is why this is
	// a distinct Kind rather than an overload: DelayedPush resolves its body
	// from e.Obj and decodes e.IDs into the ability's Remembered set, which
	// is exactly wrong for a cross-object grant. Self-grants (Animate, the
	// max-speed static) still mint through DelayedPush, byte-identically.
	// Appended after RingEmblemPush (main's own later append), still after
	// every earlier Kind, so no earlier ordinal, hash chain or golden replay
	// is affected.
	GrantAbilityPush
	// Investigate records one completed investigate action (CR 701.36a, task
	// investtrig1): Player is the investigating seat and Obj the resolving
	// source permanent (0 for a source-less body). It is an Apply no-op
	// marker, exactly like Explore: the investigate's own state change (the
	// Clue token mint) is its own TokenCreate event that precedes this one,
	// and the record is what trig:Investigated matches ("whenever you
	// investigate" — Erdwal Illuminator, Val, Marooned Surveyor). The marker
	// is separate from the mint so a plain Clue-token creation (DB$ Token |
	// TokenScript$ c_a_clue_draw, no Investigate) never fires an investigate
	// trigger. Appended after GrantAbilityPush (the branch's own append, moved
	// one ordinal by the merge with main's GrantAbilityPush), still after
	// every earlier Kind, so no earlier ordinal, hash chain or golden replay
	// is affected.
	Investigate
	// BlessingChange records a seat GAINING the city's blessing (CR
	// 702.131d, task ascend1): Player is the seat. It is one-way -- Apply
	// sets the latch and nothing ever clears it (CR 702.131a: "for the
	// rest of the game") -- and the grant's continuous re-check lives in
	// the rules emitter (rules/ascend.go), so Apply folds the bit plainly.
	// Appended after Investigate, still after every earlier Kind, so no
	// earlier ordinal, hash chain or golden replay is affected.
	BlessingChange
	// ClonePermanent folds a DB$ Clone copy basis onto an existing permanent
	// (CR 613.1a's layer-1 copy): Obj is the object that BECOMES the copy,
	// IDs[0] is the object copied FROM, Text is the copy's NewName$ (empty
	// keeps the copied face's name), and Counter is "gain-this-ability"
	// when the GainThisAbility$ True rider applies to an activated/spell
	// root (Amount is a one-based index into the become face's Abilities,
	// or 0 for legacy whole-list logs) or "gain-this-trigger" when the
	// resolving root is a TRIGGER (Amount is a one-based index into the
	// become face's Triggers). An event with no IDs (or a
	// zero id) CLEARS the copy -- the expiry and leave-the-battlefield path.
	// Appended after BlessingChange, still above NumKinds, so no earlier ordinal, hash
	// chain or golden replay is affected.
	ClonePermanent
	// Mutate records one mutate-spell resolution (CR 702.140): Obj is the
	// TARGET permanent that survives and becomes the mutated pile, IDs[0] is
	// the mutate card's object (the resolving spell), and Text is "top" when
	// the mutating card is placed on top or "under" when it is placed beneath
	// the target (CR 702.140b's choice). Apply folds the pile: the survivor's
	// Card/FaceIdx always describe the top card and every under-card lands in
	// its MergedCards (top-of-pile first), each parked in ZCeased, and
	// TimesMutated advances by Amount. It is the provenance both the
	// trig:Mutates fire and Count$TimesMutated read, so a replay rebuilds the
	// pile identically. Appended here, after ClonePermanent -- the last Kind
	// main holds -- following every prior Kind's own append-only precedent,
	// so no earlier ordinal, hash chain or golden replay is affected.
	Mutate
	// MergedTriggerPush mints a mutated pile's UNDER-CARD triggered ability
	// (CR 702.140d: the permanent has all abilities of the cards beneath
	// it, including their "whenever this creature mutates" triggers). Obj
	// is the pile (the triggering source), Player the controller, Counter
	// the Execute$ SVar name (kept as the log's readable provenance and as
	// a consistency check), Amount the PAIR (under-card pile index, that
	// face's own Triggers index) packed by MergedTriggerAmount, and IDs the
	// Remembered capture the ordinary trigger push encodes.
	//
	// Apply mints the ability from THAT face's own compiled trigger -- the
	// TriggerPush shape, f.Triggers[idx].Effect -- never from a by-name
	// SVar walk. Two reasons, both load-bearing:
	//   - the top face may define the same SVar name with a different body
	//     (Cubwarden and Everquill Phoenix both name their token SVar
	//     TrigToken), so a top-first by-name walk steals the under-card's
	//     body; and
	//   - cards.ResolveSVar parses a FRESH *SA on every call, so an ability
	//     minted that way has no pointer identity with the compiled
	//     cards.Trigger.Effect. Every consumer that recovers a resolving
	//     ability's owning trigger does so by that pointer
	//     (findTriggerForAbilityFace), so a fresh parse silently disabled
	//     the OptionalDecider$ gate, the intervening-if recheck, the
	//     ResolvedLimit$ count, the ability's label and the merged-face
	//     SVar table at resolution. Minting the compiled pointer is what
	//     makes an under-card trigger an ordinary trigger everywhere else.
	//
	// It is a sibling of DelayedPush/GrantTriggerPush -- the minting shape
	// is GrantTriggerPush's (no registration consumed) -- appended here,
	// after Mutate, following every prior Kind's own append-only precedent,
	// so no earlier ordinal, hash chain or golden replay is affected.
	MergedTriggerPush
	// Discover records one completed discover action (CR 701.57, task
	// trigdisc1): Player is the discovering seat and Obj the resolving source
	// permanent (0 for a source-less body). It is an Apply no-op marker,
	// exactly like Explore/Investigate: the discover's own state changes (the
	// exiles, the reveal Notes, the chosen card's move) are their own events
	// that precede this one, and the record is what trig:Discover matches
	// ("whenever you discover" -- Val, Marooned Surveyor, Curator of Sun's
	// Creation). The marker is ONE per completed discover action, never one
	// per exiled card. Appended after MergedTriggerPush, still after every
	// earlier Kind, so no earlier ordinal, hash chain or golden replay is
	// affected.
	Discover
	// Seek records one completed seek action (Forge's Alchemy seek, task
	// trigdisc1): Player is the seeking seat and Obj the resolving source
	// permanent (0 for a source-less body). It is an Apply no-op marker,
	// exactly like Discover, and is what trig:SeekAll matches ("whenever you
	// seek one or more cards" -- Vexyr, Ich-Tekik's Heir; Val, Marooned
	// Surveyor; Lurker in the Deep). One marker per seek ACTION, never one
	// per sought card -- a seek of three cards is one marker and one
	// trigger -- and the emitter's contract (api:Seek, still unimplemented)
	// is to emit only when the seek actually found at least one card, so the
	// oracle's "one or more cards" holds. Appended after Discover, still
	// after every earlier Kind, so no earlier ordinal, hash chain or golden
	// replay is affected.
	Seek
	// Connive records one completed connive action (CR 702.59, task connive1):
	// Obj is the conniving permanent, Player its controller, IDs the cards it
	// discarded in discard order, and Amount the number of NONLAND cards
	// among them (the +1/+1 counters that went on the conniver). It is an
	// Apply no-op marker, exactly like Explore: the connive's own state
	// changes (the draws, the discards, the counter) are their own events
	// that precede this one, and the record is what trig:Connives matches
	// (Iron Monger Sadistic Tycoon, Glorious Purpose, Ultron Unlimited).
	// One marker per completed connive ACTION, never one per discarded card.
	// Appended after Seek, still after every earlier Kind, so no earlier
	// ordinal, hash chain or golden replay is affected.
	Connive
	// Enlist records one CR 702.160 enlist action (the `K:Enlist` keyword,
	// task enlist1): Obj is the ATTACKING creature that enlisted (the
	// trigger's source for Mode$ Enlisted, so ValidCard$ Card.Self matches
	// it) and IDs[0] the nonattacking creature it tapped, Player the
	// attacker's controller. Apply folds the per-combat stamp the
	// enlistedThisCombat filter predicate reads; the tap is its own Tap
	// event and the +X/+0 is a rules-registered continuous pump, so this
	// event is the enlist action's canonical record and the Mode$ Enlisted
	// trigger's carrier. Appended here, after Connive, following every prior
	// Kind's own append-only precedent, so no earlier ordinal, hash chain
	// or golden replay is affected.
	Enlist
	// Exploit records one completed exploit sacrifice (CR 702.58a, task
	// exploit1): Obj is the EXPLOITING creature (the permanent whose exploit
	// ability resolved), Player its controller, IDs[0] the exploited creature
	// -- the one sacrificed to pay. It is an Apply no-op marker, exactly like
	// Explore/Investigate: the sacrifice's own state change (the
	// battlefield-to-graveyard MoveZone) is its own event that precedes this
	// one, and the record is what trig:Exploited matches ("Whenever a
	// creature you control exploits a creature", Colonel Autumn; "When
	// CARDNAME exploits a creature", Graf Reaver). A declined exploit
	// election records nothing at all -- CR 702.58a's "you may sacrifice a
	// creature" means no sacrifice, no exploit. Merged here after Enlist,
	// following every prior Kind's own append-only precedent, so no earlier
	// ordinal, hash chain or golden replay is affected.
	Exploit
	// AlterAttribute records one Forge AlterAttribute application (task
	// alterattr1): Obj is the permanent whose designation changed, Text is
	// the attribute name -- the engine models exactly one, "Suspected"
	// (CR 702.157, the Blame Game precon's Nelly Borca / Hot Pursuit
	// family) -- and Amount 1 grants it, -1 removes it (the Activate$ False
	// arm, Forge's "becomes unprepared" spelling). Apply folds the
	// designation; the two CR 702.157b end conditions -- the permanent
	// leaves the battlefield, another player gains control of it -- are the
	// Move and ControlChange folds' own clears, the same blocks the
	// Ring-bearer designation's clears live in. Appended here, after
	// Exploit, following every prior Kind's own append-only precedent, so no
	// earlier ordinal, hash chain or golden replay is affected.
	AlterAttribute
	// GainedAbilityPush creates the stack object for an activated ability
	// GAINED off a foreign card (Forge's GainsAbilitiesOf$ on a Mode$
	// Continuous static, the Idris, Soul of the TARDIS shape). Like
	// AbilityPush it mints inside Apply (Ruling T20-a) so a log-only replay
	// creates the same object a live game did, but the ability is not a
	// Face().Abilities index of the recipient: Obj is the recipient (the
	// minted object's Source, so `Defined$ Self`/`CARDNAME` names it),
	// IDs[0] is the FOREIGN card's object id, and Amount is the index of the
	// ability in that card's Face().Abilities. The body is the foreign
	// face's compiled SA, so a replay re-resolves the identical pointer (the
	// MergedTriggerPush reasoning: pointer identity is what the
	// activation-limit census and the owning-face SVar reads rely on). A
	// foreign card that left the scoped zone, or a stale index, mints
	// nothing. Appended here, after AlterAttribute, following every prior
	// Kind's own append-only precedent, so no earlier ordinal, hash chain or
	// golden replay is affected.
	GainedAbilityPush
	// GainedTriggerPush creates the stack object for a triggered ability
	// GAINED off a foreign card (Forge's GainsTriggerAbsOf$ on a Mode$
	// Continuous static). Obj is the recipient (the minted object's Source),
	// IDs[0] is the foreign card's object id, Amount is the index of the
	// trigger in that card's Face().Triggers, and Counter carries the
	// trigger's Execute$ name as readable provenance checked at Apply the way
	// MergedTriggerPush checks its own: a truncated or tampered log mints
	// nothing rather than the wrong ability. Appended here, after
	// GainedAbilityPush, following every prior Kind's own append-only
	// precedent, so no earlier ordinal, hash chain or golden replay is
	// affected.
	GainedTriggerPush
	// Surveil records one completed surveil instruction (CR 701.42, task
	// trig-surveil): Player is the surveiling seat and Obj the resolving
	// source permanent (0 for a source-less body). It is an Apply no-op
	// marker, exactly like Explore/Investigate: the surveil's own state
	// changes (the KArrange answer's LibraryOrder and any graveyard
	// MoveZones) are their own events that follow this one, and the record
	// is what trig:Surveil matches ("whenever you surveil" -- Mirko,
	// Obsessive Theorist; Dimir Spybug; Thoughtbound Phantasm; Whispering
	// Snitch). One marker per surveil instruction per acting player, emitted
	// by api:Surveil (effects/cardflow.go effSurveil) before the arrangement
	// -- the trigger bodies queue and resolve after the surveil spell or
	// ability finishes either way. Appended here, after GainedTriggerPush
	// (main's own append while this branch carried Surveil after
	// AlterAttribute; the merge keeps main's ordinals intact and appends the
	// branch's Kind after them, still after every earlier Kind), following
	// every prior Kind's own append-only precedent, so no earlier ordinal,
	// hash chain or golden replay is affected.
	Surveil
	// Unattached records an Aura/Equipment permanent Obj becoming detached
	// from the permanent it was attached to (CR 701.3b), on a path where Obj
	// itself stays on the battlefield: the attachmentSBAs detach arms in
	// rules/attach.go (the bearer left, the bearer stopped being a valid
	// bearer, or protection) and the CR 702.114b bestowed type switch. It is
	// deliberately SEPARATE from Attach's empty-IDs detach shape, because
	// Mode$ Unattached is a real trigger mode (the Grafted Exoskeleton family)
	// and Mode$ Attached must keep ignoring a detach -- a distinct Kind is
	// what lets the two modes' event masks stay exact. IDs holds the former
	// bearer (the object Obj became unattached FROM), which is what the
	// trigger's TriggeredObjectLKICopy referent resolves; Text carries the
	// detach reason for the transcript, exactly as Attach's detach shape does.
	// Apply clears Obj's AttachedTo (the same fold an empty-IDs Attach makes).
	// Appended here, after Surveil (main appended GainedAbilityPush,
	// GainedTriggerPush and Surveil after AlterAttribute while this branch
	// carried Unattached there; the merge keeps main's ordinals intact and
	// appends the branch's Kind after them), following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	Unattached
	// PlayerNoted records a player-notation write (Forge's `NoteCards$
	// <defined> | NoteCardsFor$ <label>` on a DB$ Pump body -- Seize the
	// Spotlight's fame/fortune branches, Master of Ceremonies' money/
	// friends/secrets). Player is the seat the note lands on and Text is the
	// label; events.Apply appends Text to that seat's state.Player.Notes
	// (idempotent), which `Player.NotedFor<label>` reads from the shared
	// player filter. It is a dedicated Kind rather than a Note marker because
	// the notation is real game state a later resolution reads -- a plain Note
	// is transcript-only and Apply writes nothing for it, so a log-only replay
	// could not rebuild the note. The CARD half of the parameter family
	// (`NoteCards$ Self` read back by `Card.NotedFor<label>`) is a separate
	// family and does not ride this event. Appended here, after Unattached,
	// following every prior Kind's own append-only precedent, so no earlier
	// ordinal, hash chain or golden replay is affected.
	PlayerNoted
	// PlayerNoteCleared removes one ClearNotedCardsFor$ label from a player
	// (a DB$ Pump body's ClearNotedCardsFor$ parameter, folded by events.Apply
	// out of the same state.Player.Notes slice). Appended after PlayerNoted to
	// preserve every earlier kind ordinal.
	PlayerNoteCleared
	// DelayedRemove consumes a delayed-trigger registration that can no longer
	// resolve (CR 603.7). Amount is the registration ID. It is an event rather
	// than a direct slice edit so replay folds the same garbage collection.
	// Appended after PlayerNoteCleared to preserve earlier event ordinals.
	DelayedRemove
	// TurnFaceUp records a face-down battlefield permanent being turned face
	// up (CR 708.6 / CR 702.36e): Obj is the permanent. Apply clears its
	// FaceDown marker (and the folded face-down set), which makes its printed
	// characteristics live again and is what a Mode$ TurnFaceUp trigger
	// matches against. It is emitted by effects.effSetState's Mode$
	// TurnFaceUp arm (the effect-driven turn-up the corpus's AB$ SetState
	// lines carry); the morph/disguise special action that would also emit it
	// is a separate subsystem. A Kind rather than a FlipFace rider: a
	// transform changes the active FACE, while a turn-up only reveals the
	// face that was already current, so the two must not share an event.
	// Appended after DelayedRemove, following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	TurnFaceUp
	// SearchedLibrary records one completed search of a library (CR 701.23;
	// trig:SearchedLibrary). Player is the player whose library was searched
	// and Obj is the resolving source, if any. It is a pure marker: the
	// search's card moves and shuffle are recorded by their own events.
	// Appended after TurnFaceUp to preserve all earlier Kind ordinals.
	SearchedLibrary
	// KeywordAbilityPush mints the stack object of a keyword-GRANTED activated
	// ability (CR 613.1f): a layer-6 `AddKeyword$ Cycling:1 U` grant (Tectonic
	// Reformation, Rhet-Tomb Mystic, Jo Grant, Homing Sliver) gives a hand card
	// a cycling ability no printed face carries, so its activation cannot be
	// anchored on a Face().Abilities index (AbilityPush) or an SVar name
	// (DelayedPush/GrantAbilityPush -- a granted keyword lives in no face's
	// SVar table). Player is the activator, Obj the activating card (the
	// minted object's Source, so `Defined$ Self`/`CARDNAME` in the body names
	// it), and Counter carries the DERIVED keyword line the activation resolved
	// ("Cycling:1 U", "TypeCycling:Sliver:3") -- Apply re-synthesizes the body
	// from it, exactly as the offer loop and pendingCast re-derive it, so a
	// log-only replay creates the identical ability object (the Ruling
	// T20-a/GainedAbilityPush precedent). Appended after SearchedLibrary,
	// following every prior Kind's own append-only precedent, so no earlier
	// ordinal, hash chain or golden replay is affected.
	KeywordAbilityPush
	// Scry records one completed scry instruction's final partition
	// (CR 701.18, task scrybottom): Player is the scrying seat and Obj the
	// resolving source permanent (0 for a source-less body); Amount is the
	// number of cards the player actually chose to put on the BOTTOM of
	// their library (0 when every looked-at card was kept on top). It is an
	// Apply no-op marker, exactly like Surveil/Discover: the scry's own
	// state change (the KArrange answer's LibraryOrder) is its own event
	// that follows this one, and the record is what trig:Scry matches. It is
	// emitted by rules' handleArrange AFTER the KArrange answer is known --
	// never at the look -- so a "whenever you choose to put one or more
	// cards on the bottom" trigger (The Temporal Anchor's `ToBottom$ True`)
	// sees the count actually bottomed rather than the number looked at, and
	// fires not at all when the answer bottomed none. Appended after
	// KeywordAbilityPush, following every prior Kind's own append-only
	// precedent, so no earlier ordinal, hash chain or golden replay is
	// affected.
	Scry
	// StoreSVar records one api:StoreSVar write -- Forge's sa.setSVar, the
	// ability-body SVar write primitive. Obj is the object whose runtime SVar
	// table is written (the resolving body's source: Minion of the Wastes /
	// Phyrexian Processor's entering permanent), Text is the SVar name
	// (SVar$ LifePaidOnETB), and Amount is the resolved integer value. Apply
	// folds it into Object.RuntimeSVars, which overlays the printed face SVar
	// table for the CDA/count/token reads that consume it; a later
	// StoreSVar of the same name on the same object overwrites (Forge's
	// setSVar is last-write-wins). The value must be event-backed so a replay
	// derives the identical board, and the fold is a keyed map write, so no
	// map range ever reaches an event. Appended after Scry to preserve every
	// earlier Kind ordinal, hash chain and golden replay.
	StoreSVar
	// TurnFaceDown records a battlefield permanent being turned face down by
	// SetState. Appended after StoreSVar to preserve prior event ordinals.
	TurnFaceDown
	// CloneStatic appends one named SVar static to a copy's layer-1 face.
	// Text is the original SVar body; appended to preserve existing ordinals.
	CloneStatic
	// DamageProvenance records one game-long (recipient, source) damage fact:
	// Obj is the damage SOURCE, IDs[0] is the recipient (a plain ObjID for an
	// object, a state.PlayerRef-encoded PlayerID for a seat -- the TriggerPush
	// encoding, so PlayerID 0 stays distinguishable from "no recipient"), and
	// Amount is the damage that actually landed post-replacement/post-
	// protection. Appended after CloneStatic to preserve prior event ordinals
	// and the hash chain.
	DamageProvenance
	// EnduringStoryChange records a seat gaining the CR 702.175 designation.
	EnduringStoryChange
	// PhaseOut records CR 702.25's phased-out/phase-in status on a
	// battlefield permanent (api:Phases). Obj is the permanent and Amount 1
	// phases it OUT, -1 phases it IN. It is deliberately NOT a MoveZone:
	// phasing is not a zone change (the permanent stays on the battlefield
	// and no leaves/enters event fires), it only makes the permanent
	// invisible to everything that treats a permanent as existing, and it
	// phases in at its controller's next untap step. Apply folds it into
	// Object.PhasedOut, which the Move fold clears when the permanent
	// actually leaves the battlefield. Appended after CloneStatic, the last
	// pre-existing Kind, so no earlier ordinal, hash chain or golden replay
	// is affected.
	PhaseOut
	// GiftPromise records the cast-time CR 702.168 Gift election: whether the
	// caster promised an opponent a gift and which opponent. Obj is the
	// spell on the stack (the object the promise attaches to), Player is the
	// promised opponent when one was named, and Amount is 1 for a promise
	// and 0 for a decline. Apply folds it onto Object.CastFlags /
	// GiftPromisedTo, which the PromisedGift filter predicate, the
	// Count$PromisedGift head and Defined$ Promised all read. It is a real
	// state-bearing event rather than a Choose marker because the promise
	// SURVIVES the stack->battlefield move (a permanent's gift is given on
	// entry) and must be replay-derived. Appended after
	// PhaseOut, following every prior Kind's own append-only precedent, so no
	// earlier ordinal, hash chain or golden replay is affected.
	GiftPromise
	// GiveGift records one completed gift action (CR 702.168b): Player is
	// the giver (the cast spell's controller) and Obj the resolving source
	// (the spell on the stack, or the permanent whose ETB gift resolved). It
	// is an Apply no-op marker, exactly like Investigate: the gift's own
	// state change (the drawn card, the created token) is its own event that
	// precedes this one, and the record is what trig:GiveGift matches
	// ("whenever you give a gift" -- Jolly Gerbils). The marker is separate
	// from the gift's own effects so an unrelated draw or token creation
	// never fires a gift trigger. Appended after GiftPromise, still after
	// every earlier Kind, so no earlier ordinal, hash chain or golden replay
	// is affected.
	GiveGift
	// RollDice is the SYNTHETIC roll-action PROPOSAL rules' RollDiceProposed
	// hook holds out to R:Event$ RollDice replacement matching before any die
	// of one roll action is rolled (CR 614.4's before-the-action window, task
	// rolldice-repl; the Scry proposal's discipline). Player is the roller,
	// Obj the rolling source, Amount the proposed dice count and Counter the
	// proposed ignored-low count (decimal, "" = none). It is NEVER emitted --
	// the roll's log witnesses are the per-die and batch Notes the ordinary
	// effRollDice path emits -- so no log event ever carries this Kind and
	// every earlier ordinal, the hash chain and the golden replays are
	// untouched. Appended after GiveGift, following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	RollDice
	// Proliferate records one completed proliferate action (CR 701.27, task
	// trig-proliferate): Player is the proliferating seat ("whenever YOU
	// proliferate" -- what ValidPlayer$ matches) and Obj the resolving source
	// permanent (the spell or activated ability on the stack). It is an Apply
	// no-op marker, exactly like Surveil/Discover: the proliferation's own
	// state change (the CounterChange / PlayerCounterChange batch per chosen
	// recipient) is its own event that precedes this one, and the record is
	// what trig:Proliferate matches (Voidwing Hybrid, Ezuri Stalker of
	// Spheres, Scheming Aspirant and the mode's other corpus carriers). The
	// marker exists because a CounterChange cannot be the signal -- a
	// proliferate with no eligible recipient still happened, while an ordinary
	// counter addition is not a proliferate action -- and it is emitted ONCE
	// per completed action, never once per recipient or counter. Appended
	// after RollDice, following every prior Kind's own append-only precedent,
	// so no earlier ordinal, hash chain or golden replay is affected.
	Proliferate
	// Evolved records one completed evolve keyword action (CR 702.99b): the
	// Evolve keyword ability resolved and put its +1/+1 counter on the
	// evolving creature. It is a pure Apply no-op marker, exactly like
	// GiveGift/Investigate -- the counter placement is its own CounterChange
	// event, and this record is what trig:Evolved matches ("whenever this
	// creature evolves"). Obj is the evolving permanent (the Evolve trigger's
	// own source, so ValidCard$ Card.Self matches) and Player its controller.
	// A separate Kind rather than a CounterChange rider because an unrelated
	// +1/+1 counter must not fire the mode. Appended after Proliferate, still
	// after every earlier Kind, so no earlier ordinal, hash chain or golden
	// replay is affected.
	Evolved
	// DelayedForget drops one remembered target from an Effect trigger.
	// Amount identifies its registration; Obj is the remembered card.
	// Appended after Evolved, still above NumKinds, so no earlier ordinal,
	// hash chain or golden replay is affected.
	DelayedForget
	// CardNoted records a card-notation write (Forge's NoteCardsEffect with
	// `NoteCards$ Remembered | NoteCardsFor$ <label>` -- Volatile Chimera,
	// Arcane Savant, Caller of the Untamed -- or `NoteCards$ TriggeredSource`
	// -- Maelstrom Archangel Avatar): the resolution notes the CARD it
	// carried (its Remembered set, or the triggering source) so a later
	// resolution reads it back through the shared card filter's
	// `Card.NotedFor<label>` qualifier (ChooseCard's Choices$, DB$ Play's
	// Valid$ and RepeatEach's RepeatCards$). CopyPermanent's RevealFromExile
	// cost is an evidenced corpus shape but remains unsupported. Obj is the
	// noted object and Text the label; events.Apply
	// appends Text to that object's state.Object.Notes (idempotent), which the
	// filter reads. It is a dedicated Kind rather than a Note marker because
	// the notation is real game state a later resolution reads. It is the
	// object-side sibling of PlayerNoted (the `NoteCards$ Self` player half)
	// and does not touch state.Player.Notes. A note is card-identity
	// provenance, not zone-local state: the noted card sits in exile for the
	// setup-path carriers, so the fold never clears on a zone move.
	// Appended after DelayedForget, following every prior Kind's own
	// append-only precedent, so no earlier ordinal, hash chain or golden
	// replay is affected.
	CardNoted
	// Cascade is the cascade instruction's replacement PROPOSAL boundary
	// (Averna, the Chaos Bloom's R:Event$ Cascade). It is never emitted: the
	// rules tier holds it out to the replacement matcher exactly as Engine.Scry
	// holds events.Scry, and the IDs carry the ordered exiled batch the body's
	// ReplacedCards selector reads. Appended after CardNoted, preserving every
	// earlier ordinal, hash chain and golden replay.
	Cascade
	// Clash records one clashing player's win/lose result from a completed
	// CR 701.31 clash action (Forge's TriggerType.Clashed). Obj is the
	// resolving source permanent (0 for a source-less body), Player is the
	// clashing seat this record reports, and Amount is 1 when that player WON
	// the clash and 0 when they lost (or tied -- CR 701.31 treats a tie as no
	// winner, and Forge's ClashEffect fires the trigger for both clashing
	// players with Won$ False on a tie). The reveal and the top/bottom
	// placements are their own preceding Notes/MoveZone/LibraryOrder events;
	// this is a pure Apply no-op marker, exactly like Explore/Investigate /
	// GiveGift, so an unrelated reveal never fires a clash trigger. effClash
	// emits ONE record per clashing player (Forge fires runTrigger once per
	// player), which is what the `Won$ True`/`Won$ False` orientation of
	// trig:Clashed reads. Appended after Cascade, still above NumKinds,
	// following every prior Kind's own append-only precedent, so no earlier
	// ordinal, hash chain or golden replay is affected.
	Clash
	// PlanarDeckShuffle records the seeded order of one seat's private planar
	// deck. Appended after Clash (the kinds main already carries), following
	// every prior Kind's own append-only precedent, so no earlier ordinal, hash
	// chain or golden replay is affected.
	PlanarDeckShuffle
	// PlanarReveal publicly turns the top planar-deck card face up.
	// Appended here after PlanarDeckShuffle; earlier ordinals remain stable.
	PlanarReveal
	// PlanarWalk planeswalks a seat to a plane (CR 901.8): Player is the
	// walking seat, Obj the plane being walked AWAY from (0 when there is
	// none, so the away trigger has no source), and IDs the destination
	// plane(s) of a Defined$ planeswalk (empty for the ordinary rotation,
	// which takes the next plane of the deck). Amount carries
	// PlanarWalkDontPlaneswalkAway when the walk's DontPlaneswalkAway$ True
	// suppresses the away-from trigger (Norn's Seedcore). Appended here
	// after PlanarReveal; earlier ordinals remain stable.
	PlanarWalk
	// Specialize records a permanent's chosen specialization face. Amount is
	// the destination face index; Apply bounds-checks it just like FlipFace.
	Specialize
	// ChaosEnsues marks the moment chaos ensues on a seat's current plane
	// (CR 901.9): Player is the seat whose planar deck owns the plane, Obj
	// that seat's current plane object. It is an Apply no-op marker like
	// PlanarRoll: the plane's chaos ability is an ordinary triggered ability
	// (Mode$ ChaosEnsues) that the trigger walk queues when this marker is
	// checked. Emitted by rules when a kept planar-die result shows the chaos
	// face, and by effects' DB$ ChaosEnsues (the "Will of the Planeswalkers"
	// cycle). Appended here after Specialize; earlier ordinals remain stable.
	ChaosEnsues
	// NumKinds is the explicit upper bound for the append-only event kind
	// registry below. New kinds must be appended above this line: inserting or
	// reordering a kind renumbers the hash-chained event stream and breaks replay.
	NumKinds = int(ChaosEnsues) + 1
)

// PlanarWalkDontPlaneswalkAway is PlanarWalk's Amount flag: the resolving
// DB$ Planeswalk carried DontPlaneswalkAway$ True, so the walk does not fire
// the walked-away-from plane's PlaneswalkedFrom ability (CR 901.8; Norn's
// Seedcore's "don't planeswalk away from any plane"). It is not a count, so
// the only defined values are 0 (absent) and this flag.
const PlanarWalkDontPlaneswalkAway int32 = 1

// mergedTriggerShift is the width MergedTriggerPush's Amount gives the
// under-card's own Triggers index; the pile index sits above it. Both are
// small non-negative card-script indices (a pile is a handful of cards, a
// face a handful of T: lines), so 16 bits each is far beyond any real value
// and the packing stays inside int32 with room to spare.
const mergedTriggerShift = 16

// MergedTriggerAmount packs a MergedTriggerPush's Amount: mergedIdx is the
// under-card's position in the pile's MergedCards (top-of-pile first) and
// trigIdx is that under-card face's own Triggers index. A negative or
// oversized index yields -1, which MergedTriggerIndexes reports as invalid
// so Apply degrades the push to a no-op rather than minting a wrong ability.
func MergedTriggerAmount(mergedIdx, trigIdx int) int32 {
	if mergedIdx < 0 || trigIdx < 0 ||
		mergedIdx >= 1<<mergedTriggerShift || trigIdx >= 1<<mergedTriggerShift {
		return -1
	}
	return int32(mergedIdx)<<mergedTriggerShift | int32(trigIdx)
}

// MergedTriggerIndexes unpacks MergedTriggerAmount. ok is false for a value
// this build cannot read (a negative Amount -- a tampered or truncated log),
// which every caller treats as "mint nothing".
func MergedTriggerIndexes(amount int32) (mergedIdx, trigIdx int, ok bool) {
	if amount < 0 {
		return 0, 0, false
	}
	return int(amount >> mergedTriggerShift), int(amount & (1<<mergedTriggerShift - 1)), true
}

// CopyToken's Amount rider bitmask (DB$ CopyPermanent's entry-state
// riders, folded in Apply so replay derives the identical object):
// TokenTapped$ True, TokenAttacking$ True, and AtEOT$ ExileCombat -- the
// latter flags the copy IsMyriad so the existing end-of-combat cleanup
// (MyriadCleanup, CR 702.109a) exiles it with the same semantics every
// Myriad token already had: end-of-combat exile, battlefield only.
const (
	CopyTokenTapped      int32 = 1
	CopyTokenAttacking   int32 = 2
	CopyTokenExileCombat int32 = 4
)

// ExtraPhaseRiders is the rider payload an api:AddPhase grant forwards on
// its ExtraPhase event, Text-encoded (Ruling T20-a's field-reuse precedent
// -- the event gains no field), joined with "|":
//
//   - "DELAY=<step ordinal>" + "VP=<ValidPlayer$ value>": the grant's
//     ExtraPhaseDelayedTrigger$ pair (Moraug's "at the beginning of that
//     combat, untap all creatures you control"). The delayed phase cannot
//     ride the IDs slice beside the entry/FollowedBy steps: an absent rider
//     and the zero Step (untap) would be indistinguishable, so the riders
//     live in Text and the IDs slots stay unambiguous (IDs[0] the entry
//     step, IDs[1] an explicit FollowedBy$ only).
//   - "RANGEEND=<step ordinal>": a multi-step ExtraPhase$ value whose range
//     is not the entry's default (the fold derives Entry's own range through
//     state.ExtraPhaseRangeEnd); the rider names the extra phase's LAST step
//     so a whole named phase ("Upkeep,Draw", "Untap->Combat Damage") walks
//     its full range. Only the +1 grant carries it -- the consume/complete
//     messages match on the fold's own stored RangeEnd.
type ExtraPhaseRiders struct {
	HasDelayedPhase bool
	DelayedPhase    state.Step
	HasRangeEnd     bool
	RangeEnd        state.Step
	ValidPlayer     string
}

const (
	extraPhaseDelayKey = "DELAY="
	extraPhaseRangeKey = "RANGEEND="
	extraPhaseVPKey    = "VP="
)

// EncodeExtraPhaseRiders writes the rider payload as the canonical Text
// marker. Deterministic key order (DELAY, RANGEEND, VP), so the same riders
// always encode identically.
func EncodeExtraPhaseRiders(r ExtraPhaseRiders) string {
	var parts []string
	if r.HasDelayedPhase {
		parts = append(parts, extraPhaseDelayKey+strconv.FormatInt(int64(r.DelayedPhase), 10))
	}
	if r.HasRangeEnd {
		parts = append(parts, extraPhaseRangeKey+strconv.FormatInt(int64(r.RangeEnd), 10))
	}
	if r.ValidPlayer != "" {
		parts = append(parts, extraPhaseVPKey+r.ValidPlayer)
	}
	return strings.Join(parts, "|")
}

// DecodeExtraPhaseRiders reads the rider payload back; the zero value (no
// riders) for any other Text.
func DecodeExtraPhaseRiders(text string) ExtraPhaseRiders {
	var r ExtraPhaseRiders
	for part := range strings.SplitSeq(text, "|") {
		if v, ok := strings.CutPrefix(part, extraPhaseDelayKey); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 && state.Step(n).Valid() {
				r.HasDelayedPhase, r.DelayedPhase = true, state.Step(n)
			}
			continue
		}
		if v, ok := strings.CutPrefix(part, extraPhaseRangeKey); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 && state.Step(n).Valid() {
				r.HasRangeEnd, r.RangeEnd = true, state.Step(n)
			}
			continue
		}
		if v, ok := strings.CutPrefix(part, extraPhaseVPKey); ok {
			r.ValidPlayer = strings.TrimSpace(v)
		}
	}
	return r
}

// kindNames is declared with NumKinds's length, never [...] inferred, so
// kindNames and the enum cannot drift apart: a Kind added without a name (or
// a name added without a Kind) is a compile error, the same lockstep
// zoneNames has with numZones.
var kindNames = [NumKinds]string{"game_start", "shuffle", "move_zone", "draw",
	"life", "damage", "tap", "untap", "step", "turn", "priority", "stack_push",
	"stack_resolve", "mana_add", "mana_clear", "counter", "declare_attackers",
	"declare_blockers", "player_lost", "game_over", "decision_ask",
	"decision_made", "note", "land_played", "targets_chosen", "flip_face",
	"clock_tick", "trigger_push", "end_combat_reset", "cast_info", "choose",
	"token_create", "stack_copy", "attach", "ability_push", "mode_chosen", "commander_damage",
	"delayed_register", "delayed_push", "library_order", "extra_turn", "door_unlock", "speed_change",
	"monarch_change", "control_change", "card_token", "keyword_trigger_push", "goad", "player_counter", "imprint", "starting_player_change",
	"pair", "myriad_copy", "myriad_cleanup", "grant_trigger_push", "mana_activate", "token_attacks",
	"x_change", "note_number", "extra_phase", "copy_token", "exert", "planar_roll", "explore", "combat_retarget", "ring_tempts_you", "ring_emblem_push", "grant_ability_push", "investigate", "blessing_change", "clone_permanent", "mutate", "merged_trigger_push",
	"discover", "seek", "connive", "enlist", "exploit", "alter_attribute",
	"gained_ability_push", "gained_trigger_push", "surveil", "unattached", "player_noted", "player_note_cleared",
	"delayed_remove", "turn_face_up", "searched_library", "keyword_ability_push", "scry", "store_svar", "turn_face_down", "clone_static",
	"damage_provenance", "enduring_story_change", "phase_out", "gift_promise", "give_gift", "roll_dice",
	"proliferate", "evolved", "delayed_forget", "card_noted", "cascade", "clash",
	"planar_deck_shuffle", "planar_reveal", "planar_walk", "specialize", "chaos_ensues"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

// ExtraTurnSkipUntapText is the canonical Text marker on an ExtraTurn grant
// whose Forge AddTurn source carries SkipUntap$ True. Text is already part of
// Event's encoded union, so it preserves this turn-specific rider without a
// schema change.
const ExtraTurnSkipUntapText = "extra turn; skip untap"

// ExtraTurnSkippedText marks a -1 ExtraTurn consumption whose BeginTurn
// replacement skipped the granted turn. It distinguishes that bookkeeping
// consumption from one immediately followed by a TurnChange, so the ordinary
// rotation scan does not mistake the next normal turn for an extra turn.
const ExtraTurnSkippedText = "extra turn; skipped"

// Event is a state delta. The field set is a flat union so encoding stays
// allocation-free and an external consumer needs no engine code to read it.
// manaRestrictionPrefix marks a ManaAdd event whose added (or spent) mana is
// governed by RestrictValid$. Text is otherwise unused by ManaAdd, so this
// preserves the event wire shape while keeping the provenance replayable.
const manaRestrictionPrefix = "mana-restriction:"

// ManaRestrictionText encodes a RestrictValid$ constraint on a ManaAdd event.
// The optional " @<source-id>" segment (appended only when the producing
// source is known and nonzero, so every historical encoding stays
// byte-identical) records WHICH permanent's ability produced the batch — the
// source-relative predicates a Valid value may carry (Cavern of Souls'
// ChosenType) are resolved against it, never against the card being paid for.
func ManaRestrictionText(valid string, source state.ObjID) string {
	if source == 0 {
		return manaRestrictionPrefix + valid
	}
	return manaRestrictionPrefix + valid + " @" + strconv.FormatUint(uint64(source), 10)
}

// The AddsNoCounter$ suffixes. They are appended AFTER the optional source
// segment, and only when the producing ability carries AddsNoCounter$, so
// every historical encoding stays byte-identical. " nc" is the plain flag
// (Cavern of Souls); " nc!Permanent" is Forge's conditional
// AddsNoCounter$ !Permanent (Boseiju: the spell must not be a permanent).
const (
	manaRestrictionNC        = " nc"
	manaRestrictionNCNotPerm = " nc!Permanent"
)

// manaRestrictionACPrefix introduces the AddsCounters$ rider segment. The
// rider value is base64-encoded so an arbitrary Forge parameter string (it may
// hold spaces, `|`, `+`, `_`, `$`) round-trips through the Text encoding
// unambiguously; the segment is appended after the source/AddsNoCounter$
// segments and before the PersistentMana$ suffix, and only when the producing
// ability carries a rider, so every historical encoding stays byte-identical.
const manaRestrictionACPrefix = " ac="

// ManaAddsCountersText appends the AddsCounters$ rider segment to a ManaAdd
// event's provenance encoding (which may already carry the restriction, the
// source and the AddsNoCounter$ condition). An empty rider appends nothing.
func ManaAddsCountersText(text, rider string) string {
	if rider == "" {
		return text
	}
	return text + manaRestrictionACPrefix + base64.StdEncoding.EncodeToString([]byte(rider))
}

// ManaRestrictionTextNC is ManaRestrictionText for a batch whose producing
// ability also carries AddsNoCounter$. cond is "True" (the plain flag) or
// "NotPermanent" (AddsNoCounter$ !Permanent); an empty cond encodes the plain
// historical shape.
func ManaRestrictionTextNC(valid string, source state.ObjID, cond string) string {
	base := ManaRestrictionText(valid, source)
	switch cond {
	case "NotPermanent":
		return base + manaRestrictionNCNotPerm
	case "True":
		return base + manaRestrictionNC
	default:
		return base
	}
}

// manaPersistentSuffix marks a ManaAdd event whose added mana carries
// PersistentMana$ True (Forge): the mana does not empty as steps and phases
// end (CR 500.4 with the card's exception, e.g. Rousing Refrain, Savage
// Ventmaw), until the turn ends — events.Apply's TurnChange fold expires it.
// The suffix rides Text after every other encoding (the restriction prefix
// and its optional source/nc segments), so it composes with a restricted
// batch (Klauth's PersistentMana$ True | RestrictValid$ Spell) and ordinary
// historical ManaAdd events never carry it.
const manaPersistentSuffix = " pm"

// ManaPersistentText appends the PersistentMana$ True marker to a ManaAdd
// event's Text encoding (which may already carry the restriction encoding).
// On a POSITIVE add the marker raises Player.PersistentMana with the pool
// unit; on a negative PLAIN spend event the marker names the persistent share
// the payment consumed (the payment path attributes a slot's units
// ordinary-first over its visible pool and splits a spend that spans both
// shares into an unmarked and a marked event), and on a restricted spend the
// marker is meaningless — the consumed batch's own Persistent flag is
// authoritative.
func ManaPersistentText(text string) string { return text + manaPersistentSuffix }

// ManaRestrictionFromText returns the constraint carried by a restricted
// ManaAdd event, with the producing source id when the encoding carries one
// (0 otherwise), the AddsNoCounter$ condition when one is encoded (""), and
// the producing ability's AddsCounters$ rider value when one is encoded ("").
// It deliberately accepts no aliases: ordinary historical ManaAdd events must
// remain unrestricted. A bare empty Valid with a condition or a rider still
// counts as a restriction batch (the batch is unrestricted spend-wise but
// carries the provenance).
func ManaRestrictionFromText(text string) (valid string, source state.ObjID, cond, addsCounters string, restricted bool) {
	valid, ok := strings.CutPrefix(text, manaRestrictionPrefix)
	if !ok || valid == "" {
		return "", 0, "", "", false
	}
	if head, tail, found := strings.Cut(valid, manaRestrictionACPrefix); found {
		if raw, err := base64.StdEncoding.DecodeString(tail); err == nil {
			addsCounters = string(raw)
			valid = head
		}
	}
	if s, found := strings.CutSuffix(valid, manaRestrictionNCNotPerm); found {
		cond, valid = "NotPermanent", s
	} else if s, found := strings.CutSuffix(valid, manaRestrictionNC); found {
		cond, valid = "True", s
	}
	if _, tail, found := strings.Cut(valid, " @"); found {
		head, _, _ := strings.Cut(valid, " @")
		if n, err := strconv.ParseUint(tail, 10, 64); err == nil {
			return head, state.ObjID(n), cond, addsCounters, true
		}
	}
	return valid, 0, cond, addsCounters, true
}

// manaAddsCounterGrantFields is the field separator in a CastInfo's rider
// payload and manaAddsCounterGrantRecords the record separator. Both are
// control bytes that strconv.Quote escapes inside a field, so no field value
// can forge a boundary.
const (
	manaAddsCounterGrantFields  = "\x1f"
	manaAddsCounterGrantRecords = "\x1e"
)

// ManaAddsCounterGrantsText encodes a cast's AddsCounters$ rider grants into
// the pay-time CastInfo's Text payload. The encoding is deterministic (a
// fixed field/record order and strconv.Quote per string) and base64-armoured,
// so the log carries the exact producing-ability snapshots and a replay
// reconstructs identical grants. An empty slice encodes to "" so unrelated
// casts stay byte-identical.
func ManaAddsCounterGrantsText(grants []state.ManaAddsCounterGrant) string {
	if len(grants) == 0 {
		return ""
	}
	recs := make([]string, 0, len(grants))
	for _, g := range grants {
		recs = append(recs, strings.Join([]string{
			strconv.Quote(g.Filter),
			strconv.Quote(g.Kind),
			strconv.Quote(g.Amount),
			strconv.Itoa(int(g.Count)),
		}, manaAddsCounterGrantFields))
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(recs, manaAddsCounterGrantRecords)))
}

// ManaAddsCounterGrantsFromText decodes the rider payload
// ManaAddsCounterGrantsText wrote. A malformed payload, an unresolvable field
// or a non-positive count yields no grant (fail closed) rather than a guessed
// one; an empty payload is the ordinary no-rider cast.
func ManaAddsCounterGrantsFromText(text string) []state.ManaAddsCounterGrant {
	if text == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil
	}
	var out []state.ManaAddsCounterGrant
	for _, rec := range strings.Split(string(raw), manaAddsCounterGrantRecords) {
		fields := strings.Split(rec, manaAddsCounterGrantFields)
		if len(fields) != 4 {
			return nil
		}
		filter, err1 := strconv.Unquote(fields[0])
		kind, err2 := strconv.Unquote(fields[1])
		amount, err3 := strconv.Unquote(fields[2])
		n, err4 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || n <= 0 {
			return nil
		}
		out = append(out, state.ManaAddsCounterGrant{Filter: filter, Kind: kind, Amount: amount, Count: int32(n)})
	}
	return out
}

type Event struct {
	Seq     uint64           `json:"seq"`
	Kind    Kind             `json:"kind"`
	Player  state.PlayerID   `json:"player,omitempty"`
	Obj     state.ObjID      `json:"obj,omitempty"`
	From    state.Zone       `json:"from,omitempty"`
	To      state.Zone       `json:"to,omitempty"`
	Amount  int32            `json:"amount,omitempty"`
	Step    state.Step       `json:"step,omitempty"`
	Counter string           `json:"counter,omitempty"`
	Text    string           `json:"text,omitempty"`
	IDs     []state.ObjID    `json:"ids,omitempty"`
	Pairs   [][2]state.ObjID `json:"pairs,omitempty"`
	// Secret marks an event whose payload is hidden information. View
	// projection redacts it for everyone but Player.
	Secret bool `json:"secret,omitempty"`
}

// Append writes a compact, deterministic encoding of e to dst. Every field is
// included and length-prefixed where variable, so two different events can
// never encode identically.
func (e Event) Append(dst []byte) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], e.Seq)
	dst = append(dst, b[:]...)
	dst = append(dst, byte(e.Kind), byte(e.Player), byte(e.From), byte(e.To), byte(e.Step))
	binary.LittleEndian.PutUint32(b[:4], uint32(e.Obj))
	dst = append(dst, b[:4]...)
	binary.LittleEndian.PutUint32(b[:4], uint32(e.Amount))
	dst = append(dst, b[:4]...)
	if e.Secret {
		dst = append(dst, 1)
	} else {
		dst = append(dst, 0)
	}
	dst = appendStr(dst, e.Counter)
	dst = appendStr(dst, e.Text)
	binary.LittleEndian.PutUint32(b[:4], uint32(len(e.IDs)))
	dst = append(dst, b[:4]...)
	for _, id := range e.IDs {
		binary.LittleEndian.PutUint32(b[:4], uint32(id))
		dst = append(dst, b[:4]...)
	}
	binary.LittleEndian.PutUint32(b[:4], uint32(len(e.Pairs)))
	dst = append(dst, b[:4]...)
	for _, p := range e.Pairs {
		binary.LittleEndian.PutUint32(b[:4], uint32(p[0]))
		dst = append(dst, b[:4]...)
		binary.LittleEndian.PutUint32(b[:4], uint32(p[1]))
		dst = append(dst, b[:4]...)
	}
	return dst
}

func appendStr(dst []byte, s string) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(len(s)))
	dst = append(dst, b[:]...)
	return append(dst, s...)
}

// flagNames pairs each CastFlags bit with the name CastInfo's Counter field
// carries for it, in a fixed order -- so FlagsString(FlagsFrom(s)) is
// canonical (a determinism requirement: this order must never depend on map
// iteration).
var flagNames = [...]struct {
	name string
	bit  uint64
}{
	{"kicked", state.FlagKicked},
	{"surged", state.FlagSurged},
	{"flashback", state.FlagFlashback},
	{"miracle", state.FlagMiracle},
	// Appended at the end, keeping the historic four first: a flag list is
	// canonicalised in THIS table order, so appending new flags after the
	// old ones keeps FlagsString(FlagsFrom(s)) stable for every name the
	// original table already knew.
	{"evoked", state.FlagEvoked},
	{"dashed", state.FlagDashed},
	{"overloaded", state.FlagOverloaded},
	{"warped", state.FlagWarped},
	{"buyback", state.FlagBuyback},
	{"harmonize", state.FlagHarmonize},
	{"suspend", state.FlagSuspend},
	{"escaped", state.FlagEscaped},
	{"mayplay", state.FlagMayPlay},
	// The and/or Kicker's index bits (Wastescape Battlemage's two
	// independently optional kickers). Appended at the end per the table's
	// own ordering rule.
	{"kicked 1", state.FlagKicked1},
	{"kicked 2", state.FlagKicked2},
	// The can't-be-countered provenance of a spell paid with AddsNoCounter$
	// mana (Cavern of Souls). Appended at the end per the table's own
	// ordering rule.
	{"ncount", state.FlagNoCounter},
	// The Adventure spell face's cast (CR 714.3a). Appended at the end per
	// the table's own ordering rule.
	{"adventure", state.FlagAdventure},
	// The Replicate keyword's payment provenance (CR 702.55a); the payment
	// COUNT rides the same CastInfo's Amount. Appended at the end per the
	// table's own ordering rule.
	{"replicated", state.FlagReplicated},
	// Converge's spend provenance (CR 107.4f-family); the distinct-colour
	// COUNT rides the same CastInfo's Amount. Appended at the end per the
	// table's own ordering rule.
	{"converged", state.FlagConverged},
	// The Bestow keyword's alternative-cost cast (CR 702.114a); the flag is
	// the provenance rules' resolution reader uses to substitute the
	// synthesized Aura attach spell. Appended at the end per the table's
	// own ordering rule.
	{"bestowed", state.FlagBestowed},
	// Multikicker's payment provenance (CR 702.43); the TIMES-KICKED COUNT
	// rides the same CastInfo's Amount. Appended at the end per the table's
	// own ordering rule.
	{"multikicked", state.FlagMultikicked},
	// Foretell's cast provenance (CR 702.126a): set by BOTH provenance
	// markers -- the {2} face-down hand exile (rules' payCast foretell
	// branch, which emits its own CastInfo) and the later foretell-cost
	// cast from exile (modeFlags). Appended at the end per the table's own
	// ordering rule.
	{"foretold", state.FlagForetold},
	// Conspire's tap provenance (CR 702.78a): the flag is what the keyword
	// expansion's copy trigger reads through Count$Conspired, so a declined
	// Conspire emits no flag and resolves like the plain cast. Appended at
	// the end per the table's own ordering rule.
	{"conspired", state.FlagConspired},
	// Convoke's creature provenance (CR 702.66, task connive1): the flag is
	// what Defined$ Convoked reads -- the creatures tapped to help pay for
	// the cast ride the pay-time CastInfo's IDs. Emitted only for a face
	// whose SVar table or abilities reference the selector (rules/cast.go's
	// faceWantsConvoked), so every unrelated convoke cast stays
	// byte-identical. Appended at the end per the table's own ordering rule.
	{"convoked", state.FlagConvoked},
	// The total-mana-spent capture (task castprov1): a face whose SVar
	// table reads the Count$CastTotalManaSpent head stamps its pay-time
	// CastInfo with the flag, so the Amount folds into Object.ManaSpent
	// instead of overwriting X. Appended at the end per the table's own
	// ordering rule.
	{"manaspent", state.FlagManaSpent},
	// The SNOW-unit part of the total-mana-spent capture (task castfilter1):
	// a face whose SVar table reads the filtered Count$CastTotalManaSpent Snow
	// head stamps its pay-time CastInfo with this flag too, so the Amount
	// folds into Object.ManaSnowSpent instead of overwriting X or the
	// unfiltered total. Appended at the end per the table's own ordering
	// rule.
	{"manasnowspent", state.FlagManaSnowSpent},
	// The TREASURE-/CAVE-/DESERT-sourced parts of the total-mana-spent
	// capture (task castfilter2): a face whose SVar table reads the filtered
	// Count$CastTotalManaSpent Treasure/Cave/Desert head stamps its pay-time
	// CastInfo with these flags too, so each Amount folds into its own
	// Object field instead of overwriting X, the total, or an earlier tag.
	// Appended at the end per the table's own ordering rule.
	{"manatreasurespent", state.FlagManaTreasureSpent},
	{"manacavespent", state.FlagManaCaveSpent},
	{"manadesertspent", state.FlagManaDesertSpent},
	// The ARTIFACT-sourced part of the total-mana-spent capture (task
	// mayplay-mfa): a face whose SVar table reads the filtered
	// Count$CastTotalManaSpent Artifact head, or whose static reads the
	// CastSa Spell.ManaFromArtifact predicate, stamps its pay-time CastInfo
	// with this flag too, so the Amount folds into Object.ManaArtifactSpent
	// instead of overwriting X or an earlier tag. Appended at the end per
	// the table's own ordering rule.
	{"manaartifactspent", state.FlagManaArtifactSpent},
	// The DB$ Play ReplaceGraveyard$ Exile rider (task replplay1): the Play
	// SA's own provenance stamps its pay-time CastInfo with this flag, so
	// spellRestZone/spellFizzleZone send the played card to exile instead
	// of the graveyard. Appended at the end per the table's own ordering
	// rule.
	{"replacegraveyard", state.FlagReplaceGraveyard},
	// The Aftermath half's cast (CR 702.85a): the flag is what the resolution
	// reader (spellRestZone) and the fizzle reader (spellFizzleZone) read to
	// exile the card instead of the graveyard. Appended at the end per the
	// table's own ordering rule.
	{"aftermath", state.FlagAftermath},
	// Mutate's cast provenance (CR 702.140a), the placement choice riding
	// FlagMutatedTop beside it. Appended at the end per the table's own
	// ordering rule.
	{"mutated", state.FlagMutated},
	{"mutated top", state.FlagMutatedTop},
	// The Squad keyword's payment provenance (CR 702.66); the payment COUNT
	// rides the same CastInfo's Amount. Appended at the end per the table's
	// own ordering rule.
	{"squadpaid", state.FlagSquadPaid},
	// The Offspring keyword's additional-cost provenance (CR 702.175a); a
	// bool, paid at most once. Appended at the end per the table's own
	// ordering rule.
	{"offspringpaid", state.FlagOffspringPaid},
	{"optionalcostpaid", state.FlagOptionalCostPaid},
	// The Fuse cast (CR 702.101b) of a non-Room Split card: one spell
	// resolving both halves. Appended at the end per the table's own
	// ordering rule.
	{"fused", state.FlagFused},
	// The ManaExpend wake-up marker (trig:ManaExpend): a cast made while a
	// ManaExpend trigger face is on the caster's battlefield stamps its
	// pay-time CastInfo with this flag, so its Amount names the cast's pool
	// spend to the crossing matcher. The engine's per-turn tally itself is
	// scratch (rules' manaExpended), NOT an event fold: it must count casts
	// made before the carrier entered, which emit no such event. Appended at
	// the end per the table's own ordering rule.
	{"manaexpend", state.FlagManaExpendCast},
	// The Jump-start cast (CR 702.84a): the card was cast from the graveyard
	// by discarding a card in addition to its other costs, so the resolution
	// and fizzle readers exile it instead of the graveyard. Appended at the
	// end per the table's own ordering rule.
	{"jumpstart", state.FlagJumpstart},
	// The K:MayFlashSac off-sorcery cast (kw:MayFlashSac): the flag is the
	// provenance the keyword's own ETB hook reads to register the delayed
	// cleanup-step sacrifice, so a sorcery-timed cast of the same card emits
	// no flag and no sacrifice. Appended at the end per the table's own
	// ordering rule.
	{"mayflashsac", state.FlagMayFlashSac},
	// Compleated's life-paid amount reduces a planeswalker's entry loyalty.
	{"compleated", state.FlagCompleated},
	// The K:Mayhem alternative-cost cast (kw:Mayhem): the flag is the
	// provenance the Card.CastSa Spell.Mayhem condition reads (Sandman's
	// Quicksand's "if this spell's mayhem cost was paid" split). Appended
	// at the end per the table's own ordering rule.
	{"mayhem", state.FlagMayhem},
	// The morph family's face-down cast (CR 702.37a/702.168a/702.169a): the
	// flag names the keyword family the {3} face-down cast rode, what the
	// resolution reader dispatches on (no printed spell abilities, no
	// targets) and what a later turn-face-up action prices from. Appended
	// at the end per the table's own ordering rule.
	{"morphed", state.FlagMorphed},
	{"megamorphed", state.FlagMegamorphed},
	{"disguised", state.FlagDisguised},
	// The CR 702.168 Gift promise: the caster named an opponent as the
	// gift's receiver as the spell was cast. Read by the PromisedGift
	// predicate, the Count$PromisedGift head and Defined$ Promised. Appended
	// at the end per the table's own ordering rule.
	{"promisedgift", state.FlagPromisedGift},
	// Rebound's hand-cast provenance (CR 702.95a): the spell was cast from
	// its controller's hand, so it is exiled as it resolves and its next
	// upkeep offers the free recast. Read by spellRestZone and
	// moveResolvedOffStack. Appended at the end per the table's own ordering
	// rule.
	{"rebound", state.FlagRebound},
	// The K:Blitz alternative-cost cast (CR 702.152a): the flag is the
	// provenance rules/altcast.go's entry hook (blitzEnter) reads for the
	// haste grant, the dies-draw granted trigger and the next-end-step
	// sacrifice. Appended at the end per the table's own ordering rule.
	{"blitzed", state.FlagBlitzed},
	// The AddsCounters$ mana-spend rider (Opal Palace, Biophagus, Animal
	// Attendant, Guildmages' Forum: 4 corpus files): a spell whose payment
	// consumed a rider-bearing mana batch stamps this flag, and the grants
	// ride the same CastInfo's Text into Object.ManaAddsCounterGrants.
	// Appended after main's blitzed entry to preserve its ordering.
	{"addscounters", state.FlagAddsCounters},
}

// FlagsFrom parses a comma-separated flag list (CastInfo.Counter's shape)
// into a CastFlags word. Unrecognized names are silently ignored, the same
// totality stance as everywhere else in this package: a stray or future
// flag name in an untrusted log must not make this panic.
func FlagsFrom(s string) uint64 {
	var f uint64
	for part := range strings.SplitSeq(s, ",") {
		for _, fn := range flagNames {
			if strings.TrimSpace(part) == fn.name {
				f |= fn.bit
			}
		}
	}
	return f
}

// FlagsString is FlagsFrom's inverse: a canonical, fixed-order csv of the
// flag names set in f.
func FlagsString(f uint64) string {
	var parts []string
	for _, fn := range flagNames {
		if f&fn.bit != 0 {
			parts = append(parts, fn.name)
		}
	}
	return strings.Join(parts, ",")
}

// StackCopyCounter is the rider payload the StackCopy event's Counter may
// carry (the field every other StackCopy leaves empty). The corpus's one
// carrier is the Casualty:X script-rider family (Ob Nixilis, the Adversary:
// the copy "isn't legendary and has starting loyalty X"), delivered through
// the __kwCasualty: trigger rebuild in Apply; the same grammar composes the
// NonLegendary$/SetLoyalty$ params of any CopySpellAbility whose copy must
// resolve with changed characteristics. Tokens are comma-joined, fixed order:
// "nonlegendary", then "loyalty=<n>". A 0 loyalty token is real (the copy
// enters with no loyalty counters and dies), so presence is a separate bit.
type StackCopyCounter struct {
	NonLegendary bool
	Loyalty      int32
	HasLoyalty   bool
}

// ParseStackCopyCounter reads the grammar StackCopyCounterString writes; an
// empty or unrecognised payload reads as no riders.
func ParseStackCopyCounter(counter string) StackCopyCounter {
	var cc StackCopyCounter
	for tok := range strings.SplitSeq(counter, ",") {
		switch {
		case tok == "nonlegendary":
			cc.NonLegendary = true
		case strings.HasPrefix(tok, "loyalty="):
			if n, err := strconv.Atoi(strings.TrimPrefix(tok, "loyalty=")); err == nil && n >= 0 {
				cc.Loyalty, cc.HasLoyalty = int32(n), true
			}
		}
	}
	return cc
}

// StackCopyCounterString is ParseStackCopyCounter's inverse: empty for no
// riders, "nonlegendary" before "loyalty=<n>" otherwise.
func StackCopyCounterString(cc StackCopyCounter) string {
	toks := make([]string, 0, 2)
	if cc.NonLegendary {
		toks = append(toks, "nonlegendary")
	}
	if cc.HasLoyalty {
		toks = append(toks, "loyalty="+strconv.Itoa(int(cc.Loyalty)))
	}
	return strings.Join(toks, ",")
}
