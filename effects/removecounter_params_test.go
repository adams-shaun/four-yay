package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestRemoveCounterKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestRemoveCounterKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(removeCounterKnownKeys[:]) ||
		len(slices.Compact(slices.Clone(removeCounterKnownKeys[:]))) != len(removeCounterKnownKeys) {
		t.Fatal("removeCounterKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileRemoveCounter pins the compiled shapes the resolution reads:
// the kind and count, both loud exotic-shape Notes in their historical
// order, the Choices$ arm's zone and bounds, the riders and the unread
// report.
func TestCompileRemoveCounter(t *testing.T) {
	sa := &cards.SA{API: "RemoveCounter", Params: map[string]string{
		"CounterType": " Any ", "CounterNum": " Any ", "ChoiceOptional": "Maybe", "UpTo": "True",
		"CounterNumShared": "True", "TgtZone": "Graveyard", "ValidTgts": "Card", "RememberAmount": "True",
		"Optional": "True", "RememberRemoved": " true ", "ChoiceZone": "Library", "Hidden": "True",
	}}
	p := RemoveCounterOf(sa)
	if p.ExoticNote != "unimplemented RemoveCounter shape: CounterType$ Any, ChoiceOptional$, UpTo$, CounterNum$ Any, "+
		"CounterNumShared$, TgtZone$ Graveyard, RememberAmount$, Optional$" {
		t.Fatalf("exotic = %q", p.ExoticNote)
	}
	if p.ChoiceNote != "unimplemented RemoveCounter choice shape: CounterType$ Any, CounterNum$ Any, UpTo$, "+
		"CounterNumShared$, ChoiceZone$ Library" {
		t.Fatalf("choice = %q", p.ChoiceNote)
	}
	if p.Kind != "Any" || p.AllKinds || p.NumAll || !p.NumSet || p.ChoiceOptional || !p.RememberAmount ||
		!p.RememberRemoved || p.Choices != "" {
		t.Fatalf("shape = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if RemoveCounterOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	def := RemoveCounterOf(&cards.SA{API: "RemoveCounter", Params: map[string]string{"Defined": "Self"}})
	if def.Kind != "P1P1" || def.NumSet || def.NumAll || def.ExoticNote != "" || def.ChoiceNote != "" ||
		def.ChoiceZone != state.ZBattlefield || def.RememberRemoved || !def.Defined.Is(RefSelf) {
		t.Fatalf("defaults = %+v", def)
	}
	all := RemoveCounterOf(&cards.SA{API: "RemoveCounter", Params: map[string]string{"CounterType": "All",
		"CounterNum": "All"}})
	if !all.AllKinds || !all.NumAll || all.ExoticNote != "" {
		t.Fatalf("all = %+v", all)
	}
	pick := RemoveCounterOf(&cards.SA{API: "RemoveCounter", Params: map[string]string{"Choices": " Saga.YouCtrl ",
		"CounterType": "LORE", "CounterNum": "X", "ChoiceZone": "Exile", "ChoiceOptional": "True", "ChoiceNum": "2"}})
	if pick.Choices != "Saga.YouCtrl" || pick.ChoiceNote != "" || pick.ChoiceZone != state.ZExile || !pick.ChoiceOptional ||
		!pick.ChoiceNumSet || pick.ChoiceNum != (ParamText{Text: "2", Present: true}) ||
		pick.CounterNum != (ParamText{Text: "X", Present: true}) {
		t.Fatalf("pick = %+v", pick)
	}
}

// TestRemoveCounterOfIsAllocationFree: a configured record or a front-cache
// hit allocates nothing.
func TestRemoveCounterOfIsAllocationFree(t *testing.T) {
	bound := slottedSA(t, "RemoveCounter", map[string]string{"CounterType": "TIME", "CounterNum": "1"})
	f := NewSAFacts(bound)
	f.Publish()
	if LoadSAFacts(bound) != f {
		t.Fatal("precondition: the configured record is not published on bound's facts slot")
	}
	cached := &cards.SA{API: "RemoveCounter", Params: map[string]string{"CounterType": "P1P1"}}
	RemoveCounterOf(cached)
	if n := allocsPerRun(100, func() {
		_ = RemoveCounterOf(bound)
		_ = RemoveCounterOf(cached)
	}); n != 0 {
		t.Fatalf("RemoveCounterOf allocated %v objects per run; want 0", n)
	}
	if f.RemoveCounter == nil || !f.RemoveCounter.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the RemoveCounter half")
	}
}
