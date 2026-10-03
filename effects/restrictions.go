package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func CantRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Target", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// CantAttackParamsReadableForRules is the FACE S:-line whitelist for a
// CantAttack static: the shared CantRestrictionParamsReadable core EXTENDED by
// exactly the conditional parameter family rules' attackBlocked reads --
// UnlessDefender$ (through effects.UnlessDefenderHolds) and the shared
// rules-side continuous gate rules/layers.go continuousGateHolds, which
// evaluates CheckSVar$ / SVarCompare$ / Condition$ / ClassBand$ and the
// IsPresent$ / IsPresent2$ / PresentCompare$ / PresentZone$ count family
// (PresentZone$ Battlefield/Graveyard/Exile/Hand/Stack; see
// countStaticPresent). It lives here, beside CantRestrictionParamsReadable and
// mirrors MustAttackParamsReadableForRules below, so the face whitelist and the
// gate evaluators cannot drift apart unseen.
//
// The present family joined this list with compound-statics1. The earlier
// measurement (271 `Mode$ CantAttack` files, 63 raw lines carrying the gate
// family, NONE pairing it with IsPresent$/PresentCompare$) predated commit
// f81f996e ("split compound S:Mode$ comma lists into one static per mode"):
// the split makes a compound line's CantAttack half inherit the SHARED Params
// map, so an `S:Mode$ CantAttack,CantBlock | ... | IsPresent$ Creature.YouCtrl
// | PresentCompare$ LE2` line (Bast, Panther Goddess) now reaches
// attackBlocked as a CantAttack static carrying IsPresent. The gate machinery
// already evaluates it, so excluding the keys only skipped the attack half
// whole while the block half (blockRestricted's CantBlock loop, which runs
// continuousGateHolds with no whitelist) bound at runtime -- the asymmetry
// this ticket fixes.
//
// Measured over the corpus: of the 24 real `Mode$ CantAttack` files carrying
// this family, 20 spell the battlefield IsPresent$/PresentCompare$ count shape
// (Desperate Castaways, Gadrak the Crown-Scourge, War Falcon, ...), one a bare
// IsPresent$ "if" shape (Wirecat, Shauku Endbringer with PresentCompare$ GT1),
// one PresentZone$ Hand (Kefnet the Mindful) and one PresentZone$ Exile
// (Ketramose, the New Dawn); IsPresent2$/ClassBand$ have zero CantAttack
// carriers but are whitelisted anyway because continuousGateHolds evaluates
// them, the same fail-closed principle MinMaxBlocker's whitelist states. None
// is a repo-deck card.
//
// A line carrying PresentCompare$ WITHOUT IsPresent$/IsPresent2$ is rejected:
// presentGate is the only reader of PresentCompare and it runs only when a
// present spec is present, so an orphan compare would fall through the gate
// unread and restrict blanket, over-restricting. A present KEY whose spec is
// empty or whitespace-only is rejected the same way (and even with no compare):
// countPresent("") matches nothing, so the "gate" reads count 0 forever and an
// EQ0 compare would hold unconditionally. Measured 0 corpus rows for both
// shapes; the guards keep it that way.
//
// A present spec carrying a predicate this build's matcher does not recognise
// is rejected too: countPresent counts through the matcher, so an unparseable
// spec matches nothing and an EQ0 compare ("restrict unless X is ABSENT")
// would read count 0 unconditionally and blanket-restrict. The one corpus row
// (Flowering Lumberknot, `IsPresent$ Creature.PairedWith+withSoulbond |
// PresentCompare$ EQ0`) now resolves through the paired creature's Soulbond
// keyword, rather than treating the predicate as unread.
// UnknownPredicates (this package) is the same census the matcher's
// recognisedPredicate classifier drives, so the check cannot drift from what
// countPresent really resolves.
//
// The Effect-delivered registration gate (effEffect, which keeps the narrower
// CantRestrictionParamsReadable for BOTH modes) cannot share this list: its
// continuous path reads neither evaluator, so a gate-bearing body must not
// register blanket -- a gated "can't attack" would become unconditional,
// over-restricting, and could leave a MustAttack creature with no legal pair.
// A static carrying any OTHER parameter (ValidCause$, ForCost$, ValidSA$,
// Cost$, ...) still fails the whitelist and is skipped whole, the deliberate
// permissive direction. Iterating the params map only yields a boolean, so map
// order never reaches an event/option/view -- determinism is preserved.
func CantAttackParamsReadableForRules(params map[string]string) bool {
	var present1, present2 string
	hasCmp, has1, has2 := false, false, false
	for k, v := range params {
		switch k {
		case "PresentCompare":
			hasCmp = true
		case "IsPresent":
			present1, has1 = v, true
		case "IsPresent2":
			present2, has2 = v, true
		}
		switch k {
		case "Mode", "ValidCard", "Target", "Description", "Secondary",
			"CheckSVar", "SVarCompare", "Condition", "UnlessDefender",
			"IsPresent", "IsPresent2", "PresentCompare", "PresentZone", "ClassBand":
		default:
			return false
		}
	}
	// presentGate is dispatched on the KEY being present, not on the value:
	// an empty/whitespace `IsPresent$` still calls countPresent(""), which
	// matches nothing (count 0). So a blank present spec is not "no gate" --
	// it is a gate that can never see its object, and `PresentCompare$ EQ0`
	// would hold unconditionally and restrict blanket. Reject a present key
	// whose spec is blank, whichever key carries the compare (and whether or
	// not one does).
	spec1 := strings.TrimSpace(present1)
	spec2 := strings.TrimSpace(present2)
	if (has1 && spec1 == "") || (has2 && spec2 == "") {
		return false
	}
	// An orphan compare (PresentCompare$ with no present spec at all) would
	// never be evaluated: presentGate is its only reader and it runs only when
	// a present key is set. Admit the line only when a NON-BLANK spec is there.
	if hasCmp && spec1 == "" && spec2 == "" {
		return false
	}
	// An unread present spec would match nothing, so an EQ0 compare would hold
	// unconditionally and over-restrict; keep such a line skipped whole.
	if spec1 != "" && len(UnknownPredicates(spec1)) != 0 {
		return false
	}
	if spec2 != "" && len(UnknownPredicates(spec2)) != 0 {
		return false
	}
	return true
}

// UnlessDefenderHolds evaluates a CantAttack static's UnlessDefender$ predicate
// (Forge StaticAbilityCantAttackBlock: `unlessDefender.hasProperty(type,
// hostCard.getController(), hostCard, stAb)`, where the defending player is the
// subject and You is the static's controller). It reports whether the SHORT
// predicate on the defending player holds; a CantAttack static is bypassed (the
// creature may attack) exactly when it does. The grammar is Forge's
// PlayerProperty conditional family, the subset the corpus spells, and every
// unsupported property FAILS CLOSED (returns false) so the restriction is
// enforced rather than silently dropped -- the same deny direction every other
// unread static gate takes. A leading `!` negates the whole predicate.
//
// Supported properties:
//
//   - `controls<objSpec>[_<cmp><n>]`: the defender controls at least one (or,
//     with a trailing count token, the compared number of) battlefield object
//     matching objSpec as an object filter. The comma in a spec such as
//     `controlsEnchantment,Permanent.enchanted` is part of the OBJECT spec
//     (Forge's getValidCards list), never a predicate separator.
//   - `hasFewer<Type>sIn<Play|Yard>ThanYou`: the defender controls (or holds in
//     its graveyard) strictly fewer objects of that type than You.
//   - `HasCardsIn<zone>_<type>_<cmp><n>`: the defender's named zone holds the
//     compared number of cards of that type (`Card` counts every card).
//   - `IsPoisoned`: the defender has a poison counter.
//   - `isMonarch`: the defender is the monarch (CR 716.2).
//
// source is the static's source object (the object-spec matcher's Source
// binding) and you is the static's controller. The evaluation is a pure read:
// no event, no state write, and the battlefield/zone walks are the engine's
// one deterministic scan order.
func UnlessDefenderHolds(g *state.Game, spec string, defender, you state.PlayerID, source state.ObjID) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	neg := strings.HasPrefix(spec, "!")
	if neg {
		spec = strings.TrimSpace(spec[1:])
	}
	holds, known := unlessDefenderProperty(g, spec, defender, you, source)
	if !known {
		return false
	}
	if neg {
		return !holds
	}
	return holds
}

// unlessDefenderProperty evaluates ONE UnlessDefender$ property (no `!`). An
// unknown or malformed property returns known=false so a leading `!` cannot
// turn an unread predicate into permission to attack.
func unlessDefenderProperty(g *state.Game, property string, defender, you state.PlayerID, source state.ObjID) (holds, known bool) {
	if int(defender) >= len(g.Players) {
		return false, false
	}
	switch {
	case property == "isMonarch":
		return g.IsMonarch(defender), true
	case property == "IsPoisoned":
		return g.Players[defender].Counter("Poison") > 0, true
	case strings.HasPrefix(property, "controls"):
		// The object spec is everything after "controls"; an optional trailing
		// `_<cmp><n>` narrows an existential read to a count compare.
		objSpec, op, want, counted := splitCountCompare(strings.TrimSpace(property[len("controls"):]))
		if objSpec == "" {
			return false, false
		}
		sc := NewSpecContext(you, source)
		n := int32(0)
		for _, id := range g.Zone(state.ZBattlefield, defender) {
			if MatchesObjectCtx(g, objSpec, g.Obj(id), sc) {
				n++
			}
		}
		if !counted {
			return n > 0, true
		}
		return playerCompare(n, op, want), true
	case strings.HasPrefix(property, "HasCardsIn"):
		// HasCardsIn[zone]_[type]_[comparator]
		parts := strings.Split(strings.TrimPrefix(property, "HasCardsIn"), "_")
		if len(parts) != 3 {
			return false, false
		}
		z, ok := unlessDefenderZone(parts[0])
		if !ok {
			return false, false
		}
		op, want, ok := unlessDefenderCompare(parts[2])
		if !ok {
			return false, false
		}
		return playerCompare(unlessDefenderTypeCount(g, z, defender, parts[1]), op, want), true
	case strings.HasPrefix(property, "hasFewer"):
		// hasFewer[Type]sIn[Play|Yard]ThanYou
		if int(you) >= len(g.Players) {
			return false, false
		}
		body := strings.TrimPrefix(property, "hasFewer")
		i := strings.Index(body, "sIn")
		if i < 0 {
			return false, false
		}
		cardType, tail := body[:i], body[i+len("sIn"):]
		if cardType == "" {
			return false, false
		}
		z := state.ZBattlefield
		switch {
		case strings.HasPrefix(tail, "PlayThan"):
		case strings.HasPrefix(tail, "YardThan"):
			z = state.ZGraveyard
		default:
			return false, false
		}
		return unlessDefenderTypeCount(g, z, defender, cardType) < unlessDefenderTypeCount(g, z, you, cardType), true
	}
	return false, false
}

// unlessDefenderZone maps a Forge zone word in a HasCardsIn property to a
// state zone, failing closed on any zone this build cannot name.
func unlessDefenderZone(word string) (state.Zone, bool) {
	switch word {
	case "Battlefield":
		return state.ZBattlefield, true
	case "Graveyard":
		return state.ZGraveyard, true
	case "Hand":
		return state.ZHand, true
	case "Library":
		return state.ZLibrary, true
	case "Exile":
		return state.ZExile, true
	}
	return 0, false
}

// unlessDefenderCompare parses a Forge comparator token ("GE7") into an
// operator and a threshold.
func unlessDefenderCompare(tok string) (string, int32, bool) {
	if len(tok) < 3 {
		return "", 0, false
	}
	op := tok[:2]
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
	default:
		return "", 0, false
	}
	n, err := strconv.ParseInt(tok[2:], 10, 32)
	if err != nil {
		return "", 0, false
	}
	return op, int32(n), true
}

// unlessDefenderTypeCount counts objects of a Forge type word in a player's
// zone. `Card`/`Any` is the universal type (every object counts), matching
// CardLists.getType's treatment of the base card type; any other word rides
// the shared hasType predicate.
func unlessDefenderTypeCount(g *state.Game, z state.Zone, p state.PlayerID, cardType string) int32 {
	n := int32(0)
	for _, id := range g.Zone(z, p) {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		if strings.EqualFold(cardType, "Card") || strings.EqualFold(cardType, "Any") || hasType(o, cardType) {
			n++
		}
	}
	return n
}

// CantBlockByRestrictionParamsReadable is the parameter whitelist an
// Effect-registered CantBlockBy static must pass before this build enforces
// it (task cbb1; the same discipline CantRestrictionParamsReadable enforces
// for CantAttack/CantSacrifice, so the registration and consultation paths
// cannot disagree): the two-side specs the continuous consultation reads
// (rules/statics.go blockRestricted's registered-effects walk: ValidAttacker$
// against the ATTACKER, ValidBlocker$ against the would-be blocker, the
// historical ValidCard$ fallback), plus display text. A body carrying a
// condition or scoping parameter this build's continuous path does not
// evaluate (Condition$, IsPresent$, CheckSVar$, the Relative$ spellings,
// space_beleren's ValidBlockerRelative$ sector grammar, ...) must not
// register blanket -- a gated "can't be blocked by ..." would become an
// UNCONDITIONAL one, over-restricting -- so it stays the unimplemented Note;
// enforcing it blanket would make a conditional "can't be blocked"
// unconditional, the over-restricting direction for a restriction. Measured
// over the 594 CantBlockBy corpus files (619 raw lines): 48 carry an
// IsPresent$/PresentCompare$/CheckSVar$/SVarCompare$/Condition$ gate or a
// Relative$/ValidDefender$/ValidBlockerRelative$/PresentZone$/EffectZone$
// scoping and stay loud Notes; the other 571 read only the whitelisted
// parameters. Secondary$ is allowed: it marks a Forge-side duplicate for
// modifier composition, and a boolean restriction cannot be applied twice.
func CantBlockByRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidAttacker", "ValidBlocker", "ValidCard", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// MustAttackParamsReadable is the parameter whitelist a MustAttack line must
// pass before effEffect registers it as an Effect-delivered per-player attack
// REQUIREMENT (Territorial Hellkite's `DB$ Effect | StaticAbilities$
// AttackChosen`, and the four plain-SubAbility siblings Knight Rampager,
// Ursine Monstrosity, Raving Dead and Ruhan of the Fomori). It mirrors
// CantRestrictionParamsReadable's shape, with ValidCreature$ in place of
// ValidCard$/Target$ (Forge's MustAttack names the required creature with
// ValidCreature$) and the MustAttack$ player reference itself. A line
// carrying any other parameter (IsPresent$, PresentCompare$, Condition$,
// CheckSVar$, AffectedZone$, ValidPlayer$, ...) names a condition this
// registration path does not evaluate; registering it blanket would
// OVER-require -- the non-permissive direction for a requirement -- so it
// fails closed and is reported unimplemented, which is the pre-registration
// behaviour. Secondary$ is allowed: it marks a Forge-side duplicate for
// modifier composition, and a boolean requirement cannot be applied twice.
func MustAttackParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCreature", "MustAttack", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// MustAttackParamsReadableForRules is the FACE S:-line whitelist: the shared
// MustAttackParamsReadable core EXTENDED by exactly the condition-gate keys
// the rules package's shared continuous gate (rules/layers.go
// continuousGateHolds) evaluates -- IsPresent$, IsPresent2$, PresentCompare$,
// PresentZone$, CheckSVar$, SVarCompare$, Condition$ and ClassBand$. It
// lives here, beside MustAttackParamsReadable, so the two lists cannot drift
// apart unseen: the face route (rules' attackRequirements) CAN evaluate those
// gates -- the evaluator, continuousGateHolds, is rules-side, which is why
// this function cannot simply be MustAttackParamsReadable -- while the
// Effect-delivered route (effEffect's registration above) cannot, so its
// whitelist stays at the core set: registering a gate-bearing line as an
// Effect requirement would apply it blanket and OVER-require, the
// non-permissive direction for a requirement. The superset direction
// (every effect-readable line is face-readable) and the gate-key divergence
// are pinned by rules' TestMustAttackFaceAndEffectWhitelistsAgree.
func MustAttackParamsReadableForRules(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCreature", "MustAttack", "Description", "Secondary",
			"IsPresent", "IsPresent2", "PresentCompare", "PresentZone",
			"CheckSVar", "SVarCompare", "Condition", "ClassBand":
		default:
			return false
		}
	}
	return true
}

// CantBlockUnlessRestrictionParamsReadable is the parameter whitelist a
// CantBlockUnless static must pass before this build enforces it -- used by
// BOTH delivery routes that can register one (effEffect's restriction case
// and registerAnimateStaticAbilities' staticAbilities$ grant), so the two
// paths cannot disagree about what is readable. The readable parameters are
// the mode, the two combat specs the block-prop reader resolves (ValidCard$
// against the blocker, Attacker$ against the attacker), the Cost$ the reader
// prices (rules' blockUnlessCharge), the gate parameters the shared
// continuousGateHolds grammar evaluates, and display text. A static carrying
// any other parameter names a condition or scoping this build does not
// evaluate -- enforcing it blanket would OVER-restrict, the permissive
// direction for a restriction -- so it is skipped/reported instead.
func CantBlockUnlessRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Attacker", "Cost", "Description", "Secondary",
			"IsPresent", "IsPresent2", "CheckSVar", "SVarCompare", "Condition":
		default:
			return false
		}
	}
	return true
}

// CantAttackUnlessRestrictionParamsReadable is the parameter whitelist a
// CantAttackUnless static must pass before this build enforces it -- used by
// BOTH delivery routes that can register one (effEffect's restriction case
// and registerAnimateStaticAbilities' staticAbilities$ grant) AND by rules'
// attackPairCharge pricing read, so the three cannot disagree about what is
// readable. The readable parameters are the mode, the two combat specs the
// attack-prop reader resolves (ValidCard$ against the attacking creature,
// Target$ against the defending player/planeswalker -- the same list
// restrictionPlayerTargetMatches reads), the Cost$ the reader prices
// (rules' attackUnlessCharge), the gate parameters the shared
// continuousGateHolds grammar evaluates, RememberingAttacker$ (which binds
// the attacking creature into the pricing SVar context), and display text
// (Description$ and the TriggerDescription$ an oracle-triggered DB$ Effect
// body writes -- Sivitri's SVar carries TriggerDescription$, not
// Description$). A static carrying any other parameter names a condition or
// scoping this build does not evaluate -- enforcing it blanket would
// OVER-restrict, the permissive direction for a restriction -- so it is
// reported unimplemented instead.
func CantAttackUnlessRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Target", "Cost", "Description", "TriggerDescription", "Secondary", "Attacker",
			"IsPresent", "IsPresent2", "CheckSVar", "SVarCompare", "Condition",
			"RememberingAttacker":
		default:
			return false
		}
	}
	return true
}

// CantSacrificeRestrictionParamsReadable is the parameter whitelist a face
// CantSacrifice static must pass before rules' SacrificeBlocked enforces it
// (task vc-static1). It is the CantAttack list above PLUS the two
// cause-scoping parameters SacrificeBlocked itself evaluates -- ValidCause$
// against actionCause() through the shared stack-kind classifier
// (rules/layers.go causeSpecAdmits) and ForCost$ against the
// cost-driven/effect-driven split of the Host method's callers -- so a line
// carrying them is enforced, not skipped. The Master, Multiplied's
// `ValidCard$ Creature.YouCtrl+token | ValidCause$ Triggered.YouCtrl |
// ForCost$ False` is the filing carrier.
//
// It DELIBERATELY diverges from CantRestrictionParamsReadable: effEffect's
// registration gate keeps the narrower list for BOTH modes, because the
// continuous-effect path (rules' restrictionApplies) reads neither
// parameter and registering such a body would over-restrict blanket -- a
// Cause-scoped CantSacrifice delivered by a DB$ Effect stays an
// unimplemented Note. The asymmetry is the permissive direction for a
// restriction on the path that cannot evaluate the cause.
func CantSacrificeRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Target", "Description", "Secondary", "ValidCause", "ForCost":
		default:
			return false
		}
	}
	return true
}

// CantExileRestrictionParamsReadable is the parameter whitelist a face
// CantExile static must pass before rules' ExileBlocked enforces it -- the
// CantSacrifice list above with the exile restriction's own object scope.
// Readable: the mode, the object spec (ValidCard$, the corpus's dominant
// `Creature.YouCtrl+token` shape; the ValidCards$ plural and the
// ValidObject$ alias are the other spellings the shared spec reader resolves),
// the player spec ValidTarget$ the CantAttack reader shares, the two
// cause-scoping parameters exileBlocked itself evaluates -- ValidCause$
// against actionCause() through the shared stack-kind classifier
// (rules/layers.go causeSpecAdmits) and ForCost$ against the
// cost-driven/effect-driven split of the ExileBlocked callers -- and display
// text. The Master, Multiplied's `ValidCard$ Creature.YouCtrl+token |
// ValidCause$ Triggered.YouCtrl | ForCost$ False` is the corpus's one
// carrier.
//
// It is used by the rules-side face-static walk (rules/layers.go
// exileBlocked) alone; effEffect's registration gate keeps the NARROWER
// CantRestrictionParamsReadable for CantExile, the same asymmetry
// CantSacrificeRestrictionParamsReadable documents -- the cause-scoped body is
// read on the face route that evaluates ValidCause$/ForCost$, while an
// Effect-delivered body carrying those keys stays an unimplemented Note
// rather than registering a blanket prohibition the continuous path cannot
// scope. A line carrying any other parameter names a condition or scoping
// this build does not evaluate; enforcing it blanket would OVER-restrict --
// the permissive direction for a restriction -- so the static is
// skipped/reported instead. Secondary$ is allowed: it marks a Forge-side
// duplicate for modifier composition, and a boolean restriction cannot be
// applied twice.
func CantExileRestrictionParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidCards", "ValidObject", "ValidTarget", "Target", "Description", "Secondary", "ValidCause", "ForCost":
		default:
			return false
		}
	}
	return true
}

// CantPutCounterParamsReadable is the parameter whitelist a CantPutCounter
// static must pass before this build enforces it -- used BOTH by the
// face-static reader (rules/layers.go's PutCounterBlocked activeStatics walk)
// and by effEffect's registration case, so the two paths cannot disagree about
// what is readable. The readable parameters are the restriction's own mode and
// scope (Mode$, the object spec ValidCard$/ValidObject$, the player spec
// ValidPlayer$, the counter kind CounterType$), the AffectedZone$ rider the
// Solemnity object line carries, and display text. Duration$ is readable: the
// lock's own lifetime, consumed by effEffect's CantPutCounter arm (an absent
// Duration$ there is the THIS-TURN lock the corpus's one Effect-delivered
// carrier writes -- see that arm). A static carrying any other
// parameter names a condition or scoping this build does not evaluate
// (ActiveZones$, IsPresent$, CheckSVar$, ...) -- enforcing it blanket would
// OVER-restrict, the permissive direction for a restriction -- so it is
// skipped/reported. Secondary$ is allowed: a Forge-side duplicate for modifier
// composition, and a boolean restriction cannot be applied twice.
func CantPutCounterParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidObject", "ValidPlayer", "CounterType", "AffectedZone", "Duration", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// CantGainLifeParamsReadable is the parameter whitelist a CantGainLife static
// must pass before effEffect's registration case enforces it -- used BOTH by
// effEffect's registration case and (beside it, through
// restrictionPlayerSpecMatches) by rules/replacement_life.go's
// lifeGainForbidden registered-restriction walk, so the two paths cannot
// disagree about what is readable. The readable parameters are the mode, the
// player scope (ValidPlayer$, the only scoping every corpus body names --
// measured spellings You, Player, Player.Opponent and Player.IsRemembered,
// all through the shared player grammar) and display text. Duration$ is NOT
// listed: the lock's own lifetime is consumed by effEffect's
// absentDurationMeansThisTurn gate and effectUntilEOT read, exactly as the
// sibling restriction cases consume it, so an explicit Duration$ never needs
// the whitelist's vouch. A static carrying any other parameter (IsPresent$,
// CheckSVar$, CantBePrevented$, ...) names a condition or scoping this build
// does not evaluate; enforcing it blanket would OVER-restrict -- a gated
// "can't gain life" would become an unconditional one -- so it is
// skipped/reported, which is the pre-registration behaviour and the
// permissive direction for a restriction. Secondary$ is allowed: it marks a
// Forge-side duplicate for modifier composition, and a boolean restriction
// cannot be applied twice.
func CantGainLifeParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidPlayer", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// UnspentManaParamsReadable is the parameter whitelist an UnspentMana static
// must pass before this build enforces it -- used BOTH by the face-static
// reader (rules/statics.go's unspentManaKeep activeStatics walk) and by
// effEffect's registration case, so the two paths cannot disagree about what
// is readable. The readable parameters are the mode, the player scope
// (ValidPlayer$), the colour scope (ManaType$, a comma-separated colour-word
// list effects.ColorLetters parses; absent protects every slot, the Upwelling
// spelling) and display text. Duration$ rides the registration only through
// effEffect's own effectUntilEOT read (an instant/sorcery source's grant is
// the UntilEOT lifetime the oracle's "until end of turn" states), so it is
// NOT readable here: an UnspentMana body naming Duration$ explicitly would
// need a lifetime the whitelist cannot vouch for. A static carrying any other
// parameter names a condition or scoping this build does not evaluate
// (IsPresent$, CheckSVar$, ActiveZones$, ...) -- enforcing it blanket would
// OVER-protect mana that should empty, so it is skipped/reported. Secondary$
// is allowed: a Forge-side duplicate for modifier composition, and a boolean
// keep cannot be applied twice.
func UnspentManaParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidPlayer", "ManaType", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// CanAttackDefenderGrantParamsReadable is the parameter whitelist an
// Effect-granted CanAttackDefender body (a StaticAbilities$ CanAttack grant
// such as Assault Formation's SVar:CanAttack) must pass before effEffect
// registers it as a CanAttackDefender restriction. Readable: the mode, the
// object spec (ValidCard$ — the corpus's dominant Card.EffectSource and
// IsRemembered shapes — plus the ValidCards$ spelling one carrier uses), the
// ValidTarget$ alias, the ValidAttacked$ player gate the face read evaluates,
// and display text. The gate family (IsPresent$/CheckSVar$/Condition$/...)
// is DELIBERATELY excluded: the continuous-effect path cannot evaluate a
// gate, and registering such a body would grant blanket — a Defender
// creature the gate should still wall would attack. The asymmetry with
// CanAttackDefenderParamsReadable below is the same documented divergence
// CantRestrictionParamsReadable vs CantSacrificeRestrictionParamsReadable
// carries: the grant path keeps the narrower list.
func CanAttackDefenderGrantParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidCards", "ValidTarget", "ValidAttacked", "Description", "Secondary":
		default:
			return false
		}
	}
	return true
}

// ManaConvertParamsReadable is the deliberately narrow whitelist for an
// Effect-delivered ManaConvert static. Unknown qualifiers fail closed rather
// than granting a conversion with a scope the payment path cannot evaluate.
// AffectedZone$ is admitted because the real corpus carrier (Abstruse
// Appropriation's `ManaConvert | ValidCard$ Card.IsRemembered | ValidSA$
// Spell.MayPlaySource | AffectedZone$ Exile`) names the zone the remembered
// card is cast FROM; rules/mana_convert.go enforces that scope against the
// cast's origin zone, so admitting it here is not a blanket grant.
func ManaConvertParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidSA", "ValidPlayer", "ManaConversion", "Optional", "EffectZone", "AffectedZone", "Description", "SpellDescription":
		default:
			return false
		}
	}
	return true
}

// IsGrantableCostStaticMode names the cost-modifier modes a GRANT delivers
// (state.ContinuousEffect.CostStaticGranted): the three rules' cost
// collector composes. AlternativeCost/OptionalCost grants are not collected
// through that route and stay on each carrier's unimplemented path.
func IsGrantableCostStaticMode(mode string) bool {
	return mode == "ReduceCost" || mode == "RaiseCost" || mode == "SetCost"
}

// CostStaticParamsReadable is the parameter whitelist an Effect-delivered
// cost-modifier static (Mode$ ReduceCost/RaiseCost/SetCost/AlternativeCost
// behind an AB$ Effect's StaticAbilities$ entry, task
// param:api:Effect.ForgetOnCast) must pass before effEffect registers it
// into the continuous registry. It lists exactly the keys the cost chain's
// own gates evaluate -- rules' costStaticApplies (Type$, ValidCard$,
// ValidSpell$, ValidTarget$, AffectedZone$, IsPresent$, OnlyFirstSpell$,
// RaiseTo$, Secondary$, Relative$, CheckSVar$ + SVarCompare$, Condition$ --
// whose unread values fail closed), costActorMatches (Activator$/Caster$),
// costModifiers' own reads (Amount$, Cost$, Color$, MinMana$,
// IgnoreGeneric$), alternativeCostScopeOK (ValidSA$, ValidPlayer$,
// IsPresent$, EffectZone$, CheckSVar$/CheckSecondSVar$ + their compares,
// ClassBand$), presentGate's PresentZone$/PresentCompare$ and the Announce$
// X binding -- plus the display-only Description$ keys. A line carrying any
// other key would register blanket where that key was meant to scope, so it
// is refused: the unimplemented Note is the permissive direction for a
// grant, exactly the whitelist discipline every other registration arm
// here keeps.
func CostStaticParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "Type", "ValidCard", "ValidSA", "ValidPlayer", "ValidSpell",
			"ValidTarget", "Activator", "Caster", "Amount", "Cost", "Announce",
			"Color", "MinMana", "IgnoreGeneric", "RaiseTo", "OnlyFirstSpell",
			"IsPresent", "PresentZone", "PresentCompare", "CheckSVar", "SVarCompare",
			"CheckSecondSVar", "SecondSVarCompare", "Condition", "EffectZone",
			"AffectedZone", "Secondary", "Relative", "ClassBand",
			"UnlessValidTarget", "PlayerTurn", "Phases",
			"Description", "SpellDescription":
		default:
			return false
		}
	}
	return true
}

// CanAttackDefenderParamsReadable is the parameter whitelist a FACE
// CanAttackDefender static must pass before rules' attacker-legality read
// (rules/attack_defender.go attackAllowedThroughDefender) enforces it. It is
// the grant list above PLUS the gate family (IsPresent$/IsPresent2$/
// CheckSVar$/SVarCompare$/Condition$), which the face read evaluates through
// the shared continuousGateHolds grammar — the same shape
// cantAttackUnlessParamsReadable carries for CantAttackUnless. A static
// carrying any other parameter names a scoping this build does not evaluate;
// skipping it is the conservative direction for a permission (the creature
// stays walled, today's behaviour), never the wrong-wide one.
func CanAttackDefenderParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "ValidCards", "ValidTarget", "ValidAttacked", "Description", "Secondary",
			"IsPresent", "IsPresent2", "CheckSVar", "SVarCompare", "Condition":
		default:
			return false
		}
	}
	return true
}

// absentDurationMeansThisTurn is the ONE home for the restriction modes whose
// Effect-granted bodies write no inline Duration$ yet whose card text names a
// THIS-TURN lifetime. effEffect now gives every absent Duration$ the Forge
// end-of-turn default; this helper records the mode-specific corpus audit and
// keeps the intent explicit at the registration site. An explicit Permanent
// remains a game-lasting effect, while treating these absent values as
// Permanent would outlive the turn the card names, the non-permissive direction
// for a restriction.
//
// The membership test is structural, not per-card: add a mode here only when
// its absent Duration$ is this-turn by the corpus's own oracle text, and the
// registration branch below picks it up without a new copy of the expiry
// logic. An EXPLICIT Duration$ always takes precedence over this list.
//
//   - CantPutCounter: Melira, the Living Cure's Effect-delivered lock is "you
//     can't get additional poison counters this turn"; with no Duration$ the
//     registration must be UntilEOT (cantputcounter1-r2).
//   - CanAttackDefender: the Effect-granted permission family is uniformly
//     "can attack this turn as though it didn't have defender" -- measured,
//     ALL 22 corpus StaticAbilities$ CanAttack bodies write no inline
//     Duration$ (18 Card.EffectSource self-grants, Assault Formation's
//     Creature.IsRemembered, Wakestone Gargoyle's Creature.YouCtrl+withDefender
//     team grant). Krotiq Nestguard's activated grant and Wakestone Gargoyle's
//     both broke before canattackdefender1-r2 (canattackdefender1-r2).
//   - CantBlockBy: Forge's Effect SA with NO Duration$ is a THIS-TURN effect
//     -- the corpus's own convention proves it: every no-Duration unblockable
//     grant is an activated/triggered ability whose oracle says "this turn"
//     (Suspicious Bookcase, Kaito Cunning Infiltrator's +1, Kappa Cannoneer's
//     counter trigger; 108 activated carriers), while the "for as long as"
//     shapes spell Duration$ UntilHostLeavesPlayOrEOT out explicitly and the
//     forever shapes spell Duration$ Permanent (cbb1, joined from main).
//   - CantGainLife: the corpus's ABSENT-Duration population is uniformly
//     this-turn -- measured, the four no-Duration bodies all read "this turn"
//     in their own Description$/oracle text (Skullcrack and Call In a
//     Professional's "players can't gain life this turn"; Atarka's Command
//     and Roiling Vortex's "your opponents can't gain life this turn", the
//     last an activated ability on a battlefield enchantment, so a
//     source-leaves read would over-restrict the rest of the game).
//     Meanwhile the three explicit Duration$ Permanent bodies (Screaming
//     Nemesis, Stigma Lasher, Welcome the Darkness) are unaffected: an
//     EXPLICIT Duration$ always takes precedence over this list.
//
// DELIBERATELY ABSENT: CantAttack (42 bodies -- "Creatures can't attack you"
// and the "during your next turn" shapes are not this-turn), CantTarget (5 --
// "Players and Permanents can't be the targets" is a permanent lock),
// CantPreventDamage (14 -- "Damage can't be prevented" is a permanent lock),
// and CantSacrifice (5 -- "This permanent can't be sacrificed" is a static).
// A mode joins this list only when its ABSENT-Duration population is uniformly
// this-turn (the precise rule: every corpus Effect body that writes NO
// Duration$ names a this-turn lifetime; a body WITH an explicit Duration$ is
// never touched by this list, so a mixed population is safe).
func absentDurationMeansThisTurn(mode string) bool {
	switch mode {
	case "CantPutCounter", "CantBlockBy", "CanAttackDefender", "NumLoyaltyAct", "CombatDamageToughness", "CantGainLife":
		return true
	}
	return false
}

// IsNextTurnDuration reports whether a Duration$ value names the
// controller's NEXT-turn lifetime (UntilYourNextTurn, UntilTheEndOfYourNextTurn)
// -- the two largest non-Permanent durations the wave survey measured. Such
// an effect is neither UntilEOT (which would expire it a full turn early, at
// the end of the current turn) nor source-leaves (which never expires it);
// it gets a real turn-boundary lifetime via ContinuousEffect.UntilTurn,
// computed in rules.Engine.AddContinuous. Exported so rules/layers.go can
// recognise the same spelling effEffect saw; the two largest values by far
// (105 + 70 raw lines), so this closes most of the turn-spanning gap.
func IsNextTurnDuration(dur string) bool {
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "untilyournextturn", "untiltheendofyournextturn":
		return true
	}
	return false
}

// IsUntilYourNextTurn distinguishes the start-of-next-turn boundary from
// UntilTheEndOfYourNextTurn, which lasts through that turn's cleanup.
func IsUntilYourNextTurn(dur string) bool {
	return strings.EqualFold(strings.TrimSpace(dur), "UntilYourNextTurn")
}

// replacementLineWith reads ReplaceWith$ off a parseReplacementLine-built
// static line -- the SVar name of the R: body's own ReplaceWith$ body, not a
// card Params map. Factored into its own function so the paramcensus rot
// guard can classify the read through a tracked helper parameter rather than
// an unclassified local.
func replacementLineWith(params map[string]string) string {
	return params["ReplaceWith"]
}

// replacementRedirectsToExile reports whether an Effect-delivered Event$
// Moved replacement is the plain "exile it instead" redirect: a
// ReplaceWith$ body `DB$ ChangeZone | Defined$ ReplacedCard | Destination$
// Exile` (Origin$/Hidden$ riders only), optionally chaining the one-shot
// self-exile idiom `DB$ ChangeZone | Defined$ Self | Origin$ Command |
// Destination$ Exile` (effects/zone.go ends the registration on it). That is
// exactly the body shape a PRINTED R: line resolves through the same
// replacement dispatcher (Rest in Peace, Leyline of the Void), so the
// Effect-created registration cannot lose the moved object: the replaced
// move is discarded and the body moves the same card to exile.
//
// The line's own parameters must be ones the Moved matcher reads (the
// Fizzle$/Optional$/Layer$ riders and any other stay on the loud Note path),
// and every other body shape -- a library/battlefield destination, counters,
// a RememberChanged$/plot/delayed-trigger chain -- keeps its loud Note.
//
// Before this, Mavinda, Students' Advocate's "if that spell would be put into
// your graveyard, exile it instead" was a Note: the recast card went back to
// the graveyard while the MayPlay grant still named it, so a {0} sorcery
// (Indicate) was recast forever inside one turn (cardfuzz batch5 line 7).
// The same promise is the whole point of Jace, Vryn's Prodigy, Dire Fleet
// Daredevil, Gaea's Will and the other ~20 carriers of this shape.
func replacementRedirectsToExile(params map[string]string, body string, svars map[string]string) bool {
	for k := range params {
		switch k {
		case "Event", "ValidCard", "ValidLKI", "Origin", "Destination", "ActiveZones",
			"EffectZone", "ReplaceWith", "Description":
		default:
			return false
		}
	}
	if replacementBodyAPI(body) != "ChangeZone" {
		return false
	}
	return redirectExileBody(replacementBodyParams(body), svars)
}

// redirectExileBody is replacementRedirectsToExile's body gate: body is the
// Key$ Value map of the ReplaceWith$ SVar body (a parsed SVar line, not a
// card Params map) and svars the face's SVar table its SubAbility$ names.
func redirectExileBody(body map[string]string, svars map[string]string) bool {
	if body["Defined"] != "ReplacedCard" || body["Destination"] != "Exile" {
		return false
	}
	for k, v := range body {
		switch k {
		case "DB", "Defined", "Destination", "Origin", "StackDescription":
		case "Hidden":
			if !strings.EqualFold(v, "True") {
				return false
			}
		case "SubAbility":
			line := svars[v]
			if replacementBodyAPI(line) != "ChangeZone" || !selfExileIdiom(replacementBodyParams(line)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// selfExileIdiom reports whether sub (a parsed SVar body) is exactly the
// one-shot `DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$
// Exile` idiom that ends the Effect's registration (effects/zone.go).
func selfExileIdiom(sub map[string]string) bool {
	return len(sub) == 4 && sub["Defined"] == "Self" && sub["Origin"] == "Command" && sub["Destination"] == "Exile"
}

// replacementBodyParams splits one SVar body line into its Key$ Value map.
func replacementBodyParams(body string) map[string]string {
	params := make(map[string]string)
	for seg := range strings.SplitSeq(body, "|") {
		key, val, ok := strings.Cut(strings.TrimSpace(seg), "$")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return params
}

// replacementLineCantHappen reports whether a parseReplacementLine-built
// replacement body declares Layer$ CantHappen with no body of its own -- the
// complete replacement is stopping the event (Mistrise Village's AntiMagic).
// Factored into its own function so the paramcensus rot guard can classify
// the read through a tracked helper parameter rather than an unclassified
// local, the same shape replacementLineWith takes.
func replacementLineCantHappen(params map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(params["Layer"]), "CantHappen")
}

// replacementLinePrevents reports whether a parseReplacementLine-built
// replacement body is the bodyless full-prevention form: Prevent$ True with
// no ReplaceWith$ body of its own -- the complete replacement is stopping
// the damage (Selfless Squire's RPrevent, task dponce1). Factored into its
// own function so the paramcensus rot guard can classify the read through a
// tracked helper parameter rather than an unclassified local, the same shape
// replacementLineCantHappen takes.
func replacementLinePrevents(params map[string]string) bool {
	return strings.EqualFold(params["Prevent"], "True")
}

// replacementBodyAPI names the API a retained replacement body's head
// resolves to ("DB$ PutCounter | Defined$ ReplacedCard | ..." ->
// "PutCounter"), so an effEffect registration gate can admit exactly the
// body shapes the rules dispatcher handles without hard-coding card names.
// An unparsable body returns "" (and the gate declines it).
func replacementBodyAPI(body string) string {
	head, _, _ := strings.Cut(body, "|")
	kind, api, ok := strings.Cut(strings.TrimSpace(head), "$")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(api) == "" {
		return ""
	}
	return strings.TrimSpace(api)
}

// effectOneShotDelayedMode names the event modes with a non-repeat delayed
// dispatch end to end: events.Apply's DelayedRegister decode recognizes the
// mode prefix without the |EF marker, and rules.checkEventDelayedTriggers has
// a non-EffectRepeat arm that fires it and removes it (one-shot). It is ONE
// home shared by effEffect's OneOff$ True decision and effDelayedTrigger's
// mode admission, so a mode can never be one-shot on one path and inert on
// the other. Mode$ Phase is deliberately absent: a phase registration is
// inherently one-shot (no |EF, consumed by its DelayedPush) and takes the
// Phase arm, not this event-mode one.
func effectOneShotDelayedMode(mode string) bool {
	switch mode {
	case "SpellCast", "ChangesZone", "ChangesController", "DamageDone", "AttackersDeclared":
		return true
	}
	return false
}

// Delayed registrations can express a turn ceiling, but not a continuous
// Effect's source-relative or next-turn lifetime. Reject those forms rather
// than register a promise that can fire after the Effect expires.
func effectTriggerThisTurnDuration(dur string) bool {
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "", "eot", "endofturn", "untilendofturn", "end of turn", "this turn":
		return true
	}
	return false
}

// effectTriggerBody resolves an Effect Triggers$ entry's SVar body: the
// source's own face table first, else the resolving ability's SVar table
// (the lookup the Effect trigger registration loop uses).
func effectTriggerBody(h Host, c *Ctx, name string) string {
	if name == "" {
		return ""
	}
	if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
		if raw := o.Face().SVars[name]; raw != "" {
			return raw
		}
	}
	return c.SVars[name]
}

// effectSelfExileOnCastTrigger reports whether tr is the Effect self-exile
// idiom: a SpellCast trigger whose Execute$ body moves the Effect itself out
// of the Command zone into exile (Forge's effect token; TARDIS's
// `SVar:ExileEffect:Mode$ SpellCast | EffectZone$ Command | ValidCard$
// Card.YouCtrl | Execute$ RemoveEffect | Static$ True` with
// `RemoveEffect:DB$ ChangeZone | Origin$ Command | Destination$ Exile |
// Defined$ Self`). The EffectZone$ Command marker plus the ChangeZone body
// identify it.
func effectSelfExileOnCastTrigger(h Host, c *Ctx, tr cards.Trigger) bool {
	if tr.Mode != "SpellCast" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(tr.ParamStr(cards.PKEffectZone)), "Command") {
		return false
	}
	exec := strings.TrimSpace(tr.ParamStr(cards.PKExecute))
	if exec == "" {
		return false
	}
	body := strings.ReplaceAll(effectTriggerBody(h, c, exec), " ", "")
	return strings.Contains(body, "ChangeZone") &&
		strings.Contains(body, "Origin$Command") &&
		strings.Contains(body, "Destination$Exile")
}

// effectSelfExileOnCastSpec scans an Effect's Triggers$ name list for the
// self-exile-on-cast idiom above and returns the cast spec that ends the
// Effect: the trigger's ValidCard$ (the spell that consumes the Effect), or
// "Card" for a body that names none. Empty when no entry has the shape, so a
// Triggers$-only Effect keeps its existing registrations untouched.
func effectSelfExileOnCastSpec(h Host, c *Ctx, names string) string {
	for name := range strings.FieldsSeq(names) {
		raw := effectTriggerBody(h, c, name)
		if raw == "" {
			continue
		}
		tr, ok := cards.ParseTriggerLine(raw)
		if !ok || !effectSelfExileOnCastTrigger(h, c, tr) {
			continue
		}
		if spec := strings.TrimSpace(tr.ParamStr(cards.PKValidCard)); spec != "" {
			return spec
		}
		return "Card"
	}
	return ""
}

// effectUntilEOT decides expiry for an Effect registration: a one-shot spell
// (instant/sorcery) source, an absent Duration$, or an explicit this-turn
// Duration$ is UntilEOT and is dropped at end-of-turn cleanup
// (rules' EndOfTurnCleanup). An explicit Permanent or source-relative form
// persists while its source stays on the battlefield, the same rule the layer
// effects use. A Duration$ that spans the controller's NEXT turn is NOT
// UntilEOT (it would expire a turn early); it is instead given a real
// turn-boundary lifetime (state.ContinuousEffect.UntilTurn) computed in
// rules.Engine.AddContinuous, so effectUntilEOT returns false for it.
func effectUntilEOT(h Host, source state.ObjID, dur string) bool {
	if strings.TrimSpace(dur) == "" {
		return true
	}
	if IsNextTurnDuration(dur) {
		return false
	}
	if o := h.Game().Obj(source); o != nil {
		if f := o.Face(); f != nil && (f.IsInstant() || f.IsSorcery()) {
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "eot", "endofturn", "untilendofturn", "untilyournextendstep",
		"untilhostleavesplayoreot", "untilendofcombat", "end of turn",
		"this turn", "thisturnandnextturn":
		return true
	}
	return false
}

// effCleanup is "DB$ Cleanup | ClearRemembered$ True": nothing in this build
// persists a Remembered/Imprinted list on an object yet (Ctx.Remembered is a
// per-resolution parameter, not stored state), so there is nothing to
// actually clear. The Note records that the step ran.

// RestrictionPlayerSpecMatches resolves ONE player spec of a restriction's
// Target$ against the defender, with the two extensions the ordinary
// MatchesPlayerSpec grammar cannot answer because its public entry point
// intentionally carries no source object: an IsRemembered clause (Player.
// IsRemembered, and its ! negation and + compounds) resolves against the
// registered effect's captured player set (state.ContinuousEffect.
// RememberedPlayers — Call for Aid's RememberObjects$ TargetedPlayer), not
// against a source object's event-backed list, which a one-shot sorcery
// source does not carry; and a CardOwner clause (Player.CardOwner — Xantcha,
// Sleeper Agent's "can't attack its owner", Alexios's and Elrond's ditto)
// resolves the defender against the restriction source's OWNER, which is not
// its current controller once the source has changed hands. A face static
// passes an empty remembered set, so its IsRemembered clauses match nobody
// (fail closed); a caller with no source (source 0) fails CardOwner closed
// the same way. Today only CantAttack's Target$ walk passes a source: the
// PutCounterBlocked ValidPlayer$ selectors and the CanAttackDefender
// ValidAttacked$ selector deliberately pass 0, keeping their pre-CardOwner
// behavior unchanged.
func RestrictionPlayerSpecMatches(g *state.Game, spec string, defender, controller state.PlayerID, source state.ObjID, rememberedPlayers []state.PlayerID) bool {
	if !strings.Contains(spec, "IsRemembered") && !strings.Contains(spec, "CardOwner") {
		return MatchesPlayerSpec(g, spec, defender, controller)
	}
	for clause := range strings.SplitSeq(spec, "+") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		if neg, has := clauseIsRemembered(clause); has {
			found := false
			for _, p := range rememberedPlayers {
				if p == defender {
					found = true
					break
				}
			}
			if found == neg {
				return false
			}
			continue
		}
		if clauseIsCardOwner(clause) {
			if !playerIsSourceOwner(g, source, defender) {
				return false
			}
			continue
		}
		if !MatchesPlayerSpec(g, clause, defender, controller) {
			return false
		}
	}
	return true
}

// clauseIsCardOwner reports whether one "+"-clause of a player spec is the
// bare Player.CardOwner (or Any.CardOwner) property, which names the source
// object's owner rather than the defending player.
func clauseIsCardOwner(clause string) bool {
	base, qualifier, ok := strings.Cut(strings.TrimSpace(clause), ".")
	if !ok || !strings.EqualFold(strings.TrimSpace(qualifier), "CardOwner") {
		return false
	}
	base = strings.TrimSpace(base)
	return strings.EqualFold(base, "Player") || strings.EqualFold(base, "Any")
}

// playerIsSourceOwner reports whether p owns the restriction source object.
// A source id with no object (the 0 sentinel a source-less caller passes)
// fails closed.
func playerIsSourceOwner(g *state.Game, source state.ObjID, p state.PlayerID) bool {
	o := g.Obj(source)
	return o != nil && o.Owner == p
}

// clauseIsRemembered reports whether one "+"-clause of a player spec carries
// the IsRemembered qualifier (in either polarity, under the spec's own
// dot-separated token grammar) and which polarity it is.
func clauseIsRemembered(clause string) (neg, has bool) {
	for tok := range strings.SplitSeq(clause, ".") {
		tok = strings.TrimSpace(tok)
		if strings.EqualFold(tok, "!IsRemembered") {
			return true, true
		}
		if strings.EqualFold(tok, "IsRemembered") {
			return false, true
		}
	}
	return false, false
}
