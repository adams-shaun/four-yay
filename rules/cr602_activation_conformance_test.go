package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 602.2b (4883-4886) imports 601.2b-i: announce choices, choose targets,
// lock total cost, activate mana abilities, THEN pay. CR 602.2 (4869-4875)
// requires reversal of an incomplete activation. Every leaf here passes with
// the conformance flag on, so all of them run in the ordinary lane. All SAs
// are corpus SAs.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func crActivationSA(t *testing.T, e *Engine, id state.ObjID, api, cost string) int {
	t.Helper()
	for i, sa := range e.G.Obj(id).Face().Abilities {
		if sa.Kind == "AB" && sa.API == api && sa.Params["Cost"] == cost {
			return i
		}
	}
	t.Fatalf("CR 602.2b %s seq %d: missing compiled %s cost %q", e.G.Obj(id).Face().Name, len(e.L.Events), api, cost)
	return -1
}

// Whole compiled-corpus PRICING invariant over top-level AB costs containing
// two-color hybrid symbols and otherwise ONLY literal mana. This excludes
// nonmana costs rather than pretending they are paid. It counts examined SAs,
// not cards or successful gameplay; seq 0 means there is no event timeline.
func TestCR602HybridActivationCostsRequireColor(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	checked, wrong := 0, 0
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, sa := range f.Abilities {
				if sa.Kind != "AB" {
					continue
				}
				pool, hybrids, admitted := state.Mana{}, 0, true
				for _, sym := range strings.Fields(sa.Params["Cost"]) {
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
							pool[state.MC] += int32(n)
						}
					}
				}
				if !admitted || hybrids == 0 {
					continue
				}
				checked++
				pool[state.MC] += int32(2 * hybrids)
				if ParseCost(sa.Params["Cost"]).CanPay(pool) {
					wrong++
					if wrong <= 8 {
						t.Errorf("CR 602.2b/107.4e %q seq 0: activation %s cost %q accepts %v with no color left for hybrid", f.Name, sa.API, sa.Params["Cost"], pool)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("CR 602.2b/107.4e corpus seq 0: no activation costs examined")
	}
	t.Logf("MEASURED CR 602.2b/107.4e seq 0: examined=%d wrongly accepted=%d compiled AB costs", checked, wrong)
}

func TestCR602PhyrexianActivationCannotPayColorlessWithoutLife(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Immolating Souleater")
	id := crAbortMove(t, e, 0, "Immolating Souleater", state.ZBattlefield)
	crActivationSA(t, e, id, "Pump", "RP")
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.askPriority(0)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Immolating Souleater", crAbortOption(t, e, "Immolating Souleater", "ability", id))
	// A proper announcement may suspend; it must not already spend colorless
	// without life. No requirement here that a particular decision kind exists.
	if e.G.Players[0].Pool[state.MC] == 0 && e.G.Players[0].Life == 20 {
		t.Errorf("CR 602.2b/107.4f Immolating Souleater seq %d: RP charged C1 and no life; pool=%v life=%d stack=%v", start, e.G.Players[0].Pool, e.G.Players[0].Life, e.G.Stack)
	}
}

func TestCR602ActivationTargetsPrecedePayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Qasali Pridemage")
	id := crAbortMove(t, e, 0, "Qasali Pridemage", state.ZBattlefield)
	target := crAbortMove(t, e, 1, "Aether Vial", state.ZBattlefield)
	ai := crActivationSA(t, e, id, "Destroy", "1 Sac<1/CARDNAME>")
	if e.G.Obj(id).Face().Abilities[ai].Params["ValidTgts"] != "Artifact,Enchantment" {
		t.Fatal("CR 602.2b Qasali Pridemage seq 0: target fixture changed")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.askPriority(0)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Qasali Pridemage", crAbortOption(t, e, "Qasali Pridemage", "ability", id))
	// Answer only a sacrifice selection, not the still-unanswered target ask.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		crAbortAnswer(t, e, "Qasali Pridemage", crAbortOption(t, e, "Qasali Pridemage", "sacrifice", id))
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("CR 602.2b Qasali Pridemage seq %d: no target decision: %+v", start, d)
	}
	crAbortOption(t, e, "Qasali Pridemage", "permanent", target) // fixed legal artifact exists
	if e.G.Players[0].Pool[state.MC] != 1 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Errorf("CR 602.2b -> 601.2c/h Qasali Pridemage seq %d: target ask seq %d unanswered but pool=%v source=%s; want C1 and battlefield", start, d.Seq, e.G.Players[0].Pool, e.G.Obj(id).Zone)
	}
}

func TestCR602IllegalActivationReversesSacrificeAndConsequences(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Qasali Pridemage", "Blood Artist")
	id := crAbortMove(t, e, 0, "Qasali Pridemage", state.ZBattlefield)
	artist := crAbortMove(t, e, 0, "Blood Artist", state.ZBattlefield)
	crActivationSA(t, e, id, "Destroy", "1 Sac<1/CARDNAME>")
	// Independent legality oracle: only two creatures, no artifact/enchantment
	// on either battlefield. Blood Artist detects the CONSEQUENCE of an illegal
	// sacrifice, not activation itself; legal activation watchers are a separate test.
	for _, p := range []state.PlayerID{0, 1} {
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			for _, typ := range e.G.Obj(oid).Face().Types {
				if typ == "Artifact" || typ == "Enchantment" {
					t.Fatal("CR 602.2 Qasali Pridemage seq 0: fixture accidentally has a legal target")
				}
			}
		}
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.askPriority(0)
	start := len(e.L.Events)
	// Correct offer-time rejection is also conformant; no direct manufactured
	// option is needed to prove the currently reachable illegal action.
	idx := -1
	for _, opt := range e.Pending().Options {
		if opt.Kind == "ability" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx >= 0 {
		crAbortAnswer(t, e, "Qasali Pridemage", idx)
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			crAbortAnswer(t, e, "Qasali Pridemage", crAbortOption(t, e, "Qasali Pridemage", "sacrifice", id))
		}
	}
	if e.G.Players[0].Pool[state.MC] != 1 || e.G.Obj(id).Zone != state.ZBattlefield || len(e.G.Stack) != 0 {
		t.Errorf("CR 602.2/733.1 Qasali Pridemage seq %d: illegal activation not reversed: pool=%v source=%s stack=%v", start, e.G.Players[0].Pool, e.G.Obj(id).Zone, e.G.Stack)
	}
	for _, pt := range e.pendingTriggers {
		if pt.Source == artist {
			t.Errorf("CR 602.2/733.1 Blood Artist seq %d: illegal sacrifice left a queued death trigger", start)
		}
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.TriggerPush && ev.Obj == artist {
			t.Errorf("CR 602.2/733.1 Qasali Pridemage seq %d: illegal sacrifice produced Blood Artist TriggerPush seq %d", start, ev.Seq)
		}
	}
}

func TestCR602ManaWindowDuringActivation(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Azure Mage", "Island", "Island", "Island", "Island")
	id := crAbortMove(t, e, 0, "Azure Mage", state.ZBattlefield)
	ai := crActivationSA(t, e, id, "Draw", "3 U")
	var lands []state.ObjID
	for i := 0; i < 4; i++ {
		lands = append(lands, crAbortMove(t, e, 0, "Island", state.ZBattlefield))
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatal("CR 602.2b Azure Mage seq 0: expected empty pool")
	}
	e.askPriority(0)
	start := len(e.L.Events)
	e.pending = nil // defensive entry bypasses pool-only offer gate
	e.beginActivation(0, decision.Option{Kind: "ability", Obj: id, Ability: ai})
	e.Advance()
	window := false
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		for _, opt := range d.Options {
			for _, land := range lands {
				if opt.Kind == "activate" && opt.Obj == land {
					window = true
				}
			}
		}
	}
	if !window {
		t.Errorf("CR 602.2b -> 601.2g Azure Mage seq %d: four untapped Islands but no mana window inside activation; pool=%v stack=%v next=%+v", start, e.G.Players[0].Pool, e.G.Stack, e.Pending())
	}
}

func TestCR602ActivationCostIncludesReduction(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Azure Mage", "Heartstone")
	id := crAbortMove(t, e, 0, "Azure Mage", state.ZBattlefield)
	heart := crAbortMove(t, e, 0, "Heartstone", state.ZBattlefield)
	crActivationSA(t, e, id, "Draw", "3 U")
	guard := false
	for _, st := range e.G.Obj(heart).Face().Statics {
		if st.Mode == "ReduceCost" && st.Params["Type"] == "Ability" && st.Params["Amount"] == "1" {
			guard = true
		}
	}
	if !guard {
		t.Fatal("CR 602.2b Azure Mage/Heartstone seq 0: missing real activation reducer")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 4})
	e.askPriority(0)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Azure Mage", crAbortOption(t, e, "Azure Mage", "ability", id))
	// Independent cost: {3}{U} minus {1} = {2}{U}; minimum-one floor irrelevant.
	if len(e.G.Stack) != 1 || e.G.Players[0].Pool[state.MU] != 1 {
		t.Errorf("CR 602.2b -> 601.2f Azure Mage/Heartstone seq %d: activation should leave U1; pool=%v stack=%v", start, e.G.Players[0].Pool, e.G.Stack)
	}
}

func TestCR602NewNoncreatureCanActivateTapAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "death-n-taxes")
	id := crAbortMove(t, e, 0, "Aether Vial", state.ZBattlefield)
	ai := crActivationSA(t, e, id, "ChangeZone", "T")
	for _, typ := range e.G.Obj(id).Face().Types {
		if typ == "Creature" {
			t.Fatal("CR 602.5a Aether Vial seq 0: fixture is a creature")
		}
	}
	e.askPriority(0)
	start := len(e.L.Events)
	// Vial's effect is optional and untargeted. It need not have a creature
	// available to put into play, and ONLY creatures are covered by 602.5a.
	if _, ok := findAbilityOption(e, id, ai); !ok {
		t.Errorf("CR 602.5a Aether Vial seq %d: untapped noncreature's T ability withheld on entry; tapped=%t summonSick=%t", start, e.G.Obj(id).Tapped, e.G.Obj(id).SummonSick)
	}
}

func TestCR602OncePerTurnActivationRestriction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Basking Rootwalla")
	id := crAbortMove(t, e, 0, "Basking Rootwalla", state.ZBattlefield)
	ai := crActivationSA(t, e, id, "Pump", "1 G")
	if e.G.Obj(id).Face().Abilities[ai].Params["ActivationLimit"] != "1" {
		t.Fatal("CR 602.5 Basking Rootwalla seq 0: fixture lost once-per-turn restriction")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 4})
	e.askPriority(0)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Basking Rootwalla", crAbortOption(t, e, "Basking Rootwalla", "ability", id))
	if len(e.G.Stack) != 1 {
		t.Fatalf("CR 602.5 Basking Rootwalla seq %d: first legal activation did not complete", start)
	}
	for _, opt := range e.Pending().Options {
		if opt.Kind == "ability" && opt.Obj == id && opt.Ability == ai {
			t.Errorf("CR 602.1b/602.5 Basking Rootwalla seq %d: second activation offered same turn %d with first on stack (option %d)", start, e.G.Turn, opt.Index)
		}
	}
}
