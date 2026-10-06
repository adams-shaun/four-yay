package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestStaticMayPlayIsObservedAsOffered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key, zone, probe string
	}{
		{"Festival of Embers", "static#0.0", "graveyard", "Shock"},
		{"Vizier of the Menagerie", "static#0.1", "library_top", "Llanowar Elves"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("precondition: %s absent from corpus", tc.card)
			}
			for _, req := range levelb.Requirements(c) {
				if req.Key != tc.key {
					continue
				}
				if req.Sub != "static.continuous" {
					t.Fatalf("precondition: requirement = %s, want static.continuous", req.Sub)
				}
				it, skip := templates.GenerateB(reg, tc.card, req)
				if skip != nil {
					t.Fatalf("GenerateB: %s", skip.Reason)
				}
				seat := it.Setup["p0"]
				var zone []string
				switch tc.zone {
				case "graveyard":
					zone = seat.Graveyard
				case "library_top":
					zone = seat.LibraryTop
				}
				if !contains(zone, tc.probe) {
					t.Fatalf("precondition: %s absent from p0 %s: %v", tc.probe, tc.zone, zone)
				}
				last := it.Steps[len(it.Steps)-1]
				if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
					t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
				}
				if last.Expect[0].Offered.Kind != "cast" || last.Expect[0].Offered.Card != "p0:"+tc.probe {
					t.Fatalf("offered assertion = %+v", last.Expect[0].Offered)
				}
				return
			}
			t.Fatalf("precondition: %s has no requirement %s", tc.card, tc.key)
		})
	}
}
