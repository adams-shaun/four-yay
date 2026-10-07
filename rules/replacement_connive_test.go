package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestLeaderSuperGeniusReplacesConnive(t *testing.T) {
	t.Parallel()
	e, _ := conniveEngine(t, []string{"Leader, Super-Genius", "Lethal Scheme", "Grizzly Bears", "Iron Monger, Sadistic Tycoon", "Lightning Bolt", "Lightning Bolt"}, []string{"Grizzly Bears"})
	leader := conniveMoveTo(t, e, 0, "Leader, Super-Genius", state.ZBattlefield)
	conniver := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	ironMonger := conniveMoveTo(t, e, 0, "Iron Monger, Sadistic Tycoon", state.ZBattlefield)
	opponent := conniveMoveTo(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	// Ensure the connive has a known nonland discard and that both permanents
	// are in the zones the replacement and connive rules actually inspect.
	bolt := conniveMoveTo(t, e, 0, "Lightning Bolt", state.ZHand)
	if e.G.Obj(leader) == nil || e.G.Obj(leader).Zone != state.ZBattlefield ||
		e.G.Obj(conniver) == nil || e.G.Obj(conniver).Zone != state.ZBattlefield ||
		e.G.Obj(conniver).Controller != 0 || e.G.Obj(opponent).Zone != state.ZBattlefield {
		t.Fatal("replacement preconditions not met: Leader and conniver must be on battlefield under seat 0")
	}
	if e.G.Obj(bolt) == nil || e.G.Obj(bolt).Zone != state.ZHand || len(e.G.Zone(state.ZHand, 0)) == 0 {
		t.Fatal("known hand precondition not met")
	}
	beforeHand := len(e.G.Zone(state.ZHand, 0))
	beforeEvents := len(e.L.Events)
	castLethalSchemeAtBear(t, e, opponent, conniver)
	// Leader draws once, then its replacement body makes the creature connive
	// once (a second draw). Answer the real discard choice.
	if d := e.Pending(); d == nil || d.ResumeKind != "connive" {
		t.Fatalf("pending after replacement = %+v, want connive discard", d)
	}
	submitConniveDiscard(t, e, conniveCorpusCard(t, "Lightning Bolt"))
	passUntilStackEmpty(t, e, 40)

	draws, records := 0, 0
	for _, ev := range e.L.Events[beforeEvents:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
		if ev.Kind == events.Connive && ev.Obj == conniver {
			records++
		}
	}
	if draws != 2 {
		t.Fatalf("seat 0 draws during replaced connive = %d, want 2 (replacement draw + one connive draw)", draws)
	}
	if records != 1 {
		t.Fatalf("completed connive records for creature = %d, want exactly one", records)
	}
	if got := e.G.Obj(conniver).Counter("P1P1"); got != 1 {
		t.Fatalf("conniver +1/+1 counters = %d, want 1 from its one nonland discard", got)
	}
	if got := e.G.Obj(ironMonger).Counter("P1P1"); got != 1 {
		t.Fatalf("Iron Monger +1/+1 counters = %d, want 1 from trig:Connives on the completed record", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != beforeHand+1 {
		t.Fatalf("hand after replacement and connive = %d, want %d (one net replacement draw)", got, beforeHand+1)
	}
}

func TestLeaderSuperGeniusConniveCarrierPinned(t *testing.T) {
	leader := conniveCorpusCard(t, "Leader, Super-Genius")
	found := false
	for _, repl := range leader.Faces[0].Repls {
		if repl.EventKind() == cards.ReplConnive {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Leader, Super-Genius corpus face does not compile its R:Event$ Connive carrier")
	}
	count := 0
	for _, card := range searchTestRegistry(t).AllCards() {
		for _, face := range card.Faces {
			for _, repl := range face.Repls {
				if repl.EventKind() == cards.ReplConnive {
					count++
				}
			}
		}
	}
	if count != 1 {
		t.Fatalf("corpus Connive replacement carrier count = %d, want measured single carrier", count)
	}
}
