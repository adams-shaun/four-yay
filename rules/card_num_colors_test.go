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

func TestJaredCarthalionMinus3UsesDerivedColorCount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		mustCorpusCard(t, reg, "Jared Carthalion"),
		mustCorpusCard(t, reg, "Leyline of the Guildpact"),
		mustCorpusCard(t, reg, "Grizzly Bears"),
	}, nil)
	jared := moveByName(t, e, 0, "Jared Carthalion", state.ZBattlefield)
	moveByName(t, e, 0, "Leyline of the Guildpact", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(jared).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: Jared and Grizzly Bears must be on the battlefield")
	}
	if got := effects.ColorsOf(e.G.Obj(bear)); got != "G" {
		t.Fatalf("precondition: Grizzly Bears face colours = %q, want G", got)
	}
	if got := e.Colors(bear); got != "WUBRG" {
		t.Fatalf("precondition: Grizzly Bears derived colours = %q, want WUBRG", got)
	}
	if got := e.G.Obj(jared).Counter("LOYALTY"); got != 5 {
		t.Fatalf("precondition: Jared loyalty = %d, want 5", got)
	}
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == jared && o.Ability == 1 {
			opt = o
			break
		}
	}
	if opt.Obj == 0 {
		t.Fatalf("Jared [-3] was not offered: %+v", e.legalActions(0))
	}
	e.beginActivation(0, opt)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 5 {
		t.Fatalf("Grizzly Bears counters = %d, want 5", got)
	}
	if got := e.G.Obj(jared).Counter("LOYALTY"); got != 2 {
		t.Fatalf("Jared loyalty = %d, want 2", got)
	}
}

func TestKnightOfNewAlaraUsesDerivedColorCount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		mustCorpusCard(t, reg, "Knight of New Alara"),
		mustCorpusCard(t, reg, "Leyline of the Guildpact"),
		mustCorpusCard(t, reg, "Qasali Pridemage"),
	}, nil)
	knight := moveByName(t, e, 0, "Knight of New Alara", state.ZBattlefield)
	moveByName(t, e, 0, "Leyline of the Guildpact", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Qasali Pridemage", state.ZBattlefield)
	if e.G.Obj(knight).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: Knight and Qasali Pridemage must be on the battlefield")
	}
	if effects.ColorsOf(e.G.Obj(bear)) != "WG" || e.Colors(bear) != "WUBRG" {
		t.Fatalf("precondition: creature face=%q derived=%q; want WG and WUBRG", effects.ColorsOf(e.G.Obj(bear)), e.Colors(bear))
	}
	if got := e.Power(bear); got != 7 {
		t.Fatalf("Qasali Pridemage power = %d, want 7 (+5/+5 from Knight)", got)
	}
	if got := e.Toughness(bear); got != 7 {
		t.Fatalf("Qasali Pridemage toughness = %d, want 7 (+5/+5 from Knight)", got)
	}
}

// TestAdversarialKnightCountsAffectedObjectNotSource is the unequal-colour
// regression for the AffectedX anchor: Knight of New Alara (two colours)
// pumps Rafiq of the Many (three colours) with NO Leyline on the table, so
// counting the static's grantor instead of the affected creature awards +2
// (power 5) rather than +3 (power 6). The Leyline test above cannot see the
// difference because it makes every permanent all five colours.
func TestAdversarialKnightCountsAffectedObjectNotSource(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		mustCorpusCard(t, reg, "Knight of New Alara"),
		mustCorpusCard(t, reg, "Rafiq of the Many"),
	}, nil)
	knight := moveByName(t, e, 0, "Knight of New Alara", state.ZBattlefield)
	rafiq := moveByName(t, e, 0, "Rafiq of the Many", state.ZBattlefield)
	if e.G.Obj(knight).Zone != state.ZBattlefield || e.G.Obj(rafiq).Zone != state.ZBattlefield {
		t.Fatal("precondition: Knight and Rafiq must be on the battlefield")
	}
	if got := effects.ColorsOf(e.G.Obj(knight)); got != "WG" {
		t.Fatalf("precondition: Knight face colours = %q, want WG", got)
	}
	if got := effects.ColorsOf(e.G.Obj(rafiq)); got != "WUG" {
		t.Fatalf("precondition: Rafiq face colours = %q, want WUG", got)
	}
	if got, want := e.Colors(knight), "WG"; got != want {
		t.Fatalf("precondition: Knight derived colours = %q, want %q", got, want)
	}
	if got, want := e.Colors(rafiq), "WUG"; got != want {
		t.Fatalf("precondition: Rafiq derived colours = %q, want %q", got, want)
	}
	if got := e.Power(knight); got != 2 {
		t.Fatalf("Knight power = %d, want 2 (its own pump excludes itself)", got)
	}
	if got := e.Power(rafiq); got != 6 {
		t.Fatalf("Rafiq power = %d, want 6 (base 3 + 3 for its own three colours, not the Knight's two)", got)
	}
	if got := e.Toughness(rafiq); got != 6 {
		t.Fatalf("Rafiq toughness = %d, want 6", got)
	}
}

// TestOrdinaryStaticPumpKeepsGrantorCountAnchor distinguishes AffectedX from
// another static SVar expression. Mace's X must count its own charge counters,
// rather than the equipped bear's counters.
func TestOrdinaryStaticPumpKeepsGrantorCountAnchor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		mustCorpusCard(t, reg, "Mace of the Valiant"),
		mustCorpusCard(t, reg, "Grizzly Bears"),
	}, nil)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	mace := moveByName(t, e, 0, "Mace of the Valiant", state.ZBattlefield)
	if e.G.Obj(mace).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: Mace and Grizzly Bears must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: mace, IDs: []state.ObjID{bear}})
	if got := e.G.Obj(mace).AttachedTo; got != bear {
		t.Fatalf("precondition: Mace attached to %d, want %d", got, bear)
	}
	if got := e.Power(bear); got != 2 {
		t.Fatalf("precondition: bare Grizzly Bears power = %d, want 2", got)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: mace, Counter: "CHARGE", Amount: 2})
	if got := e.G.Obj(mace).Counter("CHARGE"); got != 2 {
		t.Fatalf("precondition: Mace charge counters = %d, want 2", got)
	}
	if got := e.G.Obj(bear).Counter("CHARGE"); got != 0 {
		t.Fatalf("precondition: Grizzly Bears charge counters = %d, want 0", got)
	}
	if got := e.Power(bear); got != 4 {
		t.Fatalf("Grizzly Bears power = %d, want 4 (base 2 + Mace's two counters)", got)
	}
	if got := e.Toughness(bear); got != 4 {
		t.Fatalf("Grizzly Bears toughness = %d, want 4", got)
	}
}

func TestCardNumColorsOffBattlefieldUsesFace(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		mustCorpusCard(t, reg, "Leyline of the Guildpact"),
		mustCorpusCard(t, reg, "Grizzly Bears"),
	}, nil)
	moveByName(t, e, 0, "Leyline of the Guildpact", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZHand)
	if e.G.Obj(bear).Zone != state.ZHand {
		t.Fatalf("precondition: bear zone = %s, want hand", e.G.Obj(bear).Zone)
	}
	if effects.ColorsOf(e.G.Obj(bear)) != "G" {
		t.Fatalf("precondition: bear face colours = %q, want G", effects.ColorsOf(e.G.Obj(bear)))
	}
	ctx := &effects.Ctx{Source: bear, Controller: 0}
	if got := effects.EvalCount(e, ctx, "CardNumColors"); got != 1 {
		t.Fatalf("off-battlefield CardNumColors = %d, want 1", got)
	}
}
