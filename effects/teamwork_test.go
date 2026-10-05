package effects

import (
	"testing"
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
	// Model the durable field folded by events.Apply. CastFlags can be
	// replaced by a later CastInfo, so it is not the filter's provenance home.
	o.TeamworkPaid = true
	if got := EvalCount(h, c, "Count$Teamwork.2.1"); got != 2 {
		t.Fatalf("paid Count$Teamwork branch=%d, want 2", got)
	}
	if !matchesObjectText(h.g, "Card.Self+Teamwork", o, sc) {
		t.Fatal("paid source did not match Card.Self+Teamwork")
	}
}
