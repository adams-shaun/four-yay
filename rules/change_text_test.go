package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusTextCard requires the exact compiled SA at the pinned corpus revision,
// rather than re-parsing a test approximation of the carrier script.
func corpusTextCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("%s not found in compiled corpus", name)
	}
	return c
}

// TestNewBloodChangeTextEndToEnd casts compiled corpus New Blood and pins its
// ChangeText body end to end: after the cast resolves, the stolen creature's
// text carries the chosen creature type replaced by Vampire. Preconditions
// make the assertion non-vacuous -- the victim is on the battlefield, its
// printed text names the chosen type and not the replacement, and the
// creature-type ask really was posed and answered.
func TestNewBloodChangeTextEndToEnd(t *testing.T) {
	t.Parallel()
	newBlood := corpusTextCard(t, "New Blood")
	vampire := card(t, "Name:Vampire Source\nManaCost:1 B\nTypes:Creature Vampire\nPT:2/2\nOracle:x\n")
	elf := card(t, "Name:Llanowar Elves\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\nOracle:x\n")
	victim := card(t, "Name:Elf Lord\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\nOracle:Other Elf creatures you control get +1/+1.\n")
	e, cfg, _ := corpusDeckEngine(t, []*cards.Card{newBlood}, []*cards.Card{vampire, elf, victim})

	var victimID, vampireID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield {
			continue
		}
		switch o.Card {
		case victim:
			victimID = o.ID
		case vampire:
			vampireID = o.ID
		}
	}
	if victimID == 0 || vampireID == 0 {
		t.Fatalf("setup: victim=%d vampire=%d, both must be on the battlefield", victimID, vampireID)
	}
	// New Blood must actually steal an opponent's permanent, not merely
	// re-resolve GainControl on a creature the caster already controls.
	e.emit(events.Event{Kind: events.ControlChange, Obj: victimID, Player: 1})
	if e.G.Obj(victimID).Controller != 1 || e.G.Obj(vampireID).Controller != 0 {
		t.Fatalf("precondition: victim and Vampire must have opposing controllers")
	}
	printed := e.G.Obj(victimID).Face().Oracle
	if !strings.Contains(printed, "Elf") || strings.Contains(printed, "Vampire") {
		t.Fatalf("precondition: printed text must name Elf and not Vampire, got %q", printed)
	}
	if before := e.Text(victimID); before != printed {
		t.Fatalf("precondition: Text before the cast must be the printed Oracle %q, got %q", printed, before)
	}

	// Fund the {2}{B}{B} cost (generic may be paid with black) and cast.
	addMana(t, e, 0, "BBBB")
	castCardNow(t, e, "New Blood")

	// Drive the cast to completion, answering the additional tap cost (the
	// Vampire), the spell's target (the victim) and the creature-type ask.
	sawTypeAsk := false
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if sawTypeAsk && len(e.G.Stack) == 0 {
			break
		}
		switch d.Kind {
		case decision.KTarget:
			if idx := indexOfObjOption(d, victimID); idx >= 0 {
				submitChoices(t, e, idx)
			} else if idx := indexOfObjOption(d, vampireID); idx >= 0 {
				submitChoices(t, e, idx)
			} else {
				t.Fatalf("target ask offers neither victim nor Vampire: %+v", d.Options)
			}
		case decision.KChoose:
			if strings.HasPrefix(d.Prompt, "Choose the text word") {
				sawTypeAsk = true
				submitTextChoice(t, e, "Elf")
			} else {
				t.Fatalf("unexpected KChoose prompt %q: %+v", d.Prompt, d.Options)
			}
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected pending decision %v (%s)", d.Kind, d.Prompt)
		}
	}
	if !sawTypeAsk {
		t.Fatal("api:ChangeText never posed its creature-type ask")
	}

	if e.G.Obj(victimID).Controller != 0 {
		t.Fatalf("New Blood did not steal the target: controller=%d", e.G.Obj(victimID).Controller)
	}
	got := e.Text(victimID)
	want := "Other Vampire creatures you control get +1/+1."
	if got != want {
		t.Fatalf("New Blood substituted text = %q, want %q", got, want)
	}
	replayCheck(t, e, cfg)
}

// TestChangeTextSubstitutesAChosenColorWord casts compiled Alter Reality's
// ChangeColorWord$ Choose Choose: both words are picked in one combined ask.
func TestChangeTextSubstitutesAChosenColorWord(t *testing.T) {
	t.Parallel()
	recolor := corpusTextCard(t, "Alter Reality")
	target := card(t, "Name:Black Knight\nManaCost:B B\nTypes:Creature Human Knight\nPT:2/2\nOracle:Protection from black.\n")
	e, cfg, _ := corpusDeckEngine(t, []*cards.Card{recolor}, []*cards.Card{target})
	var id state.ObjID
	for i := range e.G.Objs {
		if e.G.Objs[i].Card == target && e.G.Objs[i].Zone == state.ZBattlefield {
			id = e.G.Objs[i].ID
		}
	}
	if id == 0 {
		t.Fatal("setup: no target creature on the battlefield")
	}
	if e.Text(id) != "Protection from black." {
		t.Fatalf("precondition: printed text = %q", e.Text(id))
	}

	addMana(t, e, 0, "UU")
	castCardNow(t, e, "Alter Reality")
	sawAsks := 0
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if sawAsks >= 1 && len(e.G.Stack) == 0 {
			break
		}
		switch d.Kind {
		case decision.KTarget:
			if idx := indexOfObjOption(d, id); idx >= 0 {
				submitChoices(t, e, idx)
			} else {
				t.Fatalf("target ask does not offer the target: %+v", d.Options)
			}
		case decision.KChoose:
			sawAsks++
			fromIdx, toIdx := -1, -1
			for _, o := range d.Options {
				if o.Kind == "changetext_from" && o.Label == "black" {
					fromIdx = o.Index
				}
				if o.Kind == "changetext_to" && o.Label == "white" {
					toIdx = o.Index
				}
			}
			if fromIdx < 0 || toIdx < 0 {
				t.Fatalf("combined ChangeText ask must offer from=black and to=white: %+v", d.Options)
			}
			submitChoices(t, e, fromIdx, toIdx)
		case decision.KPriority:
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected pending decision %v (%s)", d.Kind, d.Prompt)
		}
	}
	if sawAsks != 1 {
		t.Fatalf("ChangeColorWord$ Choose Choose posed %d word asks, want 1 combined ask", sawAsks)
	}
	got := e.Text(id)
	if got != "Protection from white." {
		t.Fatalf("color-word substituted text = %q, want %q", got, "Protection from white.")
	}
	replayCheck(t, e, cfg)
}

// submitTextChoice answers the pending ChangeText KChoose with the option whose
// label is label, proving the ask was posed (a missing pending ask fails loudly
// rather than letting the test pass with the primitive inert).
func submitTextChoice(t *testing.T, e *Engine, label string) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision: api:ChangeText never posed its word ask")
	}
	if d.Kind != decision.KChoose {
		t.Fatalf("pending decision kind = %v, want KChoose", d.Kind)
	}
	for _, o := range d.Options {
		if o.Label == label {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("option %q not offered by the ChangeText ask: %+v", label, d.Options)
}

// changeTextCarriers is every corpus card whose compiled script references
// api:ChangeText or api:ExchangeTextBox (measured at FORGE_REF: 12 ChangeText
// files + 2 ExchangeTextBox files).
var changeTextCarriers = []string{
	"Alter Reality", "Artificial Evolution", "Balduvian Shaman", "Crystal Spray",
	"Glamerdye", "Magical Hack", "Mind Bend", "New Blood", "Sleight of Mind",
	"Spectral Shift", "Trait Doctoring", "Whim of Volrath",
	"Deadpool, Trading Card", "Exchange of Words",
}

// changeTextCarrierGaps is the complete measured Unsupported result for every
// corpus api:ChangeText/api:ExchangeTextBox carrier at FORGE_REF, as a golden:
// a nil entry means the card is fully playable (every carrier's text primitive
// is now registered), and a non-nil entry is the exact remaining gap a
// DIFFERENT primitive owns. It is asserted in full -- empty and non-empty
// alike -- so the gate cannot pass by scoping itself to the text family, and a
// card whose gap later closes (or opens) makes this table stale and fails.
//
// The one non-nil entry is unrelated to text modification. The operator's
// 2026-09-25 ruling explicitly leaves this keyword out of scope here:
//   - Spectral Shift: now fully playable. Its former kw:Entwine gap closed
//     when agent-20260925T000311Z-22c4c559 landed CR 702.42 Entwine (the
//     optional additional cost whose payment forces all Charm modes).
//
// Trait Doctoring's former kw:Cipher gap closed when agent-20260919T181215Z-
// 71ad9572 landed Cipher, so it is now fully playable and reads nil.
var changeTextCarrierGaps = map[string][]string{
	"Alter Reality":          nil,
	"Artificial Evolution":   nil,
	"Balduvian Shaman":       nil,
	"Crystal Spray":          nil,
	"Glamerdye":              nil,
	"Magical Hack":           nil,
	"Mind Bend":              nil,
	"New Blood":              nil,
	"Sleight of Mind":        nil,
	"Whim of Volrath":        nil,
	"Deadpool, Trading Card": nil,
	"Exchange of Words":      nil,
	"Spectral Shift":         nil,
	"Trait Doctoring":        nil,
}

// TestChangeTextCarriersUnsupportedIsExact implements the operator's
// 2026-09-25 registration ruling: assert the COMPLETE Unsupported result for
// all 14 carriers. Twelve must be fully playable; the other two must report
// exactly their unrelated keyword gaps, not any text primitive. A card that
// gains or loses any unsupported primitive makes this table stale and fails.
func TestChangeTextCarriersUnsupportedIsExact(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	// The table must cover every carrier and no extra name, so a corpus rename
	// or a missing carrier entry cannot silently shrink the check.
	if len(changeTextCarrierGaps) != len(changeTextCarriers) {
		t.Fatalf("gap table has %d entries, %d carriers", len(changeTextCarrierGaps), len(changeTextCarriers))
	}
	for _, name := range changeTextCarriers {
		want, ok := changeTextCarrierGaps[name]
		if !ok {
			t.Fatalf("carrier %q has no entry in the gap table", name)
		}
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("carrier %q not found in the corpus", name)
		}
		got := reg.Unsupported(c, supported)
		if !sameStringSet(got, want) {
			t.Errorf("%s Unsupported = %v, want %v", name, got, want)
		}
	}
}

// sameStringSet compares two string lists as sets (order-insensitive, exact
// multiplicity by length).
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

// TestExchangeTextBoxSwapsTwoObjectsText pins api:ExchangeTextBox: after it
// resolves over two objects, each renders the OTHER's printed text. The used
// path is effects.Resolve with both targets already named (the Defined$
// ParentTarget shape the corpus uses), so it isolates the layer-3 TextSet
// registration from the ask machinery.
func TestExchangeTextBoxSwapsTwoObjectsText(t *testing.T) {
	t.Parallel()
	a := card(t, "Name:Alpha\nManaCost:B\nTypes:Creature Human\nPT:1/1\nOracle:Flying.\n")
	b := card(t, "Name:Beta\nManaCost:B\nTypes:Creature Beast\nPT:2/2\nOracle:Trample.\n")
	e, _, _ := corpusDeckEngine(t, nil, []*cards.Card{a, b})
	var aID, bID state.ObjID
	for i := range e.G.Objs {
		switch e.G.Objs[i].Card {
		case a:
			aID = e.G.Objs[i].ID
		case b:
			bID = e.G.Objs[i].ID
		}
	}
	if aID == 0 || bID == 0 || aID == bID {
		t.Fatalf("setup: a=%d b=%d", aID, bID)
	}
	if e.Text(aID) != "Flying." || e.Text(bID) != "Trample." {
		t.Fatalf("precondition: texts = %q / %q", e.Text(aID), e.Text(bID))
	}
	sa := &cards.SA{API: "ExchangeTextBox", Params: map[string]string{"Duration": "AsLongAsInPlay", "Defined": "Targeted"}}
	ctx := &effects.Ctx{Source: aID, Controller: 0, Targets: []state.Target{{Obj: aID}, {Obj: bID}}}
	effects.Resolve(e, ctx, sa)
	if got := e.Text(aID); got != "Trample." {
		t.Fatalf("Alpha text = %q, want Beta's printed text", got)
	}
	if got := e.Text(bID); got != "Flying." {
		t.Fatalf("Beta text = %q, want Alpha's printed text", got)
	}
}
