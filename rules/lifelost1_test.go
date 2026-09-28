package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Exsanguinate's X loss and subsequent gain must read the same source's
// AFLifeLost, rather than the printed Number$0 fallback.
func TestExsanguinateGainsLifeLostThisWay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Exsanguinate")}, nil)
	id := moveByName(t, e, 0, "Exsanguinate", state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand || o.Face().Name != "Exsanguinate" {
		t.Fatal("precondition: real Exsanguinate must be in hand")
	}
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: life totals = %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	addMana(t, e, 0, "CCBB") // {X}{X}{B}{B}, X = 2
	opt := castByName(t, e, 0, "Exsanguinate")
	if opt == nil || opt.Obj != id {
		t.Fatal("precondition: Exsanguinate cast option missing")
	}
	submitChoices(t, e, opt.Index)
	announceXCast(t, e, id, 2)
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("precondition: spell did not resolve: zone %s", e.G.Obj(id).Zone)
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("opponent life = %d, want 18 (loss must occur)", got)
	}
	if got := e.G.Players[0].Life; got != 22 {
		t.Fatalf("caster life = %d, want 22 (not printed default 20)", got)
	}
}

// CR 119.3 adjusts each player's life; CR 700.5 counts the entering Gray
// Merchant's own two black symbols for devotion. Its follow-on gains the
// amount lost by the opponent, not zero from the printed AFLifeLost default.
func TestCR119LifeLostThisWayGainsThatMuch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	file := loadOracleFiles(t)["testdata/oracle/conditional-static/gray-merchant-of-asphodel.json"]
	if file.Card != "Gray Merchant of Asphodel" {
		t.Fatalf("precondition: wrong oracle fixture %q", file.Card)
	}
	for _, sc := range file.Scenarios {
		if sc.Name != "self-devotion-two-life-gain" {
			continue
		}
		fails, _, run := runOracleScenario(reg, sc)
		if run.e == nil || len(run.e.G.Players) != 2 {
			t.Fatal("precondition: two-seat scenario did not start")
		}
		id := run.refs["p0:Gray Merchant of Asphodel"]
		if id == 0 || run.e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatal("precondition: Gray Merchant did not enter the battlefield")
		}
		if got := run.e.G.Players[1].Life; got != 18 {
			t.Fatalf("precondition: opponent lost two life, got %d", got)
		}
		if len(fails) != 0 || run.e.G.Players[0].Life != 22 {
			t.Fatalf("Gray Merchant controller life = %d, want 22 (not starting 20); scenario failures: %v", run.e.G.Players[0].Life, fails)
		}
		return
	}
	t.Fatal("precondition: Gray Merchant life-gain scenario missing")
}
