package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The stage-1 "choose a mana ability" labels were built straight from the raw
// Produced$ script token (fb-e079def5): a Talisman-of-Indulgence-shaped wheel
// showed the jargon "Add Combo B R", which no player can read and which
// defeats the web wheel's mana-pip styling (it tints a list only when EVERY
// label is a single "Add <C>"). manaAbilityLabel renders them for humans; the
// direct unit table lives in TestManaAbilityLabelShapes below, and these two
// engine tests pin the reported flow end to end.

// TestTalismanOfIndulgenceStageOneFlattensIntoColourPips pins the reported
// card's flow end to end (task fb-20260917T232800Z): Talisman of Indulgence
// carries TWO mana abilities ({T}: Add {C}, and {T}: Add {B} or {R} plus the
// 1-damage SubAbility), and the stage-1 wheel offers THREE options -- "Add
// C", "Add B", "Add R" -- the per-colour pip bubbles the player asked for.
// The explicit Combo ability is flattened into one option per colour (the
// ability's own token order), so answering "Add B" pays the tap once and
// lands exactly one black mana with NO second decision; the 1-damage
// SubAbility still fires (the pin at the bottom of TestKarplusanForestAddsOne
// ChosenColourAndDealsOneDamage covers the SubAbility side on the twin
// shape). This supersedes the pre-flattening "Add C" + "Add B or R" pin: the
// feedback is the newer instruction.
func TestTalismanOfIndulgenceStageOneFlattensIntoColourPips(t *testing.T) {
	t.Parallel()
	const talisman = "Name:Talisman of Indulgence\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Combo B R | SubAbility$ DBPain\n" +
		"SVar:DBPain:DB$ DealDamage | NumDmg$ 1 | Defined$ You\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, talisman)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 {
		t.Fatalf("stage-1 decision = %+v, want a 3-option flattened choose", d)
	}
	for i, want := range []string{"Add C", "Add B", "Add R"} {
		o := d.Options[i]
		if o.Label != want || o.Obj != id || o.Kind != "mana" || o.Index != i {
			t.Fatalf("stage-1 option %d = %+v, want label %q for %d", i, o, want, id)
		}
	}
	if d.Options[1].Ability != 1 || d.Options[2].Ability != 1 {
		t.Fatalf("flattened options carry the combo ability's index: %+v", d.Options)
	}
	// Answering the flattened "Add B" pays the tap once and lands exactly
	// one black mana with NO second decision (the stage-2 ask is gone).
	life := e.G.Players[0].Life
	submitChoices(t, e, 1)
	if d = e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("flattened answer still posed a stage-2 ask: %+v", d)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("pool after choosing the flattened B = %d black, want 1: %+v", got, e.G.Players[0].Pool)
	}
	if total := e.G.Players[0].Pool.Total(); total != 1 {
		t.Fatalf("pool total = %d, want exactly one mana", total)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("source not tapped after the flattened activation")
	}
	if got := e.G.Players[0].Life; got != life-1 {
		t.Fatalf("life = %d, want %d (the combo's 1-damage SubAbility still fired)", got, life-1)
	}
	replayCheck(t, e, cfg)
}

// TestTalismanOfIndulgenceRealCorpusFlattens pins the same flow on the REAL
// compiled corpus card (testutil.CorpusRegistry, the Talisman of Indulgence
// script itself), driven through the ordinary priority action rather than the
// synthetic activateMana helper: the stage-1 wheel offers exactly "Add C",
// "Add B", "Add R", and the "Add B" answer taps once, lands one black and
// opens no stage-2 ask.
func TestTalismanOfIndulgenceRealCorpusFlattens(t *testing.T) {
	t.Parallel()
	tal, ok := testutil.CorpusRegistry(t).Lookup("Talisman of Indulgence")
	if !ok {
		t.Fatal("corpus missing Talisman of Indulgence")
	}
	e := layerEngine(t)
	id := onBoardCard(t, e, 0, tal)
	e.askPriority(0)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 {
		t.Fatalf("real-corpus stage-1 decision = %+v, want 3 flattened options", d)
	}
	labels := map[string]bool{}
	for _, o := range d.Options {
		if o.Obj != id || o.Kind != "mana" {
			t.Fatalf("stage-1 option %+v is not a mana option for %d", o, id)
		}
		labels[o.Label] = true
	}
	if !labels["Add C"] || !labels["Add B"] || !labels["Add R"] || len(labels) != 3 {
		t.Fatalf("real-corpus stage-1 labels = %v, want exactly Add C, Add B, Add R", labels)
	}
	submitChoices(t, e, manaOption(t, d, "B"))
	if d = e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("flattened answer still posed a stage-2 ask: %+v", d)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("real-corpus pool after Add B = %d black, want 1: %+v", got, e.G.Players[0].Pool)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("real Talisman not tapped after the flattened activation")
	}
}

// TestManaProducedAnyStageOneLabel pins the Any shape's stage-1 label on a
// two-ability source (a one-ability Any source never opens the wheel -- it
// goes straight to the five-colour ask). The Any ability is NOT flattened
// (the choice is not enumerable at option-build time), so it keeps the
// single "Add any color" option and the stage-2 five-colour ask still
// follows it (task fb-20260917T232800Z's scope boundary).
func TestManaProducedAnyStageOneLabel(t *testing.T) {
	t.Parallel()
	const src = "Name:Any Source\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Any\nOracle:x\n"
	e, cfg, id := manaSourceEngine(t, src)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("stage-1 decision = %+v, want a 2-option ability choose", d)
	}
	anyIdx := -1
	for _, o := range d.Options {
		if o.Label == "Add Any" {
			t.Fatalf("raw Any token leaked into a stage-1 label: %+v", d.Options)
		}
		if o.Label == "Add any color" {
			anyIdx = o.Index
			continue
		}
		if o.Label != "Add C" {
			t.Fatalf("unexpected stage-1 label %q: %+v", o.Label, d.Options)
		}
	}
	if anyIdx < 0 {
		t.Fatalf("no Add any color option: %+v", d.Options)
	}
	// The Any ability's stage-2 five-colour ask still opens after the
	// stage-1 answer.
	submitChoices(t, e, anyIdx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
		t.Fatalf("stage-2 Any colour decision = %+v, want the five-colour ask", d)
	}
	submitChoices(t, e, manaOption(t, d, "G"))
	if got := e.G.Players[0].Pool[state.MG]; got != 1 {
		t.Fatalf("pool after choosing G = %d green, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaAbilityLabelShapes is the direct unit table over the formatter,
// including the shapes no engine fixture reaches (a three-colour combo, a
// doubled pip, Chosen). The formatter now serves only the NON-flattened
// stage-1 options (an explicit multi-colour Combo is expanded into per-colour
// options before manaAbilityLabel is consulted, task fb-20260917T232800Z),
// but its own output contract is unchanged.
func TestManaAbilityLabelShapes(t *testing.T) {
	t.Parallel()
	cases := []struct{ produced, want string }{
		{"Combo B R", "Add B or R"},
		{"Combo R G", "Add R or G"},
		{"Combo W U", "Add W or U"},
		{"Combo U B R", "Add U, B or R"},
		{"Combo R G W", "Add R, G or W"},
		{"Combo R G ", "Add R or G"}, // trailing space trimmed
		{" Any ", "Add any color"},
		{"Combo Any", "Add any color"},
		{"Chosen", "Add chosen color"},
		{"C", "Add C"},
		{"G", "Add G"},
		{"RR", "Add RR"},
	}
	for _, c := range cases {
		ma := &cards.SA{Kind: "AB", API: "Mana", Params: map[string]string{"Produced": c.produced}}
		if got := manaAbilityLabel(ma, ""); got != c.want {
			t.Errorf("manaAbilityLabel(Produced$ %q) = %q, want %q", c.produced, got, c.want)
		}
	}
	// The formatter and the resolution's colour classifier must agree on
	// which Combo shapes are a real colour list: everything the resolver
	// would ask about renders as a colour list, never as raw jargon.
	if cols, ok := effects.ComboColours("Combo B R"); !ok || len(cols) != 2 {
		t.Fatalf("ComboColours(Combo B R) = %v, %v", cols, ok)
	}
}
