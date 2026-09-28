package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The compiled Shuttle's attack trigger must run the victim's declined
// sacrifice through a nested GainControl ask and its Tap/ChangeCombatants
// continuation, not merely resolve the GainControl helper in isolation.
func TestMidnightCrusaderShuttleVillainousChoiceGainControl(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	shuttle := mustCorpusCard(t, reg, "Midnight Crusader Shuttle")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := Config{Seed: 109, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{shuttle, bear}, mountainDeck(t, 38)...),
			append([]*cards.Card{bear, bear}, mountainDeck(t, 38)...),
		}}
	e := New(cfg)
	// Stage an active combat through events, without first posting a stale
	// priority decision from a different step.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	src := placeInDeck(t, e, 0, shuttle, state.ZBattlefield)
	own := placeInDeck(t, e, 0, bear, state.ZBattlefield)
	first := placeInDeck(t, e, 1, bear, state.ZBattlefield)
	second := placeInDeck(t, e, 1, bear, state.ZBattlefield)
	for _, id := range []state.ObjID{src, own, first, second} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on battlefield", id)
		}
	}
	if first == second || first == own || second == own || e.G.Obj(second).Controller == e.G.Obj(src).Controller {
		t.Fatal("precondition: distinct eligible victims and excluded controller creature required")
	}
	// Stage the combat in the replay log, then resolve the real queued
	// attack trigger (not a direct effect call).
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{src}})
	if !e.G.Obj(src).IsAttacking || e.G.Obj(src).Attacking != 1 {
		t.Fatalf("precondition: Shuttle is not attacking victim 1: %+v", e.G.Obj(src))
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("Shuttle attack trigger not queued")
	}
	e.resolveTop()
	modeSeen, pickSeen := false, false
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no pending decision while resolving Shuttle")
		}
		var answer int = -1
		switch d.Kind {
		case decision.KPriority:
			for _, opt := range d.Options {
				if opt.Kind == "pass" {
					answer = opt.Index
				}
			}
		case decision.KModes:
			if modeSeen || d.Player != 1 || d.ResumeKind != "villainous" || len(d.Options) != 2 {
				t.Fatalf("villainous choice = %+v, want victim seat 1 with two modes", d)
			}
			modeSeen = true
			// The second mode is DBGainControl, declining DBSacrifice.
			answer = d.Options[1].Index
		case decision.KChoose:
			if !modeSeen || pickSeen || d.Player != 0 || d.ResumeKind != "choice" ||
				d.Prompt != "Choose a creature that player controls" || len(d.Options) != 2 {
				t.Fatalf("control choice = %+v, want controller seat 0 choosing two victim creatures", d)
			}
			pickSeen = true
			if d.Options[0].Obj != first || d.Options[1].Obj != second || first == second {
				t.Fatalf("eligible ordered choices = %+v, want [%d %d], not own creature %d", d.Options, first, second, own)
			}
			answer = d.Options[1].Index
		default:
			t.Fatalf("unexpected Shuttle resolution decision %+v", d)
		}
		if answer < 0 {
			t.Fatalf("no legal answer for %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{answer}}); err != nil {
			t.Fatalf("submit %d: %v", answer, err)
		}
	}
	if len(e.G.Stack) != 0 || !modeSeen || !pickSeen {
		t.Fatalf("chain incomplete: stack=%v modes=%v pick=%v", e.G.Stack, modeSeen, pickSeen)
	}
	chosen := e.G.Obj(src).Chosen
	if len(chosen) != 1 || chosen[0].IsPlayer || chosen[0].Obj != second {
		t.Fatalf("source chosen-card binding = %+v, want answered creature %d", chosen, second)
	}
	if o := e.G.Obj(second); o.Controller != 0 || !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("answered victim = controller %d tapped %v attacking %v defender %d, want 0 true true 1", o.Controller, o.Tapped, o.IsAttacking, o.Attacking)
	}
	if o := e.G.Obj(first); o.Controller != 1 || o.IsAttacking || o.Tapped {
		t.Fatalf("unselected victim changed: controller %d attacking %v tapped %v", o.Controller, o.IsAttacking, o.Tapped)
	}
	if o := e.G.Obj(own); o.Controller != 0 || o.IsAttacking {
		t.Fatalf("own creature changed: %+v", o)
	}
	e.EndOfTurnCleanup()
	if e.G.Obj(second).Controller != 1 || e.G.Obj(first).Controller != 1 {
		t.Fatalf("control did not expire at EOT: selected=%d other=%d", e.G.Obj(second).Controller, e.G.Obj(first).Controller)
	}
	replayCheck(t, e, cfg)
}
