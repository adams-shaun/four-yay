package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// donorRows are the has-all-abilities-of rows (levelb-static-granted-abilities)
// the donor observation serves: a donor card matching the grant's filter is
// placed in the GainsAbilitiesOfZones$ zone, and the source is offered the
// donor ability's activation labelled with its rule text.
var donorRows = []struct{ card, key, zone, label string }{
	{"Marvin, Murderous Mimic", "static#0.0", "Battlefield", "You gain 1 life for each Elf on the battlefield"},
	{"Thranduil, the Elvenking", "static#0.0", "Graveyard", "You gain 1 life for each Elf on the battlefield"},
}

// donorWellwisher is the one donor donorProbeNames offers; the test names it
// so a change to the candidate list is a clear failure, not a silent skip.
const donorWellwisher = "Wellwisher"

// zoneCards returns p0's cards in the named zone.
func zoneCards(p0 oraclegen.Seat, zone string) []string {
	switch zone {
	case "Battlefield":
		return p0.Battlefield
	case "Graveyard":
		return p0.Graveyard
	case "Exile":
		return p0.Exile
	case "Hand":
		return p0.Hand
	case "Library":
		return p0.LibraryTop
	}
	return nil
}

// withoutDonor is the item with the donor removed from its zone: the control
// checkpoint that must NOT offer the granted ability.
func withoutDonor(it oraclegen.Item, donor, zone string) oraclegen.Item {
	out := it
	setup := make(map[string]oraclegen.Seat, len(it.Setup))
	for k, v := range it.Setup {
		setup[k] = v
	}
	p0 := setup["p0"]
	switch zone {
	case "Battlefield":
		p0.Battlefield = slices.DeleteFunc(slices.Clone(p0.Battlefield), func(n string) bool { return n == donor })
	case "Graveyard":
		p0.Graveyard = slices.DeleteFunc(slices.Clone(p0.Graveyard), func(n string) bool { return n == donor })
	case "Exile":
		p0.Exile = slices.DeleteFunc(slices.Clone(p0.Exile), func(n string) bool { return n == donor })
	case "Hand":
		p0.Hand = slices.DeleteFunc(slices.Clone(p0.Hand), func(n string) bool { return n == donor })
	case "Library":
		p0.LibraryTop = slices.DeleteFunc(slices.Clone(p0.LibraryTop), func(n string) bool { return n == donor })
	}
	setup["p0"] = p0
	out.Setup = setup
	return out
}

// TestStaticGainedAbilitiesOfDonorIsOfferedOnlyWithTheDonor serves the
// GainsAbilitiesOf$ rows: the source is on p0's battlefield, the donor card
// really sits in the grant's zone, gorge accepts the donor ability's
// activation on the source with the donor present, and the same checkpoint
// without the donor rejects it, so the assertion is the grant's doing.
func TestStaticGainedAbilitiesOfDonorIsOfferedOnlyWithTheDonor(t *testing.T) {
	for _, r := range donorRows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it, skip := zoneItem(t, r.card, r.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s (the donor observation regressed to a skip)", skip.Reason)
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
			p0 := it.Setup["p0"]
			if !slices.Contains(p0.Battlefield, r.card) {
				t.Fatalf("precondition: source %q is not on p0's battlefield %v", r.card, p0.Battlefield)
			}
			if !slices.Contains(zoneCards(p0, r.zone), donorWellwisher) {
				t.Fatalf("precondition: donor %q is not in p0's %s %v", donorWellwisher, r.zone, zoneCards(p0, r.zone))
			}
			if res := runZone(t, it); len(res.Fails) != 0 {
				t.Fatalf("with the donor: %v", res.Fails)
			}
			if res := runZone(t, withoutDonor(it, donorWellwisher, r.zone)); len(res.Fails) == 0 {
				t.Fatalf("without the donor %s %s is still offered: the assertion is not the grant's", got.Kind, got.Card)
			} else if !strings.Contains(res.Fails[0], "offered=false") {
				t.Fatalf("control failed for a reason other than the missing offer: %v", res.Fails)
			}
		})
	}
}

// TestStaticGainedAbilitiesOfUnservedRowsKeepTheNamedSkip: the donor
// observation must not claim a row whose filter or zone it cannot place a
// donor for. Koh's ChosenCard grant stays a named skip (it needs the card's
// own ChooseCard activation first), so the census still tells the shape apart.
func TestStaticGainedAbilitiesOfUnservedRowsKeepTheNamedSkip(t *testing.T) {
	it, skip := zoneItem(t, "Koh, the Face Stealer", "static#0.0")
	if skip == nil {
		t.Fatalf("served (%s), want the named donor skip", it.ID)
	}
	if want := "static gains the activated abilities of other cards (needs a donor card)"; skip.Reason != want {
		t.Fatalf("skip = %q, want %q", skip.Reason, want)
	}
}
