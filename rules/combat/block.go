package combat

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// CanBlock reports whether blocker may be declared against attacker (CR
// 509.1a): an untapped creature controlled by the defending player, gated by
// Flying/Reach (CR 702.9b), Horsemanship (CR 702.31b), Fear, Shadow and by
// any CantBlock/CantBlockBy static (BlockRestricted, restrictions.go).
func CanBlock(b Board, blocker, attacker state.ObjID) bool {
	g := b.Game()
	bo, a := g.Obj(blocker), g.Obj(attacker)
	if bo == nil || a == nil || !a.IsAttacking {
		return false
	}
	if bo.Zone != state.ZBattlefield || a.Zone != state.ZBattlefield {
		return false
	}
	if bo.PhasedOut || a.PhasedOut {
		// CR 702.25b/d: a phased-out permanent is treated as though it does
		// not exist, so it can neither block nor be blocked.
		return false
	}
	bf := bo.Face()
	// Derived, not printed -- see CanAttack (an animated manland blocks).
	if bf == nil || !b.IsCreature(blocker) || bo.BestowedAttached() {
		return false
	}
	if bo.Tapped || bo.Controller != a.Attacking {
		return false
	}
	// CR 702.157b: a suspected creature can't block. The designation is the
	// declaration-legality rule itself, checked here where every other
	// can't-block gate lives (Flying, Shadow, BlockRestricted), so the ask's
	// options and the validator's recompute share one oracle.
	if bo.Suspected {
		return false
	}
	// CR 702.86 (kw:Unleash): a creature with unleash can't block while it
	// has a +1/+1 counter on it. The keyword rides the derived list (printed
	// plus layer-6 granted -- Tesak's "Other Dogs you control have unleash"),
	// and the counter is live state, so both halves are read here, the same
	// status-gate shape the Suspected check above practises.
	if b.HasKW(blocker, KWUnleash) && bo.Counter("P1P1") > 0 {
		return false
	}
	// CR 509.1a / 702.16j: a creature that the attacker is protected from
	// cannot block it.
	if b.ProtectedFrom(attacker, blocker) {
		return false
	}
	// CR 702.27/702.28: Shadow creatures can block only Shadow creatures,
	// and a Shadow creature is blockable only by one. Fear permits only an
	// artifact or black creature to block it.
	if b.HasKW(attacker, KWShadow) != b.HasKW(blocker, KWShadow) {
		return false
	}
	if b.HasKW(attacker, KWFear) && !bf.IsArtifact() && !strings.ContainsRune(b.ObjectColors(bo), 'B') {
		return false
	}
	// CR 702.13a: an Intimidate attacker can be blocked only by artifact
	// creatures and/or creatures that share a colour with it -- the Fear
	// predicate generalised from one fixed colour (black) to a colour
	// INTERSECTION. Attacker-keyed and per-pair like Fear/Shadow/Skulk, with
	// derived (layer-5) colours on both sides; a colourless Intimidate
	// attacker has no colour to share, so only an artifact creature blocks
	// it.
	if b.HasKW(attacker, KWIntimidate) {
		attColors, blockColors := b.ObjectColors(a), b.ObjectColors(bo)
		shared := false
		for i := 0; i < len(attColors); i++ {
			if strings.ContainsRune(blockColors, rune(attColors[i])) {
				shared = true
				break
			}
		}
		if !bf.IsArtifact() && !shared {
			return false
		}
	}
	// CR 702.31b: a creature with horsemanship can be blocked only by a
	// creature with horsemanship. The rule is asymmetric and attacker-keyed
	// -- unlike Shadow, a horsemanship creature MAY block a creature without
	// horsemanship -- so only the attacker side is gated here.
	if b.HasKW(attacker, KWHorsemanship) && !b.HasKW(blocker, KWHorsemanship) {
		return false
	}
	if b.HasKW(attacker, KWFlying) && !b.HasKW(blocker, KWFlying) && !b.HasKW(blocker, KWReach) {
		return false
	}
	// CR 702.14: a creature with landwalk can't be blocked as long as the
	// defending player controls a land of the specified type. Unlike Shadow
	// or Horsemanship, this is not a blocker-keyword comparison at all: the
	// gate reads the DEFENDER's controlled lands, via the same real
	// characteristic filter (land subtypes, supertypes, nonBasic) the rest of
	// the engine uses. Attacker-keyed and per-pair; checked here where every
	// other can't-block rule lives so the ask's options and the validator's
	// recompute share one oracle.
	if LandwalkEvades(b, attacker) {
		return false
	}
	// CR 702.110a: a creature with skulk can't be blocked by creatures with
	// greater power. Attacker-keyed and per-pair like Fear/Shadow; DERIVED
	// power, never printed PT (a +1/+1'd or pumped blocker's real power is
	// what the CR means). CR 509.1h: this is a declaration-legality rule,
	// checked here at CR 509.1a -- a blocker's power growing past the
	// attacker's after declaration does not unblock it, and no re-check runs.
	if b.HasKW(attacker, KWSkulk) {
		blockerPower := b.Power(blocker)
		if blockerPower > b.Power(attacker) {
			return false
		}
	}
	if BlockRestricted(b, blocker, attacker) {
		return false
	}
	return true
}

// LandwalkSpec returns the land filter a single keyword line names when that
// line is a `Landwalk` keyword, and false for any other keyword. Forge spells
// the parameter as the filter over the defending player's lands that makes the
// creature unblockable (`Landwalk:Island`, `Landwalk:Swamp.Snow`,
// `Landwalk:Land.Legendary`, `Landwalk:Land.nonBasic`, `Landwalk:Desert`). A
// second colon introduces the human-readable description Forge carries
// (`Landwalk:Land.Snow:snow Land`) and is NOT part of the spec; a line with no
// parameter yields the empty spec, which the caller treats as no evasion
// rather than as a universal one.
func LandwalkSpec(k string) (string, bool) {
	if !strings.EqualFold(cards.KeywordHead(k), "Landwalk") {
		return "", false
	}
	i := strings.IndexByte(k, ':')
	if i < 0 {
		return "", true
	}
	spec := k[i+1:]
	if j := strings.IndexByte(spec, ':'); j >= 0 {
		spec = spec[:j]
	}
	return strings.TrimSpace(spec), true
}

// LandwalkEvades reports whether attacker carries a Landwalk keyword whose
// land filter matches at least one land the defending player controls
// (CR 702.14). The defending player is the seat the attacker was declared
// against (state.Object.Attacking, which CanBlock already pairs the blocker
// to).
//
// The filter is evaluated with the engine's ordinary land characteristics --
// layer-4 derived types and supertypes, so a type-changing effect (Yavimaya,
// Cradle of Growth making every land a Forest) is honoured, not the printed
// name alone. Matching goes through effects.MatchesSpecCtx: the parameter is a
// complete Forge filter spec, whose base carries the land subtype (`Island`,
// `Swamp.Snow`) or `Land` with a supertype/qualifier predicate
// (`Land.Legendary`, `Land.nonBasic`, `Land.Snow`). Board.LandSpecCtx binds
// the layer-3 rename set and the layer-4 derived-type table (refreshed for
// this board first), so a granted land type matches too. An empty or
// unrecognised parameter fails CLOSED -- the spec matches no land, so the
// creature stays ordinarily blockable rather than becoming universally
// unblockable. Walk order is the defender's deterministic battlefield zone
// slice, never a map.
func LandwalkEvades(b Board, attacker state.ObjID) bool {
	g := b.Game()
	a := g.Obj(attacker)
	if a == nil {
		return false
	}
	defender := a.Attacking
	sc := b.LandSpecCtx(defender, attacker)
	for _, k := range b.Keywords(attacker) {
		spec, isLandwalk := LandwalkSpec(k)
		if !isLandwalk || spec == "" {
			continue
		}
		for _, land := range g.Zone(state.ZBattlefield, defender) {
			if effects.MatchesSpecCtx(g, spec, land, sc) {
				return true
			}
		}
	}
	return false
}

// MustBlockCandidates returns every creature with an active blocking duty.
// The decision's whole-team solver determines which of these duties are
// actually satisfiable together over the offered pairs (CR 509.1c).
func MustBlockCandidates(b Board, defender state.PlayerID) map[state.ObjID]bool {
	required := make(map[state.ObjID]bool)
	matches := func(spec string, source state.ObjID, controller state.PlayerID, id state.ObjID) bool {
		return spec == "" || b.MatchesSpec(spec, id, source, controller, nil, nil)
	}
	for _, id := range b.Game().Zone(state.ZBattlefield, defender) {
		if !b.IsCreature(id) {
			continue
		}
		for _, sv := range b.Statics("MustBlock") {
			if matches(sv.ParamStr(cards.PKValidCreature), sv.Source, sv.Controller, id) {
				required[id] = true
				break
			}
		}
		if required[id] {
			continue
		}
		for ceI, ceL := 0, b.Active(); ceI < len(ceL); ceI++ {
			ce := &ceL[ceI]
			if ce.Restriction == "MustBlock" && b.RestrictionApplies(ce, id) {
				required[id] = true
				break
			}
		}
	}
	return required
}

// MustBlockPairRequired checks a blocker's duty against this PARTICULAR
// attacker. An api:MustBlock DefinedAttacker$ duty must not be discharged by
// blocking a different creature; printed/statics without that binding allow
// any attacker. The option flags feed both the ask and the team validator.
func MustBlockPairRequired(b Board, blocker, attacker state.ObjID) bool {
	for _, sv := range b.Statics("MustBlock") {
		spec := sv.ParamStr(cards.PKValidCreature)
		if (len(spec) == 0 || b.MatchesStaticSpec(spec, blocker, sv)) && b.StaticGateHolds(sv) {
			return true
		}
	}
	for ceI, ces := 0, b.Active(); ceI < len(ces); ceI++ {
		ce := &ces[ceI]
		if ce.Restriction == effects.ModeMustBlock && (ce.MustBlockAttacker == 0 || ce.MustBlockAttacker == attacker) && b.RestrictionApplies(ce, blocker) {
			return true
		}
	}
	return false
}

// MustBlockAllPair reports whether the SAME active BlockAllDefined$ duty
// names this blocker and attacker. An unrelated ordinary MustBlock duty cannot
// borrow the multiple-block permission from another effect on the blocker.
func MustBlockAllPair(b Board, blocker, attacker state.ObjID) bool {
	for ceI, ces := 0, b.Active(); ceI < len(ces); ceI++ {
		ce := &ces[ceI]
		if ce.Restriction == effects.ModeMustBlock && ce.MustBlockAllAttackers &&
			ce.MustBlockAttacker == attacker && b.RestrictionApplies(ce, blocker) {
			return true
		}
	}
	return false
}

// ValidateMinMaxBlockers enforces CR 509.1a's MinMaxBlocker bounds on ONE
// attacker's declared blocker count n (already non-zero). A Min$ bound admits
// only 0 or at least min blockers; a Max$ bound admits only at most max; Min$
// All admits only a declaration every one of the defending player's legal
// blockers takes part in. The count of legal blockers for the All case is
// recomputed with CanBlock, the same oracle askBlockers' options use, so the
// solver and the option list can never disagree about which creatures could
// have blocked.
func ValidateMinMaxBlockers(b Board, attacker state.ObjID, n int, defender state.PlayerID) error {
	min, max, minOK, maxOK, all := MinMaxBlockerBounds(b, attacker)
	if !minOK && !maxOK && !all {
		return nil
	}
	if all {
		if n == 0 {
			// An unblocked declaration is always legal: the restriction
			// constrains WHO may block, never forces a block.
			return nil
		}
		// Min$ All (Tromokratis: "can't be blocked unless all creatures
		// defending player controls block it" -- the oracle's own
		// parenthetical: if ANY creature that player controls doesn't
		// block it, it can't be blocked). The declaration must therefore
		// match the defender's WHOLE creature count, not merely the legal
		// subset: a creature that cannot block does not block, so one such
		// creature makes every blocking declaration illegal.
		if n != DefenderCreatureCount(b, defender) {
			return fmt.Errorf("attacker %d can't be blocked unless all %d of the defender's creatures block it (declared %d)", attacker, DefenderCreatureCount(b, defender), n)
		}
		return nil
	}
	if minOK && n < min {
		return fmt.Errorf("attacker %d can't be blocked by fewer than %d creatures (declared %d)", attacker, min, n)
	}
	if maxOK && n > max {
		return fmt.Errorf("attacker %d can't be blocked by more than %d creatures (declared %d)", attacker, max, n)
	}
	return nil
}

// LegalBlockerCount counts the defending player's creatures that could block
// attacker (CanBlock's own oracle). askBlockers uses it to drop an attacker
// whose Min$ bound cannot possibly be met -- a declaration nobody could make
// legally is a decision worth not posing -- and it is the reachable half of a
// Min$ All bound (see DefenderCreatureCount).
func LegalBlockerCount(b Board, attacker state.ObjID, defender state.PlayerID) int {
	n := 0
	for _, bid := range b.Game().Zone(state.ZBattlefield, defender) {
		if CanBlock(b, bid, attacker) {
			n++
		}
	}
	return n
}

// DefenderCreatureCount counts every creature permanent the defending player
// controls -- the denominator of a Min$ All bound, which the oracle defines
// as "all creatures defending player controls" rather than the legal subset:
// a creature that cannot block still does not block, so its presence makes a
// Min$ All blocking declaration impossible (only the unblocked declaration is
// legal). Creature-ness is the same derived test CanBlock uses.
func DefenderCreatureCount(b Board, defender state.PlayerID) int {
	g := b.Game()
	n := 0
	for _, id := range g.Zone(state.ZBattlefield, defender) {
		if o := g.Obj(id); o != nil && o.EffectiveIsCreature() && !o.BestowedAttached() && !o.ReconfiguredAttached() {
			n++
		}
	}
	return n
}
