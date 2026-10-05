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
		if source.ID == 0 {
			continue
		}
		for _, hit := range source.DamageDealtThisTurn {
			if !damageSourceMatches(g, sourceSpec, source, hit, c) ||
				code == evalCountBodyCostNonCombatDamageThisTurn && hit.Combat {
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

// Match each hit using its source's recorded characteristics when damage landed.
// Older records without a zone/type snapshot retain the live filter semantics.
func damageSourceMatches(g *state.Game, spec string, source *state.Object, hit state.DamageDealtRecord, c *Ctx) bool {
	snapshot := *source
	snapshot.Controller = hit.SourceControl
	if hit.HasSourceZone {
		snapshot.Zone = hit.SourceZone
	}
	sc := c.SpecContext(c.Controller)
	if hit.SourceTypes != nil {
		// The hit's complete effective type list overrides any current layer-4
		// entry, including one that appeared after the damage was dealt.
		types := make([]ObjectTypes, 0, len(sc.Layers.DerivedTypes)+1)
		for _, dt := range sc.Layers.DerivedTypes {
			if dt.ID != source.ID {
				types = append(types, dt)
			}
		}
		sc.Layers.DerivedTypes = append(types, ObjectTypes{ID: source.ID, Types: hit.SourceTypes})
	}
	if hit.HasSourceColors {
		// The source's colour is read as it was when the damage was dealt
		// (a red Ojer that later returns as its colourless Temple face still
		// counts its earlier hits as red).
		colors := make([]ObjectColors, 0, len(sc.Layers.DerivedColors)+1)
		for _, dc := range sc.Layers.DerivedColors {
			if dc.ID != source.ID {
				colors = append(colors, dc)
			}
		}
		sc.Layers.DerivedColors = append(colors, ObjectColors{ID: source.ID, Mask: ColorMaskFromLetters(hit.SourceColors)})
	}
	if snapshot.Zone == state.ZBattlefield {
		return matchesObjectPtr(g, spec, &snapshot, &sc)
	}
	if source.IsCopy && snapshot.Zone != state.ZStack {
		return false
	}
	return compiledMatchZone(compiledSpecFor(spec), g, &snapshot, &sc, snapshot.Zone)
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
