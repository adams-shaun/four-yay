package templates_test

import (
	"slices"
	"strings"
	"testing"
)

// TestStaticExileCardTypesFixtureServesKeenEyed is the observed-path half of
// the exiled-with fixture family: the source's own graveyard-to-exile ability
// exiles one card of each type, so the CardTypes count over
// Card.ExiledWithSource reaches its gate and the card's own P/T and keyword
// move (ticket agent-20261009T153027Z-bc3dacf9, level-B class G7).
func TestStaticExileCardTypesFixtureServesKeenEyed(t *testing.T) {
	s, served := stateFixtureServed(t, "Keen-Eyed Curator")
	if !served {
		t.Fatal("static#0.0 is skipped, want the exiled-with fixture to serve it")
	}
	// Precondition: the four activations really exiled their victims, one per
	// card type. Without them the gate is false and the grant below is the
	// proof the association matched.
	var exile []string
	for _, p := range s.Players {
		if p.Seat == 0 {
			exile = p.Exile
		}
	}
	if len(exile) != 4 {
		t.Fatalf("p0 exile = %v, want the four cards the activations exiled", exile)
	}
	var pt string
	var kw []string
	found := false
	for i := range s.Permanents {
		p := &s.Permanents[i]
		if p.Name == "Keen-Eyed Curator" && p.Controller == 0 {
			pt, kw, found = p.PT, p.Keywords, true
		}
	}
	if !found {
		t.Fatal("precondition: Keen-Eyed Curator is on p0's final battlefield")
	}
	if pt != "7/7" || !slices.Contains(kw, "Trample") {
		t.Fatalf("Keen-Eyed Curator %s %v, want 7/7 trample (printed 3/3 plus the four-type grant)", pt, kw)
	}
}

// TestStaticExileOfferRowServesIntrepid is the offer half: the source's own
// activation exiles a Dinosaur owned by p0, and the MayPlay permission over
// Creature.YouOwn+ExiledWithSource offers that card as a cast. The offered
// assertion is the proof the engine recorded the exiled-with association: a
// setup-placed exile never satisfies the filter.
func TestStaticExileOfferRowServesIntrepid(t *testing.T) {
	it, skip := zoneItem(t, "Intrepid Paleontologist", "static#0.0")
	if skip != nil {
		t.Fatalf("GenerateB: %s (the exiled-with offer regressed to a skip)", skip.Reason)
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
	}
	got := last.Expect[0].Offered
	if got.Kind != "cast" || !strings.Contains(got.Card, "Gigantosaurus") {
		t.Fatalf("offered assertion = %+v, want the Dinosaur probe as a cast", got)
	}
	victim := strings.TrimPrefix(got.Card, "p0:")
	// Precondition: the victim starts in p0's graveyard, so only the source's
	// own activation can move it (and stamp the association).
	if !slices.Contains(it.Setup["p0"].Graveyard, victim) {
		t.Fatalf("precondition: victim %q is not in p0's graveyard %v", victim, it.Setup["p0"].Graveyard)
	}
	res := runZone(t, it)
	if len(res.Fails) != 0 {
		t.Fatalf("gorge does not satisfy the scenario: %v", res.Fails)
	}
	// Precondition: the victim really reached exile. The offer can only be
	// there if the engine's ExiledWithSource filter matched it.
	final := res.Snapshots[len(res.Snapshots)-1]
	if !slices.Contains(final.Players[0].Exile, victim) {
		t.Fatalf("precondition: %s is not in p0's final exile %v", victim, final.Players[0].Exile)
	}
}

// TestStaticExileOfferRowServesDawnhand is the Blight/RaiseCost shape: the
// source's own {T}, Blight 2 activation exiles a creature owned by p0, and
// the MayPlay permission over Creature.YouOwn+ExiledWithSource (with its
// RemoveAnyCounter<3/Any/Creature> raise) offers it. The fixture blights a
// separate 6/4 body holding CHARGE counters, because CR 704.5q would remove a
// +1/+1 counter paired with the Blight's -1/-1 counter and eat the raise.
func TestStaticExileOfferRowServesDawnhand(t *testing.T) {
	it, skip := zoneItem(t, "Dawnhand Dissident", "static#0.0")
	if skip != nil {
		t.Fatalf("GenerateB: %s (the exiled-with offer regressed to a skip)", skip.Reason)
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
	}
	got := last.Expect[0].Offered
	if got.Kind != "cast" || !strings.Contains(got.Card, "Hill Giant") {
		t.Fatalf("offered assertion = %+v, want the exiled creature as a cast", got)
	}
	// Preconditions: the blight body really holds the raise's counters (on
	// the battlefield, not the probe), and the victim starts in the graveyard.
	p0 := it.Setup["p0"]
	if !slices.Contains(p0.Battlefield, "Craw Wurm") {
		t.Fatalf("precondition: the blight body is not on p0's battlefield %v", p0.Battlefield)
	}
	if p0.Counters["Craw Wurm"]["CHARGE"] != 3 {
		t.Fatalf("precondition: Craw Wurm holds %v, want three CHARGE counters for the raise", p0.Counters)
	}
	if !slices.Contains(p0.Graveyard, "Hill Giant") {
		t.Fatalf("precondition: the victim is not in p0's graveyard %v", p0.Graveyard)
	}
	res := runZone(t, it)
	if len(res.Fails) != 0 {
		t.Fatalf("gorge does not satisfy the scenario: %v", res.Fails)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if !slices.Contains(final.Players[0].Exile, "Hill Giant") {
		t.Fatalf("precondition: the victim is not in p0's final exile %v", final.Players[0].Exile)
	}
	// The blight body survived its -1/-1 counters (a 6/4 minus two).
	survived := false
	for _, p := range final.Permanents {
		if p.Name == "Craw Wurm" && p.Controller == 0 && p.PT == "4/2" {
			survived = true
		}
	}
	if !survived {
		t.Fatalf("precondition: Craw Wurm did not survive the blight at 4/2: %+v", final.Permanents)
	}
}

// TestStaticExileDonorRowServesTerritoryForge is the donor half: the source's
// cast ETB exiles an artifact donor, and the source is offered the donor's
// {T} activation labelled with the donor ability's rule text. The ETB is the
// only move that can stamp the association the GainsAbilitiesOf filter reads.
func TestStaticExileDonorRowServesTerritoryForge(t *testing.T) {
	it, skip := zoneItem(t, "Territory Forge", "static#0.0")
	if skip != nil {
		t.Fatalf("GenerateB: %s (the exiled-with donor regressed to a skip)", skip.Reason)
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
	}
	got := last.Expect[0].Offered
	if got.Kind != "activate" || !strings.Contains(got.Label, "You gain 1 life") {
		t.Fatalf("offered assertion = %+v, want the donor's activation labelled with its rule text", got)
	}
	// Precondition: the donor is placed on the battlefield for the ETB to
	// exile -- never in exile by setup, which carries no association.
	if !slices.Contains(it.Setup["p0"].Battlefield, "Braidwood Cup") {
		t.Fatalf("precondition: donor is not on p0's battlefield %v", it.Setup["p0"].Battlefield)
	}
	res := runZone(t, it)
	if len(res.Fails) != 0 {
		t.Fatalf("gorge does not satisfy the scenario: %v", res.Fails)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if !slices.Contains(final.Players[0].Exile, "Braidwood Cup") {
		t.Fatalf("precondition: the donor is not in p0's final exile %v", final.Players[0].Exile)
	}
}

// TestStaticExileUnservedRowsKeepTheNamedGap locks the measured engine gaps
// the family cannot serve: each row must keep a precise named reason, never
// the generic "counts cards exiled with the source" the family replaced.
func TestStaticExileUnservedRowsKeepTheNamedGap(t *testing.T) {
	for _, tc := range []struct{ card, key, want string }{
		{"Valgavoth, Terror Eater", "static#0.0", "static a MayPlayAltManaCost$ card in exile is not offered (the engine delivers the alternative cost only on a hand cast)"},
		{"Veteran Survivor", "static#0.0", "static the ExiledWith$Amount count reads only the reverse exiled-with stamp (engine gap)"},
		{"Maralen, Fae Ascendant", "static#0.0", "static cards exiled by the source's Dig carry no exiled-with association (engine gap)"},
		{"The Enigma Jewel", "static#1.0", "static needs the craft activation (the driver has no craft op)"},
		{"Azula, Cunning Usurper", "static#0.0", "static an opponent-owned exiled card is not offered (the engine's may-play walk covers only the caster's own cards)"},
		// Null Summoner is the same class outside the ticket's nine rows: the
		// opponent-owned shape gets the same named reason.
		{"Null Summoner", "static#0.0", "static an opponent-owned exiled card is not offered (the engine's may-play walk covers only the caster's own cards)"},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			it, skip := zoneItem(t, tc.card, tc.key)
			if skip == nil {
				t.Fatalf("served (%s), want the named engine gap", it.ID)
			}
			if skip.Reason != tc.want {
				t.Fatalf("skip = %q, want %q", skip.Reason, tc.want)
			}
		})
	}
}
