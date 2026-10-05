package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestNumDamageCountUsesDamageTimeRecipientType(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Jace, Vryn's Prodigy")
	if !ok || len(card.Faces) < 2 {
		t.Fatal("corpus missing transformable Jace")
	}
	e, ids := pcdrEngine(t, "Case of the Burning Masks", "Jace, Vryn's Prodigy")
	source, recipient := ids["Case of the Burning Masks"], ids["Jace, Vryn's Prodigy"]
	permanent := e.G.Obj(recipient)
	if permanent == nil || permanent.Zone != state.ZBattlefield || !damageTimeHasType(permanent.Face().Types, "Creature") {
		t.Fatal("precondition: damage recipient must be a battlefield creature")
	}
	body := "Count$NumDamageThisTurn Card.YouCtrl,Emblem.YouCtrl Player,Creature"
	damageCountEvent(e, source, recipient, 0, 3, true)
	permanent.SetFaceIdx(1)
	if damageTimeHasType(permanent.Face().Types, "Creature") {
		t.Fatal("precondition: recipient's transformed face must not be a creature")
	}
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: source}, body); got != 3 {
		t.Fatalf("count after creature recipient transforms = %d, want damage-time creature total 3", got)
	}
}

func damageTimeHasType(types []string, want string) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}
