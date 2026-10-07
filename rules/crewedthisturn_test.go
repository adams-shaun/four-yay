package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCrewedThisTurnCensus is the MECHANISM-CLASS census for Forge's
// Creature.CrewedThisTurn / Card.CrewedThisTurn filter (CR 702.122, task
// crewedthisturn1). It scans the compiled corpus for every face whose specs
// name the token and pins the carrier set, so a body added to or removed from
// the corpus is caught here rather than silently changing what the filter must
// support. The engine's predicate (effects/filter.go's "CrewedThisTurn") is
// source-relative and crew-event-backed, not card-specific, so all six share
// one implementation:
//
//   - Turtle Van            -- "put a +1/+1 counter on target creature that
//     crewed it this turn" (the end-to-end carrier in
//     TestSetAudit_tmt_TurtleVan_CrewedThisTurnTarget)
//   - Getaway Car           -- "return up to one target creature that crewed
//     it this turn to its owner's hand"
//   - Golden Argosy         -- "exile each creature that crewed it this turn"
//     (a non-target ChangeType$ filter, proving the source binding reaches
//     the effect walk too)
//   - Leisure Bicycle       -- "target creature that crewed it this turn
//     explores"
//   - Smogbelcher Chariot   -- "target creature that crewed it this turn
//     perpetually gains ..." (spelled Card.CrewedThisTurn)
//   - Subterranean Schooner -- "target creature that crewed it this turn
//     explores"
func TestCrewedThisTurnCensus(t *testing.T) {
	const token = "CrewedThisTurn"
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("crew census corpus unavailable")
	}

	got := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if faceNamesToken(f, token) {
				got[f.Name] = true
			}
		}
	}
	// Precondition: the scan must find something, or a corpus/API change has
	// made this test vacuous (it would pass with the token entirely absent).
	if len(got) == 0 {
		t.Fatalf("census found no face naming %q: did the corpus cache change shape?", token)
	}

	want := []string{
		"Turtle Van",
		"Getaway Car",
		"Golden Argosy",
		"Leisure Bicycle",
		"Smogbelcher Chariot",
		"Subterranean Schooner",
	}
	var missing []string
	for _, name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	var extra []string
	for name := range got {
		found := false
		for _, w := range want {
			if name == w {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("Creature.CrewedThisTurn carriers drifted: missing %v, unexpected %v (corpus set = %v)",
			missing, extra, sortedCrewNames(got))
	}
}

// faceNamesToken reports whether any spec on the face names token: the SVar
// bodies (where ValidTgts$/ChangeType$ live for every current carrier), the
// root abilities and their Sub chains, the triggers, statics and replacements.
// Display text (Oracle/TriggerDescription) is deliberately NOT scanned -- it
// prints the phrase with spaces, never the filter token, so a scan of it could
// only produce false positives.
func faceNamesToken(f *cards.Face, token string) bool {
	for _, body := range f.SVars {
		if strings.Contains(body, token) {
			return true
		}
	}
	for _, sa := range f.Abilities {
		if saNamesToken(sa, token) {
			return true
		}
	}
	for _, tr := range f.Triggers {
		if paramsNameToken(tr.Params, token) || saNamesToken(tr.Effect, token) {
			return true
		}
	}
	for _, st := range f.Statics {
		if paramsNameToken(st.Params, token) {
			return true
		}
	}
	for _, rp := range f.Repls {
		if paramsNameToken(rp.Params, token) || saNamesToken(rp.With, token) {
			return true
		}
	}
	return false
}

// crewVehicle drives seat 0's priority window to the Vehicle's Crew ability
// and answers its tap election with crewerID, then drains the stack. The same
// flow TestSetAudit_tmt_TurtleVan_CrewedThisTurnTarget runs inline.
func crewVehicle(t *testing.T, e *Engine, vanID, crewerID state.ObjID) {
	t.Helper()
	if e.Pending() == nil {
		e.priorityRound()
	}
	opt := abilityFor(t, e, 0, vanID)
	if opt == nil {
		t.Fatalf("precondition: Turtle Van's Crew ability not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		chosen := -1
		for _, o := range d.Options {
			if o.Obj == crewerID {
				chosen = o.Index
			}
		}
		if chosen < 0 {
			break
		}
		submitChoices(t, e, chosen)
	}
	passUntilStackEmpty(t, e, 40)
}

func crewListHas(list []state.ObjID, id state.ObjID) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// TestCrewedThisTurnBlinkBreaksThePairing pins the CR 400.7 half of the crew
// pairing (review round 2): a Vehicle that leaves the battlefield and returns
// in the same turn is a NEW object, and the crew record keyed by its stable
// ObjID must not carry over -- the returning Van's attack trigger must not
// offer a creature that crewed its OLD stint. events.Apply's
// leaving-the-battlefield fold sweeps the departing permanent's id from every
// battlefield object's CrewedVehicles, so the test asserts the folded state
// directly AND end to end: after the blink, re-crewing with a different Bear
// offers exactly that Bear as the trigger's target, never the old crewer.
func TestCrewedThisTurnBlinkBreaksThePairing(t *testing.T) {
	van := setAuditRealCard(t, "Turtle Van")
	haste := card(t, setAuditHasteSrc)
	bear := card(t, ninjutsuBearSrc)
	e, cfg := setAuditDeck(t, 9493, []*cards.Card{van, haste, bear, bear, bear}, []*cards.Card{})
	vanID := searchMoveByName(t, e, "Turtle Van", state.ZBattlefield)
	if o := e.G.Obj(vanID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Turtle Van not on battlefield: %+v", o)
	}
	putCreature(t, e, 0, setAuditHasteSrc)
	crewerA := putCreature(t, e, 0, ninjutsuBearSrc)
	crewerB := putCreature(t, e, 0, ninjutsuBearSrc)
	for _, id := range []state.ObjID{crewerA, crewerB} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: crewer not on battlefield: %+v", o)
		}
	}

	crewVehicle(t, e, vanID, crewerA)
	// Precondition: the fold recorded the pairing on the crewer and animated
	// the Van (the assert both halves of the feature under test depend on).
	if oA := e.G.Obj(crewerA); oA == nil || oA.CrewedTurn != e.G.Turn || !crewListHas(oA.CrewedVehicles, vanID) {
		t.Fatalf("precondition: the crew pairing was not recorded on the crewer: %+v", oA)
	}
	if o := e.G.Obj(vanID); o == nil || !e.IsCreature(vanID) {
		t.Fatalf("precondition: Turtle Van did not become a creature after crewing: %+v", o)
	}

	// Blink the Van: exile it and return it in the same turn, via direct
	// MoveZone events (the shape action_statics_test.go drives its
	// leave/return with). CR 400.7: the returning Van is a new object.
	e.emit(events.Event{Kind: events.MoveZone, Obj: vanID, From: state.ZBattlefield, To: state.ZExile})
	if o := e.G.Obj(vanID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: the Van did not leave the battlefield: %+v", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: vanID, From: state.ZExile, To: state.ZBattlefield})
	if o := e.G.Obj(vanID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the Van did not return to the battlefield: %+v", o)
	}
	// Both the pairing and the self-scoped crew animation belong to the old
	// battlefield incarnation and must be gone after the blink.
	if e.IsCreature(vanID) {
		t.Fatal("CR 400.7: the returned Van retained its old crew animation")
	}
	// The sweep under test: the old stint's pairing must be gone from the
	// crewer.
	if oA := e.G.Obj(crewerA); oA == nil || crewListHas(oA.CrewedVehicles, vanID) {
		t.Fatalf("CR 400.7: the blink did not clear the old Vehicle's pairing from the crewer: %+v", oA)
	}

	// Crew it AGAIN, with a different Bear. Only the new pairing may match the
	// filter; the old crewer is still a creature on the battlefield, so a
	// stale pairing would widen the target list, not empty it.
	crewVehicle(t, e, vanID, crewerB)
	if oB := e.G.Obj(crewerB); oB == nil || oB.CrewedTurn != e.G.Turn || !crewListHas(oB.CrewedVehicles, vanID) {
		t.Fatalf("precondition: the second crew pairing was not recorded: %+v", oB)
	}

	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, vanID)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("CR 603.4: the returned Van's attack trigger must ask for a creature that crewed it; got %+v", d)
	}
	offeredB, offeredA := false, false
	for _, o := range d.Options {
		if o.Obj == crewerB {
			offeredB = true
		}
		if o.Obj == crewerA {
			offeredA = true
		}
	}
	if !offeredB {
		t.Fatalf("Creature.CrewedThisTurn must offer the NEW stint's crewer; options were %+v", d.Options)
	}
	if offeredA {
		t.Fatalf("CR 400.7: the OLD stint's crewer must not be offered after the blink; options were %+v", d.Options)
	}
	replayCheck(t, e, cfg)
}

func saNamesToken(sa *cards.SA, token string) bool {
	for sa != nil {
		if paramsNameToken(sa.Params, token) {
			return true
		}
		sa = sa.Sub
	}
	return false
}

func paramsNameToken(params map[string]string, token string) bool {
	for _, v := range params {
		if strings.Contains(v, token) {
			return true
		}
	}
	return false
}

func sortedCrewNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
