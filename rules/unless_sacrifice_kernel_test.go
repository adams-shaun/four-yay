package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Braids, Arisen Nightmare's two-seat optional sacrifice and Power Taint's
// EnchantedController unless payer, under the kernel.

func TestBraidsOpponentChoosesItsSacrificeKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 737)
	relic := onBoard(t, e, 0, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	braids := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Braids, Arisen Nightmare"))
	trinket := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Mind Stone"))
	hand := len(e.G.Zone(state.ZHand, 0))
	life := e.G.Players[1].Life

	e.emit(events.Event{Kind: events.TriggerPush, Obj: braids, Player: 0, Amount: 0})
	kr9ResolveTop(e)

	// Seat 0's optional sacrifice: give up the relic, keep Braids.
	answerSacrifice(t, e, "Relic")
	// Seat 1's optional sacrifice: the artifact that shares the relic's card
	// type. It sacrifices Catching Sphere.
	answerSacrifice(t, e, "Mind Stone")

	if e.G.Obj(braids).Zone != state.ZBattlefield {
		t.Fatalf("Braids zone = %v, want it kept", e.G.Obj(braids).Zone)
	}
	if e.G.Obj(relic).Zone != state.ZGraveyard || e.G.Obj(trinket).Zone != state.ZGraveyard {
		t.Fatalf("relic zone %v trinket zone %v, want both sacrificed",
			e.G.Obj(relic).Zone, e.G.Obj(trinket).Zone)
	}
	if got := e.G.Players[1].Life; got != life {
		t.Fatalf("opponent life = %d, want %d (they sacrificed, so no loss)", got, life)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand {
		t.Fatalf("Braids controller hand = %d, want %d (no draw when the opponent sacrificed)", got, hand)
	}
}

func TestBraidsOpponentDeclinesItsSacrificeKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 737)
	relic := onBoard(t, e, 0, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	braids := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Braids, Arisen Nightmare"))
	onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Mind Stone"))
	hand := len(e.G.Zone(state.ZHand, 0))
	life := e.G.Players[1].Life

	e.emit(events.Event{Kind: events.TriggerPush, Obj: braids, Player: 0, Amount: 0})
	kr9ResolveTop(e)

	answerSacrifice(t, e, "Relic")
	answerSacrifice(t, e, "")

	if e.G.Obj(relic).Zone != state.ZGraveyard {
		t.Fatalf("relic zone = %v, want the controller's sacrifice", e.G.Obj(relic).Zone)
	}
	if got := e.G.Players[1].Life; got != life-2 {
		t.Fatalf("opponent life = %d, want %d (they declined)", got, life-2)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("Braids controller hand = %d, want %d (the draw for the declined opponent)", got, hand+1)
	}
}

func TestPowerTaintUnlessPayerEnchantedControllerKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 737)
	taint := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Power Taint"))
	enchanted := onBoard(t, e, 1, "Name:Victim Enchantment\nTypes:Enchantment\nOracle:x\n")
	e.G.Obj(taint).AttachedTo = enchanted
	sa := cards.ResolveSVar(e.G.Obj(taint).Face().SVars, "TrigLoseLife")
	if sa == nil || sa.Params["UnlessPayer"] != "EnchantedController" || sa.Params["UnlessCost"] != "2" {
		t.Fatalf("Power Taint TrigLoseLife = %+v, want priceable EnchantedController unless", sa)
	}
	kr9Probe(e, func() {
		effects.Resolve(e, &effects.Ctx{Source: taint, Controller: 0,
			TriggerContext: effects.TriggerContext{TriggerPlayer: state.Target{Player: 1, IsPlayer: true}}}, sa)
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want Power Taint unless-pay decision", d)
	}
	if d.Player != 1 {
		t.Fatalf("Power Taint payer = seat %d, want enchanted controller seat 1", d.Player)
	}
}
