package templates

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// probeRequirement returns the static.continuous requirement of name with
// the given key, failing when the card or the requirement is missing so a
// test never passes on an empty loop.
func probeRequirement(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
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

// servedFinal generates the item for name/key, replays it and returns the
// item with its final snapshot.
func servedFinal(t *testing.T, reg *cards.Registry, name, key string) (oraclegen.Item, rules.OracleSnapshot) {
	t.Helper()
	it, skip := GenerateB(reg, name, probeRequirement(t, reg, name, key))
	if skip != nil {
		t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("%s %s does not replay: err=%v fails=%v", name, key, err, res.Fails)
	}
	return it, res.Snapshots[len(res.Snapshots)-1]
}

// permanent finds controller c's permanent called name.
func permanent(s rules.OracleSnapshot, c int, name string) (rules.OracleSnapPerm, bool) {
	for _, p := range s.Permanents {
		if p.Controller == c && p.Name == name {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// printedPT is the printed P/T of a probe card, read from the corpus so the
// assertions never restate it.
func printedPT(t *testing.T, reg *cards.Registry, name string) string {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || c.Faces[0].PT == "" {
		t.Fatalf("precondition: probe %s has no printed P/T", name)
	}
	return c.Faces[0].PT
}

// TestStaticContinuousSubtypeProbe: Camellia's "other Squirrels you control"
// grants menace, which Grizzly Bears (no Squirrel) never shows. The Squirrel
// probe the filter names is on p0's battlefield and gains menace its printed
// card lacks; the Bear gains nothing.
func TestStaticContinuousSubtypeProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, final := servedFinal(t, reg, "Camellia, the Seedmiser", "static#0.0")
	const probe = "Curious Forager"
	if !slices.Contains(it.Setup["p0"].Battlefield, probe) || !slices.Contains(it.Setup["p0"].Battlefield, "Grizzly Bears") {
		t.Fatalf("p0 battlefield %v lacks the %s probe or the Bear", it.Setup["p0"].Battlefield, probe)
	}
	p, ok := permanent(final, 0, probe)
	if !ok {
		t.Fatalf("%s is not on p0's final battlefield: %+v", probe, final.Permanents)
	}
	if c, _ := reg.Lookup(probe); slices.Contains(c.Faces[0].Keywords, "Menace") {
		t.Fatalf("precondition: %s already prints menace", probe)
	}
	if !slices.Contains(p.Keywords, "Menace") {
		t.Fatalf("%s keywords %v lack the granted menace", probe, p.Keywords)
	}
	if p.PT != printedPT(t, reg, probe) {
		t.Fatalf("Camellia grants menace, not P/T: %s has P/T %s", probe, p.PT)
	}
	bear, ok := permanent(final, 0, "Grizzly Bears")
	if !ok {
		t.Fatal("precondition: p0's control Bear is missing")
	}
	if len(bear.Keywords) != 0 {
		t.Fatalf("a Squirrel lord gave the Bear keywords: %v", bear.Keywords)
	}
}

// TestStaticContinuousMountVehicleProbe: Cloudspire Captain's "Mounts and
// Vehicles you control get +1/+1" lands on the Mount probe, a creature.
func TestStaticContinuousMountVehicleProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, final := servedFinal(t, reg, "Cloudspire Captain", "static#0.0")
	const probe = "Gila Courser"
	if !slices.Contains(it.Setup["p0"].Battlefield, probe) {
		t.Fatalf("p0 battlefield %v lacks the Mount probe", it.Setup["p0"].Battlefield)
	}
	p, ok := permanent(final, 0, probe)
	if !ok {
		t.Fatalf("%s is not on p0's final battlefield", probe)
	}
	if printed := printedPT(t, reg, probe); p.PT == printed {
		t.Fatalf("%s P/T %s equals its printed %s", probe, p.PT, printed)
	}
}

// TestStaticContinuousVehicleKeywordProbe: Mu Yanling grants Vehicles flying.
// An uncrewed Vehicle is no creature, so the snapshot has no P/T for it and
// the grant shows as its keyword.
func TestStaticContinuousVehicleKeywordProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	probe, ok := reg.Lookup("Dune Drifter")
	if !ok || len(probe.Faces) == 0 || probe.Faces[0].IsCreature() || slices.Contains(probe.Faces[0].Keywords, "Flying") {
		t.Fatal("precondition: Dune Drifter must be a noncreature without printed flying")
	}
	_, final := servedFinal(t, reg, "Mu Yanling, Wind Rider", "static#0.0")
	p, ok := permanent(final, 0, "Dune Drifter")
	if !ok {
		t.Fatalf("Dune Drifter is not on p0's final battlefield")
	}
	if p.PT != "" || !slices.Contains(p.Keywords, "Flying") {
		t.Fatalf("Dune Drifter pt=%q keywords=%v, want an uncrewed Vehicle with flying", p.PT, p.Keywords)
	}
}

// TestStaticContinuousAttackingProbe: Goblin Oriflamme's "attacking creatures
// you control get +1/+0" shows only once a probe attacks, so the scenario ends
// with p0's Bear declared as an attacker, pumped to 3/2.
func TestStaticContinuousAttackingProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, final := servedFinal(t, reg, "Goblin Oriflamme", "static#0.0")
	var attack *oraclegen.Step
	for i := range it.Steps {
		if it.Steps[i].Op == "attack" {
			attack = &it.Steps[i]
		}
	}
	if attack == nil || len(attack.Attackers) == 0 {
		t.Fatalf("scenario has no attack step: %+v", it.Steps)
	}
	bear, ok := permanent(final, 0, "Grizzly Bears")
	if !ok || !bear.Attacking {
		t.Fatalf("p0's Bear is not attacking: %+v", bear)
	}
	if bear.PT != "3/2" {
		t.Fatalf("attacking Bear P/T = %s, want 3/2", bear.PT)
	}
	opp, ok := permanent(final, 1, "Grizzly Bears")
	if !ok {
		t.Fatal("precondition: p1's control Bear is missing")
	}
	if printed := printedPT(t, reg, "Grizzly Bears"); opp.PT != printed {
		t.Fatalf("a you-control static pumped p1's Bear: %s, printed %s", opp.PT, printed)
	}
}

// TestStaticContinuousSelfAttacking: Kitesail Corsair has flying as long as
// it is attacking. A cast Corsair is summoning sick, so the card is placed on
// the battlefield and attacks itself, ending with the flying it gained.
func TestStaticContinuousSelfAttacking(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, final := servedFinal(t, reg, "Kitesail Corsair", "static#0.0")
	if !slices.Contains(it.Setup["p0"].Battlefield, "Kitesail Corsair") {
		t.Fatalf("the Corsair is not placed on the battlefield: %v", it.Setup["p0"].Battlefield)
	}
	p, ok := permanent(final, 0, "Kitesail Corsair")
	if !ok || !p.Attacking || !slices.Contains(p.Keywords, "Flying") {
		t.Fatalf("Corsair attacking=%v keywords=%v, want attacking with flying", p.Attacking, p.Keywords)
	}
}

// TestStaticContinuousTappedProbe: Adept Watershaper's "other tapped creatures
// you control have indestructible". A setup-tapped p0 permanent is untapped by
// p0's first untap step, so the Bear taps by attacking.
func TestStaticContinuousTappedProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, final := servedFinal(t, reg, "Adept Watershaper", "static#0.0")
	bear, ok := permanent(final, 0, "Grizzly Bears")
	if !ok || !bear.Tapped || !slices.Contains(bear.Keywords, "Indestructible") {
		t.Fatalf("Bear tapped=%v keywords=%v, want a tapped Bear with indestructible", bear.Tapped, bear.Keywords)
	}
}

// TestStaticContinuousBearRowUnchanged: a row the Bear alone already observes
// keeps the one-probe scenario, so its served item (and verdict) is untouched
// by the filter-derived probes.
func TestStaticContinuousBearRowUnchanged(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, _ := servedFinal(t, reg, "Anthem of Champions", "static#0.0")
	if got := it.Setup["p0"].Battlefield; len(got) != 1 || got[0] != "Grizzly Bears" {
		t.Fatalf("p0 battlefield = %v, want only the Bear", got)
	}
	for _, s := range it.Steps {
		if s.Op == "attack" {
			t.Fatalf("a Bear-observed row gained an attack step: %+v", it.Steps)
		}
	}
}

// TestStaticContinuousNamedSkips: a qualifier the setup cannot give a probe
// (counters, a token) is a named skip, not the generic one. Each card's static
// must otherwise show nothing, so the test pins the reason text. A grant of a
// named ability (Bria's Prowess, Ward) is no longer here: the named-keyword
// vocabulary serves those, pinned by TestStaticNamedKeywordGrant.
func TestStaticContinuousNamedSkips(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, want string }{
		{"Vigorbloom Vanguard", "static#0.0", "counters"},
		{"Gideon's Memorial", "static#0.0", "token"},
		{"Firion, Wild Rose Warrior", "static#0.0", "equipped"},
	} {
		_, skip := GenerateB(reg, tc.card, probeRequirement(t, reg, tc.card, tc.key))
		if skip == nil {
			t.Fatalf("%s: served, want a named skip mentioning %q", tc.card, tc.want)
		}
		if !strings.Contains(skip.Reason, tc.want) || strings.Contains(skip.Reason, "not observable") {
			t.Fatalf("%s: skip %q, want a named reason mentioning %q", tc.card, skip.Reason, tc.want)
		}
	}
}

// TestStaticProbeTableIsInert: a probe must not move itself off its printed
// P/T or evergreen keywords, in any shape the template puts it in -- otherwise
// staticObserved would report the probe's self-change as the static's effect
// and an item would assert a static the engine never applied. Each probe is
// checked three ways, all stricter than the served scenario (no Grizzly Bears
// control): cast alone, so its own enter trigger fires; placed on the
// battlefield, because a served scenario places probes by setup; and placed
// and attacking, so its own begin-combat or attacks trigger fires.
func TestStaticProbeTableIsInert(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	seen := map[string]bool{}
	for _, row := range staticProbeTable {
		if seen[row.card] {
			continue
		}
		seen[row.card] = true
		row := row
		t.Run(row.word, func(t *testing.T) {
			specs := staticProbeSpecs(reg, []string{row.card})
			spec, ok := specs[row.card]
			if !ok {
				t.Fatalf("precondition: probe %s missing from the corpus", row.card)
			}
			check := func(shape string, final rules.OracleSnapshot) {
				t.Helper()
				p, ok := permanent(final, 0, row.card)
				if !ok {
					t.Fatalf("%s: probe %s is not on p0's battlefield", shape, row.card)
				}
				if p.PT != spec.pt || oraclediff.EvergreenKeywords(p.Keywords) != spec.keywords {
					t.Errorf("%s: probe %s changed itself: PT=%q (printed %q) keywords=%q (printed %q)",
						shape, row.card, p.PT, spec.pt, oraclediff.EvergreenKeywords(p.Keywords), spec.keywords)
				}
			}
			// Cast alone: enter triggers fire against an otherwise empty board,
			// which is how a self-targeting enter ability (Gurmag Rakshasa's
			// "target creature you control gets +2/+2") pumps the probe itself.
			c, ok := reg.Lookup(row.card)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: probe %s missing from the corpus", row.card)
			}
			f := c.Faces[0]
			if mana, why := oraclegen.PoolFor(f.ManaCost); why == "" {
				if it, sk := castResolveWith(reg, f, row.card, mana, nil, nil); sk == nil {
					res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
					if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
						t.Fatalf("cast-alone scenario does not replay: err=%v fails=%v", err, res.Fails)
					}
					check("cast alone", res.Snapshots[len(res.Snapshots)-1])
				}
			}
			for _, attack := range []bool{false, true} {
				if attack && !spec.creature {
					continue // a non-creature probe never attacks
				}
				sc := oraclegen.Scenario{
					Setup: map[string]oraclegen.Seat{
						"p0": {Battlefield: []string{row.card}},
						"p1": {},
					},
				}
				if attack {
					sc.Steps = append(sc.Steps, oraclegen.Step{
						Op: "attack", Seat: 0, Defender: "p1",
						Attackers: []string{"p0:" + row.card},
					})
				}
				b, _ := json.Marshal(sc)
				res, err := rules.RunOracleScenarioJSON(reg, b)
				if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
					t.Fatalf("attack=%v: probe-alone scenario does not replay: err=%v fails=%v", attack, err, res.Fails)
				}
				check(fmt.Sprintf("attack=%v", attack), res.Snapshots[len(res.Snapshots)-1])
			}
		})
	}
}

// TestStaticProbeTable: every table probe exists, is the type its word names
// (Outlaw: one of the types that make an Outlaw; withFlying: a flier), has no
// static or replacement of its own to confound the observation, and reads a
// printed P/T the comparison can use.
func TestStaticProbeTable(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	outlaw := []string{"Assassin", "Mercenary", "Pirate", "Rogue", "Warlock"}
	seen := map[string]bool{}
	for _, row := range staticProbeTable {
		if seen[row.word] {
			t.Errorf("word %s appears twice; the first row wins, so the second is dead", row.word)
		}
		seen[row.word] = true
		c, ok := reg.Lookup(row.card)
		if !ok || len(c.Faces) != 1 {
			t.Errorf("%s: probe %s is not a single-faced corpus card", row.word, row.card)
			continue
		}
		f := c.Faces[0]
		if len(f.Statics) != 0 || len(f.Repls) != 0 {
			t.Errorf("%s: probe %s has a static or replacement of its own", row.word, row.card)
		}
		has := func(ty string) bool { return oraclegen.HasType(f, ty) }
		switch row.word {
		case "Outlaw":
			ok = false
			for _, ty := range outlaw {
				ok = ok || has(ty)
			}
		case "withFlying":
			ok = strings.Contains(strings.Join(f.Keywords, " "), "Flying")
		default:
			ok = has(row.word)
		}
		if !ok {
			t.Errorf("%s: probe %s (%v %v) does not have what the word names", row.word, row.card, f.Types, f.Keywords)
		}
		isVehicle := has("Vehicle")
		if !isVehicle && (f.PT == "" || strings.Contains(f.PT, "*")) {
			t.Errorf("%s: probe %s has no fixed printed P/T (%q)", row.word, row.card, f.PT)
		}
	}
}
