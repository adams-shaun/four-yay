package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:Vote's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on CopyPermanent's pattern (copypermanent_params.go). compileVote is
// the ONLY reader of a Vote ability's own parameters: the resolution
// (vote_effect.go for the fixed-list and card ballots, vote.go for the
// player ballot) reads the compiled VoteParams, including the split Choices$
// option list, which is a pure function of the text and so is decided here
// once. internal/codeshape's voteParamLeaks ratchet holds that: no parameter
// read in vote.go or vote_effect.go, and no read of a Vote-only key anywhere
// else in rules/ or effects/.
//
// The Defined$ voter list comes from the Defined-reference tier
// (definedPlayers).

// VoteParams is one Vote ability's parameters, compiled once. Text fields are
// trimmed.
type VoteParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Card is VoteCard$: non-empty selects the card ballot, a permanent
	// filter.
	Card string
	// Player is VotePlayer$: the player ballot's selector; PlayerOther is
	// its Other spelling (every living player but the voter).
	Player      string
	PlayerOther bool
	// Choices are Choices$'s SVar names, trimmed, blanks dropped (nil when
	// the value is blank).
	Choices []string
	// Secretly is Secretly$ True; StoreVoteNum is StoreVoteNum$ True; UpTo
	// is UpTo$ True; RememberVoted is RememberVotedObjects$ True.
	Secretly      bool
	StoreVoteNum  bool
	UpTo          bool
	RememberVoted bool
	// TiedAbility is VoteTiedAbility$, SubAbility is VoteSubAbility$ and
	// Message is VoteMessage$.
	TiedAbility string
	SubAbility  string
	Message     string

	// Unread are the parameters present on the ability that no Vote reader
	// consumes: the resolution Notes them once.
	Unread []string
}

// voteKnownKeys is every parameter key a Vote resolution consumes or
// deliberately ignores, sorted (changeZoneKnownKeys' contract): rules'
// TestVoteKnownKeysMatchTheCensus holds it equal to the parameter census's
// measured api:Vote read set plus its ignored and structural keys.
var voteKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment", "AITgts",
	"Activation", "ActivationAfterBlockers", "ActivationFirstCombat", "ActivationGameTypes",
	"ActivationLimit", "ActivationPhases", "ActivationZone", "Activator", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "Affected", "AlternateCost",
	"AlternativeCost", "Announce", "AnnounceTitle", "Boast", "ChangeTypeDesc",
	"CharacteristicDefining", "CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices",
	"ChooseFromList", "ClassBand", "ClearImprinted", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare", "ConditionCompare2",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn", "ConditionPresent", "ConditionPresent2",
	"ConditionSVarCompare", "ConditionZone", "CopyCard", "Cost", "CostDesc", "Defined", "DefinedCards",
	"DefinedTarget", "Description", "Exclude", "Exhaust", "ForgetOtherTargets", "GameActivationLimit", "Image",
	"ImprintCards", "ImprintPlayed", "InstantSpeed", "IsCurse", "IsPresent", "KW",
	"Keyword", "KeywordLine", "MaxTotalTargetCMC", "MaxTotalTargetPower", "Mentor",
	"ModeCost", "Monstrosity", "NewController", "NumDmg", "OpponentTurn", "Planeswalker",
	"PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc", "PresentCompare", "PresentDefined",
	"PresentZone", "RandomNumTargets", "ReduceAmount", "ReduceCost", "RememberAnimated", "RememberCostMana",
	"RememberObjects", "RememberTargets", "RememberVotedObjects", "ReplaceColor", "ReplaceGraveyard",
	"ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly", "ReplaceType", "SVarCompare",
	"Secretly", "SelectPrompt", "SetChosenMode", "SetColor", "ShowCards", "SorcerySpeed",
	"SpellDescription", "StackDescription", "StoreVoteNum", "SubAbility", "TargetMax",
	"TargetMin", "TargetType", "TargetUnique", "TargetValidTargeting", "TargetingPlayer",
	"TargetingPlayerControls", "TargetsAtRandom", "TargetsForEachPlayer",
	"TargetsWithControllerProperty", "TargetsWithDefinedController",
	"TargetsWithDifferentCMC", "TargetsWithDifferentControllers",
	"TargetsWithDifferentNames", "TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType", "TargetsWithSharedCardType",
	"TargetsWithSharedTypes", "TgtPrompt", "TgtZone", "TokenScript", "TriggerDescription",
	"Type", "Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "UpTo", "ValidCard", "ValidCards", "ValidCardsDesc", "ValidChoices",
	"ValidCounterType", "ValidDescription", "ValidTgts", "VoteCard", "VoteMessage",
	"VotePlayer", "VoteSubAbility", "VoteTiedAbility", "WithoutManaCost", "XMax", "XMin",
}

// isVoteSA reports whether sa resolves as api:Vote.
func isVoteSA(sa *cards.SA) bool {
	return sa != nil && sa.API == "Vote"
}

// VoteOf returns sa's compiled Vote parameters: the configured record's when
// it is bound to sa's Params map, else the front cache's entry for that map,
// else a fresh compile stored in the front cache (ChangeZoneOf's contract).
func VoteOf(sa *cards.SA) *VoteParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Vote; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &voteFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileVote(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// voteFront is VoteOf's direct-mapped front cache (czFront's shape).
var voteFront [1 << 10]atomic.Pointer[VoteParams]

// compileVote is the one reader of a Vote ability's parameters.
func compileVote(sa *cards.SA) *VoteParams {
	p := &VoteParams{paramBinding: bindParams(sa)}
	p.Card = strings.TrimSpace(sa.ParamStr(cards.PKVoteCard))
	p.Player = strings.TrimSpace(sa.ParamStr(cards.PKVotePlayer))
	p.PlayerOther = strings.EqualFold(p.Player, "Other")
	if raw := sa.ParamStr(cards.PKChoices); strings.TrimSpace(raw) != "" {
		parts := strings.Split(raw, ",")
		p.Choices = make([]string, 0, len(parts))
		for _, s := range parts {
			if s = strings.TrimSpace(s); s != "" {
				p.Choices = append(p.Choices, s)
			}
		}
	}
	p.Secretly = isTrue(sa.ParamStr(cards.PKSecretly))
	p.StoreVoteNum = isTrue(sa.ParamStr(cards.PKStoreVoteNum))
	p.UpTo = isTrue(sa.ParamStr(cards.PKUpTo))
	p.RememberVoted = isTrue(sa.ParamStr(cards.PKRememberVotedObjects))
	p.TiedAbility = strings.TrimSpace(sa.ParamStr(cards.PKVoteTiedAbility))
	p.SubAbility = strings.TrimSpace(sa.ParamStr(cards.PKVoteSubAbility))
	p.Message = strings.TrimSpace(sa.ParamStr(cards.PKVoteMessage))
	p.Unread = unreadKeys(sa, voteKnownKeys[:])
	return p
}

// VoteKnownKeys is a copy of voteKnownKeys, for the census check.
func VoteKnownKeys() []string { return slices.Clone(voteKnownKeys[:]) }
