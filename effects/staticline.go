package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func cascadeKeywordGrantFromLine(params map[string]string) (kws []string, affected, zone string, ok bool) {
	raw := strings.TrimSpace(params["AddKeyword"])
	if raw == "" {
		return nil, "", "", false
	}
	for _, k := range cards.SplitKeywordList(raw) {
		if !strings.EqualFold(cards.KeywordHead(k), "Cascade") {
			return nil, "", "", false
		}
		kws = append(kws, "Cascade")
	}
	if len(kws) == 0 {
		return nil, "", "", false
	}
	for _, key := range []string{"Condition", "CheckSVar", "SVarCompare", "IsPresent", "IsPresent2", "PresentCompare"} {
		if strings.TrimSpace(params[key]) != "" {
			return nil, "", "", false
		}
	}
	affected = strings.TrimSpace(params["Affected"])
	if affected == "" {
		affected = "Card.Self"
	}
	return kws, affected, strings.TrimSpace(params["AffectedZone"]), true
}

// setMaxHandSizeGrantFromLine reports whether a Mode$ Continuous static body
// (an S: line or an SVar static an Effect SA registers) carries the
// SetMaxHandSize$ grant this build implements, and resolves its readable
// fields. The implemented shape is Affected$ plus SetMaxHandSize$ Unlimited
// or a plain non-negative integer, with only display/placement metadata
// alongside; a value this build cannot price (an SVar name like X or Y, the
// numeric-SVar carriers) fails closed, exactly the way the printed-static
// reader's value read does, so the two routes agree. A condition gate
// (Condition$/CheckSVar$/IsPresent$/...) is not evaluated on this
// registration path, so a line carrying one is refused rather than applied
// blanket -- the permissive direction for a grant.
func setMaxHandSizeGrantFromLine(params map[string]string) (val, affected, zone string, ok bool) {
	val = strings.TrimSpace(params["SetMaxHandSize"])
	if val == "" {
		return "", "", "", false
	}
	if _, ok := HandSizeValueOK(val); !ok {
		return "", "", "", false
	}
	for _, key := range []string{"Condition", "CheckSVar", "SVarCompare", "IsPresent", "IsPresent2", "PresentCompare"} {
		if strings.TrimSpace(params[key]) != "" {
			return "", "", "", false
		}
	}
	affected = strings.TrimSpace(params["Affected"])
	if affected == "" {
		affected = "Card.Self"
	}
	return val, affected, strings.TrimSpace(params["AffectedZone"]), true
}

// HandSizeValueOK is the ONE SetMaxHandSize$ value grammar both the
// printed-static read (rules' maxHandSizeFor) and the Effect-delivery
// whitelist (setMaxHandSizeGrantFromLine) consult, so the two registration
// paths cannot disagree about what is readable. It accepts the literal word
// Unlimited (any casing) or a plain non-negative decimal integer and returns
// the priced maximum; a dynamic value (an SVar name like X or Y) reports
// false, matching the fail-closed direction the printed read already took.
func HandSizeValueOK(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if strings.EqualFold(raw, "Unlimited") {
		return UnlimitedHandSize, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// UnlimitedHandSize is the value SetMaxHandSize$ Unlimited maps to on the
// effects side of the shared grammar: far above any hand a game can assemble,
// so the CR 514.1 discard never triggers. rules' maxHandSizeFor keeps its own
// copy (unlimitedHandSize) because rules must not reach into effects for a
// constant; both are tested to agree by TestHandSizeValueGrammarIsShared.
const UnlimitedHandSize = 1 << 20

func mayPlayGrantFromLine(params map[string]string) (state.ContinuousEffect, bool) {
	ignoreColor, ignoreType, limit, playerTurn, afterStack, ok := mayPlayEffectParams(params, false)
	if !ok {
		return state.ContinuousEffect{}, false
	}
	return state.ContinuousEffect{
		Affects:                params["Affected"],
		AffectedZone:           strings.TrimSpace(params["AffectedZone"]),
		MayPlay:                true,
		MayPlayIgnoreColor:     ignoreColor,
		MayPlayIgnoreType:      ignoreType,
		MayPlayLimit:           limit,
		MayPlayPlayerTurn:      playerTurn,
		MayPlayValidAfterStack: afterStack,
	}, true
}

// mayPlayFreeGrantFromLine builds the FREE-cast may-play ContinuousEffect
// from one parsed static line: MayPlay$ True plus MayPlayWithoutManaCost$
// True (the "you may cast/play it this turn without paying its mana cost"
// shape -- Dauthi Voidwalker, Idol of Endurance, Nicol Bolas, God-Pharaoh,
// Fire Lord Ozai). ok=false is the fail-closed grant: nothing is registered
// rather than a half-read grant going live. The value rides a separate
// ContinuousEffect flag (MayPlayFree) because the printed-S: battlefield
// route's grant entries carry no free read -- the free-cast MayPlay static
// CHANGES what the cast costs, and the field is consumed exactly where the
// plain grant's cost is (rules/mayplay.go's mayPlayGrant).
func mayPlayFreeGrantFromLine(params map[string]string) (state.ContinuousEffect, bool) {
	limit, playerTurn, afterStack, ok := mayPlayEffectFreeParams(params)
	if !ok {
		return state.ContinuousEffect{}, false
	}
	return state.ContinuousEffect{
		Affects:                params["Affected"],
		AffectedZone:           strings.TrimSpace(params["AffectedZone"]),
		MayPlay:                true,
		MayPlayFree:            true,
		MayPlayLimit:           limit,
		MayPlayPlayerTurn:      playerTurn,
		MayPlayValidAfterStack: afterStack,
	}, true
}

// MayPlayStaticParams reports whether a Mode$ Continuous static body (an S:
// line or an SVar static an Effect SA registers) carries the may-play grant
// this build implements, and resolves its readable riders. The
// implemented shape is MayPlay$ True plus an Affected$/AffectedZone$ pair and
// only display/placement metadata; MayPlayIgnoreColor$ (mana as any colour),
// MayPlayIgnoreType$ (mana as any type -- Rakdos, the Muscle's rider: the
// colour widening plus {C} pips payable by any colour), MayPlayLimit$ (an
// integer once-per-turn cap) and Condition$ PlayerTurn
// ("during each of your turns", the Kess/Karador family) are read.
// MayPlayWithoutManaCost$ is the FREE-cast shape, read by its own whitelist
// (MayPlayFreeStaticParams below), never by this one. Anything else --
// MayPlayText$ (it changes what the cast IS, not just where it may come
// from), a Condition$ whose value is not PlayerTurn, a
// ValidAfterStack$/Secondary$ qualifier (it changes when the grant lives),
// or a MayPlayLimit$ value that is not a non-negative integer -- fails
// closed.
//
// The one exception is the printed route's ability-word Condition$
// (Null Summoner's "Threshold -- As long as there are seven or more cards in
// your graveyard, you may cast the exiled card"): this function's only
// callers are rules/layers.go's printed-S: registration, whose static walk
// evaluates every Condition$ through continuousGateHolds BEFORE the grant is
// built (Delirium, Threshold, Metalcraft, Hellbent, ... -- fail CLOSED on any
// value it cannot read), so a printed grant exists exactly while its
// condition holds. A non-PlayerTurn Condition$ is therefore left to that gate
// rather than refused here. The Effect-delivery scans below have no such
// gate and keep refusing it.
func MayPlayStaticParams(params map[string]string) (ignoreColor, ignoreType bool, limit int32, playerTurn bool, ok bool) {
	v, okv := params["MayPlay"]
	if !okv || !strings.EqualFold(strings.TrimSpace(v), "True") {
		return false, false, 0, false, false
	}
	scan := params
	if cond, has := params["Condition"]; has && !strings.EqualFold(strings.TrimSpace(cond), "PlayerTurn") {
		scan = make(map[string]string, len(params))
		for k, val := range params {
			if k != "Condition" {
				scan[k] = val
			}
		}
	}
	ignoreColor, ignoreType, limit, playerTurn, _, ok = mayPlayParamsScan(scan, false, false)
	return ignoreColor, ignoreType, limit, playerTurn, ok
}

// mayPlayEffectParams is the Effect-delivery sibling of MayPlayStaticParams:
// the same MayPlay$ True grammar, but the DB$ Effect registration path
// (mayPlayGrantFromLine) may additionally carry a ValidAfterStack$
// spell-characteristic qualifier (Nahiri, Forged in Fury's
// MayPlay$ True + ValidAfterStack$ Spell.Equipment static). The value is
// returned verbatim to ride state.ContinuousEffect.MayPlayValidAfterStack and
// is NOT interpreted here -- effects must not reach into rules' spec matcher.
// The printed-S: route keeps the stricter MayPlayStaticParams, so
// rules/layers.go's printed grant (which carries no such field) never
// silently drops the qualifier and widens the offer.
func mayPlayEffectParams(params map[string]string, allowFree bool) (ignoreColor, ignoreType bool, limit int32, playerTurn bool, validAfterStack string, ok bool) {
	v, okv := params["MayPlay"]
	if !okv || !strings.EqualFold(strings.TrimSpace(v), "True") {
		return false, false, 0, false, "", false
	}
	return mayPlayParamsScan(params, allowFree, true)
}

// mayPlayEffectFreeParams is the free-cast Effect-delivery sibling of
// MayPlayFreeStaticParams: MayPlay$ True + MayPlayWithoutManaCost$ True plus
// the optional ValidAfterStack$ qualifier the effect route may carry (Nahiri's
// STPlay2: free Equipment casts gated on Spell.Equipment).
func mayPlayEffectFreeParams(params map[string]string) (limit int32, playerTurn bool, validAfterStack string, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(params["MayPlayWithoutManaCost"]), "True") {
		return 0, false, "", false
	}
	_, _, limit, playerTurn, validAfterStack, ok = mayPlayParamsScan(params, true, true)
	return limit, playerTurn, validAfterStack, ok
}

// MayPlayFreeStaticParams reports whether a Mode$ Continuous static body
// carries the FREE-cast may-play grant: MayPlay$ True plus
// MayPlayWithoutManaCost$ True. The free rider changes what the cast costs
// (the mana part is free, CR 118.9), so the PLAIN whitelist above keeps
// refusing it -- the two grants must never be conflated. Everything else is
// the same grammar, read through the ONE shared key scan (mayPlayParams),
// so a rider the plain path rejects is rejected here too: MayPlayText$, a
// Condition$ whose value is not PlayerTurn, a ValidAfterStack$/Secondary$
// qualifier, a MayPlayLimit$ value that is not a non-negative integer, a
// MayPlayPlayer$/IgnoreColor/IgnoreType value (the free shape carries none
// of them in the corpus -- the key scan still rejects them) -- all fail
// closed. MayPlayDontGrantZonePermissions$ cannot co-occur meaningfully
// with WithoutManaCost$ (a DontGrant static only exempts costs); the scan
// rejects it, and MayPlayAltManaCost$/RaiseCost$ likewise -- the free cast
// cannot also carry an alternative cost this registration path cannot
// charge.
func MayPlayFreeStaticParams(params map[string]string) (limit int32, playerTurn bool, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(params["MayPlayWithoutManaCost"]), "True") {
		return 0, false, false
	}
	_, _, limit, playerTurn, _, ok = mayPlayParamsScan(params, true, false)
	return limit, playerTurn, ok
}

// mayPlayParamsScan is the ONE parameter scan every may-play whitelist
// shares. allowFree widens the key whitelist by exactly
// MayPlayWithoutManaCost$ (the caller has already required it to be True);
// allowAfterStack widens it by exactly ValidAfterStack$ (the
// Effect-delivery path accepts the qualifier and carries it verbatim on the
// registered effect for rules to evaluate). Every other unknown key fails
// closed. The ValidAfterStack$ value is returned unread -- effects must not
// interpret rules' spec grammar -- so an unsupported value is fail-closed at
// the matcher, never here.
func mayPlayParamsScan(params map[string]string, allowFree, allowAfterStack bool) (ignoreColor, ignoreType bool, limit int32, playerTurn bool, validAfterStack string, ok bool) {
	for key := range params {
		switch key {
		case "Mode", "MayPlay", "MayPlayIgnoreColor", "MayPlayIgnoreType",
			"MayPlayLimit", "Condition", "Affected", "AffectedZone", "Description", "EffectZone":
			// The keys the implemented grant (and only it) carries.
		case "MayPlayWithoutManaCost":
			if !allowFree {
				return false, false, 0, false, "", false
			}
		case "ValidAfterStack":
			if !allowAfterStack || strings.TrimSpace(params[key]) == "" {
				return false, false, 0, false, "", false
			}
		default:
			return false, false, 0, false, "", false
		}
	}
	limit = 0
	if raw, okv := params["MayPlayLimit"]; okv {
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
		if err != nil || n < 0 {
			// A MayPlayLimit$ value this build cannot enforce must not
			// silently become "unlimited".
			return false, false, 0, false, "", false
		}
		limit = int32(n)
	}
	playerTurn = strings.EqualFold(strings.TrimSpace(params["Condition"]), "PlayerTurn")
	if cond, okv := params["Condition"]; okv && !playerTurn {
		// A Condition$ other than PlayerTurn changes when the grant lives;
		// never register it half-read.
		_, _ = cond, okv
		return false, false, 0, false, "", false
	}
	ignoreColor = strings.EqualFold(strings.TrimSpace(params["MayPlayIgnoreColor"]), "True")
	ignoreType = strings.EqualFold(strings.TrimSpace(params["MayPlayIgnoreType"]), "True")
	if allowAfterStack {
		validAfterStack = strings.TrimSpace(params["ValidAfterStack"])
	}
	return ignoreColor, ignoreType, limit, playerTurn, validAfterStack, true
}

// parseStaticLine parses an S: static body an SVar holds ("Mode$ CantTarget |
// ValidTarget$ Card.IsRemembered | ...") into its mode and parameter map. The
// body has no SP$/AB$/DB$ head, so cards' parseSA is the wrong shape; this is
// the S: line's own grammar (cards/parse.go's "S" case). An empty or
// malformed body degrades to "" mode and a nil map, which the switch in
// effEffect treats as unimplemented rather than as a registration.
// parseReplacementLine parses an Effect's SVar replacement body ("Event$
// DamageDone | ...") using the same key/value grammar as parseStaticLine.
func parseReplacementLine(svars map[string]string, name string) (string, map[string]string) {
	body := strings.TrimSpace(svars[name])
	if body == "" {
		return "", nil
	}
	params := make(map[string]string)
	for seg := range strings.SplitSeq(body, "|") {
		key, val, ok := strings.Cut(strings.TrimSpace(seg), "$")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return params["Event"], params
}

// staticLineParams is a parsed SVar static body, deliberately distinct from
// cards.SA.Params: it is metadata carried by a DB$ Effect's StaticAbilities$
// reference, not a card primitive's parameter map.
type staticLineParams map[string]string

func effectGainsLimitPerTurn(params staticLineParams) int {
	n, err := strconv.Atoi(strings.TrimSpace(params["GainsAbilitiesLimitPerTurn"]))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// effectGainsAbilitiesOfDefined converts an Effect-delivered Continuous
// static's Defined set into the existing activated-ability grant payload.
// Remembered is copied into the resolving context because an Effect's capture
// is persisted on its registration as object ids.
func effectGainsAbilitiesOfDefined(h Host, c *Ctx, params staticLineParams, remembered []state.ObjID) (state.ContinuousEffect, bool) {
	spec := strings.TrimSpace(params["GainsAbilitiesOfDefined"])
	if spec == "" {
		return state.ContinuousEffect{}, false
	}
	definedCtx := *c
	definedCtx.Remembered = make([]state.Target, 0, len(remembered))
	for _, id := range remembered {
		definedCtx.Remembered = append(definedCtx.Remembered, state.Target{Obj: id})
	}
	faces := GainedFacesOfDefined(h, &definedCtx, spec)
	if len(faces) == 0 {
		return state.ContinuousEffect{}, false
	}
	affected := strings.TrimSpace(params["Affected"])
	if affected == "" && strings.TrimSpace(params["AffectedDefined"]) != "" {
		affected = "Card.Self"
	}
	return state.ContinuousEffect{
		Source: c.Source, Controller: c.Controller, Layer: state.LAbilities,
		Affects: affected, AffectedZone: strings.TrimSpace(params["AffectedZone"]),
		GainedFaces: faces, GainsValidAbilities: strings.TrimSpace(params["GainsValidAbilities"]),
		GainsLimitPerTurn: effectGainsLimitPerTurn(params),
	}, true
}

func parseStaticLine(svars map[string]string, name string) (string, staticLineParams) {
	body := strings.TrimSpace(svars[name])
	if body == "" {
		return "", nil
	}
	params := make(staticLineParams)
	mode := ""
	for seg := range strings.SplitSeq(body, "|") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		key, val, ok := strings.Cut(seg, "$")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		params[key] = val
		if key == "Mode" {
			mode = val
		}
	}
	cards.NormalizeAffectedDefined(params)
	return mode, params
}

// ParseStaticLine is the exported form of parseStaticLine: rules reads a
// granted static's SVar body for the whitelist gates that must agree with
// the registration path (staticgoad1's etbCloneWhitelist AddStaticAbilities$
// value check), so the two cannot disagree about the body grammar. One
// parser, two tiers.
func ParseStaticLine(svars map[string]string, name string) (string, map[string]string) {
	mode, params := parseStaticLine(svars, name)
	return mode, params
}

// goadStaticGrantReadable reports whether a Mode$ Continuous static body is
// an entirely readable Goad$ True line: the literal True (any other value —
// Forge's Yes spellings included — is unmodelled), and NO parameter outside
// the display/selector whitelist. A body carrying a condition gate
// (CheckSVar$, IsPresent$) or an additional grant parameter must not
// register blanket — it fails closed to the caller's honest unimplemented
// Note (the shipped-statics convention the EffEffect whitelist arms keep).
// The same gate drives the DB$ Clone AddStaticAbilities$ route (effClone)
// and rules' ETB-clone whitelist value check (rules/cast.go), so all three
// delivery paths agree on what a readable goad grant is.
func goadStaticGrantReadable(params map[string]string) bool {
	if !strings.EqualFold(strings.TrimSpace(params["Goad"]), "True") {
		return false
	}
	for key := range params {
		if !goadStaticGrantReadableKeys1.Has(key) {
			return false
		}
	}
	return true
}

// NumLoyaltyActParamsReadable reports whether an Effect-delivered
// Mode$ NumLoyaltyAct static body is entirely readable: the line's selector
// (ValidCard$), its two grant parameters (Twice$ True, Additional$ N) and
// the display text are the only keys this build evaluates. OnlySourceAbs$
// True is accepted as source-scoping already implied by the corpus's
// `ValidCard$ Card.EffectSource` selector (Comet, Stellar Pup's LoyaltyAbs).
// Any other key is a condition gate or scoping term this build does not
// evaluate, so the caller fails closed to its honest unimplemented Note.
func NumLoyaltyActParamsReadable(params map[string]string) bool {
	for key := range params {
		if !numLoyaltyActParamsReadableKeys2.Has(key) {
			return false
		}
	}
	return true
}

// LoyaltyFlashParamsReadable reports whether an Effect-delivered
// Mode$ CastWithFlash body is the loyalty-timing grant this build evaluates:
// "you may activate loyalty abilities of <ValidCard$> any time you could
// cast an instant" (Jace's Machinations' InstantJace, Teferi, Temporal
// Archmage's emblem). Its ValidSA$ must be exactly Activated.Loyalty and its
// Caster$, when present, You; the rest is the selector and display text.
// Every other CastWithFlash body (the spell-flash grants) stays the honest
// unimplemented Note, so this whitelist widens nothing on the spell side.
// rules' loyaltyAtInstantSpeed reads the registration this admits.
func LoyaltyFlashParamsReadable(params map[string]string) bool {
	loyalty := false
	for key, v := range params {
		switch key {
		case "Mode", "ValidCard", "Description":
		case "ValidSA":
			if strings.TrimSpace(v) != "Activated.Loyalty" {
				return false
			}
			loyalty = true
		case "Caster":
			if strings.TrimSpace(v) != "You" {
				return false
			}
		default:
			return false
		}
	}
	return loyalty
}

// GoadStaticGrantReadable is the exported form of goadStaticGrantReadable:
// rules' etbCloneWhitelist value check (staticgoad1) reads a granted
// AddStaticAbilities$ body through it, so the ETB offer and the effClone
// registration cannot disagree about what a supported goad grant is.
func GoadStaticGrantReadable(params map[string]string) bool {
	return goadStaticGrantReadable(params)
}

// effectRemembered resolves RememberObjects$ into the concrete object ids the
// Effect captured. "Targeted"/"ParentTarget" remember the chosen targets;
// "Remembered" (and creature-flavoured spellings) remember the objects the
// resolution already had, while "Imprinted" reads the source's persistent
// imprint list. An ABSENT RememberObjects$ defaults to "Targeted" (the chosen
// targets), NOT to the source; an UNRECOGNISED member contributes nothing.
// Objects only: a player-only remember yields
// an empty slice, which a restriction whose ValidCard$ is Card.IsRemembered
// then applies to nothing. The player half of the same capture lives in
// effectRememberedPlayers below.
func effectRemembered(h Host, c *Ctx, sa *cards.SA) []state.ObjID {
	ro := sa.ParamStr(cards.PKRememberObjects)
	if ro == "" {
		ro = "Targeted"
	}
	var out []state.ObjID
	for _, member := range strings.Split(ro, "&") {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}
		// A "Valid <filter>" member is a WHOLE member: its filter grammar uses
		// the comma as OR (Kill Switch's "Valid Artifact.Other", the
		// "Creature.blockedBySource,Creature.blockingSource" pair), so the
		// comma split below must not cut it into unknown fragments. Route it
		// through the same fail-closed resolver definedSpec uses.
		if member == "Valid" || strings.HasPrefix(member, "Valid ") {
			if ts, ok := knownDefinedTargets(h, c, member); ok {
				out = appendEffectRememberedObjects(h, out, ts)
			}
			continue
		}
		for _, part := range strings.Split(member, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			switch effectRememberedb5e1Codes.Code(string(part)) {
			case effectRememberedb5e1You:
				out = append(out, c.Source)
			case effectRememberedb5e1Targeted:
				targets := c.Targets
				if c.PickedTargets != nil {
					targets = c.PickedTargets
				}
				out = appendEffectRememberedObjects(h, out, targets)
			case effectRememberedb5e1ParentTarget:
				out = appendEffectRememberedObjects(h, out, parentLinkTargets(c))
			case effectRememberedb5e1Remembered:
				out = appendEffectRememberedObjects(h, out, c.Remembered)
			case effectRememberedb5e1Imprinted:
				// Effect RememberObjects$ Imprinted captures the source's persistent
				// Dig/ChangeZone imprint list (Synth Eradicator's may-play rider).
				if o := h.Game().Obj(c.Source); o != nil {
					for _, id := range o.Imprinted {
						if h.Game().Obj(id) != nil {
							out = append(out, id)
						}
					}
				}
			case effectRememberedb5e1ReplacedCard:
				// The card the enclosing replacement acted on (Opposition Agent's
				// RepExile → DBEffect: the found card the replacement just exiled
				// is the one the may-play grant remembers). Outside a replacement
				// (c.Replaced zero) or after the object ceased to exist, nothing.
				if c.Repl.Replaced != 0 && h.Game().Obj(c.Repl.Replaced) != nil {
					out = append(out, c.Repl.Replaced)
				}
			case effectRememberedb5e1TriggeredCard:
				// The card the firing trigger's event captured (Mistrise Village's
				// Effect RememberObjects$ TriggeredCard: the spell the can't-be-
				// countered promise covers). The SpellCast referent capture binds
				// c.TriggerCard to the cast stack object; a stale id (the spell
				// already resolved) remembers nothing, the same live-object
				// discipline the cases above apply. TriggeredObject(LKICopy) is the
				// same capture under the CounterPlayerAddedAll batch triggers'
				// spelling (Rikku's RememberObjects$ TriggeredObjectLKICopy: the
				// creature the counters landed on).
				if c.TriggerCard != 0 && h.Game().Obj(c.TriggerCard) != nil {
					out = append(out, c.TriggerCard)
				}
			case effectRememberedb5e1ChosenCard:
				// Dauthi Voidwalker and the wider ChooseCard -> Effect family do
				// not set RememberChosen$: the chosen card lives in Ctx.Chosen, or
				// on the event-backed source when a later ability reads it.
				chosen := c.Chosen
				if len(chosen) == 0 {
					if o := h.Game().Obj(c.Source); o != nil {
						chosen = o.Chosen
					}
				}
				out = appendEffectRememberedObjects(h, out, chosen)
			case effectRememberedb5e1RememberedLKI:
				// Object selectors this helper previously left unresolved. Each is
				// a name definedSpec/knownDefinedTargets already resolves, so read
				// the ONE shared resolver rather than re-deriving the referent
				// here: RememberedLKI is the capture-excluding LKI group (never the
				// raw Remembered slice), TriggeredTargetLKICopy prefers the Attached
				// bearer role, TriggeredAttackerLKICopy is the trigger's captured
				// attacker, and DelayTriggerRemembered is the delayed
				// registration's own capture. knownDefinedTargets is fail-closed: an
				// unrecognised spelling answers ok=false and contributes nothing.
				if ts, ok := knownDefinedTargets(h, c, part); ok {
					out = appendEffectRememberedObjects(h, out, ts)
				}
			}
		}
	}
	return out
}

// appendEffectRememberedObjects appends the live object entries of ts to out,
// dropping player targets, the zero id and any id no longer in the game.
// effectRemembered records objects only, and a missing or stale referent must
// contribute nothing rather than the source or a guessed id.
func appendEffectRememberedObjects(h Host, out []state.ObjID, ts []state.Target) []state.ObjID {
	for _, t := range ts {
		if t.IsPlayer || t.Obj == 0 || h.Game().Obj(t.Obj) == nil {
			continue
		}
		out = append(out, t.Obj)
	}
	return out
}

// effectRememberedPlayers resolves RememberObjects$ into the concrete PLAYER
// ids the Effect captured — the player half of effectRemembered, which
// deliberately records objects only (a player-only remember yields an empty
// slice there). The player-flavoured RememberObjects$ spellings are read:
// "TargetedPlayer"/"Targeted" (the chosen player targets — Call for Aid's
// "target opponent" and The Brothers' War's "choose two target players",
// whose remembered selves the registered restrictions then resolve) and
// "TargetedController" (the controller of each captured target — The
// Motherlode, Excavator's DBEffect remembers the defending player of the
// land it destroyed) and "RememberedPlayer"/"RememberedPlayers"/
// "Remembered" (the resolution's
// remembered players — the per-opponent token-then-effect carriers For Each
// of You a Gift, Furygale Flocking, City of the Daleks and Rotted Ones Lay
// Siege bind the RepeatEach loop's current player into Ctx.Remembered, which
// their DBEff's `RememberObjects$ Remembered` then captures). Anything else
// contributes no player, so an effect whose remember the helper cannot read
// registers a restriction with an empty player set (its IsRemembered target
// clauses match nobody — fail closed). Deduplicated, first-capture order.
func effectRememberedPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	ro := sa.ParamStr(cards.PKRememberObjects)
	if ro == "" {
		return nil
	}
	var out []state.PlayerID
	seen := make(map[state.PlayerID]bool)
	add := func(p state.PlayerID) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, part := range strings.FieldsFunc(ro, func(r rune) bool {
		return r == '&' || r == ',' || r == ' '
	}) {
		part = strings.TrimSpace(part)
		switch effectRememberedPlayersb5e2Codes.Code(string(part)) {
		case effectRememberedPlayersb5e2TargetedPlayer:
			targets := c.Targets
			if part == "Targeted" && c.PickedTargets != nil {
				targets = c.PickedTargets
			}
			for _, t := range targets {
				if t.IsPlayer {
					add(t.Player)
				}
			}
		case effectRememberedPlayersb5e2TargetedOrController:
			targets := c.Targets
			if c.PickedTargets != nil {
				targets = c.PickedTargets
			}
			for _, t := range targets {
				if t.IsPlayer {
					add(t.Player)
				} else if o := h.Game().Obj(t.Obj); o != nil {
					add(o.Controller)
				}
			}
		case effectRememberedPlayersb5e2TargetedController:
			// The Motherlode, Excavator's DBEffect remembers the controller of
			// its targeted land -- the defending player its registered
			// CantBlockBy restriction's ValidBlocker$
			// Creature.RememberedPlayerCtrl clause reads. A PLAYER target
			// contributes itself; an OBJECT target contributes its controller
			// (the same object-tail read TargetedOrController makes).
			targets := c.Targets
			if c.PickedTargets != nil {
				targets = c.PickedTargets
			}
			for _, t := range targets {
				if t.IsPlayer {
					add(t.Player)
				} else if o := h.Game().Obj(t.Obj); o != nil {
					add(o.Controller)
				}
			}
		case effectRememberedPlayersb5e2ChosenPlayer:
			// The Black Gate's DBEffect remembers its ChoosePlayer answer
			// beside its targeted player: the same current-resolution set
			// every other ChosenPlayer consumer reads through ChosenTargets.
			for _, t := range ChosenTargets(h.Game(), c) {
				if t.IsPlayer {
					add(t.Player)
				}
			}
		case effectRememberedPlayersb5e2TriggeredTarget:
			// The player the firing trigger's event targeted (Stigma Lasher's
			// DamageDone | ValidTarget$ Player: "that player can't gain life
			// for the rest of the game"). The role is bound at fire time
			// (rules/trigger_referents.go's DamageDone case: ev.Obj==0 => the
			// damaged player) and rides the trigger's stack context into the
			// Effect's resolution. Read the direct TriggerTarget role rather
			// than definedSpec's TriggeredTarget arm: an OBJECT recipient
			// (a DamageDone to a creature) must contribute no player rather
			// than fall back to an unrelated chosen player target -- this
			// helper is the player half, beside the TargetedPlayer sibling.
			if c.TriggerTarget.IsPlayer {
				add(c.TriggerTarget.Player)
			}
		case effectRememberedPlayersb5e2PlayerIsRemembered:
			// Screaming Nemesis's DBEffect RememberObjects$ Player.IsRemembered:
			// the Effect is created INSIDE the resolution whose DealDamage
			// RememberDamaged$ True just remembered the damaged player, so it
			// must capture that player from the live Ctx.Remembered set (the
			// persistent-list precedence of definedSpec's Player.IsRemembered
			// arm is for a later, independent resolution). Same body as the
			// Remembered* case beside it.
			for _, t := range c.Remembered {
				if t.IsPlayer {
					add(t.Player)
				}
			}
		case effectRememberedPlayersb5e2RememberedPlayer:
			for _, t := range c.Remembered {
				if t.IsPlayer {
					add(t.Player)
				}
			}
		}
	}
	return out
}

// CantRestrictionParamsReadable is the parameter whitelist a CantAttack /
// CantSacrifice static must pass before this build enforces it — used BOTH by
// the face-static readers (rules/layers_restrict.go's SacrificeBlocked and
// rules/combat's AttackBlocked activeStatics walks) and by effEffect's registration case (an Effect body
// carrying an unreadable parameter must not register blanket, so the two
// registration paths cannot disagree about what is readable): Mode$, the
// ValidCard$ object spec, the Target$ player spec, and display text only.
// A static carrying any other parameter (UnlessDefender$, IsPresent$,
// Cost$, CheckSVar$, ValidSA$, ...) names a condition or scoping this build
// does not evaluate; enforcing it blanket would OVER-restrict — a "can't
// attack unless ..." would become "can't attack at all", and a creature a
// MustAttack static requires could be left without a single legal pair — so
// the static is skipped/reported, which is the pre-registration behaviour and
// the permissive direction for a restriction. Secondary$ is allowed: it marks
// a Forge-side duplicate for modifier composition, and a boolean restriction
// cannot be applied twice.

var goadStaticGrantReadableKeys1 = state.NewNameSet("Mode", "Affected", "Description", "Goad")

var numLoyaltyActParamsReadableKeys2 = state.NewNameSet("Mode", "ValidCard", "Twice", "Additional", "OnlySourceAbs", "Description")

const (
	effectRememberedb5e1You           uint16 = 1 // "You", "Self", "Source"
	effectRememberedb5e1Targeted      uint16 = 2 // "Targeted", "ThisTargetedCard"
	effectRememberedb5e1ParentTarget  uint16 = 3 // "ParentTarget"
	effectRememberedb5e1Remembered    uint16 = 4 // "Remembered", "Remembered.Creature", "Remembered.Permanent", "RememberedCard"
	effectRememberedb5e1Imprinted     uint16 = 5 // "Imprinted"
	effectRememberedb5e1ReplacedCard  uint16 = 6 // "ReplacedCard"
	effectRememberedb5e1TriggeredCard uint16 = 7 // "TriggeredCard", "TriggeredObject", "TriggeredObjectLKICopy"
	effectRememberedb5e1ChosenCard    uint16 = 8 // "ChosenCard"
	effectRememberedb5e1RememberedLKI uint16 = 9 // "RememberedLKI", "TriggeredAttackerLKICopy", "TriggeredTargetLKICopy", "DelayTriggerRemembered"
)

var effectRememberedb5e1Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "You", Val: effectRememberedb5e1You},
	state.StrEntry[uint16]{Key: "Self", Val: effectRememberedb5e1You},
	state.StrEntry[uint16]{Key: "Source", Val: effectRememberedb5e1You},
	state.StrEntry[uint16]{Key: "Targeted", Val: effectRememberedb5e1Targeted},
	state.StrEntry[uint16]{Key: "ThisTargetedCard", Val: effectRememberedb5e1Targeted},
	state.StrEntry[uint16]{Key: "ParentTarget", Val: effectRememberedb5e1ParentTarget},
	state.StrEntry[uint16]{Key: "Remembered", Val: effectRememberedb5e1Remembered},
	state.StrEntry[uint16]{Key: "Remembered.Creature", Val: effectRememberedb5e1Remembered},
	state.StrEntry[uint16]{Key: "Remembered.Permanent", Val: effectRememberedb5e1Remembered},
	state.StrEntry[uint16]{Key: "RememberedCard", Val: effectRememberedb5e1Remembered},
	state.StrEntry[uint16]{Key: "Imprinted", Val: effectRememberedb5e1Imprinted},
	state.StrEntry[uint16]{Key: "ReplacedCard", Val: effectRememberedb5e1ReplacedCard},
	state.StrEntry[uint16]{Key: "TriggeredCard", Val: effectRememberedb5e1TriggeredCard},
	state.StrEntry[uint16]{Key: "TriggeredObject", Val: effectRememberedb5e1TriggeredCard},
	state.StrEntry[uint16]{Key: "TriggeredObjectLKICopy", Val: effectRememberedb5e1TriggeredCard},
	state.StrEntry[uint16]{Key: "ChosenCard", Val: effectRememberedb5e1ChosenCard},
	state.StrEntry[uint16]{Key: "RememberedLKI", Val: effectRememberedb5e1RememberedLKI},
	state.StrEntry[uint16]{Key: "TriggeredAttackerLKICopy", Val: effectRememberedb5e1RememberedLKI},
	state.StrEntry[uint16]{Key: "TriggeredTargetLKICopy", Val: effectRememberedb5e1RememberedLKI},
	state.StrEntry[uint16]{Key: "DelayTriggerRemembered", Val: effectRememberedb5e1RememberedLKI},
)

const (
	effectRememberedPlayersb5e2TargetedPlayer       uint16 = 1 // "TargetedPlayer", "Targeted"
	effectRememberedPlayersb5e2TargetedOrController uint16 = 2 // "TargetedOrController"
	effectRememberedPlayersb5e2TargetedController   uint16 = 3 // "TargetedController"
	effectRememberedPlayersb5e2ChosenPlayer         uint16 = 4 // "ChosenPlayer"
	effectRememberedPlayersb5e2TriggeredTarget      uint16 = 5 // "TriggeredTarget"
	effectRememberedPlayersb5e2PlayerIsRemembered   uint16 = 6 // "Player.IsRemembered"
	effectRememberedPlayersb5e2RememberedPlayer     uint16 = 7 // "RememberedPlayer", "RememberedPlayers", "Remembered"
)

var effectRememberedPlayersb5e2Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "TargetedPlayer", Val: effectRememberedPlayersb5e2TargetedPlayer},
	state.StrEntry[uint16]{Key: "Targeted", Val: effectRememberedPlayersb5e2TargetedPlayer},
	state.StrEntry[uint16]{Key: "TargetedOrController", Val: effectRememberedPlayersb5e2TargetedOrController},
	state.StrEntry[uint16]{Key: "TargetedController", Val: effectRememberedPlayersb5e2TargetedController},
	state.StrEntry[uint16]{Key: "ChosenPlayer", Val: effectRememberedPlayersb5e2ChosenPlayer},
	state.StrEntry[uint16]{Key: "TriggeredTarget", Val: effectRememberedPlayersb5e2TriggeredTarget},
	state.StrEntry[uint16]{Key: "Player.IsRemembered", Val: effectRememberedPlayersb5e2PlayerIsRemembered},
	state.StrEntry[uint16]{Key: "RememberedPlayer", Val: effectRememberedPlayersb5e2RememberedPlayer},
	state.StrEntry[uint16]{Key: "RememberedPlayers", Val: effectRememberedPlayersb5e2RememberedPlayer},
	state.StrEntry[uint16]{Key: "Remembered", Val: effectRememberedPlayersb5e2RememberedPlayer},
)
