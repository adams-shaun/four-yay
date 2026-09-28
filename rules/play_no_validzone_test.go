package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestReversalOfFortuneAndPlayNoValidZone(t *testing.T) {
	t.Parallel()
	e, _, source := newFixtureDeckWithOpponentCard(t, 92341,
		"Name:Play source\nManaCost:0\nTypes:Sorcery\nOracle:x\n",
		"Name:Own probe\nManaCost:1 G\nTypes:Sorcery\nOracle:x\n",
		"Name:Opponent instant\nManaCost:R\nTypes:Instant\nOracle:x\n")
	bear := moveSeeded(t, e, 1, "Name:Opponent instant\nManaCost:R\nTypes:Instant\nOracle:x\n", state.ZHand)
	mine := moveSeeded(t, e, 0, "Name:Own probe\nManaCost:1 G\nTypes:Sorcery\nOracle:x\n", state.ZHand)
	if e.G.Obj(bear).Zone != state.ZHand || e.G.Obj(bear).Owner != 1 || e.G.Obj(mine).Zone != state.ZHand {
		t.Fatalf("precondition: cards not in expected hands: opponent=%+v own=%+v", e.G.Obj(bear), e.G.Obj(mine))
	}
	if e.G.Obj(source).Zone != state.ZHand {
		t.Fatalf("precondition: play source zone = %s, want hand", e.G.Obj(source).Zone)
	}
	resolvePlay := func(sa *cards.SA, remembered, targets []state.Target) *decision.Decision {
		effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Remembered: remembered, Targets: targets}, sa)
		return e.Pending()
	}

	rememberedSA := &cards.SA{Kind: "DB", API: "Play", Params: map[string]string{
		"Valid": "Card.nonCreature+nonLand+IsRemembered", "Optional": "True",
	}}
	ask := resolvePlay(rememberedSA, []state.Target{{Obj: bear}}, nil)
	if ask == nil || ask.Kind != decision.KModes || ask.ResumeKind != "play" {
		t.Fatalf("remembered Play ask = %+v, want play KModes", ask)
	}
	if len(ask.Options) != 1 || ask.Options[0].Obj != bear {
		t.Fatalf("remembered options = %+v, want only opponent-hand remembered card %d", ask.Options, bear)
	}
	submitChoices(t, e) // decline; the source card remains untouched

	// An unbound Valid$ filter must retain the controller-only hand default,
	// pinning the privacy boundary used by Face of Boe and Conundrum.
	ask = resolvePlay(&cards.SA{Kind: "DB", API: "Play", Params: map[string]string{
		"Valid": "Card", "Optional": "True",
	}}, nil, nil)
	if ask == nil || len(ask.Options) == 0 {
		t.Fatalf("controller-hand Play ask = %+v", ask)
	}
	for _, option := range ask.Options {
		if option.Obj == bear {
			t.Fatalf("unbound Valid$ leaked opponent hand card %d into options: %+v", bear, ask.Options)
		}
	}
	submitChoices(t, e)

	// Reversal of Fortune's real no-zone Play parameter shape: TargetedPlayerCtrl
	// reaches the targeted player's hand; CopyCard casts a copy, not the original.
	original := bear
	if e.G.Obj(original).Zone != state.ZHand || e.G.Obj(original).Owner != 1 {
		t.Fatalf("precondition: targeted original = %+v", e.G.Obj(original))
	}
	copySA := &cards.SA{Kind: "DB", API: "Play", Params: map[string]string{
		"CopyCard": "True", "Optional": "True", "Valid": "Sorcery.TargetedPlayerCtrl,Instant.TargetedPlayerCtrl",
		"WithoutManaCost": "True", "ValidSA": "Spell",
	}}
	ask = resolvePlay(copySA, nil, []state.Target{{Player: 1, IsPlayer: true}})
	if ask == nil || ask.Kind != decision.KModes || ask.ResumeKind != "play" || len(ask.Options) != 1 || ask.Options[0].Obj != original {
		t.Fatalf("Reversal play ask = %+v; wanted opponent card %d", ask, original)
	}
	before := len(e.L.Events)
	submitChoices(t, e, ask.Options[0].Index)
	if got := e.G.Obj(original); got == nil || got.Zone != state.ZHand {
		t.Fatalf("CopyCard moved the original: %+v", got)
	}
	var copied state.ObjID
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.PutOnStack && ev.Obj != original {
			copied = ev.Obj
		}
	}
	copyObj := e.G.Obj(copied)
	if copied == 0 || copyObj == nil || copyObj.Zone != state.ZStack || !copyObj.IsCopy || copyObj.Face() == nil || copyObj.Face().Name != "Opponent instant" {
		t.Fatalf("Reversal cast copy = %+v (id %d), events=%+v", copyObj, copied, e.L.Events[before:])
	}
}
