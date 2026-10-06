package rules

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// manaSAFacts is the configured-text half of the mana walk's per-ability
// gates (appendAvailableManaAbilitiesGate's printed loop), computed once per
// configured AB$ ability when the compiledText is built. Every field is a
// pure function of the ability's own Params (and its compiled cost), so a
// fact that says "this gate has nothing to read" lets the walk skip a
// dozen small-map lookups per ability per object per offer; a gate the
// ability does carry still runs through its ordinary evaluator. An ability
// outside the configured set (a runtime-built SA) has no entry and takes
// every gate the ordinary way. In the rules test binary manaSAFactsVerify
// recomputes the facts on every hit and panics on a difference (the
// "configured text is immutable" argument, checked). It is the mana half of
// the ability's saFacts record (sa_facts.go), which owns the ability's
// published slot.
type manaSAFacts struct {
	// zoneOK is abilityZoneOK(ab, z) for every z < 32 (bit z).
	zoneOK uint32
	// loyalty is isLoyaltyAbility(ab).
	loyalty bool
	// defaultActivator: Activator$ is blank, so activatorAllows is the
	// controller test alone.
	defaultActivator bool
	// noActivation: Activation$ is absent or blank (activationConditionOK
	// is true).
	noActivation bool
	// noPhaseGate: none of the parameters activationPhasesOK reads is
	// present (it is true).
	noPhaseGate bool
	// noIsPresent: IsPresent$ is absent or blank, so manaActivationGateHolds
	// reduces to activationPhasesOK.
	noIsPresent bool
	// noCheckSVar: CheckSVar$ is absent (manaSVarGateOK is true).
	noCheckSVar bool
	// noLimit: neither ActivationLimit$ nor a non-empty GameActivationLimit$
	// is present (the walk's limit block is skipped).
	noLimit bool
	// cost is the ability's compiled Cost$.
	cost *compiledCost
	// plainSym/plainAmt: the ability is a plain AB$ Mana (plainManaShape)
	// adding plainAmt of the one symbol plainSym; plainSym 0 otherwise.
	plainSym byte
	plainAmt int32
	// shapeKnown: the ability carries no SubAbility$, so its payment-plan
	// shape verdict (paymentPlanShapeTierOf) reads only its own Params and
	// cost and is shapeTier/shapeCons/shapeDetail.
	shapeKnown  bool
	shapeTier   pay.Tier
	shapeCons   pay.Consequence
	shapeDetail string
	// static is the payment census's per-ability text reads
	// (manaStaticOf).
	static pay.ManaStatic
	// potential is the ability's PotentialMana production
	// (addPotentialManaOf).
	potential potentialManaAdd
}

func computeManaStaticFacts(mp *effects.ManaParams, cost *Cost) pay.ManaStatic {
	return pay.ManaStatic{Produced: mp.Produced, Amount: availableAmountOf(mp),
		RestrictValid: mp.RestrictValid != "", Counts: mp.Counts, Any: mp.CountsAny,
		FreeCost: pay.ManaFreeCost(*cost), TapOnly: pay.PaymentPlanTapOnlyCost(*cost), Tap: cost.Tap, Untap: cost.Untap}
}

// manaStaticOf is ab's census text reads: its configured facts', or read
// now for an ability outside the configured set.
func (e *Engine) manaStaticOf(ab *cards.SA) pay.ManaStatic {
	if f := e.manaFactsOf(ab); f != nil {
		if manaSAFactsVerify {
			c := e.parseCost(ab.ParamStr(cards.PKCost))
			if fresh := computeManaStaticFacts(effects.ManaOf(ab), &c); fresh != f.static {
				panic(fmt.Sprintf("rules: configured census facts for %q disagree with a recompute", ab.Line))
			}
		}
		return f.static
	}
	c := e.parseCost(ab.ParamStr(cards.PKCost))
	return computeManaStaticFacts(effects.ManaOf(ab), &c)
}

// manaSAFactsVerify: see derivedMemoVerify. Set by the rules test binary.
var manaSAFactsVerify = derivedMemoVerifyFlag != ""

// buildManaSAFactsValue computes ab's mana facts by value from its compiled
// production parameters mp (effects.ManaOf(ab)): buildSAFacts takes its heap
// copy, and the verify-mode recompute in manaFactsOf only compares it.
func buildManaSAFactsValue(ab *cards.SA, mp *effects.ManaParams, costOf func(string) *compiledCost) manaSAFacts {
	f := manaSAFacts{cost: costOf(ab.ParamStr(cards.PKCost))}
	f.zoneOK = abilityZoneMask(ab)
	raw := ab.ParamStr(cards.PKCost)
	if containsLoyaltyFold(raw) {
		f.loyalty = isLoyaltyAbilityRef(ab, &f.cost.Cost)
	} else {
		f.loyalty = isLoyaltyAbilityRef(ab, &freeCost.Cost)
	}
	f.defaultActivator = strings.TrimSpace(ab.ParamStr(cards.PKActivator)) == ""
	if raw, ok := ab.Param(cards.PKActivation); !ok || strings.TrimSpace(raw) == "" {
		f.noActivation = true
	}
	// The five keys activationPhasesOK reads, spelled out so the param
	// census sees static keys.
	_, phases := ab.Param(cards.PKActivationPhases)
	_, firstCombat := ab.Param(cards.PKActivationFirstCombat)
	_, afterBlockers := ab.Param(cards.PKActivationAfterBlockers)
	_, playerTurn := ab.Param(cards.PKPlayerTurn)
	_, opponentTurn := ab.Param(cards.PKOpponentTurn)
	f.noPhaseGate = !phases && !firstCombat && !afterBlockers && !playerTurn && !opponentTurn
	if spec, ok := ab.Param(cards.PKIsPresent); !ok || strings.TrimSpace(spec) == "" {
		f.noIsPresent = true
	}
	_, check := ab.Param(cards.PKCheckSVar)
	f.noCheckSVar = !check
	_, limited := ab.Param(cards.PKActivationLimit)
	f.noLimit = !limited && ab.ParamStr(cards.PKGameActivationLimit) == ""
	f.plainSym, f.plainAmt = plainManaShape(ab, mp)
	f.static = computeManaStaticFacts(mp, &f.cost.Cost)
	f.potential = computePotentialManaAdd(mp)
	if tier, c, detail, rider := pay.PaymentPlanShapeTierOf(ab, f.cost.Cost); !rider {
		f.shapeKnown, f.shapeTier, f.shapeCons, f.shapeDetail = true, tier, c, detail
	}
	return f
}

// manaFactsOf returns ab's configured mana facts (the mana half of its
// saFacts record), or nil for an ability outside the configured set or one
// that is not an AB$ ability.
func (e *Engine) manaFactsOf(ab *cards.SA) *manaSAFacts {
	if e == nil {
		return nil
	}
	sf := e.compiledText.factsOf(ab)
	if sf == nil || sf.Rules == nil {
		return nil
	}
	f := manaHalf(sf)
	if manaSAFactsVerify {
		fresh := buildManaSAFactsValue(ab, effects.ManaOf(ab), e.compiledCostOf)
		if (fresh.cost != f.cost && !sameCompiledCost(fresh.cost, f.cost)) || !sameFactsIgnoringCost(fresh, *f) {
			panic(fmt.Sprintf("rules: configured mana facts for %q disagree with a recompute (%+v vs %+v)", ab.Line, *f, fresh))
		}
	}
	return f
}

// sameFactsIgnoringCost compares two fact sets except the cost pointer
// (compared by identity: a configured text has exactly one compiled cost).
func sameFactsIgnoringCost(a, b manaSAFacts) bool {
	a.cost, b.cost = nil, nil
	return a == b
}

// zoneOKFact is abilityZoneOK through the facts' mask.
func (f *manaSAFacts) zoneOKFact(ab *cards.SA, z state.Zone) bool {
	if z < 32 {
		return f.zoneOK&(1<<z) != 0
	}
	return abilityZoneOK(ab, z)
}

// sameCompiledCost compares two compiled costs' facts (not the memoized text,
// which only one of them may have built yet).
func sameCompiledCost(a, b *compiledCost) bool {
	return reflect.DeepEqual(&a.Cost, &b.Cost) && a.BareTap == b.BareTap && a.BeyondTap == b.BeyondTap
}
