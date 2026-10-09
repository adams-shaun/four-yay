package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// grantedActivatedRows are the appendix rows a granted non-mana activated
// ability observation serves (levelb-static-granted-abilities): the recipient
// probe (or the source's host, for an Aura/Equipment grant) is offered the
// activation, labelled with the ability's rule text.
var grantedActivatedRows = []struct{ card, key, label string }{
	{"Food Fight", "static#0.0", "deals damage to any target"},
	{"Trusty Boomerang", "static#0.0", "Tap target creature"},
	{"Ringing Strike Mastery", "static#0.0", "Untap this creature"},
	{"Friendly Neighborhood", "static#0.0", "+1/+1 until end of turn"},
}

// TestStaticGrantedActivatedAbilityIsOfferedOnlyWithTheSource serves a
// granted non-mana activated ability as an offered option on its recipient
// probe: the recipient really carries the grant (it is on the battlefield or
// attached to the source), gorge accepts the activation with the source
// present, and the same checkpoint without the source does not offer it, so
// the assertion is the static's doing.
func TestStaticGrantedActivatedAbilityIsOfferedOnlyWithTheSource(t *testing.T) {
	for _, r := range grantedActivatedRows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it, skip := zoneItem(t, r.card, r.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s (the granted-activated-ability observation regressed to a skip)", skip.Reason)
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
			if res := runZone(t, it); len(res.Fails) != 0 {
				t.Fatalf("with the source: %v", res.Fails)
			}
			// The control removes the source AND every step that references it
			// (its cast, its resolve, an Aura/Equipment attach), so the only
			// failure left is the missing offer -- not a harness error on a
			// dangling source ref.
			ctl := withoutSource(it, r.card, true)
			ctl.Steps = slices.DeleteFunc(slices.Clone(ctl.Steps), func(s oraclegen.Step) bool {
				return s.Card == "p0:"+r.card
			})
			if res := runZone(t, ctl); len(res.Fails) == 0 {
				t.Fatalf("without the source %s %s is still offered: the assertion is not the static's", got.Kind, got.Card)
			} else if !strings.Contains(res.Fails[0], "offered=false") {
				t.Fatalf("control failed for a reason other than the missing offer: %v", res.Fails)
			}
		})
	}
}
