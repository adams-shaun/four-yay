package rules

// NonLegendary$ True on DB$ CopySpellAbility (The Sixth Doctor's Time Lord's
// Prerogative): "Whenever you cast a historic spell, copy it, except the copy
// isn't legendary." CopySpellAbility's copy previously kept the original's
// Legendary supertype, so a copied legendary PERMANENT spell resolved into a
// token the CR 704.5j legend rule gathered against its own original and binned
// one of the pair.
//
// The rider is carried on the StackCopy event's Counter (a field StackCopy
// otherwise leaves empty) and folded by events.Apply into Object.CopyNonLegendary
// -- the same event-sourced shape ClonePermanent's Counter riders take -- which
// rules' typeCharacteristics applies as a layer-4 base strip. No event field is
// added or reordered.
//
// The fixture carries the real corpus card's SpellCast trigger line and its
// TrigCopy SVar VERBATIM (.cards/cardsfolder/t/the_sixth_doctor.txt), on the
// real card. The 4-seat/constructed harness cannot easily pay {4}{G}{U}, so the
// card is moved onto the battlefield directly and a cheap legendary creature
// spell is cast under it.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// sixthDoctorSrc is .cards/cardsfolder/t/the_sixth_doctor.txt's trigger line
// and TrigCopy SVar, VERBATIM. The card's `ValidCard$ Card.Historic` is the
// real predicate; a legendary creature spell is historic (CR 205.4), so the
// trigger fires on it.
const sixthDoctorSrc = "Name:The Sixth Doctor\nManaCost:4 G U\nTypes:Legendary Creature Time Lord Doctor\nPT:3/3\n" +
	"T:Mode$ SpellCast | TriggerZones$ Battlefield | ValidCard$ Card.Historic | ValidActivatingPlayer$ You | ActivationLimit$ 1 | Execute$ TrigCopy | TriggerDescription$ Time Lord's Prerogative - Whenever you cast a historic spell, copy it, except the copy isn't legendary. This ability triggers only once each turn.\n" +
	"SVar:TrigCopy:DB$ CopySpellAbility | Defined$ TriggeredSpellAbility | NonLegendary$ True\n" +
	"Oracle:x\n"

// legendaryBearSrc is the historic legendary permanent spell the Sixth Doctor
// copies: a Legendary creature, so the original spell and its copy would be
// two same-named legendary permanents under one controller -- a CR 704.5j
// duplicate set unless the copy's Legendary supertype is stripped.
const legendaryBearSrc = "Name:Test Legendary Bear\nManaCost:G\nTypes:Legendary Creature Bear\nPT:2/2\n" +
	"Oracle:x\n"

// derivedHasType reports whether the object's DERIVED type list carries the
// given word -- the same list rules' legendaryUnderLayers reads, so an
// assertion here is an assertion about the legend rule's own input.
func derivedHasType(e *Engine, id state.ObjID, word string) bool {
	return slices.ContainsFunc(e.Derived(id).Types, func(t string) bool { return t == word })
}

// TestSixthDoctorCopyIsNonLegendary is the row's end-to-end leaf: the Sixth
// Doctor's SpellCast trigger copies the cast legendary creature spell, both
// permanents resolve, and -- because the copy is not legendary -- NO CR 704.5j
// duplicate set forms and both remain on the battlefield.
func TestSixthDoctorCopyIsNonLegendary(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 97, []string{sixthDoctorSrc, legendaryBearSrc}, nil)
	doc := moveSeeded(t, e, 0, sixthDoctorSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, legendaryBearSrc, state.ZHand)

	addMana(t, e, 0, "G")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 60)

	// Precondition: The Sixth Doctor's SpellCast trigger actually fired on the
	// legendary (historic) spell under it. A fail-closed Historic predicate
	// would leave this at 0 and the copy assertions below vacuous.
	if got := pushCount(e, doc); got != 1 {
		t.Fatalf("precondition failed: The Sixth Doctor's SpellCast trigger pushed %d times, want 1 (Card.Historic did not match the legendary spell)", got)
	}

	// Precondition: the trigger actually fired and started a copy. A silent
	// Historic predicate would leave only one permanent below, and the
	// duplicate-set assertion would be vacuous -- so demand two.
	var original, copy *state.Object
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield || o.Face() == nil || o.Face().Name != "Test Legendary Bear" {
			continue
		}
		switch {
		case !o.IsToken && !o.IsCopy:
			original = o
		case o.IsToken:
			copy = o
		}
	}
	if original == nil {
		t.Fatalf("precondition failed: the cast legendary spell is not on the battlefield as a plain permanent")
	}
	if copy == nil {
		t.Fatalf("precondition failed: the Sixth Doctor's copy did not resolve onto the battlefield as a token (the SpellCast copy never fired)")
	}
	if copy.ID == original.ID {
		t.Fatalf("copy and original share id %d", copy.ID)
	}
	// The compared values must actually differ in the right direction: the
	// original IS legendary, so a rule that gathered a legendary copy against
	// it has a real duplicate set to work on.
	if !derivedHasType(e, original.ID, "Legendary") {
		t.Fatalf("precondition failed: the original %q derives non-legendary types %v", original.Face().Name, e.Derived(original.ID).Types)
	}
	// The substantive assertion: the copy lost the Legendary supertype.
	if derivedHasType(e, copy.ID, "Legendary") {
		t.Errorf("copy obj id=%d derived types %v still carry Legendary; NonLegendary$ True was not applied", copy.ID, e.Derived(copy.ID).Types)
	}
	// Both permanents must survive the SBA pass the legend rule runs in.
	n := 0
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == "Test Legendary Bear" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("battlefield holds %d permanents named Test Legendary Bear, want both the original and the non-legendary copy", n)
	}
	if d := e.Pending(); d != nil {
		for _, opt := range d.Options {
			if opt.Kind == "keep" {
				t.Errorf("engine is parked on a CR 704.5j legend choice over %s -- the non-legendary copy must not form a duplicate set", d.Kind)
			}
		}
	}
	// Nothing else in the log may have moved either permanent off the
	// battlefield (the SBA pass runs between the two resolutions).
	for _, id := range []state.ObjID{original.ID, copy.ID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Errorf("obj %d left the battlefield (zone %v); the legend rule binned it", id, o.Zone)
		}
	}
	replayCheck(t, e, cfg)
}
