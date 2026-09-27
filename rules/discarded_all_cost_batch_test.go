package rules

// The cost-discard half of the Mode$ DiscardedAll batch cadence: a cost that
// discards a settled set of cards is ONE discard action (CR 701.8), so a
// multi-card cost discard queues ONE trigger whose TriggerCount$Amount is the
// number of matching cards -- never one batch-of-one per emitted DiscardCost
// event. Every cost discard goes through Engine.payDiscardCost, which brackets
// its emission loop with BeginDiscardBatch/EndDiscardBatch (the same bracket
// effects/cardflow.go's effDiscard uses for api:Discard).
//
// Real cost payment, not the batch methods directly: Anurid Brushhopper's
// real corpus activated ability `Cost$ Discard<2/Card>` is offered and paid
// through the production activation flow, and Magmakin Artillerist's real
// DiscardedAll trigger is the observer whose X reads TriggerCount$Amount.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDiscardedAllMultiCardCostIsOneBatch drives a REAL two-card discard cost
// (Anurid Brushhopper's `Discard<2/Card>` activated ability) and asserts the
// observer (Magmakin Artillerist, "Whenever you discard one or more cards, this
// deals X damage..." where X is the number of cards discarded) fires ONCE for
// the payment with amount 2. Before the cost-discard bracket, each DiscardCost
// event was its own batch-of-one: two pushes and two points of damage, or --
// for a FirstTime$/amount card -- the wrong total.
func TestDiscardedAllMultiCardCostIsOneBatch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	brush := mustCorpusCard(t, reg, "Anurid Brushhopper")
	idx := costAbilityIndex(t, brush, "Discard<2")
	e, cfg, ids := edrBoard(t, reg, 6201, map[string]state.Zone{
		"Anurid Brushhopper":   state.ZBattlefield,
		"Magmakin Artillerist": state.ZBattlefield,
		"Grizzly Bears":        state.ZHand,
		"Hill Giant":           state.ZHand,
	})
	brushID, magID := ids["Anurid Brushhopper"], ids["Magmakin Artillerist"]
	bears, giant := ids["Grizzly Bears"], ids["Hill Giant"]

	// PRECONDITIONS, each its own failure: the ability source is on the
	// battlefield (where its activated ability is offered), the observer is on
	// the battlefield (so its TriggerZones$ Battlefield trigger is live), the
	// two fodder cards are in hand (where Discard<2/Card> reads them and where
	// the ask must offer them), the two fodder ids differ (so the count is
	// genuinely two distinct cards, not one id twice), and the opponent's life
	// is a real number the damage can move.
	if o := e.G.Obj(brushID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Anurid Brushhopper id %d zone = %v, want battlefield (vacuous setup)", brushID, o)
	}
	if o := e.G.Obj(magID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin Artillerist id %d zone = %v, want battlefield (vacuous setup)", magID, o)
	}
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Grizzly Bears id %d zone = %v, want hand (vacuous setup)", bears, o)
	}
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Hill Giant id %d zone = %v, want hand (vacuous setup)", giant, o)
	}
	if bears == giant {
		t.Fatalf("the two fodder ids are equal (%d): the count-2 claim would be vacuous", bears)
	}
	before := e.G.Players[1].Life
	if before <= 0 {
		t.Fatalf("opponent life = %d, want a real positive life the damage can reduce", before)
	}
	if pushes := triggerPushesFor(e, magID); pushes != 0 {
		t.Fatalf("Magmakin already pushed %d triggers before the discard (vacuous setup)", pushes)
	}

	// Pay the {1}{G}{W} activation cost, then answer the Discard<2/Card> ask
	// with the two fodder cards.
	addMana(t, e, 0, "1GW")
	edrSeatZeroPriority(t, e)
	opt := abilityOption(t, e, brushID, idx)
	submitChoices(t, e, opt.Index)

	// The ask must be a real exact-2 discard choice over the two fodder: if no
	// ask is posed the payment path changed and this test would be vacuous.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 {
		t.Fatalf("discard ask = %+v, want a KChoose Min=2 Max=2 discard-cost choice", d)
	}
	picks := make([]int, 0, 2)
	for _, want := range []state.ObjID{bears, giant} {
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("discard ask did not offer fodder %d: %+v", want, d.Options)
		}
		picks = append(picks, found)
	}
	submitChoices(t, e, picks...)

	// Both cards really left the hand for the graveyard -- the cost was paid,
	// so the observer had a real discard action to see.
	for _, id := range []state.ObjID{bears, giant} {
		if got := zoneOfObj(e, id); got != state.ZGraveyard {
			t.Fatalf("fodder %d zone = %v, want graveyard (the discard cost must really run)", id, got)
		}
	}
	// A two-card cost discard is ONE discard action, so it queues ONE trigger.
	// A per-event latch (before this ticket) queues two, which the trigger
	// drain surfaces as a KTriggerOrder ask over both; name that directly here
	// rather than letting the drain report it obliquely.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		t.Fatalf("a two-card cost discard offered %d simultaneous DiscardedAll triggers, want 1 (one action, one trigger)", len(d.Options))
	}
	drainMillTrigger(t, e, 40)

	// And exactly one instance reached the stack and resolved.
	if got := triggerPushesFor(e, magID); got != 1 {
		t.Fatalf("a two-card cost discard pushed %d DiscardedAll triggers, want 1 (one action, one trigger)", got)
	}
	// Its TriggerCount$Amount is 2: Magmakin deals exactly 2 to the single
	// opponent. A per-event batch would deal 1 (or 1 then 1, moving life by 2
	// only through two separate resolutions the single-trigger assertion above
	// already excludes).
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("a two-card cost discard dealt %d damage, want 2 (TriggerCount$Amount = the action's card count)", before-got)
	}
	replayCheck(t, e, cfg)
}

// TestDiscardedAllSingleCardCostIsOneBatch is the batch-of-one companion: a
// single-card cost discard still fires the observer exactly once with amount 1.
// Mnemonic Sphere's real `Cost$ U Discard<1/CARDNAME>` ability is activated
// from hand and discards itself; Magmakin sees one discard action.
func TestDiscardedAllSingleCardCostIsOneBatch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sphere := mustCorpusCard(t, reg, "Mnemonic Sphere")
	idx := costAbilityIndex(t, sphere, "Discard<1")
	e, _, ids := edrBoard(t, reg, 6202, map[string]state.Zone{
		"Mnemonic Sphere":      state.ZHand,
		"Magmakin Artillerist": state.ZBattlefield,
	})
	sph, magID := ids["Mnemonic Sphere"], ids["Magmakin Artillerist"]

	if o := e.G.Obj(sph); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Mnemonic Sphere id %d zone = %v, want hand (vacuous setup)", sph, o)
	}
	if o := e.G.Obj(magID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin Artillerist id %d zone = %v, want battlefield (vacuous setup)", magID, o)
	}
	before := e.G.Players[1].Life
	if before <= 0 {
		t.Fatalf("opponent life = %d, want a real positive life", before)
	}

	addMana(t, e, 0, "U")
	edrSeatZeroPriority(t, e)
	opt := abilityOption(t, e, sph, idx)
	submitChoices(t, e, opt.Index)
	// Discard<1/CARDNAME> with the source its sole candidate still poses the
	// ask; answer it with the Sphere itself.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		picked := -1
		for _, o := range d.Options {
			if o.Obj == sph {
				picked = o.Index
			}
		}
		if picked < 0 {
			t.Fatalf("discard ask did not offer the source to discard: %+v", d.Options)
		}
		submitChoices(t, e, picked)
	}
	if got := zoneOfObj(e, sph); got != state.ZGraveyard {
		t.Fatalf("Mnemonic Sphere zone = %v, want graveyard (the discard cost must really run)", got)
	}
	drainMillTrigger(t, e, 40)
	if got := triggerPushesFor(e, magID); got != 1 {
		t.Fatalf("a one-card cost discard pushed %d DiscardedAll triggers, want 1", got)
	}
	if got := e.G.Players[1].Life; got != before-1 {
		t.Fatalf("a one-card cost discard dealt %d damage, want 1", before-got)
	}
}
