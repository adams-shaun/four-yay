package rules

// The may-play candidate index's offer-surface equivalence (the loop-growth
// index brief): with the index on and off, the offered option sets and keys
// are byte-identical on the Rakdos, the Muscle + Phyrexian Altar + Forsaken
// Miner loop board -- the N-growing grant accumulation the index exists for
// -- and on a full deterministic repo-deck game, and the engine's event
// stream is untouched. The index-on arm also runs mayPlayCandIndexVerify,
// which re-derives every indexed enumeration with the nested scan and panics
// on a difference, so an offer that deduplication could mask still goes red.
//
// This file's package-var toggling (mayPlayCandIndexOff/mayPlayCandIndexVerify)
// is why the test must never call t.Parallel: other tests in this package
// read the same package state.
import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// capturePending serialises one pending decision's full offer surface: the
// decision's wire JSON (options in order, every wire field) plus the
// server-side-only fields the wire omits -- Option.Key (the typed may-play
// permission a cast consumes), MayPlayPerm and Controller, and whether a
// server-side Grant rides the option. Byte-identical across two runs is the
// brief's offer-surface equivalence.
func capturePending(e *Engine) string {
	d := e.Pending()
	if d == nil {
		return "<none>"
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "unmarshalable: " + err.Error()
	}
	var side []string
	for _, o := range d.Options {
		side = append(side, fmt.Sprintf("i=%d key=%q perm=%q ctrl=%d grant=%v",
			o.Index, o.Key, o.MayPlayPerm, o.Controller, o.Grant != nil))
	}
	return string(raw) + "\n" + strings.Join(side, ";")
}

// minerBoard is loopBoard with a custom seat-1 deck: seat 1's library is
// what Rakdos's sacrifice trigger digs to exile, so an interleaved
// devil/mountain deck puts non-land cards under the dig and makes the SPELL
// walk's effect arm (mayPlaySpellIds) a real part of the offer surface, not
// just the land walk's (an all-mountain library offers only play_land).
func minerBoard(t *testing.T, deck1 []*cards.Card, names []string, zones []state.Zone) (*Engine, []state.ObjID) {
	t.Helper()
	reg := loopCorpus(t)
	deck0 := mountainDeck(t, 80)
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatal(name)
		}
		deck0 = append(deck0, c)
	}
	if deck1 == nil {
		deck1 = mountainDeck(t, 80)
	}
	e := New(Config{Seed: 17, Names: []string{"pilot", "opponent"}, Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)
	ids := make([]state.ObjID, len(names))
	for i, name := range names {
		c, _ := reg.Lookup(name)
		for j := range e.G.Objs {
			o := &e.G.Objs[j]
			if o.Owner == 0 && o.Card == c {
				ids[i] = o.ID
				break
			}
		}
		if ids[i] == 0 {
			t.Fatal("missing object", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: ids[i], From: e.G.Obj(ids[i]).Zone, To: zones[i]})
	}
	e.priorityRound()
	return e, ids
}

// runMinerLoop drives the Miner cycle (the TestLoopPrototypeMiner body,
// minus that test's own instrumentation) with the index in whatever state
// the caller set, capturing the offer surface at the top of every iteration
// -- the priority decision that pays the O(grants x exiled cards) walk the
// index removes. It returns the captures, the event count and the chain
// head; the cycle's own net-resource postconditions guard the replay.
func runMinerLoop(t *testing.T, n int, deck1 []*cards.Card) (snapshots []string, eventCount int, head string) {
	e, ids := minerBoard(t, deck1, []string{"Rakdos, the Muscle", "Phyrexian Altar", "Forsaken Miner"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield})
	loopDrain(t, e, 0, 0, 1)
	library := len(e.G.Zone(state.ZLibrary, 1))
	for i := 0; i < n; i++ {
		snapshots = append(snapshots, capturePending(e))
		loopAction(t, e, ids[1], "activate")
		loopDrain(t, e, ids[2], 0, 1)
		if e.G.Obj(ids[2]).Zone != state.ZBattlefield {
			t.Fatalf("iteration %d: Miner in %v", i+1, e.G.Obj(ids[2]).Zone)
		}
		if e.G.Players[0].Pool.Total() != 0 || len(e.G.Zone(state.ZLibrary, 1)) != max(0, library-i-1) || e.G.Players[1].Life != 20 || e.G.Over {
			t.Fatal("Miner cycle net resources wrong")
		}
	}
	return snapshots, len(e.L.Events), e.L.Head()
}

// countMayPlayGrants is the precondition read: how many MayPlay
// ContinuousEffects the board has accumulated (the loop-growth profile
// measured exactly N after N Miner sacrifices).
func countMayPlayGrants(e *Engine) (total int) {
	for _, ce := range e.active() {
		if ce.MayPlay {
			total++
		}
	}
	return total
}

func TestMayPlayIndexOfferSurface(t *testing.T) {
	oldOff, oldVer := mayPlayCandIndexOff, mayPlayCandIndexVerify
	t.Cleanup(func() { mayPlayCandIndexOff, mayPlayCandIndexVerify = oldOff, oldVer })

	// Seat 1's library is what Rakdos's trigger digs to exile: interleaving
	// Ornithopters (free casts) with the mountains puts non-land cards under
	// the dig, so both effect arms (mayPlayLandIds and mayPlaySpellIds)
	// contribute real may-play offers -- the thopter is castable from exile
	// even with an empty pool, so a cast offer exists at the priority point.
	reg0 := loopCorpus(t)
	thopter, ok := reg0.Lookup("Ornithopter")
	if !ok {
		t.Fatal("Ornithopter missing from corpus")
	}
	mountains := mountainDeck(t, 40)
	deck1 := make([]*cards.Card, 0, 80)
	for i := 0; i < 40; i++ {
		deck1 = append(deck1, thopter, mountains[i])
	}

	// --- Rakdos / Phyrexian Altar / Forsaken Miner board -------------------
	mayPlayCandIndexOff, mayPlayCandIndexVerify = true, false
	offSnaps, offEvents, offHead := runMinerLoop(t, 20, deck1)
	mayPlayCandIndexOff, mayPlayCandIndexVerify = false, true
	onSnaps, onEvents, onHead := runMinerLoop(t, 20, deck1)

	// Preconditions: the accumulating shape the index targets is real. The
	// board holds exactly N grants after N sacrifices (measured 2026-09-28),
	// the loop's own postconditions already held the cycle's net resources,
	// and the offer surface carries may-play offers from a public zone
	// through BOTH walks -- a vacuous (empty-offers) comparison would pass
	// an index that offers nothing at all.
	e, ids := minerBoard(t, deck1, []string{"Rakdos, the Muscle", "Phyrexian Altar", "Forsaken Miner"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield})
	loopDrain(t, e, 0, 0, 1)
	loopAction(t, e, ids[1], "activate")
	loopDrain(t, e, ids[2], 0, 1)
	// A second sacrifice digs the next library card, which the interleaved
	// deck makes an Ornithopter: the spell walk's effect arm then offers a
	// cast from exile, not just the land walk's play_land.
	loopAction(t, e, ids[1], "activate")
	loopDrain(t, e, ids[2], 0, 1)
	if got := countMayPlayGrants(e); got != 2 {
		t.Fatalf("precondition: %d active MayPlay grants after two sacrifices, want 2", got)
	}
	landExile, castExile := 0, 0
	d := e.Pending()
	if d == nil {
		t.Fatal("precondition: no pending decision after the sacrifice resolves")
	}
	for _, o := range d.Options {
		if o.Obj == 0 {
			continue
		}
		z := e.G.Obj(o.Obj).Zone
		if z != state.ZExile && z != state.ZGraveyard {
			continue
		}
		switch o.Kind {
		case "play_land":
			landExile++
		case "cast":
			castExile++
		}
	}
	if landExile == 0 {
		t.Fatalf("precondition: no may-play land offer from a public zone (land walk not exercised; options: %+v)", d.Options)
	}
	if castExile == 0 {
		t.Fatalf("precondition: no may-play cast offer from a public zone (spell walk not exercised; options: %+v)", d.Options)
	}
	if len(onSnaps) != 20 || len(offSnaps) != 20 {
		t.Fatalf("precondition: captured %d/%d snapshots, want 20 each", len(onSnaps), len(offSnaps))
	}
	if onSnaps[0] == onSnaps[19] {
		t.Fatal("precondition: the offer surface did not change from iteration 1 to 20; the accumulating shape is not exercised")
	}

	// The equivalence: option sets and keys byte-identical, and the engine's
	// event stream and head untouched by the index.
	for i := range onSnaps {
		if onSnaps[i] != offSnaps[i] {
			t.Fatalf("iteration %d: offer surface diverged with the index on\noff: %s\non:  %s", i+1, offSnaps[i], onSnaps[i])
		}
	}
	if onEvents != offEvents || onHead != offHead {
		t.Fatalf("index changed the event stream: off events=%d head=%s, on events=%d head=%s", offEvents, offHead, onEvents, onHead)
	}

	// --- repo decks --------------------------------------------------------
	// One full deterministic 2-seat game over two of the pinned Legacy
	// decks, driven by the shipped test bot, every pending decision's offer
	// surface captured and compared. The index-on arm keeps
	// mayPlayCandIndexVerify on, so every indexed enumeration is re-derived
	// with the nested scan and panics on a difference even where dedupe
	// would mask a diverging pair.
	reg := testutil.CorpusRegistry(t)
	names := testutil.LegacyDeckNames()
	decks := [][]*cards.Card{
		testutil.RepoDeck(t, reg, names[0]),
		testutil.RepoDeck(t, reg, names[1]),
	}
	runDeckGame := func() (snaps []string, head string) {
		cfg := Config{Seed: 42, Names: names[:2], Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards, Mulligans: 1}
		e := NewStartingPlayerChoice(cfg)
		b := newTestBot(7)
		e.AskStartingPlayer()
		e.Advance()
		for n := 0; !e.G.Over && e.Pending() != nil && n < 400000; n++ {
			snaps = append(snaps, capturePending(e))
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("deck game intent %d: %v", n, err)
			}
		}
		if !e.G.Over {
			t.Fatal("deck game did not finish")
		}
		if len(snaps) == 0 {
			t.Fatal("deck game posed no decisions")
		}
		return snaps, e.L.Head()
	}
	mayPlayCandIndexOff, mayPlayCandIndexVerify = true, false
	offDeck, offDeckHead := runDeckGame()
	mayPlayCandIndexOff, mayPlayCandIndexVerify = false, true
	onDeck, onDeckHead := runDeckGame()
	if len(onDeck) != len(offDeck) {
		t.Fatalf("deck game: %d decisions with the index on, %d with it off", len(onDeck), len(offDeck))
	}
	for i := range onDeck {
		if onDeck[i] != offDeck[i] {
			t.Fatalf("deck game decision %d: offer surface diverged with the index on\noff: %s\non:  %s", i+1, offDeck[i], onDeck[i])
		}
	}
	if onDeckHead != offDeckHead {
		t.Fatalf("deck game: index changed the chain head: off=%s on=%s", offDeckHead, onDeckHead)
	}
}

// idsOf finds the battlefield object a corpus card became.
func idsOf(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for j := range e.G.Objs {
		o := &e.G.Objs[j]
		if o.Owner == 0 && o.Face() != nil && o.Face().Name == name {
			return o.ID
		}
	}
	t.Fatalf("no battlefield object named %s", name)
	return 0
}
