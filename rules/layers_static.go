package rules

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// stackSelfStaticOK admits a printed Continuous static with NO EffectZone$
// whose SOURCE sits on the stack, when the static's own text scopes itself to
// the stack (an IsPresent$/PresentZone$ or AffectedZone$ naming Stack).
// effectZoneOK's default admission is the battlefield -- right for a
// permanent's continuous statics, wrong for a spell's own on-the-stack
// static: Molten Disaster's kicked split second (IsPresent$ Card.Self+kicked
// | PresentZone$ Stack) names the stack as the zone it functions from, and
// CR 113.6 has it live exactly there, while an unqualified lord static (a
// creature spell's "creatures you control get +1/+1") still stays
// battlefield-only. PresentZone$ is a comma list in the grammar, hence the
// substring read.
// staticZoneAdmits is the source-zone admission a Continuous static's
// ExcludeZone$ and EffectZone$ parameters jointly express, the ONE read
// staticEffects' gate and cdaPTStatic's layer-7a CDA claim both make. With
// no ExcludeZone$ the ordinary EffectZone$ gate stands unchanged (empty =
// battlefield). With one, the named zones are excluded and -- absent an
// explicit EffectZone$ -- every OTHER zone admits, which is what lets a
// zone-conditional CDA (Grist) live exactly off the battlefield. An
// unrecognised word excludes NOTHING (the mirror-image direction of
// affectedZoneOK's fail-closed deny): the unparseable exclusion degrades to
// the ordinary gate, today's applies-as-gated behaviour, rather than going
// silent.
func staticZoneAdmits(exclude, effectZone string, z state.Zone) bool {
	exclude = strings.TrimSpace(exclude)
	if exclude == "" {
		return effectZoneOK(effectZone, z)
	}
	zones, all, ok := effects.ParseZones(exclude)
	if !ok {
		return effectZoneOK(effectZone, z)
	}
	if all || slices.Contains(zones, z) {
		return false
	}
	return effectZone == "" || effectZoneOK(effectZone, z)
}

func (e *Engine) stackSelfStaticOK(st cards.Static, o *state.Object) bool {
	if o == nil || o.Zone != state.ZStack || st.ParamStr(cards.PKEffectZone) != "" {
		return false
	}
	return strings.Contains(st.ParamStr(cards.PKPresentZone), "Stack") ||
		st.ParamStr(cards.PKAffectedZone) == "Stack"
}

// continuousGateKeys are the keys continuousGateHolds reads to decide
// anything: a static carrying none of them passes every gate (ClassBand$
// empty, no IsPresent$/IsPresent2$, no Condition$, no CheckSVar$).
var continuousGateKeys = cards.ParamMaskOf(cards.PKClassBand, cards.PKIsPresent, cards.PKIsPresent2, cards.PKCondition, cards.PKCheckSVar)

// gainsAbilitiesKeys are gainsAbilitiesOf's keys: with none present it is
// false.
var gainsAbilitiesKeys = cards.ParamMaskOf(cards.PKGainsAbilitiesOf, cards.PKGainsAbilitiesOfDefined, cards.PKGainsTriggerAbsOf)

// adjustLandPlaysGrantOf is adjustLandPlaysGrant on a static, skipping the
// map walk when the compiled set proves AdjustLandPlays$ absent.
func adjustLandPlaysGrantOf(st cards.Static) (int32, bool) {
	if !st.HasParam(cards.PKAdjustLandPlays) {
		return 0, false
	}
	return adjustLandPlaysGrant(st.Params)
}

// staticSourceZones is staticEffects' per-seat source walk, in one fixed
// order: the battlefield (every default EffectZone$ static's zone), then the
// non-battlefield zones a Continuous static's EffectZone$ can name. The
// order mirrors collectCostStatics' (rules/statics.go) so the two
// zone-scoped collectors cannot drift apart; hand and library are included
// because Forge's EffectZone$ All statics (Chittering Illuminator's
// may-play-from-top-of-library grant) are live there too, and the shared
// stack is skipped for every seat after the first (see the walk).
var staticSourceZones = []state.Zone{
	state.ZBattlefield, state.ZStack, state.ZGraveyard,
	state.ZHand, state.ZLibrary, state.ZExile, state.ZCommand,
}

// staticWork is one staticEffects queue entry: the static to emit and its
// AddStaticAbility$ depth (0 = a printed static, 1 = a granted one).
type staticWork struct {
	st    cards.Static
	depth int
}

// gainedFacesForSource collects every foreign face a live has-all-abilities-of
// grant gives `source` right now: the union of the activated half (GainedFaces)
// and the triggered half (GainedTriggerFaces) of every active grant whose Affects
// spec matches source, in active()'s deterministic layer/timestamp
// order and each effect's own face order. It is the ONE recovery point
// the resolution-time owning-face reads use (findTriggerForAbilityFace for a
// gained TRIGGER, pileFaceForSA for a gained ACTIVATED ability), so the
// OptionalDecider$/intervening-if gates and the SVar-table reads all see the
// foreign card's own face exactly as the offer/queue walk did. The union is
// safe because each consumer matches its SA/trigger by pointer identity, so a
// face present only in the other half never binds. A source no
// live grant matches returns nil.
func (e *Engine) gainedFacesForSource(source state.ObjID) []state.GainedFace {
	var out []state.GainedFace
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if len(ce.GainedFaces) == 0 && len(ce.GainedTriggerFaces) == 0 {
			continue
		}
		if !e.matchesSpecFrom(ce.Affects, source, ce.Controller, ce.Source) {
			continue
		}
		out = append(out, ce.GainedFaces...)
		out = append(out, ce.GainedTriggerFaces...)
	}
	return out
}

// gainsAbilitiesOf reports whether a Mode$ Continuous static grants abilities
// off a named card (Forge's GainsAbilitiesOf$ / GainsTriggerAbsOf$). Either
// parameter alone is enough: a static may grant only activations, only
// triggers, or both (Idris, Soul of the TARDIS carries both). The two halves
// are carried on separate face lists (GainedFaces / GainedTriggerFaces)
// because the parameters mean different ability kinds.
func gainsAbilitiesOf(st cards.Static) bool {
	return strings.TrimSpace(st.Params["GainsAbilitiesOf"]) != "" ||
		strings.TrimSpace(st.Params["GainsAbilitiesOfDefined"]) != "" ||
		strings.TrimSpace(st.Params["GainsTriggerAbsOf"]) != ""
}

// gainsLimitPerTurn parses a has-all-abilities-of static's
// GainsAbilitiesLimitPerTurn$ cap (Mairsil the Pretender's "only once each
// turn"). Only a plain integer is enforced -- the corpus's every carrier is
// a literal 1 -- and an unparseable value degrades to 0 (no cap), the
// permissive direction for an unmodelled expression.
func gainsLimitPerTurn(st cards.Static) int {
	n, err := strconv.Atoi(strings.TrimSpace(st.Params["GainsAbilitiesLimitPerTurn"]))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// gainedFacesForSpec resolves one has-all-abilities-of parameter's named
// cards: every object in the GainsAbilitiesOfZones$ zones (default
// Battlefield, Forge's StaticAbilityContinuous default) whose face matches
// the given card filter, paired with its object id. The spec is
// evaluated with src = the static's own source object, so
// `Card.ExiledWithSource` matches exactly the cards this source exiled.
//
// The walk order is fully deterministic -- the staticSourceZones order,
// then each alive seat in APNAP order, then the zone slice -- so the granted
// face list, and therefore the option and trigger order it feeds, is
// reproducible run to run. A spec matching nothing returns nil (no grant);
// an unparseable zones value returns nil, the fail-closed direction every
// grant branch takes. Faces are de-duplicated by object id.
func (e *Engine) gainedFacesForSpec(st cards.Static, spec string, src state.ObjID, controller state.PlayerID) []state.GainedFace {
	if spec == "" {
		return nil
	}
	zones, all, ok := effects.ParseZones(strings.TrimSpace(st.Params["GainsAbilitiesOfZones"]))
	if strings.TrimSpace(st.Params["GainsAbilitiesOfZones"]) == "" {
		// Forge's default zone for the has-all-abilities-of statics is the
		// battlefield (StaticAbilityContinuous's default), not every zone.
		zones, all, ok = []state.Zone{state.ZBattlefield}, false, true
	}
	if !ok {
		return nil
	}
	inZones := func(z state.Zone) bool {
		if all {
			return true
		}
		for _, want := range zones {
			if want == z {
				return true
			}
		}
		return false
	}
	var out []state.GainedFace
	seen := map[state.ObjID]bool{}
	for _, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			if !inZones(z) {
				continue
			}
			for _, id := range e.G.Zone(z, p) {
				if seen[id] {
					continue
				}
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				if !e.matchesSpecFrom(spec, id, controller, src) {
					continue
				}
				seen[id] = true
				out = append(out, state.GainedFace{Obj: id, Face: o.Face()})
			}
		}
	}
	return out
}

// parseSVarGrant parses Forge's AddSVar$ value shape "SVar:<Name>:<Value>":
// the named variable the affected object gains. ok is false for any other
// shape.
func parseSVarGrant(raw string) (name, value string, ok bool) {
	rest, ok := strings.CutPrefix(raw, "SVar:")
	if !ok {
		return "", "", false
	}
	name, value, ok = strings.Cut(rest, ":")
	if !ok || name == "" {
		return "", "", false
	}
	return name, value, true
}

// cdaPTStatic resolves ONE static's characteristic-defining P/T claim
// (CharacteristicDefining$ True), the layer-7a base cdaSetPT applies in
// every zone (CR 613.4a, CR 604.3/208.2). A static carrying any parameter
// beyond the implemented shape's whitelist fails closed (no claim -- the
// explicit-whitelist rule adjustLandPlaysGrant documents), as does one
// scoped to anything but the card itself, and one whose SetPower$/
// SetToughness$ value is neither a literal nor an SVar/inline Count$
// expression the evaluator resolves (EvalCountOK's verdict -- e.g.
// LifePaidOnETB's paid-life shape). Iterating st.Params only yields the
// whitelist boolean, so map order never reaches a value -- determinism is
// preserved.
func (e *Engine) cdaPTStatic(st cards.Static, ctx *effects.Ctx) (p, t int32, hasP, hasT bool) {
	for key := range st.Params {
		switch key {
		case "Mode", "CharacteristicDefining", "SetPower", "SetToughness", "Affected", "Description", "ExcludeZone":
			// The keys the implemented CDA shape (and only it) carries.
		default:
			return 0, 0, false, false
		}
	}
	if aff := strings.TrimSpace(st.Params["Affected"]); aff != "" && aff != "Card.Self" {
		return 0, 0, false, false
	}
	// ExcludeZone$ narrows the claim's zones (Grist, the Hunger Tide): the CDA
	// read is every zone by CR 604.3/208.2, minus the ones the static names --
	// and beside any explicit EffectZone$, exactly as the emission gate reads
	// the pair. The same staticZoneAdmits helper, so the layer-7a claim and
	// any emitted fallback ce cannot disagree about where the static is live.
	// A source object already gone carries no zone to admit.
	if raw := strings.TrimSpace(st.Params["ExcludeZone"]); raw != "" {
		if oz := e.G.Obj(ctx.Source); oz == nil || !staticZoneAdmits(raw, st.Params["EffectZone"], oz.Zone) {
			return 0, 0, false, false
		}
	}
	if raw, ok := st.Params["SetPower"]; ok {
		if n, ok := e.cdaValue(ctx, raw); ok {
			p, hasP = n, true
		}
	}
	if raw, ok := st.Params["SetToughness"]; ok {
		if n, ok := e.cdaValue(ctx, raw); ok {
			t, hasT = n, true
		}
	}
	return p, t, hasP, hasT
}

// cdaValue resolves one CDA P/T value: a literal integer, else the SVar
// named on the card's own face, else an inline Count$ expression -- each
// through effects.EvalCountOK's resolvability verdict, so an unmodelled
// count body fails closed (no claim) instead of degrading to a silent zero.
func (e *Engine) cdaValue(ctx *effects.Ctx, raw string) (int32, bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n), true
	}
	// A runtime SVar write (api:StoreSVar) shadows the printed body of the
	// same name: Minion of the Wastes / Nameless Race store the life paid as
	// they entered under LifePaidOnETB, whose printed default is Number$0, so
	// the stored value must be checked BEFORE the face table is consulted.
	if o := e.G.Obj(ctx.Source); o != nil {
		if v, ok := o.RuntimeSVars[raw]; ok {
			return v, true
		}
	}
	if strings.HasPrefix(raw, "Count$") {
		return effects.EvalCountOK(e, ctx, raw)
	}
	if body, ok := ctx.SVars[raw]; ok {
		return effects.EvalCountOK(e, ctx, body)
	}
	return 0, false
}

// cdaSetPT is the object's own layer-7a characteristic-defining P/T
// (CR 613.4a): the first usable CDA static on the current face (script
// order) resolves the base power and toughness cdaPTStatic's whitelist
// admits. CR 604.3/208.2 put the ability in EVERY zone, which is exactly
// why it is read directly off the face in derivedScalar rather than emitted
// from the battlefield-only static scan. A face with no usable CDA degrades
// to no claim (the printed P/T stands).
func (e *Engine) cdaSetPT(o *state.Object) (p, t int32, hasP, hasT bool) {
	f := o.Face()
	if f == nil {
		return 0, 0, false, false
	}
	var ctx *effects.Ctx // built only for a face that has a CDA static
	for _, st := range f.Statics {
		if st.Mode != "Continuous" || strings.TrimSpace(st.Params["CharacteristicDefining"]) == "" {
			continue
		}
		if ctx == nil {
			ctx = &effects.Ctx{Source: o.ID, Controller: o.Controller, SVars: f.SVars}
		}
		if pp, tt, hp, ht := e.cdaPTStatic(st, ctx); hp || ht {
			return pp, tt, hp, ht
		}
	}
	return 0, 0, false, false
}

// GrantedSVar reports the named variable a live static grant (AddSVar$ on a
// Mode$ Continuous static, e.g. Sword of Fire and Ice's MustBeBlocked on the
// equipped creature) gives id: the value Forge's "SVar:<Name>:<Value>"
// grant shape carries, or ok=false when no live grant gives id that name.
// The lookup is a read over active()'s sorted slice (never a map range), so
// the answer is deterministic; the map it reads is key-resolved, so map
// order never reaches an event, option, view or file.
func (e *Engine) GrantedSVar(id state.ObjID, name string) (string, bool) {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if len(ce.AddSVars) == 0 {
			continue
		}
		if v, ok := ce.AddSVars[name]; ok && e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
			return v, true
		}
	}
	return "", false
}

// grantedSVarsFor merges every live AddSVar$ grant that affects id into one
// map; nil when none applies (the common path allocates nothing). The
// result is a FRESH map, never the face's own table: the caller layers it
// under the printed table (a printed SVar of the same name wins, the same
// precedence the roll-publication read documents) and must not mutate
// immutable card data.
func (e *Engine) grantedSVarsFor(id state.ObjID) map[string]string {
	var merged map[string]string
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if len(ce.AddSVars) == 0 {
			continue
		}
		if !e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
			continue
		}
		if merged == nil {
			merged = map[string]string{}
		}
		for n, v := range ce.AddSVars {
			merged[n] = v
		}
	}
	return merged
}

// MayLookAtLibraryTop reports whether p may look at the top card of p's own
// library right now: a live Continuous MayLookAt grant (Oracle of Mul Daya)
// whose Affected$ spec matches that top card -- the TopLibrary predicate in
// the spec itself pins the object to the top of its owner's library, and
// YouCtrl resolves against the granting static's controller, so a stolen
// Oracle reveals to its controller, never to a library owner the grant does
// not cover. A read over active()'s sorted slice; the answer is boolean, so
// no order reaches anything ordered.
func (e *Engine) MayLookAtLibraryTop(p state.PlayerID) bool {
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		return false
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.MayLookAt && e.matchesSpecFrom(ce.Affects, lib[0], ce.Controller, ce.Source) {
			return true
		}
	}
	return false
}

// continuousGateHolds evaluates the "as long as" condition gates a Mode$
// Continuous static can carry, the intervening-if that decides whether the
// grant lives at this instant: IsPresent$/IsPresent2$ (an existence count over
// every battlefield, PresentCompare$ pricing the count -- default GE1),
// CheckSVar$/SVarCompare$ (the named SVar -- or inline Count$ expression --
// compared under the threshold, no compare meaning "nonzero"), and
// Condition$ (the ability-word condition family). Every evaluator is shared
// with the restriction/cost/ability gates (rules/statics.go's presentGate,
// checkSVarHolds and costConditionHolds; rules/legal.go's
// activationConditionOK) so the ONE grammar governs every static family.
// staticEffects re-runs once per emitted event, so evaluating the gate there
// is the continuous recheck the grant needs. A gate this build cannot evaluate
// fails closed -- the shipped statics convention: an unreadable "as long as"
// must not silently always-apply.
func (e *Engine) continuousGateHolds(sv staticView) bool {
	if !e.classBandGateHolds(sv.Params, sv.Source) {
		return false
	}
	if spec, ok := sv.Param(cards.PKIsPresent); ok && !e.presentGate(sv, spec) {
		return false
	}
	if spec, ok := sv.Param(cards.PKIsPresent2); ok && !e.presentGate(sv, spec) {
		return false
	}
	if !e.continuousConditionHolds(sv) {
		return false
	}
	return e.checkSVarHolds(sv)
}

// continuousConditionHolds evaluates Condition$ on a Mode$ Continuous static
// -- the "Delirium --", "Threshold --", "Metalcraft --" ability-word family
// whose grant lives only while the condition is met. The evaluable values map
// onto the shared condition machinery the other static families already use:
//
//   - Delirium: the controller's graveyard holds 4+ distinct core card types
//     (rules/replacement.go's graveyardCardTypeCount, the ONE census shared
//     with rules/legal.go's activationConditionOK);
//   - PlayerTurn / NotPlayerTurn: the static's controller is or is not the
//     active player (the same reads rules/statics.go's costConditionHolds and
//     restrictionGateHolds make);
//   - Metalcraft: 3+ artifacts the controller controls (costConditionHolds'
//     Count$ arm);
//   - Threshold: 7+ cards in the controller's graveyard;
//   - Hellbent: the controller's hand is empty.
//   - Blessing: the controller holds the city's blessing (CR 702.131, the
//     Ascend latch, state.Player.Blessing -- granted by rules/ascend.go's
//     emit-side scan and spell-resolution grant).
//
// Every other value -- FatefulHour, Monarch, MaxSpeed
// and anything new -- FAILS CLOSED (the gate never holds), matching every
// sibling gate's documented deny direction. MaxSpeed is safe to deny here:
// its statics carry only AddAbility$/AddStaticAbility$/AddTrigger$/
// AddReplacementEffect$/AddSVar$, never a layer-walk key, and the speed family
// is read separately by rules/speed.go's maxSpeedAbilities. An absent or empty
// Condition$ keeps holding, as before.
func (e *Engine) continuousConditionHolds(sv staticView) bool {
	raw, ok := sv.Param(cards.PKCondition)
	if !ok {
		return true
	}
	switch strings.TrimSpace(raw) {
	case "":
		return true
	case "Delirium":
		return e.graveyardCardTypeCount(sv.Controller) >= 4
	case "PlayerTurn":
		return e.G.Active == sv.Controller
	case "NotPlayerTurn":
		return e.G.Active != sv.Controller
	case "Metalcraft":
		return e.metalcraftHolds(sv.Controller)
	case "Threshold":
		return e.thresholdHolds(sv.Controller)
	case "Hellbent":
		return len(e.G.Zone(state.ZHand, sv.Controller)) == 0
	case "Blessing":
		// CR 702.131: the city's blessing is a one-way event-folded latch.
		if int(sv.Controller) >= len(e.G.Players) {
			return false
		}
		return e.G.Players[sv.Controller].Blessing
	case "EnduringStory":
		return e.playerHasEnduringStory(sv.Controller)
	}
	return false
}

// adjustLandPlaysGrant reports whether a Mode$ Continuous static carries the
// additional-land-drops grant this package implements, and resolves its
// value: a literal positive integer AdjustLandPlays$ plus only display
// metadata (Description$), with the Affects player spec left to the gate's
// own fail-closed evaluation. Anything else -- a non-literal value
// (Unlimited, an SVar-driven Z), a rider that changes when the grant lives
// (IsPresent$, Condition$, CheckSVar$, SVarCompare$, Secondary$,
// EffectZone$) or any other semantic parameter -- fails closed so the grant
// is never silently under- or over-applied. Iterating the params map only
// yields a boolean, so map order never reaches an event/option/view --
// determinism is preserved.
func adjustLandPlaysGrant(params map[string]string) (int32, bool) {
	raw, ok := params["AdjustLandPlays"]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		// A value this build cannot price as a count (Unlimited, an SVar
		// token) must not silently become a smaller grant.
		return 0, false
	}
	for key := range params {
		switch key {
		case "Mode", "AdjustLandPlays", "Affected", "Description":
			// The keys the implemented grant (and only it) carries.
		default:
			return 0, false
		}
	}
	return int32(n), true
}

// mayPlayGrant reports whether a Mode$ Continuous static carries the
// may-play grant this package implements: MayPlay$ True, an Affects
// (Affected$) spec and an AffectedZone, plus only display/placement metadata
// and the riders it reads (MayPlayIgnoreColor$, MayPlayIgnoreType$,
// MayPlayLimit$). A richer grant is out of scope and must fail closed (MayPlay
// stays false) so it is never silently over-applied -- in particular a
// MayPlayWithoutManaCost$ (free cast) static changes what the cast IS, not
// just where it may come from, and a Condition$/CheckSVar$/ValidAfterStack$/
// Secondary$ qualifier changes when the grant lives. The explicit whitelist,
// rather than a blacklist of currently-known gating keys, means a newly
// encountered semantic parameter also fails closed. Iterating st.Params only
// yields a boolean, so map order never reaches an event/option/view --
// determinism is preserved.
func mayPlayGrant(st cards.Static) bool {
	_, _, _, _, ok := effects.MayPlayStaticParams(st.Params)
	return ok
}

// hasStat reports whether a static line carries the named parameter.
func hasStat(st cards.Static, key string) bool {
	_, ok := st.Params[key]
	return ok
}

// staticAmount evaluates a static's P/T parameter at derivation time. It
// deliberately goes through effects.Num: that is the shared Forge numeric
// grammar for signed SVar names and Count$ bodies. The source and its SVar
// table are rebound on every call, so a life total, counters, or zones changing
// after the static entered changes its value without any cached snapshot.
func (e *Engine) staticAmount(ce *ContinuousEffect, expr string) int32 {
	return e.staticAmountOn(ce, expr, ce.Source)
}

// staticAmountOn evaluates a static's numeric expression with Ctx.Source
// anchored on `anchor` while the SVar table still comes from the grantor
// (ce.SVars, falling back to the grantor's face). staticAmount delegates
// with the grantor itself as the anchor. The layer-7c modify walk uses an
// affected-object anchor only for the explicit AffectedX convention.
func (e *Engine) staticAmountOn(ce *ContinuousEffect, expr string, anchor state.ObjID) int32 {
	if expr == "" {
		return 0
	}
	src := e.G.Obj(ce.Source)
	if src == nil || src.Face() == nil {
		return 0
	}
	svars := ce.SVars
	if svars == nil {
		svars = src.Face().SVars
	}
	sa := &cards.SA{Params: map[string]string{"Amount": expr}}
	ctx := &effects.Ctx{Source: anchor, Controller: ce.Controller, SVars: svars}
	if strings.Contains(expr, "Count$ValidSelf Card$CreatureType") || strings.Contains(svars[expr], "Count$ValidSelf Card$CreatureType") {
		e.refreshDerivedTypes()
		ctx.EffectiveTypes = e.EffectiveTypes()
	}
	return effects.Num(e, ctx, sa, "Amount", 0)
}

// addPT saturates instead of allowing a large static expression to wrap a
// characteristic through zero. Forge's calculateAmount is int-bounded too;
// keeping the clamp at this boundary makes all P/T additions deterministic.
func addPT(a, b int32) int32 {
	n := int64(a) + int64(b)
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}

// statKeywords parses AddKeyword$ through the shared Forge keyword-list
// parser. In particular its ampersands divide keywords while commas remain
// inside a keyword's parameters.
func statKeywords(st cards.Static) []string {
	return cards.SplitKeywordList(st.Params["AddKeyword"])
}

func statRemoveKeywords(st cards.Static) []string {
	return cards.SplitKeywordList(st.Params["RemoveKeyword"])
}

func statCantHaveKeywords(st cards.Static) []string {
	return cards.SplitKeywordList(st.ParamStr(cards.PKCantHaveKeyword))
}

// statList parses additive TYPE parameters. Type lists retain their existing
// comma-separated grammar; they must not use SplitKeywordList, whose
// ampersand grammar is specific to keyword parameters.
func statList(st cards.Static, key string) []string {
	var out []string
	for v := range strings.SplitSeq(st.Params[key], ",") {
		for part := range strings.SplitSeq(strings.TrimSpace(v), " & ") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// resolveChosenTypes resolves the AddType$ value "ChosenType" against the
// static host's own recorded ETB choice (state.Object.ChosenType, set by the
// Choose event the cast/play-time ask emitted). Everything else passes
// through unchanged. ok is false when a ChosenType entry names a host with no
// recorded choice — the caller withholds the grant whole.
func resolveChosenName(raw string, o *state.Object) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(raw), "ChosenName") {
		if o == nil || o.ChosenName == "" {
			return "", false
		}
		return o.ChosenName, true
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", false
	}
	return name, true
}

func resolveChosenTypes(list []string, o *state.Object) ([]string, bool) {
	out := make([]string, 0, len(list))
	for _, t := range list {
		if t != "ChosenType" {
			out = append(out, t)
			continue
		}
		if o == nil || o.ChosenType == "" {
			return nil, false
		}
		out = append(out, o.ChosenType)
	}
	return out, true
}

// resolveChosenColors resolves a SetColor$/AddColor$ value against the static
// host's own recorded "as this enters, choose a color" / CR 903.4b pregame
// choice (state.Object.ChosenColor, set by the Choose event the ask emitted).
// It is the layer-5 twin of resolveChosenTypes. A value of "ChosenColor"
// resolves to the host's recorded colour -- the event records a single WUBRG
// letter (rules/resolution.go resumeETBEntry), but a full colour word is accepted too so
// the two spellings cannot drift -- and a host with NO recorded choice fails
// closed: ok=false, the caller emits nothing and the object keeps its printed
// colours (today's shipped behaviour for the whole family).
//
// Everything else passes through the ordinary colour-word parser. A bare
// WUBRG letter is accepted directly (the layer-5 walk at ~1652 reads
// strings.IndexByte("WUBRG", l[0]), so a letter element is already legal),
// which is the shape the recorded choice itself carries; a value the parser
// cannot fully recognise still fails closed, exactly as before.
func resolveChosenColors(raw string, o *state.Object) ([]string, bool) {
	if strings.EqualFold(strings.TrimSpace(raw), "ChosenColor") {
		if o == nil || o.ChosenColor == "" {
			return nil, false
		}
		if cols, ok := effects.ColorLetters(o.ChosenColor); ok && len(cols) > 0 {
			return cols, true
		}
		// A bare WUBRG letter (the recorded form) bypasses the word parser.
		if l := strings.ToUpper(strings.TrimSpace(o.ChosenColor)); len(l) == 1 && strings.IndexByte("WUBRG", l[0]) >= 0 {
			return []string{l}, true
		}
		return nil, false
	}
	return effects.ColorLetters(raw)
}
