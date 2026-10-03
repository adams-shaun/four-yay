package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestCharmKnownKeysSorted: the unread lookup binary-searches the table.
func TestCharmKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(charmKnownKeys[:]) || len(slices.Compact(slices.Clone(charmKnownKeys[:]))) != len(charmKnownKeys) {
		t.Fatal("charmKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileCharm pins the compiled shapes the rules paths read: the trimmed
// mode list (with its clipped capacity, so an append never writes into the
// shared record), the presence bit, the bounds' raw text, the flags, the
// unread report (a Charm only) and the front cache's identity rule.
func TestCompileCharm(t *testing.T) {
	sa := &cards.SA{API: "Charm", Params: map[string]string{
		"Choices": "DBA, DBB", "CharmNum": "2", "MinCharmNum": "0", "CanRepeatModes": "True",
		"ChoiceRestriction": "ThisTurn", "Random": "Compare", "RandomCompareSVar": "Y",
		"RandomCompare": "LT1", "Optional": "True", "NotAParam": "x",
	}}
	p := CharmOf(sa)
	if !slices.Equal(p.Modes, []string{"DBA", "DBB"}) || cap(p.Modes) != len(p.Modes) || !p.HasChoices {
		t.Fatalf("modes = %q (cap %d), has %v", p.Modes, cap(p.Modes), p.HasChoices)
	}
	if p.CharmNum.Text != "2" || !p.MinCharmNum.Present || !p.CanRepeatModes || !p.OptionalTrue ||
		p.ChoiceRestriction != "ThisTurn" || p.Random != "Compare" || p.RandomCompareSVar != "Y" {
		t.Fatalf("compiled = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"NotAParam"}) {
		t.Fatalf("unread = %v", p.Unread)
	}
	if CharmOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	cp := *sa
	cp.Params = map[string]string{"Choices": "DBC"}
	if got := CharmOf(&cp); got == p || !slices.Equal(got.Modes, []string{"DBC"}) {
		t.Fatal("a rewritten Params map read the stale record")
	}
	// No Choices$: the one-name list every reader built by hand before.
	none := CharmOf(&cards.SA{API: "VillainousChoice", Params: map[string]string{"Defined": "Opponent"}})
	if !slices.Equal(none.Modes, []string{""}) || none.HasChoices || none.Unread != nil || none.Defined != "Opponent" {
		t.Fatalf("no-choices record = %+v", none)
	}
	if CharmOf(nil).HasChoices || len(CharmOf(nil).Modes) != 1 {
		t.Fatal("nil SA must read as an ability with no parameters")
	}
}
