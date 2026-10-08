package rules

import (
	"slices"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

var (
	librarySearchRegistryOnce sync.Once
	librarySearchRegistry     *cards.Registry
)

func searchTestRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	librarySearchRegistryOnce.Do(func() {
		librarySearchRegistry = testutil.CorpusRegistry(t)
	})
	if librarySearchRegistry == nil {
		t.Skip("library-search corpus unavailable")
	}
	return librarySearchRegistry
}

func searchCorpusCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("missing corpus card %q", name)
	}
	return c
}

// searchEngine uses only compiled corpus cards. The repeated basics and bears
// make the library's eligible set visible in the fixture without committing
// any Forge script text.
func searchEngine(t *testing.T, reg *cards.Registry, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
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
	cfg := Config{Seed: 9202, Names: []string{"searcher", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

func TestCorpusNameChoiceUsesFullUniverse(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Pithing Needle", "Phyrexian Revoker", "Cabal Therapy")
	if _, ok := reg.Lookup("Wasteland"); !ok {
		t.Fatal("corpus precondition: Wasteland missing")
	}
	all := e.etbOptions(0, e.G.Zone(state.ZLibrary, 0)[0], "name", "", "", "", "")
	land := false
	for _, o := range all {
		if o.Label == "Wasteland" {
			land = true
		}
	}
	if !land {
		t.Fatalf("unrestricted NameCard omitted unseen land; options=%d", len(all))
	}
	nonland := e.etbOptions(0, e.G.Zone(state.ZLibrary, 0)[0], "name", "Card.nonLand", "", "", "")
	for _, o := range nonland {
		if o.Label == "Wasteland" {
			t.Fatal("nonland NameCard offered Wasteland")
		}
	}
}

func searchMoveByName(t *testing.T, e *Engine, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from hand/library", name)
	return 0
}

func changeZoneAbilityIndex(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("source %d has no face", id)
	}
	for i, sa := range o.Face().Abilities {
		if sa.Kind == "AB" && sa.API == "ChangeZone" && sa.Params["Origin"] == "Library" {
			return i
		}
	}
	t.Fatalf("%s has no library ChangeZone activated ability", o.Face().Name)
	return -1
}

func activateSearch(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	idx := changeZoneAbilityIndex(t, e, id)
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	// Evolving Wilds' CARDNAME sacrifice is a real casting-flow KChoose.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind != "search" {
		if len(d.Options) != 1 || d.Options[0].Obj != id {
			t.Fatalf("unexpected activation cost choice: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	return passUntilNonPriority(t, e, 20)
}

func castSearchSpell(t *testing.T, e *Engine, name string) (state.ObjID, *decision.Decision) {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, "WUBRGCCCCCCCC")
	return id, castFixture(t, e, id, -1)
}

func basicLibraryIDs(e *Engine) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		o := e.G.Obj(id)
		if o != nil && effects.MatchesSpecFrom(e.G, "Land.Basic", id, 0, 0) {
			out = append(out, id)
		}
	}
	return out
}

func optionIDs(d *decision.Decision) []state.ObjID {
	out := make([]state.ObjID, 0, len(d.Options))
	for _, o := range d.Options {
		out = append(out, o.Obj)
	}
	return out
}

func searchEvents(log []events.Event, start int, player state.PlayerID) []events.Event {
	var out []events.Event
	for _, ev := range log[start:] {
		if ev.Player == player || ev.Obj != 0 {
			switch ev.Kind {
			case events.MoveZone, events.Tap, events.Shuffle, events.LibraryOrder:
				out = append(out, ev)
			}
		}
	}
	return out
}

func TestEvolvingWildsAnswerMovesThenTapsThenShuffles(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Evolving Wilds")
	wilds := searchMoveByName(t, e, "Evolving Wilds", state.ZBattlefield)
	d := activateSearch(t, e, wilds)
	picked := d.Options[0].Obj
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(picked).Zone != state.ZBattlefield || !e.G.Obj(picked).Tapped {
		t.Fatalf("picked basic = %+v, want tapped on battlefield", e.G.Obj(picked))
	}
	if slices.Contains(e.G.Zone(state.ZLibrary, 0), picked) {
		t.Fatal("picked basic remains in library")
	}
	es := searchEvents(e.L.Events, start, 0)
	if len(es) < 3 || es[0].Kind != events.MoveZone || es[0].Obj != picked ||
		es[1].Kind != events.Tap || es[1].Obj != picked || es[2].Kind != events.Shuffle {
		t.Fatalf("search event prefix = %+v, want MoveZone(%d), Tap(%d), Shuffle", es, picked, picked)
	}
	shuffles := 0
	for _, ev := range es {
		if ev.Kind == events.Shuffle {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("search shuffles = %d, want exactly one", shuffles)
	}
	replayCheck(t, e, cfg)
}

func TestEvolvingWildsFailToFindStillShuffles(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Evolving Wilds")
	wilds := searchMoveByName(t, e, "Evolving Wilds", state.ZBattlefield)
	activateSearch(t, e, wilds)
	start := len(e.L.Events)
	submitChoices(t, e)
	moves, shuffles := 0, 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary {
			moves++
		}
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if moves != 0 || shuffles != 1 {
		t.Fatalf("fail-to-find emitted %d library moves and %d shuffles, want 0/1", moves, shuffles)
	}
	replayCheck(t, e, cfg)
}

func TestExplosiveVegetationAllowsTwoPicksInAnswerOrder(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Explosive Vegetation")
	_, d := castSearchSpell(t, e, "Explosive Vegetation")
	if d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 || len(d.Options) < 2 {
		t.Fatalf("Explosive Vegetation decision = %+v", d)
	}
	first, second := d.Options[1].Obj, d.Options[0].Obj
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)
	var moved []state.ObjID
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZBattlefield {
			moved = append(moved, ev.Obj)
		}
	}
	if !slices.Equal(moved, []state.ObjID{first, second}) {
		t.Fatalf("move order = %v, want answer order [%d %d]", moved, first, second)
	}
}

func TestTimeOfNeedMovesChosenLegendToHandAndShuffles(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Time of Need", "Isamaru, Hound of Konda")
	_, d := castSearchSpell(t, e, "Time of Need")
	if len(d.Options) != 1 {
		t.Fatalf("Time of Need options = %+v, want Isamaru only", d.Options)
	}
	picked := d.Options[0].Obj
	start := len(e.L.Events)
	submitChoices(t, e, 0)
	if e.G.Obj(picked).Zone != state.ZHand {
		t.Fatalf("picked legend zone = %s, want hand", e.G.Obj(picked).Zone)
	}
	shuffles := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("shuffles = %d, want 1", shuffles)
	}
}

func TestVampiricTutorPutsChosenCardOnTopWithLibraryOrder(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Vampiric Tutor")
	_, d := castSearchSpell(t, e, "Vampiric Tutor")
	picked := d.Options[len(d.Options)-1].Obj
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 || lib[0] != picked {
		t.Fatalf("library top = %v, want picked card %d", lib, picked)
	}
	orders := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.LibraryOrder && ev.Player == 0 {
			orders++
		}
	}
	if orders != 1 {
		t.Fatalf("LibraryOrder events = %d, want 1", orders)
	}
}

func TestLibrarySearchOptionsVisibleOnlyToChooser(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Evolving Wilds")
	wilds := searchMoveByName(t, e, "Evolving Wilds", state.ZBattlefield)
	d := activateSearch(t, e, wilds)
	chooser := view.Project(e.G, e, 0, d)
	if chooser.Decision == nil || len(chooser.Decision.Options) == 0 || chooser.Decision.Options[0].Label == "" {
		t.Fatalf("chooser view does not name search options: %+v", chooser.Decision)
	}
	opponent := view.Project(e.G, e, 1, d)
	if opponent.Decision != nil {
		t.Fatalf("opponent received hidden search decision: %+v", opponent.Decision)
	}
	omniscient := view.ProjectFor(e.G, e, view.NoSeat, view.Omniscient, d)
	if omniscient.Decision == nil || len(omniscient.Decision.Options) != len(d.Options) {
		t.Fatalf("omniscient spectator did not receive search options: %+v", omniscient.Decision)
	}
}

func TestLibrarySearchChoicesReplayByteIdentically(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Evolving Wilds", "Evolving Wilds")
	for attempt := 0; attempt < 2; attempt++ {
		wilds := searchMoveByName(t, e, "Evolving Wilds", state.ZBattlefield)
		d := activateSearch(t, e, wilds)
		if attempt == 0 {
			submitChoices(t, e, d.Options[0].Index)
		} else {
			submitChoices(t, e)
		}
		if attempt == 0 {
			passUntilStackEmpty(t, e, 20)
		}
	}
	replayCheck(t, e, cfg)
}

func TestVampiricTutorSearchResumeRunsSubAbilityOnceWithoutReask(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Vampiric Tutor")
	_, d := castSearchSpell(t, e, "Vampiric Tutor")
	life := e.G.Players[0].Life
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Players[0].Life != life-2 {
		t.Fatalf("Vampiric Tutor sub-ability changed life %d -> %d, want exactly -2", life, e.G.Players[0].Life)
	}
	if pd := e.Pending(); pd != nil && pd.Kind == decision.KChoose && pd.ResumeKind == "search" {
		t.Fatalf("search re-asked after answer: %+v", pd)
	}
	shuffles := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	// One genesis shuffle plus exactly one tutor shuffle.
	if shuffles != 2 {
		t.Fatalf("seat 0 shuffles = %d, want genesis + one search", shuffles)
	}
	replayCheck(t, e, cfg)
}
