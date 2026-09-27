package effects

// The `Count$ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid>$<Property>` sum
// form (Genesis of the Daleks' `SVar:Y:..._Dalek$CardPower`): before the fix
// the whole `Dalek$CardPower` token went to the zone-spec matcher as one
// filter, matched nothing, and the damage evaluated to zero. It now splits
// the recognised property off the <Valid> tail and sums each matching entry's
// value through the shared objectProperty reader -- the same read the nearby
// `Count$Valid <spec>$CardPower` aggregate uses.

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// movedThisTurn establishes a real entry record: the object is moved to dest
// (its live zone list, its Zone field and the per-add state.Entered list that
// the ThisTurnEntered heads fold) from origin, mirroring events.Move.
func movedThisTurn(h *fakeHost, o *state.Object, dest, origin state.Zone) {
	g := h.g
	if from := g.Zone(origin, o.Controller); from != nil {
		g.SetZone(origin, o.Controller, withoutID(from, o.ID))
	}
	g.SetZone(dest, o.Controller, append(g.Zone(dest, o.Controller), o.ID))
	o.Zone = dest
	g.Entered = append(g.Entered, state.ZoneEntry{Obj: o.ID, To: dest, From: origin})
}

func withoutID(ids []state.ObjID, id state.ObjID) []state.ObjID {
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// liveZone returns the live zone of an object, or a sentinel when it is gone.
func liveZone(g *state.Game, id state.ObjID) state.Zone {
	if o := g.Obj(id); o != nil {
		return o.Zone
	}
	return state.Zone(255)
}

// entryFor returns the state.Entered record for an object, if any.
func entryFor(g *state.Game, id state.ObjID) (state.ZoneEntry, bool) {
	for _, e := range g.Entered {
		if e.Obj == id {
			return e, true
		}
	}
	return state.ZoneEntry{}, false
}

// TestThisTurnEnteredPropertySumsPower pins the sum semantics with values that
// DIFFER from the count: three Dalek creatures died this turn with powers
// 3/4/5, so the count form reads 3 and the $CardPower form reads 12. A
// surviving Dalek on the battlefield (never entered the graveyard) and a
// non-Dalek that died are both ineligible and must not contribute, so the
// assertion cannot pass by merely summing every entry.
func TestThisTurnEnteredPropertySumsPower(t *testing.T) {
	h := newHost(t, 2)
	g := h.g

	// Precise printed powers, all distinctive and all different from the
	// count, so a count-shaped regression cannot accidentally match.
	for _, p := range []string{"3/3", "4/4", "5/5"} {
		o := g.AddObject(mkCard(t, "Name:Dalek "+p+"\nTypes:Artifact Creature Dalek\nPT:"+p+"\nOracle:x\n"), 0)
		movedThisTurn(h, o, state.ZGraveyard, state.ZBattlefield)
	}
	// A NON-Dalek creature that died this turn: eligible by destination and
	// origin, excluded by the filter.
	dead := g.AddObject(mkCard(t, "Name:Human Scout\nTypes:Creature Human Scout\nPT:2/2\nOracle:x\n"), 0)
	movedThisTurn(h, dead, state.ZGraveyard, state.ZBattlefield)
	// A Dalek that is still alive (never entered the graveyard): ineligible
	// by destination.
	alive := g.AddObject(mkCard(t, "Name:Dalek Alive\nTypes:Artifact Creature Dalek\nPT:9/9\nOracle:x\n"), 0)
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), alive.ID))
	alive.Zone = state.ZBattlefield
	// Guard the setup: exactly the three 3/4/5 Dalek entries sum to 12, and
	// the ineligible non-Dalek contributes a different value, so a wrongly
	// widened sum (17) cannot satisfy the assertion below.
	sum, nDalek := int32(0), 0
	for _, id := range g.Zone(state.ZGraveyard, 0) {
		o := g.Obj(id)
		if o == nil || !hasType(o, "Dalek") {
			continue
		}
		sum += int32(o.Face().Power())
		nDalek++
	}
	if sum != 12 || nDalek != 3 {
		t.Fatalf("precondition: %d Dalek entries in the graveyard summing to %d, want 3 and 12", nDalek, sum)
	}

	c := &Ctx{Controller: 0}
	const base = "Count$ThisTurnEntered_Graveyard_from_Battlefield_Dalek"
	count, ok := EvalCountOK(h, c, base)
	if !ok || count != 3 {
		t.Fatalf("count form %s = (%d, %v), want (3, true)", base, count, ok)
	}
	got, ok := EvalCountOK(h, c, base+"$CardPower")
	if !ok {
		t.Fatalf("sum form %s$CardPower did not evaluate", base)
	}
	if got != 12 {
		t.Errorf("%s$CardPower = %d, want 12 (3+4+5, not the count 3)", base, got)
	}
	// The related $CardToughness form reads the same matching set; here it
	// equals CardPower (the Daleks are square), a case the printed-face read
	// handles identically.
	if got, ok := EvalCountOK(h, c, base+"$CardToughness"); !ok || got != 12 {
		t.Errorf("%s$CardToughness = (%d, %v), want (12, true)", base, got, ok)
	}
	// An UNRECOGNISED property keeps the whole token as the spec -- the
	// fail-closed read the Count$Valid family also takes -- so it evaluates
	// to zero (nothing matches the literal `Dalek$Bogus`), never a sum.
	if got, ok := EvalCountOK(h, c, base+"$Bogus"); ok && got != 0 {
		t.Errorf("%s$Bogus = %d, want 0 (unrecognised property kept as spec)", base, got)
	}
}

// TestThisTurnEnteredPropertyRespectsOriginAndFilter pins that the sum obeys
// the same destination/origin/filter constraints as the count: a Dalek that
// entered the graveyard from the library (wrong origin) and a Dalek that
// entered exile (wrong destination) contribute nothing.
func TestThisTurnEnteredPropertyRespectsOriginAndFilter(t *testing.T) {
	h := newHost(t, 2)
	g := h.g
	fromLibrary := g.AddObject(mkCard(t, "Name:Dalek A\nTypes:Artifact Creature Dalek\nPT:6/6\nOracle:x\n"), 0)
	movedThisTurn(h, fromLibrary, state.ZGraveyard, state.ZLibrary)
	exiled := g.AddObject(mkCard(t, "Name:Dalek B\nTypes:Artifact Creature Dalek\nPT:7/7\nOracle:x\n"), 0)
	movedThisTurn(h, exiled, state.ZExile, state.ZBattlefield)
	// The one honest entry, different from both ineligible powers.
	honest := g.AddObject(mkCard(t, "Name:Dalek C\nTypes:Artifact Creature Dalek\nPT:4/4\nOracle:x\n"), 0)
	movedThisTurn(h, honest, state.ZGraveyard, state.ZBattlefield)

	// Assert each object really IS where the rule reads it, with the origin
	// the scenario claims -- so a mis-wired setup fails loudly rather than
	// the sum assertion silently passing.
	if o := g.Obj(fromLibrary.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the library-origin Dalek is in zone %v, want the graveyard", liveZone(g, fromLibrary.ID))
	}
	if e, ok := entryFor(g, fromLibrary.ID); !ok || e.To != state.ZGraveyard || e.From != state.ZLibrary {
		t.Fatalf("precondition: library-origin Dalek entry = %+v, want To graveyard, From library", e)
	}
	if o := g.Obj(exiled.ID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: the exiled Dalek is in zone %v, want exile", liveZone(g, exiled.ID))
	}
	if e, ok := entryFor(g, exiled.ID); !ok || e.To != state.ZExile || e.From != state.ZBattlefield {
		t.Fatalf("precondition: exiled Dalek entry = %+v, want To exile, From battlefield", e)
	}
	if o := g.Obj(honest.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the honest Dalek is in zone %v, want the graveyard", liveZone(g, honest.ID))
	}
	if e, ok := entryFor(g, honest.ID); !ok || e.To != state.ZGraveyard || e.From != state.ZBattlefield {
		t.Fatalf("precondition: honest Dalek entry = %+v, want To graveyard, From battlefield", e)
	}

	c := &Ctx{Controller: 0}
	const base = "Count$ThisTurnEntered_Graveyard_from_Battlefield_Dalek"
	// Guard the setup: the two ineligible bodies really carry different
	// powers, so a widened sum would read 6+7+4=17, not 4.
	if got, ok := EvalCountOK(h, c, base); !ok || got != 1 {
		t.Fatalf("precondition: count = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, base+"$CardPower"); !ok || got != 4 {
		t.Errorf("%s$CardPower = (%d, %v), want (4, true); a wrong origin/destination leaked in", base, got, ok)
	}
}

// TestAlenaKessigTrapperGreatestEnteredPower guards the ADJACENT grammar the
// fix must not touch: Alena's real SVar is
// `Count$Valid Creature.YouCtrl+ThisTurnEntered$GreatestCardPower`, a
// Count$Valid extreme-reduction over a `+ThisTurnEntered` filter predicate,
// which never reaches evalThisTurnEnteredAs. This pins that the greatest
// power (not the count, not a sum) is still read from the real corpus SVar
// after the property-sum split landed on the ThisTurnEntered_* count path.
func TestAlenaKessigTrapperGreatestEnteredPower(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Alena, Kessig Trapper")
	if !ok {
		t.Fatal("corpus missing Alena, Kessig Trapper")
	}
	face := card.Faces[0]
	const wantSVar = "Count$Valid Creature.YouCtrl+ThisTurnEntered$GreatestCardPower"
	if got := face.SVars["X"]; got != wantSVar {
		t.Fatalf("precondition: Alena SVar X = %q, want %q", got, wantSVar)
	}

	h := newHost(t, 2)
	g := h.g
	// Two creatures that entered this turn under seat 0 with powers 4 and 7;
	// the greatest is 7, the count is 2, the sum is 11. The ThisTurnEntered
	// filter predicate reads the per-object provenance flag events.Move sets,
	// so set it alongside the entry record.
	for _, p := range []string{"4/4", "7/7"} {
		o := g.AddObject(mkCard(t, "Name:Entered "+p+"\nTypes:Creature Human\nPT:"+p+"\nOracle:x\n"), 0)
		g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
		o.Zone = state.ZBattlefield
		o.EnteredThisTurn = true
		g.Entered = append(g.Entered, state.ZoneEntry{Obj: o.ID, To: state.ZBattlefield, From: state.ZHand})
	}
	// A creature that did NOT enter this turn: excluded by the filter.
	old := g.AddObject(mkCard(t, "Name:Old Bear\nTypes:Creature Bear\nPT:9/9\nOracle:x\n"), 0)
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), old.ID))
	old.Zone = state.ZBattlefield

	// Assert the zone preconditions the filter depends on: both claimed
	// entered creatures are battlefield permanents that really entered this
	// turn, and the old Bear is on the battlefield but did NOT.
	entered := 0
	for _, id := range g.Zone(state.ZBattlefield, 0) {
		o := g.Obj(id)
		if o != nil && o.EnteredThisTurn && o.Zone == state.ZBattlefield {
			entered++
		}
	}
	if entered != 2 {
		t.Fatalf("precondition: %d battlefield creatures entered this turn, want 2", entered)
	}
	if !old.EnteredThisTurn && old.Zone == state.ZBattlefield {
		// the old Bear is the false case: correct by construction
	} else {
		t.Fatalf("precondition: the old Bear must not have entered this turn (zone %v)", old.Zone)
	}

	got := EvalCount(h, &Ctx{Controller: 0}, face.SVars["X"])
	// 7 (greatest entered power), never 9 (the old Bear), 2 (the count) or
	// 11 (the sum).
	if got != 7 {
		t.Errorf("Alena SVar X = %d, want 7 (greatest entered power)", got)
	}
}
