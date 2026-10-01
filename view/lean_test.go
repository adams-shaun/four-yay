package view_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// leanExpect is the fresh Seat projection v with the parts omit names
// zeroed exactly as ProjectLeanInto leaves them.
func leanExpect(v view.View, omit view.Omit) view.View {
	if omit&view.OmitDecision != 0 {
		v.Decision = nil
	}
	if omit&view.OmitOwnDeck != 0 {
		v.OwnDeck = nil
	}
	stripCard := func(cv *view.CardView) {
		if omit&view.OmitAbilityCosts != 0 {
			cv.AbilityCosts = nil
		}
		if omit&view.OmitEffectiveCost != 0 {
			cv.EffectiveManaCost = ""
		}
		if omit&view.OmitDerivedChars != 0 {
			cv.Power, cv.Toughness, cv.Keywords = 0, 0, nil
			cv.Name = cv.Printing.Name
		}
	}
	stripList := func(cs []view.CardView, drop bool) []view.CardView {
		if drop {
			return nil
		}
		cs = slices.Clone(cs)
		for i := range cs {
			stripCard(&cs[i])
		}
		return cs
	}
	lists := omit&view.OmitCardLists != 0
	players := slices.Clone(v.Players)
	for i := range players {
		p := &players[i]
		if omit&view.OmitLibrary != 0 {
			p.Library = nil
		}
		if omit&view.OmitAvailable != 0 {
			p.Available = nil
		}
		if omit&(view.OmitArchetype|view.OmitCardLists|view.OmitDerivedChars) != 0 {
			p.Archetype = nil
		}
		if omit&view.OmitPotential != 0 {
			p.PotentialActions = nil
		}
		p.Library = stripList(p.Library, false)
		p.Battlefield = stripList(p.Battlefield, lists && p.ID != v.Viewer)
		p.Hand = stripList(p.Hand, lists)
		p.Graveyard = stripList(p.Graveyard, lists)
		p.Exile = stripList(p.Exile, lists)
		p.Command = stripList(p.Command, lists)
		p.PlanarDeck = stripList(p.PlanarDeck, lists)
		p.Commanders = stripList(p.Commanders, lists)
		if lists {
			p.CommanderCasts = nil
		}
		if p.LibraryTop != nil {
			top := *p.LibraryTop
			stripCard(&top)
			p.LibraryTop = &top
		}
	}
	v.Players = players
	stack := slices.Clone(v.Stack)
	for i := range stack {
		if stack[i].Card != nil {
			c := *stack[i].Card
			stripCard(&c)
			stack[i].Card = &c
		}
	}
	v.Stack = stack
	return v
}

// TestProjectLeanIntoAcrossWholeGames pins the lean projection: at every
// decision of whole real games, for every seat and a set of omit masks
// (none, each bit alone, the builtin seats' masks, all of them), a View
// refilled in place by ProjectLeanInto is exactly the fresh Seat projection
// with the omitted parts zeroed -- in particular omit == 0 is Project. It
// also pins view.RoundFold against RoundOf and rules.Engine's
// ViewCharacteristics against its four single-fact accessors for every
// object.
func TestProjectLeanIntoAcrossWholeGames(t *testing.T) {
	const seats, maxDecisions = 4, 400
	names, decks := testutil.SampleDecks(t, seats)
	all := view.OmitLibrary | view.OmitAvailable | view.OmitArchetype | view.OmitAbilityCosts |
		view.OmitEffectiveCost | view.OmitDecision | view.OmitOwnDeck | view.OmitPotential |
		view.OmitCardLists | view.OmitDerivedChars
	masks := []view.Omit{0, all, all &^ (view.OmitCardLists | view.OmitDerivedChars)}
	for b := view.OmitLibrary; b <= view.OmitDerivedChars; b <<= 1 {
		masks = append(masks, b)
	}
	dsts := make([]view.View, len(masks)*seats)
	checked := 0
	for _, seed := range []uint64{3, 11} {
		e := rules.New(rules.Config{Seed: seed, Names: names, Decks: decks})
		e.Advance()
		b := seat.NewBot(seed)
		var fold view.RoundFold
		check := func() {
			d := e.Pending()
			if got, want := fold.Of(e.G, e.L.Events), view.RoundOf(e.G, e.L.Events); got != want {
				t.Fatalf("seed %d: RoundFold %d, RoundOf %d", seed, got, want)
			}
			for p := 0; p < seats; p++ {
				full := view.Project(e.G, e, state.PlayerID(p), d)
				for k, m := range masks {
					dst := &dsts[k*seats+p]
					view.ProjectLeanInto(dst, e.G, e, state.PlayerID(p), d, m)
					if want := leanExpect(full, m); !reflect.DeepEqual(*dst, want) {
						equalProjection(t, "lean", *dst, want)
					}
					checked++
				}
			}
			for id := state.ObjID(1); int(id) <= len(e.G.Objs); id++ {
				name, kw, pw, tg := e.ViewCharacteristics(id)
				kw = slices.Clone(kw)
				if name != e.Name(id) || !slices.Equal(kw, e.Keywords(id)) || pw != e.Power(id) || tg != e.Toughness(id) {
					t.Fatalf("seed %d obj %d: ViewCharacteristics (%q %v %d/%d) != accessors (%q %v %d/%d)",
						seed, id, name, kw, pw, tg, e.Name(id), e.Keywords(id), e.Power(id), e.Toughness(id))
				}
			}
		}
		for i := 0; i < maxDecisions && !e.G.Over && e.Pending() != nil; i++ {
			check()
			d := e.Pending()
			in, err := b.Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Submit(in); err != nil {
				t.Fatalf("seed %d intent %d: %v", seed, i, err)
			}
		}
		check()
	}
	if checked < 1000 {
		t.Fatalf("only %d lean projections compared", checked)
	}
}
