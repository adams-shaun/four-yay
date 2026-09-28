package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Engine-level pin for the Count$Valid <spec>$Colors distinct-colour count,
// on its live corpus carrier Shimmercreep ("Vivid — When this creature
// enters, each opponent loses X life and you gain X life, where X is the
// number of colors among permanents you control"). The count is a
// DISTINCT-set read (max 5), not a sum over permanents: the board fixtures
// below separate the two readings.

// colorsBoardPut moves one copy of the named corpus card from hand/library
// onto seat 0's battlefield (the commanderCharmCast shape: a raw MoveZone
// event, then a re-asked priority round). The fixtures are vanilla bodies
// with no ETB triggers, so the board stays quiet.
func colorsBoardPut(t *testing.T, e *Engine, name string) {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				e.pending = nil
				e.priorityRound()
				return
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from hand/library", name)
}

// colorsBoardEngine starts a seat-0 game whose deck is Shimmercreep plus the
// named board fixtures padded with Mountains, and lays the fixtures onto the
// battlefield before the cast.
func colorsBoardEngine(t *testing.T, reg *cards.Registry, board ...string) (*Engine, Config) {
	t.Helper()
	deck := []*cards.Card{searchCorpusCard(t, reg, "Shimmercreep")}
	for _, name := range board {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	mountain := searchCorpusCard(t, reg, "Mountain")
	for len(deck) < 40 {
		deck = append(deck, mountain)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 4210, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	for _, name := range board {
		colorsBoardPut(t, e, name)
	}
	return e, cfg
}

// castShimmercreep resolves one Shimmercreep cast end to end (cast, the ETB
// Vivid trigger, and its LoseLife/GainLife chain). The spell has no targets
// and no mid-cast ask, so after the cast submit the drain runs straight
// through resolution and the trigger.
func castShimmercreep(t *testing.T, e *Engine, cfg Config) {
	t.Helper()
	id := searchMoveByName(t, e, "Shimmercreep", state.ZHand)
	addMana(t, e, 0, "CCCCB")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	if d = e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("unexpected target ask: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Shimmercreep = %+v, want on the battlefield", o)
	}
	replayCheck(t, e, cfg)
}

func TestShimmercreepVividColorsLifeSwing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	// Four colours on the board (W Savannah Lions, U Zephyr Sprite, G
	// Grizzly Bears, R Hill Giant) plus Shimmercreep's own black: X = 5.
	t.Run("four board colours plus its own black", func(t *testing.T) {
		e, cfg := colorsBoardEngine(t, reg, "Savannah Lions", "Zephyr Sprite", "Grizzly Bears", "Hill Giant")
		castShimmercreep(t, e, cfg)
		if got := e.G.Players[1].Life; got != 15 {
			t.Errorf("opponent life = %d, want 15 (loses W,U,G,R+B = 5)", got)
		}
		if got := e.G.Players[0].Life; got != 25 {
			t.Errorf("caster life = %d, want 25 (gains 5)", got)
		}
	})

	// Three identical green bears: the count is DISTINCT colours
	// (G + Shimmercreep's B = 2), not a sum over the four permanents
	// (which would be 4) — X = 2.
	t.Run("distinct colours, not a sum over permanents", func(t *testing.T) {
		e, cfg := colorsBoardEngine(t, reg, "Grizzly Bears", "Grizzly Bears", "Grizzly Bears")
		castShimmercreep(t, e, cfg)
		if got := e.G.Players[1].Life; got != 18 {
			t.Errorf("opponent life = %d, want 18 (loses G+B = 2, not 4)", got)
		}
		if got := e.G.Players[0].Life; got != 22 {
			t.Errorf("caster life = %d, want 22 (gains 2)", got)
		}
	})
}
