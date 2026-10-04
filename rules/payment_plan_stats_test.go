package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The aph-plan-diagnostics board: seat 0 holds four instants the offer
// builder plans in one build -- a plannable {R} Shock, an {X}{R} spell the
// cost classifier declines (cost:x), a Spree spell the shape gate declines
// (shape:modal_cost), and a {C} spell whose only colourless source is a
// Mill-costed mana ability the manual window may use but no V1 witness does
// (insufficient, naming that source's tier).
const (
	paymentStatsXSpell     = "Name:Stats X Spell\nManaCost:X R\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	paymentStatsSpreeSpell = "Name:Stats Spree Spell\nManaCost:R\nTypes:Instant\nK:Spree\nA:SP$ Charm | Choices$ DBOne | MinCharmNum$ 1\nSVar:DBOne:DB$ Draw | NumCards$ 1 | ModeCost$ R\nOracle:x\n"
	paymentStatsCSpell     = "Name:Stats Colorless Spell\nManaCost:C\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	paymentStatsMillSource = "Name:Stats Millikin\nTypes:Artifact Creature Construct\nPT:0/1\nA:AB$ Mana | Cost$ T Mill<1> | Produced$ C\nOracle:x\n"
	paymentStatsTaxRelic   = "Name:Tax Relic\nTypes:Artifact\nS:Mode$ RaiseCost | ValidCard$ Card | Type$ Spell | Amount$ 1 | Description$ Spells cost {1} more.\nOracle:x\n"
)

type paymentStatsBoard struct {
	e                     *Engine
	shock, x, spree, cSpl state.ObjID
}

func newPaymentStatsBoard(t *testing.T) paymentStatsBoard {
	t.Helper()
	e, _, shock := newFixtureDeck(t, 9901, paymentPlanShock, paymentStatsXSpell, paymentStatsSpreeSpell, paymentStatsCSpell)
	b := paymentStatsBoard{e: e, shock: shock}
	b.x = moveSeededToHand(t, e, 0, "Stats X Spell")
	b.spree = moveSeededToHand(t, e, 0, "Stats Spree Spell")
	b.cSpl = moveSeededToHand(t, e, 0, "Stats Colorless Spell")
	onBoard(t, e, 0, paymentPlanMountain)
	onBoard(t, e, 0, paymentPlanMountain)
	mill := onBoard(t, e, 0, paymentStatsMillSource)
	e.G.Obj(mill).SummonSick = false
	return b
}

// runPaymentStatsScenario builds the offer, submits the Shock's plan, raises
// its cost after the offer and answers the target, which stops automation
// with a cost_changed fallback before any source is tapped.
func runPaymentStatsScenario(t *testing.T, b paymentStatsBoard) []decision.PaymentAction {
	t.Helper()
	e := b.e
	d := paymentPlanReask(t, e)
	var actions []decision.PaymentAction
	for _, a := range d.PaymentActions {
		actions = append(actions, decision.ClonePaymentAction(a))
	}
	a := paymentPlanActionFor(t, d, b.shock)
	submitPaymentPlan(t, e, d, a)
	onBoard(t, e, 1, paymentStatsTaxRelic)
	submitChoices(t, e, 0) // the Shock's target
	nd := e.Pending()
	if nd == nil || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackCostChanged {
		t.Fatalf("pending = %s, want a cost_changed fallback", paymentPlanPendingSummary(nd))
	}
	return actions
}

func TestPaymentPlanStatsCountsOutcomesNodesAndFallbacks(t *testing.T) {
	t.Parallel()
	b := newPaymentStatsBoard(t)
	// Attribute each outcome to its cast with the pure query (which records
	// nothing), so the histograms below are exactly these four verdicts.
	wantNodes := 0
	for _, tc := range []struct {
		name           string
		id             state.ObjID
		reason, detail string
		plan           bool
	}{
		{"shock", b.shock, "", "", true},
		{"x", b.x, "unsupported", "cost:x", false},
		{"spree", b.spree, "unsupported", "shape:modal_cost", false},
		{"colorless", b.cSpl, "insufficient", "source:last_resort", false},
	} {
		got := b.e.PlanCastPayment(0, paymentCast(tc.id))
		if (got.Plan != nil) != tc.plan || got.Reason != tc.reason || got.Detail != tc.detail {
			t.Fatalf("%s outcome = %+v, want reason %q detail %q plan %v", tc.name, got, tc.reason, tc.detail, tc.plan)
		}
		wantNodes += got.Nodes
	}
	stats := &PaymentPlanStats{}
	b.e.SetPaymentPlanStats(stats)
	actions := runPaymentStatsScenario(t, b)

	if len(actions) != 1 || actions[0].Cast.Object != b.shock {
		t.Fatalf("offered actions = %+v, want the Shock's alone", actions)
	}
	if stats.DecisionsBuilt != 1 || stats.PoolDeclined != 0 {
		t.Errorf("builds = %d (pool declined %d), want 1 (0): Submit must reuse the cached offer", stats.DecisionsBuilt, stats.PoolDeclined)
	}
	if stats.Candidates != 4 {
		t.Errorf("candidates = %d, want 4", stats.Candidates)
	}
	if want := map[string]int{"ready": 1, "unsupported": 2, "insufficient": 1}; !reflect.DeepEqual(stats.ByReason, want) {
		t.Errorf("by reason = %v, want %v", stats.ByReason, want)
	}
	if want := map[string]int{"cost:x": 1, "shape:modal_cost": 1, "source:last_resort": 1}; !reflect.DeepEqual(stats.ByDetail, want) {
		t.Errorf("by detail = %v, want %v", stats.ByDetail, want)
	}
	if stats.Nodes <= 0 || stats.Nodes != wantNodes || stats.MaxNodes <= 0 || stats.MaxNodes > stats.Nodes {
		t.Errorf("nodes = %d (max %d), want the candidates' positive total %d bounding its max", stats.Nodes, stats.MaxNodes, wantNodes)
	}
	if stats.SearchLimitHits != 0 {
		t.Errorf("search_limit hits = %d, want 0", stats.SearchLimitHits)
	}
	if stats.ActionsOffered != 1 || stats.PlansOffered != 1 {
		t.Errorf("offered = %d actions / %d plans, want 1/1", stats.ActionsOffered, stats.PlansOffered)
	}
	if stats.PlannedSubmissions != 1 {
		t.Errorf("planned submissions = %d, want 1", stats.PlannedSubmissions)
	}
	if want := map[string]int{paymentFallbackCostChanged: 1}; !reflect.DeepEqual(stats.Fallbacks, want) {
		t.Errorf("fallbacks = %v, want %v", stats.Fallbacks, want)
	}
}

// The sink is an observer: the same scenario on a sinkless engine produces
// the same events, chain head and offer.
func TestPaymentPlanStatsSinkChangesNothing(t *testing.T) {
	t.Parallel()
	with := newPaymentStatsBoard(t)
	with.e.SetPaymentPlanStats(&PaymentPlanStats{})
	without := newPaymentStatsBoard(t)
	offeredWith := runPaymentStatsScenario(t, with)
	offeredWithout := runPaymentStatsScenario(t, without)
	if !reflect.DeepEqual(offeredWith, offeredWithout) {
		t.Fatalf("offer with sink = %+v\nwithout = %+v", offeredWith, offeredWithout)
	}
	if !reflect.DeepEqual(with.e.L.Events, without.e.L.Events) {
		t.Fatal("event stream differs with the sink attached")
	}
	if hw, hn := with.e.L.Head(), without.e.L.Head(); hw != hn {
		t.Fatalf("chain head with sink %s, without %s", hw, hn)
	}
	if !reflect.DeepEqual(with.e.Pending(), without.e.Pending()) {
		t.Fatal("pending decision differs with the sink attached")
	}
}

// Clone never carries the sink (spec §7), and planning on the clone never
// counts into the original's.
func TestPaymentPlanStatsNotCloned(t *testing.T) {
	t.Parallel()
	b := newPaymentStatsBoard(t)
	stats := &PaymentPlanStats{}
	b.e.SetPaymentPlanStats(stats)
	paymentPlanReask(t, b.e)
	before := *stats
	c := b.e.Clone()
	if c.PaymentPlanStats() != nil || c.paymentStats != nil {
		t.Fatal("Clone carried the payment-plan stats sink")
	}
	if got := c.PaymentActionsForPriority(0, c.Pending().Seq); len(got) != 1 {
		t.Fatalf("clone offer = %+v, want the Shock's action", got)
	}
	if !reflect.DeepEqual(*stats, before) {
		t.Fatalf("clone's build counted into the original's sink: %+v, was %+v", *stats, before)
	}
	if b.e.PaymentPlanStats() != stats {
		t.Fatal("original lost its sink")
	}
}

// A pool the planner cannot account for declines the whole build before any
// candidate is walked: it counts once, as a pool-declined build, and adds no
// per-cast outcome.
func TestPaymentPlanStatsPoolDeclinedBuild(t *testing.T) {
	t.Parallel()
	b := newPaymentStatsBoard(t)
	stats := &PaymentPlanStats{}
	b.e.SetPaymentPlanStats(stats)
	b.e.G.Players[0].Snow[state.MR] = 1
	if got := b.e.PaymentActionsForPriority(0, 1); len(got) != 0 {
		t.Fatalf("offer on a snow pool = %+v, want none", got)
	}
	want := PaymentPlanStats{DecisionsBuilt: 1, PoolDeclined: 1}
	if !reflect.DeepEqual(*stats, want) {
		t.Fatalf("stats = %+v, want %+v", *stats, want)
	}
}
