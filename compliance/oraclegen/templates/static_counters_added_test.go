package templates_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestStaticCountersAddedThisTurn: Beast, Erudite Aerialist's "as long as
// you've put one or more +1/+1 counters on Beast this turn, he has flying"
// (CheckSVar$ X, Count$CountersAddedThisTurn P1P1 You Card.Self) was
// unobservable because setup counters are placed before turn 1 and no counter
// is added after the card is in play. The fixture casts a +1/+1-counter
// instant on it after it resolves. The counter's own +1/+1 is the fixture's
// baseline, so flying is the only change read. The precondition asserts the
// counter really is on Beast, and the control replays the same scenario
// without the counter cast and finds no flying.
func TestStaticCountersAddedThisTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Beast, Erudite Aerialist"
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	if slices.Contains(c.Faces[0].Keywords, "Flying") {
		t.Fatalf("precondition: %s prints Flying", name)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	p, ok := permNamedSnap(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if p.Counters["P1P1"] != 1 {
		t.Fatalf("%s counters = %v, want one +1/+1 counter added this turn", name, p.Counters)
	}
	if !slices.Contains(p.Keywords, "Flying") {
		t.Fatalf("%s keywords = %v, want Flying granted by the counter this turn", name, p.Keywords)
	}
	// Control: stop after the card resolves (cast + resolve), before the
	// counter instant.
	sc := it.Scenario
	if len(sc.Steps) < 4 {
		t.Fatalf("scenario steps = %+v, want the card's cast/resolve then the counter's", sc.Steps)
	}
	sc.Steps = sc.Steps[:2]
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("control does not replay: err=%v fails=%v", err, res.Fails)
	}
	cp, ok := permNamedSnap(res.Snapshots[len(res.Snapshots)-1], name)
	if !ok {
		t.Fatalf("control: %s not on the battlefield", name)
	}
	if slices.Contains(cp.Keywords, "Flying") || cp.Counters["P1P1"] != 0 {
		t.Fatalf("control %s keywords=%v counters=%v, want the printed state", name, cp.Keywords, cp.Counters)
	}
}
