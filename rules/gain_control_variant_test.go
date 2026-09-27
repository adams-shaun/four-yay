package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestAliciaMastersEndStepReturnsCreaturesToOwners is the end-to-end pin for
// GainControlVariant's owner-directed batch shape (Alicia Masters, Skilled
// Sculptor's "Sense the Good"): at seat 0's end step, every creature returns
// to its owner -- a creature owned by seat 1 but stolen by seat 0 comes back,
// a seat-1-owned non-creature stays put (AllValid$ Creature excludes it), and
// the whole match replays byte-identically from the log alone.
func TestAliciaMastersEndStepReturnsCreaturesToOwners(t *testing.T) {
	reg := searchTestRegistry(t)
	alicia := searchCorpusCard(t, reg, "Alicia Masters, Skilled Sculptor")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")

	deck0 := []*cards.Card{alicia, bear, bear}
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain, bear)
	}
	deck1 := []*cards.Card{bear, mountain}
	for len(deck1) < 40 {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 424242,
		Names: []string{"alicia", "victim"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	src := placeInDeck(t, e, 0, alicia, state.ZBattlefield)
	mine := placeInDeck(t, e, 0, bear, state.ZBattlefield)
	theirs := placeInDeck(t, e, 1, bear, state.ZBattlefield)
	nonCreature := placeInDeck(t, e, 1, mountain, state.ZBattlefield)
	for _, id := range []state.ObjID{src, mine, theirs, nonCreature} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on the battlefield", id)
		}
	}
	// Steal seat 1's bear and Mountain. Both now have controller != owner;
	// only the creature is a legal AllValid$ Creature subject.
	e.emit(events.Event{Kind: events.ControlChange, Obj: theirs, Player: 0})
	e.emit(events.Event{Kind: events.ControlChange, Obj: nonCreature, Player: 0})
	if e.G.Obj(theirs).Controller != 0 || e.G.Obj(nonCreature).Controller != 0 {
		t.Fatalf("precondition: theft failed: bear=%d mountain=%d",
			e.G.Obj(theirs).Controller, e.G.Obj(nonCreature).Controller)
	}
	if e.G.Obj(mine).Owner != 0 || e.G.Obj(mine).Controller != 0 {
		t.Fatalf("precondition: my bear owner/controller = %d/%d", e.G.Obj(mine).Owner, e.G.Obj(mine).Controller)
	}

	// Drive to seat 0's end step and let Alicia's trigger resolve.
	driveToSeat0EndStep(t, e)

	if got := e.G.Obj(theirs).Controller; got != 1 {
		t.Fatalf("stolen Grizzly Bears controller = %d, want 1 (its owner)", got)
	}
	if got := e.G.Obj(nonCreature).Controller; got != 0 {
		t.Fatalf("stolen Mountain controller = %d, want 0 (AllValid$ Creature must exclude it)", got)
	}
	if got := e.G.Obj(mine).Controller; got != 0 {
		t.Fatalf("own Grizzly Bears controller = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// driveToSeat0EndStep advances the match, answering priority with pass and
// empty combat declarations, until seat 0's end step is reached and the stack
// is empty -- long enough for Alicia Masters' end-step trigger to resolve.
func driveToSeat0EndStep(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 8000; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			continue
		}
		var choices []int
		switch d.Kind {
		case decision.KAttackers, decision.KBlockers:
			// empty declaration
		case decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					choices = []int{o.Index}
				}
			}
			if choices == nil {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
		default:
			if len(d.Options) == 0 {
				t.Fatalf("empty non-priority decision %+v", d)
			}
			choices = []int{d.Options[0].Index}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if e.G.Over {
			t.Fatalf("game ended before seat 0's end step")
		}
		if e.G.Turn == 1 && e.G.Active == 0 && e.G.Step == state.StepEnd && len(e.G.Stack) == 0 {
			return
		}
	}
	t.Fatal("did not reach seat 0's end step")
}

// TestGainControlVariantOwnerGrantOverridesOlderStealAtCleanup pins the
// layering the owner-directed hand-back depends on (CR 613.7): the variant
// must register a new control effect on a matching permanent even when that
// permanent is ALREADY under its owner's control, because an older steal can
// still be tracked and would retake it the moment a later temporary return
// expires. Sequence: an older PERMANENT steal to seat 0, a later EOT return
// that leaves seat 1 visibly in control, then Alicia Masters' owner hand-back
// while seat 1 already controls the creature. At cleanup the EOT return
// expires; without the newer owner grant the older permanent steal becomes
// the latest surviving effect and retakes the creature (controller 0).
func TestGainControlVariantOwnerGrantOverridesOlderStealAtCleanup(t *testing.T) {
	e := New(Config{Seed: 7331, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	alicia := e.G.AddObject(choiceCorpusCard(t, "Alicia Masters, Skilled Sculptor"), 0)
	victim := e.G.AddObject(card(t, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{alicia.ID, victim.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	if o := e.G.Obj(victim.ID); o == nil || o.Zone != state.ZBattlefield || o.Owner != 1 || o.Controller != 1 {
		t.Fatalf("precondition: victim owner/controller/zone = %d/%d/%d, want 1/1/battlefield", o.Owner, o.Controller, o.Zone)
	}
	// An older PERMANENT steal to seat 0.
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim.ID, Player: 0})
	e.RegisterControl(effects.ControlGrant{Obj: victim.ID, ObjStamp: e.G.Obj(victim.ID).Timestamp, Previous: 1, Controller: 0})
	// A later EOT return restores the visible controller to its owner while
	// the older permanent steal remains tracked.
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim.ID, Player: 1})
	e.RegisterControl(effects.ControlGrant{Obj: victim.ID, ObjStamp: e.G.Obj(victim.ID).Timestamp, Previous: 0, Controller: 1,
		Duration: effects.ControlDuration{EOT: true}})
	if got := e.G.Obj(victim.ID).Controller; got != 1 {
		t.Fatalf("precondition: EOT return left controller %d, want 1", got)
	}
	// The owner hand-back resolves while the creature already belongs to its
	// owner (the exact case the variant must not skip).
	sa := cards.ResolveSVar(alicia.Face().SVars, "TrigGainControl")
	if sa.API != "GainControlVariant" {
		t.Fatalf("carried SA is %s, want GainControlVariant", sa.API)
	}
	effects.Resolve(e, &effects.Ctx{Source: alicia.ID, Controller: 0, SVars: alicia.Face().SVars}, sa)
	if got := e.G.Obj(victim.ID).Controller; got != 1 {
		t.Fatalf("owner hand-back changed controller to %d, want 1", got)
	}
	// The EOT return expires here; the newer owner grant must keep control
	// with the owner instead of letting the older steal retake it.
	e.EndOfTurnCleanup()
	if got := e.G.Obj(victim.ID).Controller; got != 1 {
		t.Fatalf("after cleanup controller = %d, want 1 (older permanent steal retook the creature)", got)
	}
}
