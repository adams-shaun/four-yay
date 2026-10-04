package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCascadeSelfExileTriggerEndsGrantAfterOneCastKernel pins the one-cast
// precision of TARDIS's Effect grant: its `Triggers$ ExileEffect` is read as
// the grant's cast-driven lifetime (ForgetOnCast Card.YouCtrl), so the next
// spell cascades and the one after it does not.
func TestCascadeSelfExileTriggerEndsGrantAfterOneCastKernel(t *testing.T) {
	e, _ := cascadeTestEngineFiller(t, 9224, "Night's Whisper", []string{"Forest", "Lightning Bolt"}, []string{"TARDIS", "The Tenth Doctor"}, "Night's Whisper")
	first := searchMoveByName(t, e, "Night's Whisper", state.ZHand)
	var second state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		o := e.G.Obj(id)
		if id != first && o != nil && o.Face() != nil && o.Face().Name == "Night's Whisper" {
			second = id
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
			break
		}
	}
	if second == 0 {
		t.Fatal("precondition: second Night's Whisper not in library")
	}
	var tardis state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "TARDIS" {
			tardis = id
		}
	}
	if tardis == 0 {
		t.Fatal("precondition: TARDIS is not on the battlefield")
	}
	// Resolve the real Attacks trigger's Effect body directly, bypassing only
	// the attack/IsPresent gates this slice cannot reach.
	face := e.G.Obj(tardis).Face()
	triggerIdx := -1
	for i, tr := range face.Triggers {
		if tr.Mode == "Attacks" {
			triggerIdx = i
		}
	}
	if triggerIdx < 0 || face.Triggers[triggerIdx].Effect == nil || face.Triggers[triggerIdx].Effect.API != "Effect" {
		t.Fatal("precondition: real TARDIS face has no Attacks Effect trigger")
	}
	sa := face.Triggers[triggerIdx].Effect
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: tardis, Controller: 0, SVars: face.SVars}
	}, sa)
	if d := e.Pending(); d != nil && d.ResumeKind == "planeswalk_optional" {
		submitChoices(t, e, d.Options[1].Index) // no planar deck in this engine
	}
	kr4Settle(e)
	passUntilStackEmpty(t, e, 40)

	grant, ok := findCascadeGrant(e)
	if !ok {
		t.Fatal("precondition: the AddKeyword$ Cascade grant is not registered")
	}
	if grant.ForgetOnCast != "Card.YouCtrl" {
		t.Fatalf("grant ForgetOnCast = %q, want the ExileEffect trigger's ValidCard$ Card.YouCtrl", grant.ForgetOnCast)
	}
	if before := cascadeTriggerPushes(e); before != 0 {
		t.Fatalf("precondition: %d cascade triggers before any probe cast, want 0", before)
	}

	castProbe := func(id state.ObjID) {
		t.Helper()
		addMana(t, e, 0, "BB")
		d := e.Pending()
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == id {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no cast option for probe %d: %+v", id, d.Options)
		}
		submitChoices(t, e, idx)
		d = passUntilNonPriority(t, e, 40)
		if d != nil && d.ResumeKind == "play" {
			_ = cascadeElection(t, e, "Lightning Bolt")
			submitChoices(t, e) // decline the free cast
		}
		passUntilStackEmpty(t, e, 40)
	}
	castProbe(first)
	if n := cascadeTriggerPushes(e); n != 1 {
		t.Fatalf("after the first probe %d cascade triggers, want 1 (the grant is live)", n)
	}
	castProbe(second)
	if n := cascadeTriggerPushes(e); n != 1 {
		t.Fatalf("after the second probe %d cascade triggers, want still 1 (the grant ended after one cast)", n)
	}
	if _, ok := findCascadeGrant(e); ok {
		t.Fatal("the Cascade grant is still registered after the qualifying cast")
	}
}
