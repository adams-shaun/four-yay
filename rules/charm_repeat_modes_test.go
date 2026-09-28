package rules

// CanRepeatModes$ True (CR 601.2b's "you may choose the same mode more than
// once"): the modal pick becomes an ordered multiset over the distinct
// Choices$ modes, so one mode can fill several of the CharmNum$ slots. The
// pin is Fiery Confluence -- the corpus carrier whose oracle is literally
// "Choose three. You may choose the same mode more than once." -- driven end
// to end through the cast-time announcement (castModeAsk), the cast's own
// record (ChosenModes), and resolution's effCharm re-run. Three picks of the
// damage-opponents mode must deal 2 damage to each opponent three times.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fieryConfluenceEngine seeds the REAL corpus Fiery Confluence into seat 0's
// deck (so genesis creates it and the log replays byte-for-byte), bridges it
// into hand, and returns the engine, the Config its log replays against, and
// the card's id. This mirrors newFixtureDeck's fixture-seeding path; the card
// itself comes from the corpus so the exact shipped parameters are what the
// engine defends.
func fieryConfluenceEngine(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	fiery, ok := testutil.CorpusRegistry(t).Lookup("Fiery Confluence")
	if !ok {
		t.Fatal("corpus missing Fiery Confluence")
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{fiery}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == "Fiery Confluence" {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == "Fiery Confluence" {
				id = cand
			}
		}
		if id == 0 {
			t.Fatal("Fiery Confluence not found in seat 0's hand or library")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	return e, cfg, id
}

// TestFieryConfluenceRepeatsAMode pins CanRepeatModes$ True end to end on the
// corpus card. The engine must offer the repeatable KModes (Repeatable true,
// Min == Max == CharmNum 3), accept the SAME option index three times
// (Decision.Validate's no-duplicate rule relaxed only for this ask), and run
// the mode three times at resolution.
func TestFieryConfluenceRepeatsAMode(t *testing.T) {
	t.Parallel()
	e, cfg, id := fieryConfluenceEngine(t, 1)
	addMana(t, e, 0, "RRRR") // 2 R R

	// Cast through the priority window's own offer so castModeAsk runs.
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the cast-time KModes announcement, got %+v", d)
	}
	if !d.Repeatable {
		t.Fatal("Fiery Confluence's cast-mode ask is not Repeatable -- CanRepeatModes$ True is unread")
	}
	if d.Min != 3 || d.Max != 3 {
		t.Fatalf("mode bounds = %d..%d, want 3..3 (CharmNum$ 3)", d.Min, d.Max)
	}
	// DBDestroy (ValidTgts$ Artifact) has no legal target on this empty
	// board, so only the two targetless damage modes are offered -- and the
	// repeatable ask must still allow all three slots to be filled.
	if len(d.Options) != 2 {
		t.Fatalf("eligible mode options = %d, want 2 (destroy-artifact mode has no target): %+v",
			len(d.Options), d.Options)
	}
	opp := -1
	for _, o := range d.Options {
		if o.Label == "CARDNAME deals 2 damage to each opponent." {
			opp = o.Index
		}
	}
	if opp < 0 {
		t.Fatalf("damage-opponents mode not offered: %+v", d.Options)
	}

	// The repeated pick: the SAME index three times. Pre-fix this intent is
	// rejected by Validate ("duplicate choice"), which is exactly the bug.
	start := e.G.Players[1].Life
	submitChoices(t, e, opp, opp, opp)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[1].Life; got != start-6 {
		t.Fatalf("opponent life = %d, want %d (the mode must resolve three times, 2 damage each)", got, start-6)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Errorf("Fiery Confluence resolved to %s, want Graveyard", z)
	}
	// The ModeChosen marker records the three picks in answer order --
	// duplicates preserved -- and replay re-derives them byte-for-byte.
	labels := modeChosenLabels(t, e)
	if len(labels) != 3 || labels[0] != labels[1] || labels[1] != labels[2] {
		t.Fatalf("ModeChosen = %v, want the same mode label three times", labels)
	}
	replayCheck(t, e, cfg)
}

// modeChosenLabels recovers the most recent ModeChosen payload (the option
// labels, comma-joined) from the log for the assertion above.
func modeChosenLabels(t *testing.T, e *Engine) []string {
	t.Helper()
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.ModeChosen && ev.Text != "" {
			return strings.Split(ev.Text, ",")
		}
	}
	return nil
}

// mysticConfluenceEngine seeds the REAL corpus Mystic Confluence into seat
// 0's hand plus three Grizzly Bears (also corpus cards, so the exact shipped
// ValidTgts$ Creature spec is what the resolution asks against), bridges the
// bears onto the battlefield, and returns the engine, the Config its log
// replays against, the spell's id and the three bears' ids in hand order.
func mysticConfluenceEngine(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	mystic, ok := reg.Lookup("Mystic Confluence")
	if !ok {
		t.Fatal("corpus missing Mystic Confluence")
	}
	bear, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("corpus missing Grizzly Bears")
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{mystic, bear, bear, bear}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()

	var id state.ObjID
	var bears []state.ObjID
	var needHand, needBattle []state.ObjID
	for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, cand := range e.G.Zone(zone, 0) {
			switch e.G.Obj(cand).Face().Name {
			case "Mystic Confluence":
				id = cand
				if zone == state.ZLibrary {
					needHand = append(needHand, cand)
				}
			case "Grizzly Bears":
				bears = append(bears, cand)
				if zone == state.ZLibrary {
					needBattle = append(needBattle, cand)
				}
			}
		}
	}
	if id == 0 || len(bears) != 3 {
		t.Fatalf("fixture not dealt as expected: mystic %d bears %d", id, len(bears))
	}
	for _, b := range needHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: b, From: state.ZLibrary, To: state.ZHand})
	}
	for _, b := range needBattle {
		e.emit(events.Event{Kind: events.MoveZone, Obj: b, From: state.ZLibrary, To: state.ZBattlefield})
	}
	// Bears dealt straight into the opening hand move from there.
	for _, b := range bears {
		if o := e.G.Obj(b); o != nil && o.Zone == state.ZHand {
			e.emit(events.Event{Kind: events.MoveZone, Obj: b, From: state.ZHand, To: state.ZBattlefield})
		}
	}
	e.pending = nil
	e.Advance()
	return e, cfg, id, bears
}

// TestMysticConfluenceRepeatsTargetedModeAsksPerInstance pins the finding the
// Fiery Confluence pin could not reach: a REPEATED TARGET-BEARING mode. The
// announcement ask covers the mode's FIRST instance (one creature target);
// every later instance must keep its own targeting ask -- chosen per instance
// -- rather than silently re-running against the one shared target (which
// returned ONE bear for a three-pick answer). Three picks of the return mode
// must return three distinct creatures.
func TestMysticConfluenceRepeatsTargetedModeAsksPerInstance(t *testing.T) {
	t.Parallel()
	e, cfg, id, bears := mysticConfluenceEngine(t, 1)
	addMana(t, e, 0, "UUUUU") // 3 U U

	// Cast through the priority window's own offer; the cast-time ask that
	// follows the KModes announcement is the mode's FIRST instance target.
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the cast-time KModes announcement, got %+v", d)
	}
	if !d.Repeatable {
		t.Fatal("Mystic Confluence's cast-mode ask is not Repeatable")
	}
	ret := -1
	for _, o := range d.Options {
		if o.Label == "Return target creature to its owner's hand." {
			ret = o.Index
		}
	}
	if ret < 0 {
		t.Fatalf("return mode not offered: %+v", d.Options)
	}
	// Three picks of the SAME target-bearing mode.
	submitChoices(t, e, ret, ret, ret)

	// Instance 1: the cast announcement's own target ask (KTarget).
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("instance 1 ask = %+v, want the announcement KTarget", dt)
	}
	if dt.Min != 1 || dt.Max != 1 {
		t.Fatalf("instance 1 bounds %d..%d, want 1..1 (single-target mode)", dt.Min, dt.Max)
	}
	bearIdx := func(d *decision.Decision, bear state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == bear {
				return o.Index
			}
		}
		return -1
	}
	i1 := bearIdx(dt, bears[0])
	if i1 < 0 {
		t.Fatalf("bear 0 not offered: %+v", dt.Options)
	}
	submitChoices(t, e, i1)

	// Instances 2 and 3: their own mid-resolution asks (KChoose over the
	// remaining battlefield creatures), one per instance, in walk order.
	for k, bear := range []state.ObjID{bears[1], bears[2]} {
		dc := passUntilAsk(t, e)
		if dc == nil || dc.Kind != decision.KChoose {
			t.Fatalf("instance %d ask = %+v, want a KChoose", k+2, dc)
		}
		idx := bearIdx(dc, bear)
		if idx < 0 {
			t.Fatalf("instance %d: bear %d not offered: %+v", k+2, k+1, dc.Options)
		}
		submitChoices(t, e, idx)
	}

	passUntilStackEmpty(t, e, 20)
	// All three bears are back in seat 0's hand -- each instance returned its
	// OWN target, not one shared target three times.
	for k, bear := range bears {
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
			t.Fatalf("bear %d (obj %d) in %v, want Hand -- instance %d did not return its own target", k, bear, o, k+1)
		}
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Errorf("Mystic Confluence resolved to %s, want Graveyard", z)
	}
	replayCheck(t, e, cfg)
}
