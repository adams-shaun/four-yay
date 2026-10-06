package params

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestRefOfClassifies pins the tag and flag vocabulary: the tag is the exact
// trimmed text, the flags read the text as written.
func TestRefOfClassifies(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		kind  RefKind
		flags RefFlag
		text  string
	}{
		{"", RefAbsent, RefPresent, ""},
		{"   ", RefAbsent, RefPresent, ""},
		{"Self", RefSelf, RefPresent, "Self"},
		{" You ", RefYou, RefPresent, "You"},
		{"Remembered", RefRemembered, RefPresent | RefPlainRemembered, "Remembered"},
		{"RememberedLKI", RefOther, RefPresent | RefPlainRemembered, "RememberedLKI"},
		{"RememberedController", RefOther, RefPresent, "RememberedController"},
		{"Targeted", RefTargeted, RefPresent, "Targeted"},
		{"Parent", RefParent, RefPresent, "Parent"},
		{"Imprinted", RefImprinted, RefPresent, "Imprinted"},
		{"ImprintedLKI", RefImprintedLKI, RefPresent, "ImprintedLKI"},
		{"ActivePlayer", RefActivePlayer, RefPresent, "ActivePlayer"},
		{"TriggeredSpellAbility", RefTriggeredSpellAbility, RefPresent, "TriggeredSpellAbility"},
		{"self", RefOther, RefPresent, "self"},
		{"Valid", RefOther, RefPresent | RefValid, "Valid"},
		{"Valid Creature.YouCtrl", RefOther, RefPresent | RefValid, "Valid Creature.YouCtrl"},
		{"ValidStack Spell", RefOther, RefPresent, "ValidStack Spell"},
		{"Self & Remembered", RefOther, RefPresent | RefCompound, "Self & Remembered"},
	} {
		r := RefOf(tc.raw)
		if r.Kind != tc.kind || r.Flags != tc.flags || r.Text != tc.text || r.Raw != tc.raw {
			t.Errorf("RefOf(%q) = %+v, want kind %d flags %b text %q", tc.raw, r, tc.kind, tc.flags, tc.text)
		}
	}
	if r := refOf("", false); r.Present() || r.Set() || r.Param() != (ParamText{}) {
		t.Errorf("absent ref = %+v", r)
	}
}

// TestCompileDefined pins the three compiled references and the separate
// DefinedPlayer$ reader.
func TestCompileDefined(t *testing.T) {
	sa := &cards.SA{API: "ChangeZone", Params: map[string]string{
		"Defined": " Targeted ", "DefinedCards": "ExiledWith", "DefinedPlayer": " TargetedController ",
	}}
	p := DefinedOf(sa)
	if !p.Defined.Is(RefTargeted) || p.Defined.Raw != " Targeted " || !p.Defined.Present() {
		t.Fatalf("Defined = %+v", p.Defined)
	}
	if p.Cards.Text != "ExiledWith" || p.Cards.Kind != RefOther {
		t.Fatalf("Cards = %+v", p.Cards)
	}
	if p.Target.Present() || p.Target.Set() {
		t.Fatalf("Target = %+v", p.Target)
	}
	if dp := DefinedPlayerRef(sa); dp.Param() != (ParamText{Text: "TargetedController", Present: true}) {
		t.Fatalf("DefinedPlayer = %+v", dp)
	}
	if DefinedOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	if DefinedOf(nil) != &noDefined || DefinedOf(&cards.SA{}) != &noDefined {
		t.Fatal("an ability with no parameters must answer the shared empty record")
	}
}

// TestDefinedOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing, and neither does classifying selector text.
func TestDefinedOfIsAllocationFree(t *testing.T) {
	bound := slottedSA(t, "Pump", map[string]string{"Defined": "Self", "NumAtt": "+1"})
	f := newTestFacts(bound)
	f.Publish()
	if LoadFacts(bound) != &f.Facts {
		t.Fatal("precondition: the configured record is not published on bound's facts slot")
	}
	cached := &cards.SA{API: "Destroy", Params: map[string]string{"Defined": "Remembered"}}
	DefinedOf(cached)
	if n := allocsPerRun(100, func() {
		_ = DefinedOf(bound)
		_ = DefinedOf(cached)
		_ = DefinedRefOf(cached)
		_ = RefOf("Valid Creature.YouCtrl & Self")
	}); n != 0 {
		t.Fatalf("DefinedOf allocated %v objects per run; want 0", n)
	}
	if f.Defined == nil || !f.Defined.boundTo(bound.Params) || !f.Defined.Defined.Is(RefSelf) {
		t.Fatal("NewSAFacts did not compile the Defined half")
	}
}
