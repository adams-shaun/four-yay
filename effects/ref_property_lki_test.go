package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestCardManaCostLKIReadsRememberedSnapshot(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	live := g.Obj(ids["myBear"])
	if live == nil || live.Face() == nil || live.Face().Cmc() == 0 {
		t.Fatal("expected a live referenced object with non-zero mana value")
	}
	wantLKI := live.Face().Cmc()
	snapshot := live.CloneDeep()
	current, diags := cards.ParseBytes("current.txt", []byte("Name:Changed Bear\nManaCost:6\nTypes:Creature\nPT:2/2\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parse current face: %v", diags)
	}
	current.Link()
	live.SetCard(current, live.FaceIdx)
	if live.Face().Cmc() == 0 || live.Face().Cmc() == wantLKI {
		t.Fatalf("setup failed: live mana value %d must be non-zero and differ from LKI %d", live.Face().Cmc(), wantLKI)
	}
	ctx := &Ctx{Source: ids["myBear"], Controller: 0,
		Remembered: []state.Target{{Obj: ids["myBear"]}}, LKI: &snapshot}
	if got := EvalCount(h, ctx, "TriggeredCard$CardManaCostLKI"); got != wantLKI {
		t.Fatalf("LKI mana value = %d, want snapshot value %d", got, wantLKI)
	}
	liveCtx := *ctx
	liveCtx.LKI = nil
	if got := EvalCount(h, &liveCtx, "TriggeredCard$CardManaCost"); got != live.Face().Cmc() {
		t.Fatalf("live mana value = %d, want current value %d", got, live.Face().Cmc())
	}
}
