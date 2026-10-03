package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestAttachKnownKeysSorted: the unread lookup binary-searches the table.
func TestAttachKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(attachKnownKeys[:]) || len(slices.Compact(slices.Clone(attachKnownKeys[:]))) != len(attachKnownKeys) {
		t.Fatal("attachKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileAttach pins the compiled shapes the offer and the resolution
// share: the Object$ kind, the Enchant self-attach, the pools, the prompt
// default, ValidTgts$' inZone zones, the single Origin$ zone and the unread
// report.
func TestCompileAttach(t *testing.T) {
	sa := &cards.SA{API: "Attach", Params: map[string]string{
		"Object": "Self", "Keyword": "Enchant", "ValidTgts": "Creature.inZoneGraveyard,Creature.inZoneExile,Card.inZoneGraveyard",
		"Origin": "Graveyard", "Optional": "True", "RememberAttached": "True", "Hidden": "True",
	}}
	p := AttachOf(sa)
	if p.objectKind != attachObjectSelf || !p.ObjectPresent || !p.EnchantSelf || p.Unattach {
		t.Fatalf("object = %+v", p)
	}
	if !p.OptionalTrue || !p.RememberAttached || p.ChoicePrompt != "Choose card" || p.Choices != "" {
		t.Fatalf("flags = %+v", p)
	}
	if !slices.Equal(p.ValidTgtsZones, []state.Zone{state.ZGraveyard, state.ZExile}) {
		t.Fatalf("ValidTgts zones = %v", p.ValidTgtsZones)
	}
	if !p.OriginSingle || p.OriginZone != state.ZGraveyard {
		t.Fatalf("origin = %v %v", p.OriginSingle, p.OriginZone)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if AttachOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	for spec, want := range map[string]attachObjectKind{
		"": attachObjectSelf, "Remembered": attachObjectRemembered,
		"AttachedTo Targeted.Equipment": attachObjectAttachedTo, "TriggeredCardLKICopy": attachObjectDefined,
	} {
		q := AttachOf(&cards.SA{API: "Attach", Params: map[string]string{"Object": spec, "Origin": "Any", "ChoiceTitle": "Pick"}})
		if q.objectKind != want || q.OriginSingle || q.ChoicePrompt != "Pick" || q.EnchantSelf {
			t.Fatalf("Object$ %q compiled %+v", spec, q)
		}
	}
	if q := AttachOf(&cards.SA{API: "Attach", Params: map[string]string{"Unattach": "True"}}); !q.Unattach || q.ObjectPresent {
		t.Fatalf("Unattach$ = %+v", q)
	}
}
