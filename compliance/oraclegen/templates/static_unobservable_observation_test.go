package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// unobservableReq is name's static.continuous requirement with the given key,
// failing on a missing card or key so no test passes on an empty loop.
func unobservableReq(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			if r.Sub != "static.continuous" {
				t.Fatalf("precondition: %s %s is %s, want static.continuous", name, key, r.Sub)
			}
			return r
		}
	}
	t.Fatalf("precondition: %s has no requirement %s", name, key)
	return levelb.Requirement{}
}

// unobservableItem generates name/key, fails on a skip, and replays it in
// gorge, returning the item and its final snapshot.
func unobservableItem(t *testing.T, reg *cards.Registry, name, key string) (oraclegen.Item, rules.OracleSnapshot) {
	t.Helper()
	it, skip := templates.GenerateB(reg, name, unobservableReq(t, reg, name, key))
	if skip != nil {
		t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("%s %s does not replay: err=%v fails=%v", name, key, err, res.Fails)
	}
	return it, res.Snapshots[len(res.Snapshots)-1]
}

// TestStaticAbilityRemovalOnKeywordedHost: an Aura that removes all abilities
// of the permanent it enchants (Flood the Engine, Frozen in Ice, Honest Work)
// is unobservable on the vanilla fixture; the scenario enchants a host that
// prints an ability, so the removal moves the host's keyword (or its P/T for
// Honest Work's Humble Merchant rewrite). The host and its printed line are
// asserted as preconditions, so the test cannot pass on a scenario that never
// attached or on a host that had nothing to lose.
func TestStaticAbilityRemovalOnKeywordedHost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Flood the Engine", "Frozen in Ice", "Honest Work"} {
		name := name
		t.Run(name, func(t *testing.T) {
			it, final := unobservableItem(t, reg, name, "static#0.0")
			var hostRef string
			for _, st := range it.Steps {
				if st.Op == "cast" && st.Card == "p0:"+name && len(st.Targets) == 1 {
					hostRef = st.Targets[0]
				}
			}
			if hostRef == "" {
				t.Fatalf("%s: no single-target cast step", name)
			}
			hostName := hostRef[strings.Index(hostRef, ":")+1:]
			hc, ok := reg.Lookup(hostName)
			if !ok || len(hc.Faces) == 0 {
				t.Fatalf("%s: host %s not in the corpus", name, hostName)
			}
			if len(hc.Faces[0].Keywords) == 0 {
				t.Fatalf("%s: host %s prints no ability to remove", name, hostName)
			}
			var host *rules.OracleSnapPerm
			for i := range final.Permanents {
				if final.Permanents[i].Ref == hostRef {
					host = &final.Permanents[i]
				}
			}
			if host == nil {
				t.Fatalf("%s: host %s not on the final battlefield", name, hostRef)
			}
			if strings.Join(host.Keywords, " ") == strings.Join(hc.Faces[0].Keywords, " ") && host.PT == hc.Faces[0].PT {
				t.Fatalf("%s: host %s unchanged: kw=%v pt=%s, printed kw=%v pt=%s",
					name, hostName, host.Keywords, host.PT, hc.Faces[0].Keywords, hc.Faces[0].PT)
			}
		})
	}
}

// TestStaticMaxHandSizeObservation: a Continuous SetMaxHandSize$ static
// changes no snapshot field, so the item asserts gorge's effective CR 514.1
// maximum through the runner's max_hand_size expectation. unobservableItem
// replays the scenario, so a wrong maximum fails there; the test also pins the
// exact value the engine prices.
func TestStaticMaxHandSizeObservation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		want      int
	}{
		{"Vnwxt, Verbose Host", "static#0.0", 1 << 20},
		{"The Ten Rings", "static#0.0", 10},
		{"Doctor Octopus, Master Planner", "static#0.1", 8},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			it, _ := unobservableItem(t, reg, tc.name, tc.key)
			found := false
			for _, st := range it.Steps {
				for _, e := range st.Expect {
					if v, ok := e.MaxHandSize["p0"]; ok {
						found = true
						if v != tc.want {
							t.Fatalf("%s: max_hand_size p0 = %d, want %d", tc.name, v, tc.want)
						}
					}
				}
			}
			if !found {
				t.Fatalf("%s: scenario carries no max_hand_size expectation: %+v", tc.name, it.Steps)
			}
		})
	}
}

// TestStaticComputedCountHandFixture: Stingerback Terror's "gets -1/-1 for
// each card in your hand" is a signed computed pump; the fixture holds a card
// in p0's hand so the count is nonzero and the card's P/T moves off its
// printed line. Both the printed P/T and the fixture card are asserted, so the
// test cannot pass on a scenario that left the count at zero.
func TestStaticComputedCountHandFixture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Stingerback Terror"
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || c.Faces[0].PT == "" || strings.Contains(c.Faces[0].PT, "*") {
		t.Fatalf("precondition: %s must print a fixed P/T", name)
	}
	printed := c.Faces[0].PT
	it, final := unobservableItem(t, reg, name, "static#0.0")
	if !slices.Contains(it.Setup["p0"].Hand, "Wastes") {
		t.Fatalf("p0 hand %v holds no Wastes fixture, so the hand count is zero", it.Setup["p0"].Hand)
	}
	var perm *rules.OracleSnapPerm
	for i := range final.Permanents {
		if final.Permanents[i].Name == name {
			perm = &final.Permanents[i]
		}
	}
	if perm == nil {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if perm.PT == printed {
		t.Fatalf("%s P/T %s equals printed %s; the hand count did not move it", name, perm.PT, printed)
	}
}
