package rules

// The Rakdos-params brief, gap 6: AddKeyword$ Escape granted by an Effect /
// continuous static, and the printed K:Escape family it generalises. Before
// this work no "Escape" machinery existed at all (grep Escape over rules/
// cards/ found nothing), so neither the four grant cards (Underworld Breach,
// The Master of Keys, Welcome to Jurassic Park, Stormcage Containment
// Facility) nor the 33 printed K:Escape cards could cast from the graveyard.
//
// CR 702.42a: escape is a cast from the graveyard for the escape cost --
// the card's mana cost (the granted text's CardManaCost placeholder) plus
// ExileFromGrave<N/Card.Other> parts -- with no post-resolution destination
// change (unlike flashback, the spell goes where it would otherwise go).
// The escape cast records FlagEscaped, which the "sacrifice it unless it
// escaped" ETB family reads through Card.Self+escaped
// (effects/conditions.go's ConditionNotPresent$ evaluator).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// graveCard seeds a real deck card straight into seat 0's graveyard.
func graveyardCard(t *testing.T, e *Engine, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZGraveyard})
	return o.ID
}

// answerTriggerOrder answers every pending trigger-order ask (Kroxa's entry
// queues its ETB discard and its "sacrifice unless it escaped" together) and
// drains each resolution through the ordinary stack loop.
func answerTriggerOrder(t *testing.T, e *Engine) {
	t.Helper()
	for n := 0; n < 6; n++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTriggerOrder {
			return
		}
		var picks []int
		for _, o := range d.Options {
			picks = append(picks, o.Index)
		}
		submitChoices(t, e, picks...)
		passUntilStackEmpty(t, e, 50)
	}
}

func TestUnderworldBreachGrantsEscapeAndTheEscapeCastResolves(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Underworld Breach"))
	breach := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: breach, From: state.ZHand, To: state.ZBattlefield})

	// The card to escape-cast ({1}{G} Bears) and three OTHER cards for the
	// ExileFromGrave<3/Card.Other> part. All four sit in seat 0's graveyard.
	bears := graveyardCard(t, e, "Name:Grave Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	fod1 := graveyardCard(t, e, "Name:Fodder One\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n")
	fod2 := graveyardCard(t, e, "Name:Fodder Two\nManaCost:2 U\nTypes:Creature Fish\nPT:1/1\nOracle:x\n")
	fod3 := graveyardCard(t, e, "Name:Fodder Three\nManaCost:G\nTypes:Creature Insect\nPT:1/1\nOracle:x\n")

	addMana(t, e, 0, "GG")
	d := e.Pending()
	esc := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears && o.Mode == "escape" {
			esc = o.Index
		}
	}
	if esc < 0 {
		t.Fatalf("escape cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, esc)
	// The exile ask: exactly the three fodder cards (Card.Other excludes the
	// cast card itself), Min=Max=3.
	de := e.Pending()
	if de == nil || de.Kind != decision.KChoose || len(de.Options) != 3 {
		t.Fatalf("exile cost ask missing or wrong: %+v", de)
	}
	for _, o := range de.Options {
		if o.Obj != fod1 && o.Obj != fod2 && o.Obj != fod3 {
			t.Fatalf("exile option %+v is not a fodder card", o)
		}
	}
	submitChoices(t, e, de.Options[0].Index, de.Options[1].Index, de.Options[2].Index)
	passUntilStackEmpty(t, e, 50)

	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("escape-cast Bears zone=%v, want battlefield", o)
	}
	if o := e.G.Obj(bears); o != nil && o.CastFlags&state.FlagEscaped == 0 {
		t.Fatalf("escape cast did not record FlagEscaped: %+v", o.CastFlags)
	}
	for _, id := range []state.ObjID{fod1, fod2, fod3} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("fodder %d zone=%v, want exile", id, o)
		}
	}
	// The {1}{G} escape mana was paid; nothing else was charged from the pool.
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("pool=%d after the escape payment, want 0", pool)
	}
	// The three exiles are the escape cost's provenance (ExiledWith tracks
	// them as exiled-by-source cards, which is what a Chrome Mox-style read
	// would consult; here it just says the moves happened through the cost).
	if o := e.G.Obj(breach); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Underworld Breach left the battlefield: %v", o)
	}
}

func TestKroxaSacrificesItselfWhenItDoesNotEscape(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Kroxa, Titan of Death's Hunger"))
	kroxa := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "BR")
	opt := castOptionFor(t, e, kroxa)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 50)
	answerTriggerOrder(t, e)
	if o := e.G.Obj(kroxa); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("unescaped Kroxa zone=%v, want graveyard (the ETB 'sacrifice it unless it escaped' ran)", o)
	}
}

func TestKroxaEscapeCastDoesNotSacrificeOnETB(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Kroxa, Titan of Death's Hunger"))
	kroxa := e.G.Zone(state.ZHand, 0)[0]
	// The escape cast is a GRAVEYARD walk: Kroxa moves there first.
	e.emit(events.Event{Kind: events.MoveZone, Obj: kroxa, From: state.ZHand, To: state.ZGraveyard})
	// Five OTHER cards in the graveyard: the printed escape cost's
	// ExileFromGrave<5/Card.Other> part.
	for _, src := range []string{
		"Name:Fod A\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n",
		"Name:Fod B\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n",
		"Name:Fod C\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n",
		"Name:Fod D\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n",
		"Name:Fod E\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n",
	} {
		graveyardCard(t, e, src)
	}
	addMana(t, e, 0, "BBRR")
	d := e.Pending()
	esc := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == kroxa && o.Mode == "escape" {
			esc = o.Index
		}
	}
	if esc < 0 {
		t.Fatalf("escape cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, esc)
	de := e.Pending()
	if de == nil || len(de.Options) != 5 {
		t.Fatalf("exile cost ask missing or wrong: %+v", de)
	}
	var picks []int
	for _, o := range de.Options {
		picks = append(picks, o.Index)
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 50)
	answerTriggerOrder(t, e)
	if o := e.G.Obj(kroxa); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("escaped Kroxa zone=%v, want battlefield (the ETB sacrifice must not run)", o)
	}
	if o := e.G.Obj(kroxa); o != nil && o.CastFlags&state.FlagEscaped == 0 {
		t.Fatalf("escaped Kroxa lost FlagEscaped: %+v", o.CastFlags)
	}
}

func TestEscapeNotOfferedWithoutTheGrant(t *testing.T) {
	t.Parallel()
	// Without a grant, a nonland card in the graveyard is never offered an
	// escape cast; the Underworld Breach static is what carries the keyword.
	e := handEngine(t, corpusAlternativeCard(t, "Underworld Breach"))
	bears := graveyardCard(t, e, "Name:Grave Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	graveyardCard(t, e, "Name:Fodder One\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n")
	graveyardCard(t, e, "Name:Fodder Two\nManaCost:2 U\nTypes:Creature Fish\nPT:1/1\nOracle:x\n")
	graveyardCard(t, e, "Name:Fodder Three\nManaCost:G\nTypes:Creature Insect\nPT:1/1\nOracle:x\n")
	addMana(t, e, 0, "G")
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears {
			t.Fatalf("escape cast offered without the Breach on the battlefield: %+v", o)
		}
	}
}
