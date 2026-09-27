package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSkirkProspectorOffersSacrificeChoiceWithExtraGoblin uses real corpus
// cards. A second Goblin must widen activation into a legal choice.
func TestSkirkProspectorOffersSacrificeChoiceWithExtraGoblin(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _, ids := realCardEngine(t, reg, 74, "Skirk Prospector", "Goblin Guide")
	prospector, guide := ids[0], ids[1]
	submitChoices(t, e, activateOption(t, e, prospector))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("Prospector decision = %+v, want one-of-two Goblin sacrifice choice", d)
	}
	for _, o := range d.Options {
		if o.Obj != prospector && o.Obj != guide {
			t.Fatalf("non-Goblin sacrifice option: %+v", o)
		}
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(d.Options[0].Obj).Zone != state.ZGraveyard {
		t.Fatalf("chosen Goblin was not sacrificed")
	}
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("Prospector pool = %+v, want one red", e.G.Players[0].Pool)
	}
}

// passIndex finds the pass option of a priority decision.
func passIndex(t *testing.T, d *decision.Decision) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d.Options)
	return -1
}

// TestSkirkProspectorPaysAWardFromItsPaymentWindow proves the sacrifice
// choice works inside a CR 702.21a ward payment window: casting Gut Shot at
// warded Winter, Misanthropic Guide with an empty pool opens the ward mana
// window, activating Prospector there asks which Goblin dies, and answering
// resumes the suspended ward payment so the spell is not countered.
func TestSkirkProspectorPaysAWardFromItsPaymentWindow(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var d0 []*cards.Card
	for _, name := range []string{"Skirk Prospector", "Goblin Guide", "Gut Shot"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %s missing", name)
		}
		d0 = append(d0, c)
	}
	winter, ok := reg.Lookup("Winter, Misanthropic Guide")
	if !ok {
		t.Fatal("corpus fixture: Winter, Misanthropic Guide missing")
	}
	cfg := seatZeroStart(Config{Seed: 77, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(d0, mountainDeck(t, 37)...), append([]*cards.Card{winter}, mountainDeck(t, 39)...)}})
	e := New(cfg)
	e.Advance()
	prospector := moveByName(t, e, 0, "Skirk Prospector", state.ZBattlefield)
	guide := moveByName(t, e, 0, "Goblin Guide", state.ZBattlefield)
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	winterID := moveByName(t, e, 1, "Winter, Misanthropic Guide", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	submitChoices(t, e, castOptionFor(t, e, moveByName(t, e, 0, "Gut Shot", state.ZHand)).Index)
	// The {R/P} pip offers both its red and Phyrexian-life faces while the
	// Mountain is untapped.  Pick life so the Mountain and Prospector remain
	// available for the later ward payment window this test exercises.
	pip := e.Pending()
	if pip == nil || pip.Kind != decision.KChoose {
		t.Fatalf("after cast = %+v, want Gut Shot's pay-the-symbol ask", pip)
	}
	life := -1
	for _, o := range pip.Options {
		if o.Kind == "pay_life" {
			life = o.Index
		}
	}
	if life < 0 {
		t.Fatalf("Gut Shot payment options = %+v, want a life face", pip.Options)
	}
	submitChoices(t, e, life)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after cast = %+v, want Gut Shot's target ask", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == winterID {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no target option for the warded Winter: %+v", d.Options)
	}
	submitChoices(t, e, tgt)

	// Pass priority until the ward trigger (CR 702.21a) reaches the ask: the
	// targeting is on the stack, so the ward ability goes on the stack the
	// next time a player would receive priority and resolves into the
	// pay-or-counter modes ask.
	for i := 0; i < 8; i++ {
		p := e.Pending()
		if p == nil {
			t.Fatal("no decision while waiting for the ward ask")
		}
		if p.Kind != decision.KPriority {
			break
		}
		submitChoices(t, e, passIndex(t, p))
	}

	// The ward pay ask goes to the targeting player; the pool is empty, so
	// answering Pay opens the CR 702.21a mana window.
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KModes || pay.Player != 0 {
		t.Fatalf("after targeting = %+v, want seat 0's ward pay ask", pay)
	}
	payIdx := -1
	for _, o := range pay.Options {
		if strings.HasPrefix(o.Label, "Pay") {
			payIdx = o.Index
		}
	}
	if payIdx < 0 {
		t.Fatalf("no Pay option on the ward ask: %+v", pay.Options)
	}
	submitChoices(t, e, payIdx)

	// Ward window: the Mountain first, then the Prospector, whose sacrifice
	// cost asks which Goblin dies.
	win := e.Pending()
	if win == nil || win.ResumeKind != "ward_mana" {
		t.Fatalf("after Pay = %+v, want the ward mana window", win)
	}
	submitChoices(t, e, activateOption(t, e, mountain))
	win = e.Pending()
	if win == nil || win.ResumeKind != "ward_mana" {
		t.Fatalf("after the Mountain = %+v, want the ward window reopened for the second mana", win)
	}
	submitChoices(t, e, activateOption(t, e, prospector))
	sac := e.Pending()
	if sac == nil || sac.Kind != decision.KChoose || sac.Min != 1 || sac.Max != 1 || len(sac.Options) != 2 {
		t.Fatalf("ward-window activation = %+v, want one-of-two Goblin sacrifice choice", sac)
	}
	for _, o := range sac.Options {
		if o.Obj != prospector && o.Obj != guide {
			t.Fatalf("non-Goblin sacrifice option: %+v", o)
		}
	}
	submitChoices(t, e, sac.Options[1].Index)

	// The window reopens with every source tapped; Done pays the floating
	// {C}{R} as the Ward {2}, so the spell survives to resolve.
	win = e.Pending()
	if win == nil || win.ResumeKind != "ward_mana" {
		t.Fatalf("after the sacrifice = %+v, want the ward window reopened with only Done", win)
	}
	done := -1
	for _, o := range win.Options {
		if o.Kind == "done" {
			done = o.Index
		}
	}
	if done < 0 {
		t.Fatalf("no Done option: %+v", win.Options)
	}
	submitChoices(t, e, done)

	// The suspended payment resumed: ward paid, spell resolves.
	if e.G.Obj(guide).Zone != state.ZGraveyard {
		t.Fatalf("chosen Goblin zone = %s, want sacrificed to graveyard", e.G.Obj(guide).Zone)
	}
	if po := e.G.Obj(prospector); po.Zone != state.ZBattlefield {
		t.Fatalf("Prospector zone = %s, want still on the battlefield after paying its own sacrifice cost", po.Zone)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("ward payer pool = %d, want 0 after the resumed ward payment", got)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(winterID); got.Zone != state.ZBattlefield || got.Damage != 1 {
		t.Fatalf("Gut Shot target = %s damage %d, want on the battlefield with the ward-paid damage", got.Zone, got.Damage)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Text == "countered by ward" {
			t.Fatal("the targeting spell was countered by ward; the resumed payment failed")
		}
	}
}

// TestSkirkProspectorBotChoosesLeastValuableGoblin proves the new activation
// ask is answered by the real bot policy with a legal choice.
func TestSkirkProspectorBotChoosesLeastValuableGoblin(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _, ids := realCardEngine(t, reg, 75, "Skirk Prospector", "Goblin Guide")
	prospector := ids[0]
	submitChoices(t, e, activateOption(t, e, prospector))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Prospector decision = %+v, want two choices", d)
	}
	intent := newTestBot(1).answer(e, d)
	if err := e.Submit(intent); err != nil {
		t.Fatalf("bot sacrifice answer rejected: %v", err)
	}
	if e.G.Obj(prospector).Zone != state.ZGraveyard {
		t.Fatalf("bot sacrificed %v, want least-valuable Prospector", e.G.Obj(prospector).Zone)
	}
}

// TestLionsEyeDiamondPaysItsWholeCost pins the CARDNAME-sacrifice shape on a
// real corpus card: Lion's Eye Diamond's mana ability costs
// "Sac<1/CARDNAME> Discard<0/Hand>", so the sacrifice has exactly ONE legal
// candidate and must never pose a choice, and EVERY other cost part -- here
// the hand discard -- must still be paid before the mana is produced. The
// direct-resolve subtest is the non-interactive caller (attack_cost.go's tap
// window takes the same route): gating the whole cost continuation on the
// interactive flag silently skipped the discard there.
func TestLionsEyeDiamondPaysItsWholeCost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	handOf := func(e *Engine) []state.ObjID {
		return append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	}
	// assertPaid checks the whole cost was paid and then answers the
	// Produced$ Any colour ask the paid ability opens, so the three mana are
	// observed in the pool rather than assumed.
	assertPaid := func(t *testing.T, e *Engine, led state.ObjID, hand []state.ObjID) {
		t.Helper()
		if len(hand) == 0 {
			t.Fatal("fixture: seat 0 holds no cards, so the discard cost is vacuous")
		}
		if z := e.G.Obj(led).Zone; z != state.ZGraveyard {
			t.Fatalf("Lion's Eye Diamond zone = %s, want sacrificed to the graveyard", z)
		}
		for _, id := range hand {
			if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
				t.Fatalf("hand card %d zone = %s, want discarded to the graveyard by the mana cost", id, z)
			}
		}
		if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
			t.Fatalf("hand size after the Discard<0/Hand> cost = %d, want 0", got)
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 || d.Options[0].Kind != "mana" {
			t.Fatalf("after the paid cost = %+v, want the Produced$ Any colour ask", d)
		}
		submitChoices(t, e, d.Options[3].Index)
		if got := e.G.Players[0].Pool[state.MR]; got != 3 {
			t.Fatalf("pool = %+v, want the ability's three red mana", e.G.Players[0].Pool)
		}
	}

	t.Run("direct resolve", func(t *testing.T) {
		e := layerEngine(t)
		e.Advance()
		led := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Lion's Eye Diamond"))
		hand := handOf(e)
		e.resolveManaAbility(0, led, e.availableManaAbilities(0, led)[0], false)
		assertPaid(t, e, led, hand)
	})

	t.Run("activated", func(t *testing.T) {
		e, _, ids := realCardEngine(t, reg, 76, "Lion's Eye Diamond")
		led := ids[0]
		hand := handOf(e)
		submitChoices(t, e, activateOption(t, e, led))
		if d := e.Pending(); d != nil {
			for _, o := range d.Options {
				if o.Kind == "sacrifice" {
					t.Fatalf("Sac<1/CARDNAME> posed a sacrifice choice: %+v", d.Options)
				}
			}
		}
		assertPaid(t, e, led, hand)
	})
}
