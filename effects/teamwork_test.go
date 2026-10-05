package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTeamworkCountAndFilterUsePaidProvenance(t *testing.T) {
	for _, key := range []string{"kw:Teamwork", "count:Teamwork"} {
		if !Supported()[key] {
			t.Fatalf("%s remains unsupported", key)
		}
	}
	h, c := fixtureHost(t)
	o := h.g.Obj(c.Source)
	if o == nil {
		t.Fatal("fixture source missing")
	}
	paid, unpaid := int32(2), int32(1)
	if paid == unpaid {
		t.Fatalf("precondition: Teamwork branch values are equal: paid=%d unpaid=%d", paid, unpaid)
	}
	if got := EvalCount(h, c, "Count$Teamwork.2.1"); got != unpaid {
		t.Fatalf("unpaid Count$Teamwork branch=%d, want %d", got, unpaid)
	}
	sc := SpecContext{You: 0, Source: c.Source}
	if matchesObjectText(h.g, "Card.Self+Teamwork", o, sc) {
		t.Fatal("unpaid source matched Card.Self+Teamwork")
	}
	// The paid marker is normally folded by events.Apply; the evaluator and
	// filter deliberately read the same durable provenance bit.
	o.CastFlags |= state.FlagTeamworkPaid
	o.TeamworkPaid = true
	if got := EvalCount(h, c, "Count$Teamwork.2.1"); got != 2 {
		t.Fatalf("paid Count$Teamwork branch=%d, want 2", got)
	}
	if !matchesObjectText(h.g, "Card.Self+Teamwork", o, sc) {
		t.Fatal("paid source did not match Card.Self+Teamwork")
	}
}
