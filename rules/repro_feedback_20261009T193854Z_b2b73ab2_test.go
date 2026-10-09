// Reproduces feedback report 20261009T193854Z-b2b73ab2 ("treasure token isn't
// being used to calculate 'autopay' -- Brazen Scourge should be an available
// action"). The snapshot under testdata/feedback/20261009T193854Z-b2b73ab2/
// replays 487/487 recorded intents to seat 0's turn-20 main1 priority.
//
// The report's premise is measured false: the offer walk DOES price the
// Treasure token. Cathar Commando's {1}{W} is offered below, and the only
// white source on the board is the Treasure's sacrifice ability; Brazen
// Scourge's {1}{R}{R} is three mana against one untapped Mountain plus one
// Treasure, so its absence is correct. This pin freezes both halves, so a
// payment-walk regression cannot silently start offering an unpayable cast
// or drop sacrifice-cost mana from the offer walk.
//
// This is the target package's EXTERNAL test package: feedback imports the
// engine tier, so an internal-package test would form an import cycle.
package rules_test

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
	"github.com/adams-shaun/gorge/state"
)

// The report's two hand cards and the board's two mana sources, as captured.
const (
	brazenScourgeObj  = state.ObjID(3)  // {1}{R}{R} 3/3 haste
	catharCommandoObj = state.ObjID(4)  // {1}{W} 3/1 flash
	mountainObj       = state.ObjID(28) // untapped, taps for {R}
	treasureObj       = state.ObjID(97) // untapped, sacrifice for one mana of any colour
)

func TestFeedbackRepro20261009T193854Z_b2b73ab2(t *testing.T) {
	e := feedback.EngineAt(t, filepath.Join("testdata", "feedback", "20261009T193854Z-b2b73ab2"), -1)

	// Precondition: the report point is seat 0's main-phase priority window.
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("pending = %#v, want seat 0's priority at the report point", d)
	}

	// Precondition: the two hand cards are the objects the report names, and
	// the compiled costs are what the arithmetic below uses.
	scourge := e.G.Obj(brazenScourgeObj)
	if scourge == nil || scourge.Zone != state.ZHand || scourge.Face() == nil || scourge.Face().Name != "Brazen Scourge" {
		t.Fatalf("obj %d = %#v, want Brazen Scourge in seat 0's hand", brazenScourgeObj, scourge)
	}
	if got := scourge.Face().ManaCost; got != "1 R R" {
		t.Fatalf("Brazen Scourge mana cost = %q, want %q ({1}{R}{R}, three mana)", got, "1 R R")
	}
	commando := e.G.Obj(catharCommandoObj)
	if commando == nil || commando.Zone != state.ZHand || commando.Face() == nil || commando.Face().Name != "Cathar Commando" {
		t.Fatalf("obj %d = %#v, want Cathar Commando in seat 0's hand", catharCommandoObj, commando)
	}
	if got := commando.Face().ManaCost; got != "1 W" {
		t.Fatalf("Cathar Commando mana cost = %q, want %q ({1}{W})", got, "1 W")
	}

	// Precondition: the board's mana is exactly one untapped Mountain and one
	// untapped Treasure. The other permanents either cannot produce mana
	// (Goldvein Pick, Prideful Parent, Cat Token) or are tapped (three Plains,
	// Healer's Hawk), so two mana exist and {1}{R}{R}'s three is unpayable.
	mtn := e.G.Obj(mountainObj)
	if mtn == nil || mtn.Zone != state.ZBattlefield || mtn.Tapped || mtn.Face() == nil || mtn.Face().Name != "Mountain" {
		t.Fatalf("obj %d = %#v, want an untapped Mountain on the battlefield", mountainObj, mtn)
	}
	treasure := e.G.Obj(treasureObj)
	if treasure == nil || treasure.Zone != state.ZBattlefield || treasure.Tapped || treasure.Face() == nil || treasure.Face().Name != "Treasure Token" {
		t.Fatalf("obj %d = %#v, want an untapped Treasure Token on the battlefield", treasureObj, treasure)
	}

	// The offer walk ran and counted the Treasure: Cathar Commando's {1}{W}
	// is offered, and the Mountain's R cannot pay a W symbol, so the only
	// source of the W is the Treasure's sacrifice ability.
	offers := e.PotentialActions(0)
	if len(offers) == 0 {
		t.Fatal("PotentialActions(0) is empty; the offer walk did not run")
	}
	commandoOffered := false
	for _, o := range offers {
		if o.Kind == "cast" && o.Obj == catharCommandoObj {
			commandoOffered = true
		}
	}
	if !commandoOffered {
		t.Fatalf("PotentialActions(0) does not offer cast of Cathar Commando (%d); its {1}{W} is payable only through the Treasure token, so the walk is not counting it: %+v", catharCommandoObj, offers)
	}

	// The reported "missing action" is correctly absent: {1}{R}{R} is three
	// mana and the seat has two, Treasure included.
	for _, o := range offers {
		if o.Kind == "cast" && o.Obj == brazenScourgeObj {
			t.Fatalf("PotentialActions(0) offers cast of Brazen Scourge (%d), but {1}{R}{R} cannot be paid from one Mountain plus one Treasure: %+v", brazenScourgeObj, offers)
		}
	}

	// The seat-box readout is deliberately narrower than the offer walk: it
	// counts only free-to-tap mana abilities, so the Treasure's
	// sacrifice-cost ability contributes nothing and the readout is {R:1}.
	if got, want := e.AvailableMana(0), (state.Mana{state.MR: 1}); got != want {
		t.Fatalf("AvailableMana(0) = %v, want %v (free tap sources only: the untapped Mountain)", got, want)
	}
}
