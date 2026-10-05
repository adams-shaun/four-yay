package effects

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestNumberPow reads Mathemagics' printed X through the SVar operand path,
// rather than treating /Pow.X as an unknown suffix or a fixed number of draws.
func TestNumberPow(t *testing.T) {
	h, c := fixtureHost(t)
	c.SVars = map[string]string{"X": "Count$xPaid", "Y": "Number$2/Pow.X"}
	for _, tc := range []struct{ x, want int32 }{{0, 1}, {1, 2}, {3, 8}, {30, 1 << 30}, {31, math.MaxInt32}} {
		c.X = tc.x
		x, xOK := EvalCountOK(h, c, c.SVars["X"])
		if !xOK || x != tc.x {
			t.Fatalf("precondition: paid X = (%d,%v), want %d", x, xOK, tc.x)
		}
		n, ok := EvalCountOK(h, c, c.SVars["Y"])
		if !ok || n != tc.want {
			t.Errorf("X=%d: Number$2/Pow.X = (%d,%v), want (%d,true)", tc.x, n, ok, tc.want)
		}
		// Exercise Draw's NumCards$ Y boundary as well as the count leaf.
		draw := &cards.SA{API: "Draw", Params: map[string]string{"NumCards": "Y"}}
		if got := Num(h, c, draw, "NumCards", 1); got != tc.want {
			t.Errorf("X=%d: Draw NumCards$ Y = %d, want %d", tc.x, got, tc.want)
		}
	}
	for _, exp := range []string{"UnrecognisedCountHead", "Number$-1"} {
		c.SVars["X"] = exp
		if n, ok := EvalCountOK(h, c, c.SVars["Y"]); ok || n != 0 {
			t.Errorf("exponent %q = (%d,%v), want (0,false)", exp, n, ok)
		}
	}
	if n, ok := EvalCountOK(h, c, "Number$2/Pow.-1"); ok || n != 0 {
		t.Errorf("negative literal exponent = (%d,%v), want (0,false)", n, ok)
	}
}

// The Forge pin currently has exactly one Pow operator carrier. A new carrier
// must get reviewed instead of inheriting unverified numeric semantics.
func TestNumberPowCorpusCarriers(t *testing.T) {
	testutil.CorpusRegistry(t) // an absent corpus must SKIP, not pass vacuously
	var carriers []string
	root := filepath.Join("..", ".cards", "cardsfolder")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "/Pow.") {
				rel, _ := filepath.Rel(root, path)
				carriers = append(carriers, rel+": "+line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(carriers)
	want := []string{"m/mathemagics.txt: SVar:Y:Number$2/Pow.X"}
	if len(carriers) != len(want) {
		t.Fatalf("Pow carriers = %v, want %v", carriers, want)
	}
	for i := range want {
		if carriers[i] != want[i] {
			t.Fatalf("Pow carriers = %v, want %v", carriers, want)
		}
	}
}
