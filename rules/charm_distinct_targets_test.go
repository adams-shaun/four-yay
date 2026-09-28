package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestDistinctCharmModesKeepTheirOwnTargets proves both halves of the fix:
// the target decision has one slot per selected mode, and resolution binds
// those slots independently rather than handing both effects one flat list.
func TestDistinctCharmModesKeepTheirOwnTargets(t *testing.T) {
	t.Parallel()
	charm := "Name:Distinct Charm\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Charm | CharmNum$ 2 | Choices$ Lose,Gain\n" +
		"SVar:Lose:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2 | SpellDescription$ Target player loses 2 life.\n" +
		"SVar:Gain:DB$ GainLife | ValidTgts$ Player | LifeAmount$ 3 | SpellDescription$ Target player gains 3 life.\n" +
		"Oracle:x\n"
	e, cfg, id := newFixtureDeck(t, 6310, charm)
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the mode announcement", d)
	}
	submitChoices(t, e, 0, 1)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want two per-mode target slots", d)
	}
	first, second := -1, -1
	for _, opt := range d.Options {
		if opt.Group == "charm-mode-0" && opt.Kind == "player" && opt.Player == 0 {
			first = opt.Index
		}
		if opt.Group == "charm-mode-1" && opt.Kind == "player" && opt.Player == 1 {
			second = opt.Index
		}
	}
	if first < 0 || second < 0 || first == second {
		t.Fatalf("fixture did not offer distinct mode targets: first=%d second=%d options=%+v", first, second, d.Options)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	if life0 == life0-2 || life1 == life1+3 {
		t.Fatal("fixture life values make the expected per-mode changes vacuous")
	}
	submitChoices(t, e, first, second)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != life0-2 {
		t.Fatalf("mode 0 target seat 0 life=%d, want %d", got, life0-2)
	}
	if got := e.G.Players[1].Life; got != life1+3 {
		t.Fatalf("mode 1 target seat 1 life=%d, want %d", got, life1+3)
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("resolved Charm zone=%s, want graveyard", e.G.Obj(id).Zone)
	}
	replayCheck(t, e, cfg)
}
