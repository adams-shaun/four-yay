package rules

// TriggeredSourceSAController on REAL corpus cards: the controller of the
// CAUSING spell or ability, never the triggered permanent's controller and
// never the resolving trigger's controller.
//
//   - Ashenmoor Liege (`.cards/cardsfolder/a/ashenmoor_liege.txt`): an
//     opponent's ability targeting the Liege makes THAT PLAYER lose 4 life
//     (`DB$ LoseLife | Defined$ TriggeredSourceSAController | LifeAmount$ 4`).
//   - Black Bolt, Inhuman King (`.cards/cardsfolder/b/black_bolt_inhuman_king.txt`):
//     Lethal Voice's destroy ask offers only "nonland permanent that player
//     controls" (`ValidTgts$ Permanent.nonLand+ControlledBy
//     TriggeredSourceSAController`) -- the two-token referent grammar -- and
//     the chosen target is still legal at resolution.
//
// Leyline of Combustion (the third real user, with the batch latch) is pinned
// in becomestargetonce_test.go.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAshenmoorLiegeTriggeredSourceSAController: seat 1 activates an ability
// targeting seat 0's Ashenmoor Liege; the Liege's trigger makes seat 1 (the
// ability's controller) lose 4 life, and seat 0 none.
func TestAshenmoorLiegeTriggeredSourceSAController(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	lcard := mustCorpusCard(t, reg, "Ashenmoor Liege")
	trig := corpusTriggerMode(t, lcard, "BecomesTarget")
	if trig.Params["ValidSource"] != "SpellAbility.OppCtrl" {
		t.Fatalf("corpus fixture drift: Ashenmoor Liege's ValidSource$ = %q, want SpellAbility.OppCtrl", trig.Params["ValidSource"])
	}

	// Seat 1 gets a synthetic {T} ability that can target any creature;
	// untapping the Liege changes nothing but its tapped state, so the only
	// life movement below is the trigger's payout.
	const tapperSrc = "Name:OneTap\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\n" +
		"A:AB$ Untap | Cost$ T | ValidTgts$ Creature | SpellDescription$ Untap target creature.\n" +
		"Oracle:x\n"
	e, ids := becomesTargetOnceBoard(t, reg, []string{"Ashenmoor Liege"}, nil)
	liege := ids["Ashenmoor Liege"]
	tapperID := onBoard(t, e, 1, tapperSrc)
	e.G.Obj(tapperID).SummonSick = false
	if o := e.G.Obj(liege); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ashenmoor Liege is not on the battlefield: %+v", o)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	if life0 != life1 {
		t.Fatalf("precondition: seats start at different life (%d vs %d)", life0, life1)
	}

	e.G.Active, e.G.Priority = 1, 1
	e.askPriority(1)
	opt := abilityOption(t, e, tapperID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("precondition: expected a target ask, got %+v", d)
	}
	submitChoices(t, e, targetOptionIdx(t, e, liege))
	if len(e.G.Stack) == 0 {
		t.Fatal("the targeting ability never reached the stack")
	}
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[1].Life; got != life1-4 {
		t.Fatalf("seat 1 (the targeting player) life = %d, want %d", got, life1-4)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 (the Liege's controller) life = %d, want unchanged %d", got, life0)
	}
}

// TestBlackBoltTriggeredSourceSAController: seat 1 activates an ability
// targeting seat 0's Black Bolt; Lethal Voice's destroy ask (posed to Black
// Bolt's controller) must offer the NONLAND permanents of the targeting
// player's seat only, and the chosen target must still be legal at
// resolution (it leaves the battlefield destroyed).
func TestBlackBoltTriggeredSourceSAController(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	lcard := mustCorpusCard(t, reg, "Black Bolt, Inhuman King")
	trig := corpusTriggerMode(t, lcard, "BecomesTarget")
	if trig.Params["ValidSource"] != "SpellAbility.OppCtrl" {
		t.Fatalf("corpus fixture drift: Black Bolt's ValidSource$ = %q, want SpellAbility.OppCtrl", trig.Params["ValidSource"])
	}

	// Seat 1's board: a {T} ability targeting a creature (to turn Black
	// Bolt's trigger), plus two nonland permanents the destroy ask may
	// choose between -- a creature and an artifact. Neither is a land, so
	// both are legal under Permanent.nonLand; what must be EXCLUDED is
	// Black Bolt himself (seat 0's board).
	const tapperSrc = "Name:OneTap\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\n" +
		"A:AB$ Untap | Cost$ T | ValidTgts$ Creature | SpellDescription$ Untap target creature.\n" +
		"Oracle:x\n"
	const idolSrc = "Name:Idol\nManaCost:2\nTypes:Artifact\nOracle:x\n"
	e, ids := becomesTargetOnceBoard(t, reg, []string{"Black Bolt, Inhuman King"}, nil)
	bolt := ids["Black Bolt, Inhuman King"]
	tapperID := onBoard(t, e, 1, tapperSrc)
	idolID := onBoard(t, e, 1, idolSrc)
	e.G.Obj(tapperID).SummonSick = false
	if o := e.G.Obj(bolt); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Black Bolt is not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(idolID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the seat-1 artifact is not on the battlefield: %+v", o)
	}

	e.G.Active, e.G.Priority = 1, 1
	e.askPriority(1)
	opt := abilityOption(t, e, tapperID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("precondition: expected a target ask, got %+v", d)
	}
	submitChoices(t, e, targetOptionIdx(t, e, bolt))

	// The destroy ask arrives while the trigger resolves: answer it by hand,
	// after asserting its shape.
	var offered []state.ObjID
	for i := 0; i < 40; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while waiting for the destroy ask")
		}
		if d.Kind != decision.KTarget {
			if d.Kind != decision.KPriority {
				t.Fatalf("unexpected %v decision waiting for the destroy ask: %+v", d.Kind, d)
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		for _, o := range d.Options {
			offered = append(offered, o.Obj)
		}
		break
	}
	if len(offered) == 0 {
		t.Fatal("the destroy ask offered no target: the ControlledBy TriggeredSourceSAController filter matched nothing")
	}
	for _, id := range offered {
		if id == bolt {
			t.Fatal("the destroy ask offered Black Bolt himself; the referent named the CAUSING seat, not the trigger's controller")
		}
		if o := e.G.Obj(id); o == nil || o.Controller != 1 {
			t.Fatalf("the destroy ask offered %d (controller %+v): only the targeting player's permanents are legal", id, o)
		}
	}
	found := false
	for _, id := range offered {
		if id == idolID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the destroy ask did not offer the seat-1 artifact %d: %+v", idolID, offered)
	}
	// Take the artifact and let everything resolve; the CR 608.2b recheck
	// must agree with the offer.
	d = e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == idolID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the artifact's option vanished: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit destroy target: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(idolID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the chosen target was not destroyed at resolution: %+v", o)
	}
	if o := e.G.Obj(bolt); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Black Bolt left the battlefield: %+v", o)
	}
	// The triggering ability resolved too (its target was untapped, a no-op),
	// so the whole sequence really ran to completion.
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == bolt {
			pushes++
		}
	}
	if pushes != 1 {
		t.Fatalf("Black Bolt's trigger fired %d times, want 1", pushes)
	}
}
