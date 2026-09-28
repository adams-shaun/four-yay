package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func compleatedCast(t *testing.T, reg *cards.Registry, name, mana string, lifePaid, wantLoyalty int32) {
	t.Helper()
	card := mustCorpusCard(t, reg, name)
	if len(card.Faces) == 0 || card.Faces[0] == nil || card.Faces[0].Loyalty == "" {
		t.Fatalf("%s has no printed planeswalker loyalty", name)
	}
	foundKeyword := false
	for _, k := range card.Faces[0].Keywords {
		if strings.EqualFold(strings.TrimSpace(k), "Compleated") {
			foundKeyword = true
		}
	}
	if !foundKeyword {
		t.Fatalf("%s has no printed K:Compleated", name)
	}
	cfg := Config{Seed: 31, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append([]*cards.Card{card}, mountainDeck(t, 39)...), mountainDeck(t, 40),
	}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	cardToHand(t, e, card)
	addMana(t, e, 0, mana)
	beforeLife := e.G.Players[0].Life
	cast := castByName(t, e, 0, name)
	if cast == nil {
		t.Fatalf("%s was not offered to cast", name)
	}
	submitChoices(t, e, cast.Index)
	paid := int32(0)
	for paid < lifePaid {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("%s payment decision = %+v", name, d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pay_life" {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatalf("%s payment menu lacks pay_life at %d: %+v", name, paid, d.Options)
		}
		submitChoices(t, e, idx)
		paid += 2
	}
	if lifePaid == 0 {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("%s expected mana payment decision, got %+v", name, d)
		}
		found := false
		for _, o := range d.Options {
			if strings.EqualFold(o.Kind, "pay_b") {
				submitChoices(t, e, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s payment menu lacks a mana option: %+v", name, d.Options)
		}
	}
	passUntilStackEmpty(t, e, 30)
	var permanent *state.Object
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == card && o.Zone == state.ZBattlefield {
			permanent = o
			break
		}
	}
	if permanent == nil {
		t.Fatalf("%s did not enter the battlefield", name)
	}
	if got := permanent.Counter("LOYALTY"); got != wantLoyalty {
		t.Fatalf("%s loyalty = %d, want %d", name, got, wantLoyalty)
	}
	if e.G.Players[0].Life != beforeLife-lifePaid {
		t.Fatalf("%s life = %d, want %d", name, e.G.Players[0].Life, beforeLife-lifePaid)
	}
	replayCheck(t, e, cfg)
}

func TestCompleatedLoyaltyFollowsPhyrexianLifePaid(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	t.Run("Vraska life", func(t *testing.T) {
		compleatedCast(t, reg, "Vraska, Betrayal's Sting", "CCCCB", 2, 4)
	})
	t.Run("Vraska mana", func(t *testing.T) {
		compleatedCast(t, reg, "Vraska, Betrayal's Sting", "CCCCBB", 0, 6)
	})
	t.Run("Nissa two life faces", func(t *testing.T) {
		compleatedCast(t, reg, "Nissa, Ascended Animist", "CCCGG", 4, 3)
	})
}
