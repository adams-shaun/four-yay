package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// zoneItem generates the level-B item for one static requirement.
func zoneItem(t *testing.T, card, key string) (oraclegen.Item, *oraclegen.Skip) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s absent from corpus", card)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != key {
			continue
		}
		if req.Sub != "static.continuous" {
			t.Fatalf("precondition: %s %s = %s, want static.continuous", card, key, req.Sub)
		}
		return templates.GenerateB(reg, card, req)
	}
	t.Fatalf("precondition: %s has no requirement %s", card, key)
	return oraclegen.Item{}, nil
}

// withoutSource is the item with its source's cast (and the resolve after it)
// dropped, and the source removed from p0's zones: the same checkpoint as if
// the static were never in play. dropCast names the card whose cast to drop.
func withoutSource(it oraclegen.Item, dropCast string, removeSource bool) oraclegen.Item {
	out := it
	out.Setup = map[string]oraclegen.Seat{}
	for k, v := range it.Setup {
		out.Setup[k] = v
	}
	if removeSource {
		p0 := out.Setup["p0"]
		p0.Hand = slices.DeleteFunc(slices.Clone(p0.Hand), func(n string) bool { return n == it.Card })
		p0.Battlefield = slices.DeleteFunc(slices.Clone(p0.Battlefield), func(n string) bool { return n == it.Card })
		out.Setup["p0"] = p0
	}
	out.Steps = nil
	for i, s := range it.Steps {
		if s.Op == "cast" && s.Card == "p0:"+dropCast {
			continue
		}
		if s.Op == "resolve" && i > 0 && it.Steps[i-1].Op == "cast" && it.Steps[i-1].Card == "p0:"+dropCast {
			continue
		}
		out.Steps = append(out.Steps, s)
	}
	return out
}

func runZone(t *testing.T, it oraclegen.Item) rules.OracleResult {
	t.Helper()
	res, err := rules.RunOracleScenarioJSON(testutil.CorpusRegistry(t), it.Raw())
	if err != nil {
		t.Fatalf("%s: %v", it.ID, err)
	}
	return res
}

// TestStaticZonePermissionIsOfferedOnlyWithTheSource serves play permissions
// as an offered option: the scenario replays clean in gorge with the source,
// and the same checkpoint without the source offers nothing.
func TestStaticZonePermissionIsOfferedOnlyWithTheSource(t *testing.T) {
	for _, tc := range []struct {
		card, key, zone, probe string
		// dropCast is the card whose cast the control omits; self is
		// set for a static on the probe itself (no source to remove).
		dropCast string
		self     bool
	}{
		{"Festival of Embers", "static#0.0", "graveyard", "Shock", "Festival of Embers", false},
		{"Vizier of the Menagerie", "static#0.1", "library_top", "Llanowar Elves", "Vizier of the Menagerie", false},
		{"Conspiracy Unraveler", "static#0.0", "hand", "Shock", "Conspiracy Unraveler", false},
		{"Undead Sprinter", "static#0.0", "graveyard", "Undead Sprinter", "Shock", true},
	} {
		t.Run(tc.card, func(t *testing.T) {
			it, skip := zoneItem(t, tc.card, tc.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			if it.ID != tc.card+"/"+tc.key+"/v1" || it.Template != tc.key {
				t.Fatalf("identity = %q / %q, want the level-B %s row", it.ID, it.Template, tc.key)
			}
			seat := it.Setup["p0"]
			zone := map[string][]string{"graveyard": seat.Graveyard, "library_top": seat.LibraryTop, "hand": seat.Hand}[tc.zone]
			if !slices.Contains(zone, tc.probe) {
				t.Fatalf("precondition: %s absent from p0 %s: %v", tc.probe, tc.zone, zone)
			}
			last := it.Steps[len(it.Steps)-1]
			if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
				t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
			}
			if got := last.Expect[0].Offered; got.Card != "p0:"+tc.probe || got.Kind != "cast" {
				t.Fatalf("offered assertion = %+v", got)
			}
			if res := runZone(t, it); len(res.Fails) != 0 {
				t.Fatalf("with the source: %v", res.Fails)
			}
			// The control must really differ from the item, or it proves nothing.
			ctl := withoutSource(it, tc.dropCast, !tc.self)
			if len(ctl.Steps) >= len(it.Steps) {
				t.Fatalf("precondition: control kept every step (%d of %d)", len(ctl.Steps), len(it.Steps))
			}
			if res := runZone(t, ctl); len(res.Fails) == 0 {
				t.Fatalf("without the source the probe is still offered: the assertion is not the static's")
			}
		})
	}
}

// TestStaticZonePermissionLifelinkOnSpells observes "spells you control have
// lifelink" through the life a damage spell gains its caster.
func TestStaticZonePermissionLifelinkOnSpells(t *testing.T) {
	it, skip := zoneItem(t, "Heartflame Duelist", "static#0.0")
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}
	with := runZone(t, it)
	without := runZone(t, withoutSource(it, "Heartflame Duelist", true))
	life := func(res rules.OracleResult) int32 {
		for _, p := range res.Snapshots[len(res.Snapshots)-1].Players {
			if p.Seat == 0 {
				return p.Life
			}
		}
		t.Fatal("no seat 0 in the snapshot")
		return 0
	}
	if life(with) <= life(without) {
		t.Fatalf("p0 life with the source %d, without %d: lifelink not observed", life(with), life(without))
	}
}

// TestStaticZonePermissionNamedSkips keeps the rows this level cannot
// observe apart from the generic "not observable" bucket: a look-at
// permission, and the Surveyor cycle's graveyard AddAbility$ grant, which the
// engine offers no activation for.
func TestStaticZonePermissionNamedSkips(t *testing.T) {
	for _, tc := range []struct{ card, key, reason string }{
		{"Glarb, Calamity's Augur", "static#0.0", "static look-at not observable"},
		{"Glitch Ghost Surveyor", "static#0.0", "static granted ability in Graveyard is not offered by the engine"},
		{"Goblin Surveyor", "static#0.0", "static granted ability in Graveyard is not offered by the engine"},
		{"Loxodon Surveyor", "static#0.0", "static granted ability in Graveyard is not offered by the engine"},
		{"Mutant Surveyor", "static#0.0", "static granted ability in Graveyard is not offered by the engine"},
	} {
		_, skip := zoneItem(t, tc.card, tc.key)
		if skip == nil || skip.Reason != tc.reason {
			t.Errorf("%s %s skip = %v, want %q", tc.card, tc.key, skip, tc.reason)
		}
	}
}
