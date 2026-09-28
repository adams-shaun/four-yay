package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestMirrorImageCopy(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Mirror Image"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 0)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MU] = 1
	castMode(t, e, id, "")
	// The election is posed at the ENTRY boundary (CR 614.12), so the spell
	// must resolve first; the ask parks the move until it is answered.
	e.resolveTop()
	if d := e.Pending(); d == nil || d.ResumeKind != "etb" || len(d.Options) < 2 {
		t.Fatalf("expected Mirror Image copy choice, got %+v", d)
	} else {
		submitChoices(t, e, d.Options[0].Index)
	}
	if d := e.Derived(id); d.Power != 6 || d.Toughness != 6 {
		t.Fatalf("Mirror Image copy has %d/%d, want 6/6", d.Power, d.Toughness)
	}
}

func TestMalleableImpostorCopy(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Malleable Impostor"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 1)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 3
	e.G.Players[0].Pool[state.MU] = 1
	// Precondition: the template is a real 6/6 on the battlefield, distinct
	// from the impostor's own printed 0/0 -- the assertions below are about
	// the copy's characteristics, so a vacuous board would pass silently.
	bd := e.Derived(bear.ID)
	if bd.Power != 6 || bd.Toughness != 6 {
		t.Fatalf("template precondition: dreadmaw is %d/%d, want 6/6", bd.Power, bd.Toughness)
	}
	castMode(t, e, id, "")
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("expected ETB copy choice, got %+v", d)
	}
	var target, decline = -1, -1
	for _, o := range d.Options {
		if o.Obj == bear.ID {
			target = o.Index
		}
		if o.Kind == "clone" && o.Obj == 0 && o.Label == "Enter as itself" {
			decline = o.Index
		}
	}
	if target == -1 {
		t.Fatalf("copy target not offered: %+v", d.Options)
	}
	if decline == -1 {
		// The Optional carrier's decline ("you may have it enter as a copy")
		// must be on the offered list, or the "may" degrades to a mandatory
		// take.
		t.Fatalf("decline option not offered: %+v", d.Options)
	}
	submitChoices(t, e, target)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("impostor did not enter battlefield: %+v", o)
	}
	if d := e.Derived(id); d.Power != 6 || d.Toughness != 6 {
		t.Fatalf("copy has %d/%d, want 6/6", d.Power, d.Toughness)
	}
	if !hasEvent(e, events.ClonePermanent, id) {
		t.Fatal("copy did not emit ClonePermanent")
	}
	// The CR 707.9e exception riders: the copy is a Faerie Shapeshifter IN
	// ADDITION TO its other types (the copied Dreadmaw's own Dinosaur types
	// must survive) and has flying (which the Dreadmaw lacks, so this proves
	// the AddKeywords$ rider, not the copied card).
	der := e.Derived(id)
	for _, want := range []string{"Dinosaur", "Faerie", "Shapeshifter"} {
		if !slices.Contains(der.Types, want) {
			t.Fatalf("copy types = %v, want %s among them", der.Types, want)
		}
	}
	if !slices.Contains(der.Keywords, "Flying") {
		t.Fatalf("copy keywords = %v, want Flying (the AddKeywords$ rider)", der.Keywords)
	}
}

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
	e.resolveTop()
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
