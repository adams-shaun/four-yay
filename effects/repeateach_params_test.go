package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestRepeatEachKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestRepeatEachKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(repeatEachKnownKeys[:]) ||
		len(slices.Compact(slices.Clone(repeatEachKnownKeys[:]))) != len(repeatEachKnownKeys) {
		t.Fatal("repeatEachKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileRepeatEach pins the compiled shapes the resolution reads: the
// raw body and subject selectors, the True riders, the RepeatCards$ zone set
// (blank Zone$ = the battlefield, unknown entries name nothing), the
// DefinedCards$ selector from the Defined tier, and the unread report.
func TestCompileRepeatEach(t *testing.T) {
	sa := &cards.SA{API: "RepeatEach", Params: map[string]string{
		"RepeatSubAbility": "DBBody ", "DamageMap": " true", "ChangeZoneTable": "True", "AmountFromVotes": "TRUE",
		"ClearRememberedBeforeLoop": "True", "RepeatOptionalForEachPlayer": "True",
		"RepeatOptionalMessage": " Pay? ", "RepeatPlayers": " Player", "RepeatSpellAbilities": "Card",
		"RepeatTargeted": "True", "RepeatCards": " Creature ", "Zone": "Graveyard, Exile ,Sideboard",
		"ChooseOrder": " RememberedPlayer ", "DefinedCards": "Remembered", "Hidden": "True",
	}}
	p := RepeatEachOf(sa)
	if p.SubAbility != "DBBody " || !p.DamageMap || !p.ChangeZoneTable || !p.AmountFromVotes || !p.ClearRemembered ||
		!p.OptionalForEach || p.OptionalMessage != "Pay?" || p.Players != " Player" || p.SpellAbilities != "Card" ||
		!p.Targeted || p.Cards != "Creature" || p.ChooseOrder != "RememberedPlayer" || p.DefinedCards != "Remembered" {
		t.Fatalf("shape = %+v", p)
	}
	if p.Zones != zoneMaskOf([]state.Zone{state.ZGraveyard, state.ZExile}) {
		t.Fatalf("zones = %b", p.Zones)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if RepeatEachOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	def := RepeatEachOf(&cards.SA{API: "RepeatEach", Params: map[string]string{"RepeatSubAbility": "DBBody"}})
	if def.Zones != zoneMaskOf([]state.Zone{state.ZBattlefield}) || def.DamageMap || def.Targeted ||
		def.Players != "" || def.Cards != "" || def.ChooseOrder != "" || def.DefinedCards != "" || len(def.Unread) != 0 {
		t.Fatalf("defaults = %+v", def)
	}
}

// TestRepeatEachOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing.
func TestRepeatEachOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "RepeatEach", Params: map[string]string{"RepeatSubAbility": "DBA", "RepeatPlayers": "Player"}}
	f := NewSAFacts(bound)
	f.Publish()
	cached := &cards.SA{API: "RepeatEach", Params: map[string]string{"RepeatSubAbility": "DBB", "RepeatCards": "Creature"}}
	RepeatEachOf(cached)
	if n := allocsPerRun(100, func() {
		_ = RepeatEachOf(bound)
		_ = RepeatEachOf(cached)
	}); n != 0 {
		t.Fatalf("RepeatEachOf allocated %v objects per run; want 0", n)
	}
	if f.RepeatEach == nil || !f.RepeatEach.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the RepeatEach half")
	}
}
