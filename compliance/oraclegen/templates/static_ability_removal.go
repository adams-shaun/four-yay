// Observation for a RemoveAllAbilities$ static that lands on the permanent
// its source is attached to (ticket cli-20261009T031407Z-dd0d6fbb). The
// default continuous-static scenario enchants the vanilla Grizzly Bears,
// whose abilities the removal cannot change, so the row was skipped as
// "removes the abilities of a permanent the fixture gives none". The Aura is
// cast instead onto a host that prints an ability (an evergreen keyword), so
// the removal is observable as the host's keyword, type, colour or P/T line
// moving. Every helper here only adds a candidate after the existing paths
// failed, so a row served before keeps its scenario bytes.
package templates

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// staticAbilityRemovalHosts are the probe hosts an ability-removing Aura is
// retried on, in preference order. Each prints an evergreen keyword the
// removal takes away, so the host's keyword line moves even though the
// fixture's printed creature (Grizzly Bears) has no abilities to lose. A
// keyworded artifact creature comes first so "enchant creature or Vehicle"
// accepts it; the rest cover a narrower "enchant creature" filter.
var staticAbilityRemovalHosts = []string{"Ornithopter", "Giant Spider", "Shivan Dragon", "Serra Angel"}

// staticAbilityRemovalItem serves a RemoveAllAbilities$ static whose affected
// permanent is the one the source is attached to (Flood the Engine, Frozen in
// Ice, Honest Work, Infinite Coursework, Enchanted River's Grasp). The Aura is
// cast onto a host with a printed ability; the removal shows as the host's
// keyword (or its type/colour/P/T, for Honest Work's Humble Merchant
// rewrite). ok is false when no host makes the removal observable.
func staticAbilityRemovalItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !st.HasParam(cards.PKRemoveAllAbilities) || !staticRemovalOnAttachment(st.ParamStr(cards.PKAffected)) {
		return oraclegen.Item{}, false
	}
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Item{}, false
	}
	for _, host := range staticAbilityRemovalHosts {
		hc, ok := reg.Lookup(host)
		if !ok || len(hc.Faces) == 0 {
			continue
		}
		spec := staticProbeSpecs(reg, []string{host})[host]
		for _, seat := range []int{0, 1} {
			base, sk := castResolveWith(reg, f, name, mana, nil, func(fx *oraclegen.Fixture) {
				staticPlaceHost(fx, seat, host)
			})
			if sk != nil {
				continue
			}
			sc := base.Scenario
			sc.Steps = append([]oraclegen.Step(nil), base.Steps...)
			ref := fmt.Sprintf("p%d:%s", seat, host)
			targeted := false
			for i := range sc.Steps {
				if sc.Steps[i].Op == "cast" && sc.Steps[i].Card == "p0:"+name {
					sc.Steps[i].Targets = []string{ref}
					targeted = true
				}
			}
			if !targeted {
				continue
			}
			res, err := rules.RunOracleScenarioJSON(reg, oraclegen.Item{Scenario: sc}.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				continue
			}
			if !staticAbilityRemovalObserved(res.Snapshots[len(res.Snapshots)-1], ref, spec) {
				continue
			}
			it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
			it.Compare = []string{oraclediff.CompareKeywords}
			return it, true
		}
	}
	return oraclegen.Item{}, false
}

// staticPlaceHost puts the host on the seat's battlefield in setup.
func staticPlaceHost(fx *oraclegen.Fixture, seat int, host string) {
	if seat == 0 {
		fx.P0().Battlefield = appendFixtureUnique(fx.P0().Battlefield, host)
		return
	}
	fx.P1().Battlefield = appendFixtureUnique(fx.P1().Battlefield, host)
}

// staticRemovalOnAttachment reports whether the removal is defined by the
// permanent the source is attached to (an Aura's EnchantedBy, an Equipment's
// EquippedBy/AttachedBy), which the default probe cannot show. The
// AffectedDefined$ form is normalized to this filter by cards.
func staticRemovalOnAttachment(affected string) bool {
	for _, w := range affectedWords(affected) {
		switch w {
		case "EnchantedBy", "AttachedBy", "EquippedBy":
			return true
		}
	}
	return false
}

// staticAbilityRemovalObserved reports whether the host permanent shows the
// removal: its P/T, keyword line or types/colours differ from the printed
// spec. The host is matched by ref, so a SetName$ host (Honest Work's Humble
// Merchant) is still recognised even though its name no longer keys the
// printed spec.
func staticAbilityRemovalObserved(s rules.OracleSnapshot, ref string, printed staticProbeSpec) bool {
	for _, p := range s.Permanents {
		if p.Ref != ref {
			continue
		}
		if p.PT != printed.pt {
			return true
		}
		if oraclediff.ComparedKeywords(p.Keywords, true) != printed.namedKeywords {
			return true
		}
		return staticCharsMoved(p, printed.chars)
	}
	return false
}
