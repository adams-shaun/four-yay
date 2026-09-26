package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// exile_from_top_cost_test.go pins ExileFromTop<N/Card> as a normal
// cast/activation cost end to end: the token prices with no generic
// substitution and no Unknown census entry, the payment exiles the ACTUAL top
// N cards of the payer's library in order (never a chooser over deeper cards),
// and those cards feed the `Exiled$<Property>` paid list the ability body
// reads. Before this work the head hit ParseCost's final unrecognised-symbol
// fallback: a phantom {1} rode the price (Storm Elemental's {U} activation
// demanded {U}{1}) and no card was exiled, so `X:Exiled$Valid Land.Snow`
// always read zero.
//
// No Forge script text is committed here; every card comes from the compiled
// .cards corpus.

// exileFromTopOrderLibrary moves ids to the top of p's library in order
// through one real LibraryOrder event (replayable, unlike a direct SetZone),
// leaving every other library card beneath in its existing order.
func exileFromTopOrderLibrary(t *testing.T, e *Engine, p state.PlayerID, ids ...state.ObjID) {
	t.Helper()
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...)
	top := map[state.ObjID]bool{}
	for _, id := range ids {
		if !top[id] {
			top[id] = true
		}
	}
	ordered := append([]state.ObjID(nil), ids...)
	for _, id := range lib {
		if !top[id] {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) != len(lib) {
		t.Fatalf("LibraryOrder would lose cards: %d -> %d", len(lib), len(ordered))
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: ordered})
}

// exileFromTopAbilityOption returns the index of the activated-ability option
// whose label contains want, or fails.
func exileFromTopAbilityOption(t *testing.T, e *Engine, obj state.ObjID, want string) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if (o.Kind == "ability" || o.Kind == "activate") && o.Obj == obj && strings.Contains(o.Label, want) {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no ability %q for %d among %+v", want, obj, d.Options)
	}
	return idx
}

// TestExileFromTopCostParse is the grammar leaf: ExileFromTop<N/Card> prices
// with no generic substitution and no Unknown entry, stays in its dedicated
// top-library cost slice, round-trips through formatCost, and a spec other than
// the measured "Card" is left unmodelled (never a deeper-card filter).
func TestExileFromTopCostParse(t *testing.T) {
	c := ParseCost("U ExileFromTop<1/Card>")
	if c.Colored[state.MU] != 1 || c.Generic != 0 {
		t.Fatalf("ParseCost(\"U ExileFromTop<1/Card>\") = %+v, want one blue and no generic", c)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("ExileFromTop<1/Card> reported Unknown: %v", c.Unknown)
	}
	if len(c.ExileFromTop) != 1 {
		t.Fatalf("ExileFromTop parts = %+v, want 1", c.ExileFromTop)
	}
	part := c.ExileFromTop[0]
	if part.N != 1 || part.Spec != "Card" {
		t.Fatalf("ExileFromTop part = %+v, want {N:1 Spec:Card}", part)
	}
	if len(c.Exile) != 0 {
		t.Fatalf("ExileFromTop leaked into Cost.Exile (the hand/grave slice): %+v", c.Exile)
	}
	out := formatCost(c)
	if !strings.Contains(out, "ExileFromTop<1/Card>") {
		t.Fatalf("formatCost(%q) = %q, want the ExileFromTop origin preserved", "U ExileFromTop<1/Card>", out)
	}
	back := ParseCost(out)
	if back.Generic != 0 || len(back.Unknown) != 0 || len(back.ExileFromTop) != 1 || back.ExileFromTop[0].N != 1 || len(back.Exile) != 0 {
		t.Fatalf("round trip of %q = %+v", out, back)
	}
	if m := c.Generic; m != 0 {
		t.Fatalf("generic = %d, want 0", m)
	}

	// A non-"Card" spec is not modelled: it must NOT become a deeper-library
	// filter (the library is ordered, and the position is the cost's meaning).
	bad := ParseCost("ExileFromTop<1/Land>")
	if bad.Generic != 1 || len(bad.ExileFromTop) != 0 {
		t.Fatalf("ParseCost(ExileFromTop<1/Land>) = %+v, want one generic and no ExileFromTop part", bad)
	}
	if len(bad.Unknown) == 0 || bad.Unknown[0] != "ExileFromTop" {
		t.Fatalf("non-Card spec Unknown = %v, want [ExileFromTop]", bad.Unknown)
	}
}

func TestExileFromTopPartsUseOneCurrentPrefix(t *testing.T) {
	parts := []CostPart{{N: 2, Spec: "Card"}, {N: 2, Spec: "Card"}}
	old := []state.ObjID{6, 11, 8, 21}
	got, ok := exileFromTopCards(old, parts)
	if !ok || len(got) != 4 || got[0] != 6 || got[1] != 11 || got[2] != 8 || got[3] != 21 {
		t.Fatalf("two top-two parts selected %v, ok=%v; want distinct aggregate prefix %v", got, ok, old)
	}
	if _, ok := exileFromTopCards(old[:2], parts); ok {
		t.Fatal("two top-two parts were payable from only two cards")
	}
	// Payment resolves against the library's post-mana-window state, not IDs
	// captured before a mana ability draws or reorders it.
	current := []state.ObjID{31, 32, 33}
	got, ok = exileFromTopCards(current, []CostPart{{N: 1, Spec: "Card"}})
	if !ok || len(got) != 1 || got[0] != current[0] {
		t.Fatalf("settled top after library change = %v, ok=%v; want current top %d", got, ok, current[0])
	}
}

// TestStormElementalExileFromTopCost pins the real carrier: the {U} activation
// exiles the actual top card of the payer's library, charges no extra generic,
// and its body's `X:Exiled$Valid Land.Snow` gives +1/+1 exactly when that top
// card is a snow land.
func TestStormElementalExileFromTopCost(t *testing.T) {
	// Snow case: the top of library is a snow land.
	e, cfg := paidCostEngine(t, []string{"Storm Elemental", "Snow-Covered Island"}, nil)
	storm := paidCostMoveTo(t, e, 0, "Storm Elemental", state.ZBattlefield)
	snow := paidCostMoveTo(t, e, 0, "Snow-Covered Island", state.ZLibrary)
	// Precondition: the snow land really is a snow land AND a land, and it is
	// the actual top of the library.
	if f := e.G.Obj(snow).Face(); !hasTypeWord(f.Types, "Snow") || !hasTypeWord(f.Types, "Land") {
		t.Fatalf("precondition: %s types=%v, want a snow land", f.Name, f.Types)
	}
	exileFromTopOrderLibrary(t, e, 0, snow)
	if top := e.G.Zone(state.ZLibrary, 0)[0]; top != snow {
		t.Fatalf("precondition: library top = %d, want snow land %d", top, snow)
	}
	before := e.Power(storm)
	addMana(t, e, 0, "U")
	idx := exileFromTopAbilityOption(t, e, storm, "snow land")
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(snow); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the top snow land was not exiled: %+v", o)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("activation left %d mana in the pool, want 0 (no spurious generic)", got)
	}
	if got := e.Power(storm); got != before+1 {
		t.Fatalf("Storm Elemental power = %d, want %d (Exiled$Valid Land.Snow matched the exiled top card)", got, before+1)
	}
	replayCheck(t, e, cfg)

	// Control: the top of library is a non-snow land. The same activation must
	// exile it and give NO bonus, proving the +1/+1 above came from matching
	// the paid card, not from the activation itself.
	e2, cfg2 := paidCostEngine(t, []string{"Storm Elemental", "Island"}, nil)
	storm2 := paidCostMoveTo(t, e2, 0, "Storm Elemental", state.ZBattlefield)
	plain := paidCostMoveTo(t, e2, 0, "Island", state.ZLibrary)
	if f := e2.G.Obj(plain).Face(); !hasTypeWord(f.Types, "Land") || hasTypeWord(f.Types, "Snow") {
		t.Fatalf("precondition control: %s types=%v, want a non-snow land", f.Name, f.Types)
	}
	exileFromTopOrderLibrary(t, e2, 0, plain)
	if top := e2.G.Zone(state.ZLibrary, 0)[0]; top != plain {
		t.Fatalf("precondition control: library top = %d, want Island %d", top, plain)
	}
	before2 := e.Power(storm2)
	addMana(t, e2, 0, "U")
	idx = exileFromTopAbilityOption(t, e2, storm2, "snow land")
	submitChoices(t, e2, idx)
	passUntilStackEmpty(t, e2, 20)
	if o := e2.G.Obj(plain); o == nil || o.Zone != state.ZExile {
		t.Fatalf("control: the top Island was not exiled: %+v", o)
	}
	if got := e.Power(storm2); got != before2 {
		t.Fatalf("control: Storm Elemental power = %d, want %d (non-snow does not match)", got, before2)
	}
	replayCheck(t, e2, cfg2)
}

// TestExileFromTopMultiCardCost pins the N>1 forms (Arc-Slogger's ExileFromTop
// <10/Card>, Whirling Catapult's <2/Card>): exactly the top N cards in library
// order are paid, no chooser is posed, and an insufficient library withholds
// the activation entirely.
func TestExileFromTopMultiCardCost(t *testing.T) {
	e, cfg := paidCostEngine(t, []string{"Whirling Catapult", "Grizzly Bears"}, nil)
	cat := paidCostMoveTo(t, e, 0, "Whirling Catapult", state.ZBattlefield)
	// Put two named cards on top in a known order.
	bears := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZLibrary)
	forest := paidCostMoveTo(t, e, 0, "Forest", state.ZLibrary)
	// paidCostMoveTo leaves them wherever the Move placed them; force the
	// actual top two to be bears then forest.
	exileFromTopOrderLibrary(t, e, 0, bears, forest)
	lib := e.G.Zone(state.ZLibrary, 0)
	if lib[0] != bears || lib[1] != forest {
		t.Fatalf("precondition: library top two = %d,%d, want %d,%d", lib[0], lib[1], bears, forest)
	}
	addMana(t, e, 0, "GG") // two green pays Whirling Catapult's generic {2}
	mark := len(e.L.Events)
	idx := exileFromTopAbilityOption(t, e, cat, "")
	submitChoices(t, e, idx)
	// The payment stage poses no chooser for a top-of-library cost.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("top-of-library payment posed a chooser: %+v", d)
	}
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZExile {
		t.Fatalf("arc: first top card not exiled: %+v", o)
	}
	if o := e.G.Obj(forest); o == nil || o.Zone != state.ZExile {
		t.Fatalf("arc: second top card not exiled: %+v", o)
	}
	// The two MoveZone events exited the library in top order: bears then
	// forest, proving the paid list preserves library order.
	var exiled []state.ObjID
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.To == state.ZExile && ev.From == state.ZLibrary {
			exiled = append(exiled, ev.Obj)
		}
	}
	if len(exiled) != 2 || exiled[0] != bears || exiled[1] != forest {
		t.Fatalf("library->exile order = %v, want [%d %d] (top order)", exiled, bears, forest)
	}
	replayCheck(t, e, cfg)

	// Insufficient library: Arc-Slogger needs ten top cards. Remove all but
	// nine from the library and confirm the ability is not offered at all.
	e2, _ := paidCostEngine(t, []string{"Arc-Slogger"}, nil)
	slogger := paidCostMoveTo(t, e2, 0, "Arc-Slogger", state.ZBattlefield)
	lib2 := append([]state.ObjID(nil), e2.G.Zone(state.ZLibrary, 0)...)
	for _, id := range lib2[9:] {
		e2.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZExile})
	}
	if got := len(e2.G.Zone(state.ZLibrary, 0)); got != 9 {
		t.Fatalf("precondition: library = %d, want 9", got)
	}
	addMana(t, e2, 0, "R")
	offered := false
	for _, o := range e2.Pending().Options {
		if o.Obj == slogger {
			offered = true
		}
	}
	if offered {
		t.Fatalf("Arc-Slogger offered with only 9 cards in library: %+v", e2.Pending().Options)
	}
}
