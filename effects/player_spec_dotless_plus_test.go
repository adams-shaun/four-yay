package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// agent-20261009T174731Z-42d5e0f4: a dotless clause after `+` is a qualifier
// on the same candidate, not an independent spec with a base of its own.
// Before the fix every dotless clause compiled to an unknown base
// ("Active"/"isMonarch"/"lifeGE5"...) that matched nobody, so
// `Player.Opponent+Active` (Unstable Glyphbridge's back face) never fired,
// and the NEGATED spellings (`+!IsRemembered`, `+!EnchantedBy`) only ever
// "passed" because the unknown base's fail-closed false was inverted by the
// leading `!` -- they could not go false when the property held.

// TestPlayerSpecDotlessPlusClause pins the dotless `+` reading on real
// qualifiers: each clause is an independent candidate property, conjoined.
func TestPlayerSpecDotlessPlusClause(t *testing.T) {
	g := state.NewGame([]string{"you", "them", "third"})
	src := qualifierObject(t, g, 0, "Name:Source\nTypes:Creature Beast\nPT:2/2\nOracle:x\n")

	// Precondition: the perspective seat (0) is distinct from the active
	// player (1) -- an Opponent+Active candidate can only ever be seat 1.
	g.Active = 1
	if g.Active != 1 || g.Active == 0 {
		t.Fatalf("precondition: active seat must be 1, got %d", g.Active)
	}
	for _, c := range []struct {
		spec string
		seat state.PlayerID
		want bool
	}{
		{"Player.Opponent+Active", 1, true},  // opponent of 0 AND active
		{"Player.Opponent+Active", 0, false}, // active but never an opponent
		{"Player.Opponent+Active", 2, false}, // opponent but not active
		{"Player+Active", 1, true},           // bare Player base conjoins too
		{"Player+Active", 0, false},
	} {
		if got := MatchesPlayerSpecFrom(g, c.spec, c.seat, 0, src.ID); got != c.want {
			t.Errorf("MatchesPlayerSpecFrom(%q, seat %d) = %v, want %v", c.spec, c.seat, got, c.want)
		}
	}

	// NonActive is Active's complement: with seat 1 active, the active
	// opponent seat 1 drops out and nobody matches (seat 0 is not an
	// opponent); with seat 0 active, the non-active opponent seat 1 matches.
	g.Active = 0
	for _, c := range []struct {
		seat state.PlayerID
		want bool
	}{{1, true}, {0, false}} {
		if got := MatchesPlayerSpecFrom(g, "Player.Opponent+NonActive", c.seat, 0, src.ID); got != c.want {
			t.Errorf("NonActive@active0: seat %d = %v, want %v", c.seat, got, c.want)
		}
	}
	g.Active = 1
	for _, c := range []struct {
		seat state.PlayerID
		want bool
	}{{1, false}, {0, false}} {
		if got := MatchesPlayerSpecFrom(g, "Player.Opponent+NonActive", c.seat, 0, src.ID); got != c.want {
			t.Errorf("NonActive@active1: seat %d = %v, want %v", c.seat, got, c.want)
		}
	}

	// isMonarch conjoins through the dotless spelling: the monarch seat (1)
	// qualifies among the opponents; dethroning it (monarch back on the
	// perspective seat) leaves nobody.
	g.HasMonarch, g.Monarch = true, 1
	if !g.IsMonarch(1) || g.IsMonarch(0) {
		t.Fatalf("precondition: monarch must be seat 1, got %v", g.Monarch)
	}
	for _, c := range []struct {
		seat state.PlayerID
		want bool
	}{{1, true}, {0, false}, {2, false}} {
		if got := MatchesPlayerSpecFrom(g, "Player.Opponent+isMonarch", c.seat, 0, src.ID); got != c.want {
			t.Errorf("isMonarch@1: seat %d = %v, want %v", c.seat, got, c.want)
		}
	}
	g.HasMonarch, g.Monarch = true, 0
	if got := MatchesPlayerSpecFrom(g, "Player.Opponent+isMonarch", 1, 0, src.ID); got {
		t.Error("isMonarch@0: seat 1 matched, want false")
	}

	// A literal dotless compare qualifier (the spelling
	// MatchesPlayerSpecWithSVars already rewrites to) conjoins too. The
	// lives must actually differ or the pin proves nothing.
	g.Players[0].Life, g.Players[1].Life, g.Players[2].Life = 4, 5, 4
	if g.Players[0].Life == g.Players[1].Life {
		t.Fatal("precondition: seat 0 and seat 1 lives must differ")
	}
	for _, c := range []struct {
		seat state.PlayerID
		want bool
	}{{1, true}, {0, false}, {2, false}} {
		if got := MatchesPlayerSpecFrom(g, "Player+lifeGE5", c.seat, 0, src.ID); got != c.want {
			t.Errorf("lifeGE5: seat %d = %v, want %v", c.seat, got, c.want)
		}
	}

	// The negated spellings, now evaluated FOR REAL: the remembered/
	// enchanted opponent is excluded, the untouched one admitted.
	src.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	for _, c := range []struct {
		seat state.PlayerID
		want bool
	}{{1, false}, {2, true}, {0, false}} {
		if got := MatchesPlayerSpecFrom(g, "Player.Opponent+!IsRemembered", c.seat, 0, src.ID); got != c.want {
			t.Errorf("!IsRemembered: seat %d = %v, want %v", c.seat, got, c.want)
		}
	}
	for _, c := range []struct {
		seat state.PlayerID
		want bool
	}{{1, true}, {2, true}, {0, false}} {
		if got := MatchesPlayerSpecFrom(g, "Player.Opponent+!EnchantedBy", c.seat, 0, src.ID); got != c.want {
			t.Errorf("!EnchantedBy (aura-free): seat %d = %v, want %v", c.seat, got, c.want)
		}
	}
	// Precondition: the Aura must really enchant seat 1 (an enchantment on
	// the battlefield with AttachedPlayer 1) -- the case the accidental
	// inverted pass could never represent.
	aura := qualifierObject(t, g, 0, "Name:Curse\nTypes:Enchantment Aura\nOracle:x\n")
	aura.HasAttachedPlayer = true
	aura.AttachedPlayer = 1
	if !aura.Face().IsEnchantment() || aura.AttachedPlayer != 1 {
		t.Fatalf("precondition: the aura must enchant seat 1: %+v", aura.Face())
	}
	if MatchesPlayerSpecFrom(g, "Player.Opponent+!EnchantedBy", 1, 0, src.ID) {
		t.Error("!EnchantedBy: the enchanted opponent still matched, want false")
	}
	if !MatchesPlayerSpecFrom(g, "Player.Opponent+!EnchantedBy", 2, 0, src.ID) {
		t.Error("!EnchantedBy: the untouched opponent stopped matching, want true")
	}
	if !MatchesPlayerSpecFrom(g, "Player.Opponent+EnchantedBy", 1, 0, src.ID) {
		t.Error("EnchantedBy: the enchanted opponent must match through the dotless spelling")
	}
}
