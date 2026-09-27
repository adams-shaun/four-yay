package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// paid_cost_ref_test.go pins the evaluator half of the cast-cost PAID-list
// references: the `Exiled$<Property>` / `Revealed$<Property>` count bodies and
// the bare `Defined$ Exiled` / `Revealed` selectors read Ctx.Exiled /
// Ctx.Revealed -- the cards a cast's own cost exiled or revealed (Forge's
// AbilityUtils.getPaidCards -> SpellAbility.getPaidList, CostExile's "Exiled"
// row and CostReveal's "Revealed" row). The end-to-end cast flow is pinned in
// rules/cost_paid_exiled_revealed_test.go; these tests pin the property
// dispatch (CardPower/CardManaCost/Valid) and the fail-closed empty binding.

// TestPaidCostExiledAndRevealedValidCountThePaidCards pins the `Valid`
// property over both paid lists: the count of paid cards matching a card spec,
// for the corpus spellings `X:Exiled$Valid Land.Snow` (Storm Elemental) and
// `X:Exiled$Valid Thrull` (Soul Exchange), plus the `Revealed` sibling.
func TestPaidCostExiledAndRevealedValidCountThePaidCards(t *testing.T) {
	h, c := fixtureHost(t)
	snow := h.g.AddObject(mkCard(t, "Name:Snowfield\nTypes:Snow Land\nOracle:x\n"), 0).ID
	creature := h.g.AddObject(mkCard(t, "Name:PaidCrawler\nManaCost:3\nTypes:Creature Thrull\nPT:4/4\nOracle:x\n"), 0).ID
	plain := h.g.AddObject(mkCard(t, "Name:PaidRock\nManaCost:2\nTypes:Artifact\nOracle:x\n"), 0).ID
	// Precondition: the paid cards really differ in the properties asserted.
	if !MatchesObjectCtx(h.g, "Land.Snow", h.g.Obj(snow), c.SpecContext(c.Controller)) {
		t.Fatal("precondition: snow land does not match Land.Snow")
	}
	if MatchesObjectCtx(h.g, "Thrull", h.g.Obj(snow), c.SpecContext(c.Controller)) {
		t.Fatal("precondition: snow land must not match Thrull")
	}
	c.Exiled = []state.ObjID{snow, creature, plain}
	c.Revealed = []state.ObjID{creature, plain}

	if got := EvalCount(h, c, "Exiled$Valid Land.Snow"); got != 1 {
		t.Errorf("Exiled$Valid Land.Snow = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Exiled$Valid Thrull"); got != 1 {
		t.Errorf("Exiled$Valid Thrull = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Exiled$Amount"); got != 3 {
		t.Errorf("Exiled$Amount = %d, want 3", got)
	}
	if got := EvalCount(h, c, "Revealed$Valid Thrull"); got != 1 {
		t.Errorf("Revealed$Valid Thrull = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Revealed$Valid Artifact"); got != 1 {
		t.Errorf("Revealed$Valid Artifact = %d, want 1", got)
	}
}

// TestPaidCostRefsReadThePaidCardsNotTheSource pins CardManaCost and CardPower
// over both lists against a source whose own characteristics differ, so a
// fallback to the source would be caught.
func TestPaidCostRefsReadThePaidCardsNotTheSource(t *testing.T) {
	h, c := fixtureHost(t)
	src := h.g.Obj(c.Source)
	paid := h.g.AddObject(mkCard(t, "Name:PaidValue\nManaCost:5 R\nTypes:Creature Beast\nPT:7/3\nOracle:x\n"), 0).ID
	if got := h.g.Obj(paid).Face().ManaValue(); got != 6 {
		t.Fatalf("precondition: paid card MV = %d, want 6", got)
	}
	if int32(h.g.Obj(paid).Face().Power()) != 7 {
		t.Fatalf("precondition: paid card power = %d, want 7", h.g.Obj(paid).Face().Power())
	}
	if int32(h.g.Obj(paid).Face().Power()) == int32(src.Face().Power()) {
		t.Fatal("precondition: paid card and source must differ in power")
	}
	c.Exiled = []state.ObjID{paid}
	c.Revealed = []state.ObjID{paid}
	if got := EvalCount(h, c, "Exiled$CardManaCost"); got != 6 {
		t.Errorf("Exiled$CardManaCost = %d, want 6", got)
	}
	if got := EvalCount(h, c, "Exiled$CardPower"); got != 7 {
		t.Errorf("Exiled$CardPower = %d, want 7", got)
	}
	if got := EvalCount(h, c, "Revealed$CardManaCost"); got != 6 {
		t.Errorf("Revealed$CardManaCost = %d, want 6", got)
	}
	if got := EvalCount(h, c, "Revealed$CardPower"); got != 7 {
		t.Errorf("Revealed$CardPower = %d, want 7", got)
	}
}

// TestPaidCostRefsFailClosedWithoutAPaidList pins the absent binding: an empty
// Ctx.Exiled / Ctx.Revealed is a legitimate zero, never a fallback to the
// resolving source (whose own mana value is nonzero here) or the chosen
// targets.
func TestPaidCostRefsFailClosedWithoutAPaidList(t *testing.T) {
	h, c := fixtureHost(t)
	src := h.g.AddObject(mkCard(t, "Name:PaidlessSource\nManaCost:4 U\nTypes:Creature\nPT:3/3\nOracle:x\n"), 0).ID
	c.Source = src
	c.Targets = []state.Target{{Obj: src}}
	// Precondition: the source, which is also the chosen target, has a
	// nonzero mana value and power, so any fallback would be detectable.
	if h.g.Obj(src).Face().ManaValue() == 0 {
		t.Fatal("precondition: source must have a nonzero mana value")
	}
	if int32(h.g.Obj(src).Face().Power()) == 0 {
		t.Fatal("precondition: source must have a nonzero power")
	}
	for _, body := range []string{"Exiled$CardManaCost", "Exiled$CardPower", "Exiled$Amount", "Revealed$CardManaCost", "Revealed$CardPower", "Revealed$Amount"} {
		if got := EvalCount(h, c, body); got != 0 {
			t.Errorf("%s without a paid list = %d, want 0", body, got)
		}
	}
}

// TestDefinedPaidCostSelectorsReadThePaidList pins the bare Defined-position
// selectors through the same binding: `Defined$ Exiled` / `Revealed` name the
// paid cards (ok=true), and an absent paid list is a known-empty pool, never
// the chosen targets.
func TestDefinedPaidCostSelectorsReadThePaidList(t *testing.T) {
	h, c := fixtureHost(t)
	paid := h.g.AddObject(mkCard(t, "Name:PaidOne\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID
	other := h.g.AddObject(mkCard(t, "Name:PaidTwo\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID
	c.Exiled = []state.ObjID{paid, other}
	ts, ok := definedSpec(h, c, "Exiled")
	if !ok || len(ts) != 2 || ts[0].Obj != paid || ts[1].Obj != other {
		t.Fatalf("definedSpec Exiled = %v ok=%v, want [%d %d]", ts, ok, paid, other)
	}
	c.Revealed = []state.ObjID{other}
	ts, ok = definedSpec(h, c, "Revealed")
	if !ok || len(ts) != 1 || ts[0].Obj != other {
		t.Fatalf("definedSpec Revealed = %v ok=%v, want [%d]", ts, ok, other)
	}
	// Absent paid list: known-empty (ok=true), never the targets.
	c.Targets = []state.Target{{Obj: paid}}
	c.Exiled = nil
	ts, ok = definedSpec(h, c, "Exiled")
	if !ok || len(ts) != 0 {
		t.Fatalf("definedSpec Exiled with no paid list = %v ok=%v, want empty ok", ts, ok)
	}
}

// TestPaidCostRefsReadTheRealCorpusSVars drives the REAL compiled spellings
// through the evaluator, so a reviewer reverting the ref case cannot leave a
// synthetic-only test green: Charge of the Forever-Beast's
// `X:Revealed$CardPower`, Draconic Intervention's `X:Exiled$CardManaCost`, and
// Storm Elemental's `X:Exiled$Valid Land.Snow`.
func TestPaidCostRefsReadTheRealCorpusSVars(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type probe struct {
		card string
		body string
		paid []string // card sources to bind as the paid list
		want int32
	}
	beast := "Name:RevealedBeast\nManaCost:3 G\nTypes:Creature Beast\nPT:5/4\nOracle:x\n"
	bolt := "Name:PaidBolt\nManaCost:R\nTypes:Instant\nOracle:x\n"
	snowland := "Name:Snowfield\nTypes:Snow Land\nOracle:x\n"
	for _, tc := range []probe{
		{"Charge of the Forever-Beast", "Revealed$CardPower", []string{beast}, 5},
		{"Draconic Intervention", "Exiled$CardManaCost", []string{bolt}, 1},
		{"Storm Elemental", "Exiled$Valid Land.Snow", []string{snowland}, 1},
	} {
		card, ok := reg.Lookup(tc.card)
		if !ok {
			t.Fatalf("corpus missing %s", tc.card)
		}
		if got := card.Faces[0].SVars["X"]; got != tc.body {
			t.Fatalf("%s SVar X = %q, want %q", tc.card, got, tc.body)
		}
		h, c := fixtureHost(t)
		ids := make([]state.ObjID, 0, len(tc.paid))
		for _, src := range tc.paid {
			ids = append(ids, h.g.AddObject(mkCard(t, src), 0).ID)
		}
		if len(ids) == 1 {
			o := h.g.Obj(ids[0])
			// Precondition: the paid card actually carries the property the
			// body reads, and it is nonzero.
			if tc.want == 5 && int32(o.Face().Power()) != 5 {
				t.Fatalf("precondition: %s power = %d, want 5", tc.card, o.Face().Power())
			}
			if tc.want == 1 && tc.body == "Exiled$CardManaCost" && o.Face().ManaValue() != 1 {
				t.Fatalf("precondition: %s paid MV = %d, want 1", tc.card, o.Face().ManaValue())
			}
			if tc.want == 1 && tc.body == "Exiled$Valid Land.Snow" &&
				!MatchesObjectCtx(h.g, "Land.Snow", o, c.SpecContext(c.Controller)) {
				t.Fatal("precondition: snow land does not match Land.Snow")
			}
		}
		if strings.HasPrefix(tc.body, "Revealed") {
			c.Revealed = ids
		} else {
			c.Exiled = ids
		}
		if got := EvalCount(h, c, tc.body); got != tc.want {
			t.Errorf("real %s SVar = %d, want %d", tc.card, got, tc.want)
		}
	}
}
