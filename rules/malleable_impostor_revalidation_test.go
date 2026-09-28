package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMalleableImpostorRevalidatesTemplateAtEntry proves the ETB copy choice
// is only an announcement, not a frozen permission: the chosen creature can
// stop being a legal template between the moment the option list was built
// and the moment the replacement body consumes the answer, and the copy must
// not happen then.
//
// After the entry-boundary migration the ask is posed by
// applyETBChoiceReplacement at the parked MoveZone, so the window this pins is
// the one the ask itself holds open: the option list is built when the ask is
// posed, and the board can change (here: directly, as a response would) while
// the answer is outstanding. The stale option is still on the list and can
// still be chosen; effects' cloneETBTemplateLegal is what refuses it.
func TestMalleableImpostorRevalidatesTemplateAtEntry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		respond func(*Engine, state.ObjID)
		check   func(*testing.T, *Engine, state.ObjID)
	}{
		{
			name: "template_leaves_battlefield",
			respond: func(e *Engine, id state.ObjID) {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
			},
			check: func(t *testing.T, e *Engine, id state.ObjID) {
				t.Helper()
				if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
					t.Fatalf("template zone after response = %s, want graveyard", got)
				}
			},
		},
		{
			name: "template_changes_controller",
			respond: func(e *Engine, id state.ObjID) {
				e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 0})
			},
			check: func(t *testing.T, e *Engine, id state.ObjID) {
				t.Helper()
				if got := e.G.Obj(id).Controller; got != 0 {
					t.Fatalf("template controller after response = %d, want 0", got)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, corpusAlternativeCard(t, "Malleable Impostor"))
			template := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 1)
			template.Zone = state.ZBattlefield
			e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{template.ID})
			id := e.G.Zone(state.ZHand, 0)[0]
			e.G.Players[0].Pool[state.MC] = 3
			e.G.Players[0].Pool[state.MU] = 1

			// Precondition: this is a legal Creature.OppCtrl template when the
			// as-enters election is announced, so the later no-copy result must
			// come from replacement-time revalidation rather than setup.
			castMode(t, e, id, "")
			e.resolveTop()
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
				t.Fatalf("expected copy choice, got %+v", d)
			}
			choice := -1
			for _, opt := range d.Options {
				if opt.Kind == "clone" && opt.Obj == template.ID {
					choice = opt.Index
				}
			}
			if choice < 0 {
				t.Fatalf("opposing template absent from initial choices: %+v", d.Options)
			}
			// Invalidate the template while the entry ask is outstanding: the
			// option list was already built, so the stale template is still
			// offered and still answerable.
			tc.respond(e, template.ID)
			tc.check(t, e, template.ID)
			submitChoices(t, e, choice)
			if d := e.Pending(); d != nil && d.Kind == decision.KPriority && len(e.G.Stack) > 0 {
				passUntilStackEmpty(t, e, 60)
			}

			// The previously chosen template is no longer legal, so the
			// optional replacement enters as itself. Malleable is printed 0/0,
			// hence the SBA moves it to the graveyard; it must never copy the
			// stale object or fall back to an unregistered Clone API note.
			if hasEvent(e, events.ClonePermanent, id) {
				t.Fatal("invalidated template emitted ClonePermanent")
			}
			if hasNote(e, "unimplemented API Clone") {
				t.Fatal("invalidated template used the unimplemented Clone fallback")
			}
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZGraveyard || o.Face() == nil || o.Face().Name != "Malleable Impostor" {
				t.Fatalf("invalidated copy should enter as Malleable Impostor then die, got %+v", o)
			}
		})
	}
}
