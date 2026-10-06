package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestSingleModeCharmAnnouncesChainTargets(t *testing.T) {
	t.Parallel()
	charm := "Name:Chain Charm\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Charm | CharmNum$ 1 | Choices$ MTarget\n" +
		"SVar:MTarget:DB$ Draw | NumCards$ 1 | SubAbility$ TargetPlayer\n" +
		"SVar:TargetPlayer:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2 | SpellDescription$ Target player loses 2 life.\n" +
		"Oracle:x\n"
	e, _, id := newFixtureDeck(t, 6391, charm)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Charm object = %+v, want it in hand", o)
	}
	if e.G.Players[1].Life <= 0 {
		t.Fatalf("precondition: target player life = %d, want a living player", e.G.Players[1].Life)
	}
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 1 {
		t.Fatalf("pending = %+v, want the single mode announcement", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
		t.Fatalf("pending after selecting one mode = %+v, want linked cast target ask", d)
	}
	if len(d.Options) == 0 {
		t.Fatal("linked target ask has no legal candidates")
	}
	var opponent int
	foundOpponent := false
	for _, opt := range d.Options {
		if opt.Kind == "player" && opt.Player == 1 {
			opponent, foundOpponent = opt.Index, true
		}
	}
	if !foundOpponent {
		t.Fatalf("opponent is not a legal linked target: %+v", d.Options)
	}
	if e.G.Obj(id).Zone != state.ZStack || len(e.G.Stack) != 1 {
		t.Fatalf("pre-payment state: card zone=%s stack=%d, want spell pushed before target announcement", e.G.Obj(id).Zone, len(e.G.Stack))
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 1 {
		t.Fatalf("pre-payment mana = %d black, want 1 before target answer", got)
	}
	submitChoices(t, e, opponent)
	if e.Pending() == nil || e.Pending().Kind != decision.KPriority {
		t.Fatalf("after linked target answer pending = %+v, want payment/priority progression", e.Pending())
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('B')]; got != 0 {
		t.Fatalf("mana after target announcement = %d black, want payment completed", got)
	}
}

func TestSingleModeCharmOmitsModeWithUnavailableChainTarget(t *testing.T) {
	t.Parallel()
	charm := "Name:Chain Feasibility Charm\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Charm | CharmNum$ 1 | Choices$ MPlayer,MCreature\n" +
		"SVar:MPlayer:DB$ Draw | NumCards$ 1 | SubAbility$ TargetPlayer\n" +
		"SVar:TargetPlayer:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2\n" +
		"SVar:MCreature:DB$ Draw | NumCards$ 1 | SubAbility$ TargetCreature\n" +
		"SVar:TargetCreature:DB$ Destroy | ValidTgts$ Creature.YouCtrl\n" +
		"Oracle:x\n"
	e, _, id := newFixtureDeck(t, 6392, charm)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Charm object = %+v, want it in hand", o)
	}
	noTargets := cards.ResolveSVar(e.G.Obj(id).Face().SVars, "TargetCreature")
	if got := e.legalTargetCandidates(0, id, id, noTargets); len(got) != 0 {
		t.Fatalf("precondition: unavailable mode unexpectedly has legal creature targets: %+v", got)
	}
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 1 || d.Options[0].Label != "MPlayer" {
		t.Fatalf("modes = %+v, want only the feasible MPlayer mode", d)
	}
}
