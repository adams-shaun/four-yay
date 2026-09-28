package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDoctorCompanionSeatsBothCommanders is the engine half of the Doctor Who
// cycle's deck-construction clause: one commander carrying K:Doctor's
// companion (real corpus Rose Tyler) and the other being a Doctor (real
// corpus The Tenth Doctor) is a legal pair, so the engine seats BOTH as seat
// 0's commanders in the command zone and each is castable from there. The
// companion half is deliberately NOT a Doctor itself (Rose Tyler is Human),
// so a seat that required the companion to carry the subtype would seat
// nothing.
//
// This is the deck-layer pair predicate read through the engine's own seating
// gate (rules/engine.go's partnerPairOK -> deck.IsPartnerPair), so it proves
// the two layers agree about a Doctor's-companion pair rather than testing
// the deck package alone.
func TestDoctorCompanionSeatsBothCommanders(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rose := lookup(t, reg, "Rose Tyler")
	doctor := lookup(t, reg, "The Tenth Doctor")
	if !partnerPairOK(rose, doctor) {
		t.Fatal("fixture drift: corpus Rose Tyler + The Tenth Doctor no longer read as a legal Doctor's-companion pair")
	}
	cfg := commanderConfig(t, reg, []*cards.Card{rose, doctor}, []int{0, 1})
	e := New(cfg)
	e.Advance()

	cmds := e.G.Players[0].Commanders
	if len(cmds) != 2 {
		t.Fatalf("seat 0 has %d commanders in the command zone, want 2 (the pair)", len(cmds))
	}
	for i, id := range cmds {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZCommand {
			t.Fatalf("commander %d is not in the command zone (obj %v)", i, o)
		}
	}
	if e.G.Players[0].Life != 40 {
		t.Fatalf("commander starting life is %d, want 40", e.G.Players[0].Life)
	}

	// Both halves are castable from the command zone: Rose Tyler is {1}{W},
	// The Tenth Doctor is {3}{U}{R}. The seating gate running is the
	// precondition the offer depends on, so assert it did not reject the
	// configuration (a rejection would leave the command zone empty above,
	// but name it explicitly so a future fail-open regression is loud).
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "commander configuration rejected under CR 903") {
			t.Fatalf("the Doctor's-companion pair was rejected at seating: %q", ev.Text)
		}
	}
	toMain1(t, e)
	addMana(t, e, 0, "WW")
	addMana(t, e, 0, "UUURR")
	d := e.Pending()
	roseCast, doctorCast := false, false
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast Rose Tyler" {
			roseCast = true
		}
		if o.Kind == "cast" && o.Label == "Cast The Tenth Doctor" {
			doctorCast = true
		}
	}
	if !roseCast || !doctorCast {
		t.Fatalf("both commanders must be offered as command-zone casts (rose=%v doctor=%v): %+v", roseCast, doctorCast, d.Options)
	}
}

// TestTwoDoctorCompanionsSeatBothCommanders is the engine half of the
// clause's bidirectional reading: two DISTINCT Doctors that EACH carry
// K:Doctor's companion are a legal pair, because for each card the other
// commander is the Doctor. The pre-fix predicate required exactly one
// companion half and rejected this shape. No real corpus card is a Doctor
// that carries the keyword (measured: all 27 Doctor's-companion cards are
// Humans/Robots/Time Lords without the Doctor subtype), so the cards are
// authored here rather than drawn from the corpus; the pair predicate under
// test (partnerPairOK -> deck.IsPartnerPair) is the production one either way.
func TestTwoDoctorCompanionsSeatBothCommanders(t *testing.T) {
	t.Parallel()
	reg := cards.NewRegistry()
	fifth := card(t, "Name:The Fifth Doctor\nManaCost:W\nTypes:Legendary Creature Time Lord Doctor\nK:Doctor's companion\n")
	sixth := card(t, "Name:The Sixth Doctor\nManaCost:U\nTypes:Legendary Creature Time Lord Doctor\nK:Doctor's companion\n")
	reg.Add(fifth)
	reg.Add(sixth)
	reg.Add(card(t, "Name:Mountain\nTypes:Basic Land Mountain\n"))
	// Precondition: both cards really are Doctors that carry the keyword, so
	// the pair is the shape under test rather than two ordinary partners.
	if !isDoctorCardForTest(fifth) || !hasCompanionForTest(fifth) ||
		!isDoctorCardForTest(sixth) || !hasCompanionForTest(sixth) {
		t.Fatal("fixture drift: the authored Doctors must each carry Doctor's companion and the Doctor subtype")
	}
	if !partnerPairOK(fifth, sixth) {
		t.Fatal("two Doctors each carrying Doctor's companion must be a legal pair")
	}

	cfg := commanderConfig(t, reg, []*cards.Card{fifth, sixth}, []int{0, 1})
	e := New(cfg)
	e.Advance()

	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "commander configuration rejected under CR 903") {
			t.Fatalf("the two-Doctor-companion pair was rejected at seating: %q", ev.Text)
		}
	}
	cmds := e.G.Players[0].Commanders
	if len(cmds) != 2 {
		t.Fatalf("seat 0 has %d commanders in the command zone, want 2 (the pair)", len(cmds))
	}
	for i, id := range cmds {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZCommand {
			t.Fatalf("commander %d is not in the command zone (obj %v)", i, o)
		}
	}
}

// isDoctorCardForTest / hasCompanionForTest read the deck package's predicate
// inputs through the same accessors production uses (front face only), so the
// precondition above cannot pass on a card the real pair check would read
// differently.
func isDoctorCardForTest(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	for _, ty := range c.Faces[0].Types {
		if strings.EqualFold(strings.TrimSpace(ty), "Doctor") {
			return true
		}
	}
	return false
}

func hasCompanionForTest(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	return c.Faces[0].HasKeyword("Doctor's companion")
}
