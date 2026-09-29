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

func TestGreatestPowerReadsDerivedPowerForComparisonSet(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Grizzly Bears", "Colossal Dreadmaw")
	bear := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	big := crAbortMove(t, e, 0, "Colossal Dreadmaw", state.ZBattlefield)
	e.emit(events.Event{Kind: events.ControlChange, Obj: bear, Player: 1})
	e.emit(events.Event{Kind: events.ControlChange, Obj: big, Player: 1})
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(big).Zone != state.ZBattlefield {
		t.Fatal("precondition: both comparison creatures must be on the battlefield")
	}
	if got := e.Power(bear); got != 2 {
		t.Fatalf("precondition: Grizzly Bears power = %d, want printed 2", got)
	}
	if got := e.Power(big); got != 6 {
		t.Fatalf("precondition: Colossal Dreadmaw power = %d, want printed 6", got)
	}
	ctx := &effects.Ctx{Source: big, Controller: 1, TriggerContext: effects.TriggerContext{TriggerCard: big}}
	const spec = "Creature.greatestPowerControlledByCardController"
	if got, ok := effects.EvalCountOK(e, ctx, "Count$ValidSelf "+spec); !ok || got != 1 {
		t.Fatalf("precondition: unpumped Dreadmaw ValidSelf count = %d, resolved=%v; want 1, true", got, ok)
	}
	e.AddContinuous(state.ContinuousEffect{Source: bear, Controller: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.Self", AddPower: 5, AddToughness: 5})
	bearPrinted, bigPrinted := e.G.Obj(bear).Face().Power(), e.G.Obj(big).Face().Power()
	if e.Power(bear) != 7 || e.Power(big) != 6 || bearPrinted >= bigPrinted {
		t.Fatalf("precondition: derived/printed ordering must flip: bear derived=%d printed=%d; Dreadmaw derived=%d printed=%d",
			e.Power(bear), bearPrinted, e.Power(big), bigPrinted)
	}
	if got, ok := effects.EvalCountOK(e, ctx, "Count$ValidSelf "+spec); !ok || got != 0 {
		t.Fatalf("pumped Dreadmaw ValidSelf count = %d, resolved=%v; want 0, true", got, ok)
	}
}

// TestMyrkulsEdictChoicePoolReadsDerivedPower drives Myrkul's Edict's
// SacTopPower chain with a REAL continuous pump on the SMALLER creature: the
// Choices$ pool must be sized at derived power, so the pumped 2/2 Grizzly
// Bears (derived 7) displaces the printed-greatest 3/3 Hill Giant and is the
// only creature offered -- before the fix the pool offered the Hill Giant,
// because choiceMatches sized the comparison at printed-plus-counter power.
func TestMyrkulsEdictChoicePoolReadsDerivedPower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	edict := mustCorpusCard(t, reg, "Myrkul's Edict")
	giantCard := mustCorpusCard(t, reg, "Hill Giant")
	bearCard := mustCorpusCard(t, reg, "Grizzly Bears")
	e := New(Config{Seed: 12, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(edict, 0)
	giant := e.G.AddObject(giantCard, 1)
	bear := e.G.AddObject(bearCard, 1)
	for _, o := range []*state.Object{src, giant, bear} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZBattlefield, To: state.ZStack})
	e.AddContinuous(state.ContinuousEffect{Source: bear.ID, Controller: 1, Layer: LPT, Sub: SubModify,
		Affects: "Creature.Self", AddPower: 5, AddToughness: 5})
	// Precondition: the pump really moved the derived read while the printed
	// faces still order the other way -- otherwise the pool assertion below
	// cannot distinguish derived from printed.
	if e.Power(bear.ID) != 7 || e.Power(giant.ID) != 3 {
		t.Fatalf("precondition: derived powers bear=%d giant=%d, want 7 and 3",
			e.Power(bear.ID), e.Power(giant.ID))
	}
	if bearPrinted, giantPrinted := e.G.Obj(bear.ID).Face().Power(), e.G.Obj(giant.ID).Face().Power(); bearPrinted >= giantPrinted {
		t.Fatalf("precondition: printed powers bear=%d giant=%d, want bear < giant",
			bearPrinted, giantPrinted)
	}
	sa := cards.ResolveSVar(src.Face().SVars, "SacTopPower")
	if sa == nil {
		t.Fatal("Myrkul's Edict has no SacTopPower SVar")
	}
	ctx := &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}
	e.contChain = e.contChain[:0]
	e.repeatReported = nil
	effects.Resolve(e, ctx, sa)
	if e.resume != nil {
		e.resume.outer = e.buildContinuationChain(e.contChain, src.ID, nil)
	}

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("chooser = seat %d, want the remembered player seat 1", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bear.ID {
		t.Fatalf("pumped greatest-power pool = %+v, want only the derived-greatest Grizzly Bears (%d)",
			d.Options, bear.ID)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 10)
	if z := e.G.Obj(bear.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen derived-greatest creature zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(giant.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("untouched Hill Giant zone = %s, want Battlefield", z)
	}
}
