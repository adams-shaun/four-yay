package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The printed Taps + Teamwork trigger reads the tap's cost provenance, not
// merely whether the tapped creature is involved in a Teamwork cast.
func TestAgentMariaHillTeamworkTapTrigger(t *testing.T) {
	reg := searchTestRegistry(t)
	mariaCard := searchCorpusCard(t, reg, "Agent Maria Hill")
	spellCard := searchCorpusCard(t, reg, "Go Nuts!")
	bearCard := searchCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := tokenReplGameSeats(t, 64271, []*cards.Card{mariaCard, spellCard, bearCard}, []*cards.Card{bearCard})
	maria := moveSeededCard(t, e, 0, mariaCard, state.ZBattlefield)
	bear := moveSeededCard(t, e, 0, bearCard, state.ZBattlefield)
	opponent := moveSeededCard(t, e, 1, bearCard, state.ZBattlefield)
	if o := e.G.Obj(opponent); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: Go Nuts! second Teamwork Charm mode needs an opponent creature: %+v", o)
	}
	spell := searchMoveByName(t, e, "Go Nuts!", state.ZHand)
	if o := e.G.Obj(maria); o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.Counter("P1P1") != 0 {
		t.Fatalf("precondition: Maria must be untapped, counterless, on battlefield: %+v", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: second Teamwork payer must be untapped on battlefield: %+v", o)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: real Teamwork spell must be in hand: %+v", o)
	}
	// An ordinary tap of exactly the same creature must not satisfy Teamwork$.
	e.emitTap(maria, 0, false)
	e.priorityRound() // flush queued triggers; a missing match must not hide in the queue
	if o := e.G.Obj(maria); o == nil || !o.Tapped || o.Counter("P1P1") != 0 || triggerPushesFor(e, maria) != 0 {
		t.Fatalf("ordinary tap fired Maria's Teamwork trigger: Maria battlefield=%v tapped=%v pushes=%d (want 0)",
			o != nil && o.Zone == state.ZBattlefield, o != nil && o.Tapped, triggerPushesFor(e, maria))
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && ev.Obj == maria && ev.Counter != "" {
			t.Fatalf("ordinary tap payload changed: %+v", ev)
		}
	}
	e.emit(events.Event{Kind: events.Untap, Obj: maria})
	if e.G.Obj(maria).Tapped {
		t.Fatal("precondition: Maria remained tapped before Teamwork election")
	}
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptMode(t, e.Pending().Options, spell, "teamworked").Index)
	d := teamworkAskOptions(t, e)
	submitChoices(t, e, teamworkOption(t, d, maria), teamworkOption(t, d, bear))
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || !o.TeamworkPaid {
		t.Fatalf("precondition: paid spell must be on stack: %+v", o)
	}
	if !e.G.Obj(maria).Tapped || !e.G.Obj(bear).Tapped {
		t.Fatalf("precondition: elected creatures did not tap: Maria=%v bear=%v", e.G.Obj(maria).Tapped, e.G.Obj(bear).Tapped)
	}
	if n := triggerPushesFor(e, maria); n != 1 {
		t.Fatalf("Maria Teamwork tap pushed %d triggers, want 1 (ordinary tap pushed 0)", n)
	}
	marked := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && ev.Counter == events.TapTeamworkCounter {
			if ev.Obj != maria && ev.Obj != bear {
				t.Fatalf("unexpected Teamwork tap: %+v", ev)
			}
			marked++
		}
	}
	if marked != 2 {
		t.Fatalf("replay-visible Teamwork taps=%d, want 2", marked)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && (ev.Obj == maria || ev.Obj == bear) && ev.Counter == events.TapTeamworkCounter && ev.Text != "tapped as a cost" {
			t.Fatalf("Teamwork tap changed ordinary cost text: %+v", ev)
		}
	}
	beforeHand := len(e.G.Zone(state.ZHand, 0))
	beforeCounters := e.G.Obj(maria).Counter("P1P1")
	if beforeCounters != 0 {
		t.Fatalf("precondition: Maria already has %d counters before trigger resolves", beforeCounters)
	}
	// The trigger sits above Go Nuts!; resolve ONLY the trigger, avoiding the
	// spell's own counter/fight effects when attributing Maria's counter/draw.
	for i := 0; i < 8 && e.G.Obj(maria).Counter("P1P1") == beforeCounters; i++ {
		passPriorityOnce(t, e)
	}
	if got := e.G.Obj(maria).Counter("P1P1"); got != beforeCounters+1 {
		t.Fatalf("Maria's printed trigger added %d counters, want 1", got-beforeCounters)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != beforeHand+1 {
		t.Fatalf("Maria's printed trigger drew %d cards, want 1", got-beforeHand)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("spell resolved before attributing Maria's trigger: %+v", o)
	}
	// Log-only folding must retain the trigger's counter and draw. ChosenModes
	// is live cast announcement state (not folded by replayFromLog), so compare
	// the durable trigger result rather than the entire mid-stack object.
	replayed := replayFromLog(t, cfg, e.L.Events)
	if got := replayed.Obj(maria); got == nil || got.Zone != state.ZBattlefield || got.Counter("P1P1") != beforeCounters+1 {
		t.Fatalf("replayed Maria trigger counter = %+v, want %d", got, beforeCounters+1)
	}
	if got := len(replayed.Zone(state.ZHand, 0)); got != beforeHand+1 {
		t.Fatalf("replayed Maria trigger draw left %d cards in hand, want %d", got, beforeHand+1)
	}
}
