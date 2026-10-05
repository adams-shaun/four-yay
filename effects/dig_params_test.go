package effects

import (
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

// TestDigOfIsAllocationFree verifies configured Dig records allocate nothing.
// Front-cache hits are covered by TestCompileDig; using that process-wide,
// direct-mapped cache here would make the allocation measurement depend on
// unrelated DigOf calls evicting its slot.
func TestDigOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "Dig", Params: map[string]string{"DigNum": "3", "ChangeNum": "1"}}
	f := NewSAFacts(bound)
	f.Publish()
	cached := &cards.SA{API: "Dig", Params: map[string]string{"DigNum": "2"}}
	cachedFacts := NewSAFacts(cached)
	cachedFacts.Publish()
	if n := allocsPerRun(100, func() {
		_ = DigOf(bound)
		_ = DigOf(cached)
	}); n != 0 {
		t.Fatalf("DigOf allocated %v objects per run; want 0", n)
	}
	if f.Dig == nil || !f.Dig.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the Dig half")
	}
	if cachedFacts.Dig == nil || !cachedFacts.Dig.boundTo(cached.Params) {
		t.Fatal("NewSAFacts did not compile the cached Dig half")
	}
}
