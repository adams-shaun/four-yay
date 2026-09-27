package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRememberedLKIGroupKeepsExplicitlyRememberedSource pins the source-kept
// half of the RememberedLKI ref-group fix (task agent-20260919T194848Z-a720129d,
// t2). The plain-Remembered resolver (rememberedWithSource) drops every
// remembered entry whose object IS the resolving source; that guard was this
// build's stand-in for keeping a trigger's fire-time capture (the trigger's
// own source) out of a plain Remembered read. For RememberedLKI the
// capture-occurrence exclusion (rememberedExcludingCapture) already removes
// exactly the seeded capture, so the blanket source drop only ever deleted a
// REAL memory of the source.
//
// Two real corpus shapes depend on the source being kept:
//
//   - Cosima, God of the Voyage // The Omenkeel, SVar:DBReturn:
//     `ChangeZone | Defined$ Self | Origin$ Exile | Destination$ Battlefield |
//     RememberLKI$ True`, with `SVar:X:RememberedLKI$CardCounters.VOYAGE`.
//     The exiled god returns to the battlefield and its own X reads the
//     voyage counters the source carried in exile.
//   - Riders of the Mark, SVar:TrigChangeZone:
//     `ChangeZone | Defined$ Self | Origin$ Battlefield | Destination$ Hand |
//     RememberChanged$ True`, with `SVar:Y:RememberedLKI$CardToughness`.
//     It returns to its owner's hand and makes tokens equal to its toughness.
//
// Both move the SOURCE (Defined$ Self), so the moved object id == Ctx.Source,
// and a bare rememberedWithSource read answers zero for either. The fixture
// seeds the ctx the way a real chain does: the trigger's fire-time capture is
// already in Remembered, and the ChangeZone persist step appends the moved
// source, so the same id appears twice; the capture-occurrence exclusion must
// remove only the seeded occurrence and keep the explicit one.
func TestRememberedLKIGroupKeepsExplicitlyRememberedSource(t *testing.T) {
	h := newHost(t, 2)
	// A source with a distinct, non-default toughness and a named counter,
	// so a property read cannot pass by coincidence.
	card := mkCard(t, "Name:Cosima\nTypes:Creature God\nPT:2/5\nOracle:x\n")
	so := h.g.AddObject(card, 0)
	// Place the source on the battlefield (AddObject defaults to the
	// library). The corpus shapes move the source between exile/hand and the
	// battlefield; where it sits at read time is not what the test pins -- the
	// ref-group membership is -- but the precondition below asserts it.
	h.g.Obj(so.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), so.ID))
	// The voyage counters the source carried. They are read through the
	// object's LIVE counters (the LKI snapshot for a remembered source is not
	// required by the corpus shape: Cosima's counters are placed on the
	// exiled card by the DBReturn's own WithCountersAmount$ X, and the read
	// happens after the return).
	h.g.Obj(so.ID).AddCounter("VOYAGE", 2)
	// Riders' shape reads the source's toughness, which is printed 5 and
	// distinct from every other creature's here.
	toughness := int32(h.g.Obj(so.ID).Face().Toughness())

	// The real chain ctx: Captured = the source (the trigger's fire-time
	// capture), Remembered = the same id ONCE as the capture seed plus ONCE
	// as the explicit append the ChangeZone persist step makes.
	capture := state.Target{Obj: so.ID}
	explicit := state.Target{Obj: so.ID}
	c := &Ctx{Source: so.ID, Controller: 0,
		Remembered: []state.Target{capture, explicit},
		Captured:   []state.Target{capture},
	}

	// Precondition: the source is really on the battlefield with the counter
	// and toughness the assertions read; a vacuous board would let the test
	// pass against the wrong mapping.
	if o := h.g.Obj(so.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source not on battlefield: %+v", o)
	}
	if got := h.g.Obj(so.ID).Counter("VOYAGE"); got != 2 {
		t.Fatalf("precondition: source VOYAGE counters = %d, want 2", got)
	}
	if toughness != 5 {
		t.Fatalf("precondition: source printed toughness = %d, want 5", toughness)
	}

	// The capture-occurrence exclusion must remove exactly the seeded
	// occurrence: with the fix the sample set keeps the explicit source
	// entry, so the two properties answer their real values. Without it
	// (rememberedWithSource's blanket `t.Obj != c.Source`) both answer 0.
	for _, tc := range []struct {
		expr string
		want int32
	}{
		// Cosima's read.
		{"RememberedLKI$CardCounters.VOYAGE", 2},
		// Riders of the Mark's read.
		{"RememberedLKI$CardToughness", toughness},
		// The group itself must still contain exactly the one kept object.
		{"RememberedLKI$Amount", 1},
	} {
		if got := EvalCount(h, c, tc.expr); got != tc.want {
			t.Errorf("%s = %d, want %d (explicitly-remembered source dropped)", tc.expr, got, tc.want)
		}
	}

	// The plain Remembered ref now reads the SAME group as the LKI spelling
	// (one resolution rule): the capture-occurrence exclusion keeps the
	// explicit source memory, so the same ctx answers the source's real
	// toughness here too. This oracle previously pinned 0 -- the blanket
	// `t.Obj != c.Source` guard rememberedWithSource used as a stand-in for
	// the capture exclusion -- but that guard also deleted a real memory of
	// the source, which is what made Lukamina's DBReturn return nothing. The
	// capture-ONLY contrast (a ctx whose Remembered is just the seeded
	// capture) still reads zero; that distinct-capture/no-capture coverage
	// lives in remembered_capture_test.go and the direct gate test below.
	if got := EvalCount(h, c, "Remembered$CardToughness"); got != toughness {
		t.Errorf("Remembered$CardToughness = %d, want %d (explicitly-remembered source must be kept by the plain group too)", got, toughness)
	}
}
