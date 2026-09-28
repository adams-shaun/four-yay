package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func staticCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %s", name)
	}
	return c
}

func TestStaticPTVariablesUseLiveSourceSVars(t *testing.T) {
	t.Parallel()
	t.Run("Timberpack Wolf reads a live SVar", func(t *testing.T) {
		e := layerEngine(t)
		one := onBoardCard(t, e, 0, staticCorpusCard(t, "Timberpack Wolf"))
		two := onBoardCard(t, e, 0, staticCorpusCard(t, "Timberpack Wolf"))
		if got := e.Derived(one); got.Power != 3 || got.Toughness != 3 {
			t.Fatalf("first wolf = %d/%d, want 3/3", got.Power, got.Toughness)
		}
		if got := e.Derived(two); got.Power != 3 || got.Toughness != 3 {
			t.Fatalf("second wolf = %d/%d, want 3/3", got.Power, got.Toughness)
		}
	})

	t.Run("Death's Shadow is a signed live value", func(t *testing.T) {
		e := layerEngine(t)
		shadow := onBoardCard(t, e, 0, staticCorpusCard(t, "Death's Shadow"))
		if got := e.Derived(shadow); got.Power != -7 || got.Toughness != -7 {
			t.Fatalf("shadow = %d/%d, want -7/-7 at 20 life", got.Power, got.Toughness)
		}
		e.checkStateBased()
		if o := e.G.Obj(shadow); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("Death's Shadow zone = %v, want graveyard after zero-toughness SBA", o)
		}
	})

	t.Run("Tarmogoyf SetPower X", func(t *testing.T) {
		e := layerEngine(t)
		goyf := onBoardCard(t, e, 0, staticCorpusCard(t, "Tarmogoyf"))
		if got := e.Derived(goyf); got.Power != 0 || got.Toughness != 1 {
			t.Fatalf("empty-graveyard Tarmogoyf = %d/%d, want 0/1", got.Power, got.Toughness)
		}
		// Distinct card types among both players' graveyards price X: a
		// creature, an instant, a land, and a duplicate creature make three.
		for _, src := range []string{
			"Name:Grave Beast\nTypes:Creature Beast\nPT:2/2\nOracle:x\n",
			"Name:Grave Bolt\nTypes:Instant\nOracle:x\n",
			"Name:Grave Forest\nTypes:Land Forest\nOracle:x\n",
			"Name:Grave Twin\nTypes:Creature Beast\nPT:2/2\nOracle:x\n",
		} {
			id := onBoardCard(t, e, 0, card(t, src))
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
		}
		if got := e.Derived(goyf); got.Power != 3 || got.Toughness != 4 {
			t.Fatalf("Tarmogoyf over three distinct types = %d/%d, want 3/4", got.Power, got.Toughness)
		}
	})

	t.Run("Krovikan Mist SetPower X", func(t *testing.T) {
		e := layerEngine(t)
		one := onBoardCard(t, e, 0, staticCorpusCard(t, "Krovikan Mist"))
		onBoardCard(t, e, 0, staticCorpusCard(t, "Krovikan Mist"))
		if got := e.Derived(one); got.Power != 2 || got.Toughness != 2 {
			t.Fatalf("Krovikan Mist = %d/%d, want 2/2 for two Illusions", got.Power, got.Toughness)
		}
	})

	t.Run("one-sided corpus setters preserve the other characteristic", func(t *testing.T) {
		e := layerEngine(t)
		forerunners := onBoardCard(t, e, 0, staticCorpusCard(t, "Kolaghan Forerunners"))
		if got := e.Derived(forerunners); got.Power != 1 || got.Toughness != 3 {
			t.Fatalf("Kolaghan Forerunners = %d/%d, want 1/3", got.Power, got.Toughness)
		}

		e = layerEngine(t)
		commanderCard := staticCorpusCard(t, "Wintermoor Commander")
		commander := onBoardCard(t, e, 0, commanderCard)
		onBoardCard(t, e, 0, commanderCard)
		if got := e.Derived(commander); got.Power != 2 || got.Toughness != 2 {
			t.Fatalf("Wintermoor Commander = %d/%d, want 2/2", got.Power, got.Toughness)
		}
	})
}

func TestTypePredicatesSeeChangelingAndLayerFour(t *testing.T) {
	t.Parallel()
	t.Run("Goblin Piledriver counts Changeling Outcast", func(t *testing.T) {
		e := layerEngine(t)
		pile := onBoardCard(t, e, 0, staticCorpusCard(t, "Goblin Piledriver"))
		outcast := onBoardCard(t, e, 0, staticCorpusCard(t, "Changeling Outcast"))
		resolveAttackPump(t, e, pile, outcast)
		if got := e.Power(pile); got != 3 {
			t.Fatalf("Piledriver power = %d, want 3 with attacking Changeling", got)
		}
	})

	t.Run("Changeling has every creature type but no noncreature subtype", func(t *testing.T) {
		e := layerEngine(t)
		outcast := onBoardCard(t, e, 0, staticCorpusCard(t, "Changeling Outcast"))
		if !effects.MatchesSpecFrom(e.G, "Creature.Surrakar", outcast, 0, outcast) {
			t.Error("Changeling Outcast must match the corpus creature type Surrakar")
		}
		if effects.MatchesSpecFrom(e.G, "Creature.nonSurrakar", outcast, 0, outcast) {
			t.Error("Changeling Outcast must not match Creature.nonSurrakar")
		}
		for _, spec := range []string{"Creature.Arcane", "Creature.Alara", "Creature.Ajani"} {
			if effects.MatchesSpecFrom(e.G, spec, outcast, 0, outcast) {
				t.Errorf("Changeling Outcast must not match %s", spec)
			}
		}
	})

	t.Run("real type-changing static feeds a later real type predicate", func(t *testing.T) {
		e := layerEngine(t)
		onBoardCard(t, e, 0, staticCorpusCard(t, "Kudo, King Among Bears"))
		target := onBoardCard(t, e, 0, staticCorpusCard(t, "Llanowar Elves"))
		onBoardCard(t, e, 0, staticCorpusCard(t, "Beorn the Fierce"))
		// Kudo's layer-4 static makes the Elf a Bear and sets its base P/T
		// to 2/2. Beorn's later real Bear predicate must see that derived
		// type and add +2/+2.
		if got := e.Derived(target); got.Power != 4 || got.Toughness != 4 {
			t.Fatalf("Kudo/Beorn Elf = %d/%d, want 4/4", got.Power, got.Toughness)
		}
	})

	t.Run("later static sees a layer-four type", func(t *testing.T) {
		e := layerEngine(t)
		giver := onBoard(t, e, 0, "Name:Type Giver\nTypes:Creature Wizard\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other | AddType$ Goblin\nOracle:x\n")
		target := onBoard(t, e, 0, "Name:Target\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
		lord := onBoard(t, e, 0, "Name:Lord\nTypes:Creature Goblin\nPT:1/1\nS:Mode$ Continuous | Affected$ Goblin.Other | AddPower$ 1 | AddToughness$ 1\nOracle:x\n")
		_ = giver
		_ = lord
		if got := e.Derived(target); got.Power != 2 || got.Toughness != 2 {
			t.Fatalf("layer-four Goblin target = %d/%d, want 2/2", got.Power, got.Toughness)
		}
	})

	t.Run("special positive predicate sees a layer-four supertype", func(t *testing.T) {
		e := layerEngine(t)
		onBoard(t, e, 0, "Name:Crown Giver\nTypes:Creature Wizard\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other | AddType$ Legendary\nOracle:x\n")
		target := onBoard(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		onBoard(t, e, 0, "Name:Legend Lord\nTypes:Creature Human\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Legendary | AddPower$ 1\nOracle:x\n")
		if got := e.Derived(target); got.Power != 3 {
			t.Fatalf("derived Legendary Bear power = %d, want 3", got.Power)
		}
	})

	t.Run("special negated predicate sees a layer-four type", func(t *testing.T) {
		e := layerEngine(t)
		onBoard(t, e, 0, "Name:Animist\nTypes:Creature Wizard\nPT:1/1\nS:Mode$ Continuous | Affected$ Permanent.Other | AddType$ Creature\nOracle:x\n")
		target := onBoard(t, e, 0, "Name:Relic\nTypes:Artifact\nPT:2/2\nOracle:x\n")
		onBoard(t, e, 0, "Name:Anti Creature Lord\nTypes:Creature Human\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.nonCreature | AddPower$ 1\nOracle:x\n")
		if got := e.Derived(target); got.Power != 2 {
			t.Fatalf("derived Creature must not satisfy nonCreature, power = %d, want 2", got.Power)
		}
	})

	t.Run("Any base sees a layer-four card type", func(t *testing.T) {
		e := layerEngine(t)
		onBoard(t, e, 0, "Name:Animist\nTypes:Creature Wizard\nPT:1/1\nS:Mode$ Continuous | Affected$ Permanent.Other | AddType$ Creature\nOracle:x\n")
		target := onBoard(t, e, 0, "Name:Relic\nTypes:Artifact\nPT:2/2\nOracle:x\n")
		onBoard(t, e, 0, "Name:Any Lord\nTypes:Creature Human\nPT:1/1\nS:Mode$ Continuous | Affected$ Any | AddPower$ 1\nOracle:x\n")
		if got := e.Derived(target); got.Power != 3 {
			t.Fatalf("derived Creature must satisfy Any, power = %d, want 3", got.Power)
		}
	})
}
