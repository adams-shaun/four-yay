package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestExactlySearchAllowsDecliningButNotAPartialFind pins the Exactly$ True
// ask shape on Extrapolate the Impossible ("You may reveal exactly two cards
// you own with different names from outside the game"): the reveal is
// optional, so the empty answer is legal (Decision.AllowNone), and it is
// exactly two, so a one-card answer is refused (Min == Max == 2). Before
// AllowNone the quantity-only search forced Min = Max = 2 with no decline.
func TestExactlySearchAllowsDecliningButNotAPartialFind(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sc := oracleScenario{
		Setup: map[string]oracleSeat{"p0": {Hand: []string{"Extrapolate the Impossible"},
			Sideboard: []string{"Lightning Bolt", "Grizzly Bears"}}},
		Steps: []oracleStep{{Op: "cast", Seat: 0, Card: "p0:Extrapolate the Impossible", Mana: "CB"}},
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) > 0 || run.e == nil {
		t.Fatalf("%v\ntranscript:\n%v", fails, transcript)
	}
	e := run.e
	var d *decision.Decision
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		submitChoicePass(t, e)
	}
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("reveal ask = %+v, want the search choose", d)
	}
	if d.Min != 2 || d.Max != 2 || !d.AllowNone {
		t.Fatalf("reveal ask bounds min=%d max=%d allowNone=%v, want exactly 2 or none", d.Min, d.Max, d.AllowNone)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err == nil {
		t.Fatalf("a one-card reveal validated; Exactly$ True admits exactly two or none")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("the two-card reveal is refused: %v", err)
	}
	// Decline: nothing is revealed, so the opponent picks nothing and the
	// hand stays empty; both cards stay outside the game.
	submitChoices(t, e)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
		t.Fatalf("hand size after declining = %d, want 0", got)
	}
	if got := len(e.G.Zone(state.ZSideboard, 0)); got != 2 {
		t.Fatalf("sideboard size after declining = %d, want 2", got)
	}
	replayCheck(t, e, run.cfg)
}
