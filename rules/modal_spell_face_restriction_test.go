package rules

// agent-20260923T132455Z-3f6a9ab9 review round 3 MAJOR: the CR 712.8
// modal_spell offer used to sit behind the card-level castRestricted gate,
// which evaluates the FRONT face, while its affordability probe priced the
// BACK face. Two prohibition directions regressed together:
//
//   - a CantBeCast static that matches the back face's type but not the
//     front's (Nikya of the Old Ways' "you can't cast noncreature spells"
//     against Extus // Awaken the Blood Avatar) still offered the prohibited
//     back-face sorcery, which recheckIllegal then reversed (CR 733.1) and
//     re-offered forever;
//   - one that matches the front's type but not the back's (Steel Golem's
//     "you can't cast creature spells") withheld the otherwise-legal
//     back-face offer entirely.
//
// The fix evaluates the back face's CantBeCast prohibition under the same
// offerAsFace probe that prices its cost (castRestrictedAsFace in
// rules/legal.go), so both directions answer with the face actually cast --
// the same face recheckIllegal re-checks after beginCast's real FlipFace
// (CR 712.4d).

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// addCorpusBattlefield moves a real corpus card onto seat 0's battlefield
// with a real MoveZone event and returns its id. It re-asks priority
// afterwards, the same way sacXFixture's addMana does: the pending priority
// decision is a snapshot, and an offer assertion made against a decision
// built before the static carrier arrived would read the pre-restriction
// board.
func addCorpusBattlefield(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(corpusAlternativeCard(t, name), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.priorityRound()
	return o.ID
}

// awakenPreconditions asserts the Extus // Awaken board both
// prohibition-direction tests lean on: the card is the modal front in hand,
// a spell back face is reachable, and the pool is below the unreduced
// {6}{B}{R}=8 (so the back face's reducible cost is the only affordability
// question).
func awakenPreconditions(t *testing.T, e *Engine, spell state.ObjID) {
	t.Helper()
	o := e.G.Obj(spell)
	if o == nil || o.FaceIdx != 0 || o.Face().Name != "Extus, Oriq Overlord" || o.Zone != state.ZHand {
		if o == nil {
			t.Fatal("precondition: Awaken card missing from the engine")
		}
		t.Fatalf("precondition: card is zone=%s face=%d %q, want Extus front in hand",
			o.Zone, o.FaceIdx, o.Face().Name)
	}
	if modalSpellBack(o) == nil {
		t.Fatal("precondition: no modal spell back face reachable from the hand card")
	}
	if pool := e.G.Players[0].Pool; pool.Total() >= 8 {
		t.Fatalf("precondition: pool %+v totals %d, want below the unreduced {6}{B}{R}=8",
			pool, pool.Total())
	}
}

// probeBackRestricted evaluates castRestricted against the Awaken back face
// under the same offerAsFace probe the offer walk prices with.
func probeBackRestricted(t *testing.T, e *Engine, spell state.ObjID) bool {
	t.Helper()
	back := modalSpellBack(e.G.Obj(spell))
	if back == nil {
		t.Fatal("no modal spell back face to probe")
	}
	return e.offerAsFace(spell, back, func() bool {
		return e.castRestricted(0, spell)
	})
}

// modalSpellOption returns the index of the modal_spell cast offer for
// spell and whether one exists.
func modalSpellOption(e *Engine, spell state.ObjID) (int, bool) {
	for _, option := range e.Pending().Options {
		if option.Kind == "cast" && option.Obj == spell && option.Mode == "modal_spell" &&
			option.Label == "Cast Awaken the Blood Avatar" {
			return option.Index, true
		}
	}
	return -1, false
}

// TestModalSpellBackFaceRestrictionWithholdsOffer is the "back face
// prohibited" direction: Nikya's Card.nonCreature CantBeCast static on the
// battlefield matches the Awaken sorcery back face but not the Extus
// creature front, so the modal_spell offer must be withheld even though the
// FRONT face is unrestricted (which is what the pre-fix gate read).
func TestModalSpellBackFaceRestrictionWithholdsOffer(t *testing.T) {
	t.Parallel()
	e, spell, _ := sacXFixture(t, "Awaken the Blood Avatar", []string{
		"Name:C1\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C2\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C3\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C4\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "BR")
	awakenPreconditions(t, e, spell)

	nikya := addCorpusBattlefield(t, e, "Nikya of the Old Ways")
	no := e.G.Obj(nikya)
	if no.Zone != state.ZBattlefield || no.Face().Name != "Nikya of the Old Ways" {
		t.Fatalf("precondition: Nikya is zone=%s %q, want on the battlefield",
			no.Zone, no.Face().Name)
	}
	// Precondition: the front face is NOT what the static hits -- if the
	// card-level gate were true here, the buggy offer walk withheld the
	// modal option too and this test could not fail.
	if e.castRestricted(0, spell) {
		t.Fatal("precondition: the Extus front face is restricted, so the " +
			"back-face-only fixture is vacuous")
	}
	// Precondition: the BACK face really is what the static hits under the
	// probe the offer walk prices with.
	if !probeBackRestricted(t, e, spell) {
		t.Fatal("precondition: the Awaken back face is not restricted by " +
			"Nikya's Card.nonCreature static under the face probe")
	}

	if idx, ok := modalSpellOption(e, spell); ok {
		t.Fatalf("modal_spell offered for a back face the CantBeCast static "+
			"prohibits: %+v", e.Pending().Options[idx])
	}
}

// TestModalSpellOfferSurvivesFrontFaceRestriction is the "front face
// prohibited" direction: Steel Golem's ValidCard$ Creature CantBeCast static
// matches the Extus creature front but not the Awaken sorcery back face, so
// the back-face cast must still be offered, paid and resolved -- the old
// card-level gate withheld it entirely. The Golem is itself the fourth
// Sac<X/Creature> candidate, so the full cast path runs with exactly the
// three token creatures plus the Golem.
func TestModalSpellOfferSurvivesFrontFaceRestriction(t *testing.T) {
	t.Parallel()
	e, spell, ids := sacXFixture(t, "Awaken the Blood Avatar", []string{
		"Name:C1\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C2\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C3\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "BR")
	awakenPreconditions(t, e, spell)
	golem := addCorpusBattlefield(t, e, "Steel Golem")
	golemObj := e.G.Obj(golem)
	if golemObj.Zone != state.ZBattlefield || golemObj.Face().Name != "Steel Golem" {
		t.Fatalf("precondition: Steel Golem is zone=%s %q, want on the battlefield",
			golemObj.Zone, golemObj.Face().Name)
	}
	if !golemObj.Face().IsCreature() {
		t.Fatalf("precondition: Steel Golem face %q is not a creature, so it is "+
			"not the fourth sacrifice candidate", golemObj.Face().Name)
	}
	// Precondition: the FRONT face really is restricted -- this is the gate
	// that withheld the modal offer before the fix, so it must bind here or
	// the test cannot fail on the old code.
	if !e.castRestricted(0, spell) {
		t.Fatal("precondition: the Extus front face is not restricted by Steel " +
			"Golem's ValidCard$ Creature static")
	}
	// Precondition: the BACK face is NOT restricted under the same probe.
	if probeBackRestricted(t, e, spell) {
		t.Fatal("precondition: the Awaken back face is restricted, so this is " +
			"not the front-only direction")
	}

	idx, ok := modalSpellOption(e, spell)
	if !ok {
		t.Fatalf("modal_spell withheld although only the front face is "+
			"restricted: %+v", e.Pending().Options)
	}
	ds := announceSacXAt(t, e, idx, 4)
	if o := e.G.Obj(spell); o.FaceIdx != 1 || o.Face().Name != "Awaken the Blood Avatar" {
		t.Fatalf("modal spell offer did not select Awaken face: %+v", o)
	}
	if len(ds.Options) != 4 {
		t.Fatalf("sacrifice options %+v, want exactly the three creatures plus Steel Golem", ds.Options)
	}
	want := map[state.ObjID]bool{ids[0]: true, ids[1]: true, ids[2]: true, golem: true}
	chosen := make([]int, 0, 4)
	for _, o := range ds.Options {
		if !want[o.Obj] {
			t.Fatalf("sacrifice option %+v is not one of the four Sac<Creature> candidates", o)
		}
		chosen = append(chosen, o.Index)
	}
	submitChoices(t, e, chosen...)
	drainResolution(t, e, 80)
	for id := range want {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("candidate %d zone=%s, want graveyard (all four sacrificed)",
				id, e.G.Obj(id).Zone)
		}
	}
	if o := e.G.Obj(spell); o.Zone != state.ZGraveyard {
		t.Fatalf("Awaken zone=%s, want graveyard (sorcery resolved)", o.Zone)
	}
	token := false
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.Power(id) == 3 && e.Toughness(id) == 6 {
			token = true
		}
	}
	if !token {
		t.Fatalf("no 3/6 Avatar token on seat 0's battlefield after resolution: %v",
			e.G.Zone(state.ZBattlefield, 0))
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after payment=%+v, want empty", pool)
	}
}
