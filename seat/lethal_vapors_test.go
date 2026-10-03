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
func lethalVaporsFixture(t *testing.T) (*rules.Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	vapors, ok := reg.Lookup("Lethal Vapors")
	if !ok {
		t.Fatal(`corpus card "Lethal Vapors" not found`)
	}
	swamp, _ := reg.Lookup("Swamp")
	plains, _ := reg.Lookup("Plains")
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

// TestBotStopsActivatingAnOpponentsLethalVapors is the cardfuzz livelock
// (seed 8880833984888918124, sig "livelock ... ability_push ... {Lethal
// Vapors}"): the production bot held priority on its opponent's Lethal
// Vapors and activated the free "any player may activate" ability forever.
// The rules allow every activation (CR 602.2, CR 117.3c: the activator
// receives priority again; CR 732.2a: a one-player optional loop is ended by
// that player choosing how many times to repeat it), so the engine keeps
// offering it and the loop is the POLICY's to end. A5's per-source budget
// should have ended it, but both Board adapters zeroed Card.Activated on
// every permanent the deciding seat does not control, so a foreign source's
// activations never counted. Both adapter halves are driven here.
//
// The test also pins the engine half: once the activations resolve,
// Lethal Vapors is destroyed and the ACTIVATOR (the ability's controller,
// "you"), not the Vapors' controller, has one skipped turn per activation.
func TestBotStopsActivatingAnOpponentsLethalVapors(t *testing.T) {
	for _, half := range []string{"view", "game"} {
		t.Run(half, func(t *testing.T) {
			e, vid := lethalVaporsFixture(t)
			bot := NewBot(7)
			activations, intents := 0, 0
			for !e.G.Over && intents < 2000 {
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
							activations++
							if activations > 4 {
								t.Fatalf("the bot activated its opponent's Lethal Vapors %d times in one turn (A5's per-source budget is 4)", activations)
							}
						}
					}
				} else {
					in = decision.Intent{Seq: d.Seq, Player: d.Player}
					for _, o := range d.Options {
						if o.Kind == "pass" {
							in.Choices = []int{o.Index}
						}
					}
				}
				if err := e.Submit(in); err != nil {
					t.Fatalf("intent %d: %v", intents, err)
				}
			}
			if activations == 0 {
				t.Fatal("the bot never activated Lethal Vapors: the fixture no longer reaches the loop")
			}
			if activations > 4 {
				t.Fatalf("the bot activated its opponent's Lethal Vapors %d times in one turn (A5 budget is 4)", activations)
			}
			if z := e.G.Obj(vid).Zone; z != state.ZGraveyard {
				t.Fatalf("Lethal Vapors not destroyed (zone %v)", z)
			}
			skipped := e.G.SkipTurns[1]
			for _, ev := range e.L.Events {
				if ev.Kind == events.SkipTurn && ev.Player != 1 {
					t.Fatalf("SkipTurn charged to seat %d, want the activator (seat 1)", ev.Player)
				}
			}
			if e.G.SkipTurns[0] != 0 || skipped != activations {
				t.Fatalf("SkipTurns = %v after %d activations, want seat 1 (the activator) to skip one turn per activation", e.G.SkipTurns, activations)
			}
		})
	}
}
