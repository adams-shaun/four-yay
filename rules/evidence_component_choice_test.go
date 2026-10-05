package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const evidenceMultiComponent = "Name:Evidence Multi Component\nManaCost:2 U\nTypes:Creature Wizard\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost\n" +
	"SVar:TrigCost:AB$ Draw | Cost$ ExileAnyGrave<2/Card> CollectEvidence<4> | NumCards$ 1\nOracle:x\n"

// A two-card component must not offer a combination that consumes both
// evidence-enabling cards. The old N==1-only filtering offered both MV-4
// cards alongside the two MV-0 cards, so choosing the two high cards caused
// the already-paid trigger to decline before evidence could be collected.
func TestEvidenceTriggerMultiCardComponentPreservesEvidence(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	four := card(t, "Name:Evidence Four\nManaCost:4\nTypes:Artifact\nOracle:x\n")
	zero := card(t, "Name:Evidence Zero\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	high1 := evidenceGraveCard(t, e, four)
	high2 := evidenceGraveCard(t, e, four)
	low1 := evidenceGraveCard(t, e, zero)
	low2 := evidenceGraveCard(t, e, zero)
	if e.G.Obj(high1).Zone != state.ZGraveyard || e.G.Obj(high2).Zone != state.ZGraveyard ||
		e.G.Obj(low1).Zone != state.ZGraveyard || e.G.Obj(low2).Zone != state.ZGraveyard {
		t.Fatal("all four component candidates must be in the payer's graveyard")
	}
	if e.G.Obj(high1).Face().Cmc() <= e.G.Obj(low1).Face().Cmc() ||
		evidenceManaValue(e.G, 0, []state.ObjID{high1, high2, low1, low2}) != 8 {
		t.Fatal("fixture must contain two MV-4 and two MV-0 candidates")
	}

	d := etbCostWindow(t, e, evidenceMultiComponent)
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("ExileAnyGrave<2> + CollectEvidence<4> not payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) < 2 || d.Options[0].Kind != "exile_cost" {
		t.Fatalf("component ask = %+v, want at least two legal exile options", d)
	}
	// Any two options in this filtered set must retain total MV >= 4.
	for i := 0; i < len(d.Options); i++ {
		for j := i + 1; j < len(d.Options); j++ {
			reserved := map[state.ObjID]bool{d.Options[i].Obj: true, d.Options[j].Obj: true}
			if !evidenceCanReach(e.G, 0, 4, reserved) {
				t.Fatalf("offered component pair %d,%d leaves insufficient evidence", d.Options[i].Obj, d.Options[j].Obj)
			}
		}
	}
	// Exercise the bot's first two options. These must be a valid component
	// answer and leave the evidence stage live rather than declining the body.
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("component selection bounds Min=%d Max=%d, want exactly 2", d.Min, d.Max)
	}
	if d.Options[0].Kind != "exile_cost" || d.Options[1].Kind != "exile_cost" {
		t.Fatalf("unexpected component option kinds: %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	pending := e.Pending()
	if pending == nil || pending.Kind != decision.KChoose || len(pending.Options) == 0 || pending.Options[0].Kind != "evidence" {
		t.Fatalf("valid multi-card answer did not reach evidence stage; pending=%+v", pending)
	}
	if len(evidenceEvents(e, mark)) != 0 {
		t.Fatal("components settled before evidence selection")
	}

	// Confirm at least one excluded answer would have consumed the enabling
	// cards; this makes the regression setup itself meaningful.
	if evidenceCanReach(e.G, 0, 4, map[state.ObjID]bool{high1: true, high2: true}) {
		t.Fatal("fixture must make evidence impossible after reserving both high cards")
	}
}
