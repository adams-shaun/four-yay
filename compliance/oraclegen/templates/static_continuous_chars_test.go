package templates_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// staticSkipReason is the skip reason of name's static.continuous requirement
// key, failing when the requirement is missing or the row is served.
func staticSkipReason(t *testing.T, name, key string) string {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		if r.Sub != "static.continuous" {
			t.Fatalf("precondition: %s %s classified %s", name, key, r.Sub)
		}
		_, skip := templates.GenerateB(reg, name, r)
		if skip == nil {
			t.Fatalf("%s %s is served, want a skip", name, key)
		}
		return skip.Reason
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return ""
}

// TestStaticContinuousCDAPowerToughness: Burrowguard Mentor prints */* and
// defines its own P/T. The CDA is the observation: the card's line carries
// whatever P/T gorge computes, which is exactly what XMage's value is compared
// to.
func TestStaticContinuousCDAPowerToughness(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Burrowguard Mentor")
	if !ok || !strings.Contains(c.Faces[0].PT, "*") {
		t.Fatalf("precondition: Burrowguard Mentor must print a * P/T")
	}
	_, frozen := continuousItem(t, "Burrowguard Mentor", "static#0.0")
	l, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Burrowguard Mentor ", "")
	if !ok {
		t.Fatalf("permanents+ lacks the Mentor: %v", lastLines(t, frozen, "permanents+"))
	}
	if !strings.Contains(l, "/") {
		t.Fatalf("the Mentor's line carries no P/T: %q", l)
	}
}

// TestStaticContinuousGainControl: Kitnap's Aura takes p1's probe. p1's Bear is
// c1 o1 before the static and c0 o1 after -- the controller field, not P/T.
func TestStaticContinuousGainControl(t *testing.T) {
	_, frozen := continuousItem(t, "Kitnap", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents-"), "c1 o1 Grizzly Bears ", " 2/2"); !ok {
		t.Fatalf("permanents- lacks p1's Bear under p1's control: %v", lastLines(t, frozen, "permanents-"))
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c0 o1 Grizzly Bears ", ""); !ok {
		t.Fatalf("permanents+ lacks p1's Bear under p0's control: %v", lastLines(t, frozen, "permanents+"))
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c1 o1 Grizzly Bears ", ""); ok {
		t.Fatalf("p1 still controls its Bear: %v", lastLines(t, frozen, "permanents+"))
	}
}

// TestStaticContinuousTypeChange: Toph, the First Metalbender's static adds
// the Land type to artifact permanents you control; the Ornithopter probe the
// type-derived plan adds gains it, so its type line moves with P/T and
// keywords unchanged.
func TestStaticContinuousTypeChange(t *testing.T) {
	_, frozen := continuousItem(t, "Toph, the First Metalbender", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents-"), "c0 o0 Ornithopter [artifact creature thopter]", ""); !ok {
		t.Fatalf("permanents- lacks the plain Ornithopter probe: %v", lastLines(t, frozen, "permanents-"))
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Ornithopter [artifact creature land thopter]", ""); !ok {
		t.Fatalf("permanents+ lacks the Ornithopter that became a land: %v", lastLines(t, frozen, "permanents+"))
	}
}

// TestStaticContinuousUnobservedStaysSkipped: a conditional self static the
// fixture leaves false (Living Conundrum needs an empty library) shows no
// change, and a CDA-marked static on a card that prints a plain P/T is not a
// characteristic-defining P/T (Colossal Rattlewurm). Neither is served.
func TestStaticContinuousUnobservedStaysSkipped(t *testing.T) {
	if got, want := staticSkipReason(t, "Living Conundrum", "static#0.0"), "static effect not observable on a probe or the card"; got != want {
		t.Fatalf("Living Conundrum skip = %q, want %q", got, want)
	}
	if got := staticSkipReason(t, "Colossal Rattlewurm", "static#0.0"); !strings.Contains(got, "keywords outside the compared evergreen set") {
		t.Fatalf("Colossal Rattlewurm skip = %q, want the keyword gap", got)
	}
}

// TestStaticContinuousNamedGaps: the shapes the widened observation still
// cannot reach carry their own reason rather than the generic one.
func TestStaticContinuousNamedGaps(t *testing.T) {
	for _, row := range []struct{ card, key, want string }{
		{"Flood the Engine", "static#0.0", "removes the abilities of a permanent the fixture gives none"},
		{"Lumbering Worldwagon", "static#0.0", "characteristic-defining P/T of a non-creature"},
		{"Vnwxt, Verbose Host", "static#0.0", "hand size"},
	} {
		if got := staticSkipReason(t, row.card, row.key); !strings.Contains(got, row.want) {
			t.Errorf("%s %s skip = %q, want it to contain %q", row.card, row.key, got, row.want)
		}
	}
}
