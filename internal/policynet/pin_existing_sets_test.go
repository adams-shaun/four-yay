package policynet_test

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/view"
)

// TestExistingFeatureSetsArePinnedOverARealGame pins every checkpointable
// feature set that existed before the honest own-library set (v1, mz,
// entity) bit for bit over one whole real-deck game: every decision's
// deciding-seat view is encoded (state and every option) under each set,
// both from the projected View directly and from its JSON round trip (the
// training corpora store the view as JSON), and folded into one FNV-64
// digest per set. A v1 and an mz model with a fixed seed then score every
// decision, and those float bits are folded in too, so an existing
// checkpoint's inputs AND outputs are pinned, not just the encoder hash.
//
// The digests were measured on the commit before the own-library feature
// set existed. A new feature set, a new view field or a new manifest field
// must never move them: only a deliberate re-encoding of an existing set
// may, and that invalidates every checkpoint trained under it.
func TestExistingFeatureSetsArePinnedOverARealGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := []string{"mono-red-prowess", "mono-blue-tempo"}
	decks := make([][]*cards.Card, len(names))
	for i, n := range names {
		var err error
		if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
			t.Fatal(err)
		}
	}
	e := rules.New(rules.Config{Seed: 30_000_001, Names: names, Decks: decks, Tokens: reg.Tokens})
	e.Advance()
	rngs := []*rand.Rand{rand.New(rand.NewPCG(3, 0)), rand.New(rand.NewPCG(3, 1))}
	board := botpolicy.NewBoard(len(names))

	sets := []policynet.FeatureSet{policynet.FeaturesV1, policynet.FeaturesMZ, policynet.FeaturesEntity}
	digests := make([]uint64, len(sets))
	hs := make([]interface {
		Write([]byte) (int, error)
		Sum64() uint64
	}, len(sets))
	for i := range hs {
		hs[i] = fnv.New64a()
	}
	models := map[policynet.FeatureSet]*policynet.Model{}
	for _, fs := range []policynet.FeatureSet{policynet.FeaturesV1, policynet.FeaturesMZ} {
		m := policynet.NewModel(policynet.TableRows, 8, 16, rand.New(rand.NewPCG(11, uint64(fs))))
		m.Features = fs
		models[fs] = m
	}
	writeJSON := func(i int, x any) {
		b, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		hs[i].Write(b)
	}
	decisions := 0
	for n := 0; n < 1500 && !e.G.Over; n++ {
		d := e.Pending()
		if d == nil {
			break
		}
		v := view.Project(e.G, e, d.Player, d)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var rt view.View
		if err := json.Unmarshal(raw, &rt); err != nil {
			t.Fatal(err)
		}
		for i, fs := range sets {
			for _, vv := range []view.View{v, rt} {
				st := policynet.EncodeStateWith(fs, vv, d.Player, nil)
				writeJSON(i, st)
				opts := make([]policynet.Option, len(d.Options))
				for k := range d.Options {
					opts[k] = policynet.EncodeOptionWith(fs, vv, d.Player, d.Kind, d.Options[k], k, len(d.Options))
				}
				writeJSON(i, opts)
				if m := models[fs]; m != nil {
					var b [4]byte
					for _, y := range m.Score(st, opts) {
						u := math.Float32bits(y)
						b[0], b[1], b[2], b[3] = byte(u), byte(u>>8), byte(u>>16), byte(u>>24)
						hs[i].Write(b[:])
					}
				}
			}
		}
		decisions++
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if err := e.Submit(in); err != nil {
			t.Fatal(err)
		}
	}
	for i := range sets {
		digests[i] = hs[i].Sum64()
	}
	// Re-pinned at 0e5196eb7 (CR 103.8a: the starting player skips the whole
	// turn-1 draw step): the pinned GAME changed, not any set's encoding.
	want := []uint64{0xaa14d019fe36aee3, 0xd9a7ed8af91a4a83, 0xca51959b21684b69}
	t.Logf("decisions %d, digests v1=%#016x mz=%#016x entity=%#016x", decisions, digests[0], digests[1], digests[2])
	if decisions < 200 {
		t.Fatalf("only %d decisions encoded; the pin is too thin", decisions)
	}
	for i, fs := range sets {
		if digests[i] != want[i] {
			t.Errorf("feature set %s: digest %#016x, pinned %#016x -- an existing feature set's encoding (or a model's output over it) moved", fs, digests[i], want[i])
		}
	}
}
