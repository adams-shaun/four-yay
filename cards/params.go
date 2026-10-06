package cards

import (
	"math/bits"
	"slices"
	"strings"
	"sync/atomic"
	"unsafe"
)

// ParamKey is a compiled parameter key: a dense ordinal over the fixed
// vocabulary below, the keys the rules hot paths read. A node's ParamSet
// answers a ParamKey read with a mask test and a popcount rank into a value
// slice instead of a string-keyed map probe. Keys outside the vocabulary are
// read from Params as before.
//
// The rules parameter census (rules/paramcensus_test.go) counts
// `x.Param(cards.PK<Key>)`, `x.ParamStr(cards.PK<Key>)`, `x.ParamCode(cards.PK<Key>)` and
// `x.HasParam(cards.PK<Key>)` as reads of <Key>, exactly like
// `x.Params["<Key>"]`, so a constant's name MUST be "PK" + the key text.
//
// The type is uint16 and the vocabulary may grow to paramKeyCap keys (the
// corpus names ~1160 distinct parameters in all). A ParamSet's mask is sized
// to the vocabulary actually declared (paramMaskWords), so a new key costs
// nothing until it crosses a 64-key word boundary, and then 10 bytes per
// compiled node.
type ParamKey uint16

const (
	pkNone ParamKey = iota
	PKAdamant
	PKAILogic
	PKActivation
	PKActivationAfterBlockers
	PKActivationFirstCombat
	PKActivationGameTypes
	PKActivationLimit
	PKActivationPhases
	PKActivationZone
	PKActivator
	PKActivatorThisTurnCast
	PKActiveZones
	PKAdapt
	PKAddAbilities
	PKAddAbility
	PKAddAllCreatureTypes
	PKAddColor
	PKAddColors
	PKAddKeyword
	PKAddKeywords
	PKAddPower
	PKAddSVar
	PKAddSVars
	PKAddStaticAbilities
	PKAddStaticAbility
	PKAddToughness
	PKAddTrigger
	PKAddTriggers
	PKAddType
	PKAddTypes
	PKAdjustLandPlays
	PKAffected
	PKAffectedZone
	PKAllValid
	PKAmount
	PKAnnounce
	PKAnyNumber
	PKAtRandom
	PKAttachedTo
	PKAttackedTarget
	PKAttacker
	PKAttackingPlayer
	PKBoast
	PKCantHaveKeyword
	PKCaster
	PKChangeNum
	PKChangeType
	PKCharacteristicDefining
	PKCheckSVar
	PKCheckSecondSVar
	PKChoiceOptional
	PKChoiceTitle
	PKChoiceZone
	PKChoices
	PKChooseFromDefined
	PKChooseFromList
	PKChooseOrder
	PKChooser
	PKClassBand
	PKClearImprinted
	PKCombatDamage
	PKCondition
	PKConditionActivationLimit
	PKConditionCheckSVar
	PKConditionCompare
	PKConditionCompare2
	PKConditionDefined
	PKConditionFirstCombat
	PKConditionNotPresent
	PKConditionPhases
	PKConditionPlayerTurn
	PKConditionPresent
	PKConditionPresent2
	PKConditionSVarCompare
	PKConditionZone
	PKController
	PKCost
	PKCounterNum
	PKCounterType
	PKBlockAllDefined
	PKDefined
	PKDefinedAttacker
	PKDefinedCards
	PKDefinedPlayer
	PKDefinedTarget
	PKDestAltSVar
	PKDestination
	PKDestinationAlternative
	PKDifferentNames
	PKDiscard
	PKDividedAsYouChoose
	PKDividedRandomly
	PKDuration
	PKETB
	PKEffectOnly
	PKEffectZone
	PKEvolve
	PKExcludeZone
	PKExcludedOrigins
	PKExecute
	PKExhaust
	PKFaceDown
	PKFirstForetell
	PKFirstTime
	PKForgetOtherRemembered
	PKFoundSearchingLibrary
	PKGainControl
	PKGainsAbilitiesLimitPerTurn
	PKGainsAbilitiesOf
	PKGainsAbilitiesOfDefined
	PKGainsAbilitiesOfZones
	PKGainsTriggerAbsOf
	PKGainsValidAbilities
	PKGameActivationLimit
	PKGoad
	PKImprint
	PKImprintCards
	PKImprintLast
	PKInstantSpeed
	PKIntoPlayTapped
	PKIsPresent
	PKIsPresent2
	PKKW
	PKKeyword
	PKLayer
	PKLeaveBattlefield
	PKLibraryPosition
	PKLifeAmount
	PKMandatory
	PKMax
	PKMaxTotalTargetCMC
	PKMayLookAt
	PKMayPlay
	PKMayPlayAltManaCost
	PKMayPlayWithoutManaCost
	PKMentor
	PKMin
	PKMode
	PKMonstrosity
	PKNewController
	PKNoLooking
	PKNoReveal
	PKNoShuffle
	PKNonLegendary
	PKNotThisAbility
	PKNumDmg
	PKNumRandomChoices
	PKNumTurns
	PKNumber
	PKObject
	PKOnlyFirstSpell
	PKOpponentTurn
	PKOptional
	PKOptionalDecider
	PKOptionalPrompt
	PKOrigin
	PKPhase
	PKPlacer
	PKPlayerTurn
	PKPowerUp
	PKPresentCompare
	PKPresentDefined
	PKPresentZone
	PKPrevent
	PKProduced
	PKPumpDuration
	PKPumpKeywords
	PKRandom
	PKRandomNumTargets
	PKReduceCost
	PKRelative
	PKRememberAmount
	PKRememberChanged
	PKRememberDamaged
	PKRememberLKI
	PKRememberObjects
	PKRememberOwnLoss
	PKRememberPumped
	PKRememberPut
	PKRememberTargets
	PKRemoveAllAbilities
	PKRemoveCardTypes
	PKRemoveCreatureTypes
	PKRemoveKeyword
	PKRemoveKeywords
	PKRemoveType
	PKReplacementResult
	PKRestrictValid
	PKReveal
	PKRevolt
	PKSVarCompare
	PKSecondary
	PKSecretly
	PKSelectPrompt
	PKSetColor
	PKSetName
	PKSetPower
	PKSetToughness
	PKShuffle
	PKSorcerySpeed
	PKSpellDescription
	PKStatic
	PKStaticAbilities
	PKStoreVoteNum
	PKSubAbility
	PKTapped
	PKTarget
	PKTargetMax
	PKTargetMin
	PKTargetType
	PKTargetUnique
	PKTargetValidTargeting
	PKTargetingPlayer
	PKTargetingPlayerControls
	PKTargetsAtRandom
	PKTargetsForEachPlayer
	PKTargetsWithControllerProperty
	PKTargetsWithDefinedController
	PKTargetsWithDifferentCMC
	PKTargetsWithDifferentControllers
	PKTargetsWithEqualToughness
	PKTargetsWithSameCardType
	PKTargetsWithSameController
	PKTargetsWithSameCreatureType
	PKTargetsWithSharedCardType
	PKTgtPrompt
	PKTgtZone
	PKTeamwork
	PKThisTurn
	PKTokenScript
	PKTriggerDescription
	PKTriggerZones
	PKTriggers
	PKType
	PKTypes
	PKTypeLimit
	PKUnattach
	PKUnlessCost
	PKUnlessPayer
	PKUnlessSwitched
	PKUpTo
	PKValid
	PKValidActivatingPlayer
	PKValidActivator
	PKValidAmountEach
	PKValidAttackers
	PKValidAttackersAmount
	PKValidBlocker
	PKValidCard
	PKValidCards
	PKValidCause
	PKValidChoices
	PKValidCreature
	PKValidDefender
	PKValidDescription
	PKValidEntity
	PKValidLKI
	PKValidMode
	PKValidObject
	PKValidPlayer
	PKValidSA
	PKValidSAonCard
	PKValidSource
	PKValidSpell
	PKValidTarget
	PKValidTgts
	PKValidToken
	PKValidZone
	PKVarName
	PKVarValue
	PKWard
	PKWithCountersAmount
	PKWithCountersType
	PKZone
	PKAIManaPref
	PKAbilities
	PKActivate
	PKAddKWs
	PKAfterPhase
	PKAllCards
	PKAllCounters
	PKAmountByChosenMap
	PKAnnihilator
	PKAtEOT
	PKAttacking
	PKAttributes
	PKBecomeStartingPlayer
	PKBranchConditionSVar
	PKBranchConditionSVarCompare
	PKChangeColorWord
	PKChangeController
	PKChangeSingleTarget
	PKChangeTypeWord
	PKChoiceAmount
	PKChoicePrompt
	PKChooseEach
	PKChooseFromDefinedCards
	PKChosenPile
	PKChosenSVar
	PKCipherCopy
	PKClearChosenCard
	PKClearChosenPlayer
	PKClearRemembered
	PKColors
	PKColorsFrom
	PKControlledByPlayer
	PKControllerUntaps
	PKCopyCard
	PKCounterType2
	PKDefinedDamagers
	PKDefinedMagnet
	PKDefinedPiles
	PKDiscardValid
	PKDiscardValidDesc
	PKDontPlaneswalkAway
	PKDungeon
	PKEachExistingCounter
	PKEachToItself
	PKEvenOddResults
	PKExclude
	PKExpression
	PKExtraPhase
	PKExtraPhaseDelayedTrigger
	PKExtraPhaseDelayedTriggerExcute
	PKExtraTurnDelayedTrigger
	PKExtraTurnDelayedTriggerExecute
	PKFaceDownSetType
	PKFalseSubAbility
	PKFlipUntilYouLose
	PKFlipper
	PKFollowedBy
	PKForEachPlayer
	PKForbiddenNewTypes
	PKForgetChanged
	PKForgetChosen
	PKForgetOtherTargets
	PKForgetPlayed
	PKFound
	PKGains
	PKHeadsSubAbility
	PKHiddenKeywords
	PKIgnoreFreeze
	PKImprintFound
	PKInvalidTypes
	PKKeywords
	PKLeftRightPile
	PKListTitle
	PKLook
	PKLoseControl
	PKLoseSubAbility
	PKMatchedAbility
	PKMaxRollsResults
	PKMayChooseTarget
	PKMayShuffle
	PKMinAmount
	PKName
	PKNextRoom
	PKNoCall
	PKNoLonger
	PKNoRegen
	PKNonBasicSpell
	PKNumCards
	PKNumPhases
	PKOrColors
	PKOtherSVar
	PKOtherwiseSubAbility
	PKOverwriteColors
	PKPhaseInOrOut
	PKPhaseout
	PKPlayerChoices
	PKPower
	PKPreventionSubAbility
	PKPumpZone
	PKRandomTarget
	PKRandomTargetRestriction
	PKRedistribute
	PKRememberAffected
	PKRememberAmass
	PKRememberAnimated
	PKRememberChosen
	PKRememberClasher
	PKRememberCloaked
	PKRememberControlled
	PKRememberCopies
	PKRememberCountered
	PKRememberCounteredCMC
	PKRememberCounteredSA
	PKRememberDestroyed
	PKRememberDifference
	PKRememberDiscarded
	PKRememberDiscardingPlayers
	PKRememberDiscovered
	PKRememberEach
	PKRememberExchanged
	PKRememberFound
	PKRememberGoaded
	PKRememberInvestigatingPlayers
	PKRememberKept
	PKRememberLoser
	PKRememberManifested
	PKRememberMilled
	PKRememberNumber
	PKRememberPeeked
	PKRememberRemovedCards
	PKRememberRemovedFromCombat
	PKRememberResult
	PKRememberRevealed
	PKRememberSacrificed
	PKRememberTapped
	PKRememberUntapped
	PKRemoveConditionSVar
	PKRemoveLandTypes
	PKRemoveTypes
	PKRepeatCheckSVar
	PKRepeatCompare
	PKRepeatDefined
	PKRepeatOptional
	PKRepeatOptionalDecider
	PKRepeatPresent
	PKRepeatSVarCompare
	PKRepeatSubAbility
	PKReplaceColor
	PKReplaceMana
	PKReplaceOnly
	PKReplaceType
	PKReplacements
	PKRestrictFromValid
	PKRestrictFromZone
	PKRestrictToRemembered
	PKResultSVar
	PKResultSubAbilities
	PKRevealAllValid
	PKRevealDefined
	PKRevealOptional
	PKRevealType
	PKRevealValid
	PKRoomName
	PKSVar
	PKSacValid
	PKSeparator
	PKSetLoyalty
	PKShieldEffectTarget
	PKShowMilledCards
	PKShowSacrificedCards
	PKSkipUntap
	PKSource
	PKStartingWith
	PKStaticCommandCheckSVar
	PKStaticCommandSVarCompare
	PKStaticEffect
	PKStaticEffectCheckSVar
	PKStaticEffectSVarCompare
	PKStrictAmount
	PKTailsSubAbility
	PKTapper
	PKTapperController
	PKTargetRestriction
	PKToEachOther
	PKToughness
	PKTrueSubAbility
	PKTwoColors
	PKUnchosenPile
	PKUnlessResolveSubs
	PKUnlessType
	PKUnmatchedAbility
	PKUntap
	PKUntapType
	PKUntapUpTo
	PKUseDifferenceBetweenRolls
	PKUseHighestRoll
	PKValidCards2
	PKValidDefined
	PKValidPlayers
	PKValidTypes
	PKVerb
	PKWinSubAbility
	PKWontPhaseInNormal
	PKsVars
	PKstaticAbilities
	PKActivatorThisTurnCastEach
	PKActivePhases
	PKAlone
	PKAlternateCost
	PKAlternativeCost
	PKAnnounceTitle
	PKAttacked
	PKBeginTurn
	PKBlessing
	PKCheckDefinedPlayer
	PKCheckOnTriggeredCard
	PKClassLevel
	PKColor
	PKControlOpponentsSearchingLibrary
	PKCounterAmount
	PKDamageAmount
	PKDelirium
	PKDescription
	PKDethrone
	PKDrawLimit
	PKEcho
	PKEnduringStory
	PKExtraTurn
	PKFirstAttack
	PKFirstCardInDrawStep
	PKFirstCombat
	PKFirstExtraCardDrawnThisTurn
	PKForCost
	PKForEachShard
	PKHellbent
	PKIgnoreGeneric
	PKIncrement
	PKIsCombat
	PKIsDamage
	PKIsSingleTarget
	PKKeywordLine
	PKLifeTotal
	PKList
	PKManaAmount
	PKManaConversion
	PKMaxAttackers
	PKMayPlayLimit
	PKMayPlayText
	PKMetalcraft
	PKMinLimit
	PKMinMana
	PKModeCost
	PKMustAttack
	PKMyriad
	PKNatural
	PKNewCounterAmount
	PKNoResolvingCheck
	PKNotFirstCardInDrawStep
	PKNum
	PKOneOff
	PKOrderDuplicates
	PKPhaseCount
	PKPhases
	PKPlaneswalker
	PKPlayer
	PKPreventionShield
	PKRaiseTo
	PKReduceAmount
	PKRememberCostMana
	PKRememberingAttacker
	PKReplaceWith
	PKResolvedLimit
	PKResolvedOnly
	PKResult
	PKRolledToVisitAttractions
	PKSetMaxHandSize
	PKSkip
	PKStackDescription
	PKTargetsValid
	PKThisDoor
	PKThreshold
	PKToBottom
	PKTraining
	PKTrigger
	PKTriggerController
	PKUltimate
	PKUnlessDefender
	PKUnlessValidTarget
	PKValidAttachment
	PKValidAttacked
	PKValidAttacker
	PKValidAttackingPlayer
	PKValidBlocked
	PKValidCounterType
	PKValidDefenders
	PKValidEnlisted
	PKValidExplored
	PKValidExplorer
	PKValidLoseReason
	PKValidNewController
	PKValidObjectToSource
	PKValidOriginalController
	PKValidResult
	PKValidSides
	PKValidStepTurnToController
	PKValidTrigger
	PKValidTurned
	PKValue
	PKWon
	PKXMax
	PKXMin
	PKAddsCounters
	PKAddsNoCounter
	PKAlternativeDecider
	PKAmountFromVotes
	PKAtEOTTrig
	PKAttachedToPlayer
	PKBolster
	PKCanRepeatModes
	PKChangeZoneTable
	PKCharmNum
	PKChoiceRestriction
	PKChooseDifferent
	PKClearNotedCardsFor
	PKClearRememberedBeforeLoop
	PKColorOrType
	PKCounterNumPerDefined
	PKCounterTypePerDefined
	PKDamageMap
	PKDamageSource
	PKDefinedName
	PKDestAltSVarCompare
	PKEachFromSource
	PKEffectOwner
	PKExactly
	PKExcessSVar
	PKExcessSVarCondition
	PKExileFaceDown
	PKExileOnMoved
	PKFaceDownPower
	PKFaceDownToughness
	PKFallbackAbility
	PKForetold
	PKForgetCounter
	PKForgetImprinted
	PKForgetOnCast
	PKForgetOnMoved
	PKForgetOnPhasedIn
	PKHidden
	PKImprintOnHost
	PKImprintTokens
	PKKWChoice
	PKLibraryPositionAlternative
	PKMaxRevealed
	PKMaxTotalTargetPower
	PKMinCharmNum
	PKNextTurn
	PKNoteCards
	PKNoteCardsFor
	PKNoteNumber
	PKNumAtt
	PKNumDef
	PKOriginAlternative
	PKPawprint
	PKPersistentMana
	PKPersistentUntilEndOfCombat
	PKPopulate
	PKRandomCompare
	PKRandomCompareSVar
	PKRandomCopied
	PKRandomNum
	PKRandomOrder
	PKRandomType
	PKReflectProperty
	PKRelativeTarget
	PKRememberAttached
	PKRememberCards
	PKRememberChain
	PKRememberDrawn
	PKRememberOriginalTokens
	PKRememberSearched
	PKRememberTokens
	PKRememberVotedObjects
	PKRenown
	PKReorder
	PKRepeatCards
	PKRepeatOptionalForEachPlayer
	PKRepeatOptionalMessage
	PKRepeatPlayers
	PKRepeatSpellAbilities
	PKRepeatTargeted
	PKRepeatTypesFrom
	PKReplaceDyingDefined
	PKReplacementEffects
	PKSetChosenNumber
	PKShareLandType
	PKShuffleNonMandatory
	PKStackable
	PKSupport
	PKTargetsWithDifferentNames
	PKTargetsWithSharedTypes
	PKTempRemember
	PKTokenAttacking
	PKTokenOwner
	PKTokenRemembered
	PKTokenTapped
	PKTransformed
	PKTriggersWhenSpent
	PKUnearth
	PKUnimprint
	PKUpto
	PKUseAllOriginZones
	PKValidSupportedCopy
	PKVoteCard
	PKVoteMessage
	PKVotePlayer
	PKVoteSubAbility
	PKVoteTiedAbility
	PKWithDifferentNames
	PKWithMayLook
	PKWithTotalCMC
	PKWithTotalCardTypes
	PKXChoice
	PKTgtPrompt2
	PKSorcerySpeed2
	PKChoose
	PKDefinedPlayerChooses
	PKChoiceNum
	PKChooseCounter
	PKCounterTypeChoice
	PKPromptToSkipOptionalAbility
	PKOptionalAbilityPrompt
	PKSetChosenMode
	PKWithoutManaCost
	PKPlayCost
	PKReplaceGraveyard
	PKReplaceGraveyardValid
	PKImprintPlayed
	PKShowCards
	PKValidCrew
	PKValidSaddled
	PKFirstTimeSaddled
	// PKLevel is the target level of a Class level-up activator (kw:Class
	// synthesises `AB$ ClassLevelUp | Level$ N`; CR 716.2d sets the
	// designation to N). Appended after the vocabulary inherited from main.
	PKLevel
	PKTapCreaturesForMana
	// DigMultiple's distinct choice and remainder grammar (append-only).
	PKDigNum
	PKChangeValid
	PKDestinationZone
	PKDestinationZone2
	PKChosenZone
	PKRestRandomOrder
	PKImprintRest
	PKChangeLater
	PKChooseAmount
	// Meld-specific operands. Appended: earlier parameter ordinals are stable.
	PKPrimary
	PKSecondaryType
	// ImmediateTrigger's RememberSVarAmount$ rider (New Way Forward): the
	// per-trigger remembered Integer the reflexive ability's
	// Count$TriggerRememberAmount reads. Appended so earlier ordinals stay
	// stable and a pre-compiled IR cache stays valid.
	PKRememberSVarAmount
	paramKeyCount
)

// paramKeyCap bounds the vocabulary: 2048 keys, room for every parameter
// name the corpus uses. It is a budget, not a representation limit (ParamKey
// is uint16): a ParamSet carries paramMaskWords mask words plus a uint16 rank
// per word, so the vocabulary's size is paid on every compiled node
// (~85k over the corpus). Raising it is a memory decision; see
// TestParamKeyRoomRemains and TestParamSetSizeTracksVocabulary.
const paramKeyCap = 2048

// The vocabulary fits the budget.
const _ = uint(paramKeyCap - int(paramKeyCount))

// paramMaskWords is a ParamMask's width in 64-bit words: just enough for the
// declared vocabulary (ordinals 0..paramKeyCount-1), so a ParamSet is no
// wider than the keys that exist.
//
// The dense mask was chosen over a two-level (summary word plus dense
// non-zero words) layout by measurement over the corpus's 85k compiled
// nodes: the two-level read is one more dependent load and measured about
// twice as slow on a cold sweep, while the dense mask is the same size as the
// uint8-era set until the vocabulary passes 256 keys and only overtakes the
// two-level layout's memory past ~640 keys.
const paramMaskWords = (int(paramKeyCount) + 63) / 64

// paramKeyNames maps each ParamKey to its Forge key text.
var paramKeyNames = [paramKeyCount]string{
	PKAdamant:                          "Adamant",
	PKAILogic:                          "AILogic",
	PKActivation:                       "Activation",
	PKActivationAfterBlockers:          "ActivationAfterBlockers",
	PKActivationFirstCombat:            "ActivationFirstCombat",
	PKActivationGameTypes:              "ActivationGameTypes",
	PKActivationLimit:                  "ActivationLimit",
	PKActivationPhases:                 "ActivationPhases",
	PKActivationZone:                   "ActivationZone",
	PKActivator:                        "Activator",
	PKActivatorThisTurnCast:            "ActivatorThisTurnCast",
	PKActiveZones:                      "ActiveZones",
	PKAdapt:                            "Adapt",
	PKAddAbilities:                     "AddAbilities",
	PKAddAbility:                       "AddAbility",
	PKAddAllCreatureTypes:              "AddAllCreatureTypes",
	PKAddColor:                         "AddColor",
	PKAddColors:                        "AddColors",
	PKAddKeyword:                       "AddKeyword",
	PKAddKeywords:                      "AddKeywords",
	PKAddPower:                         "AddPower",
	PKAddSVar:                          "AddSVar",
	PKAddSVars:                         "AddSVars",
	PKAddStaticAbilities:               "AddStaticAbilities",
	PKAddStaticAbility:                 "AddStaticAbility",
	PKAddToughness:                     "AddToughness",
	PKAddTrigger:                       "AddTrigger",
	PKAddTriggers:                      "AddTriggers",
	PKAddType:                          "AddType",
	PKAddTypes:                         "AddTypes",
	PKAdjustLandPlays:                  "AdjustLandPlays",
	PKAffected:                         "Affected",
	PKAffectedZone:                     "AffectedZone",
	PKAllValid:                         "AllValid",
	PKAmount:                           "Amount",
	PKAnnounce:                         "Announce",
	PKAnyNumber:                        "AnyNumber",
	PKAtRandom:                         "AtRandom",
	PKAttachedTo:                       "AttachedTo",
	PKAttackedTarget:                   "AttackedTarget",
	PKAttacker:                         "Attacker",
	PKAttackingPlayer:                  "AttackingPlayer",
	PKBoast:                            "Boast",
	PKCantHaveKeyword:                  "CantHaveKeyword",
	PKCaster:                           "Caster",
	PKChangeNum:                        "ChangeNum",
	PKChangeType:                       "ChangeType",
	PKCharacteristicDefining:           "CharacteristicDefining",
	PKCheckSVar:                        "CheckSVar",
	PKCheckSecondSVar:                  "CheckSecondSVar",
	PKChoiceOptional:                   "ChoiceOptional",
	PKChoiceTitle:                      "ChoiceTitle",
	PKChoiceZone:                       "ChoiceZone",
	PKChoices:                          "Choices",
	PKChooseFromDefined:                "ChooseFromDefined",
	PKChooseFromList:                   "ChooseFromList",
	PKChooseOrder:                      "ChooseOrder",
	PKChooser:                          "Chooser",
	PKClassBand:                        "ClassBand",
	PKClearImprinted:                   "ClearImprinted",
	PKCombatDamage:                     "CombatDamage",
	PKCondition:                        "Condition",
	PKConditionActivationLimit:         "ConditionActivationLimit",
	PKConditionCheckSVar:               "ConditionCheckSVar",
	PKConditionCompare:                 "ConditionCompare",
	PKConditionCompare2:                "ConditionCompare2",
	PKConditionDefined:                 "ConditionDefined",
	PKConditionFirstCombat:             "ConditionFirstCombat",
	PKConditionNotPresent:              "ConditionNotPresent",
	PKConditionPhases:                  "ConditionPhases",
	PKConditionPlayerTurn:              "ConditionPlayerTurn",
	PKConditionPresent:                 "ConditionPresent",
	PKConditionPresent2:                "ConditionPresent2",
	PKConditionSVarCompare:             "ConditionSVarCompare",
	PKConditionZone:                    "ConditionZone",
	PKController:                       "Controller",
	PKCost:                             "Cost",
	PKCounterNum:                       "CounterNum",
	PKCounterType:                      "CounterType",
	PKDefined:                          "Defined",
	PKBlockAllDefined:                  "BlockAllDefined",
	PKDefinedAttacker:                  "DefinedAttacker",
	PKDefinedCards:                     "DefinedCards",
	PKDefinedPlayer:                    "DefinedPlayer",
	PKDefinedTarget:                    "DefinedTarget",
	PKDestAltSVar:                      "DestAltSVar",
	PKDestination:                      "Destination",
	PKDestinationAlternative:           "DestinationAlternative",
	PKDifferentNames:                   "DifferentNames",
	PKDiscard:                          "Discard",
	PKDividedAsYouChoose:               "DividedAsYouChoose",
	PKDividedRandomly:                  "DividedRandomly",
	PKDuration:                         "Duration",
	PKETB:                              "ETB",
	PKEffectOnly:                       "EffectOnly",
	PKEffectZone:                       "EffectZone",
	PKEvolve:                           "Evolve",
	PKExcludeZone:                      "ExcludeZone",
	PKExcludedOrigins:                  "ExcludedOrigins",
	PKExecute:                          "Execute",
	PKExhaust:                          "Exhaust",
	PKFaceDown:                         "FaceDown",
	PKFirstForetell:                    "FirstForetell",
	PKFirstTime:                        "FirstTime",
	PKFirstTimeSaddled:                 "FirstTimeSaddled",
	PKForgetOtherRemembered:            "ForgetOtherRemembered",
	PKFoundSearchingLibrary:            "FoundSearchingLibrary",
	PKGainControl:                      "GainControl",
	PKGainsAbilitiesLimitPerTurn:       "GainsAbilitiesLimitPerTurn",
	PKGainsAbilitiesOf:                 "GainsAbilitiesOf",
	PKGainsAbilitiesOfDefined:          "GainsAbilitiesOfDefined",
	PKGainsAbilitiesOfZones:            "GainsAbilitiesOfZones",
	PKGainsTriggerAbsOf:                "GainsTriggerAbsOf",
	PKGainsValidAbilities:              "GainsValidAbilities",
	PKGameActivationLimit:              "GameActivationLimit",
	PKGoad:                             "Goad",
	PKImprint:                          "Imprint",
	PKImprintCards:                     "ImprintCards",
	PKImprintLast:                      "ImprintLast",
	PKInstantSpeed:                     "InstantSpeed",
	PKIntoPlayTapped:                   "IntoPlayTapped",
	PKIsPresent:                        "IsPresent",
	PKIsPresent2:                       "IsPresent2",
	PKKW:                               "KW",
	PKKeyword:                          "Keyword",
	PKLayer:                            "Layer",
	PKLeaveBattlefield:                 "LeaveBattlefield",
	PKLibraryPosition:                  "LibraryPosition",
	PKLifeAmount:                       "LifeAmount",
	PKMandatory:                        "Mandatory",
	PKMax:                              "Max",
	PKMaxTotalTargetCMC:                "MaxTotalTargetCMC",
	PKMayLookAt:                        "MayLookAt",
	PKMayPlay:                          "MayPlay",
	PKMayPlayAltManaCost:               "MayPlayAltManaCost",
	PKMayPlayWithoutManaCost:           "MayPlayWithoutManaCost",
	PKMentor:                           "Mentor",
	PKMin:                              "Min",
	PKMode:                             "Mode",
	PKMonstrosity:                      "Monstrosity",
	PKNewController:                    "NewController",
	PKNoLooking:                        "NoLooking",
	PKNoReveal:                         "NoReveal",
	PKNoShuffle:                        "NoShuffle",
	PKNonLegendary:                     "NonLegendary",
	PKNotThisAbility:                   "NotThisAbility",
	PKNumDmg:                           "NumDmg",
	PKNumRandomChoices:                 "NumRandomChoices",
	PKNumTurns:                         "NumTurns",
	PKNumber:                           "Number",
	PKObject:                           "Object",
	PKOnlyFirstSpell:                   "OnlyFirstSpell",
	PKOpponentTurn:                     "OpponentTurn",
	PKOptional:                         "Optional",
	PKOptionalDecider:                  "OptionalDecider",
	PKOptionalPrompt:                   "OptionalPrompt",
	PKOrigin:                           "Origin",
	PKPhase:                            "Phase",
	PKPlacer:                           "Placer",
	PKPlayerTurn:                       "PlayerTurn",
	PKPowerUp:                          "PowerUp",
	PKPresentCompare:                   "PresentCompare",
	PKPresentDefined:                   "PresentDefined",
	PKPresentZone:                      "PresentZone",
	PKPrevent:                          "Prevent",
	PKProduced:                         "Produced",
	PKPumpDuration:                     "PumpDuration",
	PKPumpKeywords:                     "PumpKeywords",
	PKRandom:                           "Random",
	PKRandomNumTargets:                 "RandomNumTargets",
	PKReduceCost:                       "ReduceCost",
	PKRelative:                         "Relative",
	PKRememberAmount:                   "RememberAmount",
	PKRememberChanged:                  "RememberChanged",
	PKRememberDamaged:                  "RememberDamaged",
	PKRememberLKI:                      "RememberLKI",
	PKRememberObjects:                  "RememberObjects",
	PKRememberOwnLoss:                  "RememberOwnLoss",
	PKRememberPumped:                   "RememberPumped",
	PKRememberPut:                      "RememberPut",
	PKRememberSVarAmount:               "RememberSVarAmount",
	PKRememberTargets:                  "RememberTargets",
	PKRemoveAllAbilities:               "RemoveAllAbilities",
	PKRemoveCardTypes:                  "RemoveCardTypes",
	PKRemoveCreatureTypes:              "RemoveCreatureTypes",
	PKRemoveKeyword:                    "RemoveKeyword",
	PKRemoveKeywords:                   "RemoveKeywords",
	PKRemoveType:                       "RemoveType",
	PKReplacementResult:                "ReplacementResult",
	PKRestrictValid:                    "RestrictValid",
	PKReveal:                           "Reveal",
	PKRevolt:                           "Revolt",
	PKSVarCompare:                      "SVarCompare",
	PKSecondary:                        "Secondary",
	PKSecretly:                         "Secretly",
	PKSelectPrompt:                     "SelectPrompt",
	PKSetColor:                         "SetColor",
	PKSetName:                          "SetName",
	PKSetPower:                         "SetPower",
	PKSetToughness:                     "SetToughness",
	PKShuffle:                          "Shuffle",
	PKSorcerySpeed:                     "SorcerySpeed",
	PKSpellDescription:                 "SpellDescription",
	PKStatic:                           "Static",
	PKStaticAbilities:                  "StaticAbilities",
	PKStoreVoteNum:                     "StoreVoteNum",
	PKSubAbility:                       "SubAbility",
	PKTapped:                           "Tapped",
	PKTarget:                           "Target",
	PKTargetMax:                        "TargetMax",
	PKTargetMin:                        "TargetMin",
	PKTargetType:                       "TargetType",
	PKTargetUnique:                     "TargetUnique",
	PKTargetValidTargeting:             "TargetValidTargeting",
	PKTargetingPlayer:                  "TargetingPlayer",
	PKTargetingPlayerControls:          "TargetingPlayerControls",
	PKTargetsAtRandom:                  "TargetsAtRandom",
	PKTargetsForEachPlayer:             "TargetsForEachPlayer",
	PKTargetsWithControllerProperty:    "TargetsWithControllerProperty",
	PKTargetsWithDefinedController:     "TargetsWithDefinedController",
	PKTargetsWithDifferentCMC:          "TargetsWithDifferentCMC",
	PKTargetsWithDifferentControllers:  "TargetsWithDifferentControllers",
	PKTargetsWithEqualToughness:        "TargetsWithEqualToughness",
	PKTargetsWithSameCardType:          "TargetsWithSameCardType",
	PKTargetsWithSameController:        "TargetsWithSameController",
	PKTargetsWithSameCreatureType:      "TargetsWithSameCreatureType",
	PKTargetsWithSharedCardType:        "TargetsWithSharedCardType",
	PKTgtPrompt:                        "TgtPrompt",
	PKTgtZone:                          "TgtZone",
	PKTeamwork:                         "Teamwork",
	PKThisTurn:                         "ThisTurn",
	PKTokenScript:                      "TokenScript",
	PKTriggerDescription:               "TriggerDescription",
	PKTriggerZones:                     "TriggerZones",
	PKTriggers:                         "Triggers",
	PKType:                             "Type",
	PKTypes:                            "Types",
	PKTypeLimit:                        "TypeLimit",
	PKUnattach:                         "Unattach",
	PKUnlessCost:                       "UnlessCost",
	PKUnlessPayer:                      "UnlessPayer",
	PKUnlessSwitched:                   "UnlessSwitched",
	PKUpTo:                             "UpTo",
	PKValid:                            "Valid",
	PKValidActivatingPlayer:            "ValidActivatingPlayer",
	PKValidActivator:                   "ValidActivator",
	PKValidAmountEach:                  "ValidAmountEach",
	PKValidAttackers:                   "ValidAttackers",
	PKValidAttackersAmount:             "ValidAttackersAmount",
	PKValidBlocker:                     "ValidBlocker",
	PKValidCard:                        "ValidCard",
	PKValidCards:                       "ValidCards",
	PKValidCause:                       "ValidCause",
	PKValidChoices:                     "ValidChoices",
	PKValidCreature:                    "ValidCreature",
	PKValidDefender:                    "ValidDefender",
	PKValidDescription:                 "ValidDescription",
	PKValidEntity:                      "ValidEntity",
	PKValidLKI:                         "ValidLKI",
	PKValidMode:                        "ValidMode",
	PKValidObject:                      "ValidObject",
	PKValidPlayer:                      "ValidPlayer",
	PKValidSA:                          "ValidSA",
	PKValidSAonCard:                    "ValidSAonCard",
	PKValidSource:                      "ValidSource",
	PKValidSpell:                       "ValidSpell",
	PKValidTarget:                      "ValidTarget",
	PKValidTgts:                        "ValidTgts",
	PKValidToken:                       "ValidToken",
	PKValidZone:                        "ValidZone",
	PKVarName:                          "VarName",
	PKVarValue:                         "VarValue",
	PKWard:                             "Ward",
	PKWithCountersAmount:               "WithCountersAmount",
	PKWithCountersType:                 "WithCountersType",
	PKZone:                             "Zone",
	PKAIManaPref:                       "AIManaPref",
	PKAbilities:                        "Abilities",
	PKActivate:                         "Activate",
	PKAddKWs:                           "AddKWs",
	PKAfterPhase:                       "AfterPhase",
	PKAllCards:                         "AllCards",
	PKAllCounters:                      "AllCounters",
	PKAmountByChosenMap:                "AmountByChosenMap",
	PKAnnihilator:                      "Annihilator",
	PKAtEOT:                            "AtEOT",
	PKAttacking:                        "Attacking",
	PKAttributes:                       "Attributes",
	PKBecomeStartingPlayer:             "BecomeStartingPlayer",
	PKBranchConditionSVar:              "BranchConditionSVar",
	PKBranchConditionSVarCompare:       "BranchConditionSVarCompare",
	PKChangeColorWord:                  "ChangeColorWord",
	PKChangeController:                 "ChangeController",
	PKChangeSingleTarget:               "ChangeSingleTarget",
	PKChangeTypeWord:                   "ChangeTypeWord",
	PKChoiceAmount:                     "ChoiceAmount",
	PKChoicePrompt:                     "ChoicePrompt",
	PKChooseEach:                       "ChooseEach",
	PKChooseFromDefinedCards:           "ChooseFromDefinedCards",
	PKChosenPile:                       "ChosenPile",
	PKChosenSVar:                       "ChosenSVar",
	PKCipherCopy:                       "CipherCopy",
	PKClearChosenCard:                  "ClearChosenCard",
	PKClearChosenPlayer:                "ClearChosenPlayer",
	PKClearRemembered:                  "ClearRemembered",
	PKColors:                           "Colors",
	PKColorsFrom:                       "ColorsFrom",
	PKControlledByPlayer:               "ControlledByPlayer",
	PKControllerUntaps:                 "ControllerUntaps",
	PKCopyCard:                         "CopyCard",
	PKCounterType2:                     "CounterType2",
	PKDefinedDamagers:                  "DefinedDamagers",
	PKDefinedMagnet:                    "DefinedMagnet",
	PKDefinedPiles:                     "DefinedPiles",
	PKDiscardValid:                     "DiscardValid",
	PKDiscardValidDesc:                 "DiscardValidDesc",
	PKDontPlaneswalkAway:               "DontPlaneswalkAway",
	PKDungeon:                          "Dungeon",
	PKEachExistingCounter:              "EachExistingCounter",
	PKEachToItself:                     "EachToItself",
	PKEvenOddResults:                   "EvenOddResults",
	PKExclude:                          "Exclude",
	PKExpression:                       "Expression",
	PKExtraPhase:                       "ExtraPhase",
	PKExtraPhaseDelayedTrigger:         "ExtraPhaseDelayedTrigger",
	PKExtraPhaseDelayedTriggerExcute:   "ExtraPhaseDelayedTriggerExcute",
	PKExtraTurnDelayedTrigger:          "ExtraTurnDelayedTrigger",
	PKExtraTurnDelayedTriggerExecute:   "ExtraTurnDelayedTriggerExecute",
	PKFaceDownSetType:                  "FaceDownSetType",
	PKFalseSubAbility:                  "FalseSubAbility",
	PKFlipUntilYouLose:                 "FlipUntilYouLose",
	PKFlipper:                          "Flipper",
	PKFollowedBy:                       "FollowedBy",
	PKForEachPlayer:                    "ForEachPlayer",
	PKForbiddenNewTypes:                "ForbiddenNewTypes",
	PKForgetChanged:                    "ForgetChanged",
	PKForgetChosen:                     "ForgetChosen",
	PKForgetOtherTargets:               "ForgetOtherTargets",
	PKForgetPlayed:                     "ForgetPlayed",
	PKFound:                            "Found",
	PKGains:                            "Gains",
	PKHeadsSubAbility:                  "HeadsSubAbility",
	PKHiddenKeywords:                   "HiddenKeywords",
	PKIgnoreFreeze:                     "IgnoreFreeze",
	PKImprintFound:                     "ImprintFound",
	PKInvalidTypes:                     "InvalidTypes",
	PKKeywords:                         "Keywords",
	PKLeftRightPile:                    "LeftRightPile",
	PKListTitle:                        "ListTitle",
	PKLook:                             "Look",
	PKLoseControl:                      "LoseControl",
	PKLoseSubAbility:                   "LoseSubAbility",
	PKMatchedAbility:                   "MatchedAbility",
	PKMaxRollsResults:                  "MaxRollsResults",
	PKMayChooseTarget:                  "MayChooseTarget",
	PKMayShuffle:                       "MayShuffle",
	PKMinAmount:                        "MinAmount",
	PKName:                             "Name",
	PKNextRoom:                         "NextRoom",
	PKNoCall:                           "NoCall",
	PKNoLonger:                         "NoLonger",
	PKNoRegen:                          "NoRegen",
	PKNonBasicSpell:                    "NonBasicSpell",
	PKNumCards:                         "NumCards",
	PKNumPhases:                        "NumPhases",
	PKOrColors:                         "OrColors",
	PKOtherSVar:                        "OtherSVar",
	PKOtherwiseSubAbility:              "OtherwiseSubAbility",
	PKOverwriteColors:                  "OverwriteColors",
	PKPhaseInOrOut:                     "PhaseInOrOut",
	PKPhaseout:                         "Phaseout",
	PKPlayerChoices:                    "PlayerChoices",
	PKPower:                            "Power",
	PKPreventionSubAbility:             "PreventionSubAbility",
	PKPumpZone:                         "PumpZone",
	PKRandomTarget:                     "RandomTarget",
	PKRandomTargetRestriction:          "RandomTargetRestriction",
	PKRedistribute:                     "Redistribute",
	PKRememberAffected:                 "RememberAffected",
	PKRememberAmass:                    "RememberAmass",
	PKRememberAnimated:                 "RememberAnimated",
	PKRememberChosen:                   "RememberChosen",
	PKRememberClasher:                  "RememberClasher",
	PKRememberCloaked:                  "RememberCloaked",
	PKRememberControlled:               "RememberControlled",
	PKRememberCopies:                   "RememberCopies",
	PKRememberCountered:                "RememberCountered",
	PKRememberCounteredCMC:             "RememberCounteredCMC",
	PKRememberCounteredSA:              "RememberCounteredSA",
	PKRememberDestroyed:                "RememberDestroyed",
	PKRememberDifference:               "RememberDifference",
	PKRememberDiscarded:                "RememberDiscarded",
	PKRememberDiscardingPlayers:        "RememberDiscardingPlayers",
	PKRememberDiscovered:               "RememberDiscovered",
	PKRememberEach:                     "RememberEach",
	PKRememberExchanged:                "RememberExchanged",
	PKRememberFound:                    "RememberFound",
	PKRememberGoaded:                   "RememberGoaded",
	PKRememberInvestigatingPlayers:     "RememberInvestigatingPlayers",
	PKRememberKept:                     "RememberKept",
	PKRememberLoser:                    "RememberLoser",
	PKRememberManifested:               "RememberManifested",
	PKRememberMilled:                   "RememberMilled",
	PKRememberNumber:                   "RememberNumber",
	PKRememberPeeked:                   "RememberPeeked",
	PKRememberRemovedCards:             "RememberRemovedCards",
	PKRememberRemovedFromCombat:        "RememberRemovedFromCombat",
	PKRememberResult:                   "RememberResult",
	PKRememberRevealed:                 "RememberRevealed",
	PKRememberSacrificed:               "RememberSacrificed",
	PKRememberTapped:                   "RememberTapped",
	PKRememberUntapped:                 "RememberUntapped",
	PKRemoveConditionSVar:              "RemoveConditionSVar",
	PKRemoveLandTypes:                  "RemoveLandTypes",
	PKRemoveTypes:                      "RemoveTypes",
	PKRepeatCheckSVar:                  "RepeatCheckSVar",
	PKRepeatCompare:                    "RepeatCompare",
	PKRepeatDefined:                    "RepeatDefined",
	PKRepeatOptional:                   "RepeatOptional",
	PKRepeatOptionalDecider:            "RepeatOptionalDecider",
	PKRepeatPresent:                    "RepeatPresent",
	PKRepeatSVarCompare:                "RepeatSVarCompare",
	PKRepeatSubAbility:                 "RepeatSubAbility",
	PKReplaceColor:                     "ReplaceColor",
	PKReplaceMana:                      "ReplaceMana",
	PKReplaceOnly:                      "ReplaceOnly",
	PKReplaceType:                      "ReplaceType",
	PKReplacements:                     "Replacements",
	PKRestrictFromValid:                "RestrictFromValid",
	PKRestrictFromZone:                 "RestrictFromZone",
	PKRestrictToRemembered:             "RestrictToRemembered",
	PKResultSVar:                       "ResultSVar",
	PKResultSubAbilities:               "ResultSubAbilities",
	PKRevealAllValid:                   "RevealAllValid",
	PKRevealDefined:                    "RevealDefined",
	PKRevealOptional:                   "RevealOptional",
	PKRevealType:                       "RevealType",
	PKRevealValid:                      "RevealValid",
	PKRoomName:                         "RoomName",
	PKSVar:                             "SVar",
	PKSacValid:                         "SacValid",
	PKSeparator:                        "Separator",
	PKSetLoyalty:                       "SetLoyalty",
	PKShieldEffectTarget:               "ShieldEffectTarget",
	PKShowMilledCards:                  "ShowMilledCards",
	PKShowSacrificedCards:              "ShowSacrificedCards",
	PKSkipUntap:                        "SkipUntap",
	PKSource:                           "Source",
	PKStartingWith:                     "StartingWith",
	PKStaticCommandCheckSVar:           "StaticCommandCheckSVar",
	PKStaticCommandSVarCompare:         "StaticCommandSVarCompare",
	PKStaticEffect:                     "StaticEffect",
	PKStaticEffectCheckSVar:            "StaticEffectCheckSVar",
	PKStaticEffectSVarCompare:          "StaticEffectSVarCompare",
	PKStrictAmount:                     "StrictAmount",
	PKTailsSubAbility:                  "TailsSubAbility",
	PKTapper:                           "Tapper",
	PKTapperController:                 "TapperController",
	PKTargetRestriction:                "TargetRestriction",
	PKToEachOther:                      "ToEachOther",
	PKToughness:                        "Toughness",
	PKTrueSubAbility:                   "TrueSubAbility",
	PKTwoColors:                        "TwoColors",
	PKUnchosenPile:                     "UnchosenPile",
	PKUnlessResolveSubs:                "UnlessResolveSubs",
	PKUnlessType:                       "UnlessType",
	PKUnmatchedAbility:                 "UnmatchedAbility",
	PKUntap:                            "Untap",
	PKUntapType:                        "UntapType",
	PKUntapUpTo:                        "UntapUpTo",
	PKUseDifferenceBetweenRolls:        "UseDifferenceBetweenRolls",
	PKUseHighestRoll:                   "UseHighestRoll",
	PKValidCards2:                      "ValidCards2",
	PKValidDefined:                     "ValidDefined",
	PKValidPlayers:                     "ValidPlayers",
	PKValidTypes:                       "ValidTypes",
	PKVerb:                             "Verb",
	PKWinSubAbility:                    "WinSubAbility",
	PKWontPhaseInNormal:                "WontPhaseInNormal",
	PKsVars:                            "sVars",
	PKstaticAbilities:                  "staticAbilities",
	PKActivatorThisTurnCastEach:        "ActivatorThisTurnCastEach",
	PKActivePhases:                     "ActivePhases",
	PKAlone:                            "Alone",
	PKAlternateCost:                    "AlternateCost",
	PKAlternativeCost:                  "AlternativeCost",
	PKAnnounceTitle:                    "AnnounceTitle",
	PKAttacked:                         "Attacked",
	PKBeginTurn:                        "BeginTurn",
	PKBlessing:                         "Blessing",
	PKCheckDefinedPlayer:               "CheckDefinedPlayer",
	PKCheckOnTriggeredCard:             "CheckOnTriggeredCard",
	PKClassLevel:                       "ClassLevel",
	PKColor:                            "Color",
	PKControlOpponentsSearchingLibrary: "ControlOpponentsSearchingLibrary",
	PKCounterAmount:                    "CounterAmount",
	PKDamageAmount:                     "DamageAmount",
	PKDelirium:                         "Delirium",
	PKDescription:                      "Description",
	PKDethrone:                         "Dethrone",
	PKDrawLimit:                        "DrawLimit",
	PKEcho:                             "Echo",
	PKEnduringStory:                    "EnduringStory",
	PKExtraTurn:                        "ExtraTurn",
	PKFirstAttack:                      "FirstAttack",
	PKFirstCardInDrawStep:              "FirstCardInDrawStep",
	PKFirstCombat:                      "FirstCombat",
	PKFirstExtraCardDrawnThisTurn:      "FirstExtraCardDrawnThisTurn",
	PKForCost:                          "ForCost",
	PKForEachShard:                     "ForEachShard",
	PKHellbent:                         "Hellbent",
	PKIgnoreGeneric:                    "IgnoreGeneric",
	PKIncrement:                        "Increment",
	PKIsCombat:                         "IsCombat",
	PKIsDamage:                         "IsDamage",
	PKIsSingleTarget:                   "IsSingleTarget",
	PKKeywordLine:                      "KeywordLine",
	PKLifeTotal:                        "LifeTotal",
	PKList:                             "List",
	PKManaAmount:                       "ManaAmount",
	PKManaConversion:                   "ManaConversion",
	PKMaxAttackers:                     "MaxAttackers",
	PKMayPlayLimit:                     "MayPlayLimit",
	PKMayPlayText:                      "MayPlayText",
	PKMetalcraft:                       "Metalcraft",
	PKMinLimit:                         "MinLimit",
	PKMinMana:                          "MinMana",
	PKModeCost:                         "ModeCost",
	PKMustAttack:                       "MustAttack",
	PKMyriad:                           "Myriad",
	PKNatural:                          "Natural",
	PKNewCounterAmount:                 "NewCounterAmount",
	PKNoResolvingCheck:                 "NoResolvingCheck",
	PKNotFirstCardInDrawStep:           "NotFirstCardInDrawStep",
	PKNum:                              "Num",
	PKOneOff:                           "OneOff",
	PKOrderDuplicates:                  "OrderDuplicates",
	PKPhaseCount:                       "PhaseCount",
	PKPhases:                           "Phases",
	PKPlaneswalker:                     "Planeswalker",
	PKPlayer:                           "Player",
	PKPreventionShield:                 "PreventionShield",
	PKRaiseTo:                          "RaiseTo",
	PKReduceAmount:                     "ReduceAmount",
	PKRememberCostMana:                 "RememberCostMana",
	PKRememberingAttacker:              "RememberingAttacker",
	PKReplaceWith:                      "ReplaceWith",
	PKResolvedLimit:                    "ResolvedLimit",
	PKResolvedOnly:                     "ResolvedOnly",
	PKResult:                           "Result",
	PKRolledToVisitAttractions:         "RolledToVisitAttractions",
	PKSetMaxHandSize:                   "SetMaxHandSize",
	PKSkip:                             "Skip",
	PKStackDescription:                 "StackDescription",
	PKTargetsValid:                     "TargetsValid",
	PKThisDoor:                         "ThisDoor",
	PKThreshold:                        "Threshold",
	PKToBottom:                         "ToBottom",
	PKTraining:                         "Training",
	PKTrigger:                          "Trigger",
	PKTriggerController:                "TriggerController",
	PKUltimate:                         "Ultimate",
	PKUnlessDefender:                   "UnlessDefender",
	PKUnlessValidTarget:                "UnlessValidTarget",
	PKValidAttachment:                  "ValidAttachment",
	PKValidAttacked:                    "ValidAttacked",
	PKValidAttacker:                    "ValidAttacker",
	PKValidAttackingPlayer:             "ValidAttackingPlayer",
	PKValidBlocked:                     "ValidBlocked",
	PKValidCounterType:                 "ValidCounterType",
	PKValidDefenders:                   "ValidDefenders",
	PKValidEnlisted:                    "ValidEnlisted",
	PKValidCrew:                        "ValidCrew",
	PKValidSaddled:                     "ValidSaddled",
	PKValidExplored:                    "ValidExplored",
	PKValidExplorer:                    "ValidExplorer",
	PKValidLoseReason:                  "ValidLoseReason",
	PKValidNewController:               "ValidNewController",
	PKValidObjectToSource:              "ValidObjectToSource",
	PKValidOriginalController:          "ValidOriginalController",
	PKValidResult:                      "ValidResult",
	PKValidSides:                       "ValidSides",
	PKValidStepTurnToController:        "ValidStepTurnToController",
	PKValidTrigger:                     "ValidTrigger",
	PKValidTurned:                      "ValidTurned",
	PKValue:                            "Value",
	PKWon:                              "Won",
	PKXMax:                             "XMax",
	PKXMin:                             "XMin",
	PKAddsCounters:                     "AddsCounters",
	PKAddsNoCounter:                    "AddsNoCounter",
	PKAlternativeDecider:               "AlternativeDecider",
	PKAmountFromVotes:                  "AmountFromVotes",
	PKAtEOTTrig:                        "AtEOTTrig",
	PKAttachedToPlayer:                 "AttachedToPlayer",
	PKBolster:                          "Bolster",
	PKCanRepeatModes:                   "CanRepeatModes",
	PKChangeZoneTable:                  "ChangeZoneTable",
	PKCharmNum:                         "CharmNum",
	PKChoiceRestriction:                "ChoiceRestriction",
	PKChooseDifferent:                  "ChooseDifferent",
	PKClearNotedCardsFor:               "ClearNotedCardsFor",
	PKClearRememberedBeforeLoop:        "ClearRememberedBeforeLoop",
	PKColorOrType:                      "ColorOrType",
	PKCounterNumPerDefined:             "CounterNumPerDefined",
	PKCounterTypePerDefined:            "CounterTypePerDefined",
	PKDamageMap:                        "DamageMap",
	PKDamageSource:                     "DamageSource",
	PKDefinedName:                      "DefinedName",
	PKDestAltSVarCompare:               "DestAltSVarCompare",
	PKEachFromSource:                   "EachFromSource",
	PKEffectOwner:                      "EffectOwner",
	PKExactly:                          "Exactly",
	PKExcessSVar:                       "ExcessSVar",
	PKExcessSVarCondition:              "ExcessSVarCondition",
	PKExileFaceDown:                    "ExileFaceDown",
	PKExileOnMoved:                     "ExileOnMoved",
	PKFaceDownPower:                    "FaceDownPower",
	PKFaceDownToughness:                "FaceDownToughness",
	PKFallbackAbility:                  "FallbackAbility",
	PKForetold:                         "Foretold",
	PKForgetCounter:                    "ForgetCounter",
	PKForgetImprinted:                  "ForgetImprinted",
	PKForgetOnCast:                     "ForgetOnCast",
	PKForgetOnMoved:                    "ForgetOnMoved",
	PKForgetOnPhasedIn:                 "ForgetOnPhasedIn",
	PKHidden:                           "Hidden",
	PKImprintOnHost:                    "ImprintOnHost",
	PKImprintTokens:                    "ImprintTokens",
	PKKWChoice:                         "KWChoice",
	PKLibraryPositionAlternative:       "LibraryPositionAlternative",
	PKMaxRevealed:                      "MaxRevealed",
	PKMaxTotalTargetPower:              "MaxTotalTargetPower",
	PKMinCharmNum:                      "MinCharmNum",
	PKNextTurn:                         "NextTurn",
	PKNoteCards:                        "NoteCards",
	PKNoteCardsFor:                     "NoteCardsFor",
	PKNoteNumber:                       "NoteNumber",
	PKNumAtt:                           "NumAtt",
	PKNumDef:                           "NumDef",
	PKOriginAlternative:                "OriginAlternative",
	PKPawprint:                         "Pawprint",
	PKPersistentMana:                   "PersistentMana",
	PKPersistentUntilEndOfCombat:       "PersistentUntilEndOfCombat",
	PKPopulate:                         "Populate",
	PKRandomCompare:                    "RandomCompare",
	PKRandomCompareSVar:                "RandomCompareSVar",
	PKRandomCopied:                     "RandomCopied",
	PKRandomNum:                        "RandomNum",
	PKRandomOrder:                      "RandomOrder",
	PKRandomType:                       "RandomType",
	PKReflectProperty:                  "ReflectProperty",
	PKRelativeTarget:                   "RelativeTarget",
	PKRememberAttached:                 "RememberAttached",
	PKRememberCards:                    "RememberCards",
	PKRememberChain:                    "RememberChain",
	PKRememberDrawn:                    "RememberDrawn",
	PKRememberOriginalTokens:           "RememberOriginalTokens",
	PKRememberSearched:                 "RememberSearched",
	PKRememberTokens:                   "RememberTokens",
	PKRememberVotedObjects:             "RememberVotedObjects",
	PKRenown:                           "Renown",
	PKReorder:                          "Reorder",
	PKRepeatCards:                      "RepeatCards",
	PKRepeatOptionalForEachPlayer:      "RepeatOptionalForEachPlayer",
	PKRepeatOptionalMessage:            "RepeatOptionalMessage",
	PKRepeatPlayers:                    "RepeatPlayers",
	PKRepeatSpellAbilities:             "RepeatSpellAbilities",
	PKRepeatTargeted:                   "RepeatTargeted",
	PKRepeatTypesFrom:                  "RepeatTypesFrom",
	PKReplaceDyingDefined:              "ReplaceDyingDefined",
	PKReplacementEffects:               "ReplacementEffects",
	PKSetChosenNumber:                  "SetChosenNumber",
	PKShareLandType:                    "ShareLandType",
	PKShuffleNonMandatory:              "ShuffleNonMandatory",
	PKStackable:                        "Stackable",
	PKSupport:                          "Support",
	PKTargetsWithDifferentNames:        "TargetsWithDifferentNames",
	PKTargetsWithSharedTypes:           "TargetsWithSharedTypes",
	PKTempRemember:                     "TempRemember",
	PKTokenAttacking:                   "TokenAttacking",
	PKTokenOwner:                       "TokenOwner",
	PKTokenRemembered:                  "TokenRemembered",
	PKTokenTapped:                      "TokenTapped",
	PKTransformed:                      "Transformed",
	PKTriggersWhenSpent:                "TriggersWhenSpent",
	PKUnearth:                          "Unearth",
	PKUnimprint:                        "Unimprint",
	PKUpto:                             "Upto",
	PKUseAllOriginZones:                "UseAllOriginZones",
	PKValidSupportedCopy:               "ValidSupportedCopy",
	PKVoteCard:                         "VoteCard",
	PKVoteMessage:                      "VoteMessage",
	PKVotePlayer:                       "VotePlayer",
	PKVoteSubAbility:                   "VoteSubAbility",
	PKVoteTiedAbility:                  "VoteTiedAbility",
	PKWithDifferentNames:               "WithDifferentNames",
	PKWithMayLook:                      "WithMayLook",
	PKWithTotalCMC:                     "WithTotalCMC",
	PKWithTotalCardTypes:               "WithTotalCardTypes",
	PKXChoice:                          "XChoice",
	PKTgtPrompt2:                       "TgtPrompt2",
	PKSorcerySpeed2:                    "SorcerySpeed2",
	PKChoose:                           "Choose",
	PKDefinedPlayerChooses:             "DefinedPlayerChooses",
	PKChoiceNum:                        "ChoiceNum",
	PKChooseCounter:                    "ChooseCounter",
	PKCounterTypeChoice:                "CounterTypeChoice",
	PKPromptToSkipOptionalAbility:      "PromptToSkipOptionalAbility",
	PKOptionalAbilityPrompt:            "OptionalAbilityPrompt",
	PKSetChosenMode:                    "SetChosenMode",
	PKWithoutManaCost:                  "WithoutManaCost",
	PKPlayCost:                         "PlayCost",
	PKReplaceGraveyard:                 "ReplaceGraveyard",
	PKReplaceGraveyardValid:            "ReplaceGraveyardValid",
	PKImprintPlayed:                    "ImprintPlayed",
	PKShowCards:                        "ShowCards",
	PKLevel:                            "Level",
	PKTapCreaturesForMana:              "TapCreaturesForMana",
	PKDigNum:                           "DigNum",
	PKChangeValid:                      "ChangeValid",
	PKDestinationZone:                  "DestinationZone",
	PKDestinationZone2:                 "DestinationZone2",
	PKChosenZone:                       "ChosenZone",
	PKRestRandomOrder:                  "RestRandomOrder",
	PKImprintRest:                      "ImprintRest",
	PKChangeLater:                      "ChangeLater",
	PKChooseAmount:                     "ChooseAmount",
	PKPrimary:                          "Primary",
	PKSecondaryType:                    "SecondaryType",
}

// String is the key's Forge text.
func (k ParamKey) String() string { return paramKeyNames[k] }

// paramKeyByName is the load-time inverse of paramKeyNames (never read on a
// hot path: newParamSet runs once per node at load).
var paramKeyByName = func() map[string]ParamKey {
	m := make(map[string]ParamKey, paramKeyCount)
	for k := ParamKey(1); k < paramKeyCount; k++ {
		m[paramKeyNames[k]] = k
	}
	return m
}()

// ParamMask is a set of ParamKeys.
type ParamMask [paramMaskWords]uint64

// ParamMaskOf builds a mask of keys (package-init constant tables).
func ParamMaskOf(keys ...ParamKey) ParamMask {
	var m ParamMask
	for _, k := range keys {
		m[k>>6] |= 1 << (k & 63)
	}
	return m
}

// ParamSet is one Params map compiled against the ParamKey vocabulary: the
// presence mask of the vocabulary keys it holds and their values in key
// order. It is immutable once built and answers only for the exact map it
// was built from (src, n): a node copy whose Params was replaced or resized
// falls back to the map read.
//
// A read is a mask test and a rank: rank[w] is the number of keys present in
// the words below w (a per-word popcount prefix computed once at build), so
// a key's value index is rank[w] plus the popcount of its own word below its
// bit, whichever word it lives in. No loop, no map, no allocation: a range test
// (never taken for a declared key) and the bit test.
type ParamSet struct {
	// src is the map the set was built from. Held as the map itself (not
	// its address) so two identically parsed nodes stay reflect.DeepEqual;
	// bound compares identities.
	src  map[string]string
	n    int
	has  ParamMask
	rank [paramMaskWords]uint16
	vals []string
	// codes is each value's ParamCoder code, parallel to vals (nil when no
	// key the set holds has a coder).
	codes []uint16
}

func mapIdentity(m map[string]string) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&m))
}

func newParamSet(m map[string]string) *ParamSet {
	if m == nil {
		return nil
	}
	ps := &ParamSet{src: m, n: len(m)}
	for key := range m {
		if k, ok := paramKeyByName[key]; ok {
			ps.has[k>>6] |= 1 << (k & 63)
		}
	}
	n := 0
	for w, h := range ps.has {
		ps.rank[w] = uint16(n) // n < paramKeyCap: a uint16 holds any count
		n += bits.OnesCount64(h)
	}
	ps.vals = make([]string, 0, n)
	for w, h := range ps.has {
		for ; h != 0; h &= h - 1 {
			k := ParamKey(w<<6 | bits.TrailingZeros64(h))
			ps.vals = append(ps.vals, m[paramKeyNames[k]])
		}
	}
	paramCodersSealed.Store(true)
	for w, h := range ps.has {
		for i := int(ps.rank[w]); h != 0; h &= h - 1 {
			k := ParamKey(w<<6 | bits.TrailingZeros64(h))
			if c := paramCoders[k]; c != nil {
				if ps.codes == nil {
					ps.codes = make([]uint16, n)
				}
				ps.codes[i] = c(ps.vals[i])
			}
			i++
		}
	}
	return ps
}

// ParamCoder classifies one parameter value into a downstream package's
// dense code (a zone word, a condition keyword, ...). It must be a pure
// function of the text.
type ParamCoder func(value string) uint16

// paramCoders is the registered coder per key and paramCoderOwners the
// registering vocabulary's name; paramCodersSealed is set by the first
// ParamSet built, after which a registration would leave earlier sets
// without their codes.
var (
	paramCoders       [paramKeyCount]ParamCoder
	paramCoderOwners  [paramKeyCount]string
	paramCodersSealed atomic.Bool
)

// RegisterParamCoder makes every ParamSet built from now on store key k's
// value classified by f, so a hot read takes the stored code (ParamCode)
// instead of re-classifying the text on every call. owner names the
// vocabulary ("effects.ZoneList"); a second registration for k is allowed
// only under the same owner -- a test binary can link two copies of one
// package, each running its init. Package init only: it panics once a set
// has been built.
func RegisterParamCoder(k ParamKey, owner string, f ParamCoder) {
	if paramCodersSealed.Load() {
		panic("cards: RegisterParamCoder after a ParamSet was built: " + paramKeyNames[k])
	}
	if paramCoders[k] != nil && paramCoderOwners[k] != owner {
		panic("cards: ParamCoder for " + paramKeyNames[k] + " registered by " + paramCoderOwners[k] + " and " + owner)
	}
	paramCoders[k], paramCoderOwners[k] = f, owner
}

func (ps *ParamSet) index(k ParamKey) (int, bool) {
	w := int(k) >> 6
	if w >= paramMaskWords {
		return 0, false
	}
	h, b := ps.has[w], uint64(1)<<(k&63)
	if h&b == 0 {
		return 0, false
	}
	return int(ps.rank[w]) + bits.OnesCount64(h&(b-1)), true
}

// paramCode is key k's value code: the code stored at load when ps is bound
// to m, else the registered coder applied to m's value. ok reports the key's
// presence; a key with no coder answers 0.
func paramCode(ps *ParamSet, m map[string]string, k ParamKey) (uint16, bool) {
	if ps.bound(m) {
		i, ok := ps.index(k)
		if !ok {
			return 0, false
		}
		if ps.codes != nil {
			return ps.codes[i], true
		}
		if c := paramCoders[k]; c != nil {
			return c(ps.vals[i]), true
		}
		return 0, true
	}
	v, ok := m[paramKeyNames[k]]
	if !ok {
		return 0, false
	}
	if c := paramCoders[k]; c != nil {
		return c(v), true
	}
	return 0, true
}

// SameParamMap reports whether a and b are the same Params map instance: the
// identity rule a compiled view of a node's parameters -- a ParamSet here, a
// downstream typed parameter struct elsewhere -- uses to answer only for the
// exact map it was compiled from (with the map's size at compile time, which
// the caller keeps, catching an in-place write).
func SameParamMap(a, b map[string]string) bool {
	return mapIdentity(a) == mapIdentity(b)
}

// ParamNames returns the ability's parameter keys, sorted (a fresh slice;
// load-time compilers only -- a downstream unread-parameter check walks it
// instead of ranging the map).
func (sa *SA) ParamNames() []string {
	out := make([]string, 0, len(sa.Params))
	for k := range sa.Params {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// ParamMapIdentity is m's identity as an address, for a downstream cache
// keyed by the map instance (compare entries with SameParamMap).
func ParamMapIdentity(m map[string]string) uintptr {
	return uintptr(mapIdentity(m))
}

func (ps *ParamSet) bound(m map[string]string) bool {
	return ps != nil && ps.n == len(m) && mapIdentity(ps.src) == mapIdentity(m)
}

func (ps *ParamSet) get(k ParamKey) (string, bool) {
	w := int(k) >> 6
	if w >= paramMaskWords {
		// Outside the vocabulary: absent, as an unset bit would be. The
		// test also lets the compiler drop the has/rank bounds checks.
		return "", false
	}
	h, b := ps.has[w], uint64(1)<<(k&63)
	if h&b == 0 {
		return "", false
	}
	return ps.vals[int(ps.rank[w])+bits.OnesCount64(h&(b-1))], true
}

func paramGet(ps *ParamSet, m map[string]string, k ParamKey) (string, bool) {
	if ps.bound(m) {
		return ps.get(k)
	}
	v, ok := m[paramKeyNames[k]]
	return v, ok
}

// paramMayHaveAny reports whether m may hold any key of mask: exact when ps
// is bound to m, else conservatively true.
func paramMayHaveAny(ps *ParamSet, m map[string]string, mask ParamMask) bool {
	if ps.bound(m) {
		var x uint64
		for w := range mask {
			x |= ps.has[w] & mask[w]
		}
		return x != 0
	}
	return true
}

// Param is Params[k] with presence, through the compiled set when bound.
func (s Static) Param(k ParamKey) (string, bool) { return paramGet(s.ps, s.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (s Static) ParamStr(k ParamKey) string { v, _ := paramGet(s.ps, s.Params, k); return v }

// ParamCode is key k's value as its registered ParamCoder's code, stored at
// load (RegisterParamCoder); ok reports the key's presence.
func (s Static) ParamCode(k ParamKey) (uint16, bool) { return paramCode(s.ps, s.Params, k) }

// HasParam reports whether key k is present.
func (s Static) HasParam(k ParamKey) bool { _, ok := paramGet(s.ps, s.Params, k); return ok }

// MayHaveAnyParam is false only when the static provably holds no key of mask.
func (s Static) MayHaveAnyParam(mask ParamMask) bool { return paramMayHaveAny(s.ps, s.Params, mask) }

// ParamSetOf is the static's compiled parameter set, for a view that carries
// the Params map onward (nil when unbound).
func (s Static) ParamSetOf() *ParamSet { return s.ps }

// Param is Params[k] with presence, through the compiled set when bound.
func (t Trigger) Param(k ParamKey) (string, bool) { return paramGet(t.ps, t.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (t Trigger) ParamStr(k ParamKey) string { v, _ := paramGet(t.ps, t.Params, k); return v }

// ParamCode is Static.ParamCode for a trigger line.
func (t Trigger) ParamCode(k ParamKey) (uint16, bool) { return paramCode(t.ps, t.Params, k) }

// HasParam reports whether key k is present.
func (t Trigger) HasParam(k ParamKey) bool { _, ok := paramGet(t.ps, t.Params, k); return ok }

// Param is Params[k] with presence, through the compiled set when bound.
func (r Repl) Param(k ParamKey) (string, bool) { return paramGet(r.ps, r.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (r Repl) ParamStr(k ParamKey) string { v, _ := paramGet(r.ps, r.Params, k); return v }

// ParamCode is Static.ParamCode for a replacement line.
func (r Repl) ParamCode(k ParamKey) (uint16, bool) { return paramCode(r.ps, r.Params, k) }

// HasParam reports whether key k is present.
func (r Repl) HasParam(k ParamKey) bool { _, ok := paramGet(r.ps, r.Params, k); return ok }

// OptionalValue reports the compiled Optional$ True replacement election flag.
// Parsed faces bind it once; an unbound synthetic Repl reads its raw parameter
// exactly, matching the script's canonical spelling.
func (r Repl) OptionalValue() bool {
	if r.optionalBound {
		return r.optional
	}
	v, ok := r.Param(PKOptional)
	return ok && v == "True"
}

// ParamSetParam reads key k of m through ps (a view carrying a node's Params
// map and its ParamSet side by side).
func ParamSetParam(ps *ParamSet, m map[string]string, k ParamKey) (string, bool) {
	return paramGet(ps, m, k)
}

// ParamSetMayHaveAny is MayHaveAnyParam for a view's (ps, map) pair.
// ParamSetCode is ParamCode through a caller-held set (a view that keeps a
// node's ParamSet and Params apart).
func ParamSetCode(ps *ParamSet, m map[string]string, k ParamKey) (uint16, bool) {
	return paramCode(ps, m, k)
}

func ParamSetMayHaveAny(ps *ParamSet, m map[string]string, mask ParamMask) bool {
	return paramMayHaveAny(ps, m, mask)
}

// Param is Params[k] with presence, through the compiled set when bound.
func (sa *SA) Param(k ParamKey) (string, bool) { return paramGet(sa.ps, sa.Params, k) }

// ParamStr is Params[k] ("" when absent).
func (sa *SA) ParamStr(k ParamKey) string { v, _ := paramGet(sa.ps, sa.Params, k); return v }

// ParamCode is Static.ParamCode for an ability.
func (sa *SA) ParamCode(k ParamKey) (uint16, bool) { return paramCode(sa.ps, sa.Params, k) }

// HasParam reports whether key k is present.
func (sa *SA) HasParam(k ParamKey) bool { _, ok := paramGet(sa.ps, sa.Params, k); return ok }

// MayHaveAnyParam is false only when the ability provably holds no key of
// mask.
func (sa *SA) MayHaveAnyParam(mask ParamMask) bool { return paramMayHaveAny(sa.ps, sa.Params, mask) }

// SetParam writes key k into the ability's own Params map (allocating it
// when nil). The compiled set no longer describes the map, so reads go to
// the map from here on. Write only to a node the caller owns -- a copy or a
// node it built -- never to a shared parsed node.
func (sa *SA) SetParam(k ParamKey, v string) {
	if sa.Params == nil {
		sa.Params = map[string]string{}
	}
	sa.Params[paramKeyNames[k]] = v
	sa.ps = nil
}

// SetParam is SA.SetParam for a replacement line.
func (r *Repl) SetParam(k ParamKey, v string) {
	if r.Params == nil {
		r.Params = map[string]string{}
	}
	r.Params[paramKeyNames[k]] = v
	r.ps = nil
}

// deriveParamSets binds each printed static's, trigger's and ability's
// ParamSet (every ability reachable from the face: its Abilities, their
// SubAbility$ chains, trigger Execute$ bodies and replacement bodies). It
// runs at load (derive, and again at the end of link, which re-resolves
// bodies); a node built later stays unbound and reads its map.
func (f *Face) deriveParamSets() {
	for i := range f.Statics {
		st := &f.Statics[i]
		st.ps = newParamSet(st.Params)
		st.mode, st.modeBound = StaticModeOf(st.Mode), true
	}
	for i := range f.Triggers {
		t := &f.Triggers[i]
		t.ps = newParamSet(t.Params)
		t.mode, t.modeBound = TriggerModeOf(t.Mode), true
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		r.ps = newParamSet(r.Params)
		r.event, r.eventBound = ReplEventOf(r.Event), true
		r.optional = strings.EqualFold(strings.TrimSpace(r.Params[paramKeyNames[PKOptional]]), "True")
		r.optionalBound = true
	}
	bindSA := func(sa *SA) {
		for d := 0; sa != nil && d <= maxSVarDepth+1; d++ {
			sa.ps = newParamSet(sa.Params)
			sa.api, sa.apiBound = APICodeForName(sa.API), true
			if sa.extSlot == nil {
				sa.extSlot = &ExtSlot{}
			}
			sa = sa.Sub
		}
	}
	for _, a := range f.Abilities {
		bindSA(a)
	}
	for i := range f.Triggers {
		bindSA(f.Triggers[i].Effect)
	}
	for i := range f.Repls {
		bindSA(f.Repls[i].With)
	}
}
