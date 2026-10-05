package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// noResolve is the resolver used whenever a caller has none of its own
// (MatchesSpec/MatchesSpecFrom, and the shape-only checks in
// UnknownPredicates below): every non-literal numeric RHS is
// a recognised shape that never matches, never a hard "unknown predicate".
func noResolve(string) (int32, bool) { return 0, false }

// numericPred handles the "<field><CMP><n>" family: powerLE2, cmcGE3, and so
// on, plus a right-hand side that is not a literal integer ("cmcEQY",
// "cmcEQChosen", "powerGEX"), which resolve looks up by name -- typically an
// {X} paid or a Chosen* value SpecContext.Resolve closes over. Returns
// ok=false when the token is not of this shape at all; ok=true with
// result=false when the shape is recognised but the RHS did not resolve, so
// a filter spec is either a hard "no" or "not this predicate", never a
// silent match.
// objectPowerInContext reads a bound layer-derived power when available and
// otherwise preserves the direct filter matcher’s printed-plus-counter
// fallback. greatestPower uses the full per-object table when its caller can
// supply one; numeric predicates use the candidate-only DerivedPower bind.
func objectPowerInContext(o *state.Object, sc SpecContext) int {
	for _, d := range sc.DerivedPTs {
		if d.ID == o.ID {
			return int(d.Power)
		}
	}
	if sc.HasDerivedPT {
		return int(sc.DerivedPower)
	}
	return objectPower(o)
}

func objectPower(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	dp, _ := o.CounterPTTotals()
	return f.Power() + int(dp)
}

// GreatestPowerDerivedPTs returns every battlefield object's CURRENT
// layer-derived power (the reader's Power) when spec consults the
// greatestPower predicate, and nil otherwise; callers append the return onto
// their SpecContext's DerivedPTs (an append of nil is a no-op). It is the ONE
// bind every seam that can reach a greatestPower spec goes through -- rules'
// matchesSpec (ValidTgts$/static specs), Count$ValidSelf, Count$Valid,
// ConditionPresent$/IsPresent$, Defined$ Valid (the battlefield sweep and the
// zone-suffixed family), the AttachedTo qualifier words and the Choices$
// matcher -- so the WHOLE comparison set reads derived power, not only the
// candidate: a pump on any creature in the set must be able to displace every
// printed-greatest peer. A caller that cannot supply a reader (nil, or a spec
// without greatestPower) keeps objectPower's printed-plus-counter fallback,
// which is what a direct or census call wants.
//
// The value-table return, not a bind-into-pointer helper, is deliberate: a
// &sc argument escapes the caller's SpecContext to the heap on every call,
// and the hot Count$Valid scan is pinned allocation-free
// (TestEvalCountValidZoneScanIsAllocationFree). The reader is an interface
// value, deliberately not a func parameter: a seam passes its Host (or rules'
// *Engine) without constructing a bound-method closure per call.
func GreatestPowerDerivedPTs(g *state.Game, spec string, reader interface{ Power(state.ObjID) int32 }) []ObjectPower {
	if reader == nil || !strings.Contains(spec, "greatestPower") {
		return nil
	}
	var out []ObjectPower
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone == state.ZBattlefield {
			out = append(out, ObjectPower{ID: o.ID, Power: reader.Power(o.ID)})
		}
	}
	return out
}

// objectToughness is objectPower's counterpart, the same face-plus-counter
// read summed over every P/T counter kind the toughness family uses.
func objectToughness(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	_, dt := o.CounterPTTotals()
	return f.Toughness() + int(dt)
}

// objectBasePower / objectBaseToughness are the object-alone BASE P/T read:
// the printed face value with NO counters. They are the fallback for a filter
// call whose SpecContext carries no rules-bound base characteristic (a direct
// effects-level match, or the census probe). The rules tier binds the real
// base through SpecContext.BasePower/BaseToughness -- the value through layer
// 7b, before 7c modifies and 7d counters (CR 613.4) -- and that binding is
// authoritative when present. The fallback deliberately never adds a +1/+1
// counter, so a counter can never move a base-power comparison on a caller
// that forgot to bind.
func objectBasePower(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	return f.Power()
}

// objectBaseToughness is objectBasePower's toughness counterpart.
func objectBaseToughness(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	return f.Toughness()
}

// objectManaValue is the one mana-value read the extreme-CMC classifier
// shares with the rest of the engine: the compiled face's CMC, the same
// Face().Cmc() read manaValueOf, cascade and the Count$ walkers use. It is
// the analogue of objectPower for greatestCMC/lowerCMC, so the two extreme
// classifiers cannot drift on what a card's mana value is.
func objectManaValue(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	return int(f.Cmc())
}

// cmcSetMember reports whether o is in the comparison set a greatestCMC_<prop>
// suffix names, mirroring Forge's CardProperty branch: NonLandPermanent is the
// nonland-permanent predicate, and every other prop is the named card type
// (Forge's CardLists.getType -> CardPredicates.isType). An empty prop is not a
// shape Forge produces (greatestCMC_ always carries a suffix), so it fails
// closed to no set rather than widening to "every battlefield card".
func cmcSetMember(o *state.Object, prop string) bool {
	switch cmcSetMemberCodes.Code(string(prop)) {
	case cmcSetMemberEmpty:
		return false
	case cmcSetMemberNonLandPermanent:
		return !hasType(o, "Land")
	case cmcSetMemberPermanent:
		return true
	}
	return hasType(o, prop)
}

func numericPred(name string, g *state.Game, o *state.Object, sc SpecContext) (result, ok bool) {
	if name == "cmcChosenEvenOdd" {
		if g == nil || o == nil {
			return false, true
		}
		source := g.Obj(sc.Source)
		if source == nil || (source.ChosenType != "odd" && source.ChosenType != "even") {
			return false, true
		}
		odd := objectManaValue(o)%2 == 1
		return (source.ChosenType == "odd") == odd, true
	}
	resolve := sc.Resolve
	if resolve == nil {
		resolve = noResolve
	}
	// classLevel_<CMP><n> reads a Class's designation, independently of counters.
	if rest, ok := strings.CutPrefix(name, "classLevel_"); ok {
		if len(rest) < 3 {
			return false, false
		}
		n, err := strconv.Atoi(rest[2:])
		if err != nil {
			return false, false
		}
		switch CmpOpOf(rest[:2]) {
		case CmpLT:
			return o.ClassLevel() < int32(n), true
		case CmpGE:
			return o.ClassLevel() >= int32(n), true
		}
		return false, false
	}
	// counters_<CMP><n>_<KIND>: a counter-kind comparison, e.g. counters_EQ0_P1P1
	// ("no +1/+1 counters", the Undying condition). Reads the object's current
	// counter count of KIND off the object it is applied to -- which for a
	// zone-change trigger is the LKI snapshot, so a "dies" condition sees what
	// the permanent had the moment it left the battlefield, not the reset state
	// Move leaves behind (events/apply.go's Move clears Counters).
	if strings.HasPrefix(name, "counters_") {
		rest := name[len("counters_"):]
		// Rest is "<CMP><n>_<KIND>"; at minimum "EQ0_A".
		if len(rest) < 4 {
			return false, false
		}
		cmp := rest[:2]
		numStr, kind, okSplit := strings.Cut(rest[2:], "_")
		if !okSplit || kind == "" {
			return false, false
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			v, resolved := resolve(numStr)
			if !resolved {
				return false, true // recognised shape, unresolvable RHS never matches
			}
			n = int(v)
		}
		have := o.Counter(kind)
		target := int32(n)
		switch CmpOpOf(cmp) {
		case CmpLE:
			return have <= target, true
		case CmpGE:
			return have >= target, true
		case CmpEQ:
			return have == target, true
		case CmpLT:
			return have < target, true
		case CmpGT:
			return have > target, true
		}
		return false, false
	}
	// The four characteristic operands a power/toughness comparison may name, on
	// the candidate object this spec is matched against. `power`/`toughness`
	// are the CURRENT derived values; `basePower`/`baseToughness` are the base
	// through layer 7b (CR 613.4: printed or characteristic-defining, then a
	// 7b set, BEFORE 7c modifies and 7d counters). When the rules tier bound
	// the candidate's layer-derived values (SpecContext.HasDerivedPT /
	// HasBasePT, set by rules' matchesSpec) those are authoritative; an
	// unbound context -- a direct filter call or the census probe -- keeps the
	// object-alone read the power predicates always had (printed face plus
	// P1P1 counters for current, printed face alone for base).
	currentPower := func() int {
		if sc.HasDerivedPT {
			return int(sc.DerivedPower)
		}
		return objectPower(o)
	}
	currentToughness := func() int {
		if sc.HasDerivedPT {
			return int(sc.DerivedToughness)
		}
		return objectToughness(o)
	}
	basePowerValue := func() int {
		if sc.HasBasePT {
			return int(sc.BasePower)
		}
		return objectBasePower(o)
	}
	baseToughnessValue := func() int {
		if sc.HasBasePT {
			return int(sc.BaseToughness)
		}
		return objectBaseToughness(o)
	}
	// fieldValue reads one of the four characteristic operands by name. It is
	// the one place the RHS-operand spelling and the LHS field spelling agree,
	// so `powerGTbasePower` and `basePowerEQ1` read the same values. The
	// capitalised RHS spellings (`Power`/`Toughness`/`BasePower`/
	// `BaseToughness`) are Forge's alias for the same operand.
	fieldValue := func(operand string) (int, bool) {
		switch numericOperandCodes.Code(string(operand)) {
		case numericOperandPower:
			return currentPower(), true
		case numericOperandToughness:
			return currentToughness(), true
		case numericOperandBasePower:
			return basePowerValue(), true
		case numericOperandBaseToughness:
			return baseToughnessValue(), true
		}
		return 0, false
	}
	// applyCmp folds the comparison operator. `NOT` is Forge's "not equal"
	// spelling (powerNOTbasePower, "power different from its base power");
	// the other four are the two-character operators. An unrecognised operator
	// reports ok=false so a caller can distinguish "not this shape" from a
	// recognised-but-false comparison.
	applyCmp := func(cmp string, lhs, rhs int) (result, ok bool) {
		switch numericCmpOrNotCodes.Code(string(cmp)) {
		case numericCmpOrNotLE:
			return lhs <= rhs, true
		case numericCmpOrNotGE:
			return lhs >= rhs, true
		case numericCmpOrNotEQ:
			return lhs == rhs, true
		case numericCmpOrNotLT:
			return lhs < rhs, true
		case numericCmpOrNotGT:
			return lhs > rhs, true
		case numericCmpOrNotNOT:
			return lhs != rhs, true
		}
		return false, false
	}
	for _, field := range [...]string{"power", "toughness", "cmc", "basePower", "baseToughness"} {
		// The P/T half of this vocabulary is ptNumericFields, which
		// SpecReadsPT's dependency test reads through ptNumericField; cmc is
		// the evaluator's one non-P/T field and is matched here only.
		if !strings.HasPrefix(name, field) {
			continue
		}
		rest := name[len(field):]
		if len(rest) < 3 {
			return false, false
		}
		cmp, numStr := rest[:2], rest[2:]
		if strings.HasPrefix(rest, "NOT") {
			cmp, numStr = "NOT", rest[3:]
			if numStr == "" {
				return false, false
			}
		}
		// powerLTtoughness / powerGTbasePower / powerNOTbasePower (and the
		// mirrors): compare two characteristics instead of a numeric RHS
		// (Assault Formation, Bedrock Tortoise, Baird Argivian Recruiter).
		// The shape is recognised before any object read, so UnknownPredicates'
		// nil-object probe resolves it and the matcher and the census cannot
		// disagree.
		if lhs, lhsOK := fieldValue(field); lhsOK {
			if rhs, rhsOK := fieldValue(numStr); rhsOK {
				return applyCmp(cmp, lhs, rhs)
			}
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			v, resolved := resolve(numStr)
			if !resolved {
				return false, true // recognised shape, unresolvable RHS never matches
			}
			n = int(v)
		}
		f := o.Face()
		if f == nil {
			return false, true
		}
		var have int
		switch numericFieldCodes.Code(string(field)) {
		case numericFieldPower:
			have = currentPower()
		case numericFieldToughness:
			have = currentToughness()
		case numericFieldBasePower:
			have = basePowerValue()
		case numericFieldBaseToughness:
			have = baseToughnessValue()
		case numericFieldCmc:
			// CR 202.3e: {X} counts as its chosen value in a spell's mana
			// value. A caller that has the chosen X in hand (the CR 601.2e
			// post-announcement cast-illegality recheck) passes it through
			// SpecContext.ManaValue; otherwise the printed cost is used,
			// which counts an un-chosen X as 0, exactly as the offer-time
			// 601.3a check must.
			if sc.HasManaValue {
				have = int(sc.ManaValue)
			} else {
				have = int(parseCMC(f.ManaCost))
			}
		}
		return applyCmp(cmp, have, n)
	}
	return false, false
}

var cmcBraceNormalizer = strings.NewReplacer("{", " ", "}", " ")

// parseCMC counts a mana cost's converted value without importing rules.
func parseCMC(cost string) int32 {
	cost = cmcBraceNormalizer.Replace(cost)
	if strings.EqualFold(strings.TrimSpace(cost), "no cost") {
		return 0
	}
	var n int32
	for sym := range strings.FieldsSeq(cost) {
		if v, err := strconv.Atoi(sym); err == nil {
			n += int32(v)
			continue
		}
		if sym != "X" {
			n++
		}
	}
	return n
}

// sameNameContextBase recognises only the three base-prefix forms which
// carry sameName's referent. Keeping this rewrite name-specific is important:
// Remembered.*, Targeted.*, and Triggered.* have many unrelated predicates
// whose grammar and behaviour this task must not expand.
func sameNameContextBase(base, rest string) bool {
	if !strings.HasPrefix(base, "Remembered") &&
		!strings.HasPrefix(base, "Targeted") &&
		!strings.HasPrefix(base, "Triggered") {
		return false
	}
	return hasPredicate(rest, "sameName")
}

func hasPredicate(rest, want string) bool {
	for p := range strings.SplitSeq(rest, "+") {
		if p == want {
			return true
		}
	}
	return false
}

// sameNameContextReferent resolves the object whose name a sameName context
// base names. An absent binding fails closed rather than falling back to the
// ability source.
func sameNameContextReferent(g *state.Game, base string, sc SpecContext) (state.ObjID, bool) {
	switch {
	case strings.HasPrefix(base, "Remembered"):
		for _, t := range sc.Remembered {
			if !t.IsPlayer && t.Obj != 0 && g.Obj(t.Obj) != nil {
				return t.Obj, true
			}
		}
	case strings.HasPrefix(base, "Targeted"):
		bound, _ := sc.TargetBinding()
		for _, t := range bound {
			if !t.IsPlayer && t.Obj != 0 && g.Obj(t.Obj) != nil {
				return t.Obj, true
			}
		}
	case strings.HasPrefix(base, "Triggered"):
		if sc.TriggerCard != 0 && g.Obj(sc.TriggerCard) != nil {
			return sc.TriggerCard, true
		}
	}
	return 0, false
}

// isPermanentCard is Forge's card.isPermanent() reading used only by
// Targeted.Permanent+sameName: a battlefield object is permanent, and away
// from the battlefield a card's printed type decides it.
func isPermanentCard(o *state.Object) bool {
	if o.Zone == state.ZBattlefield {
		return true
	}
	f := o.Face()
	return f != nil && f.IsPermanent()
}

// matchesBase handles the base type, including a "non" prefix.

type cmcSetMemberCode uint16

const (
	cmcSetMemberEmpty cmcSetMemberCode = iota + 1
	cmcSetMemberNonLandPermanent
	cmcSetMemberPermanent
)

var cmcSetMemberCodes = state.NewStrCodes(
	state.StrEntry[cmcSetMemberCode]{Key: "", Val: cmcSetMemberEmpty},
	state.StrEntry[cmcSetMemberCode]{Key: "NonLandPermanent", Val: cmcSetMemberNonLandPermanent},
	state.StrEntry[cmcSetMemberCode]{Key: "Permanent", Val: cmcSetMemberPermanent},
)

type numericOperandCode uint16

const (
	numericOperandPower numericOperandCode = iota + 1
	numericOperandToughness
	numericOperandBasePower
	numericOperandBaseToughness
)

var numericOperandCodes = state.NewStrCodes(
	state.StrEntry[numericOperandCode]{Key: "power", Val: numericOperandPower},
	state.StrEntry[numericOperandCode]{Key: "Power", Val: numericOperandPower},
	state.StrEntry[numericOperandCode]{Key: "toughness", Val: numericOperandToughness},
	state.StrEntry[numericOperandCode]{Key: "Toughness", Val: numericOperandToughness},
	state.StrEntry[numericOperandCode]{Key: "basePower", Val: numericOperandBasePower},
	state.StrEntry[numericOperandCode]{Key: "BasePower", Val: numericOperandBasePower},
	state.StrEntry[numericOperandCode]{Key: "baseToughness", Val: numericOperandBaseToughness},
	state.StrEntry[numericOperandCode]{Key: "BaseToughness", Val: numericOperandBaseToughness},
)

type numericCmpOrNotCode uint16

const (
	numericCmpOrNotLE numericCmpOrNotCode = iota + 1
	numericCmpOrNotGE
	numericCmpOrNotEQ
	numericCmpOrNotLT
	numericCmpOrNotGT
	numericCmpOrNotNOT
)

var numericCmpOrNotCodes = state.NewStrCodes(
	state.StrEntry[numericCmpOrNotCode]{Key: "LE", Val: numericCmpOrNotLE},
	state.StrEntry[numericCmpOrNotCode]{Key: "GE", Val: numericCmpOrNotGE},
	state.StrEntry[numericCmpOrNotCode]{Key: "EQ", Val: numericCmpOrNotEQ},
	state.StrEntry[numericCmpOrNotCode]{Key: "LT", Val: numericCmpOrNotLT},
	state.StrEntry[numericCmpOrNotCode]{Key: "GT", Val: numericCmpOrNotGT},
	state.StrEntry[numericCmpOrNotCode]{Key: "NOT", Val: numericCmpOrNotNOT},
)

type numericFieldCode uint16

const (
	numericFieldPower numericFieldCode = iota + 1
	numericFieldToughness
	numericFieldBasePower
	numericFieldBaseToughness
	numericFieldCmc
)

var numericFieldCodes = state.NewStrCodes(
	state.StrEntry[numericFieldCode]{Key: "power", Val: numericFieldPower},
	state.StrEntry[numericFieldCode]{Key: "toughness", Val: numericFieldToughness},
	state.StrEntry[numericFieldCode]{Key: "basePower", Val: numericFieldBasePower},
	state.StrEntry[numericFieldCode]{Key: "baseToughness", Val: numericFieldBaseToughness},
	state.StrEntry[numericFieldCode]{Key: "cmc", Val: numericFieldCmc},
)
