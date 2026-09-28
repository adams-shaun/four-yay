package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestSurgeRealCorpusRecklessBushwhacker(t *testing.T) {
	t.Parallel()

	boltSrc := "Name:Surge Test Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	goblinSrc := "Name:Surge Test Goblin\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"
	newGame := func(seed uint64) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
		e, _, _ := altCostEngine(t, seed, []string{"Reckless Bushwhacker"}, []string{boltSrc, goblinSrc}, nil)
		reckless := findAndMoveToHand(t, e, 0, "Reckless Bushwhacker")
		bolt := findAndMoveToHand(t, e, 0, "Surge Test Bolt")
		goblin := findCardObj(t, e, 0, "Surge Test Goblin", state.ZBattlefield)
		if face := e.G.Obj(reckless).Face(); face == nil || face.Name != "Reckless Bushwhacker" {
			t.Fatalf("expected real corpus Reckless Bushwhacker, got face %+v", face)
		}
		if e.G.Obj(reckless).Zone != state.ZHand {
			t.Fatalf("Bushwhacker zone = %s, want hand", e.G.Obj(reckless).Zone)
		}
		if e.G.Obj(goblin).Zone != state.ZBattlefield || e.Derived(goblin).Power != 1 {
			t.Fatalf("witness precondition: zone=%s power=%d, want battlefield 1/1", e.G.Obj(goblin).Zone, e.Derived(goblin).Power)
		}
		return e, reckless, bolt, goblin
	}
	modeIndex := func(t *testing.T, e *Engine, id state.ObjID, mode string) int {
		t.Helper()
		for _, option := range castOptions(t, e) {
			if option.Obj == id && option.Mode == mode {
				return option.Index
			}
		}
		return -1
	}

	// A + C: without another spell this turn, only the printed-cost cast is
	// offered; its ETB must not pump the witness creature.
	e, reckless, _, goblin := newGame(991)
	addMana(t, e, 0, "RRR")
	if got := e.spellsCastThisTurn(0); got != 0 {
		t.Fatalf("initial spells cast this turn = %d, want 0", got)
	}
	if got := modeIndex(t, e, reckless, "surged"); got != -1 {
		t.Fatalf("surge offered before another spell (option index %d)", got)
	}
	plain := modeIndex(t, e, reckless, "")
	if plain < 0 {
		t.Fatal("printed-cost Bushwhacker option missing before another spell")
	}
	submitChoices(t, e, plain)
	passUntilStackEmpty(t, e, 20)
	plainPower := e.Derived(goblin).Power
	if plainPower != 1 {
		t.Fatalf("plain cast witness power = %d, want 1 (ETB pump must not fire)", plainPower)
	}

	// B + D: a prior spell opens surge, which pays exactly {1}{R}, carries
	// FlagSurged, and makes the corpus card's surged-only ETB pump observable.
	e2, reckless2, bolt, goblin2 := newGame(992)
	addMana(t, e2, 0, "RRR")
	before := e2.spellsCastThisTurn(0)
	castObj(t, e2, bolt)
	after := e2.spellsCastThisTurn(0)
	if after != before+1 || after == 0 {
		t.Fatalf("spell count did not advance: before=%d after=%d", before, after)
	}
	surged := modeIndex(t, e2, reckless2, "surged")
	if surged < 0 {
		t.Fatal("surged Bushwhacker option missing after casting another spell")
	}
	pool := e2.G.Players[0].Pool.Total()
	submitChoices(t, e2, surged)
	if got := e2.G.Players[0].Pool.Total(); got != pool-2 {
		t.Fatalf("surge payment changed pool from %d to %d, want exactly 2 mana", pool, got)
	}
	if o := e2.G.Obj(reckless2); o == nil || o.CastFlags&state.FlagSurged == 0 {
		t.Fatalf("surged cast lacks FlagSurged: object=%+v", o)
	}
	passUntilStackEmpty(t, e2, 20)
	pumpedPower := e2.Derived(goblin2).Power
	if pumpedPower != 2 || pumpedPower == e2.Derived(goblin2).BasePower {
		t.Fatalf("surged cast witness power = %d (base %d), want changed 2/1", pumpedPower, e2.Derived(goblin2).BasePower)
	}
}
