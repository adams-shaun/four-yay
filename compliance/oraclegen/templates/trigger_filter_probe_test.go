package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func triggerFilterOf(t *testing.T, reg *cards.Registry, name string, idx int) string {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces[0].Triggers) <= idx {
		t.Fatalf("precondition: %s trigger %d is unavailable", name, idx)
	}
	return c.Faces[0].Triggers[idx].ParamStr(cards.PKValidCard)
}

// TestTriggerProbeSatisfiesFilter: the probe the template picks is accepted
// by gorge's own filter matcher, and the probes the old fixed list tried first
// (Shock, Grizzly Bears) are not, so an item that plays through proves the
// choice came from the filter.
func TestTriggerProbeSatisfiesFilter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, sub string
		slot           int
		zone           state.Zone
		oldProbes      []string
	}{
		{"Mage Tower Referee", "trigger#0.0", "trigger.spell-cast", 0, state.ZStack, []string{"Shock", "Grizzly Bears", "Giant Growth"}},
		{"Geometer's Arthropod", "trigger#0.0", "trigger.spell-cast", 0, state.ZStack, []string{"Shock", "Grizzly Bears", "Giant Growth"}},
		{"Boggart Mischief", "trigger#0.1", "trigger.dies-other", 1, state.ZBattlefield, []string{"Grizzly Bears"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := triggerFilterOf(t, reg, tc.name, tc.slot)
			fp := newFilterProbe(filter, tc.zone)
			if !fp.decided {
				t.Fatalf("precondition: matcher does not decide %q", filter)
			}
			for _, old := range tc.oldProbes {
				card, ok := reg.Lookup(old)
				if !ok {
					t.Fatalf("precondition: %s missing", old)
				}
				if fp.accepts(card) {
					t.Fatalf("precondition: the old probe %s already satisfies %q; the test cannot tell the new choice from the old", old, filter)
				}
			}
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			chosen := ""
			for _, st := range it.Scenario.Steps {
				if st.Op == "cast" && tc.zone == state.ZStack {
					chosen = strings.TrimPrefix(strings.SplitN(st.Card, "#", 2)[0], "p0:")
				}
			}
			if tc.zone == state.ZBattlefield {
				for _, b := range it.Scenario.Setup["p0"].Battlefield {
					if b != tc.name {
						chosen = b
					}
				}
			}
			card, ok := reg.Lookup(chosen)
			if chosen == "" || !ok {
				t.Fatalf("no probe card found in the item: %+v", it.Scenario)
			}
			if !fp.accepts(card) {
				t.Fatalf("chosen probe %s is rejected by %q", chosen, filter)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("no snapshot shows an ability on the stack sourced by %s", tc.name)
			}
		})
	}
}

// TestSpellCastCountFromActivatorCondition: "your second spell" (EQ2), "other
// than your first spell" (GT1) and "your third spell" (EQ3) need that many
// casts in one turn.
func TestSpellCastCountFromActivatorCondition(t *testing.T) {
	for cond, want := range map[string]int{"": 1, "EQ2": 2, "GT1": 2, "EQ3": 3, "GE2": 2, "EQ9": 1, "weird": 1} {
		if got := activatorCastCount(cond); got != want {
			t.Errorf("activatorCastCount(%q) = %d, want %d", cond, got, want)
		}
	}
	reg := testutil.CorpusRegistry(t)
	for name, casts := range map[string]int{"Geralf, the Fleshwright": 2, "Emeritus of Conflict // Lightning Bolt": 3} {
		it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.spell-cast")
		n := 0
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" {
				n++
			}
		}
		if n != casts {
			t.Errorf("%s: %d cast steps, want %d", name, n, casts)
		}
	}
}

// TestTriggerFilterGapsAreNamed: a filter the cause cannot meet is skipped
// with the predicate or qualifier named, not as a bare "did not fire".
func TestTriggerFilterGapsAreNamed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, want string }{
		{"Namor the Sub-Mariner", "trigger#0.0", "ManaCostPartialBlue"},
		{"Codie, Ravenous Codex", "trigger#0.0", "prepared"},
		// Ares, God of War's attacking-victim row is served by the g17
		// mid-combat destroy cause (trigger_victim_specials.go); no census
		// card names an unserved dying-victim qualifier any more.
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s missing", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				_, skip := GenerateB(reg, tc.name, r)
				if skip == nil || !strings.Contains(skip.Reason, tc.want) || strings.HasSuffix(skip.Reason, "did not fire") {
					t.Fatalf("skip = %+v, want a reason naming %q", skip, tc.want)
				}
				return
			}
			t.Fatalf("precondition: %s has no requirement %s", tc.name, tc.key)
		})
	}
}
