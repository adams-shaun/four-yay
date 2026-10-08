package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"slices"
	"testing"
)

func TestFlipCoinModCarrierCensus(t *testing.T) {
	if !effects.Supported()["stat:FlipCoinMod"] {
		t.Fatal("FlipCoinMod static unsupported")
	}
	reg := testutil.CorpusRegistry(t)
	var got []string
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f != nil && slices.Contains(f.Primitives(), "stat:FlipCoinMod") {
				got = append(got, f.Name)
			}
		}
	}
	if !slices.Equal(got, []string{"Edgar, King of Figaro"}) {
		t.Fatalf("FlipCoinMod carriers = %q", got)
	}
}
