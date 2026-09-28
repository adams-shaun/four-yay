package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestWheelOfSunAndMoonRestInPeaceReplacementOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		chosenCard string
		wantZone   state.Zone
	}{
		{name: "Wheel", chosenCard: "Wheel of Sun and Moon", wantZone: state.ZLibrary},
		{name: "Rest in Peace", chosenCard: "Rest in Peace", wantZone: state.ZExile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := crResolutionEngine(t, []string{"Rest in Peace", "Grizzly Bears"}, []string{"Wheel of Sun and Moon"})
			wheel := crAbortMove(t, e, 1, "Wheel of Sun and Moon", state.ZBattlefield)
			rip := crAbortMove(t, e, 0, "Rest in Peace", state.ZBattlefield)
			card := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
			wheelFilter, ripFilter := false, false
			for _, r := range e.G.Obj(wheel).Face().Repls {
				if r.Event == "Moved" && r.Params["Destination"] == "Graveyard" &&
					r.Params["ValidCard"] == "Card.!token+OwnedBy Player.EnchantedBy" &&
					r.With != nil && r.With.API == "ChangeZone" && r.With.Params["Destination"] == "Library" {
					wheelFilter = true
				}
			}
			for _, r := range e.G.Obj(rip).Face().Repls {
				if r.Event == "Moved" && r.Params["Destination"] == "Graveyard" &&
					r.Params["ValidCard"] == "Card" && r.With != nil && r.With.Params["Destination"] == "Exile" {
					ripFilter = true
				}
			}
			if !wheelFilter || !ripFilter || e.G.Obj(wheel).Zone != state.ZBattlefield || e.G.Obj(rip).Zone != state.ZBattlefield {
				t.Fatal("precondition: real battlefield Wheel and Rest in Peace replacements are absent")
			}
			e.emit(events.Event{Kind: events.Attach, Obj: wheel, Player: 0, Text: "attach to player"})
			if !e.G.Obj(wheel).HasAttachedPlayer || e.G.Obj(wheel).AttachedPlayer != 0 {
				t.Fatal("precondition: Wheel is not attached to card owner's seat zero")
			}
			if target := e.G.Obj(card); target.Owner != 0 || target.Controller != 0 || target.Zone != state.ZBattlefield {
				t.Fatalf("precondition: affected card owner/controller/zone = %d/%d/%s", target.Owner, target.Controller, target.Zone)
			}
			if e.G.Obj(wheel).Controller == e.G.Obj(card).Controller {
				t.Fatal("precondition: affected player must differ from Wheel's controller")
			}
			e.pending = nil
			e.emit(events.Event{Kind: events.MoveZone, Obj: card, From: state.ZBattlefield, To: state.ZGraveyard})
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 || e.G.Obj(card).Zone != state.ZBattlefield {
				t.Fatalf("both replacements must be offered to affected player before move; zone=%s pending=%+v", e.G.Obj(card).Zone, d)
			}
			choice := -1
			for _, o := range d.Options {
				if o.Label == "Apply "+tc.chosenCard+"'s replacement" {
					choice = o.Index
				}
			}
			if choice < 0 {
				t.Fatalf("replacement choice %q absent: %+v", tc.chosenCard, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
				t.Fatalf("answer replacement choice: %v", err)
			}
			if got := e.G.Obj(card).Zone; got != tc.wantZone {
				t.Fatalf("choosing %s put card in %s, want %s", tc.chosenCard, got, tc.wantZone)
			}
			if e.G.Obj(card).Zone == state.ZGraveyard {
				t.Fatal("replaced card entered graveyard")
			}
			if tc.wantZone == state.ZLibrary {
				library := e.G.Zone(state.ZLibrary, 0)
				if len(library) == 0 || library[len(library)-1] != card {
					t.Fatalf("Wheel choice did not put card on library bottom: %v", library)
				}
			}
		})
	}

	t.Run("token excluded from Wheel", func(t *testing.T) {
		e := crResolutionEngine(t, []string{"Rest in Peace", "Grizzly Bears"}, []string{"Wheel of Sun and Moon"})
		wheel := crAbortMove(t, e, 1, "Wheel of Sun and Moon", state.ZBattlefield)
		rip := crAbortMove(t, e, 0, "Rest in Peace", state.ZBattlefield)
		bear := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		e.emit(events.Event{Kind: events.Attach, Obj: wheel, Player: 0, Text: "attach to player"})
		tokenID := e.G.NextID
		e.emit(events.Event{Kind: events.CardToken, Obj: bear, Player: 0})
		token := e.G.Obj(tokenID)
		if token == nil || !token.IsToken || token.Zone != state.ZBattlefield ||
			!e.G.Obj(wheel).HasAttachedPlayer || e.G.Obj(rip).Zone != state.ZBattlefield {
			t.Fatal("precondition: token or real replacement fixture missing")
		}
		e.pending = nil
		e.emit(events.Event{Kind: events.MoveZone, Obj: tokenID, From: state.ZBattlefield, To: state.ZGraveyard})
		if token.Zone != state.ZExile || token.Zone == state.ZGraveyard || e.Pending() != nil {
			t.Fatalf("!token Wheel replacement should not compete; token zone=%s pending=%+v", token.Zone, e.Pending())
		}
	})
}
