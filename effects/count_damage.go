package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

func evalDamageHistory(h Host, c *Ctx, g *state.Game, head, arg string) (int32, bool, bool) {
	code := evalCountBodyCostCodes.Code(head)
	if code != evalCountBodyCostMaxCombatDamageThisTurn && code != evalCountBodyCostNumDamageThisTurn && code != evalCountBodyCostNonCombatDamageThisTurn {
		return 0, false, false
	}
	if code == evalCountBodyCostMaxCombatDamageThisTurn {
		return maxPlayerCombatDamage(g), true, true
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return 0, false, true
	}
	sourceSpec, recipientSpec := fields[0], "Any"
	if len(fields) > 1 {
		recipientSpec = fields[1]
	}
	var total int32
	for i := range g.Objs {
		source := &g.Objs[i]
		if source.ID == 0 || !matchesZoneSpecCtx(g, sourceSpec, source.ID, c.SpecContext(c.Controller), source.Zone) {
			continue
		}
		for _, hit := range source.DamageDealtThisTurn {
			if code == evalCountBodyCostNonCombatDamageThisTurn && hit.Combat {
				continue
			}
			if damageRecipientMatches(g, recipientSpec, hit, c) {
				if code == evalCountBodyCostNumDamageThisTurn {
					total++ // A source qualifies once, regardless of hits or amount.
					break
				}
				total += hit.Amount
			}
		}
	}
	return total, true, true
}

// MaxCombatDamageThisTurn asks how much combat damage any ONE player took,
// across all sources. Damage to permanents is not damage to their controller.
func maxPlayerCombatDamage(g *state.Game) int32 {
	perPlayer := make([]int32, len(g.Players))
	for i := range g.Objs {
		for _, hit := range g.Objs[i].DamageDealtThisTurn {
			if p, ok := hit.Recipient.PlayerRef(); hit.Combat && ok && p >= 0 && int(p) < len(perPlayer) {
				perPlayer[p] += hit.Amount
			}
		}
	}
	var max int32
	for _, n := range perPlayer {
		if n > max {
			max = n
		}
	}
	return max
}

func damageRecipientMatches(g *state.Game, spec string, hit state.DamageDealtRecord, c *Ctx) bool {
	if player, ok := hit.Recipient.PlayerRef(); ok {
		return MatchesPlayerSpec(g, spec, player, c.Controller)
	}
	o := g.Obj(hit.Recipient)
	if o == nil {
		return false
	}
	// Damage filters describe the recipient when damage was dealt. In
	// particular, a permanent remains a permanent for this count after lethal
	// damage moves it to another zone.
	snapshot := *o
	snapshot.Zone = hit.RecipientZone
	snapshot.Controller = hit.RecipientControl
	sc := c.SpecContext(c.Controller)
	if hit.RecipientTypes != nil {
		types := append([]ObjectTypes(nil), sc.Layers.DerivedTypes...)
		found := false
		for i := range types {
			if types[i].ID == snapshot.ID {
				types[i].Types = hit.RecipientTypes
				found = true
				break
			}
		}
		if !found {
			types = append(types, ObjectTypes{ID: snapshot.ID, Types: hit.RecipientTypes})
		}
		sc.Layers.DerivedTypes = types
	}
	return matchesObjectPtr(g, spec, &snapshot, &sc)
}
