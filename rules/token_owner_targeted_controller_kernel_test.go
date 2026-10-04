package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTokenOwnerTargetedControllerSurvivesAMidResolutionAsk: Generous Gift's
// Destroy then Token | TokenOwner$ TargetedController shape with a
// ChoosePlayer ask between them. The Destroy resets the victim's live
// controller to its owner before the chained Token runs, so the token must
// be minted from the target-controller LKI (the controller at the start of
// resolution), across the ask and its re-executed answer.
//
// The victim is OWNED by seat 0 but CONTROLLED by seat 1, so the
// pre-destruction controller (1) and the post-destroy owner/caster (0) are
// distinguishable.
func TestTokenOwnerTargetedControllerSurvivesAMidResolutionAsk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := New(Config{Seed: 741, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens})

	victim := e.G.AddObject(card(t, "Name:Relic\nTypes:Artifact\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim.ID, From: state.ZLibrary, To: state.ZBattlefield})
	steal := card(t, "Name:Steal\nTypes:Sorcery\nManaCost:1 R\nA:SP$ GainControl | ValidTgts$ Permanent\nOracle:x\n")
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: victim.ID}}, TargetsOffered: true},
		steal.Faces[0].SpellAbility())
	if o := e.G.Obj(victim.ID); o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: victim = %+v, want owner 0 / controller 1 on the battlefield", o)
	}

	gift := card(t, "Name:Generous Gift\nManaCost:2 W\nTypes:Instant\n"+
		"A:SP$ Destroy | Defined$ Targeted | ValidTgts$ Permanent | SubAbility$ DBChoose\n"+
		"SVar:DBChoose:DB$ ChoosePlayer | Choices$ Player | SubAbility$ DBToken\n"+
		"SVar:DBToken:DB$ Token | TokenScript$ g_3_3_elephant | TokenOwner$ TargetedController\n"+
		"Oracle:x\n")
	src := e.G.AddObject(gift, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: src.ID, IDs: []state.ObjID{victim.ID}})
	head := gift.Faces[0].Abilities[0]
	if head.Sub == nil || head.Sub.Sub == nil || head.Sub.API != "ChoosePlayer" || head.Sub.Sub.API != "Token" {
		t.Fatalf("precondition: chain not Destroy -> ChoosePlayer -> Token: %+v", head.Sub)
	}

	kr9ResolveTop(e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChoosePlayer ask mid-resolution, got %+v", d)
	}
	if o := e.G.Obj(victim.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: victim = %+v, want already destroyed before the ask", o)
	}
	submitChoices(t, e, d.Options[0].Index)

	if got := tokensNamed(e, 0, "Elephant"); got != 0 {
		t.Fatalf("caster/owner battlefield = %v, want no Elephant token", e.G.Zone(state.ZBattlefield, 0))
	}
	if got := tokensNamed(e, 1, "Elephant"); got != 1 {
		t.Fatalf("target controller battlefield = %v, want exactly one Elephant token", e.G.Zone(state.ZBattlefield, 1))
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognized TokenOwner") {
			t.Fatalf("TargetedController emitted the fallback Note: %q", ev.Text)
		}
	}
}
