package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestStaticNotObservableShapes pins the generator mistakes behind
// "static effect not observable on a probe or the card" that the scenario, not
// the engine, made: a cast prelude that collides with the probe (B1), a
// counter-gated base that drops the filter's probes (B2), an effect landing on
// the attach host rather than a probe (A1) and a back-face Equipment that was
// never attached (A2).
func TestStaticNotObservableShapes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gen := func(t *testing.T, card, key string) (oraclegen.Item, *oraclegen.Skip) {
		t.Helper()
		c, ok := reg.Lookup(card)
		if !ok {
			t.Fatalf("%s absent from the corpus", card)
		}
		for _, r := range levelb.Requirements(c) {
			if r.Key == key && r.Sub == "static.continuous" {
				return GenerateB(reg, card, r)
			}
		}
		t.Fatalf("%s: no static.continuous requirement %s", card, key)
		return oraclegen.Item{}, nil
	}
	served := func(t *testing.T, card, key string) oraclegen.Scenario {
		t.Helper()
		it, skip := gen(t, card, key)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", card, key, skip.Reason)
		}
		return it.Scenario
	}
	casts := func(sc oraclegen.Scenario, card string) bool {
		for _, st := range sc.Steps {
			if st.Op == "cast" && st.Card == "p0:"+card {
				return true
			}
		}
		return false
	}
	has := func(xs []string, x string) bool { return hasString(xs, x) }

	t.Run("B1 prelude casts a stand-in, not the probe", func(t *testing.T) {
		for _, row := range []struct{ card, key string }{
			{"Armory Mice", "static#0.0"},
			{"Gallant Pie-Wielder", "static#0.0"},
			{"Grand Ball Guest", "static#0.0"},
			{"Tuinvale Guide", "static#0.0"},
			{"Bristlebane Outrider", "static#0.1"},
		} {
			sc := served(t, row.card, row.key)
			p0 := sc.Setup["p0"]
			if !has(p0.Battlefield, staticProbe) {
				t.Errorf("%s: probe %s is not on p0's battlefield: %v", row.card, staticProbe, p0.Battlefield)
			}
			if has(p0.Hand, staticProbe) || casts(sc, staticProbe) {
				t.Errorf("%s: the prelude still casts the probe card %s", row.card, staticProbe)
			}
			if !has(p0.Hand, staticProbeStandIn) || !casts(sc, staticProbeStandIn) {
				t.Errorf("%s: the prelude does not cast the stand-in %s (hand %v)", row.card, staticProbeStandIn, p0.Hand)
			}
		}
	})

	t.Run("B2 counter-gated base keeps the filter's probes", func(t *testing.T) {
		for _, row := range []struct{ card, key, probe string }{
			{"Captain America, Super-Soldier", "static#0.0", "Hero in Training"},
			{"Zhao, the Moon Slayer", "static#0.0", "Dryad Arbor"},
		} {
			sc := served(t, row.card, row.key)
			if bf := sc.Setup["p0"].Battlefield; !has(bf, row.probe) {
				t.Errorf("%s: probe %s missing from p0's battlefield %v", row.card, row.probe, bf)
			}
			if len(sc.Steps) != 0 {
				t.Errorf("%s: a counter-gated scenario has steps: %v", row.card, sc.Steps)
			}
		}
	})

	t.Run("A1 effect on the attach host that is not a probe", func(t *testing.T) {
		for _, row := range []struct{ card, key string }{
			{"Puppet Crafting", "static#0.0"},
			{"Shimmerwilds Growth", "static#0.0"},
		} {
			it, skip := gen(t, row.card, row.key)
			if skip != nil {
				t.Errorf("%s skipped: %s", row.card, skip.Reason)
				continue
			}
			c, _ := reg.Lookup(row.card)
			f := c.Faces[0]
			st, _ := staticSlotOf(f, levelb.Requirement{Slot: "0"})
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Snapshots) == 0 {
				t.Fatalf("%s does not replay: %v", row.card, err)
			}
			snap := res.Snapshots[len(res.Snapshots)-1]
			probes := staticProbeSpecs(reg, []string{staticProbe})
			// Precondition: the probes alone show nothing, so the row serves
			// only through the attach host.
			if staticObserved(snap, f, row.card, st, probes) {
				t.Errorf("%s: a probe already shows the effect, the host path is not exercised", row.card)
			}
			if !staticObserved(snap, f, row.card, st, staticWithAttachHost(reg, snap, f.Name, probes)) {
				t.Errorf("%s: the attach host shows no effect", row.card)
			}
		}
	})

	t.Run("A2 back-face Equipment is attached", func(t *testing.T) {
		for _, row := range []struct{ card, key string }{
			{"Idol of the Deep King", "static#1.0"},
			{"Sidequest: Play Blitzball", "static#1.0"},
		} {
			sc := served(t, row.card, row.key)
			var attach *oraclegen.Step
			for i := range sc.Steps {
				if sc.Steps[i].Op == "attach" {
					attach = &sc.Steps[i]
				}
			}
			if attach == nil {
				t.Errorf("%s: no attach step in %v", row.card, sc.Steps)
				continue
			}
			if attach.Card != "p0:"+row.card || attach.AttachedTo != "p0:"+staticProbe {
				t.Errorf("%s: attach step %+v", row.card, *attach)
			}
			if !has(sc.Setup["p0"].BackFace, row.card) {
				t.Errorf("%s: not placed on its back face", row.card)
			}
		}
	})

	t.Run("an out-of-scope row keeps the generic skip", func(t *testing.T) {
		_, skip := gen(t, "Crystal Barricade", "static#0.0")
		if skip == nil {
			t.Fatal("Crystal Barricade (player hexproof) is served, want the generic skip")
		}
		if !strings.HasSuffix(skip.Reason, "effect not observable on a probe or the card") {
			t.Errorf("Crystal Barricade skip = %q", skip.Reason)
		}
	})
}
