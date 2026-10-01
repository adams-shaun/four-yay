package botpolicy

import (
	"os"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Policy obligations, not CR requirements on a player's strategic choices.
// Kept opt-in until I-12 is fixed; the ordinary target suite must not pin
// missing information or a dead seat as a proven lethal opportunity.
func requireCR601Audit(t *testing.T, finding string) {
	t.Helper()
	if os.Getenv("GORGE_CR_CONFORMANCE") != "1" {
		t.Skip("known policy divergence: " + finding)
	}
}

func issue12Corpus(t *testing.T) (*cards.Registry, int) {
	t.Helper()
	r := testutil.CorpusRegistry(t)
	bolt, ok := r.Lookup("Lightning Bolt")
	if !ok {
		t.Fatal("I-12 fixture: corpus Bolt absent")
	}
	sa := bolt.Faces[0].SpellAbility()
	if sa == nil || sa.API != "DealDamage" {
		t.Fatal("I-12 fixture: real Bolt damage SA absent")
	}
	n, err := strconv.Atoi(sa.Params["NumDmg"])
	if err != nil || n != 3 {
		t.Fatal("I-12 fixture: real Bolt is not literal three damage")
	}
	return r, n
}

func TestIssue12UnknownLifeIsNotLethal(t *testing.T) {
	requireCR601Audit(t, "I-12: unknown life is not zero")
	_, dmg := issue12Corpus(t)
	b := boardOf(def(1, 6, 6))
	b.Life.Set(0, 20)
	b.Life.Delete(1)
	got, d := effectTargetDecision(b, &dmg, []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || d.Options[got[0]].Kind == "player" {
		t.Fatalf("I-12 unknown-is-no contract: missing life selected as lethal face: %v", got)
	}
}

func TestIssue12DeadFaceDoesNotDisplaceLivingLethal(t *testing.T) {
	requireCR601Audit(t, "I-12: nonpositive life is not a useful lethal target")
	_, dmg := issue12Corpus(t)
	b := boardOf()
	b.Life.Set(0, 20)
	b.Life.Set(1, -4)
	b.Life.Set(2, 3)
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		TargetEffect: &decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{Amount: &dmg}},
		Options:      []decision.Option{{Index: 0, Kind: "player", Player: 1}, {Index: 1, Kind: "player", Player: 2}}}
	got := Decide(b, d, rng(1)).Choices
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("I-12 live-lethal contract: selected %v, want living player option 1", got)
	}
}

func TestIssue12CombinedAttackThreatBeatsUnkillableTappedCreature(t *testing.T) {
	requireCR601Audit(t, "I-12: consider combined lethal attackers")
	r, dmg := issue12Corpus(t)
	courser, ok := r.Lookup("Centaur Courser")
	if !ok {
		t.Fatal("I-12 fixture: missing Courser")
	}
	dreadmaw, ok := r.Lookup("Colossal Dreadmaw")
	if !ok {
		t.Fatal("I-12 fixture: missing Dreadmaw")
	}
	c, big := courser.Faces[0], dreadmaw.Faces[0]
	b := boardOf(def(1, int32(c.Power()), int32(c.Toughness())), def(2, int32(c.Power()), int32(c.Toughness())), def(3, int32(big.Power()), int32(big.Toughness())))
	tapped := b.Creatures.Get(203)
	tapped.Tapped = true
	b.Creatures.Set(203, tapped)
	b.Life.Set(0, 5)
	b.Life.Set(1, 20)
	if c.Power()*2 < int(b.Life.Get(0)) || c.Power() >= int(b.Life.Get(0)) || c.Toughness() > dmg || big.Toughness() <= dmg {
		t.Fatal("I-12 fixture does not distinguish combined lethal from a single attacker")
	}
	got, d := effectTargetDecision(b, &dmg, []tgt{opp(201), opp(202), opp(203)}, 1, 1)
	if len(got) != 1 || (objAt(d, got[0]) != 201 && objAt(d, got[0]) != 202) {
		t.Fatalf("I-12 threat-removal contract: picked %v; Bolt must remove a killable member of the lethal pair, not the tapped unkillable creature", choicesObj(got, d))
	}
}
