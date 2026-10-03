package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// isLoyaltyAbility reports whether ab is a planeswalker loyalty ability
// (CR 606): it carries the Planeswalker$ parameter (case-insensitive -- three
// corpus lines spell it "true"), or its parsed Cost$ contains an
// AddCounter/SubCounter part of the LOYALTY kind. The OR is load-bearing on
// the cost side: the dynamic [-X] form SubCounter<X/LOYALTY> (20 raw lines)
// parses into an announced SubCounter part of the LOYALTY kind (see
// rules/mana.go's subCounterCost), so the cost alone still identifies it even
// if a hypothetical carrier omitted the parameter; the param additionally
// covers every fixed [+N]/[-N] shape (978 raw lines, 977 carrying the
// parameter) as well as the 20 dynamic [-X] lines, every one of which carries
// it.
func isLoyaltyAbility(ab *cards.SA) bool {
	return isLoyaltyAbilityCost(ab, ParseCost(ab.ParamStr(cards.PKCost)))
}

// isLoyaltyAbility is the engine-owned form of the loyalty classifier. Its
// card-script cost is configured text, so use the immutable parser sidecar.
func (e *Engine) isLoyaltyAbility(ab *cards.SA) bool {
	raw := ab.ParamStr(cards.PKCost)
	if !containsLoyaltyFold(raw) {
		// No AddCounter/SubCounter part of raw can carry a LOYALTY spec (each
		// part's Spec is a substring of the raw text, and no non-ASCII rune
		// case-folds onto l/o/y/a/t), so only the Planeswalker$ marker can
		// classify it: skip the parsed-cost lookup and its large Cost copy
		// (isLoyaltyMarked is isLoyaltyAbilityCost's marker half, which is
		// its whole answer for a cost with no counter parts).
		return isLoyaltyMarked(ab)
	}
	return isLoyaltyAbilityRef(ab, e.costRef(raw))
}

// containsLoyaltyFold reports whether s contains "loyalty" in any ASCII case.
func containsLoyaltyFold(s string) bool {
	const w = "loyalty"
	for i := 0; i+len(w) <= len(s); i++ {
		if s[i]|0x20 != 'l' {
			continue
		}
		j := 1
		for ; j < len(w); j++ {
			if s[i+j]|0x20 != w[j] {
				break
			}
		}
		if j == len(w) {
			return true
		}
	}
	return false
}

func isLoyaltyAbilityCost(ab *cards.SA, c Cost) bool {
	return isLoyaltyAbilityRef(ab, &c)
}

// isLoyaltyAbilityRef is isLoyaltyAbilityCost over a read-only cost pointer
// (a shared compiled cost from costRef is never copied or written).
func isLoyaltyAbilityRef(ab *cards.SA, c *Cost) bool {
	if isLoyaltyMarked(ab) {
		return true
	}
	for _, part := range c.AddCounter {
		if strings.EqualFold(part.Spec, "LOYALTY") {
			return true
		}
	}
	for _, part := range c.SubCounter {
		if strings.EqualFold(part.Spec, "LOYALTY") {
			return true
		}
	}
	return false
}

// isLoyaltyMarked reports whether ab carries the Planeswalker$ True marker,
// the half of isLoyaltyAbilityCost that reads no cost.
func isLoyaltyMarked(ab *cards.SA) bool {
	if v, ok := ab.Params["Planeswalker"]; ok && strings.EqualFold(strings.TrimSpace(v), "True") {
		// Ultimate$ (Ugin, Eye of the Storms' [-X]: AB$ ChangeZone ...
		// Ultimate$ True) marks the planeswalker's ultimate for Forge's
		// deck-tooling and the client's loyalty-UI presentation; the rules
		// meaning -- a loyalty ability, once per permanent per turn (CR
		// 606.3) -- is already covered by the Planeswalker$ marker this gate
		// reads. The recognition keeps the parameter census honest (its value
		// is discarded, so it is read only on this branch rather than on every
		// ability the offer walk classifies); the presentation half is named
		// in the deck import report's Issues.
		_ = ab.Params["Ultimate"]
		return true
	}
	return false
}

// loyaltyActivationsThisTurn counts how many loyalty abilities of the
// permanent id have been activated in its current battlefield stint this turn.
// It folds the whole replayable log forward because both facts an AbilityPush
// needs are historical: the source's active face at that event, and whether a
// MoveZone ACTUALLY crossed the battlefield boundary. Event.From is advisory
// (events.Move deliberately uses the object's recorded zone instead), so it
// must not decide a stint boundary.
//
// Every genesis object starts outside the battlefield and on face zero. The
// fold keeps just those two facts for id. MoveZone's To is authoritative after
// Apply succeeds, so a same-zone re-append leaves onBattlefield unchanged even
// when a malformed From claims otherwise. FlipFace is applied after any push
// on its old face, exactly as events.Apply does. TurnChange resets the count
// but deliberately retains the folded zone and face for the next turn.
func (e *Engine) loyaltyActivationsThisTurn(id state.ObjID) int {
	o := e.G.Obj(id)
	if o == nil || o.Card == nil || len(o.Card.Faces) == 0 {
		return 0
	}
	// The count resets at the current turn's TurnChange, so only this turn's
	// events can count; the stint and face they start from is the kept
	// prefix fold (activation_count_index.go).
	start := e.turnStart()
	onBattlefield, faceIdx := e.loyaltyStintAt(o, start)
	used := e.loyaltyActivationsFrom(o, start, onBattlefield, faceIdx)
	if walkCacheVerify {
		if want := e.loyaltyActivationsFrom(o, 0, false, 0); want != used {
			panic(fmt.Sprintf("rules: loyalty activations of obj %d from the turn start %d, the whole-log fold %d", id, used, want))
		}
	}
	return used
}

// loyaltyActivationsFrom is loyaltyActivationsThisTurn's fold over the log
// from index from, starting at the given stint and face.
func (e *Engine) loyaltyActivationsFrom(o *state.Object, from int, onBattlefield bool, faceIdx int) int {
	id := o.ID
	used := 0
	if o.IsToken {
		// A token is minted straight onto the battlefield: TokenCreate (and
		// the copy-mint kinds) fold AddObject + Move with no MoveZone event,
		// so the fold would never see it enter and would count none of its
		// activations (a fresh empower Jace token could [-1] and then [-3]
		// in one turn). A token that leaves ceases to exist (CR 111.7), so
		// its one stint runs from its minting; a leaving MoveZone still
		// ends it below.
		onBattlefield = true
	}
	for _, ev := range e.L.Events[from:] {
		switch ev.Kind {
		case events.TurnChange:
			// CR 606.3's window is a turn, not a player's own turn.
			// Mirror events.Apply's validity gate: an invalid player leaves
			// Game.Turn unchanged, so its rejected event cannot open a new
			// loyalty-activation window in this historical fold either.
			if int(ev.Player) < len(e.G.Players) {
				used = 0
			}

		case events.MoveZone:
			if ev.Obj != id || !ev.To.Valid() {
				continue
			}
			nextOnBattlefield := ev.To == state.ZBattlefield
			if onBattlefield != nextOnBattlefield {
				// CR 400.7: every real departure or entry starts a new
				// permanent stint. This is derived from the folded zone,
				// never the caller-controlled Event.From.
				used = 0
			}
			onBattlefield = nextOnBattlefield

		case events.AbilityPush:
			if ev.Obj != id || !onBattlefield || int(ev.Player) >= len(e.G.Players) ||
				ev.Amount < 0 || faceIdx >= len(o.Card.Faces) {
				continue
			}
			// Amount indexes the active face's ability list. Check the exact
			// face and bounds that events.Apply used at push time.
			f := o.Card.Faces[faceIdx]
			if f != nil && int(ev.Amount) < len(f.Abilities) && e.isLoyaltyAbility(f.Abilities[int(ev.Amount)]) {
				used++
			}

		case events.GainedAbilityPush:
			// A GAINED loyalty activation (GainsAbilitiesOf$, Nicol Bolas
			// Dragon-God's class) counts toward the same CR 606.3
			// once-per-permanent window: the foreign face's ability at index
			// Amount is the loyalty ability that was activated FROM id, so the
			// printed and gained activations share one per-permanent tally.
			if ev.Obj != id || !onBattlefield || len(ev.IDs) == 0 {
				continue
			}
			foreign := e.G.Obj(ev.IDs[0])
			if foreign == nil || foreign.Face() == nil {
				continue
			}
			abilities := foreign.Face().Abilities
			if ev.Amount < 0 || int(ev.Amount) >= len(abilities) {
				continue
			}
			if e.isLoyaltyAbility(abilities[int(ev.Amount)]) {
				used++
			}

		case events.GrantAbilityPush:
			// An SVar-anchored GRANTED loyalty ability (an AddAbility$ static
			// whose body carries Planeswalker$ True -- Rowan's Talent's
			// "Enchanted planeswalker has [+1]: ...") is a loyalty ability of
			// THIS permanent (the recipient) and shares its CR 606.3 tally.
			// Counter names the body on the grantor (IDs[0]); a grantor that
			// has since left resolves through grantedSAFrom's recipient
			// fallback or not at all, and an unresolvable body is not counted.
			if ev.Obj != id || !onBattlefield || len(ev.IDs) == 0 || ev.Counter == "" {
				continue
			}
			if body := e.grantedSAFrom(ev.IDs[0], id, ev.Counter); body != nil && e.isLoyaltyAbility(body) {
				used++
			}

		case events.FlipFace:
			if ev.Obj == id && ev.Amount >= 0 && int(ev.Amount) < len(o.Card.Faces) {
				faceIdx = int(ev.Amount)
			}
		}
	}
	return used
}

// loyaltyAbilityLimit resolves how many loyalty abilities the permanent id
// may have activated this turn: 1 (CR 606.3) raised by every live
// S:Mode$ NumLoyaltyAct static whose ValidCard$ matches the permanent --
// Oath of Teferi's "twice each turn rather than only once" (Twice$ True,
// ValidCard$ Planeswalker.YouCtrl) and Urza, Lord Protector's self-scoped
// same (ValidCard$ Card.Self). Any matching Twice$ True raises the base limit
// to 2 (max, so two stacked Twice statics do not compound); every matching
// Additional$ N then adds N -- Forge's two parameters are accumulated
// separately so their result cannot depend on battlefield scan order. An
// additional-activation grant (The Chain Veil's "as though none of its loyalty
// abilities had been activated") therefore stacks on top of a twice grant
// rather than being absorbed by it. Statics that match neither parameter leave
// the limit alone. The ValidCard$ match resolves against the static's own source
// and controller (e.specCtx), exactly as castRestricted and abilityRestricted
// resolve theirs.
//
// Both delivery routes are read (task pw-numloyaltyact): a printed S: line
// (activeStatics' APNAP walk) and an Effect-delivered one -- Kaito, Dancing
// Shadow's PWTwice, Comet, Stellar Pup's LoyaltyAbs and Urza Assembles the
// Titans' PWTwice register a NumLoyaltyAct registry entry (effects' effEffect),
// read here in registry order. A granted line's own Remembered set is bound
// for the `Card.IsRemembered` spelling, exactly as combat.staticGoadLines binds it
// for Goad. Both routes share one accumulator so neither can drift from the
// other's Twice/Additional combination rule.
func (e *Engine) loyaltyAbilityLimit(id state.ObjID) int {
	twice := false
	additional := 0
	// apply folds one live NumLoyaltyAct line into the accumulator. The
	// ValidCard$ spec is matched against the permanent with the static's own
	// source, controller and remembered set bound (the same binding
	// restrictionApplies and combat.goadLineMatches use).
	apply := func(params map[string]string, source state.ObjID, controller state.PlayerID, remembered []state.ObjID) {
		sc := e.specCtx(source, controller)
		for _, r := range remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if !e.matchesSpec(params["ValidCard"], id, sc) {
			return
		}
		if params["Twice"] == "True" {
			twice = true
		}
		if raw, ok := params["Additional"]; ok {
			additional += int(parseAmount(raw, 0))
		}
	}
	for _, sv := range e.activeStatics("NumLoyaltyAct") {
		apply(sv.Params, sv.Source, sv.Controller, nil)
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "NumLoyaltyAct" {
			continue
		}
		apply(ce.RestrictParams, ce.Source, ce.Controller, ce.Remembered)
	}
	base := 1
	if twice {
		base = 2
	}
	return base + additional
}

// activationUsedCount counts this object's activations of one ability over
// the replayable event log. The identity has two shapes, exactly as
// boastGateOK reads them: a PRINTED or mana activation mints an AbilityPush /
// ManaActivate whose Amount is the ability's FLAT pile index (CR 702.140d,
// top face first then under-cards); a GRANTED activation (AddAbility$ /
// Animate) mints a DelayedPush or GrantAbilityPush whose Amount is -1 and
// whose Counter names the granting SVar instead. ability < 0 selects the
// granted shape, svar == "" the printed shape -- so one scanner serves both
// ActivationLimit$ (thisTurn) and GameActivationLimit$ (the whole game), and
// a third caller cannot forget one of them.
//
// thisTurn bounds the walk at the latest TurnChange: the per-turn limit's
// window (CR 606.3's "this turn", and Forge's ActivationLimit$, which
// ActivationTable resets each turn). The per-game walk does NOT break there.
// Both walks stop at the object's latest zone change (objectStintStart):
// under CR 400.7 a permanent that leaves and returns -- bounced and recast,
// flickered -- is a new object, and "Activate only once" (CR 602.5b),
// "only once each turn", Exhaust and Power-up all belong to the object, so
// the new object's ability is available again (Mild-Mannered Librarian).
func (e *Engine) activationUsedCount(id state.ObjID, ability int, svar string, thisTurn bool) int {
	if !thisTurn {
		// The whole stint's count, folded on incrementally
		// (activation_count_index.go).
		return e.gameActivationsUsed(id, ability, svar)
	}
	used := 0
	stint := e.objectStintStart(id)
	for i := len(e.L.Events) - 1; i >= stint; i-- {
		ev := e.L.Events[i]
		if thisTurn && ev.Kind == events.TurnChange {
			break
		}
		if ev.Obj != id {
			continue
		}
		switch ev.Kind {
		case events.AbilityPush, events.ManaActivate:
			// A ManaActivate carrying IDs is a GAINED mana activation
			// (gainedManaRef): its Amount indexes the foreign face, not
			// this object's pile, so it is never a printed activation.
			if ev.Kind == events.ManaActivate && len(ev.IDs) > 0 {
				continue
			}
			if ability >= 0 && ev.Amount == int32(ability) {
				used++
			}
		case events.DelayedPush, events.GrantAbilityPush:
			if svar != "" && ev.Counter == svar {
				used++
			}
		}
	}
	return used
}

// activationLimitReachedAt reports whether this object has already activated
// the ability at the FLAT pile index ability as many times as its
// ActivationLimit permits this turn. merged selects the face whose SVar table
// a computed limit resolves against (0 = the top face). The limit itself is
// resolved by resolveActivationLimitAt: a literal integer is used directly,
// and a computed expression (an SVar name or an inline Count$...) is
// evaluated through the effects count path, so a limit such as Withering
// Wisps' "number of snow Swamps you control" is enforced rather than silently
// ignored. A limit that resolves to zero or to fewer activations than have
// already been used withholds the offer. An expression that genuinely cannot
// be resolved stays unenforced (today's behaviour): resolveActivationLimitAt
// reports ok=false.
func (e *Engine) activationLimitReachedAt(id state.ObjID, p state.PlayerID, ability int, raw string, merged int) bool {
	limit, ok := e.resolveActivationLimitAt(id, p, raw, merged)
	if !ok || limit < 0 {
		return false
	}
	return e.activationUsedCount(id, ability, "", true) >= limit
}

// additionalActivationLimit returns the largest finite MinLimit$ supplied by
// an applicable Activations static, or baseline when none applies. Forge's
// MinLimit is an absolute ceiling (e.g. 2 means twice, not baseline + 2).
// Negative/unparseable limits are deliberately ignored: they represent a
// different unbounded/conditional rule and must not disable a finite cap.
func (e *Engine) additionalActivationLimit(id state.ObjID, actor state.PlayerID, ab *cards.SA, baseline int) int {
	limit := baseline
	for _, sv := range e.activeStatics("Activations") {
		validSA := strings.TrimSpace(sv.ParamStr(cards.PKValidSA))
		matched := false
		for alt := range strings.SplitSeq(validSA, ",") {
			parts := strings.SplitN(strings.TrimSpace(alt), ".", 2)
			if len(parts) != 2 || parts[0] != "Activated" {
				continue
			}
			switch parts[1] {
			case "Exhaust":
				matched = strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKExhaust)), "True")
			case "PowerUp":
				matched = strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKPowerUp)), "True")
			}
			if matched {
				break
			}
		}
		if !matched {
			continue
		}
		if spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard)); spec != "" &&
			!e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			continue
		}
		if !e.actorMatches(sv, "ValidPlayer", actor) {
			continue
		}
		if turn := strings.TrimSpace(sv.ParamStr(cards.PKPlayerTurn)); turn != "" {
			if turn != "You" || e.G.Active != actor {
				continue
			}
		}
		if !e.checkSVarHolds(sv) {
			continue
		}
		min, err := strconv.Atoi(strings.TrimSpace(sv.Params["MinLimit"]))
		if err == nil && min > limit {
			limit = min
		}
	}
	return limit
}

// activationLimitBlocked is the ONE gate every activation offer site calls
// for ActivationLimit$ (this turn), GameActivationLimit$ (the whole game),
// Exhaust$ True, and PowerUp$ True (once per host card per game). All are
// read here so a new offer
// site cannot miss one -- the printed and granted loops in this file and the
// mana walk in mana_activation.go all funnel through it. svar is the
// granted-ability identity ("" for a printed ability); merged selects the face
// a computed limit resolves against. The per-GAME count is scanned with the
// same identity shapes and no turn boundary, but only over the object's
// current zone stint (see activationUsedCount).
//
// A limit that resolves to zero or to fewer activations than already used
// withholds; an unresolvable expression stays unenforced, exactly as the
// per-turn gate already documented.
func (e *Engine) activationLimitBlocked(p state.PlayerID, id state.ObjID, sa *cards.SA, ability int, svar string, merged int) bool {
	if sa == nil {
		return false
	}
	if raw, ok := sa.Param(cards.PKActivationLimit); ok {
		if limit, ok := e.resolveActivationLimitAt(id, p, raw, merged); ok && limit >= 0 &&
			e.activationUsedCount(id, ability, svar, true) >= limit {
			return true
		}
	}
	if raw, ok := sa.Param(cards.PKGameActivationLimit); ok {
		if limit, ok := e.resolveActivationLimitAt(id, p, raw, merged); ok && limit >= 0 {
			limit = e.additionalActivationLimit(id, p, sa, limit)
			if e.activationUsedCount(id, ability, svar, false) >= limit {
				return true
			}
		}
	}
	// Exhaust$ True and PowerUp$ True use the same per-object counter as
	// GameActivationLimit$: it spans the whole game but not a zone change
	// (CR 400.7 -- a permanent that leaves and returns is a new object whose
	// exhaust ability can be activated again). Activations statics raise this finite ceiling; they
	// never make the ability unlimited unless a supported static explicitly
	// has a negative MinLimit; those conditional/unbounded statics are not modeled.
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKExhaust)), "True") ||
		strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKPowerUp)), "True") {
		limit := e.additionalActivationLimit(id, p, sa, 1)
		if limit >= 0 && e.activationUsedCount(id, ability, svar, false) >= limit {
			return true
		}
	}
	return false
}

// boastGateOK implements CR 702.142's Boast activation restriction for an
// ability whose SA carries Boast$ True: the ability may be activated only if
// its source creature attacked this turn, and only once each turn. Both
// halves are read from replay-derivable state: "attacked this turn" is the
// event-folded Object.AttacksThisTurn (events.Apply's DeclareAttackers case,
// reset in TurnChange's per-object loop -- the same fact the Raid gate's
// Count$AttackersDeclared and the FirstAttack$ trigger gate read), and
// "already used this turn" is the activation-event scan the ActivationLimit$
// gate uses, with one extension.
//
// The extension is the granted-ability identity. A PRINTED AB$ mints an
// AbilityPush whose Amount is the ability's face index (events.Apply's
// AbilityPush case); a GRANTED AB$ (Besieged Viking Village's AddAbility$
// ABBoast) goes through beginGrantedActivation, which mints a DelayedPush
// whose Amount is -1 and whose Counter names the granting SVar instead
// (rules/speed.go). A scan that only looked at AbilityPush/Amount --
// activationLimitReached's shape -- could not see a granted Boast at all and
// would re-offer it every window. Matching either identity closes that: the
// printed form matches on index, the granted form on the SVar name.
func (e *Engine) boastGateOK(id state.ObjID, ability int, svar string) bool {
	o := e.G.Obj(id)
	if o == nil || o.AttacksThisTurn == 0 {
		return false
	}
	used := 0
	// CR 400.7: only this object's own activations count (see
	// activationUsedCount).
	stint := e.objectStintStart(id)
	for i := len(e.L.Events) - 1; i >= stint; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Obj != id {
			continue
		}
		switch ev.Kind {
		case events.AbilityPush:
			if svar == "" && ev.Amount == int32(ability) {
				used++
			}
		case events.DelayedPush, events.GrantAbilityPush:
			// A granted activation's identity is its SVar name. A self-grant
			// mints through DelayedPush (Counter = the name); a CROSS-object
			// grant -- a printed Continuous AddAbility$ static such as
			// Besieged Viking Village's "All creatures have 'Boast -- {1}: ...'"
			// -- mints through GrantAbilityPush, whose Counter is the same
			// name. Reading only DelayedPush would leave the granted Boast
			// re-offered in every priority window of the turn it was used.
			if svar != "" && ev.Counter == svar {
				used++
			}
		}
	}
	return used == 0
}

// resolveActivationLimitAt interprets an ActivationLimit$ value. A literal
// integer is used directly. A non-literal value is resolved through the
// Count$/SVar evaluator the rest of the tree uses (effects.EvalCount), bound
// to the source object and the SVar table of the face at pile position merged
// (0 = the top face), so a computed limit is enforced rather than silently
// ignored. ok reports whether the limit was resolvable at all: false keeps the
// pre-fix behaviour of leaving the limit unenforced, which is also what a
// value that is neither a literal nor an SVar reference (such as a
// description-suffixed literal from a keyword template) gets.
func (e *Engine) resolveActivationLimitAt(id state.ObjID, p state.PlayerID, raw string, merged int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return n, true
	}
	o := e.G.Obj(id)
	if o == nil || o.PileFaceFor(merged) == nil {
		return 0, false
	}
	svars := e.pileSVars(id, merged)
	ctx := effects.NewCtxPtr(id, p, effects.CtxInit{SVars: svars})
	if strings.HasPrefix(raw, "Count$") {
		return int(effects.EvalCount(e, ctx, raw)), true
	}
	if body, ok := svars[raw]; ok {
		return int(effects.EvalCount(e, ctx, body)), true
	}
	return 0, false
}
