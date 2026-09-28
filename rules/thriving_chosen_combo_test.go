package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The "Produced$ Combo <letter> Chosen" family (the five Thriving lands and
// five gates: "{T}: Add {R} or one mana of the chosen color") pinned end to
// end on REAL corpus cards (Thriving Bluff, Citadel Gate) plus inline
// fixtures for the shapes no corpus card carries (a multi-ability wheel with
// a Combo-Chosen ability, and the nothing-recorded fail-closed guard).
// Corpus scripts are gitignored GPL text and are never inlined.

// TestThrivingBluffEntryRecordsFallbackAndAsksFixedOrChosen pins the whole
// land-drop flow on the real corpus card: the ETBReplacement ChooseColor
// entry records the deterministic fallback "W" (the cast-time ask does not
// run for a land drop -- a documented gap, see the report's Issues), the
// activation's stage-2 colour ask offers exactly the fixed letter and the
// recorded colour in the ability's own token order ("Add R", "Add W"), and
// the answered pip pools exactly one mana with no colourless. The land
// enters tapped (its own ETBTapped replacement), so the test untaps it
// before activating.
func TestThrivingBluffEntryRecordsFallbackAndAsksFixedOrChosen(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{corpusCard(t, "Thriving Bluff")}, nil)
	bluff := moveCorpusCard(t, e, "Thriving Bluff", 0, state.ZBattlefield)
	if got := e.G.Obj(bluff).ChosenColor; got != "W" {
		t.Fatalf("Thriving Bluff ChosenColor = %q, want the deterministic fallback W", got)
	}
	if !e.G.Obj(bluff).Tapped {
		t.Fatal("Thriving Bluff did not enter tapped")
	}
	e.emit(events.Event{Kind: events.Untap, Obj: bluff})
	addMana(t, e, 0, "")
	activateMana(t, e, bluff)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Combo R Chosen decision = %+v, want a 2-option colour ask", d)
	}
	labels := []string{d.Options[0].Label, d.Options[1].Label}
	if labels[0] != "Add R" || labels[1] != "Add W" {
		t.Fatalf("options %v, want [Add R Add W] (fixed letter first, ability token order)", labels)
	}
	submitChoices(t, e, manaOption(t, d, "W"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MW] != 1 || pool[state.MR] != 0 || pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want exactly one white and nothing else", pool)
	}
	replayCheck(t, e, cfg)
}

// TestThrivingBluffChosenEqualsFixedResolvesDirectly pins the single-colour
// shortcut: with the recorded as-enters colour EQUAL to the fixed letter
// (hand-planted Choose "color" event for R on the real card -- the driver
// the brief offers for this branch), the substitution yields "Combo R", a
// decision nobody could answer differently, so the activation resolves
// directly with no stage-2 ask and pools one red.
func TestThrivingBluffChosenEqualsFixedResolvesDirectly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{corpusCard(t, "Thriving Bluff")}, nil)
	bluff := moveCorpusCard(t, e, "Thriving Bluff", 0, state.ZBattlefield)
	e.emit(events.Event{Kind: events.Choose, Obj: bluff, Counter: "color", Text: "R"})
	e.emit(events.Event{Kind: events.Untap, Obj: bluff})
	addMana(t, e, 0, "")
	activateMana(t, e, bluff)
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("chosen == fixed posed an extra decision: %+v", e.Pending())
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MR] != 1 || pool[state.MW] != 0 || pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want exactly one red", pool)
	}
	replayCheck(t, e, cfg)
}

// TestCitadelGateFallbackRespectsTheExcludedColour pins the no-host entry
// fallback on a real corpus card: Exclude$ white makes Blue the first legal
// colour, rather than the old forbidden White. Its Combo W Chosen activation
// then offers both distinct pips; the chosen Blue produces blue mana.
func TestCitadelGateFallbackRespectsTheExcludedColour(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{corpusCard(t, "Citadel Gate")}, nil)
	gate := moveCorpusCard(t, e, "Citadel Gate", 0, state.ZBattlefield)
	if o := e.G.Obj(gate); o.Zone != state.ZBattlefield || o.ChosenColor != "U" {
		t.Fatalf("Citadel Gate zone=%s ChosenColor=%q, want battlefield and U (Exclude$ white)", o.Zone, o.ChosenColor)
	}
	e.emit(events.Event{Kind: events.Untap, Obj: gate})
	addMana(t, e, 0, "")
	activateMana(t, e, gate)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Label != "Add W" || d.Options[1].Label != "Add U" {
		t.Fatalf("Combo W Chosen options = %+v, want distinct W and U pips", d)
	}
	submitChoices(t, e, manaOption(t, d, "U"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MU] != 1 || pool[state.MW] != 0 || pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want exactly one blue", pool)
	}
	replayCheck(t, e, cfg)
}

// comboChosenSource is an inline fixture (never corpus text): a land with
// TWO mana abilities, the second one the Combo-Chosen shape, so the stage-1
// wheel opens -- the shape no corpus carrier has (all ten carry a single
// ability, which resolves without the wheel).
const comboChosenSource = "Name:Chosen Land\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G\n" +
	"A:AB$ Mana | Cost$ T | Produced$ Combo R Chosen\nOracle:x\n"

// TestThrivingChosenNothingRecordedKeepsRawLabelAndFailsClosed pins the
// fail-closed guard: with nothing recorded the wheel keeps the raw jargon
// label (the documented pre-fix behaviour, unchanged for this branch) and
// the resolution emits the loud "unhandled Produced$" Note with NO ManaAdd.
func TestThrivingChosenNothingRecordedKeepsRawLabelAndFailsClosed(t *testing.T) {
	t.Parallel()
	e, cfg, id := manaSourceEngine(t, comboChosenSource)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("wheel = %+v, want two ability options", d)
	}
	raw := -1
	for _, o := range d.Options {
		if o.Label == "Add Combo R Chosen" {
			raw = o.Index
		}
	}
	if raw < 0 {
		t.Fatalf("raw label not offered with nothing recorded: %+v", d.Options)
	}
	submitChoices(t, e, raw)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool = %d after the fail-closed resolution, want 0", got)
	}
	note := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unhandled Produced$ R Chosen") {
			note = true
		}
		if ev.Kind == events.ManaAdd {
			t.Fatalf("fail-closed resolution emitted a ManaAdd: %+v", ev)
		}
	}
	if !note {
		t.Fatal("no loud unhandled Produced$ note")
	}
	replayCheck(t, e, cfg)
}

// TestThrivingChosenWheelFlattensToFixedAndChosenPips pins the stage-1 wheel
// on the inline two-ability fixture with a planted W (recorded != fixed):
// the Combo-Chosen ability flattens into "Add R" and "Add W" pips in the
// ability's own token order (next to the plain {G} ability's "Add G"), and
// answering a pip resolves directly with the cost paid once and no stage-2
// ask.
func TestThrivingChosenWheelFlattensToFixedAndChosenPips(t *testing.T) {
	t.Parallel()
	e, cfg, id := manaSourceEngine(t, comboChosenSource)
	e.emit(events.Event{Kind: events.Choose, Obj: id, Counter: "color", Text: "W"})
	addMana(t, e, 0, "")
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 {
		t.Fatalf("wheel = %+v, want three pips (G, R, W)", d)
	}
	var labels []string
	for _, o := range d.Options {
		labels = append(labels, o.Label)
	}
	if strings.Join(labels, ",") != "Add G,Add R,Add W" {
		t.Fatalf("wheel labels %v, want [Add G Add R Add W] (no raw token anywhere)", labels)
	}
	submitChoices(t, e, manaOption(t, d, "W"))
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("flattened answer opened a stage-2 ask: %+v", e.Pending())
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MW] != 1 || pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want exactly one white", pool)
	}
	replayCheck(t, e, cfg)
}

// TestThrivingChosenWheelChosenEqualsFixedShowsAddLetter pins the wheel's
// single-colour branch: with the recorded colour EQUAL to the fixed letter
// the ability does not flatten and its label is the plain "Add R" pip, whose
// answer resolves directly (no stage-2), pooling one red.
func TestThrivingChosenWheelChosenEqualsFixedShowsAddLetter(t *testing.T) {
	t.Parallel()
	e, cfg, id := manaSourceEngine(t, comboChosenSource)
	e.emit(events.Event{Kind: events.Choose, Obj: id, Counter: "color", Text: "R"})
	addMana(t, e, 0, "")
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("wheel = %+v, want two ability options", d)
	}
	var labels []string
	for _, o := range d.Options {
		labels = append(labels, o.Label)
	}
	if strings.Join(labels, ",") != "Add G,Add R" {
		t.Fatalf("wheel labels %v, want [Add G Add R]", labels)
	}
	submitChoices(t, e, manaOption(t, d, "R"))
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("single-colour answer opened a stage-2 ask: %+v", e.Pending())
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MR] != 1 || pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want exactly one red", pool)
	}
	replayCheck(t, e, cfg)
}
