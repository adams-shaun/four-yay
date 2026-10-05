package effects

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestDigKnownKeysSorted: the unread lookup binary-searches the table.
func TestDigKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(digKnownKeys[:]) || len(slices.Compact(slices.Clone(digKnownKeys[:]))) != len(digKnownKeys) {
		t.Fatal("digKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileDig pins the compiled shapes the resolution reads: the window
// and cap values, the defaulted spec and destinations, the riders and the
// unread report.
func TestCompileDig(t *testing.T) {
	sa := &cards.SA{API: "Dig", Params: map[string]string{
		"DigNum": " X ", "ChangeNum": "Any", "WithTotalCMC": "4", "ChangeValid": "Permanent.cmcLE3",
		"DestinationZone": "Battlefield", "Optional": "True", "OptionalAbilityPrompt": "Look?", "Reveal": "True",
		"NoLooking": "true", "ForceRevealToController": "True", "SkipReorder": "True", "Tapped": " True ",
		"FromBottom": "True", "RestRandomOrder": "True", "Imprint": "True", "GainControl": "True",
		"RememberChanged": "True", "LibraryPosition": " 0 ", "DestinationZone2": " Library ", "Choser": " Opponent ",
		"Hidden": "True",
	}}
	p := DigOf(sa)
	if p.DigNum != (ParamText{Text: " X ", Present: true}) || !p.DigNumX || !p.ChangeNumAny || p.ChangeNumCapped ||
		p.WithTotalCMC != (ParamText{Text: "4", Present: true}) || p.Spec != "PermanentCard.cmcLE3" ||
		p.Dest != state.ZBattlefield || !p.Optional || !p.PromptToSkipOptional || !p.RevealWin {
		t.Fatalf("window = %+v", p)
	}
	if !p.NoLooking || !p.ForceReveal || !p.SkipReorder || !p.Tapped || !p.FromBottom || !p.RestRandomOrder ||
		!p.Imprint || !p.GainControl || !p.RememberChanged || p.PrimaryPos != "0" || p.Dest2Name != "Library" ||
		p.Pos2 != "-1" || p.Choser != "Opponent" {
		t.Fatalf("riders = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if DigOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	def := DigOf(&cards.SA{API: "Dig", Params: map[string]string{"ChangeNum": "All", "Optional": "true",
		"Reveal": "True", "NoReveal": "True"}})
	if def.Spec != "Card" || def.Dest != state.ZHand || def.ChangeNumAny || def.ChangeNumCapped || def.Optional ||
		def.RevealWin || def.Dest2Name != "Library" || def.Pos2 != "-1" {
		t.Fatalf("defaults = %+v", def)
	}
	capped := DigOf(&cards.SA{API: "Dig", Params: map[string]string{"ChangeNum": "2",
		"DestinationZone2": "Graveyard", "LibraryPosition2": "0"}})
	if !capped.ChangeNumCapped || capped.Dest2Name != "Graveyard" || capped.Pos2 != "0" {
		t.Fatalf("capped = %+v", capped)
	}
}

// TestDigOfIsAllocationFree pins both configured-record and front-cache hits.
// Pick a currently empty direct-mapped slot so earlier Dig compiles cannot
// evict the entry during testing.AllocsPerRun; this test is not parallel, and
// no other test should have a live DigOf caller.
func TestDigOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "Dig", Params: map[string]string{"DigNum": "3", "ChangeNum": "1"}}
	f := NewSAFacts(bound)
	f.Publish()

	var cached *cards.SA
	for i := 0; i < len(digFront); i++ {
		candidate := &cards.SA{API: "Dig", Params: map[string]string{"DigNum": "2", "slot": fmt.Sprint(i)}}
		if digFront[paramMapSlot(candidate.Params)].Load() == nil {
			cached = candidate
			break
		}
	}
	if cached == nil {
		t.Fatal("no empty Dig front-cache slot available for allocation check")
	}
	cachedParams := DigOf(cached)
	if cachedParams == nil || DigOf(cached) != cachedParams {
		t.Fatal("could not prime and verify Dig front-cache hit")
	}
	if n := allocsPerRun(100, func() {
		_ = DigOf(bound)
		_ = DigOf(cached)
	}); n != 0 {
		t.Fatalf("DigOf allocated %v objects per run; want 0", n)
	}
	if f.Dig == nil || !f.Dig.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the Dig half")
	}
}
