package policynet

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/view"
)

// ownLibView is a small seat-0 view whose own-library composition closes:
// the list is Bolt x4, Goblin x2, Mountain x4 (land); seat 0 sees one Bolt in
// hand, one Mountain on the battlefield and one Goblin in the graveyard, so
// 7 cards remain -- Bolt 3, Goblin 1, Mountain 3.
func ownLibView() view.View {
	m := &deck.Manifest{Name: "me", Main: []deck.ManifestRow{
		{Name: "Bolt", Count: 4}, {Name: "Goblin", Count: 2}, {Name: "Mountain", Count: 4, Land: true},
	}}
	return view.View{
		Viewer: 0, OwnDeck: m, Visibility: "seat", Step: "main1", Phase: "main1",
		Players: []view.PlayerView{
			{ID: 0, Life: 20, LibrarySize: 7, HandSize: 1,
				Hand:        []view.CardView{{ID: 1, Name: "Bolt", Types: "Instant", ManaCost: "R", Owner: 0}},
				Battlefield: []view.CardView{{ID: 2, Name: "Mountain", Types: "Basic Land Mountain", Owner: 0}},
				Graveyard:   []view.CardView{{ID: 3, Name: "Goblin", Types: "Creature Goblin", Owner: 0}},
				Exile:       []view.CardView{}, Command: []view.CardView{}},
			{ID: 1, Life: 20, LibrarySize: 50, HandSize: 7,
				Battlefield: []view.CardView{}, Graveyard: []view.CardView{}, Exile: []view.CardView{}, Command: []view.CardView{}},
		},
		Stack: []view.StackView{},
	}
}

func sparseValue(st State, s string) (float32, bool) {
	r := hashID(s)
	for _, f := range st.Sparse {
		if f.Row == r {
			return f.Value, true
		}
	}
	return 0, false
}

// mz-ownlib is mz plus exactly the honest own-library rows: copies left over
// library size per name, the land fraction; the unknown row when the view
// cannot account for the library; and never a read of the view's own
// Library list.
func TestOwnLibFeatures(t *testing.T) {
	v := ownLibView()
	mz := EncodeStateWith(FeaturesMZ, v, 0, nil)
	st := EncodeStateWith(FeaturesMZOwnLib, v, 0, nil)
	if !reflect.DeepEqual(st.Dense, mz.Dense) || len(st.Sparse) != len(mz.Sparse)+4 {
		t.Fatalf("mz-ownlib is not mz plus four rows: %d vs %d sparse", len(st.Sparse), len(mz.Sparse))
	}
	for name, want := range map[string]float32{
		"mz|ownlibleft|Bolt": 3.0 / 7, "mz|ownlibleft|Goblin": 1.0 / 7, "mz|ownlibleft|Mountain": 3.0 / 7, "mz|ownlib|land": 3.0 / 7,
	} {
		if got, ok := sparseValue(st, name); !ok || got != want {
			t.Fatalf("%s = %v (present %v), want %v", name, got, ok, want)
		}
	}
	for i := 1; i < len(st.Sparse); i++ {
		if st.Sparse[i].Row < st.Sparse[i-1].Row {
			t.Fatal("sparse bag not sorted")
		}
	}
	// The view's own Library list is the engine's contents: it must not be
	// read. A Library claiming anything at all changes nothing.
	lied := ownLibView()
	lied.Players[0].Library = []view.CardView{{Name: "Goblin"}, {Name: "Goblin"}, {Name: "Goblin"}}
	if !reflect.DeepEqual(EncodeStateWith(FeaturesMZOwnLib, lied, 0, nil), st) {
		t.Fatal("mz-ownlib read the view's Library list")
	}
	// An own card the seat cannot identify (a face-down exile it may not
	// look at): unknown, and no composition row at all.
	hidden := ownLibView()
	hidden.Players[0].LibrarySize = 6
	hidden.Players[0].Exile = []view.CardView{{ID: 9, FaceDown: true, Owner: 0}}
	hs := EncodeStateWith(FeaturesMZOwnLib, hidden, 0, nil)
	if v, ok := sparseValue(hs, "mz|ownlib|unknown"); !ok || v != 1 {
		t.Fatal("hidden own card: no unknown row")
	}
	if _, ok := sparseValue(hs, "mz|ownlib|land"); ok {
		t.Fatal("hidden own card: a composition row was emitted")
	}
	// No deck list (a view without OwnDeck): unknown.
	bare := ownLibView()
	bare.OwnDeck = nil
	if v, ok := sparseValue(EncodeStateWith(FeaturesMZOwnLib, bare, 0, nil), "mz|ownlib|unknown"); !ok || v != 1 {
		t.Fatal("no deck list: no unknown row")
	}
	// Options encode exactly as mz.
	v2 := ownLibView()
	if !reflect.DeepEqual(EncodeOptionWith(FeaturesMZOwnLib, v2, 0, decision.KPriority, decision.Option{Index: 0, Kind: "cast", Obj: 1}, 0, 1), EncodeOptionWith(FeaturesMZ, v2, 0, decision.KPriority, decision.Option{Index: 0, Kind: "cast", Obj: 1}, 0, 1)) {
		t.Fatal("mz-ownlib changed an option encoding")
	}
}

// The mz-ownlib hash is pinned, distinct from every other set's, maps back
// to the set, and the set is checkpointable and round-trips a checkpoint.
func TestOwnLibCheckpoint(t *testing.T) {
	const pinned = uint64(0xdfccca3c483243e2)
	h := EncoderHashFor(FeaturesMZOwnLib)
	t.Logf("mz-ownlib encoder hash %#016x", h)
	if h != pinned {
		t.Fatalf("EncoderHashFor(mz-ownlib) = %#016x, want pinned %#016x", h, pinned)
	}
	for _, fs := range []FeatureSet{FeaturesV1, FeaturesMZ, FeaturesMZOppHand, FeaturesMZOracle, FeaturesEntity} {
		if EncoderHashFor(fs) == h {
			t.Fatalf("mz-ownlib hash collides with %s", fs)
		}
	}
	if fs, ok := FeaturesForHash(h); !ok || fs != FeaturesMZOwnLib || FeaturesMZOwnLib.Diagnostic() {
		t.Fatalf("FeaturesForHash = %v %v", fs, ok)
	}
	if fs, ok := onPolicyFeatures(fmt.Sprintf("%016x", h)); !ok || fs != FeaturesMZOwnLib {
		t.Fatalf("onPolicyFeatures = %v %v", fs, ok)
	}
	m := NewModel(TableRows, 4, 3, rand.New(rand.NewPCG(1, 2)))
	m.Features = FeaturesMZOwnLib
	var buf bytes.Buffer
	if err := WriteCheckpoint(m, &buf); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCheckpoint(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Features != FeaturesMZOwnLib {
		t.Fatalf("round-tripped features %s", got.Features)
	}
}
