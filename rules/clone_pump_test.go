package rules

// api:Clone's PumpKeywords$/PumpDuration$ riders (ticket
// api-clone-pump-riders). The two real corpus carriers are Loose in the Park
// (A:AB$ Clone | ... | PumpKeywords$ Haste | AddTypes$ Land | Duration$
// UntilEndOfTurn) and The Fourteenth Doctor (SVar:DBCopy:DB$ Clone | ...
// | PumpKeywords$ Haste | PumpDuration$ EOT). Both carry Haste, which the
// copied object in each test does NOT print, so the grant is observable and
// the assertion cannot pass vacuously.
//
// The fixtures below are inline Forge scripts in the real carrier shape (the
// clone_api_test.go convention), because driving either real card end to end
// needs that card's own draft/exile (Loose in the Park) or ETB-replacement and
// graveyard (The Fourteenth Doctor) machinery -- all of which the clone tests
// above already cover; this file pins only the rider pair.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// noClonePumpNote fails if the Clone body emitted its loud unimplemented-API
// fallback or an unread-parameter Note naming PumpKeywords$/PumpDuration$.
// A "the keyword is present" assertion alone would pass with the whole rider
// unregistered only if some other primitive granted Haste, so this pairs with
// it: it proves effClone's own registration ran.
func noClonePumpNote(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		if strings.Contains(ev.Text, "unimplemented API Clone") {
			t.Fatalf("Clone body fell back to unimplemented: %q", ev.Text)
		}
		if strings.Contains(ev.Text, "PumpKeywords$") || strings.Contains(ev.Text, "PumpDuration$") {
			t.Fatalf("PumpKeywords$/PumpDuration$ named by a skip note: %q", ev.Text)
		}
	}
}

// TestClonePumpKeywordsRideTheCopyLifetime is the Loose in the Park shape: an
// UntilEndOfTurn clone whose PumpKeywords$ has NO PumpDuration$, so the grant
// lasts as long as the copy and both expire together at this turn's cleanup.
func TestClonePumpKeywordsRideTheCopyLifetime(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Park Mimic\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | Duration$ UntilEndOfTurn | AddTypes$ Land | PumpKeywords$ Haste | SpellDescription$ becomes a copy until end of turn and gains haste.\n" +
		"Oracle:x\n"
	const oxSrc = "Name:Fixture Plain Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 93, src, oxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxSrc, state.ZBattlefield)

	// PRECONDITION: the copied Ox does not print Haste, so any Haste on the
	// copy below comes only from PumpKeywords$.
	if e.HasKeyword(ox, "Haste") {
		t.Fatal("fixture Ox already has Haste; the grant assertion would be vacuous")
	}
	if e.HasKeyword(id, "Haste") {
		t.Fatal("the pre-copy artifact already has Haste")
	}

	addMana(t, e, 0, "C")
	activateCloneAbility(t, e, id, ox)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Plain Ox" {
		t.Fatalf("copy name %v, want Fixture Plain Ox", o.Face())
	}
	if !e.HasKeyword(id, "Haste") {
		t.Fatalf("copy keywords %v, want the PumpKeywords$ Haste", e.Derived(id).Keywords)
	}
	// The copied face itself must NOT carry Haste -- otherwise the assertion
	// above would pass on the copy alone and prove nothing about the rider.
	if f := e.G.Obj(id).Face(); f.HasKeyword("Haste") {
		t.Fatal("the copied face prints Haste; PumpKeywords$ is not what granted it")
	}
	noClonePumpNote(t, e)
	replayCheck(t, e, cfg)

	// Cleanup: the UntilEndOfTurn copy and its PumpKeywords$ grant both go.
	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Park Mimic" {
		t.Fatalf("after cleanup the object is %v, want Fixture Park Mimic", o.Face())
	}
	if e.HasKeyword(id, "Haste") {
		t.Fatalf("reverted object kept the clone's PumpKeywords$ Haste: %v", e.Derived(id).Keywords)
	}
}

// TestClonePumpDurationEOtExpiresWhileThePermanentCopySurvives is the The
// Fourteenth Doctor shape: the clone itself is PERMANENT and the
// PumpKeywords$ grant is EOT, so at cleanup the keyword must go while the copy
// stays -- proving the two durations (Clone's Duration$ and PumpDuration$) are
// independent, which is the bug the rider's absence produced.
func TestClonePumpDurationEOtExpiresWhileThePermanentCopySurvives(t *testing.T) {
	t.Parallel()
	const src = "Name:Fixture Doctor Mimic\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | PumpKeywords$ Haste | PumpDuration$ EOT | SpellDescription$ becomes a permanent copy and gains haste until end of turn.\n" +
		"Oracle:x\n"
	const oxSrc = "Name:Fixture Plain Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/3\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 94, src, oxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, oxSrc, state.ZBattlefield)

	// PRECONDITION: neither the source nor the pre-copy object prints Haste.
	if e.HasKeyword(ox, "Haste") || e.HasKeyword(id, "Haste") {
		t.Fatal("fixture carries Haste before the copy; the grant assertion would be vacuous")
	}

	addMana(t, e, 0, "C")
	activateCloneAbility(t, e, id, ox)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Plain Ox" {
		t.Fatalf("copy name %v, want Fixture Plain Ox", o.Face())
	}
	if !e.HasKeyword(id, "Haste") {
		t.Fatalf("copy keywords %v, want the PumpKeywords$ Haste", e.Derived(id).Keywords)
	}
	noClonePumpNote(t, e)
	replayCheck(t, e, cfg)

	// Cleanup: the EOT keyword grant expires, but the permanent copy remains
	// (the copy has no Duration$, so its marker is not UntilEOT).
	e.pending = nil
	e.setStep(state.StepCleanup)
	e.priorityRound()
	if e.HasKeyword(id, "Haste") {
		t.Fatalf("PumpDuration$ EOT grant survived cleanup: %v", e.Derived(id).Keywords)
	}
	if o := e.G.Obj(id); o.Face() == nil || o.Face().Name != "Fixture Plain Ox" {
		t.Fatalf("the PERMANENT copy was dropped with the EOT keyword grant: %v", o.Face())
	}
}
