package combat

import (
	"github.com/adams-shaun/gorge/cards"
	"math"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// CanAttack reports whether id may be declared as an attacker against SOME
// defender (CR 508.1a): a creature under the active player's control, untapped,
// either not summoning sick or hasty, and not walled by Defender (CR 702.3b)
// -- unless a CanAttackDefender static lifts the wall against some defender
// (defender.go). A reconfigure card while attached is not a
// creature (CR 702.150c): the derived type switch (reconfigureTypeSwitch)
// already dropped Creature, so IsCreature answers false here with no extra
// gate. The pair-precise read is CanAttackPair; this defender-blind form is
// only for callers that genuinely have no defender in hand
// (mustAttackRequired's creature-shaped gates, whose per-pair half is
// attackPairAvailable, and validateAttackers' belt check, whose precise half
// is the offered-pair membership test).
func CanAttack(b Board, id state.ObjID) bool {
	o, ok := AttackableCreature(b, id)
	if !ok {
		return false
	}
	if !b.HasKW(id, KWDefender) {
		return true
	}
	for _, d := range b.Game().AliveFrom(0) {
		if d != o.Controller && AttackAllowedThroughDefender(b, id, d) {
			return true
		}
	}
	return false
}

// CanAttackPair is the (attacker, defender) pair reading of CanAttack: the
// same checks with the Defender wall lifted exactly when a CanAttackDefender
// static applies to THIS pair (CR 702.3b) -- the pair-precise half the offer
// list (attackOffers), the validator and the encore requirement read. A
// ValidAttacked$-scoped static lifts the wall only against the defenders the
// spec admits, so a Defender creature may be attackable against one defender
// and walled against the rest.
func CanAttackPair(b Board, id state.ObjID, defender state.PlayerID) bool {
	if _, ok := AttackableCreature(b, id); !ok {
		return false
	}
	if b.HasKW(id, KWDefender) && !AttackAllowedThroughDefender(b, id, defender) {
		return false
	}
	return true
}

// AttackableCreature is the defender-blind half both reads share: the object
// exists, is a battlefield creature of the active player (the DERIVED type,
// layer 4 -- an animated land attacks, while its printed face is a Land, and
// everything the printed face admits the derived walk admits too, so ordinary
// creatures are unchanged; a bestowed card stays excluded, BestowedAttached),
// untapped, and either not summoning sick or hasty.
func AttackableCreature(b Board, id state.ObjID) (*state.Object, bool) {
	g := b.Game()
	o := g.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != g.Active {
		return nil, false
	}
	if o.PhasedOut {
		// CR 702.25b/d: a phased-out permanent is treated as though it does
		// not exist, so it cannot attack. The one gate both CanAttack and
		// CanAttackPair share.
		return nil, false
	}
	f := o.Face()
	if f == nil || !b.IsCreature(id) || o.BestowedAttached() {
		return nil, false
	}
	if o.Tapped {
		return nil, false
	}
	if o.SummonSick && !b.HasKW(id, KWHaste) {
		return nil, false
	}
	return o, true
}

// encoreAttackDefender reports the opponent an encore token must attack this
// turn if able. The requirement expires by turn number and becomes impossible
// (therefore nonbinding) if that opponent has left the game.
func encoreAttackDefender(b Board, id state.ObjID) (state.PlayerID, bool) {
	g := b.Game()
	o := g.Obj(id)
	if o == nil || o.EncoreAttackTurn == 0 || o.EncoreAttackTurn != g.Turn ||
		int(o.EncoreAttackDefender) >= len(g.Players) || g.Players[o.EncoreAttackDefender].Lost ||
		!CanAttackPair(b, id, o.EncoreAttackDefender) {
		return 0, false
	}
	return o.EncoreAttackDefender, true
}

// RequirementSet is every requirement binding ONE creature's attack this
// combat (CR 508.1d's "attacks if able" duties). A requirement is either
// NAMED (it names a specific defending player) or BROAD (any defender
// satisfies it), and a goad is a requirement to attack a non-goader when one
// is available.
//
// The set is the ONE home for "what must this creature attack": rules'
// attackOffers derives its pair list from it (dropping every pair that
// satisfies fewer named requirements than the best available defender -- CR
// 508.1d's "satisfy as many requirements as possible"), and
// mustAttackRequired reads its emptiness as "is this creature required at
// all". A requirement whose player reference this build cannot resolve
// contributes nothing (fail closed, the safe direction for a requirement),
// exactly the convention MustAttackParamsReadable documents.
type RequirementSet struct {
	// Named counts, per defending player, how many named requirements that
	// defender satisfies. A pair attacking the defender satisfies every one of
	// them. nil when no named requirement applies.
	Named map[state.PlayerID]int
	// Broad is set by an unconditional MustAttack static (no MustAttack$
	// player reference): every defender satisfies it.
	Broad bool
	// Goad is set by a live goad (CR 701.38b): the creature must attack a
	// player, preferably a non-goader. GoadMayAttack handles the player
	// preference; SatisfiedByOffer excludes battles from satisfying this duty.
	Goad bool
}

// Any reports whether at least one requirement binds the creature.
func (s RequirementSet) Any() bool {
	return len(s.Named) > 0 || s.Broad || s.Goad
}

// addNamed records one named requirement for defender.
func (s *RequirementSet) addNamed(defender state.PlayerID) {
	if s.Named == nil {
		s.Named = make(map[state.PlayerID]int, 2)
	}
	s.Named[defender]++
}

// SatisfiedBy reports how many NAMED requirements the given player defender
// satisfies. The broad requirement contributes uniformly across all pairs;
// goad is scored separately by SatisfiedByOffer for player attacks only.
func (s RequirementSet) SatisfiedBy(defender state.PlayerID) int {
	return s.Named[defender]
}

// SatisfiedByOffer distinguishes attacking a player from attacking a battle
// that player protects: def is the offer's defending player and battle the
// attacked permanent (0 for a player attack). Named-player and goad duties
// are discharged only by attacking a player; the protector field on a battle
// offer is not itself the defender of the attack. Goad's non-goader
// preference is enforced by GoadMayAttack when enumerating the legal player
// pairs.
func (s RequirementSet) SatisfiedByOffer(def state.PlayerID, battle state.ObjID) int {
	if battle != 0 {
		return 0
	}
	n := s.SatisfiedBy(def)
	if s.Goad {
		n++
	}
	return n
}

// MaxNamed is the greatest number of named requirements any single defender
// satisfies at once -- the best any offered pair can do against the named
// half of the requirement set.
func (s RequirementSet) MaxNamed() int {
	best := 0
	for _, n := range s.Named {
		if n > best {
			best = n
		}
	}
	return best
}

// AttackRequirements collects every requirement binding creature id this
// combat: the encore designation, each applicable Effect-registered and face
// Mode$ MustAttack static, and a live goad. Multiple named requirements are
// kept SEPARATELY (a map count per defender) rather than collapsed to the
// first, so two simultaneous "attacks that player" duties are both honoured
// and neither silently wins.
func AttackRequirements(b Board, id state.ObjID) RequirementSet {
	var s RequirementSet
	o := b.Game().Obj(id)
	if o == nil {
		return s
	}
	if p, ok := encoreAttackDefender(b, id); ok {
		s.addNamed(p)
	}
	for ceI, ceL := 0, b.Active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "MustAttack" {
			continue
		}
		if !mustAttackLineSelects(b, ce.RestrictParams["ValidCreature"], id, ce.Source, ce.Controller, ce.Remembered) {
			continue
		}
		spec := strings.TrimSpace(ce.RestrictParams["MustAttack"])
		if spec == "" {
			s.Broad = true
			continue
		}
		if p, ok := requirementDefender(b, spec, ce.Source, ce.Controller, ce.RememberedPlayers); ok {
			s.addNamed(p)
		}
	}
	for _, sv := range b.Statics("MustAttack") {
		if !effects.MustAttackParamsReadableForRules(sv.Params) || !b.StaticGateHolds(sv) {
			continue
		}
		if !mustAttackLineSelects(b, sv.ParamStr(cards.PKValidCreature), id, sv.Source, sv.Controller, nil) {
			continue
		}
		spec := strings.TrimSpace(sv.Params["MustAttack"])
		if spec == "" {
			s.Broad = true
			continue
		}
		if p, ok := requirementDefender(b, spec, sv.Source, sv.Controller, nil); ok {
			s.addNamed(p)
		}
	}
	if HasActiveGoad(b, o) {
		s.Goad = true
	}
	return s
}

// mustAttackLineSelects resolves a MustAttack line's ValidCreature$ against
// the candidate creature, with the registration's remembered set bound for
// the Card.IsRemembered family (Knight Rampager, Ursine Monstrosity, Raving
// Dead, Ruhan of the Fomori all scope the requirement to a remembered self).
// An absent ValidCreature$ is Forge's Card.Self default, the same default
// AttackRequirements applies.
func mustAttackLineSelects(b Board, spec string, id state.ObjID, source state.ObjID, controller state.PlayerID, remembered []state.ObjID) bool {
	v := strings.TrimSpace(spec)
	if v == "" {
		v = "Card.Self"
	}
	return b.MatchesSpec(v, id, source, controller, remembered, nil)
}

// requirementDefender resolves a MustAttack$ player reference to the
// defending player it names, from the requirement registration's own
// bindings. ChosenPlayer/Player.Chosen reads the source object's event-backed
// Chosen list (the ChoosePlayer answer, which survives from the begin-combat
// trigger to the declare-attackers step because choiceRecord emits it on the
// source). RememberedPlayer/Player.IsRemembered reads the registration's
// captured PLAYERS (state.ContinuousEffect.RememberedPlayers), and
// Remembered.NonActive additionally requires that player not be the active
// one. You binds to the controller captured by the registration. Remembered
// binds only when exactly one player was captured; ambiguous registrations
// fail closed. Other references need bindings or evaluators this build does
// not carry and therefore fail closed.
func requirementDefender(b Board, spec string, source state.ObjID, controller state.PlayerID, rememberedPlayers []state.PlayerID) (state.PlayerID, bool) {
	switch strings.TrimSpace(spec) {
	case "ChosenPlayer", "Player.Chosen":
		if o := b.Game().Obj(source); o != nil {
			for _, t := range o.Chosen {
				if t.IsPlayer {
					return t.Player, true
				}
			}
		}
	case "RememberedPlayer", "Player.IsRemembered":
		if len(rememberedPlayers) > 0 {
			return rememberedPlayers[0], true
		}
	case "You":
		return controller, true
	case "Remembered":
		if len(rememberedPlayers) == 1 {
			return rememberedPlayers[0], true
		}
	case "Remembered.NonActive":
		for _, p := range rememberedPlayers {
			if p != b.Game().Active {
				return p, true
			}
		}
	}
	return 0, false
}

// MaxAttackers reports the global attacker ceiling. Defender-scoped ceilings
// are enforced per defender by validateAttackDeclaration; they cannot be
// represented by the KAttackers decision's single Max value.
func MaxAttackers(b Board) int {
	const huge = int(^uint(0) >> 1)
	maxAllowed := huge
	for _, sv := range b.Statics("AttackRestrict") {
		if strings.TrimSpace(sv.ParamStr(cards.PKValidDefender)) != "" || !b.StaticGateHolds(sv) {
			continue
		}
		// attackCeiling defaults an absent/invalid MaxAttackers$ to the
		// maximum int32, so an unparseable restriction contributes no ceiling.
		n := attackCeiling(sv.Params["MaxAttackers"])
		if int(n) < maxAllowed {
			maxAllowed = int(n)
		}
	}
	return maxAllowed
}

// AttackRestrictLimit returns the tightest active, gated AttackRestrict
// ceiling that applies to attacks at defender, and whether any applies.
// Multiple restrictions can name one defender; the smallest ceiling binds
// (CR 508.1c), and validateAttackDeclaration, the option Group cap and the
// decision's per-Group limit all derive from this one read so the engine's
// declaration check and the wire's repair rule cannot disagree.
func AttackRestrictLimit(b Board, defender state.PlayerID) (int, bool) {
	limit := 0
	found := false
	for _, sv := range b.Statics("AttackRestrict") {
		if !b.StaticGateHolds(sv) {
			continue
		}
		spec := strings.TrimSpace(sv.ParamStr(cards.PKValidDefender))
		if spec == "" || !effects.MatchesPlayerSpecCtx(b.Game(), spec, defender, sv.Controller, b.PlayerSpecCtx(sv.Source)) {
			continue
		}
		n := int(attackCeiling(sv.Params["MaxAttackers"]))
		if !found || n < limit {
			limit, found = n, true
		}
	}
	return limit, found
}

// attackCeiling parses a MaxAttackers$ amount the way rules' parseAmount
// does with a math.MaxInt32 default: a non-negative int32 literal (an
// optional sign and ASCII digits, surrounding space trimmed), anything else
// -- absent, non-literal, negative or out of range -- the default. The digit
// pre-check keeps strconv from allocating a *NumError on the common absent
// read.
func attackCeiling(s string) int32 {
	s = strings.TrimSpace(s)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i == len(s) {
		return math.MaxInt32
	}
	for j := i; j < len(s); j++ {
		if s[j] < '0' || s[j] > '9' {
			return math.MaxInt32
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 || v > int64(math.MaxInt32) {
		return math.MaxInt32
	}
	return int32(v)
}
