package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// granted_activation_cost_test.go is task grantcost1's leaf: a granted
// activation (rules/speed.go's beginGrantedActivation, the max-speed
// "granted" option and beginActivation's SVar branch) used to pay ONLY its
// mana/life/{T} parts -- every Sac/Discard/SubCounter/AddCounter/Exile/...
// component of the granted body's Cost$ was silently dropped, so 64 live
// corpus carriers activated for free (or undercharged). The fix routes the
// granted activation through the SAME cast flow a printed activated ability
// uses (pendingCast/continueCast/payCast), so every part is asked and paid.
// Both mint identities are covered: Acidic Sliver is a SELF-grant
// (DelayedPush), Ichormoon Gauntlet's grant onto a planeswalker is
// CROSS-OBJECT (GrantAbilityPush).

// TestGrantedAbilityPaysNonManaCosts covers Done-means (a): Acidic Sliver's
// real corpus granted body, `Cost$ 2 Sac<1/CARDNAME>`. Before the fix the
// activation spent the {2} and dealt the damage but NEVER sacrificed the
// Sliver -- it survived and the ability could be reused for free. After the
// fix the Sliver is gone from the battlefield, the mana is spent, and the 2
// damage still resolves.
func TestGrantedAbilityPaysNonManaCosts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sliver := mustCorpusCard(t, reg, "Acidic Sliver")
	mountain := mustCorpusCard(t, reg, "Mountain")

	e := printedAddabilityEngine(t, sliver, mountain)
	id := moveCardToBattlefield(t, e, sliver)
	addMana(t, e, 0, "CC") // exactly the {2}; the mana-spend half is exercised too

	opt, ok := findSVarOption(t, e, id, "Damage")
	if !ok {
		t.Fatalf("Acidic Sliver's granted damage ability not offered: %+v", e.Pending().Options)
	}
	if opt.GrantSource != id {
		t.Fatalf("self-grant GrantSource = %d, want the Sliver itself %d", opt.GrantSource, id)
	}
	submitChoices(t, e, opt.Index)

	// ValidTgts$ Any: the target ask now comes BEFORE payment (CR 601.2c),
	// the same ordering every printed activation uses.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a KTarget decision for the granted ability, got %+v", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("opponent face not offered by ValidTgts$ Any: %+v", d.Options)
	}
	life := e.G.Players[1].Life
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[1].Life; got != life-2 {
		t.Fatalf("granted ability dealt no damage: opponent life %d -> %d, want -2", life, got)
	}
	for i := range e.G.Players[0].Pool {
		if e.G.Players[0].Pool[i] != 0 {
			t.Fatalf("the {2} was not spent: pool %v", e.G.Players[0].Pool)
		}
	}
	if o := e.G.Obj(id); o == nil || o.Zone == state.ZBattlefield {
		zone := "gone"
		if o != nil {
			zone = o.Zone.String()
		}
		t.Fatalf("DEFECT: granted Sac<1/CARDNAME> was not paid; the Sliver is still on the battlefield (zone %s)", zone)
	}
}

// TestGrantedActivationChargesLoyaltySubCounter covers Done-means (b) and the
// CROSS-OBJECT mint: Ichormoon Gauntlet's real corpus grant gives planeswalkers
// `[-12]: Take an extra turn after this one`, whose `Cost$ SubCounter<12/LOYALTY>`
// was never charged before the fix -- the ultimate fired for free. Garruk the
// Slayer enters with 20 loyalty, so the payment is affordable; after the fix
// twelve loyalty counters must be REMOVED (20 -> 8) and the extra turn granted.
func TestGrantedActivationChargesLoyaltySubCounter(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	gauntlet := mustCorpusCard(t, reg, "Ichormoon Gauntlet")
	garruk := mustCorpusCard(t, reg, "Garruk the Slayer")

	e := printedAddabilityEngine(t, gauntlet, garruk)
	moveCardToBattlefield(t, e, gauntlet)
	garrukID := moveCardToBattlefield(t, e, garruk)
	addMana(t, e, 0, "") // drive to Main1 and re-ask priority

	if got := e.G.Obj(garrukID).Counter("LOYALTY"); got != 20 {
		t.Fatalf("Garruk entered with %d loyalty, want 20", got)
	}

	opt, ok := findSVarOption(t, e, garrukID, "PWExtraTurn")
	if !ok {
		t.Fatalf("Ichormoon's granted [-12] ability not offered on the planeswalker: %+v", e.Pending().Options)
	}
	if opt.GrantSource == 0 || opt.GrantSource == garrukID {
		t.Fatalf("granted option GrantSource = %d, want the Gauntlet id", opt.GrantSource)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(garrukID).Counter("LOYALTY"); got != 8 {
		t.Fatalf("DEFECT: granted SubCounter<12/LOYALTY> was not charged: loyalty = %d, want 8", got)
	}
	if len(e.G.ExtraTurnQueue) == 0 || e.G.ExtraTurns[0] == 0 {
		t.Fatalf("the granted extra turn did not resolve: ExtraTurns=%v queue=%v", e.G.ExtraTurns, e.G.ExtraTurnQueue)
	}
}
