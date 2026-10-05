package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestHollowMarauderCountRefs(t *testing.T) {
	h, c := fixtureHost(t)
	big := h.g.AddObject(mkCard(t, "Name:Big\nTypes:Creature\nManaCost:4\nPT:4/4\nOracle:x\n"), 0)
	small := h.g.AddObject(mkCard(t, "Name:Small\nTypes:Creature\nManaCost:2\nPT:2/2\nOracle:x\n"), 0)
	c.Targets = []state.Target{{Obj: big.ID}, {Player: 0, IsPlayer: true}, {Player: 1, IsPlayer: true}}
	c.Remembered = []state.Target{{Obj: big.ID}, {Obj: small.ID}}
	if big.Card.Faces[0].ManaValue() < 4 || small.Card.Faces[0].ManaValue() >= 4 {
		t.Fatal("precondition: remembered cards must straddle mana value 4")
	}
	for _, tc := range []struct {
		expr string
		want int32
	}{{"TargetedPlayer$Amount", 2}, {"ThisTargetedPlayer$Amount", 2}, {"TargetedPlayer$Amount/Minus.Remembered$Valid Card.cmcGE4", 1}} {
		if got := EvalCount(h, c, tc.expr); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, got, tc.want)
		}
	}
}

func TestHollowMarauderETBDrawsPerNonBigDiscard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Hollow Marauder")
	if !ok {
		t.Fatal("corpus has no Hollow Marauder")
	}
	if len(card.Faces) == 0 || !strings.EqualFold(card.Faces[0].SVars["Y"], "TargetedPlayer$Amount/Minus.Remembered$Valid Card.cmcGE4") {
		t.Fatalf("precondition: unexpected compiled Hollow Marauder Y: %q", card.Faces[0].SVars["Y"])
	}
	h := &fakeHost{g: state.NewGame(names(3))}
	big := h.g.AddObject(mkCard(t, "Name:Big\nTypes:Creature\nManaCost:4\nPT:4/4\nOracle:x\n"), 0)
	small := h.g.AddObject(mkCard(t, "Name:Small\nTypes:Creature\nManaCost:2\nPT:2/2\nOracle:x\n"), 0)
	ctx := &Ctx{Source: big.ID, Controller: 0, SVars: card.Faces[0].SVars,
		Targets:    []state.Target{{Player: 1, IsPlayer: true}, {Player: 2, IsPlayer: true}},
		Remembered: []state.Target{{Obj: big.ID}, {Obj: small.ID}}}
	if big.Card.Faces[0].ManaValue() < 4 || small.Card.Faces[0].ManaValue() >= 4 || len(ctx.Targets) == 0 || len(ctx.Remembered) != 2 {
		t.Fatal("precondition: two targets and remembered cards on both sides of mana value 4")
	}
	want := int32(len(ctx.Targets) - 1)
	if got := EvalCount(h, ctx, card.Faces[0].SVars["Y"]); got != want {
		t.Fatalf("Hollow Marauder compiled Y = %d, want %d draws for non-big discards", got, want)
	}
}
