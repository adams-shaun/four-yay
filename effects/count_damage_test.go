package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestDamageCountHeadsReadTheirCorpusCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sidequest, ok := reg.Lookup("Sidequest: Play Blitzball")
	if !ok {
		t.Fatal("corpus missing Sidequest: Play Blitzball")
	}
	caseCard, ok := reg.Lookup("Case of the Burning Masks")
	if !ok {
		t.Fatal("corpus missing Case of the Burning Masks")
	}
	ojer, ok := reg.Lookup("Ojer Axonil, Deepest Might")
	if !ok {
		t.Fatal("corpus missing Ojer Axonil, Deepest Might")
	}
	maxBody := sidequest.Faces[0].SVars["X"]
	numBody := caseCard.Faces[0].SVars["X"]
	noncombatBody := ojer.Faces[1].SVars["X"]
	if maxBody != "Count$MaxCombatDamageThisTurn" || numBody != "Count$NumDamageThisTurn Card.YouCtrl,Emblem.YouCtrl Player,Permanent" || noncombatBody != "Count$NonCombatDamageThisTurn Card.Red+YouCtrl Any" {
		t.Fatalf("unexpected corpus Count$ bodies: %q, %q, %q", maxBody, numBody, noncombatBody)
	}
	h, c := fixtureHost(t)
	playerRecipient := state.PlayerRef(1)
	c.Source = h.g.AddObject(sidequest, 0).ID
	h.g.Obj(c.Source).DamageDealtThisTurn = []state.DamageDealtRecord{{Recipient: playerRecipient, Amount: 4, Combat: true}, {Recipient: playerRecipient, Amount: 2, Combat: false}}
	if got := EvalCount(h, c, maxBody); got != 4 {
		t.Fatalf("Blitzball corpus count = %d, want max combat damage 4", got)
	}
	h.g.Obj(c.Source).DamageDealtThisTurn = nil
	caseID := h.g.AddObject(caseCard, 0).ID
	h.g.Obj(caseID).DamageDealtThisTurn = []state.DamageDealtRecord{{Recipient: playerRecipient, Amount: 3}}
	c.Source = caseID
	if got := EvalCount(h, c, numBody); got != 3 {
		t.Fatalf("Burning Masks corpus count = %d, want 3", got)
	}
	h.g.Obj(caseID).DamageDealtThisTurn = nil
	ojerID := h.g.AddObject(ojer, 0).ID
	h.g.Obj(ojerID).DamageDealtThisTurn = []state.DamageDealtRecord{{Recipient: playerRecipient, Amount: 5}}
	c.Source = ojerID
	if got := EvalCount(h, c, noncombatBody); got != 5 {
		t.Fatalf("Ojer corpus count = %d, want noncombat damage 5", got)
	}
}
