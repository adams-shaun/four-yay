package rules

// The unless-payment half of the Mode$ DiscardedAll batch cadence: a
// Discard<2/Card> unless cost is ONE discard action (CR 701.8), so a
// multi-card cost settlement queues ONE trigger whose TriggerCount$Amount is
// the number of cards -- never one batch-of-one per emitted Discard event.
// Every unless-payment discard settlement goes through
// (*Engine).advanceUnlessPayment, which brackets its emission loop with
// BeginDiscardBatch/EndDiscardBatch (the same bracket rules/cast.go's
// payDiscardCost and effects/cardflow.go's effDiscard use).
//
// Real payment, not the batch methods directly: Thrilling Discovery's real
// corpus switched unless cost (`DBDraw: DB$ Draw | NumCards$ 3 | UnlessCost$
// Discard<2/Card> | UnlessPayer$ You | UnlessSwitched$ True`) is cast and paid
// through the production cast/resolution/unless flows, and Magmakin
// Artillerist's real DiscardedAll trigger is the observer whose X reads
// TriggerCount$Amount.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDiscardedAllMultiCardUnlessPaymentIsOneBatch casts the REAL corpus
// Thrilling Discovery and pays its `Discard<2/Card>` unless cost with two
// DISTINCT hand cards. Before the settlement bracket, each of the two
// Discard events was its own batch-of-one: the observer queued two triggers
// of amount 1 (or, for a FirstTime$/amount card, the wrong total). It asserts
// the payer really chose two distinct cards (both hand -> graveyard), the
// observer is active in its battlefield trigger zone, exactly ONE trigger is
// pushed, and its TriggerCount$Amount causes two damage rather than one.
func TestDiscardedAllMultiCardUnlessPaymentIsOneBatch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, 6301, map[string]state.Zone{
		"Thrilling Discovery":  state.ZHand,
		"Magmakin Artillerist": state.ZBattlefield,
		"Grizzly Bears":        state.ZHand,
		"Hill Giant":           state.ZHand,
		"Gray Ogre":            state.ZHand,
	})
	spell := ids["Thrilling Discovery"]
	mag := ids["Magmakin Artillerist"]
	bears, giant, ogre := ids["Grizzly Bears"], ids["Hill Giant"], ids["Gray Ogre"]

	// PRECONDITIONS, each its own loud failure: the spell is in hand (where it
	// can be cast), the observer is on the battlefield (where its
	// TriggerZones$ Battlefield trigger is live), the three fodder cards are in
	// hand (where Discard<2/Card> reads them and where the ask must offer
	// them), the fodder ids all differ (so a count of 2 is genuinely two
	// distinct cards, never the same id twice), and the opponent's life is a
	// real positive number the damage can reduce.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Thrilling Discovery id %d zone = %v, want hand (vacuous setup)", spell, o)
	}
	if o := e.G.Obj(mag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin Artillerist id %d zone = %v, want battlefield (vacuous setup)", mag, o)
	}
	for _, id := range []state.ObjID{bears, giant, ogre} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("fodder %d zone = %v, want hand (vacuous setup)", id, o)
		}
	}
	if bears == giant || bears == ogre || giant == ogre {
		t.Fatalf("fodder ids not distinct (%d, %d, %d): the count-2 claim would be vacuous", bears, giant, ogre)
	}
	before := e.G.Players[1].Life
	if before <= 0 {
		t.Fatalf("opponent life = %d, want a real positive life the damage can reduce", before)
	}
	if pushes := triggerPushesFor(e, mag); pushes != 0 {
		t.Fatalf("Magmakin already pushed %d triggers before the discard (vacuous setup)", pushes)
	}

	// Cast Thrilling Discovery ({R}{W}); the payment (601.2h) completes the
	// cast, then the spell resolves: GainLife 2 poses DBDraw's unless ask.
	addMana(t, e, 0, "RW")
	submitChoices(t, e, passToCast(t, e, spell))

	var ask *decision.Decision
	for i := 0; i < 12 && e.Pending() != nil; i++ {
		d := e.Pending()
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			ask = d
			break
		}
		if d.Kind == decision.KPriority {
			castFirst(t, e, "pass")
			continue
		}
		t.Fatalf("unexpected decision %v while driving the cast to its unless ask: %+v", d.Kind, d)
	}
	if ask == nil {
		t.Fatal("never reached the unless_pay ask for Thrilling Discovery's Discard<2/Card> cost")
	}
	if ask.Player != 0 {
		t.Fatalf("unless ask player = seat %d, want the caster seat 0 (UnlessPayer$ You)", ask.Player)
	}
	answerUnlessPay(t, e, true)

	// The ask must be a real exact-2 discard choice over the hand; if the
	// engine auto-picked (or posed no ask) the two-card choice this test would
	// be vacuous. The hand holds three distinct fodder plus any mountains the
	// deterministic deal left, so more than two candidates are eligible.
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

	// Both chosen cards really left the hand for the graveyard -- the cost was
	// paid with two distinct cards, so the observer had a real discard action
	// to see.
	for _, id := range []state.ObjID{bears, giant} {
		if got := zoneOfObj(e, id); got != state.ZGraveyard {
			t.Fatalf("chosen fodder %d zone = %v, want graveyard (the discard cost must really run)", id, got)
		}
	}

	// The two-card discard is ONE action, so it queues ONE trigger. A
	// per-event latch (before this ticket) queues two, which the trigger drain
	// surfaces as a KTriggerOrder ask over both; name that directly rather
	// than letting the drain report it obliquely.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		t.Fatalf("a two-card unless discard offered %d simultaneous DiscardedAll triggers, want 1 (one action, one trigger)", len(d.Options))
	}
	drainMillTrigger(t, e, 40)

	if got := triggerPushesFor(e, mag); got != 1 {
		t.Fatalf("a two-card unless discard pushed %d DiscardedAll triggers, want 1 (one action, one trigger)", got)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("a two-card unless discard dealt %d damage, want 2 (TriggerCount$Amount = the action's card count)", before-got)
	}
	replayCheck(t, e, cfg)
}

// TestDiscardedAllSingleCardUnlessPaymentIsOneBatch is the batch-of-one
// companion on the same flow: Witch's Mark's real switched `Discard<1/Card>`
// unless cost still fires the observer exactly once with amount 1, so the
// multi-card bracket neither inflates nor merges a single-card payment.
func TestDiscardedAllSingleCardUnlessPaymentIsOneBatch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, 6302, map[string]state.Zone{
		"Witch's Mark":         state.ZHand,
		"Magmakin Artillerist": state.ZBattlefield,
		"Grizzly Bears":        state.ZHand,
	})
	spell := ids["Witch's Mark"]
	mag := ids["Magmakin Artillerist"]
	bears := ids["Grizzly Bears"]

	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Witch's Mark id %d zone = %v, want hand (vacuous setup)", spell, o)
	}
	if o := e.G.Obj(mag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin id %d zone = %v, want battlefield (vacuous setup)", mag, o)
	}
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZHand {
		t.Fatalf("fodder %d zone = %v, want hand (vacuous setup)", bears, o)
	}
	before := e.G.Players[1].Life
	if before <= 0 {
		t.Fatalf("opponent life = %d, want a real positive life (vacuous setup)", before)
	}
	if pushes := triggerPushesFor(e, mag); pushes != 0 {
		t.Fatalf("Magmakin already pushed %d triggers before the discard (vacuous setup)", pushes)
	}

	addMana(t, e, 0, "RR")
	submitChoices(t, e, passToCast(t, e, spell))
	var ask *decision.Decision
	for i := 0; i < 12 && e.Pending() != nil; i++ {
		d := e.Pending()
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			ask = d
			break
		}
		if d.Kind == decision.KPriority {
			castFirst(t, e, "pass")
			continue
		}
		t.Fatalf("unexpected decision %v while driving the cast to its unless ask: %+v", d.Kind, d)
	}
	if ask == nil {
		t.Fatal("never reached the unless_pay ask for Witch's Mark's Discard<1/Card> cost")
	}
	answerUnlessPay(t, e, true)

	// Exactly one fodder-bearing candidate: the engine auto-picks the single
	// card. The prior decision may be a KChoose if other hand cards are
	// eligible, so answer the exact-1 ask when it appears.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.Min == 1 && d.Max == 1 {
		found := -1
		for _, o := range d.Options {
			if o.Obj == bears {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("single-card discard ask did not offer fodder %d: %+v", bears, d.Options)
		}
		submitChoices(t, e, found)
	}
	if got := zoneOfObj(e, bears); got != state.ZGraveyard {
		t.Fatalf("chosen fodder %d zone = %v, want graveyard (the discard cost must really run)", bears, got)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		t.Fatalf("a single-card unless discard offered %d triggers, want 1", len(d.Options))
	}
	// Witch's Mark's switched body chains an optional token sub whose target
	// ask (Min 0) follows the payment; answer it with nothing, then pass the
	// priority rounds so the DiscardedAll trigger resolves.
	for i := 0; i < 30 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil || (len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0) {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			castFirst(t, e, "pass")
		case decision.KChoose:
			submitChoices(t, e) // the optional token target: none
		default:
			t.Fatalf("unexpected decision %v while draining the single-card payment: %+v", d.Kind, d)
		}
	}
	if got := triggerPushesFor(e, mag); got != 1 {
		t.Fatalf("a single-card unless discard pushed %d DiscardedAll triggers, want 1", got)
	}
	if got := e.G.Players[1].Life; got != before-1 {
		t.Fatalf("a single-card unless discard dealt %d damage, want 1 (TriggerCount$Amount = 1)", before-got)
	}
	replayCheck(t, e, cfg)
}
