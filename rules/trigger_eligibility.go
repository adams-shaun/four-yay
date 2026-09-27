package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// triggerEventMask is only an over-approximation: an eligible trigger still
// runs every existing zone, phase, condition, batch and firing-limit gate.
// Event ordinals are unchanged. Future kinds beyond the mask go through the
// full matcher rather than being silently truncated by a shift.
type triggerEventMask uint64

type objectTriggerEventMasks struct {
	faces     [2]*cards.Face
	masks     [2]triggerEventMask
	interests [2]cards.TriggerInterest
	compiled  [2]bool
}

const allTriggerEvents triggerEventMask = ^triggerEventMask(0)

// triggerMaskKindBits is how many Kind ordinals triggerEventMask can encode,
// one bit each. A kind at or beyond this ordinal (or any ordinal the mask
// cannot represent) must fail OPEN to the full matcher, never be silently
// truncated by a shift: the mask is an over-approximation, so allowing an
// event the text may not need is safe, while rejecting one it does need would
// drop a real trigger. Both the textual mask (allows) and the compiled
// interest prefilter (compiledTriggerInterestAllows) use this ONE bound, so a
// kind appended past the mask's reach fails open in both paths together
// rather than one path rejecting what the other allows -- the divergence that
// CombatRetarget (ordinal 64, the first kind past the old 64-bit mask)
// exposed.
const triggerMaskKindBits = 64

func (m triggerEventMask) allows(kind events.Kind) bool {
	return kind >= triggerMaskKindBits || m&(1<<kind) != 0
}

// eventTriggerInterest maps replay-stable event kinds to cards-owned semantic
// trigger classes. Every current irrelevant kind is named explicitly so a
// future event reaches the conservative catch-all default until audited.
func eventTriggerInterest(kind events.Kind) cards.TriggerInterest {
	switch kind {
	case events.MoveZone:
		return cards.TriggerInterestZoneChange
	case events.Draw:
		return cards.TriggerInterestZoneChange | cards.TriggerInterestDraw
	case events.LifeChange:
		return cards.TriggerInterestLifeChange
	case events.Damage:
		return cards.TriggerInterestDamage
	case events.Tap:
		return cards.TriggerInterestTap
	case events.StepChange:
		return cards.TriggerInterestStepChange
	case events.PutOnStack:
		return cards.TriggerInterestZoneChange | cards.TriggerInterestStackPut
	case events.DeclareAttackers, events.DeclareBlockers:
		return cards.TriggerInterestAttackDeclaration
	case events.TargetsChosen:
		return cards.TriggerInterestTargetsChosen
	case events.AbilityPush, events.KeywordAbilityPush:
		return cards.TriggerInterestAbilityPush
	case events.GameStart, events.Shuffle, events.Untap, events.TurnChange,
		events.Priority, events.Resolve, events.ManaAdd, events.ManaClear,
		events.CounterChange, events.PlayerLost, events.GameOver,
		events.DecisionAsk, events.DecisionMade, events.Note, events.LandPlayed,
		events.FlipFace, events.ClockTick, events.TriggerPush,
		events.EndCombatReset, events.Choose,
		events.TokenCreate, events.StackCopy, events.ModeChosen,
		events.CmdDamage, events.DelayedRegister, events.DelayedPush, events.DelayedRemove,
		events.GrantAbilityPush,
		events.LibraryOrder, events.ExtraTurn, events.DoorUnlock,
		events.SpeedChange, events.ControlChange,
		events.CardToken, events.KeywordTriggerPush, events.Goad,
		events.PlayerCounterChange, events.Imprint, events.StartingPlayerChange,
		events.Pair, events.MyriadCopy, events.MyriadCleanup,
		events.GrantTriggerPush, events.ManaActivate,
		events.TokenAttacks, events.XChange, events.NoteNumber, events.ExtraPhase,
		events.CopyToken, events.Exert, events.PlanarRoll,
		events.CombatRetarget, events.RingTemptsYou, events.RingEmblemPush,
		events.BlessingChange, events.ClonePermanent, events.CloneStatic, events.TurnFaceDown,
		events.DamageProvenance, events.EnduringStoryChange,
		events.Mutate, events.MergedTriggerPush,
		events.Enlist, events.AlterAttribute, events.Unattached, events.PlayerNoted,
		events.PlayerNoteCleared, events.CardNoted,
		events.GainedAbilityPush, events.GainedTriggerPush,
		events.StoreSVar, events.GiftPromise, events.GiveGift, events.PhaseOut, events.RollDice,
		events.DelayedForget, events.Cascade, events.Clash,
		events.PlanarDeckShuffle, events.PlanarReveal, events.PlanarWalk,
		events.ChaosEnsues:
		// Cascade is a never-emitted PROPOSAL (the cascade instruction's
		// replacement boundary, events.Cascade): it is held out to the
		// replacement matcher and logged nowhere, so no trigger mode can ever
		// observe it and it carries no interest bits -- the zero mapping every
		// other bookkeeping kind in this list has.
		// Clash is matched by trig:Clashed through the full matcher
		// (clashMatches) via the fail-open path, exactly like GiveGift;
		// its ordinal sits past triggerMaskKindBits. Naming it here keeps
		// the audit complete and out of the catch-all default.
		// AlterAttribute (alterattr1) is the same shape past the bound as
		// Enlist: the suspected designation (CR 702.157) is a status no
		// trigger mode fires on -- the corpus reads it through filter
		// predicates (Creature.IsSuspected on ValidAttackers$, AllValid$),
		// never through an event -- and its ordinal sits past
		// triggerMaskKindBits, so both classifiers fail open before this map
		// is consulted. Naming it keeps the audit complete if the bound ever
		// widens.
		//
		// DamageProvenance is the game-long bookkeeping fact Engine.emit emits
		// beside a landed Damage event (the_fallen, diseased_vermin): it is a
		// record of what already happened, matched only by the damage
		// predicates' state read, never by a trigger mode -- the same reading
		// CmdDamage, Imprint and Goad get. Without it here the default arm gave
		// the kind TriggerInterestAny, so every point of damage ran a second
		// full trigger scan.
		//
		// ClonePermanent is a characteristic change (the api:Clone layer-1
		// CopyFace basis), not a game event any trigger mode fires on -- the
		// same reading FlipFace and CardToken get. Without it here the
		// default arm gave the kind TriggerInterestAny, so every clone and
		// every clone expiry ran a full trigger scan.
		//
		// Mutate and MergedTriggerPush are named for the same documentary
		// reason even though both currently sit PAST triggerMaskKindBits, so
		// both classifiers fail open before this map is consulted: Mutate is
		// matched by trig:Mutates through the full matcher (mutatesMatches),
		// and MergedTriggerPush is a mint marker no mode fires on. Enlist is
		// the same shape past the bound: trig:Enlisted matches the full
		// events.Enlist carrier through enlistedMatches. GainedAbilityPush and
		// GainedTriggerPush (gains1) are the has-all-abilities-of mint
		// markers: the ability itself is matched on the event that caused it
		// (an ordinary trigger scan), and the push only mints its stack
		// object -- the GrantAbilityPush/GrantTriggerPush shape, and like
		// those two past the bound so both classifiers fail open anyway.
		// Naming them keeps the audit complete if the bound ever widens.
		//
		// PlayerNoted and PlayerNoteCleared are the two halves of the same
		// player-notation bookkeeping (NoteCardsFor$ writes a label,
		// ClearNotedCardsFor$ removes one): the label is read back by the
		// `Player.NotedFor<X>` filter predicate at a later resolution, never
		// by a trigger mode -- no T: line in the corpus fires on a note being
		// written or cleared. Both ordinals (83, 84) sit past
		// triggerMaskKindBits, so both classifiers fail open before this map
		// is consulted; naming the clear half alongside the write half keeps
		// the audit complete if the bound ever widens, and keeps it out of
		// the catch-all default that would otherwise run a full trigger scan
		// on every cleared label.
		//
		// CardNoted is the card-notation write (NoteCards$ Remembered /
		// TriggeredSource | NoteCardsFor$ -- Volatile Chimera, Arcane Savant,
		// Caller of the Untamed, Maelstrom Archangel Avatar), the object-side
		// sibling of the PlayerNoted pair: the label is read back by the
		// `Card.NotedFor<X>` filter predicate at a later resolution, never by
		// a trigger mode. Its ordinal sits past triggerMaskKindBits, so both
		// classifiers fail open before this map is consulted; naming it keeps
		// it out of the catch-all default that would otherwise run a full
		// trigger scan on every noted card.
		//
		// EnduringStoryChange (storied1) is CR 702.175's one-way "enduring
		// story" designation latch -- the exact BlessingChange shape: a
		// seat-status fact the corpus reads through the state predicate
		// (rules/layers.go's `EnduringStory` SVar read; Dáin's Condition$
		// EnduringStory; Balin Loremaster's "if you have an enduring story"
		// trigger rider), never through a trigger mode -- no T: line in the
		// corpus fires on the designation being GAINED. Its ordinal (94) sits
		// past triggerMaskKindBits, so both classifiers fail open before this
		// map is consulted; naming it keeps the audit complete if the bound
		// ever widens and keeps it out of the catch-all default that would
		// otherwise claim the kind trigger-relevant.
		//
		// PhaseOut (phases1) records CR 702.25's phased-out/phase-in status
		// on a battlefield permanent. No trigger mode fires on it: the
		// corpus's `Mode$ Phase` is the beginning-of-combat PHASE step
		// (events.StepChange), never this status fold, and a phased-out
		// permanent is read through state predicates, not a trigger. Its
		// ordinal sits past triggerMaskKindBits, so both classifiers fail
		// open before this map is consulted; naming it keeps the audit
		// complete if the bound ever widens.
		//
		// PlanarDeckShuffle, PlanarReveal and PlanarWalk are the CR 901
		// planar-deck lifecycle markers this foundation ticket adds. The
		// deck shuffle is private bookkeeping and the reveal is the state
		// move that turns the top plane face up; neither is matched by a
		// Mode$ line. PlanarWalk IS matched now -- trig:PlaneswalkedTo and
		// trig:PlaneswalkedFrom (rules/planar.go's planeswalkedToMatches /
		// planeswalkedFromMatches) -- but through the per-event synthetic
		// plane scan checkPlaneswalkTriggers, not the per-face prefilter
		// this function feeds: a plane lives in the private ZPlanarDeck
		// zone, which the per-face walk never visits, so this map's zero
		// answer for PlanarWalk never gates a walk trigger. All three
		// ordinals sit past triggerMaskKindBits, so both classifiers fail
		// open before this map is consulted; naming them keeps the audit
		// complete if the bound ever widens and keeps them out of the
		// catch-all default that would otherwise claim the kinds
		// trigger-relevant.
		//
		// DelayedForget (efftrig1) drops one remembered card from an Effect
		// trigger registration (a ForgetOnMoved$/ForgetCounter$ trim). No
		// trigger mode fires on it: it is the registration-local trim the
		// continuous side also performs, and the trim is read back through the
		// registration's Remembered list at the promise's own fire time, never
		// through a T: line. It is the DelayedRemove shape (already zero-mapped
		// above): bookkeeping about the delayed registry, not a game event a
		// matcher consults.
		return 0
	case events.Attach:
		return cards.TriggerInterestAttach
	case events.Explore:
		return cards.TriggerInterestExplore
	case events.CastInfo:
		// manaexpend1: the pay-time CastInfo carries trig:ManaExpend's
		// crossing read (rules/cast.go's FlagManaExpendCast emission), so it
		// has its own interest bit rather than the fail-open default the
		// default arm would give it -- a ManaExpend-only face's compiled
		// scan set narrows to the one event kind it fires on.
		return cards.TriggerInterestCastInfo
	case events.MonarchChange:
		// The monarch designation transition carries trig:BecomeMonarch
		// (rules' becomeMonarchMatches), so it has its own interest bit rather
		// than the zero mapping it carried while no mode matched it.
		return cards.TriggerInterestMonarch
	default:
		return cards.TriggerInterestAny
	}
}

// compiledTriggerInterestEvent is compiledTriggerInterestAllows' per-event
// half, for walks that test many faces against one event: for every
// interests value, compiledTriggerInterestAllows(interests, kind) ==
// all || interests&mask != 0.
func compiledTriggerInterestEvent(kind events.Kind) (all bool, mask cards.TriggerInterest) {
	if kind >= triggerMaskKindBits {
		return true, 0
	}
	eventInterest := eventTriggerInterest(kind)
	if eventInterest == cards.TriggerInterestAny {
		return true, 0
	}
	return false, cards.TriggerInterestAny | eventInterest
}

func compiledTriggerInterestAllows(interests cards.TriggerInterest, kind events.Kind) bool {
	// Kinds the 64-bit textual mask cannot encode fail open here too, or the
	// compiled prefilter would reject an event the textual mask admits.
	if kind >= triggerMaskKindBits {
		return true
	}
	eventInterest := eventTriggerInterest(kind)
	return interests&cards.TriggerInterestAny != 0 || eventInterest == cards.TriggerInterestAny || interests&eventInterest != 0
}

// Keep this aligned with triggerMatches' actual dispatch, not with a wider
// interpretation of Forge mode names. Unknown modes retain the old path so
// adding a matcher cannot silently lose triggers before this table catches up.
func triggerModeEvents(mode string) triggerEventMask {
	switch mode {
	case "ChangesZone", "ChangesZoneAll":
		return 1<<events.MoveZone | 1<<events.Draw | 1<<events.PutOnStack
	case "SpellCast":
		return 1 << events.PutOnStack
	case "SpellCastOrCopy":
		return 1<<events.PutOnStack | 1<<events.StackCopy
	case "SpellCopy":
		return 1 << events.StackCopy
	case "AbilityCast":
		// KeywordAbilityPush lies past this mask's 64-bit bound and fails
		// open to the matcher, which reads its replayable Counter body.
		return 1 << events.AbilityPush
	case "SpellAbilityCast":
		// The spell-or-activate union (targetsvalid1): the activation arm
		// matches an AbilityPush or KeywordAbilityPush, the spell arm a
		// PutOnStack. AbilityCast stays narrow above -- its oracle text is
		// activation-only.
		return 1<<events.AbilityPush | 1<<events.PutOnStack
	case "Attacks", "AttackersDeclaredOneTarget", "AttackersDeclared":
		return 1 << events.DeclareAttackers
	case "AttackerBlocked", "AttackerBlockedByCreature", "AttackerUnblocked", "AttackerUnblockedOnce", "Blocks":
		return 1 << events.DeclareBlockers
	case "Untaps":
		return 1 << events.Untap
	case "Specializes":
		// The event Kind is beyond this mask's bit width; allow the matcher
		// to inspect the full event and keep this mode's candidate set narrow.
		return 0
	case "PhaseOutAll":
		// CR 702.25b: the batch-level "whenever one or more permanents phase
		// out" trigger matches the events.PhaseOut marker the api:Phases
		// primitive emits (Amount >= 1 is a phase-out; the phase-in half is
		// the opposite event). The Kind's ordinal is past the 64-bit mask's
		// reach, the Surveil/Discover shape: a mask bit is not encodable and
		// allows() fails open for every kind at or past
		// triggerMaskKindBits, so the mode is admitted through that fail-open
		// path and gated by the full matcher (phaseOutAllMatches). Naming the
		// mode here rather than letting it fall to the allTriggerEvents
		// default keeps a PhaseOutAll-only face's mask narrow for every other
		// kind.
		return 0
	case "Sacrificed", "Discarded", "DiscardedAll", "LandPlayed", "Milled", "MilledAll":
		return 1 << events.MoveZone
	case "Cycled":
		return 1 << events.MoveZone
	case "Explores":
		return 1 << events.Explore
	case "Connives":
		// The marker Kind's ordinal is past the 64-bit mask's reach, the
		// Investigated/Discover shape: a mask bit is not encodable and
		// allows() fails open for every kind at or past triggerMaskKindBits,
		// so the mode is admitted through that fail-open path. Naming the
		// mode here (rather than letting it fall to the allTriggerEvents
		// default) keeps a Connives-only face's mask narrow for every other
		// kind.
		return 0
	case "SearchedLibrary":
		// This marker is appended beyond the 64-bit trigger-mask range, so
		// naming it keeps a SearchedLibrary-only face narrow on older Kinds.
		return 0
	case "GiveGift":
		// The GiveGift marker's ordinal is past the 64-bit mask's reach, the
		// Investigated/Surveil shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path. Naming the mode here
		// keeps a GiveGift-only face's mask narrow for every other kind.
		return 0
	case "Investigated":
		// The Kind's ordinal (67) is past the 64-bit mask's reach, the
		// RingTemptsYou shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path. Naming the mode here
		// (rather than letting it fall to the allTriggerEvents default)
		// keeps an Investigated-only face's mask narrow for every other
		// kind.
		return 0
	case "Discover", "SeekAll":
		// The marker Kinds' ordinals (Discover 73, Seek 74) are past the
		// 64-bit mask's reach, the RingTemptsYou/Investigated shape: a mask
		// bit is not encodable and allows() fails open for every kind at or
		// past triggerMaskKindBits, so the modes are admitted through that
		// fail-open path. Naming the modes here (rather than letting them
		// fall to the allTriggerEvents default) keeps a Discover/SeekAll-only
		// face's mask narrow for every other kind.
		return 0
	case "Foretell":
		// trig:Foretell (task agent-20260923T032009Z-3b9d3432): "Whenever you
		// foretell a card, ..." (CR 702.126b; Dream Devourer, the corpus's
		// sole carrier at the pin -- measured 1 file). It matches the {2}
		// Foretell special action's pay-time FlagForetold CastInfo
		// (rules/cast.go's foretell branch, card still in hand; ordinal 29,
		// inside the 64-bit mask's reach) and the effect-designation exile
		// MoveZone markers (applyFaceDownMarker's Foretold$ True
		// composition); both shapes existed before the mode did. The exact
		// event shapes are the full matcher's (foretellMatches,
		// rules/trigmatch_foretell.go) -- the MoveZone bit is needed for the
		// designation arm and is over-approximate for every other zone
		// change, which the mask is for by design. Naming the mode here
		// rather than letting it fall to the allTriggerEvents default keeps
		// a Foretell-only face's mask narrow for every other kind.
		return 1<<events.CastInfo | 1<<events.MoveZone
	case "Surveil":
		// The Surveil marker's ordinal (79, task trig-surveil) is past the
		// 64-bit mask's reach, the Discover/SeekAll shape: a mask bit is not
		// encodable and allows() fails open for every kind at or past
		// triggerMaskKindBits, so the mode is admitted through that fail-open
		// path and gated by the full matcher (surveilMatches). Naming the
		// mode here rather than letting it fall to the allTriggerEvents
		// default keeps a Surveil-only face's mask narrow for every other
		// kind.
		return 0
	case "Proliferate":
		// The Proliferate marker's ordinal is past the 64-bit mask's reach
		// (task trig-proliferate, appended after GiveGift), the
		// Surveil/Discover shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path and gated by the full
		// matcher (proliferateMatches). Naming the mode here rather than
		// letting it fall to the allTriggerEvents default keeps a
		// Proliferate-only face's mask narrow for every other kind.
		return 0
	case "Scry":
		// The Scry marker's ordinal is past the 64-bit mask's reach, the
		// Surveil/Discover shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path and gated by the full
		// matcher (scryMatches). Naming the mode here rather than letting it
		// fall to the allTriggerEvents default keeps a Scry-only face's mask
		// narrow for every other kind.
		return 0
	case "Exploited":
		// The Exploit marker's ordinal is past the 64-bit mask's reach, the
		// Investigated/Discover shape: a mask bit is not encodable and
		// allows() fails open for every kind at or past triggerMaskKindBits,
		// so the mode is admitted through that fail-open path and gated by
		// the full matcher (exploitedMatches). Naming the mode here rather
		// than letting it fall to the allTriggerEvents default keeps an
		// Exploited-only face's mask narrow for every other kind.
		return 0
	case "Clashed":
		// The Clash marker's ordinal is past the 64-bit mask's reach, the
		// Exploited/GiveGift shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path and gated by the full
		// matcher (clashMatches). Naming the mode here rather than letting it
		// fall to the allTriggerEvents default keeps a Clashed-only face's
		// mask narrow for every other kind.
		return 0
	case "ChaosEnsues":
		// The ChaosEnsues marker's ordinal is past the 64-bit mask's reach,
		// the Clashed/GiveGift shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path and gated by the
		// synthetic plane scan (checkChaosEnsuesTriggers) plus the full
		// matcher (chaosEnsuesMatches). Naming the mode here rather than
		// letting it fall to the allTriggerEvents default keeps a
		// ChaosEnsues-only face's mask narrow for every other kind.
		return 0
	case "BecomeMonstrous":
		// The AlterAttribute carrier's ordinal is past the 64-bit mask's
		// reach, the Exploited/Investigated shape: a mask bit is not encodable
		// and allows() fails open for every kind at or past
		// triggerMaskKindBits, so the mode is admitted through that fail-open
		// path and gated by the full matcher (becomeMonstrousMatches, task
		// agent-20260919T190014Z). Naming the mode here rather than letting it
		// fall to the allTriggerEvents default keeps a BecomeMonstrous-only
		// face's mask narrow for every other kind.
		return 0
	case "Evolved":
		// The Evolved marker's ordinal is past the 64-bit mask's reach, the
		// GiveGift/Surveil shape: a mask bit is not encodable and allows()
		// fails open for every kind at or past triggerMaskKindBits, so the
		// mode is admitted through that fail-open path and gated by the full
		// matcher (evolvedMatches, task trig:Evolved). Naming the mode here
		// rather than letting it fall to the allTriggerEvents default keeps
		// an Evolved-only face's mask narrow for every other kind.
		return 0
	case "FullyUnlock":
		// CR 709.5's "whenever you fully unlock a Room" (task
		// agent-20260919T191104Z-95f1e316): the Eerie enchantments' other-
		// permanent half, matched by fullyUnlockMatches (rules/
		// trigmatch_room.go). It fires on the single DoorUnlock transition
		// event the unlock activation emits, whose ordinal (41) is inside the
		// 64-bit mask's reach, so an exact bit is encodable -- naming the mode
		// rather than letting it fall to the allTriggerEvents default keeps a
		// FullyUnlock-only face's mask narrow for every other kind.
		return 1 << events.DoorUnlock
	case "RingTemptsYou":
		// The Kind's ordinal (65) is past the 64-bit mask's reach: a mask bit
		// is not encodable, and allows() fails open for every kind at or past
		// triggerMaskKindBits (the CombatRetarget lesson), so the mode is
		// admitted through that fail-open path. Naming the mode here (rather
		// than letting it fall to the allTriggerEvents default) keeps a
		// RingTemptsYou-only face's mask narrow for every other kind.
		return 0
	case "BecomeMonarch":
		// The monarch designation transition (trig:BecomeMonarch), matched by
		// rules' becomeMonarchMatches. MonarchChange is ordinal 43, inside the
		// 64-bit mask's reach, so an exact bit is encodable.
		return 1 << events.MonarchChange
	case "CommitCrime", "BecomesTarget", "BecomesTargetOnce":
		return 1 << events.TargetsChosen
	case "Attached":
		return 1 << events.Attach
	case "Unattached":
		// CR 701.3b's detach half. The Kind's ordinal is past the 64-bit
		// mask's reach (Unattached is appended after Surveil, the same
		// post-CombatRetarget range as Enlisted/Mutates), so a mask bit is not
		// encodable and allows() fails open for every kind at or past
		// triggerMaskKindBits -- the mode is admitted through that fail-open
		// path and gated by the full matcher (unattachedMatches). Naming the
		// mode here rather than letting it fall to the allTriggerEvents default
		// keeps an Unattached-only face's mask narrow for every other kind, and
		// keeps Mode$ Attached's mask exact (its bit is events.Attach, never
		// events.Unattached).
		return 0
	case "Exerted":
		// The mode fires on the CR 702.100 exert itself (events.Exert with
		// Amount >= 0); the Amount == -1 untap-step consume marker is the
		// same Kind but rejected by exertedMatches, so the mask stays exact.
		return 1 << events.Exert
	case "Enlisted":
		// enlist1: the mode fires on the CR 702.160 enlist action itself
		// (events.Enlist, the Exerted shape). The Kind's ordinal (75) is past
		// the 64-bit mask's reach, the RingTemptsYou/Investigated shape: a
		// mask bit is not encodable and allows() fails open for every kind at
		// or past triggerMaskKindBits, so the mode is admitted through that
		// fail-open path and gated by the full matcher (enlistedMatches).
		// Naming the mode here rather than letting it fall to the
		// allTriggerEvents default keeps an Enlisted-only face's mask narrow
		// for every other kind.
		return 0
	case "Taps", "TapsForMana":
		return 1 << events.Tap
	case "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll":
		return 1 << events.Damage
	case "DamagePreventedOnce":
		// The mode fires on the STORED prevention Note (rules/replacement.go's
		// full-prevention arm and its ReplaceDamage/protection siblings), not
		// on the Damage event the prevention replaces -- a prevented hit is a
		// Note, never a Damage.
		return 1 << events.Note
	case "FlippedCoin":
		// The mode fires on the canonical coin-flip result Note both
		// api:FlipCoin (effects/flipcoin.go) and the cumulative-upkeep FlipCoin
		// cost action (rules/cumulative.go) emit -- one shared encoding, so a
		// cost-side flip fires the trigger exactly like an effect-side one
		// (Karplusan Minotaur).
		return 1 << events.Note
	case "Vote":
		// The mode fires on the canonical vote-finished Note (effects/
		// vote.go) both api:Vote shapes emit once a vote fully finishes --
		// the exact carrier-event shape FlippedCoin shares, with the two
		// List$ opponent sets riding IDs/Pairs as player refs.
		return 1 << events.Note
	case "RolledDie", "RolledDieOnce":
		// Both modes fire on a canonical roll Note effects/dice.go emits
		// (decoded by DieRollResult / DieRollBatchResult), the same
		// carrier-event shape FlippedCoin/Vote share: RolledDie on the per-die
		// Note (once per die), RolledDieOnce on the per-resolution batch Note
		// (once per roll action).
		return 1 << events.Note
	case "CounterAdded", "CounterAddedOnce", "CounterRemoved", "CounterRemovedOnce":
		return 1 << events.CounterChange
	case "CounterPlayerAddedAll":
		// The batch "whenever you put one or more counters on ..." mode
		// (Generous Patron, Rikku Resourceful Guardian): fires on the object
		// AND player placement events the matcher
		// (counterPlayerAddedAllMatches) reads.
		return 1<<events.CounterChange | 1<<events.PlayerCounterChange
	case "ClassLevelGained":
		// CR 702.118c: the same CounterChange event the level-up
		// activator's PutCounter emits carries the level band crossing
		// (matcher: classLevelGainedMatches).
		return 1 << events.CounterChange
	case "Mutates":
		// CR 702.140f: "whenever this creature mutates". The event is the
		// mutate-spell merge fold (events.Mutate), fired once per mutation --
		// whose ordinal (71) is past the 64-bit mask's reach, the
		// RingTemptsYou/Investigated shape: a mask bit is not encodable and
		// allows() fails open for every kind at or past triggerMaskKindBits
		// (the CombatRetarget lesson), so the mode is admitted through that
		// fail-open path and gated by the full matcher (mutatesMatches).
		// Naming the mode here rather than letting it fall to the
		// allTriggerEvents default keeps a Mutates-only face's mask narrow
		// for every other kind.
		return 0
	case "TurnFaceUp":
		// CR 708.6/702.36e: the turn-up marker events.TurnFaceUp (task
		// agent-20260919T183249Z-0fb8ed97). Its ordinal is past the 64-bit
		// mask's reach -- the Mutates/Investigated shape -- so a mask bit is
		// not encodable and allows() fails open for it, gated by the full
		// matcher (turnFaceUpMatches). Returning 0 here rather than the
		// allTriggerEvents default keeps a TurnFaceUp-only face's mask narrow
		// for every other kind.
		return 0
	case "TokenCreated", "TokenCreatedOnce":
		return 1 << events.TokenCreate
	case "Drawn":
		return 1 << events.Draw
	case "LifeLost":
		return 1<<events.Damage | 1<<events.LifeChange
	case "LifeGained":
		return 1 << events.LifeChange
	case "Phase":
		return 1 << events.StepChange
	default:
		// Always reads state on every event. LifeLostAll also has a batch-
		// finishing entry point; leave its existing gates authoritative.
		return allTriggerEvents
	}
}

func grantedKeywordTriggerEvent(kind events.Kind) bool {
	return kind == events.TargetsChosen || kind == events.DeclareAttackers || kind == events.DeclareBlockers ||
		kind == events.PutOnStack || kind == events.MoveZone || kind == events.StepChange
}

func triggerMaskForFace(f *cards.Face) triggerEventMask {
	if f == nil || len(f.Triggers) == 0 {
		return 0
	}
	var m triggerEventMask
	for _, t := range f.Triggers {
		// Phase diagnostics are event-visible and run on unrelated events
		// and in hidden zones too. Keep ALL Phase-bearing faces on the
		// original path, without caching whether a diagnostic was emitted.
		if strings.TrimSpace(t.Params["Phase"]) != "" {
			return allTriggerEvents
		}
		m |= triggerModeEvents(t.Mode)
	}
	return m
}

// faceMayTrigger caches immutable printed eligibility, never object/zone
// membership or a dynamic match. New fixture objects, token faces, transforms
// and Room unlocks therefore need no invalidation. The LIVE queue owner owns
// the map even when matching against a scratch look-back observer; no snapshot
// cache is shared or mutated. Clones start with an independent, empty map.
func (e *Engine) faceMayTrigger(f *cards.Face, kind events.Kind) bool {
	if f == nil || len(f.Triggers) == 0 {
		return false
	}
	if interests, ok := f.CompiledTriggerInterests(); ok {
		return compiledTriggerInterestAllows(interests, kind)
	}
	m, ok := e.triggerEventMasks[f]
	if !ok {
		m = triggerMaskForFace(f)
		if e.triggerEventMasks == nil {
			e.triggerEventMasks = make(map[*cards.Face]triggerEventMask)
		}
		e.triggerEventMasks[f] = m
	}
	return m.allows(kind)
}

// objectFaceMayTriggerHoisted is objectFaceMayTrigger with the event's
// compiled-interest half precomputed by compiledTriggerInterestEvent: a
// corpus-bound face answers from its catalog row without re-deriving the
// event's interest class per object; every other face takes
// objectFaceMayTrigger unchanged.
func (e *Engine) objectFaceMayTriggerHoisted(id state.ObjID, faceIdx uint8, f *cards.Face, kind events.Kind, evAll bool, evMask cards.TriggerInterest) bool {
	if interests, ok := f.CompiledTriggerInterests(); ok {
		return evAll || interests&evMask != 0
	}
	return e.objectFaceMayTrigger(id, faceIdx, f, kind)
}

// objectFaceMayTrigger is the object-walk fast path. Object IDs are dense, and
// the two face slots stay stable for the immutable lifetime of a Card, so the
// repeated event scan can avoid hashing a face pointer. The pointer check keeps
// synthetic face replacement and transforms safe; uncommon faces beyond the
// two-face card model use the conservative face cache above.
func (e *Engine) objectFaceMayTrigger(id state.ObjID, faceIdx uint8, f *cards.Face, kind events.Kind) bool {
	if f == nil {
		return false
	}
	// Corpus-bound faces already own an immutable catalog row. Avoid growing
	// per-engine object state just to cache the same interest bits again;
	// synthetic fixtures and dynamically replaced faces retain the fallback
	// below, including its pointer-identity guard.
	if interests, ok := f.CompiledTriggerInterests(); ok {
		return compiledTriggerInterestAllows(interests, kind)
	}
	if id == 0 || faceIdx >= 2 {
		return e.faceMayTrigger(f, kind)
	}
	i := int(id) - 1
	if i >= len(e.triggerObjectMasks) {
		e.triggerObjectMasks = append(e.triggerObjectMasks, make([]objectTriggerEventMasks, i+1-len(e.triggerObjectMasks))...)
	}
	entry := &e.triggerObjectMasks[i]
	if entry.faces[faceIdx] != f {
		entry.faces[faceIdx] = f
		entry.interests[faceIdx], entry.compiled[faceIdx] = f.CompiledTriggerInterests()
		if entry.compiled[faceIdx] {
			entry.masks[faceIdx] = 0
		} else {
			entry.masks[faceIdx] = triggerMaskForFace(f)
		}
	}
	if entry.compiled[faceIdx] {
		return compiledTriggerInterestAllows(entry.interests[faceIdx], kind)
	}
	return entry.masks[faceIdx].allows(kind)
}
