package rules

// Kernel-era restorations of the flexible_pip_upkeep_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestCumulativeUpkeepFlexiblePipPayment is the cumulative-upkeep regression:
// a {W/U} upkeep cost is announced, the elected green face is paid from a
// Forest, the permanent survives and only the elected resource is consumed.
func TestCumulativeUpkeepFlexiblePipPayment(t *testing.T) {
	t.Parallel()
	const cumulativeHybrid = "Name:Test Cumulative Hybrid\nManaCost:1\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Cumulative upkeep:W/U\nOracle:test\n"
	e, cfg, bear := kr6FlexCumulativeEngine(t, cumulativeHybrid, []string{
		"Name:Island\nTypes:Basic Land Island\nOracle:x\n",
		"Name:Forest\nTypes:Basic Land Forest\nOracle:x\n",
	})
	// Precondition: the permanent is on the battlefield and the age counter
	// the window reads exists.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the cumulative permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if !e.G.Obj(bear).Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: the fixture does not print Cumulative upkeep")
	}
	if got := e.G.Obj(bear).Counter("AGE"); got != 1 {
		t.Fatalf("precondition: age counters = %d, want 1", got)
	}
	if c := ParseCost("W/U"); len(c.Hybrid) != 1 {
		t.Fatalf("precondition: ParseCost(W/U) = %+v, want one hybrid pip", c)
	}

	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 || faces[0] != "pay_W" || faces[1] != "pay_U" {
		t.Fatalf("hybrid {W/U} faces = %v, want the distinct pay_W and pay_U", faces)
	}
	submitChoices(t, e, 1) // elect blue

	// Drain the mana window: tap the Island, then re-open to the election.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear ||
		len(d.Options) == 0 || d.Options[0].Kind != "activate" {
		t.Fatalf("expected the mana-ability window after the announcement, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the cumulative election, got %+v", d)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("hybrid cumulative election = %+v, want a pay option once blue is in the pool", d.Options)
	}
	poolBefore := e.G.Players[0].Pool
	if poolBefore[state.MU] != 1 || poolBefore[state.MW] != 0 {
		t.Fatalf("precondition: pool before payment = %+v, want exactly one blue", poolBefore)
	}
	submitChoices(t, e, 0) // pay

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid the hybrid upkeep but the permanent is %+v", o)
	}
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("hybrid upkeep left %d mana in the pool, want the elected blue consumed", total)
	}
	replayCheck(t, e, cfg)
}

// TestCumulativeUpkeepUnpayableElectionStillSacrifices is the cumulative
// decline half: no mana source means the announcement is posed but the pay
// option is absent and the sacrifice path is retained.
func TestCumulativeUpkeepUnpayableElectionStillSacrifices(t *testing.T) {
	t.Parallel()
	const cumulativeHybrid = "Name:Test Cumulative Hybrid\nManaCost:1\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Cumulative upkeep:W/U\nOracle:test\n"
	e, cfg, bear := kr6FlexCumulativeEngine(t, cumulativeHybrid, nil)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the cumulative permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if n := len(untappedManaSourceIDs(e, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 has %d untapped mana sources, want 0", n)
	}

	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 {
		t.Fatalf("hybrid {W/U} faces = %v, want both announcement faces", faces)
	}
	submitChoices(t, e, 0) // elect white, then find no mana
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the cumulative election, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "cumulative_sac" {
		t.Fatalf("unpayable hybrid cumulative election = %+v, want sacrifice-only", d.Options)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("declined upkeep but the permanent is %+v", o)
	}
	if text := lastMoveZoneText(e, bear); text != "sacrificed for cumulative upkeep" {
		t.Fatalf("sacrifice move text = %q", text)
	}
	replayCheck(t, e, cfg)
}
