package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestSharedSetPropCountsCandidatesNotTokens is cardfuzz all seed
// 3286887339596248657: an amassed Sliver Army token derives "Sliver" twice
// (its token script's Sliver Army types plus amass's permanent "it's also a
// Sliver" layer-4 grant, CR 701.55b). SetPropCapacity counted the token's two
// "sliver" occurrences as two candidates sharing Sliver, so Secret Tunnel's
// "two target creatures you control that share a creature type" was offered
// with only that token and a Nightmare Horror, and its 2..2 target ask had no
// legal answer (the bot's reply was rejected: "expected 2..2 choices, got 1").
// CR 601.2c/602.2b: an activation whose targets cannot be chosen is not
// offered.
func TestSharedSetPropCountsCandidatesNotTokens(t *testing.T) {
	t.Parallel()
	sa := setPropSA("Creature.YouCtrl", map[string]string{
		"TargetsWithSameCreatureType": "True",
		"TargetMin":                   "2",
		"TargetMax":                   "2",
	})
	e := newSeats(t, 2)
	putBattlefield(t, e, 0, "Name:Stalker\nTypes:Creature Nightmare Horror\nPT:1/1\nOracle:x\n")
	army := putBattlefield(t, e, 0, "Name:Sliver Army\nTypes:Creature Sliver Army\nPT:0/0\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{Source: army, Controller: 0, Affects: "Card.Self",
		Layer: state.LType, AddTypes: []string{"Sliver"}, Permanent: true})
	n := 0
	for _, tok := range e.setPropTokens("creaturetype", army) {
		if tok == "sliver" {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("precondition: the amassed Army must derive Sliver twice, tokens %v", e.setPropTokens("creaturetype", army))
	}
	if e.targetSAAvailable(0, army, 0, sa, 0, false) {
		t.Fatal("a same-creature-type pair was judged available with no two creatures sharing a type")
	}
}
