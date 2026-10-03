package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins `api:Token`'s TokenRemembered$ parameter end to end on the
// real compiled Timothar, Baron of Bats script. Timothar's dies trigger is
//
//	SVar:TrigToken:AB$ Token | Cost$ 1 ExileAnyGrave<1/Card.TriggeredNewCard> |
//	  TokenRemembered$ ExiledCards | TokenScript$ b_1_1_bat_flying |
//	  ImprintTokens$ True | SubAbility$ DBAnimate
//	SVar:DBAnimate:DB$ Animate | Defined$ Imprinted | Duration$ Permanent | Triggers$ CDTrigger
//	SVar:CDTrigger:Mode$ DamageDone | ValidSource$ Card.Self | ValidTarget$ Player |
//	  CombatDamage$ True | Execute$ TrigSac | TriggerZones$ Battlefield
//	SVar:TrigSac:DB$ Sacrifice | SubAbility$ DBReturn
//	SVar:DBReturn:DB$ ChangeZone | Defined$ Remembered | Origin$ Exile |
//	  Destination$ Battlefield | Tapped$ True | GainControl$ True
//
// so the whole card depends on TokenRemembered$ binding the exiled Vampire to
// the CREATED Bat: without that read the Bat's granted return trigger resolves
// `Defined$ Remembered` against an empty list and the exiled card is stranded.

// tokenRememberedCard remedies the one rider that is measured into the target
// object: the Bat's Remembered list (set by the TokenRemembered$ Choose
// "remembered" event) is where the granted return trigger's `Defined$
// Remembered` resolves.
func tokenRememberedCards(e *Engine, id state.ObjID, ids ...state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	want := map[state.ObjID]bool{}
	for _, w := range ids {
		want[w] = true
	}
	seen := map[state.ObjID]bool{}
	for _, r := range o.Remembered {
		if !r.IsPlayer {
			seen[r.Obj] = true
		}
	}
	for w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// tokenRememberedBoard is exileAnyGraveBoard's shape with the token scripts
// registered on the Config, so a Timothar token mint resolves and the whole
// board round-trips through replayFromLog (which rebuilds from cfg).
func tokenRememberedBoard(t *testing.T, reg *cards.Registry, top, bearer string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	topCard := mustCorpusCard(t, reg, top)
	bearerCard := mustCorpusCard(t, reg, bearer)
	cfg := Config{Seed: 12, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{topCard, bearerCard}, mountainDeck(t, 38)...),
			mountainDeck(t, 40),
		}}
	e := New(cfg)
	out := map[string]state.ObjID{}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Card == nil {
			continue
		}
		switch o.Card {
		case topCard:
			out[top] = o.ID
		case bearerCard:
			out[bearer] = o.ID
		}
	}
	if out[top] == 0 || out[bearer] == 0 {
		t.Fatalf("board cards not found: %+v", out)
	}
	for _, id := range []state.ObjID{out[top], out[bearer]} {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg, out
}

// driveToAttackersAt drives the game (passing priority, declaring no attackers
// at every declare-attackers step) until it reaches turn `turn` seat `active`
// at a declare-attackers decision -- the pre-attack stopping point a fixture
// attacking on a LATER turn than the current one needs, so no direct
// SummonSick mutation (which replayCheck would flag) is used.
func driveToAttackersAt(t *testing.T, e *Engine, turn int32, active state.PlayerID) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Over {
			t.Fatalf("game ended before turn %d seat %d", turn, active)
		}
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == state.StepDeclareAttackers {
			if d := e.Pending(); d != nil && d.Kind == decision.KAttackers {
				return
			}
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			passOnce(t, e)
		case decision.KAttackers:
			submitNoAttackers(t, e)
		default:
			if len(d.Options) == 0 {
				t.Fatalf("empty non-priority decision %+v while driving to turn %d", d, turn)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		}
	}
	t.Fatalf("did not reach turn %d seat %d declare-attackers within the pass budget", turn, active)
}
