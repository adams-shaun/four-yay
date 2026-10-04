package cards

// The line-head vocabularies the engine dispatches on: a trigger's Mode$, a
// static's Mode$ and a replacement's Event$. deriveParamSets resolves each
// printed node's name once at load (ModeKind / EventKind read the stored
// code); a node built later resolves on read. A name the engine never
// dispatches on is code 0 and matches no case.

// TriggerMode is a T: line's Mode$ as a dense code.
type TriggerMode uint16

// The vocabulary, in name order. 0 is a name outside it.
const (
	TriggerAbilityCast TriggerMode = iota + 1
	TriggerAlways
	TriggerAttached
	TriggerAttackerBlocked
	TriggerAttackerBlockedByCreature
	TriggerAttackerUnblocked
	TriggerAttackerUnblockedOnce
	TriggerAttackersDeclared
	TriggerAttackersDeclaredOneTarget
	TriggerAttacks
	TriggerBecomeMonarch
	TriggerBecomeMonstrous
	TriggerBecomesTarget
	TriggerBecomesTargetOnce
	TriggerBlocks
	TriggerChangesController
	TriggerChangesZone
	TriggerChangesZoneAll
	TriggerChaosEnsues
	TriggerClashed
	TriggerClassLevelGained
	TriggerCommitCrime
	TriggerConnives
	TriggerCounterAdded
	TriggerCounterAddedOnce
	TriggerCounterPlayerAddedAll
	TriggerCounterRemoved
	TriggerCounterRemovedOnce
	TriggerCycled
	TriggerDamageAll
	TriggerDamageDealtOnce
	TriggerDamageDone
	TriggerDamageDoneOnce
	TriggerDamagePreventedOnce
	TriggerDiscarded
	TriggerDiscardedAll
	TriggerDiscover
	TriggerDrawn
	TriggerElementalBend
	TriggerEnlisted
	TriggerEvolved
	TriggerExerted
	TriggerExploited
	TriggerExplores
	TriggerFlippedCoin
	TriggerForetell
	TriggerFullyUnlock
	TriggerGiveGift
	TriggerInvestigated
	TriggerLandPlayed
	TriggerLifeGained
	TriggerLifeLost
	TriggerLifeLostAll
	TriggerManaExpend
	TriggerMilled
	TriggerMilledAll
	TriggerMutates
	TriggerPhase
	TriggerPhaseOutAll
	TriggerPlaneswalkedFrom
	TriggerPlaneswalkedTo
	TriggerProliferate
	TriggerRingTemptsYou
	TriggerRolledDie
	TriggerRolledDieOnce
	TriggerSacrificed
	TriggerScry
	TriggerSearchedLibrary
	TriggerSeekAll
	TriggerSpecializes
	TriggerSpellAbilityCast
	TriggerSpellCast
	TriggerSpellCastOrCopy
	TriggerSpellCopy
	TriggerSurveil
	TriggerTaps
	TriggerTapsForMana
	TriggerTokenCreated
	TriggerTokenCreatedOnce
	TriggerTransformed
	TriggerTurnFaceUp
	TriggerUnattached
	TriggerUntaps
	TriggerVote
	TriggerModeCount = iota + 1 // one past the last; sizes a dense per-TriggerMode array
)

var triggerModeNames = [TriggerModeCount]string{
	TriggerAbilityCast:                "AbilityCast",
	TriggerAlways:                     "Always",
	TriggerAttached:                   "Attached",
	TriggerAttackerBlocked:            "AttackerBlocked",
	TriggerAttackerBlockedByCreature:  "AttackerBlockedByCreature",
	TriggerAttackerUnblocked:          "AttackerUnblocked",
	TriggerAttackerUnblockedOnce:      "AttackerUnblockedOnce",
	TriggerAttackersDeclared:          "AttackersDeclared",
	TriggerAttackersDeclaredOneTarget: "AttackersDeclaredOneTarget",
	TriggerAttacks:                    "Attacks",
	TriggerBecomeMonarch:              "BecomeMonarch",
	TriggerBecomeMonstrous:            "BecomeMonstrous",
	TriggerBecomesTarget:              "BecomesTarget",
	TriggerBecomesTargetOnce:          "BecomesTargetOnce",
	TriggerBlocks:                     "Blocks",
	TriggerChangesController:          "ChangesController",
	TriggerChangesZone:                "ChangesZone",
	TriggerChangesZoneAll:             "ChangesZoneAll",
	TriggerChaosEnsues:                "ChaosEnsues",
	TriggerClashed:                    "Clashed",
	TriggerClassLevelGained:           "ClassLevelGained",
	TriggerCommitCrime:                "CommitCrime",
	TriggerConnives:                   "Connives",
	TriggerCounterAdded:               "CounterAdded",
	TriggerCounterAddedOnce:           "CounterAddedOnce",
	TriggerCounterPlayerAddedAll:      "CounterPlayerAddedAll",
	TriggerCounterRemoved:             "CounterRemoved",
	TriggerCounterRemovedOnce:         "CounterRemovedOnce",
	TriggerCycled:                     "Cycled",
	TriggerDamageAll:                  "DamageAll",
	TriggerDamageDealtOnce:            "DamageDealtOnce",
	TriggerDamageDone:                 "DamageDone",
	TriggerDamageDoneOnce:             "DamageDoneOnce",
	TriggerDamagePreventedOnce:        "DamagePreventedOnce",
	TriggerDiscarded:                  "Discarded",
	TriggerDiscardedAll:               "DiscardedAll",
	TriggerDiscover:                   "Discover",
	TriggerDrawn:                      "Drawn",
	TriggerElementalBend:              "ElementalBend",
	TriggerEnlisted:                   "Enlisted",
	TriggerEvolved:                    "Evolved",
	TriggerExerted:                    "Exerted",
	TriggerExploited:                  "Exploited",
	TriggerExplores:                   "Explores",
	TriggerFlippedCoin:                "FlippedCoin",
	TriggerForetell:                   "Foretell",
	TriggerFullyUnlock:                "FullyUnlock",
	TriggerGiveGift:                   "GiveGift",
	TriggerInvestigated:               "Investigated",
	TriggerLandPlayed:                 "LandPlayed",
	TriggerLifeGained:                 "LifeGained",
	TriggerLifeLost:                   "LifeLost",
	TriggerLifeLostAll:                "LifeLostAll",
	TriggerManaExpend:                 "ManaExpend",
	TriggerMilled:                     "Milled",
	TriggerMilledAll:                  "MilledAll",
	TriggerMutates:                    "Mutates",
	TriggerPhase:                      "Phase",
	TriggerPhaseOutAll:                "PhaseOutAll",
	TriggerPlaneswalkedFrom:           "PlaneswalkedFrom",
	TriggerPlaneswalkedTo:             "PlaneswalkedTo",
	TriggerProliferate:                "Proliferate",
	TriggerRingTemptsYou:              "RingTemptsYou",
	TriggerRolledDie:                  "RolledDie",
	TriggerRolledDieOnce:              "RolledDieOnce",
	TriggerSacrificed:                 "Sacrificed",
	TriggerScry:                       "Scry",
	TriggerSearchedLibrary:            "SearchedLibrary",
	TriggerSeekAll:                    "SeekAll",
	TriggerSpecializes:                "Specializes",
	TriggerSpellAbilityCast:           "SpellAbilityCast",
	TriggerSpellCast:                  "SpellCast",
	TriggerSpellCastOrCopy:            "SpellCastOrCopy",
	TriggerSpellCopy:                  "SpellCopy",
	TriggerSurveil:                    "Surveil",
	TriggerTaps:                       "Taps",
	TriggerTapsForMana:                "TapsForMana",
	TriggerTokenCreated:               "TokenCreated",
	TriggerTokenCreatedOnce:           "TokenCreatedOnce",
	TriggerTransformed:                "Transformed",
	TriggerTurnFaceUp:                 "TurnFaceUp",
	TriggerUnattached:                 "Unattached",
	TriggerUntaps:                     "Untaps",
	TriggerVote:                       "Vote",
}

var triggerModeCodes = func() StrCodes[TriggerMode] {
	e := make([]StrEntry[TriggerMode], 0, len(triggerModeNames))
	for m := TriggerMode(1); m < TriggerModeCount; m++ {
		e = append(e, StrEntry[TriggerMode]{Key: triggerModeNames[m], Val: m})
	}
	return NewStrCodes(e...)
}()

// TriggerModeOf resolves a name to its TriggerMode, 0 when it is outside the vocabulary.
func TriggerModeOf(name string) TriggerMode { return triggerModeCodes.Code(name) }

// String is the name m was resolved from ("" for 0).
func (m TriggerMode) String() string {
	if m < TriggerModeCount {
		return triggerModeNames[m]
	}
	return ""
}

// StaticMode is an S: line's Mode$ as a dense code.
type StaticMode uint16

// The vocabulary, in name order. 0 is a name outside it.
const (
	StaticAlternativeCost StaticMode = iota + 1
	StaticCanAttackDefender
	StaticCantAttack
	StaticCantAttackUnless
	StaticCantBeActivated
	StaticCantBeCast
	StaticCantBlockBy
	StaticCantBlockUnless
	StaticCantExile
	StaticCantGainLife
	StaticCantPreventDamage
	StaticCantPutCounter
	StaticCantRegenerate
	StaticCantSacrifice
	StaticCantTarget
	StaticCastWithFlash
	StaticCombatDamageToughness
	StaticContinuous
	StaticManaConvert
	StaticMustAttack
	StaticMustBlock
	StaticNumLoyaltyAct
	StaticOptionalCost
	StaticRaiseCost
	StaticReduceCost
	StaticSetCost
	StaticUnspentMana
	StaticModeCount = iota + 1 // one past the last; sizes a dense per-StaticMode array
)

var staticModeNames = [StaticModeCount]string{
	StaticAlternativeCost:       "AlternativeCost",
	StaticCanAttackDefender:     "CanAttackDefender",
	StaticCantAttack:            "CantAttack",
	StaticCantAttackUnless:      "CantAttackUnless",
	StaticCantBeActivated:       "CantBeActivated",
	StaticCantBeCast:            "CantBeCast",
	StaticCantBlockBy:           "CantBlockBy",
	StaticCantBlockUnless:       "CantBlockUnless",
	StaticCantExile:             "CantExile",
	StaticCantGainLife:          "CantGainLife",
	StaticCantPreventDamage:     "CantPreventDamage",
	StaticCantPutCounter:        "CantPutCounter",
	StaticCantRegenerate:        "CantRegenerate",
	StaticCantSacrifice:         "CantSacrifice",
	StaticCantTarget:            "CantTarget",
	StaticCastWithFlash:         "CastWithFlash",
	StaticCombatDamageToughness: "CombatDamageToughness",
	StaticContinuous:            "Continuous",
	StaticManaConvert:           "ManaConvert",
	StaticMustAttack:            "MustAttack",
	StaticMustBlock:             "MustBlock",
	StaticNumLoyaltyAct:         "NumLoyaltyAct",
	StaticOptionalCost:          "OptionalCost",
	StaticRaiseCost:             "RaiseCost",
	StaticReduceCost:            "ReduceCost",
	StaticSetCost:               "SetCost",
	StaticUnspentMana:           "UnspentMana",
}

var staticModeCodes = func() StrCodes[StaticMode] {
	e := make([]StrEntry[StaticMode], 0, len(staticModeNames))
	for m := StaticMode(1); m < StaticModeCount; m++ {
		e = append(e, StrEntry[StaticMode]{Key: staticModeNames[m], Val: m})
	}
	return NewStrCodes(e...)
}()

// StaticModeOf resolves a name to its StaticMode, 0 when it is outside the vocabulary.
func StaticModeOf(name string) StaticMode { return staticModeCodes.Code(name) }

// String is the name m was resolved from ("" for 0).
func (m StaticMode) String() string {
	if m < StaticModeCount {
		return staticModeNames[m]
	}
	return ""
}

// ReplEvent is an R: line's Event$ as a dense code.
type ReplEvent uint16

// The vocabulary, in name order. 0 is a name outside it.
const (
	ReplAddCounter ReplEvent = iota + 1
	ReplAttached
	ReplBeginPhase
	ReplBeginTurn
	ReplCascade
	ReplCounter
	ReplCreateToken
	ReplDamageDone
	ReplDraw
	ReplDrawCards
	ReplExplore
	ReplGainLife
	ReplGameLoss
	ReplGameWin
	ReplMoved
	ReplProduceMana
	ReplRollDice
	ReplRollPlanarDice
	ReplScry
	ReplTransform
	ReplTurnFaceUp
	ReplUntap
	ReplEventCount = iota + 1 // one past the last; sizes a dense per-ReplEvent array
)

var replEventNames = [ReplEventCount]string{
	ReplAddCounter:     "AddCounter",
	ReplAttached:       "Attached",
	ReplBeginPhase:     "BeginPhase",
	ReplBeginTurn:      "BeginTurn",
	ReplCascade:        "Cascade",
	ReplCounter:        "Counter",
	ReplCreateToken:    "CreateToken",
	ReplDamageDone:     "DamageDone",
	ReplDraw:           "Draw",
	ReplDrawCards:      "DrawCards",
	ReplExplore:        "Explore",
	ReplGainLife:       "GainLife",
	ReplGameLoss:       "GameLoss",
	ReplGameWin:        "GameWin",
	ReplMoved:          "Moved",
	ReplProduceMana:    "ProduceMana",
	ReplRollDice:       "RollDice",
	ReplRollPlanarDice: "RollPlanarDice",
	ReplScry:           "Scry",
	ReplTransform:      "Transform",
	ReplTurnFaceUp:     "TurnFaceUp",
	ReplUntap:          "Untap",
}

var replEventCodes = func() StrCodes[ReplEvent] {
	e := make([]StrEntry[ReplEvent], 0, len(replEventNames))
	for m := ReplEvent(1); m < ReplEventCount; m++ {
		e = append(e, StrEntry[ReplEvent]{Key: replEventNames[m], Val: m})
	}
	return NewStrCodes(e...)
}()

// ReplEventOf resolves a name to its ReplEvent, 0 when it is outside the vocabulary.
func ReplEventOf(name string) ReplEvent { return replEventCodes.Code(name) }

// String is the name m was resolved from ("" for 0).
func (m ReplEvent) String() string {
	if m < ReplEventCount {
		return replEventNames[m]
	}
	return ""
}

// ModeKind is t.Mode as a TriggerMode.
func (t *Trigger) ModeKind() TriggerMode {
	if t.modeBound {
		return t.mode
	}
	return TriggerModeOf(t.Mode)
}

// ModeKind is s.Mode as a StaticMode.
func (s *Static) ModeKind() StaticMode {
	if s.modeBound {
		return s.mode
	}
	return StaticModeOf(s.Mode)
}

// EventKind is r.Event as a ReplEvent.
func (r *Repl) EventKind() ReplEvent {
	if r.eventBound {
		return r.event
	}
	return ReplEventOf(r.Event)
}
