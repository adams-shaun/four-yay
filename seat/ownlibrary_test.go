package seat

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ownLibraryGame seats the named repo decks (a Commander game when every
// deck names a commander) with the corpus registry.
func ownLibraryGame(t *testing.T, seed uint64, names ...string) *rules.Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg := rules.Config{Seed: seed, Names: names, Tokens: reg.Tokens}
	commander := true
	for _, n := range names {
		cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, n))
		f := testutil.RepoDeckFile(t, n)
		if len(f.CommanderNames()) == 0 {
			commander = false
		}
		cfg.Commanders = append(cfg.Commanders, f.CommanderIndices())
	}
	if commander {
		cfg.Format, cfg.StartingLife = rules.FormatCommander, 40
	} else {
		cfg.Commanders = nil
	}
	e := rules.New(cfg)
	e.Advance()
	return e
}

// trueLibrary is the engine's real library of p as counts parallel to m's
// Main rows, plus the number of library cards no Main row names. TEST ORACLE
// ONLY: this is exactly the hidden read the derivation must never make.
func trueLibrary(g *state.Game, p state.PlayerID, m *deck.Manifest) ([]int32, int) {
	counts := make([]int32, len(m.Main))
	foreign := 0
	for _, id := range g.Zone(state.ZLibrary, p) {
		o := g.Obj(id)
		name := o.Card.Faces[0].Name
		i := slices.IndexFunc(m.Main, func(r deck.ManifestRow) bool { return r.Name == name })
		if i < 0 {
			foreign++
			continue
		}
		counts[i]++
	}
	return counts, foreign
}

// viewLibraryCounts folds the projection's own unordered Library list (the
// view's true contents) into counts parallel to m's Main: a second, fully
// independent oracle for the same fact.
func viewLibraryCounts(v view.View, p state.PlayerID, m *deck.Manifest) []int32 {
	counts := make([]int32, len(m.Main))
	for _, pv := range v.Players {
		if pv.ID != p {
			continue
		}
		for _, cv := range pv.Library {
			name := cv.Name
			if cv.CardName != "" {
				name = cv.CardName
			}
			if i := slices.IndexFunc(m.Main, func(r deck.ManifestRow) bool { return r.Name == name }); i >= 0 {
				counts[i]++
			}
		}
	}
	return counts
}

// TestOwnLibraryIsTheTrueLibraryOverWholeGames is the derivation's honesty
// check over ordinary play: on every decision of whole real-deck games
// (constructed pairs and a four-seat Commander game with disguise and
// manifest-style face-down cards), the deciding seat's composition folded by
// the game half (Board.OwnLibrary off state.Game) and by the view half
// (view.OwnLibrary off the projected View) are identical, and whenever it is
// Known it equals the engine's true library multiset exactly (and the view's
// own Library list), with the land split matching. Unknown decisions are
// counted and must stay rare: ordinary games account for the library.
func TestOwnLibraryIsTheTrueLibraryOverWholeGames(t *testing.T) {
	for _, tc := range []struct {
		seed  uint64
		decks []string
		// maxUnknownPct bounds how often the fold may be Unknown.
		maxUnknownPct float64
	}{
		{1, []string{"mono-red-prowess", "mono-blue-tempo"}, 1},
		{2, []string{"the-epic-storm", "uw-control"}, 1},
		{3, []string{"tron", "dimir-tempo"}, 1},
		{4, []string{"death-n-taxes", "eldrazi-stompy"}, 1},
		{5, []string{"deadly-disguise", "pro-shaper", "ulalek-eldrazi", "valgavoth-endless-punishment"}, 5},
	} {
		t.Run(fmt.Sprint(tc.decks), func(t *testing.T) {
			e := ownLibraryGame(t, tc.seed, tc.decks...)
			rngs := make([]*rand.Rand, len(tc.decks))
			for i := range rngs {
				rngs[i] = rand.New(rand.NewPCG(tc.seed, uint64(i)))
			}
			board := botpolicy.NewBoard(len(tc.decks))
			var known, unknown int
			reasons := map[deck.LibraryUnknown]int{}
			for n := 0; n < 4000 && !e.G.Over; n++ {
				d := e.Pending()
				if d == nil {
					break
				}
				me := d.Player
				v := view.Project(e.G, e, me, d)
				var fromView deck.LibraryComposition
				view.OwnLibrary(v, me, &fromView)
				brd := botpolicy.BoardFromGameInto(e.G, e, me, &board)
				if !brd.OwnLibrary.Equal(fromView) {
					t.Fatalf("intent %d seat %d: halves diverged: game %+v, view %+v", n, me, brd.OwnLibrary, fromView)
				}
				m := e.OwnDeck(me)
				if fromView.Known {
					known++
					truth, foreign := trueLibrary(e.G, me, m)
					if foreign != 0 || !slices.Equal(truth, fromView.Counts) {
						t.Fatalf("intent %d seat %d: Known composition %v is not the true library %v (foreign %d)", n, me, fromView.Counts, truth, foreign)
					}
					if vc := viewLibraryCounts(v, me, m); !slices.Equal(vc, fromView.Counts) {
						t.Fatalf("intent %d seat %d: Known composition %v is not the view's Library %v", n, me, fromView.Counts, vc)
					}
					var lands int32
					for _, id := range e.G.Zone(state.ZLibrary, me) {
						if e.G.Obj(id).Card.Faces[0].IsLand() {
							lands++
						}
					}
					if lands != fromView.Lands || fromView.Lands+fromView.Nonlands != int32(len(e.G.Zone(state.ZLibrary, me))) {
						t.Fatalf("intent %d seat %d: land split %d/%d, true lands %d of %d", n, me, fromView.Lands, fromView.Nonlands, lands, len(e.G.Zone(state.ZLibrary, me)))
					}
				} else {
					unknown++
					reasons[fromView.Unknown]++
					if len(fromView.Counts) != 0 || fromView.Lands != 0 || fromView.Nonlands != 0 {
						t.Fatalf("intent %d: an Unknown composition carries counts %+v", n, fromView)
					}
				}
				in := botpolicy.Decide(brd, d, rngs[me])
				if err := e.Submit(in); err != nil {
					t.Fatalf("intent %d: %v", n, err)
				}
			}
			pct := 100 * float64(unknown) / float64(known+unknown)
			t.Logf("%d known, %d unknown (%.2f%%) %v, over %d turns", known, unknown, pct, reasons, e.G.Turn)
			if known < 100 {
				t.Fatalf("only %d Known decisions: the check is vacuous", known)
			}
			if pct > tc.maxUnknownPct {
				t.Fatalf("Unknown on %.2f%% of decisions, want at most %.2f%%: %v", pct, tc.maxUnknownPct, reasons)
			}
		})
	}
}

// ownLibraryMidGame plays the constructed pair to seat 0's first priority
// decision on or after turn 6 and returns two independent clones of it.
func ownLibraryMidGame(t *testing.T) (*rules.Engine, *rules.Engine) {
	t.Helper()
	e := ownLibraryGame(t, 11, "mono-red-prowess", "mono-blue-tempo")
	rngs := []*rand.Rand{rand.New(rand.NewPCG(11, 0)), rand.New(rand.NewPCG(11, 1))}
	board := botpolicy.NewBoard(2)
	for n := 0; n < 4000; n++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			t.Fatal("game ended before the mid-game root")
		}
		if d.Player == 0 && d.Kind == "priority" && e.G.Turn >= 6 {
			return e.Clone(), e.Clone()
		}
		if err := e.Submit(botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("never reached the mid-game root")
	return nil, nil
}

// bothHalves folds seat 0's composition off e both ways and requires the
// halves to agree.
func bothHalves(t *testing.T, e *rules.Engine) deck.LibraryComposition {
	t.Helper()
	var fromView deck.LibraryComposition
	view.OwnLibrary(view.Project(e.G, e, 0, e.Pending()), 0, &fromView)
	if fromGame := botpolicy.BoardFromGame(e.G, e, 0).OwnLibrary; !fromGame.Equal(fromView) {
		t.Fatalf("halves diverged: game %+v, view %+v", fromGame, fromView)
	}
	return fromView
}

// swapCards exchanges the printed identities of two objects: a test-only
// edit that changes WHICH card is hidden where without changing anything
// the seat can see.
func swapCards(g *state.Game, a, b state.ObjID) {
	oa, ob := g.Obj(a), g.Obj(b)
	oa.Card, ob.Card = ob.Card, oa.Card
}

// TestOwnLibraryIgnoresHiddenOrderAndIdentities is the derivation's
// no-leak check: two engines with the same observable state for seat 0 but
// different hidden facts fold the same composition. The hidden edits are
// test-only state surgery on clones of one mid-game engine:
//
//   - the seat's own library reversed (order is hidden);
//   - the opponent's hand and library identities exchanged (hidden to seat 0);
//   - and, in the unknown case, one of the seat's library cards exiled face
//     down under an opponent's look-only permission -- a DIFFERENT card in each
//     engine, so the true libraries differ -- where the fold must say
//     Unknown in both rather than read either library;
//   - and an unseen card entering the seat's library (a foreign card
//     shuffled in), again different in each engine.
func TestOwnLibraryIgnoresHiddenOrderAndIdentities(t *testing.T) {
	a, b := ownLibraryMidGame(t)
	base := bothHalves(t, a)
	if !base.Known {
		t.Fatalf("precondition: mid-game composition is unknown (%s)", base.Unknown)
	}

	// Hidden order and opponent hidden identities.
	lib := slices.Clone(b.G.Zone(state.ZLibrary, 0))
	slices.Reverse(lib)
	b.G.SetZone(state.ZLibrary, 0, lib)
	oppHand, oppLib := b.G.Zone(state.ZHand, 1), b.G.Zone(state.ZLibrary, 1)
	swapped := 0
	for i := 0; i < len(oppHand) && i < len(oppLib); i++ {
		if b.G.Obj(oppHand[i]).Card != b.G.Obj(oppLib[len(oppLib)-1-i]).Card {
			swapCards(b.G, oppHand[i], oppLib[len(oppLib)-1-i])
			swapped++
		}
	}
	if swapped == 0 {
		t.Fatal("precondition: no opponent identity differed to swap")
	}
	if got := bothHalves(t, b); !got.Equal(base) {
		t.Fatalf("hidden order/identity changed the composition:\n a %+v\n b %+v", base, got)
	}

	// A face-down exile the seat may not look at: a different card in each
	// engine (the first and last library cards of different names).
	libA := a.G.Zone(state.ZLibrary, 0)
	first := libA[0]
	last := state.ObjID(0)
	for i := len(libA) - 1; i > 0; i-- {
		if a.G.Obj(libA[i]).Card.Faces[0].Name != a.G.Obj(first).Card.Faces[0].Name {
			last = libA[i]
			break
		}
	}
	if last == 0 {
		t.Fatal("precondition: library holds one name only")
	}
	a2, b2 := a.Clone(), a.Clone()
	exileFaceDown := func(e *rules.Engine, id state.ObjID) {
		events.Apply(e.G, events.Event{Kind: events.MoveZone, Player: 1, Obj: id, From: state.ZLibrary, To: state.ZExile,
			Counter: "exiled_with_face_down_maylook", Amount: 1})
		if o := e.G.Obj(id); o.Zone != state.ZExile || !o.FaceDown || o.MayLookPlayer != 1 {
			t.Fatalf("precondition: face-down exile fold did not land: zone %v facedown %v looker %v", o.Zone, o.FaceDown, o.MayLookPlayer)
		}
	}
	exileFaceDown(a2, first)
	exileFaceDown(b2, last)
	ta, _ := trueLibrary(a2.G, 0, a2.OwnDeck(0))
	tb, _ := trueLibrary(b2.G, 0, b2.OwnDeck(0))
	if slices.Equal(ta, tb) {
		t.Fatal("precondition: the two true libraries do not differ")
	}
	ua, ub := bothHalves(t, a2), bothHalves(t, b2)
	if ua.Known || ua.Unknown&deck.UnknownHiddenOwnCard == 0 || !ua.Equal(ub) {
		t.Fatalf("face-down exile: want an identical Unknown(hidden-own-card) in both, got\n a %+v\n b %+v", ua, ub)
	}

	// An unseen foreign card shuffled into the seat's library: an opponent's
	// library card re-owned into seat 0's library, a different one per
	// engine.
	a3, b3 := a.Clone(), a.Clone()
	shuffleIn := func(e *rules.Engine, pick int) {
		opp := slices.Clone(e.G.Zone(state.ZLibrary, 1))
		id := opp[pick]
		e.G.SetZone(state.ZLibrary, 1, slices.Delete(opp, pick, pick+1))
		e.G.Obj(id).Owner, e.G.Obj(id).Controller = 0, 0
		e.G.SetZone(state.ZLibrary, 0, append(slices.Clone(e.G.Zone(state.ZLibrary, 0)), id))
	}
	shuffleIn(a3, 0)
	shuffleIn(b3, len(a.G.Zone(state.ZLibrary, 1))-1)
	sa, sb := bothHalves(t, a3), bothHalves(t, b3)
	if sa.Known || sa.Unknown&deck.UnknownSizeMismatch == 0 || !sa.Equal(sb) {
		t.Fatalf("shuffled-in card: want an identical Unknown(size-mismatch) in both, got\n a %+v\n b %+v", sa, sb)
	}
}
