package scriptfacts

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// These tests exercise the leaf's exported API directly, so the package is
// covered in its own compilation unit (the historical tests in package rules
// go through the rules/scriptfacts_bridge.go forwarders). Everything here is
// built from cards.Face/cards.SA/cards.Trigger literals -- no corpus, no game
// state -- which is exactly the leaf's contract.

func TestPrintedHeadsOfClassifiesAndVerifies(t *testing.T) {
	// One face per head: Of must map each printed head to exactly its own bit,
	// so a wrong table entry (a head filed under a sibling) fails loudly
	// rather than staying invisible behind another set bit.
	for _, tc := range []struct {
		kw   string
		want Heads
	}{
		{"Kicker:2", Kicker}, {"Surge", Surge}, {"Entwine", Entwine},
		{"Replicate", Replicate}, {"Multikicker", Multikicker}, {"Squad", Squad},
		{"Evoke", Evoke}, {"Dash", Dash}, {"Overload", Overload}, {"Warp", Warp},
		{"Emerge", Emerge}, {"Bestow", Bestow}, {"Mutate", Mutate},
		{"Buyback", Buyback}, {"Suspend", Suspend}, {"Plot", Plot},
		{"Morph", Morph}, {"Megamorph", Megamorph}, {"Disguise", Disguise},
		{"MayFlashCost:1", MayFlashCost},
		{"AlternateAdditionalCost", AlternateAdditionalCost}, {"Impending", Impending},
	} {
		got := Of(&cards.Face{Keywords: []string{tc.kw}})
		if got != tc.want {
			t.Errorf("Of(%q) = %b, want exactly %b", tc.kw, got, tc.want)
		}
	}

	f := &cards.Face{Name: "Sample", Keywords: []string{"Kicker:2", "Flying", "MayFlashCost:1"}}
	h := Of(f)
	if h != Kicker|MayFlashCost {
		t.Fatalf("Of(%v) = %b, want exactly Kicker|MayFlashCost", f.Keywords, h)
	}
	// A non-ASCII head can fold to an ASCII one, so Of must answer All.
	if got := Of(&cards.Face{Keywords: []string{"K\u212Aicker"}}); got != All {
		t.Fatalf("Of(non-ASCII head) = %b, want All", got)
	}
	// Verify must panic when a printed head's bit is clear, and not otherwise.
	Verify(f, h)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Verify did not panic on a clear bit for a printed head")
			}
		}()
		Verify(f, h&^Kicker)
	}()
}

func TestForetellCostExplicitAndPrinted(t *testing.T) {
	explicit := &cards.Face{Name: "Explicit", ManaCost: "4 W", Keywords: []string{"Foretell:2 U"}}
	got, ok := ForetellCost(explicit)
	if !ok || got.Generic != 2 || got.Colored[state.MU] != 1 {
		t.Fatalf("explicit Foretell cost = %+v ok=%v, want {2}{U}", got, ok)
	}
	printed := &cards.Face{Name: "Printed", ManaCost: "4 W"}
	got, ok = ForetellCost(printed)
	if !ok || got.Generic != 2 || got.Colored[state.MW] != 1 {
		t.Fatalf("printed Foretell cost = %+v ok=%v, want {2}{W}", got, ok)
	}
	if _, ok := ForetellCost(nil); ok {
		t.Fatal("ForetellCost(nil) reported ok")
	}
}

func TestTriggerOptionalSpecSuppressesPlayPermissions(t *testing.T) {
	elected := cards.Trigger{Params: map[string]string{"OptionalDecider": "You", "TriggerDescription": "You may draw a card."}}
	if got := TriggerOptionalSpec(elected); got != "You" {
		t.Fatalf("TriggerOptionalSpec(elected) = %q, want %q", got, "You")
	}
	permission := cards.Trigger{Params: map[string]string{
		"OptionalDecider":    "You",
		"TriggerDescription": "Until the end of your next turn, you may play that card.",
	}}
	if got := TriggerOptionalSpec(permission); got != "" {
		t.Fatalf("TriggerOptionalSpec(play permission) = %q, want empty", got)
	}
	if got := TriggerOptionalSpec(cards.Trigger{}); got != "" {
		t.Fatalf("TriggerOptionalSpec(no spec) = %q, want empty", got)
	}
}

func TestGrantedKeywordTriggerReadsTheKeywordLine(t *testing.T) {
	tr := GrantedKeywordTrigger("Prowess")
	if tr == nil {
		t.Fatal("GrantedKeywordTrigger(Prowess) = nil, want a synthesized trigger")
	}
	if tr.Mode != "SpellCast" || tr.Effect == nil || tr.Effect.API != "Pump" {
		t.Fatalf("Prowess trigger = %+v, want SpellCast mode with a Pump body", tr)
	}
	if !GrantedTriggerHeads("Prowess") {
		t.Fatal("GrantedTriggerHeads(Prowess) = false")
	}
	if GrantedKeywordTrigger("Flying") != nil {
		t.Fatal("GrantedKeywordTrigger(Flying) synthesized a trigger; only fixed-body heads may")
	}
	if strings.TrimSpace(tr.Effect.ParamStr(cards.PKNumAtt)) != "+1" {
		t.Fatalf("Prowess body NumAtt = %q, want +1", tr.Effect.ParamStr(cards.PKNumAtt))
	}
}
