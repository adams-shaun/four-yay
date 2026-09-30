package deck

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// parseCurveCard compiles a one-face card with a printed mana cost and pins
// the CMC the curve derivation will read, so a silent derivation change
// (cost parsing, front-face selection) fails here first as a precondition.
func parseCurveCard(t *testing.T, name, cost string, wantCMC int32) *cards.Card {
	t.Helper()
	src := "Name:" + name + "\nTypes:Creature\nPT:1/1\n"
	if cost != "" {
		src += "ManaCost:" + cost + "\n"
	}
	c, diags := cards.ParseBytes("curve.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	c.Link()
	if got := c.Faces[0].Cmc(); got != wantCMC {
		t.Fatalf("precondition: %q parsed to CMC %d, want %d", name, got, wantCMC)
	}
	return c
}

// TestCurveOfBucketsAscendingByCost pins the derivation: copies bucket at
// their front-face CMC, zero-cost cards bucket at 0, equal costs collapse to
// one row, and the rows come out ascending by cost regardless of the input's
// order.
func TestCurveOfBucketsAscendingByCost(t *testing.T) {
	bolt := parseCurveCard(t, "Bolt", "{R}", 1)
	bear := parseCurveCard(t, "Bear", "{1}{G}", 2)
	giant := parseCurveCard(t, "Giant", "{3}{G}{G}{G}", 6)
	land := parseCurveCard(t, "Island", "", 0)
	// Deliberately out of cost order and with repeats at two costs.
	main := []*cards.Card{giant, bear, land, bolt, bolt, bear, land}
	got := CurveOf(main)
	want := []CurveRow{{CMC: 0, Count: 2}, {CMC: 1, Count: 2}, {CMC: 2, Count: 2}, {CMC: 6, Count: 1}}
	if len(got) != len(want) {
		t.Fatalf("curve = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("curve row %d = %#v, want %#v (full curve %#v)", i, got[i], want[i], got)
		}
	}
}

// TestCurveOfEmptyIsNil pins the zero shape: no cards means a nil curve, so
// an omitempty manifest member is absent rather than an empty lie.
func TestCurveOfEmptyIsNil(t *testing.T) {
	if got := CurveOf(nil); got != nil {
		t.Fatalf("nil main derived %#v, want nil", got)
	}
	if got := CurveOf([]*cards.Card{}); got != nil {
		t.Fatalf("empty main derived %#v, want nil", got)
	}
}

// TestNewManifestCarriesCurveAndCloneOwnsIt pins the manifest integration:
// NewManifest derives the curve from Main (not the sideboard), and a Clone
// owns its curve storage.
func TestNewManifestCarriesCurveAndCloneOwnsIt(t *testing.T) {
	bolt := parseCurveCard(t, "Bolt", "{R}", 1)
	land := parseCurveCard(t, "Island", "", 0)
	main := []*cards.Card{bolt, bolt, land}
	sideboard := []*cards.Card{bolt}
	m := NewManifest("curve deck", "", main, sideboard, nil)
	if len(m.Curve) != 2 || m.Curve[0].CMC != 0 || m.Curve[0].Count != 1 || m.Curve[1].CMC != 1 || m.Curve[1].Count != 2 {
		t.Fatalf("manifest curve = %#v, want [{0 1} {1 2}] (sideboard must not be counted)", m.Curve)
	}
	clone := m.Clone()
	clone.Curve[0] = CurveRow{CMC: 9, Count: 9}
	if m.Curve[0].CMC != 0 || m.Curve[0].Count != 1 {
		t.Fatalf("Clone shares curve storage: original = %#v after clone mutation", m.Curve)
	}
}
