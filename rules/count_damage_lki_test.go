package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestNumDamageCountUsesRecipientAtDamageTime(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Case of the Burning Masks")
	if !ok {
		t.Fatal("corpus missing Case of the Burning Masks")
	}
	body := card.Faces[0].SVars["X"]
	if body != "Count$NumDamageThisTurn Card.YouCtrl,Emblem.YouCtrl Player,Permanent" {
		t.Fatalf("Case of the Burning Masks corpus SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Case of the Burning Masks", "Grizzly Bears")
	source, recipient := ids["Case of the Burning Masks"], ids["Grizzly Bears"]
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Case source must be on the battlefield")
	}
	if o := e.G.Obj(recipient); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: damage recipient must be a battlefield permanent")
	}
	// Model a Clone that will cease to exist on leaving the battlefield.
	// Its damage-time Permanent identity still qualifies for Case's count.
	e.G.Obj(recipient).IsCopy = true
	if !e.G.Obj(recipient).IsCopy {
		t.Fatal("precondition: recipient must be a battlefield copy")
	}
	damageCountEvent(e, source, recipient, 0, 3, true)
	e.emit(events.Event{Kind: events.MoveZone, Obj: recipient, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(recipient); o == nil || o.Zone != state.ZGraveyard {
		t.Fatal("precondition: damaged permanent must have left the battlefield")
	}
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: source}, body); got != 1 {
		t.Fatalf("Case corpus count after damaged permanent leaves: %d, want one source with a damage-time permanent recipient", got)
	}
}
