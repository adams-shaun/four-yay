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
		source := c.TriggerCard
		if source == 0 {
			source = c.Source
		}
		return maxCombatDamage(g.Obj(source)), true, true
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
				total += hit.Amount
			}
		}
	}
	return total, true, true
}

func maxCombatDamage(source *state.Object) int32 {
	if source == nil {
		return 0
	}
	var max int32
	for i, hit := range source.DamageDealtThisTurn {
		if !hit.Combat {
			continue
		}
		var total int32
		for _, other := range source.DamageDealtThisTurn[i:] {
			if other.Combat && other.Recipient == hit.Recipient {
				total += other.Amount
			}
		}
		if total > max {
			max = total
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
	return matchesObjectPtr(g, spec, &snapshot, &sc)
}
