package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestSoulImmolationRaiseCostBlightX is the REAL-corpus regression for the
// RaiseCost-carried Blight<X>: Soul Immolation's
//
//	S:Mode$ RaiseCost | ValidCard$ Card.Self | Type$ Spell | Cost$ Blight<X>
//
// static is NOT a plain mana/life raise, so raiseFromCost rejected it and the
// static degraded to the Amount$ fallback (absent → a zero raise). The
// `Announce$ X | XMax$ GrTo` damage then resolved with X = 0: no X ask, no
// blight payment, zero damage. The fix carries the Blight<X> part through
// costMods.extra so the SAME announced X reaches the offer gate, the ask, the
// payment and the DamageAll effect.
//
// The assertion is deliberately X-dependent: at X = 2 the opponent loses 2
// life and their 2/2 dies, neither of which happens under the silent zero.
func TestRaiseCostBlightSoulImmolationX(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := blightEngine(t, reg, 2, "Soul Immolation")
	spell := blightMove(t, e, 0, "Soul Immolation", state.ZHand)
	mine := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	theirs := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)

	// Preconditions. The spell must be a real sorcery in the caster's hand;
	// the blight rule reads controlled battlefield creatures, so the
	// controlled bear must be exactly there with toughness 2 (the X cap the
	// card's XMax$ GrTo names); the opponent must start above the damage
	// with a live 2/2 for the X-dependent creature damage to show on.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand || o.Face() == nil ||
		o.Face().Name != "Soul Immolation" {
		t.Fatalf("precondition: spell zone=%v, want Soul Immolation in hand", o)
	}
	if o := e.G.Obj(mine); o == nil || o.Zone != state.ZBattlefield || e.Toughness(mine) != 2 {
		t.Fatalf("precondition: controlled bear zone=%v toughness=%d, want battlefield/2", o, e.Toughness(mine))
	}
	if o := e.G.Obj(theirs); o == nil || o.Zone != state.ZBattlefield || e.Toughness(theirs) != 2 {
		t.Fatalf("precondition: opposing bear zone=%v toughness=%d, want battlefield/2", o, e.Toughness(theirs))
	}
	lifeBefore := e.G.Players[1].Life
	if lifeBefore <= 0 {
		t.Fatalf("precondition: opponent life=%d must be positive", lifeBefore)
	}
	// No blight counters exist yet, so a zero after the cast can only be the
	// silent-zero defect this test pins.
	if got := blightCounters(t, e)[mine]; got != 0 {
		t.Fatalf("precondition: bear already has %d M1M1 counters", got)
	}

	addMana(t, e, 0, "RRRRR") // {3}{R}{R}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending %+v, want priority at Main1", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("precondition: Soul Immolation not offered to cast: %+v", d.Options)
	}
	submitChoices(t, e, castIdx)

	// The X announcement, the blight pick and (if any) the mana payment, in
	// whatever order the stages run. With exactly one controlled creature the
	// blight pick is auto-settled (no decision), so it is asserted on the
	// resulting counters rather than on a pick option.
	xSeen, xValue := false, int32(-1)
	blightAskedMine := false
	for i := 0; i < 16; i++ {
		d = e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		switch {
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x":
			for _, o := range d.Options {
				if int32(o.Amount) == 2 {
					xSeen, xValue = true, 2
					submitChoices(t, e, o.Index)
					break
				}
			}
			if !xSeen {
				t.Fatalf("X=2 not offered by the RaiseCost-carried Blight<X> ask: %+v", d.Options)
			}
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "blightcost":
			for _, o := range d.Options {
				if o.Obj == mine {
					blightAskedMine = true
					submitChoices(t, e, o.Index)
					break
				}
			}
			if !blightAskedMine {
				t.Fatalf("controlled bear %d not offered as a blight target: %+v", mine, d.Options)
			}
		default:
			t.Fatalf("unexpected decision during the Soul Immolation cast: %+v", d)
		}
	}
	if !xSeen || xValue != 2 {
		t.Fatal("Soul Immolation never announced X = 2 (the RaiseCost Blight<X> did not reach xAsk)")
	}
	if got := blightCounters(t, e)[mine]; got != 2 {
		t.Fatalf("paid Blight<X=2> placed %d counters on the only controlled bear, want 2", got)
	}

	passUntilStackEmpty(t, e, 30)

	// The X-dependent effect: DamageAll dealt X to each opponent and each of
	// their creatures. Under the silent zero this cast dealt 0 to both.
	if got := e.G.Players[1].Life; got != lifeBefore-2 {
		t.Fatalf("opponent life=%d, want %d (X=2 damage); X-dependent DamageAll did not resolve X", got, lifeBefore-2)
	}
	if o := e.G.Obj(theirs); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("opposing 2/2 zone=%v damage=%d, want graveyard (X=2 lethal damage)", o.Zone, o.Damage)
	}
	replayCheck(t, e, cfg)
}
