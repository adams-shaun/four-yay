package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ChangeType$ "EACH Forest & Plains" used to reach the filter matcher as one
// unknown base, so Krosan Verge's activation resolved as a silent
// fail-to-find plus a shuffle, every time. These leaves pin the fixed
// behaviour end to end on the REAL compiled corpus card (the reported
// carrier; in no repo deck, so no chain head depends on it).

// vergeEngine deals seat 0 Krosan Verge plus ten Forests (and, when
// withPlains, ten Plainses), with Grizzly Bears padding the deck and the
// opponent's, and drives to seat 0's Main1.
func vergeEngine(t *testing.T, reg *cards.Registry, withPlains bool) (*Engine, Config) {
	t.Helper()
	deck := []*cards.Card{searchCorpusCard(t, reg, "Krosan Verge")}
	forest := searchCorpusCard(t, reg, "Forest")
	for i := 0; i < 10; i++ {
		deck = append(deck, forest)
		if withPlains {
			deck = append(deck, searchCorpusCard(t, reg, "Plains"))
		}
	}
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: 9301, Names: []string{"verge", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// activateVerge lands Krosan Verge untapped and funds {2}: the land enters
// tapped (its own ETBTapped replacement) and is untapped again, then the
// colorless pool is filled before the priority round that offers the
// activation. The {T} is paid by the source itself and the Sac<1/CARDNAME>
// is its sole candidate, so neither asks.
func activateVerge(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	verge := searchMoveByName(t, e, "Krosan Verge", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Untap, Obj: verge})
	addMana(t, e, 0, "CC")
	return activateSearch(t, e, verge)
}

// firstGroupIndex returns the first option index carrying group, or -1.
func firstGroupIndex(d *decision.Decision, group string) int {
	for _, o := range d.Options {
		if o.Group == group {
			return o.Index
		}
	}
	return -1
}

// TestKrosanVergeFetchesAForestAndAPlainsTapped is the reported scenario:
// the activation offers one pick per listed type and the answered Forest and
// Plains both enter the battlefield tapped, then the library shuffles.
func TestKrosanVergeFetchesAForestAndAPlainsTapped(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := vergeEngine(t, reg, true)
	d := activateVerge(t, e)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("after activating Krosan Verge: %+v, want a KChoose search", d)
	}
	if d.Player != 0 {
		t.Fatalf("search player = %d, want the controller (0)", d.Player)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("search range = %d..%d, want 0..2 (one per listed type; CR 701.23b keeps Min 0)", d.Min, d.Max)
	}
	if len(d.Options) < 2 {
		t.Fatalf("%d options, want at least one Forest and one Plains: %+v", len(d.Options), d.Options)
	}
	idxF := firstGroupIndex(d, "0")
	idxP := firstGroupIndex(d, "1")
	if idxF < 0 || idxP < 0 {
		t.Fatalf("options lack the two type groups: %+v", d.Options)
	}
	// Group exclusivity: two picks from one type are refused on the wire.
	second := -1
	seen := 0
	for _, o := range d.Options {
		if o.Group == "0" {
			seen++
			if seen == 2 {
				second = o.Index
			}
		}
	}
	if second >= 0 {
		if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idxF, second}}); err == nil {
			t.Fatal("two Forest picks accepted: the Group contract must refuse one-per-type violations")
		}
	}
	submitChoices(t, e, idxF, idxP)
	passUntilStackEmpty(t, e, 20)

	var fetchedF, fetchedP bool
	for _, ev := range e.L.Events {
		if ev.Kind != events.Tap || ev.Text != "entered tapped" {
			continue
		}
		if name := objName(t, e, ev.Obj); name == "Forest" {
			fetchedF = true
		} else if name == "Plains" {
			fetchedP = true
		}
	}
	if !fetchedF || !fetchedP {
		t.Fatalf("fetched forest=%v plains=%v: a Forest AND a Plains must enter tapped", fetchedF, fetchedP)
	}
	if o := e.G.Obj(d.Options[idxF].Obj); o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
		t.Fatalf("the answered Forest = %+v, want a tapped battlefield permanent", o)
	}
	if o := e.G.Obj(d.Options[idxP].Obj); o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
		t.Fatalf("the answered Plains = %+v, want a tapped battlefield permanent", o)
	}
	if o := e.G.Obj(d.Source); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("Krosan Verge stayed on the battlefield: the Sac<1/CARDNAME> cost must have sacrificed it")
	}
	replayCheck(t, e, cfg)
}

// TestKrosanVergeWithNoPlainsOffersOnlyForests pins the missing-type shape:
// a library of Forests and no Plains offers only the Forest group (Max 1),
// and the answered Forest still moves.
func TestKrosanVergeWithNoPlainsOffersOnlyForests(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := vergeEngine(t, reg, false)
	d := activateVerge(t, e)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("after activating Krosan Verge: %+v, want a KChoose search", d)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("search range = %d..%d, want 0..1 (only the Forest type has candidates)", d.Min, d.Max)
	}
	for _, o := range d.Options {
		if o.Group != "0" {
			t.Fatalf("option %+v carries group %q, want only the Forest group \"0\"", o, o.Group)
		}
		if name := objName(t, e, o.Obj); name != "Forest" {
			t.Fatalf("option %q in a Forest-only search", name)
		}
	}
	submitChoices(t, e, firstGroupIndex(d, "0"))
	passUntilStackEmpty(t, e, 20)
	moved := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield && objName(t, e, ev.Obj) == "Forest" {
			moved++
		}
	}
	if moved == 0 {
		t.Fatal("the answered Forest did not move onto the battlefield")
	}
}

// TestKrosanVergeWithNeitherTypeFailsToFindSilently keeps the no-ask
// fail-to-find: a library holding neither a Forest nor a Plains asks nothing
// (no KChoose DecisionAsk), still shuffles, and moves nothing.
func TestKrosanVergeWithNeitherTypeFailsToFindSilently(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := vergeEngineNeither(t, reg)
	verge := searchMoveByName(t, e, "Krosan Verge", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Untap, Obj: verge})
	addMana(t, e, 0, "CC")
	idx := changeZoneAbilityIndex(t, e, verge)
	submitChoices(t, e, abilityOption(t, e, verge, idx).Index)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events {
		if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KChoose) {
			t.Fatalf("a KChoose was posed on an all-types-empty search: %+v", ev)
		}
	}
	if o := e.G.Obj(verge); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Krosan Verge = %+v, want sacrificed to the graveyard", o)
	}
	moved := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield && ev.From == state.ZLibrary {
			moved++
		}
	}
	if moved != 0 {
		t.Fatalf("%d cards moved from the library onto the battlefield on a fail-to-find", moved)
	}
	if o := e.G.Zone(state.ZLibrary, 0); len(o) > 0 && e.G.Obj(o[0]) == nil {
		t.Fatal("library corrupted")
	}
}

// vergeEngineNeither is vergeEngine with neither a Forest nor a Plains in
// the deck: the library holds bears only, so the EACH search has no
// candidates at all.
func vergeEngineNeither(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	deck := []*cards.Card{searchCorpusCard(t, reg, "Krosan Verge")}
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: 9301, Names: []string{"verge", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}
