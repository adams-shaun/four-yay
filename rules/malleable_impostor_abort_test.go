package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMalleableImpostorAbortLeavesCopyChoiceUntouched pins CR 733.1 for the
// ETB copy answer after the entry-boundary migration (cli-.../cc1e86f8): the
// election is posed by applyETBChoiceReplacement when the permanent would
// ENTER, so a proposal that never gets that far cannot record one, and an
// abort has nothing to undo. The property this pins is therefore structural
// -- "the game returns to the moment before the spell was proposed" -- and is
// asserted the way crAbortSites' spell_mana_after_choice does after the same
// migration: no entry choice may be pending during the proposal, and the
// object's recorded copy fields (including one a PRIOR entry recorded) must
// come out of the abort byte-for-byte as they went in, with no Choose event
// of the copy kind written by the proposal.
func TestMalleableImpostorAbortLeavesCopyChoiceUntouched(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		priorValid bool
	}{
		{name: "no_prior_election"},
		{name: "prior_template", priorValid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, corpusAlternativeCard(t, "Malleable Impostor"))
			prior := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 1)
			prior.Zone = state.ZBattlefield
			picked := e.G.AddObject(corpusAlternativeCard(t, "Craw Wurm"), 1)
			picked.Zone = state.ZBattlefield
			e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{prior.ID, picked.ID})
			id := e.G.Zone(state.ZHand, 0)[0]
			if e.G.Obj(id).ETBCloneChoiceValid || e.G.Players[0].Pool.Total() != 0 {
				t.Fatalf("fixture precondition: choice valid=%t mana=%d, want false/0", e.G.Obj(id).ETBCloneChoiceValid, e.G.Players[0].Pool.Total())
			}
			if tc.priorValid {
				e.emit(events.Event{Kind: events.Choose, Obj: id, Counter: "clone", IDs: []state.ObjID{prior.ID}})
			}
			beforeChoice, beforeValid := e.G.Obj(id).ETBCloneChoice, e.G.Obj(id).ETBCloneChoiceValid
			beforeEvents := len(e.L.Events)

			// Direct beginCast deliberately exercises the ordinary
			// insufficient-mana abort; the legal-action gate would withhold
			// this unaffordable cast before the proposal exists at all.
			castMode(t, e, id, "")
			if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "etb" {
				t.Fatalf("entry choice was posed during the proposal: %+v", d)
			}

			e.continueCast() // pushes, fails payment, and reaches the normal abortCast path.
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZHand {
				t.Fatalf("unpayable cast did not reverse to hand: %+v", o)
			}
			if o.ETBCloneChoiceValid != beforeValid || o.ETBCloneChoice != beforeChoice {
				t.Fatalf("abort choice = valid=%t choice=%d, want valid=%t choice=%d", o.ETBCloneChoiceValid, o.ETBCloneChoice, beforeValid, beforeChoice)
			}
			// No copy-kind Choose event may be written by the aborted
			// proposal in either direction: none is recorded, so none needs
			// reversing, and a reversal event would itself move the chain
			// head for a choiceless abort.
			for _, ev := range e.L.Events[beforeEvents:] {
				if ev.Kind == events.Choose && ev.Obj == id && ev.Counter == "clone" {
					t.Fatalf("aborted proposal wrote a copy Choose event: %+v", ev)
				}
			}
		})
	}
}
