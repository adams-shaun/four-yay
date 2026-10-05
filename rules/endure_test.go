package rules

// Endure N (CR 701.63) at the engine level: the ETB trigger's DB$ Endure poses
// a real mid-resolution KChoose and honours the answered branch. The
// effects-side unit tests cover the branches in isolation; these drive them
// through the real cast/trigger/tape path so the election is proven answered
// by the seat, never silently defaulted.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const endureCritterSrc = "Name:Endure Test Critter\nManaCost:G\nTypes:Creature Beast\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigEndure | TriggerDescription$ When this creature enters, it endures 2.\n" +
	"SVar:TrigEndure:DB$ Endure | Num$ 2\nOracle:x\n"

const endureSpiritTokSrc = "Name:Spirit Token\nManaCost:no cost\nColors:white\nTypes:Creature Spirit\nPT:*/*\nOracle:\n"

// endureDrive answers the Endure election branch index and returns the
// decision it answered, asserting the engine really posed the election.
func endureDrive(t *testing.T, e *Engine, branch int) *decision.Decision {
	t.Helper()
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while draining (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "endure" {
			if len(d.Options) != 2 || d.Options[0].Kind != "endure_counters" || d.Options[1].Kind != "endure_spirit" {
				t.Fatalf("Endure decision options = %+v, want the counters/spirit pair", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{branch}}); err != nil {
				t.Fatalf("submit Endure answer: %v", err)
			}
			return d
		}
		if d.Kind == decision.KPriority {
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		submitChoices(t, e, tapePick(d)...)
	}
	t.Fatal("Endure election was never posed")
	return nil
}

// TestEndureEngineCounterBranch: the seat answers for counters and the entered
// creature carries two +1/+1 counters, with no Spirit minted.
func TestEndureEngineCounterBranch(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 201, []string{endureCritterSrc}, nil)
	e.G.Tokens["w_x_x_spirit"] = card(t, endureSpiritTokSrc)
	id := find("Endure Test Critter", 0)
	addMana(t, e, 0, "G")
	castFirst(t, e, "cast")
	if d := endureDrive(t, e, 0); d == nil {
		t.Fatal("no election")
	}
	passUntilStackEmpty(t, e, 30)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Endure Test Critter is not on the battlefield: %+v", o)
	}
	if got := o.Counter("P1P1"); got != 2 {
		t.Fatalf("counter branch placed %d +1/+1 counters, want 2", got)
	}
	for _, bid := range e.G.Zone(state.ZBattlefield, 0) {
		if bo := e.G.Obj(bid); bo != nil && bo.IsToken {
			t.Fatalf("counter branch minted a token: %+v", bo)
		}
	}
	replayCheck(t, e, cfg)
}

// TestEndureEngineSpiritBranch: the seat answers for the Spirit and an N/N
// white Spirit token is minted, with no counters on the entered creature.
func TestEndureEngineSpiritBranch(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 202, []string{endureCritterSrc}, nil)
	e.G.Tokens["w_x_x_spirit"] = card(t, endureSpiritTokSrc)
	id := find("Endure Test Critter", 0)
	addMana(t, e, 0, "G")
	castFirst(t, e, "cast")
	if d := endureDrive(t, e, 1); d == nil {
		t.Fatal("no election")
	}
	passUntilStackEmpty(t, e, 30)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Endure Test Critter is not on the battlefield: %+v", o)
	}
	if got := o.Counter("P1P1"); got != 0 {
		t.Fatalf("spirit branch placed %d +1/+1 counters on the creature, want 0", got)
	}
	var spirit state.ObjID
	for _, bid := range e.G.Zone(state.ZBattlefield, 0) {
		if bo := e.G.Obj(bid); bo != nil && bo.IsToken && bo.Face() != nil && bo.Face().Name == "Spirit Token" {
			spirit = bid
		}
	}
	if spirit == 0 {
		t.Fatalf("spirit branch minted no Spirit token; battlefield = %v", e.G.Zone(state.ZBattlefield, 0))
	}
	if got := e.Power(spirit); got != 2 {
		t.Fatalf("Spirit power = %d, want 2", got)
	}
	if got := e.Toughness(spirit); got != 2 {
		t.Fatalf("Spirit toughness = %d, want 2", got)
	}
	replayCheck(t, e, cfg)
}
