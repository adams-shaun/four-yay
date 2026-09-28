package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSacrificeCostPrompt pins the shared sacrifice chooser's wording for
// activations and casts without changing its candidate rules: tapping a
// creature does not stop it paying a sacrifice-only cost.
func TestSacrificeCostPrompt(t *testing.T) {
	t.Parallel()
	const abilitySrc = "Name:Sacrifice Source\nTypes:Artifact\n" +
		"A:AB$ GainLife | Cost$ Sac<1/Creature> | Defined$ You | LifeAmount$ 1 | SpellDescription$ Gain 1 life.\nOracle:x\n"
	const creatureSrc = "Name:Prompt Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	const spellSrc = "Name:Prompt Rites\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Draw | Cost$ B Sac<1/Creature> | NumCards$ 2 | SpellDescription$ Draw two cards.\nOracle:x\n"

	t.Run("activated ability", func(t *testing.T) {
		e, _, source := newFixtureDeck(t, 181, abilitySrc, creatureSrc)
		bear := moveSeeded(t, e, 0, creatureSrc, state.ZBattlefield)
		moveSeeded(t, e, 0, abilitySrc, state.ZBattlefield)
		e.emit(events.Event{Kind: events.Tap, Obj: bear})
		if e.G.Obj(source).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield || !e.G.Obj(bear).Tapped {
			t.Fatal("activation fixture needs a battlefield source and a tapped battlefield sacrifice candidate")
		}
		e.Advance()
		submitChoices(t, e, abilityOption(t, e, source, 0).Index)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Source != source || len(d.Options) != 1 || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != bear {
			t.Fatalf("activated ability sacrifice decision = %+v, want tapped bear offered", d)
		}
		if want := "Sacrifice a permanent to activate Sacrifice Source"; d.Prompt != want {
			t.Fatalf("activation prompt = %q, want %q", d.Prompt, want)
		}
	})

	t.Run("spell", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 182, spellSrc, creatureSrc)
		bear := moveSeeded(t, e, 0, creatureSrc, state.ZBattlefield)
		if e.G.Obj(spell).Zone != state.ZHand || e.G.Obj(bear).Zone != state.ZBattlefield {
			t.Fatal("spell fixture needs a spell in hand and a battlefield sacrifice candidate")
		}
		addMana(t, e, 0, "B")
		castIndex := -1
		for _, o := range e.Pending().Options {
			if o.Kind == "cast" && o.Obj == spell {
				castIndex = o.Index
				break
			}
		}
		if castIndex < 0 {
			t.Fatal("spell with sacrifice cost not offered")
		}
		submitChoices(t, e, castIndex)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Source != spell || len(d.Options) != 1 || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != bear {
			t.Fatalf("spell sacrifice decision = %+v, want bear offered", d)
		}
		if want := "Sacrifice a permanent to cast Prompt Rites"; d.Prompt != want {
			t.Fatalf("spell prompt = %q, want %q", d.Prompt, want)
		}
	})
}
