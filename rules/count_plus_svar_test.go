package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestTempleOfTheDragonQueenPlusSVarOperand exercises the real corpus
// replacement whose DragonPresence count adds the DragonControlled SVar. The
// three cases distinguish the remembered reveal from a Dragon already on the
// battlefield; a direct EvalCount test cannot catch a broken ETB path.
func TestTempleOfTheDragonQueenPlusSVarOperand(t *testing.T) {
	t.Parallel()
	// No reveal and no controlled Dragon: the land must enter tapped.
	e, _, temple := templeDragonQueenGame(t, 9401, false, false)
	playTempleLand(t, e, temple)
	resolveTempleChoices(t, e)
	assertTempleEntry(t, e, temple, true)

	// A revealed Dragon satisfies the condition when no Dragon is controlled.
	e, _, temple = templeDragonQueenGame(t, 9402, true, false)
	playTempleLand(t, e, temple)
	resolveTempleChoices(t, e)
	assertDragonWasRevealed(t, e)
	assertTempleEntry(t, e, temple, false)

	// A controlled Dragon satisfies the condition even without a reveal.
	e, _, temple = templeDragonQueenGame(t, 9403, false, true)
	playTempleLand(t, e, temple)
	resolveTempleChoices(t, e)
	assertTempleEntry(t, e, temple, false)

	// Revealed AND controlled: the oracle's OR -- either alone satisfies, so
	// the land still enters untapped (presence = 2, the gate is only EQ0).
	e, _, temple = templeDragonQueenGame(t, 9404, true, true)
	playTempleLand(t, e, temple)
	resolveTempleChoices(t, e)
	assertDragonWasRevealed(t, e)
	assertTempleEntry(t, e, temple, false)
}

func templeDragonQueenGame(t *testing.T, seed uint64, reveal, control bool) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	fixtures := []string{"Temple of the Dragon Queen"}
	if reveal || control {
		fixtures = append(fixtures, "Dragon Whelp")
	}
	if reveal && control {
		// The fourth case needs two Whelps: one on the battlefield to
		// control, one in hand to reveal.
		fixtures = append(fixtures, "Dragon Whelp")
	}
	e, cfg := searchEngine(t, reg, fixtures...)
	temple := searchMoveByName(t, e, "Temple of the Dragon Queen", state.ZHand)
	if control {
		// Move the controlled Whelp FIRST: searchMoveByName scans hand
		// before library, so in the revealed+controlled case the battlefield
		// move must not consume the Whelp the hand reveal needs.
		dragon := searchMoveByName(t, e, "Dragon Whelp", state.ZBattlefield)
		if e.G.Obj(dragon).Zone != state.ZBattlefield {
			t.Fatalf("controlled Dragon zone = %s, want battlefield", e.G.Obj(dragon).Zone)
		}
	}
	if reveal {
		searchMoveByName(t, e, "Dragon Whelp", state.ZHand)
	}
	return e, cfg, temple
}

func resolveTempleChoices(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		if len(d.Options) == 0 {
			t.Fatalf("Temple choice has no options: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("answer Temple choice: %v", err)
		}
	}
	passUntilStackEmpty(t, e, 30)
}

func playTempleLand(t *testing.T, e *Engine, temple state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority to play Temple, got %+v", d)
	}
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == temple {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
				t.Fatalf("play Temple: %v", err)
			}
			return
		}
	}
	t.Fatalf("no play_land option for Temple: %+v", d.Options)
}

func assertDragonWasRevealed(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		for _, id := range ev.IDs {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Dragon Whelp" {
				return
			}
		}
	}
	t.Fatal("Temple's real Reveal did not reveal a Dragon Whelp")
}

func assertTempleEntry(t *testing.T, e *Engine, temple state.ObjID, tapped bool) {
	t.Helper()
	o := e.G.Obj(temple)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Temple zone = %v, pending=%+v stack=%v, want battlefield", o, e.Pending(), e.G.Zone(state.ZStack, 0))
	}
	if o.Tapped != tapped {
		t.Fatalf("Temple entered tapped=%v, want %v", o.Tapped, tapped)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API Reveal" {
			t.Fatal("Temple reveal handler was not registered")
		}
	}
}
