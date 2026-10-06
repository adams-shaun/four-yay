package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestGraveyardAddAbilityGrantOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card := lookup(t, reg, "Glitch Ghost Surveyor")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{card}, nil)
	id := moveByName(t, e, 0, "Glitch Ghost Surveyor", state.ZGraveyard)
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatal("precondition: Surveyor is not in p0's graveyard")
	}
	if e.G.Players[0].Speed != 0 {
		t.Fatalf("precondition: expected distinct initial speed, got %d", e.G.Players[0].Speed)
	}
	e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: maxSpeed})
	for i := 0; i < 3; i++ {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	}
	e.priorityRound()
	if e.G.Players[0].Speed != maxSpeed || e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatal("precondition: max-speed Surveyor must remain in p0's graveyard")
	}
	opt := findGrantedOption(t, e, id, "ABDraw")
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(id).Zone != state.ZExile {
		t.Fatalf("Surveyor zone after activation = %s, want exile", e.G.Obj(id).Zone)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand size after draw = %d, want %d", got, handBefore+1)
	}
	replayCheck(t, e, cfg)

	for _, name := range []string{"Goblin Surveyor", "Loxodon Surveyor", "Mutant Surveyor", "Leonin Surveyor"} {
		t.Run(name, func(t *testing.T) {
			card := lookup(t, reg, name)
			e, _ := corpusEngineCfg(t, reg, []*cards.Card{card}, nil)
			id := moveByName(t, e, 0, name, state.ZGraveyard)
			e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: maxSpeed})
			for i := 0; i < 3; i++ {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
			}
			e.priorityRound()
			if e.G.Obj(id).Zone != state.ZGraveyard || e.G.Players[0].Speed != maxSpeed {
				t.Fatal("precondition: Surveyor must be in graveyard at max speed")
			}
			findGrantedOption(t, e, id, "ABDraw")
		})
	}

	t.Run("below max speed is withheld", func(t *testing.T) {
		e, _ := corpusEngineCfg(t, reg, []*cards.Card{card}, nil)
		id := moveByName(t, e, 0, "Glitch Ghost Surveyor", state.ZGraveyard)
		e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: maxSpeed - 1})
		e.priorityRound()
		if e.G.Obj(id).Zone != state.ZGraveyard || e.G.Players[0].Speed >= maxSpeed {
			t.Fatal("precondition: Surveyor must be in graveyard below max speed")
		}
		if options := matchingGrantedOptions(e, id, "ABDraw"); len(options) != 0 {
			t.Fatalf("offered below max speed: %+v", options)
		}
	})

	t.Run("synthetic off-battlefield carrier", func(t *testing.T) {
		src := "Name:Grave Grant\nManaCost:1\nTypes:Creature Wizard\nPT:1/1\n" +
			"S:Mode$ Continuous | EffectZone$ Graveyard | AffectedZone$ Graveyard | Affected$ Card.Self | Condition$ MaxSpeed | AddAbility$ ABTest | Description$ x\n" +
			"SVar:ABTest:AB$ Draw | Cost$ ExileFromGrave<1/CARDNAME/this card> | NumCards$ 1 | Secondary$ True | ActivationZone$ Graveyard | SpellDescription$ Draw a card.\nOracle:x\n"
		e, cfg, id := newFixtureDeck(t, 91, src)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
		e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: maxSpeed})
		e.priorityRound()
		if e.G.Obj(id).Zone != state.ZGraveyard || e.G.Players[0].Speed < maxSpeed {
			t.Fatal("precondition: synthetic carrier must be in graveyard at max speed")
		}
		opt := findGrantedOption(t, e, id, "ABTest")
		handBefore := len(e.G.Zone(state.ZHand, 0))
		submitChoices(t, e, opt.Index)
		passUntilStackEmpty(t, e, 20)
		if e.G.Obj(id).Zone != state.ZExile || len(e.G.Zone(state.ZHand, 0)) != handBefore+1 {
			t.Fatalf("synthetic activation: zone=%s hand=%d want exile and %d", e.G.Obj(id).Zone, len(e.G.Zone(state.ZHand, 0)), handBefore+1)
		}
		replayCheck(t, e, cfg)
	})
}

func matchingGrantedOptions(e *Engine, id state.ObjID, svar string) []decision.Option {
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return nil
	}
	var out []decision.Option
	for _, opt := range d.Options {
		if opt.Kind == "granted" && opt.Obj == id && opt.SVar == svar {
			out = append(out, opt)
		}
	}
	return out
}

func findGrantedOption(t *testing.T, e *Engine, id state.ObjID, svar string) decision.Option {
	t.Helper()
	options := matchingGrantedOptions(e, id, svar)
	if len(options) != 1 {
		t.Fatalf("got %d granted options for obj %d SVar %s, want exactly one; pending=%+v", len(options), id, svar, e.Pending())
	}
	return options[0]
}
