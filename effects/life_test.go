package effects

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestLoseLifePublishesAFLifeLost(t *testing.T) {
	for _, tc := range []struct {
		name, defined string
		amount, total int32
		p0, p1        int32
	}{
		{"opponent", "Player.Opponent", 2, 2, 20, 18},
		{"all players", "Player", 4, 8, 16, 16},
		{"zero overwrites earlier loss", "Player.Opponent", 0, 0, 20, 18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, c := fixtureHost(t)
			if o := h.g.Obj(c.Source); o == nil || o.Zone != state.ZLibrary {
				t.Fatal("precondition: source object must exist in fixture library")
			}
			if h.g.Obj(c.Source).RuntimeSVars != nil {
				t.Fatal("precondition: source already has runtime SVars")
			}
			if h.g.Players[0].Life != 20 || h.g.Players[1].Life != 20 {
				t.Fatal("precondition: starting life must be 20 for both seats")
			}
			c.SVars = map[string]string{"AFLifeLost": "Number$0"}
			if tc.amount == 0 {
				Resolve(h, c, sa(t, "DB$ LoseLife | Defined$ Player.Opponent | LifeAmount$ 2"))
				if got, ok := runtimeSVar(c, "AFLifeLost"); !ok || got != 2 {
					t.Fatalf("precondition: earlier loss = %d, present=%v; want 2", got, ok)
				}
			}
			Resolve(h, c, sa(t, "DB$ LoseLife | Defined$ "+tc.defined+" | LifeAmount$ "+strconv.Itoa(int(tc.amount))))
			if got, ok := runtimeSVar(c, "AFLifeLost"); !ok || got != tc.total {
				t.Fatalf("runtime AFLifeLost = %d, present=%v; want %d", got, ok, tc.total)
			}
			if got := Num(h, c, sa(t, "DB$ GainLife | LifeAmount$ AFLifeLost"), "LifeAmount", -1); got != tc.total {
				t.Fatalf("printed-default-shadowed LifeAmount = %d, want %d", got, tc.total)
			}
			if got := h.g.Players[0].Life; got != tc.p0 {
				t.Errorf("p0 life = %d, want %d", got, tc.p0)
			}
			if got := h.g.Players[1].Life; got != tc.p1 {
				t.Errorf("p1 life = %d, want %d", got, tc.p1)
			}
		})
	}
}
