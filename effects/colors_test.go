package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func objFrom(t *testing.T, src string) *state.Object {
	t.Helper()
	c, diags := cards.ParseBytes("x.txt", []byte(src))
	if len(diags) > 0 {
		t.Fatal(diags)
	}
	return &state.Object{ID: 1, Card: c}
}

func TestColorsOf(t *testing.T) {
	cases := []struct{ src, want string }{
		{"Name:Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n", "R"},
		{"Name:Dimir\nManaCost:U B\nTypes:Instant\nOracle:x\n", "UB"},
		{"Name:Colorless\nManaCost:3\nTypes:Artifact\nOracle:x\n", ""},
		{"Name:Colored by line\nManaCost:2\nTypes:Artifact\nColors:green\nOracle:x\n", "G"},
		{"Name:Breaker\nManaCost:6 G\nTypes:Creature Eldrazi\nK:Devoid\nOracle:x\n", ""},
		{"Name:Land\nManaCost:no cost\nTypes:Land\nOracle:x\n", ""},
		{"Name:Hybrid\nManaCost:W/U\nTypes:Instant\nOracle:x\n", "WU"},
	}
	for _, tc := range cases {
		if got := ColorsOf(objFrom(t, tc.src)); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.src[:20], got, tc.want)
		}
	}
	if ColorsOf(&state.Object{}) != "" || ColorsOf(nil) != "" {
		t.Fatal("faceless/nil object is not colourless")
	}
}

func BenchmarkColorsOf(b *testing.B) {
	object := func(mana, explicit string, keywords ...string) *state.Object {
		return &state.Object{Card: &cards.Card{Faces: []*cards.Face{{
			ManaCost: mana, Colors: explicit, Keywords: keywords,
		}}}}
	}
	objects := []*state.Object{
		object("W U B R G", ""),
		object("2 W/U B/R", ""),
		object("3", "green, blue"),
		object("6 G", "", "Devoid"),
		object("3", ""),
	}
	wantLen := len("WUBRG") + len("WUBR") + len("UG")
	b.ReportAllocs()
	b.ResetTimer()
	total := 0
	for range b.N {
		for _, o := range objects {
			total += len(ColorsOf(o))
		}
	}
	b.StopTimer()
	if total != b.N*wantLen {
		b.Fatalf("colour digest = %d, want %d", total, b.N*wantLen)
	}
}

func TestColorsOfDoesNotAllocateForImmutableFace(t *testing.T) {
	o := &state.Object{Card: &cards.Card{Faces: []*cards.Face{{
		ManaCost: "3", Colors: "green, blue",
	}}}}
	if got := ColorsOf(o); got != "UG" {
		t.Fatalf("ColorsOf = %q, want UG", got)
	}
	if allocs := allocsPerRun(1000, func() { ColorsOf(o) }); allocs != 0 {
		t.Fatalf("ColorsOf allocated %.2f objects/call, want zero", allocs)
	}
}
