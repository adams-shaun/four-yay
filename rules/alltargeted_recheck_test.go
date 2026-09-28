package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Wayta's root fighter remains legal when the sub's Fight victim gains
// shroud in the response window. The chosen victim must not be used at
// resolution, even though it is still on the battlefield.
func TestWaytaCastSubTargetRecheckedAfterResponse(t *testing.T) {
	t.Parallel()
	waytaSrc := alltargetedCorpusText(t, "w/wayta_trainer_prodigy.txt")
	e, _, _ := newFixtureDeckWithOpponentCard(t, 86, waytaSrc, atBearSrc, atBearSrc)
	wayta := moveSeeded(t, e, 0, waytaSrc, state.ZBattlefield)
	fighter := putCreature(t, e, 0, atBearSrc)
	victim := putCreature(t, e, 1, atBearSrc)
	addMana(t, e, 0, "GGG")
	e.Advance()
	submitChoices(t, e, abilityOption(t, e, wayta, 0).Index)
	chooseObj := func(want state.ObjID, resume string) {
		t.Helper()
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget || d.ResumeKind != resume {
			t.Fatalf("target ask = %+v, want resume %q", d, resume)
		}
		for _, opt := range d.Options {
			if opt.Obj == want {
				submitChoices(t, e, opt.Index)
				return
			}
		}
		t.Fatalf("target %d not offered: %+v", want, d.Options)
	}
	chooseObj(fighter, "")
	chooseObj(victim, "cast_sub")
	if e.G.Obj(fighter).Zone != state.ZBattlefield || e.G.Obj(victim).Zone != state.ZBattlefield || len(e.G.Stack) != 1 {
		t.Fatalf("pre-response board: fighter=%s victim=%s stack=%v", e.G.Obj(fighter).Zone, e.G.Obj(victim).Zone, e.G.Stack)
	}
	// A response grants only the victim shroud. It remains a creature on the
	// battlefield; the root target stays legal. This registration models the
	// resolving response, not a new cast of Wayta's ability.
	e.AddContinuous(ContinuousEffect{Source: victim, Controller: 1, Affects: "Card.Self",
		Layer: LAbilities, AddKeywords: []string{"Shroud"}})
	if !e.HasKeyword(victim, "Shroud") || e.HasKeyword(fighter, "Shroud") {
		t.Fatalf("response did not isolate the victim: victim=%v fighter=%v",
			e.HasKeyword(victim, "Shroud"), e.HasKeyword(fighter, "Shroud"))
	}
	drainNoPostAsk(t, e)
	// A lethal fight moves both 2/2 Bears to the graveyard and clears their
	// Damage fields, so zone (not Damage alone) is the non-vacuous oracle.
	if e.G.Obj(fighter).Zone != state.ZBattlefield || e.G.Obj(victim).Zone != state.ZBattlefield ||
		e.G.Obj(fighter).Damage != 0 || e.G.Obj(victim).Damage != 0 {
		t.Fatalf("illegal sub target fought: fighter=%s damage=%d victim=%s damage=%d",
			e.G.Obj(fighter).Zone, e.G.Obj(fighter).Damage, e.G.Obj(victim).Zone, e.G.Obj(victim).Damage)
	}
}
