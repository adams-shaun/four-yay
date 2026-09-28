package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestParseCostExileFromGraveX(t *testing.T) {
	t.Parallel()
	got := ParseCost("X T ExileFromGrave<X/Card>")
	if got.X != 1 || !got.Tap || got.Generic != 0 || len(got.Unknown) != 0 || len(got.Exile) != 1 {
		t.Fatalf("parsed cost = %+v", got)
	}
	part := got.Exile[0]
	if !part.Announced || part.N != 0 || part.Zone != state.ZGraveyard || part.Spec != "Card" {
		t.Fatalf("exile part = %+v", part)
	}
	fixed := ParseCost("ExileFromGrave<1/Card>")
	if len(fixed.Exile) != 1 || fixed.Exile[0].N != 1 || fixed.Exile[0].Announced || fixed.Exile[0].Zone != state.ZGraveyard {
		t.Fatalf("fixed cost changed: %+v", fixed)
	}
	folded := ParseCost("ExileFromGrave<X/Creature;Artifact>")
	if len(folded.Unknown) != 0 || len(folded.Exile) != 1 || folded.Exile[0].Spec != "Creature,Artifact" || !folded.Exile[0].Announced {
		t.Fatalf("comma-folded exile cost = %+v", folded)
	}
	combined := ParseCost("T").Plus(folded)
	if !combined.Tap || len(combined.Exile) != 1 || !combined.Exile[0].Announced {
		t.Fatalf("Plus lost the announced cost: %+v", combined)
	}
	if got := formatCost(got); got != "X T ExileFromGrave<X/Card>" {
		t.Fatalf("X exile cost renders %q", got)
	}
	// The alternate-cost boundary remains explicit: only the ordinary
	// FromGrave X token is parsed. Other X exile zones need their own payer.
	for _, s := range []string{"ExileFromHand<X/Card>", "ExileAnyGrave<X/Card>"} {
		c := ParseCost(s)
		if len(c.Exile) != 0 || len(c.Unknown) == 0 {
			t.Fatalf("unsupported X exile token %s = %+v", s, c)
		}
	}
}
