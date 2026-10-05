package cards

// The line-head vocabularies: a trigger's Mode$, a static's Mode$ and a
// replacement's Event$, each ONE dense code set shared by the engine's
// dispatch and the compiled catalog's rows (compiled_catalog.go).
// deriveParamSets resolves each printed node's name once at load (ModeKind /
// EventKind read the stored code); a node built later resolves on read. A
// name outside the vocabulary is code 0 and matches no case.
//
// Values are explicit and APPEND-ONLY: the catalog persists them in its rows
// (CompiledCatalogSchema), so a new name takes the next value and an
// assigned value never moves. The first values are the catalog's historical
// assignment; the dispatch-only names follow. A few names (the static modes
// CantBlock, Panharmonicon, AttackRestrict, UntapOtherPlayer and the
// replacement event LifeReduced) are catalogued but dispatched on nowhere:
// their codes match no engine case, exactly as an unknown name does.

// TriggerMode is a T: line's Mode$ as a dense code.
type TriggerMode uint16

// The vocabulary. 0 is a name outside it.
const (
	TriggerChangesZone                TriggerMode = 1
	TriggerSpellCast                  TriggerMode = 2
	TriggerAbilityCast                TriggerMode = 3
	TriggerSpellAbilityCast           TriggerMode = 4
	TriggerAttacks                    TriggerMode = 5
	TriggerAttackersDeclaredOneTarget TriggerMode = 6
	TriggerAttackersDeclared          TriggerMode = 7
	TriggerAttackerBlocked            TriggerMode = 8
	TriggerSacrificed                 TriggerMode = 9
	TriggerDiscarded                  TriggerMode = 10
	TriggerLandPlayed                 TriggerMode = 11
	TriggerCycled                     TriggerMode = 12
	TriggerCommitCrime                TriggerMode = 13
	TriggerBecomesTarget              TriggerMode = 14
	TriggerTaps                       TriggerMode = 15
	TriggerTapsForMana                TriggerMode = 16
	TriggerDamageDone                 TriggerMode = 17
	TriggerDamageDealtOnce            TriggerMode = 18
	TriggerDamageDoneOnce             TriggerMode = 19
	TriggerCounterAdded               TriggerMode = 20
	TriggerDrawn                      TriggerMode = 21
	TriggerLifeLost                   TriggerMode = 22
	TriggerLifeLostAll                TriggerMode = 23
	TriggerPhase                      TriggerMode = 24
	TriggerAlways                     TriggerMode = 25
	TriggerAttached                   TriggerMode = 26
	TriggerAttackerBlockedByCreature  TriggerMode = 27
	TriggerAttackerUnblocked          TriggerMode = 28
	TriggerAttackerUnblockedOnce      TriggerMode = 29
	TriggerBecomeMonarch              TriggerMode = 30
	TriggerBecomeMonstrous            TriggerMode = 31
	TriggerBecomesTargetOnce          TriggerMode = 32
	TriggerBlocks                     TriggerMode = 33
	TriggerChangesController          TriggerMode = 34
	TriggerChangesZoneAll             TriggerMode = 35
	TriggerChaosEnsues                TriggerMode = 36
	TriggerClashed                    TriggerMode = 37
	TriggerClassLevelGained           TriggerMode = 38
	TriggerConnives                   TriggerMode = 39
	TriggerCounterAddedOnce           TriggerMode = 40
	TriggerCounterPlayerAddedAll      TriggerMode = 41
	TriggerCounterRemoved             TriggerMode = 42
	TriggerCounterRemovedOnce         TriggerMode = 43
	TriggerDamageAll                  TriggerMode = 44
	TriggerDamagePreventedOnce        TriggerMode = 45
	TriggerDiscardedAll               TriggerMode = 46
	TriggerDiscover                   TriggerMode = 47
	TriggerElementalBend              TriggerMode = 48
	TriggerEnlisted                   TriggerMode = 49
	TriggerEvolved                    TriggerMode = 50
	TriggerExerted                    TriggerMode = 51
	TriggerExploited                  TriggerMode = 52
	TriggerExplores                   TriggerMode = 53
	TriggerFlippedCoin                TriggerMode = 54
	TriggerForetell                   TriggerMode = 55
	TriggerFullyUnlock                TriggerMode = 56
	TriggerGiveGift                   TriggerMode = 57
	TriggerInvestigated               TriggerMode = 58
	TriggerLifeGained                 TriggerMode = 59
	TriggerManaExpend                 TriggerMode = 60
	TriggerMilled                     TriggerMode = 61
	TriggerMilledAll                  TriggerMode = 62
	TriggerMutates                    TriggerMode = 63
	TriggerPhaseOutAll                TriggerMode = 64
	TriggerPlaneswalkedFrom           TriggerMode = 65
	TriggerPlaneswalkedTo             TriggerMode = 66
	TriggerProliferate                TriggerMode = 67
	TriggerRingTemptsYou              TriggerMode = 68
	TriggerRolledDie                  TriggerMode = 69
	TriggerRolledDieOnce              TriggerMode = 70
	TriggerScry                       TriggerMode = 71
	TriggerSearchedLibrary            TriggerMode = 72
	TriggerSeekAll                    TriggerMode = 73
	TriggerSpecializes                TriggerMode = 74
	TriggerSpellCastOrCopy            TriggerMode = 75
	TriggerSpellCopy                  TriggerMode = 76
	TriggerSurveil                    TriggerMode = 77
	TriggerTokenCreated               TriggerMode = 78
	TriggerTokenCreatedOnce           TriggerMode = 79
	TriggerTransformed                TriggerMode = 80
	TriggerTurnFaceUp                 TriggerMode = 81
	TriggerUnattached                 TriggerMode = 82
	TriggerUntaps                     TriggerMode = 83
	TriggerVote                       TriggerMode = 84
	// set-mechanic / keyword-action trigger modes (task
	// triage-478c51d1): the trigger modes Standard's set mechanics and
	// keyword actions print. Appended after TriggerVote, following the
	// vocabulary's append-only convention, so no earlier ordinal moves.
	TriggerCrewed         TriggerMode = 85
	TriggerSaddled        TriggerMode = 86
	TriggerBecomesSaddled TriggerMode = 87
	TriggerBecomesPlotted TriggerMode = 88
	TriggerSacrificedOnce TriggerMode = 89
	// Aggregate tap trigger modes (task cli-20261005T075020Z-05241a06):
	// Forge's batch siblings of Taps/Untaps, "whenever one or more ...
	// become tapped/untapped". They are the "one or more" readings of the
	// Tap/Untap events (the millBatch/discardBatch cadence), not new event
	// kinds, so they append here rather than being aliased onto
	// TriggerTaps/TriggerUntaps. Appended after TriggerSacrificedOnce,
	// following the vocabulary's append-only convention, so no earlier
	// ordinal moves.
	TriggerTapAll    TriggerMode = 90
	TriggerUntapAll  TriggerMode = 91
	TriggerExiled    TriggerMode = 92
	TriggerLosesGame TriggerMode = 93
	TriggerTurnBegin TriggerMode = 94
	TriggerModeCount             = 95 // one past the last; sizes a dense per-TriggerMode array
)

var triggerModeNames = [TriggerModeCount]string{
	TriggerChangesZone:                "ChangesZone",
	TriggerSpellCast:                  "SpellCast",
	TriggerAbilityCast:                "AbilityCast",
	TriggerSpellAbilityCast:           "SpellAbilityCast",
	TriggerAttacks:                    "Attacks",
	TriggerAttackersDeclaredOneTarget: "AttackersDeclaredOneTarget",
	TriggerAttackersDeclared:          "AttackersDeclared",
	TriggerAttackerBlocked:            "AttackerBlocked",
	TriggerSacrificed:                 "Sacrificed",
	TriggerDiscarded:                  "Discarded",
	TriggerLandPlayed:                 "LandPlayed",
	TriggerCycled:                     "Cycled",
	TriggerCommitCrime:                "CommitCrime",
	TriggerBecomesTarget:              "BecomesTarget",
	TriggerTaps:                       "Taps",
	TriggerTapsForMana:                "TapsForMana",
	TriggerDamageDone:                 "DamageDone",
	TriggerDamageDealtOnce:            "DamageDealtOnce",
	TriggerDamageDoneOnce:             "DamageDoneOnce",
	TriggerCounterAdded:               "CounterAdded",
	TriggerDrawn:                      "Drawn",
	TriggerLifeLost:                   "LifeLost",
	TriggerLifeLostAll:                "LifeLostAll",
	TriggerPhase:                      "Phase",
	TriggerAlways:                     "Always",
	TriggerAttached:                   "Attached",
	TriggerAttackerBlockedByCreature:  "AttackerBlockedByCreature",
	TriggerAttackerUnblocked:          "AttackerUnblocked",
	TriggerAttackerUnblockedOnce:      "AttackerUnblockedOnce",
	TriggerBecomeMonarch:              "BecomeMonarch",
	TriggerBecomeMonstrous:            "BecomeMonstrous",
	TriggerBecomesTargetOnce:          "BecomesTargetOnce",
	TriggerBlocks:                     "Blocks",
	TriggerChangesController:          "ChangesController",
	TriggerChangesZoneAll:             "ChangesZoneAll",
	TriggerChaosEnsues:                "ChaosEnsues",
	TriggerClashed:                    "Clashed",
	TriggerClassLevelGained:           "ClassLevelGained",
	TriggerConnives:                   "Connives",
	TriggerCounterAddedOnce:           "CounterAddedOnce",
	TriggerCounterPlayerAddedAll:      "CounterPlayerAddedAll",
	TriggerCounterRemoved:             "CounterRemoved",
	TriggerCounterRemovedOnce:         "CounterRemovedOnce",
	TriggerDamageAll:                  "DamageAll",
	TriggerDamagePreventedOnce:        "DamagePreventedOnce",
	TriggerDiscardedAll:               "DiscardedAll",
	TriggerDiscover:                   "Discover",
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
	TriggerLifeGained:                 "LifeGained",
	TriggerManaExpend:                 "ManaExpend",
	TriggerMilled:                     "Milled",
	TriggerMilledAll:                  "MilledAll",
	TriggerMutates:                    "Mutates",
	TriggerPhaseOutAll:                "PhaseOutAll",
	TriggerPlaneswalkedFrom:           "PlaneswalkedFrom",
	TriggerPlaneswalkedTo:             "PlaneswalkedTo",
	TriggerProliferate:                "Proliferate",
	TriggerRingTemptsYou:              "RingTemptsYou",
	TriggerRolledDie:                  "RolledDie",
	TriggerRolledDieOnce:              "RolledDieOnce",
	TriggerScry:                       "Scry",
	TriggerSearchedLibrary:            "SearchedLibrary",
	TriggerSeekAll:                    "SeekAll",
	TriggerSpecializes:                "Specializes",
	TriggerSpellCastOrCopy:            "SpellCastOrCopy",
	TriggerSpellCopy:                  "SpellCopy",
	TriggerSurveil:                    "Surveil",
	TriggerTokenCreated:               "TokenCreated",
	TriggerTokenCreatedOnce:           "TokenCreatedOnce",
	TriggerTransformed:                "Transformed",
	TriggerTurnFaceUp:                 "TurnFaceUp",
	TriggerUnattached:                 "Unattached",
	TriggerUntaps:                     "Untaps",
	TriggerVote:                       "Vote",
	TriggerCrewed:                     "Crewed",
	TriggerSaddled:                    "Saddled",
	TriggerBecomesSaddled:             "BecomesSaddled",
	TriggerBecomesPlotted:             "BecomesPlotted",
	TriggerSacrificedOnce:             "SacrificedOnce",
	TriggerTapAll:                     "TapAll",
	TriggerUntapAll:                   "UntapAll",
	TriggerExiled:                     "Exiled",
	TriggerLosesGame:                  "LosesGame",
	TriggerTurnBegin:                  "TurnBegin",
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

// The vocabulary. 0 is a name outside it.
const (
	StaticContinuous            StaticMode = 1
	StaticCantBeCast            StaticMode = 2
	StaticCantBeActivated       StaticMode = 3
	StaticRaiseCost             StaticMode = 4
	StaticReduceCost            StaticMode = 5
	StaticSetCost               StaticMode = 6
	StaticAlternativeCost       StaticMode = 7
	StaticCastWithFlash         StaticMode = 8
	StaticCantBlock             StaticMode = 9
	StaticCantBlockBy           StaticMode = 10
	StaticPanharmonicon         StaticMode = 11
	StaticManaConvert           StaticMode = 12
	StaticMustAttack            StaticMode = 13
	StaticAttackRestrict        StaticMode = 14
	StaticNumLoyaltyAct         StaticMode = 15
	StaticCantGainLife          StaticMode = 16
	StaticCantPreventDamage     StaticMode = 17
	StaticUntapOtherPlayer      StaticMode = 18
	StaticOptionalCost          StaticMode = 19
	StaticCanAttackDefender     StaticMode = 20
	StaticCantAttack            StaticMode = 21
	StaticCantAttackUnless      StaticMode = 22
	StaticCantBlockUnless       StaticMode = 23
	StaticCantExile             StaticMode = 24
	StaticCantPutCounter        StaticMode = 25
	StaticCantRegenerate        StaticMode = 26
	StaticCantSacrifice         StaticMode = 27
	StaticCantTarget            StaticMode = 28
	StaticCombatDamageToughness StaticMode = 29
	StaticMustBlock             StaticMode = 30
	StaticUnspentMana           StaticMode = 31
	StaticCantPlayLand          StaticMode = 32
	StaticModeCount                        = 33 // one past the last; sizes a dense per-StaticMode array
)

var staticModeNames = [StaticModeCount]string{
	StaticContinuous:            "Continuous",
	StaticCantBeCast:            "CantBeCast",
	StaticCantBeActivated:       "CantBeActivated",
	StaticRaiseCost:             "RaiseCost",
	StaticReduceCost:            "ReduceCost",
	StaticSetCost:               "SetCost",
	StaticAlternativeCost:       "AlternativeCost",
	StaticCastWithFlash:         "CastWithFlash",
	StaticCantBlock:             "CantBlock",
	StaticCantBlockBy:           "CantBlockBy",
	StaticPanharmonicon:         "Panharmonicon",
	StaticManaConvert:           "ManaConvert",
	StaticMustAttack:            "MustAttack",
	StaticAttackRestrict:        "AttackRestrict",
	StaticNumLoyaltyAct:         "NumLoyaltyAct",
	StaticCantGainLife:          "CantGainLife",
	StaticCantPreventDamage:     "CantPreventDamage",
	StaticUntapOtherPlayer:      "UntapOtherPlayer",
	StaticOptionalCost:          "OptionalCost",
	StaticCanAttackDefender:     "CanAttackDefender",
	StaticCantAttack:            "CantAttack",
	StaticCantAttackUnless:      "CantAttackUnless",
	StaticCantBlockUnless:       "CantBlockUnless",
	StaticCantExile:             "CantExile",
	StaticCantPutCounter:        "CantPutCounter",
	StaticCantRegenerate:        "CantRegenerate",
	StaticCantSacrifice:         "CantSacrifice",
	StaticCantTarget:            "CantTarget",
	StaticCombatDamageToughness: "CombatDamageToughness",
	StaticMustBlock:             "MustBlock",
	StaticUnspentMana:           "UnspentMana",
	StaticCantPlayLand:          "CantPlayLand",
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

// The vocabulary. 0 is a name outside it.
const (
	ReplMoved          ReplEvent = 1
	ReplUntap          ReplEvent = 2
	ReplBeginPhase     ReplEvent = 3
	ReplTransform      ReplEvent = 4
	ReplProduceMana    ReplEvent = 5
	ReplGainLife       ReplEvent = 6
	ReplLifeReduced    ReplEvent = 7
	ReplDamageDone     ReplEvent = 8
	ReplCounter        ReplEvent = 9
	ReplDraw           ReplEvent = 10
	ReplAddCounter     ReplEvent = 11
	ReplAttached       ReplEvent = 12
	ReplBeginTurn      ReplEvent = 13
	ReplCascade        ReplEvent = 14
	ReplCreateToken    ReplEvent = 15
	ReplDrawCards      ReplEvent = 16
	ReplExplore        ReplEvent = 17
	ReplGameLoss       ReplEvent = 18
	ReplGameWin        ReplEvent = 19
	ReplRollDice       ReplEvent = 20
	ReplRollPlanarDice ReplEvent = 21
	ReplScry           ReplEvent = 22
	ReplTurnFaceUp     ReplEvent = 23
	ReplEventCount               = 24 // one past the last; sizes a dense per-ReplEvent array
)

var replEventNames = [ReplEventCount]string{
	ReplMoved:          "Moved",
	ReplUntap:          "Untap",
	ReplBeginPhase:     "BeginPhase",
	ReplTransform:      "Transform",
	ReplProduceMana:    "ProduceMana",
	ReplGainLife:       "GainLife",
	ReplLifeReduced:    "LifeReduced",
	ReplDamageDone:     "DamageDone",
	ReplCounter:        "Counter",
	ReplDraw:           "Draw",
	ReplAddCounter:     "AddCounter",
	ReplAttached:       "Attached",
	ReplBeginTurn:      "BeginTurn",
	ReplCascade:        "Cascade",
	ReplCreateToken:    "CreateToken",
	ReplDrawCards:      "DrawCards",
	ReplExplore:        "Explore",
	ReplGameLoss:       "GameLoss",
	ReplGameWin:        "GameWin",
	ReplRollDice:       "RollDice",
	ReplRollPlanarDice: "RollPlanarDice",
	ReplScry:           "Scry",
	ReplTurnFaceUp:     "TurnFaceUp",
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
