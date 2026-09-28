package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestSproutingGoblinKickedETBSearchesBasicLandTypedLand pins Forge's
// Card.hasABasicLandType predicate end to end on its corpus carrier (task
// hasbasiclandtype). Sprouting Goblin's kicked ETB is
// `ChangeZone Origin$ Library | Destination$ Hand |
//
//	ChangeType$ Land.hasABasicLandType`
//
// -- "search your library for a land card with a basic land type, reveal it,
// put it into your hand, then shuffle". Before the predicate was read, the
// filter matched nothing on every run (a legitimate-looking fail-to-find) so
// the ability silently did nothing.
//
// The test proves both directions:
//
//   - kicked: the search ask is posed and offers the basic-typed lands
//     (Forest, Mountain) while excluding a Wasteland (a Land with NO basic
//     land type -- CR 205.3i), and the answered pick moves to hand.
//   - unkicked: the trigger never fires, so no search ask is posed.
func TestSproutingGoblinKickedETBSearchesBasicLandTypedLand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Sprouting Goblin", "Wasteland")
	goblin := searchMoveByName(t, e, "Sprouting Goblin", state.ZHand)
	waste := searchMoveByName(t, e, "Wasteland", state.ZLibrary)

	// Precondition for the negative below: Wasteland IS a land, so its
	// exclusion can only come from the basic-land-type clause, not from it
	// being absent from the library.
	if e.G.Obj(waste).Zone != state.ZLibrary {
		t.Fatalf("precondition: Wasteland zone = %s, want library", e.G.Obj(waste).Zone)
	}

	// Base {1}{R} + kicker {G}.
	addMana(t, e, 0, "GRC")
	submitChoices(t, e, castModeOption(t, e, goblin, "kicked"))

	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose for the kicked ETB", d)
	}
	if len(d.Options) == 0 {
		t.Fatal("search offered no options; Land.hasABasicLandType matched nothing")
	}
	// Every offered option must be a land with a basic land type, and the
	// Wasteland must not be among them. Gather the ids once so the
	// assertions cannot disagree with the answer below.
	sawBasic, sawWaste := false, false
	for _, o := range d.Options {
		obj := e.G.Obj(o.Obj)
		if obj == nil {
			t.Fatalf("search option %d has no object", o.Index)
		}
		if o.Obj == waste {
			sawWaste = true
		}
		sawBasicTyped := effects.MatchesSpec(e.G, "Land.hasABasicLandType", o.Obj, 0)
		if !sawBasicTyped {
			t.Fatalf("search offered %q, which has no basic land type", obj.Face().Name)
		}
		sawBasic = true
	}
	if sawWaste {
		t.Error("the search offered Wasteland, a Land with no basic land type (CR 205.3i)")
	}
	if !sawBasic {
		t.Fatal("search offered no basic-land-typed land")
	}

	picked := d.Options[0].Obj
	pickedName := e.G.Obj(picked).Face().Name
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Obj(picked).Zone; got != state.ZHand {
		t.Fatalf("searched %q destination = %s, want hand", pickedName, got)
	}
	replayCheck(t, e, cfg)
}

// TestSproutingGoblinUnkickedETBSearchesNothing is the negative half: a plain
// (unkicked) cast's ValidCard$ Card.Self+kicked never matches, so the ETB
// trigger does not fire and no search ask is ever posed. This is the
// fail-without-the-fix's counterpart: it pins that the trigger itself is
// gated on kicked and not merely on entering.
func TestSproutingGoblinUnkickedETBSearchesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Sprouting Goblin", "Wasteland")
	goblin := searchMoveByName(t, e, "Sprouting Goblin", state.ZHand)

	// Base {1}{R} only -- enough for the plain cast, no {G} for the kicker.
	addMana(t, e, 0, "RC")
	submitChoices(t, e, plainCastOption(t, e, goblin).Index)

	// The creature must have resolved to the battlefield (the ETB trigger's
	// source is where the rule looks), and no search ask may appear while
	// passing through the trigger window.
	if d := passUntilNonPriority(t, e, 40); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "search" {
		t.Fatalf("unkicked Sprouting Goblin posed a search ask: %+v", d)
	}
	if got := e.G.Obj(goblin).Zone; got != state.ZBattlefield {
		t.Fatalf("unkicked Sprouting Goblin zone = %s, want battlefield (precondition for the no-trigger assertion)", got)
	}
	// No land moved out of the library by a search.
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if id == goblin {
			t.Fatal("goblin still in library; the cast never resolved")
		}
	}
	replayCheck(t, e, cfg)
}
