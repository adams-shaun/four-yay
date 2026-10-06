package templates_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// continuousItem generates the static.continuous item for name's requirement
// key, failing on any precondition that would make the test vacuous: the
// requirement exists and is static.continuous, and the item carries the
// keywords opt-in and a probe on both seats.
func continuousItem(t *testing.T, name, key string) (oraclegen.Item, []compliance.Frozen) {
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
			t.Fatalf("precondition: %s %s classified %s, want static.continuous", name, key, r.Sub)
		}
		it, skip := templates.GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s: %s", name, key, skip.Reason)
		}
		if len(it.Compare) != 1 || it.Compare[0] != "keywords" {
			t.Fatalf("item Compare = %v, want [keywords]", it.Compare)
		}
		for _, seat := range []string{"p0", "p1"} {
			if !contains(it.Setup[seat].Battlefield, "Grizzly Bears") {
				t.Fatalf("%s's battlefield %v holds no Grizzly Bears probe", seat, it.Setup[seat].Battlefield)
			}
		}
		frozen, err := gate.Freeze(reg, it)
		if err != nil {
			t.Fatalf("freeze: %v", err)
		}
		return it, frozen
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return oraclegen.Item{}, nil
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// lastLines returns the lines of a frozen permanents field at the last
// post-setup checkpoint.
func lastLines(t *testing.T, frozen []compliance.Frozen, field string) []string {
	t.Helper()
	var at string
	for _, f := range frozen {
		if f.Field == field && f.At != "" {
			at = f.At
		}
	}
	if at == "" {
		t.Fatalf("no frozen %s at a post-setup checkpoint: %+v", field, frozen)
	}
	for _, f := range frozen {
		if f.Field == field && f.At == at {
			return strings.Split(f.Value, "\n")
		}
	}
	return nil
}

// line finds the line starting with prefix and ending in suffix.
func line(lines []string, prefix, suffix string) (string, bool) {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) && strings.HasSuffix(l, suffix) {
			return l, true
		}
	}
	return "", false
}

// TestStaticContinuousAnthem: Anthem of Champions' "creatures you control get
// +1/+1" lands on p0's probe. The frozen permanents- line holds the 2/2 probe
// it left, so the change is measured against an unmodified one, and p1's
// probe (not "you control") is untouched.
func TestStaticContinuousAnthem(t *testing.T) {
	_, frozen := continuousItem(t, "Anthem of Champions", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents-"), "c0 o0 Grizzly Bears ", " 2/2"); !ok {
		t.Fatalf("permanents- lacks p0's 2/2 probe: %v", lastLines(t, frozen, "permanents-"))
	}
	if l, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Grizzly Bears ", " 3/3"); !ok {
		t.Fatalf("permanents+ lacks p0's 3/3 probe: %v", lastLines(t, frozen, "permanents+"))
	} else if strings.Contains(l, "kw=") {
		t.Fatalf("an anthem grants no keyword, got %q", l)
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c1 o1 Grizzly Bears ", ""); ok {
		t.Fatalf("p1's probe changed under a you-control anthem: %v", lastLines(t, frozen, "permanents+"))
	}
}

// TestStaticContinuousKeywordGrant: Garruk's Uprising's "creatures you control
// have trample" shows as kw=trample on p0's probe, the opt-in keywords field.
func TestStaticContinuousKeywordGrant(t *testing.T) {
	_, frozen := continuousItem(t, "Garruk's Uprising", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents-"), "c0 o0 Grizzly Bears ", " 2/2"); !ok {
		t.Fatalf("permanents- lacks p0's keywordless probe: %v", lastLines(t, frozen, "permanents-"))
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Grizzly Bears ", " 2/2 kw=trample"); !ok {
		t.Fatalf("permanents+ lacks p0's trample probe: %v", lastLines(t, frozen, "permanents+"))
	}
}

// TestStaticContinuousSelfConditionTrue: Inspiring Paladin has +1/+0 and first
// strike as long as it is your turn. The fixture casts it on p0's turn 1, so
// the condition is TRUE and the card's own line carries 3/3 first strike.
func TestStaticContinuousSelfConditionTrue(t *testing.T) {
	_, frozen := continuousItem(t, "Inspiring Paladin", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Inspiring Paladin ", " 3/3 kw=first strike"); !ok {
		t.Fatalf("permanents+ lacks the Paladin at 3/3 first strike: %v", lastLines(t, frozen, "permanents+"))
	}
}

// TestStaticContinuousSelfConditionFalse: Brightspear Zealot's first strike
// needs two spells cast this turn; the fixture casts one, so the condition is
// FALSE. Nothing is observable, so the requirement is a skip rather than an
// item that asserts a static gorge did not apply.
func TestStaticContinuousSelfConditionFalse(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Brightspear Zealot")
	if !ok {
		t.Fatal("Brightspear Zealot not in the corpus")
	}
	served := 0
	for _, r := range levelb.Requirements(c) {
		if r.Sub != "static.continuous" {
			continue
		}
		served++
		if _, skip := templates.GenerateB(reg, "Brightspear Zealot", r); skip == nil || !strings.Contains(skip.Reason, "not observable") {
			t.Fatalf("%s: skip = %v, want the not-observable skip", r.Key, skip)
		}
	}
	if served == 0 {
		t.Fatal("precondition: Brightspear Zealot has no static.continuous requirement")
	}
}

// TestStaticContinuousAffectsOpponent: Cryoshatter enchants the fixture's
// target, p1's probe, and its static lands on that opponent creature: p1's
// probe leaves 2/2 for -3/2 while p0's is untouched.
func TestStaticContinuousAffectsOpponent(t *testing.T) {
	_, frozen := continuousItem(t, "Cryoshatter", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents-"), "c1 o1 Grizzly Bears ", " 2/2"); !ok {
		t.Fatalf("permanents- lacks p1's 2/2 probe: %v", lastLines(t, frozen, "permanents-"))
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c1 o1 Grizzly Bears ", " -3/2"); !ok {
		t.Fatalf("permanents+ lacks p1's -3/2 probe: %v", lastLines(t, frozen, "permanents+"))
	}
	if _, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Grizzly Bears ", ""); ok {
		t.Fatalf("p0's probe changed under an enchant-opponent static: %v", lastLines(t, frozen, "permanents+"))
	}
}
