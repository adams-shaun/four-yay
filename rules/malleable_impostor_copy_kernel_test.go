package rules

// Kernel-era restorations of the malleable_impostor_copy_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMalleableImpostorDecline pins the decline arm of the Optional
// election: entering as itself means the printed 0/0 enters, the toughness
// SBA removes it, and the event stream shows both the self-entry move and
// the SBA move -- and no ClonePermanent, since no copy was made.
func TestMalleableImpostorDecline(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Malleable Impostor"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 3
	e.G.Players[0].Pool[state.MU] = 1
	castMode(t, e, id, "")
	kr6ResolveTop(e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("expected ETB copy choice, got %+v", d)
	}
	decline := -1
	for _, o := range d.Options {
		if o.Kind == "clone" && o.Obj == 0 && o.Label == "Enter as itself" {
			decline = o.Index
		}
	}
	if decline == -1 {
		t.Fatalf("decline option not offered: %+v", d.Options)
	}
	submitChoices(t, e, decline)
	kr6Settle(e)
	if d := e.Pending(); d != nil && d.Kind == decision.KPriority && len(e.G.Stack) > 0 {
		passUntilStackEmpty(t, e, 60)
	}
	if hasEvent(e, events.ClonePermanent, id) {
		t.Fatal("declined election emitted ClonePermanent")
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("declined 0/0 should be in the graveyard (SBA), got %+v", o)
	}
	entry, sba := false, false
	for _, ev := range e.L.Events {
		if ev.Kind != events.MoveZone || ev.Obj != id {
			continue
		}
		if ev.From == state.ZStack && ev.To == state.ZBattlefield {
			entry = true
		}
		if ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			sba = true
		}
	}
	if !entry || !sba {
		t.Fatalf("event stream missing the self-entry and/or SBA move: entry=%v sba=%v", entry, sba)
	}
}
