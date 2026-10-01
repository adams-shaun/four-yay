package searchprobe

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// ---- hand-built scenarios ------------------------------------------------

type seatBoard struct {
	seat        state.PlayerID
	lib, hand   int
	handCards   []uint32 // the actor's own hand only
	battlefield []uint32
	graveyard   []uint32
}

func knownTestBoard(t *testing.T, seats ...seatBoard) Board {
	t.Helper()
	list := func(ids []uint32) []BoardCard {
		out := []BoardCard{}
		for _, id := range ids {
			out = append(out, BoardCard{ID: id})
		}
		return out
	}
	var b Board
	for _, s := range seats {
		p := BoardPlayer{Seat: s.seat, LibrarySize: s.lib, HandSize: s.hand, Battlefield: list(s.battlefield), Graveyard: list(s.graveyard), Exile: []BoardCard{}, Command: []BoardCard{}}
		if s.seat == 0 {
			p.Hand = list(s.handCards)
		}
		b.Players = append(b.Players, p)
	}
	return b
}

func refsOf(ids []Identity) []uint32 {
	out := []uint32{}
	for _, id := range ids {
		out = append(out, id.ID)
	}
	return out
}

func knownLibrary(k KnownCards, p state.PlayerID) (top, bottom, members []uint32) {
	for _, l := range k.Libraries {
		if l.Player == p {
			return refsOf(l.Top), refsOf(l.Bottom), refsOf(l.Members)
		}
	}
	return []uint32{}, []uint32{}, []uint32{}
}

func knownHand(k KnownCards, p state.PlayerID) []uint32 {
	for _, h := range k.Hands {
		if h.Player == p {
			return refsOf(h.Cards)
		}
	}
	return []uint32{}
}

func wantRefs(t *testing.T, what string, got []uint32, want ...uint32) {
	t.Helper()
	if want == nil {
		want = []uint32{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// scryHistory: the actor (seat 0) scries 2 over its cards 1 and 2, keeping
// 2 on top and sending 1 to the bottom.
func scryHistory(t *testing.T) History {
	ids := []Identity{{ID: 1, Name: "Alpha", Owner: 0}, {ID: 2, Name: "Beta", Owner: 0}, {ID: 3, Name: "Gamma", Owner: 0}}
	options := []ObservedOption{
		{Action: Action{Decision: decision.KArrange, Kind: "bottom", Obj: 1}},
		{Action: Action{Decision: decision.KArrange, Kind: "bottom", Obj: 2}},
	}
	return History{Actor: 0, Answers: map[int][]Action{1: {options[1].Action}}, Frames: []Frame{
		{Identities: ids[2:], Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}, {Kind: events.Shuffle, Player: 1}},
			Board: knownTestBoard(t, seatBoard{seat: 0, lib: 10, hand: 1, handCards: []uint32{3}}, seatBoard{seat: 1, lib: 10, hand: 1})},
		{Identities: ids[:2], Events: []ObservedEvent{{Kind: events.Note, Player: 0, From: state.ZLibrary, Text: "looks at the top of the library", Secret: true}},
			Decision: &ObservedDecision{Player: 0, Kind: decision.KArrange, Min: 0, Max: 2, Options: options},
			Board:    knownTestBoard(t, seatBoard{seat: 0, lib: 10, hand: 1, handCards: []uint32{3}}, seatBoard{seat: 1, lib: 10, hand: 1})},
		{Events: []ObservedEvent{{Kind: events.Scry, Player: 0, Amount: 1}, {Kind: events.LibraryOrder, Player: 0, Secret: true}},
			Board: knownTestBoard(t, seatBoard{seat: 0, lib: 10, hand: 1, handCards: []uint32{3}}, seatBoard{seat: 1, lib: 10, hand: 1})},
	}}
}

func TestKnownCardsScryPositions(t *testing.T) {
	h := scryHistory(t)
	// At the arrange decision the window is the top of the library, top first.
	atDecision, err := ProjectKnownCards(History{Actor: 0, Frames: h.Frames[:2]})
	if err != nil {
		t.Fatal(err)
	}
	top, bottom, members := knownLibrary(atDecision, 0)
	wantRefs(t, "decision top", top, 1, 2)
	wantRefs(t, "decision bottom", bottom)
	wantRefs(t, "decision members", members, 1, 2)

	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	top, bottom, members = knownLibrary(k, 0)
	wantRefs(t, "scried top", top, 2)
	wantRefs(t, "scried bottom", bottom, 1)
	wantRefs(t, "scried members", members, 1, 2)
	wantRefs(t, "actor hand", knownHand(k, 0), 3)
	wantRefs(t, "opponent hand", knownHand(k, 1))
}

func TestKnownCardsShuffleWipesPositionsKeepsMembers(t *testing.T) {
	h := scryHistory(t)
	h.Frames = append(h.Frames, Frame{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}},
		Board: knownTestBoard(t, seatBoard{seat: 0, lib: 10, hand: 1, handCards: []uint32{3}}, seatBoard{seat: 1, lib: 10, hand: 1})})
	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	top, bottom, members := knownLibrary(k, 0)
	wantRefs(t, "top", top)
	wantRefs(t, "bottom", bottom)
	wantRefs(t, "members", members, 1, 2)
	// An unseen draw from a shuffled library may have taken any member.
	h.Frames = append(h.Frames, Frame{Events: []ObservedEvent{{Kind: events.Draw, Player: 0, From: state.ZLibrary, To: state.ZHand}},
		Board: knownTestBoard(t, seatBoard{seat: 0, lib: 9, hand: 2, handCards: []uint32{3}}, seatBoard{seat: 1, lib: 10, hand: 1})})
	k, err = ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _, members = knownLibrary(k, 0)
	wantRefs(t, "members after unseen draw", members)
}

func TestKnownCardsDrawOfKnownTopMovesToHand(t *testing.T) {
	h := scryHistory(t)
	h.Frames = append(h.Frames, Frame{Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 2, From: state.ZLibrary, To: state.ZHand, Secret: true}},
		Board: knownTestBoard(t, seatBoard{seat: 0, lib: 9, hand: 2, handCards: []uint32{3, 2}}, seatBoard{seat: 1, lib: 10, hand: 1})})
	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	top, bottom, members := knownLibrary(k, 0)
	wantRefs(t, "top", top)
	wantRefs(t, "bottom", bottom, 1)
	wantRefs(t, "members", members, 1)
	wantRefs(t, "hand", knownHand(k, 0), 2, 3)
}

// An opponent's known top card is drawn unseen: the drawer's hand now holds
// it. The known top here comes from a tuck (a public move into the library
// appends at the bottom) into a library that holds only that card.
func TestKnownCardsUnseenDrawOfKnownCard(t *testing.T) {
	ids := []Identity{{ID: 7, Name: "Bear", Owner: 1}}
	h := History{Actor: 0, Frames: []Frame{
		{Identities: ids, Events: []ObservedEvent{{Kind: events.Shuffle, Player: 1}},
			Board: knownTestBoard(t, seatBoard{seat: 0, lib: 5}, seatBoard{seat: 1, lib: 0, hand: 3, battlefield: []uint32{7}})},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 0, Obj: 7, From: state.ZBattlefield, To: state.ZLibrary}},
			Board: knownTestBoard(t, seatBoard{seat: 0, lib: 5}, seatBoard{seat: 1, lib: 1, hand: 3})},
	}}
	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	top, bottom, members := knownLibrary(k, 1)
	wantRefs(t, "tucked top", top)
	wantRefs(t, "tucked bottom", bottom, 7)
	wantRefs(t, "tucked members", members, 7)
	h.Frames = append(h.Frames, Frame{Events: []ObservedEvent{{Kind: events.Draw, Player: 1, From: state.ZLibrary, To: state.ZHand, Secret: true}},
		Board: knownTestBoard(t, seatBoard{seat: 0, lib: 5}, seatBoard{seat: 1, lib: 0, hand: 4})})
	if k, err = ProjectKnownCards(h); err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "drawer hand", knownHand(k, 1), 7)
	_, _, members = knownLibrary(k, 1)
	wantRefs(t, "library after draw", members)

	// In a bigger library the tucked bottom card survives an unseen draw.
	big := History{Actor: 0, Frames: []Frame{h.Frames[0], h.Frames[1], {}}}
	big.Frames[0].Board = knownTestBoard(t, seatBoard{seat: 0, lib: 5}, seatBoard{seat: 1, lib: 8, hand: 3, battlefield: []uint32{7}})
	big.Frames[1].Board = knownTestBoard(t, seatBoard{seat: 0, lib: 5}, seatBoard{seat: 1, lib: 9, hand: 3})
	big.Frames[2] = Frame{Events: []ObservedEvent{{Kind: events.Draw, Player: 1, From: state.ZLibrary, To: state.ZHand, Secret: true}},
		Board: knownTestBoard(t, seatBoard{seat: 0, lib: 5}, seatBoard{seat: 1, lib: 8, hand: 4})}
	if k, err = ProjectKnownCards(big); err != nil {
		t.Fatal(err)
	}
	_, bottom, _ = knownLibrary(k, 1)
	wantRefs(t, "bottom after draw from a larger library", bottom, 7)
	wantRefs(t, "drawer hand", knownHand(k, 1))
}

func TestKnownCardsRevealedHandCardStaysKnownUntilItLeaves(t *testing.T) {
	ids := []Identity{{ID: 9, Name: "Dragon", Owner: 1}}
	opp := func(hand int) seatBoard { return seatBoard{seat: 1, lib: 30, hand: hand} }
	me := seatBoard{seat: 0, lib: 30}
	h := History{Actor: 0, Frames: []Frame{
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 1}}, Board: knownTestBoard(t, me, opp(7))},
		{Identities: ids, Events: []ObservedEvent{{Kind: events.Note, Player: 1, IDs: []uint32{9}, Text: "revealed Dragon as a cost"}}, Board: knownTestBoard(t, me, opp(7))},
		// Kept: an ordinary turn of unseen draws and a land play leave it.
		{Events: []ObservedEvent{{Kind: events.Draw, Player: 1, From: state.ZLibrary, To: state.ZHand, Secret: true}}, Board: knownTestBoard(t, me, opp(8))},
	}}
	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "revealed hand", knownHand(k, 1), 9)

	// An unseen hand-to-library move (Brainstorm) may have been that card.
	hidden := h
	hidden.Frames = append(append([]Frame(nil), h.Frames...), Frame{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 1, From: state.ZHand, To: state.ZLibrary, Secret: true}}, Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 31, hand: 7})})
	if k, err = ProjectKnownCards(hidden); err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "hand after unseen put-back", knownHand(k, 1))

	// A card that left the zone publicly is dropped.
	cast := h
	cast.Frames = append(append([]Frame(nil), h.Frames...), Frame{Events: []ObservedEvent{{Kind: events.PutOnStack, Player: 1, Obj: 9, From: state.ZHand, To: state.ZStack}}, Board: knownTestBoard(t, me, opp(7))})
	if k, err = ProjectKnownCards(cast); err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "hand after cast", knownHand(k, 1))
}

func TestKnownCardsBounceThenDiscard(t *testing.T) {
	ids := []Identity{{ID: 4, Name: "Bear", Owner: 1}}
	me := seatBoard{seat: 0, lib: 30}
	h := History{Actor: 0, Frames: []Frame{
		{Identities: ids, Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 30, hand: 2, battlefield: []uint32{4}})},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 0, Obj: 4, From: state.ZBattlefield, To: state.ZHand}}, Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 30, hand: 3})},
	}}
	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "bounced", knownHand(k, 1), 4)
	h.Frames = append(h.Frames, Frame{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 1, Obj: 4, From: state.ZHand, To: state.ZGraveyard}}, Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 30, hand: 2, graveyard: []uint32{4}})})
	if k, err = ProjectKnownCards(h); err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "discarded", knownHand(k, 1))
}

func TestKnownCardsUnmodelledSizeChangeForgets(t *testing.T) {
	ids := []Identity{{ID: 4, Name: "Bear", Owner: 1}}
	me := seatBoard{seat: 0, lib: 30}
	h := History{Actor: 0, Frames: []Frame{
		{Identities: ids, Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 30, hand: 2, battlefield: []uint32{4}})},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 0, Obj: 4, From: state.ZBattlefield, To: state.ZHand}}, Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 30, hand: 3})},
		// The hand shrank with no observed event: some unmodelled channel.
		{Board: knownTestBoard(t, me, seatBoard{seat: 1, lib: 30, hand: 2})},
	}}
	k, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	wantRefs(t, "hand after unexplained shrink", knownHand(k, 1))
}

// ---- engine-driven soundness ---------------------------------------------

type knownTally struct{ top, bottom, oppHand, oppLibrary, frames int }

func (k *knownTally) add(known KnownCards) {
	for _, h := range known.Hands {
		if h.Player != known.Actor {
			k.oppHand += len(h.Cards)
		}
	}
	for _, l := range known.Libraries {
		k.top += len(l.Top)
		k.bottom += len(l.Bottom)
		if l.Player != known.Actor {
			k.oppLibrary += len(l.Members)
		}
	}
	k.frames++
}

// TestKnownCardsAreTrueInRealGames plays real repo decks that scry, surveil,
// rearrange, Brainstorm and bounce, and after every observed frame checks,
// for both seats' observation, that every claim the projection makes holds
// in the TRUE engine. A claim that fails here is knowledge the seat did not
// have.
func TestKnownCardsAreTrueInRealGames(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pairs := [][2]string{
		{"ur-delver", "dimir-tempo"},
		{"uw-tempo", "mono-blue-tempo"},
		{"foundations-keen-engineering", "uw-control"},
		{"the-epic-storm", "ur-delver"},
		{"mono-blue-tempo", "foundations-keen-engineering"},
	}
	var tally knownTally
	for pi, pair := range pairs {
		decks := make([][]*cards.Card, 2)
		for i, n := range pair {
			var err error
			if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
				t.Fatal(err)
			}
		}
		for seed := uint64(1); seed <= 3; seed++ {
			e := rules.New(rules.Config{Seed: 40_000_000 + uint64(pi)*100 + seed, Names: pair[:], Decks: decks, Tokens: reg.Tokens})
			e.Advance()
			collectors := []*Collector{NewCollector(0), NewCollector(1)}
			trackers := []*KnownCardTracker{NewKnownCardTracker(0), NewKnownCardTracker(1)}
			live := []bool{true, true}
			rngs := BotRandoms(seed, 2)
			board := botpolicy.NewBoard(2)
			pos := 0
			for i := 0; i < 1500 && (live[0] || live[1]); i++ {
				d := e.Pending()
				for s := range collectors {
					if !live[s] {
						continue
					}
					frame, err := collectors[s].Capture(e, e.L.Events[pos:])
					if err != nil {
						// An unsupported observation shape ends that seat's
						// perspective, exactly as it ends a searched decision.
						var f *Failure
						if !errors.As(err, &f) {
							t.Fatal(err)
						}
						live[s] = false
						continue
					}
					if err := trackers[s].Observe(frame); err != nil {
						t.Fatal(err)
					}
					known := trackers[s].Known()
					if err := known.holds(e, collectors[s]); err != nil {
						t.Fatalf("%v seed %d seat %d frame %d: projection claims what is not true: %v", pair, seed, s, i, err)
					}
					tally.add(known)
				}
				if d == nil {
					break
				}
				in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
				if live[d.Player] {
					actions, rest, err := collectors[d.Player].IntentActions(d, in)
					if err != nil || len(rest) > 0 {
						// A Rest pile is not in History; stop that seat's
						// perspective rather than feed it a partial answer.
						live[d.Player] = false
					} else {
						trackers[d.Player].Answer(actions)
					}
				}
				pos = len(e.L.Events)
				if err := e.Submit(in); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Logf("known-card claims over %d observed frames: top %d bottom %d opponent-hand %d opponent-library %d", tally.frames, tally.top, tally.bottom, tally.oppHand, tally.oppLibrary)
	if tally.frames < 1000 || tally.top == 0 || tally.oppHand == 0 {
		t.Fatalf("soundness sweep is vacuous: %+v", tally)
	}
}

// ---- synthetic engine fixtures ---------------------------------------------

type effectGame struct {
	setup     PublicGame
	h         History
	engine    *rules.Engine
	collector *Collector
	tracker   *KnownCardTracker
}

// playEffectGame plays a two-seat game of the given decks under a forced
// chance tape until the actor's first priority with an empty stack after an
// event of kind (moving to zone `to`, when kind is MoveZone) was observed. It
// checks the projection against the true engine at every frame.
func playEffectGame(t *testing.T, decks [][]*cards.Card, tape []rules.ChanceDraw, kind events.Kind, to state.Zone) effectGame {
	t.Helper()
	setup := PublicGame{Names: []string{"a", "b"}, Decks: decks}
	e, err := rules.NewHypothetical(rules.Config{Seed: 1, Names: setup.Names, Decks: decks}, tape)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	g := effectGame{setup: setup, engine: e, collector: NewCollector(0), tracker: NewKnownCardTracker(0)}
	g.h = History{Actor: 0, Answers: make(map[int][]Action)}
	r := rand.New(rand.NewPCG(4, 8))
	pos := 0
	seen := false
	for i := 0; i < 300; i++ {
		f, err := g.collector.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatal(err)
		}
		g.h.Frames = append(g.h.Frames, f)
		if err := g.tracker.Observe(f); err != nil {
			t.Fatal(err)
		}
		if err := g.tracker.Known().holds(e, g.collector); err != nil {
			t.Fatalf("frame %d: projection claims what is not true: %v", i, err)
		}
		if i > 0 {
			for _, ev := range f.Events {
				if ev.Kind == kind && (kind != events.MoveZone || ev.To == to) {
					seen = true
				}
			}
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("ended before history root")
		}
		if seen && len(e.G.Stack) == 0 && d.Player == 0 {
			return g
		}
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		if d.Player == 0 {
			if g.h.Answers[i], err = g.collector.Actions(d, in); err != nil {
				t.Fatal(err)
			}
			g.tracker.Answer(g.h.Answers[i])
		}
		pos = len(e.L.Events)
		if err = e.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("fixture never executed requested effect (turn %d, actor hand %d, battlefield %d)", e.G.Turn, len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZBattlefield, 0)))
	return g
}

func effectDecks(t *testing.T, actorSpell string) [][]*cards.Card {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	spell := syntheticCard(t, "Name:Known Experiment\nManaCost:2\nTypes:Sorcery\nA:SP$ "+actorSpell+"\nOracle:Fixture.\n")
	decks := make([][]*cards.Card, 2)
	for p := range decks {
		decks[p] = make([]*cards.Card, 20)
		for i := range decks[p] {
			decks[p][i] = land
		}
	}
	decks[0][0] = spell
	return decks
}

// effectTape forces the toss and every shuffle; opponentLast picks the value
// the opponent's shuffle draws.
func effectTape(opponent func(n int) int) []rules.ChanceDraw {
	tape := []rules.ChanceDraw{{Bound: 2, Value: 0}}
	for p := 0; p < 2; p++ {
		for n := 20; n > 1; n-- {
			v := n - 1
			if p == 1 && opponent != nil {
				v = opponent(n)
			}
			tape = append(tape, rules.ChanceDraw{Bound: n, Value: v})
		}
	}
	return tape
}

func uniqueLibraryCards(t *testing.T, deck []*cards.Card) []*cards.Card {
	t.Helper()
	out := append([]*cards.Card(nil), deck...)
	for i := 1; i < len(out); i++ {
		out[i] = syntheticCard(t, "Name:Library Card "+string(rune('A'+i))+"\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	}
	return out
}

// TestKnownCardsFromEngineEffects drives the real engine through the
// knowledge-bearing effects and checks, at every frame, that the projection
// is true (playEffectGame), then that the expected fact was learned.
func TestKnownCardsFromEngineEffects(t *testing.T) {
	for _, tc := range []struct {
		name, sa string
		kind     events.Kind
		to       state.Zone
		check    func(t *testing.T, k KnownCards)
	}{
		{"scry", "Scry | ScryNum$ 1", events.LibraryOrder, state.ZLibrary, func(t *testing.T, k KnownCards) {
			top, bottom, _ := knownLibrary(k, 0)
			if len(top)+len(bottom) == 0 {
				t.Fatalf("scry taught no position: %+v", k)
			}
		}},
		{"surveil", "Surveil | Amount$ 3", events.LibraryOrder, state.ZLibrary, func(t *testing.T, k KnownCards) {}},
		{"rearrange", "RearrangeTopOfLibrary | Defined$ You | NumCards$ 3", events.LibraryOrder, state.ZLibrary, func(t *testing.T, k KnownCards) {
			if top, _, _ := knownLibrary(k, 0); len(top) != 3 {
				t.Fatalf("rearranged top = %v, want 3 known cards", top)
			}
		}},
		{"bounce", "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land", events.MoveZone, state.ZHand, func(t *testing.T, k KnownCards) {
			if len(knownHand(k, 1)) == 0 {
				t.Fatalf("bounced opponent lands unknown: %+v", k)
			}
		}},
		{"tuck", "ChangeZoneAll | Origin$ Battlefield | Destination$ Library | ChangeType$ Land | LibraryPosition$ 0", events.MoveZone, state.ZLibrary, func(t *testing.T, k KnownCards) {
			if _, _, members := knownLibrary(k, 1); len(members) == 0 {
				t.Fatalf("tucked opponent lands unknown: %+v", k)
			}
		}},
		{"search", "ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Basic | ChangeNum$ 1 | Shuffle$ True", events.Shuffle, state.ZLibrary, func(t *testing.T, k KnownCards) {
			if top, bottom, _ := knownLibrary(k, 0); len(top)+len(bottom) != 0 {
				t.Fatalf("positions survived a shuffle: %v %v", top, bottom)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decks := effectDecks(t, tc.sa)
			// Distinct names in the actor's library make a wrong position
			// claim detectable by name, not only by reference.
			decks[0] = uniqueLibraryCards(t, decks[0])
			g := playEffectGame(t, decks, effectTape(nil), tc.kind, tc.to)
			k, err := ProjectKnownCards(g.h)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(k, g.tracker.Known()) {
				t.Fatal("ProjectKnownCards differs from the incremental tracker")
			}
			tc.check(t, k)
		})
	}
}

// TestKnownCardsProjectionIgnoresHiddenState: two engines that differ only in
// hidden, never-revealed information -- where the opponent's one distinct
// card sits in its hidden zones -- produce the same observation history and
// therefore the same projection, which is non-trivial (the bounced lands are
// known in the opponent's hand).
func TestKnownCardsProjectionIgnoresHiddenState(t *testing.T) {
	decks := effectDecks(t, "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land")
	decks[1] = append([]*cards.Card(nil), decks[1]...)
	decks[1][0] = syntheticCard(t, "Name:Secret Relic\nManaCost:9\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	// Both tapes leave the relic undrawn in the opponent's library, at
	// different depths; the opponent's other 19 cards are identical.
	a := playEffectGame(t, decks, effectTape(func(int) int { return 0 }), events.MoveZone, state.ZHand)
	b := playEffectGame(t, decks, effectTape(func(n int) int {
		if n == 20 {
			return 1
		}
		return 0
	}), events.MoveZone, state.ZHand)
	relic := func(e *rules.Engine) (state.Zone, int) {
		for _, zone := range []state.Zone{state.ZHand, state.ZLibrary} {
			for i, id := range e.G.Zone(zone, 1) {
				if o := e.G.Obj(id); o != nil && o.Card != nil && o.Card.Faces[0].Name == "Secret Relic" {
					return zone, i
				}
			}
		}
		t.Fatal("secret relic left the hidden zones")
		return 0, 0
	}
	za, ia := relic(a.engine)
	zb, ib := relic(b.engine)
	if za == zb && ia == ib {
		t.Fatalf("fixture needs different hidden states: relic at %v/%d in both", za, ia)
	}
	if !reflect.DeepEqual(a.h, b.h) {
		for i := 0; i < len(a.h.Frames) && i < len(b.h.Frames); i++ {
			if !reflect.DeepEqual(a.h.Frames[i], b.h.Frames[i]) {
				t.Fatalf("fixture needs identical observations: frame %d differs (%d/%d frames)\n%+v\n%+v", i, len(a.h.Frames), len(b.h.Frames), a.h.Frames[i].Events, b.h.Frames[i].Events)
			}
		}
		t.Fatalf("fixture needs identical observations (%d/%d frames)", len(a.h.Frames), len(b.h.Frames))
	}
	ka, err := ProjectKnownCards(a.h)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := ProjectKnownCards(b.h)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ka, kb) {
		t.Fatalf("projection depends on hidden state:\n%+v\n%+v", ka, kb)
	}
	if len(knownHand(ka, 1)) == 0 {
		t.Fatalf("projection is trivial: %+v", ka)
	}
	var named []Identity
	for _, h := range ka.Hands {
		named = append(named, h.Cards...)
	}
	for _, l := range ka.Libraries {
		named = append(named, l.Members...)
	}
	for _, card := range named {
		if card.Name == "Secret Relic" {
			t.Fatal("projection names the never-revealed card")
		}
	}
}

// ---- the Sample constraint --------------------------------------------------

// sampleDigestWithoutClaims is worldsDigest with the constraint's own
// diagnostic zeroed, so an on/off pair can be compared for identical worlds.
func sampleDigestWithoutClaims(t *testing.T, r SampleResult) string {
	r.KnownCardClaims = 0
	return worldsDigest(t, r)
}

func TestSampleKnownCardsConstraintHonouredAndInvisibleOnReplays(t *testing.T) {
	for _, tc := range []struct {
		name, sa string
		kind     events.Kind
		to       state.Zone
	}{
		{"rearrange", "RearrangeTopOfLibrary | Defined$ You | NumCards$ 3", events.LibraryOrder, state.ZLibrary},
		{"scry", "Scry | ScryNum$ 1", events.LibraryOrder, state.ZLibrary},
		{"bounce", "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land", events.MoveZone, state.ZHand},
		{"tuck", "ChangeZoneAll | Origin$ Battlefield | Destination$ Library | ChangeType$ Land | LibraryPosition$ 0", events.MoveZone, state.ZLibrary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decks := effectDecks(t, tc.sa)
			decks[0] = uniqueLibraryCards(t, decks[0])
			g := playEffectGame(t, decks, effectTape(nil), tc.kind, tc.to)
			opts := SampleOptions{Seed: 818, Attempts: 16, Worlds: 4, MaxSubmits: 5000}
			off, err := Sample(g.setup, g.h, opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.KnownCards = true
			on, err := Sample(g.setup, g.h, opts)
			if err != nil {
				t.Fatal(err)
			}
			known, err := ProjectKnownCards(g.h)
			if err != nil {
				t.Fatal(err)
			}
			if on.KnownCardClaims != known.Count() || on.KnownCardClaims == 0 {
				t.Fatalf("claims = %d, projection holds %d", on.KnownCardClaims, known.Count())
			}
			if on.KnownCardViolations != 0 {
				t.Fatalf("a full replay contradicted the projection: %+v", on)
			}
			if len(on.Worlds) != 4 {
				t.Fatalf("worlds = %d", len(on.Worlds))
			}
			for _, w := range on.Worlds {
				if err := known.Holds(w); err != nil {
					t.Fatal(err)
				}
			}
			if sampleDigestWithoutClaims(t, on) != sampleDigestWithoutClaims(t, off) {
				t.Fatal("the constraint changed which worlds a full replay accepts")
			}
			if off.KnownCardClaims != 0 || off.KnownCardViolations != 0 {
				t.Fatalf("flag off reported constraint counters: %+v", off)
			}
		})
	}
}

// TestKnownCardsHoldsDetectsViolations: a world whose known cards were moved
// fails Holds. The tampering runs through events.Apply on a clone.
func TestKnownCardsHoldsDetectsViolations(t *testing.T) {
	decks := effectDecks(t, "RearrangeTopOfLibrary | Defined$ You | NumCards$ 3")
	decks[0] = uniqueLibraryCards(t, decks[0])
	g := playEffectGame(t, decks, effectTape(nil), events.LibraryOrder, state.ZLibrary)
	known, err := ProjectKnownCards(g.h)
	if err != nil {
		t.Fatal(err)
	}
	w := World{Engine: g.engine.Clone(), Observer: g.collector.Clone()}
	if err := known.Holds(w); err != nil {
		t.Fatalf("the true world fails its own projection: %v", err)
	}
	lib := append([]state.ObjID(nil), w.Engine.G.Zone(state.ZLibrary, 0)...)
	lib[0], lib[1] = lib[1], lib[0]
	events.Apply(w.Engine.G, events.Event{Kind: events.LibraryOrder, Player: 0, IDs: lib, Secret: true})
	if err := known.Holds(w); err == nil {
		t.Fatal("swapped known top cards were not detected")
	}

	bounce := effectDecks(t, "ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ Land")
	g = playEffectGame(t, bounce, effectTape(nil), events.MoveZone, state.ZHand)
	if known, err = ProjectKnownCards(g.h); err != nil {
		t.Fatal(err)
	}
	w = World{Engine: g.engine.Clone(), Observer: g.collector.Clone()}
	hand := knownHand(known, 1)
	if len(hand) == 0 {
		t.Fatal("fixture learned no opponent hand card")
	}
	id := w.Observer.object(hand[0])
	events.Apply(w.Engine.G, events.Event{Kind: events.MoveZone, Player: 1, Obj: id, From: state.ZHand, To: state.ZLibrary, Secret: true})
	if err := known.Holds(w); err == nil {
		t.Fatal("a known hand card moved to the library was not detected")
	}
}

// TestSampleKnownCardsOnRealDeckFixture: on the bench fixture the constraint
// accepts exactly the worlds the unconstrained sampler accepts.
func TestSampleKnownCardsOnRealDeckFixture(t *testing.T) {
	f := benchRoot(t)
	opts := benchSampleOptions()
	opts.MinESS = 1
	off, err := Sample(f.setup, f.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.KnownCards = true
	on, err := Sample(f.setup, f.h, opts)
	if err != nil {
		t.Fatal(err)
	}
	if on.KnownCardViolations != 0 {
		t.Fatalf("violations %d", on.KnownCardViolations)
	}
	if sampleDigestWithoutClaims(t, on) != sampleDigestWithoutClaims(t, off) {
		t.Fatal("the constraint changed the real-deck sample")
	}
	t.Logf("real-deck fixture: %d known-card claims, %d worlds", on.KnownCardClaims, len(on.Worlds))
}
