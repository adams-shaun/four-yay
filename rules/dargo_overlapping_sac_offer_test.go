package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// Both announced Sac parts have a candidate at X=1, but it is the SAME
// artifact. The shared-X reduction makes the mana payable at X=1, not X=0;
// a cast must not be offered unless both sacrifices can actually settle.
func TestDargoOverlappingSacXPartsWithholdUnsettleableOffer(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
	}, "RRRRR")
	if len(ids) != 1 || e.G.Obj(ids[0]).Zone != state.ZBattlefield {
		t.Fatalf("precondition: artifact candidate not on battlefield: %v", ids)
	}
	base := pay.WithSpellAbilityExtras(e.G.Obj(spell).Face(), e.castOfferBase(0, spell))
	extra := ParseCost("Sac<X/Artifact>")
	if len(base.Sac) != 1 || !base.Sac[0].Announced || len(extra.Sac) != 1 || !extra.Sac[0].Announced {
		t.Fatalf("precondition: want two announced Sac parts: base=%+v extra=%+v", base.Sac, extra.Sac)
	}
	if n := len(pay.SacrificeCostCandidates(asPayer(e), 0, spell, base.Sac[0], false)); n != 1 {
		t.Fatalf("precondition: Dargo Sac pool=%d, want 1", n)
	}
	if n := len(pay.SacrificeCostCandidates(asPayer(e), 0, spell, extra.Sac[0], false)); n != 1 {
		t.Fatalf("precondition: added Sac pool=%d, want 1", n)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 5 || pool.Total() >= 7 || pool[state.MR] == 0 {
		t.Fatalf("precondition: pool=%+v, want five including red: pays X=1's {4}{R}, not X=0's {6}{R}", pool)
	}
	if !e.offerCastable(0, spell, base, spellScope(""), false) {
		t.Fatal("precondition: X=1 with only Dargo's Sac part must be payable")
	}
	if e.offerCastable(0, spell, base.Plus(extra), spellScope(""), false) {
		t.Fatal("offered X=1 although both sacrifice parts require the same sole artifact")
	}
	// The rule is assignment, not greedy reservation: Dargo's broad part
	// must take the creature, leaving the artifact for the narrower part.
	e, spell, ids = dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "RRRRR")
	for _, id := range ids {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition: artifact %d not on battlefield", id)
		}
	}
	base = pay.WithSpellAbilityExtras(e.G.Obj(spell).Face(), e.castOfferBase(0, spell))
	if len(base.Sac) != 1 || !base.Sac[0].Announced {
		t.Fatalf("precondition: Dargo announced Sac part missing: %+v", base.Sac)
	}
	if n := len(pay.SacrificeCostCandidates(asPayer(e), 0, spell, base.Sac[0], false)); n != 2 {
		t.Fatalf("precondition: Dargo candidate pool=%d, want 2", n)
	}
	if n := len(pay.SacrificeCostCandidates(asPayer(e), 0, spell, extra.Sac[0], false)); n != 1 {
		t.Fatalf("precondition: added artifact-only candidate pool=%d, want 1", n)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 5 || pool.Total() >= 7 || pool[state.MR] == 0 {
		t.Fatalf("precondition: pool=%+v, want five including red: payable only after reduction", pool)
	}
	if !e.offerCastable(0, spell, base.Plus(extra), spellScope(""), false) {
		t.Fatal("withheld X=1 though creature and artifact can pay the distinct parts")
	}
}

// An already-offered full-price cast can announce X=0, but cannot announce
// X=1 if the only artifact would have to pay both parts. The bot sees the
// same filtered announcement options as the human seat.
func TestDargoOverlappingSacXPartsExcludeUnsettleableAnnouncement(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
	}, "RRRRRRR")
	if len(ids) != 1 || e.G.Obj(ids[0]).Zone != state.ZBattlefield {
		t.Fatalf("precondition: sole artifact not on battlefield: %v", ids)
	}
	base := pay.WithSpellAbilityExtras(e.G.Obj(spell).Face(), e.castOfferBase(0, spell))
	extra := ParseCost("Sac<X/Artifact>")
	if len(base.Sac) != 1 || !base.Sac[0].Announced || len(extra.Sac) != 1 || !extra.Sac[0].Announced {
		t.Fatalf("precondition: want two announced Sac parts: base=%+v extra=%+v", base.Sac, extra.Sac)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 7 || pool[state.MR] == 0 {
		t.Fatalf("precondition: pool=%+v, want full {6}{R} payable even at X=0", pool)
	}
	cost := base.Plus(extra)
	if !e.offerCastable(0, spell, cost, spellScope(""), false) {
		t.Fatal("precondition: full-price X=0 cast was withheld")
	}
	e.cast = &pendingCast{player: 0, card: spell, from: state.ZHand, ability: -1, cost: cost}
	if !e.xAsk() {
		t.Fatal("missing shared-X announcement ask")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("X ask: %+v", d)
	}
	foundZero := false
	for _, o := range d.Options {
		if o.Kind != "x" {
			t.Fatalf("unexpected X option: %+v", o)
		}
		if o.Amount == 0 {
			foundZero = true
		} else {
			t.Fatalf("unsettleable X=%d offered despite a single artifact for both parts", o.Amount)
		}
	}
	if !foundZero {
		t.Fatal("full-price X=0 missing from ask")
	}
}
