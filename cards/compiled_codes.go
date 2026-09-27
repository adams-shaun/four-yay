package cards

import "strings"

// Runtime catalog IDs are one-based. Zero is always unknown or unbound.
type (
	FaceID    uint32
	AbilityID uint32
	StringID  uint32
)

type SAKind uint8

const (
	SAKindUnknown   SAKind = 0
	SAKindSpell     SAKind = 1
	SAKindActivated SAKind = 2
	SAKindDrawback  SAKind = 3
	SAKindStatic    SAKind = 4
)

func saKindCode(kind string) SAKind {
	switch kind {
	case "SP":
		return SAKindSpell
	case "AB":
		return SAKindActivated
	case "DB":
		return SAKindDrawback
	case "ST":
		return SAKindStatic
	default:
		return SAKindUnknown
	}
}

// APICode is the stable opcode for an engine-owned effect API. Values are
// explicit and append-only; changing an assigned value requires a catalog
// schema bump.
type APICode uint16

const (
	APIUnknown               APICode = 0
	APIAddTurn               APICode = 1
	APIAmass                 APICode = 2
	APIAnimate               APICode = 3
	APIAttach                APICode = 4
	APIBecomeMonarch         APICode = 5
	APIBranch                APICode = 6
	APIChangeTargets         APICode = 7
	APIChangeZone            APICode = 8
	APIChangeZoneAll         APICode = 9
	APICharm                 APICode = 10
	APIChooseCard            APICode = 11
	APIChooseNumber          APICode = 12
	APIChoosePlayer          APICode = 13
	APIChooseType            APICode = 14
	APICleanup               APICode = 15
	APIControlSpell          APICode = 16
	APICopySpellAbility      APICode = 17
	APICounter               APICode = 18
	APICumulativeUpkeep      APICode = 19
	APIDamageAll             APICode = 20
	APIDealDamage            APICode = 21
	APIDelayedTrigger        APICode = 22
	APIDestroy               APICode = 23
	APIDestroyAll            APICode = 24
	APIDig                   APICode = 25
	APIDiscard               APICode = 26
	APIDraw                  APICode = 27
	APIEffect                APICode = 28
	APIEncore                APICode = 29
	APIExtort                APICode = 30
	APIFog                   APICode = 31
	APIGainControl           APICode = 32
	APIGainLife              APICode = 33
	APIGoad                  APICode = 34
	APIHideaway              APICode = 35
	APILoseLife              APICode = 36
	APILosesGame             APICode = 37
	APIMana                  APICode = 38
	APIManaReflected         APICode = 39
	APIMill                  APICode = 40
	APIMyriad                APICode = 41
	APINameCard              APICode = 42
	APIPair                  APICode = 43
	APIPeekAndReveal         APICode = 44
	APIPermanentCreature     APICode = 45
	APIPlay                  APICode = 46
	APIProtection            APICode = 47
	APIPump                  APICode = 48
	APIPumpAll               APICode = 49
	APIPutCounter            APICode = 50
	APIRearrangeTopOfLibrary APICode = 51
	APIRegenerate            APICode = 52
	APIRemoveCounterAll      APICode = 53
	APIRepeat                APICode = 54
	APIRepeatEach            APICode = 55
	APIReplaceEffect         APICode = 56
	APIReplaceMana           APICode = 57
	APIRestartGame           APICode = 58
	APIReveal                APICode = 59
	APIRevealHand            APICode = 60
	APIRollDice              APICode = 61
	APISacrifice             APICode = 62
	APISacrificeAll          APICode = 63
	APIScry                  APICode = 64
	APISetState              APICode = 65
	APIShuffle               APICode = 66
	APISurveil               APICode = 67
	APITap                   APICode = 68
	APITapAll                APICode = 69
	APIToken                 APICode = 70
	APIUntap                 APICode = 71
	APIUntapAll              APICode = 72
	APIVote                  APICode = 73
	APIWard                  APICode = 74
	APIEcho                  APICode = 75
	APIChangeX               APICode = 76
	APIAnimateAll            APICode = 77
	// Appended after AnimateAll (task alterattr1), following the enum's own
	// append-only precedent: no assigned value moves, so an existing compiled
	// cache stays valid.
	APIAlterAttribute APICode = 78
	// Appended after AlterAttribute (task api-winsgame), following the enum's
	// own append-only precedent: no assigned value moves, so an existing
	// compiled cache stays valid.
	APIWinsGame APICode = 79

	// APICodeCount includes the zero/unknown slot and sizes dense dispatch.
	APICodeCount = 80
)

// APICodeForName returns the stable opcode for an engine-owned effect API.
// Unknown and extension APIs return zero and retain textual dispatch.
func APICodeForName(api string) APICode {
	switch api {
	case "AddTurn":
		return APIAddTurn
	case "Amass":
		return APIAmass
	case "Animate":
		return APIAnimate
	case "AnimateAll":
		return APIAnimateAll
	case "AlterAttribute":
		return APIAlterAttribute
	case "WinsGame":
		return APIWinsGame
	case "Attach":
		return APIAttach
	case "BecomeMonarch":
		return APIBecomeMonarch
	case "Branch":
		return APIBranch
	case "ChangeTargets":
		return APIChangeTargets
	case "ChangeX":
		return APIChangeX
	case "ChangeZone":
		return APIChangeZone
	case "ChangeZoneAll":
		return APIChangeZoneAll
	case "Charm":
		return APICharm
	case "ChooseCard":
		return APIChooseCard
	case "ChooseNumber":
		return APIChooseNumber
	case "ChoosePlayer":
		return APIChoosePlayer
	case "ChooseType":
		return APIChooseType
	case "Cleanup":
		return APICleanup
	case "ControlSpell":
		return APIControlSpell
	case "CopySpellAbility":
		return APICopySpellAbility
	case "Counter":
		return APICounter
	case "CumulativeUpkeep":
		return APICumulativeUpkeep
	case "DamageAll":
		return APIDamageAll
	case "DealDamage":
		return APIDealDamage
	case "DelayedTrigger":
		return APIDelayedTrigger
	case "Destroy":
		return APIDestroy
	case "DestroyAll":
		return APIDestroyAll
	case "Dig":
		return APIDig
	case "Discard":
		return APIDiscard
	case "Draw":
		return APIDraw
	case "Echo":
		return APIEcho
	case "Effect":
		return APIEffect
	case "Encore":
		return APIEncore
	case "Extort":
		return APIExtort
	case "Fog":
		return APIFog
	case "GainControl":
		return APIGainControl
	case "GainLife":
		return APIGainLife
	case "Goad":
		return APIGoad
	case "Hideaway":
		return APIHideaway
	case "LoseLife":
		return APILoseLife
	case "LosesGame":
		return APILosesGame
	case "Mana":
		return APIMana
	case "ManaReflected":
		return APIManaReflected
	case "Mill":
		return APIMill
	case "Myriad":
		return APIMyriad
	case "NameCard":
		return APINameCard
	case "Pair":
		return APIPair
	case "PeekAndReveal":
		return APIPeekAndReveal
	case "PermanentCreature":
		return APIPermanentCreature
	case "Play":
		return APIPlay
	case "Protection":
		return APIProtection
	case "Pump":
		return APIPump
	case "PumpAll":
		return APIPumpAll
	case "PutCounter":
		return APIPutCounter
	case "RearrangeTopOfLibrary":
		return APIRearrangeTopOfLibrary
	case "Regenerate":
		return APIRegenerate
	case "RemoveCounterAll":
		return APIRemoveCounterAll
	case "Repeat":
		return APIRepeat
	case "RepeatEach":
		return APIRepeatEach
	case "ReplaceEffect":
		return APIReplaceEffect
	case "ReplaceMana":
		return APIReplaceMana
	case "RestartGame":
		return APIRestartGame
	case "Reveal":
		return APIReveal
	case "RevealHand":
		return APIRevealHand
	case "RollDice":
		return APIRollDice
	case "Sacrifice":
		return APISacrifice
	case "SacrificeAll":
		return APISacrificeAll
	case "Scry":
		return APIScry
	case "SetState":
		return APISetState
	case "Shuffle":
		return APIShuffle
	case "Surveil":
		return APISurveil
	case "Tap":
		return APITap
	case "TapAll":
		return APITapAll
	case "Token":
		return APIToken
	case "Untap":
		return APIUntap
	case "UntapAll":
		return APIUntapAll
	case "Vote":
		return APIVote
	case "Ward":
		return APIWard
	default:
		return APIUnknown
	}
}

type TriggerModeCode uint16

const (
	TriggerModeUnknown                    TriggerModeCode = 0
	TriggerModeChangesZone                TriggerModeCode = 1
	TriggerModeSpellCast                  TriggerModeCode = 2
	TriggerModeAbilityCast                TriggerModeCode = 3
	TriggerModeSpellAbilityCast           TriggerModeCode = 4
	TriggerModeAttacks                    TriggerModeCode = 5
	TriggerModeAttackersDeclaredOneTarget TriggerModeCode = 6
	TriggerModeAttackersDeclared          TriggerModeCode = 7
	TriggerModeAttackerBlocked            TriggerModeCode = 8
	TriggerModeSacrificed                 TriggerModeCode = 9
	TriggerModeDiscarded                  TriggerModeCode = 10
	TriggerModeLandPlayed                 TriggerModeCode = 11
	TriggerModeCycled                     TriggerModeCode = 12
	TriggerModeCommitCrime                TriggerModeCode = 13
	TriggerModeBecomesTarget              TriggerModeCode = 14
	TriggerModeTaps                       TriggerModeCode = 15
	TriggerModeTapsForMana                TriggerModeCode = 16
	TriggerModeDamageDone                 TriggerModeCode = 17
	TriggerModeDamageDealtOnce            TriggerModeCode = 18
	TriggerModeDamageDoneOnce             TriggerModeCode = 19
	TriggerModeCounterAdded               TriggerModeCode = 20
	TriggerModeDrawn                      TriggerModeCode = 21
	TriggerModeLifeLost                   TriggerModeCode = 22
	TriggerModeLifeLostAll                TriggerModeCode = 23
	TriggerModePhase                      TriggerModeCode = 24
	TriggerModeAlways                     TriggerModeCode = 25
	TriggerModeAttached                   TriggerModeCode = 26
	TriggerModeAttackerBlockedByCreature  TriggerModeCode = 27
)

func triggerModeCode(mode string) TriggerModeCode {
	switch mode {
	case "ChangesZone":
		return TriggerModeChangesZone
	case "SpellCast":
		return TriggerModeSpellCast
	case "AbilityCast":
		return TriggerModeAbilityCast
	case "SpellAbilityCast":
		return TriggerModeSpellAbilityCast
	case "Attacks":
		return TriggerModeAttacks
	case "AttackersDeclaredOneTarget":
		return TriggerModeAttackersDeclaredOneTarget
	case "AttackersDeclared":
		return TriggerModeAttackersDeclared
	case "AttackerBlocked":
		return TriggerModeAttackerBlocked
	case "AttackerBlockedByCreature":
		return TriggerModeAttackerBlockedByCreature
	case "Sacrificed":
		return TriggerModeSacrificed
	case "Discarded":
		return TriggerModeDiscarded
	case "LandPlayed":
		return TriggerModeLandPlayed
	case "Cycled":
		return TriggerModeCycled
	case "CommitCrime":
		return TriggerModeCommitCrime
	case "BecomesTarget":
		return TriggerModeBecomesTarget
	case "Taps":
		return TriggerModeTaps
	case "TapsForMana":
		return TriggerModeTapsForMana
	case "DamageDone":
		return TriggerModeDamageDone
	case "DamageDealtOnce":
		return TriggerModeDamageDealtOnce
	case "DamageDoneOnce":
		return TriggerModeDamageDoneOnce
	case "CounterAdded":
		return TriggerModeCounterAdded
	case "Drawn":
		return TriggerModeDrawn
	case "LifeLost":
		return TriggerModeLifeLost
	case "LifeLostAll":
		return TriggerModeLifeLostAll
	case "Phase":
		return TriggerModePhase
	case "Always":
		return TriggerModeAlways
	case "Attached":
		return TriggerModeAttached
	default:
		return TriggerModeUnknown
	}
}

type StaticModeCode uint16

const (
	StaticModeUnknown           StaticModeCode = 0
	StaticModeContinuous        StaticModeCode = 1
	StaticModeCantBeCast        StaticModeCode = 2
	StaticModeCantBeActivated   StaticModeCode = 3
	StaticModeRaiseCost         StaticModeCode = 4
	StaticModeReduceCost        StaticModeCode = 5
	StaticModeSetCost           StaticModeCode = 6
	StaticModeAlternativeCost   StaticModeCode = 7
	StaticModeCastWithFlash     StaticModeCode = 8
	StaticModeCantBlock         StaticModeCode = 9
	StaticModeCantBlockBy       StaticModeCode = 10
	StaticModePanharmonicon     StaticModeCode = 11
	StaticModeManaConvert       StaticModeCode = 12
	StaticModeMustAttack        StaticModeCode = 13
	StaticModeAttackRestrict    StaticModeCode = 14
	StaticModeNumLoyaltyAct     StaticModeCode = 15
	StaticModeCantGainLife      StaticModeCode = 16
	StaticModeCantPreventDamage StaticModeCode = 17
	StaticModeUntapOtherPlayer  StaticModeCode = 18
	StaticModeOptionalCost      StaticModeCode = 19
)

func staticModeCode(mode string) StaticModeCode {
	switch mode {
	case "Continuous":
		return StaticModeContinuous
	case "CantBeCast":
		return StaticModeCantBeCast
	case "CantBeActivated":
		return StaticModeCantBeActivated
	case "RaiseCost":
		return StaticModeRaiseCost
	case "ReduceCost":
		return StaticModeReduceCost
	case "SetCost":
		return StaticModeSetCost
	case "OptionalCost":
		return StaticModeOptionalCost
	case "AlternativeCost":
		return StaticModeAlternativeCost
	case "CastWithFlash":
		return StaticModeCastWithFlash
	case "CantBlock":
		return StaticModeCantBlock
	case "CantBlockBy":
		return StaticModeCantBlockBy
	case "Panharmonicon":
		return StaticModePanharmonicon
	case "ManaConvert":
		return StaticModeManaConvert
	case "MustAttack":
		return StaticModeMustAttack
	case "AttackRestrict":
		return StaticModeAttackRestrict
	case "NumLoyaltyAct":
		return StaticModeNumLoyaltyAct
	case "CantGainLife":
		return StaticModeCantGainLife
	case "CantPreventDamage":
		return StaticModeCantPreventDamage
	case "UntapOtherPlayer":
		return StaticModeUntapOtherPlayer
	default:
		return StaticModeUnknown
	}
}

type ReplacementEventCode uint8

const (
	ReplacementEventUnknown     ReplacementEventCode = 0
	ReplacementEventMoved       ReplacementEventCode = 1
	ReplacementEventUntap       ReplacementEventCode = 2
	ReplacementEventBeginPhase  ReplacementEventCode = 3
	ReplacementEventTransform   ReplacementEventCode = 4
	ReplacementEventProduceMana ReplacementEventCode = 5
	ReplacementEventGainLife    ReplacementEventCode = 6
	ReplacementEventLifeReduced ReplacementEventCode = 7
	ReplacementEventDamageDone  ReplacementEventCode = 8
	ReplacementEventCounter     ReplacementEventCode = 9
	ReplacementEventDraw        ReplacementEventCode = 10
)

func replacementEventCode(event string) ReplacementEventCode {
	switch event {
	case "Moved":
		return ReplacementEventMoved
	case "Untap":
		return ReplacementEventUntap
	case "BeginPhase":
		return ReplacementEventBeginPhase
	case "Transform":
		return ReplacementEventTransform
	case "ProduceMana":
		return ReplacementEventProduceMana
	case "GainLife":
		return ReplacementEventGainLife
	case "LifeReduced":
		return ReplacementEventLifeReduced
	case "DamageDone":
		return ReplacementEventDamageDone
	case "Counter":
		return ReplacementEventCounter
	case "Draw":
		return ReplacementEventDraw
	default:
		return ReplacementEventUnknown
	}
}

type TypeMask uint32

const (
	TypeArtifact TypeMask = 1 << iota
	TypeBattle
	TypeConspiracy
	TypeCreature
	TypeDungeon
	TypeEnchantment
	TypeInstant
	TypeKindred
	TypeLand
	TypePhenomenon
	TypePlane
	TypePlaneswalker
	TypeScheme
	TypeSorcery
	TypeTribal
	TypeVanguard
	TypeBasic
	TypeLegendary
	TypeOngoing
	TypeSnow
	TypeWorld
	TypeSpacecraft
	TypeVehicle
	TypeRoom
)

func typeMaskFor(name string) TypeMask {
	switch name {
	case "Artifact":
		return TypeArtifact
	case "Battle":
		return TypeBattle
	case "Conspiracy":
		return TypeConspiracy
	case "Creature":
		return TypeCreature
	case "Dungeon":
		return TypeDungeon
	case "Enchantment":
		return TypeEnchantment
	case "Instant":
		return TypeInstant
	case "Kindred":
		return TypeKindred
	case "Land":
		return TypeLand
	case "Phenomenon":
		return TypePhenomenon
	case "Plane":
		return TypePlane
	case "Planeswalker":
		return TypePlaneswalker
	case "Scheme":
		return TypeScheme
	case "Sorcery":
		return TypeSorcery
	case "Tribal":
		return TypeTribal
	case "Vanguard":
		return TypeVanguard
	case "Basic":
		return TypeBasic
	case "Legendary":
		return TypeLegendary
	case "Ongoing":
		return TypeOngoing
	case "Snow":
		return TypeSnow
	case "World":
		return TypeWorld
	case "Spacecraft":
		return TypeSpacecraft
	case "Vehicle":
		return TypeVehicle
	case "Room":
		return TypeRoom
	}
	switch strings.ToLower(name) {
	case "artifact":
		return TypeArtifact
	case "battle":
		return TypeBattle
	case "conspiracy":
		return TypeConspiracy
	case "creature":
		return TypeCreature
	case "dungeon":
		return TypeDungeon
	case "enchantment":
		return TypeEnchantment
	case "instant":
		return TypeInstant
	case "kindred":
		return TypeKindred
	case "land":
		return TypeLand
	case "phenomenon":
		return TypePhenomenon
	case "plane":
		return TypePlane
	case "planeswalker":
		return TypePlaneswalker
	case "scheme":
		return TypeScheme
	case "sorcery":
		return TypeSorcery
	case "tribal":
		return TypeTribal
	case "vanguard":
		return TypeVanguard
	case "basic":
		return TypeBasic
	case "legendary":
		return TypeLegendary
	case "ongoing":
		return TypeOngoing
	case "snow":
		return TypeSnow
	case "world":
		return TypeWorld
	case "spacecraft":
		return TypeSpacecraft
	case "vehicle":
		return TypeVehicle
	case "room":
		return TypeRoom
	default:
		return 0
	}
}

type KeywordMask uint32

const (
	KeywordAlternateAdditionalCost KeywordMask = 1 << iota
	KeywordBuyback
	KeywordChapter
	KeywordCycling
	KeywordDethrone
	KeywordDevoid
	KeywordDredge
	KeywordEnchant
	KeywordFlash
	KeywordFlashback
	KeywordHarmonize
	KeywordKicker
	KeywordMadness
	KeywordMayEffectFromOpeningHand
	KeywordMiracle
	KeywordRiot
	KeywordSurge
	KeywordSuspend
)

func keywordMaskFor(head string) KeywordMask {
	switch head {
	case "AlternateAdditionalCost":
		return KeywordAlternateAdditionalCost
	case "Buyback":
		return KeywordBuyback
	case "Chapter":
		return KeywordChapter
	case "Cycling":
		return KeywordCycling
	case "Dethrone":
		return KeywordDethrone
	case "Devoid":
		return KeywordDevoid
	case "Dredge":
		return KeywordDredge
	case "Enchant":
		return KeywordEnchant
	case "Flash":
		return KeywordFlash
	case "Flashback":
		return KeywordFlashback
	case "Harmonize":
		return KeywordHarmonize
	case "Kicker":
		return KeywordKicker
	case "Madness":
		return KeywordMadness
	case "MayEffectFromOpeningHand":
		return KeywordMayEffectFromOpeningHand
	case "Miracle":
		return KeywordMiracle
	case "Riot":
		return KeywordRiot
	case "Surge":
		return KeywordSurge
	case "Suspend":
		return KeywordSuspend
	}
	// Case-insensitive fallback. Most lookups land here -- every keyword with
	// no mask ("Flying", "Haste", ...) misses the exact switch above -- and
	// strings.ToLower allocates for any head carrying an upper-case letter,
	// which made this the single largest allocation site under the legal-offer
	// walk. ASCII heads are folded into a stack buffer instead (switching on
	// string(buf) does not allocate); a non-ASCII head keeps strings.ToLower so
	// Unicode folding (U+212A KELVIN SIGN -> 'k') is byte-for-byte unchanged.
	var buf [len("mayeffectfromopeninghand")]byte
	ascii := true
	for i := 0; i < len(head); i++ {
		if head[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if !ascii {
		return keywordMaskForLower(strings.ToLower(head))
	}
	if len(head) > len(buf) {
		return 0
	}
	for i := 0; i < len(head); i++ {
		c := head[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		buf[i] = c
	}
	return keywordMaskForLower(string(buf[:len(head)]))
}

func keywordMaskForLower(lower string) KeywordMask {
	switch lower {
	case "alternateadditionalcost":
		return KeywordAlternateAdditionalCost
	case "buyback":
		return KeywordBuyback
	case "chapter":
		return KeywordChapter
	case "cycling":
		return KeywordCycling
	case "dethrone":
		return KeywordDethrone
	case "devoid":
		return KeywordDevoid
	case "dredge":
		return KeywordDredge
	case "enchant":
		return KeywordEnchant
	case "flash":
		return KeywordFlash
	case "flashback":
		return KeywordFlashback
	case "harmonize":
		return KeywordHarmonize
	case "kicker":
		return KeywordKicker
	case "madness":
		return KeywordMadness
	case "mayeffectfromopeninghand":
		return KeywordMayEffectFromOpeningHand
	case "miracle":
		return KeywordMiracle
	case "riot":
		return KeywordRiot
	case "surge":
		return KeywordSurge
	case "suspend":
		return KeywordSuspend
	default:
		return 0
	}
}

type ColourMask uint8

const (
	ColourMaskWhite ColourMask = 1 << iota
	ColourMaskBlue
	ColourMaskBlack
	ColourMaskRed
	ColourMaskGreen
	ColourMaskColourless
)

func colourMaskFor(symbol string) ColourMask {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "W":
		return ColourMaskWhite
	case "U":
		return ColourMaskBlue
	case "B":
		return ColourMaskBlack
	case "R":
		return ColourMaskRed
	case "G":
		return ColourMaskGreen
	case "C":
		return ColourMaskColourless
	default:
		return 0
	}
}

type FaceFlags uint8

const (
	FaceFlagPermanent FaceFlags = 1 << iota
	FaceFlagCharacteristicDefining
)

// TriggerInterest is a cards-owned semantic event class. It deliberately
// does not encode events.Kind ordinals.
type TriggerInterest uint16

const (
	TriggerInterestAny TriggerInterest = 1 << iota
	TriggerInterestZoneChange
	TriggerInterestStackPut
	TriggerInterestAbilityPush
	TriggerInterestAttackDeclaration
	TriggerInterestTargetsChosen
	TriggerInterestTap
	TriggerInterestDamage
	TriggerInterestDraw
	TriggerInterestLifeChange
	TriggerInterestStepChange
	TriggerInterestAttach
	TriggerInterestExplore
	TriggerInterestCastInfo
	// TriggerInterestMonarch is the monarch-designation transition class
	// (events.MonarchChange), which trig:BecomeMonarch fires on. It gets a
	// dedicated bit so a BecomeMonarch-only face's compiled scan set narrows
	// to that one event kind instead of the fail-open Any default.
	TriggerInterestMonarch
)

func triggerInterestForMode(mode string) TriggerInterest {
	switch mode {
	case "ChangesZone", "Sacrificed", "Discarded", "DiscardedAll", "LandPlayed", "Cycled", "Milled", "MilledAll":
		return TriggerInterestZoneChange
	case "SpellCast":
		return TriggerInterestStackPut
	case "AbilityCast":
		return TriggerInterestAbilityPush
	case "SpellAbilityCast":
		// The spell-or-activate union (targetsvalid1): the activation arm
		// matches an AbilityPush, the spell arm a PutOnStack.
		return TriggerInterestAbilityPush | TriggerInterestStackPut
	case "Attacks", "AttackersDeclaredOneTarget", "AttackersDeclared", "AttackerBlocked",
		"AttackerBlockedByCreature", "Blocks":
		return TriggerInterestAttackDeclaration
	case "CommitCrime", "BecomesTarget", "BecomesTargetOnce":
		return TriggerInterestTargetsChosen
	case "Attached":
		return TriggerInterestAttach
	case "Explores":
		return TriggerInterestExplore
	case "ManaExpend":
		// The crossing trigger fires on its own pay-time FlagManaExpendCast
		// CastInfo emission (rules' manaExpendMatches); the dedicated bit
		// keeps a ManaExpend-only face's scan set narrow instead of the
		// fail-open default.
		return TriggerInterestCastInfo
	case "Foretell":
		// trig:Foretell (task agent-20260923T032009Z-3b9d3432) fires on the
		// {2} Foretell action's pay-time FlagForetold CastInfo and the
		// effect-designation exile MoveZone marker (rules'
		// foretellMatches); the two bits keep a Foretell-only face's scan
		// set narrow instead of the fail-open Any default.
		return TriggerInterestCastInfo | TriggerInterestZoneChange
	case "BecomeMonarch":
		// The monarch designation transition (events.MonarchChange), matched
		// by rules' becomeMonarchMatches. The dedicated bit narrows a
		// BecomeMonarch-only face's scan set to that kind.
		return TriggerInterestMonarch
	case "Taps", "TapsForMana":
		return TriggerInterestTap
	case "DamageDone", "DamageDealtOnce", "DamageDoneOnce":
		return TriggerInterestDamage
	case "Drawn":
		return TriggerInterestDraw
	case "LifeLost":
		return TriggerInterestDamage | TriggerInterestLifeChange
	case "Phase":
		return TriggerInterestStepChange
	case "CounterAdded", "LifeLostAll", "Always":
		return TriggerInterestAny
	default:
		return TriggerInterestAny
	}
}
