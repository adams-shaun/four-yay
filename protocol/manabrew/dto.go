package manabrew

import "encoding/json"

// Wire enums and shared DTOs from the ManaBrew protocol
// (https://docs.manabrew.app/protocol/game-view/ and /protocol/shared-types/).
//
// Types the published spec references but never defines (ZoneKind, DayTime,
// PlayerStatus, PlayerCounterKind, Mana inside ActivatableAbilityInfo) are
// modelled as opaque string/number types; constants marked INFERRED are
// conveniences derived from the spec's examples, not from a published enum.

// StepKind is the defined step enum (game-view page).
type StepKind string

const (
	StepUntap                   StepKind = "untap"
	StepUpkeep                  StepKind = "upkeep"
	StepDraw                    StepKind = "draw"
	StepMain1                   StepKind = "main1"
	StepCombatBegin             StepKind = "combatBegin"
	StepCombatDeclareAttackers  StepKind = "combatDeclareAttackers"
	StepCombatDeclareBlockers   StepKind = "combatDeclareBlockers"
	StepCombatFirstStrikeDamage StepKind = "combatFirstStrikeDamage"
	StepCombatDamage            StepKind = "combatDamage"
	StepCombatEnd               StepKind = "combatEnd"
	StepMain2                   StepKind = "main2"
	StepEndOfTurn               StepKind = "endOfTurn"
	StepCleanup                 StepKind = "cleanup"
)

// ZoneKind is UNDEFINED in the published spec; the values below are INFERRED
// from the game-view examples.
type ZoneKind string

const (
	ZoneLibrary     ZoneKind = "library"
	ZoneHand        ZoneKind = "hand"
	ZoneBattlefield ZoneKind = "battlefield"
	ZoneGraveyard   ZoneKind = "graveyard"
	ZoneExile       ZoneKind = "exile"
	ZoneCommand     ZoneKind = "command"
	ZoneStack       ZoneKind = "stack"
)

// DayTime is UNDEFINED in the published spec.
type DayTime string

// PlayerStatus is UNDEFINED in the published spec.
type PlayerStatus string

// PlayerCounterKind is UNDEFINED; the game-view page names the canonical
// counter keys "P1P1", "Loyalty" and uppercase one-offs.
type PlayerCounterKind string

const (
	CounterP1P1    PlayerCounterKind = "P1P1"
	CounterLoyalty PlayerCounterKind = "Loyalty"
)

// ManaColor is the colour palette used by manaPool maps and Mana values.
type ManaColor string

const (
	ColorWhite     ManaColor = "W"
	ColorBlue      ManaColor = "U"
	ColorBlack     ManaColor = "B"
	ColorRed       ManaColor = "R"
	ColorGreen     ManaColor = "G"
	ColorColorless ManaColor = "C"
)

// TargetRefKind is the defined TargetRef kind enum (shared-types page).
type TargetRefKind string

const (
	RefPlayer TargetRefKind = "player"
	RefCard   TargetRefKind = "card"
	RefSpell  TargetRefKind = "spell"
)

// ScryDestination is the defined scry destination enum.
type ScryDestination string

const (
	DestinationLibraryTop    ScryDestination = "libraryTop"
	DestinationLibraryBottom ScryDestination = "libraryBottom"
	DestinationGraveyard     ScryDestination = "graveyard"
	DestinationExile         ScryDestination = "exile"
	DestinationHand          ScryDestination = "hand"
)

// TargetingIntent is the defined intent enum (shared-types page).
type TargetingIntent string

const (
	IntentDamage      TargetingIntent = "damage"
	IntentDestroy     TargetingIntent = "destroy"
	IntentSacrifice   TargetingIntent = "sacrifice"
	IntentExile       TargetingIntent = "exile"
	IntentBounce      TargetingIntent = "bounce"
	IntentMill        TargetingIntent = "mill"
	IntentDiscard     TargetingIntent = "discard"
	IntentCounter     TargetingIntent = "counter"
	IntentTap         TargetingIntent = "tap"
	IntentUntap       TargetingIntent = "untap"
	IntentCopy        TargetingIntent = "copy"
	IntentBuff        TargetingIntent = "buff"
	IntentDebuff      TargetingIntent = "debuff"
	IntentHeal        TargetingIntent = "heal"
	IntentLoseLife    TargetingIntent = "loseLife"
	IntentReveal      TargetingIntent = "reveal"
	IntentDraw        TargetingIntent = "draw"
	IntentFetch       TargetingIntent = "fetch"
	IntentGainControl TargetingIntent = "gainControl"
	IntentFight       TargetingIntent = "fight"
	IntentAttach      TargetingIntent = "attach"
	IntentAttack      TargetingIntent = "attack"
	IntentBlock       TargetingIntent = "block"
	IntentHostile     TargetingIntent = "hostile"
	IntentFriendly    TargetingIntent = "friendly"
)

// Mana is UNDEFINED in the published spec; {color, amount} is INFERRED from
// the examples.
type Mana struct {
	Color  ManaColor `json:"color"`
	Amount int       `json:"amount"`
}

// TargetRef is a reference to a targetable player, card or spell.
type TargetRef struct {
	Kind   TargetRefKind   `json:"kind"`
	ID     string          `json:"id"`
	Intent TargetingIntent `json:"intent,omitempty"`
	Oracle string          `json:"oracle,omitempty"`
}

// CardIdentity names a card independent of any game instance.
type CardIdentity struct {
	Name       string `json:"name"`
	SetCode    string `json:"setCode"`
	CardNumber string `json:"cardNumber"`
	IsToken    bool   `json:"isToken"`
	// TokenScript is a Forge tokenscripts stem ("taken directly from the Forge
	// spec" per the protocol index).
	TokenScript string `json:"tokenScript,omitempty"`
}

// ClassLevelDto is UNDEFINED in the published spec; the fields are INFERRED.
type ClassLevelDto struct {
	Level     int      `json:"level"`
	MinLevel  int      `json:"minLevel,omitempty"`
	MaxLevel  int      `json:"maxLevel,omitempty"`
	Abilities []string `json:"abilities,omitempty"`
}

// SagaChapterDto is UNDEFINED in the published spec; the fields are INFERRED.
type SagaChapterDto struct {
	Chapter int    `json:"chapter"`
	Ability string `json:"ability,omitempty"`
}

// CardChoiceDto is UNDEFINED in the published spec; the fields are INFERRED.
type CardChoiceDto struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// CardDto is the full card shape carried by visible CardViews and prompts
// (game-view page). Counter keys follow the engine's canonical form: "P1P1",
// "Loyalty", uppercase one-offs.
type CardDto struct {
	ID         string       `json:"id"`
	Identity   CardIdentity `json:"identity"`
	Color      []string     `json:"color"`
	ManaCost   string       `json:"manaCost"`
	CMC        float64      `json:"cmc"`
	Types      []string     `json:"types"`
	Subtypes   []string     `json:"subtypes"`
	Supertypes []string     `json:"supertypes"`
	Power      *string      `json:"power"`
	Toughness  *string      `json:"toughness"`
	// BasePower/BaseToughness have an unspecified type in the published spec;
	// modelled as strings for parity with power/toughness. INFERRED.
	BasePower         *string          `json:"basePower,omitempty"`
	BaseToughness     *string          `json:"baseToughness,omitempty"`
	FinalChapter      *int             `json:"finalChapter,omitempty"`
	ClassLevel        *int             `json:"classLevel,omitempty"`
	ClassLevels       []ClassLevelDto  `json:"classLevels"`
	SagaChapters      []SagaChapterDto `json:"sagaChapters"`
	Text              string           `json:"text"`
	Choices           []CardChoiceDto  `json:"choices"`
	ControllerID      string           `json:"controllerId"`
	OwnerID           string           `json:"ownerId"`
	Tapped            bool             `json:"tapped"`
	IsCrewed          bool             `json:"isCrewed"`
	IsAttacking       bool             `json:"isAttacking"`
	AttackingPlayerID string           `json:"attackingPlayerId,omitempty"`
	AttackTargetID    string           `json:"attackTargetId,omitempty"`
	Keywords          []string         `json:"keywords"`
	Counters          map[string]int   `json:"counters"`
	Damage            int              `json:"damage"`
	SummoningSick     bool             `json:"summoningSick"`
	IsCopy            bool             `json:"isCopy"`
	IsDoubleFaced     bool             `json:"isDoubleFaced"`
	IsTransformed     bool             `json:"isTransformed"`
	IsFaceDown        bool             `json:"isFaceDown"`
	IsBestowed        bool             `json:"isBestowed"`
	PhasedOut         bool             `json:"phasedOut"`
	Exerted           bool             `json:"exerted"`
	IsRingBearer      bool             `json:"isRingBearer"`
	AttachedTo        string           `json:"attachedTo,omitempty"`
	AttachmentIDs     []string         `json:"attachmentIds"`
	MergedCardIDs     []string         `json:"mergedCardIds"`
	FlashbackCost     string           `json:"flashbackCost,omitempty"`
	KickerCost        string           `json:"kickerCost,omitempty"`
	EffectiveManaCost string           `json:"effectiveManaCost,omitempty"`
	CommanderTax      *int             `json:"commanderTax,omitempty"`
	MadnessCost       string           `json:"madnessCost,omitempty"`
	IsMadnessExiled   bool             `json:"isMadnessExiled"`
	IsPlotted         bool             `json:"isPlotted"`
	IsWarpExiled      bool             `json:"isWarpExiled"`
	Foil              bool             `json:"foil"`
	WouldDieInCombat  bool             `json:"wouldDieInCombat"`
}

// CardViewValue is the closed set of card view payloads; see PromptInputData
// (package prompts) for why the union is a struct carrier.
type CardViewValue interface{ isCardViewValue() }

// CardView is the visibility-tagged union a ZoneDto carries:
//
//	({"visibility":"visible"} & CardDto) | {"visibility":"hidden","id"}
type CardView struct {
	Value CardViewValue `json:"-"`
}

// VisibleCard is a fully readable card.
type VisibleCard struct {
	Visibility string `json:"visibility"`
	CardDto           // flattened into the same object
}

// HiddenCard carries only the instance id.
type HiddenCard struct {
	Visibility string `json:"visibility"`
	ID         string `json:"id"`
}

func (VisibleCard) isCardViewValue() {}
func (HiddenCard) isCardViewValue()  {}

func (c *CardView) UnmarshalJSON(b []byte) error {
	var h struct {
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	var v CardViewValue
	switch h.Visibility {
	case "visible":
		var x VisibleCard
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	case "hidden":
		var x HiddenCard
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	default:
		return unknownDiscriminator("visibility", h.Visibility)
	}
	*c = CardView{Value: v}
	return nil
}

func (c CardView) MarshalJSON() ([]byte, error) {
	if c.Value == nil {
		return nil, unknownDiscriminator("visibility", "<nil>")
	}
	return json.Marshal(c.Value)
}

// ZoneDto is one (zone, owner) bucket. Battlefield is bucketed by controller,
// so ZoneDto.OwnerID names the owner for hand/library/graveyard/exile/command
// and the controller for battlefield buckets (game-view page).
type ZoneDto struct {
	Zone    ZoneKind   `json:"zone"`
	OwnerID string     `json:"ownerId"`
	Cards   []CardView `json:"cards"`
	Count   int        `json:"count"`
}

// PlayerDto is one player's public state (game-view page). encoding/json sorts
// the keys of the counters/manaPool/commanderDamage maps, which keeps encodes
// deterministic.
type PlayerDto struct {
	ID                  string                    `json:"id"`
	Name                string                    `json:"name"`
	Status              PlayerStatus              `json:"status"`
	IsHuman             bool                      `json:"isHuman"`
	Life                int                       `json:"life"`
	MaxHandSize         int                       `json:"maxHandSize"`
	UnlimitedHandSize   bool                      `json:"unlimitedHandSize"`
	LandsPlayedThisTurn int                       `json:"landsPlayedThisTurn"`
	MaxLandPlaysPerTurn int                       `json:"maxLandPlaysPerTurn"`
	UnlimitedLandPlays  bool                      `json:"unlimitedLandPlays"`
	CardsDrawnThisTurn  int                       `json:"cardsDrawnThisTurn"`
	DamagePrevention    int                       `json:"damagePrevention"`
	IsExtraTurn         bool                      `json:"isExtraTurn"`
	ExtraTurnCount      int                       `json:"extraTurnCount"`
	ControlledBy        string                    `json:"controlledBy,omitempty"`
	PlayerKeywords      []string                  `json:"playerKeywords"`
	CommanderCasts      map[string]int            `json:"commanderCasts"`
	DungeonState        json.RawMessage           `json:"dungeonState,omitempty"` // DungeonStateDto is UNDEFINED
	ActiveSchemeNames   []string                  `json:"activeSchemeNames,omitempty"`
	TeamNumber          *int                      `json:"teamNumber,omitempty"`
	Counters            map[PlayerCounterKind]int `json:"counters"`
	ManaPool            map[ManaColor]int         `json:"manaPool"`
	CommanderDamage     map[string]int            `json:"commanderDamage"`
	HasCityBlessing     bool                      `json:"hasCityBlessing"`
	HasEnduringStory    bool                      `json:"hasEnduringStory"`
	RingLevel           int                       `json:"ringLevel"`
	Speed               int                       `json:"speed"`
}

// CombatAssignmentDto is one blocker→attacker pairing.
type CombatAssignmentDto struct {
	BlockerID  string `json:"blockerId"`
	AttackerID string `json:"attackerId"`
}

// StackObjectDto is one object on the stack (game-view page).
type StackObjectDto struct {
	ID                string       `json:"id"`
	SourceID          string       `json:"sourceId"`
	ControllerID      string       `json:"controllerId"`
	OwnerID           string       `json:"ownerId"`
	Identity          CardIdentity `json:"identity"`
	Text              string       `json:"text"`
	SourceAbilityText string       `json:"sourceAbilityText,omitempty"`
	IsPermanentSpell  bool         `json:"isPermanentSpell"`
	IsCasting         bool         `json:"isCasting"`
	IsDoubleFaced     bool         `json:"isDoubleFaced"`
	FaceIndex         int          `json:"faceIndex"`
	Targets           []TargetRef  `json:"targets"`
}

// GameViewDto is the authoritative full state carried by a `state` message
// (game-view page).
type GameViewDto struct {
	GameID             string                `json:"gameId"`
	Turn               int                   `json:"turn"`
	Step               StepKind              `json:"step"`
	CombatAssignments  []CombatAssignmentDto `json:"combatAssignments"`
	ActivePlayerID     string                `json:"activePlayerId"`
	PriorityPlayerID   string                `json:"priorityPlayerId"`
	Players            []PlayerDto           `json:"players"`
	Zones              []ZoneDto             `json:"zones"`
	Stack              []StackObjectDto      `json:"stack"`
	GameOver           bool                  `json:"gameOver"`
	WinnerID           *string               `json:"winnerId"`
	MonarchID          *string               `json:"monarchId"`
	InitiativeHolderID *string               `json:"initiativeHolderId"`
	DayTime            DayTime               `json:"dayTime"`
	ActivePlaneNames   []string              `json:"activePlaneNames,omitempty"`
}

// AvailableAction is one legal action in a chooseAction prompt. The published
// spec defines it as a union on `type` (cast | activateAbility | undoMana),
// but only `kind`, `input.type`, `output.type` and `visibility` carry union
// codecs in this package (the MB-1 brief's required discriminators), so it is
// decoded into one flat struct with optional fields. The doc inconsistency
// where the example sends `modeLabel` while the interface says `label` shows
// up as a reported unknown path ("actions[].modeLabel"), never as a decode
// error.
type AvailableAction struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	CardID         string `json:"cardId,omitempty"`
	Mode           string `json:"mode,omitempty"` // PlayCardMode is UNDEFINED; only "cast" appears in any example
	Label          string `json:"label,omitempty"`
	AbilityIndex   int    `json:"abilityIndex,omitempty"`
	Description    string `json:"description,omitempty"`
	IsManaAbility  bool   `json:"isManaAbility,omitempty"`
	IsClassLevelUp bool   `json:"isClassLevelUp,omitempty"`
	Cost           string `json:"cost,omitempty"`
	ProducedMana   []Mana `json:"producedMana,omitempty"`
}

// Deck and its children are the deck data format
// (https://docs.manabrew.app/protocol/deck/). No message carries a Deck yet;
// the shapes are modelled so the adapter can accept one later.
type Deck struct {
	Name            string              `json:"name"`
	Version         string              `json:"version,omitempty"`
	ID              string              `json:"id,omitempty"`
	Description     string              `json:"description,omitempty"`
	Color           []string            `json:"color,omitempty"`
	Format          string              `json:"format,omitempty"`
	Cards           []DeckCard          `json:"cards,omitempty"`
	Sideboard       []DeckCard          `json:"sideboard,omitempty"`
	Attractions     []DeckCard          `json:"attractions,omitempty"`
	Contraptions    []DeckCard          `json:"contraptions,omitempty"`
	Schemes         []DeckCard          `json:"schemes,omitempty"`
	Planes          []DeckCard          `json:"planes,omitempty"`
	Commanders      []DeckCard          `json:"commanders,omitempty"`
	Companion       *DeckCard           `json:"companion,omitempty"`
	Maybeboard      []DeckCard          `json:"maybeboard,omitempty"`
	Draft           json.RawMessage     `json:"draft,omitempty"`
	Labels          []DeckLabel         `json:"labels,omitempty"`
	CustomTags      []string            `json:"customTags,omitempty"`
	CardTags        map[string][]string `json:"cardTags,omitempty"`
	Editor          string              `json:"editor,omitempty"`
	CoverCardName   string              `json:"coverCardName,omitempty"`
	CoverCardFace   string              `json:"coverCardFace,omitempty"`
	PlaymatURL      string              `json:"playmatUrl,omitempty"`
	PlaymatAssetID  string              `json:"playmatAssetId,omitempty"`
	PlaymatSettings json.RawMessage     `json:"playmatSettings,omitempty"`
	StackPositions  json.RawMessage     `json:"stackPositions,omitempty"`
	Tokens          []DeckCard          `json:"tokens,omitempty"`
}

// DeckCardIdentity identifies a deck entry; ID is a client instance id.
type DeckCardIdentity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SetCode     string `json:"setCode"`
	CardNumber  string `json:"cardNumber"`
	OracleID    string `json:"oracleId,omitempty"`
	TokenScript string `json:"tokenScript,omitempty"`
	Foil        bool   `json:"foil,omitempty"`
}

// CardImageUris is INFERRED from the deck page's uris field; the field set is
// the Scryfall image-URI layout.
type CardImageUris struct {
	Small      string `json:"small,omitempty"`
	Normal     string `json:"normal,omitempty"`
	Large      string `json:"large,omitempty"`
	ArtCrop    string `json:"art_crop,omitempty"`
	BorderCrop string `json:"border_crop,omitempty"`
}

// CardBackFaceSummary summarises the back face of a double-faced deck card.
type CardBackFaceSummary struct {
	Name      string   `json:"name,omitempty"`
	ManaCost  string   `json:"manaCost,omitempty"`
	Types     []string `json:"types,omitempty"`
	Power     string   `json:"power,omitempty"`
	Toughness string   `json:"toughness,omitempty"`
}

// DeckCard is one card entry of a Deck.
type DeckCard struct {
	Identity      DeckCardIdentity     `json:"identity"`
	URIs          CardImageUris        `json:"uris"`
	ImageLanguage string               `json:"imageLanguage,omitempty"`
	AllParts      []json.RawMessage    `json:"allParts,omitempty"`
	Color         []string             `json:"color"`
	ColorIdentity []string             `json:"colorIdentity"`
	ManaCost      string               `json:"manaCost"`
	CMC           float64              `json:"cmc"`
	Types         []string             `json:"types"`
	Subtypes      []string             `json:"subtypes"`
	Supertypes    []string             `json:"supertypes"`
	Keywords      []string             `json:"keywords"`
	Power         string               `json:"power"`
	Toughness     string               `json:"toughness"`
	Text          string               `json:"text"`
	Layout        string               `json:"layout"`
	IsDoubleFaced bool                 `json:"isDoubleFaced"`
	BackFace      *CardBackFaceSummary `json:"backFace,omitempty"`
}

// DeckLabel is one named deck label with an optional colour.
type DeckLabel struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}
