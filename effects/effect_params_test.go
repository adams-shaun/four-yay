package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestEffectKnownKeysSorted: the unread lookup binary-searches the table.
func TestEffectKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(effectKnownKeys[:]) || len(slices.Compact(slices.Clone(effectKnownKeys[:]))) != len(effectKnownKeys) {
		t.Fatal("effectKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileEffect pins the compiled shapes the target ask, the opening-hand
// Effect and the resolution share, at the spelling each reader used.
func TestCompileEffect(t *testing.T) {
	sa := &cards.SA{API: "Effect", Params: map[string]string{
		"StaticAbilities": "STA,STB", "Triggers": "TrigA TrigB", "ReplacementEffects": "R1",
		"Name": " Emblem ", "Stackable": " false ", "ForgetOnMoved": " Battlefield ", "ExileOnMoved": "Graveyard",
		"ForgetCounter": "VOW", "ForgetOnCast": "False", "ImprintOnHost": "True", "ForgetOnPhasedIn": "True",
		"RememberLKI": "Targeted", "SetChosenNumber": "X", "EffectOwner": " Opponent ", "Hidden": "True",
	}}
	p := EffectOf(sa)
	if p.Duration != "" || p.StaticAbilities != "STA,STB" || p.Triggers != "TrigA TrigB" || p.ReplacementEffects != "R1" {
		t.Fatalf("lists = %+v", p)
	}
	if p.Name != "Emblem" || !p.NotStackable || p.ForgetOnMoved != "Battlefield" || p.ExileOnMoved != "Graveyard" ||
		p.ForgetCounter != "VOW" || p.ForgetOnCast != "" || !p.ImprintOnHostTrue || !p.ForgetOnPhasedIn ||
		p.RememberLKI != "Targeted" || p.SetChosenNumber != "X" || p.EffectOwner != "Opponent" {
		t.Fatalf("riders = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if EffectOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	q := EffectOf(&cards.SA{API: "Effect", Params: map[string]string{"Duration": "Permanent", "ImprintOnHost": "Maybe", "Stackable": "True"}})
	if q.Duration != "Permanent" || q.NotStackable || q.ImprintOnHost != "Maybe" || q.ImprintOnHostTrue {
		t.Fatalf("second shape = %+v", q)
	}
	tr, ok := cards.ParseTriggerLine("Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | Execute$ TrigDraw | OneOff$ True | OptionalDecider$ You | Static$ True | ThisTurn$ True")
	if !ok {
		t.Fatal("trigger line did not parse")
	}
	if tl := readEffectTriggerLine(&tr); tl.Execute != "TrigDraw" || !tl.OneOff || tl.OptionalDecider != "You" ||
		!tl.Static || tl.ThisTurn != "True" || tl.Phase != "End of Turn" || tl.ValidPlayer != "You" {
		t.Fatalf("trigger line = %+v", tl)
	}
}
