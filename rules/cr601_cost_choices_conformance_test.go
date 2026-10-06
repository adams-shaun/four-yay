package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07 revision.
// CR 601.2b (lines 4683-4696) announces modes, X and hybrid/Phyrexian
// payments before targets; 601.2f (4730-4739) composes and locks total cost;
// 601.2g (4741-4742) grants the mana-ability window before payment.
// These cost-choice assertions run in the ordinary suite.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This is a whole-compiled-corpus PRICING invariant, not a claim that every
// face can be cast from hand. Only literal generic, colored and two-color
// hybrid symbols are admitted. The pool has exactly the nonhybrid colored
// pips and ample colorless mana: nothing remains to pay even ONE hybrid pip.
// CR 107.4e (761-766) independently requires one of its two colors. No engine
// parser or target finder constructs the oracle. seq 0 denotes no game events.
func TestCR601HybridCostsCannotSpendOnlyColorless(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	checked, rejected := 0, 0
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			pool := state.Mana{}
			hybrids, generic, admitted := 0, int32(0), true
			for _, sym := range strings.Fields(f.ManaCost) {
				switch {
				case len(sym) == 1 && strings.ContainsAny(sym, "WUBRGC"):
					pool[state.ManaIndex(sym[0])]++
				case len(sym) == 2 && sym[0] != sym[1] && strings.ContainsAny(sym[:1], "WUBRG") && strings.ContainsAny(sym[1:], "WUBRG"):
					hybrids++
				default:
					n, err := strconv.Atoi(sym)
					if err != nil || n < 0 || n > 100 {
						admitted = false
					} else {
						generic += int32(n)
					}
				}
			}
			if !admitted || hybrids == 0 {
				continue
			}
			checked++ // examined costs, never only the erroneous acceptances
			pool[state.MC] += generic + int32(2*hybrids)
			if poolCanPay(ParseCost(f.ManaCost), pool) {
				rejected++
				if rejected <= 8 {
					t.Errorf("CR 601.2b/107.4e %q seq 0: cost %q accepts pool %v with no colored mana left for its hybrid symbols", f.Name, f.ManaCost, pool)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("CR 601.2b/107.4e corpus seq 0: no two-color hybrid costs examined")
	}
	t.Logf("MEASURED CR 601.2b/107.4e seq 0: examined=%d incorrectly accepted=%d compiled face costs", checked, rejected)
}

// Both Phyrexian probes are real repo-deck cards. Judge's Familiar is a
// corpus supplement: creature spells legitimately have no explicit SP SA.
// Any route that actually spends colorless on a Phyrexian symbol must ALSO
// charge life; reaching an unanswered target ask already must not have paid
// or bypassed the 601.2b choice. These tests do not infer life payment from
// ParseCost, nor manufacture a payment decision or SA.
func TestCR601SpecialManaRequiresAnnouncement(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, tc := range []struct {
		name, deck, cost, api string
		mana                  int32
	}{
		{"Dismember", "dimir-tempo", "1 BP BP", "Pump", 3},
		{"Gitaxian Probe", "ur-delver", "UP", "RevealHand", 1},
		{"Judge's Familiar", "ur-delver", "WU", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := crAbortEngine(t, reg, tc.deck, tc.name)
			id := crAbortMove(t, e, 0, tc.name, state.ZHand)
			// A creature target exists for Dismember, avoiding the no-target abort.
			crAbortMove(t, e, 1, "Mother of Runes", state.ZBattlefield)
			f := e.G.Obj(id).Face()
			sa := f.SpellAbility()
			if f.ManaCost != tc.cost || (tc.api != "" && (sa == nil || sa.API != tc.api)) {
				t.Fatalf("CR 601.2b %s seq %d: compiled fixture changed", tc.name, len(e.L.Events))
			}
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: tc.mana})
			e.askPriority(0)
			before, start := e.G.Players[0], len(e.L.Events)
			checked++
			// A future correct offer gate can reject Familiar's colorless-only
			// proposal. Phyrexian cards remain payable with life in this fixture.
			idx := -1
			for _, opt := range e.Pending().Options {
				if opt.Kind == "cast" && opt.Obj == id && opt.Mode == "" && opt.AltCostIndex == 0 {
					idx = opt.Index
					break
				}
			}
			if idx < 0 {
				if tc.api == "" {
					return
				}
				t.Fatalf("CR 601.2b %s seq %d: payable Phyrexian life-payment proposal withheld", tc.name, start)
			}
			crAbortAnswer(t, e, tc.name, idx)
			d := e.Pending()
			t.Logf("MEASURED %s seq %d: mana=%q SA=%s pool=%v -> %v life=%d -> %d next=%+v", tc.name, start, f.ManaCost, tc.api, before.Pool, e.G.Players[0].Pool, before.Life, e.G.Players[0].Life, d)
			if tc.api == "" {
				if e.G.Obj(id).Zone == state.ZStack {
					t.Errorf("CR 601.2b/107.4e %s seq %d: cast with only colorless, no W or U", tc.name, start)
				}
				return
			}
			if e.G.Players[0].Pool != before.Pool || e.G.Players[0].Life != before.Life || d == nil || d.Kind == decision.KTarget || d.Kind == decision.KPriority {
				t.Errorf("CR 601.2b/107.4f %s seq %d: no payment announcement before targets/payment: pool=%v life=%d next=%+v", tc.name, start, e.G.Players[0].Pool, e.G.Players[0].Life, d)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 601.2b special-mana corpus seq 0: examined zero proposals")
	}
}

// A modal spell announces its modes on the cast proposal before targets or
// payment (CR 601.2b), not when the spell resolves.
func TestCR601ModesAnnouncedBeforePayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, name := range []string{"Azorius Charm", "Boros Charm"} {
		t.Run(name, func(t *testing.T) {
			e := crAbortEngine(t, reg, "ur-delver", name)
			id := crAbortMove(t, e, 0, name, state.ZHand)
			sa := e.G.Obj(id).Face().SpellAbility()
			if sa == nil || sa.API != "Charm" || sa.Params["Choices"] == "" {
				t.Fatalf("CR 601.2b %s seq %d: missing compiled modal SA", name, len(e.L.Events))
			}
			for _, color := range []string{"W", "U", "R"} {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: color, Amount: 1})
			}
			e.askPriority(0)
			before, start := e.G.Players[0].Pool, len(e.L.Events)
			checked++
			crAbortAnswer(t, e, name, crAbortOption(t, e, name, "cast", id))
			d := e.Pending()
			if d == nil || d.Kind != decision.KModes || d.Source != id || e.G.Players[0].Pool != before {
				t.Errorf("CR 601.2b %s seq %d: mode must be announced before payment; pool=%v -> %v next=%+v", name, start, before, e.G.Players[0].Pool, d)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 601.2b modal corpus seq 0: examined zero proposals")
	}
}

// Fixed oracles: Power Sink X=2 costs {1}{U} with Electromancer, not {2}{U}.
// Faithless Looting's flashback {2}{R} costs {3}{R} under Thalia. Reducing
// before folding X, or replacing an already-taxed cost, loses the modifier.
func TestCR601TotalCostIncludesModifiers(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, arm := range []string{"X_reduction", "flashback_tax"} {
		t.Run(arm, func(t *testing.T) {
			name := "Power Sink"
			if arm == "flashback_tax" {
				name = "Faithless Looting"
			}
			e := crAbortEngine(t, reg, "ur-delver", name, "Goblin Electromancer")
			zone := state.ZHand
			if arm == "flashback_tax" {
				zone = state.ZGraveyard
			}
			id := crAbortMove(t, e, 0, name, zone)
			if arm == "X_reduction" {
				reducer := crAbortMove(t, e, 0, "Goblin Electromancer", state.ZBattlefield)
				if e.G.Obj(id).Face().ManaCost != "X U" || len(e.G.Obj(reducer).Face().Statics) == 0 {
					t.Fatalf("CR 601.2f %s seq %d: fixture changed", name, len(e.L.Events))
				}
				target := crAbortMove(t, e, 1, "Aether Vial", state.ZHand)
				e.emit(events.Event{Kind: events.PutOnStack, Obj: target, Player: 1, From: state.ZHand, To: state.ZStack})
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 3})
			} else {
				crAbortMove(t, e, 1, "Thalia, Guardian of Thraben", state.ZBattlefield)
				if cost, ok := e.G.Obj(id).Face().KeywordParam("Flashback"); !ok || cost != "2 R" {
					t.Fatalf("CR 601.2f %s seq %d: flashback fixture changed", name, len(e.L.Events))
				}
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 4})
			}
			e.askPriority(0)
			start := len(e.L.Events)
			checked++
			crAbortAnswer(t, e, name, crAbortOption(t, e, name, "cast", id))
			if arm == "X_reduction" {
				idx := -1
				if d := e.Pending(); d != nil {
					for _, opt := range d.Options {
						if opt.Kind == "x" && opt.Amount == 2 {
							idx = opt.Index
						}
					}
				}
				if idx < 0 {
					t.Fatalf("CR 601.2b/f %s seq %d: no payable X=2 choice", name, start)
				}
				crAbortAnswer(t, e, name, idx)
				if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
					t.Fatalf("CR 601.2c/f %s seq %d: no target ask", name, start)
				}
				// The only other stack object is the independently placed Aether Vial.
				crAbortAnswer(t, e, name, crAbortOption(t, e, name, "spell", e.G.Stack[0]))
			}
			want := int32(0)
			if arm == "X_reduction" {
				want = 1
			}
			if e.G.Obj(id).Zone != state.ZStack || e.G.Players[0].Pool.Total() != want {
				t.Errorf("CR 601.2f %s seq %d: composed cost leaves pool total %d, want %d; zone=%s", name, start, e.G.Players[0].Pool.Total(), want, e.G.Obj(id).Zone)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 601.2f corpus seq 0: examined zero cost compositions")
	}
}

// Altar's Reap's explicit SP Cost carries its mandatory additional sacrifice.
// No creature exists to pay it; two black mana alone can never complete this
// proposal. The CR 601.2h example (4748-4751) names this very card.
func TestCR601AdditionalSacrificeIsPartOfTotalCost(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Altar's Reap")
	id := crAbortMove(t, e, 0, "Altar's Reap", state.ZHand)
	sa := e.G.Obj(id).Face().SpellAbility()
	if sa == nil || sa.API != "Draw" || sa.Params["Cost"] != "1 B Sac<1/Creature>" || len(e.G.Zone(state.ZBattlefield, 0)) != 0 {
		t.Fatalf("CR 601.2f/h Altar's Reap seq %d: compiled additional-cost/empty-board fixture changed", len(e.L.Events))
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 2})
	e.askPriority(0)
	checked, start := 0, len(e.L.Events)
	before := e.G.Players[0].Pool
	e.pending = nil // bypass offer gate: execution must enforce costs too
	checked++
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()
	if e.G.Obj(id).Zone != state.ZHand || e.G.Players[0].Pool != before {
		t.Errorf("CR 601.2b/f/h Altar's Reap seq %d: no creature to sacrifice but proposal consumed resources: zone=%s pool=%v -> %v", start, e.G.Obj(id).Zone, before, e.G.Players[0].Pool)
	}
	if checked == 0 {
		t.Fatal("CR 601.2f/h Altar's Reap seq 0: examined zero additional-cost proposals")
	}
}

func TestCR601ManaWindowBeforePayment(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Mountain")
	id := crAbortMove(t, e, 0, "Lightning Bolt", state.ZHand)
	land := crAbortMove(t, e, 0, "Mountain", state.ZBattlefield)
	mas := e.G.Obj(land).Face().ManaAbilities()
	if len(mas) != 1 || mas[0].API != "Mana" || mas[0].Params["Produced"] != "R" || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("CR 601.2g Lightning Bolt seq %d: real Mountain/empty-pool fixture changed", len(e.L.Events))
	}
	e.askPriority(0)
	checked, start := 0, len(e.L.Events)
	// Direct proposal intentionally bypasses the pool-only offer gate, as in
	// the reversal tests. Pre-tapping at priority is NOT the 601.2g window.
	e.pending = nil
	checked++
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()
	// A correct lifecycle may first ask targets; answer the living opponent
	// from fixed board facts, never via targetBounds or legalTargets.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		idx := -1
		for _, opt := range d.Options {
			if opt.Kind == "player" && opt.Player == 1 {
				idx = opt.Index
			}
		}
		if idx < 0 {
			t.Fatalf("CR 601.2c/g Lightning Bolt seq %d: no living opponent target", start)
		}
		crAbortAnswer(t, e, "Lightning Bolt", idx)
	}
	d := e.Pending()
	window := false
	if d != nil && d.Kind != decision.KPriority {
		for _, opt := range d.Options {
			if opt.Kind == "activate" && opt.Obj == land {
				window = true
			}
		}
	}
	if !window {
		t.Errorf("CR 601.2g/605.3a Lightning Bolt seq %d: payable via untapped Mountain but no mid-cast mana window; source=%s tapped=%t pool=%v next=%+v", start, e.G.Obj(id).Zone, e.G.Obj(land).Tapped, e.G.Players[0].Pool, d)
	}
	if checked == 0 {
		t.Fatal("CR 601.2g Lightning Bolt seq 0: examined zero proposals")
	}
}
