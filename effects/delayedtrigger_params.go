package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is api:DelayedTrigger's parameter compiler (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8), on ChangeZone's pattern (changezone_params.go). compileDelayedTrigger is
// the ONLY reader of a DelayedTrigger ability's parameters: the resolution
// (delayed_trigger.go) reads the compiled DelayedTriggerParams, including the
// registration text each mode emits, which is a pure function of the
// ability's text and so is built here once instead of per resolution.
// internal/codeshape's delayedTriggerParamLeaks ratchet holds that: no
// parameter read in delayed_trigger.go, and no read of a DelayedTrigger-only
// key anywhere else in rules/ or effects/.

// DelayedTriggerParams is one DelayedTrigger ability's parameters, compiled
// once.
type DelayedTriggerParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Mode is Mode$, trimmed.
	Mode string
	// Phase is Phase$ as the script spells it (the phase parse and the
	// unrecognised-phase Note read it untrimmed).
	Phase string
	// PhaseText is the Mode$ Phase registration's event Text: Phase$ plus
	// the "|VP=" ValidPlayer$ and "|IP=/|PZ=/|PC=" IsPresent$/PresentZone$/
	// PresentCompare$ suffixes rules' delayed scan decodes.
	PhaseText string
	// EventText is an event-matched registration's Text before the ThisTurn$
	// expiry suffix: "<Mode>:" plus the inline trigger body
	// (delayedTriggerBody's fixed key order).
	EventText string
	// SpellCastText is the Mode$ SpellCast registration's Text before the
	// ThisTurn$ suffix.
	SpellCastText string
	// RememberObjects is RememberObjects$, trimmed.
	RememberObjects string
	// NextTurn is NextTurn$ True.
	NextTurn bool
	// RememberChainFalse is RememberChain$ False.
	RememberChainFalse bool
	// Execute is Execute$, trimmed.
	Execute string
	// ThisTurn is ThisTurn$ True.
	ThisTurn bool
	// Static is Static$, trimmed.
	Static string

	// Unread are the parameters present on the ability that no
	// DelayedTrigger reader consumes: the resolution Notes them once.
	Unread []string
}

// delayedTriggerKnownKeys is every parameter key a DelayedTrigger resolution
// consumes or deliberately ignores, sorted (changeZoneKnownKeys' contract):
// rules' TestDelayedTriggerKnownKeysMatchTheCensus holds it equal to the
// parameter census's measured api:DelayedTrigger read set plus its ignored
// and structural keys.
var delayedTriggerKnownKeys = [...]string{
	"AILifeThreshold", "AILogic", "AINoRecursiveCheck", "AIPhyrexianPayment",
	"AITgts", "Activation", "ActivationAfterBlockers", "ActivationFirstCombat",
	"ActivationGameTypes", "ActivationLimit", "ActivationPhases", "ActivationZone",
	"Activator", "ActiveZones", "AddKeywords", "AddStaticAbilities", "AddType",
	"AddTypes", "AdditionalDesc", "AdditionalDescription", "Affected",
	"AlternateCost", "AlternativeCost", "Announce", "AnnounceTitle", "AttackedTarget",
	"AttackingPlayer", "Boast", "ChangeTypeDesc", "CharacteristicDefining",
	"CheckSVar", "ChoiceTitle", "ChoiceZone", "Choices", "ChooseFromList",
	"ClassBand", "ClearImprinted", "CombatDamage", "Condition",
	"ConditionActivationLimit", "ConditionCheckSVar", "ConditionCompare",
	"ConditionDefined", "ConditionDescription", "ConditionFirstCombat",
	"ConditionNotPresent", "ConditionPhases", "ConditionPlayerTurn",
	"ConditionPresent", "ConditionSVarCompare", "CopyCard", "Cost", "CostDesc",
	"Defined", "DefinedCards", "DefinedTarget", "Description", "Destination",
	"Exclude", "ExcludedOrigins", "Execute", "Exhaust", "GameActivationLimit",
	"Image", "ImprintCards", "ImprintPlayed", "InstantSpeed", "IntoPlayTapped",
	"IsCurse", "IsPresent", "KW", "Keyword", "KeywordLine", "MaxTotalTargetCMC",
	"MaxTotalTargetPower", "Mentor", "Mode", "ModeCost", "Monstrosity",
	"NewController", "NextTurn", "NumDmg", "OpponentTurn", "Origin", "Phase",
	"Planeswalker", "PlayCost", "PlayerTurn", "PowerUp", "PrecostDesc",
	"PresentCompare", "PresentDefined", "PresentZone", "ReduceAmount", "ReduceCost",
	"RememberChain", "RememberCostMana", "RememberObjects", "ReplaceColor",
	"ReplaceGraveyard", "ReplaceGraveyardValid", "ReplaceMana", "ReplaceOnly",
	"ReplaceType", "SVarCompare", "SelectPrompt", "SetChosenMode", "SetColor",
	"ShowCards", "SorcerySpeed", "SpellDescription", "StackDescription", "Static",
	"SubAbility", "TargetMax", "TargetMin", "TargetType", "TargetUnique",
	"TargetValidTargeting", "TargetingPlayer", "TargetingPlayerControls",
	"TargetsForEachPlayer", "TargetsWithControllerProperty",
	"TargetsWithDefinedController", "TargetsWithDifferentCMC",
	"TargetsWithDifferentControllers", "TargetsWithDifferentNames",
	"TargetsWithEqualToughness", "TargetsWithSameCardType",
	"TargetsWithSameController", "TargetsWithSameCreatureType",
	"TargetsWithSharedCardType", "TargetsWithSharedTypes", "TgtPrompt", "TgtZone",
	"ThisTurn", "TokenScript", "TriggerDescription", "TriggerZones", "Type",
	"Ultimate", "UnlessAI", "UnlessCost", "UnlessPayer", "UnlessResolveSubs",
	"UnlessSwitched", "ValidActivatingPlayer", "ValidAttackers",
	"ValidAttackersAmount", "ValidCard", "ValidCards", "ValidCardsDesc",
	"ValidChoices", "ValidCounterType", "ValidDescription", "ValidOriginalController",
	"ValidPlayer", "ValidSA", "ValidSource", "ValidTarget", "ValidTgts",
	"VoteMessage", "WithoutManaCost", "XMax", "XMin",
}

// isDelayedTriggerSA reports whether sa resolves as api:DelayedTrigger.
func isDelayedTriggerSA(sa *cards.SA) bool {
	return sa != nil && (sa.CompiledAPI() == cards.APIDelayedTrigger || sa.API == "DelayedTrigger")
}

// DelayedTriggerOf returns sa's compiled DelayedTrigger parameters: the
// configured record's when it is bound to sa's Params map, else the front
// cache's entry for that map, else a fresh compile stored in the front cache
// (ChangeZoneOf's contract).
func DelayedTriggerOf(sa *cards.SA) *DelayedTriggerParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.DelayedTrigger; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &delayedTriggerFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDelayedTrigger(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// delayedTriggerFront is DelayedTriggerOf's direct-mapped front cache
// (czFront's shape).
var delayedTriggerFront [1 << 10]atomic.Pointer[DelayedTriggerParams]

// compileDelayedTrigger is the one reader of a DelayedTrigger ability's
// parameters.
func compileDelayedTrigger(sa *cards.SA) *DelayedTriggerParams {
	p := &DelayedTriggerParams{paramBinding: bindParams(sa)}
	p.Mode = strings.TrimSpace(sa.ParamStr(cards.PKMode))
	p.Phase = sa.ParamStr(cards.PKPhase)
	p.RememberObjects = strings.TrimSpace(sa.ParamStr(cards.PKRememberObjects))
	p.NextTurn = isTrue(sa.Params["NextTurn"])
	p.RememberChainFalse = strings.EqualFold(strings.TrimSpace(sa.Params["RememberChain"]), "False")
	p.Execute = strings.TrimSpace(sa.ParamStr(cards.PKExecute))
	p.ThisTurn = isTrue(sa.ParamStr(cards.PKThisTurn))
	p.Static = strings.TrimSpace(sa.ParamStr(cards.PKStatic))

	// The Phase registration's Text (see effDelayedTrigger).
	text := p.Phase
	if vp := strings.TrimSpace(sa.ParamStr(cards.PKValidPlayer)); vp != "" {
		text += "|VP=" + vp
	}
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKIsPresent)); spec != "" {
		text += "|IP=" + spec
		if zone := strings.TrimSpace(sa.ParamStr(cards.PKPresentZone)); zone != "" {
			text += "|PZ=" + zone
		}
		if cmp := strings.TrimSpace(sa.ParamStr(cards.PKPresentCompare)); cmp != "" {
			text += "|PC=" + cmp
		}
	}
	p.PhaseText = text
	p.EventText = p.Mode + ":" + delayedTriggerBody(sa)
	p.SpellCastText = "SpellCast:" + delayedSpellCastBody(sa)
	p.Unread = unreadKeys(sa, delayedTriggerKnownKeys[:])
	return p
}

// delayedTriggerBody serializes the trigger parameters in a fixed order. A
// map iteration here would make the event bytes (and therefore replay heads)
// nondeterministic.
func delayedTriggerBody(sa *cards.SA) string {
	// Literal keys at both the read and append sites keep the parameter census
	// attributable; call order fixes the registration's replay-visible bytes.
	parts := []string{"Mode$ " + strings.TrimSpace(sa.ParamStr(cards.PKMode))}
	add := func(prefix, value string) {
		if v := strings.TrimSpace(value); v != "" {
			parts = append(parts, prefix+v)
		}
	}
	add("ValidCard$ ", sa.ParamStr(cards.PKValidCard))
	add("ValidCards$ ", sa.ParamStr(cards.PKValidCards))
	add("Origin$ ", sa.ParamStr(cards.PKOrigin))
	add("Destination$ ", sa.ParamStr(cards.PKDestination))
	add("ExcludedOrigins$ ", sa.ParamStr(cards.PKExcludedOrigins))
	add("ValidSource$ ", sa.ParamStr(cards.PKValidSource))
	add("ValidTarget$ ", sa.ParamStr(cards.PKValidTarget))
	add("CombatDamage$ ", sa.ParamStr(cards.PKCombatDamage))
	add("ValidAttackers$ ", sa.ParamStr(cards.PKValidAttackers))
	add("ValidAttackersAmount$ ", sa.ParamStr(cards.PKValidAttackersAmount))
	add("AttackingPlayer$ ", sa.ParamStr(cards.PKAttackingPlayer))
	add("AttackedTarget$ ", sa.ParamStr(cards.PKAttackedTarget))
	add("ValidPlayer$ ", sa.ParamStr(cards.PKValidPlayer))
	add("ValidOriginalController$ ", sa.Params["ValidOriginalController"])
	add("ValidActivatingPlayer$ ", sa.ParamStr(cards.PKValidActivatingPlayer))
	add("PlayerTurn$ ", sa.ParamStr(cards.PKPlayerTurn))
	add("ValidSA$ ", sa.ParamStr(cards.PKValidSA))
	add("TriggerZones$ ", sa.ParamStr(cards.PKTriggerZones))
	add("ActiveZones$ ", sa.ParamStr(cards.PKActiveZones))
	add("ThisTurn$ ", sa.ParamStr(cards.PKThisTurn))
	add("Static$ ", sa.ParamStr(cards.PKStatic))
	add("IsPresent$ ", sa.ParamStr(cards.PKIsPresent))
	add("PresentDefined$ ", sa.ParamStr(cards.PKPresentDefined))
	add("PresentCompare$ ", sa.ParamStr(cards.PKPresentCompare))
	add("PresentZone$ ", sa.ParamStr(cards.PKPresentZone))
	return strings.Join(parts, " | ")
}

// delayedSpellCastBody is the Mode$ SpellCast registration's inline body
// (effDelayedTriggerSpellCast). Each clause is unrolled over its explicit
// key: the census's rot guard refuses a dynamic Params key it cannot
// attribute, and explicit reads cannot hide one. Static$ rides the body too
// -- Forge's static delayed trigger resolves its Execute IMMEDIATELY at fire
// time (no stack push), which is what makes Mistrise's promise active before
// the opponent can respond.
func delayedSpellCastBody(sa *cards.SA) string {
	body := "Mode$ SpellCast"
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidCard)); v != "" {
		body += " | ValidCard$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidActivatingPlayer)); v != "" {
		body += " | ValidActivatingPlayer$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidPlayer)); v != "" {
		body += " | ValidPlayer$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKPlayerTurn)); v != "" {
		body += " | PlayerTurn$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKValidSA)); v != "" {
		body += " | ValidSA$ " + v
	}
	if v := strings.TrimSpace(sa.ParamStr(cards.PKStatic)); v != "" {
		body += " | Static$ " + v
	}
	return body
}

// DelayedTriggerKnownKeys is a copy of delayedTriggerKnownKeys, for the
// census check.
func DelayedTriggerKnownKeys() []string { return slices.Clone(delayedTriggerKnownKeys[:]) }
