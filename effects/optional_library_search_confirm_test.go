package effects

// The explicit Optional$ confirm-before-search gate for the hidden-library
// ChangeZone search (effSearchLibrary): Forge's ChangeZoneEffect
// changeHiddenOriginResolve poses its confirmAction gate BEFORE the fetch
// list is consulted, so an Optional$ True/You library search must ask the
// search player whether to proceed before it offers (or fails to find) a
// card. The marker is read through the ONE shared optionalConfirmMarker the
// hidden-hand walk and the Hidden$ True pick already use. A decline skips
// that player's search, card pick and search-specific shuffle/tail; an
// accepted confirmation enters the ordinary Min-0/mandatory search and its
// shuffle path unchanged, even when the pool is empty. ChoiceOptional$ and a
// markerless text-may search stay confirmation-free, and the object-valued
// Defined$ fetch list keeps its own election in moveDefinedLibraryObjects
// with no second confirm.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const olscOptional = "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | Optional$ True"

// olscFixtureNoHost builds a 2-seat board whose source searches seat 0's own
// library (no DefinedPlayer$), holding two creatures. It returns a fresh
// askHost, the source id and the two creature ids in library order.
func olscFixture(t *testing.T) (*askHost, state.ObjID, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Searcher\nTypes:Sorcery\nOracle:x\n"), 0)
	bear := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	bird := h.g.AddObject(mkCard(t, "Name:Birds of Paradise\nTypes:Creature\nPT:0/1\nOracle:x\n"), 0)
	ids := []state.ObjID{bear.ID, bird.ID}
	h.g.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
	h.g.SetZone(state.ZLibrary, 0, ids)
	h.g.Obj(src.ID).Zone = state.ZHand
	for _, id := range ids {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	return h, src.ID, ids
}

// TestOptionalLibrarySearchConfirm pins the two-ask contract of an explicit
// Optional$ library search: the confirmation gate first, then (on accept) the
// existing Min-0 search pick.
func TestOptionalLibrarySearchConfirm(t *testing.T) {
	t.Run("confirmation gate runs before the pick", func(t *testing.T) {
		h, src, ids := olscFixture(t)
		// Precondition: both creatures really sit in the library the search
		// reads, and are distinct cards.
		if ids[0] == ids[1] || h.g.Obj(ids[0]).Zone != state.ZLibrary || h.g.Obj(ids[1]).Zone != state.ZLibrary {
			t.Fatal("precondition: two distinct creatures must sit in the library")
		}
		c := &Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Obj: ids[0]}}}
		Resolve(h, c, sa(t, olscOptional))
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "search_confirm" || confirm.Player != 0 ||
			confirm.Kind != decision.KChoose ||
			confirm.Min != 1 || confirm.Max != 1 || confirm.Source != src ||
			len(confirm.Options) != 2 || confirm.Options[0].Kind != "yes" || confirm.Options[1].Kind != "no" {
			t.Fatalf("first ask = %+v, want a search_confirm Min==Max==1 yes/no gate", confirm)
		}
		// The remembered snapshot rides the confirmation so the resumed pick
		// re-checks against it.
		if len(confirm.ResumeRemembered) != 1 || confirm.ResumeRemembered[0].Obj != ids[0] {
			t.Fatalf("confirmation ResumeRemembered = %+v, want the walk's remembered snapshot", confirm.ResumeRemembered)
		}
		for _, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZLibrary {
				t.Fatalf("the confirmation itself moved ids[%d] to %s", id, o.Zone)
			}
		}
	})

	t.Run("decline poses no pick and shuffles nothing", func(t *testing.T) {
		h, src, ids := olscFixture(t)
		Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, olscOptional))
		if h.asked == nil || h.asked.ResumeKind != "search_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		h.asked = nil
		Resolve(h, &Ctx{Source: src, Controller: 0, SearchConfirmDone: true, SearchConfirm: "no",
			SearchConfirmTarget: 0}, sa(t, olscOptional))
		if h.asked != nil {
			t.Fatalf("a declined confirmation posed a card pick: %+v", h.asked)
		}
		for i, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZLibrary {
				t.Fatalf("decline moved ids[%d] to %s", i, o.Zone)
			}
		}
	})

	t.Run("accepted confirmation picks one", func(t *testing.T) {
		h, src, ids := olscFixture(t)
		Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, olscOptional))
		if h.asked == nil || h.asked.ResumeKind != "search_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		Resolve(h, &Ctx{Source: src, Controller: 0, SearchConfirmDone: true, SearchConfirm: "yes",
			SearchConfirmTarget: 0}, sa(t, olscOptional))
		pick := h.asked
		if pick == nil || pick.ResumeKind != "search" || pick.Min != 0 || pick.Max != 1 ||
			len(pick.Options) != 2 || pick.Options[0].Obj != ids[0] || pick.Options[1].Obj != ids[1] {
			t.Fatalf("accepted confirmation pick = %+v, want a Min 0 search over both creatures", pick)
		}
		// The answer moves exactly the picked creature.
		Resolve(h, &Ctx{Source: src, Controller: 0, Search: []state.ObjID{ids[1]}, SearchDone: true,
			LibraryTarget: 0}, sa(t, olscOptional))
		if o := h.g.Obj(ids[1]); o.Zone != state.ZHand {
			t.Fatalf("answered creature on %s, want hand", o.Zone)
		}
		if o := h.g.Obj(ids[0]); o.Zone != state.ZLibrary {
			t.Fatalf("unchosen creature moved: on %s", o.Zone)
		}
	})

	t.Run("accepted confirmation may find nothing", func(t *testing.T) {
		h, src, ids := olscFixture(t)
		Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, olscOptional))
		if h.asked == nil || h.asked.ResumeKind != "search_confirm" {
			t.Fatalf("precondition: no confirmation gate was posed: %+v", h.asked)
		}
		Resolve(h, &Ctx{Source: src, Controller: 0, SearchConfirmDone: true, SearchConfirm: "yes",
			SearchConfirmTarget: 0}, sa(t, olscOptional))
		if h.asked == nil || h.asked.ResumeKind != "search" {
			t.Fatalf("precondition: the accepted confirmation posed no pick: %+v", h.asked)
		}
		Resolve(h, &Ctx{Source: src, Controller: 0, Search: nil, SearchDone: true,
			LibraryTarget: 0}, sa(t, olscOptional))
		for i, id := range ids {
			if o := h.g.Obj(id); o.Zone != state.ZLibrary {
				t.Fatalf("accept-then-find-nothing moved ids[%d] to %s", i, o.Zone)
			}
		}
	})

	t.Run("empty eligible pool still confirms then completes the tail", func(t *testing.T) {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		src := h.g.AddObject(mkCard(t, "Name:Searcher\nTypes:Sorcery\nOracle:x\n"), 0)
		// A land is the only library card, but the filter wants a Creature:
		// the eligible pool is empty, yet Forge asks before consulting it.
		isle := h.g.AddObject(mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"), 0)
		h.g.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
		h.g.SetZone(state.ZLibrary, 0, []state.ObjID{isle.ID})
		h.g.Obj(src.ID).Zone = state.ZHand
		h.g.Obj(isle.ID).Zone = state.ZLibrary
		body := sa(t, olscOptional+" | ShuffleNonMandatory$ True")
		Resolve(h, &Ctx{Source: src.ID, Controller: 0}, body)
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "search_confirm" {
			t.Fatalf("empty-pool first ask = %+v, want the confirmation gate", confirm)
		}
		// Decline: nothing moves and no shuffle/tail ask follows.
		h.asked = nil
		Resolve(h, &Ctx{Source: src.ID, Controller: 0, SearchConfirmDone: true, SearchConfirm: "no",
			SearchConfirmTarget: 0}, body)
		if h.asked != nil {
			t.Fatalf("a declined empty-pool fetch posed %+v, want nothing", h.asked)
		}
		if o := h.g.Obj(isle.ID); o.Zone != state.ZLibrary {
			t.Fatalf("decline moved the land to %s", o.Zone)
		}
		// Accept: the fail-to-find still owes its may-shuffle confirm tail.
		h.asked = nil
		Resolve(h, &Ctx{Source: src.ID, Controller: 0, SearchConfirmDone: true, SearchConfirm: "yes",
			SearchConfirmTarget: 0}, body)
		tail := h.asked
		if tail == nil || tail.ResumeKind != "search_mayshuffle" {
			t.Fatalf("accepted empty-pool fetch next ask = %+v, want the may-shuffle tail", tail)
		}
		shuffles := 0
		for _, ev := range h.log {
			if ev.Kind == events.Shuffle && ev.Player == 0 {
				shuffles++
			}
		}
		if o := h.g.Obj(isle.ID); o.Zone != state.ZLibrary {
			t.Fatalf("accepted empty fetch moved the land to %s", o.Zone)
		}
		if shuffles != 0 {
			t.Fatalf("the tail shuffled before its answer (shuffles=%d)", shuffles)
		}
	})

	t.Run("markerless text-may search stays confirmation-free", func(t *testing.T) {
		h, src, _ := olscFixture(t)
		Resolve(h, &Ctx{Source: src, Controller: 0},
			sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | SpellDescription$ Search your library for a creature card, then shuffle."))
		d := h.asked
		if d == nil || d.ResumeKind != "search" || d.Min != 0 || d.Max != 1 {
			t.Fatalf("markerless first ask = %+v, want the Min 0 search with no confirmation gate", d)
		}
	})

	t.Run("ChoiceOptional$ cardinality marker does not confirm", func(t *testing.T) {
		h, src, _ := olscFixture(t)
		Resolve(h, &Ctx{Source: src, Controller: 0},
			sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | ChoiceOptional$ True"))
		d := h.asked
		if d == nil || d.ResumeKind != "search" {
			t.Fatalf("ChoiceOptional$ first ask = %+v, want the Min 0 search with no confirmation gate", d)
		}
	})

	t.Run("object-valued Defined$ fetch keeps its own election, no search confirm", func(t *testing.T) {
		h := &fx42AskHost{}
		h.g = state.NewGame(names(2))
		src := h.g.AddObject(mkCard(t, "Name:Searcher\nTypes:Sorcery\nOracle:x\n"), 0)
		monster := h.g.AddObject(mkCard(t, "Name:Sea Monster\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
		h.g.SetZone(state.ZLibrary, 0, []state.ObjID{monster.ID})
		h.g.Obj(monster.ID).Zone = state.ZLibrary
		body := sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | Defined$ Remembered | Optional$ True")
		// Precondition: the rememberer names a real library object.
		if h.g.Obj(monster.ID).Zone != state.ZLibrary {
			t.Fatal("precondition: the remembered card is not in the library")
		}
		Resolve(h, &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: monster.ID}}}, body)
		if len(h.asks) != 1 {
			t.Fatalf("object-valued Defined$ fetch posed %d asks = %+v, want exactly its one defined_library_optional election", len(h.asks), h.asks)
		}
		d := h.asks[0]
		if d.ResumeKind != "defined_library_optional" {
			t.Fatalf("object-valued Defined$ first ask = %+v, want its own defined_library_optional election (never a search_confirm)", d)
		}
	})

	t.Run("real corpus carrier: Path to Exile's basic-land search", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		path, ok := reg.Lookup("Path to Exile")
		if !ok || len(path.Faces) == 0 {
			t.Fatal("Path to Exile is absent from the corpus")
		}
		fetch := cards.ResolveSVar(path.Faces[0].SVars, "DBChange")
		if fetch == nil {
			t.Fatal("Path to Exile has no compiled DBChange continuation")
		}
		if fetch.Params["Optional"] != "True" || fetch.Params["Origin"] != "Library" {
			t.Fatalf("precondition: the corpus ability lost its Optional$ Library shape: %+v", fetch.Params)
		}
		h := &askHost{}
		h.g = state.NewGame(names(2))
		src := h.g.AddObject(path, 0)
		// Seat 1 controls the exiled creature and searches its own library.
		creature := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
		forest := h.g.AddObject(mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 1)
		h.g.SetZone(state.ZBattlefield, 1, []state.ObjID{creature.ID})
		h.g.SetZone(state.ZLibrary, 1, []state.ObjID{forest.ID})
		h.g.Obj(creature.ID).Zone = state.ZBattlefield
		h.g.Obj(forest.ID).Zone = state.ZLibrary
		if h.g.Obj(forest.ID).Zone != state.ZLibrary {
			t.Fatal("precondition: the basic land is not in seat 1's library")
		}
		c := &Ctx{Source: src.ID, Controller: 0, Targets: []state.Target{{Obj: creature.ID}}}
		Resolve(h, c, fetch)
		confirm := h.asked
		if confirm == nil || confirm.ResumeKind != "search_confirm" {
			t.Fatalf("Path to Exile DBChange first ask = %+v, want its search_confirm gate", confirm)
		}
	})
}

// TestOptionalLibrarySearchMultiPlayer pins that the confirmation is posed
// per search player, that its cursor keeps the answered player attached, and
// that a decline skips only that player.
func TestOptionalLibrarySearchMultiPlayer(t *testing.T) {
	newBoard := func(t *testing.T) (*fx42AskHost, state.ObjID) {
		t.Helper()
		h := &fx42AskHost{}
		h.g = state.NewGame(names(3))
		src := h.g.AddObject(mkCard(t, "Name:Searcher\nTypes:Sorcery\nOracle:x\n"), 0)
		for p := state.PlayerID(1); p <= 2; p++ {
			bear := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), p)
			h.g.SetZone(state.ZLibrary, p, []state.ObjID{bear.ID})
			h.g.Obj(bear.ID).Zone = state.ZLibrary
		}
		h.g.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
		h.g.Obj(src.ID).Zone = state.ZHand
		return h, src.ID
	}
	body := func(t *testing.T) *cards.SA {
		return sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | DefinedPlayer$ Opponent | Optional$ True")
	}

	t.Run("decline skips only the answering player", func(t *testing.T) {
		h, src := newBoard(t)
		Resolve(h, &Ctx{Source: src, Controller: 0}, body(t))
		if len(h.asks) != 1 {
			t.Fatalf("first pass posed %d asks, want the first opponent's confirmation", len(h.asks))
		}
		first := h.asks[0]
		if first.ResumeKind != "search_confirm" || first.ResumeTarget != 0 {
			t.Fatalf("first confirmation = %+v, want search_confirm cursor 0", first)
		}
		// Decline opponent 0's confirmation: opponent 1 must still confirm.
		h.suspended = false
		Resolve(h, &Ctx{Source: src, Controller: 0, SearchConfirmDone: true, SearchConfirm: "no",
			SearchConfirmTarget: 0}, body(t))
		if len(h.asks) != 2 {
			t.Fatalf("after decline, %d asks total, want opponent 1's confirmation", len(h.asks))
		}
		second := h.asks[1]
		if second.ResumeKind != "search_confirm" || second.ResumeTarget != 1 {
			t.Fatalf("second confirmation = %+v, want search_confirm cursor 1", second)
		}
	})

	t.Run("accepted first player is not re-asked after its pick", func(t *testing.T) {
		h, src := newBoard(t)
		Resolve(h, &Ctx{Source: src, Controller: 0}, body(t))
		if len(h.asks) != 1 || h.asks[0].ResumeKind != "search_confirm" || h.asks[0].ResumeTarget != 0 {
			t.Fatalf("precondition: wants opponent 0's confirmation, got %+v", h.asks)
		}
		// Accept opponent 0: its pick is posed next.
		h.suspended = false
		Resolve(h, &Ctx{Source: src, Controller: 0, SearchConfirmDone: true, SearchConfirm: "yes",
			SearchConfirmTarget: 0}, body(t))
		if len(h.asks) != 2 || h.asks[1].ResumeKind != "search" || h.asks[1].ResumeTarget != 0 {
			t.Fatalf("after accept, asks = %+v, want opponent 0's search pick", h.asks)
		}
		pick := h.asks[1]
		if len(pick.Options) != 1 {
			t.Fatalf("opponent 0's pick options = %+v, want its own library card", pick.Options)
		}
		taken := pick.Options[0].Obj
		// Answer the pick: no confirmation may be re-posed for opponent 0,
		// and opponent 1's confirmation is the next ask.
		h.suspended = false
		Resolve(h, &Ctx{Source: src, Controller: 0, Search: []state.ObjID{taken}, SearchDone: true,
			LibraryTarget: 0}, body(t))
		if len(h.asks) != 3 {
			t.Fatalf("after the pick answer, %d asks total = %+v, want opponent 1's confirmation third", len(h.asks), h.asks)
		}
		if h.asks[2].ResumeKind != "search_confirm" || h.asks[2].ResumeTarget != 1 {
			t.Fatalf("third ask = %+v, want opponent 1's search_confirm (opponent 0 must not be re-asked)", h.asks[2])
		}
		if o := h.g.Obj(taken); o.Zone != state.ZHand {
			t.Fatalf("the answered card moved to %s, want hand", o.Zone)
		}
	})
}

// TestOptionalLibrarySearchNoHost pins R-9: a host with no decision channel
// plays the may as do (accept), then the search path's existing no-host pick
// policy runs.
func TestOptionalLibrarySearchNoHost(t *testing.T) {
	h, src, ids := olscFixture(t)
	fh := &fakeHost{g: h.g}
	Resolve(fh, &Ctx{Source: src, Controller: 0}, sa(t, olscOptional))
	// Both asks were built and answered deterministically in the player's
	// place: the confirmation gate and the Mandatory-ish Min pick.
	if fh.askCount != 2 {
		t.Fatalf("no-host ask count = %d, want 2 (the confirmation gate and the pick)", fh.askCount)
	}
	// A stated-quality search's no-host stand-in legitimately finds nothing,
	// so accept-then-empty leaves both creatures in the library. The point is
	// that the confirmation did not abort the fetch: the pick ask was posed.
	if fh.lastAsk == nil || fh.lastAsk.ResumeKind != "search" {
		t.Fatalf("no-host last ask = %+v, want the search pick after the accepted confirmation", fh.lastAsk)
	}
	for i, id := range ids {
		if o := fh.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("no-host stated-quality search moved ids[%d] to %s", i, o.Zone)
		}
	}
	// A quantity-only Optional$ search has a real no-host take: accept then
	// find the deterministic first card.
	qh, qsrc, qids := olscFixture(t)
	qfh := &fakeHost{g: qh.g}
	Resolve(qfh, &Ctx{Source: qsrc, Controller: 0},
		sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card | ChangeNum$ 1 | Optional$ True"))
	if qfh.askCount != 2 {
		t.Fatalf("quantity no-host ask count = %d, want 2 (confirm + pick)", qfh.askCount)
	}
	if o := qfh.g.Obj(qids[0]); o.Zone != state.ZHand {
		t.Fatalf("quantity no-host search left the first card on %s, want hand", o.Zone)
	}
	if o := qfh.g.Obj(qids[1]); o.Zone != state.ZLibrary {
		t.Fatalf("quantity no-host search moved the second card to %s", o.Zone)
	}
}
