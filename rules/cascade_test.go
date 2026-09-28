package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Cascade (CR 702.85, task cascade1). The engine deals seat 0 a corpus-only
// deck (no Forge script text is committed here — the helpers come from
// search_library_test.go), arranges the top of its library with a logged
// LibraryOrder event, and drives the whole exile-until + may-cast sequence
// through real cast flows.

// cascadeTestEngine deals seat 0 a 40-card corpus deck whose protagonist is
// spellName (found and moved to hand), puts battlefield names on the
// battlefield in the order given, and sets the library's top to window (in
// order) with a LibraryOrder event — both replay-safe, the
// ao_dawn_sky_budget_test.go pattern.
func cascadeTestEngine(t *testing.T, seed uint64, spellName string, window, battlefield []string) (*Engine, Config) {
	return cascadeTestEngineFiller(t, seed, spellName, window, battlefield, "Grizzly Bears")
}

// cascadeTestEngineFiller is cascadeTestEngine with the filler card named:
// the no-candidate arm needs a library whose every nonland card fails the
// mana-value comparison, so it fills with a basic land instead.
func cascadeTestEngineFiller(t *testing.T, seed uint64, spellName string, window, battlefield []string, filler string) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, filler)
	deck := []*cards.Card{searchCorpusCard(t, reg, spellName)}
	for _, name := range window {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for _, name := range battlefield {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"cascader", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// Protagonist to hand.
	spellID := searchMoveByName(t, e, spellName, state.ZHand)
	_ = spellID
	// Battlefield permanents.
	for _, name := range battlefield {
		searchMoveByName(t, e, name, state.ZBattlefield)
	}
	cascadeArrangeWindow(t, e, window)
	return e, cfg
}

// cascadeArrangeWindow pulls each named card back into seat 0's library (a
// hand card moves in first, the ao_dawn_sky_budget_test.go pattern) and sets
// the whole library order with a LibraryOrder event so the named cards sit
// on top in the given order. Replay-safe: logged moves only.
func cascadeArrangeWindow(t *testing.T, e *Engine, window []string) {
	t.Helper()
	take := func(name string) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
					if z == state.ZHand {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
					}
					return id
				}
			}
		}
		t.Fatalf("window card %q not in seat 0 hand/library", name)
		return 0
	}
	win := make([]state.ObjID, 0, len(window))
	for _, name := range window {
		win = append(win, take(name))
	}
	inWin := make(map[state.ObjID]bool, len(win))
	for _, id := range win {
		inWin[id] = true
	}
	newLib := append([]state.ObjID(nil), win...)
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if !inWin[id] {
			newLib = append(newLib, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: newLib})
	e.pending = nil
	e.priorityRound()
}

// cascadeElection asserts that the pending decision is the cascade free-cast
// election for the named card and returns its option index.
func cascadeElection(t *testing.T, e *Engine, cardName string) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	if d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("want the cascade play election, got kind %v resume %q: %+v", d.Kind, d.ResumeKind, d)
	}
	if len(d.Options) != 1 {
		t.Fatalf("election options: %+v", d.Options)
	}
	o := d.Options[0]
	if o.Obj == 0 || e.G.Obj(o.Obj) == nil || e.G.Obj(o.Obj).Face().Name != cardName {
		t.Fatalf("election option %+v does not name %q", o, cardName)
	}
	return o.Index
}

// movedTo counts the log's library→to moves of id.
func movedTo(t *testing.T, e *Engine, id state.ObjID, from, to state.Zone) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == from && ev.To == to {
			n++
		}
	}
	return n
}

// TestBloodbraidElfCascadeExilesUntilLesserAndOffersFreeCast is the printed
// K:Cascade carrier, end to end on the REAL corpus card: casting Bloodbraid
// Elf (mana value 4) queues its cascade trigger, the trigger resolves ABOVE
// the spell, exiles the land on top (returned to the bottom) and the
// nonland card with lesser mana value beneath it, and poses the free-cast
// election as a real optional decision; accepting casts that card without
// paying its mana cost, from exile, and it resolves before the Elf does.
func TestBloodbraidElfCascadeExilesUntilLesserAndOffersFreeCast(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9211, "Bloodbraid Elf", []string{"Forest", "Grizzly Bears"}, nil)
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, bearID := lib[0], lib[1]
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	addMana(t, e, 0, "GGRR")
	castFixture(t, e, elfID, -1)
	// The cast committed, its trigger was queued, the drain minted it above
	// the spell, and the trigger's resolution suspended on the election.
	idx := cascadeElection(t, e, "Grizzly Bears")
	if got := e.G.Obj(bearID).Zone; got != state.ZExile {
		t.Fatalf("found card in %s, want exile", got)
	}
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("unmatched land still in %s, want already returned to the library", got)
	}
	// The reveal Note names both exiled cards.
	if !hasNote(e, "cascades, exiling") {
		t.Fatal("the exile reveal Note is missing")
	}
	// Accept: the free cast begins from exile without mana.
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(bearID).Zone; got != state.ZBattlefield {
		t.Fatalf("free-cast Grizzly Bears in %s, want the battlefield", got)
	}
	if got := e.G.Obj(elfID).Zone; got != state.ZBattlefield {
		t.Fatalf("resolved Bloodbraid Elf in %s, want the battlefield", got)
	}
	if n := movedTo(t, e, bearID, state.ZLibrary, state.ZExile); n != 1 {
		t.Fatalf("found card exiled %d times", n)
	}
	if n := movedTo(t, e, bearID, state.ZExile, state.ZLibrary); n != 0 {
		t.Fatalf("the cast found card was returned to the library %d times", n)
	}
	replayCheck(t, e, cfg)
}

// TestCascadeDeclinedFoundCardGoesToBottom pins the decline arm: the
// election's empty answer (Min 0 makes it legal) leaves the found card
// UNGAST, and the cascade's chained tail puts it on the bottom of the
// library beneath the cards already returned.
func TestCascadeDeclinedFoundCardGoesToBottom(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9212, "Bloodbraid Elf", []string{"Forest", "Grizzly Bears"}, nil)
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, bearID := lib[0], lib[1]
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	addMana(t, e, 0, "GGRR")
	castFixture(t, e, elfID, -1)
	cascadeElection(t, e, "Grizzly Bears")
	submitChoices(t, e) // the decline: no options chosen
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(bearID).Zone; got != state.ZLibrary {
		t.Fatalf("declined found card in %s, want the library", got)
	}
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("unmatched land in %s, want the library", got)
	}
	lib = e.G.Zone(state.ZLibrary, 0)
	if lib[len(lib)-1] != bearID || lib[len(lib)-2] != forestID {
		t.Fatalf("bottom order wrong: the found card must sit beneath the rest")
	}
	if got := e.G.Obj(elfID).Zone; got != state.ZBattlefield {
		t.Fatalf("resolved Bloodbraid Elf in %s, want the battlefield", got)
	}
	replayCheck(t, e, cfg)
}

// TestCascadeNoCandidateExilesAndBottomsWithNoElection pins the run-out arm:
// a library whose nonland cards all cost at least the cascade spell's mana
// value is exiled whole and returned, with no election posed.
func TestCascadeNoCandidateExilesAndBottomsWithNoElection(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngineFiller(t, 9213, "Bloodbraid Elf", []string{"Forest", "Hill Giant"}, nil, "Mountain")
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, giantID := lib[0], lib[1]
	elfID := searchMoveByName(t, e, "Bloodbraid Elf", state.ZHand)
	addMana(t, e, 0, "GGRR")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == elfID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", elfID, d.Options)
	}
	submitChoices(t, e, idx)
	// One pass per seat: the cascade trigger resolves with nothing to ask —
	// no card qualifies — so priority comes straight back.
	passOnce(t, e)
	passOnce(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("want plain priority (no election over an unqualifying library), got %+v", d)
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(giantID).Zone; got != state.ZLibrary {
		t.Fatalf("too-expensive card in %s, want the library", got)
	}
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("land in %s, want the library", got)
	}
	lib = e.G.Zone(state.ZLibrary, 0)
	if lib[0] != forestID || lib[1] != giantID {
		t.Fatalf("bottom order wrong: the whole exile-and-return round trip must preserve the library order")
	}
	if n := movedTo(t, e, giantID, state.ZLibrary, state.ZExile); n != 1 {
		t.Fatalf("Hill Giant exiled %d times", n)
	}
	replayCheck(t, e, cfg)
}

// TestMaelstromWandererCascadesTwice is CR 702.85b's two-instance carrier on
// the REAL corpus card: two printed K:Cascade lines queue two triggers, the
// controller is asked their order, and each trigger runs its own
// exile-until + election in turn.
func TestMaelstromWandererCascadesTwice(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9214, "Maelstrom Wanderer", []string{"Forest", "Lightning Bolt", "Grizzly Bears"}, nil)
	wandererID := searchMoveByName(t, e, "Maelstrom Wanderer", state.ZHand)
	addMana(t, e, 0, "GGUURRRR")
	d := castFixture(t, e, wandererID, -1)
	// Two same-controller triggers: the order ask comes first.
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("want the trigger-order ask, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("order options: %+v", d.Options)
	}
	submitChoices(t, e, 0, 1)
	// The triggers sit on the stack above the Wanderer; both seats pass and
	// the one on top resolves first.
	d = passUntilNonPriority(t, e, 20)
	// First resolution: the Bolt election.
	idx := cascadeElection(t, e, "Lightning Bolt")
	boltID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Lightning Bolt" {
			boltID = id
		}
	}
	if boltID == 0 {
		t.Fatal("Lightning Bolt not in exile after the first cascade's scan")
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, d.Options[0].Index)
	}
	// Second cascade: the Bears election.
	d = passUntilNonPriority(t, e, 20)
	idx = cascadeElection(t, e, "Grizzly Bears")
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 60)
	bears := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			bears++
		}
	}
	if bears != 1 {
		t.Fatalf("%d Grizzly Bears on the battlefield, want the free-cast one", bears)
	}
	if got := e.G.Obj(wandererID).Zone; got != state.ZBattlefield {
		t.Fatalf("resolved Maelstrom Wanderer in %s, want the battlefield", got)
	}
	replayCheck(t, e, cfg)
}

// TestDarkApostleGrantedCascadeRegistersAndOffers is the GRANTED route's
// carrier, on the REAL corpus card whose grant is exactly the shape the brief
// names: AB$ Effect | StaticAbilities$ GrantCascade, with
// SVar:GrantCascade:Mode$ Continuous | Affected$ Card.nonCreature+YouCtrl |
// AffectedZone$ Stack | AddKeyword$ Cascade. Activating the ability
// registers the layer-6 grant; the next noncreature spell the controller
// casts then cascades. (TARDIS carries the identical DB$ Effect shape but is
// unreachable end to end — its K:Crew is an unsupported primitive, so the
// vehicle can never attack — see the report's Issues.)
func TestDarkApostleGrantedCascadeRegistersAndOffers(t *testing.T) {
	t.Parallel()
	e, cfg := cascadeTestEngine(t, 9215, "Night's Whisper", []string{"Forest", "Lightning Bolt"}, []string{"Dark Apostle"})
	apostleID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Dark Apostle" {
			apostleID = id
		}
	}
	if apostleID == 0 {
		t.Fatal("Dark Apostle not on the battlefield")
	}
	// Gift of Chaos carries a {T} cost and the Apostle is a creature: drive
	// past the summoning-sick turn (turn 2 is the opponent's), then
	// re-arrange the library window — turn 3's draw ate the old top.
	driveToStep(t, e, 3, 0, state.StepMain1)
	cascadeArrangeWindow(t, e, []string{"Forest", "Lightning Bolt"})
	lib := e.G.Zone(state.ZLibrary, 0)
	forestID, boltID := lib[0], lib[1]
	// Activate Gift of Chaos ({3}, {T}).
	abIdx := -1
	f := e.G.Obj(apostleID).Face()
	for i, sa := range f.Abilities {
		if sa.Kind == "AB" && sa.API == "Effect" {
			abIdx = i
		}
	}
	if abIdx < 0 {
		t.Fatal("Dark Apostle has no AB$ Effect ability")
	}
	addMana(t, e, 0, "RRR")
	opt := abilityOption(t, e, apostleID, abIdx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	// The activation resolves; its Note machinery is effEffect's own. Cast
	// the next noncreature spell: Night's Whisper (mana value 2).
	whisperID := searchMoveByName(t, e, "Night's Whisper", state.ZHand)
	addMana(t, e, 0, "BBR")
	d := castFixture(t, e, whisperID, -1)
	idx := cascadeElection(t, e, "Lightning Bolt")
	if got := e.G.Obj(boltID).Zone; got != state.ZExile {
		t.Fatalf("found card in %s, want exile", got)
	}
	if got := e.G.Obj(forestID).Zone; got != state.ZLibrary {
		t.Fatalf("unmatched land still in %s, want already returned to the library", got)
	}
	// Accept the free cast; Lightning Bolt asks its target, then resolves.
	submitChoices(t, e, idx)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the free-cast Bolt's target ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(boltID).Zone; got != state.ZGraveyard {
		t.Fatalf("free-cast Lightning Bolt in %s, want the graveyard", got)
	}
	if got := e.G.Obj(whisperID).Zone; got != state.ZGraveyard {
		t.Fatalf("resolved Night's Whisper in %s, want the graveyard", got)
	}
	replayCheck(t, e, cfg)
}
