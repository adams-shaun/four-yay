package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const comboRGSource = "Name:Dual Land\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ Combo R G\nOracle:x\n"

// TestManaComboRGAsksOnlyRG is the leaf that FAILED before the fix: a
// Produced$ Combo R G source was walked one rune at a time, so tapping it
// added five colourless plus a red and a green. It must now add exactly ONE
// mana of a colour the player chose, with no colourless in the pool, and the
// ask must offer R and G only.
func TestManaComboRGAsksOnlyRG(t *testing.T) {
	t.Parallel()
	e, cfg, id := manaSourceEngine(t, comboRGSource)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("Combo R G decision = %+v, want a 2-option colour choice", d)
	}
	labels := map[string]bool{}
	for _, o := range d.Options {
		if o.Kind != "mana" || o.Obj != id {
			t.Fatalf("colour option %+v is not a mana option for %d", o, id)
		}
		labels[o.Label] = true
	}
	if !labels["Add R"] || !labels["Add G"] {
		t.Fatalf("Combo R G offered %v, want exactly Add R and Add G", labels)
	}
	if len(labels) != 2 {
		t.Fatalf("Combo R G offered the wrong colour set: %v", labels)
	}
	submitChoices(t, e, manaOption(t, d, "G"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MG] != 1 || pool[state.MR] != 0 || pool[state.MC] != 0 {
		t.Fatalf("Combo R G pool = %v, want exactly one green and no colourless", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaComboRGChoosingRedPoolsNoColourless proves the same leaf on the
// other branch: choose R and the pool holds exactly one red.
func TestManaComboRGChoosingRedPoolsNoColourless(t *testing.T) {
	t.Parallel()
	e, _, id := manaSourceEngine(t, comboRGSource)
	activateMana(t, e, id)
	submitChoices(t, e, manaOption(t, e.Pending(), "R"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MR] != 1 || pool[state.MG] != 0 || pool[state.MC] != 0 {
		t.Fatalf("Combo R G (R) pool = %v, want exactly one red and no colourless", pool)
	}
}

// TestManaComboTwoColoursAskIsolatedFromOtherShapes pins that a Combo colour
// ask is the shape's own decision, not the five-colour Any ask: exactly two
// options for "Combo R G".
func TestManaComboTwoColoursOfferExactlyTwo(t *testing.T) {
	t.Parallel()
	e, _, id := manaSourceEngine(t, comboRGSource)
	activateMana(t, e, id)
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Combo R G options = %+v, want exactly two", e.Pending())
	}
	_ = id
}

// TestManaComboThreeColourAsk offers exactly the three named colours on a
// "Combo U B R" shape, proving the arity is parsed rather than hard-coded to
// two.
func TestManaComboThreeColourAsk(t *testing.T) {
	t.Parallel()
	const src = "Name:Tri Land\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Combo U B R\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 {
		t.Fatalf("Combo U B R options = %+v, want three", d)
	}
	labels := map[string]bool{}
	for _, o := range d.Options {
		labels[o.Label] = true
	}
	for _, want := range []string{"Add U", "Add B", "Add R"} {
		if !labels[want] {
			t.Fatalf("Combo U B R missing %s: %v", want, labels)
		}
	}
	submitChoices(t, e, manaOption(t, d, "U"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MU] != 1 || pool[state.MC] != 0 {
		t.Fatalf("Combo U B R pool = %v, want exactly one blue", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaProducedRRSameSymbolStillAddsTwoRed pins that the legitimate rune
// walk is not collateral damage: a literal "RR" still adds exactly two red.
func TestManaProducedRRSameSymbolStillAddsTwoRed(t *testing.T) {
	t.Parallel()
	const src = "Name:RR Land\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ RR\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("literal RR must not ask a colour choice: %+v", d)
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 2 || pool[state.MR] != 2 || pool[state.MC] != 0 {
		t.Fatalf("Produced$ RR pool = %v, want two red and no colourless", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaProducedWUAddsOneOfEach pins the mixed-symbol literal walk too:
// "W U" adds exactly one white and one blue.
func TestManaProducedWUAddsOneOfEach(t *testing.T) {
	t.Parallel()
	const src = "Name:WU Land\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ W U\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	pool := e.G.Players[0].Pool
	if pool.Total() != 2 || pool[state.MW] != 1 || pool[state.MU] != 1 || pool[state.MC] != 0 {
		t.Fatalf("Produced$ W U pool = %v, want one white and one blue", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaProducedAnyStillAsksAllFive pins that Produced$ Any keeps its
// unchanged five-colour ask.
func TestManaProducedAnyStillAsksAllFive(t *testing.T) {
	t.Parallel()
	const src = "Name:Any Land\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Any\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
		t.Fatalf("Produced$ Any decision = %+v, want five colour options", d)
	}
	for _, c := range []string{"W", "U", "B", "R", "G"} {
		manaOption(t, d, c) // fails if any of the five is absent
	}
	submitChoices(t, e, manaOption(t, d, "B"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MB] != 1 || pool[state.MC] != 0 {
		t.Fatalf("Produced$ Any pool = %v, want exactly one black", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaComboAnyStillAsksAllFive pins that "Combo Any" keeps its
// unchanged five-colour ask (it is not treated as a restricted combo).
func TestManaComboAnyStillAsksAllFive(t *testing.T) {
	t.Parallel()
	const src = "Name:Combo Any Land\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Combo Any\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
		t.Fatalf("Combo Any decision = %+v, want five colour options", d)
	}
	for _, c := range []string{"W", "U", "B", "R", "G"} {
		manaOption(t, d, c)
	}
	submitChoices(t, e, manaOption(t, d, "R"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MR] != 1 || pool[state.MC] != 0 {
		t.Fatalf("Combo Any pool = %v, want exactly one red", pool)
	}
	replayCheck(t, e, cfg)
}

// TestKarplusanForestIsTheBugCard names the card the bug was found on: its
// second ability "Combo R G" plus a SubAbility$ DBPain must add exactly one
// mana of a colour the player chose AND still deal its 1 damage to the
// controller, with no colourless from the combo.
func TestKarplusanForestAddsOneChosenColourAndDealsOneDamage(t *testing.T) {
	t.Parallel()
	const src = "Name:Karplusan Forest\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Combo R G | SubAbility$ DBPain\n" +
		"SVar:DBPain:DB$ DealDamage | NumDmg$ 1 | Defined$ You\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	life := e.G.Players[0].Life
	activateMana(t, e, id)
	// Two distinct mana abilities share one tap cost, so the engine asks
	// which one before the tap -- and the explicit Combo ability is FLATTENED
	// into per-colour options (task fb-20260917T232800Z): the wheel offers
	// "Add C", "Add R", "Add G", and the flattened "Add G" answer pays the
	// tap once with no second decision.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Karplusan ability decision = %+v, want a KChoose", d)
	}
	labels := map[string]bool{}
	for _, o := range d.Options {
		if o.Kind != "mana" || o.Obj != id {
			t.Fatalf("stage-1 option %+v is not a mana option for %d", o, id)
		}
		labels[o.Label] = true
	}
	if !labels["Add C"] || !labels["Add R"] || !labels["Add G"] || len(labels) != 3 {
		t.Fatalf("Karplusan stage-1 labels = %v, want exactly Add C, Add R and Add G", labels)
	}
	submitChoices(t, e, manaOption(t, d, "G"))
	d = e.Pending()
	if d != nil && d.Kind == decision.KChoose {
		t.Fatalf("flattened answer still posed a stage-2 ask: %+v", d)
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MG] != 1 || pool[state.MC] != 0 {
		t.Fatalf("Karplusan pool = %v, want exactly one green, no colourless", pool)
	}
	if got := e.G.Players[0].Life; got != life-1 {
		t.Fatalf("Karplusan controller life = %d, want %d (one damage)", got, life-1)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("Karplusan Forest not tapped after activation")
	}
	replayCheck(t, e, cfg)
}

// TestKarplusanFirstAbilityStillAddsColourless pins the first Karplusan
// ability is untouched: tapping for the plain "C" adds one colourless.
func TestKarplusanFirstAbilityStillAddsColourless(t *testing.T) {
	t.Parallel()
	const src = "Name:Karplusan Forest\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Combo R G | SubAbility$ DBPain\n" +
		"SVar:DBPain:DB$ DealDamage | NumDmg$ 1 | Defined$ You\nOracle:x\n"
	e, _, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	d := e.Pending()
	colourlessIdx := -1
	for _, o := range d.Options {
		if o.Kind == "mana" && o.Obj == id && o.Label == "Add C" {
			colourlessIdx = o.Index
		}
	}
	if colourlessIdx < 0 {
		t.Fatalf("no Produced$ C ability offered: %+v", d.Options)
	}
	submitChoices(t, e, colourlessIdx)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("plain C ability must not ask a colour choice: %+v", d)
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MC] != 1 {
		t.Fatalf("Karplusan C pool = %v, want one colourless", pool)
	}
}
