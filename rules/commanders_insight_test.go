package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const commandersInsightTestCommander = `Name:Insight Commander
ManaCost:0
Types:Legendary Creature Wizard
PT:1/1
Oracle:x
`

func castTargetCommanderAndReturn(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("commander cast: pending = %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no command-zone cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZCommand})
	e.pending = nil
	e.Advance()
}

func TestCommandersInsightUsesTargetedPlayersCommanderCastCount(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	insight := searchCorpusCard(t, reg, "Commander's Insight")
	commander := card(t, commandersInsightTestCommander)
	deck0 := append([]*cards.Card{insight}, mountainDeck(t, 39)...)
	deck1 := append([]*cards.Card{commander}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 9401, Names: []string{"caster", "target"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens,
		Commanders: [][]int{{}, {0}}, StartingLife: 40, Format: FormatCommander})
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 2, 1, state.StepMain1)
	cmd := e.G.Players[1].Commanders[0]
	castTargetCommanderAndReturn(t, e, cmd)
	addMana(t, e, 1, "CC")
	castTargetCommanderAndReturn(t, e, cmd)
	if got := e.CommanderCastsFromCommandZone(1); got != 2 {
		t.Fatalf("target commander casts = %d, want 2", got)
	}
	if e.G.Obj(cmd).Zone != state.ZCommand {
		t.Fatalf("target commander zone = %s, want command (precondition)", e.G.Obj(cmd).Zone)
	}
	if got := e.CommanderCastsFromCommandZone(0); got == 2 {
		t.Fatalf("test precondition: caster count must differ from target count")
	}

	driveToStep(t, e, 3, 0, state.StepMain1)
	insightID := searchMoveByName(t, e, "Commander's Insight", state.ZHand)
	var d *decision.Decision
	addMana(t, e, 0, "UUUC") // announce X=1
	before := len(e.L.Events)
	d = e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == insightID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no Commander's Insight cast option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d = e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after casting Insight: pending = %+v, want X choice", d)
	}
	submitChoices(t, e, 1)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after announcing X: pending = %+v, want target choice", d)
	}
	targetIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			targetIdx = o.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("target player not offered: %+v", d.Options)
	}
	submitChoices(t, e, targetIdx)
	passUntilStackEmpty(t, e, 30)
	draws := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Draw && ev.Player == 1 {
			draws++
		}
	}
	if draws != 3 {
		t.Fatalf("target draws = %d, want X=1 plus the target's two commander casts", draws)
	}
	commanderReplayCheck(t, e, cfg)
}
