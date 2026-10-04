package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Explore family end to end (task explore1): api:Explore (CR 701.35a —
// reveal the top card; land to hand; otherwise a +1/+1 counter and the LCI
// "put the card back or put it into your graveyard" election), the
// "Whenever a creature you control explores" trigger mode (trig:Explores)
// and the R:Event$ Explore replacement (repl:Explore). Every test drives real
// corpus cards: the explore source is Enter the Unknown's SP$ Explore cast or
// the Map token's {1},{T},sacrifice activation, and the trigger/replacement
// carriers are the corpus cards the brief names.

// counterOn reports the number of kind counters on the object (0 when the
// object or the counter kind is absent).
func counterOn(t *testing.T, e *Engine, id state.ObjID, kind string) int32 {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("object %d missing", id)
	}
	for i := range o.Counters {
		if o.Counters[i].Kind == kind {
			return o.Counters[i].N
		}
	}
	return 0
}

// exploreRecords returns every events.Explore record in the log.
func exploreRecords(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Explore {
			out = append(out, ev)
		}
	}
	return out
}

// castCardByName drives one cast option for a named card in seat 0's hand and
// returns after the cast option is submitted (a following target ask, if the
// spell carries one, is the caller's).
func castCardByName(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending for the cast")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
}

// exploreTarget asks an Enter the Unknown-shaped KTarget and submits the named
// explorer.
func exploreTarget(t *testing.T, e *Engine, explorer state.ObjID) {
	t.Helper()
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask = %+v, want KTarget", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == explorer {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("explorer %d not offered: %+v", explorer, d.Options)
	}
	submitChoices(t, e, idx)
}

// passUntil drives pass submissions (and Advances when nothing is pending)
// until cond() holds or the budget runs out; a non-priority ask is unexpected
// in these tests' flows and fails loudly rather than being answered blind.
func passUntil(t *testing.T, e *Engine, cond func() bool) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if cond() {
			return
		}
		d := e.Pending()
		switch {
		case d == nil:
			e.Advance()
		case d.Kind == decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		default:
			t.Fatalf("unexpected ask %+v (kind %v, resume %q)", d, d.Kind, d.ResumeKind)
		}
	}
	t.Fatal("condition never became true")
}

// arrangeLibraryTop moves the first library copy of each named card to the
// top of seat 0's library, in the given order (the arrange machinery's own
// events.LibraryOrder event; the payload is the COMPLETE new order), and
// returns the ids now on top.
func arrangeLibraryTop(t *testing.T, e *Engine, names ...string) []state.ObjID {
	t.Helper()
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	head := make([]state.ObjID, 0, len(names))
	for _, name := range names {
		found := false
		for i, id := range lib {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				head = append(head, id)
				lib = append(lib[:i:i], lib[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("arrangeLibraryTop: no %s in the library", name)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: append(head, lib...)})
	return head
}

// TestExploreSupported registers the three primitives the brief names — the
// coverage census (make report) reads effects.Supported(), so a missing entry
// would silently keep every carrier unplayable.
func TestExploreSupported(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	for _, want := range []string{"api:Explore", "trig:Explores", "repl:Explore"} {
		if !supported[want] {
			t.Fatalf("effects.Supported() is missing %s", want)
		}
	}
}

// TestMerfolkCaveDiverPumpsOnAnExplore is the trigger half (trig:Explores):
// Merfolk Cave-Diver on the battlefield, Enter the Unknown's SP$ Explore cast
// targeting it. The explore reveals the library's top card (a Forest — the
// land shape: it goes to the hand, no counter) and records the events.Explore
// marker; Cave-Diver's "Whenever a creature you control explores, CARDNAME
// gets +1/+0 until end of turn and can't be blocked this turn" trigger fires
// off that record and pumps it.
func TestMerfolkCaveDiverPumpsOnAnExplore(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Merfolk Cave-Diver", "Enter the Unknown")
	cd := searchMoveByName(t, e, "Merfolk Cave-Diver", state.ZBattlefield)
	etu := searchMoveByName(t, e, "Enter the Unknown", state.ZHand)
	// The land shape needs a land on top: arrange the library's first Forest.
	top := arrangeLibraryTop(t, e, "Forest")
	forest := top[0]
	addMana(t, e, 0, "G")

	castCardByName(t, e, etu)
	exploreTarget(t, e, cd)

	// The cast resolves over the following priority passes; the explore then
	// records, and the Cave-Diver trigger fires off the record and resolves
	// the same way (+1/+0 until end of turn, base 2 -> 3).
	passUntil(t, e, func() bool { return len(exploreRecords(e)) == 1 })

	// The explore itself: the top card was a Forest (chainAskDeck's filler
	// alternation), so it moved to the hand and the record is the land shape.
	recs := exploreRecords(e)
	if len(recs) != 1 {
		t.Fatalf("explore records = %d, want 1", len(recs))
	}
	if recs[0].Obj != cd || recs[0].Amount != 1 || recs[0].Player != 0 {
		t.Fatalf("record = %+v, want Obj the Cave-Diver, Amount 1, player 0", recs[0])
	}
	if len(recs[0].IDs) != 1 || recs[0].IDs[0] != forest {
		t.Fatalf("record IDs = %+v, want the arranged Forest %d", recs[0].IDs, forest)
	}
	if got := e.G.Obj(recs[0].IDs[0]); got == nil || got.Zone != state.ZHand {
		t.Fatalf("revealed card zone = %+v, want the land in hand", got)
	}
	if got := counterOn(t, e, cd, "P1P1"); got != 0 {
		t.Fatalf("Cave-Diver P1P1 counters = %d, want 0 (the revealed card was a land)", got)
	}

	// The trigger fires off the record and resolves over the following
	// priority rounds: +1/+0 until end of turn (base 2 -> 3) and the
	// can't-be-blocked Effect static. The pump is asserted before any pass
	// could carry turn 1 through cleanup (where until-EOT effects expire),
	// so no drivePastResolution here.
	passUntil(t, e, func() bool { return e.Power(cd) == 3 })
	if e.G.Turn != 1 {
		t.Fatalf("turn = %d, want the pump still live on turn 1", e.G.Turn)
	}
}

// TestExploreNonlandAsksPutBackOrGraveyard is the nonland shape and the
// mid-resolution election: the library's top card is arranged to be a Grizzly
// Bears (the LCI wording: "put a +1/+1 counter on this creature, then put the
// card back or put it into your graveyard"), so the explore reveals it, poses
// the real KChoose (the "explore" ask), and the answered "top" applies the
// counter and keeps the card where it is. Wildgrowth Walker is both the
// explorer and the trigger carrier, so its own "Whenever a creature you
// control explores" trigger also fires: a second +1/+1 counter and 3 life.
func TestExploreNonlandAsksPutBackOrGraveyard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Wildgrowth Walker", "Enter the Unknown")

	// Arrange a nonland on top of the library: the explore must pose the LCI
	// destination election, which only a nonland reveals.
	top := arrangeLibraryTop(t, e, "Grizzly Bears")
	bear := top[0]

	walker := searchMoveByName(t, e, "Wildgrowth Walker", state.ZHand)
	etu := searchMoveByName(t, e, "Enter the Unknown", state.ZHand)
	addMana(t, e, 0, "GG")
	castCardByName(t, e, walker)
	passUntil(t, e, func() bool {
		o := e.G.Obj(walker)
		return o != nil && o.Zone == state.ZBattlefield
	})
	addMana(t, e, 0, "G")
	castCardByName(t, e, etu)

	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask = %+v, want KTarget", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == walker {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the walker was not offered as the explore target: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// The nonland explore's destination election.
	de := passUntilAsk(t, e)
	if de == nil || de.Kind != decision.KChoose || de.ResumeKind != "explore" {
		t.Fatalf("election = %+v, want KChoose with ResumeKind explore", de)
	}
	if de.Min != 1 || de.Max != 1 || len(de.Options) != 2 {
		t.Fatalf("election bounds/options = %d..%d over %+v, want 1..1 over two options", de.Min, de.Max, de.Options)
	}
	if de.Options[0].Kind != "graveyard" || de.Options[1].Kind != "top" {
		t.Fatalf("option order = %+v, want the state-changing graveyard on 0 and top on 1", de.Options)
	}
	for _, o := range de.Options {
		if o.Obj != bear {
			t.Fatalf("option %+v does not name the revealed bear", o)
		}
	}
	submitChoices(t, e, de.Options[1].Index) // "top"

	// The answered election: the +1/+1 counter went on, the bear stayed on
	// top, and the record is the nonland shape.
	if got := counterOn(t, e, walker, "P1P1"); got != 1 {
		t.Fatalf("walker P1P1 counters = %d, want 1 from the explore", got)
	}
	if got := e.G.Zone(state.ZLibrary, 0); len(got) == 0 || got[0] != bear {
		t.Fatalf("library top = %+v, want the bear put back on top", got)
	}
	recs := exploreRecords(e)
	if len(recs) != 1 || recs[0].Amount != 0 || len(recs[0].IDs) != 1 || recs[0].IDs[0] != bear {
		t.Fatalf("explore records = %+v, want one Amount-0 record naming the bear", recs)
	}

	// The walker's own trigger fires off the record: a second counter and 3 life.
	life0 := int32(20) // seatZeroStart's starting life
	passUntil(t, e, func() bool { return counterOn(t, e, walker, "P1P1") == 2 })
	if got := e.G.Players[0].Life; got != life0+3 {
		t.Fatalf("life = %d, want %d (the walker's explore trigger gained 3)", got, life0+3)
	}
}

// TestExploreGraveyardAnswerMovesTheCard is the other arm of the election:
// the same nonland shape answered with the state-changing "graveyard" option
// (option 0, what the no-host stand-in and botpolicy's clamp take) moves the
// revealed card from the library to the graveyard after the counter.
func TestExploreGraveyardAnswerMovesTheCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Wildgrowth Walker", "Enter the Unknown")
	// Arrange a nonland on top of the library: the explore must pose the LCI
	// destination election, which only a nonland reveals.
	top := arrangeLibraryTop(t, e, "Grizzly Bears")
	bear := top[0]

	walker := searchMoveByName(t, e, "Wildgrowth Walker", state.ZHand)
	etu := searchMoveByName(t, e, "Enter the Unknown", state.ZHand)
	addMana(t, e, 0, "GG")
	castCardByName(t, e, walker)
	passUntil(t, e, func() bool {
		o := e.G.Obj(walker)
		return o != nil && o.Zone == state.ZBattlefield
	})
	addMana(t, e, 0, "G")
	castCardByName(t, e, etu)
	exploreTarget(t, e, walker)

	de := passUntilAsk(t, e)
	if de == nil || de.ResumeKind != "explore" {
		t.Fatalf("election = %+v, want the explore resume ask", de)
	}
	submitChoices(t, e, de.Options[0].Index) // "graveyard", the state-changing option 0

	if got := counterOn(t, e, walker, "P1P1"); got != 1 {
		t.Fatalf("walker P1P1 counters = %d, want 1 from the explore", got)
	}
	if got := e.G.Obj(bear); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("bear zone = %+v, want the graveyard", got)
	}
	recs := exploreRecords(e)
	if len(recs) != 1 || recs[0].Amount != 0 {
		t.Fatalf("explore records = %+v, want one Amount-0 record", recs)
	}
}

// TestTopographyTrackerExploresTwice is the replacement half (repl:Explore):
// Topography Tracker's "If a creature you control would explore, instead it
// explores, then it explores again" (R:Event$ Explore | ValidExplorer$
// Creature.YouCtrl | ReplaceWith$ Explore1, body DB$ Explore | Defined$
// ReplacedCard | Num$ 2). The explore is activated through the Map token its
// own ETB created, targeting the Tracker itself: the replaced process
// performs NOTHING (no reveal of its own), and the replacement body's two
// fresh explores each reveal a land to the hand — two records, no counter.
func TestTopographyTrackerExploresTwice(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Topography Tracker")
	trk := searchMoveByName(t, e, "Topography Tracker", state.ZHand)
	// The replacement body's two explores must both hit lands (no election
	// between them): arrange a Forest and a Mountain on top.
	top := arrangeLibraryTop(t, e, "Forest", "Mountain")
	addMana(t, e, 0, "GGG")
	castCardByName(t, e, trk)

	// The ETB's Map token is on the battlefield.
	passUntil(t, e, func() bool {
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Map Token" {
				return true
			}
		}
		return false
	})
	var mapID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Map Token" {
			mapID = id
		}
	}
	if mapID == 0 {
		t.Fatal("no Map token on the battlefield after the Tracker entered")
	}

	addMana(t, e, 0, "G")
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision pending for the token's activation")
	}
	opt := abilityOption(t, e, mapID, 0)
	submitChoices(t, e, opt.Index)
	exploreTarget(t, e, trk)

	// The activation resolves over the following priority passes.
	passUntil(t, e, func() bool { return len(exploreRecords(e)) == 2 })

	// Two explores, both the land shape (the chainAskDeck filler's top two
	// cards are a Forest and a Mountain), and nothing else: the replaced
	// explore never revealed anything, so exactly two reveal Notes exist.
	recs := exploreRecords(e)
	if len(recs) != 2 {
		t.Fatalf("explore records = %d, want 2 (the replacement's Num$ 2)", len(recs))
	}
	seen := map[state.ObjID]bool{}
	for _, r := range recs {
		if r.Obj != trk || r.Player != 0 {
			t.Fatalf("record %+v does not name the tracker as the explorer", r)
		}
		if r.Amount != 1 || len(r.IDs) != 1 || (r.IDs[0] != top[0] && r.IDs[0] != top[1]) {
			t.Fatalf("record %+v, want the Amount-1 land shape over the arranged top", r)
		}
		if seen[r.IDs[0]] {
			t.Fatalf("the same card %+v was revealed twice", r.IDs)
		}
		seen[r.IDs[0]] = true
		if got := e.G.Obj(r.IDs[0]); got == nil || got.Zone != state.ZHand {
			t.Fatalf("revealed card zone = %+v, want the hand", got)
		}
	}
	if got := len(revealNotes(e)); got != 2 {
		t.Fatalf("public reveal notes = %d, want exactly 2 (the replaced process never revealed)", got)
	}
	if got := counterOn(t, e, trk, "P1P1"); got != 0 {
		t.Fatalf("tracker P1P1 counters = %d, want 0 (both reveals were lands)", got)
	}
	if got := e.G.Obj(mapID); got == nil || got.Zone != state.ZCeased {
		t.Fatalf("map token zone = %+v, want the sacrificed token ceased (a token that left the battlefield ceases to exist, CR 111.7)", got)
	}
}

// TestTwistsAndTurnsScryThenExplore is the other repl:Explore body: "instead
// you scry 1, then that creature explores" (ReplaceWith$ DBScry, body DB$ Scry
// | SubAbility$ DBExplore). The replaced process performs nothing; the scry is
// a real arrange ask posed INSIDE the replacement body, and the body's chained
// explore is a fresh explore of the replaced card. The library top is arranged
// to a land so the body's explore takes the land shape with no second ask.
func TestTwistsAndTurnsScryThenExplore(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := chainAskDeck(t, reg, "Wildgrowth Walker", "Twists and Turns")
	walker := searchMoveByName(t, e, "Wildgrowth Walker", state.ZHand)
	addMana(t, e, 0, "GG")
	castCardByName(t, e, walker)
	passUntil(t, e, func() bool {
		o := e.G.Obj(walker)
		return o != nil && o.Zone == state.ZBattlefield
	})

	top := arrangeLibraryTop(t, e, "Forest")
	twists := searchMoveByName(t, e, "Twists and Turns", state.ZHand)
	addMana(t, e, 0, "G")
	castCardByName(t, e, twists)

	// Twists' ETB asks its explore target.
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask = %+v, want KTarget", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == walker {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the walker was not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// The replacement's scry ask, posed inside the replacement body.
	de := passUntilAsk(t, e)
	if de == nil || de.Kind != decision.KArrange {
		t.Fatalf("arrange ask = %+v, want KArrange", de)
	}
	submitChoices(t, e, de.Options[len(de.Options)-1].Index) // bottom (decline-to-keep)

	// The body's chained explore of the walker: the arranged Forest on top
	// goes to the hand (land shape, no further ask), and the walker's own
	// explore trigger fires off the record (+1/+1 counter and 3 life).
	passUntil(t, e, func() bool { return counterOn(t, e, walker, "P1P1") == 1 })
	recs := exploreRecords(e)
	if recs[0].Obj != walker || recs[0].Amount != 1 || len(recs[0].IDs) != 1 || recs[0].IDs[0] != top[0] {
		t.Fatalf("explore record = %+v, want the walker exploring the arranged Forest", recs[0])
	}
	if got := counterOn(t, e, walker, "P1P1"); got != 1 {
		t.Fatalf("walker P1P1 counters = %d, want 1 (the explore trigger's)", got)
	}
	if got := e.G.Players[0].Life; got != int32(23) {
		t.Fatalf("life = %d, want 23 (the walker's explore trigger gained 3)", got)
	}
}
