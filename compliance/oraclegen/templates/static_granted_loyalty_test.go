package templates_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// grantedLoyaltyRows are the granted loyalty-ability rows
// (levelb-static-granted-abilities) this ticket serves: a planeswalker probe
// is offered the granted activation, labelled with the ability's text. The
// Avatar's [-10] and Kiora's [-8] are beyond every probe's printed loyalty, so
// the scenario puts the difference on the probe as LOYALTY counters; the
// Pyromancer's "[+1]: Add {R}." is a loyalty MANA ability, which CR 605.1a
// keeps out of the mana-ability class, so it is offered as an ordinary ability
// whose label carries its text.
var grantedLoyaltyRows = []struct {
	card, key, label string
	// cost is the granted ability's loyalty cost; > 0 means the probe needs
	// setup LOYALTY counters to pay it.
	cost int
}{
	{"Avatar of Burgeoning Echoes", "static#0.0", "+1/+1 counter on target creature for each land you control", 10},
	{"Kiora of Salt and Sand", "static#0.0", "8/8 blue Leviathan creature token with hexproof", 8},
	{"Way of the Pyromancer", "static#0.0", "Add {R}", 0},
}

// TestStaticGrantedLoyaltyAbilityIsOfferedOnlyWithTheSource locks both rows:
// the probe really carries the loyalty the cost needs (printed loyalty plus
// setup counters), gorge accepts the activation with the source present, and
// the same checkpoint without the source does not offer it, so the assertion
// is the static's doing.
func TestStaticGrantedLoyaltyAbilityIsOfferedOnlyWithTheSource(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, r := range grantedLoyaltyRows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it, skip := zoneItem(t, r.card, r.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s (the granted-loyalty observation regressed to a skip)", skip.Reason)
			}
			if want := r.card + "/" + r.key + "/v1"; it.ID != want {
				t.Fatalf("identity = %q, want the level-B row %q", it.ID, want)
			}
			last := it.Steps[len(it.Steps)-1]
			if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
				t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
			}
			got := last.Expect[0].Offered
			if got.Kind != "activate" || !strings.Contains(strings.ToLower(got.Label), strings.ToLower(r.label)) {
				t.Fatalf("offered assertion = %+v, want kind activate label containing %q", got, r.label)
			}
			probe := strings.TrimPrefix(got.Card, "p0:")
			p0 := it.Setup["p0"]
			if !slices.Contains(p0.Battlefield, probe) {
				t.Fatalf("precondition: probe %q is not on p0's battlefield %v", probe, p0.Battlefield)
			}
			// The probe's printed loyalty plus the setup counters must reach
			// the granted cost, and for the [-10] the printed loyalty alone
			// must NOT: otherwise the counter fixture is vacuous.
			c, ok := reg.Lookup(probe)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: probe %q not in the corpus", probe)
			}
			printed, err := strconv.Atoi(c.Faces[0].Loyalty)
			if err != nil {
				t.Fatalf("precondition: probe %q prints loyalty %q, want a number", probe, c.Faces[0].Loyalty)
			}
			added := p0.Counters[probe]["LOYALTY"]
			if int(printed)+int(added) < r.cost {
				t.Fatalf("precondition: probe %s loyalty %d + counters %d < granted cost %d", probe, printed, added, r.cost)
			}
			if r.cost > 0 && printed >= r.cost {
				t.Fatalf("precondition: probe %s prints %d loyalty >= cost %d, so the counters are not what makes it payable", probe, printed, r.cost)
			}
			if res := runZone(t, it); len(res.Fails) != 0 {
				t.Fatalf("with the source: %v", res.Fails)
			}
			ctl := withoutSource(it, r.card, true)
			if res := runZone(t, ctl); len(res.Fails) == 0 {
				t.Fatalf("without the source %s %s is still offered: the assertion is not the static's", got.Kind, got.Card)
			} else if !strings.Contains(res.Fails[0], "offered=false") {
				t.Fatalf("control failed for a reason other than the missing offer: %v", res.Fails)
			}
		})
	}
}
