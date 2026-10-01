package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests answer real engine decisions through the same BoardFromGame +
// Decide path used by the hosted bot. Corpus cards provide the named
// replacement, multikicker, and mutate cases; each asserts that the real ask
// and a non-fallback answer were reached.
func TestBotPolicyCorpusReplacementOrder(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	other := onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Creature | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
		"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
	fiery := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	target := onBoard(t, e, 1, "Name:Order target\nTypes:Creature\nPT:1/1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Order source\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(fiery).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("replacement sources are not both on the battlefield")
	}
	// The corpus source has printed mana value 6 while the authored competing
	// source has none; verify the policy's ranking facts actually differ.
	board := botpolicy.BoardFromGame(e.G, e, 1)
	if board.Cards.Get(fiery).CMC <= board.Cards.Get(other).CMC {
		t.Fatalf("replacement worth precondition: Fiery CMC %d, other CMC %d", board.Cards.Get(fiery).CMC, board.Cards.Get(other).CMC)
	}
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 1})
	e.damaging = 0
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) < 2 {
		t.Fatalf("pending = %+v, want a real competing KReplacement ask", d)
	}
	fieryIndex := -1
	for _, o := range d.Options {
		if o.Obj == fiery && o.Kind == "replacement" {
			fieryIndex = o.Index
		}
	}
	if fieryIndex < 0 || d.Options[0].Index == fieryIndex {
		t.Fatalf("precondition: Fiery must be a non-first replacement option: %+v", d.Options)
	}
	bot := newTestBot(101)
	in := bot.answer(e, d)
	if len(in.Choices) != 1 || in.Choices[0] != fieryIndex {
		t.Fatalf("bot replacement answer %v, want higher-worth corpus source option %d", in.Choices, fieryIndex)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("submit bot replacement choice: %v", err)
	}
}

func TestBotPolicyCorpusMultikickerPaysMaximum(t *testing.T) {
	t.Parallel()
	e, _, anthem := gateFixture(t, 911, "Marshal's Anthem", gateRaiderSrc, gateRaiderSrc)
	gateMoveFromLibrary(t, e, "Raider", state.ZGraveyard)
	gateMoveFromLibrary(t, e, "Raider", state.ZGraveyard)
	addMana(t, e, 0, "WWWWWWWW")
	cast := castOptMode(t, castOptions(t, e), anthem, "multikicked")
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 || d.Options[0].Kind != "multikick" {
		t.Fatalf("real corpus multikick ask = %+v", d)
	}
	if d.Options[0].Amount == d.Options[len(d.Options)-1].Amount {
		t.Fatalf("precondition: offered affordable multikick counts do not differ: %+v", d.Options)
	}
	bot := newTestBot(102)
	in := bot.answer(e, d)
	if len(in.Choices) != 1 || in.Choices[0] != d.Options[len(d.Options)-1].Index {
		t.Fatalf("bot multikick answer %v, want maximum option %d", in.Choices, d.Options[len(d.Options)-1].Index)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("submit bot multikick choice: %v", err)
	}
	if got := e.G.Obj(anthem).TimesKicked; got != 2 {
		t.Fatalf("Marshal's Anthem TimesKicked = %d, want 2 after bot paid the affordable kicks", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after bot's multikick payment = %d, want 0", got)
	}
}

func TestBotPolicyCorpusMutatePlacesUnder(t *testing.T) {
	reg := sharedCorpus(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	e, _ := tokenReplGame(t, 701, phoenix)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	cast := mutatedCastOption(t, e, phoenixID)
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 || d.Options[0].Kind != "mutate_place" {
		t.Fatalf("real Everquill Phoenix placement ask = %+v", d)
	}
	if d.Options[0].Amount == d.Options[1].Amount {
		t.Fatalf("precondition: top and under options do not differ: %+v", d.Options)
	}
	bot := newTestBot(103)
	in := bot.answer(e, d)
	under := -1
	for _, o := range d.Options {
		if o.Kind == "mutate_place" && o.Amount == 0 {
			under = o.Index
		}
	}
	if under < 0 || len(in.Choices) != 1 || in.Choices[0] != under {
		t.Fatalf("bot mutate answer %v, want Under option %d", in.Choices, under)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("submit bot mutate placement: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("mutate target ask = %+v", d)
	}
	submitChoices(t, e, indexOfObjOption(d, bear))
	mutateDrain(t, e, 40)
	pile := e.G.Obj(bear)
	if pile == nil || pile.Zone != state.ZBattlefield || pile.Face().Name != "Mutate Bear" || pile.TimesMutated != 1 {
		t.Fatalf("mutate pile = %+v, want Bear on top with Everquill Phoenix beneath", pile)
	}
}
