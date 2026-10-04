package pay

import (
	"maps"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// board.go holds the payment layer's small board reads and cost-part
// executions (lasagna spec W5 E7): mana-source enumeration, a source's
// recorded colour, a printed ability's wire identity, and the energy and
// library-order cost parts. They read the game directly or emit through the
// Engine seam; none is an Engine method.

// CostTargetingSA returns the spell or activated ability whose ValidTgts$ and
// TargetMin$/TargetMax$ a target-relative cost read shares. A spell's face
// carries the declaration while it is in hand; an activated ability is the
// scope's own SA. This is the ONE derivation costPotentialTargets and
// costAmountTargets use, so the offer census and the amount's legal-assignment
// size can never name different declarations.
func CostTargetingSA(g *state.Game, id state.ObjID, scope CostScope) *cards.SA {
	if scope.Kind == "Static" {
		// A special action (specialActionScope) announces no targets.
		return nil
	}
	if scope.Kind == "Ability" {
		return scope.Ab
	}
	if o := g.Obj(id); o != nil && o.Face() != nil {
		return o.Face().SpellAbility()
	}
	return nil
}

// EnergyPayable reports whether the payer's ENERGY counter total covers the
// cost's fixed energy parts (Forge CostPayEnergy.canPay reads the same total).
// A dynamic PayEnergy<X> part is bounded by that total at its own X ask, so
// this gate makes no assumption about the not-yet-chosen value.
func EnergyPayable(g *state.Game, p state.PlayerID, c *Cost) bool {
	total := c.EnergyCostTotal()
	return total == 0 || g.Players[p].Counter("ENERGY") >= total
}

// InstantSpeedOnly reports whether a mana ability's InstantSpeed$ True
// timing restriction is present ("Activate only as an instant", Lion's Eye
// Diamond): the ability is activatable exactly when its controller holds
// priority. The engine activates mana abilities in exactly two contexts: a
// priority window (the "activate for mana" action) and a payment window
// (paying for a spell, a ward or a cumulative-upkeep cost). CR 605.4 lets a
// player activate mana abilities while paying a cost only as far as the
// ability's own rules permit, and the card's text bars everything but a
// priority moment -- so the ability is activatable at priority and never
// inside a payment window.
func InstantSpeedOnly(ma *cards.SA) bool {
	return strings.EqualFold(strings.TrimSpace(ma.ParamStr(cards.PKInstantSpeed)), "True")
}

// manaDiscards performs the pure offer-side feasibility walk for a mana
// ability's discard cost. It reserves deterministic candidates but consumes
// no RNG; the payment continuation makes the actual choice.
func ManaExiles(e Engine, p state.PlayerID, source state.ObjID, cost Cost) ([]state.ObjID, bool) {
	var exiles []state.ObjID
	reserved := map[state.ObjID]bool{}
	for _, part := range cost.Exile {
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		var candidates []state.ObjID
		for _, id := range e.Game().Zone(zone, p) {
			if !reserved[id] && e.MatchesSpecFrom(part.Spec, id, p, source) {
				candidates = append(candidates, id)
			}
		}
		if part.N <= 0 || int(part.N) > len(candidates) {
			return nil, false
		}
		for i := 0; i < int(part.N); i++ {
			reserved[candidates[i]] = true
			exiles = append(exiles, candidates[i])
		}
	}
	return exiles, true
}

// ChosenProducedColour reads the recorded as-enters chosen colour for
// source: a single WUBRG letter, else "" (nothing valid recorded). Shared
// by the activation-path Produced$ read sites.
func ChosenProducedColour(g *state.Game, source state.ObjID) string {
	if o := g.Obj(source); o != nil {
		if col := strings.TrimSpace(o.ChosenColor); len(col) == 1 && strings.ContainsRune("WUBRG", rune(col[0])) {
			return col
		}
	}
	return ""
}

// BattlefieldManaSourceIDs lists the payer's battlefield permanents first,
// followed by other players' battlefield permanents in seat order. The latter
// matter when an ability's Activator$ explicitly permits the payer; the shared
// availableManaAbilities gate filters each source. Zone order within each
// owner is retained for deterministic option ordering.
func BattlefieldManaSourceIDs(g *state.Game, p state.PlayerID) []state.ObjID {
	return AppendBattlefieldManaSourceIDs(g, nil, p)
}

// AppendBattlefieldManaSourceIDs is battlefieldManaSourceIDs appending to
// dst (a list borrowed with idsBorrow by a caller that only ranges it).
func AppendBattlefieldManaSourceIDs(g *state.Game, dst []state.ObjID, p state.PlayerID) []state.ObjID {
	ids := append(dst, g.Zone(state.ZBattlefield, p)...)
	for _, owner := range g.AliveFrom(0) {
		if owner != p {
			ids = append(ids, g.Zone(state.ZBattlefield, owner)...)
		}
	}
	return ids
}

// ManaSourceIDs adds the payer's non-battlefield printed mana sources to the
// public battlefield set. Hand and graveyard sources remain owner-scoped.
func ManaSourceIDs(g *state.Game, p state.PlayerID) []state.ObjID {
	ids := BattlefieldManaSourceIDs(g, p)
	for _, z := range []state.Zone{state.ZHand, state.ZGraveyard} {
		ids = append(ids, g.Zone(z, p)...)
	}
	return ids
}

// ChargeEnergyCost spends a cost's energy parts from the payer's pool, one
// PlayerCounterChange per part (a player counter, not an object's -- CR
// 118.2d). A fixed part spends its N; a dynamic part spends the announced x.
// This is the ONE energy-charging site, shared by the cast/activation payment
// path and the triggered-cost window, so a paid cost can never spend its
// energy in one place and skip it in another.
func ChargeEnergyCost(e Engine, p state.PlayerID, c Cost, x int32) {
	for _, part := range c.Energy {
		amt := part.N
		if part.Spec == "X" {
			amt = x
		}
		if amt > 0 {
			e.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p,
				Counter: "ENERGY", Amount: -amt})
		}
	}
}

// ExileCostCandidates is the zone scan both the offerability walk
// (nonManaCastable) and the payment chooser (exAsk) use for one Exile cost
// part. A bound referent (a granted ability's OriginalHost -- The Dominion
// Bracelet) names the GRANTOR permanent, which need not be controlled by the
// activating player: control of the creature carrying the granted ability
// can change hands while the grant stays, and its new controller may still
// activate it. Such a referent is therefore appended to the payer's own zone
// list whenever it sits in the required zone, regardless of controller; the
// ordinary filter path below stays payer-only. One helper means offer and
// payment can never disagree about which objects can pay.
func ExileCostCandidates(g *state.Game, zone state.Zone, p state.PlayerID, part cost.CostPart) []state.ObjID {
	cands := g.Zone(zone, p)
	if part.Referent == 0 {
		return cands
	}
	o := g.Obj(part.Referent)
	if o == nil || o.Zone != zone {
		return cands
	}
	for _, oid := range cands {
		if oid == part.Referent {
			return cands
		}
	}
	out := make([]state.ObjID, 0, len(cands)+1)
	out = append(out, cands...)
	return append(out, part.Referent)
}

// PutLibPicksOnTop emits the LibraryOrder that lifts the just-moved picks to
// the top of each owner's library, preserving pick order. MoveZone already
// appended them to the bottom; the order carries the complete new library and
// is Secret (a hidden zone must not leak). Grouped per owner in first-pick
// order -- deterministic because the picks slice is -- so a pick owned by
// another player lands in the right library (the zone owner Move already used).
func PutLibPicksOnTop(e Engine, picks []state.ObjID) {
	byOwner := map[state.PlayerID][]state.ObjID{}
	var owners []state.PlayerID
	for _, id := range picks {
		o := e.Game().Obj(id)
		if o == nil || o.Zone != state.ZLibrary {
			continue
		}
		if _, ok := byOwner[o.Owner]; !ok {
			owners = append(owners, o.Owner)
		}
		byOwner[o.Owner] = append(byOwner[o.Owner], id)
	}
	for _, owner := range owners {
		sel := byOwner[owner]
		selected := make(map[state.ObjID]bool, len(sel))
		for _, id := range sel {
			selected[id] = true
		}
		lib := e.Game().Zone(state.ZLibrary, owner)
		order := make([]state.ObjID, 0, len(lib))
		order = append(order, sel...)
		for _, id := range lib {
			if !selected[id] {
				order = append(order, id)
			}
		}
		e.Emit(events.Event{Kind: events.LibraryOrder, Player: owner, IDs: order, Secret: true})
	}
}

func PlanRiderHasTarget(g *state.Game, id state.ObjID, mana *cards.SA) bool {
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return true
	}
	rider := cards.ResolveSVar(o.Face().SVars, strings.TrimSpace(mana.ParamStr(cards.PKSubAbility)))
	if rider == nil {
		return false
	}
	for _, key := range slices.Sorted(maps.Keys(rider.Params)) {
		if strings.Contains(strings.ToLower(key), "target") || key == "ValidTgts" || key == "ValidTarget" {
			return true
		}
	}
	return false
}

// PlanController is the controller of source id, or seat 0 for a
// source that is no longer on the battlefield (the helper is only reached
// with a live battlefield source, but a nil read must never panic).
func PlanController(g *state.Game, id state.ObjID) state.PlayerID {
	if o := g.Obj(id); o != nil {
		return o.Controller
	}
	return 0
}

func PaymentAbility(g *state.Game, id state.ObjID, ma *cards.SA) (decision.PaymentAbility, bool) {
	o := g.Obj(id)
	if o == nil || o.Face() == nil || ma == nil {
		return decision.PaymentAbility{}, false
	}
	if strings.HasPrefix(ma.Line, "intrinsic:") {
		return decision.PaymentAbility{Kind: decision.PaymentAbilityIntrinsic, Intrinsic: "basic_land"}, true
	}
	for i, a := range o.Face().Abilities {
		if a == ma {
			return decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted, Face: uint32(o.FaceIdx), Index: uint32(i)}, true
		}
	}
	return decision.PaymentAbility{}, false // grants, merged and foreign abilities are V1 exclusions.
}
