//go:build manabrew

package manabrew

// The x_gorge_own_deck_v1 extension (seat-deck-manifest spec, interface
// mapping item 4): the translator-level contract for how the seat's native
// own-deck manifest (view.View.OwnDeck) is rendered into the optional
// gameView member. The end-to-end owner-only/stream/poll agreement lives in
// host/manabrewhttp/own_deck_test.go; this file pins the payload shape and
// its non-widening guarantees at the pure-function layer.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

func ownDeckFixture() *deck.Manifest {
	return &deck.Manifest{
		Name:       "alpha",
		Main:       []deck.ManifestRow{{Name: "Bear", Count: 4}, {Name: "Island", Count: 17}},
		Sideboard:  []deck.ManifestRow{{Name: "Counterspell", Count: 3}},
		Commanders: []string{"Bear"},
	}
}

// TestOwnDeckExtensionCarriesTheNativeManifest pins the payload: exactly the
// manifest's ordered name/count rows, nothing else -- no card instances, no
// object ids, no library order, no opponent data.
func TestOwnDeckExtensionCarriesTheNativeManifest(t *testing.T) {
	manifest := ownDeckFixture()
	fixture := projectionFixture()
	fixture.OwnDeck = manifest

	got, err := json.Marshal(New("table", 2, nil).gameView(fixture))
	if err != nil {
		t.Fatal(err)
	}
	var gv mb.GameViewDto
	if err := json.Unmarshal(got, &gv); err != nil {
		t.Fatal(err)
	}
	if gv.OwnDeck == nil {
		t.Fatal("seat state with a manifest omitted x_gorge_own_deck_v1")
	}
	want := &mb.OwnDeckExtension{
		Name:       "alpha",
		Main:       []mb.OwnDeckRow{{Name: "Bear", Count: 4}, {Name: "Island", Count: 17}},
		Sideboard:  []mb.OwnDeckRow{{Name: "Counterspell", Count: 3}},
		Commanders: []string{"Bear"},
	}
	if !reflect.DeepEqual(gv.OwnDeck, want) {
		t.Fatalf("own deck extension = %#v, want %#v", gv.OwnDeck, want)
	}
	// The row order is the manifest's own canonical order, not re-sorted or
	// collapsed a second time.
	raw := string(got)
	if !strings.Contains(raw, `"x_gorge_own_deck_v1":{"name":"alpha","main":[{"name":"Bear","count":4},{"name":"Island","count":17}],"sideboard":[{"name":"Counterspell","count":3}],"commanders":["Bear"]}`) {
		t.Fatalf("extension wire shape drifted: %s", raw)
	}
}

// TestOwnDeckExtensionZeroShape pins the spec's zero-card contract: an empty
// main is [] not null, an empty sideboard is omitted, and a manifest-less
// seat state omits the member entirely.
func TestOwnDeckExtensionZeroShape(t *testing.T) {
	empty := &deck.Manifest{Name: "empty"}
	fixture := projectionFixture()
	fixture.OwnDeck = empty
	got, err := json.Marshal(New("table", 2, nil).gameView(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"x_gorge_own_deck_v1":{"name":"empty","main":[]}`) {
		t.Fatalf("empty manifest did not render main as []: %s", got)
	}
	if strings.Contains(string(got), `"sideboard"`) {
		t.Fatalf("empty sideboard was not omitted: %s", got)
	}

	// A seat state with no manifest (the translator's other callers: the
	// synthetic fixtures, an implementation with none) omits the optional
	// member rather than publishing an empty lie.
	bare, err := json.Marshal(New("table", 2, nil).gameView(projectionFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "x_gorge_own_deck_v1") {
		t.Fatalf("manifest-less state emitted the extension: %s", bare)
	}
}

// TestOwnDeckExtensionDoesNotRetainTheManifest pins the copy contract: the
// rendered extension owns its row storage, so mutating the manifest (or a
// published Clone of it) after rendering cannot change what was published.
func TestOwnDeckExtensionDoesNotRetainTheManifest(t *testing.T) {
	manifest := ownDeckFixture()
	fixture := projectionFixture()
	fixture.OwnDeck = manifest
	ext := ownDeckExtension(manifest)
	manifest.Main[0].Name = "mutated"
	manifest.Sideboard[0].Count = 99
	manifest.Commanders[0] = "mutated"
	if ext.Main[0].Name != "Bear" || ext.Sideboard[0].Count != 3 || ext.Commanders[0] != "Bear" {
		t.Fatalf("extension retained the manifest's backing slices: %#v", ext)
	}
}

// TestPromptNeverCarriesTheOwnDeckExtension proves the extension is a
// state-only member: existing prompt messages are unchanged by the feature
// (PromptMessage gained no such member), so an old client's prompt handling
// cannot be affected by it. Two real mapped kinds are enough: the member
// would have to live on the shared prompt envelope to reach either.
func TestPromptNeverCarriesTheOwnDeckExtension(t *testing.T) {
	vv := projectionFixture()
	prompts := []*decision.Decision{
		newDec(1, 0, decision.KPriority),
		newDec(2, 0, decision.KChoose, decision.Option{Index: 0, Kind: "x", Label: "five", Amount: 5}),
	}
	built := 0
	for _, d := range prompts {
		pm, err := New("table", 2, nil).Prompt(d, &vv)
		if err != nil {
			continue // an unmapped stub is not a prompt message at all
		}
		built++
		raw, err := json.Marshal(mb.EngineMessage{Value: pm})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "x_gorge_own_deck_v1") {
			t.Fatalf("%s prompt carries the own-deck extension: %s", d.Kind, raw)
		}
	}
	if built == 0 {
		t.Fatal("no prompt built for the mapped kinds; the prompt surface moved")
	}
}
