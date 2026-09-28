package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// led1Board seats the corpus card c and one plain Mountain on seat 0's
// battlefield at a main1 priority window — the fb-led1 shape: a mana source
// whose activation costs more than a bare tap beside a plain land, at the
// window the client's empty-priority-window floor decides about.
func led1Board(t *testing.T, c *cards.Card) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	cfg := Config{Seed: 11, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append([]*cards.Card{c}, mountainDeck(t, 30)...), mountainDeck(t, 30),
	}}
	e := New(cfg)
	var led, mtn state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Zone == state.ZBattlefield {
			continue
		}
		if o.Card == c && led == 0 {
			led = o.ID
		}
		if o.Card.Faces[0].Name == "Mountain" && mtn == 0 {
			mtn = o.ID
		}
	}
	if led == 0 || mtn == 0 {
		t.Fatalf("opening deal missing copies: led=%d mtn=%d", led, mtn)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: led, From: e.G.Obj(led).Zone, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: mtn, From: e.G.Obj(mtn).Zone, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg, led, mtn
}

// TestLed1CostlyManaActivationCarriesCostMarker pins the wire marker the
// fb-led1 fix adds: a priority window's "activate" option carries the
// activation cost whenever the offered mana ability costs more than a bare
// tap (Lion's Eye Diamond's {T}, Sacrifice), and carries none for a plain
// tap (a Mountain), so every existing land window serialises byte-identically.
// The marker is what lets the client's empty-priority-window floor stop on
// the LED window instead of auto-passing it away (fb-20260917T192520Z-26136705).
func TestLed1CostlyManaActivationCarriesCostMarker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c := mustCorpusCard(t, reg, "Lion's Eye Diamond")
	e, _, led, mtn := led1Board(t, c)
	acts := e.legalActions(0)
	var ledOpt, mtnOpt *decision.Option
	for i := range acts {
		o := &acts[i]
		if o.Kind != "activate" {
			continue
		}
		if o.Obj == led {
			ledOpt = o
		}
		if o.Obj == mtn {
			mtnOpt = o
		}
	}
	if ledOpt == nil {
		t.Fatal("legalActions did not offer Lion's Eye Diamond's mana activation")
	}
	if ledOpt.Cost == "" {
		t.Fatal("LED activate option carries no cost marker: the empty-priority-window floor swallows every window whose only action is this activation (fb-20260917T192520Z-26136705)")
	}
	if !strings.Contains(ledOpt.Cost, "Sac<1/") {
		t.Fatalf("LED cost marker %q does not name the sacrifice component", ledOpt.Cost)
	}
	if mtnOpt == nil {
		t.Fatal("legalActions did not offer the Mountain's mana activation")
	}
	if mtnOpt.Cost != "" {
		t.Fatalf("plain-tap Mountain activate option carries cost marker %q: every existing plain-land window must serialise unchanged", mtnOpt.Cost)
	}
}

// TestLed1PayLifeManaActivationCarriesCostMarker pins the same marker on the
// pay-life shape (Treasonous Ogre / Mana Confluence in the reporter's own
// deck): a cost whose only non-tap component is life is still more than a
// bare tap, so narrowing the marker to irreversible components would
// re-swear these windows.
func TestLed1PayLifeManaActivationCarriesCostMarker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Mana Confluence", "Treasonous Ogre"} {
		t.Run(name, func(t *testing.T) {
			c := mustCorpusCard(t, reg, name)
			e, _, src, _ := led1Board(t, c)
			found := false
			for _, o := range e.legalActions(0) {
				if o.Kind == "activate" && o.Obj == src {
					found = true
					if o.Cost == "" {
						t.Fatalf("%s activate option carries no cost marker though its cost is more than a bare tap", name)
					}
					if !strings.Contains(o.Cost, "PayLife<") {
						t.Fatalf("%s cost marker %q does not name the life component", name, o.Cost)
					}
				}
			}
			if !found {
				t.Fatalf("%s mana activation not offered", name)
			}
		})
	}
}
