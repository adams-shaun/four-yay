package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// d8Req finds name's level-B requirement for key, so the test builds the same
// level-B scenario the D8 diverge rows carry.
func d8Req(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return levelb.Requirement{}
}

// TestD8AllColourTokens pins the `Colors:all` token scripts. The two tokens
// the D8 rows name (The Wandering Minstrel's Elemental, Dragonbroods'
// Relic's Reliquary Dragon) spell their colours `Colors:all`, which
// effects.ColorMaskOf used to ignore, so gorge's snapshot showed a
// colourless token where XMage and the token script both say all five.
func TestD8AllColourTokens(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, token string
	}{
		{"The Wandering Minstrel", "trigger#0.0", "Elemental Token"},
		{"Dragonbroods' Relic", "activate#0.1", "Reliquary Dragon"},
	} {
		it, skip := GenerateB(reg, tc.name, d8Req(t, reg, tc.name, tc.key))
		if skip != nil {
			t.Fatalf("%s %s: %s", tc.name, tc.key, skip.Reason)
		}
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
			t.Fatalf("%s %s does not replay: err=%v fails=%v", tc.name, tc.key, err, res.Fails)
		}
		found := false
		for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
			if p.Name != tc.token {
				continue
			}
			found = true
			if p.Colors != "WUBRG" {
				t.Errorf("%s %s: token colours = %q, want WUBRG", tc.name, tc.key, p.Colors)
			}
		}
		if !found {
			t.Fatalf("precondition: %s %s created no %s token", tc.name, tc.key, tc.token)
		}
	}
}

// TestD8SpellCastTriggeredPlayer pins a SpellCast trigger's TriggeredPlayer:
// Adrenaline Jockey's "if it's not their turn, this creature deals 4 damage
// to them" names the caster, which the trigger context now binds. Before the
// binding the referent was empty, so the trigger resolved for no damage.
func TestD8SpellCastTriggeredPlayer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Adrenaline Jockey"
	it, skip := GenerateB(reg, name, d8Req(t, reg, name, "trigger#0.0"))
	if skip != nil {
		t.Fatalf("%s trigger#0.0: %s", name, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("%s does not replay: err=%v fails=%v", name, err, res.Fails)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	onBattlefield := false
	for _, p := range last.Permanents {
		if p.Name == name {
			onBattlefield = true
		}
	}
	if !onBattlefield {
		t.Fatalf("precondition: %s is not on the battlefield in the last snapshot", name)
	}
	for _, p := range last.Players {
		switch p.Seat {
		case 0:
			if p.Life != 18 {
				t.Errorf("p0 life = %d, want 18 (the Shock resolved)", p.Life)
			}
		case 1:
			if p.Life != 16 {
				t.Errorf("p1 life = %d, want 16 (Adrenaline Jockey's trigger hit the caster)", p.Life)
			}
		}
	}
}
