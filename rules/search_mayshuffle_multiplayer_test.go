package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMultiPlayerSearchStopsAtMayShuffleConfirm uses Veteran Explorer's real
// death trigger. After seat 0 finds a basic land, the search must leave its
// may-shuffle confirm pending; it must not pose seat 1's search while that
// decision is outstanding (CR 701.23).
func TestMultiPlayerSearchStopsAtMayShuffleConfirm(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Veteran Explorer")
	veteran := searchMoveByName(t, e, "Veteran Explorer", state.ZHand)
	if o := e.G.Obj(veteran); o == nil {
		t.Fatalf("precondition: Veteran Explorer object is missing")
	} else if o.Zone != state.ZHand {
		t.Fatalf("precondition: Veteran Explorer zone = %v, want hand", o.Zone)
	}

	// Put the real carrier onto the battlefield, then kill it to enqueue its
	// ChangesZone trigger. The search libraries contain distinct basic lands;
	// this also proves the first search actually has a card to move.
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: veteran, From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(veteran); o == nil {
		t.Fatalf("precondition: Veteran Explorer object disappeared entering battlefield")
	} else if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Veteran Explorer zone = %v, want battlefield", o.Zone)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: veteran, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(veteran); o == nil {
		t.Fatalf("precondition: Veteran Explorer object disappeared entering graveyard")
	} else if o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: Veteran Explorer zone = %v, want graveyard", o.Zone)
	}

	e.Advance()
	// Veteran Explorer's TrigChange carries Optional$ True on its library
	// ChangeZone, so Forge's confirmAction gate runs before the fetch list is
	// consulted: the first non-priority decision is the search confirmation,
	// not the card pick. Accept it, then the real resolution reaches the
	// library search directly.
	confirm := passUntilNonPriority(t, e, 40)
	if confirm == nil || confirm.Kind != decision.KChoose || confirm.ResumeKind != "search_confirm" || confirm.Player != 0 {
		t.Fatalf("first library ask = %+v, want seat 0 search confirmation", confirm)
	}
	if len(confirm.Options) != 2 || confirm.Options[0].Kind != "yes" || confirm.Options[1].Kind != "no" {
		t.Fatalf("precondition: search confirmation is not a yes/no gate: %+v", confirm.Options)
	}
	submitChoices(t, e, confirm.Options[0].Index)

	search := passUntilNonPriority(t, e, 40)
	if search == nil || search.Kind != decision.KChoose || search.ResumeKind != "search" || search.Player != 0 {
		t.Fatalf("first library search = %+v, want seat 0 search", search)
	}
	if len(search.Options) == 0 {
		t.Fatal("precondition: seat 0's library search has no basic-land option")
	}
	submitChoices(t, e, search.Options[0].Index)

	pending := e.Pending()
	if pending == nil || pending.Kind != decision.KChoose || pending.ResumeKind != "search_mayshuffle" || pending.Player != 0 {
		t.Fatalf("pending after seat 0 search = %+v, want seat 0 may-shuffle confirm", pending)
	}
	if pending.ResumeTarget != 0 {
		t.Fatalf("may-shuffle target = %d, want seat 0", pending.ResumeTarget)
	}
}
