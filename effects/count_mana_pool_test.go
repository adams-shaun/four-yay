package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestManaPoolCountCorpusForms(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	want := map[string]string{"Ozai, the Phoenix King": "All", "Omnath, Locus of the Void": "All", "Glissa Sunseeker": "All", "Omnath, Locus of Mana": "green"}
	for name, arg := range want {
		card, ok := reg.Lookup(name)
		if !ok || len(card.Faces) == 0 {
			t.Fatalf("corpus card %q missing", name)
		}
		body, ok := card.Faces[0].SVars["X"]
		if !ok || body != "Count$ManaPool:"+arg {
			t.Fatalf("%s X = %q, want Count$ManaPool:%s", name, body, arg)
		}
	}
	g, _ := board(t)
	g.Players[0].Pool[state.MR] = 3
	g.Players[0].Pool[state.MG] = 2
	h := &fakeHost{g: g}
	ctx := &Ctx{Controller: 0}
	for _, tc := range []struct {
		arg  string
		want int32
	}{{"All", 5}, {"green", 2}} {
		got, ok := EvalCountOK(h, ctx, "Count$ManaPool:"+tc.arg)
		if !ok || got != tc.want {
			t.Errorf("Count$ManaPool:%s = %d, %v; want %d, true", tc.arg, got, ok, tc.want)
		}
	}
}
