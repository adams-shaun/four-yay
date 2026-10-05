package rules

// Kernel-era restoration of effects/effect_remembered_sub_target_test.go's
// TestEffectRememberedTargetPrefetchesChangeZoneSub (What Must Be Done's
// "Release Juno" shape): an Effect with RememberObjects$ Targeted & Self
// whose ChangeZone sub carries the target. The behaviour that matters is
// that the ONE chosen target is both what the Effect remembers (its ETB
// replacement adds two +1/+1 counters to a remembered creature) and what the
// sub returns from the graveyard, and it is asked only once. CR 601.2c: that
// one ask is the cast-time announcement (a cast_sub KTarget), not a
// mid-resolution choice.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestKr1EffectRememberedTargetFeedsTheChangeZoneSub(t *testing.T) {
	t.Parallel()
	src := "Name:JunoFx\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Effect | RememberObjects$ Targeted & Self | ReplacementEffects$ ETBCreat | SubAbility$ DBReturn\n" +
		"SVar:DBReturn:DB$ ChangeZone | ValidTgts$ Creature.YouOwn | TgtPrompt$ Select target creature card | Origin$ Graveyard | Destination$ Battlefield\n" +
		"SVar:ETBCreat:Event$ Moved | ValidCard$ Creature.IsRemembered | Destination$ Battlefield | ReplaceWith$ DBPutP1P1 | ReplacementResult$ Updated | Description$ x\n" +
		"SVar:DBPutP1P1:DB$ PutCounter | Defined$ ReplacedCard | CounterType$ P1P1 | ETB$ True | CounterNum$ 2\nOracle:x\n"
	e, cfg, id := kr1New(t, 250, src, kr1Creatures("Juno Target", "Other Body"), nil)
	addMana(t, e, 0, "B")
	tgt := kr1Put(t, e, 0, "Juno Target", state.ZGraveyard)
	other := kr1Put(t, e, 0, "Other Body", state.ZGraveyard)
	// The offer census now reads the sub's graveyard pool (CR 601.2c), so the
	// priority options cached before the moves must be rebuilt.
	e.pending = nil
	e.priorityRound()
	// CR 601.2c: the sub's target is announced with the cast -- a cast_sub
	// KTarget over the owned graveyard creatures -- and asked exactly once.
	castID := e.Pending()
	castIdx := -1
	for _, o := range castID.Options {
		if o.Kind == "cast" && o.Obj == id {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, castID.Options)
	}
	submitChoices(t, e, castIdx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("decision = %+v, want the cast-time target ask over both graveyard creatures", d)
	}
	submitChoices(t, e, kr1OptIndex(t, d, tgt))
	if p := kr1Settle(t, e); p != nil {
		t.Fatalf("the target was asked again after the cast announcement answered it: %+v", p)
	}
	o := e.G.Obj(tgt)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("targeted creature zone = %s, want battlefield", o.Zone)
	}
	if n := o.Counter("P1P1"); n != 2 {
		t.Fatalf("targeted creature +1/+1 counters = %d, want 2 (the Effect remembered the sub's target)", n)
	}
	if kr1Zone(e, other) != state.ZGraveyard {
		t.Fatal("the untargeted creature moved")
	}
	replayCheck(t, e, cfg)
}
