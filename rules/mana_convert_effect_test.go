package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func manaConvertCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q missing", name)
	}
	return c
}

func TestEffectManaConvertReachesPayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{manaConvertCard(t, reg, "North Star"), manaConvertCard(t, reg, "Ancestral Recall")}, nil)
	star := moveByName(t, e, 0, "North Star", state.ZBattlefield)
	spell := moveByName(t, e, 0, "Ancestral Recall", state.ZHand)
	face := e.G.Obj(star).Face()
	if e.G.Obj(star).Zone != state.ZBattlefield || face == nil {
		t.Fatal("precondition: North Star is not on the battlefield with a face")
	}
	effect := face.Abilities[0]
	if effect.API != "Effect" || effect.Params["StaticAbilities"] != "Convert" {
		t.Fatalf("precondition: North Star effect changed: %+v", effect)
	}
	effects.Resolve(e, &effects.Ctx{Source: star, Controller: 0, SVars: face.SVars}, effect)
	if !strings.Contains(e.G.Obj(star).Face().SVars["Convert"], "ManaConversion$ AnyType->AnyType") {
		t.Fatal("precondition: North Star's real Convert SVar changed")
	}
	e.G.Players[0].Pool[state.MR] = 1
	if !pay.PayManaConvFor(asPayer(e), 0, spell, false, ParseCost("U"), asEval(e).Conv(0, spell, false)) {
		t.Fatal("Effect-delivered North Star conversion did not pay blue with red mana")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("converted payment left mana in the pool: %+v", e.G.Players[0].Pool)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "continuous effect ManaConvert unimplemented") {
			t.Fatalf("Effect-delivered ManaConvert fell through its unimplemented handler: %q", ev.Text)
		}
	}
}

func TestCommandManaConvertReachesPayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{manaConvertCard(t, reg, "Emissary's Ploy"), manaConvertCard(t, reg, "Grizzly Bears")}, nil)
	conspiracy := moveByName(t, e, 0, "Emissary's Ploy", state.ZCommand)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(conspiracy).Zone != state.ZCommand || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: command static or creature is in the wrong zone")
	}
	e.emit(events.Event{Kind: events.Choose, Obj: conspiracy, Counter: "number", Amount: 2})
	e.G.Players[0].Pool[state.MR] = 1
	if !pay.PayManaConvFor(asPayer(e), 0, bear, false, ParseCost("G"), asEval(e).Conv(0, bear, false)) {
		t.Fatal("command-zone ManaConvert did not pay green with red mana")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("command conversion left mana in the pool: %+v", e.G.Players[0].Pool)
	}
}

func TestOptionalManaConvertIsAskedBeforePayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{manaConvertCard(t, reg, "North Star"), manaConvertCard(t, reg, "Ancestral Recall")}, nil)
	star := moveByName(t, e, 0, "North Star", state.ZBattlefield)
	moveByName(t, e, 0, "Ancestral Recall", state.ZHand)
	face := e.G.Obj(star).Face()
	effects.Resolve(e, &effects.Ctx{Source: star, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	e.G.Players[0].Pool[state.MR] = 1
	e.priorityRound()
	if got := optionKinds(e.Pending())["cast"]; got != 1 {
		t.Fatalf("precondition: Ancestral Recall was not offered from red plus North Star: %+v", e.Pending())
	}
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Use optional mana conversion?" {
		t.Fatalf("North Star Optional$ did not pose a real election: %+v", d)
	}
	if len(d.Options) != 2 || d.Options[0].Index != 0 || d.Options[1].Index != 1 {
		t.Fatalf("optional conversion options = %+v", d.Options)
	}
}
