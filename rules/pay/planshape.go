package pay

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects/params"
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// CastWindowOtherPartsAbsent reports whether c carries none of the cost
// components the paid-cost layer does not price. It is deliberately broader
// than manaFreeCost (which only needs a bare tap): every part whose payment
// needs a choice, an event or a resource this probe does not model is
// refused, as is any token the parser did not understand.
func CastWindowOtherPartsAbsent(c costvocab.Cost) bool {
	return len(c.Discard) == 0 && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.ExileFromTop) == 0 && len(c.Reveal) == 0 && len(c.RevealOrChoose) == 0 && len(c.RevealChosen) == 0 &&
		len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.Blight) == 0 &&
		len(c.Exert) == 0 && !c.Forage && !c.LifeHalfUp && len(c.Draw) == 0 &&
		len(c.Energy) == 0 && len(c.LifeX) == 0 && len(c.DamageYou) == 0 &&
		len(c.GainLife) == 0 &&
		len(c.Return) == 0 && len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 &&
		len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 &&
		len(c.Unknown) == 0 && len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 &&
		len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 && c.Snow == 0 && c.X == 0
}

// AlternativeExec is the ability the executor activates for a: ma itself
// for fixed production, else ma with its Produced$ rewritten to the
// selected colour.
func AlternativeExec(a Alt) *cards.SA {
	if a.ExecProduced == "" {
		return a.Ma
	}
	return WithProduced(a.Ma, a.Ma, a.ExecProduced)
}

// PaymentPlanTapOnlyCost is the V1 source contract.  A payment witness can
// record and replay the source, ability and mana result, but it intentionally
// carries no representation for an additional activation cost.  Require an
// actual tap and reject every parsed or unknown companion cost, including
// Mill<N>, even where the ordinary manual mana window can pay it without a
// further choice.
func PaymentPlanTapOnlyCost(c costvocab.Cost) bool {
	return c.Tap && c.XMin == 0 && ManaFreeCost(c) && CastWindowOtherPartsAbsent(c)
}

// PaymentPlanShapeTierOf is paymentPlanAbilityShapeTier's source-independent
// part over ma's Params and its parsed cost: the verdict, or rider true when
// ma carries a SubAbility$ whose chain (read off the source's face) decides
// it.
func PaymentPlanShapeTierOf(ma *cards.SA, cost costvocab.Cost) (tier Tier, c Consequence, detail string, rider bool) {
	deferred := func(detail string) (Tier, Consequence, string, bool) {
		return TierDeferred, Consequence{}, detail, false
	}
	if ma == nil || ma.API != "Mana" {
		return deferred("source:special_production")
	}
	// The key loop carries the key NAMES only (never a Params value) into
	// the prefix checks below; slices.Sorted(maps.Keys) keeps it a plain
	// string-slice walk rather than a range the param census would have to
	// classify.
	//
	// The verdict is the one the sorted walk reaches first: the smallest key
	// failing any of the three checks decides it. Taking that minimum over
	// the unsorted keys is order-independent (so deterministic) and spares
	// the sorted copy.
	failing, found := "", false
	for key := range maps.Keys(ma.Params) {
		if (!found || key < failing) && paymentPlanShapeKeyFails(key) {
			failing, found = key, true
		}
	}
	if found {
		key := failing
		if strings.HasPrefix(key, "Condition") {
			return deferred("source:conditional")
		}
		if ContainsTargetFold(key) || key == "ValidTgts" || key == "ValidTarget" {
			return deferred("source:target")
		}
		return deferred("source:param:" + key)
	}
	if params.ManaOf(ma).RestrictValid != "" {
		return deferred("source:special_production")
	}
	if PlanHasSpecialProductionParam(ma) {
		return deferred("source:special_production")
	}
	if cost.XMin != 0 || cost.X != 0 || cost.Generic != 0 || cost.Colored.Total() != 0 || len(cost.Discard)+len(cost.SubCounter)+len(cost.Exile)+len(cost.ExileFromTop)+len(cost.TapPermanent)+len(cost.Energy)+len(cost.LifeX) != 0 {
		return deferred("source:last_resort")
	}
	if strings.TrimSpace(ma.ParamStr(cards.PKSubAbility)) != "" {
		return TierDeferred, Consequence{}, "", true
	}
	if cost.Sac != nil || cost.Life != 0 || cost.Return != nil {
		// Every other cost part must be absent: the witness discloses only
		// the tap, the self-sacrifice, the life and the self-return.
		if !PlanLastResortCostOK(cost) {
			return deferred("source:last_resort")
		}
		if len(cost.Sac) > 0 && len(cost.Sac) == 1 && PlanSelfCost(cost.Sac[0], 0) {
			c.Sacrifice = true
		} else if len(cost.Sac) > 0 {
			return deferred("source:last_resort")
		}
		if cost.Life > 0 {
			c.Life = uint32(cost.Life)
		}
		if len(cost.Return) > 0 && len(cost.Return) == 1 && PlanSelfCost(cost.Return[0], 0) {
			c.ReturnToHand = true
		} else if len(cost.Return) > 0 {
			return deferred("source:last_resort")
		}
		return TierLastResort, c, "source:last_resort", false
	}
	if !PaymentPlanTapOnlyCost(cost) {
		return deferred("source:last_resort")
	}
	return TierNormal, Consequence{}, "", false
}

// paymentPlanShapeKeyFails reports whether key alone defers the ability in
// paymentPlanAbilityShapeTier's key walk (a Condition key, a target key or
// an unreviewed parameter).
func paymentPlanShapeKeyFails(key string) bool {
	return strings.HasPrefix(key, "Condition") || ContainsTargetFold(key) || key == "ValidTgts" || key == "ValidTarget" ||
		!paymentPlanKnownManaParam(key)
}

func paymentPlanKnownManaParam(key string) bool {
	if strings.HasPrefix(key, "AddsKeywords") {
		return true
	}
	return paymentPlanKnownManaParamSet.Has(key)
}

func PaymentPlanDamageRider(e Engine, id state.ObjID, mana *cards.SA) (uint32, bool) {
	o := e.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return 0, false
	}
	return paymentPlanDamageBody(cards.ResolveSVar(o.Face().SVars, strings.TrimSpace(mana.ParamStr(cards.PKSubAbility))))
}

// paymentPlanDamageBody reports N when rider is exactly `DealDamage |
// Defined$ You | NumDmg$ <literal N>` with no further parameter or sub.
func paymentPlanDamageBody(rider *cards.SA) (uint32, bool) {
	if rider == nil || rider.API != "DealDamage" || !params.DefinedRefOf(rider).Is(params.RefYou) || strings.TrimSpace(rider.ParamStr(cards.PKSubAbility)) != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(params.DealDamageOf(rider).NumDmg.Text), 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	for _, k := range slices.Sorted(maps.Keys(rider.Params)) {
		if k != "API" && k != "Defined" && k != "NumDmg" && k != "SpellDescription" && k != "StackDescription" {
			return 0, false
		}
	}
	return uint32(n), true
}

func PaymentPlanParadiseRider(e Engine, id state.ObjID, mana *cards.SA) bool {
	o := e.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return false
	}
	rider := cards.ResolveSVar(o.Face().SVars, strings.TrimSpace(mana.ParamStr(cards.PKSubAbility)))
	if rider == nil || rider.API != "Pump" || params.DefinedRefOf(rider).Raw != "Self" {
		return false
	}
	for _, key := range slices.Sorted(maps.Keys(rider.Params)) {
		if key != "API" && key != "Defined" && key != "KW" && key != "Duration" && key != "SpellDescription" && key != "StackDescription" {
			return false
		}
	}
	text := strings.ToLower(strings.Join([]string{rider.ParamStr(cards.PKKW), rider.ParamStr(cards.PKSpellDescription), rider.ParamStr(cards.PKStackDescription)}, " "))
	return strings.Contains(text, "hidden") && strings.Contains(text, "return")
}

// PaymentPlanChoiceShape reports whether a Produced$ value is one of the
// finite choice shapes V1 can plan: the literal Any, a bare Chosen/
// ChosenColor, or a Combo list. It is deliberately about the SHAPE only;
// whether the choice resolves to any colour (an unrecorded Chosen, a Combo
// naming no plain colour, an empty commander identity) is
// paymentPlanChoiceColours' fail-closed answer, not this predicate's.
func PaymentPlanChoiceShape(raw string) bool {
	if paymentPlanChoiceShapeSet.Has(raw) {
		return true
	}
	return strings.HasPrefix(raw, "Combo ")
}

// PaymentSourceZoneSeqScan is the reference answer: it deliberately scans
// backwards so a later incarnation cannot be authorized by a witness made for
// an earlier visit to the battlefield.
func PaymentSourceZoneSeqScan(e Engine, id state.ObjID) uint64 {
	o := e.Game().Obj(id)
	if o == nil {
		return decision.GenesisZoneSeq
	}
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Obj != id {
			continue
		}
		if ev.Kind == events.MoveZone && ev.To == o.Zone {
			return ev.Seq
		}
		if ev.Kind == events.TokenCreate && o.Zone == state.ZBattlefield {
			return ev.Seq
		}
	}
	return decision.GenesisZoneSeq
}

var paymentPlanKnownManaParamSet = state.NewNameSet(
	"API",
	"Cost",
	"Produced",
	"Amount",
	"SubAbility",
	"SpellDescription",
	"StackDescription",
	"AILogic",
	"PrecostDesc",
	"Activation",
	"Activator",
	"ActivationPhases",
	"PlayerTurn",
	"OpponentTurn",
	"ActivationFirstCombat",
	"ActivationAfterBlockers",
	"IsPresent",
	"PresentCompare",
	"CheckSVar",
	"SVarCompare",
	"ActivationLimit",
	"GameActivationLimit",
	"InstantSpeed",
	"RestrictValid",
	"TriggersWhenSpent",
	"AddsCounters",
	"AddsKeywords",
	"AddsKeywordsAll",
	"AddsNoCounter",
	"PersistentMana",
	"UnlessCost",
	"Defined",
)

var paymentPlanChoiceShapeSet = state.NewNameSet(
	"Chosen",
	"ChosenColor",
	"ComboChosen",
)

// PaymentPlanConvRestricts classifies a payer's effective conversion set
// (paymentConv, the payment path's own parse of every ManaConvert static
// reaching the payer) by whether it can make a planned payment INVALID.
//
//   - wild / wildC ("spend mana as though it were mana of any color/type":
//     Mycosynth Lattice, Chromatic Orrery, the AnyType->AnyColor family) and
//     to ("White->Red") only ever ADD pips a unit of mana may pay. A witness
//     the ordinary solver proved payable without them stays payable with
//     them, so they are not a global plan-blocker.
//   - onlyC ("you may spend other mana only as though it were colorless
//     mana": Celestial Dawn's nonWhite<-C) REMOVES pips a unit may pay, so a
//     plan priced without it can be unpayable: global.
//
// Every manaConv field is classified here; a field added to manaConv later
// must be classified too. A ManaConversion$ token the parser cannot read is
// inert at payment as well (manaColourFrom/applyManaConversionTo), so it can
// invalidate nothing the solver priced.
func PaymentPlanConvRestricts(c *Conv) bool {
	return slices.Contains(c.OnlyC[:], true)
}

// PaymentPlanObjName is an object's current face name, for diagnostics.
func PaymentPlanObjName(e Engine, id state.ObjID) string {
	if o := e.Game().Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return "object " + strconv.Itoa(int(id))
}

// PaymentPlanNoUntapShape is spec §3.2's no_untap row: exactly the source's
// own "doesn't untap during your untap step" -- ValidCard$ Card.Self, Layer$
// CantHappen, no ReplaceWith$ body, an optional ValidStepTurnToController$
// You and presentation/zone keys only. Anything else is not a fully
// determined consequence.
func PaymentPlanNoUntapShape(r cards.Repl) bool {
	if r.With != nil || strings.TrimSpace(r.ParamStr(cards.PKValidCard)) != "Card.Self" ||
		!strings.EqualFold(strings.TrimSpace(r.ParamStr(cards.PKLayer)), "CantHappen") {
		return false
	}
	if v, ok := r.Param(cards.PKValidStepTurnToController); ok && strings.TrimSpace(v) != "You" {
		return false
	}
	if v, ok := r.Param(cards.PKActiveZones); ok && strings.TrimSpace(v) != "Battlefield" {
		return false
	}
	for _, k := range slices.Sorted(maps.Keys(r.Params)) {
		if !paymentPlanNoUntapShapeKeys.Has(k) {
			return false
		}
	}
	return true
}

// PaymentPlanSelfDamageTrigger is spec §3.2's damage:N trigger row: the
// source's own `Mode$ Taps | ValidCard$ Card.Self` whose effect is exactly
// `DealDamage | Defined$ You | NumDmg$ <literal>` and nothing else (City of
// Brass). f owns the trigger's Execute$ table.
func PaymentPlanSelfDamageTrigger(f *cards.Face, t cards.Trigger) (uint32, bool) {
	if t.Mode != "Taps" || strings.TrimSpace(t.ParamStr(cards.PKValidCard)) != "Card.Self" {
		return 0, false
	}
	if v, ok := t.Param(cards.PKTriggerZones); ok && strings.TrimSpace(v) != "Battlefield" {
		return 0, false
	}
	for _, k := range slices.Sorted(maps.Keys(t.Params)) {
		switch paymentPlanSelfDamageTriggerCodes.Code(string(k)) {
		case paymentPlanSelfDamageTriggerKnown:
		default:
			return 0, false
		}
	}
	body := t.Effect
	if body == nil && f != nil {
		body = cards.ResolveSVar(f.SVars, strings.TrimSpace(t.ParamStr(cards.PKExecute)))
	}
	return paymentPlanDamageBody(body)
}

// PaymentPlanAlive mirrors the engine walks' AliveFrom scope: an object in a
// lost player's zones is not walked for triggers or replacements.
func PaymentPlanAlive(e Engine, o *state.Object) bool {
	holder := o.Owner
	if o.Zone == state.ZBattlefield {
		holder = o.Controller
	}
	return int(holder) < len(e.Game().Players) && !e.Game().Players[holder].Lost
}

// FaceGrantsSunburstForPlan reports whether f carries a keyword GRANT naming
// Sunburst: a Keywords$/KW$/AddKeyword$ value in any ability, trigger body,
// replacement body, SVar or static. The face's own printed keywords are not
// grants.
func FaceGrantsSunburstForPlan(f *cards.Face) bool {
	// Cheap gate first: nearly every face never mentions the word.
	if f == nil || !f.Mentions("Sunburst") {
		return false
	}
	grantKey := func(k string) bool {
		return k == "Keywords" || k == "KW" || k == "AddKeyword" || k == "AddKeywords"
	}
	grantValue := func(v string) bool {
		for kw := range strings.SplitSeq(v, "&") {
			if strings.HasPrefix(strings.TrimSpace(kw), "Sunburst") {
				return true
			}
		}
		return false
	}
	grants := func(params map[string]string) bool {
		for _, k := range [...]string{"Keywords", "KW", "AddKeyword", "AddKeywords"} {
			if grantValue(params[k]) {
				return true
			}
		}
		return false
	}
	var walk func(sa *cards.SA, depth int) bool
	walk = func(sa *cards.SA, depth int) bool {
		return sa != nil && depth <= 32 && (grants(sa.Params) || walk(sa.Sub, depth+1))
	}
	for _, a := range f.Abilities {
		if walk(a, 0) {
			return true
		}
	}
	for _, t := range f.Triggers {
		if walk(t.Effect, 0) {
			return true
		}
	}
	for _, r := range f.Repls {
		if walk(r.With, 0) {
			return true
		}
	}
	for _, st := range f.Statics {
		if grants(st.Params) {
			return true
		}
	}
	// SVar bodies are raw `Key$ Value | ...` text: read the grant keys off
	// their segments without parsing whole abilities. The answer is a plain
	// any-of, so the map's iteration order cannot matter.
	for _, raw := range f.SVars {
		for seg := range strings.SplitSeq(raw, "|") {
			if k, v, ok := strings.Cut(strings.TrimSpace(seg), "$"); ok && grantKey(strings.TrimSpace(k)) && grantValue(v) {
				return true
			}
		}
	}
	return false
}

// PaymentPlanSpellTargets reports whether casting f's ordinary spell
// announces a target (CR 601.2c): an Aura spell (CR 303.4a), a mutating
// creature spell (CR 702.140a), or a spell ability whose chain -- its
// SubAbility$ links and a Charm's Choices$ modes -- declares a target. It
// fails closed (true) on a nil face.
func PaymentPlanSpellTargets(f *cards.Face) bool {
	if f == nil {
		return true
	}
	if slices.Contains(f.Types, "Aura") || f.HasKeyword("Enchant") {
		return true
	}
	if _, ok := f.KeywordParam("Enchant"); ok {
		return true
	}
	if _, ok := f.KeywordParam("Mutate"); ok {
		return true
	}
	targets := func(sa *cards.SA) bool {
		return params.TargetsOf(sa).Has(params.TgtDeclares)
	}
	var walk func(sa *cards.SA, depth int) bool
	walk = func(sa *cards.SA, depth int) bool {
		if sa == nil || depth > 32 {
			return false
		}
		if targets(sa) || walk(sa.Sub, depth+1) {
			return true
		}
		if name := strings.TrimSpace(sa.ParamStr(cards.PKSubAbility)); name != "" && sa.Sub == nil {
			if walk(cards.ResolveSVar(f.SVars, name), depth+1) {
				return true
			}
		}
		for mode := range strings.SplitSeq(sa.ParamStr(cards.PKChoices), ",") {
			if mode = strings.TrimSpace(mode); mode != "" && walk(cards.ResolveSVar(f.SVars, mode), depth+1) {
				return true
			}
		}
		return false
	}
	return walk(f.SpellAbility(), 0)
}

var paymentPlanNoUntapShapeKeys = state.NewNameSet("Event", "ValidCard", "Layer", "ValidStepTurnToController", "ActiveZones", "Description")

type paymentPlanSelfDamageTriggerCode uint16

const (
	paymentPlanSelfDamageTriggerKnown paymentPlanSelfDamageTriggerCode = iota + 1
)

var paymentPlanSelfDamageTriggerCodes = state.NewStrCodes(
	state.StrEntry[paymentPlanSelfDamageTriggerCode]{Key: "Mode", Val: paymentPlanSelfDamageTriggerKnown},
	state.StrEntry[paymentPlanSelfDamageTriggerCode]{Key: "ValidCard", Val: paymentPlanSelfDamageTriggerKnown},
	state.StrEntry[paymentPlanSelfDamageTriggerCode]{Key: "Execute", Val: paymentPlanSelfDamageTriggerKnown},
	state.StrEntry[paymentPlanSelfDamageTriggerCode]{Key: "TriggerZones", Val: paymentPlanSelfDamageTriggerKnown},
	state.StrEntry[paymentPlanSelfDamageTriggerCode]{Key: "TriggerDescription", Val: paymentPlanSelfDamageTriggerKnown},
)

// UnlessManaReachable reports whether pool plus one production alternative
// per window unit (or none of a unit) can satisfy cost's mana/life component.
// It is the exact affordability question the offer gate and the window's
// safety ordering both ask: a unit with several free abilities offers its
// alternatives as a CHOICE (the payer taps the permanent for one of them),
// never as their sum, so a dual land contributes {U} or {R} -- and the
// search proves a {U} tax payable from it. The search is bounded by
// manaPipCount (a minimal cover never needs more sources than the mana it
// supplies) and by a hard node budget; beyond that it fails closed, which is
// the conservative direction (never offer a Pay the window cannot complete).
func UnlessManaReachable(e Engine, p state.PlayerID, cost costvocab.Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, conv *Conv, units []WindowUnit) bool {
	return ManaReachable(e, p, cost, pool, snow, typed, life, PipRider{}, conv, units)
}

// ManaReachable reports whether cost can be paid from pool plus at most one
// production alternative per mana source.  The cast announcement path uses a
// non-empty rider here: a MayPlayIgnoreColor grant must widen the same
// candidate face test as it widens the eventual payment.  The unless-payment
// callers deliberately retain their ordinary no-rider semantics through the
// wrapper above.
func ManaReachable(e Engine, p state.PlayerID, cost costvocab.Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, rider PipRider, conv *Conv, units []WindowUnit) bool {
	payable := func(pool state.Mana, lifeNow int32) bool {
		_, ok := ResolveManaWith(cost, pool, snow, typed, lifeNow,
			e.PayLifeInsteadOfB(p), rider, conv)

		return ok
	}
	if payable(pool, life) {
		return true
	}
	budget := cost.ManaPipCount()
	if budget <= 0 {
		return false
	}
	nodes := 0
	var rec func(start, remaining int, acc state.Mana, lifeLeft int32) bool
	rec = func(start, remaining int, acc state.Mana, lifeLeft int32) bool {
		if payable(ManaAdd(pool, acc), lifeLeft) {
			return true
		}
		if remaining <= 0 {
			return false
		}
		nodes++
		if nodes > 1<<18 {
			return false
		}
		for i := start; i < len(units); i++ {
			for _, a := range units[i].Alts {
				// A PayLife activation spends real life: it must be
				// present before the tap and is gone for the priced cost
				// afterwards. Every shared-window alt carries life 0, so
				// this is inert for the attack and unless windows.
				if a.Life > lifeLeft {
					continue
				}
				if rec(i+1, remaining-1, ManaAdd(acc, a.Mana()), lifeLeft-a.Life) {
					return true
				}
			}
		}
		return false
	}
	return rec(0, budget, state.Mana{}, life)
}
