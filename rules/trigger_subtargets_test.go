package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggerChainTargetsAnnouncedOnPlacement pins CR 603.3d for Uldaros
// Theorix's cast trigger: its eight per-card-type targeting links are asked
// while the trigger is put on the stack (KTarget, ResumeKind "trig_sub"),
// before anyone receives priority and before anything resolves -- not as
// mid-resolution choose asks -- and the tail link's Defined$ Targeted then
// exiles every announced target (the root+chain union).
func TestTriggerChainTargetsAnnouncedOnPlacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sc := oracleScenario{
		Setup: map[string]oracleSeat{"p0": {Hand: []string{"Uldaros Theorix"},
			Graveyard: []string{"Lightning Bolt", "Grizzly Bears"}}},
		Steps: []oracleStep{{Op: "cast", Seat: 0, Card: "p0:Uldaros Theorix", Mana: "CCCUBB"}},
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) > 0 || run.e == nil {
		t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
	}
	e := run.e
	bolt, err := run.resolve("p0:Lightning Bolt")
	if err != nil {
		t.Fatal(err)
	}
	bears, err := run.resolve("p0:Grizzly Bears")
	if err != nil {
		t.Fatal(err)
	}
	// Pass priority until Uldaros resolves and its trigger is placed.
	var d *decision.Decision
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		submitChoicePass(t, e)
	}
	trigger := state.ObjID(0)
	if n := len(e.G.Stack); n > 0 {
		trigger = e.G.Stack[n-1]
	}
	if o := e.G.Obj(trigger); o == nil || o.Ability == nil {
		t.Fatalf("the cast trigger is not on top of the stack: %+v", e.G.Stack)
	}
	// Chain order: the artifact root and the battle link have no candidate;
	// the creature link is asked first, then the instant link.
	for _, want := range []state.ObjID{bears, bolt} {
		if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" || d.Source != trigger {
			t.Fatalf("chain-link ask = %+v, want a placement trig_sub target ask", d)
		}
		idx := indexOfObjOption(d, want)
		if idx < 0 || len(d.Options) != 1 || d.Min != 0 || d.Max != 1 {
			t.Fatalf("chain-link ask offers %+v (min %d max %d), want only obj %d, up to one", d.Options, d.Min, d.Max, want)
		}
		submitChoices(t, e, idx)
		d = e.Pending()
	}
	// The announcement is complete: priority, with the trigger still on the
	// stack and both cards still in the graveyard.
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the announcement = %+v, want priority", d)
	}
	if got := e.G.Stack; len(got) == 0 || got[len(got)-1] != trigger {
		t.Fatalf("the trigger left the stack during its announcement: %+v", got)
	}
	for _, id := range []state.ObjID{bolt, bears} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("obj %d zone %s before the trigger resolved, want graveyard", id, z)
		}
	}
	// Resolve: decline the free casts so only the exile is observed.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			submitChoicePass(t, e)
			continue
		}
		if d.Kind == decision.KTarget {
			t.Fatalf("a target ask was re-posed at resolution: %+v", d)
		}
		submitChoices(t, e)
	}
	for _, id := range []state.ObjID{bolt, bears} {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("obj %d zone %s after the trigger resolved, want exile", id, z)
		}
	}
	replayCheck(t, e, run.cfg)
}

// TestTriggerChainTargetedReadsTheUnion: The Spot, Living Portal's ETB ("exile
// up to one target nonland permanent and up to one target nonland permanent
// card from a graveyard") is a targeting root, a targeting graveyard link and
// an untargeted ChangeZone | Defined$ Targeted. The exile acts on BOTH
// targets: the root's and the announced link's.
func TestTriggerChainTargetedReadsTheUnion(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sc := oracleScenario{
		Setup: map[string]oracleSeat{
			"p0": {Hand: []string{"The Spot, Living Portal"}},
			"p1": {Battlefield: []string{"Grizzly Bears"}, Graveyard: []string{"Hill Giant"}},
		},
		Steps: []oracleStep{
			{Op: "cast", Seat: 0, Card: "p0:The Spot, Living Portal", Mana: "CCCWB"},
			{Op: "resolve", Targets: []string{"p1:Grizzly Bears", "p1:Hill Giant"}},
		},
		Expect: []oracleExpect{
			{Card: "p1:Grizzly Bears", Zone: "exile"},
			{Card: "p1:Hill Giant", Zone: "exile"},
		},
	}
	if fails, transcript, _ := runOracleScenario(reg, sc); len(fails) > 0 {
		t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
	}
}

// TestAnnouncedChainTargetedReadsTheUnionOnASpell: the union binding is not
// trigger-only. Urgent Necropsy announces its chain on cast (the AllTargeted$
// evidence cost, alltargeted1) and ends in an untargeted Destroy | Defined$
// Targeted: "destroy up to one target artifact, up to one target creature,
// ..." destroys the artifact root target AND the creature link's target.
func TestAnnouncedChainTargetedReadsTheUnionOnASpell(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sc := oracleScenario{
		Setup: map[string]oracleSeat{
			"p0": {Hand: []string{"Urgent Necropsy"}, Graveyard: []string{"Hill Giant"}},
			"p1": {Battlefield: []string{"Sol Ring", "Grizzly Bears"}},
		},
		Steps: []oracleStep{
			{Op: "cast", Seat: 0, Card: "p0:Urgent Necropsy", Mana: "CCBG",
				Targets: []string{"p1:Sol Ring", "p1:Grizzly Bears"},
				Answers: []oracleAnswer{{Kind: "choose", Pick: []string{"p0:Hill Giant"}}}},
			{Op: "resolve"},
		},
		Expect: []oracleExpect{
			{Card: "p1:Sol Ring", Zone: "graveyard"},
			{Card: "p1:Grizzly Bears", Zone: "graveyard"},
			{Card: "p0:Hill Giant", Zone: "exile"},
		},
	}
	if fails, transcript, _ := runOracleScenario(reg, sc); len(fails) > 0 {
		t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
	}
}
