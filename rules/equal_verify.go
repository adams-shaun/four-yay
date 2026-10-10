package rules

import (
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Typed reflect.DeepEqual replacements for the derived-memo verify checks
// (layerInertVerify, walkCacheVerify, castsOnlyWalkVerify,
// staticZoneSkipVerify, layer4PrecheckVerify). Every function keeps
// DeepEqual's semantics exactly: a nil slice or map equals only a nil one,
// and a pointer equals when it is the same address or both pointees are
// equal (the cards IR through cards.FaceEqual/TriggerEqual). The call sites
// keep their own empty-list allowances. equal_verify_test.go holds each
// field list to its struct's, so a field added later fails the tests until
// it is compared here.

const (
	continuousEffectFieldCount = 114
	attackOfferFieldCount      = 4
	blockChargeFieldCount      = 7
	objectTypesFieldCount      = 3
	optionFieldCount           = 42
	grantFieldCount            = 3
)

func sliceEq[T comparable](a, b []T) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return slices.Equal(a, b)
}

func gainedFacesEqual(a, b []state.GainedFace) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Obj != b[i].Obj || !cards.FaceEqual(a[i].Face, b[i].Face) {
			return false
		}
	}
	return true
}

// continuousEffectEqual is reflect.DeepEqual(*a, *b) for two effects.
func continuousEffectEqual(a, b *ContinuousEffect) bool {
	return true &&
		a.Source == b.Source &&
		a.SourceIncarnation == b.SourceIncarnation &&
		a.DurationSource == b.DurationSource &&
		a.Timestamp == b.Timestamp &&
		a.Layer == b.Layer &&
		a.Sub == b.Sub &&
		a.Affects == b.Affects &&
		a.Controller == b.Controller &&
		a.UntilEOT == b.UntilEOT &&
		a.FromEffect == b.FromEffect &&
		cards.StringMapEqual(a.SVars, b.SVars) &&
		a.AddPower == b.AddPower &&
		a.AddToughness == b.AddToughness &&
		a.DoublePower == b.DoublePower &&
		a.DoubleToughness == b.DoubleToughness &&
		a.SetPower == b.SetPower &&
		a.SetToughness == b.SetToughness &&
		a.HasSet == b.HasSet &&
		sliceEq(a.AddKeywords, b.AddKeywords) &&
		sliceEq(a.AddTypes, b.AddTypes) &&
		a.SetName == b.SetName &&
		a.TextFrom == b.TextFrom &&
		a.TextTo == b.TextTo &&
		a.TextSet == b.TextSet &&
		a.TextSetSet == b.TextSetSet &&
		a.AddPowerExpr == b.AddPowerExpr &&
		a.AddToughnessExpr == b.AddToughnessExpr &&
		a.AddPowerAffected == b.AddPowerAffected &&
		a.AddToughnessAffected == b.AddToughnessAffected &&
		a.SetPowerExpr == b.SetPowerExpr &&
		a.SetToughnessExpr == b.SetToughnessExpr &&
		a.SetPowerAffected == b.SetPowerAffected &&
		a.SetToughnessAffected == b.SetToughnessAffected &&
		a.SetPowerPresent == b.SetPowerPresent &&
		a.SetToughnessPresent == b.SetToughnessPresent &&
		a.StaticSet == b.StaticSet &&
		sliceEq(a.AddColors, b.AddColors) &&
		a.OverwriteColors == b.OverwriteColors &&
		a.RemoveCreatureTypes == b.RemoveCreatureTypes &&
		a.RemoveSubTypes == b.RemoveSubTypes &&
		sliceEq(a.RemoveTypes, b.RemoveTypes) &&
		a.SetCreatureTypes == b.SetCreatureTypes &&
		a.AddAllCreatureTypes == b.AddAllCreatureTypes &&
		a.CDAAllCreatureTypes == b.CDAAllCreatureTypes &&
		a.RemoveCardTypes == b.RemoveCardTypes &&
		a.RemoveLandTypes == b.RemoveLandTypes &&
		a.RemoveLegendary == b.RemoveLegendary &&
		sliceEq(a.AddAbilities, b.AddAbilities) &&
		gainedFacesEqual(a.GainedFaces, b.GainedFaces) &&
		gainedFacesEqual(a.GainedTriggerFaces, b.GainedTriggerFaces) &&
		a.GainsValidAbilities == b.GainsValidAbilities &&
		a.GainsLimitPerTurn == b.GainsLimitPerTurn &&
		a.GainedZones == b.GainedZones &&
		a.Restriction == b.Restriction &&
		cards.StringMapEqual(a.RestrictParams, b.RestrictParams) &&
		cards.StringMapEqual(a.RestrictSVars, b.RestrictSVars) &&
		a.Name == b.Name &&
		sliceEq(a.Remembered, b.Remembered) &&
		sliceEq(a.RememberedPlayers, b.RememberedPlayers) &&
		sliceEq(a.Chosen, b.Chosen) &&
		a.ChosenBound == b.ChosenBound &&
		a.Duration == b.Duration &&
		a.Permanent == b.Permanent &&
		a.ReplacementEvent == b.ReplacementEvent &&
		cards.StringMapEqual(a.ReplacementParams, b.ReplacementParams) &&
		a.ReplacementBody == b.ReplacementBody &&
		a.ChosenNumber == b.ChosenNumber &&
		a.RemoveAbilities == b.RemoveAbilities &&
		a.SourceAbilityLayer == b.SourceAbilityLayer &&
		sliceEq(a.RemoveKeywords, b.RemoveKeywords) &&
		sliceEq(a.CantHaveKeywords, b.CantHaveKeywords) &&
		a.MayPlay == b.MayPlay &&
		a.AffectedZone == b.AffectedZone &&
		a.SetMaxHandSize == b.SetMaxHandSize &&
		a.MayPlayIgnoreColor == b.MayPlayIgnoreColor &&
		a.MayPlayLimit == b.MayPlayLimit &&
		sliceEq(a.ShieldTargets, b.ShieldTargets) &&
		sliceEq(a.ShieldTargetPlayers, b.ShieldTargetPlayers) &&
		a.MayPlayPlayerTurn == b.MayPlayPlayerTurn &&
		a.MayPlayFree == b.MayPlayFree &&
		a.MayPlayValidAfterStack == b.MayPlayValidAfterStack &&
		cards.TriggerEqual(a.AddTrigger, b.AddTrigger) &&
		a.TriggerGrantor == b.TriggerGrantor &&
		a.AbilityGrantor == b.AbilityGrantor &&
		cards.StringMapEqual(a.AddSVars, b.AddSVars) &&
		a.GainControl == b.GainControl &&
		a.MayLookAt == b.MayLookAt &&
		a.MayPlayIgnoreType == b.MayPlayIgnoreType &&
		a.ForgetOnMoved == b.ForgetOnMoved &&
		a.ExileOnMoved == b.ExileOnMoved &&
		a.ExileOnMovedAlso == b.ExileOnMovedAlso &&
		a.ImprintOnHost == b.ImprintOnHost &&
		a.ForgetCounter == b.ForgetCounter &&
		a.ForgetOnCast == b.ForgetOnCast &&
		a.MustBlockAttacker == b.MustBlockAttacker &&
		a.MustBlockAllAttackers == b.MustBlockAllAttackers &&
		a.AssignmentStaticMode == b.AssignmentStaticMode &&
		cards.StringMapEqual(a.AssignmentStaticParams, b.AssignmentStaticParams) &&
		cards.StringMapEqual(a.AssignmentStaticSVars, b.AssignmentStaticSVars) &&
		a.CostStaticMode == b.CostStaticMode &&
		cards.StringMapEqual(a.CostStaticSVars, b.CostStaticSVars) &&
		cards.StringMapEqual(a.CostStaticParams, b.CostStaticParams) &&
		a.CostStaticGranted == b.CostStaticGranted &&
		a.AdjustLandPlays == b.AdjustLandPlays &&
		a.CloneTarget == b.CloneTarget &&
		a.CloneDurationTarget == b.CloneDurationTarget &&
		a.CloneSource == b.CloneSource &&
		a.CloneName == b.CloneName &&
		a.CloneChosenName == b.CloneChosenName &&
		sliceEq(a.CloneStaticBodies, b.CloneStaticBodies) &&
		a.CloneGainThisAbility == b.CloneGainThisAbility &&
		a.CloneAbilityIndex == b.CloneAbilityIndex &&
		a.CloneTriggerIndex == b.CloneTriggerIndex &&
		a.UntilTurn == b.UntilTurn
}

// continuousEffectsEqual is reflect.DeepEqual(a, b) for two effect lists.
func continuousEffectsEqual(a, b []ContinuousEffect) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if !continuousEffectEqual(&a[i], &b[i]) {
			return false
		}
	}
	return true
}

// attackOffersEqual is reflect.DeepEqual(a, b) for two attack offer lists.
func attackOffersEqual(a, b []attackOffer) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := &a[i], &b[i]
		if x.id != y.id || x.def != y.def || x.battle != y.battle ||
			x.charge.mana != y.charge.mana || x.charge.life != y.charge.life || x.charge.unpriceable != y.charge.unpriceable ||
			!sliceEq(x.charge.taps, y.charge.taps) || !sliceEq(x.charge.sacs, y.charge.sacs) ||
			!sliceEq(x.charge.returns, y.charge.returns) || !sliceEq(x.charge.phyrexian, y.charge.phyrexian) {
			return false
		}
	}
	return true
}

// objectTypesListEqual is reflect.DeepEqual(a, b) for two layer-4 tables.
func objectTypesListEqual(a, b []effects.ObjectTypes) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].AllCreatureTypes != b[i].AllCreatureTypes || !sliceEq(a[i].Types, b[i].Types) {
			return false
		}
	}
	return true
}

// optionEqual is reflect.DeepEqual(*a, *b) for two decision options.
func optionEqual(a, b *decision.Option) bool {
	if a.Grant != b.Grant {
		if a.Grant == nil || b.Grant == nil || a.Grant.Already != b.Grant.Already ||
			a.Grant.Duplicate != b.Grant.Duplicate || !sliceEq(a.Grant.Keywords, b.Grant.Keywords) {
			return false
		}
	}
	return true &&
		a.Index == b.Index &&
		a.Kind == b.Kind &&
		a.Label == b.Label &&
		a.Obj == b.Obj &&
		a.ManaSymbol == b.ManaSymbol &&
		a.Counter == b.Counter &&
		a.Player == b.Player &&
		a.Attacker == b.Attacker &&
		a.Battle == b.Battle &&
		a.Required == b.Required &&
		a.BlockMust == b.BlockMust &&
		a.BlockMustAll == b.BlockMustAll &&
		a.AttackMust == b.AttackMust &&
		a.MinBlockers == b.MinBlockers &&
		a.MaxBlockers == b.MaxBlockers &&
		a.Controller == b.Controller &&
		a.Group == b.Group &&
		sliceEq(a.SetProps, b.SetProps) &&
		a.AltCostIndex == b.AltCostIndex &&
		a.CostLife == b.CostLife &&
		a.CostTaps == b.CostTaps &&
		a.CostPhyrexian == b.CostPhyrexian &&
		a.TapPoolCost == b.TapPoolCost &&
		a.Mode == b.Mode &&
		a.MayPlayPerm == b.MayPlayPerm &&
		a.Key == b.Key &&
		a.Amount == b.Amount &&
		a.Ability == b.Ability &&
		a.SVar == b.SVar &&
		a.Keyword == b.Keyword &&
		a.Cost == b.Cost &&
		a.Attach == b.Attach &&
		a.SelfSkipTurns == b.SelfSkipTurns &&
		a.ForeignSource == b.ForeignSource &&
		a.GrantSource == b.GrantSource &&
		a.GainedSource == b.GainedSource &&
		a.GainedIdx == b.GainedIdx &&
		sliceEq(a.GrantStatics, b.GrantStatics) &&
		a.PlanBacked == b.PlanBacked &&
		a.Value == b.Value &&
		a.Value2 == b.Value2
}

// optionsEqual is reflect.DeepEqual(a, b) for two option lists.
func optionsEqual(a, b []decision.Option) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		if !optionEqual(&a[i], &b[i]) {
			return false
		}
	}
	return true
}
