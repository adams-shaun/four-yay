package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

type rememberedTargetHost struct{ Host }

func TestEffectRememberedTargetPrefetchesChangeZoneSub(t *testing.T) {
	const subLine = "DB$ ChangeZone | ValidTgts$ Creature.YouOwn | Origin$ Graveyard | Destination$ Battlefield"
	c := &Ctx{
		SVars:  map[string]string{"DBReturn": subLine},
		Choice: []state.Target{{Obj: 41}}, ChoiceDone: true,
	}
	effect := &cards.SA{
		Kind: "DB", API: "Effect", Line: "DB$ Effect | RememberObjects$ Targeted & Self | ReplacementEffects$ ETBCreat | SubAbility$ DBReturn",
		Params: map[string]string{"RememberObjects": "Targeted & Self", "SubAbility": "DBReturn"},
	}

	picked, remembered, suspended := prefetchRememberedChangeZoneTarget(rememberedTargetHost{}, c, effect)
	if !remembered || suspended {
		t.Fatalf("prefetch = (remembered %t, suspended %t), want captured answer", remembered, suspended)
	}
	if len(picked) != 1 || picked[0].Obj != 41 {
		t.Fatalf("picked targets = %+v, want object 41", picked)
	}
	child := cards.ResolveSVar(c.SVars, "DBReturn")
	if child == nil || child.API != "ChangeZone" || child.Params["ValidTgts"] == "" {
		t.Fatalf("inline sub = %+v, want targeted ChangeZone", child)
	}
	if got := c.SubPreAsk[child.Line]; len(got) != 1 || got[0].Obj != 41 {
		t.Fatalf("saved sub answer = %+v, want object 41", got)
	}
}
