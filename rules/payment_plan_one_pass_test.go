package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Ticket aph-offer-one-pass: the payment extension is built from ONE
// PotentialMana candidate walk, ONE hypothetical huge-pool legality walk and
// the pending decision's own Options, instead of the pre-ticket N+2 walks
// (the candidate walk, a duplicate legalActions for BaseOptionIndex, and one
// huge-pool walk per candidate inside PlanCastPayment). The offers must be
// byte-identical to the shared-builder result, which paymentActionsReference
// keeps as the test-only oracle.

// paymentActionsReference is the equivalence oracle: an independent
// re-implementation of the PaymentActionsForPriority body (originally kept
// verbatim from the aph-offer-one-pass builder; updated in ticket
// fb-20260927T163321Z-69285807 to derive each candidate's PlannedCast origin
// from its object's zone, hand → "hand" and command zone → "command_zone",
// exactly as the production builder does -- anything else is skipped). Every
// candidate goes through the public PlanCastPayment (whose own huge-pool
// candidate walk is unchanged), and BaseOptionIndex comes from a fresh
// legalActions walk.
func paymentActionsReference(e *Engine, p state.PlayerID, seq uint64) []decision.PaymentAction {
	if e.G.Over {
		return nil
	}
	hyp := e.PotentialMana(p)
	candidates := e.legalActionsPriced(p, &hyp)
	legacy := e.legalActions(p)
	var out []decision.PaymentAction
	for _, opt := range candidates {
		if opt.Kind != "cast" || opt.Mode != "" || opt.AltCostIndex != 0 {
			continue
		}
		// The origin comes from the object's actual zone, mirroring the
		// production builder: only a hand or command-zone object can ever
		// carry a plain (Mode "" AltCostIndex 0) cast; anything else is
		// skipped.
		originObj := e.G.Obj(opt.Obj)
		if originObj == nil {
			continue
		}
		var origin string
		switch originObj.Zone {
		case state.ZHand:
			origin = "hand"
		case state.ZCommand:
			origin = "command_zone"
		default:
			continue
		}
		cast := decision.PlannedCast{Object: opt.Obj, Face: 0, Origin: origin}
		got := e.PlanCastPayment(p, cast)
		if got.Plan == nil {
			continue
		}
		plan := *got.Plan
		pid, err := decision.PaymentPlanID(seq, p, cast, plan)
		if err != nil {
			continue
		}
		plan.ID = pid
		aid, err := decision.PaymentActionID(decision.PaymentPlanV1, seq, p, cast)
		if err != nil {
			continue
		}
		a := decision.PaymentAction{ID: aid, Cast: cast, Label: opt.Label, Plans: []decision.PaymentPlan{plan}}
		for i := range legacy {
			if legacy[i].Kind == "cast" && legacy[i].Obj == opt.Obj && legacy[i].Mode == "" && legacy[i].AltCostIndex == 0 {
				idx := legacy[i].Index
				a.BaseOptionIndex = &idx
				break
			}
		}
		out = append(out, a)
	}
	return out
}

// onePassHugeRejects counts the PotentialMana walk's plain hand casts that
// the huge-pool walk does NOT list: the diagnostic behind keeping both
// walks (a nonzero count would mean the huge walk filters something the
// PotentialMana walk admits).
func onePassHugeRejects(e *Engine, p state.PlayerID) int {
	hyp := e.PotentialMana(p)
	n := 0
	for _, opt := range e.legalActionsPriced(p, &hyp) {
		if opt.Kind != "cast" || opt.Mode != "" || opt.AltCostIndex != 0 {
			continue
		}
		if o := e.G.Obj(opt.Obj); o == nil || o.Zone != state.ZHand {
			continue
		}
		if !e.paymentPlanCastCandidate(p, opt.Obj) {
			n++
		}
	}
	return n
}

// onePassCompare builds the pending priority decision's extension through
// the lazy consumer path and compares it, IDs and BaseOptionIndex included,
// with the reference oracle computed at the same state. It returns the
// number of offered actions.
func onePassCompare(t *testing.T, e *Engine, where string) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("%s: pending = %#v, want priority", where, d)
	}
	want := paymentActionsReference(e, d.Player, d.Seq)
	head, n := e.L.Head(), len(e.L.Events)
	got := e.EnsurePaymentActions()
	// EnsurePaymentActions stores a Decision.Clone of the builder's result
	// (an empty, non-nil slice for no offers); compare like for like.
	if published := (&decision.Decision{PaymentActions: want}).Clone().PaymentActions; !reflect.DeepEqual(got, published) {
		t.Fatalf("%s: one-pass offers differ from the reference oracle\n got %#v\nwant %#v", where, got, want)
	}
	if e.L.Head() != head || len(e.L.Events) != n {
		t.Fatalf("%s: building offers changed the log", where)
	}
	// The public, options-free entry must agree too.
	if again := e.PaymentActionsForPriority(d.Player, d.Seq); !reflect.DeepEqual(again, want) {
		t.Fatalf("%s: PaymentActionsForPriority differs from the reference oracle\n got %#v\nwant %#v", where, again, want)
	}
	return len(got)
}

// onePassToHand puts one authored card straight into p's hand, eventless,
// the hand-zone twin of onBoard.
func onePassToHand(tb testing.TB, e *Engine, p state.PlayerID, src string) state.ObjID {
	tb.Helper()
	o := e.G.AddObject(card(tb, src), p)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1
	e.typesEpoch = -1
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
	return o.ID
}

// onePassEngine is a two-seat Mountain-deck game at seat 0's first priority
// ask (turn-1 upkeep), before any fixture is placed.
func onePassEngine(tb testing.TB, seed uint64) *Engine {
	tb.Helper()
	e := New(seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(tb, 40), mountainDeck(tb, 40)}}))
	e.Advance()
	return e
}

func onePassReask(e *Engine, p state.PlayerID) {
	e.pending = nil
	e.askPriority(p)
}

const (
	opIsland   = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	opMountain = "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"
	opSwamp    = "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"
	opForest   = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
	opBadlands = "Name:OP Badlands\nTypes:Land Swamp Mountain\nOracle:x\n"
	opBolt     = "Name:OP Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	opDraw     = "Name:OP Draw\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	opGrixis   = "Name:OP Grixis\nManaCost:1 U B\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	opBig      = "Name:OP Big\nManaCost:5 B B\nTypes:Instant\nA:SP$ Draw | NumCards$ 2\nOracle:x\n"
	opBlaze    = "Name:OP Blaze\nManaCost:X R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ X\nSVar:X:Count$xPaid\nOracle:x\n"
	opHybrid   = "Name:OP Hybrid\nManaCost:1 R/G\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	opFree     = "Name:OP Free\nManaCost:0\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	opSorcery  = "Name:OP Sorcery\nManaCost:R\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	opShatter  = "Name:OP Shatter\nManaCost:R\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Artifact\nOracle:x\n"
	opKicker   = "Name:OP Kicked\nManaCost:R\nTypes:Instant\nK:Kicker:1\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	opTwoCost  = "Name:OP Two Costs\nManaCost:U\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nS:Mode$ AlternativeCost | ValidCard$ Card.Self | Cost$ U\nOracle:x\n"
	opFlashGuy = "Name:OP Flash Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Flash\nOracle:x\n"
	opReducer  = "Name:OP Reducer\nTypes:Artifact\nS:Mode$ ReduceCost | ValidCard$ Instant | Type$ Spell | Activator$ You | Amount$ 1\nOracle:x\n"
	opLock     = "Name:OP Lock\nTypes:Enchantment\nS:Mode$ CantBeCast | ValidCard$ Card.cmcEQ2\nOracle:x\n"
)

// TestPaymentPlanOnePassMatchesReferenceOnFixtureBoards pins byte-identical
// offers (IDs, plans, labels, BaseOptionIndex) on fixture boards chosen to
// reach every early exit of the builder: plain funded casts, insufficient
// and unsupported shapes, a castable-now cast with a base option, floating
// and restricted pools, cost statics, cast prohibitions, the other seat, and
// a board with no sources.
func TestPaymentPlanOnePassMatchesReferenceOnFixtureBoards(t *testing.T) {
	t.Parallel()
	type board struct {
		name  string
		seat  state.PlayerID
		build func(t *testing.T, e *Engine)
		// minOffers is a precondition floor so a board that silently stops
		// offering anything cannot pass vacuously.
		minOffers int
	}
	hand := func(e *Engine, p state.PlayerID, t *testing.T, srcs ...string) {
		for _, s := range srcs {
			onePassToHand(t, e, p, s)
		}
	}
	lands := func(e *Engine, p state.PlayerID, t *testing.T, srcs ...string) {
		for _, s := range srcs {
			onBoard(t, e, p, s)
		}
	}
	boards := []board{
		{"mixed hand on basics and a dual", 0, func(t *testing.T, e *Engine) {
			lands(e, 0, t, opIsland, opIsland, opMountain, opSwamp, opBadlands)
			hand(e, 0, t, opBolt, opDraw, opGrixis, opBig, opBlaze, opHybrid, opSorcery, opShatter, opKicker, opFlashGuy)
		}, 3},
		{"duplicate copies and a free spell", 0, func(t *testing.T, e *Engine) {
			lands(e, 0, t, opMountain, opMountain)
			hand(e, 0, t, opBolt, opBolt, opFree, opTwoCost)
		}, 3},
		{"plain floating pool gives base options", 0, func(t *testing.T, e *Engine) {
			lands(e, 0, t, opIsland)
			hand(e, 0, t, opDraw, opBolt, opTwoCost)
			e.G.Players[0].Pool[state.ManaIndex('U')] = 1
			e.G.Players[0].Pool[state.ManaIndex('R')] = 1
		}, 2},
		{"typed floating mana declines every plan", 0, func(t *testing.T, e *Engine) {
			lands(e, 0, t, opIsland, opMountain)
			hand(e, 0, t, opDraw, opBolt)
			e.G.Players[0].Snow[state.ManaIndex('R')] = 1
		}, 0},
		{"cost reducer and a cast prohibition", 0, func(t *testing.T, e *Engine) {
			lands(e, 0, t, opIsland, opMountain, opForest)
			lands(e, 0, t, opReducer, opLock)
			hand(e, 0, t, opDraw, opGrixis, opBolt, opFlashGuy, opHybrid)
		}, 1},
		{"no sources at all", 0, func(t *testing.T, e *Engine) {
			hand(e, 0, t, opBolt, opDraw, opFree)
		}, 1},
		{"the other seat's priority", 1, func(t *testing.T, e *Engine) {
			lands(e, 1, t, opIsland, opSwamp, opMountain)
			lands(e, 0, t, opIsland, opIsland)
			hand(e, 1, t, opGrixis, opBolt, opBig)
			hand(e, 0, t, opDraw)
		}, 2},
		{"last-resort source only funds nothing", 0, func(t *testing.T, e *Engine) {
			onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
			lands(e, 0, t, opSwamp)
			hand(e, 0, t, opDraw, opBolt, opGrixis)
		}, 0},
		{"normal sources beside a last-resort one", 0, func(t *testing.T, e *Engine) {
			onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
			lands(e, 0, t, opSwamp, opSwamp, opIsland)
			hand(e, 0, t, "Name:OP Two\nManaCost:2\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n", opGrixis, opBig)
		}, 2},
	}
	for i, b := range boards {
		t.Run(b.name, func(t *testing.T) {
			e := onePassEngine(t, uint64(97100+i))
			b.build(t, e)
			onePassReask(e, b.seat)
			got := onePassCompare(t, e, b.name)
			if got < b.minOffers {
				t.Fatalf("precondition: %d offers, want at least %d", got, b.minOffers)
			}
			if b.minOffers == 0 && got != 0 {
				t.Fatalf("precondition: %d offers on a board that must offer none", got)
			}
			if n := onePassHugeRejects(e, b.seat); n != 0 {
				t.Logf("huge-pool walk rejected %d PotentialMana candidates", n)
			}
			// The same answer after a clone taken before the build.
			fresh := onePassEngine(t, uint64(97100+i))
			b.build(t, fresh)
			onePassReask(fresh, b.seat)
			clone := fresh.Clone()
			if !reflect.DeepEqual(clone.EnsurePaymentActions(), fresh.EnsurePaymentActions()) {
				t.Fatal("clone-before-build offers differ from the original's")
			}
		})
	}
}

// onePassGameStats accumulates the auto-pay game drive's coverage.
type onePassGameStats struct {
	decisions, offered, planned, hugeRejects, staleOptions int
}

// onePassDriveGame plays one game to its end (or the intent cap) in which
// every seat auto-pays -- a seat with any offer submits the first offered
// plan unless its policy played a land -- comparing the one-pass offers with
// the reference builder at EVERY priority decision.
func onePassDriveGame(t *testing.T, cfg Config, botSeed uint64, st *onePassGameStats) {
	t.Helper()
	e := New(cfg)
	b := newTestBot(botSeed)
	e.Advance()
	for n := 0; !e.G.Over && e.Pending() != nil && n < 3000; n++ {
		d := e.Pending()
		if d.Kind != decision.KPriority {
			if err := e.Submit(b.answer(e, d)); err != nil {
				t.Fatalf("%v seed %d intent %d: %v", cfg.Names, cfg.Seed, n, err)
			}
			continue
		}
		st.decisions++
		st.hugeRejects += onePassHugeRejects(e, d.Player)
		// A priority ask deferred behind a commander-zone choice keeps the
		// Options it was built with; count any drift from a fresh walk (the
		// reference builder read the fresh walk, the one-pass builder reads
		// the offered list).
		if !reflect.DeepEqual(d.Options, e.legalActions(d.Player)) {
			st.staleOptions++
		}
		k := onePassCompare(t, e, "game")
		st.offered += k
		if st.decisions%25 == 0 {
			clone := e.Clone()
			clone.Pending().PaymentActions, clone.Pending().PaymentActionsBuilt = nil, false
			if !reflect.DeepEqual(clone.EnsurePaymentActions(), d.PaymentActions) {
				t.Fatalf("%v seed %d intent %d: clone rebuild differs", cfg.Names, cfg.Seed, n)
			}
		}
		in := b.answer(e, d)
		landDrop := len(in.Choices) == 1 && in.Choices[0] < len(d.Options) && d.Options[in.Choices[0]].Kind == "play_land"
		if k > 0 && !landDrop {
			a := d.PaymentActions[0]
			in = decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{
				ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0])}}
			st.planned++
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("%v seed %d intent %d: %v", cfg.Names, cfg.Seed, n, err)
		}
	}
}

// onePassBenchBoard is a fixed mid-game board: seat 0 holds nine lands
// (basics and a dual), a cost reducer and ten spells of every builder
// branch -- funded, insufficient, X, hybrid, kicker, alternative cost,
// no-target, wrong timing, flash creature -- while seat 1 holds creatures.
func onePassBenchBoard(tb testing.TB) *Engine {
	e := onePassEngine(tb, 97200)
	for _, s := range []string{opIsland, opIsland, opIsland, opMountain, opMountain, opSwamp, opSwamp, opForest, opBadlands, opReducer} {
		onBoard(tb, e, 0, s)
	}
	for i := 0; i < 4; i++ {
		onBoard(tb, e, 1, "Name:OP Wall\nManaCost:1 W\nTypes:Creature Wall\nPT:0/4\nK:Defender\nOracle:x\n")
	}
	for _, s := range []string{opBolt, opDraw, opGrixis, opBig, opBlaze, opHybrid, opKicker, opTwoCost, opShatter, opSorcery, opFlashGuy, opBolt} {
		onePassToHand(tb, e, 0, s)
	}
	onePassReask(e, 0)
	return e
}

// BenchmarkPriorityAskPaymentActions pins the cost of building one priority
// decision's payment extension on a fixed mid-game board.
func BenchmarkPriorityAskPaymentActions(b *testing.B) {
	e := onePassBenchBoard(b)
	d := e.Pending()
	if got := e.EnsurePaymentActions(); len(got) < 4 {
		b.Fatalf("precondition: bench board offers %d actions, want >= 4", len(got))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.PaymentActions, d.PaymentActionsBuilt = nil, false
		e.EnsurePaymentActions()
	}
}

// TestPaymentPlanOnePassBenchBoardMatchesReference keeps the benchmark board
// honest: it is also an equivalence fixture.
func TestPaymentPlanOnePassBenchBoardMatchesReference(t *testing.T) {
	t.Parallel()
	e := onePassBenchBoard(t)
	if n := onePassCompare(t, e, "bench board"); n < 4 {
		t.Fatalf("precondition: bench board offers %d actions, want >= 4", n)
	}
}

// TestPaymentPlanOnePassWalkCount pins the walk budget: however many
// candidates the board has, one build opens exactly one legal-action walk
// (the PotentialMana candidate walk; the huge-pool legality walk is implied
// by it -- paymentCastCandidates' priced) and never a legalActions walk for
// BaseOptionIndex. The verify modes that re-run the skipped walks
// (pricedCandidatesVerify, castsOnlyWalkVerify) are off for the count. The count comes from
// Engine.legalActionWalks, which legalActionsPriced bumps on every call --
// the derived-memo generation delta CANNOT measure this since
// paymentActionsForPriority (merge 39948fe8c) opens ONE beginDerivedMemo
// scope around the whole build, so its generation advances by at most one
// no matter how many walks run inside. The counter is direct: a regression
// that runs `options = e.legalActions(p)` once per candidate cast (instead
// of once for the whole build) makes the delta climb with the candidate
// count, which this test then fails.
func TestPaymentPlanOnePassWalkCount(t *testing.T) {
	prevPriced, prevCasts := pricedCandidatesVerify, castsOnlyWalkVerify
	pricedCandidatesVerify, castsOnlyWalkVerify = false, false
	defer func() { pricedCandidatesVerify, castsOnlyWalkVerify = prevPriced, prevCasts }()
	e := onePassBenchBoard(t)
	walks := e.legalActionWalks
	if n := len(e.EnsurePaymentActions()); n < 4 {
		t.Fatalf("precondition: %d offers, want >= 4", n)
	}
	// Precondition: the counter must actually move on a build that offers
	// actions -- a counter never bumped, or a builder that never walks,
	// cannot pass this test silently.
	got := e.legalActionWalks - walks
	if got < 1 {
		t.Fatalf("precondition: building offers counted %d legal-action walks, want >= 1", got)
	}
	if got > 1 {
		t.Fatalf("building offers opened %d legal-action walks, want at most 1", got)
	}
	// A floating typed unit declines every plan before any walk runs.
	e.G.Players[0].Snow[state.ManaIndex('R')] = 1
	onePassReask(e, 0)
	walks = e.legalActionWalks
	if n := len(e.EnsurePaymentActions()); n != 0 {
		t.Fatalf("offers with floating snow mana = %d, want 0", n)
	}
	if got := e.legalActionWalks - walks; got != 0 {
		t.Fatalf("a declined pool still opened %d walks", got)
	}
}
