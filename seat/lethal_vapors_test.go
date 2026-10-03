package seat

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// lethalVaporsFixture builds a two-seat engine with the real Lethal Vapors
// on seat 0's battlefield (a logged MoveZone, the event shape every "put
// onto the battlefield" effect logs). Its "{0}: Destroy Lethal Vapors. You
// skip your next turn. Any player may activate this ability." (Activator$
// Player) is offered to seat 1, who does not control the source.
func lethalVaporsFixture(t *testing.T, seat1Card string) (*rules.Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	vapors, ok := reg.Lookup("Lethal Vapors")
	if !ok {
		t.Fatal(`corpus card "Lethal Vapors" not found`)
	}
	swamp, _ := reg.Lookup("Swamp")
	plains, ok := reg.Lookup(seat1Card)
	if !ok {
		t.Fatalf("corpus card %q not found", seat1Card)
	}
	deck0 := []*cards.Card{vapors}
	deck1 := []*cards.Card{}
	for i := 0; i < 39; i++ {
		deck0 = append(deck0, swamp)
	}
	for i := 0; i < 40; i++ {
		deck1 = append(deck1, plains)
	}
	e := rules.New(rules.Config{Names: []string{"vapors", "other"}, Decks: [][]*cards.Card{deck0, deck1}, Seed: 3})
	var vid state.ObjID
	from := state.ZLibrary
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Card != nil && o.Face().Name == "Lethal Vapors" {
			vid, from = o.ID, o.Zone
			break
		}
	}
	if vid == 0 {
		t.Fatal("Lethal Vapors not found at genesis")
	}
	e.Emit(events.Event{Kind: events.MoveZone, Obj: vid, From: from, To: state.ZBattlefield})
	e.Advance()
	return e, vid
}

// driveLethalVapors plays the fixture with seat 1 on the production bot
// (through the view or the game Board adapter) and seat 0 passing, until
// every activation of seat 0's Lethal Vapors has resolved, the game ends, or
// the intent budget runs out, and returns how many times seat 1 activated it.
func driveLethalVapors(t *testing.T, e *rules.Engine, vid state.ObjID, half string, budget int) int {
	t.Helper()
	bot, other := NewBot(7), NewBot(11)
	activations, intents := 0, 0
	for !e.G.Over && intents < budget {
		if o := e.G.Obj(vid); o.Zone != state.ZBattlefield && len(e.G.Stack) == 0 {
			break // every activation has resolved
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		intents++
		var in decision.Intent
		if d.Player == 1 {
			var err error
			if half == "view" {
				in, err = bot.Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
			} else {
				in, err = bot.DecideBoard(context.Background(), botpolicy.BoardFromGame(e.G, e, d.Player), *d)
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.Kind == decision.KPriority {
				if ch := d.Chosen(in); len(ch) == 1 && ch[0].Kind == "ability" && ch[0].Obj == vid {
					if !ch[0].ForeignSource || ch[0].SelfSkipTurns != 1 {
						t.Fatalf("Lethal Vapors' option facts: ForeignSource %v SelfSkipTurns %d, want true and 1", ch[0].ForeignSource, ch[0].SelfSkipTurns)
					}
					activations++
				}
			}
		} else if d.Kind == decision.KPriority {
			in = decision.Intent{Seq: d.Seq, Player: d.Player}
			for _, o := range d.Options {
				if o.Kind == "pass" {
					in.Choices = []int{o.Index}
				}
			}
		} else {
			// Seat 0's non-priority asks (a cleanup discard) go to a bot.
			var err error
			if in, err = other.DecideBoard(context.Background(), botpolicy.BoardFromGame(e.G, e, d.Player), *d); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("intent %d: %v", intents, err)
		}
	}
	return activations
}

// TestBotActivatesAnOpponentsLethalVaporsAtMostOnce is the fuzz-found
// self-harm defect: the production bot activated its opponent's Lethal
// Vapors ("{0}: Destroy Lethal Vapors. You skip your next turn. Any player
// may activate this ability.") up to A5's four times, because a free {0}
// ranked as the cheapest ability and nothing priced the SkipTurn rider --
// every activation threw away a turn, and every one after the first gained
// nothing (the first resolution already destroys it). The seat holds seven
// Serra Angels the Vapors would destroy, so A6 (botpolicy.turnSkipWorthIt)
// allows exactly ONE activation, in its own main phase. Both Board adapter
// halves are driven.
//
// It also pins the engine half: Lethal Vapors is destroyed and the
// ACTIVATOR (the ability's controller, "you"), not the Vapors' controller,
// has one skipped turn per activation.
func TestBotActivatesAnOpponentsLethalVaporsAtMostOnce(t *testing.T) {
	for _, half := range []string{"view", "game"} {
		t.Run(half, func(t *testing.T) {
			e, vid := lethalVaporsFixture(t, "Serra Angel")
			activations := driveLethalVapors(t, e, vid, half, 2000)
			if activations != 1 {
				t.Fatalf("the bot activated its opponent's Lethal Vapors %d times, want exactly 1 (the first kills it; each repeat only skips a turn)", activations)
			}
			if z := e.G.Obj(vid).Zone; z != state.ZGraveyard {
				t.Fatalf("Lethal Vapors not destroyed (zone %v)", z)
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.SkipTurn && ev.Player != 1 {
					t.Fatalf("SkipTurn charged to seat %d, want the activator (seat 1)", ev.Player)
				}
			}
			if e.G.SkipTurns[0] != 0 || e.G.SkipTurns[1] != 1 {
				t.Fatalf("SkipTurns = %v after one activation, want seat 1 (the activator) to skip exactly one turn", e.G.SkipTurns)
			}
		})
	}
}

// TestBotNeverTradesATurnForNothingOnLethalVapors is A6's other half: a
// seat with no creature card for the Vapors to destroy (forty Plains) gains
// nothing from killing it, so the turn loss always outweighs it and the bot
// never activates it -- the cardfuzz livelock fixture (seed
// 8880833984888918124), where A5's four-per-turn budget alone still threw
// away four turns a turn.
func TestBotNeverTradesATurnForNothingOnLethalVapors(t *testing.T) {
	for _, half := range []string{"view", "game"} {
		t.Run(half, func(t *testing.T) {
			e, vid := lethalVaporsFixture(t, "Plains")
			if activations := driveLethalVapors(t, e, vid, half, 600); activations != 0 {
				t.Fatalf("the bot activated its opponent's Lethal Vapors %d times holding no creature, want 0", activations)
			}
			if e.G.SkipTurns[1] != 0 {
				t.Fatalf("SkipTurns = %v, want seat 1 never to skip a turn", e.G.SkipTurns)
			}
		})
	}
}
