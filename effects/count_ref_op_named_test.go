package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// doranSVars is Doran, Besieged by Time's SVar chain: the pump's X1 is
// |power - toughness| of the triggering attacker, spelled as an Abs over a
// property read whose /Minus operand is the NAMED SVar Z1.
var doranSVars = map[string]string{
	"X1": "SVar$Y1/Abs",
	"Y1": "TriggeredAttacker$CardPower/Minus.Z1",
	"Z1": "TriggeredAttacker$CardToughness",
	"X2": "SVar$Y2/Abs",
	"Y2": "TriggeredBlocker$CardPower/Minus.Z2",
	"Z2": "TriggeredBlocker$CardToughness",
}

// TestRefPropertyNamedOperandDoranChain pins the <Ref>$<Property>/<Op>.<SVar>
// shape (Doran, Besieged by Time, Jaws of Defeat, Lady Loki): the named operand
// resolves instead of being dropped, and /Abs takes |n|. The attack half (a 0/5
// attacker: |0-5| = 5) and the block half (a 2/2 blocker: |2-2| = 0) differ, so
// a read that drops the operand (0 and 2) fails both.
func TestRefPropertyNamedOperandDoranChain(t *testing.T) {
	h := newHost(t, 2)
	doran := h.g.AddObject(mkCard(t, "Name:Doran\nManaCost:3\nTypes:Creature Treefolk\nPT:0/5\nOracle:x\n"), 0)
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nManaCost:2\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)

	// Preconditions: the compared values really differ per half.
	pc := &Ctx{Controller: 0, Remembered: refObj(doran.ID)}
	if p, tg := EvalCount(h, pc, "TriggeredAttacker$CardPower"), EvalCount(h, pc, "TriggeredAttacker$CardToughness"); p != 0 || tg != 5 {
		t.Fatalf("precondition: Doran reads %d/%d, want 0/5", p, tg)
	}

	attack := &Ctx{Controller: 0, Remembered: refObj(doran.ID), SVars: doranSVars}
	block := &Ctx{Controller: 0, Remembered: refObj(bear.ID), SVars: doranSVars}

	if got := EvalCount(h, attack, "SVar$Y1"); got != -5 {
		t.Errorf("attack: raw Y1 = %d, want -5 (0 power minus 5 toughness)", got)
	}
	if got := EvalCount(h, attack, "SVar$X1"); got != 5 {
		t.Errorf("attack: X1 (SVar$Y1/Abs) = %d, want 5", got)
	}
	if got := EvalCount(h, block, "SVar$Y2"); got != 0 {
		t.Errorf("block: raw Y2 = %d, want 0 (2 power minus 2 toughness)", got)
	}
	if got := EvalCount(h, block, "SVar$X2"); got != 0 {
		t.Errorf("block: X2 (SVar$Y2/Abs) = %d, want 0", got)
	}
	// The pump parameter path: NumAtt$ +X1 resolves through numResolvedText.
	if got := EvalCount(h, attack, "TriggeredAttacker$CardPower/Minus.Z1"); got != -5 {
		t.Errorf("attack: direct named-operand read = %d, want -5", got)
	}
}

// TestRefPropertyNumericOperandUnchanged keeps the literal-operand spelling on
// the same site exactly as before the named-operand applier replaced it.
func TestRefPropertyNumericOperandUnchanged(t *testing.T) {
	h := newHost(t, 2)
	doran := h.g.AddObject(mkCard(t, "Name:Doran\nManaCost:3\nTypes:Creature Treefolk\nPT:0/5\nOracle:x\n"), 0)
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nManaCost:2\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	d := &Ctx{Controller: 0, Remembered: refObj(doran.ID)}
	b := &Ctx{Controller: 0, Remembered: refObj(bear.ID)}
	if got := EvalCount(h, d, "TriggeredAttacker$CardToughness/Minus.2"); got != 3 {
		t.Errorf("Doran toughness/Minus.2 = %d, want 3", got)
	}
	if got := EvalCount(h, b, "TriggeredBlocker$CardPower/Plus.3"); got != 5 {
		t.Errorf("bear power/Plus.3 = %d, want 5", got)
	}
	// An unresolvable name falls soft: the base value stands.
	if got := EvalCount(h, b, "TriggeredBlocker$CardPower/Minus.Nope"); got != 2 {
		t.Errorf("unresolved operand = %d, want base 2", got)
	}
}

// TestCountOpAbsThroughNamedSVarChain pins the /Abs arm beyond Doran:
// Psychic Transfer's SVar$Y/Abs over Count$YourLifeTotal/Minus.Z (named
// operand, both heads modelled) and Profane Transfusion's
// Count$RememberedNumber/Abs shape.
func TestCountOpAbsThroughNamedSVarChain(t *testing.T) {
	h := newHost(t, 2)
	h.g.Players[0].Life = 7
	h.g.Players[1].Life = 12
	svars := map[string]string{
		"X": "SVar$Y/Abs",
		"Y": "Count$YourLifeTotal/Minus.Z",
		"Z": "Count$OppGreatestLifeTotal",
	}
	c := &Ctx{Controller: 0, SVars: svars}
	if got := EvalCount(h, c, "SVar$Y"); got != -5 {
		t.Fatalf("precondition: Y = %d, want -5 (7 - 12)", got)
	}
	if got := EvalCount(h, c, "SVar$X"); got != 5 {
		t.Errorf("Abs of negative = %d, want 5", got)
	}
	h.g.Players[0].Life = 15
	if got := EvalCount(h, c, "SVar$Y"); got != 3 {
		t.Fatalf("precondition: Y = %d, want 3 (15 - 12)", got)
	}
	if got := EvalCount(h, c, "SVar$X"); got != 3 {
		t.Errorf("Abs of positive = %d, want 3", got)
	}
}

func refObj(id state.ObjID) []state.Target { return []state.Target{{Obj: id}} }
