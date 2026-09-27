package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestManaColorAnswerUsesStructuredSymbol(t *testing.T) {
	const petal = "Name:Lotus Petal\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ Sac<1/CARDNAME> | Produced$ Any\nOracle:x\n"
	e, _, id := manaSourceEngine(t, petal)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
		t.Fatalf("precondition: Lotus Petal colour decision = %+v", d)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition: Lotus Petal zone = %s, want Graveyard after paying cost", got)
	}
	opt := d.Options[2]
	if opt.ManaSymbol != "B" || opt.Label == "presentation changed" {
		t.Fatalf("precondition: selected option = %+v, want distinct label and B symbol", opt)
	}
	d.Options[2].Label = "presentation changed"
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("pool after rewritten-label answer = %d black, want 1", got)
	}
}

func TestNestedManaColorAnswerUsesStructuredSymbol(t *testing.T) {
	e := handEngine(t)
	onBoard(t, e, 0, upkeepEnchantment)
	grotto := onBoard(t, e, 0, luckGrotto)
	if obj := e.G.Obj(grotto); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Luck Grotto = %+v, want battlefield", obj)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: grotto, Counter: "LUCK", Amount: 1})
	if got := e.G.Obj(grotto).Counter("LUCK"); got != 1 {
		t.Fatalf("precondition: luck counters = %d, want 1", got)
	}
	resolveUpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil || !strings.Contains(d.Prompt, "cumulative upkeep") {
		t.Fatalf("precondition: want cumulative mana window, got %+v", d)
	}
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == grotto }))
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
		t.Fatalf("nested mana colour decision = %+v", d)
	}
	opt := d.Options[1]
	if opt.ManaSymbol != "U" || opt.Label == "presentation changed" {
		t.Fatalf("precondition: prompt=%q selected option=%+v options=%+v, want distinct label and U symbol", d.Prompt, opt, d.Options)
	}
	d.Options[1].Label = "presentation changed"
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool[state.MU]; got != 1 {
		t.Fatalf("pool after nested colour answer = %d blue, want 1", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("mana handler did not run: %q", ev.Text)
		}
	}
}
