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

// selfFilterReq is name's static.continuous requirement with the given key.
func selfFilterReq(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
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

// selfFilterItem generates name/key, fails on a skip, replays it in gorge and
// returns the item and the final snapshot.
func selfFilterItem(t *testing.T, reg *cards.Registry, name, key string) (oraclegen.Item, rules.OracleSnapshot) {
	t.Helper()
	it, skip := templates.GenerateB(reg, name, selfFilterReq(t, reg, name, key))
	if skip != nil {
		t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("%s %s does not replay: err=%v fails=%v", name, key, err, res.Fails)
	}
	return it, res.Snapshots[len(res.Snapshots)-1]
}

func permNamed(snap rules.OracleSnapshot, name string) (rules.OracleSnapPerm, bool) {
	for _, p := range snap.Permanents {
		if p.Controller == 0 && p.Name == name {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// TestStaticSelfCounterGate: a static gated on its own counters through
// CheckSVar$ X with SVar:X:Count$CardCounters.ALL (Warden of the Inner Sky)
// was unobservable because the bare scenario holds no counters. The fixture
// places the card on the battlefield holding the counters its gate names, so
// the grant is live at the first checkpoint. The precondition asserts the
// printed card carries neither granted keyword and the setup counters are
// exactly the gate's, so the test cannot pass on a card that already had them.
func TestStaticSelfCounterGate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Warden of the Inner Sky"
	c, _ := reg.Lookup(name)
	printed := strings.Join(c.Faces[0].Keywords, " ")
	for _, kw := range []string{"Flying", "Vigilance"} {
		if strings.Contains(printed, kw) {
			t.Fatalf("precondition: %s already prints %s", name, kw)
		}
	}
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	if got := it.Setup["p0"].Counters[name]["P1P1"]; got != 3 {
		t.Fatalf("%s setup counters = %v, want P1P1:3", name, it.Setup["p0"].Counters[name])
	}
	p, ok := permNamed(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	for _, kw := range []string{"Flying", "Vigilance"} {
		if !slices.Contains(p.Keywords, kw) {
			t.Fatalf("%s keywords = %v, want %s granted", name, p.Keywords, kw)
		}
	}
}

// TestStaticFilterHasCounters: Formation Breaker's "as long as you control a
// creature with a counter on it" pump was unobservable because the bare
// fixture creatures held no counters. The fixture puts a +1/+1 counter on a
// stand-in creature (not the probe), so the pump lands on the card itself. The
// precondition asserts the counter is on a card that is not the probe and the
// printed P/T differs from the observed one.
func TestStaticFilterHasCounters(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Formation Breaker"
	c, _ := reg.Lookup(name)
	printed := c.Faces[0].PT
	if printed != "2/1" {
		t.Fatalf("precondition: %s printed P/T = %q, want 2/1", name, printed)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.1")
	onCounter := ""
	for card, kinds := range it.Setup["p0"].Counters {
		if kinds["P1P1"] > 0 {
			onCounter = card
		}
	}
	if onCounter == "" {
		t.Fatalf("%s: no fixture creature holds a counter: %v", name, it.Setup["p0"].Counters)
	}
	if onCounter == "Grizzly Bears" {
		t.Fatalf("%s: the counter is on the probe, which would shift its compared P/T", name)
	}
	p, ok := permNamed(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if p.PT == printed {
		t.Fatalf("%s P/T %s equals printed %s; the HasCounters gate did not pump it", name, p.PT, printed)
	}
}

// TestStaticFilterNamedCount: Phoenix Fleet Airship's "eight or more
// permanents named Phoenix Fleet Airship" type-change was unobservable because
// the bare scenario holds one. The fixture places eight copies (and the card
// itself, so nine), so the Vehicle becomes an artifact creature. The
// precondition asserts the card is printed as a non-creature Vehicle and the
// fixture really reaches eight permanents of that name.
func TestStaticFilterNamedCount(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Phoenix Fleet Airship"
	c, _ := reg.Lookup(name)
	if c.Faces[0].IsCreature() {
		t.Fatalf("precondition: %s is printed as a creature, so the type change is not observable", name)
	}
	_, final := selfFilterItem(t, reg, name, "static#0.0")
	n := 0
	for _, p := range final.Permanents {
		if p.Controller == 0 && p.Name == name {
			n++
		}
	}
	if n < 8 {
		t.Fatalf("only %d permanents named %s on the final battlefield, want >=8", n, name)
	}
	p, ok := permNamed(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if !slices.Contains(p.Types, "Creature") {
		t.Fatalf("%s types = %v, want Creature added", name, p.Types)
	}
}

// TestStaticFilterLegendaryProbe: Serah Farron's back-face "Legendary
// creatures you control get +2/+2" was unobservable because the probe table
// had no legendary creature. The fixture adds Barktooth Warbeard, a vanilla
// legendary creature, so the pump is observable. The precondition asserts the
// probe is present and its printed P/T differs from the observed one.
func TestStaticFilterLegendaryProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Serah Farron"
	_, final := selfFilterItem(t, reg, name, "static#1.1")
	probe, ok := permNamed(final, "Barktooth Warbeard")
	if !ok {
		t.Fatalf("legendary probe Barktooth Warbeard is not on the final battlefield")
	}
	pc, _ := reg.Lookup("Barktooth Warbeard")
	if probe.PT == pc.Faces[0].PT {
		t.Fatalf("Barktooth Warbeard P/T %s equals printed %s; the legendary pump did not land", probe.PT, pc.Faces[0].PT)
	}
}

// TestStaticSourceCounterEquipment: Excalibur II's "equipped creature gets
// +1/+1 for each charge counter on CARDNAME" was unobservable because the bare
// scenario holds no charge counters. The fixture places the Equipment holding
// one charge counter and attaches it to the probe. The precondition asserts
// the counter is seeded on the source, the attach step exists, and the probe's
// printed P/T differs from the observed one.
func TestStaticSourceCounterEquipment(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Excalibur II"
	const probeName = "Grizzly Bears"
	pc, _ := reg.Lookup(probeName)
	printed := pc.Faces[0].PT
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	if got := it.Setup["p0"].Counters[name]["CHARGE"]; got != 1 {
		t.Fatalf("%s setup counters = %v, want CHARGE:1", name, it.Setup["p0"].Counters[name])
	}
	attached := false
	for _, st := range it.Steps {
		if st.Op == "attach" && st.Card == "p0:"+name && st.AttachedTo == "p0:"+probeName {
			attached = true
		}
	}
	if !attached {
		t.Fatalf("%s: no attach step from the Equipment to the probe: %+v", name, it.Steps)
	}
	probe, ok := permNamed(final, probeName)
	if !ok {
		t.Fatalf("%s: probe %s is not on the final battlefield", name, probeName)
	}
	if probe.PT == printed {
		t.Fatalf("%s: probe P/T %s equals printed %s; the charge-counter amount did not land", name, probe.PT, printed)
	}
}

// TestStaticEnchantedProbe: a static on enchanted creatures you control (A
// Tale for the Ages' +2/+2, Archon of the Wild Rose's 4/4 flier) was
// unobservable because the bare probe is not enchanted. The fixture attaches
// an inert Aura (Pacifism) to the probe, so the static lands. The precondition
// asserts the Aura is really attached and the probe's printed P/T differs from
// the observed one.
func TestStaticEnchantedProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const probeName = "Grizzly Bears"
	pc, _ := reg.Lookup(probeName)
	printed := pc.Faces[0].PT
	for _, tc := range []struct{ card, key string }{
		{"A Tale for the Ages", "static#0.0"},
		{"Archon of the Wild Rose", "static#0.0"},
	} {
		tc := tc
		t.Run(tc.card, func(t *testing.T) {
			it, final := selfFilterItem(t, reg, tc.card, tc.key)
			attached := false
			for _, st := range it.Steps {
				if st.Op == "attach" && st.AttachedTo == "p0:"+probeName {
					attached = true
				}
			}
			if !attached {
				t.Fatalf("%s: no attach step onto the probe: %+v", tc.card, it.Steps)
			}
			probe, ok := permNamed(final, probeName)
			if !ok {
				t.Fatalf("%s: probe %s is not on the final battlefield", tc.card, probeName)
			}
			if probe.PT == printed {
				t.Fatalf("%s: probe P/T %s equals printed %s; the enchanted gate did not land", tc.card, probe.PT, printed)
			}
		})
	}
}
