package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// wonderscapeSageBoard places Wonderscape Sage and the named land on the
// battlefield under seat 0, clears the Sage's summoning sickness so its
// activated {T} ability is offered, and returns both object ids. The deck
// fixture is built from real corpus cards only.
func wonderscapeSageBoard(t *testing.T, land string) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Wonderscape Sage", land)
	sage := searchMoveByName(t, e, "Wonderscape Sage", state.ZBattlefield)
	soil := searchMoveByName(t, e, land, state.ZBattlefield)
	// CR 302.6: a creature that entered this turn cannot pay a {T} cost
	// without haste. Clear the marker replay-honestly by beginning seat 0's
	// next turn -- events.Apply's TurnChange clears SummonSick for the
	// active player's battlefield (the same fold a real turn boundary runs),
	// so the log-only replay reproduces it. The card ability under test, not
	// the summoning-sickness gate, is what this test exercises; driving a
	// real untap/upkeep/draw turn would add unrelated events to the window
	// the ConditionDefined$ Returned group scans.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.pending = nil
	e.priorityRound()
	return e, cfg, sage, soil
}

// wonderscapeSageActivate activates the Sage, answers the Return cost ask with
// the named land, and returns the first non-priority decision that follows --
// the DBDiscard rider for a returned Forest, or the cleanup discard for a
// returned Desert. It fails loudly if the Return ask never appears, so a
// regression in the Return<N/Spec> cost path cannot make the test vacuous.
func wonderscapeSageActivate(t *testing.T, e *Engine, sage, soil state.ObjID) *decision.Decision {
	t.Helper()
	activateAbility(t, e, sage)
	d := passUntilNonPriority(t, e, 20)
	if d == nil {
		t.Fatal("no decision after activating Wonderscape Sage")
	}
	found := false
	for _, o := range d.Options {
		if o.Kind == "returncost" && o.Obj == soil {
			submitChoices(t, e, o.Index)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the Return<1/Land> cost ask did not offer the land %d: %+v", soil, d)
	}
	// The land really returned to its owner's hand; without this precondition
	// the rider's suppression could be asserted against a cost that never
	// paid.
	if got := e.G.Obj(soil).Zone; got != state.ZHand {
		t.Fatalf("precondition: returned land zone = %s, want hand", got)
	}
	return passUntilNonPriority(t, e, 20)
}

// TestWonderscapeSageNonbasicLandTypeSuppressesDiscard pins Forge's
// Card.hasANonBasicLandType predicate end to end on its one corpus carrier
// (task hasanonbasiclandtype). Wonderscape Sage's activated ability returns a
// land as a cost and draws, then
//
//	DB$ Discard | ConditionDefined$ Returned |
//	ConditionPresent$ Land.hasANonBasicLandType |
//	ConditionCompare$ EQ0 | Defined$ You | NumCards$ 1 | Mode$ TgtChoose
//
// — "discard a card unless that land had a nonbasic land type". The gate is
// the conjunction of two mechanisms this test proves together:
//
//   - ConditionDefined$ Returned must enumerate the land the activation's own
//     Return cost returned (otherwise the gate is unresolved and runs the sub
//     unconditionally, so BOTH cases discard);
//   - Land.hasANonBasicLandType must read CR 205.3i's nonbasic land types
//     (otherwise a returned Desert would still discard).
//
// Desert is a Land Desert: a nonbasic land type, so EQ0 is false and the
// discard is suppressed.
func TestWonderscapeSageNonbasicLandTypeSuppressesDiscard(t *testing.T) {
	t.Parallel()
	e, cfg, sage, desert := wonderscapeSageBoard(t, "Desert")
	d := wonderscapeSageActivate(t, e, sage, desert)
	if d == nil {
		t.Fatal("no decision after the Sage's ability resolved")
	}
	// The rider, if it fired, is the resolving Sage's own sub-ability
	// (Decision.Source == the Sage) and its prompt chooses cards to discard.
	// Cleanup down to hand size has Source == 0. Assert the Sage's rider did
	// NOT fire: a returned Desert has a nonbasic land type.
	if d.Source == sage {
		t.Fatalf("a returned Desert must suppress the discard rider; got the Sage's own ask %q", d.Prompt)
	}
	if strings.Contains(d.Prompt, "card(s) to discard") && !strings.Contains(d.Prompt, "hand-size limit") {
		t.Fatalf("a returned Desert must suppress the discard rider; got %q", d.Prompt)
	}
	replayCheck(t, e, cfg)
}

// TestWonderscapeSageBasicLandTypeKeepsDiscard is the positive half: a
// returned Forest has only a basic land type, so Land.hasANonBasicLandType is
// false, EQ0 is true, and the discard rider fires. This is the assertion that
// fails if the predicate were implemented as an always-true or as a
// `Land.nonBasic` supertype test -- but it also fails if ConditionDefined$
// Returned stopped resolving, because then the group is empty for BOTH lands
// and the Desert test above would have caught that.
func TestWonderscapeSageBasicLandTypeKeepsDiscard(t *testing.T) {
	t.Parallel()
	e, cfg, sage, forest := wonderscapeSageBoard(t, "Forest")
	d := wonderscapeSageActivate(t, e, sage, forest)
	if d == nil {
		t.Fatal("no decision after the Sage's ability resolved")
	}
	if d.Source != sage {
		t.Fatalf("a returned Forest must fire the Sage's discard rider; got %q (source %d)", d.Prompt, d.Source)
	}
	if !strings.Contains(d.Prompt, "card(s) to discard") {
		t.Fatalf("the rider ask should choose cards to discard; got %q", d.Prompt)
	}
	// Precondition: the Forest really carries only a basic land type, so the
	// rider fired for the reason this test claims.
	if o := e.G.Obj(forest); o == nil || o.Face() == nil || !slices.Contains(o.Face().Types, "Forest") {
		t.Fatalf("precondition: returned Forest lost its basic land type: %+v", e.G.Obj(forest))
	}
	submitChoices(t, e, d.Options[0].Index)
	replayCheck(t, e, cfg)
}
